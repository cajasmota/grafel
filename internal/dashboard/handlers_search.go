package dashboard

// handlers_search.go — Global typeahead search
//
//	GET /api/search/{group}?q=

import (
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/cajasmota/archigraph/internal/graph"
)

// handleSearch — GET /api/search/{group}?q=
func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	group := r.PathValue("group")
	if group == "" {
		writeErr(w, http.StatusBadRequest, "group required")
		return
	}
	q := r.URL.Query().Get("q")
	if q == "" {
		writeErr(w, http.StatusBadRequest, "q required")
		return
	}
	limit := 20

	grp, err := s.graphs.GetGroup(group)
	if err != nil {
		writeErr(w, http.StatusNotFound, err.Error())
		return
	}

	qLow := strings.ToLower(q)

	// Score entities by simple name prefix / substring match.
	// A production implementation would use BM25 (same as MCP); for Phase 1
	// this substring search is fast and correct enough for typeahead.
	type entityHit struct {
		e    *graph.Entity
		repo string
		score int // 2 = prefix, 1 = substring
	}

	var hits []entityHit
	for _, r := range sortedRepos(grp) {
		for i := range r.Doc.Entities {
			e := &r.Doc.Entities[i]
			nameLow := strings.ToLower(e.Name)
			score := 0
			if strings.HasPrefix(nameLow, qLow) {
				score = 2
			} else if strings.Contains(nameLow, qLow) {
				score = 1
			}
			if score > 0 {
				hits = append(hits, entityHit{e: e, repo: r.Slug, score: score})
			}
		}
	}
	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].score != hits[j].score {
			return hits[i].score > hits[j].score
		}
		return hits[i].e.Name < hits[j].e.Name
	})
	if len(hits) > limit {
		hits = hits[:limit]
	}

	entities := make([]map[string]any, 0, len(hits))
	for _, h := range hits {
		entities = append(entities, map[string]any{
			"id":          dashPrefixedID(h.repo, h.e.ID),
			"label":       h.e.Name,
			"kind":        dashStripScopePrefix(h.e.Kind),
			"source_file": h.e.SourceFile,
			"start_line":  h.e.StartLine,
			"repo":        h.repo,
		})
	}

	// Doc search: scan doc paths for markdown files whose names match q.
	docs := []map[string]any{}
	if docPaths, dErr := groupDocPaths(group); dErr == nil {
		for repoSlug, docPath := range docPaths {
			if docPath == "" {
				continue
			}
			matches := searchDocFiles(docPath, repoSlug, qLow, 5)
			docs = append(docs, matches...)
		}
	}

	// Path search: filter http_endpoint paths.
	paths := []map[string]any{}
	for _, r := range sortedRepos(grp) {
		for i := range r.Doc.Entities {
			e := &r.Doc.Entities[i]
			if !strings.EqualFold(dashStripScopePrefix(e.Kind), httpEndpointKind) &&
				e.Kind != "Endpoint" && e.Kind != "Route" {
				continue
			}
			path := e.Properties["path"]
			if path == "" {
				path = e.Name
			}
			if strings.Contains(strings.ToLower(path), qLow) {
				paths = append(paths, map[string]any{
					"id":   dashPrefixedID(r.Slug, e.ID),
					"path": path,
					"verb": e.Properties["verb"],
					"repo": r.Slug,
				})
			}
		}
		if len(paths) >= 10 {
			break
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"entities": entities,
		"docs":     docs,
		"paths":    paths,
	})
}

// searchDocFiles walks a directory looking for markdown files whose names or
// paths match q.
func searchDocFiles(dir, repoSlug, qLow string, limit int) []map[string]any {
	var out []map[string]any
	_ = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || len(out) >= limit {
			return nil
		}
		if info.IsDir() {
			return nil
		}
		name := strings.ToLower(info.Name())
		if !strings.HasSuffix(name, ".md") {
			return nil
		}
		rel, _ := filepath.Rel(dir, path)
		if strings.Contains(name, qLow) || strings.Contains(strings.ToLower(rel), qLow) {
			out = append(out, map[string]any{
				"path": repoSlug + "/" + rel,
				"repo": repoSlug,
			})
		}
		return nil
	})
	return out
}
