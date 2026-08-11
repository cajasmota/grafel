package watch

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cajasmota/grafel/internal/daemon/walk"
	"github.com/fsnotify/fsnotify"
)

// fdPreChargedCap bounds Watcher.fdPreCharged. Its entries are paths charged
// from a directory listing that fsnotify may still report a Create for; most
// never will, so without a ceiling the set would grow with every new directory
// a repo ever gains. See subscribeDirRecursive.
const fdPreChargedCap = 8192

// EventSink is the per-repo callback invoked once a repo settles
// (i.e. no new fs events for the debounce window). When bulk is true
// the caller received 50+ events within a 1-second window and should
// perform a full repo reindex rather than a file-level diff.
type EventSink func(repoPath string, bulk bool)

// Config holds tunable parameters for the Watcher. Zero values fall
// back to the built-in defaults.
type Config struct {
	// Debounce is the quiet-period window after the last event before
	// the sink is called. Default: 5 s (was 2 s before #1270).
	Debounce time.Duration

	// BulkThreshold is the number of events per repo within BulkWindow
	// that switches the watcher from file-level to repo-level reindex
	// signalling. Default: 50.
	BulkThreshold int

	// BulkWindow is the measurement window for bulk detection.
	// Default: 1 s.
	BulkWindow time.Duration

	// HeartbeatInterval controls how often the watcher checks that
	// fsnotify is still running. If the internal channel closes
	// unexpectedly (OS-level inotify error, resource exhaustion, etc.)
	// the watcher restarts itself and emits a full diff scan for every
	// registered repo. Default: 30 s.
	HeartbeatInterval time.Duration

	// ExcludeDirs is a list of additional directory basenames (beyond
	// SkipDirs) that this watcher instance will not subscribe to.
	// Useful for per-group custom exclusions.
	ExcludeDirs []string

	// FDBudget caps the total number of OS file descriptors this watcher may
	// commit to fs watches, across every subscription (#6180). Zero uses the
	// process-wide default, which is derived from the effective RLIMIT_NOFILE
	// on macOS and disabled elsewhere; a negative value disables the budget
	// for this watcher. Tests inject a small value so the arithmetic is
	// deterministic and never depends on the host's real descriptor limit.
	FDBudget int

	// fdCost overrides the platform descriptor cost model. Unexported: it
	// exists so in-package tests can exercise the macOS (kqueue) arithmetic on
	// every platform. The zero value selects defaultCostModel().
	//
	// It is only legal together with a non-zero FDBudget. A cost model is a
	// property of the LEDGER, not of a Watcher (#6268 (D)): FDBudget == 0
	// selects the process-wide sharedFDBudget, and letting one Watcher charge
	// that shared ledger with kqueue arithmetic while another charges it with
	// inotify arithmetic makes `used` meaningless. NewWatcherConfig rejects the
	// combination instead of silently picking one.
	fdCost fdCostModel

	// testEvents / testErrors replace the backend's own channels as the ones
	// the loop goroutine drains. Unexported, and only useful together with
	// Watcher.closeBackend: a test standing in for a backend must OWN the
	// channels it sends on, because fsnotify's readEvents goroutine both sends
	// on and close()s its own (backend_kqueue.go:441-443), and a second sender
	// racing that close is a data race, not a simulation (#6287). Nil selects
	// the real backend's channels, which is what production always gets. Both
	// must be supplied together — see NewWatcherConfig.
	testEvents chan fsnotify.Event
	testErrors chan error

	// disableQuarantine builds the Watcher with no quarantine tracker at all.
	// It exists so a ledger test can remove the tracker's side effects — it
	// persists its decisions by creating <repo>/.grafel, inside the repo the
	// ledger is measuring (#6287) — at CONSTRUCTION, before the loop goroutine
	// starts. Assigning w.quarantine afterwards would be an unsynchronised
	// write to a field that goroutine reads on every event.
	disableQuarantine bool
}

func (c *Config) debounce() time.Duration {
	if c.Debounce > 0 {
		return c.Debounce
	}
	return 5 * time.Second
}

func (c *Config) bulkThreshold() int {
	if c.BulkThreshold > 0 {
		return c.BulkThreshold
	}
	return 50
}

func (c *Config) bulkWindow() time.Duration {
	if c.BulkWindow > 0 {
		return c.BulkWindow
	}
	return time.Second
}

func (c *Config) heartbeatInterval() time.Duration {
	if c.HeartbeatInterval > 0 {
		return c.HeartbeatInterval
	}
	return 30 * time.Second
}

// Watcher is a single fsnotify-backed instance that watches one or
// more registered repos. Each repo has its own debounce timer; when
// the timer fires, the EventSink is called with the repo path.
//
// Reliability improvements over the original design (#1270):
//   - Debounce window increased to 5 s (was 2 s) and is configurable.
//   - Bulk detection: 50+ events in 1 s → sink called with bulk=true so
//     the scheduler can short-circuit to a full repo reindex instead of
//     per-file diff, preventing re-index storms after git checkout.
//   - Heartbeat loop: if the fsnotify goroutine crashes silently (channel
//     closed without a Stop call) the watcher recreates the fsnotify
//     instance, re-subscribes all repos, and triggers a recovery scan.
//   - Dropped-event counter (separate from skip counter) tracks how many
//     events were lost while the watcher was restarting.
//   - Per-directory exclusion list (ExcludeDirs) for per-group tuning.
//   - ExtendedStats returns per-repo event rates and last-event timestamps
//     for the /diagnostics endpoint.
//
// The watcher is goroutine-safe: AddRepo, RemoveRepo, and Stop may be
// called concurrently. Internal state is guarded by a single mutex
// because the volume of mutations is low (handful of repos, lifecycle
// is registration/deregistration, not per-event).
type Watcher struct {
	logger    *slog.Logger
	cfg       Config
	sink      EventSink
	extraSkip map[string]struct{}
	// clk is the time seam for the debounce/bulk path. Defaults to the real
	// wall clock; tests inject a fake clock so coalesce/debounce outcomes are
	// deterministic instead of racing the CI scheduler. Production behaviour
	// is identical to using time.Now/time.AfterFunc directly.
	clk clock
	// quarantine is the adaptive trash detector (#5394). When non-nil, it
	// observes per-directory churn at the event boundary and drops events
	// under directories it has quarantined. nil disables the feature.
	quarantine *QuarantineTracker
	// fdb is the descriptor ledger (#6180). It is shared process-wide unless
	// Config.FDBudget overrides it, because the kernel's ceiling is per
	// process. The platform arithmetic lives on the ledger (fdb.model()), so
	// every consumer of a given ledger charges it identically (#6268 (D)).
	fdb *fdBudget
	mu  sync.Mutex
	fs  *fsnotify.Watcher
	// closeBackend closes the fsnotify instance. It is a field rather than a
	// direct w.fs.Close() call so a test can stand in for a backend that, like
	// fsnotify's Windows one, SENDS on Events/Errors from inside Close (#6287).
	// Defaults to w.fs.Close.
	closeBackend func() error
	// events/errs are the channels the loop goroutine drains. They are the
	// backend's own unless Config.testEvents overrides them, and they are
	// re-pointed together with w.fs when the heartbeat recreates the backend.
	events    <-chan fsnotify.Event
	errs      <-chan error
	repos     map[string]*repoState // key: absolute repo path
	dirToRepo map[string]string     // key: absolute dir path → repo path
	// fdReserved records how many descriptors each subscribed repo holds, so
	// RemoveRepo can hand them back.
	fdReserved map[string]int
	// fdUnwatched names repos refused because the budget was full. These are
	// NOT in repos: they receive no events, and this is how they say so.
	fdUnwatched map[string]struct{}
	// fdPreCharged holds entry paths that subscribeDirRecursive already charged
	// from its own listing and for which fsnotify may STILL deliver a Create.
	// See the note on subscribeDirRecursive; chargeEventOpen consumes it.
	fdPreCharged map[string]struct{}
	stopOnce     sync.Once
	stopCh       chan struct{}
	restartCh    chan struct{} // signals heartbeat loop to recreate fsnotify
	// loopWG counts LIVE loop goroutines, which is not always one: the
	// heartbeat can retire a loop and start another against a fresh backend.
	// Stop waits on this rather than on a one-shot "stopped" channel, because a
	// one-shot channel cannot express "the generation that exited was replaced"
	// — the loop that exits via the unexpected-close arm does not close it, so
	// a Stop landing during a restart waited out its whole timeout (#6287).
	// Every Add is performed under w.mu strictly before close(w.stopCh), which
	// is also taken under w.mu, so Add can never race the Wait.
	loopWG sync.WaitGroup
	// counters — accessed atomically outside mu where latency matters
	totalEvents   uint64
	droppedSkips  uint64
	droppedReplay uint64 // events lost during fsnotify restart
}

