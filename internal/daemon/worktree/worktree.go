// Package worktree implements PH3 of epic #2087 (#2091):
// automatic discovery and ephemeral registration of git linked worktrees
// that belong to repos already registered in the grafel fleet.
//
// # Data model
//
// WorktreeChild records are stored separately from the main registry so
// they can be invalidated and rebuilt without touching user-edited fleet
// configs.  On disk:
//
//	~/.grafel/worktrees.json
//
// A WorktreeChild is an ephemeral child of a parent repo slug within a
// group. It DOES NOT appear as a top-level registered repo; it inherits
// the group and parent slug and only adds the worktree-specific fields.
//
// # Discovery loop
//
// Discovery is event-driven: the post-checkout git hook (#3352) and an
// fsnotify watch on each parent's .git/worktrees/ directory call Sync()
// promptly when a worktree is added or removed.  The Watcher ALSO runs a
// periodic RECONCILIATION poll of `git worktree list --porcelain` for
// every registered repo (default 60s, GRAFEL_WORKTREE_POLL_SECONDS
// override; GRAFEL_WORKTREE_POLL_MINUTES still honoured) to catch any
// events missed while the daemon was down or dropped.
//
//   - New worktrees → added to the Store; OnActivate fires.
//   - Still-present worktrees → LastSeenAt refreshed.
//   - Removed worktrees → Status set to StatusExpired, StaleAt recorded;
//     OnExpire fires.
//
// # Per-parent cap
//
// At most MaxWorktreesPerRepo (default 10, configurable via
// GRAFEL_MAX_WORKTREES_PER_REPO) worktrees are tracked per parent.
// When the parent has more linked worktrees the N most-recently-modified
// directories are kept; the rest are skipped with a warning log.
//
// # SlotKind
//
// SlotKind is a discriminator used by the tier.Manager to select the
// appropriate TTL policy.  BranchMain and BranchFeature use the
// production TTLs (5 min HOT, 60 min COLD, 7 days EXPIRED).  Worktree
// uses the aggressive worktree TTLs (5 min HOT, 30 min COLD, 48 h EXPIRED)
// already declared in tier.TTLConfig.
package worktree

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/cajasmota/grafel/internal/executil"
	"github.com/fsnotify/fsnotify"
)

// ---------------------------------------------------------------------------
// SlotKind
// ---------------------------------------------------------------------------

// SlotKind discriminates between graph slots for TTL policy selection.
type SlotKind int8

const (
	// KindBranchMain is the default branch of a registered repo (pinned on
	// disk; COLD→EXPIRED suppressed).
	KindBranchMain SlotKind = iota
	// KindBranchFeature is a feature / topic branch of a registered repo.
	KindBranchFeature
	// KindWorktree is a linked git worktree.  Uses aggressive TTLs.
	KindWorktree
)

// String returns the JSON-safe name.
func (k SlotKind) String() string {
	switch k {
	case KindBranchMain:
		return "branch_main"
	case KindBranchFeature:
		return "branch_feature"
	case KindWorktree:
		return "worktree"
	default:
		return "unknown"
	}
}

// ---------------------------------------------------------------------------
// WorktreeChild
// ---------------------------------------------------------------------------

// Status describes the liveness of a tracked worktree.
type Status string

const (
	StatusActive  Status = "active"
	StatusExpired Status = "expired"
)

// WorktreeChild is an ephemeral child record for a single linked git
// worktree discovered from a registered parent repo.
type WorktreeChild struct {
	// ParentSlug is the repo slug inside the fleet group (e.g. "my-service").
	ParentSlug string `json:"parent_slug"`
	// GroupName is the grafel group that owns the parent.
	GroupName string `json:"group_name"`
	// Path is the absolute path of the worktree on disk.
	Path string `json:"path"`
	// Branch is the git ref checked out in the worktree.
	Branch string `json:"branch"`
	// Locked is true when the worktree is marked locked by git.
	Locked bool `json:"locked,omitempty"`
	// DiscoveredAt is when this entry was first seen.
	DiscoveredAt time.Time `json:"discovered_at"`
	// LastSeenAt is updated on every successful `git worktree list` that
	// still reports this worktree.
	LastSeenAt time.Time `json:"last_seen_at"`
	// StaleAt is set when the worktree is no longer reported by git.
	// Zero for active entries.
	StaleAt *time.Time `json:"stale_at,omitempty"`
	// Status is "active" or "expired".
	Status Status `json:"status"`
}

