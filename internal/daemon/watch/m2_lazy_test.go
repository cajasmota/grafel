// Package watch — M2 lazy fsnotify subscription tests (#2179).
//
// These tests verify the core M2 invariants:
//   - Boot with N registered groups → 0 fsnotify subscriptions
//   - First query on group A → only A's repos get subscribed
//   - All refs for a repo become COLD (Pause) → unsubscribe that repo
//   - Resume after Pause → re-subscribe (same as PH2a, verified here for M2 path)
//   - Multi-group: 2 active + 3 cold → only 2 groups consume watcher resources
package watch

import (
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// makeGroups creates N distinct temp dirs representing group repos.
// Returns a map of groupName → []repoPath.
func makeGroups(t *testing.T, names []string) map[string][]string {
	t.Helper()
	out := make(map[string][]string, len(names))
	for _, name := range names {
		dir := t.TempDir()
		out[name] = []string{dir}
	}
	return out
}

// assertRepoCount asserts the watcher's Repos() slice has exactly want entries.
func assertRepoCount(t *testing.T, w *Watcher, want int, msg string) {
	t.Helper()
	if got := len(w.Repos()); got != want {
		t.Errorf("%s: want %d watcher repos, got %d", msg, want, got)
	}
}

// assertSubscribedRepoCount asserts the manager's SubscribedRepoCount.
func assertSubscribedRepoCount(t *testing.T, m *DefaultManager, want int, msg string) {
	t.Helper()
	if got := m.SubscribedRepoCount(); got != want {
		t.Errorf("%s: want SubscribedRepoCount=%d, got %d", msg, want, got)
	}
}

// ---------------------------------------------------------------------------
// Test 1: Boot — register N groups, no queries → 0 fsnotify subscriptions
// ---------------------------------------------------------------------------

// TestM2_BootZeroSubscriptions verifies that registering 5 groups at boot
// (without any queries) leaves the watcher with zero fsnotify subscriptions.
// This is the core M2 idle-daemon invariant.
func TestM2_BootZeroSubscriptions(t *testing.T) {
	w := newNopWatcher(t)
	m := NewDefaultManager(w, nil)

	groups := makeGroups(t, []string{"alpha", "beta", "gamma", "delta", "epsilon"})

	// Simulate S1 boot-time registration: just declare slots via Register,
	// no SubscribeGroup or AddRepo calls (M2 boot path).
	for _, repos := range groups {
		for _, repo := range repos {
			m.Register(repo, "main")
		}
	}

	// Assert: watcher has zero repos subscribed.
	assertRepoCount(t, w, 0, "after boot with 5 groups, no queries")
	assertSubscribedRepoCount(t, m, 0, "after boot with 5 groups, no queries")

	if n := m.SubscribedGroupCount(); n != 0 {
		t.Errorf("SubscribedGroupCount: want 0, got %d", n)
	}
}

// ---------------------------------------------------------------------------
// Test 2: Query group A → only A's repos get subscribed
// ---------------------------------------------------------------------------

// TestM2_QueryGroupSubscribesOnlyThatGroup verifies that calling SubscribeGroup
// for group A subscribes only A's repos, leaving B's repos unsubscribed.
func TestM2_QueryGroupSubscribesOnlyThatGroup(t *testing.T) {
	w := newNopWatcher(t)
	m := NewDefaultManager(w, nil)

	repoA := t.TempDir()
	repoB := t.TempDir()

	// Boot: declare both groups (no subscribe).
	m.Register(repoA, "main")
	m.Register(repoB, "main")

	assertRepoCount(t, w, 0, "pre-query")

	// First query on group A → subscribe A.
	m.SubscribeGroup("groupA", []string{repoA})

	// Only A is subscribed.
	repos := w.Repos()
	foundA, foundB := false, false
	for _, r := range repos {
		if r == repoA {
			foundA = true
		}
		if r == repoB {
			foundB = true
		}
	}
	if !foundA {
		t.Error("repoA should be subscribed after SubscribeGroup(groupA)")
	}
	if foundB {
		t.Error("repoB should NOT be subscribed — groupB not queried yet")
	}

	if n := m.SubscribedGroupCount(); n != 1 {
		t.Errorf("SubscribedGroupCount: want 1, got %d", n)
	}
	if n := m.SubscribedRepoCount(); n != 1 {
		t.Errorf("SubscribedRepoCount: want 1, got %d", n)
	}
}

// ---------------------------------------------------------------------------
// Test 3: Idle (Pause all refs) → unsubscribe
// ---------------------------------------------------------------------------

// TestM2_IdlePauseUnsubscribes verifies that pausing all refs for a repo
// (simulating the WARM→COLD tier transition) removes the fsnotify subscription.
// This is the "idle TTL → unsubscribe" half of M2.
func TestM2_IdlePauseUnsubscribes(t *testing.T) {
	w := newNopWatcher(t)
	m := NewDefaultManager(w, nil)

	repo := t.TempDir()

	// Subscribe the repo via SubscribeGroup (first query).
	m.SubscribeGroup("groupA", []string{repo})
	if len(w.Repos()) != 1 {
		t.Fatalf("prereq: want 1 subscribed repo after SubscribeGroup, got %d", len(w.Repos()))
	}

	// Simulate tier WARM→COLD: Pause the sentinel ref.
	// (In production, Pause is called with the indexed ref. Using "" here
	// since SubscribeGroup registers with the sentinel.)
	m.Pause(repo, "")

	// Watcher should now have 0 repos.
	assertRepoCount(t, w, 0, "after idle Pause")
	assertSubscribedRepoCount(t, m, 0, "after idle Pause")
}

// ---------------------------------------------------------------------------
// Test 4: Re-query after idle → re-subscribe
// ---------------------------------------------------------------------------

// TestM2_ReQueryResubscribes verifies that after an idle unsubscription,
// a new query (Resume call from cold-wake) re-subscribes the repo.
func TestM2_ReQueryResubscribes(t *testing.T) {
	w := newNopWatcher(t)
	m := NewDefaultManager(w, nil)

	repo := t.TempDir()

	// First subscription.
	m.SubscribeGroup("groupA", []string{repo})
	if len(w.Repos()) != 1 {
		t.Fatalf("prereq: want 1 repo after first subscribe")
	}

	// Idle eviction.
	m.Pause(repo, "")
	if len(w.Repos()) != 0 {
		t.Fatalf("prereq: want 0 repos after pause")
	}

	// Re-query: tier cold-wake calls Resume.
	elapsed := m.Resume(repo, "")
	if elapsed > 2*time.Second {
		t.Errorf("Resume took too long: %s", elapsed)
	}

	// Re-subscribed.
	assertRepoCount(t, w, 1, "after Resume (re-query)")
	assertSubscribedRepoCount(t, m, 1, "after Resume (re-query)")
}

// ---------------------------------------------------------------------------
// Test 5: Multi-tier — 2 active groups + 3 cold → only 2 groups use watchers
// ---------------------------------------------------------------------------

// TestM2_MultiTierActiveVsCold verifies that with 5 groups, subscribing 2 and
// leaving 3 cold results in only 2 groups consuming fsnotify resources.
func TestM2_MultiTierActiveVsCold(t *testing.T) {
	w := newNopWatcher(t)
	m := NewDefaultManager(w, nil)

	// Create 5 groups, each with 1 repo.
	groupNames := []string{"g1", "g2", "g3", "g4", "g5"}
	groups := makeGroups(t, groupNames)

	// Boot: declare all 5 groups (no subscribe).
	for _, repos := range groups {
		for _, repo := range repos {
			m.Register(repo, "main")
		}
	}
	assertRepoCount(t, w, 0, "pre-query: 5 cold groups")

	// Subscribe 2 of the 5 groups (MCP queries arrive for g1 and g2 only).
	m.SubscribeGroup("g1", groups["g1"])
	m.SubscribeGroup("g2", groups["g2"])

	// Exactly 2 repos subscribed.
	assertRepoCount(t, w, 2, "after subscribing 2 groups")
	if n := m.SubscribedGroupCount(); n != 2 {
		t.Errorf("SubscribedGroupCount: want 2 (g1+g2 active), got %d", n)
	}
	if n := m.SubscribedRepoCount(); n != 2 {
		t.Errorf("SubscribedRepoCount: want 2, got %d", n)
	}

	// Verify that g3/g4/g5 repos are NOT in the watcher.
	watchedSet := make(map[string]bool)
	for _, r := range w.Repos() {
		watchedSet[r] = true
	}
	for _, coldGroup := range []string{"g3", "g4", "g5"} {
		for _, repo := range groups[coldGroup] {
			if watchedSet[repo] {
				t.Errorf("cold group %s repo %s should NOT be in watcher", coldGroup, repo)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Test 6: SubscribeGroup idempotent — double-call doesn't double-subscribe
// ---------------------------------------------------------------------------

// TestM2_SubscribeGroupIdempotent verifies that calling SubscribeGroup twice
// for the same group does not double-subscribe the repos.
func TestM2_SubscribeGroupIdempotent(t *testing.T) {
	w := newNopWatcher(t)
	m := NewDefaultManager(w, nil)

	repo := t.TempDir()

	m.SubscribeGroup("groupA", []string{repo})
	m.SubscribeGroup("groupA", []string{repo}) // second call — should be a no-op

	assertRepoCount(t, w, 1, "after double SubscribeGroup")
	assertSubscribedRepoCount(t, m, 1, "after double SubscribeGroup")
}

// ---------------------------------------------------------------------------
// Test 7: Register declares slot but does not subscribe
// ---------------------------------------------------------------------------

// TestM2_RegisterDoesNotSubscribe verifies that Register (called at boot)
// does not trigger any fsnotify subscription.
func TestM2_RegisterDoesNotSubscribe(t *testing.T) {
	w := newNopWatcher(t)
	m := NewDefaultManager(w, nil)

	// Register many slots — as if registerKnownGroupsCold ran.
	for i := 0; i < 20; i++ {
		m.Register(t.TempDir(), "main")
	}

	assertRepoCount(t, w, 0, "Register must not subscribe fsnotify")
	assertSubscribedRepoCount(t, m, 0, "Register must not subscribe fsnotify")
}
