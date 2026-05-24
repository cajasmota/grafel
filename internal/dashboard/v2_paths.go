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

	"github.com/cajasmota/archigraph/internal/engine"
	"github.com/cajasmota/archigraph/internal/graph"
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
	// AuthChip is the rendered chip label for the route in the left-rail
	// list (e.g. `[Public]`, `[Auth required]`, `[Roles: ADMIN]`,
	// `[Auth: default]`, `[Auth: probable]`, `[Auth: unknown]`). Computed
	// from the resolved auth_policy emitted by the indexer (#1942 Phase 1).
	AuthChip string `json:"auth_chip,omitempty"`
	// AuthChipTone is the visual tone hint for the chip:
	// "accent" | "muted" | "warning". Lets the frontend keep the chip
	// taxonomy in sync without hard-coding label parsing.
	AuthChipTone string   `json:"auth_chip_tone,omitempty"`
	Repos        []string `json:"repos"`
	Controller   string   `json:"controller"`
	// Confidence (#1129) is the 0..1 candidate-quality score computed at
	// list-build time. Always populated when the confidence filter ran.
	Confidence float64 `json:"confidence,omitempty"`
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

	// Candidate-quality bar (#1129). LowConfidenceRoutes is a flat list of
	// routes that were filtered out of the per-backend tree because their
	// confidence score sat below the paths-surface floor (default 0.30). The
	// UI hides these by default but exposes them via an
	// "Include low-signal routes" toggle. NoiseRejectedCount mirrors
	// len(LowConfidenceRoutes) for badge rendering. ConfidenceFloor is the
	// effective floor (after env override).
	LowConfidenceRoutes []v2LowConfidenceRoute `json:"low_confidence_routes"`
	NoiseRejectedCount  int                    `json:"noise_rejected_count"`
	ConfidenceFloor     float64                `json:"confidence_floor"`
}

// v2LowConfidenceRoute is the flattened shape used in the low-confidence
// bucket. It carries enough context (backend, controller) for the UI to show
// the route in its natural parent group when the toggle is on.
type v2LowConfidenceRoute struct {
	BackendID    string      `json:"backend_id"`
	ControllerID string      `json:"controller_id"`
	Route        v2PathRoute `json:"route"`
	Confidence   float64     `json:"confidence"`
	Signals      []string    `json:"signals"`
}

// v2PathParameter is one parameter in the detail pane.
type v2PathParameter struct {
	Name     string   `json:"name"`
	In       string   `json:"in"`
	Type     string   `json:"type"`
	Required bool     `json:"required"`
	Desc     string   `json:"desc"`
	Verbs    []string `json:"verbs,omitempty"`
	// Refs #1935 Phase 1 — when the parameter type resolves to a
	// user-defined class entity in the group, TypeEntityID carries the
	// prefixed entity id ("<slug>:<id>") and HasChildren is true. The
	// frontend uses HasChildren to render the expand glyph and
	// TypeEntityID as the input to GET /api/v2/groups/:id/shape.
	TypeEntityID string `json:"type_entity_id,omitempty"`
	HasChildren  bool   `json:"has_children,omitempty"`
}

// v2ResponseShape is one verb's response metadata.
type v2ResponseShape struct {
	Verb        string   `json:"verb"`
	StatusCodes []int    `json:"status_codes"`
	Keys        []string `json:"keys"`
	Dynamic     bool     `json:"dynamic,omitempty"`
	// Refs #1935 Phase 1 — when the handler's return type resolves to a
	// known class entity, the response row can be expanded into a
	// ShapeTree subtree. TypeName is the simple-name token surfaced
	// from the Java extractor (e.g. "LoginResponse"); TypeEntityID is
	// the prefixed entity id; HasChildren mirrors the frontend's
	// expand-glyph predicate. Empty when the return type is void,
	// primitive, or unresolved.
	TypeName     string `json:"type_name,omitempty"`
	TypeEntityID string `json:"type_entity_id,omitempty"`
	HasChildren  bool   `json:"has_children,omitempty"`
}

