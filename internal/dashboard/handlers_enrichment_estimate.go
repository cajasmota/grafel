package dashboard

// handlers_enrichment_estimate.go — Pre-run cost estimator endpoint (#1287).
//
//	GET /api/enrichments/{group}/estimate
//
// Returns a cost breakdown per criticality tier before the user triggers a
// batch enrichment run, so they can make an informed decision about the cost.
//
// Pricing constants live in internal/enrichment/pricing.go — update them
// there when Anthropic publishes new rates.
//
// Token estimation formula (per entity):
//
//	tokens = TokensPerEntity(kind) + promptOverheadTokens
//
// where TokensPerEntity returns a conservative upper-bound per entity kind
// and promptOverheadTokens = 200 covers system prompt + JSON scaffolding.
//
// The estimate excludes entities that already have a completed job (status=done)
// so users who are doing incremental enrichment see only the remaining cost.

import (
	"net/http"

	"github.com/cajasmota/archigraph/internal/enrichment"
	"github.com/cajasmota/archigraph/internal/jobs"
)

// estimateTier is the per-band breakdown in the estimate response.
type estimateTier struct {
	// Band is the criticality band: "critical" | "high" | "medium" | "low".
	Band string `json:"band"`
	// Model is the Claude model this band uses: "sonnet" (critical) or "haiku" (others).
	Model string `json:"model"`
	// Count is the number of candidate entities pending enrichment in this band.
	Count int `json:"count"`
	// EstTokens is the total estimated token count for all entities in this band.
	EstTokens int `json:"est_tokens"`
	// EstUSD is the estimated cost in USD at published Anthropic rates.
	EstUSD float64 `json:"est_usd"`
}

// enrichmentEstimateResponse is the wire type for GET /api/enrichments/{group}/estimate.
type enrichmentEstimateResponse struct {
	// Tiers is the per-band breakdown.
	Tiers []estimateTier `json:"tiers"`
	// AlreadyEnriched is the number of entities with a completed job — excluded from the estimate.
	AlreadyEnriched int `json:"already_enriched"`
	// TotalEstTokens is the sum of est_tokens across all tiers.
	TotalEstTokens int `json:"total_est_tokens"`
	// TotalEstUSD is the sum of est_usd across all tiers.
	TotalEstUSD float64 `json:"total_est_usd"`
	// EstMinutes is the rough wall-clock estimate for the full run.
	EstMinutes float64 `json:"est_minutes"`
}

// handleEnrichmentEstimate — GET /api/enrichments/{group}/estimate
//
// Reads the pending enrichment candidates from disk, groups them by
// criticality band, computes per-band token and USD estimates, and returns
// the breakdown. Already-enriched entities (job status=done) are excluded.
//
// Returns 200 with an empty breakdown (not 404) when the group has no
// pending candidates — this lets the frontend differentiate "no cost"
// from "not found".
func (s *Server) handleEnrichmentEstimate(w http.ResponseWriter, r *http.Request) {
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

	// Build a set of already-enriched entity IDs so we can exclude them.
	alreadyDone := make(map[string]bool)
	if s.jobQueue != nil {
		entityStatus := buildEntityStatusMap(s.jobQueue, group)
		for entityID, status := range entityStatus {
			if status == jobs.StatusDone {
				alreadyDone[entityID] = true
			}
		}
	}

	// Collect all pending (non-repair) candidates across all repos in the group.
	// band → slice of candidates
	byBand := make(map[string][]candidateRaw)
	for _, repo := range grp.Repos {
		if repo == nil || repo.Path == "" {
			continue
		}
		for _, c := range readAllCandidates(repo.Path) {
			if repairKinds[c.Kind] {
				continue // repair tab handles these
			}
			if alreadyDone[c.SubjectID] {
				continue // already enriched — skip from estimate
			}
			band := c.CriticalityBand
			if band == "" {
				band = "low"
			}
			byBand[band] = append(byBand[band], c)
		}
	}

	// Build tier rows in canonical order.
	bandOrder := []string{"critical", "high", "medium", "low"}
	var tiers []estimateTier
	totalTokens := 0
	totalUSD := 0.0

	for _, band := range bandOrder {
		candidates := byBand[band]
		model := modelForBand(band)

		bandTokens := 0
		for _, c := range candidates {
			bandTokens += enrichment.EstimateEntityTokens(c.Kind)
		}

		bandUSD := enrichment.USDForTokens(bandTokens, model)
		tiers = append(tiers, estimateTier{
			Band:      band,
			Model:     model,
			Count:     len(candidates),
			EstTokens: bandTokens,
			EstUSD:    roundUSD(bandUSD),
		})
		totalTokens += bandTokens
		totalUSD += bandUSD
	}

	resp := enrichmentEstimateResponse{
		Tiers:           tiers,
		AlreadyEnriched: len(alreadyDone),
		TotalEstTokens:  totalTokens,
		TotalEstUSD:     roundUSD(totalUSD),
		EstMinutes:      roundMinutes(enrichment.EstimateWallMinutes(totalTokens)),
	}
	writeJSON(w, http.StatusOK, resp)
}

// modelForBand returns the Claude model tier for a given criticality band.
// Exported for test use.
func modelForBand(band string) string {
	if band == "critical" {
		return "sonnet"
	}
	return "haiku"
}

// roundUSD rounds a USD value to 2 decimal places for clean display.
func roundUSD(v float64) float64 {
	return float64(int(v*100+0.5)) / 100
}

// roundMinutes rounds a minute value to 1 decimal place.
func roundMinutes(v float64) float64 {
	return float64(int(v*10+0.5)) / 10
}
