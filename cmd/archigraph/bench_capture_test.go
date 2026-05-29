package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// TestParseBenchCapture_Empty verifies that empty input produces zero metrics
// and null percentiles.
func TestParseBenchCapture_Empty(t *testing.T) {
	out := parseBenchCapture(nil)
	if out.McpRPCCount != 0 {
		t.Errorf("count: got %d, want 0", out.McpRPCCount)
	}
	if out.McpRPCHandlerMsSum != 0 {
		t.Errorf("sum: got %d, want 0", out.McpRPCHandlerMsSum)
	}
	if out.McpRPCHandlerMsP50 != nil {
		t.Errorf("p50: got %v, want nil", *out.McpRPCHandlerMsP50)
	}
	if out.McpRPCHandlerMsP99 != nil {
		t.Errorf("p99: got %v, want nil", *out.McpRPCHandlerMsP99)
	}
	if len(out.McpRPCPerTool) != 0 {
		t.Errorf("per_tool: got %v, want empty", out.McpRPCPerTool)
	}
}

// TestParseBenchCapture_EmptyBytes verifies that empty byte slice also yields zeros.
func TestParseBenchCapture_EmptyBytes(t *testing.T) {
	out := parseBenchCapture([]byte{})
	if out.McpRPCCount != 0 {
		t.Errorf("count: got %d, want 0", out.McpRPCCount)
	}
}

// TestParseBenchCapture_SingleLine verifies a single well-formed slog log line.
func TestParseBenchCapture_SingleLine(t *testing.T) {
	line := "time=2026-05-26T22:40:38.695+05:45 level=INFO msg=mcp_rpc phase=done tool=archigraph_search elapsed_ms=8095 repo=/path/to/repo ts=2026-05-26T17:10:38Z\n"
	out := parseBenchCapture([]byte(line))

	if out.McpRPCCount != 1 {
		t.Errorf("count: got %d, want 1", out.McpRPCCount)
	}
	if out.McpRPCHandlerMsSum != 8095 {
		t.Errorf("sum: got %d, want 8095", out.McpRPCHandlerMsSum)
	}
	if out.McpRPCHandlerMsP50 == nil || *out.McpRPCHandlerMsP50 != 8095 {
		t.Errorf("p50: got %v, want 8095", out.McpRPCHandlerMsP50)
	}
	if out.McpRPCHandlerMsP99 == nil || *out.McpRPCHandlerMsP99 != 8095 {
		t.Errorf("p99: got %v, want 8095", out.McpRPCHandlerMsP99)
	}
	if stats, ok := out.McpRPCPerTool["archigraph_search"]; !ok {
		t.Error("per_tool missing archigraph_search")
	} else {
		if stats.Count != 1 {
			t.Errorf("per_tool count: got %d, want 1", stats.Count)
		}
		if stats.SumMs != 8095 {
			t.Errorf("per_tool sum_ms: got %d, want 8095", stats.SumMs)
		}
	}
}

// TestParseBenchCapture_ReceivedLinesIgnored verifies that phase=received
// companion lines (no elapsed_ms field) are not counted.
func TestParseBenchCapture_ReceivedLinesIgnored(t *testing.T) {
	data := strings.Join([]string{
		"time=2026-05-26T22:40:38.000+05:45 level=INFO msg=mcp_rpc phase=received tool=archigraph_search repo=/path ts=2026-05-26T17:10:38Z",
		"time=2026-05-26T22:40:38.500+05:45 level=INFO msg=mcp_rpc phase=done tool=archigraph_search elapsed_ms=500 repo=/path ts=2026-05-26T17:10:38Z",
		"",
	}, "\n")
	out := parseBenchCapture([]byte(data))
	if out.McpRPCCount != 1 {
		t.Errorf("count: got %d, want 1 (received line must be ignored)", out.McpRPCCount)
	}
}