// v2PerStatusResponse is one entry in the per-status-code tab strip
// for the Response section (#1938 Phase 1). Each entry describes the
// response shape for a specific HTTP status code extracted from
// @APIResponse / @ApiResponse annotations.
type v2PerStatusResponse struct {
	StatusCode   int    `json:"status_code"`
	TypeName     string `json:"type_name,omitempty"`
	TypeEntityID string `json:"type_entity_id,omitempty"`
	HasChildren  bool   `json:"has_children,omitempty"`
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
	// AuthPolicy is the structured posture resolved by the indexer
	// (#1942 Phase 1). Frontend renders the header chip from AuthChip /
	// AuthChipTone and the expandable evidence list from SourceChain.
	AuthPolicy   *v2AuthPolicy      `json:"auth_policy,omitempty"`
	AuthChip     string             `json:"auth_chip,omitempty"`
	AuthChipTone string             `json:"auth_chip_tone,omitempty"`
	Description     v2DescriptionBlock    `json:"description"`
	Parameters      []v2PathParameter     `json:"parameters"`
	ResponseShapes  []v2ResponseShape     `json:"response_shapes"`
	// PerStatusResponses carries per-status-code response metadata extracted
	// from @APIResponse / @ApiResponse annotations (#1938 Phase 1). Non-empty
	// only for Java endpoints using MicroProfile OpenAPI or JAX-RS 2.x.
	// The frontend renders a tab strip above the ShapeTree when this is non-empty.
	PerStatusResponses []v2PerStatusResponse `json:"per_status_responses,omitempty"`
	Handlers        []v2HandlerDetail     `json:"handlers"`
	InboundFetches  []v2PathEntity     `json:"inbound_fetches"`
	Outbound        v2OutboundQueries  `json:"outbound"`
	SideEffects     []v2PathEntity     `json:"side_effects"`
	Tests           []v2PathEntity     `json:"tests"`
}

// v2AuthSignal is one evidence row in the resolved auth_policy source chain.
// Mirrors engine.AuthSignal but uses snake_case JSON for the wire format the
// dashboard already consumes elsewhere.
type v2AuthSignal struct {
	Kind     string `json:"kind"`
	EntityID string `json:"entity_id,omitempty"`
	Text     string `json:"text"`
	File     string `json:"file"`
	Line     int    `json:"line"`
}

