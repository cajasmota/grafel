package daemon

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestConfiguredMemorySettings(t *testing.T) {
	home := t.TempDir()
	t.Setenv("GRAFEL_HOME", home)
	p := filepath.Join(home, "settings.json")
	if err := os.WriteFile(p, []byte(`{
  "daemon_rss_budget_mb": 8192,
  "daemon_go_memory_limit_mb": 6144,
  "unrelated": true
}`), 0o644); err != nil {
		t.Fatal(err)
	}

	if got := ConfiguredRSSBudgetMB(); got != 8192 {
		t.Fatalf("RSS budget = %d, want 8192", got)
	}
	if got := ConfiguredGoMemoryLimitMB(); got != 6144 {
		t.Fatalf("Go memory limit = %d, want 6144", got)
	}
}

func TestConfiguredGoMemoryLimitMB_AbsentOrInvalidUsesAuto(t *testing.T) {
	home := t.TempDir()
	t.Setenv("GRAFEL_HOME", home)
	p := filepath.Join(home, "settings.json")

	for _, body := range []string{
		`{"daemon_rss_budget_mb":8192}`,
		`{"daemon_go_memory_limit_mb":99}`,
		`{"daemon_go_memory_limit_mb":32769}`,
		`not-json`,
	} {
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		if got := ConfiguredGoMemoryLimitMB(); got != 0 {
			t.Fatalf("ConfiguredGoMemoryLimitMB(%q) = %d, want 0", body, got)
		}
	}
}

func TestPersistConfiguredRSSBudgetMBPreservesOtherSettings(t *testing.T) {
	home := t.TempDir()
	t.Setenv("GRAFEL_HOME", home)
	p := filepath.Join(home, "settings.json")
	if err := os.WriteFile(p, []byte(`{"theme":"dark","daemon_go_memory_limit_mb":6144}`), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := PersistConfiguredRSSBudgetMB(8192); err != nil {
		t.Fatal(err)
	}

	var got map[string]any
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if got["theme"] != "dark" || got["daemon_go_memory_limit_mb"] != float64(6144) {
		t.Fatalf("unrelated settings were not preserved: %#v", got)
	}
	if got["daemon_rss_budget_mb"] != float64(8192) {
		t.Fatalf("daemon_rss_budget_mb = %#v, want 8192", got["daemon_rss_budget_mb"])
	}
}

func TestPersistConfiguredRSSBudgetMBRejectsOutOfRange(t *testing.T) {
	t.Setenv("GRAFEL_HOME", t.TempDir())
	for _, mb := range []int64{99, 32769} {
		if err := PersistConfiguredRSSBudgetMB(mb); err == nil {
			t.Fatalf("PersistConfiguredRSSBudgetMB(%d) succeeded, want error", mb)
		}
	}
}
func TestRSSBudgetMBDefault(t *testing.T) {
	tests := []struct {
		name          string
		totalMemoryMB int64
		want          int64
	}{
		{name: "unknown memory", totalMemoryMB: 0, want: 500},
		{name: "four GiB machine", totalMemoryMB: 4096, want: 512},
		{name: "eight GiB machine", totalMemoryMB: 8192, want: 1024},
		{name: "large machine is capped", totalMemoryMB: 32768, want: 2048},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := rssBudgetMB(tt.totalMemoryMB); got != tt.want {
				t.Fatalf("rssBudgetMB(%d) = %d, want %d", tt.totalMemoryMB, got, tt.want)
			}
		})
	}
}
