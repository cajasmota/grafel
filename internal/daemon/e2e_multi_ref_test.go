// e2e_multi_ref_test.go — end-to-end tests for the multi-branch + worktree
// feature introduced by epic #2087 and surfaced by #2098.
//
// Issue: #2223
// Refs:  #2098, #2087, #2219, #2220
//
// Three tests:
//
//	TestE2E_MultiRef_DaemonReindexesPerRef      — store layout per ref
//	TestE2E_MultiRef_WorktreeSwitchesActiveRef  — hot-ref switching via StateDirForRepo
//	TestE2E_MultiRef_DashboardSurfacesRefList   — HTTP GET /api/groups/:g/refs response
//
// Fixture is built at test-time via exec.Command("git", …) so the test is
// self-contained and reproducible on any host that has git in PATH.
package daemon_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/cajasmota/archigraph/internal/daemon"
	"github.com/cajasmota/archigraph/internal/dashboard"
	"github.com/cajasmota/archigraph/internal/graph"
	"github.com/cajasmota/archigraph/internal/registry"
)

// ── fixture ─────────────────────────────────────────────────────────────────

// multiRefFixture holds the paths that describe the fixture created by
// buildMultiRefFixture.
type multiRefFixture struct {
	// RepoPath is the absolute path of the main (parent) git repository.
	RepoPath string
	// WorktreeFoo is the absolute path of the linked worktree for feature/foo.
	WorktreeFoo string
	// WorktreeBar is the absolute path of the linked worktree for feature/bar.
	WorktreeBar string
}

// fixtureEntityNames maps ref-name → entity names that should appear in that ref.
var fixtureEntityNames = map[string][]string{
	"main":        {"EntityA", "EntityB", "EntityC"},
	"feature/foo": {"EntityA", "EntityB", "EntityC", "EntityD", "EntityE"},
	"feature/bar": {"EntityA", "EntityB", "EntityCPrime", "EntityD"},
}

// buildMultiRefFixture creates a temporary git repository with 3 branches and
// 2 linked worktrees:
//
//   - Branch main:        entities A, B, C
//   - Branch feature/foo: entities A, B, C, D, E (D and E added)
//   - Branch feature/bar: entities A, B, C', D   (C renamed to C')
//
// Two linked worktrees are added: one for feature/foo, one for feature/bar.
// The function returns a multiRefFixture and registers t.Cleanup to remove
// all created directories.
func buildMultiRefFixture(t *testing.T) multiRefFixture {
	t.Helper()

	// Ensure git is available.
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not in PATH; skipping multi-ref fixture test")
	}

	// Use /tmp directly to keep paths short enough for Unix socket sun_path
	// limits (mirrors shortTempRoot in daemon_test.go).
	var tmpBase string
	if _, err := os.Stat("/tmp"); err == nil {
		tmpBase = "/tmp"
	} else {
		tmpBase = os.TempDir()
	}

	base, err := os.MkdirTemp(tmpBase, "archi-mrf-")
	if err != nil {
		t.Fatalf("mktemp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(base) })

	repoPath := filepath.Join(base, "multi-ref-repo")
	wtFoo := filepath.Join(base, "wt-feature-foo")
	wtBar := filepath.Join(base, "wt-feature-bar")

	// Helper: run a git command inside repoPath and fatal on error.
	gitRun := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = repoPath
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=Test",
			"GIT_AUTHOR_EMAIL=test@test.invalid",
			"GIT_COMMITTER_NAME=Test",
			"GIT_COMMITTER_EMAIL=test@test.invalid",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}

	// ── Init repo ────────────────────────────────────────────────────────────
	if err := os.MkdirAll(repoPath, 0o755); err != nil {
		t.Fatalf("mkdir repo: %v", err)
	}
	gitRun("init", "-b", "main")
	gitRun("config", "user.email", "test@test.invalid")
	gitRun("config", "user.name", "Test")

	// Write a small Go module so the directory looks like real code.
	if err := os.WriteFile(filepath.Join(repoPath, "go.mod"),
		[]byte("module example.com/multi-ref-fixture\n\ngo 1.21\n"), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}

	// ── Branch main: entities A, B, C ────────────────────────────────────────
	writeFixtureEntities(t, repoPath, fixtureEntityNames["main"]...)
	gitRun("add", "-A")
	gitRun("commit", "-m", "main: A B C")

	// ── Branch feature/foo: A, B, C, D, E ────────────────────────────────────
	gitRun("checkout", "-b", "feature/foo")
	writeFixtureEntities(t, repoPath, fixtureEntityNames["feature/foo"]...)
	gitRun("add", "-A")
	gitRun("commit", "-m", "feature/foo: add D E")

	// ── Branch feature/bar: A, B, C', D ──────────────────────────────────────
	gitRun("checkout", "main")
	gitRun("checkout", "-b", "feature/bar")
	writeFixtureEntities(t, repoPath, fixtureEntityNames["feature/bar"]...)
	gitRun("add", "-A")
	gitRun("commit", "-m", "feature/bar: rename C to CPrime, add D")

	// Switch back to main so the repo HEAD is at a clean state.
	gitRun("checkout", "main")

	// ── Linked worktrees ──────────────────────────────────────────────────────
	gitRun("worktree", "add", wtFoo, "feature/foo")
	gitRun("worktree", "add", wtBar, "feature/bar")

	t.Cleanup(func() {
		// Prune linked worktrees so git is happy on subsequent operations.
		_ = exec.Command("git", "-C", repoPath, "worktree", "remove", "--force", wtFoo).Run()
		_ = exec.Command("git", "-C", repoPath, "worktree", "remove", "--force", wtBar).Run()
	})

	return multiRefFixture{
		RepoPath:    repoPath,
		WorktreeFoo: wtFoo,
		WorktreeBar: wtBar,
	}
}

