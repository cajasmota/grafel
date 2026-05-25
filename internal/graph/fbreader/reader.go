// Package fbreader provides zero-copy mmap access to the archigraph v2
// FlatBuffers graph format produced by internal/graph/fbwriter.
//
// The Reader is intentionally thin: it memory-maps the file, parses the
// FlatBuffer root, and exposes lazy lookups over the entity and
// relationship vectors. Callers do NOT pay an unmarshal cost up-front;
// individual fields are decoded on demand against the mmap'd bytes.
//
// See ADR-0016 for the rationale.
package fbreader

import (
	"fmt"

	"golang.org/x/exp/mmap"

	fb "github.com/cajasmota/archigraph/internal/graph/fbgraph"
)

// Reader holds an mmap'd graph.fb plus a parsed root view. The zero
// value is not usable; call Open.
type Reader struct {
	ra    *mmap.ReaderAt
	buf   []byte
	root  *fb.Graph
	nEnts int
	nRels int
}

// Open memory-maps graphFB and returns a Reader. Close releases the
// mapping; the caller is responsible for invoking it.
func Open(path string) (*Reader, error) {
	ra, err := mmap.Open(path)
	if err != nil {
		return nil, fmt.Errorf("fbreader: mmap %s: %w", path, err)
	}
	// Guard against truncated or malformed files: a valid FlatBuffer needs
	// at least a 4-byte root-table offset followed by a 4-byte vtable offset.
	// Without this check, GetRootAsGraph panics on short buffers and the
	// mmap is leaked — on Windows this prevents the test's t.TempDir cleanup
	// from removing the file (issue surfaced by TestStatusGraphFileDetection).
	if ra.Len() < 8 {
		ra.Close()
		return nil, fmt.Errorf("fbreader: %s too short to be a flatbuffer (%d bytes)", path, ra.Len())
	}
	// FlatBuffers needs a contiguous []byte. mmap.ReaderAt does not expose
	// the slice directly, so we read into a single allocation. This is
	// O(N) but a single bulk memcpy from the page cache; the win comes
	// from skipping JSON parse, not from skipping the read itself.
	buf := make([]byte, ra.Len())
	if _, err := ra.ReadAt(buf, 0); err != nil {
		ra.Close()
		return nil, fmt.Errorf("fbreader: read mmap: %w", err)
	}
	// Belt-and-suspenders: catch any residual panic from FlatBuffer parsing
	// of a malformed but length-passing buffer, and free the mmap before
	// re-panicking. Without this defer, a panic from GetRootAsGraph would
	// leak the mmap even with the length check above (the check is necessary
	// but not always sufficient — vtable offsets inside the buffer can still
	// be invalid).
	defer func() {
		if r := recover(); r != nil {
			ra.Close()
			panic(r)
		}
	}()
	root := fb.GetRootAsGraph(buf, 0)
	return &Reader{
		ra:    ra,
		buf:   buf,
		root:  root,
		nEnts: root.EntitiesLength(),
		nRels: root.RelationshipsLength(),
	}, nil
}

// Close releases the underlying mmap.
func (r *Reader) Close() error {
	if r == nil || r.ra == nil {
		return nil
	}
	return r.ra.Close()
}

// Version returns the on-disk schema version (Graph.version).
func (r *Reader) Version() int { return int(r.root.Version()) }

// EntityCount returns the number of entities in the graph without
// decoding any of them.
func (r *Reader) EntityCount() int { return r.nEnts }

// RelationshipCount returns the number of relationships in the graph.
func (r *Reader) RelationshipCount() int { return r.nRels }

// LookupEntityByID returns the entity with the given id, or nil. Uses
// the FlatBuffers binary-search key index emitted by `(key)` on
// Entity.id (O(log N) over the mmap'd vector — no allocations beyond
// the returned wrapper).
func (r *Reader) LookupEntityByID(id string) *fb.Entity {
	if r == nil || r.root == nil {
		return nil
	}
	out := &fb.Entity{}
	if r.root.EntitiesByKey(out, id) {
		return out
	}
	return nil
}

// EntityAt returns the i-th entity. Useful for iteration / benchmarking;
// callers that don't already have an index typically prefer LookupEntityByID.
func (r *Reader) EntityAt(i int) *fb.Entity {
	if i < 0 || i >= r.nEnts {
		return nil
	}
	out := &fb.Entity{}
	if r.root.Entities(out, i) {
		return out
	}
	return nil
}

