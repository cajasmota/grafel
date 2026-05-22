// Synthetic http_endpoint entity emission for typed-HTTP cross-repo
// matching (issue #534, phase 1).
//
// For every HTTP route that the extractor can statically identify on the
// PRODUCER side (a backend that *serves* a route), this pass emits a
// synthetic entity with a deterministic ID of the form
// `http:<METHOD>:<canonical-path>`. The synthetic entity carries the verb,
// canonical path, framework, and source-handler reference as properties,
// and is connected to the handler with a SERVED_BY edge (handler-method
// side) and an IMPLEMENTS edge (handler -> synthetic) so the existing
// graph passes can traverse from either direction.
//
// The producer-side emission is deliberately decoupled from the cross-repo
// linker. Because the synthetic ID is identical on both sides, phase 2
// (#510 / #533, client-side fetch / axios / requests extraction) can emit
// the same `http:<METHOD>:<path>` entity from the consumer's file; the
// existing import-channel linker will then match the two repositories on
// shared entity ID without any new matching code.
//
// Frameworks covered in phase 1 (verified against the corpora index):
//
//   - JAX-RS  (Java)   @Path on class + @GET/@POST/... on methods
//   - Spring MVC (Java) re-uses the composed Route entities from the AST
//     pass in spring_routes.go; the verb already lives on the
//     `http_method` property.
//   - Django  (Python) re-uses composed Route entities from the AST pass
//     in django_routes.go (method is "ANY" — Django wires methods at the
//     view level).
//   - Flask   (Python) `@<bp>.route(...)` and `@<bp>.<verb>(...)` decorators
//   - FastAPI (Python) `@app.<verb>(...)` / `@router.<verb>(...)`
//   - Express (JS/TS)  `app.<verb>(...)` / `<router>.<verb>(...)`
//
// Frameworks deferred to phase 2 chain-fixes: MicroProfile Rest Client,
// Spring Cloud Feign, Retrofit, Refit, gRPC service definitions, React
// Query URL extraction.
//
// Refs #534.
package engine

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/engine/httproutes"
	"github.com/cajasmota/archigraph/internal/types"
)

// httpEndpointKind is the legacy entity Kind retained for backward
// compatibility. After #1217 the synthesis pass emits
// httpEndpointDefinitionKind for producer-side handlers and
// httpEndpointCallKind for consumer-side call sites. The legacy constant
// is kept so that the resolve pass, dashboard, and link layers can use
// IsHTTPEndpointKind() to match all three forms transparently.
const httpEndpointKind = "http_endpoint"

// httpEndpointDefinitionKind is the new kind for backend handler definitions
// (producer side). Introduced by #1217.
const httpEndpointDefinitionKind = "http_endpoint_definition"

// httpEndpointCallKind is the new kind for consumer-side HTTP call sites.
// Introduced by #1217.
const httpEndpointCallKind = "http_endpoint_call"

// servesEdgeKind is the relationship Kind from a synthetic http_endpoint
// to its handler (Route / Controller / Operation / View). Direction:
// `http_endpoint:* -> handler`. Read as "the endpoint is served by this
// handler".
const servesEdgeKind = "SERVED_BY"

// implementsEdgeKind is the inverse: handler IMPLEMENTS synthetic. We emit
// both directions to make downstream queries cheap from either side.
const implementsEdgeKind = "IMPLEMENTS"

// fetchesEdgeKind is the consumer-side counterpart: caller FETCHES
// http_endpoint. Introduced by #721 (JS/TS + Python + Java). Direction:
// `<caller-function/method> -> http_endpoint:<verb>:<path>`. The edge's
// FromID is intentionally an unresolved kind-qualified reference
// (`Function:<name>`) — the resolve pass binds it to a stamped entity
// later. Emitting the edge here makes the producer↔consumer narrative
// chain queryable via the same FETCHES edge kind across all languages.
const fetchesEdgeKind = "FETCHES"

// synthesisSupportsLanguage reports whether applyHTTPEndpointSynthesis
// can emit synthetics for `lang`. The detector consults this when
// deciding whether to allow a file through even though no YAML rules
// are compiled for its language.
func synthesisSupportsLanguage(lang string) bool {
	switch lang {
	case "java", "python", "javascript", "typescript":
		return true
	// #727: WebSocket / SSE / GraphQL subscription synthesis covers
	// additional languages. We allow these through even when no YAML
	// rules are compiled for them so the per-protocol synthesizers can
	// run. Files in these languages that contain none of the recognised
	// anchors are no-ops in the synthesizers themselves.
	case "kotlin", "go", "csharp", "ruby", "php", "rust":
		return true
	// #1483: Elixir Finch / HTTPoison consumer-side extraction.
	case "elixir":
		return true
	default:
		return false
	}
}

