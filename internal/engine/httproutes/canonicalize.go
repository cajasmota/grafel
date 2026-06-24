// Package httproutes provides canonicalisation of HTTP route paths across
// frameworks so that synthetic http_endpoint entities use the same ID on
// both producer and consumer sides regardless of the framework conventions
// of the originating code.
//
// The canonical form uses `{name}` for path parameters (matching the
// OpenAPI / JAX-RS / FastAPI form). All paths are rooted with a leading
// `/`. Trailing slashes are stripped except for the root path itself.
//
// Convention: trailing slash is normalised AWAY. Django often writes
// `path("users/", ...)` and Flask/FastAPI/Spring usually omit it; we pick
// the shorter form so backend and frontend agree on
// `http:GET:/users/{id}` regardless of source convention.
package httproutes

import (
	"strings"
)

// Framework identifiers passed to Canonicalize.
const (
	FrameworkDjango  = "django"
	FrameworkFlask   = "flask"
	FrameworkFastAPI = "fastapi"
	FrameworkSpring  = "spring"
	FrameworkJAXRS   = "jaxrs"
	FrameworkExpress = "express"
	// FrameworkGin, FrameworkEcho, FrameworkChi (#722) share Express's
	// `:name` parameter convention; their canonicalisation reuses the
	// colon-param walker. They are listed as distinct constants so call
	// sites in per-language extractors read naturally.
	FrameworkGin  = "gin"
	FrameworkEcho = "echo"
	FrameworkChi  = "chi"
	// FrameworkAxum (#1420) uses {param} curly-brace syntax identical to
	// FastAPI/JAX-RS; canonicalisation reuses canonicalizeCurlyBraces.
	FrameworkAxum = "axum"
	// FrameworkStarlette (#2690) — Route("/users/{id}", endpoint=..., methods=[...])
	// uses {name} curly-brace placeholders identical to FastAPI / JAX-RS.
	FrameworkStarlette = "starlette"
	// FrameworkPyramid (#2690) — config.add_route("name", "/users/{id}") +
	// @view_config(route_name="name", request_method="GET"). Path syntax is
	// {name} curly-brace, same as Starlette.
	FrameworkPyramid = "pyramid"
	// FrameworkTornado (#2690) — Application([(r"/users/([0-9]+)", Handler)]).
	// Tornado paths are raw Python regex; the synthesizer pre-rewrites
	// `(?P<name>...)` named groups and bare capture groups before this
	// function sees the path, so canonicalisation here is identity +
	// normaliseSlashes (default case).
	FrameworkTornado = "tornado"
	// FrameworkASPNetCore (#2692) — ASP.NET Core `[Route("/api/{id}")]` and
	// `[HttpGet("/path/{id:int}")]` use the same curly-brace syntax as Spring
	// (and accept the optional `:type` route-constraint suffix). Canonicalisation
	// reuses canonicalizeCurlyBraces which strips the constraint suffix.
	FrameworkASPNetCore = "aspnet_core"
	// FrameworkPhoenix (#2692) — Elixir Phoenix routers use `:name` colon-prefixed
	// path parameters (`get "/users/:id", ...`), identical to Express/Rails/Gin.
	FrameworkPhoenix = "phoenix"
	// FrameworkRocket (#2692) — Rust Rocket attribute macros use `<name>` angle-
	// bracket path parameters (`#[get("/users/<id>")]`), like Django/Flask.
	FrameworkRocket = "rocket"
	// FrameworkAdonis (#2851) — AdonisJS `Route.get('/users/:id', ...)` uses
	// the Express-style `:name` colon-prefixed path parameter convention.
	FrameworkAdonis = "adonisjs"
	// FrameworkHapi (#2851) — Hapi `server.route({ path: '/users/{id}' })` uses
	// `{name}` curly-brace path parameters (and `{name?}` / `{name*}` modifiers,
	// which canonicalizeCurlyBraces normalises by stripping the suffix marker).
	FrameworkHapi = "hapi"
	// FrameworkFeathers (#2851) — Feathers `app.use('/messages', service)`
	// registers a REST service at a mount path; the synthesizer expands the
	// service to its standard verb set. Paths are plain strings with no param
	// syntax, so canonicalisation is identity + slash normalisation.
	FrameworkFeathers = "feathers"
	// FrameworkMarble (#2851) — Marble.js `r.pipe(r.matchPath('/users/:id'),
	// r.matchType('GET'))` uses the Express-style `:name` colon convention.
	FrameworkMarble = "marblejs"
	// FrameworkPolka (#2851) — Polka is an Express-compatible micro-router;
	// `app.get('/users/:id', ...)` uses the `:name` colon convention.
	FrameworkPolka = "polka"
	// FrameworkRestify (#2851) — Restify `server.get('/users/:id', ...)` uses
	// the Express-style `:name` colon convention.
	FrameworkRestify = "restify"
	// FrameworkSails (#2851) — Sails config/routes.js maps `'GET /users/:id':
	// 'UserController.find'`; the path uses the `:name` colon convention.
	FrameworkSails = "sails"
	// FrameworkSanic (#2980) — Sanic `@app.get("/users/<int:user_id>")` and
	// `@bp.route(...)` use Flask-style `<converter:name>` / `<name>` angle-bracket
	// path parameters. Canonicalisation reuses the angle-bracket walker.
	FrameworkSanic = "sanic"
	// FrameworkLitestar (#2980) — Litestar `@get("/users/{user_id:int}")` route
	// handlers and `Controller` classes use `{name:type}` curly-brace path
	// parameters identical to FastAPI; the `:type` suffix is stripped by
	// canonicalizeCurlyBraces.
	FrameworkLitestar = "litestar"
	// FrameworkRobyn (#2980) — Robyn `@app.get("/users/:id")` uses the
	// Express-style `:name` colon-prefixed path parameter convention.
	FrameworkRobyn = "robyn"
	// FrameworkAiohttp (#2979) — aiohttp `app.router.add_get("/users/{user_id}")`
	// and `@routes.get("/users/{user_id}")` use the FastAPI-style `{name}` /
	// `{name:regex}` curly-brace path parameter convention; the `:regex`
	// suffix is stripped by canonicalizeCurlyBraces.
	FrameworkAiohttp = "aiohttp"
	// FrameworkBottle (#2979) — Bottle `@route("/users/<id>")` / `@get(...)`
	// use the Flask-style `<name>` / `<name:filter>` angle-bracket path
	// parameter convention. Canonicalisation reuses the angle-bracket walker.
	FrameworkBottle = "bottle"
	// FrameworkCherryPy (#3065) — CherryPy `@cherrypy.expose` method routing
	// uses plain path strings (the URL is derived from the class/method structure
	// or from _cp_dispatch). Canonicalisation is identity + slash normalisation.
	FrameworkCherryPy = "cherrypy"
	// FrameworkFalcon (#3065) — Falcon `app.add_route('/users/{user_id}', resource)`
	// uses `{name}` curly-brace path parameters; canonicalisation reuses
	// canonicalizeCurlyBraces.
	FrameworkFalcon = "falcon"
	// FrameworkHug (#3065) — Hug `@hug.get('/path')` / `@hug.post('/path')`
	// decorators use plain path strings with `{name}` curly-brace parameters
	// identical to FastAPI. Canonicalisation reuses canonicalizeCurlyBraces.
	FrameworkHug = "hug"
	// FrameworkQuart (#3065) — Quart mirrors the Flask routing API exactly:
	// `@app.route('/path')` / `@app.get('/path')` with Flask-style `<converter:name>`
	// angle-bracket path parameters. Canonicalisation reuses the angle-bracket walker.
	FrameworkQuart = "quart"
	// FrameworkJavalin (#3085) — Javalin `app.get("/users/{id}", handler)` uses
	// `{name}` curly-brace path parameters identical to JAX-RS / Spring.
	// Canonicalisation reuses canonicalizeCurlyBraces.
	FrameworkJavalin = "javalin"
	// FrameworkVertx (#3086) — Vert.x Web `router.get("/users/:id", ...)` uses
	// `{name}` curly-brace path parameters identical to JAX-RS / Spring.
	// Canonicalisation reuses canonicalizeCurlyBraces.
	FrameworkVertx = "vertx"
	// FrameworkAkkaHTTP (#3092) — Akka-HTTP Java DSL `path("users")` / `pathPrefix("api")`
	// directives use plain string segments; there are no in-string path parameter markers
	// (dynamic segments come from Scala PathMatcher / segment() calls, not embedded `{name}`).
	// Canonicalisation is identity + slash normalisation (default case).
	FrameworkAkkaHTTP = "akka-http"
	// FrameworkPlug (#3468) — Elixir Plug.Router `get "/users/:id" do ... end`
	// uses the Express-style `:name` colon-prefixed path parameter convention,
	// identical to Phoenix. Canonicalisation reuses canonicalizeColonParams.
	FrameworkPlug = "plug"
	// FrameworkCowboy (#3468) — Erlang/Elixir Cowboy dispatch route tables
	// (`{"/users/:id", Handler, []}`) use the Phoenix-style `:name` colon
	// convention; Cowboy's native `:name` / `[...]` bindings are normalised to
	// the colon form by the synthesizer before canonicalisation. Reuses
	// canonicalizeColonParams.
	FrameworkCowboy = "cowboy"
	// FrameworkLapis (#3484) — Lua Lapis `app:get("/users/:id", fn)` and named
	// `app:match("name", "/users/:id", fn)` routes use the Express-style `:name`
	// colon-prefixed path parameter convention. Splat params `*` are passed
	// through. Canonicalisation reuses canonicalizeColonParams.
	FrameworkLapis = "lapis"
	// FrameworkOpenResty (#3484) — OpenResty nginx `location /path { ... }`
	// stanzas use literal path prefixes (no param syntax); lua-resty-router
	// `r:get("/users/:id", fn)` uses the `:name` colon convention. Both are
	// normalised by canonicalizeColonParams (a literal nginx path has no `:`
	// segments, so it passes through unchanged save for slash normalisation).
	FrameworkOpenResty = "openresty"
	// FrameworkGqlgen (#3613) — gqlgen is the dominant schema-first GraphQL
	// server for Go. Operation endpoints are synthesised as the canonical
	// `/graphql/<RootType>/<field>` path shared with the JS/TS GraphQL server
	// (synthesizeGraphQLResolvers) and the Python Strawberry server. The path
	// carries no framework-specific parameter syntax, so canonicalisation is
	// identity + slash normalisation (default case).
	FrameworkGqlgen = "gqlgen"
	// FrameworkPothos (#3619) — Pothos is a code-first GraphQL schema builder
	// for JS/TS. Root fields registered via builder.queryField / mutationField /
	// subscriptionField (and queryType/mutationType/subscriptionType field maps)
	// are synthesised as the canonical `/graphql/<RootType>/<field>` path shared
	// with gqlgen (Go), Apollo (JS), and Strawberry (Python). The path carries
	// no framework-specific parameter syntax, so canonicalisation is identity +
	// slash normalisation (default case).
	FrameworkPothos = "pothos"
	// FrameworkTypeGraphQL (#3619) — TypeGraphQL is a code-first GraphQL library
	// for TS that defines root operations via @Query / @Mutation / @Subscription
	// decorated methods inside @Resolver classes. Root fields are synthesised as
	// the canonical `/graphql/<RootType>/<field>` path shared with the other
	// GraphQL servers. The path carries no framework-specific parameter syntax,
	// so canonicalisation is identity + slash normalisation (default case).
	FrameworkTypeGraphQL = "type-graphql"
	// FrameworkGraphQLRuby (#3621) — graphql-ruby is the dominant GraphQL
	// server for Ruby. Operation endpoints are synthesised as the canonical
	// `/graphql/<RootType>/<field>` path shared with the JS/TS, Python, Go and
	// C# GraphQL servers. The path carries no framework-specific parameter
	// syntax, so canonicalisation is identity + slash normalisation (default
	// case).
	FrameworkGraphQLRuby = "graphql-ruby"
	// FrameworkVapor (#4749) — Swift Vapor routes (`app.get("users", ":id")` /
	// `routes.post("users")`) use the Express-style `:name` colon-prefixed path
	// parameter convention. A Vapor route is declared as a sequence of path
	// COMPONENTS — string literals and `:param` dynamic components — that the
	// synthesizer joins with `/` before canonicalisation. Canonicalisation
	// reuses canonicalizeColonParams.
	FrameworkVapor = "vapor"
	// FrameworkClojure (#4749) — Clojure web routes (Compojure
	// `(GET "/users/:id" [] handler)`, Reitit `["/users/:id" {:get handler}]`,
	// Ring/Pedestal) use the Express-style `:name` colon-prefixed path-parameter
	// convention. Canonicalisation reuses canonicalizeColonParams.
	FrameworkClojure = "clojure"
	// FrameworkKemal (#4749) — Crystal web frameworks (Kemal `get "/users/:id"`,
	// Lucky/Amber `get "/users/:id", Controller, :action`) use the Sinatra/
	// Express-style `:name` colon-prefixed path parameter convention (Crystal
	// syntax is Ruby-like). Kemal also accepts a trailing `*` wildcard segment
	// (`get "/files/*path"`), which the colon walker leaves intact (the segment
	// matcher wildcards it). Canonicalisation reuses canonicalizeColonParams.
	FrameworkKemal = "kemal"
	// FrameworkGiraffe (#4749) — F# web frameworks Giraffe and Saturn. Giraffe
	// composes routes with the `route "/literal"` combinator (a fully static
	// path, no param syntax) and `routef "/users/%i"` (printf-style typed
	// placeholders — `%i`/`%d`/`%u`/`%s`/`%b`/`%c`/`%f`/`%O` for int/decimal/
	// uint/string/bool/char/float/guid). The `routef` placeholders are rewritten
	// to the canonical `{}` positional-wildcard form so a `routef "/users/%i"`
	// definition matches a concrete test route `/users/42` segment-for-segment.
	// Saturn's `router { get "/users/:id" handler }` DSL uses the Sinatra/
	// Express-style `:name` colon convention, so the same canonicaliser handles
	// both `%fmt` and `:name`.
	FrameworkGiraffe = "giraffe"
	// FrameworkGrails (#4749) — Groovy Grails. Grails has two route surfaces:
	// (1) the convention `/<controller>/<action>` mapping derived from a
	// `FooController` class in grails-app/controllers + its action methods, and
	// (2) explicit `UrlMappings.groovy` mappings which use Grails dollar-prefixed
	// path parameters (`/book/$id`, `/book/$author/$title`). The synthesizer
	// pre-rewrites `$name` → `{name}`, so canonicalisation is the shared
	// curly-brace pass.
	FrameworkGrails = "grails"
	// FrameworkRatpack (#4749) — Groovy/JVM Ratpack handler DSL
	// (`get("path") { … }`, `path("api/:id") { … }`). Ratpack uses the
	// Express-style `:name` colon-prefixed path-parameter convention and
	// declares paths WITHOUT a leading slash (the synthesizer adds it).
	// Canonicalisation reuses canonicalizeColonParams.
	FrameworkRatpack = "ratpack"
	// FrameworkJester (#4749) — Nim's Jester web framework declares routes
	// inside a `routes:` block with indented verb entries carrying a string
	// path literal: `get "/users/@id":`, `post "/x":`. Jester path parameters
	// use the `@name` AT-prefixed convention (`/users/@id` → `{id}`), and a
	// trailing `@rest` greedy-match or a `re"…"` regex segment is left intact
	// (the segment matcher wildcards it). Canonicalisation rewrites `@name`
	// segments to the shared `{name}` form via canonicalizeJesterParams.
	FrameworkJester = "jester"
	// FrameworkPrologue (#4749) — Nim's Prologue web framework registers routes
	// with `app.get("/users/{id}", handler)` / `app.addRoute("/x", handler,
	// HttpGet)`. Prologue (and HappyX) path parameters use the curly-brace
	// `{name}` convention identical to FastAPI/Spring, so canonicalisation
	// reuses canonicalizeCurlyBraces.
	FrameworkPrologue = "prologue"
	// FrameworkShelf (#4758) — Dart's shelf_router (`Router()..get('/users/<id>',
	// handler)`, `router.post('/x', h)`) uses ANGLE-bracket path parameters with
	// an OPTIONAL inline regex constraint after a `|` pipe: `<id>` and
	// `<id|[0-9]+>` both name a single param `id`. Canonicalisation strips the
	// `|regex` tail and rewrites `<name>` → `{name}` via canonicalizeShelfParams.
	FrameworkShelf = "shelf"
	// FrameworkDartFrog (#4758) — Dart Frog's file-based routing derives the path
	// from the route file location under `routes/`: a directory/file segment
	// `[id]` is a dynamic path parameter and `index.dart` collapses to its parent
	// directory. The synthesizer pre-derives the route path from the file path,
	// pre-rewriting `[name]` → `{name}`, so canonicalisation here is the shared
	// curly-brace pass (defensive — handles any residual `[name]`).
	FrameworkDartFrog = "dart_frog"
	// FrameworkConduit (#4758) — Dart's Conduit (`router.route("/users/[:id]")`)
	// declares OPTIONAL path parameters as `[:id]` (square-bracketed colon param)
	// and required ones as `:id`. canonicalizeConduitParams drops the optional
	// `[ ]` wrapper and rewrites `:name` → `{name}`.
	FrameworkConduit = "conduit"
	// FrameworkGrape (#4417) — Ruby Grape API (`class API < Grape::API`) declares
	// routes with verb blocks (`get ':id' do ... end`) nested inside `resource`/
	// `namespace`/`group` blocks. The synthesizer composes the resource/namespace
	// prefixes onto the verb path. Grape path parameters use the Sinatra/Express-
	// style `:name` colon-prefixed convention (`/users/:id` → `/users/{id}`), so
	// canonicalisation reuses canonicalizeColonParams.
	FrameworkGrape = "grape"
	// FrameworkRoda (#4417) — Ruby Roda routing tree (`route do |r| r.on "users"
	// do r.get Integer do |id| ... end end end`). The synthesizer composes the
	// `r.on`/`r.is` branch segments into the path with the verb at the leaf. Roda
	// captures are NORMALISED by the synthesizer before canonicalisation: a
	// `String`/`Integer`/`Float`/`:sym` class-matcher or symbol-capture branch
	// segment is rewritten to the Express-style `:param` form, so canonicalisation
	// reuses canonicalizeColonParams.
	FrameworkRoda = "roda"
	// FrameworkScotty (#5373) — Haskell Scotty `get "/users/:id"` verb routes.
	// Scotty uses the Sinatra/Express-style `:name` colon path-parameter
	// convention, so canonicalisation reuses canonicalizeColonParams.
	FrameworkScotty = "scotty"
	// FrameworkYesod (#5373) — Haskell Yesod `parseRoutes` quasiquote route
	// table. A path segment is either a literal, a typed single-segment capture
	// `#Type` (e.g. `/user/#UserId`) or a typed multi-segment capture `*Type`
	// (e.g. `/static/*Texts`). The synthesizer NORMALISES `#Foo`/`*Foo` capture
	// segments to the Express-style `:foo` form (lower-cased type name) before
	// canonicalisation, so it reuses canonicalizeColonParams.
	FrameworkYesod = "yesod"
	// FrameworkServant (#5373) — Haskell Servant type-level API DSL. A route is a
	// chain of `:>`-combined components ending in a verb combinator
	// (`Get`/`Post`/…): string-literal segments are path literals and
	// `Capture "name" Type` segments are path params. The synthesizer
	// NORMALISES each `Capture "name" _` to the Express-style `:name` form before
	// canonicalisation, so it reuses canonicalizeColonParams.
	FrameworkServant = "servant"
	// FrameworkDream (#5374) — OCaml Dream `Dream.get "/users/:id" handler` verb
	// routes. Dream uses the Sinatra/Express-style `:name` colon path-parameter
	// convention, so canonicalisation reuses canonicalizeColonParams.
	FrameworkDream = "dream"
)

