package feedback

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// priorReport is a minimal but format-faithful stand-in for a stored report
// under ~/.grafel/feedback/. It carries both Section-2 tables so the parser is
// exercised on the real shape rather than a convenient one.
const priorReportBothTables = `# grafel feedback report

Generated: 2026-07-16T06:58:27Z
grafel version: v0.1.8
Group profile: 2 language(s) (java, css), 2000-2500 entities, 5500-6000 relationships
Confidence: 71% (20/28 sanity checks passed)

## 1. Extractor Coverage

| Kind | Language | Count (range) |
|---|---|---|
| Route | java | 21-100 |

## 2. Orphan Rate

An entity is orphan when it has no semantic edge in EITHER direction.

| Kind | Total | Orphan | Orphan % |
|---|---|---|---|
| Route | 40 | 3 | 7.5% |
| Service | 22 | 22 | 100.0% |

**High-orphan kinds** (> 30%):

- ` + "`Service`" + `: 100.0% orphan rate

**Expected/terminal orphans** — no entity of these kinds carries a semantic edge.

| Kind | Total | Terminal orphan | Terminal orphan % |
|---|---|---|---|
| SCOPE.Stylesheet | 182 | 182 | 100.0% |

## 3. Resolution Disposition

| Disposition | Percentage |
|---|---|
| resolved | 59.97% |
`

// TestParseKindParticipation_ReadsBothSection2Tables pins how history is read:
// a kind participates when it appears in the defect orphan table below 100%,
// and does NOT participate when it is at 100% there or listed under the
// terminal table. Rows outside Section 2 must never be mistaken for kinds.
func TestParseKindParticipation_ReadsBothSection2Tables(t *testing.T) {
	got := parseKindParticipation(priorReportBothTables)

	want := map[string]bool{
		"Route":            true,  // 7.5% orphan → participates
		"Service":          false, // 100.0% in the defect table → no participation
		"SCOPE.Stylesheet": false, // terminal table → no participation
	}
	if len(got) != len(want) {
		t.Fatalf("parsed %d kinds (%v), want %d (%v)", len(got), got, len(want), want)
	}
	for kind, wantPart := range want {
		gotPart, ok := got[kind]
		if !ok {
			t.Errorf("kind %q missing from parsed participation", kind)
			continue
		}
		if gotPart != wantPart {
			t.Errorf("kind %q participation = %v, want %v", kind, gotPart, wantPart)
		}
	}
	// Section 1 and Section 3 rows must not leak in.
	for _, bad := range []string{"Disposition", "Language", "resolved"} {
		if _, ok := got[bad]; ok {
			t.Errorf("non-kind row %q leaked into participation map", bad)
		}
	}
}

