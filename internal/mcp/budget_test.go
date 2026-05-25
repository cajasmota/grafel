package mcp_test

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"testing"

	mcpapi "github.com/mark3labs/mcp-go/mcp"
	mcpsrv "github.com/mark3labs/mcp-go/server"

	"github.com/cajasmota/archigraph/internal/mcp"
)

// TestMCPHandshakeBudget verifies that the total MCP handshake size stays
// within the token ceiling defined by the spec (issue #1333).
//
// Budget:  3,500 tokens (ceiling)
// Method:  conservative 4-chars-per-token estimate, matching cmd/mcp-audit.
//
// When this test fails, you have added a tool or description that exceeds the
// budget. Options:
//   - Shorten tool descriptions to fit within maxDescLen (80 chars).
//   - Remove a tool or fold it into an existing action-dispatch bundle.
//   - Request a budget increase by updating defaultCeiling AND the SCHEMA.md
//     handshake section with a justification comment.
func TestMCPHandshakeBudget(t *testing.T) {
	const (
		// tokenCeiling matches cmd/mcp-audit defaultCeiling.
		// Baseline: 2,963 tokens (28 tools, measured 2026-05-21 post refactor/mcp-real-3k).
		// 2026-05-23 (#1384, epic #1380): ceiling bumped to 3,100 to seat the new
		// archigraph_module_analysis tool (module-level SCC/PageRank/betweenness).
		// Current measurement at 29 tools: 3,085 tokens. See cmd/mcp-audit/main.go.
		// 2026-05-23 (#1659): ceiling bumped to 3,200 to seat archigraph_apply_docgen_repairs
		// (docgen→graph repair feedback loop). 30 tools, measured 3,176 tokens.
		// 2026-05-23 (#1738): ceiling bumped to 3,300 to seat token_budget params on
		// expand/traces/endpoints/find_callers/find_callees (5 params, +48 tokens).
		// Measured: 3,248 tokens.
		// 2026-05-23 (#1754): ceiling bumped to 3,350 to seat archigraph_subgraph
		// unified tool. Pre-shim-drop measurement: 3,319 tokens (31 tools).
		// feat/drop-subgraph-shims: drop archigraph_get_subgraph + archigraph_summarize_subgraph
		// shims (0 real callers per #1742 research). Net saving: ~180 tokens.
		// 29 tools, ceiling lowered to 3,200.
		// 2026-05-24 (#1741/#1753/#1741/#1772 token sprint bundle): +archigraph_neighbors
		// tool (unifies find_callers + find_callees) + `fields` array param added to
		// find/inspect/expand/search_entities/neighbors for #1741 GraphQL-style
		// selection. Old find_callers/find_callees stay as deprecated aliases until
		// next release (one-release deprecation policy). Net: +1 tool, +5 fields params,
		// shorter sentinel/expand descriptions. Measured: 3,452 tokens (31 tools).
		// Ceiling bumped to 3,500 to seat the new surface. Drops to ~3,200 next release
		// when find_callers/find_callees aliases are removed.
		// PH5 (#2093): +archigraph_diff_refs (ref_a, ref_b, repo, group, cwd params).
		// Measured: 3,562 tokens (32 tools). Ceiling bumped to 3,600.
		// #2214 (epic #2207): +6 archigraph_docgen_* tools (start_run/status/validate/
		// promote/abort/list). These are the daemon-side docgen staging tools.
		// Measured: 4,128 tokens (38 tools). Ceiling bumped to 4,200.
		tokenCeiling  = 4200
		charsPerToken = 4
		envelopeBytes = 512 // initEnvelopeBytes constant from cmd/mcp-audit
		maxDescLen    = 80
	)

	tmp, err := os.CreateTemp(t.TempDir(), "registry-*.json")
	if err != nil {
		t.Fatalf("create temp registry: %v", err)
	}
	if _, err := tmp.WriteString(`{"groups":{}}`); err != nil {
		t.Fatalf("write temp registry: %v", err)
	}
	tmp.Close()

	srv, err := mcp.NewServer(mcp.Config{RegistryPath: tmp.Name()})
	if err != nil {
		t.Fatalf("new server: %v", err)
	}

	byName := srv.MCP.ListTools()
	if len(byName) == 0 {
		t.Fatal("no tools registered — server may not have initialised correctly")
	}

	totalChars := envelopeBytes
	var descViolations []string

	for name, st := range byName {
		b, err := json.Marshal(st.Tool)
		if err != nil {
			t.Fatalf("marshal tool %s: %v", name, err)
		}
		totalChars += len(b)

		if dl := len(st.Tool.Description); dl > maxDescLen {
			descViolations = append(descViolations,
				fmt.Sprintf("%s: description %d chars (limit %d)", name, dl, maxDescLen))
		}
	}

	handshakeTokens := int(math.Ceil(float64(totalChars) / charsPerToken))

	t.Logf("tools=%d  chars=%d  tokens=%d  ceiling=%d",
		len(byName), totalChars, handshakeTokens, tokenCeiling)

	for _, v := range descViolations {
		t.Errorf("description too long: %s", v)
	}

	if handshakeTokens > tokenCeiling {
		t.Errorf("handshake %d tokens exceeds ceiling %d (delta +%d)",
			handshakeTokens, tokenCeiling, handshakeTokens-tokenCeiling)
	}
}

