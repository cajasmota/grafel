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

	"github.com/cajasmota/grafel/internal/daemon"
	"github.com/cajasmota/grafel/internal/daemon/client"
	"github.com/cajasmota/grafel/internal/daemon/proto"
	"github.com/cajasmota/grafel/internal/daemon/watchreg"
	"github.com/cajasmota/grafel/internal/gitmeta"
	"github.com/cajasmota/grafel/internal/registry"
)

// watchBackoffConfig tunes the standalone watcher's failure handling
// (issue #5140). When the daemon is unreachable the watcher must NOT
// tight-loop and spam its err log forever (that, together with a stale
// orphan process, was a primary driver of the observed CPU runaway).
// Instead it backs off exponentially and exits after maxConsecutive
// consecutive failures so a watcher whose daemon was restarted dies
// rather than busy-looping.
type watchBackoffConfig struct {
	// base is the first backoff sleep after a failure.
	base time.Duration
	// max caps the per-failure backoff sleep.
	max time.Duration
	// maxConsecutive is the number of back-to-back failures after which
	// the watcher gives up and exits. Zero means "never die" (only used
	// by tests that want to exercise the sleep schedule in isolation).
	maxConsecutive int
}

func defaultWatchBackoff() watchBackoffConfig {
	return watchBackoffConfig{
		base:           2 * time.Second,
		max:            60 * time.Second,
		maxConsecutive: 10,
	}
}

// activeWatchBackoff is the backoff policy runWatch uses. It is a var
// (not a constant call) so tests can substitute a fast schedule without
// waiting out the production exponential delays.
var activeWatchBackoff = defaultWatchBackoff

// backoffSleep returns the sleep duration for the Nth (1-based)
// consecutive failure: base * 2^(failures-1), capped at max. A
// failures count <= 1 yields the base delay.
func (c watchBackoffConfig) backoffSleep(failures int) time.Duration {
	d := c.base
	for i := 1; i < failures; i++ {
		d *= 2
		if d >= c.max {
			return c.max
		}
	}
	if d > c.max {
		return c.max
	}
	return d
}

// shouldDie reports whether the watcher has hit its consecutive-failure
// ceiling and must exit. maxConsecutive == 0 disables the ceiling.
func (c watchBackoffConfig) shouldDie(failures int) bool {
	return c.maxConsecutive > 0 && failures >= c.maxConsecutive
}

// indexRPCFunc issues the daemon's Index RPC. It is a var (not a direct
// client.Dial + c.Index call) so tests can stub it and assert on the
// IndexArgs the watcher builds (e.g. Async) without a live daemon
// connection.
var indexRPCFunc = func(args proto.IndexArgs) (proto.IndexReply, error) {
	c, err := client.Dial()
	if err != nil {
		return proto.IndexReply{}, err
	}
	defer c.Close()
	return c.Index(args)
}

// indexViaDaemon calls the daemon's Index RPC for one repo. Returns
// the canonical "daemon not running" error so the watcher loop's log
// line is identical to what `grafel index` would print.
//
// Async is set (#5140-followup / watch-head-gate): a tick that DOES need
// to reindex (HEAD moved) must not itself block the watcher loop on a
// full synchronous index — it enqueues onto the daemon's debounced/
// coalescing scheduler (service.go's Async fast-paths) and returns as
// soon as the request is queued, exactly like the git-hooks path (#3366).
//
// TRADEOFF (intentional, not a bug): the error returned here now reflects
// only whether the ENQUEUE was ACCEPTED, not whether the index actually
// completed. An async index that fails INSIDE the daemon is therefore no
// longer surfaced as `grafel watch: index failed (N/10)` on this path —
// failure reporting for the reindex itself now belongs to the daemon
// scheduler (mirrors the git-hooks async path #3366). See the matching
// note in maybeTriggerIndex where lastSHA is cached on enqueue-accepted.
func indexViaDaemon(repo string) error {
	_, err := indexRPCFunc(proto.IndexArgs{RepoPath: repo, Async: true})
	if err != nil {
		if errors.Is(err, client.ErrDaemonNotRunning) {
			return errDaemonNotRunning
		}
		return err
	}
	return nil
}

