package cli

import (
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/cajasmota/archigraph/internal/registry"
)

// newWatchCmd is the long-lived watcher daemon. The actual fsnotify-
// driven loop is intentionally minimal here: it polls graph.json's
// staleness and re-runs `archigraph index <repo>` when the repo has
// been modified since the last index. We keep dependencies low until
// PORT-7 brings in a real fsnotify-backed watcher.
//
// In addition to the per-repo reindex, the watcher also tracks the
// mtime of every registered repo's `<repo>/.archigraph/graph.json`
// across the group(s) the watched repo participates in. Whenever any
// of those mtimes advances, the cross-repo link passes are re-run
// via the RunLinks hook so links.json stays in sync with the freshly
// produced per-repo graphs.
func newWatchCmd() *cobra.Command {
	var interval time.Duration
	var group string
	cmd := &cobra.Command{
		Use:   "watch <repo>",
		Short: "Long-lived watcher process (used by launchd/systemd units)",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) != 1 {
				return errors.New("watch expects exactly one repo path")
			}
			return runWatch(args[0], group, interval)
		},
	}
	cmd.Flags().DurationVar(&interval, "interval", 30*time.Second, "poll interval between reindex checks")
	cmd.Flags().StringVar(&group, "group", "", "group name to re-run link passes for (defaults to every group containing the repo)")
	return cmd
}

func runWatch(repo, group string, interval time.Duration) error {
	if _, err := os.Stat(repo); err != nil {
		return fmt.Errorf("repo: %w", err)
	}
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	tick := time.NewTicker(interval)
	defer tick.Stop()
	fmt.Fprintf(os.Stderr, "archigraph watch: %s (every %s)\n", repo, interval)

	// graphMtimes tracks the last-seen mtime of every registered repo's
	// graph.json across the groups we care about. When any value
	// changes between ticks, we re-run the link passes for the affected
	// group(s).
	graphMtimes := snapshotGraphMtimes(repo, group)

	for {
		select {
		case <-stop:
			return nil
		case <-tick.C:
			// 1. Reindex the watched repo first.
			if activeHooks.RunIndex != nil {
				if err := activeHooks.RunIndex([]string{repo}); err != nil {
					fmt.Fprintf(os.Stderr, "archigraph watch: index failed: %v\n", err)
				}
			}
			// 2. Detect any cross-repo graph.json mtime changes and
			// re-run link passes for the affected groups.
			changed := detectGraphChanges(repo, group, graphMtimes)
			for _, g := range changed {
				if activeHooks.RunLinks == nil {
					break
				}
				if err := activeHooks.RunLinks(g); err != nil {
					fmt.Fprintf(os.Stderr, "archigraph watch: links pass failed for %s: %v\n", g, err)
				}
			}
		}
	}
}

// groupsForRepo returns the names of the groups whose config lists
// `repo` (compared by absolute path). If `explicit` is non-empty only
// that single group is returned.
func groupsForRepo(repo, explicit string) []string {
	if explicit != "" {
		return []string{explicit}
	}
	refs, err := registry.Groups()
	if err != nil {
		return nil
	}
	absRepo, err := filepath.Abs(repo)
	if err != nil {
		absRepo = repo
	}
	var out []string
	for _, ref := range refs {
		cfg, err := registry.LoadGroupConfig(ref.ConfigPath)
		if err != nil {
			continue
		}
		for _, r := range cfg.Repos {
			rp, err := filepath.Abs(r.Path)
			if err != nil {
				rp = r.Path
			}
			if rp == absRepo {
				out = append(out, ref.Name)
				break
			}
		}
	}
	return out
}

// repoPathsForGroup returns the absolute paths of every repo configured
// in the named group.
func repoPathsForGroup(group string) []string {
	refs, err := registry.Groups()
	if err != nil {
		return nil
	}
	for _, ref := range refs {
		if ref.Name != group {
			continue
		}
		cfg, err := registry.LoadGroupConfig(ref.ConfigPath)
		if err != nil {
			return nil
		}
		var out []string
		for _, r := range cfg.Repos {
			out = append(out, r.Path)
		}
		return out
	}
	return nil
}

// snapshotGraphMtimes captures the current mtime of every group-mate's
// graph.json. Missing files are recorded as the zero time.
func snapshotGraphMtimes(repo, explicitGroup string) map[string]time.Time {
	out := map[string]time.Time{}
	for _, g := range groupsForRepo(repo, explicitGroup) {
		for _, p := range repoPathsForGroup(g) {
			gj := filepath.Join(p, ".archigraph", "graph.json")
			if fi, err := os.Stat(gj); err == nil {
				out[gj] = fi.ModTime()
			} else {
				out[gj] = time.Time{}
			}
		}
	}
	return out
}

// detectGraphChanges compares current graph.json mtimes against the
// previous snapshot, updates the snapshot in place, and returns the
// list of unique groups for which a change was observed.
func detectGraphChanges(repo, explicitGroup string, prev map[string]time.Time) []string {
	groups := groupsForRepo(repo, explicitGroup)
	dirty := map[string]bool{}
	// Reverse-index graph.json → group(s) so a single mtime change
	// triggers exactly the groups that consume that graph.
	graphToGroups := map[string][]string{}
	for _, g := range groups {
		for _, p := range repoPathsForGroup(g) {
			gj := filepath.Join(p, ".archigraph", "graph.json")
			graphToGroups[gj] = append(graphToGroups[gj], g)
		}
	}
	for gj, groupsForFile := range graphToGroups {
		var cur time.Time
		if fi, err := os.Stat(gj); err == nil {
			cur = fi.ModTime()
		}
		old := prev[gj]
		if !cur.Equal(old) {
			prev[gj] = cur
			for _, g := range groupsForFile {
				dirty[g] = true
			}
		}
	}
	out := make([]string, 0, len(dirty))
	for g := range dirty {
		out = append(out, g)
	}
	return out
}
