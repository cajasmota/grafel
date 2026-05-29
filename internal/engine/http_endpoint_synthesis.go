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
	// #1596: Infrastructure-as-Code languages have no compiled YAML rule sets
	// of their own (Terraform rules live under the `hcl` key; CloudFormation
	// YAML has none), so without this they would short-circuit out of Detect
	// before the IaC-aware synthesis passes run. Allowing them through lets
	// applyEventBusEdges (HCL EventBridge / serverless.yml) and applyIaCSNSEdges
	// (Terraform / CloudFormation SNS→SQS fan-out) scan the raw content. Files
	// with none of the recognised anchors are no-ops inside those passes.
	case "terraform", "hcl", "yaml":
		return true
	// #1708: Debezium / Kafka-Connect connector configs are JSON. The
	// classifier only routes path-narrow JSON files (cdc/, debezium/,
	// kafka-connect/, *-connector.json, …) to language="json", so this
	// case is reached only for likely-connector files. Files that don't
	// content-sniff as a connector are no-ops in applyDebeziumCDCEdges.
	case "json":
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
func applyHTTPEndpointSynthesis(args DetectorPassArgs) DetectorPassResult {
	lang := args.Lang
	path := args.Path
	content := args.Content
	entities := args.Entities
	relationships := args.Relationships
	if len(content) == 0 {
		return DetectorPassResult{Entities: entities, Relationships: relationships}
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
			// #1496 — dedup is SIDE-scoped. A producer-side definition and a
			// consumer-side call for the same (verb, path) are legitimately
			// distinct entities and must both survive: a gateway that SERVES
			// `POST /orders` (NestJS @Controller) while also CALLING a
			// downstream `POST /orders` (axios proxy) needs both synthetics or
			// the cross-repo consumer edge can never form. The dedup still
			// collapses same-side duplicates (e.g. AST-composed Route + YAML
			// regex both claiming the same producer endpoint) because those
			// share both the path-ID and the patternType.
			dedupKey := patternType + "\x00" + id
			if seen[dedupKey] {
				return
			}
			seen[dedupKey] = true

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

			// Issue #1725 — http_endpoint_definition/_call were emitted with
			// empty qualified_name in 100% of cases (638/638 on upvate-core).
			// The synthetic ID is already the canonical routable form
			// (e.g. "http:POST:/api/v1/inspections/{pk}/create"); use it as
			// the QN so downstream queries can join definitions, calls, and
			// cross-repo links on a stable, predictable field.
			entities = append(entities, types.EntityRecord{
				ID:                 id,
				Name:               id,
				QualifiedName:      id,
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

	// emitDef wraps emit and stamps StartLine on the just-appended entity.
	// Used by decorator-based python frameworks (Flask, FastAPI) where the
	// handler def lives in the same file as the decorator: source_file is
	// already correct, but source_line was previously left at 0. Issue
	// #2678 requires the line to point at the `def <handler>` line, not
	// the decorator line and not the default 0.
	emitDef := func(method, canonicalPath, framework, refKind, refName string, defLine int) {
		before := len(entities)
		emit(method, canonicalPath, framework, refKind, refName)
		if defLine > 0 && len(entities) > before {
			entities[len(entities)-1].StartLine = defLine
		}
	}

	// emitFile wraps emit and stamps `handler_file` (cross-file hint,
	// #2691 — Rails maps "users#index" to app/controllers/users_controller.rb)
	// plus StartLine (#2691 — Sinatra anchors the synthetic at its verb
	// block line in the same file). Either may be empty / 0.
	emitFile := func(method, canonicalPath, framework, refKind, refName, handlerFile string, defLine int) {
		before := len(entities)
		emit(method, canonicalPath, framework, refKind, refName)
		if len(entities) == before {
			return
		}
		last := &entities[len(entities)-1]
		if handlerFile != "" {
			if last.Properties == nil {
				last.Properties = map[string]string{}
			}
			last.Properties["handler_file"] = handlerFile
		}
		if defLine > 0 {
			last.StartLine = defLine
		}
	}

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
			// #1496 — mirror the side-scoped dedup key emitClient uses, so the
			// "did emitClient actually append a new entity?" check below is
			// correct (it stamps caller_file / url_kind on the appended entity).
			alreadySeen := seen["http_endpoint_client_synthesis\x00"+id]
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
		// #2980 — ASGI frameworks (Sanic / Litestar / Robyn) run BEFORE
		// Flask / FastAPI. Sanic's `@app.route` / `@app.get` shape overlaps
		// Flask's, and Robyn's `@app.get` shape overlaps FastAPI's, so the
		// file-signal-gated ASGI synthesizers must claim each (verb, path) ID
		// first to stamp the correct `framework` label. The side-scoped dedup
		// in makeEmit then prevents Flask/FastAPI from re-emitting the same ID.
		// Each ASGI synthesizer is gated on a framework-specific marker
		// (Sanic / Robyn / litestar|Controller) so it no-ops on plain
		// Flask/FastAPI files.
		synthesizeSanic(string(content), emitDef)
		synthesizeLitestar(string(content), emitDef)
		synthesizeRobyn(string(content), emitDef)
		// #2979 — aiohttp / Bottle run BEFORE Flask / FastAPI for the same
		// label-correctness reason. aiohttp's `@routes.get(...)` decorator and
		// Bottle's `@route(...)` / `@get(...)` decorators overlap Flask's
		// shorthand/generic shapes, so the file-signal-gated synthesizers must
		// claim each (verb, path) ID first to stamp `aiohttp` / `bottle`. The
		// side-scoped dedup in makeEmit then suppresses duplicate Flask
		// emission. aiohttp additionally gates on a server-routing signal
		// (`app.router.add_` / RouteTableDef) so pure-`ClientSession` files
		// no-op (dual-use skip).
		synthesizeAiohttp(string(content), emitDef)
		synthesizeBottle(string(content), emitDef)
		// Producer side: Flask + FastAPI run FIRST so their synthetics —
		// which carry a real handler function name as source_handler —
		// claim each ID before the Django composed-route pass walks the
		// generic YAML Route entities. Previously synthesizeDjangoFromComposed
		// ran first and dedup-stole every Flask/@blueprint.route(...) URL,
		// emitting a synthetic with `source_handler=Route:<path>`
		// (Spring-style placeholder), which the resolver dropped and
		// the response-shape extractor could not parse. #753.
		synthesizeFlask(string(content), emitDef)
		synthesizeFastAPI(string(content), emitDef)
		// #2690 — Starlette / Tornado / Pyramid endpoint synthesis.
		// Starlette and Pyramid use emitDef because the handler def lives in
		// the same file as the routing site in the common case; when the
		// handler is referenced symbolically and lives elsewhere, the
		// resolver's cross-file rebind (#2680) takes over.
		// Tornado handlers are classes with verb-named methods (`get`, `post`,
		// ...); the synthesizer enumerates those methods from the same-file
		// class body and stamps the def line of each method.
		synthesizeStarlette(string(content), emitDef)
		synthesizeTornado(string(content), emitDef)
		synthesizePyramid(string(content), emitDef)
		// Producer side: Django composed Routes (from django_routes.go).
		// Method is unknown statically, so emit with verb=ANY.
		synthesizeDjangoFromComposed(entities, path, emit)
		// Consumer side (#721, extends #533): requests / httpx /
		// aiohttp / urllib / session-style HTTP client calls.
		// Now emits FETCHES edges at extraction time.
		synthesizePyClientWithRuntime(string(content), emitClientRuntime)
	case "javascript", "typescript":
		// Capture the producer-side entity count before the JS/TS backend
		// synthesizers run so #2852 can resolve auth_coverage over exactly the
		// http_endpoint_definition entities this file just emitted.
		jstsAuthBefore := len(entities)
		// Producer side (#2851): Polka / Restify are Express-shaped but use
		// distinct receiver/import conventions. Run BEFORE synthesizeExpress
		// so their endpoints are tagged with the correct framework — the
		// side-scoped dedup in makeEmit lets whichever synthesizer claims a
		// (verb, path) ID first win, and Express would otherwise mislabel
		// these as "express". The file-signal gate (polka() / restify /
		// createServer()) keeps this a no-op on plain Express files.
		synthesizePolkaRestify(string(content), emit)
		// Producer side: Express (also catches Hono and Koa-Router whose
		// receivers match the `app`/`router` allowlist).
		synthesizeExpress(string(content), emit)
		// Producer side: NestJS @Controller + @Get/@Post/... decorators (#1418).
		synthesizeNestJS(string(content), emit)
		// Producer side: Fastify — `fastify.<verb>(...)` / `server.<verb>(...)`.
		// The Express synthesizer's receiver allowlist does not include
		// "fastify", so a dedicated pass is needed (#2678 audit).
		synthesizeFastify(string(content), emit)
		// Producer side (#2851): backend-HTTP frameworks with no prior
		// routing extraction. Each synthesizer is import-/path-guarded so it
		// no-ops on files that don't use that framework.
		//   AdonisJS  — Route.get(...) + Route.resource(...)
		//   Hapi      — server.route({ method, path, handler })
		//   Feathers  — app.use('/svc', service) → REST verb expansion
		//   Marble.js — r.pipe(r.matchPath(...), r.matchType(...))
		//   Polka/Restify — Express-shaped app.<verb>(...) with distinct receivers
		//   Sails     — config/routes.js declarative map
		// Adonis / Feathers / Sails attribute handlers to a Controller class
		// (or service) by NAME — `UsersController.index`, `MessageService`,
		// `WidgetController.find`. The resolver's same-name symbol match can't
		// bind the dotted `Controller.method` ref directly, so these
		// synthesizers emit a `handler_file` hint (the controller/service
		// basename) and reference the bare method name, mirroring the Rails
		// (#2691) / Phoenix (#2692) cross-file attribution mechanism.
		synthesizeAdonis(string(content), emitFile)
		synthesizeHapi(string(content), emit)
		synthesizeFeathers(string(content), emitFile)
		synthesizeMarble(string(content), emit)
		synthesizeSails(path, string(content), emitFile)
		// Producer side: Next.js API routes (pages/api/*, app/api/*/route.ts).
		// The route is implicit from the file path, not from a call site.
		synthesizeNextAPIRoute(path, string(content), emit)
		// Producer side: Apollo / GraphQL resolvers (#1422). GraphQL is
		// schema-first rather than REST, so resolver fields are emitted as
		// graphql_field endpoint-ish entities keyed by operation + field.
		gqlTransportBefore := len(entities)
		synthesizeGraphQLResolvers(string(content), emit)
		// #2906 — RPC transport binding. GraphQL resolvers are
		// transport-agnostic; the server-setup adapter (startStandaloneServer /
		// expressMiddleware over HTTP, graphql-ws useServer over WS) decides the
		// wire protocol. Stamp `transport` on the resolver-field synthetics this
		// file just emitted when an adapter signal is present in the module.
		applyRPCTransportBinding(string(content), entities, gqlTransportBefore,
			"graphql", gqlHTTPAdapterSignals, gqlWSAdapterSignals)
		// Producer side: tRPC procedure resolvers (#2693). Each leaf
		// procedure inside a `router({ ... })` literal becomes an
		// addressable endpoint identified by its dotted path
		// (`users.list`, `users.create`). Verb mapping: .query → GET,
		// .mutation → POST, .subscription → SUBSCRIBE. The synthesizer
		// uses emitDef so source_line points at the .query / .mutation /
		// .subscription call site (the arrow function's def line);
		// because the leaf is an inline arrow expression with no
		// addressable handler symbol, no source_handler is stamped — the
		// shared resolver short-circuits the rebind and preserves the
		// precise attribution this synthesizer produces.
		trpcTransportBefore := len(entities)
		synthesizeTRPC(string(content), emitDef)
		// #2906 — RPC transport binding. tRPC routers are transport-agnostic;
		// the adapter that serves them (createHTTPServer / fetchRequestHandler /
		// express|fastify|next adapter over HTTP, applyWSSHandler over WS)
		// decides the wire protocol. Stamp `transport` on the procedure
		// synthetics this file just emitted when an adapter signal is present.
		applyRPCTransportBinding(string(content), entities, trpcTransportBefore,
			"trpc", trpcHTTPAdapterSignals, trpcWSAdapterSignals)
		// #2865 — tRPC input-schema extraction. Recover each procedure's
		// `.input(z.object({…}))` validator and stamp input_schema /
		// input_schema_lib / has_input_schema on the procedure synthetics this
		// file just emitted. Keyed on the shared dotted `path` property.
		applyTRPCSchemaBinding(string(content), entities, trpcTransportBefore)
		// #2852 — resolve auth_coverage over the producer-side endpoints emitted
		// above. Detects passport/express-jwt/session middleware, Nest
		// @UseGuards (class + method), Hapi route auth, AdonisJS .middleware('auth')
		// and Marble.js auth effects, stamping the auth_policy property contract
		// (auth_policy/auth_method/auth_confidence/auth_required + the MCP
		// signal-1 auth_middleware/auth_guard keys). Runs before the consumer
		// (client) synthesizers so it only sees producer http_endpoint_definition
		// entities.
		applyJSTSAuthPolicy(string(content), path, entities, jstsAuthBefore)
		// #2853 — resolve middleware_coverage over the same producer-side
		// endpoints. The middleware chain is the superset of auth (auth is one
		// kind of middleware): app.use/router.use global chains, per-route
		// middleware arrays (Express/Koa/Hono/Polka/Restify), Fastify hooks,
		// NestJS interceptor/pipe/filter/guard decorators, Hapi ext/pre points,
		// AdonisJS named middleware, Feathers hooks and Marble per-effect
		// middleware. Stamps middleware_chain/middleware_count/middleware_names/
		// middleware_scope. Shares the jstsAuthBefore window so it only sees the
		// producer http_endpoint_definition entities this file emitted.
		applyJSTSMiddlewareCoverage(string(content), path, entities, jstsAuthBefore)
		// Consumer side (#721): fetch / axios / generic *Client
		// HTTP client calls. Now emits FETCHES edges at extraction time.
		//
		// #2709 — JS/TS extractor enumerates object-subscript template-literal
		// interpolations and tags resulting entities with a
		// `polymorphic_subscript` property. The adapter below bridges the
		// shared (verb, path, framework, refKind, refName, runtimeDynamic)
		// closure with the JS-specific extra polySubscript argument: after the
		// upstream emit appends the entity, we stamp the property in place.
		emitJSClientRuntime := func(method, canonicalPath, framework, refKind, refName string, runtimeDynamic bool, polySubscript string) {
			before := len(entities)
			emitClientRuntime(method, canonicalPath, framework, refKind, refName, runtimeDynamic)
			if polySubscript == "" || len(entities) == before {
				return
			}
			last := &entities[len(entities)-1]
			if last.Properties == nil {
				last.Properties = map[string]string{}
			}
			last.Properties["polymorphic_subscript"] = polySubscript
		}
		synthesizeFetchAxiosWithRuntime(string(content), emitJSClientRuntime)
	case "go":
		// Producer side: Gin / Echo / Chi route registrations. #722.
		synthesizeGoRouters(string(content), emit)
		// Producer side: gorilla/mux (#2684), net/http stdlib including
		// Go 1.22 method-prefix patterns (#2685), and huma OpenAPI
		// (#2686). Each synthesizer is independently import-guarded so
		// it no-ops on files that don't use that framework. They use
		// emitDef so the registration call's line number is stamped on
		// each synthetic; the shared resolver then stashes that line as
		// `registration_start_line` before rebinding StartLine to the
		// handler def.
		synthesizeGorillaMux(string(content), emitDef)
		synthesizeNetHTTPStdlib(string(content), emitDef)
		synthesizeHuma(string(content), emitDef)
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
		// Producer side (#2691): Rails routes.rb DSL + Sinatra verb blocks.
		// Rails synthesizer derives the expected controller file path from the
		// "users#index" handler ref + enclosing namespace stack and stamps it as
		// `handler_file`; the resolver post-pass (#2680 rebind) consumes that hint
		// for path-targeted same-file lookup.
		synthesizeRailsRoutes(string(content), path, emitFile)
		// Sinatra blocks are inline — same-file by construction. emitFile stamps
		// StartLine on the synthetic so the audit2678 attribution lands on the
		// verb block's line in app.rb.
		synthesizeSinatra(string(content), path, emitFile)
		// Consumer side (#721 wave 2b): Net::HTTP, Faraday, HTTParty, RestClient.
		synthesizeRubyClientWithRuntime(string(content), emitClientRuntime)
	case "csharp":
		// Producer side (#2692): ASP.NET Core attribute routing —
		// [HttpGet/Post/...] + class-level [Route("/api/[controller]")].
		synthesizeASPNetCore(string(content), emit)
		// Consumer side (#721 wave 2b): HttpClient, RestSharp, Refit, WebClient.
		synthesizeCSharpClientWithRuntime(string(content), emitClientRuntime)
	case "rust":
		// Producer side (#1420): axum Router::new().route(...) registrations.
		synthesizeAxumRoutes(string(content), emit)
		// Producer side (#2692): Rocket attribute macros
		// (#[get("/path")], #[post("/path")], ...).
		synthesizeRocket(string(content), emit)
		// Consumer side (#721 wave 2c): reqwest, hyper, ureq, surf.
		synthesizeRustClientWithRuntime(string(content), emitClientRuntime)
	case "php":
		// Producer side (#1419): Laravel Route::verb/resource/apiResource.
		synthesizeLaravel(string(content), emit)
		// Consumer side (#721 wave 2c): Guzzle, Symfony HttpClient, cURL, file_get_contents,
		// WordPress HTTP API, Laravel Http facade.
		synthesizePHPClientWithRuntime(string(content), emitClientRuntime)
	case "elixir":
		// Producer side (#2692): Phoenix router file scope/verb/resources.
		// The synth supplies a controller-module snake_case file hint
		// (e.g. `user_controller`); we stamp it as `handler_file` on the
		// just-emitted synthetic so the resolver substring-matches it
		// against candidate handler entities (#2692 extension of the
		// #2691 Rails handler_file mechanism).
		synthesizePhoenix(string(content), func(method, canonicalPath, framework, refKind, refName, fileHint string) {
			before := len(entities)
			emit(method, canonicalPath, framework, refKind, refName)
			if fileHint == "" || len(entities) == before {
				return
			}
			last := &entities[len(entities)-1]
			if last.Properties == nil {
				last.Properties = map[string]string{}
			}
			last.Properties["handler_file"] = fileHint
		})
		// Consumer side (#1483): Finch.build(:verb, url) + HTTPoison.<verb>(url).
		synthesizeElixirHTTPClients(string(content), emitClient)
	}

	// #722 — response/request shape extraction. Mutates Properties on
	// the synthetic entities emitted above; never adds or removes
	// entities, so it cannot regress the bug-rate of upstream passes.
	applyResponseShapes(lang, content, entities)

	return DetectorPassResult{Entities: entities, Relationships: relationships}
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

func synthesizeFlask(content string, emit emitDefFn) {
	if !strings.Contains(content, ".route(") && !strings.Contains(content, ".get(") &&
		!strings.Contains(content, ".post(") && !strings.Contains(content, ".put(") &&
		!strings.Contains(content, ".patch(") && !strings.Contains(content, ".delete(") {
		return
	}
	// Shorthand verbs first — they have an unambiguous verb. Use the
	// SubmatchIndex variant so we can derive the 1-based line of the
	// handler `def` (capture group 3) for issue #2678 attribution.
	for _, idx := range flaskRouteVerbDecoratorRe.FindAllStringSubmatchIndex(content, -1) {
		if len(idx) < 8 {
			continue
		}
		verb := strings.ToUpper(content[idx[2]:idx[3]])
		raw := content[idx[4]:idx[5]]
		handler := content[idx[6]:idx[7]]
		canonical := httproutes.Canonicalize(httproutes.FrameworkFlask, raw)
		defLine := lineOfOffset(content, idx[6])
		emit(verb, canonical, "flask", "Controller", handler, defLine)
	}
	// Generic .route(...) form.
	for _, idx := range flaskRouteRe.FindAllStringSubmatchIndex(content, -1) {
		if len(idx) < 8 {
			continue
		}
		raw := content[idx[2]:idx[3]]
		extras := content[idx[4]:idx[5]]
		handler := content[idx[6]:idx[7]]
		canonical := httproutes.Canonicalize(httproutes.FrameworkFlask, raw)
		defLine := lineOfOffset(content, idx[6])
		methods := parseFlaskMethods(extras)
		if len(methods) == 0 {
			// Flask's default: no `methods` kwarg means GET only.
			methods = []string{"GET"}
		}
		for _, verb := range methods {
			emit(verb, canonical, "flask", "Controller", handler, defLine)
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

func synthesizeFastAPI(content string, emit emitDefFn) {
	if !strings.Contains(content, "FastAPI") && !strings.Contains(content, "APIRouter") &&
		!strings.Contains(content, "@app.") && !strings.Contains(content, "@router.") {
		return
	}
	// SubmatchIndex variant so the 1-based line of the handler `def`
	// (capture group 3) can be recovered for issue #2678 attribution.
	for _, idx := range fastapiVerbDecoratorRe.FindAllStringSubmatchIndex(content, -1) {
		if len(idx) < 8 {
			continue
		}
		verb := strings.ToUpper(content[idx[2]:idx[3]])
		raw := content[idx[4]:idx[5]]
		handler := content[idx[6]:idx[7]]
		canonical := httproutes.Canonicalize(httproutes.FrameworkFastAPI, raw)
		defLine := lineOfOffset(content, idx[6])
		emit(verb, canonical, "fastapi", "Controller", handler, defLine)
	}
}

// ---------------------------------------------------------------------------
// Starlette (Python) — #2690
// ---------------------------------------------------------------------------
//
// Starlette declares routes as a flat list of `Route(...)` instances and
// optional `Mount("/prefix", routes=[...])` blocks that nest a sub-list under
// a path prefix. The canonical shape is:
//
//	Route("/users/{id}", endpoint=get_user, methods=["GET"])
//
// `endpoint=` is the handler reference (a function or class). `methods=` is
// the verb list — when omitted Starlette defaults to GET. We emit one
// http_endpoint_definition per (verb, path) tuple. The handler reference is
// forwarded as `SCOPE.Operation:<name>` so the cross-file resolver rebind
// (#2680) can attribute the endpoint to the handler file when the routes
// module and the handler module are split.

// starletteRouteRe captures Route("/path", endpoint=<handler>, methods=[...]).
// The `endpoint=` and `methods=` kwargs may appear in either order, and either
// may be absent. We match the path argument positionally (first string
// literal) and then scan the remainder of the call for `endpoint=` and
// `methods=` separately so kwarg ordering does not affect extraction.
//
// Capture groups: 1 = path literal.
var starletteRouteRe = regexp.MustCompile(
	`\bRoute\s*\(\s*["']([^"'\n\r]+)["']([^)]*)\)`,
)

// starletteEndpointKwargRe captures `endpoint=<identifier>` inside the tail
// of a Route(...) call. The handler may be a dotted name (`mod.handler`); we
// keep the final segment because that is the entity name the SCOPE.Operation
// extractor uses.
var starletteEndpointKwargRe = regexp.MustCompile(`endpoint\s*=\s*([A-Za-z_][\w.]*)`)

// starletteMethodsKwargRe captures the methods=[...] list. Both list and
// tuple literals are accepted, matching the Flask methods extractor.
var starletteMethodsKwargRe = regexp.MustCompile(`methods\s*=\s*[\[\(]([^\]\)]+)[\]\)]`)

// starletteMountRe captures Mount("/prefix", routes=...) so we can join the
// prefix onto each Route inside. Tracking the mount span via braces would
// require a balanced-paren walk; instead we use a single-mount heuristic
// that handles the dominant convention (one Mount("/api", routes=routes)
// wrapping a routes module) by emitting both prefixed and unprefixed
// synthetics when a Mount appears in the same file. The byPath linker
// normalizes leading dynamic segments across files.
var starletteMountRe = regexp.MustCompile(
	`\bMount\s*\(\s*["']([^"'\n\r]+)["']`,
)

func synthesizeStarlette(content string, emit emitDefFn) {
	if !strings.Contains(content, "Route(") {
		return
	}
	// Detect a single same-file Mount prefix (dominant convention). Multiple
	// Mounts in one file fall back to no-prefix attribution; the cross-repo
	// linker tolerates that because the byPath index normalises leading
	// dynamic segments.
	mountPrefix := ""
	if mm := starletteMountRe.FindStringSubmatch(content); len(mm) >= 2 {
		mountPrefix = strings.TrimRight(mm[1], "/")
	}

	for _, idx := range starletteRouteRe.FindAllStringSubmatchIndex(content, -1) {
		if len(idx) < 6 {
			continue
		}
		raw := content[idx[2]:idx[3]]
		tail := content[idx[4]:idx[5]]

		handler := ""
		if em := starletteEndpointKwargRe.FindStringSubmatchIndex(tail); len(em) >= 4 {
			handler = tail[em[2]:em[3]]
			// Keep only the final dotted segment as the entity name.
			if i := strings.LastIndexByte(handler, '.'); i >= 0 {
				handler = handler[i+1:]
			}
		}

		methods := parseStarletteMethods(tail)
		if len(methods) == 0 {
			// Starlette default: GET only.
			methods = []string{"GET"}
		}

		fullPath := raw
		if mountPrefix != "" {
			fullPath = joinPathFragments(mountPrefix, raw)
		}
		canonical := httproutes.Canonicalize(httproutes.FrameworkStarlette, fullPath)

		// Def-line: when the handler is defined in this file the line of
		// `def <handler>` is recoverable; otherwise the synthesiser falls
		// back to the Route(...) line so the integration test still has a
		// non-zero anchor. The resolver rebind (#2680) replaces both file
		// and line at handler-resolution time when a SCOPE.Operation match
		// exists in another file.
		defLine := 0
		if handler != "" {
			defLine = findPyDefLine(content, handler)
		}
		if defLine == 0 {
			defLine = lineOfOffset(content, idx[0])
		}

		for _, verb := range methods {
			emit(verb, canonical, "starlette", "SCOPE.Operation", handler, defLine)
		}
	}
}

// parseStarletteMethods returns the verbs declared in a `methods=[...]`
// kwarg inside a Route(...) call. Mirrors parseFlaskMethods; kept separate
// so future Starlette-specific quirks (Starlette accepts `methods=None`
// meaning "all verbs") can be folded in without disturbing Flask.
func parseStarletteMethods(args string) []string {
	mm := starletteMethodsKwargRe.FindStringSubmatch(args)
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
// Tornado (Python) — #2690
// ---------------------------------------------------------------------------
//
// Tornado registers routes via `Application([...])` where each tuple is
// `(pattern, HandlerClass)` and the verbs come from the HTTP-method-named
// methods present on the class (e.g. `def get(self):`, `def post(self):`).
// The class typically inherits from `tornado.web.RequestHandler`.
//
// In the dominant convention the Application(...) list and the handler
// class live in the same file. We extract:
//
//	1. Application(...) entries — pairs of (regex pattern, ClassName).
//	2. RequestHandler subclasses defined in the same file and their HTTP
//	   verb methods.
//
// For each registration we look up the class and emit one synthetic per
// verb method present, stamped at that method's `def` line. When the class
// is not in this file, we emit a single ANY synthetic with the class name
// as `SCOPE.Class:<Name>` so the cross-file resolver rebind (#2680) can
// retarget the synthetic to the handler file. The verb-per-method
// expansion is deferred to the same-file path; cross-file Tornado is rare
// enough that ANY is acceptable.

// tornadoAppEntryRe matches a single (pattern, Handler) tuple inside an
// Application([...]) constructor. We do not match the surrounding
// Application(...) call because it may span many lines; instead we anchor
// on the tuple shape directly. The Tornado-specific signals that gate this
// pass (RequestHandler subclass or Application( ) call elsewhere in the
// file) prevent it from firing on generic Python tuples.
//
// Capture groups: 1 = pattern (raw), 2 = HandlerClass name (last dotted
// segment retained).
var tornadoAppEntryRe = regexp.MustCompile(
	`\(\s*r?["']([^"'\n\r]+)["']\s*,\s*([A-Za-z_][\w.]*)\s*\)`,
)

// tornadoHandlerClassRe matches a class declaration that inherits from
// something containing "RequestHandler" (covers `tornado.web.RequestHandler`,
// `web.RequestHandler`, bare `RequestHandler`, and project-internal base
// classes that themselves end with `RequestHandler`).
//
// Capture groups: 1 = class name.
var tornadoHandlerClassRe = regexp.MustCompile(
	`(?m)^class\s+([A-Za-z_][\w]*)\s*\([^)]*RequestHandler[^)]*\)\s*:`,
)

// tornadoVerbMethodRe matches an HTTP-verb-named method declaration. Used
// to enumerate the verbs implemented on a RequestHandler subclass.
//
// Capture groups: 1 = verb (lowercase).
var tornadoVerbMethodRe = regexp.MustCompile(
	`(?m)^[ \t]+(?:async\s+)?def\s+(get|post|put|patch|delete|head|options)\s*\(`,
)

func synthesizeTornado(content string, emit emitDefFn) {
	if !strings.Contains(content, "RequestHandler") && !strings.Contains(content, "Application(") {
		return
	}

	// Build a same-file class index: ClassName → {verbs, defLines}.
	type classInfo struct {
		verbs    []string
		defLines map[string]int // upper-case verb → 1-based def line
	}
	classes := map[string]*classInfo{}
	for _, cm := range tornadoHandlerClassRe.FindAllStringSubmatchIndex(content, -1) {
		if len(cm) < 4 {
			continue
		}
		name := content[cm[2]:cm[3]]
		bodyStart := cm[1]
		bodyEnd := findPyClassBodyEnd(content, bodyStart)
		body := content[bodyStart:bodyEnd]
		info := &classInfo{defLines: map[string]int{}}
		for _, vm := range tornadoVerbMethodRe.FindAllStringSubmatchIndex(body, -1) {
			if len(vm) < 4 {
				continue
			}
			verb := strings.ToUpper(body[vm[2]:vm[3]])
			if _, dup := info.defLines[verb]; dup {
				continue
			}
			info.verbs = append(info.verbs, verb)
			info.defLines[verb] = lineOfOffset(content, bodyStart+vm[0])
		}
		classes[name] = info
	}

	// Walk every (pattern, Handler) tuple in the file.
	for _, m := range tornadoAppEntryRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 6 {
			continue
		}
		raw := content[m[2]:m[3]]
		handler := content[m[4]:m[5]]
		// Keep only the final dotted segment.
		if i := strings.LastIndexByte(handler, '.'); i >= 0 {
			handler = handler[i+1:]
		}
		// Convert the Python regex pattern into the canonical {name} form
		// before passing it to the canonicaliser.
		pyPath := tornadoRewritePattern(raw)
		canonical := httproutes.Canonicalize(httproutes.FrameworkTornado, pyPath)
		if canonical == "" {
			continue
		}

		info, sameFile := classes[handler]
		if sameFile && len(info.verbs) > 0 {
			// Forward the per-verb method as the handler reference. The
			// Python extractor emits class methods as
			// SCOPE.Operation:<ClassName>.<method> (verified against the
			// indexed corpus); using that exact (kind, name) lets the
			// resolver rebind source_file/start_line/end_line to the
			// method entity directly, which is what the #2678 audit
			// requires (def-line attribution, never the registration site).
			for _, verb := range info.verbs {
				methodName := handler + "." + strings.ToLower(verb)
				emit(verb, canonical, "tornado", "SCOPE.Operation", methodName, info.defLines[verb])
			}
			continue
		}
		// Cross-file: emit a single ANY synthetic referencing the handler
		// class. SCOPE.Component is the Python extractor's class kind; the
		// resolver rebind retargets file/line when the class is found in
		// another module. (SCOPE.Class is included as a fallback in
		// resolverKindEquivalents, so the rebind still works if a future
		// extractor change moves classes back to SCOPE.Class.)
		emit("ANY", canonical, "tornado", "SCOPE.Component", handler, lineOfOffset(content, m[0]))
	}
}

// tornadoRewritePattern rewrites a Tornado regex URL pattern into the
// canonical `{name}` form used across all synthesizers. Three forms are
// recognised:
//
//	(?P<name>regex)  → {name}
//	(regex)          → {}
//	other characters → passed through
//
// The output is then handed to httproutes.Canonicalize(FrameworkTornado, ...)
// which strips any residual `:regex` constraints and normalises slashes.
// We delegate `(?P<...)` handling to stripPythonNamedGroups via a small
// wrapper rather than re-implementing the balanced-paren walker.
func tornadoRewritePattern(raw string) string {
	// Trim Tornado's common anchors so the canonicaliser sees the path.
	raw = strings.TrimPrefix(raw, "^")
	raw = strings.TrimSuffix(raw, "$")
	// Strip Python named groups first.
	out := stripPythonNamedGroupsExported(raw)
	// Rewrite remaining bare capture groups `(...)` to `{}`.
	var b strings.Builder
	b.Grow(len(out))
	depth := 0
	i := 0
	for i < len(out) {
		c := out[i]
		switch c {
		case '\\':
			// Keep escape sequences verbatim.
			if i+1 < len(out) {
				b.WriteByte(c)
				b.WriteByte(out[i+1])
				i += 2
				continue
			}
		case '[':
			// Character classes are opaque — copy until matching ']'.
			j := i + 1
			if j < len(out) && out[j] == ']' {
				j++
			}
			for j < len(out) && out[j] != ']' {
				if out[j] == '\\' && j+1 < len(out) {
					j += 2
					continue
				}
				j++
			}
			if j < len(out) {
				j++
			}
			// Replace the whole character class with `{}` only if it is
			// the body of a capture group we are inside. Otherwise emit
			// it verbatim. The depth-aware logic below handles the inner
			// case by skipping content when depth > 0.
			if depth == 0 {
				b.WriteString(out[i:j])
			}
			i = j
			continue
		case '(':
			depth++
			if depth == 1 {
				b.WriteString("{}")
			}
			i++
			continue
		case ')':
			if depth > 0 {
				depth--
			}
			i++
			continue
		}
		if depth == 0 {
			b.WriteByte(c)
		}
		i++
	}
	// Convert angle-bracket placeholders left by stripPythonNamedGroups
	// (`<name>`) into the curly-brace canonical form.
	res := b.String()
	res = canonicaliseAngleBracketLite(res)
	return res
}

// stripPythonNamedGroupsExported is a thin wrapper around the unexported
// httproutes.stripPythonNamedGroups so the tornado rewriter can reuse the
// balanced-paren walker without re-implementing it. We keep the alias here
// (rather than exporting the original) because the httproutes package's
// contract is "framework path → canonical path"; the rewrite here is a
// pre-canonicalisation regex transform that does not belong in that API.
func stripPythonNamedGroupsExported(raw string) string {
	const marker = "(?P<"
	if !strings.Contains(raw, marker) {
		return raw
	}
	var b strings.Builder
	b.Grow(len(raw))
	i := 0
	for i < len(raw) {
		idx := strings.Index(raw[i:], marker)
		if idx < 0 {
			b.WriteString(raw[i:])
			break
		}
		b.WriteString(raw[i : i+idx])
		nameStart := i + idx + len(marker)
		nameEnd := strings.IndexByte(raw[nameStart:], '>')
		if nameEnd < 0 {
			b.WriteString(raw[i+idx:])
			break
		}
		name := raw[nameStart : nameStart+nameEnd]
		bodyStart := nameStart + nameEnd + 1
		depth := 1
		j := bodyStart
		for j < len(raw) && depth > 0 {
			c := raw[j]
			switch c {
			case '\\':
				j += 2
				continue
			case '[':
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
					j++
				}
				continue
			case '(':
				depth++
			case ')':
				depth--
				if depth == 0 {
					j++
				} else {
					j++
				}
				continue
			}
			j++
		}
		b.WriteByte('<')
		b.WriteString(name)
		b.WriteByte('>')
		i = j
	}
	return b.String()
}

// canonicaliseAngleBracketLite rewrites `<name>` placeholders left by
// stripPythonNamedGroupsExported into `{name}`. This is a Tornado-local
// helper kept distinct from httproutes.canonicalizeAngleBrackets so the
// surrounding regex anchors (`\d+`, `[^/]+`) embedded inside Tornado's
// named groups are not interpreted as Django/Flask converter prefixes.
func canonicaliseAngleBracketLite(raw string) string {
	if !strings.ContainsAny(raw, "<>") {
		return raw
	}
	var b strings.Builder
	b.Grow(len(raw))
	i := 0
	for i < len(raw) {
		if raw[i] != '<' {
			b.WriteByte(raw[i])
			i++
			continue
		}
		end := strings.IndexByte(raw[i+1:], '>')
		if end < 0 {
			b.WriteByte(raw[i])
			i++
			continue
		}
		name := strings.TrimSpace(raw[i+1 : i+1+end])
		if name == "" {
			b.WriteString("{}")
		} else {
			b.WriteByte('{')
			b.WriteString(name)
			b.WriteByte('}')
		}
		i += 1 + end + 1
	}
	return b.String()
}

// findPyClassBodyEnd returns the byte offset of the end of a Python class
// body that opens immediately after `start`. The body is the contiguous
// run of lines whose indentation is strictly greater than the class
// declaration's. Returns len(content) when the body extends to EOF.
func findPyClassBodyEnd(content string, start int) int {
	// Skip to the start of the next line (after the `:` and newline).
	nl := strings.IndexByte(content[start:], '\n')
	if nl < 0 {
		return len(content)
	}
	pos := start + nl + 1
	for pos < len(content) {
		// Read one line.
		lineEnd := strings.IndexByte(content[pos:], '\n')
		var line string
		if lineEnd < 0 {
			line = content[pos:]
		} else {
			line = content[pos : pos+lineEnd]
		}
		// A blank or whitespace-only line is part of the body.
		trimmed := strings.TrimRight(line, " \t\r")
		if trimmed == "" {
			if lineEnd < 0 {
				return len(content)
			}
			pos += lineEnd + 1
			continue
		}
		// A line that starts with no leading whitespace ends the class body.
		if line[0] != ' ' && line[0] != '\t' {
			return pos
		}
		if lineEnd < 0 {
			return len(content)
		}
		pos += lineEnd + 1
	}
	return len(content)
}

// findPyDefLine returns the 1-based line of `def <name>(` or `async def
// <name>(` in content, or 0 when not present. Used by the Starlette and
// Pyramid synthesizers to attribute the synthetic at the handler def line
// when the handler is defined in the same file as the routing site.
func findPyDefLine(content, name string) int {
	if name == "" {
		return 0
	}
	// Anchor on `def <name>(`; allow `async def` and arbitrary leading
	// whitespace. Search the whole file — multiple defs with the same name
	// are uncommon and the first match is the desired one in practice.
	pat := regexp.MustCompile(`(?m)^[ \t]*(?:async\s+)?def\s+` + regexp.QuoteMeta(name) + `\s*\(`)
	loc := pat.FindStringIndex(content)
	if loc == nil {
		return 0
	}
	return lineOfOffset(content, loc[0])
}

// ---------------------------------------------------------------------------
// Pyramid (Python) — #2690
// ---------------------------------------------------------------------------
//
// Pyramid's URL ↔ handler binding is two-step:
//
//	1. config.add_route("route_name", "/path/{id}") declares the URL and
//	   names it.
//	2. @view_config(route_name="route_name", request_method="GET") on a
//	   handler function/class binds the URL name to a handler + verb.
//
// The two declarations frequently live in different modules: a routes /
// __init__.py module calls add_route, while views.py declares the handler
// with @view_config. The synthesizer recovers the linkage in two passes:
//
//	Pass A (corpus-wide concern, deferred): scan add_route calls per file.
//	Pass B (per-file): scan @view_config decorators and, when the matching
//	  add_route lives in the same file, emit the resolved (verb, path) pair.
//
// For this issue we implement the same-file pairing path (dominant on the
// fixtures and on the indexed corpora). When add_route lives in a sibling
// module the synthesizer still emits a synthetic per @view_config with a
// deliberately-namespaced fallback path `/_pyramid_unbound_route_/{name}`
// so it never collides with a real path and is easy to spot. The cross-file
// resolver rebind (#2680) then attributes the synthetic to the handler file,
// and the byPath index remains uncorrupted.

// pyramidAddRouteRe captures `config.add_route("name", "/path")`. The
// receiver name is flexible (`config`, `cfg`, `app`, `c`); we accept any
// identifier so projects using their own conventions still match.
//
// Capture groups: 1 = route name, 2 = raw path.
var pyramidAddRouteRe = regexp.MustCompile(
	`\b\w+\.add_route\s*\(\s*["']([^"'\n\r]+)["']\s*,\s*["']([^"'\n\r]+)["']`,
)

// pyramidViewConfigRe captures the @view_config(...) decorator and the
// following function/class name. The kwargs may appear in any order; we
// extract route_name and request_method via separate regexes from the
// captured kwarg blob.
//
// Capture groups: 1 = kwarg blob, 2 = decorated function/class name.
var pyramidViewConfigRe = regexp.MustCompile(
	`@view_config\s*\(([^)]*)\)\s*[\r\n]+(?:\s*@[^\n\r]*[\r\n]+)*` +
		`\s*(?:async\s+)?(?:def|class)\s+([A-Za-z_][\w]*)`,
)

// pyramidRouteNameRe extracts route_name="..." from a view_config kwarg blob.
var pyramidRouteNameRe = regexp.MustCompile(`route_name\s*=\s*["']([^"'\n\r]+)["']`)

// pyramidRequestMethodRe extracts request_method="..." from a view_config
// kwarg blob. Pyramid also accepts a list/tuple form
// (`request_method=("GET","POST")`); both shapes are recognised.
var pyramidRequestMethodRe = regexp.MustCompile(
	`request_method\s*=\s*(?:["']([^"'\n\r]+)["']|[\[\(]([^\]\)]+)[\]\)])`,
)

func synthesizePyramid(content string, emit emitDefFn) {
	if !strings.Contains(content, "view_config") && !strings.Contains(content, "add_route") {
		return
	}

	// Build the same-file route_name → raw path map.
	routes := map[string]string{}
	for _, m := range pyramidAddRouteRe.FindAllStringSubmatch(content, -1) {
		if len(m) < 3 {
			continue
		}
		routes[m[1]] = m[2]
	}

	for _, idx := range pyramidViewConfigRe.FindAllStringSubmatchIndex(content, -1) {
		if len(idx) < 6 {
			continue
		}
		kwargs := content[idx[2]:idx[3]]
		handler := content[idx[4]:idx[5]]

		var routeName string
		if rm := pyramidRouteNameRe.FindStringSubmatch(kwargs); len(rm) >= 2 {
			routeName = rm[1]
		}
		if routeName == "" {
			// view_config without a route_name is registered via a
			// traversal or a context predicate — out of scope for the
			// REST-style HTTP endpoint pass.
			continue
		}

		// Same-file add_route lookup. When absent we still emit a
		// synthetic so the resolver can rebind to the handler file; the
		// canonical path is the unknown-route placeholder
		// `/{route_name}` so it never collides with a real path and is
		// easy to spot in the dashboard. A follow-up will widen this to
		// a cross-module add_route scan.
		raw, known := routes[routeName]
		if !known {
			raw = "/_pyramid_unbound_route_/" + routeName
		}

		methods := parsePyramidMethods(kwargs)
		if len(methods) == 0 {
			// Pyramid's default: when request_method is unset the view
			// matches any verb. We emit ANY rather than a list of every
			// HTTP method.
			methods = []string{"ANY"}
		}

		canonical := httproutes.Canonicalize(httproutes.FrameworkPyramid, raw)
		defLine := findPyDefLine(content, handler)
		if defLine == 0 {
			defLine = lineOfOffset(content, idx[0])
		}
		for _, verb := range methods {
			emit(verb, canonical, "pyramid", "SCOPE.Operation", handler, defLine)
		}
	}
}

// parsePyramidMethods returns the verbs declared on a `request_method=`
// kwarg, accepting either a single string or a list/tuple of strings.
func parsePyramidMethods(kwargs string) []string {
	mm := pyramidRequestMethodRe.FindStringSubmatch(kwargs)
	if len(mm) < 3 {
		return nil
	}
	body := mm[1]
	if body == "" {
		body = mm[2]
	}
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
// Sanic (Python) — #2980
// ---------------------------------------------------------------------------
//
// Sanic is an ASGI-native framework whose routing idioms mirror Flask:
//
//	app = Sanic("app")
//	@app.get("/users/<int:user_id>")
//	@app.route("/items", methods=["GET", "POST"])
//	async def handler(request): ...
//
//	bp = Blueprint("v1", url_prefix="/v1")
//	@bp.get("/resource")
//	async def handler(request): ...
//
// Path parameters use the Flask-style `<converter:name>` / `<name>` angle-bracket
// convention, so canonicalisation reuses the Flask/Django angle-bracket walker
// (FrameworkSanic is grouped with them in Canonicalize).
//
// Blueprint prefix composition is handled the same way Flask blueprints are
// conceptually handled, but Sanic encodes the prefix on the Blueprint
// constructor (`url_prefix="/v1"`) rather than on the registration site. We
// build a same-file map of {blueprint receiver → url_prefix} and prepend the
// prefix to every route decorated on that receiver. The dominant convention
// (blueprint + its routes in one module) is covered; cross-module blueprints
// fall back to the unprefixed path, which the byPath linker normalises.

// sanicBlueprintRe captures `bp = Blueprint("name", url_prefix="/v1")`. The
// url_prefix kwarg may appear before or after other kwargs; we capture the
// receiver and then scan the tail for url_prefix separately so ordering does
// not matter.
//
// Capture groups: 1 = receiver variable, 2 = constructor argument tail.
var sanicBlueprintRe = regexp.MustCompile(
	`(?m)^[ \t]*([A-Za-z_]\w*)\s*=\s*Blueprint\s*\(([^)]*)\)`,
)

// sanicURLPrefixKwargRe extracts `url_prefix="/v1"` from a Blueprint
// constructor tail.
var sanicURLPrefixKwargRe = regexp.MustCompile(`url_prefix\s*=\s*["']([^"'\n\r]*)["']`)

// sanicVerbDecoratorRe captures @<recv>.<verb>("/path") shorthand decorators.
// Sanic exposes get/post/put/patch/delete/head/options/websocket; we cover the
// standard HTTP verbs. The handler def follows on the next def/async def line,
// tolerating stacked decorators.
//
// Capture groups: 1 = receiver, 2 = verb, 3 = path, 4 = handler name.
var sanicVerbDecoratorRe = regexp.MustCompile(
	`@(\w+)\.(get|post|put|patch|delete|head|options)\s*\(\s*["']([^"'\n\r]+)["'][^)]*\)[ \t]*[\r\n]+(?:\s*@[^\n\r]*[\r\n]+)*\s*(?:async\s+)?def\s+(\w+)`,
)

// sanicRouteRe captures the generic @<recv>.route("/path", methods=[...]) form.
//
// Capture groups: 1 = receiver, 2 = path, 3 = kwargs tail, 4 = handler name.
var sanicRouteRe = regexp.MustCompile(
	`@(\w+)\.route\s*\(\s*["']([^"'\n\r]+)["']([^\n\r]*)\)[ \t]*[\r\n]+(?:\s*@[^\n\r]*[\r\n]+)*\s*(?:async\s+)?def\s+(\w+)`,
)

func synthesizeSanic(content string, emit emitDefFn) {
	// File-signal gate: require a Sanic-specific marker so this pass no-ops on
	// Flask files (which share the @<recv>.route / @<recv>.get decorator shape).
	if !strings.Contains(content, "Sanic") {
		return
	}

	// Build the same-file blueprint receiver → url_prefix map so blueprint
	// routes compose the prefix (Sanic Blueprint mirrors Flask Blueprint).
	prefixes := map[string]string{}
	for _, m := range sanicBlueprintRe.FindAllStringSubmatch(content, -1) {
		if len(m) < 3 {
			continue
		}
		recv := m[1]
		if pm := sanicURLPrefixKwargRe.FindStringSubmatch(m[2]); len(pm) >= 2 {
			prefixes[recv] = strings.TrimRight(pm[1], "/")
		} else {
			// Blueprint with no url_prefix: record an empty prefix so the
			// receiver is still recognised as a blueprint (no-op composition).
			prefixes[recv] = ""
		}
	}

	compose := func(recv, raw string) string {
		if pfx, ok := prefixes[recv]; ok && pfx != "" {
			return joinPathFragments(pfx, raw)
		}
		return raw
	}

	// Shorthand verbs first — unambiguous verb.
	for _, idx := range sanicVerbDecoratorRe.FindAllStringSubmatchIndex(content, -1) {
		if len(idx) < 10 {
			continue
		}
		recv := content[idx[2]:idx[3]]
		verb := strings.ToUpper(content[idx[4]:idx[5]])
		raw := content[idx[6]:idx[7]]
		handler := content[idx[8]:idx[9]]
		canonical := httproutes.Canonicalize(httproutes.FrameworkSanic, compose(recv, raw))
		defLine := lineOfOffset(content, idx[8])
		emit(verb, canonical, "sanic", "Controller", handler, defLine)
	}

	// Generic .route(...) form with optional methods=[...].
	for _, idx := range sanicRouteRe.FindAllStringSubmatchIndex(content, -1) {
		if len(idx) < 10 {
			continue
		}
		recv := content[idx[2]:idx[3]]
		raw := content[idx[4]:idx[5]]
		extras := content[idx[6]:idx[7]]
		handler := content[idx[8]:idx[9]]
		canonical := httproutes.Canonicalize(httproutes.FrameworkSanic, compose(recv, raw))
		defLine := lineOfOffset(content, idx[8])
		methods := parseFlaskMethods(extras) // identical methods=[...] shape
		if len(methods) == 0 {
			// Sanic's default for app.route without methods is GET.
			methods = []string{"GET"}
		}
		for _, verb := range methods {
			emit(verb, canonical, "sanic", "Controller", handler, defLine)
		}
	}
}

// ---------------------------------------------------------------------------
// Litestar (Python) — #2980
// ---------------------------------------------------------------------------
//
// Litestar (formerly Starlite) differs from FastAPI: route decorators are bare
// (`@get`, `@post`, ...) rather than receiver-bound (`@app.get`), and handlers
// are commonly grouped under a `Controller` subclass with a class-level
// `path = "/base"` attribute, then mounted under a `Router(path="/api",
// route_handlers=[...])`.
//
//	class UserController(Controller):
//	    path = "/users"
//	    @get("/{user_id:int}")
//	    async def get_user(self, user_id: int) -> ...: ...
//
//	@post("/items")
//	async def create_item(data: Item) -> ...: ...
//
// Path parameters use the FastAPI-style `{name:type}` curly-brace convention
// (FrameworkLitestar is grouped with FastAPI in Canonicalize, which strips the
// `:type` suffix).
//
// Prefix composition: a handler's full path is
// joinPathFragments(routerPath, controllerPath, decoratorPath). The same-file
// Controller `path` attribute is recovered by scanning each Controller class
// body. A same-file `Router(path=...)` prefix (dominant single-router
// convention) is prepended to every handler; multi-router files fall back to no
// router prefix, which the byPath linker normalises.

// litestarRouterPathRe captures a same-file `Router(path="/api", ...)` prefix.
var litestarRouterPathRe = regexp.MustCompile(`\bRouter\s*\(\s*path\s*=\s*["']([^"'\n\r]+)["']`)

// litestarControllerClassRe matches a `class X(Controller):` declaration.
//
// Capture groups: 1 = class name.
var litestarControllerClassRe = regexp.MustCompile(
	`(?m)^class\s+([A-Za-z_]\w*)\s*\([^)]*\bController\b[^)]*\)\s*:`,
)

// litestarControllerPathRe captures a class-level `path = "/base"` attribute.
var litestarControllerPathRe = regexp.MustCompile(`(?m)^[ \t]+path\s*=\s*["']([^"'\n\r]+)["']`)

// litestarVerbDecoratorRe captures a bare `@get("/path")` / `@post(...)` route
// handler decorator and the following handler def. The decorator may be
// followed by stacked decorators (e.g. `@get(...)` plus a guard decorator).
//
// Capture groups: 1 = verb, 2 = path, 3 = handler name.
var litestarVerbDecoratorRe = regexp.MustCompile(
	`(?m)^[ \t]*@(get|post|put|patch|delete|head|options)\s*\(\s*["']([^"'\n\r]*)["'][^)]*\)[ \t]*[\r\n]+(?:\s*@[^\n\r]*[\r\n]+)*\s*(?:async\s+)?def\s+(\w+)`,
)

// litestarBareVerbDecoratorRe captures the no-argument decorator form
// `@get()` / `@post()` (Litestar allows omitting the path, defaulting to the
// Controller / Router base path). Captured separately so the path-required
// regex above stays strict.
//
// Capture groups: 1 = verb, 2 = handler name.
var litestarBareVerbDecoratorRe = regexp.MustCompile(
	`(?m)^[ \t]*@(get|post|put|patch|delete|head|options)\s*\(\s*\)[ \t]*[\r\n]+(?:\s*@[^\n\r]*[\r\n]+)*\s*(?:async\s+)?def\s+(\w+)`,
)

func synthesizeLitestar(content string, emit emitDefFn) {
	// File-signal gate: require a Litestar marker. The bare `@get(...)`
	// decorator shape is generic enough that we must avoid firing on unrelated
	// Python files (e.g. a project-local `get` decorator).
	if !strings.Contains(content, "litestar") && !strings.Contains(content, "Litestar") &&
		!strings.Contains(content, "Controller") {
		return
	}

	// Single-router prefix (dominant convention).
	routerPrefix := ""
	if rm := litestarRouterPathRe.FindStringSubmatch(content); len(rm) >= 2 {
		routerPrefix = strings.TrimRight(rm[1], "/")
	}

	// Map each handler-def offset to the enclosing Controller's path prefix by
	// recording each Controller class body span + its `path` attribute.
	type ctrlSpan struct {
		start, end int
		prefix     string
	}
	var ctrls []ctrlSpan
	for _, cm := range litestarControllerClassRe.FindAllStringSubmatchIndex(content, -1) {
		if len(cm) < 4 {
			continue
		}
		bodyStart := cm[1]
		bodyEnd := findPyClassBodyEnd(content, bodyStart)
		prefix := ""
		if pm := litestarControllerPathRe.FindStringSubmatch(content[bodyStart:bodyEnd]); len(pm) >= 2 {
			prefix = strings.TrimRight(pm[1], "/")
		}
		ctrls = append(ctrls, ctrlSpan{start: bodyStart, end: bodyEnd, prefix: prefix})
	}
	controllerPrefixAt := func(off int) string {
		for _, c := range ctrls {
			if off >= c.start && off < c.end {
				return c.prefix
			}
		}
		return ""
	}

	compose := func(handlerOff int, raw string) string {
		full := joinPathFragments(controllerPrefixAt(handlerOff), raw)
		if routerPrefix != "" {
			full = joinPathFragments(routerPrefix, full)
		}
		return full
	}

	emitOne := func(verb, raw, handler string, defOff int) {
		canonical := httproutes.Canonicalize(httproutes.FrameworkLitestar, compose(defOff, raw))
		defLine := lineOfOffset(content, defOff)
		emit(strings.ToUpper(verb), canonical, "litestar", "SCOPE.Operation", handler, defLine)
	}

	// Path-bearing decorators.
	for _, idx := range litestarVerbDecoratorRe.FindAllStringSubmatchIndex(content, -1) {
		if len(idx) < 8 {
			continue
		}
		verb := content[idx[2]:idx[3]]
		raw := content[idx[4]:idx[5]]
		handler := content[idx[6]:idx[7]]
		emitOne(verb, raw, handler, idx[6])
	}
	// Bare decorators (path defaults to the Controller / Router base path).
	for _, idx := range litestarBareVerbDecoratorRe.FindAllStringSubmatchIndex(content, -1) {
		if len(idx) < 6 {
			continue
		}
		verb := content[idx[2]:idx[3]]
		handler := content[idx[4]:idx[5]]
		emitOne(verb, "", handler, idx[4])
	}
}

// ---------------------------------------------------------------------------
// Robyn (Python) — #2980
// ---------------------------------------------------------------------------
//
// Robyn is a Rust-backed Python web framework with a FastAPI-like decorator
// surface bound to a `Robyn(__file__)` app instance:
//
//	app = Robyn(__file__)
//	@app.get("/users/:id")
//	async def handler(request): ...
//
// Path parameters use the Express-style `:name` colon convention
// (FrameworkRobyn is grouped with Express et al. in Canonicalize). Robyn has no
// blueprint/router prefix concept in its core API, so no prefix composition is
// needed.

// robynVerbDecoratorRe captures @<recv>.<verb>("/path") decorators bound to a
// Robyn app instance. The handler def follows on the next def/async def line.
//
// Capture groups: 1 = receiver, 2 = verb, 3 = path, 4 = handler name.
var robynVerbDecoratorRe = regexp.MustCompile(
	`@(\w+)\.(get|post|put|patch|delete|head|options)\s*\(\s*["']([^"'\n\r]+)["'][^)]*\)[ \t]*[\r\n]+(?:\s*@[^\n\r]*[\r\n]+)*\s*(?:async\s+)?def\s+(\w+)`,
)

func synthesizeRobyn(content string, emit emitDefFn) {
	// File-signal gate: require the Robyn marker. The decorator shape overlaps
	// with FastAPI / Sanic, so without this gate we would double-claim those
	// frameworks' endpoints (the side-scoped dedup tolerates it, but the
	// framework label would be wrong).
	if !strings.Contains(content, "Robyn") {
		return
	}
	for _, idx := range robynVerbDecoratorRe.FindAllStringSubmatchIndex(content, -1) {
		if len(idx) < 10 {
			continue
		}
		verb := strings.ToUpper(content[idx[4]:idx[5]])
		raw := content[idx[6]:idx[7]]
		handler := content[idx[8]:idx[9]]
		canonical := httproutes.Canonicalize(httproutes.FrameworkRobyn, raw)
		defLine := lineOfOffset(content, idx[8])
		emit(verb, canonical, "robyn", "Controller", handler, defLine)
	}
}

// ---------------------------------------------------------------------------
// aiohttp (Python) — #2979
// ---------------------------------------------------------------------------
//
// aiohttp is a dual-use library: it ships BOTH an async HTTP server and an
// async HTTP client (`ClientSession`). Server-side routing has two idioms:
//
//	app = web.Application()
//	app.router.add_get("/users/{user_id}", handler)
//	app.router.add_route("GET", "/items", handler)
//
//	routes = web.RouteTableDef()
//	@routes.get("/users/{user_id}")
//	async def get_user(request): ...
//	app.add_routes(routes)
//
// Path parameters use the FastAPI-style `{name}` / `{name:regex}` curly-brace
// convention (FrameworkAiohttp is grouped with FastAPI in Canonicalize, which
// strips the `:regex` suffix).
//
// Dual-use gate: a file that only uses `ClientSession` (HTTP client) must NOT
// synthesize endpoints. We require an explicit server-routing signal
// (`app.router.add_` or `RouteTableDef` / `@routes.`) before emitting, so
// client-only modules no-op.

// aiohttpAddVerbRe captures `<recv>.router.add_get("/path", handler)` and the
// other verb-specific add_* methods. The handler is the second positional arg
// (a bare function reference); we capture it for handler attribution.
//
// Capture groups: 1 = verb, 2 = path, 3 = handler reference.
var aiohttpAddVerbRe = regexp.MustCompile(
	`\.router\.add_(get|post|put|patch|delete|head|options)\s*\(\s*["']([^"'\n\r]+)["']\s*,\s*([A-Za-z_][\w.]*)`,
)

// aiohttpAddRouteRe captures the generic `<recv>.router.add_route("GET",
// "/path", handler)` form where the verb is the first string argument.
//
// Capture groups: 1 = verb, 2 = path, 3 = handler reference.
var aiohttpAddRouteRe = regexp.MustCompile(
	`\.router\.add_route\s*\(\s*["'](\w+)["']\s*,\s*["']([^"'\n\r]+)["']\s*,\s*([A-Za-z_][\w.]*)`,
)

// aiohttpRoutesDecoratorRe captures `@routes.get("/path")` RouteTableDef
// decorators and the following handler def. The receiver name is captured so
// it can be matched against a RouteTableDef() assignment for the gate; in
// practice `@routes.` is the dominant idiom and the RouteTableDef presence is
// asserted by the file-level gate.
//
// Capture groups: 1 = receiver, 2 = verb, 3 = path, 4 = handler name.
var aiohttpRoutesDecoratorRe = regexp.MustCompile(
	`@(\w+)\.(get|post|put|patch|delete|head|options)\s*\(\s*["']([^"'\n\r]+)["'][^)]*\)[ \t]*[\r\n]+(?:\s*@[^\n\r]*[\r\n]+)*\s*(?:async\s+)?def\s+(\w+)`,
)

func synthesizeAiohttp(content string, emit emitDefFn) {
	// Server-routing gate. A file that imports aiohttp purely as a client
	// (`ClientSession`) carries none of these markers, so it no-ops — this is
	// the dual-use skip the aiohttp.yaml rule pack calls out.
	hasAddRouter := strings.Contains(content, ".router.add_")
	hasRouteTable := strings.Contains(content, "RouteTableDef") || strings.Contains(content, "@routes.")
	if !hasAddRouter && !hasRouteTable {
		return
	}

	// `app.router.add_get("/path", handler)` shorthand verbs.
	for _, idx := range aiohttpAddVerbRe.FindAllStringSubmatchIndex(content, -1) {
		if len(idx) < 8 {
			continue
		}
		verb := strings.ToUpper(content[idx[2]:idx[3]])
		raw := content[idx[4]:idx[5]]
		handler := content[idx[6]:idx[7]]
		canonical := httproutes.Canonicalize(httproutes.FrameworkAiohttp, raw)
		defLine := lineOfOffset(content, idx[6])
		emit(verb, canonical, "aiohttp", "Controller", handler, defLine)
	}

	// `app.router.add_route("GET", "/path", handler)` generic form.
	for _, idx := range aiohttpAddRouteRe.FindAllStringSubmatchIndex(content, -1) {
		if len(idx) < 8 {
			continue
		}
		verb := strings.ToUpper(content[idx[2]:idx[3]])
		raw := content[idx[4]:idx[5]]
		handler := content[idx[6]:idx[7]]
		canonical := httproutes.Canonicalize(httproutes.FrameworkAiohttp, raw)
		defLine := lineOfOffset(content, idx[6])
		emit(verb, canonical, "aiohttp", "Controller", handler, defLine)
	}

	// `@routes.get("/path")` RouteTableDef decorators.
	for _, idx := range aiohttpRoutesDecoratorRe.FindAllStringSubmatchIndex(content, -1) {
		if len(idx) < 10 {
			continue
		}
		verb := strings.ToUpper(content[idx[4]:idx[5]])
		raw := content[idx[6]:idx[7]]
		handler := content[idx[8]:idx[9]]
		canonical := httproutes.Canonicalize(httproutes.FrameworkAiohttp, raw)
		defLine := lineOfOffset(content, idx[8])
		emit(verb, canonical, "aiohttp", "Controller", handler, defLine)
	}
}

// ---------------------------------------------------------------------------
// Bottle (Python) — #2979
// ---------------------------------------------------------------------------
//
// Bottle is a single-file WSGI micro-framework. Routes are declared with the
// module-level decorator functions (or app-bound equivalents):
//
//	@route("/users/<id>")            # default GET
//	@route("/items", method="POST")  # explicit verb
//	@route("/x", method=["GET","POST"])
//	@get("/users/<id:int>")
//	@post("/items")
//	def handler(): ...
//
// Path parameters use the Flask-style `<name>` / `<name:filter>` angle-bracket
// convention (FrameworkBottle is grouped with Flask in Canonicalize).
//
// The verb decorators may be bare (`@get(...)`) or app-bound
// (`@app.get(...)`); both are handled. Method composition for the generic
// `@route(..., method=...)` form mirrors Flask's `methods=[...]` parsing.

// bottleVerbDecoratorRe captures `@get("/path")` / `@app.post("/path")`
// shorthand verb decorators and the following handler def. The receiver
// portion (`app.`) is optional.
//
// Capture groups: 1 = verb, 2 = path, 3 = handler name.
var bottleVerbDecoratorRe = regexp.MustCompile(
	`(?m)^[ \t]*@(?:\w+\.)?(get|post|put|patch|delete|head|options)\s*\(\s*["']([^"'\n\r]+)["'][^)]*\)[ \t]*[\r\n]+(?:\s*@[^\n\r]*[\r\n]+)*\s*(?:async\s+)?def\s+(\w+)`,
)

// bottleRouteRe captures the generic `@route("/path", method="POST")` /
// `@app.route("/path", method=["GET","POST"])` form. The receiver is optional.
//
// Capture groups: 1 = path, 2 = kwargs tail, 3 = handler name.
var bottleRouteRe = regexp.MustCompile(
	`(?m)^[ \t]*@(?:\w+\.)?route\s*\(\s*["']([^"'\n\r]+)["']([^\n\r]*)\)[ \t]*[\r\n]+(?:\s*@[^\n\r]*[\r\n]+)*\s*(?:async\s+)?def\s+(\w+)`,
)

// bottleMethodKwargRe extracts the verb(s) from a `method="POST"` or
// `method=["GET", "POST"]` kwarg on a @route decorator (Bottle uses the
// singular `method=`, unlike Flask's `methods=`).
var bottleMethodKwargRe = regexp.MustCompile(`method\s*=\s*(\[[^\]]*\]|["'][^"'\n\r]*["'])`)

// parseBottleMethods returns the verbs declared in a Bottle `@route` decorator
// `method=` kwarg (string or list form). Empty result means the default (GET).
func parseBottleMethods(args string) []string {
	mm := bottleMethodKwargRe.FindStringSubmatch(args)
	if len(mm) < 2 {
		return nil
	}
	body := strings.Trim(mm[1], "[]")
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

func synthesizeBottle(content string, emit emitDefFn) {
	// File-signal gate: require a Bottle marker. The bare `@get(...)` /
	// `@route(...)` decorator shapes are generic, so without this gate the
	// synthesizer could fire on unrelated Python files. `bottle.py` in repo
	// root is the definitive single-file signal; `from bottle import` /
	// `import bottle` / `Bottle(` cover the imported-app idioms.
	if !strings.Contains(content, "bottle") && !strings.Contains(content, "Bottle") {
		return
	}

	// Shorthand verb decorators first — unambiguous verb.
	for _, idx := range bottleVerbDecoratorRe.FindAllStringSubmatchIndex(content, -1) {
		if len(idx) < 8 {
			continue
		}
		verb := strings.ToUpper(content[idx[2]:idx[3]])
		raw := content[idx[4]:idx[5]]
		handler := content[idx[6]:idx[7]]
		canonical := httproutes.Canonicalize(httproutes.FrameworkBottle, raw)
		defLine := lineOfOffset(content, idx[6])
		emit(verb, canonical, "bottle", "Controller", handler, defLine)
	}

	// Generic @route(..., method=...) form.
	for _, idx := range bottleRouteRe.FindAllStringSubmatchIndex(content, -1) {
		if len(idx) < 8 {
			continue
		}
		raw := content[idx[2]:idx[3]]
		extras := content[idx[4]:idx[5]]
		handler := content[idx[6]:idx[7]]
		canonical := httproutes.Canonicalize(httproutes.FrameworkBottle, raw)
		defLine := lineOfOffset(content, idx[6])
		methods := parseBottleMethods(extras)
		if len(methods) == 0 {
			// Bottle's default for @route without method= is GET.
			methods = []string{"GET"}
		}
		for _, verb := range methods {
			emit(verb, canonical, "bottle", "Controller", handler, defLine)
		}
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

// expressMountRe captures an Express sub-router mount of the form
// `<recv>.use('/prefix', <subRouter>)`, mirroring the express.yaml ROUTES_TO
// rule (~:104). The mount prefix (group 2) is composed onto every route the
// mounted sub-router (group 3) registers, so a route file's producer path
// carries the full canonical mount path (#2934). Group 1 = mounting receiver
// (unused — the prefix attaches to the sub-router var, which is what later
// routes register against).
//
// Only string-literal first-argument mounts are captured; bare
// `app.use(middleware)` (no path) is correctly ignored.
var expressMountRe = regexp.MustCompile(
	`([$\w][\w$]*)\.use\s*\(\s*['"` + "`" + `](/[^'"` + "`" + `\n\r]*)['"` + "`" + `]\s*,\s*([$\w][\w$]*)\s*[\),]`,
)

// buildExpressMountPrefixes returns a map of sub-router variable name → fully
// composed mount prefix for every `<recv>.use('/prefix', subRouter)` mount in
// the file, resolving NESTED mounts transitively (#2934). For
//
//	app.use('/api', v1)
//	v1.use('/admin', adminRouter)
//
// the result is {v1: "/api", adminRouter: "/api/admin"}. Routes registered on
// adminRouter then compose to `/api/admin/...`.
//
// The resolution is order-independent: we first collect every (parent, prefix,
// child) mount edge, then walk the chains to a fixed point. Self-mounts and
// cycles (pathological) are bounded by the edge count so the loop always
// terminates. A child with no resolvable parent prefix simply maps to its own
// literal prefix.
func buildExpressMountPrefixes(content string) map[string]string {
	type mount struct {
		parent string
		prefix string
		child  string
	}
	var mounts []mount
	for _, m := range expressMountRe.FindAllStringSubmatch(content, -1) {
		if len(m) < 4 {
			continue
		}
		mounts = append(mounts, mount{parent: m[1], prefix: m[2], child: m[3]})
	}
	if len(mounts) == 0 {
		return nil
	}
	// Seed each child with its own literal mount prefix.
	prefixes := make(map[string]string, len(mounts))
	for _, mt := range mounts {
		prefixes[mt.child] = normalizeMountPrefix(mt.prefix)
	}
	// Iterate to a fixed point, prepending each parent's resolved prefix.
	// Bounded by len(mounts) passes (longest chain length).
	for i := 0; i < len(mounts); i++ {
		changed := false
		for _, mt := range mounts {
			if mt.parent == mt.child {
				continue // self-mount guard
			}
			parentPrefix, ok := prefixes[mt.parent]
			if !ok {
				continue
			}
			composed := joinPathFragments(parentPrefix, normalizeMountPrefix(mt.prefix))
			if prefixes[mt.child] != composed {
				prefixes[mt.child] = composed
				changed = true
			}
		}
		if !changed {
			break
		}
	}
	return prefixes
}

// composeExpressMount prepends the resolved mount prefix for `receiver` (if
// any) onto the raw route path (#2934). When the receiver var was never
// mounted at a string-literal prefix the raw path is returned unchanged,
// preserving the pre-#2934 producer path for the common single-file /
// top-level-app case. The composed path is fed to Canonicalize so params and
// slashes normalize uniformly.
func composeExpressMount(mountPrefixes map[string]string, receiver, raw string) string {
	if len(mountPrefixes) == 0 {
		return raw
	}
	prefix, ok := mountPrefixes[receiver]
	if !ok || prefix == "" {
		return raw
	}
	return joinPathFragments(prefix, raw)
}

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
	// #2934 — sub-router mount prefixes. `app.use('/api', router)` mounts
	// `router` at `/api`, so routes registered on `router` must compose to
	// `/api/<path>`. Resolves nested mounts transitively. nil when the file
	// has no string-literal mounts (the common single-file case), making the
	// composition lookup a no-op that preserves the pre-#2934 bare path.
	mountPrefixes := buildExpressMountPrefixes(content)

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
		canonical := httproutes.Canonicalize(httproutes.FrameworkExpress, composeExpressMount(mountPrefixes, receiver, raw))
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
		canonical := httproutes.Canonicalize(httproutes.FrameworkExpress, composeExpressMount(mountPrefixes, receiver, raw))
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

// emitDefFn extends emitFn with a `defLine` argument carrying the 1-based
// line of the handler's `def` statement. Used by per-framework synthesisers
// (Flask, FastAPI) where the handler def lives in the same file as the
// routing decorator and the line is recoverable from the match offset.
// A defLine of 0 means "unknown" and leaves StartLine untouched.
type emitDefFn func(method, canonicalPath, framework, handlerKind, handlerName string, defLine int)

// lineOfOffset returns the 1-based line number containing byte offset `off`
// in `content`. Newlines are counted up to (but not including) the offset,
// so the very first line is line 1.
func lineOfOffset(content string, off int) int {
	if off < 0 {
		return 0
	}
	if off > len(content) {
		off = len(content)
	}
	return 1 + strings.Count(content[:off], "\n")
}

// joinPathFragments concatenates a class-level prefix with a method-level
// path, mirroring the slash convention used by joinRoutePaths in
// spring_routes.go. An empty prefix or method passes the other through
// verbatim.
// normalizeMountPrefix cleans a raw mount/group/plugin prefix into a leading-
// slash, no-trailing-slash form suitable for joinPathFragments composition
// (#2934). `"/api/"` → `"/api"`, `"api"` → `"/api"`, `"/"` and `""` → `""`
// (root mount contributes nothing). The trailing-slash trim keeps the join
// idempotent; the empty result lets a root-level `app.use('/', router)` mount
// pass child paths through unchanged.
func normalizeMountPrefix(raw string) string {
	p := strings.TrimSpace(raw)
	if p == "" || p == "/" {
		return ""
	}
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	for len(p) > 1 && strings.HasSuffix(p, "/") {
		p = strings.TrimSuffix(p, "/")
	}
	return p
}

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
