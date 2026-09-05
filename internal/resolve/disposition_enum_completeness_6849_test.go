package resolve

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"sort"
	"testing"
)

// dispositionSourceFile is the single file that declares BOTH the Disposition
// const block and the AllDispositions slice. The guard below reads the const
// block from source rather than from a hand-written roster of names — a
// hand-maintained roster would be the very defect it is guarding against, one
// level up (#6849).
const dispositionSourceFile = "refs.go"

// dispositionConst is one constant of the `Disposition = iota` const block as
// it is actually written in refs.go. Value is the constant's iota value, which
// is the index of its ValueSpec within the const block — `_` placeholders and
// specs of other types still consume an iota slot, so the index is taken over
// every spec in the block, not only the ones this walk keeps.
type dispositionConst struct {
	Name  string
	Value int
}

// readDispositionSource returns the FULL contents of refs.go, having verified
// against os.Stat that nothing truncated the read. #6834 layer 3: a source
// scanner handed a partial file reports a clean tree for the part it never saw,
// and a token check ("does the text contain DispositionExternalSQL?") cannot
// tell a truncated file from a complete one.
func readDispositionSource(t *testing.T) []byte {
	t.Helper()

	info, err := os.Stat(dispositionSourceFile)
	if err != nil {
		t.Fatalf("stat %s: %v", dispositionSourceFile, err)
	}
	src, err := os.ReadFile(dispositionSourceFile)
	if err != nil {
		t.Fatalf("read %s: %v", dispositionSourceFile, err)
	}
	if int64(len(src)) != info.Size() {
		t.Fatalf("read %d bytes of %s but os.Stat reports %d; the scan would silently "+
			"skip the tail of the file", len(src), dispositionSourceFile, info.Size())
	}
	if info.Size() == 0 {
		t.Fatalf("%s is empty; the scan has nothing to read", dispositionSourceFile)
	}
	return src
}

// declaredDispositionsIn parses the given Go source and returns every constant
// declared with the type Disposition, in source order.
//
// Const-block type elision is handled: inside a `const (...)` group a spec with
// neither a type nor values repeats the previous spec's type and expression, so
// the last explicit type is carried forward for exactly that case. That is how
// this particular block is written — only DispositionResolved carries the
// `Disposition = iota` annotation.
func declaredDispositionsIn(t *testing.T, filename string, src []byte) []dispositionConst {
	t.Helper()

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, filename, src, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", filename, err)
	}

	var out []dispositionConst
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.CONST {
			continue
		}
		var carriedType ast.Expr
		// iota counts every spec in the block, including ones this walk
		// discards, so the index comes from the block-wide loop.
		for i, spec := range gen.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			typ := vs.Type
			if typ == nil && len(vs.Values) == 0 {
				typ = carriedType // elided spec: repeats the previous type
			} else {
				carriedType = typ
			}
			ident, ok := typ.(*ast.Ident)
			if !ok || ident.Name != "Disposition" {
				continue
			}
			for _, name := range vs.Names {
				if name.Name == "_" {
					continue
				}
				out = append(out, dispositionConst{Name: name.Name, Value: i})
			}
		}
	}
	return out
}

// declaredDispositionsFromSource is the real-tree entry point: the full,
// stat-verified contents of refs.go walked for Disposition constants.
func declaredDispositionsFromSource(t *testing.T) []dispositionConst {
	t.Helper()
	return declaredDispositionsIn(t, dispositionSourceFile, readDispositionSource(t))
}

