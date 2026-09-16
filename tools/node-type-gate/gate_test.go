package main

import (
	"bytes"
	"strings"
	"testing"
)

// TestBaselineParser_Rejects covers the rules that keep the baseline readable.
// A baseline nobody can read is a mute button, so each of these is a property
// the file format must actually enforce rather than merely document.
//
// VARIED across rows: exactly one defect per row, and a different one each
// time — field count, class vocabulary, empty dir, empty literal, a location
// that is not file:line, a missing justification, and a duplicate key.
// HELD CONSTANT: everything else about the row (a well-formed
// paired-with-correct-name entry for internal/extractors/lua), so a rejection
// can only be attributed to the varied defect.
func TestBaselineParser_Rejects(t *testing.T) {
	const ok = "paired-with-correct-name | internal/extractors/lua | function_statement | internal/extractors/lua/lua.go:175 | live name in the same switch"
	rows := []struct {
		name string
		text string
		want string
	}{
		{"too few fields", "paired-with-correct-name | internal/extractors/lua | function_statement", "at least 4"},
		{"unknown class", "misc | internal/extractors/lua | function_statement | internal/extractors/lua/lua.go:175 | note", "unknown class"},
		{"empty dir", "paired-with-correct-name |  | function_statement | internal/extractors/lua/lua.go:175 | note", "non-empty"},
		{"empty literal", "paired-with-correct-name | internal/extractors/lua |  | internal/extractors/lua/lua.go:175 | note", "non-empty"},
		{"location is not file:line", "paired-with-correct-name | internal/extractors/lua | function_statement | internal/extractors/lua/lua.go | note", "file:line"},
		{"no justification", "paired-with-correct-name | internal/extractors/lua | function_statement | internal/extractors/lua/lua.go:175 |", "needs a note"},
		{"duplicate key", ok + "\n" + ok, "duplicate"},
		{"known-defect with no issue reference", "known-defect | internal/extractors/lua | function_statement | internal/extractors/lua/lua.go:175 | someone will get to it", "must name its issue"},
	}
	for _, r := range rows {
		t.Run(r.name, func(t *testing.T) {
			_, err := ParseBaseline(strings.NewReader(r.text))
			if err == nil {
				t.Fatalf("accepted a malformed row: %q", r.text)
			}
			if !strings.Contains(err.Error(), r.want) {
				t.Errorf("rejected for the wrong reason: got %v, want a message containing %q", err, r.want)
			}
		})
	}

	// The control row: the same shape, with none of the defects, is accepted.
	// Without it every assertion above could be satisfied by a parser that
	// rejects everything.
	b, err := ParseBaseline(strings.NewReader(ok))
	if err != nil {
		t.Fatalf("well-formed row rejected: %v", err)
	}
	if len(b.Entries) != 1 {
		t.Fatalf("well-formed row produced %d entries, want 1", len(b.Entries))
	}
	if b.Entries[0].Line != 175 {
		t.Errorf("line parsed as %d, want 175", b.Entries[0].Line)
	}
}

// TestBaselineParser_CommentsAndBlanks: the header exists to be read, so it
// must not be data.
func TestBaselineParser_CommentsAndBlanks(t *testing.T) {
	in := "# a comment\n\n   \n" +
		"paired-with-correct-name | internal/extractors/lua | function_statement | internal/extractors/lua/lua.go:175 | note\n" +
		"   # indented comment\n"
	b, err := ParseBaseline(strings.NewReader(in))
	if err != nil {
		t.Fatalf("ParseBaseline: %v", err)
	}
	if len(b.Entries) != 1 {
		t.Fatalf("got %d entries, want 1", len(b.Entries))
	}
}

// TestBaselineMatch_IsKeyedOnDirLiteralFile pins the suppression key. Widening
// it to the package, or to the literal alone, would suppress defects nobody
// listed — which is exactly what Control 3 forbids at the integration level.
func TestBaselineMatch_IsKeyedOnDirLiteralFile(t *testing.T) {
	const row = "paired-with-correct-name | internal/extractors/lua | function_statement | internal/extractors/lua/lua.go:175 | note"
	b, err := ParseBaseline(strings.NewReader(row))
	if err != nil {
		t.Fatalf("ParseBaseline: %v", err)
	}
	if !b.Match("internal/extractors/lua", "function_statement", "internal/extractors/lua/lua.go") {
		t.Fatal("exact key did not match")
	}
	// VARIED one field at a time; HELD CONSTANT the other two.
	cases := []struct{ dir, lit, file string }{
		{"internal/extractors/php", "function_statement", "internal/extractors/lua/lua.go"},
		{"internal/extractors/lua", "local_function", "internal/extractors/lua/lua.go"},
		{"internal/extractors/lua", "function_statement", "internal/extractors/lua/other.go"},
	}
	for _, c := range cases {
		if b.Match(c.dir, c.lit, c.file) {
			t.Errorf("matched a different key: dir=%q lit=%q file=%q", c.dir, c.lit, c.file)
		}
	}
}

