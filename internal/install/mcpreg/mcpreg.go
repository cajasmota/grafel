// Package mcpreg writes archigraph entries into the per-tool MCP
// configuration files used by IDEs and AI clients.
//
// Per ADR-0004 there is exactly ONE archigraph MCP entry per tool — a
// single global server that routes by caller-CWD. We never write
// per-project `.mcp.json` files.
//
// Claude Code uses ~/.claude.json as its primary config file (since the
// daemon-architecture rewrite in ADR-0017). The legacy Claude Desktop
// path (~/Library/Application Support/Claude/settings.json on macOS) is
// no longer targeted.
//
// Multiple Claude Code config dirs are supported: the installer scans
// for any ~/.claude-* directory that contains a .claude.json or is itself
// a known config dir, and registers in each one.
package mcpreg

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ServerName is the canonical key used in mcpServers maps.
const ServerName = "archigraph"

// homeDir returns the current user's home directory.
// On all platforms it checks the HOME environment variable first so
// that tests can redirect it with t.Setenv("HOME", tmpDir).
// os.UserHomeDir() on Windows ignores HOME and uses USERPROFILE instead,
// which breaks tests that rely on t.Setenv("HOME", ...) for isolation.
func homeDir() (string, error) {
	if h := os.Getenv("HOME"); h != "" {
		return h, nil
	}
	return os.UserHomeDir()
}

// Entry is the per-tool MCP server entry.
type Entry struct {
	Command string   `json:"command"`
	Args    []string `json:"args,omitempty"`
	Type    string   `json:"type,omitempty"`
}

// Tool is a target client.
type Tool string

const (
	ClaudeCode Tool = "claude-code"
	Windsurf   Tool = "windsurf"
)

// SettingsPath returns the absolute path to the settings file for a tool.
// For ClaudeCode this is ~/.claude.json (the modern Claude Code format).
func SettingsPath(tool Tool) (string, error) {
	home, err := homeDir()
	if err != nil {
		return "", err
	}
	switch tool {
	case ClaudeCode:
		// Modern Claude Code global config: ~/.claude.json
		// This is the file Claude Code reads for mcpServers entries.
		return filepath.Join(home, ".claude.json"), nil
	case Windsurf:
		return filepath.Join(home, ".codeium", "windsurf", "mcp_config.json"), nil
	}
	return "", fmt.Errorf("unknown tool: %s", tool)
}

// DetectClaudeConfigDirs returns the list of ~/.claude.json paths to
// register in. It starts with the primary ~/.claude.json and then scans
// for any ~/.claude-* directories that contain a .claude.json file
// (e.g. ~/.claude-personal/.claude.json).
//
// The caller can pass an explicit list via dirs; when non-nil it is used
// as-is and the auto-detection is skipped. This supports the
// --claude-config-dirs CLI flag.
func DetectClaudeConfigDirs(dirs []string) []string {
	if len(dirs) > 0 {
		return dirs
	}
	home, err := homeDir()
	if err != nil {
		return nil
	}

	// Primary config file.
	primary := filepath.Join(home, ".claude.json")
	seen := map[string]bool{primary: true}
	out := []string{primary}

	// Scan for ~/.claude-* directories.
	entries, err := os.ReadDir(home)
	if err != nil {
		return out
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasPrefix(name, ".claude-") {
			continue
		}
		candidate := filepath.Join(home, name, ".claude.json")
		if !seen[candidate] {
			seen[candidate] = true
			out = append(out, candidate)
		}
	}
	return out
}

// Register writes (or updates) the archigraph entry in the given tool's
// settings file. Other entries in `mcpServers` are preserved.
//
// The entry registered points at binPath with args ["mcp-bridge"] — the
// short-lived stdio↔socket bridge command (ADR-0017 #827).
//
// The registryPath parameter is kept for API compatibility but is no
// longer included in the MCP entry — the bridge connects to the daemon
// socket directly and the registry is the daemon's concern.
func Register(tool Tool, binPath, _ string) (string, error) {
	path, err := SettingsPath(tool)
	if err != nil {
		return "", err
	}
	return RegisterPath(path, binPath)
}

// RegisterPath writes (or updates) the archigraph entry in an arbitrary
// .claude.json file. This is the workhorse used by both Register (single
// path) and the multi-dir install loop.
func RegisterPath(path, binPath string) (string, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	doc, err := readSettings(path)
	if err != nil {
		return "", err
	}
	servers, _ := doc["mcpServers"].(map[string]any)
	if servers == nil {
		servers = map[string]any{}
	}
	servers[ServerName] = Entry{
		Command: binPath,
		Args:    []string{"mcp-bridge"},
		Type:    "stdio",
	}
	doc["mcpServers"] = servers
	return path, writeSettings(path, doc)
}

// Unregister removes the archigraph entry from the tool's settings.
// Returns nil if the file or entry doesn't exist (idempotent).
func Unregister(tool Tool) error {
	path, err := SettingsPath(tool)
	if err != nil {
		return err
	}
	return UnregisterPath(path)
}

// UnregisterPath removes the archigraph entry from an arbitrary config
// file. Used by the multi-dir uninstall loop.
func UnregisterPath(path string) error {
	doc, err := readSettings(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	servers, _ := doc["mcpServers"].(map[string]any)
	if servers == nil {
		return nil
	}
	delete(servers, ServerName)
	doc["mcpServers"] = servers
	return writeSettings(path, doc)
}

func readSettings(path string) (map[string]any, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return map[string]any{}, nil
		}
		return nil, err
	}
	if len(b) == 0 {
		return map[string]any{}, nil
	}
	doc := map[string]any{}
	if err := json.Unmarshal(b, &doc); err != nil {
		return nil, fmt.Errorf("%s: %w", filepath.Base(path), err)
	}
	return doc, nil
}

func writeSettings(path string, doc map[string]any) error {
	b, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
