package types_test

// all_entity_kinds_roster_6830_test.go — grades types.AllEntityKinds() as the
// SLICE it is declared and returned as, against the EntityKind constants the
// package actually declares.
//
// Two gaps are closed here, filed separately but sharing one mechanism (#6830,
// #6832): every incumbent guard on this roster treats it as a SET.
//
//   - #6830 — a constant listed twice collapses under a set comparison. The
//     incumbent count check in TestEntityKindDeclarations6776_MatchAllEntityKindsExactly
//     compares len(declared), which counts CONSTANTS PARSED FROM SOURCE,
//     against len(listed), which is a set — so neither comparand is the length
//     of the slice being validated.
//   - #6832 — the incumbent element collector took only *ast.Ident elements, so
//     an element written as a conversion literal, EntityKind("SCOPE.X"), was
//     invisible to it.
//
// MEASURED at the time this guard was added: 93 EntityKind constants declared,
// 93 elements in AllEntityKinds() (94/94 since #6776 arm B9), every element an *ast.Ident, zero duplicate
// values, zero conversion literals, zero constants sharing a value, and no
// EntityKind constant declared outside kinds.go. So this ships as a RATCHET,
// not a repair — the roster is correct today and nothing observed that it was.
//
// Practical blast radius, surveyed because #6830 recorded it as unexamined:
// the roster has exactly TWO production consumers, and BOTH de-duplicate —
// types.IsValidEntityKind (a linear scan with an early return) and
// internal/graph/fbwriter's entityEnumKindSet (a map built from it, where
// len(all) is only a capacity hint). A duplicate therefore changes no shipped
// behaviour today. The contract is still wrong: AllEntityKinds() is a public,
// slice-returning API, and any future consumer that emits one row per element,
// builds a histogram or drives a table test from it would be wrong with
// nothing to say so. One TEST consumer is already length-sensitive
// (internal/cli's TestKindConstantsAreASCII derives a vacuity floor from
// len(types.AllEntityKinds())+len(types.AllRelationshipKinds())), which is why
// a duplicate is not literally ALIVE repo-wide — but it fails there with the
// diagnosis "the declaration shape changed and this test is no longer reading
// the source of truth", which sends the reader to the wrong file entirely.

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/types"
)

// entityKindSourceDir is the package DIRECTORY the guard scans, not a single
// file, on purpose.
//
// kinds.go is where every EntityKind constant lives today, and a file-scoped
// scan would be a file-scoped roster guarding a package-scoped enum: a constant
// declared in any other file of internal/types would be invisible to it, and
// the exact defect this guard exists to prevent — an EntityKind that exists and
// never reaches AllEntityKinds() — could be introduced with the package green.
// The sibling guard for internal/resolve (#6849) shipped file-scoped first and
// had to be widened for precisely this reason.
const entityKindSourceDir = "."

// minScannedEntityKindGoFiles is a floor under the package scan, set from a
// MEASUREMENT: the scan reports 11 non-test .go files in internal/types. The
// floor is deliberately slack — it catches a filter that COLLAPSES the scan,
// not the file count, which churns.
//
// A count floor alone is provably not enough here, and internal/types is a
// sharp instance: it holds 13 _test.go files against 11 non-test ones, so a
// filter inverted to test files reads MORE files than the real scan and clears
// any floor the real scan could satisfy, while reading none of the sources this
// guard grades. Only the named anchors below catch that — confirmed by mutant
// rather than argued: the inverted filter reported "the walk read 13 file(s)"
// and died on a missing ANCHOR, with the floor of 6 comfortably cleared.
//
// Both counts re-derived from disk after this branch was rebased onto main.
const minScannedEntityKindGoFiles = 6

// entityKindMustScan names production files of this package the scan MUST have
// read, each with tokens that must appear in the bytes actually collected.
//
// These anchors are what makes the WIDENING to a directory scan graded. Without
// them, narrowing the filter back to kinds.go alone — the file-scoped shape
// this guard deliberately is not — leaves the package green while silently
// re-opening the hole. The scan proving it read SOMETHING (the len == 0 floor)
// and that it read kinds.go does not observe that it read the REST of the
// package, which is the entire content of the widening.
//
// Known limitation, stated rather than left to be rediscovered: these anchors
// pin FOUR files out of eleven. A filter excluding some other single non-anchor
// file would still clear both the floor and this list. That is inherent to the
// pattern (the same is true of internal/links/source_read_guard_6823_test.go
// and internal/resolve's #6849 guard, which this follows): it catches a scan
// that collapses, not one that loses one file. Widen the list if a future
// defect shows that is not enough.
var entityKindMustScan = []struct {
	file   string
	tokens []string
}{
	{"kinds.go", []string{"package types", "func AllEntityKinds() []EntityKind {"}},
	{"entity.go", []string{"package types", "type EntityRecord struct {"}},
	{"relationship.go", []string{"package types", "type RelationshipRecord struct {"}},
	{"derived_kinds.go", []string{"package types", "func AllDerivedRelationshipKinds() []RelationshipKind {"}},
}

