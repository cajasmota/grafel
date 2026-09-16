package golang

import (
	"sort"
	"strings"
	"testing"
)

// #7041 — the METADATA-level grading of goTypeParameterNames, one level below
// the edge-level tests in field_type_refs_7041_test.go.
//
// WHY THIS FILE EXISTS AT ALL. The scala arm of #6912 (#7064) shipped a shadow
// guard switching on `variant_type_parameter` / `type_parameter` — NEITHER node
// exists in that grammar — and the guard was a silent no-op that no mutant could
// kill, because it was dead code. The node-type strings this collector matches
// are therefore asserted HERE, against a real parse, so a corrupted matcher
// string fails a test that names the string rather than only a downstream edge.
//
// The spellings below are not recalled; they were read off a CST dump of the
// exact form space this table enumerates. For the record, `type Box[T any]`
// parses as:
//
//	type_declaration
//	  type_spec
//	    type_identifier               "Box"
//	    type_parameter_list           "[T any]"
//	      type_parameter_declaration  "T any"
//	        identifier                "T"      <- the NAME, what we collect
//	        type_constraint           "any"    <- the CONSTRAINT, never collected
//	          type_identifier         "any"
//	    struct_type                   "struct { Item T }"
//
// AXES OF THIS TABLE
//
//	VARIED: the constraint form (`any`, `comparable`, a same-file named type, a
//	  union, a `~` approximation element, an interface literal, an interface
//	  literal embedding a named type); the arity of the parameter list (one
//	  declaration, two declarations, one declaration binding two names); the
//	  DECLARING FORM (generic struct, generic interface, generic function,
//	  method on a generic receiver, generic type definition with no body of its
//	  own); and the parameter name's own shape (ordinary, a predeclared
//	  identifier).
//	HELD CONSTANT: the file (one parse for the whole table) and the fact that
//	  every declaration is at package scope — Go has no other scope in which a
//	  type parameter list may appear on a declaration this pass anchors on.
//	  Both are attacked at the EDGE level instead: the relative POSITION of the
//	  colliding declaration is swept in TestGoFieldTypeRefs_7041_FormSpace, and
//	  the declaring-form rows there assert what each form emits.
func TestGoTypeParameterNames_FormSpace(t *testing.T) {
	src := []byte(`package models

type Plain struct{ X int }

type Box[T any] struct { Item T }
type Pair[K comparable, V any] struct { Key K; Val V }
type Multi[K, V any] struct { A K; B V }
type Con[T Plain] struct { It T }
type Uni[T Plain | Other] struct { U T }
type Appr[T ~Alias] struct { A T }
type Lit[T interface{ Ship() }] struct { L T }
type Emb[T interface{ Plain }] struct { E T }
type Shadow[string any] struct { S string }
type Iface[T any] interface { Get() T }
type Defn[T any] Box[T]

type Other struct{ Y int }
type Alias = int

func Gen[T Plain](x T) T { return x }

func (b Box[T]) Do(v T) T { return v }
`)
	parser, err := goAdapter.NewParser(goGrammar())
	if err != nil {
		t.Fatalf("parser: %v", err)
	}
	defer parser.Close()
	tree, err := parser.Parse(src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	defer tree.Close()

	// Premise: the parse really did produce the node types this collector
	// matches. Without this, every "want none" row below would pass vacuously
	// on a grammar that spells them differently — the #7064 failure exactly.
	for _, nodeType := range []string{"type_parameter_list", "type_parameter_declaration", "type_constraint"} {
		if got := findAll(tree.RootNode(), nodeType); len(got) == 0 {
			t.Fatalf("the grammar produced ZERO %q nodes for this fixture; the "+
				"collector matches that string, so every assertion below is vacuous",
				nodeType)
		}
	}

	got := map[string][]string{}
	for _, spec := range findAll(tree.RootNode(), "type_spec", "type_alias") {
		name := ""
		for i := 0; i < int(spec.NamedChildCount()); i++ {
			if c := spec.NamedChild(i); c.Type() == "type_identifier" {
				name = nodeText(c, src)
				break
			}
		}
		if name == "" {
			continue
		}
		got[name] = sortedKeys(goTypeParameterNames(spec, src))
	}

	want := map[string][]string{
		// one declaration, one name, the `any` constraint.
		"Box": {"T"},
		// two declarations, each with its own constraint.
		"Pair": {"K", "V"},
		// ONE declaration binding TWO names against one constraint. A collector
		// that takes the declaration's FIRST identifier only would return {"K"}
		// and leave `V` over-firing.
		"Multi": {"K", "V"},
		// the constraint is a SAME-FILE DECLARED TYPE. `Plain` must NOT appear:
		// harvesting it would refuse a correct edge from any sibling field typed
		// `Plain`, which is the silent-deletion direction cpp shipped first.
		"Con": {"T"},
		// union constraint over two same-file types — neither element collected.
		"Uni": {"T"},
		// `~Alias` — the approximation element wraps the name in a negated_type;
		// `Alias` must not be collected.
		"Appr": {"T"},
		// interface-literal constraint. `Ship` is a method name inside it.
		"Lit": {"T"},
		// interface literal EMBEDDING a same-file named type.
		"Emb": {"T"},
		// the parameter name is a predeclared identifier. Legal Go, and the
		// shadowing is exactly what makes it collectable.
		"Shadow": {"string"},
		// declaring form: generic INTERFACE, not struct.
		"Iface": {"T"},
		// declaring form: a generic type DEFINITION whose body is another
		// generic type.
		"Defn": {"T"},
		// NON-generic declarations bind nothing. Three of them, so "returns
		// empty" is not carried by a single row.
		"Plain": nil,
		"Other": nil,
		"Alias": nil,
	}
	for name, w := range want {
		g, ok := got[name]
		if !ok {
			t.Errorf("no type_spec named %q found in the fixture; the row grades nothing", name)
			continue
		}
		if strings.Join(g, ",") != strings.Join(w, ",") {
			t.Errorf("goTypeParameterNames(%s) = %v, want %v", name, g, w)
		}
	}
	for name := range got {
		if _, ok := want[name]; !ok {
			t.Errorf("type_spec %q is in the fixture but not in the table — every "+
				"declaration must be graded or the enumeration is not one", name)
		}
	}
}

// TestGoTypeParameterNames_FunctionFormsAreNotTypeSpecs records, as a graded
// fact rather than as prose, that a generic FUNCTION and a method on a GENERIC
// RECEIVER carry a type_parameter_list too — and that neither is a node this
// pass is ever handed, because neither emits a SCOPE.Schema/field entity.
//
// It is here so the declaring-form axis is closed at the CST level: if a future
// change starts anchoring field edges on function-shaped declarations, the
// collector must be re-pointed and this test says where.
func TestGoTypeParameterNames_FunctionFormsAreNotTypeSpecs(t *testing.T) {
	src := []byte(`package models

func Gen[T any](x T) T { return x }

func (b Box[T]) Do(v T) T { return v }
`)
	parser, err := goAdapter.NewParser(goGrammar())
	if err != nil {
		t.Fatalf("parser: %v", err)
	}
	defer parser.Close()
	tree, err := parser.Parse(src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	defer tree.Close()

	if got := findAll(tree.RootNode(), "type_spec", "type_alias"); len(got) != 0 {
		t.Fatalf("expected no type_spec in a function-only file, got %d", len(got))
	}
	fn := findAll(tree.RootNode(), "function_declaration")
	if len(fn) != 1 {
		t.Fatalf("want 1 function_declaration, got %d", len(fn))
	}
	// The generic FUNCTION does carry the same node, so the collector would work
	// on it verbatim — it is simply never called with one.
	if got := sortedKeys(goTypeParameterNames(fn[0], src)); strings.Join(got, ",") != "T" {
		t.Errorf("goTypeParameterNames(func Gen[T any]) = %v, want [T]", got)
	}
	// The METHOD's `[T]` is a type_arguments list on the receiver, NOT a
	// type_parameter_list, so the collector returns nothing for it.
	md := findAll(tree.RootNode(), "method_declaration")
	if len(md) != 1 {
		t.Fatalf("want 1 method_declaration, got %d", len(md))
	}
	if got := sortedKeys(goTypeParameterNames(md[0], src)); len(got) != 0 {
		t.Errorf("goTypeParameterNames(func (b Box[T]) Do) = %v, want none — the "+
			"receiver's [T] is type_arguments, not a type_parameter_list", got)
	}
}

func sortedKeys(m map[string]bool) []string {
	if len(m) == 0 {
		return nil
	}
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
