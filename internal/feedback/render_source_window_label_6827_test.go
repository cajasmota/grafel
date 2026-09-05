package feedback

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
)

// #6827 — "Source-window completeness" was rendered as *entities with valid
// start/END line* while the collector has only ever checked `StartLine > 0`.
// The one-sided check is deliberate and documented in report.go (requiring
// `EndLine > StartLine` scored 0.0% against real production data, and the two
// span-less fixture guards exist so that lesson is not re-learned), so the
// LABEL is the defect, not the check.
//
// These tests observe the RENDERED report, not a helper that produces it, and
// they are scoped to the Source-window subsection: a whole-report Contains
// survives deleting the very paragraph it claims to check.

const (
	// The value line. Names a start line and nothing else.
	sourceWindowValueLabel = "Entities with a start line: **"
	// The limitation the report must state OUT LOUD. Asserted verbatim
	// because it is the whole point of #6827: without it a reader takes the
	// percentage to mean end lines were checked and found present.
	sourceWindowLimitation = "**End lines are NOT examined**: this percentage says nothing about whether any entity carries an end line, and must not be read as span completeness."
)

// TestRender_SourceWindowLabelDoesNotPromiseEndLines pins the rendered wording.
func TestRender_SourceWindowLabelDoesNotPromiseEndLines(t *testing.T) {
	sec := sourceWindowSection(t, renderLabelFixture(t))

	if !strings.Contains(sec, sourceWindowValueLabel) {
		t.Errorf("source-window value line does not state what is measured\nwant substring: %q\nsection:\n%s", sourceWindowValueLabel, sec)
	}
	// The exact defect #6827 reports. "start/end" is the old two-sided
	// promise; any reappearance of it re-opens the issue.
	if strings.Contains(sec, "start/end") {
		t.Errorf("source-window section still promises a two-sided start/end check the collector never performs\nsection:\n%s", sec)
	}
	if !strings.Contains(sec, sourceWindowLimitation) {
		t.Errorf("source-window section does not state that end lines go unexamined\nwant substring: %q\nsection:\n%s", sourceWindowLimitation, sec)
	}
}

// TestRender_SourceWindowLimitationAppearsOnlyOverItsOwnMetric is the
// exclusivity half: presence alone is satisfied by pasting the caption over
// every metric, which would make it false everywhere else it lands.
func TestRender_SourceWindowLimitationAppearsOnlyOverItsOwnMetric(t *testing.T) {
	out := renderLabelFixture(t)
	sec := sourceWindowSection(t, out)

	if rest := strings.Replace(out, sec, "", 1); strings.Contains(rest, sourceWindowLimitation) {
		t.Errorf("the source-window end-line caption also appears outside the Source-window section, where it describes a metric it does not apply to")
	}
}

// TestGenerate_SourceWindowCountsStartLineOnly is the consequence assertion,
// kept separate from the wording assertion above: a correct label over a
// counter that in fact counts everything — or that consults EndLine — is the
// same defect wearing new prose.
//
// The fixture varies EndLine on BOTH sides of the start-line split — present
// and absent among the counted entities, present and absent among the
// uncounted ones — so the expected value is reachable only by ignoring EndLine
// entirely. It is deliberately NOT span-less and does not replace the two
// guards that are (report_golden_test.go, report_test.go's makeEntity).
func TestGenerate_SourceWindowCountsStartLineOnly(t *testing.T) {
	const (
		withStart    = 60
		withoutStart = 40
	)

	entities := make([]graph.Entity, 0, withStart+withoutStart)
	for i := 0; i < withStart; i++ {
		e := makeEntity(fmt.Sprintf("ent%013x", i), fmt.Sprintf("F%d", i), "SCOPE.Function", "go", "a.go", 10+i)
		if i%2 == 0 {
			e.EndLine = 1000 + i // half the counted entities DO carry a span
		}
		entities = append(entities, e)
	}
	for i := 0; i < withoutStart; i++ {
		e := makeEntity(fmt.Sprintf("ent%013x", 5000+i), fmt.Sprintf("G%d", i), "SCOPE.Function", "go", "b.go", 0)
		if i%2 == 0 {
			e.EndLine = 9000 + i // an end line WITHOUT a start line must not count
		}
		entities = append(entities, e)
	}

	r, err := Generate(context.Background(), []*graph.Document{makeDoc(entities, nil)}, Opts{GroupName: "g"})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if r.IsSuppressed() {
		t.Fatalf("report unexpectedly suppressed (entities=%d)", r.TotalEntities)
	}

	var sb strings.Builder
	if err := Render(&sb, r); err != nil {
		t.Fatalf("Render: %v", err)
	}
	sec := sourceWindowSection(t, sb.String())

	// 60 of 100 — reachable ONLY by counting StartLine > 0 and nothing else.
	// A check accepting StartLine >= 0 prints 100.0% (100 of 100); one
	// requiring EndLine > StartLine prints 30.0% (30 of 100).
	const want = "Entities with a start line: **60.0%** (60 of 100)"
	if !strings.Contains(sec, want) {
		t.Errorf("source-window completeness is not a pure start-line count\nwant substring: %q\nsection:\n%s", want, sec)
	}
}

// sourceWindowSection returns the text of the "### Source-window completeness"
// subsection, up to the heading that follows it.
func sourceWindowSection(t *testing.T, out string) string {
	t.Helper()
	return between(t, out, "### Source-window completeness", "### Annotation coverage")
}