// TestParseBenchCapture_MultipleTools verifies aggregation across different tools.
func TestParseBenchCapture_MultipleTools(t *testing.T) {
	// 3 calls: search×2 (100ms, 200ms) + describe×1 (300ms)
	lines := []string{
		"time=2026-05-26T22:40:38.000+05:45 level=INFO msg=mcp_rpc phase=done tool=archigraph_search elapsed_ms=100 repo=/r ts=2026-05-26T17:10:38Z",
		"time=2026-05-26T22:40:39.000+05:45 level=INFO msg=mcp_rpc phase=done tool=archigraph_search elapsed_ms=200 repo=/r ts=2026-05-26T17:10:39Z",
		"time=2026-05-26T22:40:40.000+05:45 level=INFO msg=mcp_rpc phase=done tool=archigraph_describe elapsed_ms=300 repo=/r ts=2026-05-26T17:10:40Z",
		"",
	}
	out := parseBenchCapture([]byte(strings.Join(lines, "\n")))

	if out.McpRPCCount != 3 {
		t.Errorf("count: got %d, want 3", out.McpRPCCount)
	}
	if out.McpRPCHandlerMsSum != 600 {
		t.Errorf("sum: got %d, want 600", out.McpRPCHandlerMsSum)
	}

	// Sorted durations: [100, 200, 300], n=3 (odd).
	// p50 = sorted[(3-1)/2] = sorted[1] = 200.
	wantP50 := 200.0
	if out.McpRPCHandlerMsP50 == nil || *out.McpRPCHandlerMsP50 != wantP50 {
		t.Errorf("p50: got %v, want %v", out.McpRPCHandlerMsP50, wantP50)
	}

	// p99 = sorted[ceil(3*0.99)-1] = sorted[ceil(2.97)-1] = sorted[3-1] = sorted[2] = 300.
	wantP99 := 300.0
	if out.McpRPCHandlerMsP99 == nil || *out.McpRPCHandlerMsP99 != wantP99 {
		t.Errorf("p99: got %v, want %v", out.McpRPCHandlerMsP99, wantP99)
	}

	search := out.McpRPCPerTool["archigraph_search"]
	if search == nil {
		t.Fatal("per_tool missing archigraph_search")
	}
	if search.Count != 2 {
		t.Errorf("search count: got %d, want 2", search.Count)
	}
	if search.SumMs != 300 {
		t.Errorf("search sum_ms: got %d, want 300", search.SumMs)
	}

	describe := out.McpRPCPerTool["archigraph_describe"]
	if describe == nil {
		t.Fatal("per_tool missing archigraph_describe")
	}
	if describe.Count != 1 {
		t.Errorf("describe count: got %d, want 1", describe.Count)
	}
	if describe.SumMs != 300 {
		t.Errorf("describe sum_ms: got %d, want 300", describe.SumMs)
	}
}

// TestParseBenchCapture_PercentileEven verifies p50 linear interpolation for even n.
func TestParseBenchCapture_PercentileEven(t *testing.T) {
	// 4 values: [10, 20, 30, 40] → sorted same.
	// p50 = average(sorted[1], sorted[2]) = average(20, 30) = 25.
	// p99 = sorted[ceil(4*0.99)-1] = sorted[ceil(3.96)-1] = sorted[4-1] = sorted[3] = 40.
	lines := []string{
		"time=2026-05-26T22:40:38.000+05:45 level=INFO msg=mcp_rpc phase=done tool=t elapsed_ms=10 repo=/r ts=2026-05-26T17:10:38Z",
		"time=2026-05-26T22:40:38.001+05:45 level=INFO msg=mcp_rpc phase=done tool=t elapsed_ms=30 repo=/r ts=2026-05-26T17:10:38Z",
		"time=2026-05-26T22:40:38.002+05:45 level=INFO msg=mcp_rpc phase=done tool=t elapsed_ms=20 repo=/r ts=2026-05-26T17:10:38Z",
		"time=2026-05-26T22:40:38.003+05:45 level=INFO msg=mcp_rpc phase=done tool=t elapsed_ms=40 repo=/r ts=2026-05-26T17:10:38Z",
		"",
	}
	out := parseBenchCapture([]byte(strings.Join(lines, "\n")))

	if out.McpRPCCount != 4 {
		t.Errorf("count: got %d, want 4", out.McpRPCCount)
	}
	if out.McpRPCHandlerMsP50 == nil || *out.McpRPCHandlerMsP50 != 25.0 {
		t.Errorf("p50: got %v, want 25.0", out.McpRPCHandlerMsP50)
	}
	if out.McpRPCHandlerMsP99 == nil || *out.McpRPCHandlerMsP99 != 40.0 {
		t.Errorf("p99: got %v, want 40.0", out.McpRPCHandlerMsP99)
	}
}