// applyHTTPEndpointSynthesis runs after the existing route-composition
// passes and APPENDS synthetic http_endpoint entities + edges to the
// detector's output. It never modifies or removes existing entities or
// edges, so it cannot regress the bug-rate of the surrounding pipeline.
//
// `lang` lets the pass no-op cleanly for files that don't contain any of
// the supported frameworks.
func applyHTTPEndpointSynthesis(
	lang string,
	path string,
	content []byte,
	entities []types.EntityRecord,
	relationships []types.RelationshipRecord,
) ([]types.EntityRecord, []types.RelationshipRecord) {
	if len(content) == 0 {
		return entities, relationships
	}

	// Dedup-by-ID across all per-language emitters: a single endpoint can
	// be claimed by both the AST pass (composed Route) and a YAML-rule
	// regex (e.g. Spring's @GetMapping pattern). We only want one
	// synthetic per endpoint per file.
	seen := map[string]bool{}
	// makeEmit builds an emit-closure for the PRODUCER (backend handler) side.
	// #1217: entities are now emitted with httpEndpointDefinitionKind. The
	// synthetic ID retains the canonical `http:<METHOD>:<path>` form so
	// cross-repo linkers continue to pair definitions with calls by Name.
	// owning_backend is derived by walking the handler file path upward.
	makeEmit := func(patternType, refPropKey string) emitFn {
		return func(method, canonicalPath, framework, refKind, refName string) {
			if canonicalPath == "" {
				return
			}
			id := httproutes.SyntheticID(method, canonicalPath)
			if seen[id] {
				return
			}
			seen[id] = true

			props := map[string]string{
				"verb":         strings.ToUpper(method),
				"path":         canonicalPath,
				"framework":    framework,
				"pattern_type": patternType,
			}
			if refName != "" {
				props[refPropKey] = fmt.Sprintf("%s:%s", refKind, refName)
			}
			// #1217 — derive owning_backend for producer-side definitions.
			// Walk up from the handler file until a manifest or framework
			// marker is found; fall back to the top-level directory name.
			if patternType == "http_endpoint_synthesis" {
				props["owning_backend"] = deriveOwningBackend(path)
			}

			// Issue #708 — mark consumer-side synthetics whose canonical
			// path begins with a `{<name>}` placeholder, either at the
			// very start or immediately after the leading slash (e.g.
			// `{tenantId}/contracts/{id}` or `/{tenantId}/contracts/{id}`).
			// The first segment is a tenant ID / environment selector that
			// determines which backend the call targets at runtime — static
			// link matching can never land these.
			if patternType == "http_endpoint_client_synthesis" &&
				hasDynamicBaseURLPath(canonicalPath) {
				props["dynamic_baseurl"] = "true"
			}

			// #1217: use the new split kinds.
			kind := httpEndpointDefinitionKind
			if patternType == "http_endpoint_client_synthesis" {
				kind = httpEndpointCallKind
			}

			entities = append(entities, types.EntityRecord{
				ID:                 id,
				Name:               id,
				Kind:               kind,
				SourceFile:         path,
				Language:           lang,
				Properties:         props,
				EnrichmentRequired: false,
				EnrichmentStatus:   types.StatusPending,
				QualityScore:       0.8,
			})
		}
	}
	emit := makeEmit("http_endpoint_synthesis", "source_handler")
	emitClient := makeEmit("http_endpoint_client_synthesis", "source_caller")

	// makeRuntimeEmit wraps the consumer-side emit with a FETCHES edge
	// emission (#721). The edge's FromID is a kind-qualified reference
	// (`<kind>:<name>`) that the downstream resolver binds to a stamped
	// entity. When `runtimeDynamic` is true the synthetic carries
	// `runtime_dynamic=true`, surfacing the URL to the repair flow (#732).
	// #1217: the emitted entity is http_endpoint_call (via emitClient).
	// caller_file is stamped from the containing file path and url_kind is
	// derived from the path shape.
	makeRuntimeEmit := func() func(method, canonicalPath, framework, refKind, refName string, runtimeDynamic bool) {
		return func(method, canonicalPath, framework, refKind, refName string, runtimeDynamic bool) {
			if canonicalPath == "" {
				return
			}
			id := httproutes.SyntheticID(method, canonicalPath)
			alreadySeen := seen[id]
			emitClient(method, canonicalPath, framework, refKind, refName)
			// Stamp runtime_dynamic on the newly emitted entity if this
			// is the first time we see this ID and the URL was derived
			// from a runtime-dynamic source (env var, unresolved const).
			// Note: the entity is the last one appended by emitClient.
			if !alreadySeen && len(entities) > 0 {
				last := &entities[len(entities)-1]
				if runtimeDynamic {
					last.Properties["runtime_dynamic"] = "true"
				}
				// #1217 — stamp caller_file and url_kind on every http_endpoint_call.
				last.Properties["caller_file"] = path
				last.Properties["url_kind"] = urlKindFromPath(canonicalPath, runtimeDynamic)
			}
			// FETCHES edge: <kind>:<name> → http_endpoint_call:<verb>:<path>.
			// We only emit the edge when we have a caller reference; the
			// resolver will discard edges whose FromID never resolves.
			if refName != "" {
				relationships = append(relationships, types.RelationshipRecord{
					FromID: fmt.Sprintf("%s:%s", refKind, refName),
					ToID:   id,
					Kind:   fetchesEdgeKind,
					Properties: map[string]string{
						"verb":      strings.ToUpper(method),
						"path":      canonicalPath,
						"framework": framework,
					},
				})
			}
		}
	}
	emitClientRuntime := makeRuntimeEmit()

	// Phase 1 deliberately emits synthetic entities WITHOUT producer-side
	// handler→endpoint edges. The referenced entity is recorded as a
	// property (`source_handler`) so a follow-up pass can resolve it
	// against the existing entity table once the AST extractors emit
	// stable controller / function IDs. Consumer-side FETCHES edges ARE
	// emitted here — the unresolved `Function:<name>` FromID is a soft
	// reference that the graph walk tolerates gracefully.

	switch lang {
	case "java":
		// Spring MVC composed Routes already carry a verb on the
		// `http_method` property; reuse them rather than re-scanning the
		// file (the AST pass is the source of truth for prefix composition).
		synthesizeSpringFromComposed(entities, path, emit)
		// JAX-RS: scan the file directly.
		synthesizeJAXRS(string(content), emit)
		// Consumer side (#721): HttpClient / RestTemplate /
		// WebClient / OkHttp / Apache HttpClient / Retrofit.
		synthesizeJavaClientWithRuntime(string(content), emitClientRuntime)
	case "python":
		// Producer side: Flask + FastAPI run FIRST so their synthetics —
		// which carry a real handler function name as source_handler —
		// claim each ID before the Django composed-route pass walks the
		// generic YAML Route entities. Previously synthesizeDjangoFromComposed
		// ran first and dedup-stole every Flask/@blueprint.route(...) URL,
		// emitting a synthetic with `source_handler=Route:<path>`
		// (Spring-style placeholder), which the resolver dropped and
		// the response-shape extractor could not parse. #753.
		synthesizeFlask(string(content), emit)
		synthesizeFastAPI(string(content), emit)
		// Producer side: Django composed Routes (from django_routes.go).
		// Method is unknown statically, so emit with verb=ANY.
		synthesizeDjangoFromComposed(entities, path, emit)
		// Consumer side (#721, extends #533): requests / httpx /
		// aiohttp / urllib / session-style HTTP client calls.
		// Now emits FETCHES edges at extraction time.
		synthesizePyClientWithRuntime(string(content), emitClientRuntime)
	case "javascript", "typescript":
		// Producer side: Express.
		synthesizeExpress(string(content), emit)
		// Producer side: NestJS @Controller + @Get/@Post/... decorators (#1418).
		synthesizeNestJS(string(content), emit)
		// Producer side: Apollo / GraphQL resolvers (#1422). GraphQL is
		// schema-first rather than REST, so resolver fields are emitted as
		// graphql_field endpoint-ish entities keyed by operation + field.
		synthesizeGraphQLResolvers(string(content), emit)
		// Consumer side (#721): fetch / axios / generic *Client
		// HTTP client calls. Now emits FETCHES edges at extraction time.
		synthesizeFetchAxiosWithRuntime(string(content), emitClientRuntime)
	case "go":
		// Producer side: Gin / Echo / Chi route registrations. #722.
		synthesizeGoRouters(string(content), emit)
		// Consumer side (#721 wave 2a): net/http, resty, fasthttp.
		synthesizeGoClientWithRuntime(string(content), emitClientRuntime)
	case "kotlin":
		// Consumer side (#721 wave 2a): Ktor, OkHttp-Kotlin, Retrofit-K.
		synthesizeKotlinClientWithRuntime(string(content), emitClientRuntime)
		// Consumer side (#1421): Spring RestTemplate / WebClient / FeignClient
		// are commonly used in Kotlin Spring Boot services. The Java client
		// synthesizer covers these patterns idiomatically since both JVM
		// languages share the same Spring APIs.
		synthesizeJavaClientWithRuntime(string(content), emitClientRuntime)
	case "ruby":
		// Consumer side (#721 wave 2b): Net::HTTP, Faraday, HTTParty, RestClient.
		synthesizeRubyClientWithRuntime(string(content), emitClientRuntime)
	case "csharp":
		// Consumer side (#721 wave 2b): HttpClient, RestSharp, Refit, WebClient.
		synthesizeCSharpClientWithRuntime(string(content), emitClientRuntime)
	case "rust":
		// Producer side (#1420): axum Router::new().route(...) registrations.
		synthesizeAxumRoutes(string(content), emit)
		// Consumer side (#721 wave 2c): reqwest, hyper, ureq, surf.
		synthesizeRustClientWithRuntime(string(content), emitClientRuntime)
	case "php":
		// Producer side (#1419): Laravel Route::verb/resource/apiResource.
		synthesizeLaravel(string(content), emit)
		// Consumer side (#721 wave 2c): Guzzle, Symfony HttpClient, cURL, file_get_contents,
		// WordPress HTTP API, Laravel Http facade.
		synthesizePHPClientWithRuntime(string(content), emitClientRuntime)
	case "elixir":
		// Consumer side (#1483): Finch.build(:verb, url) + HTTPoison.<verb>(url).
		synthesizeElixirHTTPClients(string(content), emitClient)
	}

	// #722 — response/request shape extraction. Mutates Properties on
	// the synthetic entities emitted above; never adds or removes
	// entities, so it cannot regress the bug-rate of upstream passes.
	applyResponseShapes(lang, content, entities)

	return entities, relationships
}

