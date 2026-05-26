// cross_link_cache.go — ref-keyed in-memory cache for cross-repo link
// candidates (issue #2224, closes the stale-cache bug exposed by the
// multi-branch surface introduced in epic #2098).
//
// # Problem
//
// The cross-repo candidate pipeline cross-matches entities from repo A against
// entities in repo B. When repo A switches from ref "main" to "feature/foo",
// A's entity set changes. Any cached candidate list derived from the (repoA,
// repoB) pair is now stale — A's new-ref entities may match different
// candidates than the old-ref entities.
//
// # Solution
//
// Cache key: (repoA, refA, repoB, refB) — a 4-tuple that binds each entry to
// the exact ref that was active for both repos at computation time.
//
// Secondary index: map[(repo,ref)] → []cacheKey so that, on a ref-switch event
// for any participating repo, invalidation is O(affected entries) rather than
// O(total cache size).
//
// Invalidation is synchronous: CrossLinkCache.InvalidateRepo(repo, oldRef)
// removes every cache entry whose key references (repo, oldRef). The next
// call to GetOrCompute for any pair involving the new ref will find a cache
// miss and recompute lazily.
//
// Thread safety: all cache operations are serialised through a single sync.Mutex.
package mcp

import (
	"sync"
)

// crossLinkKey is the 4-tuple cache key.
//
// When a group has more than 2 repos the same 4-tuple may appear from
// multiple calling paths; the cache deduplicates them.
type crossLinkKey struct {
	repoA string
	refA  string
	repoB string
	refB  string
}

// repoRefKey is the secondary-index key used to look up all cache entries
// that reference a (repo, ref) pair so they can be bulk-invalidated on a
// ref-switch event.
type repoRefKey struct {
	repo string
	ref  string
}

// CrossLinkCache is the in-memory ref-keyed cross-repo candidate cache.
//
// Cache entry lifecycle:
//   - On cache miss:    compute via ComputeFn, store result, update secondary index.
//   - On ref-switch:    InvalidateRepo(repo, oldRef) drops every entry whose
//     key references (repo, oldRef). Stale entries are never
//     returned; the secondary index prevents O(N) scans.
//   - On flush need:    Flush() removes all entries (e.g. on group reconfigure).
type CrossLinkCache struct {
	mu      sync.Mutex
	entries map[crossLinkKey][]CrossRepoLink // primary: key → computed result
	index   map[repoRefKey][]crossLinkKey    // secondary: (repo,ref) → keys
}

// NewCrossLinkCache constructs an empty CrossLinkCache.
func NewCrossLinkCache() *CrossLinkCache {
	return &CrossLinkCache{
		entries: make(map[crossLinkKey][]CrossRepoLink),
		index:   make(map[repoRefKey][]crossLinkKey),
	}
}

// GetOrCompute returns the cached candidate list for the given (repoA,
// refA, repoB, refB) tuple. On a miss, fn is called to compute the
// result, which is then stored in the cache.
//
// fn must be safe to call while the cache lock is NOT held — the cache
// releases its lock before calling fn to avoid holding it during
// potentially expensive graph operations. This means concurrent calls
// with the same key may both compute and store; the last write wins.
// This is acceptable because candidates are deterministic for a fixed
// (repo, ref) pair.
func (c *CrossLinkCache) GetOrCompute(
	repoA, refA, repoB, refB string,
	fn func() []CrossRepoLink,
) []CrossRepoLink {
	key := crossLinkKey{repoA, refA, repoB, refB}

	c.mu.Lock()
	if v, ok := c.entries[key]; ok {
		c.mu.Unlock()
		return v
	}
	c.mu.Unlock()

	// Compute without holding the lock.
	result := fn()

	c.mu.Lock()
	defer c.mu.Unlock()
	// Double-check in case a concurrent call already stored a result.
	if existing, ok := c.entries[key]; ok {
		return existing
	}
	c.entries[key] = result
	c.addToIndexLocked(key)
	return result
}

