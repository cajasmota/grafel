// http_endpoint_python_middleware.go — ordered middleware-chain binding for the
// Python backend-HTTP synthesizers (Django / DRF / FastAPI), child of #3628.
//
// This pass brings Python endpoints to parity with the Go (#3777) and JS/TS
// (#2853) middleware passes: it resolves a structured, ORDERED middleware chain
// and stamps it on every synthetic http_endpoint_definition this file emitted,
// using the shared cross-stack contract (see http_endpoint_middleware_chain.go).
//
// Resolved scopes (request-traversal order, OUTERMOST-first):
//
//	global — Django `settings.MIDDLEWARE = [...]` (the framework-wide ordered
//	         pipeline) and FastAPI `app.add_middleware(X)` registrations. These
//	         wrap every route, so they are the outermost entries.
//	view   — DRF view/ViewSet class attributes `permission_classes` /
//	         `authentication_classes` / `throttle_classes`. They apply to every
//	         endpoint served by that view class.
//	route  — FastAPI per-route `dependencies=[Depends(...)]` on the route
//	         decorator. Innermost — runs last before the handler.
//
// Honest-partial: a MIDDLEWARE list assembled at runtime (a comprehension, a
// `+ EXTRA` concat, an `if DEBUG:` conditional append) is not statically
// resolvable and is skipped. DRF view attributes only bind when the view class
// is defined in the SAME file as its endpoints (cross-file ViewSet attribution
// is out of scope — no fabricated binding). FastAPI dependencies bind only to
// the decorated route they sit on.
package engine

import (
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/engine/httproutes"
	"github.com/cajasmota/archigraph/internal/types"
)

// pythonMWScope* name the three Python middleware scopes, outermost-first.
const (
	pythonMWScopeGlobal = "global"
	pythonMWScopeView   = "view"
	pythonMWScopeRoute  = "route"
)

// pythonMWScopeOrder lists the Python scopes outermost-first for the
// middleware_scope rendering.
var pythonMWScopeOrder = []string{pythonMWScopeGlobal, pythonMWScopeView, pythonMWScopeRoute}

// djangoMiddlewareListRe captures the `MIDDLEWARE = [ ... ]` settings list. It
// requires a STATIC list literal: a `MIDDLEWARE` whose value is a `[...]` whose
// body is purely string literals/commas/whitespace. A list built dynamically
// (comprehension, `+ EXTRA`, function call) will not match the inner shape and
// is skipped (honest-partial). Group 1 = the raw list body.
var djangoMiddlewareListRe = regexp.MustCompile(`(?s)\bMIDDLEWARE(?:_CLASSES)?\s*=\s*\[([^\]]*)\]`)

// pyDottedStringTokenRe captures a single-/double-quoted string literal token
// (used to extract the dotted middleware paths from a MIDDLEWARE list and the
// throttle/permission class names where given as strings).
var pyDottedStringTokenRe = regexp.MustCompile(`["']([^"'\n\r]+)["']`)

// fastapiAddMiddlewareRe captures `app.add_middleware(CORSMiddleware, ...)` /
// `application.add_middleware(GZipMiddleware)`. Group 1 = the middleware class
// (first positional argument).
var fastapiAddMiddlewareRe = regexp.MustCompile(`\.add_middleware\s*\(\s*([A-Za-z_][\w.]*)`)

// fastapiDependsRe captures one `Depends(<callable>)` inside a dependencies list.
// Group 1 = the dependency callable expression.
var fastapiDependsRe = regexp.MustCompile(`Depends\s*\(\s*([A-Za-z_][\w.]*)`)

// drfClassDeclRe captures a DRF view/ViewSet/APIView class declaration. Group 1
// = class name, group 2 = the base-class list (used to confirm it is a DRF view
// and to recover its name). Only classes whose bases look like a DRF view are
// considered; the body is then scanned for the *_classes attributes.
var drfClassDeclRe = regexp.MustCompile(`(?m)^class\s+([A-Za-z_]\w*)\s*\(([^)]*)\)\s*:`)

// drfClassesAttrRe captures a `permission_classes = [...]` /
// `authentication_classes = (...)` / `throttle_classes = [...]` assignment.
// Group 1 = the attribute kind, group 2 = the raw list body.
var drfClassesAttrRe = regexp.MustCompile(`(?s)\b(permission|authentication|throttle)_classes\s*=\s*[\[(]([^\])]*)[\])]`)

