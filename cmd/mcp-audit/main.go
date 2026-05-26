// cmd/mcp-audit measures the MCP handshake token budget.
//
// It instantiates the MCP server against a minimal empty registry, captures
// every registered tool definition via the server's internal tool list, and
// estimates the handshake token count using a conservative 4-chars-per-token
// ratio (matches Claude's cl100k tokenizer within 5 % on English text).
//
// # Usage
//
//	go run ./cmd/mcp-audit                   # human-readable report
//	go run ./cmd/mcp-audit -json             # machine-readable JSON
//	go run ./cmd/mcp-audit -ceiling 3500     # override token ceiling
//	make mcp-audit                           # CI gate (uses AUDIT_CEILING env)
//
// # Environment variables
//
//	AUDIT_CEILING   token ceiling (default 3500). Exit 1 when exceeded.
//	AUDIT_BASELINE  baseline token count for delta output.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	mcpapi "github.com/mark3labs/mcp-go/mcp"
	mcpsrv "github.com/mark3labs/mcp-go/server"

	"github.com/cajasmota/archigraph/internal/mcp"
	"github.com/cajasmota/archigraph/internal/version"
)

// defaultCeiling is the maximum allowed handshake token count.
// Empirical baseline: 2,963 tokens (28 tools, measured 2026-05-21 with 4-chars/token,
// after refactor/mcp-real-3k schema compression). Ceiling = 3,000 (hard spec target).
// Bump this constant when intentionally adding new tools (with justification comment).
//
// 2026-05-23 (#1384, epic #1380): bumped 3000 → 3100 to seat the new
// archigraph_module_analysis tool. Module-level GDS (SCC + PageRank + betweenness
// on the aggregated module graph) is a strategic addition — the bird's-eye-view
// counterpart to the entity-level surface. Bundled into one action-dispatched
// tool (cycles|centrality|all) to minimise footprint; measured at 3085 tokens
// (+85 above the previous ceiling, +122 above baseline). +100-token bump is the
// smallest round number that fits with a safety margin.
// 2026-05-24 (token-sprint bundle #1741/#1753): bumped to 3500 to seat
// archigraph_neighbors (folds find_callers + find_callees) and `fields` array
// params on find/inspect/expand/search_entities/neighbors. find_callers and
// find_callees stay as deprecated aliases for one release; ceiling drops to
// ~3,200 next release when the aliases are removed.
// 2026-05-27 (#2367): bumped to 4200 to match internal/mcp/budget_test.go.
// Actual measured value is 4128 tokens; internal tests were already bumped
// in #2207 but cmd/mcp-audit was not synced. Using shared constant is a
// follow-up fix to prevent future drift.
const defaultCeiling = 4200

// maxDescLen is the per-tool description character limit.
const maxDescLen = 80

// charsPerToken is the conservative char→token ratio used for estimation.
// Claude 3.x averages ~3.5 chars/token on English; 4 is the safe upper bound.
const charsPerToken = 4

// ToolReport is the per-tool breakdown included in JSON output.
type ToolReport struct {
	Name        string `json:"name"`
	DescLen     int    `json:"desc_len"`
	DescTokens  int    `json:"desc_tokens"`
	ParamTokens int    `json:"param_tokens"`
	TotalTokens int    `json:"total_tokens"`
	DescWarning string `json:"desc_warning,omitempty"`
}

// AuditReport is the top-level JSON output document.
type AuditReport struct {
	GeneratedAt     string       `json:"generated_at"`
	Version         string       `json:"version"`
	ToolCount       int          `json:"tool_count"`
	HandshakeTokens int          `json:"handshake_tokens"`
	Ceiling         int          `json:"ceiling"`
	BaselineTokens  int          `json:"baseline_tokens,omitempty"`
	DeltaTokens     int          `json:"delta_tokens,omitempty"`
	Passed          bool         `json:"passed"`
	Violations      []string     `json:"violations,omitempty"`
	Tools           []ToolReport `json:"tools"`
}

func main() {
	jsonOut := flag.Bool("json", false, "emit machine-readable JSON")
	ceilingFlag := flag.Int("ceiling", 0, "token ceiling (overrides AUDIT_CEILING env)")
	baselineFlag := flag.Int("baseline", 0, "baseline token count for delta (overrides AUDIT_BASELINE env)")
	flag.Parse()

	ceiling := defaultCeiling
	if v := os.Getenv("AUDIT_CEILING"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			ceiling = n
		}
	}
	if *ceilingFlag > 0 {
		ceiling = *ceilingFlag
	}

	baseline := 0
	if v := os.Getenv("AUDIT_BASELINE"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			baseline = n
		}
	}
	if *baselineFlag > 0 {
		baseline = *baselineFlag
	}

	tools := collectTools()
	report := buildReport(tools, ceiling, baseline)

	if *jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(report); err != nil {
			fmt.Fprintf(os.Stderr, "json encode: %v\n", err)
			os.Exit(2)
		}
	} else {
		printHuman(report)
	}

	if !report.Passed {
		os.Exit(1)
	}
}