// Get returns the cached candidate list for the given tuple, or (nil, false)
// on a miss. Callers that prefer explicit miss handling can use this instead
// of GetOrCompute.
func (c *CrossLinkCache) Get(repoA, refA, repoB, refB string) ([]CrossRepoLink, bool) {
	key := crossLinkKey{repoA, refA, repoB, refB}
	c.mu.Lock()
	v, ok := c.entries[key]
	c.mu.Unlock()
	return v, ok
}

// Set stores a result unconditionally. Used by callers that computed the
// result outside GetOrCompute (e.g. after a links-pass completes).
func (c *CrossLinkCache) Set(repoA, refA, repoB, refB string, links []CrossRepoLink) {
	key := crossLinkKey{repoA, refA, repoB, refB}
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, exists := c.entries[key]; !exists {
		c.addToIndexLocked(key)
	}
	c.entries[key] = links
}

// InvalidateRepo removes every cache entry whose key references the
// (repo, ref) pair supplied. This is the hook called by the daemon when
// a BranchSwitchEvent arrives for a participating repo.
//
// Complexity: O(k) where k is the number of cached pairs involving
// (repo, ref) — typically a small constant. The secondary index makes
// this O(1) in the common case of a single cross-repo pair per group.
//
// Returns the number of cache entries evicted.
func (c *CrossLinkCache) InvalidateRepo(repo, ref string) int {
	if repo == "" || ref == "" {
		return 0
	}
	rk := repoRefKey{repo, ref}

	c.mu.Lock()
	defer c.mu.Unlock()

	keys, ok := c.index[rk]
	if !ok {
		return 0
	}

	evicted := 0
	for _, k := range keys {
		if _, exists := c.entries[k]; exists {
			delete(c.entries, k)
			evicted++
		}
		// Also remove the sibling (repoB,refB) reverse-index entry so the
		// secondary index stays compact.
		if k.repoA == repo && k.refA == ref {
			sibling := repoRefKey{k.repoB, k.refB}
			c.removeKeyFromIndexLocked(sibling, k)
		} else {
			sibling := repoRefKey{k.repoA, k.refA}
			c.removeKeyFromIndexLocked(sibling, k)
		}
	}
	delete(c.index, rk)
	return evicted
}

// Len returns the number of entries currently in the cache.
func (c *CrossLinkCache) Len() int {
	c.mu.Lock()
	n := len(c.entries)
	c.mu.Unlock()
	return n
}

// Flush removes all entries and resets the secondary index.
func (c *CrossLinkCache) Flush() {
	c.mu.Lock()
	c.entries = make(map[crossLinkKey][]CrossRepoLink)
	c.index = make(map[repoRefKey][]crossLinkKey)
	c.mu.Unlock()
}

// ── internal helpers ─────────────────────────────────────────────────────────

// addToIndexLocked registers key in the secondary index for both its A and B
// (repo,ref) components. MUST be called with c.mu held.
func (c *CrossLinkCache) addToIndexLocked(key crossLinkKey) {
	rkA := repoRefKey{key.repoA, key.refA}
	c.index[rkA] = append(c.index[rkA], key)
	rkB := repoRefKey{key.repoB, key.refB}
	c.index[rkB] = append(c.index[rkB], key)
}

// removeKeyFromIndexLocked removes a single crossLinkKey from the secondary
// index slice for rk. MUST be called with c.mu held.
func (c *CrossLinkCache) removeKeyFromIndexLocked(rk repoRefKey, target crossLinkKey) {
	slice := c.index[rk]
	for i, k := range slice {
		if k == target {
			slice[i] = slice[len(slice)-1]
			slice = slice[:len(slice)-1]
			break
		}
	}
	if len(slice) == 0 {
		delete(c.index, rk)
	} else {
		c.index[rk] = slice
	}
}
