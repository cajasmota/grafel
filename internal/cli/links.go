package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/cajasmota/archigraph/internal/daemon"
	"github.com/cajasmota/archigraph/internal/engine"
	"github.com/cajasmota/archigraph/internal/graph"
	"github.com/cajasmota/archigraph/internal/links"
	"github.com/cajasmota/archigraph/internal/registry"
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
// cross-repo chains. Writes all output to the canonical archigraph home.
// Returns nil when the group has no per-repo graph.json files yet
// (links are a no-op until the indexer has run at least once).
func RunLinksForGroup(group string) error {
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
	res, err := links.RunAllPasses(group, graphsDir, "")
	if err != nil {
		return err
	}
	// P5 — phantom-edge promotion (#769).
	if _, perr := runPhantomEdgePass(group, cfg, res.OutLinks); perr != nil {
		// Best-effort: log but don't fail the link pass.
		fmt.Fprintf(os.Stderr, "archigraph: phantom-edge pass warning: %v\n", perr)
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
// where each repo's path resolves to its per-repo .archigraph/graph.json,
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
// files (graph.fb and/or graph.json). This keeps the layout that
// loadAllGraphs expects without duplicating bytes. ADR-0016 flip-day
// (#808): graph.fb is symlinked when present so LoadGraphFromDir
// can prefer the binary format in downstream passes.
func stageGraphsDir(cfg *registry.GroupConfig) (string, func(), error) {
	tmp, err := os.MkdirTemp("", "archigraph-links-")
	if err != nil {
		return "", func() {}, err
	}
	cleanup := func() { _ = os.RemoveAll(tmp) }
	for _, r := range cfg.Repos {
		stateDir := daemon.StateDirForRepo(r.Path)
		jsonSrc := daemon.GraphPathForRepo(r.Path)
		fbSrc := filepath.Join(stateDir, "graph.fb")

		hasFB := func() bool { _, e := os.Stat(fbSrc); return e == nil }()
		hasJSON := func() bool { _, e := os.Stat(jsonSrc); return e == nil }()
		if !hasFB && !hasJSON {
			continue
		}
		dstDir := filepath.Join(tmp, r.Slug)
		if err := os.MkdirAll(dstDir, 0o755); err != nil {
			cleanup()
			return "", func() {}, err
		}
		if hasJSON {
			if err := os.Symlink(jsonSrc, filepath.Join(dstDir, "graph.json")); err != nil {
				cleanup()
				return "", func() {}, err
			}
		}
		if hasFB {
			if err := os.Symlink(fbSrc, filepath.Join(dstDir, "graph.fb")); err != nil {
				cleanup()
				return "", func() {}, err
			}
		}
	}
	return tmp, cleanup, nil
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
	if len(allLinks) == 0 {
		return 0, nil // nothing to promote
	}

	// Load each repo's graph.Document. Prefer graph.fb when available
	// (ADR-0016 flip-day #808); fall back to graph.json via LoadGraphFromDir.
	docs := make(map[string]*graph.Document, len(cfg.Repos))
	graphPaths := make(map[string]string, len(cfg.Repos)) // slug → graph.json path for WriteAtomic
	for _, r := range cfg.Repos {
		stateDir := daemon.StateDirForRepo(r.Path)
		fbPath := filepath.Join(stateDir, "graph.fb")
		jsonPath := daemon.GraphPathForRepo(r.Path)
		// Check that at least one graph file exists before attempting load.
		hasFB := func() bool { _, e := os.Stat(fbPath); return e == nil }()
		hasJSON := func() bool { _, e := os.Stat(jsonPath); return e == nil }()
		if !hasFB && !hasJSON {
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

		// Re-run process flow.
		_ = engine.RunProcessFlow(doc, engine.DefaultProcessFlowConfig())

		// Update stats.
		doc.Stats.Entities = len(doc.Entities)
		doc.Stats.Relationships = len(doc.Relationships)

		// Sort entities + relationships for determinism (mirrors index.go).
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

		// Write atomically.
		p := graphPaths[slug]
		if werr := graph.WriteAtomic(p, doc, false); werr != nil {
			return added, fmt.Errorf("phantom-edge pass: write %s: %w", slug, werr)
		}
		fmt.Fprintf(os.Stderr,
			"archigraph: phantom-edge pass group=%s repo=%s phantom_edges=%d\n",
			group, slug, added)
	}
	return added, nil
}

// stripProcessEntities returns new entity and relationship slices with all
// SCOPE.Process entities and their STEP_IN_PROCESS / ENTRY_POINT_OF edges
// removed. Used before re-running RunProcessFlow after phantom-edge injection.
func stripProcessEntities(doc *graph.Document) ([]graph.Entity, []graph.Relationship) {
	// Collect process entity IDs to drop.
	processIDs := make(map[string]bool)
	for _, e := range doc.Entities {
		if e.Kind == string(engine.EntityKindProcess) {
			processIDs[e.ID] = true
		}
	}
	if len(processIDs) == 0 {
		return doc.Entities, doc.Relationships
	}
	entities := doc.Entities[:0:0]
	for _, e := range doc.Entities {
		if !processIDs[e.ID] {
			entities = append(entities, e)
		}
	}
	rels := doc.Relationships[:0:0]
	for _, r := range doc.Relationships {
		// Drop STEP_IN_PROCESS and ENTRY_POINT_OF edges for removed processes.
		if processIDs[r.FromID] || processIDs[r.ToID] {
			if r.Kind == string(engine.RelationshipKindStepInProcess) ||
				r.Kind == string(engine.RelationshipKindEntryPointOf) {
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