// writeFixtureEntities writes one tiny Go file with all entities into dir.
// Each entity is a single exported function declaration.
func writeFixtureEntities(t *testing.T, dir string, names ...string) {
	t.Helper()
	content := "package fixture\n"
	for _, name := range names {
		content += fmt.Sprintf("\nfunc %s() {}\n", name)
	}
	if err := os.WriteFile(filepath.Join(dir, "entities.go"), []byte(content), 0o644); err != nil {
		t.Fatalf("write entities.go: %v", err)
	}
}

// buildRefGraphFiles writes per-ref graph.json + graph-stats.json files into
// the daemon store for repoPath, simulating what `archigraph index` would
// produce. This lets the three E2E tests exercise the store + dashboard
// without needing a real extractor.
//
// entityMap maps ref → slice of entity names to include.
func buildRefGraphFiles(t *testing.T, repoPath string, entityMap map[string][]string) {
	t.Helper()
	for ref, names := range entityMap {
		stateDir := daemon.StateDirForRepoRef(repoPath, ref)
		if err := os.MkdirAll(stateDir, 0o755); err != nil {
			t.Fatalf("mkdir stateDir for ref %q: %v", ref, err)
		}

		entities := make([]graph.Entity, 0, len(names))
		for _, n := range names {
			entities = append(entities, graph.Entity{
				ID:   n,
				Name: n,
				Kind: "Function",
			})
		}
		doc := &graph.Document{
			Version:    1,
			Repo:       repoPath,
			IndexedRef: ref,
			Entities:   entities,
		}
		docBytes, err := json.Marshal(doc)
		if err != nil {
			t.Fatalf("marshal doc for ref %q: %v", ref, err)
		}
		if err := os.WriteFile(filepath.Join(stateDir, "graph.json"), docBytes, 0o644); err != nil {
			t.Fatalf("write graph.json for ref %q: %v", ref, err)
		}

		// Write graph-stats.json sidecar so the /refs endpoint can read entity counts.
		stats := graph.GraphStatsSidecar{TotalEntities: len(names)}
		statsBytes, _ := json.Marshal(stats)
		if err := os.WriteFile(filepath.Join(stateDir, "graph-stats.json"), statsBytes, 0o644); err != nil {
			t.Fatalf("write graph-stats.json for ref %q: %v", ref, err)
		}
	}
}

// buildGroupFixture sets up the ARCHIGRAPH_HOME on-disk layout (registry.json
// + group fleet config) for the given repoPath and returns the group name and
// repo slug that can be used to query the dashboard.
func buildGroupFixture(t *testing.T, home, repoPath string) (groupName, repoSlug string) {
	t.Helper()

	groupName = "e2e-multi-ref"
	repoSlug = "multi-ref-repo"

	cfgDir := filepath.Join(home, "groups")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatalf("mkdir groups: %v", err)
	}

	cfg := &registry.GroupConfig{
		Name:  groupName,
		Repos: []registry.Repo{{Slug: repoSlug, Path: repoPath}},
	}
	cfgPath := filepath.Join(cfgDir, groupName+".fleet.json")
	raw, _ := json.Marshal(cfg)
	if err := os.WriteFile(cfgPath, raw, 0o644); err != nil {
		t.Fatalf("write fleet config: %v", err)
	}

	regRaw, _ := json.Marshal(map[string]any{
		"version": 1,
		"groups":  []map[string]any{{"name": groupName, "config_path": cfgPath}},
	})
	if err := os.WriteFile(filepath.Join(home, "registry.json"), regRaw, 0o644); err != nil {
		t.Fatalf("write registry.json: %v", err)
	}

	return groupName, repoSlug
}