// ---------------------------------------------------------------------------
// Spring MVC (reuse composed entities)
// ---------------------------------------------------------------------------

// synthesizeSpringFromComposed walks `entities` looking for the Routes
// emitted by spring_routes.go (Kind=Route, framework=java,
// pattern_type=ast_driven, http_method set) and emits one
// http_endpoint per (verb, path) tuple.
func synthesizeSpringFromComposed(entities []types.EntityRecord, path string, emit emitFn) {
	for _, e := range entities {
		if e.Kind != "Route" || e.SourceFile != path {
			continue
		}
		if e.Properties == nil {
			continue
		}
		// Accept both ast_driven (spring_routes.go composition) and
		// yaml_driven (regex rule) Route entities. Either source gives
		// us a usable canonical path; the AST pass adds an http_method
		// property when present, while the YAML pass leaves it blank.
		if e.Properties["framework"] != "java" {
			continue
		}
		verb := e.Properties["http_method"]
		if verb == "" {
			verb = "ANY"
		}
		canonical := httproutes.Canonicalize(httproutes.FrameworkSpring, e.Name)
		// Source-handler reference: spring_routes.go emits the matching
		// edge as Route:<composed> -> Controller:<methodName>, but we
		// don't have the method name available here without re-walking
		// the AST. Leave handler unset — the IMPLEMENTS edge will be
		// emitted at the Spring AST level in a follow-up if needed.
		emit(verb, canonical, "spring_mvc", "Route", e.Name)
	}
}

// ---------------------------------------------------------------------------
// Django (reuse composed entities)
// ---------------------------------------------------------------------------

// synthesizeDjangoFromComposed walks `entities` looking for Routes
// emitted by django_routes.go (Kind=Route, framework=python,
// pattern_type=ast_driven) and emits one ANY-verb http_endpoint per.
// Django wires HTTP methods at the View / ViewSet level, not the URL
// level; we can refine the verb in a follow-up by walking ViewSet
// classes for `def get(self, ...)` / `def post(self, ...)` etc.
//
// #748 — Only ast_driven routes are processed here. Routes with
// pattern_type=yaml_driven come from the Django YAML source_patterns
// (specifically the bare `path("...")` regex) which can also fire on
// non-Django Python files — most importantly FastAPI files that
// happen to call `path(...)` in their scope. Processing yaml_driven
// Route entities here causes FastAPI endpoints to be emitted as
// `http:ANY:/path` instead of `http:GET:/path` (or whatever the
// actual decorator verb is), because this function always emits with
// verb=ANY.
//
// ast_driven routes come exclusively from django_routes.go /
// django_urlconf_nested.go / drf_router pass — all of which require
// Django-specific structural signals (urlpatterns, DRF router binding)
// before emitting. They are safe to process here. yaml_driven routes
// from FastAPI / Flask files that accidentally match the Django
// path() regex are NOT safe and must be skipped.
func synthesizeDjangoFromComposed(entities []types.EntityRecord, path string, emit emitFn) {
	for _, e := range entities {
		if e.Kind != "Route" || e.SourceFile != path {
			continue
		}
		if e.Properties == nil {
			continue
		}
		if e.Properties["framework"] != "python" {
			continue
		}
		// #748 — skip yaml_driven routes: the Django YAML path() regex
		// fires on any `path("...")` call regardless of file type, which
		// includes FastAPI @router.get / @app.get decorated files.
		// Only ast_driven routes (from the Django AST composition passes)
		// are reliable signals of a true Django URL conf.
		if e.Properties["pattern_type"] != "ast_driven" {
			continue
		}
		// Only treat path-shaped names as routes. The Django YAML rule
		// for url(r'^pattern', view) emits Route entities whose .Name is
		// the regex/path; for `path("api/users/", ...)` style it's the
		// raw path. Skip names that don't look path-shaped to avoid
		// polluting the graph with non-route Routes (e.g. handler-named
		// composed entries the YAML pass might emit in other shapes).
		raw := e.Name
		if raw == "" {
			continue
		}
		canonical := httproutes.Canonicalize(httproutes.FrameworkDjango, raw)
		if canonical == "" {
			continue
		}
		// Issue #1125 — reject XML namespace XPath strings that the Django
		// YAML `path(...)` rule may have captured from python-docx / lxml
		// code (e.g. `element.find(path('./w:tblBorders'))`).
		if isXMLNamespacePath(canonical) {
			continue
		}
		// #1412 — skip Django admin routes. Django's admin site is registered
		// via `include(admin.site.urls)` which produces Route entities for
		// admin/ prefix paths. These are internal CMS scaffolding routes (~100
		// sub-paths per project), not application API endpoints. Suppressing
		// them reduces endpoint noise by ~18.5% on typical Django projects.
		// admin_class entities + REGISTERS/HANDLES_SIGNAL edges are unaffected.
		if isAdminRoute(e) {
			continue
		}
		emit("ANY", canonical, "django", "Route", raw)

		// #703 — DRF DefaultRouter / SimpleRouter auto-generates a
		// parallel `/<prefix>/{pk}` detail route for every list route
		// it emits. The single-file AST pass (django_routes.go) only
		// records the list-route prefix; synthesise the matching
		// detail-route http_endpoint here so the cross-repo linker can
		// match consumer-side calls to the producer side. Only fire on
		// ast_driven routes (the AST pass composes DRF router prefixes)
		// and skip when the path already contains a `{...}` placeholder
		// — those routes are path()-based and already encode their
		// parameter.
		//
		// With #704 byPath normalization on main, the matcher collapses
		// {pk}/{id}/{param}/{userId} all to {*} at lookup time — so
		// emitting ONE canonical {pk} placeholder is sufficient. No
		// multi-variant loop needed.
		// NOTE: the ast_driven gate at the top of the loop already ensures
		// we never reach this point with a yaml_driven route — the check
		// that was here previously is now a no-op and has been removed.
		if strings.Contains(canonical, "{") {
			continue
		}
		detail := strings.TrimSuffix(canonical, "/") + "/{pk}"
		detailCanonical := httproutes.Canonicalize(httproutes.FrameworkDjango, detail)
		if detailCanonical != "" && detailCanonical != canonical {
			// Empty refName so the resolver leaves this synthetic in the
			// `NoHandlerProp` keep-path. The list-route synthetic above
			// is the one with the real Route handler; the detail
			// variant is matched by canonical Name from the cross-repo
			// linker via the byPath normalized index.
			emit("ANY", detailCanonical, "django", "Route", "")
		}
	}
}

