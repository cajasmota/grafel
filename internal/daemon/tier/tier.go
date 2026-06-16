// Package tier implements the HOT/WARM/COLD/EXPIRED state machine for loaded
// graphs (PH2 of epic #2087 / issue #2090, extended by PH3 #2091, PH6 #2094,
// S1 #2151, P0.3 #2141).
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
// P0.3 (#2141): pressure-driven eviction. When total heap allocation (tracked
// via runtime.MemStats.HeapInuse) exceeds a configurable fraction of system
// memory (default 60%, tunable via GRAFEL_HEAP_MAX_PCT), the scanner
// immediately evicts the oldest-touched HOT/WARM slots to COLD, independent
// of their TTL. Pinned-main slots are exempt from pressure eviction; they
// degrade to WARM instead.
//
// PH6 (#2094): when a slot reaches EXPIRED the Manager calls the optional
// DiskEvictCallback which deletes the graph artifacts from disk.
// Pinned main branches (default branch of a registered repo) are
// disk-pinned: they can WARM→COLD but never COLD→EXPIRED.
//
// S1 (#2151): boot-time lazy hydration. Groups are registered at COLD tier
// on daemon startup (walk registry, REGISTER paths, do not open graph.fb).
// Dashboard /api/v2/groups returns tier=cold, entities=0 until first detail
// query triggers the on-demand cold-wake path.
//
// # Env-tunable TTLs
//
//	GRAFEL_TIER_HOT_MINUTES           default 5
//	GRAFEL_TIER_COLD_MINUTES          default 60
//	GRAFEL_TIER_COLD_MINUTES_WORKTREE default 30
//	GRAFEL_TIER_EXPIRED_DAYS          default 7  (feature branches)
//	GRAFEL_TIER_EXPIRED_DAYS_WORKTREE default 2
//	GRAFEL_HEAP_MAX_PCT               default 60  (P0.3 pressure threshold)
package tier

import (
	"context"
	"log/slog"
	"os"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/cajasmota/grafel/internal/gitmeta"
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

	// HeapMaxPct is the fraction of system memory (0–100) at which pressure-
	// driven eviction kicks in (P0.3 #2141). Default 60. When HeapInuse exceeds
	// HeapMaxPct% of system physical memory, the oldest HOT/WARM slots are evicted
	// to COLD regardless of their TTL. Pinned-main slots are exempt.
	// 0 disables pressure eviction entirely.
	HeapMaxPct int

	// SystemMemoryBytes is the total physical memory in bytes used to compute the
	// pressure threshold. 0 means "read from runtime.MemStats at check time".
	// Overridable in tests to avoid real sysinfo calls.
	SystemMemoryBytes uint64
}

// DefaultTTLConfig returns the spec's production defaults.
func DefaultTTLConfig() TTLConfig {
	return TTLConfig{
		HotWindow:             5 * time.Minute,
		ColdWindow:            60 * time.Minute,
		ColdWindowWorktree:    30 * time.Minute,
		ExpiredWindow:         7 * 24 * time.Hour,
		ExpiredWindowWorktree: 48 * time.Hour,
		HeapMaxPct:            60,
	}
}

