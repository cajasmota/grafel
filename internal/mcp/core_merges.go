package mcp

import (
	"context"

	mcpapi "github.com/mark3labs/mcp-go/mcp"
)

// core_merges.go — CORE-cluster canonical tool dispatchers (epic #5546, #5549).
//
// Milestone 0.1.5 consolidates ~68 MCP tools into ~22 intent-named tools, each
// genuinely-similar group collapsed under one verb with a discriminator param.
// This file implements the CORE cluster: the everyday interactive surface.
//
// Mechanism: each canonical tool's handler reads a single discriminator
// argument (view / search / direction / mode / kind / detail / scope) and
// dispatches to the EXISTING handler funcs unchanged — behaviour is
// byte-identical to the absorbed tools. No analytical logic lives here; this is
// pure routing. The absorbed tools stay registered as standalone tools for
// back-compat until #5552 converts them to hidden aliases.
//
// Where a discriminator's name collides with a param the underlying handler
// itself reads (e.g. grafel_related's `direction` vs handleNeighbors's own
// `direction`), reqWithArgs clones the request and rewrites the inner arg so
// the delegate sees exactly what it expects.

// reqWithArgs returns a shallow clone of req whose argument map is the original
// args plus the given overrides. A nil override value deletes that key. The
// original request is never mutated (handlers may run concurrently).
func reqWithArgs(req mcpapi.CallToolRequest, overrides map[string]any) mcpapi.CallToolRequest {
	src := req.GetArguments()
	merged := make(map[string]any, len(src)+len(overrides))
	for k, v := range src {
		merged[k] = v
	}
	for k, v := range overrides {
		if v == nil {
			delete(merged, k)
			continue
		}
		merged[k] = v
	}
	out := req
	out.Params.Arguments = merged
	return out
}

// handleCoreOrient routes grafel_orient by view= over the orientation handlers.
//
//	overview (default) → handleOrient        (key entities, cross-cutting edges)
//	me                 → handleWhoami        (resolve group/repo/ref from cwd)
//	clusters           → handleListCommunities (Louvain communities)
//	topology           → handleTopology      (message-channel topology)
//	modules            → handleModuleAnalysis (module SCC/PageRank/betweenness)
//	stats              → handleGraphStats     (corpus-level metrics)
func (s *Server) handleCoreOrient(ctx context.Context, req mcpapi.CallToolRequest) (*mcpapi.CallToolResult, error) {
	switch argString(req, "view", "overview") {
	case "me", "whoami":
		return s.handleWhoami(ctx, req)
	case "clusters", "communities":
		return s.handleListCommunities(ctx, req)
	case "topology":
		// handleTopology requires action=; default to orphan publishers scan
		// when the caller did not pass one through the orient surface.
		if argString(req, "action", "") == "" {
			req = reqWithArgs(req, map[string]any{"action": "orphan_publishers"})
		}
		return s.handleTopology(ctx, req)
	case "modules", "module_analysis":
		return s.handleModuleAnalysis(ctx, req)
	case "stats":
		return s.handleGraphStats(ctx, req)
	case "overview", "":
		return s.handleOrient(ctx, req)
	default:
		return s.handleOrient(ctx, req)
	}
}

// handleCoreFind routes grafel_find by search= between the BM25 graph query and
// the substring entity search.
//
//	bm25 (default) → handleQueryGraph    (semantic "where is X?" BM25 ranking)
//	substring      → handleSearchEntities (literal substring over entity names)
func (s *Server) handleCoreFind(ctx context.Context, req mcpapi.CallToolRequest) (*mcpapi.CallToolResult, error) {
	switch argString(req, "search", "bm25") {
	case "substring", "literal", "name":
		return s.handleSearchEntities(ctx, req)
	default:
		return s.handleQueryGraph(ctx, req)
	}
}

// handleCoreRelated routes grafel_related by direction= over the
// neighbour/caller/callee/navigation handlers. Default direction=callers — the
// hot "who calls this?" case.
//
//	callers (default) → handleFindCallers   (inbound callers)
//	callees           → handleFindCallees   (outbound callees)
//	neighbors         → handleNeighbors(direction=both)
//	uses              → handleNavigates(direction=outgoing)  (NAVIGATES_TO out)
//	used_by           → handleNavigates(direction=incoming)  (NAVIGATES_TO in)
func (s *Server) handleCoreRelated(ctx context.Context, req mcpapi.CallToolRequest) (*mcpapi.CallToolResult, error) {
	switch argString(req, "direction", "callers") {
	case "callees":
		return s.handleFindCallees(ctx, req)
	case "neighbors", "both":
		// handleNeighbors reads its OWN `direction` (in|out|both); the outer
		// discriminator value "neighbors" is not a valid inner value, so rewrite.
		return s.handleNeighbors(ctx, reqWithArgs(req, map[string]any{"direction": "both"}))
	case "uses":
		return s.handleNavigates(ctx, reqWithArgs(req, map[string]any{"direction": "outgoing"}))
	case "used_by":
		return s.handleNavigates(ctx, reqWithArgs(req, map[string]any{"direction": "incoming"}))
	default: // "callers"
		return s.handleFindCallers(ctx, req)
	}
}

