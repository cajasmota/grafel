package dashboard

// handlers_repairs.go — Pending queue endpoints for the dashboard (#987).
//
//	GET  /api/repairs/{group}           — repair_edge + dynamic_baseurl_endpoint candidates
//	GET  /api/enrichments/{group}       — all other enrichment candidates (describe_entity, classify_domain, …)
//	POST /api/repairs/{group}/action    — apply or reject a repair candidate (#1016)
//	POST /api/enrichments/{group}/action — apply or reject an enrichment candidate (#1016)

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/cajasmota/archigraph/internal/daemon"
	"github.com/cajasmota/archigraph/internal/enrichment"
)

// repairKinds is the closed set of candidate kinds surfaced on the "Repair
// candidates" tab. Everything else lands on the "Enrichment candidates" tab.
var repairKinds = map[string]bool{
	"repair_edge":                true,
	"dynamic_baseurl_endpoint":   true,
}

// pendingCandidateRow is the wire shape shared by both /api/repairs and
// /api/enrichments. The richer Context map is forwarded as-is so the
// dashboard can display subject / proposed-value without a second round-trip.
type pendingCandidateRow struct {
	CandidateID     string         `json:"candidate_id"`
	Repo            string         `json:"repo"`
	Kind            string         `json:"kind"`
	SubjectID       string         `json:"subject_id"`
	Context         map[string]any `json:"context,omitempty"`
	Hint            string         `json:"hint,omitempty"`
	Confidence      float64        `json:"confidence,omitempty"`
	DiscoveredAt    string         `json:"discovered_at,omitempty"`
	AutoResolvable  bool           `json:"auto_resolvable"`
	// Score is the 0–100 prioritisation score (issue #1131). Present on
	// enrichment candidates; absent (0) on repair candidates.
	Score           int    `json:"score,omitempty"`
	// ScoreBreakdown lists the modifiers that produced Score.
	ScoreBreakdown  string `json:"score_breakdown,omitempty"`
	// CriticalityBand is "critical" / "high" / "medium" / "low".
	CriticalityBand string `json:"criticality_band,omitempty"`
}

// handleRepairs — GET /api/repairs/{group}
//
// Returns repair_edge and dynamic_baseurl_endpoint candidates for every repo in
// the group. These are the structurally ambiguous edges that require an agent
// (or a human) to choose a resolution.
func (s *Server) handleRepairs(w http.ResponseWriter, r *http.Request) {
	group := r.PathValue("group")
	if group == "" {
		writeErr(w, http.StatusBadRequest, "group required")
		return
	}

	grp, err := s.graphs.GetGroup(group)
	if err != nil {
		writeErr(w, http.StatusNotFound, err.Error())
		return
	}

	items := []pendingCandidateRow{}
	autoResolvable := 0

	for slug, repo := range grp.Repos {
		if repo == nil || repo.Path == "" {
			continue
		}
		for _, c := range readAllCandidates(repo.Path) {
			if !repairKinds[c.Kind] {
				continue
			}
			ar := c.Confidence >= 0.85
			if ar {
				autoResolvable++
			}
			items = append(items, pendingCandidateRow{
				CandidateID:    c.ID,
				Repo:           slug,
				Kind:           c.Kind,
				SubjectID:      c.SubjectID,
				Context:        c.Context,
				Hint:           c.Hint,
				Confidence:     c.Confidence,
				DiscoveredAt:   c.DiscoveredAt,
				AutoResolvable: ar,
			})
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"items":                 items,
		"total":                 len(items),
		"open_count":            len(items), // alias for total (tests + clients rely on both)
		"auto_resolvable_count": autoResolvable,
	})
}

