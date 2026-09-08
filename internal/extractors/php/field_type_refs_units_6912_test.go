package php

import (
	"sort"
	"testing"

	"github.com/cajasmota/grafel/internal/types"
)

// #6912 arm H — INTERNAL tests for the conjuncts Extract cannot reach.
//
// Extract is called once per file and every record it builds carries that file's
// path, so the `r.SourceFile != filePath` conjuncts never fire from outside and a
// mutant deleting one survives the whole external suite. Likewise the
// component-address-family ambiguity rule: PHP emits exactly ONE
// componentKindFamily kind (SCOPE.Component), so no PHP source can put two family
// kinds at one (file, name). "Unreachable from outside" is a reason to call the
// function directly, not a reason to leave a conjunct ungraded.

func phpFTKeys(m map[string]phpFieldTypeTarget) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// TestPhpFieldTypeRefs_Unit_TargetsAreScopedToTheRequestedFile grades BOTH
// same-file conjuncts, in OPPOSITE failure directions. Grading one is exactly
// what makes the other look covered.
//
//   - the RIVAL scan (pass 1): a cross-file record wrongly SUPPRESSING an edge.
//   - the TARGET scan (pass 2): a cross-file record wrongly BECOMING a target.
func TestPhpFieldTypeRefs_Unit_TargetsAreScopedToTheRequestedFile(t *testing.T) {
	records := []types.EntityRecord{
		{Name: "Customer", Kind: "SCOPE.Component", Subtype: "class", SourceFile: "a.php"},
		{Name: "Money", Kind: "SCOPE.Component", Subtype: "class", SourceFile: "a.php"},
		// Same shape, other file. Neither may appear in a.php's target set —
		// this is the TARGET-scan direction.
		{Name: "Shipper", Kind: "SCOPE.Component", Subtype: "interface", SourceFile: "b.php"},
		// An IN-FAMILY record in b.php sharing a name with an a.php type. This
		// row grades the RIVAL-scan direction, which the rows above cannot:
		// they share no name with a.php, so a cross-file leak there changes
		// nothing. With that conjunct removed, nameKinds["Customer"] gains
		// SCOPE.Model from b.php and the ambiguity rule wrongly suppresses
		// a.php's perfectly unambiguous Customer.
		{Name: "Customer", Kind: "SCOPE.Model", Subtype: "model", SourceFile: "b.php"},
	}

	got := phpInFileTypeTargets(records, "a.php")
	if want := []string{"customer", "money"}; len(got) != 2 ||
		got["customer"].name != "Customer" || got["money"].name != "Money" {
		t.Fatalf("a.php targets = %v, want %v", phpFTKeys(got), want)
	}
	if got["customer"].toID != "scope:component:class:php:a.php:Customer" {
		t.Errorf("toID = %q", got["customer"].toID)
	}
	if _, ok := got["shipper"]; ok {
		t.Error("Shipper is declared in b.php and must NOT be a target for " +
			"a.php — same-file targets are this arm's central promise")
	}

	// The mirror, so the assertion above is not merely "b.php was ignored":
	// asking for b.php returns b.php's own declarations.
	gotB := phpInFileTypeTargets(records, "b.php")
	if _, ok := gotB["shipper"]; !ok {
		t.Fatalf("b.php's own target set %v is missing Shipper — the assertions "+
			"above would pass even if the function always returned nothing",
			phpFTKeys(gotB))
	}

	// The THIRD site: the field loop's own SourceFile conjunct. A field in
	// b.php must not pick up a.php's targets.
	withField := append([]types.EntityRecord{}, records...)
	withField = append(withField, types.EntityRecord{
		Name: "Foreign.buyer", Kind: "SCOPE.Schema", Subtype: "field", SourceFile: "b.php",
		Properties: map[string]string{
			"field_name": "buyer", "field_type": "Money", "parent_class": "Foreign",
		},
	})
	out := attachPhpFieldTypeRefs(withField, "a.php")
	for i := range out {
		if out[i].Name == "Foreign.buyer" && len(out[i].Relationships) != 0 {
			t.Errorf("a b.php field received %d edge(s) from a.php's target set — "+
				"the field loop's SourceFile conjunct is not holding",
				len(out[i].Relationships))
		}
	}
}

