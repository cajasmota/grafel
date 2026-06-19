package watch

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cajasmota/grafel/internal/daemon/walk"
	"github.com/fsnotify/fsnotify"
)

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
	mu        sync.Mutex
	fs        *fsnotify.Watcher
	repos     map[string]*repoState // key: absolute repo path
	dirToRepo map[string]string     // key: absolute dir path → repo path
	stopOnce  sync.Once
	stopCh    chan struct{}
	stoppedCh chan struct{}
	restartCh chan struct{} // signals heartbeat loop to recreate fsnotify
	// counters — accessed atomically outside mu where latency matters
	totalEvents   uint64
	droppedSkips  uint64
	droppedReplay uint64 // events lost during fsnotify restart
}

// repoState tracks per-repo bookkeeping.
type repoState struct {
	path string

	// debounce timer
	timer   *time.Timer
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

	w := &Watcher{
		logger:    logger,
		cfg:       cfg,
		sink:      sink,
		extraSkip: extraSkip,
		fs:        fw,
		repos:     map[string]*repoState{},
		dirToRepo: map[string]string{},
		stopCh:    make(chan struct{}),
		stoppedCh: make(chan struct{}),
		restartCh: make(chan struct{}, 1),
	}
	go w.loop()
	go w.heartbeat()
	return w, nil
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
func (w *Watcher) subscribeRepo(abs string) (int, error) {
	added := 0
	dirCap := walk.WatchDirCap()
	capWarned := false
	walkErr := filepath.WalkDir(abs, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !d.IsDir() {
			return nil
		}
		// TCC guard (#5296): never subscribe to protected macOS media folders
		// or media-library bundles — fsnotify-watching them trips the privacy
		// prompt. Checked even at the root as defence-in-depth.
		if protected, reason := walk.IsProtectedPath(p); protected {
			w.logger.Warn("watcher: skip protected", "path", p, "reason", reason)
			return filepath.SkipDir
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
			return filepath.SkipDir
		}
		if p != abs {
			base := filepath.Base(p)
			// Layer 1 + 2: hard-coded + per-instance excludes.
			if w.shouldSkipDir(base) {
				return filepath.SkipDir
			}
			// Layer 3: .gitignore + per-repo watch.json.
			relPath, relErr := filepath.Rel(abs, p)
			if relErr == nil {
				relPath = filepath.ToSlash(relPath)
				if skip, reason := ShouldSkipDirGitignore(abs, p, relPath); skip {
					w.logger.Info("watcher: skip", "path", p, "reason", reason)
					return filepath.SkipDir
				}
			}
		}
		if err := w.fs.Add(p); err != nil {
			w.logger.Warn("watcher: add failed", "path", p, "err", err)
			return nil
		}
		w.mu.Lock()
		w.dirToRepo[p] = abs
		w.mu.Unlock()
		added++
		return nil
	})
	return added, walkErr
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

// Stats returns coarse counters for /status output. Signature is
// intentionally identical to the pre-#1270 version so service.go
// doesn't need changes.
func (w *Watcher) Stats() (repos int, dirs int, events uint64, dropped uint64) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return len(w.repos), len(w.dirToRepo),
		atomic.LoadUint64(&w.totalEvents),
		atomic.LoadUint64(&w.droppedSkips) + atomic.LoadUint64(&w.droppedReplay)
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

// Stop halts the watcher and frees the fsnotify handles. Safe to call
// multiple times.
func (w *Watcher) Stop() {
	w.stopOnce.Do(func() {
		close(w.stopCh)
		_ = w.fs.Close()
		<-w.stoppedCh
		w.mu.Lock()
		for _, rs := range w.repos {
			if rs.timer != nil {
				rs.timer.Stop()
			}
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
			// loop goroutine detected unexpected channel closure.
			w.logger.Warn("watcher: fsnotify closed unexpectedly — restarting")
			atomic.AddUint64(&w.droppedReplay, 1)

			// Recreate fsnotify.
			fw, err := fsnotify.NewWatcher()
			if err != nil {
				w.logger.Error("watcher: restart failed", "err", err)
				continue
			}
			w.mu.Lock()
			w.fs = fw
			// Clear stale dirToRepo — will be repopulated by subscribeRepo.
			w.dirToRepo = make(map[string]string, len(w.dirToRepo))
			repos := make([]string, 0, len(w.repos))
			for p := range w.repos {
				repos = append(repos, p)
			}
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
	}
}

// loop drains the fsnotify channels until the watcher is closed. We
// route every event through the per-repo debounce timer; the timer's
// callback runs the sink on its own goroutine.
func (w *Watcher) loop() {
	for {
		select {
		case ev, ok := <-w.fs.Events:
			if !ok {
				// Channel closed — either Stop() was called or fsnotify
				// crashed. If it wasn't us, signal the heartbeat to restart.
				select {
				case <-w.stopCh:
					// intentional stop — close stoppedCh once
					select {
					case <-w.stoppedCh: // already closed
					default:
						close(w.stoppedCh)
					}
				default:
					// unexpected — ask heartbeat to restart
					select {
					case w.restartCh <- struct{}{}:
					default:
					}
				}
				return
			}
			w.handleEvent(ev)
		case err, ok := <-w.fs.Errors:
			if !ok {
				select {
				case <-w.stopCh:
					select {
					case <-w.stoppedCh:
					default:
						close(w.stoppedCh)
					}
				default:
					select {
					case w.restartCh <- struct{}{}:
					default:
					}
				}
				return
			}
			if err != nil {
				w.logger.Error("watcher: error", "err", err)
			}
		case <-w.stopCh:
			// Stop() was called while we were blocked waiting; drain
			// the channel close from fsnotify.Close() which happens next.
			select {
			case <-w.stoppedCh:
			default:
				close(w.stoppedCh)
			}
			return
		}
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
	if ShouldSkipPath(ev.Name) {
		atomic.AddUint64(&w.droppedSkips, 1)
		return
	}

	// Track newly-created directories so events under them surface.
	if ev.Op.Has(fsnotify.Create) {
		if fi, err := os.Stat(ev.Name); err == nil && fi.IsDir() {
			base := filepath.Base(ev.Name)
			if !w.shouldSkipDir(base) {
				w.subscribeDirRecursive(ev.Name)
			}
		}
	}

	repo := w.repoFor(ev.Name)
	if repo == "" {
		return
	}
	w.recordAndArm(repo)
}

// repoFor finds which registered repo a path belongs to. We walk up
// the path components and look it up in dirToRepo; if no parent dir is
// watched we treat the event as orphaned and drop it.
func (w *Watcher) repoFor(p string) string {
	w.mu.Lock()
	defer w.mu.Unlock()
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
func (w *Watcher) subscribeDirRecursive(root string) {
	repo := w.repoFor(filepath.Join(root, "_"))
	if repo == "" {
		return
	}
	_ = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !d.IsDir() {
			return nil
		}
		// TCC guard (#5296): never recurse into protected media folders/bundles.
		if protected, _ := walk.IsProtectedPath(p); protected {
			return filepath.SkipDir
		}
		base := filepath.Base(p)
		if p != root && w.shouldSkipDir(base) {
			return filepath.SkipDir
		}
		if err := w.fs.Add(p); err != nil {
			return nil
		}
		w.mu.Lock()
		w.dirToRepo[p] = repo
		w.mu.Unlock()
		return nil
	})
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

	now := time.Now()
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
	rs.timer = time.AfterFunc(debounce, func() {
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
