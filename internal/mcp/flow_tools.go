// flow_tools.go — MCP handlers for flow-aware graph traversal tools (issue #1252).
//
// Implements:
//   - archigraph_find_callers       — what calls this entity (inbound edges, N hops)
//   - archigraph_find_callees       — what does this entity call (outbound edges, N hops)
//   - archigraph_impact_radius      — entities affected if this one changes, with risk score
//   - archigraph_subgraph           — unified subgraph tool (format=raw|markdown) (#1754)
//   - archigraph_find_dead_code     — unreferenced public operations carrying a dead-code marker
//
// All handlers operate against the in-memory LoadedGroup data — no HTTP calls.
package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/cajasmota/archigraph/internal/graph"
	mcpapi "github.com/mark3labs/mcp-go/mcp"
)

// ---------------------------------------------------------------------------
// archigraph_neighbors (#1753) — unified callers + callees + both.
// ---------------------------------------------------------------------------

// handleNeighbors is the unified neighbors tool that subsumes find_callers and
// find_callees behind a `direction` discriminator. It defers to the existing
// handlers for direction=in / direction=out to preserve byte-for-byte response
// shape, and merges the two when direction=both.
func (s *Server) handleNeighbors(ctx context.Context, req mcpapi.CallToolRequest) (*mcpapi.CallToolResult, error) {
	dir := strings.ToLower(strings.TrimSpace(argString(req, "direction", "both")))
	switch dir {
	case "in", "inbound", "callers":
		return s.handleFindCallers(ctx, req)
	case "out", "outbound", "callees":
		return s.handleFindCallees(ctx, req)
	case "", "both", "all":
		// Run both and merge. We pull the JSON envelopes back as maps so the
		// merged response carries entity_id/entity_name/repo + counts for each
		// direction independently. Token budget is shared evenly.
		inRes, _ := s.handleFindCallers(ctx, req)
		outRes, _ := s.handleFindCallees(ctx, req)
		return mergeNeighbors(inRes, outRes), nil
	default:
		return mcpapi.NewToolResultError("invalid direction: " + dir + " (want in|out|both)"), nil
	}
}

// mergeNeighbors combines callers + callees JSON envelopes into one record.
// On any parse failure it returns whichever input is non-nil first.
func mergeNeighbors(inRes, outRes *mcpapi.CallToolResult) *mcpapi.CallToolResult {
	parse := func(res *mcpapi.CallToolResult) map[string]any {
		if res == nil || len(res.Content) == 0 {
			return nil
		}
		tc, ok := res.Content[0].(mcpapi.TextContent)
		if !ok {
			return nil
		}
		var obj map[string]any
		if err := json.Unmarshal([]byte(tc.Text), &obj); err != nil {
			return nil
		}
		return obj
	}
	in := parse(inRes)
	out := parse(outRes)
	if in == nil && out == nil {
		if inRes != nil {
			return inRes
		}
		return outRes
	}
	merged := map[string]any{}
	pick := in
	if pick == nil {
		pick = out
	}
	for _, k := range []string{"entity_id", "entity_name", "repo", "depth"} {
		if v, ok := pick[k]; ok {
			merged[k] = v
		}
	}
	if in != nil {
		if v, ok := in["callers"]; ok {
			merged["callers"] = v
		}
		if v, ok := in["truncation_note"]; ok {
			merged["callers_truncation_note"] = v
		}
	}
	if out != nil {
		if v, ok := out["callees"]; ok {
			merged["callees"] = v
		}
		if v, ok := out["truncation_note"]; ok {
			merged["callees_truncation_note"] = v
		}
	}
	merged["direction"] = "both"
	return jsonResult(merged)
}

// ---------------------------------------------------------------------------
// archigraph_find_callers
// ---------------------------------------------------------------------------

