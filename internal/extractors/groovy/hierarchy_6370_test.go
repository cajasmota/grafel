package groovy_test

import (
	"context"
	"os"
	"sort"
	"testing"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/resolve"
	"github.com/cajasmota/grafel/internal/types"
)

// #6370 — Groovy emitted NO EXTENDS/IMPLEMENTS edge at all: it is absent from
// `cross/hierarchy`'s supportedLanguages AND its own extractor never looked at
// the `extends`/`implements` clause. A user asking "what extends this type"
// got an empty answer indistinguishable from "nothing does".
//
// The edges are emitted from THIS extractor rather than by registering groovy
// in cross/hierarchy, because that pass mints its own SCOPE.Component per class
// AND per parent (extractor.go:565-579), and buildClass already emits one
// SCOPE.Component per groovy class — registering there would duplicate every
// type. TestGroovyHierarchy_NoDuplicateComponents is the standing guard on that.

// hierTargets returns the sorted ToIDs of the edges of `edgeKind` embedded on
// the SCOPE.Component named `owner`, and fails if any carries a non-empty
// FromID (a file-anchored relationship — see file_anchored_rels_guard_test.go
// and #6295/#6298/#6365/#6367).
func hierTargets(t *testing.T, ents []types.EntityRecord, owner, edgeKind string) []string {
	t.Helper()
	e := gFind(ents, owner, "SCOPE.Component")
	if e == nil {
		t.Fatalf("no SCOPE.Component named %q; got %v", owner, componentNames(ents))
	}
	var out []string
	for _, r := range e.Relationships {
		if r.Kind != edgeKind {
			continue
		}
		if r.FromID != "" {
			t.Errorf("%s %s -> %s has FromID=%q, want empty so assembly anchors it on the TYPE",
				owner, edgeKind, r.ToID, r.FromID)
		}
		out = append(out, r.ToID)
	}
	sort.Strings(out)
	return out
}

func componentNames(ents []types.EntityRecord) []string {
	var out []string
	for _, e := range ents {
		if e.Kind == "SCOPE.Component" && e.Subtype == "class" {
			out = append(out, e.Name)
		}
	}
	sort.Strings(out)
	return out
}

