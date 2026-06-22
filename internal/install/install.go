// Package install applies an grafel group config: registers the MCP
// entry, installs git hooks, generates watcher units, and writes the
// per-group config + state directories.
//
// The package is intentionally a thin coordinator over the dedicated
// sub-packages (hooks, watchers, mcpreg, registry). Keep wiring here;
// keep file-shape decisions in the sub-packages.
package install

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/cajasmota/grafel/internal/install/agenthooks"
	"github.com/cajasmota/grafel/internal/install/hooks"
	"github.com/cajasmota/grafel/internal/install/mcpreg"
	"github.com/cajasmota/grafel/internal/install/rulesfiles"
	"github.com/cajasmota/grafel/internal/install/tooladapter"
	"github.com/cajasmota/grafel/internal/install/watchers"
	"github.com/cajasmota/grafel/internal/registry"
)

// Options is the input to Apply.
type Options struct {
	Group   string
	Config  *registry.GroupConfig
	BinPath string // grafel binary
	// DryRun keeps every action in memory; nothing is written.
	DryRun bool
	// SkipHooks/SkipWatchers/SkipMCP gate the corresponding install steps.
	SkipHooks    bool
	SkipWatchers bool
	SkipMCP      bool
	// SkipRulesFiles skips writing per-repo IDE rules files
	// (AGENTS.md, CLAUDE.md, .windsurfrules, .cursorrules,
	// .codeium/instructions.md, .github/copilot-instructions.md).
	SkipRulesFiles bool
	// InstallAgentHooks, when true, installs the OPT-IN Claude Code
	// PreToolUse grep-interceptor hook (#4273) into each repo's
	// .claude/settings.json. It is also gated by Config.Features.AgentHooks;
	// either the explicit option OR the persisted feature flag enables it.
	// CLAUDE CODE ONLY; advisory-only; never blocks.
	InstallAgentHooks bool
	// MCPTools, when non-nil, restricts MCP registration to the named tool
	// adapter IDs (e.g. {"claude","cursor"}) — the wizard's "choose which AI
	// tools get the grafel MCP server" selection (#5344). Semantics:
	//
	//   - nil   → back-compat: register every enabled MCP-supporting tool
	//             (today's behaviour; existing non-interactive callers).
	//   - []    → register NONE (the --no-mcp / "none selected" case).
	//   - [ids] → register exactly the listed tools (filtered to the enabled
	//             MCP-supporting set; unknown/non-MCP IDs are ignored).
	//
	// The filter is by adapter ID, applied on top of the enabled-tools set, so
	// a tool the group config doesn't enable is never registered even if named.
	MCPTools *[]string
}

// Result reports what an Apply call did so the CLI can print a summary.
type Result struct {
	GroupConfigPath string
	HooksInstalled  []string                 // repo paths
	WatcherUnits    []string                 // unit-file paths
	WatcherStatuses []watchers.WatcherStatus // per-unit activation state
	MCPSettings     []string                 // settings.json paths touched
	// RulesFiles maps repo path → relative rules-file paths written
	// (e.g. ".windsurfrules"). Empty when SkipRulesFiles is true.
	RulesFiles map[string][]string
	// RulesFilesStaleSkipped maps repo path → rules-file paths left
	// untouched because they contain mixed predecessor + unrelated
	// content. The user is warned to migrate these manually.
	RulesFilesStaleSkipped map[string][]string
	// RulesFilesStaleReplaced maps repo path → rules-file paths that
	// were entirely predecessor content and got overwritten.
	RulesFilesStaleReplaced map[string][]string
	// AgentHooksInstalled lists repo paths that got the opt-in Claude Code
	// PreToolUse grep-interceptor hook (#4273). Empty unless agent hooks
	// were enabled.
	AgentHooksInstalled []string
	// WatcherWarnings collects non-fatal watcher-activation failures. The
	// group config is fully saved before watchers are activated, so a watcher
	// that fails to load (e.g. a flaky launchctl error that survives the
	// bounded retry) must NOT abort the install — the group is still
	// registered and will index. Callers surface these as warnings.
	WatcherWarnings []string
}

