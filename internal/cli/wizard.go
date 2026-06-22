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
	"github.com/cajasmota/grafel/internal/install/mcptools"
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
		noIndex        bool
		mcpToolsCSV    string
		noMCP          bool
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
				NoIndex:        noIndex,
				ErrOut:         cmd.ErrOrStderr(),
			}
			// Resolve the MCP-tools selection from flags (#5344). --no-mcp wins
			// and registers none; --mcp-tools=a,b registers exactly those; with
			// neither flag the selection stays nil (interactive prompt, or
			// back-compat "all detected" in non-interactive mode).
			if noMCP {
				none := []string{}
				opts.MCPTools = &none
			} else if cmd.Flags().Changed("mcp-tools") {
				ids := splitCSV(mcpToolsCSV)
				opts.MCPTools = &ids
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
	cmd.Flags().BoolVar(&noIndex, "no-index", false, "skip indexing the group at the end (default: index with live progress; requires a running daemon)")
	cmd.Flags().StringVar(&mcpToolsCSV, "mcp-tools", "", "comma-separated AI tool IDs to register the grafel MCP server in (e.g. claude,cursor); skips the interactive picker. Without this flag (and without --no-mcp), interactive runs prompt and non-interactive runs register every detected tool")
	cmd.Flags().BoolVar(&noMCP, "no-mcp", false, "do not register the grafel MCP server in any AI tool")
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
	NoIndex             bool
	// MCPTools, when non-nil, is the resolved selection of AI tool IDs to
	// register the grafel MCP server in (#5344): nil = no explicit choice
	// (prompt interactively, or all-detected non-interactively), empty = none,
	// [ids] = exactly those. Set from --mcp-tools / --no-mcp.
	MCPTools *[]string
	ErrOut   io.Writer // stderr sink for warnings; nil → os.Stderr
}

// errWriter returns the configured stderr sink, defaulting to os.Stderr.
func (o wizardOptions) errWriter() io.Writer {
	if o.ErrOut != nil {
		return o.ErrOut
	}
	return os.Stderr
}

