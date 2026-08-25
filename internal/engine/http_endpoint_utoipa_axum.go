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
// A file may register the same handler through MORE THAN ONE producer — a
// `.route("/x", get(h))` call read by synthesizeAxumRoutes, or a Rocket verb
// attribute `#[get("/x")]` read by synthesizeRocket, either of them alongside a
// `routes!(h)`. (`#[utoipa::path]` beside Rocket attribute macros is a shipped
// utoipa combination, not a hypothetical.) Minting from two producers for one
// handler is the duplicate-producer hazard of #6530, so this pass dedupes on the
// HANDLER NAME against EVERY other Rust producer that reads this same file:
// utoipaRegisteredElsewhere collects the handler arguments of axumRouteRe AND
// the decorated functions of rocketRouteAttrRe, and any handler in that set is
// skipped here entirely.
//
// The registration in code is the authoritative mount point — it is what the
// router actually serves — while the attribute path is documentation. Where the
// two disagree, minting the attribute path as well would invent a second
// endpoint for one handler.
//
// This dedupe is FILE-SCOPED, and so is the emit-level `dedupKey` it complements
// (http_endpoint_synthesis.go). A handler whose attribute and `routes!` mount
// live in one file while a `.route(...)` for it lives in ANOTHER file is not
// deduped by either mechanism and will mint twice, once per path. That needs a
// genuine double mount to occur and is not addressed here; no test observes it.
//
// Ordering note: this pass runs after synthesizeAxumRoutes but BEFORE
// synthesizeRocket. Where two producers' IDs agree, `dedupKey` keeps the FIRST
// emit, so without the Rocket half of the dedupe key a Rocket endpoint would not
// merely be duplicated — it would be relabelled framework=utoipa_axum. The
// Rocket guard tests assert the framework stamp for exactly that reason.
//
// Scope — Arms A and B1 only (#6560)
// ----------------------------------
// Arm B1 added the multi-handler form `routes!(a, b, c)`: every argument is
// resolved against the same-file attribute map exactly as the single-handler
// case already was, and the macro is skipped WHOLE if any argument is not a bare
// identifier. Everything stays file-scoped, so the dedupe below is untouched.
//
// Deliberately NOT handled here; these are Arm B2 and are left to emit nothing
// rather than to emit a guess:
//
//   - cross-module handlers (`routes!(crate::items::list)`, or a bare name whose
//     `#[utoipa::path]` attribute lives in another file) — the attribute map is
//     built from THIS file only, and an unknown handler is skipped. A fabricated
//     endpoint is worse than a missing one.
//   - `OpenApiRouter::nest("/prefix", ...)` prefix composition — the axum pass's
//     nest handling is not threaded onto these routes, so a nested utoipa router
//     yields the unprefixed attribute path.
//
// Known house behaviour, not new here: a `routes!(...)` occurrence inside a
// block comment or a string literal is still read as a registration, exactly as
// synthesizeAxumRoutes reads a commented-out `.route(`. A COMMENTED-OUT
// `#[utoipa::path]` attribute is a different matter and IS rejected — see
// utoipaHandlerRoutes — because binding it to the next function stamps a route
// on a function that never declared one.
package engine

import (
	"regexp"
	"strings"

	"github.com/cajasmota/grafel/internal/engine/httproutes"
)

// utoipaRoutesMacroRe matches a `routes!(a)` / `routes!(a, b, c)` registration
// whose arguments are ALL plain identifiers (#6560 Arm B1 widened this from the
// single-identifier form Arm A shipped).
//
// Capture group 1 = the whole argument list, split by utoipaMacroHandlerArgs.
//
// Two properties of this pattern carry the Arm B2 boundary and are the reason it
// is written as one alternation rather than as a loose `([^)]*)` capture:
//
//   - The argument list admits ONLY `[A-Za-z_]\w*` separated by commas, so a
//     path-qualified argument (`crate::items::list`) makes the WHOLE macro fail
//     to match. Such a handler's `#[utoipa::path]` attribute lives in another
//     module, and this pass's attribute map is file-scoped, so it could not be
//     resolved anyway — and minting only the macro's plain arguments would be a
//     silent half-registration, the exact failure the Arm A pin forbade.
//   - The trailing `\)` is a required anchor. Without it the capture runs past
//     the macro it belongs to and registers identifiers that are not arguments
//     at all.
//
// Neither `(` nor `)` is in any character class here, so a match can never span
// from one `routes!(...)` into the next; `\s` spans newlines so the rustfmt
// multi-line form is read normally. A trailing comma is Rust-legal and accepted.
var utoipaRoutesMacroRe = regexp.MustCompile(
	`\broutes!\s*\(\s*([A-Za-z_]\w*(?:\s*,\s*[A-Za-z_]\w*)*)\s*,?\s*\)`)

