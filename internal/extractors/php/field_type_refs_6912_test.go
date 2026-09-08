package php_test

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

// #6912 arm H — PHP field→declared-type REFERENCES edges.
//
// The fixture below is the arm's shape-space enumeration. Every row is a
// property declaration PHP will actually compile — which is a WEAKER and more
// honest claim than the one this comment carried in the first revision.
//
// That revision said every row was "legal PHP 8.1, checked shape by shape
// against the grammar". It was neither: `public callable $cb;` was in it, and
// **`callable` is not permitted as a property type in any PHP version** (typed
// properties exclude `void` and `callable`, the latter because it is
// context-dependent), so that row was a fatal error rather than a program. It
// has been replaced by `public object $obj;`, which exercises the same axis —
// a builtin refused by the declaration gate — and is legal. The version label
// was wrong too: `(Customer&Shipper)|null` is a PHP **8.2** DNF type, not 8.1.
// Recorded rather than quietly corrected, because a fixture row that the
// language does not permit is the third instance of this defect on this board
// (an F# arm shipped `Mut: mutable Customer`), and the pattern is that the
// CLAIM of having checked is what goes unchecked.
//
// AXES VARIED, one per property, holding the others constant:
//   - type composition: bare, nullable `?T`, union `A|B`, intersection `A&B`,
//     union-with-primitive, DNF `(A&B)|null` (PHP 8.2)
//   - name qualification: bare, relative `Ns\T`, fully-qualified `\Ns\T`,
//     leading-separator-only `\T`
//   - target existence: declared in THIS file / declared in ANOTHER file /
//     declared nowhere
//   - target kind: class, interface, trait, enum, import placeholder, file
//     carrier, AND — the case that is both refused and admitted at once — a
//     name carried by an admitted class AND a refused import placeholder in the
//     same file (`Order2.carrier`, see
//     TestPhpFieldTypeRefs_ImportPlaceholderSharingAClassNameIsOneNode).
//     The `namespace` row lives in the imports.php fixture instead, because
//     `namespace App;` puts its record in whichever file declares it and
//     TestPhpFieldTypeRefs_ImportPlaceholderIsNeverATarget is where the
//     empty-Subtype refusal is graded.
//   - PHP builtin: int, string, array, iterable, object, mixed, self
//   - declaration site: class property, promoted constructor parameter,
//     multi-declarator property, untyped property
//   - identifier case: declared spelling vs. lower-cased use
//
// AXES HELD CONSTANT: one file per fixture (same-file binding is the arm's
// promise and its cross-file direction is graded by the unit test, not here);
// one declaring class per fixture where a second would confuse the owner
// self-reference rule; property visibility (public) except where promotion
// requires otherwise; no framework/ORM annotations (the custom lane is gated off
// by default and is not this pass's input).
const phpFTPath = "models.php"

const phpFTSrc = `<?php
namespace App;

use Ship\Thing;
use Other\Order;

interface Shipper {}
trait Loggable {}
class Money {}
class Customer {}
class Ship {}

class Order2 {
    public Customer $buyer;
    public ?Customer $maybeBuyer = null;
    public Customer|Money $either;
    public Customer&Shipper $both;
    public (Customer&Shipper)|null $dnf;
    public Money|int $mixedUnion;
    public customer $folded;
    public Order $imported;
    public Other\Order $relative;
    public \Other\Order $qualified;
    public \Money $globalMoney;
    public Nowhere $missing;
    public Loggable $trait;
    public int $qty;
    public string $label;
    public array $rows;
    public iterable $it;
    public object $obj;
    public mixed $any;
    public Ship $carrier;
    public self $me;
    public static $untyped;
    public Money $m1, $m2;
    public function __construct(
        public readonly Customer $owner,
        private ?Money $amount,
        protected int $count = 0,
    ) {}
}
`

// phpFTOne extracts one PHP file through the registered extractor.
func phpFTOne(t *testing.T, path, src string) []types.EntityRecord {
	t.Helper()
	tree := parseForTest(t, src)
	ext, ok := extractor.Get("php")
	if !ok {
		t.Fatal("php extractor not registered")
	}
	got, err := ext.Extract(context.Background(), extractor.FileInput{
		Path: path, Content: []byte(src), Language: "php", TSTree: tree,
	})
	if err != nil {
		t.Fatalf("extract %s: %v", path, err)
	}
	return got
}

