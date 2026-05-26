package mcp_test

// schema_contract_2318_test.go — meta-test for issue #2318.
//
// Contract: every arg key that a handler reads via argInt/argString/argBool/argFloat
// MUST be declared in the corresponding tool's JSON-Schema Properties map.
//
// This file tests the specific gaps identified in the audit for #2318, and provides
// a helper (assertParamDeclared) that can be extended for future audits.
//
// If the AST-scanning meta-test (TestAllHandlerArgsDeclaredInSchema) is ever
// implemented as a full static-analysis pass, this file is the seed for it.
// For now, the contract is enforced via explicit per-gap assertions added when
// gaps are discovered and fixed.
//
// To extend: when a new arg is added to a handler via argInt/argString/argBool,
// add a corresponding assertParamDeclared case here. The test will catch future
// regressions where someone removes the WithNumber/WithString call from server.go
// without updating the handler (or vice versa).

import (
	"os"
	"testing"

	"github.com/cajasmota/archigraph/internal/mcp"
	mcpsrv "github.com/mark3labs/mcp-go/server"
)

// newMinimalServer creates a Server with an empty registry for schema tests.
func newMinimalServer(t *testing.T) *mcp.Server {
	t.Helper()
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
		t.Fatalf("NewServer: %v", err)
	}
	return srv
}

// assertParamDeclared fails the test if the named parameter is absent from
// the tool's InputSchema.Properties. It is a regression guard: once a param
// is declared it must stay declared.
func assertParamDeclared(t *testing.T, byName map[string]*mcpsrv.ServerTool, toolName, paramName string) {
	t.Helper()
	st, ok := byName[toolName]
	if !ok {
		t.Errorf("tool %q not registered (check registerTools in server.go)", toolName)
		return
	}
	props := st.Tool.InputSchema.Properties
	if props == nil {
		t.Errorf("tool %q has no InputSchema.Properties", toolName)
		return
	}
	if _, ok := props[paramName]; !ok {
		t.Errorf("tool %q is missing schema declaration for param %q — "+
			"add mcpapi.WithNumber/WithString/WithBoolean/WithArray(%q) to registerTools in server.go",
			toolName, paramName, paramName)
	}
}

// assertNumberDefault fails if the param's schema default does not match wantDefault.
func assertNumberDefault(t *testing.T, byName map[string]*mcpsrv.ServerTool, toolName, paramName string, wantDefault float64) {
	t.Helper()
	st, ok := byName[toolName]
	if !ok {
		t.Errorf("tool %q not registered", toolName)
		return
	}
	props := st.Tool.InputSchema.Properties
	if props == nil {
		t.Errorf("tool %q has no InputSchema.Properties", toolName)
		return
	}
	raw, ok := props[paramName]
	if !ok {
		t.Errorf("tool %q: param %q not in schema (cannot check default)", toolName, paramName)
		return
	}
	m, ok := raw.(map[string]any)
	if !ok {
		t.Errorf("tool %q param %q schema is %T, want map[string]any", toolName, paramName, raw)
		return
	}
	def, ok := m["default"]
	if !ok {
		// No default is acceptable for optional params without a default.
		return
	}
	var got float64
	switch v := def.(type) {
	case float64:
		got = v
	case int:
		got = float64(v)
	case int64:
		got = float64(v)
	default:
		t.Errorf("tool %q param %q default is unexpected type %T", toolName, paramName, def)
		return
	}
	if got != wantDefault {
		t.Errorf("tool %q param %q default = %v, want %v", toolName, paramName, got, wantDefault)
	}
}

// TestSchemaContract_2318_clusters verifies that the two args added to
// handleListCommunities in PR #2310 (issue #2289) are now declared in the
// archigraph_clusters tool schema (#2318).
//
// Before this fix, callers had no way to discover top_entities_limit or min_size
// from the tools/list handshake — they worked at runtime but were invisible to
// schema-aware clients and documentation generators.
func TestSchemaContract_2318_clusters(t *testing.T) {
	srv := newMinimalServer(t)
	byName := srv.MCP.ListTools()

	// top_entities_limit: default 3 (caps the top_entities array per community)
	assertParamDeclared(t, byName, "archigraph_clusters", "top_entities_limit")
	assertNumberDefault(t, byName, "archigraph_clusters", "top_entities_limit", 3)

	// min_size: default 20 (filters out small communities)
	assertParamDeclared(t, byName, "archigraph_clusters", "min_size")
	assertNumberDefault(t, byName, "archigraph_clusters", "min_size", 20)
}

// TestSchemaContract_2318_find_context_filter verifies that context_filter,
// which was documented in SCHEMA.md and read by handleQueryGraph, is now
// declared in the archigraph_find tool schema (#2318).
func TestSchemaContract_2318_find_context_filter(t *testing.T) {
	srv := newMinimalServer(t)
	byName := srv.MCP.ListTools()

	assertParamDeclared(t, byName, "archigraph_find", "context_filter")
}

// TestSchemaContract_2318_intentionally_undeclared documents the known
// intentional gaps (the #1639 pattern) so auditors can distinguish "intentional
// omission for token budget" from "accidental gap".
//
// This test does NOT assert those params are absent — that would be fragile.
// It only logs their names so a future developer reading the test output
// understands the policy.
func TestSchemaContract_2318_intentionally_undeclared(t *testing.T) {
	intentionalGaps := []struct {
		tool  string
		param string
		why   string
	}{
		// archigraph_find: verbose, min_score, max_results — #1639 pattern / #1921 / #1807
		{"archigraph_find", "verbose", "#1639 token ceiling pattern (#1921/#1807)"},
		{"archigraph_find", "min_score", "#1639 token ceiling pattern (#1921/#1807)"},
		{"archigraph_find", "max_results", "#1639 token ceiling pattern (#1921/#1807)"},
		// archigraph_traces: min_steps, cross_stack_only, verbose — #1639 pattern
		{"archigraph_traces", "min_steps", "#1639 token ceiling pattern"},
		{"archigraph_traces", "cross_stack_only", "#1639 token ceiling pattern"},
		{"archigraph_traces", "verbose", "#1639 token ceiling pattern"},
		// archigraph_find_callers/callees: verbose — #1639 token ceiling pattern
		{"archigraph_find_callers", "verbose", "#1639 token ceiling pattern"},
		{"archigraph_find_callees", "verbose", "#1639 token ceiling pattern"},
		// archigraph_module_analysis: top_n, limit, min_size, repo_filter — #1639 pattern
		{"archigraph_module_analysis", "top_n", "#1639 token ceiling pattern"},
		{"archigraph_module_analysis", "limit", "#1639 token ceiling pattern"},
		{"archigraph_module_analysis", "min_size", "#1639 token ceiling pattern"},
		// archigraph_repairs: submit-only args — #1756 / #1639 pattern
		{"archigraph_repairs", "residual_id", "#1756 token ceiling pattern"},
		{"archigraph_repairs", "resolution", "#1756 token ceiling pattern"},
	}

	for _, g := range intentionalGaps {
		t.Logf("intentional schema gap: tool=%s param=%s reason=%s", g.tool, g.param, g.why)
	}
	// No assertions — these are purely informational.
	// To enforce that intentional gaps do NOT get accidentally re-declared,
	// you could add assertParamAbsent calls here. Not done because intentional
	// declaration in the future is legitimate (just needs to update the budget).
}
