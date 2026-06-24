// Cross-framework auth_coverage for the JS/TS backend-HTTP frameworks (#2852).
//
// This pass resolves a structured `auth_policy` for every synthetic
// http_endpoint_definition emitted by the JS/TS backend synthesizers
// (#2851 + the pre-existing Express/Koa/Fastify/Hono/Nest passes). It mirrors
// the Java auth_policy resolver (java_auth_policy.go, #1942) and the Django
// DRF class-level approach (#2816): combine route-level, router/app-level and
// (Nest) class-level signals into a per-endpoint posture, with honest
// precedence and confidence.
//
// The analog of Django's "class-level + decorator + default permissions" for
// JS/TS backends is:
//
//   - route-level middleware/guard — the strongest signal: an auth middleware
//     passed directly to the route registration, e.g.
//     `app.get('/me', requireAuth, handler)` or Nest `@UseGuards(AuthGuard)`
//     on the handler method.
//   - router/app-level middleware — `app.use(passport.authenticate('jwt'))`,
//     `router.use(requireAuth)`, a Nest `@UseGuards(...)` on the controller
//     class, or a Hapi server-default auth strategy. Applies to every endpoint
//     registered after it in the same file (router/app scope).
//   - framework auth config — Hapi route `options.auth` / `config.auth`,
//     AdonisJS `.middleware('auth')` route chains, Sails policy maps.
//
// The recognised auth-middleware vocabulary is deliberately cross-framework
// (passport, express-jwt, express-session, the Nest @nestjs/passport guards,
// Hapi auth strategies, and the common hand-rolled `requireAuth` /
// `isAuthenticated` / `ensureLoggedIn` / `authenticate` idioms) so a single
// recogniser greens all twelve framework families.
//
// Output: the same property contract the Java resolver writes, so the
// grafel_auth_coverage MCP tool (auth_coverage.go signal 1) and the
// security dashboard light up uniformly across languages:
//
//	auth_policy     — JSON-encoded AuthPolicy (source chain for the dashboard)
//	auth_method     — "middleware" | "guard" | "config" | "framework_default" | "unknown"
//	auth_confidence — "high" | "medium" | "low"
//	auth_required   — "true" | "false" (omitted when method=="unknown")
//	auth_middleware — the recognised middleware symbol (MCP signal-1 key)
//	auth_guard      — the recognised Nest guard symbol (MCP signal-1 key)
//
// Refs #2852.
package engine

import (
	"regexp"
	"sort"
	"strings"

	"github.com/cajasmota/grafel/internal/engine/httproutes"
	"github.com/cajasmota/grafel/internal/types"
)

// jsAuthMiddlewareNames is the cross-framework vocabulary of auth/authz
// middleware and helper names. Matching is case-insensitive on the bare
// symbol (the trailing call args, if any, are ignored). The list covers the
// well-known libraries plus the conventional hand-rolled guard names that
// recur across Express/Koa/Fastify/Hono/Adonis/Restify/Polka apps.
var jsAuthMiddlewareNames = map[string]bool{
	// passport.js — `passport.authenticate('jwt')`, `passport.authorize(...)`.
	"passport.authenticate": true,
	"passport.authorize":    true,
	// express-jwt — `expressjwt({...})` / `jwt({...})` (the express-jwt v7
	// default export is conventionally imported as `expressjwt` or `jwt`).
	"expressjwt":     true,
	"expressjwt.jwt": true,
	// express-session / cookie-session gate — session presence is a weak but
	// real auth signal when paired with a guard; we treat the explicit guard
	// names below as the decisive ones.
	"ensureauthenticated":   true,
	"ensureloggedin":        true,
	"isauthenticated":       true,
	"requireauth":           true,
	"requireauthentication": true,
	"requirelogin":          true,
	"requireuser":           true,
	"authenticate":          true,
	"authrequired":          true,
	"authmiddleware":        true,
	"authguard":             true,
	"checkauth":             true,
	"checkjwt":              true,
	"verifytoken":           true,
	"verifyjwt":             true,
	"jwtauth":               true,
	"protect":               true,
	"protectroute":          true,
	"authorized":            true,
	"loginrequired":         true,
	"mustbeauthenticated":   true,
}

// jsAuthMiddlewareSubstrings catches qualified / namespaced auth helpers whose
// exact symbol varies but whose intent is unambiguous, e.g.
// `auth.required`, `authMiddleware.verify`, `guards.jwt`. Matched as a
// lowercase substring of the bare receiver+method symbol. Kept narrow to avoid
// false positives on incidental identifiers.
var jsAuthMiddlewareSubstrings = []string{
	"passport.authenticate",
	"passport.authorize",
}

// jsAuthGuardDecoratorRe captures NestJS `@UseGuards(AuthGuard)` /
// `@UseGuards(AuthGuard('jwt'), RolesGuard)`. Group 1 = the raw argument list.
var jsAuthGuardDecoratorRe = regexp.MustCompile(`@UseGuards\s*\(([^)]*)\)`)

// jsAuthGuardArgRe extracts each guard identifier from a @UseGuards argument
// list, handling both `AuthGuard` and the factory form `AuthGuard('jwt')`.
var jsAuthGuardArgRe = regexp.MustCompile(`([A-Za-z_$][\w$]*)\s*(?:\([^)]*\))?`)

// jsRolesDecoratorRe captures NestJS `@Roles('admin', 'user')` /
// `@Roles(Role.Admin)` role declarations. Group 1 = the raw argument list.
var jsRolesDecoratorRe = regexp.MustCompile(`@Roles\s*\(([^)]*)\)`)

// jsPermissionsDecoratorRe captures NestJS fine-grained permission decorators —
// `@RequirePermissions('user:delete')` (nest-access-control / casl convention),
// `@Permissions('orders.read')`, `@CheckPermissions(...)`. Group 1 = the raw
// argument list. The leading word boundary keeps it from matching a longer
// identifier suffix.
var jsPermissionsDecoratorRe = regexp.MustCompile(`@(?:RequirePermissions|Permissions|CheckPermissions|RequirePermission)\s*\(([^)]*)\)`)

// jsScopesDecoratorRe captures NestJS OAuth scope decorators —
// `@Scopes('write:users')` / `@RequireScopes('read')`. Group 1 = the raw args.
var jsScopesDecoratorRe = regexp.MustCompile(`@(?:Scopes|RequireScopes|RequireScope)\s*\(([^)]*)\)`)

