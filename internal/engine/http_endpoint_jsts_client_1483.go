// NestJS HttpService (RxJS) + Apollo Client URI consumer-side extraction.
//
// Issue #1483 — three missing HTTP consumer idioms.
//
// Idiom 1 — NestJS HttpService (RxJS):
//
//	NestJS injects @nestjs/axios HttpService into services. Calls take the form
//	  this.httpService.get("http://orders:3000/orders")
//	  this.httpService.post("http://catalog:3001/products", body)
//	returning RxJS Observables. The existing axiosClientRe / axiosInstanceCallRe
//	patterns do NOT fire because:
//	  - The receiver is `this.httpService` (member expression, not bare identifier).
//	  - `httpService` ends in "Service", not "Client" / "HttpClient" / "apiClient".
//	We handle this with a dedicated regex that anchors on the `this.httpService.`
//	prefix followed by an HTTP verb.
//
// Idiom 2 — Apollo Client URI:
//
//	Client services create an ApolloClient pointing at a downstream GraphQL
//	service:
//	  new ApolloClient({ uri: "http://search-graphql:4000/graphql" })
//	  new ApolloClient({ uri: process.env.GQL_URL || "http://search-graphql:4000/graphql" })
//	We extract the static literal URI and emit a GRAPHQL call to that endpoint
//	(same path shape as the producer side, which emits http:GRAPHQL:/graphql/...
//	paths via synthesizeGraphQLResolvers). The cross-repo linker matches on the
//	canonical path so admin→search-graphql links resolve.
//
// Both synthesizers are called from synthesizeFetchAxiosWithRuntime (the JS/TS
// consumer-side entry point) via the existing emitClientRuntime closure, so
// FETCHES edges and http_endpoint_call kinds are emitted automatically.
//
// Refs #1483.
package engine

import (
	"regexp"
	"strings"

	"github.com/cajasmota/grafel/internal/engine/httproutes"
	"github.com/cajasmota/grafel/internal/extractor"
)

// ---------------------------------------------------------------------------
// Idiom 1 — NestJS HttpService (RxJS)
// ---------------------------------------------------------------------------

// nestHttpServiceCallRe matches `this.httpService.<verb>(url, ...)` calls.
// Capture groups:
//
//	1 = HTTP verb (get|post|put|patch|delete|head|options)
//	2 = URL string literal (single/double quotes) — OR empty if backtick
//	3 = URL template-literal body (backtick) — OR empty if string literal
//
// The leading `(?:^|[^\w$.])` boundary prevents matching in the middle of
// longer chains. `this.httpService` is the idiomatic NestJS injection name;
// some projects alias it as `this.http` — we match either.
//
// Two separate regexes handle static vs template-literal URL args to avoid
// the regexp alternation ambiguity with backtick quoting.
//
// #6433 — `httpClient` joins the receiver alternation. Angular services name
// the injected field `http`, `httpClient`, or (rarely) `httpService`; all three
// are gated by hasInjectedHTTPClientToken below.
const nestHTTPReceiverAlternation = `(?:httpService|httpClient|http)`

// jsGenericArgs matches an optional TypeScript generic argument list with ONE
// level of nesting: <Thing>, <readonly Thing[]>, <Array<Thing>>,
// <Record<string, Thing>>, <HttpResponse<Thing>>.
//
// #6446 — the original group was `<[^<>()]*>`, which excluded < and >, so a
// nested generic matched NOTHING: not the static pass, not the template pass,
// not even the dynamic marker. Array<T> / HttpResponse<T> / Record<string,T> are
// routine Angular response types.
//
// ( and ) stay excluded at every level so a call expression can never be
// mistaken for a type argument. Two levels of nesting
// (`Array<Record<string, T>>`) still do not match — RE2 has no recursion, and
// each extra level is another hand-written alternation. Recorded, not fixed.
const jsGenericArgs = `(?:<[^()<>]*(?:<[^()<>]*>[^()<>]*)*>)?`

