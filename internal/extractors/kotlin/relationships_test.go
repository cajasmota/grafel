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

// TestKotlin_ImportsEmittedFullPath locks in the Ktor-verb fix:
// the kotlin extractor emits one SCOPE.Component (Subtype="import")
// per import_header, with Name set to the FULL dotted module path
// (NOT split on '.', so no resurrected `org`/`com`/`java` ghost
// entities that broke parity verdict classification in the original
// #41 reject). Each entity carries one IMPORTS relationship
// FromID=file path, ToID=full dotted module path with the optional
// `.*` wildcard suffix stripped — consumed by the cross-file resolver
// and the external-synthesis pass to gate language-specific allowlists
// (Ktor server DSL HTTP verbs `get/post/put/delete/...` need a real
// `io.ktor.server.*` import to classify, per the chi-router gate
// precision model #131).
func TestKotlin_ImportsEmittedFullPath(t *testing.T) {
	src := `package x
import io.ktor.server.routing.get
import io.ktor.server.routing.*
import kotlin.io.println
class A
`
	ents := runKotlin(t, src)

	// Helper: collect (Name, ToID) tuples for every IMPORTS edge.
	type impEdge struct {
		entName string
		entKind string
		entSub  string
		toID    string
	}
	var imps []impEdge
	for _, e := range ents {
		for _, r := range e.Relationships {
			if r.Kind == "IMPORTS" {
				imps = append(imps, impEdge{
					entName: e.Name, entKind: e.Kind, entSub: e.Subtype, toID: r.ToID,
				})
			}
		}
	}
	// After Track B (analog of #642/#650/#670 for Kotlin) the IMPORTS
	// ToID for known-external Kotlin/JVM packages is rewritten to the
	// `ext:<root>[:<leaf>]` form so the resolver's external-disposition
	// gate classifies them ExternalKnown directly. The entity Name
	// (left-hand key) still carries the original fully-qualified
	// dotted path — the rewrite is on the relationship ToID only.
	want := map[string]string{
		"io.ktor.server.routing.get": "ext:io.ktor:get",
		"io.ktor.server.routing":     "ext:io.ktor",
		"kotlin.io.println":          "ext:kotlin:println",
	}
	if len(imps) != len(want) {
		t.Fatalf("expected %d IMPORTS edges, got %d: %+v", len(want), len(imps), imps)
	}
	for _, e := range imps {
		if e.entKind != "SCOPE.Component" {
			t.Errorf("import entity %q kind=%q, want SCOPE.Component", e.entName, e.entKind)
		}
		if e.entSub != "import" {
			t.Errorf("import entity %q subtype=%q, want \"import\"", e.entName, e.entSub)
		}
		wantTo, ok := want[e.entName]
		if !ok {
			t.Errorf("unexpected import entity name %q", e.entName)
			continue
		}
		if e.toID != wantTo {
			t.Errorf("import %q: ToID=%q, want %q", e.entName, e.toID, wantTo)
		}
	}

	// Ghost-entity guard: no `org` / `com` / `java` / `io` / `kotlin`
	// short-segment SCOPE.Component entities — the original #41 hazard
	// was a split-on-'.' implementation that produced these.
	for _, e := range ents {
		if e.Kind != "SCOPE.Component" {
			continue
		}
		switch e.Name {
		case "org", "com", "java", "io", "kotlin":
			t.Errorf("ghost import entity %q (Kind=%q, Subtype=%q) — "+
				"import Name must be the FULL dotted path, never the first segment",
				e.Name, e.Kind, e.Subtype)
		}
	}
}

// --- Issue #690: Kotlin property CONTAINS emission ---

