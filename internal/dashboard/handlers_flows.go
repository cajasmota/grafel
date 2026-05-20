package dashboard

// handlers_flows.go — Process Flow Explorer endpoints
//
//	GET /api/flows/{group}?entry=&cross_stack_only=&limit=
//	GET /api/flows/{group}/{processId}

import (
	"bufio"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/cajasmota/archigraph/internal/mcp"
)

const (
	processEntityKind = "SCOPE.Process"
	stepInProcessEdge = "STEP_IN_PROCESS"
)

// handleFlowsList — GET /api/flows/{group}
func (s *Server) handleFlowsList(w http.ResponseWriter, r *http.Request) {
	group := r.PathValue("group")
	if group == "" {
		writeErr(w, http.StatusBadRequest, "group required")
		return
	}
	q := r.URL.Query()
	crossOnly := q.Get("cross_stack_only") == "true"
	limit := 50
	if v := q.Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	entryFilter := q.Get("entry")

	grp, err := s.graphs.GetGroup(group)
	if err != nil {
		writeErr(w, http.StatusNotFound, err.Error())
		return
	}

	type ProcessItem struct {
		ProcessID   string                 `json:"process_id"`
		Repo        string                 `json:"repo"`
		Label       string                 `json:"label"`
		EntryID     string                 `json:"entry_id"`
		EntryName   string                 `json:"entry_name"`
		TerminalID  string                 `json:"terminal_id"`
		StepCount   int                    `json:"step_count"`
		CrossStack  bool                   `json:"cross_stack"`
		ChainLabels []string               `json:"chain_labels"`
		SourceFile  string                 `json:"source_file,omitempty"`
		// Enrichment fields (from YAML frontmatter, if a doc file exists).
		DocsSummary string                 `json:"docs_summary,omitempty"`
		Group       string                 `json:"group,omitempty"`
		GroupLabel  string                 `json:"group_label,omitempty"`
		Rank        float64                `json:"rank,omitempty"`
		Gaps        []string               `json:"gaps,omitempty"`
		Disqualified bool                  `json:"disqualified,omitempty"`
		Enrichment  *EnrichmentFrontmatter `json:"enrichment,omitempty"`
	}

	// Load docgen state for documentation enrichment.
	docgenState, _ := mcp.LoadDocgenState(group)

	var items []ProcessItem
	for _, r := range sortedRepos(grp) {
		for i := range r.Doc.Entities {
			e := &r.Doc.Entities[i]
			if e.Kind != processEntityKind {
				continue
			}
			cs := e.Properties["cross_stack"] == "true"
			if crossOnly && !cs {
				continue
			}
			pid := dashPrefixedID(r.Slug, e.ID)
			if entryFilter != "" && e.Properties["entry_id"] != entryFilter && pid != entryFilter {
				continue
			}
			sc, _ := strconv.Atoi(e.Properties["step_count"])
			item := ProcessItem{
				ProcessID:   pid,
				Repo:        r.Slug,
				Label:       e.Name,
				EntryID:     e.Properties["entry_id"],
				EntryName:   e.Properties["entry_name"],
				TerminalID:  e.Properties["terminal_id"],
				StepCount:   sc,
				CrossStack:  cs,
				ChainLabels: splitChainLabels(e.Properties["chain_labels"]),
				SourceFile:  e.SourceFile,
			}
			// Enrich from doc frontmatter when available.
			if fm, summary := extractFlowDocs(group, e.ID, docgenState); fm != nil {
				item.DocsSummary = fm.Summary
				item.Group = fm.Group
				item.GroupLabel = fm.GroupLabel
				item.Rank = fm.Rank
				item.Gaps = fm.Gaps
				item.Disqualified = fm.Disqualified
				item.Enrichment = fm
			} else if summary != "" {
				item.DocsSummary = summary
			}
			items = append(items, item)
		}
	}

	sort.Slice(items, func(i, j int) bool {
		if items[i].CrossStack != items[j].CrossStack {
			return items[i].CrossStack
		}
		if items[i].StepCount != items[j].StepCount {
			return items[i].StepCount > items[j].StepCount
		}
		return items[i].Label < items[j].Label
	})
	if len(items) > limit {
		items = items[:limit]
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"processes": items,
		"count":     len(items),
	})
}

