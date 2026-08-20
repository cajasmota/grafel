package dashboard

import (
	"testing"

	"github.com/cajasmota/grafel/internal/daemon"
)

// These tests used to assert the budget resolved AT CALL TIME on every
// /api/system poll. That premise is the #6323 defect: the dashboard polls every
// 5s and each poll re-read settings.json and forked sysctl. The budget is now
// resolved once per process — which is also honest, because a changed budget
// only takes effect at the next daemon start.

func TestBuildSystemReplyUsesPersistedRSSBudget(t *testing.T) {
	t.Setenv("GRAFEL_HOME", t.TempDir())
	t.Setenv("GRAFEL_MAX_RSS_BUDGET_MB", "")
	if err := daemon.PersistConfiguredRSSBudgetMB(8192); err != nil {
		t.Fatal(err)
	}
	daemon.ResetRSSBudgetCache()
	t.Cleanup(daemon.ResetRSSBudgetCache)

	reply := (&Server{}).buildSystemReply()
	if reply.RSSBudgetMb != 8192 {
		t.Fatalf("rss_budget_mb = %.0f, want 8192", reply.RSSBudgetMb)
	}
}

func TestBuildSystemReplyEnvironmentOverridesPersistedRSSBudget(t *testing.T) {
	t.Setenv("GRAFEL_HOME", t.TempDir())
	if err := daemon.PersistConfiguredRSSBudgetMB(8192); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GRAFEL_MAX_RSS_BUDGET_MB", "4096")
	daemon.ResetRSSBudgetCache()
	t.Cleanup(daemon.ResetRSSBudgetCache)

	reply := (&Server{}).buildSystemReply()
	if reply.RSSBudgetMb != 4096 {
		t.Fatalf("rss_budget_mb = %.0f, want 4096", reply.RSSBudgetMb)
	}
}

// Repeated polls must not re-resolve: a budget persisted after the first poll
// is a next-start value and must not appear in the live reply.
func TestBuildSystemReplyDoesNotReResolvePerPoll(t *testing.T) {
	t.Setenv("GRAFEL_HOME", t.TempDir())
	t.Setenv("GRAFEL_MAX_RSS_BUDGET_MB", "")
	if err := daemon.PersistConfiguredRSSBudgetMB(2048); err != nil {
		t.Fatal(err)
	}
	daemon.ResetRSSBudgetCache()
	t.Cleanup(daemon.ResetRSSBudgetCache)

	s := &Server{}
	if got := s.buildSystemReply().RSSBudgetMb; got != 2048 {
		t.Fatalf("first poll rss_budget_mb = %.0f, want 2048", got)
	}
	if err := daemon.PersistConfiguredRSSBudgetMB(8192); err != nil {
		t.Fatal(err)
	}
	if got := s.buildSystemReply().RSSBudgetMb; got != 2048 {
		t.Fatalf("second poll rss_budget_mb = %.0f, want the unchanged 2048", got)
	}
}

// #6323 (second half): the number reported is the NEXT-START budget, so the
// reply must say so rather than presenting it as the live scheduler value.
func TestBuildSystemReplyLabelsBudgetAsNextStart(t *testing.T) {
	t.Setenv("GRAFEL_HOME", t.TempDir())
	t.Setenv("GRAFEL_MAX_RSS_BUDGET_MB", "2048")
	daemon.ResetRSSBudgetCache()
	t.Cleanup(daemon.ResetRSSBudgetCache)

	if got := (&Server{}).buildSystemReply().RSSBudgetScope; got != "next_start" {
		t.Fatalf("rss_budget_scope = %q, want %q", got, "next_start")
	}
}
