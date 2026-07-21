// F3 of ADR-0027 (mmap + zero-copy resident graph): the mmap-backed read path.
//
// F1 gave us a lifetime-safe MapHandle (deferred-unmap refcount-drain); F2 gave
// us the EntityView/RelationshipView seam plus a handle-keyed hot index whose
// only inputs are (captured handle, entityViewSource). F3 supplies the OTHER
// implementation of that seam: views that read ZERO-COPY straight out of the
// FlatBuffers accessors — which alias the MapHandle's mmap — and an
// entityViewSource that yields them from the FB reader instead of the heap
// Document. A default-off flag (GRAFEL_SERVE_FROM_MMAP) selects which source the
// hot-index build uses per repo load; OFF keeps F2's docEntityViewSource, so
// default behaviour stays byte-identical to the heap path.
//
// LIFETIME (load-bearing). Every string these views return is an unsafe.String
// aliasing the mmap bytes of the handle the owning call captured under s.mu
// (the read-through-captured-handle invariant, ADR-0027 §Correctness). Such a
// string is valid ONLY for the duration of that borrow. The hot index is keyed
// off the captured handle, so its aliased keys/values live exactly as long as
// the borrow — the last releaser's munmap runs only after the borrow drops.
// A zero-copy string MUST NOT be stored past release(); doing so is a
// use-after-unmap. This is exactly what the -race lifetime test guards.
package mcp

import (
	"os"
	"strings"
	"unsafe"

	"github.com/cajasmota/grafel/internal/graph"
	fb "github.com/cajasmota/grafel/internal/graph/fbgraph"
)

// GRAFEL_SERVE_FROM_MMAP, when truthy, makes the hot-index build read through the
// mmap (mmapEntityViewSource) instead of the heap Document (docEntityViewSource).
// Default OFF: default serve behaviour is byte-identical to the pre-F3 heap path.
// Read ONCE at package load (below), never per-query, per ADR-0027 §F3.
var serveFromMMapEnabled = parseServeFromMMapFlag(os.Getenv("GRAFEL_SERVE_FROM_MMAP"))

