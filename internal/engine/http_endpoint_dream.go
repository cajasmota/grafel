// http_endpoint_dream.go — OCaml Dream web route registration → canonical
// http_endpoint_definition synthesis (#5374, the OCaml slice of the low-ranked-
// language bootstrap epic #5360).
//
// Background
// ----------
// The OCaml base extractor (internal/extractors/ocaml/extractor.go) is a
// regex-based structural extractor: it mines modules, let/let-rec functions,
// type declarations and IMPORTS/CALLS/CONTAINS edges, but has NO web-framework
// awareness — Dream `Dream.get "/users/:id" handler` route registrations are not
// recognised as HTTP endpoints, so no `http_endpoint_definition` entity is ever
// produced for OCaml. The shared endpoint resolver
// (ResolveHTTPEndpointHandlers) and the language-agnostic e2e route-test linker
// (linkE2ERouteTestsToEndpoints, #4351) both key off
// `http_endpoint_definition` + `path`, so an OCaml route could never surface.
//
// This pass closes the PRODUCER-side gap: it emits one canonical
// http_endpoint_definition per statically-known Dream route, in the SAME shape
// the axum / Scotty / Kemal / Vapor synthesizers emit.
//
// Dream route syntax
// ------------------
// Dream (https://aantron.github.io/dream/) registers routes by passing a list of
// verb-route values to `Dream.router`:
//
//	Dream.router [
//	  Dream.get  "/"            home_handler;
//	  Dream.get  "/users/:id"   show_user;
//	  Dream.post "/users"       create_user;
//	  Dream.delete "/users/:id" delete_user;
//	]
//
// Dream uses the Sinatra/Express-style `:name` colon path-parameter convention,
// canonicalised through FrameworkDream into the shared `{param}` form.
//
// Honest exclusions (no fabricated routes)
// ----------------------------------------
//   - Interpolated / variable / non-literal paths (`Dream.get path handler`,
//     `Dream.get (prefix ^ "/x") h`) — the path must be a STRING LITERAL.
//   - `Dream.scope "/api" middlewares [ ... ]` prefix grouping is NOT threaded:
//     a route declared inside a scope is emitted with its OWN literal path, not
//     the scope-prefixed path. Scope-prefix composition is a documented
//     follow-up (mirrors the Crystal/Nim/Haskell bootstraps, which likewise emit
//     the inline literal only).
//   - WebSocket routes (`Dream.get "/ws" (Dream.websocket ...)`) are still HTTP
//     GET upgrades and ARE emitted (the verb is a real GET); the websocket
//     handler body is not inspected.
package engine

import (
	"regexp"
	"strings"

	"github.com/cajasmota/grafel/internal/engine/httproutes"
)

// dreamRouteRe matches a Dream verb route registration with a leading
// string-literal path:
//
//	Dream.get  "/users/:id" handler
//	Dream.post "/users"     handler
//
// Group 1 is the verb; group 2 is the path literal. The `Dream.` qualifier is
// required so an arbitrary `get`/`post` function is never misread.
var dreamRouteRe = regexp.MustCompile(
	`\bDream\.(get|post|put|delete|patch|options|head)\s+"([^"\n\r]*)"`,
)

// dreamHasRoute is a fast pre-filter: the file must reference the Dream module
// and at least one verb-route registration to be worth scanning.
func dreamHasRoute(content string) bool {
	if !strings.Contains(content, "Dream.") {
		return false
	}
	return strings.Contains(content, "Dream.get ") ||
		strings.Contains(content, "Dream.post ") ||
		strings.Contains(content, "Dream.put ") ||
		strings.Contains(content, "Dream.delete ") ||
		strings.Contains(content, "Dream.patch ") ||
		strings.Contains(content, "Dream.options ") ||
		strings.Contains(content, "Dream.head ")
}

// synthesizeDreamRoutes scans an OCaml source file for Dream verb routes and
// emits one http_endpoint_definition per statically-known (verb, path).
func synthesizeDreamRoutes(content string, emit emitFn) {
	if !dreamHasRoute(content) {
		return
	}
	for _, m := range dreamRouteRe.FindAllStringSubmatch(content, -1) {
		if len(m) < 3 {
			continue
		}
		raw := strings.TrimSpace(m[2])
		if raw == "" {
			continue
		}
		if !strings.HasPrefix(raw, "/") {
			raw = "/" + raw
		}
		canonical := httproutes.Canonicalize(httproutes.FrameworkDream, raw)
		if canonical == "" {
			continue
		}
		emit(strings.ToUpper(m[1]), canonical, "dream", "Controller", "")
	}
}
