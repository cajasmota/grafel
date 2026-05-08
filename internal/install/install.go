// Package install applies an archigraph group config: registers the MCP
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

	"github.com/cajasmota/archigraph/internal/install/hooks"
	"github.com/cajasmota/archigraph/internal/install/mcpreg"
	"github.com/cajasmota/archigraph/internal/install/watchers"
	"github.com/cajasmota/archigraph/internal/registry"
)

// Options is the input to Apply.
type Options struct {
	Group   string
	Config  *registry.GroupConfig
	BinPath string // archigraph binary
	// DryRun keeps every action in memory; nothing is written.
	DryRun bool
	// SkipHooks/SkipWatchers/SkipMCP gate the corresponding install steps.
	SkipHooks    bool
	SkipWatchers bool
	SkipMCP      bool
}

// Result reports what an Apply call did so the CLI can print a summary.
type Result struct {
	GroupConfigPath string
	HooksInstalled  []string // repo paths
	WatcherUnits    []string // unit-file paths
	MCPSettings     []string // settings.json paths touched
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
			return nil, fmt.Errorf("resolving archigraph binary: %w", err)
		}
		opts.BinPath = bp
	}

	res := &Result{}

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
		if !opts.SkipWatchers && opts.Config.Features.Watchers {
			u := watchers.Unit{Group: opts.Group, Repo: repo, BinPath: opts.BinPath}
			if !opts.DryRun {
				path, err := watchers.Write(u)
				if err != nil {
					return nil, fmt.Errorf("watcher for %s: %w", repo, err)
				}
				res.WatcherUnits = append(res.WatcherUnits, path)
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
		for _, tool := range []mcpreg.Tool{mcpreg.ClaudeCode, mcpreg.Windsurf} {
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
		for _, r := range cfg.Repos {
			_ = hooks.Uninstall(r.Path)
			_ = watchers.Remove(watchers.Unit{Group: group, Repo: r.Path, BinPath: bin})
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
