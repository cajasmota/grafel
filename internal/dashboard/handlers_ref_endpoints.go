// handlers_ref_endpoints.go — ?ref= query support on 6 read-only endpoints
// and a new GET /api/groups/:g/refs listing endpoint (issue #2220).
//
// These endpoints are the API surface for the multi-branch UI introduced by
// epic #2098. They are sibling to the CLI --ref flags added by #2219 and the
// v2 /api/v2/groups/{group}/refs endpoint from PH1c (#2089).
//
// Endpoints:
//
//	GET /api/groups/:g/stats?ref=<ref>
//	GET /api/groups/:g/repos/:r/entities?ref=<ref>
//	GET /api/groups/:g/repos/:r/relationships?ref=<ref>
//	GET /api/groups/:g/repos/:r/cross-repo-edges?ref=<ref>
//	GET /api/groups/:g/repos/:r/orphans?ref=<ref>
//	GET /api/groups/:g/repos/:r/patterns?ref=<ref>
//	GET /api/groups/:g/refs
//
// ?ref= semantics (all 6 endpoints):
//
//	missing / "@current" → current HEAD ref (pre-#2220 default, backward compatible)
//	"<name>"             → that specific ref's graph
//	"@all"               → aggregate across all indexed refs for the repo/group
//	invalid              → HTTP 400 {"error": "invalid ref", "available": [...]}
//
// Read-only — none of these endpoints trigger reindex.
package dashboard

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/cajasmota/grafel/internal/daemon"
	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/registry"
)

// ── /api/groups/:g/stats ─────────────────────────────────────────────────────

// handleGroupStats — GET /api/groups/:g/stats?ref=<ref>
//
// Returns aggregate entity + relationship counts for all repos in the group,
// optionally scoped to a specific git ref. Supports ?ref=@all to sum across
// all indexed refs.
func (s *Server) handleGroupStats(w http.ResponseWriter, r *http.Request) {
	group := r.PathValue("group")
	if group == "" {
		writeErr(w, http.StatusBadRequest, "group required")
		return
	}

	ref, isAll, ok := resolveRefParam(w, r, group)
	if !ok {
		return
	}

	if isAll {
		// Aggregate stats across all indexed refs.
		refs := knownRefsForGroup(group)
		if len(refs) == 0 {
			writeJSON(w, http.StatusOK, map[string]any{
				"group":               group,
				"ref":                 "@all",
				"refs_included":       []string{},
				"total_entities":      0,
				"total_relationships": 0,
				"repos":               []any{},
			})
			return
		}
		type repoStat struct {
			Slug          string `json:"slug"`
			Entities      int    `json:"entities"`
			Relationships int    `json:"relationships"`
		}
		repoTotals := map[string]*repoStat{}
		var totalEntities, totalRels int

		for _, refName := range refs {
			grp, err := s.graphs.GetGroupForRef(group, refName)
			if err != nil {
				continue
			}
			for slug, dr := range grp.Repos {
				if dr == nil || dr.Doc == nil {
					continue
				}
				if _, seen := repoTotals[slug]; !seen {
					repoTotals[slug] = &repoStat{Slug: slug}
				}
				repoTotals[slug].Entities += len(dr.Doc.Entities)
				repoTotals[slug].Relationships += len(dr.Doc.Relationships)
				totalEntities += len(dr.Doc.Entities)
				totalRels += len(dr.Doc.Relationships)
			}
		}

		repoList := make([]repoStat, 0, len(repoTotals))
		for _, rs := range repoTotals {
			repoList = append(repoList, *rs)
		}
		sort.Slice(repoList, func(i, j int) bool { return repoList[i].Slug < repoList[j].Slug })

		writeJSON(w, http.StatusOK, map[string]any{
			"group":               group,
			"ref":                 "@all",
			"refs_included":       refs,
			"total_entities":      totalEntities,
			"total_relationships": totalRels,
			"repos":               repoList,
		})
		return
	}

	grp, err := s.graphs.GetGroupForRef(group, ref)
	if err != nil {
		writeErr(w, http.StatusNotFound, err.Error())
		return
	}

	type repoStat struct {
		Slug          string `json:"slug"`
		Entities      int    `json:"entities"`
		Relationships int    `json:"relationships"`
	}
	var totalEntities, totalRels int
	repoList := make([]repoStat, 0, len(grp.Repos))
	for slug, dr := range grp.Repos {
		if dr == nil || dr.Doc == nil {
			continue
		}
		repoList = append(repoList, repoStat{
			Slug:          slug,
			Entities:      len(dr.Doc.Entities),
			Relationships: len(dr.Doc.Relationships),
		})
		totalEntities += len(dr.Doc.Entities)
		totalRels += len(dr.Doc.Relationships)
	}
	sort.Slice(repoList, func(i, j int) bool { return repoList[i].Slug < repoList[j].Slug })

	refLabel := ref
	if refLabel == "" {
		refLabel = "current"
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"group":               group,
		"ref":                 refLabel,
		"total_entities":      totalEntities,
		"total_relationships": totalRels,
		"repos":               repoList,
	})
}

