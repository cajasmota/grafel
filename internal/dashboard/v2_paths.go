// v2_paths.go — Paths / API & Endpoints Explorer surface for WebUI v2.
//
// Endpoints:
//
//	GET /api/v2/groups/:id/paths          → PathsListResponse (backends grouped)
//	GET /api/v2/groups/:id/paths/orphans  → OrphansResponse
//	GET /api/v2/groups/:id/paths/:hash    → PathDetail
//
// Data decision: these handlers port and shape the logic from handlers_paths.go
// (v1) into the v2 envelope format. The v1 routes are untouched. The v2 shapes
// mirror the TypeScript interfaces in webui-v2/src/data/types.ts.

package dashboard

import (
	"net/http"
	"sort"
	"strings"

	"github.com/cajasmota/archigraph/internal/mcp"
	"github.com/cajasmota/archigraph/internal/types"
)

// ---------------------------------------------------------------------------
// Wire types — mirror webui-v2/src/data/types.ts
// ---------------------------------------------------------------------------

// v2PathRoute is one route row in the grouped left-rail list.
type v2PathRoute struct {
	PathHash        string   `json:"path_hash"`
	Path            string   `json:"path"`
	Verbs           []string `json:"verbs"`
	HandlersCount   int      `json:"handlers_count"`
	Multiplicity    int      `json:"multiplicity"`
	Frameworks      []string `json:"frameworks"`
	IsWebhook       bool     `json:"is_webhook"`
	WebhookProvider string   `json:"webhook_provider,omitempty"`
	Auth            bool     `json:"auth"`
	Repos           []string `json:"repos"`
	Controller      string   `json:"controller"`
}

// v2ControllerGroup is one controller/module grouping inside a backend.
type v2ControllerGroup struct {
	ID        string        `json:"id"`
	Label     string        `json:"label"`
	File      string        `json:"file"`
	IsWebhook bool          `json:"is_webhook,omitempty"`
	Routes    []v2PathRoute `json:"routes"`
}

// v2PathBackend is one backend service section in the left rail.
type v2PathBackend struct {
	ID               string              `json:"id"`
	Label            string              `json:"label"`
	ServiceType      string              `json:"service_type"`
	Framework        string              `json:"framework"`
	Language         string              `json:"language"`
	CrossBackendRefs bool                `json:"cross_backend_refs"`
	AnyRate          int                 `json:"any_rate"`
	Groups           []v2ControllerGroup `json:"groups"`
}

// v2PathTotals is the aggregate counts shown in the sub-stats bar.
type v2PathTotals struct {
	Routes      int `json:"routes"`
	Endpoints   int `json:"endpoints"`
	Controllers int `json:"controllers"`
	Backends    int `json:"backends"`
	// Orphans is the count of frontend FETCH calls that resolve to no backend
	// handler. Surfaced here so the Orphan-callers tab can show a counter badge
	// without a second round-trip (#1551).
	Orphans int `json:"orphans"`
}

// v2PathsListResponse is the payload for GET /api/v2/groups/:id/paths.
type v2PathsListResponse struct {
	Backends []v2PathBackend `json:"backends"`
	Totals   v2PathTotals    `json:"totals"`
}

// v2PathParameter is one parameter in the detail pane.
type v2PathParameter struct {
	Name     string   `json:"name"`
	In       string   `json:"in"`
	Type     string   `json:"type"`
	Required bool     `json:"required"`
	Desc     string   `json:"desc"`
	Verbs    []string `json:"verbs,omitempty"`
}

// v2ResponseShape is one verb's response metadata.
type v2ResponseShape struct {
	Verb        string   `json:"verb"`
	StatusCodes []int    `json:"status_codes"`
	Keys        []string `json:"keys"`
	Dynamic     bool     `json:"dynamic,omitempty"`
}

// v2HandlerDetail is one handler implementation in the detail pane.
type v2HandlerDetail struct {
	Verb          string `json:"verb"`
	QualifiedName string `json:"qualified_name"`
	Framework     string `json:"framework,omitempty"`
	Repo          string `json:"repo"`
	SourceFile    string `json:"source_file"`
	StartLine     int    `json:"start_line"`
	Language      string `json:"language,omitempty"`
	HasDocs       bool   `json:"has_docs,omitempty"`
	DocsSummary   string `json:"docs_summary,omitempty"`
	DocsPath      string `json:"docs_path,omitempty"`
	Auth          string `json:"auth,omitempty"`
}

// v2PathEntity is a related entity shown in the detail sections.
type v2PathEntity struct {
	Label         string `json:"label"`
	QualifiedName string `json:"qualified_name"`
	Kind          string `json:"kind"`
	Repo          string `json:"repo"`
	SourceFile    string `json:"source_file"`
	StartLine     int    `json:"start_line"`
	Edge          string `json:"edge,omitempty"`
	Protocol      string `json:"protocol,omitempty"`
}