func runWizard(out io.Writer, opts wizardOptions) error {
	cfg := &registry.GroupConfig{}
	cfg.Features.Watchers = opts.Watchers
	cfg.Features.GitHooks = opts.GitHooks
	cfg.Features.AgentHooks = opts.AgentHooks
	cfg.GroupDocs = opts.GroupDocs

	// NON-INTERACTIVE path (--repos/--parent/--exclude): unchanged flag-driven
	// discovery, for scripting. Requires --group up front.
	if opts.NonInteractive {
		if opts.GroupName == "" {
			return errors.New("group name required")
		}
		cfg.Name = opts.GroupName
		candidates, err := discoverCandidates(out, opts)
		if err != nil {
			return err
		}
		if len(candidates) == 0 {
			return errors.New("no repos selected")
		}
		for _, p := range candidates {
			abs, _ := filepath.Abs(p)
			cfg.Repos = append(cfg.Repos, registry.Repo{
				Slug:  filepath.Base(abs),
				Path:  abs,
				Stack: registry.StackList{detect.Stack(abs)},
			})
		}
		return finishWizard(out, cfg, opts)
	}

	// INTERACTIVE path — when stdin/stdout are a real terminal, drive the
	// cohesive Bubble Tea TUI (#5340): header + step rail, spacious body, footer
	// key hints, and a per-repo indexing view. The TUI owns presentation only —
	// it calls back into applyGroupConfig + the daemon index here, so all the
	// decision/registration LOGIC is preserved. A cancel registers nothing.
	if wizardUseTUI(out) {
		res, err := runInteractiveTUI(out, opts.errWriter(), opts)
		if err != nil {
			return err
		}
		if res.Cancelled {
			fmt.Fprintln(out, "cancelled — nothing was registered.")
			return nil
		}
		// All side effects (register/install/index) ran inside the TUI loop.
		return nil
	}

	// NON-TTY interactive fallback (pipes, CI, $TERM=dumb): keep the original
	// huh/line-based flow so scripts and non-terminal environments never launch
	// the full-screen TUI.
	// INTERACTIVE path — action-first (#5336). Pick an action (single / group /
	// monorepo / add-to-group) with a smart cwd default, then resolve candidates
	// per action via the shared detect.ClassifyPath classifier.
	repos, addTo, err := runInteractiveRepoSelect(out)
	if err != nil {
		return err
	}
	// "Add to existing group" short-circuits: append the chosen repos to the
	// target group's config and apply, rather than creating a new group.
	if addTo != "" {
		return addReposToExistingGroup(out, addTo, repos, opts)
	}
	if len(repos) == 0 {
		return errors.New("no repos selected")
	}
	cfg.Repos = append(cfg.Repos, repos...)

	// Group name — prompted AFTER the action so "add to existing group" can skip
	// it. Pre-fill a suggestion from the CONTAINER folder (the common parent of
	// the selected repos), not a child repo's slug: from ivivo/ holding
	// backend+frontend the default is "ivivo", not "backend" (#5338). For a
	// single repo the repo's own basename is the sensible default.
	if opts.GroupName == "" {
		opts.GroupName = defaultGroupName(repos)
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
	return finishWizard(out, cfg, opts)
}

// finishWizard runs the remaining interactive prompts (features, group docs) and
// then persists + installs the assembled group config. Shared by both the
// non-interactive and interactive paths.
func finishWizard(out io.Writer, cfg *registry.GroupConfig, opts wizardOptions) error {

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

	// Step 4b — choose which AI tools get the grafel MCP server (#5344). Only
	// prompt interactively when no explicit selection was passed via flags. The
	// non-interactive path leaves opts.MCPTools as-is (nil → all detected).
	if opts.MCPTools == nil && !opts.NonInteractive {
		sel, err := promptMCPTools()
		if err != nil {
			return err
		}
		opts.MCPTools = sel
	}

	// Steps 5-7 — persist + register + manifests + install. Shared with the
	// non-interactive `group add` command via applyGroupConfig.
	if _, err := applyGroupConfig(out, cfg, groupApplyOptions{RunInstall: opts.RunInstall, MCPTools: opts.MCPTools}); err != nil {
		return err
	}

	// Step 8 — index the freshly-registered group with live phase progress so
	// the wizard ends register → "Indexing…" → "Done", matching the dashboard
	// (#5338). Opt out with --no-index. A down daemon is a warning, not a
	// failure: the group is registered and will index later.
	//
	// The non-interactive (scripting/CI) path does NOT auto-index — it is
	// flag-driven and callers there opt in explicitly (e.g. `group add
	// --index`), so a missing daemon never breaks an automated `wizard
	// --non-interactive` run.
	if opts.NonInteractive {
		return nil
	}
	return maybeIndexGroup(out, opts.errWriter(), cfg.Name, opts.NoIndex)
}

// promptMCPTools runs the non-TTY (huh) MCP-tools picker (#5344). It returns a
// non-nil selection of tool IDs to register the grafel MCP server in, defaulted
// per the B+C heuristic (recently-used / previously-configured checked,
// remembered last choice overriding). When ≤1 tool is detected the picker is
// skipped and the detected set is auto-used (nil when none are detected, which
// preserves the all-detected back-compat behaviour downstream).
func promptMCPTools() (*[]string, error) {
	tools := mcptools.Detect()
	if len(tools) == 0 {
		return nil, nil // nothing detected — keep back-compat (register all)
	}
	if len(tools) == 1 {
		sel := []string{tools[0].ID}
		return &sel, nil
	}
	opts := make([]huh.Option[string], 0, len(tools))
	for _, t := range tools {
		label := t.DisplayName
		if t.HasGrafel {
			label += " (configured)"
		}
		opts = append(opts, huh.NewOption(label, t.ID).Selected(t.DefaultSelected))
	}
	selected := mcptools.DefaultSelection(tools)
	if err := huh.NewMultiSelect[string]().
		Title("Configure MCP for which tools?").
		Description("Your AI agents that can query this graph.\n" + navHintMulti).
		Options(opts...).
		Height(wizardListHeight(len(tools))).
		Value(&selected).
		WithTheme(wizardTheme()).
		Run(); err != nil {
		return nil, err
	}
	return &selected, nil
}

// maybeIndexGroup indexes group with live progress unless noIndex is set. A
// daemon-not-running condition is downgraded to a warning so the wizard still
// completes successfully (the group is already registered).
func maybeIndexGroup(out, errOut io.Writer, group string, noIndex bool) error {
	if noIndex {
		fmt.Fprintf(out, "skipping index (--no-index); run `grafel rebuild %s` when ready\n", group)
		return nil
	}
	if err := indexGroupWithProgress(out, errOut, group); err != nil {
		if errors.Is(err, errDaemonNotRunning) {
			fmt.Fprintf(out, "daemon not running — group registered but not indexed; run `grafel rebuild %s` once the daemon is up\n", group)
			return nil
		}
		return err
	}
	return nil
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
	// MCPTools, when non-nil, restricts MCP registration to the named tool
	// adapter IDs — the wizard's "choose which AI tools get the grafel MCP"
	// selection (#5344). nil preserves today's behaviour (all enabled tools);
	// an empty slice registers none. Threaded straight into install.Options.
	MCPTools *[]string
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
		MCPTools:       ga.MCPTools,
	})
	if err != nil {
		return nil, err
	}
	// Remember the chosen MCP tools so a later wizard run defaults to them (C,
	// #5344). Only persist when a concrete selection was made (non-nil); a nil
	// selection means "no explicit choice" and must not clobber a prior one.
	if ga.MCPTools != nil {
		if perr := mcptools.SaveLastChoice(*ga.MCPTools); perr != nil {
			fmt.Fprintf(out, "warning: could not remember MCP tool choice: %v\n", perr)
		}
	}
	fmt.Fprintf(out, "installed %d hooks, %d watchers, %d MCP entries\n",
		len(res.HooksInstalled), len(res.WatcherUnits), len(res.MCPSettings))
	for _, warn := range res.WatcherWarnings {
		fmt.Fprintf(out, "warning: %s\n", warn)
	}
	return res, nil
}

