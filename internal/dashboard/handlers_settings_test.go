package dashboard

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// setupSettingsServer returns a test server with a temp ~/.archigraph directory.
func setupSettingsServer(t *testing.T) (*Server, func()) {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("ARCHIGRAPH_HOME", tmp)
	srv, _ := NewServer(DefaultConfig(), newFakeStore())
	cleanup := func() { os.Unsetenv("ARCHIGRAPH_HOME") }
	return srv, cleanup
}

func TestHandleGetSettings_defaults(t *testing.T) {
	srv, cleanup := setupSettingsServer(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/api/settings", nil)
	w := httptest.NewRecorder()
	srv.handleGetSettings(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d; body: %s", w.Code, w.Body.String())
	}

	var reply settingsReply
	if err := json.NewDecoder(w.Body).Decode(&reply); err != nil {
		t.Fatalf("decode: %v", err)
	}
	def := DefaultAppSettings()
	if reply.Settings.Theme != def.Theme {
		t.Errorf("want theme %q, got %q", def.Theme, reply.Settings.Theme)
	}
	if reply.Settings.LogLevel != def.LogLevel {
		t.Errorf("want log_level %q, got %q", def.LogLevel, reply.Settings.LogLevel)
	}
	if reply.Settings.TelemetryEnabled != false {
		t.Errorf("want telemetry_enabled=false by default")
	}
}

func TestHandlePutSettings_validPatch(t *testing.T) {
	srv, cleanup := setupSettingsServer(t)
	defer cleanup()

	patch := map[string]any{
		"theme":     "dark",
		"log_level": "debug",
	}
	body, _ := json.Marshal(patch)
	req := httptest.NewRequest(http.MethodPut, "/api/settings", bytes.NewReader(body))
	w := httptest.NewRecorder()
	srv.handlePutSettings(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d; body: %s", w.Code, w.Body.String())
	}

	var reply settingsReply
	if err := json.NewDecoder(w.Body).Decode(&reply); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if reply.Settings.Theme != "dark" {
		t.Errorf("want theme=dark, got %q", reply.Settings.Theme)
	}
	if reply.Settings.LogLevel != "debug" {
		t.Errorf("want log_level=debug, got %q", reply.Settings.LogLevel)
	}
}

func TestHandlePutSettings_invalidTheme(t *testing.T) {
	srv, cleanup := setupSettingsServer(t)
	defer cleanup()

	patch := map[string]any{"theme": "rainbow"}
	body, _ := json.Marshal(patch)
	req := httptest.NewRequest(http.MethodPut, "/api/settings", bytes.NewReader(body))
	w := httptest.NewRecorder()
	srv.handlePutSettings(w, req)

	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("want 422, got %d; body: %s", w.Code, w.Body.String())
	}
}

func TestHandlePutSettings_rssOutOfRange(t *testing.T) {
	srv, cleanup := setupSettingsServer(t)
	defer cleanup()

	patch := map[string]any{"daemon_rss_budget_mb": 9999}
	body, _ := json.Marshal(patch)
	req := httptest.NewRequest(http.MethodPut, "/api/settings", bytes.NewReader(body))
	w := httptest.NewRecorder()
	srv.handlePutSettings(w, req)

	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("want 422, got %d; body: %s", w.Code, w.Body.String())
	}
}

func TestHandleResetSettings(t *testing.T) {
	srv, cleanup := setupSettingsServer(t)
	defer cleanup()

	// First, set a non-default value.
	patch := map[string]any{"theme": "dark", "log_level": "warn"}
	body, _ := json.Marshal(patch)
	req := httptest.NewRequest(http.MethodPut, "/api/settings", bytes.NewReader(body))
	srv.handlePutSettings(httptest.NewRecorder(), req)

	// Now reset.
	req2 := httptest.NewRequest(http.MethodPost, "/api/settings/reset", nil)
	w := httptest.NewRecorder()
	srv.handleResetSettings(w, req2)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}

	var reply settingsReply
	if err := json.NewDecoder(w.Body).Decode(&reply); err != nil {
		t.Fatalf("decode: %v", err)
	}
	def := DefaultAppSettings()
	if reply.Settings.Theme != def.Theme {
		t.Errorf("after reset: want theme %q, got %q", def.Theme, reply.Settings.Theme)
	}
}

func TestHandlePutSettings_persistsAcrossLoad(t *testing.T) {
	srv, cleanup := setupSettingsServer(t)
	defer cleanup()

	patch := map[string]any{"theme": "auto", "telemetry_enabled": true}
	body, _ := json.Marshal(patch)
	req := httptest.NewRequest(http.MethodPut, "/api/settings", bytes.NewReader(body))
	srv.handlePutSettings(httptest.NewRecorder(), req)

	// Verify the file was actually written.
	tmp := os.Getenv("ARCHIGRAPH_HOME")
	p := filepath.Join(tmp, "settings.json")
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("settings.json not written: %v", err)
	}
	var on_disk map[string]any
	if err := json.Unmarshal(b, &on_disk); err != nil {
		t.Fatalf("disk JSON invalid: %v", err)
	}
	if on_disk["theme"] != "auto" {
		t.Errorf("disk: want theme=auto, got %v", on_disk["theme"])
	}

	// Second GET should return the persisted values.
	req2 := httptest.NewRequest(http.MethodGet, "/api/settings", nil)
	w := httptest.NewRecorder()
	srv.handleGetSettings(w, req2)

	var reply settingsReply
	json.NewDecoder(w.Body).Decode(&reply)
	if reply.Settings.Theme != "auto" {
		t.Errorf("GET after PUT: want theme=auto, got %q", reply.Settings.Theme)
	}
	if !reply.Settings.TelemetryEnabled {
		t.Errorf("GET after PUT: want telemetry_enabled=true")
	}
}

func TestHandlePutSettings_restartRequired(t *testing.T) {
	srv, cleanup := setupSettingsServer(t)
	defer cleanup()

	patch := map[string]any{"daemon_rss_budget_mb": 800}
	body, _ := json.Marshal(patch)
	req := httptest.NewRequest(http.MethodPut, "/api/settings", bytes.NewReader(body))
	w := httptest.NewRecorder()
	srv.handlePutSettings(w, req)

	var reply settingsReply
	json.NewDecoder(w.Body).Decode(&reply)
	found := false
	for _, k := range reply.RestartRequired {
		if k == "daemon_rss_budget_mb" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected daemon_rss_budget_mb in restart_required, got %v", reply.RestartRequired)
	}
}