// Canonicalize maps a framework-specific raw path string to the canonical
// `{param}` form. The output always starts with `/` and has no trailing
// slash (except for the bare root path `/`).
//
// Recognised input forms:
//   - Django:   `<int:user_id>`, `<str:slug>`, `<uuid:pk>`, `<name>` -> `{user_id}` / `{slug}` / `{pk}` / `{name}`
//   - Django re_path / DRF @action(url_path=…): `(?P<name>regex)` -> `{name}`
//   - Flask:    `<int:id>`, `<float:x>`, `<path:rest>`, `<uuid:u>`, `<id>` -> `{id}` / `{x}` / `{rest}` / `{u}` / `{id}`
//   - FastAPI / JAX-RS / Spring: `{id}`, `{id:regex}` -> `{id}` (regex constraint stripped)
//   - Express:  `:id`, `:id?` -> `{id}` (optional marker dropped — phase 1)
func Canonicalize(framework, raw string) string {
	if raw == "" {
		return "/"
	}

	var out string
	switch framework {
	case FrameworkBottle:
		// Bottle path params are `<name>` / `<name:filter>` — the NAME comes
		// FIRST, before the optional filter (the inverse of Flask's
		// `<converter:name>`). Strip the `:filter` suffix so the shared
		// angle-bracket walker (which keeps the post-colon segment for Flask)
		// receives a bare `<name>` and emits `{name}`.
		out = canonicalizeAngleBrackets(stripBottleFilters(raw))
	case FrameworkDjango, FrameworkFlask, FrameworkRocket, FrameworkSanic, FrameworkQuart:
		// #2669 — Django re_path and DRF @action(url_path=…) frequently embed
		// Python named-group regex `(?P<name>charclass)` inside the URL. Pre-strip
		// these to `<name>` so the angle-bracket walker can canonicalise them
		// uniformly with normal `<int:id>` converters. Without this the embedded
		// `(?P<…>)` survives into the canonical path and breaks byPath bucketing
		// across the producer and consumer sides.
		out = stripPythonNamedGroups(raw)
		out = canonicalizeAngleBrackets(out)
	case FrameworkFastAPI, FrameworkSpring, FrameworkJAXRS, FrameworkAxum,
		FrameworkStarlette, FrameworkPyramid, FrameworkASPNetCore, FrameworkHapi,
		FrameworkLitestar, FrameworkAiohttp, FrameworkFalcon, FrameworkHug,
		FrameworkJavalin, FrameworkVertx, FrameworkGrails, FrameworkPrologue,
		FrameworkDartFrog:
		out = canonicalizeCurlyBraces(raw)
	case FrameworkShelf:
		// Dart shelf_router `<id>` / `<id|[0-9]+>` angle-bracket params → `{id}`.
		out = canonicalizeShelfParams(raw)
	case FrameworkConduit:
		// Dart Conduit `[:id]` optional + `:id` required params → `{id}`.
		out = canonicalizeConduitParams(raw)
	case FrameworkJester:
		// Nim Jester `@name` AT-prefixed path params → `{name}`.
		out = canonicalizeJesterParams(raw)
	case FrameworkTornado:
		// Tornado paths arrive already pre-processed by the synthesizer
		// (named-group → {name}, bare group → {}). Curly-brace pass
		// strips any stray `:regex` constraints, mirroring the other
		// curly-brace frameworks.
		out = canonicalizeCurlyBraces(raw)
	case FrameworkExpress, FrameworkGin, FrameworkEcho, FrameworkChi, FrameworkPhoenix,
		FrameworkAdonis, FrameworkMarble, FrameworkPolka, FrameworkRestify, FrameworkSails,
		FrameworkRobyn, FrameworkPlug, FrameworkCowboy,
		FrameworkLapis, FrameworkOpenResty, FrameworkVapor, FrameworkClojure,
		FrameworkKemal, FrameworkRatpack, FrameworkGrape, FrameworkRoda,
		FrameworkScotty, FrameworkYesod, FrameworkServant, FrameworkDream:
		out = canonicalizeColonParams(raw)
	case FrameworkGiraffe:
		// Giraffe `routef "/users/%i"` printf placeholders → `{}` first, then
		// the colon walker handles Saturn `:name` params. A plain Giraffe
		// `route "/users"` has neither and passes through unchanged.
		out = canonicalizeColonParams(canonicalizeGiraffeFormat(raw))
	default:
		// Unknown framework: pass through but still normalise slashes.
		out = raw
	}

	return normaliseSlashes(out)
}