// TestLoadKindParticipation_ScopedToGroupAndOred checks that history is read
// per group (the group name lives in the FILENAME, not the report body) and
// that participation ORs across reports: participating in ANY prior report is
// enough to make a later zero-participation run a regression.
func TestLoadKindParticipation_ScopedToGroupAndOred(t *testing.T) {
	dir := t.TempDir()

	// Older report for our group: Route participated.
	write := func(name, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("mygroup-20260715T221721.md", priorReportBothTables)
	// Newer report for our group where Route had already gone terminal. The
	// OR must still remember that it once participated.
	write("mygroup-20260716T065826.md", `## 2. Orphan Rate

An entity is orphan when it has no semantic edge in EITHER direction (CONTAINS/DECLARES excluded, in both directions).

| Kind | Total | Terminal orphan | Terminal orphan % |
|---|---|---|---|
| Route | 40 | 40 | 100.0% |

## 3. Resolution Disposition
`)
	// A different group must be invisible.
	write("othergroup-20260716T065826.md", `## 2. Orphan Rate

An entity is orphan when it has no semantic edge in EITHER direction (CONTAINS/DECLARES excluded, in both directions).

| Kind | Total | Orphan | Orphan % |
|---|---|---|---|
| SCOPE.Stylesheet | 50 | 1 | 2.0% |

## 3. x
`)
	// Non-report noise.
	write("mygroup-notes.txt", "ignore me")
	// Matching is EXACT-STEM, not prefix: a different group whose name merely
	// starts with ours must stay invisible.
	write("mygroup-extra-20260716T065826.md", `## 2. Orphan Rate

An entity is orphan when it has no semantic edge in EITHER direction (CONTAINS/DECLARES excluded, in both directions).

| Kind | Total | Orphan | Orphan % |
|---|---|---|---|
| SCOPE.PrefixDecoy | 50 | 1 | 2.0% |

## 3. x
`)

	got, err := loadKindParticipation(dir, "mygroup")
	if err != nil {
		t.Fatalf("loadKindParticipation: %v", err)
	}
	if !got["Route"] {
		t.Errorf("Route participation = %v, want true (it participated in the older report)", got["Route"])
	}
	if part, ok := got["SCOPE.Stylesheet"]; !ok || part {
		t.Errorf("SCOPE.Stylesheet = (%v, %v), want (false, true) — othergroup must not bleed in", part, ok)
	}
	if _, ok := got["SCOPE.PrefixDecoy"]; ok {
		t.Errorf("group matching is prefix-based: mygroup-extra-*.md bled into group %q history (%v)", "mygroup", got)
	}
}

// TestLoadKindParticipation_NoHistoryIsEmptyNotError is the first-run case:
// an absent or empty directory is normal, not a failure.
func TestLoadKindParticipation_NoHistoryIsEmptyNotError(t *testing.T) {
	got, err := loadKindParticipation(filepath.Join(t.TempDir(), "does-not-exist"), "mygroup")
	if err != nil {
		t.Fatalf("missing history dir must not error, got %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("want empty participation, got %v", got)
	}
}

// TestKindNamesSurviveAnonymisation is the confirmation the issue asks for
// rather than assumes: kind names are the join key of this whole check, so
// they must pass through anonymisation VERBATIM and round-trip back out of a
// rendered report. If a future change starts scrubbing kinds, this fails.
func TestKindNamesSurviveAnonymisation(t *testing.T) {
	const kind = "SCOPE.Stylesheet"

	// NameHash uses kind only to pick a prefix; it never emits the kind, and
	// PathScrub never sees it. Anonymisation therefore has no path that could
	// rewrite a kind — but the render is what actually ships, so assert there.
	r := &Report{
		TotalEntities:      1000,
		Languages:          []string{"css"},
		EntitiesByLanguage: map[string]int{"css": 1000},
		OrphanByKind: map[string]KindStats{
			"Route": {Total: 40, OrphanCount: 3, OrphanPct: 7.5},
		},
		OrphanTerminalByKind: map[string]KindStats{
			kind: {Total: 182, OrphanCount: 182, OrphanPct: 100.0},
		},
		FrameworkHits: map[string]int{},
	}
	var buf bytes.Buffer
	if err := Render(&buf, r); err != nil {
		t.Fatalf("Render: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, kind) {
		t.Fatalf("kind %q does not appear verbatim in the rendered report — anonymisation now scrubs kinds and longitudinal comparison is broken", kind)
	}

	// Round-trip: the parser must recover exactly what Render wrote.
	got := parseKindParticipation(out)
	if part, ok := got[kind]; !ok || part {
		t.Errorf("round-trip of terminal kind %q = (%v, ok=%v), want (false, ok=true)", kind, part, ok)
	}
	if part, ok := got["Route"]; !ok || !part {
		t.Errorf("round-trip of participating kind Route = (%v, ok=%v), want (true, ok=true)", part, ok)
	}
}

// TestRunSanityChecks_KindThatStoppedParticipatingFails is direction 1 of the
// two-directional requirement: a kind that HAD semantic edges in a prior
// report and has none now is a resolver regression and must fail.
func TestRunSanityChecks_KindThatStoppedParticipatingFails(t *testing.T) {
	r := &Report{
		TotalEntities:      1000,
		EntitiesByLanguage: map[string]int{"python": 1000},
		OrphanTerminalByKind: map[string]KindStats{
			"Route": {Total: 40, OrphanCount: 40, OrphanPct: 100.0},
		},
		FrameworkHits:      map[string]int{},
		priorParticipation: map[string]bool{"Route": true},
	}

	results, _ := runSanityChecks(r)
	var found *SanityResult
	for i := range results {
		if results[i].Name == participationRegressionCheckName("Route") {
			found = &results[i]
		}
	}
	if found == nil {
		t.Fatalf("no %s check emitted; got %+v", participationRegressionCheckName("Route"), results)
	}
	if found.Passed {
		t.Errorf("regression check PASSED for a kind that lost all semantic edges; note=%q", found.Note)
	}
	if !strings.Contains(found.Note, "prior report") {
		t.Errorf("note must cite the prior-report evidence, got %q", found.Note)
	}
}

// TestRunSanityChecks_TerminalByDesignKindWithHistoryDoesNotFail is direction
// 2, and is the whole point of #6377: a kind observed in prior reports that
// NEVER participated (CSS selectors, markdown fences, requirements.txt) is
// terminal by design. It must raise nothing at all — neither the old
// participation gate nor the new regression gate.
func TestRunSanityChecks_TerminalByDesignKindWithHistoryDoesNotFail(t *testing.T) {
	r := &Report{
		TotalEntities:      1000,
		EntitiesByLanguage: map[string]int{"css": 1000},
		OrphanTerminalByKind: map[string]KindStats{
			"SCOPE.Stylesheet": {Total: 182, OrphanCount: 182, OrphanPct: 100.0},
		},
		FrameworkHits:      map[string]int{},
		priorParticipation: map[string]bool{"SCOPE.Stylesheet": false},
	}

	results, _ := runSanityChecks(r)
	for _, res := range results {
		if res.Name == participationCheckName("SCOPE.Stylesheet") ||
			res.Name == participationRegressionCheckName("SCOPE.Stylesheet") {
			t.Errorf("terminal-by-design kind with history raised %q (passed=%v, note=%q) — #6377 false positive intact",
				res.Name, res.Passed, res.Note)
		}
	}
}

// TestRunSanityChecks_FirstRunKeepsParticipationGate is the constraint that
// must not be lost: with NO history for a kind, the longitudinal check has
// nothing to say, so the original 100%-end gate still fires. That is the
// new-extractor case kind-carries-semantic-edges exists to catch.
func TestRunSanityChecks_FirstRunKeepsParticipationGate(t *testing.T) {
	for _, tc := range []struct {
		name  string
		prior map[string]bool
	}{
		{"no history at all", nil},
		{"history exists but not for this kind", map[string]bool{"Route": true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := &Report{
				TotalEntities:      1000,
				EntitiesByLanguage: map[string]int{"go": 1000},
				OrphanTerminalByKind: map[string]KindStats{
					"SCOPE.NewKind": {Total: 30, OrphanCount: 30, OrphanPct: 100.0},
				},
				FrameworkHits:      map[string]int{},
				priorParticipation: tc.prior,
			}
			results, _ := runSanityChecks(r)
			found := false
			for _, res := range results {
				if res.Name != participationCheckName("SCOPE.NewKind") {
					continue
				}
				found = true
				if res.Passed {
					t.Errorf("first-run participation gate PASSED; note=%q", res.Note)
				}
			}
			if !found {
				t.Fatalf("first-run participation gate did not fire; got %+v", results)
			}
		})
	}
}
