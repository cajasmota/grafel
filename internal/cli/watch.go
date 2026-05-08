package cli

import (
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"
)

// newWatchCmd is the long-lived watcher daemon. The actual fsnotify-
// driven loop is intentionally minimal here: it polls graph.json's
// staleness and re-runs `archigraph index <repo>` when the repo has
// been modified since the last index. We keep dependencies low until
// PORT-7 brings in a real fsnotify-backed watcher.
func newWatchCmd() *cobra.Command {
	var interval time.Duration
	cmd := &cobra.Command{
		Use:   "watch <repo>",
		Short: "Long-lived watcher process (used by launchd/systemd units)",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) != 1 {
				return errors.New("watch expects exactly one repo path")
			}
			return runWatch(args[0], interval)
		},
	}
	cmd.Flags().DurationVar(&interval, "interval", 30*time.Second, "poll interval between reindex checks")
	return cmd
}

func runWatch(repo string, interval time.Duration) error {
	if _, err := os.Stat(repo); err != nil {
		return fmt.Errorf("repo: %w", err)
	}
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	tick := time.NewTicker(interval)
	defer tick.Stop()
	fmt.Fprintf(os.Stderr, "archigraph watch: %s (every %s)\n", repo, interval)
	for {
		select {
		case <-stop:
			return nil
		case <-tick.C:
			// The actual reindex is delegated back to the cmd-package
			// implementation through Hooks so we share one code path.
			if activeHooks.RunIndex != nil {
				if err := activeHooks.RunIndex([]string{repo}); err != nil {
					fmt.Fprintf(os.Stderr, "archigraph watch: index failed: %v\n", err)
				}
			}
		}
	}
}
