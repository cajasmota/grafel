package dashboard

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/cajasmota/grafel/internal/install"
	"github.com/cajasmota/grafel/internal/install/tooladapter"
	"github.com/cajasmota/grafel/internal/registry"
)

// newToolsTestServer registers a group on the on-disk registry (isolated to a
// temp GRAFEL_HOME) and returns an httptest server whose ApplyToolDelta is
// mocked so the test never touches the live machine's rules files / MCP config.
// The mock records the (prev,next) it was called with.
func newToolsTestServer(t *testing.T, groupName string, tools []string) (*httptest.Server, *registry.GroupConfig, *struct {
	called     bool
	prev, next []string
	cfgPath    string
}) {
	t.Helper()
	archHome := t.TempDir()
	t.Setenv("GRAFEL_HOME", archHome)
	t.Setenv("GRAFEL_DAEMON_ROOT", t.TempDir())

	configDir := filepath.Join(archHome, "configs")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("mkdir configs: %v", err)
	}
	configPath := filepath.Join(configDir, groupName+".fleet.json")
	cfg := registry.GroupConfig{Name: groupName, Tools: tools}
	cfg.Repos = []registry.Repo{{Slug: "alpha", Path: "/repos/alpha"}}
	b, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal cfg: %v", err)
	}
	if err := os.WriteFile(configPath, b, 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if err := registry.AddGroup(groupName, configPath); err != nil {
		t.Fatalf("AddGroup: %v", err)
	}

	rec := &struct {
		called     bool
		prev, next []string
		cfgPath    string
	}{cfgPath: configPath}

	srv, err := NewServer(DefaultConfig(), newFakeStore())
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	srv.applyToolDelta = func(_ *registry.GroupConfig, _, _ string, prev, next []string, _ *install.ToolDeltaOps) (*install.ToolDeltaResult, error) {
		rec.called = true
		rec.prev = append([]string{}, prev...)
		rec.next = append([]string{}, next...)
		d := tooladapter.ComputeDelta(prev, next)
		return &install.ToolDeltaResult{Enabled: d.Enabled, Disabled: d.Disabled}, nil
	}
	return httptest.NewServer(srv.routes()), &cfg, rec
}

