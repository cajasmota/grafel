package groovy_test

import (
	"context"
	"testing"

	"github.com/cajasmota/grafel/internal/extractor"
	_ "github.com/cajasmota/grafel/internal/extractors/groovy"
	"github.com/cajasmota/grafel/internal/types"
)

// runGroovy parses src with the real groovy grammar and returns extracted entities.
func runGroovy(t *testing.T, src string) []types.EntityRecord {
	t.Helper()
	tree := parseForTest(t, src)
	ext, _ := extractor.Get("groovy")
	ents, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "Test.groovy",
		Content:  []byte(src),
		Language: "groovy",
		TSTree:   tree,
	})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	return ents
}

func gFind(ents []types.EntityRecord, name, kind string) *types.EntityRecord {
	for i := range ents {
		if ents[i].Name == name && ents[i].Kind == kind {
			return &ents[i]
		}
	}
	return nil
}

func gHasRel(ents []types.EntityRecord, name, kind, edgeKind, toID string) bool {
	e := gFind(ents, name, kind)
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

// TestGroovy_ContainsClassMethods (#372): class with N methods → N
// CONTAINS edges keyed on the canonical Format A structural-ref.
func TestGroovy_ContainsClassMethods(t *testing.T) {
	src := `class Foo {
    def a() {}
    def b(int x) {}
    def c() {}
}
`
	ents := runGroovy(t, src)
	foo := gFind(ents, "Foo", "SCOPE.Component")
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
		want := "scope:operation:method:groovy:Test.groovy:" + m
		if !gHasRel(ents, "Foo", "SCOPE.Component", "CONTAINS", want) {
			t.Errorf("expected CONTAINS Foo→%s", want)
		}
	}
}

