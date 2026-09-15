package cpp_test

import (
	"sort"
	"strings"
	"testing"

	extreg "github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/resolve"
	"github.com/cajasmota/grafel/internal/types"
)

// #6912 arm I — C/C++ field→declared-type REFERENCES edges.
//
// AXES VARIED (one row per cell, and the two axes the language keeps SEPARATE
// are varied separately — see the DECLARATOR / TYPE-EXPRESSION note below):
//
//   - DECLARATOR decoration, type expression held constant at `Order`:
//     value, pointer `*`, reference `&`, array `[4]`, multi-declarator `a, b`.
//     In the tree-sitter C++ grammar this decoration lives in the DECLARATOR,
//     not in the `type` field, so every one of these rows arrives at the pass
//     as the identical string "Order". They are therefore NOT a test of the
//     type scanner at all — they are a test that the field ENTITIES exist and
//     each gets its own edge. Listing them as a thorough type-shape sweep is
//     the mistake this fixture is arranged to avoid.
//
//   - TYPE EXPRESSION, declarator held constant at a plain value/pointer:
//     bare `Order`, template arg `std::vector<Order>`, two template args
//     `std::map<Item, Order>`, smart pointer `std::unique_ptr<Order>`,
//     two-target `std::pair<Order, Item>`, in-file template `Box<Order>`,
//     elaborated `struct Item`, sized `unsigned int`, cv-qualified `const int`,
//     global-qualified `::Order`, namespace-qualified `Ns::Order` (its own
//     test), inline anonymous `struct { int inner; }`.
//
//   - TARGET KIND, i.e. what the name resolves to IN THIS FILE: a defined
//     class / struct / union (admitted); a FORWARD DECLARATION (refused — the
//     not-a-declaration row); an `#include` placeholder; a `using namespace`
//     placeholder; a `using X::Y` placeholder; a SCOPE.Schema enum; a
//     SCOPE.Schema template and an explicit specialisation; a `using`-alias and
//     a `typedef` (which mint NO record at all); the owner itself; a name
//     nothing declares.
//
// AXES HELD CONSTANT: one declaring class per fixture (so the owner
// self-reference rule has one meaning); one file (the same-file conjuncts are
// graded by their own two tests, in opposite directions); language `cpp` except
// in TestCppFieldTypeRefs_CLanguageHeaderEmitsTheSameEdge, which is the `.h` →
// language `c` row.
const cppFTPath = "model/holder.cpp"

const cppFTSrc = `
#include <vector>
#include "order.h"

using std::string;
using namespace std;

class Fwd;

struct Item { int n; };
class Order { public: int id; };
union Money { int cents; };
struct Empty {};

enum class Color { Red, Green };
enum Plain { P1, P2 };

template<typename TP> struct Box { TP v; };
struct TP { int x; };
template<> struct Box<int> { int v; };

using Alias = Order;
typedef Order OrderT;

class Holder {
public:
  Order plain;
  Order* ptr;
  Order& ref;
  Order arr[4];
  Item a, b;

  std::vector<Order> vec;
  std::map<Item, Order> mapped;
  std::unique_ptr<Order> uptr;
  std::pair<Order, Item> paired;
  Box<Order> templated;
  struct Item elaborated;
  const Order& constref;
  ::Order globalq;
  unsigned int uns;
  const int cint;
  int prim;

  string viaUsingDecl;
  vector<Order> viaUsingNs;

  Color color;
  Plain plainEnum;
  Box<int> spec;
  Alias aliased;
  OrderT typedefed;
  Fwd fwd;
  Money money;
  Empty empty;
  Holder* self;
  Missing missing;
  TP tp;

  struct { int inner; } anon;
};
`

// cppFTTargetsOf returns the sorted target_type values of the field-type edges
// carried by the named field entity.
func cppFTTargetsOf(recs []types.EntityRecord, fieldName string) []string {
	var out []string
	for i := range recs {
		if recs[i].Name != fieldName {
			continue
		}
		for _, r := range recs[i].Relationships {
			if r.Kind == "REFERENCES" && r.Properties.Get("ref_kind") == "field_target_type" {
				out = append(out, r.Properties.Get("target_type"))
			}
		}
	}
	sort.Strings(out)
	return out
}

func cppFTEqual(t *testing.T, got, want []string, what string) {
	t.Helper()
	if len(got) != len(want) {
		t.Errorf("%s = %v (%d), want %v (%d)", what, got, len(got), want, len(want))
		return
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("%s = %v, want %v", what, got, want)
			return
		}
	}
}

func cppFTExtract(t *testing.T, src, path string) []types.EntityRecord {
	t.Helper()
	recs, err := extractCPP(src, path)
	if err != nil {
		t.Fatalf("extract %s: %v", path, err)
	}
	return recs
}

// TestCppFieldTypeRefs_FieldTypeIsVerbatim pins the premise the arm rests on:
// emitClassFieldMembers (struct_fields.go:61) records the `type` field's
// VERBATIM text, so the property can be tokenised rather than stashed from the
// AST the way arm G (swift) had to. If this ever becomes a lossy projection —
// swift's field_type is the first pre-order type_identifier and yields
// `Dictionary` for `Dictionary<String, Order>` — the scanner is reading a lie
// and this test is the one that says so.
//
// It also pins the SECOND half of that premise, which is cpp-specific: the
// pointer / reference / array decoration lives in the DECLARATOR, so `Order*`,
// `Order&` and `Order arr[4]` all arrive as the bare string "Order". That is
// what makes those rows a declarator-axis test rather than a type-axis one.
func TestCppFieldTypeRefs_FieldTypeIsVerbatim(t *testing.T) {
	recs := cppFTExtract(t, cppFTSrc, cppFTPath)
	got := map[string]string{}
	for i := range recs {
		if recs[i].Kind == "SCOPE.Schema" && recs[i].Subtype == "field" {
			got[recs[i].Name] = recs[i].Properties["field_type"]
		}
	}
	want := map[string]string{
		// declarator axis — every one is the SAME type string
		"Holder.plain": "Order",
		"Holder.ptr":   "Order",
		"Holder.ref":   "Order",
		"Holder.arr":   "Order",
		"Holder.a":     "Item",
		"Holder.b":     "Item",
		// type-expression axis — verbatim, nothing unwrapped or dropped
		"Holder.vec":        "std::vector<Order>",
		"Holder.mapped":     "std::map<Item, Order>",
		"Holder.uptr":       "std::unique_ptr<Order>",
		"Holder.paired":     "std::pair<Order, Item>",
		"Holder.templated":  "Box<Order>",
		"Holder.elaborated": "struct Item",
		"Holder.constref":   "Order",
		"Holder.globalq":    "::Order",
		"Holder.uns":        "unsigned int",
		"Holder.cint":       "int",
		"Holder.anon":       "struct { int inner; }",
	}
	for name, w := range want {
		g, ok := got[name]
		if !ok {
			t.Errorf("no field entity %q — the fixture premise is gone", name)
			continue
		}
		if g != w {
			t.Errorf("%s field_type = %q, want the verbatim source text %q", name, g, w)
		}
	}
}

