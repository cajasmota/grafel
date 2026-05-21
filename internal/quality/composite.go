// Package quality — composite graph health scorer.
//
// CompositeScore combines three orthogonal signals into a single 0–100
// "graph health" number that is suitable for trending, CI gating, and
// at-a-glance dashboards.
//
// Formula (issue #1236):
//
//	health = 100 - (orphan_rate_pct * 0.3 + bug_rate_pct * 0.5 + recall_miss_pct * 0.2)
//
// All inputs are 0–100 percentages (not 0–1 ratios). The result is
// clamped to [0, 100] and rounded to one decimal place.
//
// Grade thresholds:
//
//	≥ 90  → A
//	≥ 75  → B
//	≥ 60  → C
//	≥ 45  → D
//	< 45  → F
package quality

import "math"

// CompositeResult holds the decomposed inputs and the derived composite score.
type CompositeResult struct {
	// OrphanRatePct is the percentage of entities with no inbound edges (0–100).
	OrphanRatePct float64 `json:"orphan_rate_pct"`
	// BugRatePct is the percentage of edges pointing to unresolved targets (0–100).
	BugRatePct float64 `json:"bug_rate_pct"`
	// RecallMissPct is the percentage of expected entities/relationships not
	// found by the extractor (0–100). Zero when no fixture-based measurement
	// is available for this group.
	RecallMissPct float64 `json:"recall_miss_pct"`
	// Score is the composite health score (0–100, higher is better).
	Score float64 `json:"score"`
	// Grade is the letter grade derived from Score (A–F).
	Grade string `json:"grade"`
}

// CompositeScoreFromPcts computes the composite health score given the three
// component percentages (each in [0, 100]). Inputs outside that range are
// clamped so callers don't need to guard.
func CompositeScoreFromPcts(orphanRatePct, bugRatePct, recallMissPct float64) CompositeResult {
	orphanRatePct = clamp100(orphanRatePct)
	bugRatePct = clamp100(bugRatePct)
	recallMissPct = clamp100(recallMissPct)

	raw := 100.0 - (orphanRatePct*0.3 + bugRatePct*0.5 + recallMissPct*0.2)
	score := math.Round(clamp100(raw)*10) / 10

	return CompositeResult{
		OrphanRatePct: orphanRatePct,
		BugRatePct:    bugRatePct,
		RecallMissPct: recallMissPct,
		Score:         score,
		Grade:         scoreGrade(score),
	}
}

// CompositeScoreFromRatios is a convenience wrapper for callers that work with
// 0–1 ratios (as produced by the audit and history machinery).
func CompositeScoreFromRatios(orphanRate, bugRate, recallMiss float64) CompositeResult {
	return CompositeScoreFromPcts(orphanRate*100, bugRate*100, recallMiss*100)
}

// scoreGrade maps a score to a letter grade.
func scoreGrade(score float64) string {
	switch {
	case score >= 90:
		return "A"
	case score >= 75:
		return "B"
	case score >= 60:
		return "C"
	case score >= 45:
		return "D"
	default:
		return "F"
	}
}

func clamp100(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 100 {
		return 100
	}
	return v
}
