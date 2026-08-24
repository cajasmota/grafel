// http_endpoint_utoipa_axum.go — utoipa_axum `routes!(handler)` registrations →
// canonical http_endpoint_definition synthesis (#6560, Arm A).
//
// Background
// ----------
// synthesizeAxumRoutes (http_endpoint_axum.go) has exactly one route source:
// axumRouteRe, which requires a literal path string inside a `.route(...)` call.
// utoipa_axum registers differently — the verb and path live on the handler's
// `#[utoipa::path(...)]` attribute, and the registration names only the handler:
//
//	#[utoipa::path(get, path = "/items")]
//	async fn list_items() -> &'static str { "[]" }
//
//	OpenApiRouter::new().routes(routes!(list_items))
//
// No `.route(` call exists, so the axum loop finds nothing. The file-signal
// pre-filter is NOT what blocks it: axumHasAxum passes, because
// `utoipa_axum::router::OpenApiRouter` supplies the "axum" substring and
// `OpenApiRouter::new()` satisfies the `Router::` alternative. The pass runs and
// emits nothing.
//
// The attribute IS already parsed — internal/custom/rust/utoipa.go emits a
// `SCOPE.Operation` / Subtype="endpoint" marker carrying a `route_path`
// property — but that is a different entity family. As http_endpoint_vapor.go
// records for the identical Swift case (#4749), those markers are invisible to
// ResolveHTTPEndpointHandlers and to the e2e route-test linker
// (linkE2ERouteTestsToEndpoints), so the handler gets no IMPLEMENTS edge and its
// path can never be matched by a caller or a route-string test.
//
// This pass closes the PRODUCER-side gap in the same shape the Vapor bridge
// does: it emits one canonical http_endpoint_definition per statically-known
// utoipa_axum route, leaving the custom-extractor's SCOPE.Operation emit exactly
// as it is — so no existing consumer of `route_path` changes shape and the two
// producers stay separable.
//
// Ownership / deduplication
// -------------------------
// A file may register the same handler BOTH ways (`routes!(h)` and
// `.route("/x", get(h))`). Minting from both producers would recreate the
// duplicate-producer hazard of #6530, so this pass dedupes on the HANDLER NAME:
// any handler that appears as the handler argument of an axumRouteRe match in
// the same file is skipped here entirely. The `.route(...)` registration is the
// authoritative mount point (it is what the router actually serves); the
// attribute path is documentation, and where the two disagree, minting the
// attribute path as well would invent a second endpoint for one handler.
//
// Scope — Arm A only (#6560)
// --------------------------
// Deliberately NOT handled here; these are Arm B and are left to emit nothing
// rather than to emit a guess:
//
//   - multi-handler `routes!(a, b)` — the regex requires a single identifier
//     followed by the closing paren, so the multi form simply does not match.
//   - cross-module handlers (`routes!(crate::items::list)`, or a bare name whose
//     `#[utoipa::path]` attribute lives in another file) — the attribute map is
//     built from THIS file only, and an unknown handler is skipped. A fabricated
//     endpoint is worse than a missing one.
//   - `OpenApiRouter::nest("/prefix", ...)` prefix composition — the axum pass's
//     nest handling is not threaded onto these routes, so a nested utoipa router
//     yields the unprefixed attribute path.
package engine

import (
	"regexp"
	"strings"

	"github.com/cajasmota/grafel/internal/engine/httproutes"
)

// utoipaRoutesMacroRe matches the SINGLE-handler `routes!(handler)` form.
//
// The trailing `\s*\)` is what confines this pass to Arm A: `routes!(a, b)`
// does not match, and neither does a path-qualified `routes!(crate::x::h)`.
//
// Capture group 1 = handler identifier.
var utoipaRoutesMacroRe = regexp.MustCompile(`\broutes!\s*\(\s*([A-Za-z_]\w*)\s*\)`)

// utoipaPathAttrStartRe matches the start of a `#[utoipa::path(` attribute. The
// argument list is then read with a depth-counted paren scan because it nests
// (`responses((status = 200, body = Item))`, `params(...)`, …).
var utoipaPathAttrStartRe = regexp.MustCompile(`#\[\s*utoipa::path\s*\(`)

// utoipaAttrMethodRe extracts the HTTP method keyword, which utoipa takes as the
// first positional argument of the attribute.
var utoipaAttrMethodRe = regexp.MustCompile(
	`(?i)\b(get|post|put|delete|patch|head|options|trace|connect)\b`)

// utoipaAttrPathRe extracts `path = "/items/{id}"` from the attribute.
var utoipaAttrPathRe = regexp.MustCompile(`\bpath\s*=\s*"([^"\r\n]+)"`)

// utoipaAttrFnRe matches the `fn <name>` that the attribute decorates.
var utoipaAttrFnRe = regexp.MustCompile(`\bfn\s+([A-Za-z_]\w*)`)

