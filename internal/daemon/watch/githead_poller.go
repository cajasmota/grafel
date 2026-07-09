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
// Monorepo M1 (issue #2178)
//
// Repos that share a git common-dir (git worktrees, or sub-repos registered
// from a monorepo where all paths resolve to the same physical .git/) are
// deduplicated at the poll layer. A single pair of os.Stat calls is issued
// per common-dir per poll cycle instead of one per repo:
//
//   - Stat 1: .git/HEAD          — detects branch switches (checkout)
//   - Stat 2: .git/refs/heads/X  — detects new commits on the current branch
//
// When either file's mod-time changes, gitmeta.Capture is called once and
// the resulting HEAD snapshot is fanned out to all repos sharing that
// common-dir. Per-repo git-diff classification (S4) still runs independently
// for each repo path so that the source-change filter remains accurate.
//
// Thread safety: all mutable state is protected by GitHeadPoller.mu.
package watch

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cajasmota/grafel/internal/gitmeta"
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

// HeadSnapshot is the last-seen HEAD state for one common-dir group.
type HeadSnapshot struct {
	Ref        string // symbolic ref ("main", "feat/x"); "" for detached HEAD
	SHA        string // abbreviated commit SHA (12 chars)
	HeadMTime  int64  // mod-time of .git/HEAD (nanoseconds) — branch switch signal
	RefMTime   int64  // mod-time of .git/refs/heads/<branch> — commit signal
	CurrentRef string // the resolved branch name used to find the ref file
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

// commonDirGroup holds all state for repos that share one git common-dir.
// The group is the unit of polling: two os.Stat calls per cycle (HEAD + ref),
// not two per repo.
type commonDirGroup struct {
	// commonDir is the absolute path of the shared .git directory (output of
	// git rev-parse --git-common-dir, resolved to an absolute real path via
	// filepath.EvalSymlinks to handle OS-level symlinks such as /tmp → /private/tmp).
	commonDir string

	// headFile is the absolute path to the .git/HEAD file.
	headFile string

	// repos is the set of repo paths in this group (key = abs repo path).
	repos map[string]struct{}

	// snap is the last observed HEAD state.
	snap HeadSnapshot

	// representative is any one repo path used to run gitmeta.Capture when
	// a change is detected. Since all repos in the group share a common-dir
	// they all observe the same HEAD.
	representative string
}

// GitHeadPoller polls .git/HEAD (via gitmeta.Capture) for every registered
// common-dir group and notifies the BranchSwitchSink when a change is
// detected. Multiple repos sharing the same git common-dir are deduplicated
// into a single poll per cycle (Monorepo M1, issue #2178).
type GitHeadPoller struct {
	interval time.Duration
	sink     BranchSwitchSink
	logger   *slog.Logger

	mu          sync.Mutex
	groups      map[string]*commonDirGroup // key: absolute common-dir path
	repoToGroup map[string]string          // key: abs repo path → common-dir

	statCalls uint64 // atomic — total os.Stat calls on HEAD/ref files (for measurement)

	stopOnce sync.Once
	stopCh   chan struct{}
	doneCh   chan struct{}
}

// defaultPollInterval is the default HEAD polling interval (Option B).
const defaultPollInterval = 2 * time.Second

// NewGitHeadPoller constructs a poller. interval=0 uses the default (2 s).
// sink must be non-nil. logger may be nil.
func NewGitHeadPoller(interval time.Duration, sink BranchSwitchSink, logger *slog.Logger) *GitHeadPoller {
	if interval <= 0 {
		interval = defaultPollInterval
	}
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(os.Stderr, nil)).With("pkg", "githead-poller")
	}
	return &GitHeadPoller{
		interval:    interval,
		sink:        sink,
		logger:      logger,
		groups:      make(map[string]*commonDirGroup),
		repoToGroup: make(map[string]string),
		stopCh:      make(chan struct{}),
		doneCh:      make(chan struct{}),
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

// StatCalls returns the cumulative number of os.Stat calls issued against
// HEAD and ref files since the poller was created. Used for before/after
// measurement of the M1 dedup.
func (p *GitHeadPoller) StatCalls() uint64 {
	return atomic.LoadUint64(&p.statCalls)
}

// GroupCount returns the number of distinct common-dir groups currently
// registered. Used by tests to verify dedup is in effect.
func (p *GitHeadPoller) GroupCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.groups)
}

// resolveCommonDir resolves the git common-dir for repoPath to a canonical
// absolute path. filepath.EvalSymlinks is used to resolve OS-level symlinks
// (e.g. macOS /tmp → /private/tmp) so that the base repo and its linked
// worktrees map to the same key. Returns "" if repoPath is not a git repo.
func resolveCommonDir(repoPath string) string {
	return gitmeta.ResolveCommonDir(repoPath)
}