// ── /api/groups/:g/repos/:r/entities ─────────────────────────────────────────

// handleRepoEntities — GET /api/groups/:g/repos/:r/entities?ref=<ref>
//
// Returns the entity list for a single repo, optionally scoped to a git ref.
// ?ref=@all returns the union of entities across all indexed refs (by entity ID,
// last-ref-wins for deduplication).
func (s *Server) handleRepoEntities(w http.ResponseWriter, r *http.Request) {
	group := r.PathValue("group")
	repo := r.PathValue("repo")
	if group == "" || repo == "" {
		writeErr(w, http.StatusBadRequest, "group and repo required")
		return
	}

	ref, isAll, ok := resolveRefParam(w, r, group)
	if !ok {
		return
	}

	if isAll {
		refs := allRefsForGroupRepo(group, repo)
		seen := map[string]map[string]any{}
		for _, refName := range refs {
			grp, err := s.graphs.GetGroupForRef(group, refName)
			if err != nil {
				continue
			}
			dr, ok := grp.Repos[repo]
			if !ok || dr == nil || dr.Doc == nil {
				continue
			}
			for i := range dr.Doc.Entities {
				e := &dr.Doc.Entities[i]
				seen[e.ID] = serializeEntity(repo, e)
			}
		}
		entities := make([]map[string]any, 0, len(seen))
		for _, e := range seen {
			entities = append(entities, e)
		}
		sortEntitySlice(entities)
		writeJSON(w, http.StatusOK, map[string]any{
			"group":    group,
			"repo":     repo,
			"ref":      "@all",
			"entities": entities,
			"count":    len(entities),
		})
		return
	}

	grp, err := s.graphs.GetGroupForRef(group, ref)
	if err != nil {
		writeErr(w, http.StatusNotFound, err.Error())
		return
	}
	dr, ok2 := grp.Repos[repo]
	if !ok2 || dr == nil {
		writeErr(w, http.StatusNotFound, "repo not found: "+repo)
		return
	}
	if dr.Doc == nil {
		writeErr(w, http.StatusNotFound, "repo graph not loaded: "+repo)
		return
	}

	entities := make([]map[string]any, 0, len(dr.Doc.Entities))
	for i := range dr.Doc.Entities {
		entities = append(entities, serializeEntity(repo, &dr.Doc.Entities[i]))
	}
	sortEntitySlice(entities)

	refLabel := ref
	if refLabel == "" {
		refLabel = "current"
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"group":    group,
		"repo":     repo,
		"ref":      refLabel,
		"entities": entities,
		"count":    len(entities),
	})
}

// ── /api/groups/:g/repos/:r/relationships ────────────────────────────────────

// relEntry is the wire shape for a single relationship.
type relEntry struct {
	Source string `json:"source"`
	Target string `json:"target"`
	Kind   string `json:"kind"`
}

// handleRepoRelationships — GET /api/groups/:g/repos/:r/relationships?ref=<ref>
func (s *Server) handleRepoRelationships(w http.ResponseWriter, r *http.Request) {
	group := r.PathValue("group")
	repo := r.PathValue("repo")
	if group == "" || repo == "" {
		writeErr(w, http.StatusBadRequest, "group and repo required")
		return
	}

	ref, isAll, ok := resolveRefParam(w, r, group)
	if !ok {
		return
	}

	collectRels := func(doc *graph.Document) []relEntry {
		out := make([]relEntry, 0, len(doc.Relationships))
		for _, rel := range doc.Relationships {
			out = append(out, relEntry{
				Source: dashPrefixedID(repo, rel.FromID),
				Target: dashPrefixedID(repo, rel.ToID),
				Kind:   dashStripScopePrefix(rel.Kind),
			})
		}
		return out
	}

	if isAll {
		refs := allRefsForGroupRepo(group, repo)
		type dedupeKey struct{ src, tgt, kind string }
		seenRels := map[dedupeKey]bool{}
		var rels []relEntry
		for _, refName := range refs {
			grp, err := s.graphs.GetGroupForRef(group, refName)
			if err != nil {
				continue
			}
			dr, ok2 := grp.Repos[repo]
			if !ok2 || dr == nil || dr.Doc == nil {
				continue
			}
			for _, rel := range collectRels(dr.Doc) {
				k := dedupeKey{rel.Source, rel.Target, rel.Kind}
				if !seenRels[k] {
					seenRels[k] = true
					rels = append(rels, rel)
				}
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"group":         group,
			"repo":          repo,
			"ref":           "@all",
			"relationships": rels,
			"count":         len(rels),
		})
		return
	}

	grp, err := s.graphs.GetGroupForRef(group, ref)
	if err != nil {
		writeErr(w, http.StatusNotFound, err.Error())
		return
	}
	dr, ok2 := grp.Repos[repo]
	if !ok2 || dr == nil {
		writeErr(w, http.StatusNotFound, "repo not found: "+repo)
		return
	}
	if dr.Doc == nil {
		writeErr(w, http.StatusNotFound, "repo graph not loaded: "+repo)
		return
	}

	rels := collectRels(dr.Doc)
	refLabel := ref
	if refLabel == "" {
		refLabel = "current"
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"group":         group,
		"repo":          repo,
		"ref":           refLabel,
		"relationships": rels,
		"count":         len(rels),
	})
}

