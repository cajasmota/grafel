package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/cajasmota/grafel/internal/daemon"
	"github.com/cajasmota/grafel/internal/engine"
	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/graph/flows"
	"github.com/cajasmota/grafel/internal/links"
	"github.com/cajasmota/grafel/internal/registry"
)

// newLinksCmd is the hidden top-level entry point used by hooks. It
// exposes a single sub-command, `pass <group>`, that runs the three
// cross-repo link passes against the per-repo graph.json files of every
// repo in the group.
func newLinksCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:    "links",
		Short:  "Run cross-repo link passes",
		Hidden: true,
	}
	cmd.AddCommand(newLinksPassCmd())
	return cmd
}

// RunLinksForGroup is the watcher-facing entry point. It re-runs the
// cross-repo link passes for a named group, then runs the phantom-edge
// pass (#769) to promote cross-repo CALLS links into phantom Relationships
// on each source repo's graph.Document, and re-runs RunProcessFlow on
// any doc that gained phantom edges so Process entities reflect the new
// cross-repo chains. Writes all output to the canonical grafel home.
// Returns nil when the group has no per-repo graph.json files yet
// (links are a no-op until the indexer has run at least once).
func RunLinksForGroup(group string) error {
	return RunLinksForGroupCtx(context.Background(), group)
}

// RunLinksForGroupCtx is the context-aware form of RunLinksForGroup. It checks
// ctx.Err() at each heavy pass boundary (stage → link passes → phantom-edge
// promotion → sidecar writes) so a cancellation — notably a `grafel delete
// <group>` landing mid-rebuild (v0.1.8 leak fix) — stops the sequence within one
// sub-pass instead of running the full multi-minute link/phantom recompute to
// completion for a group that no longer exists. Cancellation between passes is
// reported as ctx.Err() so the caller can distinguish it from a real failure;
// the partial work already written to disk is harmless (the group is being torn
// down anyway).
func RunLinksForGroupCtx(ctx context.Context, group string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	groups, err := registry.Groups()
	if err != nil {
		return err
	}
	var ref *registry.GroupRef
	for i := range groups {
		if groups[i].Name == group {
			ref = &groups[i]
			break
		}
	}
	if ref == nil {
		return fmt.Errorf("unknown group: %s", group)
	}
	cfg, err := registry.LoadGroupConfig(ref.ConfigPath)
	if err != nil {
		return err
	}
	graphsDir, cleanup, err := stageGraphsDir(cfg)
	if err != nil {
		return err
	}
	defer cleanup()
	if err := ctx.Err(); err != nil {
		return err
	}
	// #2761 substrate Phase 0: register each repo's source path so the
	// constant propagation pass can read .ts / .py / .java / .go files
	// from the actual repo working tree (graphsDir contains symlinked
	// graph files only, not source).
	srcPaths := map[string]string{}
	for _, r := range cfg.Repos {
		srcPaths[r.Slug] = r.Path
	}
	links.SetRepoSourcePaths(srcPaths)
	// #5692: time the cross-repo link phase so its duration can be persisted
	// into each affected repo's graph-stats.json sidecar (link_ms) for
	// index-timing observability. Pure measurement — no behaviour change.
	linkStart := time.Now()
	res, err := links.RunAllPasses(group, graphsDir, "")
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	// P5 — phantom-edge promotion (#769).
	if _, perr := runPhantomEdgePass(group, cfg, res.OutLinks); perr != nil {
		// Best-effort: log but don't fail the link pass.
		fmt.Fprintf(os.Stderr, "grafel: phantom-edge pass warning: %v\n", perr)
	}
	// #5692: record link_ms into each group repo's dedicated link-stats.json.
	// The link pass is the SOLE writer of that file, so this never races the
	// reindex worker's graph-stats.json write. Best-effort — a write failure
	// never fails the link pass (pure observability).
	side := &graph.LinkStatsSidecar{
		Version:    1,
		ComputedAt: time.Now().UTC(),
		LinkMS:     time.Since(linkStart).Milliseconds(),
	}
	for _, r := range cfg.Repos {
		stateDir := daemon.StateDirForRepo(r.Path)
		if stateDir == "" {
			continue
		}
		if serr := graph.WriteLinkStats(stateDir, side); serr != nil {
			fmt.Fprintf(os.Stderr, "grafel: link-stats write warning (%s): %v\n", r.Slug, serr)
		}
	}
	return nil
}

func newLinksPassCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "pass <group>",
		Short: "Run P1/P2/P3 link passes for a group",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return errors.New("supply a group name")
			}
			return runLinksForGroup(cmd, args[0])
		},
	}
}

// runLinksForGroup loads the group config, builds a synthetic graphs dir
// where each repo's path resolves to its per-repo .grafel/graph.json,
// then invokes links.RunAllPasses. The graphs-dir convention used by
// loadAllGraphs is "any directory containing one or more graph.json
// files at any depth"; we pass the group state dir and write symlinks
// pointing at each repo's graph.json. To keep this hermetic we instead
// build a temporary scratch dir.
func runLinksForGroup(cmd *cobra.Command, group string) error {
	groups, err := registry.Groups()
	if err != nil {
		return err
	}
	var ref *registry.GroupRef
	for i := range groups {
		if groups[i].Name == group {
			ref = &groups[i]
			break
		}
	}
	if ref == nil {
		return fmt.Errorf("unknown group: %s", group)
	}
	cfg, err := registry.LoadGroupConfig(ref.ConfigPath)
	if err != nil {
		return err
	}

	graphsDir, cleanup, err := stageGraphsDir(cfg)
	if err != nil {
		return err
	}
	defer cleanup()

	// #2761 substrate Phase 0 (mirrors RunLinksForGroup): publish each
	// repo's source root so the constant propagation pass can lift
	// bindings from real source files.
	srcPaths := map[string]string{}
	for _, r := range cfg.Repos {
		srcPaths[r.Slug] = r.Path
	}
	links.SetRepoSourcePaths(srcPaths)

	res, err := links.RunAllPasses(group, graphsDir, "")
	if err != nil {
		return err
	}
	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "links: group=%s\n", res.Group)
	for _, r := range res.Results {
		fmt.Fprintf(out, "  %-7s links=%-4d candidates=%-4d skipped=%d\n",
			r.Pass, r.LinksAdded, r.Candidates, r.Skipped)
	}
	fmt.Fprintf(out, "  output: %s\n", res.OutLinks)

	// P5 — phantom-edge promotion (#769).
	phantomAdded, perr := runPhantomEdgePass(group, cfg, res.OutLinks)
	if perr != nil {
		fmt.Fprintf(out, "  phantom-edge pass warning: %v\n", perr)
	} else {
		fmt.Fprintf(out, "  phantom  edges_added=%-4d\n", phantomAdded)
	}
	return nil
}

