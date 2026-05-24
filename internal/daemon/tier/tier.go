// Package tier implements the HOT/WARM/COLD/EXPIRED state machine for loaded
// graphs (PH2 of epic #2087 / issue #2090, extended by PH3 #2091, PH6 #2094).
//
// # Tier definitions
//
//   - HOT   – graph resident in memory, accessed within the hot window.
//   - WARM  – graph resident in memory, idle longer than the hot window.
//   - COLD  – graph evicted from memory; graph.fb is still on disk.
//   - EXPIRED – disk artifact eligible for deletion.
//
// # Transitions
//
//	HOT  → WARM  : 5 min idle  (status change only, no memory action)
//	WARM → COLD  : 1 h  idle   (release in-memory reference; GC eligible)
//	COLD → HOT   : demand wake (reload from disk; ≤ 300–500 ms typical)
//	COLD → EXPIRED : idle past ExpiredWindow (PH6: also triggers disk delete)
//
// PH6 (#2094): when a slot reaches EXPIRED the Manager calls the optional
// DiskEvictCallback which deletes the graph artifacts from disk.
// Pinned main branches (default branch of a registered repo) are
// disk-pinned: they can WARM→COLD but never COLD→EXPIRED.
//
// # Env-tunable TTLs
//
//	ARCHIGRAPH_TIER_HOT_MINUTES           default 5
//	ARCHIGRAPH_TIER_COLD_MINUTES          default 60
//	ARCHIGRAPH_TIER_COLD_MINUTES_WORKTREE default 30
//	ARCHIGRAPH_TIER_EXPIRED_DAYS          default 7  (feature branches)
//	ARCHIGRAPH_TIER_EXPIRED_DAYS_WORKTREE default 2
package tier

import (
	"context"
	"log"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/cajasmota/archigraph/internal/gitmeta"
)

// ---------------------------------------------------------------------------
// Tier
// ---------------------------------------------------------------------------

// Tier is the lifecycle state of a loaded (repoPath, ref) graph slot.
type Tier int8

const (
	// TierHot – graph resident in memory; accessed recently.
	TierHot Tier = iota
	// TierWarm – graph resident in memory; idle > HotWindow.
	TierWarm
	// TierCold – graph evicted from memory; disk artifact present.
	TierCold
	// TierExpired – disk artifact eligible for deletion (PH6 only).
	TierExpired
)

// String returns the lowercase JSON-safe tier name.
func (t Tier) String() string {
	switch t {
	case TierHot:
		return "hot"
	case TierWarm:
		return "warm"
	case TierCold:
		return "cold"
	case TierExpired:
		return "expired"
	default:
		return "unknown"
	}
}

// ---------------------------------------------------------------------------
// Callbacks
// ---------------------------------------------------------------------------

// EvictionCallback is invoked by the Manager scanner when a WARM slot becomes
// COLD. The implementation should release any in-memory graph reference and
// may trigger runtime.GC(). Invoked synchronously from the scanner goroutine;
// must not block for more than a few milliseconds.
type EvictionCallback func(key SlotKey)

// ReloadCallback is invoked by Touch when a COLD slot receives a query.
// The implementation should reload the graph from disk and return nil on
// success. On failure the slot stays COLD and Touch returns the error.
type ReloadCallback func(key SlotKey) error

// DiskEvictCallback is invoked by the Manager scanner when a COLD slot
// transitions to EXPIRED (PH6 #2094). The implementation should delete the
// graph artifacts from disk and return the number of bytes freed.
// If nil, disk eviction is skipped (dry-run / test scenarios).
// Invoked synchronously from the scanner goroutine outside the Manager lock.
type DiskEvictCallback func(key SlotKey) (freedBytes int64, err error)

// WatcherHook is the narrow interface tier.Manager uses to pause/resume the
// fsnotify subscription for a slot. Implemented by watch.DefaultManager.
// Either method may be nil — Manager checks before calling.
// PH2a of epic #2087 (#2096).
type WatcherHook interface {
	// Pause removes the fsnotify subscription for (repoPath, ref) when all
	// refs for that repoPath are paused.
	Pause(repoPath, ref string)
	// Resume re-adds the fsnotify subscription for (repoPath, ref) when it
	// was fully unsubscribed.
	Resume(repoPath, ref string) time.Duration
}

// ---------------------------------------------------------------------------
// SlotKey
// ---------------------------------------------------------------------------

// SlotKey uniquely identifies a (repoPath, ref) graph slot inside the Manager.
type SlotKey struct {
	RepoPath string // absolute repo path on disk
	Ref      string // git branch/tag name ("" = _unknown sentinel)
}

// ---------------------------------------------------------------------------
// TTLConfig
// ---------------------------------------------------------------------------

