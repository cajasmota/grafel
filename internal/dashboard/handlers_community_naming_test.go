package dashboard

// handlers_community_naming_test.go — HTTP-level tests for
//
//	GET /api/community-naming/{group}   (#1301)

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// newCommunityNamingServer creates a minimal Server with one repo whose
// enrichment-candidates.json contains a mix of name_community and
// describe_entity candidates. Returned along with the repo's state dir.
func newCommunityNamingServer(t *testing.T) (*Server, string) {
	t.Helper()
	cfg := DefaultConfig()
	srv, err := NewServer(cfg, newFakeStore())
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	// Create a temporary repo directory with an archigraph state dir.
	repoDir := t.TempDir()
	stateDir := filepath.Join(repoDir, ".archigraph")
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatalf("mkdir state: %v", err)
	}

	// Write a candidates file with mixed kinds.
	candidates := []map[string]any{
		{
			"id":         "cn:community:1",
			"kind":       "name_community",
			"task_type":  "community",
			"subject_id": "community:1",
			"context":    map[string]any{"auto_name": "AuthCluster", "size": 42, "top_entities": []string{"UserService", "TokenValidator"}},
			"confidence_floor": 0.6,
			"discovered_at":    "2026-05-01T00:00:00Z",
		},
		{
			"id":         "cn:community:2",
			"kind":       "name_community",
			"task_type":  "community",
			"subject_id": "community:2",
			"context":    map[string]any{"auto_name": "BillingCluster", "size": 17, "top_entities": []string{"InvoiceService"}},
			"confidence_floor": 0.6,
			"discovered_at":    "2026-05-01T00:00:00Z",
		},
		{
			"id":         "ec:entity:abc",
			"kind":       "describe_entity",
			"task_type":  "entity",
			"subject_id": "entity:abc",
			"context":    map[string]any{"name": "AuthHandler", "kind": "Service"},
			"score":      75,
			"discovered_at": "2026-05-01T00:00:00Z",
		},
	}
	data, _ := json.Marshal(map[string]any{"version": 2, "candidates": candidates})
	if err := os.WriteFile(filepath.Join(stateDir, "enrichment-candidates.json"), data, 0o644); err != nil {
		t.Fatalf("write candidates: %v", err)
	}

	// Seed group "g1" into the graph cache with the temp repo.
	srv.graphs.mu.Lock()
	srv.graphs.entries["g1"] = &cacheEntry{
		group: &DashGroup{
			Name: "g1",
			Repos: map[string]*DashRepo{
				"repo1": {Slug: "repo1", Path: repoDir},
			},
		},
		loadedAt: time.Now().Add(60 * time.Second),
	}
	srv.graphs.mu.Unlock()
	return srv, stateDir
}

func TestHandleCommunityNaming_ReturnsCommunityKindsOnly(t *testing.T) {
	srv, _ := newCommunityNamingServer(t)
	h := srv.routes()

	req := httptest.NewRequest(http.MethodGet, "/api/community-naming/g1", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	items, ok := resp["items"].([]any)
	if !ok {
		t.Fatalf("items is not an array: %T", resp["items"])
	}
	total := resp["total"].(float64)

	if got := len(items); got != 2 {
		t.Errorf("want 2 community items, got %d", got)
	}
	if total != 2 {
		t.Errorf("want total=2, got %v", total)
	}

	// Verify only name_community kinds.
	for _, raw := range items {
		item := raw.(map[string]any)
		if kind, _ := item["kind"].(string); kind != "name_community" {
			t.Errorf("unexpected kind %q in community-naming response", kind)
		}
	}
}

func TestHandleCommunityNaming_EnrichmentsExcludesCommunity(t *testing.T) {
	// Verify that /api/enrichments/{group} no longer includes name_community rows.
	srv, _ := newCommunityNamingServer(t)
	h := srv.routes()

	req := httptest.NewRequest(http.MethodGet, "/api/enrichments/g1", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	items, ok := resp["items"].([]any)
	if !ok {
		t.Fatalf("items is not an array: %T", resp["items"])
	}

	// Only describe_entity should be present (1 item).
	if got := len(items); got != 1 {
		t.Errorf("want 1 entity enrichment item (describe_entity), got %d", got)
	}
	for _, raw := range items {
		item := raw.(map[string]any)
		if kind, _ := item["kind"].(string); kind == "name_community" {
			t.Errorf("name_community should not appear in /api/enrichments response")
		}
	}
}

func TestHandleCommunityNaming_MissingGroup(t *testing.T) {
	srv, _ := newCommunityNamingServer(t)
	h := srv.routes()

	req := httptest.NewRequest(http.MethodGet, "/api/community-naming/nonexistent", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Errorf("want 404 for missing group, got %d", rr.Code)
	}
}
