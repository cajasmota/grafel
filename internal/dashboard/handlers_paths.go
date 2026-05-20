package dashboard

// handlers_paths.go — API & Contracts Explorer endpoints
//
//	GET /api/paths/{group}?prefix=&q=&page=&size=&framework=&webhook=&filter_repo=
//	GET /api/paths/{group}/{pathHash}
//
// The path-grouping aggregator is the key new logic here: it groups
// http_endpoint entities by (path, verb), deduplicates DRF ViewSet expansion
// artifacts, builds a PathTreeNode prefix tree, and paginates at 50 rows/page.

import (
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/cajasmota/archigraph/internal/mcp"
)

const (
	httpEndpointKind = "http_endpoint"
	// pageSize is kept for backward-compat but the default is now 5000 (all paths).
	pageSize = 5000
)

// PathRow is one grouped API path returned by the list endpoint.
type PathRow struct {
	PathHash     string   `json:"path_hash"`
	Path         string   `json:"path"`
	Verbs        []string `json:"verbs"`
	Handlers     []string `json:"handlers"`
	Multiplicity int      `json:"multiplicity"`
	Frameworks   []string `json:"frameworks"`
	IsWebhook    bool     `json:"is_webhook"`
	Repos        []string `json:"repos"`
}

// PathTreeNode is one node in the hierarchical prefix tree.
type PathTreeNode struct {
	Segment  string         `json:"segment"`
	Path     string         `json:"path"`
	Children []PathTreeNode `json:"children,omitempty"`
	HasPaths bool           `json:"has_paths"`
}

