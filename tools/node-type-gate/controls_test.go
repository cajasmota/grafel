package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
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
	return Evaluate(surf.Scan, surf.Registrations, surf.ParseBindings, testGrammars(t), base)
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
	files := map[string]bool{}
	for _, s := range res.Sites {
		files[s.File] = true
	}
	var fs []string
	for f := range files {
		fs = append(fs, f)
	}
	sort.Strings(fs)
	fmt.Fprintf(&b, "  derived: %d sites (%v), %d dynamic, %d resolved, %d sinks, grammars %v\n",
		len(res.Sites), byForm, len(res.Dynamic), res.Resolved, len(res.Sinks), res.GrammarsForDir)
	fmt.Fprintf(&b, "  files: %v\n", fs)
	for _, s := range res.Dynamic {
		fmt.Fprintf(&b, "  dynamic: %s:%d form=%s\n", s.File, s.Line, s.Form)
	}
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

// CONTROL 2 — #7068, the dead literal that had observable behaviour.
//
// csharp.go used to match "for_each_statement"; tree-sitter-c-sharp spells it
// "foreach_statement", so collectLocalVarTypes never bound a foreach loop
// variable and every CALLS edge out of such a body kept a bare-name target.
// #7070 fixed it on main while this gate was being built — and the gate caught
// that itself: its baseline row for the literal stopped matching and turned the
// run red with "the literal was fixed or moved, delete the row". The row is
// gone.
//
// So this control is now a REGRESSION PIN rather than a detection demo, which
// is strictly the stronger thing: put the wrong spelling back and require the
// gate to name it. If anyone re-introduces #7068 — a revert, a bad merge, or
// the same mistake in a new arm — this fails.
//
// It also asserts the corrected spelling resolves cleanly. Without that half,
// the control would pass just as happily against a gate that fires on
// everything.
//
// VARIED across the two halves: the spelling in the source (as-shipped /
// wrong), which flips the required verdict.
// HELD CONSTANT: the file, the package, the call, the form (helper), and the
// baseline — the real one, because the pin must hold against the file that
// ships.
func TestControl_CSharpForEachStatementRegressionIsCaught(t *testing.T) {
	root := modRoot(t)
	const suffix = "csharp/csharp.go"
	file := filepath.Join(root, "internal", "extractors", "csharp", "csharp.go")

	// The shipped spelling resolves: no miss, no failure.
	clean := evalPkg(t, root, "./internal/extractors/csharp", nil, realBaseline(t, root))
	for _, m := range clean.Misses {
		if strings.Contains(m.Lit, "each_statement") {
			t.Errorf("the shipped tree already reports %q: %s", m.Lit, m.String())
		}
	}
	if len(sitesFor(clean, "foreach_statement")) == 0 {
		t.Fatalf("the corrected literal was never derived, so the pin below would be vacuous.\n%s", diagLines(clean))
	}

	// Re-introduce #7068 exactly: the loop-variable lookup, and only it.
	patched := overlayReplace(t,
		file,
		`for _, fr := range findAllNodes(body, "foreach_statement") {`,
		`for _, fr := range findAllNodes(body, "for_each_statement") {`,
		1)
	res := evalPkg(t, root, "./internal/extractors/csharp",
		map[string][]byte{file: patched}, realBaseline(t, root))

	d := findDiag(res, suffix, "for_each_statement")
	if d == nil {
		t.Fatalf("re-introducing #7068 did not fail the gate.\nderived sites for that literal: %v\nsinks naming findAllNodes: %v\nemitted:\n%s",
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
	if !res.Failed() {
		t.Error("Failed() is false despite the re-introduced defect")
	}
}

// TestControl_StaleBaselineRowFailsTheGate is the other half Control 2 used to
// carry, kept as its own control now that #7068 is fixed: a baseline row whose
// literal no longer exists must turn the gate red, so the list shrinks when a
// defect is fixed instead of quietly outliving it.
//
// This is not hypothetical. It is exactly what happened when #7070 landed
// mid-build: the row for for_each_statement stopped matching and the gate said
// so, unprompted.
//
// VARIED: whether the baseline carries a row nothing in the tree hits.
// HELD CONSTANT: the package, the source (unmodified), and everything else
// about the run.
func TestControl_StaleBaselineRowFailsTheGate(t *testing.T) {
	root := modRoot(t)
	withGhost, err := ParseBaseline(strings.NewReader(
		"not-a-node-type | internal/extractors/csharp | grafel_ghost_row_7065 | internal/extractors/csharp/csharp.go:1 | a row nothing matches\n"))
	if err != nil {
		t.Fatalf("ParseBaseline: %v", err)
	}
	res := evalPkg(t, root, "./internal/extractors/csharp", nil, withGhost)
	if len(res.StaleBaseline) != 1 || res.StaleBaseline[0].Lit != "grafel_ghost_row_7065" {
		t.Fatalf("a baseline row matching nothing was not reported stale: %v", res.StaleBaseline)
	}
	// There is deliberately NO res.Failed() assertion here (#7086). This run
	// drives csharp against a baseline holding only the ghost row, so every
	// literal the real baseline tolerates is un-baselined and Failures is
	// non-empty too: Failed() is already true for a reason that has nothing to
	// do with the stale row, and the assertion stayed green under a mutant that
	// deleted the StaleBaseline arm outright. A whole-output check that survives
	// deleting what it claims to check reads as a second, independent
	// confirmation while contributing nothing. The observation that carries this
	// control is res.StaleBaseline above — that is what kills the mutant — and
	// the arm's contribution to the emitted verdict is graded, arm by arm and
	// with the other two nil, by
	// TestPrintVerdict_EachFailedArmAloneFlipsTheVerdictLine.

	// The REMEDIATION TEXT is part of the behaviour, not decoration. A rename
	// produces both error kinds at once — the #7076 review measured 13
	// new-dead-literal errors plus 10 stale rows from renaming one lua file —
	// and the old text said only "delete the row", which fixes neither half and
	// silently loses the baseline coverage. Assert on what is actually emitted;
	// prose no test observes is how the wrong advice survived review once.
	var out bytes.Buffer
	printVerdict(&out, res)
	text := out.String()
	for _, want := range []string{
		// The ghost literal itself: this ties the emitted diagnostic to THIS
		// row rather than to "a stale row was reported somewhere".
		`"grafel_ghost_row_7065"`,
		"RENAMED",
		"rewrite this row's dir/path",
		"-update does NOT rewrite paths",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("the stale-row diagnostic does not mention %q; a rename would be misdirected.\ngot:\n%s", want, text)
		}
	}

	// Negative control: every row of the checked-in baseline that names this
	// package does match. Without it, an Unmatched() that returns everything
	// would satisfy the assertion above.
	real := evalPkg(t, root, "./internal/extractors/csharp", nil, realBaseline(t, root))
	for _, e := range real.StaleBaseline {
		if e.Dir == "internal/extractors/csharp" {
			t.Errorf("the checked-in baseline has a stale csharp row: %s %q in %s", e.Dir, e.Lit, e.File)
		}
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

// CONTROL 6 — the custom lane is checked, not silently skipped.
//
// The #7076 review's first finding: internal/custom/kotlin holds 11 kotlin
// node-type literals, got no grammar key (it registers under the synthetic key
// "custom_kotlin_ktor_routes" and parses for itself), and was skipped in
// SILENCE. A brand-new dead literal there produced failures=0, ExitCode=0,
// under a comment asserting such packages "never receive a tree-sitter tree" —
// which was false for it. That is the gate's own version of the defect it
// exists to catch.
//
// The mapping is now DERIVED from the package's own parse call
// (`factory.Parse(ctx, content, "kotlin")`), not hand-listed, so a new custom
// package that parses with a constant language is covered the day it lands.
//
// VARIED against Control 3, which injects the same kind of defect into a
// package mapped through extractor.Register: this one is mapped through a parse
// call instead, which is the derivation that did not exist before.
// HELD CONSTANT: the injected literal's shape (cmp), the real baseline, and the
// required verdict.
func TestControl_CustomLanePackageIsResolved(t *testing.T) {
	root := modRoot(t)
	const dir = "internal/custom/kotlin"
	file := filepath.Join(root, "internal", "custom", "kotlin", "ktor_routes.go")
	const injected = "grafel_no_such_node_custom_7065"

	// Premise: the package really is mapped now, and mapped to kotlin.
	clean := evalPkg(t, root, "./internal/custom/kotlin", nil, realBaseline(t, root))
	keys := clean.GrammarsForDir[dir]
	if len(keys) != 1 || keys[0] != "kotlin" {
		t.Fatalf("%s resolves against %v, want [kotlin] — derived from its factory.Parse(…, \"kotlin\") call", dir, keys)
	}
	// Premise: it really does contribute sites, so the verdict is not vacuous.
	if n := len(sitesFor(clean, "call_expression")); n == 0 {
		t.Fatalf("no sites derived from %s; the control below would grade nothing.\n%s", dir, diagLines(clean))
	}
	// Premise: it is not reported as skipped.
	for _, sk := range clean.Skipped {
		if sk.Dir == dir {
			t.Fatalf("%s is still being skipped (%d sites, reason %q)", dir, sk.Sites, sk.Reason)
		}
	}
	// Scoped to this package: a single-package load cannot match the baseline's
	// rows for other packages, so StaleBaseline is noise here, and only failures
	// attributed to this dir are meaningful.
	for _, m := range clean.Failures {
		if m.Dir == dir {
			t.Errorf("the shipped custom lane is not clean: %s", m.String())
		}
	}

	// The reviewer's N2: a new dead literal here must now fail.
	patched := overlayReplace(t,
		file,
		`if node.Type() == "call_expression" {`,
		`if node.Type() == "`+injected+`" {`,
		1)
	res := evalPkg(t, root, "./internal/custom/kotlin",
		map[string][]byte{file: patched}, realBaseline(t, root))

	d := findDiag(res, "custom/kotlin/ktor_routes.go", injected)
	if d == nil {
		t.Fatalf("a dead literal in the custom lane did not fail the gate — it is being skipped again.\nskipped: %+v\nemitted:\n%s", res.Skipped, diagLines(res))
	}
	if d.Dir != dir {
		t.Errorf("attributed to %q, want %q", d.Dir, dir)
	}
	if got := strings.Join(d.Grammars, ","); got != "kotlin" {
		t.Errorf("resolved against %q, want kotlin", got)
	}
	if !res.Failed() {
		t.Error("Failed() is false despite an un-baselined miss in the custom lane")
	}
}

// CONTROL 7 — a package that cannot be mapped must be NAMED, and an unnamed one
// must fail.
//
// This is the half of finding 1 that outlives the kotlin mapping. Two packages
// are skipped today: internal/engine parses under a runtime-computed language
// and genuinely has no single grammar, and internal/treesitter IS the parser —
// its kotlin surface is bound by an `if language == "kotlin"` guard the
// derivation does not model, which is a special case not worth one, not an
// impossibility. Skipping either is a reviewed decision only while the list is
// closed and each entry carries a reason that is true of it. A directory that produces sites and appears in neither the grammar
// map nor skipExemptions must turn the gate red, or the silence comes back.
//
// Driven on a synthetic surface so each axis moves alone.
//
// VARIED, one per row: whether the directory has a grammar, and whether it has
// an exemption. Row "mapped" is the negative control — without it, a gate that
// failed on every directory would pass every other row.
// HELD CONSTANT: the literal (valid in the grammar, so it can never be a miss),
// the file, the line, the form, and the empty baseline — so the only thing that
// can make a row red is the skip accounting.
func TestEvaluate_SkippedPackagesMustBeNamed(t *testing.T) {
	grammars := map[string]*Grammar{
		"alpha": {Key: "alpha", Kinds: map[string]bool{"real_alpha": true}},
	}
	exemptDir := ""
	for d := range skipExemptions {
		exemptDir = d
		break
	}
	if exemptDir == "" {
		t.Fatal("skipExemptions is empty; this test would grade nothing")
	}
	rows := []struct {
		name        string
		dir         string
		regs        []Registration
		wantFail    bool
		wantSkipped bool
		wantUnnamed bool
	}{
		{"mapped to a grammar", "pkg/mapped", []Registration{{Dir: "pkg/mapped", Key: "alpha"}}, false, false, false},
		{"unmapped and exempt", exemptDir, nil, false, true, false},
		{"unmapped and NOT exempt", "internal/custom/brandnew", nil, true, true, true},
	}
	for _, r := range rows {
		t.Run(r.name, func(t *testing.T) {
			site := Site{Pkg: r.dir, Dir: r.dir, File: r.dir + "/x.go", Line: 3, Lit: "real_alpha", Form: FormCmp, Const: true}
			base, err := ParseBaseline(strings.NewReader(""))
			if err != nil {
				t.Fatalf("baseline: %v", err)
			}
			res := Evaluate(Scan{Sites: []Site{site}}, r.regs, nil, grammars, base)

			if got := res.Failed(); got != r.wantFail {
				t.Errorf("Failed() = %v, want %v (unreviewed: %+v)", got, r.wantFail, res.UnreviewedSkips)
			}
			if got := len(res.Skipped) > 0; got != r.wantSkipped {
				t.Errorf("reported as skipped = %v, want %v", got, r.wantSkipped)
			}
			if got := len(res.UnreviewedSkips) > 0; got != r.wantUnnamed {
				t.Errorf("reported as an unreviewed skip = %v, want %v", got, r.wantUnnamed)
			}
			if r.wantSkipped {
				if len(res.Skipped) != 1 || res.Skipped[0].Dir != r.dir {
					t.Fatalf("skipped = %+v, want exactly %s", res.Skipped, r.dir)
				}
				if res.SkippedSites != 1 {
					t.Errorf("SkippedSites = %d, want 1 — the unchecked surface must be counted, not just listed", res.SkippedSites)
				}
				if res.Skipped[0].Exempt() != !r.wantUnnamed {
					t.Errorf("Exempt() = %v, want %v", res.Skipped[0].Exempt(), !r.wantUnnamed)
				}
			}
			// A skipped site is never counted as resolved: the gate must not
			// claim credit for a surface it did not look at.
			if r.wantSkipped && res.Resolved != 0 {
				t.Errorf("Resolved = %d for a skipped package, want 0", res.Resolved)
			}
		})
	}
}

// TestSkipExemptionsOnDisk_AreAllStillNeeded keeps the exemption list closed in
// the other direction: a row for a package that is now mapped, or that no
// longer produces sites, is dead prose and must be deleted. Without this the
// list only ever grows, which is how the silence comes back.
func TestSkipExemptionsOnDisk_AreAllStillNeeded(t *testing.T) {
	root := modRoot(t)
	surf, err := LoadSurface(root, scanPatterns)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	grammars := testGrammars(t)
	base, err := ParseBaseline(strings.NewReader(""))
	if err != nil {
		t.Fatalf("baseline: %v", err)
	}
	res := Evaluate(surf.Scan, surf.Registrations, surf.ParseBindings, grammars, base)

	used := map[string]bool{}
	for _, sk := range res.Skipped {
		used[sk.Dir] = true
	}
	for dir, reason := range skipExemptions {
		if !used[dir] {
			t.Errorf("skipExemptions has a row for %s, but that directory is not skipped any more (it is mapped, or produces no sites). Delete the row; its reason %q is now dead prose.", dir, reason)
		}
		if reason == "" {
			t.Errorf("skipExemptions[%s] has no reason", dir)
		}
	}
	// And the live tree must have no unreviewed skip.
	if len(res.UnreviewedSkips) > 0 {
		t.Errorf("unreviewed skips on the real tree: %+v", res.UnreviewedSkips)
	}
	t.Logf("skipped surface: %d sites across %v", res.SkippedSites, used)
}