// TTLConfig holds the tunable TTL windows.
type TTLConfig struct {
	HotWindow             time.Duration
	ColdWindow            time.Duration
	ColdWindowWorktree    time.Duration
	ExpiredWindow         time.Duration
	ExpiredWindowWorktree time.Duration
}

// DefaultTTLConfig returns the spec's production defaults.
func DefaultTTLConfig() TTLConfig {
	return TTLConfig{
		HotWindow:             5 * time.Minute,
		ColdWindow:            60 * time.Minute,
		ColdWindowWorktree:    30 * time.Minute,
		ExpiredWindow:         7 * 24 * time.Hour,
		ExpiredWindowWorktree: 48 * time.Hour,
	}
}

// EnvTTLConfig reads env-var overrides on top of DefaultTTLConfig.
func EnvTTLConfig() TTLConfig {
	cfg := DefaultTTLConfig()
	if v := envMinutes("ARCHIGRAPH_TIER_HOT_MINUTES"); v > 0 {
		cfg.HotWindow = v
	}
	if v := envMinutes("ARCHIGRAPH_TIER_COLD_MINUTES"); v > 0 {
		cfg.ColdWindow = v
	}
	if v := envMinutes("ARCHIGRAPH_TIER_COLD_MINUTES_WORKTREE"); v > 0 {
		cfg.ColdWindowWorktree = v
	}
	if v := envDays("ARCHIGRAPH_TIER_EXPIRED_DAYS"); v > 0 {
		cfg.ExpiredWindow = v
	}
	if v := envDays("ARCHIGRAPH_TIER_EXPIRED_DAYS_WORKTREE"); v > 0 {
		cfg.ExpiredWindowWorktree = v
	}
	return cfg
}

func envMinutes(name string) time.Duration {
	s := os.Getenv(name)
	if s == "" {
		return 0
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil || n <= 0 {
		return 0
	}
	return time.Duration(n) * time.Minute
}

func envDays(name string) time.Duration {
	s := os.Getenv(name)
	if s == "" {
		return 0
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil || n <= 0 {
		return 0
	}
	return time.Duration(n) * 24 * time.Hour
}

// ---------------------------------------------------------------------------
// internal slot
// ---------------------------------------------------------------------------

// SlotKind is a discriminator that controls which TTL policy applies to a
// graph slot.  Imported from internal/daemon/worktree to avoid a circular
// dependency; duplicated here as a thin alias so callers need only import
// internal/daemon/tier for TTL decisions.
//
// PH3 (#2091): the Manager now selects WARM→COLD and COLD→EXPIRED windows
// based on SlotKind, giving worktree graphs the aggressive 30 min / 48 h
// TTLs specified in ADR-0018.
type SlotKind int8

const (
	// SlotKindBranchMain is the default branch of a registered repo (disk-pinned;
	// COLD→EXPIRED suppressed).
	SlotKindBranchMain SlotKind = iota
	// SlotKindBranchFeature is a feature/topic branch of a registered repo.
	SlotKindBranchFeature
	// SlotKindWorktree is a linked git worktree.  Uses the aggressive
	// ColdWindowWorktree / ExpiredWindowWorktree TTLs from TTLConfig.
	SlotKindWorktree
)

// String returns the lowercase JSON-safe kind name.
func (k SlotKind) String() string {
	switch k {
	case SlotKindBranchMain:
		return "branch_main"
	case SlotKindBranchFeature:
		return "branch_feature"
	case SlotKindWorktree:
		return "worktree"
	default:
		return "unknown"
	}
}

type slot struct {
	tier           Tier
	kind           SlotKind
	lastAccessedAt time.Time
	isPinnedMain   bool // disk-pinned: COLD→EXPIRED suppressed
}

// ---------------------------------------------------------------------------
// Manager
// ---------------------------------------------------------------------------

// Manager tracks HOT/WARM/COLD/EXPIRED state for all active (repoPath, ref)
// graph slots. Safe for concurrent use. A background scanner goroutine (every
// 30 s) applies idle-TTL transitions and fires eviction callbacks.
type Manager struct {
	mu          sync.Mutex
	slots       map[SlotKey]*slot
	ttl         TTLConfig
	onEvict     EvictionCallback
	reload      ReloadCallback
	onDiskEvict DiskEvictCallback // PH6: nil = no disk deletion
	logger      *log.Logger
	clock       func() time.Time
	// watcher is optional (PH2a #2096). When non-nil, Pause is called on
	// WARM→COLD and Resume is called on COLD→HOT.
	watcher WatcherHook
}

const defaultScanInterval = 30 * time.Second

// NewManager creates and starts a Manager. The scanner runs until ctx is
// cancelled. onEvict and reload must not be nil. onDiskEvict may be nil
// (disables disk deletion).
func NewManager(ctx context.Context, ttl TTLConfig, onEvict EvictionCallback, reload ReloadCallback, onDiskEvict DiskEvictCallback, logger *log.Logger) *Manager {
	if logger == nil {
		logger = log.Default()
	}
	m := &Manager{
		slots:       make(map[SlotKey]*slot),
		ttl:         ttl,
		onEvict:     onEvict,
		reload:      reload,
		onDiskEvict: onDiskEvict,
		logger:      logger,
		clock:       time.Now,
	}
	go m.scanLoop(ctx, defaultScanInterval)
	return m
}

// NewManagerForTest creates a Manager without starting the scan loop, using a
// caller-supplied clock. Call Scan() explicitly to trigger transitions.
// onDiskEvict may be nil.
func NewManagerForTest(ttl TTLConfig, clock func() time.Time, onEvict EvictionCallback, reload ReloadCallback) *Manager {
	return &Manager{
		slots:   make(map[SlotKey]*slot),
		ttl:     ttl,
		onEvict: onEvict,
		reload:  reload,
		logger:  log.Default(),
		clock:   clock,
	}
}

// NewManagerForTestWithDiskEvict is like NewManagerForTest but also accepts
// a DiskEvictCallback (PH6 tests).
func NewManagerForTestWithDiskEvict(ttl TTLConfig, clock func() time.Time, onEvict EvictionCallback, reload ReloadCallback, onDiskEvict DiskEvictCallback) *Manager {
	return &Manager{
		slots:       make(map[SlotKey]*slot),
		ttl:         ttl,
		onEvict:     onEvict,
		reload:      reload,
		onDiskEvict: onDiskEvict,
		logger:      log.Default(),
		clock:       clock,
	}
}

// SetWatcherHook wires a WatcherHook so that tier transitions also pause/resume
// the fsnotify subscription. Call before the daemon starts serving. PH2a #2096.
func (m *Manager) SetWatcherHook(wh WatcherHook) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.watcher = wh
}

// ---------------------------------------------------------------------------
// Public API
// ---------------------------------------------------------------------------

// Register declares (or re-activates) a slot as HOT. isPinnedMain should be
// true for the repository's default branch. kind selects the TTL policy:
// SlotKindWorktree uses the aggressive 30-min/48-h windows; others use the
// standard branch windows.
func (m *Manager) Register(key SlotKey, isPinnedMain bool, kind SlotKind) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.slots[key]
	if !ok {
		s = &slot{}
		m.slots[key] = s
	}
	s.tier = TierHot
	s.kind = kind
	s.lastAccessedAt = m.clock()
	s.isPinnedMain = isPinnedMain
}

