// Package watch — ChangePoller: the polling change-detector (#6932, arm A).
//
// WHY IT EXISTS. On Linux, inotify costs one watch descriptor per DIRECTORY,
// recursively. Measured through the real Watcher.AddRepo on this repo: 976
// directories per worktree, and GRAFEL_MAX_WORKTREES_PER_REPO defaults to 10,
// so one lane can reach ~10,700 descriptors. fs.inotify.max_user_watches is
// per-UID, host-level and NOT namespaced — every container running as the same
// UID draws from one pool, and a container cannot raise it. The failure mode is
// a watcher that is registered and receives no events: silent.
//
// This poller is the escape hatch. It costs ZERO watch descriptors.
//
// THE MECHANISM — "hybrid B" of #6932, the variant a 14-case spike measured
// against real fsnotify, real git and grafel's real diff.Filter/walk.WalkRepo:
//
//  1. DISCOVERY: `git status --porcelain -z -unormal` per worktree. -unormal
//     collapses an untracked subtree to a single `dir/` line and stays flat at
//     ~60 ms whether there are 0 or 20,000 untracked files. Each reported
//     untracked DIRECTORY is expanded with walk.WalkRepo rooted at the subtree.
//     `-uall` was measured and rejected: it is a full tree readdir in C
//     (~130 ms floor at zero untracked files), so it rents git's walk rather
//     than escaping it.
//
//  2. THE CHANGE DECISION: a stat-sweep of the MANIFEST'S OWN KEY SET, through
//     internal/indexer/diff (LoadManifest + Filter). This is the half that
//     makes the poller complete, and it is not optional:
//
//     - `git status` reports state relative to HEAD, not change since the last
//     poll. An untracked file edited twice yields `?? foo.go` both times; a
//     tracked file edited twice yields ` M a.go` both times. A git-only poller
//     is blind to an agent repeatedly saving the file it is working on, which
//     is the most common action there is.
//
//     - Worse, a git-derived CANDIDATE set (hybrid A) fails edit-then-revert as
//     a PERMANENT silent corruption: once the bytes match HEAD again git
//     reports the tree clean, the file leaves the candidate set forever, and
//     the manifest keeps the edited SHA. Sweeping the manifest's keys converges
//     it. See TestChangePoller_EditThenRevertConverges.
//
//     Going through diff.Filter also preserves its cross-file basename
//     invalidation, so the poller's changed set matches what a full walk would
//     have produced.
//
//  3. SUBMISSION: on a non-empty changed set the poller calls the SAME
//     EventSink(repo, bulk) contract the fsnotify watcher satisfies. There is
//     no second reindex path, and the poller NEVER writes the manifest —
//     re-stamping is the indexer's job. (The #6932 spike learned this the hard
//     way: it called UpdateManifestScoped(repo, changed, nil, m), which wiped
//     the whole manifest via reconcileMembership, and its 14-case HIT/MISS
//     matrix was byte-identical before and after the fix.)
//
// LEVEL-TRIGGERED, NOT EDGE-TRIGGERED. The poller holds no "seen" set. Its
// state IS the manifest, and it re-submits every cycle for as long as the
// manifest and the disk disagree. Over-firing costs a debounced, circuit-broken
// scheduler enqueue; under-firing is the silent corruption this whole issue is
// about. The asymmetry is deliberate.
//
// That statement is only true if the changed set can actually reach empty, so
// "level-triggered" is a claim about CONVERGENCE and it is tested as one. The
// first version of this file did not converge on an uncommitted deletion or a
// staged rename: a path git reports, disk does not have, and the index pass
// prunes from the manifest read as new on every cycle forever, so the poller
// re-fired while manifest and disk AGREED. See existsOnDisk, and
// TestChangePoller_DeletionConverges / _StagedRenameConverges — both assert a
// bounded number of cycles, not first-cycle detection, because first-cycle
// detection is exactly what hid the bug.
//
// WARM-UP (arm D of #6932). A fresh worktree's first `git status` costs
// 2.4-9.0 s until git's untracked cache exists; every subsequent one is ~60 ms.
// AddRepo therefore sets core.untrackedCache=true and runs one throwaway
// `git status`, so the first real cycle is not mistaken for a hang.
//
// COST. 60 ms per cycle per worktree: at a 2 s interval that is ~3% of a core
// per worktree; at the 30 s default, ~0.2% (~2% across ten worktrees).
//
// MEASUREMENT CAVEAT, carried verbatim from #6932: every number above is
// macOS/APFS. Nothing was measured on Linux or in a container, and the two
// readdir-bound figures (the rejected full sweep, the rejected -uall floor)
// will be worse on overlayfs. Hybrid B's advantage should widen there because
// it replaces the walk entirely — but that is reasoning, not measurement.
package watch

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cajasmota/grafel/internal/daemon/walk"
	"github.com/cajasmota/grafel/internal/gitmeta"
	"github.com/cajasmota/grafel/internal/indexer/diff"
)

