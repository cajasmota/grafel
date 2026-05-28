// Cross-framework middleware_coverage for the JS/TS backend-HTTP frameworks
// (#2853).
//
// This pass resolves a structured middleware chain for every synthetic
// http_endpoint_definition emitted by the JS/TS backend synthesizers
// (#2851 + the pre-existing Express/Koa/Fastify/Hono/Nest passes). It is the
// SUPERSET of the #2852 auth pass: auth middleware (passport, guards, …) is one
// kind of middleware, but the middleware picture also covers the non-auth
// chain — logging, validation, body parsing, rate-limiting, CORS, error
// handlers, NestJS interceptors/pipes/filters, Hapi ext points, Fastify hooks,
// Adonis named middleware and Feathers hooks. The auth pass already attributes
// auth middleware per endpoint and stamps `auth_*`; this pass attributes the
// full chain and stamps the `middleware_*` property contract so the broader
// middleware view greens uniformly across all twelve framework families.
//
// Attribution model (mirrors the auth resolver's precedence, but accumulates
// rather than short-circuits — an endpoint may carry both route-level and
// app-level middleware):
//
//   - route-level chain — middleware passed directly to a route registration,
//     e.g. `app.get('/me', logger, validate, handler)`, NestJS method-level
//     `@UseInterceptors(...)`/`@UsePipes(...)`/`@UseFilters(...)`, Hapi route
//     `options.pre`/`options.ext`, Adonis `.middleware([...])`, Feathers
//     per-service hooks, Marble per-effect `use(...)`.
//   - app/router-level chain — `app.use(fn)` / `router.use(fn)` / Fastify
//     `addHook`/global hook, NestJS class-level interceptor/pipe/filter, Hapi
//     `server.ext(...)`, Feathers `app.hooks({...})`. Applies to every endpoint
//     registered in the same file scope.
//
// Output (stamped only on producer-side http_endpoint_definition entities):
//
//	middleware_chain     — JSON-encoded []AuthSignal (reuses the auth evidence
//	                       struct: Kind/Text/File/Line) of the resolved chain.
//	middleware_count     — decimal count of resolved middleware (>0 ⇒ covered).
//	middleware_names     — comma-joined recognised middleware symbols (MCP key).
//	middleware_scope     — "route" | "app" | "route+app" | "" (no middleware).
//
// Refs #2853.
package engine

import (
	"encoding/json"
	"regexp"
	"strconv"
	"strings"

	"github.com/cajasmota/archigraph/internal/engine/httproutes"
	"github.com/cajasmota/archigraph/internal/types"
)

// jsMiddlewareSkipNames is the set of route-registration trailing-argument
// symbols that are the HANDLER, not middleware — they must not be counted as a
// middleware in a route-level chain. Anything that is neither a recognised
// handler convention nor a string/literal is treated as middleware.
var jsMiddlewareSkipNames = map[string]bool{
	"handler":    true,
	"controller": true,
	"next":       true,
	"req":        true,
	"res":        true,
	"request":    true,
	"reply":      true,
	"ctx":        true,
	"context":    true,
	"async":      true,
	"function":   true,
}

// jsNestPipelineDecoratorRe captures the NestJS interceptor/pipe/filter triad
// `@UseInterceptors(...)`, `@UsePipes(...)`, `@UseFilters(...)` plus
// `@UseGuards(...)` (guards are middleware too). Group 1 = decorator name,
// group 2 = argument list.
var jsNestPipelineDecoratorRe = regexp.MustCompile(
	`@(UseInterceptors|UsePipes|UseFilters|UseGuards)\s*\(([^)]*)\)`,
)

// jsFastifyHookRe captures Fastify lifecycle hooks
// `fastify.addHook('onRequest', fn)` / `app.addHook('preHandler', fn)` and the
// per-route hook keys (`preHandler:`, `onRequest:`, `preValidation:`). Group 1
// = the hook phase name.
var jsFastifyHookRe = regexp.MustCompile(
	`\.addHook\s*\(\s*['"` + "`" + `](onRequest|preParsing|preValidation|preHandler|preSerialization|onSend|onResponse|onError|onTimeout)['"` + "`" + `]`,
)

