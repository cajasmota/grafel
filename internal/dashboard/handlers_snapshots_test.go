package dashboard

// handlers_snapshots_test.go — unit + integration tests for the graph snapshot surface (#1353).
//
// Coverage:
//   - computeDiff: added/removed entity + edge counting with kind filter
//   - snapshotID: format sanity
//   - POST /api/snapshots/{group}: 400 on missing name, 404 on unknown group
//   - GET /api/snapshots/{group}: returns [] when no snapshots exist
//   - DELETE /api/snapshots/{group}/{id}: rejects path-traversal ids
//   - GET /api/snapshots/{group}/{id}/diff: 404 on unknown snapshot id
//   - Full round-trip: save → list → diff (entities appear in added set)

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cajasmota/archigraph/internal/graph"
	"github.com/cajasmota/archigraph/internal/registry"
)

// ---------------------------------------------------------------------------
// Unit tests — computeDiff
// ---------------------------------------------------------------------------

func makeGraphJSON(t *testing.T, entities []graph.Entity, rels []graph.Relationship) []byte {
	t.Helper()
	doc := graph.Document{
		Version:       1,
		Repo:          "testrepo",
		Entities:      entities,
		Relationships: rels,
		Stats: graph.Stats{
			Entities:      len(entities),
			Relationships: len(rels),
		},
	}
	b, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal graph: %v", err)
	}
	return b
}

func TestComputeDiff_AddedEntities(t *testing.T) {
	lhs := map[string][]byte{
		"repo": makeGraphJSON(t, []graph.Entity{
			{ID: "e1", Name: "Foo", Kind: "Function"},
		}, nil),
	}
	rhs := map[string][]byte{
		"repo": makeGraphJSON(t, []graph.Entity{
			{ID: "e1", Name: "Foo", Kind: "Function"},
			{ID: "e2", Name: "Bar", Kind: "Function"},
		}, nil),
	}
	summary := computeDiff("snap1", "grp", "current", lhs, rhs, "")
	if summary.AddedCount != 1 {
		t.Errorf("want AddedCount=1, got %d", summary.AddedCount)
	}
	if summary.RemovedCount != 0 {
		t.Errorf("want RemovedCount=0, got %d", summary.RemovedCount)
	}
	if len(summary.Added) != 1 || summary.Added[0].Name != "Bar" {
		t.Errorf("want Added=[Bar], got %v", summary.Added)
	}
}

func TestComputeDiff_RemovedEntities(t *testing.T) {
	lhs := map[string][]byte{
		"repo": makeGraphJSON(t, []graph.Entity{
			{ID: "e1", Name: "Foo", Kind: "Function"},
			{ID: "e2", Name: "Bar", Kind: "Function"},
		}, nil),
	}
	rhs := map[string][]byte{
		"repo": makeGraphJSON(t, []graph.Entity{
			{ID: "e1", Name: "Foo", Kind: "Function"},
		}, nil),
	}
	summary := computeDiff("snap1", "grp", "current", lhs, rhs, "")
	if summary.RemovedCount != 1 {
		t.Errorf("want RemovedCount=1, got %d", summary.RemovedCount)
	}
	if len(summary.Removed) != 1 || summary.Removed[0].Name != "Bar" {
		t.Errorf("want Removed=[Bar], got %v", summary.Removed)
	}
}

func TestComputeDiff_KindFilter(t *testing.T) {
	lhs := map[string][]byte{
		"repo": makeGraphJSON(t, []graph.Entity{
			{ID: "e1", Name: "FooService", Kind: "Service"},
		}, nil),
	}
	rhs := map[string][]byte{
		"repo": makeGraphJSON(t, []graph.Entity{
			{ID: "e1", Name: "FooService", Kind: "Service"},
			{ID: "e2", Name: "barFunc", Kind: "Function"},
		}, nil),
	}
	// Filter to Service only — barFunc should not appear in added.
	summary := computeDiff("snap1", "grp", "current", lhs, rhs, "Service")
	if summary.AddedCount != 0 {
		t.Errorf("want AddedCount=0 (kind filter), got %d (added: %v)", summary.AddedCount, summary.Added)
	}
}

