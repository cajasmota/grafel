// v2_flows.go — v2 envelope wrappers for the process-flow explorer surface.
//
// These handlers are thin v2-envelope proxies that wrap the same graph-data
// path used by the existing v1 /api/flows/* handlers. The frontend currently
// calls /api/flows/* (v1, request()) directly, so these endpoints are provided
// for future migration and completeness of the v2 surface.
//
// Routes registered in server.go (appended after existing v2 routes):
//
//	GET /api/v2/groups/{group}/flows             → handleV2FlowsList
//	GET /api/v2/groups/{group}/flows/dead-ends   → handleV2FlowDeadEnds
//	GET /api/v2/groups/{group}/flows/truncated   → handleV2FlowTruncated
//
// NOTE: /dead-ends and /truncated are registered BEFORE the wildcard
// /{processId} pattern (not registered here) so Go 1.22 ServeMux picks the
// more-specific path first.
package dashboard

import (
	"net/http"
	"sort"
	"strconv"

	"github.com/cajasmota/archigraph/internal/mcp"
)

// handleV2FlowsList — GET /api/v2/groups/{group}/flows
//
// Returns the process-flow list (with entry-kind groups) wrapped in a v2
// envelope. Mirrors the v1 /api/flows/{group} handler but uses v2OK().
func (s *Server) handleV2FlowsList(w http.ResponseWriter, r *http.Request) {
	group := r.PathValue("group")
	if group == "" {
		writeV2Err(w, http.StatusBadRequest, "bad_request", "group required")
		return
	}
	q := r.URL.Query()
	crossOnly := q.Get("cross_stack_only") == "true"
	minSteps := flowMinStepsFromQuery(q)
	limit := 200
	if v := q.Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}

	grp, err := s.graphs.GetGroup(group)
	if err != nil {
		writeV2Err(w, http.StatusNotFound, "not_found", err.Error())
		return
	}

	docgenState, _ := mcp.LoadDocgenState(group)

	type processItem struct {
		ProcessID         string                 `json:"process_id"`
		Repo              string                 `json:"repo"`
		Label             string                 `json:"label"`
		EntryID           string                 `json:"entry_id"`
		EntryName         string                 `json:"entry_name"`
		EntryKind         string                 `json:"entry_kind"`
		EntryModule       string                 `json:"entry_module,omitempty"`
		TerminalID        string                 `json:"terminal_id"`
		TerminalIsPhantom bool                   `json:"terminal_is_phantom,omitempty"`
		StepCount         int                    `json:"step_count"`
		CrossStack        bool                   `json:"cross_stack"`
		IsCrossRepo       bool                   `json:"is_cross_repo,omitempty"`
		ChainLabels       []string               `json:"chain_labels"`
		SourceFile        string                 `json:"source_file,omitempty"`
		PriorityHint      string                 `json:"priority_hint"`
		DocgenStatus      string                 `json:"docgen_status"`
		Enrichment        *EnrichmentFrontmatter `json:"enrichment,omitempty"`
	}

	var items []processItem
	for _, repo := range sortedRepos(grp) {
		for i := range repo.Doc.Entities {
			e := &repo.Doc.Entities[i]
			if e.Kind != processEntityKind {
				continue
			}
			cs := e.Properties["cross_stack"] == "true"
			if crossOnly && !cs {
				continue
			}
			sc, _ := strconv.Atoi(e.Properties["step_count"])
			// #1639 — exclude trivial short flows from the default list;
			// cross-repo flows are exempt (meaningful even when short).
			if sc < minSteps && !cs {
				continue
			}
			pid := dashPrefixedID(repo.Slug, e.ID)
			entID := e.Properties["entry_id"]
			ek := inferEntryKind(grp, entID)
			fm, summary := extractFlowDocs(group, e.ID, docgenState)
			items = append(items, processItem{
				ProcessID:    pid,
				Repo:         repo.Slug,
				Label:        e.Name,
				EntryID:      entID,
				EntryName:    e.Properties["entry_name"],
				EntryKind:    ek,
				EntryModule:  entryModuleFromPath(e.SourceFile),
				TerminalID:   e.Properties["terminal_id"],
				StepCount:    sc,
				CrossStack:   cs,
				IsCrossRepo:  cs,
				ChainLabels:  splitChainLabels(e.Properties["chain_labels"]),
				SourceFile:   e.SourceFile,
				PriorityHint: priorityHint(ek),
				DocgenStatus: docgenStatus(fm, summary),
				Enrichment:   fm,
			})
		}
	}

	sort.Slice(items, func(i, j int) bool {
		if items[i].StepCount != items[j].StepCount {
			return items[i].StepCount > items[j].StepCount
		}
		return items[i].Label < items[j].Label
	})
	if len(items) > limit {
		items = items[:limit]
	}

	// Build entry-kind group summary.
	kindCounts := map[string]int{}
	for _, it := range items {
		kindCounts[it.EntryKind]++
	}
	type kindGroup struct {
		Kind  string `json:"kind"`
		Count int    `json:"count"`
	}
	entryKindGroups := make([]kindGroup, 0, len(kindCounts))
	for k, v := range kindCounts {
		entryKindGroups = append(entryKindGroups, kindGroup{Kind: k, Count: v})
	}
	sort.Slice(entryKindGroups, func(i, j int) bool {
		if entryKindGroups[i].Count != entryKindGroups[j].Count {
			return entryKindGroups[i].Count > entryKindGroups[j].Count
		}
		return entryKindGroups[i].Kind < entryKindGroups[j].Kind
	})

	writeV2JSON(w, http.StatusOK, v2OK(map[string]any{
		"processes":         items,
		"count":             len(items),
		"entry_kind_groups": entryKindGroups,
	}))
}

