// http_endpoint_csharp_minor.go — Carter / FastEndpoints / NancyFX / ServiceStack
// → canonical http_endpoint_definition synthesis (#3962, epic #3872).
//
// These four .NET HTTP frameworks were previously served ONLY by the regex-only
// extractor internal/custom/csharp/minor_routes.go, which emits raw
// SCOPE.Operation / SCOPE.Component entities (notes: "extracted via regex;
// heuristic"). They never reached the canonical synthesis path that ASP.NET Core
// gets from synthesizeASPNetCore (#2692), so their Routing cells
// (endpoint_synthesis / route_extraction / handler_attribution) stayed `partial`
// while aspnet-core was `full`, and no http_endpoint_definition node was ever
// produced — meaning cross-repo linking, response-shape/auth stamping and the
// request/response-shape substrate (which keys off the synthesized endpoint)
// could never join to these endpoints.
//
// This synthesizer promotes all four to the SAME canonical endpoint shape
// synthesizeASPNetCore emits — `http:<VERB>:<path>` with a
// `source_handler=SCOPE.Operation:<Class>.<Method>` attribution that
// ResolveHTTPEndpointHandlers rebinds to the handler method (HANDLES edge) —
// using each framework's idiomatic route-declaration syntax:
//
//   - Carter:        inside an `ICarterModule` whose `AddRoutes` method calls
//     `app.MapGet("/path", ...)` / `MapPost` / `MapPut` /
//     `MapDelete` / `MapPatch`. The enclosing module class is the
//     handler (`<Module>.AddRoutes`); the minimal-API lambda has
//     no named method, so the AddRoutes site is the attribution.
//
//   - FastEndpoints: `class X : Endpoint<TReq[, TRes]>` whose `Configure()` body
//     calls `Get("/path")` / `Post(...)` / ... . The endpoint
//     class IS the handler — attribution is `<Class>.HandleAsync`
//     (FastEndpoints' fixed handler-method name), or `<Class>`
//     when no HandleAsync is present.
//
//   - NancyFX:       inside a `: NancyModule` subclass, route registrations are
//     `Get["/path"]` (index syntax, Nancy 1.x) or `Get("/path")`
//     (call syntax, Nancy 2.x). Both are gated on the module's
//     constructor being the handler (`<Module>` attribution — the
//     route bodies are inline lambdas).
//
//   - ServiceStack:  a `[Route("/path", "GET POST")]` attribute decorates a
//     request DTO; the matching `class XService : Service` exposes
//     `Any/Get/Post(dto)` handler methods. The verb list comes
//     from the Route attribute (defaulting to the handler methods
//     present, else ANY→GET). Attribution is `<Service>.<Verb>`.
//
// The route-template `{id}` placeholders use the same canonicaliser as ASP.NET
// Core (FrameworkASPNetCore), so `/widgets/{id:int}` → `/widgets/{id}` matches
// the cross-stack endpoint shape. Detection is gated per-framework on a cheap
// file-signal so the synthesizer no-ops on plain ASP.NET Core / gRPC / Blazor
// C# files (which synthesizeASPNetCore / synthesizeHotChocolate already own).
//
// Refs #3962 (epic #3872, audit #3882). archigraph-csharp-parity.md §1a.
package engine

import (
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/engine/httproutes"
)

// ---------------------------------------------------------------------------
// File signals — fast pre-filters per framework.
// ---------------------------------------------------------------------------

// carterHasSignal reports a Carter file: the `ICarterModule` interface or the
// `using Carter` import.
func carterHasSignal(content string) bool {
	return strings.Contains(content, "ICarterModule") ||
		strings.Contains(content, "using Carter")
}

// fastEndpointsHasSignal reports a FastEndpoints file: an `Endpoint<` base
// class or the `using FastEndpoints` import.
func fastEndpointsHasSignal(content string) bool {
	return strings.Contains(content, "using FastEndpoints") ||
		strings.Contains(content, ": Endpoint<") ||
		strings.Contains(content, ":Endpoint<")
}

// nancyHasSignal reports a NancyFX file: the `NancyModule` base class or the
// `using Nancy` import.
func nancyHasSignal(content string) bool {
	return strings.Contains(content, "NancyModule") ||
		strings.Contains(content, "using Nancy")
}

// serviceStackHasSignal reports a ServiceStack file: the `using ServiceStack`
// import (the `: Service` base class alone is too generic to gate on).
func serviceStackHasSignal(content string) bool {
	return strings.Contains(content, "using ServiceStack")
}

