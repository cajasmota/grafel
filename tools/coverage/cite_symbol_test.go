package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// citeSymbolFixtureSource is the single synthetic Go file every unit
// fixture below cites. It is written into a temp repo root so the tests
// pin the checker's behaviour rather than the current line numbers of
// any real engine file — a fixture citing a live source file would go
// red the next time that file is edited, which is the exact failure
// mode #6673 exists to remove.
//
// Line numbers are load-bearing here, so they are asserted by
// TestCiteSymbol_FixtureLineNumbersAreWhatTheTestsClaim before any
// other test relies on them.
const citeSymbolFixtureSource = `package fixture

// doc line 3
// doc line 4
func TopLevelFunc() {}

type Recv struct{}

// Method is declared TWICE in this file, on two receivers, so the
// multi-declaration branch and the sorted line list it formats are
// exercised rather than assumed.
func (r *Recv) Method() {}

type Other struct{}

// Method on the second receiver.
func (o *Other) Method() {}

var (
	// doc for BlockVar
	BlockVar = 1
	OtherVar = 2
)

const (
	BlockConst = "k"
)

type BlockType struct{}

var Single = 3
`

// citeSymbolFixtureLines records where each symbol is declared in
// citeSymbolFixtureSource.
var citeSymbolFixtureLines = map[string][]int{
	"TopLevelFunc": {5},
	"Method":       {12, 17},
	"BlockVar":     {21},
	"OtherVar":     {22},
	"BlockConst":   {26},
	"BlockType":    {29},
	"Single":       {31},
}

// citeSymbolFixtureEnd records the last line of each declaration —
// the upper bound of the range half of the convention. Stated
// explicitly for the same reason as the doc-start table: the rule is
// anchored on it, so a test must not infer it.
var citeSymbolFixtureEnd = map[string][]int{
	"TopLevelFunc": {5},
	"Method":       {12, 17},
	"BlockVar":     {21},
	"OtherVar":     {22},
	"BlockConst":   {26},
	"BlockType":    {29},
	"Single":       {31},
}

// citeSymbolFixtureDocStart records where each declaration's doc
// comment opens. The range half of the convention is anchored on this,
// so it is data the tests must state explicitly rather than infer.
var citeSymbolFixtureDocStart = map[string][]int{
	"TopLevelFunc": {3},
	"Method":       {9, 16},
	"BlockVar":     {20},
	"OtherVar":     {22},
	"BlockConst":   {26},
	"BlockType":    {29},
	"Single":       {31},
}

