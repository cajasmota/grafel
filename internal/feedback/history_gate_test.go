package feedback

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
)

// preDirectionReport is the shape of every report actually on disk today: the
// Section-2 preamble defines orphan as "no OUTGOING semantic edges", the
// metric that #6375 (c6e6e148c) replaced. Under that metric a pure SINK reads
// 100.0% while participating fully, so the participation this file records is
// not comparable with a current report's.
const preDirectionReport = `# grafel feedback report

Generated: 2026-07-16T06:58:27Z
grafel version: v0.1.8-4-g61c99101b (commit 61c99101b, built 2026-07-15T23:20:09Z)
Group profile: 2 language(s) (java, css), 2000-2500 entities, 5500-6000 relationships
Confidence: 71% (20/28 sanity checks passed)

## 2. Orphan Rate

An entity is orphan when it has no outgoing semantic edges (CONTAINS/DECLARES excluded).

| Kind | Total | Orphan | Orphan % |
|---|---|---|---|
| Route | 40 | 3 | 7.5% |
| SCOPE.ExceptionType | 22 | 22 | 100.0% |

## 3. Resolution Disposition
`

// TestParseKindParticipation_IgnoresPreDirectionReports is FIX 1.
//
// Reading a pre-#6375 report as history is worse than reading no history at
// all: a sink kind (SCOPE.ExceptionType, SCOPE.Package, SCOPE.Plugin,
// SCOPE.Config) reads 100.0% under the old outgoing-only metric, so it is
// recorded as "observed, never participated" — which makes check 2b go SILENT
// for that kind forever. A dead THROWS/IMPORTS resolver would then raise
// nothing at all. The whole file must be ignored.
func TestParseKindParticipation_IgnoresPreDirectionReports(t *testing.T) {
	got := parseKindParticipation(preDirectionReport)
	if len(got) != 0 {
		t.Fatalf("pre-#6375 report yielded %v, want an empty map — its orphan metric is not comparable with today's", got)
	}
}

// TestLoadKindParticipation_SkipsPreDirectionFiles is FIX 1 at the directory
// level: an old file must not contribute participation state either way, so a
// kind seen only there stays UNOBSERVED (key absent) and the first-run gate
// keeps running.
func TestLoadKindParticipation_SkipsPreDirectionFiles(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "mygroup-20260716T065826.md"), []byte(preDirectionReport), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := loadKindParticipation(dir, "mygroup")
	if err != nil {
		t.Fatalf("loadKindParticipation: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("history from a pre-#6375 file leaked in: %v", got)
	}
}

// TestParseKindParticipation_IgnoresFourColumnRowsOutsideSection2 is FIX 3.
//
// The existing leak check used "Disposition"/"Language"/"resolved" rows, which
// are 2- and 3-column and can never match kindRowRe — so it passed with the
// Section-2 scoping deleted. This fixture puts a GENUINE 4-column
// "| name | N | N | X% |" row outside Section 2, which the row pattern DOES
// match, so the scoping assertion can actually fail.
func TestParseKindParticipation_IgnoresFourColumnRowsOutsideSection2(t *testing.T) {
	const md = `# grafel feedback report

## 1. Extractor Coverage

| Kind | Total | Orphan | Orphan % |
|---|---|---|---|
| SECTION1_IMPOSTOR | 40 | 3 | 7.5% |

## 2. Orphan Rate

An entity is orphan when it has no semantic edge in EITHER direction (CONTAINS/DECLARES excluded, in both directions).

| Kind | Total | Orphan | Orphan % |
|---|---|---|---|
| Route | 40 | 3 | 7.5% |

## 3. Resolution Disposition

| Kind | Total | Orphan | Orphan % |
|---|---|---|---|
| SECTION3_IMPOSTOR | 90 | 90 | 100.0% |
`
	got := parseKindParticipation(md)
	for _, bad := range []string{"SECTION1_IMPOSTOR", "SECTION3_IMPOSTOR"} {
		if _, ok := got[bad]; ok {
			t.Errorf("4-column row %q from outside Section 2 leaked into participation map: %v", bad, got)
		}
	}
	if part, ok := got["Route"]; !ok || !part {
		t.Errorf("Route = (%v, ok=%v), want (true, ok=true)", part, ok)
	}
}