var nestHttpServiceStaticRe = regexp.MustCompile(
	`(?:^|[^\w$])\bthis\s*\.\s*` + nestHTTPReceiverAlternation + `\s*\.\s*` +
		`(get|post|put|patch|delete|head|options)\s*` +
		jsGenericArgs + `\s*\(\s*` +
		`['"]((?:https?://|/)[^'"\n\r$]+)['"]`,
)

var nestHttpServiceTemplateLiteralRe = regexp.MustCompile(
	"(?:^|[^\\w$])\\bthis\\s*\\.\\s*" + nestHTTPReceiverAlternation + "\\s*\\.\\s*" +
		"(get|post|put|patch|delete|head|options)\\s*" +
		jsGenericArgs + "\\s*\\(\\s*" +
		"`([^`\\n\\r]*\\$\\{[^`\\n\\r]*)`",
)

// hasInjectedHTTPClientToken is the gate for the receiver-style (`this.<field>
// .<verb>(...)`) HTTP consumer passes.
//
// #6433 — it used to demand the literal token `httpService` / `HttpService`,
// which is the NestJS @nestjs/axios class name. Idiomatic Angular
//
//	import { HttpClient } from '@angular/common/http';
//	private http = inject(HttpClient);
//
// contains neither token, so the pass returned before its regex ever ran and
// every Angular codebase extracted zero client-side call sites.
//
// #6446 — the first replacement was `strings.Contains(content, "HttpClient")`,
// defended as "it is a class name, so it only appears where the client is
// imported, injected or type-annotated". That is factually false: a MENTION
// opens a Contains gate. A migration TODO in a comment, an `import type`, or a
// doc string each produced a bogus call site from a plain-object member named
// `http`. The gate now asks for real evidence — a DI call, a type annotation, a
// VALUE import, or the NestJS `this.httpService.` receiver — over source with
// comments and string literals blanked. It still deliberately does NOT key on
// the receiver field name (`this.http`): `http` is far too common a member name
// to be evidence on its own.
func hasInjectedHTTPClientToken(content string) bool {
	return extractor.HasInjectedHTTPClientEvidence(content)
}

// synthesizeNestHttpService extracts consumer-side HTTP calls made via the
// NestJS @nestjs/axios HttpService. Emits one synthetic per call site with
// framework="nestjs_http_service". The URL is host-stripped so the canonical
// path matches the producer-side entity regardless of internal service names.
func synthesizeNestHttpService(content string, funcs []jsFuncSpan, syms map[string]string, emit emitFn) {
	if !hasInjectedHTTPClientToken(content) {
		return
	}

	// Static string literal URLs: this.httpService.get("http://service/path")
	for _, m := range nestHttpServiceStaticRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 6 {
			continue
		}
		verb := strings.ToUpper(content[m[2]:m[3]])
		raw := content[m[4]:m[5]]
		path, ok := normalizeRawClientPath(raw)
		if !ok || path == "" {
			continue
		}
		caller := enclosingJSFuncAt(funcs, m[0])
		canonical := httproutes.Canonicalize(httproutes.FrameworkExpress, path)
		emit(verb, canonical, "nestjs_http_service", "Function", caller)
	}

	// Template-literal URLs: this.httpService.get(`http://service/orders/${id}`)
	for _, m := range nestHttpServiceTemplateLiteralRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 6 {
			continue
		}
		verb := strings.ToUpper(content[m[2]:m[3]])
		tmpl := content[m[4]:m[5]]
		path, ok := canonicalizeTemplateLiteral(tmpl, syms)
		if !ok || path == "" {
			continue
		}
		caller := enclosingJSFuncAt(funcs, m[0])
		canonical := httproutes.Canonicalize(httproutes.FrameworkExpress, path)
		emit(verb, canonical, "nestjs_http_service", "Function", caller)
	}
}

// ---------------------------------------------------------------------------
// #6433 — receiver-style calls the static / template passes left behind
// ---------------------------------------------------------------------------