// refFileForBranch returns the path to the ref file for a given branch name
// inside the common-dir (e.g. .git/refs/heads/main). Returns "" if branch is
// empty (detached HEAD). The caller must handle the case where the file does
// not exist (packed-refs case; we fall back to treating mod-time as 0).
func refFileForBranch(commonDir, branch string) string {
	if branch == "" {
		return ""
	}
	return filepath.Join(commonDir, "refs", "heads", branch)
}

// captureInitialSnap captures the current HEAD state and mod-times for a
// repo, used to set the baseline snapshot at AddRepo time.
func captureInitialSnap(repoPath, commonDir string) HeadSnapshot {
	meta := gitmeta.Capture(repoPath)
	snap := HeadSnapshot{
		Ref:        meta.Ref,
		SHA:        meta.SHA,
		CurrentRef: meta.Ref,
	}
	headFile := filepath.Join(commonDir, "HEAD")
	if fi, err := os.Stat(headFile); err == nil {
		snap.HeadMTime = fi.ModTime().UnixNano()
	}
	refFile := refFileForBranch(commonDir, meta.Ref)
	if refFile != "" {
		if fi, err := os.Stat(refFile); err == nil {
			snap.RefMTime = fi.ModTime().UnixNano()
		}
	}
	return snap
}

// AddRepo registers a repo for HEAD polling. The initial HEAD state is captured
// immediately so the first poll cycle does not spuriously emit an event.
// Repos that share a git common-dir are automatically grouped so that only one
// pair of stat calls is issued per common-dir per poll cycle.
// Idempotent: re-adding a registered repo is a no-op.
func (p *GitHeadPoller) AddRepo(repoPath string) {
	commonDir := resolveCommonDir(repoPath)
	if commonDir == "" {
		// Not a git repo; register with repoPath as its own sentinel common-dir.
		commonDir = repoPath
	}

	snap := captureInitialSnap(repoPath, commonDir)

	p.mu.Lock()
	defer p.mu.Unlock()

	if _, already := p.repoToGroup[repoPath]; already {
		return // idempotent
	}

	grp, ok := p.groups[commonDir]
	if !ok {
		grp = &commonDirGroup{
			commonDir:      commonDir,
			headFile:       filepath.Join(commonDir, "HEAD"),
			repos:          make(map[string]struct{}),
			snap:           snap,
			representative: repoPath,
		}
		p.groups[commonDir] = grp
	}
	grp.repos[repoPath] = struct{}{}
	p.repoToGroup[repoPath] = commonDir
}

// RemoveRepo deregisters a repo. Safe to call on unregistered paths.
// If the repo was the last member of its common-dir group, the group is
// also removed.
func (p *GitHeadPoller) RemoveRepo(repoPath string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	commonDir, ok := p.repoToGroup[repoPath]
	if !ok {
		return
	}
	delete(p.repoToGroup, repoPath)

	grp := p.groups[commonDir]
	if grp == nil {
		return
	}
	delete(grp.repos, repoPath)

	// Update representative if we removed it.
	if grp.representative == repoPath {
		grp.representative = ""
		for r := range grp.repos {
			grp.representative = r
			break
		}
	}

	// Drop the group when it is empty.
	if len(grp.repos) == 0 {
		delete(p.groups, commonDir)
	}
}

