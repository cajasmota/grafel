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

	"github.com/cajasmota/grafel/internal/mcp"
	"github.com/cajasmota/grafel/internal/version"
)

// defaultCeiling is the maximum allowed handshake token count.
// The value is sourced from mcp.TokenCeiling (internal/mcp/budget.go) —
// the single source of truth shared with internal/mcp/budget_test.go.
// See that file's const comment for the full bump history.
// To raise the ceiling, update TokenCeiling in internal/mcp/budget.go only.
var defaultCeiling = mcp.TokenCeiling

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
// Hidden aliases (#5546/#5552) are registered/callable but excluded from the
// tools/list handshake, so they must NOT count against the handshake budget —
// the audit measures the *advertised* surface (the real per-connect cost).
func toolsFromServer(s *mcpsrv.MCPServer) []mcpapi.Tool {
	byName := s.ListTools()
	out := make([]mcpapi.Tool, 0, len(byName))
	for name, st := range byName {
		if mcp.IsHiddenAlias(name) {
			continue
		}
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
//
// Breakdown: ~339 bytes of fixed framing (server name/version + JSON-RPC) plus
// the mcpInstructions orientation map (~2053 bytes; internal/mcp/server.go).
// When mcpInstructions changes, recompute as framing + len(mcpInstructions).
const initEnvelopeBytes = 2392

// printHuman writes a human-readable table to stdout.
func printHuman(r AuditReport) {
	fmt.Printf("grafel mcp-audit  version=%s  date=%s\n\n", r.Version, r.GeneratedAt)
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