// TestCppFieldTypeRefs_ShapeSpace ENUMERATES the fixture rather than sampling
// it: every field entity the fixture emits must have a row here with the exact
// target set it produces, and every row must correspond to a real field entity.
// A shape added to the fixture without a row fails loudly instead of going
// ungraded.
func TestCppFieldTypeRefs_ShapeSpace(t *testing.T) {
	recs := cppFTExtract(t, cppFTSrc, cppFTPath)

	want := map[string][]string{
		// --- DECLARATOR axis: one edge each, all to the same target ---
		"Holder.plain": {"Order"},
		"Holder.ptr":   {"Order"},
		"Holder.ref":   {"Order"},
		"Holder.arr":   {"Order"},
		"Holder.a":     {"Item"},
		"Holder.b":     {"Item"},

		// --- TYPE-EXPRESSION axis ---
		// `std::vector` is consumed WHOLE as a qualified name and rejected; the
		// template argument is scanned on its own.
		"Holder.vec":    {"Order"},
		"Holder.mapped": {"Item", "Order"},
		"Holder.uptr":   {"Order"},
		"Holder.paired": {"Item", "Order"},
		// `Box` is a SCOPE.Schema/template, not a Component — refused as a
		// target; its ARGUMENT is not.
		"Holder.templated": {"Order"},
		// C's elaborated-type form. `struct` is a keyword no declaration can be
		// named, so no blocklist is needed to drop it.
		"Holder.elaborated": {"Item"},
		"Holder.constref":   {"Order"},
		// `::Order` is the GLOBAL Order, which a file-scoped bare name cannot be
		// proved to be. Refused, conservatively and deliberately.
		"Holder.globalq": nil,
		"Holder.uns":     nil,
		"Holder.cint":    nil,
		"Holder.prim":    nil,

		// --- TARGET-KIND axis ---
		// `using std::string;` mints an import placeholder named "std::string",
		// so nothing in this file is named `string` at all.
		"Holder.viaUsingDecl": nil,
		// `#include <vector>` mints a SCOPE.Component/import named exactly
		// "vector" — a bare-named non-declaration in this very file. THE LIVE
		// WRONG-BINDING ROW; see TestCppFieldTypeRefs_IncludePlaceholderIsNeverATarget.
		"Holder.viaUsingNs": {"Order"},
		// enums are SCOPE.Schema — a named recall loss, see
		// TestCppFieldTypeRefs_EnumIsNotATarget.
		"Holder.color":     nil,
		"Holder.plainEnum": nil,
		// `Box<int>` — the explicit specialisation is a SCOPE.Schema/template
		// named "Box<int>", unreachable by a bare identifier; `int` declares
		// nothing.
		"Holder.spec": nil,
		// `using Alias = Order;` and `typedef Order OrderT;` mint NO record —
		// a capture ceiling, pinned by TestCppFieldTypeRefs_AliasAndTypedefMintNoRecord.
		"Holder.aliased":   nil,
		"Holder.typedefed": nil,
		// THE NOT-A-DECLARATION ROW: `class Fwd;` mints a
		// SCOPE.Component/class named Fwd in this file with no body.
		"Holder.fwd":   nil,
		"Holder.money": {"Money"},
		"Holder.empty": {"Empty"},
		// the owner itself
		"Holder.self": nil,
		// nothing declares it
		"Holder.missing": nil,
		// a real struct whose name happens to match the template parameter of
		// another declaration in the same file. Templated members emit no field
		// entities at all, so go's known-wrong over-fire (#7041) has no cpp
		// analogue — see TestCppFieldTypeRefs_TemplatedClassMembersEmitNoFieldEntity.
		"Holder.tp": {"TP"},
		// an inline anonymous type definition is refused WHOLE.
		"Holder.anon": nil,
	}

	seen := map[string]bool{}
	for i := range recs {
		r := &recs[i]
		if r.Kind != "SCOPE.Schema" || r.Subtype != "field" {
			continue
		}
		if !strings.HasPrefix(r.Name, "Holder.") {
			continue // the target declarations' own members
		}
		seen[r.Name] = true
		w, ok := want[r.Name]
		if !ok {
			t.Errorf("field %q is in the fixture but has no row in the shape "+
				"table — add one rather than leaving the shape ungraded", r.Name)
			continue
		}
		cppFTEqual(t, cppFTTargetsOf(recs, r.Name), w, r.Name)
	}
	for name := range want {
		if !seen[name] {
			t.Errorf("the shape table names %q but the fixture emits no such "+
				"field entity — the row is vacuous", name)
		}
	}
}

// TestCppFieldTypeRefs_ForwardDeclarationIsNeverATarget is THE not-a-declaration
// kill, and it is the cpp analogue of the defect that cost arm G (swift) half
// its edges (#7047 / #7056).
//
// extractClassLike (extractor.go:825) emits a SCOPE.Component for a
// class_specifier whether or not it has a body, so `class Order;` — a forward
// declaration whose definition lives in ANOTHER file — mints a record with the
// right Name, the right SourceFile, an admissible Kind and an admissible
// Subtype. It is wrong only in the dimension nothing else measures: it is not a
// declaration OF the type, and binding to it silently steals the inbound edge
// from the file that really declares it, while every instrument reports
// bound=1 dangling=0.
//
// Both directions are asserted: the forward-declared name gets NO edge, and a
// DEFINED name in the same file still does (so the test cannot pass by the pass
// being broken).
func TestCppFieldTypeRefs_ForwardDeclarationIsNeverATarget(t *testing.T) {
	src := `
class Order;
struct Money { int cents; };

class Holder {
public:
  Order* pending;
  Money price;
};
`
	recs := cppFTExtract(t, src, "fwd.cpp")

	// Premise: the forward declaration really is in this file's record set,
	// under the bare name, with an ADMITTED subtype — so the refusal below is
	// doing work rather than being masked by the allow-list.
	fwd := false
	for i := range recs {
		r := &recs[i]
		if r.Kind == "SCOPE.Component" && r.Name == "Order" && r.SourceFile == "fwd.cpp" {
			fwd = true
			if r.Subtype != "class" {
				t.Fatalf("the forward declaration now carries Subtype %q — the "+
					"refusal's discriminator has moved", r.Subtype)
			}
		}
	}
	if !fwd {
		t.Fatal("no bare-named forward-declaration record in this file — the " +
			"refusal under test is unreachable and this test is vacuous")
	}

	cppFTEqual(t, cppFTTargetsOf(recs, "Holder.pending"), nil,
		"a field typed after a FORWARD DECLARATION")
	cppFTEqual(t, cppFTTargetsOf(recs, "Holder.price"), []string{"Money"},
		"the control field in the same class")
}