// readEntityKindPackageSources returns the FULL contents of every non-test .go
// file in dir, keyed by base name, each length verified against os.Stat.
//
// The byte-length comparison is deliberate and is not interchangeable with a
// token probe: a scanner handed a PREFIX of a file reports a clean tree for the
// tail it never saw, and "does this text contain EntityKindClass?" cannot tell
// a truncated file from a complete one.
func readEntityKindPackageSources(t *testing.T, dir string) map[string][]byte {
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
	assertScannedTheTypesPackage(t, dir, out)
	return out
}

// assertScannedTheTypesPackage is proof that the walk read the RIGHT files, not
// merely some files.
//
// Its failures deliberately say "the scan is broken, not the roster". That is
// the opposite diagnosis from the completeness guard's "a constant is missing",
// and a reader handed the wrong one would go hunting for a taxonomy bug that
// does not exist while the real defect is that the guard stopped looking.
func assertScannedTheTypesPackage(t *testing.T, dir string, scanned map[string][]byte) {
	t.Helper()

	// The anchor list must itself be graded. Emptying entityKindMustScan makes
	// every check below inert while the package stays green, so it is asserted
	// rather than trusted (scored here as its own mutant, not inferred).
	//
	// Three anchor lists in this batch share the pattern. Cited by their real
	// locations because an earlier draft of this comment attributed the first
	// of them to the wrong package:
	//
	//   - goScanAnchors — internal/types/producer_entity_kinds_6776_test.go,
	//     THIS package, not internal/links. Its own doc records the emptied
	//     list as having left the package green.
	//   - dispositionMustScan — internal/resolve/disposition_enum_completeness_6849_test.go.
	//     Same result recorded on #6853.
	//   - a function-local mustScan — internal/links/source_read_guard_6823_test.go,
	//     the guard this whole form derives from. Named for provenance only: no
	//     emptied-and-ALIVE result is recorded for it, and none is claimed here.
	if len(entityKindMustScan) < 3 {
		t.Fatalf("the scan is broken, not the roster: the must-scan anchor list holds %d entr(ies), "+
			"want at least 3. Emptying or gutting it fails nothing on its own, so a filter that "+
			"reads enough files to clear the floor while missing this package's real sources would "+
			"go unnoticed and this whole layer would be inert (#6830).", len(entityKindMustScan))
	}

	if len(scanned) < minScannedEntityKindGoFiles {
		t.Fatalf("the scan is broken, not the roster: it read only %d non-test .go file(s) from %s, "+
			"want at least %d; the completeness guard would be grading a fraction of the package",
			len(scanned), dir, minScannedEntityKindGoFiles)
	}

	for _, m := range entityKindMustScan {
		body, ok := scanned[m.file]
		if !ok {
			t.Fatalf("the scan is broken, not the roster: %s was never read from %s. The walk read "+
				"%d file(s), but not the production sources this guard grades — an EntityKind "+
				"constant declared outside the files it did read is invisible to it (#6830).",
				m.file, dir, len(scanned))
		}
		info, err := os.Stat(filepath.Join(dir, m.file))
		if err != nil {
			t.Fatalf("stat %s: %v", filepath.Join(dir, m.file), err)
		}
		if int64(len(body)) != info.Size() {
			t.Fatalf("the scan is broken, not the roster: it collected %d bytes for %s but the file "+
				"is %d — it read a PREFIX, so every declaration past the cut is invisible",
				len(body), m.file, info.Size())
		}
		for _, tok := range m.tokens {
			if !bytes.Contains(body, []byte(tok)) {
				t.Fatalf("the scan is broken, not the roster: the bytes collected for %s do not "+
					"contain %q; the walk is reading something else under that name", m.file, tok)
			}
		}
	}
}

// entityKindConst is one `Name EntityKind = "VALUE"` constant as it is actually
// written in this package's source.
type entityKindConst struct {
	Name  string
	Value string
	File  string
}

// constStringValue evaluates the value expression of a const spec.
//
// It understands exactly one form with certainty — an unquoted string literal —
// and refuses everything else (a reference to another constant, a concatenation,
// an arithmetic expression). Guessing is the dangerous direction: a wrongly
// derived value silently collides with a real member and makes the completeness
// check report success. Every EntityKind constant in the tree is a plain string
// literal today; if one stops being, the guard says so rather than mis-grading.
func constStringValue(values []ast.Expr) (string, bool) {
	if len(values) != 1 {
		return "", false
	}
	bl, ok := values[0].(*ast.BasicLit)
	if !ok || bl.Kind != token.STRING {
		return "", false
	}
	v, err := strconv.Unquote(bl.Value)
	if err != nil {
		return "", false
	}
	return v, true
}