// validateToolDescription returns an error string when the tool description
// is empty or exceeds maxDescLen.  Called from TestMCPToolDescriptions.
func validateToolDescription(t mcpapi.Tool, maxLen int) string {
	if t.Description == "" {
		return t.Name + ": description is empty"
	}
	if len(t.Description) > maxLen {
		return fmt.Sprintf("%s: description %d chars (limit %d)",
			t.Name, len(t.Description), maxLen)
	}
	return ""
}

// getSchemaDefault returns the numeric default for a named parameter of a tool,
// handling both int and float64 storage (mcp-go stores the concrete inferred
// type for generic DefaultNumber[T]).
func getSchemaDefault(t *testing.T, toolName, paramName string, byName map[string]*mcpsrv.ServerTool) float64 {
	t.Helper()
	st, ok := byName[toolName]
	if !ok {
		t.Fatalf("tool %q not registered", toolName)
	}
	props := st.Tool.InputSchema.Properties
	if props == nil {
		t.Fatalf("tool %q has no InputSchema.Properties", toolName)
	}
	raw, ok := props[paramName]
	if !ok {
		t.Fatalf("tool %q has no param %q", toolName, paramName)
	}
	m, ok := raw.(map[string]any)
	if !ok {
		t.Fatalf("tool %q param %q schema is %T, want map[string]any", toolName, paramName, raw)
	}
	def, ok := m["default"]
	if !ok {
		t.Fatalf("tool %q param %q has no default", toolName, paramName)
	}
	switch v := def.(type) {
	case float64:
		return v
	case int:
		return float64(v)
	case int64:
		return float64(v)
	default:
		t.Fatalf("tool %q param %q default is unexpected type %T", toolName, paramName, def)
	}
	return 0
}

// TestDefaultLimitsReduced verifies that the tool schema defaults for
// depth/limit on the token-economy tools are at the narrower values
// introduced in #1738.
func TestDefaultLimitsReduced(t *testing.T) {
	tmp, err := os.CreateTemp(t.TempDir(), "registry-*.json")
	if err != nil {
		t.Fatalf("create temp registry: %v", err)
	}
	if _, err := tmp.WriteString(`{"groups":{}}`); err != nil {
		t.Fatalf("write temp registry: %v", err)
	}
	tmp.Close()

	srv, err := mcp.NewServer(mcp.Config{RegistryPath: tmp.Name()})
	if err != nil {
		t.Fatalf("new server: %v", err)
	}

	byName := srv.MCP.ListTools()

	cases := []struct {
		tool  string
		param string
		want  float64
	}{
		{"archigraph_expand", "depth", 1},
		{"archigraph_traces", "limit", 10},
		{"archigraph_endpoints", "limit", 20},
		{"archigraph_find_callers", "depth", 1},
		{"archigraph_find_callees", "depth", 1},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.tool+"/"+tc.param, func(t *testing.T) {
			got := getSchemaDefault(t, tc.tool, tc.param, byName)
			if got != tc.want {
				t.Errorf("tool %q param %q: default = %v, want %v", tc.tool, tc.param, got, tc.want)
			}
		})
	}
}

// TestTokenBudgetParamPresent verifies that all list-returning tools that were
// extended in #1738 expose a token_budget param with default 800.
func TestTokenBudgetParamPresent(t *testing.T) {
	tmp, err := os.CreateTemp(t.TempDir(), "registry-*.json")
	if err != nil {
		t.Fatalf("create temp registry: %v", err)
	}
	if _, err := tmp.WriteString(`{"groups":{}}`); err != nil {
		t.Fatalf("write temp registry: %v", err)
	}
	tmp.Close()

	srv, err := mcp.NewServer(mcp.Config{RegistryPath: tmp.Name()})
	if err != nil {
		t.Fatalf("new server: %v", err)
	}

	byName := srv.MCP.ListTools()

	tools := []string{
		"archigraph_find",
		"archigraph_expand",
		"archigraph_traces",
		"archigraph_endpoints",
		"archigraph_find_callers",
		"archigraph_find_callees",
	}

	for _, toolName := range tools {
		toolName := toolName
		t.Run(toolName, func(t *testing.T) {
			got := getSchemaDefault(t, toolName, "token_budget", byName)
			if got != 800 {
				t.Errorf("tool %q token_budget default = %v, want 800", toolName, got)
			}
		})
	}
}

// TestMCPToolDescriptions checks that every registered tool has a non-empty
// description that fits within maxDescLen characters.
func TestMCPToolDescriptions(t *testing.T) {
	const maxDescLen = 80

	tmp, err := os.CreateTemp(t.TempDir(), "registry-*.json")
	if err != nil {
		t.Fatalf("create temp registry: %v", err)
	}
	if _, err := tmp.WriteString(`{"groups":{}}`); err != nil {
		t.Fatalf("write temp registry: %v", err)
	}
	tmp.Close()

	srv, err := mcp.NewServer(mcp.Config{RegistryPath: tmp.Name()})
	if err != nil {
		t.Fatalf("new server: %v", err)
	}

	byName := srv.MCP.ListTools()
	for name, st := range byName {
		if msg := validateToolDescription(st.Tool, maxDescLen); msg != "" {
			t.Errorf("%s", msg)
		}
		_ = name
	}
}
