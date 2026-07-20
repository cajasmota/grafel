package mcp

// F2 of ADR-0027 (mmap + zero-copy resident graph): the handle-keyed hot index.
//
// The resident hot index (id / label / qualified-name lookups over the HOT
// accessors) is what queries filter against — never per-entity view dispatch.
// The blocking F2 criterion is that this index is BUILT FROM and KEYED OFF the
// MapHandle a call captured under s.mu in borrowGroup — NOT a live lr.Doc /
// lr.handle re-deref at read time. Reload mutates *LoadedRepo in place
// (repointing lr.handle to a successor this call never borrow()-incremented), so
// keying the index off the captured handle is exactly the read-through-captured-
// handle invariant (ADR-0027 §Correctness): the handle the index carries stays
// mapped for the whole call because the borrow deferred its munmap.
//
// The builder depends ONLY on a (handle, entityViewSource) seam. In F2 the sole
// source is the heap Document (docEntityViewSource); F3 adds an mmap-backed
// source that reads through the SAME captured handle, without touching the
// builder or its consumers. The index stores graph.EntityView values, so it is
// agnostic to whether the backing entity is materialized or mmap-aliased.
//
// This index is a NEW structure ALONGSIDE the existing LabelIndex / getByID /
// getCallsAdj internals (which a parallel Tier-2b PR owns) — it never re-keys or
// re-backs them. It is inert in F2 (no production query reads it yet), like F1's
// borrow protocol; it exists to be exercised under test and to be lit up by F3.

import (
	"strings"

	"github.com/cajasmota/grafel/internal/graph"
)

// entityViewSource yields the EntityViews a hot index is built from. It is the
// storage seam that lets the builder be fed by the heap Document today and by an
// mmap-backed reader in F3, without either knowing about the other.
type entityViewSource interface {
	// forEachEntityView invokes yield once per entity view. Implementations must
	// tolerate a nil receiver-held document and simply yield nothing.
	forEachEntityView(yield func(graph.EntityView))
}

// docEntityViewSource adapts a captured *graph.Document into an entityViewSource
// by wrapping each entity as a materialized graph.EntityView. This is F2's only
// source; F3 replaces it (per repo load, behind GRAFEL_SERVE_FROM_MMAP) with an
// mmap-backed source over the same captured handle.
type docEntityViewSource struct{ doc *graph.Document }

func (s docEntityViewSource) forEachEntityView(yield func(graph.EntityView)) {
	if s.doc == nil {
		return
	}
	for i := range s.doc.Entities {
		yield(graph.EntityViewOf(&s.doc.Entities[i]))
	}
}

// relationshipViewSource is the relationship analogue of entityViewSource: it
// yields the RelationshipViews a hot index is built from. W1 (ADR-0027) adds it
// to fill the gap F2 left — F2 had no relationship view source at all. Same seam
// shape as entityViewSource so F3 can later feed relationships from the mmap
// without touching the builder or its consumers.
//
// The int32 CSR adjacency (traversal.go) is deliberately NOT routed through this
// seam: graph traversal stays on primitive arrays for hot-path perf (ADR-0027
// §Risk). This source feeds only by-id lookup and full-scan iteration.
type relationshipViewSource interface {
	forEachRelationshipView(yield func(graph.RelationshipView))
}

// docRelationshipViewSource adapts a captured *graph.Document into a
// relationshipViewSource by wrapping each relationship as a materialized
// graph.RelationshipView. W1's only source; F3 later feeds this from mmap over
// the same captured handle.
type docRelationshipViewSource struct{ doc *graph.Document }

func (s docRelationshipViewSource) forEachRelationshipView(yield func(graph.RelationshipView)) {
	if s.doc == nil {
		return
	}
	for i := range s.doc.Relationships {
		yield(graph.RelationshipViewOf(&s.doc.Relationships[i]))
	}
}

