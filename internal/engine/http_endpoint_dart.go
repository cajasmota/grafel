// http_endpoint_dart.go — Dart server-side route registration → canonical
// http_endpoint_definition synthesis (#4758, the Dart producer slice of the
// coverage-linkage tail epic #4749; mirrors the Crystal/Kemal, Swift/Vapor and
// Nim/Jester producer slices).
//
// Background
// ----------
// The Dart base extractor is structural-only and the existing Dart engine
// passes are CONSUMER-side (http_endpoint_dart_client.go — Dio / package:http
// outbound calls). No verb+path http_endpoint_definition was ever produced for
// the SERVER side, so Dart backends were invisible as producers in the endpoint
// graph and the shared e2e route-test linker
// (engine.linkE2ERouteTestsToEndpoints, #4351) had no target to bind a Dart
// route-hit test to. internal/substrate/entry_points_dart.go already classifies
// dart_frog `onRequest` and shelf `Response handler(Request …)` functions as
// reachability entry-points, but that signal produces no endpoint entity.
//
// This pass emits one canonical http_endpoint_definition per statically-known
// Dart route, in the SAME shape axum / Vapor / Kemal / Jester emit, so the
// existing resolver and the language-agnostic e2e route-test linker light up for
// Dart exactly as they do for the flagship stacks.
//
// Dart server route syntax
// ------------------------
// shelf_router (the dominant Dart router) registers routes with a verb method
// on a `Router()` taking a string-literal path and a handler, using ANGLE-
// bracket path params with an optional inline regex (`<id>`, `<id|[0-9]+>`):
//
//	final router = Router()
//	  ..get('/users/<id>', getUser)
//	  ..post('/users', createUser);
//	router.get('/health', (Request r) => Response.ok('ok'));   → GET /health
//	  → GET /users/{id}, POST /users
//
// dart_frog uses FILE-BASED routing: a file under `routes/` maps to a path
// derived from its location (`routes/users/[id]/index.dart` → `/users/{id}`),
// and exports an `onRequest(RequestContext context)` handler. The HTTP verb is
// derived from the handler's `switch (context.request.method)` /
// `context.request.method == HttpMethod.get` dispatch when statically present,
// else ANY (the handler accepts every verb).
//
//	// routes/users/[id]/index.dart
//	Response onRequest(RequestContext context) { ... }      → ANY /users/{id}
//
// conduit registers routes on a `Router` with `route("/users/[:id]")` (optional
// `[:id]`, required `:id`) linked to a `Controller`:
//
//	router.route("/users/[:id]").link(() => UserController());  → ANY /users/{id}
//
// Honest exclusions (no fabricated routes)
// ----------------------------------------
//   - Interpolated / variable paths (`router.get(pathVar, h)`, `'/x' + id`) —
//     not statically recoverable, dropped.
//   - A dart_frog route whose verb dispatch is built dynamically is emitted as
//     ANY (the handler is reachable under every verb — not a fabrication).
//   - conduit `@Operation`-annotated controller methods (verb-precise) are left
//     to a producer follow-up; the router-level route still yields an ANY
//     endpoint here so the path is visible.
package engine

import (
	"path/filepath"
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/engine/httproutes"
)

var (
	// shelfRouteRe matches a shelf_router verb registration on a Router:
	// `..get('/users/<id>', handler)` (cascade) or `router.post('/x', h)`
	// (method-call). The receiver/cascade operator (`..` or `<ident>.`) precedes
	// the `verb(` call. Capture group 1 is the verb; group 2 is the path literal
	// (single- or double-quoted, handled by the alternation).
	shelfRouteRe = regexp.MustCompile(
		`(?m)(?:\.\.|[A-Za-z_$][\w$]*\.)(get|post|put|delete|patch|options|head|all)\s*\(\s*(?:'([^'\n\r]*)'|"([^"\n\r]*)")`)

	// conduitRouteRe matches a Conduit router route registration:
	// `router.route("/users/[:id]")`. Capture group 1 is the path literal.
	conduitRouteRe = regexp.MustCompile(
		`(?m)\.route\s*\(\s*(?:"([^"\n\r]*)"|'([^'\n\r]*)')`)

	// dartFrogVerbRe matches a dart_frog handler's static verb dispatch so the
	// file-route endpoint can carry a precise verb instead of ANY. It recognises
	// both `case HttpMethod.get` (switch dispatch) and
	// `context.request.method == HttpMethod.post` (equality dispatch). Capture
	// group 1 / group 2 is the verb tail (`get`/`post`/…).
	dartFrogVerbRe = regexp.MustCompile(
		`HttpMethod\.(get|post|put|delete|patch|head|options)\b`)
)

// shelfHasRoute is a fast pre-filter: the file must reference a shelf_router
// marker (`Router(` or `shelf_router`) to be worth scanning.
func shelfHasRoute(content string) bool {
	return strings.Contains(content, "Router(") ||
		strings.Contains(content, "shelf_router") ||
		strings.Contains(content, "shelf/shelf")
}

// conduitHasRoute is a fast pre-filter for Conduit route files.
func conduitHasRoute(content string) bool {
	return strings.Contains(content, ".route(") &&
		(strings.Contains(content, "conduit") ||
			strings.Contains(content, "Controller") ||
			strings.Contains(content, ".link("))
}

