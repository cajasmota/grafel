package entkinds_test

// scan_go_references_6818_test.go — unit tests for ScanGoReferences, the
// reference scan #6818's enum-corroboration guard is built on.
//
// The behaviour under test is the one the guard depends on: a MENTION counts,
// wherever it sits, and it does not have to be a producer. The tests below are
// therefore mostly about the SHAPES that must count — literal, bare identifier,
// qualified selector, conversion — and the two that must NOT, both of which are
// ScanGo's fabrication bugs from #6776 and are re-observed here because
// ScanGoReferences reaches the same resolver by a different path.

import (
	"sort"
	"testing"

	"github.com/cajasmota/grafel/internal/entkinds"
)

// refKindsAt returns "file:line" for every site of kind, sorted.
func refKindsAt(res entkinds.Result, kind string) []string {
	var out []string
	for _, s := range res.SitesFor(kind) {
		out = append(out, s.File)
	}
	sort.Strings(out)
	return out
}

// TestScanGoReferences6818_CountsMentionsScanGoCannotSee is the whole reason
// this function exists. Each producer shape below writes SCOPE.Widget in a way
// ScanGo's composite-literal reader is blind to, plus one pure consumer that no
// producer scan could ever see. The test observes BOTH scans on one tree, so
// the difference is a measurement rather than a claim in a comment.
func TestScanGoReferences6818_CountsMentionsScanGoCannotSee(t *testing.T) {
	root := t.TempDir()
	write(t, root, "prod/assign.go", "package prod\n\n"+
		"type rec struct{ Kind string }\n\n"+
		"func f(e *rec) { e.Kind = \"SCOPE.Widget\" }\n")
	write(t, root, "prod/callsite.go", "package prod\n\n"+
		"const widgetKind = \"SCOPE.Widget\"\n\n"+
		"func g() { emit(\"id\", widgetKind) }\n\n"+
		"func emit(string, string) {}\n")
	write(t, root, "consume/read.go", "package consume\n\n"+
		"func h(k string) bool { return k == \"SCOPE.Widget\" }\n")

	refs, err := entkinds.ScanGoReferences(root, []string{"SCOPE.Widget"})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"consume/read.go", "prod/assign.go", "prod/callsite.go", "prod/callsite.go"}
	if got := refKindsAt(refs, "SCOPE.Widget"); !equalStrings(got, want) {
		t.Errorf("reference files = %v, want %v (callsite.go twice: the const declaration and "+
			"the identifier passed to emit)", got, want)
	}

	produced, err := entkinds.ScanGo(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(produced.Sites) != 0 {
		t.Errorf("ScanGo found %+v on this tree; the test's premise is that every shape here is "+
			"invisible to it, so either the scanner widened (good — say so and re-cut this "+
			"fixture) or the fixture stopped exercising the blind spot", produced.Sites)
	}
}

// TestScanGoReferences6818_WalksIntoASelectorsExpressionReceiver is the
// regression pin for the pruning defect found in review: the selector case
// returned early after failing to read its qualifier as a bare identifier, and
// that early return dropped the WHOLE receiver subtree.
//
// `graph.Entity{Kind: "SCOPE.Widget"}.WithProperties(p)` is a selector whose X
// is a composite literal, not a package qualifier — 40 such sites sit in 22
// non-test files of this repo, and three (file, kind) pairs ScanGo resolves
// were invisible to this scan because of it. One of the three was a PLAIN
// STRING LITERAL, so this is not a resolver limit: the node was never reached.
//
// The Sel half must still not be walked, which the second case below observes:
// a method named after a constant is not a reference to it.
func TestScanGoReferences6818_WalksIntoASelectorsExpressionReceiver(t *testing.T) {
	root := t.TempDir()
	write(t, root, "prod/chain.go", "package prod\n\n"+
		"type E struct{ Kind string }\n\n"+
		"func (e E) With() E { return e }\n\n"+
		"var a = E{Kind: \"SCOPE.Widget\"}.With()\n")
	// The receiver is an arbitrary expression, not just a literal or a call.
	// Each line below is a DIFFERENT receiver shape, so restricting the descent
	// to any one node type leaves the others pruned.
	write(t, root, "prod/deep.go", "package prod\n\n"+
		"func sink(...string) []string { return nil }\n\n"+
		"var b = sink(\"SCOPE.Widget\")[0:1]\n"+ // slice of a call
		"var c = E{Kind: \"SCOPE.Widget\"}.With().With()\n"+ // call receiver
		"var f = []E{{Kind: \"SCOPE.Widget\"}}[0].With()\n"+ // index receiver
		"var g = (E{Kind: \"SCOPE.Widget\"}).With()\n"+ // parenthesised receiver
		"var h = (&E{Kind: \"SCOPE.Widget\"}).With()\n") // pointer-of receiver

	refs, err := entkinds.ScanGoReferences(root, []string{"SCOPE.Widget"})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"prod/chain.go",
		"prod/deep.go", "prod/deep.go", "prod/deep.go", "prod/deep.go", "prod/deep.go",
	}
	if got := refKindsAt(refs, "SCOPE.Widget"); !equalStrings(got, want) {
		t.Errorf("reference files = %v, want %v — a selector whose receiver is an "+
			"expression must be descended into, not pruned", got, want)
	}
}