// TestCppFieldTypeRefs_ForwardDeclarationBesideItsDefinitionStillBinds is the
// other direction of the same refusal. `class Order;` followed by the real
// `class Order { … };` in ONE file is ordinary C++ (and is what a header does
// to break a cycle). The two records share (Kind, Name, SourceFile), so
// graph.EntityID collapses them to ONE node — a bodyless record must not
// suppress the definition beside it.
func TestCppFieldTypeRefs_ForwardDeclarationBesideItsDefinitionStillBinds(t *testing.T) {
	src := `
class Order;
class Order { public: int id; };

class Holder {
public:
  Order o;
};
`
	recs := cppFTExtract(t, src, "both.cpp")
	n := 0
	for i := range recs {
		if recs[i].Kind == "SCOPE.Component" && recs[i].Name == "Order" {
			n++
		}
	}
	if n < 2 {
		t.Fatalf("this file mints %d Order components; the two-record premise "+
			"of this test is gone", n)
	}
	cppFTEqual(t, cppFTTargetsOf(recs, "Holder.o"), []string{"Order"},
		"a field whose type is BOTH forward-declared and defined here")
}

// TestCppFieldTypeRefs_IncludePlaceholderIsNeverATarget is the live
// wrong-binding kill, and it is reproduced from real C++ rather than invented.
//
// extractInclude (extractor.go:1269) mints a SCOPE.Component/import whose Name
// is the header spelling, so `#include <vector>` puts a Component literally
// named `vector` in this file; extractUsing does the same for
// `using namespace std;` (named `std`). Written as `vector<Order> v;` under a
// `using namespace std;` — which is how a great deal of C++ in the wild is
// written — the field's own type names the placeholder EXACTLY.
//
// The file must NOT declare a class of that name, or the ambiguity rule would
// mask the allow-list and this test would pass for the wrong reason.
func TestCppFieldTypeRefs_IncludePlaceholderIsNeverATarget(t *testing.T) {
	src := `
#include <vector>
using namespace std;

struct Order { int id; };

class Holder {
public:
  vector<Order> rows;
  std* nsptr;
  Order one;
};
`
	recs := cppFTExtract(t, src, "inc.cpp")

	kinds := map[string]string{}
	for i := range recs {
		r := &recs[i]
		if r.Kind == "SCOPE.Component" && r.SourceFile == "inc.cpp" {
			if r.Name == "vector" || r.Name == "std" {
				kinds[r.Name] = r.Subtype
			}
			if r.Subtype == "class" || r.Subtype == "struct" || r.Subtype == "union" {
				if r.Name == "vector" || r.Name == "std" {
					t.Fatalf("the fixture declares a real type named %q, which "+
						"lets the ambiguity rule mask the allow-list", r.Name)
				}
			}
		}
	}
	for _, n := range []string{"vector", "std"} {
		if kinds[n] != "import" {
			t.Fatalf("no bare-named import placeholder %q in this file (subtype "+
				"%q) — the refusal under test is unreachable", n, kinds[n])
		}
	}

	cppFTEqual(t, cppFTTargetsOf(recs, "Holder.rows"), []string{"Order"},
		"a field whose type names an #include placeholder AND a real type")
	cppFTEqual(t, cppFTTargetsOf(recs, "Holder.nsptr"), nil,
		"a field typed after a `using namespace` placeholder")
	cppFTEqual(t, cppFTTargetsOf(recs, "Holder.one"), []string{"Order"},
		"the control field in the same class")
}

// TestCppFieldTypeRefs_QualifiedTypeIsNotStrippedToASameFileName is the
// wrong-binding direction arm D (go) found in `resolveTypeReferences`, where
// `other.Order` bound to a same-file `Order`. C++ makes the shape ordinary:
// `Ns::Order` and a global `Order` in one translation unit are DIFFERENT types
// and both are legal.
//
// The scanner consumes a qualified name WHOLE rather than skipping the `::`,
// because a scanner that merely skipped the separator would re-enter mid-token
// and produce exactly the bare segment the skip exists to prevent.
func TestCppFieldTypeRefs_QualifiedTypeIsNotStrippedToASameFileName(t *testing.T) {
	src := `
namespace Ns { struct Order { int id; }; }

struct Order { int gid; };

class Holder {
public:
  Ns::Order inner;
  ::Order global;
  Order bare;
};
`
	recs := cppFTExtract(t, src, "qual.cpp")
	cppFTEqual(t, cppFTTargetsOf(recs, "Holder.inner"), nil,
		"a namespace-qualified type must not bind a same-file bare name")
	cppFTEqual(t, cppFTTargetsOf(recs, "Holder.global"), nil,
		"a global-qualified type must not bind a same-file bare name")
	cppFTEqual(t, cppFTTargetsOf(recs, "Holder.bare"), []string{"Order"},
		"the unqualified control in the same class")
}

// TestCppFieldTypeRefs_EnumIsNotATarget records a REFUSAL THAT IS A RECALL
// LOSS, with the cpp-specific reason — which is NOT the reason arms C/D/E give.
//
// extractEnum (extractor.go:1080) mints `enum class Color { … }` as
// SCOPE.Schema/enum. The address this pass emits is a COMPONENT-space
// structural ref, and SCOPE.Schema is not in componentKindFamily, so such an
// edge would miss tier 1 — but, unlike PHP's enum (which hits an ambiguous
// tier 2 and dangles), a lone cpp enum would fall through to the KIND-AGNOSTIC
// byLocation tier and BIND. So the refusal here cannot be justified as
// "emitting would guarantee a dangling stub": that claim is false for cpp and
// the arm does not make it.
//
// It is refused because an address whose scope segment says `component` must
// not be answered by a non-component, and because admitting it would put this
// arm's targets on TWO tiers with two different ambiguity rules for the sake of
// a population this arm measured (see the PR body). Closing it needs a
// schema-space dialect, which is #6451's shape and should be decided across
// arms rather than invented here.
func TestCppFieldTypeRefs_EnumIsNotATarget(t *testing.T) {
	src := `
enum class Color { Red, Green };
enum Plain { P1, P2 };
struct Money { int cents; };

class Holder {
public:
  Color c;
  Plain p;
  Money m;
};
`
	recs := cppFTExtract(t, src, "enum.cpp")
	for _, n := range []string{"Color", "Plain"} {
		found := false
		for i := range recs {
			if recs[i].Name == n && recs[i].Kind == "SCOPE.Schema" && recs[i].Subtype == "enum" {
				found = true
			}
		}
		if !found {
			t.Fatalf("no SCOPE.Schema/enum named %q — the refusal under test is "+
				"unreachable and this test is vacuous", n)
		}
	}
	cppFTEqual(t, cppFTTargetsOf(recs, "Holder.c"), nil, "an enum-class-typed field")
	cppFTEqual(t, cppFTTargetsOf(recs, "Holder.p"), nil, "a plain-enum-typed field")
	cppFTEqual(t, cppFTTargetsOf(recs, "Holder.m"), []string{"Money"},
		"the control field in the same class")
}