// handleFindCallers returns entities that call (directly or transitively) the
// given entity. It walks the inbound adjacency up to `depth` hops and returns
// results grouped by hop distance so the agent can see the call fan-in at each
// level.
func (s *Server) handleFindCallers(_ context.Context, req mcpapi.CallToolRequest) (*mcpapi.CallToolResult, error) {
	entityID, err := req.RequireString("entity_id")
	if err != nil {
		return mcpapi.NewToolResultError(err.Error()), nil
	}
	_, lg, errRes := s.resolveAndGroup(req)
	if errRes != nil {
		return errRes, nil
	}
	depth := argInt(req, "depth", 1)
	if depth < 1 {
		depth = 1
	}
	if depth > 5 {
		depth = 5
	}
	verbose := argBool(req, "verbose", false)

	repoHint, local := splitPrefixed(entityID)
	repos := reposToConsider(lg, nil)
	if repoHint != "" {
		if r, ok := lg.Repos[repoHint]; ok && r.Doc != nil {
			repos = []*LoadedRepo{r}
		}
	}

	// Default shape: id, name, file, line, hop_count.
	// Verbose shape: also includes kind, repo.
	type caller struct {
		EntityID   string `json:"id"`
		Name       string `json:"name"`
		Kind       string `json:"kind,omitempty"`
		Repo       string `json:"repo,omitempty"`
		SourceFile string `json:"file,omitempty"`
		StartLine  int    `json:"line,omitempty"`
		HopCount   int    `json:"hop_count"`
	}

	for _, r := range repos {
		if r.Doc == nil {
			continue
		}
		target := local
		if target == "" {
			target = entityID
		}
		byID := r.ByID
		if _, ok := byID[target]; !ok {
			continue
		}

		// BFS over inbound-only adjacency, restricted to reference kinds
		// (CALLS, REFERENCES, TESTS, ROUTES_TO, etc.). CONTAINS is excluded:
		// a module/file that CONTAINS an entity is not a caller. Without this
		// filter find_callers was returning CONTAINS-linked parent nodes and
		// other structural edges as fake "callers" (#1915).
		//
		// #2039: track the edge kind that *discovered* each node. When the
		// discovering edge is REFERENCES or IMPORTS, a file/module CONTAINER
		// source is a legitimate caller — post-#2020 file entities own these
		// edges (e.g. `core/admin.py` REFERENCES Models, `views.py` IMPORTS
		// HasPermission, `__init__.py` IMPORTS a re-exported module). The
		// noiseContainer filter below must NOT drop those.
		adj := r.Adjacency
		visited := map[string]int{target: 0}
		// discoveredVia[id] = edge kind via which `id` was first reached on
		// the inbound BFS. Used below to decide whether to allow file/module
		// container sources through the noise filter.
		discoveredVia := map[string]string{}
		frontier := []string{target}
		for d := 0; d < depth; d++ {
			next := []string{}
			for _, n := range frontier {
				for _, e := range adj.in[n] {
					if !inboundRefKinds[e.kind] {
						continue
					}
					if _, seen := visited[e.target]; seen {
						continue
					}
					visited[e.target] = d + 1
					discoveredVia[e.target] = e.kind
					next = append(next, e.target)
				}
			}
			frontier = next
			if len(frontier) == 0 {
				break
			}
		}

		callers := []caller{}
		for id, d := range visited {
			if id == target {
				continue
			}
			e := byID[id]
			if e == nil {
				continue
			}
			// #1614: drop file/module CONTAINER components and inferred
			// shadows. Callers should be operation/component-level referencers,
			// not the synthetic file node that "contains" the call. (q02/q03/q10)
			//
			// #2039: exception — when the discovering inbound edge is
			// REFERENCES or IMPORTS, a file/module container IS the legitimate
			// caller (file-level reference / import edges live on the file
			// entity post-#2020). Keep noiseShadow filtered unconditionally.
			switch classifyNoise(e) {
			case noiseShadow:
				continue
			case noiseContainer:
				dk := discoveredVia[id]
				if dk != "REFERENCES" && dk != "IMPORTS" {
					continue
				}
			}
			c := caller{
				EntityID:   prefixedID(r.Repo, e.ID),
				Name:       e.Name,
				SourceFile: e.SourceFile,
				StartLine:  e.StartLine,
				HopCount:   d,
			}
			if verbose {
				c.Kind = stripScopePrefix(e.Kind)
				c.Repo = r.Repo
			}
			callers = append(callers, c)
		}
		sort.Slice(callers, func(i, j int) bool {
			if callers[i].HopCount != callers[j].HopCount {
				return callers[i].HopCount < callers[j].HopCount
			}
			return callers[i].Name < callers[j].Name
		})

		root := byID[target]
		rootName := target
		if root != nil {
			rootName = root.Name
		}

		// #1738: token-budget cap — shed callers from tail until under budget.
		tokenBudget := argInt(req, "token_budget", 800)
		if tokenBudget < 100 {
			tokenBudget = 100
		}
		budgetBytes := tokenBudget * 4
		if budgetBytes > 64*1024 {
			budgetBytes = 64 * 1024
		}
		preCapLen := len(callers)
		callers = capByRenderedBytes(callers, budgetBytes, false)

		// #1618: distinguish "entity found, zero callers" from "entity not found".
		// An empty callers array with result="no_incoming_edges" is an explicit
		// graph signal — agents MUST NOT infer a plausible relationship to fill
		// the gap. Report the absence verbatim.
		result := map[string]any{
			"entity_id":   prefixedID(r.Repo, target),
			"entity_name": rootName,
			"repo":        r.Repo,
			"depth":       depth,
			"callers":     callers,
			"count":       len(callers),
		}
		if preCapLen > len(callers) {
			result["truncation_note"] = fmt.Sprintf(
				"response capped at token_budget=%d (~%d bytes); %d callers omitted — pass a larger token_budget or reduce depth",
				tokenBudget, budgetBytes, preCapLen-len(callers),
			)
		}
		if len(callers) == 0 && preCapLen == 0 {
			result["result"] = "no_incoming_edges"
			result["note"] = "Graph shows no callers for this entity within the requested depth. Do not infer a relationship — report the absence."
		}
		return jsonResult(result), nil
	}
	return mcpapi.NewToolResultError("entity not found: " + entityID), nil
}

// ---------------------------------------------------------------------------
// archigraph_find_callees
// ---------------------------------------------------------------------------

