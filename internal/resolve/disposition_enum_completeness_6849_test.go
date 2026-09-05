package resolve

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// dispositionSourceDir is the package directory the guard scans. It is the
// DIRECTORY, not a single file, on purpose.
//
// The first cut of this guard parsed only refs.go, which is where the
// Disposition const block lives today. That made it a FILE-scoped roster
// guarding a slice-scoped roster — a constant declared in any other file of
// this package was invisible to it, so the exact defect #6849 exists to
// prevent (a Disposition that exists and never reaches AllDispositions) could
// still be introduced with the whole package green. Scanning the directory is
// what makes the guard enum-scoped rather than file-scoped.
const dispositionSourceDir = "."

// dispositionAnchorFile is where the const block lives today. It is asserted to
// be among the files actually read (a layer-2 pin), but it is NOT the limit of
// the scan — see dispositionSourceDir.
const dispositionAnchorFile = "refs.go"

// dispositionConst is one constant of type Disposition as it is actually
// written in this package's source.
type dispositionConst struct {
	Name  string
	Value int
	File  string
}

// readPackageGoSources returns the FULL contents of every non-test .go file in
// dir, keyed by base name, having verified each length against os.Stat.
//
// #6834 layer 3: a source scanner handed a partial file reports a clean tree
// for the part it never saw, and a token check ("does the text contain
// DispositionExternalSQL?") cannot tell a truncated file from a complete one,
// so the check is a byte-length comparison rather than a content probe.
func readPackageGoSources(t *testing.T, dir string) map[string][]byte {
	t.Helper()

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir %s: %v", dir, err)
	}

	out := map[string][]byte{}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		path := filepath.Join(dir, name)
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("stat %s: %v", path, err)
		}
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		if int64(len(src)) != info.Size() {
			t.Fatalf("read %d bytes of %s but os.Stat reports %d; the scan would silently "+
				"skip the tail of the file", len(src), path, info.Size())
		}
		out[name] = src
	}
	if len(out) == 0 {
		t.Fatalf("no non-test .go files found in %s; the scan has nothing to read", dir)
	}
	return out
}

// constExprValue evaluates the value expression of a const spec sitting at
// index idx of its const block.
//
// It deliberately understands only the two forms that can be evaluated with
// certainty from the AST alone:
//
//	iota          -> idx  (the spec's position in the block)
//	an int literal -> that literal
//
// Anything else — iota arithmetic (`iota + 1`), a reference to another
// constant, a shifted expression — returns ok=false, and the caller FAILS
// LOUDLY rather than guessing. Guessing is the dangerous direction: a wrong
// value silently collides with a real member and makes the completeness guard
// report success. There is no such form in this package today; if one is
// added, the guard says so instead of quietly mis-grading it.
func constExprValue(values []ast.Expr, idx int) (int, bool) {
	if len(values) != 1 {
		return 0, false
	}
	switch v := values[0].(type) {
	case *ast.Ident:
		if v.Name == "iota" {
			return idx, true
		}
	case *ast.BasicLit:
		if v.Kind == token.INT {
			n, err := strconv.Atoi(v.Value)
			if err != nil {
				return 0, false
			}
			return n, true
		}
	}
	return 0, false
}

// declaredDispositionsIn parses one Go source file and returns every constant
// declared with the type Disposition, in source order, plus the names of any
// whose value could not be evaluated.
//
// Const-block type elision is handled: inside a `const (...)` group a spec with
// neither a type nor values repeats the previous spec's type AND its value
// expression, so both are carried forward for exactly that case. That is how
// the Disposition block is written — only DispositionResolved carries the
// `Disposition = iota` annotation.
func declaredDispositionsIn(t *testing.T, filename string, src []byte) (consts []dispositionConst, unevaluable []string) {
	t.Helper()

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, filename, src, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", filename, err)
	}

	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.CONST {
			continue
		}
		var carriedType ast.Expr
		var carriedValues []ast.Expr
		// iota counts every spec in the block, including ones this walk
		// discards, so the index comes from the block-wide loop.
		for i, spec := range gen.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			typ, values := vs.Type, vs.Values
			if typ == nil && len(values) == 0 {
				// Elided spec: repeats the previous type and expression.
				typ, values = carriedType, carriedValues
			} else {
				carriedType, carriedValues = typ, values
			}
			ident, ok := typ.(*ast.Ident)
			if !ok || ident.Name != "Disposition" {
				continue
			}
			for _, name := range vs.Names {
				if name.Name == "_" {
					continue
				}
				val, ok := constExprValue(values, i)
				if !ok {
					unevaluable = append(unevaluable, name.Name+" in "+filename)
					continue
				}
				consts = append(consts, dispositionConst{Name: name.Name, Value: val, File: filename})
			}
		}
	}
	return consts, unevaluable
}

