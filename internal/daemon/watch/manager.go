// Package watch — WatcherManager: pause/resume fsnotify subscriptions per
// (repoPath, ref) when a tier slot transitions HOT/WARM → COLD or vice-versa.
//
// PH2a of epic #2087 (#2096).
//
// Design: the daemon uses a single shared fsnotify Watcher that subscribes
// entire repo trees. "Pausing" a slot means removing that repo's directory
// tree from the fsnotify subscription and recording it in the paused set.
// "Resuming" re-adds it. Because the Watcher already owns
// AddRepo/RemoveRepo, we delegate to those APIs rather than introducing a
// second Watcher instance.
//
// A single repoPath may be referenced by multiple refs (branches). The
// fsnotify subscription is per-repo-path, not per-ref. We therefore track a
// reference count: the subscription stays alive as long as at least one ref
// for the repo is not paused. The subscription is removed only when all refs
// for a repo are paused, and restored when the first ref for that repo is
// resumed.
package watch

import (
	"log"
	"os"
	"sync"
	"time"
)

// Manager is the narrow interface the tier package uses to pause/resume
// per-(repoPath,ref) watcher subscriptions. Implemented by DefaultManager.
type Manager interface {
	// Pause marks (repoPath, ref) as paused. When this call causes all refs
	// for the given repoPath to become paused, the fsnotify subscription for
	// that repo tree is removed.
	Pause(repoPath, ref string)
	// Resume marks (repoPath, ref) as active. When the repo was fully
	// unsubscribed (all refs paused), the fsnotify subscription is re-added.
	// Returns the time taken for the resume operation.
	Resume(repoPath, ref string) time.Duration
	// IsPaused reports whether (repoPath, ref) is currently paused.
	IsPaused(repoPath, ref string) bool
	// ActiveCount returns the number of (repoPath, ref) pairs currently active.
	ActiveCount() int
	// PausedCount returns the number of (repoPath, ref) pairs currently paused.
	PausedCount() int
}

// slotState tracks per-ref pause state.
type slotState struct {
	paused bool
}

// DefaultManager wraps a *Watcher and implements Manager.
// It is goroutine-safe.
type DefaultManager struct {
	watcher *Watcher
	logger  *log.Logger

	mu     sync.Mutex
	// slots maps "repoPath\x00ref" → slotState
	slots  map[string]*slotState
	// refCounts maps repoPath → number of active (non-paused) refs
	refCounts map[string]int
}

// NewDefaultManager creates a DefaultManager backed by w.
// logger may be nil.
func NewDefaultManager(w *Watcher, logger *log.Logger) *DefaultManager {
	if logger == nil {
		logger = log.New(os.Stderr, "watch-mgr: ", log.LstdFlags)
	}
	return &DefaultManager{
		watcher:   w,
		logger:    logger,
		slots:     make(map[string]*slotState),
		refCounts: make(map[string]int),
	}
}

func slotKey(repoPath, ref string) string {
	return repoPath + "\x00" + ref
}

// Register declares that (repoPath, ref) is actively watched. This is
// called by the server at startup for every repo it subscribes. It
// initialises the ref-count bookkeeping without touching the fsnotify
// subscription (AddRepo was already called by the boot path).
func (m *DefaultManager) Register(repoPath, ref string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	k := slotKey(repoPath, ref)
	if _, exists := m.slots[k]; exists {
		return // idempotent
	}
	m.slots[k] = &slotState{paused: false}
	m.refCounts[repoPath]++
}

// Pause marks (repoPath, ref) paused. If this is the last active ref for
// the repo, the fsnotify subscription is removed.
func (m *DefaultManager) Pause(repoPath, ref string) {
	start := time.Now()
	m.mu.Lock()
	k := slotKey(repoPath, ref)
	st, known := m.slots[k]
	if !known {
		st = &slotState{}
		m.slots[k] = st
	}
	if st.paused {
		m.mu.Unlock()
		return // already paused — idempotent
	}
	st.paused = true
	m.refCounts[repoPath]--
	removeRepo := m.refCounts[repoPath] <= 0
	if removeRepo {
		m.refCounts[repoPath] = 0
	}
	m.mu.Unlock()

	if removeRepo {
		m.watcher.RemoveRepo(repoPath)
		m.logger.Printf("watcher-mgr: paused repo=%s ref=%s — fsnotify unsubscribed (elapsed %s)",
			repoPath, ref, time.Since(start).Round(time.Microsecond))
	} else {
		m.logger.Printf("watcher-mgr: paused ref=%s repo=%s — other refs still active",
			ref, repoPath)
	}
}

// Resume marks (repoPath, ref) active. If the repo was fully unsubscribed,
// the fsnotify subscription is re-added via AddRepo.
// Returns the time taken for the resume.
func (m *DefaultManager) Resume(repoPath, ref string) time.Duration {
	start := time.Now()
	m.mu.Lock()
	k := slotKey(repoPath, ref)
	st, known := m.slots[k]
	if !known {
		st = &slotState{}
		m.slots[k] = st
	}
	if !st.paused {
		m.mu.Unlock()
		return time.Since(start) // already active — idempotent
	}
	st.paused = false
	wasZero := m.refCounts[repoPath] == 0
	m.refCounts[repoPath]++
	m.mu.Unlock()

	if wasZero {
		// Re-subscribe.
		n, err := m.watcher.AddRepo(repoPath)
		elapsed := time.Since(start)
		if err != nil {
			m.logger.Printf("watcher-mgr: resume repo=%s ref=%s AddRepo err=%v (elapsed %s)",
				repoPath, ref, err, elapsed.Round(time.Microsecond))
		} else {
			m.logger.Printf("watcher-mgr: resumed repo=%s ref=%s dirs=%d (elapsed %s)",
				repoPath, ref, n, elapsed.Round(time.Microsecond))
		}
		return elapsed
	}
	m.logger.Printf("watcher-mgr: resumed ref=%s repo=%s — subscription already active",
		ref, repoPath)
	return time.Since(start)
}

// IsPaused reports whether (repoPath, ref) is currently paused.
func (m *DefaultManager) IsPaused(repoPath, ref string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	st, ok := m.slots[slotKey(repoPath, ref)]
	if !ok {
		return false
	}
	return st.paused
}

// ActiveCount returns the number of slot entries that are not paused.
func (m *DefaultManager) ActiveCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for _, st := range m.slots {
		if !st.paused {
			n++
		}
	}
	return n
}

// PausedCount returns the number of slot entries that are paused.
func (m *DefaultManager) PausedCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for _, st := range m.slots {
		if st.paused {
			n++
		}
	}
	return n
}