// EnvTTLConfig reads env-var overrides on top of DefaultTTLConfig.
func EnvTTLConfig() TTLConfig {
	cfg := DefaultTTLConfig()
	if v := envMinutes("GRAFEL_TIER_HOT_MINUTES"); v > 0 {
		cfg.HotWindow = v
	}
	if v := envMinutes("GRAFEL_TIER_COLD_MINUTES"); v > 0 {
		cfg.ColdWindow = v
	}
	if v := envMinutes("GRAFEL_TIER_COLD_MINUTES_WORKTREE"); v > 0 {
		cfg.ColdWindowWorktree = v
	}
	if v := envDays("GRAFEL_TIER_EXPIRED_DAYS"); v > 0 {
		cfg.ExpiredWindow = v
	}
	if v := envDays("GRAFEL_TIER_EXPIRED_DAYS_WORKTREE"); v > 0 {
		cfg.ExpiredWindowWorktree = v
	}
	if v := envInt("GRAFEL_HEAP_MAX_PCT"); v > 0 {
		cfg.HeapMaxPct = v
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

func envInt(name string) int {
	s := os.Getenv(name)
	if s == "" {
		return 0
	}
	n, err := strconv.Atoi(s)
	if err != nil || n <= 0 {
		return 0
	}
	return n
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
	logger      *slog.Logger
	clock       func() time.Time
	// watcher is optional (PH2a #2096). When non-nil, Pause is called on
	// WARM→COLD and Resume is called on COLD→HOT.
	watcher WatcherHook

	// sysMemFn returns total physical memory bytes for pressure-eviction
	// threshold calculation (P0.3 #2141). Defaults to readSysMemBytes.
	// Overridable in tests.
	sysMemFn func() uint64

	// heapFn returns the current in-heap allocated bytes (HeapInuse from
	// runtime.MemStats). Overridable in tests to simulate pressure.
	heapFn func() uint64
}

const defaultScanInterval = 30 * time.Second

// NewManager creates and starts a Manager. The scanner runs until ctx is
// cancelled. onEvict and reload must not be nil. onDiskEvict may be nil
// (disables disk deletion).
func NewManager(ctx context.Context, ttl TTLConfig, onEvict EvictionCallback, reload ReloadCallback, onDiskEvict DiskEvictCallback, logger *slog.Logger) *Manager {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(os.Stderr, nil)).With("pkg", "tier")
	}
	m := &Manager{
		slots:       make(map[SlotKey]*slot),
		ttl:         ttl,
		onEvict:     onEvict,
		reload:      reload,
		onDiskEvict: onDiskEvict,
		logger:      logger,
		clock:       time.Now,
		sysMemFn:    readSysMemBytes,
		heapFn:      readHeapInuse,
	}
	go m.scanLoop(ctx, defaultScanInterval)
	return m
}

// NewManagerForTest creates a Manager without starting the scan loop, using a
// caller-supplied clock. Call Scan() explicitly to trigger transitions.
// onDiskEvict may be nil.
func NewManagerForTest(ttl TTLConfig, clock func() time.Time, onEvict EvictionCallback, reload ReloadCallback) *Manager {
	return &Manager{
		slots:    make(map[SlotKey]*slot),
		ttl:      ttl,
		onEvict:  onEvict,
		reload:   reload,
		logger:   slog.New(slog.NewTextHandler(os.Stderr, nil)).With("pkg", "tier"),
		clock:    clock,
		sysMemFn: readSysMemBytes,
		heapFn:   readHeapInuse,
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
		logger:      slog.New(slog.NewTextHandler(os.Stderr, nil)).With("pkg", "tier"),
		clock:       clock,
		sysMemFn:    readSysMemBytes,
		heapFn:      readHeapInuse,
	}
}

// NewManagerForTestWithHeap is like NewManagerForTest but also accepts
// custom heap and system-memory probe functions for pressure-eviction tests (P0.3).
func NewManagerForTestWithHeap(ttl TTLConfig, clock func() time.Time, onEvict EvictionCallback, reload ReloadCallback, heapFn func() uint64, sysMemFn func() uint64) *Manager {
	return &Manager{
		slots:    make(map[SlotKey]*slot),
		ttl:      ttl,
		onEvict:  onEvict,
		reload:   reload,
		logger:   slog.New(slog.NewTextHandler(os.Stderr, nil)).With("pkg", "tier"),
		clock:    clock,
		sysMemFn: sysMemFn,
		heapFn:   heapFn,
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

// RegisterCold declares a slot at COLD tier without loading any graph into
// memory. Used by S1 (#2151) lazy-hydration boot path: registry groups are
// walked at startup, paths are registered so the tier manager knows they
// exist, but graph.fb is NOT opened until the first MCP query triggers
// Touch → cold-wake. If the slot already exists it is left at its current
// tier (to avoid evicting a HOT slot that was freshly indexed).
func (m *Manager) RegisterCold(key SlotKey, isPinnedMain bool, kind SlotKind) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.slots[key]; ok {
		// Slot already known — don't downgrade a live HOT/WARM slot.
		return
	}
	m.slots[key] = &slot{
		tier:           TierCold,
		kind:           kind,
		lastAccessedAt: m.clock(),
		isPinnedMain:   isPinnedMain,
	}
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
			m.logger.Info("tier: cold-wake watcher resumed", "repo", key.RepoPath, "ref", key.Ref, "took", resumeElapsed.Round(time.Millisecond))
		}

		if err := m.reload(key); err != nil {
			m.logger.Error("tier: cold-load failed", "repo", key.RepoPath, "ref", key.Ref, "err", err)
			return err
		}
		m.mu.Lock()
		if s2, ok2 := m.slots[key]; ok2 {
			s2.tier = TierHot
			s2.lastAccessedAt = m.clock()
		}
		m.mu.Unlock()
		m.logger.Info("tier: cold-load OK → HOT", "repo", key.RepoPath, "ref", key.Ref)
	}
	return nil
}

