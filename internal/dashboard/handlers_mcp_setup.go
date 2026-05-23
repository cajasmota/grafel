package dashboard

// handlers_mcp_setup.go — MCP Setup Wizard backend (issue #1247)
//
// Endpoints:
//   GET  /api/mcp-setup/hosts             — detect installed hosts + current config state
//   POST /api/mcp-setup/install?host=X    — idempotent merge of archigraph entry
//   POST /api/mcp-setup/uninstall?host=X  — remove archigraph entry
//   POST /api/mcp-setup/verify?host=X     — test-query against the local MCP server
//
// Supported hosts: claude, cursor, windsurf
// All config mutations create a backup before writing. No mutation happens
// without an explicit user action (POST).

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// ── Types ─────────────────────────────────────────────────────────────────────

// MCPHostID is the canonical identifier for a supported MCP host.
type MCPHostID string

const (
	HostClaude   MCPHostID = "claude"
	HostCursor   MCPHostID = "cursor"
	HostWindsurf MCPHostID = "windsurf"
)

// MCPInstallState describes the current installation state of the archigraph
// MCP server entry within a host's configuration file.
type MCPInstallState string

const (
	StateInstalled    MCPInstallState = "installed"     // entry present and well-formed
	StatePartial      MCPInstallState = "partial"       // entry present but malformed / wrong args
	StateNotInstalled MCPInstallState = "not_installed" // no entry found
	StateHostAbsent   MCPInstallState = "host_absent"   // host config file not found (host may not be installed)
)

// MCPHostInfo is the per-host response payload.
type MCPHostInfo struct {
	ID          MCPHostID       `json:"id"`
	Label       string          `json:"label"`
	ConfigPath  string          `json:"config_path"`
	Exists      bool            `json:"exists"`
	State       MCPInstallState `json:"state"`
	CurrentArgs []string        `json:"current_args,omitempty"` // args from existing entry, if any
	Error       string          `json:"error,omitempty"`
}

// mcpHostsReply is the GET /api/mcp-setup/hosts response.
type mcpHostsReply struct {
	Hosts     []MCPHostInfo `json:"hosts"`
	MCPPort   int           `json:"mcp_port"`
	ServerArg string        `json:"server_arg"` // "archigraph" — for display
}

// mcpActionReply is returned by install / uninstall / verify.
type mcpActionReply struct {
	OK      bool   `json:"ok"`
	Message string `json:"message"`
	// For verify: latency in milliseconds
	LatencyMs *int64 `json:"latency_ms,omitempty"`
}

// ── Config path resolution ────────────────────────────────────────────────────

// configPathFor returns the canonical MCP config file path for a host on
// the current OS. Returns ("", false) when the host is not supported on
// this OS (e.g. Windows-only paths on macOS).
func configPathFor(host MCPHostID) string {
	home, _ := os.UserHomeDir()
	if home == "" {
		return ""
	}

	switch host {
	case HostClaude:
		// Claude Code: ~/.claude/mcp.json  (all platforms)
		return filepath.Join(home, ".claude", "mcp.json")

	case HostCursor:
		switch runtime.GOOS {
		case "darwin":
			return filepath.Join(home, "Library", "Application Support", "Cursor", "User", "mcp.json")
		case "linux":
			xdg := os.Getenv("XDG_CONFIG_HOME")
			if xdg == "" {
				xdg = filepath.Join(home, ".config")
			}
			return filepath.Join(xdg, "Cursor", "User", "mcp.json")
		case "windows":
			appdata := os.Getenv("APPDATA")
			if appdata == "" {
				appdata = filepath.Join(home, "AppData", "Roaming")
			}
			return filepath.Join(appdata, "Cursor", "User", "mcp.json")
		}

	case HostWindsurf:
		switch runtime.GOOS {
		case "darwin":
			return filepath.Join(home, "Library", "Application Support", "Windsurf", "User", "mcp.json")
		case "linux":
			xdg := os.Getenv("XDG_CONFIG_HOME")
			if xdg == "" {
				xdg = filepath.Join(home, ".config")
			}
			return filepath.Join(xdg, "Windsurf", "User", "mcp.json")
		case "windows":
			appdata := os.Getenv("APPDATA")
			if appdata == "" {
				appdata = filepath.Join(home, "AppData", "Roaming")
			}
			return filepath.Join(appdata, "Windsurf", "User", "mcp.json")
		}
	}
	return ""
}

// hostLabel returns a human-readable name for the host.
func hostLabel(h MCPHostID) string {
	switch h {
	case HostClaude:
		return "Claude Code"
	case HostCursor:
		return "Cursor"
	case HostWindsurf:
		return "Windsurf"
	}
	return string(h)
}

// ── MCP JSON config shape ─────────────────────────────────────────────────────
//
// All three hosts use a compatible mcpServers / mcp.servers envelope. We
// target the Claude Code shape (mcpServers) since Cursor and Windsurf also
// accept it via their compatibility mode. If the file already contains
// the alternate shape we preserve it.