// jsFastifyRouteHookRe captures the per-route-options hook keys inside a
// `{ ... }` route config. Group 1 = the hook key.
var jsFastifyRouteHookRe = regexp.MustCompile(
	`\b(preHandler|onRequest|preValidation|preParsing|preSerialization|onSend|onResponse)\s*:`,
)

// jsHapiExtRe captures a Hapi server extension point
// `server.ext('onPreHandler', fn)` / `server.ext({ type: 'onRequest', ... })`.
// Group 1 = the ext-point name (when given as a string).
var jsHapiExtRe = regexp.MustCompile(
	`\.ext\s*\(\s*(?:['"` + "`" + `](onRequest|onPreAuth|onCredentials|onPostAuth|onPreHandler|onPostHandler|onPreResponse)['"` + "`" + `]|\{)`,
)

// jsHapiRoutePreRe captures a Hapi per-route `pre:` / `options.pre:` /
// `config.pre:` middleware list and per-route `ext:` points.
var jsHapiRoutePreRe = regexp.MustCompile(`\b(pre|ext)\s*:\s*(\[|\{)`)

// jsFeathersHookRe captures a Feathers hook registration
// `app.hooks({...})` / `service.hooks({ before: {...}, after: {...} })`.
// Group 1 = the hook timing keyword.
var jsFeathersHookRe = regexp.MustCompile(`\b(before|after|error)\s*:\s*(?:\{|\[)`)

// jsKoaUseRe / jsHonoUseRe are covered by the shared jsAppUseRe (auth pass)
// receiver list (app|router|server|fastify|api|r|v\d+). Koa and Hono both use
// `app.use(fn)`, so app-level middleware detection is unified.

// MiddlewareScope classifies where a chain's middleware originate.
const (
	middlewareScopeRoute    = "route"
	middlewareScopeApp      = "app"
	middlewareScopeRouteApp = "route+app"
)

// applyJSTSMiddlewareCoverage resolves and stamps the middleware chain on every
// JS/TS synthetic backend endpoint emitted for this file. Like the auth pass it
// mutates Properties in place and never adds or removes entities.
//
// `before` is the entity-slice length captured before the JS/TS synthesizers
// ran; only entities at index >= before that belong to this file are
// considered.
func applyJSTSMiddlewareCoverage(content, path string, entities []types.EntityRecord, before int) {
	if len(content) == 0 || before >= len(entities) {
		return
	}

	// App/router-level middleware (file scope): app.use / router.use /
	// Fastify addHook / NestJS class-level pipeline decorators / Hapi server.ext
	// / Feathers app-level hooks.
	appLevel := indexAppLevelMiddleware(content, path)

	// Route-level chains keyed by "<VERB> <canonical-path>".
	routeMW := indexRouteLevelMiddleware(content, path)
	// NestJS method-level interceptor/pipe/filter/guard decorators by method.
	nestMethodMW := indexNestMethodMiddleware(content, path)
	// Hapi per-route pre/ext middleware by (verb, canonical path).
	hapiRouteMW := indexHapiRouteMiddleware(content, path)
	// Adonis route-chain middleware by (verb, canonical path).
	adonisRouteMW := indexAdonisRouteMiddleware(content, path)
	// Marble per-effect middleware by (verb, canonical path).
	marbleRouteMW := indexMarbleRouteMiddleware(content, path)
	// Feathers per-service hook coverage by mount path (service prefix).
	feathersMW := indexFeathersHookMiddleware(content, path)

	for i := before; i < len(entities); i++ {
		e := &entities[i]
		if e.Kind != httpEndpointDefinitionKind || e.SourceFile != path {
			continue
		}
		if e.Properties == nil {
			continue
		}
		verb := strings.ToUpper(e.Properties["verb"])
		canonical := e.Properties["path"]
		key := verb + " " + canonical

		var route []AuthSignal
		route = append(route, routeMW[key]...)
		if handler := nestHandlerName(e.Properties["source_handler"]); handler != "" {
			route = append(route, nestMethodMW[handler]...)
		}
		route = append(route, hapiRouteMW[key]...)
		route = append(route, adonisRouteMW[key]...)
		route = append(route, marbleRouteMW[key]...)
		// Feathers: a service's hooks apply to every verb at its mount path.
		route = append(route, feathersMW[feathersMountOf(canonical)]...)

		stampMiddlewareChain(e.Properties, route, appLevel)
	}
}

