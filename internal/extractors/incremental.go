// Package extractors — incremental.go implements the S3 incremental
// file-level reindex path (issue #2153 of epic #2149).
//
// # Conservative v1 design (S3 #2167) + follow-up (S3 #2170)
//
// The full-reindex pipeline rewrites graph.fb from scratch on every daemon
// watcher tick. For a 60 k-entity repo that takes ~5 s. When only one file
// changed, we want ~200 ms: parse that file, swap its entities in the graph,
// and atomically re-emit graph.fb without touching anything else.
//
// Correctness guarantee: the opt-in flag (GRAFEL_INCREMENTAL_REINDEX=1)
// is NOT set by default. Four safety valves are applied before attempting a
// partial reindex (#2170 adds env-override limit + main-branch hot-path):
//
//  1. Trigger limit: if more than the effective limit files changed in the
//     debounced batch we fall back to full reindex. The effective limit is:
//     - GRAFEL_INCREMENTAL_MAX_FILES env var (if set to a valid int)
//     - 50 when the active ref is the repo's default (main) branch
//     - 20 otherwise (feature branches)
//     The #2167 conservative default of 5 is still the hard floor when the
//     env override is absent; 20 is the raised default for feature branches.
//
//  2. AST-hash gate: files whose content hash (SHA-256) is unchanged since
//     the last manifest stamp are skipped entirely (whitespace-only edits).
//
//  3. Signature-change incremental (#2170): entities whose Signature or key
//     Properties changed trigger a reverse-index look-up for inbound CALLS /
//     REFERENCES edges, which are re-resolved in the scoped pass rather than
//     falling back to full reindex.
//
//  4. Unresolved-relationship safety net: if the scoped resolver encounters
//     a relationship whose target is outside the changed-file set and cannot
//     be re-resolved from the existing graph, we fall back to full reindex
//     and log the reason.
//
// Manifest robustness (#2170):
//   - GC: manifest entries for files that no longer exist are removed before
//     any incremental pass so the deleted-file list stays clean.
//   - Corruption recovery: if LoadManifest returns a malformed manifest we log
//     and fall back to full reindex rather than panicking.
//
// Golden-file equivalence is verified in incremental_test.go: a full reindex
// and an incremental pass on the same input must produce byte-identical
// graph.fb output.
package extractors

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/cajasmota/grafel/internal/classifier"
	"github.com/cajasmota/grafel/internal/coverage"
	"github.com/cajasmota/grafel/internal/daemon/walk"
	"github.com/cajasmota/grafel/internal/engine"
	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/extractors/sresolver"
	"github.com/cajasmota/grafel/internal/gitmeta"
	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/graph/fbwriter"
	"github.com/cajasmota/grafel/internal/indexer/diff"
	"github.com/cajasmota/grafel/internal/install/detect"
	"github.com/cajasmota/grafel/internal/module"
	"github.com/cajasmota/grafel/internal/types"
)

// defaultIncrementalFiles is the raised default trigger limit for feature
// branches (S3 #2170). The S3 #2167 conservative value of 5 still acts as the
// minimum; 20 is the new default for non-main branches.
const defaultIncrementalFiles = 20

// mainBranchIncrementalFiles is the hot-path limit for the default (main)
// branch. Commits to main tend to be small focused changes; we allow up to 50
// files before falling back to a full reindex.
const mainBranchIncrementalFiles = 50

// effectiveLimit returns the trigger-limit for the given repoPath and optional
// ExtractorConfig.
//
// Priority (issue #2320):
//  1. cfg.IncrementalMaxFiles (when cfg is non-nil and > 0) — Config channel.
//  2. GRAFEL_INCREMENTAL_MAX_FILES env var (backward-compat fallback).
//  3. mainBranchIncrementalFiles when the active ref is the repo's default branch.
//  4. defaultIncrementalFiles (20) for feature branches.
func effectiveLimit(repoPath string, cfg *extractor.ExtractorConfig) int {
	if n := cfg.EffectiveIncrementalMaxFiles(); n > 0 {
		return n
	}
	if gitmeta.IsDefaultBranch(repoPath) {
		return mainBranchIncrementalFiles
	}
	return defaultIncrementalFiles
}

// IncrementalEnabled reports whether S3 incremental reindex is opt-in active.
// Reads GRAFEL_INCREMENTAL_REINDEX once per call — cheap, no caching needed
// at this level (the scheduler gate is the hot path).
//
// Issue #2320: callers that have an ExtractorConfig should call
// cfg.IsIncrementalEnabled() directly; this function is the backward-compat
// entry point for callers that have not yet been migrated.
func IncrementalEnabled() bool {
	var cfg *extractor.ExtractorConfig // nil → pure env-var path
	return cfg.IsIncrementalEnabled()
}

