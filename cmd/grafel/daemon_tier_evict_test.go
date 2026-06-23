package main

// daemon_tier_evict_test.go — tests for the #5238 dashboard-cache eviction
// lever wired into the tier WARM→COLD transition.
//
// These cover:
//  1. groupsForRepoPath resolves a repo path back to its registry group(s).
//  2. setDashboardGroupInvalidator + tierEvictCallback actually invoke the
//     registered invalidator for the demoted repo's group (so the dashboard
//     GraphCache's heavy materialised state is dropped, not just the cheap
//     mmap'd MCP reader).

import (
	"path/filepath"
	"sort"
	"sync"
	"testing"

	"github.com/cajasmota/grafel/internal/daemon/tier"
	"github.com/cajasmota/grafel/internal/registry"
	"github.com/cajasmota/grafel/internal/testsupport"
)

// writeTestGroup creates an isolated GRAFEL_HOME, registers a group with the
// given repo paths, and returns the group name. It uses t.Setenv so the
// registry resolves under the test's temp home.
func writeTestGroup(t *testing.T, group string, repoPaths ...string) {
	t.Helper()
	testsupport.IsolateHome(t)
	t.Setenv("GRAFEL_HOME", t.TempDir())

	cfgPath, err := registry.ConfigPathFor(group)
	if err != nil {
		t.Fatalf("ConfigPathFor: %v", err)
	}
	cfg := &registry.GroupConfig{Name: group}
	for i, p := range repoPaths {
		cfg.Repos = append(cfg.Repos, registry.Repo{
			Slug: filepath.Base(p) + "_" + string(rune('a'+i)),
			Path: p,
		})
	}
	if err := registry.SaveGroupConfig(cfgPath, cfg); err != nil {
		t.Fatalf("SaveGroupConfig: %v", err)
	}
	if err := registry.AddGroup(group, cfgPath); err != nil {
		t.Fatalf("AddGroup: %v", err)
	}
}

func TestGroupsForRepoPath(t *testing.T) {
	repoA := "/tmp/repo-a"
	repoB := "/tmp/repo-b"
	writeTestGroup(t, "mygroup", repoA, repoB)

	if got := groupsForRepoPath(repoA); len(got) != 1 || got[0] != "mygroup" {
		t.Errorf("groupsForRepoPath(%q) = %v; want [mygroup]", repoA, got)
	}
	if got := groupsForRepoPath(repoB); len(got) != 1 || got[0] != "mygroup" {
		t.Errorf("groupsForRepoPath(%q) = %v; want [mygroup]", repoB, got)
	}
	// Unknown repo → no groups (no panic, empty slice).
	if got := groupsForRepoPath("/tmp/not-registered"); len(got) != 0 {
		t.Errorf("groupsForRepoPath(unknown) = %v; want empty", got)
	}
}

// TestTierEvictCallbackInvalidatesDashboard verifies that a WARM→COLD eviction
// for a repo invokes the registered dashboard invalidator with that repo's
// group name — the #5238 fix. Without the wiring the dashboard GraphCache's
// materialised graph state would persist on the heap until the group was next
// re-requested past its TTL.
func TestTierEvictCallbackInvalidatesDashboard(t *testing.T) {
	repoA := "/tmp/repo-evict"
	writeTestGroup(t, "evictgroup", repoA)

	var (
		mu      sync.Mutex
		invoked []string
	)
	setDashboardGroupInvalidator(func(group string) {
		mu.Lock()
		invoked = append(invoked, group)
		mu.Unlock()
	})
	// Reset the package global so this test does not leak the hook into others.
	t.Cleanup(func() { setDashboardGroupInvalidator(nil) })

	// daemonMCPCache must be non-nil for tierEvictCallback's MCP-cache
	// Invalidate call; the default-capacity cache is fine (the path won't
	// resolve to a real graph.fb, which is a harmless no-op).
	if daemonMCPCache == nil {
		t.Skip("daemonMCPCache not initialised in this test binary")
	}

	tierEvictCallback(tier.SlotKey{RepoPath: repoA, Ref: "main"})

	mu.Lock()
	defer mu.Unlock()
	sort.Strings(invoked)
	if len(invoked) != 1 || invoked[0] != "evictgroup" {
		t.Errorf("tierEvictCallback invoked dashboard invalidator with %v; want [evictgroup]", invoked)
	}
}

// TestTierEvictCallbackNilInvalidatorSafe verifies tierEvictCallback does not
// panic when no dashboard invalidator is registered (e.g. a daemon started
// without the embedded dashboard).
func TestTierEvictCallbackNilInvalidatorSafe(t *testing.T) {
	setDashboardGroupInvalidator(nil)
	if daemonMCPCache == nil {
		t.Skip("daemonMCPCache not initialised in this test binary")
	}
	// Must not panic.
	tierEvictCallback(tier.SlotKey{RepoPath: "/tmp/unregistered-repo", Ref: "main"})
}