// Repos returns a snapshot of currently polled repo paths.
func (p *GitHeadPoller) Repos() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]string, 0, len(p.repoToGroup))
	for r := range p.repoToGroup {
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
func classifyRefChange(repoPath, oldSHA, newSHA string, logger *slog.Logger) (ReindexHint, []string) {
	if oldSHA == "" || newSHA == "" {
		return ReindexUnknown, nil
	}
	// Same SHA (pure ref change, e.g. "git checkout -b newbranch"): the
	// working tree has not changed but the ref name has. Emit as Unknown so
	// the scheduler re-indexes under the new ref name.
	if oldSHA == newSHA {
		return ReindexUnknown, nil
	}

	// git diff --name-only <old>..<new>. Bounded (#5286): the poller's loop
	// goroutine calls this synchronously; an unbounded git diff on a U-state
	// (uninterruptible I/O) child during heavy churn previously wedged the whole
	// HEAD poller (no further branch-switch detection). On timeout/error we
	// fail-closed to ReindexUnknown → the scheduler does a full reindex, and the
	// poller keeps ticking.
	out, ok := gitmeta.RunGitBounded(repoPath, "diff", "--name-only", oldSHA+".."+newSHA)
	if !ok {
		logger.Warn("classifyRefChange: git diff failed or timed out (#5286) — full reindex", "repo", repoPath)
		return ReindexUnknown, nil
	}

	// Filter to indexed-source paths.
	lines := strings.Split(strings.TrimSpace(out), "\n")
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

// statModTime stats a file and returns its mod-time in nanoseconds.
// Returns 0 if the file cannot be stat-ed (missing, permission error, etc.).
// Increments the poller's stat counter.
func (p *GitHeadPoller) statModTime(path string) int64 {
	atomic.AddUint64(&p.statCalls, 1)
	fi, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return fi.ModTime().UnixNano()
}

// poll issues one stat per tracked file per common-dir group per cycle, then
// fans out BranchSwitchEvents to all repos in changed groups. S4 git-diff
// classification is per-repo (correct: the working tree differs per repo path
// even when HEAD is shared).
func (p *GitHeadPoller) poll() {
	// Snapshot group metadata under the lock, then do all I/O outside it.
	p.mu.Lock()
	type groupSnap struct {
		grp            *commonDirGroup
		prev           HeadSnapshot
		repos          []string
		representative string
	}
	snaps := make([]groupSnap, 0, len(p.groups))
	for _, grp := range p.groups {
		repos := make([]string, 0, len(grp.repos))
		for r := range grp.repos {
			repos = append(repos, r)
		}
		snaps = append(snaps, groupSnap{
			grp:            grp,
			prev:           grp.snap,
			repos:          repos,
			representative: grp.representative,
		})
	}
	p.mu.Unlock()

	for _, s := range snaps {
		// Stat 1: .git/HEAD — detects branch switches (one stat per group).
		newHeadMTime := p.statModTime(s.grp.headFile)
		if newHeadMTime == 0 {
			// HEAD file gone — repo deleted or temporarily inaccessible.
			continue
		}

		// Stat 2: .git/refs/heads/<branch> — detects new commits.
		// We use the previously known branch name; if the branch changed,
		// Stat 1's mod-time will have already triggered a capture below.
		refFile := refFileForBranch(s.grp.commonDir, s.prev.CurrentRef)
		var newRefMTime int64
		if refFile != "" {
			newRefMTime = p.statModTime(refFile)
		}

		// Skip if neither file changed since the last poll.
		if newHeadMTime == s.prev.HeadMTime && newRefMTime == s.prev.RefMTime {
			continue
		}

		// At least one file changed. Capture the new ref + SHA using the
		// representative repo path.
		if s.representative == "" {
			continue
		}
		meta := gitmeta.Capture(s.representative)
		current := HeadSnapshot{
			Ref:        meta.Ref,
			SHA:        meta.SHA,
			HeadMTime:  newHeadMTime,
			RefMTime:   newRefMTime,
			CurrentRef: meta.Ref,
		}

		// Update the stored snapshot under the lock.
		p.mu.Lock()
		if grp, ok := p.groups[s.grp.commonDir]; ok {
			grp.snap = current
		}
		p.mu.Unlock()

		// Check whether ref+SHA actually changed. If only mod-times changed
		// but content is identical (e.g. git gc rewrote a file), skip fan-out.
		if current.Ref == s.prev.Ref && current.SHA == s.prev.SHA {
			continue
		}

		// Fan out to every repo in the group.
		for _, repoPath := range s.repos {
			p.mu.Lock()
			_, stillRegistered := p.repoToGroup[repoPath]
			p.mu.Unlock()
			if !stillRegistered {
				continue
			}

			ev := BranchSwitchEvent{
				RepoPath: repoPath,
				OldRef:   s.prev.Ref,
				OldSHA:   s.prev.SHA,
				NewRef:   current.Ref,
				NewSHA:   current.SHA,
			}

			// S4 git-diff validation: per-repo source-change classification.
			hint, changedFiles := classifyRefChange(repoPath, s.prev.SHA, current.SHA, p.logger)
			ev.ReindexHint = hint
			ev.ChangedFiles = changedFiles

			if hint == ReindexNone {
				p.logger.Info("ref-change: no-source-changes — skipping reindex",
					"repo", repoPath, "old_sha", s.prev.SHA, "new_sha", current.SHA)
				continue
			}

			p.logger.Info("branch-switch detected",
				"repo", repoPath, "old_ref", ev.OldRef, "old_sha", ev.OldSHA,
				"new_ref", ev.NewRef, "new_sha", ev.NewSHA, "hint", hint, "changed_files", len(changedFiles))
			p.sink(ev)
		}
	}
}