// dynamicClientPathPrefix opens the canonical path stamped on a call site whose
// URL argument genuinely cannot be resolved statically.
//
// hasDynamicBaseURLPath keys on a first segment that opens a placeholder, so any
// path under this prefix classifies as url_kind=dynamic_baseurl with no
// special-casing downstream.
//
// #6446 — the marker used to be the bare path "/{dynamic}", identical for every
// dynamic site. makeEmit dedups on (patternType, id), so N dynamic call sites in
// one file collapsed into ONE entity. The reporter's headline complaint was an
// undercount; a collapsing marker reintroduces one. The argument expression is
// now slugged into a second segment, so two sites collapse only when they call
// the same expression — which is the right answer, not an accident.
const dynamicClientPathPrefix = "/{dynamic}"

// receiverClientCallHeadRe matches only the CALL HEAD of a receiver-style client
// call — `this.<field>.<verb><T>(` — stopping at the first character of the
// argument list. The argument is classified in Go rather than in the regex,
// because the interesting cases (a call expression, a template literal opening
// with an interpolation) are exactly the ones a URL-shaped regex cannot express.
var receiverClientCallHeadRe = regexp.MustCompile(
	`(?:^|[^\w$])\bthis\s*\.\s*` + nestHTTPReceiverAlternation + `\s*\.\s*` +
		`(get|post|put|patch|delete|head|options)\s*` +
		jsGenericArgs + `\s*\(\s*`,
)

// dynamicSlugUnsafeRe matches every byte that may not appear in a path slug.
var dynamicSlugUnsafeRe = regexp.MustCompile(`[^A-Za-z0-9_.$-]+`)

// slugifyDynamicArg renders an unresolvable argument expression as a single
// path segment, so two distinct dynamic call sites in one file get two distinct
// entity IDs. `base(code)` → `base_code`. Returns "" when nothing usable is
// left, in which case the caller falls back to the bare prefix.
func slugifyDynamicArg(expr string) string {
	slug := dynamicSlugUnsafeRe.ReplaceAllString(strings.TrimSpace(expr), "_")
	slug = strings.Trim(slug, "_.-$")
	if len(slug) > 48 {
		slug = strings.Trim(slug[:48], "_.-$")
	}
	return slug
}

// firstArgExpr returns the source text of the first argument of a call whose
// argument list starts at rest[0], stopping at the matching close paren or the
// first top-level comma. Bounded to one line and 200 bytes so a malformed source
// cannot make this quadratic.
func firstArgExpr(rest string) string {
	depth := 0
	limit := len(rest)
	if limit > 200 {
		limit = 200
	}
	for i := 0; i < limit; i++ {
		switch rest[i] {
		case '\n', '\r':
			return rest[:i]
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			if depth == 0 {
				return rest[:i]
			}
			depth--
		case ',':
			if depth == 0 {
				return rest[:i]
			}
		}
	}
	return rest[:limit]
}

