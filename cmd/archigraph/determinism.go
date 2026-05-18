package main

import (
	"os"
	"sort"
	"strconv"
	"time"

	"github.com/cajasmota/archigraph/internal/graph"
	"github.com/cajasmota/archigraph/internal/types"
)

// Issue #481 — determinism helpers. The indexer fans Pass 1 / 2.5 / 3 out
// across a worker pool; the merged slices therefore accumulate in goroutine-
// scheduling order, which is non-deterministic. Downstream passes
// (resolve.BuildIndex's first-writer-wins, the seenEntity/seenRel dedup in
// buildDocument, and graph algorithms that consume slice order) inherit the
// non-determinism and graph.json comes out byte-different across runs of the
// SAME repo. Sorting at every fan-in boundary by canonical fields makes the
// whole pipeline reproducible without changing the resolved set's semantics
// — every comparator orders on identity, not on data that downstream stages
// might rewrite.

// sortClassifiedFiles orders the Pass 1 classifyAndRead output by repo-
// relative path. The path is unique per file in any given walk.
func sortClassifiedFiles(cs []classifiedFile) {
	sort.Slice(cs, func(i, j int) bool { return cs[i].relPath < cs[j].relPath })
}

// sortEntityRecords orders an EntityRecord slice by the tuple
// (Kind, Name, SourceFile, StartLine, EndLine, Signature). This is the same
// tuple BuildIndex uses to disambiguate, so first-writer-wins is now
// deterministic. We use sort.SliceStable so records that compare equal keep
// their relative arrival order (defence in depth — should be rare in
// practice).
func sortEntityRecords(rs []types.EntityRecord) {
	sort.SliceStable(rs, func(i, j int) bool {
		a, b := &rs[i], &rs[j]
		if a.Kind != b.Kind {
			return a.Kind < b.Kind
		}
		if a.Name != b.Name {
			return a.Name < b.Name
		}
		if a.SourceFile != b.SourceFile {
			return a.SourceFile < b.SourceFile
		}
		if a.StartLine != b.StartLine {
			return a.StartLine < b.StartLine
		}
		if a.EndLine != b.EndLine {
			return a.EndLine < b.EndLine
		}
		return a.Signature < b.Signature
	})
}

// sortRelationshipRecords orders Pass 2.5 standalone relationships by
// (FromID, ToID, Kind). Properties are intentionally ignored — two edges
// with the same triple but different properties would still dedupe to one
// edge downstream (graph.RelationshipID hashes only the triple).
func sortRelationshipRecords(rs []types.RelationshipRecord) {
	sort.SliceStable(rs, func(i, j int) bool {
		a, b := &rs[i], &rs[j]
		if a.FromID != b.FromID {
			return a.FromID < b.FromID
		}
		if a.ToID != b.ToID {
			return a.ToID < b.ToID
		}
		return a.Kind < b.Kind
	})
}

// sortDocumentForEmission is the final, post-everything sort applied
// immediately before graph.WriteAtomic. Even with every fan-in already
// sorted, intermediate passes that mutate slices in place (external
// synthesis appends, Pass 4 reads via maps) deserve a defensive belt-and-
// braces sort. Sorting on the canonical graph IDs keeps diffs minimal.
func sortDocumentForEmission(doc *graph.Document) {
	sort.SliceStable(doc.Entities, func(i, j int) bool {
		return doc.Entities[i].ID < doc.Entities[j].ID
	})
	sort.SliceStable(doc.Relationships, func(i, j int) bool {
		a, b := &doc.Relationships[i], &doc.Relationships[j]
		if a.FromID != b.FromID {
			return a.FromID < b.FromID
		}
		if a.ToID != b.ToID {
			return a.ToID < b.ToID
		}
		if a.Kind != b.Kind {
			return a.Kind < b.Kind
		}
		return a.ID < b.ID
	})
	// Pass 4 outputs — order by community ID (already deterministic from the
	// algorithms layer) then size as a tiebreaker, then top-entity name to
	// stabilise ties across re-runs.
	sort.SliceStable(doc.Communities, func(i, j int) bool {
		a, b := &doc.Communities[i], &doc.Communities[j]
		if a.Size != b.Size {
			return a.Size > b.Size
		}
		if a.ID != b.ID {
			return a.ID < b.ID
		}
		ai, bi := "", ""
		if len(a.TopEntities) > 0 {
			ai = a.TopEntities[0]
		}
		if len(b.TopEntities) > 0 {
			bi = b.TopEntities[0]
		}
		return ai < bi
	})
	sort.SliceStable(doc.SurpriseEdges, func(i, j int) bool {
		a, b := &doc.SurpriseEdges[i], &doc.SurpriseEdges[j]
		if a.Score != b.Score {
			return a.Score > b.Score
		}
		if a.FromID != b.FromID {
			return a.FromID < b.FromID
		}
		return a.ToID < b.ToID
	})
}

// deterministicGeneratedAt returns the timestamp to stamp into Document.
// When SOURCE_DATE_EPOCH is set (Reproducible Builds convention,
// https://reproducible-builds.org/specs/source-date-epoch/) the timestamp
// is derived from it so byte-for-byte determinism is achievable in tests
// and verify2 harnesses. In normal operation we keep the real wall clock so
// "when was this graph generated?" stays a meaningful question.
func deterministicGeneratedAt() time.Time {
	if v := os.Getenv("SOURCE_DATE_EPOCH"); v != "" {
		if secs, err := strconv.ParseInt(v, 10, 64); err == nil {
			return time.Unix(secs, 0).UTC()
		}
	}
	return time.Now().UTC()
}