// TestCppFieldTypeRefs_TemplatedClassMembersEmitNoFieldEntity pins the reason
// go's known-wrong template-parameter over-fire (#7041) has NO cpp analogue,
// so that the absence is a recorded measurement rather than an untested
// assumption. walkStructural's template_declaration arm (extractor.go:310)
// walks the templated class's body for Operations only — it never calls
// emitClassFieldMembers — so `template<typename TP> struct Box { TP v; };`
// beside a real `struct TP` produces no field entity for `v` and therefore no
// edge to mis-bind.
//
// It is a RECALL CEILING as well as a safety property, and it is stated in both
// directions: the templated member is absent, the non-templated one is present.
func TestCppFieldTypeRefs_TemplatedClassMembersEmitNoFieldEntity(t *testing.T) {
	src := `
struct TP { int x; };
template<typename TP> struct Box { TP v; };

class Holder {
public:
  TP real;
};
`
	recs := cppFTExtract(t, src, "tmpl.cpp")
	for i := range recs {
		if recs[i].Kind == "SCOPE.Schema" && recs[i].Subtype == "field" &&
			strings.HasPrefix(recs[i].Name, "Box.") {
			t.Fatalf("a templated class now emits field entity %q. The #7041 "+
				"over-fire shape is reachable in cpp: give `Box.v` a deliberate "+
				"rule rather than letting it bind `struct TP`.", recs[i].Name)
		}
	}
	cppFTEqual(t, cppFTTargetsOf(recs, "Holder.real"), []string{"TP"},
		"a non-templated field naming the same struct")
}

// TestCppFieldTypeRefs_AliasAndTypedefMintNoRecord pins a capture ceiling this
// arm found and deliberately did NOT close: neither `using Alias = Order;`
// (alias_declaration) nor `typedef Order OrderT;` (type_definition) is a case in
// walkStructural, so neither mints any record at all. A field typed after one
// gets no edge — and, equally, neither can become a bare-named non-declaration
// carrier. The test asserts the CURRENT behaviour so closing the capture fails
// loudly rather than silently changing the shape table above.
func TestCppFieldTypeRefs_AliasAndTypedefMintNoRecord(t *testing.T) {
	src := `
struct Order { int id; };
using Alias = Order;
typedef Order OrderT;

class Holder {
public:
  Alias a;
  OrderT o;
  Order direct;
};
`
	recs := cppFTExtract(t, src, "alias.cpp")
	for i := range recs {
		if n := recs[i].Name; n == "Alias" || n == "OrderT" {
			t.Fatalf("%q now mints a %s/%s record. The alias capture ceiling is "+
				"closed: decide deliberately whether an alias is a target "+
				"(it is a declaration) and update the shape table.",
				n, recs[i].Kind, recs[i].Subtype)
		}
	}
	cppFTEqual(t, cppFTTargetsOf(recs, "Holder.a"), nil, "a `using`-alias-typed field")
	cppFTEqual(t, cppFTTargetsOf(recs, "Holder.o"), nil, "a typedef-typed field")
	cppFTEqual(t, cppFTTargetsOf(recs, "Holder.direct"), []string{"Order"},
		"the control field in the same class")
}

// TestCppFieldTypeRefs_FriendDeclarationMintsNoRecord is the second
// not-a-declaration probe. `friend class Buddy;` names a type the file does not
// declare; if it minted a bare-named Component the way the forward declaration
// does, a field typed `Buddy` would bind to it.
func TestCppFieldTypeRefs_FriendDeclarationMintsNoRecord(t *testing.T) {
	src := `
struct Money { int cents; };

class Holder {
  friend class Buddy;
public:
  Buddy* b;
  Money m;
};
`
	recs := cppFTExtract(t, src, "friend.cpp")
	for i := range recs {
		if recs[i].Name == "Buddy" {
			t.Fatalf("`friend class Buddy;` now mints a %s/%s record. It is NOT a "+
				"declaration of Buddy: check that the definition marker refuses it.",
				recs[i].Kind, recs[i].Subtype)
		}
	}
	cppFTEqual(t, cppFTTargetsOf(recs, "Holder.b"), nil, "a field typed after a friend name")
	cppFTEqual(t, cppFTTargetsOf(recs, "Holder.m"), []string{"Money"},
		"the control field in the same class")
}

// TestCppFieldTypeRefs_AnonymousInlineTypeIsRefusedWhole grades the brace rule.
// An inline type definition in type position is not a REFERENCE to a declared
// type, and its body's member names are identifiers the scanner would otherwise
// treat as type candidates — so `struct { int Order; } anon;` would emit an
// edge to the same-file `class Order` from a MEMBER NAME. The whole expression
// is refused when it contains a brace.
func TestCppFieldTypeRefs_AnonymousInlineTypeIsRefusedWhole(t *testing.T) {
	src := `
struct Order { int id; };

class Holder {
public:
  struct { int Order; } trap;
  struct { Order inner; } nested;
  Order fine;
};
`
	recs := cppFTExtract(t, src, "anon.cpp")
	// Premise: the inline definitions really do reach the pass as brace-bearing
	// type text, or the rule under test is unreachable.
	braces := 0
	for i := range recs {
		if recs[i].Kind == "SCOPE.Schema" && recs[i].Subtype == "field" &&
			strings.ContainsRune(recs[i].Properties["field_type"], '{') {
			braces++
		}
	}
	if braces < 2 {
		t.Fatalf("only %d brace-bearing field types in this fixture; the rule "+
			"under test is unreachable", braces)
	}
	cppFTEqual(t, cppFTTargetsOf(recs, "Holder.trap"), nil,
		"an inline anonymous type whose MEMBER is named after a same-file class")
	cppFTEqual(t, cppFTTargetsOf(recs, "Holder.nested"), nil,
		"an inline anonymous type that mentions a same-file class")
	cppFTEqual(t, cppFTTargetsOf(recs, "Holder.fine"), []string{"Order"},
		"the control field in the same class")
}

// TestCppFieldTypeRefs_SelfReferenceIsRefused — `class Node { Node* next; };` is
// the commonest field shape in C++ and the CONTAINS edge already relates the two
// records; a self edge asserts nothing new.
func TestCppFieldTypeRefs_SelfReferenceIsRefused(t *testing.T) {
	src := `
struct Payload { int n; };
struct Node {
  Node* next;
  Payload p;
};
`
	recs := cppFTExtract(t, src, "node.cpp")
	cppFTEqual(t, cppFTTargetsOf(recs, "Node.next"), nil, "a self-referential field")
	cppFTEqual(t, cppFTTargetsOf(recs, "Node.p"), []string{"Payload"},
		"the control field in the same struct")
}

