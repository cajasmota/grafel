package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// POSITIVE CONTROLS.
//
// A scan-and-assert-absence tool has five ways to be a no-op: it did not read,
// it read the wrong files, it read the right files but not the content that
// matters, it read the content but did not detect, or it detected but did not
// act. A count floor (minResolvedSites, asserted in gate_test.go) catches only
// the first. These four controls cover the rest, and they are part of the suite
// rather than a one-off manual check.
//
// Every control asserts on the EMITTED DIAGNOSTIC — file, literal, package —
// not on a counter the tool keeps about itself. A diagnostic signature is not a
// regression pin, and a counter is not a diagnostic.
//
// Each control injects its defect through a go/packages OVERLAY, so the real
// extractor file on disk is never modified and the controls cannot leave the
// tree dirty on failure.

// modRoot resolves the module root from the test's working directory
// (tools/node-type-gate).
func modRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	root := filepath.Dir(filepath.Dir(wd))
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("module root %s has no go.mod: %v", root, err)
	}
	return root
}

// overlayReplace reads a real source file, asserts the old text occurs EXACTLY
// wantCount times, and returns the patched bytes. The exact-count assertion is
// the "prove the edit landed" rule: an edit that silently does not apply reads
// as a clean tree and argues the opposite of the truth.
func overlayReplace(t *testing.T, path, old, new string, wantCount int) []byte {
	t.Helper()
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	got := bytes.Count(src, []byte(old))
	if got != wantCount {
		t.Fatalf("overlay premise failed: %s contains %q %d times, want %d — the file moved under the control", path, old, got, wantCount)
	}
	return bytes.Replace(src, []byte(old), []byte(new), -1)
}

var (
	grammarsOnce sync.Once
	grammarsVal  map[string]*Grammar
	grammarsErr  error
)

func testGrammars(t *testing.T) map[string]*Grammar {
	t.Helper()
	grammarsOnce.Do(func() { grammarsVal, grammarsErr = loadGrammars() })
	if grammarsErr != nil {
		t.Fatalf("loadGrammars: %v", grammarsErr)
	}
	return grammarsVal
}

func emptyBaseline(t *testing.T) *Baseline {
	t.Helper()
	b, err := ParseBaseline(strings.NewReader(""))
	if err != nil {
		t.Fatalf("empty baseline: %v", err)
	}
	return b
}

func realBaseline(t *testing.T, root string) *Baseline {
	t.Helper()
	b, err := readBaseline(filepath.Join(root, "tools", "node-type-gate", "baseline.txt"))
	if err != nil {
		t.Fatalf("read baseline: %v", err)
	}
	return b
}

// evalPkg loads one package (with an optional overlay) and evaluates the gate
// over it.
func evalPkg(t *testing.T, root, pattern string, overlay map[string][]byte, base *Baseline) Result {
	t.Helper()
	var (
		surf *Surface
		err  error
	)
	if overlay == nil {
		surf, err = LoadSurface(root, []string{pattern})
	} else {
		surf, err = LoadSurfaceOverlay(root, []string{pattern}, overlay)
	}
	if err != nil {
		t.Fatalf("load %s: %v", pattern, err)
	}
	return Evaluate(surf.Scan, surf.Registrations, testGrammars(t), base)
}

// findDiag returns the emitted failure for (fileSuffix, literal), or nil.
func findDiag(res Result, fileSuffix, lit string) *Miss {
	for i := range res.Failures {
		m := res.Failures[i]
		if m.Lit == lit && strings.HasSuffix(m.File, fileSuffix) {
			return &res.Failures[i]
		}
	}
	return nil
}

func diagLines(res Result) string {
	var b strings.Builder
	for _, m := range res.Failures {
		fmt.Fprintf(&b, "  %s\n", m.String())
	}
	for _, e := range res.StaleBaseline {
		fmt.Fprintf(&b, "  stale: %s %q %s\n", e.Dir, e.Lit, e.File)
	}
	if b.Len() == 0 {
		b.WriteString("  (no diagnostics)\n")
	}
	// A "nothing was flagged" failure is ambiguous on its own: the literal may
	// have resolved, or it may never have been derived. Say which.
	byForm := map[string]int{}
	for _, s := range res.Sites {
		byForm[s.Form]++
	}
	fmt.Fprintf(&b, "  derived: %d sites (%v), %d resolved, %d sinks, grammars %v\n",
		len(res.Sites), byForm, res.Resolved, len(res.Sinks), res.GrammarsForDir)
	return b.String()
}

// sinksNaming lists the discovered helper parameter positions whose function
// name contains want. "the fixpoint never found the helper" and "the helper was
// found but the call site was not" are different bugs and must not look alike.
func sinksNaming(res Result, want string) []string {
	var out []string
	for _, s := range res.Sinks {
		if strings.Contains(s, want) {
			out = append(out, s)
		}
	}
	return out
}

