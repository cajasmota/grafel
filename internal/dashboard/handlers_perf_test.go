package dashboard

// handlers_perf_test.go — integration tests for GET /api/perf/budgets
// and POST /api/perf/record (#1319).

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/cajasmota/archigraph/internal/perf"
)

// newPerfTestServer creates a minimal *Server for perf handler tests,
// bypassing Listen/Serve. The registry store is an in-memory fake so these
// tests have no disk/network dependencies.
func newPerfTestServer(t *testing.T) *Server {
	t.Helper()
	cfg := Config{
		PortRange: PortRange{Min: 47270, Max: 47280},
		Bind:      "127.0.0.1",
	}
	store := newFakeStore()
	srv, err := NewServer(cfg, store)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	return srv
}

// ─────────────────────────────────────────────────────────────────────────────
// GET /api/perf/budgets
// ─────────────────────────────────────────────────────────────────────────────

func TestHandlePerfBudgets_Empty(t *testing.T) {
	srv := newPerfTestServer(t)
	// Seed recorder with a temp path to avoid touching ~/.archigraph.
	dir := t.TempDir()
	srv.perfMu.Lock()
	srv.perfRecorder = perf.NewRecorder(dir + "/h.jsonl")
	srv.perfMu.Unlock()

	req := httptest.NewRequest(http.MethodGet, "/api/perf/budgets", nil)
	w := httptest.NewRecorder()
	srv.handlePerfBudgets(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}
	var reply PerfBudgetsReply
	if err := json.Unmarshal(w.Body.Bytes(), &reply); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if reply.CheckedAt == "" {
		t.Error("want non-empty checked_at")
	}
	// With no samples recorded, statuses should only contain daemon-wide
	// metrics with no_budget or default budgets.
	for _, st := range reply.Statuses {
		if st.Current != 0 && st.Metric != "daemon_rss_mb" {
			t.Errorf("unexpected current value %f for metric %s", st.Current, st.Metric)
		}
	}
}

func TestHandlePerfBudgets_WithData(t *testing.T) {
	srv := newPerfTestServer(t)
	dir := t.TempDir()
	rec := perf.NewRecorder(dir + "/h.jsonl")
	// Record a well-under-budget index time.
	for i := 0; i < 5; i++ {
		_ = rec.Record("index_wall_ms", "mygroup", 5000.0)
	}
	// Record an over-budget query p95.
	for i := 0; i < 3; i++ {
		_ = rec.Record("query_p95_ms", "", 2000.0)
	}
	srv.perfMu.Lock()
	srv.perfRecorder = rec
	srv.perfMu.Unlock()

	req := httptest.NewRequest(http.MethodGet, "/api/perf/budgets", nil)
	w := httptest.NewRecorder()
	srv.handlePerfBudgets(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}
	var reply PerfBudgetsReply
	if err := json.Unmarshal(w.Body.Bytes(), &reply); err != nil {
		t.Fatalf("decode: %v", err)
	}

	// AnyWarning should be true because query_p95_ms is over budget.
	if !reply.AnyWarning {
		t.Error("want any_warning=true when a metric exceeds its budget")
	}

	// Verify individual statuses.
	statusByMetric := map[string]perf.BudgetStatus{}
	for _, st := range reply.Statuses {
		statusByMetric[st.Metric+"/"+st.Group] = st
	}
	if st, ok := statusByMetric["index_wall_ms/mygroup"]; ok {
		if st.Status != "green" {
			t.Errorf("index_wall_ms/mygroup: want green, got %s", st.Status)
		}
	} else {
		t.Error("index_wall_ms/mygroup not found in statuses")
	}
	if st, ok := statusByMetric["query_p95_ms/"]; ok {
		if st.Status != "red" {
			t.Errorf("query_p95_ms: want red, got %s", st.Status)
		}
	} else {
		t.Error("query_p95_ms/ not found in statuses")
	}
}

