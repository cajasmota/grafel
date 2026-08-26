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

// Method has the same name as nothing else in the file.
func (r *Recv) Method() {}

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
var citeSymbolFixtureLines = map[string]int{
	"TopLevelFunc": 5,
	"Method":       10,
	"BlockVar":     14,
	"OtherVar":     15,
	"BlockConst":   19,
	"BlockType":    22,
	"Single":       24,
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
	return root, filepath.ToSlash(rel)
}

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
		got, ok := idx.declLinesForSymbol(rel, sym)
		if !ok {
			t.Fatalf("fixture source did not parse")
		}
		if len(got) != 1 || got[0] != want {
			t.Errorf("symbol %q: fixture declares it at %v, test claims %d — update citeSymbolFixtureLines", sym, got, want)
		}
	}
}

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
		// Range opening on the first doc-comment line, closing past the
		// declaration — convention rule (2).
		{"func/range/doc-comment-open", fmt.Sprintf("matched by `TopLevelFunc` (%s:3-6).", rel)},
		{"method/single", fmt.Sprintf("the receiver helper `Method` (%s:10) does it.", rel)},
		{"var-block/single", fmt.Sprintf("`BlockVar` (%s:14) is the pattern.", rel)},
		{"var-block/range", fmt.Sprintf("`BlockVar` (%s:13-14) is the pattern.", rel)},
		{"const-block/single", fmt.Sprintf("`BlockConst` (%s:19) names the edge kind.", rel)},
		{"type/single", fmt.Sprintf("`BlockType` (%s:22) carries the fields.", rel)},
		{"standalone-var/single", fmt.Sprintf("`Single` (%s:24) is set once.", rel)},
		// Two citations in one note, one at the head and one at the tail.
		{"two-citations-one-note", fmt.Sprintf("`TopLevelFunc` (%s:5) calls into `Method` (%s:10).", rel, rel)},
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
			notes: fmt.Sprintf("`TopLevelFunc` (%s:10).", rel),
			want:  "is stale",
		},
		{
			// Range drift far enough that the declaration falls outside.
			name:  "stale-range",
			notes: fmt.Sprintf("`TopLevelFunc` (%s:14-19).", rel),
			want:  "is stale",
		},
		{
			// The `-1` doc-comment rot #6671 corrected six of: cited one
			// line above the declaration.
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
			// A number with no symbol in front of it is unverifiable
			// prose by construction — this is the rule that stops the
			// population from growing back.
			name:  "unanchored",
			notes: fmt.Sprintf("the dispatch happens at %s:5.", rel),
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
		// A non-Go file reference: the checker parses Go, and the
		// population regex requires a .go suffix.
		{"markdown-line-ref", "the sibling bar is lang.rust.framework.warp.md:21."},
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
	// Guard against the check silently observing nothing — the reason
	// the citations went unchecked for so long is that no code read
	// them at all.
	if seen == 0 {
		t.Fatal("no line citations found in the registry: the check is observing nothing")
	}
	t.Logf("validated %d symbol-anchored line citation(s)", seen)
}