// repoState tracks per-repo bookkeeping.
type repoState struct {
	path string

	// debounce timer
	timer   timer
	pending bool

	// bulk detection — count events in the current bulkWindow
	bulkCount     int
	bulkWindowEnd time.Time
	bulkTriggered bool // true once we emitted a bulk=true signal this burst

	// stats
	lastEventAt time.Time
	totalEvents uint64
}

// NewWatcher constructs a Watcher with the given EventSink and Config.
// A zero-value Config is valid; defaults are applied for every zero field.
// logger may be nil.
//
// Deprecated convenience: callers that pass only a debounce duration should
// migrate to NewWatcherConfig. This overload is kept for back-compat with
// existing call sites in server.go.
func NewWatcher(debounce time.Duration, sink EventSink, logger *slog.Logger) (*Watcher, error) {
	return NewWatcherConfig(Config{Debounce: debounce}, sink, logger)
}

// NewWatcherConfig constructs a Watcher with the full Config surface.
func NewWatcherConfig(cfg Config, sink EventSink, logger *slog.Logger) (*Watcher, error) {
	if sink == nil {
		return nil, errors.New("watch: sink is required")
	}
	// Descriptor budget (#6180) + cost-model ownership (#6268 (D)). A zero
	// Config.FDBudget shares the process-wide ledger, whose arithmetic is fixed
	// by build tag; any explicit budget gets a private ledger that may carry an
	// injected model. Validated before fsnotify.NewWatcher so a rejected config
	// does not leak a watcher handle.
	if cfg.FDBudget == 0 && cfg.fdCost != (fdCostModel{}) {
		return nil, errors.New("watch: Config.fdCost requires a non-zero Config.FDBudget — " +
			"the cost model belongs to the ledger, and FDBudget 0 selects the process-wide one")
	}
	// Both or neither. One alone leaves the loop draining a real channel and a
	// nil one — the nil arm of a select never fires, so the errors (or events)
	// half of the loop would be silently dead for the Watcher's whole life.
	if (cfg.testEvents == nil) != (cfg.testErrors == nil) {
		return nil, errors.New("watch: Config.testEvents and Config.testErrors must be set together")
	}
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(os.Stderr, nil)).With("pkg", "watch")
	}
	fw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("fsnotify: %w", err)
	}

	extraSkip := make(map[string]struct{}, len(cfg.ExcludeDirs))
	for _, d := range cfg.ExcludeDirs {
		extraSkip[d] = struct{}{}
	}

	fdb := sharedFDBudget()
	if cfg.FDBudget != 0 {
		fdb = newFDBudgetCost(cfg.FDBudget, cfg.fdCost)
	}

	w := &Watcher{
		logger:       logger,
		cfg:          cfg,
		sink:         sink,
		extraSkip:    extraSkip,
		clk:          realClock{},
		fdb:          fdb,
		fs:           fw,
		repos:        map[string]*repoState{},
		dirToRepo:    map[string]string{},
		fdReserved:   map[string]int{},
		fdUnwatched:  map[string]struct{}{},
		fdPreCharged: map[string]struct{}{},
		stopCh:       make(chan struct{}),
		restartCh:    make(chan struct{}, 1),
	}
	// Read w.fs under the lock: the heartbeat can swap it for a fresh backend
	// concurrently, and closing the one this Watcher no longer owns would
	// orphan the live one — a descriptor leak on the shutdown path, in the
	// package whose whole job is not leaking descriptors (#6287).
	w.closeBackend = func() error {
		w.mu.Lock()
		fs := w.fs
		w.mu.Unlock()
		return fs.Close()
	}
	w.events, w.errs = fw.Events, fw.Errors
	if cfg.testEvents != nil {
		w.events, w.errs = cfg.testEvents, cfg.testErrors
	}
	// Adaptive index-trash quarantine (#5394). The tracker observes
	// per-directory churn at the event boundary and quarantines dirs that
	// thrash pathologically (build loops the static skip + gitignore missed),
	// then self-heals when they go quiet. Wired with the watcher's logger as
	// the audit sink. nil/disabled is a transparent no-op.
	logfn := func(event, repo, rel, detail string) {
		w.logger.Info("watcher: quarantine", "event", event, "repo", repo, "dir", rel, "detail", detail)
	}
	if !cfg.disableQuarantine {
		w.quarantine = NewQuarantineTracker(logfn)
	}

	w.loopWG.Add(1)
	go w.loop()
	go w.heartbeat()
	go w.quarantineSweep()
	return w, nil
}

// Quarantine returns the watcher's quarantine tracker (#5394) for the
// transparency surface / CLI (Q2). May be nil if the feature is disabled.
func (w *Watcher) Quarantine() *QuarantineTracker { return w.quarantine }

// quarantineSweep periodically re-evaluates quarantined directories and
// auto-un-quarantines any that have gone quiet (self-heal). The interval is a
// fraction of the heal window so recovery is responsive without busy-looping.
func (w *Watcher) quarantineSweep() {
	interval := quarantineSweepInterval()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-w.stopCh:
			return
		case <-ticker.C:
			if w.quarantine == nil {
				continue
			}
			if healed := w.quarantine.Sweep(); len(healed) > 0 {
				for repo, dirs := range healed {
					w.logger.Info("watcher: quarantine self-heal", "repo", repo, "dirs", dirs)
				}
			}
		}
	}
}

