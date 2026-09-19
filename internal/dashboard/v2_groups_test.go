package dashboard

import (
	"bytes"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/quality"
)

// decodeV2Group is the wire shape returned by the v2 group endpoints.
type decodeV2Group struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Repos       []string `json:"repos"`
	EntityCount int      `json:"entityCount"`
	Fidelity    *float64 `json:"fidelity"`
	IndexedAt   *int64   `json:"indexedAt"`
	Health      string   `json:"health"`
}

// TestV2Groups_RichShape verifies GET /api/v2/groups returns the paginated
// rich Group list with derived health fields.
func TestV2Groups_RichShape(t *testing.T) {
	st := newFakeStore()
	st.groups["indexed"] = GroupSummary{
		Name:        "indexed",
		ConfigPath:  "/i.json",
		Repos:       []string{"a", "b"},
		EntityCount: 1200,
		LastIndexed: time.Now().UTC().Format(time.RFC3339),
	}
	st.groups["fresh"] = GroupSummary{
		Name:       "fresh",
		ConfigPath: "/f.json",
		Repos:      []string{},
	}
	srv, err := NewServer(DefaultConfig(), st)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	ts := httptest.NewServer(srv.routes())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/v2/groups")
	if err != nil {
		t.Fatalf("GET /api/v2/groups: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}

	var body struct {
		OK         bool            `json:"ok"`
		Data       []decodeV2Group `json:"data"`
		Pagination V2Pagination    `json:"pagination"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !body.OK {
		t.Error("ok: want true")
	}
	if body.Pagination.Total != 2 {
		t.Errorf("pagination.total: want 2, got %d", body.Pagination.Total)
	}
	if len(body.Data) != 2 {
		t.Fatalf("data: want 2 groups, got %d", len(body.Data))
	}

	byID := map[string]decodeV2Group{}
	for _, g := range body.Data {
		byID[g.ID] = g
	}

	indexed, ok := byID["indexed"]
	if !ok {
		t.Fatal("missing 'indexed' group")
	}
	if indexed.Health != healthHealthy {
		t.Errorf("indexed.health: want healthy, got %q", indexed.Health)
	}
	if indexed.EntityCount != 1200 {
		t.Errorf("indexed.entityCount: want 1200, got %d", indexed.EntityCount)
	}
	if indexed.Fidelity == nil {
		t.Error("indexed.fidelity: want non-null")
	}
	if indexed.IndexedAt == nil {
		t.Error("indexed.indexedAt: want non-null")
	}
	if len(indexed.Repos) != 2 {
		t.Errorf("indexed.repos: want 2, got %v", indexed.Repos)
	}

	fresh, ok := byID["fresh"]
	if !ok {
		t.Fatal("missing 'fresh' group")
	}
	if fresh.Health != healthUnindexed {
		t.Errorf("fresh.health: want unindexed, got %q", fresh.Health)
	}
	if fresh.Fidelity != nil {
		t.Errorf("fresh.fidelity: want null, got %v", *fresh.Fidelity)
	}
	if fresh.IndexedAt != nil {
		t.Errorf("fresh.indexedAt: want null, got %v", *fresh.IndexedAt)
	}
	if fresh.Repos == nil {
		t.Error("fresh.repos: want [] not null")
	}
}

// TestV2Groups_EmptyRegistry verifies an empty registry returns an empty data
// array (not null), so the Landing empty state renders.
func TestV2Groups_EmptyRegistry(t *testing.T) {
	srv, err := NewServer(DefaultConfig(), newFakeStore())
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	ts := httptest.NewServer(srv.routes())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/v2/groups")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	var body struct {
		OK   bool            `json:"ok"`
		Data []decodeV2Group `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !body.OK {
		t.Error("ok: want true")
	}
	if body.Data == nil {
		t.Error("data: want [] not null")
	}
	if len(body.Data) != 0 {
		t.Errorf("data: want empty, got %d", len(body.Data))
	}
}

// TestV2CreateGroup creates a group via POST /api/v2/groups and confirms the
// returned envelope + that it then appears in the list.
func TestV2CreateGroup(t *testing.T) {
	st := newFakeStore()
	srv, err := NewServer(DefaultConfig(), st)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	ts := httptest.NewServer(srv.routes())
	defer ts.Close()

	reqBody := bytes.NewBufferString(`{"name":"newgroup"}`)
	resp, err := http.Post(ts.URL+"/api/v2/groups", "application/json", reqBody)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("want 201, got %d", resp.StatusCode)
	}

	var body struct {
		OK   bool          `json:"ok"`
		Data decodeV2Group `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !body.OK {
		t.Error("ok: want true")
	}
	if body.Data.Name != "newgroup" {
		t.Errorf("name: want newgroup, got %q", body.Data.Name)
	}
	if body.Data.Health != healthUnindexed {
		t.Errorf("health: want unindexed, got %q", body.Data.Health)
	}
}

// TestV2CreateGroup_BadRequest verifies an empty name yields a v2 error.
func TestV2CreateGroup_BadRequest(t *testing.T) {
	srv, err := NewServer(DefaultConfig(), newFakeStore())
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	ts := httptest.NewServer(srv.routes())
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/api/v2/groups", "application/json", bytes.NewBufferString(`{"name":""}`))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", resp.StatusCode)
	}
	var body struct {
		OK    bool `json:"ok"`
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.OK {
		t.Error("ok: want false")
	}
	if body.Error.Code != "bad_request" {
		t.Errorf("error.code: want bad_request, got %q", body.Error.Code)
	}
}

// TestV2Groups_RealFidelityFromHistory verifies that when a health-history entry
// exists for a group, the fidelity returned is 100-bug_rate/100 not 1.0.
func TestV2Groups_RealFidelityFromHistory(t *testing.T) {
	// Write a history entry so latestGroupBugRate can find it.
	histDir := t.TempDir()
	if err := quality.AppendEntry(histDir, quality.HealthEntry{
		Timestamp:   time.Now(),
		Group:       "indexed",
		BugRate:     fptr7283(6.0),
		HealthScore: fptr7283(94.0),
	}); err != nil {
		t.Fatalf("AppendEntry: %v", err)
	}

	st := newFakeStore()
	st.groups["indexed"] = GroupSummary{
		Name:        "indexed",
		ConfigPath:  "/i.json",
		Repos:       []string{"a"},
		EntityCount: 500,
		LastIndexed: time.Now().UTC().Format(time.RFC3339),
	}
	srv, err := NewServer(DefaultConfig(), st)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	// Inject the test history root.
	srv.historyRoot = histDir

	ts := httptest.NewServer(srv.routes())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/v2/groups")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	var body struct {
		OK   bool            `json:"ok"`
		Data []decodeV2Group `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Data) == 0 {
		t.Fatal("no groups returned")
	}
	var g decodeV2Group
	for _, d := range body.Data {
		if d.ID == "indexed" {
			g = d
		}
	}
	if g.ID == "" {
		t.Fatal("group 'indexed' not found in response")
	}
	if g.Fidelity == nil {
		t.Fatal("fidelity: want non-nil")
	}
	// bug_rate=6.0 → fidelity = round((100-6)*10)/1000 = 940/1000 = 0.940
	wantFid := 0.94
	if math.Abs(*g.Fidelity-wantFid) > 1e-9 {
		t.Errorf("fidelity: want %.4f, got %.4f", wantFid, *g.Fidelity)
	}
	if g.Health != healthWarning {
		t.Errorf("health: want %q, got %q", healthWarning, g.Health)
	}
}
