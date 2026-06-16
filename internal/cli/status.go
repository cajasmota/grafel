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
	cmd := &cobra.Command{
		Use:   "status [group]",
		Short: "Show daemon + index status",
		Long: `Show daemon health and per-repo index state.

When --ref is supplied the registry view is filtered to repos that have a
graph stored for that ref.  Use --ref @all to show state across every known
ref (implies a per-ref breakdown in the output).`,
		RunE: func(cmd *cobra.Command, args []string) error {
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
			}
			if st.WatcherRepos > 0 || st.WatcherEvents > 0 {
				fmt.Fprintf(w, "  watcher: repos=%d dirs=%d events=%d dropped=%d",
					st.WatcherRepos, st.WatcherDirs, st.WatcherEvents, st.WatcherDropped)
				// PH2a (#2096): show pause/resume slot counts when available.
				if st.WatcherActiveSlots > 0 || st.WatcherPausedSlots > 0 {
					fmt.Fprintf(w, " slots_active=%d slots_paused=%d",
						st.WatcherActiveSlots, st.WatcherPausedSlots)
				}
				fmt.Fprintln(w)
			}
			if st.QueueLen > 0 || len(st.IndexInFlight) > 0 ||
				len(st.PendingAlgo) > 0 || len(st.PendingLinks) > 0 {
				fmt.Fprintf(w, "  scheduler: queue=%d in_flight=%d pending_algo=%d pending_links=%d\n",
					st.QueueLen, len(st.IndexInFlight), len(st.PendingAlgo), len(st.PendingLinks))
			}
			if st.RSSBudgetMB > 0 {
				// Two separate lines: daemon idle RSS (informational) vs.
				// admission budget (delta-based predicted in-flight sum).
				// These are intentionally distinct — idle RSS can exceed the
				// budget without blocking jobs, because jobs are only blocked
				// when sum(predicted_in_flight) + new_job_pred > budget.
				// #3648: report the honest process footprint (resident set
				// size), not the old MemStats.Sys mislabel. On macOS RSS
				// under-counts swapped/compressed pages — note that, plus the
				// Go-heap breakdown, so the number is interpretable.
				fmt.Fprintf(w, "  mem: footprint=%dMB heap_inuse=%dMB heap_released=%dMB go_sys=%dMB\n",
					st.RSSUsedMB,
					st.HeapInuseBytes/(1024*1024),
					st.HeapReleasedBytes/(1024*1024),
					st.SysBytes/(1024*1024))
				if st.FootprintLabel != "" {
					fmt.Fprintf(w, "       (footprint = %s)\n", st.FootprintLabel)
				}
				admHeadroom := st.RSSBudgetMB - st.AdmissionUsedMB
				if admHeadroom < 0 {
					admHeadroom = 0
				}
				fmt.Fprintf(w, "  admission: queued=%d admitted=%d predicted=%dMB / budget=%dMB (headroom=%dMB)\n",
					len(st.BlockedJobs), len(st.InFlightJobs),
					st.AdmissionUsedMB, st.RSSBudgetMB, admHeadroom)
				if st.RebuildConcurrencyCap > 0 {
					fmt.Fprintf(w, "  rebuild: in_flight=%d / cap=%d\n",
						st.RebuildInFlight, st.RebuildConcurrencyCap)
				}
				if len(st.InFlightJobs) > 0 {
					for _, j := range st.InFlightJobs {
						fmt.Fprintf(w, "    admitted: %s (predicted=%dMB)\n", j.Path, j.PredictedMB)
					}
				}
				if len(st.BlockedJobs) > 0 {
					for _, p := range st.BlockedJobs {
						fmt.Fprintf(w, "    queued:   %s\n", p)
					}
				}
			}
			if len(st.IndexedRepos) > 0 {
				fmt.Fprintln(w, "  indexed repos:")
				for _, r := range st.IndexedRepos {
					last := r.LastIndex
					if last == "" {
						last = "(never)"
					}
					fmt.Fprintf(w, "    %s  last_index=%s  indexes=%d  algos=%d",
						r.Path, last, r.IndexCount, r.AlgoCount)
					if r.LastErr != "" {
						fmt.Fprintf(w, "  err=%s", r.LastErr)
					}
					fmt.Fprintln(w)
				}
			}
			if n := len(st.RecentLog); n > 0 {
				start := n - 5
				if start < 0 {
					start = 0
				}
				fmt.Fprintln(w, "  recent events:")
				for _, e := range st.RecentLog[start:] {
					line := fmt.Sprintf("    %s  %s", e.Time, e.Kind)
					if e.Repo != "" {
						line += "  " + e.Repo
					}
					if e.Msg != "" {
						line += "  " + e.Msg
					}
					fmt.Fprintln(w, line)
				}
			}
		}
	case errors.Is(err, client.ErrDaemonNotRunning):
		fmt.Fprintln(w, "Daemon: not running")
	default:
		fmt.Fprintf(w, "Daemon: error: %v\n", err)
	}

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