// Result is the outcome of a TryIncremental call.
type Result struct {
	// Done is true when the incremental patch completed successfully and the
	// caller should NOT fall through to a full reindex.
	Done bool

	// FallbackReason is non-empty when Done=false and the incremental path
	// explicitly decided to fall back (as opposed to encountering an error it
	// could not recover from).
	FallbackReason string

	// ChangedFiles is the number of files that were re-extracted.
	ChangedFiles int

	// Duration is the wall-clock time spent on the incremental pass.
	Duration time.Duration
}

// FileStamp records the per-file hash state used by the AST-hash gate.
type FileStamp struct {
	ContentHash string // hex SHA-256 of raw bytes
	Mtime       int64  // UnixNano — fast first-pass filter
}

// StampFile computes the FileStamp for the file at absPath.
func StampFile(absPath string) (FileStamp, error) {
	info, err := os.Lstat(absPath)
	if err != nil {
		return FileStamp{}, fmt.Errorf("stat %s: %w", absPath, err)
	}
	data, err := os.ReadFile(absPath)
	if err != nil {
		return FileStamp{}, fmt.Errorf("read %s: %w", absPath, err)
	}
	h := sha256.New()
	h.Write(data)
	return FileStamp{
		ContentHash: hex.EncodeToString(h.Sum(nil)),
		Mtime:       info.ModTime().UnixNano(),
	}, nil
}