// ---------------------------------------------------------------------------
// JAX-RS (Java)
// ---------------------------------------------------------------------------

// jaxrsClassPathRe captures the class-level @Path("/prefix") value.
var jaxrsClassPathRe = regexp.MustCompile(`@Path\s*\(\s*"([^"\n\r]*)"\s*\)\s*[\r\n]+(?:[^@\r\n]*[\r\n]+)*?[^{]*?\bclass\s+\w+`)

// jaxrsMethodAnnotationRe captures method-level verb + optional @Path on
// the same handler. We scan the whole file, since the grouping with the
// owning class is approximated by emitting per-method with the class
// prefix detected by jaxrsClassPathRe.
//
// Matches forms like:
//
//	@GET
//	@Path("/{id}")
//	public Foo get(@PathParam("id") long id) { ... }
//
// or just `@GET` followed by a method declaration with no method-level path.
var jaxrsMethodVerbRe = regexp.MustCompile(`@(GET|POST|PUT|DELETE|PATCH|HEAD|OPTIONS)\b(?:[^\n\r{}]*[\r\n]+){0,3}?\s*(?:@Path\s*\(\s*"([^"\n\r]*)"\s*\)\s*[\r\n]+)?\s*(?:@[\w.]+(?:\([^)]*\))?\s*[\r\n]*)*?\s*(?:public|protected|private|static|final|abstract|\s)+[\w<>\[\],.\s?]+?\s+(\w+)\s*\(`)

// synthesizeJAXRS scans a Java file for JAX-RS handlers. Supports a single
// class-level @Path prefix per file (the dominant convention); files with
// multiple JAX-RS resource classes will still emit endpoints but only
// under the first class prefix.
func synthesizeJAXRS(content string, emit emitFn) {
	if !strings.Contains(content, "@Path") {
		return
	}
	classPrefix := ""
	if m := jaxrsClassPathRe.FindStringSubmatch(content); len(m) >= 2 {
		classPrefix = m[1]
	}
	for _, m := range jaxrsMethodVerbRe.FindAllStringSubmatch(content, -1) {
		if len(m) < 4 {
			continue
		}
		verb := m[1]
		methodPath := m[2]
		methodName := m[3]
		full := joinPathFragments(classPrefix, methodPath)
		canonical := httproutes.Canonicalize(httproutes.FrameworkJAXRS, full)
		emit(verb, canonical, "jaxrs", "Controller", methodName)
	}
}

// ---------------------------------------------------------------------------
// Flask (Python)
// ---------------------------------------------------------------------------

// flaskRouteVerbDecoratorRe captures @<obj>.<verb>("/path") for the
// shorthand verbs Flask exposes (get/post/put/patch/delete). The handler
// function name is captured from the next `def` line. We accept anything
// up to a bare `)` followed by end-of-line because Flask decorators may
// carry trailing kwargs (defaults={}, strict_slashes=False, etc.).
var flaskRouteVerbDecoratorRe = regexp.MustCompile(`@\w+\.(get|post|put|patch|delete)\s*\(\s*["']([^"'\n\r]+)["'][^\n\r]*\)[ \t]*[\r\n]+(?:\s*@[^\n\r]*[\r\n]+)*\s*def\s+(\w+)`)

// flaskRouteRe captures the generic @<obj>.route("/path", ...) form and
// the handler function name. The trailing kwargs (including a
// methods=[...] or methods=(...) argument) are captured for parseFlaskMethods.
// We tolerate one level of nested parens / brackets in the kwargs by
// matching greedily up to the end of the line that closes the decorator.
var flaskRouteRe = regexp.MustCompile(`@\w+\.route\s*\(\s*["']([^"'\n\r]+)["']([^\n\r]*)\)[ \t]*[\r\n]+(?:\s*@[^\n\r]*[\r\n]+)*\s*def\s+(\w+)`)

// flaskMethodsArgRe extracts the list of HTTP methods from a
// `methods=["GET", "POST"]` or `methods=("GET", "POST")` keyword argument
// inside a @<obj>.route(...) call. Both list and tuple literals are
// accepted because both are idiomatic in the wild.
var flaskMethodsArgRe = regexp.MustCompile(`methods\s*=\s*[\[\(]([^\]\)]+)[\]\)]`)

func synthesizeFlask(content string, emit emitFn) {
	if !strings.Contains(content, ".route(") && !strings.Contains(content, ".get(") &&
		!strings.Contains(content, ".post(") && !strings.Contains(content, ".put(") &&
		!strings.Contains(content, ".patch(") && !strings.Contains(content, ".delete(") {
		return
	}
	// Shorthand verbs first — they have an unambiguous verb.
	for _, m := range flaskRouteVerbDecoratorRe.FindAllStringSubmatch(content, -1) {
		if len(m) < 4 {
			continue
		}
		verb := strings.ToUpper(m[1])
		raw := m[2]
		handler := m[3]
		canonical := httproutes.Canonicalize(httproutes.FrameworkFlask, raw)
		emit(verb, canonical, "flask", "Controller", handler)
	}
	// Generic .route(...) form.
	for _, m := range flaskRouteRe.FindAllStringSubmatch(content, -1) {
		if len(m) < 4 {
			continue
		}
		raw := m[1]
		extras := m[2]
		handler := m[3]
		canonical := httproutes.Canonicalize(httproutes.FrameworkFlask, raw)
		methods := parseFlaskMethods(extras)
		if len(methods) == 0 {
			// Flask's default: no `methods` kwarg means GET only.
			methods = []string{"GET"}
		}
		for _, verb := range methods {
			emit(verb, canonical, "flask", "Controller", handler)
		}
	}
}

