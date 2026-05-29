package cli

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/cajasmota/archigraph/internal/daemon"
	"github.com/cajasmota/archigraph/internal/feedback"
	"github.com/cajasmota/archigraph/internal/graph"
	"github.com/cajasmota/archigraph/internal/registry"
	"github.com/cajasmota/archigraph/internal/version"
)

// newFeedbackCmd returns the cobra command for `archigraph feedback`.
//
// The command generates a privacy-preserving markdown quality report from the
// local graph (fully offline — no network calls). The report covers extractor
// coverage, orphan rate, resolution disposition, and framework recognition.
// All entity names are replaced with per-report ephemeral 4-hex hashes.
// File paths are replaced with depth-preserved structural templates.
func newFeedbackCmd() *cobra.Command {
	var (
		groupFlag string
		outFlag   string
		yesFlag   bool
	)

	cmd := &cobra.Command{
		Use:   "feedback",
		Short: "Generate an anonymized quality report for sharing with archigraph maintainers",
		Long: `Generate a privacy-preserving markdown report covering:
  - Extractor coverage (entity kinds, source-window completeness, annotation coverage)
  - Orphan rate by entity kind
  - Resolution disposition vector
  - Framework recognition

Anonymization guarantees:
  - Entity names replaced with per-report ephemeral 4-hex hashes (salt never persisted)
  - File paths replaced with depth-preserved structural templates
  - Per-kind stats suppressed when N < 10
  - Report suppressed entirely when total entities < 50

The report is written to a local file only. No network calls. No telemetry.
The user decides whether to paste it into a GitHub issue.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runFeedback(cmd, groupFlag, outFlag, yesFlag)
		},
	}

	cmd.Flags().StringVar(&groupFlag, "group", "", "group name (default: inferred from current directory)")
	cmd.Flags().StringVar(&outFlag, "out", "", "output path (default: ~/.archigraph/feedback/<group>-<timestamp>.md)")
	cmd.Flags().BoolVar(&yesFlag, "yes", false, "skip confirmation prompt (for CI / scripting)")
	cmd.AddCommand(newFeedbackRollupCmd())
	cmd.AddCommand(newFeedbackTimelineCmd())
	return cmd
}

// runFeedback is the implementation of `archigraph feedback`.
func runFeedback(cmd *cobra.Command, groupName, outPath string, yes bool) error {
	w := cmd.OutOrStdout()

	// 1. Resolve group name.
	if groupName == "" {
		resolved, err := inferGroupFromCWD()
		if err != nil {
			return fmt.Errorf("could not infer group from current directory: %w\nUse --group <name> to specify a group explicitly.", err)
		}
		groupName = resolved
	}

	// 2. Load group config to discover repos.
	reg, err := registry.Load()
	if err != nil {
		return fmt.Errorf("feedback: load registry: %w", err)
	}
	var groupRef *registry.GroupRef
	for i := range reg.Groups {
		if reg.Groups[i].Name == groupName {
			groupRef = &reg.Groups[i]
			break
		}
	}
	if groupRef == nil {
		return fmt.Errorf("feedback: group %q not found in registry (run `archigraph list` to see available groups)", groupName)
	}

	cfg, err := registry.LoadGroupConfig(groupRef.ConfigPath)
	if err != nil {
		return fmt.Errorf("feedback: load group config: %w", err)
	}

	// 3. Resolve output path.
	if outPath == "" {
		ts := time.Now().UTC().Format("20060102T150405")
		home, err := registry.HomeDir()
		if err != nil {
			return fmt.Errorf("feedback: resolve home dir: %w", err)
		}
		outDir := filepath.Join(home, "feedback")
		if err := os.MkdirAll(outDir, 0o755); err != nil {
			return fmt.Errorf("feedback: create output dir: %w", err)
		}
		outPath = filepath.Join(outDir, groupName+"-"+ts+".md")
	}

	// 4. Show confirmation prompt (skipped with --yes).
	totalEntities, totalRels, nRepos := quickGroupStats(cfg.Repos)
	if !yes {
		fmt.Fprintf(w, "archigraph feedback — anonymized quality report\n")
		fmt.Fprintf(w, "Group: %s (%d repos, ~%d entities, ~%d relationships)\n\n",
			groupName, nRepos, totalEntities, totalRels)
		fmt.Fprintf(w, "What will be collected:\n")
		fmt.Fprintf(w, "  + Extractor coverage stats (no source text)\n")
		fmt.Fprintf(w, "  + Orphan rates by entity kind\n")
		fmt.Fprintf(w, "  + Resolution disposition vector\n")
		fmt.Fprintf(w, "  + Framework detector hit counts\n\n")
		fmt.Fprintf(w, "What will NOT be collected:\n")
		fmt.Fprintf(w, "  - Source code\n")
		fmt.Fprintf(w, "  - File paths (depth structure only, segments replaced)\n")
		fmt.Fprintf(w, "  - Identifier names (4-hex hashes, per-report ephemeral salt)\n")
		fmt.Fprintf(w, "  - Any network calls (fully offline)\n\n")
		fmt.Fprintf(w, "Report will be written to: %s\n\n", outPath)
		fmt.Fprintf(w, "Proceed? [y/N] ")

		// Read confirmation.
		scanner := bufio.NewScanner(os.Stdin)
		scanner.Scan()
		answer := strings.TrimSpace(strings.ToLower(scanner.Text()))
		if answer != "y" && answer != "yes" {
			fmt.Fprintln(w, "Cancelled.")
			return nil
		}
	}

	// 5. Load graphs for all repos.
	var docs []*graph.Document
	for _, repo := range cfg.Repos {
		stateDir := daemon.StateDirForRepo(repo.Path)
		doc, err := graph.LoadGraphFromDir(stateDir)
		if err != nil {
			// Non-fatal: log and skip repos with missing graphs.
			fmt.Fprintf(w, "warning: skipping repo %s (graph not found: %v)\n", repo.Slug, err)
			continue
		}
		docs = append(docs, doc)
	}

	if len(docs) == 0 {
		return fmt.Errorf("feedback: no indexed graphs found for group %q — run `archigraph index` first", groupName)
	}

	// 6. Generate report.
	fmt.Fprintln(w, "Generating report...")
	report, err := feedback.Generate(context.Background(), docs, feedback.Opts{
		GroupName: groupName,
		Version:   version.String(),
	})
	if err != nil {
		return fmt.Errorf("feedback: generate: %w", err)
	}

	// 7. Write to file.
	f, err := os.Create(outPath)
	if err != nil {
		return fmt.Errorf("feedback: create output file: %w", err)
	}
	defer f.Close()

	if err := feedback.Render(f, report); err != nil {
		return fmt.Errorf("feedback: render: %w", err)
	}

	if report.IsSuppressed() {
		fmt.Fprintf(w, "\nReport suppressed: group has fewer than 50 entities (got %d).\n", report.TotalEntities)
		fmt.Fprintf(w, "Index a larger group and re-run `archigraph feedback`.\n")
		fmt.Fprintf(w, "Suppression notice written to: %s\n", outPath)
		return nil
	}

	fmt.Fprintf(w, "\nReport written to: %s\n", outPath)
	passing := 0
	for i := range report.SanityResults {
		if report.SanityResults[i].Passed {
			passing++
		}
	}
	fmt.Fprintf(w, "Confidence: %d%% (%d/%d sanity checks passed)\n",
		report.Confidence, passing, len(report.SanityResults))
	fmt.Fprintf(w, "\nVerify the report by opening the .md file and scanning for any unhashed paths or\n")
	fmt.Fprintf(w, "identifiers before sharing. Then file a GitHub issue using the template at:\n")
	fmt.Fprintf(w, "  https://github.com/cajasmota/archigraph/issues/new?template=feedback-report.yml\n")
	return nil
}

// inferGroupFromCWD attempts to find a registered group whose repos include
// the current working directory (or a parent directory thereof).
func inferGroupFromCWD() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	reg, err := registry.Load()
	if err != nil {
		return "", err
	}
	for _, g := range reg.Groups {
		cfg, err := registry.LoadGroupConfig(g.ConfigPath)
		if err != nil {
			continue
		}
		for _, repo := range cfg.Repos {
			if repo.Path == "" {
				continue
			}
			abs, err := filepath.Abs(repo.Path)
			if err != nil {
				continue
			}
			// Match if cwd is the repo path or a subdirectory thereof.
			rel, err := filepath.Rel(abs, cwd)
			if err == nil && !strings.HasPrefix(rel, "..") {
				return g.Name, nil
			}
		}
	}
	return "", fmt.Errorf("no registered group contains directory %s", cwd)
}

// quickGroupStats reads graph-stats.json for each repo to get fast approximate
// totals for the confirmation prompt. Falls back to zeroes on error.
func quickGroupStats(repos []registry.Repo) (totalEntities, totalRels, nRepos int) {
	nRepos = len(repos)
	for _, repo := range repos {
		stateDir := daemon.StateDirForRepo(repo.Path)
		doc, err := graph.LoadGraphFromDir(stateDir)
		if err != nil {
			continue
		}
		totalEntities += len(doc.Entities)
		totalRels += len(doc.Relationships)
	}
	return
}
