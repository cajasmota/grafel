// Package mcpreg writes archigraph entries into the per-tool MCP
// configuration files used by IDEs and AI clients.
//
// Per ADR-0004 there is exactly ONE archigraph MCP entry per tool — a
// single global server that routes by caller-CWD. We never write
// per-project `.mcp.json` files.
package mcpreg

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// ServerName is the canonical key used in mcpServers maps.
const ServerName = "archigraph"

// Entry is the per-tool MCP server entry.
type Entry struct {
	Command string   `json:"command"`
	Args    []string `json:"args,omitempty"`
}

// Tool is a target client.
type Tool string

const (
	ClaudeCode Tool = "claude-code"
	Windsurf   Tool = "windsurf"
)

// SettingsPath returns the absolute path to the settings file for a tool.
func SettingsPath(tool Tool) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	switch tool {
	case ClaudeCode:
		if runtime.GOOS == "darwin" {
			return filepath.Join(home, "Library", "Application Support", "Claude", "settings.json"), nil
		}
		return filepath.Join(home, ".config", "claude", "settings.json"), nil
	case Windsurf:
		return filepath.Join(home, ".codeium", "windsurf", "mcp_config.json"), nil
	}
	return "", fmt.Errorf("unknown tool: %s", tool)
}

// Register writes (or updates) the archigraph entry in the given tool's
// settings file. Other entries in `mcpServers` are preserved.
func Register(tool Tool, binPath, registryPath string) (string, error) {
	path, err := SettingsPath(tool)
	if err != nil {
		return "", err
	}
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
		Args:    []string{"mcp", "serve", "--registry", registryPath},
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