// stripPythonNamedGroups rewrites Python regex named-group syntax
// `(?P<name>charclass)` (and the rarer `(?P<name>literal)`) found inside
// Django `re_path` patterns and DRF `@action(url_path=…)` strings into the
// canonical angle-bracket form `<name>`. The wrapping `(?P<` and `>…)` are
// removed so the angle-bracket walker downstream sees a plain `<name>`
// token. Group bodies (anything between `>` and the matching `)`) are
// balanced-aware: nested `(` / `)` are tracked so a group such as
// `(?P<id>(\d+))` still strips cleanly. Character classes `[…]` are
// treated as opaque so `[^/.)]+` doesn't terminate the scan early.
//
// Examples:
//
//	"group/(?P<group_id>[^/.]+)/x"           → "group/<group_id>/x"
//	"(?P<a>\\d+)/(?P<b>[a-z]+)"              → "<a>/<b>"
//	"(?P<a>(?:\\d+))"                        → "<a>"
//
// Inputs without any `(?P<` substring are returned unchanged so this is
// safe to call unconditionally on every Django / Flask path.
func stripPythonNamedGroups(raw string) string {
	const marker = "(?P<"
	if !strings.Contains(raw, marker) {
		return raw
	}
	var b strings.Builder
	b.Grow(len(raw))
	i := 0
	for i < len(raw) {
		// Locate the next `(?P<` opening from i.
		idx := strings.Index(raw[i:], marker)
		if idx < 0 {
			b.WriteString(raw[i:])
			break
		}
		// Copy everything up to the marker verbatim.
		b.WriteString(raw[i : i+idx])
		// Parse the group: (?P<NAME>BODY) where BODY may contain nested
		// parens (depth-tracked) and character classes (opaque).
		nameStart := i + idx + len(marker)
		nameEnd := strings.IndexByte(raw[nameStart:], '>')
		if nameEnd < 0 {
			// Malformed — bail and copy the rest as-is.
			b.WriteString(raw[i+idx:])
			break
		}
		name := raw[nameStart : nameStart+nameEnd]
		// Walk the body to find the balanced closing `)`.
		bodyStart := nameStart + nameEnd + 1
		depth := 1
		j := bodyStart
		for j < len(raw) && depth > 0 {
			c := raw[j]
			switch c {
			case '\\':
				// Skip an escaped char so `\)` doesn't decrement depth.
				j += 2
				continue
			case '[':
				// Character class — find its matching `]`. Inside a class,
				// a leading `]` is literal, and `\` still escapes.
				j++
				if j < len(raw) && raw[j] == ']' {
					j++
				}
				for j < len(raw) && raw[j] != ']' {
					if raw[j] == '\\' && j+1 < len(raw) {
						j += 2
						continue
					}
					j++
				}
				if j < len(raw) {
					j++ // consume the closing `]`
				}
				continue
			case '(':
				depth++
			case ')':
				depth--
				if depth == 0 {
					j++ // consume closing `)`
				} else {
					j++
				}
				continue
			}
			j++
		}
		// Emit `<name>` and advance past the closing `)`.
		b.WriteByte('<')
		b.WriteString(name)
		b.WriteByte('>')
		i = j
	}
	return b.String()
}