// v2DescriptionBlock is the description section data.
type v2DescriptionBlock struct {
	HasDocs     bool   `json:"has_docs"`
	Summary     string `json:"summary"`
	DocsPath    string `json:"docs_path,omitempty"`
	AIGenerated bool   `json:"ai_generated,omitempty"`
}

// v2OutboundQueries groups downstream entities by kind.
type v2OutboundQueries struct {
	DB       []v2PathEntity `json:"db"`
	Event    []v2PathEntity `json:"event"`
	Queue    []v2PathEntity `json:"queue"`
	External []v2PathEntity `json:"external"`
	GRPC     []v2PathEntity `json:"grpc"`
}

// v2PathDetail is the full detail for GET /api/v2/groups/:id/paths/:hash.
type v2PathDetail struct {
	PathHash        string             `json:"path_hash"`
	Path            string             `json:"path"`
	Verbs           []string           `json:"verbs"`
	Repos           []string           `json:"repos"`
	IsWebhook       bool               `json:"is_webhook"`
	WebhookProvider string             `json:"webhook_provider,omitempty"`
	Auth            bool               `json:"auth"`
	AuthScheme      string             `json:"auth_scheme,omitempty"`
	Description     v2DescriptionBlock `json:"description"`
	Parameters      []v2PathParameter  `json:"parameters"`
	ResponseShapes  []v2ResponseShape  `json:"response_shapes"`
	Handlers        []v2HandlerDetail  `json:"handlers"`
	InboundFetches  []v2PathEntity     `json:"inbound_fetches"`
	Outbound        v2OutboundQueries  `json:"outbound"`
	SideEffects     []v2PathEntity     `json:"side_effects"`
	Tests           []v2PathEntity     `json:"tests"`
}

// v2OrphanCaller is one orphan caller row.
type v2OrphanCaller struct {
	ID          string `json:"id"`
	Method      string `json:"method"`
	URLPattern  string `json:"url_pattern"`
	CallerFile  string `json:"caller_file"`
	CallerLine  int    `json:"caller_line"`
	CallerLabel string `json:"caller_label"`
	Repo        string `json:"repo"`
	Reason      string `json:"reason"`
	RepairHint  string `json:"repair_hint,omitempty"`
}

// v2OrphanTotals is the breakdown by severity.
type v2OrphanTotals struct {
	NoHandlerFound  int `json:"no_handler_found"`
	DynamicBaseURL  int `json:"dynamic_baseurl"`
	TemplateLiteral int `json:"template_literal"`
}

// v2OrphansResponse is the payload for GET /api/v2/groups/:id/paths/orphans.
type v2OrphansResponse struct {
	Orphans []v2OrphanCaller `json:"orphans"`
	Totals  v2OrphanTotals   `json:"totals"`
}

// ---------------------------------------------------------------------------
// Handlers
// ---------------------------------------------------------------------------