// ─────────────────────────────────────────────────────────────────────────────
// Action-first interactive flow (#5336)
// ─────────────────────────────────────────────────────────────────────────────

// wizardAction is one of the four top-level index actions.
type wizardAction string

const (
	actionSingle   wizardAction = "single"
	actionGroup    wizardAction = "group"
	actionMonorepo wizardAction = "monorepo"
	actionAddGroup wizardAction = "add-group"
)

// defaultGroupName suggests a group name for the selected repos. For a single
// repo it is that repo's basename. For multiple repos it is the basename of
// their common parent directory — the CONTAINER folder (e.g. ivivo/ for
// ivivo/backend + ivivo/frontend) — so the default is the umbrella name rather
// than an arbitrary child repo's slug (#5338). Falls back to the first repo's
// basename when no common parent can be derived.
func defaultGroupName(repos []registry.Repo) string {
	if len(repos) == 0 {
		return ""
	}
	if len(repos) == 1 {
		return filepath.Base(repos[0].Path)
	}
	parent := filepath.Dir(repos[0].Path)
	for _, r := range repos[1:] {
		if filepath.Dir(r.Path) != parent {
			// Repos don't share a single parent — fall back to the first
			// repo's container folder rather than an unrelated ancestor.
			return filepath.Base(filepath.Dir(repos[0].Path))
		}
	}
	if base := filepath.Base(parent); base != "" && base != "." && base != string(filepath.Separator) {
		return base
	}
	return filepath.Base(repos[0].Path)
}

// repoFromPath builds a registry.Repo for an absolute path, detecting its stack.
func repoFromPath(abs string) registry.Repo {
	return registry.Repo{
		Slug:  filepath.Base(abs),
		Path:  abs,
		Stack: registry.StackList{detect.Stack(abs)},
	}
}

// runInteractiveRepoSelect drives the action-first interactive flow. It returns
// the chosen repos and, when the user picked "add to existing group", the name
// of that group (in which case the repos are appended there by the caller rather
// than forming a new group). Replaces the old filepath.Dir(cwd) sibling scan.
func runInteractiveRepoSelect(out io.Writer) (repos []registry.Repo, addToGroup string, err error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, "", err
	}
	class, _ := detect.ClassifyPath(cwd)

	// Step 1 — action select. ALWAYS show all four; pre-place the cursor on the
	// smart default derived from ClassifyPath(cwd).
	action := defaultAction(class)
	if err := huh.NewSelect[wizardAction]().
		Title("What do you want to index?").
		Description(fmt.Sprintf("Detected: %s\n%s", describeClassification(class), navHintSelect)).
		Options(
			huh.NewOption("Index a single repository", actionSingle),
			huh.NewOption("Index a group of related repositories", actionGroup),
			huh.NewOption("Index a monorepo", actionMonorepo),
			huh.NewOption("Add a repository to an existing group", actionAddGroup),
		).
		Height(wizardListHeight(4)).
		Value(&action).
		WithTheme(wizardTheme()).
		Run(); err != nil {
		return nil, "", err
	}

	switch action {
	case actionSingle:
		repos, err = resolveSingleAction(class)
		return repos, "", err
	case actionGroup:
		repos, err = resolveGroupAction(out, class)
		return repos, "", err
	case actionMonorepo:
		repos, err = resolveMonorepoAction(out, class)
		return repos, "", err
	case actionAddGroup:
		return resolveAddToGroupAction(out)
	default:
		return nil, "", errors.New("no action selected")
	}
}