// parseFlaskMethods returns the verbs declared in a `methods=[...]`
// keyword argument. The argument may use either single or double quotes
// and arbitrary whitespace.
func parseFlaskMethods(args string) []string {
	mm := flaskMethodsArgRe.FindStringSubmatch(args)
	if len(mm) < 2 {
		return nil
	}
	body := mm[1]
	var out []string
	for _, tok := range strings.Split(body, ",") {
		tok = strings.TrimSpace(tok)
		tok = strings.Trim(tok, `"'`)
		if tok == "" {
			continue
		}
		out = append(out, strings.ToUpper(tok))
	}
	return out
}

// ---------------------------------------------------------------------------
// FastAPI (Python)
// ---------------------------------------------------------------------------

// fastapiVerbDecoratorRe captures @app.<verb>("/path") and
// @router.<verb>("/path") forms. The handler function follows on the next
// `def`/`async def` line; intermediate decorators (e.g. @app.middleware,
// @Depends) are allowed.
var fastapiVerbDecoratorRe = regexp.MustCompile(`@(?:app|router|api|\w+_router)\.(get|post|put|patch|delete|head|options|trace)\s*\(\s*["']([^"'\n\r]+)["'][^)]*\)\s*[\r\n]+(?:\s*@[^\n\r]*[\r\n]+)*\s*(?:async\s+)?def\s+(\w+)`)

func synthesizeFastAPI(content string, emit emitFn) {
	if !strings.Contains(content, "FastAPI") && !strings.Contains(content, "APIRouter") &&
		!strings.Contains(content, "@app.") && !strings.Contains(content, "@router.") {
		return
	}
	for _, m := range fastapiVerbDecoratorRe.FindAllStringSubmatch(content, -1) {
		if len(m) < 4 {
			continue
		}
		verb := strings.ToUpper(m[1])
		raw := m[2]
		handler := m[3]
		canonical := httproutes.Canonicalize(httproutes.FrameworkFastAPI, raw)
		emit(verb, canonical, "fastapi", "Controller", handler)
	}
}

// ---------------------------------------------------------------------------
// Express (JS/TS)
// ---------------------------------------------------------------------------

// expressAllowedReceiverRe matches receiver names that look like an Express
// app or router object. The allowlist covers the most common conventions:
//   - `app`, `router`, `r`, `srv`, `server`, `httpServer` (exact matches)
//   - any identifier ending in `Router`, `App`, `Server`, `Srv`, `Handler`
//     (e.g. `apiRouter`, `httpServer`, `myApp`)
//
// This replaces the open `(?:\w+)` anchor in both expressVerbRe and
// expressVerbRePathOnly which matched ANY identifier — including FormData,
// URLSearchParams, Dimensions, Map, etc. (issue #653).
var expressAllowedReceiverRe = regexp.MustCompile(
	`(?:^|[^.\w$])(app|router|r|srv|server|httpServer|` +
		`\w+[Rr]outer|\w+[Aa]pp|\w+[Ss]erver|\w+[Ss]rv|\w+[Hh]andler)\b`,
)

// expressBlocklistRe matches receiver names that are definitively NOT HTTP
// routers — they are browser/RN/DOM APIs or known HTTP CLIENT variable names
// that share the same method names. Even if the allowlist regex accidentally
// matches one of these, this blocklist is the final veto.
//
// Round 1 (#653): formData, urlSearchParams, searchParams, headers,
// dimensions, localStorage, sessionStorage, cache, map, set, params, query,
// queryParams.
//
// Round 2 (#684): $http (Angular/Vue axios instance), api, client, http,
// request, xhr, and any name ending in Client or Api (e.g. apiClient,
// myClient, branchesApi). These are consumer-side HTTP wrapper variables
// recognized by synthesizeFetchAxios (#672) — they must never be treated as
// Express producers.
var expressBlocklistRe = regexp.MustCompile(
	`^(?i:formData|formdata|urlSearchParams|urlsearchparams|` +
		`searchParams|searchparams|headers|dimensions|` +
		`localStorage|localstorage|sessionStorage|sessionstorage|` +
		`cache|map|set|params|query|queryParams|queryparams|` +
		`\$http|\$api|\$client|api|client|http|request|xhr)$` +
		`|^(?i:.*[Cc]lient|.*[Aa]pi|.*[Ss]ervice)$`,
)

// expressHTTPClientConstructorRe matches assignments that create an HTTP
// client instance from a known factory. Variables assigned via these
// constructors are consumer-side and must never be classified as Express
// producers even if their name would otherwise pass the allowlist.
//
// Patterns matched:
//   - `const $http = axios.create(...)`
//   - `const apiClient = axios.create(...)`
//   - `const http = ky.create(...)`
//   - `const myHttp = got.extend(...)`
var expressHTTPClientConstructorRe = regexp.MustCompile(
	`(?:const|let|var)\s+([$\w][\w$]*)\s*=\s*` +
		`(?:axios\.create|ky\.create|ky\.extend|got\.extend|got\.create|` +
		`superagent\.agent|needle|wretch)\s*\(`,
)

// expressVerbRe captures the canonical Express form
// `<receiver>.<verb>("/path", handler)` where verb is one of the HTTP verbs.
// capture group 1 = receiver, 2 = verb, 3 = path string, 4 = handler name.
// The receiver is now captured (not discarded) so we can apply the
// allowlist/blocklist gates before emitting.
var expressVerbRe = regexp.MustCompile(`([$\w][\w$]*)\.(get|post|put|patch|delete|head|options|all)\s*\(\s*['"` + "`" + `]([^'"` + "`" + `\n\r]+)['"` + "`" + `]\s*(?:,[^,)]*)*?,\s*([\w$.]+)\s*[\),]`)

// expressVerbRePathOnly captures the path-only variant where the handler
// is inline (function expression / arrow); we still emit the synthetic
// entity but without a handler reference.
// capture group 1 = receiver, 2 = verb, 3 = path string.
var expressVerbRePathOnly = regexp.MustCompile(`([$\w][\w$]*)\.(get|post|put|patch|delete|head|options|all)\s*\(\s*['"` + "`" + `]([^'"` + "`" + `\n\r]+)['"` + "`" + `]`)

// isExpressReceiver returns true when the receiver identifier looks like an
// Express app/router variable (allowlist) and is not on the hard blocklist.
// It also consults the per-file HTTP-client symbol table built by
// buildExpressClientSymbolTable so that variables assigned from axios.create()
// / ky.create() / got.extend() are never misclassified as producers (#684).
func isExpressReceiver(receiver string, clientSymbols map[string]bool) bool {
	// Hard blocklist — highest priority veto.
	if expressBlocklistRe.MatchString(receiver) {
		return false
	}
	// File-local symbol table check: a variable assigned from a known HTTP
	// client constructor in this file is ALWAYS a consumer, never a producer.
	if clientSymbols[receiver] {
		return false
	}
	// Allowlist: must look like an express app or router variable.
	// We test the receiver in isolation (prefix the string with a space so
	// the word-boundary anchor in expressAllowedReceiverRe fires correctly).
	return expressAllowedReceiverRe.MatchString(" " + receiver)
}