// declaredEntityKindsIn parses one Go source file and returns every constant
// declared with the type EntityKind, in source order, plus the names of any
// whose value could not be evaluated.
//
// Const-block type elision is handled: inside a `const (...)` group a spec with
// neither a type nor values repeats the previous spec's type AND its value
// expression, so both are carried forward for exactly that case. No EntityKind
// constant is written that way today; the handling exists so that one added
// later is READ rather than skipped.
//
// THE REFUSAL CHANNEL (`unevaluable`) IS LOAD-BEARING, and is graded by
// TestDeclaredEntityKindsIn6830_RefusesWhatItCannotEvaluate rather than by the
// real tree — there are zero non-literal EntityKind constants in the tree, which
// is exactly why it went ungraded on the first pass. Neutering the append here
// (`_ = unevaluable`) leaves ./internal/types GREEN, and with
// `const X EntityKind = EntityKind("SCOPE.ZZUneval")` planted in entity.go the
// whole package still passes: the constant is skipped, is absent from
// AllEntityKinds(), and is reported by nothing. Restoring the refusal fails both
// entry points with "could not evaluate the value of 1 EntityKind constant(s)".
// Both measured. This is the mechanism that makes the walk LOUD instead of
// BLIND, and deleting it costs nothing today.
//
// STATED LIMITATION — two shapes this walk skips IN SILENCE (verified ALIVE
// across internal/types, internal/entkinds and internal/graph/fbwriter, on this
// branch; the reviewer reports the same on base). Both are genuine EntityKind
// constants by every rule Go has, and both are deliberately NOT fixed here:
// widening the collector is a scope increase on a ratchet PR and wants its own
// issue, in the same way #6832 was split from #6830.
//
//	type EntityKindAliasZZ = EntityKind          // a TYPE ALIAS: same type,
//	const A EntityKindAliasZZ = "SCOPE.ZZAlias"  // but the AST says "EntityKindAliasZZ"
//
//	const B = EntityKind("SCOPE.ZZNoType")       // NO type expression at all
//
// Note the boundary this draws against the refusal above, because it is sharp
// and it is backwards: an EXPLICITLY TYPED const with a conversion value is
// refused LOUDLY, while dropping the type makes the very same value SILENT.
// Same value, same conversion, opposite handling. The gate is
// `typ.(*ast.Ident).Name == "EntityKind"` and it `continue`s without a word on
// anything else — which is #6832's defect shape (a collector that ignores what
// it does not recognise) reappearing on the DECLARATION side, one level from
// where this PR closes it on the ELEMENT side.
func declaredEntityKindsIn(t *testing.T, filename string, src []byte) (consts []entityKindConst, unevaluable []string) {
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
		for _, spec := range gen.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			typ, values := vs.Type, vs.Values
			if typ == nil && len(values) == 0 {
				typ, values = carriedType, carriedValues
			} else {
				carriedType, carriedValues = typ, values
			}
			ident, ok := typ.(*ast.Ident)
			if !ok || ident.Name != "EntityKind" {
				continue
			}
			for _, name := range vs.Names {
				if name.Name == "_" {
					continue
				}
				val, ok := constStringValue(values)
				if !ok {
					unevaluable = append(unevaluable, name.Name+" in "+filename)
					continue
				}
				consts = append(consts, entityKindConst{Name: name.Name, Value: val, File: filename})
			}
		}
	}
	return consts, unevaluable
}

// declaredEntityKindsFromSource is the real-tree entry point: every non-test
// .go file of internal/types, read in full and walked for EntityKind constants.
// Results are sorted by (value, name) so the output does not depend on
// directory iteration order.
func declaredEntityKindsFromSource(t *testing.T) []entityKindConst {
	t.Helper()

	sources := readEntityKindPackageSources(t, entityKindSourceDir)
	names := make([]string, 0, len(sources))
	for name := range sources {
		names = append(names, name)
	}
	sort.Strings(names)

	var all []entityKindConst
	var unevaluable []string
	for _, name := range names {
		consts, bad := declaredEntityKindsIn(t, name, sources[name])
		all = append(all, consts...)
		unevaluable = append(unevaluable, bad...)
	}
	if len(unevaluable) > 0 {
		t.Fatalf("could not evaluate the value of %d EntityKind constant(s): %v\n"+
			"The guard refuses to guess: a wrongly derived value would collide with a real member "+
			"and make the completeness check report success. Teach constStringValue the new form, "+
			"or give the constant a plain string-literal value.", len(unevaluable), unevaluable)
	}

	sort.Slice(all, func(i, j int) bool {
		if all[i].Value != all[j].Value {
			return all[i].Value < all[j].Value
		}
		return all[i].Name < all[j].Name
	})
	return all
}