// handleEnrichments — GET /api/enrichments/{group}
//
// Returns all non-repair enrichment candidates (describe_entity, classify_domain,
// describe_role, name_community, infer_xlang_call, summarize_api, …) for every
// repo in the group.
func (s *Server) handleEnrichments(w http.ResponseWriter, r *http.Request) {
	group := r.PathValue("group")
	if group == "" {
		writeErr(w, http.StatusBadRequest, "group required")
		return
	}

	grp, err := s.graphs.GetGroup(group)
	if err != nil {
		writeErr(w, http.StatusNotFound, err.Error())
		return
	}

	items := []pendingCandidateRow{}

	for slug, repo := range grp.Repos {
		if repo == nil || repo.Path == "" {
			continue
		}
		for _, c := range readAllCandidates(repo.Path) {
			if repairKinds[c.Kind] {
				continue // repair tab handles these
			}
			items = append(items, pendingCandidateRow{
				CandidateID:     c.ID,
				Repo:            slug,
				Kind:            c.Kind,
				SubjectID:       c.SubjectID,
				Context:         c.Context,
				Hint:            c.Hint,
				Confidence:      c.Confidence,
				DiscoveredAt:    c.DiscoveredAt,
				Score:           c.Score,
				ScoreBreakdown:  c.ScoreBreakdown,
				CriticalityBand: c.CriticalityBand,
			})
		}
	}

	// Sort by Score DESC (high-priority first), then SubjectID for stable tiebreak.
	for i := 1; i < len(items); i++ {
		for j := i; j > 0 && (items[j].Score > items[j-1].Score ||
			(items[j].Score == items[j-1].Score && items[j].SubjectID < items[j-1].SubjectID)); j-- {
			items[j], items[j-1] = items[j-1], items[j]
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"items": items,
		"total": len(items),
	})
}

// candidateRaw is the full on-disk shape of one enrichment-candidates.json entry.
// We parse the Context map so the REST layer can forward it without importing
// internal/enrichment.
type candidateRaw struct {
	ID              string         `json:"id"`
	Kind            string         `json:"kind"`
	SubjectID       string         `json:"subject_id"`
	Context         map[string]any `json:"context,omitempty"`
	Hint            string         `json:"hint,omitempty"`
	Confidence      float64        `json:"confidence,omitempty"`
	DiscoveredAt    string         `json:"discovered_at,omitempty"`
	Score           int            `json:"score,omitempty"`
	ScoreBreakdown  string         `json:"score_breakdown,omitempty"`
	CriticalityBand string         `json:"criticality_band,omitempty"`
}

