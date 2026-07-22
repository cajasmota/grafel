// Package fbwriter — segmented.go implements the bounded-memory SegmentedWriter
// that produces the multi-segment gen-dir graph format the #5901 read substrate
// already understands. It is the write half of the #5890 streaming-write epic
// (issue #5902, slice (c)); the reader (MultiReader / OpenSegmentsWithRanges)
// landed dark in #5901 and is unchanged here.
//
// # The problem it solves (#5726)
//
// The single-file writer (streaming.go) streams entities into ONE growing
// flatbuffers.Builder and finalizes it in a single tmp+rename. The builder
// grows to the FULL serialized size (~150-250 MB on a 60 k-entity corpus, and
// unbounded on huge ones) and the vendored flatbuffers library hard-panics at
// its 2 GiB cap — the #5726 "graph too large to serialize" cliff (today merely
// recovered into a degraded error). SegmentedWriter bounds the builder: it
// FLUSHES a finished segment file and starts a FRESH builder whenever the
// current builder crosses a byte threshold, so PEAK builder memory stays ~one
// threshold, NOT O(graph).
//
// # Layout produced
//
// A graph that fits under the threshold in ONE builder takes the single-file
// fast path: a plain graph.<gen>.fb written by the existing flat writer,
// byte-identical to today (no dir, no manifest — decision 1). Only a graph too
// large for one builder gets the gen-dir layout:
//
//	graph.<gen>/
//	    seg-0000.fb   entity segment 0  (entities [MinKey..MaxKey], sorted)
//	    seg-0001.fb   entity segment 1  (entities (prev.MaxKey..MaxKey])
//	    ...
//	    seg-000N.fb   relationship segment 0
//	    ...
//	    manifest.json
//
// # Per-segment sort + key bounds (the #5901 forward-carry invariant)
//
// Entities are sorted by their id string with Go's `<` (byte-wise, unsigned) —
// the SAME ordering FlatBuffers `(key)` uses (memcmp on Entity.id) and the SAME
// ordering the reader's KeyRange.contains uses. Because the writer streams the
// GLOBALLY sorted entity slice and cuts a new segment only at a threshold
// boundary, every entity segment is (a) internally sorted, so its FlatBuffers
// EntitiesByKey binary search works, and (b) a CONTIGUOUS, NON-OVERLAPPING
// key window: segment i's MaxKey < segment i+1's MinKey (ids are unique). Each
// SegmentMeta records [MinKey,MaxKey] as the exact byte-wise min (first) and
// max (last) id in that segment. The reader routes a lookup to the one segment
// whose inclusive [Min,Max] window contains the key and provably never
// false-skips a present entity, because the segments tile the whole sorted id
// space with no gaps that could strand a key. See TestSegmentedRoundTrip.
//
// # Independent entity / relationship cadence (decision 5)
//
// Entity segments and relationship segments are flushed on their OWN thresholds
// into SEPARATE files with the matching SegmentKind; from_id/to_id are stable
// id strings (graph.fbs `(key)`-free shared strings) so a relationship in one
// segment resolves its endpoints against entities in any segment via the
// reader's cross-segment LookupEntityByID — no forward-ref bookkeeping.
package fbwriter

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"

	flatbuffers "github.com/google/flatbuffers/go"

	"github.com/cajasmota/grafel/internal/graph"
)

// DefaultSegmentThresholdBytes is the default builder-size flush threshold: a
// segment is finalized and a fresh builder started once the in-progress
// builder crosses this many serialized bytes. 100 MB keeps peak builder memory
// far below the flatbuffers 2 GiB cap (#5726) while keeping the segment count
// low on typical corpora. Tunable via GRAFEL_SEGMENT_BYTES.
const DefaultSegmentThresholdBytes = 100 << 20

// segInitBuilderSize is the initial capacity of each per-segment builder. It
// only affects the number of internal doubling reallocs, never the finished
// bytes; 4 MB matches NewStreamingWriter's default.
const segInitBuilderSize = 4 << 20

// StreamSegmentsEnabled reports whether the flag-gated segmented producer is
// ON (GRAFEL_STREAM_SEGMENTS truthy). OFF by default: producers then write
// today's single flat graph.<gen>.fb with zero behaviour change. This slice
// does NOT flip the default — the flip rides the final #5912 serve slice.
func StreamSegmentsEnabled() bool {
	return envTruthy(os.Getenv("GRAFEL_STREAM_SEGMENTS"))
}

