// http_endpoint_haskell.go — Haskell web route registration → canonical
// http_endpoint_definition synthesis (#5373, the Haskell slice of the
// low-ranked-language bootstrap epic #5360).
//
// Background
// ----------
// The Haskell base extractor (internal/extractors/haskell/extractor.go) is a
// regex/layout-heuristic structural extractor: it mines modules, type
// signatures + definitions (SCOPE.Operation), data/newtype/class/instance
// SCOPE.Component entities and IMPORTS/CALLS/CONTAINS edges, but has NO
// web-framework awareness — Scotty `get "/users/:id"` verb routes, Yesod
// `parseRoutes` quasiquote route tables and Servant type-level API DSLs are not
// recognised as HTTP endpoints, so no `http_endpoint_definition` entity is ever
// produced for Haskell. The shared endpoint resolver
// (ResolveHTTPEndpointHandlers) and the language-agnostic e2e route-test linker
// (linkE2ERouteTestsToEndpoints, #4351) both key off
// `http_endpoint_definition` + `path`, so a Haskell route could never surface.
//
// This pass closes the PRODUCER-side gap: it emits one canonical
// http_endpoint_definition per statically-known Haskell route, in the SAME
// shape the axum / Vapor / Kemal / Clojure synthesizers emit.
//
// Haskell web route syntax
// ------------------------
//
//   - Scotty (cheapest first; Sinatra-like): a top-level verb function takes a
//     string-literal path pattern and a handler `do` block:
//
//     get  "/users/:id" $ do ...        → GET  /users/{id}
//     post "/users"     $ do ...        → POST /users
//     delete "/users/:id" handler       → DELETE /users/{id}
//
//     Scotty uses the Express-style `:name` colon path-parameter convention.
//
//   - Yesod: routes live in a `parseRoutes` quasiquote (usually inside
//     `mkYesod "App" [parseRoutes| ... |]`). Each line is
//     `/path/segments  ResourceR  GET POST ...`:
//
//     /users          UsersR    GET POST     → GET /users, POST /users
//     /user/#UserId   UserR     GET          → GET /user/{userid}
//     /static/*Texts  StaticR   GET          → GET /static/{texts}
//
//     A `#Type` segment is a typed single-segment capture; a `*Type` segment is
//     a typed multi-segment capture. Both are normalised to the Express-style
//     `:name` form (lower-cased type name) before canonicalisation.
//
//   - Servant: the API is a TYPE — a chain of `:>`-combined components ending in
//     a verb combinator. String-literal segments are path literals;
//     `Capture "name" Type` segments are path params; `:<|>` alternates
//     sub-APIs:
//
//     type API = "users" :> Capture "id" Int :> Get '[JSON] User
//     :<|> "users" :> ReqBody '[JSON] User :> Post '[JSON] User
//     → GET /users/{id} , POST /users
//
// Honest exclusions (no fabricated routes)
// -----------------------------------------
//   - Scotty: interpolated / variable / non-literal paths (`get path $ ...`,
//     `get (mconcat [...]) ...`) — the path must be a STRING LITERAL.
//   - Yesod: type-safe URL interpolation (`@{UserR uid}`) is a CONSUMER concern,
//     not a route declaration, and is not emitted. Subsite mounts
//     (`/admin AdminR Admin getAdmin`) and attribute/`!` route options are not
//     threaded. The synthesizer reads the literal `parseRoutes` block only.
//   - Servant: the verb is read from the LEAF combinator on a `:>` chain; HList
//     type-family composition, named sub-APIs spread across `type` aliases
//     (`type API = UserAPI :<|> ItemAPI`) and `:<|>` whose operands are bare
//     type names (not inline `:>` chains) are NOT resolved across declarations
//     — a documented partial. Each inline `:>` chain that ends in a verb is
//     emitted.
package engine

import (
	"regexp"
	"strings"

	"github.com/cajasmota/grafel/internal/engine/httproutes"
)

// ---------------------------------------------------------------------------
// Scotty
// ---------------------------------------------------------------------------

