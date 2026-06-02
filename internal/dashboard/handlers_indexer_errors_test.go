package dashboard

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/cajasmota/archigraph/internal/audit"
	idxerrors "github.com/cajasmota/archigraph/internal/errors"
)

// setupIndexerErrServer returns a test Server with a temp audit log wired in.
func setupIndexerErrServer(t *testing.T) (*Server, string) {
	t.Helper()
	tmp := t.TempDir()
	logPath := filepath.Join(tmp, "audit.jsonl")
	srv, _ := NewServer(DefaultConfig(), newFakeStore())
	l := audit.New(logPath)
	srv.SetAuditLog(l)
	t.Cleanup(func() { l.Close() })
	return srv, logPath
}

func TestHandleIndexerErrors_Empty(t *testing.T) {
	srv, _ := setupIndexerErrServer(t)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/indexer-errors", nil)
	srv.handleIndexerErrors(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d; want 200", w.Code)
	}
	var reply IndexerErrorReply
	if err := json.NewDecoder(w.Body).Decode(&reply); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if reply.Total != 0 {
		t.Errorf("Total = %d; want 0", reply.Total)
	}
	if len(reply.Errors) != 0 {
		t.Errorf("Errors len = %d; want 0", len(reply.Errors))
	}
}

func TestHandleIndexerErrors_FiltersNonErrors(t *testing.T) {
	srv, logPath := setupIndexerErrServer(t)

	l := audit.New(logPath)
	l.AppendOK("index", "my-repo", nil) // not an error — excluded
	l.AppendErr("index", "my-repo", map[string]any{
		"error_code": "IDX-001",
		"file_path":  "/protected/src",
	}, "[IDX-001] permission denied (/protected/src)")
	l.Close()

	// Give background worker time to flush.
	time.Sleep(50 * time.Millisecond)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/indexer-errors", nil)
	srv.handleIndexerErrors(w, req)

	var reply IndexerErrorReply
	if err := json.NewDecoder(w.Body).Decode(&reply); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if reply.Total != 1 {
		t.Errorf("Total = %d; want 1", reply.Total)
	}
	if reply.Errors[0].Code != "IDX-001" {
		t.Errorf("Code = %q; want IDX-001", reply.Errors[0].Code)
	}
	if reply.Errors[0].FilePath != "/protected/src" {
		t.Errorf("FilePath = %q; want /protected/src", reply.Errors[0].FilePath)
	}
	if reply.Errors[0].Hint == "" {
		t.Error("Hint should not be empty for IDX-001")
	}
	if reply.Errors[0].DocsURL == "" {
		t.Error("DocsURL should not be empty")
	}
}

func TestHandleIndexerErrors_FiltersNonIndexerOps(t *testing.T) {
	srv, logPath := setupIndexerErrServer(t)

	l := audit.New(logPath)
	// settings_update is not an indexer operation.
	l.AppendErr("settings_update", "", nil, "some settings error")
	l.AppendErr("index", "repo-a", map[string]any{"error_code": "IDX-002"}, "[IDX-002] file too large")
	l.Close()

	time.Sleep(50 * time.Millisecond)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/indexer-errors", nil)
	srv.handleIndexerErrors(w, req)

	var reply IndexerErrorReply
	json.NewDecoder(w.Body).Decode(&reply) //nolint:errcheck
	if reply.Total != 1 {
		t.Errorf("Total = %d; want 1 (settings_update excluded)", reply.Total)
	}
}

func TestHandleIndexerErrors_CodeFilter(t *testing.T) {
	srv, logPath := setupIndexerErrServer(t)

	l := audit.New(logPath)
	l.AppendErr("index", "repo-a", map[string]any{"error_code": "IDX-001"}, "[IDX-001] permission denied")
	l.AppendErr("index", "repo-b", map[string]any{"error_code": "IDX-002"}, "[IDX-002] file too large")
	l.Close()

	time.Sleep(50 * time.Millisecond)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/indexer-errors?code=IDX-002", nil)
	srv.handleIndexerErrors(w, req)

	var reply IndexerErrorReply
	json.NewDecoder(w.Body).Decode(&reply) //nolint:errcheck
	if reply.Total != 1 {
		t.Errorf("Total = %d; want 1", reply.Total)
	}
	if reply.Errors[0].Code != "IDX-002" {
		t.Errorf("expected IDX-002 only; got %s", reply.Errors[0].Code)
	}
}

