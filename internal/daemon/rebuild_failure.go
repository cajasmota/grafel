package daemon

import (
	"sync"
	"time"

	"github.com/cajasmota/grafel/internal/statusfile"
)

// rebuild_failure.go — #5822 sub-ask 3: makes a rebuild-watchdog SIGKILL (or
// any other hard rebuild failure) VISIBLE instead of silent.
//
// Before this file existed, a per-repo rebuild watchdog firing (#5143's
// defaultPerRepoRebuildTimeout, cmd/grafel daemonRebuildFuncCore) discarded
// the result entirely: nothing was persisted, so `grafel status` kept
// showing the previous (now stale) index with no error or warning — the
// only trace was daemon.err ("repo index timed out after 30m0s",
// "subprocess-indexer: exited ... signal: killed").
//
// rebuildFailures is the in-memory "last rebuild FAILED" registry, keyed by
// repo path. It is consulted by writeRepoStatusFile on EVERY status-plane
// write (heartbeat tick + on-change refresh) so the marker survives being
// overwritten by the next periodic write — writeRepoStatusFile reconstructs
// its statusfile.File from scratch on every call, so without this registry
// a marker set here would vanish again within one heartbeat interval
// (default 5s). The marker is cleared ONLY by a subsequent SUCCESSFUL
// rebuild of the same repo (ClearRebuildFailure) — never by the passage of
// time alone — so a stale FAILED line never lingers, but it also never
// silently disappears while the underlying problem is unresolved.
//
// Scope/trade-off: this registry is in-process only and does not survive an
// engine restart, mirroring the existing status-plane contract (e.g.
// indexstate.RepoStates is also in-memory/per-process). daemon.err already
// gives an operator a restart-durable trace; a repo that is still genuinely
// oversized will simply re-trip the watchdog on the very next rebuild
// attempt.
var (
	rebuildFailureMu sync.Mutex
	rebuildFailures  = map[string]*statusfile.RebuildFailure{}
)

// RecordRebuildFailure persists a "last rebuild FAILED" marker for repoPath —
// e.g. the per-repo watchdog SIGKILL or any other hard rebuild failure — and
// immediately flushes repoPath's status-plane sidecar so `grafel status` /
// `grafel doctor` surface it right away rather than waiting for the next
// heartbeat tick.
//
// This does NOT touch or clobber the last-good graph.fb: it is an ADDITIONAL
// failure marker recorded alongside it, cleared only by a subsequent
// successful rebuild (see ClearRebuildFailure).
func RecordRebuildFailure(repoPath, reason, ref, commit string) {
	rebuildFailureMu.Lock()
	rebuildFailures[repoPath] = &statusfile.RebuildFailure{
		Reason: reason,
		Ref:    ref,
		Commit: commit,
		At:     time.Now().UTC(),
	}
	rebuildFailureMu.Unlock()
	writeRepoStatusFile(repoPath, nil)
}

// ClearRebuildFailure removes any previously recorded rebuild-failure marker
// for repoPath — called after a SUCCESSFUL rebuild (#5822) — and flushes the
// status-plane sidecar so a stale FAILED line does not linger after a good
// rebuild. A no-op (no extra write) when no marker was recorded.
func ClearRebuildFailure(repoPath string) {
	rebuildFailureMu.Lock()
	_, had := rebuildFailures[repoPath]
	delete(rebuildFailures, repoPath)
	rebuildFailureMu.Unlock()
	if had {
		writeRepoStatusFile(repoPath, nil)
	}
}

// currentRebuildFailure returns a COPY of the in-memory marker for repoPath,
// or nil when none is recorded. Consulted by writeRepoStatusFile on every
// status-plane write so the marker survives being overwritten by later
// heartbeat/on-change writes that don't know about it.
func currentRebuildFailure(repoPath string) *statusfile.RebuildFailure {
	rebuildFailureMu.Lock()
	defer rebuildFailureMu.Unlock()
	f := rebuildFailures[repoPath]
	if f == nil {
		return nil
	}
	cp := *f
	return &cp
}