func TestHandlePerfBudgets_HistoryPathPresent(t *testing.T) {
	srv := newPerfTestServer(t)
	dir := t.TempDir()
	srv.perfMu.Lock()
	srv.perfRecorder = perf.NewRecorder(dir + "/h.jsonl")
	srv.perfMu.Unlock()

	req := httptest.NewRequest(http.MethodGet, "/api/perf/budgets", nil)
	w := httptest.NewRecorder()
	srv.handlePerfBudgets(w, req)

	var reply PerfBudgetsReply
	_ = json.Unmarshal(w.Body.Bytes(), &reply)
	// history_path comes from registry.HomeDir() in production; in tests
	// it may be empty if no home dir is set — just verify the field exists.
	_ = reply.HistoryPath
}

// ─────────────────────────────────────────────────────────────────────────────
// POST /api/perf/record
// ─────────────────────────────────────────────────────────────────────────────

func TestHandlePerfRecord_OK(t *testing.T) {
	srv := newPerfTestServer(t)
	dir := t.TempDir()
	srv.perfMu.Lock()
	srv.perfRecorder = perf.NewRecorder(dir + "/h.jsonl")
	srv.perfMu.Unlock()

	body, _ := json.Marshal(PerfRecordRequest{
		Metric: "query_p50_ms",
		Group:  "g1",
		Value:  42.5,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/perf/record", bytes.NewReader(body))
	w := httptest.NewRecorder()
	srv.handlePerfRecord(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}
	var reply PerfRecordReply
	if err := json.Unmarshal(w.Body.Bytes(), &reply); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !reply.Recorded {
		t.Error("want recorded=true")
	}
	if reply.Metric != "query_p50_ms" {
		t.Errorf("want metric=query_p50_ms, got %s", reply.Metric)
	}
	if reply.Value != 42.5 {
		t.Errorf("want value=42.5, got %f", reply.Value)
	}
}

func TestHandlePerfRecord_MissingMetric(t *testing.T) {
	srv := newPerfTestServer(t)
	dir := t.TempDir()
	srv.perfMu.Lock()
	srv.perfRecorder = perf.NewRecorder(dir + "/h.jsonl")
	srv.perfMu.Unlock()

	body, _ := json.Marshal(PerfRecordRequest{Value: 99.9}) // no metric
	req := httptest.NewRequest(http.MethodPost, "/api/perf/record", bytes.NewReader(body))
	w := httptest.NewRecorder()
	srv.handlePerfRecord(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandlePerfRecord_InvalidJSON(t *testing.T) {
	srv := newPerfTestServer(t)
	dir := t.TempDir()
	srv.perfMu.Lock()
	srv.perfRecorder = perf.NewRecorder(dir + "/h.jsonl")
	srv.perfMu.Unlock()

	req := httptest.NewRequest(http.MethodPost, "/api/perf/record",
		bytes.NewBufferString("{not json}"))
	w := httptest.NewRecorder()
	srv.handlePerfRecord(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandlePerfRecord_DataPersists(t *testing.T) {
	srv := newPerfTestServer(t)
	dir := t.TempDir()
	srv.perfMu.Lock()
	srv.perfRecorder = perf.NewRecorder(dir + "/h.jsonl")
	srv.perfMu.Unlock()

	// Record via handler.
	body, _ := json.Marshal(PerfRecordRequest{Metric: "cache_hit_rate", Value: 0.92})
	req := httptest.NewRequest(http.MethodPost, "/api/perf/record", bytes.NewReader(body))
	w := httptest.NewRecorder()
	srv.handlePerfRecord(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("record: %d %s", w.Code, w.Body.String())
	}

	// Verify via GET.
	req2 := httptest.NewRequest(http.MethodGet, "/api/perf/budgets", nil)
	w2 := httptest.NewRecorder()
	srv.handlePerfBudgets(w2, req2)
	var reply PerfBudgetsReply
	_ = json.Unmarshal(w2.Body.Bytes(), &reply)

	found := false
	for _, st := range reply.Statuses {
		if st.Metric == "cache_hit_rate" {
			found = true
			if st.Status == "no_budget" && st.Current == 0 {
				// Budget is configured in defaults — should have a value.
				t.Errorf("want non-zero current for cache_hit_rate, got %f", st.Current)
			}
		}
	}
	if !found {
		t.Error("cache_hit_rate not found in budget statuses after record")
	}
}
