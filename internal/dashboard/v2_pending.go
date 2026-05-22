// v2_pending.go — Pending screen endpoints for WebUI v2 (#1442).
//
// GET /api/v2/groups/{group}/candidates?tab=repairs|enrichments
//
//	Returns repair + enrichment candidates in the v2 wire shape that
//	webui-v2/src/data/types.ts expects. Both tabs are returned together
//	when ?tab is omitted; pass ?tab=repairs or ?tab=enrichments to scope.
//
// PUT /api/v2/groups/{group}/candidates/{cid}/hint
//
//	Persists a hint string on the matching candidate entry. Body: {"hint":"..."}
//	Empty hint string clears the hint. 404 when candidate not found.
package dashboard

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/cajasmota/archigraph/internal/daemon"
)

// ---------------------------------------------------------------------------
// Wire shapes — mirror webui-v2/src/data/types.ts
// ---------------------------------------------------------------------------

type v2EntityRef struct {
	Name string `json:"name"`
	Type string `json:"type"`
	Repo string `json:"repo"`
	File string `json:"file"`
}

type v2RepairCandidate struct {
	ID          string      `json:"id"`
	Severity    string      `json:"severity"`
	IssueType   string      `json:"issueType"`
	Entity      v2EntityRef `json:"entity"`
	Description string      `json:"description"`
	Confidence  float64     `json:"confidence"`
	DetectedAt  int64       `json:"detectedAt"` // unix ms
}

type v2EnrichmentCandidate struct {
	ID             string      `json:"id"`
	EnrichmentType string      `json:"enrichmentType"`
	Entity         v2EntityRef `json:"entity"`
	Description    string      `json:"description"`
	Confidence     float64     `json:"confidence"`
	DetectedAt     int64       `json:"detectedAt"` // unix ms
}

type v2CandidatesResponse struct {
	Repairs     []v2RepairCandidate     `json:"repairs"`
	Enrichments []v2EnrichmentCandidate `json:"enrichments"`
}

// ---------------------------------------------------------------------------
// Mapping helpers
// ---------------------------------------------------------------------------

// kindToRepairIssueType maps the daemon's internal candidate kind strings to
// the design-doc RepairIssueType values WebUI v2 expects.
var kindToRepairIssueType = map[string]string{
	"repair_edge":              "broken_link",
	"dynamic_baseurl_endpoint": "mismatched_handler",
}

// kindToEnrichmentType maps daemon kinds to design-doc EnrichmentType values.
var kindToEnrichmentType = map[string]string{
	"describe_entity":    "summary",
	"summarize_api":      "summary",
	"classify_domain":    "tags",
	"describe_role":      "summary",
	"param_descriptions": "param_descriptions",
	"relationship_tag":   "relationship_tag",
}

// criticalityBandToSeverity maps CriticalityBand strings to the design-doc
// Severity values. Falls back to "info".
var criticalityBandToSeverity = map[string]string{
	"critical": "critical",
	"high":     "warning",
	"medium":   "warning",
	"low":      "info",
}

// parseDetectedAt converts an RFC3339 string to unix-ms; returns current time on parse failure.
func parseDetectedAt(s string) int64 {
	if s == "" {
		return time.Now().UnixMilli()
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Now().UnixMilli()
	}
	return t.UnixMilli()
}

// entityRefFromContext extracts a v2EntityRef from a candidate's Context map.
// Falls back to SubjectID as the name when context keys are absent.
func entityRefFromContext(ctx map[string]any, repo, subjectID string) v2EntityRef {
	getString := func(key string) string {
		if v, ok := ctx[key]; ok {
			if s, ok := v.(string); ok {
				return s
			}
		}
		return ""
	}
	name := getString("entity_name")
	if name == "" {
		name = getString("subject_name")
	}
	if name == "" {
		// Trim to the last segment (e.g. "pkg.Foo.Bar" → "Bar")
		parts := strings.Split(subjectID, ".")
		name = parts[len(parts)-1]
	}
	entityType := getString("entity_type")
	if entityType == "" {
		entityType = getString("kind")
	}
	if entityType == "" {
		entityType = "function"
	}
	file := getString("file")
	if file == "" {
		file = getString("source_file")
	}
	return v2EntityRef{
		Name: name,
		Type: entityType,
		Repo: repo,
		File: file,
	}
}

// descriptionFromContext extracts a human-readable description from the
// candidate context map. Falls back to a sensible default.
func descriptionFromContext(ctx map[string]any, _ string) string {
	for _, key := range []string{"description", "reason", "details", "message"} {
		if v, ok := ctx[key]; ok {
			if s, ok := v.(string); ok && s != "" {
				return s
			}
		}
	}
	return "Detected by archigraph. No additional context available."
}

// ---------------------------------------------------------------------------
// Handlers
// ---------------------------------------------------------------------------