// TestKotlin_PropertyContains_BareDeclaration (#690): a class with body
// val/var properties must emit SCOPE.Schema/field entities and CONTAINS
// edges from the class.
func TestKotlin_PropertyContains_BareDeclaration(t *testing.T) {
	src := `class UserService {
    val name: String = "default"
    var count: Int = 0
    private val repo: Any = TODO()
    fun find() {}
}
`
	ents := runKotlin(t, src)
	svc := ktFind(ents, "UserService", "SCOPE.Component")
	if svc == nil {
		t.Fatal("expected UserService component")
	}
	// Field entities must exist.
	for _, fn := range []string{"UserService.name", "UserService.count", "UserService.repo"} {
		if ktFind(ents, fn, "SCOPE.Schema") == nil {
			t.Errorf("expected SCOPE.Schema entity %s", fn)
		}
	}
	// CONTAINS edges via structural-ref stubs.
	wantContains := map[string]bool{
		"scope:schema:field:kotlin:Test.kt:UserService.name":      false,
		"scope:schema:field:kotlin:Test.kt:UserService.count":     false,
		"scope:schema:field:kotlin:Test.kt:UserService.repo":      false,
		"scope:operation:method:kotlin:Test.kt:find":              false,
	}
	for _, r := range svc.Relationships {
		if r.Kind == "CONTAINS" {
			if _, ok := wantContains[r.ToID]; ok {
				wantContains[r.ToID] = true
			}
		}
	}
	for stub, seen := range wantContains {
		if !seen {
			t.Errorf("expected CONTAINS UserService→%s (rels=%+v)", stub, svc.Relationships)
		}
	}
}

// TestKotlin_PropertyContains_PrimaryConstructor (#690): data class primary
// constructor val/var parameters must also get CONTAINS edges.
// `data class User(val id: Int, val name: String)` → 2 field entities.
func TestKotlin_PropertyContains_PrimaryConstructor(t *testing.T) {
	src := `data class User(val id: Int, val name: String)
`
	ents := runKotlin(t, src)
	user := ktFind(ents, "User", "SCOPE.Component")
	if user == nil {
		t.Fatal("expected User component")
	}
	for _, field := range []string{"User.id", "User.name"} {
		if ktFind(ents, field, "SCOPE.Schema") == nil {
			t.Errorf("expected SCOPE.Schema entity %s", field)
		}
		want := "scope:schema:field:kotlin:Test.kt:" + field
		if !ktHasRel(ents, "User", "SCOPE.Component", "CONTAINS", want) {
			t.Errorf("expected CONTAINS User→%s (rels=%+v)", want, user.Relationships)
		}
	}
}

// TestKotlin_PropertyContains_PrimaryConstructorPlainParam (#690): a primary
// constructor parameter WITHOUT a val/var binding is NOT a property — it must
// NOT produce a SCOPE.Schema/field entity or a CONTAINS edge.
func TestKotlin_PropertyContains_PrimaryConstructorPlainParam(t *testing.T) {
	src := `class Wrapper(x: Int)
`
	ents := runKotlin(t, src)
	// x is a plain constructor parameter, not a property.
	for _, e := range ents {
		if e.Kind == "SCOPE.Schema" && e.Subtype == "field" {
			t.Errorf("unexpected SCOPE.Schema/field entity %q for plain parameter", e.Name)
		}
	}
}

// TestKotlin_PropertyContains_ObjectDeclaration (#690): Kotlin object
// declarations also emit CONTAINS for their val/var properties.
func TestKotlin_PropertyContains_ObjectDeclaration(t *testing.T) {
	src := `object Config {
    val timeout: Int = 30
    val prefix: String = "app"
    fun load() {}
}
`
	ents := runKotlin(t, src)
	cfg := ktFind(ents, "Config", "SCOPE.Component")
	if cfg == nil {
		t.Fatal("expected Config component")
	}
	for _, field := range []string{"Config.timeout", "Config.prefix"} {
		want := "scope:schema:field:kotlin:Test.kt:" + field
		if !ktHasRel(ents, "Config", "SCOPE.Component", "CONTAINS", want) {
			t.Errorf("expected CONTAINS Config→%s (rels=%+v)", want, cfg.Relationships)
		}
	}
}