// handlePathsList — GET /api/paths/{group}
func (s *Server) handlePathsList(w http.ResponseWriter, r *http.Request) {
	group := r.PathValue("group")
	if group == "" {
		writeErr(w, http.StatusBadRequest, "group required")
		return
	}
	q := r.URL.Query()
	prefix := q.Get("prefix")
	search := q.Get("q")
	filterFramework := q.Get("framework")
	filterWebhook := q.Get("webhook")
	filterRepo := q.Get("filter_repo")

	grp, err := s.graphs.GetGroup(group)
	if err != nil {
		writeErr(w, http.StatusNotFound, err.Error())
		return
	}

	// Collect all http_endpoint entities across repos.
	type rawEndpoint struct {
		ID         string
		Path       string
		Verb       string
		Handler    string
		Framework  string
		IsWebhook  bool
		Repo       string
		SourceFile string
		StartLine  int
	}

	var endpoints []rawEndpoint
	for _, r := range sortedRepos(grp) {
		if filterRepo != "" && r.Slug != filterRepo {
			continue
		}
		for i := range r.Doc.Entities {
			e := &r.Doc.Entities[i]
			if !strings.EqualFold(dashStripScopePrefix(e.Kind), httpEndpointKind) &&
				e.Kind != "Endpoint" && e.Kind != "Route" {
				continue
			}
			// Skip frontend-only synthetic call-site entries — those belong
			// in the Orphan Callers tab, not the Endpoints list.
			if e.Properties["pattern_type"] == "http_endpoint_client_synthesis" {
				continue
			}
			path := e.Properties["path"]
			if path == "" {
				path = e.Name
			}
			verb := strings.ToUpper(e.Properties["verb"])
			if verb == "" {
				verb = "ANY"
			}
			// DRF dedup: skip urlconf_nested_include ANY entries when a
			// drf_router_expanded entry exists for the same path.
			if verb == "ANY" && e.Properties["urlconf_nested_include"] == "true" {
				continue
			}
			framework := e.Properties["framework"]
			isWebhook := e.Properties["is_webhook"] == "true"
			endpoints = append(endpoints, rawEndpoint{
				ID:         dashPrefixedID(r.Slug, e.ID),
				Path:       path,
				Verb:       verb,
				Handler:    e.Name,
				Framework:  framework,
				IsWebhook:  isWebhook,
				Repo:       r.Slug,
				SourceFile: e.SourceFile,
				StartLine:  e.StartLine,
			})
		}
	}

	// Group by path.
	type pathKey = string
	grouped := map[pathKey]*PathRow{}
	pathOrder := []string{}

	for _, ep := range endpoints {
		if _, ok := grouped[ep.Path]; !ok {
			grouped[ep.Path] = &PathRow{
				PathHash:   hashStr(ep.Path),
				Path:       ep.Path,
				Verbs:      []string{},
				Handlers:   []string{},
				Frameworks: []string{},
				Repos:      []string{},
			}
			pathOrder = append(pathOrder, ep.Path)
		}
		pr := grouped[ep.Path]
		pr.Multiplicity++
		if !containsStr(pr.Verbs, ep.Verb) {
			pr.Verbs = append(pr.Verbs, ep.Verb)
		}
		if !containsStr(pr.Handlers, ep.Handler) {
			pr.Handlers = append(pr.Handlers, ep.Handler)
		}
		if ep.Framework != "" && !containsStr(pr.Frameworks, ep.Framework) {
			pr.Frameworks = append(pr.Frameworks, ep.Framework)
		}
		if ep.IsWebhook {
			pr.IsWebhook = true
		}
		if !containsStr(pr.Repos, ep.Repo) {
			pr.Repos = append(pr.Repos, ep.Repo)
		}
	}

	// Sort verb lists for determinism.
	sort.Strings(pathOrder)
	for _, key := range pathOrder {
		sort.Strings(grouped[key].Verbs)
	}

	// Filter.
	var rows []PathRow
	for _, key := range pathOrder {
		pr := grouped[key]
		if prefix != "" && !strings.HasPrefix(pr.Path, prefix) {
			continue
		}
		if search != "" && !strings.Contains(strings.ToLower(pr.Path), strings.ToLower(search)) &&
			!containsSubstr(pr.Handlers, search) {
			continue
		}
		if filterFramework != "" && !containsStr(pr.Frameworks, filterFramework) {
			continue
		}
		if filterWebhook == "true" && !pr.IsWebhook {
			continue
		}
		rows = append(rows, *pr)
	}

	// Build prefix tree from the full filtered set.
	tree := buildPrefixTree(rows)

	// Cap at 10000 to prevent unbounded responses
	maxRows := 10000
	if len(rows) > maxRows {
		rows = rows[:maxRows]
	}

	total := len(rows)

	writeJSON(w, http.StatusOK, map[string]any{
		"paths": rows,
		"tree":  tree,
		"total": total,
	})
}

