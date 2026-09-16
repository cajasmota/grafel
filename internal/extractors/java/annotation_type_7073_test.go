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

// TestJavaAnnotationType_7073_UsageSiteBinding MEASURES — it does not assert —
// what minting the declaration does for a usage site. It drives the real
// resolver over the declaring file plus a consumer file and reports which of
// the consumer's outbound edges land on the new entity.
//
// The measured result is recorded in the assertions below: the IMPORTS edge
// from the consumer file DOES bind once the declaration exists (it dangled
// before), while the `@Audited` application sites themselves still emit NO edge
// at all — the extractor has never modelled annotation USE as an edge, so
// minting the declaration cannot make a non-existent edge resolve. That second
// half is a separate, honestly-stated gap, pinned here so it cannot be silently
// claimed as fixed.
func TestJavaAnnotationType_7073_UsageSiteBinding(t *testing.T) {
	const consumerPath = "com/example/audit/Svc.java"
	const consumerSrc = `package com.example.audit;

import com.example.audit.Audited;

@Audited("x")
public class Svc {
    @Audited("y")
    public void go() {}
}
`
	recs := append(
		extractJava7073(t, javaAnn7073Path, javaAnn7073Src),
		extractJava7073(t, consumerPath, consumerSrc)...,
	)
	for i := range recs {
		if recs[i].ID == "" {
			recs[i].ID = graph.EntityID("issue7073", recs[i].Kind, recs[i].Name, recs[i].SourceFile)
		}
	}

	// The declaration is in the record set at all — the premise of any binding
	// claim below.
	decl := findJava7073(recs, "SCOPE.Component", "Audited")
	if len(decl) != 1 {
		t.Fatalf("premise failed: no Audited declaration to bind to; entities:\n  %s",
			describeJava7073(recs))
	}

	// Positive control on the address dialect: the annotation's simple name must
	// be UNAMBIGUOUS in this fixture, otherwise "it bound" would be untestable.
	idx := resolve.BuildIndex(recs)
	if _, st := idx.LookupStatusHint("Audited", "IMPORTS"); st != 1 {
		t.Fatalf("premise failed: %q does not resolve uniquely (status %d), so this "+
			"fixture cannot distinguish a bound edge from a lucky one", "Audited", st)
	}

	// Half one — MEASURED: annotation APPLICATION sites emit no edge.
	var annEdges []string
	for _, e := range recs {
		if e.SourceFile != consumerPath {
			continue
		}
		for _, r := range e.Relationships {
			if r.Kind == "IMPORTS" {
				continue
			}
			if strings.Contains(r.ToID, "Audited") {
				annEdges = append(annEdges, e.Name+" -["+r.Kind+"]-> "+r.ToID)
			}
		}
	}
	if len(annEdges) != 0 {
		t.Errorf("MEASUREMENT MOVED: @Audited application sites now emit %d edge(s): %v.\n"+
			"This test pins the honest scope of #7073 — if annotation USE now produces "+
			"edges, update this test and the PR body rather than leaving the claim stale.",
			len(annEdges), annEdges)
	}

	// Half two — MEASURED: the file-level IMPORTS edge names the annotation and
	// now has a real entity to reach.
	var importTargets []string
	for _, e := range recs {
		if e.SourceFile != consumerPath || e.Subtype != "file" {
			continue
		}
		for _, r := range e.Relationships {
			if r.Kind == "IMPORTS" {
				importTargets = append(importTargets, r.ToID)
			}
		}
	}
	found := false
	for _, tgt := range importTargets {
		if tgt == "com.example.audit.Audited" {
			found = true
		}
	}
	if !found {
		t.Errorf("consumer file has no IMPORTS edge naming com.example.audit.Audited; got %v",
			importTargets)
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
