package dashboard

// handlers_init.go — GET /api/dashboard/init
//
// Returns a combined first-paint payload so the SPA can render the app
// shell in one round-trip instead of 3-4 sequential calls.
//
// Shape: { registry, groups[], daemon_status, served_at }

import (
	"net/http"
	"sync"
	"time"
)

// handleDashboardInit — GET /api/dashboard/init
//
// Fetches registry + group stats in parallel and merges them into a single
// response.  Target: < 100 ms on a local repo set.
func (s *Server) handleDashboardInit(w http.ResponseWriter, r *http.Request) {
	var (
		wg        sync.WaitGroup
		groupsMu  sync.Mutex
		groups    []GroupSummary
		groupsErr string
	)

	wg.Add(1)
	go func() {
		defer wg.Done()
		gs, err := s.registry.ListGroups()
		groupsMu.Lock()
		defer groupsMu.Unlock()
		if err != nil {
			groupsErr = err.Error()
			return
		}
		if gs == nil {
			gs = []GroupSummary{}
		}
		groups = gs
	}()

	wg.Wait()

	if groupsErr != "" {
		writeErr(w, http.StatusInternalServerError, "list groups: "+groupsErr)
		return
	}

	// Build group summaries enriched with entity / relationship counts where
	// the graph has already been loaded (best-effort; never blocks first paint).
	enriched := make([]map[string]any, 0, len(groups))
	for _, g := range groups {
		entry := map[string]any{
			"name":        g.Name,
			"config_path": g.ConfigPath,
			"repos":       g.Repos,
		}
		// Best-effort: try to load cached stats (sub-second when already warm).
		if grp, err := s.graphs.GetGroup(g.Name); err == nil {
			totalE, totalR := 0, 0
			for _, r := range grp.Repos {
				if r.Doc != nil {
					totalE += len(r.Doc.Entities)
					totalR += len(r.Doc.Relationships)
				}
			}
			entry["total_entities"] = totalE
			entry["total_relationships"] = totalR
			if fws := groupTopFrameworks(grp, 8); len(fws) > 0 {
				entry["frameworks"] = fws
			}
		}
		enriched = append(enriched, entry)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"registry": map[string]any{
			"groups": groups,
		},
		"groups":    enriched,
		"settings":  map[string]any{},
		"served_at": time.Now().UTC().Format(time.RFC3339),
	})
}
