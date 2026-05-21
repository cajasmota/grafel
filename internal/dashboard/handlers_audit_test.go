package dashboard

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cajasmota/archigraph/internal/audit"
)

// setupAuditServer returns a test server with a temp audit log and broker wired in.
func setupAuditServer(t *testing.T) (*Server, string) {
	t.Helper()
	tmp := t.TempDir()
	logPath := filepath.Join(tmp, "audit.jsonl")
	srv, _ := NewServer(DefaultConfig(), newFakeStore())
	l := audit.New(logPath)
	b := audit.NewBroker()
	srv.SetAuditLog(l)
	srv.SetAuditBroker(b)
	t.Cleanup(func() { l.Close() })
	return srv, logPath
}

func TestHandleAuditHistory_empty(t *testing.T) {
	srv, _ := setupAuditServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/audit", nil)
	w := httptest.NewRecorder()
	srv.handleAuditHistory(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}

	var reply auditHistoryReply
	if err := json.NewDecoder(w.Body).Decode(&reply); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if reply.Count != 0 {
		t.Errorf("want count=0, got %d", reply.Count)
	}
	if reply.Entries == nil {
		t.Error("entries should be non-nil empty slice")
	}
}

func TestHandleAuditHistory_withEntries(t *testing.T) {
	srv, logPath := setupAuditServer(t)

	// Write some entries directly.
	l := audit.New(logPath)
	l.AppendOK("rebuild", "fixture-a", nil)
	l.AppendOK("settings_update", "", nil)
	l.AppendErr("rebuild", "fixture-b", nil, "daemon unreachable")
	l.Close()

	// Give worker time to flush.
	time.Sleep(50 * time.Millisecond)

	req := httptest.NewRequest(http.MethodGet, "/api/audit?limit=10", nil)
	w := httptest.NewRecorder()
	srv.handleAuditHistory(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}

	var reply auditHistoryReply
	if err := json.NewDecoder(w.Body).Decode(&reply); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if reply.Count != 3 {
		t.Errorf("want count=3, got %d", reply.Count)
	}
	// Should be newest-first: last written = rebuild-fixture-b
	if reply.Entries[0].Target != "fixture-b" {
		t.Errorf("want newest entry first (fixture-b), got %q", reply.Entries[0].Target)
	}
}

func TestHandleAuditHistory_filter(t *testing.T) {
	srv, logPath := setupAuditServer(t)

	l := audit.New(logPath)
	l.AppendOK("rebuild", "fixture-a", nil)
	l.AppendOK("settings_update", "", nil)
	l.Close()
	time.Sleep(50 * time.Millisecond)

	req := httptest.NewRequest(http.MethodGet, "/api/audit?filter=rebuild", nil)
	w := httptest.NewRecorder()
	srv.handleAuditHistory(w, req)

	var reply auditHistoryReply
	if err := json.NewDecoder(w.Body).Decode(&reply); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if reply.Count != 1 {
		t.Errorf("want count=1 after filter, got %d", reply.Count)
	}
	if reply.Entries[0].Operation != "rebuild" {
		t.Errorf("want operation=rebuild, got %q", reply.Entries[0].Operation)
	}
}

func TestHandleAuditHistory_noLog(t *testing.T) {
	// Server with no audit log configured.
	srv, _ := NewServer(DefaultConfig(), newFakeStore())

	req := httptest.NewRequest(http.MethodGet, "/api/audit", nil)
	w := httptest.NewRecorder()
	srv.handleAuditHistory(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}

	var reply auditHistoryReply
	if err := json.NewDecoder(w.Body).Decode(&reply); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if reply.Note == "" {
		t.Error("want non-empty note when log not configured")
	}
}

func TestHandleAuditExport_json(t *testing.T) {
	srv, logPath := setupAuditServer(t)

	l := audit.New(logPath)
	l.AppendOK("rebuild", "g", nil)
	l.Close()
	time.Sleep(50 * time.Millisecond)

	req := httptest.NewRequest(http.MethodGet, "/api/audit/export", nil)
	w := httptest.NewRecorder()
	srv.handleAuditExport(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("want application/json, got %q", ct)
	}

	var entries []audit.Entry
	if err := json.NewDecoder(w.Body).Decode(&entries); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("want 1 entry, got %d", len(entries))
	}
}

func TestHandleAuditExport_csv(t *testing.T) {
	srv, logPath := setupAuditServer(t)

	l := audit.New(logPath)
	l.AppendOK("rebuild", "g", nil)
	l.Close()
	time.Sleep(50 * time.Millisecond)

	req := httptest.NewRequest(http.MethodGet, "/api/audit/export?format=csv", nil)
	w := httptest.NewRecorder()
	srv.handleAuditExport(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); ct != "text/csv" {
		t.Errorf("want text/csv, got %q", ct)
	}
	if len(w.Body.Bytes()) == 0 {
		t.Error("expected non-empty CSV body")
	}

	// Verify at least header row + 1 data row
	body := w.Body.String()
	if body[:9] != "timestamp" {
		t.Errorf("expected CSV to start with header, got: %q", body[:min(30, len(body))])
	}
}

func TestHandleAuditStream_noBroker(t *testing.T) {
	srv, _ := NewServer(DefaultConfig(), newFakeStore())

	req := httptest.NewRequest(http.MethodGet, "/api/audit/stream", nil)
	w := httptest.NewRecorder()
	srv.handleAuditStream(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("want 503, got %d", w.Code)
	}
}

func TestAuditorCalledOnMaintenance(t *testing.T) {
	srv, logPath := setupAuditServer(t)

	// Ensure the auditor writes to logPath by checking the log after a settings_reset.
	tmp := t.TempDir()
	t.Setenv("ARCHIGRAPH_HOME", tmp)
	defer os.Unsetenv("ARCHIGRAPH_HOME")

	req := httptest.NewRequest(http.MethodPost, "/api/settings/reset", nil)
	w := httptest.NewRecorder()
	srv.handleResetSettings(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("reset settings: want 200, got %d: %s", w.Code, w.Body.String())
	}

	// Allow background goroutine to flush.
	time.Sleep(80 * time.Millisecond)

	entries, err := audit.ReadHistory(logPath, 10, "settings_reset")
	if err != nil {
		t.Fatalf("read audit log: %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("want 1 settings_reset audit entry, got %d", len(entries))
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
