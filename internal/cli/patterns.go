// Package cli — `archigraph patterns` subcommand surface (ADR-0018, PR δ).
//
// All verbs operate directly on the per-group patterns.json store. The
// daemon owns mutation under high contention (the MCP tool's record /
// apply / refine paths); the CLI surface here is for humans inspecting
// and editing patterns out-of-band, so direct file IO is appropriate and
// matches the ADR-0017 thin-client model for read paths.
package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/cajasmota/archigraph/internal/agentpatterns"
	"github.com/cajasmota/archigraph/internal/registry"
)

// newPatternsCmd is the entrypoint surfaced from root.go.
func newPatternsCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "patterns",
		Short: "Manage agent-learned patterns (ADR-0018)",
		Long: `Inspect, edit, export, and configure the per-group pattern store.

Patterns are first-class graph entities that store codebase-specific
recipes learned by agents over time. They live at
<group>/.archigraph/patterns.json and are written by the MCP
archigraph_patterns tool.`,
	}

	root.AddCommand(
		newPatternsListCmd(),
		newPatternsShowCmd(),
		newPatternsEditCmd(),
		newPatternsDeleteCmd(),
		newPatternsExportCmd(),
		newPatternsImportCmd(),
		newPatternsConfigCmd(),
		newPatternsGCCmd(),
	)
	return root
}

// ---------------------------------------------------------------------------
// shared helpers
// ---------------------------------------------------------------------------

// resolvePatternsDir returns the per-group .archigraph/ dir. Mirrors the
// MCP server's patternsDir() logic (memory_dir override → default).
func resolvePatternsDir(groupName string) (string, error) {
	if groupName == "" {
		groups, err := registry.Groups()
		if err != nil {
			return "", err
		}
		if len(groups) == 0 {
			return "", errors.New("no groups registered; pass --group or run `archigraph wizard`")
		}
		if len(groups) > 1 {
			return "", errors.New("multiple groups registered; pass --group <name>")
		}
		groupName = groups[0].Name
	}

	// Try config file first (memory_dir override).
	cfgPath, err := registry.ConfigPathFor(groupName)
	if err == nil {
		if data, err := os.ReadFile(cfgPath); err == nil {
			var inline struct {
				MemoryDir string `json:"memory_dir,omitempty"`
			}
			_ = json.Unmarshal(data, &inline)
			if inline.MemoryDir != "" {
				return inline.MemoryDir, nil
			}
		}
	}

	home, err := registry.HomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "groups", groupName+"-patterns"), nil
}

func addGroupFlag(cmd *cobra.Command, target *string) {
	cmd.Flags().StringVar(target, "group", "", "group name (defaults to sole registered group)")
}

// ---------------------------------------------------------------------------
// list
// ---------------------------------------------------------------------------

func newPatternsListCmd() *cobra.Command {
	var group string
	var needsAttention bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List patterns (table)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			dir, err := resolvePatternsDir(group)
			if err != nil {
				return err
			}
			patterns, err := agentpatterns.Load(dir)
			if err != nil {
				return err
			}
			if needsAttention {
				patterns = filterNeedsAttention(patterns)
			}
			out := cmd.OutOrStdout()
			if len(patterns) == 0 {
				fmt.Fprintln(out, "No patterns found.")
				return nil
			}
			return writePatternsTable(out, patterns)
		},
	}
	addGroupFlag(cmd, &group)
	cmd.Flags().BoolVar(&needsAttention, "needs-attention", false,
		"only show rejected, low-confidence (<0.3), or stale (>90 days) patterns")
	return cmd
}

func filterNeedsAttention(patterns []agentpatterns.Pattern) []agentpatterns.Pattern {
	out := make([]agentpatterns.Pattern, 0, len(patterns))
	now := time.Now().Unix()
	const staleSecs = int64(90 * 24 * 3600)
	for _, p := range patterns {
		if p.RejectTimestamp > 0 {
			out = append(out, p)
			continue
		}
		if p.Confidence < 0.3 {
			out = append(out, p)
			continue
		}
		if p.LastApplied > 0 && now-p.LastApplied > staleSecs {
			out = append(out, p)
			continue
		}
	}
	return out
}

func writePatternsTable(w io.Writer, patterns []agentpatterns.Pattern) error {
	sort.Slice(patterns, func(i, j int) bool { return patterns[i].ID < patterns[j].ID })
	fmt.Fprintf(w, "%-16s  %-8s  %-12s  %-5s  %-4s  %-10s  %-3s  %s\n",
		"ID", "CAND?", "CATEGORY", "CONF", "OBS", "LAST_APP", "ANT", "TRIGGER")
	for _, p := range patterns {
		trig := p.Trigger.NaturalLanguage
		if len(trig) > 60 {
			trig = trig[:57] + "..."
		}
		la := "—"
		if p.LastApplied > 0 {
			la = time.Unix(p.LastApplied, 0).UTC().Format("2006-01-02")
		}
		fmt.Fprintf(w, "%-16s  %-8t  %-12s  %4.2f   %4d  %-10s  %3d  %s\n",
			p.ID, p.IsCandidate, p.Category, p.Confidence, p.Observations, la, len(p.AntiPatterns), trig)
	}
	return nil
}