// declaredDispositionsFromSource is the real-tree entry point: every non-test
// .go file of this package, read in full and walked for Disposition constants.
// Results are sorted by (value, name) so the output is deterministic
// independent of directory iteration order.
func declaredDispositionsFromSource(t *testing.T) []dispositionConst {
	t.Helper()

	sources := readPackageGoSources(t, dispositionSourceDir)
	names := make([]string, 0, len(sources))
	for name := range sources {
		names = append(names, name)
	}
	sort.Strings(names)

	var all []dispositionConst
	var unevaluable []string
	for _, name := range names {
		consts, bad := declaredDispositionsIn(t, name, sources[name])
		all = append(all, consts...)
		unevaluable = append(unevaluable, bad...)
	}
	if len(unevaluable) > 0 {
		t.Fatalf("could not evaluate the value of %d Disposition constant(s): %v\n"+
			"The guard refuses to guess: a wrongly-derived value would collide with a real "+
			"member and make the completeness check report success. Teach constExprValue the "+
			"new form, or give the constant a plain iota / integer-literal value.",
			len(unevaluable), unevaluable)
	}

	sort.Slice(all, func(i, j int) bool {
		if all[i].Value != all[j].Value {
			return all[i].Value < all[j].Value
		}
		return all[i].Name < all[j].Name
	})
	return all
}