// canonicalizeAngleBrackets rewrites `<converter:name>` and `<name>` to
// `{name}`. Used for Django and Flask which both use angle-bracket syntax
// with optional converter prefixes (Django: `int`, `str`, `slug`, `uuid`,
// `path`; Flask: `int`, `float`, `path`, `uuid`, `string` — default).
// stripBottleFilters rewrites Bottle path params from `<name:filter>` to
// `<name>` (Bottle puts the name FIRST, then an optional `:filter` such as
// `:int` / `:re:[0-9]+` / `:path`). This inverts Flask's `<converter:name>`
// ordering, so it must run BEFORE the shared angle-bracket walker — which keeps
// the post-colon segment — to avoid mangling the param name into the filter.
// A bare `<name>` (no colon) passes through untouched.
func stripBottleFilters(raw string) string {
	var b strings.Builder
	b.Grow(len(raw))
	i := 0
	for i < len(raw) {
		c := raw[i]
		if c != '<' {
			b.WriteByte(c)
			i++
			continue
		}
		end := strings.IndexByte(raw[i+1:], '>')
		if end < 0 {
			b.WriteByte(c)
			i++
			continue
		}
		inner := raw[i+1 : i+1+end]
		if idx := strings.IndexByte(inner, ':'); idx >= 0 {
			inner = inner[:idx]
		}
		b.WriteByte('<')
		b.WriteString(strings.TrimSpace(inner))
		b.WriteByte('>')
		i += 1 + end + 1
	}
	return b.String()
}