// TestCppFieldTypeRefs_CLanguageHeaderEmitsTheSameEdge is the `.h` row.
//
// classifier.go:389 maps `.h` to language **c**, not cpp, and the SAME extractor
// serves both (the `lang` parameter). Header-only C++ libraries therefore run
// through the C grammar under language "c" — spdlog, the only C/C++ repo of any
// size in our corpus, is 109 `.h` to 40 `.cpp`. This test pins that the pass is
// language-parameterised rather than hardcoded to "cpp": the structural ref
// carries the file's own language segment, and it must bind.
func TestCppFieldTypeRefs_CLanguageHeaderEmitsTheSameEdge(t *testing.T) {
	src := `
struct Item { int n; };
struct Holder {
  struct Item i;
  Item j;
};
`
	recs, err := extractC(src, "model/holder.h")
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	cppFTEqual(t, cppFTTargetsOf(recs, "Holder.i"), []string{"Item"},
		"a C elaborated-type field")
	cppFTEqual(t, cppFTTargetsOf(recs, "Holder.j"), []string{"Item"},
		"a C plain field")
	want := extreg.BuildComponentStructuralRef("c", "model/holder.h", "Item")
	found := false
	for i := range recs {
		for _, r := range recs[i].Relationships {
			if r.Properties.Get("ref_kind") == "field_target_type" {
				if r.ToID != want {
					t.Errorf("%s -> %q; a .h file must address its target under "+
						"language segment \"c\", want %q", recs[i].Name, r.ToID, want)
				}
				found = true
			}
		}
	}
	if !found {
		t.Fatal("no field-type edge in a C header — the assertion above is vacuous")
	}
}

// TestCppFieldTypeRefs_FunctionShapedMemberIsNeverASource is the SOURCE-endpoint
// half of the #7047/#7056 audit, and it is reproduced from the corpus rather
// than invented.
//
// emitClassFieldMembers' guard skips a field_declaration whose DIRECT child is a
// function_declarator (`void greet();`). It does not skip one wrapped in
// pointer/reference decoration, so `virtual const VideoInfo& GetVideoInfo() = 0;`
// mints a SCOPE.Schema/field named after the FUNCTION whose `field_type` is its
// RETURN type — as does a pointer-to-member-function data member
// (`AVSMap& (C::* p)();`). On the first revision of this arm, 16 of 23 corpus
// edges hung off one of those two shapes, asserting
// `ref_kind=field_target_type` about a member whose declared type is not that
// type. Both rows below are taken verbatim in shape from
// `staxrip/Source/FrameServer/avisynth.h`.
//
// The capture defect is pre-existing (#4854, filed as #7061) and is not fixed
// here; the records still exist. What is asserted is that this pass refuses them
// as SOURCES — and the premise (the bogus field entity exists, with the return
// type in field_type) is asserted FIRST so the test cannot pass because the
// capture was quietly fixed underneath it.
//
// THE DEPTH ROWS EXIST BECAUSE A COMPOUND MUTANT HID THE RECURSION. The first
// revision scored one mutant that narrowed the declarator scan "to the node
// itself", which deleted the child loop AND the recursion at once; the first
// half is lethal alone, so the recursion was never graded, and removing only the
// recursion was ALIVE against both suite and corpus. A compound grades its
// second half only if its first half is not lethal alone — the rule this arm
// applied to M3 and to the three markers, and then broke here.
func TestCppFieldTypeRefs_FunctionShapedMemberIsNeverASource(t *testing.T) {
	src := `
struct VideoInfo { int w; };
struct AVSMap { int n; };
struct Item { int n; };

class IClip {
public:
  virtual const VideoInfo& GetVideoInfo() = 0;
  virtual AVSMap* GetMap() = 0;
  void plain();
  AVSMap& (IClip::* getProperties)();
  Item** getPP();
  Item*** getPPP();
  Item*& getPR();
  Item** (*fpp)(int);
  Item data;
};
`
	recs := cppFTExtract(t, src, "clip.cpp")

	// Premise 1: the bogus field entities exist, named after the FUNCTION, with
	// the RETURN type in field_type.
	prem := map[string]string{
		"IClip.GetVideoInfo":  "VideoInfo",
		"IClip.GetMap":        "AVSMap",
		"IClip.getProperties": "AVSMap",
		// DEPTH. The decoration nests without limit and every layer is
		// ordinary C++, so a scan of the node plus its DIRECT children
		// answers depth 1 and lets all of these through. Each row puts the
		// function_declarator one layer deeper than the last, and the final
		// one mixes pointer and function nesting.
		"IClip.getPP":  "Item", // depth 2
		"IClip.getPPP": "Item", // depth 3
		"IClip.getPR":  "Item", // depth 2, pointer under reference
		"IClip.fpp":    "Item", // function pointer RETURNING a pointer
	}
	got := map[string]string{}
	for i := range recs {
		if recs[i].Kind == "SCOPE.Schema" && recs[i].Subtype == "field" {
			got[recs[i].Name] = recs[i].Properties["field_type"]
		}
	}
	for name, typ := range prem {
		g, ok := got[name]
		if !ok {
			t.Fatalf("no field entity %q — the #4854 capture defect this refusal "+
				"exists for has been fixed. Delete the refusal rather than "+
				"leaving a test that passes for a new reason.", name)
		}
		if g != typ {
			t.Fatalf("%s field_type = %q, want the RETURN type %q — the premise "+
				"of this test has moved", name, g, typ)
		}
	}
	// Premise 2: a BARE member function declaration still mints nothing, so the
	// two halves of the guard are not confused with each other.
	if _, ok := got["IClip.plain"]; ok {
		t.Fatal("`void plain();` now mints a field entity — the direct-child " +
			"guard in emitClassFieldMembers has changed")
	}

	for name := range prem {
		cppFTEqual(t, cppFTTargetsOf(recs, name), nil,
			"a field-type edge hanging off "+name)
	}
	// Positive control: a plain data member in the SAME class still binds, so
	// the refusal is not simply switching the pass off.
	cppFTEqual(t, cppFTTargetsOf(recs, "IClip.data"), []string{"Item"},
		"the plain data member in the same class")
}