// shouldSkipDir extends the package-level ShouldSkipDir with instance
// extra excludes.
func (w *Watcher) shouldSkipDir(base string) bool {
	if ShouldSkipDir(base) {
		return true
	}
	_, ok := w.extraSkip[base]
	return ok
}

// AddRepo subscribes to every directory under repoPath that survives
// the skip list. Returns the number of directories added. Idempotent:
// re-adding a registered repo is a no-op.
func (w *Watcher) AddRepo(repoPath string) (int, error) {
	abs, err := filepath.Abs(repoPath)
	if err != nil {
		return 0, err
	}
	// TCC guard (#5296): refuse to register a repo whose root resolves into a
	// protected macOS media folder (~/Music, ~/Photos, ...) or is a media
	// library bundle. Subscribing fsnotify there would walk the tree and trip
	// a macOS privacy prompt for Music/Photos access.
	if protected, reason := walk.IsProtectedPath(abs); protected {
		w.logger.Warn("watcher: refusing protected path", "repo", abs, "reason", reason)
		return 0, fmt.Errorf("watch: refusing to register protected path %s (%s)", abs, reason)
	}
	w.mu.Lock()
	if _, ok := w.repos[abs]; ok {
		w.mu.Unlock()
		return 0, nil
	}
	w.repos[abs] = &repoState{path: abs}
	w.mu.Unlock()

	added, err := w.subscribeRepo(abs)
	if err != nil {
		if isFDBudgetError(err) {
			// The subscription was refused and fully unwound. Drop the repo
			// from the watched set as well — leaving it there would make it
			// LOOK watched in Repos()/Stats() while receiving no events, which
			// is precisely the silent half-failure this guard exists to stop.
			// subscribeRepo has already recorded it in fdUnwatched.
			w.mu.Lock()
			if rs, ok := w.repos[abs]; ok {
				if rs.timer != nil {
					rs.timer.Stop()
				}
				delete(w.repos, abs)
			}
			w.mu.Unlock()
			return 0, err
		}
		return added, err
	}
	w.logger.Info("watcher: registered", "repo", abs, "dirs", added, "debounce", w.cfg.debounce())
	return added, nil
}

// subscribeRepo walks the repo tree and adds every non-skipped dir to
// the fsnotify instance. Separated from AddRepo so the restart path
// can call it without the idempotency guard.
//
// Three-layer skip check (S4 #2154):
//  1. Hard-coded SkipDirs / walk.IsHardcodedSkip (ShouldSkipDir)
//  2. Per-instance ExcludeDirs (extraSkip)
//  3. .gitignore + .grafel/watch.json (ShouldSkipDirGitignore)
//
// Descriptor budget (#6180): the walk is also the estimator. Each directory is
// charged, at the moment it is subscribed, for what that one w.fs.Add opens —
// the directory itself plus its file entries (chargeDir). Charging as the walk
// reaches each directory, rather than from a pre-flight estimate, means the
// budget can never be overshot by more than one directory's worth before it
// notices.
//
// Pruned directories still cost (#6268 (C)). fsnotify v1.10.1
// backend_kqueue.go:582-616 (watchDirectoryFiles) opens one descriptor for
// EVERY entry of a directory grafel Add()ed — subdirectories included, via
// internalWatch at :672-681. That happens when the PARENT is Add()ed, i.e.
// before this walk reaches the child and decides to prune it. Pruning saves a
// directory's entries; it never saves the directory's own descriptor. The
// entries are genuinely free, because watchDirectoryFiles does not recurse:
// internalWatch passes info.dirFlags|NOTE_DELETE|NOTE_RENAME for a subdirectory
// (:677) and the watchDir predicate at :419 requires NOTE_WRITE, which only
// Add()/AddWith supplies (noteAllEvents, :345).
func (w *Watcher) subscribeRepo(abs string) (int, error) {
	added := 0
	dirCap := walk.WatchDirCap()
	capWarned := false
	cost := w.fdb.model()

	reserved := 0
	budgetHit := false
	// Dirs this walk actually subscribed. A file is only charged if the
	// directory holding it is one we opened — files under a skipped or
	// failed-to-add directory cost fsnotify nothing.
	subscribed := map[string]struct{}{}

	// prune charges the descriptor fsnotify already opened for a directory we
	// are about to skip, then returns filepath.SkipDir. Nothing is charged for
	// a directory whose parent we did not subscribe: watchDirectoryFiles only
	// ran for parents grafel Add()ed, so no descriptor exists for the child.
	prune := func(p string) error {
		if cost.perEntry() <= 0 {
			return filepath.SkipDir
		}
		if _, parentSubscribed := subscribed[filepath.Dir(p)]; !parentSubscribed {
			return filepath.SkipDir
		}
		if !w.fdb.reserve(cost.perEntry()) {
			budgetHit = true
			return errFDBudget
		}
		reserved += cost.perEntry()
		return filepath.SkipDir
	}

	walkErr := filepath.WalkDir(abs, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !d.IsDir() {
			// Files are charged with their directory, from the listing taken
			// just before w.fs.Add — see chargeDir.
			return nil
		}
		// TCC guard (#5296): never subscribe to protected macOS media folders
		// or media-library bundles — fsnotify-watching them trips the privacy
		// prompt. Checked even at the root as defence-in-depth.
		if protected, reason := walk.IsProtectedPath(p); protected {
			w.logger.Warn("watcher: skip protected", "path", p, "reason", reason)
			return prune(p)
		}
		// Watch-dir cap (#5296): the live failure subscribed 875 dirs on a
		// non-code tree. Once we exceed the cap, WARN once and stop walking
		// further subtrees so we never register an unbounded watch set.
		if dirCap > 0 && added >= dirCap {
			if !capWarned {
				capWarned = true
				w.logger.Warn("watcher: watch-dir cap exceeded — may not be a real code repo; skipping remaining subtrees",
					"repo", abs, "cap", dirCap)
			}
			return prune(p)
		}
		if p != abs {
			base := filepath.Base(p)
			// Layer 1 + 2: hard-coded + per-instance excludes.
			if w.shouldSkipDir(base) {
				return prune(p)
			}
			// Layer 3: .gitignore + per-repo watch.json.
			relPath, relErr := filepath.Rel(abs, p)
			if relErr == nil {
				relPath = filepath.ToSlash(relPath)
				if skip, reason := ShouldSkipDirGitignore(abs, p, relPath); skip {
					w.logger.Info("watcher: skip", "path", p, "reason", reason)
					return prune(p)
				}
			}
		}
		n := chargeDir(p, cost)
		if !w.fdb.reserve(n) {
			budgetHit = true
			return errFDBudget
		}
		if err := w.fs.Add(p); err != nil {
			w.fdb.release(n)
			w.logger.Warn("watcher: add failed", "path", p, "err", err)
			return nil
		}
		reserved += n
		subscribed[p] = struct{}{}
		w.mu.Lock()
		w.dirToRepo[p] = abs
		w.mu.Unlock()
		added++
		return nil
	})

	if budgetHit {
		// Refuse, do not half-watch. Every descriptor this attempt opened is
		// handed back so a refusal cannot slowly poison the ledger.
		for p := range subscribed {
			_ = w.fs.Remove(p)
		}
		w.mu.Lock()
		for p := range subscribed {
			delete(w.dirToRepo, p)
		}
		// Hand back the event-time charges as well as the walk's own (#6268).
		// AddRepo drops w.mu before calling this, so the loop goroutine runs
		// concurrently with the walk: as soon as the first directory is
		// published into dirToRepo above, a Create under it reaches
		// chargeEventOpen and lands in fdReserved[abs]. Those descriptors are
		// closed by the fs.Remove unwind above — remove(name, true) also drops
		// every internal watch inside the directory
		// (backend_kqueue.go:325-333) — so releasing only `reserved` would
		// leave them charged forever on a refused subscription, which is
		// exactly the poisoning this branch exists to prevent.
		unwind := reserved + w.fdReserved[abs]
		delete(w.fdReserved, abs)
		w.fdUnwatched[abs] = struct{}{}
		w.mu.Unlock()
		w.fdb.release(unwind)

		used, limit := w.fdb.snapshot()
		w.logger.Warn("watcher: NOT WATCHING repo — file-descriptor budget exhausted; "+
			"edits under this repo will not trigger a re-index until budget frees up",
			"repo", abs,
			"dirs_reached", added,
			"fd_used", used,
			"fd_limit", limit,
			"override_env", fdBudgetEnv)
		return 0, fmt.Errorf("%w: %s does not fit in the remaining watch budget (used %d of %d descriptors); "+
			"raise %s or unregister repos", errFDBudget, abs, used, limit, fdBudgetEnv)
	}

	w.mu.Lock()
	// += and not =: chargeEventOpen may have attributed event-time descriptors
	// to this repo while the walk was still running (see the refusal branch
	// above for why the walk and the loop goroutine overlap). Assigning here
	// would discard them from the per-repo tally while leaving them on the
	// global ledger, so RemoveRepo and Stop would hand back less than the repo
	// holds — failure mode (B), on the AddRepo path.
	w.fdReserved[abs] += reserved
	delete(w.fdUnwatched, abs)
	w.mu.Unlock()
	return added, walkErr
}