// Touch records an access for key, refreshing lastAccessedAt. If the slot is
// COLD, triggers an in-place reload (via the ReloadCallback) and transitions
// back to HOT. If the slot is unknown it is auto-registered as HOT.
// Returns an error only when a required reload fails.
func (m *Manager) Touch(key SlotKey) error {
	m.mu.Lock()
	s, ok := m.slots[key]
	if !ok {
		s = &slot{tier: TierHot}
		m.slots[key] = s
	}
	wasCold := s.tier == TierCold
	s.lastAccessedAt = m.clock()
	m.mu.Unlock()

	if wasCold {
		// PH2a: resume watcher subscription before reloading the graph so that
		// any file changes that arrived while the slot was COLD are captured.
		m.mu.Lock()
		wh := m.watcher
		m.mu.Unlock()
		if wh != nil {
			resumeElapsed := wh.Resume(key.RepoPath, key.Ref)
			m.logger.Printf("tier: cold-wake watcher resumed %s@%s (took %s)", key.RepoPath, key.Ref, resumeElapsed.Round(time.Millisecond))
		}

		if err := m.reload(key); err != nil {
			m.logger.Printf("tier: cold-load failed %s@%s: %v", key.RepoPath, key.Ref, err)
			return err
		}
		m.mu.Lock()
		if s2, ok2 := m.slots[key]; ok2 {
			s2.tier = TierHot
			s2.lastAccessedAt = m.clock()
		}
		m.mu.Unlock()
		m.logger.Printf("tier: cold-load OK %s@%s → HOT", key.RepoPath, key.Ref)
	}
	return nil
}

// Get returns the current Tier for key. Returns TierCold for unknown slots.
func (m *Manager) Get(key SlotKey) Tier {
	m.mu.Lock()
	s, ok := m.slots[key]
	if !ok {
		m.mu.Unlock()
		return TierCold
	}
	t := s.tier
	m.mu.Unlock()
	return t
}

// TierForRef returns the tier string ("hot"/"warm"/"cold"/"expired") for the
// given (repoPath, ref) pair. Returns "cold" for unknown slots. Implements
// dashboard.TierQuerier.
func (m *Manager) TierForRef(repoPath, ref string) string {
	return m.Get(SlotKey{RepoPath: repoPath, Ref: ref}).String()
}

