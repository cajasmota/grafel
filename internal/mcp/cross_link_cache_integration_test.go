// cross_link_cache_integration_test.go — integration test for issue #2224.
//
// Verifies end-to-end: after switching a repo's active ref via
// State.NotifyRefSwitch, cross-repo candidate lookups return fresh data
// and NOT the pre-switch cached result.
//
// Test strategy follows the pattern from internal/daemon/e2e_multi_ref_test.go:
// build on-disk graph fixtures and drive the State reload + invalidation loop
// directly without a running daemon process.
package mcp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/cajasmota/archigraph/internal/daemon"
	"github.com/cajasmota/archigraph/internal/graph"
)

// buildTwoRepoFixture creates the on-disk home / store environment for two
// synthetic repos and returns their absolute paths. It does NOT write graph
// files — callers should call writeRepoGraph for each (repo, ref) combination
// they want to exercise.
//
// The helper sets ARCHIGRAPH_HOME and daemon.EnvRoot environment variables
// (via t.Setenv so they are restored on test exit).
func buildTwoRepoFixture(t *testing.T) (repoAPath, repoBPath string) {
	t.Helper()

	home := t.TempDir()
	t.Setenv("ARCHIGRAPH_HOME", home)
	storeRoot := filepath.Join(home, "store")
	t.Setenv(daemon.EnvRoot, storeRoot)

	// Use /tmp directly for short paths (Unix socket sun_path limits).
	var tmpBase string
	if _, err := os.Stat("/tmp"); err == nil {
		tmpBase = "/tmp"
	} else {
		tmpBase = os.TempDir()
	}

	base, err := os.MkdirTemp(tmpBase, "archi-clc-")
	if err != nil {
		t.Fatalf("mktemp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(base) })

	repoAPath = filepath.Join(base, "repo-a")
	repoBPath = filepath.Join(base, "repo-b")
	for _, rp := range []string{repoAPath, repoBPath} {
		if err := os.MkdirAll(rp, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", rp, err)
		}
	}
	return repoAPath, repoBPath
}

// writeRepoGraph writes a graph.json file for repoPath+ref into the daemon
// store so State.Reload can discover it.
func writeRepoGraph(t *testing.T, repoPath, ref string, entityNames []string) {
	t.Helper()
	stateDir := daemon.StateDirForRepoRef(repoPath, ref)
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatalf("mkdir stateDir for %s ref %q: %v", repoPath, ref, err)
	}

	entities := make([]graph.Entity, 0, len(entityNames))
	for _, n := range entityNames {
		entities = append(entities, graph.Entity{ID: n, Name: n, Kind: "Function"})
	}
	doc := &graph.Document{
		Version:    1,
		Repo:       repoPath,
		IndexedRef: ref,
		Entities:   entities,
	}
	data, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal graph for ref %q: %v", ref, err)
	}
	if err := os.WriteFile(filepath.Join(stateDir, "graph.json"), data, 0o644); err != nil {
		t.Fatalf("write graph.json for ref %q: %v", ref, err)
	}
}

// buildGroupRegistryForCacheTest writes a minimal registry.json + fleet
// config that covers repoAPath and repoBPath under the given group name.
// Returns the registry path.
func buildGroupRegistryForCacheTest(t *testing.T, home, groupName, repoAPath, repoBPath string) string {
	t.Helper()

	cfgDir := filepath.Join(home, "groups")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatalf("mkdir groups: %v", err)
	}

	// Fleet config matching the format LoadRegistry expects (CLI array format).
	type fleetRepo struct {
		Slug string `json:"slug"`
		Path string `json:"path"`
	}
	type fleetCfg struct {
		Name  string      `json:"name"`
		Repos []fleetRepo `json:"repos"`
	}
	cfg := fleetCfg{
		Name: groupName,
		Repos: []fleetRepo{
			{Slug: "repo-a", Path: repoAPath},
			{Slug: "repo-b", Path: repoBPath},
		},
	}
	cfgRaw, _ := json.Marshal(cfg)
	cfgPath := filepath.Join(cfgDir, groupName+".fleet.json")
	if err := os.WriteFile(cfgPath, cfgRaw, 0o644); err != nil {
		t.Fatalf("write fleet config: %v", err)
	}

	// Registry.
	regRaw, _ := json.Marshal(map[string]any{
		"version": 1,
		"groups": []map[string]any{
			{"name": groupName, "config_path": cfgPath},
		},
	})
	regPath := filepath.Join(home, "registry.json")
	if err := os.WriteFile(regPath, regRaw, 0o644); err != nil {
		t.Fatalf("write registry.json: %v", err)
	}
	return regPath
}

