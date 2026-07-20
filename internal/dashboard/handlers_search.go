package dashboard

// handlers_search.go — Global typeahead search
//
//	GET /api/search/{group}?q=
//
// Performance: the handler uses the pre-built SearchIndex stored in
// DashGroup.Search.  No full entity scan happens at request time (#2104).

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/cajasmota/grafel/internal/types"
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
	const entityLimit = 20
	const pathLimit = 10

	grp, err := s.graphs.GetGroup(group)
	if err != nil {
		writeErr(w, http.StatusNotFound, err.Error())
		return
	}

	qLow := strings.ToLower(q)

	// --- Entity search via pre-built index (O(trigram candidates) not O(N)) ---
	entities := []map[string]any{}
	if grp.Search != nil {
		for _, h := range grp.Search.searchEntities(qLow, entityLimit) {
			entities = append(entities, map[string]any{
				"id":          dashPrefixedID(h.repoSlug, h.entity.ID),
				"label":       h.entity.Name,
				"kind":        dashStripScopePrefix(h.entity.Kind),
				"source_file": h.entity.SourceFile,
				"start_line":  h.entity.StartLine,
				"repo":        h.repoSlug,
			})
		}
	} else {
		// Fallback for tests/fixtures that bypass loadGroupForRef.
		entities = searchEntitiesLinear(grp, qLow, entityLimit)
	}

	// --- Doc search: scan doc paths for markdown files matching q ---
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

	// --- Path search via pre-built index ---
	paths := []map[string]any{}
	if grp.Search != nil {
		for _, ep := range grp.Search.searchPaths(qLow, pathLimit) {
			paths = append(paths, map[string]any{
				"id":   ep.prefixID,
				"path": ep.path,
				"verb": ep.verb,
				"repo": ep.repoSlug,
			})
		}
	} else {
		// Fallback for tests/fixtures that bypass loadGroupForRef.
		paths = searchPathsLinear(grp, qLow, pathLimit)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"entities": entities,
		"docs":     docs,
		"paths":    paths,
	})
}

// searchEntitiesLinear is the legacy O(N) scan used when no SearchIndex is
// available (e.g. unit tests that construct a DashGroup directly).
func searchEntitiesLinear(grp *DashGroup, qLow string, limit int) []map[string]any {
	type entityHit struct {
		repoSlug string
		id       string
		name     string
		kind     string
		srcFile  string
		startLn  int
		score    int
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
				hits = append(hits, entityHit{
					repoSlug: r.Slug,
					id:       dashPrefixedID(r.Slug, e.ID),
					name:     e.Name,
					kind:     dashStripScopePrefix(e.Kind),
					srcFile:  e.SourceFile,
					startLn:  e.StartLine,
					score:    score,
				})
			}
		}
	}
	// Sort: higher score first, then name ascending.
	for i := 1; i < len(hits); i++ {
		for j := i; j > 0; j-- {
			a, b := hits[j-1], hits[j]
			if a.score > b.score || (a.score == b.score && a.name <= b.name) {
				break
			}
			hits[j-1], hits[j] = hits[j], hits[j-1]
		}
	}
	if len(hits) > limit {
		hits = hits[:limit]
	}
	out := make([]map[string]any, len(hits))
	for i, h := range hits {
		out[i] = map[string]any{
			"id":          h.id,
			"label":       h.name,
			"kind":        h.kind,
			"source_file": h.srcFile,
			"start_line":  h.startLn,
			"repo":        h.repoSlug,
		}
	}
	return out
}

// searchPathsLinear is the legacy O(N) HTTP-endpoint scan used as fallback.
func searchPathsLinear(grp *DashGroup, qLow string, limit int) []map[string]any {
	var out []map[string]any
	for _, r := range sortedRepos(grp) {
		for i := range r.Doc.Entities {
			e := &r.Doc.Entities[i]
			bareKind := dashStripScopePrefix(e.Kind)
			if !isHTTPEndpointEntity(bareKind, e.Kind) {
				continue
			}
			path := e.PropGet("path")
			if path == "" {
				path = e.Name
			}
			if strings.Contains(strings.ToLower(path), qLow) {
				out = append(out, map[string]any{
					"id":   dashPrefixedID(r.Slug, e.ID),
					"path": path,
					"verb": e.PropGet("verb"),
					"repo": r.Slug,
				})
			}
		}
		if len(out) >= limit {
			break
		}
	}
	return out
}

// isHTTPEndpointEntity mirrors the multi-kind check used elsewhere (#1217).
func isHTTPEndpointEntity(bareKind, rawKind string) bool {
	return types.IsHTTPEndpointKind(bareKind) ||
		strings.EqualFold(bareKind, httpEndpointKind) ||
		rawKind == "Endpoint" || rawKind == "Route"
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