// ---------------------------------------------------------------------------
// show
// ---------------------------------------------------------------------------

func newPatternsShowCmd() *cobra.Command {
	var group string
	cmd := &cobra.Command{
		Use:   "show <id>",
		Short: "Pretty-print a single pattern",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, err := resolvePatternsDir(group)
			if err != nil {
				return err
			}
			patterns, err := agentpatterns.Load(dir)
			if err != nil {
				return err
			}
			p := agentpatterns.ByID(patterns, args[0])
			if p == nil {
				return fmt.Errorf("pattern %q not found", args[0])
			}
			data, err := json.MarshalIndent(p, "", "  ")
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), string(data))
			return nil
		},
	}
	addGroupFlag(cmd, &group)
	return cmd
}

// ---------------------------------------------------------------------------
// edit
// ---------------------------------------------------------------------------

func newPatternsEditCmd() *cobra.Command {
	var group string
	cmd := &cobra.Command{
		Use:   "edit <id>",
		Short: "Open the pattern JSON in $EDITOR and re-save on close",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, err := resolvePatternsDir(group)
			if err != nil {
				return err
			}
			patterns, err := agentpatterns.Load(dir)
			if err != nil {
				return err
			}
			p := agentpatterns.ByID(patterns, args[0])
			if p == nil {
				return fmt.Errorf("pattern %q not found", args[0])
			}
			data, err := json.MarshalIndent(p, "", "  ")
			if err != nil {
				return err
			}
			editor := os.Getenv("EDITOR")
			if editor == "" {
				editor = "vi"
			}
			tmp, err := os.CreateTemp("", "archigraph-pattern-*.json")
			if err != nil {
				return err
			}
			tmpPath := tmp.Name()
			defer os.Remove(tmpPath)
			if _, err := tmp.Write(data); err != nil {
				return err
			}
			if err := tmp.Close(); err != nil {
				return err
			}

			cmdExec := exec.Command(editor, tmpPath)
			cmdExec.Stdin = os.Stdin
			cmdExec.Stdout = os.Stdout
			cmdExec.Stderr = os.Stderr
			if err := cmdExec.Run(); err != nil {
				return fmt.Errorf("editor: %w", err)
			}
			edited, err := os.ReadFile(tmpPath)
			if err != nil {
				return err
			}
			var updated agentpatterns.Pattern
			if err := json.Unmarshal(edited, &updated); err != nil {
				return fmt.Errorf("invalid JSON after edit: %w", err)
			}
			if updated.ID != p.ID {
				return fmt.Errorf("pattern ID may not change (%s → %s); use record/delete", p.ID, updated.ID)
			}
			patterns = agentpatterns.Upsert(patterns, updated)
			if err := agentpatterns.Save(dir, patterns); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "saved %s\n", updated.ID)
			return nil
		},
	}
	addGroupFlag(cmd, &group)
	return cmd
}

// ---------------------------------------------------------------------------
// delete
// ---------------------------------------------------------------------------

func newPatternsDeleteCmd() *cobra.Command {
	var group string
	var force bool
	cmd := &cobra.Command{
		Use:   "delete <id>",
		Short: "Delete a pattern",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, err := resolvePatternsDir(group)
			if err != nil {
				return err
			}
			patterns, err := agentpatterns.Load(dir)
			if err != nil {
				return err
			}
			p := agentpatterns.ByID(patterns, args[0])
			if p == nil {
				return fmt.Errorf("pattern %q not found", args[0])
			}
			if !force {
				fmt.Fprintf(cmd.OutOrStdout(), "About to delete %s (%s). Re-run with --force to confirm.\n",
					p.ID, p.Trigger.NaturalLanguage)
				return nil
			}
			out := make([]agentpatterns.Pattern, 0, len(patterns)-1)
			for _, q := range patterns {
				if q.ID != args[0] {
					out = append(out, q)
				}
			}
			if err := agentpatterns.Save(dir, out); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "deleted %s\n", args[0])
			return nil
		},
	}
	addGroupFlag(cmd, &group)
	cmd.Flags().BoolVar(&force, "force", false, "actually delete (otherwise: dry-run)")
	return cmd
}

// ---------------------------------------------------------------------------
// export / import (delegate to internal/agentpatterns/sync.go)
// ---------------------------------------------------------------------------

func newPatternsExportCmd() *cobra.Command {
	var group, repo, file string
	cmd := &cobra.Command{
		Use:   "export",
		Short: "Export patterns into the CLAUDE.md marker block",
		RunE: func(cmd *cobra.Command, _ []string) error {
			dir, err := resolvePatternsDir(group)
			if err != nil {
				return err
			}
			patterns, err := agentpatterns.Load(dir)
			if err != nil {
				return err
			}
			target := file
			if target == "" {
				if repo == "" {
					return errors.New("pass --repo <path> or --file <path>")
				}
				target = filepath.Join(repo, "CLAUDE.md")
			}
			if err := agentpatterns.UpsertFile(target, patterns, agentpatterns.ExportOptions{}); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "exported %d patterns to %s\n",
				countApproved(patterns), target)
			return nil
		},
	}
	addGroupFlag(cmd, &group)
	cmd.Flags().StringVar(&repo, "repo", "", "repo path (writes <repo>/CLAUDE.md)")
	cmd.Flags().StringVar(&file, "file", "", "explicit target file (overrides --repo)")
	return cmd
}

