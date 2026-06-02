// Package fbwriter — StreamingWriter provides a low-memory path for writing
// graph.fb files during a full reindex.
//
// # Memory model
//
// The conventional WriteAtomic path calls Marshal(doc) which requires a
// complete *graph.Document with all entity and relationship slices already
// assembled in memory before a single FlatBuffer byte is written. For a
// 60 k-entity repository that means ≈ 60 k × ~30 KB per entity struct =
// ~1.8 GB peak working set just for the extraction buffer.
//
// StreamingWriter eliminates that buffer: callers serialize each entity
// (or relationship) immediately via WriteEntity / WriteRelationship. The
// entity data is written into the FlatBuffers builder right away; we retain
// only the 8-byte UOffsetT pointer per entity (480 KB for 60 k entities)
// plus the growing builder byte-buffer which holds the serialized tables.
// The builder buffer starts at 4 MB and grows as needed; for a 60 k-entity
// corpus the final buffer is typically 150–250 MB, but the PEAK RSS is
// dramatically lower because the caller can discard each entity struct
// after calling WriteEntity rather than accumulating them all in memory.
//
// # FlatBuffers ordering constraint
//
// FlatBuffers requires all child objects (strings, vectors, sub-tables) to
// be written into the builder BEFORE the parent table is opened. In practice
// this means we cannot construct the top-level Graph table incrementally.
// What we CAN do is flush each Entity/Relationship table into the builder
// immediately and stash its UOffsetT; then at Close() time we build the
// three top-level vectors from those stashed offsets (a single O(N) prepend
// loop over 8-byte integers) and close the Graph root. This is the standard
// FlatBuffers pattern for variable-length arrays; it does not require holding
// the original struct values after serialization.
//
// # Backward compatibility
//
// WriteAtomic and Marshal are kept unchanged and still work for callers that
// already hold a complete *graph.Document (metadata patches, tests). They now
// delegate to streamingMarshal which uses StreamingWriter internals so both
// paths exercise the same serialization code.
package fbwriter

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	flatbuffers "github.com/google/flatbuffers/go"

	"github.com/cajasmota/archigraph/internal/graph"
	fb "github.com/cajasmota/archigraph/internal/graph/fbgraph"
)

// GraphMetadata carries the scalar and string fields for the top-level
// Graph table. Populated at Close() time after all entities and
// relationships have been streamed in.
type GraphMetadata struct {
	// Repo is the repository tag (graph.Document.Repo).
	Repo string
	// GeneratedAt is the index timestamp. Zero → uses time.Now().UTC().
	GeneratedAt time.Time
	// IndexedRef is the git ref name at index time (may be empty).
	IndexedRef string
	// IndexedSHA is the abbreviated commit hash (may be empty).
	IndexedSHA string
	// IsWorktree is true when the repo is a linked git worktree.
	IsWorktree bool
	// CoverageStatus is "" / "full" for a normal checkout or "partial" when
	// git sparse-checkout is active (#2181 / M4 of #2175).
	CoverageStatus string
	// AlgorithmStats, when non-nil, writes the Pass-4 graph-algorithm
	// aggregate scalars into the Graph table.
	AlgorithmStats *graph.AlgorithmStats
	// Communities is the Pass-4 per-community aggregate list. May be nil.
	Communities []graph.CommunityResult
}

// StreamingWriter serializes entities and relationships into a FlatBuffers
// buffer one record at a time, avoiding the need to hold a fully-assembled
// *graph.Document before writing. Call Close(metadata) once all records have
// been written; it finalizes the Graph root and writes the file atomically.
//
// # When to use
//
// Use StreamingWriter during the main index pipeline where entities are
// produced batch-by-batch and the caller wants to discard each batch after
// serialization rather than accumulate the full slice. For one-shot document
// writes continue to use WriteAtomic.
//
// # Concurrency
//
// StreamingWriter is NOT safe for concurrent use from multiple goroutines.
// The indexer pipeline already guarantees single-writer access (fan-in
// happens before the write phase), so no additional synchronization is needed.
type StreamingWriter struct {
	b             *flatbuffers.Builder
	entityOffsets []flatbuffers.UOffsetT
	relOffsets    []flatbuffers.UOffsetT
	outPath       string // empty when used in-memory (streamingMarshal)
	closed        bool
}