// sitesFor lists every derived site for one literal, whatever its verdict.
func sitesFor(res Result, lit string) []Site {
	var out []Site
	for _, s := range res.Sites {
		if s.Lit == lit {
			out = append(out, s)
		}
	}
	return out
}

// CONTROL 1 — the scala defect that prompted #7065.
//
// #7064 shipped `case "variant_type_parameter", "type_parameter":` against
// tree-sitter-scala, which has neither node. The guard was dead for the
// language's dominant generic form and emitted a wrong edge that BOUND, with
// 26/26 bound reported. Re-introduce exactly that text and require the gate to
// name both literals, at the right file, in the right package.
//
// VARIED across the two asserted literals: the literal itself (one plausible
// invented name, one name that is real in OTHER grammars — "type_parameter"
// exists in java, kotlin, rust — so this row also grades that resolution is
// per-package and not against a union of all grammars).
// HELD CONSTANT: the file, the package, the form (switch), the defect shape
// (both arms of one case unmatchable).
func TestControl_ScalaVariantTypeParameterIsDetected(t *testing.T) {
	root := modRoot(t)
	file := filepath.Join(root, "internal", "extractors", "scala", "field_type_refs.go")
	patched := overlayReplace(t,
		file,
		`case "covariant_type_parameter", "contravariant_type_parameter":`,
		`case "variant_type_parameter", "type_parameter":`,
		1)

	base := evalPkg(t, root, "./internal/extractors/scala", nil, emptyBaseline(t))
	for _, lit := range []string{"variant_type_parameter", "type_parameter"} {
		if d := findDiag(base, "scala/field_type_refs.go", lit); d != nil {
			t.Fatalf("baseline run already reports %q — the control cannot show it was the overlay that caused it", lit)
		}
	}

	res := evalPkg(t, root, "./internal/extractors/scala",
		map[string][]byte{file: patched}, emptyBaseline(t))

	for _, lit := range []string{"variant_type_parameter", "type_parameter"} {
		d := findDiag(res, "scala/field_type_refs.go", lit)
		if d == nil {
			t.Fatalf("gate did not flag %q in scala/field_type_refs.go.\nemitted:\n%s", lit, diagLines(res))
		}
		if d.Dir != "internal/extractors/scala" {
			t.Errorf("%q attributed to package %q, want internal/extractors/scala", lit, d.Dir)
		}
		if d.Form != FormSwitch {
			t.Errorf("%q reported with form %q, want %q — grep cannot see this form, which is the point", lit, d.Form, FormSwitch)
		}
		if got := strings.Join(d.Grammars, ","); got != "scala" {
			t.Errorf("%q resolved against grammars %q, want scala", lit, got)
		}
	}
	// "type_parameter" is a real node in java, kotlin and rust. If the gate
	// resolved against a union of all grammars it would pass here, so assert
	// the premise that makes this row meaningful.
	g := testGrammars(t)
	if !g["java"].Kinds["type_parameter"] {
		t.Fatal("premise gone: type_parameter is no longer a java node, so this row no longer grades per-package resolution")
	}
	if g["scala"].Kinds["type_parameter"] {
		t.Fatal("premise gone: type_parameter IS now a scala node")
	}
}