// jsQuotedTokenRe pulls single- or double-quoted tokens out of an argument
// list (the Java extractQuotedTokens helper is double-quote only; JS/TS role
// strings are conventionally single-quoted).
var jsQuotedTokenRe = regexp.MustCompile(`['"` + "`" + `]([^'"` + "`" + `]+)['"` + "`" + `]`)

// jsAppUseRe captures `app.use(...)` / `router.use(...)` / `server.use(...)`
// registrations. Group 1 = receiver, group 2 = the first argument (up to a
// comma or close paren). App/router-level auth middleware applies to every
// endpoint registered in the same file/router scope.
var jsAppUseRe = regexp.MustCompile(
	`\b(app|router|server|fastify|api|r|v\d+)\.(?:use|register|addHook|decorate)\s*\(\s*([^,)\r\n]+)`,
)

// jsRouteRegistrationRe captures an Express-shaped route registration so we can
// inspect its middleware chain (everything between the path and the final
// handler). Group 1 = receiver, 2 = verb, 3 = path, 4 = the remaining args
// (middleware chain + handler).
var jsRouteRegistrationRe = regexp.MustCompile(
	`\b([$\w][\w$]*)\.(get|post|put|patch|delete|del|all|head|options|opts)\s*\(` +
		`\s*['"` + "`" + `]([^'"` + "`" + `\n\r]+)['"` + "`" + `]\s*,\s*([^\r\n]*)`,
)

// jsHapiAuthKwargRe captures a Hapi route `options.auth` / `config.auth` /
// top-level `auth:` setting. Group 1 = the auth value (strategy name, `false`,
// or an object). `auth: false` is an explicit public marker; any other value
// names a strategy and means the route is protected.
var jsHapiAuthKwargRe = regexp.MustCompile(
	`\bauth\s*:\s*(false|true|\{[^}]*\}|['"` + "`" + `][^'"` + "`" + `]+['"` + "`" + `]|[A-Za-z_$][\w$.]*)`,
)

// jsAdonisMiddlewareRe captures an AdonisJS route-chain middleware call
// `.middleware('auth')` / `.middleware(['auth', 'acl:admin'])` /
// `.use([middleware.auth()])` (Adonis 6). Group 1 = the raw argument list.
var jsAdonisMiddlewareRe = regexp.MustCompile(
	`\.(?:middleware|use)\s*\(\s*(\[[^\]]*\]|['"` + "`" + `][^'"` + "`" + `]+['"` + "`" + `]|[^)]*)\)`,
)

// jsClassDeclRe matches a Nest controller class declaration so a directly
// preceding @UseGuards decorator block can be classified as class-level.
var jsClassDeclRe = regexp.MustCompile(`(?m)^[ \t]*(?:export\s+)?(?:abstract\s+)?class\s+[A-Za-z_$][\w$]*`)

// jsMarbleAuthRe captures a Marble.js auth middleware in an effect pipe, e.g.
// `authorize$`, `requireAuth$`, `use(authorize$)`. Marble auth middlewares are
// conventionally suffixed `$` (they are RxJS-operator effects). Group 1 = name.
var jsMarbleAuthRe = regexp.MustCompile(`\b((?:authorize|requireAuth|auth|isAuthenticated)\$)`)

// normalizeAuthSymbol lowercases and trims a candidate middleware symbol,
// stripping any trailing call arguments so `passport.authenticate('jwt')`
// reduces to `passport.authenticate`.
func normalizeAuthSymbol(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '('); i >= 0 {
		s = s[:i]
	}
	s = strings.TrimSpace(s)
	return strings.ToLower(s)
}

// recognizeAuthMiddleware reports whether `arg` names a recognised auth
// middleware and returns the canonical (original-case) symbol for evidence.
func recognizeAuthMiddleware(arg string) (string, bool) {
	raw := strings.TrimSpace(arg)
	norm := normalizeAuthSymbol(raw)
	if norm == "" {
		return "", false
	}
	if jsAuthMiddlewareNames[norm] {
		return symbolHead(raw), true
	}
	for _, sub := range jsAuthMiddlewareSubstrings {
		if strings.Contains(norm, sub) {
			return symbolHead(raw), true
		}
	}
	// Bare identifier ending in a guard-ish token, e.g. `requireJwtAuth`,
	// `verifyAccessToken`, `ensureSignedIn` — recognise the common compound
	// patterns without exploding the explicit table.
	if jsAuthCompoundRe.MatchString(norm) {
		return symbolHead(raw), true
	}
	return "", false
}

// jsAuthCompoundRe matches compound hand-rolled guard identifiers that combine
// a verb (require/ensure/verify/check/assert/is) with an auth noun
// (auth/authenticated/login/loggedin/jwt/token/session/user). This catches the
// long tail of project-local guard names without enumerating every spelling.
var jsAuthCompoundRe = regexp.MustCompile(
	`^(?:require|ensure|verify|check|assert|is|must|need)[a-z]*` +
		`(?:auth|authenticated|login|loggedin|signedin|jwt|token|session|user|admin|role|permission)`,
)

// symbolHead returns the first whitespace-delimited token of a candidate arg,
// preserving original case (used as evidence text).
func symbolHead(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexAny(s, " \t(,)"); i >= 0 {
		s = s[:i]
	}
	return s
}

// jsAuthContext is the per-file resolved auth state, computed once per file and
// then projected onto each synthetic endpoint in that file.
type jsAuthContext struct {
	// appLevel is the set of router/app-level middlewares (file scope). Any
	// non-empty entry means every endpoint in the file inherits coverage at
	// MEDIUM confidence unless a route-level signal overrides.
	appLevel []AuthSignal
	// classGuards maps a 0-based byte offset (the @UseGuards site) so an
	// endpoint can inherit its enclosing Nest controller class guard. We model
	// this coarsely as "any class-level guard in the file" → file-scope medium
	// coverage, plus the precise route-level guard below for high confidence.
	classGuards []AuthSignal
	// hapiServerDefaultAuth is true when the file sets a Hapi server-wide
	// default auth strategy (`server.auth.default(...)` / `auth.strategy` +
	// default). Endpoints inherit it unless they opt out with `auth: false`.
	hapiServerDefaultAuth bool
	hapiServerDefaultLine int
	// classMetaAuth is the controller class-level metadata-decorator posture
	// (@RequirePage / @Authenticated / @Public ... on the @Controller class).
	// It applies to every method without its own override (deploy-9). nestMetaNone
	// when no controller in the file carries a class-level metadata decorator.
	classMetaAuth     nestMetaAuthResult
	classMetaAuthLine int
	file              string
}

