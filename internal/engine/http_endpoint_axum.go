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

	"github.com/cajasmota/grafel/internal/engine/httproutes"
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

// rustRouterCtorRe counts router CONSTRUCTIONS, which is how rustNestPrefixFor
// tells one router-building expression from the next.
//
// This was a literal `strings.Count(content, "Router::new()")` until #6560 B2a
// review. That spelling is not the inventory — it is one member of it — and the
// gap was not theoretical: `OpenApiRouter::with_openapi(ApiDoc::openapi())` is
// utoipa-axum's CANONICAL constructor (the `::new()` form is the one that omits
// the OpenAPI document), and it contains no `Router::new()`. Two independent
// routers built that way read as one scope, so a `.nest("/admin", …)` in the
// second reached back into the first and replaced a real `GET /health` with a
// phantom `GET /admin/health`. Same for `Router::<AppState>::new()`, where the
// turbofish splits the substring — that one had been missing from the axum pass
// since #1420.
//
// STATED INVENTORY, so the next reader does not have to rediscover it by
// accident. Matched:
//
//   - `Router::new()` / `OpenApiRouter::new()` and any `*Router` type,
//     including a local alias (`type ApiRouter = OpenApiRouter;`)
//   - `Router::default()` / `OpenApiRouter::default()` — both types implement
//     Default, and `::default()` is a legal empty-router constructor
//   - `OpenApiRouter::with_openapi(...)` — utoipa-axum only
//   - the turbofish form of all three, `Router::<S>::new()`
//
// Deliberately NOT matched, and each is a MISS in the permissive direction (two
// scopes may read as one, as the bug above did):
//
//   - a rename-import that erases the `Router` suffix
//     (`use ...::OpenApiRouter as Api;` then `Api::new()`) — nothing in the text
//     identifies `Api` as a router without resolving the import
//   - `OpenApiRouter::from(some_axum_router)` — real, but it CONVERTS an
//     existing router rather than starting a chain, so counting it would split
//     a scope that is genuinely one
//   - a constructor returned by a user helper (`fn base() -> OpenApiRouter`)
//
// Widening this set makes the guard STRICTER — more nests are rejected as
// out-of-scope — so every miss above costs recall of a prefix, never a phantom.
// That is the direction to fail in.
var rustRouterCtorRe = regexp.MustCompile(
	`\w*Router\b(?:::<[^>\n]*>)?::(?:new|default|with_openapi)\s*\(`)

// rustRouterCtorCount reports how many router constructions appear in src. It
// replaces a substring count; see rustRouterCtorRe for the inventory.
func rustRouterCtorCount(src string) int {
	return len(rustRouterCtorRe.FindAllStringIndex(src, -1))
}

// rustNestEntry records one `.nest("prefix", ...)` occurrence: the byte offset
// of the `.nest` token and the static prefix it mounts at.
type rustNestEntry struct {
	offset int
	prefix string
}

// rustNestEntries collects every `.nest("prefix", ...)` call in a Rust source
// file, in source order, with its byte offset.
//
// Extracted from synthesizeAxumRoutes (#6560 B2a) so the utoipa_axum pass can
// compose the same prefixes onto `routes!(...)` registrations. It is shared
// rather than duplicated because a second copy of the windowed heuristic below
// would drift from this one silently — the two passes read the SAME `.nest(`
// syntax out of the SAME language, and `OpenApiRouter::new().nest(...)` is the
// utoipa spelling of exactly the shape axumNestRe already matches.
func rustNestEntries(content string) []rustNestEntry {
	var nests []rustNestEntry
	for _, m := range axumNestRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 || m[2] < 0 {
			continue
		}
		nests = append(nests, rustNestEntry{offset: m[0], prefix: content[m[2]:m[3]]})
	}
	return nests
}

// rustNestWindow is how far, in bytes, a `.nest(...)` may sit from a route
// registration and still be taken to mount it.
//
// It is a heuristic bound, not a syntactic one: this pass does not parse Rust,
// so the window is what stands in for "the same router-building expression".
// Widening it lets an unrelated nest elsewhere in the file capture a route;
// narrowing it drops the prefix from the ordinary rustfmt-wrapped builder
// chain, which routinely runs several hundred bytes.
const rustNestWindow = 2000