// handleFindCallees returns entities called by the given entity. It walks the
// outbound adjacency up to `depth` hops, returning results grouped by hop
// distance so the agent sees the call fan-out at each level.
func (s *Server) handleFindCallees(_ context.Context, req mcpapi.CallToolRequest) (*mcpapi.CallToolResult, error) {
	entityID, err := req.RequireString("entity_id")
	if err != nil {
		return mcpapi.NewToolResultError(err.Error()), nil
	}
	_, lg, errRes := s.resolveAndGroup(req)
	if errRes != nil {
		return errRes, nil
	}
	depth := argInt(req, "depth", 1)
	if depth < 1 {
		depth = 1
	}
	if depth > 5 {
		depth = 5
	}
	verbose := argBool(req, "verbose", false)

	repoHint, local := splitPrefixed(entityID)
	repos := reposToConsider(lg, nil)
	if repoHint != "" {
		if r, ok := lg.Repos[repoHint]; ok && r.Doc != nil {
			repos = []*LoadedRepo{r}
		}
	}

	// Default shape: id, name, file, line, hop_count.
	// Verbose shape: also includes kind, repo.
	type callee struct {
		EntityID   string `json:"id"`
		Name       string `json:"name"`
		Kind       string `json:"kind,omitempty"`
		Repo       string `json:"repo,omitempty"`
		SourceFile string `json:"file,omitempty"`
		StartLine  int    `json:"line,omitempty"`
		HopCount   int    `json:"hop_count"`
	}

	for _, r := range repos {
		if r.Doc == nil {
			continue
		}
		target := local
		if target == "" {
			target = entityID
		}
		byID := r.ByID
		if _, ok := byID[target]; !ok {
			continue
		}

		// BFS over outbound-only adjacency.
		adj := r.Adjacency
		visited := map[string]int{target: 0}
		frontier := []string{target}
		for d := 0; d < depth; d++ {
			next := []string{}
			for _, n := range frontier {
				for _, e := range adj.out[n] {
					if _, seen := visited[e.target]; seen {
						continue
					}
					visited[e.target] = d + 1
					next = append(next, e.target)
				}
			}
			frontier = next
			if len(frontier) == 0 {
				break
			}
		}

		callees := []callee{}
		for id, d := range visited {
			if id == target {
				continue
			}
			e := byID[id]
			if e == nil {
				continue
			}
			c := callee{
				EntityID:   prefixedID(r.Repo, e.ID),
				Name:       e.Name,
				SourceFile: e.SourceFile,
				StartLine:  e.StartLine,
				HopCount:   d,
			}
			if verbose {
				c.Kind = stripScopePrefix(e.Kind)
				c.Repo = r.Repo
			}
			callees = append(callees, c)
		}
		sort.Slice(callees, func(i, j int) bool {
			if callees[i].HopCount != callees[j].HopCount {
				return callees[i].HopCount < callees[j].HopCount
			}
			return callees[i].Name < callees[j].Name
		})

		root := byID[target]
		rootName := target
		if root != nil {
			rootName = root.Name
		}

		// #1738: token-budget cap — shed callees from tail until under budget.
		tokenBudget := argInt(req, "token_budget", 800)
		if tokenBudget < 100 {
			tokenBudget = 100
		}
		budgetBytes := tokenBudget * 4
		if budgetBytes > 64*1024 {
			budgetBytes = 64 * 1024
		}
		preCapLen := len(callees)
		callees = capByRenderedBytes(callees, budgetBytes, false)

		// #1618: distinguish "entity found, zero callees" from "entity not found".
		// An empty callees array with result="no_outgoing_edges" is an explicit
		// graph signal — agents MUST NOT infer a plausible relationship to fill
		// the gap. Report the absence verbatim.
		result := map[string]any{
			"entity_id":   prefixedID(r.Repo, target),
			"entity_name": rootName,
			"repo":        r.Repo,
			"depth":       depth,
			"callees":     callees,
			"count":       len(callees),
		}
		if preCapLen > len(callees) {
			result["truncation_note"] = fmt.Sprintf(
				"response capped at token_budget=%d (~%d bytes); %d callees omitted — pass a larger token_budget or reduce depth",
				tokenBudget, budgetBytes, preCapLen-len(callees),
			)
		}
		if len(callees) == 0 && preCapLen == 0 {
			result["result"] = "no_outgoing_edges"
			result["note"] = "Graph shows no callees for this entity. Do not infer a relationship — report the absence."
		}
		return jsonResult(result), nil
	}
	return mcpapi.NewToolResultError("entity not found: " + entityID), nil
}

// ---------------------------------------------------------------------------
// archigraph_impact_radius
// ---------------------------------------------------------------------------

// impactRiskScore computes a heuristic risk score [0.0, 1.0] for an affected
// entity. Higher means "more risky to touch". Factors:
//   - in-degree (more callers → higher blast radius if it breaks)
//   - is the entity a public API endpoint or topic publisher
//   - lack of test coverage indicator (entity has "test_coverage" property)
func impactRiskScore(e *graph.Entity, inDegree int) float64 {
	score := 0.0

	// In-degree contribution: log-scale, max contribution 0.5.
	if inDegree > 0 {
		// ln(inDegree+1)/ln(51) caps at 1.0 for inDegree=50, then clamp at 0.5.
		contrib := 0.0
		for n := inDegree + 1; n > 1; n /= 2 {
			contrib += 0.1
		}
		if contrib > 0.5 {
			contrib = 0.5
		}
		score += contrib
	}

	// API boundary: endpoints and topics are higher risk.
	k := strings.ToLower(e.Kind)
	if strings.Contains(k, "http_endpoint") || strings.Contains(k, "endpoint") ||
		strings.Contains(k, "topic") || strings.Contains(k, "queue") {
		score += 0.25
	}

	// No test coverage: increase risk.
	cov := e.Properties["test_coverage"]
	if cov == "" || cov == "0" || cov == "none" {
		score += 0.25
	}

	if score > 1.0 {
		score = 1.0
	}
	return score
}

