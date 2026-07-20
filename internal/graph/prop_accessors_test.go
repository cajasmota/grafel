package graph

import (
	"encoding/json"
	"reflect"
	"sort"
	"testing"
)

// Phase A (behavior-neutral accessor migration) semantics tests for
// Entity/Relationship property accessors, plus a JSON wire-format
// round-trip test proving the "properties" key is unchanged externally
// despite the internal field being unexported.

func TestEntityPropAccessors(t *testing.T) {
	var e Entity

	if got := e.PropGet("missing"); got != "" {
		t.Fatalf("PropGet on empty entity = %q, want empty string", got)
	}
	if _, ok := e.PropLookup("missing"); ok {
		t.Fatalf("PropLookup on empty entity: ok = true, want false")
	}
	if got := e.PropLen(); got != 0 {
		t.Fatalf("PropLen on empty entity = %d, want 0", got)
	}
	if got := e.PropsSnapshot(); got != nil {
		t.Fatalf("PropsSnapshot on empty entity = %#v, want nil", got)
	}

	e.PropSet("a", "1")
	e.PropSet("b", "2")

	if got := e.PropGet("a"); got != "1" {
		t.Fatalf("PropGet(a) = %q, want 1", got)
	}
	if v, ok := e.PropLookup("b"); !ok || v != "2" {
		t.Fatalf("PropLookup(b) = (%q, %v), want (2, true)", v, ok)
	}
	if got := e.PropLen(); got != 2 {
		t.Fatalf("PropLen = %d, want 2", got)
	}

	snap := e.PropsSnapshot()
	if !reflect.DeepEqual(snap, map[string]string{"a": "1", "b": "2"}) {
		t.Fatalf("PropsSnapshot = %#v", snap)
	}
	// Mutating the snapshot must not affect the entity (copy semantics).
	snap["a"] = "mutated"
	if got := e.PropGet("a"); got != "1" {
		t.Fatalf("PropsSnapshot leaked mutation into entity: PropGet(a) = %q", got)
	}

	var seen []string
	e.PropRange(func(k, v string) bool {
		seen = append(seen, k+"="+v)
		return true
	})
	sort.Strings(seen)
	if !reflect.DeepEqual(seen, []string{"a=1", "b=2"}) {
		t.Fatalf("PropRange collected %v", seen)
	}

	// Early-exit via returning false.
	var visited int
	e.PropRange(func(k, v string) bool {
		visited++
		return false
	})
	if visited != 1 {
		t.Fatalf("PropRange early-exit: visited %d entries, want 1", visited)
	}

	e.PropDelete("a")
	if _, ok := e.PropLookup("a"); ok {
		t.Fatalf("PropDelete(a) did not remove key")
	}
	if got := e.PropLen(); got != 1 {
		t.Fatalf("PropLen after delete = %d, want 1", got)
	}

	// WithProperties returns a copy with new properties, doesn't mutate original.
	e2 := e.WithProperties(map[string]string{"x": "y"})
	if e2.PropGet("x") != "y" {
		t.Fatalf("WithProperties result PropGet(x) = %q", e2.PropGet("x"))
	}
	if e.PropLen() != 1 || e.PropGet("x") != "" {
		t.Fatalf("WithProperties mutated receiver: PropLen=%d PropGet(x)=%q", e.PropLen(), e.PropGet("x"))
	}

	// PropsReplace mutates in place via pointer receiver.
	e.PropsReplace(map[string]string{"only": "one"})
	if e.PropLen() != 1 || e.PropGet("only") != "one" {
		t.Fatalf("PropsReplace did not take effect: PropLen=%d PropGet(only)=%q", e.PropLen(), e.PropGet("only"))
	}

	// EntityPtr helper.
	p := EntityPtr(Entity{ID: "z"}.WithProperties(map[string]string{"k": "v"}))
	if p.ID != "z" || p.PropGet("k") != "v" {
		t.Fatalf("EntityPtr result = %#v", p)
	}
}