// ---------------------------------------------------------------------------
// Carter
// ---------------------------------------------------------------------------

// carterModuleRe matches `class X : ICarterModule` (allowing other bases).
// Capture group 1 = module class name.
var carterModuleRe = regexp.MustCompile(
	`(?m)class\s+(\w+)\s*:\s*(?:\w+\s*,\s*)*ICarterModule\b`,
)

// carterMapRouteRe matches `app.MapGet("/path", ...)` etc. (any receiver
// identifier, not just `app`). Capture group 1 = verb; group 2 = path.
var carterMapRouteRe = regexp.MustCompile(
	`\b\w+\.Map(Get|Post|Put|Delete|Patch)\s*\(\s*["']([^"']+)["']`,
)

// synthesizeCarter emits one canonical endpoint per `app.MapVerb("/path")`
// route declared in an ICarterModule file, attributed to the enclosing
// module's AddRoutes method.
func synthesizeCarter(content string, emit emitFn) {
	if !carterHasSignal(content) {
		return
	}
	module := ""
	if m := carterModuleRe.FindStringSubmatch(content); len(m) >= 2 {
		module = m[1]
	}
	if module == "" {
		// No ICarterModule class in this file — the Map* calls (if any) are
		// plain minimal-API, owned by a future minimal-API synthesizer, not
		// Carter. Stay conservative and emit nothing.
		return
	}
	handler := module + ".AddRoutes"
	for _, m := range carterMapRouteRe.FindAllStringSubmatch(content, -1) {
		verb := strings.ToUpper(m[1])
		canonical := httproutes.Canonicalize(httproutes.FrameworkASPNetCore, m[2])
		if canonical == "" {
			continue
		}
		emit(verb, canonical, "carter", "SCOPE.Operation", handler)
	}
}

// ---------------------------------------------------------------------------
// FastEndpoints
// ---------------------------------------------------------------------------

// feEndpointClassRe matches `class X : Endpoint<...>` (with optional generics
// and additional interfaces). Capture group 1 = endpoint class name.
var feEndpointClassRe = regexp.MustCompile(
	`(?m)class\s+(\w+)\s*:\s*Endpoint\s*<`,
)

// feRouteRe matches a verb route registration `Get("/path")` / `Post("/path")`
// inside an endpoint's Configure() body. Capture group 1 = verb; group 2 = path.
// The leading boundary `(?:[^.\w]|^)` excludes `client.Get(...)`-style consumer
// calls (those have a `.` receiver) — FastEndpoints' Configure() calls the
// verb helpers bare (`Get("/x")`, `Verbs(...)`).
var feRouteRe = regexp.MustCompile(
	`(?m)(?:[^.\w]|^)(Get|Post|Put|Delete|Patch)\s*\(\s*["']([^"']+)["']`,
)

// synthesizeFastEndpoints emits one canonical endpoint per verb route declared
// inside a `class X : Endpoint<...>` file. Each endpoint class owns its routes;
// when the file declares a single endpoint class, all bare verb routes attribute
// to it. The handler is the class's `HandleAsync` (FastEndpoints' fixed handler
// method) when present, else the class itself.
func synthesizeFastEndpoints(content string, emit emitFn) {
	if !fastEndpointsHasSignal(content) {
		return
	}
	classes := feEndpointClassRe.FindAllStringSubmatchIndex(content, -1)
	if len(classes) == 0 {
		return
	}
	// Attribute each bare verb route to the endpoint class whose declaration
	// most-immediately precedes it (single-endpoint-per-file is the dominant
	// case; multiple endpoints per file resolve by nearest-preceding class).
	for _, rm := range feRouteRe.FindAllStringSubmatchIndex(content, -1) {
		verb := strings.ToUpper(content[rm[2]:rm[3]])
		path := content[rm[4]:rm[5]]
		canonical := httproutes.Canonicalize(httproutes.FrameworkASPNetCore, path)
		if canonical == "" {
			continue
		}
		className := feNearestPrecedingClass(classes, content, rm[0])
		if className == "" {
			continue
		}
		handler := className
		if strings.Contains(feClassBody(content, classes, className), "HandleAsync") {
			handler = className + ".HandleAsync"
		}
		emit(verb, canonical, "fastendpoints", "SCOPE.Operation", handler)
	}
}