// TryIncremental attempts a file-level incremental reindex for repoPath.
// stateDir is the on-disk directory where graph.fb and file-index.json live.
// logger may be nil (falls back to stderr).
// cfg is optional (nil-safe): when non-nil its IncrementalMaxFiles value
// overrides the env-var / gitmeta heuristic for the trigger limit (issue #2396).
//
// The call flow:
//  1. Load the diff manifest; detect changed files.
//  2. If > maxIncrementalFiles changed → fallback (full reindex).
//  3. AST-hash gate: skip files with identical SHA-256 content hash.
//  4. Load existing graph.Document from stateDir.
//  5. Remove entities (and their outbound relationships) sourced from changed files.
//  6. Re-extract each changed file via the registered language extractor.
//  7. Scoped resolver pass: re-resolve inbound cross-file relationships
//     targeting newly extracted entities.
//  8. Merge new entities/rels into the document, sort, write graph.fb atomically.
//  9. Update the diff manifest.
func TryIncremental(ctx context.Context, repoPath, stateDir string, logger *log.Logger, cfg *extractor.ExtractorConfig) Result {
	t0 := time.Now()
	if logger == nil {
		logger = log.New(os.Stderr, "incremental: ", log.LstdFlags)
	}

	// --- Step 1: load manifest + detect changed files ---
	// Manifest robustness (#2170): LoadManifest already returns an empty
	// manifest on corruption (json.Unmarshal error or version mismatch) and
	// logs internally. For an incremental pass a fresh manifest means no
	// known baseline → we cannot safely do incremental → fall back.
	manifest := diff.LoadManifest(stateDir)
	if manifest == nil {
		// Should never happen given diff.LoadManifest always returns non-nil,
		// but guard defensively.
		logger.Printf("incremental: manifest nil (corruption?) → fall back to full reindex")
		return fallback(t0, "manifest-nil")
	}

	// Walk the repo to get the full file list.
	absRepo, err := filepath.Abs(repoPath)
	if err != nil {
		return fallback(t0, "abs-repo: "+err.Error())
	}
	allFiles, walkErr := walkSourceFiles(absRepo)
	if walkErr != nil {
		return fallback(t0, "walk: "+walkErr.Error())
	}

	changedFiles, _ := diff.FilterWithGit(absRepo, allFiles, manifest)

	// Detect deleted files: files that were in the manifest but no longer
	// appear in the current walk (i.e. they have been deleted from disk).
	allFilesSet := make(map[string]bool, len(allFiles))
	for _, f := range allFiles {
		allFilesSet[f] = true
	}
	var deletedFiles []string
	for rel := range manifest.Files {
		if !allFilesSet[rel] {
			deletedFiles = append(deletedFiles, rel)
		}
	}

	// --- Manifest GC (#2170): eagerly remove entries for deleted files ---
	// Remove them from the manifest NOW so that if we fall back to full reindex
	// or succeed incrementally, the manifest saved at the end does not contain
	// stale entries for files that no longer exist. (The subsequent code also
	// calls `delete(manifest.Files, rel)` per deleted file during entity
	// pruning, but doing it here in a single pass is cleaner.)
	for _, rel := range deletedFiles {
		logger.Printf("incremental: manifest-gc removing deleted entry %s", rel)
		delete(manifest.Files, rel)
	}

	totalChanged := len(changedFiles) + len(deletedFiles)

	// --- Step 2: trigger limit (#2170 raised limits + main-branch hot-path) ---
	// Issue #2396: cfg is now threaded through from the caller so programmatic
	// config overrides the env-var / gitmeta path when non-nil.
	limit := effectiveLimit(absRepo, cfg)
	if totalChanged > limit {
		// Persist the manifest-GC deletions (applied above) BEFORE falling back,
		// so now-absent files — e.g. build artifacts newly excluded by the
		// gitignore-aware walk — don't recur as deletedFiles and re-trip this
		// fallback on every pass, looping the reindex forever (#5667). Without
		// this save the GC is in-memory only and discarded on fallback.
		_ = diff.SaveManifest(stateDir, absRepo, manifest)
		return fallback(t0, fmt.Sprintf("too-many-changed files=%d limit=%d",
			totalChanged, limit))
	}
	if totalChanged == 0 {
		// Nothing to do — manifest is already up-to-date.
		diff.UpdateManifest(absRepo, allFiles, manifest)
		_ = diff.SaveManifest(stateDir, absRepo, manifest)
		return Result{Done: true, Duration: time.Since(t0)}
	}

	// --- Step 3: AST-hash gate ---
	// Skip files where the content hash matches the last stamp (whitespace edits).
	var reallyChanged []string
	for _, rel := range changedFiles {
		abs := filepath.Join(absRepo, filepath.FromSlash(rel))
		stamp, sErr := StampFile(abs)
		if sErr != nil {
			reallyChanged = append(reallyChanged, rel) // be conservative: re-extract on error
			continue
		}
		prev, ok := manifest.Files[rel]
		if !ok || prev.SHA256 != stamp.ContentHash {
			reallyChanged = append(reallyChanged, rel)
		}
		// else: hash unchanged (whitespace-only) — skip silently
	}

	// Add deleted files to the reallyChanged set so their entities are pruned.
	// Deleted files always count as "really changed" — there's no AST hash to compare.
	// Note: manifest entries for deleted files were already removed during the
	// manifest-GC step above.
	reallyChanged = append(reallyChanged, deletedFiles...)

	if len(reallyChanged) == 0 {
		// All changes were whitespace-only (or only deletions already absent).
		logger.Printf("incremental: all %d changed file(s) had unchanged AST hash — skipping reindex", len(changedFiles))
		return Result{Done: true, Duration: time.Since(t0)}
	}

	// Re-check trigger limit after whitespace filtering.
	if len(reallyChanged) > limit {
		// Persist the manifest-GC deletions before falling back (see #5667).
		_ = diff.SaveManifest(stateDir, absRepo, manifest)
		return fallback(t0, fmt.Sprintf("too-many-changed after-hash-gate files=%d limit=%d",
			len(reallyChanged), limit))
	}

	// --- Step 4: load existing graph ---
	doc, loadErr := graph.LoadGraphFromDir(stateDir)
	if loadErr != nil {
		// No existing graph → can't do incremental.
		return fallback(t0, "load-graph: "+loadErr.Error())
	}

	// --- Step 5: remove old entities + outbound rels for changed files ---
	changedSet := make(map[string]bool, len(reallyChanged))
	for _, f := range reallyChanged {
		changedSet[f] = true
	}

	// Capture old entity property hashes for signature-change detection (#2170).
	// We record (qualifiedName → propertiesHash) before removal so that after
	// re-extraction we can detect entities whose Signature or Properties changed.
	oldEntityPropHash := make(map[string]string) // qualifiedName → hash
	for _, e := range doc.Entities {
		if changedSet[e.SourceFile] {
			oldEntityPropHash[entityPropKey(e)] = entityPropertiesHash(e)
		}
	}

	// Collect entity IDs sourced from changed files so we can also prune
	// their outbound relationships. Capture each removed entity's module key
	// too (#5309 layer 2): a fully-deleted file leaves no replacement entity in
	// newEntities, so its module would otherwise be invisible to the
	// affected-module set and its stale CONTAINS edges would survive.
	removedEntityIDs := make(map[string]bool)
	removedModuleKeys := make(map[module.ModuleKey]struct{})
	filteredEntities := doc.Entities[:0]
	for _, e := range doc.Entities {
		if changedSet[e.SourceFile] {
			removedEntityIDs[e.ID] = true
			if e.Kind != module.KindModule {
				removedModuleKeys[entityModuleKey(&e, doc.Repo)] = struct{}{}
			}
		} else {
			filteredEntities = append(filteredEntities, e)
		}
	}
	doc.Entities = filteredEntities

	// Remove outbound relationships from removed entities. Inbound edges
	// from surviving files to removed entities are handled below, after
	// re-extraction reveals which removed-entity IDs are actually re-emitted
	// (entity IDs are deterministic over kind/name/source_file, so re-extracting
	// a file usually re-creates entities with the same ID — keeping inbound
	// cross-file edges valid for free).
	// Track removed relationships so the incremental flow pass (#5309 layer 3)
	// can tell whether the blast radius touched a flow-input edge.
	var removedRels []graph.Relationship
	filteredRels := doc.Relationships[:0]
	for _, r := range doc.Relationships {
		if !removedEntityIDs[r.FromID] {
			filteredRels = append(filteredRels, r)
		} else {
			removedRels = append(removedRels, r)
		}
	}
	doc.Relationships = filteredRels

	// --- Step 6: re-extract each changed file ---
	cls, clsErr := classifier.New("", nil)
	if clsErr != nil {
		return fallback(t0, "classifier: "+clsErr.Error())
	}

	var newEntities []graph.Entity
	var newRels []graph.Relationship

	for _, rel := range reallyChanged {
		abs := filepath.Join(absRepo, filepath.FromSlash(rel))
		content, readErr := os.ReadFile(abs)
		if readErr != nil {
			// File deleted → nothing to extract; entities were already removed.
			logger.Printf("incremental: %s deleted or unreadable — entities removed", rel)
			continue
		}

		// Classify to get language.
		cr := cls.ClassifyWithSize(ctx, rel, int64(len(content)))
		if cr.Skip || cr.Language == "" {
			logger.Printf("incremental: %s — classifier returned no language, skipping", rel)
			continue
		}

		ext, ok := Get(cr.Language)
		if !ok {
			logger.Printf("incremental: no extractor for language=%s file=%s", cr.Language, rel)
			continue
		}

		records, extErr := ext.Extract(ctx, extractor.FileInput{
			Path:     rel,
			Content:  content,
			Language: cr.Language,
			TSTree:   nil, // re-parse inline
			RepoRoot: absRepo,
		})
		if extErr != nil {
			logger.Printf("incremental: extract %s: %v", rel, extErr)
			// Non-fatal: use partial results.
		}

		// Convert types.EntityRecord → graph.Entity (same logic as buildDocument).
		for _, rec := range records {
			e := entityRecordToGraphEntity(rec, doc.Repo)
			newEntities = append(newEntities, e)
			for _, relRec := range rec.Relationships {
				newRels = append(newRels, relRecordToGraphRel(relRec))
			}
		}
	}

	// --- Step 6a: inbound-dangling prune (#2719) ---
	// Now that we know which entity IDs are coming back via re-extraction,
	// drop inbound edges to removed entities whose ID is NOT among the
	// re-extracted set. Without this pass, deleting an entity (or renaming it
	// such that its EntityID changes) leaves stale inbound edges pointing at
	// nothing — invisible orphans until a full reindex sweeps them up.
	// Entities re-extracted with the same ID keep their inbound edges intact
	// (entity IDs are deterministic over kind/name/source_file), which
	// preserves the carefully-resolved cross-file CALLS / REFERENCES edges
	// that other files asserted into the previous graph.
	reEmittedIDs := make(map[string]bool, len(newEntities))
	for _, e := range newEntities {
		reEmittedIDs[e.ID] = true
	}
	prunedInbound := doc.Relationships[:0]
	for _, r := range doc.Relationships {
		if removedEntityIDs[r.ToID] && !reEmittedIDs[r.ToID] {
			removedRels = append(removedRels, r) // truly removed → drop the dangling inbound edge
			continue
		}
		prunedInbound = append(prunedInbound, r)
	}
	doc.Relationships = prunedInbound

	// --- Step 6b: signature-change detection (#2170) ---
	// For each newly extracted entity, compare its properties hash against the
	// old hash. Entities with changed signatures (arity, parameter types,
	// exported-ness) are collected; we will pass them to the scoped resolver so
	// it can re-resolve inbound CALLS/REFERENCES edges rather than triggering
	// the safety-net full-reindex fallback.
	var signatureChangedIDs []string
	for _, e := range newEntities {
		key := entityPropKey(e)
		oldHash, existed := oldEntityPropHash[key]
		if existed && oldHash != entityPropertiesHash(e) {
			signatureChangedIDs = append(signatureChangedIDs, e.ID)
			logger.Printf("incremental: signature-change detected entity=%s file=%s", e.QualifiedName, e.SourceFile)
		}
	}

	// --- Step 7: scoped resolver pass ---
	// Re-resolve inbound cross-file relationships targeting the newly
	// extracted entities. Uses a lightweight name-index over the full
	// (surviving) entity set.
	//
	// When signature changes are detected, pass them to the resolver so it can
	// re-resolve inbound CALLS/REFERENCES edges for those entities rather than
	// triggering the safety-net fallback (#2170).
	scopedResult := sresolver.ResolveScoped(
		newEntities,
		doc.Entities, // existing surviving entities
		newRels,
		doc.Relationships,
		logger,
		sresolver.WithSignatureChangedIDs(signatureChangedIDs),
	)
	if scopedResult.FallbackRequired {
		logger.Printf("incremental: fallback reason=unresolved-rel target=%s", scopedResult.UnresolvedTarget)
		return fallback(t0, "unresolved-rel target="+scopedResult.UnresolvedTarget)
	}
	newRels = scopedResult.NewRelationships

	// --- Step 7a: stamp Properties["module"] on new entities (#5309 layer 2) ---
	// The full-rebuild path stamps every sourced entity with a deterministic
	// module label (cmd/grafel/index.go buildDocument) BEFORE the module-agg
	// pass. The incremental path's entityRecordToGraphEntity carries only the
	// extractor-supplied properties, so freshly extracted entities arrive with
	// no "module" key. Stamp them here using the SAME label rule the full path
	// uses — single-module label for a plain repo, else the path-rollup over the
	// package-boundary markers — so the module layer rebuilt below is
	// byte-equivalent to a full rebuild. (Surviving entities keep the label they
	// were stamped with on the previous build.)
	stampModuleOnEntities(newEntities, doc, absRepo, allFiles)

	// --- Step 8: merge + sort + write ---
	doc.Entities = append(doc.Entities, newEntities...)
	doc.Relationships = append(doc.Relationships, newRels...)

	// --- Step 8·flows: incremental per-repo flow passes (#5309 layer 3) ───────
	// The full path runs RunProcessFlow (Pass 7) + RunEventFlow (Pass 7.5) over
	// the finalized graph, BEFORE module-aggregation (Pass 8). The incremental
	// path previously skipped both, carrying the prior build's Process /
	// EventFlow entities + their ENTRY_POINT_OF / STEP_IN_PROCESS /
	// SEED_OF_EVENT_FLOW / STEP_IN_EVENT_FLOW edges forward — which a code change
	// can staleify. engine.RunFlowsIncremental is blast-radius-scoped: when the
	// change cannot touch a flow input (CALLS / FETCHES / HTTP-boundary / pub-sub
	// edges or any flow-relevant entity — e.g. docs/comment-only changes) the
	// prior flows are already byte-equivalent to a full rebuild and are kept
	// verbatim; otherwise the stale flows are stripped and both walkers re-run
	// over the finalized graph, reproducing exactly what a full rebuild emits.
	//
	// Run BEFORE module-aggregation so the ordering matches the full path: a full
	// rebuild's Pass 8 sees the Process / EventFlow entities Pass 7 emitted and
	// folds them into the module layer (a CONTAINS edge from the `_external`
	// Module node for each). Capture the flow-emitted entities/edges and feed them
	// into the affected-module set so that module layer is re-derived too.
	flowsRecomputed, flowEntities, flowRels := engine.RunFlowsIncremental(doc, newEntities, removedEntityIDs, newRels, removedRels)

	// --- Step 8a: incremental module-aggregation (#5309 layer 2) ─────────────
	// The full path runs module.Aggregate (CONTAINS / DEPENDS_ON + Module nodes)
	// as Pass 8 over the finalized graph. The incremental path carries the prior
	// build's module layer forward in doc, which a file change leaves stale:
	// CONTAINS edges to removed entities, Module nodes whose members vanished,
	// DEPENDS_ON weights that moved. Re-run the aggregation scoped to the modules
	// whose membership or cross-module dependencies changed — every other
	// module's nodes/edges are preserved verbatim. The result is byte-equivalent
	// to a full rebuild's module layer without re-aggregating the whole graph.
	//
	// The freshly (re)emitted flow entities/edges (#5309 layer 3) join the
	// affected set so their `_external` Module node + CONTAINS edges are
	// (re-)derived exactly as a full rebuild's Pass 8 would, and so a flow strip
	// that removed the last member of a module triggers that module's re-derive.
	aggNewEnts := append(append([]graph.Entity(nil), newEntities...), flowEntities...)
	aggNewRels := append(append([]graph.Relationship(nil), newRels...), flowRels...)
	affectedModules := affectedModuleSet(doc, removedModuleKeys, aggNewEnts, aggNewRels)
	module.AggregateIncremental(doc, affectedModules)

	// --- Step 8a.9: lib-boundary re-stamp (#5309 layer 3) ────────────────────
	// The full path's Pass 8.9 (engine.ApplyLibBoundary) classifies every
	// DEPENDS_ON edge first_party/third_party from the locality/kind props the
	// extractors already attached. It runs AFTER module-aggregation (the agg
	// pass emits fresh Module→Module DEPENDS_ON edges carrying only a `weight`
	// prop). The incremental path's module-agg likewise emits unstamped
	// DEPENDS_ON edges, so the `boundary` property must be (re)applied here or
	// the freshly (re)emitted edges diverge from a full rebuild — surfaced once
	// the flow Process entities (Pass 7) introduce new `_external`→first-party
	// DEPENDS_ON pairs. The pass is deterministic, idempotent and bounded by the
	// DEPENDS_ON edge count (a pure function of the now-finalized edge set).
	engine.ApplyLibBoundary(doc)

	// --- Step 8a': structural coupling re-stamp (#5309 layer 2) ──────────────
	// The full path's Pass 8.6 (engine.ApplyStructuralCoupling) annotates each
	// Module node with afferent/efferent coupling + instability derived from the
	// DEPENDS_ON edges module-agg just (re)emitted. It is bounded by the module
	// count (not the entity count) and is a pure function of the module graph, so
	// re-running it over the corrected DEPENDS_ON set lands the same ca/ce/
	// instability/coupling_computed properties a full rebuild would — without
	// which freshly re-emitted Module nodes would carry no coupling props and
	// survivors could carry stale ones.
	engine.ApplyStructuralCoupling(doc)

	// --- Step 8b: static test-reachability re-stamp (#5309 layer 2) ──────────
	// coverage.Enrich's reachability sub-pass stamps test_reachable /
	// reaching_tests / reach_depth onto production entities from the in-graph
	// TESTS+CALLS edges. It is a deterministic function of the (now finalized)
	// graph with no external dependency, so re-running it after the merge lands
	// the same property set a full rebuild would. New entities get stamped and
	// survivors are refreshed in case a changed edge moved their reachability.
	coverage.Enrich(doc, absRepo, coverage.Config{})

	// #2706 — belt-and-suspenders prune of Django migration entities.
	// The incremental path bypasses the per-extractor prune gates only
	// indirectly (it calls extractor.Extract which respects them), but the
	// merged `doc.Entities` slice includes survivors carried forward from
	// the previous on-disk graph. If a previous build slipped any migration
	// entities through (e.g. before the per-extractor prune existed, or via
	// a new emission path) they would survive here forever. The central
	// sweep keeps the incremental and full-rebuild paths in lockstep.
	if ePruned, rPruned := PruneMigrationEntities(doc); ePruned > 0 {
		logger.Printf("incremental: migration-prune dropped %d entities + %d relationships", ePruned, rPruned)
	}

	doc.Stats.Entities = len(doc.Entities)
	doc.Stats.Relationships = len(doc.Relationships)
	doc.GeneratedAt = time.Now().UTC()

	sortGraphDocumentForEmission(doc)

	fbPath := filepath.Join(stateDir, "graph.fb")
	if writeErr := fbwriter.WriteAtomic(fbPath, doc); writeErr != nil {
		return fallback(t0, "write-graph-fb: "+writeErr.Error())
	}

	// #5442 — refresh the graph-stats.json sidecar so the dashboard group
	// overview and `grafel status` report this repo's real entity count and a
	// real last-indexed time when the group is cold (not loaded in memory).
	// The incremental path does not run Pass-4 graph-algo, so community /
	// modularity / god-node fields are omitted; the counts + timestamp are
	// what those surfaces read. Best-effort: a sidecar write failure never
	// fails the reindex (graph.fb is already written, and the fbreader-header
	// fallback still recovers the counts on a sidecar miss).
	side := &graph.GraphStatsSidecar{
		Version:            1,
		ComputedAt:         doc.GeneratedAt,
		TotalFiles:         doc.Stats.Files,
		TotalEntities:      doc.Stats.Entities,
		TotalRelationships: doc.Stats.Relationships,
	}
	if serr := graph.WriteSidecar(fbPath, side, false); serr != nil {
		logger.Printf("incremental: sidecar write failed: %v (non-fatal)", serr)
	}

	// --- Step 9: update manifest ---
	diff.UpdateManifest(absRepo, allFiles, manifest)
	if saveErr := diff.SaveManifest(stateDir, absRepo, manifest); saveErr != nil {
		logger.Printf("incremental: save manifest: %v (non-fatal)", saveErr)
	}

	dur := time.Since(t0)
	logger.Printf("incremental: done changed=%d entities=%d rels=%d flows_recomputed=%t took=%s",
		len(reallyChanged), len(newEntities), len(newRels), flowsRecomputed, dur.Truncate(time.Millisecond))

	return Result{
		Done:         true,
		ChangedFiles: len(reallyChanged),
		Duration:     dur,
	}
}