// rosterElement is one element of the AllEntityKinds() slice literal, AS
// WRITTEN. The whole point of #6832 is that the element's SHAPE matters and had
// been discarded, so it is carried here rather than normalised away.
//
// Exactly one of Name / Literal is set for a classified element; Raw is the
// source text and is set for every element, including the ones the classifier
// refuses.
type rosterElement struct {
	Index   int
	Raw     string
	Name    string // set when the element is a named constant identifier
	Literal string // set when the element is an EntityKind("…") conversion
	OK      bool   // false when the classifier could not recognise the shape
}

// allEntityKindsRosterElements returns every element of the composite literal
// returned by AllEntityKinds(), in slice order, classified by shape.
//
// #6832: the incumbent collector took only *ast.Ident elements and SKIPPED
// everything else in silence, so `EntityKind("SCOPE.Template")` smuggled into
// the roster left internal/types fully green. This one recognises the
// conversion shape and, critically, marks anything it cannot recognise as
// OK=false so the caller can fail on it. A collector that ignores what it does
// not recognise keeps acquiring blind spots; that is how this defect got here.
func allEntityKindsRosterElements(t *testing.T, src []byte) []rosterElement {
	t.Helper()

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "kinds.go", src, 0)
	if err != nil {
		t.Fatalf("parse kinds.go: %v", err)
	}

	var body *ast.FuncDecl
	for _, d := range file.Decls {
		fd, ok := d.(*ast.FuncDecl)
		if ok && fd.Name.Name == "AllEntityKinds" && fd.Recv == nil {
			body = fd
			break
		}
	}
	if body == nil {
		t.Fatalf("no func AllEntityKinds found in the scanned source; the guard is reading the "+
			"wrong bytes (scanned %d bytes)", len(src))
	}

	var lits []*ast.CompositeLit
	ast.Inspect(body.Body, func(n ast.Node) bool {
		if cl, ok := n.(*ast.CompositeLit); ok {
			lits = append(lits, cl)
			return false // do not descend: a nested literal is not a roster element
		}
		return true
	})
	if len(lits) != 1 {
		t.Fatalf("AllEntityKinds contains %d top-level composite literal(s), want exactly 1; the "+
			"function was restructured and this guard is no longer reading the roster", len(lits))
	}

	out := make([]rosterElement, 0, len(lits[0].Elts))
	for i, e := range lits[0].Elts {
		el := rosterElement{Index: i, Raw: exprText(fset, e)}
		switch x := e.(type) {
		case *ast.Ident:
			el.Name, el.OK = x.Name, true
		case *ast.CallExpr:
			fn, ok := x.Fun.(*ast.Ident)
			if ok && fn.Name == "EntityKind" && len(x.Args) == 1 {
				if v, ok := constStringValue(x.Args); ok {
					el.Literal, el.OK = v, true
				}
			}
		}
		out = append(out, el)
	}
	return out
}

// exprText renders an expression back to source text for error messages.
func exprText(fset *token.FileSet, e ast.Expr) string {
	var buf bytes.Buffer
	if err := printer.Fprint(&buf, fset, e); err != nil {
		return fmt.Sprintf("%T", e)
	}
	return buf.String()
}