// dispositionCoverageProblems is the whole decision. It reports every way the
// slice can fail to be an exact, duplicate-free enumeration of the declared
// constants: a declared constant absent from the slice, a slice entry that no
// constant declares, and a constant listed more than once.
//
// Order is deliberately NOT graded. AllDispositions documents itself as being
// in "canonical order" for stable log output, but reordering it cannot make any
// report wrong — every consumer sums or maps over it — so a guard that failed
// on a reorder would be over-tight and would be deleted the first time someone
// legitimately reordered the block.
//
// Both this test file's real-tree guard and its detector test call THIS
// function, so the detector cannot certify a decision the guard does not make.
func dispositionCoverageProblems(declared []dispositionConst, all []Disposition) []string {
	counts := map[Disposition]int{}
	for _, d := range all {
		counts[d]++
	}
	declaredByValue := map[Disposition]string{}
	for _, c := range declared {
		declaredByValue[Disposition(c.Value)] = c.Name
	}

	var problems []string
	for _, c := range declared {
		switch n := counts[Disposition(c.Value)]; {
		case n == 0:
			problems = append(problems, fmt.Sprintf(
				"missing: %s (value %d) is declared but absent from AllDispositions", c.Name, c.Value))
		case n > 1:
			problems = append(problems, fmt.Sprintf(
				"duplicated: %s (value %d) appears %d times in AllDispositions", c.Name, c.Value, n))
		}
	}
	for d, n := range counts {
		if _, ok := declaredByValue[d]; !ok {
			problems = append(problems, fmt.Sprintf(
				"extra: Disposition(%d) appears %d time(s) in AllDispositions but no constant declares it", int(d), n))
		}
	}
	sort.Strings(problems)
	return problems
}

// TestDeclaredDispositionsExtraction_NonVacuous is the floor under the
// completeness guard below. If the extraction ever breaks — a renamed file, a
// reworked const layout, a botched AST walk — it would find zero constants and
// the completeness guard would pass by examining nothing (#6834 layers 1-3).
//
// The floor is deliberately NOT stated as a count against len(AllDispositions).
// That comparand would make this test co-fire with the completeness guard on an
// over-long slice, and a check that only ever fails alongside another one is
// ungraded. Instead the floor is "found nothing at all" plus sentinels that pin
// four long-lived constants by NAME and by iota VALUE — a walk that narrows to a
// subset of the block, reads a truncated file, or is pointed at the wrong file
// dies on the sentinels, independently of what the slice happens to contain.
func TestDeclaredDispositionsExtraction_NonVacuous(t *testing.T) {
	declared := declaredDispositionsFromSource(t)

	if len(declared) == 0 {
		t.Fatalf("extracted no Disposition constants from %s; the extraction is not reading the "+
			"declarations and the completeness guard would be vacuous", dispositionSourceFile)
	}

	// Sentinels pin that the walk reads the real names AND their real iota
	// values — a walk that returned the right number of wrongly-valued
	// constants would satisfy a bare population floor.
	byName := map[string]int{}
	for _, d := range declared {
		byName[d.Name] = d.Value
	}
	sentinels := map[string]Disposition{
		"DispositionResolved":     DispositionResolved,
		"DispositionExternalSQL":  DispositionExternalSQL,
		"DispositionBugResolver":  DispositionBugResolver,
		"DispositionUnclassified": DispositionUnclassified,
	}
	for name, want := range sentinels {
		got, ok := byName[name]
		if !ok {
			t.Errorf("extraction did not find %s in %s; the AST walk is not reading the declarations",
				name, dispositionSourceFile)
			continue
		}
		if Disposition(got) != want {
			t.Errorf("extraction read %s = %d, want %d", name, got, int(want))
		}
	}
}

// TestDispositionCoverageProblems_DetectsEachFailure is #6834 layer 4: proof
// that the matcher can actually detect, exercised on fabricated inputs so the
// real tree being healthy does not certify a matcher that reports nothing.
//
// PERMISSIVE direction first: a real-shaped healthy input must produce NO
// problems, otherwise the guard fires on everything and grades nothing.
func TestDispositionCoverageProblems_DetectsEachFailure(t *testing.T) {
	declared := []dispositionConst{
		{Name: "A", Value: 0},
		{Name: "B", Value: 1},
		{Name: "C", Value: 2},
	}
	complete := []Disposition{0, 1, 2}

	if got := dispositionCoverageProblems(declared, complete); len(got) != 0 {
		t.Fatalf("a complete slice reported problems %v; the guard over-fires", got)
	}
	// Reordering is explicitly harmless.
	if got := dispositionCoverageProblems(declared, []Disposition{2, 0, 1}); len(got) != 0 {
		t.Errorf("a reordered slice reported problems %v; the guard is over-tight", got)
	}

	cases := []struct {
		name string
		all  []Disposition
		want string
	}{
		{"missing", []Disposition{0, 2}, "missing: B (value 1) is declared but absent from AllDispositions"},
		{"extra", []Disposition{0, 1, 2, 9}, "extra: Disposition(9) appears 1 time(s) in AllDispositions but no constant declares it"},
		{"duplicate", []Disposition{0, 1, 1, 2}, "duplicated: B (value 1) appears 2 times in AllDispositions"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := dispositionCoverageProblems(declared, tc.all)
			if len(got) != 1 || got[0] != tc.want {
				t.Fatalf("problems = %v, want exactly [%q]", got, tc.want)
			}
		})
	}
}