// applyPythonMiddlewareCoverage resolves and stamps the ordered middleware chain
// on every Python synthetic backend endpoint emitted for this file. Like the
// JS/TS and Go passes it mutates Properties in place and never adds or removes
// entities. `before` is the entity-slice length captured before the Python
// synthesizers ran; only http_endpoint_definition entities at index >= before
// that belong to this file are considered.
func applyPythonMiddlewareCoverage(content, path string, entities []types.EntityRecord, before int) {
	if len(content) == 0 || before >= len(entities) {
		return
	}

	global := indexPythonGlobalMiddleware(content)
	viewByHandler, viewByClass := indexDRFViewMiddleware(content)
	// Map DRF router-registered prefix → the view chain, when the ViewSet class
	// is defined in THIS file (same-file honest-partial). A `router.register(
	// r'reports', ReportViewSet)` ties the URL prefix to the class, so a
	// composed route path ending in `/reports` (or `/reports/{...}`) inherits
	// the class chain even though the endpoint's source_handler is the route
	// path rather than the action method.
	viewByPrefix := indexDRFRouterPrefixes(content, viewByClass)
	routeDeps := indexFastAPIRouteDependencies(content)

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

		var chain []middlewareEntry
		// global (outermost): Django MIDDLEWARE / FastAPI add_middleware.
		chain = append(chain, global...)
		// view: DRF *_classes on the view serving this endpoint — bound either by
		// the action handler method name (when the endpoint references it) or by
		// the router-registered URL prefix (Django composed routes).
		handler := pythonHandlerName(e.Properties["source_handler"])
		if handler != "" {
			chain = append(chain, viewByHandler[handler]...)
		}
		chain = append(chain, drfPrefixChainFor(canonical, viewByPrefix)...)
		// route (innermost): FastAPI per-route dependencies, keyed by handler
		// (the def name) and by "<VERB> <path>".
		if handler != "" {
			chain = append(chain, routeDeps[handler]...)
		}
		chain = append(chain, routeDeps[verb+" "+canonical]...)

		chain = dedupeMiddlewareEntries(chain)
		stampMiddlewareChainEntries(e.Properties, chain, pythonMWScopeOrder)
	}
}

// drfRouterRegisterRe captures a `router.register(r"prefix", ViewSetClass)` call.
// Group 1 = URL prefix, group 2 = ViewSet class name.
var drfRouterRegisterRe = regexp.MustCompile(
	`\.register\s*\(\s*r?["']([^"'\n\r]+)["']\s*,\s*([A-Za-z_]\w*)`,
)

// indexDRFRouterPrefixes maps each router-registered URL prefix to the view
// chain of the ViewSet class it registers (when that class is same-file). The
// prefix is normalized to a leading-slash, trailing-slash-trimmed form.
func indexDRFRouterPrefixes(content string, viewByClass map[string][]middlewareEntry) map[string][]middlewareEntry {
	out := map[string][]middlewareEntry{}
	if len(viewByClass) == 0 || !strings.Contains(content, ".register(") {
		return out
	}
	for _, m := range drfRouterRegisterRe.FindAllStringSubmatch(content, -1) {
		prefix := "/" + strings.Trim(strings.TrimSpace(m[1]), "/")
		cls := strings.TrimSpace(m[2])
		if chain, ok := viewByClass[cls]; ok {
			out[prefix] = chain
		}
	}
	return out
}

// drfPrefixChainFor returns the view chain bound to the DRF router prefix that a
// canonical endpoint path belongs to. A path matches a prefix when it equals the
// prefix, or sits directly under it (`/api/reports`, `/api/reports/{pk}` both
// match the `/reports` registration regardless of the mount segment).
func drfPrefixChainFor(canonical string, viewByPrefix map[string][]middlewareEntry) []middlewareEntry {
	if len(viewByPrefix) == 0 {
		return nil
	}
	for prefix, chain := range viewByPrefix {
		if prefix == "/" {
			continue
		}
		// Match the prefix as a path segment: `<...>/reports` or `<...>/reports/<...>`.
		if canonical == prefix ||
			strings.HasSuffix(canonical, prefix) ||
			strings.Contains(canonical, prefix+"/") {
			return chain
		}
	}
	return nil
}

