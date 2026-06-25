package sched

import (
	"runtime"
	"testing"
)

// TestReindexCPUBudgetDefaultAndEnv verifies the daemon-wide reindex CPU budget
// resolver (#5602): default is ~half the host cores (floored at 1), overridable
// by a strictly-positive GRAFEL_REINDEX_CPU_BUDGET, with invalid values falling
// back to the default. No real CPU load is spawned.
func TestReindexCPUBudgetDefaultAndEnv(t *testing.T) {
	wantDefault := runtime.NumCPU() / 2
	if wantDefault < 1 {
		wantDefault = 1
	}

	t.Setenv(ReindexBudgetEnv, "")
	if got := reindexCPUBudget(); got != wantDefault {
		t.Fatalf("default budget = %d, want %d (host=%d)", got, wantDefault, runtime.NumCPU())
	}
	t.Setenv(ReindexBudgetEnv, "5")
	if got := reindexCPUBudget(); got != 5 {
		t.Fatalf("env override budget = %d, want 5", got)
	}
	for _, bad := range []string{"0", "-3", "garbage", "  "} {
		t.Setenv(ReindexBudgetEnv, bad)
		if got := reindexCPUBudget(); got != wantDefault {
			t.Fatalf("invalid budget %q = %d, want default %d", bad, got, wantDefault)
		}
	}
}

// TestReindexConcurrencyMirror verifies the sched-side mirror of the daemon
// IndexGate cap reads the SAME GRAFEL_INDEX_CONCURRENCY env, defaulting to 2.
// This is the divisor the budget is split across, so it must track the gate.
func TestReindexConcurrencyMirror(t *testing.T) {
	t.Setenv("GRAFEL_INDEX_CONCURRENCY", "")
	if got := reindexConcurrency(); got != 2 {
		t.Fatalf("default concurrency = %d, want 2", got)
	}
	t.Setenv("GRAFEL_INDEX_CONCURRENCY", "4")
	if got := reindexConcurrency(); got != 4 {
		t.Fatalf("env concurrency = %d, want 4", got)
	}
	for _, bad := range []string{"0", "-1", "garbage"} {
		t.Setenv("GRAFEL_INDEX_CONCURRENCY", bad)
		if got := reindexConcurrency(); got != 2 {
			t.Fatalf("invalid concurrency %q = %d, want default 2", bad, got)
		}
	}
}

// TestReindexGraphPhaseGOMAXPROCSIsDaemonWideCeiling is the core #5602 proof:
// the per-child graph-phase GOMAXPROCS is budget/concurrency, so the SUM across
// all concurrent reindexes (perChild × concurrency) never exceeds the single
// daemon-wide budget — the property the per-subprocess cap lacked. Driven purely
// by env knobs; no real CPU is consumed.
func TestReindexGraphPhaseGOMAXPROCSIsDaemonWideCeiling(t *testing.T) {
	cases := []struct {
		name        string
		budget      string
		concurrency string
		wantPerCT   int
	}{
		// budget 6, 2 concurrent slots → 3 each, total 6 ≤ budget.
		{"12core_default_cap2", "6", "2", 3},
		// budget 6, 3 slots → 2 each, total 6 ≤ budget.
		{"budget6_conc3", "6", "3", 2},
		// Single-group reindex (1 slot) gets the whole budget — no throughput cliff.
		{"single_group_gets_budget", "6", "1", 6},
		// Budget smaller than concurrency → floored at 1 per child (never 0).
		{"budget_below_conc_floors_at_1", "1", "4", 1},
		// Tight 2-core host: budget 1, cap 2 → 1 each (floored), total 2.
		{"tiny_host", "1", "2", 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(ReindexBudgetEnv, tc.budget)
			t.Setenv("GRAFEL_INDEX_CONCURRENCY", tc.concurrency)

			perChild := ReindexGraphPhaseGOMAXPROCS()
			if perChild != tc.wantPerCT {
				t.Fatalf("per-child GOMAXPROCS = %d, want %d (budget=%s conc=%s)",
					perChild, tc.wantPerCT, tc.budget, tc.concurrency)
			}
			if perChild < 1 {
				t.Fatalf("per-child GOMAXPROCS must be >= 1, got %d", perChild)
			}

			// Daemon-wide ceiling: the SUM of the per-child graph-phase cores
			// across every concurrent reindex the IndexGate could admit must not
			// exceed the budget (except the unavoidable floor-at-1 case, which is
			// the minimum the runtime can run on). This is the property the old
			// per-subprocess cap did NOT guarantee — it granted each child the
			// full host cores independently.
			conc := reindexConcurrency()
			budget := reindexCPUBudget()
			total := perChild * conc
			if perChild > 1 && total > budget {
				t.Fatalf("total graph-phase cores across %d concurrent reindexes = %d, "+
					"exceeds daemon-wide budget %d", conc, total, budget)
			}
		})
	}
}

// TestTwoConcurrentReindexesShareBudget asserts the headline #5602 fix: two
// concurrent group reindexes SHARE the daemon-wide budget rather than each
// getting full parallelism. Before the fix each child ran at host cores
// (concurrency × hostCores total); after it, the two children's graph-phase
// GOMAXPROCS sum to the single budget.
func TestTwoConcurrentReindexesShareBudget(t *testing.T) {
	t.Setenv(ReindexBudgetEnv, "8")
	t.Setenv("GRAFEL_INDEX_CONCURRENCY", "2")

	// Each concurrent reindex resolves the same per-child share.
	childA := ReindexGraphPhaseGOMAXPROCS()
	childB := ReindexGraphPhaseGOMAXPROCS()
	if childA != childB {
		t.Fatalf("concurrent reindexes resolved different per-child caps: %d vs %d", childA, childB)
	}
	if got, want := childA+childB, 8; got != want {
		t.Fatalf("two concurrent reindexes draw %d cores total, want budget %d", got, want)
	}
}