// entityKindRosterProblems is the whole decision. It reports every way the
// declared enum and the roster slice can fail to be in exact correspondence:
//
//   - unclassified — an element shape the collector cannot read (#6832)
//   - literal      — an element written as EntityKind("…") rather than as the
//     named constant; refused as a matter of policy, see below
//   - unknown      — an element naming an identifier no EntityKind constant declares
//   - extra        — an element whose VALUE no constant declares
//   - duplicated   — a value the roster carries more than once (#6830)
//   - missing      — a declared constant whose value the roster never carries
//   - aliased      — two constants sharing one value, so one bucket would be
//     reported under two names
//
// Policy on conversion literals: they are REFUSED, not accepted-and-matched.
// The roster exists to be the closed list of the constant block, and an element
// that spells a value inline bypasses the block entirely — it is exactly the
// shape that let a fabricated kind reach IsValidEntityKind. Handling it means
// SEEING it, which is what #6832 asks for; seeing it and permitting it would
// leave the smuggling route open under a friendlier name. A literal is still
// graded on its value as well, so the diagnosis names both faults.
//
// Order is deliberately NOT graded. Both production consumers are order-blind
// (a linear scan with early return, and a map build), so a reorder changes
// nothing a reader tracks. A guard that failed on a reorder would be over-tight
// and would be deleted the first time someone reordered the block legitimately,
// taking the real checks with it.
func entityKindRosterProblems(declared []entityKindConst, elements []rosterElement) []string {
	declaredByName := map[string]entityKindConst{}
	declaredByValue := map[string]string{}

	var problems []string
	for _, c := range declared {
		declaredByName[c.Name] = c
		if prev, dup := declaredByValue[c.Value]; dup {
			problems = append(problems, fmt.Sprintf(
				"aliased: %s and %s both have value %q, so one bucket would be reported under two names",
				prev, c.Name, c.Value))
			continue
		}
		declaredByValue[c.Value] = c.Name
	}

	counts := map[string]int{}
	for _, el := range elements {
		switch {
		case !el.OK:
			problems = append(problems, fmt.Sprintf(
				"unclassified: element %d is written as %s, a shape the roster collector cannot read; "+
					"it would be skipped in silence and graded by nothing (#6832)", el.Index, el.Raw))
		case el.Name != "":
			c, ok := declaredByName[el.Name]
			if !ok {
				problems = append(problems, fmt.Sprintf(
					"unknown: element %d names %s, which no EntityKind constant declares", el.Index, el.Name))
				continue
			}
			counts[c.Value]++
		default:
			problems = append(problems, fmt.Sprintf(
				"literal: element %d is written as %s instead of the named constant; the roster must "+
					"list constants from the block, not spell values inline (#6832)", el.Index, el.Raw))
			counts[el.Literal]++
		}
	}

	for value, n := range counts {
		name, ok := declaredByValue[value]
		if !ok {
			problems = append(problems, fmt.Sprintf(
				"extra: the roster carries %q %d time(s) but no EntityKind constant declares it", value, n))
			continue
		}
		if n > 1 {
			problems = append(problems, fmt.Sprintf(
				"duplicated: %s (value %q) appears %d times in AllEntityKinds()", name, value, n))
		}
	}
	for _, c := range declared {
		if declaredByValue[c.Value] != c.Name {
			continue // an alias; already reported above
		}
		if counts[c.Value] == 0 {
			problems = append(problems, fmt.Sprintf(
				"missing: %s (value %q, declared in %s) is absent from AllEntityKinds()",
				c.Name, c.Value, c.File))
		}
	}
	sort.Strings(problems)
	return problems
}

// TestEntityKindRosterExtraction6830_NonVacuous is the floor under the guard
// below. If the extraction ever breaks — a moved const block, a reworked
// AllEntityKinds, a botched AST walk, a scan pointed at the wrong directory —
// it would find zero constants or zero elements and the completeness guard
// would pass by examining nothing.
//
// The floor is deliberately NOT stated as len(elements) == len(declared): that
// comparand would make this test co-fire with the completeness guard on every
// roster defect, and a check that only ever fails alongside another one is
// ungraded.
func TestEntityKindRosterExtraction6830_NonVacuous(t *testing.T) {
	// readEntityKindPackageSources runs assertScannedTheTypesPackage for every
	// caller, so by the time this returns the scan has already been proved to
	// have read the whole package rather than a fraction of it.
	declared := declaredEntityKindsFromSource(t)
	if len(declared) == 0 {
		t.Fatalf("extracted no EntityKind constants from %s; the completeness guard would be vacuous",
			entityKindSourceDir)
	}

	src, err := os.ReadFile(filepath.Join(entityKindSourceDir, "kinds.go"))
	if err != nil {
		t.Fatalf("read kinds.go: %v", err)
	}
	elements := allEntityKindsRosterElements(t, src)
	if len(elements) == 0 {
		t.Fatal("extracted no elements from AllEntityKinds(); the completeness guard would be vacuous")
	}

	// The roster is read from source, but it is types.AllEntityKinds() that
	// ships. Tie the two together so a guard reading a stale or wrong file
	// cannot pass: the element count must equal the length of the SLICE the
	// package actually returns. This is the only assertion in this file that
	// observes len() of the returned slice directly, and it is the reason the
	// guard grades the API rather than a text file that resembles it.
	if got := len(types.AllEntityKinds()); got != len(elements) {
		t.Fatalf("AllEntityKinds() returns %d elements at run time but the source walk found %d; "+
			"the guard is reading bytes that are not the ones being compiled", got, len(elements))
	}

	byName := map[string]string{}
	for _, d := range declared {
		byName[d.Name] = d.Value
	}
	sentinels := map[string]types.EntityKind{
		"EntityKindClass":                  types.EntityKindClass,
		"EntityKindTemplate":               types.EntityKindTemplate,
		"HTTPEndpointKindLegacy":           types.HTTPEndpointKindLegacy,
		"EntityKindHTTPEndpointDefinition": types.EntityKindHTTPEndpointDefinition,
	}
	for name, want := range sentinels {
		got, ok := byName[name]
		if !ok {
			t.Errorf("extraction did not find %s in %s; the AST walk is not reading the declarations",
				name, entityKindSourceDir)
			continue
		}
		if types.EntityKind(got) != want {
			t.Errorf("extraction read %s = %q, want %q", name, got, string(want))
		}
	}
}

