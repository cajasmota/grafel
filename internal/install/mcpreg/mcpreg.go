// Package mcpreg writes grafel entries into the per-tool MCP
// configuration files used by IDEs and AI clients.
//
// Per ADR-0004 there is exactly ONE grafel MCP entry per tool — a
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
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ServerName is the canonical key used in mcpServers maps.
const ServerName = "grafel"

// backupSentinelAbsent is the byte content written into a backup file when the
// original target did NOT exist before grafel touched it. On restore this
// tells us to DELETE the file grafel created rather than leave an orphan
// `{}` or `{"mcpServers":{}}` behind.
const backupSentinelAbsent = "GRAFEL_BACKUP_ORIGINAL_ABSENT"

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

// xdgConfigHome returns the XDG_CONFIG_HOME directory, defaulting to
// ~/.config when the env var is not set. Tests can override via
// t.Setenv("XDG_CONFIG_HOME", ...).
func xdgConfigHome() (string, error) {
	if x := os.Getenv("XDG_CONFIG_HOME"); x != "" {
		return x, nil
	}
	home, err := homeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config"), nil
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
	ClaudeCode        Tool = "claude-code"
	Windsurf          Tool = "windsurf"
	WindsurfJetBrains Tool = "windsurf-jetbrains"
	Cursor            Tool = "cursor"
	Codex             Tool = "codex"
	ContinueDev       Tool = "continue-dev"
	Zed               Tool = "zed"
	Kiro              Tool = "kiro"
	Antigravity       Tool = "antigravity"
)

// ConfigShape describes the JSON layout of a host's config file for
// purposes of mcpServers placement and key-preservation behaviour.
type ConfigShape int

const (
	// ShapeFlat: mcpServers lives at the root of the JSON document.
	// Example: { "mcpServers": { ... } }
	ShapeFlat ConfigShape = iota

	// ShapeNested: mcpServers is nested one level below an outer key.
	// Example Continue.dev: { "models": [...], "mcpServers": { ... } }
	// The shape is still a flat root for mcpServers in Continue.dev's
	// case — all top-level keys are preserved, mcpServers is just one of
	// many. Identical to ShapeFlat in practice; kept as a named constant
	// for documentation purposes.
	ShapeNested

	// ShapeBroadSettings: the config file holds many unrelated settings;
	// mcpServers is just one key among many (Zed, VS Code, etc.).
	// Behaviour is identical to ShapeFlat — all keys outside mcpServers
	// are left untouched. Named constant kept for documentation clarity.
	ShapeBroadSettings
)

// HostTarget describes a single candidate config-file path for a host.
type HostTarget struct {
	// Path is the absolute path to the config file.
	Path string
	// Shape describes the JSON layout (flat, nested, broad-settings).
	Shape ConfigShape
}