// chargeDir returns the descriptor cost of taking an fsnotify watch on the
// directory dir: the directory's own descriptor plus one per NON-directory
// entry it holds.
//
// Subdirectory entries are deliberately excluded. Their descriptor is opened
// once — by whichever of the parent's watchDirectoryFiles or grafel's own
// Add() gets there first, since addWatch short-circuits on alreadyWatching
// (backend_kqueue.go:358) — and it is charged where that subdirectory is
// itself handled: as perDir when grafel subscribes it, or by prune when grafel
// skips it. Counting it here as well would charge one descriptor twice.
//
// WHEN the listing is taken relative to the caller's w.fs.Add matters, and the
// two callers choose differently on purpose. Add runs watchDirectoryFiles
// synchronously (backend_kqueue.go:430), and every entry it opens it also
// markSeen (:612), which stops sendCreateIfNew from ever reporting that entry
// as a Create (:657).
//
//   - Listing BEFORE the Add under-counts: a file that appears in between is
//     opened and silently absorbed by watchDirectoryFiles, and no later event
//     will charge it. subscribeRepo has no choice: it is the path that can
//     still REFUSE a subscription, and a refusal is only meaningful before the
//     descriptors exist — reserve must precede Add, so the count reserve needs
//     must precede it too. That it is also the path least likely to race a
//     writer is a consolation, not the reason; no test pins it, because
//     nothing in the suite races a writer against an initial subscribe.
//   - Listing AFTER the Add over-counts unless duplicates are suppressed: the
//     listing sees files that watchDirectoryFiles did not open, fsnotify opens
//     them later and DOES report Create for them, and both charges land.
//     subscribeDirRecursive takes this option together with the record
//     parameter, because it runs while a writer is by definition active.
func chargeDir(dir string, cost fdCostModel) int {
	return chargeDirRecording(dir, cost, nil)
}

// chargeDirRecording is chargeDir, additionally recording every entry path it
// charged into record (when record is non-nil) so a duplicate Create for the
// same path can be recognised. See subscribeDirRecursive.
func chargeDirRecording(dir string, cost fdCostModel, record map[string]struct{}) int {
	n := cost.perDir
	if cost.perEntry() <= 0 {
		return n
	}
	ents, err := os.ReadDir(dir)
	if err != nil {
		return n
	}
	for _, e := range ents {
		if e.IsDir() {
			continue
		}
		n += cost.perEntry()
		if record != nil {
			record[filepath.Join(dir, e.Name())] = struct{}{}
		}
	}
	return n
}

// RemoveRepo unsubscribes every directory associated with a repo. Any
// pending debounced event is cancelled.
func (w *Watcher) RemoveRepo(repoPath string) {
	abs, err := filepath.Abs(repoPath)
	if err != nil {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if rs, ok := w.repos[abs]; ok {
		if rs.timer != nil {
			rs.timer.Stop()
		}
		delete(w.repos, abs)
	}
	for d, owner := range w.dirToRepo {
		if owner == abs {
			_ = w.fs.Remove(d)
			delete(w.dirToRepo, d)
		}
	}
	// Hand the descriptors back (#6180). Without this a store that churns
	// repos would exhaust the budget permanently while holding nothing.
	if n := w.fdReserved[abs]; n > 0 {
		w.fdb.release(n)
	}
	delete(w.fdReserved, abs)
	delete(w.fdUnwatched, abs)
	// Evict gitignore cache so a re-add picks up any .gitignore changes.
	evictRepoIgnoreState(abs)
}

// Repos returns a snapshot of the currently watched repos.
func (w *Watcher) Repos() []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	out := make([]string, 0, len(w.repos))
	for p := range w.repos {
		out = append(out, p)
	}
	return out
}

// Stats returns coarse counters for /status output.
//
// unwatched (#6180) is the number of repos that are NOT being watched because
// the file-descriptor budget was full when they were offered. It is reported
// alongside the healthy counters on purpose: a repo that is registered with
// the daemon but receiving no fs events is the exact failure this codebase
// keeps shipping silently, and /status is where an operator looks first.
func (w *Watcher) Stats() (repos int, dirs int, events uint64, dropped uint64, unwatched int) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return len(w.repos), len(w.dirToRepo),
		atomic.LoadUint64(&w.totalEvents),
		atomic.LoadUint64(&w.droppedSkips) + atomic.LoadUint64(&w.droppedReplay),
		len(w.fdUnwatched)
}

// FDBudgetStats reports the descriptor ledger for /diagnostics (#6180): how
// many descriptors the watch set has committed, the ceiling (0 == accounting
// disabled on this platform), and the repos refused for want of budget.
func (w *Watcher) FDBudgetStats() (used, limit, unwatched int, unwatchedRepos []string) {
	used, limit = w.fdb.snapshot()
	w.mu.Lock()
	defer w.mu.Unlock()
	for p := range w.fdUnwatched {
		unwatchedRepos = append(unwatchedRepos, p)
	}
	sort.Strings(unwatchedRepos)
	return used, limit, len(w.fdUnwatched), unwatchedRepos
}

