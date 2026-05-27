// http_endpoint_rocket.go — Rust Rocket attribute macros → http_endpoint_definition synthesis.
//
// Rocket uses attribute macros on handler functions in the same file:
//
//	#[get("/hello")]
//	fn hello() -> &'static str { "world" }
//
//	#[post("/users/<id>", data = "<user>")]
//	fn create_user(id: u32, user: Json<User>) -> Status { Status::Ok }
//
// Path parameters use angle-bracket syntax (`<id>`) identical to Django /
// Flask; canonicalisation reuses the angle-bracket walker via
// FrameworkRocket. Trailing macro arguments (`data = "..."`,
// `format = "..."`, `rank = N`) are tolerated.
//
// Rocket also requires `rocket::routes![handler1, handler2, ...]` (or the
// `#[launch]` builder) to be registered for the route to actually fire,
// but for the static endpoint catalogue we treat the attribute macro
// itself as authoritative. Unused handlers behind the attribute are rare
// and never harmful to surface.
//
// Same-file by construction: the function definition follows the attribute
// on the next line, so source_file is already correct on the synthetic.
// The ResolveHTTPEndpointHandlers rebind (#2680) populates start_line by
// looking up the resolved handler entity.
//
// Refs #2692.
package engine

import (
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/engine/httproutes"
)

// rocketRouteAttrRe matches a Rocket HTTP verb attribute and the function
// it decorates. We allow trailing macro arguments (any combination of
// `data="..."`, `format="..."`, `rank=N`) and intervening attributes
// (`#[cfg(...)]`, etc.) between the verb attribute and the `fn` keyword.
//
// Capture groups:
//
//	1 = verb (get / post / put / patch / delete / head / options)
//	2 = path string
//	3 = function name
var rocketRouteAttrRe = regexp.MustCompile(
	`#\[\s*(get|post|put|patch|delete|head|options)\s*\(\s*"([^"\r\n]+)"[^)]*\)\s*\]` +
		`\s*(?:[\r\n]+(?:\s*#\[[^\]\r\n]*\]\s*[\r\n]+)*)?\s*` +
		`(?:pub(?:\s*\([^)]*\))?\s+)?(?:async\s+)?fn\s+([A-Za-z_]\w*)\s*\(`,
)

// rocketHasRoutes is a fast pre-filter: returns true when the file
// references Rocket or its attribute macros.
func rocketHasRoutes(content string) bool {
	if !strings.Contains(content, "rocket") &&
		!strings.Contains(content, "Rocket") {
		// Some downstream files may only contain `#[get(...)]` without an
		// explicit `rocket` import (it's pulled in by the crate-level
		// `#[macro_use] extern crate rocket;`). Fall back to the verb
		// attribute shape itself.
		if !strings.Contains(content, "#[get(") &&
			!strings.Contains(content, "#[post(") &&
			!strings.Contains(content, "#[put(") &&
			!strings.Contains(content, "#[patch(") &&
			!strings.Contains(content, "#[delete(") &&
			!strings.Contains(content, "#[head(") &&
			!strings.Contains(content, "#[options(") {
			return false
		}
	}
	return true
}

// synthesizeRocket scans a Rust source file for Rocket route-attribute
// macros and calls emit for each (verb, canonical-path, framework,
// handlerKind, handlerName) tuple.
//
// The handler-kind is "Controller" to reuse the resolverKindEquivalents
// fallback that maps to the Rust extractor's SCOPE.Operation kind (the
// same convention synthesizeAxumRoutes uses, see http_endpoint_axum.go).
// This keeps the handler-resolution path identical between the two Rust
// frameworks.
func synthesizeRocket(content string, emit emitFn) {
	if !rocketHasRoutes(content) {
		return
	}
	for _, m := range rocketRouteAttrRe.FindAllStringSubmatch(content, -1) {
		if len(m) < 4 {
			continue
		}
		verb := strings.ToUpper(m[1])
		raw := m[2]
		handler := m[3]
		canonical := httproutes.Canonicalize(httproutes.FrameworkRocket, raw)
		if canonical == "" {
			continue
		}
		emit(verb, canonical, "rocket", "Controller", handler)
	}
}
