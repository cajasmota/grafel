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
//     The file records BOTH halves of the decision (#6191): the tools that
//     were chosen AND the tools that were offered alongside them and left
//     unchecked. (C) therefore overrides (B) in both directions. It used to
//     store only the selected ids, which made an opt-out indistinguishable
//     from "never offered" — see SaveLastChoice.
//
//     A tool in NEITHER list was not part of the LAST decision, so (B) decides
//     for it and nothing is stranded off by having been absent. Two different
//     things put a tool there: it was not installed when that decision was
//     made, or it was declined earlier and then uninstalled before some later
//     save — SaveLastChoice rewrites the whole document rather than merging, so
//     a decline is forgotten once the tool stops being detected. Reinstalling
//     it then arrives at (B) again.
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
	last, err := ReadLastChoice()
	if err != nil {
		// The file could not be READ (permissions, I/O). It may well record an
		// opt-out, so it must not be mistaken for "no choice was ever made" —
		// that is the one input the B2 bound trusts. Substitute an empty
		// non-nil set: B2 is suppressed and (C) names nothing, so the (B)
		// default decides, exactly as before #6170. Same rule as
		// install.PreviouslyRegisteredMCPPaths: an unreadable config is not
		// evidence of absence.
		last = map[string]bool{}
	}
	return detectWith(last, prev)
}