// resolveJSTSFileAuth scans a file once and returns its app/router-level and
// class-level auth signals (the cross-endpoint context).
func resolveJSTSFileAuth(content, file string) jsAuthContext {
	ctx := jsAuthContext{file: file}

	// App/router-level middleware: app.use(passport.authenticate('jwt')).
	for _, m := range jsAppUseRe.FindAllStringSubmatchIndex(content, -1) {
		arg := content[m[4]:m[5]]
		if sym, ok := recognizeAuthMiddleware(arg); ok {
			ctx.appLevel = append(ctx.appLevel, AuthSignal{
				Kind: "middleware",
				Text: "app/router-level: " + sym,
				File: file,
				Line: lineAtOffset(content, m[0]),
			})
		}
	}

	// Nest class-level @UseGuards on the controller — only the @UseGuards whose
	// decorator block sits directly above a `class` declaration (not a method),
	// so method-level guards don't leak into the file-scope inheritance set.
	for _, m := range jsClassDeclRe.FindAllStringSubmatchIndex(content, -1) {
		block := precedingDecoratorBlock(content, m[0])
		if block == "" {
			continue
		}
		gm := jsAuthGuardDecoratorRe.FindStringSubmatch(block)
		if gm == nil || len(parseGuardArgs(gm[1])) == 0 {
			continue
		}
		ctext := "class @UseGuards(" + strings.TrimSpace(gm[1]) + ")"
		if rm := jsRolesDecoratorRe.FindStringSubmatch(block); rm != nil {
			ctext += " @Roles(" + strings.TrimSpace(rm[1]) + ")"
		}
		if pm := jsPermissionsDecoratorRe.FindStringSubmatch(block); pm != nil {
			ctext += " @RequirePermissions(" + strings.TrimSpace(pm[1]) + ")"
		}
		if sm := jsScopesDecoratorRe.FindStringSubmatch(block); sm != nil {
			ctext += " @Scopes(" + strings.TrimSpace(sm[1]) + ")"
		}
		ctx.classGuards = append(ctx.classGuards, AuthSignal{
			Kind: "guard",
			Text: ctext,
			File: file,
			Line: lineAtOffset(content, m[0]),
		})
	}

	// Nest class-level metadata-decorator posture (deploy-9): @RequirePage /
	// @Authenticated / @Public ... on the @Controller class. Applies to every
	// method that lacks its own metadata override.
	if cls := resolveNestClassMetadataAuth(content); cls.kind != nestMetaNone {
		ctx.classMetaAuth = cls
		ctx.classMetaAuthLine = nestFirstControllerLine(content)
	}

	// Hapi server-wide default auth strategy.
	if strings.Contains(content, "auth.default") || strings.Contains(content, "auth.strategy") {
		if idx := strings.Index(content, "auth.default"); idx >= 0 {
			ctx.hapiServerDefaultAuth = true
			ctx.hapiServerDefaultLine = lineAtOffset(content, idx)
		}
	}

	return ctx
}

// parseGuardArgs extracts guard identifiers from a @UseGuards argument list.
func parseGuardArgs(args string) []string {
	var out []string
	for _, m := range jsAuthGuardArgRe.FindAllStringSubmatch(args, -1) {
		name := strings.TrimSpace(m[1])
		if name == "" {
			continue
		}
		out = append(out, name)
	}
	return out
}

// lineAtOffset returns the 1-based line number of byte offset off in content.
func lineAtOffset(content string, off int) int {
	if off < 0 || off > len(content) {
		return 0
	}
	return strings.Count(content[:off], "\n") + 1
}

// applyJSTSAuthPolicy resolves and stamps an auth_policy on every JS/TS
// synthetic backend endpoint emitted for this file. It mutates the Properties
// map in place and never adds or removes entities, so it cannot regress the
// surrounding synthesis pass.
//
// `before` is the entity-slice length captured before the JS/TS synthesizers
// ran; only entities at index >= before that belong to this file are
// considered (the producer-side synthetics this file just emitted).
func applyJSTSAuthPolicy(content, path string, entities []types.EntityRecord, before int) {
	if len(content) == 0 || before >= len(entities) {
		return
	}
	ctx := resolveJSTSFileAuth(content, path)

	// Index route-level middleware chains by (verb, canonical path) so each
	// endpoint can look up its own registration site.
	routeAuth := indexRouteLevelAuth(content, path)
	// Index Nest method-level guards by handler symbol (the decorator sits
	// immediately above the @Get/@Post method; we attribute by nearest method).
	methodGuards := indexNestMethodGuards(content, path)
	// Index Nest method-level metadata-decorator auth (@RequirePage / @Public /
	// @Authenticated ...) by handler method symbol (deploy-9).
	methodMetaAuth := indexNestMethodMetadataAuth(content)
	// Index Hapi per-route auth from the route object bodies.
	hapiRouteAuth := indexHapiRouteAuth(content, path)
	// Index Adonis route-chain middleware by canonical path.
	adonisRouteAuth := indexAdonisRouteAuth(content, path)
	// Index Marble.js per-effect auth middleware by canonical path.
	marbleRouteAuth := indexMarbleRouteAuth(content, path)
	// #5499 — index home-rolled / custom auth checks in handler bodies
	// (`const u = await requireUser()`, `if (!session) redirect('/login')`).
	// Keyed by handler name and by handler start line.
	authBodyByName, authBodyByLine := indexAuthBodyChecks(content)

	for i := before; i < len(entities); i++ {
		e := &entities[i]
		if e.Kind != httpEndpointDefinitionKind || e.SourceFile != path {
			continue
		}
		if e.Properties == nil {
			continue
		}
		// #4041 — tRPC procedures already had their auth resolved by
		// applyTRPCAuthBinding (transport-agnostic middleware-in-a-builder).
		// This route/decorator-keyed resolver cannot improve on that and would
		// otherwise overwrite auth_method=trpc_middleware with the no-signal
		// "unknown" fall-through. Leave tRPC synthetics untouched.
		if e.Properties["framework"] == "trpc" {
			continue
		}
		framework := e.Properties["framework"]
		verb := strings.ToUpper(e.Properties["verb"])
		canonical := e.Properties["path"]
		key := verb + " " + canonical

		bodyPolicy, hasBody := authBodyEndpointKey(e, authBodyByName, authBodyByLine, path)
		policy := resolveEndpointAuth(
			framework, key, e.Properties["source_handler"],
			ctx, routeAuth, methodGuards, methodMetaAuth, hapiRouteAuth, adonisRouteAuth, marbleRouteAuth,
			bodyPolicy, hasBody,
		)
		stampAuthPolicy(e.Properties, policy)
	}
}