// segmentThresholdBytes resolves the flush threshold, honouring the
// GRAFEL_SEGMENT_BYTES override (positive integer, bytes) and falling back to
// DefaultSegmentThresholdBytes for an unset/invalid value.
func segmentThresholdBytes() int {
	if v := os.Getenv("GRAFEL_SEGMENT_BYTES"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return DefaultSegmentThresholdBytes
}

// envTruthy interprets an env-var value as a boolean (mirrors the helper in
// internal/graph/algorithms.go; kept local to avoid an import cycle).
func envTruthy(v string) bool {
	switch v {
	case "1", "t", "T", "true", "TRUE", "True", "yes", "YES", "Yes", "on", "ON", "On":
		return true
	}
	return false
}

// WriteGraphGenSegmented is the flag-ON producer path. It sorts doc into the
// canonical byte-wise key order, then:
//
//   - if the whole graph fits under the threshold in a single builder, takes
//     the single-file fast path (writeGraphGenFlat → plain graph.<gen>.fb,
//     byte-identical to the flag-OFF path); otherwise
//   - streams entity segments and relationship segments on independent
//     thresholds into a fresh graph.<gen>/ dir, writes manifest.json, flips the
//     `current` pointer at the gen dir, and best-effort GCs stale generations.
//
// Returns the absolute gen path written: the graph.<gen>.fb file for the
// single-file case, or the graph.<gen>/ dir for a segment-set (filepath.Dir of
// either is stateDir, so directory-keyed sidecar writers land correctly).
func WriteGraphGenSegmented(stateDir string, doc *graph.Document) (string, error) {
	if doc == nil {
		return "", fmt.Errorf("fbwriter: nil document")
	}
	sortDocForSegments(doc)
	threshold := segmentThresholdBytes()

	// Single-file fast path (decision 1). The probe is bounded: it early-exits
	// the instant the builder crosses the threshold, so even a multi-GiB graph
	// never materialises more than ~threshold bytes here.
	if graphFitsSingleBuilder(doc, threshold) {
		return writeGraphGenFlat(stateDir, doc)
	}
	return writeSegments(stateDir, doc, threshold)
}

// sortDocForSegments sorts doc into the canonical emission order in place:
// entities by byte-wise id (the FlatBuffers `(key)` / reader KeyRange order),
// relationships by (from,to,kind). Producers already sort before calling, so
// this is idempotent; it is repeated here to make the per-segment key-bounds
// invariant hold regardless of the caller.
func sortDocForSegments(doc *graph.Document) {
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
		return a.Kind < b.Kind
	})
}

// graphFitsSingleBuilder reports whether the entire graph serializes to fewer
// than threshold bytes in ONE builder. It builds entity then relationship
// tables into a throwaway builder and returns false the instant the running
// serialized size crosses the threshold — a bounded probe whose peak is
// ~threshold+one-record, never O(graph). The builder is discarded; the caller
// re-marshals via the canonical flat path to guarantee byte-identity.
func graphFitsSingleBuilder(doc *graph.Document, threshold int) bool {
	b := flatbuffers.NewBuilder(segInitBuilderSize)
	for i := range doc.Entities {
		buildEntity(b, &doc.Entities[i])
		if int(b.Offset()) >= threshold {
			return false
		}
	}
	for i := range doc.Relationships {
		buildRelationship(b, &doc.Relationships[i])
		if int(b.Offset()) >= threshold {
			return false
		}
	}
	return true
}