// TestCppFieldTypeRefs_TemplateParameterIsNeverATarget replaces a claim of
// UNREACHABILITY that was false.
//
// The first revision asserted that go's #7041 template-parameter over-fire has
// no cpp analogue because a templated class's members emit no field entity. That
// is true of DIRECT members and false four lines further in: walkStructural's
// template arm recurses into the body, so a class NESTED in a template does get
// field entities, and `T val;` there would have bound to a same-file `struct T`.
//
// Three directions, because refusing by name is exactly the kind of rule that
// over-reaches:
//
//	Inner.val    typed by the PARAMETER                     -> no edge
//	Inner.other  typed by a real type, INSIDE the template  -> edge
//	Early.tp     typed `T` BEFORE the template              -> edge
//	Holder.tp    typed `T` AFTER  the template              -> edge
//
// BOTH sides of the template are exercised on purpose. The stamp applies to a
// RANGE of the record slice, so a range that started at 0 rather than at the
// template's own first record would silently refuse `Early.tp` — a class that
// merely happens to be declared earlier in the file. A fixture with only the
// AFTER row cannot see that, and left the range bound ungraded.
func TestCppFieldTypeRefs_TemplateParameterIsNeverATarget(t *testing.T) {
	src := `
struct T { int x; };
struct Item { int n; };

struct Early {
  T tp;
};

template<typename T> struct Outer4 {
  struct Inner4 {
    T val;
    Item other;
  };
};

class Holder {
public:
  T tp;
};
`
	recs := cppFTExtract(t, src, "tparam.cpp")

	// Premise: the nested class really does emit field entities, and the real
	// `struct T` really is a target-eligible declaration in this file.
	if _, ok := cppFTFieldType(recs, "Inner4.val"); !ok {
		t.Fatal("no field entity Inner4.val — a class nested in a template no " +
			"longer emits members and this test is vacuous")
	}
	real := false
	for i := range recs {
		if recs[i].Kind == "SCOPE.Component" && recs[i].Name == "T" {
			real = true
		}
	}
	if !real {
		t.Fatal("no SCOPE.Component named T — there is nothing for the parameter " +
			"to collide with and this test is vacuous")
	}

	cppFTEqual(t, cppFTTargetsOf(recs, "Inner4.val"), nil,
		"a nested-class member typed by the enclosing template's PARAMETER")
	cppFTEqual(t, cppFTTargetsOf(recs, "Inner4.other"), []string{"Item"},
		"a nested-class member typed by a real same-file type")
	cppFTEqual(t, cppFTTargetsOf(recs, "Early.tp"), []string{"T"},
		"a member typed `T` in a class declared BEFORE the template")
	cppFTEqual(t, cppFTTargetsOf(recs, "Holder.tp"), []string{"T"},
		"a member typed `T` in a class declared AFTER the template")
}

// TestCppTemplateParams_EnumeratesTheParameterFormSpace ENUMERATES the template
// parameter forms rather than sampling them.
//
// The round-2 fixture varied the POSITIONAL axis four ways — outside any
// template, before it, inside it, two deep — and held the parameter-list
// CONTENTS constant at a single bare `typename T`. An independent review found
// five forms the collector got wrong behind that constant: three that bound
// NOTHING (so #7041's wrong binding stayed live) and two that bound the DEFAULT
// ARGUMENT or the parameter's own TYPE/CONSTRAINT (so a correct edge was
// silently deleted). Same varied/held-constant defect as the round-1 findings,
// one level down.
//
// NONE OF THESE FORMS OCCURS IN OUR CORPUS. That is a corpus-relative zero, not
// a bound on the language, and it is not a reason to leave any of them wrong.
//
// The names are read from the field metadata #6912 actually consumes, so this
// grades the whole path rather than the helper in isolation. Every row is legal
// C++ and the parameter is deliberately named `Order`, the name of a real
// declaration in the same file, so a miss is a wrong binding and an over-collect
// is a deleted edge.
func TestCppTemplateParams_EnumeratesTheParameterFormSpace(t *testing.T) {
	cases := []struct {
		label string
		param string
		want  []string
	}{
		{"type parameter, typename", "typename Order", []string{"Order"}},
		{"type parameter, class", "class Order", []string{"Order"}},
		{"type parameter with a DEFAULT naming a real type",
			"typename Order = Item", []string{"Order"}},
		{"non-type parameter", "int Order", []string{"Order"}},
		{"non-type parameter with a default", "int Order = 4", []string{"Order"}},
		{"non-type parameter whose TYPE is a real same-file type",
			"Item* Order", []string{"Order"}},
		{"constrained type parameter (the CONSTRAINT is a real name)",
			"Cc Order", []string{"Order"}},
		{"template-template parameter",
			"template<typename> class Order", []string{"Order"}},
		{"template-template parameter with a default",
			"template<typename> class Order = Tmpl", []string{"Order"}},
		{"type parameter pack", "typename... Order", []string{"Order"}},
		{"non-type parameter pack", "int... Order", []string{"Order"}},
		{"TWO parameters, both with defaults naming real types",
			"typename Order = Item, typename Second = Money", []string{"Order", "Second"}},
		{"unnamed parameter binds nothing", "typename", nil},
		// The row that grades the NESTED-LIST SKIP on its own. With the outer
		// template-template parameter left unnamed, the only name anywhere in
		// the declaration is the INNER list's — which is not in scope for the
		// outer template's body. A walk that did not skip the inner list would
		// bind `Order` here and refuse a correct edge to `struct Order`.
		// Not named by the review; found by enumerating the forms.
		{"unnamed template-template parameter with a NAMED inner parameter",
			"template<typename Order> class", nil},
	}
	for _, c := range cases {
		src := `
struct Order { int id; };
struct Item { int n; };
struct Money { int cents; };
template<class Z> concept Cc = true;
template<typename Y> struct Tmpl { int y; };

template<` + c.param + `> struct Outer6 {
  struct Inner6 { int x; };
};
`
		recs := cppFTExtract(t, src, "forms.cpp")
		var got []string
		found := false
		for i := range recs {
			if recs[i].Name != "Inner6.x" {
				continue
			}
			found = true
			if recs[i].Metadata != nil {
				got, _ = recs[i].Metadata["template_params"].([]string)
			}
		}
		if !found {
			t.Errorf("%s: no Inner6.x field entity — the row is vacuous", c.label)
			continue
		}
		cppFTEqual(t, got, c.want, "template<"+c.param+"> binds")
	}
}