// resolveEndpointAuth combines the per-endpoint and per-file signals into a
// final AuthPolicy with honest precedence (highest first):
//
//  1. route-level middleware / Nest method @UseGuards / Hapi route auth /
//     Adonis route .middleware('auth') — HIGH.
//  2. Hapi explicit `auth: false` — HIGH public.
//  3. router/app-level middleware / Nest class @UseGuards / Hapi server default
//     — MEDIUM (file scope).
//  4. unknown — no signal.
func resolveEndpointAuth(
	framework, key, sourceHandler string,
	ctx jsAuthContext,
	routeAuth map[string][]AuthSignal,
	methodGuards map[string][]AuthSignal,
	methodMetaAuth map[string]nestMetaAuthResult,
	hapiRouteAuth map[string]hapiAuth,
	adonisRouteAuth map[string][]AuthSignal,
	marbleRouteAuth map[string][]AuthSignal,
	bodyPolicy AuthPolicy, hasBody bool,
) AuthPolicy {
	// 1a. Express-shaped route-level middleware chain.
	if sigs, ok := routeAuth[key]; ok && len(sigs) > 0 {
		return AuthPolicy{
			Required: true, Method: "middleware", Confidence: "high",
			Permissions: permsFromSignals(sigs),
			Scopes:      scopesFromSignals(sigs),
			SourceChain: sigs,
		}
	}
	// 1b. Nest method-level guard (attributed by handler method symbol).
	if handler := nestHandlerName(sourceHandler); handler != "" {
		if sigs, ok := methodGuards[handler]; ok && len(sigs) > 0 {
			return AuthPolicy{
				Required: true, Method: "guard", Confidence: "high",
				Roles:       rolesFromSignals(sigs),
				Permissions: permsFromSignals(sigs),
				Scopes:      scopesFromSignals(sigs),
				SourceChain: sigs,
			}
		}
		// 1b'. Nest method-level metadata decorator (@RequirePage / @Public /
		// @Authenticated ...). A method-level verdict — protective OR explicitly
		// public — overrides the class-level default, mirroring NestJS
		// `Reflector.getAllAndOverride([handler, class])` precedence (deploy-9).
		if res, ok := methodMetaAuth[handler]; ok && res.kind != nestMetaNone {
			return nestMetaPolicy(res, "high", ctx.file, 0)
		}
	}
	// 1c. Hapi per-route auth.
	if ha, ok := hapiRouteAuth[key]; ok && ha.set {
		if ha.public {
			return AuthPolicy{
				Required: false, Method: "config", Confidence: "high",
				SourceChain: []AuthSignal{ha.signal},
			}
		}
		return AuthPolicy{
			Required: true, Method: "config", Confidence: "high",
			SourceChain: []AuthSignal{ha.signal},
		}
	}
	// 1d. Adonis route-chain middleware.
	if sigs, ok := adonisRouteAuth[key]; ok && len(sigs) > 0 {
		return AuthPolicy{
			Required: true, Method: "middleware", Confidence: "high",
			SourceChain: sigs,
		}
	}
	// 1e. Marble.js per-effect auth middleware.
	if sigs, ok := marbleRouteAuth[key]; ok && len(sigs) > 0 {
		return AuthPolicy{
			Required: true, Method: "middleware", Confidence: "high",
			SourceChain: sigs,
		}
	}

	// 2. Home-rolled / custom auth check in the handler body (#5499) —
	//    `const u = await requireUser()` / `if (!session) redirect('/login')`.
	//    The check is IN the handler (route-direct), so it ranks above the
	//    file-scope inherited signals below, but below an explicit route
	//    middleware / method decorator above (those are the declared gate).
	if hasBody {
		return bodyPolicy
	}

	// 3. File-scope signals (router/app-level middleware, Nest class guards,
	//    Hapi server default). MEDIUM confidence — the endpoint inherits the
	//    posture but the binding is file-scoped rather than route-direct.
	if len(ctx.appLevel) > 0 {
		return AuthPolicy{
			Required: true, Method: "middleware", Confidence: "medium",
			SourceChain: ctx.appLevel,
		}
	}
	if len(ctx.classGuards) > 0 {
		return AuthPolicy{
			Required: true, Method: "guard", Confidence: "medium",
			Roles:       rolesFromSignals(ctx.classGuards),
			Permissions: permsFromSignals(ctx.classGuards),
			Scopes:      scopesFromSignals(ctx.classGuards),
			SourceChain: ctx.classGuards,
		}
	}
	// 3'. Nest class-level metadata decorator (@RequirePage / @Public /
	// @Authenticated ... on the @Controller class). Applies to every method
	// without its own override — MEDIUM confidence (class scope). A class-level
	// @Public is an explicit public verdict for the whole controller (deploy-9).
	if framework == "nestjs" && ctx.classMetaAuth.kind != nestMetaNone {
		return nestMetaPolicy(ctx.classMetaAuth, "medium", ctx.file, ctx.classMetaAuthLine)
	}
	if ctx.hapiServerDefaultAuth && framework == "hapi" {
		return AuthPolicy{
			Required: true, Method: "framework_default", Confidence: "low",
			SourceChain: []AuthSignal{{
				Kind: "framework_default",
				Text: "hapi server.auth.default strategy; routes require auth unless auth:false",
				File: ctx.file, Line: ctx.hapiServerDefaultLine,
			}},
		}
	}

	// 4. No signal.
	return AuthPolicy{Method: "unknown", Confidence: "low"}
}

// nestHandlerName extracts the bare method name from a Nest source_handler ref
// such as "SCOPE.Operation:UsersController.findOne" → "findOne", or
// "Controller:findOne" → "findOne".
func nestHandlerName(ref string) string {
	if ref == "" {
		return ""
	}
	if i := strings.IndexByte(ref, ':'); i >= 0 {
		ref = ref[i+1:]
	}
	if i := strings.LastIndexByte(ref, '.'); i >= 0 {
		ref = ref[i+1:]
	}
	return strings.TrimSpace(ref)
}