// RepoStat holds per-repo watcher statistics for the /diagnostics endpoint.
type RepoStat struct {
	Path        string    `json:"path"`
	TotalEvents uint64    `json:"total_events"`
	LastEventAt time.Time `json:"last_event_at,omitempty"`
}

// ExtendedStats returns per-repo event rates plus overall counters.
// Used by the /diagnostics handler added in #1270.
func (w *Watcher) ExtendedStats() (repoStats []RepoStat, totalEvents, droppedSkips, droppedReplay uint64) {
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, rs := range w.repos {
		repoStats = append(repoStats, RepoStat{
			Path:        rs.path,
			TotalEvents: rs.totalEvents,
			LastEventAt: rs.lastEventAt,
		})
	}
	return repoStats,
		atomic.LoadUint64(&w.totalEvents),
		atomic.LoadUint64(&w.droppedSkips),
		atomic.LoadUint64(&w.droppedReplay)
}

// ForceRescan triggers the sink for every registered repo with bulk=true
// to request a full diff reconciliation. Called by the heartbeat loop
// after a crash-and-restart and exposed for the "force re-scan" button
// on /diagnostics.
func (w *Watcher) ForceRescan() {
	w.mu.Lock()
	paths := make([]string, 0, len(w.repos))
	for p := range w.repos {
		paths = append(paths, p)
	}
	w.mu.Unlock()
	for _, p := range paths {
		w.logger.Info("watcher: force-rescan", "repo", p)
		w.sink(p, true)
	}
}

// RescanRepo asks the sink to reconcile a single repo with bulk=true, the same
// request ForceRescan makes for every registered repo. It exists for the
// catch-up case (#6269): a repo that was NOT subscribed for a while received no
// events for the edits made during that window, so re-subscribing alone leaves
// the graph stale.
//
// The sink call runs on its own goroutine because RescanRepo's caller is
// DefaultManager.Resume, which the tier cold-wake path invokes inline on the
// MCP request goroutine (Cache.GetForRepoRef → fireAccessHook →
// tier.Manager.Touch → Manager.Resume are all direct calls, none of them
// deferred to a goroutine and none of them carrying a timeout). Waiting here
// would put a user's query behind whatever the sink does.
//
// repoPath is absolutised the way AddRepo absolutises it, so the sink is handed
// the same key the watcher registered.
func (w *Watcher) RescanRepo(repoPath string) {
	abs, err := filepath.Abs(repoPath)
	if err != nil {
		return
	}
	w.logger.Info("watcher: catch-up rescan after re-subscribe", "repo", abs)
	go w.sink(abs, true)
}

// watcherStopTimeout bounds each of the two waits in Stop. A var, not a const,
// so the test that pins the bound can shorten it.
//
// Nothing in a healthy shutdown takes anywhere near this long: it exists only
// so a wedged backend degrades into a loud, bounded failure instead of an
// unbounded hang that takes a whole test binary — or a daemon shutdown — with
// it (#6287).
var watcherStopTimeout = 10 * time.Second

// stopping reports whether Stop has been requested. The loop goroutine uses it
// to switch to drain-only mode: see Stop.
func (w *Watcher) stopping() bool {
	select {
	case <-w.stopCh:
		return true
	default:
		return false
	}
}

// Stop halts the watcher and frees the fsnotify handles. Safe to call
// multiple times.
//
// The ORDER here is load-bearing, and getting it wrong deadlocked the whole
// package on Windows (#6287). close(w.stopCh) only marks the shutdown as
// intentional — it must NOT stop the loop goroutine draining w.fs.Events and
// w.fs.Errors, because fsnotify's Windows backend finishes Close() by walking
// its watch set through deleteWatch and startRead, and BOTH of those SEND on
// those channels (backend_windows.go:453/457 and :465/475) before Close's
// acknowledgement is delivered. With the drain already gone, that send blocks
// forever, Close() never returns, and Stop() never returns either. The
// observed goroutine dump was exactly that: the test goroutine parked in
// readDirChangesW.Close and the I/O thread parked in sendError, with no loop
// goroutine left in the process.
//
// So: request the stop, close the backend WHILE the loop is still draining,
// and only then wait for the loop to observe the channel close. Both waits are
// bounded — a hang here blanks every remaining test in the package, which is a
// strictly worse failure than a loud one.
//
// close(w.stopCh) is taken under w.mu, and the heartbeat's restart branch
// checks stopping() and does its loopWG.Add under the same lock. That pairing
// is what makes the two shutdown properties hold at once: a restart either
// completes before the stop is published — in which case closeBackend reads the
// NEW backend and its loop generation is counted — or it is abandoned. Without
// it, a Stop landing while the heartbeat was mid-restart waited out its whole
// timeout, because the loop that exits via the unexpected-close arm signals
// nothing to Stop.
func (w *Watcher) Stop() {
	w.stopOnce.Do(func() {
		w.mu.Lock()
		close(w.stopCh)
		w.mu.Unlock()

		closed := make(chan struct{})
		go func() {
			defer close(closed)
			_ = w.closeBackend()
		}()
		select {
		case <-closed:
		case <-time.After(watcherStopTimeout):
			w.logger.Error("watcher: fsnotify Close did not return; abandoning the backend",
				"timeout", watcherStopTimeout)
		}
		drained := make(chan struct{})
		go func() {
			defer close(drained)
			w.loopWG.Wait()
		}()
		select {
		case <-drained:
		case <-time.After(watcherStopTimeout):
			w.logger.Error("watcher: event loop did not finish draining after Close; abandoning it",
				"timeout", watcherStopTimeout)
		}

		w.mu.Lock()
		for _, rs := range w.repos {
			if rs.timer != nil {
				rs.timer.Stop()
			}
		}
		// fs.Close() released every descriptor this watcher held; give the
		// budget back so a later Watcher in the same process can use it
		// (#6180 — the ledger is process-wide, not per-Watcher).
		for repo, n := range w.fdReserved {
			w.fdb.release(n)
			delete(w.fdReserved, repo)
		}
		w.mu.Unlock()
	})
}

// heartbeat monitors the loop goroutine. If it detects the fsnotify
// channel was closed without a Stop (i.e. an OS-level failure), it
// recreates the fsnotify instance, re-subscribes all repos, and triggers
// a full recovery scan via ForceRescan.
func (w *Watcher) heartbeat() {
	ticker := time.NewTicker(w.cfg.heartbeatInterval())
	defer ticker.Stop()
	for {
		select {
		case <-w.stopCh:
			return
		case <-ticker.C:
			// still healthy — nothing to do
		case <-w.restartCh:
			if !w.restartBackend() {
				return
			}
		}
	}
}

