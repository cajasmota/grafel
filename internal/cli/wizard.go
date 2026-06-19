package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"

	"github.com/cajasmota/grafel/internal/install"
	"github.com/cajasmota/grafel/internal/install/detect"
	"github.com/cajasmota/grafel/internal/registry"
)

func newWizardCmd() *cobra.Command {
	var (
		nonInteractive bool
		groupName      string
		parentDir      string
		reposCSV       string
		repoPaths      []string
		excludes       []string
		groupDocs      string
		watchers       bool
		gitHooks       bool
		agentHooks     bool
		runInstall     bool
	)
	cmd := &cobra.Command{
		Use:   "wizard",
		Short: "Interactive setup for a new group",
		RunE: func(cmd *cobra.Command, _ []string) error {
			out := cmd.OutOrStdout()
			opts := wizardOptions{
				NonInteractive: nonInteractive,
				GroupName:      groupName,
				ParentDir:      parentDir,
				ReposCSV:       reposCSV,
				Repos:          repoPaths,
				Excludes:       excludes,
				GroupDocs:      groupDocs,
				Watchers:       watchers,
				GitHooks:       gitHooks,
				AgentHooks:     agentHooks,
				RunInstall:     runInstall,
			}
			return runWizard(out, opts)
		},
	}
	cmd.Flags().BoolVar(&nonInteractive, "non-interactive", false, "skip prompts; require all values via flags")
	cmd.Flags().StringVar(&groupName, "group", "", "group name (non-interactive)")
	cmd.Flags().StringVar(&parentDir, "parent", "", "parent dir for repo discovery (non-interactive); every git subdir is registered unless pruned with --exclude")
	cmd.Flags().StringVar(&reposCSV, "repos", "", "comma-separated explicit repo paths; registers EXACTLY these (curated set), bypassing --parent auto-discovery")
	cmd.Flags().StringArrayVar(&repoPaths, "repo", nil, "explicit repo path; repeatable; same curated semantics as --repos (combined with it)")
	cmd.Flags().StringArrayVar(&excludes, "exclude", nil, "glob matched against a candidate's basename or full path to prune --parent discovery; repeatable")
	cmd.Flags().StringVar(&groupDocs, "group-docs", "", "optional path to shared group docs")
	cmd.Flags().BoolVar(&watchers, "watchers", true, "enable watchers")
	cmd.Flags().BoolVar(&gitHooks, "git-hooks", true, "enable git hooks")
	cmd.Flags().BoolVar(&agentHooks, "agent-hooks", false, "opt-in: install the Claude Code PreToolUse grep-interceptor hook that nudges toward grafel on structural greps (advisory-only, never blocks; Claude Code only)")
	cmd.Flags().BoolVar(&runInstall, "install", true, "run install at the end")
	return cmd
}

type wizardOptions struct {
	NonInteractive      bool
	GroupName           string
	ParentDir, ReposCSV string
	Repos               []string // explicit --repo paths (combined with --repos CSV)
	Excludes            []string // --exclude globs (pruned from --parent discovery)
	GroupDocs           string
	Watchers, GitHooks  bool
	AgentHooks          bool
	RunInstall          bool
}