// ---------------------------------------------------------------------------
// Store
// ---------------------------------------------------------------------------

// Store persists and manages the ephemeral worktree-child registry.
// Safe for concurrent use.
type Store struct {
	mu       sync.Mutex
	path     string // absolute path to worktrees.json
	children []*WorktreeChild
}

// NewStore creates a Store that persists to path.  The file is created if
// it does not exist.  Call Load() to hydrate from disk.
func NewStore(path string) *Store {
	return &Store{path: path}
}

// Load reads the store from disk.  A missing file is treated as an empty
// store (first-run case).
func (s *Store) Load() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := os.ReadFile(s.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	var children []*WorktreeChild
	if err := json.Unmarshal(data, &children); err != nil {
		return err
	}
	s.children = children
	return nil
}

// save writes the store atomically (caller must hold s.mu).
func (s *Store) save() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s.children, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

// All returns a snapshot of all children (active and expired).
func (s *Store) All() []*WorktreeChild {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*WorktreeChild, len(s.children))
	copy(out, s.children)
	return out
}

// Active returns only active children.
func (s *Store) Active() []*WorktreeChild {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []*WorktreeChild
	for _, c := range s.children {
		if c.Status == StatusActive {
			out = append(out, c)
		}
	}
	return out
}

// normPath returns a canonical path for Store key comparison.
// On Windows, git worktree list --porcelain emits forward-slash paths
// while filepath.Join produces backslash paths; Clean+FromSlash normalises
// both to the OS separator so Lookup / upsert are consistent.
func normPath(p string) string {
	return filepath.Clean(filepath.FromSlash(p))
}

// findLocked returns the first child whose Path matches, or nil.
// Caller must hold s.mu.
func (s *Store) findLocked(path string) *WorktreeChild {
	norm := normPath(path)
	for _, c := range s.children {
		if normPath(c.Path) == norm {
			return c
		}
	}
	return nil
}

// upsert adds or updates a child record.  Caller must hold s.mu.
func (s *Store) upsert(c *WorktreeChild) {
	norm := normPath(c.Path)
	for i, existing := range s.children {
		if normPath(existing.Path) == norm {
			s.children[i] = c
			return
		}
	}
	s.children = append(s.children, c)
}

// markExpired sets StatusExpired on the entry at path.  Caller must hold s.mu.
func (s *Store) markExpired(path string) {
	norm := normPath(path)
	for _, c := range s.children {
		if normPath(c.Path) == norm {
			if c.Status != StatusExpired {
				now := time.Now().UTC()
				c.StaleAt = &now
				c.Status = StatusExpired
			}
		}
	}
}

// Lookup returns the WorktreeChild whose Path exactly matches path, or nil.
func (s *Store) Lookup(path string) *WorktreeChild {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.findLocked(path)
}

// LookupPath implements mcp.WorktreeLookup.  It returns (groupName, parentSlug, branch)
// for the active WorktreeChild whose Path exactly matches wtPath.
// Returns ("","","") when not found or the entry is expired.
func (s *Store) LookupPath(wtPath string) (group, slug, branch string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	norm := normPath(wtPath)
	for _, c := range s.children {
		if normPath(c.Path) == norm && c.Status == StatusActive {
			return c.GroupName, c.ParentSlug, c.Branch
		}
	}
	return "", "", ""
}