func TestComputeDiff_EdgeChanges(t *testing.T) {
	lhsRels := []graph.Relationship{{ID: "r1", FromID: "e1", ToID: "e2", Kind: "calls"}}
	rhsRels := []graph.Relationship{
		{ID: "r1", FromID: "e1", ToID: "e2", Kind: "calls"},
		{ID: "r2", FromID: "e2", ToID: "e3", Kind: "calls"},
	}
	lhs := map[string][]byte{
		"repo": makeGraphJSON(t, []graph.Entity{{ID: "e1"}, {ID: "e2"}}, lhsRels),
	}
	rhs := map[string][]byte{
		"repo": makeGraphJSON(t, []graph.Entity{{ID: "e1"}, {ID: "e2"}, {ID: "e3"}}, rhsRels),
	}
	summary := computeDiff("snap1", "grp", "current", lhs, rhs, "")
	if summary.EdgeAdded != 1 {
		t.Errorf("want EdgeAdded=1, got %d", summary.EdgeAdded)
	}
	if summary.EdgeRemoved != 0 {
		t.Errorf("want EdgeRemoved=0, got %d", summary.EdgeRemoved)
	}
}

func TestComputeDiff_NoDiff(t *testing.T) {
	data := map[string][]byte{
		"repo": makeGraphJSON(t, []graph.Entity{{ID: "e1", Name: "Foo", Kind: "Function"}}, nil),
	}
	summary := computeDiff("snap1", "grp", "current", data, data, "")
	if summary.AddedCount != 0 || summary.RemovedCount != 0 {
		t.Errorf("want zero diff, got added=%d removed=%d", summary.AddedCount, summary.RemovedCount)
	}
}

// ---------------------------------------------------------------------------
// Unit tests — snapshotID
// ---------------------------------------------------------------------------

func TestSnapshotID_Format(t *testing.T) {
	id := snapshotID()
	// Format: 20060102T150405Z — 16 chars, ends with Z, no path separators.
	if len(id) != 16 {
		t.Errorf("want len=16, got %d (%q)", len(id), id)
	}
	if !strings.HasSuffix(id, "Z") {
		t.Errorf("want Z suffix, got %q", id)
	}
	if strings.ContainsAny(id, "/\\") {
		t.Errorf("id must not contain path separators: %q", id)
	}
}

// ---------------------------------------------------------------------------
// Helpers for HTTP integration tests
// ---------------------------------------------------------------------------

// setupSnapshotEnv sets up a minimal filesystem that lets the snapshot
// handlers find a group "testgroup" with one repo "repo1", honoring
// ARCHIGRAPH_HOME so all state stays inside t.TempDir().
//
// It returns:
//   - the Server under test
//   - the absolute path of the fake repo directory (contains graph.json)
func setupSnapshotEnv(t *testing.T) (*Server, string) {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("ARCHIGRAPH_HOME", tmp)

	// Create the fake repo directory with a graph.json.
	repoDir := filepath.Join(tmp, "repos", "repo1")
	archigraphDir := filepath.Join(repoDir, ".archigraph")
	if err := os.MkdirAll(archigraphDir, 0o755); err != nil {
		t.Fatalf("mkdir repo: %v", err)
	}
	doc := graph.Document{
		Version: 1,
		Repo:    "repo1",
		Entities: []graph.Entity{
			{ID: "e1", Name: "Alpha", Kind: "Function", SourceFile: "main.go"},
			{ID: "e2", Name: "Beta", Kind: "Service", SourceFile: "svc.go"},
		},
		Relationships: []graph.Relationship{
			{ID: "rel1", FromID: "e1", ToID: "e2", Kind: "calls"},
		},
		Stats: graph.Stats{Entities: 2, Relationships: 1},
	}
	graphBytes, _ := json.Marshal(doc)
	if err := os.WriteFile(filepath.Join(archigraphDir, "graph.json"), graphBytes, 0o644); err != nil {
		t.Fatalf("write graph.json: %v", err)
	}

	// Create registry.json + group config.
	groupCfgDir := filepath.Join(tmp, "groups", "testgroup")
	if err := os.MkdirAll(groupCfgDir, 0o755); err != nil {
		t.Fatalf("mkdir group cfg dir: %v", err)
	}
	groupCfgPath := filepath.Join(groupCfgDir, "config.json")
	groupCfg := registry.GroupConfig{
		Name: "testgroup",
		Repos: []registry.Repo{
			{Slug: "repo1", Path: repoDir},
		},
	}
	if err := registry.SaveGroupConfig(groupCfgPath, &groupCfg); err != nil {
		t.Fatalf("save group config: %v", err)
	}
	reg := struct {
		Version int                  `json:"version"`
		Groups  []registry.GroupRef  `json:"groups"`
	}{
		Version: 1,
		Groups:  []registry.GroupRef{{Name: "testgroup", ConfigPath: groupCfgPath}},
	}
	regBytes, _ := json.MarshalIndent(reg, "", "  ")
	if err := os.WriteFile(filepath.Join(tmp, "registry.json"), regBytes, 0o644); err != nil {
		t.Fatalf("write registry.json: %v", err)
	}

	srv, err := NewServer(DefaultConfig(), newFakeStore())
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	return srv, repoDir
}

