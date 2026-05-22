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
//	Persists a hint string keyed by the ENTITY ID (SubjectID) of the
//	candidate. Body: {"hint":"..."}.  Empty hint string clears the hint.
//	{cid} is treated as an entity ID so hints survive candidate-ID churn
//	across re-index sweeps (#1518). 404 when no candidate for that entity
//	is found in any repo of the group.
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
	// EntityID is the stable entity identifier (SubjectID). Clients MUST use
	// this field — not ID — when calling PUT …/candidates/{cid}/hint.
	EntityID    string      `json:"entityId"`
	Severity    string      `json:"severity"`
	IssueType   string      `json:"issueType"`
	Entity      v2EntityRef `json:"entity"`
	Description string      `json:"description"`
	Confidence  float64     `json:"confidence"`
	DetectedAt  int64       `json:"detectedAt"` // unix ms
	// Hint is the team-authored hint currently stored for this entity (may be "").
	Hint        string      `json:"hint,omitempty"`
}

type v2EnrichmentCandidate struct {
	ID             string      `json:"id"`
	// EntityID is the stable entity identifier (SubjectID). Clients MUST use
	// this field — not ID — when calling PUT …/candidates/{cid}/hint.
	EntityID       string      `json:"entityId"`
	EnrichmentType string      `json:"enrichmentType"`
	Entity         v2EntityRef `json:"entity"`
	Description    string      `json:"description"`
	Confidence     float64     `json:"confidence"`
	DetectedAt     int64       `json:"detectedAt"` // unix ms
	// Hint is the team-authored hint currently stored for this entity (may be "").
	Hint           string      `json:"hint,omitempty"`
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
		// Load the entity-keyed hint store once per repo.
		hints := readEntityHints(repo.Path)
		for _, c := range readAllCandidates(repo.Path) {
			// Resolve hint: prefer entity-keyed store (#1518); fall back to the
			// legacy candidate-level Hint field for backward compatibility.
			hint := hints[c.SubjectID]
			if hint == "" {
				hint = c.Hint
			}

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
					EntityID:    c.SubjectID,
					Severity:    sev,
					IssueType:   issueType,
					Entity:      entityRefFromContext(c.Context, slug, c.SubjectID),
					Description: descriptionFromContext(c.Context, c.Kind),
					Confidence:  c.Confidence,
					DetectedAt:  parseDetectedAt(c.DiscoveredAt),
					Hint:        hint,
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
					EntityID:       c.SubjectID,
					EnrichmentType: enrichType,
					Entity:         entityRefFromContext(c.Context, slug, c.SubjectID),
					Description:    descriptionFromContext(c.Context, c.Kind),
					Confidence:     c.Confidence,
					DetectedAt:     parseDetectedAt(c.DiscoveredAt),
					Hint:           hint,
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
// Persists a hint keyed by the ENTITY ID (SubjectID) of the candidate.
// {cid} in the URL path is interpreted as the entity ID, not the ephemeral
// candidate ID, so hints survive candidate-ID churn across re-index sweeps
// (#1518).
//
// The handler verifies that at least one candidate in the group has the
// given entity ID (SubjectID) before writing, so callers get a proper 404
// when the entity is no longer present.
//
// Responds 200 { ok:true, data: { hint: "<saved>", entityId: "<eid>" } }.
// Responds 404 when no candidate with that entity ID is found in any repo.
func (s *Server) handleV2CandidateHint(w http.ResponseWriter, r *http.Request) {
	group := r.PathValue("group")
	entityID := r.PathValue("cid") // path param name kept for URL compat; value is now entity ID
	if group == "" || entityID == "" {
		writeV2Err(w, http.StatusBadRequest, "bad_request", "group and entityId required")
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

	// Find any repo that has a candidate for this entity ID and persist the hint.
	for _, repo := range grp.Repos {
		if repo == nil || repo.Path == "" {
			continue
		}
		if updated := upsertEntityHint(repo.Path, entityID, req.Hint); updated {
			writeV2JSON(w, http.StatusOK, v2OK(map[string]string{
				"hint":     req.Hint,
				"entityId": entityID,
			}))
			return
		}
	}

	writeV2Err(w, http.StatusNotFound, "not_found", "no candidate found for entity")
}

// entityHintsFile returns the path to the entity-hints store for a repo.
// The store is a flat JSON object: { "<entityID>": "<hint>", ... }.
// It lives next to enrichment-candidates.json in the archigraph state dir.
func entityHintsFile(repoPath string) string {
	return filepath.Join(daemon.StateDirForRepo(repoPath), "entity-hints.json")
}

// readEntityHints loads the entity-keyed hint store from disk.
// Returns an empty map on any read or parse error (non-fatal).
func readEntityHints(repoPath string) map[string]string {
	if repoPath == "" {
		return map[string]string{}
	}
	data, err := os.ReadFile(entityHintsFile(repoPath))
	if err != nil {
		return map[string]string{}
	}
	var m map[string]string
	if json.Unmarshal(data, &m) != nil {
		return map[string]string{}
	}
	return m
}

// upsertEntityHint writes (or clears) a hint for entityID in the entity-hints
// store.  It first confirms that the entity exists among the repo's candidates
// (so stale entity IDs get a 404 rather than silently creating orphan hints).
// Returns true when the hint was persisted successfully.
func upsertEntityHint(repoPath, entityID, hint string) bool {
	if repoPath == "" || entityID == "" {
		return false
	}

	// Verify the entity exists in current candidates.
	found := false
	for _, c := range readAllCandidates(repoPath) {
		if c.SubjectID == entityID {
			found = true
			break
		}
	}
	if !found {
		return false
	}

	m := readEntityHints(repoPath)

	if hint == "" {
		delete(m, entityID)
	} else {
		m[entityID] = hint
	}

	out, err := json.Marshal(m)
	if err != nil {
		return false
	}
	return os.WriteFile(entityHintsFile(repoPath), out, 0o644) == nil
}