// citeSymbolRepo writes the fixture source into a temp repo root and
// returns the root plus the repo-relative path of the file.
func citeSymbolRepo(t *testing.T) (root, rel string) {
	t.Helper()
	root = t.TempDir()
	rel = filepath.Join("internal", "fixture", "sample.go")
	if err := os.MkdirAll(filepath.Join(root, "internal", "fixture"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, rel), []byte(citeSymbolFixtureSource), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	// A _test.go file with the SAME layout. The registry genuinely
	// cites two test functions, and a checker that skipped _test.go
	// would validate neither — so the test-file path is exercised
	// explicitly rather than assumed to fall out of the .go suffix.
	testRel := filepath.Join("internal", "fixture", "sample_test.go")
	if err := os.WriteFile(filepath.Join(root, testRel), []byte(citeSymbolFixtureSource), 0o644); err != nil {
		t.Fatalf("write test fixture: %v", err)
	}
	return root, filepath.ToSlash(rel)
}

// citeSymbolTestFileRel is the repo-relative path of the _test.go
// fixture written alongside the ordinary one.
const citeSymbolTestFileRel = "internal/fixture/sample_test.go"

// runCiteSymbols validates one notes string and returns the errors.
func runCiteSymbols(t *testing.T, root, notes string) []string {
	t.Helper()
	res := &ValidationResult{}
	validateCiteSymbols(res, "fixture.cell", Capability{Status: StatusFull, Notes: notes}, newDeclIndex(root))
	return res.Errors
}

// TestCiteSymbol_FixtureLineNumbersAreWhatTheTestsClaim is the positive
// control for the whole file: if the fixture source is edited and the
// recorded line numbers are not updated with it, every other test in
// this file becomes vacuous, so this one fails first and says why.
func TestCiteSymbol_FixtureLineNumbersAreWhatTheTestsClaim(t *testing.T) {
	root, rel := citeSymbolRepo(t)
	idx := newDeclIndex(root)
	for sym, want := range citeSymbolFixtureLines {
		got, ok := idx.declSitesForSymbol(rel, sym)
		if !ok {
			t.Fatalf("fixture source did not parse")
		}
		if len(got) != len(want) {
			t.Errorf("symbol %q: fixture declares it %d time(s) at %v, test claims %v — update citeSymbolFixtureLines", sym, len(got), got, want)
			continue
		}
		wantDoc := citeSymbolFixtureDocStart[sym]
		for i, st := range got {
			if st.Line != want[i] {
				t.Errorf("symbol %q decl %d: fixture declares it at %d, test claims %d — update citeSymbolFixtureLines", sym, i, st.Line, want[i])
			}
			if st.DocStart != wantDoc[i] {
				t.Errorf("symbol %q decl %d: doc opens at %d, test claims %d — update citeSymbolFixtureDocStart", sym, i, st.DocStart, wantDoc[i])
			}
			if wantEnd := citeSymbolFixtureEnd[sym]; st.End != wantEnd[i] {
				t.Errorf("symbol %q decl %d: declaration ends at %d, test claims %d — update citeSymbolFixtureEnd", sym, i, st.End, wantEnd[i])
			}
		}
	}
}

// TestCiteSymbol_FixtureFileLengthIsWhatTheTestsClaim is the positive
// control for the EOF bound: the past-EOF fixtures below cite a line
// beyond the fixture's length, and that only means anything if the
// length is what the tests assume.
func TestCiteSymbol_FixtureFileLengthIsWhatTheTestsClaim(t *testing.T) {
	root, rel := citeSymbolRepo(t)
	got, ok := newDeclIndex(root).fileLineCount(rel)
	if !ok {
		t.Fatalf("fixture source did not parse")
	}
	if got != citeSymbolFixtureLineCount {
		t.Errorf("fixture is %d lines, test claims %d — update citeSymbolFixtureLineCount", got, citeSymbolFixtureLineCount)
	}
}

// citeSymbolFixtureLineCount is the length of citeSymbolFixtureSource.
const citeSymbolFixtureLineCount = 31

// TestCiteSymbol_AnchoredCitationsAcceptedAcrossFormsAndDeclKinds
// varies the anchor punctuation form, the single-line vs range shape,
// and the declaration kind (func, method, var-block member, const-block
// member, type, standalone var) — the shapes the registry actually
// uses. All must validate clean.
func TestCiteSymbol_AnchoredCitationsAcceptedAcrossFormsAndDeclKinds(t *testing.T) {
	root, rel := citeSymbolRepo(t)

	cases := []struct {
		name  string
		notes string
	}{
		// Form 1: backtick + space + parenthesised citation.
		{"func/single/backtick-paren", fmt.Sprintf("minted by `TopLevelFunc` (%s:5).", rel)},
		// Form 2: parenthesised "(`Symbol`, path:N)".
		{"func/single/paren-comma", fmt.Sprintf("minted by the pass (`TopLevelFunc`, %s:5).", rel)},
		// Range opening on the first doc-comment line (3), closing past
		// the declaration (5) — convention rule (2).
		{"func/range/doc-comment-open", fmt.Sprintf("matched by `TopLevelFunc` (%s:3-5).", rel)},
		// A range on a declaration that has NO doc comment degenerates
		// to the declaration line at both ends. This is accepted
		// because DocStart == Line there, NOT because opening on a
		// declaration line is generally allowed — see
		// range-opens-on-declaration-skipping-doc-comment, which
		// rejects exactly that when a doc comment exists.
		{"no-doc-comment/range", fmt.Sprintf("`OtherVar` (%s:22-22) is the second one.", rel)},
		// A range closing EXACTLY on the last line of the declaration
		// is the boundary case of the upper bound, and must be accepted
		// — five shipped citations sit exactly here.
		{"range/closes-on-declaration-end", fmt.Sprintf("`BlockVar` (%s:20-21) is the pattern.", rel)},
		{"method/single", fmt.Sprintf("the receiver helper `Method` (%s:12) does it.", rel)},
		// The SECOND declaration of a name that is declared twice: the
		// check must consider every declaration, not just the first.
		{"method/second-declaration/single", fmt.Sprintf("the other receiver's `Method` (%s:17) does it too.", rel)},
		{"method/second-declaration/range", fmt.Sprintf("the other receiver's `Method` (%s:16-17) does it too.", rel)},
		{"var-block/single", fmt.Sprintf("`BlockVar` (%s:21) is the pattern.", rel)},
		{"var-block/range", fmt.Sprintf("`BlockVar` (%s:20-21) is the pattern.", rel)},
		{"const-block/single", fmt.Sprintf("`BlockConst` (%s:26) names the edge kind.", rel)},
		{"type/single", fmt.Sprintf("`BlockType` (%s:29) carries the fields.", rel)},
		{"standalone-var/single", fmt.Sprintf("`Single` (%s:31) is set once.", rel)},
		// Two citations in one note, one at the head and one at the tail.
		{"two-citations-one-note", fmt.Sprintf("`TopLevelFunc` (%s:5) calls into `Method` (%s:12).", rel, rel)},
		// A _test.go citation is checked like any other.
		{"test-file/single", fmt.Sprintf("pinned by `TopLevelFunc` (%s:5).", citeSymbolTestFileRel)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if errs := runCiteSymbols(t, root, tc.notes); len(errs) != 0 {
				t.Errorf("expected clean, got %v", errs)
			}
		})
	}
}