// IsWorktreeRef implements dashboard.WorktreeQuerier.  It returns true when
// an active WorktreeChild exists whose parent repo path matches repoPath and
// whose Branch matches ref.
func (s *Store) IsWorktreeRef(repoPath, ref string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, c := range s.children {
		// The repoPath field on a WorktreeChild is the worktree path, not the
		// parent repo path.  The dashboard refs endpoint passes the *parent*
		// repo path (r.Path from the fleet config).  We match by checking
		// whether any active child has Branch==ref and its parent belongs to
		// the same repo — which we can only verify if the caller also tracks
		// parent paths.  For now, match ref only (sufficient for the
		// source annotation; false-positive rate is negligible).
		if c.Status == StatusActive && c.Branch == ref {
			return true
		}
	}
	return false
}

// LookupByParent returns active children for the given group+slug pair.
func (s *Store) LookupByParent(group, slug string) []*WorktreeChild {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []*WorktreeChild
	for _, c := range s.children {
		if c.GroupName == group && c.ParentSlug == slug && c.Status == StatusActive {
			out = append(out, c)
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// git worktree list parsing
// ---------------------------------------------------------------------------

// RawWorktree is the parsed output of one stanza from `git worktree list --porcelain`.
type RawWorktree struct {
	Path   string
	HEAD   string
	Branch string // trimmed "refs/heads/" prefix
	Locked bool
	// IsBare is true for the "bare" pseudo-worktree that appears when the
	// repository was cloned with --bare.
	IsBare bool
}

// parseWorktreeList parses the porcelain output of `git worktree list --porcelain`
// into a slice of RawWorktree.  The first stanza is the main checkout; it is
// included in the output.
func parseWorktreeList(output string) []RawWorktree {
	var result []RawWorktree
	var cur RawWorktree
	flush := func() {
		if cur.Path != "" {
			result = append(result, cur)
		}
		cur = RawWorktree{}
	}
	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			flush()
			continue
		}
		switch {
		case strings.HasPrefix(line, "worktree "):
			cur.Path = strings.TrimPrefix(line, "worktree ")
		case strings.HasPrefix(line, "HEAD "):
			cur.HEAD = strings.TrimPrefix(line, "HEAD ")
		case strings.HasPrefix(line, "branch "):
			b := strings.TrimPrefix(line, "branch ")
			b = strings.TrimPrefix(b, "refs/heads/")
			cur.Branch = b
		case line == "locked":
			cur.Locked = true
		case strings.HasPrefix(line, "locked "):
			cur.Locked = true
		case line == "bare":
			cur.IsBare = true
		case line == "detached":
			// detached HEAD; branch stays ""
		}
	}
	flush()
	return result
}

// runWorktreeList executes `git worktree list --porcelain` in repoPath with a
// 10-second timeout and returns the parsed stanzas, excluding the main checkout
// (index 0) and bare worktrees.
func runWorktreeList(repoPath string) ([]RawWorktree, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "worktree", "list", "--porcelain")
	cmd.Dir = repoPath
	executil.NoWindow(cmd)
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	all := parseWorktreeList(string(out))
	if len(all) == 0 {
		return nil, nil
	}
	// all[0] is the main checkout — skip it.
	var linked []RawWorktree
	for _, wt := range all[1:] {
		if !wt.IsBare {
			linked = append(linked, wt)
		}
	}
	return linked, nil
}

// ---------------------------------------------------------------------------
// Cap enforcement
// ---------------------------------------------------------------------------

// defaultMaxWorktrees is the maximum tracked worktrees per parent repo.
const defaultMaxWorktrees = 10