// TestPhpFieldTypeRefs_Unit_InFamilyRivalSuppressesTheTarget grades rule 2 in the
// direction it exists for, and its MIRROR in the same test so that "suppressed"
// is not confused with "never worked".
//
// The rule is VACUOUS from PHP source today — SCOPE.Component is the only
// componentKindFamily kind any PHP producer emits — so it is graded here by
// direct call. It is kept because a producer that starts minting a SCOPE.Model or
// SCOPE.View for a PHP class (an Eloquent-model detector is the obvious
// candidate) would otherwise make this pass emit a stub the resolver refuses:
// lookupLocationKind weighs both kinds, finds no unique answer, and ambigLocation
// blanks the ref.
func TestPhpFieldTypeRefs_Unit_InFamilyRivalSuppressesTheTarget(t *testing.T) {
	base := []types.EntityRecord{
		{Name: "Customer", Kind: "SCOPE.Component", Subtype: "class", SourceFile: "a.php"},
		{Name: "Money", Kind: "SCOPE.Component", Subtype: "class", SourceFile: "a.php"},
	}

	// Control: with no rival, both are targets.
	if got := phpInFileTypeTargets(base, "a.php"); len(got) != 2 {
		t.Fatalf("control: targets = %v, want both", phpFTKeys(got))
	}

	// IN-family rival — a SECOND family kind at the same (file, name).
	inFamily := append(append([]types.EntityRecord{}, base...),
		types.EntityRecord{Name: "Customer", Kind: "SCOPE.Model", Subtype: "model", SourceFile: "a.php"})
	got := phpInFileTypeTargets(inFamily, "a.php")
	if _, ok := got["customer"]; ok {
		t.Error("an in-family rival at the same (file, name) must suppress the " +
			"target: the resolver's own tier cannot pick between them")
	}
	if _, ok := got["money"]; !ok {
		t.Error("the untouched name must still be a target — rule 2 must drop " +
			"one name, not the file")
	}

	// OUT-of-family rival — arm D's rule would drop this one; PHP's tier does
	// not weigh it, so it must survive. This is the counterfactual that stops
	// arm D's rule being inherited.
	outFamily := append(append([]types.EntityRecord{}, base...),
		types.EntityRecord{Name: "Customer", Kind: "SCOPE.Operation", Subtype: "function", SourceFile: "a.php"},
		types.EntityRecord{Name: "Money", Kind: "SCOPE.Enum", Subtype: "enum", SourceFile: "a.php"})
	got = phpInFileTypeTargets(outFamily, "a.php")
	if _, ok := got["customer"]; !ok {
		t.Error("a same-file SCOPE.Operation is NOT weighed by the tier that " +
			"resolves a component-space ref, so it must not suppress the class")
	}
	if _, ok := got["money"]; !ok {
		t.Error("a same-file SCOPE.Enum is likewise out of family and must not " +
			"suppress the class")
	}
}