// dispositionCoverageProblems is the whole decision. It reports every way the
// declared enum and the slice can fail to be in exact correspondence:
//
//   - missing    — a declared constant with no entry in the slice
//   - extra      — a slice entry no constant declares
//   - duplicated — a constant listed more than once (the slice is consumed as a
//     set but declared as a slice; that asymmetry is #6830 in a third place)
//   - aliased    — two constants sharing one value, so one bucket would be
//     reported under two names
//
// Order is deliberately NOT graded. render.go:206 and cmd/grafel/index.go:3057
// do emit their rows in slice order, so a reorder genuinely changes rendered
// output — but only the ORDER of the rows, never a count, a percentage or a
// denominator, because every consumer that computes anything sums or maps over
// the slice. Nothing a reader tracks becomes wrong. A guard that failed on a
// reorder would be over-tight and would be deleted the first time someone
// reordered the block legitimately, taking the real checks with it.
//
// Both this file's real-tree guard and its detector test call THIS function, so
// the detector cannot certify a decision the guard does not make.
func dispositionCoverageProblems(declared []dispositionConst, all []Disposition) []string {
	counts := map[Disposition]int{}
	for _, d := range all {
		counts[d]++
	}
	declaredByValue := map[Disposition]string{}

	var problems []string
	for _, c := range declared {
		if prev, dup := declaredByValue[Disposition(c.Value)]; dup {
			problems = append(problems, fmt.Sprintf(
				"aliased: %s and %s both have value %d, so one bucket would be reported under two names",
				prev, c.Name, c.Value))
			continue
		}
		declaredByValue[Disposition(c.Value)] = c.Name

		switch n := counts[Disposition(c.Value)]; {
		case n == 0:
			problems = append(problems, fmt.Sprintf(
				"missing: %s (value %d, declared in %s) is absent from AllDispositions",
				c.Name, c.Value, c.File))
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
// completeness guard below. If the extraction ever breaks — a moved const
// block, a reworked layout, a botched AST walk, a scan pointed at the wrong
// directory — it would find zero constants and the completeness guard would
// pass by examining nothing (#6834 layers 1-3).
//
// The floor is deliberately NOT stated as a count against len(AllDispositions).
// That comparand would make this test co-fire with the completeness guard on an
// over-long slice, and a check that only ever fails alongside another one is
// ungraded.
//
// Honest note on the sentinels: only their NAME half can fail alone. A walk
// that returned the right names with wrong VALUES necessarily disagrees with
// AllDispositions by value too, so the completeness guard always co-fires on
// that. The value assertions are kept for the error message — they say "the
// walk misread the block" instead of "the slice is wrong" — not because they
// close a gap nothing else covers.
func TestDeclaredDispositionsExtraction_NonVacuous(t *testing.T) {
	sources := readPackageGoSources(t, dispositionSourceDir)
	if _, ok := sources[dispositionAnchorFile]; !ok {
		t.Fatalf("the scan of %s did not read %s, which is where the Disposition const block "+
			"lives; it is looking at the wrong place and the completeness guard would be vacuous",
			dispositionSourceDir, dispositionAnchorFile)
	}

	declared := declaredDispositionsFromSource(t)
	if len(declared) == 0 {
		t.Fatalf("extracted no Disposition constants from %s; the extraction is not reading the "+
			"declarations and the completeness guard would be vacuous", dispositionSourceDir)
	}

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
				name, dispositionSourceDir)
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
		{Name: "A", Value: 0, File: "a.go"},
		{Name: "B", Value: 1, File: "a.go"},
		{Name: "C", Value: 2, File: "a.go"},
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
		name     string
		declared []dispositionConst
		all      []Disposition
		want     string
	}{
		{
			"missing", declared, []Disposition{0, 2},
			"missing: B (value 1, declared in a.go) is absent from AllDispositions",
		},
		{
			// The P4 shape: a constant declared in a DIFFERENT file of the
			// package and never added to the slice.
			"missing_from_another_file",
			append(append([]dispositionConst{}, declared...), dispositionConst{Name: "D", Value: 3, File: "other.go"}),
			complete,
			"missing: D (value 3, declared in other.go) is absent from AllDispositions",
		},
		{
			"extra", declared, []Disposition{0, 1, 2, 9},
			"extra: Disposition(9) appears 1 time(s) in AllDispositions but no constant declares it",
		},
		{
			"duplicate", declared, []Disposition{0, 1, 1, 2},
			"duplicated: B (value 1) appears 2 times in AllDispositions",
		},
		{
			"aliased",
			append(append([]dispositionConst{}, declared...), dispositionConst{Name: "BAlias", Value: 1, File: "other.go"}),
			complete,
			"aliased: B and BAlias both have value 1, so one bucket would be reported under two names",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := dispositionCoverageProblems(tc.declared, tc.all)
			if len(got) != 1 || got[0] != tc.want {
				t.Fatalf("problems = %v, want exactly [%q]", got, tc.want)
			}
		})
	}
}

// TestDeclaredDispositionsIn_ReadsAnArbitraryConstBlock is #6834 layer 4 for
// the AST walk itself: in-memory sources whose answers are known by
// construction, including the shapes that break naive walks — a `_` placeholder
// that still consumes an iota slot, a const block of a different type that must
// be ignored entirely, a constant carrying an explicit integer value, an elided
// spec repeating a literal rather than iota, and an expression the walk must
// refuse to evaluate instead of guessing.
func TestDeclaredDispositionsIn_ReadsAnArbitraryConstBlock(t *testing.T) {
	t.Run("iota_underscore_and_foreign_block", func(t *testing.T) {
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
		got, bad := declaredDispositionsIn(t, "synthetic.go", src)
		assertConsts(t, got, bad, []dispositionConst{
			{Name: "AlphaD", Value: 0, File: "synthetic.go"},
			{Name: "BetaD", Value: 1, File: "synthetic.go"},
			{Name: "DeltaD", Value: 3, File: "synthetic.go"},
		})
	})

	t.Run("explicit_values_and_literal_elision", func(t *testing.T) {
		// This is the P4 shape at the AST level: a constant whose value is a
		// plain literal, not an iota position. An index-based walk would read
		// GammaD as 0 and silently collide it with the first real member.
		src := []byte(`package p

type Disposition int

const GammaD Disposition = 8

const (
	HotelD Disposition = 40
	IndiaD
)
`)
		got, bad := declaredDispositionsIn(t, "other.go", src)
		assertConsts(t, got, bad, []dispositionConst{
			{Name: "GammaD", Value: 8, File: "other.go"},
			{Name: "HotelD", Value: 40, File: "other.go"},
			{Name: "IndiaD", Value: 40, File: "other.go"}, // elision repeats the literal
		})
	})

	t.Run("unevaluable_expression_is_refused_not_guessed", func(t *testing.T) {
		src := []byte(`package p

type Disposition int

const (
	JulietD Disposition = iota + 1
)
`)
		got, bad := declaredDispositionsIn(t, "arith.go", src)
		if len(got) != 0 {
			t.Errorf("guessed a value for an unevaluable expression: %v", got)
		}
		if len(bad) != 1 || bad[0] != "JulietD in arith.go" {
			t.Fatalf("unevaluable = %v, want [\"JulietD in arith.go\"]", bad)
		}
	})
}