// looksLikeExpressPath returns true when a raw string argument looks like an
// HTTP path (must start with `/`). This blocks single-word keys like
// "cronjob_opt_in", "window", "segment" that are valid JS keys but not HTTP
// paths. Belt-and-suspenders on top of the receiver gate.
func looksLikeExpressPath(raw string) bool {
	return len(raw) > 0 && raw[0] == '/'
}

// buildExpressClientSymbolTable scans the file content for variable
// assignments from known HTTP-client constructors (axios.create, ky.create,
// got.extend, etc.) and returns the set of variable names that are confirmed
// consumer-side HTTP clients. These variables must never be matched as
// Express producers regardless of their name shape.
func buildExpressClientSymbolTable(content string) map[string]bool {
	symbols := make(map[string]bool)
	for _, m := range expressHTTPClientConstructorRe.FindAllStringSubmatch(content, -1) {
		if len(m) >= 2 && m[1] != "" {
			symbols[m[1]] = true
		}
	}
	return symbols
}

func synthesizeExpress(content string, emit emitFn) {
	if !strings.Contains(content, ".get(") && !strings.Contains(content, ".post(") &&
		!strings.Contains(content, ".put(") && !strings.Contains(content, ".patch(") &&
		!strings.Contains(content, ".delete(") && !strings.Contains(content, ".all(") &&
		!strings.Contains(content, ".head(") && !strings.Contains(content, ".options(") {
		return
	}
	// Build the per-file HTTP-client symbol table once for the whole pass.
	// Variables assigned from axios.create() / ky.create() / got.extend()
	// are consumer-side and must never be emitted as Express producers (#684).
	clientSymbols := buildExpressClientSymbolTable(content)

	// First pass: handler-named form (groups: receiver, verb, path, handler).
	withHandler := map[string]bool{}
	for _, m := range expressVerbRe.FindAllStringSubmatch(content, -1) {
		if len(m) < 5 {
			continue
		}
		receiver := m[1]
		verb := strings.ToUpper(m[2])
		raw := m[3]
		handler := m[4]
		// Receiver-shape gate (allowlist + blocklist + symbol table) — primary false-positive guard.
		if !isExpressReceiver(receiver, clientSymbols) {
			continue
		}
		// Path-shape gate — belt-and-suspenders; rejects non-path string literals.
		if !looksLikeExpressPath(raw) {
			continue
		}
		// #1423 — inline arrow / function-expression handlers. The
		// handler-named regex's group-4 `([\w$.]+)` greedily captures an
		// identifier from *inside* an inline handler — e.g.
		// `app.get("/x", async (req, res) => {...})` captures `res` (the
		// last param before the `)`), yielding `source_handler=Controller:res`
		// which the resolve pass can never bind and therefore DROPS the whole
		// synthetic (handler_dropped). When the matched region between the
		// path literal and the handler token contains a `(` it means the
		// "handler" is actually a function parameter, not a named reference.
		// In that case clear the handler so the synthetic is emitted with no
		// source_handler (NoHandlerProp keep-path) and survives resolve. The
		// path-only second pass would otherwise dedup it away with a handler
		// it can't use, so we MUST claim the (verb,path) here.
		if isInlineExpressHandler(m[0], raw) {
			handler = ""
		}
		canonical := httproutes.Canonicalize(httproutes.FrameworkExpress, raw)
		// Express `.all(...)` registers every verb on the path; emit as ANY.
		if verb == "ALL" {
			verb = "ANY"
		}
		key := verb + ":" + canonical
		withHandler[key] = true
		emit(verb, canonical, "express", "Controller", handler)
	}
	// Second pass: path-only form (groups: receiver, verb, path), skipping any
	// (verb, path) already claimed by the handler-named scan.
	for _, m := range expressVerbRePathOnly.FindAllStringSubmatch(content, -1) {
		if len(m) < 4 {
			continue
		}
		receiver := m[1]
		verb := strings.ToUpper(m[2])
		raw := m[3]
		// Same gates as the handler-named pass.
		if !isExpressReceiver(receiver, clientSymbols) {
			continue
		}
		if !looksLikeExpressPath(raw) {
			continue
		}
		canonical := httproutes.Canonicalize(httproutes.FrameworkExpress, raw)
		if verb == "ALL" {
			verb = "ANY"
		}
		key := verb + ":" + canonical
		if withHandler[key] {
			continue
		}
		emit(verb, canonical, "express", "Controller", "")
	}
}

// isInlineExpressHandler reports whether the handler captured by
// expressVerbRe came from inside an inline function expression / arrow
// function rather than being a bare named-reference handler.
//
// expressVerbRe's group 4 (`([\w$.]+)`) can match a function parameter when
// the handler is inline, e.g. `app.get("/x", async (req, res) => {...})`
// captures `res`. We distinguish the two cases by inspecting the matched
// region AFTER the path literal: if it contains an opening paren `(` or a
// `function` keyword, the captured token is a parameter name (inline
// handler), not a named handler reference. Named handlers like
// `app.get("/x", handlerFn)` have no `(` between the path and the token.
func isInlineExpressHandler(fullMatch, raw string) bool {
	// Find the path literal inside the full match and inspect the tail.
	idx := strings.Index(fullMatch, raw)
	if idx < 0 {
		return false
	}
	tail := fullMatch[idx+len(raw):]
	return strings.ContainsRune(tail, '(') || strings.Contains(tail, "function")
}

// ---------------------------------------------------------------------------
// NestJS (JS/TS) — #1418
// ---------------------------------------------------------------------------
//
// NestJS controllers declare a class-level route prefix via
// `@Controller('prefix')` (or `@Controller()` for the root) and per-handler
// verbs via method decorators `@Get()`, `@Post('sub')`, `@Put(':id')`, etc.
// The combined route is `<prefix>/<method-path>` and the verb is the
// decorator name. We emit one http_endpoint_definition per decorated method
// with the composed canonical path, mirroring the Spring/JAX-RS shape.
//
// This is a regex pass (no AST) consistent with the other framework
// synthesizers. It handles the single-controller-per-file convention that
// NestJS overwhelmingly follows; a file with two @Controller classes will
// attribute all methods to the first prefix (acceptable — the cross-repo
// linker matches on path, and split controllers are rare).

// nestControllerRe captures the class-level @Controller('prefix') value.
// The prefix is optional (`@Controller()` → root prefix ""). Accepts single,
// double, or backtick quotes.
var nestControllerRe = regexp.MustCompile(
	"@Controller\\s*\\(\\s*(?:['\"`]([^'\"`\\n\\r]*)['\"`])?\\s*\\)",
)

