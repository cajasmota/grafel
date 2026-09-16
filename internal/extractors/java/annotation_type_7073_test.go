package java_test

import (
	"context"
	"sort"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/resolve"
	"github.com/cajasmota/grafel/internal/types"
)

// Issue #7073 — a Java annotation type declaration (`public @interface Audited`)
// minted NO entity at all, so a project's own annotations were structurally
// invisible: nothing to find, nothing to point an edge at.
//
// Grammar evidence (tree-sitter-java CST, dumped from the same ts.Language the
// daemon parses with):
//
//	annotation_type_declaration          name=identifier  body=annotation_type_body
//	  modifiers
//	  @interface
//	  identifier                         "Audited"
//	  annotation_type_body
//	    annotation_type_element_declaration   type=… name=identifier value=…
//
// i.e. the declaration exposes the SAME `name` / `body` fields as
// interface_declaration, and its members are annotation_type_element_declaration
// — NOT method_declaration, which is why nothing downstream ever saw them.
//
// # Fixture axes
//
// javaAnn7073Src varies exactly ONE axis — the declaration keyword
// (`@interface` vs `interface`) — and holds constant: the package, the file,
// the `public` modifier, the member's name (`value`), the member's declared
// type (`String`), the member arity (zero parameters), and the nesting depth
// (both top-level). That is the pair the fix must tell apart; every other axis
// is pinned so a matcher that fired on both would be caught by the negative
// assertions below rather than masked by an unrelated difference.
const javaAnn7073Path = "com/example/audit/Decls.java"

const javaAnn7073Src = `package com.example.audit;

public @interface Audited {
    String value();
    int level() default 0;
}

public interface Plain {
    String value();
}
`

// javaAnn7073NestedSrc varies the OTHER axis — nesting depth — while holding
// the keyword constant at `@interface`. Without this the qualification of a
// nested annotation type (and of its elements) would be ungraded.
const javaAnn7073NestedPath = "com/example/audit/Outer.java"

const javaAnn7073NestedSrc = `package com.example.audit;

public class Outer {
    public @interface Nested {
        String value();
    }
}
`

func extractJava7073(t *testing.T, path, src string) []types.EntityRecord {
	t.Helper()
	tree := parseForTest(t, src)
	ext, ok := extractor.Get("java")
	if !ok {
		t.Fatal("java extractor not registered")
	}
	got, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     path,
		Content:  []byte(src),
		Language: "java",
		TSTree:   tree,
	})
	if err != nil {
		t.Fatalf("extract %s: %v", path, err)
	}
	return got
}

func findJava7073(recs []types.EntityRecord, kind, name string) []types.EntityRecord {
	var out []types.EntityRecord
	for _, e := range recs {
		if e.Kind == kind && e.Name == name {
			out = append(out, e)
		}
	}
	return out
}

func describeJava7073(recs []types.EntityRecord) string {
	var lines []string
	for _, e := range recs {
		lines = append(lines, e.Kind+"|"+e.Subtype+"|"+e.Name)
	}
	sort.Strings(lines)
	return strings.Join(lines, "\n  ")
}

// TestJavaAnnotationType_7073_MintsComponent is the positive direction: the
// annotation type itself must exist as a queryable entity.
func TestJavaAnnotationType_7073_MintsComponent(t *testing.T) {
	recs := extractJava7073(t, javaAnn7073Path, javaAnn7073Src)

	got := findJava7073(recs, "SCOPE.Component", "Audited")
	if len(got) != 1 {
		t.Fatalf("want exactly 1 SCOPE.Component named %q, got %d. All entities:\n  %s",
			"Audited", len(got), describeJava7073(recs))
	}
	a := got[0]
	if a.Subtype != "annotation" {
		t.Errorf("Audited.Subtype = %q, want %q", a.Subtype, "annotation")
	}
	if a.QualifiedName != "com.example.audit.Audited" {
		t.Errorf("Audited.QualifiedName = %q, want %q", a.QualifiedName, "com.example.audit.Audited")
	}
	if a.Language != "java" {
		t.Errorf("Audited.Language = %q, want %q", a.Language, "java")
	}
	if a.SourceFile != javaAnn7073Path {
		t.Errorf("Audited.SourceFile = %q, want %q", a.SourceFile, javaAnn7073Path)
	}
	// Span: the declaration starts at the `public @interface` line (3) and ends
	// at its closing brace (6). A span collapsed onto one line is the signature
	// of a record built from the identifier rather than the declaration node.
	if a.StartLine != 3 || a.EndLine != 6 {
		t.Errorf("Audited span = %d-%d, want 3-6", a.StartLine, a.EndLine)
	}
	if !strings.Contains(a.Signature, "@interface Audited") {
		t.Errorf("Audited.Signature = %q, want it to contain %q", a.Signature, "@interface Audited")
	}
}