// CONTROL 2 — #7068, the one dead literal with observable behaviour.
//
// csharp.go matches "for_each_statement"; tree-sitter-c-sharp spells it
// "foreach_statement", so collectLocalVarTypes never binds a foreach loop
// variable and every CALLS edge from such a body keeps a bare-name target.
//
// This control runs in BOTH directions, which is what separates a pin from a
// skip: with an empty baseline the gate must name the literal; with the real
// baseline it must be tolerated AND its row must say #7068; and once the
// literal is corrected the row must go stale, so the baseline cannot outlive
// the defect it describes.
//
// VARIED across the three sub-assertions: the baseline (empty / real) and the
// source (as-shipped / corrected).
// HELD CONSTANT: the file, the literal's position, the package, the form.
func TestControl_CSharpForEachStatementIsDetected(t *testing.T) {
	root := modRoot(t)
	const suffix = "csharp/csharp.go"

	// (a) Detected at all, with an empty baseline.
	res := evalPkg(t, root, "./internal/extractors/csharp", nil, emptyBaseline(t))
	d := findDiag(res, suffix, "for_each_statement")
	if d == nil {
		t.Fatalf("gate did not flag for_each_statement.\nderived sites for that literal: %v\nsinks naming findAllNodes: %v\nemitted:\n%s",
			sitesFor(res, "for_each_statement"), sinksNaming(res, "findAllNodes"), diagLines(res))
	}
	if d.Dir != "internal/extractors/csharp" {
		t.Errorf("attributed to %q, want internal/extractors/csharp", d.Dir)
	}
	if d.Form != FormHelper {
		t.Errorf("form %q, want %q: the literal is an argument to findAllNodes, which only the helper fixpoint reaches", d.Form, FormHelper)
	}
	if !strings.Contains(d.String(), "for_each_statement") || !strings.Contains(d.String(), "csharp.go") {
		t.Errorf("diagnostic does not name both the literal and the file: %s", d.String())
	}

	// (b) Tolerated by the real baseline, with a row that names the issue.
	real := realBaseline(t, root)
	res2 := evalPkg(t, root, "./internal/extractors/csharp", nil, real)
	if d := findDiag(res2, suffix, "for_each_statement"); d != nil {
		t.Errorf("baselined literal still fails the gate: %s", d.String())
	}
	var row *BaselineEntry
	for i := range real.Entries {
		if real.Entries[i].Lit == "for_each_statement" {
			row = &real.Entries[i]
		}
	}
	if row == nil {
		t.Fatal("baseline has no row for for_each_statement")
	}
	if row.Class != "known-defect" {
		t.Errorf("for_each_statement is classed %q, want known-defect — it is the one dead literal with observable behaviour", row.Class)
	}
	if !strings.Contains(row.Note, "7068") {
		t.Errorf("for_each_statement row does not name its issue: %q", row.Note)
	}

	// (c) Correct the literal and the baseline row must go stale. Without this
	// the baseline would silently outlive the fix.
	file := filepath.Join(root, "internal", "extractors", "csharp", "csharp.go")
	patched := overlayReplace(t, file, `"for_each_statement"`, `"foreach_statement"`, 1)
	fresh := realBaseline(t, root)
	res3 := evalPkg(t, root, "./internal/extractors/csharp",
		map[string][]byte{file: patched}, fresh)
	if findDiag(res3, suffix, "for_each_statement") != nil {
		t.Error("the corrected source still reports for_each_statement")
	}
	stale := false
	for _, e := range res3.StaleBaseline {
		if e.Lit == "for_each_statement" {
			stale = true
		}
	}
	if !stale {
		t.Errorf("fixing the literal did not make its baseline row stale.\nemitted:\n%s", diagLines(res3))
	}
	if !res3.Failed() {
		t.Error("a stale baseline row must fail the gate; that is the mechanism that makes the list shrink")
	}
}

// CONTROL 3 — the baseline suppresses ONLY what it lists.
//
// This is the control that matters most. A baseline that suppresses by package,
// by class, or by accident would make the gate useless while leaving every
// other control green. Inject a node name that exists in NO grammar into a file
// that ALREADY has baselined rows, and require that the new literal fails while
// the neighbouring baselined literals in the same file stay quiet.
//
// VARIED: whether the literal has a baseline row (the injected one does not;
// "case_class_definition" in the same file does).
// HELD CONSTANT: the file, the package, the grammar, the form (switch), and the
// baseline object.
func TestControl_NewDeadLiteralBypassesTheBaseline(t *testing.T) {
	root := modRoot(t)
	file := filepath.Join(root, "internal", "extractors", "scala", "scala.go")
	const injected = "grafel_no_such_node_7065"
	patched := overlayReplace(t,
		file,
		`	case "class_definition", "case_class_definition":`,
		`	case "`+injected+`":
		_ = 0
	case "class_definition", "case_class_definition":`,
		1)

	base := realBaseline(t, root)
	res := evalPkg(t, root, "./internal/extractors/scala",
		map[string][]byte{file: patched}, base)

	d := findDiag(res, "scala/scala.go", injected)
	if d == nil {
		t.Fatalf("a literal in NO grammar, absent from the baseline, did not fail the gate — the baseline is suppressing more than it lists.\nemitted:\n%s", diagLines(res))
	}
	if d.Dir != "internal/extractors/scala" {
		t.Errorf("attributed to %q, want internal/extractors/scala", d.Dir)
	}
	if !res.Failed() {
		t.Error("Failed() is false despite an un-baselined miss")
	}

	// The same file's baselined literals must remain tolerated: suppression is
	// per row, not per file.
	if d := findDiag(res, "scala/scala.go", "case_class_definition"); d != nil {
		t.Errorf("a baselined literal in the same file also failed: %s", d.String())
	}
	baselined := 0
	for _, m := range res.Misses {
		if m.Baselined {
			baselined++
		}
	}
	if baselined == 0 {
		t.Error("no miss was tolerated at all — this run does not actually exercise suppression, so the row above proves nothing")
	}
}