// readAllCandidates reads every entry from a repo's enrichment-candidates.json.
// Returns nil (not an error) when the file is absent.
func readAllCandidates(repoPath string) []candidateRaw {
	if repoPath == "" {
		return nil
	}
	path := filepath.Join(daemon.StateDirForRepo(repoPath), "enrichment-candidates.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	// Try flat array first.
	var arr []candidateRaw
	if json.Unmarshal(data, &arr) == nil {
		return arr
	}
	// Try {"candidates": [...]} wrapper (v2 schema).
	var obj struct {
		Candidates []candidateRaw `json:"candidates"`
	}
	if json.Unmarshal(data, &obj) == nil {
		return obj.Candidates
	}
	return nil
}

// handleEnrichmentTasks — GET /api/enrichments/{group}/tasks
//
// Returns one EnrichmentTaskRow per unique entity (subject) that has at least
// one pending enrichment action, aggregated across all repos in the group.
// This is the "1 candidate per entity with N pending actions" view requested
// in issue #1134.
//
// The response shape is:
//
//	{
//	  "tasks":        [ {EnrichmentTaskRow}, … ],  // sorted by overall_score DESC
//	  "total_tasks":  42,                          // unique entity count
//	  "total_actions": 97,                         // sum of pending action counts
//	  "overdue_count": 5
//	}
func (s *Server) handleEnrichmentTasks(w http.ResponseWriter, r *http.Request) {
	group := r.PathValue("group")
	if group == "" {
		writeErr(w, http.StatusBadRequest, "group required")
		return
	}

	grp, err := s.graphs.GetGroup(group)
	if err != nil {
		writeErr(w, http.StatusNotFound, err.Error())
		return
	}

	var allTasks []enrichmentTaskRow
	totalActions := 0
	overdueCount := 0

	for slug, repo := range grp.Repos {
		if repo == nil || repo.Path == "" {
			continue
		}
		// Build resolved-set from enrichment-resolutions.json so completed
		// actions are marked correctly.
		resolvedSet := buildResolvedSet(repo.Path)

		// Group candidates by SubjectID.
		type actionEntry struct {
			candidateID     string
			kind            string
			score           float64
			intScore        int
			criticalityBand string
			reason          string
			discoveredAt    string
			completed       bool
		}
		type subjectEntry struct {
			subjectKind string
			subjectName string
			actions     []actionEntry
		}
		subjects := make(map[string]*subjectEntry)
		var subjectOrder []string

		for _, c := range readAllCandidates(repo.Path) {
			if repairKinds[c.Kind] {
				continue // repair tab handles these
			}
			key := c.SubjectID + "|" + c.Kind
			completed := resolvedSet[key]

			se, exists := subjects[c.SubjectID]
			if !exists {
				se = &subjectEntry{}
				subjects[c.SubjectID] = se
				subjectOrder = append(subjectOrder, c.SubjectID)
				// Extract subject kind/name from context.
				if v, ok := c.Context["kind"].(string); ok {
					se.subjectKind = v
				}
				if v, ok := c.Context["name"].(string); ok {
					se.subjectName = v
				}
			}
			se.actions = append(se.actions, actionEntry{
				candidateID:     c.ID,
				kind:            c.Kind,
				score:           c.Confidence,
				intScore:        c.Score,
				criticalityBand: c.CriticalityBand,
				reason:          c.Hint,
				discoveredAt:    c.DiscoveredAt,
				completed:       completed,
			})
		}

		for _, sid := range subjectOrder {
			se := subjects[sid]
			var overallScore, maxScore float64
			var maxIntScore int
			var oldestPending string
			pendingCount := 0

			actions := make([]enrichmentActionWire, 0, len(se.actions))
			for _, a := range se.actions {
				if a.score > maxScore {
					maxScore = a.score
				}
				if a.intScore > maxIntScore {
					maxIntScore = a.intScore
				}
				if !a.completed {
					pendingCount++
					if a.score > overallScore {
						overallScore = a.score
					}
					if oldestPending == "" || (a.discoveredAt != "" && a.discoveredAt < oldestPending) {
						oldestPending = a.discoveredAt
					}
				}
				actions = append(actions, enrichmentActionWire{
					Kind:            a.kind,
					CandidateID:     a.candidateID,
					Score:           a.score,
					IntScore:        a.intScore,
					CriticalityBand: a.criticalityBand,
					Reason:          a.reason,
					Completed:       a.completed,
				})
			}

			if pendingCount == 0 {
				continue // all actions resolved — skip
			}

			overdue := isOverdue(oldestPending)
			if overdue {
				overdueCount++
			}
			totalActions += pendingCount

			// Derive criticality band from the max integer score.
			var taskBand string
			switch {
			case maxIntScore >= 80:
				taskBand = "critical"
			case maxIntScore >= 60:
				taskBand = "high"
			case maxIntScore >= 40:
				taskBand = "medium"
			default:
				taskBand = "low"
			}

			allTasks = append(allTasks, enrichmentTaskRow{
				SubjectID:       sid,
				SubjectKind:     se.subjectKind,
				SubjectName:     se.subjectName,
				Repo:            slug,
				PendingActions:  actions,
				PendingCount:    pendingCount,
				OverallScore:    overallScore,
				MaxActionScore:  maxScore,
				IntScore:        maxIntScore,
				CriticalityBand: taskBand,
				Overdue:         overdue,
				DiscoveredAt:    oldestPending,
			})
		}
	}

	// Sort: OverallScore DESC → MaxActionScore DESC → SubjectID ASC.
	sortEnrichmentTasks(allTasks)

	if allTasks == nil {
		allTasks = []enrichmentTaskRow{}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"tasks":        allTasks,
		"total_tasks":  len(allTasks),
		"total_actions": totalActions,
		"overdue_count": overdueCount,
	})
}