// handleV2Candidates — GET /api/v2/groups/{group}/candidates
func (s *Server) handleV2Candidates(w http.ResponseWriter, r *http.Request) {
	group := r.PathValue("group")
	if group == "" {
		writeV2Err(w, http.StatusBadRequest, "bad_request", "group required")
		return
	}
	tab := r.URL.Query().Get("tab") // "repairs", "enrichments", or ""

	grp, err := s.graphs.GetGroup(group)
	if err != nil {
		writeV2Err(w, http.StatusNotFound, "not_found", err.Error())
		return
	}

	var repairs []v2RepairCandidate
	var enrichments []v2EnrichmentCandidate

	for slug, repo := range grp.Repos {
		if repo == nil || repo.Path == "" {
			continue
		}
		for _, c := range readAllCandidates(repo.Path) {
			if repairKinds[c.Kind] {
				if tab == "enrichments" {
					continue
				}
				issueType := kindToRepairIssueType[c.Kind]
				if issueType == "" {
					issueType = "broken_link"
				}
				sev := criticalityBandToSeverity[c.CriticalityBand]
				if sev == "" {
					if c.Confidence >= 0.85 {
						sev = "warning"
					} else {
						sev = "info"
					}
				}
				repairs = append(repairs, v2RepairCandidate{
					ID:          c.ID,
					Severity:    sev,
					IssueType:   issueType,
					Entity:      entityRefFromContext(c.Context, slug, c.SubjectID),
					Description: descriptionFromContext(c.Context, c.Kind),
					Confidence:  c.Confidence,
					DetectedAt:  parseDetectedAt(c.DiscoveredAt),
				})
			} else if !communityNamingKinds[c.Kind] {
				if tab == "repairs" {
					continue
				}
				enrichType := kindToEnrichmentType[c.Kind]
				if enrichType == "" {
					enrichType = "summary"
				}
				enrichments = append(enrichments, v2EnrichmentCandidate{
					ID:             c.ID,
					EnrichmentType: enrichType,
					Entity:         entityRefFromContext(c.Context, slug, c.SubjectID),
					Description:    descriptionFromContext(c.Context, c.Kind),
					Confidence:     c.Confidence,
					DetectedAt:     parseDetectedAt(c.DiscoveredAt),
				})
			}
		}
	}

	if repairs == nil {
		repairs = []v2RepairCandidate{}
	}
	if enrichments == nil {
		enrichments = []v2EnrichmentCandidate{}
	}

	writeV2JSON(w, http.StatusOK, v2OK(v2CandidatesResponse{
		Repairs:     repairs,
		Enrichments: enrichments,
	}))
}

// v2HintReq is the body for PUT /api/v2/groups/{group}/candidates/{cid}/hint.
type v2HintReq struct {
	Hint string `json:"hint"`
}

// handleV2CandidateHint — PUT /api/v2/groups/{group}/candidates/{cid}/hint
//
// Persists the hint on the matching candidate in enrichment-candidates.json.
// Responds 200 { ok:true, data: { hint: "<saved>" } } on success.
// Responds 404 when the candidate is not found in any repo.
func (s *Server) handleV2CandidateHint(w http.ResponseWriter, r *http.Request) {
	group := r.PathValue("group")
	cid := r.PathValue("cid")
	if group == "" || cid == "" {
		writeV2Err(w, http.StatusBadRequest, "bad_request", "group and cid required")
		return
	}

	var req v2HintReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeV2Err(w, http.StatusBadRequest, "bad_request", "invalid JSON body")
		return
	}

	grp, err := s.graphs.GetGroup(group)
	if err != nil {
		writeV2Err(w, http.StatusNotFound, "not_found", err.Error())
		return
	}

	for _, repo := range grp.Repos {
		if repo == nil || repo.Path == "" {
			continue
		}
		if updated := updateCandidateHint(repo.Path, cid, req.Hint); updated {
			writeV2JSON(w, http.StatusOK, v2OK(map[string]string{"hint": req.Hint}))
			return
		}
	}

	writeV2Err(w, http.StatusNotFound, "not_found", "candidate not found")
}

// updateCandidateHint reads enrichment-candidates.json in repoPath, finds
// the entry with id == cid, updates its Hint, and writes the file back.
// Returns true when the candidate was found and the file written successfully.
func updateCandidateHint(repoPath, cid, hint string) bool {
	if repoPath == "" {
		return false
	}
	filePath := filepath.Join(daemon.StateDirForRepo(repoPath), "enrichment-candidates.json")
	data, err := os.ReadFile(filePath)
	if err != nil {
		return false
	}

	// Support both flat-array and {"candidates":[…]} shapes.
	var arr []candidateRaw
	wrapped := false
	if json.Unmarshal(data, &arr) != nil {
		var obj struct {
			Candidates []candidateRaw `json:"candidates"`
		}
		if json.Unmarshal(data, &obj) != nil {
			return false
		}
		arr = obj.Candidates
		wrapped = true
	}

	found := false
	for i := range arr {
		if arr[i].ID == cid {
			arr[i].Hint = hint
			found = true
			break
		}
	}
	if !found {
		return false
	}

	var out []byte
	var marshalErr error
	if wrapped {
		out, marshalErr = json.Marshal(struct {
			Candidates []candidateRaw `json:"candidates"`
		}{Candidates: arr})
	} else {
		out, marshalErr = json.Marshal(arr)
	}
	if marshalErr != nil {
		return false
	}
	return os.WriteFile(filePath, out, 0o644) == nil
}
