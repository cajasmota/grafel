package dashboard

// handlers_community_naming.go — Community-naming queue endpoint (#1301).
//
//	GET /api/community-naming/{group}  — name_community candidates for every
//	                                     repo in the group, separate from the
//	                                     entity-level enrichment queue.
//
// Background: the enrichment task queue contained 1,556 name_community jobs
// mixed with ~3,059 entity-level jobs (describe_entity / classify_domain /
// describe_role). Because community naming is a fundamentally different
// workflow — naming graph-detected clusters rather than enriching individual
// code entities — the two kinds are split at the API layer so the frontend
// can show them in separate panels with separate progress tracking.

import (
	"net/http"
)

// communityNamingRow is the wire shape for one community-naming candidate.
// It mirrors pendingCandidateRow but is kept separate so each queue can
// evolve its fields independently.
type communityNamingRow struct {
	CandidateID  string         `json:"candidate_id"`
	Repo         string         `json:"repo"`
	Kind         string         `json:"kind"`
	SubjectID    string         `json:"subject_id"`
	Context      map[string]any `json:"context,omitempty"`
	Confidence   float64        `json:"confidence,omitempty"`
	DiscoveredAt string         `json:"discovered_at,omitempty"`
}

// handleCommunityNaming — GET /api/community-naming/{group}
//
// Returns all name_community candidates aggregated across every repo in
// the group. Each row represents one community cluster that has not yet
// received an agent-assigned name.
//
// Response shape:
//
//	{
//	  "items": [ {communityNamingRow}, … ],
//	  "total": 1556
//	}
func (s *Server) handleCommunityNaming(w http.ResponseWriter, r *http.Request) {
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

	items := []communityNamingRow{}

	for slug, repo := range grp.Repos {
		if repo == nil || repo.Path == "" {
			continue
		}
		for _, c := range readAllCandidates(repo.Path) {
			if !communityNamingKinds[c.Kind] {
				continue // entity enrichment and repair tabs handle the rest
			}
			items = append(items, communityNamingRow{
				CandidateID:  c.ID,
				Repo:         slug,
				Kind:         c.Kind,
				SubjectID:    c.SubjectID,
				Context:      c.Context,
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
