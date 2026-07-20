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

	got, ok := v.Property("module")
	if !ok || got != "pkg" {
		t.Errorf("Property(module) = (%q, %v), want (\"pkg\", true)", got, ok)
	}
	if _, ok := v.Property("absent"); ok {
		t.Error("Property(absent) reported present")
	}
	if snap := v.Properties(); snap["module"] != "pkg" {
		t.Errorf("Properties()[module] = %q, want %q", snap["module"], "pkg")
	}
}

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
	if got, ok := v.Property("call_site"); !ok || got != "42" {
		t.Errorf("Property(call_site) = (%q, %v), want (\"42\", true)", got, ok)
	}
}

func TestRelationshipViewOfNilIsNil(t *testing.T) {
	if v := graph.RelationshipViewOf(nil); v != nil {
		t.Errorf("RelationshipViewOf(nil) = %v, want nil", v)
	}
}