// parseServeFromMMapFlag interprets the env value. Pure and total so the flag
// wiring is unit-testable without mutating process env. Truthy: 1/true/yes/on
// (case-insensitive, trimmed); everything else — including "" — is OFF.
func parseServeFromMMapFlag(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

// serveFromMMap reports whether the mmap-backed read path is enabled. Reads the
// cached flag value captured at load — no per-call env lookup.
func serveFromMMap() bool { return serveFromMMapEnabled }

// zeroCopyString aliases bv as a string WITHOUT copying, via unsafe.String over
// the FlatBuffers ByteVector bytes (which alias the MapHandle's mmap). The
// returned string is valid ONLY while the owning borrow is held.
//
// Empty-ByteVector guard (ADR-0027 §plan): unsafe.String(&bv[0], 0) evaluates
// &bv[0], which indexes an empty slice and PANICS. It hits real data — an empty
// Signature/Subtype/module is a zero-length (present) vector, and an absent
// field is a nil vector. Both are len==0 → "". Guarded here, tested by
// TestZeroCopyStringEmptyByteVector.
func zeroCopyString(bv []byte) string {
	if len(bv) == 0 {
		return ""
	}
	return unsafe.String(&bv[0], len(bv))
}

// mmapEntityView is the F3 zero-copy EntityView: it wraps one *fb.Entity whose
// _tab.Bytes alias the mmap, and returns cold string fields as unsafe.String
// aliases over the FB ByteVector accessors. It holds its OWN *fb.Entity (the FB
// reader hands out a fresh wrapper per EntityAt), so the hot index may retain it
// for the borrow's lifetime.
type mmapEntityView struct{ e *fb.Entity }

func (v mmapEntityView) ID() string            { return zeroCopyString(v.e.Id()) }
func (v mmapEntityView) Kind() string          { return zeroCopyString(v.e.Kind()) }
func (v mmapEntityView) Name() string          { return zeroCopyString(v.e.Name()) }
func (v mmapEntityView) QualifiedName() string { return zeroCopyString(v.e.QualifiedName()) }
func (v mmapEntityView) Subtype() string       { return zeroCopyString(v.e.Subtype()) }
func (v mmapEntityView) SourceFile() string    { return zeroCopyString(v.e.SourceFile()) }
func (v mmapEntityView) Language() string      { return zeroCopyString(v.e.Language()) }
func (v mmapEntityView) Signature() string     { return zeroCopyString(v.e.Signature()) }

// PropGet returns the value for key, or "" if absent — zero-copy. Mirrors
// graph.Entity.PropGet.
func (v mmapEntityView) PropGet(key string) string {
	val, _ := v.PropLookup(key)
	return val
}

// PropLookup returns the value for key and whether present, zero-copy.
//
// Parity with the materialized view (fbEntityToGraphEntity + PropLookup): the
// writer stores `module` as a top-level FB scalar AND — via PropsSnapshot — in
// the property vector, and the loader folds the scalar back in with PropSet
// (scalar wins when present). We mirror that: for "module" the non-empty scalar
// takes precedence; otherwise fall through to the property vector. All other
// keys are a straight vector lookup.
func (v mmapEntityView) PropLookup(key string) (string, bool) {
	if key == "module" {
		if mod := v.e.Module(); len(mod) > 0 {
			return zeroCopyString(mod), true
		}
	}
	return lookupPropertyEntry(key, v.e.PropertiesLength(), func(pe *fb.PropertyEntry, i int) bool {
		return v.e.Properties(pe, i)
	})
}

// PropLen returns the number of properties. Parity: the writer always emits the
// module scalar INTO the property vector too (buildEntity: PropsSnapshot()
// includes "module"), so PropertiesLength already counts it — matching heap,
// where fbEntityToGraphEntity's PropSet("module") updates the existing vector
// entry in place rather than adding one.
func (v mmapEntityView) PropLen() int { return v.e.PropertiesLength() }

// PropRange calls f for every key/value pair in key-sorted order, zero-copy,
// stopping early if f returns false. The FB property vector is written key-sorted
// (fbwriter.buildPropertyVector), and the module fold updates its entry in place,
// so iterating the vector reproduces heap PropRange's order and values exactly.
// The yielded strings alias the mmap and are valid only for the borrow (same
// contract as the cold accessors) — they must not be retained past release().
func (v mmapEntityView) PropRange(f func(k, v string) bool) {
	n := v.e.PropertiesLength()
	var pe fb.PropertyEntry
	for i := 0; i < n; i++ {
		if v.e.Properties(&pe, i) {
			if !f(zeroCopyString(pe.Key()), zeroCopyString(pe.Value())) {
				return
			}
		}
	}
}

// PropsSnapshot returns an independent snapshot map of all properties, HEAP-COPYING
// keys and values so the result is safe to retain past the borrow (matching heap
// PropsSnapshot's owned-string contract) — unlike the zero-copy scalar accessors.
// Parity: returns nil for an empty set (propsToMap does), and folds the module
// scalar in (overriding, matching PropSet) when non-empty.
func (v mmapEntityView) PropsSnapshot() map[string]string {
	n := v.e.PropertiesLength()
	mod := v.e.Module()
	if n == 0 && len(mod) == 0 {
		return nil // parity: PropsSnapshot returns nil, not an empty map
	}
	out := make(map[string]string, n+1)
	var pe fb.PropertyEntry
	for i := 0; i < n; i++ {
		if v.e.Properties(&pe, i) {
			// string(...) copies out of the mmap — safe past release().
			out[string(pe.Key())] = string(pe.Value())
		}
	}
	if len(mod) > 0 {
		out["module"] = string(mod)
	}
	return out
}

// mmapRelationshipView is the F3 zero-copy RelationshipView over one
// *fb.Relationship aliasing the mmap.
type mmapRelationshipView struct{ r *fb.Relationship }

func (v mmapRelationshipView) FromID() string { return zeroCopyString(v.r.FromId()) }
func (v mmapRelationshipView) ToID() string   { return zeroCopyString(v.r.ToId()) }
func (v mmapRelationshipView) Kind() string   { return zeroCopyString(v.r.Kind()) }

// ID mirrors fbRelToGraphRel: the relationship id is tunneled through the "id"
// property (the writer stores it there), restored to rel.ID on load. "" when
// absent.
func (v mmapRelationshipView) ID() string {
	if id, ok := v.PropLookup("id"); ok {
		return id
	}
	return ""
}

// PropGet returns the value for key, or "" if absent — zero-copy. Mirrors
// graph.Relationship.PropGet.
func (v mmapRelationshipView) PropGet(key string) string {
	val, _ := v.PropLookup(key)
	return val
}

// PropLookup returns the value for key and whether present, zero-copy — a straight
// property-vector lookup, matching Relationship.PropLookup.
func (v mmapRelationshipView) PropLookup(key string) (string, bool) {
	return lookupPropertyEntry(key, v.r.PropertiesLength(), func(pe *fb.PropertyEntry, i int) bool {
		return v.r.Properties(pe, i)
	})
}

// PropLen returns the number of properties (incl. the tunneled "id"), matching
// heap Relationship.PropLen (fbRelToGraphRel leaves "id" in the property set).
func (v mmapRelationshipView) PropLen() int { return v.r.PropertiesLength() }

// PropRange calls f for every key/value pair in key-sorted order, zero-copy,
// stopping early if f returns false. Same lifetime contract as the entity view's.
func (v mmapRelationshipView) PropRange(f func(k, v string) bool) {
	n := v.r.PropertiesLength()
	var pe fb.PropertyEntry
	for i := 0; i < n; i++ {
		if v.r.Properties(&pe, i) {
			if !f(zeroCopyString(pe.Key()), zeroCopyString(pe.Value())) {
				return
			}
		}
	}
}

// PropsSnapshot returns an independent snapshot map, HEAP-COPYING keys and values
// so the result survives past the borrow (matching heap PropsSnapshot). Returns
// nil for an empty set. No module fold — relationships have no module scalar.
func (v mmapRelationshipView) PropsSnapshot() map[string]string {
	n := v.r.PropertiesLength()
	if n == 0 {
		return nil
	}
	out := make(map[string]string, n)
	var pe fb.PropertyEntry
	for i := 0; i < n; i++ {
		if v.r.Properties(&pe, i) {
			out[string(pe.Key())] = string(pe.Value())
		}
	}
	return out
}

// lookupPropertyEntry scans an FB PropertyEntry vector for key, returning its
// zero-copy value. O(vector) — cold, run only on result serialization (O(results),
// not O(graph)). Shared by the entity and relationship views.
func lookupPropertyEntry(key string, n int, at func(pe *fb.PropertyEntry, i int) bool) (string, bool) {
	var pe fb.PropertyEntry
	for i := 0; i < n; i++ {
		if at(&pe, i) && zeroCopyString(pe.Key()) == key {
			return zeroCopyString(pe.Value()), true
		}
	}
	return "", false
}

// mmapEntityViewSource is the F3 entityViewSource: it iterates entities from the
// FB reader of the captured handle, yielding mmapEntityViews bound to that
// handle's mapping. buildHotIndex(capturedHandle, mmapEntityViewSource{...})
// then builds the hot index zero-copy, with no heap Document involved.
//
// The handle is the one the owning call captured under s.mu (via groupBorrow);
// every view it yields aliases that handle's mmap and is valid only for the
// borrow — satisfying the read-through-captured-handle invariant.
type mmapEntityViewSource struct{ handle *MapHandle }

func (s mmapEntityViewSource) forEachEntityView(yield func(graph.EntityView)) {
	r := s.handle.Reader()
	if r == nil {
		return
	}
	n := r.EntityCount()
	for i := 0; i < n; i++ {
		// EntityAt allocates a fresh *fb.Entity per call, so each view owns its
		// own wrapper and is safe for the index to retain.
		e := r.EntityAt(i)
		if e == nil {
			continue
		}
		yield(mmapEntityView{e: e})
	}
}

// entityViewSourceFor selects the hot-index build source for repo per the
// GRAFEL_SERVE_FROM_MMAP flag: the mmap-backed source (over the captured handle)
// when ON, else F2's heap-Document source. doc is the repo's captured Doc, used
// only by the OFF path. The choice is per repo load — the same binary can A/B.
func (b *groupBorrow) entityViewSourceFor(repo string, doc *graph.Document) entityViewSource {
	if serveFromMMap() {
		return mmapEntityViewSource{handle: b.Handle(repo)}
	}
	return docEntityViewSource{doc: doc}
}

// Compile-time assertions that the mmap views satisfy the F2 interfaces.
var (
	_ graph.EntityView       = mmapEntityView{}
	_ graph.RelationshipView = mmapRelationshipView{}
	_ entityViewSource       = mmapEntityViewSource{}
)
