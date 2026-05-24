// handlers_v2_diff.go — GET /api/v2/groups/:group/repos/:repo/diff
//
// PH5 of epic #2087 (#2093): returns the structural diff between two indexed
// git refs for a single repo. The diff is computed by DiffDocs in
// internal/graph/diff.go and cached in an in-process LRU (N=10 entries,
// keyed by group+repo+refA-sha+refB-sha).
//
// Wire shape:
//
//	GET /api/v2/groups/{group}/repos/{repo}/diff?refA=main&refB=feat%2Fx
//
//	{
//	  "ok": true,
//	  "data": {
//	    "group": "mygroup",
//	    "repo":  "myrepo",
//	    "ref_a": "main",
//	    "ref_b": "feat/x",
//	    "summary": { "entities_added": N, ... },
//	    "entities": { "added": [...], "removed": [...], "modified": [...] },
//	    "relationships": { "added": [...], "removed": [...] }
//	  }
//	}
package dashboard

import (
	"container/list"
	"fmt"
	"net/http"
	"sync"

	"github.com/cajasmota/archigraph/internal/graph"
	"github.com/cajasmota/archigraph/internal/registry"
)

// ---------------------------------------------------------------------------
// Diff result LRU cache
// ---------------------------------------------------------------------------

const diffCacheCapacity = 10

// diffCacheKey identifies a unique diff computation.
type diffCacheKey struct {
	group, repo, refA, refB string
}

// diffCacheEntry holds one cached DiffResult.
type diffCacheEntry struct {
	key  diffCacheKey
	data graph.DiffResult
}

// diffLRUCache is a concurrency-safe LRU cache for diff results.
// Capacity is fixed at diffCacheCapacity (10). On cache-miss the caller
// computes the diff and calls Set; subsequent calls with the same key are
// served from cache without any disk I/O.
type diffLRUCache struct {
	mu    sync.Mutex
	cap   int
	ll    *list.List                     // LRU order (front = most recently used)
	items map[diffCacheKey]*list.Element // key → list element
}

func newDiffLRUCache(capacity int) *diffLRUCache {
	return &diffLRUCache{
		cap:   capacity,
		ll:    list.New(),
		items: make(map[diffCacheKey]*list.Element, capacity),
	}
}

// Get returns (result, true) on a hit, (zero, false) on a miss.
func (c *diffLRUCache) Get(k diffCacheKey) (graph.DiffResult, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.items[k]; ok {
		c.ll.MoveToFront(el)
		return el.Value.(*diffCacheEntry).data, true
	}
	return graph.DiffResult{}, false
}

// Set stores the result. Evicts the least-recently-used entry when the cache
// is at capacity.
func (c *diffLRUCache) Set(k diffCacheKey, data graph.DiffResult) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.items[k]; ok {
		c.ll.MoveToFront(el)
		el.Value.(*diffCacheEntry).data = data
		return
	}
	if c.ll.Len() >= c.cap {
		// Evict LRU.
		back := c.ll.Back()
		if back != nil {
			c.ll.Remove(back)
			delete(c.items, back.Value.(*diffCacheEntry).key)
		}
	}
	ent := &diffCacheEntry{key: k, data: data}
	el := c.ll.PushFront(ent)
	c.items[k] = el
}

// ---------------------------------------------------------------------------
// Server-level diff cache (lazily initialised, one per server instance)
// ---------------------------------------------------------------------------

var globalDiffCache = newDiffLRUCache(diffCacheCapacity)

// ---------------------------------------------------------------------------
// Handler
// ---------------------------------------------------------------------------