// handlePathDetail — GET /api/paths/{group}/{pathHash}
func (s *Server) handlePathDetail(w http.ResponseWriter, r *http.Request) {
	group := r.PathValue("group")
	pathHash := r.PathValue("pathHash")
	if group == "" || pathHash == "" {
		writeErr(w, http.StatusBadRequest, "group and pathHash required")
		return
	}

	grp, err := s.graphs.GetGroup(group)
	if err != nil {
		writeErr(w, http.StatusNotFound, err.Error())
		return
	}

	// Find all endpoints with this pathHash.
	type endpointDetail struct {
		ID              string   `json:"id"`
		Verb            string   `json:"verb"`
		Path            string   `json:"path"`
		Handler         string   `json:"handler"`
		Framework       string   `json:"framework,omitempty"`
		IsWebhook       bool     `json:"is_webhook,omitempty"`
		ResponseKeys    []string `json:"response_keys,omitempty"`
		StatusCodes     []int    `json:"status_codes,omitempty"`
		InboundFetches  []string `json:"inbound_fetches,omitempty"`
		OutboundQueries []string `json:"outbound_queries,omitempty"`
		Repo            string   `json:"repo"`
		SourceFile      string   `json:"source_file"`
		StartLine       int      `json:"start_line"`
		HasDocs         bool                   `json:"has_docs,omitempty"`
		DocsSummary     string                 `json:"docs_summary,omitempty"`
		DocsPath        string                 `json:"docs_path,omitempty"`
		Enrichment      *EnrichmentFrontmatter `json:"enrichment,omitempty"`
	}

	var matched []endpointDetail
	var pathStr string
	isWebhook := false
	var webhookProvider string

	// Load docgen state for documentation enrichment.
	docgenState, _ := mcp.LoadDocgenState(group)

	for _, repo := range sortedRepos(grp) {
		for i := range repo.Doc.Entities {
			e := &repo.Doc.Entities[i]
			if !strings.EqualFold(dashStripScopePrefix(e.Kind), httpEndpointKind) &&
				e.Kind != "Endpoint" && e.Kind != "Route" {
				continue
			}
			// Skip frontend-only call-site synthetics — they are not real endpoints.
			if e.Properties["pattern_type"] == "http_endpoint_client_synthesis" {
				continue
			}
			path := e.Properties["path"]
			if path == "" {
				path = e.Name
			}
			if hashStr(path) != pathHash {
				continue
			}
			if pathStr == "" {
				pathStr = path
			}

			verb := strings.ToUpper(e.Properties["verb"])
			if verb == "" {
				verb = "ANY"
			}

			// Collect response keys.
			var respKeys []string
			if rk := e.Properties["response_keys"]; rk != "" {
				respKeys = strings.Split(rk, ",")
			}

			// Collect status codes.
			var statusCodes []int
			if sc := e.Properties["status_codes"]; sc != "" {
				for _, s := range strings.Split(sc, ",") {
					if n, err := strconv.Atoi(strings.TrimSpace(s)); err == nil {
						statusCodes = append(statusCodes, n)
					}
				}
			}

			// Collect inbound FETCHES and outbound QUERIES from edges.
			inbound := []string{}
			outbound := []string{}
			for _, rel := range repo.Doc.Relationships {
				if rel.ToID == e.ID {
					if rel.Kind == "FETCHES" {
						inbound = append(inbound, dashPrefixedID(repo.Slug, rel.FromID))
					}
				}
				if rel.FromID == e.ID {
					if rel.Kind == "QUERIES" || rel.Kind == "ACCESSES_TABLE" {
						outbound = append(outbound, dashPrefixedID(repo.Slug, rel.ToID))
					}
				}
			}

			// Track webhook status
			if e.Properties["is_webhook"] == "true" {
				isWebhook = true
				webhookProvider = e.Properties["webhook_provider"]
			}

			// Enrich with docgen data (frontmatter preferred, first-line fallback).
			hasDocs, docsSummary, docsPath, enrichment := extractEndpointDocsEnriched(group, pathHash, docgenState)

			matched = append(matched, endpointDetail{
				ID:              dashPrefixedID(repo.Slug, e.ID),
				Verb:            verb,
				Path:            path,
				Handler:         e.Name,
				Framework:       e.Properties["framework"],
				IsWebhook:       e.Properties["is_webhook"] == "true",
				ResponseKeys:    respKeys,
				StatusCodes:     statusCodes,
				InboundFetches:  inbound,
				OutboundQueries: outbound,
				Repo:            repo.Slug,
				SourceFile:      e.SourceFile,
				StartLine:       e.StartLine,
				HasDocs:         hasDocs,
				DocsSummary:     docsSummary,
				DocsPath:        docsPath,
				Enrichment:      enrichment,
			})
		}
	}

	if len(matched) == 0 {
		writeErr(w, http.StatusNotFound, "path not found: "+pathHash)
		return
	}

	// Collect all unique verbs.
	verbSet := map[string]bool{}
	for _, m := range matched {
		verbSet[m.Verb] = true
	}
	verbs := make([]string, 0, len(verbSet))
	for v := range verbSet {
		verbs = append(verbs, v)
	}
	sort.Strings(verbs)

	// Transform handlers to HandlerDetail shape with resolved entities.
	type HandlerDetail struct {
		Entity      map[string]any         `json:"entity"`
		Verb        string                 `json:"verb"`
		Framework   string                 `json:"framework,omitempty"`
		SourceFile  string                 `json:"source_file"`
		StartLine   int                    `json:"start_line"`
		Language    string                 `json:"language"`
		HasDocs     bool                   `json:"has_docs,omitempty"`
		DocsSummary string                 `json:"docs_summary,omitempty"`
		DocsPath    string                 `json:"docs_path,omitempty"`
		Enrichment  *EnrichmentFrontmatter `json:"enrichment,omitempty"`
	}

	handlers := make([]HandlerDetail, len(matched))
	for i, m := range matched {
		_, entity := findEntity(grp, m.ID)
		handlers[i] = HandlerDetail{
			Entity:      serializeEntity(m.Repo, entity),
			Verb:        m.Verb,
			Framework:   m.Framework,
			SourceFile:  m.SourceFile,
			StartLine:   m.StartLine,
			Language:    entity.Language,
			HasDocs:     m.HasDocs,
			DocsSummary: m.DocsSummary,
			DocsPath:    m.DocsPath,
			Enrichment:  m.Enrichment,
		}
	}

	// Build response_shapes from the matched endpoints.
	type ResponseShape struct {
		Verb        string `json:"verb"`
		Keys        []string `json:"keys"`
		Dynamic     bool   `json:"dynamic"`
		StatusCodes []int  `json:"status_codes"`
	}

	// Group by verb to build distinct response shapes.
	shapesByVerb := map[string]*ResponseShape{}
	for _, m := range matched {
		if _, ok := shapesByVerb[m.Verb]; !ok {
			shapesByVerb[m.Verb] = &ResponseShape{
				Verb:        m.Verb,
				Keys:        []string{},
				StatusCodes: []int{},
			}
		}
		shape := shapesByVerb[m.Verb]
		// Merge response keys (deduplicate).
		for _, k := range m.ResponseKeys {
			if !containsStr(shape.Keys, k) {
				shape.Keys = append(shape.Keys, k)
			}
		}
		// Merge status codes (deduplicate).
		for _, sc := range m.StatusCodes {
			found := false
			for _, existing := range shape.StatusCodes {
				if existing == sc {
					found = true
					break
				}
			}
			if !found {
				shape.StatusCodes = append(shape.StatusCodes, sc)
			}
		}
		// Mark as dynamic if any endpoint has dynamic response.
		if len(m.ResponseKeys) > 0 || len(m.StatusCodes) > 0 {
			// If we have any response metadata, assume it could be dynamic.
			// In a more sophisticated system, check for 'dynamic' property.
		}
	}
	responseShapes := make([]ResponseShape, 0, len(shapesByVerb))
	for _, shape := range shapesByVerb {
		sort.Ints(shape.StatusCodes)
		responseShapes = append(responseShapes, *shape)
	}
	sort.Slice(responseShapes, func(i, j int) bool {
		return responseShapes[i].Verb < responseShapes[j].Verb
	})

	// Resolve inbound_fetches and outbound_queries to Entity objects.
	inboundFetchIDs := map[string]bool{}
	outboundQueryIDs := map[string]bool{}
	for _, m := range matched {
		for _, id := range m.InboundFetches {
			inboundFetchIDs[id] = true
		}
		for _, id := range m.OutboundQueries {
			outboundQueryIDs[id] = true
		}
	}

	inboundFetches := make([]map[string]any, 0)
	for id := range inboundFetchIDs {
		_, entity := findEntity(grp, id)
		if entity != nil {
			repo, _ := dashSplitPrefixed(id)
			inboundFetches = append(inboundFetches, serializeEntity(repo, entity))
		}
	}

	outboundQueries := make([]map[string]any, 0)
	for id := range outboundQueryIDs {
		_, entity := findEntity(grp, id)
		if entity != nil {
			repo, _ := dashSplitPrefixed(id)
			outboundQueries = append(outboundQueries, serializeEntity(repo, entity))
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"path":               pathStr,
		"path_hash":          pathHash,
		"verbs":              verbs,
		"handlers":           handlers,
		"response_shapes":    responseShapes,
		"inbound_fetches":    inboundFetches,
		"outbound_queries":   outboundQueries,
		"is_webhook":         isWebhook,
		"webhook_provider":   webhookProvider,
	})
}