// defaultAction maps a classification's suggested action to a wizardAction.
func defaultAction(c detect.Classification) wizardAction {
	switch c.Suggested {
	case detect.ActionGroup:
		return actionGroup
	case detect.ActionMonorepo:
		return actionMonorepo
	case detect.ActionSingle:
		return actionSingle
	default:
		return actionSingle
	}
}

// describeClassification renders a short human label of what cwd looks like.
func describeClassification(c detect.Classification) string {
	switch {
	case len(c.ChildGitRepos) > 0:
		return fmt.Sprintf("%s holds %d git repos (%s)", filepath.Base(c.AbsPath),
			len(c.ChildGitRepos), strings.Join(c.ChildGitRepos, ", "))
	case c.Monorepo != detect.KindNone && len(c.Packages) > 0:
		return fmt.Sprintf("%s monorepo, %d packages", c.Monorepo, len(c.Packages))
	case c.IsGitRepo && len(c.SiblingGitRepos) > 0:
		return fmt.Sprintf("git repo with %d sibling repos", len(c.SiblingGitRepos))
	case c.IsGitRepo:
		return "single git repo"
	default:
		return "no git repo at " + filepath.Base(c.AbsPath)
	}
}

// resolveSingle confirms cwd when it is a git repo, else prompts for a path. No
// scan is performed.
func resolveSingleAction(class detect.Classification) ([]registry.Repo, error) {
	if class.IsGitRepo {
		confirm := true
		if err := huh.NewConfirm().
			Title(fmt.Sprintf("Index %s?", class.AbsPath)).
			Value(&confirm).Run(); err != nil {
			return nil, err
		}
		if confirm {
			return []registry.Repo{repoFromPath(class.AbsPath)}, nil
		}
	}
	abs, err := promptGitRepoPath("Path to the repository")
	if err != nil {
		return nil, err
	}
	return []registry.Repo{repoFromPath(abs)}, nil
}

// resolveGroup resolves the candidate source AUTOMATICALLY (option 1a — no
// "siblings vs parent" prompt): child git repos if present (ivivo→backend+
// frontend), elif cwd is a git repo → cwd + its siblings, else prompt for a
// folder. The candidates are shown as a filtered, scrollable [ ]/[✓] multiselect
// with a count in the title, plus an explicit "scan a different folder…" entry.
func resolveGroupAction(out io.Writer, class detect.Classification) ([]registry.Repo, error) {
	candidates := groupCandidates(class)
	for {
		if len(candidates) == 0 {
			abs, err := promptDir("Folder to scan for git repos")
			if err != nil {
				return nil, err
			}
			candidates = groupCandidates(mustClassify(abs))
			if len(candidates) == 0 {
				fmt.Fprintf(out, "no git repos found under %s\n", abs)
			}
			continue
		}
		chosen, rescan, err := multiSelectRepos(candidates)
		if err != nil {
			return nil, err
		}
		if rescan {
			abs, err := promptDir("Folder to scan for git repos")
			if err != nil {
				return nil, err
			}
			candidates = groupCandidates(mustClassify(abs))
			continue
		}
		return reposFromPaths(chosen), nil
	}
}

// groupCandidates derives the absolute candidate repo paths for the group action
// from a classification (option 1a precedence).
func groupCandidates(class detect.Classification) []string {
	if len(class.ChildGitRepos) > 0 {
		out := make([]string, 0, len(class.ChildGitRepos))
		for _, name := range class.ChildGitRepos {
			out = append(out, filepath.Join(class.AbsPath, name))
		}
		return out
	}
	if class.IsGitRepo {
		out := append([]string{class.AbsPath}, class.SiblingGitRepos...)
		sort.Strings(out)
		return out
	}
	return nil
}