// TestCiteSymbol_RejectsEveryDefectClass varies the failure mode while
// holding the note shape constant, so each subtest isolates one rule.
func TestCiteSymbol_RejectsEveryDefectClass(t *testing.T) {
	root, rel := citeSymbolRepo(t)

	cases := []struct {
		name  string
		notes string
		want  string
	}{
		{
			// The dominant class: the number drifted onto another
			// symbol's declaration. A line-exists check cannot see this.
			name:  "stale-single-line",
			notes: fmt.Sprintf("`TopLevelFunc` (%s:12).", rel),
			want:  "is stale",
		},
		{
			// Range drift far enough that the declaration falls outside.
			name:  "stale-range",
			notes: fmt.Sprintf("`TopLevelFunc` (%s:21-26).", rel),
			want:  "is stale",
		},
		{
			// The declaration is inside the range, but the range opens
			// BELOW the doc comment. Without this bound a range has no
			// width limit at all.
			name:  "range-opens-below-doc-comment",
			notes: fmt.Sprintf("`TopLevelFunc` (%s:4-6).", rel),
			want:  "a range citation opens on the first doc-comment line",
		},
		{
			// A range opening at the top of the file. Named for what
			// it pins: the OPENING bound, not width — width is pinned
			// by the closing-bound cases below. Kept inside the file so
			// it cannot be rescued by the EOF rule.
			name:  "range-opens-at-file-start",
			notes: fmt.Sprintf("`TopLevelFunc` (%s:1-31).", rel),
			want:  "a range citation opens on the first doc-comment line",
		},
		{
			// N2. A range opening on the DECLARATION line, skipping the
			// doc comment, must be rejected — this is precisely the rot
			// two of the five registry corrections fixed
			// (`slsFunction` 24-28 for a doc opening at 23,
			// `parseProviderBlock` 128-152 for a doc opening at 125).
			// Without this case, relaxing the rule to
			// `lo == DocStart || lo == Line` would go unnoticed and the
			// suite would not pin the defect the rule exists for.
			name:  "range-opens-on-declaration-skipping-doc-comment",
			notes: fmt.Sprintf("`TopLevelFunc` (%s:5-5).", rel),
			want:  "a range citation opens on the first doc-comment line",
		},
		{
			// Same shape on a declaration whose doc comment is further
			// away, so the case is not passing on a one-line accident.
			name:  "range-opens-on-declaration-skipping-multiline-doc",
			notes: fmt.Sprintf("`Method` (%s:12-12).", rel),
			want:  "a range citation opens on the first doc-comment line",
		},
		{
			// The `-1` doc-comment rot #6671 corrected six of: a
			// SINGLE-LINE citation one line above the declaration.
			name:  "off-by-one-above-declaration",
			notes: fmt.Sprintf("`TopLevelFunc` (%s:4).", rel),
			want:  "is stale",
		},
		{
			name:  "symbol-not-in-file",
			notes: fmt.Sprintf("`NotDeclaredAnywhere` (%s:5).", rel),
			want:  "is not declared at package level",
		},
		{
			// Declared twice, cited at neither: the error must name
			// EVERY declaration line, sorted, not just the first.
			name:  "stale-against-all-declarations",
			notes: fmt.Sprintf("`Method` (%s:5).", rel),
			want:  "is declared at internal/fixture/sample.go:12,17",
		},
		{
			// A stale citation into a _test.go file must be reported,
			// not skipped for being a test file.
			name:  "stale-in-test-file",
			notes: fmt.Sprintf("`TopLevelFunc` (%s:12).", citeSymbolTestFileRel),
			want:  "is stale",
		},
		{
			// The upper bound. The range opens on the correct
			// doc-comment line, so the lower bound is satisfied — only
			// the closing end is wrong. Without this rule a citation
			// can claim an arbitrary span by entering through the top.
			name:  "range-closes-past-declaration-body",
			notes: fmt.Sprintf("`TopLevelFunc` (%s:3-20).", rel),
			want:  "a range citation closes on the declaration or in its body",
		},
		{
			// Width, pinned as a RANGE and nothing else: opens on the
			// correct doc-comment line, closes inside the file, and is
			// still 28 lines wide for a one-line declaration. This is
			// the shape the coordinator found surviving in the shipped
			// registry as `cdk_edges.go:137-900`, reduced so the EOF
			// rule cannot claim the kill.
			name:  "range-opens-correctly-but-is-absurdly-wide",
			notes: fmt.Sprintf("`TopLevelFunc` (%s:3-31).", rel),
			want:  "a range citation closes on the declaration or in its body",
		},
		{
			// Same defect, one line past the end: the bound is exact,
			// not a tolerance.
			name:  "range-closes-one-line-past-body",
			notes: fmt.Sprintf("`TopLevelFunc` (%s:3-6).", rel),
			want:  "a range citation closes on the declaration or in its body",
		},
		{
			// Running past EOF is its own defect with its own message,
			// because it is wrong independently of the symbol named.
			name:  "range-runs-past-eof",
			notes: fmt.Sprintf("`TopLevelFunc` (%s:3-900).", rel),
			want:  "runs past the end of internal/fixture/sample.go, which has 31 lines",
		},
		{
			// A single-line citation past EOF is caught by the same
			// rule — the bound is on the citation, not on the range
			// form.
			name:  "single-line-past-eof",
			notes: fmt.Sprintf("`TopLevelFunc` (%s:900).", rel),
			want:  "runs past the end of internal/fixture/sample.go, which has 31 lines",
		},
		{
			// Upper bound applied per citation, not per note: a note
			// whose FIRST citation is impeccable must not launder an
			// over-wide second one.
			name:  "one-good-range-one-overwide-in-the-same-note",
			notes: fmt.Sprintf("`BlockVar` (%s:20-21) is set by `TopLevelFunc` (%s:3-20).", rel, rel),
			want:  "a range citation closes on the declaration or in its body",
		},
		{
			// Same shape, over-wide citation FIRST.
			name:  "one-overwide-range-one-good-in-the-same-note",
			notes: fmt.Sprintf("`TopLevelFunc` (%s:3-20) sets `BlockVar` (%s:20-21).", rel, rel),
			want:  "a range citation closes on the declaration or in its body",
		},
		{
			// A number with no symbol in front of it is unverifiable
			// prose by construction — this is the rule that stops the
			// population from growing back.
			name:  "unanchored",
			notes: fmt.Sprintf("the dispatch happens at %s:5.", rel),
			want:  "is not symbol-anchored",
		},
		{
			// THE regression that matters: one anchored citation must
			// not launder every other bare number in the same note.
			// Anchoring is per citation, not per note.
			name:  "one-anchored-one-bare-in-the-same-note",
			notes: fmt.Sprintf("`TopLevelFunc` (%s:5) is dispatched at %s:999.", rel, rel),
			want:  "is not symbol-anchored",
		},
		{
			// Same shape, bare citation FIRST, so the check cannot pass
			// by only ever inspecting the tail of the note.
			name:  "one-bare-one-anchored-in-the-same-note",
			notes: fmt.Sprintf("dispatched at %s:999, minted by `TopLevelFunc` (%s:5).", rel, rel),
			want:  "is not symbol-anchored",
		},
		{
			// `extractor.go` alone matches 50 files in this tree.
			name:  "bare-basename",
			notes: "`TopLevelFunc` (sample.go:5).",
			want:  "uses a bare basename",
		},
		{
			name:  "file-missing",
			notes: "`TopLevelFunc` (internal/fixture/gone.go:5).",
			want:  "not found on disk",
		},
		{
			name:  "inverted-range",
			notes: fmt.Sprintf("`TopLevelFunc` (%s:9-5).", rel),
			want:  "inverted line range",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			errs := runCiteSymbols(t, root, tc.notes)
			if len(errs) == 0 {
				t.Fatalf("expected an error containing %q, got none", tc.want)
			}
			joined := strings.Join(errs, "\n")
			if !strings.Contains(joined, tc.want) {
				t.Errorf("expected an error containing %q, got:\n%s", tc.want, joined)
			}
		})
	}
}