// hsScottyRouteRe matches a Scotty verb function call with a leading
// string-literal path:
//
//	get    "/users/:id" $ do ...
//	post   "/users"     handler
//	delete "/users/:id" $ ...
//
// The verb must appear at a statement boundary (start of line after optional
// indentation) so an arbitrary function named `get`/`post` invoked with a `.`
// receiver is not misread. Capture group 1 is the verb; group 2 is the path
// literal.
var hsScottyRouteRe = regexp.MustCompile(
	`(?m)^[ \t]*(get|post|put|delete|patch|options|head)\s+"([^"\n\r]*)"`,
)

// hsScottyHasRoute is a fast pre-filter: the file must reference a Scotty marker
// AND a verb call with a leading string literal to be worth scanning.
func hsScottyHasRoute(content string) bool {
	if !strings.Contains(content, "Web.Scotty") && !strings.Contains(content, "scotty") {
		return false
	}
	return strings.Contains(content, `get "`) ||
		strings.Contains(content, `post "`) ||
		strings.Contains(content, `put "`) ||
		strings.Contains(content, `delete "`) ||
		strings.Contains(content, `patch "`) ||
		strings.Contains(content, `options "`) ||
		strings.Contains(content, `head "`)
}

// synthesizeScottyRoutes scans a Haskell source file for Scotty verb routes and
// emits one http_endpoint_definition per statically-known (verb, path).
func synthesizeScottyRoutes(content string, emit emitFn) {
	if !hsScottyHasRoute(content) {
		return
	}
	for _, m := range hsScottyRouteRe.FindAllStringSubmatch(content, -1) {
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
		canonical := httproutes.Canonicalize(httproutes.FrameworkScotty, raw)
		if canonical == "" {
			continue
		}
		emit(strings.ToUpper(m[1]), canonical, "scotty", "Controller", "")
	}
}

// ---------------------------------------------------------------------------
// Yesod
// ---------------------------------------------------------------------------

// hsYesodBlockRe captures the body of a `parseRoutes` quasiquote:
//
//	[parseRoutes|
//	  /users        UsersR   GET POST
//	  /user/#UserId UserR    GET
//	|]
//
// Capture group 1 is the block body between the opening `|` and the closing `|]`.
var hsYesodBlockRe = regexp.MustCompile(`(?s)parseRoutes\|(.*?)\|\]`)

// hsYesodLineRe matches one route line inside a parseRoutes block:
//
//	/user/#UserId UserR GET POST
//
// Capture group 1 is the path; group 2 is the trailing remainder (resource name
// + verbs / handler refs). A line with no uppercase verb token is a subsite /
// resource-only mount and is skipped by the verb scan.
var hsYesodLineRe = regexp.MustCompile(`(?m)^\s*(/\S*)\s+(.*)$`)

// hsYesodVerbRe extracts an HTTP-verb token from the trailing remainder of a
// Yesod route line. Verbs are bare uppercase words (GET POST PUT DELETE PATCH).
var hsYesodVerbRe = regexp.MustCompile(`\b(GET|POST|PUT|DELETE|PATCH|HEAD|OPTIONS)\b`)

// hsYesodCaptureRe rewrites a Yesod typed capture segment `#Type` (single) or
// `*Type` / `+Type` (multi) into the Express-style `:type` form so the shared
// colon canonicaliser folds it to `{type}`.
var hsYesodCaptureRe = regexp.MustCompile(`[#*+]([A-Za-z][A-Za-z0-9_']*)`)

