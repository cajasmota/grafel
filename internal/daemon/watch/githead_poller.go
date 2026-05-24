// Package watch — GitHeadPoller (PH1b of epic #2087 / issue #2089).
//
// Option B implementation: keep .git/ in SkipDirs (no fsnotify noise from
// git object/pack writes) and poll .git/HEAD content for each registered
// repo on a configurable interval (default 2 s). When the poller detects a
// branch switch (HEAD ref OR commit SHA changes) it emits a synthetic event
// via the BranchSwitchSink callback so the scheduler can trigger a new index
// pass targeted at the new ref.
//
// Design notes
//   - Polling is deliberately coarse (2 s) — sub-second precision is not
//     needed; the goal is "detect checkout within a few seconds" not
//     "detect checkout within ms".
//   - During COLD tier the poller is not paused (it is lightweight: a single
//     file read per repo every 2 s). Pausing is reserved for a future
//     tier-aware optimisation.
//   - The poller captures ref+SHA via gitmeta.Capture rather than reading
//     .git/HEAD directly. This gives us the symbolic ref name ("main",
//     "feat/x") and the commit SHA in one go, using the same code path that
//     the store layout uses — so there are no translation bugs between what
//     the poller observes and what StateDirForRepoRef produces.
//
// Thread safety: all mutable state is protected by GitHeadPoller.mu.
package watch

import (
	"log"
	"os"
	"sync"
	"time"

	"github.com/cajasmota/archigraph/internal/gitmeta"
)

// HeadSnapshot is the last-seen HEAD state for one repo.
type HeadSnapshot struct {
	Ref string // symbolic ref ("main", "feat/x"); "" for detached HEAD
	SHA string // abbreviated commit SHA (12 chars)
}

// BranchSwitchEvent is emitted by the poller when it detects a HEAD change.
type BranchSwitchEvent struct {
	RepoPath string
	OldRef   string
	OldSHA   string
	NewRef   string
	NewSHA   string
}

// BranchSwitchSink is the callback invoked for each detected branch switch.
type BranchSwitchSink func(ev BranchSwitchEvent)

// GitHeadPoller polls .git/HEAD (via gitmeta.Capture) for every registered
// repo and notifies the BranchSwitchSink when a change is detected.
type GitHeadPoller struct {
	interval time.Duration
	sink     BranchSwitchSink
	logger   *log.Logger

	mu       sync.Mutex
	heads    map[string]HeadSnapshot // key: absolute repo path
	stopOnce sync.Once
	stopCh   chan struct{}
	doneCh   chan struct{}
}

// defaultPollInterval is the default HEAD polling interval (Option B).
const defaultPollInterval = 2 * time.Second

// NewGitHeadPoller constructs a poller. interval=0 uses the default (2 s).
// sink must be non-nil. logger may be nil.
func NewGitHeadPoller(interval time.Duration, sink BranchSwitchSink, logger *log.Logger) *GitHeadPoller {
	if interval <= 0 {
		interval = defaultPollInterval
	}
	if logger == nil {
		logger = log.New(os.Stderr, "githead-poller: ", log.LstdFlags)
	}
	return &GitHeadPoller{
		interval: interval,
		sink:     sink,
		logger:   logger,
		heads:    make(map[string]HeadSnapshot),
		stopCh:   make(chan struct{}),
		doneCh:   make(chan struct{}),
	}
}

// Start begins the polling loop. Must be called once; call Stop to shut down.
func (p *GitHeadPoller) Start() {
	go p.loop()
}

// Stop halts the poller and waits for the goroutine to exit. Safe to call
// multiple times (subsequent calls are no-ops).
func (p *GitHeadPoller) Stop() {
	p.stopOnce.Do(func() {
		close(p.stopCh)
	})
	<-p.doneCh
}

// AddRepo registers a repo for HEAD polling. The initial HEAD state is captured
// immediately so the first poll cycle does not spuriously emit an event.
// Idempotent: re-adding a registered repo updates the baseline snapshot.
func (p *GitHeadPoller) AddRepo(repoPath string) {
	meta := gitmeta.Capture(repoPath)
	snap := HeadSnapshot{Ref: meta.Ref, SHA: meta.SHA}
	p.mu.Lock()
	p.heads[repoPath] = snap
	p.mu.Unlock()
}

// RemoveRepo deregisters a repo. Safe to call on unregistered paths.
func (p *GitHeadPoller) RemoveRepo(repoPath string) {
	p.mu.Lock()
	delete(p.heads, repoPath)
	p.mu.Unlock()
}

// Repos returns a snapshot of currently polled repo paths.
func (p *GitHeadPoller) Repos() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]string, 0, len(p.heads))
	for r := range p.heads {
		out = append(out, r)
	}
	return out
}

// loop runs the polling tick until stopCh is closed.
func (p *GitHeadPoller) loop() {
	defer close(p.doneCh)
	ticker := time.NewTicker(p.interval)
	defer ticker.Stop()
	for {
		select {
		case <-p.stopCh:
			return
		case <-ticker.C:
			p.poll()
		}
	}
}

// poll captures the current HEAD for every registered repo and calls the
// sink for any that changed.
func (p *GitHeadPoller) poll() {
	p.mu.Lock()
	repos := make([]string, 0, len(p.heads))
	for r := range p.heads {
		repos = append(repos, r)
	}
	p.mu.Unlock()

	for _, repoPath := range repos {
		meta := gitmeta.Capture(repoPath)
		current := HeadSnapshot{Ref: meta.Ref, SHA: meta.SHA}

		p.mu.Lock()
		prev, ok := p.heads[repoPath]
		if !ok {
			// repo was removed between the snapshot and now
			p.mu.Unlock()
			continue
		}
		changed := current.Ref != prev.Ref || current.SHA != prev.SHA
		if changed {
			p.heads[repoPath] = current
		}
		p.mu.Unlock()

		if changed {
			ev := BranchSwitchEvent{
				RepoPath: repoPath,
				OldRef:   prev.Ref,
				OldSHA:   prev.SHA,
				NewRef:   current.Ref,
				NewSHA:   current.SHA,
			}
			p.logger.Printf("branch-switch detected: %s %s@%s -> %s@%s",
				repoPath, ev.OldRef, ev.OldSHA, ev.NewRef, ev.NewSHA)
			p.sink(ev)
		}
	}
}