// TestCppFieldTypeRefs_TemplateParameterFormsAreAllRefused is the EDGE-level
// half of the enumeration above: for every form whose parameter name can appear
// in a member's type TEXT, the parameter must produce no edge while a real
// same-file type in the same class still does.
//
// The member expressions differ per form because C++ requires it — a type
// parameter can be written bare, a non-type parameter only inside a template
// argument, a template-template parameter only when applied — and getting that
// wrong is how a row ends up grading nothing. Each is legal as written.
func TestCppFieldTypeRefs_TemplateParameterFormsAreAllRefused(t *testing.T) {
	cases := []struct {
		label  string
		param  string
		member string
	}{
		{"type parameter", "typename Order", "Order v;"},
		{"type parameter with a default", "typename Order = Item", "Order v;"},
		{"constrained type parameter", "Cc Order", "Order v;"},
		{"non-type parameter", "int Order", "std::array<int, Order> v;"},
		{"non-type parameter with a default", "int Order = 4", "std::array<int, Order> v;"},
		{"template-template parameter", "template<typename> class Order", "Order<int> v;"},
		{"template-template parameter with a default",
			"template<typename> class Order = Tmpl", "Order<int> v;"},
		{"type parameter pack", "typename... Order", "std::tuple<Order...> v;"},
		{"non-type parameter pack", "int... Order",
			"std::array<int, sizeof...(Order)> v;"},
	}
	for _, c := range cases {
		src := `
#include <array>
#include <tuple>

struct Order { int id; };
struct Item { int n; };
template<class Z> concept Cc = true;
template<typename Y> struct Tmpl { int y; };

template<` + c.param + `> struct Outer7 {
  struct Inner7 {
    ` + c.member + `
    Item real;
  };
};
`
		recs := cppFTExtract(t, src, "forms2.cpp")
		if _, ok := cppFTFieldType(recs, "Inner7.v"); !ok {
			t.Errorf("%s: no Inner7.v field entity — the row is vacuous", c.label)
			continue
		}
		cppFTEqual(t, cppFTTargetsOf(recs, "Inner7.v"), nil,
			c.label+": a member whose type names the PARAMETER")
		cppFTEqual(t, cppFTTargetsOf(recs, "Inner7.real"), []string{"Item"},
			c.label+": the real-type control in the same nested class")
	}
}

// TestCppFieldTypeRefs_NestedTemplateParametersAreAllRefused grades the reason
// stampCppTemplateParams APPENDS rather than overwrites. A class nested two
// templates deep is in scope of BOTH parameter lists; the inner template stamps
// first and the outer's call covers the inner's output, so an overwrite would
// silently drop the inner list and re-open the collision for `U` alone.
//
// Both parameters have a same-named real struct in the file, so each row fails
// on its own rather than one masking the other, and a real type is bound in the
// same nested class as a positive control.
func TestCppFieldTypeRefs_NestedTemplateParametersAreAllRefused(t *testing.T) {
	src := `
struct T { int x; };
struct U { int y; };
struct Item { int n; };

template<typename T> struct Outer5 {
  template<typename U> struct Middle5 {
    struct Inner5 {
      T fromOuter;
      U fromInner;
      Item real;
    };
  };
};
`
	recs := cppFTExtract(t, src, "nested.cpp")
	for _, n := range []string{"Inner5.fromOuter", "Inner5.fromInner", "Inner5.real"} {
		if _, ok := cppFTFieldType(recs, n); !ok {
			t.Fatalf("no field entity %q — the nesting premise of this test is gone", n)
		}
	}
	cppFTEqual(t, cppFTTargetsOf(recs, "Inner5.fromOuter"), nil,
		"a member typed by the OUTER template's parameter")
	cppFTEqual(t, cppFTTargetsOf(recs, "Inner5.fromInner"), nil,
		"a member typed by the INNER template's parameter")
	cppFTEqual(t, cppFTTargetsOf(recs, "Inner5.real"), []string{"Item"},
		"a member of the same nested class typed by a real same-file type")
}

// cppFTFieldType returns a field entity's declared type and whether it exists.
func cppFTFieldType(recs []types.EntityRecord, name string) (string, bool) {
	for i := range recs {
		if recs[i].Name == name && recs[i].Kind == "SCOPE.Schema" && recs[i].Subtype == "field" {
			return recs[i].Properties["field_type"], true
		}
	}
	return "", false
}

// TestCppFieldTypeRefs_SelfReferenceComparisonIsCaseSensitive grades the ONE
// axis the self-reference rule was untested on. C++ identifiers are
// case-sensitive, so `struct money {};` and `struct Money {};` are two different
// types and a field of the first inside the second is NOT a self-reference. A
// case-INSENSITIVE comparison (arm H folds, because PHP class names fold) would
// silently delete this edge.
func TestCppFieldTypeRefs_SelfReferenceComparisonIsCaseSensitive(t *testing.T) {
	src := `
struct money { int cents; };
struct Money {
  money m;
  Money* self;
};
`
	recs := cppFTExtract(t, src, "case.cpp")
	cppFTEqual(t, cppFTTargetsOf(recs, "Money.m"), []string{"money"},
		"a field whose type differs from its owner ONLY in case")
	cppFTEqual(t, cppFTTargetsOf(recs, "Money.self"), nil,
		"the genuine self-reference in the same struct")
}

// TestCppFieldTypeRefs_ConstructorDoesNotSuppressItsClass is the measured reason
// this arm does NOT inherit arm D's all-kinds ambiguity rule, and it is a
// property of C++ rather than a corpus accident.
//
// A constructor is NAMED AFTER ITS CLASS. extractFunction emits an inline
// `IClip() { }` as SCOPE.Operation named `IClip`, in the same file as the
// SCOPE.Component named `IClip`. Arm D's rule — count every same-file kind —
// therefore refuses a target for EVERY class that defines a constructor inline,
// which in header-heavy C++ is most of them. The resolving tier does not care:
// SCOPE.Operation is not in componentKindFamily, so lookupLocationKind answers
// at tier 1 without hesitation.
//
// The edge is driven through the REAL resolver here, not merely counted, because
// the claim under test is about what the resolver weighs.
func TestCppFieldTypeRefs_ConstructorDoesNotSuppressItsClass(t *testing.T) {
	src := `
class IClip {
public:
  IClip() { }
  ~IClip() { }
  int n;
};

class PClip {
public:
  IClip* p;
};
`
	recs := cppFTExtract(t, src, "clip.cpp")

	// Premise: the same-named Operation really is there, or the rule this test
	// distinguishes is not exercised.
	op := false
	for i := range recs {
		if recs[i].Kind == "SCOPE.Operation" && recs[i].Name == "IClip" {
			op = true
		}
	}
	if !op {
		t.Fatal("no same-named SCOPE.Operation for the constructor — arm D's rule " +
			"and this arm's agree on this fixture and the test is vacuous")
	}

	cppFTEqual(t, cppFTTargetsOf(recs, "PClip.p"), []string{"IClip"},
		"a field typed after a class that defines a constructor inline")

	for i := range recs {
		if recs[i].ID == "" {
			recs[i].ID = graph.EntityID("issue6912", recs[i].Kind, recs[i].Name, recs[i].SourceFile)
		}
	}
	idToRec := map[string]*types.EntityRecord{}
	for i := range recs {
		idToRec[recs[i].ID] = &recs[i]
	}
	idx := resolve.BuildIndex(recs)
	resolve.ReferencesEmbedded(recs, idx)
	n := 0
	for i := range recs {
		for _, r := range recs[i].Relationships {
			if r.Properties.Get("ref_kind") != "field_target_type" {
				continue
			}
			n++
			tgt, ok := idToRec[r.ToID]
			if !ok {
				t.Fatalf("%s -> %q did not bind", recs[i].Name, r.ToID)
			}
			if tgt.Kind != "SCOPE.Component" {
				t.Fatalf("%s bound to a %s/%s, not the class", recs[i].Name, tgt.Kind, tgt.Subtype)
			}
		}
	}
	if n != 1 {
		t.Fatalf("%d field-type edges, want 1 — the resolver drive is vacuous", n)
	}
}

