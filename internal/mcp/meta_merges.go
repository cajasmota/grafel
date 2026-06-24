package mcp

import (
	"context"

	mcpapi "github.com/mark3labs/mcp-go/mcp"
)

// meta_merges.go — WORKFLOW + META-cluster canonical tool dispatchers (epic
// #5546, #5551).
//
// Third of the 0.1.5 MCP-consolidation clusters (CORE lives in core_merges.go
// #5549, ANALYSIS in analysis_merges.go #5550). Each canonical tool reads a
// single discriminator argument (action / kind) and dispatches to the EXISTING
// handler funcs unchanged — behaviour is byte-identical to the absorbed tools.
// No logic lives here; this is pure routing. The absorbed tools stay registered
// as standalone tools for back-compat until #5552 converts them to hidden
// aliases.
//
// reqWithArgs (defined in core_merges.go) is reused where a discriminator's
// value must be rewritten into an inner argument the delegate reads itself.

// handleWorkflowDocgen routes grafel_docgen by action= over the six docgen
// staging-run lifecycle handlers. Default action=status — the read-only "what's
// in flight?" case (cheaper and side-effect-free than start).
//
//	start    → handleDocgenStartRun (create/resume staging run; needs group)
//	status   → handleDocgenStatus   (files written + per-file SHA; needs run_id)
//	list     → handleDocgenList     (enumerate canonical docs; needs group)
//	promote  → handleDocgenPromote  (atomic staging→canonical; needs run_id)
//	abort    → handleDocgenAbort    (rm -rf staging, release lock; needs run_id)
//	validate → handleDocgenValidate (frontmatter+cross-links, read-only)
func (s *Server) handleWorkflowDocgen(ctx context.Context, req mcpapi.CallToolRequest) (*mcpapi.CallToolResult, error) {
	if e := validateDiscriminator("action", argString(req, "action", ""),
		[]string{"start", "start_run", "status", "list", "promote", "abort", "validate"},
		[]string{"start", "status", "list", "promote", "abort", "validate"}); e != nil {
		return e, nil
	}
	switch argString(req, "action", "status") {
	case "start", "start_run":
		return s.handleDocgenStartRun(ctx, req)
	case "list":
		return s.handleDocgenList(ctx, req)
	case "promote":
		return s.handleDocgenPromote(ctx, req)
	case "abort":
		return s.handleDocgenAbort(ctx, req)
	case "validate":
		return s.handleDocgenValidate(ctx, req)
	default: // "status"
		return s.handleDocgenStatus(ctx, req)
	}
}

// handleWorkflowDocgenApply routes grafel_docgen_apply by kind= over the
// doc-enrichment / repair apply handlers. Default kind=semantics (Layer-2
// DesignDecision apply step).
//
//	semantics (default) → handleApplyDocSemantics (L2 DesignDecision + RATIONALE_FOR)
//	repairs             → handleApplyDocgenRepairs (docgen→graph repair apply)
//	                      OR handleRepairs when action= is present (residual
//	                      repair queue list/submit) — keeps both surfaces reachable.
//	enrichments         → handleEnrichments (enrichment-candidate queue;
//	                      reads its own action=list|submit|reject)
func (s *Server) handleWorkflowDocgenApply(ctx context.Context, req mcpapi.CallToolRequest) (*mcpapi.CallToolResult, error) {
	if e := validateDiscriminator("kind", argString(req, "kind", ""),
		[]string{"semantics", "repairs", "repair", "enrichments", "enrichment"},
		[]string{"semantics", "repairs", "enrichments"}); e != nil {
		return e, nil
	}
	switch argString(req, "kind", "semantics") {
	case "repairs", "repair":
		// The residual-repair queue (handleRepairs) is driven by action=list|
		// submit; the docgen→graph apply step (handleApplyDocgenRepairs) takes
		// no action. Route on the presence of action so neither is lost.
		if argString(req, "action", "") != "" {
			return s.handleRepairs(ctx, req)
		}
		return s.handleApplyDocgenRepairs(ctx, req)
	case "enrichments", "enrichment":
		return s.handleEnrichments(ctx, req)
	default: // "semantics"
		return s.handleApplyDocSemantics(ctx, req)
	}
}

// handleMetaEvent routes grafel_event by kind= over the local-only telemetry
// handlers. Default kind=feedback (agent-experience feedback).
//
//	feedback (default) → handleFeedbackEvent (agent-experience; needs outcome)
//	persona            → handlePersonaEvent  (persona lifecycle; needs persona+event_type)
func (s *Server) handleMetaEvent(ctx context.Context, req mcpapi.CallToolRequest) (*mcpapi.CallToolResult, error) {
	if e := validateDiscriminator("kind", argString(req, "kind", ""),
		[]string{"feedback", "persona"},
		[]string{"feedback", "persona"}); e != nil {
		return e, nil
	}
	switch argString(req, "kind", "feedback") {
	case "persona":
		return s.handlePersonaEvent(ctx, req)
	default: // "feedback"
		return s.handleFeedbackEvent(ctx, req)
	}
}