// TestPhpFieldTypeRefs_Unit_OnlyComponentsAreTargets grades pass 2's
// `r.Kind != "SCOPE.Component"` conjunct, which an independent review found
// ALIVE under two mutants (delete it; widen it to admit SCOPE.Schema).
//
// It is MUTUALLY MASKED by the subtype allow-list: no core PHP producer mints a
// record with Subtype ∈ {class, interface, trait} under any Kind but
// SCOPE.Component, so through Extract the two guards only ever fire together and
// the allow-list alone carries every refusal — including the enum one, which
// TestPhpFieldTypeRefs_EnumIsNotATarget therefore grades through the allow-list
// rather than through this conjunct. Two guards that only fire together grade
// neither, so they are scored part by part here.
//
// The distinguishing input is `{Kind: "SCOPE.Model", Subtype: "class"}` — which
// is EXACTLY the future producer this arm cites to justify keeping rule 2 (an
// Eloquent-model detector re-kinding a PHP class). Rule 2 is graded against that
// producer by direct call; leaving the conjunct the same producer would exercise
// ungraded was an inconsistency, not a considered scope.
//
// The kinds are ENUMERATED rather than hand-picked: every Kind this package or
// its custom lane can plausibly grow, both in and out of the address family,
// each paired with an ADMITTED subtype so the allow-list cannot be what refuses
// it. A positive control in the same input keeps the assertions from passing
// because the function returned nothing.
func TestPhpFieldTypeRefs_Unit_OnlyComponentsAreTargets(t *testing.T) {
	// Each row: a record with an ADMITTED subtype under a NON-Component Kind.
	// None may become a target.
	kinds := []string{
		"SCOPE.Model",   // in-family — the Eloquent-detector shape
		"SCOPE.View",    // in-family
		"SCOPE.Class",   // in-family via the index's trim alias
		"SCOPE.Schema",  // out-of-family — the reviewer's N8 widening
		"SCOPE.Enum",    // out-of-family — the parallel value-set population
		"SCOPE.Pattern", // out-of-family — emitted by internal/custom/php
		"SCOPE.Service", // out-of-family — emitted by internal/custom/php
	}
	for _, kind := range kinds {
		records := []types.EntityRecord{
			// The positive control: a real Component target, distinct name.
			{Name: "Money", Kind: "SCOPE.Component", Subtype: "class", SourceFile: "a.php"},
			// The row under test. A DISTINCT name, so rule 2 cannot be what
			// refuses it — this test grades the Kind conjunct alone.
			{Name: "Ghost", Kind: kind, Subtype: "class", SourceFile: "a.php"},
		}
		got := phpInFileTypeTargets(records, "a.php")
		if _, ok := got["money"]; !ok {
			t.Fatalf("kind %q: the control target is missing (%v) — every "+
				"assertion in this row would be vacuous", kind, phpFTKeys(got))
		}
		if _, ok := got["ghost"]; ok {
			t.Errorf("a record kinded %q with Subtype \"class\" became a target. "+
				"Only SCOPE.Component records are type declarations this package "+
				"emits; the allow-list is a subtype filter and cannot refuse this "+
				"on its own.", kind)
		}
	}

	// The mirror, so "refused" is not "the subtype was wrong": the SAME
	// non-Component kinds with a REFUSED subtype are also absent — i.e. the two
	// guards are independent rather than one standing in for the other.
	for _, kind := range kinds {
		records := []types.EntityRecord{
			{Name: "Money", Kind: "SCOPE.Component", Subtype: "class", SourceFile: "a.php"},
			{Name: "Ghost", Kind: kind, Subtype: "", SourceFile: "a.php"},
		}
		if got := phpInFileTypeTargets(records, "a.php"); len(got) != 1 {
			t.Errorf("kind %q with an empty subtype: targets = %v, want only the "+
				"control", kind, phpFTKeys(got))
		}
	}
}

// TestPhpFieldTypeRefs_Unit_ScopeClassParticipatesViaTheTrimAlias grades the one
// entry of phpComponentAddressFamily that is NOT a literal member of
// componentKindFamily.
//
// BuildIndex writes every entity under its raw Kind AND its SCOPE-trimmed alias,
// so a `SCOPE.Class` entity is keyed under "Class", which IS in the family. Drop
// that entry and the pass emits a stub the resolver blanks. Arm F shipped this
// hole in a first revision with a comment claiming otherwise.
func TestPhpFieldTypeRefs_Unit_ScopeClassParticipatesViaTheTrimAlias(t *testing.T) {
	records := []types.EntityRecord{
		{Name: "Customer", Kind: "SCOPE.Component", Subtype: "class", SourceFile: "a.php"},
		{Name: "Customer", Kind: "SCOPE.Class", Subtype: "class", SourceFile: "a.php"},
	}
	if got := phpInFileTypeTargets(records, "a.php"); len(got) != 0 {
		t.Errorf("targets = %v; a SCOPE.Class rival is visible to "+
			"lookupLocationKind through the index's trim alias and must "+
			"suppress the target", phpFTKeys(got))
	}
	// Every member of the table must be able to suppress, or it is decoration.
	for kind := range phpComponentAddressFamily {
		if kind == "SCOPE.Component" {
			continue
		}
		recs := []types.EntityRecord{
			{Name: "Customer", Kind: "SCOPE.Component", Subtype: "class", SourceFile: "a.php"},
			{Name: "Customer", Kind: kind, Subtype: "x", SourceFile: "a.php"},
		}
		if got := phpInFileTypeTargets(recs, "a.php"); len(got) != 0 {
			t.Errorf("a rival kinded %q did not suppress the target — the table "+
				"entry is inert", kind)
		}
	}
}

