// Package algo provides the on-demand algorithm-result cache for Silent Daemon
// S2 (#2152). The cache sits alongside graph.fb as a sidecar file:
//
//	<state-dir>/algo_results.fb
//
// "algo_results.fb" uses JSON encoding (not FlatBuffers); the .fb extension
// signals that the file lives in the graph artifact directory and follows the
// same lifecycle as graph.fb.
//
// Invalidation: the cache is considered stale when graph.fb's modification
// time is newer than the cache file's modification time. On next reindex, the
// daemon writes a fresh graph.fb which automatically invalidates the sidecar.
//
// Thread safety: Cache is safe for concurrent reads and writes; an internal
// mutex serialises per-key computation to prevent thundering herds.
package algo

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/cajasmota/grafel/internal/graph"
)

const cacheFileName = "algo_results.fb"

// Results holds the computed algorithm output for a single (repo, ref) pair.
// It mirrors the fields from graph.AlgorithmResults that are needed by
// rank-sensitive MCP tools.
type Results struct {
	// PageRank maps entity ID → PageRank score (damping=0.85).
	PageRank map[string]float64 `json:"pagerank"`
	// Centrality maps entity ID → betweenness centrality.
	Centrality map[string]float64 `json:"centrality"`
	// CommunityID maps entity ID → community id (-1 = ungrouped).
	CommunityID map[string]int `json:"community_id"`
	// GodNodes is the set of entity IDs in the top-5% by combined rank.
	GodNodes map[string]bool `json:"god_nodes"`
	// ArticulationPoints is the set of cut-vertices in the undirected graph.
	ArticulationPoints map[string]bool `json:"articulation_points"`
	// SurpriseEndpoints is the set of entity IDs on rare cross-community edges.
	SurpriseEndpoints map[string]bool `json:"surprise_endpoints"`
	// ComputedAt is when the results were computed (UTC).
	ComputedAt time.Time `json:"computed_at"`
}

// envelope wraps Results for disk serialisation.
type envelope struct {
	GraphMtime int64   `json:"graph_mtime"` // graph.fb mtime at compute time (UnixNano)
	Results    Results `json:"results"`
}

// ComputeFn computes fresh algo results for a (repoPath, ref). Callers
// typically delegate to graph.RunAlgorithms.
type ComputeFn func(ctx context.Context, repoPath, ref string) (*Results, error)

// Cache is an on-demand, disk-backed cache of algorithm results.
// Construct with New; call Get to obtain (possibly cached) results.
type Cache struct {
	mu      sync.Map // key string → *entry; protects per-key singleflight
	compute ComputeFn
}

// entry serialises concurrent Gets for the same key — only one compute
// runs at a time. The channel is closed when the result is ready.
type entry struct {
	mu   sync.Mutex
	res  *Results
	err  error
	done chan struct{}
}

// New returns a Cache backed by the supplied ComputeFn.
func New(compute ComputeFn) *Cache {
	return &Cache{compute: compute}
}

