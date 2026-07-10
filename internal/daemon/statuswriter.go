package daemon

import (
	"log/slog"
	"os"
	"strconv"
	"time"

	"github.com/cajasmota/grafel/internal/indexstate"
	"github.com/cajasmota/grafel/internal/registry"
	"github.com/cajasmota/grafel/internal/statusfile"
	"github.com/cajasmota/grafel/internal/version"
)

// #5725/#5729-W1 — the status-plane foundation. The daemon (engine) is the
// SOLE writer of internal/statusfile's per-repo sidecar; a poll-safe reader
// (grafel status --json, a statusline, or the future standalone `serve`
// process per ADR-0024) reads it directly off disk, never over the RPC
// socket, so it can never block behind an in-flight index.
//
// Two triggers keep the file fresh:
//  1. registerStatusFileHook wires indexstate.SetOnRepoStatesChanged so every
//     scheduler state transition (index start/complete/dirty) refreshes the
//     file promptly.
//  2. runStatusHeartbeat is a periodic ticker (default every
//     defaultStatusHeartbeatInterval) that refreshes it even when nothing
//     changed, so a reader can detect a wedged/crashed engine via a stale
//     HeartbeatAt rather than trusting indefinitely-old data.

// defaultStatusHeartbeatInterval is how often the periodic heartbeat rewrites
// every known repo's status file absent any state change.
const defaultStatusHeartbeatInterval = 5 * time.Second

// EnvStatusHeartbeatSeconds overrides defaultStatusHeartbeatInterval (tests /
// operators who want a tighter or looser cadence).
const EnvStatusHeartbeatSeconds = "GRAFEL_STATUS_HEARTBEAT_SECONDS"

// statusHeartbeatInterval resolves the configured heartbeat cadence.
func statusHeartbeatInterval() time.Duration {
	if raw := os.Getenv(EnvStatusHeartbeatSeconds); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			return time.Duration(n) * time.Second
		}
	}
	return defaultStatusHeartbeatInterval
}

// writeRepoStatusFile computes and atomically writes repoPath's current
// status-plane sidecar. logger may be nil (tests). Failures are logged (when
// a logger is available) and otherwise swallowed — the status file is a
// best-effort observability aid, never load-bearing for indexing itself, so
// a write failure must never propagate into the scheduler/RPC hot path.
func writeRepoStatusFile(repoPath string, logger *slog.Logger) {
	f := &statusfile.File{
		EnginePID:   os.Getpid(),
		HeartbeatAt: time.Now().UTC(),
		Version:     version.String(),
		RepoPath:    repoPath,
	}

	// Per-repo scheduler state (indexing/queued/dirty + the ref it targets).
	for _, st := range indexstate.RepoStates() {
		if st.Path != repoPath {
			continue
		}
		f.IndexedRef = st.IndexedRef
		f.Indexing = st.State == indexstate.StateIndexing || st.State == indexstate.StateDirty
		break
	}
	// Process-wide queue depth (#5493 concurrency gate) — the closest
	// available proxy for "how much work is ahead of this repo" without
	// threading per-repo queue position through the scheduler snapshot.
	conc := indexstate.GetIndexConcurrency()
	f.QueueLen = conc.Queued

	stateDir := StateDirForRepo(repoPath)
	if stateDir != "" {
		f.Entities, f.Relationships = readGraphStatsSidecar(stateDir)
		if graphPath, mtimeNano := FindGraphFile(repoPath); graphPath != "" {
			f.GraphFBMtime = mtimeNano
		}
	}

	ci := IndexedCommitForRepo(repoPath)
	f.IndexedCommit = ci.CommitShort

	if err := statusfile.Write(repoPath, f); err != nil && logger != nil {
		logger.Warn("statusfile: write failed", "repo", repoPath, "err", err)
	}
}

// writeAllRepoStatusFiles refreshes the status file for every repo reposFn
// returns. reposFn is called fresh each time so newly-registered repos are
// picked up without a daemon restart.
//
// IMPORTANT: callers must NOT pass cfg.ReposToWatch here. That callback is
// documented (see server.go's boot-path watcher-subscription goroutine) as
// safe to invoke only from that one call site for some configurations (e.g.
// tests that simulate a slow/stalled filesystem walk with a func that has a
// one-shot side effect). The status-plane writer instead uses
// knownRepoPathsForStatus, a side-effect-free, independently-callable-any-
// number-of-times repo lister built directly off the registry.
func writeAllRepoStatusFiles(reposFn func() []string, logger *slog.Logger) {
	if reposFn == nil {
		return
	}
	for _, repo := range reposFn() {
		writeRepoStatusFile(repo, logger)
	}
}

// knownRepoPathsForStatus returns every repo from every registered fleet
// group (deduped, resolved to absolute paths), independent of cfg.ReposToWatch.
// It is side-effect-free and safe to call repeatedly — every heartbeat tick
// and every scheduler state-change hook fire calls it fresh — unlike
// cfg.ReposToWatch, which some callers (notably
// TestBoot_WatcherSubscriptionDoesNotBlockBind) construct with one-shot side
// effects and an implicit "call at most once" expectation.
func knownRepoPathsForStatus(logger *slog.Logger) []string {
	groups, err := registry.Groups()
	if err != nil {
		return nil
	}
	seen := map[string]bool{}
	var raw []string
	for _, g := range groups {
		gc, err := registry.LoadGroupConfig(g.ConfigPath)
		if err != nil {
			continue
		}
		for _, r := range gc.Repos {
			raw = append(raw, r.Path)
		}
	}
	resolved := ResolveFleetRepoPaths(raw, logger)
	out := make([]string, 0, len(resolved))
	for _, abs := range resolved {
		if seen[abs] {
			continue
		}
		seen[abs] = true
		out = append(out, abs)
	}
	return out
}

// registerStatusFileHook wires indexstate's on-change hook so a scheduler
// state transition (index start/complete/dirty) triggers an immediate
// status-file refresh across all known repos, rather than waiting for the
// next periodic heartbeat tick. Call once at daemon startup, after
// scheduler.Start(); pass nil to indexstate.SetOnRepoStatesChanged to
// unregister (e.g. in tests, or on daemon shutdown).
func registerStatusFileHook(reposFn func() []string, logger *slog.Logger) {
	indexstate.SetOnRepoStatesChanged(func() {
		writeAllRepoStatusFiles(reposFn, logger)
	})
}

// runStatusHeartbeat periodically refreshes every known repo's status file
// until stop is closed. Intended to run in its own goroutine for the
// lifetime of the daemon (see server.go Run).
func runStatusHeartbeat(reposFn func() []string, interval time.Duration, stop <-chan struct{}, logger *slog.Logger) {
	if interval <= 0 {
		interval = defaultStatusHeartbeatInterval
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			writeAllRepoStatusFiles(reposFn, logger)
		}
	}
}