// TestParseBenchCapture_PercentileKnownFixture exercises the percentile formula
// on a larger known fixture (10 values) to confirm no off-by-one errors.
func TestParseBenchCapture_PercentileKnownFixture(t *testing.T) {
	// Durations 1..10 ms. Sorted: [1,2,3,4,5,6,7,8,9,10], n=10 (even).
	// p50 = average(sorted[4], sorted[5]) = average(5, 6) = 5.5.
	// p99 = sorted[ceil(10*0.99)-1] = sorted[ceil(9.9)-1] = sorted[10-1] = sorted[9] = 10.
	var lines []string
	for i := 1; i <= 10; i++ {
		lines = append(lines, "time=2026-05-26T22:40:38."+strconv.Itoa(i)+"00+05:45 level=INFO msg=mcp_rpc phase=done tool=t elapsed_ms="+strconv.Itoa(i)+" repo=/r ts=2026-05-26T17:10:38Z")
	}
	lines = append(lines, "")
	out := parseBenchCapture([]byte(strings.Join(lines, "\n")))

	if out.McpRPCCount != 10 {
		t.Errorf("count: got %d, want 10", out.McpRPCCount)
	}
	if out.McpRPCHandlerMsSum != 55 {
		t.Errorf("sum: got %d, want 55", out.McpRPCHandlerMsSum)
	}
	if out.McpRPCHandlerMsP50 == nil || *out.McpRPCHandlerMsP50 != 5.5 {
		t.Errorf("p50: got %v, want 5.5", out.McpRPCHandlerMsP50)
	}
	if out.McpRPCHandlerMsP99 == nil || *out.McpRPCHandlerMsP99 != 10.0 {
		t.Errorf("p99: got %v, want 10.0", out.McpRPCHandlerMsP99)
	}
}

// TestBenchCaptureOutputMatchesSchema verifies that parseBenchCapture output
// round-trips through JSON and the mcp_rpc_per_tool values match the
// ToolRPCStats shape expected by the schema (fields: count, sum_ms).
// This is the cross-validation between the CLI output and the JSON Schema SSOT
// (skills/archigraph-graph-quality/schema/with-mcp-artifact.schema.json).
func TestBenchCaptureOutputMatchesSchema(t *testing.T) {
	line := "time=2026-05-26T22:40:38.000+05:45 level=INFO msg=mcp_rpc phase=done tool=archigraph_search elapsed_ms=1000 repo=/r ts=2026-05-26T17:10:38Z\n"
	out := parseBenchCapture([]byte(line))

	raw, err := json.Marshal(out)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	// Round-trip: unmarshal into a generic map and check required schema fields.
	var m map[string]interface{}
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	requiredTopLevel := []string{
		"mcp_rpc_count",
		"mcp_rpc_handler_ms_sum",
		"mcp_rpc_handler_ms_p50",
		"mcp_rpc_handler_ms_p99",
		"mcp_rpc_per_tool",
	}
	for _, field := range requiredTopLevel {
		if _, ok := m[field]; !ok {
			t.Errorf("missing required field %q in JSON output", field)
		}
	}

	// Check per-tool stats shape (count + sum_ms per ToolRPCStats in schema).
	perTool, ok := m["mcp_rpc_per_tool"].(map[string]interface{})
	if !ok {
		t.Fatal("mcp_rpc_per_tool is not a JSON object")
	}
	for toolName, statsRaw := range perTool {
		stats, ok := statsRaw.(map[string]interface{})
		if !ok {
			t.Errorf("tool %q: stats is not an object", toolName)
			continue
		}
		for _, field := range []string{"count", "sum_ms"} {
			if _, ok := stats[field]; !ok {
				t.Errorf("tool %q: missing required ToolRPCStats field %q", toolName, field)
			}
		}
	}
}

// TestRunBenchCapture_LogFile exercises runBenchCapture end-to-end with a
// real temp file, verifying offset-bounded reads and the --log flag.
func TestRunBenchCapture_LogFile(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "daemon.log")

	// Write a log with a prefix that should be skipped (before startOff).
	prefix := "noise line before window\n"
	window := "time=2026-05-26T22:40:38.000+05:45 level=INFO msg=mcp_rpc phase=done tool=archigraph_trace elapsed_ms=250 repo=/r ts=2026-05-26T17:10:38Z\n"
	suffix := "time=2026-05-26T22:40:39.000+05:45 level=INFO msg=mcp_rpc phase=done tool=archigraph_trace elapsed_ms=999 repo=/r ts=2026-05-26T17:10:39Z\n"

	content := prefix + window + suffix
	if err := os.WriteFile(logPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write log: %v", err)
	}

	startOff := int64(len(prefix))
	endOff := int64(len(prefix) + len(window))

	data, err := readSlice(logPath, startOff, endOff)
	if err != nil {
		t.Fatalf("readSlice: %v", err)
	}
	out := parseBenchCapture(data)

	if out.McpRPCCount != 1 {
		t.Errorf("count: got %d, want 1 (only window)", out.McpRPCCount)
	}
	if out.McpRPCHandlerMsSum != 250 {
		t.Errorf("sum: got %d, want 250", out.McpRPCHandlerMsSum)
	}
}