// utoipaHasRoutesMacro is a fast pre-filter: the file must both mention utoipa
// and contain a `routes!` macro invocation.
//
// This is a CHEAP EARLY EXIT ONLY, not the correctness guard. What actually
// keeps Rocket's `rocket::routes!(...)` mount macro out of this pass is the
// same-file attribute map below: a Rocket file has no `#[utoipa::path(`, so the
// map is empty and nothing is minted. Dropping the "utoipa" substring test here
// changes no result in the suite (scored as a surviving mutant), so treat it as
// a performance filter and keep the attribute-map requirement load-bearing.
func utoipaHasRoutesMacro(content string) bool {
	return strings.Contains(content, "utoipa") && strings.Contains(content, "routes!")
}

// utoipaBalancedParens returns the text inside the paren group that opens at
// openParenOff (which must point at the '(' itself), plus the index just past
// the matching ')'.
func utoipaBalancedParens(src string, openParenOff int) (inner string, endOff int, ok bool) {
	if openParenOff >= len(src) || src[openParenOff] != '(' {
		return "", openParenOff, false
	}
	depth := 0
	for i := openParenOff; i < len(src); i++ {
		switch src[i] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return src[openParenOff+1 : i], i + 1, true
			}
		}
	}
	return "", openParenOff, false
}

// utoipaAttrRoute is the (verb, path) contract declared by one
// `#[utoipa::path(...)]` attribute.
type utoipaAttrRoute struct {
	verb string
	path string
}

// utoipaHandlerRoutes builds handler-name → (verb, path) for every
// `#[utoipa::path(...)]` attribute in this file that declares BOTH a method and
// a path and decorates a named function. A partial contract is skipped rather
// than completed with a default, mirroring internal/custom/rust/utoipa.go.
func utoipaHandlerRoutes(content string) map[string]utoipaAttrRoute {
	out := map[string]utoipaAttrRoute{}
	for _, m := range utoipaPathAttrStartRe.FindAllStringIndex(content, -1) {
		// m[1]-1 is the '(' opening the attribute argument list.
		inner, endOff, ok := utoipaBalancedParens(content, m[1]-1)
		if !ok {
			continue
		}
		mm := utoipaAttrMethodRe.FindStringSubmatch(inner)
		if mm == nil {
			continue
		}
		pm := utoipaAttrPathRe.FindStringSubmatch(inner)
		if pm == nil {
			continue
		}
		fm := utoipaAttrFnRe.FindStringSubmatch(content[endOff:])
		if fm == nil {
			continue
		}
		handler := fm[1]
		if _, dup := out[handler]; dup {
			// Two attributes on one handler name: keep the first and do not
			// guess which mount is real.
			continue
		}
		out[handler] = utoipaAttrRoute{
			verb: strings.ToUpper(mm[1]),
			path: pm[1],
		}
	}
	return out
}

// synthesizeUtoipaAxumRoutes scans a Rust source file for single-handler
// `routes!(handler)` registrations and emits one http_endpoint_definition per
// handler whose `#[utoipa::path(...)]` attribute is in the SAME file.
//
// Handlers already registered through a `.route(...)` call in this file are
// skipped — see the deduplication note in the file header.
//
// The handler kind is "Controller", the convention synthesizeAxumRoutes and
// synthesizeRocket use: resolverKindEquivalents maps Controller → SCOPE.Operation
// (the kind Rust functions land as), so the http-endpoint-resolve pass finds the
// handler entity and emits its IMPLEMENTS edge.
func synthesizeUtoipaAxumRoutes(content string, emit emitFn) {
	if !utoipaHasRoutesMacro(content) {
		return
	}
	attrs := utoipaHandlerRoutes(content)
	if len(attrs) == 0 {
		return
	}

	// Dedupe key: handler name already minted by synthesizeAxumRoutes from a
	// `.route("path", verb(handler))` registration in this same file.
	routeRegistered := map[string]bool{}
	for _, m := range axumRouteRe.FindAllStringSubmatch(content, -1) {
		if len(m) < 4 {
			continue
		}
		routeRegistered[m[3]] = true
	}

	minted := map[string]bool{}
	for _, m := range utoipaRoutesMacroRe.FindAllStringSubmatch(content, -1) {
		if len(m) < 2 {
			continue
		}
		handler := m[1]
		if routeRegistered[handler] || minted[handler] {
			continue
		}
		route, known := attrs[handler]
		if !known {
			// No same-file contract for this handler — Arm B territory.
			continue
		}
		canonical := httproutes.Canonicalize(httproutes.FrameworkAxum, route.path)
		if canonical == "" {
			continue
		}
		minted[handler] = true
		emit(route.verb, canonical, "utoipa_axum", "Controller", handler)
	}
}
