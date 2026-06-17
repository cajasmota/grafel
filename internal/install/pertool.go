// pertool.go — per-enabled-tool awareness shared by doctor and uninstall (#5258).
//
// install.json records the Claude .claude.json paths (MCP.RegisteredPaths) and
// the owned skills, but NOT the enabled-tool set: that lives in each group's
// config (registry.GroupConfig.Tools, resolved through
// tooladapter.EnabledTools). To make `grafel doctor` report — and `grafel
// uninstall` sweep — EVERY enabled tool (Cursor/Windsurf/Codex/Kiro/…), both
// commands resolve the enabled adapters from the registry here.
//
// The resolution is best-effort and never fatal: a machine with no groups
// registered yields an empty set, and an unreadable group config is skipped
// (doctor surfaces it via its own rules-file scan, which shares this path's
// registry read).
package install

import (
	"github.com/cajasmota/grafel/internal/install/mcpreg"
	"github.com/cajasmota/grafel/internal/install/tooladapter"
	"github.com/cajasmota/grafel/internal/registry"
)

// enabledToolBinding pairs a resolved adapter with the group it was enabled in.
// One adapter may appear once per group (the same tool can be enabled across
// multiple groups); callers that operate on user-global artifacts (MCP configs)
// de-duplicate by mcpreg.Tool, while per-repo artifacts (rules files) are
// scoped per group's repos.
type enabledToolBinding struct {
	group   string
	repos   []string
	adapter tooladapter.Adapter
}

// resolveEnabledToolBindings walks every registered grafel group and returns
// one binding per (group, enabled-adapter) pair, in a deterministic order
// (group order from the registry × registry adapter order). It never errors:
// registry/config read failures collapse to a shorter list. groupsFn and
// loadFn are injectable so doctor/uninstall tests can drive the set without a
// real registry on disk.
func resolveEnabledToolBindings(
	groupsFn func() ([]registry.GroupRef, error),
	loadFn func(path string) (*registry.GroupConfig, error),
) []enabledToolBinding {
	if groupsFn == nil {
		groupsFn = registry.Groups
	}
	if loadFn == nil {
		loadFn = registry.LoadGroupConfig
	}
	groups, err := groupsFn()
	if err != nil {
		return nil
	}
	var out []enabledToolBinding
	for _, g := range groups {
		cfg, lerr := loadFn(g.ConfigPath)
		if lerr != nil || cfg == nil {
			continue
		}
		repos := make([]string, 0, len(cfg.Repos))
		for _, r := range cfg.Repos {
			repos = append(repos, r.Path)
		}
		for _, a := range tooladapter.EnabledAdapters(cfg) {
			out = append(out, enabledToolBinding{
				group:   g.Name,
				repos:   repos,
				adapter: a,
			})
		}
	}
	return out
}

// mcpToolsFromBindings returns the distinct mcpreg.Tool entries registered by
// every enabled tool across all groups, in first-seen order. This is the set
// `uninstall` must deregister from (each tool's own config file/format) so the
// sweep covers Cursor/Windsurf/Codex/Kiro and not just Claude's
// .claude.json paths.
func mcpToolsFromBindings(bindings []enabledToolBinding) []mcpreg.Tool {
	seen := map[mcpreg.Tool]bool{}
	var out []mcpreg.Tool
	for _, b := range bindings {
		if !b.adapter.SupportsMCP() {
			continue
		}
		t := b.adapter.MCPTool()
		if t == "" || seen[t] {
			continue
		}
		seen[t] = true
		out = append(out, t)
	}
	return out
}