// TestParseKindParticipation_RoundingDoesNotEraseParticipation is FIX 5.
//
// 9999 of 10000 orphans renders as "100.0%" at %.1f. Keying participation off
// the printed percentage therefore records a kind that DID participate as
// never having participated, and a later genuine regression on that kind is
// then silently accepted as the status quo. The integer Total/Orphan columns
// are exact, so they are what the parser reads.
func TestParseKindParticipation_RoundingDoesNotEraseParticipation(t *testing.T) {
	const md = `## 2. Orphan Rate

An entity is orphan when it has no semantic edge in EITHER direction (CONTAINS/DECLARES excluded, in both directions).

| Kind | Total | Orphan | Orphan % |
|---|---|---|---|
| SCOPE.Class | 10000 | 9999 | 100.0% |

## 3. x
`
	got := parseKindParticipation(md)
	if part, ok := got["SCOPE.Class"]; !ok || !part {
		t.Errorf("SCOPE.Class = (%v, ok=%v), want (true, ok=true) — one entity in 10000 participated, %%.1f rounding must not erase it", part, ok)
	}
}

// TestParseKindParticipation_TerminalTableOverridesDefectRow pins a property
// of the REAL rendered shape that a hand-built fixture hides: Generate puts
// every kind with Total >= 10 into OrphanByKind, terminal kinds included (with
// an orphan count of 0, because their orphans are counted in the terminal
// map). A terminal-by-design kind therefore appears in BOTH Section-2 tables —
// as "0 of 182 orphaned" in the defect table and "182 of 182" in the terminal
// table. Reading the defect row as participation would record CSS stylesheets
// as having semantic edges and re-fire the regression gate on them forever,
// which is #6377 all over again. Terminal-table membership is authoritative.
func TestParseKindParticipation_TerminalTableOverridesDefectRow(t *testing.T) {
	r := &Report{
		TotalEntities:      1000,
		Languages:          []string{"css"},
		EntitiesByLanguage: map[string]int{"css": 1000},
		OrphanByKind: map[string]KindStats{
			"SCOPE.Stylesheet": {Total: 182, OrphanCount: 0, OrphanPct: 0.0},
		},
		OrphanTerminalByKind: map[string]KindStats{
			"SCOPE.Stylesheet": {Total: 182, OrphanCount: 182, OrphanPct: 100.0},
		},
		FrameworkHits: map[string]int{},
	}
	var buf bytes.Buffer
	if err := Render(&buf, r); err != nil {
		t.Fatalf("Render: %v", err)
	}
	got := parseKindParticipation(buf.String())
	if part, ok := got["SCOPE.Stylesheet"]; !ok || part {
		t.Errorf("SCOPE.Stylesheet = (%v, ok=%v), want (false, ok=true) — the terminal table must win over the kind's 0%%-orphan defect row", part, ok)
	}
}

// terminalKindDocs builds a group whose SCOPE.Stylesheet kind has zero
// semantic participation (12 entities, CONTAINS only) alongside enough wired
// filler to clear minEntitiesForReport.
func terminalKindDocs() *graph.Document {
	ents := []graph.Entity{makeEntity("file1", "app.css", "SCOPE.Component", "css", "app.css", 1)}
	var rels []graph.Relationship
	for i := 0; i < 12; i++ {
		id := string(rune('a'+i)) + "sel"
		ents = append(ents, makeEntity(id, ".btn", "SCOPE.Stylesheet", "css", "app.css", 10+i))
		rels = append(rels, rel6346("c"+id, "file1", id, "CONTAINS"))
	}
	pe, pr := pad6346()
	ents = append(ents, pe...)
	rels = append(rels, pr...)
	return makeDoc(ents, rels)
}

