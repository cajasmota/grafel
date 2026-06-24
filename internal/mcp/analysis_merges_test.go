package mcp

import (
	"context"
	"encoding/json"
	"testing"

	mcpapi "github.com/mark3labs/mcp-go/mcp"
)

// analysis_merges_test.go — dispatch tests for the ANALYSIS-cluster canonical
// tools (#5546/#5550). Each test asserts that a discriminator value on the new
// canonical handler produces the same output as the absorbed handler it routes
// to (order-insensitive via normalizeForCompare, defined in core_merges_test.go).
// Helpers coreTestServer / callBare / assertSameDispatch are shared from there.

// 1. grafel_debt kind= → dead_code/find_dead_code/cycles/import_cycles/stubs/impure/license.
func TestAnalysisDebtDispatch(t *testing.T) {
	srv := coreTestServer(t)
	g := map[string]any{"group": "g"}
	with := func(kind string) map[string]any {
		return map[string]any{"group": "g", "kind": kind}
	}
	stub := map[string]any{"group_v3": "g", "group_oracle": "g"}
	stubKind := map[string]any{"group_v3": "g", "group_oracle": "g", "kind": "stubs"}
	assertSameDispatch(t, "kind=dead_code", srv.handleAnalysisDebt, with("dead_code"), srv.handleDeadCode, g)
	assertSameDispatch(t, "kind=default", srv.handleAnalysisDebt, g, srv.handleDeadCode, g)
	assertSameDispatch(t, "kind=find_dead_code", srv.handleAnalysisDebt, with("find_dead_code"), srv.handleFindDeadCode, g)
	assertSameDispatch(t, "kind=cycles", srv.handleAnalysisDebt, with("cycles"), srv.handleQualityCycles, g)
	assertSameDispatch(t, "kind=import_cycles", srv.handleAnalysisDebt, with("import_cycles"), srv.handleModuleCyclesSidecar, g)
	assertSameDispatch(t, "kind=stubs", srv.handleAnalysisDebt, stubKind, srv.handleStubDetector, stub)
	assertSameDispatch(t, "kind=impure", srv.handleAnalysisDebt, with("impure"), srv.handlePureFunctions, g)
	assertSameDispatch(t, "kind=license", srv.handleAnalysisDebt, with("license"), srv.handleLicenseAudit, g)
}

// 2. grafel_security kind= → findings/secrets/auth_coverage.
func TestAnalysisSecurityDispatch(t *testing.T) {
	srv := coreTestServer(t)
	g := map[string]any{"group": "g"}
	with := func(kind string) map[string]any {
		return map[string]any{"group": "g", "kind": kind}
	}
	assertSameDispatch(t, "kind=findings", srv.handleAnalysisSecurity, with("findings"), srv.handleSecurityFindings, g)
	assertSameDispatch(t, "kind=default", srv.handleAnalysisSecurity, g, srv.handleSecurityFindings, g)
	assertSameDispatch(t, "kind=secrets", srv.handleAnalysisSecurity, with("secrets"), srv.handleSecrets, g)
	assertSameDispatch(t, "kind=auth_coverage", srv.handleAnalysisSecurity, with("auth_coverage"), srv.handleAuthCoverage, g)
}

// 3. grafel_test_analysis kind= → coverage/reachability/contract_effectiveness/coverage_effectiveness.
func TestAnalysisTestDispatch(t *testing.T) {
	srv := coreTestServer(t)
	g := map[string]any{"group": "g"}
	with := func(kind string) map[string]any {
		return map[string]any{"group": "g", "kind": kind}
	}
	assertSameDispatch(t, "kind=coverage", srv.handleAnalysisTest, with("coverage"), srv.handleTestCoverage, g)
	assertSameDispatch(t, "kind=default", srv.handleAnalysisTest, g, srv.handleTestCoverage, g)
	assertSameDispatch(t, "kind=reachability", srv.handleAnalysisTest, with("reachability"), srv.handleTestReachability, g)
	assertSameDispatch(t, "kind=contract_effectiveness", srv.handleAnalysisTest, with("contract_effectiveness"), srv.handleContractTestEffectiveness, g)
	assertSameDispatch(t, "kind=coverage_effectiveness", srv.handleAnalysisTest, with("coverage_effectiveness"), srv.handleCoverageEffectiveness, g)
}

// 4. grafel_patterns kind= → code (agent store) / graph / template.
func TestAnalysisPatternsDispatch(t *testing.T) {
	srv := coreTestServer(t)
	// code: handlePatterns reads its own action=; query is the read path.
	codeArgs := map[string]any{"group": "g", "action": "query", "text": "x"}
	assertSameDispatch(t, "kind=code", srv.handleAnalysisPatterns,
		map[string]any{"group": "g", "kind": "code", "action": "query", "text": "x"},
		srv.handlePatterns, codeArgs)
	assertSameDispatch(t, "kind=default", srv.handleAnalysisPatterns, codeArgs, srv.handlePatterns, codeArgs)
	// graph: dispatcher defaults action=list.
	assertSameDispatch(t, "kind=graph", srv.handleAnalysisPatterns,
		map[string]any{"group": "g", "kind": "graph"},
		srv.handleGraphPatterns, map[string]any{"group": "g", "action": "list"})
	// template.
	assertSameDispatch(t, "kind=template", srv.handleAnalysisPatterns,
		map[string]any{"group": "g", "kind": "template"},
		srv.handleTemplatePatterns, map[string]any{"group": "g"})
}