func canonicalizeAngleBrackets(raw string) string {
	var b strings.Builder
	b.Grow(len(raw))
	i := 0
	for i < len(raw) {
		c := raw[i]
		if c != '<' {
			b.WriteByte(c)
			i++
			continue
		}
		// Find matching '>'. If none, treat as literal.
		end := strings.IndexByte(raw[i+1:], '>')
		if end < 0 {
			b.WriteByte(c)
			i++
			continue
		}
		inner := raw[i+1 : i+1+end]
		// `converter:name` -> `name`. Plain `name` stays.
		if idx := strings.IndexByte(inner, ':'); idx >= 0 {
			inner = inner[idx+1:]
		}
		// Defensive: drop any embedded regex specifiers (Django `re_path` uses
		// `(?P<name>regex)` style but those don't pass through here — they're
		// handled by the django_routes AST pass).
		inner = strings.TrimSpace(inner)
		if inner == "" {
			b.WriteString("{}")
		} else {
			b.WriteByte('{')
			b.WriteString(inner)
			b.WriteByte('}')
		}
		i += 1 + end + 1
	}
	return b.String()
}

// canonicalizeCurlyBraces strips the optional regex constraint from
// `{name:regex}` forms (used by Spring MVC) and leaves bare `{name}` alone.
// FastAPI and JAX-RS already use `{name}` natively so this is mostly a
// pass-through there.
func canonicalizeCurlyBraces(raw string) string {
	var b strings.Builder
	b.Grow(len(raw))
	i := 0
	for i < len(raw) {
		c := raw[i]
		if c != '{' {
			b.WriteByte(c)
			i++
			continue
		}
		end := strings.IndexByte(raw[i+1:], '}')
		if end < 0 {
			b.WriteByte(c)
			i++
			continue
		}
		inner := raw[i+1 : i+1+end]
		// Drop a `:regex` suffix if present.
		if idx := strings.IndexByte(inner, ':'); idx >= 0 {
			inner = inner[:idx]
		}
		inner = strings.TrimSpace(inner)
		if inner == "" {
			b.WriteString("{}")
		} else {
			b.WriteByte('{')
			b.WriteString(inner)
			b.WriteByte('}')
		}
		i += 1 + end + 1
	}
	return b.String()
}