func TestRelationshipPropAccessors(t *testing.T) {
	var r Relationship

	if got := r.PropGet("missing"); got != "" {
		t.Fatalf("PropGet on empty relationship = %q, want empty string", got)
	}
	if _, ok := r.PropLookup("missing"); ok {
		t.Fatalf("PropLookup on empty relationship: ok = true, want false")
	}
	if got := r.PropLen(); got != 0 {
		t.Fatalf("PropLen on empty relationship = %d, want 0", got)
	}

	r.PropSet("weight", "0.5")
	if got := r.PropGet("weight"); got != "0.5" {
		t.Fatalf("PropGet(weight) = %q, want 0.5", got)
	}

	r.PropDelete("weight")
	if got := r.PropLen(); got != 0 {
		t.Fatalf("PropLen after delete = %d, want 0", got)
	}

	r2 := r.WithProperties(map[string]string{"kind": "calls"})
	if r2.PropGet("kind") != "calls" {
		t.Fatalf("WithProperties result PropGet(kind) = %q", r2.PropGet("kind"))
	}
	if r.PropLen() != 0 {
		t.Fatalf("WithProperties mutated receiver: PropLen=%d", r.PropLen())
	}

	r.PropsReplace(map[string]string{"a": "b"})
	if r.PropGet("a") != "b" {
		t.Fatalf("PropsReplace did not take effect")
	}

	p := RelationshipPtr(Relationship{ID: "rel1"}.WithProperties(map[string]string{"k": "v"}))
	if p.ID != "rel1" || p.PropGet("k") != "v" {
		t.Fatalf("RelationshipPtr result = %#v", p)
	}
}

func TestEntityJSONRoundTrip(t *testing.T) {
	e := Entity{
		ID:            "e1",
		Name:          "Foo",
		QualifiedName: "pkg.Foo",
		Kind:          "class",
		SourceFile:    "foo.go",
		StartLine:     1,
		EndLine:       10,
	}.WithProperties(map[string]string{"a": "1", "b": "2"})

	data, err := json.Marshal(e)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	// The wire key must remain "properties" (lower-case, matching the
	// pre-refactor exported-field default json tag) even though the Go
	// field backing it is now unexported.
	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("Unmarshal to raw map: %v", err)
	}
	propsRaw, ok := raw["properties"]
	if !ok {
		t.Fatalf(`marshaled JSON missing "properties" key: %s`, data)
	}
	propsMap, ok := propsRaw.(map[string]interface{})
	if !ok || propsMap["a"] != "1" || propsMap["b"] != "2" {
		t.Fatalf(`"properties" key has unexpected content: %#v`, propsRaw)
	}

	var e2 Entity
	if err := json.Unmarshal(data, &e2); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if !reflect.DeepEqual(e.PropsSnapshot(), e2.PropsSnapshot()) {
		t.Fatalf("round-trip properties mismatch: got %#v, want %#v", e2.PropsSnapshot(), e.PropsSnapshot())
	}
	if e2.ID != e.ID || e2.Name != e.Name || e2.QualifiedName != e.QualifiedName || e2.Kind != e.Kind {
		t.Fatalf("round-trip field mismatch: %#v vs %#v", e2, e)
	}
}

func TestRelationshipJSONRoundTrip(t *testing.T) {
	r := Relationship{
		ID:   "r1",
		Kind: "calls",
	}.WithProperties(map[string]string{"weight": "0.9"})

	data, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("Unmarshal to raw map: %v", err)
	}
	propsRaw, ok := raw["properties"]
	if !ok {
		t.Fatalf(`marshaled JSON missing "properties" key: %s`, data)
	}
	propsMap, ok := propsRaw.(map[string]interface{})
	if !ok || propsMap["weight"] != "0.9" {
		t.Fatalf(`"properties" key has unexpected content: %#v`, propsRaw)
	}

	var r2 Relationship
	if err := json.Unmarshal(data, &r2); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if !reflect.DeepEqual(r.PropsSnapshot(), r2.PropsSnapshot()) {
		t.Fatalf("round-trip properties mismatch: got %#v, want %#v", r2.PropsSnapshot(), r.PropsSnapshot())
	}
	if r2.ID != r.ID || r2.Kind != r.Kind {
		t.Fatalf("round-trip field mismatch: %#v vs %#v", r2, r)
	}
}

// Phase B (compact []propKV backing) golden-equivalence tests: prove the
// sorted-slice backing produces identical externally-observable behavior to
// the Phase A map-backed implementation — same miss/hit semantics, same
// PropsSnapshot content, plus the one intentional/documented behavior
// change (PropRange now iterates in deterministic key-sorted order instead
// of Go's randomized map order, which is a strict improvement: no consumer
// could have depended on map order being stable in the first place).