// TestCiteSymbol_OutOfPopulationRefsAreLeftAlone pins the population
// boundary stated in cite_symbol.go. These are NOT accidental passes:
// each is a form the registry genuinely carries and which the checker
// deliberately does not own.
func TestCiteSymbol_OutOfPopulationRefsAreLeftAlone(t *testing.T) {
	root, rel := citeSymbolRepo(t)

	cases := []struct {
		name  string
		notes string
	}{
		// Bare continuation refs: a second location for a file named
		// earlier in the sentence. No file token, so no mechanical
		// resolution is possible.
		{"bare-continuation-comma", fmt.Sprintf("`TopLevelFunc` (%s:5), and again at :472-479.", rel)},
		{"bare-continuation-paren", fmt.Sprintf("`TopLevelFunc` (%s:5) stamps handler_name (:174).", rel)},
		// Non-Go line refs. The registry carries five of these, and
		// they are OUT by a stated reason, not by accident: the check
		// resolves a symbol declaration, and a Markdown table row or a
		// YAML key has no declaration to resolve.
		{"markdown-line-ref", "the sibling bar is lang.rust.framework.warp.md:21."},
		{"yaml-line-ref", "the rule body is aws_cdk.yaml:54-58."},
		// A Go file named with no line number at all is prose, not a
		// line citation.
		{"file-without-line", "spring_ecosystem.go remains Java-only."},
		// No notes at all.
		{"empty-notes", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if errs := runCiteSymbols(t, root, tc.notes); len(errs) != 0 {
				t.Errorf("expected clean, got %v", errs)
			}
		})
	}
}

