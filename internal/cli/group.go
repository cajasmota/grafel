package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/cajasmota/grafel/internal/daemon/client"
	"github.com/cajasmota/grafel/internal/daemon/proto"
	"github.com/cajasmota/grafel/internal/install/detect"
	"github.com/cajasmota/grafel/internal/install/tooladapter"
	"github.com/cajasmota/grafel/internal/registry"
)

// newGroupCmd returns the `grafel group` parent command. Today it hosts a
// single subcommand, `add`, the non-interactive counterpart to `wizard` so
// agents and CI can register a group without a TTY. It is grouped under a
// parent (rather than flat like delete/remove) to leave room for future
// machine-friendly group operations.
func newGroupCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "group",
		Short: "Manage grafel groups (non-interactive)",
	}
	cmd.AddCommand(newGroupAddCmd())
	return cmd
}

func newGroupAddCmd() *cobra.Command {
	var (
		repoArgs  []string
		reposCSV  string
		groupDocs string
		watchers  bool
		gitHooks  bool
		rules     bool
		mcp       bool
		runInst   bool
		doIndex   bool
		jsonOut   bool
		toolsCSV  string
	)

	cmd := &cobra.Command{
		Use:   "add <group>",
		Short: "Register a group non-interactively (scriptable wizard)",
		Long: `add registers a new group entirely from flags — no prompts — so agents
and CI can automate setup. It runs the same transaction as the interactive
wizard: it writes the per-group fleet config, registers the group in the
global registry, writes per-repo committed manifests, and (unless
--install=false) runs the install transaction (git hooks, IDE rules files,
MCP settings, watchers).

Repos are supplied with --repo, repeatable, as either "slug=path" or a bare
"path" (the slug then defaults to the directory basename). --repos accepts a
comma-separated list of bare paths as a convenience.

With --index, the group is indexed via the daemon after registration so the
graph is queryable immediately (requires a running daemon).

The command is idempotent: re-running updates the registry entry in place and
overwrites the fleet config atomically.

MCP-server registration is machine-level and group-agnostic (one daemon serves
all groups), so adding a group never duplicates the mcp.json entry — query the
new group immediately via the 'group' MCP parameter.

Examples:
  grafel group add new-backend --repo core=/abs/path/to/repo --index
  grafel group add legacy --repo /abs/api --repo /abs/web --no-watchers --json`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			gaFlags := groupAddFlags{
				repoArgs:  repoArgs,
				reposCSV:  reposCSV,
				groupDocs: groupDocs,
				watchers:  watchers,
				gitHooks:  gitHooks,
				rules:     rules,
				mcp:       mcp,
				runInst:   runInst,
				doIndex:   doIndex,
				jsonOut:   jsonOut,
				tools:     toolsCSV,
			}
			return runGroupAddImpl(cmd, args[0], gaFlags, "")
		},
	}

	cmd.Flags().StringArrayVar(&repoArgs, "repo", nil,
		`repo to include as "slug=path" or bare "path" (repeatable)`)
	cmd.Flags().StringVar(&reposCSV, "repos", "",
		"comma-separated bare repo paths (slug defaults to basename)")
	cmd.Flags().StringVar(&groupDocs, "group-docs", "",
		"optional path to shared group docs")
	cmd.Flags().BoolVar(&watchers, "watchers", false,
		"install OS watcher units for each repo")
	cmd.Flags().BoolVar(&gitHooks, "git-hooks", true,
		"install git hooks (post-merge/checkout reindex)")
	cmd.Flags().BoolVar(&rules, "rules", true,
		"write per-repo IDE rules files (CLAUDE.md/.cursorrules/.windsurfrules/AGENTS.md)")
	cmd.Flags().BoolVar(&mcp, "mcp", true,
		"register/refresh MCP settings (machine-level; safe to leave on)")
	cmd.Flags().BoolVar(&runInst, "install", true,
		"run the install transaction (hooks/rules/mcp/watchers)")
	cmd.Flags().BoolVar(&doIndex, "index", false,
		"index the group via the daemon after registering (requires a running daemon)")
	cmd.Flags().BoolVar(&jsonOut, "json", false,
		"emit machine-readable JSON result")
	cmd.Flags().StringVar(&toolsCSV, "tools", "",
		"comma-separated AI coding tools whose rules files + MCP get scaffolded (e.g. claude,codex). When omitted, a pending selection from a prior `grafel install --tools` is adopted, else every supported tool is targeted. Run 'grafel tools list' for valid IDs")
	return cmd
}

type groupAddFlags struct {
	repoArgs  []string
	reposCSV  string
	groupDocs string
	watchers  bool
	gitHooks  bool
	rules     bool
	mcp       bool
	runInst   bool
	doIndex   bool
	jsonOut   bool
	tools     string // --tools CSV → GroupConfig.Tools (#5701)
}

type groupAddRepo struct {
	Slug string `json:"slug"`
	Path string `json:"path"`
}

type groupAddResult struct {
	Group      string         `json:"group"`
	ConfigPath string         `json:"config_path"`
	Repos      []groupAddRepo `json:"repos"`
	Installed  *installCounts `json:"installed,omitempty"`
	Indexed    bool           `json:"indexed"`
}