// stampMiddlewareChain writes the resolved chain onto an endpoint Properties map.
func stampMiddlewareChain(props map[string]string, route, appLevel []AuthSignal) {
	chain := make([]AuthSignal, 0, len(route)+len(appLevel))
	chain = append(chain, route...)
	chain = append(chain, appLevel...)
	chain = dedupeSignals(chain)
	if len(chain) == 0 {
		return
	}
	if encoded := encodeMiddlewareChain(chain); encoded != "" {
		props["middleware_chain"] = encoded
	}
	props["middleware_count"] = strconv.Itoa(len(chain))
	props["middleware_names"] = middlewareNames(chain)
	switch {
	case len(route) > 0 && len(appLevel) > 0:
		props["middleware_scope"] = middlewareScopeRouteApp
	case len(route) > 0:
		props["middleware_scope"] = middlewareScopeRoute
	default:
		props["middleware_scope"] = middlewareScopeApp
	}
}

// dedupeSignals removes duplicate (Kind, Text) signals while preserving order.
func dedupeSignals(in []AuthSignal) []AuthSignal {
	seen := map[string]bool{}
	out := in[:0:0]
	for _, s := range in {
		k := s.Kind + "\x00" + s.Text
		if seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, s)
	}
	return out
}

// encodeMiddlewareChain JSON-encodes the chain for the middleware_chain property.
func encodeMiddlewareChain(chain []AuthSignal) string {
	b, err := json.Marshal(chain)
	if err != nil {
		return ""
	}
	return string(b)
}

// middlewareNames returns the comma-joined recognised middleware symbols for the
// MCP signal property, stripping the scope-prefix annotations the indexers add.
func middlewareNames(chain []AuthSignal) string {
	names := make([]string, 0, len(chain))
	seen := map[string]bool{}
	for _, s := range chain {
		n := middlewareEvidenceSymbol(s.Text)
		if n == "" || seen[n] {
			continue
		}
		seen[n] = true
		names = append(names, n)
	}
	return strings.Join(names, ",")
}

// middlewareEvidenceSymbol strips the indexer scope/idiom prefixes to expose the
// bare middleware symbol.
func middlewareEvidenceSymbol(text string) string {
	for _, prefix := range []string{
		"app/router-level: ", "route-level: ", "fastify-hook: ", "hapi-ext: ",
		"nest-class: ", "nest-method: ", "feathers-hook: ", "marble: ",
	} {
		text = strings.TrimPrefix(text, prefix)
	}
	return strings.TrimSpace(text)
}

// ---------------------------------------------------------------------------
// App/router-level middleware indexer
// ---------------------------------------------------------------------------

