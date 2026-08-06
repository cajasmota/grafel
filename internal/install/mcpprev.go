// mcpprev.go — the durable "grafel's MCP was registered here" signal (#6170).
//
// The tool-selection wizard pre-checks a tool from two erasable facts: the
// config file was touched recently, or it still contains a grafel entry. Both
// go false the moment something deletes the entry (the #6168 rollback being
// the demonstrated way) and the file then sits untouched past RecentWindow.
// The tool arrives UNCHECKED, and a user pressing enter through the next run
// persists that accident as their preference — a one-way ratchet.
//
// install.json already records what grafel itself did: state.MCP.RegisteredPaths
// is the list of host config paths RunCopy/RunDev registered the server in, and
// it survives the entry being deleted. That is the signal the wizard was
// missing. No new store.
package install

import (
	"path/filepath"

	"github.com/cajasmota/grafel/internal/install/mcpreg"
	"github.com/cajasmota/grafel/internal/install/tooladapter"
	"github.com/cajasmota/grafel/internal/registry"
)

// PreviouslyRegisteredMCPPaths returns the set of MCP host config paths a
// previous grafel install recorded registering the server in, for
// mcptools.DetectWithPrevious.
//
// Paths belonging to a tool the user has DELIBERATELY turned off are removed
// first. That subtraction is the safety property: the recorded registration
// must only repair an accidental unchecking, never re-check a box the user
// went out of their way to clear. The durable record of "off" is the group
// configs' explicit tool selection (registry.GroupConfig.Tools, resolved via
// tooladapter.EnabledAdapters) — the same set `uninstall` and `doctor` sweep.
//
// Everything here is best-effort: an unreadable install.json or registry
// yields nil, which simply restores the pre-#6170 default. It never errors and
// never writes.
func PreviouslyRegisteredMCPPaths() map[string]bool {
	return previouslyRegisteredMCPPaths(DefaultStatePath, nil, nil)
}

// previouslyRegisteredMCPPaths is the injectable core of
// PreviouslyRegisteredMCPPaths. statePathFn locates install.json; groupsFn /
// loadFn are the registry accessors (nil = the real ones).
func previouslyRegisteredMCPPaths(
	statePathFn func() (string, error),
	groupsFn func() ([]registry.GroupRef, error),
	loadFn func(path string) (*registry.GroupConfig, error),
) map[string]bool {
	statePath, err := statePathFn()
	if err != nil {
		return nil
	}
	state, err := ReadState(statePath)
	if err != nil || state == nil {
		return nil
	}

	out := make(map[string]bool, len(state.MCP.RegisteredPaths))
	for _, p := range state.MCP.RegisteredPaths {
		if p != "" {
			out[filepath.Clean(p)] = true
		}
	}
	if len(out) == 0 {
		return nil
	}

	disabled, ok := deliberatelyDisabledMCPTools(groupsFn, loadFn)
	if !ok {
		// We could not read the tool selection, so we cannot know whether a
		// tool was deliberately disabled. Offer nothing rather than risk
		// re-checking a box the user cleared: the cost of failing closed is
		// the pre-#6170 default, the cost of failing open is a silent
		// override of the user's decision on a transient read error.
		return nil
	}
	for _, tool := range disabled {
		for _, p := range mcpConfigPathsFor(tool) {
			delete(out, filepath.Clean(p))
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// deliberatelyDisabledMCPTools returns the MCP-capable tools that NO registered
// group enables — i.e. the user made an explicit tool selection and left these
// out — and whether the selection could be read at all.
//
// ok=false means "unknown": the registry or a group config would not load, so
// the caller MUST fail closed. It is deliberately distinguished from the two
// legitimately-empty answers — no groups registered (nothing to disable
// anything) and a group with an empty Tools list, which means "all tools"
// (tooladapter.EnabledAdapters). Absence of evidence is not an opt-out; an
// unreadable config is not evidence of absence.
//
// This walks the registry directly rather than through
// resolveEnabledToolBindings because that helper collapses a load failure into
// a shorter list — fine for doctor's advisory sweep, wrong here, where the
// difference between "nothing disabled" and "cannot tell" is the whole point.
func deliberatelyDisabledMCPTools(
	groupsFn func() ([]registry.GroupRef, error),
	loadFn func(path string) (*registry.GroupConfig, error),
) ([]mcpreg.Tool, bool) {
	if groupsFn == nil {
		groupsFn = registry.Groups
	}
	if loadFn == nil {
		loadFn = registry.LoadGroupConfig
	}
	groups, err := groupsFn()
	if err != nil {
		return nil, false
	}
	if len(groups) == 0 {
		return nil, true // no explicit selection exists anywhere
	}

	enabled := make(map[mcpreg.Tool]bool)
	for _, g := range groups {
		cfg, err := loadFn(g.ConfigPath)
		if err != nil || cfg == nil {
			return nil, false
		}
		for _, a := range tooladapter.EnabledAdapters(cfg) {
			if a.SupportsMCP() {
				if t := a.MCPTool(); t != "" {
					enabled[t] = true
				}
			}
		}
	}

	var out []mcpreg.Tool
	for _, a := range tooladapter.All() {
		if !a.SupportsMCP() {
			continue
		}
		t := a.MCPTool()
		if t == "" || enabled[t] {
			continue
		}
		out = append(out, t)
	}
	return out, true
}

// mcpConfigPathsFor returns every host config path grafel's installer may have
// registered this tool at — the canonical settings path plus the extra targets
// step 3 of RunCopy fans out to (Claude's sidecar profile dirs, Windsurf's
// desktop + JetBrains variants). Those two families are exactly what lands in
// state.MCP.RegisteredPaths, so this is the inverse of that mapping.
func mcpConfigPathsFor(tool mcpreg.Tool) []string {
	var out []string
	if p, err := mcpreg.SettingsPath(tool); err == nil {
		out = append(out, p)
	}
	switch tool {
	case mcpreg.ClaudeCode:
		out = append(out, mcpreg.DetectClaudeConfigDirs(nil)...)
	case mcpreg.Windsurf:
		// DetectWindsurfPaths covers BOTH the desktop and the JetBrains config
		// files, which is what step 3 registers. There is deliberately no
		// WindsurfJetBrains arm: no adapter returns that tool from MCPTool(),
		// so it can never reach this function.
		out = append(out, mcpreg.DetectWindsurfPaths()...)
	}
	return out
}
