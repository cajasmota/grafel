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
	CandidateID    string         `json:"candidate_id"`
	Repo           string         `json:"repo"`
	Kind           string         `json:"kind"`
	SubjectID      string         `json:"subject_id"`
	Context        map[string]any `json:"context,omitempty"`
	Hint           string         `json:"hint,omitempty"`
	Confidence     float64        `json:"confidence,omitempty"`
	DiscoveredAt   string         `json:"discovered_at,omitempty"`
	AutoResolvable bool           `json:"auto_resolvable"`
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
				CandidateID:  c.ID,
				Repo:         slug,
				Kind:         c.Kind,
				SubjectID:    c.SubjectID,
				Context:      c.Context,
				Hint:         c.Hint,
				Confidence:   c.Confidence,
				DiscoveredAt: c.DiscoveredAt,
			})
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
	ID           string         `json:"id"`
	Kind         string         `json:"kind"`
	SubjectID    string         `json:"subject_id"`
	Context      map[string]any `json:"context,omitempty"`
	Hint         string         `json:"hint,omitempty"`
	Confidence   float64        `json:"confidence,omitempty"`
	DiscoveredAt string         `json:"discovered_at,omitempty"`
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