// rustNestPrefixFor returns the best nest prefix that should be applied to a
// route registration at routeOffset, or "" when no nest governs it.
//
// Two strategies are tried, and the NEAREST qualifying nest by byte distance
// wins regardless of which one matched it:
//
//  1. Inline chain: a .nest("prefix", ...) that appears BEFORE the
//     registration within rustNestWindow bytes and where the text between them
//     contains at most one Router::new() — covers
//     `Router::new().nest("/api", Router::new().route(...))`.
//
//  2. Outer-function nest: the routes in a helper function
//     (e.g. `orders_router()`) are all written before the .nest() that mounts
//     them. We therefore also look for a .nest() that appears AFTER the
//     registration but within rustNestWindow bytes ahead, as long as no second
//     Router::new() separates them.
//
// TIE-BREAK, documented rather than pinned (#6560 B2a review): when two nests
// are EQUIDISTANT on opposite sides of the registration, the first one scanned
// wins, because the comparison is strict `<`. That is arbitrary — there is no
// reason a nest 300 bytes before is a better mount than one 300 bytes after —
// and the mutant `<=` (last wins) survives the suite with a real witness. It is
// left unpinned deliberately: a fixture would freeze an arbitrary choice as if
// it were a decision, and either answer is equally defensible. What IS pinned is
// that distance, not source order, decides when the distances DIFFER — see
// TestUtoipaAxum_NestPrefixNearestOfTwo.
//
// The "at most one router construction" clause is what keeps a nest from
// reaching across into a different router scope. Which spellings count is
// rustRouterCtorRe's business, and its inventory is stated there — a substring
// test for `Router::new()` looked like it covered the utoipa spellings and did
// not.
func rustNestPrefixFor(content string, nests []rustNestEntry, routeOffset int) string {
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
		if dist > rustNestWindow {
			continue
		}
		// Determine the text window between the two.
		start, end := n.offset, routeOffset
		if n.offset > routeOffset {
			start, end = routeOffset, n.offset
		}
		between := content[start:end]
		// Allow at most one router construction (the one that .nest() or
		// .route() is being called on). More than one means a different
		// router scope. See rustRouterCtorRe for which spellings count.
		if rustRouterCtorCount(between) > 1 {
			continue
		}
		if bestDist < 0 || dist < bestDist {
			bestDist = dist
			bestPrefix = n.prefix
		}
	}
	return bestPrefix
}

// rustJoinNestPrefix composes a nest prefix with a route path, normalising the
// slash between them. An empty prefix leaves the path untouched.
//
// Negative result, recorded so it is not re-litigated (#6560 B2a): the Trim
// calls are NOT observable by any test, and no fixture was added to try. Both
// callers pass the result to httproutes.Canonicalize, which ends in
// normaliseSlashes, so the mutant `prefix + "/" + rawPath` — which yields
// "/api//items" for the ordinary inputs — SURVIVED the whole package suite and
// is equivalent for every input either caller can produce. What is observable
// is dropping the separator altogether (`prefix + rawPath` → "/apiitems"), and
// that mutant is killed by the nest fixtures in both passes. The Trims are kept
// because they make the intent legible at the join, not because they change the
// canonical ID.
func rustJoinNestPrefix(prefix, rawPath string) string {
	if prefix == "" {
		return rawPath
	}
	return strings.TrimRight(prefix, "/") + "/" + strings.TrimLeft(rawPath, "/")
}

// synthesizeAxumRoutes scans a Rust source file for axum route registrations
// and emits one http_endpoint_definition per (verb, path) pair found.
//
// Single-level .nest("/prefix", ...) prefixes are composed onto each route by
// rustNestPrefixFor — see that function for the two strategies and for the
// rustNestWindow bound. Since #6560 B2a the same helper serves the utoipa_axum
// pass, so `.route(...)` and `routes!(...)` mount identically.
//
// The handler function name is passed as the refName so the synthesiser can
// stamp source_handler=Function:<name> on the entity.
func synthesizeAxumRoutes(content string, emit emitFn) {
	if !axumHasAxum(content) {
		return
	}

	// Collect nest prefixes with their byte positions so each .route() call
	// can be associated with the nearest nest prefix in the same
	// method-chain scope. See rustNestPrefixFor for the two strategies.
	nests := rustNestEntries(content)

	for _, m := range axumRouteRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 8 {
			continue
		}
		rawPath := content[m[2]:m[3]]
		verbLower := content[m[4]:m[5]]
		handler := content[m[6]:m[7]]

		verb := strings.ToUpper(verbLower)

		// Apply nest prefix if one is present.
		rawPath = rustJoinNestPrefix(rustNestPrefixFor(content, nests, m[0]), rawPath)

		canonical := httproutes.Canonicalize(httproutes.FrameworkAxum, rawPath)
		// Use "Controller" as the handler kind — the resolver maps
		// Controller → SCOPE.Operation (the kind Rust functions land as),
		// so the http-endpoint-resolve pass can find the handler entity and
		// emit an IMPLEMENTS edge without dropping the synthetic. This is
		// the same convention used by synthesizeGoRouters (#722).
		emit(verb, canonical, "axum", "Controller", handler)
	}
}
