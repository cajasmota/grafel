// Package watch — quarantine.go
//
// Self-healing index quarantine (epic #5394, tier T1: churn-based).
//
// The static skip list (skip.go) + .gitignore (gitignore.go, #5395) catch the
// ~90% of *known* index trash cheaply. This file is the ADAPTIVE layer that
// catches the long tail: a custom build-output dir, non-gitignored generated
// content, or a per-repo oddity the static list can't anticipate, that churns
// pathologically and trips a continuous reindex loop (the #5392 incident
// class).
//
// Mechanism (T1 — the clearest, safest signal):
//
//		Observe → Detect → Quarantine → (persist) → Self-heal
//
//	  - Observe: every watcher event that survives the static + gitignore filter
//	    is attributed to its DIRECTORY. We keep a sliding-window churn count per
//	    directory ("this dir produced N reindex-arming events in the last
//	    ChurnWindow").
//	  - Detect / Quarantine: when a directory's windowed churn crosses
//	    ChurnThreshold it is QUARANTINED — added to a per-repo set so subsequent
//	    events under it are dropped at the event boundary (it stops arming
//	    reindexes), and the decision is appended to an audit log.
//	  - Persist: the quarantine set is written to <repo>/.grafel/quarantine.json
//	    so it survives a daemon restart (a build loop that quarantined itself
//	    does not re-thrash on reboot).
//	  - Self-heal: Sweep() periodically re-evaluates quarantined dirs and
//	    un-quarantines any that have gone quiet for HealQuiet — so a dir that was
//	    a build output during a build window, but is now a legitimate (or simply
//	    idle) source dir, recovers automatically.
//
// SAFETY (epic #5394, non-negotiable): we must NEVER quarantine a legitimately
// active source directory. Guarding invariants:
//   - Conservative, *sustained* churn requirement: a normal human edit burst
//     (a handful of saves) stays well under ChurnThreshold; only a mechanical
//     build loop (tens–hundreds of writes within the window) trips it.
//   - Hysteresis via the heal cool-down (HealQuiet ≫ ChurnWindow) so we never
//     flap quarantine↔active.
//   - Everything is env-tunable so an operator can loosen/tighten or disable.
//   - The quarantine set extends — never duplicates — the static + gitignore
//     skip (this layer only ever sees paths those already let through).
//
// The tracker is self-contained and clock-injectable (the `now` field) so the
// detect/quarantine/heal logic is testable deterministically without real
// timing or a live fsnotify stream.
package watch

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"sync"
	"time"
)

// Quarantine tuning. All values are env-overridable (see quarantineConfig).
//
// Defaults are deliberately conservative: a directory must produce
// defaultChurnThreshold (40) reindex-arming events within defaultChurnWindow
// (2 min) to be quarantined — far above any human edit burst, squarely in
// mechanical-build-loop territory. A quarantined dir self-heals after
// defaultHealQuiet (15 min) of no observed events.
const (
	defaultChurnThreshold = 40
	defaultChurnWindow    = 2 * time.Minute
	defaultHealQuiet      = 15 * time.Minute
)

// quarantineConfig holds the resolved (env-aware) thresholds.
type quarantineConfig struct {
	threshold int
	window    time.Duration
	healQuiet time.Duration
	disabled  bool
}

var (
	quarantineCfgOnce sync.Once
	quarantineCfg     quarantineConfig
)

// loadQuarantineConfig parses the env once. Recognised vars:
//
//	GRAFEL_QUARANTINE_DISABLE=1            — turn the feature off entirely
//	GRAFEL_QUARANTINE_CHURN_THRESHOLD=<n>  — events/window to quarantine
//	GRAFEL_QUARANTINE_CHURN_WINDOW_SEC=<n> — sliding window, seconds
//	GRAFEL_QUARANTINE_HEAL_QUIET_SEC=<n>   — quiet period before auto-heal
func loadQuarantineConfig() quarantineConfig {
	quarantineCfgOnce.Do(func() {
		c := quarantineConfig{
			threshold: defaultChurnThreshold,
			window:    defaultChurnWindow,
			healQuiet: defaultHealQuiet,
		}
		if v := os.Getenv("GRAFEL_QUARANTINE_DISABLE"); v == "1" || v == "true" {
			c.disabled = true
		}
		if v := os.Getenv("GRAFEL_QUARANTINE_CHURN_THRESHOLD"); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n > 0 {
				c.threshold = n
			}
		}
		if v := os.Getenv("GRAFEL_QUARANTINE_CHURN_WINDOW_SEC"); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n > 0 {
				c.window = time.Duration(n) * time.Second
			}
		}
		if v := os.Getenv("GRAFEL_QUARANTINE_HEAL_QUIET_SEC"); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n > 0 {
				c.healQuiet = time.Duration(n) * time.Second
			}
		}
		quarantineCfg = c
	})
	return quarantineCfg
}