// indexPythonGlobalMiddleware resolves the global-scope chain: the Django
// `MIDDLEWARE` settings list (in declaration order) and every FastAPI
// `add_middleware(X)` registration. Returns the ordered, scope-tagged entries.
func indexPythonGlobalMiddleware(content string) []middlewareEntry {
	var out []middlewareEntry

	// Django MIDDLEWARE = [ '...SecurityMiddleware', ... ] — ordered pipeline.
	if m := djangoMiddlewareListRe.FindStringSubmatch(content); m != nil {
		for _, q := range pyDottedStringTokenRe.FindAllStringSubmatch(m[1], -1) {
			dotted := strings.TrimSpace(q[1])
			if dotted == "" {
				continue
			}
			name := dotted
			if idx := strings.LastIndex(name, "."); idx >= 0 {
				name = name[idx+1:]
			}
			out = append(out, middlewareEntry{
				Name:     name,
				Expr:     dotted,
				Scope:    pythonMWScopeGlobal,
				AuthKind: middlewareAuthKind(name),
			})
		}
	}

	// FastAPI app.add_middleware(X) — registration order is request order from
	// the OUTSIDE in for the LAST-added (Starlette wraps in reverse), but to
	// stay honest and consistent with the declarative Django list we record
	// registrations in source order. Each is a distinct global middleware.
	for _, m := range fastapiAddMiddlewareRe.FindAllStringSubmatch(content, -1) {
		cls := strings.TrimSpace(m[1])
		name := cls
		if idx := strings.LastIndex(name, "."); idx >= 0 {
			name = name[idx+1:]
		}
		out = append(out, middlewareEntry{
			Name:     name,
			Expr:     cls,
			Scope:    pythonMWScopeGlobal,
			AuthKind: middlewareAuthKind(name),
		})
	}

	return out
}

// indexDRFViewMiddleware scans every DRF view/ViewSet/APIView class body for the
// permission_classes / authentication_classes / throttle_classes attributes and
// returns two maps: by lowercase CRUD handler method name (so a ViewSet's
// inherited list/retrieve endpoints inherit the class chain) and by class name.
//
// Only classes whose base list names a DRF view convention are considered, so a
// plain domain class with a `permission_classes` attribute is not mistaken for a
// view. The per-handler map keys on the canonical DRF action method names so the
// synthesized endpoints (whose source_handler is the action) bind.
func indexDRFViewMiddleware(content string) (byHandler, byClass map[string][]middlewareEntry) {
	byHandler = map[string][]middlewareEntry{}
	byClass = map[string][]middlewareEntry{}
	if !strings.Contains(content, "_classes") {
		return byHandler, byClass
	}

	decls := drfClassDeclRe.FindAllStringSubmatchIndex(content, -1)
	for ci, loc := range decls {
		className := content[loc[2]:loc[3]]
		bases := content[loc[4]:loc[5]]
		if !drfIsViewBase(bases) {
			continue
		}
		// Body spans from this class decl to the next top-level class decl.
		bodyStart := loc[1]
		bodyEnd := len(content)
		if ci+1 < len(decls) {
			bodyEnd = decls[ci+1][0]
		}
		body := content[bodyStart:bodyEnd]

		var chain []middlewareEntry
		for _, am := range drfClassesAttrRe.FindAllStringSubmatch(body, -1) {
			kind := am[1] // permission | authentication | throttle
			for _, sym := range drfClassNames(am[2]) {
				name := sym
				if idx := strings.LastIndex(name, "."); idx >= 0 {
					name = name[idx+1:]
				}
				ak := ""
				if kind == "permission" || kind == "authentication" {
					ak = middlewareAuthKind(name)
					if ak == "" {
						ak = "auth"
					}
				}
				chain = append(chain, middlewareEntry{
					Name:     name,
					Expr:     sym,
					Scope:    pythonMWScopeView,
					AuthKind: ak,
				})
			}
		}
		if len(chain) == 0 {
			continue
		}
		byClass[className] = chain
		// Bind to every DRF action method name so the action-expanded endpoints
		// (list/retrieve/create/update/partial_update/destroy + the @action
		// methods declared in the body) inherit the view chain.
		for _, h := range drfHandlerMethods(body) {
			byHandler[h] = append(byHandler[h], chain...)
		}
	}
	return byHandler, byClass
}

// drfIsViewBase reports whether a class base list names a DRF view convention
// (APIView, GenericAPIView, *ViewSet, ViewSet, or a generics.* base).
func drfIsViewBase(bases string) bool {
	l := bases
	return strings.Contains(l, "APIView") ||
		strings.Contains(l, "ViewSet") ||
		strings.Contains(l, "GenericAPIView") ||
		strings.Contains(l, "generics.") ||
		strings.Contains(l, "ModelViewSet") ||
		strings.Contains(l, "mixins.")
}

// drfClassNames extracts the individual class symbols from a *_classes list
// body. Entries are bare identifiers / dotted refs (`IsAuthenticated`,
// `permissions.IsAdminUser`); trailing `()` instantiations are tolerated.
func drfClassNames(body string) []string {
	var out []string
	for _, m := range pyIdentRefRe.FindAllString(body, -1) {
		tok := strings.TrimSpace(m)
		if tok == "" {
			continue
		}
		out = append(out, tok)
	}
	return out
}

// pyIdentRefRe matches a bare/dotted Python identifier reference used as a class
// in a *_classes list (e.g. `IsAuthenticated`, `permissions.IsAdminUser`).
var pyIdentRefRe = regexp.MustCompile(`[A-Za-z_][\w.]*`)