// CONTROL 5 — constant folding.
//
// An earlier AST scanner in this repo was silently zeroed by `const K =
// string(types.X)`: the literal moved behind a converted named constant and the
// site count dropped to 0 with no error (#7065's brief calls this out, and the
// same trap cost a previous change its whole premise). Reproduce that exact
// shape and require the gate to still see the name.
//
// VARIED against Control 3, which injects the same KIND of defect as a bare
// string literal: here the literal is reached only through a typed named
// constant produced by a conversion.
// HELD CONSTANT: the file, the package, the grammar, the form (switch), and the
// required verdict (the gate names the literal).
func TestControl_LiteralBehindAConvertedConstantIsStillSeen(t *testing.T) {
	root := modRoot(t)
	file := filepath.Join(root, "internal", "extractors", "scala", "scala.go")
	const injected = "grafel_no_such_node_const_7065"
	patched := overlayReplace(t,
		file,
		`	case "class_definition", "case_class_definition":`,
		`	case ntGateFoldedKind:
		_ = 0
	case "class_definition", "case_class_definition":`,
		1)
	patched = append(patched, []byte(`

type ntGateKind string

const ntGateFoldedKind = string(ntGateKind("`+injected+`"))
`)...)

	res := evalPkg(t, root, "./internal/extractors/scala",
		map[string][]byte{file: patched}, realBaseline(t, root))

	d := findDiag(res, "scala/scala.go", injected)
	if d == nil {
		t.Fatalf("a dead node name reached through `const K = string(T(\"…\"))` was not seen — constant folding is off, and a scan that cannot fold is silently blind.\nemitted:\n%s", diagLines(res))
	}
	if d.Form != FormSwitch {
		t.Errorf("form %q, want %q", d.Form, FormSwitch)
	}
	if d.Dir != "internal/extractors/scala" {
		t.Errorf("attributed to %q, want internal/extractors/scala", d.Dir)
	}
}

// CONTROL 4 — the false-positive direction.
//
// A literal valid in ONE of a multi-grammar package's grammars and absent from
// another must NOT fail. The audit counted 57 such rows (C++-only names in the
// shared c/cpp package, TS-only names in the shared js/ts/tsx package); a gate
// firing on them would be a third wrong by construction, and would be switched
// off within a week.
//
// VARIED: the package and its grammar set (cpp → {c,cpp}; javascript →
// {javascript,tsx,typescript}), and which member grammar the literal is missing
// from (cpp: absent from c; javascript: absent from plain javascript).
// HELD CONSTANT: the required verdict (no failure) and the premise style (the
// asymmetry is asserted from the symbol tables, not assumed).
func TestControl_LiteralPresentInOneGrammarOfAPackageDoesNotFail(t *testing.T) {
	g := testGrammars(t)
	cases := []struct {
		name      string
		pattern   string
		dir       string
		file      string
		lit       string
		presentIn string
		absentIn  string
	}{
		{"cpp-only name in the shared c/cpp package", "./internal/extractors/cpp",
			"internal/extractors/cpp", "cpp/extractor.go", "class_specifier", "cpp", "c"},
		{"ts-only name in the shared js/ts/tsx package", "./internal/extractors/javascript",
			"internal/extractors/javascript", "javascript/references.go", "type_identifier", "typescript", "javascript"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Premise: the asymmetry this row exists to exercise still holds.
			if !g[tc.presentIn].Kinds[tc.lit] {
				t.Fatalf("premise gone: %q is not a %s node any more", tc.lit, tc.presentIn)
			}
			if g[tc.absentIn].Kinds[tc.lit] {
				t.Fatalf("premise gone: %q IS now a %s node, so this row no longer varies anything", tc.lit, tc.absentIn)
			}

			root := modRoot(t)
			res := evalPkg(t, root, tc.pattern, nil, emptyBaseline(t))

			// Premise: the literal is actually in the derived surface. Without
			// this the "no failure" verdict could come from never having seen it.
			seen := false
			for _, s := range res.Sites {
				if s.Lit == tc.lit && s.Dir == tc.dir {
					seen = true
					break
				}
			}
			if !seen {
				t.Fatalf("%q was never derived from %s — the no-failure verdict below would be vacuous", tc.lit, tc.dir)
			}
			// Premise: the package really is multi-grammar here.
			if len(res.GrammarsForDir[tc.dir]) < 2 {
				t.Fatalf("%s resolved against %v — not a multi-grammar package, so this row grades nothing", tc.dir, res.GrammarsForDir[tc.dir])
			}

			if d := findDiag(res, tc.file, tc.lit); d != nil {
				t.Errorf("gate fired on a literal that is valid in %s: %s", tc.presentIn, d.String())
			}
			for _, m := range res.Misses {
				if m.Lit == tc.lit {
					t.Errorf("%q recorded as a miss at all: %s", tc.lit, m.String())
				}
			}
		})
	}
}