// mcpServerEntry is the per-server definition inside any host's MCP config.
type mcpServerEntry struct {
	Command string   `json:"command"`
	Args    []string `json:"args"`
}

// readMCPConfig reads and parses the host's MCP config file. Returns a nil
// map and no error when the file does not exist.
func readMCPConfig(path string) (map[string]any, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out map[string]any
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return out, nil
}

// writeMCPConfig atomically writes cfg to path, creating parent directories.
// The original file is backed up to <path>.bak before overwriting.
func writeMCPConfig(path string, cfg map[string]any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	// Backup original.
	if _, err := os.Stat(path); err == nil {
		if err2 := copyFile(path, path+".bak"); err2 != nil {
			return fmt.Errorf("backup: %w", err2)
		}
	}
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}

// mcpServersMap extracts or creates the mcpServers sub-object from a config
// map, normalising both "mcpServers" (Claude Code) and "mcp.servers" (Cursor/
// Windsurf) shapes. Returns the servers map and a setter that writes it back.
func mcpServersMap(cfg map[string]any) (servers map[string]any, commit func()) {
	// Prefer "mcpServers" (Claude Code canonical form).
	if raw, ok := cfg["mcpServers"]; ok {
		if m, ok := raw.(map[string]any); ok {
			return m, func() { cfg["mcpServers"] = m }
		}
	}
	// Try "mcp" → "servers" (Cursor/Windsurf).
	if raw, ok := cfg["mcp"]; ok {
		if mcpObj, ok := raw.(map[string]any); ok {
			if srvRaw, ok := mcpObj["servers"]; ok {
				if m, ok := srvRaw.(map[string]any); ok {
					return m, func() { mcpObj["servers"] = m; cfg["mcp"] = mcpObj }
				}
			}
		}
	}
	// Neither present — create mcpServers.
	m := map[string]any{}
	return m, func() { cfg["mcpServers"] = m }
}

// detectState inspects a loaded config map (nil = file not found) and returns
// the installation state for the archigraph entry together with any current args.
func detectState(cfg map[string]any) (MCPInstallState, []string) {
	if cfg == nil {
		return StateHostAbsent, nil
	}
	servers, _ := mcpServersMap(cfg)
	raw, ok := servers["archigraph"]
	if !ok {
		return StateNotInstalled, nil
	}
	// Marshal/unmarshal to convert map[string]any → mcpServerEntry.
	b, _ := json.Marshal(raw)
	var entry mcpServerEntry
	if err := json.Unmarshal(b, &entry); err != nil {
		return StatePartial, nil
	}
	if entry.Command != "archigraph" {
		return StatePartial, entry.Args
	}
	// Check that "mcp" is among the args.
	hasMCP := false
	for _, a := range entry.Args {
		if a == "mcp" {
			hasMCP = true
			break
		}
	}
	if !hasMCP {
		return StatePartial, entry.Args
	}
	return StateInstalled, entry.Args
}

// ── Handler: GET /api/mcp-setup/hosts ────────────────────────────────────────

// boundPort returns the TCP port the server is listening on, or 0 if the
// listener has not been set (e.g. during unit tests).
func (s *Server) boundPort() int {
	if s.listener == nil {
		return 0
	}
	_, portStr, err := net.SplitHostPort(s.listener.Addr().String())
	if err != nil {
		return 0
	}
	p, _ := strconv.Atoi(portStr)
	return p
}

func (s *Server) handleMCPSetupHosts(w http.ResponseWriter, _ *http.Request) {
	port := s.boundPort()
	hosts := []MCPHostID{HostClaude, HostCursor, HostWindsurf}
	infos := make([]MCPHostInfo, 0, len(hosts))

	for _, h := range hosts {
		info := MCPHostInfo{
			ID:    h,
			Label: hostLabel(h),
		}
		path := configPathFor(h)
		info.ConfigPath = path
		if path == "" {
			info.State = StateHostAbsent
			info.Error = "unsupported OS for this host"
			infos = append(infos, info)
			continue
		}
		_, statErr := os.Stat(path)
		info.Exists = statErr == nil

		cfg, err := readMCPConfig(path)
		if err != nil {
			info.State = StatePartial
			info.Error = err.Error()
		} else {
			info.State, info.CurrentArgs = detectState(cfg)
		}
		infos = append(infos, info)
	}

	writeJSON(w, http.StatusOK, mcpHostsReply{
		Hosts:     infos,
		MCPPort:   port,
		ServerArg: "archigraph",
	})
}

// ── Handler: POST /api/mcp-setup/install?host=X ──────────────────────────────