// NewStreamingWriter creates a StreamingWriter that will eventually write to
// outPath via an atomic tmp→rename. The output directory is created if it
// does not exist. The FlatBuffers builder is pre-allocated with a 4 MB
// initial buffer which is large enough for most small-to-medium repos and
// avoids repeated doubling on the common path.
func NewStreamingWriter(outPath string) (*StreamingWriter, error) {
	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		return nil, fmt.Errorf("fbwriter.StreamingWriter: mkdir %s: %w",
			filepath.Dir(outPath), err)
	}
	// 4 MB initial buffer. Typical graph.fb is 20–80 MB; the builder grows
	// automatically via doubling so this just avoids the first few allocs.
	b := flatbuffers.NewBuilder(4 << 20)
	return &StreamingWriter{
		b:       b,
		outPath: outPath,
	}, nil
}

// WriteEntity serializes e into the internal FlatBuffers builder and stashes
// the resulting table offset. The caller's entity struct can be discarded
// immediately after this call; only the 8-byte UOffsetT is retained.
func (sw *StreamingWriter) WriteEntity(e *graph.Entity) error {
	if sw.closed {
		return fmt.Errorf("fbwriter.StreamingWriter: WriteEntity called after Close")
	}
	off := buildEntity(sw.b, e)
	sw.entityOffsets = append(sw.entityOffsets, off)
	return nil
}

// WriteRelationship serializes r into the internal FlatBuffers builder and
// stashes the resulting table offset. The caller's relationship struct can be
// discarded immediately after this call.
func (sw *StreamingWriter) WriteRelationship(r *graph.Relationship) error {
	if sw.closed {
		return fmt.Errorf("fbwriter.StreamingWriter: WriteRelationship called after Close")
	}
	off := buildRelationship(sw.b, r)
	sw.relOffsets = append(sw.relOffsets, off)
	return nil
}

// Close finalizes the FlatBuffers buffer by building the three top-level
// vectors (entities, relationships, communities), closing the Graph root
// table, and writing the result atomically to outPath via a sibling .tmp +
// rename. It must be called exactly once; subsequent calls return an error.
//
// meta carries the document-level scalar fields (repo tag, timestamp, git
// metadata, algorithm stats). Communities are also provided here rather than
// via streaming because they are assembled at the end of the index pass by
// the graph-algo engine — a separate call site from the per-entity extraction
// loop.
func (sw *StreamingWriter) Close(meta GraphMetadata) error {
	if sw.closed {
		return fmt.Errorf("fbwriter.StreamingWriter: already closed")
	}
	sw.closed = true

	buf := sw.finalize(meta)

	// In-memory mode (outPath == "") — nothing to write.
	if sw.outPath == "" {
		return nil
	}

	tmp := sw.outPath + ".tmp"
	if err := os.WriteFile(tmp, buf, 0o644); err != nil {
		return fmt.Errorf("fbwriter.StreamingWriter: write tmp: %w", err)
	}
	if err := os.Rename(tmp, sw.outPath); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("fbwriter.StreamingWriter: rename: %w", err)
	}
	return nil
}

