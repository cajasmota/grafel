package mcp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestSessionMetricsCounters verifies per-tool call counting, error tracking,
// and that p50/p95 percentiles are computed for recorded latencies.
func TestSessionMetricsCounters(t *testing.T) {
	m := NewSessionMetrics("test-session-1", "") // no rollup dir → no disk I/O

	// Record 10 calls with ascending latencies 10ms … 100ms, no errors.
	for i := 1; i <= 10; i++ {
		m.Record("archigraph_find", time.Duration(i*10)*time.Millisecond, false)
	}
	// Record 3 calls, 2 of them errors.
	m.Record("archigraph_inspect", 5*time.Millisecond, false)
	m.Record("archigraph_inspect", 50*time.Millisecond, true)
	m.Record("archigraph_inspect", 200*time.Millisecond, true)

	snap := m.Snapshot()

	if snap.SessionID != "test-session-1" {
		t.Errorf("session_id: got %q, want %q", snap.SessionID, "test-session-1")
	}
	if snap.TotalCalls != 13 {
		t.Errorf("total_calls: got %d, want 13", snap.TotalCalls)
	}
	if snap.TotalErrors != 2 {
		t.Errorf("total_errors: got %d, want 2", snap.TotalErrors)
	}

	// Locate per-tool stats.
	toolStats := map[string]ToolStat{}
	for _, ts := range snap.Tools {
		toolStats[ts.Tool] = ts
	}

	findStat, ok := toolStats["archigraph_find"]
	if !ok {
		t.Fatal("missing archigraph_find in snapshot")
	}
	if findStat.Calls != 10 {
		t.Errorf("find calls: got %d, want 10", findStat.Calls)
	}
	if findStat.Errors != 0 {
		t.Errorf("find errors: got %d, want 0", findStat.Errors)
	}
	// p50 of [10,20,30,40,50,60,70,80,90,100] ms = 50ms (index 4 of 10 sorted).
	if findStat.P50MS < 40 || findStat.P50MS > 60 {
		t.Errorf("find p50_ms: got %d, want ~50", findStat.P50MS)
	}
	// p95 of 10 samples = index 9 = 100ms.
	if findStat.P95MS < 90 || findStat.P95MS > 100 {
		t.Errorf("find p95_ms: got %d, want ~100", findStat.P95MS)
	}

	inspectStat, ok := toolStats["archigraph_inspect"]
	if !ok {
		t.Fatal("missing archigraph_inspect in snapshot")
	}
	if inspectStat.Calls != 3 {
		t.Errorf("inspect calls: got %d, want 3", inspectStat.Calls)
	}
	if inspectStat.Errors != 2 {
		t.Errorf("inspect errors: got %d, want 2", inspectStat.Errors)
	}
	// ErrorPct = 2/3 * 100 ≈ 66.67
	if inspectStat.ErrorPct < 60 || inspectStat.ErrorPct > 70 {
		t.Errorf("inspect error_pct: got %.2f, want ~66.67", inspectStat.ErrorPct)
	}
}

// TestSessionMetricsEmpty verifies a zero-call snapshot is well-formed.
func TestSessionMetricsEmpty(t *testing.T) {
	m := NewSessionMetrics("empty-session", "")
	snap := m.Snapshot()
	if snap.TotalCalls != 0 {
		t.Errorf("total_calls: got %d, want 0", snap.TotalCalls)
	}
	if len(snap.Tools) != 0 {
		t.Errorf("tools: got %d entries, want 0", len(snap.Tools))
	}
}

// TestRollupSerializationRoundTrip writes a rollup via FlushRollup and reads
// it back via ReadRollups, verifying the JSON-lines encoding is correct.
func TestRollupSerializationRoundTrip(t *testing.T) {
	dir := t.TempDir()
	m := NewSessionMetrics("roundtrip-session", dir)

	// Record some calls.
	m.Record("archigraph_find", 15*time.Millisecond, false)
	m.Record("archigraph_find", 25*time.Millisecond, false)
	m.Record("archigraph_clusters", 300*time.Millisecond, true)

	m.FlushRollup()

	records, err := ReadRollups(dir, 1)
	if err != nil {
		t.Fatalf("ReadRollups: %v", err)
	}
	if len(records) < 2 {
		t.Fatalf("expected at least 2 rollup records, got %d", len(records))
	}

	// Build a map tool → record for assertions.
	byTool := map[string]RollupRecord{}
	for _, r := range records {
		byTool[r.Tool] = r
	}

	findRec, ok := byTool["archigraph_find"]
	if !ok {
		t.Fatal("missing archigraph_find in rollup records")
	}
	if findRec.SessionID != "roundtrip-session" {
		t.Errorf("session_id: got %q, want %q", findRec.SessionID, "roundtrip-session")
	}
	if findRec.Calls != 2 {
		t.Errorf("find calls: got %d, want 2", findRec.Calls)
	}
	if findRec.Errors != 0 {
		t.Errorf("find errors: got %d, want 0", findRec.Errors)
	}
	if findRec.P50MS < 10 || findRec.P50MS > 30 {
		t.Errorf("find p50_ms: got %d, want ~15..25", findRec.P50MS)
	}

	clustersRec, ok := byTool["archigraph_clusters"]
	if !ok {
		t.Fatal("missing archigraph_clusters in rollup records")
	}
	if clustersRec.Errors != 1 {
		t.Errorf("clusters errors: got %d, want 1", clustersRec.Errors)
	}

	// Verify the file is valid JSON-lines (each line independent).
	date := time.Now().UTC().Format("2006-01-02")
	path := filepath.Join(dir, "mcp-"+date+".jsonl")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read rollup file: %v", err)
	}
	lines := splitLines(raw)
	for i, line := range lines {
		if len(line) == 0 {
			continue
		}
		var rec RollupRecord
		if err := json.Unmarshal(line, &rec); err != nil {
			t.Errorf("line %d not valid JSON: %v", i, err)
		}
	}
}

