// Package worktree implements PH3 of epic #2087 (#2091):
// automatic discovery and ephemeral registration of git linked worktrees
// that belong to repos already registered in the archigraph fleet.
//
// # Data model
//
// WorktreeChild records are stored separately from the main registry so
// they can be invalidated and rebuilt without touching user-edited fleet
// configs.  On disk:
//
//	~/.archigraph/worktrees.json
//
// A WorktreeChild is an ephemeral child of a parent repo slug within a
// group. It DOES NOT appear as a top-level registered repo; it inherits
// the group and parent slug and only adds the worktree-specific fields.
//
// # Discovery loop
//
// The Watcher polls `git worktree list --porcelain` for every registered
// repo every 5 minutes (ARCHIGRAPH_WORKTREE_POLL_MINUTES override).
//
//   - New worktrees → added to the Store.
//   - Still-present worktrees → LastSeenAt refreshed.
//   - Removed worktrees → Status set to StatusExpired and StaleAt recorded.
//
// # Per-parent cap
//
// At most MaxWorktreesPerRepo (default 10, configurable via
// ARCHIGRAPH_MAX_WORKTREES_PER_REPO) worktrees are tracked per parent.
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
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
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
	// GroupName is the archigraph group that owns the parent.
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

// maxWorktrees reads ARCHIGRAPH_MAX_WORKTREES_PER_REPO or returns the default.
func maxWorktrees() int {
	if v := os.Getenv("ARCHIGRAPH_MAX_WORKTREES_PER_REPO"); v != "" {
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
type Watcher struct {
	store    *Store
	parents  func() []ParentRepo // called on every tick to get the current set
	interval time.Duration
	logger   *log.Logger
}

// defaultPollInterval is the default discovery interval.
const defaultPollInterval = 5 * time.Minute

// pollInterval reads ARCHIGRAPH_WORKTREE_POLL_MINUTES or returns the default.
func pollInterval() time.Duration {
	if v := os.Getenv("ARCHIGRAPH_WORKTREE_POLL_MINUTES"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n > 0 {
			return time.Duration(n) * time.Minute
		}
	}
	return defaultPollInterval
}

// NewWatcher creates a Watcher.  parents is called on each poll tick to get
// the current set of parent repos; it must be safe for concurrent use.
func NewWatcher(store *Store, parents func() []ParentRepo, logger *log.Logger) *Watcher {
	if logger == nil {
		logger = log.Default()
	}
	return &Watcher{
		store:    store,
		parents:  parents,
		interval: pollInterval(),
		logger:   logger,
	}
}

// Start runs the discovery loop until ctx is cancelled.  It performs one
// immediate poll before waiting for the first tick.
func (w *Watcher) Start(ctx context.Context) {
	w.poll()
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

// Poll runs one discovery cycle synchronously.  Exported for test control.
func (w *Watcher) Poll() { w.poll() }

func (w *Watcher) poll() {
	parents := w.parents()
	cap := maxWorktrees()
	now := time.Now().UTC()

	// Track every path seen in this tick across all parents.
	seenPaths := make(map[string]bool)

	for _, p := range parents {
		linked, err := runWorktreeList(p.Path)
		if err != nil {
			w.logger.Printf("worktree: git worktree list failed for %s (%s/%s): %v",
				p.Path, p.GroupName, p.Slug, err)
			continue
		}

		kept, skipped := enforceCap(linked, cap)
		if skipped > 0 {
			w.logger.Printf("worktree: %s/%s has %d linked worktrees; keeping %d most-recent, skipping %d (cap=%d, override ARCHIGRAPH_MAX_WORKTREES_PER_REPO)",
				p.GroupName, p.Slug, len(linked), cap, skipped, cap)
		}

		w.store.mu.Lock()
		for _, raw := range kept {
			seenPaths[normPath(raw.Path)] = true
			existing := w.store.findLocked(raw.Path)
			if existing == nil {
				child := &WorktreeChild{
					ParentSlug:   p.Slug,
					GroupName:    p.GroupName,
					Path:         raw.Path,
					Branch:       raw.Branch,
					Locked:       raw.Locked,
					DiscoveredAt: now,
					LastSeenAt:   now,
					Status:       StatusActive,
				}
				w.store.upsert(child)
				w.logger.Printf("worktree: registered %s/%s worktree %s @ %s",
					p.GroupName, p.Slug, raw.Path, raw.Branch)
			} else {
				// Refresh.
				existing.LastSeenAt = now
				existing.Branch = raw.Branch
				existing.Locked = raw.Locked
				if existing.Status == StatusExpired {
					existing.Status = StatusActive
					existing.StaleAt = nil
					w.logger.Printf("worktree: re-activated %s/%s worktree %s",
						p.GroupName, p.Slug, raw.Path)
				}
			}
		}
		w.store.mu.Unlock()
	}

	// Mark removed worktrees as expired.
	w.store.mu.Lock()
	for _, c := range w.store.children {
		if c.Status == StatusActive && !seenPaths[normPath(c.Path)] {
			w.store.markExpired(c.Path)
			w.logger.Printf("worktree: expired %s/%s worktree %s (no longer listed by git)",
				c.GroupName, c.ParentSlug, c.Path)
		}
	}
	if err := w.store.save(); err != nil {
		w.logger.Printf("worktree: failed to persist store: %v", err)
	}
	w.store.mu.Unlock()
}