// indexAppLevelMiddleware collects every file-scope middleware registration:
// app.use / router.use (Express/Koa/Hono/Polka/Restify), Fastify addHook,
// NestJS class-level pipeline decorators, Hapi server.ext, and Feathers
// app-level hooks. Returns the deduplicated signal list.
func indexAppLevelMiddleware(content, file string) []AuthSignal {
	var out []AuthSignal

	// Marble.js uses `r.use(effect$)` INSIDE a route pipe (route-scoped, handled
	// by indexMarbleRouteMiddleware), so the generic `r.use` receiver must not be
	// treated as an app-level chain in a Marble file.
	isMarble := strings.Contains(content, "r.pipe") && strings.Contains(content, "matchPath")

	// app.use(fn) / router.use(fn) / server.use(fn) — the generic chain.
	for _, m := range jsAppUseRe.FindAllStringSubmatchIndex(content, -1) {
		if isMarble && content[m[2]:m[3]] == "r" {
			continue
		}
		arg := strings.TrimSpace(content[m[4]:m[5]])
		sym := symbolHead(arg)
		if !isMiddlewareArg(sym) {
			continue
		}
		out = append(out, AuthSignal{
			Kind: "middleware",
			Text: "app/router-level: " + sym,
			File: file,
			Line: lineAtOffset(content, m[0]),
		})
	}

	// Fastify global lifecycle hooks: fastify.addHook('preHandler', fn).
	for _, m := range jsFastifyHookRe.FindAllStringSubmatchIndex(content, -1) {
		phase := content[m[2]:m[3]]
		out = append(out, AuthSignal{
			Kind: "hook",
			Text: "fastify-hook: " + phase,
			File: file,
			Line: lineAtOffset(content, m[0]),
		})
	}

	// Hapi server extension points: server.ext('onPreHandler', fn).
	for _, m := range jsHapiExtRe.FindAllStringSubmatchIndex(content, -1) {
		phase := "ext"
		if m[2] >= 0 {
			phase = content[m[2]:m[3]]
		}
		out = append(out, AuthSignal{
			Kind: "ext",
			Text: "hapi-ext: " + phase,
			File: file,
			Line: lineAtOffset(content, m[0]),
		})
	}

	// NestJS class-level interceptor/pipe/filter/guard decorators.
	out = append(out, nestClassPipelineSignals(content, file)...)

	// Feathers app-level hooks: app.hooks({ before: {...}, after: {...} }).
	if strings.Contains(content, "app.hooks(") || strings.Contains(content, "feathers().hooks(") {
		for _, m := range jsFeathersHookRe.FindAllStringSubmatchIndex(content, -1) {
			timing := content[m[2]:m[3]]
			out = append(out, AuthSignal{
				Kind: "hook",
				Text: "feathers-hook: app " + timing,
				File: file,
				Line: lineAtOffset(content, m[0]),
			})
		}
	}

	return dedupeSignals(out)
}

