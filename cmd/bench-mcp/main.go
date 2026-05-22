// cmd/bench-mcp benchmarks MCP handler latency against the live registry.
//
// It boots an in-process *mcp.Server (which reads ~/.archigraph/registry.json),
// looks up each tool's handler by name via the underlying *mcpsrv.MCPServer,
// and runs a fixed set of representative queries N times each, reporting
// per-tool median / p95 elapsed_ms.
//
// Usage:
//
//	go run ./cmd/bench-mcp -group upvate -runs 5 -out docs/verify2/mcp-speed-after.json
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"
	"time"

	mcpapi "github.com/mark3labs/mcp-go/mcp"

	"github.com/cajasmota/archigraph/internal/mcp"
)

type query struct {
	Name string
	Tool string
	Args map[string]any
}

type sample struct {
	Name      string  `json:"name"`
	Tool      string  `json:"tool"`
	Runs      int     `json:"runs"`
	MedianMS  float64 `json:"median_ms"`
	P95MS     float64 `json:"p95_ms"`
	MinMS     float64 `json:"min_ms"`
	MaxMS     float64 `json:"max_ms"`
	BytesOut  int     `json:"bytes_out"`
	IsError   bool    `json:"is_error,omitempty"`
	ErrorText string  `json:"error_text,omitempty"`
}

type report struct {
	Group     string    `json:"group"`
	Runs      int       `json:"runs"`
	Timestamp time.Time `json:"timestamp"`
	Samples   []sample  `json:"samples"`
}

func main() {
	group := flag.String("group", "upvate", "group name to bench against")
	runs := flag.Int("runs", 5, "runs per query")
	out := flag.String("out", "", "output JSON path (default stdout)")
	flag.Parse()

	srv, err := mcp.NewServer(mcp.Config{})
	if err != nil {
		fmt.Fprintln(os.Stderr, "boot mcp server:", err)
		os.Exit(1)
	}
	g := *group

	queries := []query{
		{
			Name: "find: authentication middleware",
			Tool: "archigraph_find",
			Args: map[string]any{"question": "authentication middleware", "group": g, "depth": 3.0, "token_budget": 800.0},
		},
		{
			Name: "inspect: TokenAuthenticationMiddleware",
			Tool: "archigraph_inspect",
			Args: map[string]any{"label_or_id": "TokenAuthenticationMiddleware", "group": g},
		},
		{
			Name: "get_source: TokenAuthenticationMiddleware.process_request",
			Tool: "archigraph_get_source",
			Args: map[string]any{"node_id": "TokenAuthenticationMiddleware.process_request", "group": g, "context_lines": 20.0},
		},
		{
			Name: "find_callers: TokenAuthenticationMiddleware (label-resolve)",
			Tool: "archigraph_find_callers",
			Args: map[string]any{"entity_id": "upvate-core::c84f9b9c0c3a7b18", "group": g, "depth": 2.0},
		},
		{
			Name: "find_callers: depth=3 (heavier BFS)",
			Tool: "archigraph_find_callers",
			Args: map[string]any{"entity_id": "upvate-core::c84f9b9c0c3a7b18", "group": g, "depth": 3.0},
		},
		{
			Name: "traces: list",
			Tool: "archigraph_traces",
			Args: map[string]any{"action": "list", "group": g, "limit": 25.0},
		},
		{
			Name: "traces: follow (deep BFS)",
			Tool: "archigraph_traces",
			Args: map[string]any{"action": "follow", "group": g, "entry_point_id": "upvate-core::14d45f8830972c90", "max_depth": 8.0, "branching_factor": 3.0},
		},
		{
			Name: "expand: depth=2 (neighbors)",
			Tool: "archigraph_expand",
			Args: map[string]any{"node": "upvate-core::c84f9b9c0c3a7b18", "group": g, "depth": 2.0},
		},
		{
			Name: "impact_radius: depth=2",
			Tool: "archigraph_impact_radius",
			Args: map[string]any{"entity_id": "upvate-core::c84f9b9c0c3a7b18", "group": g, "depth": 2.0},
		},
		{
			Name: "endpoints: definitions path=proposal",
			Tool: "archigraph_endpoints",
			Args: map[string]any{"action": "definitions", "group": g, "path_contains": "proposal", "limit": 50.0},
		},
		{
			Name: "endpoints: definitions all",
			Tool: "archigraph_endpoints",
			Args: map[string]any{"action": "definitions", "group": g, "limit": 200.0},
		},
		{
			Name: "stats: corpus",
			Tool: "archigraph_stats",
			Args: map[string]any{"group": g},
		},
	}

	rep := report{Group: g, Runs: *runs, Timestamp: time.Now()}
	for _, q := range queries {
		s := runQuery(srv, q, *runs)
		rep.Samples = append(rep.Samples, s)
		fmt.Fprintf(os.Stderr, "%-55s median=%7.1fms p95=%7.1fms min=%7.1fms max=%7.1fms bytes=%d\n",
			q.Name, s.MedianMS, s.P95MS, s.MinMS, s.MaxMS, s.BytesOut)
	}

	data, _ := json.MarshalIndent(rep, "", "  ")
	if *out != "" {
		if err := os.WriteFile(*out, data, 0644); err != nil {
			fmt.Fprintln(os.Stderr, "write:", err)
			os.Exit(1)
		}
		fmt.Fprintln(os.Stderr, "wrote", *out)
	} else {
		fmt.Println(string(data))
	}
}

func runQuery(srv *mcp.Server, q query, runs int) sample {
	st := srv.MCP.GetTool(q.Tool)
	if st == nil {
		return sample{Name: q.Name, Tool: q.Tool, IsError: true, ErrorText: "tool not registered"}
	}
	h := st.Handler

	mkReq := func() mcpapi.CallToolRequest {
		r := mcpapi.CallToolRequest{}
		r.Params.Arguments = q.Args
		r.Params.Name = q.Tool
		return r
	}

	// Warmup once (uncounted).
	var bytesOut int
	var isErr bool
	var errText string
	{
		res, err := h(context.Background(), mkReq())
		if err == nil && res != nil && res.IsError {
			isErr = true
			for _, c := range res.Content {
				if tc, ok := c.(mcpapi.TextContent); ok {
					errText = tc.Text
					break
				}
			}
		}
	}

	timings := make([]float64, 0, runs)
	for i := 0; i < runs; i++ {
		start := time.Now()
		res, _ := h(context.Background(), mkReq())
		elapsed := float64(time.Since(start).Microseconds()) / 1000.0
		timings = append(timings, elapsed)
		if res != nil && i == runs-1 {
			for _, c := range res.Content {
				if tc, ok := c.(mcpapi.TextContent); ok {
					bytesOut += len(tc.Text)
				}
			}
		}
	}
	sort.Float64s(timings)
	med := timings[len(timings)/2]
	p95i := int(float64(len(timings)-1) * 0.95)
	p95 := timings[p95i]
	return sample{
		Name:      q.Name,
		Tool:      q.Tool,
		Runs:      runs,
		MedianMS:  med,
		P95MS:     p95,
		MinMS:     timings[0],
		MaxMS:     timings[len(timings)-1],
		BytesOut:  bytesOut,
		IsError:   isErr,
		ErrorText: errText,
	}
}