// stageGraphsDir creates a scratch directory containing one sub-dir per
// repo. Each sub-dir has symlinks pointing at the repo's on-disk graph
// files (graph.fb and/or graph.json, or — for a SEGMENTED repo — the whole
// graph.<gen>/ segment dir plus a `current` pointer). This keeps the layout
// that loadAllGraphs/LoadGraphFromDir expects without duplicating bytes.
// ADR-0016 flip-day (#808): graph.fb is symlinked when present so
// LoadGraphFromDir can prefer the binary format in downstream passes.
//
// #5904(e) PR-c (#5915 P2 gap): staging used to resolve only the flat
// graph.fb path via graph.CurrentGraphPath, so a repo whose active generation
// is a multi-segment gen dir (graph.<gen>/seg-*.fb + manifest.json — no flat
// .fb ever exists for it) had hasFB==false and was silently DROPPED from
// staging, and therefore from every cross-repo link pass. This now resolves
// each repo via graph.CurrentGraphDescriptor and stages per its Kind:
//
//   - GraphSingleFile: symlink desc.Path as dstDir/graph.fb — BYTE-FOR-BYTE
//     the pre-existing behavior (the common path).
//   - GraphSegmentSet: symlink the whole generation into dstDir/graph.<gen>/
//     (every segment file + manifest.json) and write a `current` pointer in
//     dstDir naming that gen dir, so LoadGraphFromDir(dstDir) resolves it as
//     a segment-set exactly as it would at the repo's real state dir.
//   - GraphAbsent: nothing to stage for the fb side (graph.json, if any, is
//     still staged below).
func stageGraphsDir(cfg *registry.GroupConfig) (string, func(), error) {
	tmp, err := os.MkdirTemp("", "grafel-links-")
	if err != nil {
		return "", func() {}, err
	}
	cleanup := func() { _ = os.RemoveAll(tmp) }
	for _, r := range cfg.Repos {
		stateDir := daemon.StateDirForRepo(r.Path)
		jsonSrc := daemon.GraphPathForRepo(r.Path)
		hasJSON := func() bool { _, e := os.Stat(jsonSrc); return e == nil }()

		desc, derr := graph.CurrentGraphDescriptor(stateDir)
		if derr != nil {
			cleanup()
			return "", func() {}, fmt.Errorf("stage graphs: resolve %s: %w", r.Slug, derr)
		}
		if desc.Kind == graph.GraphAbsent && !hasJSON {
			continue
		}

		dstDir := filepath.Join(tmp, r.Slug)
		if err := os.MkdirAll(dstDir, 0o755); err != nil {
			cleanup()
			return "", func() {}, err
		}
		if hasJSON {
			if err := linkOrCopyFile(jsonSrc, filepath.Join(dstDir, "graph.json")); err != nil {
				cleanup()
				return "", func() {}, err
			}
		}

		switch desc.Kind {
		case graph.GraphSingleFile:
			if err := linkOrCopyFile(desc.Path, filepath.Join(dstDir, "graph.fb")); err != nil {
				cleanup()
				return "", func() {}, err
			}
		case graph.GraphSegmentSet:
			genDirName := filepath.Base(desc.GenDir)
			dstGenDir := filepath.Join(dstDir, genDirName)
			if err := os.MkdirAll(dstGenDir, 0o755); err != nil {
				cleanup()
				return "", func() {}, err
			}
			for _, seg := range desc.Segments {
				if err := linkOrCopyFile(seg, filepath.Join(dstGenDir, filepath.Base(seg))); err != nil {
					cleanup()
					return "", func() {}, err
				}
			}
			manifestSrc := filepath.Join(desc.GenDir, graph.ManifestFileName)
			if err := linkOrCopyFile(manifestSrc, filepath.Join(dstGenDir, graph.ManifestFileName)); err != nil {
				cleanup()
				return "", func() {}, err
			}
			// Point the staged repo's `current` pointer at the staged gen dir
			// so LoadGraphFromDir(dstDir) resolves the same segment-set shape
			// CurrentGraphDescriptor resolved at the real state dir.
			if err := graph.WriteCurrentPointerRaw(dstDir, genDirName); err != nil {
				cleanup()
				return "", func() {}, err
			}
		case graph.GraphAbsent:
			// Nothing further to stage; graph.json (if any) is already linked.
		}
	}
	return tmp, cleanup, nil
}

