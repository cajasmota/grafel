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

	"github.com/cajasmota/archigraph/internal/install"
	"github.com/cajasmota/archigraph/internal/install/detect"
	"github.com/cajasmota/archigraph/internal/registry"
)

func newWizardCmd() *cobra.Command {
	var (
		nonInteractive bool
		groupName      string
		parentDir      string
		reposCSV       string
		groupDocs      string
		watchers       bool
		gitHooks       bool
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
				GroupDocs:      groupDocs,
				Watchers:       watchers,
				GitHooks:       gitHooks,
				RunInstall:     runInstall,
			}
			return runWizard(out, opts)
		},
	}
	cmd.Flags().BoolVar(&nonInteractive, "non-interactive", false, "skip prompts; require all values via flags")
	cmd.Flags().StringVar(&groupName, "group", "", "group name (non-interactive)")
	cmd.Flags().StringVar(&parentDir, "parent", "", "parent dir for repo discovery (non-interactive)")
	cmd.Flags().StringVar(&reposCSV, "repos", "", "comma-separated repo paths (non-interactive)")
	cmd.Flags().StringVar(&groupDocs, "group-docs", "", "optional path to shared group docs")
	cmd.Flags().BoolVar(&watchers, "watchers", true, "enable watchers")
	cmd.Flags().BoolVar(&gitHooks, "git-hooks", true, "enable git hooks")
	cmd.Flags().BoolVar(&runInstall, "install", true, "run install at the end")
	return cmd
}

type wizardOptions struct {
	NonInteractive      bool
	GroupName           string
	ParentDir, ReposCSV string
	GroupDocs           string
	Watchers, GitHooks  bool
	RunInstall          bool
}

func runWizard(out io.Writer, opts wizardOptions) error {
	cfg := &registry.GroupConfig{}
	cfg.Features.Watchers = opts.Watchers
	cfg.Features.GitHooks = opts.GitHooks
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
	candidates, err := discoverCandidates(opts)
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
			Stack: detect.Stack(abs),
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

	// Step 5 — save config + register group.
	cfgPath, err := registry.ConfigPathFor(cfg.Name)
	if err != nil {
		return err
	}
	if err := registry.SaveGroupConfig(cfgPath, cfg); err != nil {
		return err
	}
	if err := registry.AddGroup(cfg.Name, cfgPath); err != nil {
		return err
	}
	fmt.Fprintf(out, "saved %s\n", cfgPath)

	// Step 6 — write committed manifest in each repo.
	if err := writeManifests(cfg); err != nil {
		fmt.Fprintf(out, "warning: writing manifest: %v\n", err)
	}

	// Step 7 — install.
	if opts.RunInstall {
		bin, _ := os.Executable()
		res, err := install.Apply(install.Options{
			Group:   cfg.Name,
			Config:  cfg,
			BinPath: bin,
		})
		if err != nil {
			return err
		}
		fmt.Fprintf(out, "installed %d hooks, %d watchers, %d MCP entries\n",
			len(res.HooksInstalled), len(res.WatcherUnits), len(res.MCPSettings))
	}
	return nil
}

// discoverCandidates returns absolute paths to repos selected for this
// group. Sources, in order: explicit --repos CSV, scan of --parent, or
// the cwd's parent.
func discoverCandidates(opts wizardOptions) ([]string, error) {
	if opts.ReposCSV != "" {
		out := splitCSV(opts.ReposCSV)
		for i, p := range out {
			abs, err := filepath.Abs(p)
			if err != nil {
				return nil, err
			}
			out[i] = abs
		}
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
		if _, err := os.Stat(filepath.Join(full, ".git")); err == nil {
			out = append(out, full)
		}
	}
	sort.Strings(out)
	return out, nil
}

// writeManifests writes <repo>/.archigraph/group.json into each repo so
// teammates can `archigraph onboard` without needing extra context.
func writeManifests(cfg *registry.GroupConfig) error {
	m := registry.Manifest{Group: cfg.Name}
	for _, r := range cfg.Repos {
		m.Repos = append(m.Repos, struct {
			Slug     string `json:"slug"`
			CloneURL string `json:"clone_url,omitempty"`
			Stack    string `json:"stack,omitempty"`
		}{Slug: r.Slug, CloneURL: r.CloneURL, Stack: r.Stack})
	}
	for _, r := range cfg.Repos {
		dir := filepath.Join(r.Path, ".archigraph")
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