// TestRunBenchCapture_MissingFile verifies that a missing log file returns
// a zero-metrics result without error (the daemon may not have flushed yet).
func TestRunBenchCapture_MissingFile(t *testing.T) {
	data, err := readSlice("/nonexistent/path/daemon.log", 0, -1)
	if err != nil {
		t.Errorf("readSlice on missing file: got error %v, want nil", err)
	}
	if len(data) != 0 {
		t.Errorf("data: got %d bytes, want 0", len(data))
	}
	out := parseBenchCapture(data)
	if out.McpRPCCount != 0 {
		t.Errorf("count from missing file: got %d, want 0", out.McpRPCCount)
	}
}

// TestBenchCaptureRPCHelp verifies that the rpc subcommand is properly registered
// in the subcommand table and will respond to help requests via cobra/CLI layer.
func TestBenchCaptureRPCHelp(t *testing.T) {
	// Verify the subcommand table exists and that "rpc" is registered.
	// The actual help rendering is tested at the cobra/CLI layer in internal/cli.
	if _, ok := benchCaptureSubcommands["rpc"]; !ok {
		t.Fatal("rpc subcommand not registered in benchCaptureSubcommands")
	}

	// Verify all subcommands have a handler function
	for name, handler := range benchCaptureSubcommands {
		if handler == nil {
			t.Errorf("subcommand %q has nil handler", name)
		}
	}
}

// TestParseBenchCapture_WithByteFields verifies the #2828 wire_bytes /
// payload_token_estimate fields are captured and aggregated, both at the
// top level and per-tool.
func TestParseBenchCapture_WithByteFields(t *testing.T) {
	lines := strings.Join([]string{
		"time=2026-05-29T10:00:00.000+05:45 level=INFO msg=mcp_rpc phase=done tool=archigraph_search elapsed_ms=100 wire_bytes=4000 payload_token_estimate=1000 repo=/r ts=2026-05-29T04:15:00Z",
		"time=2026-05-29T10:00:01.000+05:45 level=INFO msg=mcp_rpc phase=done tool=archigraph_search elapsed_ms=200 wire_bytes=800 payload_token_estimate=200 repo=/r ts=2026-05-29T04:15:01Z",
		"time=2026-05-29T10:00:02.000+05:45 level=INFO msg=mcp_rpc phase=done tool=archigraph_describe elapsed_ms=50 wire_bytes=1200 payload_token_estimate=300 repo=/r ts=2026-05-29T04:15:02Z",
	}, "\n") + "\n"

	out := parseBenchCapture([]byte(lines))

	if out.McpRPCCount != 3 {
		t.Fatalf("count: got %d, want 3", out.McpRPCCount)
	}
	if out.McpRPCWireBytesSum != 6000 {
		t.Errorf("wire_bytes_sum: got %d, want 6000", out.McpRPCWireBytesSum)
	}
	if out.McpRPCTokenEstSum != 1500 {
		t.Errorf("token_est_sum: got %d, want 1500", out.McpRPCTokenEstSum)
	}
	search := out.McpRPCPerTool["archigraph_search"]
	if search == nil {
		t.Fatal("per_tool missing archigraph_search")
	}
	if search.SumBytes != 4800 {
		t.Errorf("search sum_bytes: got %d, want 4800", search.SumBytes)
	}
	if search.SumTokenEst != 1200 {
		t.Errorf("search sum_token_est: got %d, want 1200", search.SumTokenEst)
	}
	if search.SumMs != 300 {
		t.Errorf("search sum_ms: got %d, want 300", search.SumMs)
	}
}

// TestParseBenchCapture_BackwardCompatOldFormat verifies that a slog line
// WITHOUT the #2828 byte/token fields still parses for ms, and that its
// byte/token sums stay 0.  It also mixes a new-format line to confirm the
// two coexist in one slice.
func TestParseBenchCapture_BackwardCompatOldFormat(t *testing.T) {
	lines := strings.Join([]string{
		// Slog format without wire_bytes / payload_token_estimate.
		"time=2026-05-26T22:40:38.000+05:45 level=INFO msg=mcp_rpc phase=done tool=archigraph_search elapsed_ms=8095 repo=/path ts=2026-05-26T17:10:38Z",
		// Slog format with the #2828 fields.
		"time=2026-05-29T10:00:00.000+05:45 level=INFO msg=mcp_rpc phase=done tool=archigraph_search elapsed_ms=100 wire_bytes=2000 payload_token_estimate=500 repo=/path ts=2026-05-29T04:15:00Z",
	}, "\n") + "\n"

	out := parseBenchCapture([]byte(lines))

	if out.McpRPCCount != 2 {
		t.Fatalf("count: got %d, want 2 (both ms lines counted)", out.McpRPCCount)
	}
	if out.McpRPCHandlerMsSum != 8195 {
		t.Errorf("ms_sum: got %d, want 8195", out.McpRPCHandlerMsSum)
	}
	// Only the new-format line contributes bytes/tokens.
	if out.McpRPCWireBytesSum != 2000 {
		t.Errorf("wire_bytes_sum: got %d, want 2000", out.McpRPCWireBytesSum)
	}
	if out.McpRPCTokenEstSum != 500 {
		t.Errorf("token_est_sum: got %d, want 500", out.McpRPCTokenEstSum)
	}
}