// stampModuleOnEntities stamps Properties["module"] on each entity in ents that
// does not already carry one, matching the full-rebuild path's labeling
// (cmd/grafel/index.go buildDocument):
//
//   - PLAIN repo → one label for every sourced entity. The full path uses
//     repoSlug-or-repoTag; we recover that label from the existing graph (in a
//     plain repo every sourced entity already shares one "module" value).
//   - MONOREPO  → the deterministic path rollup over the package-boundary
//     markers, via module.Derive.
//
// Sourceless synthetic entities are stamped "_external", mirroring the full
// path's post-assembly _external sweep.
func stampModuleOnEntities(ents []graph.Entity, doc *graph.Document, absRepo string, allFiles []string) {
	if len(ents) == 0 {
		return
	}

	// Determine the plain-repo single label, matching the full-rebuild path
	// (cmd/grafel/index.go Run): a PLAIN (non-monorepo) repo forces every sourced
	// entity into ONE module label == repoSlug-or-repoTag (issue #1628); a TRUE
	// monorepo (>1 workspace package) uses the per-package path rollup.
	//
	// The full path's label is repoSlug (falling back to repoTag); the
	// incremental path only has doc.Repo (the repoTag), which equals the full
	// path's label whenever repoSlug is empty or equal to repoTag — the normal
	// case. We cross-check against the existing graph: in a plain repo every
	// sourced survivor already shares one "module" value, which is the
	// authoritative label the previous full build stamped. Prefer that recovered
	// label (exact, even when repoSlug != repoTag); fall back to doc.Repo.
	plainLabel := ""
	if mono, derr := detect.DetectMonorepo(absRepo); derr != nil || mono.Kind == detect.KindNone || len(mono.Packages) <= 1 {
		// Plain repo. Recover the exact label the previous build used, if any
		// sourced survivor carries one; otherwise use the repo tag.
		single, multiple := "", false
		for k := range doc.Entities {
			e := &doc.Entities[k]
			if e.Kind == module.KindModule || e.SourceFile == "" || e.Properties == nil {
				continue
			}
			m, ok := e.Properties["module"]
			if !ok || m == "" || m == "_external" {
				continue
			}
			if single == "" {
				single = m
			} else if single != m {
				multiple = true
				break
			}
		}
		switch {
		case single != "" && !multiple:
			plainLabel = single
		case doc.Repo != "":
			plainLabel = doc.Repo
		}
	}

	// Markers for the monorepo path. BuildMarkerSet expects repo-relative
	// forward-slash paths — exactly what walkSourceFiles produced into allFiles.
	markers := module.BuildMarkerSet(allFiles)

	for i := range ents {
		e := &ents[i]
		if e.Properties != nil {
			if v, ok := e.Properties["module"]; ok && v != "" {
				continue // extractor-supplied label preserved
			}
		}
		var label string
		switch {
		case e.SourceFile == "":
			label = "_external"
		case plainLabel != "":
			label = plainLabel
		default:
			label = module.Derive(e.SourceFile, markers)
		}
		if e.Properties == nil {
			e.Properties = map[string]string{}
		}
		e.Properties["module"] = label
	}
}

