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
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/cajasmota/grafel/internal/graph"
	mcpapi "github.com/mark3labs/mcp-go/mcp"
)

// ---------------------------------------------------------------------------
// grafel_topology — action-dispatch bundle (#1281)
// Replaces: topology_orphan_publishers, topology_orphan_subscribers,
//           topology_topic_detail
// ---------------------------------------------------------------------------

// handleTopology dispatches on action= to the appropriate topology handler.
//
// #5781: action defaults to "channels" (the full topic listing with per-topic
// publisher/consumer edge counts) when none is supplied, so `grafel_orient
// view=topology` shows the message topics the caller expects rather than only
// the orphan-publisher subset.
func (s *Server) handleTopology(ctx context.Context, req mcpapi.CallToolRequest) (*mcpapi.CallToolResult, error) {
	action := argString(req, "action", "channels")
	switch action {
	case "channels", "list":
		return s.handleTopologyChannels(ctx, req)
	case "orphan_publishers":
		return s.handleTopologyOrphanPublishers(ctx, req)
	case "orphan_subscribers":
		return s.handleTopologyOrphanSubscribers(ctx, req)
	case "topic_detail":
		return s.handleTopologyTopicDetail(ctx, req)
	default:
		return mcpapi.NewToolResultError(
			"unknown action " + action + " (allowed: channels, orphan_publishers, orphan_subscribers, topic_detail)",
		), nil
	}
}

// topologyChannelsMax caps the channel listing so a topic-heavy corpus can't
// blow the MCP token budget. Truncation is surfaced via the response.
const topologyChannelsMax = 200

// handleTopologyChannels lists every message topic/channel in scope with its
// publisher (inbound PUBLISHES_TO) and consumer (inbound SUBSCRIBES_TO /
// DELIVERS_TO) edge counts. This is the default view for grafel_orient
// view=topology (#5781).
func (s *Server) handleTopologyChannels(_ context.Context, req mcpapi.CallToolRequest) (*mcpapi.CallToolResult, error) {
	_, lg, errRes := s.resolveAndGroup(req)
	if errRes != nil {
		return errRes, nil
	}
	repos := reposToConsider(lg, argStringSlice(req, "repo_filter"))

	type channel struct {
		TopicID        string `json:"topic_id"`
		TopicName      string `json:"topic_name"`
		Repo           string `json:"repo"`
		PublisherCount int    `json:"publisher_count"`
		ConsumerCount  int    `json:"consumer_count"`
		SourceFile     string `json:"source_file,omitempty"`
	}

	var out []channel
	for _, r := range repos {
		if r.Doc == nil {
			continue
		}
		// Tally publisher/consumer edges per topic in a single pass.
		//
		// Edge directions (see internal/types/kinds.go, async_trigger_edges.go):
		//   PUBLISHES_TO  handler → topic   (topic = ToID)   inbound
		//   SUBSCRIBES_TO handler → topic   (topic = ToID)   inbound
		//   DELIVERS_TO   topic   → handler (topic = FromID) outbound — the
		//                 synthesised INVERSE of SUBSCRIBES_TO (#5686). A served
		//                 doc may carry topic→handler DELIVERS_TO after
		//                 stripProcessEntities removes SUBSCRIBES_TO.
		//
		// consumer_count is the number of DISTINCT consumer handlers: because
		// DELIVERS_TO is the inverse of SUBSCRIBES_TO, a handler with both edges
		// must be counted once, so we union handler IDs per topic in a set.
		pubCount := map[string]int{}
		consumers := map[string]map[string]bool{} // topicID → set of handler IDs
		addConsumer := func(topicID, handlerID string) {
			set := consumers[topicID]
			if set == nil {
				set = map[string]bool{}
				consumers[topicID] = set
			}
			set[handlerID] = true
		}
		r.forEachRelationship(func(rel *graph.Relationship) bool {
			switch rel.Kind {
			case "PUBLISHES_TO":
				pubCount[rel.ToID]++
			case "SUBSCRIBES_TO":
				addConsumer(rel.ToID, rel.FromID) // topic=ToID, handler=FromID
			case "DELIVERS_TO":
				addConsumer(rel.FromID, rel.ToID) // topic=FromID, handler=ToID
			}
			return true
		})
		r.forEachEntity(func(e *graph.Entity) bool {
			if !isTopic(e) {
				return true
			}
			out = append(out, channel{
				TopicID:        prefixedID(r.Repo, e.ID),
				TopicName:      e.Name,
				Repo:           r.Repo,
				PublisherCount: pubCount[e.ID],
				ConsumerCount:  len(consumers[e.ID]),
				SourceFile:     e.SourceFile,
			})
			return true
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].TopicName != out[j].TopicName {
			return out[i].TopicName < out[j].TopicName
		}
		return out[i].Repo < out[j].Repo
	})
	total := len(out)
	truncated := false
	if len(out) > topologyChannelsMax {
		out = out[:topologyChannelsMax]
		truncated = true
	}
	resp := map[string]any{
		"channels":  out,
		"count":     len(out),
		"total":     total,
		"truncated": truncated,
	}
	if truncated {
		resp["truncation_note"] = "channel listing capped at " +
			strconv.Itoa(topologyChannelsMax) + "; narrow with repo_filter to see the rest"
	}
	return jsonResult(resp), nil
}