// DetectHostPaths returns HostTarget entries for a given host, identified
// by a parent directory (relative to home or xdgConfigHome) and a config
// file name. Only paths whose parent directory already exists are included —
// if the host is not installed the slice is empty and the caller should skip
// silently.
//
// parentDirParts are joined (via filepath.Join) to produce the parent dir
// path. The first element may be the special string "$XDG_CONFIG_HOME" to
// indicate that the base is xdgConfigHome() rather than homeDir().
func DetectHostPaths(parentDirParts []string, configFile string, shape ConfigShape) []HostTarget {
	var base string
	parts := parentDirParts

	if len(parts) > 0 && parts[0] == "$XDG_CONFIG_HOME" {
		xdg, err := xdgConfigHome()
		if err != nil {
			return nil
		}
		base = xdg
		parts = parts[1:]
	} else {
		h, err := homeDir()
		if err != nil {
			return nil
		}
		base = h
	}

	components := append([]string{base}, parts...)
	parentDir := filepath.Join(components...)
	configPath := filepath.Join(parentDir, configFile)

	// Include if config file itself exists OR parent dir exists.
	if _, err := os.Stat(configPath); err == nil {
		return []HostTarget{{Path: configPath, Shape: shape}}
	}
	if _, err := os.Stat(parentDir); err == nil {
		return []HostTarget{{Path: configPath, Shape: shape}}
	}
	return nil
}

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
		// Windsurf desktop app: ~/.codeium/windsurf/mcp_config.json
		return filepath.Join(home, ".codeium", "windsurf", "mcp_config.json"), nil
	case WindsurfJetBrains:
		// Windsurf JetBrains plugin: ~/.codeium/mcp_config.json
		return filepath.Join(home, ".codeium", "mcp_config.json"), nil
	case Cursor:
		// Cursor: ~/.cursor/mcp.json
		return filepath.Join(home, ".cursor", "mcp.json"), nil
	case Codex:
		// Codex CLI: ~/.codex/config.toml. Codex uses TOML (not JSON) and
		// expects the table [mcp_servers.<name>] (underscore, not the
		// JSON-world "mcpServers"). See registerTOML/unregisterTOML.
		return filepath.Join(home, ".codex", "config.toml"), nil
	case ContinueDev:
		// Continue.dev: ~/.continue/config.json
		return filepath.Join(home, ".continue", "config.json"), nil
	case Zed:
		// Zed: ~/.config/zed/settings.json
		xdg, err := xdgConfigHome()
		if err != nil {
			return "", err
		}
		return filepath.Join(xdg, "zed", "settings.json"), nil
	case Kiro:
		// Kiro (AWS agentic IDE): user-global MCP config at
		// ~/.kiro/settings/mcp.json. Kiro also supports a workspace-level
		// config at <repo>/.kiro/settings/mcp.json, but to match grafel's
		// ADR-0004 one-global-entry-per-tool model (the single daemon routes
		// by caller-CWD, so a per-repo entry is unnecessary), we register the
		// user-global file — same convention as Cursor (~/.cursor/mcp.json).
		// The JSON shape is identical to Cursor: { "mcpServers": { ... } }.
		return filepath.Join(home, ".kiro", "settings", "mcp.json"), nil
	case Antigravity:
		// Google Antigravity (agentic IDE): user-global MCP config at
		// ~/.gemini/antigravity/mcp_config.json (on Windows the same relative
		// path under %USERPROFILE%). grafel is a local stdio server, so the
		// entry is the standard JSON { "mcpServers": { "grafel": { command,
		// args } } } shape — identical to Cursor/Kiro. The `serverUrl` key is
		// only for remote HTTP MCP servers and does NOT apply here. Path source:
		// the official github-mcp-server install-antigravity guide.
		return filepath.Join(home, ".gemini", "antigravity", "mcp_config.json"), nil
	}
	return "", fmt.Errorf("unknown tool: %s", tool)
}

// DetectWindsurfPaths returns the Windsurf config paths that should be
// registered. Only paths whose parent directory already exists are returned —
// if Windsurf is not installed the list will be empty and the caller can skip
// registration silently.
//
// Two paths are considered:
//   - ~/.codeium/windsurf/mcp_config.json  (Windsurf desktop app)
//   - ~/.codeium/mcp_config.json           (Windsurf JetBrains plugin)
//
// For each path: if the config file itself exists it is always included. If
// the file does not exist but its parent directory does, the caller will
// create the file (Windsurf is installed but the MCP config hasn't been
// written yet). If neither the file nor the parent directory exists the
// path is omitted.
func DetectWindsurfPaths() []string {
	var out []string
	for _, tool := range []Tool{Windsurf, WindsurfJetBrains} {
		p, err := SettingsPath(tool)
		if err != nil {
			continue
		}
		// Include if the config file exists OR if its parent dir exists
		// (meaning Windsurf is installed but hasn't written an MCP config yet).
		if _, err := os.Stat(p); err == nil {
			out = append(out, p)
			continue
		}
		if _, err := os.Stat(filepath.Dir(p)); err == nil {
			out = append(out, p)
		}
	}
	return out
}

// DetectCursorPaths returns Cursor config paths to register.
// Uses ~/.cursor/mcp.json — ShapeFlat.
func DetectCursorPaths() []HostTarget {
	return DetectHostPaths([]string{".cursor"}, "mcp.json", ShapeFlat)
}

// DetectCodexPaths returns Codex CLI config paths to register.
// Uses ~/.codex/config.json — ShapeFlat (mcpServers key at root).
func DetectCodexPaths() []HostTarget {
	return DetectHostPaths([]string{".codex"}, "config.json", ShapeFlat)
}

// DetectContinueDevPaths returns Continue.dev config paths to register.
// Uses ~/.continue/config.json — ShapeNested (mcpServers sits alongside
// other top-level keys such as "models"; all are preserved).
func DetectContinueDevPaths() []HostTarget {
	return DetectHostPaths([]string{".continue"}, "config.json", ShapeNested)
}