func TestHandleIndexerErrors_FallbackCodeParsing(t *testing.T) {
	srv, logPath := setupIndexerErrServer(t)

	// Entry without error_code in Params — code parsed from Error string.
	l := audit.New(logPath)
	l.AppendErr("ast_extract", "repo-x", nil, "[IDX-008] AST extraction failed (/src/complex.ts)")
	l.Close()

	time.Sleep(50 * time.Millisecond)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/indexer-errors", nil)
	srv.handleIndexerErrors(w, req)

	var reply IndexerErrorReply
	json.NewDecoder(w.Body).Decode(&reply) //nolint:errcheck
	if reply.Total != 1 {
		t.Fatalf("Total = %d; want 1", reply.Total)
	}
	if reply.Errors[0].Code != "IDX-008" {
		t.Errorf("Code = %q; want IDX-008", reply.Errors[0].Code)
	}
}

func TestHandleIndexerErrors_HintAndDocsPresent(t *testing.T) {
	srv, logPath := setupIndexerErrServer(t)

	l := audit.New(logPath)
	l.AppendErr("rebuild", "repo-oom", map[string]any{"error_code": "IDX-005"}, "[IDX-005] out of memory")
	l.Close()

	time.Sleep(50 * time.Millisecond)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/indexer-errors", nil)
	srv.handleIndexerErrors(w, req)

	var reply IndexerErrorReply
	json.NewDecoder(w.Body).Decode(&reply) //nolint:errcheck
	if reply.Total != 1 {
		t.Fatalf("Total = %d; want 1", reply.Total)
	}
	rec := reply.Errors[0]
	if rec.Hint == "" {
		t.Error("Hint should be non-empty for IDX-005")
	}
	if rec.DocsURL != "https://archigraph.dev/docs/errors/IDX-005" {
		t.Errorf("DocsURL = %q; unexpected value", rec.DocsURL)
	}
}

// TestHandleIndexerErrors_HintMatchesCanonicalRegistry proves the handler now
// sources its remediation text from the internal/errors registry (the single
// source of truth) rather than a duplicated in-handler string table. The hint
// emitted over the API must be byte-identical to what idxerrors.New produces
// for the same code — guarding against drift between the two former tables.
func TestHandleIndexerErrors_HintMatchesCanonicalRegistry(t *testing.T) {
	srv, logPath := setupIndexerErrServer(t)

	l := audit.New(logPath)
	l.AppendErr("index", "repo-big", map[string]any{"error_code": "IDX-002"}, "[IDX-002] file too large")
	l.Close()

	time.Sleep(50 * time.Millisecond)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/indexer-errors", nil)
	srv.handleIndexerErrors(w, req)

	var reply IndexerErrorReply
	json.NewDecoder(w.Body).Decode(&reply) //nolint:errcheck
	if reply.Total != 1 {
		t.Fatalf("Total = %d; want 1", reply.Total)
	}
	rec := reply.Errors[0]

	canonical := idxerrors.New(idxerrors.CodeFileTooLarge, "", "", 0, nil)
	if rec.Hint != canonical.Hint {
		t.Errorf("Hint = %q; want canonical registry hint %q", rec.Hint, canonical.Hint)
	}
	if rec.DocsURL != canonical.DocsURL {
		t.Errorf("DocsURL = %q; want %q", rec.DocsURL, canonical.DocsURL)
	}
	if rec.Hint == "" {
		t.Error("canonical hint for IDX-002 must be non-empty")
	}
}