// handleFlowDetail — GET /api/flows/{group}/{processId}
//
// Returns the full step chain for one Process entity, with source snippets
// for each step.
func (s *Server) handleFlowDetail(w http.ResponseWriter, r *http.Request) {
	group := r.PathValue("group")
	processID := r.PathValue("processId")
	if group == "" || processID == "" {
		writeErr(w, http.StatusBadRequest, "group and processId required")
		return
	}

	grp, err := s.graphs.GetGroup(group)
	if err != nil {
		writeErr(w, http.StatusNotFound, err.Error())
		return
	}

	// Resolve the process entity.
	repoHint, localID := dashSplitPrefixed(processID)
	var processRepo *DashRepo
	var processEnt *struct {
		ID         string
		Name       string
		Properties map[string]string
		SourceFile string
	}

	for _, r := range sortedRepos(grp) {
		if repoHint != "" && r.Slug != repoHint {
			continue
		}
		for i := range r.Doc.Entities {
			e := &r.Doc.Entities[i]
			if e.Kind != processEntityKind {
				continue
			}
			if e.ID == localID || dashPrefixedID(r.Slug, e.ID) == processID {
				processRepo = r
				processEnt = &struct {
					ID         string
					Name       string
					Properties map[string]string
					SourceFile string
				}{e.ID, e.Name, e.Properties, e.SourceFile}
				break
			}
		}
		if processEnt != nil {
			break
		}
	}

	if processEnt == nil {
		writeErr(w, http.StatusNotFound, "process not found: "+processID)
		return
	}

	// Collect STEP_IN_PROCESS edges (sorted by step_index property).
	// rawStep carries the pre-annotation fields; annotateFlowSteps enriches them.

	// STEP_IN_PROCESS edges are emitted as FromID=processID → ToID=stepEntityID
	// (see engine/process_flow.go). Collect all edges whose FromID matches the
	// process entity, then resolve the step entity from ToID.
	var rawSteps []rawStep
	for _, r := range sortedRepos(grp) {
		for _, rel := range r.Doc.Relationships {
			if rel.Kind != stepInProcessEdge {
				continue
			}
			// FromID is the process entity ID.
			if rel.FromID != processEnt.ID && dashPrefixedID(r.Slug, rel.FromID) != processID {
				continue
			}
			// ToID is the step entity.
			stepIDLocal := rel.ToID
			stepRepo := r
			// Find the step entity.
			for i := range stepRepo.Doc.Entities {
				e := &stepRepo.Doc.Entities[i]
				if e.ID != stepIDLocal {
					continue
				}
				idx, _ := strconv.Atoi(rel.Properties["step_index"])
				rawSteps = append(rawSteps, rawStep{
					EntityID:   dashPrefixedID(stepRepo.Slug, e.ID),
					Label:      e.Name,
					SourceFile: e.SourceFile,
					StartLine:  e.StartLine,
					Repo:       stepRepo.Slug,
					StepIndex:  idx,
					EdgeKind:   rel.Kind,
					EntityKind: e.Kind,
				})
				break
			}
		}
	}

	sort.Slice(rawSteps, func(i, j int) bool { return rawSteps[i].StepIndex < rawSteps[j].StepIndex })

	// Annotate steps with step_kind, per-step side_effects, and derive
	// flow-level metadata (entry_kind, flow_side_effects, complexity_score,
	// is_cross_repo, data_lineage).
	steps, flowMeta := annotateFlowSteps(rawSteps, grp, processRepo.Slug, processEnt.Properties["entry_id"])

	// Collect source snippets for each step (context=5 lines).
	// Response shape is a map of entity_id → source string, matching the
	// FlowDetailResponse.source_snippets: Record<string, string> frontend type.
	snippets := map[string]string{}
	for _, step := range steps {
		rSlug, localID := dashSplitPrefixed(step.EntityID)
		r, ok := grp.Repos[rSlug]
		if !ok || r.Doc == nil {
			continue
		}
		// Find entity for source file info.
		for i := range r.Doc.Entities {
			e := &r.Doc.Entities[i]
			if e.ID != localID {
				continue
			}
			src, _ := readSourceLines(e.SourceFile, r.Path, e.StartLine, e.EndLine, 5)
			if src != "" {
				snippets[step.EntityID] = src
			}
			break
		}
	}

	cs := processEnt.Properties["cross_stack"] == "true"
	sc, _ := strconv.Atoi(processEnt.Properties["step_count"])

	// Load docgen state for enrichment.
	docgenStateDetail, _ := mcp.LoadDocgenState(group)
	enrichedFM, enrichedSummary := extractFlowDocs(group, processEnt.ID, docgenStateDetail)

	process := map[string]any{
		"process_id":        dashPrefixedID(processRepo.Slug, processEnt.ID),
		"repo":              processRepo.Slug,
		"label":             processEnt.Name,
		"entry_id":          processEnt.Properties["entry_id"],
		"entry_name":        processEnt.Properties["entry_name"],
		"terminal_id":       processEnt.Properties["terminal_id"],
		"step_count":        sc,
		"cross_stack":       cs,
		"chain_labels":      splitChainLabels(processEnt.Properties["chain_labels"]),
		"source_file":       processEnt.SourceFile,
		"steps":             steps,
		// Flows v2 annotations.
		"entry_kind":        flowMeta.EntryKind,
		"flow_side_effects": flowMeta.FlowSideEffects,
		"complexity_score":  flowMeta.ComplexityScore,
		"is_cross_repo":     flowMeta.IsCrossRepo,
		"data_lineage":      flowMeta.DataLineage,
	}
	if enrichedFM != nil {
		process["docs_summary"] = enrichedFM.Summary
		process["group"] = enrichedFM.Group
		process["group_label"] = enrichedFM.GroupLabel
		process["rank"] = enrichedFM.Rank
		process["gaps"] = enrichedFM.Gaps
		process["disqualified"] = enrichedFM.Disqualified
		process["enrichment"] = enrichedFM
	} else if enrichedSummary != "" {
		process["docs_summary"] = enrichedSummary
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"process":         process,
		"chain_entities":  steps,
		"source_snippets": snippets,
	})
}