// feNearestPrecedingClass returns the name of the endpoint class whose match
// start is the greatest that is still ≤ off. Returns "" when off precedes every
// class (route before any endpoint declaration).
func feNearestPrecedingClass(classes [][]int, content string, off int) string {
	name := ""
	best := -1
	for _, c := range classes {
		if c[0] <= off && c[0] > best {
			best = c[0]
			name = content[c[2]:c[3]]
		}
	}
	return name
}

// feClassBody returns the source from the named class declaration to the next
// endpoint-class declaration (or EOF), used only to test for a `HandleAsync`
// handler method. Approximate (no brace-balance) but sufficient for the
// handler-name heuristic.
func feClassBody(content string, classes [][]int, className string) string {
	start, end := -1, len(content)
	for i, c := range classes {
		if content[c[2]:c[3]] == className {
			start = c[0]
			if i+1 < len(classes) {
				end = classes[i+1][0]
			}
			break
		}
	}
	if start < 0 {
		return ""
	}
	return content[start:end]
}

// ---------------------------------------------------------------------------
// NancyFX
// ---------------------------------------------------------------------------

// nancyModuleRe matches `class X : NancyModule`. Capture group 1 = module name.
var nancyModuleRe = regexp.MustCompile(
	`(?m)class\s+(\w+)\s*:\s*NancyModule\b`,
)

// nancyIndexRouteRe matches `Get["/path"]` index-syntax routes (Nancy 1.x).
// Capture group 1 = verb; group 2 = path.
var nancyIndexRouteRe = regexp.MustCompile(
	`\b(Get|Post|Put|Delete|Patch|Head|Options)\s*\[\s*["']([^"']+)["']\s*\]`,
)

// nancyCallRouteRe matches `Get("/path", ...)` call-syntax routes (Nancy 2.x).
// Capture group 1 = verb; group 2 = path.
var nancyCallRouteRe = regexp.MustCompile(
	`\b(Get|Post|Put|Delete|Patch|Head|Options)\s*\(\s*["']([^"']+)["']`,
)

// synthesizeNancy emits one canonical endpoint per route registered inside a
// `: NancyModule` subclass (both index `Get["/x"]` and call `Get("/x")` styles).
// Attribution is the module class — Nancy routes are inline lambdas assigned in
// the module constructor, so the module IS the handler.
func synthesizeNancy(content string, emit emitFn) {
	if !nancyHasSignal(content) {
		return
	}
	m := nancyModuleRe.FindStringSubmatch(content)
	if len(m) < 2 {
		// No NancyModule class — the bare verb-call patterns would collide with
		// FastEndpoints / consumer calls; emit nothing without the module gate.
		return
	}
	module := m[1]
	emitRoute := func(verb, path string) {
		canonical := httproutes.Canonicalize(httproutes.FrameworkASPNetCore, path)
		if canonical == "" {
			return
		}
		emit(strings.ToUpper(verb), canonical, "nancyfx", "SCOPE.Operation", module)
	}
	for _, rm := range nancyIndexRouteRe.FindAllStringSubmatch(content, -1) {
		emitRoute(rm[1], rm[2])
	}
	// Call-syntax is only emitted inside a NancyModule file (the module gate
	// above) to avoid colliding with FastEndpoints' bare `Get("/x")`.
	for _, rm := range nancyCallRouteRe.FindAllStringSubmatch(content, -1) {
		emitRoute(rm[1], rm[2])
	}
}

// ---------------------------------------------------------------------------
// ServiceStack
// ---------------------------------------------------------------------------

// ssRouteAttrRe matches `[Route("/path")]` or `[Route("/path", "GET POST")]`.
// Capture group 1 = path; group 2 = optional verb string.
var ssRouteAttrRe = regexp.MustCompile(
	`\[Route\s*\(\s*["']([^"']+)["'](?:\s*,\s*["']([^"']*)["'])?\s*\)\s*\]`,
)

// ssServiceClassRe matches `class XService : Service` (or any base list ending
// in Service). Capture group 1 = service class name.
var ssServiceClassRe = regexp.MustCompile(
	`(?m)class\s+(\w+)\s*:\s*(?:\w+\s*,\s*)*Service\b`,
)

// ssHandlerMethodRe matches a ServiceStack handler method `public T Any(...)` /
// `Get` / `Post` / ... . Capture group 1 = verb (Any/Get/Post/...).
var ssHandlerMethodRe = regexp.MustCompile(
	`(?m)public\s+\S+\s+(Any|Get|Post|Put|Delete|Patch)\s*\(`,
)