// canonicalizeColonParams rewrites Express-style `:name` and `:name?` to
// `{name}`. Phase 1 drops the optional `?` marker — we treat optional and
// required path params as the same endpoint shape for cross-repo matching.
func canonicalizeColonParams(raw string) string {
	var b strings.Builder
	b.Grow(len(raw))
	i := 0
	for i < len(raw) {
		c := raw[i]
		if c != ':' {
			b.WriteByte(c)
			i++
			continue
		}
		// Consume the parameter name: [A-Za-z_][A-Za-z0-9_]*.
		j := i + 1
		for j < len(raw) && isIdentChar(raw[j]) {
			j++
		}
		if j == i+1 {
			b.WriteByte(c)
			i++
			continue
		}
		name := raw[i+1 : j]
		b.WriteByte('{')
		b.WriteString(name)
		b.WriteByte('}')
		i = j
		// Drop trailing `?` (optional marker) without altering the rest.
		if i < len(raw) && raw[i] == '?' {
			i++
		}
	}
	return b.String()
}

// canonicalizeJesterParams rewrites Nim Jester `@name` AT-prefixed path
// parameters to the shared `{name}` form (`/users/@id` → `/users/{id}`). It is
// the AT-prefixed analog of canonicalizeColonParams: a `@` followed by an
// identifier (`[A-Za-z_][A-Za-z0-9_]*`) becomes `{name}`; a lone `@` or a `@`
// not followed by an identifier byte is left verbatim. Jester also supports a
// `re"…"` regex segment and a trailing greedy `@param` that the segment matcher
// already wildcards, so no special handling is needed here beyond the
// per-parameter rewrite.
func canonicalizeJesterParams(raw string) string {
	var b strings.Builder
	b.Grow(len(raw))
	i := 0
	for i < len(raw) {
		c := raw[i]
		if c != '@' {
			b.WriteByte(c)
			i++
			continue
		}
		j := i + 1
		for j < len(raw) && isIdentChar(raw[j]) {
			j++
		}
		if j == i+1 {
			b.WriteByte(c)
			i++
			continue
		}
		b.WriteByte('{')
		b.WriteString(raw[i+1 : j])
		b.WriteByte('}')
		i = j
	}
	return b.String()
}

