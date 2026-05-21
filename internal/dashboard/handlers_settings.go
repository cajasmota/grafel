package dashboard

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sync"

	"github.com/cajasmota/archigraph/internal/registry"
)

// AppSettings is the on-disk shape of ~/.archigraph/settings.json.
// It owns all user preferences that survive daemon restarts.
// Zero-value fields fall back to the defaults returned by DefaultAppSettings().
type AppSettings struct {
	// General
	Theme        string `json:"theme"`         // "light" | "dark" | "auto"
	DefaultGroup string `json:"default_group"` // slug of the group shown on first load

	// Updates
	AutoCheckUpdates bool   `json:"auto_check_updates"`
	UpdateChannel    string `json:"update_channel"` // "stable" | "dev"
	RefreshSchedule  string `json:"refresh_schedule"` // cron-style or "" for manual

	// Telemetry
	TelemetryEnabled bool `json:"telemetry_enabled"` // default false

	// Performance — changing these requires a daemon restart
	DaemonRSSBudgetMB    int `json:"daemon_rss_budget_mb"`   // 100–2000
	WatcherDebounceSecs  int `json:"watcher_debounce_secs"`  // 1–60
	IndexerParallelism   int `json:"indexer_parallelism"`    // 1–32

	// Logs
	LogLevel string `json:"log_level"` // "debug" | "info" | "warn" | "error"
}

// DefaultAppSettings returns the canonical defaults. Any field not supplied
// by the user's settings.json is filled from here.
func DefaultAppSettings() AppSettings {
	return AppSettings{
		Theme:             "light",
		DefaultGroup:      "",
		AutoCheckUpdates:  true,
		UpdateChannel:     "stable",
		RefreshSchedule:   "",
		TelemetryEnabled:  false,
		DaemonRSSBudgetMB: 512,
		WatcherDebounceSecs: 2,
		IndexerParallelism:  4,
		LogLevel:          "info",
	}
}

// settingsPath returns ~/.archigraph/settings.json.
func settingsPath() (string, error) {
	h, err := registry.HomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(h, "settings.json"), nil
}

// settingsMu serialises concurrent GET/PUT on the settings file.
var settingsMu sync.Mutex

// loadSettings reads settings.json, merging onto defaults.
func loadSettings() (AppSettings, error) {
	settingsMu.Lock()
	defer settingsMu.Unlock()

	out := DefaultAppSettings()
	p, err := settingsPath()
	if err != nil {
		return out, err
	}
	b, err := os.ReadFile(p)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return out, nil
		}
		return out, fmt.Errorf("settings.json: %w", err)
	}
	// Unmarshal onto defaults so missing keys keep their defaults.
	if err := json.Unmarshal(b, &out); err != nil {
		return out, fmt.Errorf("settings.json: %w", err)
	}
	return out, nil
}

// saveSettings atomically writes s to settings.json.
func saveSettings(s AppSettings) error {
	settingsMu.Lock()
	defer settingsMu.Unlock()

	p, err := settingsPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	// Write to a temp file then rename for atomicity.
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

// validateSettings checks range constraints and enum values.
func validateSettings(s AppSettings) error {
	switch s.Theme {
	case "light", "dark", "auto":
	default:
		return fmt.Errorf("theme must be light, dark, or auto; got %q", s.Theme)
	}
	switch s.UpdateChannel {
	case "stable", "dev":
	default:
		return fmt.Errorf("update_channel must be stable or dev; got %q", s.UpdateChannel)
	}
	switch s.LogLevel {
	case "debug", "info", "warn", "error":
	default:
		return fmt.Errorf("log_level must be debug, info, warn, or error; got %q", s.LogLevel)
	}
	if s.DaemonRSSBudgetMB < 100 || s.DaemonRSSBudgetMB > 2000 {
		return fmt.Errorf("daemon_rss_budget_mb must be 100–2000; got %d", s.DaemonRSSBudgetMB)
	}
	if s.WatcherDebounceSecs < 1 || s.WatcherDebounceSecs > 60 {
		return fmt.Errorf("watcher_debounce_secs must be 1–60; got %d", s.WatcherDebounceSecs)
	}
	if s.IndexerParallelism < 1 || s.IndexerParallelism > 32 {
		return fmt.Errorf("indexer_parallelism must be 1–32; got %d", s.IndexerParallelism)
	}
	return nil
}

// settingsReply wraps AppSettings with computed metadata the frontend uses.
type settingsReply struct {
	Settings AppSettings `json:"settings"`
	Defaults AppSettings `json:"defaults"`
	// restart_required lists the keys whose new value differs from the
	// current persisted value AND that require a daemon restart to take effect.
	RestartRequired []string `json:"restart_required,omitempty"`
}

// handleGetSettings — GET /api/settings
func (s *Server) handleGetSettings(w http.ResponseWriter, _ *http.Request) {
	settings, err := loadSettings()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, settingsReply{
		Settings: settings,
		Defaults: DefaultAppSettings(),
	})
}

// handlePutSettings — PUT /api/settings
// Accepts a full or partial AppSettings body. Missing fields are left
// unchanged (client must GET first to build a complete payload for a
// full overwrite, or only send changed keys).
func (s *Server) handlePutSettings(w http.ResponseWriter, r *http.Request) {
	// Load current as base so a partial PUT preserves existing values.
	current, err := loadSettings()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load current settings: "+err.Error())
		return
	}

	// Decode the incoming patch onto current.
	if err := json.NewDecoder(r.Body).Decode(&current); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}

	// Validate ranges/enums.
	if err := validateSettings(current); err != nil {
		writeErr(w, http.StatusUnprocessableEntity, err.Error())
		return
	}

	// Detect which restart-required fields changed from what's on disk.
	previous, _ := loadSettings()
	var restartKeys []string
	if current.DaemonRSSBudgetMB != previous.DaemonRSSBudgetMB {
		restartKeys = append(restartKeys, "daemon_rss_budget_mb")
	}
	if current.IndexerParallelism != previous.IndexerParallelism {
		restartKeys = append(restartKeys, "indexer_parallelism")
	}

	if err := saveSettings(current); err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to save settings: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, settingsReply{
		Settings:        current,
		Defaults:        DefaultAppSettings(),
		RestartRequired: restartKeys,
	})
}

// handleResetSettings — POST /api/settings/reset
// Overwrites settings.json with factory defaults.
func (s *Server) handleResetSettings(w http.ResponseWriter, _ *http.Request) {
	d := DefaultAppSettings()
	if err := saveSettings(d); err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to reset settings: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, settingsReply{
		Settings: d,
		Defaults: d,
	})
}