// ── /api/groups/:g/repos/:r/cross-repo-edges ─────────────────────────────────

// handleRepoCrossRepoEdges — GET /api/groups/:g/repos/:r/cross-repo-edges?ref=<ref>
//
// Returns the cross-repo edges (from grp.Links) that involve the requested repo
// as source or target. ?ref=@all is honoured (same link file, but groups edges
// per indexed-ref set).
func (s *Server) handleRepoCrossRepoEdges(w http.ResponseWriter, r *http.Request) {
	group := r.PathValue("group")
	repo := r.PathValue("repo")
	if group == "" || repo == "" {
		writeErr(w, http.StatusBadRequest, "group and repo required")
		return
	}

	ref, isAll, ok := resolveRefParam(w, r, group)
	if !ok {
		return
	}

	// cross-repo links are stored group-wide, not per-ref. We still load the
	// group for the requested ref so the node-existence check is ref-scoped.
	loadRef := ref
	if isAll {
		loadRef = "" // load canonical ref; links are group-wide anyway
	}

	grp, err := s.graphs.GetGroupForRef(group, loadRef)
	if err != nil {
		writeErr(w, http.StatusNotFound, err.Error())
		return
	}

	// Filter links that touch this repo.
	prefix := repo + "::"
	type edgeEntry struct {
		Source     string  `json:"source"`
		Target     string  `json:"target"`
		Kind       string  `json:"kind"`
		Confidence float64 `json:"confidence,omitempty"`
	}
	edges := make([]edgeEntry, 0)
	for _, l := range grp.Links {
		if strings.HasPrefix(l.Source, prefix) || strings.HasPrefix(l.Target, prefix) {
			edges = append(edges, edgeEntry{
				Source:     l.Source,
				Target:     l.Target,
				Kind:       l.Kind,
				Confidence: l.Confidence,
			})
		}
	}

	refLabel := ref
	if isAll {
		refLabel = "@all"
	} else if refLabel == "" {
		refLabel = "current"
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"group": group,
		"repo":  repo,
		"ref":   refLabel,
		"edges": edges,
		"count": len(edges),
	})
}

// ── /api/groups/:g/repos/:r/orphans ──────────────────────────────────────────

