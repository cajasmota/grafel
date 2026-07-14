package daemon

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/cajasmota/grafel/internal/daemon/proto"
	"github.com/cajasmota/grafel/internal/daemon/requests"
	"github.com/cajasmota/grafel/internal/daemon/sched"
)

// requestsDrainInterval is how often the engine's drain loop scans for
// pending serve→engine request files (ADR-0024 PR4, epic #5729). Requests
// are also fire-and-forget from serve's perspective (Service.Index returns
// as soon as the file is written), so this interval bounds how long a
// split-mode reindex trigger can sit before the engine notices it — kept
// short since the scan itself is a cheap directory glob, not a full
// filesystem walk.
const requestsDrainInterval = 2 * time.Second

// maxRebuildAttempts bounds how many times a single KindRebuild request may be
// (re-)applied across drains/engine-restarts before it is dead-lettered
// (epic #5729, defect d). A group rebuild is multi-minute and, if the engine
// is killed mid-apply (memlimit/reaper/crash/RPC EOF) before the ack is
// written, the request is never removed — so a naive drain re-applies it on
// EVERY 2s tick and EVERY engine restart, an unbounded full-group-rebuild
// loop. Bounding attempts (persisted durably in the request file, so the count
// survives a restart) turns at-least-once-forever into at-most-N with a
// terminating dead-letter.
const maxRebuildAttempts = 3

// engineGroupRebuildGuard serialises KindRebuild applications per group on the
// ENGINE side — where rebuildFn actually executes (epic #5729, defect c).
//
// Service.groupRebuildMu (service.go) is the analogous capacity-1 guard, but in
// split mode it guards nothing: Service.Rebuild returns after merely WRITING
// the request file, never reaching that guard, and the engine drain calls
// rebuildFn directly here with no such serialisation. This guard puts the
// single-flight on the live path, so two concurrent KindRebuild applications
// for the SAME group run one rebuildFn at a time (the second waits), while
// DIFFERENT groups still proceed concurrently.
var engineGroupRebuildGuard groupRebuildGuard

// groupRebuildGuard is a per-group capacity-1 semaphore keyed by group name.
type groupRebuildGuard struct {
	sems sync.Map // group name -> chan struct{} (cap 1)
}

// acquire blocks until this group's slot is free and returns a release func
// that must be called (defer) once the guarded rebuild completes.
func (g *groupRebuildGuard) acquire(group string) (release func()) {
	v, _ := g.sems.LoadOrStore(group, make(chan struct{}, 1))
	ch := v.(chan struct{})
	ch <- struct{}{}
	return func() { <-ch }
}

// discoverRequestsDirs finds every `requests/` directory under the store
// layout rooted at root (either GRAFEL_DAEMON_ROOT/state or
// $GRAFEL_HOME/store — see StateDirForRepo/repoBaseDir). The engine has no
// a priori registry of which repos have dropped a request, so discovery is
// a glob over the known `<slug-or-hash>/refs/<ref>/requests` shape rather
// than a per-repo lookup.
func discoverRequestsDirs(root string) ([]string, error) {
	if root == "" {
		return nil, nil
	}
	matches, err := filepath.Glob(filepath.Join(root, "*", "refs", "*", "requests"))
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(matches))
	for _, m := range matches {
		info, statErr := os.Stat(m)
		if statErr != nil || !info.IsDir() {
			continue
		}
		out = append(out, m)
	}
	return out, nil
}

// requestsRoot resolves the store root the engine should glob for
// requests/ directories: GRAFEL_DAEMON_ROOT/state when the isolated-daemon
// env var is set (tests, parallel agents), else StoreDir() — mirroring
// repoBaseDir's own resolution (state_path.go) so discovery always agrees
// with where Service.Index (via requestsDirForRepo) actually wrote.
func requestsRoot() string {
	if root := os.Getenv(EnvRoot); root != "" {
		return filepath.Join(root, "state")
	}
	return StoreDir()
}