// Apply registers the group, writes its config, then installs hooks +
// watchers + MCP entries as configured.
func Apply(opts Options) (*Result, error) {
	if opts.Group == "" {
		return nil, errors.New("group is required")
	}
	if opts.Config == nil {
		return nil, errors.New("config is required")
	}
	if opts.BinPath == "" {
		bp, err := os.Executable()
		if err != nil {
			return nil, fmt.Errorf("resolving grafel binary: %w", err)
		}
		opts.BinPath = bp
	}

	res := &Result{}

	// Resolve the enabled tool adapters for this group. Back-compat: an
	// absent/empty Config.Tools yields every supported tool, reproducing
	// the historical hard-coded sequence (all six rules files + Claude
	// skills/hooks + Claude/Windsurf MCP). The same set drives the
	// per-repo rules/hook steps and the MCP step below.
	adaptersForRepo := tooladapter.EnabledAdapters(opts.Config)

	cfgPath, err := registry.ConfigPathFor(opts.Group)
	if err != nil {
		return nil, err
	}
	res.GroupConfigPath = cfgPath

	stateDir, err := registry.StateDirFor(opts.Group)
	if err != nil {
		return nil, err
	}

	if !opts.DryRun {
		if err := os.MkdirAll(stateDir, 0o755); err != nil {
			return nil, err
		}
		if err := registry.SaveGroupConfig(cfgPath, opts.Config); err != nil {
			return nil, err
		}
		if err := registry.AddGroup(opts.Group, cfgPath); err != nil {
			return nil, err
		}
	}

	for _, r := range opts.Config.Repos {
		repo := r.Path
		if !filepath.IsAbs(repo) {
			abs, err := filepath.Abs(repo)
			if err == nil {
				repo = abs
			}
		}
		if !opts.SkipHooks && opts.Config.Features.GitHooks {
			if !opts.DryRun {
				if err := hooks.Install(repo, opts.BinPath, opts.Group); err != nil {
					return nil, fmt.Errorf("hooks for %s: %w", repo, err)
				}
			}
			res.HooksInstalled = append(res.HooksInstalled, repo)
		}
		if !opts.SkipRulesFiles {
			// Collect the union of rules-file targets across the enabled
			// tools, ordered by rulesfiles.Targets so Result reporting is
			// identical to the historical single-WriteAll behaviour.
			targets := enabledRulesTargets(adaptersForRepo)
			if len(targets) > 0 {
				if !opts.DryRun {
					wr, werr := rulesfiles.WriteTargets(repo, rulesfiles.WriteOptions{
						GroupName: opts.Group,
					}, targets)
					if werr != nil {
						return nil, fmt.Errorf("rules files for %s: %w", repo, werr)
					}
					if wr != nil {
						if res.RulesFiles == nil {
							res.RulesFiles = map[string][]string{}
						}
						res.RulesFiles[repo] = wr.Written
						if len(wr.SkippedMixedStale) > 0 {
							if res.RulesFilesStaleSkipped == nil {
								res.RulesFilesStaleSkipped = map[string][]string{}
							}
							res.RulesFilesStaleSkipped[repo] = wr.SkippedMixedStale
						}
						if len(wr.ReplacedStale) > 0 {
							if res.RulesFilesStaleReplaced == nil {
								res.RulesFilesStaleReplaced = map[string][]string{}
							}
							res.RulesFilesStaleReplaced[repo] = wr.ReplacedStale
						}
					}
				} else {
					if res.RulesFiles == nil {
						res.RulesFiles = map[string][]string{}
					}
					res.RulesFiles[repo] = append([]string{}, targets...)
				}
			}
		}
		// The opt-in PreToolUse agent hook is Claude-only. Install it only
		// when (a) an adapter that supports it is enabled, AND (b) the
		// option/feature flag requests it.
		if (opts.InstallAgentHooks || opts.Config.Features.AgentHooks) && anyAdapterSupportsAgentHook(adaptersForRepo) {
			if !opts.DryRun {
				if _, err := agenthooks.Install(repo); err != nil {
					return nil, fmt.Errorf("agent hooks for %s: %w", repo, err)
				}
			}
			res.AgentHooksInstalled = append(res.AgentHooksInstalled, repo)
		}
		if !opts.SkipWatchers && opts.Config.Features.Watchers {
			u := watchers.Unit{Group: opts.Group, Repo: repo, BinPath: opts.BinPath}
			if !opts.DryRun {
				path, err := watchers.Write(u)
				if err != nil {
					return nil, fmt.Errorf("watcher for %s: %w", repo, err)
				}
				res.WatcherUnits = append(res.WatcherUnits, path)

				// Activate the watcher unit through the OS-native loader
				// (launchctl on macOS, systemctl on Linux, schtasks on Windows).
				// A watcher that fails to activate is a NON-FATAL warning: the
				// group config is already persisted (above), so the group is
				// registered and will index regardless. Aborting here used to
				// fail the whole wizard on a flaky launchctl error (#5338).
				loader := watchers.NewLoader()
				if lerr := loader.Load(u); lerr != nil && !watchers.IsNonFatal(lerr) {
					res.WatcherWarnings = append(res.WatcherWarnings,
						fmt.Sprintf("watcher for %s not activated: %v; the group is still registered and will index", repo, lerr))
				}
				// Report activation state regardless of non-fatal /run failures.
				if st, serr := loader.Status(u); serr == nil {
					res.WatcherStatuses = append(res.WatcherStatuses, st)
				}
			} else {
				p, _ := watchers.UnitPath(u)
				res.WatcherUnits = append(res.WatcherUnits, p)
			}
		}
	}

	if !opts.SkipMCP {
		registryPath, err := registry.RegistryPath()
		if err != nil {
			return nil, err
		}
		// The wizard MCP-tools selection (#5344) filters the enabled
		// MCP-supporting adapters by ID before registration. A nil selection
		// preserves today's behaviour (every enabled tool); a non-nil one —
		// including an empty slice (register none) — is honoured exactly.
		mcpAdapters := adaptersForRepo
		if opts.MCPTools != nil {
			mcpAdapters = filterAdaptersByID(adaptersForRepo, *opts.MCPTools)
		}
		for _, tool := range enabledMCPTools(mcpAdapters) {
			if opts.DryRun {
				p, _ := mcpreg.SettingsPath(tool)
				res.MCPSettings = append(res.MCPSettings, p)
				continue
			}
			path, err := mcpreg.Register(tool, opts.BinPath, registryPath)
			if err != nil {
				// Missing parent directory for an uninstalled tool is
				// fine — record nothing and move on.
				if errors.Is(err, os.ErrNotExist) {
					continue
				}
				return nil, fmt.Errorf("mcp register %s: %w", tool, err)
			}
			res.MCPSettings = append(res.MCPSettings, path)
		}
	}

	return res, nil
}

