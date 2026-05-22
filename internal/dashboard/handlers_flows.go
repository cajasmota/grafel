package dashboard

// handlers_flows.go — Process Flow Explorer endpoints
//
//	GET /api/flows/{group}?entry=&cross_stack_only=&limit=
//	GET /api/flows/{group}/{processId}

import (
	"bufio"
	"fmt"
	"net/http"
	"net/url"
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
	// defaultFlowMinSteps mirrors engine.DefaultFlowMinSteps (#1639): the
	// minimum step_count for a flow to appear in the default flows list.
	// Shorter flows are excluded from the default view but remain queryable
	// via the min_steps query parameter. Duplicated as a literal to avoid an
	// internal/engine import in the dashboard package.
	defaultFlowMinSteps = 4
)

// flowMinStepsFromQuery parses the optional ?min_steps= override. Returns
// defaultFlowMinSteps when absent. A value of 0 (or negative) disables the
// short-flow filter entirely so every emitted flow is returned.
func flowMinStepsFromQuery(q url.Values) int {
	v := q.Get("min_steps")
	if v == "" {
		return defaultFlowMinSteps
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 {
		return defaultFlowMinSteps
	}
	return n
}

// ─────────────────────────────────────────────────────────────────────────────
// Entry-kind classification
// ─────────────────────────────────────────────────────────────────────────────

// EntryKindGroup is a summary row in the top-level entry_kind_groups list.
type EntryKindGroup struct {
	Kind  string `json:"kind"`
	Count int    `json:"count"`
}

// inferEntryKind derives the entry_kind label for a Process entity by looking
// up its entry entity (via the entry_id property) within the group's repos.
//
// Classification precedence (first match wins):
//  1. Entry entity kind contains Handler|Route|Controller|View → "http_handler"
//  2. Entry entity kind contains Component                     → "component_render"
//  3. Entry entity kind contains ScheduledJob|Task             → "scheduled_task"
//  4. Entry entity kind contains Test|Spec                     → "test"
//  5. Entry entity kind contains CLI|Command|Main              → "cli_command"
//  6. Any incoming SUBSCRIBES_TO or READS_FROM edge on entry   → "message_consumer"
//  7. Fallback                                                  → "function"
func inferEntryKind(grp *DashGroup, entryID string) string {
	if entryID == "" {
		return "function"
	}
	// Resolve entry entity — may be bare or prefixed.
	_, entEnt := findEntity(grp, entryID)
	if entEnt == nil {
		return "function"
	}

	k := entEnt.Kind
	// Strip leading SCOPE. prefix for matching.
	if after, ok := strings.CutPrefix(k, "SCOPE."); ok {
		k = after
	}

	// Collect incoming edge kinds for this entry across all repos.
	inEdgeKinds := map[string]bool{}
	for _, r := range sortedRepos(grp) {
		for _, rel := range r.Doc.Relationships {
			if rel.ToID == entryID || dashPrefixedID(r.Slug, rel.ToID) == entryID {
				inEdgeKinds[rel.Kind] = true
			}
		}
	}

	// Delegate classification to the lower-level helper.
	result := inferEntryKindFromKind(k, inEdgeKinds)

	// inferEntryKindFromKind uses "component" but the #1148 spec uses
	// "component_render" for Component kinds — preserve that distinction.
	if result == "component" {
		return "component_render"
	}
	// inferEntryKindFromKind has no "cli_command" branch; keep it here.
	for _, sub := range []string{"CLI", "Command", "Main", "Entrypoint"} {
		if strings.Contains(k, sub) {
			return "cli_command"
		}
	}

	return result
}

// entryModuleFromPath extracts a short module label from a file path.
// e.g. "apps/api/handlers/inspections.py" → "inspections"
func entryModuleFromPath(p string) string {
	if p == "" {
		return ""
	}
	base := filepath.Base(p)
	// Strip extension.
	if idx := strings.LastIndex(base, "."); idx > 0 {
		base = base[:idx]
	}
	return base
}

// priorityHint returns a string priority that surfaces user-facing flows first.
// http_handler → "high", message_consumer / scheduled_task → "medium",
// component_render / cli_command / test → "low", function → "low".
func priorityHint(entryKind string) string {
	switch entryKind {
	case "http_handler":
		return "high"
	case "message_consumer", "scheduled_task":
		return "medium"
	default:
		return "low"
	}
}

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
	minSteps := flowMinStepsFromQuery(q)

	grp, err := s.graphs.GetGroup(group)
	if err != nil {
		writeErr(w, http.StatusNotFound, err.Error())
		return
	}

	type ProcessItem struct {
		ProcessID        string                 `json:"process_id"`
		Repo             string                 `json:"repo"`
		Label            string                 `json:"label"`
		EntryID          string                 `json:"entry_id"`
		EntryName        string                 `json:"entry_name"`
		TerminalID       string                 `json:"terminal_id"`
		StepCount        int                    `json:"step_count"`
		CrossStack       bool                   `json:"cross_stack"`
		ChainLabels      []string               `json:"chain_labels"`
		SourceFile       string                 `json:"source_file,omitempty"`
		// Entry-kind grouping metadata (#1148).
		EntryKind        string                 `json:"entry_kind"`
		EntryModule      string                 `json:"entry_module,omitempty"`
		PriorityHint     string                 `json:"priority_hint"`
		DominantStepKind interface{}            `json:"dominant_step_kind"` // null until #1147 lands
		// Enrichment fields (from YAML frontmatter, if a doc file exists).
		DocsSummary      string                 `json:"docs_summary,omitempty"`
		Group            string                 `json:"group,omitempty"`
		GroupLabel       string                 `json:"group_label,omitempty"`
		Rank             float64                `json:"rank,omitempty"`
		Gaps             []string               `json:"gaps,omitempty"`
		Disqualified     bool                   `json:"disqualified,omitempty"`
		Enrichment       *EnrichmentFrontmatter `json:"enrichment,omitempty"`
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
			// #1639 — exclude trivial short flows from the default list.
			// Cross-repo flows are exempt: a short chain that genuinely spans
			// repos is meaningful even at 2-3 steps (the cross-repo bridge is
			// the load-bearing signal).
			if sc < minSteps && !cs {
				continue
			}
			entID := e.Properties["entry_id"]
			ek := inferEntryKind(grp, entID)
			item := ProcessItem{
				ProcessID:        pid,
				Repo:             r.Slug,
				Label:            e.Name,
				EntryID:          entID,
				EntryName:        e.Properties["entry_name"],
				TerminalID:       e.Properties["terminal_id"],
				StepCount:        sc,
				CrossStack:       cs,
				ChainLabels:      splitChainLabels(e.Properties["chain_labels"]),
				SourceFile:       e.SourceFile,
				EntryKind:        ek,
				EntryModule:      entryModuleFromPath(e.SourceFile),
				PriorityHint:     priorityHint(ek),
				DominantStepKind: nil, // stubbed until #1147 lands
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

	// --- LLM enrichment operations (#1103) -----------------------------------
	// merge   → drop process items whose merged_into points at another item in
	//           the same surface; record aliases on the canonical item.
	// disqualify → split items into the "rejected_processes" bucket.
	// rank    → stable-sort kept items so explicit rank wins ties.
	// group   → emit process_groups summary keyed by enrichment.group.
	ops := LoadEnrichmentOpsForGroup(group, docgenState)
	present := map[string]bool{}
	for _, it := range items {
		present[it.ProcessID] = true
	}
	kept := make([]ProcessItem, 0, len(items))
	rejected := make([]ProcessItem, 0)
	aliasesByCanonical := map[string][]string{}
	for _, it := range items {
		if it.Disqualified || ops.IsDisqualified(it.ProcessID) {
			it.Disqualified = true
			rejected = append(rejected, it)
			continue
		}
		if dst, merged := ops.MergedInto[it.ProcessID]; merged && dst != it.ProcessID && present[dst] {
			aliasesByCanonical[dst] = append(aliasesByCanonical[dst], it.ProcessID)
			continue
		}
		kept = append(kept, it)
	}
	if len(ops.Ranks) > 0 {
		sort.SliceStable(kept, func(i, j int) bool {
			return ops.Rank(kept[i].ProcessID) > ops.Rank(kept[j].ProcessID)
		})
	}
	if len(kept) > limit {
		kept = kept[:limit]
	}

	// Build entry_kind_groups summary (sorted by count descending).
	kindCounts := map[string]int{}
	for _, it := range kept {
		kindCounts[it.EntryKind]++
	}
	entryKindGroups := make([]EntryKindGroup, 0, len(kindCounts))
	for k, v := range kindCounts {
		entryKindGroups = append(entryKindGroups, EntryKindGroup{Kind: k, Count: v})
	}
	sort.Slice(entryKindGroups, func(i, j int) bool {
		if entryKindGroups[i].Count != entryKindGroups[j].Count {
			return entryKindGroups[i].Count > entryKindGroups[j].Count
		}
		return entryKindGroups[i].Kind < entryKindGroups[j].Kind
	})

	// Process groups (#1103) — from enrichment frontmatter.
	keptIDs := make([]string, 0, len(kept))
	for _, it := range kept {
		keptIDs = append(keptIDs, it.ProcessID)
	}
	processGroups := ops.SummarizeGroups(keptIDs)

	writeJSON(w, http.StatusOK, map[string]any{
		"processes":          kept,
		"count":              len(kept),
		"entry_kind_groups":  entryKindGroups,
		"rejected_processes": rejected,
		"process_groups":     processGroups,
		"aliases":            aliasesByCanonical,
	})
}

// docgenStatus computes 'enriched' | 'pending' | 'stale' for a flow entity.
//
//   - enriched: a doc file with process_flow frontmatter was found and parsed.
//   - stale:    a doc file exists but its frontmatter has no kind/summary (legacy).
//   - pending:  no doc file found for this entity.
func docgenStatus(fm *EnrichmentFrontmatter, fallback string) string {
	if fm != nil && fm.HasData() {
		return "enriched"
	}
	if fallback != "" {
		// A doc file exists but lacks structured frontmatter.
		return "stale"
	}
	return "pending"
}

// enrichmentHealth returns a map of frontmatter field names → bool indicating
// whether the field is populated for a process_flow entity.
// Callers can surface this in the UI to show which fields are missing.
func enrichmentHealth(fm *EnrichmentFrontmatter) map[string]bool {
	if fm == nil {
		return map[string]bool{
			"summary":          false,
			"preconditions":    false,
			"expected_outcome": false,
			"steps":            false,
			"gaps":             false,
		}
	}
	return map[string]bool{
		"summary":          fm.Summary != "",
		"preconditions":    fm.Preconditions != "",
		"expected_outcome": fm.ExpectedOutcome != "",
		"steps":            len(fm.Steps) > 0,
		"gaps":             len(fm.Gaps) > 0,
	}
}

// handleTriggerEnrichment — POST /api/flows/{group}/{processId}/trigger-enrichment
//
// Enqueues an enrichment job for a specific flow entity. The job is dispatched
// to the internal job queue (#1244); the actual agent invocation is stubbed
// (logs "would invoke agent") until real MCP wiring lands in a follow-up.
//
// Returns 202 with the job record so the frontend can poll status.
func (s *Server) handleTriggerEnrichment(w http.ResponseWriter, r *http.Request) {
	group := r.PathValue("group")
	processID := r.PathValue("processId")
	if group == "" || processID == "" {
		writeErr(w, http.StatusBadRequest, "group and processId required")
		return
	}

	if s.jobQueue == nil {
		// Graceful degradation when the queue is not wired (e.g. tests that
		// don't call SetJobQueue). Return the same 202 shape the stub did.
		writeJSON(w, http.StatusAccepted, map[string]any{
			"status":     "queued",
			"message":    "job queue not initialised; enrichment not dispatched",
			"subject_id": processID,
			"group":      group,
		})
		return
	}

	kind := r.URL.Query().Get("kind")
	if kind == "" {
		kind = "describe_entity"
	}

	id, err := s.jobQueue.Enqueue(group, processID, kind, "")
	if err != nil {
		writeErr(w, http.StatusTooManyRequests, "job queue full: "+err.Error())
		return
	}

	job, _ := s.jobQueue.Get(id)
	writeJSON(w, http.StatusAccepted, jobToWire(job))
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
	process["docgen_status"] = docgenStatus(enrichedFM, enrichedSummary)
	process["enrichment_health"] = enrichmentHealth(enrichedFM)

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
// It searches docgen-state.json GeneratedPaths for a doc file that matches by:
//
//  1. Primary: file path contains the entityID substring.
//  2. Secondary: file path contains "flow" (case-insensitive).
//  3. Tertiary: parsed frontmatter has kind == "process_flow" (catches hashed IDs
//     where the path alone gives no useful signal — mirrors the topology
//     improvement from #1143).
//
// Returns (frontmatter, "") when a structured doc is found.
// Returns (nil, firstLineSummary) when a doc exists but lacks frontmatter.
// Returns (nil, "") when no documentation file is found.
func extractFlowDocs(group, entityID string, docgenState *mcp.DocgenState) (*EnrichmentFrontmatter, string) {
	return extractFlowDocsWithResolver(entityID, docgenState, func(docPath string) string {
		return getDocFilePath(group, docPath)
	})
}

// extractFlowDocsWithResolver is the testable core of extractFlowDocs.
// resolver maps a raw docPath from GeneratedPaths to an absolute file path.
func extractFlowDocsWithResolver(entityID string, docgenState *mcp.DocgenState, resolver func(string) string) (*EnrichmentFrontmatter, string) {
	if docgenState == nil || docgenState.GeneratedPaths == nil {
		return nil, ""
	}

	// Two-pass: first look for entity-id / "flow" path matches (fast path),
	// then fall back to a full scan matching on frontmatter kind (slow path).
	for _, docPath := range docgenState.GeneratedPaths {
		pathLower := strings.ToLower(docPath)
		pathMatch := strings.Contains(docPath, entityID) || strings.Contains(pathLower, "flow")
		if !pathMatch {
			continue
		}
		fullPath := resolver(docPath)
		fm, fallback := extractEnrichmentFromFile(fullPath)
		if fm != nil && fm.HasData() {
			if fm.Kind == "process_flow" || fm.Kind == "" {
				// Exact or untyped match — return immediately.
				return fm, ""
			}
			// Kind is set but not process_flow; don't use this doc.
			continue
		}
		if fallback != "" {
			return nil, fallback
		}
	}

	// Tertiary pass: scan all paths for frontmatter with kind == process_flow
	// whose entity_id field matches (handles hashed IDs).
	for _, docPath := range docgenState.GeneratedPaths {
		fullPath := resolver(docPath)
		fm, _ := extractEnrichmentFromFile(fullPath)
		if fm != nil && fm.Kind == "process_flow" &&
			(fm.EntityID == entityID || strings.Contains(fm.EntityID, entityID) || strings.Contains(entityID, fm.EntityID)) {
			return fm, ""
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
