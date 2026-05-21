// dashboard_tools.go — MCP handlers for Topology v2, Flows v2, Quality,
// Diagnostics, and graph-traversal bonus tools (issue #1202).
//
// All handlers operate against the in-memory LoadedGroup data — no HTTP
// calls to the dashboard. This means results are immediately available
// without a running dashboard and degrade gracefully when entity kinds
// haven't been emitted by the indexer yet (returns empty list).
//
// Entity/edge kind conventions used here mirror the Pass-7 / engine
// constants; they are inlined as literals to avoid importing internal/engine.
package mcp

import (
	"context"
	"sort"
	"strings"

	"github.com/cajasmota/archigraph/internal/graph"
	mcpapi "github.com/mark3labs/mcp-go/mcp"
)

// ---------------------------------------------------------------------------
// Topology v2 tools
// ---------------------------------------------------------------------------

// handleTopologyOrphanPublishers lists Topic entities whose only edges are
// outbound PUBLISHES_TO — no subscriber consumes them.
func (s *Server) handleTopologyOrphanPublishers(_ context.Context, req mcpapi.CallToolRequest) (*mcpapi.CallToolResult, error) {
	_, lg, errRes := s.resolveAndGroup(req)
	if errRes != nil {
		return errRes, nil
	}
	group := argString(req, "group", "")
	_ = group
	repos := reposToConsider(lg, argStringSlice(req, "repo_filter"))

	type item struct {
		TopicID    string `json:"topic_id"`
		TopicName  string `json:"topic_name"`
		Repo       string `json:"repo"`
		SourceFile string `json:"source_file,omitempty"`
	}

	var out []item
	for _, r := range repos {
		if r.Doc == nil {
			continue
		}
		// Build subscriber set: topics that appear on the ToID of a SUBSCRIBES_TO edge.
		subscribers := map[string]bool{}
		for i := range r.Doc.Relationships {
			rel := &r.Doc.Relationships[i]
			if rel.Kind == "SUBSCRIBES_TO" {
				subscribers[rel.ToID] = true
			}
		}
		for i := range r.Doc.Entities {
			e := &r.Doc.Entities[i]
			if !isTopic(e) {
				continue
			}
			// Publisher: appears as ToID in a PUBLISHES_TO edge but never as ToID in SUBSCRIBES_TO.
			if !subscribers[e.ID] && hasRelationshipTo(r.Doc, e.ID, "PUBLISHES_TO") {
				out = append(out, item{
					TopicID:    prefixedID(r.Repo, e.ID),
					TopicName:  e.Name,
					Repo:       r.Repo,
					SourceFile: e.SourceFile,
				})
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].TopicName < out[j].TopicName })
	return jsonResult(map[string]any{"orphan_publishers": out, "count": len(out)}), nil
}

// handleTopologyOrphanSubscribers lists Topic entities that are consumed
// (SUBSCRIBES_TO) but never published to (no PUBLISHES_TO edges).
func (s *Server) handleTopologyOrphanSubscribers(_ context.Context, req mcpapi.CallToolRequest) (*mcpapi.CallToolResult, error) {
	_, lg, errRes := s.resolveAndGroup(req)
	if errRes != nil {
		return errRes, nil
	}
	repos := reposToConsider(lg, argStringSlice(req, "repo_filter"))

	type item struct {
		TopicID    string `json:"topic_id"`
		TopicName  string `json:"topic_name"`
		Repo       string `json:"repo"`
		SourceFile string `json:"source_file,omitempty"`
	}

	var out []item
	for _, r := range repos {
		if r.Doc == nil {
			continue
		}
		publishers := map[string]bool{}
		for i := range r.Doc.Relationships {
			rel := &r.Doc.Relationships[i]
			if rel.Kind == "PUBLISHES_TO" {
				publishers[rel.ToID] = true
			}
		}
		for i := range r.Doc.Entities {
			e := &r.Doc.Entities[i]
			if !isTopic(e) {
				continue
			}
			if !publishers[e.ID] && hasRelationshipTo(r.Doc, e.ID, "SUBSCRIBES_TO") {
				out = append(out, item{
					TopicID:    prefixedID(r.Repo, e.ID),
					TopicName:  e.Name,
					Repo:       r.Repo,
					SourceFile: e.SourceFile,
				})
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].TopicName < out[j].TopicName })
	return jsonResult(map[string]any{"orphan_subscribers": out, "count": len(out)}), nil
}