// DefaultChangePollInterval is the poll cadence used when none is configured.
//
// 30 s, from #6932's cost table. A cycle is ~60 ms per worktree, so 30 s is
// ~0.2% of a core per worktree (~2% for ten) against ~3% at 2 s. Immediacy buys
// nothing here: an agent already knows the file it just wrote, and the changes
// it CANNOT predict — checkout, merge, rebase — are covered by git hooks and by
// GitHeadPoller's own 2 s .git/HEAD poll, not by this. grafel also already
// ships a bounded-staleness contract (reloadBeforeCall, debounced at 2000 ms,
// documented as "bounded staleness <= window"), so seconds-scale detection is
// the existing shape, not a regression.
const DefaultChangePollInterval = 30 * time.Second

// defaultChangeBulkThreshold matches the fsnotify watcher's BulkThreshold: at
// or above this many changed files the sink is told to do a repo-level reindex
// rather than a file-level diff.
const defaultChangeBulkThreshold = 50

// ChangePollerConfig holds the poller's tunables. The zero value is valid
// except for StateDir, which has no sensible default inside this package.
type ChangePollerConfig struct {
	// Interval is the poll cadence. Zero selects DefaultChangePollInterval.
	Interval time.Duration

	// BulkThreshold is the changed-file count at or above which the sink is
	// called with bulk=true. Zero selects defaultChangeBulkThreshold.
	BulkThreshold int

	// StateDir maps an absolute repo path to the directory holding that repo's
	// file-index.json (i.e. daemon.StateDirForRepoRef's output). It is injected
	// rather than imported because internal/daemon imports this package, not
	// the other way round. A nil StateDir, or one returning "", disables
	// polling for that repo — there is no manifest to diff against.
	StateDir func(repoPath string) string

	// DisableWarmUp skips the arm-D untracked-cache warm-up at AddRepo time.
	// Production never sets it; it exists for tests that must not shell out.
	DisableWarmUp bool
}

// ChangePoller detects working-tree changes without any fs watch descriptors
// and submits them through the standard EventSink contract. See the package
// doc above for the mechanism and why each half of it is load-bearing.
type ChangePoller struct {
	interval      time.Duration
	bulkThreshold int
	stateDir      func(string) string
	disableWarmUp bool
	sink          EventSink
	logger        *slog.Logger

	mu    sync.Mutex
	repos map[string]struct{}

	cycles  uint64 // atomic — completed poll cycles
	submits uint64 // atomic — sink invocations

	stopOnce sync.Once
	stopCh   chan struct{}
	doneCh   chan struct{}
}

