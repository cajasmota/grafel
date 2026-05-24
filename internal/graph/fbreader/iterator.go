// Package fbreader — iterator.go provides push-style streaming iterators
// over the mmap'd graph (S8 of #2149, issue #2159).
//
// Design note: Go generics are available but the FlatBuffers stubs
// generate concrete types (*fb.Entity, *fb.Relationship) so the
// iterators below are hand-specialised for each vector type. The
// callback approach (visit func(...) bool) is used throughout so
// callers can break early without materialising a full slice.
//
// The *fb.Entity / *fb.Relationship pointers passed to visit point
// into a stack-allocated wrapper that is reused across iterations —
// callers that need to retain a value beyond the callback must copy
// the fields they need (string conversions like string(e.Id()) already
// copy the bytes out of the mmap'd region, so those are safe).
package fbreader

import (
	fb "github.com/cajasmota/archigraph/internal/graph/fbgraph"
)

// EntityIterator is a pull-style wrapper around IterateEntities for
// callers that prefer a for-loop idiom over callbacks. Construct with
// NewEntityIterator; call Next() until it returns false.
//
//	it := fbreader.NewEntityIterator(r)
//	for it.Next() {
//	    e := it.Entity()
//	    _ = string(e.Id())
//	}
type EntityIterator struct {
	r   *Reader
	i   int
	cur fb.Entity
	ok  bool
}

// NewEntityIterator returns an iterator positioned before the first entity.
func NewEntityIterator(r *Reader) *EntityIterator {
	return &EntityIterator{r: r, i: 0}
}

// Next advances to the next entity and returns true when one is
// available. The first call advances to index 0.
func (it *EntityIterator) Next() bool {
	if it.r == nil || it.r.root == nil {
		return false
	}
	for it.i < it.r.nEnts {
		ok := it.r.root.Entities(&it.cur, it.i)
		it.i++
		if ok {
			it.ok = true
			return true
		}
	}
	it.ok = false
	return false
}

// Entity returns the current entity. Must only be called after a
// successful Next(). The returned pointer is valid until the next
// Next() call.
func (it *EntityIterator) Entity() *fb.Entity {
	if !it.ok {
		return nil
	}
	return &it.cur
}

// RelationshipIterator is the pull-style counterpart for relationships.
//
//	it := fbreader.NewRelationshipIterator(r)
//	for it.Next() {
//	    rel := it.Relationship()
//	    _ = string(rel.Kind())
//	}
type RelationshipIterator struct {
	r   *Reader
	i   int
	cur fb.Relationship
	ok  bool
}

// NewRelationshipIterator returns an iterator positioned before the
// first relationship.
func NewRelationshipIterator(r *Reader) *RelationshipIterator {
	return &RelationshipIterator{r: r, i: 0}
}

// Next advances to the next relationship and returns true when one is
// available.
func (it *RelationshipIterator) Next() bool {
	if it.r == nil || it.r.root == nil {
		return false
	}
	for it.i < it.r.nRels {
		ok := it.r.root.Relationships(&it.cur, it.i)
		it.i++
		if ok {
			it.ok = true
			return true
		}
	}
	it.ok = false
	return false
}

// Relationship returns the current relationship. Must only be called
// after a successful Next(). The returned pointer is valid until the
// next Next() call.
func (it *RelationshipIterator) Relationship() *fb.Relationship {
	if !it.ok {
		return nil
	}
	return &it.cur
}