// DetectKiroPaths returns Kiro config paths to register.
// Uses ~/.kiro/settings/mcp.json — ShapeFlat (mcpServers key at root,
// same shape as Cursor).
func DetectKiroPaths() []HostTarget {
	return DetectHostPaths([]string{".kiro", "settings"}, "mcp.json", ShapeFlat)
}

// DetectAntigravityPaths returns Antigravity config paths to register.
// Uses ~/.gemini/antigravity/mcp_config.json — ShapeFlat (mcpServers key at
// root, same JSON shape as Cursor/Kiro). grafel is a local stdio server, so
// no remote `serverUrl` is involved.
func DetectAntigravityPaths() []HostTarget {
	return DetectHostPaths([]string{".gemini", "antigravity"}, "mcp_config.json", ShapeFlat)
}

// DetectZedPaths returns Zed config paths to register.
// Uses $XDG_CONFIG_HOME/zed/settings.json (defaults to ~/.config/zed/settings.json).
// ShapeBroadSettings: mcpServers is upserted; all other Zed settings preserved.
func DetectZedPaths() []HostTarget {
	return DetectHostPaths([]string{"$XDG_CONFIG_HOME", "zed"}, "settings.json", ShapeBroadSettings)
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

// Register writes (or updates) the grafel entry in the given tool's
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

// backupDir returns ~/.grafel/backups/mcpreg, creating it on demand.
func backupDir() (string, error) {
	home, err := homeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, ".grafel", "backups", "mcpreg")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}

// sanitizePath turns an absolute config path into a filesystem-safe token used
// in backup file names (e.g. "/Users/x/.codeium/mcp_config.json" →
// "Users_x_.codeium_mcp_config.json"). A short hash suffix disambiguates paths
// that collapse to the same token.
func sanitizePath(path string) string {
	cleaned := filepath.ToSlash(path)
	cleaned = strings.TrimPrefix(cleaned, "/")
	repl := strings.NewReplacer("/", "_", ":", "_", "\\", "_", " ", "_")
	token := repl.Replace(cleaned)
	sum := sha256.Sum256([]byte(path))
	return token + "-" + hex.EncodeToString(sum[:4])
}

// sidecarBackupPath is the in-place ".grafel.bak" sidecar next to the
// target. It is the file consulted on restore.
func sidecarBackupPath(path string) string {
	return path + ".grafel.bak"
}

// backupOnce snapshots the original target file BEFORE grafel's first
// modification. It is a no-op if a sidecar backup already exists (so repeated
// RegisterPath calls within one install don't clobber the pristine snapshot).
//
// Two copies are written:
//   - an in-place sidecar `<path>.grafel.bak` (consulted on restore), and
//   - a timestamped copy under ~/.grafel/backups/mcpreg/ (audit trail).
//
// If the original file does not exist, a sentinel backup is written instead so
// that restore knows to DELETE grafel's file rather than leave an orphan.
func backupOnce(path string) error {
	sidecar := sidecarBackupPath(path)
	if _, err := os.Stat(sidecar); err == nil {
		// Backup already taken for this install transaction.
		return nil
	}

	orig, readErr := os.ReadFile(path)
	absent := errors.Is(readErr, os.ErrNotExist)
	if readErr != nil && !absent {
		return readErr
	}

	payload := orig
	if absent {
		payload = []byte(backupSentinelAbsent)
	}

	if err := os.WriteFile(sidecar, payload, 0o600); err != nil {
		return err
	}

	// Best-effort timestamped audit copy; failure here must not block install.
	if dir, err := backupDir(); err == nil {
		stamp := time.Now().UTC().Format(time.RFC3339)
		stamp = strings.ReplaceAll(stamp, ":", "-")
		audit := filepath.Join(dir, sanitizePath(path)+"-"+stamp+".json")
		_ = os.WriteFile(audit, payload, 0o600)
	}
	return nil
}