// Get returns algorithm results for the given state directory (the directory
// that contains graph.fb). It checks the on-disk cache first:
//
//   - If algo_results.fb exists and graph.fb has not changed since it was
//     computed, the cached results are returned immediately.
//   - Otherwise (cold cache, stale cache, or first call after reindex), the
//     ComputeFn is invoked, results are persisted to algo_results.fb, and
//     then returned.
//
// Concurrent Gets for the same stateDir are coalesced: only one ComputeFn
// call runs; the others wait and receive the same result.
func (c *Cache) Get(ctx context.Context, stateDir, repoPath, ref string) (*Results, error) {
	key := cacheKey(stateDir)

	// Fast path: try reading from disk without holding any lock.
	if r, ok := readFromDisk(stateDir); ok {
		return r, nil
	}

	// Slow path: ensure exactly one compute runs per stateDir.
	e := &entry{done: make(chan struct{})}
	actual, loaded := c.mu.LoadOrStore(key, e)
	if loaded {
		// Another goroutine already started computing — wait for it.
		existing := actual.(*entry)
		select {
		case <-existing.done:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		return existing.res, existing.err
	}

	// We own the compute. Always clean up the map entry when done.
	defer c.mu.Delete(key)
	defer close(e.done)

	// Re-check the disk cache after acquiring the key — a concurrent
	// compute may have just written it.
	if r, ok := readFromDisk(stateDir); ok {
		e.res = r
		return r, nil
	}

	r, err := c.compute(ctx, repoPath, ref)
	if err != nil {
		e.err = err
		return nil, fmt.Errorf("algo cache: compute %s@%s: %w", repoPath, ref, err)
	}

	// Persist to disk; ignore write errors (next call will recompute).
	if werr := writeToDisk(stateDir, r); werr != nil {
		// Non-fatal: log via stderr at debug level only. Callers still get results.
		_, _ = fmt.Fprintf(os.Stderr, "algo cache: write %s: %v\n", stateDir, werr)
	}

	e.res = r
	return r, nil
}

// Invalidate removes the on-disk cache for stateDir. Called by the daemon
// immediately after a successful reindex so the next query triggers a fresh
// compute. The function is idempotent — if the file does not exist no error
// is returned.
func Invalidate(stateDir string) error {
	p := filepath.Join(stateDir, cacheFileName)
	if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("algo cache: invalidate %s: %w", p, err)
	}
	return nil
}

// cacheKey returns a stable string key for the in-memory map.
func cacheKey(stateDir string) string { return stateDir }

// readFromDisk attempts to read and validate the on-disk cache. Returns
// (results, true) on a valid, fresh cache hit; (nil, false) on any miss or
// staleness.
func readFromDisk(stateDir string) (*Results, bool) {
	cachePath := filepath.Join(stateDir, cacheFileName)
	graphPath := graph.CurrentGraphPath(stateDir) // #5891: resolve active gen

	cacheInfo, err := os.Stat(cachePath)
	if err != nil {
		return nil, false // cache file does not exist
	}
	graphInfo, err := os.Stat(graphPath)
	if err != nil {
		return nil, false // graph.fb not found; nothing to validate against
	}

	// Staleness check: graph.fb must not be newer than the cache.
	// Use a 1-second tolerance to absorb filesystem timestamp granularity.
	if graphInfo.ModTime().After(cacheInfo.ModTime().Add(time.Second)) {
		return nil, false // graph.fb was updated after we cached
	}

	data, err := os.ReadFile(cachePath)
	if err != nil {
		return nil, false
	}
	var env envelope
	if err := json.Unmarshal(data, &env); err != nil {
		return nil, false
	}
	// Validate the embedded mtime matches the current graph.fb mtime.
	if env.GraphMtime != graphInfo.ModTime().UnixNano() {
		return nil, false
	}
	return &env.Results, true
}

// writeToDisk atomically writes the results to <stateDir>/algo_results.fb.
// Uses a temp-file + rename for crash-safety.
func writeToDisk(stateDir string, r *Results) error {
	graphPath := graph.CurrentGraphPath(stateDir) // #5891: resolve active gen
	graphInfo, err := os.Stat(graphPath)
	if err != nil {
		return fmt.Errorf("stat graph.fb: %w", err)
	}

	env := envelope{
		GraphMtime: graphInfo.ModTime().UnixNano(),
		Results:    *r,
	}
	data, err := json.Marshal(&env)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}

	// Ensure the directory exists (it should, since graph.fb is there, but be safe).
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		return fmt.Errorf("mkdirall: %w", err)
	}

	cachePath := filepath.Join(stateDir, cacheFileName)
	tmp := cachePath + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("write tmp: %w", err)
	}
	if err := os.Rename(tmp, cachePath); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("rename: %w", err)
	}
	return nil
}