// Forget removes every slot for repoPath (all refs) from the Manager, dropping
// them from the in-memory accounting WITHOUT firing eviction/disk callbacks.
// It is the tier-side half of the vanished-repo reaper (issue #3680): once a
// repo's directory no longer exists on disk, its slots must be untracked so the
// Manager stops counting them toward pressure and stops attempting cold-wakes
// against a path that is gone. The on-disk store is removed separately by the
// reaper. Returns the number of slots forgotten.
func (m *Manager) Forget(repoPath string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for k := range m.slots {
		if k.RepoPath == repoPath {
			delete(m.slots, k)
			n++
		}
	}
	return n
}

// ForgetRef removes the single slot for (repoPath, ref) from the Manager,
// dropping it from the in-memory accounting WITHOUT firing eviction/disk
// callbacks. It is the tier-side half of the dead-ref reaper (issue #5236):
// once a branch is deleted or a worktree is removed, its stored ref must be
// untracked so the Manager stops counting it toward pressure and stops
// attempting cold-wakes against a ref that is gone. The on-disk store dir and
// the cached mmap reader are released separately by the dead-ref sweeper.
// Returns true when a slot was actually present and removed.
func (m *Manager) ForgetRef(repoPath, ref string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := SlotKey{RepoPath: repoPath, Ref: ref}
	if _, ok := m.slots[key]; !ok {
		return false
	}
	delete(m.slots, key)
	return true
}

// Len returns the number of slots currently tracked. Used by the reaper's
// memory-accounting assertions and diagnostics.
func (m *Manager) Len() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.slots)
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
				m.logger.Info("tier: HOT→WARM", "repo", k.RepoPath, "ref", k.Ref, "idle", idle.Round(time.Second), "kind", s.kind)
			}
		case TierWarm:
			if idle >= coldWindow {
				s.tier = TierCold
				toEvict = append(toEvict, k)
				m.logger.Info("tier: WARM→COLD", "repo", k.RepoPath, "ref", k.Ref, "idle", idle.Round(time.Second), "kind", s.kind)
			}
		case TierCold:
			// PH6 (#2094): COLD→EXPIRED — delete disk artifacts when not pinned.
			if !s.isPinnedMain && idle >= expiredWindow {
				s.tier = TierExpired
				toExpire = append(toExpire, k)
				m.logger.Info("tier: COLD→EXPIRED", "repo", k.RepoPath, "ref", k.Ref, "idle", idle.Round(time.Hour), "kind", s.kind)
			}
		}
	}
	m.mu.Unlock()

	// P0.3 (#2141): pressure-driven eviction — check heap usage after TTL scan.
	// This runs outside the lock so it can call m.onEvict safely.
	pressureEvicted := m.scanPressureEvict()

	// PH2a: release in-memory graphs for newly-COLD slots but keep the
	// fsnotify subscription alive so file edits during the COLD window still
	// trigger reindex. (#2645: removing the subscription here caused TS/TSX
	// changes to go undetected when a repo had been idle for >60 min.)
	wh := m.watcher // read under mu already released; field is write-once after init
	for _, k := range toEvict {
		m.onEvict(k)
		// wh.Pause intentionally NOT called here — subscription removal is
		// deferred to the COLD→EXPIRED path below so that watcher events keep
		// firing while the graph is cold-but-still-on-disk.
	}
	if len(toEvict) > 0 || pressureEvicted > 0 {
		runtime.GC() // nudge GC so released graph objects are reclaimed promptly
	}

	// PH6: perform disk eviction for newly expired slots.
	// Only EXPIRED slots lose their fsnotify subscription — at that point the
	// graph.fb has been deleted and there is nothing to reindex into.
	for _, k := range toExpire {
		if wh != nil {
			wh.Pause(k.RepoPath, k.Ref)
		}
		if m.onDiskEvict != nil {
			freed, err := m.onDiskEvict(k)
			if err != nil {
				m.logger.Error("tier: expired-evict FAILED", "repo", k.RepoPath, "ref", k.Ref, "err", err)
			} else {
				m.logger.Info("tier: expired-evict freed", "repo", k.RepoPath, "ref", k.Ref, "freed_mb", float64(freed)/(1024*1024))
			}
		}
	}
}