// linkOrCopyFile stages src at dst by symlink, falling back to a byte copy when
// the symlink fails. On Windows, creating a symlink requires
// SeCreateSymbolicLinkPrivilege (Developer Mode or admin); without it
// os.Symlink fails with "A required privilege is not held by the client",
// which previously aborted the cross-repo link passes and left cross-repo
// edges at 0. The staging dir is a read-only temp that is deleted afterwards,
// so a copy is an equivalent fallback. On Linux/mac the symlink never fails, so
// the fallback is never taken and behavior is unchanged. Mirrors the
// symlink→copy pattern used for skills in internal/install/dev.go.
func linkOrCopyFile(src, dst string) error {
	if err := os.Symlink(src, dst); err == nil {
		return nil
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}

// runPhantomEdgePass is the P5 phantom-edge promotion pass (#769).
//
// After links.RunAllPasses writes <group>-links.json, this pass:
//  1. Reads the links file.
//  2. Loads each source repo's graph.Document from disk.
//  3. Calls links.PromoteToPhantomEdges to inject phantom CALLS edges.
//  4. For each mutated document, strips the stale SCOPE.Process entities
//     (they were emitted before phantom edges existed) and re-runs
//     engine.RunProcessFlow so Process entities reflect the new
//     cross-repo chains.
//  5. Writes each updated document back to disk atomically.
//
// Returns the total number of phantom edges added across all repos.
// On error, returns what was added so far plus the first error — the
// caller decides whether to treat it as fatal.
//
// Architecture note (why here and not inside links.RunAllPasses):
// RunAllPasses operates on the links-internal `repoGraph` projection
// (no internal/graph import). Moving phantom-edge logic there would add
// a large bidirectional import between internal/links ↔ internal/graph ↔
// internal/engine. Placing it in the CLI layer (which already imports all
// three packages) keeps the dependency arrow pointing inward.
func runPhantomEdgePass(group string, cfg *registry.GroupConfig, linksPath string) (int, error) {
	// Read the just-written links file.
	allLinks, err := links.LoadLinksDocument(linksPath)
	if err != nil {
		return 0, fmt.Errorf("phantom-edge pass: load links: %w", err)
	}
	// NOTE (#5904 PR-b): do NOT early-return on len(allLinks)==0. When the LAST
	// cross-repo link is removed the links file goes empty, and this pass — the
	// sole writer of the flow side-table — must still run so the clear-stale loop
	// below wipes every loaded repo's now-obsolete flows.json. An early return
	// here would leave a repo's cross-repo flows serving fresh forever (its
	// source_key still matches until the repo is next reindexed). With no links
	// there is simply nothing to promote (added stays 0) and affectedRepos is
	// empty, so every loaded repo falls into the cleanup path.

	// Load each repo's graph.Document. Prefer graph.fb when available
	// (ADR-0016 flip-day #808); fall back to graph.json via LoadGraphFromDir.
	docs := make(map[string]*graph.Document, len(cfg.Repos))
	graphPaths := make(map[string]string, len(cfg.Repos)) // slug → graph.json path for WriteAtomic
	for _, r := range cfg.Repos {
		stateDir := daemon.StateDirForRepo(r.Path)
		jsonPath := daemon.GraphPathForRepo(r.Path)
		// Check that a graph exists before attempting load. #5904(e) PR-c: use
		// the segment-aware descriptor rather than a flat graph.fb stat, so a
		// SEGMENTED repo (graph.<gen>/ dir + manifest.json, no flat .fb) is not
		// skipped here — the load below (loadGraphDocument → LoadGraphFromDir)
		// is already segment-aware; only this existence gate needed to catch
		// up (#5915 P2 gap).
		desc, derr := graph.CurrentGraphDescriptor(stateDir)
		if derr != nil {
			return 0, fmt.Errorf("phantom-edge pass: resolve graph for %s: %w", r.Slug, derr)
		}
		hasJSON := func() bool { _, e := os.Stat(jsonPath); return e == nil }()
		if desc.Kind == graph.GraphAbsent && !hasJSON {
			continue // repo not indexed yet
		}
		doc, err := loadGraphDocument(stateDir)
		if err != nil {
			return 0, fmt.Errorf("phantom-edge pass: load %s: %w", r.Slug, err)
		}
		docs[r.Slug] = doc
		graphPaths[r.Slug] = jsonPath // WriteAtomic still writes graph.json
	}
	if len(docs) == 0 {
		return 0, nil
	}

	// Determine which source repos will receive phantom edges so we can
	// strip stale SCOPE.Process entities and re-run process flow.
	affectedRepos := make(map[string]bool)
	for _, lk := range allLinks {
		if !strings.EqualFold(lk.Relation, links.RelationCalls) {
			continue
		}
		srcRepo, _, ok := splitKey(lk.Source)
		if ok {
			affectedRepos[srcRepo] = true
		}
	}

	// Promote phantom edges.
	added, err := links.PromoteToPhantomEdges(allLinks, docs, group)
	if err != nil {
		return added, fmt.Errorf("phantom-edge pass: promote: %w", err)
	}

	// Re-run RunProcessFlow on each affected doc + write back to disk.
	slugs := sortedStringKeys(affectedRepos)
	for _, slug := range slugs {
		doc, ok := docs[slug]
		if !ok {
			continue
		}
		// Strip stale SCOPE.Process entities + their edges so the re-run
		// starts clean. Process entities have Kind=engine.EntityKindProcess.
		doc.Entities, doc.Relationships = stripProcessEntities(doc)

		// Re-run process flow. #1893 — pass the group's other docs as
		// companions so flows extend past phantom cross-repo edges into the
		// target repo's handler chain instead of dead-ending at the HTTP
		// boundary. Companions are read-only; only `doc` is mutated.
		companions := make([]*graph.Document, 0, len(docs)-1)
		for cslug, cdoc := range docs {
			if cslug == slug || cdoc == nil {
				continue
			}
			companions = append(companions, cdoc)
		}
		_ = engine.RunProcessFlowWithCompanions(doc, companions, engine.DefaultProcessFlowConfig())

		// #1944 Phase 1 — re-run the event-flow pub/sub walker too so
		// EventFlow entities are refreshed alongside ProcessFlows after
		// phantom-edge injection. Phase 1 walker is single-doc only;
		// the Phase 3 companion-aware variant will swap in here.
		_ = engine.RunEventFlow(doc, engine.DefaultEventFlowConfig())

		// #5686 — re-synthesise async-trigger DELIVERS_TO edges. Idempotent:
		// existing DELIVERS_TO edges survive stripProcessEntities, so this is a
		// no-op unless a phantom-edge injection introduced a new same-repo
		// subscription. Keeps the async inbound-trigger surface consistent with
		// a full re-index.
		_ = engine.ApplyAsyncTriggerEdges(doc)

		// Sort entities + relationships for determinism (mirrors index.go) so the
		// side-table delta is stable run-to-run.
		sort.SliceStable(doc.Entities, func(a, b int) bool {
			return doc.Entities[a].ID < doc.Entities[b].ID
		})
		sort.SliceStable(doc.Relationships, func(a, b int) bool {
			ra, rb := &doc.Relationships[a], &doc.Relationships[b]
			if ra.FromID != rb.FromID {
				return ra.FromID < rb.FromID
			}
			if ra.ToID != rb.ToID {
				return ra.ToID < rb.ToID
			}
			return ra.Kind < rb.Kind
		})

		// #5904 PR-b: repoint the SINK from a whole-graph rewrite onto the per-repo
		// flow SIDE-TABLE. The re-synthesised cross-repo-aware flow entities + their
		// step/entry/seed edges + the phantom cross_repo CALLS edges are the DELTA
		// over the index-baked graph; write ONLY that delta to <stateDir>/flows.json
		// via flows.Upsert. graph.fb / graph.json are NOT rewritten.
		//
		// This removes the #5915 P1 hazard: for a #5901 segment-set the resident
		// Document is a COLLAPSED union of every segment, so re-serialising it as one
		// flat graph.<gen>.fb re-materialised the whole graph in memory (OOM) and
		// discarded the segmented layout. The side-table leaves the graph untouched;
		// the read overlay (flows.MergeInto / MCP applyFlowOverlay) REPLACE-merges the
		// baked intra-repo flows with this delta at read time.
		stateDir := filepath.Dir(graphPaths[slug])
		flowEnts, flowRels := extractFlowDelta(doc)
		if err := flows.Upsert(stateDir, flowEnts, flowRels); err != nil {
			return added, fmt.Errorf("phantom-edge pass: write flow side-table %s: %w", slug, err)
		}
		fmt.Fprintf(os.Stderr,
			"grafel: phantom-edge pass group=%s repo=%s phantom_edges=%d flow_entities=%d (side-table)\n",
			group, slug, added, len(flowEnts))
	}

	// Repos that were loaded but are NOT (or no longer) affected by cross-repo
	// links must not keep a stale flows.json from a previous run — otherwise the
	// read overlay would resurrect old cross-repo flows for a repo whose links
	// were removed (without a reindex to invalidate the source_key). The phantom
	// pass is the sole writer of the flow side-table, so it owns this cleanup.
	for slug := range docs {
		if affectedRepos[slug] {
			continue
		}
		stateDir := filepath.Dir(graphPaths[slug])
		if err := os.Remove(flows.Path(stateDir)); err != nil && !os.IsNotExist(err) {
			fmt.Fprintf(os.Stderr, "grafel: phantom-edge pass: clear stale flow side-table %s: %v\n", slug, err)
		}
	}
	return added, nil
}

// extractFlowDelta returns the FLOW side-table delta from a re-synthesised
// document (#5904 PR-b): every SCOPE.Process / SCOPE.EventFlow entity, their
// STEP_IN_* / ENTRY_POINT_OF / SEED_OF_EVENT_FLOW structural edges, and the
// phantom cross_repo CALLS edges. This is exactly the set the read overlay
// SUBSTITUTES for the index-baked flows (flows.Apply strips the baked flows +
// their structural edges, then appends this delta) plus the phantom CALLS edges
// (which are never baked). Ordinary entities/edges are excluded — they already
// live in graph.fb and are not rewritten.
func extractFlowDelta(doc *graph.Document) ([]graph.Entity, []graph.Relationship) {
	var ents []graph.Entity
	for i := range doc.Entities {
		if doc.Entities[i].Kind == string(engine.EntityKindProcess) ||
			doc.Entities[i].Kind == engine.EntityKindEventFlow {
			ents = append(ents, doc.Entities[i])
		}
	}
	var rels []graph.Relationship
	for i := range doc.Relationships {
		r := &doc.Relationships[i]
		switch r.Kind {
		case string(engine.RelationshipKindStepInProcess),
			string(engine.RelationshipKindEntryPointOf),
			engine.RelationshipKindStepInEventFlow,
			engine.RelationshipKindSeedOfEventFlow:
			rels = append(rels, *r)
		case "CALLS":
			if r.PropGet("cross_repo") == "true" {
				rels = append(rels, *r)
			}
		}
	}
	return ents, rels
}

// stripProcessEntities returns new entity and relationship slices with all
// SCOPE.Process AND SCOPE.EventFlow entities (and their step / entry / seed
// edges) removed. Used before re-running RunProcessFlow + RunEventFlow after
// phantom-edge injection so the re-emit isn't duplicated on top of the stale
// pass.
func stripProcessEntities(doc *graph.Document) ([]graph.Entity, []graph.Relationship) {
	// Collect process + event-flow entity IDs to drop.
	droppedIDs := make(map[string]bool)
	for _, e := range doc.Entities {
		if e.Kind == string(engine.EntityKindProcess) || e.Kind == engine.EntityKindEventFlow {
			droppedIDs[e.ID] = true
		}
	}
	if len(droppedIDs) == 0 {
		return doc.Entities, doc.Relationships
	}
	entities := doc.Entities[:0:0]
	for _, e := range doc.Entities {
		if !droppedIDs[e.ID] {
			entities = append(entities, e)
		}
	}
	rels := doc.Relationships[:0:0]
	for _, r := range doc.Relationships {
		// Drop step/entry/seed edges for removed flow entities.
		if droppedIDs[r.FromID] || droppedIDs[r.ToID] {
			switch r.Kind {
			case string(engine.RelationshipKindStepInProcess),
				string(engine.RelationshipKindEntryPointOf),
				engine.RelationshipKindStepInEventFlow,
				engine.RelationshipKindSeedOfEventFlow:
				continue
			}
		}
		rels = append(rels, r)
	}
	return entities, rels
}

// loadGraphDocument loads a graph.Document from a state directory (the
// directory containing graph.fb / graph.json). Prefers graph.fb when
// present; falls back to graph.json. ADR-0016 flip-day (#808).
//
// The path argument MUST be the state directory (e.g. the value returned
// by daemon.StateDirForRepo), NOT the graph.json path itself.
func loadGraphDocument(stateDir string) (*graph.Document, error) {
	return graph.LoadGraphFromDir(stateDir)
}

// splitKey is a local thin wrapper around the shape used by Link.Source/Target:
// "<repo>::<entityID>".
func splitKey(key string) (repo, entityID string, ok bool) {
	const sep = "::"
	i := strings.Index(key, sep)
	if i <= 0 || i+len(sep) >= len(key) {
		return "", "", false
	}
	return key[:i], key[i+len(sep):], true
}

// sortedStringKeys returns the sorted key list of a map[string]bool.
func sortedStringKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