// nestMethodDecoratorRe captures a NestJS HTTP-verb method decorator and the
// following method name. The decorator path argument is optional. We allow
// intervening decorators (e.g. @UseGuards, @HttpCode, @Param) and modifiers
// (public/private/async/static) between the verb decorator and the method
// declaration.
//
// Capture groups: 1 = verb, 2 = optional decorator path, 3 = method name.
var nestMethodDecoratorRe = regexp.MustCompile(
	"@(Get|Post|Put|Delete|Patch|Head|Options|All)\\s*\\(\\s*(?:['\"`]([^'\"`\\n\\r]*)['\"`])?\\s*[^)]*\\)" +
		"\\s*[\\r\\n]+(?:\\s*@[\\w.]+\\s*(?:\\([^)]*\\))?\\s*[\\r\\n]+)*" +
		"\\s*(?:public\\s+|private\\s+|protected\\s+|static\\s+|readonly\\s+|async\\s+)*" +
		"([A-Za-z_$][\\w$]*)\\s*\\(",
)

func synthesizeNestJS(content string, emit emitFn) {
	if !strings.Contains(content, "@Controller") {
		return
	}
	prefix := ""
	if m := nestControllerRe.FindStringSubmatch(content); len(m) >= 2 {
		prefix = m[1]
	}
	for _, m := range nestMethodDecoratorRe.FindAllStringSubmatch(content, -1) {
		if len(m) < 4 {
			continue
		}
		verb := strings.ToUpper(m[1])
		methodPath := m[2]
		methodName := m[3]
		if verb == "ALL" {
			verb = "ANY"
		}
		full := joinPathFragments("/"+strings.Trim(prefix, "/"), methodPath)
		canonical := httproutes.Canonicalize(httproutes.FrameworkExpress, full)
		if canonical == "" {
			continue
		}
		emit(verb, canonical, "nestjs", "Controller", methodName)
	}
}

// ---------------------------------------------------------------------------
// Apollo / GraphQL resolvers (JS/TS) — #1422
// ---------------------------------------------------------------------------
//
// GraphQL is a single-endpoint protocol — every operation is POSTed to one
// `/graphql` mount — so it does not map cleanly onto the REST route↔fetch
// model. To give the cross-repo linker and the topology view *something* to
// match, we emit one endpoint-ish synthetic per resolver field under the
// Query / Mutation / Subscription roots, using the synthetic verb GRAPHQL and
// a canonical path of `/graphql/<Operation>/<field>`. The HTTP CALL sites the
// resolvers make to downstream REST services (via serviceClient/axios) are
// captured by the consumer-side synthesizer (synthesizeFetchAxios) — that is
// where the real cross-repo edges (search-graphql → catalog/orders/semantic)
// come from. The resolver-field synthetics are graph-discoverability sugar.
//
// Detection is intentionally narrow: we only fire inside an object literal
// whose key is one of Query / Mutation / Subscription, capturing the field
// name of each `<field>: (...) => ...` / `<field>(...) {` / `async <field>(`
// resolver entry.

// gqlRootBlockRe matches a `Query: {`, `Mutation: {`, `Subscription: {`
// resolver-map root and captures the root name. Used to scope field
// extraction to resolver blocks only.
var gqlRootBlockRe = regexp.MustCompile(`\b(Query|Mutation|Subscription)\s*:\s*\{`)

// gqlFieldRe matches a resolver field entry inside a resolver-map root block:
//
//	searchProducts: async (_, { q }) => { ... }
//	order: (parent, args) => { ... }
//	createOrder(parent, args) { ... }
//
// Capture group 1 = field name.
var gqlFieldRe = regexp.MustCompile(
	`(?m)^[ \t]*([A-Za-z_$][\w$]*)\s*:\s*(?:async\s*)?\(` +
		`|(?m)^[ \t]*(?:async\s+)?([A-Za-z_$][\w$]*)\s*\([^)]*\)\s*\{`,
)