// NewChangePoller constructs a poller. sink must be non-nil; logger may be nil.
func NewChangePoller(cfg ChangePollerConfig, sink EventSink, logger *slog.Logger) *ChangePoller {
	if cfg.Interval <= 0 {
		cfg.Interval = DefaultChangePollInterval
	}
	if cfg.BulkThreshold <= 0 {
		cfg.BulkThreshold = defaultChangeBulkThreshold
	}
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(os.Stderr, nil)).With("pkg", "change-poller")
	}
	return &ChangePoller{
		interval:      cfg.Interval,
		bulkThreshold: cfg.BulkThreshold,
		stateDir:      cfg.StateDir,
		disableWarmUp: cfg.DisableWarmUp,
		sink:          sink,
		logger:        logger,
		repos:         make(map[string]struct{}),
		stopCh:        make(chan struct{}),
		doneCh:        make(chan struct{}),
	}
}

// Start begins the polling loop. Call at most once; Stop shuts it down.
func (p *ChangePoller) Start() { go p.loop() }

// Stop halts the poller and waits for the loop goroutine to exit. Safe to call
// repeatedly, and safe to call on a poller that was never Started (the loop
// goroutine closes doneCh; a never-started poller closes it here).
func (p *ChangePoller) Stop() {
	p.stopOnce.Do(func() { close(p.stopCh) })
	select {
	case <-p.doneCh:
	case <-time.After(2 * time.Second):
		// The loop was never started, or is mid-cycle. Either way we do not
		// hold the caller: a poller only reads.
	}
}

// AddRepo registers repoPath for polling and performs the arm-D warm-up.
// Idempotent.
func (p *ChangePoller) AddRepo(repoPath string) error {
	abs, err := filepath.Abs(repoPath)
	if err != nil {
		return err
	}
	p.mu.Lock()
	_, already := p.repos[abs]
	p.repos[abs] = struct{}{}
	p.mu.Unlock()
	if already {
		return nil
	}
	if !p.disableWarmUp {
		p.warmUp(abs)
	}
	p.logger.Info("change-poller: registered", "repo", abs, "interval", p.interval.String())
	return nil
}

// RemoveRepo deregisters repoPath. Safe on unknown paths.
func (p *ChangePoller) RemoveRepo(repoPath string) {
	abs, err := filepath.Abs(repoPath)
	if err != nil {
		return
	}
	p.mu.Lock()
	delete(p.repos, abs)
	p.mu.Unlock()
}

// Repos returns a snapshot of the polled repo paths.
func (p *ChangePoller) Repos() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]string, 0, len(p.repos))
	for r := range p.repos {
		out = append(out, r)
	}
	sort.Strings(out)
	return out
}

// Interval returns the poll cadence this poller ticks at.
func (p *ChangePoller) Interval() time.Duration { return p.interval }

// Cycles returns the number of COMPLETED poll cycles.
func (p *ChangePoller) Cycles() uint64 { return atomic.LoadUint64(&p.cycles) }

// Submits returns the number of index requests handed to the sink.
func (p *ChangePoller) Submits() uint64 { return atomic.LoadUint64(&p.submits) }

// warmUp is arm D of #6932: enable git's untracked cache for this worktree and
// pay its one-off build cost (2.4-9.0 s on a fresh worktree) here, at
// registration, rather than on the first poll cycle where it would look like a
// hang. Both calls are best-effort — a repo whose git refuses the config still
// polls correctly, just slower.
func (p *ChangePoller) warmUp(abs string) {
	t0 := time.Now()
	if _, ok := gitmeta.RunGitBoundedC(abs, "config", "core.untrackedCache", "true"); !ok {
		p.logger.Warn("change-poller: could not enable core.untrackedCache — first status per cycle will be slow", "repo", abs)
	}
	if _, ok := gitmeta.RunGitBoundedC(abs, "status", "--porcelain", "-z", "-unormal"); !ok {
		p.logger.Warn("change-poller: warm-up git status failed", "repo", abs)
	}
	p.logger.Info("change-poller: untracked-cache warm-up done", "repo", abs, "took", time.Since(t0).Truncate(time.Millisecond).String())
}