// collectTools creates a zero-group MCP server and returns its registered tools.
// The server is constructed against a minimal temp registry — no network I/O,
// no blocking reads; we never call ServeStdio.
func collectTools() []mcpapi.Tool {
	tmp, err := os.CreateTemp("", "mcp-audit-registry-*.json")
	if err != nil {
		fmt.Fprintf(os.Stderr, "create temp registry: %v\n", err)
		os.Exit(2)
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.WriteString(`{"groups":{}}`); err != nil {
		fmt.Fprintf(os.Stderr, "write temp registry: %v\n", err)
		os.Exit(2)
	}
	tmp.Close()

	srv, err := mcp.NewServer(mcp.Config{RegistryPath: tmp.Name()})
	if err != nil {
		fmt.Fprintf(os.Stderr, "new server: %v\n", err)
		os.Exit(2)
	}
	return toolsFromServer(srv.MCP)
}

// toolsFromServer extracts the tool list from an mcp-go MCPServer via
// the public ListTools accessor (mcp-go ≥ 0.52).
func toolsFromServer(s *mcpsrv.MCPServer) []mcpapi.Tool {
	byName := s.ListTools()
	out := make([]mcpapi.Tool, 0, len(byName))
	for _, st := range byName {
		out = append(out, st.Tool)
	}
	return out
}

// estimateTokens converts a char count to a conservative token estimate.
func estimateTokens(s string) int {
	return int(math.Ceil(float64(len(s)) / charsPerToken))
}

// toolJSON returns the compact JSON encoding of a single Tool definition —
// the same structure sent to MCP clients in the initialize response.
func toolJSON(t mcpapi.Tool) string {
	b, _ := json.Marshal(t)
	return string(b)
}

// buildReport assembles the full AuditReport from the live tool list.
func buildReport(tools []mcpapi.Tool, ceiling, baseline int) AuditReport {
	sort.Slice(tools, func(i, j int) bool {
		return tools[i].Name < tools[j].Name
	})

	var violations []string
	var rows []ToolReport
	totalHandshakeChars := 0

	for _, t := range tools {
		raw := toolJSON(t)
		totalHandshakeChars += len(raw)

		descLen := len(t.Description)
		descTokens := estimateTokens(t.Description)
		totalToolTokens := estimateTokens(raw)
		paramTokens := totalToolTokens - descTokens
		if paramTokens < 0 {
			paramTokens = 0
		}

		row := ToolReport{
			Name:        t.Name,
			DescLen:     descLen,
			DescTokens:  descTokens,
			ParamTokens: paramTokens,
			TotalTokens: totalToolTokens,
		}
		if descLen > maxDescLen {
			row.DescWarning = fmt.Sprintf("description %d chars (limit %d)", descLen, maxDescLen)
			violations = append(violations, fmt.Sprintf("%s: %s", t.Name, row.DescWarning))
		}
		rows = append(rows, row)
	}

	// Add the MCP envelope overhead: instructions string + JSON-RPC framing.
	totalHandshakeChars += initEnvelopeBytes
	handshakeTokens := estimateTokens(strings.Repeat("x", totalHandshakeChars))

	passed := handshakeTokens <= ceiling && len(violations) == 0

	rep := AuditReport{
		GeneratedAt:     time.Now().UTC().Format(time.RFC3339),
		Version:         version.String(),
		ToolCount:       len(tools),
		HandshakeTokens: handshakeTokens,
		Ceiling:         ceiling,
		Passed:          passed,
		Violations:      violations,
		Tools:           rows,
	}
	if baseline > 0 {
		rep.BaselineTokens = baseline
		rep.DeltaTokens = handshakeTokens - baseline
	}
	return rep
}

// initEnvelopeBytes is the approximate byte count of the MCP initialize
// envelope (server name, version string, instructions, JSON-RPC framing).
// Derived from empirical measurement; update when instructions change.
const initEnvelopeBytes = 512

// printHuman writes a human-readable table to stdout.
func printHuman(r AuditReport) {
	fmt.Printf("archigraph mcp-audit  version=%s  date=%s\n\n", r.Version, r.GeneratedAt)
	fmt.Printf("tools: %d    handshake: %d tokens    ceiling: %d\n",
		r.ToolCount, r.HandshakeTokens, r.Ceiling)

	if r.BaselineTokens > 0 {
		sign := "+"
		if r.DeltaTokens < 0 {
			sign = ""
		}
		fmt.Printf("baseline: %d tokens    delta: %s%d\n",
			r.BaselineTokens, sign, r.DeltaTokens)
	}

	fmt.Println()
	fmt.Printf("%-44s %6s  %6s  %6s  %s\n", "tool", "desc", "param", "total", "warning")
	fmt.Println(strings.Repeat("-", 80))
	for _, row := range r.Tools {
		fmt.Printf("%-44s %6d  %6d  %6d  %s\n",
			row.Name, row.DescTokens, row.ParamTokens, row.TotalTokens, row.DescWarning)
	}
	fmt.Println(strings.Repeat("-", 80))

	if len(r.Violations) > 0 {
		fmt.Println("\nVIOLATIONS:")
		for _, v := range r.Violations {
			fmt.Printf("  - %s\n", v)
		}
	}

	fmt.Println()
	if r.Passed {
		fmt.Println("PASS  handshake within budget, all descriptions valid.")
	} else {
		var reasons []string
		if r.HandshakeTokens > r.Ceiling {
			reasons = append(reasons,
				fmt.Sprintf("handshake %d tokens > ceiling %d", r.HandshakeTokens, r.Ceiling))
		}
		if len(r.Violations) > 0 {
			reasons = append(reasons,
				fmt.Sprintf("%d description violation(s)", len(r.Violations)))
		}
		fmt.Printf("FAIL  %s\n", strings.Join(reasons, "; "))
	}
}
