package dashboard

// handlers_patterns.go — Pattern store endpoint
//
//	GET /api/patterns/{group}

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
)

// handlePatterns — GET /api/patterns/{group}
//
// Returns the full pattern store for the group with exemplar entity IDs
// resolved to minimal entity cards.
func (s *Server) handlePatterns(w http.ResponseWriter, r *http.Request) {
	group := r.PathValue("group")
	if group == "" {
		writeErr(w, http.StatusBadRequest, "group required")
		return
	}

	dir := groupPatternsDir(group)
	patterns, err := loadPatterns(dir)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "load patterns: "+err.Error())
		return
	}

	// Best-effort: resolve exemplar entity IDs to entity cards.
	grp, _ := s.graphs.GetGroup(group) // may be nil if group not in registry

	out := make([]map[string]any, 0, len(patterns))
	for _, p := range patterns {
		entry := map[string]any{
			"id":           p["id"],
			"title":        patternTitle(p),
			"description":  patternDescription(p),
			"category":     p["category"],
			"is_candidate": p["is_candidate"],
			"confidence":   p["confidence"],
			"exemplar_ids": p["exemplars"],
		}
		// Resolve exemplars to entity cards.
		if grp != nil {
			if exemplarIDs, ok := p["exemplars"].([]interface{}); ok {
				cards := make([]map[string]any, 0, len(exemplarIDs))
				for _, eid := range exemplarIDs {
					if id, ok := eid.(string); ok {
						_, entity := findEntity(grp, id)
						if entity != nil {
							repoSlug := ""
							for _, r := range sortedRepos(grp) {
								for i := range r.Doc.Entities {
									if r.Doc.Entities[i].ID == entity.ID {
										repoSlug = r.Slug
										break
									}
								}
								if repoSlug != "" {
									break
								}
							}
							cards = append(cards, map[string]any{
								"id":          dashPrefixedID(repoSlug, entity.ID),
								"label":       entity.Name,
								"kind":        dashStripScopePrefix(entity.Kind),
								"source_file": entity.SourceFile,
								"start_line":  entity.StartLine,
								"repo":        repoSlug,
							})
						}
					}
				}
				if len(cards) > 0 {
					entry["exemplars_resolved"] = cards
				}
			}
		}
		out = append(out, entry)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"patterns": out,
		"count":    len(out),
	})
}

// groupPatternsDir returns the directory where patterns.json is stored for a group.
func groupPatternsDir(groupName string) string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".archigraph", "groups", groupName+"-patterns")
}

// loadPatterns reads patterns.json from a directory. Returns empty slice when
// the file doesn't exist (not an error).
func loadPatterns(dir string) ([]map[string]any, error) {
	if dir == "" {
		return nil, nil
	}
	data, err := os.ReadFile(filepath.Join(dir, "patterns.json"))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	// patterns.json may be {"patterns": [...]} or just [...].
	var env struct {
		Patterns []map[string]any `json:"patterns"`
	}
	if json.Unmarshal(data, &env) == nil && env.Patterns != nil {
		return env.Patterns, nil
	}
	var arr []map[string]any
	if err := json.Unmarshal(data, &arr); err != nil {
		return nil, err
	}
	return arr, nil
}

// patternTitle extracts a title from a pattern map (uses trigger.natural_language).
func patternTitle(p map[string]any) string {
	if t, ok := p["title"].(string); ok && t != "" {
		return t
	}
	if trigger, ok := p["trigger"].(map[string]any); ok {
		if nl, ok := trigger["natural_language"].(string); ok {
			return nl
		}
	}
	if id, ok := p["id"].(string); ok {
		return id
	}
	return ""
}

// patternDescription extracts a description from a pattern map.
func patternDescription(p map[string]any) string {
	if d, ok := p["description"].(string); ok {
		return d
	}
	if steps, ok := p["steps"].([]interface{}); ok && len(steps) > 0 {
		if s, ok := steps[0].(string); ok {
			return s
		}
	}
	return ""
}