func runWizard(out io.Writer, opts wizardOptions) error {
	cfg := &registry.GroupConfig{}
	cfg.Features.Watchers = opts.Watchers
	cfg.Features.GitHooks = opts.GitHooks
	cfg.Features.AgentHooks = opts.AgentHooks
	cfg.GroupDocs = opts.GroupDocs

	// Step 1 — group name.
	if opts.GroupName == "" && !opts.NonInteractive {
		if err := huh.NewInput().
			Title("Group name").
			Description("Used as the registry key and the per-group config filename.").
			Validate(huh.ValidateNotEmpty()).
			Value(&opts.GroupName).
			Run(); err != nil {
			return err
		}
	}
	if opts.GroupName == "" {
		return errors.New("group name required")
	}
	cfg.Name = opts.GroupName

	// Step 2 — repo discovery.
	candidates, err := discoverCandidates(out, opts)
	if err != nil {
		return err
	}
	var chosen []string
	if opts.NonInteractive || len(candidates) == 0 {
		chosen = candidates
	} else {
		opts2 := make([]huh.Option[string], 0, len(candidates))
		for _, c := range candidates {
			opts2 = append(opts2, huh.NewOption(c, c).Selected(true))
		}
		if err := huh.NewMultiSelect[string]().
			Title("Repos to include").
			Options(opts2...).
			Value(&chosen).
			Run(); err != nil {
			return err
		}
	}
	if len(chosen) == 0 {
		return errors.New("no repos selected")
	}
	for _, p := range chosen {
		abs, _ := filepath.Abs(p)
		cfg.Repos = append(cfg.Repos, registry.Repo{
			Slug:  filepath.Base(abs),
			Path:  abs,
			Stack: registry.StackList{detect.Stack(abs)},
		})
	}

	// Step 3 — features (skip prompt; defaults from flags).
	if !opts.NonInteractive {
		if err := huh.NewConfirm().
			Title("Install watchers?").
			Value(&cfg.Features.Watchers).Run(); err != nil {
			return err
		}
		if err := huh.NewConfirm().
			Title("Install git hooks?").
			Value(&cfg.Features.GitHooks).Run(); err != nil {
			return err
		}
	}

	// Step 4 — group docs.
	if opts.GroupDocs == "" && !opts.NonInteractive {
		if err := huh.NewInput().
			Title("Path to shared group docs (optional)").
			Value(&opts.GroupDocs).Run(); err != nil {
			return err
		}
		cfg.GroupDocs = opts.GroupDocs
	}

	// Steps 5-7 — persist + register + manifests + install. Shared with the
	// non-interactive `group add` command via applyGroupConfig.
	_, err = applyGroupConfig(out, cfg, groupApplyOptions{RunInstall: opts.RunInstall})
	return err
}

// groupApplyOptions controls the side-effecting half of group registration
// (the part after a GroupConfig has been assembled, whether interactively by
// the wizard or from flags by `group add`).
type groupApplyOptions struct {
	RunInstall   bool
	SkipHooks    bool
	SkipWatchers bool
	SkipMCP      bool
	SkipRules    bool
}

// applyGroupConfig persists the group config, registers it in the global
// registry, writes the per-repo committed manifests, and — unless RunInstall
// is false — runs the install transaction (git hooks, IDE rules files, MCP
// settings, watchers, gated by the Skip* toggles and the config's Features).
// It returns the install result (nil when RunInstall is false) so callers can
// report or serialize what was written. Idempotent: re-running updates the
// registry entry in place and overwrites the config atomically.
func applyGroupConfig(out io.Writer, cfg *registry.GroupConfig, ga groupApplyOptions) (*install.Result, error) {
	cfgPath, err := registry.ConfigPathFor(cfg.Name)
	if err != nil {
		return nil, err
	}
	if err := registry.SaveGroupConfig(cfgPath, cfg); err != nil {
		return nil, err
	}
	if err := registry.AddGroup(cfg.Name, cfgPath); err != nil {
		return nil, err
	}
	fmt.Fprintf(out, "saved %s\n", cfgPath)

	if err := writeManifests(cfg); err != nil {
		fmt.Fprintf(out, "warning: writing manifest: %v\n", err)
	}

	if !ga.RunInstall {
		return nil, nil
	}
	bin, _ := os.Executable()
	res, err := install.Apply(install.Options{
		Group:          cfg.Name,
		Config:         cfg,
		BinPath:        bin,
		SkipHooks:      ga.SkipHooks,
		SkipWatchers:   ga.SkipWatchers,
		SkipMCP:        ga.SkipMCP,
		SkipRulesFiles: ga.SkipRules,
	})
	if err != nil {
		return nil, err
	}
	fmt.Fprintf(out, "installed %d hooks, %d watchers, %d MCP entries\n",
		len(res.HooksInstalled), len(res.WatcherUnits), len(res.MCPSettings))
	return res, nil
}