// restartBackend replaces a backend that closed underneath the loop goroutine
// and starts a fresh loop generation against it. It reports whether the
// heartbeat should keep running: false means a stop was published while the
// replacement was being prepared, and the heartbeat is done.
//
// Split out of heartbeat so the stop check below is reachable from a test
// without racing a live heartbeat against a live Stop (#6287). The window it
// guards is only a few instructions wide, which is precisely why it cannot be
// pinned by interleaving alone.
func (w *Watcher) restartBackend() bool {
	// loop goroutine detected unexpected channel closure.
	w.logger.Warn("watcher: fsnotify closed unexpectedly — restarting")
	atomic.AddUint64(&w.droppedReplay, 1)

	// Recreate fsnotify.
	fw, err := fsnotify.NewWatcher()
	if err != nil {
		w.logger.Error("watcher: restart failed", "err", err)
		return true
	}
	w.mu.Lock()
	// A stop published while this restart was being prepared wins: a new
	// backend installed now can be one that Stop's closeBackend has already
	// read PAST, and then nothing ever closes the channels its loop generation
	// drains — an orphaned fsnotify backend plus a loop goroutine with no exit,
	// which is a descriptor leak on the shutdown path of the package whose job
	// is not leaking descriptors (#6287). Checked under the same lock Stop
	// closes stopCh under, so only two orderings exist: the replacement is
	// fully installed before the stop is published, or it is abandoned here.
	if w.stopping() {
		w.mu.Unlock()
		_ = fw.Close()
		w.logger.Info("watcher: restart abandoned — watcher is stopping")
		return false
	}
	{
		w.fs = fw
		// The loop goroutine started below drains the NEW backend's
		// channels; the old ones are closed and would return !ok forever.
		w.events, w.errs = fw.Events, fw.Errors
		// Clear stale dirToRepo — will be repopulated by subscribeRepo.
		w.dirToRepo = make(map[string]string, len(w.dirToRepo))
		// The old fsnotify instance is gone, so its descriptors are gone
		// too (#6180). Return them to the ledger before re-subscribing, or
		// the restart double-charges and every repo is refused.
		for repo, n := range w.fdReserved {
			w.fdb.release(n)
			delete(w.fdReserved, repo)
		}
		// Those descriptors are gone too, so the pending pre-charge
		// markers that stood for them are stale; a Create seen after the
		// restart is a fresh open and must be charged (#6268).
		w.fdPreCharged = map[string]struct{}{}
		repos := make([]string, 0, len(w.repos))
		for p := range w.repos {
			repos = append(repos, p)
		}
		// Counted here, under the lock, and not next to the `go` below:
		// Stop's loopWG.Wait may start the moment the lock is released, and
		// a WaitGroup.Add that races a Wait is a documented misuse.
		w.loopWG.Add(1)
		w.mu.Unlock()

		// Re-subscribe.
		for _, abs := range repos {
			if n, err := w.subscribeRepo(abs); err != nil {
				w.logger.Error("watcher: restart re-subscribe failed", "repo", abs, "err", err)
			} else {
				w.logger.Info("watcher: restart re-subscribed", "repo", abs, "dirs", n)
			}
		}

		// Restart the loop goroutine against the new fsnotify instance.
		go w.loop()

		// Trigger full diff reconciliation for every repo.
		w.ForceRescan()
	}
	return true
}

// loop drains the fsnotify channels until the watcher is closed. We
// route every event through the per-repo debounce timer; the timer's
// callback runs the sink on its own goroutine.
//
// The ONLY exit is the backend closing one of its channels (#6287). There is
// deliberately no `case <-w.stopCh: return` arm: returning on the stop signal
// would kill the drain while fsnotify's Close is still pushing its teardown
// events and errors through it, which is a deadlock on the Windows backend —
// see Stop. Once a stop has been requested the loop keeps draining but stops
// ACTING on what it drains: handleEvent can call back into w.fs (Add, via
// subscribeDirRecursive) and can arm a reindex, neither of which is wanted
// while the backend is being torn down underneath it.
//
// The guard NARROWS that window, it does not close it: a handleEvent already
// in flight when close(w.stopCh) runs is unaffected and may still be inside
// w.fs.Add when Close begins. That call is safe rather than merely unlikely —
// every backend checks isClosed() and returns ErrClosed — so the residue is a
// wasted Add, not a hang. Closing the window entirely would mean holding a
// lock across handleEvent, which would serialise the event path against every
// AddRepo; that trade has not been made.
func (w *Watcher) loop() {
	defer w.loopWG.Done()
	// Captured once. A backend restart starts a FRESH loop goroutine against
	// the channels the heartbeat has already re-pointed, so re-reading the
	// fields per iteration would buy nothing and would race the swap.
	w.mu.Lock()
	events, errs := w.events, w.errs
	w.mu.Unlock()
	for {
		select {
		case ev, ok := <-events:
			if !ok {
				w.backendClosed()
				return
			}
			if w.stopping() {
				continue // drain only — see the note above
			}
			w.handleEvent(ev)
		case err, ok := <-errs:
			if !ok {
				w.backendClosed()
				return
			}
			if err != nil && !w.stopping() {
				w.logger.Error("watcher: error", "err", err)
			}
		}
	}
}

// backendClosed handles the loop's only exit: the backend closed a channel. If
// grafel asked for that, the loop's deferred loopWG.Done is all Stop is waiting
// for. If it did NOT, the backend failed underneath us and the heartbeat is
// asked to build a new one — and the retiring generation's Done is what lets a
// concurrent Stop proceed without waiting out its timeout, whether or not a
// replacement generation is ever started (#6287).
func (w *Watcher) backendClosed() {
	if w.stopping() {
		return
	}
	select {
	case w.restartCh <- struct{}{}:
	default:
	}
}

