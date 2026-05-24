package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/cajasmota/archigraph/internal/daemon/client"
	"github.com/cajasmota/archigraph/internal/registry"
)

// newStatusCmd reports both daemon health and per-group index state.
// Status is crash-safe: if the daemon is down we print "daemon not
// running" and continue with the registry view, rather than erroring.
func newStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status [group]",
		Short: "Show daemon + index status",
		RunE: func(cmd *cobra.Command, args []string) error {
			filterGroup := ""
			if len(args) == 1 {
				filterGroup = args[0]
			}
			return runStatus(cmd.OutOrStdout(), filterGroup)
		},
	}
}

func runStatus(w io.Writer, filter string) error {
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
				fmt.Fprintf(w, "  The %s binary is likely stale. Run: archigraph doctor --kill-stale && archigraph start\n",
					st.BinaryPath)
				fmt.Fprintf(w, "  version: %s (from %s)\n", st.Version, st.BinaryPath)
				fmt.Fprintf(w, "  socket:  %s\n", st.SocketPath)
			} else {
				uptime := time.Duration(st.UptimeSec) * time.Second
				fmt.Fprintf(w, "Daemon: running  pid=%d  uptime=%s  rss=%s  in_flight=%d\n",
					st.PID, uptime, humanBytes(st.RSSBytes), st.InFlight)
				fmt.Fprintf(w, "  version: %s\n", st.Version)
				fmt.Fprintf(w, "  socket:  %s\n", st.SocketPath)
				if st.DashboardPort > 0 {
					fmt.Fprintf(w, "  dashboard: http://127.0.0.1:%d/\n", st.DashboardPort)
				}
			}
			if st.WatcherRepos > 0 || st.WatcherEvents > 0 {
				fmt.Fprintf(w, "  watcher: repos=%d dirs=%d events=%d dropped=%d\n",
					st.WatcherRepos, st.WatcherDirs, st.WatcherEvents, st.WatcherDropped)
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
				fmt.Fprintf(w, "  rss: daemon=%dMB (actual process RSS)\n", st.RSSUsedMB)
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
			fmt.Fprintf(w, "  Run 'archigraph cleanup' to remove this orphaned entry\n")
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
	}
	return nil
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
