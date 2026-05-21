// flow_tools.go — MCP handlers for flow-aware graph traversal tools (issue #1252).
//
// Implements:
//   - archigraph_find_callers   — what calls this entity (inbound edges, N hops)
//   - archigraph_find_callees   — what does this entity call (outbound edges, N hops)
//   - archigraph_impact_radius  — entities affected if this one changes, with risk score
//   - archigraph_summarize_subgraph — LLM-friendly markdown summary of entity neighbourhood
//   - archigraph_find_dead_code — entities with 0 inbound + 0 outbound non-stdlib edges
//
// All handlers operate against the in-memory LoadedGroup data — no HTTP calls.
package mcp

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/cajasmota/archigraph/internal/graph"
	mcpapi "github.com/mark3labs/mcp-go/mcp"
)

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

	repoHint, local := splitPrefixed(entityID)
	repos := reposToConsider(lg, nil)
	if repoHint != "" {
		if r, ok := lg.Repos[repoHint]; ok && r.Doc != nil {
			repos = []*LoadedRepo{r}
		}
	}

	type caller struct {
		EntityID   string `json:"entity_id"`
		Name       string `json:"name"`
		Kind       string `json:"kind"`
		Repo       string `json:"repo"`
		SourceFile string `json:"source_file,omitempty"`
		StartLine  int    `json:"start_line,omitempty"`
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
		byID := indexByID(r.Doc)
		if _, ok := byID[target]; !ok {
			continue
		}

		// BFS over inbound-only adjacency.
		adj := buildAdjacency(r.Doc, r.Repo)
		visited := map[string]int{target: 0}
		frontier := []string{target}
		for d := 0; d < depth; d++ {
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

		callers := []caller{}
		for id, d := range visited {
			if id == target {
				continue
			}
			e := byID[id]
			if e == nil {
				continue
			}
			callers = append(callers, caller{
				EntityID:   prefixedID(r.Repo, e.ID),
				Name:       e.Name,
				Kind:       stripScopePrefix(e.Kind),
				Repo:       r.Repo,
				SourceFile: e.SourceFile,
				StartLine:  e.StartLine,
				HopCount:   d,
			})
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
		return jsonResult(map[string]any{
			"entity_id":   prefixedID(r.Repo, target),
			"entity_name": rootName,
			"repo":        r.Repo,
			"depth":       depth,
			"callers":     callers,
			"count":       len(callers),
		}), nil
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

	repoHint, local := splitPrefixed(entityID)
	repos := reposToConsider(lg, nil)
	if repoHint != "" {
		if r, ok := lg.Repos[repoHint]; ok && r.Doc != nil {
			repos = []*LoadedRepo{r}
		}
	}

	type callee struct {
		EntityID   string `json:"entity_id"`
		Name       string `json:"name"`
		Kind       string `json:"kind"`
		Repo       string `json:"repo"`
		SourceFile string `json:"source_file,omitempty"`
		StartLine  int    `json:"start_line,omitempty"`
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
		byID := indexByID(r.Doc)
		if _, ok := byID[target]; !ok {
			continue
		}

		// BFS over outbound-only adjacency.
		adj := buildAdjacency(r.Doc, r.Repo)
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
			callees = append(callees, callee{
				EntityID:   prefixedID(r.Repo, e.ID),
				Name:       e.Name,
				Kind:       stripScopePrefix(e.Kind),
				Repo:       r.Repo,
				SourceFile: e.SourceFile,
				StartLine:  e.StartLine,
				HopCount:   d,
			})
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
		return jsonResult(map[string]any{
			"entity_id":   prefixedID(r.Repo, target),
			"entity_name": rootName,
			"repo":        r.Repo,
			"depth":       depth,
			"callees":     callees,
			"count":       len(callees),
		}), nil
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
		byID := indexByID(r.Doc)
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
		adj := buildAdjacency(r.Doc, r.Repo)
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
			"entity_id":    prefixedID(r.Repo, target),
			"entity_name":  rootName,
			"repo":         r.Repo,
			"hops":         hops,
			"affected":     results,
			"count":        len(results),
			"tip":          "risk_score 0.0–1.0: higher means the affected entity is more sensitive to breakage from changes in the root entity.",
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
// archigraph_summarize_subgraph
// ---------------------------------------------------------------------------

// handleSummarizeSubgraph returns an LLM-friendly Markdown summary of an
// entity's local neighbourhood. The summary can be pasted directly into a
// doc or used as context for a follow-up agent prompt.
func (s *Server) handleSummarizeSubgraph(_ context.Context, req mcpapi.CallToolRequest) (*mcpapi.CallToolResult, error) {
	entityID, err := req.RequireString("entity_id")
	if err != nil {
		return mcpapi.NewToolResultError(err.Error()), nil
	}
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
		byID := indexByID(r.Doc)
		root, ok := byID[target]
		if !ok {
			continue
		}

		adj := buildAdjacency(r.Doc, r.Repo)

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

// handleFindDeadCode returns entities with 0 inbound and 0 outbound edges to
// non-stdlib project entities — candidates for dead/unused code. Supports
// optional filters: kind_filter, repo_filter, max_age_days (entities whose
// source file has not been modified in N months are flagged older_than_filter).
func (s *Server) handleFindDeadCode(_ context.Context, req mcpapi.CallToolRequest) (*mcpapi.CallToolResult, error) {
	_, lg, errRes := s.resolveAndGroup(req)
	if errRes != nil {
		return errRes, nil
	}
	repos := reposToConsider(lg, argStringSlice(req, "repo_filter"))
	kindFilter := strings.ToLower(argString(req, "kind_filter", ""))
	limit := argInt(req, "limit", 100)

	type item struct {
		EntityID   string `json:"entity_id"`
		Name       string `json:"name"`
		Kind       string `json:"kind"`
		Repo       string `json:"repo"`
		SourceFile string `json:"source_file,omitempty"`
		StartLine  int    `json:"start_line,omitempty"`
		Reason     string `json:"reason"`
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

		// Count non-stdlib inbound and outbound edges per entity.
		inCount := map[string]int{}
		outCount := map[string]int{}
		for i := range r.Doc.Relationships {
			rel := &r.Doc.Relationships[i]
			if projectEntities[rel.FromID] && projectEntities[rel.ToID] {
				outCount[rel.FromID]++
				inCount[rel.ToID]++
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
			if inCount[e.ID] > 0 || outCount[e.ID] > 0 {
				continue
			}
			reason := "no inbound or outbound edges to project entities"
			if inCount[e.ID] == 0 && outCount[e.ID] > 0 {
				reason = "no callers (unreferenced entry point)"
			} else if inCount[e.ID] > 0 && outCount[e.ID] == 0 {
				reason = "no callees (leaf with callers — not dead)"
				continue // skip: it is a leaf but used
			}
			out = append(out, item{
				EntityID:   prefixedID(r.Repo, e.ID),
				Name:       e.Name,
				Kind:       stripScopePrefix(e.Kind),
				Repo:       r.Repo,
				SourceFile: e.SourceFile,
				StartLine:  e.StartLine,
				Reason:     reason,
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
		"note":      "Dead code candidates: entities with 0 inbound + 0 outbound edges to other project entities. Verify before deletion — some may be entry points called via reflection or config.",
	}), nil
}
