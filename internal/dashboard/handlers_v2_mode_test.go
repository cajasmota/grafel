// handlers_v2_mode_test.go — unit tests for GET/POST /api/v2/daemon/mode (S7a #2169).
package dashboard

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/cajasmota/archigraph/internal/daemon/mode"
)

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

// newModeTestServer creates a test server backed by a temp ARCHIGRAPH_HOME.
// Returns the server URL and the daemon-root directory (for config assertions).
func newModeTestServer(t *testing.T) (serverURL, daemonRoot string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("ARCHIGRAPH_HOME", home)

	daemonRoot = filepath.Join(home, "daemon")
	if err := os.MkdirAll(daemonRoot, 0o700); err != nil {
		t.Fatalf("mkdir daemonRoot: %v", err)
	}

	st := newFakeStore()
	srv, err := NewServer(DefaultConfig(), st)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	// Override historyRoot so daemonRoot() returns the temp dir.
	srv.historyRoot = daemonRoot

	ts := httptest.NewServer(srv.routes())
	t.Cleanup(ts.Close)
	return ts.URL, daemonRoot
}

// ---------------------------------------------------------------------------
// GET /api/v2/daemon/mode
// ---------------------------------------------------------------------------

// TestHandleV2GetDaemonMode_noConfig verifies the default response when no
// daemon.config.json exists: effective_mode should be "background".
func TestHandleV2GetDaemonMode_noConfig(t *testing.T) {
	url, _ := newModeTestServer(t)
	resp, err := http.Get(url + "/api/v2/daemon/mode")
	if err != nil {
		t.Fatalf("GET /api/v2/daemon/mode: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var env v2Envelope
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !env.OK {
		t.Fatal("ok=false, want true")
	}

	data, ok := env.Data.(map[string]any)
	if !ok {
		t.Fatalf("data type = %T, want map", env.Data)
	}
	effectiveMode, _ := data["effective_mode"].(string)
	if effectiveMode != "background" {
		t.Errorf("effective_mode = %q, want 'background'", effectiveMode)
	}

	// all_modes should contain all three choices
	allModes, _ := data["all_modes"].([]any)
	if len(allModes) != 3 {
		t.Errorf("len(all_modes) = %d, want 3", len(allModes))
	}
}

// TestHandleV2GetDaemonMode_withConfig verifies that a pre-written
// daemon.config.json is reflected in the response.
func TestHandleV2GetDaemonMode_withConfig(t *testing.T) {
	url, daemonRoot := newModeTestServer(t)

	// Pre-write a config that sets workstation mode.
	cfgPath := filepath.Join(daemonRoot, "daemon.config.json")
	if err := mode.SaveConfig(cfgPath, mode.Config{Mode: mode.Workstation}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	resp, err := http.Get(url + "/api/v2/daemon/mode")
	if err != nil {
		t.Fatalf("GET /api/v2/daemon/mode: %v", err)
	}
	defer resp.Body.Close()

	var env v2Envelope
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	data, _ := env.Data.(map[string]any)
	if data["effective_mode"] != "workstation" {
		t.Errorf("effective_mode = %v, want 'workstation'", data["effective_mode"])
	}
	if data["mode"] != "workstation" {
		t.Errorf("mode = %v, want 'workstation'", data["mode"])
	}
}

// ---------------------------------------------------------------------------
// POST /api/v2/daemon/mode
// ---------------------------------------------------------------------------

// TestHandleV2SetDaemonMode_validMode verifies a valid POST writes config and
// returns the expected reply shape.
func TestHandleV2SetDaemonMode_validMode(t *testing.T) {
	url, daemonRoot := newModeTestServer(t)

	body, _ := json.Marshal(map[string]string{"mode": "readonly"})
	resp, err := http.Post(url+"/api/v2/daemon/mode", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST /api/v2/daemon/mode: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var env v2Envelope
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !env.OK {
		t.Fatal("ok=false, want true")
	}

	data, _ := env.Data.(map[string]any)
	if data["mode"] != "readonly" {
		t.Errorf("reply mode = %v, want 'readonly'", data["mode"])
	}

	// Verify config was persisted on disk.
	cfgPath := filepath.Join(daemonRoot, "daemon.config.json")
	cfg, err := mode.LoadConfig(cfgPath)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.Mode != mode.Readonly {
		t.Errorf("on-disk mode = %q, want 'readonly'", cfg.Mode)
	}
}

// TestHandleV2SetDaemonMode_invalidMode verifies that an unknown mode returns
// 400 with an error envelope.
func TestHandleV2SetDaemonMode_invalidMode(t *testing.T) {
	url, _ := newModeTestServer(t)

	body, _ := json.Marshal(map[string]string{"mode": "turbo"})
	resp, err := http.Post(url+"/api/v2/daemon/mode", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST /api/v2/daemon/mode: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}

	var env v2ErrEnvelope
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if env.OK {
		t.Error("ok=true on error response, want false")
	}
	if env.Error.Code != "invalid_mode" {
		t.Errorf("error.code = %q, want 'invalid_mode'", env.Error.Code)
	}
}

// TestHandleV2SetDaemonMode_malformedJSON verifies 400 on non-JSON body.
func TestHandleV2SetDaemonMode_malformedJSON(t *testing.T) {
	url, _ := newModeTestServer(t)

	resp, err := http.Post(url+"/api/v2/daemon/mode", "application/json", bytes.NewReader([]byte(`not json`)))
	if err != nil {
		t.Fatalf("POST /api/v2/daemon/mode: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}
}

// TestHandleV2SetDaemonMode_preservesEnvOverrides verifies that POST does not
// overwrite operator EnvOverrides already in daemon.config.json.
func TestHandleV2SetDaemonMode_preservesEnvOverrides(t *testing.T) {
	url, daemonRoot := newModeTestServer(t)

	// Pre-write a config with an operator override.
	cfgPath := filepath.Join(daemonRoot, "daemon.config.json")
	if err := mode.SaveConfig(cfgPath, mode.Config{
		Mode:         mode.Background,
		EnvOverrides: map[string]string{"ARCHIGRAPH_HEAP_MAX_PCT": "50"},
	}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	body, _ := json.Marshal(map[string]string{"mode": "workstation"})
	resp, err := http.Post(url+"/api/v2/daemon/mode", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	cfg, err := mode.LoadConfig(cfgPath)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.Mode != mode.Workstation {
		t.Errorf("mode = %q, want workstation", cfg.Mode)
	}
	if cfg.EnvOverrides["ARCHIGRAPH_HEAP_MAX_PCT"] != "50" {
		t.Errorf("EnvOverrides lost; got %q, want '50'", cfg.EnvOverrides["ARCHIGRAPH_HEAP_MAX_PCT"])
	}
}
