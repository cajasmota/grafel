package daemon

import "testing"

// #6323: the dashboard polls /api/system every 5s and each poll resolved the
// budget from scratch — re-reading settings.json and forking sysctl. The budget
// only changes across a daemon restart, so it must resolve once per process.
func TestRSSBudgetMBResolvesHostMemoryOnce(t *testing.T) {
	t.Setenv("GRAFEL_HOME", t.TempDir()) // no settings.json -> memory default path
	t.Setenv("GRAFEL_MAX_RSS_BUDGET_MB", "")

	calls := 0
	orig := totalMemoryMB
	totalMemoryMB = func() int64 { calls++; return 16384 }
	t.Cleanup(func() { totalMemoryMB = orig; ResetRSSBudgetCache() })
	ResetRSSBudgetCache()

	first := RSSBudgetMB()
	for i := 0; i < 4; i++ {
		if got := RSSBudgetMB(); got != first {
			t.Fatalf("RSSBudgetMB() = %d on call %d, want stable %d", got, i+2, first)
		}
	}
	if calls != 1 {
		t.Fatalf("host memory probed %d times across 5 calls, want 1", calls)
	}
	if first != 2048 {
		t.Fatalf("RSSBudgetMB() = %d, want 2048 on a 16GiB host", first)
	}
}

// A budget persisted after the process resolved it must NOT change the live
// value: the change takes effect at the next daemon start.
func TestRSSBudgetMBIgnoresLaterPersistUntilRestart(t *testing.T) {
	t.Setenv("GRAFEL_HOME", t.TempDir())
	t.Setenv("GRAFEL_MAX_RSS_BUDGET_MB", "")
	orig := totalMemoryMB
	totalMemoryMB = func() int64 { return 16384 }
	t.Cleanup(func() { totalMemoryMB = orig; ResetRSSBudgetCache() })
	ResetRSSBudgetCache()

	first := RSSBudgetMB()
	if err := PersistConfiguredRSSBudgetMB(8192); err != nil {
		t.Fatal(err)
	}
	if got := RSSBudgetMB(); got != first {
		t.Fatalf("RSSBudgetMB() = %d after persist, want the resolved-once %d", got, first)
	}
	ResetRSSBudgetCache()
	if got := RSSBudgetMB(); got != 8192 {
		t.Fatalf("RSSBudgetMB() = %d after restart, want 8192", got)
	}
}

// The GRAFEL_MAX_RSS_BUDGET_MB override belongs in the one resolver, so every
// caller (Operations, Settings defaults, daemon startup) agrees.
func TestRSSBudgetMBEnvOverridesPersisted(t *testing.T) {
	t.Setenv("GRAFEL_HOME", t.TempDir())
	if err := PersistConfiguredRSSBudgetMB(8192); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GRAFEL_MAX_RSS_BUDGET_MB", "4096")
	ResetRSSBudgetCache()
	t.Cleanup(ResetRSSBudgetCache)

	if got := RSSBudgetMB(); got != 4096 {
		t.Fatalf("RSSBudgetMB() = %d, want the env override 4096", got)
	}
}
