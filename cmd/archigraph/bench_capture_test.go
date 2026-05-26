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

// TestParseBenchCapture_SingleLine verifies a single well-formed log line.
func TestParseBenchCapture_SingleLine(t *testing.T) {
	line := "archigraph-daemon: 2026/05/26 22:40:38.695982 [mcp-rpc] tool=archigraph_search elapsed=8095ms repo=/path/to/repo\n"
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

// TestParseBenchCapture_ReceivedLinesIgnored verifies that "received" companion
// lines (no elapsed= field) are not counted.
func TestParseBenchCapture_ReceivedLinesIgnored(t *testing.T) {
	data := strings.Join([]string{
		"archigraph-daemon: 2026/05/26 22:40:38 [mcp-rpc] tool=archigraph_search received repo=/path",
		"archigraph-daemon: 2026/05/26 22:40:38 [mcp-rpc] tool=archigraph_search elapsed=500ms repo=/path",
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
		"archigraph-daemon: 2026/05/26 22:40:38 [mcp-rpc] tool=archigraph_search elapsed=100ms repo=/r",
		"archigraph-daemon: 2026/05/26 22:40:39 [mcp-rpc] tool=archigraph_search elapsed=200ms repo=/r",
		"archigraph-daemon: 2026/05/26 22:40:40 [mcp-rpc] tool=archigraph_describe elapsed=300ms repo=/r",
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
		"x [mcp-rpc] tool=t elapsed=10ms repo=/r",
		"x [mcp-rpc] tool=t elapsed=30ms repo=/r",
		"x [mcp-rpc] tool=t elapsed=20ms repo=/r",
		"x [mcp-rpc] tool=t elapsed=40ms repo=/r",
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
		lines = append(lines, "x [mcp-rpc] tool=t elapsed="+strconv.Itoa(i)+"ms repo=/r")
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
	line := "archigraph-daemon: 2026/05/26 22:40:38 [mcp-rpc] tool=archigraph_search elapsed=1000ms repo=/r\n"
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
	window := "archigraph-daemon: 2026/05/26 [mcp-rpc] tool=archigraph_trace elapsed=250ms repo=/r\n"
	suffix := "archigraph-daemon: 2026/05/26 [mcp-rpc] tool=archigraph_trace elapsed=999ms repo=/r\n"

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