// TestJavaAnnotationType_7073_PlainInterfaceUnchanged is the NEGATIVE
// direction. A plain `interface` differs from the annotation type by one
// keyword; a matcher that reached both would swallow the ordinary case. This
// asserts the ordinary case is untouched, in the SAME file, with every other
// axis held constant.
func TestJavaAnnotationType_7073_PlainInterfaceUnchanged(t *testing.T) {
	recs := extractJava7073(t, javaAnn7073Path, javaAnn7073Src)

	got := findJava7073(recs, "SCOPE.Component", "Plain")
	if len(got) != 1 {
		t.Fatalf("want exactly 1 SCOPE.Component named %q, got %d. All entities:\n  %s",
			"Plain", len(got), describeJava7073(recs))
	}
	if sub := got[0].Subtype; sub != "interface" {
		t.Fatalf("Plain.Subtype = %q, want %q — the annotation branch reached the "+
			"ordinary interface", sub, "interface")
	}

	// Forbidden rows. `Plain.value()` is an interface METHOD and must stay a
	// SCOPE.Operation; it must NOT be reshaped into the SCOPE.Schema field the
	// annotation element becomes. This is the assertion the over-broad-matcher
	// mutant has to break.
	if ops := findJava7073(recs, "SCOPE.Operation", "Plain.value"); len(ops) != 1 {
		t.Errorf("want exactly 1 SCOPE.Operation named %q, got %d. All entities:\n  %s",
			"Plain.value", len(ops), describeJava7073(recs))
	} else if ops[0].Subtype != "method" {
		t.Errorf("Plain.value.Subtype = %q, want %q", ops[0].Subtype, "method")
	}
	if f := findJava7073(recs, "SCOPE.Schema", "Plain.value"); len(f) != 0 {
		t.Errorf("forbidden: %q minted a SCOPE.Schema field (%d) — the annotation-element "+
			"branch fired on an interface body", "Plain.value", len(f))
	}
	// And the converse forbidden row: the annotation's element must NOT be
	// minted as a callable operation.
	if ops := findJava7073(recs, "SCOPE.Operation", "Audited.value"); len(ops) != 0 {
		t.Errorf("forbidden: %q minted a SCOPE.Operation (%d) — an annotation element "+
			"is data, not a call target", "Audited.value", len(ops))
	}
}

// TestJavaAnnotationType_7073_Elements grades the members. An annotation
// element is a NAMED, TYPED, DEFAULTABLE attribute — the same shape as a record
// component, which this extractor already models as SCOPE.Schema/field — and it
// is never a call target, so modelling it as SCOPE.Operation would put a
// permanently-uncallable row into the call graph and into every dead-code
// query.
func TestJavaAnnotationType_7073_Elements(t *testing.T) {
	recs := extractJava7073(t, javaAnn7073Path, javaAnn7073Src)

	for _, want := range []struct{ name, sig string }{
		{"Audited.value", "String value()"},
		{"Audited.level", "int level() default 0"},
	} {
		got := findJava7073(recs, "SCOPE.Schema", want.name)
		if len(got) != 1 {
			t.Errorf("want exactly 1 SCOPE.Schema named %q, got %d. All entities:\n  %s",
				want.name, len(got), describeJava7073(recs))
			continue
		}
		if got[0].Subtype != "field" {
			t.Errorf("%s.Subtype = %q, want %q", want.name, got[0].Subtype, "field")
		}
		if got[0].Signature != want.sig {
			t.Errorf("%s.Signature = %q, want %q", want.name, got[0].Signature, want.sig)
		}
	}

	// The declaration must CONTAIN its elements, otherwise they are orphans and
	// "what does @Audited carry?" is unanswerable from the graph.
	decl := findJava7073(recs, "SCOPE.Component", "Audited")
	if len(decl) != 1 {
		t.Fatalf("Audited component missing; entities:\n  %s", describeJava7073(recs))
	}
	contains := map[string]bool{}
	for _, r := range decl[0].Relationships {
		if r.Kind == "CONTAINS" {
			contains[r.ToID] = true
		}
	}
	for _, elem := range []string{"Audited.value", "Audited.level"} {
		want := extractor.BuildSchemaFieldStructuralRef("java", javaAnn7073Path, elem)
		if !contains[want] {
			t.Errorf("Audited has no CONTAINS edge to %q (want ToID %q); edges: %v",
				elem, want, decl[0].Relationships)
		}
	}
}