// handleImpactRadius returns all entities that would be affected if the given
// entity changes — a "change blast radius" analysis. Each result carries a
// risk_score [0,1] indicating how dangerous that particular affected entity
// is. Results are sorted by risk_score descending so agents can prioritise.
func (s *Server) handleImpactRadius(_ context.Context, req mcpapi.CallToolRequest) (*mcpapi.CallToolResult, error) {
	entityID, err := req.RequireString("entity_id")
	if err != nil {
		return mcpapi.NewToolResultError(err.Error()), nil
	}
	_, lg, errRes := s.resolveAndGroup(req)
	if errRes != nil {
		return errRes, nil
	}
	hops := argInt(req, "hops", 2)
	if hops < 1 {
		hops = 1
	}
	if hops > 6 {
		hops = 6
	}

	repoHint, local := splitPrefixed(entityID)
	repos := reposToConsider(lg, nil)
	if repoHint != "" {
		if r, ok := lg.Repos[repoHint]; ok && r.Doc != nil {
			repos = []*LoadedRepo{r}
		}
	}

	type affected struct {
		EntityID   string  `json:"entity_id"`
		Name       string  `json:"name"`
		Kind       string  `json:"kind"`
		Repo       string  `json:"repo"`
		SourceFile string  `json:"source_file,omitempty"`
		HopCount   int     `json:"hop_count"`
		RiskScore  float64 `json:"risk_score"`
		RiskReason string  `json:"risk_reason,omitempty"`
	}

	for _, r := range repos {
		if r.Doc == nil {
			continue
		}
		target := local
		if target == "" {
			target = entityID
		}
		byID := r.ByID
		if _, ok := byID[target]; !ok {
			continue
		}

		// Precompute in-degree for risk scoring.
		inDegreeMap := map[string]int{}
		for i := range r.Doc.Relationships {
			rel := &r.Doc.Relationships[i]
			inDegreeMap[rel.ToID]++
		}

		// Impact radius = entities that transitively depend on `target`.
		// We walk the INBOUND graph from target: callers of callers.
		adj := r.Adjacency
		visited := map[string]int{target: 0}
		frontier := []string{target}
		for d := 0; d < hops; d++ {
			next := []string{}
			for _, n := range frontier {
				for _, e := range adj.in[n] {
					if _, seen := visited[e.target]; seen {
						continue
					}
					visited[e.target] = d + 1
					next = append(next, e.target)
				}
			}
			frontier = next
			if len(frontier) == 0 {
				break
			}
		}

		results := []affected{}
		for id, d := range visited {
			if id == target {
				continue
			}
			e := byID[id]
			if e == nil {
				continue
			}
			risk := impactRiskScore(e, inDegreeMap[id])
			reason := buildRiskReason(e, inDegreeMap[id])
			results = append(results, affected{
				EntityID:   prefixedID(r.Repo, e.ID),
				Name:       e.Name,
				Kind:       stripScopePrefix(e.Kind),
				Repo:       r.Repo,
				SourceFile: e.SourceFile,
				HopCount:   d,
				RiskScore:  risk,
				RiskReason: reason,
			})
		}
		// Sort by risk descending, then hop ascending, then name.
		sort.Slice(results, func(i, j int) bool {
			if results[i].RiskScore != results[j].RiskScore {
				return results[i].RiskScore > results[j].RiskScore
			}
			if results[i].HopCount != results[j].HopCount {
				return results[i].HopCount < results[j].HopCount
			}
			return results[i].Name < results[j].Name
		})

		root := byID[target]
		rootName := target
		if root != nil {
			rootName = root.Name
		}
		return jsonResult(map[string]any{
			"entity_id":   prefixedID(r.Repo, target),
			"entity_name": rootName,
			"repo":        r.Repo,
			"hops":        hops,
			"affected":    results,
			"count":       len(results),
			"tip":         "risk_score 0.0–1.0: higher means the affected entity is more sensitive to breakage from changes in the root entity.",
		}), nil
	}
	return mcpapi.NewToolResultError("entity not found: " + entityID), nil
}

// buildRiskReason produces a short human-readable reason string for the risk score.
func buildRiskReason(e *graph.Entity, inDegree int) string {
	parts := []string{}
	if inDegree > 5 {
		parts = append(parts, fmt.Sprintf("high in-degree (%d callers)", inDegree))
	}
	k := strings.ToLower(e.Kind)
	if strings.Contains(k, "http_endpoint") || strings.Contains(k, "endpoint") {
		parts = append(parts, "API boundary")
	} else if strings.Contains(k, "topic") || strings.Contains(k, "queue") {
		parts = append(parts, "message channel")
	}
	cov := e.Properties["test_coverage"]
	if cov == "" || cov == "0" || cov == "none" {
		parts = append(parts, "no test coverage")
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, "; ")
}

// ---------------------------------------------------------------------------
// archigraph_subgraph (unified, #1754)
// ---------------------------------------------------------------------------

// handleSubgraph is the unified handler for archigraph_subgraph.
// format="raw"      → JSON graph (nodes + edges), identical output to old get_subgraph.
// format="markdown" → LLM-friendly Markdown summary, identical output to old summarize_subgraph.
// Both legacy tools delegate here as deprecated trampolines.
func (s *Server) handleSubgraph(_ context.Context, req mcpapi.CallToolRequest) (*mcpapi.CallToolResult, error) {
	entityID, err := req.RequireString("entity_id")
	if err != nil {
		return mcpapi.NewToolResultError(err.Error()), nil
	}
	format := argString(req, "format", "raw")
	switch format {
	case "raw":
		return s.subgraphRaw(entityID, req)
	case "markdown":
		return s.subgraphMarkdown(entityID, req)
	default:
		return mcpapi.NewToolResultError("format must be \"raw\" or \"markdown\"; got: " + format), nil
	}
}