// quarantineSweepInterval is how often the watcher's self-heal sweep runs. It
// is a fraction of the heal window (capped to a sane floor) so recovery is
// responsive without busy-looping. Env-overridable via
// GRAFEL_QUARANTINE_SWEEP_SEC.
func quarantineSweepInterval() time.Duration {
	if v := os.Getenv("GRAFEL_QUARANTINE_SWEEP_SEC"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return time.Duration(n) * time.Second
		}
	}
	c := loadQuarantineConfig()
	d := c.healQuiet / 4
	if d < time.Minute {
		d = time.Minute
	}
	return d
}

// QuarantineReason records why a directory was quarantined, for the audit log
// and the (future, Q2) transparency surface.
type QuarantineReason struct {
	// Rel is the directory path relative to the repo root (forward-slash).
	Rel string `json:"rel"`
	// Signal is the triggering detector, e.g. "churn".
	Signal string `json:"signal"`
	// Detail is a human-readable explanation (e.g. "47 events in 2m0s").
	Detail string `json:"detail"`
	// At is when the quarantine decision was made.
	At time.Time `json:"at"`
	// Pinned, when true, means an operator explicitly quarantined this path
	// (or asked never to auto-heal it). Reserved for Q2; auto-heal skips
	// pinned entries.
	Pinned bool `json:"pinned,omitempty"`
}

// dirChurn is the sliding-window churn bookkeeping for one directory.
type dirChurn struct {
	// windowStart is the start of the current counting window.
	windowStart time.Time
	count       int
	// lastEvent is when we last observed an event for this dir (used by heal).
	lastEvent time.Time
}

// QuarantineTracker is the per-repo churn observer + quarantine set. One
// tracker instance serves all repos; state is keyed by absolute repo root.
//
// It is goroutine-safe. All decisions go through a single mutex because the
// per-event work is cheap (map lookups + integer math) and the volume that
// reaches this layer is already filtered down by the static + gitignore skips.
type QuarantineTracker struct {
	cfg quarantineConfig
	// now is injectable for deterministic tests; defaults to time.Now.
	now func() time.Time
	// audit, when non-nil, is invoked for every quarantine / un-quarantine
	// decision (the daemon wires this to its structured logger).
	audit func(event, repo, rel, detail string)

	mu sync.Mutex
	// churn[repo][relDir] → sliding-window state.
	churn map[string]map[string]*dirChurn
	// quarantined[repo][relDir] → reason. Presence == quarantined.
	quarantined map[string]map[string]QuarantineReason
	// loaded tracks which repos have had their persisted set read in.
	loaded map[string]bool

	// content is the T3 content-signature detector (#5620). It accumulates a
	// per-directory generated-vs-total fingerprint tally and quarantines dirs
	// whose content is overwhelmingly machine-generated. All of its logic
	// lives in quarantine_content.go; this is the only coupling point. nil
	// until first use (lazily initialised, kill-switch aware).
	content *contentDetector
}

// NewQuarantineTracker constructs a tracker. audit may be nil.
func NewQuarantineTracker(audit func(event, repo, rel, detail string)) *QuarantineTracker {
	return &QuarantineTracker{
		cfg:         loadQuarantineConfig(),
		now:         time.Now,
		audit:       audit,
		churn:       make(map[string]map[string]*dirChurn),
		quarantined: make(map[string]map[string]QuarantineReason),
		loaded:      make(map[string]bool),
	}
}