// rolesFromSignals scans @Roles decorators captured alongside guard signals.
// (Roles are merged into the guard source chain text by the indexers.)
func rolesFromSignals(sigs []AuthSignal) []string {
	return jsTokensFromSignals(sigs, "@Roles(")
}

// permsFromSignals scans @RequirePermissions decorators merged into a guard
// signal's source-chain text and returns the specific required permissions.
func permsFromSignals(sigs []AuthSignal) []string {
	return jsTokensFromSignals(sigs, "@RequirePermissions(")
}

// scopesFromSignals scans @Scopes decorators merged into a guard signal's
// source-chain text and returns the specific required OAuth scopes.
func scopesFromSignals(sigs []AuthSignal) []string {
	return jsTokensFromSignals(sigs, "@Scopes(")
}

// jsTokensFromSignals extracts the quoted string tokens from a `marker(...)`
// decorator embedded in any signal's Text. Returns nil when the marker is
// absent or carries only non-literal args (e.g. `@Roles(roleEnumVar)`), so a
// dynamic decorator never fabricates a token.
func jsTokensFromSignals(sigs []AuthSignal, marker string) []string {
	var out []string
	for _, s := range sigs {
		if i := strings.Index(s.Text, marker); i >= 0 {
			inner := s.Text[i+len(marker):]
			if j := strings.IndexByte(inner, ')'); j >= 0 {
				inner = inner[:j]
			}
			for _, q := range jsQuotedTokenRe.FindAllStringSubmatch(inner, -1) {
				if tok := strings.TrimSpace(q[1]); tok != "" {
					out = append(out, tok)
				}
			}
		}
	}
	return out
}

// stampAuthPolicy writes the resolved policy onto an endpoint Properties map
// using the same contract as the Java resolver (java_annotation_routes.go).
func stampAuthPolicy(props map[string]string, policy AuthPolicy) {
	if policyJSON := EncodeAuthPolicy(policy); policyJSON != "" {
		props["auth_policy"] = policyJSON
	}
	props["auth_method"] = policy.Method
	props["auth_confidence"] = policy.Confidence
	if policy.Required {
		props["auth_required"] = "true"
	} else if policy.Method != "unknown" {
		props["auth_required"] = "false"
	}
	if len(policy.Roles) > 0 {
		props["auth_roles"] = strings.Join(policy.Roles, ",")
	}
	// #authz — the specific fine-grained permission / scope required by the
	// endpoint (Nest @RequirePermissions / @Scopes), so grafel_auth_coverage
	// answers "what permission does this route require?".
	if len(policy.Permissions) > 0 {
		perms := append([]string(nil), policy.Permissions...)
		sort.Strings(perms)
		props["auth_permissions"] = strings.Join(perms, ",")
	}
	if len(policy.Scopes) > 0 {
		scs := append([]string(nil), policy.Scopes...)
		sort.Strings(scs)
		props["auth_scopes"] = strings.Join(scs, ",")
	}
	// MCP signal-1 keys (auth_coverage.go): a single recognised middleware /
	// guard symbol so the tool's cheap property check fires without parsing
	// the JSON source chain. Only stamp when the endpoint is protected.
	if policy.Required && len(policy.SourceChain) > 0 {
		head := policy.SourceChain[0]
		switch policy.Method {
		case "guard":
			props["auth_guard"] = authEvidenceSymbol(head.Text)
		case "check":
			// #5499 — a home-rolled body check. Surface the check-callee symbol
			// under auth_guard (the MCP signal-1 key) so grafel_auth_coverage
			// fires without parsing the JSON source chain.
			props["auth_guard"] = authBodyEvidenceSymbol(head.Text)
		case "middleware", "config", "framework_default":
			props["auth_middleware"] = authEvidenceSymbol(head.Text)
		}
	}
	// #deploy-9 — surface the NestJS metadata-decorator permission page(s) under
	// a dedicated key so grafel_auth_coverage answers "what page does this
	// route require?" without re-deriving it from auth_permissions.
	if policy.Required && len(policy.Permissions) > 0 {
		pages := append([]string(nil), policy.Permissions...)
		sort.Strings(pages)
		props["auth_page"] = strings.Join(pages, ",")
	}
}

// authEvidenceSymbol returns a compact symbol for the MCP signal-1 property,
// stripping the "app/router-level: " prefix and decorator wrapping.
func authEvidenceSymbol(text string) string {
	text = strings.TrimPrefix(text, "app/router-level: ")
	text = strings.TrimPrefix(text, "route-level: ")
	return text
}

// ---------------------------------------------------------------------------
// Route-level middleware indexers
// ---------------------------------------------------------------------------

// indexRouteLevelAuth scans Express-shaped route registrations for auth
// middleware in the route's middleware chain. Covers Express, Koa-Router,
// Hono, Fastify (via the same receiver/verb shape), Polka and Restify. The
// returned map is keyed by "<VERB> <canonical-path>" matching the synthetic
// endpoint property contract.
func indexRouteLevelAuth(content, _ string) map[string][]AuthSignal {
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
		sym, ok := middlewareChainHasAuth(rest)
		if !ok {
			continue
		}
		// Express-family canonicalization (Koa/Hono/Polka/Restify share it).
		canonical := httproutes.Canonicalize(httproutes.FrameworkExpress, raw)
		if canonical == "" {
			continue
		}
		key := verb + " " + canonical
		text := "route-level: " + sym
		// Capture a parameterised authz middleware's literal grant —
		// `requireScope('write:users')` → scope, `checkPermission('x')` →
		// permission — by appending a marker the stamp step parses.
		if perms, scopes := jsMiddlewareGrants(rest); len(perms) > 0 || len(scopes) > 0 {
			if len(perms) > 0 {
				text += " @RequirePermissions(" + strings.Join(jsQuoteAll(perms), ",") + ")"
			}
			if len(scopes) > 0 {
				text += " @Scopes(" + strings.Join(jsQuoteAll(scopes), ",") + ")"
			}
		}
		out[key] = append(out[key], AuthSignal{
			Kind: "middleware",
			Text: text,
			File: "",
			Line: lineAtOffset(content, m[0]),
		})
	}
	return out
}

// jsScopeMiddlewareRe matches a scope-gating middleware call carrying a literal
// scope, e.g. `requireScope('write:users')` / `hasScope("read")` /
// `checkScope('admin')`. Group 1 = the raw argument list.
var jsScopeMiddlewareRe = regexp.MustCompile(`(?i)\b(?:require|has|check|need|assert)scopes?\s*\(([^)]*)\)`)