func (s *Server) handleMCPSetupInstall(w http.ResponseWriter, r *http.Request) {
	host, ok := parseHostParam(w, r)
	if !ok {
		return
	}
	path := configPathFor(host)
	if path == "" {
		writeErr(w, http.StatusBadRequest, "host not supported on this OS")
		return
	}

	cfg, err := readMCPConfig(path)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "read config: "+err.Error())
		return
	}
	if cfg == nil {
		cfg = map[string]any{}
	}

	servers, commit := mcpServersMap(cfg)
	servers["archigraph"] = mcpServerEntry{
		Command: "archigraph",
		Args:    []string{"mcp"},
	}
	commit()

	if err := writeMCPConfig(path, cfg); err != nil {
		writeErr(w, http.StatusInternalServerError, "write config: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, mcpActionReply{
		OK:      true,
		Message: fmt.Sprintf("archigraph MCP entry installed in %s", path),
	})
}

// ── Handler: POST /api/mcp-setup/uninstall?host=X ────────────────────────────

func (s *Server) handleMCPSetupUninstall(w http.ResponseWriter, r *http.Request) {
	host, ok := parseHostParam(w, r)
	if !ok {
		return
	}
	path := configPathFor(host)
	if path == "" {
		writeErr(w, http.StatusBadRequest, "host not supported on this OS")
		return
	}

	cfg, err := readMCPConfig(path)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "read config: "+err.Error())
		return
	}
	if cfg == nil {
		// Nothing to uninstall.
		writeJSON(w, http.StatusOK, mcpActionReply{OK: true, Message: "config not found — nothing to remove"})
		return
	}

	servers, commit := mcpServersMap(cfg)
	if _, exists := servers["archigraph"]; !exists {
		writeJSON(w, http.StatusOK, mcpActionReply{OK: true, Message: "archigraph entry not present"})
		return
	}
	delete(servers, "archigraph")
	commit()

	if err := writeMCPConfig(path, cfg); err != nil {
		writeErr(w, http.StatusInternalServerError, "write config: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, mcpActionReply{
		OK:      true,
		Message: fmt.Sprintf("archigraph MCP entry removed from %s", path),
	})
}

// ── Handler: POST /api/mcp-setup/verify?host=X ───────────────────────────────
//
// Sends a lightweight JSON-RPC initialize request to the MCP server on
// localhost and reports success or failure + latency.

func (s *Server) handleMCPSetupVerify(w http.ResponseWriter, r *http.Request) {
	_, ok := parseHostParam(w, r)
	if !ok {
		return
	}

	port := s.boundPort()
	url := fmt.Sprintf("http://127.0.0.1:%d/mcp", port)

	// We use a minimal MCP initialize probe — a JSON body that any compliant
	// server will respond to with 200 or a recognisable error.
	probe := map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params": map[string]any{
			"protocolVersion": "2024-11-05",
			"capabilities":    map[string]any{},
			"clientInfo":      map[string]any{"name": "archigraph-setup-wizard", "version": "1"},
		},
	}
	body, _ := json.Marshal(probe)

	client := &http.Client{Timeout: 5 * time.Second}
	start := time.Now()
	resp, err := client.Post(url, "application/json", bytes.NewReader(body))
	latencyMs := time.Since(start).Milliseconds()

	if err != nil {
		// If the MCP endpoint is not exposed over HTTP (it may be stdio-only),
		// fall back to a simple health check on the dashboard port.
		url2 := fmt.Sprintf("http://127.0.0.1:%d/api/info", port)
		start2 := time.Now()
		resp2, err2 := client.Get(url2)
		latencyMs = time.Since(start2).Milliseconds()
		if err2 != nil {
			writeJSON(w, http.StatusOK, mcpActionReply{
				OK:        false,
				Message:   fmt.Sprintf("MCP server unreachable: %v", err),
				LatencyMs: &latencyMs,
			})
			return
		}
		resp2.Body.Close()
		writeJSON(w, http.StatusOK, mcpActionReply{
			OK:        true,
			Message:   fmt.Sprintf("daemon reachable on port %d (MCP over stdio — no HTTP probe)", port),
			LatencyMs: &latencyMs,
		})
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		writeJSON(w, http.StatusOK, mcpActionReply{
			OK:        true,
			Message:   fmt.Sprintf("MCP server responded %d in %dms", resp.StatusCode, latencyMs),
			LatencyMs: &latencyMs,
		})
		return
	}
	writeJSON(w, http.StatusOK, mcpActionReply{
		OK:        false,
		Message:   fmt.Sprintf("MCP server returned status %d", resp.StatusCode),
		LatencyMs: &latencyMs,
	})
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func parseHostParam(w http.ResponseWriter, r *http.Request) (MCPHostID, bool) {
	h := strings.TrimSpace(r.URL.Query().Get("host"))
	switch MCPHostID(h) {
	case HostClaude, HostCursor, HostWindsurf:
		return MCPHostID(h), true
	}
	writeErr(w, http.StatusBadRequest, fmt.Sprintf("unknown host %q; valid: claude, cursor, windsurf", h))
	return "", false
}
