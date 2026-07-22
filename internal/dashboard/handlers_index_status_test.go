package dashboard

// handlers_index_status_test.go — RED tests for GET
// /api/v2/groups/{group}/index-status (#47 phase 1): the web dashboard wizard
// poll endpoint that exposes the statusfile status-plane's per-repo
// indexing/enhancing flags + engine CPU/RSS to the frontend, mirroring what
// the TUI already reads directly from internal/statusfile.

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/daemon"
	"github.com/cajasmota/grafel/internal/registry"
	"github.com/cajasmota/grafel/internal/statusfile"
)

// seedIndexStatusRegistry wires a temp GRAFEL_HOME registry with one group
// containing the given repo slugs (paths under the temp home), mirroring
// seedQuarantineRegistry's pattern in handlers_quarantine_test.go.
func seedIndexStatusRegistry(t *testing.T, group string, repoSlugs ...string) map[string]string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("GRAFEL_HOME", home)
	cfgHome := filepath.Join(home, "config")
	t.Setenv("XDG_CONFIG_HOME", cfgHome)

	cfgDir := filepath.Join(cfgHome, "grafel")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}

	paths := make(map[string]string, len(repoSlugs))
	var repos []registry.Repo
	for _, slug := range repoSlugs {
		repoPath := filepath.Join(home, "repos", slug)
		if err := os.MkdirAll(repoPath, 0o755); err != nil {
			t.Fatal(err)
		}
		paths[slug] = repoPath
		repos = append(repos, registry.Repo{Slug: slug, Path: repoPath})
	}

	cfgPath := filepath.Join(cfgDir, group+".fleet.json")
	cfg := registry.GroupConfig{Name: group, Repos: repos}
	cfgData, _ := json.MarshalIndent(cfg, "", "  ")
	if err := os.WriteFile(cfgPath, cfgData, 0o644); err != nil {
		t.Fatal(err)
	}
	reg := registry.Registry{Version: 1, Groups: []registry.GroupRef{{Name: group, ConfigPath: cfgPath}}}
	regData, _ := json.MarshalIndent(reg, "", "  ")
	if err := os.WriteFile(filepath.Join(home, "registry.json"), regData, 0o644); err != nil {
		t.Fatal(err)
	}
	return paths
}

// writeIndexStatusFixture writes a per-repo statusfile.File for repoPath.
func writeIndexStatusFixture(t *testing.T, repoPath string, indexing, enhancing bool, entities, relationships, graphFBMtime int64) {
	t.Helper()
	f := &statusfile.File{
		RepoPath:      repoPath,
		HeartbeatAt:   time.Now().UTC(),
		Indexing:      indexing,
		Enhancing:     enhancing,
		Entities:      entities,
		Relationships: relationships,
		GraphFBMtime:  graphFBMtime,
	}
	if err := statusfile.Write(repoPath, f); err != nil {
		t.Fatalf("write status fixture for %s: %v", repoPath, err)
	}
}

// writeIndexStatusReindexFixture writes a per-repo statusfile.File with
// ReindexRequired/ReindexReason set (#5907 FIX1/FIX2's fields), optionally
// while an auto-enqueued reindex is actively running (indexing=true).
func writeIndexStatusReindexFixture(t *testing.T, repoPath string, indexing bool, reason string) {
	t.Helper()
	f := &statusfile.File{
		RepoPath:        repoPath,
		HeartbeatAt:     time.Now().UTC(),
		Indexing:        indexing,
		ReindexRequired: true,
		ReindexReason:   reason,
	}
	if err := statusfile.Write(repoPath, f); err != nil {
		t.Fatalf("write reindex status fixture for %s: %v", repoPath, err)
	}
}