// ── Test 1 ──────────────────────────────────────────────────────────────────

// TestE2E_MultiRef_DaemonReindexesPerRef verifies that after simulated indexing
// of all three branches the per-ref store layout holds the correct entity counts:
//
//   - main:        3 entities  (A, B, C)
//   - feature/foo: 5 entities  (A, B, C, D, E)
//   - feature/bar: 4 entities  (A, B, C', D)
//
// It also checks that each ref's graph is stored under a distinct directory
// inside the daemon store, confirming the per-branch isolation guarantee.
func TestE2E_MultiRef_DaemonReindexesPerRef(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ARCHIGRAPH_HOME", home)
	storeRoot := filepath.Join(home, "store")
	t.Setenv(daemon.EnvRoot, storeRoot)

	fx := buildMultiRefFixture(t)
	buildRefGraphFiles(t, fx.RepoPath, fixtureEntityNames)

	// Verify each ref's graph.json was written in a distinct directory
	// with the expected entity count.
	refDirs := map[string]string{}
	for ref, names := range fixtureEntityNames {
		stateDir := daemon.StateDirForRepoRef(fx.RepoPath, ref)
		refDirs[ref] = stateDir

		graphPath := filepath.Join(stateDir, "graph.json")
		raw, err := os.ReadFile(graphPath)
		if err != nil {
			t.Fatalf("ref %q: read graph.json: %v", ref, err)
		}

		var doc graph.Document
		if err := json.Unmarshal(raw, &doc); err != nil {
			t.Fatalf("ref %q: unmarshal graph.json: %v", ref, err)
		}

		if got, want := len(doc.Entities), len(names); got != want {
			t.Errorf("ref %q: entity count = %d, want %d", ref, got, want)
		}
		if doc.IndexedRef != ref {
			t.Errorf("ref %q: IndexedRef = %q, want %q", ref, doc.IndexedRef, ref)
		}
	}

	// All state dirs must be distinct — each ref lives in a separate directory.
	seen := map[string]string{} // dir → first ref that claimed it
	for ref, dir := range refDirs {
		if prev, exists := seen[dir]; exists {
			t.Errorf("refs %q and %q share state dir %q", ref, prev, dir)
		}
		seen[dir] = ref
	}

	// Verify the stats sidecar is readable and consistent.
	for ref, names := range fixtureEntityNames {
		stateDir := daemon.StateDirForRepoRef(fx.RepoPath, ref)
		statsPath := filepath.Join(stateDir, "graph-stats.json")
		raw, err := os.ReadFile(statsPath)
		if err != nil {
			t.Fatalf("ref %q: read graph-stats.json: %v", ref, err)
		}
		var stats graph.GraphStatsSidecar
		if err := json.Unmarshal(raw, &stats); err != nil {
			t.Fatalf("ref %q: unmarshal graph-stats.json: %v", ref, err)
		}
		if got, want := stats.TotalEntities, len(names); got != want {
			t.Errorf("ref %q: sidecar TotalEntities = %d, want %d", ref, got, want)
		}
	}

	t.Logf("TestE2E_MultiRef_DaemonReindexesPerRef: all 3 refs verified OK")
	for ref, dir := range refDirs {
		t.Logf("  %s → %s", ref, filepath.Base(dir))
	}
}

// ── Test 2 ──────────────────────────────────────────────────────────────────

