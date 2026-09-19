// v2_fidelity.go — fidelity derivation for v2 group endpoints.
//
// Fidelity is the complement of bug_rate:
//
//	fidelity = round(100 - bug_rate_pct, 1) / 100   (result in [0, 1])
//
// Health bands (server-side, drives the v2 Group.health field):
//
//	fidelity >= 0.97  → healthy
//	fidelity >= 0.90  → warning
//	fidelity  < 0.90  → degraded
//
// The most-recent bug_rate is read from the quality health-history JSONL
// file (~/.grafel/health-history.jsonl) by latestGroupBugRate.
// When no history exists for a group the callers fall back to their previous
// logic (indexed → 1.0/healthy).

package dashboard

import (
	"math"

	"github.com/cajasmota/grafel/internal/quality"
)

const healthDegraded = "degraded"

// fidelityFromBugRate converts a bug_rate percentage (0–100) to a 0–1
// fidelity score, rounded to three decimal places (0.001 resolution).
func fidelityFromBugRate(bugRatePct float64) float64 {
	raw := 100.0 - bugRatePct
	if raw < 0 {
		raw = 0
	}
	if raw > 100 {
		raw = 100
	}
	// Round to 3 decimal places on the 0-1 ratio to avoid IEEE-754 drift.
	return math.Round(raw*10) / 1000
}

// deriveHealthFromFidelity maps a 0–1 fidelity score to a health label.
// It returns (fidelity, health) where fidelity is the clamped input.
func deriveHealthFromFidelity(fidelity float64) (float64, string) {
	switch {
	case fidelity >= 0.97:
		return fidelity, healthHealthy
	case fidelity >= 0.90:
		return fidelity, healthWarning
	default:
		return fidelity, healthDegraded
	}
}

// latestGroupBugRate reads the quality health-history JSONL stored under
// root (e.g. ~/.grafel) and returns the bug_rate of the most-recent
// HealthEntry for groupName. Returns (0, false) when no entry exists.
//
// Uses quality.ReadHistory with a generous 3650-day window so all history
// is considered; we only need the last entry.
func latestGroupBugRate(groupName, root string) (bugRatePct float64, ok bool) {
	entries, err := quality.ReadHistory(root, groupName, 3650)
	if err != nil || len(entries) == 0 {
		return 0, false
	}
	// ReadHistory returns entries in file order (oldest first).
	// The last entry is the most recent.
	last := entries[len(entries)-1]
	// #7283 — an entry written when nothing could be measured carries no
	// bug rate. Reporting it as 0 here would render the group at fidelity
	// 1.0 / healthy off a measurement that never happened.
	//
	// NOTE: the callers' no-history fallback is ALSO "indexed → 1.0 /
	// healthy", so this returns the lie to where it already lived rather
	// than removing it. See the comment above deriveGroupHealth.
	if last.BugRate == nil {
		return 0, false
	}
	return *last.BugRate, true
}
