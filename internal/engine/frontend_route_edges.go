// Frontend route → component graph synthesis — epic #3628.
//
// Backend routing is already modelled: HTTP endpoints (SCOPE.Endpoint), API-
// gateway routes (api_gateway_routing_edges.go: SCOPE.Route -ROUTES_TO-> backend
// SCOPE.Service), and k8s/ingress topology. The CLIENT-SIDE routing graph — the
// table a single-page-app router consults to decide WHICH COMPONENT renders for
// a given URL path — was previously invisible. The only client-side routing
// signal in the graph was NAVIGATES_TO, which models a *navigation call site* or
// link directive (caller-op -> route stub); it does NOT model the route
// *definition* nor the component a route renders. Notably the Angular route-table
// scan (angular_nav_lifecycle.go) parsed `path:` but DROPPED the paired
// `component:`, so no route -> component edge existed for any framework.
//
// This pass mints the missing definitional half:
//
//	SCOPE.Route(frontend)  -- ROUTES_TO -->  component (SCOPE.Function / SCOPE.Class)
//
// It recognises the declarative route tables of the three config-driven SPA
// routers plus React's element-prop JSX form:
//
//   - React Router: <Route path="/users" element={<Users/>} />  and the
//     component={Users} form; createBrowserRouter([{path:'/users',
//     element:<Users/>}]) object form.
//   - Vue Router:   const routes = [{ path: '/users', component: Users }]
//   - Angular:      RouterModule.forRoot([{ path: 'users', component: UsersComponent }])
//
// # Node model + distinction from backend endpoints
//
// The route node is keyed `feroute:<file>:<path>` and stamped
// Properties["synthesis"]="frontend_routing" + ["scope"]="client". This keeps it
// a DISTINCT node from a backend SCOPE.Endpoint or an api-gateway SCOPE.Route
// (keyed `route:<tool>:…`, synthesis="api_gateway_routing") even when the path
// string coincides — a frontend `/users` view and a backend `GET /users` API are
// different graph entities, as they are different runtime concerns.
//
// The ROUTES_TO edge's ToID is the BARE component identifier (e.g. "Users"),
// exactly as the JSX RENDERS edge does (extractor.go) — the cross-file resolver
// (internal/resolve, byName index) rewrites it to the declaring component entity
// at pass 2. Self-contained: no new Kind is introduced (SCOPE.Route + ROUTES_TO
// already exist).
//
// # Honest-partial
//
// Only statically-resolvable path+component pairs emit. A dynamic path (`path:
// pathVar`, a template literal with `${…}`) or a non-PascalCase / unresolvable
// component reference is skipped rather than emitting a garbage node/edge.
//
// # Scope guard
//
// Append-only and gated: fires only for JS/TS files whose content contains a
// recognised router marker; every other file is a fast no-op. It never modifies
// or removes existing entities/edges, so it cannot regress the pipeline.
//
// Epic #3628. DEPLOY-DEFERRED.
package engine

import (
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/types"
)

const (
	feRouteKind     = string(types.EntityKindRoute)
	feRouteRoutesTo = string(types.RelationshipKindRoutesTo)
)

// feRouteID keys a frontend route node distinctly from backend / api-gateway
// SCOPE.Route nodes (which are keyed "route:<tool>:…"). The "feroute:" prefix +
// file scoping guarantees no collision with a backend route of the same path.
func feRouteID(file, path string) string {
	return "feroute:" + file + ":" + path
}

// reFEReactRouteElement matches a React Router <Route … /> JSX element and
// captures the path attribute (group 1). The element/component attribute is
// resolved separately from the same element text so attribute order is
// irrelevant.
var reFEReactRouteElement = regexp.MustCompile(`(?s)<Route\b([^>]*?)/?>`)

// reFEPathAttr matches a path="…" JSX attribute (string literal form).
var reFEPathAttr = regexp.MustCompile(`\bpath\s*=\s*["']([^"']*)["']`)

// reFEElementJSX matches element={<Comp …/>} and captures the component tag.
var reFEElementJSX = regexp.MustCompile(`\belement\s*=\s*\{\s*<\s*([A-Za-z_$][\w$]*)`)

// reFEComponentAttr matches component={Comp} (React Router v5) and the object-
// form `component: Comp` (Vue / Angular / createBrowserRouter). Capture group 1
// is the component identifier.
var reFEComponentAttr = regexp.MustCompile(`\bcomponent\s*[=:]\s*\{?\s*([A-Za-z_$][\w$]*)`)

// reFEObjectRoute matches a single `{ … path: '…' … }` route-config object and
// captures the path literal (group 1). Used for createBrowserRouter / Vue
// `routes:[…]` / Angular `RouterModule.forRoot([…])` object-form tables.
var reFEObjectRoute = regexp.MustCompile(`path\s*:\s*["'` + "`" + `]([^"'` + "`" + `]*)["'` + "`" + `]`)

// reFEElementObj matches the object-form `element: <Comp …/>` (createBrowserRouter).
var reFEElementObj = regexp.MustCompile(`element\s*:\s*<\s*([A-Za-z_$][\w$]*)`)

// feRouterMarkers gate the pass: the file must mention at least one to do any work.
var feRouterMarkers = []string{
	"<Route",              // react-router JSX
	"createBrowserRouter", // react-router data-router
	"createHashRouter",
	"createRoutesFromElements",
	"vue-router",   // vue
	"createRouter", // vue
	"RouterModule", // angular
	"@angular/router",
	"routes", // generic object table (cheap pre-filter; refined below)
}

