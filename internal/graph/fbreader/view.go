package fbreader

import (
	fb "github.com/cajasmota/grafel/internal/graph/fbgraph"
)

// GraphView is the read surface shared by the single-file *Reader and the
// multi-segment *MultiReader. It exists so segment-aware call sites (the #5890
// gen-dir read substrate, #5901) can hold "a graph" without caring whether it
// is backed by one mmap or N segment mmaps — the single-file fast path returns
// a *Reader, a segment-set returns a *MultiReader, and both satisfy this.
//
// Every method is already implemented identically on both concrete types
// (MultiReader was built in #5906 to mirror Reader's surface exactly), so this
// interface introduces NO behaviour change for the single-file path — it just
// names the common contract.
type GraphView interface {
	// Close releases the underlying mmap(s). After Close no field/string
	// previously read from this view may be accessed.
	Close() error

	// Version is the on-disk FB format version (first segment's header for a
	// segment-set).
	Version() int

	// EntityCount / RelationshipCount are the totals across all segments.
	EntityCount() int
	RelationshipCount() int

	// LookupEntityByID returns the entity with id, or nil.
	LookupEntityByID(id string) *fb.Entity
	// FindEntityByID is the (value, ok) idiom counterpart.
	FindEntityByID(id string) (*fb.Entity, bool)

	// EntityAt / RelationshipAt index the concatenated per-segment vectors.
	EntityAt(i int) *fb.Entity
	RelationshipAt(i int) *fb.Relationship

	// IterateEntities / IterateRelationships visit every row across segments.
	IterateEntities(visit func(e *fb.Entity) bool)
	IterateRelationships(visit func(rel *fb.Relationship) bool)

	// IterateRelationshipsFromID / ToID collect edges by endpoint.
	IterateRelationshipsFromID(id string) []*fb.Relationship
	IterateRelationshipsToID(id string) []*fb.Relationship

	// FilterEntitiesByKind collects entities of a given kind.
	FilterEntitiesByKind(kind string) []*fb.Entity

	// CommunityCount / CommunityAt expose the Pass-4 aggregate communities.
	CommunityCount() int
	CommunityAt(i int) *fb.Community

	// LoadGraphMeta / LoadAlgoStats expose the Graph-root header fields.
	LoadGraphMeta() GraphMeta
	LoadAlgoStats() AlgoStats
}

// Compile-time proof both concrete readers satisfy the shared contract. If a
// future method is added to one type's public surface that consumers rely on,
// add it here and these assertions keep the two implementations in lock-step.
var (
	_ GraphView = (*Reader)(nil)
	_ GraphView = (*MultiReader)(nil)
)