// subgraphRaw returns all nodes and edges within depth hops of entity_id.
// Extracted from the former handleGetSubgraph (dashboard_tools.go).
func (s *Server) subgraphRaw(entityID string, req mcpapi.CallToolRequest) (*mcpapi.CallToolResult, error) {
	_, lg, errRes := s.resolveAndGroup(req)
	if errRes != nil {
		return errRes, nil
	}
	depth := argInt(req, "depth", 2)
	if depth < 1 {
		depth = 1
	}
	if depth > 5 {
		depth = 5
	}
	repos := reposToConsider(lg, nil)
	repoHint, local := splitPrefixed(entityID)
	if repoHint != "" {
		if r, ok := lg.Repos[repoHint]; ok && r.Doc != nil {
			repos = []*LoadedRepo{r}
		}
	}

	for _, r := range repos {
		if r.Doc == nil {
			continue
		}
		target := local
		if target == "" {
			target = entityID
		}
		byID := r.ByID
		if _, ok := byID[target]; !ok {
			continue
		}
		adj := r.Adjacency
		visited := bfs(adj, target, depth, nil)
		byID2 := r.ByID
		type nodeOut struct {
			EntityID   string `json:"entity_id"`
			Name       string `json:"name"`
			Kind       string `json:"kind"`
			SourceFile string `json:"source_file,omitempty"`
			StartLine  int    `json:"start_line,omitempty"`
			Depth      int    `json:"depth"`
		}
		type edgeOut struct {
			FromID string `json:"from_id"`
			ToID   string `json:"to_id"`
			Kind   string `json:"kind"`
		}
		var nodes []nodeOut
		nodeSet := map[string]bool{}
		for id, d := range visited {
			if e := byID2[id]; e != nil {
				nodes = append(nodes, nodeOut{
					EntityID:   prefixedID(r.Repo, e.ID),
					Name:       e.Name,
					Kind:       e.Kind,
					SourceFile: e.SourceFile,
					StartLine:  e.StartLine,
					Depth:      d,
				})
			}
			nodeSet[id] = true
		}
		sort.Slice(nodes, func(i, j int) bool {
			if nodes[i].Depth != nodes[j].Depth {
				return nodes[i].Depth < nodes[j].Depth
			}
			return nodes[i].Name < nodes[j].Name
		})
		var edges []edgeOut
		seen := map[string]bool{}
		for i := range r.Doc.Relationships {
			rel := &r.Doc.Relationships[i]
			if !nodeSet[rel.FromID] || !nodeSet[rel.ToID] {
				continue
			}
			key := rel.FromID + ">" + rel.ToID + ":" + rel.Kind
			if seen[key] {
				continue
			}
			seen[key] = true
			edges = append(edges, edgeOut{
				FromID: prefixedID(r.Repo, rel.FromID),
				ToID:   prefixedID(r.Repo, rel.ToID),
				Kind:   rel.Kind,
			})
		}
		return jsonResult(map[string]any{
			"root":       prefixedID(r.Repo, target),
			"repo":       r.Repo,
			"depth":      depth,
			"nodes":      nodes,
			"edges":      edges,
			"node_count": len(nodes),
			"edge_count": len(edges),
		}), nil
	}
	return mcpapi.NewToolResultError("entity not found: " + entityID), nil
}

