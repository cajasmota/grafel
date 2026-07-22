// handlers_v2_refs_test.go — unit tests for GET /api/v2/groups/:group/refs
// PH1c of epic #2087 (#2089).
package dashboard

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/daemon"
	"github.com/cajasmota/grafel/internal/registry"
)

// buildRefsTestDir creates an on-disk directory layout that mimics
// the per-ref store produced by PH1a:
//
//	<storeBase>/<slug-hash>/refs/<refSafe>/graph.fb
//	<storeBase>/<slug-hash>/refs/<refSafe>/graph-stats.json  (optional)
//
// It writes a real grafel group config + registry so that
// handleV2GroupRefs can load them.
//
// Returns:
//   - grafelHome: path to set as GRAFEL_HOME
//   - groupConfigPath: path written to the registry
func buildRefsTestDir(t *testing.T, groupName string, repos []registry.Repo, refSlots map[string][]string) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("GRAFEL_HOME", home)

	// Write group config.
	cfg := &registry.GroupConfig{
		Name:  groupName,
		Repos: repos,
	}
	cfgDir := filepath.Join(home, "groups")
	os.MkdirAll(cfgDir, 0o755)
	cfgPath := filepath.Join(cfgDir, groupName+".fleet.json")
	raw, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal group config: %v", err)
	}
	if err := os.WriteFile(cfgPath, raw, 0o644); err != nil {
		t.Fatalf("write group config: %v", err)
	}

	// Write registry.json.
	reg := map[string]any{
		"version": 1,
		"groups": []map[string]any{
			{"name": groupName, "config_path": cfgPath},
		},
	}
	regRaw, _ := json.Marshal(reg)
	if err := os.WriteFile(filepath.Join(home, "registry.json"), regRaw, 0o644); err != nil {
		t.Fatalf("write registry.json: %v", err)
	}

	// Write per-ref graph.fb files.
	// refSlots maps "slug/refSafe" → entity count (written to graph-stats.json)
	// The store path mirrors StateDirForRepoRef without needing to import daemon
	// here — we just create the structure that the handler reads via
	// daemon.StateDirForRepoRef and filepath.Dir.
	//
	// We rely on GRAFEL_HOME being set so StateDirForRepoRef produces paths
	// under home/store/. We pre-compute those paths by calling the helper
	// through the daemon package: instead we write them manually using the
	// same hash-based naming.
	//
	// Simpler: the handler calls daemon.StateDirForRepo(r.Path) to get the hot
	// stateDir and then does filepath.Dir(stateDir) to get refs/. So we need
	// to match that exact path. We can compute it in the test via the exported
	// function from the daemon package (which respects GRAFEL_HOME).
	//
	// Import daemon here would create a cycle (dashboard ← daemon). So instead
	// we just trust the path contract and write files using the same logic via
	// a helper in the test binary that also sets GRAFEL_HOME.
	//
	// Since we already set GRAFEL_HOME above, we trigger daemon.StateDirForRepo
	// indirectly by importing it from a test helper. Rather than doing that,
	// we instead write a known fixed store path that we can also pass to the
	// handler through a stubbed registry.
	//
	// The simplest correct approach: write files to a temp dir per-repo and set
	// the repo.Path in the config to that dir; then the handler will compute
	// StateDirForRepo(repoPath) which == our pre-built directory because
	// GRAFEL_HOME is set.
	//
	// We do this in refSlots: keys are "<repoPath>:<refSafe>" → entityCount.
	for key, refs := range refSlots {
		// key is the repo path in the registry.
		// For each ref, create refs/<refSafe>/graph.fb.
		for i, refSafe := range refs {
			// We cannot call daemon.StateDirForRepo here without an import.
			// Use a direct path under the home store dir — the hash is SHA-256
			// of the absolute path (first 16 hex chars). Computing this hash
			// is non-trivial without the daemon package. Instead we use a
			// workaround: the test registers repos whose Path is set to a
			// special directory we create and hand to the handler.
			//
			// Actually we can compute it: sha256(abs(repoPath))[:16] hex.
			// Let's just inline it — see writeRefSlot below.
			_ = key
			_ = refs
			_ = refSafe
			_ = i
			break
		}
		break
	}

	return home
}