// indexTriggerFunc is the function each watch tick calls to trigger an
// index. It defaults to indexViaDaemon; tests substitute a counting stub
// so invocation counts can be asserted without a live daemon.
var indexTriggerFunc = indexViaDaemon

// resolveHeadSHA returns the repo's current git HEAD SHA, or "" when it
// cannot be determined (non-git directory, git not on PATH, etc.). It is a
// var (not a direct gitmeta.Capture call) so tests can stub a controllable
// SHA sequence without needing a real git repository.
var resolveHeadSHA = func(repo string) string {
	return gitmeta.Capture(repo).SHA
}

// watchTickState carries the per-tick state that must persist across
// polling ticks for the unchanged-HEAD no-op gate (defect b, watch-head-gate):
// which SHA was in effect the last time an index was successfully
// triggered, and whether any trigger has happened yet at all.
type watchTickState struct {
	haveTriggered bool
	lastSHA       string
}

// maybeTriggerIndex implements the unchanged-HEAD no-op gate. It resolves
// repo's current HEAD SHA and skips the index RPC entirely when it matches
// the SHA recorded at the last SUCCESSFUL trigger — the per-repo `grafel
// watch` poll loop previously triggered a full reindex on every tick even
// when nothing had changed, which on a large repo produced a perpetual
// rebuild loop that never settled to idle.
//
// The very first call always triggers (st.haveTriggered starts false), and
// an unresolvable SHA (resolveHeadSHA returns "") always triggers too —
// fail open rather than silently stop reindexing a repo the poller can't
// read HEAD for.
//
// The cache is updated only after a SUCCESSFUL trigger, so a failed index
// (daemon down, RPC error) does not get treated as "up to date" and does
// not suppress a retry on the next tick.
//
// NOTE (watch-head-gate tradeoff): "successful trigger" here means the
// async ENQUEUE was ACCEPTED (see indexViaDaemon), NOT that the reindex
// itself completed. So lastSHA is cached — and this SHA no longer
// re-triggers — as soon as the daemon accepts the request; a failure
// DURING the async index is owned by the daemon scheduler, not re-reported
// by the watcher. Intentional, mirrors the git-hooks async path (#3366).
//
// triggered reports whether the index RPC was invoked at all (regardless
// of whether it returned an error), so callers can distinguish "skipped by
// the gate" from "attempted and failed" for backoff bookkeeping.
func maybeTriggerIndex(repo string, st *watchTickState) (triggered bool, err error) {
	sha := resolveHeadSHA(repo)
	if st.haveTriggered && sha != "" && sha == st.lastSHA {
		return false, nil
	}
	if err := indexTriggerFunc(repo); err != nil {
		return true, err
	}
	st.haveTriggered = true
	st.lastSHA = sha
	return true, nil
}