func (p *ChangePoller) loop() {
	defer close(p.doneCh)
	ticker := time.NewTicker(p.interval)
	defer ticker.Stop()
	for {
		select {
		case <-p.stopCh:
			return
		case <-ticker.C:
			p.PollOnce()
		}
	}
}

// PollOnce runs exactly one cycle over every registered repo, submits an index
// request for each repo with a non-empty changed set, and returns the changed
// sets keyed by repo path. Exported so tests drive cycles deterministically
// instead of racing a ticker.
func (p *ChangePoller) PollOnce() map[string][]string {
	defer atomic.AddUint64(&p.cycles, 1)

	p.mu.Lock()
	repos := make([]string, 0, len(p.repos))
	for r := range p.repos {
		repos = append(repos, r)
	}
	p.mu.Unlock()
	sort.Strings(repos)

	out := make(map[string][]string, len(repos))
	for _, abs := range repos {
		changed := p.pollRepo(abs)
		if len(changed) == 0 {
			continue
		}
		out[abs] = changed
		bulk := len(changed) >= p.bulkThreshold
		atomic.AddUint64(&p.submits, 1)
		p.logger.Info("change-poller: changes detected — enqueuing reindex",
			"repo", abs, "changed", len(changed), "bulk", bulk)
		p.sink(abs, bulk)
	}
	return out
}

// pollRepo computes the changed set for one repo. Both halves of hybrid B live
// here: git for DISCOVERY of paths the manifest has never heard of, and the
// manifest's own key set for the CHANGE DECISION.
func (p *ChangePoller) pollRepo(abs string) []string {
	if p.stateDir == nil {
		return nil
	}
	stateDir := p.stateDir(abs)
	if stateDir == "" {
		return nil
	}
	m := diff.LoadManifest(stateDir)
	if len(m.Files) == 0 {
		// No baseline: the repo has never been indexed (or its manifest is
		// unreadable). Every file would read as new and the poller would ask
		// for a reindex on every cycle forever. Initial indexing belongs to the
		// scheduler, not here.
		return nil
	}

	cand := make(map[string]struct{}, len(m.Files)+16)
	// Half 1 — the manifest's key set. This is what converges edit-then-revert
	// and any other change git no longer reports.
	for rel := range m.Files {
		cand[rel] = struct{}{}
	}

	// Half 2 — discovery. Paths git knows about that the manifest does not.
	//
	// A discovered path is taken as a candidate ONLY when it exists on disk.
	// Without that filter an uncommitted deletion — `rm foo.go`, or the origin
	// half of a staged `git mv`, which is what every refactor looks like for
	// minutes at a time — never converges: git reports it until the commit, it
	// is absent from disk, and the very index pass the poller asks for PRUNES it
	// from the manifest. diff.isChanged then calls it "new" forever, and poll
	// mode enqueues a full reindex every interval for a repo whose manifest and
	// disk already agree. That is the #5665/#5667 loop shape, entering through
	// the git half instead of the walk half.
	//
	// Dropping it costs nothing: a deletion of an INDEXED file is still caught,
	// because the path is a manifest key and half 1 sweeps those
	// unconditionally — it just stops being reported once the manifest no
	// longer claims it, which is exactly convergence.
	files, dirs, ok := gitStatusDiscovery(abs)
	if !ok {
		p.logger.Warn("change-poller: git status failed — sweeping the manifest key set only", "repo", abs)
	}
	for _, f := range files {
		if _, known := cand[f]; known || existsOnDisk(abs, f) {
			cand[f] = struct{}{}
		}
	}
	for _, d := range dirs {
		for _, rel := range walkUntrackedSubtree(abs, d) {
			cand[rel] = struct{}{}
		}
	}

	rels := make([]string, 0, len(cand))
	for rel := range cand {
		rels = append(rels, rel)
	}
	sort.Strings(rels)

	changed, _ := diff.Filter(abs, rels, m)
	sort.Strings(changed)
	return changed
}

