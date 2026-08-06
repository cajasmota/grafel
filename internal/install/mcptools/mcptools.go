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
//
//   - (B2) previously registered (#6170): also checked when a previous install
//     RECORDED registering grafel at that config path — but ONLY while no
//     choice has been recorded at all. See DetectWithPrevious / detectWith.
//
//     B2 exists because both (B) terms are erased by the same accident:
//     something deletes the grafel entry (the #6168 rollback being the
//     demonstrated way) so hasGrafel goes false, and if the file is then left
//     untouched past RecentWindow the tool arrives UNCHECKED — so that run
//     silently fails to (re-)register grafel's MCP for a tool the user has.
//
//     Its coverage is PARTIAL, inherently: only RunCopy/RunDev record into
//     state.MCP.RegisteredPaths, and only the Claude and Windsurf config
//     paths. install.Apply registers MCP for every enabled adapter but
//     persists nothing, so for Cursor, Codex, Kiro and Antigravity B2 is a
//     permanent no-op and the (B) default stands alone.
//
//   - (C) remember last choice: the user's selection is persisted to
//     ~/.grafel/mcp-tools.json and, on subsequent runs, becomes the default.
//
//     NOTE the asymmetry, which detectWith depends on: the file stores only
//     the SELECTED ids, so (C) can force a tool ON but never OFF — a tool the
//     user unchecked is simply ABSENT from it, and (B) re-decides. "C
//     overrides B" holds in one direction only.
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
	return DetectWithPrevious(nil)
}

// DetectWithPrevious is Detect plus the durable (B2) "grafel was registered
// here before" signal: prev is a set of MCP host config PATHS a previous
// grafel install recorded (install.json's state.MCP.RegisteredPaths). It is
// consulted ONLY when no last choice has been recorded — see detectWith.
//
// The set is INJECTED rather than read here for layering, not for a cycle:
// internal/install does not import mcptools, so reading install.json here
// would compile. It is still the wrong shape — mcptools' tests need nothing
// but a temp $HOME, and reading the state would drag internal/install and
// internal/registry into that. Callers use install.PreviouslyRegisteredMCPPaths().
func DetectWithPrevious(prev map[string]bool) []Tool {
	last, _ := ReadLastChoice() // best-effort; nil when no prior choice
	return detectWith(last, prev)
}

// detectWith is the testable core of Detect: lastChoice is the remembered
// selection set, nil ONLY when no choice has ever been recorded; prevRegistered
// (possibly nil) is the set of config paths a previous install recorded a
// grafel registration at.
//
// B2 is bounded to the case where lastChoice is nil, and that bound is the
// safety property. It cannot be carried by precedence within the expression,
// because (C) cannot express "off": ReadLastChoice builds its set only from
// the file's `selected` list, so every value it can yield is true and an
// opted-out tool is merely ABSENT from the map — the `def = sel` branch below
// can only ever set def=true. Composing B2 under a (C) that can only say "on"
// would let the install.json record re-check a box the user cleared, which is
// a REGRESSION on today's behaviour, not a repair.
//
// So: a recorded choice — any recorded choice, including "none" — owns the
// default, and B2 applies only while none exists. It repairs a fresh accident
// and never overrules a decision.
func detectWith(lastChoice, prevRegistered map[string]bool) []Tool {
	now := nowFunc()
	// A recorded choice suppresses B2 entirely (see above).
	var prev map[string]bool
	if lastChoice == nil {
		prev = cleanPathSet(prevRegistered)
	}
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
		// (B2) grafel registered itself at this exact config path on a previous
		// install, per install.json. Unlike hasGrafel this SURVIVES the entry
		// being deleted, which is the whole point (#6170). prev is nil whenever
		// a choice has been recorded, so this is false there.
		previouslyRegistered := prev[filepath.Clean(path)]

		// (B) smart default.
		def := recent || hasGrafel || previouslyRegistered
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

// cleanPathSet normalises a set of file paths so lookups are not defeated by a
// trailing separator or an unnormalised "." component in what install.json
// happens to record. Returns nil for a nil/empty input (lookups on a nil map
// are legal and always false).
func cleanPathSet(in map[string]bool) map[string]bool {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]bool, len(in))
	for p, v := range in {
		if v && p != "" {
			out[filepath.Clean(p)] = true
		}
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