// resolveMonorepo detects packages via the shared classifier and presents a
// [ ]/[✓] multiselect of package roots. Each selected package is registered as
// its own repo (its absolute sub-path) with the module recorded.
func resolveMonorepoAction(out io.Writer, class detect.Classification) ([]registry.Repo, error) {
	if class.Monorepo == detect.KindNone || len(class.Packages) == 0 {
		// cwd isn't a monorepo — let the user point at one.
		abs, err := promptDir("Path to the monorepo")
		if err != nil {
			return nil, err
		}
		class = mustClassify(abs)
		if class.Monorepo == detect.KindNone || len(class.Packages) == 0 {
			return nil, fmt.Errorf("%s is not a monorepo (no packages detected)", abs)
		}
	}
	opts := make([]huh.Option[string], 0, len(class.Packages))
	for _, p := range class.Packages {
		opts = append(opts, huh.NewOption(p, p).Selected(true))
	}
	var chosen []string
	if err := huh.NewMultiSelect[string]().
		Title(fmt.Sprintf("%d packages found", len(class.Packages))).
		Description(navHintMulti).
		Options(opts...).
		Filterable(true).
		Height(wizardListHeight(len(class.Packages))).
		Value(&chosen).
		WithTheme(wizardTheme()).
		Run(); err != nil {
		return nil, err
	}
	if len(chosen) == 0 {
		return nil, errors.New("no packages selected")
	}
	base := filepath.Base(class.AbsPath)
	repos := make([]registry.Repo, 0, len(chosen))
	for _, pkg := range chosen {
		abs := filepath.Join(class.AbsPath, filepath.FromSlash(pkg))
		repos = append(repos, registry.Repo{
			Slug:    base + "-" + filepath.Base(pkg),
			Path:    abs,
			Stack:   registry.StackList{detect.Stack(abs)},
			Modules: []string{pkg},
		})
	}
	return repos, nil
}

// resolveAddToGroup lists existing groups, lets the user pick one, then multi-add
// newly-discovered candidate repos and/or a typed path. Returns the repos and the
// target group name (non-empty signals the add-to-group path to the caller).
func resolveAddToGroupAction(out io.Writer) ([]registry.Repo, string, error) {
	groups, err := registry.Groups()
	if err != nil {
		return nil, "", err
	}
	if len(groups) == 0 {
		return nil, "", errors.New("no existing groups to add to")
	}
	gopts := make([]huh.Option[string], 0, len(groups))
	for _, g := range groups {
		gopts = append(gopts, huh.NewOption(g.Name, g.Name))
	}
	var target string
	if err := huh.NewSelect[string]().
		Title("Add to which group?").
		Description(navHintSelect).
		Options(gopts...).
		Height(wizardListHeight(len(gopts))).
		Value(&target).
		WithTheme(wizardTheme()).
		Run(); err != nil {
		return nil, "", err
	}

	// Discover candidates from cwd; let the user also scan a different folder.
	cwd, _ := os.Getwd()
	candidates := groupCandidates(mustClassify(cwd))
	var chosen []string
	if len(candidates) > 0 {
		picked, rescan, err := multiSelectRepos(candidates)
		if err != nil {
			return nil, "", err
		}
		if rescan {
			abs, err := promptDir("Folder to scan for git repos")
			if err != nil {
				return nil, "", err
			}
			picked, _, err = multiSelectRepos(groupCandidates(mustClassify(abs)))
			if err != nil {
				return nil, "", err
			}
		}
		chosen = picked
	} else {
		abs, err := promptGitRepoPath("Path to the repository to add")
		if err != nil {
			return nil, "", err
		}
		chosen = []string{abs}
	}
	if len(chosen) == 0 {
		return nil, "", errors.New("no repos selected to add")
	}
	return reposFromPaths(chosen), target, nil
}

