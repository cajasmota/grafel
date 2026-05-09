package kotlin_test

import (
	"context"
	"testing"

	"github.com/cajasmota/archigraph/internal/extractor"
	_ "github.com/cajasmota/archigraph/internal/extractors/kotlin"
	"github.com/cajasmota/archigraph/internal/types"
)

func runKotlin(t *testing.T, src string) []types.EntityRecord {
	t.Helper()
	tree := parseForTest(t, src)
	ext, _ := extractor.Get("kotlin")
	ents, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "Test.kt",
		Content:  []byte(src),
		Language: "kotlin",
		Tree:     tree,
	})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	return ents
}

func ktFind(ents []types.EntityRecord, name, kind string) *types.EntityRecord {
	for i := range ents {
		if ents[i].Name == name && ents[i].Kind == kind {
			return &ents[i]
		}
	}
	return nil
}

func ktHasRel(ents []types.EntityRecord, name, kind, edgeKind, toID string) bool {
	e := ktFind(ents, name, kind)
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

// TestKotlin_ContainsClassMethods (#41).
func TestKotlin_ContainsClassMethods(t *testing.T) {
	src := `class Foo {
    fun a() {}
    fun b(x: Int) {}
    fun c() {}
}
`
	ents := runKotlin(t, src)
	foo := ktFind(ents, "Foo", "SCOPE.Component")
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
	// Issue #144 — CONTAINS targets are structural-ref stubs (Format A)
	// keyed on the source file.
	for _, m := range []string{"a", "b", "c"} {
		want := "scope:operation:method:kotlin:Test.kt:" + m
		if !ktHasRel(ents, "Foo", "SCOPE.Component", "CONTAINS", want) {
			t.Errorf("expected CONTAINS Foo→%s", want)
		}
	}
}

// TestKotlin_CallsBareName (#41).
func TestKotlin_CallsBareName(t *testing.T) {
	src := `class A {
    fun caller() {
        helper()
        helper()
        println("x")
    }
    fun helper() {}
}
`
	ents := runKotlin(t, src)
	if !ktHasRel(ents, "caller", "SCOPE.Operation", "CALLS", "helper") {
		t.Errorf("expected CALLS caller→helper")
	}
	caller := ktFind(ents, "caller", "SCOPE.Operation")
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

// TestKotlin_CallsKeywordsFiltered (#106): Kotlin keywords / special
// identifiers (`synchronized`, `it`, `this`, `super`, `lateinit`,
// `by`, `where`) must NOT be emitted as CALLS targets — they are not
// real call sites and the resolver can't bind them to an entity.
func TestKotlin_CallsKeywordsFiltered(t *testing.T) {
	src := `class A {
    fun caller() {
        synchronized(lock) { work() }
        list.forEach { println(it) }
        this.helper()
        super.toString()
    }
    fun helper() {}
}
`
	ents := runKotlin(t, src)
	caller := ktFind(ents, "caller", "SCOPE.Operation")
	if caller == nil {
		t.Fatal("expected caller operation")
	}
	for _, r := range caller.Relationships {
		if r.Kind != "CALLS" {
			continue
		}
		switch r.ToID {
		case "synchronized", "it", "this", "super", "lateinit", "by", "where":
			t.Errorf("kotlin keyword %q must not be emitted as CALLS target", r.ToID)
		}
	}
}

// TestKotlin_NoCallsForBareFieldAccess (#122): tree-sitter-kotlin shapes
// `chat.lastMessages` as a `navigation_expression`, NOT a `call_expression`
// — there's no parenthesized call_suffix. The extractor must not emit any
// CALLS edge for these bare property references; doing so creates
// resolver-unbindable stubs that land in bug-extractor and dominate the
// ktor-samples error rate.
func TestKotlin_NoCallsForBareFieldAccess(t *testing.T) {
	src := `class ChatService {
    val members = mutableListOf<String>()
    val lastMessages = mutableListOf<String>()
    fun caller() {
        members
        lastMessages
        chat.lastMessages
        chat.members.size
        helper()
    }
    fun helper() {}
}
`
	ents := runKotlin(t, src)
	caller := ktFind(ents, "caller", "SCOPE.Operation")
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
			t.Errorf("bare field/property reference %q must not be emitted as CALLS target", r.ToID)
		}
	}
	if !ktHasRel(ents, "caller", "SCOPE.Operation", "CALLS", "helper") {
		t.Error("real method call helper() must still produce CALLS caller→helper")
	}
}

// TestKotlin_NavigationCallTrailingIdentifier (#122): for a navigation
// call like `usersCounter.incrementAndGet()` the CALLS target must be
// the trailing method identifier, not the receiver. The previous
// implementation walked descendants via stack-based DFS (LIFO) which
// returned the leftmost simple_identifier of the receiver chain (e.g.
// `usersCounter`, `chat`, `members`), producing same-class field-access
// false positives.
func TestKotlin_NavigationCallTrailingIdentifier(t *testing.T) {
	src := `class S {
    val usersCounter = AtomicInteger()
    fun caller() {
        usersCounter.incrementAndGet()
        chat.lastMessages.add("x")
        a.b.c.d()
    }
}
`
	ents := runKotlin(t, src)
	caller := ktFind(ents, "caller", "SCOPE.Operation")
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

// TestKotlin_NoImports (#41): kotlin extractor intentionally does
// NOT emit IMPORTS edges (Python parity). Guard against future regressions
// that re-introduce ghost "org" / "com" / "java" entities.
func TestKotlin_NoImports(t *testing.T) {
	src := `package x
import kotlin.io.println
class A
`
	ents := runKotlin(t, src)
	for _, e := range ents {
		for _, r := range e.Relationships {
			if r.Kind == "IMPORTS" {
				t.Errorf("kotlin extractor should not emit IMPORTS, got %+v on %s", r, e.Name)
			}
		}
	}
}
