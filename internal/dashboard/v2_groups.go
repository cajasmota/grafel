// v2_groups.go — Landing-screen group surface for WebUI v2.
//
// Endpoints:
//
//	GET  /api/v2/groups       → rich Group list (drives the Landing cards grid)
//	POST /api/v2/groups       → create an (empty) group from a name
//
// The Landing screen needs more than the slug list returned by /api/v2/meta:
// it renders per-group entity counts, repo slugs, fidelity, last-indexed time,
// and a health state. All of that is already aggregated by the registry store
// (GroupSummary). This handler maps GroupSummary → the v2 Group wire shape the
// WebUI v2 api client expects and derives the health/fidelity fields in one
// place so the rules don't live in the browser.
//
// Create-group reuses the v1 registry CreateGroup path (same as the v1 admin
// and onboard handlers) — it creates the fleet.json + registers the group with
// no repos. Repo discovery / indexing (the full wizard) is intentionally out of
// scope for this endpoint; see the Landing PR for the data-decision write-up.
//
// S1 (#2151): the "tier" field in the v2Group response reflects the aggregate
// tier of all repos in the group (per the tier state machine). On a fresh
// daemon start with lazy hydration, all groups start as "cold" until the first
// MCP query wakes one up. "tier" precedence: hot > warm > cold > expired.

package dashboard

import (
	"encoding/json"
	"net/http"
	"time"
)

// v2Group is the wire shape consumed by the WebUI v2 Landing screen. It mirrors
// the `Group` interface in webui-v2/src/data/types.ts.
type v2Group struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Repos       []string `json:"repos"`
	EntityCount int      `json:"entityCount"`
	// Fidelity is 0..1, or null when the group has never been indexed.
	Fidelity *float64 `json:"fidelity"`
	// IndexedAt is unix-ms, or null when never indexed.
	IndexedAt *int64 `json:"indexedAt"`
	// Health is one of "healthy" | "warning" | "degraded" | "unindexed", derived server-side.
	Health string `json:"health"`
	// Tier is the aggregate tier for the group: "hot" | "warm" | "cold" | "expired".
	// Precedence across repos: hot > warm > cold > expired.
	// "cold" when the group has no graph.fb on disk (S1 #2151).
	Tier string `json:"tier"`
	// Monorepos maps parent-repo slug → list of module sub-paths (M3 #2180).
	// Omitted when no repos have declared modules.
	Monorepos map[string][]string `json:"monorepos,omitempty"`
}

const (
	healthHealthy   = "healthy"
	healthWarning   = "warning"
	healthUnindexed = "unindexed"
)

// deriveGroupHealth computes the health + fidelity for a group from its
// aggregated stats. Priority:
//  1. Real bug_rate from health-history.jsonl (via latestGroupBugRate).
//  2. Never indexed (no entities AND no last-indexed) → unindexed, fidelity null.
//  3. Indexed but no history → neutral fidelity 1.0 / healthy (stable contract).
func deriveGroupHealth(s GroupSummary, histRoot string) (health string, fidelity *float64, indexedAt *int64) {
	indexed := s.LastIndexed != ""
	if !indexed && s.EntityCount == 0 {
		return healthUnindexed, nil, nil
	}
	if t, err := time.Parse(time.RFC3339, s.LastIndexed); err == nil {
		ms := t.UnixMilli()
		indexedAt = &ms
	}

	// Try real bug_rate from history.
	if bugRate, ok := latestGroupBugRate(s.Name, histRoot); ok {
		f := fidelityFromBugRate(bugRate)
		f, hlth := deriveHealthFromFidelity(f)
		return hlth, &f, indexedAt
	}

	// Fallback: indexed but no history recorded yet.
	f := 1.0
	return healthHealthy, &f, indexedAt
}

func toV2Group(s GroupSummary, histRoot string, tq TierQuerier) v2Group {
	health, fidelity, indexedAt := deriveGroupHealth(s, histRoot)
	repos := s.Repos
	if repos == nil {
		repos = []string{}
	}
	// S1 (#2151): compute aggregate tier for the group. Use the tier querier
	// when available; default to "cold" (conservative / correct for lazy-hydrated
	// groups that have never been queried).
	tierStr := groupAggregateTier(s.RepoPaths, tq)
	return v2Group{
		ID:          s.Name,
		Name:        s.Name,
		Repos:       repos,
		EntityCount: s.EntityCount,
		Fidelity:    fidelity,
		IndexedAt:   indexedAt,
		Health:      health,
		Tier:        tierStr,
		Monorepos:   s.Monorepos,
	}
}

// groupAggregateTier returns the highest tier across all repos in the group.
// Precedence: hot > warm > cold > expired. When the tier querier is nil or no
// repos are registered, returns "cold".
//
// The function uses the HEAD ref heuristic (gitmeta-free, just reads the
// common branch names) so it can run without spawning git. For each repo it
// queries TierForRef("main") and TierForRef("master") as a best-effort probe
// since we don't have the ref list here. The actual ref tracking is done
// correctly by the tier manager once a first index pass completes.
func groupAggregateTier(repoPaths []string, tq TierQuerier) string {
	if tq == nil || len(repoPaths) == 0 {
		return "cold"
	}
	// Tier rank: hot=3, warm=2, cold=1, expired=0.
	tierRank := func(t string) int {
		switch t {
		case "hot":
			return 3
		case "warm":
			return 2
		case "cold":
			return 1
		default: // "expired" or unknown
			return 0
		}
	}
	best := 0
	bestStr := "cold"
	for _, repoPath := range repoPaths {
		// Probe the two most common default branch names.
		for _, ref := range []string{"main", "master"} {
			t := tq.TierForRef(repoPath, ref)
			if r := tierRank(t); r > best {
				best = r
				bestStr = t
			}
		}
		if best == 3 {
			break // can't get better than HOT
		}
	}
	return bestStr
}

// handleV2Groups — GET /api/v2/groups
//
// Returns the rich, paginated Group list for the Landing screen.
func (s *Server) handleV2Groups(w http.ResponseWriter, r *http.Request) {
	groups, err := s.registry.ListGroups()
	if err != nil {
		writeV2Err(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	root := s.daemonRoot()
	out := make([]v2Group, 0, len(groups))
	for _, g := range groups {
		out = append(out, toV2Group(g, root, s.tierQuerier))
	}
	pag := parsePagination(r.URL.Query(), len(out))
	end := pag.Offset + pag.Limit
	if pag.Offset > len(out) {
		pag.Offset = len(out)
	}
	if end > len(out) {
		end = len(out)
	}
	writeV2JSON(w, http.StatusOK, v2Page(out[pag.Offset:end], pag))
}

// v2CreateGroupReq is the request body for POST /api/v2/groups.
type v2CreateGroupReq struct {
	Name string `json:"name"`
}

// handleV2CreateGroup — POST /api/v2/groups
//
// Creates an empty group (fleet.json + registry entry) from a name. Repo
// discovery and indexing remain a separate flow; the returned Group has no
// repos and reports health=unindexed.
func (s *Server) handleV2CreateGroup(w http.ResponseWriter, r *http.Request) {
	var req v2CreateGroupReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeV2Err(w, http.StatusBadRequest, "bad_request", "invalid JSON body")
		return
	}
	if req.Name == "" {
		writeV2Err(w, http.StatusBadRequest, "bad_request", "name required")
		return
	}
	created, err := s.registry.CreateGroup(req.Name)
	if err != nil {
		writeV2Err(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	writeV2JSON(w, http.StatusCreated, v2OK(toV2Group(created, s.daemonRoot(), s.tierQuerier)))
}