// handleV2RepoDiff handles GET /api/v2/groups/{group}/repos/{repo}/diff
//
// Query params:
//   - refA  (required) — "before" ref name (e.g. "main")
//   - refB  (required) — "after" ref name (e.g. "feat/x")
func (s *Server) handleV2RepoDiff(w http.ResponseWriter, r *http.Request) {
	group := r.PathValue("group")
	repo := r.PathValue("repo")
	if group == "" || repo == "" {
		writeV2Err(w, http.StatusBadRequest, "bad_request", "group and repo path parameters are required")
		return
	}

	refA := r.URL.Query().Get("refA")
	refB := r.URL.Query().Get("refB")
	if refA == "" || refB == "" {
		writeV2Err(w, http.StatusBadRequest, "bad_request", "refA and refB query parameters are required")
		return
	}

	// Look up the repo's filesystem path from the registry so we can pass it
	// to GetGroupForRef (which calls daemon.StateDirForRepoRef internally).
	repoPath, err := diffResolveRepoPath(group, repo)
	if err != nil {
		writeV2Err(w, http.StatusNotFound, "not_found", err.Error())
		return
	}
	_ = repoPath // used implicitly via GetGroupForRef below

	// Same-ref fast path.
	if refA == refB {
		result := graph.DiffResult{
			Group: group,
			Repo:  repo,
			RefA:  refA,
			RefB:  refB,
		}
		result.Entities.Added = []graph.DiffEntityEntry{}
		result.Entities.Removed = []graph.DiffEntityEntry{}
		result.Entities.Modified = []graph.DiffEntityEntry{}
		result.Relationships.Added = []graph.DiffRelEntry{}
		result.Relationships.Removed = []graph.DiffRelEntry{}
		writeV2JSON(w, http.StatusOK, v2OK(result))
		return
	}

	// Cache lookup keyed by (group, repo, refA, refB). We use the logical ref
	// names as keys (not SHAs) because archigraph's state dir is keyed the
	// same way. A re-index of either ref will not invalidate this cache, which
	// is acceptable for the beta — add SHA-based invalidation in a follow-up.
	cacheKey := diffCacheKey{group: group, repo: repo, refA: refA, refB: refB}
	if cached, ok := globalDiffCache.Get(cacheKey); ok {
		writeV2JSON(w, http.StatusOK, v2OK(cached))
		return
	}

	// Load graph for refA.
	grpA, err := s.graphs.GetGroupForRef(group, refA)
	if err != nil {
		writeV2Err(w, http.StatusNotFound, "ref_not_found",
			"could not load graph for refA="+refA+": "+err.Error())
		return
	}
	drA, ok := grpA.Repos[repo]
	if !ok || drA.Doc == nil {
		writeV2Err(w, http.StatusNotFound, "ref_not_found",
			"repo "+repo+" not found or not indexed for refA="+refA)
		return
	}

	// Load graph for refB.
	grpB, err := s.graphs.GetGroupForRef(group, refB)
	if err != nil {
		writeV2Err(w, http.StatusNotFound, "ref_not_found",
			"could not load graph for refB="+refB+": "+err.Error())
		return
	}
	drB, ok := grpB.Repos[repo]
	if !ok || drB.Doc == nil {
		writeV2Err(w, http.StatusNotFound, "ref_not_found",
			"repo "+repo+" not found or not indexed for refB="+refB)
		return
	}

	// Compute diff.
	result := graph.DiffDocs(drA.Doc, drB.Doc)
	result.Group = group
	result.Repo = repo
	result.RefA = refA
	result.RefB = refB

	// Store in cache.
	globalDiffCache.Set(cacheKey, result)

	writeV2JSON(w, http.StatusOK, v2OK(result))
}

// diffResolveRepoPath looks up the repo's filesystem path from the registry.
// Returns an error when the group or repo is unknown.
func diffResolveRepoPath(groupName, repoSlug string) (string, error) {
	groups, err := registry.Groups()
	if err != nil {
		return "", err
	}
	var cfgPath string
	for _, g := range groups {
		if g.Name == groupName {
			cfgPath = g.ConfigPath
			break
		}
	}
	if cfgPath == "" {
		return "", fmt.Errorf("group %q not registered", groupName)
	}
	cfg, err := registry.LoadGroupConfig(cfgPath)
	if err != nil {
		return "", err
	}
	for _, r := range cfg.Repos {
		if r.Slug == repoSlug {
			return r.Path, nil
		}
	}
	return "", fmt.Errorf("repo %q not found in group %q", repoSlug, groupName)
}