// handleRepoOrphans — GET /api/groups/:g/repos/:r/orphans?ref=<ref>
//
// Returns entities that have no inbound or outbound relationships within the
// repo's graph. ?ref=@all unions orphans across all indexed refs.
func (s *Server) handleRepoOrphans(w http.ResponseWriter, r *http.Request) {
	group := r.PathValue("group")
	repo := r.PathValue("repo")
	if group == "" || repo == "" {
		writeErr(w, http.StatusBadRequest, "group and repo required")
		return
	}

	ref, isAll, ok := resolveRefParam(w, r, group)
	if !ok {
		return
	}

	findOrphans := func(doc *graph.Document) []map[string]any {
		// Build a set of entity IDs that appear in at least one relationship.
		connected := make(map[string]bool, len(doc.Relationships)*2)
		for _, rel := range doc.Relationships {
			connected[rel.FromID] = true
			connected[rel.ToID] = true
		}
		var out []map[string]any
		for i := range doc.Entities {
			e := &doc.Entities[i]
			if !connected[e.ID] {
				out = append(out, serializeEntity(repo, e))
			}
		}
		return out
	}

	if isAll {
		refs := allRefsForGroupRepo(group, repo)
		seen := map[string]map[string]any{}
		for _, refName := range refs {
			grp, err := s.graphs.GetGroupForRef(group, refName)
			if err != nil {
				continue
			}
			dr, ok := grp.Repos[repo]
			if !ok || dr == nil || dr.Doc == nil {
				continue
			}
			for _, e := range findOrphans(dr.Doc) {
				id, _ := e["id"].(string)
				seen[id] = e
			}
		}
		orphans := make([]map[string]any, 0, len(seen))
		for _, e := range seen {
			orphans = append(orphans, e)
		}
		sortEntitySlice(orphans)
		writeJSON(w, http.StatusOK, map[string]any{
			"group":   group,
			"repo":    repo,
			"ref":     "@all",
			"orphans": orphans,
			"count":   len(orphans),
		})
		return
	}

	grp, err := s.graphs.GetGroupForRef(group, ref)
	if err != nil {
		writeErr(w, http.StatusNotFound, err.Error())
		return
	}
	dr, ok2 := grp.Repos[repo]
	if !ok2 || dr == nil {
		writeErr(w, http.StatusNotFound, "repo not found: "+repo)
		return
	}
	if dr.Doc == nil {
		writeErr(w, http.StatusNotFound, "repo graph not loaded: "+repo)
		return
	}

	orphans := findOrphans(dr.Doc)
	if orphans == nil {
		orphans = []map[string]any{}
	}
	sortEntitySlice(orphans)

	refLabel := ref
	if refLabel == "" {
		refLabel = "current"
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"group":   group,
		"repo":    repo,
		"ref":     refLabel,
		"orphans": orphans,
		"count":   len(orphans),
	})
}

// ── /api/groups/:g/repos/:r/patterns ─────────────────────────────────────────

// handleRepoPatterns — GET /api/groups/:g/repos/:r/patterns?ref=<ref>
//
// Patterns are stored group-wide (not per-repo, not per-ref) in the agent-
// patterns store. ?ref= is accepted for API consistency but does not change
// the returned data — patterns are ref-agnostic by design (they describe
// coding idioms, not graph snapshots).
func (s *Server) handleRepoPatterns(w http.ResponseWriter, r *http.Request) {
	group := r.PathValue("group")
	repo := r.PathValue("repo")
	if group == "" || repo == "" {
		writeErr(w, http.StatusBadRequest, "group and repo required")
		return
	}

	ref, _, ok := resolveRefParam(w, r, group)
	if !ok {
		return
	}

	dir := groupPatternsDir(group)
	patterns, err := loadAgentPatterns(dir)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "load patterns: "+err.Error())
		return
	}

	refLabel := ref
	if refLabel == "" {
		refLabel = "current"
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"group":    group,
		"repo":     repo,
		"ref":      refLabel,
		"patterns": patterns,
		"count":    len(patterns),
	})
}

// ── /api/groups/:g/refs ───────────────────────────────────────────────────────

// groupRefEntry is one ref entry in the GET /api/groups/:g/refs response.
type groupRefEntry struct {
	Name          string     `json:"name"`
	IsCanonical   bool       `json:"is_canonical"`
	LastIndexedAt *time.Time `json:"last_indexed_at,omitempty"`
	EntityCount   int        `json:"entity_count,omitempty"`
	IsHot         bool       `json:"is_hot"`
}