// synthesizeReceiverClientResidualCalls emits a call site for every
// `this.<field>.<verb>(<expr>)` the static and template-literal passes above
// declined, splitting them into the two cases those passes conflated:
//
//  1. A literal the passes declined only because it lacks a leading slash —
//     `this.http.get('assets/i18n/en.json')`. The path is right there in the
//     source. #6446: emitting it as runtime_dynamic claimed "unresolvable"
//     about a URL this pass was holding, AND contradicted the cross extractor,
//     which minted the real path from the same call site in the same file.
//     These are now emitted STATIC, at the slash-prefixed path.
//
//  2. An argument that genuinely carries no knowable path — `base(code)`, a
//     call expression (#6433's reported case). Resolving it would require
//     base-URL constant folding, deliberately NOT attempted here. The call site
//     is emitted under the dynamic marker with runtime_dynamic=true, the same
//     marker the env-var concatenation pass (#721) uses, so it reaches the
//     repair flow (#732) instead of vanishing. 0 nodes is why the frontend side
//     of cross-stack linking was empty; a node marked dynamic is reviewable
//     where a missing node is invisible.
//
// Arguments the static / template passes DO resolve are skipped, so no call site
// is emitted twice — pinned by TestSynth_AngularHttpClient_StaticURLIsNotAlsoDynamic_6433,
// because deleting the skip is otherwise invisible to every other gate.
func synthesizeReceiverClientResidualCalls(content string, funcs []jsFuncSpan, syms map[string]string, emit jsRuntimeEmitFn) {
	if !hasInjectedHTTPClientToken(content) {
		return
	}

	for _, m := range receiverClientCallHeadRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 {
			continue
		}
		verb := strings.ToUpper(content[m[2]:m[3]])
		rest := content[m[1]:]
		if rest == "" {
			continue
		}
		caller := enclosingJSFuncAt(funcs, m[0])

		// emitStatic emits a resolved path with runtimeDynamic=false.
		emitStatic := func(path string) {
			emit(verb, httproutes.Canonicalize(httproutes.FrameworkExpress, path),
				"angular_http_client", "Function", caller, false, "", "")
		}

		switch rest[0] {
		case ')':
			// Zero-argument call — not an HTTP call site we can name.
			continue
		case '\'', '"':
			end := strings.IndexByte(rest[1:], rest[0])
			if end < 0 {
				continue
			}
			raw := rest[1 : 1+end]
			if _, ok := normalizeRawClientPath(raw); ok {
				// Owned by the static-literal pass above.
				continue
			}
			// Case 1: a relative literal the static pass declined to prefix.
			if path, ok := normalizeRawClientPath("/" + raw); ok && path != "" {
				emitStatic(path)
				continue
			}
		case '`':
			end := strings.IndexByte(rest[1:], '`')
			if end < 0 {
				continue
			}
			tmpl := rest[1 : 1+end]
			if strings.ContainsAny(tmpl, "\n\r") {
				continue
			}
			if _, ok := canonicalizeTemplateLiteral(tmpl, syms); ok {
				// Owned by the template-literal pass above.
				continue
			}
			// Case 1, template flavour: `assets/${lang}.json`.
			if path, ok := canonicalizeTemplateLiteral("/"+tmpl, syms); ok && path != "" {
				emitStatic(path)
				continue
			}
		}

		// Case 2: genuinely unresolvable.
		path := dynamicClientPathPrefix
		if slug := slugifyDynamicArg(firstArgExpr(rest)); slug != "" {
			path += "/" + slug
		}
		emit(verb, path, "angular_http_client", "Function", caller, true, "", "")
	}
}

// ---------------------------------------------------------------------------
// Idiom 2 — Apollo Client URI
// ---------------------------------------------------------------------------

// apolloClientURIRe matches `new ApolloClient({ uri: "..." })` or
// `new ApolloClient({ uri: X || "..." })` (common pattern for env-var
// fallback). We match the FIRST string literal inside the URI value that
// looks like an absolute URL, since the env-var form is the runtime-dynamic
// branch (handled by the runtimeDynamic flag if needed).
//
// Capture groups:
//
//	1 = uri string literal value (absolute URL, may contain path)
var apolloClientURIRe = regexp.MustCompile(
	`(?:^|[^\w$.])new\s+ApolloClient\s*\(\s*\{[^}]*\buri\s*:\s*` +
		`(?:[^'"` + "`" + `\n\r]*[|]{2}[^'"` + "`" + `\n\r]*)?\s*` + // optional `X ||` prefix
		`['"` + "`" + `]((?:https?://)[^'"` + "`" + `\n\r$]+)['"` + "`" + `]`,
)

// apolloClientURIFallbackRe matches the common pattern where the static
// literal is given as a fallback to a process.env variable:
//
//	new ApolloClient({ uri: process.env.X || "http://host/graphql" })
//
// This is a secondary pattern; the primary apolloClientURIRe handles the
// case where the literal appears directly after `uri:`.
// Both are anchored the same way, so apolloClientURIRe already fires on
// the `||` form when the group matches past the `|| ` text.