// buildPrefixTree constructs a hierarchical tree from the path list.
func buildPrefixTree(rows []PathRow) []PathTreeNode {
	// Collect unique segment prefixes.
	type node struct {
		children map[string]*node
		hasPaths bool
		fullPath string
	}
	root := &node{children: map[string]*node{}}

	for _, r := range rows {
		parts := strings.Split(strings.TrimPrefix(r.Path, "/"), "/")
		cur := root
		built := ""
		for _, seg := range parts {
			if seg == "" {
				continue
			}
			built += "/" + seg
			if _, ok := cur.children[seg]; !ok {
				cur.children[seg] = &node{children: map[string]*node{}, fullPath: built}
			}
			cur = cur.children[seg]
		}
		cur.hasPaths = true
	}

	var toNodes func(n *node) []PathTreeNode
	toNodes = func(n *node) []PathTreeNode {
		keys := make([]string, 0, len(n.children))
		for k := range n.children {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		out := make([]PathTreeNode, 0, len(keys))
		for _, k := range keys {
			child := n.children[k]
			tn := PathTreeNode{
				Segment:  k,
				Path:     child.fullPath,
				HasPaths: child.hasPaths,
				Children: toNodes(child),
			}
			out = append(out, tn)
		}
		return out
	}
	return toNodes(root)
}

// containsStr checks if a string slice contains a string.
func containsStr(sl []string, s string) bool {
	for _, v := range sl {
		if v == s {
			return true
		}
	}
	return false
}

// containsSubstr checks if any element of sl contains sub (case-insensitive).
func containsSubstr(sl []string, sub string) bool {
	low := strings.ToLower(sub)
	for _, v := range sl {
		if strings.Contains(strings.ToLower(v), low) {
			return true
		}
	}
	return false
}

// extractEndpointDocs reads documentation for an endpoint identified by pathHash.
// Deprecated: use extractEndpointDocsEnriched instead. Kept for callers that
// don't yet consume the EnrichmentFrontmatter field.
func extractEndpointDocs(group string, pathHash string, docgenState *mcp.DocgenState) (bool, string, string) {
	hasDocs, summary, path, _ := extractEndpointDocsEnriched(group, pathHash, docgenState)
	return hasDocs, summary, path
}

// extractEndpointDocsEnriched reads documentation for an endpoint identified
// by pathHash. It prefers YAML frontmatter when present; falls back to the
// first-line summary scan when frontmatter is absent (legacy behaviour).
//
// Returns: (hasDocs, docsSummary, docsPath, enrichment)
func extractEndpointDocsEnriched(group, pathHash string, docgenState *mcp.DocgenState) (bool, string, string, *EnrichmentFrontmatter) {
	if docgenState == nil || docgenState.GeneratedPaths == nil {
		return false, "", "", nil
	}

	for _, docPath := range docgenState.GeneratedPaths {
		if !strings.Contains(docPath, pathHash) && !strings.Contains(docPath, "endpoint") {
			continue
		}

		fullPath := getDocFilePath(group, docPath)
		fm, fallback := extractEnrichmentFromFile(fullPath)
		if fm != nil && fm.HasData() {
			return true, fm.Summary, docPath, fm
		}
		if fallback != "" {
			return true, fallback, docPath, nil
		}
		// File exists but empty — still report hasDocs=true.
		if _, err := os.Stat(fullPath); err == nil {
			return true, "", docPath, nil
		}
	}

	return false, "", "", nil
}

// getDocFilePath constructs the full file path to a generated documentation file.
// Docs are stored in ~/.archigraph/groups/<group>/docs/<docPath>
func getDocFilePath(group string, docPath string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	// Remove leading "./" if present
	docPath = strings.TrimPrefix(docPath, "./")
	return filepath.Join(home, ".archigraph", "groups", group, "docs", docPath)
}
