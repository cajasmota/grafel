package rust_test

import (
	"context"
	"testing"

	"github.com/cajasmota/grafel/internal/extractor"
	_ "github.com/cajasmota/grafel/internal/extractors/rust"
	"github.com/cajasmota/grafel/internal/types"
)

func runRust(t *testing.T, src string) []types.EntityRecord {
	t.Helper()
	tree := parseForTest(t, src)
	ext, _ := extractor.Get("rust")
	ents, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "test.rs",
		Content:  []byte(src),
		Language: "rust",
		TSTree:   tree,
	})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	return ents
}

func rsFind(ents []types.EntityRecord, name, kind string) *types.EntityRecord {
	for i := range ents {
		if ents[i].Name == name && ents[i].Kind == kind {
			return &ents[i]
		}
	}
	return nil
}

func rsHasRel(ents []types.EntityRecord, name, kind, edgeKind, toID string) bool {
	e := rsFind(ents, name, kind)
	if e == nil {
		return false
	}
	for _, r := range e.Relationships {
		if r.Kind == edgeKind && r.ToID == toID {
			return true
		}
	}
	return false
}

// TestRust_ContainsImplMethods (#41): impl block with N methods → N CONTAINS.
func TestRust_ContainsImplMethods(t *testing.T) {
	src := `struct Foo;
impl Foo {
    fn a(&self) {}
    fn b(&self) {}
    fn c(&self) {}
}
`
	ents := runRust(t, src)
	// Two Foo entities exist: one struct_item, one impl_item — both Components.
	// CONTAINS lives on the impl_item (matched by subtype).
	var impl *types.EntityRecord
	for i := range ents {
		if ents[i].Kind == "SCOPE.Component" && ents[i].Subtype == "impl" && ents[i].Name == "Foo" {
			impl = &ents[i]
			break
		}
	}
	if impl == nil {
		t.Fatal("expected Foo impl component")
	}
	contains := 0
	for _, r := range impl.Relationships {
		if r.Kind == "CONTAINS" {
			contains++
		}
	}
	if contains != 3 {
		t.Errorf("expected 3 CONTAINS edges, got %d (rels=%+v)", contains, impl.Relationships)
	}
	// Issue #144 — CONTAINS targets are structural-ref stubs (Format A)
	// keyed on the source file so impl→method edges disambiguate by location.
	// Issue #615 — impl method names are now qualified as "Foo.a", so the
	// structural ref is "scope:operation:method:rust:test.rs:Foo.a".
	for _, m := range []string{"Foo.a", "Foo.b", "Foo.c"} {
		want := "scope:operation:method:rust:test.rs:" + m
		found := false
		for _, r := range impl.Relationships {
			if r.Kind == "CONTAINS" && r.ToID == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected CONTAINS Foo→%s", want)
		}
	}
}

// TestRust_ContainsTraitMethods (#41): trait with N method signatures → N CONTAINS.
func TestRust_ContainsTraitMethods(t *testing.T) {
	src := `trait Foo {
    fn a(&self) {}
    fn b(&self) {}
}
`
	ents := runRust(t, src)
	foo := rsFind(ents, "Foo", "SCOPE.Component")
	if foo == nil {
		t.Fatal("expected Foo trait component")
	}
	contains := 0
	for _, r := range foo.Relationships {
		if r.Kind == "CONTAINS" {
			contains++
		}
	}
	if contains != 2 {
		t.Errorf("expected 2 CONTAINS edges, got %d", contains)
	}
	// Issue #144 — trait→method CONTAINS edges also use structural-ref stubs.
	for _, m := range []string{"a", "b"} {
		want := "scope:operation:method:rust:test.rs:" + m
		if !rsHasRel(ents, "Foo", "SCOPE.Component", "CONTAINS", want) {
			t.Errorf("expected CONTAINS Foo→%s", want)
		}
	}
}

// TestRust_CallsBareName (#41).
func TestRust_CallsBareName(t *testing.T) {
	src := `fn helper() {}
fn caller() {
    helper();
    helper();
    println!("x");
}
`
	ents := runRust(t, src)
	if !rsHasRel(ents, "caller", "SCOPE.Operation", "CALLS", "helper") {
		t.Errorf("expected CALLS caller→helper")
	}
	if !rsHasRel(ents, "caller", "SCOPE.Operation", "CALLS", "println") {
		t.Errorf("expected CALLS caller→println (macro_invocation)")
	}
	caller := rsFind(ents, "caller", "SCOPE.Operation")
	n := 0
	for _, r := range caller.Relationships {
		if r.Kind == "CALLS" && r.ToID == "helper" {
			n++
		}
	}
	if n != 1 {
		t.Errorf("expected dedup CALLS caller→helper to 1, got %d", n)
	}
}

// TestRust_Imports (#41): use_declaration emits IMPORTS edges (already
// implemented before this fix; assert behaviour is preserved).
func TestRust_Imports(t *testing.T) {
	src := `use std::fmt;
use serde::Deserialize;
fn main() {}
`
	ents := runRust(t, src)
	want := map[string]bool{"std::fmt": false, "serde::Deserialize": false}
	for _, e := range ents {
		for _, r := range e.Relationships {
			if r.Kind == "IMPORTS" {
				if _, ok := want[r.ToID]; ok {
					want[r.ToID] = true
				}
			}
		}
	}
	for k, ok := range want {
		if !ok {
			t.Errorf("expected IMPORTS edge for %q", k)
		}
	}
}
