// hierarchy_6370_test.go — unit grading for the OCaml EXTENDS/IMPLEMENTS arm.
//
// The golden fixture ocaml-objects-mini grades the arm end to end, including
// resolution and the six forbidden shapes that must never mint a hierarchy
// edge. What it cannot grade is anything that needs source a golden fixture
// should not carry — a class that inherits itself is not legal OCaml — or a
// shape whose absence from the fixture would be arbitrary rather than
// meaningful. Those live here.
package ocaml

import (
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/types"
)

// hier returns the EXTENDS/IMPLEMENTS edges of src as "owner KIND target"
// strings, with the structural-ref target reduced to its bare name so a
// failure reads as a topology rather than as a hash.
func hier(t *testing.T, src string) []string {
	t.Helper()
	var out []string
	for _, e := range extractOCaml(src, "m.ml") {
		for _, r := range e.Relationships {
			if r.Kind != "EXTENDS" && r.Kind != "IMPLEMENTS" {
				continue
			}
			target := r.ToID
			if i := strings.LastIndexByte(target, ':'); i >= 0 {
				target = target[i+1:]
			}
			out = append(out, e.Name+" "+r.Kind+" "+target)
		}
	}
	return out
}

func classNames(recs []types.EntityRecord, subtype string) []string {
	var out []string
	for _, e := range recs {
		if e.Kind == "SCOPE.Component" && e.Subtype == subtype {
			out = append(out, e.Name)
		}
	}
	return out
}

func wantEdges(t *testing.T, src string, want ...string) {
	t.Helper()
	got := hier(t, src)
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("hierarchy edges:\n got %q\nwant %q", got, want)
	}
}

// A class cannot inherit itself in legal OCaml, so the golden fixture must not
// carry the shape. The guard still has to be graded: a self-edge is never
// information, it is the signature of a mis-attributed owner (#6369), and the
// cheapest way to produce one is a producer bug rather than a source file.
func TestOCamlHierarchy_SelfEdgeSuppressed_6370(t *testing.T) {
	wantEdges(t, "class loop = object\n  inherit loop\nend\n")
}

// Two identical clauses must yield one edge, not two. Nothing in the golden
// fixture repeats a parent.
func TestOCamlHierarchy_DuplicateParentDeduped_6370(t *testing.T) {
	src := "class base = object\n  method m = 1\nend\n" +
		"class twice = object\n  inherit base\n  inherit base\nend\n"
	wantEdges(t, src, "twice EXTENDS base")
}

// `inherit!` (the override form) differs from `inherit` by one anonymous
// token, and `inherit ['a] parent` wraps the path in instantiated_class rather
// than class_application. Both are carriers the fixture does not spell.
func TestOCamlHierarchy_OverrideAndInstantiatedForms_6370(t *testing.T) {
	src := "class base = object\n  method m = 1\nend\n" +
		"class ['a] boxed = object\n  method v : 'a option = None\nend\n" +
		"class over = object\n  inherit! base\nend\n" +
		"class inst = object\n  inherit ['a] boxed\nend\n"
	wantEdges(t, src, "over EXTENDS base", "inst EXTENDS boxed")
}

// The resolution gate: an unqualified parent that this file does not declare
// yields NO edge. #6370's constraint is that "emit only when the target
// resolves" is enforced by the PRODUCER, because analytics/scan.go keys
// touched[from] unconditionally and a dangling edge de-orphans its source
// exactly as well as a real one.
//
// Graded here and not only by the golden fixture's
// `remote_relay --[EXTENDS]--> external_base` fence, because that fence can
// only name the edge in the dialect the producer happens to emit. hier()
// reduces past the last ':', so this assertion holds whether ToID is a bare
// name or a structural ref — it grades the GUARD rather than the spelling.
func TestOCamlHierarchy_UndeclaredParentYieldsNoEdge_6370(t *testing.T) {
	const undeclared = "class remote_relay = object\n  inherit external_base\nend\n"
	wantEdges(t, undeclared)
	// Positive control, holding everything but the declaration constant: the
	// same clause DOES produce an edge once the parent exists in the file, so
	// the assertion above cannot pass by the producer emitting nothing at all.
	wantEdges(t, "class external_base = object\n  method m = 1\nend\n"+undeclared,
		"remote_relay EXTENDS external_base")
}