// synthesizeApolloClientURI extracts the GraphQL endpoint URI from
// `new ApolloClient({ uri: "..." })` call sites. Emits a GRAPHQL-verb
// synthetic pointing to the canonical path of the target endpoint.
//
// The emitted entity path is the canonical path of the URI (host stripped).
// For `uri: "http://search-graphql:4000/graphql"` this yields `/graphql`,
// which matches the server-side `http:GRAPHQL:/graphql/...` synthetics emitted
// by synthesizeGraphQLResolvers (the field-level `/graphql/Query/<field>` paths
// are more specific; the `/graphql` root serves as a base match for the linker).
func synthesizeApolloClientURI(content string, funcs []jsFuncSpan, emit emitFn) {
	if !strings.Contains(content, "ApolloClient") {
		return
	}

	for _, m := range apolloClientURIRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 {
			continue
		}
		raw := content[m[2]:m[3]]
		if raw == "" {
			continue
		}

		// Strip host — we only care about the path for cross-repo matching.
		path := stripURLHost(raw)
		if !looksLikeURLPath(path) {
			continue
		}

		caller := enclosingJSFuncAt(funcs, m[0])
		canonical := httproutes.Canonicalize(httproutes.FrameworkExpress, path)
		emit("GRAPHQL", canonical, "apollo_client_uri", "Function", caller)
	}
}

// ---------------------------------------------------------------------------
// Elixir Finch / HTTPoison client extraction
// ---------------------------------------------------------------------------
//
// Idiom 3 — Elixir Finch / HTTPoison (#1483 optional).
//
// Elixir HTTP clients use:
//
//	Finch.build(:get, url) |> Finch.request(...)
//	HTTPoison.get(url)
//	HTTPoison.post(url, body)
//
// where url may be a string literal or a string interpolation:
//
//	url = "#{@base_url}/orders/#{id}"
//
// We implement a lightweight regex-based synthesizer for Elixir, hooking
// into the synthesis switch in http_endpoint_synthesis.go via a new
// "elixir" case.

// elixirFinchBuildRe matches `Finch.build(:verb, url)` and
// `Finch.build("verb", url)` calls in Elixir source.
// Capture groups:
//
//	1 = HTTP atom/string verb (:get, :post, etc.)
//	2 = URL string literal (double-quoted Elixir string)
var elixirFinchBuildRe = regexp.MustCompile(
	`\bFinch\.build\s*\(\s*:(get|post|put|patch|delete|head|options)\s*,\s*` +
		`"((?:https?://|/)[^"\n\r#]*)"`,
)

// elixirFinchBuildVarRe matches `Finch.build(:verb, varName)` where the
// URL is a local variable (resolved via the Elixir const symbol table).
// Capture groups:
//
//	1 = verb atom
//	2 = variable name
var elixirFinchBuildVarRe = regexp.MustCompile(
	`\bFinch\.build\s*\(\s*:(get|post|put|patch|delete|head|options)\s*,\s*([a-z_][a-z0-9_]*)`,
)

// elixirHTTPoisonRe matches `HTTPoison.<verb>(url, ...)` calls.
// Capture groups:
//
//	1 = HTTP verb (get|post|put|patch|delete|head|options)
//	2 = URL string literal
var elixirHTTPoisonRe = regexp.MustCompile(
	`\bHTTPoison\.(get|post|put|patch|delete|head|options)\s*\(\s*` +
		`"((?:https?://|/)[^"\n\r#]*)"`,
)

// elixirHTTPoisonVarRe matches `HTTPoison.<verb>(varName, ...)`.
// Capture groups:
//
//	1 = verb
//	2 = variable name
var elixirHTTPoisonVarRe = regexp.MustCompile(
	`\bHTTPoison\.(get|post|put|patch|delete|head|options)\s*\(\s*([a-z_][a-z0-9_]*)`,
)