// jsPermMiddlewareRe matches a permission-gating middleware call carrying a
// literal permission, e.g. `checkPermission('users:delete')` /
// `requirePermission("edit")` / `can('delete','User')` (casl). Group 1 = args.
var jsPermMiddlewareRe = regexp.MustCompile(`(?i)\b(?:check|require|has|need|assert)permissions?\s*\(([^)]*)\)`)

// jsCaslCanRe matches a casl ability check `can('delete', 'User')`. Group 1 =
// the action (the permission verb). The subject (group-2-ish) is left out — the
// permission is the action.
var jsCaslCanRe = regexp.MustCompile(`\bcan\s*\(\s*['"` + "`" + `]([^'"` + "`" + `]+)['"` + "`" + `]`)

// jsMiddlewareGrants scans an Express route middleware chain for parameterised
// authz middlewares and returns the literal permissions and scopes they
// require. Dynamic args (no string literal) yield nothing — never fabricated.
func jsMiddlewareGrants(rest string) (perms, scopes []string) {
	for _, m := range jsScopeMiddlewareRe.FindAllStringSubmatch(rest, -1) {
		for _, q := range jsQuotedTokenRe.FindAllStringSubmatch(m[1], -1) {
			if tok := strings.TrimSpace(q[1]); tok != "" {
				scopes = append(scopes, tok)
			}
		}
	}
	for _, m := range jsPermMiddlewareRe.FindAllStringSubmatch(rest, -1) {
		for _, q := range jsQuotedTokenRe.FindAllStringSubmatch(m[1], -1) {
			if tok := strings.TrimSpace(q[1]); tok != "" {
				perms = append(perms, tok)
			}
		}
	}
	for _, m := range jsCaslCanRe.FindAllStringSubmatch(rest, -1) {
		if tok := strings.TrimSpace(m[1]); tok != "" {
			perms = append(perms, tok)
		}
	}
	return perms, scopes
}

// jsQuoteAll wraps each token in single quotes so the merged decorator-marker
// text is parsed back by jsTokensFromSignals (which expects quoted literals).
func jsQuoteAll(toks []string) []string {
	out := make([]string, len(toks))
	for i, t := range toks {
		out[i] = "'" + t + "'"
	}
	return out
}

// middlewareChainHasAuth scans the args following the path (the middleware
// chain plus final handler) for a recognised auth middleware. Returns the
// canonical symbol and true on the first match.
func middlewareChainHasAuth(rest string) (string, bool) {
	// Split on commas at depth 0 to isolate each chain argument.
	for _, arg := range splitTopLevelArgs(rest) {
		if sym, ok := recognizeAuthMiddleware(arg); ok {
			return sym, true
		}
	}
	return "", false
}

// splitTopLevelArgs (defined in http_endpoint_client_synthesis.go) splits a
// comma-separated argument list respecting nesting and quoting; reused here to
// isolate each middleware-chain argument.

// ---------------------------------------------------------------------------
// NestJS method-level guard indexer
// ---------------------------------------------------------------------------

// nestHandlerMethodRe matches a Nest handler method declaration. Group 1 = the
// method name. The `(?m)^` anchor keeps it to a declaration line, not a nested
// call.
//
// The parameter list must tolerate NESTED parentheses, because real NestJS
// handlers decorate their params — `findOne(@Param('id', ParseIntPipe) id: number)`,
// `list(@Query() q: Dto)`. A `\([^)]*\)` body stops at the FIRST inner `)` (the
// one closing `@Query(`) and then fails to reach `)[:{]`, so EVERY handler with a
// decorated/parenthesised param was silently skipped — which is exactly why
// handler-level guards weren't resolved (#handler-guard). We instead allow any
// char except a statement terminator `;` or a block-open `{` between the opening
// `(` and the body `{`; a method signature never contains those before its body,
// while param decorators, generic `<...>`, default values and the `: ReturnType`
// annotation are all permitted. The trailing `\)\s*(?::[^={;]+)?\{` pins the
// match to the LAST `)` immediately before the (optionally return-typed) body.
var nestHandlerMethodRe = regexp.MustCompile(
	`(?m)^[ \t]*(?:public\s+|private\s+|protected\s+|static\s+|readonly\s+|async\s+)*` +
		`([A-Za-z_$][\w$]*)\s*\([^;{]*\)\s*(?::[^={;]+)?\{`,
)

// nestVerbDecoratorRe identifies a route decorator (@Get/@Post/...). Used to
// confirm a decorator block belongs to a route handler.
var nestVerbDecoratorRe = regexp.MustCompile(`@(Get|Post|Put|Patch|Delete|All|Options|Head)\b`)

// indexNestMethodGuards attributes each handler method's *own* preceding
// decorator block to that method. The decorator block is the contiguous run of
// `@...` decorator lines (and blank lines) immediately above the method
// declaration — so a method-level @UseGuards / @Roles binds only to its method,
// never bleeding into a sibling. A method whose block carries a verb decorator
// but NO @UseGuards still binds nothing (it inherits the class-level guard via
// the file-scope classGuards path). Returns a map keyed by method name.
func indexNestMethodGuards(content, _ string) map[string][]AuthSignal {
	out := map[string][]AuthSignal{}
	if !strings.Contains(content, "@UseGuards") {
		return out
	}
	for _, loc := range nestHandlerMethodRe.FindAllStringSubmatchIndex(content, -1) {
		method := content[loc[2]:loc[3]]
		block := precedingDecoratorBlock(content, loc[0])
		if block == "" || !nestVerbDecoratorRe.MatchString(block) {
			continue
		}
		gm := jsAuthGuardDecoratorRe.FindStringSubmatch(block)
		if gm == nil {
			continue
		}
		if len(parseGuardArgs(gm[1])) == 0 {
			continue
		}
		text := "@UseGuards(" + strings.TrimSpace(gm[1]) + ")"
		if rm := jsRolesDecoratorRe.FindStringSubmatch(block); rm != nil {
			text += " @Roles(" + strings.TrimSpace(rm[1]) + ")"
		}
		if pm := jsPermissionsDecoratorRe.FindStringSubmatch(block); pm != nil {
			text += " @RequirePermissions(" + strings.TrimSpace(pm[1]) + ")"
		}
		if sm := jsScopesDecoratorRe.FindStringSubmatch(block); sm != nil {
			text += " @Scopes(" + strings.TrimSpace(sm[1]) + ")"
		}
		out[method] = append(out[method], AuthSignal{
			Kind: "guard",
			Text: text,
			Line: lineAtOffset(content, loc[0]),
		})
	}
	return out
}