// v2AuthPolicy is the wire shape of the resolved auth posture surfaced on
// the endpoint detail response (#1942 Phase 1). The frontend renders the
// header chip from Chip/ChipTone and the expandable evidence list from
// SourceChain.
type v2AuthPolicy struct {
	Required    bool           `json:"required"`
	Method      string         `json:"method"`
	Roles       []string       `json:"roles,omitempty"`
	Scopes      []string       `json:"scopes,omitempty"`
	Confidence  string         `json:"confidence"`
	SourceChain []v2AuthSignal `json:"source_chain,omitempty"`
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
		// #1942 Phase 1 — resolved auth_policy for this endpoint.
		AuthPolicy engine.AuthPolicy
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

			authPolicy := readAuthPolicyFromEntity(e.Properties)
			eps = append(eps, rawEP{
				ID:             dashPrefixedID(repo.Slug, e.ID),
				Path:           path,
				Verb:           verb,
				Handler:        e.Name,
				Framework:      e.Properties["framework"],
				IsWebhook:      e.Properties["is_webhook"] == "true",
				WebhookProv:    e.Properties["webhook_provider"],
				Auth:           e.Properties["auth"] == "true" || e.Properties["auth_scheme"] != "" || authPolicy.Required,
				Repo:           repo.Slug,
				SourceFile:     e.SourceFile,
				StartLine:      e.StartLine,
				OwningBackend:  owningBackend,
				ControllerID:   controllerID,
				ControllerFile: controllerFile,
				Language:       e.Language,
				AuthPolicy:     authPolicy,
			})
		}
	}

	// ---- Phase 1b: #1940 — suppress ANY-verb catch-all endpoints when
	//      per-verb counterparts exist for the same (backend, path).
	//
	// Django URL-conf entries can emit method=ANY when the URL entry itself
	// carries no verb restriction. When a per-verb route (GET, POST, etc.)
	// exists for the same path (typically from DRF router expansion or a
	// class-based view), the ANY entry is redundant and clutters the list.
	// We mark it synthetic so the Endpoints tab hides it; it will instead
	// appear under the "Catch-all" filter chip (frontend TODO: #1940).
	{
		type backendPathKey struct{ backend, path string }
		nonAnyVerbs := map[backendPathKey]bool{}
		for _, ep := range eps {
			if ep.Verb != "ANY" {
				nonAnyVerbs[backendPathKey{ep.OwningBackend, ep.Path}] = true
			}
		}
		filtered := eps[:0:0]
		for _, ep := range eps {
			if ep.Verb == "ANY" &&
				ep.Repo != "" &&
				nonAnyVerbs[backendPathKey{ep.OwningBackend, ep.Path}] {
				// A per-verb route exists — suppress this synthetic catch-all.
				continue
			}
			filtered = append(filtered, ep)
		}
		eps = filtered
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
		// #1942 Phase 1 — accumulate the strongest auth_policy across all
		// handlers that share this path. Precedence: high > medium > low.
		// We attach the rendered chip directly on the route so the left-rail
		// renders without re-resolving on the client.
		if pr.AuthChip == "" || authPolicyStronger(ep.AuthPolicy, pr.AuthChipTone) {
			label, tone := resolveAuthChip(ep.AuthPolicy)
			pr.AuthChip = label
			pr.AuthChipTone = tone
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

	// ---- Phase 5: apply per-surface confidence floor (#1129) ----
	// Routes below the paths floor (default 0.30) are pulled OUT of the
	// per-backend tree and collected into a flat low_confidence_routes list.
	// The default UI tree only renders high-confidence routes; the toggle
	// surfaces the flat list when the user opts in.
	pathsFloor := FloorFor(SurfacePaths)
	var lowConfRoutes []v2LowConfidenceRoute

	for bi := range result {
		b := &result[bi]
		for gi := range b.Groups {
			g := &b.Groups[gi]
			keptRoutes := g.Routes[:0]
			for _, route := range g.Routes {
				entry := pathRouteToEntry(route, b.ID, b.Framework, b.Language)
				score, signals := ComputeCandidateConfidence(SurfacePaths, entry, nil)
				route.Confidence = roundConfidence(score)
				if pathsFloor > 0 && score < pathsFloor {
					lowConfRoutes = append(lowConfRoutes, v2LowConfidenceRoute{
						BackendID:    b.ID,
						ControllerID: g.ID,
						Route:        route,
						Confidence:   route.Confidence,
						Signals:      signals,
					})
					continue
				}
				keptRoutes = append(keptRoutes, route)
			}
			g.Routes = keptRoutes
		}
	}

	if lowConfRoutes == nil {
		lowConfRoutes = []v2LowConfidenceRoute{}
	}

	// Recompute totals AFTER filtering so the sub-stats bar reflects what the
	// user actually sees in the tree.
	totalRoutes, totalEndpoints, totalControllers = 0, 0, 0
	for _, b := range result {
		totalControllers += len(b.Groups)
		for _, g := range b.Groups {
			totalRoutes += len(g.Routes)
			for _, r := range g.Routes {
				totalEndpoints += r.HandlersCount
			}
		}
	}

	writeV2JSON(w, http.StatusOK, v2OK(v2PathsListResponse{
		Backends: result,
		Totals: v2PathTotals{
			Routes:      totalRoutes,
			Endpoints:   totalEndpoints,
			Controllers: totalControllers,
			Backends:    len(result),
			Orphans:     orphanCount,
		},
		LowConfidenceRoutes: lowConfRoutes,
		NoiseRejectedCount:  len(lowConfRoutes),
		ConfidenceFloor:     pathsFloor,
	}))
}

// pathRouteToEntry projects a v2PathRoute into the wire-format map shape that
// ComputeCandidateConfidence understands. Only fields the score function
// inspects are included; everything else is a no-op for scoring.
func pathRouteToEntry(r v2PathRoute, backendID, framework, language string) map[string]any {
	frameworkValue := framework
	if frameworkValue == "" && len(r.Frameworks) > 0 {
		frameworkValue = r.Frameworks[0]
	}
	return map[string]any{
		"path":           r.Path,
		"handler":        r.Controller,
		"controller":     r.Controller,
		"handlers_count": r.HandlersCount,
		"frameworks":     r.Frameworks,
		"framework":      frameworkValue,
		"repo":           backendID,
		"is_webhook":     r.IsWebhook,
		"auth":           r.Auth,
		"language":       language,
	}
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
		// HandlerEntityIDs are the prefixed entity IDs for all resolved handlers
		// (slug:entityID). DefEntityID is the prefixed definition entity ID.
		// Both are used to match inbound cross-repo links (#1891).
		HandlerEntityIDs []string
		DefEntityID      string
		// Issue #1909 — JAX-RS / Spring request body type inferred from method
		// parameter annotations (populated for POST/PUT/PATCH endpoints).
		RequestBodyType      string
		RequestBodyParamName string
		// Issue #1936 Phase 1 — full per-parameter JSON list emitted by the
		// Java annotation extractor. Decoded into v2PathParameter rows below.
		ParametersJSON string
		// Refs #1935 Phase 1 — handler return type extracted by
		// engine.extractJavaReturnType (e.g. "LoginResponse"). Used to
		// surface a ShapeTree-expandable response row.
		ResponseType string
		// Issue #1938 Phase 1 — JSON-encoded []engine.APIResponseEntry extracted
		// from @APIResponse / @ApiResponse annotations. Decoded below to build
		// the PerStatusResponses tab strip.
		APIResponsesJSON string
		// #1942 Phase 1 — resolved auth_policy decoded from the endpoint's
		// `auth_policy` property. Multiple `matched` entries for the same path
		// are reduced to the strongest policy when building the response.
		AuthPolicy engine.AuthPolicy
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

			// Build prefixed handler entity IDs for cross-repo link lookup (#1891).
			prefixedHandlerIDs := make([]string, 0, len(handlerIDs))
			for _, hid := range handlerIDs {
				prefixedHandlerIDs = append(prefixedHandlerIDs, dashPrefixedID(repo.Slug, hid))
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
				HandlerEntityIDs:     prefixedHandlerIDs,
				DefEntityID:          dashPrefixedID(repo.Slug, e.ID),
				// Issue #1909 — request body type from entity properties.
				RequestBodyType:      e.Properties["request_body_type"],
				RequestBodyParamName: e.Properties["request_body_param_name"],
				// Issue #1936 Phase 1 — full parameter list (Java extractor).
				ParametersJSON: e.Properties["parameters"],
				// Refs #1935 Phase 1 — handler return type for the
				// Response ShapeTree subtree.
				ResponseType: e.Properties["response_type"],
				// Issue #1938 Phase 1 — per-status @APIResponse annotations.
				APIResponsesJSON: e.Properties["api_responses"],
				// #1942 Phase 1 — auth_policy decoded from the endpoint entity.
				AuthPolicy: readAuthPolicyFromEntity(e.Properties),
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
	//
	// Refs #1935 Phase 1 — when the handler's return type resolves to
	// an in-group class entity, also stamp TypeName / TypeEntityID /
	// HasChildren so the frontend can render an expandable response
	// row in the unified ShapeTree.
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
		if h.ResponseType != "" && shape.TypeName == "" {
			shape.TypeName = h.ResponseType
			resolveType := unwrapElementType(h.ResponseType)
			if target := findClassEntityByName(grp, resolveType); target != nil {
				if slug, _ := findRepoForEntity(grp, target.ID); slug != "" {
					shape.TypeEntityID = dashPrefixedID(slug, target.ID)
					shape.HasChildren = classHasFieldChildren(grp, target)
				}
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

	// Issue #1936 Phase 1 — when the Java extractor produced a full per-param
	// list (parameters property), surface every row with its `in` chip
	// populated. We merge across verbs: identical (name, in) rows accumulate
	// their verb set so a parameter shared by multiple methods of the same
	// path collapses to one row. Path rows from the URL template are kept as
	// a fallback when the extractor did not emit them.
	type paramKey struct {
		Name string
		In   string
	}
	emitted := map[paramKey]int{} // index into params
	// Seed with path rows from URL template so paths without an annotation
	// extractor still get a baseline.
	for i, p := range params {
		emitted[paramKey{p.Name, p.In}] = i
	}
	for _, h := range hits {
		if h.ParametersJSON == "" {
			continue
		}
		decoded := engine.DecodeJavaParameters(h.ParametersJSON)
		for _, jp := range decoded {
			key := paramKey{jp.Name, jp.In}
			if idx, ok := emitted[key]; ok {
				// Same (name, in) — extend verb set; never demote required.
				if !containsStr(params[idx].Verbs, h.Verb) {
					params[idx].Verbs = append(params[idx].Verbs, h.Verb)
				}
				if jp.Required {
					params[idx].Required = true
				}
				continue
			}
			row := v2PathParameter{
				Name:     jp.Name,
				In:       jp.In,
				Type:     jp.Type,
				Required: jp.Required,
				Desc:     describeJavaParam(jp),
				Verbs:    []string{h.Verb},
			}
			params = append(params, row)
			emitted[key] = len(params) - 1
		}
	}

	// Issue #1909 — append request body parameter when the Java extractor
	// captured a JAX-RS / Spring request body type AND the richer Phase 1
	// parameter list did not already surface a body row for that verb. This
	// keeps backwards compatibility with older indexer outputs that still
	// only emit request_body_type / request_body_param_name.
	//
	// Refs #1935 Phase 1 — resolve the request body type to an in-group
	// class entity so the ShapeTree component can request its field
	// subtree. TypeEntityID is the prefixed entity id; HasChildren is
	// true when at least one CONTAINS field child is present.
	{
		seenBodyVerbs := map[string]bool{}
		for _, p := range params {
			if p.In == "body" {
				for _, v := range p.Verbs {
					seenBodyVerbs[v] = true
				}
			}
		}
		for _, h := range hits {
			if h.RequestBodyType == "" || seenBodyVerbs[h.Verb] {
				continue
			}
			seenBodyVerbs[h.Verb] = true
			paramName := h.RequestBodyParamName
			if paramName == "" {
				paramName = "body"
			}
			param := v2PathParameter{
				Name:     paramName,
				In:       "body",
				Type:     h.RequestBodyType,
				Required: true,
				Desc:     "Request body — inferred from method parameter annotation.",
				Verbs:    []string{h.Verb},
			}
			resolveType := unwrapElementType(h.RequestBodyType)
			if target := findClassEntityByName(grp, resolveType); target != nil {
				if slug, _ := findRepoForEntity(grp, target.ID); slug != "" {
					param.TypeEntityID = dashPrefixedID(slug, target.ID)
					param.HasChildren = classHasFieldChildren(grp, target)
				}
			}
			params = append(params, param)
		}
	}

	// Resolve entity IDs (#1646: all collected from the resolved handler).
	calledByIDs := collectUniqueIDs(hits, func(h matched) []string { return h.CalledByIDs })
	outboundIDs := collectUniqueIDs(hits, func(h matched) []string { return h.OutboundIDs })
	sideEffectIDs := collectUniqueIDs(hits, func(h matched) []string { return h.SideEffectIDs })
	testIDs := collectUniqueIDs(hits, func(h matched) []string { return h.TestIDs })

	// #1891 — augment calledByIDs with cross-repo callers from grp.Links.
	// The HTTP link pass emits a cross-repo link per (caller, definition/handler)
	// pair with Relation="calls" and Method="http". After normalizeLinkEndpoints
	// both Source and Target are in dashPrefixedID format ("<slug>:<entityID>").
	// Collect all handler + definition IDs this endpoint exposes, then scan links
	// whose Target matches any of them and add the Source as an inbound caller.
	endpointTargetSet := make(map[string]bool)
	for _, h := range hits {
		for _, id := range h.HandlerEntityIDs {
			if id != "" {
				endpointTargetSet[id] = true
			}
		}
		if h.DefEntityID != "" {
			endpointTargetSet[h.DefEntityID] = true
		}
	}
	// Also include entities that IMPLEMENT any matched endpoint entity.
	// The http_pass cross-repo link often targets the concrete handler method
	// (e.g. AuthController.login which IMPLEMENTS the http_endpoint entity),
	// NOT the synthetic endpoint entity itself. We resolve these by scanning
	// intra-repo IMPLEMENTS edges whose target is any of our endpoint IDs.
	for _, repo := range sortedRepos(grp) {
		for _, rel := range repo.Doc.Relationships {
			if rel.Kind != "IMPLEMENTS" {
				continue
			}
			toID := dashPrefixedID(repo.Slug, rel.ToID)
			if endpointTargetSet[toID] {
				// The handler that implements this endpoint is also a valid link Target.
				endpointTargetSet[dashPrefixedID(repo.Slug, rel.FromID)] = true
			}
		}
	}
	crossRepoCalledByIDs := collectCrossRepoCallers(grp, endpointTargetSet)
	calledByIDs = mergeUniqueStrings(calledByIDs, crossRepoCalledByIDs)

	// Called-by: inbound callers resolved to this endpoint (frontend FETCHES
	// retargeted to the definition + intra-repo CALLS into the handler, plus
	// cross-repo http_endpoint_call sources from the links file).
	// Issue #1908 — use the specialized inbound resolver that unwraps
	// http_endpoint_call entities to their actual calling code entity.
	inboundFetches := resolveInboundFetches(grp, calledByIDs)
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

	// Issue #1938 Phase 1 — build per-status-code response tab data.
	// Merge @APIResponse entries across all matched handlers; deduplicate by
	// status code (first-wins). Resolve type names to in-group class entities
	// so the frontend can request ShapeTree expansion.
	var perStatusResponses []v2PerStatusResponse
	{
		seenStatus := map[int]bool{}
		for _, h := range hits {
			if h.APIResponsesJSON == "" {
				continue
			}
			entries := engine.DecodeAPIResponses(h.APIResponsesJSON)
			for _, entry := range entries {
				if seenStatus[entry.StatusCode] {
					continue
				}
				seenStatus[entry.StatusCode] = true
				psr := v2PerStatusResponse{StatusCode: entry.StatusCode}
				if entry.TypeName != "" {
					psr.TypeName = entry.TypeName
					resolveT := unwrapElementType(entry.TypeName)
					if target := findClassEntityByName(grp, resolveT); target != nil {
						if slug, _ := findRepoForEntity(grp, target.ID); slug != "" {
							psr.TypeEntityID = dashPrefixedID(slug, target.ID)
							psr.HasChildren = classHasFieldChildren(grp, target)
						}
					}
				}
				perStatusResponses = append(perStatusResponses, psr)
			}
		}
		sort.Slice(perStatusResponses, func(i, j int) bool {
			return perStatusResponses[i].StatusCode < perStatusResponses[j].StatusCode
		})
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

	// #1942 Phase 1 — pick the strongest auth_policy across all matched
	// handlers (a single path can have several verbs each with its own
	// policy; the detail page shows the most decisive verdict).
	var strongest engine.AuthPolicy
	strongest.Method = "unknown"
	strongest.Confidence = "low"
	bestRank := 0
	for _, h := range hits {
		_, tone := resolveAuthChip(h.AuthPolicy)
		if r := toneRank(tone); r > bestRank {
			bestRank = r
			strongest = h.AuthPolicy
		}
	}
	authChip, authChipTone := resolveAuthChip(strongest)
	authPolicyWire := authPolicyToWire(strongest)
	// Detail-level `auth` boolean now also reflects a structurally resolved
	// requirement (not only legacy `auth=true` / `auth_scheme=` props).
	if !auth && strongest.Required {
		auth = true
	}

	writeV2JSON(w, http.StatusOK, v2OK(v2PathDetail{
		PathHash:           pathHash,
		Path:               pathStr,
		Verbs:              verbs,
		Repos:              repos,
		IsWebhook:          isWebhook,
		WebhookProvider:    webhookProv,
		Auth:               auth,
		AuthPolicy:         authPolicyWire,
		AuthChip:           authChip,
		AuthChipTone:       authChipTone,
		AuthScheme:         authScheme,
		Description:        description,
		Parameters:         params,
		ResponseShapes:     responseShapes,
		PerStatusResponses: perStatusResponses,
		Handlers:           handlers,
		InboundFetches:     inboundFetches,
		Outbound:           outbound,
		SideEffects:        sideEffectEntities,
		Tests:              testEntities,
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

// describeJavaParam renders a short human-readable description for one
// parameter extracted by the Java annotation pass (#1936 Phase 1). The
// dashboard already renders the `In` chip; this string is shown in the
// description column and mentions default-value + key annotation hints.
func describeJavaParam(p engine.JavaParam) string {
	parts := make([]string, 0, 3)
	switch p.In {
	case "query":
		parts = append(parts, "Query parameter.")
	case "header":
		parts = append(parts, "Request header.")
	case "cookie":
		parts = append(parts, "Cookie value.")
	case "form":
		parts = append(parts, "Form field.")
	case "matrix":
		parts = append(parts, "Matrix parameter.")
	case "body":
		parts = append(parts, "Request body.")
	case "path":
		parts = append(parts, "Path segment.")
	}
	if p.DefaultValue != "" {
		parts = append(parts, "Default: "+p.DefaultValue+".")
	}
	if len(p.Annotations) > 0 {
		parts = append(parts, strings.Join(p.Annotations, " "))
	}
	return strings.TrimSpace(strings.Join(parts, " "))
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

// collectCrossRepoCallers scans grp.Links for cross-repo entries whose Target
// is in targetSet (the set of prefixed handler/definition entity IDs for this
// endpoint). The link pass (internal/links/http_pass.go) emits these links with
// Source=<caller-repo>:<caller-entity-id> and Target=<handler-repo>:<entity-id>,
// Relation="calls", Method="http". normalizeLinkEndpoints (graphstate.go)
// ensures both endpoints are in dashPrefixedID format ("<slug>:<entityID>")
// before they reach this function.
//
// For http_endpoint_definition entities that have not yet been resolved to
// their handler (old-style graphs), the link Target may point at the definition
// entity ID directly; both forms are accepted via the caller-provided targetSet.
func collectCrossRepoCallers(grp *DashGroup, targetSet map[string]bool) []string {
	if len(grp.Links) == 0 || len(targetSet) == 0 {
		return nil
	}
	// Scan links whose Target is one of our endpoint targets. The link
	// Relation is "calls" (lower-case) written by RelationCalls in
	// internal/links/links.go; accept any relation whose lowercase form
	// starts with "call" or "fetch" to be robust against format evolution.
	seen := map[string]bool{}
	var out []string
	for _, l := range grp.Links {
		if !targetSet[l.Target] {
			continue
		}
		rel := strings.ToLower(l.Kind)
		// Accept "calls" (http_pass default), "fetches" (retargeted callers),
		// "http", and "http_fetch". Reject unrelated link kinds (imports,
		// shared_label, etc.) so only HTTP call edges surface as callers.
		if !strings.HasPrefix(rel, "call") && !strings.HasPrefix(rel, "fetch") &&
			rel != "http" && rel != "http_fetch" {
			continue
		}
		src := l.Source
		if src == "" || seen[src] {
			continue
		}
		seen[src] = true
		out = append(out, src)
	}
	return out
}

// mergeUniqueStrings appends any elements of extra not already present in base.
func mergeUniqueStrings(base, extra []string) []string {
	if len(extra) == 0 {
		return base
	}
	seen := make(map[string]bool, len(base))
	for _, s := range base {
		seen[s] = true
	}
	for _, s := range extra {
		if !seen[s] {
			seen[s] = true
			base = append(base, s)
		}
	}
	return base
}

// resolveInboundFetches resolves a list of caller entity IDs for the
// "Called by" section. Issue #1908: when the resolved entity is an
// http_endpoint_call (its name is an HTTP URL pattern like
// "http:PUT:/transfers/..."), the useful information is NOT the URL template
// but the ACTUAL calling code entity (the function/method that makes the call).
//
// Resolution strategy:
//  1. Look up the entity by the prefixed ID.
//  2. If the entity's kind contains "http_endpoint_call" OR its name starts
//     with "http:" (the canonical http_endpoint_call naming convention), scan
//     the owning repo's relationship list for an entity that has a FETCHES or
//     CALLS edge TO this http_endpoint_call entity. Use that entity instead.
//  3. Fall back to the http_endpoint_call entity itself when no caller is found
//     (better than dropping the entry entirely).
func resolveInboundFetches(grp *DashGroup, ids []string) []v2PathEntity {
	out := make([]v2PathEntity, 0, len(ids))
	for _, id := range ids {
		repo, entity := findEntity(grp, id)
		if entity == nil {
			continue
		}

		// Determine if this entity is an http_endpoint_call that needs unwrapping.
		// http_endpoint_call entities carry the URL pattern as their name
		// (e.g. "http:PUT:/transfers/confirm/{transferId}") which is not useful
		// as a "Called by" label. We unwrap these to the actual caller entity.
		kind := dashStripScopePrefix(entity.Kind)
		isCallEntity := strings.EqualFold(kind, "http_endpoint_call") ||
			strings.EqualFold(entity.Kind, "http_endpoint_call") ||
			strings.Contains(strings.ToLower(entity.Kind), "http_endpoint_call") ||
			strings.HasPrefix(entity.Name, "http:")

		if isCallEntity && repo != nil {
			// Find the entity that FETCHES or CALLS this http_endpoint_call.
			// Relationships are stored at the Doc level (not on Entity).
			localID := entity.ID
			// Build an entity-by-ID lookup for this repo so we can quickly look up
			// the caller by its FromID.
			entityByID := make(map[string]*graph.Entity, len(repo.Doc.Entities))
			entityByName := make(map[string]*graph.Entity, len(repo.Doc.Entities))
			for i := range repo.Doc.Entities {
				e := &repo.Doc.Entities[i]
				entityByID[e.ID] = e
				entityByName[e.Name] = e
			}
			// resolveSyntheticFromID resolves synthetic "Kind:Name" IDs produced
			// during JS/TS extraction (e.g. "Function:confirmTransfer"). These IDs
			// don't match the real hex entity IDs, so we extract the name portion
			// and fall back to an entity-by-name lookup in the same repo.
			resolveSyntheticFromID := func(fromID string) *graph.Entity {
				if e := entityByID[fromID]; e != nil {
					return e
				}
				// Try "Kind:Name" synthetic id formats
				if idx := strings.LastIndex(fromID, ":"); idx >= 0 {
					name := fromID[idx+1:]
					if e := entityByName[name]; e != nil {
						return e
					}
				}
				return nil
			}
			for i := range repo.Doc.Relationships {
				rel := &repo.Doc.Relationships[i]
				if rel.ToID == localID &&
					(rel.Kind == "FETCHES" || rel.Kind == "CALLS") {
					if caller := resolveSyntheticFromID(rel.FromID); caller != nil {
						callerLabel := caller.Name
						callerQN := caller.QualifiedName
						if callerQN == "" {
							callerQN = callerLabel
						}
						out = append(out, v2PathEntity{
							Label:         callerLabel,
							QualifiedName: callerQN,
							Kind:          dashStripScopePrefix(caller.Kind),
							Repo:          repo.Slug,
							SourceFile:    caller.SourceFile,
							StartLine:     caller.StartLine,
							Edge:          "CALLED_BY",
							Protocol:      caller.Properties["protocol"],
						})
						goto nextID
					}
				}
			}
			// No intra-repo caller relationship found — also scan all other repos in
			// the group (cross-repo FETCHES edges from a consumer repo).
			callerEntityLocalID := entity.ID
			callerFound := false
			for _, otherRepo := range sortedRepos(grp) {
				if otherRepo.Slug == repo.Slug {
					continue
				}
				otherEntityByID := make(map[string]*graph.Entity, len(otherRepo.Doc.Entities))
				otherEntityByName := make(map[string]*graph.Entity, len(otherRepo.Doc.Entities))
				for i := range otherRepo.Doc.Entities {
					e := &otherRepo.Doc.Entities[i]
					otherEntityByID[e.ID] = e
					otherEntityByName[e.Name] = e
				}
				resolveOtherSyntheticID := func(fromID string) *graph.Entity {
					if e := otherEntityByID[fromID]; e != nil {
						return e
					}
					if idx := strings.LastIndex(fromID, ":"); idx >= 0 {
						name := fromID[idx+1:]
						if e := otherEntityByName[name]; e != nil {
							return e
						}
					}
					return nil
				}
				for i := range otherRepo.Doc.Relationships {
					rel := &otherRepo.Doc.Relationships[i]
					if rel.ToID == callerEntityLocalID &&
						(rel.Kind == "FETCHES" || rel.Kind == "CALLS") {
						if caller := resolveOtherSyntheticID(rel.FromID); caller != nil {
							callerLabel := caller.Name
							callerQN := caller.QualifiedName
							if callerQN == "" {
								callerQN = callerLabel
							}
							out = append(out, v2PathEntity{
								Label:         callerLabel,
								QualifiedName: callerQN,
								Kind:          dashStripScopePrefix(caller.Kind),
								Repo:          otherRepo.Slug,
								SourceFile:    caller.SourceFile,
								StartLine:     caller.StartLine,
								Edge:          "CALLED_BY",
								Protocol:      caller.Properties["protocol"],
							})
							callerFound = true
							goto nextID
						}
					}
				}
			}
			if !callerFound {
				// Fallback: surface the http_endpoint_call entity as-is — the
				// URL pattern is more informative than dropping the entry.
				repoSlug := ""
				if repo != nil {
					repoSlug = repo.Slug
				}
				label := entity.Name
				qn := entity.QualifiedName
				if qn == "" {
					qn = label
				}
				out = append(out, v2PathEntity{
					Label:         label,
					QualifiedName: qn,
					Kind:          kind,
					Repo:          repoSlug,
					SourceFile:    entity.SourceFile,
					StartLine:     entity.StartLine,
					Edge:          "CALLED_BY",
					Protocol:      entity.Properties["protocol"],
				})
			}
		} else {
			// Regular non-endpoint entity: use as-is.
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
				Kind:          kind,
				Repo:          repoSlug,
				SourceFile:    entity.SourceFile,
				StartLine:     entity.StartLine,
				Edge:          "CALLED_BY",
				Protocol:      entity.Properties["protocol"],
			})
		}
	nextID:
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