// handleV2PathsList — GET /api/v2/groups/:id/paths
//
// Returns the full endpoint inventory grouped by owning-backend → controller,
// together with aggregate counts for the sub-stats bar.
//
// Data strategy: reuse the v1 handler_paths.go logic (handlePathsList) for the
// raw endpoint scan, then reshape into the v2 grouped backend structure and
// v2 envelope. The v1 route stays untouched.
func (s *Server) handleV2PathsList(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeV2Err(w, http.StatusBadRequest, "group_required", "group id required")
		return
	}

	grp, err := s.graphs.GetGroup(id)
	if err != nil {
		writeV2Err(w, http.StatusNotFound, "group_not_found", err.Error())
		return
	}

	// ---- Phase 1: collect raw endpoints (ported from handlePathsList) ----

	type rawEP struct {
		ID             string
		Path           string
		Verb           string
		Handler        string
		Framework      string
		IsWebhook      bool
		WebhookProv    string
		Auth           bool
		Repo           string
		SourceFile     string
		StartLine      int
		OwningBackend  string
		ControllerID   string
		ControllerFile string
		Language       string
	}

	var eps []rawEP

	for _, repo := range sortedRepos(grp) {
		// #1646 — per-repo handler-resolution index so each endpoint definition
		// can be grouped by its owning viewset/module rather than the shared
		// route-registration file (routers.py / urls.py).
		idx := buildRepoEntityIndex(repo)
		for i := range repo.Doc.Entities {
			e := &repo.Doc.Entities[i]
			kind := dashStripScopePrefix(e.Kind)
			isHTTP := types.IsHTTPEndpointKind(kind) ||
				strings.EqualFold(kind, httpEndpointKind) ||
				e.Kind == "Endpoint" || e.Kind == "Route"
			if !isHTTP {
				continue
			}
			if e.Kind == "http_endpoint_call" ||
				e.Properties["pattern_type"] == "http_endpoint_client_synthesis" {
				continue
			}
			path := e.Properties["path"]
			if path == "" {
				path = e.Name
			}
			if !isHTTPEndpointPath(path) {
				continue
			}
			verb := strings.ToUpper(e.Properties["verb"])
			if verb == "" {
				verb = "ANY"
			}
			if verb == "ANY" && e.Properties["urlconf_nested_include"] == "true" {
				continue
			}

			// Backend = the owning repo. The raw owning_backend property is
			// almost always the bare entity prefix ("src") which collapses every
			// service into one node, so the repo slug is the only reliable
			// top-level grouping key on a real multi-repo platform (#1551).
			owningBackend := repo.Slug

			// Controller/module — resolve the endpoint definition to its handler
			// (the viewset method that IMPLEMENTS it) and group by the OWNING
			// VIEWSET/CLASS ("RoleViewSet"), which is the unit a developer thinks
			// in. DRF router-expanded definitions all share routers.py:0, so
			// grouping by the definition's own file collapses dozens of viewsets
			// into a single "routers" node — the #1646 bug. The handler's source
			// file is the real controller file. We fall back to the framework
			// controller-file convention (NestJS/GraphQL) and then a name
			// heuristic when no handler resolves. (#1646)
			controllerID := ""
			controllerFile := e.SourceFile
			// Only re-key by the resolved handler when the endpoint entity is a
			// synthetic definition that resolved to a DISTINCT handler (the DRF
			// router-expanded case). Endpoint entities that are themselves the
			// route (NestJS/GraphQL/Go) keep the file-based grouping below.
			if strings.EqualFold(dashStripScopePrefix(e.Kind), httpEndpointDefinitionKind) {
				if handlerEnts := idx.resolveHandlers(e); len(handlerEnts) > 0 {
					h := handlerEnts[0]
					if h.ID != e.ID &&
						!strings.EqualFold(dashStripScopePrefix(h.Kind), httpEndpointDefinitionKind) {
						controllerID = handlerGroupKey(h)
						if h.SourceFile != "" {
							controllerFile = h.SourceFile
						}
					}
				}
			}
			if controllerID == "" {
				// Framework controllers ("orders.controller.ts") / GraphQL
				// resolver files map cleanly to a developer unit by file.
				controllerID = controllerKeyFromFile(e.SourceFile)
			}
			if controllerID == "" {
				controllerID = e.Properties["controller"]
			}
			if controllerID == "" {
				controllerID = inferControllerName(e.Name)
			}

			eps = append(eps, rawEP{
				ID:             dashPrefixedID(repo.Slug, e.ID),
				Path:           path,
				Verb:           verb,
				Handler:        e.Name,
				Framework:      e.Properties["framework"],
				IsWebhook:      e.Properties["is_webhook"] == "true",
				WebhookProv:    e.Properties["webhook_provider"],
				Auth:           e.Properties["auth"] == "true" || e.Properties["auth_scheme"] != "",
				Repo:           repo.Slug,
				SourceFile:     e.SourceFile,
				StartLine:      e.StartLine,
				OwningBackend:  owningBackend,
				ControllerID:   controllerID,
				ControllerFile: controllerFile,
				Language:       e.Language,
			})
		}
	}

	// ---- Phase 2: group by backend → controller → path ----

	type ctrlMeta struct {
		id    string
		file  string
		paths map[string]*v2PathRoute
		order []string // path insertion order
	}
	type beMeta struct {
		id          string
		controllers map[string]*ctrlMeta
		order       []string // controller insertion order
	}

	backends := map[string]*beMeta{}
	backendOrder := []string{}

	for _, ep := range eps {
		bID := ep.OwningBackend
		if _, ok := backends[bID]; !ok {
			backends[bID] = &beMeta{
				id:          bID,
				controllers: map[string]*ctrlMeta{},
			}
			backendOrder = append(backendOrder, bID)
		}
		bm := backends[bID]

		cID := ep.ControllerID
		if _, ok := bm.controllers[cID]; !ok {
			bm.controllers[cID] = &ctrlMeta{
				id:    cID,
				file:  ep.ControllerFile,
				paths: map[string]*v2PathRoute{},
			}
			bm.order = append(bm.order, cID)
		}
		cm := bm.controllers[cID]

		if _, ok := cm.paths[ep.Path]; !ok {
			cm.paths[ep.Path] = &v2PathRoute{
				PathHash:   hashStr(ep.Path),
				Path:       ep.Path,
				Verbs:      []string{},
				Frameworks: []string{},
				Repos:      []string{},
				Controller: cID,
			}
			cm.order = append(cm.order, ep.Path)
		}
		pr := cm.paths[ep.Path]
		pr.Multiplicity++
		pr.HandlersCount++
		if !containsStr(pr.Verbs, ep.Verb) {
			pr.Verbs = append(pr.Verbs, ep.Verb)
		}
		if ep.Framework != "" && !containsStr(pr.Frameworks, ep.Framework) {
			pr.Frameworks = append(pr.Frameworks, ep.Framework)
		}
		if ep.IsWebhook {
			pr.IsWebhook = true
			pr.WebhookProvider = ep.WebhookProv
		}
		if ep.Auth {
			pr.Auth = true
		}
		if !containsStr(pr.Repos, ep.Repo) {
			pr.Repos = append(pr.Repos, ep.Repo)
		}
	}

	// ---- Phase 3: build v2 response shape ----

	result := make([]v2PathBackend, 0, len(backends))

	for _, bID := range backendOrder {
		bm := backends[bID]

		// Collect all repos used by this backend.
		repoSet := map[string]bool{}
		for _, cID := range bm.order {
			for _, pr := range bm.controllers[cID].paths {
				for _, rr := range pr.Repos {
					repoSet[rr] = true
				}
			}
		}
		repos := make([]string, 0, len(repoSet))
		for rr := range repoSet {
			repos = append(repos, rr)
		}
		sort.Strings(repos)

		// Detect cross-backend refs: a route owned here whose repo set spans
		// beyond this backend's own repo (the backend id is the repo slug).
		crossBackendRefs := false
		anyRate := 0
		var language, framework string
		verbSet := map[string]bool{}

		groups := make([]v2ControllerGroup, 0, len(bm.order))
		for _, cID := range bm.order {
			cm := bm.controllers[cID]
			routes := make([]v2PathRoute, 0, len(cm.order))
			for _, path := range cm.order {
				pr := cm.paths[path]
				sort.Strings(pr.Verbs)
				for _, v := range pr.Verbs {
					verbSet[v] = true
					if v == "ANY" {
						anyRate++
					}
				}
				for _, rr := range pr.Repos {
					if rr != bID {
						crossBackendRefs = true
					}
				}
				routes = append(routes, *pr)
				if framework == "" && len(pr.Frameworks) > 0 {
					framework = pr.Frameworks[0]
				}
			}
			isWebhookCtrl := false
			for _, r := range routes {
				if r.IsWebhook {
					isWebhookCtrl = true
					break
				}
			}
			groups = append(groups, v2ControllerGroup{
				ID:        cID,
				Label:     controllerLabel(cID, cm.file),
				File:      cm.file,
				IsWebhook: isWebhookCtrl,
				Routes:    routes,
			})
		}

		// Infer language from first endpoint.
		for _, ep := range eps {
			if ep.OwningBackend == bID && ep.Language != "" {
				language = ep.Language
				break
			}
		}

		serviceType := serviceTypeFromVerbs(verbSet, bID, repos)

		result = append(result, v2PathBackend{
			ID:               bID,
			Label:            backendLabelFromRepo(bID, backendOrder),
			ServiceType:      serviceType,
			Framework:        framework,
			Language:         language,
			CrossBackendRefs: crossBackendRefs,
			AnyRate:          anyRate,
			Groups:           groups,
		})
	}

	// ---- Phase 4: compute totals ----

	var totalRoutes, totalEndpoints, totalControllers int
	for _, b := range result {
		totalControllers += len(b.Groups)
		for _, g := range b.Groups {
			totalRoutes += len(g.Routes)
			for _, r := range g.Routes {
				totalEndpoints += r.HandlersCount
			}
		}
	}

	// Orphan count for the tab badge — reuse the same v1 scan the Orphans tab
	// uses so the number is authoritative, not an estimate (#1551).
	orphanCount := len(collectOrphanCallers(grp))

	writeV2JSON(w, http.StatusOK, v2OK(v2PathsListResponse{
		Backends: result,
		Totals: v2PathTotals{
			Routes:      totalRoutes,
			Endpoints:   totalEndpoints,
			Controllers: totalControllers,
			Backends:    len(result),
			Orphans:     orphanCount,
		},
	}))
}