// subgraphMarkdown returns an LLM-friendly Markdown summary of entity_id's
// local neighbourhood. Extracted from the former handleSummarizeSubgraph.
func (s *Server) subgraphMarkdown(entityID string, req mcpapi.CallToolRequest) (*mcpapi.CallToolResult, error) {
	_, lg, errRes := s.resolveAndGroup(req)
	if errRes != nil {
		return errRes, nil
	}
	depth := argInt(req, "depth", 2)
	if depth < 1 {
		depth = 1
	}
	if depth > 4 {
		depth = 4
	}

	repoHint, local := splitPrefixed(entityID)
	repos := reposToConsider(lg, nil)
	if repoHint != "" {
		if r, ok := lg.Repos[repoHint]; ok && r.Doc != nil {
			repos = []*LoadedRepo{r}
		}
	}

	for _, r := range repos {
		if r.Doc == nil {
			continue
		}
		target := local
		if target == "" {
			target = entityID
		}
		byID := r.ByID
		root, ok := byID[target]
		if !ok {
			continue
		}

		adj := r.Adjacency

		// Gather inbound callers (depth hops).
		inVisited := map[string]int{}
		inFront := []string{target}
		for d := 0; d < depth; d++ {
			next := []string{}
			for _, n := range inFront {
				for _, e := range adj.in[n] {
					if _, seen := inVisited[e.target]; !seen {
						inVisited[e.target] = d + 1
						next = append(next, e.target)
					}
				}
			}
			inFront = next
		}

		// Gather outbound callees (depth hops).
		outVisited := map[string]int{}
		outFront := []string{target}
		for d := 0; d < depth; d++ {
			next := []string{}
			for _, n := range outFront {
				for _, e := range adj.out[n] {
					if _, seen := outVisited[e.target]; !seen {
						outVisited[e.target] = d + 1
						next = append(next, e.target)
					}
				}
			}
			outFront = next
		}

		// Build callers list (sorted by hop then name).
		type neighbor struct {
			name string
			kind string
			file string
			hop  int
		}
		var callers []neighbor
		for id, d := range inVisited {
			if e := byID[id]; e != nil {
				callers = append(callers, neighbor{name: e.Name, kind: stripScopePrefix(e.Kind), file: e.SourceFile, hop: d})
			}
		}
		sort.Slice(callers, func(i, j int) bool {
			if callers[i].hop != callers[j].hop {
				return callers[i].hop < callers[j].hop
			}
			return callers[i].name < callers[j].name
		})

		var callees []neighbor
		for id, d := range outVisited {
			if e := byID[id]; e != nil {
				callees = append(callees, neighbor{name: e.Name, kind: stripScopePrefix(e.Kind), file: e.SourceFile, hop: d})
			}
		}
		sort.Slice(callees, func(i, j int) bool {
			if callees[i].hop != callees[j].hop {
				return callees[i].hop < callees[j].hop
			}
			return callees[i].name < callees[j].name
		})

		// Render markdown.
		var b strings.Builder
		b.WriteString(fmt.Sprintf("# %s\n\n", root.Name))
		b.WriteString(fmt.Sprintf("**Kind:** %s  \n", stripScopePrefix(root.Kind)))
		b.WriteString(fmt.Sprintf("**Repo:** %s  \n", r.Repo))
		if root.SourceFile != "" {
			b.WriteString(fmt.Sprintf("**File:** `%s`", root.SourceFile))
			if root.StartLine > 0 {
				b.WriteString(fmt.Sprintf(":%d", root.StartLine))
			}
			b.WriteString("  \n")
		}
		if root.QualifiedName != "" && root.QualifiedName != root.Name {
			b.WriteString(fmt.Sprintf("**Qualified name:** `%s`  \n", root.QualifiedName))
		}
		b.WriteString("\n")

		if len(callers) > 0 {
			b.WriteString(fmt.Sprintf("## Called by (%d entities within %d hop(s))\n\n", len(callers), depth))
			for _, c := range callers {
				b.WriteString(fmt.Sprintf("- **%s** (%s)", c.name, c.kind))
				if c.file != "" {
					b.WriteString(fmt.Sprintf(" — `%s`", c.file))
				}
				if c.hop > 1 {
					b.WriteString(fmt.Sprintf(" _(hop %d)_", c.hop))
				}
				b.WriteString("\n")
			}
			b.WriteString("\n")
		} else {
			b.WriteString("## Called by\n\n_No callers within the graph (entry point or unreferenced)._\n\n")
		}

		if len(callees) > 0 {
			b.WriteString(fmt.Sprintf("## Calls (%d entities within %d hop(s))\n\n", len(callees), depth))
			for _, c := range callees {
				b.WriteString(fmt.Sprintf("- **%s** (%s)", c.name, c.kind))
				if c.file != "" {
					b.WriteString(fmt.Sprintf(" — `%s`", c.file))
				}
				if c.hop > 1 {
					b.WriteString(fmt.Sprintf(" _(hop %d)_", c.hop))
				}
				b.WriteString("\n")
			}
			b.WriteString("\n")
		} else {
			b.WriteString("## Calls\n\n_No callees within the graph (leaf node or all edges unresolved)._\n\n")
		}

		return mcpapi.NewToolResultText(b.String()), nil
	}
	return mcpapi.NewToolResultError("entity not found: " + entityID), nil
}

// ---------------------------------------------------------------------------
// archigraph_find_dead_code
// ---------------------------------------------------------------------------

// stdlibKindPrefixes is the set of entity kind prefixes that represent
// stdlib/external references — we skip these when counting non-stdlib edges.
var stdlibKindPrefixes = []string{
	"stdlib", "external", "builtin", "vendor", "third_party", "foreign",
}

// isStdlibEntity returns true if the entity's kind or properties indicate it is
// a stdlib/external symbol (not project code).
func isStdlibEntity(e *graph.Entity) bool {
	k := strings.ToLower(e.Kind)
	for _, p := range stdlibKindPrefixes {
		if strings.HasPrefix(k, p) || strings.Contains(k, p) {
			return true
		}
	}
	if e.Properties["is_external"] == "true" ||
		e.Properties["is_stdlib"] == "true" ||
		e.Properties["external"] == "true" {
		return true
	}
	return false
}

// inboundRefKinds are the edge kinds that count as a real reference to an
// entity for dead-code purposes. An entity that is the target of any of these
// is "used" and cannot be dead. CONTAINS is intentionally excluded — every
// entity is contained by its module, so it carries no usage signal.
var inboundRefKinds = map[string]bool{
	"CALLS":           true,
	"REFERENCES":      true,
	"IMPORTS":         true, // #2039: file/module → symbol or re-exported module
	"TESTS":           true,
	"ROUTES_TO":       true,
	"IMPLEMENTS":      true,
	"HANDLES":         true,
	"ENTRY_POINT_OF":  true,
	"RENDERS":         true,
	"FETCHES":         true,
	"STEP_IN_PROCESS": true,
	"PRODUCES":        true,
	"CONSUMES":        true,
}

// deadMarkerRe matches conventional dead-code / deprecation name markers. A
// public symbol that is structurally orphaned AND carries one of these markers
// is high-confidence dead code (vs. a legitimate-but-currently-unused public
// API export, which we deliberately do NOT flag). Matching is case-insensitive
// against the symbol's leaf name.
var deadMarkerRe = regexp.MustCompile(`(?i)(legacy|deprecated|obsolete|dead|unused|_old$|^old[_A-Z])`)

// nonCodeLanguages are languages whose "operations" are config/markup
// directives, not real callable code (Dockerfile CMD/COPY, SQL DDL, etc.).
var nonCodeLanguages = map[string]bool{
	"dockerfile": true,
	"markdown":   true,
	"sql":        true,
	"hcl":        true,
	"yaml":       true,
	"toml":       true,
	"json":       true,
}