// canonicalizeShelfParams rewrites Dart shelf_router `<name>` / `<name|regex>`
// angle-bracket path parameters to the shared `{name}` form. shelf_router
// supports an optional inline regex constraint after a `|` pipe
// (`<id|[0-9]+>`); the constraint is dropped and only the parameter name is
// kept (`/users/<id|[0-9]+>` → `/users/{id}`). A lone `<` with no matching `>`
// is left verbatim; an empty `<>` becomes `{}`.
func canonicalizeShelfParams(raw string) string {
	var b strings.Builder
	b.Grow(len(raw))
	i := 0
	for i < len(raw) {
		c := raw[i]
		if c != '<' {
			b.WriteByte(c)
			i++
			continue
		}
		end := strings.IndexByte(raw[i+1:], '>')
		if end < 0 {
			b.WriteByte(c)
			i++
			continue
		}
		inner := raw[i+1 : i+1+end]
		// Drop a `|regex` constraint tail (shelf_router's inline regex syntax).
		if idx := strings.IndexByte(inner, '|'); idx >= 0 {
			inner = inner[:idx]
		}
		inner = strings.TrimSpace(inner)
		if inner == "" {
			b.WriteString("{}")
		} else {
			b.WriteByte('{')
			b.WriteString(inner)
			b.WriteByte('}')
		}
		i += 1 + end + 1
	}
	return b.String()
}