// TestJavaAnnotationType_7073_Nested varies nesting depth with the keyword held
// constant at `@interface`.
func TestJavaAnnotationType_7073_Nested(t *testing.T) {
	recs := extractJava7073(t, javaAnn7073NestedPath, javaAnn7073NestedSrc)

	got := findJava7073(recs, "SCOPE.Component", "Nested")
	if len(got) != 1 {
		t.Fatalf("want exactly 1 SCOPE.Component named %q, got %d. All entities:\n  %s",
			"Nested", len(got), describeJava7073(recs))
	}
	if got[0].Subtype != "annotation" {
		t.Errorf("Nested.Subtype = %q, want %q", got[0].Subtype, "annotation")
	}
	// Members of a nested type carry only their IMMEDIATE parent (the extractor's
	// documented rule: "the nested class/interface/enum itself stays bare, but
	// its members are qualified by it").
	if f := findJava7073(recs, "SCOPE.Schema", "Nested.value"); len(f) != 1 {
		t.Errorf("want exactly 1 SCOPE.Schema named %q, got %d. All entities:\n  %s",
			"Nested.value", len(f), describeJava7073(recs))
	}
	// The enclosing class must still be a class.
	if c := findJava7073(recs, "SCOPE.Component", "Outer"); len(c) != 1 || c[0].Subtype != "class" {
		t.Errorf("Outer must still be exactly one SCOPE.Component/class; entities:\n  %s",
			describeJava7073(recs))
	}
}