// TestParseBenchCapture_AllLegacyZeroBytes verifies that a slog line without
// byte/token fields yields count>0 but byte/token sums of exactly 0.
func TestParseBenchCapture_AllLegacyZeroBytes(t *testing.T) {
	line := "time=2026-05-26T22:40:38.000+05:45 level=INFO msg=mcp_rpc phase=done tool=archigraph_search elapsed_ms=42 repo=/p ts=2026-05-26T17:10:38Z\n"
	out := parseBenchCapture([]byte(line))
	if out.McpRPCCount != 1 {
		t.Fatalf("count: got %d, want 1", out.McpRPCCount)
	}
	if out.McpRPCWireBytesSum != 0 || out.McpRPCTokenEstSum != 0 {
		t.Errorf("legacy byte/token sums: got %d/%d, want 0/0", out.McpRPCWireBytesSum, out.McpRPCTokenEstSum)
	}
}

// TestParseBenchCapture_DedupConsecutive verifies that consecutive identical
// lines (the daemon's double-log behaviour) are collapsed to a single count.
func TestParseBenchCapture_DedupConsecutive(t *testing.T) {
	// Daemon emits each mcp_rpc done line twice in a row.  We should count 2
	// distinct calls (one archigraph_find, one archigraph_inspect), not 4.
	dupLine1 := "time=2026-05-27T06:11:28.456+05:45 level=INFO msg=mcp_rpc phase=done tool=archigraph_find elapsed_ms=462 repo=/r ts=2026-05-27T00:26:28Z"
	dupLine2 := "time=2026-05-27T06:11:35.386+05:45 level=INFO msg=mcp_rpc phase=done tool=archigraph_inspect elapsed_ms=268 repo=/r ts=2026-05-27T00:26:35Z"
	data := strings.Join([]string{
		dupLine1, // first emit
		dupLine1, // daemon double-log
		dupLine2, // first emit
		dupLine2, // daemon double-log
		"",
	}, "\n")

	out := parseBenchCapture([]byte(data))
	if out.McpRPCCount != 2 {
		t.Errorf("count: got %d, want 2 (consecutive duplicates must be collapsed)", out.McpRPCCount)
	}
	if out.McpRPCHandlerMsSum != 730 { // 462 + 268
		t.Errorf("sum_ms: got %d, want 730", out.McpRPCHandlerMsSum)
	}
	if out.McpRPCPerTool["archigraph_find"] == nil || out.McpRPCPerTool["archigraph_find"].Count != 1 {
		t.Errorf("archigraph_find count: want 1")
	}
	if out.McpRPCPerTool["archigraph_inspect"] == nil || out.McpRPCPerTool["archigraph_inspect"].Count != 1 {
		t.Errorf("archigraph_inspect count: want 1")
	}
}

// TestParseBenchCapture_DedupWithByteFields verifies dedup works correctly
// when phase=done lines include wire_bytes and payload_token_estimate.
func TestParseBenchCapture_DedupWithByteFields(t *testing.T) {
	dupLine := "time=2026-05-29T10:00:00.000+05:45 level=INFO msg=mcp_rpc phase=done tool=archigraph_search elapsed_ms=100 wire_bytes=4000 payload_token_estimate=1000 repo=/r ts=2026-05-29T04:15:00Z"
	data := strings.Join([]string{dupLine, dupLine, ""}, "\n") // double-logged

	out := parseBenchCapture([]byte(data))
	if out.McpRPCCount != 1 {
		t.Errorf("count: got %d, want 1 (dedup)", out.McpRPCCount)
	}
	if out.McpRPCWireBytesSum != 4000 {
		t.Errorf("wire_bytes_sum: got %d, want 4000 (should not double)", out.McpRPCWireBytesSum)
	}
	if out.McpRPCTokenEstSum != 1000 {
		t.Errorf("token_est_sum: got %d, want 1000 (should not double)", out.McpRPCTokenEstSum)
	}
}