// TestEntityKindRosterProblems6830_DetectsEachFailure is proof that the matcher
// can actually detect, exercised on fabricated inputs so that the real tree
// being healthy does not certify a matcher that reports nothing.
//
// PERMISSIVE direction FIRST: a real-shaped healthy input must produce NO
// problems, and a REORDERED one must produce none either. A recall-shaped suite
// is blind to over-firing, and an over-tight roster guard is the kind the next
// person deletes.
func TestEntityKindRosterProblems6830_DetectsEachFailure(t *testing.T) {
	declared := []entityKindConst{
		{Name: "EntityKindA", Value: "SCOPE.A", File: "kinds.go"},
		{Name: "EntityKindB", Value: "SCOPE.B", File: "kinds.go"},
		{Name: "EntityKindC", Value: "SCOPE.C", File: "kinds.go"},
	}
	named := func(names ...string) []rosterElement {
		out := make([]rosterElement, 0, len(names))
		for i, n := range names {
			out = append(out, rosterElement{Index: i, Raw: n, Name: n, OK: true})
		}
		return out
	}
	healthy := named("EntityKindA", "EntityKindB", "EntityKindC")

	if got := entityKindRosterProblems(declared, healthy); len(got) != 0 {
		t.Fatalf("a complete roster reported problems %v; the guard over-fires", got)
	}
	if got := entityKindRosterProblems(declared, named("EntityKindC", "EntityKindA", "EntityKindB")); len(got) != 0 {
		t.Errorf("a reordered roster reported problems %v; the guard is over-tight and will be "+
			"deleted by whoever next reorders the block legitimately", got)
	}

	cases := []struct {
		name     string
		declared []entityKindConst
		elements []rosterElement
		want     string
	}{
		{
			"duplicate", declared,
			named("EntityKindA", "EntityKindB", "EntityKindB", "EntityKindC"),
			`duplicated: EntityKindB (value "SCOPE.B") appears 2 times in AllEntityKinds()`,
		},
		{
			"missing", declared, named("EntityKindA", "EntityKindC"),
			`missing: EntityKindB (value "SCOPE.B", declared in kinds.go) is absent from AllEntityKinds()`,
		},
		{
			// The #6849 P4 shape: a constant declared in a DIFFERENT file of the
			// package and never added to the roster. Only a directory scan finds it.
			"missing_from_another_file",
			append(append([]entityKindConst{}, declared...),
				entityKindConst{Name: "EntityKindD", Value: "SCOPE.D", File: "derived_kinds.go"}),
			healthy,
			`missing: EntityKindD (value "SCOPE.D", declared in derived_kinds.go) is absent from AllEntityKinds()`,
		},
		{
			"unknown_identifier", declared,
			append(append([]rosterElement{}, healthy...),
				rosterElement{Index: 3, Raw: "EntityKindZ", Name: "EntityKindZ", OK: true}),
			"unknown: element 3 names EntityKindZ, which no EntityKind constant declares",
		},
		{
			"aliased",
			append(append([]entityKindConst{}, declared...),
				entityKindConst{Name: "EntityKindBAlias", Value: "SCOPE.B", File: "kinds.go"}),
			healthy,
			`aliased: EntityKindB and EntityKindBAlias both have value "SCOPE.B", so one bucket would be reported under two names`,
		},
		{
			// #6832 layer 1: a shape the collector could not read at all.
			"unclassified_shape", declared,
			append(append([]rosterElement{}, healthy...),
				rosterElement{Index: 3, Raw: "someHelper()", OK: false}),
			"unclassified: element 3 is written as someHelper(), a shape the roster collector cannot " +
				"read; it would be skipped in silence and graded by nothing (#6832)",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := entityKindRosterProblems(tc.declared, tc.elements)
			if len(got) != 1 || got[0] != tc.want {
				t.Fatalf("problems = %v, want exactly [%q]", got, tc.want)
			}
		})
	}

	// #6832's two headline shapes report TWO faults each, deliberately: the
	// literal is refused on policy AND graded on its value. Asserted as exact
	// sets so neither half can quietly stop firing.
	t.Run("literal_element_duplicating_a_real_value", func(t *testing.T) {
		got := entityKindRosterProblems(declared, append(append([]rosterElement{}, healthy...),
			rosterElement{Index: 3, Raw: `EntityKind("SCOPE.B")`, Literal: "SCOPE.B", OK: true}))
		want := []string{
			`duplicated: EntityKindB (value "SCOPE.B") appears 2 times in AllEntityKinds()`,
			`literal: element 3 is written as EntityKind("SCOPE.B") instead of the named constant; ` +
				"the roster must list constants from the block, not spell values inline (#6832)",
		}
		assertProblems(t, got, want)
	})
	t.Run("literal_element_fabricating_a_value", func(t *testing.T) {
		got := entityKindRosterProblems(declared, append(append([]rosterElement{}, healthy...),
			rosterElement{Index: 3, Raw: `EntityKind("SCOPE.ZZ")`, Literal: "SCOPE.ZZ", OK: true}))
		want := []string{
			`extra: the roster carries "SCOPE.ZZ" 1 time(s) but no EntityKind constant declares it`,
			`literal: element 3 is written as EntityKind("SCOPE.ZZ") instead of the named constant; ` +
				"the roster must list constants from the block, not spell values inline (#6832)",
		}
		assertProblems(t, got, want)
	})
}

