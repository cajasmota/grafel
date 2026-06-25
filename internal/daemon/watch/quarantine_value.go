// Package watch — quarantine_value.go
//
// Q4 of the self-healing index quarantine (epic #5394, tier T2: VALUE-based).
//
// Q1 (quarantine.go) catches a directory that *churns* pathologically — a build
// loop arming reindex after reindex. That is the loudest signal. This file adds
// the quieter, complementary one: a directory that is **expensive to index AND
// never used**.
//
//	churn detector (T1):  "this dir keeps re-triggering work"   → cost over time
//	value detector (T2):  "this dir costs a lot and earns nothing" → cost ∧ ¬use
//
// The data already exists — we just join two cheap per-directory signals:
//
//   - COST: how expensive a directory is to index/parse. Recorded via
//     RecordIndexCost(repo, dir, cost). The caller supplies whatever proxy it
//     has cheaply to hand: parse wall-time (ms), entity count, or file count —
//     all monotonic "work" units. We accumulate the latest cost per directory.
//   - USAGE: whether anything under the directory has been queried or referenced
//     recently. This reuses the exact access signal Q3's Recover hook already
//     observes (an MCP tool resolving an entity whose source file lives under
//     the dir) — NoteUsage(repo, path) stamps "last used = now".
//
// Detect / Quarantine: a directory is quarantined with signal="value" when, at
// the moment fresh cost is recorded for it, BOTH hold:
//
//	(1) its accumulated index cost ≥ CostThreshold        (expensive), and
//	(2) it has had ZERO usage for at least UnusedGrace     (never used).
//
// SAFETY (same non-negotiable bar as T1):
//   - Conservative: a CHEAP dir is never quarantined no matter how unused, and a
//     RECENTLY-USED dir is never quarantined no matter how expensive. Both gates
//     must trip.
//   - A long grace period (UnusedGrace ≫ any plausible query gap) so a dir that
//     is merely momentarily idle is not mistaken for dead.
//   - Pins are respected; the repo root is never quarantined (relDir guards it).
//   - Reuses the Q1 kill switch (GRAFEL_QUARANTINE_DISABLE disables BOTH
//     detectors) plus a dedicated GRAFEL_QUARANTINE_VALUE_DISABLE for T2 only.
//   - Persists through the shared <repo>/.grafel/quarantine.json, and self-heals
//     exactly like a churn quarantine: a later query un-quarantines it via Q3
//     Recover, and the quiet-window Sweep recovers it too. (A value quarantine
//     records lastEvent=now so Sweep measures quiet from the quarantine moment —
//     and any subsequent NoteUsage immediately recovers it.)
//
// Everything here is goroutine-safe under the tracker's existing mutex and
// clock-injectable through the tracker's `now`, so the detect logic is testable
// deterministically with an injected cost + usage and a fake clock.
package watch

import (
	"os"
	"strconv"
	"time"
)

// Value-detector tuning. Env-overridable (see valueConfig). Defaults are
// deliberately conservative.
//
//   - defaultCostThreshold (2000): a directory must reach this much accumulated
//     "index work" (entities/files parsed, or parse-ms — caller's unit) before
//     it is even a candidate. A handful of small source files stays well under
//     it; only a genuinely heavy directory (a large generated/vendored tree)
//     qualifies. Set high so cheap dirs are categorically excluded.
//   - defaultUnusedGrace (24h): the dir must have gone completely unqueried /
//     unreferenced for a full day before "unused" is believed. Far longer than
//     any realistic gap between two queries that touch a live area of the repo.
const (
	defaultCostThreshold = 2000
	defaultUnusedGrace   = 24 * time.Hour
)

// valueConfig holds the resolved (env-aware) T2 thresholds.
type valueConfig struct {
	costThreshold int64
	unusedGrace   time.Duration
	disabled      bool
}

// loadValueConfig parses the T2 env each time a detector is created. Recognised:
//
//	GRAFEL_QUARANTINE_VALUE_DISABLE=1        — turn the T2 detector off only
//	GRAFEL_QUARANTINE_VALUE_COST=<n>         — cost threshold (work units)
//	GRAFEL_QUARANTINE_VALUE_UNUSED_SEC=<n>   — unused grace period, seconds
//
// (The Q1 GRAFEL_QUARANTINE_DISABLE kill switch disables the whole tracker, so
// it is honoured upstream in every public entry point and not re-read here.)
func loadValueConfig() valueConfig {
	c := valueConfig{
		costThreshold: defaultCostThreshold,
		unusedGrace:   defaultUnusedGrace,
	}
	if v := os.Getenv("GRAFEL_QUARANTINE_VALUE_DISABLE"); v == "1" || v == "true" {
		c.disabled = true
	}
	if v := os.Getenv("GRAFEL_QUARANTINE_VALUE_COST"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n > 0 {
			c.costThreshold = n
		}
	}
	if v := os.Getenv("GRAFEL_QUARANTINE_VALUE_UNUSED_SEC"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			c.unusedGrace = time.Duration(n) * time.Second
		}
	}
	return c
}