// affectedModuleSet computes the blast radius of a reindex in module-key terms:
// the modules whose membership or cross-module dependencies could have changed.
// This is the union of:
//
//   - the modules of every re-extracted (new) entity — their membership and the
//     cross-module edges they originate changed;
//   - the modules of every removed entity — CONTAINS/membership shrank and the
//     edges they originated vanished;
//   - both endpoint modules of every newly added relationship — a new
//     cross-module edge moves a DEPENDS_ON weight.
//
// Returned as the ModuleKey set AggregateIncremental scopes its strip+rebuild
// to. doc must already hold the merged (post-Step-8) entity set so endpoint
// module lookups resolve.
func affectedModuleSet(doc *graph.Document, removedModuleKeys map[module.ModuleKey]struct{}, newEnts []graph.Entity, newRels []graph.Relationship) map[module.ModuleKey]struct{} {
	// id → module key over the merged graph (used to resolve relationship
	// endpoints, including survivors).
	idMod := make(map[string]module.ModuleKey, len(doc.Entities))
	for k := range doc.Entities {
		e := &doc.Entities[k]
		if e.Kind == module.KindModule {
			continue
		}
		idMod[e.ID] = entityModuleKey(e, doc.Repo)
	}

	affected := make(map[module.ModuleKey]struct{})
	add := func(mk module.ModuleKey) { affected[mk] = struct{}{} }

	for i := range newEnts {
		add(entityModuleKey(&newEnts[i], doc.Repo))
	}
	// Removed entities (incl. fully-deleted files with no replacement): their
	// module's membership shrank, so its CONTAINS / DEPENDS_ON must be re-derived.
	for mk := range removedModuleKeys {
		add(mk)
	}

	for i := range newRels {
		r := &newRels[i]
		if mk, ok := idMod[r.FromID]; ok {
			add(mk)
		}
		if mk, ok := idMod[r.ToID]; ok {
			add(mk)
		}
	}
	return affected
}