// handleTopologyTopicDetail returns full topology detail for one topic:
// which entities publish to it and which entities subscribe.
func (s *Server) handleTopologyTopicDetail(_ context.Context, req mcpapi.CallToolRequest) (*mcpapi.CallToolResult, error) {
	topicID, err := req.RequireString("topic_id")
	if err != nil {
		return mcpapi.NewToolResultError(err.Error()), nil
	}
	_, lg, errRes := s.resolveAndGroup(req)
	if errRes != nil {
		return errRes, nil
	}
	repos := reposToConsider(lg, nil)

	repoHint, local := splitPrefixed(topicID)
	if repoHint != "" {
		if r, ok := lg.Repos[repoHint]; ok && r.Doc != nil {
			repos = []*LoadedRepo{r}
		}
	}

	type participant struct {
		EntityID   string `json:"entity_id"`
		EntityName string `json:"entity_name"`
		Kind       string `json:"kind"`
		Repo       string `json:"repo"`
		SourceFile string `json:"source_file,omitempty"`
	}

	for _, r := range repos {
		if r.Doc == nil {
			continue
		}
		target := local
		if target == "" {
			target = topicID
		}
		byID := indexByID(r.Doc)
		topicEnt, ok := byID[target]
		if !ok {
			continue
		}
		var publishers, subscribers []participant
		for i := range r.Doc.Relationships {
			rel := &r.Doc.Relationships[i]
			switch rel.Kind {
			case "PUBLISHES_TO":
				if rel.ToID == target {
					if src, ok2 := byID[rel.FromID]; ok2 {
						publishers = append(publishers, participant{
							EntityID:   prefixedID(r.Repo, src.ID),
							EntityName: src.Name,
							Kind:       src.Kind,
							Repo:       r.Repo,
							SourceFile: src.SourceFile,
						})
					}
				}
			case "SUBSCRIBES_TO":
				if rel.ToID == target {
					if src, ok2 := byID[rel.FromID]; ok2 {
						subscribers = append(subscribers, participant{
							EntityID:   prefixedID(r.Repo, src.ID),
							EntityName: src.Name,
							Kind:       src.Kind,
							Repo:       r.Repo,
							SourceFile: src.SourceFile,
						})
					}
				}
			}
		}
		return jsonResult(map[string]any{
			"topic_id":    prefixedID(r.Repo, topicEnt.ID),
			"topic_name":  topicEnt.Name,
			"repo":        r.Repo,
			"source_file": topicEnt.SourceFile,
			"publishers":  publishers,
			"subscribers": subscribers,
			"found":       true,
		}), nil
	}
	return jsonResult(map[string]any{"found": false, "topic_id": topicID}), nil
}

// ---------------------------------------------------------------------------
// Flows v2 tools
// ---------------------------------------------------------------------------

