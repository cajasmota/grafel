package dashboard

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cajasmota/grafel/internal/daemon"
)

// #6322: DefaultAppSettings hardcoded 512 while Operations resolved the real
// budget via daemon.RSSBudgetMB(). DefaultAppSettings is the merge base of
// loadSettings(), and PUT /api/settings decodes onto it, so submitting the
// settings form untouched quartered the budget on a 16GiB host.
func TestDefaultAppSettingsBudgetComesFromTheResolver(t *testing.T) {
	t.Setenv("GRAFEL_HOME", t.TempDir())
	t.Setenv("GRAFEL_MAX_RSS_BUDGET_MB", "")
	if err := daemon.PersistConfiguredRSSBudgetMB(8192); err != nil {
		t.Fatal(err)
	}
	daemon.ResetRSSBudgetCache()
	t.Cleanup(daemon.ResetRSSBudgetCache)

	if got := DefaultAppSettings().DaemonRSSBudgetMB; got != 8192 {
		t.Fatalf("DefaultAppSettings().DaemonRSSBudgetMB = %d, want 8192", got)
	}
}

// The default must be the SAME number Operations reports — one source, and in
// particular never the old hardcoded 512 on a host whose budget is not 512.
func TestDefaultAppSettingsBudgetMatchesSystemReply(t *testing.T) {
	t.Setenv("GRAFEL_HOME", t.TempDir())
	t.Setenv("GRAFEL_MAX_RSS_BUDGET_MB", "2048")
	daemon.ResetRSSBudgetCache()
	t.Cleanup(daemon.ResetRSSBudgetCache)

	def := DefaultAppSettings().DaemonRSSBudgetMB
	live := (&Server{}).buildSystemReply().RSSBudgetMb
	if float64(def) != live {
		t.Fatalf("settings default %d disagrees with /api/system %.0f", def, live)
	}
	if def == 512 {
		t.Fatalf("settings default is still the hardcoded 512")
	}
}

// A settings.json that omits the budget must still merge to the resolved
// budget, not to 512 — this is the save-quarters-the-budget path.
func TestLoadSettingsKeepsResolvedBudgetWhenFileOmitsIt(t *testing.T) {
	home := t.TempDir()
	t.Setenv("GRAFEL_HOME", home)
	t.Setenv("GRAFEL_MAX_RSS_BUDGET_MB", "2048")
	daemon.ResetRSSBudgetCache()
	t.Cleanup(daemon.ResetRSSBudgetCache)

	if err := os.WriteFile(filepath.Join(home, "settings.json"), []byte(`{"theme":"dark"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := loadSettings()
	if err != nil {
		t.Fatal(err)
	}
	if got.DaemonRSSBudgetMB != 2048 {
		t.Fatalf("loadSettings().DaemonRSSBudgetMB = %d, want 2048", got.DaemonRSSBudgetMB)
	}
}