// routeNameRe matches synthetic HTTP-endpoint operation names like
// "GET /products/{sku}" or "http:POST:/charges". These are route handlers
// reachable via the web framework, never dead code.
var routeNameRe = regexp.MustCompile(`(?i)^(get|post|put|delete|patch|head|options|http)[\s:]`)

// frameworkLifecycleNames are method names that are invoked by a framework,
// runtime, or DI container rather than by an explicit in-graph call edge.
// Flagging these would be a false positive (they are entry points / hooks).
var frameworkLifecycleNames = map[string]bool{
	"main": true, "init": true, "__init__": true, "setup": true, "run": true,
	"bootstrap": true, "configure": true, "mount": true, "application": true,
	"project": true, "start": true, "startup": true, "shutdown": true,
	"connect": true, "render": true, "health": true, "show": true,
	"controller": true, "live_view": true, "channel": true, "socket": true,
	"up": true, "down": true, "use": true, "join": true, "leave": true,
	"new": true, "create": true, "build": true, "default": true,
	"config_change": true,
}

// isConstructorName reports whether name looks like a constructor (Java/C#/Go
// style "ClassName.ClassName") — instantiated reflectively / by the framework.
func isConstructorName(name string) bool {
	if !strings.Contains(name, ".") {
		return false
	}
	parts := strings.Split(name, ".")
	if len(parts) < 2 {
		return false
	}
	return parts[len(parts)-1] == parts[len(parts)-2]
}

// isFrameworkOrHandler reports whether an operation is a framework-managed
// entry point (route handler, lifecycle hook, event listener, constructor)
// that is reachable without an explicit in-graph call edge.
func isFrameworkOrHandler(e *graph.Entity) bool {
	name := e.Name
	leaf := name
	if i := strings.LastIndexByte(leaf, '.'); i >= 0 {
		leaf = leaf[i+1:]
	}
	low := strings.ToLower(leaf)
	if routeNameRe.MatchString(name) {
		return true
	}
	if frameworkLifecycleNames[low] {
		return true
	}
	if isConstructorName(name) {
		return true
	}
	// Event-listener / framework-callback naming conventions:
	// onOrderPlaced, handle_in, handle_event, handle_info, handleMessage…
	if strings.HasPrefix(low, "on") && len(low) > 2 && (low[2] >= 'a' && low[2] <= 'z' || low[2] == '_') {
		// crude: onX with capital next char in original
		if len(leaf) > 2 && leaf[2] >= 'A' && leaf[2] <= 'Z' {
			return true
		}
	}
	if strings.HasPrefix(low, "handle") {
		return true
	}
	// HTTP-endpoint kinds are routes, never dead.
	if isHTTPEndpointKind(e.Kind) {
		return true
	}
	return false
}

// buildImportedNameSet collects every symbol name that appears as an
// `imported_name` (or `local_name`) on an IMPORTS edge across all repos in the
// group. A library export whose name is imported somewhere is consumed and so
// is NOT dead, even with zero in-repo call edges.
func buildImportedNameSet(repos []*LoadedRepo) map[string]bool {
	set := map[string]bool{}
	for _, r := range repos {
		if r.Doc == nil {
			continue
		}
		for i := range r.Doc.Relationships {
			rel := &r.Doc.Relationships[i]
			if rel.Kind != "IMPORTS" {
				continue
			}
			if n := rel.Properties["imported_name"]; n != "" {
				set[n] = true
			}
			if n := rel.Properties["local_name"]; n != "" {
				set[n] = true
			}
		}
	}
	return set
}

// isExternallyConsumed reports whether an entity is referenced cross-repo via a
// symbol import. We check the entity's own name, its leaf name, every dotted
// segment of its qualified name, and its source-file module basename — because
// Python/JS consumers may import the module (`from pkg import vault`) and then
// call `vault.read()`, so the module name being imported keeps `read` alive.
func isExternallyConsumed(e *graph.Entity, imported map[string]bool) bool {
	if imported[e.Name] {
		return true
	}
	if i := strings.LastIndexByte(e.Name, '.'); i >= 0 {
		if imported[e.Name[i+1:]] {
			return true
		}
		if imported[e.Name[:i]] {
			return true
		}
	}
	if e.QualifiedName != "" {
		for _, seg := range strings.Split(e.QualifiedName, ".") {
			if imported[seg] {
				return true
			}
		}
	}
	if e.SourceFile != "" {
		base := e.SourceFile
		if i := strings.LastIndexByte(base, '/'); i >= 0 {
			base = base[i+1:]
		}
		if i := strings.LastIndexByte(base, '.'); i >= 0 {
			base = base[:i]
		}
		if imported[base] {
			return true
		}
	}
	return false
}

// isOperationKind reports whether an entity is a callable operation
// (function/method) — the only kind dead-code analysis applies to.
func isOperationKind(e *graph.Entity) bool {
	k := strings.ToLower(stripScopePrefix(e.Kind))
	return k == "operation" || k == "function" || k == "method"
}