// TestPhpFieldTypeRefs_Unit_OnlySchemaFieldsAreAnchors grades the field loop's
// Kind and Subtype conjuncts, which fire together for anything Extract can
// produce (emitPhpFieldMembers is the only site minting Subtype "field" and it
// always sets Kind SCOPE.Schema) and so are ungradeable from outside.
func TestPhpFieldTypeRefs_Unit_OnlySchemaFieldsAreAnchors(t *testing.T) {
	props := map[string]string{
		"field_name": "buyer", "field_type": "Customer", "parent_class": "Order",
	}
	records := []types.EntityRecord{
		{Name: "Customer", Kind: "SCOPE.Component", Subtype: "class", SourceFile: "a.php"},
		// Right subtype, WRONG kind.
		{Name: "Order.a", Kind: "SCOPE.Component", Subtype: "field", SourceFile: "a.php", Properties: props},
		// Right kind, WRONG subtype.
		{Name: "Order.b", Kind: "SCOPE.Schema", Subtype: "enum", SourceFile: "a.php", Properties: props},
		// Both right — the positive control, so a broken pass cannot pass this.
		{Name: "Order.c", Kind: "SCOPE.Schema", Subtype: "field", SourceFile: "a.php", Properties: props},
	}
	out := attachPhpFieldTypeRefs(records, "a.php")
	for i := range out {
		switch out[i].Name {
		case "Order.a", "Order.b":
			if len(out[i].Relationships) != 0 {
				t.Errorf("%s (%s/%s) is not a field entity and must not anchor a "+
					"field-type edge", out[i].Name, out[i].Kind, out[i].Subtype)
			}
		case "Order.c":
			if len(out[i].Relationships) != 1 {
				t.Fatalf("the control got %d edges, want 1 — the two assertions "+
					"above are vacuous", len(out[i].Relationships))
			}
		}
	}
}

// TestPhpFieldTypeRefs_Unit_FoldedSpellingCollisionRefusesBoth grades the
// case-folding collision branch. `class Money` beside `interface MONEY` is a
// fatal redeclaration in PHP, so it is unreachable from a file PHP will load —
// but this pass is handed RECORDS, not a running interpreter, and picking one of
// the two arbitrarily would make the emitted address depend on record order.
func TestPhpFieldTypeRefs_Unit_FoldedSpellingCollisionRefusesBoth(t *testing.T) {
	records := []types.EntityRecord{
		{Name: "Money", Kind: "SCOPE.Component", Subtype: "class", SourceFile: "a.php"},
		{Name: "MONEY", Kind: "SCOPE.Component", Subtype: "interface", SourceFile: "a.php"},
		{Name: "Customer", Kind: "SCOPE.Component", Subtype: "class", SourceFile: "a.php"},
	}
	got := phpInFileTypeTargets(records, "a.php")
	if _, ok := got["money"]; ok {
		t.Errorf("two spellings folding to the same key must refuse both; got %v",
			phpFTKeys(got))
	}
	if _, ok := got["customer"]; !ok {
		t.Error("the untouched name must survive — the collision drops one key, " +
			"not the file")
	}
	// Order-independent: the same input reversed must give the same answer.
	rev := []types.EntityRecord{records[2], records[1], records[0]}
	if got2 := phpInFileTypeTargets(rev, "a.php"); len(got2) != len(got) {
		t.Errorf("the answer depends on record order: %v vs %v",
			phpFTKeys(got), phpFTKeys(got2))
	}
}