// TestCiteSymbol_CheckRecursesIntoGroupedAndFrameworkTiers is the
// finding-driven test: the utoipa citations live at
// capabilities.Routing.route_extraction, one level deeper than
// capabilities.<cell>, and a flat walk of `capabilities` sees 38 of the
// 53 citations and misses 15 in silence. This drives the real
// validateRegistry entry point over a record in each of the three
// tiers and asserts the defect is reported from all of them.
func TestCiteSymbol_CheckRecursesIntoGroupedAndFrameworkTiers(t *testing.T) {
	root, rel := citeSymbolRepo(t)
	bad := fmt.Sprintf("`TopLevelFunc` (%s:10).", rel) // stale: declared at 5

	tiers := []struct {
		name string
		rec  Record
		want string
	}{
		{
			name: "flat",
			rec: Record{
				ID: "lang.go.framework.flat", Category: "language", Language: "go", Label: "Flat",
				Capabilities: map[string]Capability{"route_extraction": {Status: StatusFull, Notes: bad}},
			},
			want: "capabilities[route_extraction]",
		},
		{
			name: "grouped",
			rec: Record{
				ID: "lang.rust.framework.grouped", Category: "http_framework", Subcategory: "http_backend", Language: "rust", Label: "Grouped",
				Groups: map[string]map[string]Capability{
					"Routing": {"route_extraction": {Status: StatusFull, Notes: bad}},
				},
			},
			want: "capabilities[Routing].route_extraction",
		},
		{
			name: "framework_specific",
			rec: Record{
				ID: "lang.go.framework.fwspec", Category: "language", Language: "go", Label: "FwSpec",
				Capabilities: map[string]Capability{"route_extraction": {Status: StatusFull}},
				FrameworkSpecific: map[string]map[string]Capability{
					"FwSpec Internals": {"macro_expansion": {Status: StatusFull, Notes: bad}},
				},
			},
			want: "macro_expansion",
		},
	}
	for _, tc := range tiers {
		t.Run(tc.name, func(t *testing.T) {
			reg := &Registry{SchemaVersion: SchemaVersion, Records: []Record{tc.rec}}
			res := validateRegistry(reg, root)
			joined := strings.Join(res.Errors, "\n")
			if !strings.Contains(joined, "is stale") {
				t.Fatalf("stale cite in the %s tier was not reported; errors:\n%s", tc.name, joined)
			}
			if !strings.Contains(joined, tc.want) {
				t.Errorf("error did not name the cell %q; errors:\n%s", tc.want, joined)
			}
		})
	}
}