// ---------------------------------------------------------------------------
// grafel_flows — action-dispatch bundle (#1281)
// Replaces: flow_dead_ends, flow_truncated, flow_detail
// ---------------------------------------------------------------------------

// handleFlows dispatches on action= to the appropriate flow handler.
func (s *Server) handleFlows(ctx context.Context, req mcpapi.CallToolRequest) (*mcpapi.CallToolResult, error) {
	action, err := req.RequireString("action")
	if err != nil {
		return mcpapi.NewToolResultError(err.Error()), nil
	}
	switch action {
	case "dead_ends":
		return s.handleFlowDeadEnds(ctx, req)
	case "truncated":
		return s.handleFlowTruncated(ctx, req)
	case "detail":
		return s.handleFlowDetail(ctx, req)
	default:
		return mcpapi.NewToolResultError(
			"unknown action " + action + " (allowed: dead_ends, truncated, detail)",
		), nil
	}
}

// ---------------------------------------------------------------------------
// grafel_graph_patterns — action-dispatch bundle (#1281)
// Replaces: patterns_list, patterns_get (renamed to disambiguate from
// grafel_patterns agent-learned store)
// ---------------------------------------------------------------------------

// handleGraphPatterns dispatches on action= to the appropriate graph-indexed
// pattern handler.
func (s *Server) handleGraphPatterns(ctx context.Context, req mcpapi.CallToolRequest) (*mcpapi.CallToolResult, error) {
	action, err := req.RequireString("action")
	if err != nil {
		return mcpapi.NewToolResultError(err.Error()), nil
	}
	switch action {
	case "list":
		return s.handlePatternsListGraph(ctx, req)
	case "get":
		return s.handlePatternsGetGraph(ctx, req)
	default:
		return mcpapi.NewToolResultError(
			"unknown action " + action + " (allowed: list, get)",
		), nil
	}
}

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
		r.forEachRelationship(func(rel *graph.Relationship) bool {
			if rel.Kind == "SUBSCRIBES_TO" {
				subscribers[rel.ToID] = true
			}
			return true
		})
		r.forEachEntity(func(e *graph.Entity) bool {
			if !isTopic(e) {
				return true
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
			return true
		})
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
		r.forEachRelationship(func(rel *graph.Relationship) bool {
			if rel.Kind == "PUBLISHES_TO" {
				publishers[rel.ToID] = true
			}
			return true
		})
		r.forEachEntity(func(e *graph.Entity) bool {
			if !isTopic(e) {
				return true
			}
			if !publishers[e.ID] && hasRelationshipTo(r.Doc, e.ID, "SUBSCRIBES_TO") {
				out = append(out, item{
					TopicID:    prefixedID(r.Repo, e.ID),
					TopicName:  e.Name,
					Repo:       r.Repo,
					SourceFile: e.SourceFile,
				})
			}
			return true
		})
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
	verbose := argBool(req, "verbose", false)
	repos := reposToConsider(lg, nil)

	// #1703: use alias-aware repo resolution so slugs with dash/underscore
	// variants (e.g. "acme-core" vs "acme_core") still narrow to the
	// correct repo.  Fall back to all repos when the hint is unrecognised —
	// the per-repo loop below will find the entity via LabelIndex.
	repoHint, local := splitPrefixed(topicID)
	if repoHint != "" {
		aliases := buildRepoAliasMap(lg)
		if _, r := lookupRepo(aliases, repoHint); r != nil && r.Doc != nil {
			repos = []*LoadedRepo{r}
		}
	}

	// Default (verbose=false): entity_id, entity_name, kind — no source_file.
	// Verbose (verbose=true): also includes source_file, repo.
	type participant struct {
		EntityID   string `json:"entity_id"`
		EntityName string `json:"entity_name"`
		Kind       string `json:"kind"`
		Repo       string `json:"repo,omitempty"`
		SourceFile string `json:"source_file,omitempty"`
	}

	for _, r := range repos {
		if r.Doc == nil {
			continue
		}
		// Determine the lookup key within this repo.  Try in order:
		//   1. local part of a "repo::id" prefixed input (exact hash ID)
		//   2. the full topicID when no prefix was present
		// Then fall back to LabelIndex for name/qualified-name inputs so that
		// the id returned by search_entities (which matches by name) round-trips
		// successfully. (#1703)
		lookup := local
		if lookup == "" {
			lookup = topicID
		}
		byID := r.getByID()
		topicEnt, ok := byID[lookup]
		if !ok {
			// Fall back: resolve by entity name or qualified name.
			topicEnt = r.LabelIndex.Lookup(lookup)
			if topicEnt == nil {
				continue
			}
		}
		// Use the canonical entity ID for relationship matching.
		canonID := topicEnt.ID
		var publishers, subscribers []participant
		r.forEachRelationship(func(rel *graph.Relationship) bool {
			switch rel.Kind {
			case "PUBLISHES_TO":
				if rel.ToID == canonID {
					if src, ok2 := byID[rel.FromID]; ok2 {
						p := participant{
							EntityID:   prefixedID(r.Repo, src.ID),
							EntityName: src.Name,
							Kind:       src.Kind,
						}
						if verbose {
							p.Repo = r.Repo
							p.SourceFile = src.SourceFile
						}
						publishers = append(publishers, p)
					}
				}
			case "SUBSCRIBES_TO":
				if rel.ToID == canonID {
					if src, ok2 := byID[rel.FromID]; ok2 {
						p := participant{
							EntityID:   prefixedID(r.Repo, src.ID),
							EntityName: src.Name,
							Kind:       src.Kind,
						}
						if verbose {
							p.Repo = r.Repo
							p.SourceFile = src.SourceFile
						}
						subscribers = append(subscribers, p)
					}
				}
			}
			return true
		})
		resp := map[string]any{
			"topic_id":    prefixedID(r.Repo, topicEnt.ID),
			"topic_name":  topicEnt.Name,
			"publishers":  publishers,
			"subscribers": subscribers,
			"found":       true,
		}
		if verbose {
			resp["repo"] = r.Repo
			resp["source_file"] = topicEnt.SourceFile
		}
		return jsonResult(resp), nil
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
		ProcessID   string `json:"process_id"`
		ProcessName string `json:"process_name"`
		Repo        string `json:"repo"`
		DeadEndID   string `json:"dead_end_step_id"`
		DeadEndName string `json:"dead_end_step_name"`
	}

	var out []item
	for _, r := range repos {
		if r.Doc == nil {
			continue
		}
		byID := r.getByID()
		// Build outbound CALLS set.
		hasOutCalls := map[string]bool{}
		r.forEachRelationship(func(rel *graph.Relationship) bool {
			if rel.Kind == "CALLS" {
				hasOutCalls[rel.FromID] = true
			}
			return true
		})
		r.forEachEntity(func(e *graph.Entity) bool {
			if e.Kind != processEntityKind {
				return true
			}
			// Check if terminal_id exists and has outbound CALLS.
			termID := e.PropGet("terminal_id")
			if termID == "" {
				return true
			}
			if !hasOutCalls[termID] {
				termEnt := byID[termID]
				termName := termID
				if termEnt != nil {
					termName = termEnt.Name
				}
				// Only flag if terminal is not explicitly marked as an end node.
				if termEnt == nil || termEnt.PropGet("is_terminal") != "true" {
					out = append(out, item{
						ProcessID:   prefixedID(r.Repo, e.ID),
						ProcessName: e.Name,
						Repo:        r.Repo,
						DeadEndID:   prefixedID(r.Repo, termID),
						DeadEndName: termName,
					})
				}
			}
			return true
		})
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
		r.forEachEntity(func(e *graph.Entity) bool {
			if e.Kind != processEntityKind {
				return true
			}
			truncated := e.PropGet("truncated")
			reason := e.PropGet("truncated_reason")
			if truncated == "true" || reason != "" {
				sc := 0
				if scStr := e.PropGet("step_count"); scStr != "" {
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
			return true
		})
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
		byID := r.getByID()
		var procEnt *graph.Entity
		r.forEachEntity(func(e *graph.Entity) bool {
			if e.Kind == processEntityKind && e.ID == target {
				procEnt = e
				return false
			}
			return true
		})
		if procEnt == nil {
			continue
		}
		steps := buildProcessSteps(r, procEnt)
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
		r.forEachRelationship(func(rel *graph.Relationship) bool {
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
			return true
		})
		return jsonResult(map[string]any{
			"process_id":   prefixedID(r.Repo, procEnt.ID),
			"process_name": procEnt.Name,
			"repo":         r.Repo,
			"cross_stack":  procEnt.PropGet("cross_stack") == "true",
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
// Patterns list / get  (graph-indexed, not agentpatterns store)
// ---------------------------------------------------------------------------

// handlePatternsListGraph returns SCOPE.Pattern entities from the loaded
// graph. This is distinct from grafel_patterns (which uses the
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
		r.forEachEntity(func(e *graph.Entity) bool {
			if e.Kind != "SCOPE.Pattern" && e.Kind != "Pattern" {
				return true
			}
			status := e.PropGet("status")
			if statusFilter != "" && !strings.EqualFold(status, statusFilter) {
				return true
			}
			if needsAttention && e.PropGet("needs_attention") != "true" {
				return true
			}
			conf := 0.0
			if cs := e.PropGet("confidence"); cs != "" {
				for _, ch := range cs {
					if (ch >= '0' && ch <= '9') || ch == '.' {
						conf = parseSimpleFloat(cs)
						break
					}
				}
			}
			if conf < confidenceMin {
				return true
			}
			out = append(out, item{
				PatternID:  prefixedID(r.Repo, e.ID),
				Name:       e.Name,
				Repo:       r.Repo,
				Status:     status,
				Confidence: conf,
				SourceFile: e.SourceFile,
			})
			return true
		})
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
		byID := r.getByID()
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
		r.forEachRelationship(func(rel *graph.Relationship) bool {
			if (rel.Kind == "EXEMPLIFIES" || rel.Kind == "INSTANCE_OF") && rel.ToID == target {
				if ex := byID[rel.FromID]; ex != nil {
					exemplars = append(exemplars, exemplar{
						EntityID:   prefixedID(r.Repo, ex.ID),
						EntityName: ex.Name,
						Kind:       ex.Kind,
					})
				}
			}
			return true
		})
		return jsonResult(map[string]any{
			"pattern_id":  prefixedID(r.Repo, e.ID),
			"name":        e.Name,
			"repo":        r.Repo,
			"source_file": e.SourceFile,
			"properties":  e.PropsSnapshot(),
			"tags":        e.Tags,
			"exemplars":   exemplars,
			"found":       true,
		}), nil
	}
	return jsonResult(map[string]any{"found": false, "pattern_id": patternID}), nil
}

// ---------------------------------------------------------------------------
// Bonus tools: search_entities, find_paths
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
	includeNoise := argBool(req, "include_noise", false)
	repos := reposToConsider(lg, argStringSlice(req, "repo_filter"))
	minConfidence := argMinConfidence(req) // #2769 Phase 1C
	ql := strings.ToLower(query)
	// #2828: optional terse output + token budget for this high-volume tool
	// (248 calls in live telemetry). format="terse" emits one compact line per
	// hit under `lines` (id, name, kind, file:line) instead of the per-record
	// object map; the default shape (`results` array) is unchanged for callers
	// that machine-parse fields. token_budget caps the returned list bytes.
	terse := strings.EqualFold(argString(req, "format", ""), "terse")
	tokenBudget := argInt(req, "token_budget", 0)

	type item struct {
		EntityID      string `json:"entity_id"`
		EntityName    string `json:"name"`
		Kind          string `json:"kind"`
		Repo          string `json:"repo"`
		QualifiedName string `json:"qualified_name,omitempty"`
		SourceFile    string `json:"source_file,omitempty"`
		StartLine     int    `json:"start_line,omitempty"`
		nameHit       bool   // internal: entity name/qualified-name matched the query substring
	}

	// #5781: when kind_filter is set, this tool ENUMERATES the kind (parity with
	// the bm25 find path's enumerateByKind) instead of requiring a name-substring
	// match — a kind-only intent whose query doesn't literally appear in any name
	// (e.g. "kafka topic" vs "kafka:feedback-topic") must still return the topics.
	// Name/qualified-name substring matches are kept and ranked FIRST; the rest of
	// the in-scope kind members are folded in after. With NO kind_filter the
	// behaviour is unchanged: name-substring matching only.
	enumerateKind := kindFilter != ""

	var out []item
	for _, r := range repos {
		if r.Doc == nil {
			continue
		}
		r.forEachEntity(func(e *graph.Entity) bool {
			// matchesKindFilter expands alias kinds (e.g. "http_endpoint" →
			// [http_endpoint, http_endpoint_definition, http_endpoint_call]).
			if !matchesKindFilter(e, kindFilter) {
				return true
			}
			// De-noise (#1712): Schema field members (SCOPE.Schema/field entities)
			// are suppressed from default results — ~25 fields per serializer
			// class clutter ranked output. Pass include_noise:true to recover them.
			if !includeNoise && classifyNoise(e) == noiseSchemaField {
				return true
			}
			// #2769 Phase 1C: drop entities below the caller's confidence floor.
			if !entityPassesConfidence(e, minConfidence) {
				return true
			}
			nameL := strings.ToLower(e.Name)
			qnL := strings.ToLower(e.QualifiedName)
			nameHit := strings.Contains(nameL, ql) || strings.Contains(qnL, ql)
			// Without a kind_filter, keep name-only matching. With a kind_filter,
			// fold in every in-scope kind member (nameHit just drives ranking).
			if !nameHit && !enumerateKind {
				return true
			}
			out = append(out, item{
				EntityID:      prefixedID(r.Repo, e.ID),
				EntityName:    e.Name,
				Kind:          e.Kind,
				Repo:          r.Repo,
				QualifiedName: e.QualifiedName,
				SourceFile:    e.SourceFile,
				StartLine:     e.StartLine,
				nameHit:       nameHit,
			})
			return true
		})
	}
	// Sort: name-substring hits first (kind enumeration only), then exact-name
	// matches, then alphabetical. When kind_filter is empty every row is a
	// name-hit, so the nameHit tier is a no-op and ordering is unchanged.
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].nameHit != out[j].nameHit {
			return out[i].nameHit
		}
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
	if tokenBudget > 0 {
		out = capByRenderedBytes(out, tokenBudget*4, terse)
	}
	resp := map[string]any{
		"count":     len(out),
		"total":     total,
		"truncated": total > len(out),
	}
	if terse {
		lines := make([]string, 0, len(out))
		for _, it := range out {
			var b strings.Builder
			b.WriteString(it.EntityID)
			b.WriteString("  ")
			b.WriteString(it.EntityName)
			b.WriteString("  ")
			b.WriteString(stripScopePrefix(it.Kind))
			if it.SourceFile != "" {
				b.WriteString("  ")
				b.WriteString(it.SourceFile)
				if it.StartLine > 0 {
					b.WriteByte(':')
					b.WriteString(strconv.Itoa(it.StartLine))
				}
			}
			lines = append(lines, b.String())
		}
		resp["format"] = "terse"
		resp["lines"] = lines
	} else {
		resp["results"] = out
	}
	return jsonResult(resp), nil
}

// reverseTraversalEdgeKinds is the set of edge kinds where the BFS in
// find_paths should ALSO follow the reverse direction. These are the
// "interface-style" edges where (handler --IMPLEMENTS--> endpoint) is
// emitted from the implementation side but a cross-repo link lands on
// the contract/endpoint node — to reach the handler, BFS must walk the
// IMPLEMENTS edge in reverse. (#1690)
var reverseTraversalEdgeKinds = map[string]bool{
	"IMPLEMENTS":      true,
	"GRPC_IMPLEMENTS": true,
	"HANDLES":         true,
	"GRPC_HANDLES":    true,
}

// buildRepoAliasMap returns a map of every recognised repo-prefix to the
// canonical *LoadedRepo. A loaded repo can be addressed by:
//
//   - its registry slug (the key in lg.Repos)
//   - the `repo` field embedded in its graph document
//
// These can DIVERGE when the on-disk repo directory uses underscores but
// the fleet config lists the slug with dashes (or vice versa). The
// cross-repo links file is written with the graph-encoded repo name, but
// MCP resolves IDs through the registry slug — so without aliasing,
// link.Target points at a repo prefix that lg.Repos doesn't contain and
// BFS expansion stops at the cross-repo hop. (#1690)
func buildRepoAliasMap(lg *LoadedGroup) map[string]*LoadedRepo {
	if lg == nil {
		return nil
	}
	aliases := make(map[string]*LoadedRepo, len(lg.Repos)*4)
	register := func(key string, r *LoadedRepo) {
		if key == "" || r == nil {
			return
		}
		if _, exists := aliases[key]; !exists {
			aliases[key] = r
		}
	}
	for slug, r := range lg.Repos {
		register(slug, r)
		if r != nil && r.Doc != nil && r.Doc.Repo != "" {
			register(r.Doc.Repo, r)
		}
		// Path basename — the links generator has historically derived the
		// `repo` field of cross-repo Source/Target prefixes from the on-disk
		// directory name, which uses underscores where the fleet slug uses
		// dashes (e.g. /Projects/Acme/acme_core vs slug acme-core). When
		// the links file was written under that older convention but the
		// current graph.fb is tagged with the new slug, neither slug nor
		// doc.Repo match the link's prefix. Register the directory basename
		// AND its dash/underscore swap so the prefix still resolves. (#1690)
		if r != nil && r.Path != "" {
			base := filepath.Base(r.Path)
			register(base, r)
			register(strings.ReplaceAll(base, "_", "-"), r)
			register(strings.ReplaceAll(base, "-", "_"), r)
		}
		// Defensive: also register the slug/doc.Repo with `_`↔`-` swapped.
		register(strings.ReplaceAll(slug, "-", "_"), r)
		register(strings.ReplaceAll(slug, "_", "-"), r)
		if r != nil && r.Doc != nil && r.Doc.Repo != "" {
			register(strings.ReplaceAll(r.Doc.Repo, "-", "_"), r)
			register(strings.ReplaceAll(r.Doc.Repo, "_", "-"), r)
		}
	}
	return aliases
}

// lookupRepo resolves a repo prefix (either the registry slug or the
// graph-encoded `Doc.Repo` name) to its LoadedRepo. Returns the canonical
// slug + repo, or ("", nil) when nothing matches. (#1690)
func lookupRepo(aliases map[string]*LoadedRepo, prefix string) (string, *LoadedRepo) {
	if r, ok := aliases[prefix]; ok && r != nil {
		return r.Repo, r
	}
	return "", nil
}

// handleFindPaths finds the shortest path between two entities up to max_hops.
// #1650: traverses cross-repo edges via lg.Links, so an id in repo A can reach
// an id in repo B when a link connects the two graphs.
// #1690: aliases path-derived and slug-derived repo prefixes so links written
// as `<doc.Repo>::<id>` resolve to the registry-loaded repo even when the
// fleet slug uses dashes vs underscores; also walks IMPLEMENTS-style edges
// in reverse so an endpoint cross-repo target can reach its handler.
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

	aliases := buildRepoAliasMap(lg)

	// canonicalize normalises a "<repo>::<id>" prefix so its repo segment
	// always matches a key in lg.Repos. Bare ids / labels fall through to
	// normalizePrefixed.
	canonicalize := func(s string) string {
		if rPref, lid := splitPrefixed(s); rPref != "" {
			if slug, r := lookupRepo(aliases, rPref); r != nil {
				if r.LabelIndex.HasID(lid) {
					return prefixedID(slug, lid)
				}
				// id may be a label/qname within this repo
				if e := r.LabelIndex.Lookup(lid); e != nil {
					return prefixedID(slug, e.ID)
				}
				return ""
			}
			return ""
		}
		return normalizePrefixed(lg, s)
	}

	// Resolve both endpoints to PREFIXED ids. Accept either a "<repo>::<id>"
	// string (under either the registry slug or the graph-encoded repo name)
	// or a bare local id / label that LabelIndex can resolve.
	prefSrc := canonicalize(from)
	prefDst := canonicalize(to)
	if prefSrc == "" {
		return jsonResult(map[string]any{
			"from":  from,
			"to":    to,
			"found": false,
			"error": "'from' entity not found in any loaded repo (try a prefixed id like <repo>::<id>)",
		}), nil
	}
	if prefDst == "" {
		return jsonResult(map[string]any{
			"from":  prefSrc,
			"to":    to,
			"found": false,
			"error": "'to' entity not found in any loaded repo",
		}), nil
	}

	type step struct {
		EntityID string `json:"entity_id"`
		Name     string `json:"name"`
		Kind     string `json:"kind"`
		Repo     string `json:"repo"`
	}

	// canonicalRepoOf returns the canonical slug for a prefixed id, falling
	// back to the literal prefix when no alias matches (so dijkstra still
	// has stable node ids to compare).
	canonicalRepoOf := func(node string) (string, string, *LoadedRepo) {
		rPref, lid := splitPrefixed(node)
		if rPref == "" {
			return "", node, nil
		}
		slug, r := lookupRepo(aliases, rPref)
		if r == nil {
			return rPref, lid, nil
		}
		return slug, lid, r
	}

	// Expand function: intra-repo CALLS/IMPORTS/etc. + cross-repo overlay.
	// The expansion also walks IMPLEMENTS-class edges in reverse so that a
	// cross-repo link landing on an http_endpoint_definition can reach the
	// implementing handler. (#1690)
	expand := func(node string) []edge {
		slug, local, r := canonicalRepoOf(node)
		out := []edge{}
		if r != nil && r.Doc != nil {
			a := r.getAdjacency()
			for _, e := range a.Outgoing(local) {
				out = append(out, edge{
					target: prefixedID(slug, e.target),
					kind:   e.kind,
					weight: 1,  // unit cost — we want shortest hop count
					relIdx: -1, // synthetic (re-prefixed) — no direct Relationship backing (#2305)
				})
			}
			// Reverse traversal for interface-style edges (#1690).
			for _, e := range a.Incoming(local) {
				if !reverseTraversalEdgeKinds[e.kind] {
					continue
				}
				out = append(out, edge{
					target: prefixedID(slug, e.target),
					kind:   e.kind + "_REVERSED",
					weight: 1,
					relIdx: -1, // synthetic reversed edge — no backing Relationship (#2305)
				})
			}
		}
		// Cross-repo overlay: a link's source-side matches the current node
		// under either the registry slug or the graph-encoded repo prefix.
		// Re-canonicalise the link Target so dijkstra sees the slug used in
		// lg.Repos — without this, BFS would store the target under the
		// graph-encoded prefix and fail to expand further. (#1690)
		for _, l := range lg.Links {
			lSrcSlug, lSrcID := splitPrefixed(l.Source)
			if lSrcSlug == "" {
				continue
			}
			cSrcSlug, _ := lookupRepo(aliases, lSrcSlug)
			if cSrcSlug == "" {
				cSrcSlug = lSrcSlug
			}
			if prefixedID(cSrcSlug, lSrcID) != node {
				continue
			}
			lTgtSlug, lTgtID := splitPrefixed(l.Target)
			cTgtSlug, _ := lookupRepo(aliases, lTgtSlug)
			if cTgtSlug == "" {
				cTgtSlug = lTgtSlug
			}
			out = append(out, edge{
				target: prefixedID(cTgtSlug, lTgtID),
				kind:   l.EffectiveKind(),
				weight: 1,
				relIdx: -1, // synthetic cross-repo overlay — no backing Relationship (#2305)
			})
		}
		return out
	}

	path, kinds, _, found := dijkstra(prefSrc, prefDst, expand)
	if !found {
		return jsonResult(map[string]any{
			"from":  prefSrc,
			"to":    prefDst,
			"paths": []any{},
			"found": false,
			"note":  "no path within max_hops; entities may not be connected even via cross-repo links",
		}), nil
	}

	steps := make([]step, 0, len(path))
	crosses := false
	firstRepo, _ := splitPrefixed(prefSrc)
	for _, pid := range path {
		slug, local, r := canonicalRepoOf(pid)
		if slug != firstRepo {
			crosses = true
		}
		st := step{EntityID: pid, Repo: slug}
		if r != nil && r.Doc != nil {
			if e := r.LabelIndex.ByID(local); e != nil {
				st.Name = e.Name
				st.Kind = e.Kind
			}
		}
		steps = append(steps, st)
	}
	_ = kinds
	return jsonResult(map[string]any{
		"from":          prefSrc,
		"to":            prefDst,
		"max_hops":      maxHops,
		"hop_count":     len(path) - 1,
		"crosses_repos": crosses,
		"steps":         steps,
		"edge_kinds":    kinds,
		"found":         true,
	}), nil
}

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

// isTopic returns true if the entity is a messaging topic/channel.
//
// #5781: Kafka topics are extracted as kind "SCOPE.MessageTopic" (leaf
// "messagetopic"), which ends in "messagetopic" — NOT ".topic" — so the old
// suffix set silently excluded every message-topic entity from the topology
// scans. dead_code.go (frameworkEntryKindsMCP) and denoise.go
// (isStructuralLineless) already treat SCOPE.MessageTopic / messagetopic as
// topic-like; this restores consistency with them.
func isTopic(e *graph.Entity) bool {
	k := strings.ToLower(e.Kind)
	return k == "topic" || k == "scope.topic" || k == "queue" || k == "scope.queue" ||
		k == "messagetopic" || k == "scope.messagetopic" ||
		strings.HasSuffix(k, ".topic") || strings.HasSuffix(k, ".queue") ||
		strings.HasSuffix(k, ".messagetopic")
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