// phpFTTargetsOf returns the sorted target_type values of the field-type edges
// carried by the named field entity.
func phpFTTargetsOf(recs []types.EntityRecord, fieldName string) []string {
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

func phpFTEqual(t *testing.T, got, want []string, what string) {
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

// TestPhpFieldTypeRefs_FieldTypeIsVerbatim pins the premise the whole arm rests
// on: phpDeclaredType returns the VERBATIM source span, so the property can be
// tokenised instead of stashed from the AST the way arm C had to. If this ever
// becomes a lossy projection (swift's `field_type` is the first pre-order
// type_identifier and yields `Dictionary` for `Dictionary<String, Order>`), the
// scanner below is reading a lie and this test is the one that says so.
func TestPhpFieldTypeRefs_FieldTypeIsVerbatim(t *testing.T) {
	recs := phpFTOne(t, phpFTPath, phpFTSrc)
	got := map[string]string{}
	for i := range recs {
		if recs[i].Kind == "SCOPE.Schema" && recs[i].Subtype == "field" {
			got[recs[i].Name] = recs[i].Properties["field_type"]
		}
	}
	want := map[string]string{
		"Order2.buyer":       "Customer",
		"Order2.maybeBuyer":  "?Customer",
		"Order2.either":      "Customer|Money",
		"Order2.both":        "Customer&Shipper",
		"Order2.mixedUnion":  "Money|int",
		"Order2.relative":    "Other\\Order",
		"Order2.qualified":   "\\Other\\Order",
		"Order2.globalMoney": "\\Money",
		"Order2.qty":         "int",
		"Order2.rows":        "array",
		"Order2.me":          "self",
		"Order2.untyped":     "",
		"Order2.m1":          "Money",
		"Order2.m2":          "Money",
		"Order2.owner":       "Customer",
		"Order2.amount":      "?Money",
		"Order2.count":       "int",
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

// TestPhpFieldTypeRefs_ShapeSpace ENUMERATES the type-expression shape space
// rather than sampling it: every property of the fixture class appears here with
// the exact target set it must produce. Hand-picked attacks miss holes; a table
// that must list every row makes an unlisted shape a compile-time-visible gap.
func TestPhpFieldTypeRefs_ShapeSpace(t *testing.T) {
	recs := phpFTOne(t, phpFTPath, phpFTSrc)

	// Every field entity in the fixture must appear in the table below, so a
	// property added to the fixture without a row here fails rather than
	// silently going ungraded.
	want := map[string][]string{
		// --- bound: the type is declared in THIS file ---
		"Order2.buyer":      {"Customer"},
		"Order2.maybeBuyer": {"Customer"},            // nullable names one type
		"Order2.either":     {"Customer", "Money"},   // a union names BOTH
		"Order2.both":       {"Customer", "Shipper"}, // an intersection names BOTH
		// `(Customer&Shipper)|null` — a PHP 8.2 DNF type. NO edge, and NOT
		// because this pass refuses it: phpDeclaredType captures nothing at all
		// for the shape, so field_type is "" (and the Signature loses the type
		// too). A CAPTURE gap that predates this arm, made visible by
		// enumerating the shape space rather than sampling it, and pinned as a
		// known ceiling by TestPhpFieldTypeRefs_DNFTypeIsAKnownCaptureCeiling.
		"Order2.dnf":        nil,
		"Order2.mixedUnion": {"Money"},    // `int` is not declared here
		"Order2.folded":     {"Customer"}, // PHP class names fold case
		"Order2.trait":      {"Loggable"}, // a trait IS an admitted target
		"Order2.m1":         {"Money"},    // multi-declarator, first
		"Order2.m2":         {"Money"},    // multi-declarator, second
		"Order2.owner":      {"Customer"}, // promoted ctor parameter
		"Order2.amount":     {"Money"},    // promoted + nullable
		// The one case where the allow-list must ADMIT rather than refuse: this
		// file declares BOTH `class Ship {}` and `use Ship\Thing;`, so the name
		// is carried by an admitted target AND a refused placeholder at once.
		// Safe only because graph.EntityID omits Subtype — pinned by
		// TestPhpFieldTypeRefs_ImportPlaceholderSharingAClassNameIsOneNode.
		"Order2.carrier": {"Ship"},

		// --- refused: qualified names are never reduced to a segment ---
		"Order2.imported":    nil, // `use Other\Order` — Order is NOT declared here
		"Order2.relative":    nil, // Other\Order
		"Order2.qualified":   nil, // \Other\Order
		"Order2.globalMoney": nil, // \Money is the GLOBAL Money, not App\Money

		// --- refused: nothing declares the name ---
		"Order2.missing": nil,

		// --- refused: PHP builtins, by the declaration gate and no blocklist ---
		"Order2.qty":     nil,
		"Order2.label":   nil,
		"Order2.rows":    nil,
		"Order2.it":      nil,
		"Order2.obj":     nil,
		"Order2.any":     nil,
		"Order2.count":   nil,
		"Order2.me":      nil, // `self` names the owner; refused twice over
		"Order2.untyped": nil, // no declared type at all
	}

	seen := map[string]bool{}
	for i := range recs {
		r := &recs[i]
		if r.Kind != "SCOPE.Schema" || r.Subtype != "field" {
			continue
		}
		seen[r.Name] = true
		w, ok := want[r.Name]
		if !ok {
			t.Errorf("field %q is in the fixture but has no row in the shape "+
				"table — add one rather than leaving the shape ungraded", r.Name)
			continue
		}
		phpFTEqual(t, phpFTTargetsOf(recs, r.Name), w, r.Name)
	}
	for name := range want {
		if !seen[name] {
			t.Errorf("the shape table names %q but the fixture emits no such "+
				"field entity — the row is vacuous", name)
		}
	}
}

// TestPhpFieldTypeRefs_DNFTypeIsAKnownCaptureCeiling pins a gap this arm found
// and deliberately did NOT close. It is filed as **#7044** — see that issue for
// the description; this test exists only to make the ceiling fail loudly when it
// is closed.
//
// In one line: PHP 8.2's disjunctive-normal-form type `(A&B)|null` is not one of
// the node types phpDeclaredType (field_members.go:135) switches on, so it
// captures NOTHING — field_type is "" and the Signature drops the type too. A
// capture defect in the property producer, not an edge defect, which is why it
// is a separate issue rather than a rider on this arm.
//
// The test asserts the CURRENT behaviour so the ceiling is visible and so that
// fixing the capture makes this test fail loudly, at which point the shape-space
// row above becomes a real binding row.
func TestPhpFieldTypeRefs_DNFTypeIsAKnownCaptureCeiling(t *testing.T) {
	src := `<?php
interface Shipper {}
class Customer {}
class Order {
    public (Customer&Shipper)|null $dnf;
    public Customer $control;
}
`
	recs := phpFTOne(t, "dnf.php", src)
	var got string
	found := false
	for i := range recs {
		if recs[i].Name == "Order.dnf" {
			found = true
			got = recs[i].Properties["field_type"]
		}
	}
	if !found {
		t.Fatal("no field entity for the DNF property — the fixture premise is gone")
	}
	if got != "" {
		t.Errorf("phpDeclaredType now captures the DNF type as %q. The capture "+
			"ceiling is closed: delete this test and give Order2.dnf a real "+
			"target row in the shape table.", got)
	}
	phpFTEqual(t, phpFTTargetsOf(recs, "Order.dnf"), nil, "a DNF-typed field")
	phpFTEqual(t, phpFTTargetsOf(recs, "Order.control"), []string{"Customer"},
		"the control field in the same class")
}

// TestPhpFieldTypeRefs_ImportPlaceholderIsNeverATarget is the wrong-binding kill.
//
// useImportRecord (php.go:650) mints a SCOPE.Component whose Name is the TOP
// namespace segment of the `use`, so `use Ship\Thing;` puts a Component literally
// named `Ship` in this file — and a component-space ref to `Ship` BINDS to it
// (proved by TestPhpFieldTypeRefs_ResolvesToEntityIDs' harness). Without the
// subtype allow-list a field typed `Ship` binds to an import placeholder: a
// confident wrong answer, which nothing downstream reports because it binds.
//
// THE FILE MUST NOT DECLARE A CLASS OF THAT NAME, or the ambiguity rule would
// mask the allow-list and this test would pass for the wrong reason.
func TestPhpFieldTypeRefs_ImportPlaceholderIsNeverATarget(t *testing.T) {
	src := `<?php
namespace App;

use Ship\Thing;

class Money {}

class Order {
    public Ship $carrier;
    public App $ns;
    public Money $price;
}
`
	recs := phpFTOne(t, "imports.php", src)

	// Premise: the import placeholder really is in this file's record set under
	// the bare name, so the refusal below is doing work.
	placeholder := false
	for i := range recs {
		if recs[i].Kind == "SCOPE.Component" && recs[i].Name == "Ship" &&
			recs[i].SourceFile == "imports.php" {
			placeholder = true
			if recs[i].Subtype != "" {
				t.Fatalf("the import placeholder now carries Subtype %q — the "+
					"allow-list's discriminator has moved", recs[i].Subtype)
			}
		}
	}
	if !placeholder {
		t.Fatal("no bare-named import placeholder in this file — the refusal " +
			"under test is unreachable and this test is vacuous")
	}
	// Nothing declares `class Ship` here, so rule 2 cannot be the reason.
	for i := range recs {
		if recs[i].Kind == "SCOPE.Component" && recs[i].Name == "Ship" && recs[i].Subtype == "class" {
			t.Fatal("the fixture declares class Ship, which lets the ambiguity " +
				"rule mask the allow-list")
		}
	}

	// The NAMESPACE row of the target-kind axis. buildNamespace (php.go:483)
	// mints its own bare SCOPE.Component named after the namespace ROOT with an
	// empty Subtype — the same shape as an import placeholder but a different
	// producer — and the axis previously claimed this row while no test drove a
	// field at it. Premise asserted first, again so the refusal is doing work.
	ns := false
	for i := range recs {
		if recs[i].Kind == "SCOPE.Component" && recs[i].Name == "App" &&
			recs[i].SourceFile == "imports.php" && recs[i].Subtype == "" {
			ns = true
		}
	}
	if !ns {
		t.Fatal("no bare-named namespace record in this file — buildNamespace's " +
			"shape has changed and the namespace row of the axis is vacuous")
	}

	phpFTEqual(t, phpFTTargetsOf(recs, "Order.carrier"), nil,
		"a field typed after an import placeholder")
	phpFTEqual(t, phpFTTargetsOf(recs, "Order.ns"), nil,
		"a field typed after the file's own namespace root")
	// Positive control in the SAME file: the pass works here.
	phpFTEqual(t, phpFTTargetsOf(recs, "Order.price"), []string{"Money"},
		"the control field in the same class")
}

// TestPhpFieldTypeRefs_UnqualifiedUseImportIsNeverATarget is the SHARPER half of
// the wrong-binding kill, and it is REPRODUCED FROM THE CORPUS rather than
// invented.
//
// `use Ship\Thing;` mints a placeholder named after the namespace ROOT, which
// only collides with a field type by coincidence. `use Closure;` — an
// unqualified use of a global class, which is how every PHP file that touches a
// closure is written — has NO backslash, so useImportRecord's `top` IS the whole
// class name: the placeholder is named EXACTLY what the field types are.
//
// The live instance:
//
//	laravel-routing/src/Illuminate/Routing/Controllers/Middleware.php
//	  use Closure;
//	  public function __construct(public Closure|string|array $middleware, ...)
//
// A promoted constructor parameter, a union type, and an import placeholder
// named `Closure` in the same file. Without phpTypeDeclSubtypes this pass emits
// `Middleware.middleware -> Closure` and it BINDS — the one edge the whole
// 390-file corpus produces without the allow-list, and it is wrong. Measured, not
// hypothesised.
func TestPhpFieldTypeRefs_UnqualifiedUseImportIsNeverATarget(t *testing.T) {
	src := `<?php
namespace Illuminate\Routing\Controllers;

use Closure;

class Money {}

class Middleware
{
    public function __construct(
        public Closure|string|array $middleware,
        public Money $price,
    ) {}
}
`
	recs := phpFTOne(t, "middleware.php", src)
	saw := false
	for i := range recs {
		if recs[i].Kind == "SCOPE.Component" && recs[i].Name == "Closure" &&
			recs[i].SourceFile == "middleware.php" && recs[i].Subtype == "" {
			saw = true
		}
	}
	if !saw {
		t.Fatal("no bare `Closure` import placeholder — useImportRecord's " +
			"single-segment `use` behaviour has changed and this test is vacuous")
	}
	phpFTEqual(t, phpFTTargetsOf(recs, "Middleware.middleware"), nil,
		"a promoted parameter typed after an unqualified `use` import")
	phpFTEqual(t, phpFTTargetsOf(recs, "Middleware.price"), []string{"Money"},
		"the promoted-parameter control in the same constructor")
}

// TestPhpFieldTypeRefs_ImportPlaceholderSharingAClassNameIsOneNode pins the
// premise that makes the two tests above SAFE, and which nothing pinned before.
//
// `models.php` declares BOTH `class Ship {}` and `use Ship\Thing;`, so the name
// `Ship` is carried by an admitted target AND a refused placeholder in ONE file.
// This is the case where the allow-list must ADMIT — and the edge only binds
// because `graph.EntityID` hashes `(repo, Kind, Name, SourceFile)` with
// **Subtype excluded**, so the two records collapse to ONE node rather than
// tripping tier 2's kind-agnostic `ambigLocation` (`refs.go:1302-1315`), which
// is what a two-node collision at one (file, name) would do.
//
// The premise is asserted FIRST — two distinct records, one computed ID — so the
// binding assertion cannot pass because the second record silently stopped being
// emitted. If `EntityID` ever starts hashing Subtype, this test fails here
// rather than the arm quietly beginning to dangle.
func TestPhpFieldTypeRefs_ImportPlaceholderSharingAClassNameIsOneNode(t *testing.T) {
	recs := phpFTOne(t, phpFTPath, phpFTSrc)

	var placeholder, class *types.EntityRecord
	for i := range recs {
		r := &recs[i]
		if r.Kind != "SCOPE.Component" || r.Name != "Ship" || r.SourceFile != phpFTPath {
			continue
		}
		switch r.Subtype {
		case "":
			placeholder = r
		case "class":
			class = r
		}
	}
	if placeholder == nil || class == nil {
		t.Fatalf("the two-record premise is gone (placeholder=%v class=%v) — "+
			"models.php must declare BOTH `use Ship\\Thing;` and `class Ship {}` "+
			"or this test grades nothing", placeholder != nil, class != nil)
	}
	pid := graph.EntityID("issue6912", placeholder.Kind, placeholder.Name, placeholder.SourceFile)
	cid := graph.EntityID("issue6912", class.Kind, class.Name, class.SourceFile)
	if pid != cid {
		t.Fatalf("the import placeholder and the class no longer collapse to one "+
			"node (%s vs %s). graph.EntityID must exclude Subtype for the "+
			"admitted-and-refused-at-once case to bind rather than dangle.", pid, cid)
	}

	// One edge, to the class, and it binds.
	phpFTEqual(t, phpFTTargetsOf(recs, "Order2.carrier"), []string{"Ship"},
		"a field typed at a name carried by both an admitted class and a placeholder")
	phpFTAssertAllBind(t, recs)
}

// TestPhpFieldTypeRefs_EnumIsNotATarget records a MEASUREMENT, not a preference.
//
// A PHP enum with at least one case is minted TWICE in the same file — as
// SCOPE.Schema/enum by buildEnum and as a SCOPE.Enum value-set by
// buildEnumValueSet — and neither Kind is in componentKindFamily. A
// component-space ref therefore misses lookupLocationKind and lands on
// ambigLocation as AMBIGUOUS: emitting it would ship a guaranteed dangling stub.
// The endpoint half of that claim is driven through the real resolver in
// TestPhpFieldTypeRefs_EnumTargetWouldDangle.
func TestPhpFieldTypeRefs_EnumIsNotATarget(t *testing.T) {
	src := `<?php
namespace App;

enum Status: string { case Open = 'open'; case Shut = 'shut'; }
class Money {}

class Order {
    public Status $status;
    public Money $price;
}
`
	recs := phpFTOne(t, "enums.php", src)
	kinds := map[string]bool{}
	for i := range recs {
		if recs[i].Name == "Status" {
			kinds[recs[i].Kind] = true
		}
	}
	if !kinds["SCOPE.Schema"] || !kinds["SCOPE.Enum"] {
		t.Fatalf("the two-record premise is gone: `Status` is minted as %v, "+
			"want both SCOPE.Schema and SCOPE.Enum", kinds)
	}
	phpFTEqual(t, phpFTTargetsOf(recs, "Order.status"), nil, "an enum-typed field")
	phpFTEqual(t, phpFTTargetsOf(recs, "Order.price"), []string{"Money"},
		"the control field in the same class")
}

// TestPhpFieldTypeRefs_EnumTargetWouldDangle proves the REASON the test above
// asserts, by hand-emitting the refused edge and driving it through the real
// resolver. Without this, "an enum target would dangle" is a claim about
// internal/resolve read from source; with it, it is an observation.
//
// The same harness binds a class target, so a failure here cannot be the harness
// being broken.
func TestPhpFieldTypeRefs_EnumTargetWouldDangle(t *testing.T) {
	src := `<?php
enum Status: string { case Open = 'open'; }
class Money {}
`
	recs := phpFTOne(t, "enums.php", src)
	for i := range recs {
		if recs[i].ID == "" {
			recs[i].ID = graph.EntityID("issue6912", recs[i].Kind, recs[i].Name, recs[i].SourceFile)
		}
	}
	known := map[string]bool{}
	for i := range recs {
		known[recs[i].ID] = true
	}
	probe := types.EntityRecord{
		Name: "Probe.f", Kind: "SCOPE.Schema", Subtype: "field",
		SourceFile: "enums.php", Language: "php",
		Relationships: []types.RelationshipRecord{
			{ToID: extractor.BuildComponentStructuralRef("php", "enums.php", "Status"),
				Kind: "REFERENCES", Properties: types.Props{{K: "target_type", V: "Status"}}},
			{ToID: extractor.BuildComponentStructuralRef("php", "enums.php", "Money"),
				Kind: "REFERENCES", Properties: types.Props{{K: "target_type", V: "Money"}}},
		},
	}
	probe.ID = graph.EntityID("issue6912", probe.Kind, probe.Name, probe.SourceFile)
	all := append(append([]types.EntityRecord{}, recs...), probe)
	idx := resolve.BuildIndex(all)
	resolve.ReferencesEmbedded(all, idx)

	for i := range all {
		if all[i].Name != "Probe.f" {
			continue
		}
		for _, r := range all[i].Relationships {
			switch r.Properties.Get("target_type") {
			case "Status":
				if known[r.ToID] {
					t.Errorf("a component-space ref to a PHP enum now BINDS (%s). "+
						"The refusal in phpTypeDeclSubtypes is a recall loss with "+
						"no remaining justification — admit enums and re-measure.",
						r.ToID)
				}
			case "Money":
				if !known[r.ToID] {
					t.Fatalf("the class control did not bind either (%s) — this "+
						"harness proves nothing about enums", r.ToID)
				}
			}
		}
	}
}

// TestPhpFieldTypeRefs_QualifiedTypeIsNotStrippedToASameFileName is the mirror of
// arm D's Go finding: reducing `Other\Order` to `Order` binds a field to a
// same-file class it does not name. Here the file declares its OWN `Order`, so a
// scanner that took the last segment would produce a WRONG binding rather than a
// dangle.
func TestPhpFieldTypeRefs_QualifiedTypeIsNotStrippedToASameFileName(t *testing.T) {
	src := `<?php
namespace App;

class Order {}
class Money {}

class Invoice {
    public Other\Order $a;
    public \Other\Order $b;
    public \Order $c;
    public Order $d;
    public Money $control;
}
`
	recs := phpFTOne(t, "qualified.php", src)
	for _, f := range []string{"Invoice.a", "Invoice.b", "Invoice.c"} {
		phpFTEqual(t, phpFTTargetsOf(recs, f), nil, f+" (a qualified name)")
	}
	// The unqualified spelling in the same class DOES bind — so the three rows
	// above are refusals of the QUALIFICATION, not of the name.
	phpFTEqual(t, phpFTTargetsOf(recs, "Invoice.d"), []string{"Order"},
		"the unqualified control")
	phpFTEqual(t, phpFTTargetsOf(recs, "Invoice.control"), []string{"Money"},
		"the second control")
}

// TestPhpFieldTypeRefs_SameNamedFunctionDoesNotSuppressTheClass is arm D's rule
// applied to PHP, and the measured reason this arm does not inherit it.
//
// PHP keeps classes and functions in SEPARATE symbol tables, so `class Money {}`
// beside `function Money() {}` in one file is legal. Arm D's count-every-kind
// rule sees SCOPE.Component + SCOPE.Operation and refuses the target; the tier
// that actually resolves this address weighs only componentKindFamily, so it
// binds without hesitation — asserted here through the real resolver rather than
// argued.
func TestPhpFieldTypeRefs_SameNamedFunctionDoesNotSuppressTheClass(t *testing.T) {
	src := `<?php
class Money {}

function Money() { return 1; }

class Order {
    public Money $price;
}
`
	recs := phpFTOne(t, "twotables.php", src)

	// Premise: both records really exist at the same (file, name).
	var comp, op bool
	for i := range recs {
		if recs[i].Name != "Money" || recs[i].SourceFile != "twotables.php" {
			continue
		}
		switch recs[i].Kind {
		case "SCOPE.Component":
			comp = true
		case "SCOPE.Operation":
			op = true
		}
	}
	if !comp || !op {
		t.Fatalf("the two-record premise is gone (component=%v operation=%v) — "+
			"arm D's counterfactual is not exercised by this fixture", comp, op)
	}
	phpFTEqual(t, phpFTTargetsOf(recs, "Order.price"), []string{"Money"},
		"a class target sharing its name with a same-file function")

	// And it binds, which is what makes keeping the edge correct rather than
	// merely more numerous.
	phpFTAssertAllBind(t, recs)
}

// TestPhpFieldTypeRefs_TypeNameMatchIsCaseInsensitive — PHP class names fold
// case, so `public customer $c` denotes `class Customer`. The assertion is on the
// BOUND ENDPOINT and not merely on the edge count, because the failure mode of a
// folded lookup is emitting the FOLDED spelling in the address, which dangles.
func TestPhpFieldTypeRefs_TypeNameMatchIsCaseInsensitive(t *testing.T) {
	src := `<?php
class Customer {}

class Order {
    public customer $c;
}
`
	recs := phpFTOne(t, "case.php", src)
	phpFTEqual(t, phpFTTargetsOf(recs, "Order.c"), []string{"Customer"},
		"target_type must carry the DECLARED spelling")
	for i := range recs {
		if recs[i].Name != "Order.c" {
			continue
		}
		for _, r := range recs[i].Relationships {
			if want := "scope:component:class:php:case.php:Customer"; r.ToID != want {
				t.Errorf("ToID = %q, want %q — the address must use the declared "+
					"spelling or it cannot bind", r.ToID, want)
			}
		}
	}
	phpFTAssertAllBind(t, recs)
}

// TestPhpFieldTypeRefs_SelfReferenceGetsNoEdge — a recursive field already has
// the owner→member CONTAINS edge; a field→owner REFERENCES asserts nothing new.
// The control in the same class means this cannot pass by the pass being off.
func TestPhpFieldTypeRefs_SelfReferenceGetsNoEdge(t *testing.T) {
	src := `<?php
class Money {}

class Node {
    public ?Node $next;
    public self $me;
    public Money $cost;
}
`
	recs := phpFTOne(t, "self.php", src)
	phpFTEqual(t, phpFTTargetsOf(recs, "Node.next"), nil, "a self-typed field")
	phpFTEqual(t, phpFTTargetsOf(recs, "Node.me"), nil, "a `self`-typed field")
	phpFTEqual(t, phpFTTargetsOf(recs, "Node.cost"), []string{"Money"}, "the control")
}

// phpFTAssertAllBind drives every field-type edge in recs through the real
// resolver and fails on any that does not land on a real entity id.
func phpFTAssertAllBind(t *testing.T, recs []types.EntityRecord) {
	t.Helper()
	cp := append([]types.EntityRecord{}, recs...)
	for i := range cp {
		if cp[i].ID == "" {
			cp[i].ID = graph.EntityID("issue6912", cp[i].Kind, cp[i].Name, cp[i].SourceFile)
		}
	}
	known := map[string]string{}
	for i := range cp {
		known[cp[i].ID] = cp[i].SourceFile + ":" + cp[i].Kind + "/" + cp[i].Subtype + ":" + cp[i].Name
	}
	n := 0
	for i := range cp {
		for _, r := range cp[i].Relationships {
			if r.Kind == "REFERENCES" && r.Properties.Get("ref_kind") == "field_target_type" {
				n++
			}
		}
	}
	if n == 0 {
		t.Fatal("no field-type edges to drive through the resolver — the " +
			"binding assertion would be vacuous")
	}
	idx := resolve.BuildIndex(cp)
	resolve.ReferencesEmbedded(cp, idx)
	for i := range cp {
		for _, r := range cp[i].Relationships {
			if r.Kind != "REFERENCES" || r.Properties.Get("ref_kind") != "field_target_type" {
				continue
			}
			if _, ok := known[r.ToID]; !ok {
				t.Errorf("%s:%s -[REFERENCES]-> %q did NOT bind (dangling stub → "+
					"bug-extractor)", cp[i].SourceFile, cp[i].Name, r.ToID)
			}
		}
	}
}

// TestPhpFieldTypeRefs_ResolvesToEntityIDs drives the fixture's edges through the
// real resolver and asserts EVERY ONE binds, NAMING the entity it binds to. This
// is what makes "a non-binding edge is worse than no edge" a graded property
// rather than an intention, and what makes the ADDRESS DIALECT graded: the rival
// file declares the same bare names, so a bare-name ToID would be ambiguous.
func TestPhpFieldTypeRefs_ResolvesToEntityIDs(t *testing.T) {
	recs := phpFTOne(t, phpFTPath, phpFTSrc)
	rival := phpFTOne(t, "other.php", `<?php
class Customer {}
class Money {}
class Shipper {}
`)
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
	if _, st := idx.LookupStatusHint("Customer", "REFERENCES"); st == 1 {
		t.Fatalf("the bare name %q binds in this fixture, so the ambiguity this "+
			"test controls for is absent and the structural address is ungraded",
			"Customer")
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
	const m = "models.php:SCOPE.Component:"
	want := []string{
		"Order2.amount => " + m + "Money",
		"Order2.both => " + m + "Customer",
		"Order2.both => " + m + "Shipper",
		"Order2.buyer => " + m + "Customer",
		"Order2.carrier => " + m + "Ship",
		"Order2.either => " + m + "Customer",
		"Order2.either => " + m + "Money",
		"Order2.folded => " + m + "Customer",
		"Order2.m1 => " + m + "Money",
		"Order2.m2 => " + m + "Money",
		"Order2.maybeBuyer => " + m + "Customer",
		"Order2.mixedUnion => " + m + "Money",
		"Order2.owner => " + m + "Customer",
		"Order2.trait => " + m + "Loggable",
	}
	phpFTEqual(t, bound, want, "resolved field-type endpoints")
}

// TestPhpFieldTypeRefs_PropertiesAgreeWithTheEndpoints — `field_name` must be the
// field entity's own leaf name so the property agrees with the entity Name and
// with the CONTAINS structural ref, rather than introducing a third spelling.
func TestPhpFieldTypeRefs_PropertiesAgreeWithTheEndpoints(t *testing.T) {
	recs := phpFTOne(t, phpFTPath, phpFTSrc)
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
			if !strings.HasPrefix(rel.ToID, "scope:component:class:php:"+phpFTPath+":") {
				t.Errorf("%s -> %q is not a same-file php component structural ref",
					r.Name, rel.ToID)
			}
		}
	}
	if n == 0 {
		t.Fatal("no field-type edges in the fixture — every assertion above is vacuous")
	}
}