// entityModuleKey mirrors module.AggregateIncremental's per-entity key
// derivation: Properties["module"] (default "_external") + Properties["repo"]
// (default docRepo).
func entityModuleKey(e *graph.Entity, docRepo string) module.ModuleKey {
	mod := "_external"
	repo := docRepo
	if e.Properties != nil {
		if v, ok := e.Properties["module"]; ok && v != "" {
			mod = v
		}
		if v, ok := e.Properties["repo"]; ok && v != "" {
			repo = v
		}
	}
	return module.NewModuleKey(repo, mod)
}

// fallback returns a Result with Done=false and the given reason.
func fallback(t0 time.Time, reason string) Result {
	return Result{
		Done:           false,
		FallbackReason: reason,
		Duration:       time.Since(t0),
	}
}

// walkSourceFiles returns repo-relative forward-slash paths for all source
// files under absRepo, using the SAME gitignore/.grafelignore-aware walker the
// full indexer uses (walk.WalkRepo). This is deliberate: the incremental
// change-detector and the full index must agree on which files exist.
//
// Previously this was a hand-rolled filepath.WalkDir with only a small
// hardcoded directory denylist and NO .gitignore handling. The full index
// (walk.WalkRepo) excluded gitignored build-artifact directories (e.g.
// ios/Pods, android/**/.cxx), but this walker did not — so those gitignored
// files entered the change manifest and, because build tooling constantly
// regenerates/deletes them, were counted as "changed" on every poll. With the
// HEAD static, that perpetually tripped the too-many-changed full-reindex
// fallback (incremental.go ~line 233), pinning daemon CPU in an endless
// reindex loop (#5665). Delegating to walk.WalkRepo makes both paths honor the
// same ignore rules, so gitignored churn can no longer drive reindexing.
func walkSourceFiles(absRepo string) ([]string, error) {
	// Mirror the full indexer: probe sparse-checkout state so a partial
	// working tree is walked consistently. ProbeRepo is best-effort and
	// returns a zero-value (no sparse filtering) when the repo isn't sparse.
	sparse := gitmeta.ProbeRepo(absRepo)
	files, _, err := walk.WalkRepo(absRepo, &walk.Options{Sparse: &sparse})
	return files, err
}

