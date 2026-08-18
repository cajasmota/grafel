package dashboard

import (
	"testing"

	"github.com/cajasmota/grafel/internal/daemon"
)

func TestBuildSystemReplyUsesPersistedRSSBudget(t *testing.T) {
	t.Setenv("GRAFEL_HOME", t.TempDir())
	t.Setenv("GRAFEL_MAX_RSS_BUDGET_MB", "")
	if err := daemon.PersistConfiguredRSSBudgetMB(8192); err != nil {
		t.Fatal(err)
	}

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

	reply := (&Server{}).buildSystemReply()
	if reply.RSSBudgetMb != 4096 {
		t.Fatalf("rss_budget_mb = %.0f, want 4096", reply.RSSBudgetMb)
	}
}