// enabledRulesTargets returns the union of rules-file targets across the
// given adapters, ordered to match rulesfiles.Targets so the resulting
// Result.RulesFiles slice is identical to the historical single-WriteAll
// ordering. Duplicate targets (multiple tools reading the same file) are
// de-duplicated.
func enabledRulesTargets(adapters []tooladapter.Adapter) []string {
	want := map[string]bool{}
	for _, a := range adapters {
		for _, t := range a.RulesFileTargets() {
			want[t] = true
		}
	}
	out := make([]string, 0, len(want))
	for _, t := range rulesfiles.Targets {
		if want[t] {
			out = append(out, t)
			delete(want, t)
		}
	}
	// Any target not present in rulesfiles.Targets (shouldn't happen for
	// the built-in adapters) is appended in adapter order for safety.
	for _, a := range adapters {
		for _, t := range a.RulesFileTargets() {
			if want[t] {
				out = append(out, t)
				delete(want, t)
			}
		}
	}
	return out
}

// filterAdaptersByID keeps only the adapters whose ID is in the selected set,
// preserving registry order. Used by the wizard MCP-tools selection (#5344) so
// MCP registration targets exactly the tools the user chose. An empty selection
// yields no adapters (register none).
func filterAdaptersByID(adapters []tooladapter.Adapter, selected []string) []tooladapter.Adapter {
	want := make(map[string]bool, len(selected))
	for _, id := range selected {
		want[id] = true
	}
	var out []tooladapter.Adapter
	for _, a := range adapters {
		if want[a.ID()] {
			out = append(out, a)
		}
	}
	return out
}

// enabledMCPTools returns the mcpreg.Tool entries to register, in adapter
// order, for the adapters that support MCP today. De-duplicated.
func enabledMCPTools(adapters []tooladapter.Adapter) []mcpreg.Tool {
	seen := map[mcpreg.Tool]bool{}
	var out []mcpreg.Tool
	for _, a := range adapters {
		if !a.SupportsMCP() {
			continue
		}
		t := a.MCPTool()
		if t == "" || seen[t] {
			continue
		}
		seen[t] = true
		out = append(out, t)
	}
	return out
}

// anyAdapterSupportsAgentHook reports whether any enabled adapter exposes
// the opt-in PreToolUse agent hook (Claude-only today).
func anyAdapterSupportsAgentHook(adapters []tooladapter.Adapter) bool {
	for _, a := range adapters {
		if a.SupportsAgentHook() {
			return true
		}
	}
	return false
}

// Uninstall reverses Apply for a single group: removes hooks/watchers
// and (optionally with purge) deletes per-group state.
func Uninstall(group string, purge bool) error {
	groups, err := registry.Groups()
	if err != nil {
		return err
	}
	var ref *registry.GroupRef
	for i := range groups {
		if groups[i].Name == group {
			ref = &groups[i]
			break
		}
	}
	if ref == nil {
		return fmt.Errorf("unknown group: %s", group)
	}
	cfg, err := registry.LoadGroupConfig(ref.ConfigPath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	bin, _ := os.Executable()
	if cfg != nil {
		loader := watchers.NewLoader()
		for _, r := range cfg.Repos {
			_ = hooks.Uninstall(r.Path)
			_ = agenthooks.Uninstall(r.Path)
			// Strip the grafel rules block from this repo's rules files,
			// mirroring the WriteTargets step in Apply. Only the
			// marker-wrapped region is removed; surrounding user content is
			// preserved, and a file is deleted only if grafel was its sole
			// author (#5274). Best-effort: I/O errors are ignored so a
			// single unwritable file never blocks the rest of uninstall.
			_, _ = rulesfiles.RemoveAll(r.Path)
			u := watchers.Unit{Group: group, Repo: r.Path, BinPath: bin}
			// Deregister from the OS scheduler before removing the unit file so
			// that the OS does not attempt to launch a missing binary.
			_ = loader.Unload(u)
			_ = watchers.Remove(u)
		}
	}
	if err := registry.RemoveGroup(group); err != nil {
		return err
	}
	if purge {
		stateDir, err := registry.StateDirFor(group)
		if err == nil {
			_ = os.RemoveAll(stateDir)
		}
		_ = os.Remove(ref.ConfigPath)
	}
	return nil
}
