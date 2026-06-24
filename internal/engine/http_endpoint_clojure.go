// http_endpoint_clojure.go — Clojure web route registration → canonical
// http_endpoint_definition synthesis (#4749, the Clojure slice of the
// coverage-linkage tail epic #4749/#4615).
//
// Background
// ----------
// The Clojure base extractor (internal/extractors/clojure/clojure.go) emits
// namespaces, `defn` functions, `deftype` and IMPORTS/CALLS/CONTAINS edges,
// and the Clojure framework rule manifests (internal/engine/rules/clojure/
// frameworks/{compojure,reitit,ring,pedestal}.yaml) DETECT that a web framework
// is present — but neither produces `http_endpoint_definition` entities. So the
// shared endpoint resolver (ResolveHTTPEndpointHandlers) and, in particular, the
// language-agnostic e2e route-test linker (linkE2ERouteTestsToEndpoints, #4351)
// had no Clojure endpoint to bind a route-string test call to.
//
// This pass closes the PRODUCER-side gap: it emits one canonical
// http_endpoint_definition per statically-known Clojure route, in the SAME shape
// the axum / Rocket / Vapor / Express synthesizers emit, so the existing
// resolver and the e2e route-test linker light up for Clojure exactly as they
// do for the flagship stacks. The route-hit `e2e_route_calls` test side is
// emitted by the custom_clojure_tests_route_e2e extractor.
//
// Clojure web route syntax
// ------------------------
// Clojure is FUNCTIONAL — routes are data/macros, not OO controllers:
//
//   - Compojure macros:  (GET  "/users/:id" [] handler)
//     (POST "/users"     req (create req))
//     (defroutes app (GET "/todos" [] list-todos) ...)
//     The verb is the leading macro symbol (GET/POST/PUT/DELETE/PATCH/...),
//     the path is the FIRST string literal, the handler is the trailing form.
//
//   - Reitit data routes: ["/users/:id" {:get  get-user
//     :post create-user}]
//     A route is a vector whose FIRST element is a string-literal path and whose
//     SECOND element is a map of `:verb handler` pairs.
//
// Both use Ring's Express-style `:name` colon path-parameter convention, so the
// canonicaliser (FrameworkClojure → canonicalizeColonParams) folds `:id` to
// `{id}` — the shape every other framework's endpoints share.
//
// Honest exclusions (no fabricated routes)
// ----------------------------------------
//   - Interpolated / variable / `(str ...)`-built paths — the path must be a
//     STRING LITERAL; a non-literal first arg is dropped (not recoverable).
//   - Compojure `(context "/prefix" [] ...)` prefix nesting is NOT threaded onto
//     the inner routes (single-form scan); the inner routes are still emitted at
//     their own path. Context-prefix threading is a documented follow-up.
//   - Reitit `:handler`-style single-handler maps (`{:handler h}` with no verb)
//     emit an ANY endpoint; verb-keyed maps emit one endpoint per verb.
package engine

import (
	"regexp"
	"strings"

	"github.com/cajasmota/grafel/internal/engine/httproutes"
)

// cljCompojureRouteRe matches a Compojure verb macro:
//
//	(GET "/users/:id" [] handler)
//	(POST "/users" req (create req))
//
// Capture group 1 is the verb macro symbol; group 2 is the string-literal path.
// The macro must be the first symbol after an open paren so a verb-named local
// or a string elsewhere is never misread.
var cljCompojureRouteRe = regexp.MustCompile(
	`\(\s*(GET|POST|PUT|DELETE|PATCH|HEAD|OPTIONS|ANY)\s+"([^"\n\r]*)"`,
)

// cljReititRouteRe matches the head of a Reitit route vector:
//
//	["/users/:id" {:get get-user :post create-user}]
//	["/health" {:get health}]
//
// Capture group 1 is the string-literal path; group 2 is the verb-map body up to
// the closing brace. The path must be the FIRST element of the vector (a string
// literal immediately after `[`).
var cljReititRouteRe = regexp.MustCompile(
	`\[\s*"(/[^"\n\r]*)"\s*\{([^}]*)\}`,
)

// cljReititVerbRe extracts each `:verb` keyword from a Reitit route-data map.
var cljReititVerbRe = regexp.MustCompile(
	`:(get|post|put|delete|patch|head|options)\b`,
)

