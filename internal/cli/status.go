package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/spf13/cobra"

	"github.com/cajasmota/grafel/internal/daemon"
	"github.com/cajasmota/grafel/internal/daemon/client"
	"github.com/cajasmota/grafel/internal/daemon/sched"
	"github.com/cajasmota/grafel/internal/daemon/worktree"
	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/registry"
)

// newStatusCmd reports both daemon health and per-group index state.
// Status is crash-safe: if the daemon is down we print "daemon not
// running" and continue with the registry view, rather than erroring.
func newStatusCmd() *cobra.Command {
	var refFlag string
	var jsonFlag bool
	cmd := &cobra.Command{
		Use:   "status [group]",
		Short: "Show daemon + index status",
		Long: `Show daemon health and per-repo index state.

When --ref is supplied the registry view is filtered to repos that have a
graph stored for that ref.  Use --ref @all to show state across every known
ref (implies a per-ref breakdown in the output).

--json switches to the poll-safe status-plane read (#5725/#5729-W1): it
resolves the current directory to its registered repo and prints the
on-disk status/heartbeat sidecar as JSON, WITHOUT dialing the daemon. It
always returns promptly — even while the daemon is mid-index for this
repo — falling back to {"status":"unknown"} rather than hanging or erroring
when no engine has ever written a status file for this repo.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if jsonFlag {
				cwd, err := os.Getwd()
				if err != nil {
					return err
				}
				return runStatusJSON(cmd.OutOrStdout(), cwd)
			}
			filterGroup := ""
			if len(args) == 1 {
				filterGroup = args[0]
			}
			resolvedRef, isAll, err := resolveRef(refFlag, true /* @all ok */)
			if err != nil {
				return err
			}
			return runStatus(cmd.OutOrStdout(), filterGroup, resolvedRef, isAll)
		},
	}
	cmd.Flags().StringVar(&refFlag, "ref", "",
		refFlagUsage)
	cmd.Flags().BoolVar(&jsonFlag, "json", false,
		"poll-safe, cwd-scoped status-plane read: prints the on-disk status file for the current repo without dialing the daemon")
	return cmd
}

// runStatus implements the status command.
//
// filter    — optional group name filter (positional arg).
// ref       — "" means current HEAD (default / @current); a named ref
//
//	narrows the view to that ref's graph state.
//
// showAll   — true when --ref @all was passed; shows a per-ref breakdown.
func runStatus(w io.Writer, filter string, ref string, showAll bool) error {
	if showAll {
		fmt.Fprintln(w, "Note: --ref @all shows per-ref graph state for all known refs.")
		fmt.Fprintln(w)
	} else if ref != "" {
		fmt.Fprintf(w, "Note: showing state for ref %q.\n\n", ref)
	}
	// Daemon section first — gives the operator a fast-glance view.
	c, err := client.Dial()
	switch {
	case err == nil:
		defer c.Close()
		st, statErr := c.Status()
		if statErr != nil {
			fmt.Fprintf(w, "Daemon: running (status rpc failed: %v)\n", statErr)
		} else {
			// Check for binary mismatch (#855).
			currentBin, _ := os.Executable()
			if st.BinaryPath != "" && currentBin != "" &&
				filepath.Clean(st.BinaryPath) != filepath.Clean(currentBin) {
				fmt.Fprintf(w, "Daemon: running (binary mismatch)\n")
				fmt.Fprintf(w, "  ⚠️ DAEMON MISMATCH: status shows a daemon from %s, but you ran %s.\n",
					st.BinaryPath, currentBin)
				fmt.Fprintf(w, "  The %s binary is likely stale. Run: grafel doctor --kill-stale && grafel start\n",
					st.BinaryPath)
				fmt.Fprintf(w, "  version: %s (from %s)\n", st.Version, st.BinaryPath)
				fmt.Fprintf(w, "  socket:  %s\n", st.SocketPath)
			} else {
				uptime := time.Duration(st.UptimeSec) * time.Second
				fmt.Fprintf(w, "Daemon: running  pid=%d  uptime=%s  rss=%s  in_flight=%d\n",
					st.PID, uptime, humanBytes(st.RSSBytes), st.InFlight)
				fmt.Fprintf(w, "  version: %s\n", st.Version)
				fmt.Fprintf(w, "  socket:  %s\n", st.SocketPath)
				if st.DaemonMode != "" {
					fmt.Fprintf(w, "  mode:    %s\n", st.DaemonMode)
				}
				if st.DashboardPort > 0 {
					fmt.Fprintf(w, "  dashboard: http://127.0.0.1:%d/\n", st.DashboardPort)
				}
				// Reindex-storm mitigations (#5231): incremental file-level
				// reindex (default ON) and subprocess/out-of-daemon indexing
				// (default OFF). Read from the local process env, which a
				// co-located daemon shares.
				var ecfg *extractor.ExtractorConfig // nil → reads env
				fmt.Fprintf(w, "  indexing: incremental=%s subprocess=%s\n",
					onOff(ecfg.IsIncrementalEnabled()), onOff(sched.SubprocessIndexEnabled()))
				// Go soft memory limit (#5237): show the resolved limit +
				// source so operators can see what's bounding daemon RSS.
				// #6045: in split mode TWO processes share this budget, so the
				// line names the total and both per-plane shares.
				memVal, memSrc := memLimitDescription()
				fmt.Fprintf(w, "  mem_limit: %s (%s)\n", memVal, memSrc)
			}
			printDaemonDetail(w, st)
		}
	case errors.Is(err, client.ErrDaemonNotRunning):
		fmt.Fprintln(w, "Daemon: not running")
	default:
		fmt.Fprintf(w, "Daemon: error: %v\n", err)
	}

	// Per-repo watcher fleet. This section exists so a stop is VERIFIABLE.
	// The `com.grafel.watcher.<group>.<slug>` units are owned by
	// launchd/systemd/schtasks and index independently of the daemon, so
	// "Daemon: not running" on its own says nothing about whether grafel is
	// still doing work. If watchers survived a `grafel stop`, this line is what
	// says so out loud instead of leaving the user to infer it from CPU load.
	printWatcherFleetStatus(w)

	// Registry / per-repo view stays — useful even when the daemon is
	// down so users can see what would be indexed once they `start`.
	groups, err := registry.Groups()
	if err != nil {
		return err
	}
	for _, g := range groups {
		if filter != "" && g.Name != filter {
			continue
		}

		// Check if config file exists (#854).
		_, statErr := os.Stat(g.ConfigPath)
		if statErr != nil && os.IsNotExist(statErr) {
			fmt.Fprintf(w, "\nGroup: %s\n", g.Name)
			fmt.Fprintf(w, "  ⚠️ config not found: %s\n", g.ConfigPath)
			fmt.Fprintf(w, "  Run 'grafel cleanup' to remove this orphaned entry\n")
			continue
		}

		cfg, err := registry.LoadGroupConfig(g.ConfigPath)
		if err != nil {
			fmt.Fprintf(w, "\nGroup: %s\n", g.Name)
			fmt.Fprintf(w, "  (config error: %v)\n", err)
			continue
		}

		// Compute rich statistics for this group.
		summary := ComputeStatusSummary(g.Name, cfg.Repos)
		PrintStatusSummary(w, summary)

		// PH3 (#2091): show linked worktree children indented under their parent.
		if cfg.Features.TrackWorktrees {
			printWorktreeChildren(w, g.Name)
		}
	}
	return nil
}

// printWatcherFleetStatus prints the per-repo watcher fleet line. It is
// deliberately quiet when no watcher units are installed at all (the common
// dev/foreground case) and never fails the status command: an enumeration
// error is reported inline, because "I could not tell you" is itself
// information a user checking whether grafel really stopped needs.
func printWatcherFleetStatus(w io.Writer) {
	sum, err := summarizeFleetWatchers()
	if err != nil {
		fmt.Fprintf(w, "Watchers: unknown (%v)\n", err)
		return
	}
	if sum.Installed == 0 {
		return
	}
	fmt.Fprintf(w, "Watchers: %d installed, %d running\n", sum.Installed, sum.Running)
	if sum.Running > 0 {
		fmt.Fprintf(w, "  these index independently of the daemon; 'grafel stop' stops them too\n")
	}
}

// printWorktreeChildren prints the ephemeral worktree-child entries for the
// given group, indented under their parent repo slug. Reads the worktrees.json
// store from the grafel home directory. Silently skips when the file does
// not exist (no worktrees discovered yet).
func printWorktreeChildren(w io.Writer, groupName string) {
	h, err := registry.HomeDir()
	if err != nil {
		return
	}
	storePath := filepath.Join(h, "worktrees.json")
	store := worktree.NewStore(storePath)
	if err := store.Load(); err != nil {
		return
	}
	active := store.Active()
	if len(active) == 0 {
		return
	}

	// Group children by parentSlug.
	bySlug := make(map[string][]*worktree.WorktreeChild)
	for _, c := range active {
		if c.GroupName != groupName {
			continue
		}
		bySlug[c.ParentSlug] = append(bySlug[c.ParentSlug], c)
	}
	if len(bySlug) == 0 {
		return
	}

	slugs := make([]string, 0, len(bySlug))
	for s := range bySlug {
		slugs = append(slugs, s)
	}
	sort.Strings(slugs)

	for _, slug := range slugs {
		children := bySlug[slug]
		// Sort children by branch for deterministic output.
		sort.Slice(children, func(i, j int) bool {
			return children[i].Branch < children[j].Branch
		})
		for _, c := range children {
			name := filepath.Base(c.Path)
			branch := c.Branch
			if branch == "" {
				branch = "(detached)"
			}
			fmt.Fprintf(w, "  └─ worktree: %s @ %s\n", name, branch)
		}
	}
}

// onOff renders a bool as "on"/"off" for status/doctor indexing-mode lines.
func onOff(b bool) string {
	if b {
		return "on"
	}
	return "off"
}

// memLimitDescription renders the Go soft memory limit for operator-facing
// surfaces (grafel status / grafel doctor) as a value + a source tag.
//
// #6045: the limit is a budget for the WHOLE INSTALLATION, but in split mode
// two processes run — serve (read plane) and engine (write plane). Reporting a
// bare single figure understated real consumption by 2x, because each process
// used to apply that figure in full. The value therefore names the total AND
// both shares, e.g.
//
//	2560MB (768MB serve + 1792MB engine)
//
// In monolith mode (GRAFEL_SPLIT_MODE=0) there is one process and the value is
// just the total.
func memLimitDescription() (value, source string) {
	total, serve, engine, src, split := daemon.MemLimitPlaneSummary()
	if total <= 0 {
		return "unbounded", src
	}
	if !split {
		return fmt.Sprintf("%dMB", total), src
	}
	return fmt.Sprintf("%dMB (%dMB serve + %dMB engine)", total, serve, engine), src
}

// humanBytes formats a byte count as a short human-readable string. We
// avoid pulling go-humanize for this; the daemon's RSS reporting is the
// only consumer.
func humanBytes(n uint64) string {
	const (
		KB = 1 << 10
		MB = 1 << 20
		GB = 1 << 30
	)
	switch {
	case n >= GB:
		return fmt.Sprintf("%.1fGB", float64(n)/float64(GB))
	case n >= MB:
		return fmt.Sprintf("%.1fMB", float64(n)/float64(MB))
	case n >= KB:
		return fmt.Sprintf("%.1fKB", float64(n)/float64(KB))
	default:
		return fmt.Sprintf("%dB", n)
	}
}