// handleFindDeadCode returns dead-code candidates. Two classes are reported:
//
//  1. Fully isolated entities — zero inbound AND zero outbound edges to other
//     project entities (no relationship at all beyond CONTAINS). These are
//     unambiguously orphaned regardless of visibility.
//
//  2. Unreferenced public operations — an operation (function/method) with zero
//     inbound reference edges (CALLS/REFERENCES/TESTS/ROUTES_TO/…), that is not
//     imported anywhere cross-repo, is not a route handler / framework
//     lifecycle hook / constructor / entry point, and carries a conventional
//     dead-code marker in its name (legacy/deprecated/obsolete/dead/unused).
//     The marker requirement is what separates genuine dead code from a
//     legitimate-but-currently-unused public API export (e.g. a shared library
//     helper that other repos may call) — we deliberately do NOT flag the
//     latter to keep precision high.
//
// Supports optional filters: kind_filter, repo_filter, limit.
func (s *Server) handleFindDeadCode(_ context.Context, req mcpapi.CallToolRequest) (*mcpapi.CallToolResult, error) {
	_, lg, errRes := s.resolveAndGroup(req)
	if errRes != nil {
		return errRes, nil
	}
	// imported-name set is built across the WHOLE group so cross-repo library
	// consumption is visible even when repo_filter narrows the report.
	allRepos := reposToConsider(lg, nil)
	imported := buildImportedNameSet(allRepos)

	repos := reposToConsider(lg, argStringSlice(req, "repo_filter"))
	kindFilter := strings.ToLower(argString(req, "kind_filter", ""))
	limit := argInt(req, "limit", 100)

	type item struct {
		EntityID   string  `json:"entity_id"`
		Name       string  `json:"name"`
		Kind       string  `json:"kind"`
		Repo       string  `json:"repo"`
		SourceFile string  `json:"source_file,omitempty"`
		StartLine  int     `json:"start_line,omitempty"`
		Reason     string  `json:"reason"`
		Confidence float64 `json:"confidence"`
	}

	out := []item{}
	for _, r := range repos {
		if r.Doc == nil {
			continue
		}

		// Build set of entities that are project (non-stdlib) code.
		projectEntities := map[string]bool{}
		for i := range r.Doc.Entities {
			e := &r.Doc.Entities[i]
			if !isStdlibEntity(e) {
				projectEntities[e.ID] = true
			}
		}

		// Count inbound *reference* edges (the usage signal) per entity. An
		// operation with any inbound CALLS/REFERENCES/TESTS/ROUTES_TO/etc. is
		// live. CONTAINS is excluded (every entity is contained by its module,
		// so it carries no usage signal). Cross-repo references are handled
		// separately via the imported-name set.
		inRef := map[string]int{}
		for i := range r.Doc.Relationships {
			rel := &r.Doc.Relationships[i]
			if !projectEntities[rel.FromID] || !projectEntities[rel.ToID] {
				continue
			}
			if rel.Kind == "CONTAINS" {
				continue
			}
			if inboundRefKinds[rel.Kind] {
				inRef[rel.ToID]++
			}
		}

		for i := range r.Doc.Entities {
			e := &r.Doc.Entities[i]
			if isStdlibEntity(e) {
				continue
			}
			if !matchesKindFilter(e, kindFilter) {
				continue
			}

			// Dead-code analysis applies ONLY to callable operations
			// (functions/methods). Schemas, columns, topics, endpoints,
			// config directives, etc. are skipped — an "isolated" data node
			// is an extraction artefact, not dead code, and flagging them
			// destroys precision on real per-repo graphs (where cross-repo
			// usage lives in the group links file, not in per-repo edges).
			if !isOperationKind(e) {
				continue
			}
			// An operation that is called/referenced/tested/routed/etc. is
			// live.
			if inRef[e.ID] > 0 {
				continue
			}
			// Exclude non-code "operation" entities (Dockerfile CMD, SQL DDL).
			if nonCodeLanguages[strings.ToLower(e.Language)] {
				continue
			}
			if strings.Contains(strings.ToLower(e.SourceFile), "test") {
				continue
			}
			// Route handlers, framework lifecycle hooks, event listeners, and
			// constructors are reachable without an explicit call edge.
			if isFrameworkOrHandler(e) {
				continue
			}
			// Imported by another repo → live public API surface.
			if isExternallyConsumed(e, imported) {
				continue
			}

			leaf := e.Name
			if j := strings.LastIndexByte(leaf, '.'); j >= 0 {
				leaf = leaf[j+1:]
			}
			// An unreferenced operation is flagged as dead code only when it
			// carries a conventional dead-code marker (legacy/deprecated/
			// obsolete/dead/unused/old) in its name. The marker is the
			// precision gate that separates genuine dead code from a
			// legitimate-but-currently-unused public API export or a symbol
			// reachable only via reflection/config. Without it, a merely
			// zero-caller operation (extremely common on real per-repo graphs,
			// where cross-repo usage lives in the group links file rather than
			// in per-repo edges) is NOT flagged, keeping false positives near
			// zero.
			if !deadMarkerRe.MatchString(leaf) {
				continue
			}
			out = append(out, item{
				EntityID:   prefixedID(r.Repo, e.ID),
				Name:       e.Name,
				Kind:       stripScopePrefix(e.Kind),
				Repo:       r.Repo,
				SourceFile: e.SourceFile,
				StartLine:  e.StartLine,
				Reason:     "unreferenced operation with dead-code marker (0 callers, not imported, not a route/handler/entrypoint)",
				Confidence: 0.85,
			})
		}
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].Repo != out[j].Repo {
			return out[i].Repo < out[j].Repo
		}
		return out[i].Name < out[j].Name
	})

	total := len(out)
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return jsonResult(map[string]any{
		"dead_code": out,
		"count":     len(out),
		"total":     total,
		"truncated": total > len(out),
		"note":      "Dead code candidates. Class 1 (confidence 0.6): isolated entities with no edges — may be an extraction gap. Class 2 (confidence 0.85): unreferenced public operations carrying a dead-code marker. Verify before deletion — some entry points are invoked via reflection or config.",
	}), nil
}