// synthesizeServiceStack emits canonical endpoints from `[Route("/path","VERBS")]`
// attributes. The verb list is taken from the attribute when present; otherwise
// it is inferred from the handler methods present on the service class (`Any`
// expands to GET+POST+PUT+DELETE? — kept conservative as a single ANY→GET), and
// finally defaults to GET. Attribution is `<Service>.<Verb>` when a service class
// exists in the file, else the request-DTO-derived route stands alone.
func synthesizeServiceStack(content string, emit emitFn) {
	if !serviceStackHasSignal(content) {
		return
	}
	routes := ssRouteAttrRe.FindAllStringSubmatch(content, -1)
	if len(routes) == 0 {
		return
	}
	service := ""
	if m := ssServiceClassRe.FindStringSubmatch(content); len(m) >= 2 {
		service = m[1]
	}
	// Raw handler-method names present on the service (Any/Get/Post/...), used
	// both to infer verbs when a Route attribute omits its verb string and to
	// pick the handler-method name attributed to each emitted endpoint.
	handlerMethods := ssHandlerMethodNames(content)
	handlerVerbs := ssVerbsFromHandlers(handlerMethods)

	for _, rm := range routes {
		path := rm[1]
		canonical := httproutes.Canonicalize(httproutes.FrameworkASPNetCore, path)
		if canonical == "" {
			continue
		}
		verbs := ssParseRouteVerbs(rm[2])
		if len(verbs) == 0 {
			verbs = handlerVerbs
		}
		if len(verbs) == 0 {
			verbs = []string{"GET"}
		}
		for _, verb := range verbs {
			handler := ""
			if service != "" {
				handler = service + "." + ssHandlerForVerb(verb, handlerMethods)
			}
			if handler != "" {
				emit(verb, canonical, "servicestack", "SCOPE.Operation", handler)
			} else {
				emit(verb, canonical, "servicestack", "", "")
			}
		}
	}
}

// ssParseRouteVerbs splits a ServiceStack Route verb string ("GET POST") into
// upper-cased tokens. An empty or "ANY" verb string returns nil so the caller
// falls back to handler-derived verbs.
func ssParseRouteVerbs(verbStr string) []string {
	verbStr = strings.TrimSpace(verbStr)
	if verbStr == "" || strings.EqualFold(verbStr, "ANY") {
		return nil
	}
	var out []string
	for _, p := range strings.Fields(verbStr) {
		if up := strings.ToUpper(p); up != "" {
			out = append(out, up)
		}
	}
	return out
}

// ssHandlerMethodNames returns the raw handler-method names present on the
// service (the literal `Any` / `Get` / `Post` / ... method identifiers),
// de-duplicated and in source order.
func ssHandlerMethodNames(content string) []string {
	seen := map[string]bool{}
	var out []string
	for _, m := range ssHandlerMethodRe.FindAllStringSubmatch(content, -1) {
		// m[1] is already a Title-cased method token (Any/Get/Post/...).
		name := m[1]
		if !seen[name] {
			seen[name] = true
			out = append(out, name)
		}
	}
	return out
}

// ssVerbsFromHandlers maps handler-method names to the HTTP verbs they serve.
// A literal `Any` method is the catch-all, conservatively counted as GET (the
// Route attribute is the authoritative verb source when present, so this only
// matters for attribute-less routes).
func ssVerbsFromHandlers(handlerMethods []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, name := range handlerMethods {
		verb := strings.ToUpper(name)
		if verb == "ANY" {
			verb = "GET"
		}
		if !seen[verb] {
			seen[verb] = true
			out = append(out, verb)
		}
	}
	return out
}

// ssHandlerForVerb returns the handler-method name that serves the given verb:
// the verb's own `Get`/`Post`/... method when the service declares one,
// otherwise `Any` (ServiceStack's catch-all handler). When the service declares
// NEITHER a verb-specific method NOR `Any`, the verb-titled name is returned as
// a best-effort attribution target.
func ssHandlerForVerb(verb string, handlerMethods []string) string {
	titled := strings.Title(strings.ToLower(verb)) //nolint:staticcheck // ASCII verb tokens; Title is fine.
	hasAny := false
	for _, name := range handlerMethods {
		if name == titled {
			return titled
		}
		if name == "Any" {
			hasAny = true
		}
	}
	if hasAny {
		return "Any"
	}
	return titled
}
