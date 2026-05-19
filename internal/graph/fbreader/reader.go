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
	// FlatBuffers needs a contiguous []byte. mmap.ReaderAt does not expose
	// the slice directly, so we read into a single allocation. This is
	// O(N) but a single bulk memcpy from the page cache; the win comes
	// from skipping JSON parse, not from skipping the read itself.
	buf := make([]byte, ra.Len())
	if _, err := ra.ReadAt(buf, 0); err != nil {
		ra.Close()
		return nil, fmt.Errorf("fbreader: read mmap: %w", err)
	}
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