// TestPhpFieldTypeRefs_Unit_OneEdgePerTargetNotPerCandidate grades the `emitted`
// dedup, which no external fixture can reach.
//
// Two candidates fold to ONE target only when the type expression names the same
// class twice in different case — `Money|money` — and PHP rejects that source as
// a duplicate union member, so no .php file can produce it. The guard is
// therefore defensive rather than reachable, and it is graded HERE, by handing
// the function the record such a file would produce, rather than left alive on
// the strength of "PHP would not compile it": this function takes RECORDS, and
// the property that matters — one edge per target, never per candidate — should
// not depend on an upstream parser's error handling.
func TestPhpFieldTypeRefs_Unit_OneEdgePerTargetNotPerCandidate(t *testing.T) {
	records := []types.EntityRecord{
		{Name: "Money", Kind: "SCOPE.Component", Subtype: "class", SourceFile: "a.php"},
		{Name: "Order.price", Kind: "SCOPE.Schema", Subtype: "field", SourceFile: "a.php",
			Properties: map[string]string{
				"field_name": "price", "field_type": "Money|money", "parent_class": "Order",
			}},
	}
	out := attachPhpFieldTypeRefs(records, "a.php")
	for i := range out {
		if out[i].Name != "Order.price" {
			continue
		}
		if n := len(out[i].Relationships); n != 1 {
			t.Errorf("got %d edges for a type naming one target twice, want 1 — "+
				"the edge count must follow the TARGET set, not the candidate list", n)
		}
	}
}

// TestPhpFieldTypeRefs_Unit_CandidateScannerEnumeratesTheShapeSpace grades the
// scanner as a PROPERTY over PHP's type syntax rather than as a handful of lucky
// fixtures. Every row is a legal PHP type expression.
func TestPhpFieldTypeRefs_Unit_CandidateScannerEnumeratesTheShapeSpace(t *testing.T) {
	cases := []struct {
		typ  string
		want []string
	}{
		{"Order", []string{"Order"}},
		{"?Order", []string{"Order"}},
		{"Order|Money", []string{"Order", "Money"}},
		{"Order|null", []string{"Order", "null"}},
		{"null|Order", []string{"null", "Order"}},
		{"Order&Shipper", []string{"Order", "Shipper"}},
		{"Order & Shipper", []string{"Order", "Shipper"}},
		{"(Order&Shipper)|null", []string{"Order", "Shipper", "null"}},
		{"int", []string{"int"}},
		{"string|int", []string{"string", "int"}},
		{"array", []string{"array"}},
		{"self", []string{"self"}},
		{"static", []string{"static"}},
		{"parent", []string{"parent"}},
		{"iterable", []string{"iterable"}},
		{"never", []string{"never"}},
		{"false", []string{"false"}},
		{"Order_2", []string{"Order_2"}},
		{"_Order", []string{"_Order"}},
		{"Ünité", []string{"Ünité"}},
		// Qualified in every spelling — each must vanish WHOLE, leaving no
		// segment behind. These are the rows a "skip the separator" scanner
		// fails: it would yield [Ns Order].
		{"Ns\\Order", nil},
		{"\\Ns\\Order", nil},
		{"\\Order", nil},
		{"A\\B\\C\\Order", nil},
		{"?\\Ns\\Order", nil},
		{"\\Ns\\Order|Money", []string{"Money"}},
		{"Money|\\Ns\\Order", []string{"Money"}},
		{"\\A\\B&\\C\\D", nil},
		{"", nil},
		{"?", nil},
	}
	for _, c := range cases {
		got := phpFieldTypeCandidates(c.typ)
		if len(got) != len(c.want) {
			t.Errorf("candidates(%q) = %v, want %v", c.typ, got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("candidates(%q) = %v, want %v", c.typ, got, c.want)
				break
			}
		}
	}
}