// handleV2PathDetail — GET /api/v2/groups/:id/paths/:hash
//
// Returns the full Swagger++ detail for a single path identified by its hash.
// The handler enriches the v1 detail response with the v2 envelope and the
// structured outbound/inbound entity shapes the detail pane needs.
func (s *Server) handleV2PathDetail(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	pathHash := r.PathValue("hash")
	if id == "" || pathHash == "" {
		writeV2Err(w, http.StatusBadRequest, "params_required", "group id and path hash required")
		return
	}

	grp, err := s.graphs.GetGroup(id)
	if err != nil {
		writeV2Err(w, http.StatusNotFound, "group_not_found", err.Error())
		return
	}

	type matched struct {
		Verb          string
		Handler       string
		QualifiedName string
		Framework     string
		IsWebhook     bool
		WebhookProv   string
		Auth          bool
		AuthScheme    string
		Repo          string
		SourceFile    string
		StartLine     int
		Language      string
		HasDocs       bool
		DocsSummary   string
		DocsPath      string
		ResponseKeys  []string
		StatusCodes   []int
		// CalledByIDs / OutboundIDs / SideEffectIDs / TestIDs are collected from
		// the RESOLVED handler (the viewset method that IMPLEMENTS this endpoint
		// definition), not the synthetic definition entity itself (#1646).
		CalledByIDs   []string
		OutboundIDs   []string
		SideEffectIDs []string
		TestIDs       []string
	}

	var hits []matched
	var pathStr string
	var isWebhook bool
	var webhookProv string

	// Per-repo entity index + handler-resolution map, built lazily and reused
	// across all matched endpoint definitions in the same repo (#1646).
	repoIdx := map[string]*repoEntityIndex{}

	docgenState, _ := mcp.LoadDocgenState(id)

	for _, repo := range sortedRepos(grp) {
		for i := range repo.Doc.Entities {
			e := &repo.Doc.Entities[i]
			kind := dashStripScopePrefix(e.Kind)
			isHTTP := types.IsHTTPEndpointKind(kind) ||
				strings.EqualFold(kind, httpEndpointKind) ||
				e.Kind == "Endpoint" || e.Kind == "Route"
			if !isHTTP {
				continue
			}
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

			if e.Properties["is_webhook"] == "true" {
				isWebhook = true
				webhookProv = e.Properties["webhook_provider"]
			}

			hasDocs, docsSummary, docsPath, _ := extractEndpointDocsEnriched(id, pathHash, docgenState)

			// Collect response keys / status codes.
			var respKeys []string
			if rk := e.Properties["response_keys"]; rk != "" {
				respKeys = strings.Split(rk, ",")
			}
			var statusCodes []int
			if sc := e.Properties["status_codes"]; sc != "" {
				for _, s := range strings.Split(sc, ",") {
					s = strings.TrimSpace(s)
					var n int
					for _, c := range s {
						if c >= '0' && c <= '9' {
							n = n*10 + int(c-'0')
						}
					}
					if n > 0 {
						statusCodes = append(statusCodes, n)
					}
				}
			}

			// #1646 — resolve this endpoint definition to its real handler(s)
			// (the viewset method that IMPLEMENTS it) and collect the section
			// edges from the HANDLER, not the synthetic definition (which carries
			// no body edges). The definition ID is still passed so retargeted
			// inbound FETCHES (caller → definition) surface as Called-by.
			idx := repoIdx[repo.Slug]
			if idx == nil {
				idx = buildRepoEntityIndex(repo)
				repoIdx[repo.Slug] = idx
			}
			handlerEnts := idx.resolveHandlers(e)
			handlerIDs := make([]string, 0, len(handlerEnts))
			for _, h := range handlerEnts {
				handlerIDs = append(handlerIDs, h.ID)
			}
			classified := classifyHandlerEdges(idx, handlerIDs, []string{e.ID})

			// Prefer the resolved handler's identity for the handler card so the
			// detail shows the real viewset method + its source location, not the
			// routers.py:0 synthetic. Fall back to the definition when unresolved.
			handlerName := e.Name
			handlerQN := e.Properties["qualified_name"]
			handlerFile := e.SourceFile
			handlerLine := e.StartLine
			handlerLang := e.Language
			if len(handlerEnts) > 0 {
				h := handlerEnts[0]
				if !strings.EqualFold(dashStripScopePrefix(h.Kind), httpEndpointDefinitionKind) {
					handlerName = h.Name
					if h.QualifiedName != "" {
						handlerQN = h.QualifiedName
					}
					handlerFile = h.SourceFile
					handlerLine = h.StartLine
					if h.Language != "" {
						handlerLang = h.Language
					}
				}
			}

			hits = append(hits, matched{
				Verb:          verb,
				Handler:       handlerName,
				QualifiedName: handlerQN,
				Framework:     e.Properties["framework"],
				IsWebhook:     e.Properties["is_webhook"] == "true",
				WebhookProv:   e.Properties["webhook_provider"],
				Auth:          e.Properties["auth"] == "true" || e.Properties["auth_scheme"] != "",
				AuthScheme:    e.Properties["auth_scheme"],
				Repo:          repo.Slug,
				SourceFile:    handlerFile,
				StartLine:     handlerLine,
				Language:      handlerLang,
				HasDocs:       hasDocs,
				DocsSummary:   docsSummary,
				DocsPath:      docsPath,
				ResponseKeys:  respKeys,
				StatusCodes:   statusCodes,
				CalledByIDs:   classified.calledBy,
				OutboundIDs:   classified.downstream,
				SideEffectIDs: classified.sideEffects,
				TestIDs:       classified.tests,
			})
		}
	}

	if len(hits) == 0 {
		writeV2Err(w, http.StatusNotFound, "path_not_found", "path not found: "+pathHash)
		return
	}

	// Collect verbs, repos, auth.
	verbSet := map[string]bool{}
	repoSet := map[string]bool{}
	var auth bool
	var authScheme string
	for _, h := range hits {
		verbSet[h.Verb] = true
		repoSet[h.Repo] = true
		if h.Auth {
			auth = true
			if authScheme == "" {
				authScheme = h.AuthScheme
			}
		}
	}
	verbs := make([]string, 0, len(verbSet))
	for v := range verbSet {
		verbs = append(verbs, v)
	}
	sort.Strings(verbs)
	repos := make([]string, 0, len(repoSet))
	for rr := range repoSet {
		repos = append(repos, rr)
	}
	sort.Strings(repos)

	// Build handlers list.
	handlers := make([]v2HandlerDetail, 0, len(hits))
	for _, h := range hits {
		qn := h.QualifiedName
		if qn == "" {
			qn = h.Handler
		}
		hAuth := ""
		if h.Auth {
			hAuth = h.AuthScheme
			if hAuth == "" {
				hAuth = "Bearer"
			}
		}
		handlers = append(handlers, v2HandlerDetail{
			Verb:          h.Verb,
			QualifiedName: qn,
			Framework:     h.Framework,
			Repo:          h.Repo,
			SourceFile:    h.SourceFile,
			StartLine:     h.StartLine,
			Language:      h.Language,
			HasDocs:       h.HasDocs,
			DocsSummary:   h.DocsSummary,
			DocsPath:      h.DocsPath,
			Auth:          hAuth,
		})
	}

	// Build response shapes grouped by verb.
	shapesByVerb := map[string]*v2ResponseShape{}
	for _, h := range hits {
		if _, ok := shapesByVerb[h.Verb]; !ok {
			shapesByVerb[h.Verb] = &v2ResponseShape{
				Verb:        h.Verb,
				Keys:        []string{},
				StatusCodes: []int{},
			}
		}
		shape := shapesByVerb[h.Verb]
		for _, k := range h.ResponseKeys {
			if !containsStr(shape.Keys, k) {
				shape.Keys = append(shape.Keys, k)
			}
		}
		for _, sc := range h.StatusCodes {
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
	}
	responseShapes := make([]v2ResponseShape, 0, len(shapesByVerb))
	for _, s := range shapesByVerb {
		sort.Ints(s.StatusCodes)
		responseShapes = append(responseShapes, *s)
	}
	sort.Slice(responseShapes, func(i, j int) bool {
		return responseShapes[i].Verb < responseShapes[j].Verb
	})

	// Extract parameters from path segments (dynamic path params).
	params := extractPathParameters(pathStr, verbs)

	// Resolve entity IDs (#1646: all collected from the resolved handler).
	calledByIDs := collectUniqueIDs(hits, func(h matched) []string { return h.CalledByIDs })
	outboundIDs := collectUniqueIDs(hits, func(h matched) []string { return h.OutboundIDs })
	sideEffectIDs := collectUniqueIDs(hits, func(h matched) []string { return h.SideEffectIDs })
	testIDs := collectUniqueIDs(hits, func(h matched) []string { return h.TestIDs })

	// Called-by: inbound callers resolved to this endpoint (frontend FETCHES
	// retargeted to the definition + intra-repo CALLS into the handler).
	inboundFetches := resolveEntitySlice(grp, calledByIDs, "CALLED_BY")
	// Downstream: the handler's outbound CALLS (services, helpers, DB-access fns).
	outboundAll := resolveEntitySlice(grp, outboundIDs, "CALLS")
	// Side effects: DB writes / model mutation / pub-sub the handler performs.
	sideEffectEntities := resolveEntitySlice(grp, sideEffectIDs, "SIDE_EFFECT")
	testEntities := resolveEntitySlice(grp, testIDs, "TESTS")

	// Split downstream callees by kind for the structured Outbound section.
	outbound := v2OutboundQueries{
		DB:       []v2PathEntity{},
		Event:    []v2PathEntity{},
		Queue:    []v2PathEntity{},
		External: []v2PathEntity{},
		GRPC:     []v2PathEntity{},
	}
	for _, e := range outboundAll {
		switch strings.ToLower(e.Kind) {
		case "datastore", "table", "db", "database", "model", "dataaccess", "collection":
			outbound.DB = append(outbound.DB, e)
		case "event", "topic":
			outbound.Event = append(outbound.Event, e)
		case "queue":
			outbound.Queue = append(outbound.Queue, e)
		case "externalapi", "external":
			outbound.External = append(outbound.External, e)
		case "service":
			if e.Protocol == "grpc" {
				outbound.GRPC = append(outbound.GRPC, e)
			} else {
				outbound.External = append(outbound.External, e)
			}
		default:
			outbound.External = append(outbound.External, e)
		}
	}

	// Description block from docgen.
	var description v2DescriptionBlock
	if len(hits) > 0 && hits[0].HasDocs {
		description = v2DescriptionBlock{
			HasDocs:     true,
			Summary:     hits[0].DocsSummary,
			DocsPath:    hits[0].DocsPath,
			AIGenerated: hits[0].DocsPath != "",
		}
	}

	writeV2JSON(w, http.StatusOK, v2OK(v2PathDetail{
		PathHash:        pathHash,
		Path:            pathStr,
		Verbs:           verbs,
		Repos:           repos,
		IsWebhook:       isWebhook,
		WebhookProvider: webhookProv,
		Auth:            auth,
		AuthScheme:      authScheme,
		Description:     description,
		Parameters:      params,
		ResponseShapes:  responseShapes,
		Handlers:        handlers,
		InboundFetches:  inboundFetches,
		Outbound:        outbound,
		SideEffects:     sideEffectEntities,
		Tests:           testEntities,
	}))
}

// handleV2PathsOrphans — GET /api/v2/groups/:id/paths/orphans
//
// Returns frontend FETCH call sites that resolve to no backend handler,
// severity-sorted and grouped. Reuses collectOrphanCallers (v1 logic)
// and reshapes the result into the v2 envelope.
func (s *Server) handleV2PathsOrphans(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeV2Err(w, http.StatusBadRequest, "group_required", "group id required")
		return
	}

	grp, err := s.graphs.GetGroup(id)
	if err != nil {
		writeV2Err(w, http.StatusNotFound, "group_not_found", err.Error())
		return
	}

	v1Rows := collectOrphanCallers(grp)

	orphans := make([]v2OrphanCaller, 0, len(v1Rows))
	for _, row := range v1Rows {
		callerLabel := row.CallerFile
		if i := strings.LastIndex(callerLabel, "/"); i >= 0 {
			callerLabel = callerLabel[i+1:]
		}
		orphans = append(orphans, v2OrphanCaller{
			ID:          row.ID,
			Method:      row.Method,
			URLPattern:  row.URLPattern,
			CallerFile:  row.CallerFile,
			CallerLine:  row.CallerLine,
			CallerLabel: callerLabel,
			Repo:        row.Repo,
			Reason:      row.Reason,
		})
	}

	// Sort by severity: no_handler_found first.
	severityOrder := map[string]int{
		string(reasonNoHandlerFound):  0,
		string(reasonDynamicBaseURL):  1,
		string(reasonTemplateLiteral): 2,
	}
	sort.Slice(orphans, func(i, j int) bool {
		oi := severityOrder[orphans[i].Reason]
		oj := severityOrder[orphans[j].Reason]
		if oi != oj {
			return oi < oj
		}
		return orphans[i].URLPattern < orphans[j].URLPattern
	})

	totals := v2OrphanTotals{}
	for _, o := range orphans {
		switch o.Reason {
		case string(reasonNoHandlerFound):
			totals.NoHandlerFound++
		case string(reasonDynamicBaseURL):
			totals.DynamicBaseURL++
		case string(reasonTemplateLiteral):
			totals.TemplateLiteral++
		}
	}

	writeV2JSON(w, http.StatusOK, v2OK(v2OrphansResponse{
		Orphans: orphans,
		Totals:  totals,
	}))
}

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