// TestCrossLinkCache_RefSwitchInvalidatesStaleData is the primary integration
// test for issue #2224.
//
// Scenario:
//
//  1. Index repoA@main (EntityA, EntityB) and repoB@main (X, Y).
//  2. Query cross-repo candidates via the cache: key (repoA, main, repoB, main).
//     fn returns one synthetic candidate (EntityA → X).
//  3. Verify: second query with same key is a cache hit (fn NOT called again).
//  4. Simulate a ref switch: repoA switches from "main" to "feature/foo".
//     Call InvalidateRepo(repoA, "main") — stale entry must be evicted.
//  5. Query with the new key (repoA, feature/foo, repoB, main):
//     fn MUST be called again (fresh computation); result includes EntityC.
//  6. Verify: second query with the new key is again a cache hit.
//  7. No regression: (repoB, main) is not invalidated.
func TestCrossLinkCache_RefSwitchInvalidatesStaleData(t *testing.T) {
	repoAPath, repoBPath := buildTwoRepoFixture(t)

	// Write graph files for all refs we exercise.
	writeRepoGraph(t, repoAPath, "main", []string{"EntityA", "EntityB"})
	writeRepoGraph(t, repoAPath, "feature/foo", []string{"EntityA", "EntityB", "EntityC"})
	writeRepoGraph(t, repoBPath, "main", []string{"X", "Y"})

	cache := NewCrossLinkCache()

	// ── Step 2: initial query (cache miss) ───────────────────────────────────
	mainCalls := 0
	mainResult := []CrossRepoLink{
		{Source: "repo-a::EntityA", Target: "repo-b::X", Kind: "call", Confidence: 0.9},
	}
	result1 := cache.GetOrCompute(repoAPath, "main", repoBPath, "main", func() []CrossRepoLink {
		mainCalls++
		return mainResult
	})
	if mainCalls != 1 {
		t.Fatalf("step 2: fn should be called exactly once on miss; got %d calls", mainCalls)
	}
	if len(result1) != 1 {
		t.Fatalf("step 2: want 1 candidate (EntityA→X), got %d", len(result1))
	}

	// ── Step 3: cache hit ─────────────────────────────────────────────────────
	_ = cache.GetOrCompute(repoAPath, "main", repoBPath, "main", func() []CrossRepoLink {
		mainCalls++
		return mainResult
	})
	if mainCalls != 1 {
		t.Fatalf("step 3: cache hit should not call fn; total calls=%d", mainCalls)
	}
	if cache.Len() != 1 {
		t.Fatalf("step 3: want 1 cached entry before invalidation, got %d", cache.Len())
	}

	// ── Step 4: simulate ref switch repoA main → feature/foo ─────────────────
	// This mirrors what the daemon does on receipt of a BranchSwitchEvent:
	//   ev.RepoPath = repoAPath
	//   ev.OldRef   = "main"
	//   ev.NewRef   = "feature/foo"
	// → daemon calls: state.NotifyRefSwitch(ev.RepoPath, ev.OldRef)
	evicted := cache.InvalidateRepo(repoAPath, "main")
	if evicted != 1 {
		t.Errorf("step 4: InvalidateRepo should evict 1 entry, got %d", evicted)
	}
	if cache.Len() != 0 {
		t.Errorf("step 4: after invalidation cache must be empty, got %d", cache.Len())
	}
	// Stale key must be gone.
	_, hit := cache.Get(repoAPath, "main", repoBPath, "main")
	if hit {
		t.Error("step 4: stale (repoA, main, repoB, main) entry must have been evicted")
	}

	// ── Step 5: fresh query with new ref (cache miss after invalidation) ──────
	fooCalls := 0
	// feature/foo has EntityC in addition to EntityA/EntityB, so we add a
	// second candidate matching EntityC → Y to represent the fresh entity set.
	fooResult := []CrossRepoLink{
		{Source: "repo-a::EntityA", Target: "repo-b::X", Kind: "call", Confidence: 0.9},
		{Source: "repo-a::EntityC", Target: "repo-b::Y", Kind: "call", Confidence: 0.8},
	}
	result2 := cache.GetOrCompute(repoAPath, "feature/foo", repoBPath, "main", func() []CrossRepoLink {
		fooCalls++
		return fooResult
	})
	if fooCalls != 1 {
		t.Errorf("step 5: fn must be called once on fresh query; got %d calls", fooCalls)
	}
	if len(result2) != 2 {
		t.Errorf("step 5: fresh result must include EntityC; want 2 candidates, got %d", len(result2))
	}

	// ── Verify: fresh result differs from stale result ────────────────────────
	if len(result2) == len(result1) {
		t.Errorf("fresh result (%d) should differ from stale result (%d) — EntityC must appear",
			len(result2), len(result1))
	}

	// ── Step 6: no regression — same-ref repeated query is a cache hit ────────
	_ = cache.GetOrCompute(repoAPath, "feature/foo", repoBPath, "main", func() []CrossRepoLink {
		fooCalls++
		return fooResult
	})
	if fooCalls != 1 {
		t.Errorf("step 6: repeated query on feature/foo should be a cache hit; calls=%d", fooCalls)
	}

	// ── Step 7: no regression — invalidating repoA@main does not evict ────────
	// (repoB, main) entries.
	cache.Set(repoBPath, "main", repoAPath, "feature/foo", []CrossRepoLink{
		{Source: "repo-b::X", Target: "repo-a::EntityC", Kind: "import"},
	})
	prevLen := cache.Len()
	_ = cache.InvalidateRepo(repoAPath, "main") // already evicted; entry count unchanged
	if cache.Len() != prevLen {
		t.Errorf("step 7: second InvalidateRepo(repoA, main) should evict 0 entries; len went %d→%d",
			prevLen, cache.Len())
	}

	t.Logf("TestCrossLinkCache_RefSwitchInvalidatesStaleData: PASS")
	t.Logf("  stale  result (main):        %d candidates", len(result1))
	t.Logf("  fresh  result (feature/foo): %d candidates (EntityC added)", len(result2))
}