func synthesizeGraphQLResolvers(content string, emit emitFn) {
	// Only operate on files that look like a GraphQL resolver map.
	if !strings.Contains(content, "Query") && !strings.Contains(content, "Mutation") &&
		!strings.Contains(content, "Subscription") {
		return
	}
	if !strings.Contains(content, "resolvers") && !strings.Contains(content, "Resolver") {
		return
	}
	for _, rb := range gqlRootBlockRe.FindAllStringSubmatchIndex(content, -1) {
		root := content[rb[2]:rb[3]]
		// The root block opens at the `{` consumed by the regex (rb[1]-1).
		blockOpen := rb[1] - 1
		blockClose := findMatchingBrace(content, blockOpen)
		if blockClose < 0 {
			continue
		}
		body := content[blockOpen+1 : blockClose]
		seenField := map[string]bool{}
		for _, fm := range gqlFieldRe.FindAllStringSubmatch(body, -1) {
			field := fm[1]
			if field == "" && len(fm) > 2 {
				field = fm[2]
			}
			if field == "" || seenField[field] {
				continue
			}
			seenField[field] = true
			path := "/graphql/" + root + "/" + field
			canonical := httproutes.Canonicalize(httproutes.FrameworkExpress, path)
			// Empty handler ref: the resolver-field name is not a separately
			// extracted entity, so passing it as source_handler would make the
			// resolve pass drop the synthetic (handler_dropped). Emit with no
			// handler so it lands in the NoHandlerProp keep-path and survives.
			emit("GRAPHQL", canonical, "graphql", "", "")
		}
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// emitFn is the closure signature used by each per-framework synthesiser.
// (method, canonicalPath, framework, handlerKind, handlerName)
type emitFn func(method, canonicalPath, framework, handlerKind, handlerName string)

// joinPathFragments concatenates a class-level prefix with a method-level
// path, mirroring the slash convention used by joinRoutePaths in
// spring_routes.go. An empty prefix or method passes the other through
// verbatim.
func joinPathFragments(prefix, method string) string {
	switch {
	case prefix == "" && method == "":
		return "/"
	case prefix == "":
		return method
	case method == "":
		return prefix
	}
	if strings.HasSuffix(prefix, "/") && strings.HasPrefix(method, "/") {
		return prefix + strings.TrimPrefix(method, "/")
	}
	if !strings.HasSuffix(prefix, "/") && !strings.HasPrefix(method, "/") {
		return prefix + "/" + method
	}
	return prefix + method
}

// hasDynamicBaseURLPath reports whether a canonical URL path indicates a
// dynamic-baseURL consumer call (issue #708). These are calls where the
// FIRST path segment is a runtime variable (tenant ID, environment
// selector, etc.) that determines which backend the request targets.
//
// Accepted forms:
//
//	{tenantId}/contracts/{id}   — leading placeholder, no root slash
//	/{tenantId}/contracts/{id}  — leading placeholder after root slash
//
// We intentionally do NOT flag paths like `/api/{version}/users` where
// the dynamic segment is NOT the first segment — those are normal
// parameterised routes that the linker can match by shape.
func hasDynamicBaseURLPath(path string) bool {
	// Drop the optional leading slash so we can check the first segment.
	rest := strings.TrimPrefix(path, "/")
	// First character of the first segment must open a placeholder.
	return strings.HasPrefix(rest, "{")
}

// ---------------------------------------------------------------------------
// #1217: owning_backend derivation + url_kind classification
// ---------------------------------------------------------------------------

// manifestFileNames is the ordered list of file names that indicate a
// backend service boundary. We walk up the directory tree from a handler
// file and stop at the first directory that contains one of these files.
var manifestFileNames = []string{
	"pyproject.toml", "setup.py", "setup.cfg", // Python
	"package.json",                                // JS/TS/Node
	"go.mod",                                      // Go
	"Cargo.toml",                                  // Rust
	"pom.xml", "build.gradle", "build.gradle.kts", // Java/Kotlin
	"Gemfile",          // Ruby
	"composer.json",    // PHP
	"*.csproj",         // C#
	"requirements.txt", // Python fallback
}

// frameworkMarkerFiles are files whose presence (anywhere in the directory
// walk) signals a framework boundary even when no manifest is found.
var frameworkMarkerFiles = []string{
	"manage.py", // Django
	"wsgi.py",   // WSGI-based Python
	"asgi.py",   // ASGI-based Python
	"app.py",    // Flask / FastAPI common entry
	"main.py",   // FastAPI common entry
	"server.js", // Express
	"app.js",    // Express
	"index.js",  // Node.js entry
	"main.go",   // Go entry
}

// deriveOwningBackend walks up the directory tree from filePath until it
// finds a directory containing a manifest file or a framework marker, then
// returns the directory name as the owning_backend. Falls back to the
// top-level directory name if no manifest is found within 8 levels.
//
// Example: for `apps/api/handlers/users.py` it might find `apps/api` (if
// that directory contains `pyproject.toml`) and return "api".
func deriveOwningBackend(filePath string) string {
	dir := filepath.Dir(filePath)
	maxLevels := 8
	for i := 0; i < maxLevels; i++ {
		if dir == "." || dir == "" || dir == "/" {
			break
		}
		if directoryHasManifest(dir) {
			return filepath.Base(dir)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	// Fallback: use the top-level directory segment of the file path.
	// This covers single-backend repos where there is no nested manifest.
	parts := strings.SplitN(filepath.ToSlash(filePath), "/", 3)
	if len(parts) > 0 && parts[0] != "" && parts[0] != "." {
		return parts[0]
	}
	return "unknown"
}

// directoryHasManifest reports whether dir contains a manifest file or
// framework marker. Uses os.Stat so it works with both real file trees
// (during actual indexing) and in-memory test scenarios.
func directoryHasManifest(dir string) bool {
	allMarkers := append(manifestFileNames, frameworkMarkerFiles...)
	for _, name := range allMarkers {
		if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
			return true
		}
	}
	return false
}

// urlKindFromPath classifies a canonical URL path into one of three
// url_kind values used on http_endpoint_call entities (#1217):
//   - "dynamic_baseurl"  — the first path segment is a runtime placeholder
//   - "template_literal" — the path contains a mid-path placeholder
//   - "literal"          — fully static path string
func urlKindFromPath(canonicalPath string, runtimeDynamic bool) string {
	if runtimeDynamic || hasDynamicBaseURLPath(canonicalPath) {
		return "dynamic_baseurl"
	}
	// Mid-path template-literal placeholder: ${…} (JS) or {name} (generic)
	// at any position except the first segment (which is already handled above).
	if strings.Contains(canonicalPath, "${") {
		return "template_literal"
	}
	// Canonical path parameters like /users/{id} are NOT template literals —
	// those are static route patterns. Only flag mid-path {…} segments that
	// do NOT look like route parameters (i.e. the placeholder itself contains
	// a space, $, or operator character — indicating a variable expansion).
	return "literal"
}

// isXMLNamespacePath reports whether a canonical path looks like an XML
// XPath namespace reference rather than an HTTP route. These arise when
// YAML extraction rules fire on Python code that uses xml.etree, lxml,
// or python-docx XPath APIs (e.g. `element.find('./w:tblBorders')`).
//
// Rejected patterns:
//   - Paths containing a `prefix:Name` segment where `prefix` is a short
//     (≤4 chars) purely alphabetic string — classic XML namespace prefix.
//   - Paths with a `./` component (XPath relative path notation).
//   - Paths containing `[@` (XPath attribute selector syntax).
//
// This guard is deliberately conservative: a false-negative (letting an
// XML path through) is worse than a false-positive (dropping a
// legitimate route). Legitimate HTTP paths never contain `./` or `[@`
// and virtually never contain a short-prefix colon segment.
// isAdminRoute reports whether a Route entity represents a Django admin URL.
// Django admin is registered via `include(admin.site.urls)` which produces
// Route entities with view=admin.site.urls or path prefix "admin/".
// Suppressing these removes ~18.5% endpoint noise from typical Django projects.
// Ref #1412.
func isAdminRoute(e types.EntityRecord) bool {
	if e.Properties != nil {
		view := e.Properties["view"]
		if strings.Contains(view, "admin.site") {
			return true
		}
	}
	// Also catch sub-routes emitted with paths beginning admin/
	name := strings.ToLower(strings.TrimPrefix(e.Name, "/"))
	return strings.HasPrefix(name, "admin/") || name == "admin"
}

func isXMLNamespacePath(canonical string) bool {
	// XPath relative-path notation: ./elem or ./../elem
	if strings.Contains(canonical, "./") {
		return true
	}
	// XPath attribute selector: //div[@class='x']
	if strings.Contains(canonical, "[@") {
		return true
	}
	// XML namespace prefix: /api/w:tblBorders or just w:tblBorders
	// Scan each slash-separated segment for the `prefix:Name` pattern.
	segments := strings.Split(strings.TrimPrefix(canonical, "/"), "/")
	for _, seg := range segments {
		// Strip curly-brace path parameters before checking.
		seg = strings.TrimPrefix(strings.TrimSuffix(seg, "}"), "{")
		colonIdx := strings.IndexByte(seg, ':')
		if colonIdx <= 0 || colonIdx == len(seg)-1 {
			continue
		}
		prefix := seg[:colonIdx]
		// XML namespace prefixes are short (1–4 chars) and purely alphabetic.
		if len(prefix) > 4 {
			continue
		}
		allAlpha := true
		for _, c := range prefix {
			if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')) {
				allAlpha = false
				break
			}
		}
		if allAlpha {
			return true
		}
	}
	return false
}