// detectWith is the testable core of Detect: lastChoice is the remembered
// selection set, nil ONLY when no choice has ever been recorded; prevRegistered
// (possibly nil) is the set of config paths a previous install recorded a
// grafel registration at.
//
// B2 is bounded to the case where lastChoice is nil, and that bound is THE
// carrier of the safety property for this fix (install.PreviouslyRegisteredMCPPaths'
// opt-out subtraction is a second, narrower layer beneath it).
//
// Since #6191 the (C) map is genuinely tri-state — present-true, present-false,
// absent — so the `def = sel` branch below can now set def either way for a
// tool the recorded choice names, and for those tools (C) alone settles it.
//
// The nil bound governs what is left: a tool the map says NOTHING about, which
// means it was not part of the last decision at all. No box was cleared for it,
// so there is nothing to protect; the bound's job there is simply that a
// recorded choice, whatever it named, retires B2 for the whole run and leaves
// (B) to decide. B2 is a repair for a first run, not a standing input.
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
	for _, d := range detectedTools() {
		a, path, mtime, fileExists := d.adapter, d.path, d.mtime, d.fileExists
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

// detectedTool is one MCP-capable adapter that is present on this machine,
// with the filesystem facts detectWith needs already gathered.
type detectedTool struct {
	adapter    mcpAdapter
	path       string
	mtime      time.Time
	fileExists bool
}

// detectedTools returns the MCP-capable tools that are DETECTED here — config
// file present, or its parent directory present — in registry order. It is the
// single definition of "detected" in this package: detectWith turns it into the
// wizard rows, and SaveLastChoice uses it as the set of tools that were on
// offer when a choice was made.
//
// It reads only the tools' own config paths. In particular it does NOT consult
// the last-choice file, so SaveLastChoice can call it without any ordering
// dependency on the file it is about to write.
func detectedTools() []detectedTool {
	var out []detectedTool
	for _, a := range mcpAdapters() {
		path, err := mcpreg.SettingsPath(a.tool)
		if err != nil {
			continue
		}
		mtime, fileExists := mcpreg.ConfigModTime(path)
		if !fileExists && !parentDirExists(path) {
			continue
		}
		out = append(out, detectedTool{adapter: a, path: path, mtime: mtime, fileExists: fileExists})
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
	// Deselected is the list of tool IDs that were OFFERED alongside Selected
	// and left unchecked.
	Deselected []string `json:"deselected,omitempty"`
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

// ReadLastChoice loads the remembered selection as a set of tool IDs.
//
// nil WITH NO ERROR is returned ONLY when no choice has been saved — the file
// does not exist. nil WITH an error is a different thing entirely: the file
// could not be read, and DetectWithPrevious substitutes an empty non-nil set
// there.
//
// That distinction is load-bearing: detectWith keys the B2 bound on it, so
// every other outcome (an empty selection, a corrupt document) must return a
// non-nil map, and a read error must be surfaced so the caller can do the
// same. Do not collapse any of those to nil.
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
		// A corrupt file must not break the wizard, but it is NOT "no choice":
		// the file exists, so a choice was recorded — we just cannot read which
		// tools it named. Yield a NON-NIL empty set, keeping it distinguishable
		// from the genuine no-file case (nil) that the B2 bound keys on. Named
		// nothing means the (C) override does nothing, so the (B) default
		// decides — unchanged behaviour, minus the fail-open.
		return map[string]bool{}, nil
	}
	set := make(map[string]bool, len(doc.Selected)+len(doc.Deselected))
	// Deselections FIRST, so that a contradictory document naming a tool in
	// both lists resolves to selected rather than to whichever loop ran last.
	// A file written by a pre-#6191 binary has no `deselected` key at all:
	// json.Unmarshal leaves the field nil, this loop does nothing, and the set
	// is exactly what it used to be — absence still means "(B) decides".
	for _, id := range doc.Deselected {
		set[id] = false
	}
	for _, id := range doc.Selected {
		set[id] = true
	}
	return set, nil
}

// SaveLastChoice persists the chosen tool IDs to ~/.grafel/mcp-tools.json so a
// later wizard run defaults to them (C). The IDs are sorted for a stable file.
// An empty slice is persisted faithfully (the user chose "none").
//
// It ALSO records the opposite half of the decision (#6191): every tool that is
// detected on this machine AT THE MOMENT OF THE SAVE and is not in ids goes
// into `deselected`. Without it an unchecked tool is merely absent from the
// document, ReadLastChoice can only ever yield true, and detectWith's override
// can force a box on but never off: unchecking Claude Code then had no effect
// at all, because ~/.claude.json is rewritten constantly so the (B) `recent`
// term re-checked it every run.
//
// "Detected at the moment of the save" is deliberately NOT described as "what
// the wizard rendered", because that is true of neither caller:
//
//   - internal/dashboard.registerWizardMCP saves AFTER it has registered the
//     chosen tools, so detection runs later than the screen the user answered.
//     Benign but worth naming: registration is gated on the selection, so a
//     tool it configures is in ids and cannot land in `deselected`, and
//     registering can only make a tool MORE detected, never less.
//
//   - internal/cli's `--mcp-tools` path renders nothing at all. Recording
//     declines there is a decision, not an accident: the flag documents itself
//     as skipping the interactive picker, so it IS the picker's answer, and a
//     detected tool the user left out of an explicit list is one they declined.
//     It already minted a durable cross-surface "on" for the tools it named;
//     recording the matching "off" is what makes that record honest rather than
//     one-directional. Pinned by TestWizard_MCPToolsFlagRecordsDeclines.
//
// Only DETECTED tools are recorded, never the whole adapter registry. A tool
// the user does not have installed was not part of the decision, so it stays
// out of both lists and (B) decides for it if it later appears — the opt-out
// records what was declined, not what happened to be absent.
//
// The document is rewritten whole, not merged: a decline survives only while
// the tool stays detected. See the (C) note in the package comment.
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
	chosen := make(map[string]bool, len(ids))
	for _, id := range ids {
		chosen[id] = true
	}
	var deselected []string
	for _, d := range detectedTools() {
		if !chosen[d.adapter.id] {
			deselected = append(deselected, d.adapter.id)
		}
	}
	sort.Strings(deselected)
	doc := lastChoiceFile{
		Selected:   sorted,
		Deselected: deselected,
		SavedAt:    nowFunc().UTC().Format(time.RFC3339),
	}
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
