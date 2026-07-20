package daemon

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/cajasmota/grafel/internal/registry"
)

const (
	MinConfiguredRSSBudgetMB     = 100
	MaxConfiguredRSSBudgetMB     = 32768
	MinConfiguredGoMemoryLimitMB = 100
	MaxConfiguredGoMemoryLimitMB = 32768
)

type daemonMemorySettings struct {
	DaemonRSSBudgetMB     int64 `json:"daemon_rss_budget_mb"`
	DaemonGoMemoryLimitMB int64 `json:"daemon_go_memory_limit_mb"`
}

func settingsJSONPath() (string, error) {
	home, err := registry.HomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "settings.json"), nil
}

// ConfiguredRSSBudgetMB reads daemon_rss_budget_mb from settings.json.
// It returns 0 when no valid configured value exists.
func ConfiguredRSSBudgetMB() int64 {
	raw, ok := configuredMemorySettings()
	if !ok {
		return 0
	}
	if raw.DaemonRSSBudgetMB < MinConfiguredRSSBudgetMB || raw.DaemonRSSBudgetMB > MaxConfiguredRSSBudgetMB {
		return 0
	}
	return raw.DaemonRSSBudgetMB
}

// ConfiguredGoMemoryLimitMB reads daemon_go_memory_limit_mb from settings.json.
// It returns 0 when the key is absent or invalid, allowing the upstream
// fraction-of-RAM default to remain authoritative.
func ConfiguredGoMemoryLimitMB() int64 {
	raw, ok := configuredMemorySettings()
	if !ok {
		return 0
	}
	if raw.DaemonGoMemoryLimitMB < MinConfiguredGoMemoryLimitMB || raw.DaemonGoMemoryLimitMB > MaxConfiguredGoMemoryLimitMB {
		return 0
	}
	return raw.DaemonGoMemoryLimitMB
}

func configuredMemorySettings() (daemonMemorySettings, bool) {
	var raw daemonMemorySettings
	p, err := settingsJSONPath()
	if err != nil {
		return raw, false
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return raw, false
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return raw, false
	}
	return raw, true
}

// PersistConfiguredRSSBudgetMB writes daemon_rss_budget_mb into settings.json
// while preserving unrelated settings keys.
func PersistConfiguredRSSBudgetMB(mb int64) error {
	if mb < MinConfiguredRSSBudgetMB || mb > MaxConfiguredRSSBudgetMB {
		return fmt.Errorf("daemon_rss_budget_mb must be %d-%d; got %d",
			MinConfiguredRSSBudgetMB, MaxConfiguredRSSBudgetMB, mb)
	}
	p, err := settingsJSONPath()
	if err != nil {
		return err
	}
	settings := map[string]any{}
	if b, err := os.ReadFile(p); err == nil && len(b) > 0 {
		if err := json.Unmarshal(b, &settings); err != nil {
			return fmt.Errorf("settings.json: %w", err)
		}
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("settings.json: %w", err)
	}
	settings["daemon_rss_budget_mb"] = mb
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(p), ".settings.json.tmp-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	if _, err := f.Write(b); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := renameReplace(tmp, p); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

func renameReplace(src, dst string) error {
	const (
		attempts  = 20
		pollEvery = 5 * time.Millisecond
	)
	var err error
	for i := 0; i < attempts; i++ {
		if err = os.Rename(src, dst); err == nil {
			return nil
		}
		time.Sleep(pollEvery)
	}
	return err
}