// TestGenerate_HistoryDirFeedsTheParticipationGate is FIX 2.
//
// Both directional tests set priorParticipation directly on a Report literal,
// so the ONLY path production uses — Opts.HistoryDir -> Generate ->
// runSanityChecks — had zero coverage: blanking the assignment in Generate
// (`r.priorParticipation = nil`) left every test green while unplugging the
// whole feature. This drives it end to end through a temp history dir.
func TestGenerate_HistoryDirFeedsTheParticipationGate(t *testing.T) {
	dir := t.TempDir()
	prior := `# grafel feedback report

grafel version: v0.3.0

## 2. Orphan Rate

An entity is orphan when it has no semantic edge in EITHER direction (CONTAINS/DECLARES excluded, in both directions).

| Kind | Total | Orphan | Orphan % |
|---|---|---|---|
| SCOPE.Stylesheet | 12 | 2 | 16.7% |

## 3. Resolution Disposition
`
	if err := os.WriteFile(filepath.Join(dir, "g-20260716T065826.md"), []byte(prior), 0o600); err != nil {
		t.Fatal(err)
	}

	r, err := Generate(context.Background(), []*graph.Document{terminalKindDocs()},
		Opts{GroupName: "g", Version: "t", HistoryDir: dir})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if _, ok := r.OrphanTerminalByKind["SCOPE.Stylesheet"]; !ok {
		t.Fatalf("fixture no longer produces a terminal kind: %+v", r.OrphanTerminalByKind)
	}

	var regression, firstRun *SanityResult
	for i := range r.SanityResults {
		switch r.SanityResults[i].Name {
		case participationRegressionCheckName("SCOPE.Stylesheet"):
			regression = &r.SanityResults[i]
		case participationCheckName("SCOPE.Stylesheet"):
			firstRun = &r.SanityResults[i]
		}
	}
	if regression == nil {
		t.Fatalf("Generate did not consume Opts.HistoryDir: no %s check emitted; got %+v",
			participationRegressionCheckName("SCOPE.Stylesheet"), r.SanityResults)
	}
	if regression.Passed {
		t.Errorf("regression check PASSED for a kind the stored report recorded participating; note=%q", regression.Note)
	}
	if firstRun != nil {
		t.Errorf("first-run gate also fired despite history being present: %q", firstRun.Note)
	}
	// FIX 4: the check must name the only remedy available when a kind
	// legitimately and permanently stops participating.
	if !strings.Contains(regression.Note, "delete") {
		t.Errorf("regression note gives the reader no way out of a permanent stop; note=%q", regression.Note)
	}
}

// TestGenerate_HistoryDirIgnoresPreDirectionReports is FIX 1 through the
// production path: with only old-format history on disk, the group must fall
// back to first-run behaviour (the participation gate still fires) rather than
// being silently disabled by a metric that meant something else.
func TestGenerate_HistoryDirIgnoresPreDirectionReports(t *testing.T) {
	dir := t.TempDir()
	prior := strings.Replace(preDirectionReport, "| Route | 40 | 3 | 7.5% |",
		"| SCOPE.Stylesheet | 12 | 12 | 100.0% |", 1)
	if err := os.WriteFile(filepath.Join(dir, "g-20260716T065826.md"), []byte(prior), 0o600); err != nil {
		t.Fatal(err)
	}

	r, err := Generate(context.Background(), []*graph.Document{terminalKindDocs()},
		Opts{GroupName: "g", Version: "t", HistoryDir: dir})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	found := false
	for _, res := range r.SanityResults {
		if res.Name == participationCheckName("SCOPE.Stylesheet") {
			found = true
		}
	}
	if !found {
		t.Errorf("pre-#6375 history silently disabled the participation gate for SCOPE.Stylesheet; got %+v", r.SanityResults)
	}
}