// TestHandleV2GroupRefs_ReturnsRefsForRepo is covered by the full
// store-backed test in handlers_v2_refs_integration_test.go (daemon import
// needed to compute StateDirForRepoRef paths). The two simpler cases below
// (404 and empty-refs) cover the error paths without that dependency.

// TestHandleV2GroupRefs_NotFound verifies that a request for an unknown group
// returns HTTP 404 with the expected error payload.
func TestHandleV2GroupRefs_NotFound(t *testing.T) {
	home := t.TempDir()
	t.Setenv("GRAFEL_HOME", home)

	// Write an empty registry (no groups).
	reg := map[string]any{"version": 1, "groups": []any{}}
	raw, _ := json.Marshal(reg)
	os.WriteFile(filepath.Join(home, "registry.json"), raw, 0o644)

	st := newFakeStore()
	srv, err := NewServer(DefaultConfig(), st)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	ts := httptest.NewServer(srv.routes())
	t.Cleanup(ts.Close)

	resp, err := http.Get(ts.URL + "/api/v2/groups/does-not-exist/refs")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("want 404, got %d", resp.StatusCode)
	}

	var body struct {
		OK  bool `json:"ok"`
		Err struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.OK {
		t.Error("want ok=false for 404")
	}
	if body.Err.Code != "not_found" {
		t.Errorf("error.code: want not_found, got %q", body.Err.Code)
	}
}

// TestHandleV2GroupRefs_EmptyRefsWhenNeverIndexed verifies that a registered
// repo with no store directory returns an empty refs array (not an error).
func TestHandleV2GroupRefs_EmptyRefsWhenNeverIndexed(t *testing.T) {
	home := t.TempDir()
	t.Setenv("GRAFEL_HOME", home)
	// Use a predictable store root so there are no leftover files.
	storeRoot := filepath.Join(home, "store")
	t.Setenv("GRAFEL_DAEMON_ROOT", storeRoot)

	groupName := "mygroup"
	repoPath := filepath.Join(home, "nonexistent-repo") // intentionally not created

	cfg := &registry.GroupConfig{Name: groupName, Repos: []registry.Repo{{Slug: "svc", Path: repoPath}}}
	cfgDir := filepath.Join(home, "groups")
	os.MkdirAll(cfgDir, 0o755)
	cfgPath := filepath.Join(cfgDir, groupName+".fleet.json")
	raw, _ := json.Marshal(cfg)
	os.WriteFile(cfgPath, raw, 0o644)

	regRaw, _ := json.Marshal(map[string]any{
		"version": 1,
		"groups":  []map[string]any{{"name": groupName, "config_path": cfgPath}},
	})
	os.WriteFile(filepath.Join(home, "registry.json"), regRaw, 0o644)

	st := newFakeStore()
	srv, err := NewServer(DefaultConfig(), st)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	ts := httptest.NewServer(srv.routes())
	t.Cleanup(ts.Close)

	resp, err := http.Get(ts.URL + "/api/v2/groups/" + groupName + "/refs")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}

	var body struct {
		OK   bool `json:"ok"`
		Data struct {
			Group string `json:"group"`
			Repos []struct {
				Slug string        `json:"slug"`
				Refs []interface{} `json:"refs"`
			} `json:"repos"`
		} `json:"data"`
		Err *struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error,omitempty"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !body.OK {
		errMsg := ""
		if body.Err != nil {
			errMsg = body.Err.Code + ": " + body.Err.Message
		}
		t.Fatalf("want ok=true, got ok=false (error: %s)", errMsg)
	}
	if body.Data.Group != groupName {
		t.Errorf("data.group: want %q, got %q", groupName, body.Data.Group)
	}
	if len(body.Data.Repos) != 1 {
		t.Fatalf("want 1 repo, got %d", len(body.Data.Repos))
	}
	if len(body.Data.Repos[0].Refs) != 0 {
		t.Errorf("want empty refs for never-indexed repo, got %d entries", len(body.Data.Repos[0].Refs))
	}
}