// TestE2E_MultiRef_WorktreeSwitchesActiveRef verifies that when the daemon
// resolves the "hot" (active) state directory for a path, it picks the branch
// checked out at that path — not the branch of the main checkout.
//
// This exercises the StateDirForRepo → gitmeta.Capture → StateDirForRepoRef
// chain.  The invariant is:
//
//   - main repo path         → hot dir for "main"          (store key: main repo)
//   - feature/foo worktree   → hot dir for "feature/foo"   (store key: worktree path)
//   - feature/bar worktree   → hot dir for "feature/bar"   (store key: worktree path)
//
// Each worktree is keyed in the store by its *own* path (not the parent repo
// path) — this is how the daemon tracks independent worktrees separately.
// All three hot dirs must be distinct.
func TestE2E_MultiRef_WorktreeSwitchesActiveRef(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ARCHIGRAPH_HOME", home)
	storeRoot := filepath.Join(home, "store")
	t.Setenv(daemon.EnvRoot, storeRoot)

	fx := buildMultiRefFixture(t)

	// StateDirForRepo reads the current HEAD ref via gitmeta.Capture.
	// For the main repo checkout the HEAD is "main".
	mainHotDir := daemon.StateDirForRepo(fx.RepoPath)
	mainExpected := daemon.StateDirForRepoRef(fx.RepoPath, "main")
	if mainHotDir != mainExpected {
		t.Errorf("main repo: hot dir = %q, want %q (ref 'main')", mainHotDir, mainExpected)
	}

	// For the feature/foo worktree, gitmeta.Capture detects "feature/foo"
	// from the worktree's HEAD. The store key is the worktree's own path
	// (not the parent repo path) so each worktree gets a separate store slot.
	fooHotDir := daemon.StateDirForRepo(fx.WorktreeFoo)
	fooExpected := daemon.StateDirForRepoRef(fx.WorktreeFoo, "feature/foo")
	if fooHotDir != fooExpected {
		t.Errorf("feature/foo worktree: hot dir = %q, want %q (ref 'feature/foo')", fooHotDir, fooExpected)
	}

	// For the feature/bar worktree, the expected ref is "feature/bar".
	barHotDir := daemon.StateDirForRepo(fx.WorktreeBar)
	barExpected := daemon.StateDirForRepoRef(fx.WorktreeBar, "feature/bar")
	if barHotDir != barExpected {
		t.Errorf("feature/bar worktree: hot dir = %q, want %q (ref 'feature/bar')", barHotDir, barExpected)
	}

	// Confirm that each hot dir reports the correct ref name in the path.
	for path, wantRef := range map[string]string{
		mainHotDir: "main",
		fooHotDir:  "feature/foo",
		barHotDir:  "feature/bar",
	} {
		wantSafe := daemon.RefSafeEncode(wantRef)
		if base := filepath.Base(path); base != wantSafe {
			t.Errorf("hot dir tail: got %q, want %q (for ref %q)", base, wantSafe, wantRef)
		}
	}

	// All three hot dirs must be distinct — each context reports a different ref.
	dirs := map[string]string{
		"main repo":      mainHotDir,
		"feature/foo wt": fooHotDir,
		"feature/bar wt": barHotDir,
	}
	seen := map[string]string{} // dir → first context that claimed it
	for ctx, dir := range dirs {
		if prev, exists := seen[dir]; exists {
			t.Errorf("contexts %q and %q resolved to the same hot dir %q", ctx, prev, dir)
		}
		seen[dir] = ctx
	}

	t.Logf("TestE2E_MultiRef_WorktreeSwitchesActiveRef: hot refs verified")
	t.Logf("  main        → %s", filepath.Base(mainHotDir))
	t.Logf("  feature/foo → %s", filepath.Base(fooHotDir))
	t.Logf("  feature/bar → %s", filepath.Base(barHotDir))
}

// ── Test 3 ──────────────────────────────────────────────────────────────────

// e2eDashStore implements dashboard.RegistryStore with stub no-op
// implementations. The E2E test exercises handler logic through on-disk files,
// not through the store interface, so no-op is correct here.
type e2eDashStore struct{}

func (e *e2eDashStore) ListGroups() ([]dashboard.GroupSummary, error) {
	return nil, nil
}
func (e *e2eDashStore) GroupGraph(group string) ([]byte, error) {
	return nil, fmt.Errorf("e2eDashStore: GroupGraph not implemented")
}
func (e *e2eDashStore) RepoGraph(group, repo string) ([]byte, error) {
	return nil, fmt.Errorf("e2eDashStore: RepoGraph not implemented")
}
func (e *e2eDashStore) CreateGroup(name string) (dashboard.GroupSummary, error) {
	return dashboard.GroupSummary{}, fmt.Errorf("e2eDashStore: CreateGroup not implemented")
}
func (e *e2eDashStore) AddRepo(group string, repo registry.Repo) error {
	return fmt.Errorf("e2eDashStore: AddRepo not implemented")
}

