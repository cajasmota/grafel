package mcp

import (
	"context"
	"encoding/json"

	mcpapi "github.com/mark3labs/mcp-go/mcp"
)

// analysis_merges.go — ANALYSIS-cluster canonical tool dispatchers (epic #5546,
// #5550).
//
// Second of the 0.1.5 MCP-consolidation clusters (the CORE cluster lives in
// core_merges.go, #5549). Each canonical tool reads a single discriminator
// argument (kind / action / aspect) and dispatches to the EXISTING analysis
// handler funcs unchanged — behaviour is byte-identical to the absorbed tools.
// No analytical logic lives here; this is pure routing. The absorbed tools stay
// registered as standalone tools for back-compat until #5552 converts them to
// hidden aliases.
//
// reqWithArgs (defined in core_merges.go) is reused where a discriminator's
// value must be rewritten into an inner argument the delegate reads itself.

// handleAnalysisDebt routes grafel_debt by kind= over the tech-debt /
// code-health handlers. Default kind=dead_code — the hot "what's unwired?" case.
//
//	dead_code (default) → handleDeadCode          (reachability dead-code)
//	find_dead_code      → handleFindDeadCode      (isolated/marked-unused/test-only)
//	cycles              → handleQualityCycles     (import cycles, Tarjan SCC)
//	import_cycles       → handleModuleCyclesSidecar (IMPORTS SCC sidecar)
//	stubs               → handleStubDetector      (v3 pure where oracle computes)
//	impure              → handlePureFunctions     (functions with no effects)
//	license             → handleLicenseAudit      (dependency-license conflicts)
func (s *Server) handleAnalysisDebt(ctx context.Context, req mcpapi.CallToolRequest) (*mcpapi.CallToolResult, error) {
	switch argString(req, "kind", "dead_code") {
	case "find_dead_code", "unwired":
		return s.handleFindDeadCode(ctx, req)
	case "cycles":
		return s.handleQualityCycles(ctx, req)
	case "import_cycles":
		return s.handleModuleCyclesSidecar(ctx, req)
	case "stubs", "stub":
		return s.handleStubDetector(ctx, req)
	case "impure", "pure", "pure_functions":
		return s.handlePureFunctions(ctx, req)
	case "license", "licenses":
		return s.handleLicenseAudit(ctx, req)
	default: // "dead_code"
		return s.handleDeadCode(ctx, req)
	}
}

// handleAnalysisSecurity routes grafel_security by kind=. Default kind=findings
// (taint-flow security findings).
//
//	findings (default) → handleSecurityFindings (taint source→sink paths)
//	secrets            → handleSecrets          (hardcoded-secret scan)
//	auth_coverage      → handleAuthCoverage     (endpoints missing auth)
func (s *Server) handleAnalysisSecurity(ctx context.Context, req mcpapi.CallToolRequest) (*mcpapi.CallToolResult, error) {
	switch argString(req, "kind", "findings") {
	case "secrets":
		return s.handleSecrets(ctx, req)
	case "auth_coverage", "auth":
		return s.handleAuthCoverage(ctx, req)
	default: // "findings"
		return s.handleSecurityFindings(ctx, req)
	}
}

// handleAnalysisTest routes grafel_test_analysis by kind=. Default
// kind=coverage (production entities with no TESTS edge).
//
//	coverage (default)     → handleTestCoverage           (no TESTS edge)
//	reachability           → handleTestReachability       (no test path / orphans)
//	contract_effectiveness → handleContractTestEffectiveness (tautological specs)
//	coverage_effectiveness → handleCoverageEffectiveness  (reachable-but-0%-lines)
func (s *Server) handleAnalysisTest(ctx context.Context, req mcpapi.CallToolRequest) (*mcpapi.CallToolResult, error) {
	switch argString(req, "kind", "coverage") {
	case "reachability", "reach":
		return s.handleTestReachability(ctx, req)
	case "contract_effectiveness", "contract_eff", "contract":
		return s.handleContractTestEffectiveness(ctx, req)
	case "coverage_effectiveness", "coverage_eff", "effectiveness":
		return s.handleCoverageEffectiveness(ctx, req)
	default: // "coverage"
		return s.handleTestCoverage(ctx, req)
	}
}