// handleEvent classifies an fsnotify event. We do not act on Chmod-only
// events (they happen during indexing and would self-trigger). New
// directories are watched recursively so freshly-checked-out commits
// pick up nested subtrees without an explicit re-register.
func (w *Watcher) handleEvent(ev fsnotify.Event) {
	atomic.AddUint64(&w.totalEvents, 1)

	if ev.Op == fsnotify.Chmod {
		return
	}

	// -----------------------------------------------------------------------
	// Descriptor accounting (#6268 (A) and (B)) runs BEFORE every filter below,
	// because the filters decide whether to REINDEX, not whether the kernel
	// opened or closed a descriptor. fsnotify v1.10.1's kqueue backend has
	// already done both by the time the event arrives:
	//
	//   Create — readEvents -> dirChange -> sendCreateIfNew -> internalWatch
	//     (backend_kqueue.go:505-506, 622-670) opens one descriptor for the new
	//     entry of a watched directory, file or subdirectory alike, and only
	//     emits Create when the path was not seenBefore (:657).
	//   Remove/Rename — readEvents calls w.remove, which closes the descriptor
	//     (:500-503, :320) and tells nobody. unwatchFiles is false there, so
	//     exactly one descriptor is closed even for a directory.
	//
	// The Create correspondence is one-to-one in the common case but NOT
	// guaranteed, and it can fail in the over-count direction:
	// sendCreateIfNew sends the event first (:658) and only then calls
	// internalWatch (:664). If internalWatch fails — addWatch's os.Lstat
	// returning ENOENT for a build temp file already unlinked, or EACCES
	// (:360-362) — no descriptor is opened, markSeen (:668) is never reached,
	// and grafel has still been told Create and charged one. Nothing releases
	// it, because the path is unwatched and will never produce a Remove; and
	// because markSeen was skipped, the same path recreated later charges
	// again. That is an unbounded upward drift on churny build output, and it
	// is a known residual of this change, not something it fixes.
	// watchDirectoryFiles tolerates exactly these errors when IT lists a
	// directory (:603-609); sendCreateIfNew has no such handling.
	//
	// In the other direction, readEvents discards dirChange's error (:506), so
	// one such failure abandons the rest of that listing and the remaining new
	// entries are neither reported nor charged — an under-count.
	//
	// Charging after the filters would leave precisely the churn-heavy paths
	// (build output, *.log) unaccounted, which is the shape of the original
	// defect. createdDir is computed here and reused below so the Create path
	// stats the path once.
	// -----------------------------------------------------------------------
	createdDir := false
	if ev.Op.Has(fsnotify.Create) {
		if fi, err := os.Stat(ev.Name); err == nil && fi.IsDir() {
			createdDir = true
		}
		w.chargeEventOpen(ev.Name)
	}
	if ev.Op.Has(fsnotify.Remove) || ev.Op.Has(fsnotify.Rename) {
		w.releaseEventClose(ev.Name)
	}

	// Cheap static filter first (no I/O, no repo lookup): SkipDirs /
	// SkipExts / generated-file globs.
	if ShouldSkipPath(ev.Name) {
		atomic.AddUint64(&w.droppedSkips, 1)
		return
	}

	// Determine the owning repo. We need it both for the gitignore-aware
	// filter and to arm the debounce timer.
	repo := w.repoFor(ev.Name)
	if repo == "" {
		return
	}

	// Repo-aware filter (#5392): drop events under a gitignored path or a
	// per-repo excluded dir BEFORE arming a reindex. Build artifacts that
	// the repo gitignores (e.g. AAB/, app/build/, *.aab) churn heavily
	// during an Android/gradle build; honouring .gitignore here prevents
	// the reindex loop + heap thrash even when the artifact dir doesn't
	// match a well-known static name.
	if ShouldSkipPathForRepo(repo, ev.Name) {
		atomic.AddUint64(&w.droppedSkips, 1)
		return
	}

	// Adaptive quarantine (#5394): the static + gitignore filters above catch
	// KNOWN trash; this catches the long tail. Observe per-directory churn and
	// drop the event if its directory is — or just became — quarantined for
	// pathological churn (a build loop the lists above didn't anticipate). A
	// normal human edit burst stays well under the threshold, so legitimate
	// source dirs are never quarantined.
	if w.quarantine.Observe(repo, ev.Name) {
		atomic.AddUint64(&w.droppedSkips, 1)
		return
	}

	// Track newly-created directories so events under them surface. The
	// directory's own descriptor was charged above; subscribeDirRecursive
	// charges only what its Add() additionally opens.
	if createdDir && !w.shouldSkipDir(filepath.Base(ev.Name)) {
		w.subscribeDirRecursive(ev.Name)
	}

	w.recordAndArm(repo)
}

// chargeEventOpen records the descriptor fsnotify opened for a path it has just
// reported Create for. The open has already happened inside fsnotify
// (sendCreateIfNew -> internalWatch), so this cannot be a reserve: there is
// nothing left to refuse. It is charged unconditionally, which lets the ledger
// go over limit — that overdraft is the true state of the process and is what
// makes the NEXT subscription refuse instead of walking on toward EMFILE.
//
// The charge is attributed to the owning repo so RemoveRepo hands it back. An
// event under no watched directory is not charged: fsnotify only opens
// descriptors for entries of directories grafel Add()ed, so a path with no
// owning repo has no descriptor of ours behind it.
func (w *Watcher) chargeEventOpen(path string) {
	n := w.fdb.model().perEntry()
	if n <= 0 {
		return
	}
	w.mu.Lock()
	if _, pre := w.fdPreCharged[path]; pre {
		// Already paid for by subscribeDirRecursive's listing. fsnotify still
		// reported Create because it opened the descriptor between the register
		// and the watchDirectoryFiles inside one Add — see the note there.
		delete(w.fdPreCharged, path)
		w.mu.Unlock()
		return
	}
	repo := w.repoForLocked(path)
	if repo == "" {
		w.mu.Unlock()
		return
	}
	w.fdReserved[repo] += n
	w.mu.Unlock()
	w.fdb.charge(n)
}

// releaseEventClose returns the descriptor fsnotify closed when it saw a
// Rename or Remove for path (backend_kqueue.go:500-503 -> remove -> :320).
// Without this the charge outlives the descriptor and a long-lived daemon in a
// churny repo overstates usage monotonically, refusing repos it could afford.
//
// A watched directory is also unmapped here: remove() closed its descriptor, so
// leaving it in dirToRepo would keep routing events for a path fsnotify no
// longer watches. The dirToRepo lookup doubles as the file/directory
// discriminator, since the path is typically gone by now and cannot be stat'd.
func (w *Watcher) releaseEventClose(path string) {
	cost := w.fdb.model()
	w.mu.Lock()
	// A pre-charge marker for this path is spent: fsnotify has closed the
	// descriptor it stood for, so a later re-creation must be charged afresh
	// rather than suppressed as a duplicate.
	delete(w.fdPreCharged, path)
	repo, isWatchedDir := w.dirToRepo[path]
	n := cost.perEntry()
	if isWatchedDir {
		n = cost.perDir
		delete(w.dirToRepo, path)
	}
	if n <= 0 {
		w.mu.Unlock()
		return
	}
	if !isWatchedDir {
		repo = w.repoForLocked(path)
	}
	if repo == "" {
		w.mu.Unlock()
		return
	}
	if w.fdReserved[repo] -= n; w.fdReserved[repo] < 0 {
		w.fdReserved[repo] = 0
	}
	w.mu.Unlock()
	w.fdb.release(n)
}

// repoFor finds which registered repo a path belongs to. We walk up
// the path components and look it up in dirToRepo; if no parent dir is
// watched we treat the event as orphaned and drop it.
func (w *Watcher) repoFor(p string) string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.repoForLocked(p)
}