// scanPressureEvict checks heap usage and evicts the oldest HOT/WARM slots
// when the pressure threshold is exceeded (P0.3 #2141). Returns the number
// of slots evicted. Pinned-main slots are exempt.
func (m *Manager) scanPressureEvict() int {
	if m.ttl.HeapMaxPct <= 0 {
		return 0
	}

	// Sample heap and system memory.
	heapBytes := m.heapFn()
	sysBytes := m.ttl.SystemMemoryBytes
	if sysBytes == 0 {
		sysBytes = m.sysMemFn()
	}
	if sysBytes == 0 {
		return 0
	}

	threshold := uint64(m.ttl.HeapMaxPct) * sysBytes / 100
	if heapBytes < threshold {
		return 0
	}
	heapMB := float64(heapBytes) / (1024 * 1024)
	m.logger.Info("tier: pressure-evict triggered", "heap_mb", heapMB, "threshold_pct", m.ttl.HeapMaxPct, "sys_mb", float64(sysBytes)/(1024*1024))

	// Collect all evictable HOT/WARM slots sorted oldest-accessed first.
	// Pinned-main slots are excluded.
	type candidate struct {
		key            SlotKey
		lastAccessedAt time.Time
	}
	m.mu.Lock()
	candidates := make([]candidate, 0, len(m.slots))
	for k, s := range m.slots {
		if s.tier != TierHot && s.tier != TierWarm {
			continue
		}
		if s.isPinnedMain {
			continue
		}
		candidates = append(candidates, candidate{key: k, lastAccessedAt: s.lastAccessedAt})
	}
	// Sort oldest-accessed first (evict LRU).
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].lastAccessedAt.Before(candidates[j].lastAccessedAt)
	})
	// Evict up to half the eligible slots to reduce pressure without
	// over-evicting (avoids thrash on the next cold-wake cycle).
	evictCount := (len(candidates) + 1) / 2
	if evictCount == 0 {
		m.mu.Unlock()
		return 0
	}
	toEvict := make([]SlotKey, 0, evictCount)
	for i := 0; i < evictCount; i++ {
		k := candidates[i].key
		s := m.slots[k]
		oldTier := s.tier.String()
		s.tier = TierCold
		toEvict = append(toEvict, k)
		m.logger.Info("tier: pressure-evict →COLD", "repo", k.RepoPath, "ref", k.Ref, "from", oldTier, "reason", "heap_threshold", "heap_mb", heapMB)
	}
	m.mu.Unlock()

	// Evict in-memory graphs only; do NOT remove fsnotify subscriptions on
	// pressure-driven WARM→COLD (same reasoning as the TTL scan: #2645).
	// Subscriptions are only removed when a slot reaches EXPIRED and its
	// graph.fb is deleted from disk.
	for _, k := range toEvict {
		m.onEvict(k)
	}
	return len(toEvict)
}

// ---------------------------------------------------------------------------
// Default-branch detection
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// Heap / memory helpers (P0.3)
// ---------------------------------------------------------------------------

// readHeapInuse returns the current value of runtime.MemStats.HeapInuse —
// the bytes in in-use heap spans. This is a proxy for "graph objects currently
// reachable" and updates on each GC cycle. Production sysMemFn.
func readHeapInuse() uint64 {
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	return ms.HeapInuse
}

// readSysMemBytes returns total physical memory in bytes. Uses
// runtime.MemStats.Sys as a conservative lower-bound when the OS-level
// total is unavailable (no syscall needed; always present).
// For production accuracy, cmd/grafel may override via TTLConfig.SystemMemoryBytes.
func readSysMemBytes() uint64 {
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	// Sys is the total memory obtained from the OS; it's a reasonable proxy
	// for physical memory when we can't call sysinfo directly.
	return ms.Sys
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