// TestBaselineUnmatched_TracksUse: the shrink mechanism. A row nothing hits must
// be reported, and a row something hits must not be.
func TestBaselineUnmatched_TracksUse(t *testing.T) {
	in := "paired-with-correct-name | internal/extractors/lua | function_statement | internal/extractors/lua/lua.go:175 | note\n" +
		"paired-with-correct-name | internal/extractors/lua | local_function | internal/extractors/lua/lua.go:204 | note\n"
	b, err := ParseBaseline(strings.NewReader(in))
	if err != nil {
		t.Fatalf("ParseBaseline: %v", err)
	}
	if got := len(b.Unmatched()); got != 2 {
		t.Fatalf("before any match: %d unmatched, want 2", got)
	}
	b.Match("internal/extractors/lua", "function_statement", "internal/extractors/lua/lua.go")
	un := b.Unmatched()
	if len(un) != 1 || un[0].Lit != "local_function" {
		t.Fatalf("after one match: %v, want only local_function", un)
	}
}

// TestBaselineFormat_RoundTrips: -update must not silently lose or corrupt
// rows, and the checked-in file must already be in that canonical form (so a
// refresh produces no diff and nobody has to guess whether the file is stale).
func TestBaselineFormat_RoundTrips(t *testing.T) {
	root := modRoot(t)
	b := realBaseline(t, root)
	var buf bytes.Buffer
	if err := b.Format(&buf, baselineHeader); err != nil {
		t.Fatalf("Format: %v", err)
	}
	back, err := ParseBaseline(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("re-parsing the formatted baseline failed: %v", err)
	}
	if len(back.Entries) != len(b.Entries) {
		t.Fatalf("round trip changed the entry count: %d -> %d", len(b.Entries), len(back.Entries))
	}
	for i := range b.Entries {
		a, c := b.Entries[i], back.Entries[i]
		if a.Class != c.Class || a.Dir != c.Dir || a.Lit != c.Lit || a.File != c.File || a.Line != c.Line || a.Note != c.Note {
			t.Errorf("row %d changed across a round trip:\n  %+v\n  %+v", i, a, c)
		}
	}
}

// TestEvaluate_ResolveRules drives Evaluate on a synthetic surface so each
// resolve rule can be varied in isolation, without waiting for a package load.
// The integration controls prove the rules fire on the real tree; this proves
// what each rule is.
//
// The two grammars are asymmetric on purpose: "alpha" has real_alpha and
// shared, "beta" has real_beta and shared. Package "pkg/multi" registers both,
// "pkg/solo" registers only alpha, "pkg/none" registers a language with no
// grammar at all.
//
// VARIED, one axis per row:
//   - which grammars hold the literal (both / only one / neither)
//   - whether the package has any grammar at all
//   - whether the literal is a whitelisted runtime kind (ERROR, MISSING)
//   - whether the literal is the empty-string sentinel
//   - whether a baseline row covers it
//
// HELD CONSTANT: the file, the line, the form (cmp), and the package for every
// row except the two that exist to vary the package. Rows "in both grammars"
// and "in neither" are the matched pair that makes the verdict column mean
// something: without the first, a gate that never fires would pass; without the
// second, a gate that always fires would.
func TestEvaluate_ResolveRules(t *testing.T) {
	grammars := map[string]*Grammar{
		"alpha": {Key: "alpha", Kinds: map[string]bool{"real_alpha": true, "shared": true}},
		"beta":  {Key: "beta", Kinds: map[string]bool{"real_beta": true, "shared": true}},
	}
	regs := []Registration{
		{Dir: "pkg/multi", Key: "alpha"}, {Dir: "pkg/multi", Key: "beta"},
		{Dir: "pkg/solo", Key: "alpha"},
		{Dir: "pkg/none", Key: "nosuchlanguage"},
	}
	site := func(dir, lit string) Site {
		return Site{Pkg: dir, Dir: dir, File: dir + "/x.go", Line: 7, Lit: lit, Form: FormCmp, Const: true}
	}
	rows := []struct {
		name     string
		site     Site
		baseline string
		wantFail bool
		wantMiss bool
	}{
		{"in both grammars", site("pkg/multi", "shared"), "", false, false},
		{"in only one grammar of a multi-grammar package", site("pkg/multi", "real_alpha"), "", false, false},
		{"in only the other grammar", site("pkg/multi", "real_beta"), "", false, false},
		{"in neither grammar", site("pkg/multi", "nowhere"), "", true, true},
		{"single-grammar package, present", site("pkg/solo", "real_alpha"), "", false, false},
		{"single-grammar package, absent", site("pkg/solo", "real_beta"), "", true, true},
		{"package with no grammar at all", site("pkg/none", "nowhere"), "", false, false},
		{"ERROR is a runtime kind, in no symbol table", site("pkg/multi", "ERROR"), "", false, false},
		{"MISSING is a runtime kind, in no symbol table", site("pkg/multi", "MISSING"), "", false, false},
		{"the empty string is a sentinel, never a node kind", site("pkg/multi", ""), "", false, false},
		{"absent but baselined", site("pkg/multi", "nowhere"), "not-a-node-type | pkg/multi | nowhere | pkg/multi/x.go:7 | synthetic", false, true},
		{"baselined under a different literal", site("pkg/multi", "nowhere"), "not-a-node-type | pkg/multi | other | pkg/multi/x.go:7 | synthetic", true, true},
	}
	for _, r := range rows {
		t.Run(r.name, func(t *testing.T) {
			base, err := ParseBaseline(strings.NewReader(r.baseline))
			if err != nil {
				t.Fatalf("baseline: %v", err)
			}
			res := Evaluate(Scan{Sites: []Site{r.site}}, regs, grammars, base)
			// StaleBaseline is not the axis under test here; the "baselined
			// under a different literal" row would otherwise fail for two
			// reasons at once and grade neither.
			res.StaleBaseline = nil
			if got := res.Failed(); got != r.wantFail {
				t.Errorf("Failed() = %v, want %v (failures: %v)", got, r.wantFail, res.Failures)
			}
			if got := len(res.Misses) > 0; got != r.wantMiss {
				t.Errorf("recorded a miss = %v, want %v", got, r.wantMiss)
			}
			if r.wantFail {
				if len(res.Failures) != 1 {
					t.Fatalf("want exactly one failure, got %d", len(res.Failures))
				}
				d := res.Failures[0].String()
				for _, want := range []string{r.site.Lit, r.site.File, r.site.Dir} {
					if !strings.Contains(d, want) {
						t.Errorf("diagnostic %q does not name %q", d, want)
					}
				}
			}
		})
	}
}