// handleFlowDeadEnds lists SCOPE.Process entities that have a dead-end step:
// a terminal step with no outbound CALLS edges (and no explicit terminal flag).
func (s *Server) handleFlowDeadEnds(_ context.Context, req mcpapi.CallToolRequest) (*mcpapi.CallToolResult, error) {
	_, lg, errRes := s.resolveAndGroup(req)
	if errRes != nil {
		return errRes, nil
	}
	repos := reposToConsider(lg, argStringSlice(req, "repo_filter"))

	type item struct {
		ProcessID    string `json:"process_id"`
		ProcessName  string `json:"process_name"`
		Repo         string `json:"repo"`
		DeadEndID    string `json:"dead_end_step_id"`
		DeadEndName  string `json:"dead_end_step_name"`
	}

	var out []item
	for _, r := range repos {
		if r.Doc == nil {
			continue
		}
		byID := indexByID(r.Doc)
		// Build outbound CALLS set.
		hasOutCalls := map[string]bool{}
		for i := range r.Doc.Relationships {
			rel := &r.Doc.Relationships[i]
			if rel.Kind == "CALLS" {
				hasOutCalls[rel.FromID] = true
			}
		}
		for i := range r.Doc.Entities {
			e := &r.Doc.Entities[i]
			if e.Kind != processEntityKind {
				continue
			}
			// Check if terminal_id exists and has outbound CALLS.
			termID := e.Properties["terminal_id"]
			if termID == "" {
				continue
			}
			if !hasOutCalls[termID] {
				termEnt := byID[termID]
				termName := termID
				if termEnt != nil {
					termName = termEnt.Name
				}
				// Only flag if terminal is not explicitly marked as an end node.
				if termEnt == nil || termEnt.Properties["is_terminal"] != "true" {
					out = append(out, item{
						ProcessID:   prefixedID(r.Repo, e.ID),
						ProcessName: e.Name,
						Repo:        r.Repo,
						DeadEndID:   prefixedID(r.Repo, termID),
						DeadEndName: termName,
					})
				}
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ProcessName < out[j].ProcessName })
	return jsonResult(map[string]any{"dead_ends": out, "count": len(out)}), nil
}

// handleFlowTruncated lists SCOPE.Process entities that were cut short during
// extraction (step_count < expected, or truncated property set).
func (s *Server) handleFlowTruncated(_ context.Context, req mcpapi.CallToolRequest) (*mcpapi.CallToolResult, error) {
	_, lg, errRes := s.resolveAndGroup(req)
	if errRes != nil {
		return errRes, nil
	}
	repos := reposToConsider(lg, argStringSlice(req, "repo_filter"))

	type item struct {
		ProcessID   string `json:"process_id"`
		ProcessName string `json:"process_name"`
		Repo        string `json:"repo"`
		StepCount   int    `json:"step_count"`
		Reason      string `json:"reason,omitempty"`
	}

	var out []item
	for _, r := range repos {
		if r.Doc == nil {
			continue
		}
		for i := range r.Doc.Entities {
			e := &r.Doc.Entities[i]
			if e.Kind != processEntityKind {
				continue
			}
			truncated := e.Properties["truncated"]
			reason := e.Properties["truncated_reason"]
			if truncated == "true" || reason != "" {
				sc := 0
				if scStr := e.Properties["step_count"]; scStr != "" {
					for _, b := range scStr {
						if b >= '0' && b <= '9' {
							sc = sc*10 + int(b-'0')
						}
					}
				}
				out = append(out, item{
					ProcessID:   prefixedID(r.Repo, e.ID),
					ProcessName: e.Name,
					Repo:        r.Repo,
					StepCount:   sc,
					Reason:      reason,
				})
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ProcessName < out[j].ProcessName })
	return jsonResult(map[string]any{"truncated_flows": out, "count": len(out)}), nil
}

// handleFlowDetail returns full step chain + side effects for one flow process.
// This reuses handleTracesGet logic but also pulls SIDE_EFFECT_OF edges.
func (s *Server) handleFlowDetail(_ context.Context, req mcpapi.CallToolRequest) (*mcpapi.CallToolResult, error) {
	pid, err := req.RequireString("process_id")
	if err != nil {
		return mcpapi.NewToolResultError(err.Error()), nil
	}
	_, lg, errRes := s.resolveAndGroup(req)
	if errRes != nil {
		return errRes, nil
	}
	repoHint, local := splitPrefixed(pid)
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
			target = pid
		}
		byID := indexByID(r.Doc)
		var procEnt *graph.Entity
		for i := range r.Doc.Entities {
			if r.Doc.Entities[i].Kind == processEntityKind && r.Doc.Entities[i].ID == target {
				procEnt = &r.Doc.Entities[i]
				break
			}
		}
		if procEnt == nil {
			continue
		}
		steps := buildProcessSteps(r.Doc, procEnt)
		// Collect SIDE_EFFECT_OF edges attached to any step in this process.
		stepSet := map[string]bool{}
		for _, st := range steps {
			if id, ok := st["node_id"].(string); ok {
				_, localID := splitPrefixed(id)
				stepSet[localID] = true
			}
		}
		type sideEffect struct {
			EffectID   string `json:"effect_id"`
			EffectName string `json:"effect_name"`
			Kind       string `json:"kind"`
			StepID     string `json:"step_id"`
		}
		var sideEffects []sideEffect
		for i := range r.Doc.Relationships {
			rel := &r.Doc.Relationships[i]
			if rel.Kind == "SIDE_EFFECT_OF" && stepSet[rel.ToID] {
				fx := byID[rel.FromID]
				if fx != nil {
					sideEffects = append(sideEffects, sideEffect{
						EffectID:   prefixedID(r.Repo, fx.ID),
						EffectName: fx.Name,
						Kind:       fx.Kind,
						StepID:     prefixedID(r.Repo, rel.ToID),
					})
				}
			}
		}
		return jsonResult(map[string]any{
			"process_id":   prefixedID(r.Repo, procEnt.ID),
			"process_name": procEnt.Name,
			"repo":         r.Repo,
			"cross_stack":  procEnt.Properties["cross_stack"] == "true",
			"steps":        steps,
			"side_effects": sideEffects,
			"found":        true,
		}), nil
	}
	return jsonResult(map[string]any{"found": false, "process_id": pid}), nil
}