// enrichmentActionWire is the JSON-wire shape for one action inside a task row.
type enrichmentActionWire struct {
	Kind            string  `json:"kind"`
	CandidateID     string  `json:"candidate_id"`
	Score           float64 `json:"score,omitempty"`
	// IntScore is the 0–100 integer score from issue #1131.
	IntScore        int     `json:"int_score,omitempty"`
	CriticalityBand string  `json:"criticality_band,omitempty"`
	Reason          string  `json:"reason,omitempty"`
	Completed       bool    `json:"completed"`
}

// enrichmentTaskRow is the JSON-wire shape for one task row.
type enrichmentTaskRow struct {
	SubjectID       string                 `json:"subject_id"`
	SubjectKind     string                 `json:"subject_kind,omitempty"`
	SubjectName     string                 `json:"subject_name,omitempty"`
	Repo            string                 `json:"repo"`
	PendingActions  []enrichmentActionWire `json:"pending_actions"`
	PendingCount    int                    `json:"pending_count"`
	OverallScore    float64                `json:"overall_score"`
	MaxActionScore  float64                `json:"max_action_score,omitempty"`
	// IntScore is the maximum integer 0–100 score across pending actions (issue #1131).
	IntScore        int     `json:"int_score,omitempty"`
	// CriticalityBand is derived from IntScore: critical/high/medium/low.
	CriticalityBand string  `json:"criticality_band,omitempty"`
	Overdue         bool    `json:"overdue"`
	DiscoveredAt    string  `json:"discovered_at,omitempty"`
}

// buildResolvedSet reads enrichment-resolutions.json for a repo and returns a
// set keyed by "subject_id|kind" for completed-action lookups.
func buildResolvedSet(repoPath string) map[string]bool {
	if repoPath == "" {
		return nil
	}
	resolutions := enrichment.ReadResolutions(daemon.StateDirForRepo(repoPath))
	out := make(map[string]bool, len(resolutions))
	for _, r := range resolutions {
		if r.SubjectID != "" && r.Kind != "" {
			out[r.SubjectID+"|"+r.Kind] = true
		}
	}
	return out
}

// isOverdue reports whether the given RFC 3339 timestamp is older than 7 days.
func isOverdue(discoveredAt string) bool {
	if discoveredAt == "" {
		return false
	}
	t, err := time.Parse(time.RFC3339, discoveredAt)
	if err != nil {
		return false
	}
	return time.Since(t) > 7*24*time.Hour
}

// sortEnrichmentTasks sorts tasks by OverallScore DESC, MaxActionScore DESC,
// then SubjectID ASC for a stable deterministic order.
func sortEnrichmentTasks(tasks []enrichmentTaskRow) {
	// Simple insertion sort — task counts per repo are typically in the
	// thousands, not millions, so O(n²) is acceptable here.
	for i := 1; i < len(tasks); i++ {
		for j := i; j > 0 && enrichmentTaskLess(tasks[j], tasks[j-1]); j-- {
			tasks[j], tasks[j-1] = tasks[j-1], tasks[j]
		}
	}
}

func enrichmentTaskLess(a, b enrichmentTaskRow) bool {
	// Primary: integer 0–100 score DESC (issue #1131) — prefers the new field
	// when both tasks have been scored. Falls back to the legacy float score.
	if a.IntScore != b.IntScore {
		return a.IntScore > b.IntScore
	}
	if a.OverallScore != b.OverallScore {
		return a.OverallScore > b.OverallScore
	}
	if a.MaxActionScore != b.MaxActionScore {
		return a.MaxActionScore > b.MaxActionScore
	}
	return a.SubjectID < b.SubjectID
}