// precedingDecoratorBlock returns the contiguous block of decorator / blank
// lines immediately above the line containing byte offset `declStart`. Walks
// backwards line-by-line, stopping at the first line that is neither blank nor
// a decorator (`@...`) — that boundary is the previous statement (the prior
// method's closing brace or the class opener), so the block belongs solely to
// the method at declStart.
func precedingDecoratorBlock(content string, declStart int) string {
	// Find the start of the declaration's own line.
	lineStart := declStart
	for lineStart > 0 && content[lineStart-1] != '\n' {
		lineStart--
	}
	end := lineStart
	cur := lineStart
	for cur > 0 {
		// Move cur to the start of the previous line.
		prevEnd := cur - 1 // the '\n' terminating the previous line
		prevStart := prevEnd
		for prevStart > 0 && content[prevStart-1] != '\n' {
			prevStart--
		}
		line := strings.TrimSpace(content[prevStart:prevEnd])
		if line == "" || strings.HasPrefix(line, "@") || strings.HasPrefix(line, "//") {
			cur = prevStart
			continue
		}
		break
	}
	if cur >= end {
		return ""
	}
	return content[cur:end]
}

// ---------------------------------------------------------------------------
// Hapi per-route auth indexer
// ---------------------------------------------------------------------------

// hapiAuth captures a per-route Hapi auth verdict.
type hapiAuth struct {
	set    bool
	public bool // auth: false
	signal AuthSignal
}