// RelationshipAt returns the i-th relationship. Convenience for the
// daemon's MCP handlers; iteration loops that already filter by id
// should prefer the dedicated IterateRelationshipsFrom/To helpers.
func (r *Reader) RelationshipAt(i int) *fb.Relationship {
	if i < 0 || i >= r.nRels {
		return nil
	}
	out := &fb.Relationship{}
	if r.root.Relationships(out, i) {
		return out
	}
	return nil
}

// IterateRelationshipsFromID walks the relationship vector and returns
// the (decoded-on-demand) entries whose from_id matches the given id.
//
// This is an O(R) scan because the FlatBuffers schema does not index
// edges by source. ADR-0016 phase-2 will add a sorted-by-from_id vector
// + binary search. For phase-1 this is good enough — the scan still
// avoids JSON unmarshal and decodes only the from_id field per row.
func (r *Reader) IterateRelationshipsFromID(id string) []*fb.Relationship {
	out := make([]*fb.Relationship, 0, 8)
	idBytes := []byte(id)
	for i := 0; i < r.nRels; i++ {
		rel := &fb.Relationship{}
		if !r.root.Relationships(rel, i) {
			continue
		}
		if bytesEqual(rel.FromId(), idBytes) {
			out = append(out, rel)
		}
	}
	return out
}

// IterateRelationshipsToID is the mirror of IterateRelationshipsFromID
// for inbound edges (find_references). Same O(R) scan with no
// unmarshal — decodes only the to_id field per row.
func (r *Reader) IterateRelationshipsToID(id string) []*fb.Relationship {
	out := make([]*fb.Relationship, 0, 8)
	idBytes := []byte(id)
	for i := 0; i < r.nRels; i++ {
		rel := &fb.Relationship{}
		if !r.root.Relationships(rel, i) {
			continue
		}
		if bytesEqual(rel.ToId(), idBytes) {
			out = append(out, rel)
		}
	}
	return out
}

// FilterEntitiesByKind returns every entity whose Kind() matches the
// requested kind. O(N) scan over the entity vector; only the kind field
// is decoded per row before the comparison.
func (r *Reader) FilterEntitiesByKind(kind string) []*fb.Entity {
	out := make([]*fb.Entity, 0, 8)
	kindBytes := []byte(kind)
	for i := 0; i < r.nEnts; i++ {
		ent := &fb.Entity{}
		if !r.root.Entities(ent, i) {
			continue
		}
		if bytesEqual(ent.Kind(), kindBytes) {
			out = append(out, ent)
		}
	}
	return out
}

// GraphMeta is the top-of-graph header returned by LoadGraphMeta.
// Strings are copied out of the mmap'd bytes so callers can safely use
// them after the Reader is closed (e.g. for log lines).
type GraphMeta struct {
	Version    int
	ComputedAt string
	RepoTag    string

	// Phase 0 git metadata (#2088). Empty/false for graphs written before
	// this field was added (FlatBuffers defaults new fields to zero-value).
	IndexedRef string
	IndexedSHA string
	IsWorktree bool

	// CoverageStatus — "" / "full" for a normal checkout; "partial" when
	// git sparse-checkout was active at index time (#2181 / M4 of #2175).
	// Empty for graphs written before this field was added.
	CoverageStatus string
}

// CommunityCount returns the number of aggregate Louvain communities
// encoded in the graph (#1620). Zero when the algo pass did not run.
func (r *Reader) CommunityCount() int {
	if r == nil || r.root == nil {
		return 0
	}
	return r.root.CommunitiesLength()
}

// CommunityAt returns the i-th aggregate community, or nil. Decodes on
// demand against the mmap'd bytes (#1620).
func (r *Reader) CommunityAt(i int) *fb.Community {
	if r == nil || r.root == nil || i < 0 || i >= r.root.CommunitiesLength() {
		return nil
	}
	out := &fb.Community{}
	if r.root.Communities(out, i) {
		return out
	}
	return nil
}