func assertProblems(t *testing.T, got, want []string) {
	t.Helper()
	sort.Strings(want)
	if len(got) != len(want) {
		t.Fatalf("problems = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("problems = %v, want %v", got, want)
		}
	}
}

// TestDeclaredEntityKindsIn6830_RefusesWhatItCannotEvaluate grades the
// DECLARATION walk and, specifically, its refusal channel.
//
// It runs on synthetic source rather than the real tree ON PURPOSE: the tree
// holds zero EntityKind constants with a non-literal value, so the real-tree
// guard exercises this path not at all — which is precisely how neutering the
// refusal (`_ = unevaluable`) stayed ALIVE with ./internal/types green, planted
// counter-example and all. See the note on declaredEntityKindsIn.
//
// PERMISSIVE FIRST: the ordinary shapes must produce NO refusals, or the walk
// refuses everything and grades nothing.
func TestDeclaredEntityKindsIn6830_RefusesWhatItCannotEvaluate(t *testing.T) {
	t.Run("literals_and_elision_are_read_not_refused", func(t *testing.T) {
		got, bad := declaredEntityKindsIn(t, "synthetic.go", []byte(`package types

type EntityKind string
type Other string

const (
	EntityKindAlphaZZ EntityKind = "SCOPE.AlphaZZ"
	EntityKindBetaZZ  EntityKind = "SCOPE.BetaZZ"
	_                 EntityKind = "SCOPE.BlankZZ"
)

const EntityKindGammaZZ EntityKind = "SCOPE.GammaZZ"

const (
	IgnoredO Other = "not.an.entity.kind"
)
`))
		if len(bad) != 0 {
			t.Fatalf("refused %v; plain string literals must be READ, not refused — a walk that "+
				"refuses everything grades nothing", bad)
		}
		want := []entityKindConst{
			{Name: "EntityKindAlphaZZ", Value: "SCOPE.AlphaZZ", File: "synthetic.go"},
			{Name: "EntityKindBetaZZ", Value: "SCOPE.BetaZZ", File: "synthetic.go"},
			{Name: "EntityKindGammaZZ", Value: "SCOPE.GammaZZ", File: "synthetic.go"},
		}
		if len(got) != len(want) {
			t.Fatalf("consts = %+v, want %+v (the `_` spec must be dropped and the Other block "+
				"ignored entirely)", got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("consts = %+v, want %+v", got, want)
			}
		}
	})

	// The mutant that was ALIVE: a typed const whose value is a CONVERSION
	// rather than a literal. It must reach `unevaluable` and must NOT be
	// silently dropped into the void, and it must NOT be guessed at either —
	// a guessed value collides with a real member and makes the completeness
	// guard report success.
	t.Run("typed_conversion_value_is_refused_not_guessed", func(t *testing.T) {
		got, bad := declaredEntityKindsIn(t, "entity.go", []byte(`package types

type EntityKind string

const EntityKindZZUneval6830 EntityKind = EntityKind("SCOPE.ZZUneval")
`))
		if len(got) != 0 {
			t.Errorf("guessed a value for a conversion expression: %+v", got)
		}
		if len(bad) != 1 || bad[0] != "EntityKindZZUneval6830 in entity.go" {
			t.Fatalf("unevaluable = %v, want [\"EntityKindZZUneval6830 in entity.go\"]; the refusal "+
				"channel is what makes this walk loud instead of blind, and neutering it leaves the "+
				"whole package green", bad)
		}
	})

	// A second, differently-shaped unevaluable value, so the case above cannot
	// be satisfied by special-casing *ast.CallExpr alone.
	t.Run("concatenated_value_is_refused_not_guessed", func(t *testing.T) {
		got, bad := declaredEntityKindsIn(t, "concat.go", []byte(`package types

type EntityKind string

const scopePrefixZZ = "SCOPE."

const EntityKindZZConcat EntityKind = scopePrefixZZ + "ZZConcat"
`))
		if len(got) != 0 {
			t.Errorf("guessed a value for a concatenation: %+v", got)
		}
		if len(bad) != 1 || bad[0] != "EntityKindZZConcat in concat.go" {
			t.Fatalf("unevaluable = %v, want [\"EntityKindZZConcat in concat.go\"]", bad)
		}
	})
}