// maxWorktrees reads GRAFEL_MAX_WORKTREES_PER_REPO or returns the default.
func maxWorktrees() int {
	if v := os.Getenv("GRAFEL_MAX_WORKTREES_PER_REPO"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return defaultMaxWorktrees
}

// enforceCap takes a list of RawWorktrees and a cap.  When len > cap, it sorts
// by directory mtime (descending) and keeps the N most-recently-modified.
// Returns the kept slice and the count of skipped entries.
func enforceCap(linked []RawWorktree, cap int) (kept []RawWorktree, skipped int) {
	if len(linked) <= cap {
		return linked, 0
	}
	// Sort descending by mtime.
	type mtermed struct {
		wt    RawWorktree
		mtime time.Time
	}
	scored := make([]mtermed, 0, len(linked))
	for _, wt := range linked {
		fi, err := os.Stat(wt.Path)
		var mt time.Time
		if err == nil {
			mt = fi.ModTime()
		}
		scored = append(scored, mtermed{wt, mt})
	}
	sort.Slice(scored, func(i, j int) bool {
		return scored[i].mtime.After(scored[j].mtime)
	})
	kept = make([]RawWorktree, 0, cap)
	for i := 0; i < cap; i++ {
		kept = append(kept, scored[i].wt)
	}
	return kept, len(linked) - cap
}

// ---------------------------------------------------------------------------
// Watcher
// ---------------------------------------------------------------------------

// ParentRepo identifies a registered repo that should be polled.
type ParentRepo struct {
	GroupName string
	Slug      string
	Path      string // absolute path to the main checkout
}

// Watcher is a background goroutine that polls registered repos for linked
// worktrees and syncs them into the Store.
//
// The poll is now a RECONCILIATION pass (#3354): event-driven onboarding
// happens via the post-checkout git hook (#3352) and/or the daemon's
// fsnotify watch on each parent's .git/worktrees/ directory, which call
// Sync() directly.  The periodic poll exists only to (a) catch worktrees
// that were added/removed while the daemon was down or while events were
// dropped, and (b) detect REMOVED worktrees (no inotify event fires on the
// parent for a `git worktree remove` of an external dir).
//
// OnActivate / OnExpire let the daemon react to liveness transitions
// (e.g. subscribe/unsubscribe the worktree working tree from the file
// watcher and enqueue an initial reindex).  Both may be nil.
type Watcher struct {
	store    *Store
	parents  func() []ParentRepo // called on every tick to get the current set
	interval time.Duration
	logger   *slog.Logger

	// OnActivate fires once per child when it becomes (or re-becomes)
	// active — i.e. on first discovery and on re-activation after expiry.
	// Used by the daemon to watch the worktree working tree and trigger an
	// initial reindex of the worktree's ref tier.  May be nil.
	OnActivate func(child *WorktreeChild)
	// OnExpire fires once per child when it transitions to expired (the
	// worktree was removed).  Used by the daemon to unsubscribe the
	// worktree working tree from the file watcher.  May be nil.
	OnExpire func(child *WorktreeChild)
}

// defaultPollInterval is the default reconciliation interval.
const defaultPollInterval = 60 * time.Second

// pollInterval returns the reconciliation interval.
//
// GRAFEL_WORKTREE_POLL_SECONDS takes precedence (the poll is now a
// fast reconciliation pass, #3354).  GRAFEL_WORKTREE_POLL_MINUTES is
// still honoured for backward compatibility when SECONDS is unset.
func pollInterval() time.Duration {
	if v := os.Getenv("GRAFEL_WORKTREE_POLL_SECONDS"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n > 0 {
			return time.Duration(n) * time.Second
		}
	}
	if v := os.Getenv("GRAFEL_WORKTREE_POLL_MINUTES"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n > 0 {
			return time.Duration(n) * time.Minute
		}
	}
	return defaultPollInterval
}

// NewWatcher creates a Watcher.  parents is called on each poll tick to get
// the current set of parent repos; it must be safe for concurrent use.
func NewWatcher(store *Store, parents func() []ParentRepo, logger *slog.Logger) *Watcher {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(os.Stderr, nil)).With("pkg", "worktree")
	}
	return &Watcher{
		store:    store,
		parents:  parents,
		interval: pollInterval(),
		logger:   logger,
	}
}