// existsOnDisk reports whether rel names a REGULAR file inside repoRoot.
//
// Lstat, not Stat: a dangling symlink must read as absent, and walk.WalkRepo
// hands nothing but regular files to the indexer either, so a path that is not
// one can never acquire a manifest stamp and would be permanently dirty.
func existsOnDisk(repoRoot, rel string) bool {
	fi, err := os.Lstat(filepath.Join(repoRoot, filepath.FromSlash(rel)))
	return err == nil && fi.Mode().IsRegular()
}

// gitStatusDiscovery runs `git status --porcelain -z -unormal` in repoRoot and
// splits its report into individual FILE paths and collapsed untracked
// DIRECTORY paths (the ones ending in "/").
//
// -z is used deliberately: porcelain v1's default output quotes and
// C-escapes any path with a space or a non-ASCII byte, and a mis-unquoted path
// is a silently dropped file. With -z the paths are literal and
// NUL-terminated, and a rename/copy record simply carries a second
// NUL-terminated origin path which is consumed here as an extra candidate.
func gitStatusDiscovery(repoRoot string) (files, dirs []string, ok bool) {
	raw, ok := gitmeta.RunGitBoundedC(repoRoot, "status", "--porcelain", "-z", "-unormal")
	if !ok {
		return nil, nil, false
	}
	recs := bytes.Split(raw, []byte{0})
	for i := 0; i < len(recs); i++ {
		rec := recs[i]
		if len(rec) < 4 {
			continue // "XY " + at least one path byte
		}
		x := rec[0]
		path := string(rec[3:])
		// A rename/copy record is followed by its origin path as a bare,
		// status-less record. Consume it as a candidate too: the origin path
		// is now absent from disk and its manifest entry must be re-examined.
		if x == 'R' || x == 'C' {
			if i+1 < len(recs) && len(recs[i+1]) > 0 {
				files = append(files, string(recs[i+1]))
				i++
			}
		}
		if strings.HasSuffix(path, "/") {
			dirs = append(dirs, path)
			continue
		}
		files = append(files, path)
	}
	return files, dirs, true
}

// walkUntrackedSubtree expands one collapsed `dir/` line from git's untracked
// report into the repo-relative file paths grafel would index inside it, using
// the production walker rather than a bespoke one — so the poller's candidate
// set and the indexer's walked set agree on the skip layers.
//
// walk.Options is nil here on purpose. The only field the indexer sets is
// Sparse, and sparse-checkout filters TRACKED files against patterns expressed
// relative to the repo ROOT; this walk is rooted at a subtree, so those
// patterns would be matched against the wrong strings, and the subtree is
// untracked by construction so sparse-checkout has no bearing on it anyway.
//
// #6932 flagged "WalkRepo rooted below the repo root" as unverified. What it
// does is pinned by TestChangePoller_SubtreeWalkAppliesSkipLayers: the
// hardcoded skip list and any .gitignore INSIDE the subtree apply; the repo
// ROOT's .gitignore stack is not inherited. That direction is safe — git has
// already excluded root-ignored paths from the untracked report it hands us, so
// the worst case is over-inclusion of a path git chose to report.
func walkUntrackedSubtree(repoRoot, relDir string) []string {
	relDir = strings.TrimSuffix(relDir, "/")
	if relDir == "" || relDir == "." {
		return nil
	}
	sub := filepath.Join(repoRoot, filepath.FromSlash(relDir))
	found, _, err := walk.WalkRepo(sub, nil)
	if err != nil {
		return nil
	}
	out := make([]string, 0, len(found))
	for _, f := range found {
		out = append(out, relDir+"/"+f)
	}
	return out
}

// ChangePoller is what a Watcher in poll mode delegates its subscriptions to.
var _ SubscriptionDelegate = (*ChangePoller)(nil)