// drfActionMethods are the canonical DRF ViewSet CRUD action method names; an
// endpoint whose source_handler is one of these (lowercased) inherits its
// view's chain.
var drfActionMethods = []string{
	"list", "create", "retrieve", "update", "partial_update", "destroy",
	"get", "post", "put", "patch", "delete", "head", "options",
}

// drfHandlerMethods returns the action method names to bind a view chain to:
// the canonical CRUD/HTTP-verb actions plus any `def <name>(self` declared in
// the body (covers @action custom endpoints and explicit overrides).
func drfHandlerMethods(body string) []string {
	out := append([]string(nil), drfActionMethods...)
	for _, m := range pyDefSelfRe.FindAllStringSubmatch(body, -1) {
		out = append(out, m[1])
	}
	return out
}

// pyDefSelfRe captures a `def <name>(self` method declaration. Group 1 = name.
var pyDefSelfRe = regexp.MustCompile(`\bdef\s+([A-Za-z_]\w*)\s*\(\s*self\b`)

// indexFastAPIRouteDependencies scans FastAPI route decorators for a
// `dependencies=[Depends(...), ...]` kwarg and binds the resolved dependency
// callables to that route, keyed BOTH by the handler def name and by
// "<VERB> <canonical-path>" so the endpoint can match on either.
func indexFastAPIRouteDependencies(content string) map[string][]middlewareEntry {
	out := map[string][]middlewareEntry{}
	if !strings.Contains(content, "Depends(") {
		return out
	}
	for _, idx := range fastapiRouteWithDepsRe.FindAllStringSubmatchIndex(content, -1) {
		verb := strings.ToUpper(content[idx[2]:idx[3]])
		raw := content[idx[4]:idx[5]]
		tail := content[idx[6]:idx[7]] // decorator argument tail (may hold dependencies=)
		handler := content[idx[8]:idx[9]]

		deps := fastapiDependsList(tail)
		if len(deps) == 0 {
			continue
		}
		var entries []middlewareEntry
		for _, d := range deps {
			name := d
			if di := strings.LastIndex(name, "."); di >= 0 {
				name = name[di+1:]
			}
			entries = append(entries, middlewareEntry{
				Name:     name,
				Expr:     "Depends(" + d + ")",
				Scope:    pythonMWScopeRoute,
				AuthKind: middlewareAuthKind(name),
			})
		}
		out[handler] = append(out[handler], entries...)
		if canonical := canonicalFastAPIPath(raw); canonical != "" {
			out[verb+" "+canonical] = append(out[verb+" "+canonical], entries...)
		}
	}
	return out
}

// fastapiRouteWithDepsRe captures a FastAPI route decorator + its handler def,
// preserving the FULL decorator argument tail (group 3) so a multi-line
// `dependencies=[...]` kwarg is visible. Group 1 = verb, 2 = path, 3 = arg tail,
// 4 = handler def name.
var fastapiRouteWithDepsRe = regexp.MustCompile(
	`(?s)@(?:app|router|api|\w+_router)\.(get|post|put|patch|delete|head|options|trace)\s*\(\s*["']([^"'\n\r]+)["']([^)]*(?:\([^)]*\)[^)]*)*)\)\s*[\r\n]+(?:\s*@[^\n\r]*[\r\n]+)*\s*(?:async\s+)?def\s+(\w+)`,
)

// fastapiDependsList extracts every `Depends(<callable>)` from a decorator arg
// tail, but ONLY when it is inside a `dependencies=` kwarg (a `Depends(...)`
// used as a normal parameter default is a handler param, not route middleware).
func fastapiDependsList(tail string) []string {
	di := strings.Index(tail, "dependencies")
	if di < 0 {
		return nil
	}
	region := tail[di:]
	var out []string
	for _, m := range fastapiDependsRe.FindAllStringSubmatch(region, -1) {
		if tok := strings.TrimSpace(m[1]); tok != "" {
			out = append(out, tok)
		}
	}
	return out
}

// pythonHandlerName strips the `<refKind>:` prefix the synthesizers stamp on
// source_handler (e.g. "Controller:get_user" → "get_user") and lowercases it
// for DRF action-method matching.
func pythonHandlerName(ref string) string {
	if ref == "" {
		return ""
	}
	if idx := strings.Index(ref, ":"); idx >= 0 {
		ref = ref[idx+1:]
	}
	return strings.ToLower(strings.TrimSpace(ref))
}

// canonicalFastAPIPath canonicalizes a raw FastAPI route path the same way the
// synthesizer does, so the route-dependency key matches the stamped endpoint
// path.
func canonicalFastAPIPath(raw string) string {
	return httproutes.Canonicalize(httproutes.FrameworkFastAPI, raw)
}