// TestScanGoReferences6818_DoesNotReadNamesInDeclaringPosition is the other
// half of the fix above, and the fabrication direction generally: descending
// into a selector's receiver — or into anything else — must never turn a name
// being DECLARED into a reference. Every declaration below is spelled exactly
// like the package's `Widget` constant and none of them mentions its value.
func TestScanGoReferences6818_DoesNotReadNamesInDeclaringPosition(t *testing.T) {
	root := t.TempDir()
	write(t, root, "prod/consts.go", "package prod\n\n"+
		"const Widget = \"SCOPE.Widget\"\n")
	write(t, root, "prod/call.go", "package prod\n\n"+
		"type box struct{ Widget string }\n\n"+ // struct FIELD name
		"type iface interface{ Widget() string }\n\n"+ // interface METHOD name
		"func (b box) Widget(Widget string) string {\n"+ // METHOD name and PARAM name
		"\ttype Widget struct{}\n"+ // function-local TYPE name
		"\t_ = Widget{}\n"+ // ...and a USE of it, in type position
		"\t_ = make([]Widget, 0)\n"+ // type position inside make
		"\t_, _ = interface{}(nil).(Widget)\n"+ // type position in an assertion
		"\treturn b.Widget\n"+
		"}\n\n"+
		"var d = box{}.Widget(\"\")\n")
	// A composite-literal KEY spelled like the constant is a FIELD NAME, and
	// reading a field name back through the same-package constant table is a
	// fabricated reference. `box{Widget: 2}.Widget` is the shape the round-2
	// receiver widening newly reaches, so it is pinned here rather than left to
	// the widening's blast radius.
	write(t, root, "prod/key.go", "package prod\n\n"+
		"var k1 = box{Widget: \"\"}\n"+
		"var k2 = box{Widget: \"\"}.Widget\n")
	// Type PARAMETERS are declared names whose USES sit in type position; both
	// halves must stay silent.
	write(t, root, "prod/generic.go", "package prod\n\n"+
		"type G[Widget any] struct{ v Widget }\n\n"+
		"func H[Widget any](x Widget) Widget { return x }\n")

	refs, err := entkinds.ScanGoReferences(root, []string{"SCOPE.Widget"})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"prod/consts.go"} // the declaration's literal, nothing else
	if got := refKindsAt(refs, "SCOPE.Widget"); !equalStrings(got, want) {
		t.Errorf("reference files = %v, want %v — a name in DECLARING position was resolved as "+
			"a reference to a same-package constant that merely shares its spelling", got, want)
	}
}

// TestScanGoReferences6818_WhatTheKeyAndTypeNarrowingGivesUp states the price
// of the two narrowings above, by observing it.
//
// Refusing to read a bare identifier in composite-literal KEY position cannot
// be done selectively: without a type checker a struct field name and a map key
// are the same node. So a map keyed by a kind CONSTANT becomes a miss. A map
// keyed by a kind LITERAL — internal/extractors/migration_prune.go:70 and the
// shape this repo actually uses — is untouched, and that is the half worth
// keeping.
//
// The struct TAG is walked, deliberately: a tag is a string literal of the
// source. The match is exact, so the only tag this can ever report is one whose
// whole value is the kind, which is why the choice is stated here rather than
// claimed to matter.
func TestScanGoReferences6818_WhatTheKeyAndTypeNarrowingGivesUp(t *testing.T) {
	root := t.TempDir()
	write(t, root, "prod/maps.go", "package prod\n\n"+
		"const widgetKind = \"SCOPE.Widget\"\n\n"+
		"var byLiteral = map[string]bool{\"SCOPE.Widget\": true}\n"+
		"var byConst = map[string]bool{widgetKind: true}\n\n"+
		"type tagged struct {\n"+
		"\tA string `SCOPE.Widget`\n"+
		"}\n")

	refs, err := entkinds.ScanGoReferences(root, []string{"SCOPE.Widget"})
	if err != nil {
		t.Fatal(err)
	}
	var details []string
	for _, site := range refs.SitesFor("SCOPE.Widget") {
		details = append(details, site.Detail)
	}
	sort.Strings(details)
	// Three literals: the constant's own declaration, the map key, the tag.
	// NOT a fourth for `byConst`'s identifier key.
	want := []string{"string literal", "string literal", "string literal"}
	if !equalStrings(details, want) {
		t.Errorf("sites = %v, want %v — a map key written as an IDENTIFIER is a documented "+
			"miss of the composite-literal-key narrowing; a LITERAL key and a struct tag are "+
			"not", details, want)
	}
	// There is deliberately no second assertion naming the identifier key's
	// line. An identifier site would carry Detail "identifier widgetKind", so
	// the comparison above already rejects it; a line check could never be the
	// only thing firing, and a guard graded by nothing is worse than no guard.
}