// SlotSnapshot is a point-in-time copy of one slot's state.
type SlotSnapshot struct {
	Key            SlotKey
	Tier           Tier
	Kind           SlotKind
	LastAccessedAt time.Time
	IsPinnedMain   bool
}

// Snapshot returns a copy of all slots for diagnostics.
func (m *Manager) Snapshot() []SlotSnapshot {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]SlotSnapshot, 0, len(m.slots))
	for k, s := range m.slots {
		out = append(out, SlotSnapshot{Key: k, Tier: s.tier, Kind: s.kind, LastAccessedAt: s.lastAccessedAt, IsPinnedMain: s.isPinnedMain})
	}
	return out
}

// Scan triggers a single scanner tick synchronously. In production the scanner
// runs automatically; this is exported for deterministic test control.
func (m *Manager) Scan() { m.scan() }

// ---------------------------------------------------------------------------
// Scanner
// ---------------------------------------------------------------------------

func (m *Manager) scanLoop(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.scan()
		}
	}
}

func (m *Manager) scan() {
	m.mu.Lock()
	now := m.clock()
	var toEvict []SlotKey
	var toExpire []SlotKey
	for k, s := range m.slots {
		idle := now.Sub(s.lastAccessedAt)

		// PH3 (#2091): select TTL windows based on SlotKind.
		// Worktree slots use the aggressive windows; others use the branch windows.
		coldWindow := m.ttl.ColdWindow
		expiredWindow := m.ttl.ExpiredWindow
		if s.kind == SlotKindWorktree {
			coldWindow = m.ttl.ColdWindowWorktree
			expiredWindow = m.ttl.ExpiredWindowWorktree
		}

		switch s.tier {
		case TierHot:
			if idle >= m.ttl.HotWindow {
				s.tier = TierWarm
				m.logger.Printf("tier: %s@%s HOT→WARM (idle %s, kind=%s)", k.RepoPath, k.Ref, idle.Round(time.Second), s.kind)
			}
		case TierWarm:
			if idle >= coldWindow {
				s.tier = TierCold
				toEvict = append(toEvict, k)
				m.logger.Printf("tier: %s@%s WARM→COLD (idle %s, kind=%s)", k.RepoPath, k.Ref, idle.Round(time.Second), s.kind)
			}
		case TierCold:
			// PH6 (#2094): COLD→EXPIRED — delete disk artifacts when not pinned.
			if !s.isPinnedMain && idle >= expiredWindow {
				s.tier = TierExpired
				toExpire = append(toExpire, k)
				m.logger.Printf("tier: %s@%s COLD→EXPIRED (idle %s, kind=%s)", k.RepoPath, k.Ref, idle.Round(time.Hour), s.kind)
			}
		}
	}
	m.mu.Unlock()

	// PH2a: pause watcher subscriptions for newly-COLD slots.
	wh := m.watcher // read under mu already released; field is write-once after init
	for _, k := range toEvict {
		m.onEvict(k)
		if wh != nil {
			wh.Pause(k.RepoPath, k.Ref)
		}
	}
	if len(toEvict) > 0 {
		runtime.GC() // nudge GC so released graph objects are reclaimed promptly
	}

	// PH6: perform disk eviction for newly expired slots.
	if m.onDiskEvict != nil {
		for _, k := range toExpire {
			freed, err := m.onDiskEvict(k)
			if err != nil {
				m.logger.Printf("tier: expired-evict %s@%s FAILED: %v", k.RepoPath, k.Ref, err)
			} else {
				m.logger.Printf("tier: expired-evict %s@%s freed=%.1fMB", k.RepoPath, k.Ref, float64(freed)/(1024*1024))
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Default-branch detection
// ---------------------------------------------------------------------------

// IsDefaultBranch reports whether ref is the repo's default branch. It
// queries `git symbolic-ref --short HEAD` and the remote origin/HEAD, then
// falls back to checking "main" / "master" as a safe hardcoded backstop.
func IsDefaultBranch(repoPath, ref string) bool {
	if ref == "" {
		return false
	}
	// Current HEAD symbolic-ref.
	if head := gitmeta.RunGit(repoPath, "symbolic-ref", "--short", "HEAD"); head == ref {
		return true
	}
	// Remote default: "origin/main" → "main".
	if originHead := gitmeta.RunGit(repoPath, "rev-parse", "--abbrev-ref", "origin/HEAD"); originHead != "" {
		if after, found := strings.CutPrefix(originHead, "origin/"); found && after == ref {
			return true
		}
	}
	// Hardcoded fallback: always pin "main" and "master".
	return ref == "main" || ref == "master"
}