// TestGroovy_CallsBareName (#372): bare-name method invocation in a
// function body emits a CALLS edge with the bare callee name. Dedup
// is applied per (target).
func TestGroovy_CallsBareName(t *testing.T) {
	src := `class A {
    def caller() {
        helper()
        helper()
        println("x")
    }
    def helper() {}
}
`
	ents := runGroovy(t, src)
	if !gHasRel(ents, "caller", "SCOPE.Operation", "CALLS", "helper") {
		t.Errorf("expected CALLS caller→helper")
	}
	if !gHasRel(ents, "caller", "SCOPE.Operation", "CALLS", "println") {
		t.Errorf("expected CALLS caller→println")
	}
	caller := gFind(ents, "caller", "SCOPE.Operation")
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

// TestGroovy_CallsDottedReceiver (#372): method invocation through a
// dotted receiver `obj.method()` emits the bare leaf method name. When
// the receiver looks like a PascalCase type (`Module.method`), the
// target is emitted as the dotted "<Type>.<method>" form.
func TestGroovy_CallsDottedReceiver(t *testing.T) {
	src := `class C {
    def f() {
        repo.find()
        Service.run()
    }
}
`
	ents := runGroovy(t, src)
	caller := gFind(ents, "f", "SCOPE.Operation")
	if caller == nil {
		t.Fatal("expected operation f")
	}
	wantDotted := "Service.run"
	wantBare := "find"
	gotDotted := false
	gotBare := false
	for _, r := range caller.Relationships {
		if r.Kind != "CALLS" {
			continue
		}
		if r.ToID == wantDotted {
			gotDotted = true
			if r.Properties["receiver_type"] != "Service" {
				t.Errorf("Service.run receiver_type=%q want Service", r.Properties["receiver_type"])
			}
		}
		if r.ToID == wantBare {
			gotBare = true
		}
	}
	if !gotDotted {
		t.Errorf("expected CALLS f→Service.run")
	}
	if !gotBare {
		t.Errorf("expected CALLS f→find")
	}
}

// TestGroovy_CallsKeywordsFiltered (#372): groovy keywords / special
// identifiers must NOT be emitted as CALLS targets.
func TestGroovy_CallsKeywordsFiltered(t *testing.T) {
	src := `class A {
    def caller() {
        helper()
    }
    def helper() {}
}
`
	ents := runGroovy(t, src)
	caller := gFind(ents, "caller", "SCOPE.Operation")
	if caller == nil {
		t.Fatal("expected caller operation")
	}
	for _, r := range caller.Relationships {
		if r.Kind != "CALLS" {
			continue
		}
		switch r.ToID {
		case "this", "super", "new":
			t.Errorf("groovy keyword %q must not be emitted as CALLS target", r.ToID)
		}
	}
}

// TestGroovy_CallsSelfRecursionDropped (#372): a function calling itself
// must not emit a CALLS edge to itself (matches java/scala semantics).
func TestGroovy_CallsSelfRecursionDropped(t *testing.T) {
	src := `class A {
    def loop() {
        loop()
    }
}
`
	ents := runGroovy(t, src)
	loop := gFind(ents, "loop", "SCOPE.Operation")
	if loop == nil {
		t.Fatal("expected operation loop")
	}
	for _, r := range loop.Relationships {
		if r.Kind == "CALLS" && r.ToID == "loop" {
			t.Errorf("self-recursion CALLS loop→loop must be dropped")
		}
	}
}

// TestGroovy_ImportsCarryProperties (#372): IMPORTS edges carry the
// metadata the cross-file resolver consumes (mirroring Python #93,
// Java #120, Scala #379): local_name, source_module, imported_name,
// wildcard.
func TestGroovy_ImportsCarryProperties(t *testing.T) {
	src := `import foo.Bar
import foo.Baz as Renamed
import foo.helpers.*
import static foo.Util.helper

class A {}
`
	ents := runGroovy(t, src)

	want := map[string]map[string]string{
		"foo.Bar": {
			"local_name":    "Bar",
			"source_module": "foo",
			"imported_name": "Bar",
		},
		"foo.Baz": {
			"local_name":    "Renamed",
			"source_module": "foo",
			"imported_name": "Baz",
		},
		"foo.Util.helper": {
			"local_name":    "helper",
			"source_module": "foo.Util",
			"imported_name": "helper",
		},
	}

	got := map[string]map[string]string{}
	wildcardSeen := false
	for _, e := range ents {
		for _, r := range e.Relationships {
			if r.Kind != "IMPORTS" {
				continue
			}
			got[r.ToID] = r.Properties
			if r.Properties["wildcard"] == "1" && r.ToID == "foo.helpers" {
				wildcardSeen = true
			}
		}
	}
	for to, wantProps := range want {
		gotProps, ok := got[to]
		if !ok {
			t.Errorf("expected IMPORTS edge to=%q (got=%v)", to, got)
			continue
		}
		for k, v := range wantProps {
			if gotProps[k] != v {
				t.Errorf("IMPORTS to=%q prop %q: got=%q want=%q (all=%v)",
					to, k, gotProps[k], v, gotProps)
			}
		}
	}
	if !wildcardSeen {
		t.Errorf("expected wildcard IMPORTS edge for foo.helpers.*")
	}
}

// TestGroovy_RelationshipsLanguageTagged (#372): every emitted
// relationship must carry Properties["language"]="groovy" so the
// cross-file resolver dispatches the correct dynamic patterns.
func TestGroovy_RelationshipsLanguageTagged(t *testing.T) {
	src := `import foo.Bar

class A {
    def f() {
        helper()
    }
    def helper() {}
}
`
	ents := runGroovy(t, src)
	relCount := 0
	for _, e := range ents {
		for _, r := range e.Relationships {
			relCount++
			if r.Properties["language"] != "groovy" {
				t.Errorf("rel %s→%s missing language=groovy (got=%q)", r.Kind, r.ToID, r.Properties["language"])
			}
		}
	}
	if relCount == 0 {
		t.Errorf("expected at least one relationship; entities=%+v", ents)
	}
}