// TestScanGoReferences6818_ResolvesTheSameConstantShapesScanGoDoes pins the
// resolver capabilities the guard relies on to see a kind referenced as
// `types.EntityKindX` rather than as a bare string — which is how most of
// internal/ mentions kinds.
func TestScanGoReferences6818_ResolvesTheSameConstantShapesScanGoDoes(t *testing.T) {
	root := t.TempDir()
	write(t, root, "types/kinds.go", "package types\n\n"+
		"const EntityKindWidget = \"SCOPE.Widget\"\n")
	write(t, root, "user/use.go", "package user\n\n"+
		"import \"x/types\"\n\n"+
		"var a = types.EntityKindWidget\n"+
		"var b = string(types.EntityKindWidget)\n")
	write(t, root, "user/local.go", "package user\n\n"+
		"const localWidget = \"SCOPE.Widget\"\n\n"+
		"var c = localWidget\n")

	refs, err := entkinds.ScanGoReferences(root, []string{"SCOPE.Widget"})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"types/kinds.go", // the declaration's own literal
		"user/local.go",  // the local const literal
		"user/local.go",  // the bare identifier reading it
		"user/use.go",    // selector
		"user/use.go",    // conversion around the selector
	}
	if got := refKindsAt(refs, "SCOPE.Widget"); !equalStrings(got, want) {
		t.Errorf("reference files = %v, want %v", got, want)
	}
}

// TestScanGoReferences6818_DoesNotFabricateFromFieldReadsOrForeignLocals
// re-observes #6776's two fabrication bugs through this entry point. Both
// shapes below LOOK like a reference to SCOPE.Widget and are not one, and a
// resolver that answered either would let a fabricated enum member be
// corroborated by an unrelated struct field or an unrelated package's constant.
func TestScanGoReferences6818_DoesNotFabricateFromFieldReadsOrForeignLocals(t *testing.T) {
	root := t.TempDir()
	// A package-level constant named Kind, and a DIFFERENT package reading a
	// struct field spelled the same way. `e.Kind` is not a package selector.
	write(t, root, "other/consts.go", "package other\n\n"+
		"const Kind = \"SCOPE.Widget\"\n")
	write(t, root, "reader/read.go", "package reader\n\n"+
		"type rec struct{ Kind string }\n\n"+
		"func f(e rec) string { return e.Kind }\n")
	// A function-local constant in one package must not resolve against another
	// package's package-level constant of the same name.
	write(t, root, "elsewhere/decl.go", "package elsewhere\n\n"+
		"const localName = \"SCOPE.Widget\"\n")
	write(t, root, "unrelated/use.go", "package unrelated\n\n"+
		"func g() string {\n\tconst localName = \"SCOPE.Other\"\n\treturn localName\n}\n")

	refs, err := entkinds.ScanGoReferences(root, []string{"SCOPE.Widget"})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"elsewhere/decl.go", "other/consts.go"} // the two declarations, nothing else
	if got := refKindsAt(refs, "SCOPE.Widget"); !equalStrings(got, want) {
		t.Errorf("reference files = %v, want %v — a field read or a foreign function-local "+
			"constant was resolved as a reference", got, want)
	}
}

// TestScanGoReferences6818_ReportsOnlyWantedKindsAndSkipsTests observes the two
// scoping decisions the guard depends on: the scan answers a membership
// question about the caller's set (so an unrelated string is not a site), and
// _test.go files do not corroborate anything — a fabricated kind pinned by a
// test roster must still be reported, which is exactly #6818's motivating case.
func TestScanGoReferences6818_ReportsOnlyWantedKindsAndSkipsTests(t *testing.T) {
	root := t.TempDir()
	write(t, root, "pkg/prod.go", "package pkg\n\nvar a = \"SCOPE.Widget\"\nvar b = \"SCOPE.Other\"\n")
	write(t, root, "pkg/roster_test.go", "package pkg\n\nvar pinned = []string{\"SCOPE.Bogus\"}\n")

	refs, err := entkinds.ScanGoReferences(root, []string{"SCOPE.Widget", "SCOPE.Bogus"})
	if err != nil {
		t.Fatal(err)
	}
	if got := refKindsAt(refs, "SCOPE.Widget"); !equalStrings(got, []string{"pkg/prod.go"}) {
		t.Errorf("SCOPE.Widget sites = %v, want [pkg/prod.go]", got)
	}
	if got := refKindsAt(refs, "SCOPE.Other"); len(got) != 0 {
		t.Errorf("SCOPE.Other was not in `wanted` but was reported at %v", got)
	}
	if got := refKindsAt(refs, "SCOPE.Bogus"); len(got) != 0 {
		t.Errorf("SCOPE.Bogus is mentioned only by a _test.go roster but was reported at %v; a "+
			"test pin must not corroborate a kind", got)
	}
	if refs.GoFilesParsed != 1 {
		t.Errorf("GoFilesParsed = %d, want 1 (the _test.go file is not parsed)", refs.GoFilesParsed)
	}
	if refs.Unresolved() != 0 {
		t.Errorf("UnresolvedSites = %+v; a reference scan has no unresolved sites by "+
			"construction — see ScanGoReferences' doc", refs.UnresolvedSites)
	}
}

func equalStrings(a, b []string) bool {
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