// TestV2GetTools_Shape verifies the GET payload lists every adapter with
// enabled/detected flags and reflects the group's explicit selection.
func TestV2GetTools_Shape(t *testing.T) {
	ts, _, _ := newToolsTestServer(t, "g1", []string{"claude", "cursor"})
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/v2/groups/g1/tools")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}
	var body struct {
		OK   bool `json:"ok"`
		Data struct {
			Tools    []v2ToolStatus `json:"tools"`
			Explicit bool           `json:"explicit"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !body.OK {
		t.Fatalf("want ok=true")
	}
	if !body.Data.Explicit {
		t.Errorf("want explicit=true for a group with a tools selection")
	}
	if len(body.Data.Tools) != len(tooladapter.All()) {
		t.Fatalf("want %d tools, got %d", len(tooladapter.All()), len(body.Data.Tools))
	}
	enabled := map[string]bool{}
	for _, ts := range body.Data.Tools {
		if ts.DisplayName == "" {
			t.Errorf("tool %q missing displayName", ts.ID)
		}
		enabled[ts.ID] = ts.Enabled
	}
	if !enabled["claude"] || !enabled["cursor"] {
		t.Errorf("claude+cursor should be enabled, got %+v", enabled)
	}
	for id, on := range enabled {
		if id != "claude" && id != "cursor" && on {
			t.Errorf("tool %q should be disabled", id)
		}
	}
}

// TestV2GetTools_DefaultAllEnabled verifies that a group with no explicit
// selection reports every tool enabled (back-compat default) and explicit=false.
func TestV2GetTools_DefaultAllEnabled(t *testing.T) {
	ts, _, _ := newToolsTestServer(t, "g2", nil)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/v2/groups/g2/tools")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	var body struct {
		Data struct {
			Tools    []v2ToolStatus `json:"tools"`
			Explicit bool           `json:"explicit"`
		} `json:"data"`
	}
	json.NewDecoder(resp.Body).Decode(&body)
	if body.Data.Explicit {
		t.Errorf("want explicit=false for a group with no selection")
	}
	for _, ts := range body.Data.Tools {
		if !ts.Enabled {
			t.Errorf("tool %q should be enabled by default", ts.ID)
		}
	}
}

func TestV2GetTools_NotFound(t *testing.T) {
	ts, _, _ := newToolsTestServer(t, "g3", nil)
	defer ts.Close()
	resp, err := http.Get(ts.URL + "/api/v2/groups/nope/tools")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("want 404, got %d", resp.StatusCode)
	}
}

// TestV2PutTools_ApplyDeltaSummary verifies the PUT path computes the delta,
// calls the (mocked) ApplyToolDelta, persists the new selection, and returns a
// per-tool summary of written/removed/unchanged.
func TestV2PutTools_ApplyDeltaSummary(t *testing.T) {
	ts, _, rec := newToolsTestServer(t, "g4", []string{"claude", "cursor"})
	defer ts.Close()

	// New selection: drop cursor, add windsurf, keep claude.
	reqBody, _ := json.Marshal(v2PutToolsReq{Tools: []string{"claude", "windsurf"}})
	req, _ := http.NewRequest(http.MethodPut, ts.URL+"/api/v2/groups/g4/tools", bytes.NewReader(reqBody))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PUT: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}
	var body struct {
		Data v2PutToolsResponse `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if !rec.called {
		t.Fatalf("ApplyToolDelta was not called")
	}
	// prev = explicit {claude,cursor}; next = {claude,windsurf}.
	wantNext := map[string]bool{"claude": true, "windsurf": true}
	if len(rec.next) != 2 || !wantNext[rec.next[0]] || !wantNext[rec.next[1]] {
		t.Errorf("next = %v, want claude+windsurf", rec.next)
	}

	action := map[string]string{}
	for _, s := range body.Data.Summary {
		action[s.ID] = s.Action
	}
	if action["windsurf"] != "written" {
		t.Errorf("windsurf action = %q, want written", action["windsurf"])
	}
	if action["cursor"] != "removed" {
		t.Errorf("cursor action = %q, want removed", action["cursor"])
	}
	if action["claude"] != "unchanged" {
		t.Errorf("claude action = %q, want unchanged", action["claude"])
	}

	// Persisted config now reflects the new selection.
	cfg, err := registry.LoadGroupConfig(rec.cfgPath)
	if err != nil {
		t.Fatalf("reload cfg: %v", err)
	}
	got := map[string]bool{}
	for _, id := range cfg.Tools {
		got[id] = true
	}
	if !got["claude"] || !got["windsurf"] || got["cursor"] {
		t.Errorf("persisted Tools = %v, want claude+windsurf", cfg.Tools)
	}
}

// TestV2PutTools_UnknownID verifies an unknown tool ID yields a 400 and does NOT
// call ApplyToolDelta or persist anything.
func TestV2PutTools_UnknownID(t *testing.T) {
	ts, _, rec := newToolsTestServer(t, "g5", []string{"claude"})
	defer ts.Close()

	reqBody, _ := json.Marshal(v2PutToolsReq{Tools: []string{"claude", "bogus-tool"}})
	req, _ := http.NewRequest(http.MethodPut, ts.URL+"/api/v2/groups/g5/tools", bytes.NewReader(reqBody))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PUT: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", resp.StatusCode)
	}
	if rec.called {
		t.Errorf("ApplyToolDelta should NOT be called on validation failure")
	}
	// Config unchanged.
	cfg, _ := registry.LoadGroupConfig(rec.cfgPath)
	if len(cfg.Tools) != 1 || cfg.Tools[0] != "claude" {
		t.Errorf("config should be unchanged, got %v", cfg.Tools)
	}
}

// TestV2PutTools_ApplyError verifies a failing ApplyToolDelta is reported as a
// 500 (the persist already happened, matching the documented contract).
func TestV2PutTools_ApplyError(t *testing.T) {
	archHome := t.TempDir()
	t.Setenv("GRAFEL_HOME", archHome)
	t.Setenv("GRAFEL_DAEMON_ROOT", t.TempDir())
	configDir := filepath.Join(archHome, "configs")
	os.MkdirAll(configDir, 0o755)
	configPath := filepath.Join(configDir, "g6.fleet.json")
	cfg := registry.GroupConfig{Name: "g6", Tools: []string{"claude"}}
	b, _ := json.Marshal(cfg)
	os.WriteFile(configPath, b, 0o644)
	registry.AddGroup("g6", configPath)

	srv, _ := NewServer(DefaultConfig(), newFakeStore())
	srv.applyToolDelta = func(_ *registry.GroupConfig, _, _ string, _, _ []string, _ *install.ToolDeltaOps) (*install.ToolDeltaResult, error) {
		return nil, os.ErrPermission
	}
	ts := httptest.NewServer(srv.routes())
	defer ts.Close()

	reqBody, _ := json.Marshal(v2PutToolsReq{Tools: []string{"cursor"}})
	req, _ := http.NewRequest(http.MethodPut, ts.URL+"/api/v2/groups/g6/tools", bytes.NewReader(reqBody))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PUT: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("want 500, got %d", resp.StatusCode)
	}
}

// TestV2PutTools_EmptyDisablesAll verifies that an explicit empty list is a
// valid request meaning "disable everything".
func TestV2PutTools_EmptyDisablesAll(t *testing.T) {
	ts, _, rec := newToolsTestServer(t, "g7", []string{"claude", "cursor"})
	defer ts.Close()

	reqBody, _ := json.Marshal(v2PutToolsReq{Tools: []string{}})
	req, _ := http.NewRequest(http.MethodPut, ts.URL+"/api/v2/groups/g7/tools", bytes.NewReader(reqBody))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PUT: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}
	if !rec.called || len(rec.next) != 0 {
		t.Errorf("want ApplyToolDelta called with empty next, got called=%v next=%v", rec.called, rec.next)
	}
	cfg, _ := registry.LoadGroupConfig(rec.cfgPath)
	if len(cfg.Tools) != 0 {
		t.Errorf("want empty Tools persisted, got %v", cfg.Tools)
	}
}