// writeEngineLivenessFixture writes the engine-global liveness sidecar with
// the given CPU%/RSS, using the SAME (daemon.DefaultLayout + EngineLivenessStatusKey)
// derivation the production writer/reader use, so the handler under test
// reads back exactly what we wrote here.
func writeEngineLivenessFixture(t *testing.T, cpuPct float64, rssMB int64) {
	t.Helper()
	layout, err := daemon.DefaultLayout()
	if err != nil {
		t.Fatalf("daemon.DefaultLayout: %v", err)
	}
	key := daemon.EngineLivenessStatusKey(layout.Root)
	f := &statusfile.File{
		HeartbeatAt: time.Now().UTC(),
		CPUPct:      cpuPct,
		RSSMB:       rssMB,
	}
	if err := statusfile.Write(key, f); err != nil {
		t.Fatalf("write engine liveness fixture: %v", err)
	}
}

func newIndexStatusServer(t *testing.T) (string, func()) {
	t.Helper()
	return newTestServer(t, newFakeStore(), DefaultConfig())
}

// indexStatusReplyWire mirrors the handler's wire response shape for
// decoding in tests (the handler wraps this in the v2 envelope:
// {"ok":true,"data":{...}}). Named distinctly from the production
// indexStatusReply type (handlers_index_status.go) to avoid a redeclaration.
type indexStatusReplyWire struct {
	Engine struct {
		CPUPct float64 `json:"cpu_pct"`
		RSSMB  int64   `json:"rss_mb"`
	} `json:"engine"`
	Repos []struct {
		RepoSlug        string `json:"repo_slug"`
		Indexing        bool   `json:"indexing"`
		Enhancing       bool   `json:"enhancing"`
		Entities        int64  `json:"entities"`
		Relationships   int64  `json:"relationships"`
		GraphFBMtime    int64  `json:"graph_fb_mtime"`
		ReindexRequired bool   `json:"reindex_required"`
		ReindexReason   string `json:"reindex_reason"`
		Status          string `json:"status"`
	} `json:"repos"`
}

type indexStatusEnvelope struct {
	OK   bool                 `json:"ok"`
	Data indexStatusReplyWire `json:"data"`
}

func TestIndexStatusEndpoint_ReflectsPerRepoAndEngineMetrics(t *testing.T) {
	paths := seedIndexStatusRegistry(t, "demo", "api", "web")
	writeIndexStatusFixture(t, paths["api"], true, false, 100, 200, 111)
	writeIndexStatusFixture(t, paths["web"], false, true, 300, 400, 222)
	writeEngineLivenessFixture(t, 142.5, 512)

	url, done := newIndexStatusServer(t)
	defer done()

	resp, err := http.Get(url + "/api/v2/groups/demo/index-status")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
	var env indexStatusEnvelope
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !env.OK {
		t.Fatal("expected ok=true envelope")
	}

	if env.Data.Engine.CPUPct != 142.5 {
		t.Errorf("engine.cpu_pct = %v, want 142.5", env.Data.Engine.CPUPct)
	}
	if env.Data.Engine.RSSMB != 512 {
		t.Errorf("engine.rss_mb = %v, want 512", env.Data.Engine.RSSMB)
	}

	if len(env.Data.Repos) != 2 {
		t.Fatalf("repos len = %d, want 2", len(env.Data.Repos))
	}
	byBlug := map[string]int{}
	for i, r := range env.Data.Repos {
		byBlug[r.RepoSlug] = i
	}
	api := env.Data.Repos[byBlug["api"]]
	if !api.Indexing || api.Enhancing {
		t.Errorf("api repo: indexing=%v enhancing=%v, want indexing=true enhancing=false", api.Indexing, api.Enhancing)
	}
	if api.Entities != 100 || api.Relationships != 200 || api.GraphFBMtime != 111 {
		t.Errorf("api repo counts wrong: %+v", api)
	}

	web := env.Data.Repos[byBlug["web"]]
	if web.Indexing || !web.Enhancing {
		t.Errorf("web repo: indexing=%v enhancing=%v, want indexing=false enhancing=true", web.Indexing, web.Enhancing)
	}
	if web.Entities != 300 || web.Relationships != 400 || web.GraphFBMtime != 222 {
		t.Errorf("web repo counts wrong: %+v", web)
	}
}