// TestRollupNoPersistDir verifies FlushRollup is a no-op when metricsDir is "".
func TestRollupNoPersistDir(t *testing.T) {
	m := NewSessionMetrics("no-dir-session", "")
	m.Record("archigraph_find", 10*time.Millisecond, false)
	// Must not panic or write anything.
	m.FlushRollup()
}

// TestReadRollupsMultipleDays verifies multi-day windowed reads.
func TestReadRollupsMultipleDays(t *testing.T) {
	dir := t.TempDir()
	// Write a synthetic record for 2 days ago.
	twoDaysAgo := time.Now().UTC().AddDate(0, 0, -2).Format("2006-01-02")
	path := filepath.Join(dir, "mcp-"+twoDaysAgo+".jsonl")
	rec := RollupRecord{
		Date:      twoDaysAgo,
		SessionID: "old-session",
		Tool:      "archigraph_stats",
		Calls:     5,
		FlushAt:   time.Now().UTC(),
	}
	line, _ := json.Marshal(rec)
	line = append(line, '\n')
	if err := os.WriteFile(path, line, 0o600); err != nil {
		t.Fatalf("write test file: %v", err)
	}

	// days=1 should NOT see the 2-day-old file.
	recs1, err := ReadRollups(dir, 1)
	if err != nil {
		t.Fatalf("ReadRollups(1): %v", err)
	}
	for _, r := range recs1 {
		if r.SessionID == "old-session" {
			t.Error("days=1 read returned record from 2 days ago")
		}
	}

	// days=3 should include the 2-day-old file.
	recs3, err := ReadRollups(dir, 3)
	if err != nil {
		t.Fatalf("ReadRollups(3): %v", err)
	}
	found := false
	for _, r := range recs3 {
		if r.SessionID == "old-session" {
			found = true
		}
	}
	if !found {
		t.Error("days=3 read did not return record from 2 days ago")
	}
}

// TestMCPMetricsToolShape verifies the archigraph_mcp_metrics tool is registered
// and returns a sensible JSON shape (session + rollup_records keys present).
func TestMCPMetricsToolShape(t *testing.T) {
	dir := t.TempDir()
	regPath := filepath.Join(dir, "registry.json")
	if err := os.WriteFile(regPath, []byte(`{"groups":{}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	srv, err := NewServer(Config{RegistryPath: regPath})
	if err != nil {
		t.Fatal(err)
	}
	// Override SessMet's metricsDir to use the temp dir so no disk writes land
	// in ~/.archigraph during tests.
	srv.SessMet = NewSessionMetrics("test-shape", dir)

	// Simulate a few calls.
	srv.SessMet.Record("archigraph_find", 12*time.Millisecond, false)
	srv.SessMet.Record("archigraph_find", 24*time.Millisecond, false)
	srv.SessMet.Record("archigraph_inspect", 5*time.Millisecond, true)

	// Call the handler directly.
	result := callTool(t, srv, "archigraph_mcp_metrics", map[string]any{"days": float64(1)})
	if result == nil {
		t.Fatal("nil result from archigraph_mcp_metrics")
	}
	if result.IsError {
		t.Fatalf("tool returned error: %v", result.Content)
	}

	// Parse the JSON payload. The wrap() middleware appends an elapsed_ms
	// trailer after the JSON object; use findJSON to trim it.
	raw := resultText(result)
	if raw == "" {
		t.Fatal("empty result text from archigraph_mcp_metrics")
	}
	end := findJSON(raw)
	if end < 0 {
		t.Fatalf("no JSON object found in result: %s", raw)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(raw[:end]), &payload); err != nil {
		t.Fatalf("parse payload: %v\nraw: %s", err, raw)
	}

	if _, ok := payload["session"]; !ok {
		t.Error("missing 'session' key in archigraph_mcp_metrics response")
	}
	if _, ok := payload["rollup_records"]; !ok {
		t.Error("missing 'rollup_records' key in archigraph_mcp_metrics response")
	}

	// Verify session has tools array.
	sessionRaw, _ := json.Marshal(payload["session"])
	var session SessionSnapshot
	if err := json.Unmarshal(sessionRaw, &session); err != nil {
		t.Fatalf("parse session: %v", err)
	}
	if session.TotalCalls != 3 {
		t.Errorf("session.total_calls: got %d, want 3", session.TotalCalls)
	}
	if session.TotalErrors != 1 {
		t.Errorf("session.total_errors: got %d, want 1", session.TotalErrors)
	}
	if len(session.Tools) != 2 {
		t.Errorf("session.tools: got %d entries, want 2", len(session.Tools))
	}
}

// ── helpers ───────────────────────────────────────────────────────────────────

// splitLines splits raw bytes into individual non-empty lines.
func splitLines(b []byte) [][]byte {
	var out [][]byte
	start := 0
	for i, c := range b {
		if c == '\n' {
			if i > start {
				out = append(out, b[start:i])
			}
			start = i + 1
		}
	}
	if start < len(b) {
		out = append(out, b[start:])
	}
	return out
}

// findJSON finds the last byte index of a top-level JSON object/array.
// Returns the exclusive end of the first complete JSON value, or -1.
func findJSON(s string) int {
	depth := 0
	inStr := false
	for i, c := range s {
		switch {
		case inStr:
			if c == '"' {
				inStr = false
			} else if c == '\\' {
				continue
			}
		case c == '"':
			inStr = true
		case c == '{' || c == '[':
			depth++
		case c == '}' || c == ']':
			depth--
			if depth == 0 {
				return i + 1
			}
		}
	}
	return -1
}
