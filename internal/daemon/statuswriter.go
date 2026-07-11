package daemon

import (
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/indexer/diff"
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
// ALL writes go through a SINGLE serialized statusWriter goroutine (see
// statusWriter.run). Two triggers feed it, both via a coalescing channel:
//  1. indexstate.SetOnRepoStatesChanged (wired to statusWriter.notify) — every
//     scheduler state transition (index start/complete/dirty) requests a
//     refresh promptly.
//  2. a periodic heartbeat tick (default every defaultStatusHeartbeatInterval)
//     so a reader can detect a wedged/crashed engine via a stale HeartbeatAt
//     rather than trusting indefinitely-old data.
//
// Serializing through one goroutine is what kills review #5734's BLOCKING
// finding: it makes concurrent same-repo writes impossible from the daemon's
// side (no tmp-file collision), and — combined with the coalescing channel —
// collapses a burst of transitions into a bounded number of write passes
// instead of spawning one all-repos-iterating, git-shelling goroutine per
// transition (review #5734 non-blocking #2).

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

// indexedCommitShortNoGit reads the short indexed-commit SHA for repoPath
// WITHOUT shelling out to git. It is the write-path counterpart to
// IndexedCommitForRepo (which additionally computes AtHead via a git subprocess
// — wasteful here, review #5734 non-blocking #3, since the status file only
// carries the short SHA). Resolution order mirrors IndexedCommitForRepo:
// diff-manifest sidecar first, then the graph.fb header's IndexedSHA.
func indexedCommitShortNoGit(repoPath string) string {
	stateDir := StateDirForRepo(repoPath)
	if stateDir == "" {
		return ""
	}
	if m := diff.LoadManifest(stateDir); m.GitCommit != "" {
		return m.GitCommit
	}
	if ps, ok := graph.PersistedStatsFromDir(stateDir); ok {
		return ps.IndexedSHA
	}
	return ""
}

// writeRepoStatusFile computes and atomically writes repoPath's current
// status-plane sidecar. logger may be nil (tests). Failures are logged (when
// a logger is available) and otherwise swallowed — the status file is a
// best-effort observability aid, never load-bearing for indexing itself, so
// a write failure must never propagate into the scheduler/RPC hot path.
//
// This is only ever called from the single statusWriter goroutine (or directly
// from tests), so it never races another writer for the same repo.
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

	// Write path is git-free: read the short SHA off disk, never shell out
	// (review #5734 non-blocking #3).
	f.IndexedCommit = indexedCommitShortNoGit(repoPath)

	if err := statusfile.Write(repoPath, f); err != nil && logger != nil {
		logger.Warn("statusfile: write failed", "repo", repoPath, "err", err)
	}
}

// knownRepoPathsForStatus returns every repo from every registered fleet
// group (deduped, resolved to absolute paths), independent of cfg.ReposToWatch.
// It is side-effect-free and safe to call repeatedly — every heartbeat tick
// and every coalesced state-change refresh calls it fresh — unlike
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

// engineLivenessStatusKey returns the statusfile key for the engine-global
// liveness heartbeat (ADR-0024 / #5729 PR2). It is an ABSOLUTE path under the
// daemon root (so statusfile.PathFor's filepath.Abs is idempotent and both the
// engine writer and the serve-side supervisor reader compute the same hash
// regardless of cwd). No file is created at this path — statusfile hashes it
// into GRAFEL_HOME/status/<hash>.json. It intentionally does NOT collide with
// any real repo's per-repo status file.
//
// Unlike the per-repo status files (which only exist for registered fleet
// repos), this engine-global file is written unconditionally by RunEngine, so
// serve's health gate has a liveness signal even on a machine with no repos
// registered yet.
func engineLivenessStatusKey(root string) string {
	return filepath.Join(root, ".engine-liveness")
}

// EngineLivenessStatusKey is the exported form of engineLivenessStatusKey for
// callers outside package daemon — specifically `grafel doctor`'s
// monolith-aware engine-liveness check (ADR-0024 PR5, epic #5729) — that need
// to read the SAME engine-global liveness statusfile the serve-side
// supervisor's own health gate reads, without duplicating (and risking
// drifting from) the key-derivation format.
func EngineLivenessStatusKey(root string) string {
	return engineLivenessStatusKey(root)
}