// TestCppFieldTypeRefs_ResolvesToEntityIDs drives every fixture edge through the
// REAL resolver and asserts each binds, NAMING the entity it binds to. This is
// what makes "a non-binding edge is worse than no edge" a graded property, and
// what makes the ADDRESS DIALECT graded: the rival file declares the same bare
// names, so a bare-name ToID would be ambiguous.
func TestCppFieldTypeRefs_ResolvesToEntityIDs(t *testing.T) {
	recs := cppFTExtract(t, cppFTSrc, cppFTPath)
	rival := cppFTExtract(t, `
struct Item { int n; };
class Order { public: int id; };
union Money { int cents; };
struct Empty {};
struct TP { int x; };
`, "model/other.cpp")

	all := append(append([]types.EntityRecord{}, recs...), rival...)
	for i := range all {
		if all[i].ID == "" {
			all[i].ID = graph.EntityID("issue6912", all[i].Kind, all[i].Name, all[i].SourceFile)
		}
	}
	idToName := map[string]string{}
	for i := range all {
		idToName[all[i].ID] = all[i].SourceFile + ":" + all[i].Kind + ":" + all[i].Name
	}
	idx := resolve.BuildIndex(all)

	// Positive control on the address dialect: the BARE name must be ambiguous
	// across the graph, or the structural address is ungraded here.
	if _, st := idx.LookupStatusHint("Order", "REFERENCES"); st == 1 {
		t.Fatalf("the bare name %q binds in this fixture, so the ambiguity this "+
			"test controls for is absent and the structural address is ungraded",
			"Order")
	}

	resolve.ReferencesEmbedded(all, idx)
	var bound []string
	for i := range all {
		for _, r := range all[i].Relationships {
			if r.Kind != "REFERENCES" || r.Properties.Get("ref_kind") != "field_target_type" {
				continue
			}
			name, ok := idToName[r.ToID]
			if !ok {
				t.Errorf("%s:%s -[REFERENCES]-> %q did NOT bind to an entity id",
					all[i].SourceFile, all[i].Name, r.ToID)
				continue
			}
			bound = append(bound, all[i].Name+" => "+name)
		}
	}
	sort.Strings(bound)
	const m = "model/holder.cpp:SCOPE.Component:"
	want := []string{
		"Holder.a => " + m + "Item",
		"Holder.arr => " + m + "Order",
		"Holder.b => " + m + "Item",
		"Holder.constref => " + m + "Order",
		"Holder.elaborated => " + m + "Item",
		"Holder.empty => " + m + "Empty",
		"Holder.mapped => " + m + "Item",
		"Holder.mapped => " + m + "Order",
		"Holder.money => " + m + "Money",
		"Holder.paired => " + m + "Item",
		"Holder.paired => " + m + "Order",
		"Holder.plain => " + m + "Order",
		"Holder.ptr => " + m + "Order",
		"Holder.ref => " + m + "Order",
		"Holder.templated => " + m + "Order",
		"Holder.tp => " + m + "TP",
		"Holder.uptr => " + m + "Order",
		"Holder.vec => " + m + "Order",
		"Holder.viaUsingNs => " + m + "Order",
	}
	cppFTEqual(t, bound, want, "resolved field-type endpoints")
}

// TestCppFieldTypeRefs_PropertiesAgreeWithTheEndpoints — the three edge
// properties must agree with the entity they hang off rather than introducing a
// fourth spelling, FromID must stay empty so graph assembly anchors the edge on
// the FIELD (the whole point of arm B's correction), and the ToID must be a
// same-file component structural ref.
func TestCppFieldTypeRefs_PropertiesAgreeWithTheEndpoints(t *testing.T) {
	recs := cppFTExtract(t, cppFTSrc, cppFTPath)
	n := 0
	for i := range recs {
		r := &recs[i]
		if r.Kind != "SCOPE.Schema" || r.Subtype != "field" {
			continue
		}
		owner := r.Properties["parent_class"]
		for _, rel := range r.Relationships {
			if rel.Kind != "REFERENCES" || rel.Properties.Get("ref_kind") != "field_target_type" {
				continue
			}
			n++
			fn := rel.Properties.Get("field_name")
			if owner+"."+fn != r.Name {
				t.Errorf("%s carries field_name=%q with parent_class=%q; the two "+
					"must rebuild the entity Name exactly", r.Name, fn, owner)
			}
			if rel.Properties.Get("target_type") == "" {
				t.Errorf("%s -> %s has an empty target_type", r.Name, rel.ToID)
			}
			if rel.FromID != "" {
				t.Errorf("%s -> %s sets FromID=%q; it must stay empty so graph "+
					"assembly anchors the edge on the field record", r.Name, rel.ToID, rel.FromID)
			}
			if !strings.HasPrefix(rel.ToID, "scope:component:class:cpp:"+cppFTPath+":") {
				t.Errorf("%s -> %q is not a same-file cpp component structural ref",
					r.Name, rel.ToID)
			}
		}
	}
	if n == 0 {
		t.Fatal("no field-type edges in the fixture — every assertion above is vacuous")
	}
}

// TestCppFieldTypeRefs_TargetTypeIsTheDECLAREDSpelling pins that `target_type`
// names the declaration rather than echoing the field's own type text — the two
// differ for every wrapped type, so an implementation that echoed the property
// would pass every count-based assertion and still be wrong.
func TestCppFieldTypeRefs_TargetTypeIsTheDeclaredSpelling(t *testing.T) {
	recs := cppFTExtract(t, cppFTSrc, cppFTPath)
	for i := range recs {
		if recs[i].Name != "Holder.vec" {
			continue
		}
		if got := recs[i].Properties["field_type"]; got != "std::vector<Order>" {
			t.Fatalf("Holder.vec field_type = %q — the premise of this test is gone", got)
		}
		for _, r := range recs[i].Relationships {
			if r.Properties.Get("ref_kind") != "field_target_type" {
				continue
			}
			if got := r.Properties.Get("target_type"); got != "Order" {
				t.Errorf("Holder.vec target_type = %q, want the DECLARED name %q",
					got, "Order")
			}
			return
		}
	}
	t.Fatal("no field-type edge on Holder.vec — this test is vacuous")
}
