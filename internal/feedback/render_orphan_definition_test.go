package feedback

import (
	"strings"
	"testing"
)

// TestRender_OrphanDefinitionSentenceMatchesTheMetric pins the legend in the
// shipped report that tells a human what the orphan number underneath it means
// (#6346 review C1, extended by review item 5).
//
// It read "no outgoing semantic edges" while the metric became direction-aware,
// which made every shipped report's own legend false. #6313 was found by a
// reporter reading that exact table by hand, so this string is load-bearing:
// nothing else in the report explains the number.
//
// Every clause of the legend is pinned, not just the first sentence. The clause
// pointing at the Expected/terminal-orphans table was added in a later revision
// WITHOUT extending this test, and deleting it left the package green — the
// same "stale legend ships unnoticed" defect this test exists to prevent, one
// revision after it was written.
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

	// Clause 1 — what "orphan" means. Must match the direction-aware metric.
	const wantDefinition = "An entity is orphan when it has no semantic edge in EITHER direction (CONTAINS/DECLARES excluded, in both directions)."
	if !strings.Contains(out, wantDefinition) {
		t.Errorf("orphan-rate legend: definition clause missing or stale.\nwant substring: %q", wantDefinition)
	}

	// Clause 2 — which BUCKET the table beneath it counts. Without this, a
	// reader sees `Route: Orphan 0` in the first table and `172/172` in the
	// Expected/terminal-orphans table below and has no way to reconcile them.
	const wantBucketClause = "The table below counts only orphans of kinds that carry a semantic edge SOMEWHERE in this group; kinds where no entity does are listed separately under **Expected/terminal orphans**, so a kind reading 0 here may still be entirely unwired there."
	if !strings.Contains(out, wantBucketClause) {
		t.Errorf("orphan-rate legend: bucket clause missing or stale — the defect table and the terminal table become irreconcilable without it.\nwant substring: %q", wantBucketClause)
	}

	// The pre-#6313 wording must not survive anywhere in the rendered report:
	// it describes a metric this package no longer computes.
	if strings.Contains(out, "no outgoing semantic edges") {
		t.Error("rendered report still claims orphan means \"no outgoing semantic edges\" — false for every report since the direction fix")
	}
}
