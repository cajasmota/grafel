package dashboard

// handlers_enrichment_writeback_test.go — unit tests for the enrichment
// write-back endpoint (#1304).
//
//	POST /api/enrichments/{group}/write?subject_id=X

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/daemon"
	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/graph/descriptions"
)

// ─────────────────────────────────────────────────────────────────────────────
// Test helpers
// ─────────────────────────────────────────────────────────────────────────────

// newWritebackServer creates a Server with a seeded group "g1" that contains
// one repo whose graph.json is written to a temp directory.
// The returned *graph.Entity pointer points directly into the cache so tests
// can verify in-memory mutation without disk I/O.
func newWritebackServer(t *testing.T) (*Server, *graph.Entity, string) {
	t.Helper()

	tmp := t.TempDir()
	// #1626: per-repo graph artifacts live in the external store.
	t.Setenv("GRAFEL_DAEMON_ROOT", t.TempDir())

	// Build a minimal graph with one entity.
	entityID := "aabbccddeeff0011"
	doc := &graph.Document{
		Version: graph.SchemaVersion,
		Entities: []graph.Entity{
			graph.Entity{
				ID:         entityID,
				Name:       "OrderCheckout",
				Kind:       "http_endpoint",
				Language:   "python",
				SourceFile: "api/views.py",
			}.WithProperties(map[string]string{}),
		},
	}

	// Write the initial graph.json — the handler will overwrite it.
	graphPath := daemon.GraphPathForRepo(tmp)
	if err := os.MkdirAll(filepath.Dir(graphPath), 0o755); err != nil {
		t.Fatalf("mkdir store: %v", err)
	}
	if err := graph.WriteAtomic(graphPath, doc, false); err != nil {
		t.Fatalf("write initial graph: %v", err)
	}

	// Build server.
	cfg := DefaultConfig()
	srv, err := NewServer(cfg, newFakeStore())
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	// Seed graph cache with the tmp repo so handlers don't hit the registry.
	// We keep a direct pointer to the entity so we can verify in-memory
	// mutation after the write-back call.
	entityPtr := &doc.Entities[0]
	srv.graphs.mu.Lock()
	srv.graphs.entries["g1"] = &cacheEntry{
		group: &DashGroup{
			Name: "g1",
			Repos: map[string]*DashRepo{
				"repo1": {
					Slug: "repo1",
					Path: tmp,
					Doc:  doc,
				},
			},
		},
		loadedAt: time.Now().Add(60 * time.Second),
	}
	srv.graphs.mu.Unlock()

	return srv, entityPtr, tmp
}