// inferControllerName derives a controller/group name from an entity name.
// Falls back to the raw name if no recognisable suffix is found.
func inferControllerName(handlerName string) string {
	// Strip method suffix: "OrderViewSet.retrieve" → "OrderViewSet"
	if dot := strings.LastIndex(handlerName, "."); dot > 0 {
		return handlerName[:dot]
	}
	// Strip :: scope separator: "pkg::OrderViewSet"
	if sc := strings.LastIndex(handlerName, "::"); sc > 0 {
		return handlerName[sc+2:]
	}
	return handlerName
}

// controllerLabel produces the display label for a controller group. When the
// group id is a resolved viewset/class name (the #1646 path — it is not a file
// path), it is used verbatim ("RoleViewSet"). Otherwise the group was keyed by
// source file (NestJS/GraphQL/fallback) and we derive a friendly label from the
// file convention.
func controllerLabel(controllerID, file string) string {
	if controllerID != "" && !strings.ContainsAny(controllerID, "/.") {
		return controllerID
	}
	return controllerLabelFromFile(file)
}

// controllerKeyFromFile derives a stable controller/module grouping key from a
// handler's source file. The full repo-relative path is used so two files with
// the same basename in different directories never collide.
func controllerKeyFromFile(sourceFile string) string {
	return strings.TrimSpace(sourceFile)
}