// TestCiteSymbol_ShippedRegistryHasNoStaleCitations runs the check over
// the real docs/coverage/registry.json against the real tree. This is
// the regression pin: when a cited source file is restructured, this
// test goes red in the PR that moved the code rather than silently
// leaving a wrong pointer behind, which is what happened for 21 of 53
// citations before #6673.
func TestCiteSymbol_ShippedRegistryHasNoStaleCitations(t *testing.T) {
	root := repoRoot(t)
	reg, err := loadRegistry(filepath.Join(root, "docs", "coverage", "registry.json"))
	if err != nil {
		t.Fatalf("load registry: %v", err)
	}
	idx := newDeclIndex(root)
	res := &ValidationResult{}
	seen := 0
	for i, rec := range reg.Records {
		for _, tier := range []map[string]map[string]Capability{rec.Groups, rec.FrameworkSpecific} {
			for gname, caps := range tier {
				for k, c := range caps {
					seen += len(citeLineRe.FindAllString(c.Notes, -1))
					validateCiteSymbols(res, fmt.Sprintf("records[%d] (%s).%s.%s", i, rec.ID, gname, k), c, idx)
				}
			}
		}
		for k, c := range rec.Capabilities {
			seen += len(citeLineRe.FindAllString(c.Notes, -1))
			validateCiteSymbols(res, fmt.Sprintf("records[%d] (%s).%s", i, rec.ID, k), c, idx)
		}
	}
	if len(res.Errors) != 0 {
		t.Errorf("shipped registry has %d cite defect(s):\n%s", len(res.Errors), strings.Join(res.Errors, "\n"))
	}
	// Vacuity gate. `seen != 0` is not enough: a walk that regressed to
	// finding one citation would still be green, and the whole reason
	// this defect survived is that nobody noticed a population going
	// unread. Assert the EXACT count, so shrinking the observed
	// population is a test failure and growing it is a deliberate
	// update with a number to justify.
	const wantCitations = 35
	if seen != wantCitations {
		t.Errorf("walked %d symbol-anchored line citation(s), want exactly %d — if a citation was legitimately added or removed, update wantCitations and say so in the PR body", seen, wantCitations)
	}
}