// TestDeclaredDispositionsIn_ReadsAnArbitraryConstBlock is #6834 layer 4 for
// the AST walk itself: an in-memory source whose answer is known by
// construction, including the two shapes that break naive walks — a `_`
// placeholder that still consumes an iota slot, and a const block of a
// different type that must be ignored entirely.
func TestDeclaredDispositionsIn_ReadsAnArbitraryConstBlock(t *testing.T) {
	src := []byte(`package p

type Disposition int
type Other int

const (
	AlphaD Disposition = iota
	BetaD
	_
	DeltaD
)

const (
	IgnoredO Other = iota
	AlsoIgnoredO
)
`)
	got := declaredDispositionsIn(t, "synthetic.go", src)
	want := []dispositionConst{
		{Name: "AlphaD", Value: 0},
		{Name: "BetaD", Value: 1},
		{Name: "DeltaD", Value: 3},
	}
	if len(got) != len(want) {
		t.Fatalf("extracted %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("extracted %v, want %v", got, want)
		}
	}
}

// TestAllDispositions_CoversTheEnumExactly is the guard #6849 asks for.
//
// AllDispositions drives BOTH the rows and the denominator of the resolution
// table in internal/feedback (#6836) and the disposition_counts / samples maps
// in cmd/grafel. The percentages there are normalised over the sum of the
// enumerated buckets, so a disposition missing from this slice does not merely
// lose its row: its share is REDISTRIBUTED across the survivors, and the reader
// gets a table that sums to 100%, is wrong, and reports no discrepancy
// anywhere. A permanently-0.00% row would at least be visible.
//
// Measured at the time this guard was added (#6849): the slice and the enum
// agreed exactly — 8 constants, 8 entries, no extras, no duplicates. So this is
// a ratchet, not a repair.
//
// There is no exemption list. A Disposition that should not be reported should
// not be declared; deleting the constant is the way to retire one.
func TestAllDispositions_CoversTheEnumExactly(t *testing.T) {
	declared := declaredDispositionsFromSource(t)
	if len(declared) == 0 {
		t.Fatalf("no Disposition constants extracted from %s; guard would be vacuous", dispositionSourceFile)
	}

	if problems := dispositionCoverageProblems(declared, AllDispositions); len(problems) > 0 {
		t.Errorf("AllDispositions is not an exact, duplicate-free enumeration of the %d Disposition "+
			"constants declared in %s:\n  %v\n"+
			"Every consumer (internal/feedback's resolution table, cmd/grafel's disposition_counts) "+
			"normalises over this slice, so an inconsistency here silently rewrites every percentage.",
			len(declared), dispositionSourceFile, problems)
	}
}

// TestDispositionString_HasALabelForEveryDeclaredConstant closes the escape
// hatch named in #6849: Disposition.String() falls back to "unknown" for values
// outside its switch, which is what lets an unenumerated value travel through
// the reports without anything noticing. The fallback itself is left in place —
// String() is total over the int domain and callers may hold a value read back
// from disk — but no DECLARED constant may reach it.
func TestDispositionString_HasALabelForEveryDeclaredConstant(t *testing.T) {
	declared := declaredDispositionsFromSource(t)
	if len(declared) == 0 {
		t.Fatalf("no Disposition constants extracted from %s; guard would be vacuous", dispositionSourceFile)
	}

	seen := map[string]string{}
	for _, c := range declared {
		label := Disposition(c.Value).String()
		if label == "unknown" {
			t.Errorf("%s (value %d) has no case in Disposition.String() and renders as %q",
				c.Name, c.Value, label)
			continue
		}
		if prev, dup := seen[label]; dup {
			t.Errorf("%s and %s both render as %q; the report would merge two buckets into one row",
				prev, c.Name, label)
			continue
		}
		seen[label] = c.Name
	}
}
