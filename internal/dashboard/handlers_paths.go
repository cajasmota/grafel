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
	"github.com/cajasmota/archigraph/internal/types"
)

const (
	httpEndpointKind           = "http_endpoint"
	httpEndpointDefinitionKind = "http_endpoint_definition"
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
	// Enrichment operation fields (#1103). Populated when LLM enrichment
	// frontmatter exists for the path: explicit Rank promotes the row to the
	// top of the list; Group + GroupLabel cluster it in the sidebar; Aliases
	// lists merged-away peer path hashes; Disqualified rows are moved to the
	// `rejected_paths` slice in the list response.
	Rank         float64  `json:"rank,omitempty"`
	Group        string   `json:"group,omitempty"`
	GroupLabel   string   `json:"group_label,omitempty"`
	Aliases      []string `json:"aliases,omitempty"`
	Disqualified bool     `json:"disqualified,omitempty"`
	MergedInto   string   `json:"merged_into,omitempty"`
}

// PathTreeNode is one node in the hierarchical prefix tree.
type PathTreeNode struct {
	Segment  string         `json:"segment"`
	Path     string         `json:"path"`
	Children []PathTreeNode `json:"children,omitempty"`
	HasPaths bool           `json:"has_paths"`
}

// BackendEndpointRow is one endpoint definition inside a BackendGroup.
type BackendEndpointRow struct {
	PathHash        string   `json:"path_hash"`
	Path            string   `json:"path"`
	Verbs           []string `json:"verbs"`
	Handlers        []string `json:"handlers"`
	Multiplicity    int      `json:"multiplicity"`
	Frameworks      []string `json:"frameworks"`
	IsWebhook       bool     `json:"is_webhook"`
	Repos           []string `json:"repos"`
	OwningBackend   string   `json:"owning_backend"`
	CrossBackendRef bool     `json:"cross_backend_ref,omitempty"`
}

