package graph_test

import (
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
)

// F2 of ADR-0027: the EntityView / RelationshipView interface seam. These tests
// pin the behavior-neutral contract — the materialized view wrapping a concrete
// *graph.Entity / *graph.Relationship returns exactly the underlying field, so
// F3 can drop in an mmap-backed impl of the SAME interface without any consumer
// change. (The wrapper exists because a struct with exported field `Name` cannot
// also expose a `Name()` method; ADR-0027 blesses "a thin wrapper type".)

func newEntity() *graph.Entity {
	e := &graph.Entity{
		ID:            "repo::pkg.Foo",
		Name:          "Foo",
		QualifiedName: "pkg.Foo",
		Kind:          "type",
		Subtype:       "struct",
		SourceFile:    "pkg/foo.go",
		Language:      "go",
		Signature:     "type Foo struct{}",
	}
	e.PropSet("module", "pkg")
	return e
}

func TestMaterializedEntityViewSatisfiesInterface(t *testing.T) {
	var _ graph.EntityView = graph.EntityViewOf(newEntity())
}

func TestMaterializedEntityViewReturnsUnderlyingFields(t *testing.T) {
	e := newEntity()
	v := graph.EntityViewOf(e)

	if got := v.ID(); got != e.ID {
		t.Errorf("ID() = %q, want %q", got, e.ID)
	}
	if got := v.Name(); got != e.Name {
		t.Errorf("Name() = %q, want %q", got, e.Name)
	}
	if got := v.QualifiedName(); got != e.QualifiedName {
		t.Errorf("QualifiedName() = %q, want %q", got, e.QualifiedName)
	}
	if got := v.Kind(); got != e.Kind {
		t.Errorf("Kind() = %q, want %q", got, e.Kind)
	}
	if got := v.Subtype(); got != e.Subtype {
		t.Errorf("Subtype() = %q, want %q", got, e.Subtype)
	}
	if got := v.SourceFile(); got != e.SourceFile {
		t.Errorf("SourceFile() = %q, want %q", got, e.SourceFile)
	}
	if got := v.Language(); got != e.Language {
		t.Errorf("Language() = %q, want %q", got, e.Language)
	}
	if got := v.Signature(); got != e.Signature {
		t.Errorf("Signature() = %q, want %q", got, e.Signature)
	}
}

func TestMaterializedEntityViewProperties(t *testing.T) {
	v := graph.EntityViewOf(newEntity())

	// W2 (ADR-0027): the view's read-only property surface is now name+signature
	// identical to *graph.Entity's — PropGet/PropLookup/PropLen/PropRange/
	// PropsSnapshot — so a property-reading consumer migrates by type change alone.
	got, ok := v.PropLookup("module")
	if !ok || got != "pkg" {
		t.Errorf("PropLookup(module) = (%q, %v), want (\"pkg\", true)", got, ok)
	}
	if _, ok := v.PropLookup("absent"); ok {
		t.Error("PropLookup(absent) reported present")
	}
	if got := v.PropGet("module"); got != "pkg" {
		t.Errorf("PropGet(module) = %q, want %q", got, "pkg")
	}
	if got := v.PropGet("absent"); got != "" {
		t.Errorf("PropGet(absent) = %q, want \"\"", got)
	}
	if n := v.PropLen(); n != 1 {
		t.Errorf("PropLen() = %d, want 1", n)
	}
	seen := map[string]string{}
	v.PropRange(func(k, val string) bool { seen[k] = val; return true })
	if seen["module"] != "pkg" || len(seen) != 1 {
		t.Errorf("PropRange collected %v, want map[module:pkg]", seen)
	}
	if snap := v.PropsSnapshot(); snap["module"] != "pkg" {
		t.Errorf("PropsSnapshot()[module] = %q, want %q", snap["module"], "pkg")
	}
}

// propReadSurface is EXACTLY graph.Entity's / Relationship's read-only property
// method set (W2, ADR-0027). The assertions below prove two things at compile
// time: (1) *graph.Entity and *graph.Relationship satisfy this surface for free
// (the concrete methods already exist), and (2) EntityView / RelationshipView are
// SUPERSETS of it (assignable to propReadSurface). Together that is the guarantee
// the M-series relies on: a consumer's PropGet/PropLookup/PropLen/PropRange/
// PropsSnapshot calls compile UNCHANGED whether it holds a *graph.Entity or an
// EntityView — migration is a pure type change on the property read path.
type propReadSurface interface {
	PropGet(key string) string
	PropLookup(key string) (string, bool)
	PropLen() int
	PropRange(f func(k, v string) bool)
	PropsSnapshot() map[string]string
}

var (
	_ propReadSurface = (*graph.Entity)(nil)
	_ propReadSurface = (*graph.Relationship)(nil)
	_ propReadSurface = graph.EntityView(nil)
	_ propReadSurface = graph.RelationshipView(nil)
)

func TestEntityViewOfNilIsNil(t *testing.T) {
	if v := graph.EntityViewOf(nil); v != nil {
		t.Errorf("EntityViewOf(nil) = %v, want nil", v)
	}
}

func TestMaterializedRelationshipView(t *testing.T) {
	r := &graph.Relationship{
		ID:     "e1",
		FromID: "repo::a",
		ToID:   "repo::b",
		Kind:   "CALLS",
	}
	r.PropSet("call_site", "42")

	var v graph.RelationshipView = graph.RelationshipViewOf(r)
	if got := v.ID(); got != r.ID {
		t.Errorf("ID() = %q, want %q", got, r.ID)
	}
	if got := v.FromID(); got != r.FromID {
		t.Errorf("FromID() = %q, want %q", got, r.FromID)
	}
	if got := v.ToID(); got != r.ToID {
		t.Errorf("ToID() = %q, want %q", got, r.ToID)
	}
	if got := v.Kind(); got != r.Kind {
		t.Errorf("Kind() = %q, want %q", got, r.Kind)
	}
	if got, ok := v.PropLookup("call_site"); !ok || got != "42" {
		t.Errorf("PropLookup(call_site) = (%q, %v), want (\"42\", true)", got, ok)
	}
	if got := v.PropGet("call_site"); got != "42" {
		t.Errorf("PropGet(call_site) = %q, want %q", got, "42")
	}
	if n := v.PropLen(); n != 1 {
		t.Errorf("PropLen() = %d, want 1", n)
	}
	if snap := v.PropsSnapshot(); snap["call_site"] != "42" {
		t.Errorf("PropsSnapshot()[call_site] = %q, want %q", snap["call_site"], "42")
	}
}

func TestRelationshipViewOfNilIsNil(t *testing.T) {
	if v := graph.RelationshipViewOf(nil); v != nil {
		t.Errorf("RelationshipViewOf(nil) = %v, want nil", v)
	}
}