// canonicalizeConduitParams rewrites Dart Conduit path parameters to the shared
// `{name}` form. Conduit declares a REQUIRED param as `:name` and an OPTIONAL
// param as `[:name]` (square-bracket wrapped). The optional `[ ]` wrapper is
// dropped first, then the colon walker rewrites every `:name` → `{name}`
// (`/users/[:id]` → `/users/{id}`, `/a/:b` → `/a/{b}`). Bare `[`/`]` that do not
// wrap a colon param are removed (Conduit uses them only for optionality).
func canonicalizeConduitParams(raw string) string {
	// Strip the optional-segment square brackets; the param shape that remains
	// (`:name`) is handled by the shared colon walker.
	cleaned := strings.NewReplacer("[", "", "]", "").Replace(raw)
	return canonicalizeColonParams(cleaned)
}

// canonicalizeGiraffeFormat rewrites Giraffe `routef` printf-style typed
// placeholders into the canonical `{}` positional-wildcard form. Giraffe
// supports `%b` (bool), `%c` (char), `%s` (string), `%i` (int), `%d` (int64),
// `%u` (uint64), `%f` (float), and `%O` (Guid) — a `%` immediately followed by
// one of these format characters becomes a single `{}` wildcard segment-token,
// so `"/users/%i"` → `"/users/{}"` and `"/x/%s/%i"` → `"/x/{}/{}"`. A literal
// `%%` (escaped percent) collapses to a single `%`. Any other `%X` (not a known
// format char) is left intact so a stray percent in a static path is not eaten.
func canonicalizeGiraffeFormat(raw string) string {
	if !strings.Contains(raw, "%") {
		return raw
	}
	var b strings.Builder
	b.Grow(len(raw))
	i := 0
	for i < len(raw) {
		if raw[i] != '%' || i+1 >= len(raw) {
			b.WriteByte(raw[i])
			i++
			continue
		}
		n := raw[i+1]
		if n == '%' {
			b.WriteByte('%')
			i += 2
			continue
		}
		switch n {
		case 'b', 'c', 's', 'i', 'd', 'u', 'f', 'O':
			b.WriteString("{}")
			i += 2
		default:
			b.WriteByte('%')
			i++
		}
	}
	return b.String()
}

// isIdentChar reports whether c can appear in an identifier (after the
// first character).
func isIdentChar(c byte) bool {
	switch {
	case c >= 'a' && c <= 'z':
		return true
	case c >= 'A' && c <= 'Z':
		return true
	case c >= '0' && c <= '9':
		return true
	case c == '_':
		return true
	default:
		return false
	}
}

// normaliseSlashes ensures the path starts with exactly one `/` and has no
// trailing `/` (except for the bare root `/`). Internal duplicate slashes
// (e.g. `/api//users`) are collapsed.
func normaliseSlashes(p string) string {
	if p == "" {
		return "/"
	}
	// Ensure leading slash.
	if p[0] != '/' {
		p = "/" + p
	}
	// Collapse internal duplicate slashes.
	for strings.Contains(p, "//") {
		p = strings.ReplaceAll(p, "//", "/")
	}
	// Strip trailing slash (but keep root).
	if len(p) > 1 && p[len(p)-1] == '/' {
		p = strings.TrimRight(p, "/")
		if p == "" {
			p = "/"
		}
	}
	return p
}

// SyntheticID builds the canonical synthetic-entity ID for an HTTP endpoint.
// Format: `http:<METHOD>:<canonical-path>` with METHOD upper-cased and path
// already canonicalised. Method `ANY` (case-insensitive) is preserved so
// callers can distinguish method-agnostic registrations from a specific
// verb.
func SyntheticID(method, canonicalPath string) string {
	return "http:" + strings.ToUpper(method) + ":" + canonicalPath
}
