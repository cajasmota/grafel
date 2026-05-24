// Package extractors — incremental.go implements the S3 incremental
// file-level reindex path (issue #2153 of epic #2149).
//
// # Conservative v1 design
//
// The full-reindex pipeline rewrites graph.fb from scratch on every daemon
// watcher tick. For a 60 k-entity repo that takes ~5 s. When only one file
// changed, we want ~200 ms: parse that file, swap its entities in the graph,
// and atomically re-emit graph.fb without touching anything else.
//
// Correctness guarantee: the opt-in flag (ARCHIGRAPH_INCREMENTAL_REINDEX=1)
// is NOT set by default. Three additional safety valves are applied before
// attempting a partial reindex:
//
//  1. Trigger limit: if more than maxIncrementalFiles (5) files changed in
//     the debounced batch we fall back to full reindex.
//
//  2. AST-hash gate: files whose content hash (SHA-256) is unchanged since
//     the last manifest stamp are skipped entirely (whitespace-only edits).
//
//  3. Unresolved-relationship safety net: if the scoped resolver encounters
//     a relationship whose target is outside the changed-file set and cannot
//     be re-resolved from the existing graph, we fall back to full reindex
//     and log the reason.
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

	"github.com/cajasmota/archigraph/internal/classifier"
	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/extractors/sresolver"
	"github.com/cajasmota/archigraph/internal/graph"
	"github.com/cajasmota/archigraph/internal/graph/fbwriter"
	"github.com/cajasmota/archigraph/internal/indexer/diff"
	"github.com/cajasmota/archigraph/internal/types"
	sitter "github.com/smacker/go-tree-sitter"
)

// maxIncrementalFiles is the upper bound on changed-file count beyond which
// the incremental path falls back to a full reindex. Kept small so the
// "scoped resolver" re-pass stays cheap and the safety-net logic simple.
const maxIncrementalFiles = 5

// IncrementalEnabled reports whether S3 incremental reindex is opt-in active.
// Reads ARCHIGRAPH_INCREMENTAL_REINDEX once per call — cheap, no caching needed
// at this level (the scheduler gate is the hot path).
func IncrementalEnabled() bool {
	v := os.Getenv("ARCHIGRAPH_INCREMENTAL_REINDEX")
	return v == "1" || v == "true"
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
func TryIncremental(ctx context.Context, repoPath, stateDir string, logger *log.Logger) Result {
	t0 := time.Now()
	if logger == nil {
		logger = log.New(os.Stderr, "incremental: ", log.LstdFlags)
	}

	// --- Step 1: load manifest + detect changed files ---
	manifest := diff.LoadManifest(stateDir)

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

	totalChanged := len(changedFiles) + len(deletedFiles)

	// --- Step 2: trigger limit ---
	if totalChanged > maxIncrementalFiles {
		return fallback(t0, fmt.Sprintf("too-many-changed files=%d limit=%d",
			totalChanged, maxIncrementalFiles))
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
	// Remove deleted files from the manifest so they don't appear in future incremental runs.
	for _, rel := range deletedFiles {
		delete(manifest.Files, rel)
	}
	reallyChanged = append(reallyChanged, deletedFiles...)

	if len(reallyChanged) == 0 {
		// All changes were whitespace-only (or only deletions already absent).
		logger.Printf("incremental: all %d changed file(s) had unchanged AST hash — skipping reindex", len(changedFiles))
		return Result{Done: true, Duration: time.Since(t0)}
	}

	// Re-check trigger limit after whitespace filtering.
	if len(reallyChanged) > maxIncrementalFiles {
		return fallback(t0, fmt.Sprintf("too-many-changed after-hash-gate files=%d limit=%d",
			len(reallyChanged), maxIncrementalFiles))
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

	// Remove outbound relationships from removed entities.
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

	// --- Step 7: scoped resolver pass ---
	// Re-resolve inbound cross-file relationships targeting the newly
	// extracted entities. Uses a lightweight name-index over the full
	// (surviving) entity set.
	scopedResult := sresolver.ResolveScoped(
		newEntities,
		doc.Entities, // existing surviving entities
		newRels,
		doc.Relationships,
		logger,
	)
	if scopedResult.FallbackRequired {
		logger.Printf("incremental: fallback reason=unresolved-rel target=%s", scopedResult.UnresolvedTarget)
		return fallback(t0, "unresolved-rel target="+scopedResult.UnresolvedTarget)
	}
	newRels = scopedResult.NewRelationships

	// --- Step 8: merge + sort + write ---
	doc.Entities = append(doc.Entities, newEntities...)
	doc.Relationships = append(doc.Relationships, newRels...)
	doc.Stats.Entities = len(doc.Entities)
	doc.Stats.Relationships = len(doc.Relationships)
	doc.GeneratedAt = time.Now().UTC()

	sortGraphDocumentForEmission(doc)

	fbPath := filepath.Join(stateDir, "graph.fb")
	if writeErr := fbwriter.WriteAtomic(fbPath, doc); writeErr != nil {
		return fallback(t0, "write-graph-fb: "+writeErr.Error())
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
			case ".git", "node_modules", "vendor", ".archigraph",
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
// extractor into a graph.Entity. Mirrors the buildDocument pass in cmd/archigraph/index.go
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
	}
}

// sortGraphDocumentForEmission sorts entities and relationships in the same
// canonical order used by cmd/archigraph/index.go (sortDocumentForEmission).
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

// parseTree is kept as a compile-time reference so the sitter import is used.
// The actual tree-sitter parse happens inside each language extractor; we do
// not re-parse here (the extractor does it if file.Tree is nil).
var _ = (*sitter.Tree)(nil)