// TestCrossLinkCache_StateNotifyRefSwitch exercises State.NotifyRefSwitch,
// which is the public entry point wired to BranchSwitchEvent in the daemon.
//
// It verifies:
//   - NotifyRefSwitch delegates to CrossLinkCache.InvalidateRepo.
//   - The correct number of entries is evicted.
//   - State.CrossLinkCache is not nil after NewState.
func TestCrossLinkCache_StateNotifyRefSwitch(t *testing.T) {
	repoAPath, repoBPath := buildTwoRepoFixture(t)
	home := os.Getenv("ARCHIGRAPH_HOME")

	regPath := buildGroupRegistryForCacheTest(t, home, "cache-notify-test-group", repoAPath, repoBPath)

	// Write minimal graphs so Reload() doesn't fail.
	writeRepoGraph(t, repoAPath, "main", []string{"EntityA", "EntityB"})
	writeRepoGraph(t, repoBPath, "main", []string{"X", "Y"})

	reg, err := LoadRegistry(regPath)
	if err != nil {
		t.Fatalf("LoadRegistry: %v", err)
	}
	st := NewState(reg)
	if st.CrossLinkCache == nil {
		t.Fatal("NewState: CrossLinkCache must not be nil")
	}

	if _, err := st.Reload(); err != nil {
		t.Fatalf("Reload: %v", err)
	}

	// Populate the cache via the exported Set method.
	st.CrossLinkCache.Set(repoAPath, "main", repoBPath, "main", []CrossRepoLink{
		{Source: "repo-a::EntityA", Target: "repo-b::X", Kind: "call"},
	})
	st.CrossLinkCache.Set(repoAPath, "feature/bar", repoBPath, "main", []CrossRepoLink{
		{Source: "repo-a::EntityA", Target: "repo-b::Y", Kind: "import"},
	})

	if st.CrossLinkCache.Len() != 2 {
		t.Fatalf("want 2 cached entries, got %d", st.CrossLinkCache.Len())
	}

	// NotifyRefSwitch for (repoA, main): must evict the main entry only.
	evicted := st.NotifyRefSwitch(repoAPath, "main")
	if evicted != 1 {
		t.Errorf("NotifyRefSwitch(repoA, main): want evicted=1, got %d", evicted)
	}
	if st.CrossLinkCache.Len() != 1 {
		t.Errorf("after NotifyRefSwitch: want 1 entry remaining, got %d", st.CrossLinkCache.Len())
	}

	// (repoA, feature/bar) entry must survive.
	_, ok := st.CrossLinkCache.Get(repoAPath, "feature/bar", repoBPath, "main")
	if !ok {
		t.Error("(repoA, feature/bar) entry must survive (repoA, main) invalidation")
	}

	// Second NotifyRefSwitch for the same (already-evicted) pair is a no-op.
	evicted2 := st.NotifyRefSwitch(repoAPath, "main")
	if evicted2 != 0 {
		t.Errorf("second NotifyRefSwitch: want evicted=0, got %d", evicted2)
	}

	t.Logf("TestCrossLinkCache_StateNotifyRefSwitch: PASS — evicted=%d, remaining=%d",
		evicted, st.CrossLinkCache.Len())
}
