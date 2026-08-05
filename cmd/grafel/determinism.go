package main

import (
	"os"
	"sort"
	"strconv"
	"time"

	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/types"
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
// deterministic.
//
// #6150 — the comparator itself moved to types.SortEntityRecordsCanonical so
// the incremental producer in internal/extractors sorts by the SAME order this
// path does instead of carrying a third copy that can drift. Same precedent as
// #5974 for the emission sort. Behaviour here is unchanged; this is a
// delegation, not a re-ordering.
func sortEntityRecords(rs []types.EntityRecord) {
	types.SortEntityRecordsCanonical(rs)
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
// immediately before graph.WriteAtomic. The implementation lives in
// internal/graph (#5974) so the incremental-reindex producer in
// internal/extractors shares one canonical order with the full-index
// producer here instead of carrying a copy that can drift.
func sortDocumentForEmission(doc *graph.Document) {
	graph.SortDocumentForEmission(doc)
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