func assertConsts(t *testing.T, got []dispositionConst, bad []string, want []dispositionConst) {
	t.Helper()
	if len(bad) != 0 {
		t.Fatalf("unexpected unevaluable constants: %v", bad)
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
// table in internal/feedback (#6836) and the disposition_counts /
// disposition_samples maps in cmd/grafel/index.go. Because this guard grades
// the shared ROOT rather than any one consumer, it protects every consumer at
// once — present and future — which is the property a per-consumer test cannot
// have.
//
// The percentages in those reports are normalised over the sum of the
// enumerated buckets, so a disposition missing from this slice does not merely
// lose its row: its share is REDISTRIBUTED across the survivors, and the reader
// gets a table that sums to 100%, is wrong, and reports no discrepancy
// anywhere. A permanently-0.00% row would at least be visible.
//
// Measured at the time this guard was added (#6849): the slice and the enum
// agreed exactly — 8 constants, 8 entries, no extras, no duplicates, no
// aliases. So this is a ratchet, not a repair.
//
// There is no exemption list. A Disposition that should not be reported should
// not be declared; deleting the constant is the way to retire one.
func TestAllDispositions_CoversTheEnumExactly(t *testing.T) {
	declared := declaredDispositionsFromSource(t)
	if len(declared) == 0 {
		t.Fatalf("no Disposition constants extracted from %s; guard would be vacuous", dispositionSourceDir)
	}

	if problems := dispositionCoverageProblems(declared, AllDispositions); len(problems) > 0 {
		t.Errorf("AllDispositions is not an exact, duplicate-free enumeration of the %d Disposition "+
			"constants declared in this package:\n  %v\n"+
			"Every consumer (internal/feedback's resolution table, cmd/grafel's disposition_counts) "+
			"normalises over this slice, so an inconsistency here silently rewrites every percentage.",
			len(declared), problems)
	}
}

// TestDispositionString_HasALabelForEveryDeclaredConstant closes the escape
// hatch named in #6849: Disposition.String() falls back to "unknown" for values
// outside its switch, which is what would let an unenumerated value travel
// through the reports without anything noticing.
//
// The fallback itself is left in place — String() needs a terminal return
// regardless, so totality over the int domain is free — but no DECLARED
// constant may reach it. Note that nothing in internal/ or cmd/ currently
// converts an int into a Disposition or deserialises one, so the fallback is
// unreachable in practice today; that is a reason to leave it alone, not a
// reason to rely on it.
func TestDispositionString_HasALabelForEveryDeclaredConstant(t *testing.T) {
	declared := declaredDispositionsFromSource(t)
	if len(declared) == 0 {
		t.Fatalf("no Disposition constants extracted from %s; guard would be vacuous", dispositionSourceDir)
	}

	seen := map[string]string{}
	for _, c := range declared {
		label := Disposition(c.Value).String()
		if label == "unknown" {
			t.Errorf("%s (value %d, declared in %s) has no case in Disposition.String() and renders as %q",
				c.Name, c.Value, c.File, label)
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