// AlgoStats are the corpus-level Pass-4 aggregates carried on the Graph
// root (#1620). Returned by LoadAlgoStats.
type AlgoStats struct {
	LouvainModularity   float64
	NumGodNodes         int
	NumArticulationPts  int
	NumSurpriseEdges    int
	RuntimeMS           int64
	DenoisedCommunities int
}

// LoadAlgoStats returns the corpus-level Pass-4 aggregates from the Graph
// root. All-zero when the algo pass did not run (#1620).
func (r *Reader) LoadAlgoStats() AlgoStats {
	if r == nil || r.root == nil {
		return AlgoStats{}
	}
	return AlgoStats{
		LouvainModularity:   r.root.LouvainModularity(),
		NumGodNodes:         int(r.root.NumGodNodes()),
		NumArticulationPts:  int(r.root.NumArticulationPoints()),
		NumSurpriseEdges:    int(r.root.NumSurpriseEdges()),
		RuntimeMS:           r.root.AlgoRuntimeMs(),
		DenoisedCommunities: int(r.root.DenoisedCommunities()),
	}
}

// LoadGraphMeta returns the (cheap) header fields of the graph: schema
// version, computed-at timestamp, repo tag, and Phase 0 git metadata (#2088).
// No vectors are touched; all strings are copied out of the mmap'd bytes.
func (r *Reader) LoadGraphMeta() GraphMeta {
	if r == nil || r.root == nil {
		return GraphMeta{}
	}
	return GraphMeta{
		Version:    int(r.root.Version()),
		ComputedAt: string(r.root.ComputedAt()),
		RepoTag:    string(r.root.RepoTag()),
		// Phase 0 git metadata (#2088). Defaults to "" / false for graphs
		// written before these fields were added.
		IndexedRef: string(r.root.IndexedRef()),
		IndexedSHA: string(r.root.IndexedSha()),
		IsWorktree: r.root.IsWorktree(),
		// M4 sparse-checkout (#2181). Defaults to "" for legacy graphs.
		CoverageStatus: string(r.root.CoverageStatus()),
	}
}

// EntityEmbeddingRef returns the content-hash pointer (PH8 / #2100) for the
// given entity, or "" when the field is absent (old graphs). Callers should
// treat "" as "no cache ref — fall back to inline embedding or recompute".
func EntityEmbeddingRef(e *fb.Entity) string {
	if e == nil {
		return ""
	}
	return string(e.EmbeddingRef())
}

// IterateEntities calls visit for every entity in the graph in vector
// order. If visit returns false, iteration stops early. Fields are
// decoded on demand against the mmap'd bytes — no heap allocation
// beyond the single *fb.Entity wrapper reused across calls.
//
// This is the preferred hot-path for handlers that need to scan all
// entities but do not need a materialised []graph.Entity slice (S8,
// #2159).
func (r *Reader) IterateEntities(visit func(e *fb.Entity) bool) {
	if r == nil || r.root == nil {
		return
	}
	var ent fb.Entity
	for i := 0; i < r.nEnts; i++ {
		if !r.root.Entities(&ent, i) {
			continue
		}
		if !visit(&ent) {
			return
		}
	}
}

// IterateRelationships calls visit for every relationship in the graph
// in vector order. If visit returns false, iteration stops early.
// Only the fields explicitly accessed inside visit are decoded; the
// rest remain as lazy FlatBuffer offsets in the mmap'd buffer.
//
// This is the preferred hot-path for handlers that need to scan all
// edges but do not need a materialised []graph.Relationship slice
// (S8, #2159).
func (r *Reader) IterateRelationships(visit func(rel *fb.Relationship) bool) {
	if r == nil || r.root == nil {
		return
	}
	var rel fb.Relationship
	for i := 0; i < r.nRels; i++ {
		if !r.root.Relationships(&rel, i) {
			continue
		}
		if !visit(&rel) {
			return
		}
	}
}

// FindEntityByID returns the entity for id and true when found, or
// nil and false when absent. It is a convenience alias for
// LookupEntityByID that uses the Go (value, ok) idiom instead of a
// nil sentinel, making it easier to use in if-init expressions.
//
//	if e, ok := r.FindEntityByID(id); ok { ... }
func (r *Reader) FindEntityByID(id string) (*fb.Entity, bool) {
	e := r.LookupEntityByID(id)
	return e, e != nil
}

func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
