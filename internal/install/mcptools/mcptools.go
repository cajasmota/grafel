// Package mcptools backs the wizard step that lets a user choose WHICH AI
// tools get the grafel MCP server, instead of auto-registering every detected
// tool (#5344).
//
// It builds on the existing tool registry (internal/install/tooladapter +
// internal/install/mcpreg) rather than hard-coding a second list: the set of
// selectable tools is exactly the MCP-supporting adapters. For each one it
// reports whether the tool's config is present on this machine, the config
// path + its last-modified time, and whether the config already contains a
// grafel entry.
//
// The default selection is the decided B + C design:
//
//   - (B) smart default: a tool is checked when its config was modified
//     recently (within RecentWindow) OR it already contains a grafel entry
//     (previously configured). Clearly-stale tools are unchecked but stay
//     visible so the user can re-check them.
//   - (C) remember last choice: the user's selection is persisted to
//     ~/.grafel/mcp-tools.json and, on subsequent runs, becomes the default
//     (C overrides B once a choice has been made).
package mcptools

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/cajasmota/grafel/internal/install/mcpreg"
	"github.com/cajasmota/grafel/internal/install/tooladapter"
)

// RecentWindow is the "recently used" horizon for the smart (B) default: a
// detected tool whose MCP config was modified within this window is checked by
// default. ~30 days balances "I use this tool" against stale configs left over
// from a tool the user abandoned.
const RecentWindow = 30 * 24 * time.Hour

// Tool describes one MCP-capable AI tool as surfaced to the wizard.
type Tool struct {
	// ID is the stable tooladapter ID (e.g. "claude", "cursor"). It is the
	// value persisted in the last-choice file and passed to install via
	// Options.MCPTools.
	ID string `json:"id"`
	// DisplayName is the human-facing name (e.g. "Claude Code").
	DisplayName string `json:"displayName"`
	// ConfigPath is the absolute path to the tool's MCP config file.
	ConfigPath string `json:"configPath"`
	// Detected reports whether the tool looks installed (its config file or
	// parent dir exists).
	Detected bool `json:"detected"`
	// HasGrafel reports whether the config already contains a grafel entry.
	HasGrafel bool `json:"hasGrafel"`
	// LastModified is the config file's mtime (zero when the file is absent).
	LastModified time.Time `json:"lastModified,omitempty"`
	// DefaultSelected is the computed B+C default checkbox state.
	DefaultSelected bool `json:"defaultSelected"`
}

// mcpAdapter pairs an adapter with its mcpreg tool. Only adapters that support
// MCP are selectable.
type mcpAdapter struct {
	id          string
	displayName string
	tool        mcpreg.Tool
}

// mcpAdapters returns the MCP-supporting adapters in registry order, drawn from
// the canonical tooladapter registry (no second hard-coded list).
func mcpAdapters() []mcpAdapter {
	var out []mcpAdapter
	for _, a := range tooladapter.All() {
		if !a.SupportsMCP() {
			continue
		}
		t := a.MCPTool()
		if t == "" {
			continue
		}
		out = append(out, mcpAdapter{id: a.ID(), displayName: a.DisplayName(), tool: t})
	}
	return out
}

// nowFunc is overridable in tests so "recent" is deterministic.
var nowFunc = time.Now

// Detect inspects every MCP-capable tool and returns ONLY the detected ones
// (config file or parent dir present), in registry order, with the B+C default
// selection already computed. When a last-choice file exists its selection
// overrides the smart (B) default for the tools it names (C); tools absent from
// the saved choice fall back to B.
//
// Detect never errors on individual tools — an unreadable config simply yields
// HasGrafel=false / a zero mtime.
func Detect() []Tool {
	last, _ := ReadLastChoice() // best-effort; nil when no prior choice
	return detectWith(last)
}

// detectWith is the testable core of Detect: lastChoice (possibly nil) is the
// remembered selection set.
func detectWith(lastChoice map[string]bool) []Tool {
	now := nowFunc()
	var out []Tool
	for _, a := range mcpAdapters() {
		path, err := mcpreg.SettingsPath(a.tool)
		if err != nil {
			continue
		}
		mtime, fileExists := mcpreg.ConfigModTime(path)
		detected := fileExists || parentDirExists(path)
		if !detected {
			continue
		}
		hasGrafel := mcpreg.HasGrafelEntry(path)
		recent := fileExists && now.Sub(mtime) <= RecentWindow

		// (B) smart default.
		def := recent || hasGrafel
		// (C) remembered choice overrides B for tools it names.
		if lastChoice != nil {
			if sel, ok := lastChoice[a.id]; ok {
				def = sel
			}
		}

		out = append(out, Tool{
			ID:              a.id,
			DisplayName:     a.displayName,
			ConfigPath:      path,
			Detected:        true,
			HasGrafel:       hasGrafel,
			LastModified:    mtime,
			DefaultSelected: def,
		})
	}
	return out
}

// parentDirExists reports whether the config file's parent directory exists —
// the "tool installed but MCP not yet configured" signal mcpreg uses.
func parentDirExists(path string) bool {
	info, err := os.Stat(filepath.Dir(path))
	return err == nil && info.IsDir()
}

// DefaultSelection returns the IDs of the tools whose DefaultSelected is true,
// in the order Detect returned them. Convenience for callers that just want the
// pre-checked set.
func DefaultSelection(tools []Tool) []string {
	var out []string
	for _, t := range tools {
		if t.DefaultSelected {
			out = append(out, t.ID)
		}
	}
	return out
}

// ── (C) last-choice persistence ──────────────────────────────────────────────

// lastChoiceFile is the persisted last-selection document.
type lastChoiceFile struct {
	// Selected is the list of tool IDs the user last chose to register.
	Selected []string `json:"selected"`
	// SavedAt is an RFC3339 timestamp, informational only.
	SavedAt string `json:"savedAt,omitempty"`
}

// LastChoicePath returns the path to ~/.grafel/mcp-tools.json. It honours HOME
// so tests can redirect it.
func LastChoicePath() (string, error) {
	home := os.Getenv("HOME")
	if home == "" {
		h, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		home = h
	}
	return filepath.Join(home, ".grafel", "mcp-tools.json"), nil
}

// ReadLastChoice loads the remembered selection as a set of tool IDs. Returns
// (nil, nil) when no choice has been saved yet (the common first-run case).
func ReadLastChoice() (map[string]bool, error) {
	path, err := LastChoicePath()
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var doc lastChoiceFile
	if err := json.Unmarshal(b, &doc); err != nil {
		// A corrupt file must not break the wizard — treat as "no choice".
		return nil, nil
	}
	set := make(map[string]bool, len(doc.Selected))
	for _, id := range doc.Selected {
		set[id] = true
	}
	return set, nil
}

// SaveLastChoice persists the chosen tool IDs to ~/.grafel/mcp-tools.json so a
// later wizard run defaults to them (C). The IDs are sorted for a stable file.
// An empty slice is persisted faithfully (the user chose "none").
func SaveLastChoice(ids []string) error {
	path, err := LastChoicePath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	sorted := append([]string(nil), ids...)
	sort.Strings(sorted)
	doc := lastChoiceFile{Selected: sorted, SavedAt: nowFunc().UTC().Format(time.RFC3339)}
	b, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