// entityRecordToGraphEntity converts a types.EntityRecord produced by an
// extractor into a graph.Entity. Mirrors the buildDocument pass in cmd/grafel/index.go
// without importing that package (avoids a cmd → internal cycle).
func entityRecordToGraphEntity(r types.EntityRecord, repoTag string) graph.Entity {
	id := r.ID
	if id == "" {
		id = graph.EntityID(repoTag, r.Kind, r.Name, r.SourceFile)
	}
	return graph.Entity{
		ID:            id,
		Name:          r.Name,
		QualifiedName: r.QualifiedName,
		Kind:          r.Kind,
		Subtype:       r.Subtype,
		SourceFile:    r.SourceFile,
		StartLine:     r.StartLine,
		EndLine:       r.EndLine,
		Language:      r.Language,
		Signature:     r.Signature,
		Tags:          r.Tags,
		Properties:    r.Properties,
		Confidence:    r.Confidence, // Phase 1C (#2769) — propagates extractor stamp.
	}
}

// relRecordToGraphRel converts an embedded types.RelationshipRecord to a
// graph.Relationship.
func relRecordToGraphRel(r types.RelationshipRecord) graph.Relationship {
	id := graph.RelationshipID(r.FromID, r.ToID, r.Kind)
	return graph.Relationship{
		ID:         id,
		FromID:     r.FromID,
		ToID:       r.ToID,
		Kind:       r.Kind,
		Properties: r.Properties,
		Confidence: r.Confidence, // Phase 1C (#2769).
	}
}

