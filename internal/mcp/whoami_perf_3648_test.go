package mcp

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	mcpapi "github.com/mark3labs/mcp-go/mcp"
)

func writeSrc(t *testing.T, repoDir, rel, body string) {
	t.Helper()
	p := filepath.Join(repoDir, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func touchFile(path string, mtime time.Time) error {
	return os.Chtimes(path, mtime, mtime)
}

// whoami_perf_3648_test.go — perf + correctness regression tests for the
// archigraph_whoami latency fix (epic #3648, root #3325).
//
// The fix has two prongs, each tested here:
//   1. whoami's index counts must equal archigraph_stats' counts (correctness:
//      no response-shape change, both read the same cached LoadedRepo fields).
//   2. ComputeDocState — the per-call os.Stat walk over every unique source
//      file (the dominant cost on a 62K-entity graph) — must be memoized and
//      served from cache on the steady-state path, while still invalidating on
//      a reindex (graph mtime change) and on a new docgen run.

// TestWhoami_CountsMatchStats asserts archigraph_whoami's entity/relationship
// counts agree exactly with archigraph_stats. They are load-bearing for the
// rewrite agent's empty-graph-trap detection (#3325) and must read the same
// cached source.
func TestWhoami_CountsMatchStats(t *testing.T) {
	tmp := t.TempDir()
	setTestHome(t, tmp)
	t.Setenv("ARCHIGRAPH_WHOAMI_NUDGE", "") // enrichment on: entity_count/index block populated

	repoDir := filepath.Join(tmp, "repo-a")
	writeGraph(t, repoDir, fixtureDoc("repo-a"))
	regPath := makeRegistry(t, tmp, map[string]map[string]string{"g": {"repo-a": repoDir}})

	srv, err := NewServer(Config{RegistryPath: regPath})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	req := mcpapi.CallToolRequest{}
	req.Params.Arguments = map[string]any{"group": "g"}

	whoamiRes, err := srv.handleWhoami(context.Background(), req)
	if err != nil {
		t.Fatalf("handleWhoami: %v", err)
	}
	statsRes, err := srv.handleGraphStats(context.Background(), req)
	if err != nil {
		t.Fatalf("handleGraphStats: %v", err)
	}

	who := extractResultJSON(t, whoamiRes)
	stats := extractResultJSON(t, statsRes)

	wantE := stats["entities"].(float64)
	wantR := stats["relationships"].(float64)

	if got := who["entity_count"].(float64); got != wantE {
		t.Errorf("entity_count: whoami=%v stats=%v", got, wantE)
	}
	if got := who["relationship_count"].(float64); got != wantR {
		t.Errorf("relationship_count: whoami=%v stats=%v", got, wantR)
	}
	// The nested index block must agree too.
	idx := who["index"].(map[string]any)
	if got := idx["entity_count"].(float64); got != wantE {
		t.Errorf("index.entity_count: whoami=%v stats=%v", got, wantE)
	}
	if got := idx["relationship_count"].(float64); got != wantR {
		t.Errorf("index.relationship_count: whoami=%v stats=%v", got, wantR)
	}
}

// TestComputeDocState_ServedFromCache proves the os.Stat walk is memoized: once
// computed, a second call with an unchanged docgen-state file and unchanged
// graph mtime returns the cached result WITHOUT re-walking. We prove "no
// re-walk" by mutating the underlying source file's mtime to a value that WOULD
// flip stale_count if the walk re-ran — the cached call must ignore it.
func TestComputeDocState_ServedFromCache(t *testing.T) {
	resetDocStateCacheForTest()
	tmp := t.TempDir()
	setTestHome(t, tmp)

	repoDir := filepath.Join(tmp, "repo-a")
	writeSrc(t, repoDir, "src/app.go", "package main")

	// Docgen happened in the future ⇒ file is fresh (stale_count == 0).
	future := time.Now().Add(1 * time.Hour)
	if err := SaveDocgenState("g", DocgenState{LastDocgenAt: &future}); err != nil {
		t.Fatal(err)
	}

	lg := makeLoadedGroupWithFile(t, "g", "repo-a", repoDir, "src/app.go")
	lg.Repos["repo-a"].mtime = time.Now()

	first := ComputeDocState("g", lg)
	if first.StaleCount != 0 {
		t.Fatalf("setup: expected fresh, got stale_count=%d", first.StaleCount)
	}

	// Bump the source file far into the future. A re-walk would now mark it
	// stale; a cached read must NOT (docgen mtime + graph mtime unchanged).
	farFuture := time.Now().Add(48 * time.Hour)
	if err := touchFile(filepath.Join(repoDir, "src/app.go"), farFuture); err != nil {
		t.Fatal(err)
	}

	cached := ComputeDocState("g", lg)
	if cached.StaleCount != 0 {
		t.Fatalf("cache miss: walk re-ran (stale_count=%d) — memo not hit", cached.StaleCount)
	}
	if cached.DocumentationState != first.DocumentationState || cached.SuggestedAction != first.SuggestedAction {
		t.Fatalf("cached result diverged: %+v vs %+v", cached, first)
	}
}

// TestComputeDocState_InvalidatesOnReindex asserts the memo busts when the graph
// mtime advances (the index-completion signal) so stale_count stays correct
// after a reindex.
func TestComputeDocState_InvalidatesOnReindex(t *testing.T) {
	resetDocStateCacheForTest()
	tmp := t.TempDir()
	setTestHome(t, tmp)

	repoDir := filepath.Join(tmp, "repo-a")
	writeSrc(t, repoDir, "src/app.go", "package main")

	past := time.Now().Add(-1 * time.Hour)
	if err := SaveDocgenState("g", DocgenState{LastDocgenAt: &past}); err != nil {
		t.Fatal(err)
	}

	lg := makeLoadedGroupWithFile(t, "g", "repo-a", repoDir, "src/app.go")
	lg.Repos["repo-a"].mtime = time.Now()

	// File is newer than docgen ⇒ stale.
	first := ComputeDocState("g", lg)
	if first.StaleCount != 1 {
		t.Fatalf("setup: expected stale_count=1, got %d", first.StaleCount)
	}

	// Simulate a reindex that picked up a fresher docgen run: move docgen
	// forward past the file AND advance the graph mtime (index completion).
	future := time.Now().Add(1 * time.Hour)
	if err := SaveDocgenState("g", DocgenState{LastDocgenAt: &future}); err != nil {
		t.Fatal(err)
	}
	lg.Repos["repo-a"].mtime = time.Now().Add(1 * time.Second) // reindex bumps mtime

	after := ComputeDocState("g", lg)
	if after.StaleCount != 0 {
		t.Fatalf("cache did not invalidate on reindex: stale_count=%d want 0", after.StaleCount)
	}
}