// writeSegments performs the real bounded segmented write: entity segments
// first (each a self-contained Graph FlatBuffer whose entities vector is the
// contiguous sorted slice for that segment), then relationship segments, then
// manifest + pointer flip + GC. Peak builder memory is bounded by the flush
// threshold: each segment builder is finalized, written, and dropped before
// the next is allocated, so no single builder ever holds the whole graph.
func writeSegments(stateDir string, doc *graph.Document, threshold int) (string, error) {
	gen := graph.NextGen(stateDir)
	genDirName := graph.GenDirName(gen)
	genDir := filepath.Join(stateDir, genDirName)
	if err := os.MkdirAll(genDir, 0o755); err != nil {
		return "", fmt.Errorf("fbwriter: mkdir gen dir %s: %w", genDir, err)
	}

	fullMeta := graphMetaFromDoc(doc)
	var (
		metas    []graph.SegmentMeta
		segIdx   int
		firstSeg = true // the first written segment carries the graph header
	)

	// metaForNextSegment returns the header metadata for the segment about to be
	// finalized: the FULL header (repo, git meta, algo stats, communities) on
	// the very first segment written — the reader's MultiReader reads header
	// fields from segment 0 — and an empty header on every later segment.
	metaForNextSegment := func() GraphMetadata {
		if firstSeg {
			return fullMeta
		}
		return GraphMetadata{}
	}

	// ── entity segments ──────────────────────────────────────────────────
	var (
		eb     *flatbuffers.Builder
		eOffs  []flatbuffers.UOffsetT
		minKey string
		maxKey string
	)
	flushEntities := func() error {
		if len(eOffs) == 0 {
			return nil
		}
		sw := &StreamingWriter{b: eb, entityOffsets: eOffs}
		buf, err := sw.finalizeSafe(metaForNextSegment())
		if err != nil {
			return err
		}
		name := graph.SegmentFileName(segIdx)
		if err := writeSegmentFile(genDir, name, buf); err != nil {
			return err
		}
		metas = append(metas, graph.SegmentMeta{
			File:        name,
			Kind:        graph.SegmentEntities,
			EntityCount: len(eOffs),
			MinKey:      minKey,
			MaxKey:      maxKey,
		})
		segIdx++
		firstSeg = false
		eb, eOffs, minKey, maxKey = nil, nil, "", "" // free the builder
		return nil
	}
	for i := range doc.Entities {
		e := &doc.Entities[i]
		if eb == nil {
			eb = flatbuffers.NewBuilder(segInitBuilderSize)
		}
		if len(eOffs) == 0 {
			minKey = e.ID // first (== smallest) id in this segment
		}
		eOffs = append(eOffs, buildEntity(eb, e))
		maxKey = e.ID // last appended (== largest so far) id in this segment
		if int(eb.Offset()) >= threshold {
			if err := flushEntities(); err != nil {
				return "", err
			}
		}
	}
	if err := flushEntities(); err != nil {
		return "", err
	}

	// ── relationship segments (independent cadence) ──────────────────────
	var (
		rb    *flatbuffers.Builder
		rOffs []flatbuffers.UOffsetT
	)
	flushRels := func() error {
		if len(rOffs) == 0 {
			return nil
		}
		sw := &StreamingWriter{b: rb, relOffsets: rOffs}
		buf, err := sw.finalizeSafe(metaForNextSegment())
		if err != nil {
			return err
		}
		name := graph.SegmentFileName(segIdx)
		if err := writeSegmentFile(genDir, name, buf); err != nil {
			return err
		}
		metas = append(metas, graph.SegmentMeta{
			File:     name,
			Kind:     graph.SegmentRelationships,
			RelCount: len(rOffs),
		})
		segIdx++
		firstSeg = false
		rb, rOffs = nil, nil // free the builder
		return nil
	}
	for i := range doc.Relationships {
		r := &doc.Relationships[i]
		if rb == nil {
			rb = flatbuffers.NewBuilder(segInitBuilderSize)
		}
		rOffs = append(rOffs, buildRelationship(rb, r))
		if int(rb.Offset()) >= threshold {
			if err := flushRels(); err != nil {
				return "", err
			}
		}
	}
	if err := flushRels(); err != nil {
		return "", err
	}

	// Defensive: an empty graph should have taken the single-file fast path;
	// if we somehow produced no segments, fall back rather than write an
	// invalid (zero-segment) manifest.
	if len(metas) == 0 {
		return writeGraphGenFlat(stateDir, doc)
	}

	m := &graph.Manifest{FormatVersion: graph.ManifestFormatVersion, Segments: metas}
	if err := graph.WriteManifest(genDir, m); err != nil {
		return "", fmt.Errorf("fbwriter: write manifest: %w", err)
	}
	if err := graph.WriteCurrentPointerRaw(stateDir, genDirName); err != nil {
		return "", fmt.Errorf("fbwriter: flip pointer to %s: %w", genDirName, err)
	}
	graph.GCStaleGens(stateDir, gen) // best-effort; never fails the write
	return genDir, nil
}

// writeSegmentFile writes buf as genDir/name via a sibling .tmp + rename. The
// gen dir is freshly allocated (NextGen) and its `current` pointer is not
// flipped until every segment + the manifest are durably in place, so a reader
// never resolves the pointer to a half-written segment set.
func writeSegmentFile(genDir, name string, buf []byte) error {
	dst := filepath.Join(genDir, name)
	tmp := dst + ".tmp"
	if err := os.WriteFile(tmp, buf, 0o644); err != nil {
		return fmt.Errorf("fbwriter: write segment tmp %s: %w", name, err)
	}
	if err := os.Rename(tmp, dst); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("fbwriter: rename segment %s: %w", name, err)
	}
	return nil
}

// graphMetaFromDoc lifts the document-level header fields into a GraphMetadata,
// mirroring streamingMarshal's construction so the header segment carries the
// same repo / git / algo / community metadata a single-file graph would.
func graphMetaFromDoc(doc *graph.Document) GraphMetadata {
	return GraphMetadata{
		Repo:           doc.Repo,
		GeneratedAt:    doc.GeneratedAt,
		IndexedRef:     doc.IndexedRef,
		IndexedSHA:     doc.IndexedSHA,
		IsWorktree:     doc.IsWorktree,
		CoverageStatus: doc.CoverageStatus,
		AlgorithmStats: doc.AlgorithmStats,
		Communities:    doc.Communities,
	}
}