// handleListFindings — GET /api/findings
func (s *Server) handleListFindings(w http.ResponseWriter, r *http.Request) {
	group := r.URL.Query().Get("group")
	if group == "" {
		writeErr(w, http.StatusBadRequest, "group required")
		return
	}

	grp, err := s.graphs.GetGroup(group)
	if err != nil {
		writeErr(w, http.StatusNotFound, err.Error())
		return
	}
	_ = grp

	// Read findings from the memory dir.
	memDir := groupMemoryDir(group)
	findings := readFindingFiles(memDir)

	writeJSON(w, http.StatusOK, map[string]any{
		"findings": findings,
	})
}

// groupMemoryDir returns the memory directory for a group.
func groupMemoryDir(group string) string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".archigraph", "groups", group+"-memory")
}

// readFindingFiles reads all *.json finding files from a directory.
func readFindingFiles(dir string) []map[string]any {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return []map[string]any{}
	}
	var out []map[string]any
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			continue
		}
		var f map[string]any
		if json.Unmarshal(data, &f) == nil {
			out = append(out, f)
		}
	}
	if out == nil {
		return []map[string]any{}
	}
	return out
}

// handleSource — GET /api/source?node_id=&group=&context_lines=
func (s *Server) handleSource(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	nodeID := q.Get("node_id")
	group := q.Get("group")
	if nodeID == "" || group == "" {
		writeErr(w, http.StatusBadRequest, "node_id and group required")
		return
	}
	contextLines := 20
	if v := q.Get("context_lines"); v != "" {
		if n, err := parseInt(v); err == nil && n >= 0 {
			contextLines = n
		}
	}

	grp, err := s.graphs.GetGroup(group)
	if err != nil {
		writeErr(w, http.StatusNotFound, err.Error())
		return
	}

	repo, entity := findEntity(grp, nodeID)
	if entity == nil {
		writeErr(w, http.StatusNotFound, "entity not found: "+nodeID)
		return
	}

	src, err := readSourceLines(entity.SourceFile, repo.Path, entity.StartLine, entity.EndLine, contextLines)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"source":      src,
		"language":    entity.Language,
		"start_line":  entity.StartLine,
		"end_line":    entity.EndLine,
		"source_file": entity.SourceFile,
		"repo":        repo.Slug,
	})
}

// parseInt is a small helper.
func parseInt(s string) (int, error) {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, os.ErrInvalid
		}
		n = n*10 + int(c-'0')
	}
	return n, nil
}

// ─────────────────────────────────────────────────────────────────────────────
// Action endpoints — POST /api/{enrichments|repairs}/{group}/action (#1016)
// ─────────────────────────────────────────────────────────────────────────────

// candidateActionReq is the JSON body accepted by both action endpoints.
type candidateActionReq struct {
	CandidateID string `json:"candidate_id"`
	// Action is "apply" or "reject".
	Action string `json:"action"`
	// Value is the proposed resolved value for apply; optional for enrichment candidates.
	Value string `json:"value,omitempty"`
	// Reason is an optional note stored in the rejection record.
	Reason string `json:"reason,omitempty"`
}

// candidateActionResp is the JSON body returned on success.
type candidateActionResp struct {
	Success     bool   `json:"success"`
	CandidateID string `json:"updated_candidate_id"`
	ResolutionID string `json:"resolution_id,omitempty"`
}

// handleEnrichmentAction — POST /api/enrichments/{group}/action
//
// Applies or rejects one enrichment candidate. The write path mirrors the
// MCP's handleSubmitEnrichment / handleRejectEnrichment using the shared
// helpers extracted in internal/enrichment (#1016 tech-debt rule).
func (s *Server) handleEnrichmentAction(w http.ResponseWriter, r *http.Request) {
	s.handleCandidateAction(w, r, false)
}

// handleRepairAction — POST /api/repairs/{group}/action
//
// Applies or rejects one repair candidate. Rejects are stored in
// enrichment-rejections.json; applies write a resolution row so the next
// index run can consume them.
func (s *Server) handleRepairAction(w http.ResponseWriter, r *http.Request) {
	s.handleCandidateAction(w, r, true)
}