// doWritebackRequest fires a POST /api/enrichments/g1/write request.
func doWritebackRequest(t *testing.T, srv *Server, subjectID string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(body); err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	path := "/api/enrichments/g1/write"
	if subjectID != "" {
		path += "?subject_id=" + subjectID
	}
	req := httptest.NewRequest(http.MethodPost, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.routes().ServeHTTP(w, req)
	return w
}

// ─────────────────────────────────────────────────────────────────────────────
// Validation: missing / bad inputs
// ─────────────────────────────────────────────────────────────────────────────

func TestWriteback_missingSubjectID(t *testing.T) {
	srv, _, _ := newWritebackServer(t)
	w := doWritebackRequest(t, srv, "", enrichmentWritebackRequest{Description: "A valid description here."})
	if w.Code != http.StatusBadRequest {
		t.Errorf("want 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestWriteback_descriptionTooShort(t *testing.T) {
	srv, _, _ := newWritebackServer(t)
	w := doWritebackRequest(t, srv, "aabbccddeeff0011", enrichmentWritebackRequest{Description: "short"})
	if w.Code != http.StatusBadRequest {
		t.Errorf("want 400, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "too short") {
		t.Errorf("want 'too short' in body, got: %s", w.Body.String())
	}
}

func TestWriteback_placeholderDescription(t *testing.T) {
	srv, _, _ := newWritebackServer(t)
	for _, bad := range []string{
		"TODO: describe this endpoint properly",
		"FIXME add description",
		"N/A for now and later",
		"placeholder text goes here in this location",
	} {
		w := doWritebackRequest(t, srv, "aabbccddeeff0011", enrichmentWritebackRequest{Description: bad})
		if w.Code != http.StatusBadRequest {
			t.Errorf("desc %q: want 400, got %d: %s", bad, w.Code, w.Body.String())
		}
	}
}

func TestWriteback_entityNotFound(t *testing.T) {
	srv, _, _ := newWritebackServer(t)
	w := doWritebackRequest(t, srv, "nonexistententity", enrichmentWritebackRequest{
		Description: "Handles the checkout flow for orders placed by authenticated users.",
		Kind:        "http_endpoint",
	})
	if w.Code != http.StatusNotFound {
		t.Errorf("want 404, got %d: %s", w.Code, w.Body.String())
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Happy path
// ─────────────────────────────────────────────────────────────────────────────

func TestWriteback_success(t *testing.T) {
	srv, entity, repoDir := newWritebackServer(t)

	desc := "Handles the checkout flow for orders placed by authenticated users via POST /checkout."
	w := doWritebackRequest(t, srv, "aabbccddeeff0011", enrichmentWritebackRequest{
		Description: desc,
		Kind:        "http_endpoint",
		ModelUsed:   "claude-haiku-4-5",
		TokensUsed:  512,
	})

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}

	// ── 1. Response shape ─────────────────────────────────────────────────
	var resp enrichmentWritebackResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.SubjectID != "aabbccddeeff0011" {
		t.Errorf("subject_id: want aabbccddeeff0011, got %q", resp.SubjectID)
	}
	if resp.DocPath == "" {
		t.Error("doc_path must be non-empty")
	}
	if resp.GraphPath == "" {
		t.Error("graph_path must be non-empty")
	}
	if resp.EnrichedAt == "" {
		t.Error("enriched_at must be non-empty")
	}

	// ── 2. In-memory entity property set ─────────────────────────────────
	if entity.PropGet("description") != desc {
		t.Errorf("entity.Properties[description]: want %q, got %q", desc, entity.PropGet("description"))
	}

	// ── 3. description lands in the side-table, graph NOT rewritten ────────
	// #5904 PR-a: the description is persisted to <stateDir>/descriptions.json,
	// and the graph file is NEVER rewritten (no collapse / OOM for a segment-set).
	stateDir := daemon.StateDirForRepo(repoDir)
	sc, ok := descriptions.Read(stateDir)
	if !ok {
		t.Fatalf("descriptions side-table not found / not fresh at %s", descriptions.Path(stateDir))
	}
	if got := sc.Results["aabbccddeeff0011"]; got != desc {
		t.Errorf("side-table description: want %q, got %q", desc, got)
	}

	// The on-disk graph.json must be UNCHANGED — the write-back must not bake the
	// description into the graph (that is the whole-graph rewrite this PR removes).
	graphPath := daemon.GraphPathForRepo(repoDir)
	raw, err := os.ReadFile(graphPath)
	if err != nil {
		t.Fatalf("read graph.json: %v", err)
	}
	var savedDoc graph.Document
	if err := json.Unmarshal(raw, &savedDoc); err != nil {
		t.Fatalf("unmarshal graph.json: %v", err)
	}
	for _, e := range savedDoc.Entities {
		if e.ID == "aabbccddeeff0011" && e.PropGet("description") != "" {
			t.Errorf("graph.json must NOT be rewritten with the description; got %q", e.PropGet("description"))
		}
	}

	// ── 4. Markdown doc file created ─────────────────────────────────────
	docPath := filepath.Join(repoDir, "docs", "enrichments", "http_endpoint", "aabbccddeeff0011.md")
	content, err := os.ReadFile(docPath)
	if err != nil {
		t.Fatalf("read doc file %s: %v", docPath, err)
	}
	contentStr := string(content)

	if !strings.Contains(contentStr, "entity_id:") {
		t.Error("doc missing entity_id frontmatter key")
	}
	if !strings.Contains(contentStr, "kind:") {
		t.Error("doc missing kind frontmatter key")
	}
	if !strings.Contains(contentStr, "summary:") {
		t.Error("doc missing summary frontmatter key")
	}
	if !strings.Contains(contentStr, desc[:30]) {
		t.Errorf("doc body missing description text; got:\n%s", contentStr)
	}
	if !strings.Contains(contentStr, "model_used:") {
		t.Error("doc missing model_used frontmatter key")
	}
	if !strings.Contains(contentStr, "tokens_used:") {
		t.Error("doc missing tokens_used frontmatter key")
	}
	if !strings.HasPrefix(contentStr, "---\n") {
		t.Error("doc must start with YAML frontmatter delimiter ---")
	}
}

// TestWriteback_idempotent — calling write-back twice for the same entity
// overwrites the doc and property without error.
func TestWriteback_idempotent(t *testing.T) {
	srv, entity, repoDir := newWritebackServer(t)

	desc1 := "First description for the checkout endpoint in production."
	desc2 := "Revised description: handles checkout including tax calculation for authenticated users."

	// First call.
	w := doWritebackRequest(t, srv, "aabbccddeeff0011", enrichmentWritebackRequest{
		Description: desc1,
		Kind:        "http_endpoint",
	})
	if w.Code != http.StatusOK {
		t.Fatalf("first call: want 200, got %d: %s", w.Code, w.Body.String())
	}

	// Re-seed the cache (Invalidate was called; seed it again).
	doc := entity // entity pointer still valid; cache may have been cleared
	_ = doc
	srv.graphs.mu.Lock()
	repoDoc := srv.graphs.entries["g1"]
	srv.graphs.mu.Unlock()
	_ = repoDoc // may be nil after invalidation — that's fine for second call

	// We need to re-seed the cache for the second call because Invalidate cleared it.
	// Reload the doc from disk.
	graphPath := daemon.GraphPathForRepo(repoDir)
	raw, err := os.ReadFile(graphPath)
	if err != nil {
		t.Fatalf("read graph.json: %v", err)
	}
	var updatedDoc graph.Document
	if err := json.Unmarshal(raw, &updatedDoc); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	srv.graphs.mu.Lock()
	srv.graphs.entries["g1"] = &cacheEntry{
		group: &DashGroup{
			Name: "g1",
			Repos: map[string]*DashRepo{
				"repo1": {
					Slug: "repo1",
					Path: repoDir,
					Doc:  &updatedDoc,
				},
			},
		},
		loadedAt: time.Now().Add(60 * time.Second),
	}
	srv.graphs.mu.Unlock()

	// Second call with different description.
	w2 := doWritebackRequest(t, srv, "aabbccddeeff0011", enrichmentWritebackRequest{
		Description: desc2,
		Kind:        "http_endpoint",
	})
	if w2.Code != http.StatusOK {
		t.Fatalf("second call: want 200, got %d: %s", w2.Code, w2.Body.String())
	}

	// Doc file should contain the revised description.
	docPath := filepath.Join(repoDir, "docs", "enrichments", "http_endpoint", "aabbccddeeff0011.md")
	content, err := os.ReadFile(docPath)
	if err != nil {
		t.Fatalf("read doc file: %v", err)
	}
	if !strings.Contains(string(content), "Revised description") {
		t.Errorf("expected revised description in doc, got:\n%s", string(content))
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Unit tests for pure helpers
// ─────────────────────────────────────────────────────────────────────────────

func TestValidateDescription(t *testing.T) {
	cases := []struct {
		desc    string
		wantErr bool
	}{
		{"Handles the checkout flow for authenticated users.", false},
		{"short", true},                        // too short
		{"TODO: describe this", true},          // placeholder
		{"FIXME: add description later", true}, // placeholder
		{"N/A not applicable here", true},      // placeholder
		{"This is a perfectly normal description of a thing.", false},
		{"describe this endpoint for me", true},                             // "describe this"
		{"Placeholder content should be rejected from the pipeline.", true}, // "Placeholder"
	}
	for _, tc := range cases {
		msg := validateDescription(tc.desc)
		gotErr := msg != ""
		if gotErr != tc.wantErr {
			t.Errorf("desc %q: wantErr=%v, got msg=%q", tc.desc, tc.wantErr, msg)
		}
	}
}

func TestSanitizePathSegment(t *testing.T) {
	cases := []struct{ in, want string }{
		{"aabbccddeeff0011", "aabbccddeeff0011"},
		{"flow::checkout", "flow--checkout"},
		{"ep/users/{id}", "ep-users--id-"},
		{"ep abc", "ep-abc"},
	}
	for _, tc := range cases {
		got := sanitizePathSegment(tc.in)
		if got != tc.want {
			t.Errorf("sanitize(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestYamlScalar(t *testing.T) {
	cases := []struct{ in, want string }{
		{"simple", "simple"},
		{"with space", "with space"}, // no special chars
		{"has:colon", "'has:colon'"},
		{"has#hash", "has#hash"}, // # not in the quoting set
		{"it's quoted", "'it''s quoted'"},
		{"", "''"},
	}
	for _, tc := range cases {
		got := yamlScalar(tc.in)
		if got != tc.want {
			t.Errorf("yamlScalar(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