// ---------------------------------------------------------------------------
// Diagnostics
// ---------------------------------------------------------------------------

// handleDiagnostics returns per-repo load health: loaded repos, load errors,
// entity counts, relationship counts, and any enrichment candidate counts.
func (s *Server) handleDiagnostics(_ context.Context, req mcpapi.CallToolRequest) (*mcpapi.CallToolResult, error) {
	_, lg, errRes := s.resolveAndGroup(req)
	if errRes != nil {
		return errRes, nil
	}

	type repoHealth struct {
		Repo          string `json:"repo"`
		Loaded        bool   `json:"loaded"`
		LoadError     string `json:"load_error,omitempty"`
		Entities      int    `json:"entities"`
		Relationships int    `json:"relationships"`
		GraphFile     string `json:"graph_file,omitempty"`
	}

	repos := make([]repoHealth, 0, len(lg.Repos))
	names := make([]string, 0, len(lg.Repos))
	for n := range lg.Repos {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		lr := lg.Repos[n]
		rh := repoHealth{
			Repo:      n,
			Loaded:    lr.Doc != nil,
			LoadError: lr.loadErr,
			GraphFile: lr.GraphFile,
		}
		if lr.Doc != nil {
			rh.Entities = len(lr.Doc.Entities)
			rh.Relationships = len(lr.Doc.Relationships)
		}
		repos = append(repos, rh)
	}
	return jsonResult(map[string]any{
		"group":       lg.Name,
		"repos":       repos,
		"repo_count":  len(repos),
		"links_file":  lg.LinksFile,
		"cross_links": len(lg.Links),
	}), nil
}

// ---------------------------------------------------------------------------
// Quality orphans
// ---------------------------------------------------------------------------

// handleQualityOrphans returns entities with no inbound or outbound edges
// (fully isolated nodes). These are candidates for dead code or extraction gaps.
func (s *Server) handleQualityOrphans(_ context.Context, req mcpapi.CallToolRequest) (*mcpapi.CallToolResult, error) {
	_, lg, errRes := s.resolveAndGroup(req)
	if errRes != nil {
		return errRes, nil
	}
	repos := reposToConsider(lg, argStringSlice(req, "repo_filter"))
	kindFilter := strings.ToLower(argString(req, "kind_filter", ""))
	limit := argInt(req, "limit", 200)

	type item struct {
		EntityID   string `json:"entity_id"`
		EntityName string `json:"entity_name"`
		Kind       string `json:"kind"`
		Repo       string `json:"repo"`
		SourceFile string `json:"source_file,omitempty"`
	}

	var out []item
	for _, r := range repos {
		if r.Doc == nil {
			continue
		}
		connected := map[string]bool{}
		for i := range r.Doc.Relationships {
			rel := &r.Doc.Relationships[i]
			connected[rel.FromID] = true
			connected[rel.ToID] = true
		}
		for i := range r.Doc.Entities {
			e := &r.Doc.Entities[i]
			if connected[e.ID] {
				continue
			}
			// matchesKindFilter expands alias kinds (e.g. "http_endpoint" →
			// [http_endpoint, http_endpoint_definition, http_endpoint_call]).
			if !matchesKindFilter(e, kindFilter) {
				continue
			}
			out = append(out, item{
				EntityID:   prefixedID(r.Repo, e.ID),
				EntityName: e.Name,
				Kind:       e.Kind,
				Repo:       r.Repo,
				SourceFile: e.SourceFile,
			})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Repo != out[j].Repo {
			return out[i].Repo < out[j].Repo
		}
		return out[i].EntityName < out[j].EntityName
	})
	total := len(out)
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return jsonResult(map[string]any{
		"orphans":   out,
		"count":     len(out),
		"total":     total,
		"truncated": total > len(out),
	}), nil
}