// handleAnalysisPatterns routes grafel_patterns by kind=. Default kind=code
// preserves the existing agent pattern store (handlePatterns reads its own
// action=query|record), so back-compat is byte-identical for callers that pass
// only action=.
//
//	code (default) → handlePatterns         (agent-learned pattern store)
//	graph          → handleGraphPatterns    (indexer-extracted patterns)
//	template       → handleTemplatePatterns (i18n/log_format/sql literals)
func (s *Server) handleAnalysisPatterns(ctx context.Context, req mcpapi.CallToolRequest) (*mcpapi.CallToolResult, error) {
	switch argString(req, "kind", "code") {
	case "graph", "graph_patterns":
		// handleGraphPatterns requires action=; default to list when the caller
		// routed in via kind= without an explicit action.
		if argString(req, "action", "") == "" {
			req = reqWithArgs(req, map[string]any{"action": "list"})
		}
		return s.handleGraphPatterns(ctx, req)
	case "template", "templates", "template_patterns":
		return s.handleTemplatePatterns(ctx, req)
	default: // "code"
		return s.handlePatterns(ctx, req)
	}
}

// handleAnalysisFindings routes grafel_findings by action= over the findings
// store. Default action=list (read-only enumeration).
//
//	list (default) → handleListFindings (enumerate stored findings)
//	save           → handleSaveResult   (persist a Q&A finding)
func (s *Server) handleAnalysisFindings(ctx context.Context, req mcpapi.CallToolRequest) (*mcpapi.CallToolResult, error) {
	switch argString(req, "action", "list") {
	case "save", "store", "persist":
		return s.handleSaveResult(ctx, req)
	default: // "list"
		return s.handleListFindings(ctx, req)
	}
}

// handleAnalysisDiff routes grafel_diff by aspect= over the comparison
// handlers. Default aspect=response_shape.
//
// The members do NOT share one return shape — this is a DISCRIMINATED UNION
// keyed by `aspect`: refs returns entity/relationship deltas;
// response_shape/auth/literals return per-endpoint parity verdicts; payload
// returns drift findings. The dispatcher stamps the chosen `aspect` onto the
// JSON-object result (via stampAspect) so the caller can tell which shape it
// received without forcing a common schema. Error/non-object results pass
// through unchanged.
//
//	response_shape (default) → handleResponseShapeDiff (per-status field drift)
//	payload                  → handlePayloadDrift      (schema/envelope drift)
//	auth                     → handleAuthPostureDiff   (auth-posture parity)
//	literals                 → handleLiteralParity     (ConstantSet/enum parity)
//	refs                     → handleDiffRefs          (entity/rel deltas, 2 refs)
func (s *Server) handleAnalysisDiff(ctx context.Context, req mcpapi.CallToolRequest) (*mcpapi.CallToolResult, error) {
	var (
		res    *mcpapi.CallToolResult
		err    error
		aspect string
	)
	switch argString(req, "aspect", "response_shape") {
	case "payload", "payload_drift":
		aspect = "payload"
		res, err = s.handlePayloadDrift(ctx, req)
	case "auth", "auth_posture":
		aspect = "auth"
		res, err = s.handleAuthPostureDiff(ctx, req)
	case "literals", "literal", "literal_parity":
		aspect = "literals"
		res, err = s.handleLiteralParity(ctx, req)
	case "refs", "diff_refs":
		aspect = "refs"
		res, err = s.handleDiffRefs(ctx, req)
	default: // "response_shape"
		aspect = "response_shape"
		res, err = s.handleResponseShapeDiff(ctx, req)
	}
	if err != nil {
		return res, err
	}
	return stampAspect(res, aspect), nil
}

// stampAspect injects "aspect":<value> as a top-level key into a JSON-object
// tool result so grafel_diff's discriminated union is self-describing. If the
// result is not a JSON object (an error result, a JSON array, or plain text) it
// is returned unchanged — the dispatch must never corrupt an absorbed handler's
// payload.
func stampAspect(res *mcpapi.CallToolResult, aspect string) *mcpapi.CallToolResult {
	if res == nil || res.IsError {
		return res
	}
	for i, c := range res.Content {
		tc, ok := c.(mcpapi.TextContent)
		if !ok {
			continue
		}
		var obj map[string]json.RawMessage
		if err := json.Unmarshal([]byte(tc.Text), &obj); err != nil {
			return res // not a JSON object — leave verbatim
		}
		obj["aspect"] = json.RawMessage(`"` + aspect + `"`)
		out, err := json.Marshal(obj)
		if err != nil {
			return res
		}
		tc.Text = string(out)
		res.Content[i] = tc
		return res
	}
	return res
}
