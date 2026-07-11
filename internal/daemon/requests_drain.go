package daemon

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

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
// requests.ApplyAndAck. Only KindReindex is understood as of PR4 — see
// internal/daemon/requests's Kind doc comments for why KindSubmitRepair /
// KindDocgenApply / KindEnrichmentEnqueue are defined but not (yet) produced
// or consumed anywhere (their handlers already write their sidecars
// directly and are picked up lazily by the next scheduled index pass, so
// they are already cross-process safe with no engine-side action needed).
//
// logger may be nil (tests exercise the apply path without one).
func drainRequestsOnce(root string, scheduler *sched.Scheduler, logger *slog.Logger) error {
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
			applyErr := requests.ApplyAndAck(dir, rec, func(r requests.Record) error {
				return applyRequest(scheduler, r)
			})
			if applyErr != nil && logger != nil {
				logger.Warn("requests: apply/ack failed", "dir", dir, "id", rec.ID, "kind", rec.Kind, "err", applyErr)
			}
		}
	}
	return nil
}

// applyRequest dispatches a single drained Record to the same in-process
// call the monolith/engine makes when it owns the scheduler directly. This
// is the ONLY kind currently produced (see drainRequestsOnce's doc comment);
// an unknown/future kind is reported as an error in the ack rather than
// silently dropped, so the producer can observe the mismatch.
func applyRequest(scheduler *sched.Scheduler, rec requests.Record) error {
	switch rec.Kind {
	case requests.KindReindex:
		if scheduler == nil {
			return fmt.Errorf("requests: reindex request for %s but no scheduler is attached", rec.RepoPath)
		}
		scheduler.Enqueue(rec.RepoPath)
		return nil
	default:
		return fmt.Errorf("requests: unsupported kind %q", rec.Kind)
	}
}

// startRequestsDrainLoop starts the periodic drain goroutine and returns a
// stop func (ep.add-compatible) that halts it. Only meaningful when a
// scheduler is attached and SplitModeEnabled() — see the call site in
// startEnginePlane (engineplane.go), which gates on both.
func startRequestsDrainLoop(scheduler *sched.Scheduler, logger *slog.Logger) (stop func()) {
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
				if err := drainRequestsOnce(requestsRoot(), scheduler, logger); err != nil && logger != nil {
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