// TestJavaAnnotationType_7073_UsageSiteBinding MEASURES — it does not assume —
// what minting the declaration does for a usage site, and pins BOTH halves of a
// partial result.
//
// Half one: an annotation APPLICATION (`@Audited("x")` written above a
// declaration) emits no edge at all. This extractor reads annotation nodes for
// detection (@Transactional, tracing, Bean Validation) but has never modelled
// annotation USE as a relationship, so minting the declaration cannot make a
// non-existent edge resolve. #7073 does not close that; the assertion fires if
// it ever does, so the scope claim cannot go stale. (Distinct from READING an
// element — `a.value()` — which does emit an edge; see
// TestJavaAnnotationType_7073_ElementIsACallTarget.)
//
// Half two: the `import a.Audited;` edge now RESOLVES onto the new component's
// entity id. The assertion is on the resolved ToID, not on the edge's presence:
// the raw import text is emitted with no reference to any entity and is
// therefore present on main too, so asserting presence would grade nothing.
func TestJavaAnnotationType_7073_UsageSiteBinding(t *testing.T) {
	const declPath = "a/Audited.java"
	const consumerPath = "b/Svc.java"
	recs := extractJavaFT(t, map[string]string{
		declPath: `package a;

public @interface Audited { String value(); }
`,
		// `Ghost` is the SELECTIVITY control: a type that does not exist in the
		// record set. Without it, "the Audited import resolved" is equally
		// explained by the resolver rewriting every import it sees.
		consumerPath: `package b;

import a.Audited;
import a.Ghost;

@Audited("x")
public class Svc {
    @Audited("y")
    public void go() {}
}
`,
	})
	for i := range recs {
		if recs[i].ID == "" {
			recs[i].ID = graph.EntityID("issue7073", recs[i].Kind, recs[i].Name, recs[i].SourceFile)
		}
	}

	decl := findJava7073(recs, "SCOPE.Component", "Audited")
	if len(decl) != 1 {
		t.Fatalf("premise failed: no Audited declaration to bind to; entities:\n  %s",
			describeJava7073(recs))
	}
	declID := decl[0].ID

	// PRE-RESOLUTION control: the edge must currently hold raw import TEXT, not
	// an entity id. This is what makes half two a statement about the resolver
	// binding onto the new entity rather than about the extractor having emitted
	// an id directly.
	rawBefore := map[string]bool{}
	for _, e := range recs {
		if e.SourceFile != consumerPath {
			continue
		}
		for _, r := range e.Relationships {
			if r.Kind == "IMPORTS" {
				rawBefore[r.ToID] = true
			}
		}
	}
	if !rawBefore["a.Audited"] {
		t.Fatalf("premise failed: consumer's pre-resolution IMPORTS targets were %v, "+
			"want the raw text %q — this test grades the resolution step, so the "+
			"unresolved starting state must be the one it claims", keysOf7073(rawBefore), "a.Audited")
	}
	if rawBefore[declID] {
		t.Fatalf("premise failed: the IMPORTS edge already held the entity id before " +
			"resolution, so nothing about binding is graded here")
	}

	idx := resolve.BuildIndex(recs)
	resolve.ReferencesEmbedded(recs, idx)

	var after []string
	for _, e := range recs {
		if e.SourceFile != consumerPath {
			continue
		}
		for _, r := range e.Relationships {
			if r.Kind == "IMPORTS" {
				after = append(after, r.ToID)
			}
		}
	}
	sort.Strings(after)

	bound := false
	ghostStayedRaw := false
	for _, tgt := range after {
		if tgt == declID {
			bound = true
		}
		if tgt == "a.Ghost" {
			ghostStayedRaw = true
		}
	}
	if !bound {
		t.Errorf("the consumer's IMPORTS edge did NOT resolve onto the Audited "+
			"component id %q; resolved targets were %v", declID, after)
	}
	if !ghostStayedRaw {
		t.Errorf("selectivity control failed: the import of a NON-EXISTENT type no "+
			"longer holds its raw text %q (targets %v), so a resolver that rewrote "+
			"every import indiscriminately would also pass the assertion above",
			"a.Ghost", after)
	}

	// Half one — annotation APPLICATION sites still emit no edge naming Audited.
	var annEdges []string
	for _, e := range recs {
		if e.SourceFile != consumerPath || e.Subtype == "file" {
			continue
		}
		for _, r := range e.Relationships {
			if r.Kind == "IMPORTS" {
				continue
			}
			if strings.Contains(r.ToID, "Audited") || r.ToID == declID {
				annEdges = append(annEdges, e.Name+" -["+r.Kind+"]-> "+r.ToID)
			}
		}
	}
	if len(annEdges) != 0 {
		t.Errorf("MEASUREMENT MOVED: @Audited APPLICATION sites now emit %d edge(s): %v.\n"+
			"This pins the honest scope of #7073 — if annotation USE now produces edges, "+
			"update this test and the PR body rather than leaving the claim stale.",
			len(annEdges), annEdges)
	}
}