// synthesizeShelfRoutes scans a Dart source file for shelf_router verb
// registrations and emits one http_endpoint_definition per statically-known
// (verb, path). A `.all(` registration is emitted as ANY.
func synthesizeShelfRoutes(content string, emit emitFn) {
	if !shelfHasRoute(content) {
		return
	}
	for _, m := range shelfRouteRe.FindAllStringSubmatch(content, -1) {
		if len(m) < 4 {
			continue
		}
		verb := strings.ToUpper(m[1])
		if verb == "ALL" {
			verb = "ANY"
		}
		raw := m[2]
		if raw == "" {
			raw = m[3]
		}
		raw = strings.TrimSpace(raw)
		if !staticDartPath(raw) {
			continue
		}
		if !strings.HasPrefix(raw, "/") {
			raw = "/" + raw
		}
		canonical := httproutes.Canonicalize(httproutes.FrameworkShelf, raw)
		if canonical == "" {
			continue
		}
		emit(verb, canonical, "shelf_router", "Controller", "")
	}
}

// synthesizeConduitRoutes scans a Dart source file for Conduit router route
// registrations and emits one ANY http_endpoint_definition per statically-known
// path (verb-precise `@Operation` methods are a follow-up).
func synthesizeConduitRoutes(content string, emit emitFn) {
	if !conduitHasRoute(content) {
		return
	}
	for _, m := range conduitRouteRe.FindAllStringSubmatch(content, -1) {
		if len(m) < 3 {
			continue
		}
		raw := m[1]
		if raw == "" {
			raw = m[2]
		}
		raw = strings.TrimSpace(raw)
		if !staticDartPath(raw) {
			continue
		}
		if !strings.HasPrefix(raw, "/") {
			raw = "/" + raw
		}
		canonical := httproutes.Canonicalize(httproutes.FrameworkConduit, raw)
		if canonical == "" {
			continue
		}
		emit("ANY", canonical, "conduit", "Controller", "")
	}
}

// synthesizeDartFrogRoutes derives a dart_frog file-based route from the source
// file's location under a `routes/` directory and emits one
// http_endpoint_definition for it, but only when the file actually exports an
// `onRequest` handler (so non-route Dart files under a `routes/` path are not
// fabricated into endpoints). The verb is read from the handler's static method
// dispatch when present, else ANY.
func synthesizeDartFrogRoutes(content, filePath string, emit emitFn) {
	if !strings.Contains(content, "onRequest") {
		return
	}
	route, ok := dartFrogRouteFromPath(filePath)
	if !ok {
		return
	}
	canonical := httproutes.Canonicalize(httproutes.FrameworkDartFrog, route)
	if canonical == "" {
		return
	}
	verbs := dartFrogVerbs(content)
	if len(verbs) == 0 {
		emit("ANY", canonical, "dart_frog", "Controller", "")
		return
	}
	for _, v := range verbs {
		emit(v, canonical, "dart_frog", "Controller", "")
	}
}

// dartFrogVerbs returns the distinct uppercased HTTP verbs a dart_frog handler
// dispatches on statically (`HttpMethod.get` / `case HttpMethod.post`). Returns
// nil when the handler does not branch on the method (→ ANY).
func dartFrogVerbs(content string) []string {
	var out []string
	seen := map[string]bool{}
	for _, m := range dartFrogVerbRe.FindAllStringSubmatch(content, -1) {
		v := strings.ToUpper(m[1])
		if seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	return out
}

// dartFrogRouteFromPath converts a dart_frog route file path into its canonical
// route path. The path under the `routes/` directory maps directly: each
// directory/file segment is a path segment, `index.dart` collapses to its
// parent directory, a `[name].dart` / `[name]` segment is a dynamic param
// (rewritten to `{name}`), and a `[...rest]` catch-all becomes `{rest}`. Returns
// (route, false) when the file is not under a `routes/` directory.
func dartFrogRouteFromPath(filePath string) (string, bool) {
	p := filepath.ToSlash(filePath)
	idx := strings.LastIndex(p, "/routes/")
	var rel string
	switch {
	case idx >= 0:
		rel = p[idx+len("/routes/"):]
	case strings.HasPrefix(p, "routes/"):
		rel = p[len("routes/"):]
	default:
		return "", false
	}
	rel = strings.TrimSuffix(rel, ".dart")
	segs := strings.Split(rel, "/")
	var out []string
	for _, s := range segs {
		if s == "" || s == "index" {
			continue
		}
		out = append(out, dartFrogSegment(s))
	}
	if len(out) == 0 {
		return "/", true
	}
	return "/" + strings.Join(out, "/"), true
}

// dartFrogSegment converts a single dart_frog path segment to canonical form:
// `[id]` → `{id}`, `[...rest]` (catch-all) → `{rest}`, a literal segment passes
// through. Only a fully-bracketed dynamic segment is treated as a param.
func dartFrogSegment(seg string) string {
	if strings.HasPrefix(seg, "[") && strings.HasSuffix(seg, "]") {
		name := strings.TrimSuffix(strings.TrimPrefix(seg, "["), "]")
		name = strings.TrimPrefix(name, "...") // catch-all `[...rest]`
		name = strings.TrimSpace(name)
		if name == "" {
			return "{}"
		}
		return "{" + name + "}"
	}
	return seg
}

// staticDartPath reports whether a captured Dart route literal is a
// statically-recoverable path. The capture regexes already pull the path from a
// SINGLE quoted literal, so cross-literal `+`/`&` concatenation can never appear
// inside `raw`; the only non-recoverable construct that survives inside one
// literal is `$`/`${…}` string interpolation (a `+` here is a legitimate regex
// quantifier in a shelf `<id|[0-9]+>` constraint, not concatenation). Reject an
// interpolated literal; accept everything else.
func staticDartPath(raw string) bool {
	if raw == "" {
		return false
	}
	if strings.Contains(raw, "$") {
		return false
	}
	return true
}