// nestClassPipelineSignals returns class-level @UseInterceptors/@UsePipes/
// @UseFilters/@UseGuards signals (the decorator block directly above a class).
func nestClassPipelineSignals(content, file string) []AuthSignal {
	if !strings.Contains(content, "@Use") {
		return nil
	}
	var out []AuthSignal
	for _, m := range jsClassDeclRe.FindAllStringSubmatchIndex(content, -1) {
		block := precedingDecoratorBlock(content, m[0])
		if block == "" {
			continue
		}
		for _, dm := range jsNestPipelineDecoratorRe.FindAllStringSubmatch(block, -1) {
			args := strings.TrimSpace(dm[2])
			if args == "" {
				continue
			}
			out = append(out, AuthSignal{
				Kind: "decorator",
				Text: "nest-class: @" + dm[1] + "(" + args + ")",
				File: file,
				Line: lineAtOffset(content, m[0]),
			})
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// Route-level chain indexer (Express family: Express/Koa/Hono/Polka/Restify)
// ---------------------------------------------------------------------------

// indexRouteLevelMiddleware scans Express-shaped route registrations and records
// every middleware argument in the chain (the args between the path and the
// final handler). Returns a map keyed by "<VERB> <canonical-path>".
func indexRouteLevelMiddleware(content, _ string) map[string][]AuthSignal {
	out := map[string][]AuthSignal{}
	for _, m := range jsRouteRegistrationRe.FindAllStringSubmatchIndex(content, -1) {
		verb := strings.ToUpper(content[m[4]:m[5]])
		if verb == "DEL" {
			verb = "DELETE"
		}
		if verb == "OPTS" {
			verb = "OPTIONS"
		}
		raw := content[m[6]:m[7]]
		rest := content[m[8]:m[9]]
		mws := middlewareChainSymbols(rest)
		if len(mws) == 0 {
			continue
		}
		canonical := httproutes.Canonicalize(httproutes.FrameworkExpress, raw)
		if canonical == "" {
			continue
		}
		key := verb + " " + canonical
		line := lineAtOffset(content, m[0])
		for _, sym := range mws {
			out[key] = append(out[key], AuthSignal{
				Kind: "middleware",
				Text: "route-level: " + sym,
				Line: line,
			})
		}
	}
	return out
}

// middlewareChainSymbols returns the middleware symbols in a route's arg list
// (everything after the path). The LAST argument is the handler and is dropped;
// every preceding non-string argument is treated as a middleware. Inline arrow
// handlers leave only the handler arg, so no middleware is reported.
func middlewareChainSymbols(rest string) []string {
	args := splitTopLevelArgs(rest)
	if len(args) <= 1 {
		// Only the handler (or nothing) — no preceding middleware chain.
		return nil
	}
	// Drop the trailing handler argument; the rest are middleware.
	mwArgs := args[:len(args)-1]
	var out []string
	for _, a := range mwArgs {
		sym := symbolHead(strings.TrimSpace(a))
		if !isMiddlewareArg(sym) {
			continue
		}
		out = append(out, sym)
	}
	return out
}

// isMiddlewareArg reports whether a route/use argument symbol denotes a
// middleware (vs. a handler, a string literal, or noise). Empty / numeric /
// quoted / known-handler tokens are rejected.
func isMiddlewareArg(sym string) bool {
	sym = strings.TrimSpace(sym)
	if sym == "" {
		return false
	}
	// Reject string / template / numeric literals.
	switch sym[0] {
	case '\'', '"', '`', '{', '[':
		return false
	}
	if sym[0] >= '0' && sym[0] <= '9' {
		return false
	}
	if jsMiddlewareSkipNames[strings.ToLower(sym)] {
		return false
	}
	// Require an identifier-ish head (call expression or member access allowed).
	return jsMiddlewareIdentRe.MatchString(sym)
}

// jsMiddlewareIdentRe matches an identifier, member access, or call expression
// head used as a middleware reference (e.g. `cors`, `cors()`, `express.json()`,
// `passport.authenticate('jwt')`).
var jsMiddlewareIdentRe = regexp.MustCompile(`^[A-Za-z_$][\w$]*(?:\.[A-Za-z_$][\w$]*)*(?:\([^)]*\))?$`)

// ---------------------------------------------------------------------------
// NestJS method-level pipeline indexer
// ---------------------------------------------------------------------------

// indexNestMethodMiddleware attributes each handler method's own preceding
// interceptor/pipe/filter/guard decorators to that method (keyed by method
// name). A method binds only its own decorator block — the same nearest-block
// discipline the auth pass uses.
func indexNestMethodMiddleware(content, _ string) map[string][]AuthSignal {
	out := map[string][]AuthSignal{}
	if !strings.Contains(content, "@Use") {
		return out
	}
	for _, loc := range nestHandlerMethodRe.FindAllStringSubmatchIndex(content, -1) {
		method := content[loc[2]:loc[3]]
		block := precedingDecoratorBlock(content, loc[0])
		if block == "" || !nestVerbDecoratorRe.MatchString(block) {
			continue
		}
		line := lineAtOffset(content, loc[0])
		for _, dm := range jsNestPipelineDecoratorRe.FindAllStringSubmatch(block, -1) {
			args := strings.TrimSpace(dm[2])
			if args == "" {
				continue
			}
			out[method] = append(out[method], AuthSignal{
				Kind: "decorator",
				Text: "nest-method: @" + dm[1] + "(" + args + ")",
				Line: line,
			})
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// Hapi per-route middleware indexer (pre / ext + Fastify route hooks share the
// object-body scan)
// ---------------------------------------------------------------------------

// indexHapiRouteMiddleware scans each `server.route({ ... })` body for a per-
// route `pre`/`ext` middleware list and any Fastify-style per-route hook keys
// (some Hapi-shaped configs carry both). Keyed by (verb, canonical path).
func indexHapiRouteMiddleware(content, _ string) map[string][]AuthSignal {
	out := map[string][]AuthSignal{}
	if !strings.Contains(content, ".route(") {
		return out
	}
	for _, idx := range hapiRouteRe.FindAllStringSubmatchIndex(content, -1) {
		braceOpen := idx[2]
		braceClose := findMatchingBrace(content, braceOpen)
		if braceClose < 0 {
			continue
		}
		body := content[braceOpen : braceClose+1]
		pm := hapiPathKwargRe.FindStringSubmatch(body)
		if len(pm) < 2 {
			continue
		}
		canonical := httproutes.Canonicalize(httproutes.FrameworkHapi, stripHapiPathModifiers(pm[1]))
		if canonical == "" {
			continue
		}
		verbs := parseHapiMethods(body)
		if len(verbs) == 0 {
			continue
		}
		var sigs []AuthSignal
		line := lineAtOffset(content, braceOpen)
		for _, prm := range jsHapiRoutePreRe.FindAllStringSubmatch(body, -1) {
			sigs = append(sigs, AuthSignal{
				Kind: "pre",
				Text: "hapi-ext: route " + prm[1],
				Line: line,
			})
		}
		for _, fm := range jsFastifyRouteHookRe.FindAllStringSubmatch(body, -1) {
			sigs = append(sigs, AuthSignal{
				Kind: "hook",
				Text: "fastify-hook: route " + fm[1],
				Line: line,
			})
		}
		if len(sigs) == 0 {
			continue
		}
		for _, verb := range verbs {
			out[verb+" "+canonical] = append(out[verb+" "+canonical], sigs...)
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// AdonisJS route-chain middleware indexer
// ---------------------------------------------------------------------------

// indexAdonisRouteMiddleware scans AdonisJS route registrations for chained
// `.middleware([...])` / `.use([...])` calls and records every named middleware
// (not just the auth ones the #2852 pass recognised). Keyed by (verb, canonical
// path).
func indexAdonisRouteMiddleware(content, _ string) map[string][]AuthSignal {
	out := map[string][]AuthSignal{}
	if !strings.Contains(content, "Route.") {
		return out
	}
	for _, m := range adonisVerbRe.FindAllStringSubmatchIndex(content, -1) {
		verb := strings.ToUpper(content[m[2]:m[3]])
		raw := content[m[4]:m[5]]
		canonical := httproutes.Canonicalize(httproutes.FrameworkAdonis, raw)
		if canonical == "" {
			continue
		}
		stmt := adonisStatementFrom(content, m[0])
		mw := jsAdonisMiddlewareRe.FindStringSubmatch(stmt)
		if mw == nil {
			continue
		}
		names := adonisMiddlewareNames(mw[1])
		if len(names) == 0 {
			continue
		}
		line := lineAtOffset(content, m[0])
		for _, n := range names {
			out[verb+" "+canonical] = append(out[verb+" "+canonical], AuthSignal{
				Kind: "middleware",
				Text: "route-level: " + n,
				Line: line,
			})
		}
	}
	return out
}

// adonisMiddlewareNames extracts the individual middleware names from an Adonis
// `.middleware(...)` argument list. Adonis names middleware by string key
// (`'auth'`, `'throttle'`, `'acl:admin'`) or by `middleware.x()` reference.
func adonisMiddlewareNames(args string) []string {
	var out []string
	for _, q := range jsQuotedTokenRe.FindAllStringSubmatch(args, -1) {
		if tok := strings.TrimSpace(q[1]); tok != "" {
			out = append(out, tok)
		}
	}
	// Adonis 6 `middleware.auth()` / `middleware.throttle()` references.
	for _, m := range jsAdonisRefMiddlewareRe.FindAllStringSubmatch(args, -1) {
		out = append(out, "middleware."+m[1])
	}
	return out
}

// jsAdonisRefMiddlewareRe captures the Adonis 6 `middleware.<name>()` reference
// form. Group 1 = the middleware name.
var jsAdonisRefMiddlewareRe = regexp.MustCompile(`\bmiddleware\.([A-Za-z_$][\w$]*)\s*\(`)

// ---------------------------------------------------------------------------
// Marble.js per-effect middleware indexer
// ---------------------------------------------------------------------------

// indexMarbleRouteMiddleware scans each Marble routing Effect for middleware
// effects piped into it via `use(...)` (the generic middleware idiom, not only
// auth). Keyed by (verb, canonical path).
func indexMarbleRouteMiddleware(content, _ string) map[string][]AuthSignal {
	out := map[string][]AuthSignal{}
	if !strings.Contains(content, "r.pipe") || !strings.Contains(content, "matchPath") {
		return out
	}
	for _, idx := range marbleEffectRe.FindAllStringSubmatchIndex(content, -1) {
		parenOpen := idx[1] - 1
		parenClose := findMatchingParenFrom(content, parenOpen)
		if parenClose < 0 {
			continue
		}
		body := content[parenOpen : parenClose+1]
		pm := marbleMatchPathRe.FindStringSubmatch(body)
		if len(pm) < 2 {
			continue
		}
		canonical := httproutes.Canonicalize(httproutes.FrameworkMarble, pm[1])
		if canonical == "" {
			continue
		}
		verb := "ANY"
		if tm := marbleMatchTypeRe.FindStringSubmatch(body); len(tm) >= 2 {
			verb = strings.ToUpper(tm[1])
		}
		line := lineAtOffset(content, idx[0])
		for _, um := range jsMarbleUseRe.FindAllStringSubmatch(body, -1) {
			out[verb+" "+canonical] = append(out[verb+" "+canonical], AuthSignal{
				Kind: "middleware",
				Text: "marble: use(" + strings.TrimSpace(um[1]) + ")",
				Line: line,
			})
		}
	}
	return out
}

// jsMarbleUseRe captures a Marble `use(<effect>)` middleware call inside a pipe.
// Group 1 = the effect name.
var jsMarbleUseRe = regexp.MustCompile(`\buse\s*\(\s*([A-Za-z_$][\w$]*\$?)\s*\)`)

// ---------------------------------------------------------------------------
// Feathers per-service hook indexer
// ---------------------------------------------------------------------------

// indexFeathersHookMiddleware scans for Feathers service hook registrations.
// Feathers attaches hooks to services (`service.hooks({ before, after, error })`)
// rather than to individual routes; we attribute the hook coverage to the
// service mount path so every verb at that path inherits it. Keyed by the
// service mount prefix (the collection path, e.g. "/messages").
func indexFeathersHookMiddleware(content, file string) map[string][]AuthSignal {
	out := map[string][]AuthSignal{}
	if !strings.Contains(content, ".hooks(") {
		return out
	}
	// Map service variable / mount path → hook signals. A pragmatic, file-scope
	// model: any service hook block in the file contributes to every service
	// mount in that file (Feathers apps conventionally register one service +
	// its hooks per module). We key on the mount paths discovered by the route
	// synthesizer's feathersUseRe so attribution stays endpoint-precise.
	var hookSigs []AuthSignal
	for _, m := range jsFeathersServiceHooksRe.FindAllStringSubmatchIndex(content, -1) {
		body := feathersHookBody(content, m[1]-1)
		line := lineAtOffset(content, m[0])
		for _, hm := range jsFeathersHookRe.FindAllStringSubmatch(body, -1) {
			hookSigs = append(hookSigs, AuthSignal{
				Kind: "hook",
				Text: "feathers-hook: " + hm[1],
				File: file,
				Line: line,
			})
		}
	}
	if len(hookSigs) == 0 {
		return out
	}
	hookSigs = dedupeSignals(hookSigs)
	for _, m := range feathersUseRe.FindAllStringSubmatch(content, -1) {
		if len(m) < 3 {
			continue
		}
		mount := strings.TrimRight(m[1], "/")
		if mount == "" {
			mount = "/"
		}
		canonical := httproutes.Canonicalize(httproutes.FrameworkFeathers, mount)
		out[canonical] = append(out[canonical], hookSigs...)
	}
	return out
}

// jsFeathersServiceHooksRe captures a `.hooks(` call opener so the object body
// can be scanned for before/after/error timing keys.
var jsFeathersServiceHooksRe = regexp.MustCompile(`\.hooks\s*\(`)

// feathersHookBody returns the argument span following a `.hooks(` opener.
func feathersHookBody(content string, parenOpen int) string {
	if parenOpen < 0 || parenOpen >= len(content) || content[parenOpen] != '(' {
		return ""
	}
	closeIdx := findMatchingParenFrom(content, parenOpen)
	if closeIdx < 0 {
		return ""
	}
	return content[parenOpen : closeIdx+1]
}

// feathersMountOf returns the collection mount prefix for a canonical endpoint
// path, dropping a trailing `/{id}` member segment so both the collection and
// member verbs map to the same service-hook key.
func feathersMountOf(canonical string) string {
	if strings.HasSuffix(canonical, "/{id}") {
		return strings.TrimSuffix(canonical, "/{id}")
	}
	return canonical
}

// ---------------------------------------------------------------------------
// Sails config/http.js middleware-order recognition (#2853 — framework_specific
// idiom)
// ---------------------------------------------------------------------------
//
// Sails does NOT chain middleware onto individual routes. Its global middleware
// pipeline is declared once in `config/http.js` as the `order` array under the
// `middleware` key. This is a bespoke, declarative middleware surface that the
// generic per-route / app.use recognisers do not capture, so the Sails
// middleware_coverage cell is classified `framework_specific` in the registry.
// The recogniser below proves the capability: it parses the http.js order array
// into the ordered list of named middleware that wrap every request.

// sailsMiddlewareOrderRe captures the `order: [ ... ]` array under the Sails
// `middleware` config key. Group 1 = the raw array body.
var sailsMiddlewareOrderRe = regexp.MustCompile(`(?s)\border\s*:\s*\[([^\]]*)\]`)

// SailsMiddlewareOrder is the parsed result of a Sails config/http.js
// middleware-order declaration.
type SailsMiddlewareOrder struct {
	// Order is the named middleware pipeline in declaration order.
	Order []string
	// File is the source path the order was parsed from.
	File string
}

// sailsHTTPConfigFile reports whether filePath is a Sails http-config file.
func sailsHTTPConfigFile(filePath string) bool {
	p := strings.ReplaceAll(filePath, "\\", "/")
	return strings.HasSuffix(p, "config/http.js") ||
		strings.HasSuffix(p, "config/http.ts")
}

// ParseSailsMiddlewareOrder parses a Sails config/http.js middleware `order`
// array into its named middleware pipeline. Reports false when the file has no
// recognisable middleware order.
func ParseSailsMiddlewareOrder(content, file string) (SailsMiddlewareOrder, bool) {
	if !strings.Contains(content, "middleware") {
		return SailsMiddlewareOrder{}, false
	}
	m := sailsMiddlewareOrderRe.FindStringSubmatch(content)
	if m == nil {
		return SailsMiddlewareOrder{}, false
	}
	var order []string
	for _, q := range jsQuotedTokenRe.FindAllStringSubmatch(m[1], -1) {
		if tok := strings.TrimSpace(q[1]); tok != "" {
			order = append(order, tok)
		}
	}
	if len(order) == 0 {
		return SailsMiddlewareOrder{}, false
	}
	return SailsMiddlewareOrder{Order: order, File: file}, true
}