type installCounts struct {
	Hooks    int `json:"hooks"`
	Watchers int `json:"watchers"`
	MCP      int `json:"mcp"`
}

// runGroupAddImpl implements `group add` with an injectable daemon socket path
// (empty → real client.Dial). Tests pass a stub socket so --index needs no real
// daemon.
func runGroupAddImpl(cmd *cobra.Command, group string, f groupAddFlags, socketPath string) error {
	out := cmd.OutOrStdout()

	repos, err := parseRepoSpecs(f.repoArgs, f.reposCSV)
	if err != nil {
		return err
	}
	if len(repos) == 0 {
		return errors.New("supply at least one repo via --repo or --repos")
	}

	cfg := &registry.GroupConfig{Name: group, GroupDocs: f.groupDocs}
	cfg.Features.Watchers = f.watchers
	cfg.Features.GitHooks = f.gitHooks
	// Per-tool selection (#5701): an explicit --tools value wins and is validated
	// before any registration. When omitted, cfg.Tools is left empty so
	// applyGroupConfig can adopt a pending selection stashed by an earlier
	// `grafel install --tools`; absent that, the empty-means-all default applies.
	if f.tools != "" {
		ids, err := tooladapter.ParseToolsFlag(f.tools)
		if err != nil {
			return err
		}
		cfg.Tools = ids
	}
	for _, r := range repos {
		cfg.Repos = append(cfg.Repos, registry.Repo{
			Slug:  r.Slug,
			Path:  r.Path,
			Stack: registry.StackList{detect.Stack(r.Path)},
		})
	}

	// JSON mode suppresses the helper's human-readable progress lines.
	applyOut := out
	if f.jsonOut {
		applyOut = io.Discard
	}
	res, err := applyGroupConfig(applyOut, cfg, groupApplyOptions{
		RunInstall:   f.runInst,
		SkipHooks:    !f.gitHooks,
		SkipWatchers: !f.watchers,
		SkipMCP:      !f.mcp,
		SkipRules:    !f.rules,
	})
	if err != nil {
		return err
	}

	indexed := false
	if f.doIndex {
		if err := indexGroup(group, socketPath); err != nil {
			return fmt.Errorf("index group %q: %w", group, err)
		}
		indexed = true
	}

	cfgPath, _ := registry.ConfigPathFor(group)
	result := groupAddResult{
		Group:      group,
		ConfigPath: cfgPath,
		Repos:      repos,
		Indexed:    indexed,
	}
	if res != nil {
		result.Installed = &installCounts{
			Hooks:    len(res.HooksInstalled),
			Watchers: len(res.WatcherUnits),
			MCP:      len(res.MCPSettings),
		}
	}

	if f.jsonOut {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(result)
	}

	fmt.Fprintf(out, "registered group %q (%d repos: %s)\n",
		group, len(repos), strings.Join(repoSlugs(repos), ", "))
	if indexed {
		fmt.Fprintln(out, "indexed via daemon")
	}
	return nil
}

// parseRepoSpecs turns --repo entries ("slug=path" or bare "path") and the
// --repos CSV (bare paths) into absolute-path repo specs. Slugs default to the
// directory basename. Order is preserved; --repo entries come before --repos.
func parseRepoSpecs(repoArgs []string, reposCSV string) ([]groupAddRepo, error) {
	var out []groupAddRepo
	add := func(slug, path string) error {
		abs, err := filepath.Abs(strings.TrimSpace(path))
		if err != nil {
			return err
		}
		if slug == "" {
			slug = filepath.Base(abs)
		}
		out = append(out, groupAddRepo{Slug: slug, Path: abs})
		return nil
	}
	for _, raw := range repoArgs {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		slug, path := "", raw
		if i := strings.Index(raw, "="); i >= 0 {
			slug, path = strings.TrimSpace(raw[:i]), strings.TrimSpace(raw[i+1:])
		}
		if path == "" {
			return nil, fmt.Errorf("invalid --repo %q: empty path", raw)
		}
		if err := add(slug, path); err != nil {
			return nil, err
		}
	}
	for _, p := range splitCSV(reposCSV) {
		if strings.TrimSpace(p) == "" {
			continue
		}
		if err := add("", p); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func repoSlugs(repos []groupAddRepo) []string {
	out := make([]string, len(repos))
	for i, r := range repos {
		out[i] = r.Slug
	}
	return out
}

// indexGroup dials the daemon and rebuilds the whole group (all repos). An
// empty socketPath uses the real client.Dial; tests inject a stub socket.
func indexGroup(group, socketPath string) error {
	var (
		c   *client.Client
		err error
	)
	if socketPath != "" {
		c, err = client.DialPath(socketPath)
	} else {
		c, err = client.Dial()
	}
	if err != nil {
		if errors.Is(err, client.ErrDaemonNotRunning) {
			return errDaemonNotRunning
		}
		return err
	}
	defer c.Close()

	// #5328: an explicit CLI group rebuild is human-awaited → foreground.
	_, err = c.Rebuild(proto.RebuildArgs{Group: group, Interactive: true})
	return err
}