// handleCoreSubgraph routes grafel_subgraph by mode=. Default mode=hops keeps
// the existing N-hop subgraph; mode=expand absorbs the old grafel_expand
// (immediate neighbours of one entity, both directions).
//
//	hops (default) → handleSubgraph     (nodes+edges within N hops)
//	expand         → handleGetNeighbors (immediate neighbours, both directions)
func (s *Server) handleCoreSubgraph(ctx context.Context, req mcpapi.CallToolRequest) (*mcpapi.CallToolResult, error) {
	switch argString(req, "mode", "hops") {
	case "expand", "neighbors":
		return s.handleGetNeighbors(ctx, req)
	default:
		return s.handleSubgraph(ctx, req)
	}
}

// handleCoreTrace routes grafel_trace by kind= over the flow/path handlers.
// When kind is omitted it preserves the historical grafel_trace behaviour
// (confidence-weighted shortest path between source/target).
//
//	path (default) → handleShortestPath (shortest path source→target)
//	data           → handleDataFlows    (request-input→sink DATA_FLOWS_TO)
//	control        → handleControlFlow  (per-function CFG + complexity)
//	def_use        → handleDefUse       (intra-procedural def-use chains)
//	effects        → handleEffects      (db/http/fs/mutation effects + sinks)
//	flows          → handleFlows        (process-flow diagnostics)
//	process        → handleTraces       (process-flow traces list/get/follow)
func (s *Server) handleCoreTrace(ctx context.Context, req mcpapi.CallToolRequest) (*mcpapi.CallToolResult, error) {
	switch argString(req, "kind", "path") {
	case "data", "data_flows":
		return s.handleDataFlows(ctx, req)
	case "control", "control_flow":
		return s.handleControlFlow(ctx, req)
	case "def_use", "defuse":
		return s.handleDefUse(ctx, req)
	case "effects":
		return s.handleEffects(ctx, req)
	case "flows":
		// handleFlows requires action=; default to dead_ends scan.
		if argString(req, "action", "") == "" {
			req = reqWithArgs(req, map[string]any{"action": "dead_ends"})
		}
		return s.handleFlows(ctx, req)
	case "process", "traces":
		return s.handleTraces(ctx, req)
	default: // "path"
		return s.handleShortestPath(ctx, req)
	}
}

// handleCoreEndpoints routes grafel_endpoints by detail=. Default detail=list
// preserves the existing HTTP-endpoint listing (which takes its own action=).
//
//	list (default) → handleEndpoints          (definitions|calls|stats)
//	contract       → handleEffectiveContract  (per-verb effective contract)
//	posture        → handleEndpointPosture    (auth/rate_limit/throws/flags)
func (s *Server) handleCoreEndpoints(ctx context.Context, req mcpapi.CallToolRequest) (*mcpapi.CallToolResult, error) {
	switch argString(req, "detail", "list") {
	case "contract":
		return s.handleEffectiveContract(ctx, req)
	case "posture":
		return s.handleEndpointPosture(ctx, req)
	default: // "list"
		// handleEndpoints requires action=; default to definitions when the
		// caller routed in via detail= without an explicit action.
		if argString(req, "action", "") == "" {
			req = reqWithArgs(req, map[string]any{"action": "definitions"})
		}
		return s.handleEndpoints(ctx, req)
	}
}

// handleCoreImpactRadius routes grafel_impact_radius by scope=.
//
//	entity (default) → handleImpactRadius (inbound blast-radius of one entity)
//	changeset        → handlePRImpact     (PR/diff impact + merge-risk)
func (s *Server) handleCoreImpactRadius(ctx context.Context, req mcpapi.CallToolRequest) (*mcpapi.CallToolResult, error) {
	switch argString(req, "scope", "entity") {
	case "changeset", "pr", "diff":
		return s.handlePRImpact(ctx, req)
	default: // "entity"
		return s.handleImpactRadius(ctx, req)
	}
}