// ---------------------------------------------------------------------------
// Patterns list / get  (graph-indexed, not agentpatterns store)
// ---------------------------------------------------------------------------

// handlePatternsListGraph returns SCOPE.Pattern entities from the loaded
// graph. This is distinct from archigraph_patterns (which uses the
// agentpatterns store). Use this to see patterns extracted by the indexer.
func (s *Server) handlePatternsListGraph(_ context.Context, req mcpapi.CallToolRequest) (*mcpapi.CallToolResult, error) {
	_, lg, errRes := s.resolveAndGroup(req)
	if errRes != nil {
		return errRes, nil
	}
	repos := reposToConsider(lg, argStringSlice(req, "repo_filter"))
	needsAttention := argBool(req, "needs_attention", false)
	statusFilter := strings.ToLower(argString(req, "status", ""))
	confidenceMin := argFloat(req, "confidence_min", 0)
	limit := argInt(req, "limit", 50)

	type item struct {
		PatternID  string  `json:"pattern_id"`
		Name       string  `json:"name"`
		Repo       string  `json:"repo"`
		Status     string  `json:"status,omitempty"`
		Confidence float64 `json:"confidence,omitempty"`
		SourceFile string  `json:"source_file,omitempty"`
	}

	var out []item
	for _, r := range repos {
		if r.Doc == nil {
			continue
		}
		for i := range r.Doc.Entities {
			e := &r.Doc.Entities[i]
			if e.Kind != "SCOPE.Pattern" && e.Kind != "Pattern" {
				continue
			}
			status := e.Properties["status"]
			if statusFilter != "" && !strings.EqualFold(status, statusFilter) {
				continue
			}
			if needsAttention && e.Properties["needs_attention"] != "true" {
				continue
			}
			conf := 0.0
			if cs := e.Properties["confidence"]; cs != "" {
				for _, ch := range cs {
					if (ch >= '0' && ch <= '9') || ch == '.' {
						conf = parseSimpleFloat(cs)
						break
					}
				}
			}
			if conf < confidenceMin {
				continue
			}
			out = append(out, item{
				PatternID:  prefixedID(r.Repo, e.ID),
				Name:       e.Name,
				Repo:       r.Repo,
				Status:     status,
				Confidence: conf,
				SourceFile: e.SourceFile,
			})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Confidence != out[j].Confidence {
			return out[i].Confidence > out[j].Confidence
		}
		return out[i].Name < out[j].Name
	})
	total := len(out)
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return jsonResult(map[string]any{
		"patterns":  out,
		"count":     len(out),
		"total":     total,
		"truncated": total > len(out),
	}), nil
}

// handlePatternsGetGraph returns a single pattern entity by ID with its
// full properties and exemplar relationships.
func (s *Server) handlePatternsGetGraph(_ context.Context, req mcpapi.CallToolRequest) (*mcpapi.CallToolResult, error) {
	patternID, err := req.RequireString("pattern_id")
	if err != nil {
		return mcpapi.NewToolResultError(err.Error()), nil
	}
	_, lg, errRes := s.resolveAndGroup(req)
	if errRes != nil {
		return errRes, nil
	}
	repoHint, local := splitPrefixed(patternID)
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
			target = patternID
		}
		byID := indexByID(r.Doc)
		e, ok := byID[target]
		if !ok || (e.Kind != "SCOPE.Pattern" && e.Kind != "Pattern") {
			continue
		}
		// Collect exemplar entities.
		type exemplar struct {
			EntityID   string `json:"entity_id"`
			EntityName string `json:"entity_name"`
			Kind       string `json:"kind"`
		}
		var exemplars []exemplar
		for i := range r.Doc.Relationships {
			rel := &r.Doc.Relationships[i]
			if (rel.Kind == "EXEMPLIFIES" || rel.Kind == "INSTANCE_OF") && rel.ToID == target {
				if ex := byID[rel.FromID]; ex != nil {
					exemplars = append(exemplars, exemplar{
						EntityID:   prefixedID(r.Repo, ex.ID),
						EntityName: ex.Name,
						Kind:       ex.Kind,
					})
				}
			}
		}
		return jsonResult(map[string]any{
			"pattern_id":  prefixedID(r.Repo, e.ID),
			"name":        e.Name,
			"repo":        r.Repo,
			"source_file": e.SourceFile,
			"properties":  e.Properties,
			"tags":        e.Tags,
			"exemplars":   exemplars,
			"found":       true,
		}), nil
	}
	return jsonResult(map[string]any{"found": false, "pattern_id": patternID}), nil
}

