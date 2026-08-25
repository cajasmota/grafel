package feedback

import (
	"strings"
	"testing"
)

// #6405 — the Section-1 kind x language table and the Section-2 orphan table
// count in units that were never comparable, and the report said nothing.
//
// They differ on THREE axes, not the one the issue names:
//
//	unit   — kindLangCounts is per entity OCCURRENCE per document;
//	         kindTotals is unique entity IDs (#6378).
//	scope  — the Section-1 table drops every entity whose language is "",
//	         Section 2 counts them.
//	floor  — Section 1 publishes per (kind x language) >= 10, Section 2 per
//	         kind >= 10, so a kind can appear in one table and not the other
//	         for reasons that have nothing to do with units.
//
// Neither counter is changed: unifying the unit would still leave the language
// filter and the floors, so the tables would stay non-comparable and a "same
// units" label would then LIE rather than merely omit. The fix is labels.
//
// The labels are deliberately NOT in the pipe-delimited header rows.
// history.go matches `| Kind | Total | Orphan | Orphan % |` literally
// (defectTableHeader) to recover per-kind participation, and that key is OR-ed
// across every stored report and never expires — a one-way door. Changing that
// row would irreversibly break longitudinal parsing of reports already on
// disk. TestRender_UnitLabelsDoNotBreakHistoryParsing below is the guard.
func TestRender_EntityKindDistTableNamesItsUnitScopeAndFloor(t *testing.T) {
	// Scoped to Section 1 and to the text ABOVE the table it describes. A
	// whole-report Contains would pass with the two captions swapped between
	// sections — a mutant that survived exactly that assertion — which is the
	// worst possible outcome here: each table would then carry a confident
	// label for the other table's units.
	out := sectionOneKindDist(t, renderLabelFixture(t))

	for _, want := range []string{
		// unit — occurrences, not unique entities.
		"Counts entity OCCURRENCES (one entity emitted into two documents counts twice)",
		// the printed value is a bucket, not the exact count.
		"as a bucketed range",
		// scope — language-less entities are silently absent.
		"entities with no language are excluded from this table entirely",
		// floor — per (kind x language).
		"A row is published when that kind x language pair has >= 10 occurrences.",
		// the point of the whole label.
		"Not comparable with `Total` in Section 2: different unit, different scope, different floor.",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("entity-kind-distribution caption missing clause:\nwant substring: %q", want)
		}
	}
}

func TestRender_OrphanTableNamesItsUnitAndFloor(t *testing.T) {
	// Scoped to Section 2, above its first table — see the note on the
	// Section-1 test for why a whole-report match is not enough.
	out := sectionTwoPreamble(t, renderLabelFixture(t))

	for _, want := range []string{
		"`Total` counts UNIQUE entities of the kind, in every language including entities with none, published when that kind has >= 10 unique entities.",
		"a different unit, scope and floor from the occurrence ranges in Section 1",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("orphan-table unit label missing clause:\nwant substring: %q", want)
		}
	}
}

// TestRender_UnitLabelsDoNotBreakHistoryParsing is the one-way-door guard: the
// labels must live OUTSIDE the row history.go matches. If a future edit moves
// them into the header row (or renames its columns), parseKindParticipation
// stops recovering kinds from the rendered report and every stored report
// becomes unreadable — silently, because nothing else reads that row.
func TestRender_UnitLabelsDoNotBreakHistoryParsing(t *testing.T) {
	out := renderLabelFixture(t)

	if !strings.Contains(out, "| Kind | Total | Orphan | Orphan % |") {
		t.Fatal("Section-2 defect-table header row changed — history.go parses it literally and the key never expires; every stored report is now unparseable")
	}

	got := parseKindParticipation(out)
	if part, ok := got["SCOPE.Route"]; !ok || !part {
		t.Errorf("round-trip of participating kind SCOPE.Route = (%v, ok=%v), want (true, ok=true)", part, ok)
	}
	if part, ok := got["SCOPE.Stylesheet"]; !ok || part {
		t.Errorf("round-trip of terminal kind SCOPE.Stylesheet = (%v, ok=%v), want (false, ok=true)", part, ok)
	}
}

// sectionOneKindDist returns the text between the "### Entity kind
// distribution" heading and the table it introduces.
func sectionOneKindDist(t *testing.T, out string) string {
	t.Helper()
	return between(t, out, "### Entity kind distribution", "| Kind | Language | Count (range) |")
}

// sectionTwoPreamble returns the text between the Section-2 heading and its
// first table row.
func sectionTwoPreamble(t *testing.T, out string) string {
	t.Helper()
	return between(t, out, "## 2. Orphan Rate", "| Kind | Total | Orphan | Orphan % |")
}

func between(t *testing.T, out, start, end string) string {
	t.Helper()
	i := strings.Index(out, start)
	if i < 0 {
		t.Fatalf("rendered report has no %q", start)
	}
	rest := out[i+len(start):]
	j := strings.Index(rest, end)
	if j < 0 {
		t.Fatalf("rendered report has no %q after %q", end, start)
	}
	return rest[:j]
}

func renderLabelFixture(t *testing.T) string {
	t.Helper()
	r := &Report{
		TotalEntities:      1000,
		GeneratedAt:        mustParseTime("2026-08-25T00:00:00Z"),
		GroupName:          "test-group",
		Languages:          []string{"go"},
		EntitiesByLanguage: map[string]int{"go": 1000},
		EntityKindDist: []EntityKindLang{
			{Kind: "SCOPE.Route", Language: "go", Count: 60},
		},
		OrphanByKind: map[string]KindStats{
			"SCOPE.Route": {Total: 31, OrphanCount: 4, OrphanPct: 12.9},
		},
		OrphanTerminalByKind: map[string]KindStats{
			"SCOPE.Stylesheet": {Total: 182, OrphanCount: 182, OrphanPct: 100.0},
		},
		FrameworkHits: map[string]int{},
	}
	var sb strings.Builder
	if err := Render(&sb, r); err != nil {
		t.Fatalf("Render: %v", err)
	}
	return sb.String()
}