// startEngineLivenessHeartbeat launches a goroutine that stamps the
// engine-global liveness statusfile (EnginePID + a fresh HeartbeatAt) once
// immediately and then every interval, until the returned stop func is called
// (which joins the goroutine). It is the engine → serve liveness contract the
// supervisor's health gate reads (ADR-0024, epic #5729 PR2).
func startEngineLivenessHeartbeat(root string, interval time.Duration, logger *slog.Logger) (stop func()) {
	if interval <= 0 {
		interval = defaultStatusHeartbeatInterval
	}
	key := engineLivenessStatusKey(root)
	stopCh := make(chan struct{})
	doneCh := make(chan struct{})
	writeOnce := func() {
		f := &statusfile.File{
			EnginePID:   os.Getpid(),
			HeartbeatAt: time.Now().UTC(),
			Version:     version.String(),
			RepoPath:    key,
		}
		if err := statusfile.Write(key, f); err != nil && logger != nil {
			logger.Warn("engine liveness: statusfile write failed", "err", err)
		}
	}
	go func() {
		defer close(doneCh)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		writeOnce()
		for {
			select {
			case <-stopCh:
				return
			case <-ticker.C:
				writeOnce()
			}
		}
	}()
	return func() {
		close(stopCh)
		<-doneCh
	}
}

// statusWriter owns the single goroutine that writes every repo's status-plane
// sidecar. All writes are serialized through run(), so no two writes ever race
// for the same repo file (review #5734 BLOCKING fix). Refresh requests arrive
// via notify(), which is non-blocking and COALESCING: a burst of scheduler
// transitions collapses into at most one extra write pass, bounding both
// goroutine count and the number of graph.fb stats reads per burst (review
// #5734 non-blocking #2).
type statusWriter struct {
	reposFn func() []string
	logger  *slog.Logger

	// trigger is buffered size 1. notify() does a non-blocking send, so when a
	// pass is already pending the extra request is dropped — the pending pass
	// will read the latest state when it runs. This is the coalescing seam.
	trigger chan struct{}
	stop    chan struct{}
	done    chan struct{}
}

// newStatusWriter constructs a statusWriter. reposFn must be side-effect-free
// and safe to call repeatedly (see knownRepoPathsForStatus).
func newStatusWriter(reposFn func() []string, logger *slog.Logger) *statusWriter {
	return &statusWriter{
		reposFn: reposFn,
		logger:  logger,
		trigger: make(chan struct{}, 1),
		stop:    make(chan struct{}),
		done:    make(chan struct{}),
	}
}

// notify requests a status-file refresh. Non-blocking and coalescing: if a
// refresh is already queued this is a no-op. Safe to call from any goroutine,
// including indexstate's on-change hook (fired under the scheduler lock).
func (w *statusWriter) notify() {
	select {
	case w.trigger <- struct{}{}:
	default:
	}
}

// run is the single serialized writer loop. It writes once immediately (so a
// reader sees state promptly at startup), then refreshes on each coalesced
// notify() and each heartbeat tick until shutdown. Intended to run in its own
// goroutine for the daemon's lifetime.
func (w *statusWriter) run(interval time.Duration) {
	defer close(w.done)
	if interval <= 0 {
		interval = defaultStatusHeartbeatInterval
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	w.writeAll()
	for {
		select {
		case <-w.stop:
			return
		case <-ticker.C:
			w.writeAll()
		case <-w.trigger:
			w.writeAll()
		}
	}
}

// writeAll refreshes every known repo's status file. Only ever called from
// run()'s single goroutine, so writes stay serialized.
func (w *statusWriter) writeAll() {
	if w.reposFn == nil {
		return
	}
	for _, repo := range w.reposFn() {
		writeRepoStatusFile(repo, w.logger)
	}
}

// shutdown stops the writer goroutine and waits for it to exit. Idempotent is
// NOT guaranteed — call exactly once, from the daemon's Run defer.
func (w *statusWriter) shutdown() {
	close(w.stop)
	<-w.done
}

// startStatusWriter wires and starts the status-plane writer: it registers the
// coalescing notify hook with indexstate and launches the single writer
// goroutine. The returned func unregisters the hook and stops the goroutine;
// call it from the daemon's Run defer. reposFn must be side-effect-free (see
// knownRepoPathsForStatus).
func startStatusWriter(reposFn func() []string, interval time.Duration, logger *slog.Logger) (stop func()) {
	w := newStatusWriter(reposFn, logger)
	indexstate.SetOnRepoStatesChanged(w.notify)
	go w.run(interval)
	return func() {
		// Unregister the hook first so no new notify() races shutdown, then
		// stop and join the goroutine.
		indexstate.SetOnRepoStatesChanged(nil)
		w.shutdown()
	}
}