// sortGraphDocumentForEmission sorts entities and relationships in the same
// canonical order used by cmd/grafel/index.go (sortDocumentForEmission).
// Kept here to avoid a cmd → internal import cycle.
func sortGraphDocumentForEmission(doc *graph.Document) {
	sort.SliceStable(doc.Entities, func(a, b int) bool {
		ra, rb := &doc.Entities[a], &doc.Entities[b]
		if ra.SourceFile != rb.SourceFile {
			return ra.SourceFile < rb.SourceFile
		}
		if ra.Kind != rb.Kind {
			return ra.Kind < rb.Kind
		}
		if ra.QualifiedName != rb.QualifiedName {
			return ra.QualifiedName < rb.QualifiedName
		}
		if ra.Name != rb.Name {
			return ra.Name < rb.Name
		}
		if ra.StartLine != rb.StartLine {
			return ra.StartLine < rb.StartLine
		}
		return ra.ID < rb.ID
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
}

// entityPropKey returns a stable string key for an entity used in the
// signature-change map: qualifiedName is preferred, falling back to name.
func entityPropKey(e graph.Entity) string {
	if e.QualifiedName != "" {
		return e.QualifiedName
	}
	return e.Name
}

// entityPropertiesHash computes a short hash of the fields that constitute an
// entity's "signature" for the purpose of signature-change detection (#2170).
// Fields hashed: Signature, Kind, Subtype, and the sorted Properties map.
// The result is a 16-char hex string.
func entityPropertiesHash(e graph.Entity) string {
	h := sha256.New()
	h.Write([]byte(e.Signature))
	h.Write([]byte{0})
	h.Write([]byte(e.Kind))
	h.Write([]byte{0})
	h.Write([]byte(e.Subtype))
	h.Write([]byte{0})
	// Sort property keys for stable hashing.
	keys := make([]string, 0, len(e.Properties))
	for k := range e.Properties {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		h.Write([]byte(k))
		h.Write([]byte("="))
		h.Write([]byte(e.Properties[k]))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}