// relDir returns the slash-form directory of the event path relative to the
// repo root. The repo root itself maps to "." — we never quarantine the root.
// Returns ("", false) when path is not under repo.
func relDir(repo, path string) (string, bool) {
	dir := filepath.Dir(path)
	rel, err := filepath.Rel(repo, dir)
	if err != nil {
		return "", false
	}
	rel = filepath.ToSlash(rel)
	// "." is the repo root itself — never quarantine the root. "" / ".." mean
	// the path is not under the repo.
	if rel == "." || rel == ".." || rel == "" {
		return "", false
	}
	if len(rel) >= 2 && rel[:2] == ".." {
		return "", false
	}
	return rel, true
}

// IsQuarantined reports whether the directory containing path is quarantined.
// Cheap fast path used at the watcher event boundary. Loads persisted state on
// first touch of a repo.
func (q *QuarantineTracker) IsQuarantined(repo, path string) bool {
	if q == nil || q.cfg.disabled {
		return false
	}
	rel, ok := relDir(repo, path)
	if !ok {
		return false
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	q.ensureLoadedLocked(repo)
	return q.isQuarantinedRelLocked(repo, rel)
}

// isQuarantinedRelLocked reports whether rel OR any ancestor directory of rel
// is quarantined (so quarantining `app/build` also drops `app/build/sub`).
func (q *QuarantineTracker) isQuarantinedRelLocked(repo, rel string) bool {
	set := q.quarantined[repo]
	if len(set) == 0 {
		return false
	}
	cur := rel
	for {
		if _, ok := set[cur]; ok {
			return true
		}
		parent := slashDir(cur)
		if parent == cur {
			return false
		}
		cur = parent
	}
}

// slashDir returns the parent of a slash-form rel dir, or the input when at the
// top level (which signals "no more ancestors").
func slashDir(rel string) string {
	i := lastSlash(rel)
	if i < 0 {
		return rel // top-level: parent would be ".", which we never quarantine
	}
	return rel[:i]
}

func lastSlash(s string) int {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == '/' {
			return i
		}
	}
	return -1
}