// synthesizeYesodRoutes scans a Haskell source file for a Yesod `parseRoutes`
// quasiquote and emits one http_endpoint_definition per (verb, path).
func synthesizeYesodRoutes(content string, emit emitFn) {
	if !strings.Contains(content, "parseRoutes") {
		return
	}
	for _, block := range hsYesodBlockRe.FindAllStringSubmatch(content, -1) {
		if len(block) < 2 {
			continue
		}
		for _, ln := range hsYesodLineRe.FindAllStringSubmatch(block[1], -1) {
			if len(ln) < 3 {
				continue
			}
			rawPath := ln[1]
			// Normalise `#Type` / `*Type` captures to `:type`.
			rawPath = hsYesodCaptureRe.ReplaceAllStringFunc(rawPath, func(seg string) string {
				m := hsYesodCaptureRe.FindStringSubmatch(seg)
				return ":" + strings.ToLower(m[1])
			})
			canonical := httproutes.Canonicalize(httproutes.FrameworkYesod, rawPath)
			if canonical == "" {
				continue
			}
			verbs := hsYesodVerbRe.FindAllStringSubmatch(ln[2], -1)
			seen := map[string]bool{}
			for _, vm := range verbs {
				verb := vm[1]
				if seen[verb] {
					continue
				}
				seen[verb] = true
				emit(verb, canonical, "yesod", "Controller", "")
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Servant
// ---------------------------------------------------------------------------

// hsServantVerbRe matches a Servant leaf verb combinator. Servant verb
// combinators are `Get`/`Post`/`Put`/`Delete`/`Patch` (capitalised), optionally
// preceded by a content-type / status modifier in the same `:>` chain.
var hsServantVerbRe = regexp.MustCompile(`\b(Get|Post|Put|Delete|Patch)\b`)

// hsServantLiteralRe matches a Servant string-literal path segment
// (`"users"`) — a path component in the type-level DSL.
var hsServantLiteralRe = regexp.MustCompile(`"([^"\n\r]*)"`)

// hsServantCaptureRe matches a Servant `Capture "name" Type` /
// `CaptureAll "name" Type` path-parameter combinator. Capture group 1 is the
// parameter name.
var hsServantCaptureRe = regexp.MustCompile(`Capture(?:All)?\s+"([^"\n\r]+)"`)

// hsServantHasAPI is a fast pre-filter: the file must reference Servant and a
// verb combinator with a `:>` chain to be worth scanning.
func hsServantHasAPI(content string) bool {
	if !strings.Contains(content, "Servant") && !strings.Contains(content, "servant") {
		return false
	}
	return strings.Contains(content, ":>")
}

// synthesizeServantRoutes scans a Haskell source for Servant type-level API
// declarations and emits one http_endpoint_definition per inline `:>` chain
// that terminates in a verb combinator. Each `:<|>`-separated operand is a
// candidate chain; only operands that are themselves inline `:>` chains ending
// in a verb are emitted (bare type-name operands are an honest exclusion).
func synthesizeServantRoutes(content string, emit emitFn) {
	if !hsServantHasAPI(content) {
		return
	}
	// Each `:<|>`-separated alternative is a route candidate. Splitting on the
	// alternation operator isolates one inline `:>` chain per route.
	for _, alt := range strings.Split(content, ":<|>") {
		// A route chain must contain at least one `:>` combinator and a verb.
		if !strings.Contains(alt, ":>") {
			continue
		}
		// The first operand carries the file's leading content + `type Name =`
		// before the first segment; keep only the text from the last `=` that
		// introduces the API type so the leading `:>` component is the first
		// path segment (`"users"`) rather than imports/pragmas. Subsequent
		// `:<|>` operands have no `=` and are unaffected.
		if eq := strings.LastIndex(alt, "="); eq >= 0 && strings.Contains(alt[eq:], ":>") {
			alt = alt[eq+1:]
		}
		verbMatch := hsServantVerbRe.FindString(alt)
		if verbMatch == "" {
			continue
		}
		// Walk the `:>`-separated components in order, building the path from
		// string-literal segments and `Capture "name" _` params. Stop at the
		// verb combinator (the leaf).
		var segs []string
		for _, comp := range strings.Split(alt, ":>") {
			comp = strings.TrimSpace(comp)
			if hsServantVerbRe.MatchString(comp) {
				break
			}
			if cm := hsServantCaptureRe.FindStringSubmatch(comp); cm != nil {
				segs = append(segs, "{"+cm[1]+"}")
				continue
			}
			// A bare string-literal component is a path segment. Guard against
			// non-path combinators that happen to carry a string (rare) by
			// requiring the component to be ONLY a quoted literal.
			if lm := hsServantLiteralRe.FindStringSubmatch(comp); lm != nil &&
				strings.HasPrefix(comp, `"`) {
				if lm[1] != "" {
					segs = append(segs, lm[1])
				}
			}
		}
		path := "/" + strings.Join(segs, "/")
		canonical := httproutes.Canonicalize(httproutes.FrameworkServant, path)
		if canonical == "" {
			continue
		}
		emit(strings.ToUpper(verbMatch), canonical, "servant", "Controller", "")
	}
}
