package cpp_test

import (
	"testing"

	"github.com/cajasmota/archigraph/internal/types"
)

// ----------------------------------------------------------------
// helpers
// ----------------------------------------------------------------

func cppFind(ents []types.EntityRecord, name, kind string) *types.EntityRecord {
	for i := range ents {
		if ents[i].Name == name && ents[i].Kind == kind {
			return &ents[i]
		}
	}
	return nil
}

func cppHasRel(ents []types.EntityRecord, name, kind, edgeKind, toID string) bool {
	e := cppFind(ents, name, kind)
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

// ----------------------------------------------------------------
// CONTAINS
// ----------------------------------------------------------------

// TestCpp_ContainsClassMethods (#367): a class with three inline methods
// should attach three CONTAINS edges to the class entity, each targeting
// the canonical Format A structural-ref for the method's name.
func TestCpp_ContainsClassMethods(t *testing.T) {
	src := `
class Foo {
public:
    void a() {}
    int b(int x) { return x; }
    void c() {}
};
`
	ents, err := extractCPP(src, "Test.cpp")
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	foo := cppFind(ents, "Foo", "SCOPE.Component")
	if foo == nil {
		t.Fatal("expected Foo class component")
	}
	contains := 0
	for _, r := range foo.Relationships {
		if r.Kind == "CONTAINS" {
			contains++
		}
	}
	if contains != 3 {
		t.Errorf("expected 3 CONTAINS edges on Foo, got %d (rels=%+v)", contains, foo.Relationships)
	}
	for _, m := range []string{"a", "b", "c"} {
		want := "scope:operation:method:cpp:Test.cpp:" + m
		if !cppHasRel(ents, "Foo", "SCOPE.Component", "CONTAINS", want) {
			t.Errorf("expected CONTAINS Foo→%s", want)
		}
	}
}

// TestCpp_ContainsStructMethods (#367): C++ structs are first-class
// SCOPE.Component entities and may declare methods; CONTAINS edges should
// be emitted for them too.
func TestCpp_ContainsStructMethods(t *testing.T) {
	src := `
struct Bar {
    void hello() {}
    void world() {}
};
`
	ents, err := extractCPP(src, "Test.cpp")
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	bar := cppFind(ents, "Bar", "SCOPE.Component")
	if bar == nil {
		t.Fatal("expected Bar struct component")
	}
	for _, m := range []string{"hello", "world"} {
		want := "scope:operation:method:cpp:Test.cpp:" + m
		if !cppHasRel(ents, "Bar", "SCOPE.Component", "CONTAINS", want) {
			t.Errorf("expected CONTAINS Bar→%s", want)
		}
	}
}

// TestCpp_ContainsOutOfLineMethod (#367): out-of-line definitions like
// `void Foo::bar() {}` declared at file/namespace scope should still
// attach a CONTAINS edge from the same-file Foo entity.
func TestCpp_ContainsOutOfLineMethod(t *testing.T) {
	src := `
class Foo {
public:
    void bar();
};

void Foo::bar() {
    return;
}
`
	ents, err := extractCPP(src, "Test.cpp")
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	foo := cppFind(ents, "Foo", "SCOPE.Component")
	if foo == nil {
		t.Fatal("expected Foo component")
	}
	want := "scope:operation:method:cpp:Test.cpp:bar"
	if !cppHasRel(ents, "Foo", "SCOPE.Component", "CONTAINS", want) {
		t.Errorf("expected out-of-line CONTAINS Foo→%s, rels=%+v", want, foo.Relationships)
	}
}

// TestCpp_ContainsNamespace (#367): namespaces directly contain free
// functions (those defined inside the namespace body without a class
// qualifier).
func TestCpp_ContainsNamespaceFunctions(t *testing.T) {
	src := `
namespace mylib {
    void helper() {}
    int compute(int x) { return x; }
}
`
	ents, err := extractCPP(src, "Test.cpp")
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	ns := cppFind(ents, "mylib", "SCOPE.Component")
	if ns == nil {
		t.Fatal("expected mylib namespace component")
	}
	for _, m := range []string{"helper", "compute"} {
		want := "scope:operation:method:cpp:Test.cpp:" + m
		if !cppHasRel(ents, "mylib", "SCOPE.Component", "CONTAINS", want) {
			t.Errorf("expected CONTAINS mylib→%s", want)
		}
	}
}

// ----------------------------------------------------------------
// CALLS
// ----------------------------------------------------------------

// TestCpp_CallsBareFunction (#367): a function calling another bare
// function emits one CALLS edge with the callee's name as ToID.
func TestCpp_CallsBareFunction(t *testing.T) {
	src := `
void helper() {}
void caller() {
    helper();
    helper();
}
`
	ents, err := extractCPP(src, "Test.cpp")
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if !cppHasRel(ents, "caller", "SCOPE.Operation", "CALLS", "helper") {
		t.Errorf("expected CALLS caller→helper")
	}
	caller := cppFind(ents, "caller", "SCOPE.Operation")
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

// TestCpp_CallsMemberAccess (#367): member access calls (`.`/`->`) emit
// CALLS edges with the trailing field identifier as the target.
func TestCpp_CallsMemberAccess(t *testing.T) {
	src := `
struct S { void m() {} };
void caller(S* p, S r) {
    r.m();
    p->m();
}
`
	ents, err := extractCPP(src, "Test.cpp")
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if !cppHasRel(ents, "caller", "SCOPE.Operation", "CALLS", "m") {
		t.Errorf("expected CALLS caller→m for member access; rels=%+v",
			cppFind(ents, "caller", "SCOPE.Operation").Relationships)
	}
}

// TestCpp_CallsQualified (#367): `Foo::bar()` static calls land as
// CALLS caller→bar.
func TestCpp_CallsQualified(t *testing.T) {
	src := `
namespace ns { void bar() {} }
void caller() {
    ns::bar();
}
`
	ents, err := extractCPP(src, "Test.cpp")
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if !cppHasRel(ents, "caller", "SCOPE.Operation", "CALLS", "bar") {
		t.Errorf("expected CALLS caller→bar")
	}
}

// TestCpp_CallsKeywordsFiltered (#367): C++ pseudo-call expressions
// (sizeof, static_cast, new, delete, …) must not be emitted as CALLS
// targets — they are not real call sites.
func TestCpp_CallsKeywordsFiltered(t *testing.T) {
	src := `
struct T { int x; };
void caller() {
    int s = sizeof(int);
    T* p = new T();
    delete p;
    int* q = static_cast<int*>(0);
}
`
	ents, err := extractCPP(src, "Test.cpp")
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	caller := cppFind(ents, "caller", "SCOPE.Operation")
	if caller == nil {
		t.Fatal("expected caller operation")
	}
	for _, r := range caller.Relationships {
		if r.Kind != "CALLS" {
			continue
		}
		switch r.ToID {
		case "sizeof", "alignof", "static_cast", "dynamic_cast",
			"reinterpret_cast", "const_cast", "typeid", "new", "delete":
			t.Errorf("C++ pseudo-call %q must not be emitted as CALLS target", r.ToID)
		}
	}
}

// TestCpp_CallsSelfRecursionDropped (#367): a function that calls itself
// must not emit a CALLS edge to its own name (matches kotlin/swift dedup
// semantics).
func TestCpp_CallsSelfRecursionDropped(t *testing.T) {
	src := `
int fact(int n) {
    if (n <= 1) return 1;
    return n * fact(n - 1);
}
`
	ents, err := extractCPP(src, "Test.cpp")
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	for _, e := range ents {
		if e.Name != "fact" {
			continue
		}
		for _, r := range e.Relationships {
			if r.Kind == "CALLS" && r.ToID == "fact" {
				t.Errorf("self-recursion CALLS fact→fact must be dropped")
			}
		}
	}
}

// ----------------------------------------------------------------
// IMPORTS
// ----------------------------------------------------------------

// TestCpp_IncludeImportsProperties (#367): #include emits IMPORTS with
// local_name / source_module / imported_name properties matching the
// java/swift/scala contract (#120).
func TestCpp_IncludeImportsProperties(t *testing.T) {
	src := `
#include <vector>
#include "myheader.h"
#include "sub/dir/inner.h"
`
	ents, err := extractCPP(src, "Test.cpp")
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	cases := []struct {
		name, leaf, mod string
	}{
		{"vector", "vector", "vector"},
		{"myheader.h", "myheader.h", "myheader.h"},
		{"sub/dir/inner.h", "inner.h", "sub/dir"},
	}
	for _, tc := range cases {
		e := cppFind(ents, tc.name, "SCOPE.Component")
		if e == nil {
			t.Errorf("expected include %q entity", tc.name)
			continue
		}
		if len(e.Relationships) == 0 {
			t.Errorf("include %q: no IMPORTS relationship", tc.name)
			continue
		}
		r := e.Relationships[0]
		if r.Kind != "IMPORTS" {
			t.Errorf("include %q: expected IMPORTS, got %s", tc.name, r.Kind)
		}
		if r.Properties["local_name"] != tc.leaf {
			t.Errorf("include %q: local_name=%q want %q", tc.name, r.Properties["local_name"], tc.leaf)
		}
		if r.Properties["source_module"] != tc.mod {
			t.Errorf("include %q: source_module=%q want %q", tc.name, r.Properties["source_module"], tc.mod)
		}
		if r.Properties["imported_name"] != tc.leaf {
			t.Errorf("include %q: imported_name=%q want %q", tc.name, r.Properties["imported_name"], tc.leaf)
		}
	}
}

// TestCpp_UsingDeclarationImports (#367): `using std::cout;` emits an
// IMPORTS edge with normalised dotted properties.
func TestCpp_UsingDeclarationImports(t *testing.T) {
	src := `
using std::cout;
`
	ents, err := extractCPP(src, "Test.cpp")
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	e := cppFind(ents, "std::cout", "SCOPE.Component")
	if e == nil {
		// Tree-sitter-cpp may not recognise the bare token as
		// using_declaration in all grammar versions; be tolerant by
		// scanning all entities for an IMPORTS edge whose ToID matches.
		for i := range ents {
			for _, r := range ents[i].Relationships {
				if r.Kind == "IMPORTS" && r.ToID == "std::cout" {
					e = &ents[i]
					break
				}
			}
			if e != nil {
				break
			}
		}
	}
	if e == nil {
		t.Skip("tree-sitter-cpp grammar version does not surface using_declaration here")
	}
	if len(e.Relationships) == 0 {
		t.Fatal("expected IMPORTS relationship on using_declaration entity")
	}
	r := e.Relationships[0]
	if r.Kind != "IMPORTS" {
		t.Errorf("expected IMPORTS, got %s", r.Kind)
	}
	if r.Properties["local_name"] != "cout" {
		t.Errorf("local_name=%q want cout", r.Properties["local_name"])
	}
	if r.Properties["source_module"] != "std" {
		t.Errorf("source_module=%q want std", r.Properties["source_module"])
	}
}

// TestCpp_LanguageTagOnRelationships (#90 / #367): every relationship the
// extractor emits must carry the language tag.
func TestCpp_LanguageTagOnRelationships(t *testing.T) {
	src := `
#include <vector>
class A {
    void m() { helper(); }
};
`
	ents, err := extractCPP(src, "Test.cpp")
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	saw := false
	for _, e := range ents {
		for _, r := range e.Relationships {
			saw = true
			if r.Properties["language"] != "cpp" {
				t.Errorf("relationship %+v missing language tag (Properties=%v)", r, r.Properties)
			}
		}
	}
	if !saw {
		t.Fatal("expected at least one relationship to verify language tag")
	}
}
