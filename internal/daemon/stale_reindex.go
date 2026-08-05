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
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/cajasmota/grafel/internal/daemon/requests"
)

// #6167 — migration batching policy.
//
// The loop guard above bounds requests PER REPO. Nothing bounded them ACROSS
// repos, and statusWriter.writeAll (statuswriter.go:521) walks every registered
// repo on every 5s heartbeat — so the first heartbeat after an fbversion bump
// enqueued one reindex per stale repo, all at once. A user with 140 registered
// repos measured queued=133, seven "heavy write stage starved by index churn"
// escalations and three shutdown-watchdog force-exits over 18 hours.
//
// The scheduler already caps CONCURRENCY (Config.Workers defaults to 2, plus
// RSS-budget admission — sched/scheduler.go:158, :1508). What was unbounded was
// the BACKLOG, and backlog is what hurts here for two reasons:
//
//  1. sched/stagegate.go:489 refuses the heavy-write token for as long as ANY
//     index job is in flight. A 133-deep queue means that condition is
//     continuously true, so group-algo/links only ever run via the drain-barrier
//     escalation at stagegate.go:470 — the seven WARNs in the capture.
//  2. requests.Write persists to disk. A 133-deep durable queue survives the
//     restart the user performs when the dashboard stops responding, so the
//     stampede replays on every restart.
//
// So the throttle belongs HERE, at request production, not at the drain: it
// keeps the on-disk backlog bounded and it creates the idle windows the stage
// gate needs. The guard's own state is in-memory and re-derives from on-disk
// staleness after a restart, so nothing is lost by bounding it.
const (
	// defaultStaleReindexBatchSize is how many auto-reindexes may be
	// outstanding at once. Sized to the scheduler's own default worker count
	// (2) so the migration keeps the pipeline fed without ever queueing behind
	// itself.
	defaultStaleReindexBatchSize = 2

	// defaultStaleReindexCooldown is the enforced quiet period after a batch
	// fully drains, before the next is admitted. This is the idle window in
	// which a deferred heavy write stage can acquire the stage-gate token
	// cleanly instead of waiting out the drain barrier. It only needs to
	// exceed the gate's retry interval, not the stage's runtime — once the
	// stage HOLDS the token, stageBusyLocked keeps index dispatch out for as
	// long as it runs.
	defaultStaleReindexCooldown = 30 * time.Second

	// defaultStaleReindexSlotTTL bounds how long one repo may occupy a batch
	// slot. Without it, a repo whose reindex never completes (crash, permanent
	// index failure) would wedge the whole migration — a worse failure than
	// the stampede it replaces. Generous against the 4m53s worst-case reindex
	// in the user's capture, so a healthy run is never cut short.
	defaultStaleReindexSlotTTL = 15 * time.Minute

	// EnvStaleReindexBatch overrides defaultStaleReindexBatchSize. Escape
	// hatch for an operator who wants a large store to migrate faster (or
	// slower) than the default. Values <= 0 fall back to the default.
	EnvStaleReindexBatch = "GRAFEL_STALE_REINDEX_BATCH"
)

// staleReindexBatchSize resolves the batch size, honouring EnvStaleReindexBatch.
func staleReindexBatchSize() int {
	if v := os.Getenv(EnvStaleReindexBatch); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return defaultStaleReindexBatchSize
}

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

	// --- #6167 migration batching (see the "at scale" note in the file doc) ---

	// inflight maps repoPath -> the time its auto-reindex was admitted, for
	// every repo this guard has enqueued and not yet observed going current.
	// Its SIZE is the outstanding-migration count the batch bounds.
	inflight map[string]time.Time
	// admittedInBatch counts admissions since the current batch opened. The
	// batch closes at batchSize and reopens only after inflight drains AND
	// cooldown elapses — that gap is the idle window the stage gate needs.
	admittedInBatch int
	// batchDrainedAt is when inflight last became empty with a full batch
	// behind it; the cooldown is measured from here.
	batchDrainedAt time.Time

	batchSize int
	cooldown  time.Duration
	slotTTL   time.Duration
	now       func() time.Time
}

func newStaleReindexGuard() *staleReindexGuard {
	return &staleReindexGuard{
		seen:      map[string]string{},
		inflight:  map[string]time.Time{},
		batchSize: staleReindexBatchSize(),
		cooldown:  defaultStaleReindexCooldown,
		slotTTL:   defaultStaleReindexSlotTTL,
		now:       time.Now,
	}
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
		g.releaseSlotLocked(repoPath)
		return false
	}
	if g.seen[repoPath] == fingerprint {
		return false // already enqueued for this exact stale generation
	}
	// #6167: batch admission. A refusal here is a DEFERRAL, not a drop — the
	// fingerprint is deliberately NOT recorded, so the next heartbeat offers
	// this repo again and it is admitted as soon as a batch opens.
	if !g.admitLocked(logger) {
		return false
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
	g.inflight[repoPath] = g.now()
	g.admittedInBatch++
	return true
}

// admitLocked applies the #6167 batch policy. MUST be called with g.mu held.
//
// The rule, in one sentence: admit up to batchSize repos, then admit nothing
// more until every one of them has gone current (or forfeited its slot to the
// TTL) AND cooldown has elapsed since that happened.
//
// The "wait for the batch to fully drain" part is load-bearing and is why this
// is not a plain semaphore. A semaphore of size 2 refills the instant one slot
// frees, so len(scheduler.inflight) never reaches zero and stagegate.go:489
// still never yields the heavy-write token — the exact starvation being fixed.
// Draining the whole batch first is what manufactures the idle window.
func (g *staleReindexGuard) admitLocked(logger *slog.Logger) bool {
	now := g.now()

	// Reclaim slots held past the TTL. Without this a repo whose reindex never
	// completes wedges the migration for every other repo — a strictly worse
	// failure than the stampede. The repo stays in g.seen, so reclaiming its
	// slot does NOT re-enqueue a known-failing reindex; it only unblocks the
	// others.
	for repo, at := range g.inflight {
		if now.Sub(at) >= g.slotTTL {
			delete(g.inflight, repo)
			if logger != nil {
				logger.Warn("statusfile: auto-reindex slot expired — releasing it so the format migration can continue",
					"repo", repo, "held_for", now.Sub(at).Truncate(time.Second), "ttl", g.slotTTL)
			}
			if len(g.inflight) == 0 {
				g.batchDrainedAt = now
			}
		}
	}

	if g.admittedInBatch >= g.batchSize {
		if len(g.inflight) > 0 {
			return false // batch still executing
		}
		if now.Sub(g.batchDrainedAt) < g.cooldown {
			return false // idle window — hands the stage gate its chance
		}
		g.admittedInBatch = 0 // open the next batch
	}
	return g.admittedInBatch < g.batchSize
}

// releaseSlotLocked frees repoPath's batch slot once its graph is current
// again, recording when the batch fully drained so the cooldown can start.
// MUST be called with g.mu held.
func (g *staleReindexGuard) releaseSlotLocked(repoPath string) {
	if _, held := g.inflight[repoPath]; !held {
		return
	}
	delete(g.inflight, repoPath)
	if len(g.inflight) == 0 {
		g.batchDrainedAt = g.now()
	}
}