func eq(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestGroovyHierarchy_Extends(t *testing.T) {
	ents := runGroovy(t, `class Child extends Parent {
    def hi() { }
}
`)
	if got := hierTargets(t, ents, "Child", "EXTENDS"); !eq(got, []string{"Parent"}) {
		t.Fatalf("Child EXTENDS = %v, want [Parent]", got)
	}
	if got := hierTargets(t, ents, "Child", "IMPLEMENTS"); len(got) != 0 {
		t.Errorf("Child IMPLEMENTS = %v, want none", got)
	}
}

func TestGroovyHierarchy_Implements(t *testing.T) {
	ents := runGroovy(t, `class Impl implements Iface1, Iface2 {
}
`)
	if got := hierTargets(t, ents, "Impl", "IMPLEMENTS"); !eq(got, []string{"Iface1", "Iface2"}) {
		t.Fatalf("Impl IMPLEMENTS = %v, want [Iface1 Iface2]", got)
	}
}

func TestGroovyHierarchy_ExtendsAndImplements(t *testing.T) {
	ents := runGroovy(t, `class Both extends Base implements IOne, ITwo {
}
`)
	if got := hierTargets(t, ents, "Both", "EXTENDS"); !eq(got, []string{"Base"}) {
		t.Errorf("Both EXTENDS = %v, want [Base]", got)
	}
	if got := hierTargets(t, ents, "Both", "IMPLEMENTS"); !eq(got, []string{"IOne", "ITwo"}) {
		t.Errorf("Both IMPLEMENTS = %v, want [IOne ITwo]", got)
	}
}

// Generic arguments are erased from the target: `RestfulController<Post>` binds
// to the declared type `RestfulController`, never to a per-instantiation name.
func TestGroovyHierarchy_GenericsErased(t *testing.T) {
	ents := runGroovy(t, `class PostController extends RestfulController<Post> implements Mixin<A, B>, Plain {
}
`)
	if got := hierTargets(t, ents, "PostController", "EXTENDS"); !eq(got, []string{"RestfulController"}) {
		t.Errorf("EXTENDS = %v, want [RestfulController]", got)
	}
	if got := hierTargets(t, ents, "PostController", "IMPLEMENTS"); !eq(got, []string{"Mixin", "Plain"}) {
		t.Errorf("IMPLEMENTS = %v, want [Mixin Plain]", got)
	}
}

func TestGroovyHierarchy_InterfaceExtends(t *testing.T) {
	ents := runGroovy(t, `interface IThing extends IOther {
}
`)
	if got := hierTargets(t, ents, "IThing", "EXTENDS"); !eq(got, []string{"IOther"}) {
		t.Fatalf("IThing EXTENDS = %v, want [IOther]", got)
	}
}

// A type with no inheritance clause must emit no hierarchy edge — an EXTENDS
// scan that fires on every class is as wrong as one that never fires.
func TestGroovyHierarchy_PlainClassHasNoEdges(t *testing.T) {
	ents := runGroovy(t, `class Plain {
    def hi() { }
}
`)
	e := gFind(ents, "Plain", "SCOPE.Component")
	if e == nil {
		t.Fatal("no SCOPE.Component named Plain")
	}
	for _, r := range e.Relationships {
		if r.Kind == "EXTENDS" || r.Kind == "IMPLEMENTS" {
			t.Errorf("Plain has unexpected %s -> %s", r.Kind, r.ToID)
		}
	}
}

// The word `extends` inside a comment or a string literal is not an
// inheritance clause.
//
// The two occurrences that matter sit INSIDE the header — between the
// declaration keyword and the body's opening brace — because that is the only
// span the scan reads. A comment above the declaration or a string in the body
// is outside it and would be ignored by a scan that did no scrubbing at all;
// asserting only on those makes the scrub untested (a "disable scrubGroovy"
// mutant survived the earlier version of this test).
func TestGroovyHierarchy_CommentsAndStringsIgnored(t *testing.T) {
	ents := runGroovy(t, `// class Ghost extends Phantom
@Grab('org.x:implements Spectre')
class Real /* extends Phantom */ extends Base {
    def s = "class Other extends Wraith"
}
`)
	if got := hierTargets(t, ents, "Real", "EXTENDS"); !eq(got, []string{"Base"}) {
		t.Errorf("Real EXTENDS = %v, want [Base] (comment/annotation-string text is not a clause)", got)
	}
	for _, e := range ents {
		for _, r := range e.Relationships {
			if r.Kind != "EXTENDS" && r.Kind != "IMPLEMENTS" {
				continue
			}
			switch r.ToID {
			case "Phantom", "Ghost", "Spectre", "Wraith":
				t.Errorf("%s: %s -> %s came from a comment or string literal", e.Name, r.Kind, r.ToID)
			}
		}
	}
}

// A self-edge is never information — it is what a mis-attributed owner looks
// like (#6369), and a hierarchy walk that follows one loops. `class Foo extends
// Foo` does not compile in Groovy, but the extractor sees text, not a compiler,
// and must not mint the loop.
func TestGroovyHierarchy_SelfReferenceDropped(t *testing.T) {
	ents := runGroovy(t, `class Foo extends Foo implements Foo {
}
`)
	if got := hierTargets(t, ents, "Foo", "EXTENDS"); len(got) != 0 {
		t.Errorf("Foo EXTENDS = %v, want none", got)
	}
	if got := hierTargets(t, ents, "Foo", "IMPLEMENTS"); len(got) != 0 {
		t.Errorf("Foo IMPLEMENTS = %v, want none", got)
	}
}

// THE failure mode of the rejected route. Registering groovy in
// cross/hierarchy would mint a SCOPE.Component for the class AND one for each
// parent, on top of the ones buildClass already emits. Exactly one class
// component per DECLARED class, and never one named after a base type.
func TestGroovyHierarchy_NoDuplicateComponents(t *testing.T) {
	ents := runGroovy(t, `class Child extends Parent implements Iface {
}

class Sibling extends Parent {
}
`)
	got := componentNames(ents)
	want := []string{"Child", "Sibling"}
	if !eq(got, want) {
		t.Fatalf("class components = %v, want %v (a base type must never become a component here)", got, want)
	}
}

// Real tree, per AGENTS.md ## Evidence: the checked-in Spock fixture carries
// both clauses on one declaration, with a generic interface argument.
func TestGroovyHierarchy_RealWorldSpockFixture(t *testing.T) {
	src, err := os.ReadFile("../../../testdata/fixtures/real-world/groovy/spock_spec.groovy")
	if err != nil {
		t.Skipf("fixture unavailable: %v", err)
	}
	ents := runGroovy(t, string(src))
	if got := hierTargets(t, ents, "PostServiceSpec", "EXTENDS"); !eq(got, []string{"Specification"}) {
		t.Errorf("PostServiceSpec EXTENDS = %v, want [Specification]", got)
	}
	if got := hierTargets(t, ents, "PostServiceSpec", "IMPLEMENTS"); !eq(got, []string{"DataTest", "ServiceUnitTest"}) {
		t.Errorf("PostServiceSpec IMPLEMENTS = %v, want [DataTest ServiceUnitTest]", got)
	}
}

// AGENTS.md ## Evidence — endpoints, not counts (#6369 is the standing example
// of edges that exist, resolve, and point at the wrong node). Two files, pushed
// through the production resolver pipeline (ResolveImports →
// ReferencesEmbedded) exactly as graph assembly does.
//
// BEFORE the fix this test could not be written at all: PostController carried
// zero EXTENDS/IMPLEMENTS relationships, so there was no endpoint to measure.
// AFTER, both edges resolve to real entities in the other file:
//
//	FROM=PostController@app/PostController.groovy EXTENDS    TO=RestfulController@app/RestfulController.groovy
//	FROM=PostController@app/PostController.groovy IMPLEMENTS TO=Auditable@app/Auditable.groovy
func TestGroovyHierarchy_ResolvedEndpoints(t *testing.T) {
	const ownerPath = "app/PostController.groovy"
	files := map[string]string{
		ownerPath:                      "class PostController extends RestfulController<Post> implements Auditable {\n    def index() { }\n}\n",
		"app/RestfulController.groovy": "class RestfulController {\n}\n",
		"app/Auditable.groovy":         "interface Auditable {\n}\n",
	}

	var recs []types.EntityRecord
	for _, p := range []string{"app/Auditable.groovy", ownerPath, "app/RestfulController.groovy"} {
		tree := parseForTest(t, files[p])
		ext, _ := extractor.Get("groovy")
		ents, err := ext.Extract(context.Background(), extractor.FileInput{
			Path: p, Content: []byte(files[p]), Language: "groovy", TSTree: tree,
		})
		if err != nil {
			t.Fatalf("Extract %s: %v", p, err)
		}
		recs = append(recs, ents...)
	}
	for i := range recs {
		if recs[i].Name == "" {
			continue
		}
		recs[i].ID = graph.EntityID("issue6370", recs[i].Kind, recs[i].Name, recs[i].SourceFile)
	}

	resolve.ResolveImports(recs, resolve.BuildImportTable(recs))
	resolve.ReferencesEmbedded(recs, resolve.BuildIndex(recs))

	byID := make(map[string]*types.EntityRecord, len(recs))
	for i := range recs {
		byID[recs[i].ID] = &recs[i]
	}
	endpoint := func(id string) string {
		if e := byID[id]; e != nil {
			return e.Name + "@" + e.SourceFile
		}
		return "<UNRESOLVED:" + id + ">"
	}

	var owner *types.EntityRecord
	for i := range recs {
		if recs[i].Name == "PostController" && recs[i].SourceFile == ownerPath {
			owner = &recs[i]
		}
	}
	if owner == nil {
		t.Fatal("no PostController entity")
	}

	got := map[string]string{}
	for _, r := range owner.Relationships {
		if r.Kind != "EXTENDS" && r.Kind != "IMPLEMENTS" {
			continue
		}
		// Replay graph assembly: the owning record's id is substituted only
		// when FromID is empty (cmd/grafel/index.go,
		// internal/extractors/incremental.go).
		from := r.FromID
		if from == "" {
			from = owner.ID
		}
		if f := endpoint(from); f != "PostController@"+ownerPath {
			t.Errorf("%s: FROM = %s, want PostController@%s (FromID=%q must be empty)",
				r.Kind, f, ownerPath, r.FromID)
		}
		got[r.Kind] = endpoint(r.ToID)
	}

	want := map[string]string{
		"EXTENDS":    "RestfulController@app/RestfulController.groovy",
		"IMPLEMENTS": "Auditable@app/Auditable.groovy",
	}
	for kind, w := range want {
		if got[kind] != w {
			t.Errorf("%s TO = %s, want %s", kind, got[kind], w)
		}
	}
}
