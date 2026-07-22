// Package mcp hosts daemon-internal helpers that serve MCP queries
// against lazily mmap'd graph.fb files (ADR-0017 Phase D / ADR-0016).
//
// The cache is the single coordination point between the indexer (which
// writes a fresh graph.fb on every successful index pass) and the MCP
// query handlers (which want zero-copy reads). It does NOT load graphs
// eagerly; the first query that targets a repo opens the mmap, and an
// LRU bound caps the number of resident handles.
//
// PH1c (epic #2087): the cache key is now the absolute graph.fb path,
// which already encodes (repoPath, ref) via the per-ref store layout
// introduced in PH1a. GetForRepoRef resolves the canonical fbPath using
// daemon.StateDirForRepoRef so callers can address a specific ref without
// reconstructing the path themselves.
package mcp

import (
	"container/list"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/cajasmota/grafel/internal/daemon"
	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/graph/fbreader"
	"github.com/cajasmota/grafel/internal/graph/fbversion"
)

// DefaultCapacity is the default maximum number of simultaneously
// mmap'd graph.fb files. Each handle pins one mmap region (sized by the
// page-cache, not the resident set), so the cap is a soft control on
// daemon RSS rather than a hard byte budget.
const DefaultCapacity = 10

// ErrCacheClosed is returned by Get / Invalidate after Close.
var ErrCacheClosed = errors.New("graph cache: closed")

// ErrUnknownRef is returned by GetForRepoRef when the resolved path
// falls under the refs/_unknown/ sentinel directory. Callers should
// treat this as "no graph available for this ref" rather than an error
// worth logging loudly. Loading _unknown graphs eagerly at startup was
// identified as root-cause D in #2141: the stale pre-PH1b artifact
// consumes heap while the live ref's graph is also resident, doubling
// the effective working set. By refusing to auto-load _unknown paths,
// callers are forced to resolve the real HEAD ref before querying.
//
// _unknown paths can still be loaded via the direct Get(path) call if
// a caller knows exactly what it wants; this guard only applies to the
// ref-aware entry point.
var ErrUnknownRef = errors.New("graph cache: refusing to load refs/_unknown sentinel")

// Entry holds one open mmap handle plus the mtime captured when it was
// opened. The mtime lets the daemon detect stale handles after an
// out-of-band write (e.g. someone reran `grafel index` manually);
// the explicit Invalidate hook from the scheduler is the primary path,
// mtime check is the belt-and-braces fallback.
type Entry struct {
	Path   string
	Reader *fbreader.Reader

	mtime int64 // unix nano of the underlying graph.fb at open time
	refs  int32 // outstanding Borrow callers; eviction waits for zero
}

// AccessHook is an optional callback invoked after a successful
// GetForRepoRef call. It receives (repoPath, ref) so the PH2 tier manager
// can update lastAccessedAt without creating an import cycle between
// internal/daemon/mcp and internal/daemon/tier. cmd/grafel wires them
// together via SetAccessHook.
type AccessHook func(repoPath, ref string)

// Cache is a concurrent-safe LRU of mmap'd graph.fb readers, keyed by
// absolute graph.fb path. Multiple goroutines may hold readers for the
// same path simultaneously (FlatBuffers reads are pure); the LRU evicts
// the least-recently-used handle when a new path crosses the capacity.
type Cache struct {
	mu       sync.Mutex
	capacity int
	// LRU: front = MRU, back = LRU.
	order *list.List
	// path -> *list.Element (whose Value is *Entry).
	entries map[string]*list.Element
	closed  atomic.Bool

	// Stats — read with Stats() for benches/observability.
	stats Stats

	// accessHook is the optional PH2 idle-tracker callback. Set via
	// SetAccessHook; nil means no-op. Guarded by accessHookMu.
	accessHookMu sync.RWMutex
	accessHook   AccessHook
}

// SetAccessHook registers (or replaces) the PH2 idle-tracker callback.
// Safe to call at any time; the previous hook is discarded atomically.
func (c *Cache) SetAccessHook(h AccessHook) {
	c.accessHookMu.Lock()
	c.accessHook = h
	c.accessHookMu.Unlock()
}