// indexHapiRouteAuth scans each `server.route({ ... })` object body for a
// `path`, `method` and `auth`/`options.auth`/`config.auth` setting and maps
// the resulting (verb, canonical path) to a hapiAuth verdict.
func indexHapiRouteAuth(content, _ string) map[string]hapiAuth {
	out := map[string]hapiAuth{}
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
		am := jsHapiAuthKwargRe.FindStringSubmatch(body)
		if am == nil {
			continue
		}
		val := strings.TrimSpace(am[1])
		ha := hapiAuth{set: true}
		if val == "false" {
			ha.public = true
			ha.signal = AuthSignal{Kind: "config", Text: "auth: false (explicit public)", Line: lineAtOffset(content, braceOpen)}
		} else {
			ha.signal = AuthSignal{Kind: "config", Text: "route auth: " + val, Line: lineAtOffset(content, braceOpen)}
		}
		for _, verb := range verbs {
			out[verb+" "+canonical] = ha
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// AdonisJS route-chain middleware indexer
// ---------------------------------------------------------------------------

// indexAdonisRouteAuth scans AdonisJS route registrations for a chained
// `.middleware('auth')` / `.use([middleware.auth()])` call. Adonis chains the
// middleware onto the route expression, so we scan each Route.<verb>(...) line
// (and its continuation up to the statement end) for an auth middleware token.
func indexAdonisRouteAuth(content, _ string) map[string][]AuthSignal {
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
		// Scan from the route call to the end of the statement (next newline
		// that isn't a chain continuation) for a .middleware(...) auth token.
		stmt := adonisStatementFrom(content, m[0])
		if mw := jsAdonisMiddlewareRe.FindStringSubmatch(stmt); mw != nil {
			if adonisMiddlewareIsAuth(mw[1]) {
				out[verb+" "+canonical] = append(out[verb+" "+canonical], AuthSignal{
					Kind: "middleware",
					Text: "route-level: .middleware(" + strings.TrimSpace(mw[1]) + ")",
					Line: lineAtOffset(content, m[0]),
				})
			}
		}
	}
	return out
}

// adonisStatementFrom returns the source span from offset `start` to the end of
// the AdonisJS route statement, following chained `.method(...)` continuations
// across newlines (a leading `.` or open paren means the chain continues).
func adonisStatementFrom(content string, start int) string {
	end := start
	depth := 0
	for end < len(content) {
		c := content[end]
		switch c {
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			if depth > 0 {
				depth--
			}
		case '\n':
			if depth == 0 {
				// Peek ahead: a chain continuation begins with optional
				// whitespace then a '.'.
				rest := strings.TrimLeft(content[end+1:], " \t\r")
				if !strings.HasPrefix(rest, ".") {
					return content[start:end]
				}
			}
		}
		end++
		if end-start > 4096 {
			break
		}
	}
	return content[start:end]
}

// adonisMiddlewareIsAuth reports whether an Adonis middleware argument list
// references an auth middleware. Adonis names middleware by string key
// ('auth', 'auth:api') or by the Adonis 6 `middleware.auth()` reference.
func adonisMiddlewareIsAuth(args string) bool {
	low := strings.ToLower(args)
	for _, tok := range []string{"auth", "authenticate", "guard", "silentauth"} {
		if strings.Contains(low, tok) {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Marble.js per-route auth indexer
// ---------------------------------------------------------------------------

// indexMarbleRouteAuth scans each Marble.js routing Effect (`const x$ =
// r.pipe(...)`) for an auth middleware effect piped before useEffect, e.g.
// `r.pipe(r.matchPath('/me'), r.matchType('GET'), use(authorize$), ...)`.
// Returns a map keyed by (verb, canonical path).
func indexMarbleRouteAuth(content, _ string) map[string][]AuthSignal {
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
		if am := jsMarbleAuthRe.FindStringSubmatch(body); am != nil {
			out[verb+" "+canonical] = append(out[verb+" "+canonical], AuthSignal{
				Kind: "middleware",
				Text: "route-level: use(" + am[1] + ")",
				Line: lineAtOffset(content, idx[0]),
			})
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// Sails policy-map recognition (#2852 — framework_specific idiom)
// ---------------------------------------------------------------------------
//
// Sails does not gate routes with middleware or guards; it maps controllers /
// actions to *policies* in `config/policies.js`. A protective global default
// (`'*': 'isLoggedIn'`) authenticates every action unless an action opts out
// with `true`. This is a bespoke auth surface that the generic route/app/guard
// middleware recognisers do not capture, so the Sails auth_coverage cell is
// classified `framework_specific` in the registry. The recogniser below proves
// the capability: it parses a policies.js map into a default posture plus the
// explicit per-action overrides.

// sailsGlobalPolicyRe captures the global default policy `'*': <value>`.
// Group 1 = value (`false` | `true` | a policy name | `[ ... ]`).
var sailsGlobalPolicyRe = regexp.MustCompile(
	`['"` + "`" + `]\*['"` + "`" + `]\s*:\s*(false|true|\[[^\]]*\]|['"` + "`" + `][^'"` + "`" + `]+['"` + "`" + `])`,
)

// sailsControllerBlockRe captures a per-controller object entry
// `AuthController: { ... }`. Group 1 = controller name, group 2 = the position
// of the opening brace is recovered via index match (the braces are balanced
// by findMatchingBrace, since action values may themselves be arrays). The
// controller name is a bare identifier ending in `Controller` (the Sails
// convention) — matching only the suffix keeps this from firing on the
// `'*'` global or arbitrary nested objects.
var sailsControllerBlockRe = regexp.MustCompile(
	`(?m)['"` + "`" + `]?([A-Za-z_$][\w$]*Controller)['"` + "`" + `]?\s*:\s*\{`,
)

// sailsControllerValueRe captures a controller-level bare value entry
// `AuthController: 'isLoggedIn'` / `AuthController: true` / `AuthController:
// ['a','b']` (object form is handled separately by sailsControllerBlockRe).
// Group 1 = controller name, group 2 = the raw value.
var sailsControllerValueRe = regexp.MustCompile(
	`(?m)['"` + "`" + `]?([A-Za-z_$][\w$]*Controller)['"` + "`" + `]?\s*:\s*` +
		`(false|true|\[[^\]]*\]|['"` + "`" + `][^'"` + "`" + `]+['"` + "`" + `])`,
)

// sailsActionRe captures one `actionName: <value>` line inside a controller
// block. Group 1 = action name, group 2 = raw value.
var sailsActionRe = regexp.MustCompile(
	`(?m)['"` + "`" + `]?([A-Za-z_$][\w$]*)['"` + "`" + `]?\s*:\s*` +
		`(false|true|\[[^\]]*\]|['"` + "`" + `][^'"` + "`" + `]+['"` + "`" + `])`,
)

// SailsPolicyMap is the parsed result of a config/policies.js file.
type SailsPolicyMap struct {
	// DefaultProtected is true when the global `'*'` default names a real policy
	// (not `true` and not absent). A `true` global default means "public by
	// default" → not protected.
	DefaultProtected bool
	// DefaultPolicy is the raw global default value (policy name / true / false).
	DefaultPolicy string
	// HasDefault reports whether a global `'*'` key was present at all.
	HasDefault bool
	// Controllers maps each controller name (e.g. "AuthController") to its
	// per-controller policy block. The block carries an optional
	// controller-level catch-all policy plus per-action overrides. #2897.
	Controllers map[string]SailsControllerPolicy
	// File is the source path the map was parsed from.
	File string
}

// SailsControllerPolicy is the policy block for one controller in a Sails
// config/policies.js map. A controller entry can be either a single policy
// value applied to every action (`AuthController: 'isLoggedIn'`) or an object
// of per-action overrides (`AuthController: { login: true, logout: 'x' }`).
type SailsControllerPolicy struct {
	// ControllerPolicy is the controller-level catch-all value when the entry
	// is a bare value (`AuthController: 'isLoggedIn'`). Empty when the entry is
	// an object of per-action overrides.
	ControllerPolicy string
	// HasControllerPolicy reports whether a controller-level value was present.
	HasControllerPolicy bool
	// Actions maps action name → raw policy value (`true` | `false` |
	// `'policyName'` | `['a','b']`) for object-form entries.
	Actions map[string]string
}

// sailsPoliciesFile reports whether filePath is a Sails policies-config file.
func sailsPoliciesFile(filePath string) bool {
	p := strings.ReplaceAll(filePath, "\\", "/")
	return strings.HasSuffix(p, "config/policies.js") ||
		strings.HasSuffix(p, "config/policies.ts")
}

// ParseSailsPolicies parses a Sails config/policies.js map and reports the
// global default auth posture. A global default that names a policy (e.g.
// `'isLoggedIn'`) protects every action; `true` means public-by-default.
func ParseSailsPolicies(content, file string) (SailsPolicyMap, bool) {
	if !strings.Contains(content, "policies") {
		return SailsPolicyMap{}, false
	}
	pm := SailsPolicyMap{File: file, Controllers: map[string]SailsControllerPolicy{}}

	// Global default `'*': <value>`.
	if m := sailsGlobalPolicyRe.FindStringSubmatch(content); m != nil {
		val := strings.TrimSpace(m[1])
		pm.DefaultPolicy = val
		pm.HasDefault = true
		pm.DefaultProtected = sailsPolicyValueProtected(val)
	}

	// Per-controller object blocks: `AuthController: { login: true, ... }`.
	// Parse the balanced `{...}` body and capture each action override.
	for _, loc := range sailsControllerBlockRe.FindAllStringSubmatchIndex(content, -1) {
		name := content[loc[2]:loc[3]]
		braceOpen := loc[1] - 1 // the `{` the regex anchored on
		braceClose := findMatchingBrace(content, braceOpen)
		if braceClose < 0 {
			continue
		}
		body := content[braceOpen+1 : braceClose]
		cp := SailsControllerPolicy{Actions: map[string]string{}}
		for _, am := range sailsActionRe.FindAllStringSubmatch(body, -1) {
			cp.Actions[am[1]] = strings.TrimSpace(am[2])
		}
		if len(cp.Actions) > 0 {
			pm.Controllers[name] = cp
		}
	}

	// Per-controller bare-value entries: `AuthController: 'isLoggedIn'`.
	// Skip names already captured as an object block above.
	for _, m := range sailsControllerValueRe.FindAllStringSubmatch(content, -1) {
		name := m[1]
		if _, ok := pm.Controllers[name]; ok {
			continue
		}
		pm.Controllers[name] = SailsControllerPolicy{
			ControllerPolicy:    strings.TrimSpace(m[2]),
			HasControllerPolicy: true,
		}
	}

	// A file with neither a global default nor any controller block is not a
	// recognisable Sails policy map.
	if !pm.HasDefault && len(pm.Controllers) == 0 {
		return SailsPolicyMap{}, false
	}
	return pm, true
}

// sailsPolicyValueProtected reports whether a raw Sails policy value gates the
// action. `true` = public (no policy) → not protected. `false` = blanket deny
// → protected (no anonymous access). A named policy / array → protected.
func sailsPolicyValueProtected(val string) bool {
	return val != "true"
}