// dirValue is the per-directory value bookkeeping: accumulated index cost and
// the last time the directory was queried/referenced.
type dirValue struct {
	// cost is the latest recorded accumulated index cost (work units) for the
	// directory. Higher = more expensive to index.
	cost int64
	// lastUse is when content under the directory was last queried/referenced.
	// The zero value means "never used since the detector started observing it";
	// firstSeen anchors the grace clock in that case.
	lastUse time.Time
	// firstSeen is when this directory was first observed by the value detector
	// (first cost record). Used as the grace baseline when lastUse is zero, so a
	// brand-new expensive dir is not quarantined before it has had a fair chance
	// to be queried.
	firstSeen time.Time
}

// valueDetector holds the T2 state. It does not own a mutex: every method runs
// under the parent QuarantineTracker's mutex (callers hold q.mu), so the state
// is consistent with the churn + quarantine maps it cooperates with.
type valueDetector struct {
	cfg valueConfig
	// byDir[repo][relDir] → cost+usage bookkeeping.
	byDir map[string]map[string]*dirValue
}

// ensureValueLocked lazily constructs the value detector. Caller holds q.mu.
func (q *QuarantineTracker) ensureValueLocked() *valueDetector {
	if q.value == nil {
		q.value = &valueDetector{
			cfg:   loadValueConfig(),
			byDir: make(map[string]map[string]*dirValue),
		}
	}
	return q.value
}

// valueEntryLocked returns (creating if needed) the bookkeeping for repo/rel.
// Caller holds q.mu.
func (v *valueDetector) entry(repo, rel string, now time.Time) *dirValue {
	byDir := v.byDir[repo]
	if byDir == nil {
		byDir = make(map[string]*dirValue)
		v.byDir[repo] = byDir
	}
	dv := byDir[rel]
	if dv == nil {
		dv = &dirValue{firstSeen: now}
		byDir[rel] = dv
	}
	return dv
}

// RecordIndexCost records that indexing/parsing the directory containing path
// cost `cost` work units (parse-ms, entity count, or file count — any monotonic
// "work" proxy the indexer has cheaply to hand). It then evaluates the T2
// predicate and quarantines the directory if it is now expensive ∧ unused.
//
// `path` may be the directory itself or any file under it — its directory is
// what we attribute the cost to (matching how Observe/Recover key on relDir).
//
// Additive and safe to call from the index path: nil/disabled/out-of-repo and
// already-quarantined are cheap no-ops. Returns true if THIS call quarantined
// the directory.
func (q *QuarantineTracker) RecordIndexCost(repo, path string, cost int64) (quarantined bool) {
	if q == nil || q.cfg.disabled || cost <= 0 {
		return false
	}
	rel, ok := relDir(repo, path)
	if !ok {
		return false
	}
	now := q.now()

	q.mu.Lock()
	defer q.mu.Unlock()
	q.ensureLoadedLocked(repo)

	v := q.ensureValueLocked()
	if v.cfg.disabled {
		return false
	}

	dv := v.entry(repo, rel, now)
	// Accumulate cost. We keep the running max so a dir that was indexed cheaply
	// once and expensively later is judged on its worst (most expensive) pass,
	// and a re-index never "forgets" how expensive the dir is.
	if cost > dv.cost {
		dv.cost = cost
	}

	// Already quarantined (this dir or an ancestor) → nothing to do.
	if q.isQuarantinedRelLocked(repo, rel) {
		return false
	}

	// Gate 1 — expensive?
	if dv.cost < v.cfg.costThreshold {
		return false
	}
	// Gate 2 — unused for the full grace period? Measure from the last use, or
	// from when we first saw the dir if it has never been used.
	since := dv.lastUse
	if since.IsZero() {
		since = dv.firstSeen
	}
	if now.Sub(since) < v.cfg.unusedGrace {
		return false
	}

	// Both gates trip: expensive AND unused. Quarantine with signal="value".
	detail := "cost " + strconv.FormatInt(dv.cost, 10) + " unused for " + v.cfg.unusedGrace.String()
	q.quarantineLocked(repo, rel, "value", detail, now)
	return true
}

// NoteUsage stamps "the directory containing path was just queried/referenced"
// so the value detector will never treat it as unused. It reuses the same
// access signal Q3's Recover observes — wire it from the MCP query hook
// alongside Recover so both the "recover a quarantined dir" and the "keep a live
// dir out of value-quarantine" effects fire from one access.
//
// Additive, nil/disabled/out-of-repo safe, cheap (a map stamp).
func (q *QuarantineTracker) NoteUsage(repo, path string) {
	if q == nil || q.cfg.disabled {
		return
	}
	rel, ok := relDir(repo, path)
	if !ok {
		return
	}
	now := q.now()

	q.mu.Lock()
	defer q.mu.Unlock()

	v := q.ensureValueLocked()
	if v.cfg.disabled {
		return
	}
	// Stamp usage for the directory itself and every ancestor up to the repo
	// root, so a query for a/b/c/x.go marks a, a/b and a/b/c all as used — an
	// expensive parent whose children are queried is genuinely in use.
	cur := rel
	for {
		dv := v.entry(repo, cur, now)
		dv.lastUse = now
		parent := slashDir(cur)
		if parent == cur {
			return
		}
		cur = parent
	}
}
