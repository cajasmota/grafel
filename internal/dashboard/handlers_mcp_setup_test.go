package dashboard

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// newMCPSetupTestServer returns a *Server suitable for MCP setup handler tests.
func newMCPSetupTestServer(t *testing.T) *Server {
	t.Helper()
	s, err := NewServer(DefaultConfig(), newFakeStore())
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	return s
}

// writeTempMCPConfig writes a JSON config to a temp file and returns the path.
func writeTempMCPConfig(t *testing.T, content map[string]any) string {
	t.Helper()
	tmp := t.TempDir()
	path := filepath.Join(tmp, "mcp.json")
	b, _ := json.MarshalIndent(content, "", "  ")
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// ── detectState ───────────────────────────────────────────────────────────────

func TestDetectState_NilConfig(t *testing.T) {
	state, args := detectState(nil)
	if state != StateHostAbsent {
		t.Errorf("expected host_absent, got %q", state)
	}
	if len(args) != 0 {
		t.Errorf("expected no args, got %v", args)
	}
}

func TestDetectState_NoEntry(t *testing.T) {
	cfg := map[string]any{"mcpServers": map[string]any{"other-tool": map[string]any{}}}
	state, _ := detectState(cfg)
	if state != StateNotInstalled {
		t.Errorf("expected not_installed, got %q", state)
	}
}

func TestDetectState_ValidEntry(t *testing.T) {
	cfg := map[string]any{
		"mcpServers": map[string]any{
			"archigraph": map[string]any{
				"command": "archigraph",
				"args":    []any{"mcp"},
			},
		},
	}
	state, args := detectState(cfg)
	if state != StateInstalled {
		t.Errorf("expected installed, got %q", state)
	}
	if len(args) == 0 || args[0] != "mcp" {
		t.Errorf("expected args=[mcp], got %v", args)
	}
}

func TestDetectState_WrongCommand(t *testing.T) {
	cfg := map[string]any{
		"mcpServers": map[string]any{
			"archigraph": map[string]any{
				"command": "wrong",
				"args":    []any{"mcp"},
			},
		},
	}
	state, _ := detectState(cfg)
	if state != StatePartial {
		t.Errorf("expected partial, got %q", state)
	}
}

func TestDetectState_MissingMCPArg(t *testing.T) {
	cfg := map[string]any{
		"mcpServers": map[string]any{
			"archigraph": map[string]any{
				"command": "archigraph",
				"args":    []any{"serve"},
			},
		},
	}
	state, _ := detectState(cfg)
	if state != StatePartial {
		t.Errorf("expected partial, got %q", state)
	}
}

// ── writeMCPConfig + readMCPConfig round-trip ─────────────────────────────────

func TestWriteReadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mcp.json")

	in := map[string]any{
		"mcpServers": map[string]any{
			"archigraph": map[string]any{"command": "archigraph", "args": []any{"mcp"}},
		},
	}
	if err := writeMCPConfig(path, in); err != nil {
		t.Fatalf("write: %v", err)
	}
	out, err := readMCPConfig(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	b1, _ := json.Marshal(in)
	b2, _ := json.Marshal(out)
	if string(b1) != string(b2) {
		t.Errorf("round-trip mismatch:\n got  %s\n want %s", b2, b1)
	}
}

func TestWriteCreatesBackup(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mcp.json")

	original := map[string]any{"mcpServers": map[string]any{}}
	if err := writeMCPConfig(path, original); err != nil {
		t.Fatal(err)
	}
	updated := map[string]any{"mcpServers": map[string]any{"archigraph": "x"}}
	if err := writeMCPConfig(path, updated); err != nil {
		t.Fatal(err)
	}
	// backup should exist
	if _, err := os.Stat(path + ".bak"); err != nil {
		t.Errorf("backup not created: %v", err)
	}
}

// ── HTTP handlers ─────────────────────────────────────────────────────────────

func TestHandleMCPSetupHosts(t *testing.T) {
	s := newMCPSetupTestServer(t)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/mcp-setup/hosts", nil)
	s.handleMCPSetupHosts(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	var reply mcpHostsReply
	if err := json.NewDecoder(rr.Body).Decode(&reply); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(reply.Hosts) != 3 {
		t.Errorf("expected 3 hosts, got %d", len(reply.Hosts))
	}
	if reply.ServerArg != "archigraph" {
		t.Errorf("expected server_arg=archigraph, got %q", reply.ServerArg)
	}
}

func TestHandleMCPSetupInstallUninstall(t *testing.T) {
	s := newMCPSetupTestServer(t)

	// Redirect the config path to a temp dir by monkey-patching via env.
	// We can't call the real configPathFor without home dir manipulation,
	// so we test the logic via the exported helper functions directly and
	// only hit the HTTP layer for the error paths here.

	// --- bad host param ---
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/mcp-setup/install?host=unknown", nil)
	s.handleMCPSetupInstall(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("bad host: expected 400, got %d", rr.Code)
	}

	rr2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodPost, "/api/mcp-setup/uninstall?host=bad", nil)
	s.handleMCPSetupUninstall(rr2, req2)
	if rr2.Code != http.StatusBadRequest {
		t.Errorf("bad host uninstall: expected 400, got %d", rr2.Code)
	}
}

func TestHandleMCPSetupVerifyBadHost(t *testing.T) {
	s := newMCPSetupTestServer(t)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/mcp-setup/verify?host=nope", nil)
	s.handleMCPSetupVerify(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rr.Code)
	}
}

// ── mcpServersMap ─────────────────────────────────────────────────────────────

func TestMCPServersMapClaudeShape(t *testing.T) {
	cfg := map[string]any{
		"mcpServers": map[string]any{"x": 1},
	}
	m, commit := mcpServersMap(cfg)
	m["archigraph"] = "new"
	commit()
	servers := cfg["mcpServers"].(map[string]any)
	if _, ok := servers["archigraph"]; !ok {
		t.Error("archigraph key not committed to mcpServers")
	}
}

func TestMCPServersMapCursorShape(t *testing.T) {
	cfg := map[string]any{
		"mcp": map[string]any{
			"servers": map[string]any{"y": 2},
		},
	}
	m, commit := mcpServersMap(cfg)
	m["archigraph"] = "new"
	commit()
	mcp := cfg["mcp"].(map[string]any)
	servers := mcp["servers"].(map[string]any)
	if _, ok := servers["archigraph"]; !ok {
		t.Error("archigraph key not committed to mcp.servers")
	}
}

func TestMCPServersMapEmpty(t *testing.T) {
	cfg := map[string]any{}
	m, commit := mcpServersMap(cfg)
	m["archigraph"] = "new"
	commit()
	servers, ok := cfg["mcpServers"]
	if !ok {
		t.Fatal("mcpServers not created")
	}
	sm := servers.(map[string]any)
	if _, ok := sm["archigraph"]; !ok {
		t.Error("archigraph not present after commit")
	}
}
