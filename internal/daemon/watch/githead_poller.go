// Package watch — GitHeadPoller (PH1b of epic #2087 / issue #2089).
//
// Option B implementation: keep .git/ in SkipDirs (no fsnotify noise from
// git object/pack writes) and poll .git/HEAD content for each registered
// repo on a configurable interval (default 2 s). When the poller detects a
// branch switch (HEAD ref OR commit SHA changes) it runs a lightweight
// "git diff --name-only OLD NEW" and emits a BranchSwitchEvent only when
// at least one indexed-source file changed (S4 of #2149, fixes #2154).
//
// Reindex policy (set in BranchSwitchEvent.ReindexHint):
//
//	NoSourceChanges  → skip reindex entirely
//	SmallDiff        → incremental if S3 available, else full
//	LargeDiff        → full reindex
//	Unknown          → full reindex (diff failed or SHAs unavailable)
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
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/cajasmota/archigraph/internal/gitmeta"
)

// ReindexHint classifies how much source changed between two commits,
// guiding the scheduler's reindex strategy.
type ReindexHint int

const (
	// ReindexUnknown means the diff could not be computed (missing SHAs,
	// git error). The scheduler should do a full reindex to be safe.
	ReindexUnknown ReindexHint = iota
	// ReindexNone means git diff showed zero source-file changes. The
	// scheduler should skip reindexing entirely.
	ReindexNone
	// ReindexSmall means ≤20 indexed-source files changed. The scheduler
	// may use incremental reindex if S3 is available.
	ReindexSmall
	// ReindexFull means >20 indexed-source files changed. The scheduler
	// should do a full reindex.
	ReindexFull
)

// smallDiffThreshold is the maximum number of changed source files before
// we recommend a full reindex instead of an incremental one.
const smallDiffThreshold = 20

// HeadSnapshot is the last-seen HEAD state for one repo.
type HeadSnapshot struct {
	Ref string // symbolic ref ("main", "feat/x"); "" for detached HEAD
	SHA string // abbreviated commit SHA (12 chars)
}

// BranchSwitchEvent is emitted by the poller when it detects a HEAD change
// that involves at least one indexed-source file change (or when the diff
// cannot be computed). Events where only non-source files changed are
// suppressed (ReindexHint == ReindexNone is never emitted to the sink;
// those are logged and discarded instead).
type BranchSwitchEvent struct {
	RepoPath     string
	OldRef       string
	OldSHA       string
	NewRef       string
	NewSHA       string
	ReindexHint  ReindexHint // scheduling guidance for the caller
	ChangedFiles []string    // source files changed (capped at smallDiffThreshold+1)
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

// classifyRefChange runs "git diff --name-only OLD NEW" in repoPath and
// filters the result to indexed-source paths using ShouldSkipPath. It returns
// a ReindexHint and the list of changed source files (capped at
// smallDiffThreshold+1 to bound memory).
//
// If either SHA is empty or the git command fails, ReindexUnknown is returned.
func classifyRefChange(repoPath, oldSHA, newSHA string, logger *log.Logger) (ReindexHint, []string) {
	if oldSHA == "" || newSHA == "" {
		return ReindexUnknown, nil
	}
	// Same SHA (pure ref change, e.g. "git checkout -b newbranch"): the
	// working tree has not changed but the ref name has. Emit as Unknown so
	// the scheduler re-indexes under the new ref name.
	if oldSHA == newSHA {
		return ReindexUnknown, nil
	}

	// git diff --name-only <old>..<new>
	cmd := exec.Command("git", "diff", "--name-only", oldSHA+".."+newSHA)
	cmd.Dir = repoPath
	out, err := cmd.Output()
	if err != nil {
		logger.Printf("classifyRefChange: git diff failed in %s: %v", repoPath, err)
		return ReindexUnknown, nil
	}

	// Filter to indexed-source paths.
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	var sourcePaths []string
	for _, rel := range lines {
		rel = strings.TrimSpace(rel)
		if rel == "" {
			continue
		}
		// Build an absolute-style path for ShouldSkipPath (it checks basenames
		// against SkipDirs and extensions against SkipExts).
		abs := repoPath + "/" + rel
		if !ShouldSkipPath(abs) {
			sourcePaths = append(sourcePaths, rel)
			if len(sourcePaths) > smallDiffThreshold {
				break // cap — we only need to know "large"
			}
		}
	}

	if len(sourcePaths) == 0 {
		return ReindexNone, nil
	}
	if len(sourcePaths) <= smallDiffThreshold {
		return ReindexSmall, sourcePaths
	}
	return ReindexFull, sourcePaths
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

			// S4 git-diff validation: compute source-change set before
			// forwarding to the sink so we can suppress no-op reindexes.
			hint, changedFiles := classifyRefChange(repoPath, prev.SHA, current.SHA, p.logger)
			ev.ReindexHint = hint
			ev.ChangedFiles = changedFiles

			if hint == ReindexNone {
				// No indexed-source files changed — suppress reindex entirely.
				p.logger.Printf("ref-change: %s %s..%s no-source-changes — skipping reindex",
					repoPath, prev.SHA, current.SHA)
				continue
			}

			p.logger.Printf("branch-switch detected: %s %s@%s -> %s@%s hint=%d changed_files=%d",
				repoPath, ev.OldRef, ev.OldSHA, ev.NewRef, ev.NewSHA, hint, len(changedFiles))
			p.sink(ev)
		}
	}
}