func keysOf7073(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// TestJavaAnnotationType_7073_ElementIsACallTarget is an ACCEPTED, DOCUMENTED
// MEASUREMENT, and it exists because the first version of this PR carried a
// FALSE universal: "no CALLS edge can ever reach an annotation element".
//
// It is false. Reading an element back is the ordinary reflection idiom, and
// `a.value()` is a genuine invocation — invokeinterface on the annotation proxy
// at runtime. The pre-existing local-variable-receiver machinery (#4682) types
// `a` as `Audited` and emits `Audited.value` as a CALLS target. On main that
// stub was harmlessly unmatched because NO entity of that name existed; minting
// the declaration turns it into a bound edge whose target is a
// SCOPE.Schema/field.
//
// Accepted rather than forbidden, deliberately:
//
//   - The edge is semantically RIGHT. "Who reads the `value` element of
//     @Audited" is a query a consumer wants, and this is the only edge that
//     answers it.
//   - A forbidden row would have to suppress the stub, and could only ever do
//     so when the annotation is declared in the SAME FILE as the reader. The
//     dominant real shape has the annotation in another file, where the
//     extractor cannot know the receiver's type is an annotation at all. A
//     guard that holds in the graded case and is structurally absent in the
//     common one is worse than none.
//   - No production consumer keys a CALLS edge on its TARGET's Kind (swept:
//     the CALLS readers in internal/mcp, internal/docgen and internal/links
//     filter on edge kind, never on target kind).
//
// So it is pinned as a measurement instead: the edge, its binding, and the KIND
// it lands on are all asserted, and any of the three changing breaks this test
// rather than drifting silently.
func TestJavaAnnotationType_7073_ElementIsACallTarget(t *testing.T) {
	recs := extractJavaFT(t, map[string]string{"App.java": `public @interface Audited { String value(); }
public class Reader {
    public String read(Class<?> c) {
        Audited a = (Audited) c.getAnnotation(Audited.class);
        return a.value();
    }
}
`})
	byID := map[string]string{}
	for i := range recs {
		if recs[i].ID == "" {
			recs[i].ID = graph.EntityID("issue7073", recs[i].Kind, recs[i].Name, recs[i].SourceFile)
		}
		byID[recs[i].ID] = recs[i].Kind + "|" + recs[i].Subtype + "|" + recs[i].Name
	}

	// Located by NAME ONLY, across every kind. Looking it up as a SCOPE.Schema
	// would make a change of the element's kind fail this test as a *premise*
	// failure before reaching the kind assertion below — i.e. the kind claim
	// would be ungraded by its own test.
	var elem *types.EntityRecord
	for i := range recs {
		if recs[i].Name == "Audited.value" {
			if elem != nil {
				t.Fatalf("premise failed: %q is minted more than once, so "+
					"\"the CALLS edge lands on it\" is ambiguous; entities:\n  %s",
					"Audited.value", describeJava7073(recs))
			}
			elem = &recs[i]
		}
	}
	if elem == nil {
		t.Fatalf("premise failed: no Audited.value element; entities:\n  %s",
			describeJava7073(recs))
	}

	idx := resolve.BuildIndex(recs)
	resolve.ReferencesEmbedded(recs, idx)

	var reader *types.EntityRecord
	for i := range recs {
		if recs[i].Kind == "SCOPE.Operation" && recs[i].Name == "Reader.read" {
			reader = &recs[i]
		}
	}
	if reader == nil {
		t.Fatalf("premise failed: no Reader.read operation; entities:\n  %s",
			describeJava7073(recs))
	}

	var onElement, external int
	for _, r := range reader.Relationships {
		if r.Kind != "CALLS" {
			continue
		}
		switch {
		case r.ToID == elem.ID:
			onElement++
		case byID[r.ToID] == "":
			external++
		}
	}
	// The MEASURED fact: exactly one CALLS edge lands on the element entity,
	// and that entity is a SCOPE.Schema/field.
	if onElement != 1 {
		t.Errorf("want exactly 1 CALLS edge from Reader.read onto the element entity "+
			"%q, got %d. Reader.read edges: %v", elem.ID, onElement, reader.Relationships)
	}
	if got := byID[elem.ID]; got != "SCOPE.Schema|field|Audited.value" {
		t.Errorf("the CALLS target is %q, want %q — if the element's kind is being "+
			"changed, this measurement and the PR's Decision 2 both need revisiting",
			got, "SCOPE.Schema|field|Audited.value")
	}
	// Positive control on selectivity: `c.getAnnotation(...)` in the same method
	// targets a JDK type with no entity here and must stay unbound. Without it,
	// "the element call bound" is equally explained by every CALLS edge binding.
	if external == 0 {
		t.Errorf("selectivity control failed: no unbound CALLS edge remains on "+
			"Reader.read, so this fixture cannot distinguish a real binding from "+
			"everything binding. Edges: %v", reader.Relationships)
	}
}

// TestJavaAnnotationType_7073_ElementSignatureKeepsDefaultLiteral grades the
// signature builder's modifier-stripping arm and its interaction with the
// `default` clause, which the element signature deliberately KEEPS (unlike
// buildFieldSignature, which truncates at `=`). A whole-span ReplaceAll of
// "public " therefore reaches INTO the default's string literal.
//
// Axes: the element's MODIFIER PREFIX (absent / present) crossed with whether
// the default literal CONTAINS a modifier keyword. Held constant: the enclosing
// annotation, the file, the declared type (String), and the arity.
func TestJavaAnnotationType_7073_ElementSignatureKeepsDefaultLiteral(t *testing.T) {
	recs := extractJava7073(t, "Cfg.java", `public @interface Cfg {
    String scope() default "public api";
    public abstract String mode();
    String plain();
}
`)
	for _, want := range []struct{ name, sig string }{
		// The corruption: the literal is VALUE text, not a modifier.
		{"Cfg.scope", `String scope() default "public api"`},
		// The modifier-stripping arm itself, graded — without this row no
		// fixture puts a modifier on an element and the arm is dead code.
		{"Cfg.mode", "String mode()"},
		// Control: neither modifier nor default.
		{"Cfg.plain", "String plain()"},
	} {
		got := findJava7073(recs, "SCOPE.Schema", want.name)
		if len(got) != 1 {
			t.Errorf("want exactly 1 SCOPE.Schema named %q, got %d. All entities:\n  %s",
				want.name, len(got), describeJava7073(recs))
			continue
		}
		if got[0].Signature != want.sig {
			t.Errorf("%s.Signature = %q, want %q", want.name, got[0].Signature, want.sig)
		}
	}
}

// TestJavaAnnotationType_7073_ElementSpans grades the element builder's span.
// Spans are persisted through the flatbuffer, hashed by cmd/grafel's
// entityTupleKey, and drive grafel_get_source — so a builder that collapsed
// every element onto line 1 would be invisible to every other assertion in this
// file while breaking source retrieval for every annotation element in a repo.
//
// Axis varied: the element's LINE EXTENT (single-line vs a declaration wrapped
// across three lines). Held constant: the enclosing annotation, the file, the
// declared type, the arity, and the absence of modifiers.
func TestJavaAnnotationType_7073_ElementSpans(t *testing.T) {
	recs := extractJava7073(t, "Spans.java", `public @interface Spans {
    String one();
    String
        two()
        default "x";
    String three();
}
`)
	for _, want := range []struct {
		name       string
		start, end int
	}{
		{"Spans.one", 2, 2},
		// The multi-line row is what distinguishes a real span from
		// StartLine==EndLine emitted for everything.
		{"Spans.two", 3, 5},
		{"Spans.three", 6, 6},
	} {
		got := findJava7073(recs, "SCOPE.Schema", want.name)
		if len(got) != 1 {
			t.Errorf("want exactly 1 SCOPE.Schema named %q, got %d. All entities:\n  %s",
				want.name, len(got), describeJava7073(recs))
			continue
		}
		if got[0].StartLine != want.start || got[0].EndLine != want.end {
			t.Errorf("%s span = %d-%d, want %d-%d",
				want.name, got[0].StartLine, got[0].EndLine, want.start, want.end)
		}
	}
}

// TestJavaAnnotationType_7073_ElementTypeRefs grades the SECOND emit site for
// the #6912 field->type edge family. An annotation element is built by its own
// code path (buildAnnotationElement), so an arm that wired only buildField and
// the record-component arm would pass every test above and still leave the
// element's declared type unlinked.
//
// Axes: the ELEMENT'S DECLARED TYPE varies across the three shapes the
// allow-list distinguishes (a same-file class, a same-file annotation, a
// primitive that is addressable by nothing). Held constant: the enclosing
// annotation, the file, the modifier, and the element arity.
func TestJavaAnnotationType_7073_ElementTypeRefs(t *testing.T) {
	recs := extractJavaFT(t, map[string]string{"Meta.java": `class Policy {}
@interface Tag { String name(); }
@interface Meta {
  Policy policy();
  Tag tag();
  int order();
}
`})
	// `int order()` is the negative row held in the same assertion: a primitive
	// names nothing addressable in this file, so it must contribute NO edge.
	// Without it a producer that emitted an edge for every element would pass.
	javaFTWantEqual(t, javaFTEdges(recs), []string{
		"Meta.java:Meta.policy => Policy",
		"Meta.java:Meta.tag => Tag",
	})
}