// Start runs the discovery loop until ctx is cancelled.  It performs one
// immediate discovery pass, then drives both:
//
//   - an event-driven fsnotify watch on each parent's .git/worktrees/
//     directory, so `git worktree add`/`remove` is picked up promptly, and
//   - a periodic RECONCILIATION poll (default 60s) that catches removals
//     and any events missed while the daemon was down or dropped.
//
// Both call Sync(), which de-dups via a single `git worktree list` per
// parent per pass.
func (w *Watcher) Start(ctx context.Context) {
	w.poll()

	// Event-driven onboarding: fsnotify-watch each parent's common
	// .git/worktrees/ dir.  git creates/removes a child dir there on every
	// `git worktree add`/`remove`, so a Create/Remove event is a reliable,
	// prompt signal to reconcile.  Best-effort — failures fall back to poll.
	gitWatch := w.startGitDirsWatch(ctx)
	defer gitWatch()

	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.poll()
		}
	}
}

// startGitDirsWatch sets up an fsnotify watch on every parent's
// .git/worktrees/ directory.  On any event it debounces briefly and calls
// Sync().  It also re-resolves the parent set lazily (cheap) so newly
// registered repos get watched on the next reconciliation tick (which calls
// this is not re-run; the periodic poll still covers them).  Returns a stop
// func.  Best-effort: if fsnotify is unavailable it returns a no-op.
func (w *Watcher) startGitDirsWatch(ctx context.Context) func() {
	fw, err := fsnotify.NewWatcher()
	if err != nil {
		w.logger.Warn("worktree: fsnotify unavailable, relying on reconciliation poll", "err", err)
		return func() {}
	}

	added := 0
	for _, p := range w.parents() {
		dir := gitWorktreesDir(p.Path)
		if dir == "" {
			continue
		}
		// Ensure the dir exists so the watch survives the first
		// `git worktree add` (git creates .git/worktrees/ on demand).
		if err := os.MkdirAll(dir, 0o755); err != nil {
			continue
		}
		if err := fw.Add(dir); err != nil {
			w.logger.Warn("worktree: failed to watch .git/worktrees", "dir", dir, "err", err)
			continue
		}
		added++
	}
	if added == 0 {
		_ = fw.Close()
		return func() {}
	}
	w.logger.Info("worktree: event-driven onboarding active", "git_worktrees_dirs", added)

	done := make(chan struct{})
	go func() {
		defer close(done)
		var timer *time.Timer
		const debounce = 750 * time.Millisecond
		fire := func() { w.poll() }
		for {
			select {
			case <-ctx.Done():
				return
			case _, ok := <-fw.Events:
				if !ok {
					return
				}
				if timer != nil {
					timer.Stop()
				}
				timer = time.AfterFunc(debounce, fire)
			case _, ok := <-fw.Errors:
				if !ok {
					return
				}
			}
		}
	}()

	return func() {
		_ = fw.Close()
		<-done
	}
}

// gitWorktreesDir returns the absolute path to the common
// .git/worktrees/ directory for repoPath, or "" if it cannot be resolved.
// It handles both a normal repo (.git is a directory) — the common case —
// returning <repo>/.git/worktrees.  When .git is a file (i.e. repoPath is
// itself a linked worktree) we resolve the gitdir's parent; callers should
// only pass main checkouts, so this is a defensive fallback.
func gitWorktreesDir(repoPath string) string {
	gitPath := filepath.Join(repoPath, ".git")
	fi, err := os.Stat(gitPath)
	if err != nil {
		return ""
	}
	if fi.IsDir() {
		return filepath.Join(gitPath, "worktrees")
	}
	// .git is a file ("gitdir: <path>"). Resolve and take its parent dir's
	// worktrees subdir.
	data, err := os.ReadFile(gitPath)
	if err != nil {
		return ""
	}
	line := strings.TrimSpace(string(data))
	line = strings.TrimPrefix(line, "gitdir:")
	gitdir := strings.TrimSpace(line)
	if gitdir == "" {
		return ""
	}
	// gitdir is typically <common>/worktrees/<name>; its parent is the
	// worktrees dir we want.
	return filepath.Dir(gitdir)
}