// TestAllEntityKindsRosterElements6830_ReadsArbitraryElementShapes is layer 4
// for the AST collector itself: in-memory sources whose answers are known by
// construction, including the shapes that break naive collectors.
//
// The middle sub-test is #6832 as a unit: before this guard the conversion
// element was skipped by the collector, so the roster it reported was a
// truthful-looking list that simply did not mention the smuggled entry.
func TestAllEntityKindsRosterElements6830_ReadsArbitraryElementShapes(t *testing.T) {
	collect := func(t *testing.T, body string) []rosterElement {
		t.Helper()
		return allEntityKindsRosterElements(t, []byte("package types\n\ntype EntityKind string\n\n"+body))
	}

	t.Run("named_constants_only", func(t *testing.T) {
		got := collect(t, `func AllEntityKinds() []EntityKind {
	return []EntityKind{
		EntityKindA,
		// a comment between elements must not become an element
		EntityKindB,
	}
}`)
		assertElements(t, got, []rosterElement{
			{Index: 0, Raw: "EntityKindA", Name: "EntityKindA", OK: true},
			{Index: 1, Raw: "EntityKindB", Name: "EntityKindB", OK: true},
		})
	})

	t.Run("conversion_literal_is_seen_not_skipped", func(t *testing.T) {
		got := collect(t, `func AllEntityKinds() []EntityKind {
	return []EntityKind{
		EntityKindA,
		EntityKind("SCOPE.Smuggled"),
	}
}`)
		assertElements(t, got, []rosterElement{
			{Index: 0, Raw: "EntityKindA", Name: "EntityKindA", OK: true},
			{Index: 1, Raw: `EntityKind("SCOPE.Smuggled")`, Literal: "SCOPE.Smuggled", OK: true},
		})
	})

	t.Run("unreadable_shape_is_refused_not_dropped", func(t *testing.T) {
		got := collect(t, `func AllEntityKinds() []EntityKind {
	return []EntityKind{
		EntityKindA,
		someHelper(),
		EntityKind(otherPackageConst),
	}
}`)
		if len(got) != 3 {
			t.Fatalf("collected %d elements, want 3 — an unreadable element was DROPPED, which is "+
				"the #6832 defect itself", len(got))
		}
		for _, i := range []int{1, 2} {
			if got[i].OK {
				t.Errorf("element %d (%s) was classified OK; the collector must refuse shapes it "+
					"cannot evaluate rather than inventing an answer", i, got[i].Raw)
			}
		}
	})
}

func assertElements(t *testing.T, got, want []rosterElement) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("elements = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("elements = %+v, want %+v", got, want)
		}
	}
}

// TestAllEntityKinds6830_ListsEveryDeclaredKindExactlyOnce is the guard #6830
// and #6832 ask for: the real tree, graded as a SLICE.
//
// It is not a restatement of TestEntityKindDeclarations6776_MatchAllEntityKindsExactly.
// That test compares NAME SETS parsed from one file; this one compares VALUES
// with MULTIPLICITY across the whole package, which is what makes a duplicate,
// a conversion literal, an aliased pair and a constant declared in a second
// file all visible. Those are four defects that the set comparison collapses
// and this one does not.
//
// There is no exemption list. An EntityKind that should not be emitted should
// not be declared; deleting the constant is the way to retire one.
func TestAllEntityKinds6830_ListsEveryDeclaredKindExactlyOnce(t *testing.T) {
	declared := declaredEntityKindsFromSource(t)

	src, err := os.ReadFile(filepath.Join(entityKindSourceDir, "kinds.go"))
	if err != nil {
		t.Fatalf("read kinds.go: %v", err)
	}
	elements := allEntityKindsRosterElements(t, src)

	problems := entityKindRosterProblems(declared, elements)
	if len(problems) == 0 {
		return
	}
	t.Errorf("AllEntityKinds() is not in exact correspondence with the EntityKind constants "+
		"internal/types declares — %d problem(s):\n  %s\n\n"+
		"AllEntityKinds() is a public, slice-returning API. Every consumer that emits one row per "+
		"element, builds a histogram, or drives a table test from it depends on this correspondence "+
		"(#6830, #6832).", len(problems), strings.Join(problems, "\n  "))
}