// Observe records one reindex-arming event for the directory containing path
// and returns true if the event should be DROPPED (i.e. the directory is — or
// just became — quarantined). The caller arms a reindex only when this returns
// false.
//
// This is the single hot-path entry point from handleEvent.
func (q *QuarantineTracker) Observe(repo, path string) (drop bool) {
	if q == nil || q.cfg.disabled {
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

	// Already quarantined (this dir or an ancestor) → drop, no further work.
	if q.isQuarantinedRelLocked(repo, rel) {
		// Keep lastEvent fresh so heal only fires after genuine quiet.
		q.touchLocked(repo, rel, now)
		return true
	}

	// Sliding-window churn accounting.
	byDir := q.churn[repo]
	if byDir == nil {
		byDir = make(map[string]*dirChurn)
		q.churn[repo] = byDir
	}
	dc := byDir[rel]
	if dc == nil {
		dc = &dirChurn{windowStart: now}
		byDir[rel] = dc
	}
	// Roll the window if it has elapsed.
	if now.Sub(dc.windowStart) > q.cfg.window {
		dc.windowStart = now
		dc.count = 0
	}
	dc.count++
	dc.lastEvent = now

	if dc.count >= q.cfg.threshold {
		detail := strconv.Itoa(dc.count) + " events in " + q.cfg.window.String()
		q.quarantineLocked(repo, rel, "churn", detail, now)
		// Reset the counter so a future un-quarantine starts clean.
		delete(byDir, rel)
		return true
	}

	// T3 content-signature detection (#5620): the churn detector above catches
	// dirs that thrash; this catches dirs whose *content* is overwhelmingly
	// machine-generated (minified/vendored/lockfiles/@generated) even at low
	// churn. All logic lives in quarantine_content.go. It returns the trip
	// detail when the generated-share crosses the threshold; quarantine then.
	if detail, trip := q.observeContentLocked(repo, rel, path, now); trip {
		q.quarantineLocked(repo, rel, "content", detail, now)
		delete(byDir, rel)
		return true
	}
	return false
}

// touchLocked refreshes lastEvent for a quarantined dir's nearest quarantined
// ancestor (or itself), so Sweep's quiet timer reflects ongoing churn.
func (q *QuarantineTracker) touchLocked(repo, rel string, now time.Time) {
	set := q.quarantined[repo]
	cur := rel
	for {
		if _, ok := set[cur]; ok {
			if dc := q.churnEntryLocked(repo, cur); dc != nil {
				dc.lastEvent = now
			} else {
				q.churn[repo] = ensureMap(q.churn[repo])
				q.churn[repo][cur] = &dirChurn{lastEvent: now, windowStart: now}
			}
			return
		}
		parent := slashDir(cur)
		if parent == cur {
			return
		}
		cur = parent
	}
}

func (q *QuarantineTracker) churnEntryLocked(repo, rel string) *dirChurn {
	if m := q.churn[repo]; m != nil {
		return m[rel]
	}
	return nil
}

func ensureMap(m map[string]*dirChurn) map[string]*dirChurn {
	if m == nil {
		return make(map[string]*dirChurn)
	}
	return m
}

// quarantineLocked records a quarantine decision, persists, and audits.
func (q *QuarantineTracker) quarantineLocked(repo, rel, signal, detail string, now time.Time) {
	set := q.quarantined[repo]
	if set == nil {
		set = make(map[string]QuarantineReason)
		q.quarantined[repo] = set
	}
	if _, exists := set[rel]; exists {
		return
	}
	set[rel] = QuarantineReason{Rel: rel, Signal: signal, Detail: detail, At: now}
	// Track lastEvent so heal can measure quiet from the quarantine moment.
	q.churn[repo] = ensureMap(q.churn[repo])
	q.churn[repo][rel] = &dirChurn{lastEvent: now, windowStart: now}
	q.persistLocked(repo)
	if q.audit != nil {
		q.audit("quarantine", repo, rel, signal+": "+detail)
	}
}

// Sweep re-evaluates every quarantined directory and auto-un-quarantines any
// that has been quiet for at least healQuiet. Returns the rels that were
// healed (for logging/observability). Safe to call on a timer.
func (q *QuarantineTracker) Sweep() (healed map[string][]string) {
	if q == nil || q.cfg.disabled {
		return nil
	}
	now := q.now()
	healed = make(map[string][]string)

	q.mu.Lock()
	defer q.mu.Unlock()

	for repo, set := range q.quarantined {
		var dirty bool
		for rel, reason := range set {
			if reason.Pinned {
				continue // operator-pinned: never auto-heal
			}
			last := reason.At
			if dc := q.churnEntryLocked(repo, rel); dc != nil && dc.lastEvent.After(last) {
				last = dc.lastEvent
			}
			if now.Sub(last) >= q.cfg.healQuiet {
				delete(set, rel)
				if m := q.churn[repo]; m != nil {
					delete(m, rel)
				}
				q.content.dropContentDir(repo, rel)
				dirty = true
				healed[repo] = append(healed[repo], rel)
				if q.audit != nil {
					q.audit("unquarantine", repo, rel, "quiet for "+q.cfg.healQuiet.String())
				}
			}
		}
		if dirty {
			q.persistLocked(repo)
		}
	}
	if len(healed) == 0 {
		return nil
	}
	return healed
}

// List returns a snapshot of the quarantined directories for a repo, sorted by
// rel. Used by the (Q2) transparency surface and tests.
func (q *QuarantineTracker) List(repo string) []QuarantineReason {
	if q == nil {
		return nil
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	q.ensureLoadedLocked(repo)
	set := q.quarantined[repo]
	out := make([]QuarantineReason, 0, len(set))
	for _, r := range set {
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Rel < out[j].Rel })
	return out
}

// Unquarantine manually removes rel from a repo's quarantine set (operator
// override, Q2). Returns true if it was present.
func (q *QuarantineTracker) Unquarantine(repo, rel string) bool {
	if q == nil {
		return false
	}
	rel = filepath.ToSlash(rel)
	q.mu.Lock()
	defer q.mu.Unlock()
	q.ensureLoadedLocked(repo)
	set := q.quarantined[repo]
	if _, ok := set[rel]; !ok {
		return false
	}
	delete(set, rel)
	if m := q.churn[repo]; m != nil {
		delete(m, rel)
	}
	q.content.dropContentDir(repo, rel)
	q.persistLocked(repo)
	if q.audit != nil {
		q.audit("unquarantine", repo, rel, "manual override")
	}
	return true
}

// Recover auto-un-quarantines the quarantined directory that contains path,
// when that path later proves to be REAL — i.e. its content is queried (an MCP
// tool resolves an entity whose source file is under a quarantined dir) or
// referenced (the graph has an edge into a quarantined dir's content). This is
// the Q3 self-heal-on-demand signal (#5618): we never want to keep hiding data
// that the rest of the system actually needs.
//
// It is the cheap counterpart to Sweep's quiet-window heal: a single query or
// reference un-quarantines immediately rather than waiting out HealQuiet.
//
// Contract:
//   - path is the absolute path to the referenced/queried file (e.g. an
//     entity's source file). Its DIRECTORY is matched against the quarantine
//     set, walking up to the nearest quarantined ancestor (so a query for
//     `app/build/x/y.go` recovers the quarantined `app/build`).
//   - PINNED entries are respected: an operator-pinned dir stays exactly as the
//     user set it and is NOT auto-recovered (returns false).
//   - On a hit it removes the entry, clears the churn bookkeeping so indexing
//     re-arms cleanly, persists, and audits. Returns the recovered rel.
//   - On no quarantined ancestor (the common case) it is a cheap membership
//     check that returns ("", false) — safe to call on every query/reference.
//
// Anti-flap: if the dir was quarantined for genuine churn and is still
// thrashing, the churn detector (Observe) will simply re-quarantine it on the
// next burst — a real-but-churning dir surfaces, then re-quarantines. That is
// the intended behaviour, not a defect.
func (q *QuarantineTracker) Recover(repo, path string) (rel string, recovered bool) {
	if q == nil || q.cfg.disabled {
		return "", false
	}
	dirRel, ok := relDir(repo, path)
	if !ok {
		return "", false
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	q.ensureLoadedLocked(repo)

	set := q.quarantined[repo]
	if len(set) == 0 {
		return "", false
	}
	// Walk up to the nearest quarantined ancestor of the path's directory.
	cur := dirRel
	for {
		if reason, hit := set[cur]; hit {
			if reason.Pinned {
				// Operator-pinned: leave exactly as the user set it.
				return "", false
			}
			delete(set, cur)
			if m := q.churn[repo]; m != nil {
				delete(m, cur)
			}
			q.content.dropContentDir(repo, cur)
			q.persistLocked(repo)
			if q.audit != nil {
				q.audit("unquarantine", repo, cur, "recover on query/reference")
			}
			return cur, true
		}
		parent := slashDir(cur)
		if parent == cur {
			return "", false
		}
		cur = parent
	}
}

// Pin marks rel as operator-pinned so the self-heal sweep never auto-removes
// it (the dir stays quarantined until an explicit Unquarantine). The dir must
// already be quarantined. Returns true if the pin state changed. Q2 operator
// override.
func (q *QuarantineTracker) Pin(repo, rel string) bool {
	return q.setPin(repo, rel, true)
}

// Unpin clears the operator pin on rel so the dir becomes eligible for
// auto-heal again. Returns true if the pin state changed.
func (q *QuarantineTracker) Unpin(repo, rel string) bool {
	return q.setPin(repo, rel, false)
}

func (q *QuarantineTracker) setPin(repo, rel string, pinned bool) bool {
	if q == nil {
		return false
	}
	rel = filepath.ToSlash(rel)
	q.mu.Lock()
	defer q.mu.Unlock()
	q.ensureLoadedLocked(repo)
	set := q.quarantined[repo]
	reason, ok := set[rel]
	if !ok || reason.Pinned == pinned {
		return false
	}
	reason.Pinned = pinned
	set[rel] = reason
	q.persistLocked(repo)
	if q.audit != nil {
		evt := "pin"
		if !pinned {
			evt = "unpin"
		}
		q.audit(evt, repo, rel, "operator override")
	}
	return true
}

// ---- daemon-less file surface (Q2 transparency: CLI + dashboard) ----
//
// The persisted <repo>/.grafel/quarantine.json is the canonical store across
// daemon restarts, and the live tracker reloads it lazily + rewrites it on
// every mutation. These package-level helpers let the CLI and the dashboard
// read and mutate the set WITHOUT a live tracker handle, staying consistent
// with the running daemon (which re-reads the file on its next touch of the
// repo). They are best-effort and never panic on a missing/corrupt file.

// ReadQuarantineFile returns the persisted quarantine set for a repo, sorted by
// rel. A missing or corrupt file yields an empty slice and no error.
func ReadQuarantineFile(repo string) ([]QuarantineReason, error) {
	data, err := os.ReadFile(quarantinePath(repo))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var f quarantineFile
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, err
	}
	out := make([]QuarantineReason, 0, len(f.Dirs))
	for _, r := range f.Dirs {
		if r.Rel != "" {
			out = append(out, r)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Rel < out[j].Rel })
	return out, nil
}

// writeQuarantineFile persists a set of reasons to <repo>/.grafel/quarantine.json
// (atomic-ish temp+rename), mirroring (*QuarantineTracker).persistLocked.
func writeQuarantineFile(repo string, dirs []QuarantineReason) error {
	f := quarantineFile{Version: 1, Dirs: append([]QuarantineReason(nil), dirs...)}
	sort.Slice(f.Dirs, func(i, j int) bool { return f.Dirs[i].Rel < f.Dirs[j].Rel })
	if err := os.MkdirAll(filepath.Join(repo, ".grafel"), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	tmp := quarantinePath(repo) + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, quarantinePath(repo))
}

// UnquarantineFile removes rel from a repo's persisted quarantine set. Returns
// true if it was present (and the file rewritten).
func UnquarantineFile(repo, rel string) (bool, error) {
	rel = filepath.ToSlash(rel)
	dirs, err := ReadQuarantineFile(repo)
	if err != nil {
		return false, err
	}
	out := make([]QuarantineReason, 0, len(dirs))
	found := false
	for _, r := range dirs {
		if r.Rel == rel {
			found = true
			continue
		}
		out = append(out, r)
	}
	if !found {
		return false, nil
	}
	return true, writeQuarantineFile(repo, out)
}

// SetPinFile sets or clears the operator pin on rel in a repo's persisted
// quarantine set. Returns true if the pin state changed.
func SetPinFile(repo, rel string, pinned bool) (bool, error) {
	rel = filepath.ToSlash(rel)
	dirs, err := ReadQuarantineFile(repo)
	if err != nil {
		return false, err
	}
	changed := false
	for i := range dirs {
		if dirs[i].Rel == rel {
			if dirs[i].Pinned != pinned {
				dirs[i].Pinned = pinned
				changed = true
			}
			break
		}
	}
	if !changed {
		return false, nil
	}
	return true, writeQuarantineFile(repo, dirs)
}

// ---- persistence: <repo>/.grafel/quarantine.json ----

// quarantineFile is the on-disk shape.
type quarantineFile struct {
	Version int                `json:"version"`
	Dirs    []QuarantineReason `json:"dirs"`
}

func quarantinePath(repo string) string {
	return filepath.Join(repo, ".grafel", "quarantine.json")
}

// ensureLoadedLocked lazily reads the persisted quarantine set for a repo on
// first touch. Errors are non-fatal (a missing/corrupt file just means "no
// quarantines yet").
func (q *QuarantineTracker) ensureLoadedLocked(repo string) {
	if q.loaded[repo] {
		return
	}
	q.loaded[repo] = true
	data, err := os.ReadFile(quarantinePath(repo))
	if err != nil {
		return
	}
	var f quarantineFile
	if json.Unmarshal(data, &f) != nil {
		return
	}
	set := make(map[string]QuarantineReason, len(f.Dirs))
	for _, r := range f.Dirs {
		if r.Rel == "" {
			continue
		}
		set[r.Rel] = r
	}
	if len(set) > 0 {
		q.quarantined[repo] = set
	}
}

// persistLocked writes the repo's quarantine set to disk. Best-effort: a write
// failure is logged via audit but never blocks the decision.
func (q *QuarantineTracker) persistLocked(repo string) {
	set := q.quarantined[repo]
	f := quarantineFile{Version: 1}
	for _, r := range set {
		f.Dirs = append(f.Dirs, r)
	}
	sort.Slice(f.Dirs, func(i, j int) bool { return f.Dirs[i].Rel < f.Dirs[j].Rel })

	dir := filepath.Join(repo, ".grafel")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		if q.audit != nil {
			q.audit("persist-error", repo, "", err.Error())
		}
		return
	}
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return
	}
	// Atomic-ish: write temp then rename.
	tmp := quarantinePath(repo) + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		if q.audit != nil {
			q.audit("persist-error", repo, "", err.Error())
		}
		return
	}
	_ = os.Rename(tmp, quarantinePath(repo))
}