// RegisterPath writes (or updates) the grafel entry in an arbitrary
// .claude.json file. This is the workhorse used by both Register (single
// path) and the multi-dir install loop.
//
// Before its FIRST modification of a given file, RegisterPath snapshots the
// original (via backupOnce) so a later rollback can restore it exactly — see
// RestorePath. The merge is surgical: only mcpServers.grafel is added or
// updated; every other key and sibling server is preserved.
func RegisterPath(path, binPath string) (string, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	if err := backupOnce(path); err != nil {
		return "", fmt.Errorf("backup %s: %w", filepath.Base(path), err)
	}
	if isTOML(path) {
		// Codex-style TOML host. The same backup/sidecar machinery above
		// already snapshotted the original, so rollback (RestorePath) is
		// format-agnostic — it restores or deletes the raw bytes.
		return path, registerTOML(path, binPath)
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

// Unregister removes the grafel entry from the tool's settings.
// Returns nil if the file or entry doesn't exist (idempotent).
func Unregister(tool Tool) error {
	path, err := SettingsPath(tool)
	if err != nil {
		return err
	}
	return UnregisterPath(path)
}

// UnregisterPath removes ONLY the grafel entry from an arbitrary config
// file, preserving every other key and sibling server. Used by the multi-dir
// uninstall loop. If removing grafel leaves mcpServers empty, the empty
// mcpServers object is dropped too so we never leave an orphan
// `{"mcpServers":{}}`. Returns nil if the file or entry doesn't exist
// (idempotent). It NEVER overwrites foreign servers or resets the file to `{}`.
func UnregisterPath(path string) error {
	if isTOML(path) {
		return unregisterTOML(path)
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
	if len(servers) == 0 {
		// Drop the now-empty mcpServers key rather than persisting an
		// orphan `{"mcpServers":{}}`.
		delete(doc, "mcpServers")
	} else {
		doc["mcpServers"] = servers
	}
	return writeSettings(path, doc)
}

// RestorePath reverses a RegisterPath using the pristine backup taken by
// backupOnce. This is the rollback/de-register entry point that MUST be used
// instead of writing `{}`:
//
//   - If grafel CREATED the file (sentinel backup), the file is DELETED so
//     no orphan `{}` / `{"mcpServers":{}}` is left behind.
//   - If a real original was backed up, the file is restored byte-for-byte,
//     bringing back every foreign server and unrelated key exactly.
//   - If no backup exists (e.g. registration never ran), fall back to the
//     surgical UnregisterPath so we still only remove grafel's own entry.
//
// The sidecar backup is removed after a successful restore.
func RestorePath(path string) error {
	sidecar := sidecarBackupPath(path)
	b, err := os.ReadFile(sidecar)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			// No snapshot — fall back to surgical removal so we never
			// clobber foreign servers.
			return UnregisterPath(path)
		}
		return err
	}

	if string(b) == backupSentinelAbsent {
		// Original did not exist; remove grafel's file entirely.
		if rmErr := os.Remove(path); rmErr != nil && !errors.Is(rmErr, os.ErrNotExist) {
			return rmErr
		}
		_ = os.Remove(sidecar)
		return nil
	}

	// Restore the original content verbatim.
	if err := os.WriteFile(path, b, 0o644); err != nil {
		return err
	}
	_ = os.Remove(sidecar)
	return nil
}

// ClearBackup discards the pristine sidecar backup for a path. Call this after
// a SUCCESSFUL install so the next install can take a fresh snapshot (and so a
// future uninstall does not "restore" stale grafel-containing content).
// Idempotent: missing backups are ignored.
func ClearBackup(path string) {
	_ = os.Remove(sidecarBackupPath(path))
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

// HasGrafelEntry reports whether the config file at path already contains a
// grafel MCP server entry. It is format-aware (JSON mcpServers.grafel for the
// JSON hosts, the [mcp_servers.grafel] table for Codex TOML) and never errors:
// a missing/unreadable/malformed file simply reports false. Used by the wizard
// tool detector to pre-check tools that were previously configured (#5344).
func HasGrafelEntry(path string) bool {
	if isTOML(path) {
		raw, err := os.ReadFile(path)
		if err != nil {
			return false
		}
		_, present := stripGrafelBlock(string(raw))
		return present
	}
	doc, err := readSettings(path)
	if err != nil {
		return false
	}
	servers, _ := doc["mcpServers"].(map[string]any)
	if servers == nil {
		return false
	}
	_, ok := servers[ServerName]
	return ok
}

// ConfigModTime returns the last-modified time of the tool's MCP config file,
// and whether that file currently exists. A missing file yields (zero, false).
// Used by the wizard tool detector to compute the "recently used" smart
// default (#5344).
func ConfigModTime(path string) (time.Time, bool) {
	info, err := os.Stat(path)
	if err != nil {
		return time.Time{}, false
	}
	return info.ModTime(), true
}