// hotIndex is the handle-keyed resident hot index over EntityViews: exact-ID,
// case-insensitive label (Name), and case-insensitive qualified-name lookups.
// handle is the MapHandle the owning call captured under s.mu; it is the read
// cursor the index binds to for its whole lifetime. The lookup maps mirror
// LabelIndex's key semantics (lowercased label/qname) but are an independent,
// additive structure — they never alias or re-key LabelIndex's maps.
type hotIndex struct {
	handle  *MapHandle
	byID    map[string]graph.EntityView
	byLabel map[string][]graph.EntityView
	byQName map[string]graph.EntityView
	// ents is the ordered entity views (Doc order) for full scans. W1 (ADR-0027)
	// keeps this alongside byID so forEachEntity yields a deterministic order
	// matching Doc.Entities, which byID's map iteration cannot.
	ents []graph.EntityView
	// relByID / rels are the W1 relationship seam: exact-id lookup + ordered
	// (Doc order) full-scan iteration. nil until addRelationships is called.
	relByID map[string]graph.RelationshipView
	rels    []graph.RelationshipView
}

// buildHotIndex builds a hot index from the views yielded by src, KEYED OFF the
// captured handle h. It never dereferences a live lr.Doc / lr.handle — the only
// inputs are the captured handle and the view source, which is the property F3
// relies on to feed the same builder from the mmap. A nil src yields an empty
// index that still carries the captured handle.
func buildHotIndex(h *MapHandle, src entityViewSource) *hotIndex {
	hi := &hotIndex{
		handle:  h,
		byID:    map[string]graph.EntityView{},
		byLabel: map[string][]graph.EntityView{},
		byQName: map[string]graph.EntityView{},
	}
	if src == nil {
		return hi
	}
	src.forEachEntityView(func(v graph.EntityView) {
		if v == nil {
			return
		}
		hi.byID[v.ID()] = v
		lbl := strings.ToLower(v.Name())
		hi.byLabel[lbl] = append(hi.byLabel[lbl], v)
		if qn := v.QualifiedName(); qn != "" {
			hi.byQName[strings.ToLower(qn)] = v
		}
		hi.ents = append(hi.ents, v)
	})
	return hi
}

// addRelationships populates the hot index's relationship seam (relByID + the
// ordered rels slice) from src and returns the index for chaining. W1 keeps this
// SEPARATE from buildHotIndex so F2's entity-only builder signature is unchanged
// (dual-API, additive). A nil src yields an empty (non-nil) relByID so a lookup
// misses cleanly instead of panicking on a nil map read... which maps already
// tolerate, but the non-nil map also marks the seam as populated.
func (hi *hotIndex) addRelationships(src relationshipViewSource) *hotIndex {
	if hi == nil {
		return hi
	}
	hi.relByID = map[string]graph.RelationshipView{}
	if src == nil {
		return hi
	}
	src.forEachRelationshipView(func(v graph.RelationshipView) {
		if v == nil {
			return
		}
		hi.relByID[v.ID()] = v
		hi.rels = append(hi.rels, v)
	})
	return hi
}

// Handle returns the MapHandle this index is keyed off — the handle captured
// under s.mu by the owning call's borrowGroup snapshot.
func (hi *hotIndex) Handle() *MapHandle {
	if hi == nil {
		return nil
	}
	return hi.handle
}

// entityByID returns the view for an exact entity ID.
func (hi *hotIndex) entityByID(id string) (graph.EntityView, bool) {
	if hi == nil {
		return nil, false
	}
	v, ok := hi.byID[id]
	return v, ok
}

// entitiesByLabel returns every view whose Name matches label, case-insensitively.
func (hi *hotIndex) entitiesByLabel(label string) []graph.EntityView {
	if hi == nil {
		return nil
	}
	return hi.byLabel[strings.ToLower(label)]
}

// entityByQName returns the view for a qualified name, case-insensitively.
func (hi *hotIndex) entityByQName(qname string) (graph.EntityView, bool) {
	if hi == nil {
		return nil, false
	}
	v, ok := hi.byQName[strings.ToLower(qname)]
	return v, ok
}

// forEachEntity invokes yield once per entity view, in Doc order. Nil-safe.
func (hi *hotIndex) forEachEntity(yield func(graph.EntityView)) {
	if hi == nil {
		return
	}
	for _, v := range hi.ents {
		yield(v)
	}
}

// relationshipByID returns the view for an exact relationship ID.
func (hi *hotIndex) relationshipByID(id string) (graph.RelationshipView, bool) {
	if hi == nil {
		return nil, false
	}
	v, ok := hi.relByID[id]
	return v, ok
}