// controllerLabelFromFile produces a human-friendly controller/module label
// from a source file path. "src/orders.controller.ts" → "OrdersController";
// "src/resolvers.ts" → "resolvers"; "src/index.ts" → "index". Falls back to the
// basename when no recognisable convention applies.
func controllerLabelFromFile(sourceFile string) string {
	base := sourceFile
	if i := strings.LastIndex(base, "/"); i >= 0 {
		base = base[i+1:]
	}
	// Strip a single trailing extension.
	if dot := strings.LastIndex(base, "."); dot > 0 {
		base = base[:dot]
	}
	// "orders.controller" / "saga.controller" → "OrdersController".
	for _, suf := range []string{".controller", ".resolver", ".router", ".service", ".handler"} {
		if strings.HasSuffix(base, suf) {
			stem := strings.TrimSuffix(base, suf)
			kind := strings.TrimPrefix(suf, ".")
			return titleFirst(stem) + titleFirst(kind)
		}
	}
	return base
}

func titleFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// backendLabelFromRepo trims the longest common slash/dash-delimited prefix
// shared by all repo backends so labels read as "gateway" / "payments" rather
// than "polyglot-platform-services-gateway". Falls back to the raw slug.
func backendLabelFromRepo(repo string, allRepos []string) string {
	if len(allRepos) < 2 {
		return repo
	}
	prefix := commonDashPrefix(allRepos)
	if prefix != "" && strings.HasPrefix(repo, prefix) {
		trimmed := strings.TrimPrefix(repo, prefix)
		if trimmed != "" {
			return trimmed
		}
	}
	return repo
}

