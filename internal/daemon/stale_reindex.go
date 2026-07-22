package daemon

// stale_reindex.go — #5907 FIX 2: the ACTION arm for stale-on-disk-format
// detection. Detection (graph.ReindexRequiredReason, surfaced on the status
// plane by statuswriter.go) has, until now, ZERO action-consumers: a repo
// whose graph.fb was written by an older grafel build than this binary
// supports sits idle forever, silently serving nothing, until a human runs
// `grafel index`. This closes that silent stall by having the engine
// auto-enqueue a reindex through the SAME requests→drain→scheduler plumbing
// Service.Index already uses — the daemon-side equivalent of the CLI's
// FormatVersionError → full-reindex fallback.
//
// The load-bearing property is the LOOP-GUARD (the #5891-class hazard): the
// engine's status writer recomputes ReindexRequired on EVERY heartbeat, so a
// naive "if required { enqueue }" would fire a reindex request storm — one per
// heartbeat for the whole time the stale graph is on disk, including the entire
// duration of the reindex it already triggered. staleReindexGuard makes the
// enqueue fire AT MOST ONCE per (repo, stale generation):
//
//   - The fingerprint is (graph.fb mtime | reindex reason). The mtime advances
//     ONLY when graph.fb is rewritten — i.e. a fresh index actually lands — so
//     every heartbeat observing the same stale file computes the SAME
//     fingerprint and is deduped. While the triggered reindex is in flight the
//     stale file is untouched (the gen `current` pointer is flipped only when
//     the new graph is complete), so the fingerprint is stable and NO second
//     request is written.
//   - When the reindex completes and the graph is current, ReindexRequired
//     flips false; maybeEnqueue then FORGETS the repo's fingerprint, so the
//     guard self-clears and a genuinely NEW stale generation later (distinct
//     mtime) can fire exactly one fresh request — but a current repo never does.

import (
	"fmt"
	"log/slog"
	"sync"

	"github.com/cajasmota/grafel/internal/daemon/requests"
)

// staleReindexGuard tracks, per repo path, the stale-format fingerprint we have
// already auto-enqueued a reindex for, so we never enqueue twice for the same
// stale generation. Concurrency-safe: writeRepoStatusFile runs on the single
// statusWriter goroutine today, but the guard is package-global and defended by
// a mutex so a future second caller (e.g. a startup reconcile pass) cannot race
// it into a double-enqueue.
type staleReindexGuard struct {
	mu sync.Mutex
	// seen maps repoPath -> the stale fingerprint a reindex was last enqueued
	// for. Absence means "no outstanding auto-reindex for a stale generation".
	seen map[string]string
}

func newStaleReindexGuard() *staleReindexGuard {
	return &staleReindexGuard{seen: map[string]string{}}
}

// defaultStaleReindexGuard is the process-wide guard used by writeRepoStatusFile.
var defaultStaleReindexGuard = newStaleReindexGuard()

// staleFingerprint identifies one stale generation of repoPath's on-disk
// graph.fb: the file mtime (advances only on a real rewrite) plus the
// reason string (which names the found format version). It is stable across
// heartbeats that observe the same stale file, and changes the moment a new
// graph is written — the two properties the loop-guard relies on.
func staleFingerprint(graphFBMtime int64, reason string) string {
	return fmt.Sprintf("%d|%s", graphFBMtime, reason)
}

// maybeEnqueue is the loop-guarded auto-reindex arm. When required is true and
// this exact (repoPath, fingerprint) has not already been enqueued, it writes a
// single KindReindex request into repoPath's control-plane requests dir — the
// engine's drain loop then applies it via scheduler.Enqueue, exactly as an
// explicit `grafel index --async` would in split mode — and records the
// fingerprint so subsequent heartbeats are deduped. When required is false it
// forgets any recorded fingerprint (self-clear). Returns true iff it wrote a
// request this call. A write failure is logged (best-effort, like the rest of
// the status writer) and NOT recorded, so the next heartbeat retries.
func (g *staleReindexGuard) maybeEnqueue(repoPath string, required bool, fingerprint string, logger *slog.Logger) bool {
	g.mu.Lock()
	defer g.mu.Unlock()

	if !required {
		delete(g.seen, repoPath)
		return false
	}
	if g.seen[repoPath] == fingerprint {
		return false // already enqueued for this exact stale generation
	}
	if _, err := requests.Write(requestsDirForRepo(repoPath), requests.Record{
		Kind:     requests.KindReindex,
		RepoPath: repoPath,
	}); err != nil {
		if logger != nil {
			logger.Warn("statusfile: auto-reindex enqueue failed", "repo", repoPath, "err", err)
		}
		return false
	}
	g.seen[repoPath] = fingerprint
	return true
}