// elixirStringConcatRe matches Elixir variable assignments with string
// interpolation that looks like a URL path:
//
//	url = "#{@base_url}/orders/#{id}"
//	path = "#{base}/products"
//
// Capture groups:
//
//	1 = variable name
//	2 = interpolated string body (content between quotes)
var elixirStringConcatRe = regexp.MustCompile(
	`(?m)\b([a-z_][a-z0-9_]*)\s*=\s*"([^"\n\r]*#\{[^"\n\r]*)"`,
)

// elixirModuleAttrRe matches `@attr_name "value"` module attributes used
// as base URL prefixes in Elixir, e.g. `@base_url "http://gateway:4000"`.
// Capture groups:
//
//	1 = attribute name (without @)
//	2 = string value
var elixirModuleAttrRe = regexp.MustCompile(
	`(?m)@([a-z_][a-z0-9_]*)\s+"([^"\n\r]+)"`,
)

// elixirModuleAttrEnvRe matches a module attribute whose value is a
// `System.get_env/2` call carrying a static default literal, e.g.
//
//	@base_url System.get_env("GATEWAY_URL", "http://gateway:3000")
//
// This is the idiomatic Elixir config pattern (#1496): the base URL is
// resolved from an env var at boot with a hard-coded fallback. The fallback
// literal is the only statically-knowable value, so we register it as the
// attribute's symbol-table value — exactly mirroring how the JS/TS side
// treats `process.env.X || "literal"`. Without this the `#{@base_url}`
// interpolation never resolves and the canonical path keeps a bogus
// `{@base_url}` segment, so the cross-repo link can never form.
//
// Capture groups:
//
//	1 = attribute name (without @)
//	2 = default string literal (2nd arg to System.get_env)
var elixirModuleAttrEnvRe = regexp.MustCompile(
	`(?m)@([a-z_][a-z0-9_]*)\s+System\.get_env\(\s*"[^"\n\r]*"\s*,\s*"([^"\n\r]+)"\s*\)`,
)

// elixirInterpolationRe matches `#{expr}` inside Elixir string interpolations.
var elixirInterpolationRe = regexp.MustCompile(`#\{([^}]+)\}`)

// buildElixirSymbolTable returns a map of variable name → string value for
// simple module attribute and local variable declarations.
func buildElixirSymbolTable(content string) map[string]string {
	syms := make(map[string]string)
	// Module attributes: @base_url "http://gateway:4000"
	for _, m := range elixirModuleAttrRe.FindAllStringSubmatch(content, -1) {
		if len(m) >= 3 {
			key := "@" + m[1]
			if _, dup := syms[key]; !dup {
				syms[key] = m[2]
			}
		}
	}
	// #1496 — Module attributes resolved from System.get_env/2 with a static
	// default: @base_url System.get_env("GATEWAY_URL", "http://gateway:3000").
	// The fallback literal is the statically-knowable value.
	for _, m := range elixirModuleAttrEnvRe.FindAllStringSubmatch(content, -1) {
		if len(m) >= 3 {
			key := "@" + m[1]
			if _, dup := syms[key]; !dup {
				syms[key] = m[2]
			}
		}
	}
	return syms
}

// canonicalizeElixirInterpolation converts an Elixir interpolated string
// like `"#{@base_url}/orders/#{id}"` into a canonical URL path by:
//  1. Resolving @module_attr references from the symbol table.
//  2. Replacing remaining `#{expr}` interpolations with `{name}` placeholders.
//  3. Stripping the host prefix if the result begins with `http://` or `https://`.
func canonicalizeElixirInterpolation(tmpl string, syms map[string]string) (string, bool) {
	result := elixirInterpolationRe.ReplaceAllStringFunc(tmpl, func(match string) string {
		inner := strings.TrimSpace(match[2 : len(match)-1])

		// Try direct symbol lookup (covers @base_url, @prefix, etc.)
		if val, ok := syms[inner]; ok {
			return val
		}
		// @attr with dot notation: `@module.field` → try @module
		if strings.HasPrefix(inner, "@") {
			parts := strings.SplitN(inner, ".", 2)
			if val, ok := syms[parts[0]]; ok {
				return val
			}
		}
		// Simple variable reference → named placeholder
		if len(inner) > 0 {
			// Use last segment of dotted expr
			if dot := strings.LastIndexByte(inner, '.'); dot >= 0 {
				seg := inner[dot+1:]
				if len(seg) > 0 {
					return "{" + seg + "}"
				}
			}
			return "{" + inner + "}"
		}
		return "{param}"
	})

	result = stripURLHost(result)
	if !looksLikeURLPathOrParam(result) {
		return "", false
	}
	return result, true
}