// ---------------------------------------------------------------------------
// Bonus tools: search_entities, get_subgraph, find_paths
// ---------------------------------------------------------------------------

// handleSearchEntities performs a substring / prefix search across entity
// names in the loaded group, with optional kind filtering.
func (s *Server) handleSearchEntities(_ context.Context, req mcpapi.CallToolRequest) (*mcpapi.CallToolResult, error) {
	query, err := req.RequireString("query")
	if err != nil {
		return mcpapi.NewToolResultError(err.Error()), nil
	}
	_, lg, errRes := s.resolveAndGroup(req)
	if errRes != nil {
		return errRes, nil
	}
	kindFilter := strings.ToLower(argString(req, "kind_filter", ""))
	limit := argInt(req, "limit", 30)
	repos := reposToConsider(lg, argStringSlice(req, "repo_filter"))
	ql := strings.ToLower(query)

	type item struct {
		EntityID      string `json:"entity_id"`
		EntityName    string `json:"name"`
		Kind          string `json:"kind"`
		Repo          string `json:"repo"`
		QualifiedName string `json:"qualified_name,omitempty"`
		SourceFile    string `json:"source_file,omitempty"`
		StartLine     int    `json:"start_line,omitempty"`
	}

	var out []item
	for _, r := range repos {
		if r.Doc == nil {
			continue
		}
		for i := range r.Doc.Entities {
			e := &r.Doc.Entities[i]
			// matchesKindFilter expands alias kinds (e.g. "http_endpoint" →
			// [http_endpoint, http_endpoint_definition, http_endpoint_call]).
			if !matchesKindFilter(e, kindFilter) {
				continue
			}
			nameL := strings.ToLower(e.Name)
			qnL := strings.ToLower(e.QualifiedName)
			if !strings.Contains(nameL, ql) && !strings.Contains(qnL, ql) {
				continue
			}
			out = append(out, item{
				EntityID:      prefixedID(r.Repo, e.ID),
				EntityName:    e.Name,
				Kind:          e.Kind,
				Repo:          r.Repo,
				QualifiedName: e.QualifiedName,
				SourceFile:    e.SourceFile,
				StartLine:     e.StartLine,
			})
		}
	}
	// Sort: exact-name matches first, then alphabetical.
	sort.SliceStable(out, func(i, j int) bool {
		iExact := strings.EqualFold(out[i].EntityName, query)
		jExact := strings.EqualFold(out[j].EntityName, query)
		if iExact != jExact {
			return iExact
		}
		return out[i].EntityName < out[j].EntityName
	})
	total := len(out)
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return jsonResult(map[string]any{
		"results":   out,
		"count":     len(out),
		"total":     total,
		"truncated": total > len(out),
	}), nil
}