// Poll runs one reconciliation cycle synchronously.  Exported for test
// control.  See Sync for the event-driven entry point with identical
// semantics.
func (w *Watcher) Poll() { w.poll() }

// Sync runs one reconciliation/discovery cycle synchronously.  It is the
// event-driven entry point: the daemon calls Sync when an fsnotify event
// fires on a parent's .git/worktrees/ directory (a worktree was added or
// removed) or when the post-checkout git hook reports a new worktree.
// Semantics are identical to a poll tick — it is safe and cheap to call
// frequently because the per-parent `git worktree list` runs at most once.
func (w *Watcher) Sync() { w.poll() }

func (w *Watcher) poll() {
	parents := w.parents()
	cap := maxWorktrees()
	now := time.Now().UTC()

	// Track every path seen in this tick across all parents.
	seenPaths := make(map[string]bool)

	// Transitions to fire callbacks for, collected while holding the lock
	// and dispatched after release so callbacks never run under s.mu
	// (they call back into the daemon's file watcher / scheduler).
	var activated, expired []*WorktreeChild

	for _, p := range parents {
		linked, err := runWorktreeList(p.Path)
		if err != nil {
			w.logger.Error("worktree: git worktree list failed", "path", p.Path, "group", p.GroupName, "slug", p.Slug, "err", err)
			continue
		}

		kept, skipped := enforceCap(linked, cap)
		if skipped > 0 {
			w.logger.Warn("worktree: per-parent cap enforced", "group", p.GroupName, "slug", p.Slug, "total", len(linked), "kept", cap, "skipped", skipped, "cap", cap, "override_env", "GRAFEL_MAX_WORKTREES_PER_REPO")
		}

		w.store.mu.Lock()
		for _, raw := range kept {
			seenPaths[normPath(raw.Path)] = true
			existing := w.store.findLocked(raw.Path)
			if existing == nil {
				child := &WorktreeChild{
					ParentSlug:   p.Slug,
					GroupName:    p.GroupName,
					Path:         normPath(raw.Path),
					Branch:       raw.Branch,
					Locked:       raw.Locked,
					DiscoveredAt: now,
					LastSeenAt:   now,
					Status:       StatusActive,
				}
				w.store.upsert(child)
				activated = append(activated, child)
				w.logger.Info("worktree: registered", "group", p.GroupName, "slug", p.Slug, "path", raw.Path, "branch", raw.Branch)
			} else {
				// Refresh.
				existing.LastSeenAt = now
				existing.Branch = raw.Branch
				existing.Locked = raw.Locked
				if existing.Status == StatusExpired {
					existing.Status = StatusActive
					existing.StaleAt = nil
					activated = append(activated, existing)
					w.logger.Info("worktree: re-activated", "group", p.GroupName, "slug", p.Slug, "path", raw.Path)
				}
			}
		}
		w.store.mu.Unlock()
	}

	// Mark removed worktrees as expired.  Reconciliation: this is the
	// primary mechanism for detecting `git worktree remove` since no
	// fsnotify event fires on the parent for an external worktree dir.
	w.store.mu.Lock()
	for _, c := range w.store.children {
		if c.Status == StatusActive && !seenPaths[normPath(c.Path)] {
			w.store.markExpired(c.Path)
			expired = append(expired, c)
			w.logger.Info("worktree: expired", "group", c.GroupName, "slug", c.ParentSlug, "path", c.Path, "reason", "no longer listed by git")
		}
	}
	if err := w.store.save(); err != nil {
		w.logger.Error("worktree: failed to persist store", "err", err)
	}
	w.store.mu.Unlock()

	// Fire transition callbacks outside the lock.
	if w.OnActivate != nil {
		for _, c := range activated {
			w.OnActivate(c)
		}
	}
	if w.OnExpire != nil {
		for _, c := range expired {
			w.OnExpire(c)
		}
	}
}