// The qualified-path guard, graded ARM BY ARM.
//
// pathLeafName rejects a path carrying a module qualifier, and the two node
// types it rejects are reached by DIFFERENT constructs: a class's `class_path`
// qualifies with `module_path`, a class type's `class_type_path` qualifies with
// `extended_module_path`. One test naming only the first leaves the second
// ungraded — measured, not assumed: deleting only the extended_module_path arm
// left the whole suite green.
//
// Every case declares a LOCAL `printer`, so dropping the guard does not merely
// add an edge, it produces `... EXTENDS printer` bound to the wrong node, which
// is #6369's failure mode stated as an input rather than as a warning.
func TestOCamlHierarchy_QualifiedParentYieldsNoEdge_6370(t *testing.T) {
	const local = "class type printer = object\n  method print : string -> unit\nend\n" +
		"class printer_cls = object\n  method print (_ : string) = ()\nend\n"
	for _, tc := range []struct{ name, src string }{
		// class_path + module_path
		{"class form, module qualifier",
			"class relay = object\n  inherit Remote.printer_cls\nend\n"},
		// class_type_path + extended_module_path
		{"class-type form, module qualifier",
			"class type relay_t = object\n  inherit Mod.printer\nend\n"},
		// class_type_path + extended_module_path built by a FUNCTOR APPLICATION.
		// `Make(Int)` is an extended_module_path with its own nested paths, and
		// it is the shape that survived the first round of grading.
		{"class-type form, functor-application qualifier",
			"class type relay_f = object\n  inherit Make(Int).printer\nend\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			wantEdges(t, local+tc.src)
		})
	}
	// Positive controls: the SAME leaf names, unqualified, do produce edges —
	// so the three cases above grade the qualifier and not the parent's
	// existence.
	wantEdges(t, local+"class relay = object\n  inherit printer_cls\nend\n",
		"relay EXTENDS printer_cls")
	wantEdges(t, local+"class type relay_t = object\n  inherit printer\nend\n",
		"relay_t EXTENDS printer")
}

// The IMPLEMENTS source is the class binding's OWN `: class-type` annotation,
// read as a DIRECT child. A recursive search reaches a `class_type_path` in a
// method's parameter type and mints a conformance the class never declared.
//
// This test exists because the claim it replaces was wrong. An earlier note
// here asserted that `class_type_path` occurs only in class-type positions,
// having enumerated five annotation sites that all produced
// `type_constructor_path`. A sixth — `#printer`, the hash-type form, which is
// how OCaml spells "any object matching this class type" — produces exactly
// `hash_type > class_type_path`, and the recursive mutant emits
// `g IMPLEMENTS printer` from it. Five sites chosen by one person is one
// confirmation, not five.
func TestOCamlHierarchy_AnnotationIsTheBindingsOwnDirectChild_6370(t *testing.T) {
	const printer = "class type printer = object\n  method print : string -> unit\nend\n"
	// A method PARAMETER typed by the hash form. `g` conforms to nothing.
	wantEdges(t, printer+"class g = object\n  method m (x : #printer) = x\nend\n")
	// Positive control: the binding's own annotation, one nesting level up, IS
	// the conformance and does produce the edge.
	wantEdges(t, printer+"class h : printer = object\n  method print (_ : string) = ()\nend\n",
		"h IMPLEMENTS printer")
}

// A class declared inside `module M = struct ... end` is not at column 0, and
// every other entity this extractor produces is. The CST walk is what makes it
// visible; a top-level-only walk would silently drop it.
func TestOCamlHierarchy_ClassInsideModule_6370(t *testing.T) {
	src := "module M = struct\n" +
		"  class base = object\n    method m = 1\n  end\n" +
		"  class derived = object\n    inherit base\n  end\n" +
		"end\n"
	wantEdges(t, src, "derived EXTENDS base")
	if got := classNames(extractOCaml(src, "m.ml"), "class"); len(got) != 2 {
		t.Fatalf("classes inside a module: got %q, want base and derived", got)
	}
}

// `and` chains several bindings under one `class` keyword. A producer reading
// only the first binding loses every class after it.
func TestOCamlHierarchy_AndChainedBindings_6370(t *testing.T) {
	src := "class base = object\n  method m = 1\nend\n" +
		"class a = object\n  inherit base\nend\n" +
		"and b = object\n  inherit base\nend\n"
	wantEdges(t, src, "a EXTENDS base", "b EXTENDS base")
}

// The three module-composition keywords are separate grammar productions and
// cannot reach the hierarchy producer. Asserted anyway: "cannot happen" is an
// argument, and this package has already shipped one wrong one (#6812's
// "no tree-sitter grammar for OCaml is bundled", false since it was vendored).
func TestOCamlHierarchy_ModuleCompositionMintsNoEdge_6370(t *testing.T) {
	src := "open Base\n" +
		"module type Comparable = sig\n  type t\nend\n" +
		"module Base = struct\n  let x = 1\nend\n" +
		"module Extended = struct\n  include Base\nend\n" +
		"module Make (C : Comparable) = struct\n  type item = C.t\nend\n"
	wantEdges(t, src)
}