// BackendGroup groups endpoint definitions that belong to a single backend service.
type BackendGroup struct {
	Name          string               `json:"name"`
	EndpointCount int                  `json:"endpoint_count"`
	ServiceType   string               `json:"service_type,omitempty"`
	Repos         []string             `json:"repos"`
	Endpoints     []BackendEndpointRow `json:"endpoints"`
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

	// Collect all http_endpoint / http_endpoint_definition entities across repos.
	type rawEndpoint struct {
		ID            string
		Path          string
		Verb          string
		Handler       string
		Framework     string
		IsWebhook     bool
		Repo          string
		SourceFile    string
		StartLine     int
		OwningBackend string
		IsDefinition  bool // true when kind == http_endpoint_definition
	}

	var endpoints []rawEndpoint
	for _, r := range sortedRepos(grp) {
		if filterRepo != "" && r.Slug != filterRepo {
			continue
		}
		for i := range r.Doc.Entities {
			e := &r.Doc.Entities[i]
			// #1217: accept all three http endpoint kind strings for backward
			// compat with graphs indexed before the split. Exclude call-site
			// synthetics (consumer side) — they belong in the Orphan Callers tab.
			kind := dashStripScopePrefix(e.Kind)
			isDefinition := strings.EqualFold(kind, httpEndpointDefinitionKind)
			isHTTPEndpoint := types.IsHTTPEndpointKind(kind) ||
				strings.EqualFold(kind, httpEndpointKind) ||
				e.Kind == "Endpoint" || e.Kind == "Route"
			if !isHTTPEndpoint {
				continue
			}
			// Exclude call-site entities (new kind) and legacy consumer-side synthetics.
			if e.Kind == "http_endpoint_call" ||
				e.Properties["pattern_type"] == "http_endpoint_client_synthesis" {
				continue
			}
			path := e.Properties["path"]
			if path == "" {
				path = e.Name
			}
			// Issue #1125 — drop XML namespace XPath strings (e.g.
			// `./w:tblBorders`) that leaked into the entity graph via YAML
			// rules firing on python-docx / lxml code. HTTP paths never
			// start with "./" or contain short-alphabetic-prefix colons.
			if !isHTTPEndpointPath(path) {
				continue
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
			owningBackend := e.Properties["owning_backend"]
			if owningBackend == "" {
				// Fallback heuristic: use the handler name prefix or repo slug
				// to infer a backend name when the property is absent (#1217
				// may not have landed yet).
				owningBackend = inferOwningBackend(e.Name, r.Slug)
			}
			endpoints = append(endpoints, rawEndpoint{
				ID:            dashPrefixedID(r.Slug, e.ID),
				Path:          path,
				Verb:          verb,
				Handler:       e.Name,
				Framework:     framework,
				IsWebhook:     isWebhook,
				Repo:          r.Slug,
				SourceFile:    e.SourceFile,
				StartLine:     e.StartLine,
				OwningBackend: owningBackend,
				IsDefinition:  isDefinition,
			})
		}
	}

	// Group by path (flat list — kept for backward compat).
	type pathKey = string
	grouped := map[pathKey]*PathRow{}
	pathOrder := []string{}

	// Also track per-path owning backend (first seen wins for the flat list).
	pathBackend := map[pathKey]string{}

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
			pathBackend[ep.Path] = ep.OwningBackend
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

	// --- LLM enrichment operations (#1103) -----------------------------------
	// merge / disqualify / rank / group applied to the per-path-hash row set.
	// We index ops by PathHash; frontmatter MUST set entity_id to the path
	// hash (or the helper MatchesEntity will fuzzy-match repo-prefixed IDs).
	docgenState, _ := mcp.LoadDocgenState(group)
	ops := LoadEnrichmentOpsForGroup(group, docgenState)
	rows, rejectedRows := applyPathEnrichment(rows, ops)
	keptIDs := make([]string, 0, len(rows))
	for _, r := range rows {
		keptIDs = append(keptIDs, r.PathHash)
	}
	pathGroups := ops.SummarizeGroups(keptIDs)

	// Build prefix tree from the full filtered set.
	tree := buildPrefixTree(rows)

	// Cap at 10000 to prevent unbounded responses
	maxRows := 10000
	if len(rows) > maxRows {
		rows = rows[:maxRows]
	}

	total := len(rows)

	// Build owning_backends grouping (new response field — sub-B #1218).
	// Convert local rawEndpoint slice to the exported type for the grouping helper.
	groupingEps := make([]rawEndpointForGrouping, len(endpoints))
	for i, ep := range endpoints {
		groupingEps[i] = rawEndpointForGrouping{
			Path:          ep.Path,
			Repo:          ep.Repo,
			OwningBackend: ep.OwningBackend,
		}
	}
	owningBackends := buildOwningBackends(rows, groupingEps, pathBackend)

	writeJSON(w, http.StatusOK, map[string]any{
		"paths":           rows,
		"tree":            tree,
		"total":           total,
		"owning_backends": owningBackends,
		"rejected_paths":  rejectedRows,
		"path_groups":     pathGroups,
	})
}

// applyPathEnrichment runs the four enrichment operations (merge, disqualify,
// rank, group) across a slice of PathRow values.
//
// Matching: ops are keyed by entity_id frontmatter, which for path rows is
// either the path_hash OR a repo-prefixed handler id. We do a two-pass index:
//
//  1. exact PathHash → ops lookup;
//  2. for any frontmatter entry whose entity_id contains the path string, the
//     enrichment is attached to the matching row (handles the common case
//     where /generate-docs emits entity_id = "<repo>:<path>" instead of a hash).
//
// Returns (kept, rejected). The kept slice is stable-sorted by explicit Rank
// (desc) so an LLM rank override floats a row to the top while preserving the
// alphabetical tie-break for unranked rows.
func applyPathEnrichment(rows []PathRow, ops *EnrichmentOps) (kept, rejected []PathRow) {
	kept = make([]PathRow, 0, len(rows))
	rejected = make([]PathRow, 0)
	if ops == nil {
		return rows, rejected
	}

	// Index path-hash → row index so merge alias attachment is O(1).
	hashIdx := map[string]int{}
	for i, r := range rows {
		hashIdx[r.PathHash] = i
	}

	// Decide per row.
	dropped := map[int]bool{}
	for i := range rows {
		r := &rows[i]
		if ops.IsDisqualified(r.PathHash) {
			r.Disqualified = true
			rejected = append(rejected, *r)
			dropped[i] = true
			continue
		}
		if dst, merged := ops.MergedInto[r.PathHash]; merged && dst != r.PathHash {
			if canonIdx, ok := hashIdx[dst]; ok {
				rows[canonIdx].Aliases = append(rows[canonIdx].Aliases, r.PathHash)
				dropped[i] = true
				continue
			}
			r.MergedInto = dst
		}
		if rk := ops.Rank(r.PathHash); rk != 0 {
			r.Rank = rk
		}
		if g := ops.Group(r.PathHash); g != "" {
			r.Group = g
			r.GroupLabel = ops.GroupLabels[g]
		}
	}
	for i, r := range rows {
		if dropped[i] {
			continue
		}
		kept = append(kept, r)
	}

	// Stable-sort: explicit rank desc, then preserve original order.
	if len(ops.Ranks) > 0 {
		sort.SliceStable(kept, func(i, j int) bool {
			return kept[i].Rank > kept[j].Rank
		})
	}
	return kept, rejected
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
		ID              string                 `json:"id"`
		Verb            string                 `json:"verb"`
		Path            string                 `json:"path"`
		Handler         string                 `json:"handler"`
		Framework       string                 `json:"framework,omitempty"`
		IsWebhook       bool                   `json:"is_webhook,omitempty"`
		ResponseKeys    []string               `json:"response_keys,omitempty"`
		StatusCodes     []int                  `json:"status_codes,omitempty"`
		InboundFetches  []string               `json:"inbound_fetches,omitempty"`
		OutboundQueries []string               `json:"outbound_queries,omitempty"`
		Repo            string                 `json:"repo"`
		SourceFile      string                 `json:"source_file"`
		StartLine       int                    `json:"start_line"`
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
			// #1217 backward compat — accept all three http endpoint kinds.
			isHTTPEndpoint2 := types.IsHTTPEndpointKind(dashStripScopePrefix(e.Kind)) ||
				strings.EqualFold(dashStripScopePrefix(e.Kind), httpEndpointKind) ||
				e.Kind == "Endpoint" || e.Kind == "Route"
			if !isHTTPEndpoint2 {
				continue
			}
			// Exclude call-site entities — they are not real endpoints.
			if e.Kind == "http_endpoint_call" ||
				e.Properties["pattern_type"] == "http_endpoint_client_synthesis" {
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
		Verb        string   `json:"verb"`
		Keys        []string `json:"keys"`
		Dynamic     bool     `json:"dynamic"`
		StatusCodes []int    `json:"status_codes"`
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
		"path":             pathStr,
		"path_hash":        pathHash,
		"verbs":            verbs,
		"handlers":         handlers,
		"response_shapes":  responseShapes,
		"inbound_fetches":  inboundFetches,
		"outbound_queries": outboundQueries,
		"is_webhook":       isWebhook,
		"webhook_provider": webhookProvider,
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

// buildOwningBackends groups filtered path rows by owning_backend, computes
// aggregates, infers service_type, and sets cross_backend_ref for endpoints
// referenced from another backend's repo. Backends are sorted by endpoint
// count descending.
//
// rawEps is the full (unfiltered) endpoint slice used to detect cross-backend
// references. pathBackend maps path strings to their primary owning backend.
func buildOwningBackends(rows []PathRow, rawEps []rawEndpointForGrouping, pathBackend map[string]string) []BackendGroup {
	// backend name → BackendGroup builder state
	type backendState struct {
		name      string
		endpoints map[string]*BackendEndpointRow // keyed by path
		repoSet   map[string]bool
	}
	states := map[string]*backendState{}
	order := []string{}

	for _, row := range rows {
		backend := pathBackend[row.Path]
		if backend == "" {
			backend = "unknown"
		}
		if _, ok := states[backend]; !ok {
			states[backend] = &backendState{
				name:      backend,
				endpoints: map[string]*BackendEndpointRow{},
				repoSet:   map[string]bool{},
			}
			order = append(order, backend)
		}
		st := states[backend]
		if _, ok := st.endpoints[row.Path]; !ok {
			st.endpoints[row.Path] = &BackendEndpointRow{
				PathHash:      row.PathHash,
				Path:          row.Path,
				Verbs:         row.Verbs,
				Handlers:      row.Handlers,
				Multiplicity:  row.Multiplicity,
				Frameworks:    row.Frameworks,
				IsWebhook:     row.IsWebhook,
				Repos:         row.Repos,
				OwningBackend: backend,
			}
		}
		for _, repo := range row.Repos {
			st.repoSet[repo] = true
		}
	}

	// Build a set of (path, backend) pairs so we can detect cross-backend refs:
	// an endpoint is cross-backend if another backend's repo also references it.
	// We use the raw endpoints to find repos that reference each path.
	pathRepoBackend := map[string]map[string]string{} // path → repo → owning_backend
	for _, ep := range rawEps {
		if _, ok := pathRepoBackend[ep.Path]; !ok {
			pathRepoBackend[ep.Path] = map[string]string{}
		}
		pathRepoBackend[ep.Path][ep.Repo] = ep.OwningBackend
	}

	// Mark cross-backend refs and collect service types.
	for _, st := range states {
		for path, epRow := range st.endpoints {
			repoBackends := pathRepoBackend[path]
			for _, rb := range repoBackends {
				if rb != st.name {
					epRow.CrossBackendRef = true
					break
				}
			}
		}
	}

	// Convert to slice, infer service_type, sort by endpoint_count desc.
	groups := make([]BackendGroup, 0, len(states))
	for _, name := range order {
		st := states[name]
		eps := make([]BackendEndpointRow, 0, len(st.endpoints))
		for _, ep := range st.endpoints {
			eps = append(eps, *ep)
		}
		// Sort endpoints within each backend by path for determinism.
		sort.Slice(eps, func(i, j int) bool { return eps[i].Path < eps[j].Path })

		repos := make([]string, 0, len(st.repoSet))
		for r := range st.repoSet {
			repos = append(repos, r)
		}
		sort.Strings(repos)

		groups = append(groups, BackendGroup{
			Name:          name,
			EndpointCount: len(eps),
			ServiceType:   inferServiceType(name, repos),
			Repos:         repos,
			Endpoints:     eps,
		})
	}
	sort.Slice(groups, func(i, j int) bool {
		if groups[i].EndpointCount != groups[j].EndpointCount {
			return groups[i].EndpointCount > groups[j].EndpointCount
		}
		return groups[i].Name < groups[j].Name
	})
	return groups
}

// rawEndpointForGrouping is an alias so buildOwningBackends can access the
// rawEndpoint fields without the local type escaping the function scope.
// We re-declare a minimal struct here to avoid coupling to the local type
// declared inside handlePathsList.
type rawEndpointForGrouping struct {
	Path          string
	Repo          string
	OwningBackend string
}

// inferOwningBackend derives a backend name from an entity name or repo slug
// when the owning_backend property is absent (pre-#1217 graphs).
//
// Heuristic: if the handler name contains a recognisable suffix like "Handler",
// "Controller", "Router", or "Service" preceded by a capitalised word, strip
// the suffix and lower-case the root.  Otherwise fall back to the repo slug.
func inferOwningBackend(handlerName, repoSlug string) string {
	suffixes := []string{"Handler", "Controller", "Router", "Service", "View"}
	for _, suf := range suffixes {
		if idx := strings.Index(handlerName, suf); idx > 0 {
			root := handlerName[:idx]
			if root != "" {
				return strings.ToLower(root)
			}
		}
	}
	return repoSlug
}

// inferServiceType returns a best-guess service_type label for a backend based
// on its name and repo list.  Categories: api, web, cron, worker.
func inferServiceType(backendName string, repos []string) string {
	combined := strings.ToLower(backendName)
	for _, r := range repos {
		combined += " " + strings.ToLower(r)
	}
	switch {
	case strings.Contains(combined, "cron") || strings.Contains(combined, "scheduler") ||
		strings.Contains(combined, "beat"):
		return "cron"
	case strings.Contains(combined, "worker") || strings.Contains(combined, "celery") ||
		strings.Contains(combined, "consumer") || strings.Contains(combined, "queue"):
		return "worker"
	case strings.Contains(combined, "web") || strings.Contains(combined, "frontend") ||
		strings.Contains(combined, "ui") || strings.Contains(combined, "spa"):
		return "web"
	default:
		return "api"
	}
}

// isHTTPEndpointPath reports whether path looks like a real HTTP endpoint
// path rather than an XML namespace XPath expression or other non-HTTP
// string that leaked into the entity graph. Issue #1125.
//
// Accepted:
//   - Paths starting with "/" (absolute routes)
//   - Full URLs starting with http(s)://
//
// Rejected:
//   - Paths containing "./" (XPath relative notation)
//   - Paths containing "[@" (XPath attribute selector)
//   - Paths containing a short (≤4 char) alphabetic prefix:Name segment
//     e.g. "w:tblBorders", "xml:lang" — classic XML namespace patterns
func isHTTPEndpointPath(path string) bool {
	if path == "" {
		return false
	}
	// Full absolute URL — always valid.
	if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
		return true
	}
	// Must start with "/" for an HTTP route.
	if !strings.HasPrefix(path, "/") {
		return false
	}
	// Reject XPath relative notation.
	if strings.Contains(path, "./") {
		return false
	}
	// Reject XPath attribute selectors.
	if strings.Contains(path, "[@") {
		return false
	}
	// Reject paths with XML namespace prefix:Name segments.
	segments := strings.Split(strings.TrimPrefix(path, "/"), "/")
	for _, seg := range segments {
		colonIdx := strings.IndexByte(seg, ':')
		if colonIdx <= 0 || colonIdx == len(seg)-1 {
			continue
		}
		prefix := seg[:colonIdx]
		if len(prefix) > 4 {
			continue
		}
		allAlpha := true
		for _, c := range prefix {
			if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')) {
				allAlpha = false
				break
			}
		}
		if allAlpha {
			return false
		}
	}
	return true
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