// handleGroupRefs — GET /api/groups/:g/refs
//
// Lists all refs available for the group with last-indexed timestamp and entity
// counts per ref. This is the simpler, non-v2-prefixed companion to
// /api/v2/groups/{group}/refs (which carries per-repo detail and tier info).
//
// Response:
//
//	{
//	  "group": "acme",
//	  "refs": [
//	    {"name": "main", "is_canonical": true,  "last_indexed_at": "...", "entity_count": 12345, "is_hot": true},
//	    {"name": "feat/foo", "is_canonical": false, "last_indexed_at": "...", "entity_count": 12400, "is_hot": false}
//	  ]
//	}
func (s *Server) handleGroupRefs(w http.ResponseWriter, r *http.Request) {
	group := r.PathValue("group")
	if group == "" {
		writeErr(w, http.StatusBadRequest, "group required")
		return
	}

	groups, err := registry.Groups()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "registry error: "+err.Error())
		return
	}
	var cfgPath string
	for _, g := range groups {
		if g.Name == group {
			cfgPath = g.ConfigPath
			break
		}
	}
	if cfgPath == "" {
		writeErr(w, http.StatusNotFound, "group not registered: "+group)
		return
	}
	cfg, err := registry.LoadGroupConfig(cfgPath)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "config error: "+err.Error())
		return
	}

	// Aggregate per-ref stats across all repos in the group.
	type refAgg struct {
		lastIndexed time.Time
		entities    int
		hotCount    int // number of repos where this ref is "hot"
		repoCount   int
	}
	agg := map[string]*refAgg{}

	for _, repo := range cfg.Repos {
		sentinel := daemon.StateDirForRepoRef(repo.Path, "")
		refsDir := filepath.Dir(sentinel)
		entries, err := os.ReadDir(refsDir)
		if err != nil {
			continue
		}
		// Determine the hot ref for this repo.
		hotDir := daemon.StateDirForRepo(repo.Path)
		hotRefSafe := filepath.Base(hotDir)

		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			refSafe := e.Name()
			refName := daemon.RefSafeDecode(refSafe)
			if refName == "" {
				continue
			}

			// #5891, #5915 J2 slice-2: CurrentGraphMtime resolves the active
			// graph's mtime segment-set aware (manifest.json), unlike
			// os.Stat(graph.CurrentGraphPath(...)) which only ever resolved a
			// flat .fb path and reported a segment-set ref as absent.
			refDir := filepath.Join(refsDir, refSafe)
			var mtime time.Time
			if mt, ok := graph.CurrentGraphMtime(refDir); ok {
				mtime = mt
			} else {
				// No active fb graph — try graph.json.
				jsonPath := filepath.Join(refsDir, refSafe, "graph.json")
				fi, ferr := os.Stat(jsonPath)
				if ferr != nil {
					continue
				}
				mtime = fi.ModTime()
			}

			// Entity count from sidecar.
			var entityCount int
			statsPath := filepath.Join(refsDir, refSafe, "graph-stats.json")
			if data, serr := os.ReadFile(statsPath); serr == nil {
				var stats graph.GraphStatsSidecar
				if jsonErr := json.Unmarshal(data, &stats); jsonErr == nil {
					entityCount = stats.TotalEntities
				}
			}

			if _, ok := agg[refName]; !ok {
				agg[refName] = &refAgg{}
			}
			a := agg[refName]
			a.repoCount++
			a.entities += entityCount
			if mtime.After(a.lastIndexed) {
				a.lastIndexed = mtime
			}
			if refSafe == hotRefSafe {
				a.hotCount++
			}
		}
	}

	// Determine canonical ref (the one that is "hot" for the most repos,
	// defaulting to "main" when present).
	canonicalRef := ""
	if _, ok := agg["main"]; ok {
		canonicalRef = "main"
	}
	if canonicalRef == "" {
		// Pick the ref that is hot for the most repos.
		maxHot := -1
		for name, a := range agg {
			if a.hotCount > maxHot {
				maxHot = a.hotCount
				canonicalRef = name
			}
		}
	}

	refs := make([]groupRefEntry, 0, len(agg))
	for name, a := range agg {
		entry := groupRefEntry{
			Name:        name,
			IsCanonical: name == canonicalRef,
			EntityCount: a.entities,
			IsHot:       a.hotCount > 0,
		}
		if !a.lastIndexed.IsZero() {
			t := a.lastIndexed.UTC()
			entry.LastIndexedAt = &t
		}
		refs = append(refs, entry)
	}

	// Sort: canonical first, then alphabetical.
	sort.Slice(refs, func(i, j int) bool {
		if refs[i].IsCanonical != refs[j].IsCanonical {
			return refs[i].IsCanonical
		}
		return refs[i].Name < refs[j].Name
	})

	writeJSON(w, http.StatusOK, map[string]any{
		"group": group,
		"refs":  refs,
	})
}

// ── helpers ──────────────────────────────────────────────────────────────────

// allRefsForGroupRepo returns all indexed ref names for a specific repo slug
// within a group. Falls back to all-group refs when the repo is not found.
func allRefsForGroupRepo(groupName, repoSlug string) []string {
	groups, err := registry.Groups()
	if err != nil {
		return nil
	}
	for _, g := range groups {
		if g.Name != groupName {
			continue
		}
		cfg, err := registry.LoadGroupConfig(g.ConfigPath)
		if err != nil {
			return nil
		}
		for _, repo := range cfg.Repos {
			if repo.Slug == repoSlug {
				return allRefsForRepo(repo.Path)
			}
		}
	}
	return nil
}

// sortEntitySlice sorts a slice of serialized entity maps by the "id" field.
func sortEntitySlice(entities []map[string]any) {
	sort.Slice(entities, func(i, j int) bool {
		a, _ := entities[i]["id"].(string)
		b, _ := entities[j]["id"].(string)
		return a < b
	})
}