// The class-type annotation is IMPLEMENTS and the `inherit` clause is EXTENDS,
// on the same binding. The fixture carries each separately; this holds them
// together so a producer that reuses one kind for both is caught by order as
// well as by count.
func TestOCamlHierarchy_AnnotationAndInheritOnOneBinding_6370(t *testing.T) {
	src := "class type printer = object\n  method print : string -> unit\nend\n" +
		"class base = object\n  method print (_ : string) = ()\nend\n" +
		"class both : printer = object\n  inherit base\n  method print (_ : string) = ()\nend\n"
	wantEdges(t, src, "both IMPLEMENTS printer", "both EXTENDS base")
}

// A file the grammar cannot parse must produce no class entity and no edge
// rather than a guess. blocks.go already declines to invent a block span on
// such input; the hierarchy arm rides the same tree and must decline too.
func TestOCamlHierarchy_UnparseableInputYieldsNothing_6370(t *testing.T) {
	src := "class ((( = = = object object object\n"
	if got := hier(t, src); len(got) != 0 {
		t.Fatalf("malformed source produced hierarchy edges: %q", got)
	}
}

// The IMPLEMENTS clause's line is the ANNOTATION's line, not the binding's.
// The two coincide on every single-line header, which is every class in the
// golden fixture, so the field was ungraded there: a mutant reading the
// binding's start row survived the whole suite. An OCaml class header may span
// lines, so the axis is real and this separates it.
func TestOCamlHierarchy_ImplementsLineIsTheAnnotationLine_6370(t *testing.T) {
	src := "class type printer = object\n  method print : string -> unit\nend\n" + // 1-3
		"class impl\n  : printer =\n  object\n  method print (_ : string) = ()\nend\n" // 4-8
	var got []string
	for _, e := range extractOCaml(src, "m.ml") {
		for _, r := range e.Relationships {
			if r.Kind == "IMPLEMENTS" {
				got = append(got, r.Properties.Get("line"))
			}
		}
	}
	if strings.Join(got, ",") != "5" {
		t.Fatalf("IMPLEMENTS clause line: got %q, want [5] — the `: printer` "+
			"annotation is on line 5 and the binding starts on line 4", got)
	}
}

// Every hierarchy edge must leave FromID empty so assembly stamps the owning
// CLASS's id. A non-empty non-hex FromID is rewritten onto the FILE entity by
// ReferencesEmbedded, merging every class in a multi-class file onto one node
// (#6295, #6298; guarded repo-wide by
// internal/extractors/file_anchored_rels_guard_test.go).
func TestOCamlHierarchy_FromIDIsEmpty_6370(t *testing.T) {
	src := "class base = object\n  method m = 1\nend\n" +
		"class derived = object\n  inherit base\nend\n"
	n := 0
	for _, e := range extractOCaml(src, "m.ml") {
		for _, r := range e.Relationships {
			if r.Kind != "EXTENDS" && r.Kind != "IMPLEMENTS" {
				continue
			}
			n++
			if r.FromID != "" {
				t.Fatalf("%s --[%s]--> %s carries FromID %q; it must be empty so assembly anchors it on the class",
					e.Name, r.Kind, r.ToID, r.FromID)
			}
		}
	}
	if n != 1 {
		t.Fatalf("expected exactly one hierarchy edge to inspect, got %d", n)
	}
}

// Every hierarchy edge carries the line of the clause that declared it, and
// the line is the CLAUSE's, not the class's. A producer stamping the
// declaration line passes a presence check and gives every parent of a
// multiply-inheriting class the same number.
func TestOCamlHierarchy_LineIsTheClauseLine_6370(t *testing.T) {
	src := "class a = object\n  method m = 1\nend\n" + // 1-3
		"class b = object\n  method m = 2\nend\n" + // 4-6
		"class c = object\n  inherit a\n  inherit b\nend\n" // 7-10
	var lines []string
	for _, e := range extractOCaml(src, "m.ml") {
		for _, r := range e.Relationships {
			if r.Kind == "EXTENDS" {
				lines = append(lines, r.Properties.Get("line"))
			}
		}
	}
	if strings.Join(lines, ",") != "8,9" {
		t.Fatalf("clause lines: got %q, want [8 9]", lines)
	}
}