// ---------------------------------------------------------------------------
// HTTP handler tests — validation / error paths
// ---------------------------------------------------------------------------

func TestHandleSaveSnapshot_MissingName(t *testing.T) {
	srv, _ := setupSnapshotEnv(t)

	body := bytes.NewBufferString(`{"description":"no name"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/snapshots/testgroup", body)
	req.SetPathValue("group", "testgroup")
	w := httptest.NewRecorder()
	srv.handleSaveSnapshot(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("want 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleSaveSnapshot_UnknownGroup(t *testing.T) {
	srv, _ := setupSnapshotEnv(t)

	body := bytes.NewBufferString(`{"name":"snap"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/snapshots/nogroup", body)
	req.SetPathValue("group", "nogroup")
	w := httptest.NewRecorder()
	srv.handleSaveSnapshot(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("want 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleListSnapshots_EmptyGroup(t *testing.T) {
	srv, _ := setupSnapshotEnv(t)

	req := httptest.NewRequest(http.MethodGet, "/api/snapshots/testgroup", nil)
	req.SetPathValue("group", "testgroup")
	w := httptest.NewRecorder()
	srv.handleListSnapshots(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}
	var reply map[string]any
	if err := json.NewDecoder(w.Body).Decode(&reply); err != nil {
		t.Fatalf("decode: %v", err)
	}
	snaps, ok := reply["snapshots"].([]any)
	if !ok || len(snaps) != 0 {
		t.Errorf("want empty snapshots array, got %v", reply["snapshots"])
	}
}

func TestHandleDeleteSnapshot_PathTraversal(t *testing.T) {
	srv, _ := setupSnapshotEnv(t)

	req := httptest.NewRequest(http.MethodDelete, "/api/snapshots/testgroup/../../etc", nil)
	req.SetPathValue("group", "testgroup")
	req.SetPathValue("id", "../../etc")
	w := httptest.NewRecorder()
	srv.handleDeleteSnapshot(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("want 400 for path traversal, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleSnapshotDiff_UnknownSnapshot(t *testing.T) {
	srv, _ := setupSnapshotEnv(t)

	req := httptest.NewRequest(http.MethodGet, "/api/snapshots/testgroup/20991231T235959Z/diff", nil)
	req.SetPathValue("group", "testgroup")
	req.SetPathValue("id", "20991231T235959Z")
	w := httptest.NewRecorder()
	srv.handleSnapshotDiff(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("want 404, got %d: %s", w.Code, w.Body.String())
	}
}

// ---------------------------------------------------------------------------
// Full round-trip: save → list → diff
// ---------------------------------------------------------------------------

func TestSnapshotRoundTrip(t *testing.T) {
	srv, _ := setupSnapshotEnv(t)

	// 1. Save a snapshot.
	body := bytes.NewBufferString(`{"name":"before-refactor","description":"initial state"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/snapshots/testgroup", body)
	req.SetPathValue("group", "testgroup")
	w := httptest.NewRecorder()
	srv.handleSaveSnapshot(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("save snapshot: want 201, got %d: %s", w.Code, w.Body.String())
	}
	var saveReply struct {
		Snapshot SnapshotMeta `json:"snapshot"`
	}
	if err := json.NewDecoder(w.Body).Decode(&saveReply); err != nil {
		t.Fatalf("decode save reply: %v", err)
	}
	snapID := saveReply.Snapshot.ID
	if snapID == "" {
		t.Fatal("snapshot id should not be empty")
	}
	if saveReply.Snapshot.Name != "before-refactor" {
		t.Errorf("want name=before-refactor, got %q", saveReply.Snapshot.Name)
	}
	if saveReply.Snapshot.Stats["repo1"] != 2 {
		t.Errorf("want stats[repo1]=2, got %d", saveReply.Snapshot.Stats["repo1"])
	}

	// 2. List snapshots — should have one entry.
	req2 := httptest.NewRequest(http.MethodGet, "/api/snapshots/testgroup", nil)
	req2.SetPathValue("group", "testgroup")
	w2 := httptest.NewRecorder()
	srv.handleListSnapshots(w2, req2)

	if w2.Code != http.StatusOK {
		t.Fatalf("list snapshots: want 200, got %d: %s", w2.Code, w2.Body.String())
	}
	var listReply struct {
		Snapshots []SnapshotMeta `json:"snapshots"`
	}
	if err := json.NewDecoder(w2.Body).Decode(&listReply); err != nil {
		t.Fatalf("decode list reply: %v", err)
	}
	if len(listReply.Snapshots) != 1 {
		t.Fatalf("want 1 snapshot, got %d", len(listReply.Snapshots))
	}
	if listReply.Snapshots[0].ID != snapID {
		t.Errorf("want snapshot id %q, got %q", snapID, listReply.Snapshots[0].ID)
	}

	// 3. Diff snapshot vs current (same data → no changes expected).
	url := fmt.Sprintf("/api/snapshots/testgroup/%s/diff", snapID)
	req3 := httptest.NewRequest(http.MethodGet, url, nil)
	req3.SetPathValue("group", "testgroup")
	req3.SetPathValue("id", snapID)
	w3 := httptest.NewRecorder()
	srv.handleSnapshotDiff(w3, req3)

	if w3.Code != http.StatusOK {
		t.Fatalf("diff: want 200, got %d: %s", w3.Code, w3.Body.String())
	}
	var diff DiffSummary
	if err := json.NewDecoder(w3.Body).Decode(&diff); err != nil {
		t.Fatalf("decode diff: %v", err)
	}
	if diff.AddedCount != 0 || diff.RemovedCount != 0 {
		t.Errorf("diff against same state: want 0 changes, got added=%d removed=%d", diff.AddedCount, diff.RemovedCount)
	}
	if diff.Group != "testgroup" {
		t.Errorf("want group=testgroup, got %q", diff.Group)
	}
	if diff.Against != "current" {
		t.Errorf("want against=current, got %q", diff.Against)
	}
}

// TestSnapshotDiff_DetectsNewEntity verifies that entities added to the live
// graph after a snapshot was taken appear in the diff's "added" set.
func TestSnapshotDiff_DetectsNewEntity(t *testing.T) {
	srv, repoDir := setupSnapshotEnv(t)

	// 1. Save snapshot (2 entities).
	body := bytes.NewBufferString(`{"name":"baseline"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/snapshots/testgroup", body)
	req.SetPathValue("group", "testgroup")
	w := httptest.NewRecorder()
	srv.handleSaveSnapshot(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("save: %d %s", w.Code, w.Body.String())
	}
	var saveReply struct {
		Snapshot SnapshotMeta `json:"snapshot"`
	}
	_ = json.NewDecoder(w.Body).Decode(&saveReply)
	snapID := saveReply.Snapshot.ID

	// Small sleep to guarantee snapshot ID is different from any second save.
	time.Sleep(2 * time.Second)

	// 2. Add a third entity to the live graph.
	archigraphDir := filepath.Join(repoDir, ".archigraph")
	doc := graph.Document{
		Version: 1,
		Repo:    "repo1",
		Entities: []graph.Entity{
			{ID: "e1", Name: "Alpha", Kind: "Function", SourceFile: "main.go"},
			{ID: "e2", Name: "Beta", Kind: "Service", SourceFile: "svc.go"},
			{ID: "e3", Name: "Gamma", Kind: "Function", SourceFile: "gamma.go"},
		},
		Stats: graph.Stats{Entities: 3},
	}
	b, _ := json.Marshal(doc)
	if err := os.WriteFile(filepath.Join(archigraphDir, "graph.json"), b, 0o644); err != nil {
		t.Fatalf("update graph.json: %v", err)
	}

	// 3. Diff snapshot vs current — Gamma should appear as added.
	url := fmt.Sprintf("/api/snapshots/testgroup/%s/diff", snapID)
	req3 := httptest.NewRequest(http.MethodGet, url, nil)
	req3.SetPathValue("group", "testgroup")
	req3.SetPathValue("id", snapID)
	w3 := httptest.NewRecorder()
	srv.handleSnapshotDiff(w3, req3)

	if w3.Code != http.StatusOK {
		t.Fatalf("diff: %d %s", w3.Code, w3.Body.String())
	}
	var diff DiffSummary
	_ = json.NewDecoder(w3.Body).Decode(&diff)
	if diff.AddedCount != 1 {
		t.Errorf("want AddedCount=1 (Gamma), got %d", diff.AddedCount)
	}
	if len(diff.Added) != 1 || diff.Added[0].Name != "Gamma" {
		t.Errorf("want Added=[Gamma], got %v", diff.Added)
	}
}