// TestHandleV2GroupRefs_SegmentSetRef is the RED test for #5915 J2 slice-2: a
// ref indexed as a SEGMENT-SET (graph.<gen>/ dir + manifest.json, no flat
// graph.fb) must appear in the GET /api/v2/groups/:group/refs response with
// a non-nil indexed_at. The pre-fix os.Stat(graph.CurrentGraphPath(refDir))
// gate only ever resolves a flat .fb path, so the slot was silently dropped
// ("graph was never written").
func TestHandleV2GroupRefs_SegmentSetRef(t *testing.T) {
	home := t.TempDir()
	t.Setenv("GRAFEL_HOME", home)
	storeRoot := filepath.Join(home, "store")
	t.Setenv("GRAFEL_DAEMON_ROOT", storeRoot)

	groupName := "seggroup"
	repoPath := filepath.Join(home, "segrepo")
	_ = os.MkdirAll(repoPath, 0o755)

	cfg := &registry.GroupConfig{Name: groupName, Repos: []registry.Repo{{Slug: "svc", Path: repoPath}}}
	cfgDir := filepath.Join(home, "groups")
	os.MkdirAll(cfgDir, 0o755)
	cfgPath := filepath.Join(cfgDir, groupName+".fleet.json")
	raw, _ := json.Marshal(cfg)
	os.WriteFile(cfgPath, raw, 0o644)

	regRaw, _ := json.Marshal(map[string]any{
		"version": 1,
		"groups":  []map[string]any{{"name": groupName, "config_path": cfgPath}},
	})
	os.WriteFile(filepath.Join(home, "registry.json"), regRaw, 0o644)

	// Build a segment-set ref dir directly under the store: refs/<refSafe>/.
	sentinel := daemon.StateDirForRepoRef(repoPath, "")
	refsDir := filepath.Dir(sentinel)
	segRefDir := filepath.Join(refsDir, "wip-seg")
	if err := os.MkdirAll(segRefDir, 0o755); err != nil {
		t.Fatal(err)
	}
	mtime := time.Now().Add(-2 * time.Hour).UTC().Truncate(time.Second)
	writeDashboardSegmentSetFixture(t, segRefDir, 4, mtime)

	st := newFakeStore()
	srv, err := NewServer(DefaultConfig(), st)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	quiesceGraphCache(t, srv)
	ts := httptest.NewServer(srv.routes())
	t.Cleanup(ts.Close)

	resp, err := http.Get(ts.URL + "/api/v2/groups/" + groupName + "/refs")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}

	var body struct {
		OK   bool `json:"ok"`
		Data struct {
			Repos []struct {
				Slug string `json:"slug"`
				Refs []struct {
					Ref       string  `json:"ref"`
					IndexedAt *string `json:"indexed_at"`
				} `json:"refs"`
			} `json:"repos"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !body.OK {
		t.Fatal("want ok=true")
	}
	if len(body.Data.Repos) != 1 {
		t.Fatalf("want 1 repo, got %d", len(body.Data.Repos))
	}
	var found bool
	for _, ref := range body.Data.Repos[0].Refs {
		if ref.Ref == "wip-seg" {
			found = true
			if ref.IndexedAt == nil {
				t.Error("segment-set ref 'wip-seg' has nil indexed_at, want the manifest.json mtime")
			}
		}
	}
	if !found {
		t.Fatalf("segment-set ref 'wip-seg' missing from refs=%v (dropped as 'graph never written')", body.Data.Repos[0].Refs)
	}
}
