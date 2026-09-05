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
// `EndLine > StartLine` scored 0.0% against real production data), so the
// LABEL is the defect, not the check.
//
// The measurement keeping it one-sided is TestGenerate_GoldenFB: a POST-#6236
// v6 graph.fb round-tripped by the current writer still carries 0 end lines
// across 672 entities. Its span-less assertion was the ONLY guard enforcing
// that shape — report_test.go's makeEntity merely DESCRIBED it, and setting an
// EndLine in that literal left the package green. TestMakeEntity_IsSpanLess
// below closes that hole, so there are now two guards rather than one claimed
// and one real.
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
	// The denominator's unit. Pinned as text HERE and as an observed count in
	// TestGenerate_SourceWindowDenominatorCountsOccurrences, because either
	// half alone is ungraded in one direction: inverting only the prose leaves
	// a false caption over a correct number, and changing only the counter
	// leaves a true caption over a wrong one.
	sourceWindowUnitClause = "Counts entity OCCURRENCES (one entity emitted into two documents counts twice)"
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
	if !strings.Contains(sec, sourceWindowUnitClause) {
		t.Errorf("source-window section does not name its denominator's unit\nwant substring: %q\nsection:\n%s", sourceWindowUnitClause, sec)
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
// same defect wearing new prose. See sourceWindowFixtureEntities for why the
// expected value is reachable only by ignoring EndLine entirely.
func TestGenerate_SourceWindowCountsStartLineOnly(t *testing.T) {
	r, err := Generate(context.Background(), []*graph.Document{makeDoc(sourceWindowFixtureEntities(), nil)}, Opts{GroupName: "g"})
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

// sourceWindowFixtureEntities builds 60 entities with a start line and 40
// without, varying EndLine on BOTH sides of that split — present and absent
// among the counted, present and absent among the uncounted — so any expected
// value derived from it is reachable only by ignoring EndLine entirely.
//
// It is deliberately NOT span-less and does not replace the guards that are
// (TestGenerate_GoldenFB, TestMakeEntity_IsSpanLess): those pin the shape the
// production writer emits, this one pins what the counter does when spans are
// present.
func sourceWindowFixtureEntities() []graph.Entity {
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
	return entities
}

// TestMakeEntity_IsSpanLess makes report_test.go's makeEntity contract REAL.
//
// That helper's comment has always said it withholds EndLine — pre-setting one
// is named there as "the fixture lie" that let the original source-window bug
// pass unit tests while scoring 0.0% in production — but nothing enforced it:
// adding `EndLine: startLine + 7` to the literal left the entire package green.
// The unit-level defence of this metric rested on a struct field being
// incidentally zero, and only the golden test would have caught a regression
// paired with the lie being restored.
func TestMakeEntity_IsSpanLess(t *testing.T) {
	e := makeEntity("ent0000000000001", "F", "SCOPE.Function", "go", "a.go", 12)
	if e.EndLine != 0 {
		t.Errorf("makeEntity returned EndLine=%d, want 0: this helper must reproduce the span-less shape the current writer actually produces (0 of 672 entities in testdata/golden carry an end line). Setting EndLine here is the fixture lie #5683 was caused by — a source-window regression would then be invisible to every unit test in this package", e.EndLine)
	}
}

// TestGenerate_SourceWindowDenominatorCountsOccurrences grades the OTHER half
// of the caption. The caption asserts the denominator counts entity
// OCCURRENCES and not unique IDs; rewriting that clause to its exact opposite
// left the package green, so the claim was true but ungraded — the same
// untested-prose shape #6827 is about.
//
// The same document is submitted twice: an occurrence denominator doubles both
// columns, a unique-ID denominator leaves them alone.
func TestGenerate_SourceWindowDenominatorCountsOccurrences(t *testing.T) {
	doc := makeDoc(sourceWindowFixtureEntities(), nil)

	r, err := Generate(context.Background(), []*graph.Document{doc, doc}, Opts{GroupName: "g"})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	var sb strings.Builder
	if err := Render(&sb, r); err != nil {
		t.Fatalf("Render: %v", err)
	}
	sec := sourceWindowSection(t, sb.String())

	// A unique-ID denominator would print "(60 of 100)" here — the same
	// entities, emitted twice.
	const want = "Entities with a start line: **60.0%** (120 of 200)"
	if !strings.Contains(sec, want) {
		t.Errorf("source-window denominator does not count entity occurrences, as its caption claims\nwant substring: %q\nsection:\n%s", want, sec)
	}
}

// sourceWindowSection returns the text of the "### Source-window completeness"
// subsection, up to the heading that follows it.
func sourceWindowSection(t *testing.T, out string) string {
	t.Helper()
	return between(t, out, "### Source-window completeness", "### Annotation coverage")
}
