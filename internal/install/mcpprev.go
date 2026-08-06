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

	for _, tool := range deliberatelyDisabledMCPTools(groupsFn, loadFn) {
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
// out.
//
// With no groups registered (or an unreadable registry) there is no explicit
// selection to read, so nothing is reported disabled: absence of evidence is
// not an opt-out. A group with an empty Tools list means "all tools"
// (tooladapter.EnabledAdapters), so it too disables nothing.
func deliberatelyDisabledMCPTools(
	groupsFn func() ([]registry.GroupRef, error),
	loadFn func(path string) (*registry.GroupConfig, error),
) []mcpreg.Tool {
	bindings := resolveEnabledToolBindings(groupsFn, loadFn)
	if len(bindings) == 0 {
		return nil
	}
	enabled := make(map[mcpreg.Tool]bool)
	for _, t := range mcpToolsFromBindings(bindings) {
		enabled[t] = true
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
	return out
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
	case mcpreg.Windsurf, mcpreg.WindsurfJetBrains:
		out = append(out, mcpreg.DetectWindsurfPaths()...)
	}
	return out
}