// splitChainLabels splits the comma-separated chain_labels property.
func splitChainLabels(s string) []string {
	if s == "" {
		return []string{}
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// extractFlowDocs looks up enrichment documentation for a process_flow entity.
// It searches docgen-state.json GeneratedPaths for a doc file whose name
// contains the entity id (or "flow"), reads it, and returns the parsed
// EnrichmentFrontmatter (or a fallback first-line summary when frontmatter
// is absent).
//
// Returns (nil, "") when no documentation file is found.
func extractFlowDocs(group, entityID string, docgenState *mcp.DocgenState) (*EnrichmentFrontmatter, string) {
	if docgenState == nil || docgenState.GeneratedPaths == nil {
		return nil, ""
	}
	for _, docPath := range docgenState.GeneratedPaths {
		if !strings.Contains(docPath, entityID) && !strings.Contains(strings.ToLower(docPath), "flow") {
			continue
		}
		fullPath := getDocFilePath(group, docPath)
		fm, fallback := extractEnrichmentFromFile(fullPath)
		if fm != nil && fm.HasData() {
			return fm, ""
		}
		if fallback != "" {
			return nil, fallback
		}
	}
	return nil, ""
}

// readSourceLines reads start..end (+ context lines) from a source file.
// Returns the snippet and any error.
func readSourceLines(sourceFile, repoPath string, startLine, endLine, contextLines int) (string, error) {
	abs := sourceFile
	if !filepath.IsAbs(abs) && repoPath != "" {
		abs = filepath.Join(repoPath, sourceFile)
	}
	f, err := os.Open(abs)
	if err != nil {
		return "", err
	}
	defer f.Close()

	from := startLine - contextLines
	if from < 1 {
		from = 1
	}
	to := endLine + contextLines
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 512*1024), 32*1024*1024)

	var b strings.Builder
	line := 0
	for scanner.Scan() {
		line++
		if line < from {
			continue
		}
		if line > to {
			break
		}
		b.WriteString(fmt.Sprintf("%5d  %s\n", line, scanner.Text()))
	}
	return b.String(), scanner.Err()
}