// discoverCandidates returns absolute paths to repos selected for this
// group. Sources, in priority order:
//
//  1. An explicit curated set — the union of the --repos CSV and any --repo
//     flags. When present this WINS: --parent auto-discovery is bypassed
//     entirely so a group can be pinned to exactly the listed repos (e.g.
//     two sibling groups that share a parent dir). Each path is resolved to
//     an absolute path and validated to exist and be a git repo; a path that
//     is missing or not a git repo is warned about and skipped. If every
//     curated path is rejected the caller gets an error rather than a silent
//     empty group.
//  2. A scan of --parent (or, when --parent is empty, the cwd's parent):
//     every immediate subdir containing a .git entry, minus any pruned by an
//     --exclude glob (matched against the basename or the full path).
func discoverCandidates(w io.Writer, opts wizardOptions) ([]string, error) {
	curated := append(splitCSV(opts.ReposCSV), opts.Repos...)
	if len(curated) > 0 {
		if opts.ParentDir != "" {
			fmt.Fprintln(w, "note: --repos/--repo given; ignoring --parent auto-discovery")
		}
		var out []string
		seen := map[string]struct{}{}
		for _, p := range curated {
			abs, err := filepath.Abs(p)
			if err != nil {
				return nil, err
			}
			if _, dup := seen[abs]; dup {
				continue
			}
			if !isGitRepo(abs) {
				fmt.Fprintf(w, "warning: skipping %q: does not exist or is not a git repo\n", abs)
				continue
			}
			seen[abs] = struct{}{}
			out = append(out, abs)
		}
		if len(out) == 0 {
			return nil, errors.New("no valid repos in --repos/--repo (each must exist and be a git repo)")
		}
		sort.Strings(out)
		return out, nil
	}
	parent := opts.ParentDir
	if parent == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return nil, err
		}
		parent = filepath.Dir(cwd)
	}
	entries, err := os.ReadDir(parent)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		full := filepath.Join(parent, e.Name())
		if !isGitRepo(full) {
			continue
		}
		if excluded(e.Name(), full, opts.Excludes) {
			fmt.Fprintf(w, "excluding %q (matched --exclude)\n", full)
			continue
		}
		out = append(out, full)
	}
	sort.Strings(out)
	return out, nil
}

// isGitRepo reports whether dir exists and contains a .git entry (dir or file,
// the latter covering git worktrees and submodules).
func isGitRepo(dir string) bool {
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		return false
	}
	_, err = os.Stat(filepath.Join(dir, ".git"))
	return err == nil
}

// excluded reports whether a discovery candidate matches any --exclude glob.
// Each glob is tried against both the basename and the full path so callers
// can write either "vendor" or "*/vendor".
func excluded(base, full string, globs []string) bool {
	for _, g := range globs {
		if ok, _ := filepath.Match(g, base); ok {
			return true
		}
		if ok, _ := filepath.Match(g, full); ok {
			return true
		}
	}
	return false
}

// writeManifests writes <repo>/.grafel/group.json into each repo so
// teammates can `grafel onboard` without needing extra context.
func writeManifests(cfg *registry.GroupConfig) error {
	m := registry.Manifest{Group: cfg.Name}
	for _, r := range cfg.Repos {
		m.Repos = append(m.Repos, struct {
			Slug     string `json:"slug"`
			CloneURL string `json:"clone_url,omitempty"`
			Stack    string `json:"stack,omitempty"`
		}{Slug: r.Slug, CloneURL: r.CloneURL, Stack: r.Stack.Primary()})
	}
	for _, r := range cfg.Repos {
		dir := filepath.Join(r.Path, ".grafel")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		path := filepath.Join(dir, "group.json")
		body := manifestJSON(m)
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			return err
		}
	}
	return nil
}

// manifestJSON is a tiny helper to keep the wizard self-contained.
func manifestJSON(m registry.Manifest) string {
	var b strings.Builder
	b.WriteString("{\n  \"group\": ")
	b.WriteString(quote(m.Group))
	b.WriteString(",\n  \"repos\": [")
	for i, r := range m.Repos {
		if i > 0 {
			b.WriteString(",")
		}
		b.WriteString("\n    {")
		b.WriteString("\"slug\": " + quote(r.Slug))
		if r.CloneURL != "" {
			b.WriteString(", \"clone_url\": " + quote(r.CloneURL))
		}
		if r.Stack != "" {
			b.WriteString(", \"stack\": " + quote(r.Stack))
		}
		b.WriteString("}")
	}
	if len(m.Repos) > 0 {
		b.WriteString("\n  ")
	}
	b.WriteString("]\n}\n")
	return b.String()
}

func quote(s string) string { return fmt.Sprintf("%q", s) }
