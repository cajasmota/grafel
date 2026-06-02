<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `frontend.routing.spa-route-component` — Frontend route → component graph (SPA client routing)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [multi](../by-language/multi.md)
- **Category:** [platform](../by-category/platform.md)
- **Capability cells:** 2

## Capabilities

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Dependency attribution | 🟢 `partial` | `2026-06-02` | — | `internal/engine/frontend_route_edges.go`<br>`internal/engine/frontend_route_edges_test.go`<br>`internal/resolve/refs.go` | epic #3628: emits a ROUTES_TO edge SCOPE.Route(frontend) → component. The edge ToID is the BARE component identifier (e.g. "Users"), exactly as the JSX RENDERS edge does; the cross-file resolver (internal/resolve byName index) rewrites it to the declaring component entity (SCOPE.Function / SCOPE.Class) at pass 2. No new Kind introduced (reuses SCOPE.Route + ROUTES_TO). Value-asserting tests assert each specific route node id + route→component edge (feroute:App.tsx:/users ROUTES_TO Users; feroute:router.ts:/u/:id ROUTES_TO UserDetail; feroute:app.routes.ts:users ROUTES_TO UsersComponent; feroute:routes.tsx:/dash ROUTES_TO Dashboard). Honest-partial: dynamic template-literal paths (${…}) mint no node; a <Route> with no resolvable element/component mints no edge; non-PascalCase component refs are dropped. |
| Resource extraction | 🟢 `partial` | `2026-06-02` | — | `internal/engine/detector.go`<br>`internal/engine/frontend_route_edges.go`<br>`internal/engine/frontend_route_edges_test.go` | epic #3628: client-side routing graph for JS/TS SPAs — the previously-invisible table a router consults to decide WHICH COMPONENT renders for a URL path. New LIVE engine pass applyFrontendRouteEdges (registered in detector.go after applyAPIGatewayRoutingEdges; JS/TS only, gated, append-only). Mints a SCOPE.Route node keyed `feroute:<file>:<path>` with props synthesis=frontend_routing, scope=client, framework, route. This is a DISTINCT node from a backend SCOPE.Endpoint or an api-gateway SCOPE.Route (`route:<tool>:…`, synthesis=api_gateway_routing) even when the path string coincides — a frontend `/users` view and a backend `GET /users` API are different runtime concerns. Recognises React Router (`<Route path="/users" element={<Users/>}/>` + v5 `component={Users}`; `createBrowserRouter([{path,element}])`), Vue Router (`routes:[{path:'/u/:id', component:UserDetail}]`), Angular (`RouterModule.forRoot([{path:'users', component:UsersComponent}])`). Partial: covers the declarative config-table + JSX-element route forms only — Next.js/Remix file-based routing and fully programmatic/computed route configs are not yet modelled (follow-up). |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update frontend.routing.spa-route-component ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