// newWatchCmd is the long-lived watcher daemon. The actual fsnotify-
// driven loop is intentionally minimal here: it polls graph.json's
// staleness and re-runs `grafel index <repo>` when the repo has
// been modified since the last index. We keep dependencies low until
// PORT-7 brings in a real fsnotify-backed watcher.
//
// In addition to the per-repo reindex, the watcher also tracks the
// mtime of every registered repo's `<repo>/.grafel/graph.json`
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
	fmt.Fprintf(os.Stderr, "grafel watch: %s (every %s)\n", repo, interval)

	// #5142: register this standalone watcher in the daemon-owned PID registry
	// so the daemon can reap us if we are ever orphaned (our owning daemon dies
	// or restarts onto a new PID). The OwnerDaemonPID stamp lets the daemon's
	// sweep distinguish a watcher it still owns from a leftover from a previous
	// daemon generation. Best-effort: a registry write failure must never stop
	// the watcher from doing its job, and the #5141 self-reap is the fallback.
	if reg := watcherRegistry(); reg != nil {
		entry := watchreg.Entry{
			PID:            os.Getpid(),
			Repo:           absRepoForWatch(repo),
			OwnerDaemonPID: liveDaemonPID(),
		}
		if err := reg.Register(entry); err != nil {
			fmt.Fprintf(os.Stderr, "grafel watch: pid registry register failed (non-fatal): %v\n", err)
		} else {
			defer func() { _ = reg.Deregister(entry.PID) }()
		}
	}

	// graphMtimes tracks the last-seen mtime of every registered repo's
	// graph.json across the groups we care about. When any value
	// changes between ticks, we re-run the link passes for the affected
	// group(s).
	//
	// NOTE (issue #5140): this is a *staleness/cross-repo-link* signal
	// only. The watcher deliberately does NOT treat a graph.json mtime
	// bump as a source change that re-triggers a repo reindex — the
	// daemon writes <repo>/.grafel/graph.json as the OUTPUT of every
	// index, and reading that write back as an input would form a
	// self-reinforcing reindex loop. The repo reindex below is driven
	// purely by the poll tick (and, in Phase B, by the daemon's own
	// fsnotify watcher, which already excludes <repo>/.grafel/ via
	// watch.ShouldSkipPath).
	graphMtimes := snapshotGraphMtimes(repo, group)

	backoff := activeWatchBackoff()
	consecutiveFailures := 0
	tickState := &watchTickState{}

	for {
		select {
		case <-stop:
			return nil
		case <-tick.C:
			// 1. Reindex the watched repo first. Per ADR-0017 the
			// indexer runs inside the daemon — `watch` becomes a thin
			// RPC client. Phase B will retire this subcommand entirely
			// once the daemon's fsnotify loop is wired in.
			//
			// maybeTriggerIndex gates the RPC on the repo's git HEAD SHA
			// (watch-head-gate): a tick whose HEAD is unchanged since the
			// last successful trigger is a cheap no-op instead of a full
			// reindex, which previously ran unconditionally on every tick
			// and, on a large repo, produced a perpetual rebuild loop that
			// never settled to idle.
			triggered, err := maybeTriggerIndex(repo, tickState)
			if err != nil {
				consecutiveFailures++
				fmt.Fprintf(os.Stderr, "grafel watch: index failed (%d/%d): %v\n",
					consecutiveFailures, backoff.maxConsecutive, err)
				// Backoff + die (issue #5140): a watcher whose daemon was
				// restarted (or is permanently gone) must not tight-loop
				// and spam its err log forever. Exit after N consecutive
				// failures so an orphaned watcher reaps itself.
				if backoff.shouldDie(consecutiveFailures) {
					return fmt.Errorf("grafel watch: giving up after %d consecutive index failures (last: %w)",
						consecutiveFailures, err)
				}
				sleep := backoff.backoffSleep(consecutiveFailures)
				select {
				case <-stop:
					return nil
				case <-time.After(sleep):
				}
				continue
			}
			if triggered {
				consecutiveFailures = 0
			}
			// 2. Detect any cross-repo graph.json mtime changes and
			// re-run link passes for the affected groups. (Staleness
			// signal only — see the note above; this does not re-trigger
			// a reindex of `repo` itself.)
			changed := detectGraphChanges(repo, group, graphMtimes)
			for _, g := range changed {
				if activeHooks.RunLinks == nil {
					break
				}
				if err := activeHooks.RunLinks(g); err != nil {
					fmt.Fprintf(os.Stderr, "grafel watch: links pass failed for %s: %v\n", g, err)
				}
			}
		}
	}
}

// watcherRegistry returns the daemon-owned watcher PID registry (#5142), or nil
// when the daemon layout cannot be resolved (in which case the watcher simply
// does not register — the #5141 self-reap remains the fallback).
func watcherRegistry() *watchreg.Registry {
	layout, err := daemon.DefaultLayout()
	if err != nil || layout.Root == "" {
		return nil
	}
	return watchreg.New(watchreg.DefaultPath(layout.Root))
}

// absRepoForWatch resolves repo to an absolute path for the registry entry,
// falling back to the raw value on error (diagnostic field only).
func absRepoForWatch(repo string) string {
	if abs, err := filepath.Abs(repo); err == nil {
		return abs
	}
	return repo
}

// liveDaemonPID returns the PID recorded in the daemon pidfile, or 0 when it is
// missing/unreadable. Stamped into the watcher's registry entry as its owner so
// the daemon sweep can detect orphans from a previous daemon generation.
func liveDaemonPID() int {
	layout, err := daemon.DefaultLayout()
	if err != nil {
		return 0
	}
	return daemon.ReadPIDFile(layout.PIDPath)
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
			gj := daemon.GraphPathForRepo(p)
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
			gj := daemon.GraphPathForRepo(p)
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