// cljPedestalRouteRe matches a Pedestal terse/table route vector head:
//
//	["/users/:id" :get  get-user :route-name :get-user]
//	["/users"     :post create-user]
//
// In Pedestal's table-routing syntax a route is a vector whose FIRST element
// is a string-literal path and whose SECOND element is the HTTP-verb keyword
// (`:get`/`:post`/…). The interceptor/handler follows. Capture group 1 is the
// literal path; group 2 is the verb keyword. The verb keyword immediately
// after the path string is what distinguishes a Pedestal table route from a
// Reitit route (whose second element is a `{...}` data map).
var cljPedestalRouteRe = regexp.MustCompile(
	`\[\s*"(/[^"\n\r]*)"\s+:(get|post|put|delete|patch|head|options|any)\b`,
)

// cljHasWebRoutes is a fast pre-filter: the file must reference a Compojure verb
// macro or a Reitit/Ring router marker to be worth scanning.
func cljHasWebRoutes(content string) bool {
	if strings.Contains(content, "(GET ") || strings.Contains(content, "(GET\"") ||
		strings.Contains(content, "(POST ") || strings.Contains(content, "(PUT ") ||
		strings.Contains(content, "(DELETE ") || strings.Contains(content, "(PATCH ") ||
		strings.Contains(content, "(ANY ") || strings.Contains(content, "defroutes") {
		return true
	}
	// Reitit data routes are framework-marked by a ring/router or reitit require.
	if strings.Contains(content, "reitit") &&
		(strings.Contains(content, "ring/router") || strings.Contains(content, ":get") ||
			strings.Contains(content, ":post")) {
		return true
	}
	// Pedestal table routes are framework-marked by an io.pedestal require and
	// carry `["/path" :verb ...]` route vectors.
	return strings.Contains(content, "io.pedestal") &&
		(strings.Contains(content, ":get") || strings.Contains(content, ":post") ||
			strings.Contains(content, ":put") || strings.Contains(content, ":delete"))
}

// synthesizeClojureRoutes scans a Clojure source file for Compojure macro routes
// and Reitit data routes and emits one http_endpoint_definition per
// statically-known (verb, path).
func synthesizeClojureRoutes(content string, emit emitFn) {
	if !cljHasWebRoutes(content) {
		return
	}

	emitRoute := func(verb, rawPath, framework string) {
		rawPath = strings.TrimSpace(rawPath)
		if rawPath == "" || !strings.HasPrefix(rawPath, "/") {
			return
		}
		canonical := httproutes.Canonicalize(httproutes.FrameworkClojure, rawPath)
		if canonical == "" {
			return
		}
		// Handler kind "Controller" maps to SCOPE.Operation in the resolver — the
		// kind Clojure `defn` handlers land as — matching the axum/Vapor
		// convention so ResolveHTTPEndpointHandlers can bind a handler IMPLEMENTS
		// edge when a same-named handler exists (no name forces one when absent).
		emit(strings.ToUpper(verb), canonical, framework, "Controller", "")
	}

	// Compojure verb macros.
	for _, m := range cljCompojureRouteRe.FindAllStringSubmatch(content, -1) {
		if len(m) < 3 {
			continue
		}
		emitRoute(m[1], m[2], "compojure")
	}

	// Reitit data routes — one endpoint per verb declared in the route-data map.
	for _, m := range cljReititRouteRe.FindAllStringSubmatch(content, -1) {
		if len(m) < 3 {
			continue
		}
		path := m[1]
		verbs := cljReititVerbRe.FindAllStringSubmatch(m[2], -1)
		if len(verbs) == 0 {
			// A verb-less route-data map (`{:handler h}` / `{:name ::x}`) is an ANY
			// mount — emit a single catch-all endpoint so the route still surfaces.
			if strings.Contains(m[2], ":handler") {
				emitRoute("ANY", path, "reitit")
			}
			continue
		}
		for _, vm := range verbs {
			emitRoute(vm[1], path, "reitit")
		}
	}

	// Pedestal table routes — `["/path" :verb handler ...]`. The verb keyword
	// directly follows the literal path; emit one endpoint per (verb, path).
	// Reitit's second element is a `{...}` data map, so the verb-keyword shape
	// here never double-fires on a Reitit route.
	if strings.Contains(content, "io.pedestal") {
		for _, m := range cljPedestalRouteRe.FindAllStringSubmatch(content, -1) {
			if len(m) < 3 {
				continue
			}
			emitRoute(m[2], m[1], "pedestal")
		}
	}
}