// isFEComponentName reports whether s is a PascalCase component identifier — the
// React/Vue/Angular convention. Mirrors isComponentName in the JS extractor.
func isFEComponentName(s string) bool {
	if s == "" {
		return false
	}
	r := rune(s[0])
	return r >= 'A' && r <= 'Z'
}

// feDynamicPath reports whether a captured path string is statically
// unresolvable (contains a template-literal interpolation). Parametric segments
// like ":id" or "[id]" are legitimate STATIC route declarations and are kept.
func feDynamicPath(p string) bool {
	return strings.Contains(p, "${")
}

// feNormalizePath gives an empty path a stable display label so the index/
// default child route is still a navigable node.
func feNormalizePath(p string) string {
	if p == "" {
		return "<index>"
	}
	return p
}

// applyFrontendRouteEdges is the per-file engine pass. Append-only.
func applyFrontendRouteEdges(args DetectorPassArgs) DetectorPassResult {
	entities := args.Entities
	relationships := args.Relationships

	if args.Lang != "javascript" && args.Lang != "typescript" {
		return DetectorPassResult{Entities: entities, Relationships: relationships}
	}
	if len(args.Content) == 0 {
		return DetectorPassResult{Entities: entities, Relationships: relationships}
	}
	src := string(args.Content)

	gated := false
	for _, m := range feRouterMarkers {
		if strings.Contains(src, m) {
			gated = true
			break
		}
	}
	if !gated {
		return DetectorPassResult{Entities: entities, Relationships: relationships}
	}

	seenEnt := map[string]bool{}
	seenEdge := map[string]bool{}

	// emit mints (route node, ROUTES_TO edge) for one resolved path+component
	// pair. Dynamic paths and non-component refs are dropped by the callers.
	emit := func(path, component, framework, via string) {
		path = feNormalizePath(strings.TrimSpace(path))
		component = strings.TrimSpace(component)
		if !isFEComponentName(component) {
			return
		}
		id := feRouteID(args.Path, path)
		if !seenEnt[id] {
			seenEnt[id] = true
			entities = append(entities, types.EntityRecord{
				ID:            id,
				Name:          path,
				QualifiedName: id,
				Kind:          feRouteKind,
				SourceFile:    args.Path,
				Language:      args.Lang,
				Properties: map[string]string{
					"synthesis": "frontend_routing",
					"scope":     "client",
					"framework": framework,
					"route":     path,
				},
				EnrichmentStatus: types.StatusPending,
				QualityScore:     0.8,
			})
		}
		edgeKey := id + "|" + component
		if seenEdge[edgeKey] {
			return
		}
		seenEdge[edgeKey] = true
		relationships = append(relationships, types.RelationshipRecord{
			FromID: id,
			ToID:   component, // bare name; cross-file resolver binds to the component entity
			Kind:   feRouteRoutesTo,
			Properties: map[string]string{
				"synthesis": "frontend_routing",
				"scope":     "client",
				"framework": framework,
				"via":       via,
				"route":     path,
			},
		})
	}

	// 1. React Router JSX <Route path=… element={<Comp/>} | component={Comp} />.
	for _, m := range reFEReactRouteElement.FindAllStringSubmatch(src, -1) {
		attrs := m[1]
		pm := reFEPathAttr.FindStringSubmatch(attrs)
		if pm == nil || feDynamicPath(pm[1]) {
			continue
		}
		comp := ""
		if em := reFEElementJSX.FindStringSubmatch(attrs); em != nil {
			comp = em[1]
		} else if cm := reFEComponentAttr.FindStringSubmatch(attrs); cm != nil {
			comp = cm[1]
		}
		if comp != "" {
			emit(pm[1], comp, "react_router", "jsx_route_element")
		}
	}

	// 2. Object-form route tables: createBrowserRouter([{path, element|component}]),
	//    Vue `routes:[{path, component}]`, Angular RouterModule.forRoot([{path,
	//    component}]). Split the source on `path:` occurrences so each route
	//    object's path is paired with the NEAREST following component/element ref
	//    within the same object literal window.
	framework := feFramework(src)
	for _, loc := range reFEObjectRoute.FindAllStringSubmatchIndex(src, -1) {
		path := src[loc[2]:loc[3]]
		if feDynamicPath(path) {
			continue
		}
		// Window: from the end of the path match to the next `path:` (or +400
		// chars), so a route's component is matched within its own object and
		// does not leak from a sibling route.
		winStart := loc[1]
		winEnd := winStart + 400
		if winEnd > len(src) {
			winEnd = len(src)
		}
		if next := reFEObjectRoute.FindStringIndex(src[winStart:]); next != nil {
			if cand := winStart + next[0]; cand < winEnd {
				winEnd = cand
			}
		}
		window := src[winStart:winEnd]
		comp := ""
		if em := reFEElementObj.FindStringSubmatch(window); em != nil {
			comp = em[1]
		} else if cm := reFEComponentAttr.FindStringSubmatch(window); cm != nil {
			comp = cm[1]
		}
		if comp != "" {
			emit(path, comp, framework, "route_table_object")
		}
	}

	return DetectorPassResult{Entities: entities, Relationships: relationships}
}

// feFramework infers the SPA router framework from import / API markers in the
// file, for the object-form table whose syntax is shared across routers.
func feFramework(src string) string {
	switch {
	case strings.Contains(src, "@angular/router") || strings.Contains(src, "RouterModule"):
		return "angular"
	case strings.Contains(src, "vue-router") || strings.Contains(src, "createRouter"):
		return "vue_router"
	case strings.Contains(src, "react-router") || strings.Contains(src, "createBrowserRouter") ||
		strings.Contains(src, "createHashRouter"):
		return "react_router"
	default:
		return "spa_router"
	}
}