// commonDashPrefix returns the longest shared prefix of dash-delimited segments,
// including the trailing dash (e.g. "polyglot-platform-services-").
func commonDashPrefix(items []string) string {
	if len(items) == 0 {
		return ""
	}
	segs := strings.Split(items[0], "-")
	commonN := len(segs)
	for _, it := range items[1:] {
		other := strings.Split(it, "-")
		n := 0
		for n < commonN && n < len(other) && segs[n] == other[n] {
			n++
		}
		commonN = n
		if commonN == 0 {
			return ""
		}
	}
	// Never consume every segment (would leave an empty label).
	if commonN >= len(segs) {
		commonN = len(segs) - 1
	}
	if commonN <= 0 {
		return ""
	}
	return strings.Join(segs[:commonN], "-") + "-"
}

// serviceTypeFromVerbs derives the display service_type from the set of verbs a
// backend actually serves, falling back to a name/repo heuristic for ambiguous
// REST cases. GRAPHQL → "GraphQL", GRPC → "gRPC", otherwise "REST".
func serviceTypeFromVerbs(verbSet map[string]bool, backendName string, repos []string) string {
	graphql, grpc, rest := false, false, false
	for v := range verbSet {
		switch strings.ToUpper(v) {
		case "GRAPHQL":
			graphql = true
		case "GRPC":
			grpc = true
		default:
			rest = true
		}
	}
	// Mixed backends are labelled by their dominant non-REST protocol so the
	// section colour is meaningful; pure-REST stays REST.
	switch {
	case grpc && !rest && !graphql:
		return "gRPC"
	case graphql && !rest:
		return "GraphQL"
	case graphql:
		return "GraphQL"
	case grpc:
		return "gRPC"
	default:
		return inferServiceTypeV2(backendName, repos)
	}
}