// TestExitCode pins the whole-tree verdict, including the count floor. The
// floor is the cheapest of the five vacuity guards — it catches only "the scan
// read nothing" — but a green exit from a scan that read nothing is exactly the
// shape this tool exists to distrust, so it must not exit 0.
//
// VARIED, one per row: the resolved-site count (below / at / above the floor),
// whether a failure is present, and whether a stale baseline row is present.
// HELD CONSTANT: every other field of Result. Row "clean" is the negative
// control without which every red row below could be satisfied by a function
// that always returns 1.
func TestExitCode(t *testing.T) {
	miss := Miss{Site: Site{Dir: "d", File: "f.go", Line: 1, Lit: "x"}}
	stale := BaselineEntry{Class: "not-a-node-type", Dir: "d", Lit: "x", File: "f.go", Line: 1, Note: "n"}
	rows := []struct {
		name string
		res  Result
		want int
	}{
		{"clean", Result{Resolved: minResolvedSites}, 0},
		{"clean, well above the floor", Result{Resolved: minResolvedSites * 2}, 0},
		{"one site below the floor", Result{Resolved: minResolvedSites - 1}, 1},
		{"read nothing at all", Result{Resolved: 0}, 1},
		{"a new dead literal", Result{Resolved: minResolvedSites, Failures: []Miss{miss}}, 1},
		{"a stale baseline row", Result{Resolved: minResolvedSites, StaleBaseline: []BaselineEntry{stale}}, 1},
	}
	for _, r := range rows {
		t.Run(r.name, func(t *testing.T) {
			if got := ExitCode(r.res); got != r.want {
				t.Errorf("ExitCode = %d, want %d", got, r.want)
			}
		})
	}
	// Failed() must stay floor-free: the positive controls drive single-package
	// runs, which resolve far fewer sites than a whole-tree floor.
	if (Result{Resolved: 0}).Failed() {
		t.Error("Failed() folded in the count floor; a single-package control run would then always be red")
	}
}

// TestBaselineOnDisk_ClassesInUse records which baseline classes the checked-in
// file actually uses, so a reviewer can see the shape of the list without
// reading 48 rows. It asserts only what must always hold: the file parses (the
// parser enforces the class vocabulary, the justification, and the
// issue-reference rule for known-defect), and it is not empty — an empty
// baseline would mean the gate is green for a reason nobody checked.
func TestBaselineOnDisk_ClassesInUse(t *testing.T) {
	root := modRoot(t)
	b := realBaseline(t, root)
	if len(b.Entries) == 0 {
		t.Fatal("the checked-in baseline is empty; either every dead literal was fixed (delete this test with the file) or the file was not read")
	}
	counts := map[string]int{}
	for _, e := range b.Entries {
		counts[e.Class]++
	}
	t.Logf("%d rows: %v", len(b.Entries), counts)
}