// drainRequestsOnce performs a single pass: discover every requests/ dir
// under root, list its pending records, and apply each via
// requests.ApplyAndAck. KindReindex (PR4) and KindRebuild (PR6 prerequisite,
// epic #5729) are understood — see internal/daemon/requests's Kind doc
// comments for why KindSubmitRepair / KindDocgenApply / KindEnrichmentEnqueue
// are defined but not (yet) produced or consumed anywhere (their handlers
// already write their sidecars directly and are picked up lazily by the next
// scheduled index pass, so they are already cross-process safe with no
// engine-side action needed).
//
// rebuildFn is the engine's in-process rebuild entrypoint (cfg.Rebuild — the
// SAME RebuildFunc Service.Rebuild calls directly in monolith/engine mode);
// may be nil (a KindRebuild request then acks as an error, same shape as a
// reindex request arriving with no scheduler attached).
//
// logger may be nil (tests exercise the apply path without one).
func drainRequestsOnce(root string, scheduler *sched.Scheduler, rebuildFn RebuildFunc, logger *slog.Logger) error {
	dirs, err := discoverRequestsDirs(root)
	if err != nil {
		return fmt.Errorf("requests: discover dirs: %w", err)
	}
	for _, dir := range dirs {
		recs, err := requests.ListPending(dir)
		if err != nil {
			if logger != nil {
				logger.Warn("requests: list pending failed (skipping dir)", "dir", dir, "err", err)
			}
			continue
		}
		for _, rec := range recs {
			rec := rec
			apply := func(r requests.Record) error {
				return applyRequest(scheduler, rebuildFn, r)
			}
			// KindRebuild is the multi-minute, non-incremental path: route it
			// through the crash-resume-bounded consumer so an apply that keeps
			// getting killed mid-flight is re-applied at most maxRebuildAttempts
			// times (persisted across engine restarts) and then dead-lettered,
			// instead of re-running on every drain tick forever (defect d).
			// Cheap/idempotent kinds (KindReindex) keep the plain at-least-once
			// ApplyAndAck — re-enqueuing is harmless and coalesces.
			if rec.Kind == requests.KindRebuild {
				outcome, applyErr := requests.ApplyAndAckBounded(dir, rec, maxRebuildAttempts, apply)
				if logger != nil {
					switch {
					case applyErr != nil:
						logger.Warn("requests: rebuild apply/ack failed", "dir", dir, "id", rec.ID, "attempts", rec.Attempts, "err", applyErr)
					case outcome == requests.OutcomeCrashed:
						logger.Warn("requests: rebuild apply crashed mid-flight; will retry (bounded)", "dir", dir, "id", rec.ID, "attempt", rec.Attempts+1, "max", maxRebuildAttempts)
					case outcome == requests.OutcomeDeadLettered:
						logger.Error("requests: rebuild dead-lettered after max attempts; giving up (not re-running)", "dir", dir, "id", rec.ID, "max", maxRebuildAttempts)
					}
				}
				continue
			}
			applyErr := requests.ApplyAndAck(dir, rec, apply)
			if applyErr != nil && logger != nil {
				logger.Warn("requests: apply/ack failed", "dir", dir, "id", rec.ID, "kind", rec.Kind, "err", applyErr)
			}
		}
	}
	return nil
}

// applyRequest dispatches a single drained Record to the same in-process
// call the monolith/engine makes when it owns the scheduler/rebuild
// entrypoint directly. An unknown/future kind is reported as an error in the
// ack rather than silently dropped, so the producer can observe the
// mismatch.
func applyRequest(scheduler *sched.Scheduler, rebuildFn RebuildFunc, rec requests.Record) error {
	switch rec.Kind {
	case requests.KindReindex:
		if scheduler == nil {
			return fmt.Errorf("requests: reindex request for %s but no scheduler is attached", rec.RepoPath)
		}
		scheduler.Enqueue(rec.RepoPath)
		return nil
	case requests.KindRebuild:
		if rebuildFn == nil {
			return fmt.Errorf("requests: rebuild request %s but no rebuild entrypoint is attached", rec.ID)
		}
		var args proto.RebuildArgs
		if err := json.Unmarshal(rec.Payload, &args); err != nil {
			return fmt.Errorf("requests: decode rebuild payload for %s: %w", rec.ID, err)
		}
		// Engine-side single-flight (defect c, epic #5729): serialise rebuilds
		// of the SAME group so at most one rebuildFn runs at a time on the live
		// (engine drain) path — the split-mode analogue of Service.groupRebuildMu,
		// which guards nothing here because Service.Rebuild returns before
		// reaching it. Different groups are keyed separately and still overlap.
		// The guard wraps ONLY the rebuild call and is released even if
		// rebuildFn panics (defer), so a crash cannot leak the slot.
		release := engineGroupRebuildGuard.acquire(args.Group)
		defer release()
		// Bounded-idempotency (defect d) is handled one layer up by
		// ApplyAndAckBounded, which persists an attempt claim before this runs;
		// a crash mid-rebuild is retried at most maxRebuildAttempts times across
		// engine restarts, then dead-lettered — never an unbounded redrain loop.
		_, _, err := rebuildFn(args)
		return err
	default:
		return fmt.Errorf("requests: unsupported kind %q", rec.Kind)
	}
}

// startRequestsDrainLoop starts the periodic drain goroutine and returns a
// stop func (ep.add-compatible) that halts it. Only meaningful when a
// scheduler is attached and SplitModeEnabled() — see the call site in
// startEnginePlane (engineplane.go), which gates on both. rebuildFn is
// threaded through to applyRequest for KindRebuild dispatch (may be nil).
func startRequestsDrainLoop(scheduler *sched.Scheduler, rebuildFn RebuildFunc, logger *slog.Logger) (stop func()) {
	done := make(chan struct{})
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		ticker := time.NewTicker(requestsDrainInterval)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				if err := drainRequestsOnce(requestsRoot(), scheduler, rebuildFn, logger); err != nil && logger != nil {
					logger.Warn("requests: drain pass failed", "err", err)
				}
			}
		}
	}()
	return func() {
		close(done)
		<-stopped
	}
}
