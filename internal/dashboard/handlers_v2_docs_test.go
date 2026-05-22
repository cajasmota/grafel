package dashboard

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cajasmota/archigraph/internal/registry"
)

// buildV2DocsTestServer creates a Server with one group "testgrp" backed by a
// registry config + on-disk generated markdown docs under <repo>/docs/.
// Returns the server and the repo path so callers can inspect the layout.
func buildV2DocsTestServer(t *testing.T) *Server {
	t.Helper()

	home := t.TempDir()
	t.Setenv("ARCHIGRAPH_HOME", home)

	repoPath := t.TempDir()
	docsDir := filepath.Join(repoPath, "docs")

	// Lay out a representative generated-docs tree.
	mustWrite(t, filepath.Join(docsDir, "overview.md"), "# Repo One\n\nTop-level overview.\n")
	mustWrite(t, filepath.Join(docsDir, "modules", "order-service", "README.md"), "# Order Service\n\nModule deep-dive.\n")
	mustWrite(t, filepath.Join(docsDir, "reference", "api.md"), "# API Reference\n\nEndpoints.\n")
	mustWrite(t, filepath.Join(docsDir, "patterns", "structural", "repository.md"), "# Repository Pattern\n")
	// Enrichment frontmatter files must be excluded from the portal tree.
	mustWrite(t, filepath.Join(docsDir, "enrichments", "http_endpoint", "ep1.md"), "---\nsummary: x\n---\n")

	// Register the group config pointing at the repo.
	cfgPath := filepath.Join(home, "testgrp.json")
	cfg := &registry.GroupConfig{
		Name:  "testgrp",
		Repos: []registry.Repo{{Slug: "repo1", Path: repoPath}},
	}
	if err := registry.SaveGroupConfig(cfgPath, cfg); err != nil {
		t.Fatalf("SaveGroupConfig: %v", err)
	}
	if err := registry.AddGroup("testgrp", cfgPath); err != nil {
		t.Fatalf("AddGroup: %v", err)
	}

	srv, err := NewServer(DefaultConfig(), newFakeStore())
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	// Group must exist in the in-memory cache for the handlers' guard check.
	srv.graphs.mu.Lock()
	srv.graphs.entries["testgrp"] = &cacheEntry{
		group:    &DashGroup{Name: "testgrp", Repos: map[string]*DashRepo{"repo1": {Slug: "repo1", Path: repoPath}}},
		loadedAt: time.Now().Add(60 * time.Second),
	}
	srv.graphs.mu.Unlock()

	return srv
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func TestHandleV2DocsTree(t *testing.T) {
	srv := buildV2DocsTestServer(t)
	r := httptest.NewRequest("GET", "/api/v2/groups/testgrp/docs/tree", nil)
	w := httptest.NewRecorder()
	srv.routes().ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var env v2Envelope
	if err := json.NewDecoder(w.Body).Decode(&env); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	if !env.OK {
		t.Fatal("envelope.ok is false")
	}
	data, ok := env.Data.([]interface{})
	if !ok || len(data) == 0 {
		t.Fatalf("expected non-empty tree, got %T %v", env.Data, env.Data)
	}
	// Root is the repo folder; it should contain category folders.
	repo, _ := data[0].(map[string]interface{})
	if repo["type"] != "folder" || repo["name"] != "repo1" {
		t.Fatalf("expected repo folder, got %v", repo)
	}
	cats, _ := repo["children"].([]interface{})
	if len(cats) != 4 {
		t.Fatalf("expected 4 category folders (overview/modules/reference/patterns), got %d: %v", len(cats), cats)
	}
	// First category should be Overview (deterministic order).
	first, _ := cats[0].(map[string]interface{})
	if first["name"] != "Overview" {
		t.Errorf("expected first category=Overview, got %v", first["name"])
	}
	// Enrichments must not appear as a category.
	for _, c := range cats {
		cm, _ := c.(map[string]interface{})
		if cm["category"] == "guide" {
			// enrichments would land in "Guides" — assert it isn't there
			t.Errorf("enrichments leaked into doc tree: %v", cm)
		}
	}
}

func TestHandleV2DocsPage(t *testing.T) {
	srv := buildV2DocsTestServer(t)
	r := httptest.NewRequest("GET", "/api/v2/groups/testgrp/docs/page?path=repo1/overview.md", nil)
	w := httptest.NewRecorder()
	srv.routes().ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var env v2Envelope
	if err := json.NewDecoder(w.Body).Decode(&env); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	obj, ok := env.Data.(map[string]interface{})
	if !ok {
		t.Fatalf("expected object data, got %T", env.Data)
	}
	if obj["title"] != "Repo One" {
		t.Errorf("expected title from H1 'Repo One', got %v", obj["title"])
	}
	md, _ := obj["markdown"].(string)
	if md == "" {
		t.Error("expected non-empty markdown")
	}
}

func TestHandleV2DocsPageTraversal(t *testing.T) {
	srv := buildV2DocsTestServer(t)
	r := httptest.NewRequest("GET", "/api/v2/groups/testgrp/docs/page?path=repo1/../../../etc/passwd", nil)
	w := httptest.NewRecorder()
	srv.routes().ServeHTTP(w, r)
	// filepath.Clean collapses the traversal back inside docRoot → 404 (file absent).
	if w.Code == http.StatusOK {
		t.Fatalf("path traversal returned 200: %s", w.Body.String())
	}
}

func TestHandleV2DocsPageMissingParam(t *testing.T) {
	srv := buildV2DocsTestServer(t)
	r := httptest.NewRequest("GET", "/api/v2/groups/testgrp/docs/page", nil)
	w := httptest.NewRecorder()
	srv.routes().ServeHTTP(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestHandleV2DocsTreeGroupNotFound(t *testing.T) {
	srv := buildV2DocsTestServer(t)
	r := httptest.NewRequest("GET", "/api/v2/groups/ghost/docs/tree", nil)
	w := httptest.NewRecorder()
	srv.routes().ServeHTTP(w, r)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}