func (c *Cache) fireAccessHook(repoPath, ref string) {
	c.accessHookMu.RLock()
	h := c.accessHook
	c.accessHookMu.RUnlock()
	if h != nil {
		h(repoPath, ref)
	}
}

// Stats captures cumulative cache behaviour. Snapshots are returned by
// value; field meaning matches the standard cache-stat vocabulary.
type Stats struct {
	Hits        int64
	Misses      int64
	Evictions   int64
	Invalidates int64
	Opens       int64
	OpenErrors  int64
}

// NewCache builds an empty cache with the given capacity. capacity <= 0
// falls back to DefaultCapacity.
func NewCache(capacity int) *Cache {
	if capacity <= 0 {
		capacity = DefaultCapacity
	}
	return &Cache{
		capacity: capacity,
		order:    list.New(),
		entries:  make(map[string]*list.Element),
	}
}

// Get returns a usable *fbreader.Reader for path, opening + mmap'ing
// the file if it is not already cached. The caller MUST invoke the
// returned release function exactly once when done (deferred is the
// idiomatic pattern); the release decrements the refcount so eviction
// can reclaim the handle.
//
// Stale entries (mtime drift) are transparently reopened.
func (c *Cache) Get(path string) (*fbreader.Reader, func(), error) {
	if c.closed.Load() {
		return nil, func() {}, ErrCacheClosed
	}

	c.mu.Lock()

	if elem, ok := c.entries[path]; ok {
		ent := elem.Value.(*Entry)
		// Detect on-disk replacement (out-of-band index or missed
		// invalidate). Compare mtime nanos; treat stat error as
		// "fine, keep using the cached handle".
		if info, err := os.Stat(path); err == nil {
			if info.ModTime().UnixNano() != ent.mtime {
				// Stale — evict in place. We must be careful not to
				// close a reader that another goroutine is currently
				// borrowing; tear-down waits until refs hits zero
				// inside removeLocked.
				c.removeLocked(elem)
				c.stats.Invalidates++
				goto open
			}
		}
		c.order.MoveToFront(elem)
		atomic.AddInt32(&ent.refs, 1)
		c.stats.Hits++
		c.mu.Unlock()
		return ent.Reader, c.releaser(ent), nil
	}

open:
	c.stats.Misses++
	c.mu.Unlock()

	// Open OUTSIDE the lock — mmap+initial read can take milliseconds
	// on a cold page cache and we don't want every other Get blocked.
	reader, mtime, err := openReader(path)
	if err != nil {
		c.mu.Lock()
		c.stats.OpenErrors++
		c.mu.Unlock()
		return nil, func() {}, err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	// Re-check: another goroutine may have raced us and inserted
	// already. If so, discard ours and use theirs.
	if elem, ok := c.entries[path]; ok {
		_ = reader.Close()
		ent := elem.Value.(*Entry)
		c.order.MoveToFront(elem)
		atomic.AddInt32(&ent.refs, 1)
		return ent.Reader, c.releaser(ent), nil
	}

	ent := &Entry{
		Path:   path,
		Reader: reader,
		mtime:  mtime,
		refs:   1,
	}
	elem := c.order.PushFront(ent)
	c.entries[path] = elem
	c.stats.Opens++

	// Evict until we're back within capacity.
	for c.order.Len() > c.capacity {
		victim := c.order.Back()
		if victim == nil {
			break
		}
		if victim == elem {
			// Don't evict the entry we just inserted; the cap is a
			// soft target. (Should be unreachable with cap >= 1.)
			break
		}
		c.removeLocked(victim)
		c.stats.Evictions++
	}

	return reader, c.releaser(ent), nil
}

// GetForRepoRef is a ref-aware entry point that resolves the canonical
// graph.fb path from (repoPath, ref) via daemon.StateDirForRepoRef and
// then delegates to Get. This is the preferred call-site for PH1c
// consumers: the cache key remains the absolute fbPath (which already
// encodes the ref via the per-ref store layout), so hit/miss semantics
// are unchanged. When ref is "" the resolved path is
// refs/_unknown/graph.fb — same sentinel as StateDirForRepo.
//
// Fix #2141 root-cause D: when the resolved path falls under
// refs/_unknown/, GetForRepoRef returns ErrUnknownRef without touching
// the cache. This prevents the stale pre-PH1b sentinel artifact from
// being loaded into heap while a valid per-ref graph is already resident
// (which would double steady-state RSS). Callers should resolve the
// real HEAD ref before calling and fall back to re-indexing on ErrUnknownRef.
// On-demand load via Get(path) still works for callers that know what they
// want; only the ref-aware entry point refuses to load _unknown paths.
//
// PH2 (#2090): fires the AccessHook (if set) with (repoPath, ref) so the
// tier manager can update lastAccessedAt for idle-TTL tracking.
//
// Returns (nil, noop, ErrCacheClosed) when the cache has been closed.
// Returns (nil, noop, ErrUnknownRef) when ref resolves to refs/_unknown/.
func (c *Cache) GetForRepoRef(repoPath, ref string) (*fbreader.Reader, func(), error) {
	stateDir := daemon.StateDirForRepoRef(repoPath, ref)
	// Refuse to eagerly load the _unknown sentinel into heap (#2141 root-cause D).
	// The sentinel dir is created for repos indexed before PH1b; its graph.fb is
	// stale once a real-ref graph exists. Callers must resolve HEAD before calling.
	if strings.Contains(filepath.ToSlash(stateDir), "/refs/_unknown") {
		return nil, func() {}, ErrUnknownRef
	}
	// #5891: resolve the active generation via the `current` pointer (flat
	// graph.fb fallback for un-migrated repos). The cache key remains the
	// absolute resolved path, so hit/miss semantics are unchanged; a gen swap
	// yields a new key → cache miss → fresh mmap of the new generation.
	fbPath := graph.CurrentGraphPath(stateDir)
	r, release, err := c.Get(fbPath)
	if err == nil {
		c.fireAccessHook(repoPath, ref)
	}
	return r, release, err
}

// InvalidateForRepoRef drops the cached handle for (repoPath, ref).
// Returns true when an entry was actually evicted. Callers that only
// know the ref should prefer this over Invalidate(path).
func (c *Cache) InvalidateForRepoRef(repoPath, ref string) bool {
	stateDir := daemon.StateDirForRepoRef(repoPath, ref)
	// #5891: under the gen layout the resident handle may be keyed by any
	// graph.<gen>.fb (or the legacy flat graph.fb), and after a reindex flips
	// the pointer to a NEW gen the OLD gen's entry is keyed by a path we can no
	// longer reconstruct from the current pointer. Evict every handle whose
	// parent dir is this state dir so the stale generation's mmap is reclaimed
	// promptly (the low-RSS invariant) rather than lingering until LRU.
	return c.InvalidateDir(stateDir) > 0
}

// InvalidateDir evicts every cached reader whose file lives directly in dir
// (i.e. filepath.Dir(path) == dir), returning the count removed. It is the
// gen-layout-safe invalidation entry point (#5891): a reindex writes a new
// graph.<gen>.fb and flips the pointer, so the previously-resident handle is
// keyed by a now-superseded gen path; evicting by directory drops it without
// needing to know the exact old gen number. Safe to call on a closed cache
// (returns 0).
func (c *Cache) InvalidateDir(dir string) int {
	if c.closed.Load() {
		return 0
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	n := 0
	for p, elem := range c.entries {
		if filepath.Dir(p) == dir {
			c.removeLocked(elem)
			c.stats.Invalidates++
			n++
		}
	}
	return n
}

// Invalidate forces removal of the cached handle for path. Called by
// the scheduler after a successful reindex so the next Get reopens
// against the freshly written graph.fb. Returns true when an entry was
// actually evicted.
func (c *Cache) Invalidate(path string) bool {
	if c.closed.Load() {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	elem, ok := c.entries[path]
	if !ok {
		return false
	}
	c.removeLocked(elem)
	c.stats.Invalidates++
	return true
}

// Close releases every cached handle and refuses further Get calls.
func (c *Cache) Close() error {
	if !c.closed.CompareAndSwap(false, true) {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	var firstErr error
	for elem := c.order.Front(); elem != nil; elem = elem.Next() {
		ent := elem.Value.(*Entry)
		if err := ent.Reader.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	c.order.Init()
	c.entries = map[string]*list.Element{}
	return firstErr
}

// Stats returns a snapshot of cumulative counters.
func (c *Cache) Stats() Stats {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.stats
}

// Len reports the current number of resident handles.
func (c *Cache) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.entries)
}

// removeLocked deletes elem from order+entries and closes its reader.
// Caller MUST hold c.mu. We do not block on outstanding refs — the
// fbreader.Reader is read-only and FlatBuffers offsets are valid until
// the underlying mmap is released; closing while another goroutine is
// mid-decode risks SIGBUS only if the kernel reclaims the pages
// immediately. In practice the page cache outlives the brief decode
// window. If this ever bites, swap to an epoch-based reclaim.
func (c *Cache) removeLocked(elem *list.Element) {
	ent := elem.Value.(*Entry)
	c.order.Remove(elem)
	delete(c.entries, ent.Path)
	_ = ent.Reader.Close()
}

func (c *Cache) releaser(ent *Entry) func() {
	return func() { atomic.AddInt32(&ent.refs, -1) }
}

// openReader stats the path, opens the FlatBuffers reader, and returns
// both alongside the file's mtime in unix nanoseconds.
//
// #5907 — this zero-copy serve path is version-GATED, mirroring the rejection
// in internal/graph/load.go's loadFBDoc. Two crash-prevention guards live here,
// both of which the ungated fbreader.Open call previously lacked:
//
//  1. A recovered open (openReaderRecover): fbreader.Open recovers-then-
//     RE-PANICS on a malformed/incompatible flatbuffer vtable (see
//     fbreader/reader.go). A breaking on-disk cut — the #5890 segmented format
//     read by a binary that predates it, or a genuinely corrupt buffer — would
//     otherwise re-panic straight out of this cache and crash the daemon. We
//     recover it into a plain error so the cache degrades to a miss.
//
//  2. A format-version gate: if the on-disk FormatVersion is below what this
//     binary supports, we Close the reader and return the SAME typed
//     *graph.FormatVersionError loadFBDoc returns — so callers can errors.As it
//     and degrade to "serve empty + reindex-required" (mirroring the detection
//     in internal/mcp/state.go), and so we NEVER cache or serve a stale-format
//     reader (Get discards the handle on any openReader error before inserting
//     it into the LRU). This is the reader-side half of the same migration the
//     status-plane's ReindexRequiredReason flag reports.
func openReader(path string) (*fbreader.Reader, int64, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, 0, fmt.Errorf("graph cache: stat %s: %w", path, err)
	}
	r, err := openAndGate(path)
	if err != nil {
		return nil, 0, err
	}
	return r, info.ModTime().UnixNano(), nil
}

// openAndGate opens path and enforces the format-version gate, returning a
// usable reader only for an on-disk format this binary can serve. It is the
// crash-prevention seam for the zero-copy MCP cache (#5907):
//
//   - A single recover covers BOTH fbreader.Open (which recovers-then-
//     re-panics on a malformed/incompatible flatbuffer vtable) AND the
//     r.Version() scalar read that follows — so neither a corrupt buffer nor a
//     breaking on-disk cut (the #5890 segmented format read by an older binary)
//     can re-panic out of the cache and crash the daemon. A recovered panic
//     becomes a plain error and the reader (if any) is Closed to free its mmap.
//   - The format-version gate mirrors internal/graph/load.go's loadFBDoc: an
//     on-disk FormatVersion below fbversion.Version yields the SAME typed
//     *graph.FormatVersionError, Closed before return so we neither cache nor
//     leak a stale-format handle. Callers can errors.As it and degrade to
//     "serve empty + reindex-required" (mirroring internal/mcp/state.go's
//     detection), while the engine's status writer independently flags the
//     repo and auto-enqueues its migration reindex.
func openAndGate(path string) (r *fbreader.Reader, err error) {
	defer func() {
		if rec := recover(); rec != nil {
			if r != nil {
				_ = r.Close()
				r = nil
			}
			err = fmt.Errorf("graph cache: recovered panic opening %s: %v", path, rec)
		}
	}()
	r, err = fbreader.Open(path)
	if err != nil {
		return nil, err
	}
	if v := r.Version(); v < fbversion.Version {
		_ = r.Close()
		return nil, fmt.Errorf("graph cache: %w",
			&graph.FormatVersionError{Found: v, Required: fbversion.Version})
	}
	return r, nil
}