// forEachRelationship invokes yield once per relationship view, in Doc order.
// Nil-safe.
func (hi *hotIndex) forEachRelationship(yield func(graph.RelationshipView)) {
	if hi == nil {
		return
	}
	for _, v := range hi.rels {
		yield(v)
	}
}

// buildHotIndex builds a handle-keyed hot index for repo from this per-call
// snapshot: it pairs the handle borrowed (captured) under s.mu with the view
// source. The handle comes from the snapshot — never a live lr.handle re-deref —
// so the resulting index satisfies the read-through-captured-handle invariant.
// In F2 the caller passes a docEntityViewSource over the repo's heap Document;
// F3 passes an mmap-backed source over the same captured handle.
func (b *groupBorrow) buildHotIndex(repo string, src entityViewSource) *hotIndex {
	return buildHotIndex(b.Handle(repo), src)
}

// hotIndexFor returns the memoized hot index for repo, built lazily off THIS
// call's captured handle and reused across borrows of that same handle (W1,
// ADR-0027). It is the production entry point the view getters below share.
//
// The build is memoized on the LoadedRepo keyed by the captured handle's
// identity (LoadedRepo.hotIndexFor): the first view-getter use for a given
// handle builds the index; later borrows of the SAME handle reuse it; a reload
// (which publishes a successor handle and clears the memo under s.mu) forces the
// next borrow to rebuild against the fresh handle. The index is never built
// eagerly on borrow — only when a consumer first calls a view getter — so the
// hot index adds zero cost until something reads it.
//
// The source is the heap Document (docEntityViewSource / docRelationshipView-
// Source over lr.Doc): the views wrap the SAME &lr.Doc.Entities[i] /
// &lr.Doc.Relationships[i] pointers the legacy LabelIndex / getByID path returns,
// so this is behavior-neutral. F3 swaps in an mmap-backed source at cutover.
func (b *groupBorrow) hotIndexFor(repo string) *hotIndex {
	if b == nil || b.Group == nil {
		return nil
	}
	lr := b.Group.Repos[repo]
	if lr == nil {
		return nil
	}
	h := b.Handle(repo)
	return lr.hotIndexFor(h, func(handle *MapHandle) *hotIndex {
		doc := lr.Doc
		return buildHotIndex(handle, docEntityViewSource{doc: doc}).
			addRelationships(docRelationshipViewSource{doc: doc})
	})
}

// entityViewByID resolves an exact entity ID to a graph.EntityView through the
// memoized hot index. Additive production surface (W1): behavior-parity with
// LoadedRepo.getByID()/LabelIndex.ByID, but view-typed and handle-keyed.
func (b *groupBorrow) entityViewByID(repo, id string) (graph.EntityView, bool) {
	return b.hotIndexFor(repo).entityByID(id)
}

// entityViewsByLabel returns every entity view whose Name matches label,
// case-insensitively — the view-typed analogue of LabelIndex.ByLabel.
func (b *groupBorrow) entityViewsByLabel(repo, label string) []graph.EntityView {
	return b.hotIndexFor(repo).entitiesByLabel(label)
}

// entityViewByQName resolves a qualified name (case-insensitive) to a view — the
// view-typed analogue of LabelIndex.ByQName.
func (b *groupBorrow) entityViewByQName(repo, qname string) (graph.EntityView, bool) {
	return b.hotIndexFor(repo).entityByQName(qname)
}

// forEachEntityView invokes yield once per entity view for repo, in Doc order —
// the view-typed full-scan analogue of iterating lr.Doc.Entities.
func (b *groupBorrow) forEachEntityView(repo string, yield func(graph.EntityView)) {
	b.hotIndexFor(repo).forEachEntity(yield)
}

// relationshipViewByID resolves an exact relationship ID to a
// graph.RelationshipView through the memoized hot index (W1 relationship seam).
func (b *groupBorrow) relationshipViewByID(repo, id string) (graph.RelationshipView, bool) {
	return b.hotIndexFor(repo).relationshipByID(id)
}

// forEachRelationshipView invokes yield once per relationship view for repo, in
// Doc order — the view-typed full-scan analogue of iterating lr.Doc.Relationships.
// Traversal/adjacency consumers keep the int32 CSR (traversal.go), NOT this seam.
func (b *groupBorrow) forEachRelationshipView(repo string, yield func(graph.RelationshipView)) {
	b.hotIndexFor(repo).forEachRelationship(yield)
}
