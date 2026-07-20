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

// buildHotIndex builds a handle-keyed hot index for repo from this per-call
// snapshot: it pairs the handle borrowed (captured) under s.mu with the view
// source. The handle comes from the snapshot — never a live lr.handle re-deref —
// so the resulting index satisfies the read-through-captured-handle invariant.
// In F2 the caller passes a docEntityViewSource over the repo's heap Document;
// F3 passes an mmap-backed source over the same captured handle.
func (b *groupBorrow) buildHotIndex(repo string, src entityViewSource) *hotIndex {
	return buildHotIndex(b.Handle(repo), src)
}