// handleV2FlowDeadEnds — GET /api/v2/groups/{group}/flows/dead-ends
//
// Returns dead-end flows wrapped in a v2 envelope.
func (s *Server) handleV2FlowDeadEnds(w http.ResponseWriter, r *http.Request) {
	group := r.PathValue("group")
	if group == "" {
		writeV2Err(w, http.StatusBadRequest, "bad_request", "group required")
		return
	}
	grp, err := s.graphs.GetGroup(group)
	if err != nil {
		writeV2Err(w, http.StatusNotFound, "not_found", err.Error())
		return
	}

	type deadEndItem struct {
		ProcessID       string `json:"process_id"`
		ProcessName     string `json:"process_name"`
		Repo            string `json:"repo"`
		Reason          string `json:"reason"`
		StepCount       int    `json:"step_count"`
		DeadEndStepID   string `json:"dead_end_step_id,omitempty"`
		DeadEndStepName string `json:"dead_end_step_name,omitempty"`
		CrossStack      bool   `json:"cross_stack,omitempty"`
	}

	var items []deadEndItem
	for _, repo := range sortedRepos(grp) {
		for i := range repo.Doc.Entities {
			e := &repo.Doc.Entities[i]
			if e.Kind != processEntityKind {
				continue
			}
			reason := e.Properties["dead_end_reason"]
			if reason == "" {
				continue
			}
			sc, _ := strconv.Atoi(e.Properties["step_count"])
			items = append(items, deadEndItem{
				ProcessID:       dashPrefixedID(repo.Slug, e.ID),
				ProcessName:     e.Name,
				Repo:            repo.Slug,
				Reason:          reason,
				StepCount:       sc,
				DeadEndStepID:   e.Properties["dead_end_step_id"],
				DeadEndStepName: e.Properties["dead_end_step_name"],
				CrossStack:      e.Properties["cross_stack"] == "true",
			})
		}
	}

	writeV2JSON(w, http.StatusOK, v2OK(map[string]any{
		"dead_ends": items,
		"count":     len(items),
	}))
}

// handleV2FlowTruncated — GET /api/v2/groups/{group}/flows/truncated
//
// Returns truncated flows wrapped in a v2 envelope. Currently always empty
// in live data — the positive empty state ("Everything resolves cleanly") is
// the primary state for this tab.
func (s *Server) handleV2FlowTruncated(w http.ResponseWriter, r *http.Request) {
	group := r.PathValue("group")
	if group == "" {
		writeV2Err(w, http.StatusBadRequest, "bad_request", "group required")
		return
	}
	if _, err := s.graphs.GetGroup(group); err != nil {
		writeV2Err(w, http.StatusNotFound, "not_found", err.Error())
		return
	}
	// Truncated flows are always empty in current data; return the same empty
	// slice the v1 handler returns, wrapped in a v2 envelope.
	writeV2JSON(w, http.StatusOK, v2OK(map[string]any{
		"processes": []any{},
		"count":     0,
	}))
}