// synthesizeElixirHTTPClients scans an Elixir source file for Finch and
// HTTPoison consumer-side HTTP calls and emits http_endpoint_call synthetics.
// This is the entry point called from applyHTTPEndpointSynthesis for lang="elixir".
func synthesizeElixirHTTPClients(content string, emit emitFn) {
	if !strings.Contains(content, "Finch") && !strings.Contains(content, "HTTPoison") {
		return
	}

	syms := buildElixirSymbolTable(content)

	// Build a table of interpolated-string variable assignments, e.g.
	// `url = "#{@base_url}/orders/#{id}"`.
	// We collect ALL assignments (not just the first per name) because the same
	// variable name (e.g. `url`) may be reused in multiple function bodies. We
	// store them as a slice per name and process each independently.
	interpolatedVarSlices := make(map[string][]string)
	for _, m := range elixirStringConcatRe.FindAllStringSubmatch(content, -1) {
		if len(m) >= 3 {
			interpolatedVarSlices[m[1]] = append(interpolatedVarSlices[m[1]], m[2])
		}
	}

	// Finch.build(:verb, "literal_url")
	for _, m := range elixirFinchBuildRe.FindAllStringSubmatch(content, -1) {
		if len(m) < 3 {
			continue
		}
		verb := strings.ToUpper(m[1])
		raw := m[2]
		path, ok := normalizeRawClientPath(raw)
		if !ok {
			continue
		}
		canonical := httproutes.Canonicalize(httproutes.FrameworkExpress, path)
		emit(verb, canonical, "finch", "", "")
	}

	// Finch.build(:verb, url_var) — resolve variable (all assignments per name)
	for _, m := range elixirFinchBuildVarRe.FindAllStringSubmatch(content, -1) {
		if len(m) < 3 {
			continue
		}
		verb := strings.ToUpper(m[1])
		varName := m[2]
		tmpls, ok := interpolatedVarSlices[varName]
		if !ok {
			continue
		}
		for _, tmpl := range tmpls {
			path, ok2 := canonicalizeElixirInterpolation(tmpl, syms)
			if !ok2 {
				continue
			}
			canonical := httproutes.Canonicalize(httproutes.FrameworkExpress, path)
			emit(verb, canonical, "finch", "", "")
		}
	}

	// HTTPoison.<verb>("literal_url")
	for _, m := range elixirHTTPoisonRe.FindAllStringSubmatch(content, -1) {
		if len(m) < 3 {
			continue
		}
		verb := strings.ToUpper(m[1])
		raw := m[2]
		path, ok := normalizeRawClientPath(raw)
		if !ok {
			continue
		}
		canonical := httproutes.Canonicalize(httproutes.FrameworkExpress, path)
		emit(verb, canonical, "httpoison", "", "")
	}

	// HTTPoison.<verb>(url_var) — resolve variable (all assignments per name)
	for _, m := range elixirHTTPoisonVarRe.FindAllStringSubmatch(content, -1) {
		if len(m) < 3 {
			continue
		}
		verb := strings.ToUpper(m[1])
		varName := m[2]
		tmpls, ok := interpolatedVarSlices[varName]
		if !ok {
			continue
		}
		for _, tmpl := range tmpls {
			path, ok2 := canonicalizeElixirInterpolation(tmpl, syms)
			if !ok2 {
				continue
			}
			canonical := httproutes.Canonicalize(httproutes.FrameworkExpress, path)
			emit(verb, canonical, "httpoison", "", "")
		}
	}
}