// 5. grafel_findings action= → list / save.
func TestAnalysisFindingsDispatch(t *testing.T) {
	srv := coreTestServer(t)
	g := map[string]any{"group": "g"}
	assertSameDispatch(t, "action=list", srv.handleAnalysisFindings,
		map[string]any{"group": "g", "action": "list"}, srv.handleListFindings, g)
	assertSameDispatch(t, "action=default", srv.handleAnalysisFindings, g, srv.handleListFindings, g)
	// save: handleSaveResult requires question + answer.
	save := map[string]any{"group": "g", "question": "q", "answer": "a"}
	assertSameDispatch(t, "action=save", srv.handleAnalysisFindings,
		map[string]any{"group": "g", "action": "save", "question": "q", "answer": "a"},
		srv.handleSaveResult, save)
}

// 6. grafel_diff aspect= → response_shape/payload/auth/literals/refs.
// The return is a discriminated union keyed by `aspect`: we compare the
// canonical result with the absorbed handler's result after STRIPPING the
// injected aspect key, then separately assert the aspect key is present + correct.
func TestAnalysisDiffDispatch(t *testing.T) {
	srv := coreTestServer(t)
	cross := func(aspect string) map[string]any {
		return map[string]any{"group_oracle": "g", "group_v3": "g", "aspect": aspect}
	}
	crossBare := map[string]any{"group_oracle": "g", "group_v3": "g"}

	type diffCase struct {
		aspect  string
		canon   map[string]any
		old     func(context.Context, mcpapi.CallToolRequest) (*mcpapi.CallToolResult, error)
		oldArgs map[string]any
	}
	cases := []diffCase{
		{"response_shape", cross("response_shape"), srv.handleResponseShapeDiff, crossBare},
		{"payload", map[string]any{"group": "g", "aspect": "payload"}, srv.handlePayloadDrift, map[string]any{"group": "g"}},
		{"auth", cross("auth"), srv.handleAuthPostureDiff, crossBare},
		{"literals", map[string]any{"group_oracle": "g", "group_v3": "g", "set": "page_slugs", "aspect": "literals"}, srv.handleLiteralParity, map[string]any{"group_oracle": "g", "group_v3": "g", "set": "page_slugs"}},
		{"refs", map[string]any{"group": "g", "repo": "r1", "ref_a": "main", "ref_b": "main", "aspect": "refs"}, srv.handleDiffRefs, map[string]any{"group": "g", "repo": "r1", "ref_a": "main", "ref_b": "main"}},
	}
	for _, c := range cases {
		got := callBare(t, srv.handleAnalysisDiff, c.canon)
		want := callBare(t, c.old, c.oldArgs)
		assertDiffAspect(t, c.aspect, got, want)
	}

	// default aspect=response_shape.
	gotDefault := callBare(t, srv.handleAnalysisDiff, crossBare)
	wantDefault := callBare(t, srv.handleResponseShapeDiff, crossBare)
	assertDiffAspect(t, "response_shape", gotDefault, wantDefault)
}

// assertDiffAspect verifies the canonical grafel_diff result equals the absorbed
// handler's result once the injected aspect key is removed, and that the aspect
// key was present with the expected value. Non-JSON-object (error) results pass
// through unchanged, in which case got must equal want verbatim.
func assertDiffAspect(t *testing.T, aspect, got, want string) {
	t.Helper()
	var gotObj map[string]json.RawMessage
	if err := json.Unmarshal([]byte(got), &gotObj); err != nil {
		// Not a JSON object (error/text result) — stampAspect is a no-op, so
		// the canonical result must be byte-identical to the absorbed handler.
		if got != want {
			t.Errorf("aspect=%s (non-object): canonical=%q want=%q", aspect, got, want)
		}
		return
	}
	a, ok := gotObj["aspect"]
	if !ok {
		t.Errorf("aspect=%s: result missing injected aspect key", aspect)
		return
	}
	if string(a) != `"`+aspect+`"` {
		t.Errorf("aspect=%s: injected aspect=%s, want %q", aspect, a, aspect)
	}
	delete(gotObj, "aspect")
	stripped, err := json.Marshal(gotObj)
	if err != nil {
		t.Fatalf("re-marshal: %v", err)
	}
	// want is the absorbed handler's native JSON object; re-marshal it through
	// the same map round-trip so key ordering matches.
	var wantObj map[string]json.RawMessage
	if err := json.Unmarshal([]byte(want), &wantObj); err != nil {
		t.Fatalf("aspect=%s: absorbed handler result not a JSON object: %v", aspect, err)
	}
	wantNorm, _ := json.Marshal(wantObj)
	if string(stripped) != string(wantNorm) {
		t.Errorf("aspect=%s: stripped canonical differs from absorbed handler\n got=%s\nwant=%s", aspect, stripped, wantNorm)
	}
}

// 7. All six ANALYSIS canonical tools are registered (#5546/#5550).
func TestAnalysisCanonicalToolsRegistered(t *testing.T) {
	srv := coreTestServer(t)
	registered := map[string]bool{}
	for _, st := range srv.MCP.ListTools() {
		registered[st.Tool.Name] = true
	}
	for _, n := range []string{
		"grafel_debt", "grafel_security", "grafel_test_analysis",
		"grafel_patterns", "grafel_findings", "grafel_diff",
	} {
		if !registered[n] {
			t.Errorf("ANALYSIS canonical tool %q not registered", n)
		}
	}
}