// handleCandidateAction is the shared body for both POST endpoints.
// repairOnly=true means only repair-kind candidates are searched.
func (s *Server) handleCandidateAction(w http.ResponseWriter, r *http.Request, repairOnly bool) {
	group := r.PathValue("group")
	if group == "" {
		writeErr(w, http.StatusBadRequest, "group required")
		return
	}

	var req candidateActionReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
		return
	}
	if req.CandidateID == "" {
		writeErr(w, http.StatusBadRequest, "candidate_id required")
		return
	}
	if req.Action != "apply" && req.Action != "reject" {
		writeErr(w, http.StatusBadRequest, "action must be \"apply\" or \"reject\"")
		return
	}

	grp, err := s.graphs.GetGroup(group)
	if err != nil {
		writeErr(w, http.StatusNotFound, err.Error())
		return
	}

	// Search every repo for the candidate.
	type matchResult struct {
		repoPath  string
		candidate candidateRaw
	}
	var match *matchResult
	for _, repo := range grp.Repos {
		if repo == nil || repo.Path == "" {
			continue
		}
		for _, c := range readAllCandidates(repo.Path) {
			if c.ID != req.CandidateID {
				continue
			}
			// Enforce repair/enrichment partition.
			isRepair := repairKinds[c.Kind]
			if repairOnly && !isRepair {
				continue
			}
			if !repairOnly && isRepair {
				continue
			}
			match = &matchResult{repoPath: repo.Path, candidate: c}
			break
		}
		if match != nil {
			break
		}
	}
	if match == nil {
		writeErr(w, http.StatusNotFound, "candidate not found: "+req.CandidateID)
		return
	}

	archigraphDir := daemon.StateDirForRepo(match.repoPath)
	now := time.Now().UTC().Format(time.RFC3339)

	switch req.Action {
	case "apply":
		// Build a Resolution using the enrichment package's canonical type.
		// SubjectID comes from the candidate; Kind and Value are required.
		subjectID, _ := match.candidate.Context["subject_id"].(string)
		if subjectID == "" {
			subjectID = match.candidate.SubjectID
		}
		value := req.Value
		if value == "" {
			// Fall back to proposed_value from context if the caller omitted the field.
			if pv, ok := match.candidate.Context["proposed_value"].(string); ok {
				value = pv
			}
		}
		res := enrichment.Resolution{
			ID:         match.candidate.ID,
			SubjectID:  subjectID,
			Kind:       match.candidate.Kind,
			Value:      value,
			Confidence: match.candidate.Confidence,
			Reason:     req.Reason,
			ResolvedAt: now,
		}
		if err := enrichment.AppendResolution(archigraphDir, res); err != nil {
			writeErr(w, http.StatusInternalServerError, "write resolution: "+err.Error())
			return
		}
		if err := enrichment.RemoveCandidateByID(archigraphDir, match.candidate.ID); err != nil {
			// Non-fatal: the candidate list will be rebuilt on the next index
			// run, and the resolution is already written. Log and continue.
			_ = err
		}
		writeJSON(w, http.StatusOK, candidateActionResp{
			Success:      true,
			CandidateID:  match.candidate.ID,
			ResolutionID: match.candidate.ID,
		})

	case "reject":
		subjectID, _ := match.candidate.Context["subject_id"].(string)
		if subjectID == "" {
			subjectID = match.candidate.SubjectID
		}
		reason := req.Reason
		if reason == "" {
			reason = "rejected via dashboard"
		}
		if err := enrichment.AppendRejection(archigraphDir, match.candidate.ID, subjectID, match.candidate.Kind, reason); err != nil {
			writeErr(w, http.StatusInternalServerError, "write rejection: "+err.Error())
			return
		}
		if err := enrichment.RemoveCandidateByID(archigraphDir, match.candidate.ID); err != nil {
			_ = err
		}
		writeJSON(w, http.StatusOK, candidateActionResp{
			Success:     true,
			CandidateID: match.candidate.ID,
		})
	}
}
