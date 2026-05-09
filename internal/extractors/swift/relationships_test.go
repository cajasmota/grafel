package swift_test

import (
	"context"
	"testing"

	"github.com/cajasmota/archigraph/internal/extractor"
	_ "github.com/cajasmota/archigraph/internal/extractors/swift"
	"github.com/cajasmota/archigraph/internal/types"
)

// runSwift parses src with the real swift grammar and returns extracted entities.
func runSwift(t *testing.T, src string) []types.EntityRecord {
	t.Helper()
	tree := parseForTest(t, src)
	ext, _ := extractor.Get("swift")
	ents, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "Test.swift",
		Content:  []byte(src),
		Language: "swift",
		Tree:     tree,
	})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	return ents
}

func swFind(ents []types.EntityRecord, name, kind string) *types.EntityRecord {
	for i := range ents {
		if ents[i].Name == name && ents[i].Kind == kind {
			return &ents[i]
		}
	}
	return nil
}

func swHasRel(ents []types.EntityRecord, name, kind, edgeKind, toID string) bool {
	e := swFind(ents, name, kind)
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

// TestSwift_ContainsClassMethods (#381): class declarations attach one
// CONTAINS edge per function declared in the body, with structural-ref
// (Format A, #144) shape `scope:operation:method:swift:<file>:<name>`.
func TestSwift_ContainsClassMethods(t *testing.T) {
	src := `class Foo {
    func a() {}
    func b(x: Int) {}
    func c() {}
}
`
	ents := runSwift(t, src)
	foo := swFind(ents, "Foo", "SCOPE.Component")
	if foo == nil {
		t.Fatal("expected Foo component")
	}
	contains := 0
	for _, r := range foo.Relationships {
		if r.Kind == "CONTAINS" {
			contains++
		}
	}
	if contains != 3 {
		t.Errorf("expected 3 CONTAINS edges, got %d (rels=%+v)", contains, foo.Relationships)
	}
	for _, m := range []string{"a", "b", "c"} {
		want := "scope:operation:method:swift:Test.swift:" + m
		if !swHasRel(ents, "Foo", "SCOPE.Component", "CONTAINS", want) {
			t.Errorf("expected CONTAINS Foo→%s", want)
		}
	}
}

// TestSwift_ContainsStructMethods covers struct bodies, which use the same
// `class_declaration`/`class_body` node types in tree-sitter-swift.
func TestSwift_ContainsStructMethods(t *testing.T) {
	src := `struct Point {
    var x: Double
    func translate() {}
    func scale() {}
}
`
	ents := runSwift(t, src)
	pt := swFind(ents, "Point", "SCOPE.Component")
	if pt == nil || pt.Subtype != "struct" {
		t.Fatalf("expected Point struct, got %+v", pt)
	}
	for _, m := range []string{"translate", "scale"} {
		want := "scope:operation:method:swift:Test.swift:" + m
		if !swHasRel(ents, "Point", "SCOPE.Component", "CONTAINS", want) {
			t.Errorf("expected CONTAINS Point→%s", want)
		}
	}
}

// TestSwift_ContainsEnumMethods covers enum bodies, which use
// `enum_class_body` in tree-sitter-swift.
func TestSwift_ContainsEnumMethods(t *testing.T) {
	src := `enum Color {
    case red
    case blue
    func describe() -> String { return "" }
}
`
	ents := runSwift(t, src)
	c := swFind(ents, "Color", "SCOPE.Component")
	if c == nil || c.Subtype != "enum" {
		t.Fatalf("expected Color enum, got %+v", c)
	}
	want := "scope:operation:method:swift:Test.swift:describe"
	if !swHasRel(ents, "Color", "SCOPE.Component", "CONTAINS", want) {
		t.Errorf("expected CONTAINS Color→%s", want)
	}
}

// TestSwift_CallsBareName: bare function calls inside a function body
// produce CALLS edges with the simple identifier as ToID, deduped.
func TestSwift_CallsBareName(t *testing.T) {
	src := `class A {
    func caller() {
        helper()
        helper()
        print("x")
    }
    func helper() {}
}
`
	ents := runSwift(t, src)
	if !swHasRel(ents, "caller", "SCOPE.Operation", "CALLS", "helper") {
		t.Errorf("expected CALLS caller→helper")
	}
	caller := swFind(ents, "caller", "SCOPE.Operation")
	n := 0
	for _, r := range caller.Relationships {
		if r.Kind == "CALLS" && r.ToID == "helper" {
			n++
		}
	}
	if n != 1 {
		t.Errorf("expected dedup CALLS caller→helper to 1, got %d", n)
	}
	if !swHasRel(ents, "caller", "SCOPE.Operation", "CALLS", "print") {
		t.Errorf("expected CALLS caller→print")
	}
}

// TestSwift_CallsKeywordsFiltered: Swift keywords / self-references must
// NOT appear as CALLS targets — they are not real call sites and the
// resolver can't bind them.
func TestSwift_CallsKeywordsFiltered(t *testing.T) {
	src := `class A {
    func caller() {
        self.helper()
        super.toString()
    }
    func helper() {}
}
`
	ents := runSwift(t, src)
	caller := swFind(ents, "caller", "SCOPE.Operation")
	if caller == nil {
		t.Fatal("expected caller operation")
	}
	for _, r := range caller.Relationships {
		if r.Kind != "CALLS" {
			continue
		}
		switch r.ToID {
		case "self", "Self", "super":
			t.Errorf("swift keyword %q must not be emitted as CALLS target", r.ToID)
		}
	}
	// helper() invoked via self.helper() should still produce a CALLS edge.
	if !swHasRel(ents, "caller", "SCOPE.Operation", "CALLS", "helper") {
		t.Error("expected CALLS caller→helper for self.helper()")
	}
	if !swHasRel(ents, "caller", "SCOPE.Operation", "CALLS", "toString") {
		t.Error("expected CALLS caller→toString for super.toString()")
	}
}

// TestSwift_NoCallsForBareFieldAccess: tree-sitter-swift shapes
// `chat.lastMessages` as a `navigation_expression`, NOT a `call_expression`
// — there's no parenthesized call_suffix. The extractor must not emit any
// CALLS edge for these bare property references.
func TestSwift_NoCallsForBareFieldAccess(t *testing.T) {
	src := `class ChatService {
    var members: [String] = []
    var lastMessages: [String] = []
    func caller() {
        _ = members
        _ = lastMessages
        _ = chat.lastMessages
        _ = chat.members.count
        helper()
    }
    func helper() {}
}
`
	ents := runSwift(t, src)
	caller := swFind(ents, "caller", "SCOPE.Operation")
	if caller == nil {
		t.Fatal("expected caller operation")
	}
	forbidden := map[string]bool{
		"members":      true,
		"lastMessages": true,
		"chat":         true,
	}
	for _, r := range caller.Relationships {
		if r.Kind != "CALLS" {
			continue
		}
		if forbidden[r.ToID] {
			t.Errorf("bare property reference %q must not be emitted as CALLS target", r.ToID)
		}
	}
	if !swHasRel(ents, "caller", "SCOPE.Operation", "CALLS", "helper") {
		t.Error("real method call helper() must still produce CALLS caller→helper")
	}
}

// TestSwift_NavigationCallTrailingIdentifier: for a navigation call like
// `usersCounter.incrementAndGet()` the CALLS target must be the trailing
// method identifier, not the receiver root.
func TestSwift_NavigationCallTrailingIdentifier(t *testing.T) {
	src := `class S {
    var usersCounter: AtomicInteger = AtomicInteger()
    func caller() {
        usersCounter.incrementAndGet()
        chat.lastMessages.add("x")
        a.b.c.d()
    }
}
`
	ents := runSwift(t, src)
	caller := swFind(ents, "caller", "SCOPE.Operation")
	if caller == nil {
		t.Fatal("expected caller operation")
	}
	want := map[string]bool{
		"incrementAndGet": false,
		"add":             false,
		"d":               false,
	}
	forbidden := map[string]bool{
		"usersCounter": true,
		"chat":         true,
		"a":            true,
	}
	for _, r := range caller.Relationships {
		if r.Kind != "CALLS" {
			continue
		}
		if forbidden[r.ToID] {
			t.Errorf("receiver root %q must not be emitted as CALLS target", r.ToID)
		}
		if _, ok := want[r.ToID]; ok {
			want[r.ToID] = true
		}
	}
	for k, v := range want {
		if !v {
			t.Errorf("expected CALLS caller→%s", k)
		}
	}
}

// TestSwift_CallsReceiverTypeProperty: when the receiver of a navigation
// call is a same-class field with a declared type, the CALLS edge carries
// Properties["receiver_type"]=<DeclaredType>.
func TestSwift_CallsReceiverTypeProperty(t *testing.T) {
	src := `class S {
    var counter: AtomicInteger = AtomicInteger()
    func caller() {
        counter.incrementAndGet()
    }
}
`
	ents := runSwift(t, src)
	caller := swFind(ents, "caller", "SCOPE.Operation")
	if caller == nil {
		t.Fatal("expected caller operation")
	}
	var found bool
	for _, r := range caller.Relationships {
		if r.Kind == "CALLS" && r.ToID == "incrementAndGet" {
			found = true
			if r.Properties["receiver_type"] != "AtomicInteger" {
				t.Errorf("expected receiver_type=AtomicInteger, got %q (props=%+v)", r.Properties["receiver_type"], r.Properties)
			}
		}
	}
	if !found {
		t.Error("expected CALLS caller→incrementAndGet")
	}
}

// TestSwift_ImportProperties: IMPORTS edges carry the property contract
// (local_name, source_module, imported_name) matching the Java/Python
// schema.
func TestSwift_ImportProperties(t *testing.T) {
	src := `import Foundation
import SwiftUI
`
	ents := runSwift(t, src)
	want := map[string]struct {
		local, mod string
	}{
		"Foundation": {"Foundation", "Foundation"},
		"SwiftUI":    {"SwiftUI", "SwiftUI"},
	}
	seen := map[string]bool{}
	for _, e := range ents {
		for _, r := range e.Relationships {
			if r.Kind != "IMPORTS" {
				continue
			}
			w, ok := want[r.ToID]
			if !ok {
				continue
			}
			seen[r.ToID] = true
			if r.FromID != "Test.swift" {
				t.Errorf("IMPORTS %s: expected FromID=Test.swift, got %q", r.ToID, r.FromID)
			}
			if r.Properties["local_name"] != w.local {
				t.Errorf("IMPORTS %s: local_name=%q want %q", r.ToID, r.Properties["local_name"], w.local)
			}
			if r.Properties["source_module"] != w.mod {
				t.Errorf("IMPORTS %s: source_module=%q want %q", r.ToID, r.Properties["source_module"], w.mod)
			}
			if r.Properties["imported_name"] != w.local {
				t.Errorf("IMPORTS %s: imported_name=%q want %q", r.ToID, r.Properties["imported_name"], w.local)
			}
		}
	}
	for k := range want {
		if !seen[k] {
			t.Errorf("expected IMPORTS edge for %s", k)
		}
	}
}