// utoipaMacroHandlerArgs splits capture group 1 of utoipaRoutesMacroRe into its
// handler identifiers, in source order. The regex has already guaranteed every
// element is a bare identifier, so this only has to trim.
func utoipaMacroHandlerArgs(argList string) []string {
	parts := strings.Split(argList, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if h := strings.TrimSpace(p); h != "" {
			out = append(out, h)
		}
	}
	return out
}

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

// utoipaAttrDecoratedFnRe matches the `fn <name>` that an attribute DECORATES,
// anchored at the end of that attribute so nothing but the rest of the
// attribute's own `]`, whitespace, further attributes, comments and the usual
// `pub` / `async` qualifiers may stand between them.
//
// The anchor is the point of this regex. An unbounded search for the next `fn`
// binds an attribute to whatever function happens to follow it — a
// `#[utoipa::path]` on a struct, or one that was commented out, would stamp its
// route onto an unrelated handler, which is the worst shape of false positive
// this pass could produce.
var utoipaAttrDecoratedFnRe = regexp.MustCompile(
	`^\]?\s*(?:(?://[^\n]*|#\[[^\]]*\])\s*)*(?:pub(?:\s*\([^)]*\))?\s+)?(?:async\s+)?fn\s+([A-Za-z_]\w*)`)

// utoipaHasRoutesMacro is a fast pre-filter: the file must contain a `routes!`
// macro invocation.
//
// It deliberately does NOT also test for the substring "utoipa". That clause was
// redundant — the attribute map below is what decides whether anything is minted
// — and worse, pairing it with a comment claiming it excluded Rocket documented
// a guard that did not hold: a file using Rocket AND utoipa passes it. Rocket
// handlers are excluded properly, by the dedupe key, not by a substring test.
func utoipaHasRoutesMacro(content string) bool {
	return strings.Contains(content, "routes!")
}

// utoipaAttrIsLineCommented reports whether the attribute starting at attrOff is
// inside a `//` line comment. A commented-out attribute decorates nothing.
func utoipaAttrIsLineCommented(content string, attrOff int) bool {
	lineStart := strings.LastIndexByte(content[:attrOff], '\n') + 1
	return strings.Contains(content[lineStart:attrOff], "//")
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
		if utoipaAttrIsLineCommented(content, m[0]) {
			continue
		}
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
		fm := utoipaAttrDecoratedFnRe.FindStringSubmatch(content[endOff:])
		if fm == nil {
			// The attribute does not decorate a function (e.g. it sits on a
			// struct, or is detached from the next `fn` by other code).
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

// utoipaRegisteredElsewhere returns the set of handler names that ANOTHER Rust
// producer already mints an http_endpoint_definition for out of this same file:
//
//   - synthesizeAxumRoutes, from `.route("path", verb(handler))` (axumRouteRe
//     capture 3);
//   - synthesizeRocket, from a verb attribute macro `#[get("/path")]` on the
//     function (rocketRouteAttrRe capture 3).
//
// This is the dedupe key. It is deliberately expressed against the OTHER passes'
// own regexes rather than a re-derived approximation, so a handler this pass
// skips is exactly a handler one of them registers.
func utoipaRegisteredElsewhere(content string) map[string]bool {
	out := map[string]bool{}
	for _, m := range axumRouteRe.FindAllStringSubmatch(content, -1) {
		if len(m) < 4 {
			continue
		}
		out[m[3]] = true
	}
	for _, m := range rocketRouteAttrRe.FindAllStringSubmatch(content, -1) {
		if len(m) < 4 {
			continue
		}
		out[m[3]] = true
	}
	return out
}

// synthesizeUtoipaAxumRoutes scans a Rust source file for `routes!(...)`
// registrations — one handler or several — and emits one
// http_endpoint_definition per handler whose `#[utoipa::path(...)]` attribute is
// in the SAME file. A handler named more than once, in one macro or across
// several, still mints exactly once.
//
// Handlers that another producer already registers out of this file — a
// `.route(...)` call or a Rocket verb attribute — are skipped; see
// utoipaRegisteredElsewhere and the deduplication note in the file header.
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

	registeredElsewhere := utoipaRegisteredElsewhere(content)

	minted := map[string]bool{}
	for _, m := range utoipaRoutesMacroRe.FindAllStringSubmatch(content, -1) {
		if len(m) < 2 {
			continue
		}
		// One macro may register several handlers. Each argument is resolved
		// independently — a sibling that another producer already owns, or that
		// has no same-file contract, is skipped WITHOUT suppressing the rest.
		for _, handler := range utoipaMacroHandlerArgs(m[1]) {
			if registeredElsewhere[handler] || minted[handler] {
				continue
			}
			route, known := attrs[handler]
			if !known {
				// No same-file contract for this handler — Arm B2 territory.
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
}