// addReposToExistingGroup loads the target group's config, appends the new repos
// (skipping ones already present by absolute path), and re-applies it.
func addReposToExistingGroup(out io.Writer, group string, repos []registry.Repo, opts wizardOptions) error {
	if len(repos) == 0 {
		return errors.New("no repos selected to add")
	}
	cfgPath, err := registry.ConfigPathFor(group)
	if err != nil {
		return err
	}
	cfg, err := registry.LoadGroupConfig(cfgPath)
	if err != nil {
		return fmt.Errorf("load group %q: %w", group, err)
	}
	existing := map[string]struct{}{}
	for _, r := range cfg.Repos {
		existing[r.Path] = struct{}{}
	}
	added := 0
	for _, r := range repos {
		if _, dup := existing[r.Path]; dup {
			fmt.Fprintf(out, "skipping %s (already in group)\n", r.Path)
			continue
		}
		cfg.Repos = append(cfg.Repos, r)
		existing[r.Path] = struct{}{}
		added++
	}
	if added == 0 {
		return errors.New("all selected repos are already in the group")
	}
	_, err = applyGroupConfig(out, cfg, groupApplyOptions{RunInstall: opts.RunInstall})
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "added %d repo(s) to group %q\n", added, group)
	// Re-index so the newly-added repos are queryable immediately (#5338).
	return maybeIndexGroup(out, opts.errWriter(), group, opts.NoIndex)
}

// multiSelectRepos renders a scrollable, type-to-filter [ ]/[✓] multiselect of
// absolute repo paths (default all selected) plus an explicit "scan a different
// folder…" entry. Returns the chosen absolute paths, or rescan=true when the
// user picked the rescan entry.
func multiSelectRepos(candidates []string) (chosen []string, rescan bool, err error) {
	const rescanSentinel = "\x00rescan"
	opts := make([]huh.Option[string], 0, len(candidates)+1)
	for _, c := range candidates {
		opts = append(opts, huh.NewOption(c, c).Selected(true))
	}
	opts = append(opts, huh.NewOption("scan a different folder…", rescanSentinel))
	var selected []string
	if err := huh.NewMultiSelect[string]().
		Title(fmt.Sprintf("%d repos found", len(candidates))).
		Description(navHintMulti).
		Options(opts...).
		Filterable(true).
		Height(wizardListHeight(len(candidates) + 1)).
		Value(&selected).
		WithTheme(wizardTheme()).
		Run(); err != nil {
		return nil, false, err
	}
	for _, s := range selected {
		if s == rescanSentinel {
			return nil, true, nil
		}
	}
	return selected, false, nil
}

// reposFromPaths maps absolute paths to registry.Repo records.
func reposFromPaths(paths []string) []registry.Repo {
	out := make([]registry.Repo, 0, len(paths))
	for _, p := range paths {
		out = append(out, repoFromPath(p))
	}
	return out
}

// mustClassify classifies a path, swallowing the error (returns a zero-value
// Classification with AbsPath set on failure).
func mustClassify(path string) detect.Classification {
	c, err := detect.ClassifyPath(path)
	if err != nil {
		abs, _ := filepath.Abs(path)
		return detect.Classification{AbsPath: abs}
	}
	return c
}

// promptDir prompts for a directory path, expanding ~ and validating existence.
func promptDir(title string) (string, error) {
	var p string
	if err := huh.NewInput().
		Title(title).
		Validate(func(s string) error {
			abs, err := expandUser(s)
			if err != nil {
				return err
			}
			info, err := os.Stat(abs)
			if err != nil || !info.IsDir() {
				return errors.New("not a directory")
			}
			return nil
		}).
		Value(&p).Run(); err != nil {
		return "", err
	}
	return expandUser(p)
}

// promptGitRepoPath prompts for a path that must be a git repo.
func promptGitRepoPath(title string) (string, error) {
	var p string
	if err := huh.NewInput().
		Title(title).
		Validate(func(s string) error {
			abs, err := expandUser(s)
			if err != nil {
				return err
			}
			if !isGitRepo(abs) {
				return errors.New("not a git repository")
			}
			return nil
		}).
		Value(&p).Run(); err != nil {
		return "", err
	}
	return expandUser(p)
}

// expandUser resolves ~ and returns an absolute path.
func expandUser(p string) (string, error) {
	p = strings.TrimSpace(p)
	if strings.HasPrefix(p, "~") {
		home, err := os.UserHomeDir()
		if err == nil {
			p = filepath.Join(home, strings.TrimPrefix(p, "~"))
		}
	}
	return filepath.Abs(p)
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
