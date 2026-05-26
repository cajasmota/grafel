package main

// bench_capture.go — `archigraph bench-capture rpc` subcommand.
//
// Reads a daemon log slice between two byte offsets, parses every
// [mcp-rpc] tool=<X> elapsed=<N>ms line, aggregates counts + handler
// durations, and emits JSON to stdout matching the BenchCaptureOutput
// schema in:
//
//	skills/archigraph-graph-quality/schema/with-mcp-artifact.schema.json
//
// Closes #2298.

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/spf13/pflag"
)

// defaultDaemonLog is the default daemon log path used when --log is not given.
const defaultDaemonLog = "~/.archigraph/logs/daemon.log"

// rpcLineRe matches lines produced by internal/daemon/mcp_rpc.go:
//
//	archigraph-daemon: 2026/05/26 22:40:38 [mcp-rpc] tool=archigraph_search elapsed=8095ms repo=/path
//
// Only lines with an elapsed= field are counted (received-only lines have no
// elapsed= and must be ignored).
var rpcLineRe = regexp.MustCompile(`\[mcp-rpc\] tool=(\S+) elapsed=(\d+)ms repo=`)

// ToolRPCStats aggregates daemon-side handler durations for one tool.
// Matches the ToolRPCStats definition in with-mcp-artifact.schema.json.
type ToolRPCStats struct {
	Count  int `json:"count"`
	SumMs  int `json:"sum_ms"`
}

// BenchCaptureOutput is the top-level JSON emitted to stdout.
// Field names and types are normative — they match the BenchCaptureOutput
// $def in with-mcp-artifact.schema.json and the mcp_rpc_* fields in
// QuestionMetrics, so the output can be merged directly into a question's
// metrics block without renaming.
type BenchCaptureOutput struct {
	McpRPCCount        int                      `json:"mcp_rpc_count"`
	McpRPCHandlerMsSum int                      `json:"mcp_rpc_handler_ms_sum"`
	// p50 and p99 are null (nil pointer → JSON null) when count == 0.
	McpRPCHandlerMsP50 *float64                 `json:"mcp_rpc_handler_ms_p50"`
	McpRPCHandlerMsP99 *float64                 `json:"mcp_rpc_handler_ms_p99"`
	McpRPCPerTool      map[string]*ToolRPCStats `json:"mcp_rpc_per_tool"`
}

// runBenchCaptureRPC is the entry-point for `archigraph bench-capture rpc`.
// argv is everything after "bench-capture rpc".
func runBenchCaptureRPC(argv []string) error {
	fs := pflag.NewFlagSet("bench-capture rpc", pflag.ContinueOnError)
	logPath := fs.String("log", defaultDaemonLog, "path to daemon log file")
	startOff := fs.Int64("start-offset", 0, "byte offset to start reading from (inclusive)")
	endOff := fs.Int64("end-offset", -1, "byte offset to stop reading at (exclusive); -1 = EOF")

	if err := fs.Parse(argv); err != nil {
		return err
	}

	// Expand ~ in the log path.
	expanded, err := expandTilde(*logPath)
	if err != nil {
		return fmt.Errorf("expand log path: %w", err)
	}

	data, err := readSlice(expanded, *startOff, *endOff)
	if err != nil {
		return fmt.Errorf("read log slice: %w", err)
	}

	out := parseBenchCapture(data)

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}

// expandTilde replaces a leading ~ with the user's home directory.
func expandTilde(p string) (string, error) {
	if !strings.HasPrefix(p, "~") {
		return p, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, p[1:]), nil
}

// readSlice opens path and reads [start, end). If end < 0, reads to EOF.
// Returns an empty slice when start >= file size (no lines to parse).
func readSlice(path string, start, end int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // empty input → zero metrics
		}
		return nil, err
	}
	defer f.Close()

	if start > 0 {
		if _, err := f.Seek(start, io.SeekStart); err != nil {
			return nil, err
		}
	}

	if end < 0 {
		return io.ReadAll(f)
	}

	limit := end - start
	if limit <= 0 {
		return nil, nil
	}
	return io.ReadAll(io.LimitReader(f, limit))
}

// parseBenchCapture scans log bytes for [mcp-rpc] elapsed= lines and
// aggregates counts, sums, and per-tool breakdowns. It then computes
// p50 and p99 over the collected durations.
//
// Percentile formulas (0-indexed, sorted ascending):
//
//	p50: sorted[(n-1)/2]  for odd n
//	     average(sorted[n/2-1], sorted[n/2]) for even n
//	p99: sorted[ ceil(n*0.99) - 1 ] clamped to [0, n-1]
func parseBenchCapture(data []byte) BenchCaptureOutput {
	out := BenchCaptureOutput{
		McpRPCPerTool: make(map[string]*ToolRPCStats),
	}

	if len(data) == 0 {
		return out
	}

	var allMs []int

	for _, line := range strings.Split(string(data), "\n") {
		m := rpcLineRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		tool := m[1]
		ms, err := strconv.Atoi(m[2])
		if err != nil {
			continue
		}

		out.McpRPCCount++
		out.McpRPCHandlerMsSum += ms
		allMs = append(allMs, ms)

		if _, ok := out.McpRPCPerTool[tool]; !ok {
			out.McpRPCPerTool[tool] = &ToolRPCStats{}
		}
		out.McpRPCPerTool[tool].Count++
		out.McpRPCPerTool[tool].SumMs += ms
	}

	if out.McpRPCCount == 0 {
		// Leave p50/p99 as nil (→ JSON null) and per-tool empty.
		return out
	}

	sort.Ints(allMs)
	n := len(allMs)

	// p50 — median.
	var p50 float64
	if n%2 == 1 {
		p50 = float64(allMs[(n-1)/2])
	} else {
		p50 = float64(allMs[n/2-1]+allMs[n/2]) / 2.0
	}
	out.McpRPCHandlerMsP50 = &p50

	// p99 — 99th percentile, clamped to valid index.
	idx := int(math.Ceil(float64(n)*0.99)) - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= n {
		idx = n - 1
	}
	p99 := float64(allMs[idx])
	out.McpRPCHandlerMsP99 = &p99

	return out
}

// benchCaptureSubcommands maps subverb names to their handler functions.
// Adding a new verb (e.g., "tokens") is a simple map entry addition.
var benchCaptureSubcommands = map[string]func([]string) error{
	"rpc": runBenchCaptureRPC,
}

// runBenchCaptureDispatch handles `archigraph bench-capture <subverb> [flags]`.
// It looks up the subverb in the subcommand table and delegates.
func runBenchCaptureDispatch(argv []string) error {
	if len(argv) == 0 {
		return fmt.Errorf("usage: archigraph bench-capture <subverb> [flags]\n  subverbs: rpc")
	}
	handler, ok := benchCaptureSubcommands[argv[0]]
	if !ok {
		var supported []string
		for k := range benchCaptureSubcommands {
			supported = append(supported, k)
		}
		return fmt.Errorf("unknown bench-capture subverb %q; supported: %v", argv[0], supported)
	}
	return handler(argv[1:])
}