// startDashboardForTest starts the dashboard HTTP server on a random free port
// using UseListener and Serve, and returns the base URL + a cancel function.
func startDashboardForTest(t *testing.T) (baseURL string, stop func()) {
	t.Helper()

	srv, err := dashboard.NewServer(dashboard.DefaultConfig(), &e2eDashStore{})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	// Bind to a random free port on loopback.
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	srv.UseListener(l)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- srv.Serve(ctx) }()

	addr := l.Addr().String()
	baseURL = "http://" + addr

	// Wait until the server accepts connections.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get(baseURL + "/api/registry")
		if err == nil {
			resp.Body.Close()
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	stop = func() {
		cancel()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Logf("dashboard server did not exit within 3s")
		}
	}
	return baseURL, stop
}

// TestE2E_MultiRef_DashboardSurfacesRefList verifies the HTTP
// GET /api/groups/:g/refs endpoint (introduced by #2220) when the fixture is
// fully indexed across all 3 refs.
//
// Checks:
//   - Response contains all 3 named refs (main, feature/foo, feature/bar).
//   - Each ref has the correct entity_count from the store sidecar.
//   - "main" has is_canonical: true.
//   - feature/foo and feature/bar have is_canonical: false.
func TestE2E_MultiRef_DashboardSurfacesRefList(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ARCHIGRAPH_HOME", home)
	storeRoot := filepath.Join(home, "store")
	t.Setenv(daemon.EnvRoot, storeRoot)

	fx := buildMultiRefFixture(t)

	// Write all 3 refs + the _unknown sentinel (mirrors buildRefEndpointFixture
	// in handlers_ref_endpoints_test.go so the "no ?ref=" default path works).
	allRefs := map[string][]string{
		"":            fixtureEntityNames["main"],
		"main":        fixtureEntityNames["main"],
		"feature/foo": fixtureEntityNames["feature/foo"],
		"feature/bar": fixtureEntityNames["feature/bar"],
	}
	buildRefGraphFiles(t, fx.RepoPath, allRefs)

	groupName, _ := buildGroupFixture(t, home, fx.RepoPath)

	// Start the dashboard HTTP server.
	baseURL, stop := startDashboardForTest(t)
	t.Cleanup(stop)

	resp, err := http.Get(baseURL + "/api/groups/" + groupName + "/refs")
	if err != nil {
		t.Fatalf("GET /api/groups/%s/refs: %v", groupName, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}

	var body struct {
		Group string `json:"group"`
		Refs  []struct {
			Name        string `json:"name"`
			IsCanonical bool   `json:"is_canonical"`
			EntityCount int    `json:"entity_count"`
			IsHot       bool   `json:"is_hot"`
		} `json:"refs"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if body.Group != groupName {
		t.Errorf("group: want %q, got %q", groupName, body.Group)
	}

	// Build a lookup map by ref name.
	type refEntry struct {
		IsCanonical bool
		EntityCount int
		IsHot       bool
	}
	refMap := map[string]refEntry{}
	for _, r := range body.Refs {
		refMap[r.Name] = refEntry{r.IsCanonical, r.EntityCount, r.IsHot}
	}

	// All 3 named refs must appear.
	for _, refName := range []string{"main", "feature/foo", "feature/bar"} {
		entry, ok := refMap[refName]
		if !ok {
			var allNames []string
			for _, r := range body.Refs {
				allNames = append(allNames, r.Name)
			}
			sort.Strings(allNames)
			t.Errorf("ref %q not found; got refs: %v", refName, allNames)
			continue
		}
		wantCount := len(fixtureEntityNames[refName])
		if entry.EntityCount != wantCount {
			t.Errorf("ref %q: entity_count = %d, want %d", refName, entry.EntityCount, wantCount)
		}
	}

	// "main" must be canonical; the feature branches must not be.
	if m, ok := refMap["main"]; ok && !m.IsCanonical {
		t.Error("ref main: is_canonical should be true")
	}
	for _, branchRef := range []string{"feature/foo", "feature/bar"} {
		if e, ok := refMap[branchRef]; ok && e.IsCanonical {
			t.Errorf("ref %q: is_canonical should be false", branchRef)
		}
	}

	t.Logf("TestE2E_MultiRef_DashboardSurfacesRefList: %d refs returned", len(body.Refs))
	for _, r := range body.Refs {
		t.Logf("  %s: entities=%d canonical=%v hot=%v", r.Name, r.EntityCount, r.IsCanonical, r.IsHot)
	}
}
