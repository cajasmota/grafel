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
	case FrameworkDjango, FrameworkFlask, FrameworkRocket, FrameworkSanic:
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
		FrameworkLitestar, FrameworkAiohttp:
		out = canonicalizeCurlyBraces(raw)
	case FrameworkTornado:
		// Tornado paths arrive already pre-processed by the synthesizer
		// (named-group → {name}, bare group → {}). Curly-brace pass
		// strips any stray `:regex` constraints, mirroring the other
		// curly-brace frameworks.
		out = canonicalizeCurlyBraces(raw)
	case FrameworkExpress, FrameworkGin, FrameworkEcho, FrameworkChi, FrameworkPhoenix,
		FrameworkAdonis, FrameworkMarble, FrameworkPolka, FrameworkRestify, FrameworkSails,
		FrameworkRobyn:
		out = canonicalizeColonParams(raw)
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