// TestIndexStatusEndpoint_MissingStatusFileYieldsZeroRow ensures a repo the
// engine has never touched (no status-plane sidecar written yet) is reported
// as a normal zero/false row rather than causing the whole endpoint to error.
func TestIndexStatusEndpoint_MissingStatusFileYieldsZeroRow(t *testing.T) {
	seedIndexStatusRegistry(t, "demo", "untouched")

	url, done := newIndexStatusServer(t)
	defer done()

	resp, err := http.Get(url + "/api/v2/groups/demo/index-status")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
	var env indexStatusEnvelope
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(env.Data.Repos) != 1 {
		t.Fatalf("repos len = %d, want 1", len(env.Data.Repos))
	}
	r := env.Data.Repos[0]
	if r.RepoSlug != "untouched" {
		t.Errorf("repo_slug = %q, want %q", r.RepoSlug, "untouched")
	}
	if r.Indexing || r.Enhancing || r.Entities != 0 || r.Relationships != 0 || r.GraphFBMtime != 0 {
		t.Errorf("untouched repo should be zero/false row, got %+v", r)
	}
	if r.ReindexRequired {
		t.Errorf("untouched repo should have reindex_required=false, got true")
	}
	if r.Status != "idle" {
		t.Errorf("untouched repo status = %q, want %q", r.Status, "idle")
	}
}

// TestIndexStatusEndpoint_ReindexRequired_Surfaced is the FIX3 (#5907) RED
// test: before this change, ReindexRequired/ReindexReason were dropped when
// building indexStatusRepo, so a repo whose graph.fb needed a reindex after a
// format upgrade read as an idle row to the web dashboard — a silent stall.
// This proves the fields are surfaced verbatim, plus the derived `status`:
// "reindexing-after-upgrade" once the auto-enqueued reindex is actively
// running, "reindex-required" while it's still queued.
func TestIndexStatusEndpoint_ReindexRequired_Surfaced(t *testing.T) {
	paths := seedIndexStatusRegistry(t, "demo", "queued", "running")
	writeIndexStatusReindexFixture(t, paths["queued"], false, "graph format v2 incompatible with v3 — reindex required")
	writeIndexStatusReindexFixture(t, paths["running"], true, "graph format v2 incompatible with v3 — reindex required")

	url, done := newIndexStatusServer(t)
	defer done()

	resp, err := http.Get(url + "/api/v2/groups/demo/index-status")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
	var env indexStatusEnvelope
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	byBlug := map[string]int{}
	for i, r := range env.Data.Repos {
		byBlug[r.RepoSlug] = i
	}

	queued := env.Data.Repos[byBlug["queued"]]
	if !queued.ReindexRequired {
		t.Error("queued repo: expected reindex_required=true")
	}
	if queued.ReindexReason == "" {
		t.Error("queued repo: expected a non-empty reindex_reason")
	}
	if queued.Status != "reindex-required" {
		t.Errorf("queued repo status = %q, want %q", queued.Status, "reindex-required")
	}

	running := env.Data.Repos[byBlug["running"]]
	if !running.ReindexRequired {
		t.Error("running repo: expected reindex_required=true")
	}
	if !running.Indexing {
		t.Error("running repo: expected indexing=true")
	}
	if running.Status != "reindexing-after-upgrade" {
		t.Errorf("running repo status = %q, want %q", running.Status, "reindexing-after-upgrade")
	}
}

// TestIndexStatusEndpoint_UnknownGroup404s ensures the endpoint is read-only
// and 404s cleanly for a group the registry doesn't know about — never a
// panic, never a write, never an index trigger.
func TestIndexStatusEndpoint_UnknownGroup404s(t *testing.T) {
	seedIndexStatusRegistry(t, "demo", "api")

	url, done := newIndexStatusServer(t)
	defer done()

	resp, err := http.Get(url + "/api/v2/groups/nonexistent/index-status")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
}
