// Rust/axum HTTP route extraction — producer side.
//
// Parses axum Router::new().route("/path", verb(handler)) and
// Router::new().nest("/prefix", inner_router) patterns to emit
// http_endpoint_definition entities for every statically-known route.
//
// Patterns covered:
//
//   - .route("/path", get(handler))    → GET /path  handler=Function:handler
//   - .route("/path", post(handler))   → POST /path
//   - .route("/path", put(handler))    → PUT /path
//   - .route("/path", patch(handler))  → PATCH /path
//   - .route("/path", delete(handler)) → DELETE /path
//   - .route("/path", head(handler))   → HEAD /path
//   - .route("/path", options(handler))→ OPTIONS /path
//   - .nest("/prefix", ...)            → prefix is prepended to inner routes
//     (single-level static prefix only — dynamic nesting is skipped)
//
// axum uses {param} curly-brace path parameters identical to FastAPI/JAX-RS
// so FrameworkAxum reuses canonicalizeCurlyBraces via Canonicalize.
//
// Refs #1420.
package engine

import (
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/engine/httproutes"
)

// axumRouteRe matches `.route("path", verb(handler))` calls.
//
// Capture groups:
//
//	1 = path string (double-quoted)
//	2 = HTTP verb function name (get/post/put/patch/delete/head/options)
//	3 = handler identifier
var axumRouteRe = regexp.MustCompile(
	`\.route\s*\(\s*"([^"\n\r]+)"\s*,\s*(get|post|put|patch|delete|head|options)\s*\(\s*([A-Za-z_]\w*)`,
)

// axumNestRe matches `.nest("prefix", ...)` calls to extract the static prefix.
//
// Capture groups:
//
//	1 = prefix string (double-quoted)
var axumNestRe = regexp.MustCompile(
	`\.nest\s*\(\s*"([^"\n\r]+)"\s*,`,
)

// axumHasAxum is a fast pre-filter: returns true when the file imports axum
// or uses Router:: / .route( patterns.
func axumHasAxum(content string) bool {
	return strings.Contains(content, "axum") &&
		(strings.Contains(content, ".route(") || strings.Contains(content, "Router::"))
}

// synthesizeAxumRoutes scans a Rust source file for axum route registrations
// and emits one http_endpoint_definition per (verb, path) pair found.
//
// Single-level .nest("/prefix", ...) prefixes are applied to all .route(...)
// calls that appear BEFORE the nest in the byte stream (the common
// `Router::new().route(...).route(...)` form) or in the immediately
// preceding function scope. Additionally, routes that follow a nest call
// within 2000 bytes and without an intervening Router::new() are also
// prefixed (covers inline nest chains).
//
// The handler function name is passed as the refName so the synthesiser can
// stamp source_handler=Function:<name> on the entity.
func synthesizeAxumRoutes(content string, emit emitFn) {
	if !axumHasAxum(content) {
		return
	}

	// Collect nest prefixes. We scan all .nest("prefix", ...) occurrences
	// and record their byte positions so each .route() call can be
	// associated with the nearest preceding nest prefix in the same
	// method-chain scope.
	type nestEntry struct {
		offset int
		prefix string
	}
	var nests []nestEntry
	for _, m := range axumNestRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 || m[2] < 0 {
			continue
		}
		prefix := content[m[2]:m[3]]
		nests = append(nests, nestEntry{offset: m[0], prefix: prefix})
	}

	// nestPrefixFor returns the best nest prefix that should be applied to
	// a .route() call at routeOffset.
	//
	// Two strategies are tried in order:
	//
	// 1. Inline chain: a .nest("prefix", ...) that appears BEFORE the
	//    .route() within 2000 bytes and the text between them contains at
	//    most one Router::new() — covers
	//    `Router::new().nest("/api", Router::new().route(...))`.
	//
	// 2. Outer-function nest: the routes in a helper function
	//    (e.g. `orders_router()`) are all called before the .nest()
	//    that mounts them. We therefore also look for a .nest() that
	//    appears AFTER the .route() but within 2000 bytes ahead, as long
	//    as no Router::new() separates them.
	nestPrefixFor := func(routeOffset int) string {
		bestDist := -1
		bestPrefix := ""

		for _, n := range nests {
			var dist int
			if n.offset < routeOffset {
				// nest precedes route — inline chain
				dist = routeOffset - n.offset
			} else {
				// nest follows route — outer mounting pattern
				dist = n.offset - routeOffset
			}
			if dist > 2000 {
				continue
			}
			// Determine the text window between the two.
			start, end := n.offset, routeOffset
			if n.offset > routeOffset {
				start, end = routeOffset, n.offset
			}
			between := content[start:end]
			// Allow at most one Router::new() (the one that .nest() or
			// .route() is being called on). More than one means a different
			// router scope.
			if strings.Count(between, "Router::new()") > 1 {
				continue
			}
			if bestDist < 0 || dist < bestDist {
				bestDist = dist
				bestPrefix = n.prefix
			}
		}
		return bestPrefix
	}

	for _, m := range axumRouteRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 8 {
			continue
		}
		rawPath := content[m[2]:m[3]]
		verbLower := content[m[4]:m[5]]
		handler := content[m[6]:m[7]]

		verb := strings.ToUpper(verbLower)

		// Apply nest prefix if one is present.
		prefix := nestPrefixFor(m[0])
		if prefix != "" {
			// Compose: prefix + rawPath, normalising double slashes.
			rawPath = strings.TrimRight(prefix, "/") + "/" + strings.TrimLeft(rawPath, "/")
		}

		canonical := httproutes.Canonicalize(httproutes.FrameworkAxum, rawPath)
		// Use "Controller" as the handler kind — the resolver maps
		// Controller → SCOPE.Operation (the kind Rust functions land as),
		// so the http-endpoint-resolve pass can find the handler entity and
		// emit an IMPLEMENTS edge without dropping the synthetic. This is
		// the same convention used by synthesizeGoRouters (#722).
		emit(verb, canonical, "axum", "Controller", handler)
	}
}