// finalize builds the top-level Graph table and returns the finished bytes.
// Used by both Close() and streamingMarshal().
func (sw *StreamingWriter) finalize(meta GraphMetadata) []byte {
	b := sw.b

	// ── entities vector ────────────────────────────────────────────────────
	// EntitiesByKey relies on a sorted-by-key vector. The calling pipeline
	// runs sortDocumentForEmission before feeding entities in, so we
	// preserve insertion order here (same as the legacy Marshal path).
	fb.GraphStartEntitiesVector(b, len(sw.entityOffsets))
	for i := len(sw.entityOffsets) - 1; i >= 0; i-- {
		b.PrependUOffsetT(sw.entityOffsets[i])
	}
	entitiesVec := b.EndVector(len(sw.entityOffsets))

	// ── relationships vector ───────────────────────────────────────────────
	fb.GraphStartRelationshipsVector(b, len(sw.relOffsets))
	for i := len(sw.relOffsets) - 1; i >= 0; i-- {
		b.PrependUOffsetT(sw.relOffsets[i])
	}
	relsVec := b.EndVector(len(sw.relOffsets))

	// ── communities vector (#1620) ─────────────────────────────────────────
	// Communities are provided at Close() time because they are assembled by
	// the graph-algo pass which runs after all entity extraction is complete.
	commOffsets := make([]flatbuffers.UOffsetT, 0, len(meta.Communities))
	for i := range meta.Communities {
		commOffsets = append(commOffsets, buildCommunity(b, &meta.Communities[i]))
	}
	fb.GraphStartCommunitiesVector(b, len(commOffsets))
	for i := len(commOffsets) - 1; i >= 0; i-- {
		b.PrependUOffsetT(commOffsets[i])
	}
	commsVec := b.EndVector(len(commOffsets))

	// ── top-level Graph table strings ─────────────────────────────────────
	// Strings must be created before GraphStart per FlatBuffers ordering rule.
	generatedAt := meta.GeneratedAt
	if generatedAt.IsZero() {
		generatedAt = time.Now().UTC()
	}
	computedAt := b.CreateString(generatedAt.UTC().Format("2006-01-02T15:04:05Z"))
	repoTag := b.CreateString(meta.Repo)
	// Phase 0 git metadata (#2088). Always create both string offsets; the
	// FB runtime writes them as zero-length strings when values are empty,
	// which is indistinguishable from "not set" to older readers.
	indexedRef := b.CreateString(meta.IndexedRef)
	indexedSHA := b.CreateString(meta.IndexedSHA)
	// M4 sparse-checkout (#2181): create the coverage_status string offset.
	// FlatBuffers omits the field when the value is "" (default), so older
	// readers that don't know this slot see a clean zero.
	coverageStatus := b.CreateString(meta.CoverageStatus)

	// ── Graph root ─────────────────────────────────────────────────────────
	fb.GraphStart(b)
	fb.GraphAddVersion(b, int32(FormatVersion))
	fb.GraphAddComputedAt(b, computedAt)
	fb.GraphAddRepoTag(b, repoTag)
	fb.GraphAddEntities(b, entitiesVec)
	fb.GraphAddRelationships(b, relsVec)
	fb.GraphAddCommunities(b, commsVec)
	if meta.AlgorithmStats != nil {
		st := meta.AlgorithmStats
		fb.GraphAddLouvainModularity(b, st.LouvainModularity)
		fb.GraphAddNumGodNodes(b, int32(st.NumGodNodes))
		fb.GraphAddNumArticulationPoints(b, int32(st.NumArticulationPts))
		fb.GraphAddNumSurpriseEdges(b, int32(st.NumSurpriseEdges))
		fb.GraphAddAlgoRuntimeMs(b, st.RuntimeMS)
		fb.GraphAddDenoisedCommunities(b, int32(st.DenoisedCommunities))
	}
	fb.GraphAddIndexedRef(b, indexedRef)
	fb.GraphAddIndexedSha(b, indexedSHA)
	if meta.IsWorktree {
		fb.GraphAddIsWorktree(b, true)
	}
	if meta.CoverageStatus != "" {
		fb.GraphAddCoverageStatus(b, coverageStatus)
	}
	root := fb.GraphEnd(b)
	fb.FinishGraphBuffer(b, root)

	return b.FinishedBytes()
}

// EntityCount returns the number of entities written so far. Safe to call
// before Close.
func (sw *StreamingWriter) EntityCount() int { return len(sw.entityOffsets) }

// RelationshipCount returns the number of relationships written so far. Safe
// to call before Close.
func (sw *StreamingWriter) RelationshipCount() int { return len(sw.relOffsets) }

// ─── streamingMarshal ─────────────────────────────────────────────────────
//
// streamingMarshal is the implementation of Marshal. It builds a FlatBuffer
// from a complete *graph.Document by streaming each entity and relationship
// through an in-memory StreamingWriter (no filesystem I/O). This ensures
// WriteAtomic and the streaming pipeline share identical serialization code.

func streamingMarshal(doc *graph.Document) ([]byte, error) {
	if doc == nil {
		return nil, fmt.Errorf("nil document")
	}

	// In-memory mode: outPath == "" skips the filesystem write in Close.
	sw := &StreamingWriter{
		b:       flatbuffers.NewBuilder(1 << 20),
		outPath: "",
	}

	for i := range doc.Entities {
		if err := sw.WriteEntity(&doc.Entities[i]); err != nil {
			return nil, err
		}
	}
	for i := range doc.Relationships {
		if err := sw.WriteRelationship(&doc.Relationships[i]); err != nil {
			return nil, err
		}
	}

	sw.closed = true // prevent double-finalize via Close
	return sw.finalize(GraphMetadata{
		Repo:           doc.Repo,
		GeneratedAt:    doc.GeneratedAt,
		IndexedRef:     doc.IndexedRef,
		IndexedSHA:     doc.IndexedSHA,
		IsWorktree:     doc.IsWorktree,
		CoverageStatus: doc.CoverageStatus,
		AlgorithmStats: doc.AlgorithmStats,
		Communities:    doc.Communities,
	}), nil
}