// repoForLocked is repoFor for callers that already hold w.mu.
func (w *Watcher) repoForLocked(p string) string {
	dir := filepath.Dir(p)
	for {
		if repo, ok := w.dirToRepo[dir]; ok {
			return repo
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

// subscribeDirRecursive adds a newly-created directory (and its
// contents) to the fsnotify subscription. Used so a `git checkout`
// that creates new subtrees does not require a daemon restart.
//
// Descriptor accounting (#6268). Whatever handleEvent's chargeEventOpen already
// put on the ledger for root is deducted below: on kqueue that is the one
// descriptor sendCreateIfNew opened for it (backend_kqueue.go:656-670), and
// charging again would double-count it. grafel's Add() on root finds it alreadyWatching (:358) and opens
// nothing new — what the Add() does buy is watchDirectoryFiles on root's
// contents (:418-433, reached because Add supplies NOTE_WRITE while
// internalWatch did not), and those entries ARE charged below. Deeper
// subdirectories are charged perDir for the same reason the initial walk
// charges them: each is opened once, by whichever of the two paths reaches it
// first, and grafel Add()s each exactly once.
func (w *Watcher) subscribeDirRecursive(root string) {
	repo := w.repoFor(filepath.Join(root, "_"))
	if repo == "" {
		return
	}
	cost := w.fdb.model()
	reserved := 0
	budgetHit := false
	subscribed := map[string]struct{}{}

	// Entries charged from a listing here may ALSO arrive as Create events, and
	// would then be charged twice for one descriptor. fsnotify's Add registers
	// the directory's new flags — now including NOTE_WRITE — at
	// backend_kqueue.go:406 and only then runs watchDirectoryFiles at :430; in
	// between, its readEvents goroutine can already see a NOTE_WRITE for that
	// directory, run dirChange, and open + report an entry that this listing has
	// just counted. Recording those paths lets chargeEventOpen recognise and
	// drop the duplicate.
	//
	// The set is MERGED into the watcher's, not assigned over it. The loop
	// goroutine is FIFO across every repo, so a Create for an unrelated
	// directory can be queued ahead of the ones pending here and trigger a
	// second subscribeDirRecursive; replacing the set would discard the still
	// pending paths and let their Creates be charged a second time — the very
	// over-count this mechanism exists to stop. Entries drain in
	// chargeEventOpen when their Create arrives and in releaseEventClose when
	// the path is deleted, so a path that is removed and recreated is charged
	// again, correctly. Entries that drain by neither route — the common case,
	// files watchDirectoryFiles absorbed silently and will never report — are
	// bounded by fdPreChargedCap: past it the set is reset and the oldest
	// pending paths lose their protection, trading an unbounded map for a
	// bounded chance of one duplicate charge.
	//
	// handleEvent runs on the single loop goroutine, which is also the
	// goroutine inside this function, so a Create emitted during the Add below
	// is only processed after this returns — after the path has been recorded.
	preCharged := map[string]struct{}{}

	// prune charges the descriptor fsnotify already opened for a directory we
	// are about to skip — see the long note on subscribeRepo's prune.
	prune := func(p string) error {
		if cost.perEntry() <= 0 {
			return filepath.SkipDir
		}
		if _, parentSubscribed := subscribed[filepath.Dir(p)]; !parentSubscribed {
			return filepath.SkipDir
		}
		if !w.fdb.reserve(cost.perEntry()) {
			budgetHit = true
			return errFDBudget
		}
		reserved += cost.perEntry()
		return filepath.SkipDir
	}

	_ = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !d.IsDir() {
			// Charged with the directory, from the listing chargeDirRecording
			// takes just after w.fs.Add.
			return nil
		}
		// TCC guard (#5296): never recurse into protected media folders/bundles.
		if protected, _ := walk.IsProtectedPath(p); protected {
			return prune(p)
		}
		base := filepath.Base(p)
		if p != root && w.shouldSkipDir(base) {
			return prune(p)
		}
		if err := w.fs.Add(p); err != nil {
			return nil
		}
		// Listed AFTER the Add here, the opposite of subscribeRepo, and paired
		// with the preCharged set. Post-Add the listing is a superset of what
		// watchDirectoryFiles opened: it also holds entries that appeared just
		// after it, which fsnotify has not opened yet but will, reporting a
		// Create that preCharged then suppresses. Charging them now is early,
		// never double. Listing before the Add would instead miss exactly the
		// entries watchDirectoryFiles absorbed silently (markSeen at
		// backend_kqueue.go:612 stops sendCreateIfNew from ever reporting them,
		// :657), and nothing would come along later to charge them.
		charge := chargeDirRecording(p, cost, preCharged)
		if p == root {
			// Deduct exactly what handleEvent's chargeEventOpen already put on
			// the ledger for this path — perEntry(), because that is what it
			// charges — rather than perDir. The two are equal under the kqueue
			// model and are NOT equal under a per-watch model, where
			// chargeEventOpen charges nothing (a new directory costs that
			// backend no descriptor until grafel takes a watch on it) and
			// deducting perDir would make a newly created directory free.
			charge -= cost.perEntry()
		}
		if charge > 0 {
			// The Add above has already happened, so these descriptors exist
			// whether or not they fit; charge records them rather than
			// pretending otherwise. Walking stops here so the next directory
			// does not open more on top of an already-exhausted budget.
			if !w.fdb.reserve(charge) {
				w.fdb.charge(charge)
				budgetHit = true
				reserved += charge
				return errFDBudget
			}
			reserved += charge
		}
		subscribed[p] = struct{}{}
		w.mu.Lock()
		w.dirToRepo[p] = repo
		w.mu.Unlock()
		return nil
	})

	// Whatever we did manage to subscribe stays (the repo as a whole is
	// already watched; this is an incremental extension, so a partial
	// extension is strictly better than none). Charge it to the owning repo
	// so RemoveRepo returns it, and say loudly when we ran out.
	w.mu.Lock()
	w.fdReserved[repo] += reserved
	if len(w.fdPreCharged)+len(preCharged) > fdPreChargedCap {
		w.fdPreCharged = preCharged
	} else {
		for p := range preCharged {
			w.fdPreCharged[p] = struct{}{}
		}
	}
	w.mu.Unlock()
	if budgetHit {
		used, limit := w.fdb.snapshot()
		w.logger.Warn("watcher: new subtree only partially watched — file-descriptor budget exhausted",
			"repo", repo, "dir", root, "fd_used", used, "fd_limit", limit,
			"override_env", fdBudgetEnv)
	}
}

// recordAndArm updates per-repo event counters and (re)starts the
// debounce timer. If the event rate crosses the bulk threshold the
// sink is called immediately with bulk=true and the debounce timer is
// reset so subsequent events don't generate a second non-bulk call.
func (w *Watcher) recordAndArm(repo string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	rs, ok := w.repos[repo]
	if !ok {
		return
	}

	now := w.clk.Now()
	rs.totalEvents++
	rs.lastEventAt = now

	// Bulk detection: count events within BulkWindow.
	bulkWin := w.cfg.bulkWindow()
	if now.Before(rs.bulkWindowEnd) {
		rs.bulkCount++
	} else {
		// New window.
		rs.bulkCount = 1
		rs.bulkWindowEnd = now.Add(bulkWin)
		rs.bulkTriggered = false
	}

	if !rs.bulkTriggered && rs.bulkCount >= w.cfg.bulkThreshold() {
		rs.bulkTriggered = true
		// Cancel any in-flight debounce timer — bulk fires immediately.
		if rs.timer != nil {
			rs.timer.Stop()
			rs.timer = nil
		}
		rs.pending = false
		repoPath := repo
		w.logger.Info("watcher: bulk-detect", "repo", repoPath, "events_in_window", rs.bulkCount)
		go w.sink(repoPath, true)
		return
	}

	// Normal debounce path (non-bulk).
	debounce := w.cfg.debounce()
	if rs.timer != nil {
		rs.timer.Reset(debounce)
		rs.pending = true
		return
	}
	rs.pending = true
	repoPath := repo
	rs.timer = w.clk.AfterFunc(debounce, func() {
		w.mu.Lock()
		rs := w.repos[repoPath]
		if rs != nil {
			rs.pending = false
			rs.timer = nil
		}
		w.mu.Unlock()
		w.sink(repoPath, false)
	})
}
