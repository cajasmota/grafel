package cli

// quarantine.go implements `grafel quarantine` (Q2 of the self-healing index
// quarantine epic #5394, ticket #5617).
//
// The self-healing watcher (#5616) auto-quarantines directories that churn
// pathologically (a build-output loop, generated content, …) so they stop
// arming reindexes. This command is the operator transparency + override
// surface over that set:
//
//	grafel quarantine list [group]        — show quarantined dirs per repo
//	grafel quarantine remove <repo> <dir> — manual un-quarantine
//	grafel quarantine pin    <repo> <dir> — operator override: never auto-heal
//	grafel quarantine unpin  <repo> <dir> — clear the pin
//
// Source of truth is each repo's persisted <repo>/.grafel/quarantine.json (the
// store the live tracker reloads + rewrites), so the command works daemon-less
// and stays consistent with a running daemon.

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/cajasmota/grafel/internal/daemon/watch"
	"github.com/cajasmota/grafel/internal/registry"
)

// quarantineRow is one quarantined directory, joined with its owning repo, for
// `list` output (table + JSON).
type quarantineRow struct {
	Group  string `json:"group"`
	Repo   string `json:"repo"`
	Path   string `json:"path"`
	Signal string `json:"signal"`
	Detail string `json:"detail"`
	Since  string `json:"since"`
	Pinned bool   `json:"pinned"`
}

func newQuarantineCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "quarantine",
		Short: "Inspect and manage auto-quarantined directories",
		Long: `Surface and override the self-healing index quarantine (#5394).

The watcher auto-quarantines directories that churn pathologically (a
build-output loop, generated content) so they stop arming reindexes. This
command shows what is quarantined and lets an operator override.

Examples:
  grafel quarantine list                  list quarantined dirs across all groups
  grafel quarantine list mygroup          filter to one group
  grafel quarantine remove myrepo app/dist  un-quarantine a directory
  grafel quarantine pin    myrepo app/dist  pin so auto-heal never touches it
  grafel quarantine unpin  myrepo app/dist  clear the pin`,
		Args: cobra.NoArgs,
	}
	cmd.AddCommand(
		newQuarantineListCmd(),
		newQuarantineRemoveCmd(),
		newQuarantinePinCmd(true),
		newQuarantinePinCmd(false),
	)
	return cmd
}

func newQuarantineListCmd() *cobra.Command {
	var jsonOut bool
	cmd := &cobra.Command{
		Use:   "list [group]",
		Short: "List quarantined directories per repo",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			groupFilter := ""
			if len(args) == 1 {
				groupFilter = args[0]
			}
			rows, err := collectQuarantineRows(groupFilter)
			if err != nil {
				return err
			}
			if jsonOut {
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				return enc.Encode(rows)
			}
			printQuarantineRows(cmd.OutOrStdout(), rows)
			return nil
		},
	}
	cmd.Flags().BoolVar(&jsonOut, "json", false, "machine-readable JSON output")
	return cmd
}

func newQuarantineRemoveCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "remove <repo> <dir>",
		Aliases: []string{"rm"},
		Short:   "Un-quarantine a directory (manual override)",
		Args:    cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			repoSlug, dir := args[0], args[1]
			repoPath, err := resolveRepoPath(repoSlug)
			if err != nil {
				return err
			}
			ok, err := watch.UnquarantineFile(repoPath, dir)
			if err != nil {
				return err
			}
			if !ok {
				fmt.Fprintf(cmd.OutOrStdout(), "%s/%s was not quarantined\n", repoSlug, dir)
				return nil
			}
			fmt.Fprintf(cmd.OutOrStdout(), "un-quarantined %s/%s\n", repoSlug, dir)
			return nil
		},
	}
}

// newQuarantinePinCmd builds either the `pin` or `unpin` subcommand.
func newQuarantinePinCmd(pin bool) *cobra.Command {
	use, short := "pin <repo> <dir>", "Pin a directory so auto-heal never removes it"
	if !pin {
		use, short = "unpin <repo> <dir>", "Clear an operator pin (re-enable auto-heal)"
	}
	return &cobra.Command{
		Use:   use,
		Short: short,
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			repoSlug, dir := args[0], args[1]
			repoPath, err := resolveRepoPath(repoSlug)
			if err != nil {
				return err
			}
			ok, err := watch.SetPinFile(repoPath, dir, pin)
			if err != nil {
				return err
			}
			verb := "pinned"
			if !pin {
				verb = "unpinned"
			}
			if !ok {
				fmt.Fprintf(cmd.OutOrStdout(),
					"%s/%s already %s (or not quarantined)\n", repoSlug, dir, verb)
				return nil
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s %s/%s\n", verb, repoSlug, dir)
			return nil
		},
	}
}

// collectQuarantineRows joins every registered repo's persisted quarantine set
// into a flat, sorted row list (optionally filtered to one group).
func collectQuarantineRows(groupFilter string) ([]quarantineRow, error) {
	groups, err := registry.Groups()
	if err != nil {
		return nil, err
	}
	var rows []quarantineRow
	for _, g := range groups {
		if groupFilter != "" && g.Name != groupFilter {
			continue
		}
		cfg, err := registry.LoadGroupConfig(g.ConfigPath)
		if err != nil {
			continue // skip misconfigured groups
		}
		for _, repo := range cfg.Repos {
			reasons, err := watch.ReadQuarantineFile(repo.Path)
			if err != nil {
				continue // unreadable file → treat as empty
			}
			for _, rsn := range reasons {
				rows = append(rows, quarantineRow{
					Group:  g.Name,
					Repo:   repo.Slug,
					Path:   rsn.Rel,
					Signal: rsn.Signal,
					Detail: rsn.Detail,
					Since:  formatQuarantineSince(rsn.At),
					Pinned: rsn.Pinned,
				})
			}
		}
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Group != rows[j].Group {
			return rows[i].Group < rows[j].Group
		}
		if rows[i].Repo != rows[j].Repo {
			return rows[i].Repo < rows[j].Repo
		}
		return rows[i].Path < rows[j].Path
	})
	if rows == nil {
		rows = []quarantineRow{}
	}
	return rows, nil
}

func printQuarantineRows(w io.Writer, rows []quarantineRow) {
	if len(rows) == 0 {
		fmt.Fprintln(w, "No quarantined directories.")
		return
	}
	tw := tabwriter.NewWriter(w, 0, 2, 2, ' ', 0)
	fmt.Fprintln(tw, "GROUP\tREPO\tPATH\tSIGNAL\tDETAIL\tSINCE\tPINNED")
	for _, r := range rows {
		pinned := ""
		if r.Pinned {
			pinned = "yes"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			r.Group, r.Repo, r.Path, r.Signal, r.Detail, r.Since, pinned)
	}
	_ = tw.Flush()
}

// formatQuarantineSince renders the quarantine timestamp as a short relative
// age ("3m ago"), falling back to "-" for a zero time.
func formatQuarantineSince(at time.Time) string {
	if at.IsZero() {
		return "-"
	}
	d := time.Since(at)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}

// resolveRepoPath maps a repo slug to its absolute path by scanning every
// registered group. Slugs are unique within a group; across groups the first
// match wins.
func resolveRepoPath(slug string) (string, error) {
	groups, err := registry.Groups()
	if err != nil {
		return "", err
	}
	for _, g := range groups {
		cfg, err := registry.LoadGroupConfig(g.ConfigPath)
		if err != nil {
			continue
		}
		for _, repo := range cfg.Repos {
			if repo.Slug == slug {
				return repo.Path, nil
			}
		}
	}
	return "", fmt.Errorf("repo %q not found in any registered group", slug)
}