// inferServiceTypeV2 maps a backend name + repo list to one of the display
// service_type values used by the UI: "REST" | "gRPC" | "GraphQL".
func inferServiceTypeV2(backendName string, repos []string) string {
	combined := strings.ToLower(backendName)
	for _, r := range repos {
		combined += " " + strings.ToLower(r)
	}
	if strings.Contains(combined, "grpc") || strings.Contains(combined, "gateway-grpc") {
		return "gRPC"
	}
	if strings.Contains(combined, "graphql") || strings.Contains(combined, "gql") {
		return "GraphQL"
	}
	return "REST"
}

// extractPathParameters builds a minimal parameter list from the path's
// dynamic segments. Real parameter metadata would come from annotations /
// OpenAPI schema; this synthesises a fallback for paths without it.
func extractPathParameters(path string, verbs []string) []v2PathParameter {
	params := []v2PathParameter{}
	re := strings.Split(strings.TrimPrefix(path, "/"), "/")
	for _, seg := range re {
		if strings.HasPrefix(seg, "{") && strings.HasSuffix(seg, "}") {
			name := seg[1 : len(seg)-1]
			params = append(params, v2PathParameter{
				Name:     name,
				In:       "path",
				Type:     "string",
				Required: true,
				Desc:     "Path segment.",
				Verbs:    verbs,
			})
		}
	}
	return params
}

// collectUniqueIDs collects deduplicated ID sets from each matched endpoint.
func collectUniqueIDs[T any](hits []T, fn func(T) []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, h := range hits {
		for _, id := range fn(h) {
			if !seen[id] {
				seen[id] = true
				out = append(out, id)
			}
		}
	}
	return out
}

// resolveEntitySlice resolves a list of prefixed entity IDs to v2PathEntity values.
func resolveEntitySlice(grp *DashGroup, ids []string, edge string) []v2PathEntity {
	out := make([]v2PathEntity, 0, len(ids))
	for _, id := range ids {
		repo, entity := findEntity(grp, id)
		if entity == nil {
			continue
		}
		label := entity.Name
		qn := entity.QualifiedName
		if qn == "" {
			qn = label
		}
		repoSlug := ""
		if repo != nil {
			repoSlug = repo.Slug
		}
		out = append(out, v2PathEntity{
			Label:         label,
			QualifiedName: qn,
			Kind:          dashStripScopePrefix(entity.Kind),
			Repo:          repoSlug,
			SourceFile:    entity.SourceFile,
			StartLine:     entity.StartLine,
			Edge:          edge,
			Protocol:      entity.Properties["protocol"],
		})
	}
	return out
}
