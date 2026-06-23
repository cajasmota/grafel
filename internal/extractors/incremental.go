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
	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/extractors/sresolver"
	"github.com/cajasmota/grafel/internal/gitmeta"
	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/graph/fbwriter"
	"github.com/cajasmota/grafel/internal/indexer/diff"
	"github.com/cajasmota/grafel/internal/types"
	sitter "github.com/smacker/go-tree-sitter"
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
	// their outbound relationships.
	removedEntityIDs := make(map[string]bool)
	filteredEntities := doc.Entities[:0]
	for _, e := range doc.Entities {
		if changedSet[e.SourceFile] {
			removedEntityIDs[e.ID] = true
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
	filteredRels := doc.Relationships[:0]
	for _, r := range doc.Relationships {
		if !removedEntityIDs[r.FromID] {
			filteredRels = append(filteredRels, r)
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
			Tree:     nil, // re-parse inline
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
			continue // truly removed → drop the dangling inbound edge
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

	// --- Step 8: merge + sort + write ---
	doc.Entities = append(doc.Entities, newEntities...)
	doc.Relationships = append(doc.Relationships, newRels...)

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
	logger.Printf("incremental: done changed=%d entities=%d rels=%d took=%s",
		len(reallyChanged), len(newEntities), len(newRels), dur.Truncate(time.Millisecond))

	return Result{
		Done:         true,
		ChangedFiles: len(reallyChanged),
		Duration:     dur,
	}
}

// fallback returns a Result with Done=false and the given reason.
func fallback(t0 time.Time, reason string) Result {
	return Result{
		Done:           false,
		FallbackReason: reason,
		Duration:       time.Since(t0),
	}
}

// walkSourceFiles returns repo-relative forward-slash paths for all
// source files under absRepo, excluding .git and common build artifacts.
// This is a thin wrapper so the incremental path doesn't import internal/walk
// directly (which would introduce a heavier dependency).
func walkSourceFiles(absRepo string) ([]string, error) {
	var paths []string
	err := filepath.WalkDir(absRepo, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // skip unreadable entries
		}
		name := d.Name()
		if d.IsDir() {
			switch name {
			case ".git", "node_modules", "vendor", ".grafel",
				"dist", "build", "__pycache__", ".mypy_cache":
				return filepath.SkipDir
			}
			return nil
		}
		rel, relErr := filepath.Rel(absRepo, path)
		if relErr != nil {
			return nil
		}
		// Forward-slash always (diff manifest uses forward-slash keys).
		rel = filepath.ToSlash(rel)
		paths = append(paths, rel)
		return nil
	})
	return paths, err
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

// parseTree is kept as a compile-time reference so the sitter import is used.
// The actual tree-sitter parse happens inside each language extractor; we do
// not re-parse here (the extractor does it if file.Tree is nil).
var _ = (*sitter.Tree)(nil)
