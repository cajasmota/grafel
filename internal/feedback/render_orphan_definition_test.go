package feedback

import (
	"strings"
	"testing"
)

// TestRender_OrphanDefinitionSentenceMatchesTheMetric pins the one sentence in
// the shipped report that tells a human what the orphan number underneath it
// means (#6346 review C1).
//
// It read "no outgoing semantic edges" while the metric became direction-aware,
// which made every shipped report's own legend false. #6313 was found by a
// reporter reading that exact table by hand, so this string is load-bearing:
// nothing else in the report explains the number.
func TestRender_OrphanDefinitionSentenceMatchesTheMetric(t *testing.T) {
	r := &Report{
		TotalEntities: 100,
		GeneratedAt:   mustParseTime("2026-05-27T00:00:00Z"),
		GroupName:     "test-group",
		OrphanByKind:  map[string]KindStats{"SCOPE.Route": {Total: 20, OrphanCount: 4, OrphanPct: 20.0}},
		FrameworkHits: map[string]int{},
	}
	var sb strings.Builder
	if err := Render(&sb, r); err != nil {
		t.Fatalf("Render: %v", err)
	}
	out := sb.String()

	const want = "An entity is orphan when it has no semantic edge in EITHER direction (CONTAINS/DECLARES excluded, in both directions)."
	if !strings.Contains(out, want) {
		t.Errorf("orphan-rate legend missing or stale.\nwant substring: %q", want)
	}
	// The pre-#6313 wording must not survive anywhere in the rendered report:
	// it describes a metric this package no longer computes.
	if strings.Contains(out, "no outgoing semantic edges") {
		t.Error("rendered report still claims orphan means \"no outgoing semantic edges\" — false for every report since the direction fix")
	}
}