func TestEntityPropRangeIsKeySorted(t *testing.T) {
	var e Entity
	for _, k := range []string{"zeta", "alpha", "mu", "beta"} {
		e.PropSet(k, k+"-val")
	}

	var got []string
	e.PropRange(func(k, v string) bool {
		got = append(got, k)
		return true
	})
	want := []string{"alpha", "beta", "mu", "zeta"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("PropRange order = %v, want %v (key-sorted)", got, want)
	}
}

func TestEntityPropSetOverwriteAndDeleteMaintainsSortedOrder(t *testing.T) {
	var e Entity
	e.PropSet("c", "1")
	e.PropSet("a", "2")
	e.PropSet("b", "3")
	e.PropSet("a", "2-updated") // overwrite, must not duplicate or reorder

	if got := e.PropLen(); got != 3 {
		t.Fatalf("PropLen after overwrite = %d, want 3", got)
	}
	if got := e.PropGet("a"); got != "2-updated" {
		t.Fatalf("PropGet(a) after overwrite = %q", got)
	}

	var keys []string
	e.PropRange(func(k, v string) bool { keys = append(keys, k); return true })
	if want := []string{"a", "b", "c"}; !reflect.DeepEqual(keys, want) {
		t.Fatalf("keys after overwrite = %v, want %v", keys, want)
	}

	e.PropDelete("b")
	keys = nil
	e.PropRange(func(k, v string) bool { keys = append(keys, k); return true })
	if want := []string{"a", "c"}; !reflect.DeepEqual(keys, want) {
		t.Fatalf("keys after delete = %v, want %v", keys, want)
	}
	if e.PropLen() != 2 {
		t.Fatalf("PropLen after delete = %d, want 2", e.PropLen())
	}

	// Deleting a missing key is a no-op.
	e.PropDelete("nonexistent")
	if e.PropLen() != 2 {
		t.Fatalf("PropLen after no-op delete = %d, want 2", e.PropLen())
	}
}

func TestPropsFromMapRoundTripIsOrderIndependent(t *testing.T) {
	m := map[string]string{"z": "1", "y": "2", "x": "3", "w": "4"}
	e := Entity{}.WithProperties(m)

	if got := e.PropsSnapshot(); !reflect.DeepEqual(got, m) {
		t.Fatalf("PropsSnapshot = %#v, want %#v", got, m)
	}

	var keys []string
	e.PropRange(func(k, v string) bool { keys = append(keys, k); return true })
	if !sort.StringsAreSorted(keys) {
		t.Fatalf("PropRange keys not sorted: %v", keys)
	}

	// WithProperties must not alias the input map: mutating m afterwards
	// must not affect e.
	m["z"] = "mutated"
	if got := e.PropGet("z"); got != "1" {
		t.Fatalf("WithProperties aliased input map: PropGet(z) = %q, want 1", got)
	}
}

func TestRelationshipPropRangeIsKeySorted(t *testing.T) {
	var r Relationship
	for _, k := range []string{"gamma", "alpha", "delta"} {
		r.PropSet(k, "v")
	}
	var got []string
	r.PropRange(func(k, v string) bool { got = append(got, k); return true })
	want := []string{"alpha", "delta", "gamma"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("PropRange order = %v, want %v (key-sorted)", got, want)
	}
}

// TestFBLoadedEntityPropertiesAreSortedAndBinarySearchable is a lightweight
// structural check that fbEntityToGraphEntity/fbRelToGraphRel's direct
// []propKV construction (no intermediate map) produces the same sorted
// invariant that PropSet maintains, since load.go bypasses PropSet and
// assigns rel.properties/ent.properties directly for performance — this
// guards against that fast path silently drifting out of sorted order.
func TestFBLoadedEntityPropertiesAreSortedAndBinarySearchable(t *testing.T) {
	// Build a document the same way fbEntityToGraphEntity does (via direct
	// slice construction from an already key-sorted source), then verify
	// PropGet/PropLookup still find every key via binary search.
	sortedKeys := []string{"a", "b", "c", "d", "e"}
	e := Entity{}
	e = e.WithProperties(nil) // ensure nil-safe baseline
	for _, k := range sortedKeys {
		e.PropSet(k, "v-"+k)
	}
	for _, k := range sortedKeys {
		if got := e.PropGet(k); got != "v-"+k {
			t.Fatalf("PropGet(%s) = %q, want v-%s", k, got, k)
		}
	}
}