func newPatternsImportCmd() *cobra.Command {
	var group, repo, file string
	cmd := &cobra.Command{
		Use:   "import",
		Short: "Report patterns in CLAUDE.md missing from the store",
		RunE: func(cmd *cobra.Command, _ []string) error {
			dir, err := resolvePatternsDir(group)
			if err != nil {
				return err
			}
			patterns, err := agentpatterns.Load(dir)
			if err != nil {
				return err
			}
			target := file
			if target == "" {
				if repo == "" {
					return errors.New("pass --repo <path> or --file <path>")
				}
				target = filepath.Join(repo, "CLAUDE.md")
			}
			report, err := agentpatterns.Diff(target, patterns, agentpatterns.ExportOptions{})
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "patterns in CLAUDE.md but missing from store (%d):\n", len(report.InBlockOnly))
			for _, r := range report.InBlockOnly {
				fmt.Fprintf(out, "  - %s\n", r.TriggerLine)
			}
			fmt.Fprintf(out, "patterns in store but missing from CLAUDE.md (%d):\n", len(report.InStoreOnly))
			for _, p := range report.InStoreOnly {
				fmt.Fprintf(out, "  - %s (%s)\n", p.Trigger.NaturalLanguage, p.ID)
			}
			return nil
		},
	}
	addGroupFlag(cmd, &group)
	cmd.Flags().StringVar(&repo, "repo", "", "repo path (reads <repo>/CLAUDE.md)")
	cmd.Flags().StringVar(&file, "file", "", "explicit source file (overrides --repo)")
	return cmd
}

func countApproved(patterns []agentpatterns.Pattern) int {
	n := 0
	for _, p := range patterns {
		if !p.IsCandidate {
			n++
		}
	}
	return n
}

// ---------------------------------------------------------------------------
// config
// ---------------------------------------------------------------------------

func newPatternsConfigCmd() *cobra.Command {
	var group string
	cmd := &cobra.Command{
		Use:   "config [key=value]",
		Short: "Get or set pattern config (no args lists current values)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, err := resolvePatternsDir(group)
			if err != nil {
				return err
			}
			cfg, err := agentpatterns.LoadConfig(dir)
			if err != nil {
				return err
			}
			if len(args) == 0 {
				return printConfig(cmd.OutOrStdout(), cfg)
			}
			parts := strings.SplitN(args[0], "=", 2)
			if len(parts) != 2 {
				return fmt.Errorf("expected key=value, got %q", args[0])
			}
			cfg, err = agentpatterns.SetConfigKey(cfg, parts[0], parts[1])
			if err != nil {
				return err
			}
			if err := agentpatterns.SaveConfig(dir, cfg); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "set %s\n", args[0])
			return nil
		},
	}
	addGroupFlag(cmd, &group)
	return cmd
}

func printConfig(w io.Writer, cfg agentpatterns.Config) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	fmt.Fprintln(w, string(data))
	return nil
}

// ---------------------------------------------------------------------------
// gc (v1.1 — stub that respects --dry-run)
// ---------------------------------------------------------------------------

func newPatternsGCCmd() *cobra.Command {
	var group string
	var dryRun bool
	cmd := &cobra.Command{
		Use:   "gc",
		Short: "Prune candidate patterns older than candidate_decay_days (v1.1)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			dir, err := resolvePatternsDir(group)
			if err != nil {
				return err
			}
			patterns, err := agentpatterns.Load(dir)
			if err != nil {
				return err
			}
			cfg, err := agentpatterns.LoadConfig(dir)
			if err != nil {
				return err
			}
			cutoff := time.Now().Add(-time.Duration(cfg.CandidateDecayDays) * 24 * time.Hour).Unix()
			var keep []agentpatterns.Pattern
			var pruned []agentpatterns.Pattern
			for _, p := range patterns {
				if p.IsCandidate && p.LastValidated > 0 && p.LastValidated < cutoff {
					pruned = append(pruned, p)
					continue
				}
				keep = append(keep, p)
			}
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "candidates eligible for pruning (older than %d days): %d\n",
				cfg.CandidateDecayDays, len(pruned))
			for _, p := range pruned {
				fmt.Fprintf(out, "  - %s (%s)\n", p.ID, p.Trigger.NaturalLanguage)
			}
			if dryRun {
				fmt.Fprintln(out, "(dry-run; no changes written)")
				return nil
			}
			if len(pruned) == 0 {
				return nil
			}
			if err := agentpatterns.Save(dir, keep); err != nil {
				return err
			}
			fmt.Fprintf(out, "pruned %d candidates\n", len(pruned))
			return nil
		},
	}
	addGroupFlag(cmd, &group)
	cmd.Flags().BoolVar(&dryRun, "dry-run", true, "report what would be pruned but do not write (default true; pass --dry-run=false to actually prune)")
	return cmd
}