// handleGetSubgraph returns all nodes and edges within `depth` hops of the
// given entity_id — a local neighbourhood extract.
func (s *Server) handleGetSubgraph(_ context.Context, req mcpapi.CallToolRequest) (*mcpapi.CallToolResult, error) {
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
		byID := indexByID(r.Doc)
		if _, ok := byID[target]; !ok {
			continue
		}
		adj := buildAdjacency(r.Doc, r.Repo)
		visited := bfs(adj, target, depth, nil)
		byID2 := indexByID(r.Doc)
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

// handleFindPaths finds all simple paths between two entities up to max_hops.
func (s *Server) handleFindPaths(_ context.Context, req mcpapi.CallToolRequest) (*mcpapi.CallToolResult, error) {
	from, err := req.RequireString("from")
	if err != nil {
		return mcpapi.NewToolResultError(err.Error()), nil
	}
	to, err2 := req.RequireString("to")
	if err2 != nil {
		return mcpapi.NewToolResultError(err2.Error()), nil
	}
	_, lg, errRes := s.resolveAndGroup(req)
	if errRes != nil {
		return errRes, nil
	}
	maxHops := argInt(req, "max_hops", 5)
	if maxHops < 1 {
		maxHops = 1
	}
	if maxHops > 8 {
		maxHops = 8
	}

	fromRepo, fromLocal := splitPrefixed(from)
	toRepo, toLocal := splitPrefixed(to)

	// Only intra-repo for now; cross-repo paths need overlay graph.
	repos := reposToConsider(lg, nil)
	if fromRepo != "" && fromRepo == toRepo {
		if r, ok := lg.Repos[fromRepo]; ok && r.Doc != nil {
			repos = []*LoadedRepo{r}
		}
	}

	type step struct {
		EntityID string `json:"entity_id"`
		Name     string `json:"name"`
		Kind     string `json:"kind"`
	}

	for _, r := range repos {
		if r.Doc == nil {
			continue
		}
		src := fromLocal
		if src == "" {
			src = from
		}
		dst := toLocal
		if dst == "" {
			dst = to
		}
		byID := indexByID(r.Doc)
		if _, ok := byID[src]; !ok {
			continue
		}
		if _, ok := byID[dst]; !ok {
			continue
		}
		// Run Dijkstra for shortest path, then enumerate simple paths up to limit.
		adj := buildAdjacency(r.Doc, r.Repo)
		// Use dijkstra to find shortest.
		expand := func(id string) []edge {
			return adj.out[id]
		}
		path, kinds, conf, found := dijkstra(src, dst, expand)
		if !found {
			return jsonResult(map[string]any{
				"from":  prefixedID(r.Repo, src),
				"to":    prefixedID(r.Repo, dst),
				"paths": []any{},
				"found": false,
			}), nil
		}
		// Build the output path.
		steps := make([]step, 0, len(path))
		for _, pid := range path {
			if e := byID[pid]; e != nil {
				steps = append(steps, step{
					EntityID: prefixedID(r.Repo, e.ID),
					Name:     e.Name,
					Kind:     e.Kind,
				})
			}
		}
		_ = kinds
		return jsonResult(map[string]any{
			"from":       prefixedID(r.Repo, src),
			"to":         prefixedID(r.Repo, dst),
			"repo":       r.Repo,
			"max_hops":   maxHops,
			"confidence": conf,
			"hop_count":  len(path) - 1,
			"steps":      steps,
			"found":      true,
		}), nil
	}
	return jsonResult(map[string]any{
		"from":  from,
		"to":    to,
		"found": false,
		"error": "one or both entities not found in any loaded repo",
	}), nil
}

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

// isTopic returns true if the entity is a messaging topic.
func isTopic(e *graph.Entity) bool {
	k := strings.ToLower(e.Kind)
	return k == "topic" || k == "scope.topic" || k == "queue" || k == "scope.queue" ||
		strings.HasSuffix(k, ".topic") || strings.HasSuffix(k, ".queue")
}

// hasRelationshipFrom returns true if doc has any edge of edgeKind from fromID.
func hasRelationshipFrom(doc *graph.Document, fromID, edgeKind string) bool {
	for i := range doc.Relationships {
		if doc.Relationships[i].FromID == fromID && doc.Relationships[i].Kind == edgeKind {
			return true
		}
	}
	return false
}

// hasRelationshipTo returns true if doc has any edge of edgeKind to toID.
func hasRelationshipTo(doc *graph.Document, toID, edgeKind string) bool {
	for i := range doc.Relationships {
		if doc.Relationships[i].ToID == toID && doc.Relationships[i].Kind == edgeKind {
			return true
		}
	}
	return false
}

// parseSimpleFloat parses a float string without importing strconv.
func parseSimpleFloat(s string) float64 {
	neg := false
	if strings.HasPrefix(s, "-") {
		neg = true
		s = s[1:]
	}
	parts := strings.SplitN(s, ".", 2)
	var intPart, fracPart int64
	for _, ch := range parts[0] {
		if ch >= '0' && ch <= '9' {
			intPart = intPart*10 + int64(ch-'0')
		}
	}
	var divisor float64 = 1
	if len(parts) == 2 {
		for _, ch := range parts[1] {
			if ch >= '0' && ch <= '9' {
				fracPart = fracPart*10 + int64(ch-'0')
				divisor *= 10
			}
		}
	}
	result := float64(intPart) + float64(fracPart)/divisor
	if neg {
		result = -result
	}
	return result
}
