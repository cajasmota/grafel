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

	"github.com/cajasmota/grafel/internal/engine/httproutes"
	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/frameworks/routes"
	"github.com/cajasmota/grafel/internal/types"
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
	// #3554: Scala sttp consumer-side HTTP client extraction. The Scala
	// producer side (tapir/http4s/akka) is handled by custom_scala_*
	// extractors, but outbound sttp calls have no YAML rules, so allow Scala
	// through for the client synthesizer; files without sttp markers are no-ops.
	case "scala":
		return true
	// #3574: mobile consumer-side HTTP client extraction (epic #3571). Dart
	// (Dio / package:http) and Swift (URLSession / Alamofire) have no compiled
	// YAML rules for their outbound calls; allow them through so the mobile
	// client synthesizers run. Files without client markers are no-ops.
	case "dart", "swift":
		return true
	// #4749: Crystal web producer-side route synthesis — Kemal / Amber / Lucky
	// `get "/path"` verb macros have no compiled YAML rules, so allow Crystal
	// through for synthesizeKemalRoutes. Files without a Crystal web marker +
	// verb-macro route are no-ops inside the synthesizer.
	case "crystal":
		return true
	// #4749: F# web producer-side route synthesis — Giraffe
	// `GET >=> route "/path"` / `routef` combinator chains and Saturn
	// `router { get "/path" handler }` blocks have no compiled YAML rules, so
	// allow F# through for synthesizeGiraffeRoutes. Files without an F# web
	// marker + route token are no-ops inside the synthesizer.
	case "fsharp":
		return true
	// #4749: Groovy Grails (convention `/controller/action` + UrlMappings.groovy)
	// and Ratpack handler-DSL producer-side route synthesis — no compiled YAML
	// rules emit http_endpoint_definition for Groovy (the Grails rule set is
	// detection-only; spring_routes.go is gated lang=="java"), so allow Groovy
	// through for synthesizeGroovyRoutes. Files without a Grails/Ratpack marker
	// are no-ops inside the synthesizer.
	case "groovy":
		return true
	// #4749 (epic #4615 tail): Nim Jester / Prologue producer-side route
	// synthesis — Jester `routes:`-block verb entries and Prologue
	// `app.get("/path", h)` registrations have no compiled YAML rules, so allow
	// Nim through for synthesizeJester / synthesizePrologue. Files without a Nim
	// web marker + route are no-ops inside the synthesizers.
	case "nim":
		return true
	// #3484: Lua Lapis / OpenResty producer-side route synthesis.
	case "lua":
		return true
	// #5373 (bootstrap epic #5360): Haskell Scotty / Yesod / Servant producer-
	// side route synthesis — Scotty `get "/path"` verb calls, Yesod
	// `[parseRoutes| ... |]` quasiquotes and Servant `:>`-chain type-level APIs
	// have no compiled YAML rules, so allow Haskell through for
	// synthesizeScottyRoutes / synthesizeYesodRoutes / synthesizeServantRoutes.
	// Files without a Haskell web marker are no-ops inside the synthesizers.
	case "haskell":
		return true
	// #5374 (bootstrap epic #5360): OCaml Dream producer-side route synthesis —
	// `Dream.get "/path" handler` verb registrations have no compiled YAML
	// rules, so allow OCaml through for synthesizeDreamRoutes. Files without a
	// `Dream.` web marker are no-ops inside the synthesizer.
	case "ocaml":
		return true
	// #4749 (epic #4615 tail): Erlang Cowboy producer-side route synthesis —
	// `cowboy_router:compile([{'_', [{"/path", handler, []}]}])` dispatch tables
	// have no compiled YAML rules, so allow Erlang through for synthesizeCowboy.
	// Files without a `cowboy_router` marker are no-ops inside the synthesizer.
	case "erlang":
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

// synthesisHandlerStructuralRef returns the merge-stable, file-scoped structural
// reference to the handler METHOD that a producer-side route synthesizer should
// use as the FromID of a synthesis-time endpoint→handler IMPLEMENTS bridge
// (#4319). It returns "" when no same-file handler method is named — in which
// case no bridge is emitted and the existing name-resolution / co-location
// fallbacks in ResolveHTTPEndpointHandlers take over.
//
// The returned stub has the canonical Format-A shape
//
//	scope:operation:method:<lang>:<file>:<methodName>
//
// which the central resolver (resolve.References) binds to the same-file handler
// Operation by (file, name) — independent of line numbers and stable across
// entity merge. Because it is file-scoped it can never bind a method in another
// controller, and an unresolved name is left unmatched (no guess).
//
// It fires only for refKinds that name a same-file handler METHOD
// (Controller / View / SCOPE.Operation as emitted by the annotation- and
// router-DSL synthesizers). It deliberately rejects:
//   - empty refName (GraphQL-resolver / inline-arrow synthetics with no symbol),
//   - the Spring `Route:<path>` placeholder (refKind="Route"),
//   - dotted qualified names (`Class.method`) — the synthesis-time bridge is for
//     the bare-method, same-file case; qualified cross-file shapes are handled by
//     the resolve pass. (A dotted name would also not match byLocation[file][name].)
//
// inlineHandlerRefKind is the sentinel refKind a producer synthesizer passes to
// emit() when it has detected an HTTP route whose handler is an ANONYMOUS /
// INLINE function (arrow or function-expression) rather than a named handler
// symbol. refName is empty in this case. makeEmit recognises it and synthesizes
// a stable inline-handler entity + endpoint→handler bridge (#4324) so the
// endpoint is never left a handler-less graph island.
const inlineHandlerRefKind = "InlineHandler"

// inlineHandlerName returns the deterministic, merge-stable Name for the
// synthetic Operation entity that stands in for an inline HTTP route handler
// (#4324). It is derived purely from the HTTP verb and the canonical route
// path — never a line number or a captured parameter — so the same route always
// yields the same handler node and the structural-ref that bridges the endpoint
// to it (BuildOperationStructuralRef over this Name) is stable across
// entity-merge. Shape: `<inline GET /health>`.
func inlineHandlerName(verb, canonicalPath string) string {
	return "<inline " + verb + " " + canonicalPath + ">"
}

func synthesisHandlerStructuralRef(lang, file, refKind, refName string) string {
	if refName == "" || file == "" || lang == "" {
		return ""
	}
	switch refKind {
	case "Controller", "View", "SCOPE.Operation":
		// same-file handler-method kinds
	default:
		return ""
	}
	// Reject dotted qualified names — the same-file byLocation index keys on the
	// bare method Name, and a dotted ref is the cross-file shape.
	if strings.ContainsAny(refName, ".:#") {
		return ""
	}
	return extractor.BuildOperationStructuralRef(lang, file, refName)
}

// dropTrailingSynthesisTimeBridge removes the most-recently-appended
// synthesis-time endpoint→handler bridge edge (#4319) for canonicalPath, if it
// is the last element of rels. Used by the CROSS-FILE emitters (emitFile /
// emitResource) to retract the same-file structural bridge that makeEmit emitted
// before they learned the handler actually lives in another file. Matching on
// the trailing element + pattern_type + path keeps it surgical: it only ever
// removes the bridge this same emit() call just added.
func dropTrailingSynthesisTimeBridge(rels []types.RelationshipRecord, canonicalPath string) []types.RelationshipRecord {
	if n := len(rels); n > 0 {
		last := rels[n-1]
		if last.Kind == implementsEdgeKind &&
			last.Properties["pattern_type"] == "http_endpoint_synthesis_time_bridge" &&
			last.Properties["path"] == canonicalPath {
			return rels[:n-1]
		}
	}
	return rels
}

// isTestSourceFile reports whether filePath is a test/spec/fixture file that
// should be excluded from production endpoint synthesis.
//
// Test files often contain framework setup code (NestJS @Controller inside an
// e2e spec, Express app.get() inside a Supertest fixture, Django views in a
// conftest, etc.) that looks identical to a production route declaration. We
// must NOT emit http_endpoint_definition entities for routes that only exist
// to support the test harness — they would appear in the production endpoint
// catalogue and generate spurious cross-repo links.
//
// The patterns below mirror the per-language conventions tracked by
// internal/graph/coverage.go's isTestFile and the testmap extractor's
// frameworkEntry filename/path hints. They are intentionally conservative:
// a file that does NOT match these patterns is treated as production code even
// if it imports a test library (import-based detection is deferred to the
// testmap extractor which emits SCOPE.Pattern/test_coverage, not endpoints).
func isTestSourceFile(filePath string) bool {
	// Normalise to forward slashes for cross-platform consistency.
	slashed := "/" + filepath.ToSlash(strings.ToLower(filePath))

	// Directory-segment fast-path: canonical test directories.
	for _, seg := range []string{"/__tests__/", "/test/", "/tests/", "/spec/", "/e2e/", "/fixtures/"} {
		if strings.Contains(slashed, seg) {
			return true
		}
	}

	base := filepath.Base(filePath)
	lower := strings.ToLower(base)
	ext := filepath.Ext(lower)
	stem := strings.TrimSuffix(lower, ext)

	switch ext {
	case ".go":
		// Go: foo_test.go
		return strings.HasSuffix(stem, "_test")
	case ".py":
		// Python: test_foo.py  or  foo_test.py
		return strings.HasPrefix(stem, "test_") || strings.HasSuffix(stem, "_test")
	case ".ts", ".tsx", ".js", ".jsx", ".mjs", ".cjs":
		// JS/TS: foo.test.ts, foo.spec.ts, foo.e2e-spec.ts, foo.e2e.spec.ts, …
		//
		// Patterns covered:
		//   foo.spec.ts      → stem="foo.spec"      HasSuffix ".spec"
		//   foo.test.ts      → stem="foo.test"      HasSuffix ".test"
		//   foo.e2e-spec.ts  → stem="foo.e2e-spec"  HasSuffix "-spec"
		//   foo.e2e-test.ts  → stem="foo.e2e-test"  HasSuffix "-test"
		//   foo.e2e.spec.ts  → lower contains ".spec."
		//   foo.e2e.test.ts  → lower contains ".test."
		return strings.Contains(lower, ".test.") ||
			strings.Contains(lower, ".spec.") ||
			strings.HasSuffix(stem, ".test") ||
			strings.HasSuffix(stem, ".spec") ||
			strings.HasSuffix(stem, "-spec") ||
			strings.HasSuffix(stem, "-test")
	case ".rb":
		// Ruby: foo_spec.rb
		return strings.HasSuffix(stem, "_spec")
	case ".java", ".kt", ".cs", ".scala", ".swift":
		// Java/Kotlin/C#/Scala/Swift: FooTest.java, FooTests.java, FooIT.java, FooSpec.java
		return strings.HasSuffix(stem, "test") ||
			strings.HasSuffix(stem, "tests") ||
			strings.HasSuffix(stem, "it") ||
			strings.HasSuffix(stem, "spec")
	case ".php":
		// PHP: FooTest.php
		return strings.HasSuffix(stem, "test")
	case ".dart":
		// Dart: foo_test.dart (the package:test / flutter_test convention).
		return strings.HasSuffix(stem, "_test")
	case ".rs":
		// Rust tests live in modules within production files; file-level
		// exclusion is not meaningful. Return false and rely on the testmap
		// extractor for Rust coverage.
		return false
	}
	return false
}

// isNonAppSourceFile reports whether filePath is an obvious build/tooling/CLI
// script that should never contribute http_endpoint_DEFINITION entities.
//
// Such files (a docs-build gate, a codegen helper, a release script) routinely
// contain route STRINGS — `"/health"`, `"/api/v1/..."` — as data, not as
// declared routes. The regex synthesizers can't tell a string literal in a
// build script from a real route, so without this gate a `/health` literal in
// `scripts/docs-check.mjs` is emitted as a phantom http_endpoint_definition;
// because it shares the canonical synthetic ID with the REAL HealthController
// route, it collides on entity-merge and can win source attribution — citing a
// build script as the route's definition.
//
// The check is intentionally CONSERVATIVE so real application code is never
// excluded:
//   - directory segments `/scripts/`, `/tools/`, `/bin/` (canonical tooling
//     homes across ecosystems), and
//   - a standalone *.mjs / *.cjs file whose name reads like a config/check/build
//     script (and which is NOT inside a recognised app-route tree).
//
// It is gated on PATH, not extension alone: a Next.js `app/api/**/route.mjs` or
// any `.mjs` under `src/`/`app/`/`pages/`/`routes/`/`api/` is left untouched so
// legitimate framework `.mjs` routes still synthesize.
func isNonAppSourceFile(filePath string) bool {
	slashed := "/" + filepath.ToSlash(strings.ToLower(filePath))

	// Canonical tooling/build directories.
	for _, seg := range []string{"/scripts/", "/tools/", "/bin/"} {
		if strings.Contains(slashed, seg) {
			return true
		}
	}

	ext := strings.ToLower(filepath.Ext(filePath))
	if ext == ".mjs" || ext == ".cjs" {
		// Inside a recognised app-route tree → treat as app code (don't exclude).
		for _, seg := range []string{"/src/", "/app/", "/pages/", "/routes/", "/api/", "/server/", "/lib/", "/handlers/"} {
			if strings.Contains(slashed, seg) {
				return false
			}
		}
		// Otherwise a standalone top-level *.mjs/*.cjs that looks like a
		// config/check/build/tooling script.
		base := strings.ToLower(filepath.Base(filePath))
		stem := strings.TrimSuffix(base, ext)
		for _, hint := range []string{
			"check", "build", "gen", "codegen", "config", "release",
			"setup", "lint", "docs", "script", "tool", "ci", "deploy",
			"migrate", "seed",
		} {
			if strings.Contains(stem, hint) {
				return true
			}
		}
	}
	return false
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

	// Guard: test/spec/fixture files contain framework setup code that looks
	// identical to production route declarations (e.g. NestJS @Controller in an
	// *.e2e-spec.ts, Express app.get() in a Supertest fixture). Do NOT emit
	// http_endpoint_definition entities from test files — they are not real
	// production routes. See isTestSourceFile for the pattern catalogue.
	if isTestSourceFile(path) {
		return DetectorPassResult{Entities: entities, Relationships: relationships}
	}

	// Dedup-by-ID across all per-language emitters: a single endpoint can
	// be claimed by both the AST pass (composed Route) and a YAML-rule
	// regex (e.g. Spring's @GetMapping pattern). We only want one
	// synthetic per endpoint per file.
	seen := map[string]bool{}
	// lastEndpointIdx records the index, in `entities`, of the http_endpoint
	// (definition/call) entity appended by the most recent emit() call, or -1
	// when that call emitted nothing (canonical-path empty, non-app file, or a
	// dedup hit). The emitDef / emitDefSig / emitFile / emitResource wrappers use
	// it to stamp StartLine / handler_file / signature onto the ENDPOINT entity.
	//
	// #4767 — this MUST NOT be `entities[len(entities)-1]`: when the route
	// handler is inline (refKind == inlineHandlerRefKind, e.g. Sinatra block
	// routes or Python lambda handlers) emit() appends a SECOND entity — the
	// synthesized inline-handler SCOPE.Operation — AFTER the endpoint. The naive
	// tail index then stamped the inline handler and left the endpoint at line 0,
	// regressing TestIssue2691_Sinatra_EndpointAttribution and the Pyramid
	// attribution test. Tracking the endpoint's own index keeps attribution
	// correct regardless of how many trailing entities emit() appends.
	lastEndpointIdx := -1
	// makeEmit builds an emit-closure for the PRODUCER (backend handler) side.
	// #1217: entities are now emitted with httpEndpointDefinitionKind. The
	// synthetic ID retains the canonical `http:<METHOD>:<path>` form so
	// cross-repo linkers continue to pair definitions with calls by Name.
	// owning_backend is derived by walking the handler file path upward.
	makeEmit := func(patternType, refPropKey string) emitFn {
		return func(method, canonicalPath, framework, refKind, refName string) {
			// #4767 — reset before any early-return so a wrapper never stamps a
			// stale endpoint index when this emit() produced nothing.
			lastEndpointIdx = -1
			if canonicalPath == "" {
				return
			}
			// #endpoint-undercount — never emit an http_endpoint_DEFINITION from
			// an obvious build/tooling/CLI script (scripts/, tools/, bin/, or a
			// standalone *.mjs/*.cjs config-/check-script). Such files contain
			// route STRINGS (e.g. a "/health" literal in a docs-build gate) that
			// the regex passes would otherwise mistake for a real route, which
			// then collides on the synthetic ID with the real controller's
			// endpoint and wins source attribution on entity merge. The exclusion
			// is conservative (gated on PATH, not just extension) so legitimate
			// app routes — including framework `.mjs` routes (Next.js etc.) — are
			// never dropped. Consumer-side CALLS are intentionally NOT gated:
			// a tooling script may legitimately call an HTTP endpoint.
			if patternType == "http_endpoint_synthesis" && isNonAppSourceFile(path) {
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
			// #4767 — record the endpoint's index NOW, before the inline-handler
			// branch below may append a second entity. The emitX wrappers stamp
			// THIS index, never the tail.
			lastEndpointIdx = len(entities) - 1

			// #4319 LONG-TERM FIX — emit the endpoint→handler bridge AT SYNTHESIS
			// TIME, from the decorated handler node, using a merge-stable,
			// file-scoped STRUCTURAL reference to the handler method (rather than
			// reconstructing the bridge later by name- / co-location matching in
			// ResolveHTTPEndpointHandlers).
			//
			// Two prior detect-and-repair fixes (#4326 bare↔qualified name match,
			// #4330 file:line co-location) both reproduced in synthetic fixtures
			// but FAILED on the live graph: the bridge was rebuilt POST-merge from
			// the `source_handler` string or the synthetic's StartLine, and the
			// merge/dedup step destabilises both (a same-(verb,path) synthetic from
			// another pass can win attribution carrying no usable source_handler,
			// and a duplicate handler Operation at the same file:line trips the
			// no-guess guard) → the endpoint is left a graph ISLAND.
			//
			// The structural edge below is IMMUNE to that lossy round-trip: it is a
			// `scope:operation:method:<lang>:<file>:<name>` stub that the CENTRAL
			// resolver (resolve.References, in buildDocument) binds to the same-file
			// handler by (file, name) — independent of StartLine, independent of the
			// http-endpoint resolve pass, and stable across entity-merge (it travels
			// as a stub, not a hex ID, and is rewritten only once, after all merging).
			// It is file-scoped so it can NEVER mis-bridge to a method in another
			// controller, and when the name does not resolve in-file the resolver
			// simply leaves it unmatched (no guess, no phantom edge) — preserving the
			// no-mis-bridge guarantee the prior fixes established.
			//
			// GENERALISED: this fires for EVERY producer synthesizer that hands a
			// real same-file handler METHOD name (NestJS / Express / Fastify /
			// FastAPI / Flask / JAX-RS / Axum / Rocket / …) via refKind ∈
			// {Controller, View, SCOPE.Operation}. It deliberately does NOT fire for
			// the cross-file `handler_file`-hint frameworks (Rails / Phoenix /
			// Sails / Adonis / Feathers — refKind=Controller but the method lives in
			// another file, handled by handlerFileHint resolution) nor for Spring's
			// `Route:<path>` placeholder, because those carry no same-file method
			// name. The existing name-resolution + co-location paths remain as
			// fallbacks; this is the primary, merge-stable bridge.
			if patternType == "http_endpoint_synthesis" {
				if bridgeRef := synthesisHandlerStructuralRef(lang, path, refKind, refName); bridgeRef != "" {
					relationships = append(relationships, types.RelationshipRecord{
						FromID: bridgeRef,
						ToID:   kind + ":" + id,
						Kind:   implementsEdgeKind,
						Properties: map[string]string{
							"pattern_type": "http_endpoint_synthesis_time_bridge",
							"framework":    framework,
							"verb":         strings.ToUpper(method),
							"path":         canonicalPath,
						},
					})
				} else if refKind == inlineHandlerRefKind {
					// #4324 — the route handler is an ANONYMOUS / INLINE function
					// (arrow or function-expression), e.g.
					//   app.get('/health', (req, res) => res.send('ok'))
					// There is no addressable handler SYMBOL the structural-ref
					// could bind to, so the synthesizer signals refKind=
					// "InlineHandler" (refName is empty). Without this branch the
					// endpoint is emitted but left a graph ISLAND with no handler
					// linkage — invisible to flow / IMPLEMENTS traversal.
					//
					// LONG-TERM ROOT FIX (handler-shape-agnostic): synthesize a
					// stable inline-handler Operation entity and bridge to it. The
					// entity's Name — and the structural-ref that targets it — are
					// derived purely from (verb, canonicalPath), NOT from a line
					// number or a captured parameter token, so both are:
					//   * deterministic   — same route ⇒ same handler node, and
					//   * merge-stable    — survives entity-merge/dedup the same way
					//                       the #4319 decorated-handler bridge does
					//                       (it travels as a Format-A stub bound by
					//                       (file, Name) after all merging, never a
					//                       hex ID or a line-sensitive co-location).
					// File-scoped ⇒ it can never mis-bridge to another file.
					handlerName := inlineHandlerName(strings.ToUpper(method), canonicalPath)
					entities = append(entities, types.EntityRecord{
						Name:             handlerName,
						QualifiedName:    handlerName,
						Kind:             "SCOPE.Operation",
						Subtype:          "inline_handler",
						SourceFile:       path,
						Language:         lang,
						EnrichmentStatus: types.StatusPending,
						QualityScore:     0.7,
						Properties: map[string]string{
							"framework":    framework,
							"handler_kind": "inline",
							"verb":         strings.ToUpper(method),
							"route_path":   canonicalPath,
							"provenance":   "SYNTHESIZED_INLINE_HTTP_HANDLER",
						},
					})
					relationships = append(relationships, types.RelationshipRecord{
						FromID: extractor.BuildOperationStructuralRef(lang, path, handlerName),
						ToID:   kind + ":" + id,
						Kind:   implementsEdgeKind,
						Properties: map[string]string{
							"pattern_type": "http_endpoint_synthesis_time_bridge",
							"handler_kind": "inline",
							"framework":    framework,
							"verb":         strings.ToUpper(method),
							"path":         canonicalPath,
						},
					})
				}
			}
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
		emit(method, canonicalPath, framework, refKind, refName)
		// #4767 — stamp the ENDPOINT entity (lastEndpointIdx), not the tail: the
		// inline-handler path appends a trailing SCOPE.Operation entity.
		if defLine > 0 && lastEndpointIdx >= 0 {
			entities[lastEndpointIdx].StartLine = defLine
		}
	}

	// emitDefSig wraps emitDef and additionally stamps the NestJS handler
	// SIGNATURE (parameters + response/request DTO) onto the just-appended
	// http_endpoint entity (#4568/#4569). The synthesized entity — not the
	// custom extractor's SCOPE.Operation — is the one the dashboard Paths panel
	// reads `parameters`/`response_type` from, so without this the Parameters
	// table is empty and the Response row is "(none)" even though the handler
	// declares both. defLine is the 1-based handler line; sig is parsed from it.
	emitDefSig := func(method, canonicalPath, framework, refKind, refName string, defLine int, sig nestSignature) {
		emit(method, canonicalPath, framework, refKind, refName)
		// #4767 — stamp the ENDPOINT entity (lastEndpointIdx), not the tail.
		if lastEndpointIdx < 0 {
			return
		}
		last := &entities[lastEndpointIdx]
		if defLine > 0 {
			last.StartLine = defLine
		}
		if last.Properties == nil {
			last.Properties = map[string]string{}
		}
		stampNestSignature(last.Properties, sig)
	}

	// emitFile wraps emit and stamps `handler_file` (cross-file hint,
	// #2691 — Rails maps "users#index" to app/controllers/users_controller.rb)
	// plus StartLine (#2691 — Sinatra anchors the synthetic at its verb
	// block line in the same file). Either may be empty / 0.
	emitFile := func(method, canonicalPath, framework, refKind, refName, handlerFile string, defLine int) {
		emit(method, canonicalPath, framework, refKind, refName)
		// #4767 — stamp the ENDPOINT entity (lastEndpointIdx), not the tail: for
		// Sinatra inline-block routes emit() appends a trailing inline-handler
		// entity, so the tail index would mis-stamp the handler and leave the
		// endpoint at line 0.
		if lastEndpointIdx < 0 {
			return
		}
		last := &entities[lastEndpointIdx]
		if handlerFile != "" {
			if last.Properties == nil {
				last.Properties = map[string]string{}
			}
			last.Properties["handler_file"] = handlerFile
			// #4319 — this is a CROSS-FILE handler (Rails / Phoenix / Sails /
			// Adonis / Feathers): the method named by refName lives in
			// handlerFile, NOT in `path`. The synthesis-time structural bridge
			// emit() just appended is keyed on THIS file, so it would never
			// resolve here; drop it so it does not show up as an unmatched stub.
			// Cross-file attribution stays the job of the handler_file-hint
			// resolution path in ResolveHTTPEndpointHandlers.
			relationships = dropTrailingSynthesisTimeBridge(relationships, canonicalPath)
		}
		if defLine > 0 {
			last.StartLine = defLine
		}
	}

	// emitResource wraps emit for the convention-over-configuration RESOURCEFUL
	// route synthesizers (Rails `resources`, Laravel `Route::resource`/
	// `apiResource`, Spring Data REST `@RepositoryRestResource`, NestJS `@Crud()`)
	// — T10 #3842. After the canonical http_endpoint synthetic is appended, it
	// stamps the route PROVENANCE (`framework_synthesized`) + per-verb EFFECTIVE
	// CONTRACT (effective_kind / effective_action / effective_status /
	// effective_error_statuses / defining_class) from the shared
	// frameworks/routes catalog, keyed on the (framework, action) pair the
	// synthesizer already knows. This fans the DRF effective-contract shape
	// (#3835) across the convention frameworks: a consumer can now tell a
	// framework-generated route from a hand-written one and read its default
	// status (create→201, destroy→204, ...) without re-deriving it. handlerFile
	// is stamped like emitFile so the resolver can path-target the handler.
	// HONEST-PARTIAL: an action with no curated contract still gets the
	// provenance tag but no fabricated status (routes.Stamp guarantees this).
	emitResource := func(method, canonicalPath, framework, refKind, refName, handlerFile, action string) {
		emit(method, canonicalPath, framework, refKind, refName)
		// #4767 — stamp the ENDPOINT entity (lastEndpointIdx), not the tail.
		if lastEndpointIdx < 0 {
			return
		}
		last := &entities[lastEndpointIdx]
		if last.Properties == nil {
			last.Properties = map[string]string{}
		}
		if handlerFile != "" {
			last.Properties["handler_file"] = handlerFile
			// #4319 — cross-file handler: drop the same-file synthesis-time
			// bridge (see emitFile for the rationale).
			relationships = dropTrailingSynthesisTimeBridge(relationships, canonicalPath)
		}
		routes.Stamp(last.Properties, framework, action)
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

	// soapClientEmit adapts the plain 5-arg emitFn used by the SOAP / JSON-RPC
	// consumer passes (#3628) onto emitClientRuntime so they emit the same
	// http_endpoint_call entity + FETCHES edge as every other consumer pass.
	// These protocols carry no runtime-dynamic URL, so runtimeDynamic is false.
	soapClientEmit := func(method, canonicalPath, framework, refKind, refName string) {
		emitClientRuntime(method, canonicalPath, framework, refKind, refName, false)
	}

	// Phase 1 deliberately emits synthetic entities WITHOUT producer-side
	// handler→endpoint edges. The referenced entity is recorded as a
	// property (`source_handler`) so a follow-up pass can resolve it
	// against the existing entity table once the AST extractors emit
	// stable controller / function IDs. Consumer-side FETCHES edges ARE
	// emitted here — the unresolved `Function:<name>` FromID is a soft
	// reference that the graph walk tolerates gracefully.

	// #3628 (api deprecation + version stamping) — capture the entity-slice
	// length before the per-language synthesizers run so applyEndpointDeprecation
	// and applyEndpointAPIVersion stamp deprecation / api_version onto exactly the
	// http_endpoint_definition entities this file just emitted (any language).
	deprecationBefore := len(entities)

	switch lang {
	case "java":
		// Capture the producer-side entity count before the Java backend
		// synthesizers run so the middleware pass (#3628) can resolve the
		// ordered middleware_chain over exactly the endpoints this file emits.
		javaMWBefore := len(entities)
		// Spring MVC composed Routes already carry a verb on the
		// `http_method` property; reuse them rather than re-scanning the
		// file (the AST pass is the source of truth for prefix composition).
		synthesizeSpringFromComposed(entities, path, emit)
		// JAX-RS: scan the file directly.
		synthesizeJAXRS(string(content), emit)
		// Producer side (#4750): stamp the method ▸ class ▸ global Spring-Security
		// posture (@PreAuthorize/@Secured + matched SecurityFilterChain rule + the
		// handler source body, #4752) onto the Spring endpoints emitted above, so
		// the authposture spring resolver decodes class/global postures live.
		applySpringCoreAuth(string(content), path, entities, javaMWBefore)
		// Javalin (#3085): lambda DSL `app.get("/path", handler)` routing.
		synthesizeJavalin(string(content), emit)
		// Vert.x (#3086): lambda DSL `router.get("/path").handler(...)` routing.
		synthesizeVertx(string(content), emit)
		// Struts (#3089): @Action annotation + struts.xml <action> routing.
		synthesizeStruts(string(content), emit)
		// Play Framework (#3090): conf/routes DSL `GET /path controllers.Foo.bar` routing.
		synthesizePlay(string(content), path, emit)
		// Akka-HTTP (#3092): directive DSL `path("foo", () -> get(() -> ...))` routing.
		synthesizeAkkaHTTP(string(content), emit)
		// Spring WebFlux (#3080): functional DSL RouterFunctions.route().GET(...)
		// and WebFilter middleware detection.
		synthesizeSpringWebFlux(string(content), emit)
		// Consumer side (#721): HttpClient / RestTemplate /
		// WebClient / OkHttp / Apache HttpClient / Retrofit.
		synthesizeJavaClientWithRuntime(string(content), emitClientRuntime)
		// SOAP (epic #3628): JAX-WS @WebService/@WebMethod producer endpoints
		// and JAX-WS generated-port client calls, keyed http:SOAP:/soap/...
		// so they cross-link via the existing Name-based HTTP linker.
		synthesizeJavaSOAPServer(string(content), emit)
		synthesizeJavaSOAPClient(string(content), soapClientEmit)
		// #3628 — bind the ordered middleware chain to the Spring producer
		// endpoints emitted above: Servlet FilterRegistrationBean urlPatterns and
		// Spring MVC HandlerInterceptor addPathPatterns are matched against each
		// route's path and stamped as middleware_chain/middleware_count/
		// middleware_names/middleware_scope using the same cross-stack contract as
		// the Go (#3777) and JS/TS (#2853) passes. Auth filters/interceptors are
		// annotated IN the chain (auth_kind), never double-modeled.
		applyJavaMiddlewareCoverage(string(content), path, entities, javaMWBefore)
		// #3628 rate-limit child — stamp the flat rate-limit contract
		// (rate_limited/rate_limit/rate_limit_scope/rate_limit_source) on the
		// Java endpoints emitted above: Resilience4j @RateLimiter / bucket4j
		// method annotations (matched by mapping path) and Spring Cloud Gateway
		// RequestRateLimiter filters (matched by Path= predicate). Mirrors the
		// JS/TS + Python passes; config-driven limits are honest-partial.
		applyJavaRateLimit(string(content), path, entities, javaMWBefore)
	case "python":
		// Capture the producer-side entity count before the Python backend
		// synthesizers run so the middleware pass (#3628) can resolve the
		// ordered middleware_chain over exactly the http_endpoint_definition
		// entities this file just emitted.
		pyMWBefore := len(entities)
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
		// #3065 — CherryPy / Falcon / Hug / Quart routing synthesis. Each
		// synthesizer is gated on a framework-specific marker so it no-ops on
		// files that use one of the similar frameworks above.
		synthesizeCherryPy(string(content), emitDef)
		synthesizeFalcon(string(content), emitDef)
		synthesizeHug(string(content), emitDef)
		synthesizeQuart(string(content), emitDef)
		// #3066 — Strawberry GraphQL operation synthesis. Maps @strawberry.type
		// root classes (Query / Mutation / Subscription) and their methods to
		// http:GRAPHQL:/graphql/<Root>/<field> synthetics. Gated on "strawberry"
		// in the file so it is a no-op on all other Python framework files.
		synthesizeStrawberry(string(content), emitDef)
		// #3620 — Graphene + Ariadne GraphQL operation synthesis. Maps Python
		// Graphene root classes (graphene.ObjectType Query/Mutation/Subscription)
		// and Ariadne binder field decorators to the same
		// http:GRAPHQL:/graphql/<Root>/<field> shape as Strawberry. Each is gated
		// on a framework-specific file marker so it no-ops on all other files.
		synthesizeGraphene(string(content), emitDef)
		synthesizeAriadne(string(content), emitDef)
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
		// Consumer side (#3608, epic #3607): Python `gql` package GraphQL
		// operations. Maps each `gql("query/mutation/subscription { <field> }")`
		// document to the canonical http:GRAPHQL:/graphql/<Root>/<field> shape so
		// it cross-links to the GraphQL server records via the Name-based HTTP
		// linker — mirroring the JS/TS, Dart and Swift GraphQL client passes.
		// Gated on a `gql(` marker so it no-ops on every other Python file.
		synthesizePyGraphQLClient(string(content), emitClientRuntime)
		// SOAP + JSON-RPC (epic #3628): zeep `client.service.<Op>()` SOAP calls
		// and xmlrpc/jsonrpc `register_function` producers + `ServerProxy(...)`
		// client calls, keyed http:SOAP:/soap/... and http:JSONRPC:/jsonrpc/...
		// so they cross-link via the existing Name-based HTTP linker.
		synthesizePySOAPClient(string(content), soapClientEmit)
		synthesizePyJSONRPCServer(string(content), emit)
		synthesizePyJSONRPCClient(string(content), soapClientEmit)
		// #3628 — bind the ordered middleware chain to the Python producer
		// endpoints emitted above (Django settings.MIDDLEWARE global pipeline,
		// FastAPI app.add_middleware + per-route dependencies, DRF view
		// permission/authentication/throttle classes). Stamps middleware_chain/
		// middleware_count/middleware_names/middleware_scope using the same
		// cross-stack contract as the Go (#3777) and JS/TS (#2853) passes. Auth
		// middleware is annotated IN the chain (auth_kind), never double-modeled.
		applyPythonMiddlewareCoverage(string(content), path, entities, pyMWBefore)
		// #3628 rate-limit child — attribute decorator/throttle limiters to the
		// SPECIFIC synthesized endpoint and resolve the numeric rate. Brings the
		// YAML-synthesized sibling frameworks (Starlette / Sanic / Litestar /
		// Quart / aiohttp / Bottle / CherryPy / Falcon / Hug / Tornado / Pyramid)
		// and DRF view-level throttles to parity with the Flask/FastAPI custom
		// extractors, stamping the same flat contract as the JS/TS, Java and PHP
		// passes. No-op when no limiter decorator / throttle attribute is present.
		applyPythonRateLimit(string(content), path, entities, pyMWBefore)
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
		// emitDef is forwarded (alongside emit) so the synthetic is anchored at
		// the handler METHOD line (#4319): NestJS controller methods are
		// co-located with their route decorator, and stamping the method line on
		// the synthetic lets the Phase-2 resolver bridge the endpoint to its
		// handler by file:line co-location when the name-based source_handler
		// resolution misses (e.g. when entity-merge keeps a same-path synthetic
		// from another pass that carries no bindable source_handler).
		synthesizeNestJS(string(content), emit, emitDefSig)
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
		// Producer side (#3619): Pothos + TypeGraphQL code-first GraphQL
		// servers. These build the schema from TS code (a `builder` object /
		// @Resolver classes) rather than the SDL-string resolver maps that
		// synthesizeGraphQLResolvers (Apollo, #1422) recognises, so they need
		// dedicated synthesizers. Both emit the SAME canonical operation-
		// endpoint shape (http:GRAPHQL:/graphql/<Root>/<field>) as gqlgen /
		// Apollo / Strawberry so GraphQL client links (#3667) join. emitDef so
		// source_line points at the resolver method / field registration; each
		// is import-/marker-gated so it no-ops on non-Pothos / non-TypeGraphQL
		// files.
		synthesizePothos(string(content), emitDef)
		synthesizeTypeGraphQL(string(content), emitDef)
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
		// #4041 — tRPC middleware / protectedProcedure auth. The #2852 JS/TS
		// resolver below is HTTP-route/decorator-keyed and cannot see tRPC's
		// transport-agnostic middleware-in-a-builder auth. This pass re-walks
		// the routers and stamps auth_required/auth_method=trpc_middleware/
		// auth_middleware on each procedure built from an auth-enforcing
		// middleware or a protectedProcedure builder, keyed on the shared
		// dotted `path` — same append-only mechanism as the schema binding.
		// Runs BEFORE applyJSTSAuthPolicy so it owns the tRPC synthetics; the
		// JS/TS resolver leaves them at method="unknown" (no route/guard key).
		applyTRPCAuthBinding(string(content), entities, trpcTransportBefore)
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
		// #3628 rate-limit child — stamp rate_limited / rate_limit /
		// rate_limit_scope / rate_limit_source on the producer-side endpoints
		// when an express-rate-limit-style limiter is applied (app.use(limiter)
		// or a route-level limiter arg). Resolves windowMs+max → human rate
		// where statically available; honest-partial (rate omitted) for
		// config-driven limiters. Shares the jstsAuthBefore window.
		applyJSTSRateLimit(string(content), path, entities, jstsAuthBefore)
		// Consumer side (#721): fetch / axios / generic *Client
		// HTTP client calls. Now emits FETCHES edges at extraction time.
		//
		// #2709 — JS/TS extractor enumerates object-subscript template-literal
		// interpolations and tags resulting entities with a
		// `polymorphic_subscript` property. The adapter below bridges the
		// shared (verb, path, framework, refKind, refName, runtimeDynamic)
		// closure with the JS-specific extra polySubscript argument: after the
		// upstream emit appends the entity, we stamp the property in place.
		emitJSClientRuntime := func(method, canonicalPath, framework, refKind, refName string, runtimeDynamic bool, polySubscript, extractionMethod string) {
			before := len(entities)
			emitClientRuntime(method, canonicalPath, framework, refKind, refName, runtimeDynamic)
			if len(entities) == before {
				return
			}
			last := &entities[len(entities)-1]
			if polySubscript == "" && extractionMethod == "" {
				return
			}
			if last.Properties == nil {
				last.Properties = map[string]string{}
			}
			if polySubscript != "" {
				last.Properties["polymorphic_subscript"] = polySubscript
			}
			// #5527 — record per-entity extraction provenance so the
			// endpoint-stats confidence classifier can honestly mark this
			// AST-extracted client call as ast/exact rather than the regex
			// passes' blanket heuristic.
			if extractionMethod != "" {
				last.Properties["extraction_method"] = extractionMethod
			}
		}
		synthesizeFetchAxiosWithRuntime(string(content), lang, emitJSClientRuntime)
		// SOAP + JSON-RPC (epic #3628): node-soap `client.<Op>Async()` SOAP
		// calls, jayson server method maps (producer) + `client.request('m')`
		// client calls, keyed http:SOAP:/soap/... and http:JSONRPC:/jsonrpc/...
		// so they cross-link via the existing Name-based HTTP linker.
		jstsRPCFuncs := indexJSEnclosingFunctions(string(content))
		synthesizeJSSOAPClient(string(content), jstsRPCFuncs, soapClientEmit)
		synthesizeJSJSONRPCServer(string(content), emit)
		synthesizeJSJSONRPCClient(string(content), jstsRPCFuncs, soapClientEmit)
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
		// Producer side (#3613): gqlgen GraphQL server. Maps Go resolver
		// methods on the generated *queryResolver/*mutationResolver/
		// *subscriptionResolver receivers to the canonical
		// http:GRAPHQL:/graphql/<Root>/<field> operation-endpoint shape shared
		// with the JS/TS GraphQL server (synthesizeGraphQLResolvers) and the
		// Python Strawberry server — so GraphQL client links (#3667) and the
		// cross-repo linker join to them. Gated on a gqlgen file-signal so it
		// no-ops on every other Go file.
		synthesizeGqlgen(string(content), emitDef)
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
		synthesizeRailsRoutes(string(content), path, emitFile, emitResource)
		// Sinatra blocks are inline — same-file by construction. emitFile stamps
		// StartLine on the synthetic so the audit2678 attribution lands on the
		// verb block's line in app.rb.
		synthesizeSinatra(string(content), path, emitFile)
		// Producer side (#4417): Grape (`class API < Grape::API` with
		// resource/namespace prefix nesting + verb blocks) and Roda (routing-tree
		// `r.on`/`r.is` branch prefixes + leaf verbs). Both attach their handler
		// as an anonymous block — same-file by construction — so each route is
		// signalled as an inline handler (refKind=inlineHandlerRefKind) and
		// emitFile stamps StartLine on the synthetic + a same-file IMPLEMENTS
		// bridge (mirrors Sinatra #4385). Each synthesizer is gated on its own
		// class signal (`< Grape::API` / `< Roda`) so it no-ops on every other
		// Ruby file.
		synthesizeGrape(string(content), path, emitFile)
		synthesizeRoda(string(content), path, emitFile)
		// Producer side (#3621): graphql-ruby GraphQL server. Maps each
		// `field :<name>` on a root operation type class (QueryType /
		// MutationType / SubscriptionType, subclasses of *BaseObject) to the
		// canonical http:GRAPHQL:/graphql/<Root>/<field> operation-endpoint
		// shape shared with the JS/TS, Python, Go and C# GraphQL servers — so
		// GraphQL client links and the cross-repo linker join to them — with a
		// same-file HANDLES attribution to the same-name `def <name>` resolver
		// method. Gated on a graphql-ruby file-signal so it no-ops on every
		// other Ruby file.
		synthesizeGraphQLRuby(string(content), path, emitFile)
		// Consumer side (#721 wave 2b): Net::HTTP, Faraday, HTTParty, RestClient.
		synthesizeRubyClientWithRuntime(string(content), emitClientRuntime)
	case "csharp":
		// Producer side (#2692): ASP.NET Core attribute routing —
		// [HttpGet/Post/...] + class-level [Route("/api/[controller]")].
		aspnetBefore := len(entities)
		synthesizeASPNetCore(string(content), emit)
		// Producer side (#4750): stamp the method ▸ class ▸ global
		// [Authorize]/[AllowAnonymous]/Roles/Policy posture (and the action source
		// body for the source-scan fallback, #4752) onto the ASP.NET endpoints
		// emitted above, so the authposture aspnet resolver decodes them live.
		applyAspnetCoreAuth(string(content), path, entities, aspnetBefore)
		// Producer side (#3617): HotChocolate GraphQL server. Maps the three
		// root types ([QueryType]/[MutationType]/[SubscriptionType] markers,
		// [ExtendObjectType(...)] extensions, or fluent .AddQueryType<T>()
		// registrations) to http:GRAPHQL:/graphql/<Root>/<field> synthetics —
		// the SAME canonical shape the JS / Python / Go / Elixir GraphQL
		// servers emit — with a HANDLES edge to each C# resolver method.
		// Gated on a HotChocolate file-signal so it no-ops on other C# files.
		hcBefore := len(entities)
		synthesizeHotChocolate(string(content), emit)
		// Producer side (#3961): stamp [Authorize] / [Authorize(Roles=...)] /
		// [Authorize(Policy=...)] / [AllowAnonymous] from each HotChocolate
		// resolver method onto its just-emitted GRAPHQL endpoint, plus the
		// resolver's typed-arg request shape + typed-return response shape.
		// Mutates Properties in place; never adds/removes entities.
		applyHotChocolateAuthShapes(string(content), path, entities, hcBefore)
		// Producer side (#3962, epic #3872): promote the four .NET minor HTTP
		// frameworks — Carter (app.MapVerb in ICarterModule.AddRoutes),
		// FastEndpoints (Endpoint<TReq> + Verb("/path") in Configure()), NancyFX
		// (Get["/path"] / Get("/path") in : NancyModule) and ServiceStack
		// ([Route("/path","VERBS")] + : Service handlers) — from the regex-only
		// SCOPE.Operation path (internal/custom/csharp/minor_routes.go) to the
		// SAME canonical http_endpoint_definition shape synthesizeASPNetCore
		// emits, so their Routing cells reach parity and the request/response
		// shape substrate (which keys off the synthesized endpoint) can join.
		// Each is file-signal-gated so it no-ops on plain ASP.NET Core files.
		synthesizeCarter(string(content), emit)
		synthesizeFastEndpoints(string(content), emit)
		synthesizeNancy(string(content), emit)
		synthesizeServiceStack(string(content), emit)
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
		// Capture the producer-side entity count before the PHP backend
		// synthesizers run so the rate-limit pass (#4073) stamps over exactly
		// the http_endpoint_definition entities this file emits.
		phpRLBefore := len(entities)
		// Producer side (#1419): Laravel Route::verb/resource/apiResource.
		synthesizeLaravel(string(content), emit, emitResource)
		// Producer side (#4752): reconcile route + group auth middleware
		// (auth / role: / can: / withoutMiddleware) into the flat auth posture the
		// authposture laravel resolver decodes live, over the endpoints emitted above.
		applyLaravelAuth(string(content), path, entities, phpRLBefore)
		// #3628 → #4073 rate-limit child — stamp the flat rate-limit contract
		// (rate_limited/rate_limit/rate_limit_scope/rate_limit_source) on the
		// Laravel endpoints emitted above: per-route
		// `->middleware('throttle:60,1')` and group-level
		// `Route::group(['middleware'=>['throttle:30,1']])` throttle middleware.
		// `throttle:<max>,<min>` resolves the rate; a NAMED limiter
		// (`throttle:api`) is honest-partial (limit/window live in a
		// RateLimiter::for() registration). Symfony's `#[RateLimiter(...)]`
		// attribute is stamped on its own SCOPE.Operation endpoints in
		// internal/custom/php/symfony.go.
		applyLaravelRateLimit(string(content), path, entities, phpRLBefore)
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
		// Producer side (#3468): Phoenix LiveView `live "/path", Mod, :action`
		// initial-GET endpoints (handler_file hint stamped like verb routes).
		synthesizePhoenixLive(string(content), func(method, canonicalPath, framework, refKind, refName, fileHint string) {
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
		// Producer side (#3468): Plug.Router verb routes + forwards.
		synthesizePlugRouter(string(content), emit)
		// Producer side (#3468): Cowboy dispatch route tables.
		synthesizeCowboy(string(content), emit)
		// Producer side (#3468): Absinthe GraphQL query/mutation/subscription fields.
		synthesizeAbsinthe(string(content), emit)
		// Consumer side (#1483): Finch.build(:verb, url) + HTTPoison.<verb>(url).
		synthesizeElixirHTTPClients(string(content), emitClient)
		// Consumer side (#3511): Tesla (use Tesla + BaseUrl middleware + verb
		// calls) + Req (Req.get!/Req.new base_url) outbound HTTP clients.
		synthesizeElixirTeslaReq(string(content), emitClient)
	case "lua":
		// Producer side (#3484): Lapis verb/match/respond_to routes.
		synthesizeLapis(string(content), emit)
		// Producer side (#3484): OpenResty nginx `location` stanzas (in
		// lua-classified config-driver files) + lua-resty-router DSL routes.
		synthesizeOpenResty(string(content), emit)
	case "clojure":
		// Producer side (#4749, epic #4615 tail): Compojure macro routes
		// (`(GET "/users/:id" [] handler)`, `(defroutes app ...)`) and Reitit
		// data routes (`["/users/:id" {:get get-user}]`) → canonical
		// http_endpoint_definition, in the same shape axum/Vapor/Express emit, so
		// the shared resolver and the e2e route-test linker (#4351) light up for
		// Clojure. The clojure framework rule manifests stay for detection; this
		// pass adds the canonical definitions the coverage substrate keys off.
		synthesizeClojureRoutes(string(content), emit)
	case "erlang":
		// Producer side (#4749, epic #4615 tail): Erlang Cowboy dispatch route
		// tables — `cowboy_router:compile([{'_', [{"/users/:id", user_handler,
		// []}]}])`. The `{"/path", Handler, _}` triple shape is identical to the
		// Elixir Cowboy form, so the existing synthesizeCowboy synthesizer (which
		// gates on a `cowboy_router` signal and reads literal-path/handler triples)
		// emits the canonical http_endpoint_definition for Erlang too. It was
		// previously only reached for `case "elixir"` even though Cowboy is the
		// Erlang HTTP server; this wires the same producer for `.erl` dispatch
		// tables so the shared resolver + e2e route-test linker (#4351) light up.
		synthesizeCowboy(string(content), emit)
	case "scala":
		// Consumer side (#3554): sttp (basicRequest/quickRequest verb
		// combinators with uri"..." literals) outbound HTTP client. The Scala
		// producer side is handled by the custom_scala_* framework extractors.
		synthesizeScalaClientWithRuntime(string(content), emitClientRuntime)
	case "dart":
		// Producer side (#4758): Dart server route registrations — shelf_router
		// (`Router()..get('/users/<id>', h)`), dart_frog file-based routes
		// (`routes/users/[id]/index.dart` + `onRequest`) and Conduit
		// (`router.route("/users/[:id]")`) → canonical http_endpoint_definition,
		// in the same shape axum/Vapor/Kemal/Jester emit, so the shared resolver
		// and the e2e route-test linker (#4351) light up for Dart. The base dart
		// extractor stays structural-only; this pass adds the canonical
		// definitions the coverage substrate keys off. Closes the #4757 N/A.
		synthesizeShelfRoutes(string(content), emit)
		synthesizeConduitRoutes(string(content), emit)
		synthesizeDartFrogRoutes(string(content), path, emit)
		// Consumer side (#3574, epic #3571): Flutter mobile HTTP clients —
		// Dio (`dio.get("/path")`) and package:http
		// (`http.get(Uri.parse("..."))`). Emits outbound http_endpoint_call
		// entities + FETCHES edges so the cross-repo linker pairs mobile
		// screens with backend routes.
		synthesizeDartClientWithRuntime(string(content), emitClientRuntime)
	case "swift":
		// Producer side (#4749): Vapor route registrations
		// (`app.get("todos", ":id") { ... }`, `routes.post("users")`,
		// `app.on(.GET, "health")`) → canonical http_endpoint_definition,
		// in the same shape axum/Rocket/Express emit, so the shared resolver
		// and the e2e route-test linker (#4351) light up for Swift. The
		// custom_swift_vapor extractor's SCOPE.Operation/endpoint markers
		// stay for navigation; this pass adds the canonical definitions the
		// coverage substrate keys off.
		synthesizeVaporRoutes(string(content), emit)
		// Consumer side (#3574, epic #3571): iOS mobile HTTP clients —
		// URLSession (`URL(string: "...")` + `httpMethod`) and Alamofire
		// (`AF.request("...", method: .post)`). Emits outbound
		// http_endpoint_call entities + FETCHES edges.
		synthesizeSwiftClientWithRuntime(string(content), emitClientRuntime)
	case "crystal":
		// Producer side (#4749): Crystal web route registrations — Kemal
		// (`get "/users/:id" do ... end`), Amber (`get "/users", Ctrl, :action`
		// inside a `routes` block) and Lucky (`get "/path"` Action-class macro)
		// → canonical http_endpoint_definition, in the same shape
		// axum/Rocket/Express/Vapor emit, so the shared resolver and the e2e
		// route-test linker (#4351) light up for Crystal. The base crystal
		// extractor stays structural-only; this pass adds the canonical
		// definitions the coverage substrate keys off.
		synthesizeKemalRoutes(string(content), emit)
	case "nim":
		// Producer side (#4749): Nim web route registrations — Jester
		// (`routes:` block `get "/users/@id":`) and Prologue
		// (`app.get("/users/{id}", handler)`, `addRoute("/x", h, HttpGet)`) →
		// canonical http_endpoint_definition, in the same shape
		// axum/Rocket/Express/Kemal/Vapor emit, so the shared resolver and the
		// e2e route-test linker (#4351) light up for Nim. The base nim extractor
		// stays structural-only; this pass adds the canonical definitions the
		// coverage substrate keys off.
		synthesizeJester(string(content), emit)
		synthesizePrologue(string(content), emit)
	case "fsharp":
		// Producer side (#4749): F# web route registrations — Giraffe
		// (`GET >=> route "/users" >=> handler`, `routef "/users/%i"`) and Saturn
		// (`router { get "/users/:id" handler }`) → canonical
		// http_endpoint_definition, in the same shape
		// axum/Rocket/Express/Vapor/Kemal emit, so the shared resolver and the
		// e2e route-test linker (#4351) light up for F#. The base fsharp
		// extractor stays structural-only; this pass adds the canonical
		// definitions the coverage substrate keys off.
		synthesizeGiraffeRoutes(string(content), emit)
	case "groovy":
		// Producer side (#4749): Groovy web route registrations — Grails
		// convention controllers (`class BookController { def show() {…} }` →
		// `ANY /book/show`), explicit UrlMappings.groovy (`"/book/$id"(...)` →
		// `/book/{id}`), and Ratpack handler DSL (`get("api/books") {…}`) →
		// canonical http_endpoint_definition, in the same shape
		// axum/Express/Vapor/Kemal emit, so the shared resolver and the e2e
		// route-test linker (#4351) light up for Groovy. The base groovy
		// extractor stays structural-only; this pass adds the canonical
		// definitions the coverage substrate keys off.
		synthesizeGroovyRoutes(string(content), path, emit)
	case "haskell":
		// Producer side (#5373, bootstrap epic #5360): Haskell web route
		// registrations — Scotty (`get "/users/:id" $ do …`), Yesod
		// (`[parseRoutes| /user/#UserId UserR GET |]`) and Servant type-level
		// API DSL (`"users" :> Capture "id" Int :> Get '[JSON] User`) →
		// canonical http_endpoint_definition, in the same shape
		// axum/Express/Vapor/Kemal/Clojure emit, so the shared resolver and the
		// e2e route-test linker (#4351) light up for Haskell. The base haskell
		// extractor stays structural-only; this pass adds the canonical
		// definitions the coverage substrate keys off.
		synthesizeScottyRoutes(string(content), emit)
		synthesizeYesodRoutes(string(content), emit)
		synthesizeServantRoutes(string(content), emit)
	case "ocaml":
		// Producer side (#5374, bootstrap epic #5360): OCaml Dream web route
		// registrations — `Dream.get "/users/:id" handler` verb routes →
		// canonical http_endpoint_definition, in the same shape
		// axum/Scotty/Kemal/Vapor emit, so the shared resolver and the e2e
		// route-test linker (#4351) light up for OCaml. The base ocaml extractor
		// stays structural-only; this pass adds the canonical definitions the
		// coverage substrate keys off.
		synthesizeDreamRoutes(string(content), emit)
	case "yaml", "json":
		// Producer side (#3628, area #16): OpenAPI 3.x / Swagger 2.0 spec
		// files declare the HTTP API surface as ground-truth. Each
		// `paths.<path>.<method>` becomes a canonical http_endpoint_definition
		// whose synthetic ID (`http:<VERB>:<canonical-path>`) is built the SAME
		// way as code-extracted routes, so a spec endpoint CONVERGES on (does not
		// duplicate) the code-extracted endpoint for the same (verb, path). The
		// per-operation wrapper stamps spec-only provenance (source=openapi_spec,
		// provenance=spec) plus operationId / summary / request+response schema
		// refs so spec-only endpoints stay distinguishable from code-extracted
		// ones and act as a parity oracle.
		synthesizeOpenAPI(string(content), func(method, canonicalPath, opID, summary string, reqRef string, respRefs []string) {
			before := len(entities)
			emit(method, canonicalPath, "openapi", "", "")
			if len(entities) == before {
				return
			}
			last := &entities[len(entities)-1]
			if last.Properties == nil {
				last.Properties = map[string]string{}
			}
			// Override the code-route framework default and mark provenance so
			// downstream parity checks can tell a spec-only endpoint apart.
			last.Properties["framework"] = "openapi"
			last.Properties["source"] = "openapi_spec"
			last.Properties["provenance"] = "spec"
			if opID != "" {
				last.Properties["operation_id"] = opID
			}
			if summary != "" {
				last.Properties["summary"] = summary
			}
			if reqRef != "" {
				last.Properties["request_schema"] = reqRef
			}
			if len(respRefs) > 0 {
				last.Properties["response_schemas"] = strings.Join(respRefs, ",")
			}
		})
	}

	// #3628 (epic) — endpoint API-version + deprecation stamping. Both passes
	// mutate Properties on the producer-side http_endpoint_definition entities
	// emitted above (index >= deprecationBefore, same source file); they never
	// add or remove entities, so they cannot regress upstream synthesis.
	//
	//   - applyEndpointAPIVersion: resolves `api_version` from the endpoint's own
	//     canonical `path` property (/api/v1, /v2, …) — language-agnostic.
	//   - applyEndpointDeprecation: resolves `deprecated` (+ deprecated_since /
	//     deprecated_replacement) from the deprecation marker that decorates the
	//     endpoint's handler in the source file (JS/TS JSDoc @deprecated, Spring
	//     @Deprecated, DRF deprecated=True, Python @deprecated / docstring, a
	//     Sunset/Deprecation response header, or a `// DEPRECATED` comment).
	applyEndpointAPIVersion(lang, string(content), entities, path, deprecationBefore)
	applyEndpointDeprecation(lang, string(content), path, entities, deprecationBefore)

	// #3628 (epic) — endpoint pagination-posture stamping. Mutates Properties on
	// the same producer-side http_endpoint_definition entities (index >=
	// deprecationBefore, same source file); adds/removes nothing. Resolves
	// `paginated` / `pagination_style` / `pagination_params` from a recognised
	// DRF pagination_class (or settings DEFAULT_PAGINATION_CLASS), a Spring
	// Pageable param / Page<…> return, an Express/FastAPI limit+offset / page /
	// cursor param shape, or a Sequelize/Prisma/Django Paginator ORM shape.
	// Honest-partial: a lone `limit` with no offset/page/cursor companion is
	// ambiguous and is left unstamped.
	applyEndpointPagination(lang, string(content), path, entities, deprecationBefore)

	// #3628 (epic) — endpoint response status-code set stamping. Mutates
	// Properties on the same producer-side http_endpoint_definition entities
	// (index >= deprecationBefore, same source file); adds/removes nothing.
	// Resolves `response_codes` (sorted unique list) + `success_code` from
	// status literals in the route decorator/annotation + handler body: FastAPI
	// status_code= / HTTPException, DRF status.HTTP_* / Response(status=...) /
	// raised DRF exceptions, Express res.status()/sendStatus() / Nest @HttpCode /
	// Nest exceptions, Spring @ResponseStatus / ResponseEntity builders /
	// ResponseStatusException. Honest-partial: a dynamic status variable is
	// skipped (only literals are recorded).
	applyEndpointResponseCodes(lang, string(content), path, entities, deprecationBefore)

	// #4583/#4584/#4585/#4586 — cross-framework scalar request-param extraction.
	// Generalises the NestJS scalar @Query/@Param work (#4568) to Express/Koa,
	// FastAPI, Spring and DRF: stamps one parameter record {name, in, type,
	// required} per scalar request param (query / path / header) the handler
	// declares onto the SYNTHESIZED http_endpoint_definition the dashboard Paths
	// panel renders. Mutates Properties in place (index >= deprecationBefore, same
	// source file); never adds/removes entities and is a no-op for any endpoint a
	// synthesizer already stamped a `parameters` signature on (e.g. NestJS).
	applyScalarRequestParams(lang, string(content), path, entities, deprecationBefore)

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

// jaxrsVerbLineRe matches a bare JAX-RS verb annotation line. Group 1 = verb.
// The handler and any method-level @Path are bound by a line-oriented forward
// scan (see synthesizeJAXRS) rather than a single multi-line regex, so an
// intervening Swagger annotation whose argument contains a ')' inside a string
// (e.g. `@Operation(summary = "Get (all) widgets")`) cannot truncate the scan
// and silently drop the route — the parens-in-string undercount bug class.
var jaxrsVerbLineRe = regexp.MustCompile(`@(GET|POST|PUT|DELETE|PATCH|HEAD|OPTIONS)\b`)

// jaxrsMethodPathLineRe matches a method-level `@Path("/x")` annotation line and
// captures the path string. Used by the forward scan.
var jaxrsMethodPathLineRe = regexp.MustCompile(`@Path\s*\(\s*"([^"\n\r]*)"\s*\)`)

// jaxrsHandlerNameRe matches a method-declaration line and captures the method
// name. Anchored to the start of a trimmed line. Mirrors javaMethodDeclRe.
var jaxrsHandlerNameRe = regexp.MustCompile(
	`^\s*(?:public|protected|private|static|final|abstract|synchronized|default|\s)+[\w<>\[\],.\s?]+?\s+(\w+)\s*\(`)

// synthesizeJAXRS scans a Java file for JAX-RS handlers. Supports a single
// class-level @Path prefix per file (the dominant convention); files with
// multiple JAX-RS resource classes will still emit endpoints but only
// under the first class prefix.
//
// Binding strategy (parens-in-string immune): for each JAX-RS verb annotation
// line, a forward line scan collects any intervening method-level `@Path` and
// then locates the handler method declaration. Comment, blank and arbitrary
// annotation lines (including multi-line annotation argument lists tracked with
// a string-aware bracket counter) are skipped. This replaces the previous
// single multi-line regex whose `(?:@[\w.]+(?:\([^)]*\))?...)*?` decorator-skip
// stopped at the first ')' inside any intervening annotation's string and
// silently dropped the handler.
func synthesizeJAXRS(content string, emit emitFn) {
	if !strings.Contains(content, "@Path") {
		return
	}
	classPrefix := ""
	if m := jaxrsClassPathRe.FindStringSubmatch(content); len(m) >= 2 {
		classPrefix = m[1]
	}
	lines := strings.Split(content, "\n")
	for lineIdx, line := range lines {
		m := jaxrsVerbLineRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		verb := m[1]
		methodPath, methodName, ok := jaxrsBindHandler(lines, lineIdx+1)
		if !ok {
			continue
		}
		full := joinPathFragments(classPrefix, methodPath)
		canonical := httproutes.Canonicalize(httproutes.FrameworkJAXRS, full)
		emit(verb, canonical, "jaxrs", "Controller", methodName)
	}
}

// jaxrsBindHandler scans forward from `fromLine` to bind a JAX-RS verb
// annotation to its handler. It returns the method-level @Path (or "") and the
// handler method name. ok is false when no handler is found before the next
// verb annotation, the class end, or EOF.
//
// The scan is immune to parens-in-strings inside intervening annotations: an
// annotation that opens a multi-line argument list is consumed via a
// string-aware bracket counter (nestjsBracketDelta) so its continuation lines
// are treated as annotation data, not as a handler. A method-level `@Path` seen
// along the way is captured as the route's method path.
func jaxrsBindHandler(lines []string, fromLine int) (methodPath, methodName string, ok bool) {
	depth := 0
	for i := fromLine; i < len(lines); i++ {
		raw := lines[i]
		t := strings.TrimSpace(raw)
		if t == "" {
			continue
		}
		// Inside a multi-line annotation argument list: keep consuming until
		// the brackets balance again.
		if depth > 0 {
			depth += nestjsBracketDelta(raw)
			if depth < 0 {
				depth = 0
			}
			continue
		}
		// Comment lines.
		if strings.HasPrefix(t, "//") || strings.HasPrefix(t, "*") ||
			strings.HasPrefix(t, "/*") || strings.HasPrefix(t, "*/") {
			continue
		}
		// Another verb annotation before a handler — give up; this verb line
		// has no handler of its own (malformed input).
		if jaxrsVerbLineRe.MatchString(t) {
			return "", "", false
		}
		// Annotation line. Capture a method-level @Path; track any multi-line
		// argument list so its continuation lines are skipped as data.
		if strings.HasPrefix(t, "@") {
			if methodPath == "" {
				if pm := jaxrsMethodPathLineRe.FindStringSubmatch(t); pm != nil {
					methodPath = pm[1]
				}
			}
			depth += nestjsBracketDelta(raw)
			if depth < 0 {
				depth = 0
			}
			continue
		}
		// Handler declaration line.
		if hm := jaxrsHandlerNameRe.FindStringSubmatch(t); hm != nil {
			return methodPath, hm[1], true
		}
		// A non-blank, non-comment, non-annotation, non-handler line means we
		// walked past the handler region. Stop.
		return "", "", false
	}
	return "", "", false
}

// ---------------------------------------------------------------------------
// Javalin (Java) — lambda DSL routing (#3085)
// ---------------------------------------------------------------------------

// javalinRouteRe captures Javalin lambda-DSL route registrations of the form:
//
//	app.get("/path", handler)
//	app.post("/path", ctx -> { ... })
//
// The receiver is always `app` (or any variable name Javalin is assigned to);
// we match against the well-known verb names and capture verb + path.
var javalinRouteRe = regexp.MustCompile(
	`\bapp\s*\.\s*(get|post|put|delete|patch|head|options)\s*\(\s*"([^"]+)"`)

// synthesizeJavalin scans a Java file for Javalin lambda-DSL route
// registrations and emits one http_endpoint_definition per (verb, path) pair.
// Javalin uses `{param}` curly-brace path parameters (JAX-RS style), so
// FrameworkJavalin is passed to Canonicalize.
func synthesizeJavalin(content string, emit emitFn) {
	// File-signal gate: require a Javalin-specific import or API reference.
	if !strings.Contains(content, "javalin") && !strings.Contains(content, "Javalin") {
		return
	}
	for _, m := range javalinRouteRe.FindAllStringSubmatch(content, -1) {
		if len(m) < 3 {
			continue
		}
		verb := strings.ToUpper(m[1])
		rawPath := m[2]
		canonical := httproutes.Canonicalize(httproutes.FrameworkJavalin, rawPath)
		emit(verb, canonical, "javalin", "Route", "")
	}
}

// ---------------------------------------------------------------------------
// Vert.x (Java) — lambda DSL routing (#3086)
// ---------------------------------------------------------------------------

// vertxRouteRe captures Vert.x Web router lambda-DSL route registrations of the form:
//
//	router.get("/path").handler(handler)
//	router.post("/path").handler(ctx -> { ... })
//	router.route("/path").handler(...)  — path-scoped handler chain
//
// The verb list excludes "route" which is matched separately.
var vertxRouteRe = regexp.MustCompile(
	`\brouter\s*\.\s*(get|post|put|delete|patch|head|options)\s*\(\s*"([^"]+)"`)

// synthesizeVertx scans a Java file for Vert.x Web router lambda-DSL route
// registrations and emits one http_endpoint_definition per (verb, path) pair.
// Vert.x Web uses `{param}` curly-brace path parameters (JAX-RS style), so
// FrameworkVertx is passed to Canonicalize.
func synthesizeVertx(content string, emit emitFn) {
	// File-signal gate: require a Vert.x-specific import or API reference.
	if !strings.Contains(content, "vertx") && !strings.Contains(content, "Vertx") &&
		!strings.Contains(content, "AbstractVerticle") && !strings.Contains(content, "Router") {
		return
	}
	// Secondary gate: require router.get/post/... pattern.
	if !strings.Contains(content, "router.") && !strings.Contains(content, "router ") {
		return
	}
	for _, m := range vertxRouteRe.FindAllStringSubmatch(content, -1) {
		if len(m) < 3 {
			continue
		}
		verb := strings.ToUpper(m[1])
		rawPath := m[2]
		canonical := httproutes.Canonicalize(httproutes.FrameworkVertx, rawPath)
		emit(verb, canonical, "vertx", "Route", "")
	}
}

// ---------------------------------------------------------------------------
// Struts (Java) — @Action annotation + struts.xml routing (#3089)
// ---------------------------------------------------------------------------

// strutsActionAnnotationSynthRe captures @Action(value="/path") and the
// shorthand @Action("/path") on Struts 2 action classes or methods.
// Capture group 1: the path string.
var strutsActionAnnotationSynthRe = regexp.MustCompile(
	`@Action\s*\(\s*(?:value\s*=\s*)?"([^"]+)"`)

// strutsNamespaceSynthRe captures @Namespace("/prefix") at the class level.
var strutsNamespaceSynthRe = regexp.MustCompile(
	`@Namespace\s*\(\s*"([^"]+)"`)

// synthesizeStruts scans a Java file for Struts 2 @Action annotation route
// declarations and emits one http_endpoint_definition per annotated action.
// Struts routes do not carry an explicit HTTP verb (all HTTP methods can
// reach an action), so we emit "ANY" as the canonical method. The path is
// the value attribute of @Action, optionally prefixed by @Namespace.
//
// FrameworkSpring is reused for canonicalization because Struts 2 uses the
// same {param} curly-brace parameter syntax as Spring and JAX-RS.
func synthesizeStruts(content string, emit emitFn) {
	// File-signal gate: require a Struts-specific import or annotation.
	if !strings.Contains(content, "@Action") &&
		!strings.Contains(content, "ActionSupport") &&
		!strings.Contains(content, "org.apache.struts2") &&
		!strings.Contains(content, "opensymphony") {
		return
	}

	// Detect package-level @Namespace prefix.
	namespace := ""
	if m := strutsNamespaceSynthRe.FindStringSubmatch(content); len(m) >= 2 {
		namespace = strings.TrimRight(m[1], "/")
		if namespace == "/" {
			namespace = ""
		}
	}

	for _, m := range strutsActionAnnotationSynthRe.FindAllStringSubmatch(content, -1) {
		if len(m) < 2 {
			continue
		}
		rawPath := m[1]
		fullPath := rawPath
		if namespace != "" && !strings.HasPrefix(rawPath, namespace) {
			if !strings.HasPrefix(rawPath, "/") {
				rawPath = "/" + rawPath
			}
			fullPath = namespace + rawPath
		} else if !strings.HasPrefix(rawPath, "/") {
			fullPath = "/" + rawPath
		}
		canonical := httproutes.Canonicalize(httproutes.FrameworkSpring, fullPath)
		emit("ANY", canonical, "struts", "Route", "")
	}
}

// ---------------------------------------------------------------------------
// Play Framework (Java) — conf/routes DSL routing (#3090)
// ---------------------------------------------------------------------------

// playRoutesLineSynthRe captures Play conf/routes lines of the form:
//
//	GET   /path                   controllers.Foo.bar
//	POST  /path/:id               controllers.Foo.create(id: Long)
//	GET   /path/$id<[0-9]+>       controllers.Foo.show(id: Long)
//
// Capture groups: 1=verb, 2=path, 3=controller.action (with optional param list).
var playRoutesLineSynthRe = regexp.MustCompile(
	`(?m)^[ \t]*(GET|POST|PUT|DELETE|PATCH|HEAD|OPTIONS)\s+(/\S*)\s+([\w.]+(?:\([^)]*\))?)`)

// playDollarParamSynthRe converts Play $param<regex> to {param} form.
var playDollarParamSynthRe = regexp.MustCompile(`\$(\w+)<[^>]*>`)

// playColonParamSynthRe converts Play :param to {param} form.
var playColonParamSynthRe = regexp.MustCompile(`:(\w+)`)

// canonicalizePlayPath normalises a Play path string (colon and dollar params)
// to the {param} canonical form. No httproutes.Canonicalize call is needed
// because this function handles both Play-specific forms directly.
func canonicalizePlayPath(raw string) string {
	// Strip query string if any.
	if q := strings.Index(raw, "?"); q >= 0 {
		raw = raw[:q]
	}
	// $param<regex> → {param}
	out := playDollarParamSynthRe.ReplaceAllString(raw, "{$1}")
	// :param → {param}
	out = playColonParamSynthRe.ReplaceAllString(out, "{$1}")
	return out
}

// isPlayRoutesFilePath returns true if the file path looks like a Play routes
// file (conf/routes or a named routes variant like conf/routes.GET).
func isPlayRoutesFilePath(path string) bool {
	base := path
	if idx := strings.LastIndex(path, "/"); idx >= 0 {
		base = path[idx+1:]
	}
	if base == "routes" || strings.HasPrefix(base, "routes.") || strings.HasSuffix(base, ".routes") {
		return true
	}
	return strings.Contains(path, "conf/routes")
}

// synthesizePlay scans a Play Framework conf/routes file and emits one
// http_endpoint_definition per (verb, canonical-path) pair. Play uses two
// path-parameter styles — colon (:id) and dollar+regex ($id<regex>) — both
// normalised to {id} by canonicalizePlayPath.
//
// The synthesizer is a no-op on Java source files (controllers); those are
// wired via the conf/routes file. The file-path gate ensures we only emit
// synthetics from the actual routes file.
func synthesizePlay(content, filePath string, emit emitFn) {
	// Only emit from Play routes files, not from Java controller source.
	if !isPlayRoutesFilePath(filePath) {
		return
	}

	for _, m := range playRoutesLineSynthRe.FindAllStringSubmatch(content, -1) {
		if len(m) < 3 {
			continue
		}
		verb := m[1]
		rawPath := m[2]
		canonical := canonicalizePlayPath(rawPath)
		emit(verb, canonical, "play", "Route", "")
	}
}

// ---------------------------------------------------------------------------
// Akka-HTTP (Java) — directive DSL routing (#3092)
// ---------------------------------------------------------------------------

// akkaHTTPPathSynthRe captures Akka-HTTP Java DSL path("segment") directives.
//
//	path("users", () -> get(() -> complete(...)))
//	path("orders", () -> post(() -> entity(...)))
//
// Capture group 1: path segment string literal.
var akkaHTTPPathSynthRe = regexp.MustCompile(
	`\bpath\s*\(\s*"([^"]+)"`)

// akkaHTTPMethodSynthRe captures HTTP method directives in Akka-HTTP Java DSL.
// Capture group 1: lowercase HTTP verb.
var akkaHTTPMethodSynthRe = regexp.MustCompile(
	`\b(get|post|put|delete|patch|head|options)\s*\(\s*(?:\(\s*\)|[a-z_]\w*\s*->|\(\s*\)\s*->)`)

// synthesizeAkkaHTTP scans a Java file for Akka-HTTP Java DSL route
// registrations and emits one http_endpoint_definition per (verb, path) pair.
// Akka-HTTP path strings are plain segments without parameter syntax markers;
// dynamic segments come from PathMatcher / segment() which are not string
// literals, so canonicalisation is identity + slash normalisation (default case).
func synthesizeAkkaHTTP(content string, emit emitFn) {
	// File-signal gate: require an Akka-HTTP-specific import or API reference.
	if !strings.Contains(content, "akka.http") && !strings.Contains(content, "AllDirectives") &&
		!strings.Contains(content, "akka-http") {
		return
	}
	// Secondary gate: require path() or pathPrefix() directive.
	if !strings.Contains(content, "path(") && !strings.Contains(content, "pathPrefix(") {
		return
	}

	for _, idx := range akkaHTTPPathSynthRe.FindAllStringSubmatchIndex(content, -1) {
		if len(idx) < 4 {
			continue
		}
		rawPath := content[idx[2]:idx[3]]
		canonical := httproutes.Canonicalize(httproutes.FrameworkAkkaHTTP, rawPath)

		// Scan up to 10 lines ahead and 5 lines before for an HTTP method directive.
		lineStart := idx[0]
		blockEnd := lineStart
		newlinesFound := 0
		for blockEnd < len(content) && newlinesFound < 10 {
			if content[blockEnd] == '\n' {
				newlinesFound++
			}
			blockEnd++
		}
		if blockEnd > len(content) {
			blockEnd = len(content)
		}
		blockStart := lineStart
		newlinesBefore := 0
		for blockStart > 0 && newlinesBefore < 5 {
			blockStart--
			if content[blockStart] == '\n' {
				newlinesBefore++
			}
		}

		snippet := content[blockStart:blockEnd]
		methods := akkaHTTPMethodSynthRe.FindAllStringSubmatch(snippet, -1)
		if len(methods) == 0 {
			// No verb detected in the surrounding block — emit with ANY.
			emit("ANY", canonical, "akka-http", "Route", "")
		} else {
			for _, mm := range methods {
				verb := strings.ToUpper(mm[1])
				emit(verb, canonical, "akka-http", "Route", "")
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Spring WebFlux (Java) — functional DSL routing (#3080)
// ---------------------------------------------------------------------------

// webFluxChainedSynthRe captures chained .GET("/path", handler) / .POST(...)
// method calls in a RouterFunctions.route() builder chain. These are always
// uppercase method names used as direct DSL verbs on the RouterFunctions API.
//
// Capture groups:
//
//	1: HTTP verb
//	2: path string
//	3: the handler argument (everything after the `"path",` up to the next
//	   top-level `)`, `.`, or end-of-statement). #4384 — captured so a lambda
//	   (`req -> ...`) can be synthesised as an inline handler and a method
//	   reference (`this::create`, `handler::create`) can be resolved to the
//	   named method, mirroring the JS #4324 / Go #4382 inline-handler fix.
//
// The handler group is OPTIONAL (`(?:...)?`) because the single-argument
// predicate-builder shape `.GET("/path")` (handler supplied separately) also
// occurs; an empty group 3 simply yields no handler signal.
var webFluxChainedSynthRe = regexp.MustCompile(
	`\.\s*(GET|POST|PUT|DELETE|PATCH|HEAD|OPTIONS)\s*\(\s*"([^"]+)"\s*(?:,\s*([^,)][^)]*?))?\s*\)`)

// webFluxPredicateSynthRe captures the two-argument overload:
//
//	RouterFunctions.route(RequestPredicates.GET("/path"), handler)
//	RouterFunctions.route(GET("/path"), handler)            (static import)
//	...andRoute(GET("/path"), handler)
//
// Capture groups:
//
//	1: HTTP verb
//	2: path string
//	3: the handler argument (after the predicate's closing paren + comma, up
//	   to the enclosing route(...) / andRoute(...) close paren). #4384.
var webFluxPredicateSynthRe = regexp.MustCompile(
	`(?:RequestPredicates\s*\.\s*)?\b(GET|POST|PUT|DELETE|PATCH|HEAD|OPTIONS)\s*\(\s*"([^"]+)"\s*\)\s*(?:,\s*([^,)][^)]*?))?\s*\)`)

// webFluxNestRe captures `.nest(path("/prefix"), ...)` / `RouterFunctions
// .nest(RequestPredicates.path("/prefix"), ...)` calls so the nested prefix can
// be composed onto the routes declared inside the nest builder. #4384.
//
// Capture group 1: the nested path prefix string.
var webFluxNestRe = regexp.MustCompile(
	`\.\s*nest\s*\(\s*(?:RequestPredicates\s*\.\s*)?path\s*\(\s*"([^"]+)"`)

// webFluxMethodRefHandler matches a Java method reference used as a functional
// route handler — `this::create`, `handler::getUser`, `OrderHandler::list`,
// `co.example.Handler::list`. The captured method name (group 1) is the bare
// method symbol the endpoint resolves to. #4384.
var webFluxMethodRefHandler = regexp.MustCompile(
	`(?:[\w.$]+\s*::\s*)([\w$]+)\s*$`)

// webFluxLambdaHandler matches a Java lambda used as a functional route handler
// — `req -> ...`, `request -> ...`, `(req) -> ...`, `(ServerRequest r) -> ...`.
// A lambda has no addressable symbol, so the endpoint gets a synthesised inline
// handler (#4324 mechanism). #4384.
var webFluxLambdaHandler = regexp.MustCompile(`->`)

// classifyWebFluxHandler inspects the raw handler-argument text captured from a
// functional-DSL route registration and returns the (refKind, refName) pair
// makeEmit needs:
//
//   - method reference (`this::create`)  → ("SCOPE.Operation", "create") so the
//     synthesis-time structural bridge resolves it to the named, same-file
//     handler method (no inline stand-in — it is a real symbol).
//   - lambda (`req -> ...`)              → (inlineHandlerRefKind, "") so makeEmit
//     synthesises a merge-stable inline handler node + bridge.
//   - unknown / absent                  → (inlineHandlerRefKind, "") — a
//     functional route ALWAYS has a handler, so model it as inline rather than
//     leaving the endpoint a handler-less graph island.
func classifyWebFluxHandler(rawHandler string) (refKind, refName string) {
	h := strings.TrimSpace(rawHandler)
	if h == "" {
		return inlineHandlerRefKind, ""
	}
	// Method reference takes precedence: `this::create` contains no `->`.
	if m := webFluxMethodRefHandler.FindStringSubmatch(h); m != nil &&
		!webFluxLambdaHandler.MatchString(h) {
		return "SCOPE.Operation", m[1]
	}
	// Anything else with a `->` is a lambda; default everything else to inline.
	return inlineHandlerRefKind, ""
}

// synthesizeSpringWebFlux scans a Java file for Spring WebFlux / WebMvc.fn
// functional-DSL route registrations and emits one http_endpoint_definition per
// (verb, path) pair, WITH a handler linkage (#4384):
//
//   - lambda handler (`req -> ...`)            → inline-handler synth + bridge
//   - method-ref handler (`this::create`)      → bridge to the named method
//
// Forms detected:
//  1. Chained builder:   RouterFunctions.route().GET("/path", handler)
//  2. Static predicate:  RouterFunctions.route(GET("/path"), handler)
//     ...andRoute(GET("/path"), handler)
//  3. Nested prefix:     .nest(path("/api"), builder -> builder.GET("/x", h))
//     — the "/api" prefix is composed onto every route inside the nest.
//
// Spring WebFlux uses `{param}` curly-brace path parameters (same as Spring
// MVC), so FrameworkSpring is passed to Canonicalize.
func synthesizeSpringWebFlux(content string, emit emitFn) {
	// File-signal gate: require RouterFunction / RouterFunctions / RequestPredicates.
	if !strings.Contains(content, "RouterFunction") &&
		!strings.Contains(content, "RouterFunctions") &&
		!strings.Contains(content, "RequestPredicates") {
		return
	}
	// Secondary gate: must have the route() call or a @Bean RouterFunction annotation
	// to confirm this is functional-DSL routing (not just an import or comment).
	if !strings.Contains(content, "route(") && !strings.Contains(content, "RouterFunction<") {
		return
	}

	// Compute the nest prefix that applies at each byte offset. A `.nest(
	// path("/prefix"), ...)` composes its prefix onto every route registered
	// inside its builder lambda. We approximate the builder scope with the
	// brace span that follows the nest call; routes whose offset falls inside
	// that span get the prefix prepended. Multiple/sibling nests are handled
	// independently and nested nests compose (longest enclosing span wins per
	// level). This is a deliberately conservative static approximation — when a
	// route is not inside any nest span it gets no prefix.
	prefixAt := webFluxNestPrefixIndex(content)

	seen := make(map[string]bool)

	emitRoute := func(verb, rawPath, rawHandler string, offset int) {
		fullPath := prefixAt(offset) + rawPath
		key := verb + ":" + fullPath
		if seen[key] {
			return
		}
		seen[key] = true
		canonical := httproutes.Canonicalize(httproutes.FrameworkSpring, fullPath)
		refKind, refName := classifyWebFluxHandler(rawHandler)
		emit(verb, canonical, "spring_webflux", refKind, refName)
	}

	// Form 2 FIRST: the predicate overload `route(GET("/p"), handler)` —
	// its inner `GET("/p")` would also match the chained regex (form 1), so we
	// claim it here first to capture the trailing handler argument, then
	// dedup form 1 against it via `seen`.
	for _, idx := range webFluxPredicateSynthRe.FindAllStringSubmatchIndex(content, -1) {
		if len(idx) < 6 {
			continue
		}
		verb := content[idx[2]:idx[3]]
		rawPath := content[idx[4]:idx[5]]
		handler := ""
		if idx[6] >= 0 {
			handler = content[idx[6]:idx[7]]
		}
		emitRoute(verb, rawPath, handler, idx[0])
	}

	// Form 1: chained .GET/.POST/... verbs with a handler argument.
	for _, idx := range webFluxChainedSynthRe.FindAllStringSubmatchIndex(content, -1) {
		if len(idx) < 6 {
			continue
		}
		verb := content[idx[2]:idx[3]]
		rawPath := content[idx[4]:idx[5]]
		handler := ""
		if idx[6] >= 0 {
			handler = content[idx[6]:idx[7]]
		}
		emitRoute(verb, rawPath, handler, idx[0])
	}
}

// webFluxNestPrefixIndex returns a function mapping a byte offset in content to
// the composed `.nest(path("/prefix"), ...)` prefix that applies there. Each
// nest call's scope is approximated by the balanced-brace `{...}` or balanced-
// paren builder span that follows it; offsets inside that span inherit the
// prefix, and enclosing nests compose (outer prefix + inner prefix). #4384.
func webFluxNestPrefixIndex(content string) func(offset int) string {
	type span struct {
		start, end int
		prefix     string
	}
	var spans []span
	for _, idx := range webFluxNestRe.FindAllStringSubmatchIndex(content, -1) {
		if len(idx) < 4 {
			continue
		}
		prefix := content[idx[2]:idx[3]]
		// Find the builder body that follows this nest call. Spring's nest
		// builder is `builder -> { ... }` (block) or `builder -> builder...`
		// (single expr). We scope to the balanced-paren span of the nest(...)
		// call itself — every route registered as an argument of this nest
		// lives within it. Start the balance at the nest's opening paren.
		open := strings.IndexByte(content[idx[0]:], '(')
		if open < 0 {
			continue
		}
		start := idx[0] + open
		end := matchBalancedParen(content, start)
		if end <= start {
			continue
		}
		spans = append(spans, span{start: start, end: end, prefix: prefix})
	}
	if len(spans) == 0 {
		return func(int) string { return "" }
	}
	return func(offset int) string {
		var composed string
		// Compose all enclosing spans, outermost first (smallest start).
		// Collect enclosing prefixes, then order by start ascending.
		var enclosing []span
		for _, s := range spans {
			if offset > s.start && offset < s.end {
				enclosing = append(enclosing, s)
			}
		}
		// Sort by start ascending (outer → inner) so prefixes compose in order.
		for i := 0; i < len(enclosing); i++ {
			for j := i + 1; j < len(enclosing); j++ {
				if enclosing[j].start < enclosing[i].start {
					enclosing[i], enclosing[j] = enclosing[j], enclosing[i]
				}
			}
		}
		for _, s := range enclosing {
			composed += s.prefix
		}
		return composed
	}
}

// matchBalancedParen returns the offset just past the `)` that balances the `(`
// at position open in content, or -1 if unbalanced. String literals are skipped
// so a `)` inside a "..." does not throw off the count.
func matchBalancedParen(content string, open int) int {
	if open >= len(content) || content[open] != '(' {
		return -1
	}
	depth := 0
	inStr := false
	for i := open; i < len(content); i++ {
		c := content[i]
		if inStr {
			if c == '\\' {
				i++
				continue
			}
			if c == '"' {
				inStr = false
			}
			continue
		}
		switch c {
		case '"':
			inStr = true
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return i + 1
			}
		}
	}
	return -1
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

// ---------------------------------------------------------------------------
// #4383 — PROGRAMMATIC route registration (Python)
//
// The decorator forms above only cover `@app.route`/`@app.get` on a named
// `def`. A second, equally idiomatic registration shape passes the handler as
// a VALUE to a registration METHOD:
//
//	Flask:     app.add_url_rule('/users', view_func=list_users)
//	           app.add_url_rule('/users', 'users', UserView.as_view('users'))
//	           bp.add_url_rule('/x', view_func=lambda: ...)
//	FastAPI:   app.add_api_route('/items', get_items, methods=['GET'])
//	           router.add_api_route('/items', get_items)
//	Starlette: app.add_route('/x', handler)
//	           Route('/x', endpoint=handler)  (already handled in synthesizeStarlette)
//
// These were NOT extracted at all before this fix, so the endpoint was missing
// entirely (not merely handler-less). We model the handler shape-agnostically,
// mirroring #4324 / #4319:
//   - a NAMED function/class reference (`list_users`, `UserView.as_view('users')`)
//     → resolve to that named symbol via refKind="SCOPE.Operation" so the
//       synthesis-time named bridge (#4319) links the endpoint to the real
//       handler def.
//   - an inline `lambda` → refKind=inlineHandlerRefKind so makeEmit synthesizes a
//     stable inline-handler stand-in + bridge (#4324) instead of an island.
//
// pyProgrammaticHandlerRef classifies the raw handler-argument text and returns
// the (refKind, refName, isLambda) triple the emit path expects.

// pyLambdaArgRe detects a `lambda ...:` handler value.
var pyLambdaArgRe = regexp.MustCompile(`^\s*lambda\b`)

// pyAsViewRe captures `SomeView.as_view('name')` / `SomeView.as_view("name")`
// (Flask class-based views) and keeps the VIEW CLASS name as the handler symbol.
var pyAsViewRe = regexp.MustCompile(`^\s*([A-Za-z_][\w.]*)\.as_view\s*\(`)

// pyNamedRefRe captures a bare dotted identifier handler value
// (`list_users`, `views.get_items`) with no trailing call — a function or
// callable passed by reference.
var pyNamedRefRe = regexp.MustCompile(`^\s*([A-Za-z_][\w.]*)\s*$`)

// pyProgrammaticHandlerRef classifies a programmatic-route handler argument.
// Returns the refKind/refName makeEmit expects:
//   - lambda            → (inlineHandlerRefKind, "")
//   - Cls.as_view(...)  → ("SCOPE.Operation", "Cls")  (resolve to the view class)
//   - named/dotted ref  → ("SCOPE.Operation", "<lastSegment>")
//   - anything else     → (inlineHandlerRefKind, "")   (callable expression with
//     no addressable symbol — model as inline, never drop the endpoint)
func pyProgrammaticHandlerRef(raw string) (refKind, refName string) {
	raw = strings.TrimSpace(raw)
	if raw == "" || pyLambdaArgRe.MatchString(raw) {
		return inlineHandlerRefKind, ""
	}
	if m := pyAsViewRe.FindStringSubmatch(raw); len(m) >= 2 {
		name := m[1]
		if i := strings.LastIndexByte(name, '.'); i >= 0 {
			name = name[i+1:]
		}
		// refKind "Controller" mirrors the proven Flask/FastAPI DECORATOR
		// convention: it survives ResolveHTTPEndpointHandlers when the handler
		// def lives in another module (live pipeline) via the Controller
		// keep-path, and still fires the #4319 same-file structural bridge
		// (which covers refKind ∈ {Controller, View, SCOPE.Operation}).
		return "Controller", name
	}
	if m := pyNamedRefRe.FindStringSubmatch(raw); len(m) >= 2 {
		name := m[1]
		if i := strings.LastIndexByte(name, '.'); i >= 0 {
			name = name[i+1:]
		}
		return "Controller", name
	}
	// Callable expression we cannot resolve to a symbol (e.g. `make_handler()`).
	return inlineHandlerRefKind, ""
}

// flaskAddURLRuleRe captures `<recv>.add_url_rule(<path>, <rest>)`. Group 1 is
// the path literal; group 2 is the remainder of the call args (which may carry
// a positional endpoint name, a `view_func=` kwarg and/or a `methods=` kwarg).
// One level of nested parens is tolerated so `view_func=UserView.as_view('x')`
// or `methods=['GET']` does not abort the match early.
var flaskAddURLRuleRe = regexp.MustCompile(
	`\b\w+\.add_url_rule\s*\(\s*["']([^"'\n\r]+)["']((?:[^()]*(?:\([^()]*\)[^()]*)*))\)`,
)

// flaskViewFuncKwargRe captures the `view_func=<value>` handler argument.
var flaskViewFuncKwargRe = regexp.MustCompile(`view_func\s*=\s*(lambda\b[^,)]*|[A-Za-z_][\w.]*\s*(?:\.as_view\s*\([^)]*\))?)`)

// fastapiAddAPIRouteRe captures `<recv>.add_api_route(<path>, <rest>)`.
// Group 1 = path literal, group 2 = remaining args (positional/`endpoint=`
// handler plus optional `methods=`). One level of nested parens tolerated.
var fastapiAddAPIRouteRe = regexp.MustCompile(
	`\b\w+\.add_api_route\s*\(\s*["']([^"'\n\r]+)["']((?:[^()]*(?:\([^()]*\)[^()]*)*))\)`,
)

// fastapiEndpointKwargRe captures the `endpoint=<value>` handler argument of an
// add_api_route call.
var fastapiEndpointKwargRe = regexp.MustCompile(`endpoint\s*=\s*(lambda\b[^,)]*|[A-Za-z_][\w.]*)`)

// starletteAddRouteRe captures `<recv>.add_route(<path>, <handler>, ...)`.
// Group 1 = path literal, group 2 = remaining args (positional/`endpoint=`
// handler plus optional `methods=`).
var starletteAddRouteRe = regexp.MustCompile(
	`\b\w+\.add_route\s*\(\s*["']([^"'\n\r]+)["']((?:[^()]*(?:\([^()]*\)[^()]*)*))\)`,
)

// pyFirstPositionalArgRe captures the first positional argument value at the
// start of an args tail (i.e. immediately after the leading comma that follows
// the path literal), so long as it is NOT a `kwarg=` assignment. Used to pull
// the handler out of `add_api_route('/x', get_items, ...)` /
// `add_route('/x', handler)` / `add_url_rule('/x', 'name', Cls.as_view(...))`.
var pyFirstPositionalArgRe = regexp.MustCompile(`^\s*,\s*(lambda\b[^,)]*|["'][^"'\n\r]*["']|[A-Za-z_][\w.]*\s*(?:\.as_view\s*\([^)]*\))?)`)

func synthesizeFlask(content string, emit emitDefFn) {
	if !strings.Contains(content, ".route(") && !strings.Contains(content, ".get(") &&
		!strings.Contains(content, ".post(") && !strings.Contains(content, ".put(") &&
		!strings.Contains(content, ".patch(") && !strings.Contains(content, ".delete(") &&
		!strings.Contains(content, ".add_url_rule(") {
		return
	}
	// #4383 — programmatic registration: app.add_url_rule('/x', view_func=...)
	// or app.add_url_rule('/x', 'endpoint', UserView.as_view('x')). Works for
	// blueprint receivers (bp.add_url_rule) identically.
	synthesizeFlaskAddURLRule(content, emit)
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

// synthesizeFlaskAddURLRule extracts programmatic Flask route registration
// via `add_url_rule` (#4383). The handler may arrive three ways:
//
//	app.add_url_rule('/users', view_func=list_users)            (kwarg, named)
//	app.add_url_rule('/users', 'users', UserView.as_view('u'))  (positional CBV)
//	app.add_url_rule('/x', view_func=lambda: ...)               (kwarg, lambda)
//
// Verb list comes from a `methods=[...]` kwarg; Flask defaults to GET.
func synthesizeFlaskAddURLRule(content string, emit emitDefFn) {
	if !strings.Contains(content, ".add_url_rule(") {
		return
	}
	for _, idx := range flaskAddURLRuleRe.FindAllStringSubmatchIndex(content, -1) {
		if len(idx) < 6 {
			continue
		}
		raw := content[idx[2]:idx[3]]
		tail := content[idx[4]:idx[5]]

		// Handler: prefer an explicit `view_func=` kwarg; otherwise the third
		// positional arg (after the path and the endpoint-name string) — skip the
		// endpoint-name string literal to reach the callable.
		handlerArg := ""
		if vm := flaskViewFuncKwargRe.FindStringSubmatch(tail); len(vm) >= 2 {
			handlerArg = vm[1]
		} else {
			rest := tail
			for {
				pm := pyFirstPositionalArgRe.FindStringSubmatch(rest)
				if len(pm) < 2 {
					break
				}
				cand := strings.TrimSpace(pm[1])
				// Skip the endpoint-NAME string literal positional arg. rest then
				// already begins at the comma preceding the NEXT positional, so the
				// leading-comma anchor stays primed without re-prepending one.
				if strings.HasPrefix(cand, `"`) || strings.HasPrefix(cand, `'`) {
					loc := pyFirstPositionalArgRe.FindStringIndex(rest)
					rest = rest[loc[1]:]
					continue
				}
				handlerArg = cand
				break
			}
		}

		refKind, refName := pyProgrammaticHandlerRef(handlerArg)
		canonical := httproutes.Canonicalize(httproutes.FrameworkFlask, raw)

		methods := parseFlaskMethods(tail)
		if len(methods) == 0 {
			methods = []string{"GET"}
		}
		defLine := 0
		if refName != "" {
			defLine = findPyDefLine(content, refName)
		}
		if defLine == 0 {
			defLine = lineOfOffset(content, idx[0])
		}
		for _, verb := range methods {
			emit(verb, canonical, "flask", refKind, refName, defLine)
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
// The decorator argument tail tolerates ONE level of nested parens so a
// `dependencies=[Depends(verify_token)]` / `response_model=Foo()` kwarg does not
// terminate the match prematurely (the inner `)` previously aborted the scan,
// dropping the whole endpoint — #3628).
var fastapiVerbDecoratorRe = regexp.MustCompile(`@(?:app|router|api|\w+_router)\.(get|post|put|patch|delete|head|options|trace)\s*\(\s*["']([^"'\n\r]+)["'](?:[^()]*(?:\([^()]*\)[^()]*)*)\)\s*[\r\n]+(?:\s*@[^\n\r]*[\r\n]+)*\s*(?:async\s+)?def\s+(\w+)`)

func synthesizeFastAPI(content string, emit emitDefFn) {
	if !strings.Contains(content, "FastAPI") && !strings.Contains(content, "APIRouter") &&
		!strings.Contains(content, "@app.") && !strings.Contains(content, "@router.") &&
		!strings.Contains(content, ".add_api_route(") {
		return
	}
	// #4383 — programmatic registration: app.add_api_route('/items', get_items,
	// methods=['GET']) / router.add_api_route(...).
	synthesizeFastAPIAddRoute(content, emit)
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

// synthesizeFastAPIAddRoute extracts programmatic FastAPI route registration
// via `add_api_route` (#4383):
//
//	app.add_api_route('/items', get_items, methods=['GET'])
//	router.add_api_route('/items', get_items)          (defaults to GET)
//	app.add_api_route('/x', endpoint=handler)          (kwarg form)
//	app.add_api_route('/x', lambda: ...)               (inline)
//
// FastAPI's add_api_route defaults to GET when `methods=` is omitted.
func synthesizeFastAPIAddRoute(content string, emit emitDefFn) {
	if !strings.Contains(content, ".add_api_route(") {
		return
	}
	for _, idx := range fastapiAddAPIRouteRe.FindAllStringSubmatchIndex(content, -1) {
		if len(idx) < 6 {
			continue
		}
		raw := content[idx[2]:idx[3]]
		tail := content[idx[4]:idx[5]]

		handlerArg := ""
		if em := fastapiEndpointKwargRe.FindStringSubmatch(tail); len(em) >= 2 {
			handlerArg = em[1]
		} else if pm := pyFirstPositionalArgRe.FindStringSubmatch(tail); len(pm) >= 2 {
			handlerArg = pm[1]
		}

		refKind, refName := pyProgrammaticHandlerRef(handlerArg)
		canonical := httproutes.Canonicalize(httproutes.FrameworkFastAPI, raw)

		methods := parseFlaskMethods(tail) // same methods=[...] shape
		if len(methods) == 0 {
			methods = []string{"GET"}
		}
		defLine := 0
		if refName != "" {
			defLine = findPyDefLine(content, refName)
		}
		if defLine == 0 {
			defLine = lineOfOffset(content, idx[0])
		}
		for _, verb := range methods {
			emit(verb, canonical, "fastapi", refKind, refName, defLine)
		}
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

// starlettePositionalEndpointRe captures the SECOND positional argument of a
// Route(...) call — the handler — in the very common
// `Route("/path", handler, methods=[...])` form (no `endpoint=` kwarg). The
// tail it scans begins at the comma after the path literal, so the first
// `name` / `mod.name` token that is NOT a `kwarg=` assignment is the handler.
// Anchored to the start of the tail (optionally after whitespace) so a name
// appearing later inside `methods=[...]` cannot be mistaken for the handler.
var starlettePositionalEndpointRe = regexp.MustCompile(`^\s*,\s*([A-Za-z_][\w.]*)\s*(?:,|$)`)

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
	// #4383 — programmatic registration: app.add_route('/x', handler).
	synthesizeStarletteAddRoute(content, emit)
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
		} else if pm := starlettePositionalEndpointRe.FindStringSubmatch(tail); len(pm) >= 2 {
			// `Route("/path", handler, methods=[...])` — handler is the second
			// positional argument (no `endpoint=` kwarg).
			handler = pm[1]
		}
		// #4383 — `Route("/x", endpoint=lambda ...: ...)` has no addressable
		// handler symbol. The identifier regexes above capture the bare word
		// `lambda` as if it were a handler name; treat it (and an empty handler)
		// as inline so the endpoint is bridged to a synthesized stand-in rather
		// than left a graph island.
		if handler == "lambda" {
			handler = ""
		}
		// Keep only the final dotted segment as the entity name.
		if i := strings.LastIndexByte(handler, '.'); i >= 0 {
			handler = handler[i+1:]
		}

		refKind := "SCOPE.Operation"
		if handler == "" {
			refKind = inlineHandlerRefKind
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
			emit(verb, canonical, "starlette", refKind, handler, defLine)
		}
	}
}

// synthesizeStarletteAddRoute extracts programmatic Starlette route
// registration via `app.add_route('/x', handler)` / `app.add_route('/x',
// handler, methods=['POST'])` (#4383). Handler is the first positional /
// `endpoint=` arg; verb list from `methods=`, default GET.
func synthesizeStarletteAddRoute(content string, emit emitDefFn) {
	if !strings.Contains(content, ".add_route(") {
		return
	}
	for _, idx := range starletteAddRouteRe.FindAllStringSubmatchIndex(content, -1) {
		if len(idx) < 6 {
			continue
		}
		raw := content[idx[2]:idx[3]]
		tail := content[idx[4]:idx[5]]

		// Skip the aiohttp `<recv>.router.add_route("GET", "/path", h)` form —
		// there the FIRST string literal is the VERB, not the path. That shape
		// is handled by synthesizeAiohttp; detect it by a leading quoted-verb
		// second argument (path would then be a string, not an identifier).
		if starletteAddRouteVerbFirst.MatchString(content[idx[0]:idx[1]]) {
			continue
		}

		handlerArg := ""
		if em := fastapiEndpointKwargRe.FindStringSubmatch(tail); len(em) >= 2 {
			handlerArg = em[1]
		} else if pm := pyFirstPositionalArgRe.FindStringSubmatch(tail); len(pm) >= 2 {
			handlerArg = pm[1]
		}

		// #4767 — a Starlette `add_route` handler is ALWAYS a callable: an
		// identifier (`add_route('/x', handler)`) or a lambda. It is NEVER a
		// string literal. Pyramid's `config.add_route("name", "/path")` is a
		// two-STRING-arg call (route name + URL path) that matches this same regex
		// but is NOT a Starlette route — synthesizePyramid owns it (pairing the
		// route name with its @view_config handler). Without this guard the
		// pyramid call was mis-synthesized here as an inline-handler endpoint on
		// the URL path string, colliding on the (verb,path) synthetic ID with the
		// real Pyramid endpoint and stealing its (correct, handler-attributed)
		// source line. Skip when the first positional handler arg is a string
		// literal — that is never a Starlette handler.
		if strings.HasPrefix(handlerArg, `"`) || strings.HasPrefix(handlerArg, `'`) {
			continue
		}

		refKind, refName := pyProgrammaticHandlerRef(handlerArg)
		canonical := httproutes.Canonicalize(httproutes.FrameworkStarlette, raw)

		methods := parseStarletteMethods(tail)
		if len(methods) == 0 {
			methods = []string{"GET"}
		}
		defLine := 0
		if refName != "" {
			defLine = findPyDefLine(content, refName)
		}
		if defLine == 0 {
			defLine = lineOfOffset(content, idx[0])
		}
		for _, verb := range methods {
			emit(verb, canonical, "starlette", refKind, refName, defLine)
		}
	}
}

// starletteAddRouteVerbFirst matches the aiohttp-style
// `.add_route("GET", "/path", ...)` shape (a quoted HTTP verb as the FIRST
// argument), which synthesizeAiohttp owns — used to skip it here.
var starletteAddRouteVerbFirst = regexp.MustCompile(
	`\.add_route\s*\(\s*["'](?i:GET|POST|PUT|PATCH|DELETE|HEAD|OPTIONS)["']\s*,\s*["']`,
)

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
// CherryPy (Python) — #3065
// ---------------------------------------------------------------------------
//
// CherryPy exposes handlers via the `@cherrypy.expose` decorator (or
// `@cp.expose` for a renamed import). The URL is derived from the class and
// method hierarchy rather than an explicit path string; the most common
// conventions are:
//
//	class Root:
//	    @cherrypy.expose
//	    def index(self):       # → GET /
//	        return "hello"
//
//	    @cherrypy.expose
//	    def users(self):       # → GET /users
//	        return []
//
// CherryPy also supports `@cherrypy.expose(['GET', 'POST'])` and the
// tools-based `@cherrypy.tools.json_in()` layering, but the dominant
// convention is the bare `@cherrypy.expose` decorator.
//
// Because CherryPy does not carry an explicit path string in the decorator
// we derive the route from the method name: `index` maps to the parent path
// segment (bare class root), and every other method name becomes a path
// segment. The class hierarchy is not walked — same-class methods emit the
// method name directly. This is a deterministic, partial mapping; the
// byPath linker normalises trailing slashes.
//
// Path parameters are not expressible in the decorator itself (CherryPy
// passes URL-tail segments as positional args to the handler); we leave the
// path as a plain segment with no `{param}` substitution. Handler attribution
// uses the method name.

// cherrypyExposeRe captures `@cherrypy.expose` or `@cp.expose` (common import
// alias) followed by the method definition. Both bare and called forms are
// matched: `@cherrypy.expose` and `@cherrypy.expose()`.
//
// Capture groups: 1 = method name.
var cherrypyExposeRe = regexp.MustCompile(
	`(?m)^[ \t]*@(?:cherrypy|cp)\.expose(?:\([^)]*\))?[ \t]*[\r\n]+(?:\s*@[^\n\r]*[\r\n]+)*\s*def\s+(\w+)\s*\(`,
)

func synthesizeCherryPy(content string, emit emitDefFn) {
	// File-signal gate: require a CherryPy marker.
	if !strings.Contains(content, "cherrypy") && !strings.Contains(content, "CherryPy") {
		return
	}
	for _, idx := range cherrypyExposeRe.FindAllStringSubmatchIndex(content, -1) {
		if len(idx) < 4 {
			continue
		}
		methodName := content[idx[2]:idx[3]]
		defLine := lineOfOffset(content, idx[2])
		// CherryPy's `index` method maps to the bare parent path `/`.
		path := "/" + methodName
		if methodName == "index" {
			path = "/"
		}
		canonical := httproutes.Canonicalize(httproutes.FrameworkCherryPy, path)
		// CherryPy dispatches all verbs to the same handler by default
		// (explicit verb restriction requires tools.allow). Emit ANY.
		emit("ANY", canonical, "cherrypy", "Controller", methodName, defLine)
	}
}

// ---------------------------------------------------------------------------
// Falcon (Python) — #3065
// ---------------------------------------------------------------------------
//
// Falcon registers routes by pairing a URL template with a Resource class:
//
//	app = falcon.App()
//	app.add_route('/users/{user_id}', UserResource())
//
// HTTP verbs are handled by methods named `on_get`, `on_post`, `on_put`,
// `on_patch`, `on_delete`, etc. on the Resource class. The synthesizer
// extracts:
//
//  1. `add_route('/path', Resource())` calls — path + resource class name.
//  2. `on_<verb>` methods on classes defined in the same file — for handler
//     attribution and def-line stamping.
//
// If the Resource class is not found in the same file we emit a single ANY
// synthetic so the cross-file resolver can rebind later.

// falconAddRouteRe captures `<recv>.add_route('/path', ResourceClass(...))`.
// The receiver is any identifier. The resource may be a bare class name or a
// call expression; we capture the first identifier after the comma.
//
// Capture groups: 1 = path, 2 = resource class name.
var falconAddRouteRe = regexp.MustCompile(
	`\b\w+\.add_route\s*\(\s*["']([^"'\n\r]+)["']\s*,\s*([A-Za-z_][\w]*)`,
)

// falconOnVerbRe captures `def on_<verb>(self, ...)` inside a class body.
//
// Capture groups: 1 = verb (lowercase), 2 = method def offset (via index).
var falconOnVerbRe = regexp.MustCompile(
	`(?m)^[ \t]+def\s+on_(get|post|put|patch|delete|head|options)\s*\(`,
)

// falconClassRe matches a class declaration that inherits from anything or
// has no base. We accept any class that has `on_get`/`on_post` etc. methods.
//
// Capture groups: 1 = class name.
var falconClassRe = regexp.MustCompile(
	`(?m)^class\s+([A-Za-z_][\w]*)\s*(?:\([^)]*\))?\s*:`,
)

func synthesizeFalcon(content string, emit emitDefFn) {
	// File-signal gate: require a Falcon marker.
	if !strings.Contains(content, "falcon") && !strings.Contains(content, "Falcon") {
		return
	}

	// Build a same-file class index: ClassName → {verbs → defLine}.
	type classVerbInfo struct {
		verbs    []string
		defLines map[string]int // upper-case verb → 1-based def line
	}
	classes := map[string]*classVerbInfo{}
	for _, cm := range falconClassRe.FindAllStringSubmatchIndex(content, -1) {
		if len(cm) < 4 {
			continue
		}
		name := content[cm[2]:cm[3]]
		bodyStart := cm[1]
		bodyEnd := findPyClassBodyEnd(content, bodyStart)
		body := content[bodyStart:bodyEnd]
		info := &classVerbInfo{defLines: map[string]int{}}
		for _, vm := range falconOnVerbRe.FindAllStringSubmatchIndex(body, -1) {
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

	for _, m := range falconAddRouteRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 6 {
			continue
		}
		raw := content[m[2]:m[3]]
		resourceClass := content[m[4]:m[5]]
		canonical := httproutes.Canonicalize(httproutes.FrameworkFalcon, raw)
		if canonical == "" {
			continue
		}

		info, sameFile := classes[resourceClass]
		if sameFile && len(info.verbs) > 0 {
			for _, verb := range info.verbs {
				methodName := resourceClass + ".on_" + strings.ToLower(verb)
				emit(verb, canonical, "falcon", "SCOPE.Operation", methodName, info.defLines[verb])
			}
			continue
		}
		// Cross-file or no verb methods found: emit ANY with class reference.
		emit("ANY", canonical, "falcon", "SCOPE.Component", resourceClass, lineOfOffset(content, m[0]))
	}
}

// ---------------------------------------------------------------------------
// Hug (Python) — #3065
// ---------------------------------------------------------------------------
//
// Hug is a decorator-driven framework that wraps any WSGI/WSGI2 app. Routes
// are declared with `@hug.get`, `@hug.post`, `@hug.put`, `@hug.patch`,
// `@hug.delete`, `@hug.options`, `@hug.head`, `@hug.cli`, `@hug.local`:
//
//	@hug.get('/users/{user_id}')
//	def get_user(user_id: int): ...
//
//	@hug.post('/items')
//	def create_item(body): ...
//
// Path parameters use FastAPI-style `{name}` curly-brace convention.
// Hug also supports `@hug.get()` (no path) — we skip those since there is
// no path to extract.

// hugVerbDecoratorRe captures `@hug.<verb>('/path')` / `@hug.<verb>("/path")`
// decorators and the following handler def/async def.
//
// Capture groups: 1 = verb, 2 = path, 3 = handler name.
var hugVerbDecoratorRe = regexp.MustCompile(
	`@hug\.(get|post|put|patch|delete|head|options)\s*\(\s*["']([^"'\n\r]+)["'][^)]*\)[ \t]*[\r\n]+(?:\s*@[^\n\r]*[\r\n]+)*\s*(?:async\s+)?def\s+(\w+)`,
)

func synthesizeHug(content string, emit emitDefFn) {
	// File-signal gate: require a hug marker.
	if !strings.Contains(content, "hug") {
		return
	}
	for _, idx := range hugVerbDecoratorRe.FindAllStringSubmatchIndex(content, -1) {
		if len(idx) < 8 {
			continue
		}
		verb := strings.ToUpper(content[idx[2]:idx[3]])
		raw := content[idx[4]:idx[5]]
		handler := content[idx[6]:idx[7]]
		canonical := httproutes.Canonicalize(httproutes.FrameworkHug, raw)
		defLine := lineOfOffset(content, idx[6])
		emit(verb, canonical, "hug", "Controller", handler, defLine)
	}
}

// ---------------------------------------------------------------------------
// Quart (Python) — #3065
// ---------------------------------------------------------------------------
//
// Quart is an async-first Python web framework that mirrors the Flask API
// exactly. Routes are declared with `@app.route('/path')` and shorthand
// method decorators `@app.get('/path')`, `@app.post('/path')`, etc. Path
// parameters use the same Flask-style `<converter:name>` / `<name>`
// angle-bracket convention.
//
// Because the decorator shapes are identical to Flask — only the import
// differs (`from quart import Quart` / `import quart`) — the synthesizer is
// gated on a Quart-specific marker. The side-scoped dedup in makeEmit ensures
// the Flask synthesizer (which runs after) does not re-claim Quart endpoints
// with the wrong framework label.

// quartVerbDecoratorRe captures `@app.<verb>("/path")` shorthand decorators
// and the following handler def/async def. Mirrors flaskRouteVerbDecoratorRe.
//
// Capture groups: 1 = verb, 2 = path, 3 = handler name.
var quartVerbDecoratorRe = regexp.MustCompile(
	`@\w+\.(get|post|put|patch|delete)\s*\(\s*["']([^"'\n\r]+)["'][^\n\r]*\)[ \t]*[\r\n]+(?:\s*@[^\n\r]*[\r\n]+)*\s*(?:async\s+)?def\s+(\w+)`,
)

// quartRouteRe captures the generic `@app.route("/path", ...)` form and the
// following handler def/async def. Mirrors flaskRouteRe with optional
// `async def`.
//
// Capture groups: 1 = path, 2 = kwargs tail, 3 = handler name.
var quartRouteRe = regexp.MustCompile(
	`@\w+\.route\s*\(\s*["']([^"'\n\r]+)["']([^\n\r]*)\)[ \t]*[\r\n]+(?:\s*@[^\n\r]*[\r\n]+)*\s*(?:async\s+)?def\s+(\w+)`,
)

func synthesizeQuart(content string, emit emitDefFn) {
	// File-signal gate: require a Quart marker. The decorator shapes are
	// identical to Flask so without this gate the synthesizer would fire on
	// all Flask files too.
	if !strings.Contains(content, "quart") && !strings.Contains(content, "Quart") {
		return
	}
	// Shorthand verbs first.
	for _, idx := range quartVerbDecoratorRe.FindAllStringSubmatchIndex(content, -1) {
		if len(idx) < 8 {
			continue
		}
		verb := strings.ToUpper(content[idx[2]:idx[3]])
		raw := content[idx[4]:idx[5]]
		handler := content[idx[6]:idx[7]]
		canonical := httproutes.Canonicalize(httproutes.FrameworkQuart, raw)
		defLine := lineOfOffset(content, idx[6])
		emit(verb, canonical, "quart", "Controller", handler, defLine)
	}
	// Generic .route(...) form.
	for _, idx := range quartRouteRe.FindAllStringSubmatchIndex(content, -1) {
		if len(idx) < 8 {
			continue
		}
		raw := content[idx[2]:idx[3]]
		extras := content[idx[4]:idx[5]]
		handler := content[idx[6]:idx[7]]
		canonical := httproutes.Canonicalize(httproutes.FrameworkQuart, raw)
		defLine := lineOfOffset(content, idx[6])
		methods := parseFlaskMethods(extras) // Quart uses the same methods=[...] shape
		if len(methods) == 0 {
			methods = []string{"GET"}
		}
		for _, verb := range methods {
			emit(verb, canonical, "quart", "Controller", handler, defLine)
		}
	}
}

// ---------------------------------------------------------------------------
// Strawberry GraphQL (Python) — #3066
// ---------------------------------------------------------------------------
//
// Strawberry is a code-first GraphQL library for Python. Schemas are defined
// by decorating Python classes with @strawberry.type and individual resolver
// methods with @strawberry.field (or leaving them undecorated — any public
// method in a @strawberry.type class is a potential resolver field).
//
// The three GraphQL root types are:
//
//	@strawberry.type
//	class Query:
//	    def users(self) -> list[User]: ...
//
//	@strawberry.type
//	class Mutation:
//	    @strawberry.mutation
//	    def create_user(self, name: str) -> User: ...
//
//	@strawberry.type
//	class Subscription:
//	    @strawberry.subscription
//	    async def user_added(self) -> typing.AsyncGenerator[User, None]: ...
//
// We map each method on a root type to:
//
//	http:GRAPHQL:/graphql/<RootType>/<fieldName>
//
// Handler attribution: the resolver method name is the handler (as
// SCOPE.Operation:<ClassName>.<method>).
//
// Detection is gated on the presence of "strawberry" in the file so the
// synthesizer is a no-op on plain Flask/FastAPI/etc. files.

// strawberryRootTypeRe matches a `@strawberry.type` (or @strawberry.mutation /
// @strawberry.subscription) decorator immediately preceding a class named
// Query, Mutation, or Subscription.
//
// Capture groups:
//
//	1 = root type name (Query | Mutation | Subscription)
var strawberryRootTypeRe = regexp.MustCompile(
	`(?m)@strawberry\.(?:type|mutation|subscription)\s*\n(?:[ \t]*@[^\n]+\n)*[ \t]*class\s+(Query|Mutation|Subscription)\s*[:(]`,
)

// strawberryMethodRe matches a public method inside a class body. It matches
// `def <name>(self` and optionally `async def <name>(self`.
//
// Capture groups:
//
//	1 = method name
var strawberryMethodRe = regexp.MustCompile(
	`(?m)^[ \t]+(?:async\s+)?def\s+([A-Za-z_]\w*)\s*\(\s*self`,
)

func synthesizeStrawberry(content string, emit emitDefFn) {
	// File-signal gate: require a strawberry marker.
	if !strings.Contains(content, "strawberry") {
		return
	}

	// Find all @strawberry.type class declarations for the three root types.
	for _, rm := range strawberryRootTypeRe.FindAllStringSubmatchIndex(content, -1) {
		if len(rm) < 4 {
			continue
		}
		rootType := content[rm[2]:rm[3]] // "Query" | "Mutation" | "Subscription"

		// Locate the class body by finding the colon that ends the class
		// declaration line and scanning forward to find the indented block.
		// We use a simplified approach: find all method defs in the class body
		// by scanning from the class header until the next unindented line
		// (i.e., the next `class` / top-level statement).
		classStart := rm[1] // byte offset right after the class header match
		// Find the end of the class body: next line that starts with a
		// non-whitespace character (top-level), or end of file.
		classEnd := len(content)
		// Scan from classStart for the next occurrence of a line that begins
		// with a non-space, non-tab character and is not a blank line or
		// comment. We walk line by line.
		searchIn := content[classStart:]
		lines := strings.Split(searchIn, "\n")
		bodyEnd := len(searchIn)
		for i, line := range lines {
			if i == 0 {
				// The first "line" is the tail of the class header — skip it.
				continue
			}
			if len(line) == 0 {
				continue // blank line — still inside the class
			}
			if line[0] != ' ' && line[0] != '\t' {
				// Top-level line — class body ends here.
				bodyEnd = 0
				for j := 0; j < i; j++ {
					bodyEnd += len(lines[j]) + 1 // +1 for '\n'
				}
				break
			}
		}
		classBody := content[classStart : classStart+bodyEnd]
		classEnd = classStart + bodyEnd
		_ = classEnd

		// Extract each public method in the class body.
		seen := map[string]bool{}
		for _, mm := range strawberryMethodRe.FindAllStringSubmatchIndex(classBody, -1) {
			if len(mm) < 4 {
				continue
			}
			methodName := classBody[mm[2]:mm[3]]
			// Skip dunder methods and already-seen names.
			if strings.HasPrefix(methodName, "_") || seen[methodName] {
				continue
			}
			seen[methodName] = true

			// Compute the absolute def-line in the file (not just the class body).
			methodOffsetInFile := classStart + mm[2]
			defLine := lineOfOffset(content, methodOffsetInFile)

			path := "/graphql/" + rootType + "/" + methodName
			canonical := httproutes.Canonicalize(httproutes.FrameworkFastAPI, path)
			handlerRef := rootType + "." + methodName
			emit("GRAPHQL", canonical, "strawberry-graphql", "SCOPE.Operation", handlerRef, defLine)
		}
	}
}

// ---------------------------------------------------------------------------
// Graphene (Python) — #3620 (epic #3607)
// ---------------------------------------------------------------------------
//
// Graphene is a code-first GraphQL library for Python. Root types subclass
// graphene.ObjectType and declare fields as class attributes; the resolver for
// a field `users` is the method `resolve_users`:
//
//	class Query(graphene.ObjectType):
//	    users = graphene.List(User)
//	    me = graphene.Field(User)
//	    def resolve_users(self, info):
//	        ...
//	    def resolve_me(self, info):
//	        ...
//
//	class Mutation(graphene.ObjectType):
//	    create_user = CreateUser.Field()
//	    def resolve_create_user(self, info, name):
//	        ...
//
// The schema is wired with `graphene.Schema(query=Query, mutation=Mutation,
// subscription=Subscription)`. We map each root-type field to the SAME
// operation-endpoint shape emitted by Strawberry / gqlgen / the JS/TS GraphQL
// server:
//
//	http:GRAPHQL:/graphql/<RootType>/<field>
//
// where RootType is Query / Mutation / Subscription and <field> is the snake_case
// field name (Graphene/Python convention — no name mangling, the attribute name
// IS the GraphQL field name). Emitting the identical id shape is what lets the
// GraphQL client-link synthesizer and the cross-repo linker join Python Graphene
// servers to their consumers.
//
// Field discovery: we enumerate `resolve_<field>` methods inside the root-type
// class body. The resolver method is the authoritative field signal (it carries
// the def line for handler attribution); a `<field> = graphene.<X>(...)` class
// attribute without a matching resolver uses Graphene's default resolver and is
// also emitted (honest-partial — no explicit handler, so source_handler points
// at the would-be resolver name). The resolver method, when present, is the
// handler, referenced as `SCOPE.Operation:<ClassName>.resolve_<field>`.
//
// Detection is gated on the presence of "graphene" in the file so the
// synthesizer is a no-op on every other Python file.

// grapheneRootTypeRe matches a class declaration whose base class list contains
// a graphene.ObjectType (or bare ObjectType) base, for one of the three root
// type names. The class name must be Query, Mutation, or Subscription.
//
// Capture groups:
//
//	1 = root type name (Query | Mutation | Subscription)
var grapheneRootTypeRe = regexp.MustCompile(
	`(?m)^[ \t]*class\s+(Query|Mutation|Subscription)\s*\(([^)]*)\)\s*:`,
)

// grapheneResolverRe matches a `def resolve_<field>(self` (optionally async)
// method inside a root-type class body, capturing the field name (the suffix
// after `resolve_`).
//
// Capture groups:
//
//	1 = field name (snake_case GraphQL field)
var grapheneResolverRe = regexp.MustCompile(
	`(?m)^[ \t]+(?:async\s+)?def\s+resolve_([A-Za-z_]\w*)\s*\(\s*self`,
)

// grapheneFieldAttrRe matches a `<field> = graphene.<Type>(...)` class-attribute
// field declaration inside a root-type class body, capturing the field name.
// This covers fields that rely on Graphene's default resolver (no explicit
// resolve_<field> method).
//
// Capture groups:
//
//	1 = field name
var grapheneFieldAttrRe = regexp.MustCompile(
	`(?m)^[ \t]+([A-Za-z_]\w*)\s*=\s*graphene\.`,
)

func synthesizeGraphene(content string, emit emitDefFn) {
	// File-signal gate: require a graphene marker so this no-ops on every other
	// Python file (including Strawberry / Ariadne GraphQL servers).
	if !strings.Contains(content, "graphene") {
		return
	}

	for _, rm := range grapheneRootTypeRe.FindAllStringSubmatchIndex(content, -1) {
		if len(rm) < 6 {
			continue
		}
		rootType := content[rm[2]:rm[3]] // "Query" | "Mutation" | "Subscription"
		bases := content[rm[4]:rm[5]]
		// Require an ObjectType base so we don't fire on unrelated classes that
		// happen to be named Query/Mutation/Subscription.
		if !strings.Contains(bases, "ObjectType") {
			continue
		}

		classStart := rm[1] // byte offset right after the class header match
		bodyEnd := grapheneClassBodyEnd(content[classStart:])
		classBody := content[classStart : classStart+bodyEnd]

		seen := map[string]bool{}

		// Resolver methods are the authoritative, handler-bearing signal. Emit
		// them first so the attribute pass below can dedup against them.
		for _, mm := range grapheneResolverRe.FindAllStringSubmatchIndex(classBody, -1) {
			if len(mm) < 4 {
				continue
			}
			field := classBody[mm[2]:mm[3]]
			if field == "" || seen[field] {
				continue
			}
			seen[field] = true

			methodOffsetInFile := classStart + mm[2]
			defLine := lineOfOffset(content, methodOffsetInFile)

			path := "/graphql/" + rootType + "/" + field
			canonical := httproutes.Canonicalize(httproutes.FrameworkFastAPI, path)
			handlerRef := rootType + ".resolve_" + field
			emit("GRAPHQL", canonical, "graphene", "SCOPE.Operation", handlerRef, defLine)
		}

		// Class-attribute fields without an explicit resolver use Graphene's
		// default resolver. Emit them too (honest-partial: source_handler points
		// at the conventional resolver name even though no def exists).
		for _, mm := range grapheneFieldAttrRe.FindAllStringSubmatchIndex(classBody, -1) {
			if len(mm) < 4 {
				continue
			}
			field := classBody[mm[2]:mm[3]]
			if field == "" || seen[field] {
				continue
			}
			seen[field] = true

			attrOffsetInFile := classStart + mm[2]
			defLine := lineOfOffset(content, attrOffsetInFile)

			path := "/graphql/" + rootType + "/" + field
			canonical := httproutes.Canonicalize(httproutes.FrameworkFastAPI, path)
			handlerRef := rootType + ".resolve_" + field
			emit("GRAPHQL", canonical, "graphene", "SCOPE.Operation", handlerRef, defLine)
		}
	}
}

// grapheneClassBodyEnd returns the byte length of the class body that begins at
// the start of searchIn (the text immediately after a class header match),
// walking line by line until the first top-level (column-0, non-blank) line.
func grapheneClassBodyEnd(searchIn string) int {
	lines := strings.Split(searchIn, "\n")
	bodyEnd := len(searchIn)
	for i, line := range lines {
		if i == 0 {
			// Tail of the class header line — skip.
			continue
		}
		if len(line) == 0 {
			continue // blank line — still inside the class
		}
		if line[0] != ' ' && line[0] != '\t' {
			bodyEnd = 0
			for j := 0; j < i; j++ {
				bodyEnd += len(lines[j]) + 1 // +1 for '\n'
			}
			break
		}
	}
	return bodyEnd
}

// ---------------------------------------------------------------------------
// Ariadne (Python) — #3620 (epic #3607)
// ---------------------------------------------------------------------------
//
// Ariadne is a schema-first GraphQL library for Python. The SDL is defined as a
// string and resolvers are bound to fields imperatively via a `QueryType()` /
// `MutationType()` / `ObjectType("Query")` binder object whose `.field("<name>")`
// decorator registers the resolver function:
//
//	query = QueryType()
//
//	@query.field("me")
//	def resolve_me(_, info):
//	    ...
//
//	@query.field("users")
//	def resolve_users(_, info):
//	    ...
//
//	mutation = MutationType()
//
//	@mutation.field("create_user")
//	def resolve_create_user(_, info, name):
//	    ...
//
// ObjectType binders can also target an arbitrary type by name
// (`ObjectType("Query")`); we resolve the binder variable → root type by
// inspecting its constructor. We map each bound field to the SAME
// operation-endpoint shape as Strawberry / Graphene / gqlgen:
//
//	http:GRAPHQL:/graphql/<RootType>/<field>
//
// Handler attribution: the decorated function immediately below the
// `@<binder>.field("<name>")` decorator is the resolver, referenced as
// `SCOPE.Operation:<funcName>`.
//
// Detection is gated on the presence of "ariadne" OR a QueryType/MutationType/
// SubscriptionType/ObjectType binder construction so the synthesizer is a no-op
// on every other Python file.

// ariadneBinderCtorRe matches a binder construction, capturing the variable name
// and the binder kind so we can resolve `@<var>.field(...)` decorators to a root
// type. Handles `query = QueryType()` and `query = ObjectType("Query")`.
//
// Capture groups:
//
//	1 = binder variable name
//	2 = binder constructor (QueryType | MutationType | SubscriptionType | ObjectType)
//	3 = ObjectType type-name argument (only set for ObjectType("Query"))
var ariadneBinderCtorRe = regexp.MustCompile(
	`(?m)^[ \t]*([A-Za-z_]\w*)\s*=\s*(QueryType|MutationType|SubscriptionType|ObjectType)\s*\(\s*(?:["']([A-Za-z_]\w*)["'])?\s*\)`,
)

// ariadneFieldDecoratorRe matches a `@<binder>.field("<name>")` decorator
// immediately followed by a (optionally async) `def <func>(`. It captures the
// binder variable, the field name, and the resolver function name.
//
// Capture groups:
//
//	1 = binder variable name
//	2 = field name
//	3 = resolver function name
var ariadneFieldDecoratorRe = regexp.MustCompile(
	`(?m)^[ \t]*@([A-Za-z_]\w*)\.field\(\s*["']([A-Za-z_]\w*)["']\s*\)\s*\n[ \t]*(?:async\s+)?def\s+([A-Za-z_]\w*)\s*\(`,
)

// ariadneCtorToRoot maps an Ariadne binder constructor to its GraphQL root type.
var ariadneCtorToRoot = map[string]string{
	"QueryType":        "Query",
	"MutationType":     "Mutation",
	"SubscriptionType": "Subscription",
}

func synthesizeAriadne(content string, emit emitDefFn) {
	// File-signal gate: require an ariadne marker or a binder construction so
	// this no-ops on every other Python file.
	if !strings.Contains(content, "ariadne") &&
		!strings.Contains(content, "QueryType") &&
		!strings.Contains(content, "MutationType") &&
		!strings.Contains(content, "SubscriptionType") {
		return
	}

	// Resolve each binder variable → root type.
	binderRoot := map[string]string{}
	for _, m := range ariadneBinderCtorRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 6 {
			continue
		}
		varName := content[m[2]:m[3]]
		ctor := content[m[4]:m[5]]
		if root, ok := ariadneCtorToRoot[ctor]; ok {
			binderRoot[varName] = root
			continue
		}
		// ObjectType("Query") — root type is the string argument.
		if m[6] >= 0 && m[7] > m[6] {
			binderRoot[varName] = content[m[6]:m[7]]
		}
	}

	seen := map[string]bool{}
	for _, m := range ariadneFieldDecoratorRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 8 {
			continue
		}
		binder := content[m[2]:m[3]]
		field := content[m[4]:m[5]]
		funcName := content[m[6]:m[7]]
		root, ok := binderRoot[binder]
		if !ok || root == "" || field == "" {
			continue
		}
		dedupKey := root + "." + field
		if seen[dedupKey] {
			continue
		}
		seen[dedupKey] = true

		defLine := lineOfOffset(content, m[0])
		path := "/graphql/" + root + "/" + field
		canonical := httproutes.Canonicalize(httproutes.FrameworkFastAPI, path)
		// Handler ref points at the decorated resolver function symbol.
		emit("GRAPHQL", canonical, "ariadne", "SCOPE.Operation", funcName, defLine)
	}
}

// ---------------------------------------------------------------------------
// gqlgen (Go) — #3613 (epic #3607)
// ---------------------------------------------------------------------------
//
// gqlgen is the dominant schema-first GraphQL server for Go
// (github.com/99designs/gqlgen). The SDL schema (`type Query { users: [User!]! }`,
// in `*.graphqls`) is compiled to a generated `Resolver` root whose root-type
// fields are implemented by user-edited Go methods on the generated receiver
// types `*queryResolver`, `*mutationResolver`, and `*subscriptionResolver`
// (canonically in `graph/schema.resolvers.go`):
//
//	func (r *queryResolver) Users(ctx context.Context) ([]*model.User, error) { ... }
//	func (r *mutationResolver) CreateUser(ctx context.Context, input model.NewUser) (*model.User, error) { ... }
//	func (r *subscriptionResolver) UserAdded(ctx context.Context) (<-chan *model.User, error) { ... }
//
// gqlgen derives the GraphQL field name from the resolver method by lower-casing
// the leading run of the exported Go method name (Go's `Users` ↔ schema field
// `users`, `CreateUser` ↔ `createUser`, `URL` ↔ `url`). We map each resolver
// method to the SAME operation-endpoint shape emitted by the JS/TS GraphQL
// server (synthesizeGraphQLResolvers) and the Python Strawberry server:
//
//	http:GRAPHQL:/graphql/<RootType>/<field>
//
// where RootType is Query / Mutation / Subscription. Emitting the identical id
// shape is what lets the GraphQL client-link synthesizer (#3667) and the
// cross-repo linker join Go GraphQL servers to their consumers.
//
// Handler attribution: the resolver method is the handler, referenced as
// `SCOPE.Operation:<receiverType>.<Method>` (e.g.
// `SCOPE.Operation:queryResolver.Users`). The Phase-2 resolver rebinds this
// `source_handler` ref into a HANDLES edge to the Go method symbol.
//
// Detection is gated on a gqlgen file-signal (a `github.com/99designs/gqlgen`
// import or one of the generated receiver types) so the synthesizer is a no-op
// on every other Go file.

// gqlgenResolverMethodRe matches a Go method declared on one of gqlgen's
// generated root resolver receiver types, capturing the receiver type and the
// exported method name.
//
// Capture groups:
//
//	1 = receiver type (queryResolver | mutationResolver | subscriptionResolver)
//	2 = method (exported field implementation, e.g. Users / CreateUser)
var gqlgenResolverMethodRe = regexp.MustCompile(
	`(?m)^func\s*\(\s*\w+\s+\*?(queryResolver|mutationResolver|subscriptionResolver)\s*\)\s+([A-Z]\w*)\s*\(`,
)

// gqlgenReceiverToRoot maps a gqlgen generated receiver type to its GraphQL
// root operation type.
var gqlgenReceiverToRoot = map[string]string{
	"queryResolver":        "Query",
	"mutationResolver":     "Mutation",
	"subscriptionResolver": "Subscription",
}

// gqlgenFieldName lower-cases the leading run of capitals of an exported Go
// method name to recover the GraphQL field name, matching gqlgen's default
// name-mapping (`Users` → `users`, `CreateUser` → `createUser`, `URL` → `url`,
// `ID` → `id`). A single leading capital lower-cases just that letter; a run of
// N>1 capitals lower-cases all but the last when the last is followed by a
// lowercase (Go's standard initialism handling), otherwise the whole run.
func gqlgenFieldName(method string) string {
	if method == "" {
		return method
	}
	r := []rune(method)
	// Count the leading run of upper-case letters.
	n := 0
	for n < len(r) && r[n] >= 'A' && r[n] <= 'Z' {
		n++
	}
	if n <= 1 {
		// Simple case: lower-case the single leading capital.
		r[0] = r[0] - 'A' + 'a'
		return string(r)
	}
	// Initialism run (e.g. URL, ID). If the run is the whole identifier
	// (URL → url) lower-case all of it; otherwise the final capital starts
	// the next word (HTTPServer → httpServer) so keep it upper.
	end := n
	if n < len(r) {
		end = n - 1
	}
	for i := 0; i < end; i++ {
		r[i] = r[i] - 'A' + 'a'
	}
	return string(r)
}

func synthesizeGqlgen(content string, emit emitDefFn) {
	// File-signal gate: require a gqlgen marker so this no-ops on every other
	// Go file. The generated receiver types are gqlgen-specific; the import is
	// the canonical project signal.
	if !strings.Contains(content, "github.com/99designs/gqlgen") &&
		!strings.Contains(content, "queryResolver") &&
		!strings.Contains(content, "mutationResolver") &&
		!strings.Contains(content, "subscriptionResolver") {
		return
	}

	seen := map[string]bool{}
	for _, m := range gqlgenResolverMethodRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 6 {
			continue
		}
		receiver := content[m[2]:m[3]]
		method := content[m[4]:m[5]]
		root, ok := gqlgenReceiverToRoot[receiver]
		if !ok {
			continue
		}
		field := gqlgenFieldName(method)
		if field == "" {
			continue
		}
		dedupKey := root + "." + field
		if seen[dedupKey] {
			continue
		}
		seen[dedupKey] = true

		defLine := lineOfOffset(content, m[0])
		path := "/graphql/" + root + "/" + field
		canonical := httproutes.Canonicalize(httproutes.FrameworkGqlgen, path)
		// Handler ref points at the resolver method symbol. The Phase-2
		// resolver rebinds `SCOPE.Operation:<receiver>.<Method>` into a HANDLES
		// edge against the extracted Go method entity.
		handlerRef := receiver + "." + method
		emit("GRAPHQL", canonical, "gqlgen", "SCOPE.Operation", handlerRef, defLine)
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
		refKind := "Controller"
		if isInlineExpressHandler(m[0], raw) {
			// #4324 — anonymous/inline arrow or function-expression handler:
			// no addressable symbol. Signal InlineHandler so makeEmit
			// synthesizes a stable handler node + bridge instead of leaving
			// the endpoint a handler-less island.
			handler = ""
			refKind = inlineHandlerRefKind
		}
		canonical := httproutes.Canonicalize(httproutes.FrameworkExpress, composeExpressMount(mountPrefixes, receiver, raw))
		// Express `.all(...)` registers every verb on the path; emit as ANY.
		if verb == "ALL" {
			verb = "ANY"
		}
		key := verb + ":" + canonical
		withHandler[key] = true
		emit(verb, canonical, "express", refKind, handler)
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
		// #4324 — the handler-named pass did not claim this (verb,path), which
		// means the handler is an inline arrow / function-expression whose body
		// the handler-named regex couldn't span (e.g. a multi-line block). A
		// route ALWAYS has a handler argument, so model it as an inline handler
		// rather than emitting a handler-less island.
		emit(verb, canonical, "express", inlineHandlerRefKind, "")
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
// synthesizers. It is multi-controller aware: a file with several @Controller
// classes attributes each HTTP-verb method to the base path of the nearest
// PRECEDING @Controller (by source position), so two controllers in one file
// keep their own prefixes instead of collapsing onto the first.

// nestControllerOpenRe locates the start of a class-level @Controller decorator
// — `@Controller(` — without consuming its argument. The argument is then read
// as a balanced span (nestjsBalancedArg) and parsed by nestjsParseControllerArg
// so EVERY decorator form is supported, not just the bare-string one:
//
//	@Controller()                                       → prefix ""        (root)
//	@Controller('users')                                → prefix "users"
//	@Controller(['users', 'people'])                    → prefix "users"   (first host)
//	@Controller({ path: 'users' })                      → prefix "users"
//	@Controller({ path: 'users', version: '1' })        → prefix "v1/users"
//	@Controller({ path: ['users','people'], version })  → prefix "v1/users"
//
// #4340: the previous regex matched ONLY the bare-string / empty forms; the
// object form `@Controller({ path, version })` (the canonical NestJS style with
// URI versioning) failed to match entirely, so the base path was DROPPED and
// every controller's routes collapsed onto the verb+method-path alone.
var nestControllerOpenRe = regexp.MustCompile(`@Controller\s*\(`)

// nestjsFirstQuotedRe captures the FIRST quoted string in a span (single,
// double, or backtick). Used to read the bare-string form and the first host
// of an array form.
var nestjsFirstQuotedRe = regexp.MustCompile("['\"`]([^'\"`\\r\\n]*)['\"`]")

// nestjsPathPropRe captures the `path:` value of an object-form @Controller
// argument when it is a single quoted string: `path: 'users'`.
var nestjsPathPropRe = regexp.MustCompile(`\bpath\s*:\s*['"` + "`" + `]([^'"` + "`" + `\r\n]*)['"` + "`" + `]`)

// nestjsPathArrayPropRe captures the `path:` value when it is an array literal,
// so the first host can be read: `path: ['users', 'people']`.
var nestjsPathArrayPropRe = regexp.MustCompile(`\bpath\s*:\s*\[([^\]]*)\]`)

// nestjsVersionPropRe captures the `version:` value of an object-form
// @Controller argument when it is a quoted string or bare number:
// `version: '1'`, `version: "2"`, `version: 1`. NestJS also supports
// VERSION_NEUTRAL and arrays of versions; those are treated as "no single URI
// prefix" (handled by the caller leaving the version empty).
var nestjsVersionPropRe = regexp.MustCompile(`\bversion\s*:\s*(?:['"` + "`" + `]([^'"` + "`" + `\r\n]*)['"` + "`" + `]|(\d+))`)

// nestjsBalancedArg reads the balanced `( ... )` argument span that begins at
// `openParen` (the index of the '(' in src). It returns the inner text (without
// the surrounding parens) and the index just past the closing paren. Brackets
// inside strings are ignored (reuses the same string-aware lexer as
// nestjsBracketDelta). Returns ("", openParen+1) if unbalanced.
func nestjsBalancedArg(src string, openParen int) (string, int) {
	depth := 0
	var quote byte
	for i := openParen; i < len(src); i++ {
		c := src[i]
		if quote != 0 {
			if c == '\\' {
				i++
				continue
			}
			if c == quote {
				quote = 0
			}
			continue
		}
		switch c {
		case '\'', '"', '`':
			quote = c
		case '(', '{', '[':
			depth++
		case ')', '}', ']':
			depth--
			if depth == 0 {
				return src[openParen+1 : i], i + 1
			}
		}
	}
	return "", openParen + 1
}

// nestjsParseControllerArg parses the inner text of a @Controller(...) argument
// (everything between the outer parens) into a route prefix, handling all
// NestJS decorator forms. The returned prefix is NOT slash-normalised — the
// caller joins it via joinPathFragments. Empty string means the root.
//
// Forms:
//   - ""                              → ""            (@Controller())
//   - "'users'"                       → "users"
//   - "['users','people']"            → "users"       (first host wins)
//   - "{ path: 'users' }"             → "users"
//   - "{ path: 'users', version:'1'}" → "v1/users"    (URI versioning)
//   - "{ version: '1' }"              → "v1"          (versioned root controller)
//   - "{ path: ['a','b'], version }"  → "v1/a"
func nestjsParseControllerArg(arg string) string {
	arg = strings.TrimSpace(arg)
	if arg == "" {
		return ""
	}
	// Object form: `{ ... }`.
	if strings.HasPrefix(arg, "{") {
		path := ""
		if m := nestjsPathPropRe.FindStringSubmatch(arg); m != nil {
			path = m[1]
		} else if m := nestjsPathArrayPropRe.FindStringSubmatch(arg); m != nil {
			// path is an array literal — take the first quoted host.
			if q := nestjsFirstQuotedRe.FindStringSubmatch(m[1]); q != nil {
				path = q[1]
			}
		}
		version := ""
		if m := nestjsVersionPropRe.FindStringSubmatch(arg); m != nil {
			if m[1] != "" {
				version = m[1]
			} else {
				version = m[2]
			}
		}
		return nestjsComposePrefix(version, path)
	}
	// Array form: `['users', 'people']` — first host wins.
	if strings.HasPrefix(arg, "[") {
		if q := nestjsFirstQuotedRe.FindStringSubmatch(arg); q != nil {
			return q[1]
		}
		return ""
	}
	// Bare-string form: `'users'`.
	if q := nestjsFirstQuotedRe.FindStringSubmatch(arg); q != nil {
		return q[1]
	}
	return ""
}

// nestjsComposePrefix combines a NestJS URI version and a path into a single
// route prefix. NestJS URI versioning mounts the version as a leading path
// segment with the default `v` prefix (`/v1/...`). An empty version yields just
// the path; an empty path with a version yields the bare version segment.
func nestjsComposePrefix(version, path string) string {
	path = strings.Trim(path, "/")
	if version == "" {
		return path
	}
	verSeg := "v" + strings.TrimPrefix(version, "v")
	if path == "" {
		return verSeg
	}
	return verSeg + "/" + path
}

// nestjsVerbPathRe captures a NestJS HTTP-verb method decorator and its
// OPTIONAL first quoted path argument. It deliberately matches ONLY the verb
// decorator itself — not the handler that follows it. Binding the handler is
// done by a line-oriented forward scan (nestjsFindHandlerName) which is immune
// to parens-in-strings inside intervening decorators (e.g. an @ApiOperation
// description containing "(parity with legacy)"). The previous combined regex
// used `[^)]*` to skip intervening decorator arguments and silently DROPPED any
// route whose preceding decorator args contained a `)` inside a string.
//
// The path argument uses a non-greedy quoted-string capture so a route segment
// is captured even if other (non-string) chars follow inside the parens
// (e.g. `@Get('x', { ... })`). A route path string practically never contains
// an unescaped quote, so the `[^'"`]` body is safe.
//
// Capture groups: 1 = verb, 2 = optional decorator path.
var nestjsVerbPathRe = regexp.MustCompile(
	"@(Get|Post|Put|Delete|Patch|Head|Options|All)\\s*\\(\\s*(?:['\"`]([^'\"`\\r\\n]*)['\"`])?",
)

// nestjsHandlerNameRe matches a method declaration line and captures the method
// name. Used by the line-oriented forward scan to bind a verb decorator to its
// handler. Anchored to the start of a (trimmed) line.
var nestjsHandlerNameRe = regexp.MustCompile(
	"^\\s*(?:public\\s+|private\\s+|protected\\s+|static\\s+|readonly\\s+|abstract\\s+|override\\s+|async\\s+|get\\s+|set\\s+)*" +
		"([A-Za-z_$][\\w$]*)\\s*\\(",
)

// nestjsFindHandlerName scans forward from `fromLine` (a line index into
// `lines`) to find the handler method bound to a verb decorator. It skips
// blank lines, comment lines (`//`, `*`, `/*`, `*/`, `/**`) and decorator lines
// (`@...`) and returns the first line that looks like a method declaration.
// Returns "" if no handler is found before the next verb decorator or EOF.
// The second return value is the 0-based index of the line the handler method
// declaration sits on (-1 when no handler is found), used by #4319 to anchor
// the synthetic at the handler method line for file:line co-location bridging.
//
// This is the parens-in-strings-immune replacement for the old combined regex.
func nestjsFindHandlerName(lines []string, fromLine int) (string, int) {
	// depth tracks unbalanced ()/{}/[] carried over from a multi-line decorator
	// argument (e.g. `@ApiOperation({` … `})` or `@ApiResponse(` … `)`). While
	// depth > 0 we are INSIDE a decorator's argument list — those continuation
	// lines (`summary: '...'`, `description: '...'`) are decorator data, not a
	// handler, so we skip them. Crucially, bracket counting here is line-based
	// but we deliberately keep it simple: a route path / description string with
	// a stray bracket is rare AND, even if it momentarily mis-counts, the worst
	// case is skipping one extra line — never binding a wrong handler, because
	// the handler regex is anchored and specific.
	depth := 0
	for i := fromLine; i < len(lines); i++ {
		raw := lines[i]
		t := strings.TrimSpace(raw)
		if t == "" {
			continue
		}
		// Comment lines (single-line, block open/continuation/close, JSDoc).
		if depth == 0 && (strings.HasPrefix(t, "//") || strings.HasPrefix(t, "*") ||
			strings.HasPrefix(t, "/*") || strings.HasPrefix(t, "*/")) {
			continue
		}
		// If we're inside a multi-line decorator argument, keep consuming until
		// the brackets balance again.
		if depth > 0 {
			depth += nestjsBracketDelta(raw)
			continue
		}
		// Decorator line. A decorator may open a multi-line argument list; track
		// it so its continuation lines are skipped as data, not parsed as a
		// handler.
		if strings.HasPrefix(t, "@") {
			depth += nestjsBracketDelta(raw)
			if depth < 0 {
				depth = 0
			}
			continue
		}
		if m := nestjsHandlerNameRe.FindStringSubmatch(t); m != nil {
			name := m[1]
			// Guard: never bind to `constructor` — a delegating/aliasing
			// controller (e.g. UsersLoginController) declares its injected
			// dependencies in a constructor that sits between the class
			// opening and the first route; without this guard the scan would
			// bind the verb decorator to `constructor` and the real handler
			// would be lost. A multi-line constructor signature is consumed by
			// the depth tracker below.
			if name == "constructor" {
				depth += nestjsBracketDelta(raw)
				if depth < 0 {
					depth = 0
				}
				continue
			}
			return name, i
		}
		// A non-blank, non-comment, non-decorator, non-handler line means we've
		// walked past the handler region for this decorator (e.g. into the next
		// class member without a recognisable signature). Stop to avoid binding
		// a far-away symbol.
		return "", -1
	}
	return "", -1
}

// nestjsBracketDelta returns the net change in (){}[]-nesting contributed by a
// line, ignoring brackets inside single/double/back-quoted strings (so a
// description string like "(parity with legacy)" or a path with "[id]" does not
// skew the count). It is a deliberately small lexer: enough to track multi-line
// decorator argument lists without a full TS parser.
func nestjsBracketDelta(line string) int {
	delta := 0
	var quote byte // 0 = not in string; otherwise the opening quote char
	for i := 0; i < len(line); i++ {
		c := line[i]
		if quote != 0 {
			if c == '\\' { // skip escaped char inside string
				i++
				continue
			}
			if c == quote {
				quote = 0
			}
			continue
		}
		switch c {
		case '\'', '"', '`':
			quote = c
		case '(', '{', '[':
			delta++
		case ')', '}', ']':
			delta--
		}
	}
	return delta
}

// nestController records one @Controller occurrence: the line index it appears
// on and the base-path prefix it declares (empty for `@Controller()`).
type nestController struct {
	lineIdx int
	prefix  string
}

func synthesizeNestJS(content string, emit emitFn, emitDef emitDefSigFn) {
	if !strings.Contains(content, "@Controller") {
		return
	}
	lines := strings.Split(content, "\n")
	// Collect EVERY @Controller declaration with the line it sits on, so each
	// HTTP-verb method can be attributed to the base path of the nearest
	// PRECEDING controller. A file with two @Controller classes no longer
	// folds the second controller's routes under the first's prefix. The line
	// index is used (rather than a byte offset) because the verb loop below is
	// line-oriented; an @Controller decorator and its prefix sit on the same
	// line in every practical NestJS source.
	//
	// Collection scans the WHOLE content (not line-by-line) so a multi-line
	// object-form argument is read as one balanced span; the 0-based line index
	// is derived from the byte offset of the decorator. nestjsParseControllerArg
	// handles every form (#4340): bare-string, array, and
	// object `{ path, version }` with NestJS URI versioning.
	var controllers []nestController
	for _, loc := range nestControllerOpenRe.FindAllStringIndex(content, -1) {
		openParen := loc[1] - 1 // index of '(' (the regex ends just past it)
		arg, _ := nestjsBalancedArg(content, openParen)
		prefix := nestjsParseControllerArg(arg)
		lineIdx := strings.Count(content[:loc[0]], "\n")
		controllers = append(controllers, nestController{lineIdx: lineIdx, prefix: prefix})
	}
	// Map each byte offset's line to an index for the forward scan. We re-find
	// the verb decorators on a per-line basis so the handler scan starts from
	// the line AFTER the verb decorator line.
	for lineIdx, line := range lines {
		m := nestjsVerbPathRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		verb := strings.ToUpper(m[1])
		methodPath := m[2]
		if verb == "ALL" {
			verb = "ANY"
		}
		methodName, methodLineIdx := nestjsFindHandlerName(lines, lineIdx+1)
		if methodName == "" {
			continue
		}
		// Attribute this method to the nearest preceding @Controller. Methods
		// before any @Controller (rare) fall back to the root prefix "".
		prefix := nestjsPrefixForLine(controllers, lineIdx)
		full := joinPathFragments("/"+strings.Trim(prefix, "/"), methodPath)
		canonical := httproutes.Canonicalize(httproutes.FrameworkExpress, full)
		if canonical == "" {
			continue
		}
		// #4568/#4569 — parse the handler signature (decorated params + return
		// type) starting at the handler method line and stamp it onto the
		// synthesized endpoint so the Parameters table and Response shape render.
		sig := nestjsReadSignature(lines, methodLineIdx)
		// #4319 — anchor the synthetic at the handler method's 1-based line so
		// the Phase-2 resolver can bridge endpoint→handler by file:line
		// co-location when the name-based source_handler match fails. methodLineIdx
		// is a 0-based index into `lines`; +1 makes it the 1-based StartLine the
		// treesitter handler Operation also carries.
		emitDef(verb, canonical, "nestjs", "Controller", methodName, methodLineIdx+1, sig)
	}
}

// nestjsPrefixForLine returns the base-path prefix of the @Controller nearest
// above `lineIdx`. `controllers` is in ascending line order (built by a forward
// line scan). Returns "" when no controller precedes the line.
func nestjsPrefixForLine(controllers []nestController, lineIdx int) string {
	prefix := ""
	for _, c := range controllers {
		if c.lineIdx > lineIdx {
			break
		}
		prefix = c.prefix
	}
	return prefix
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

// emitDefSigFn extends emitDefFn with a parsed NestJS handler signature
// (parameters + response/request DTO), stamped onto the synthesized
// http_endpoint entity so the dashboard renders the Parameters table and
// Response shape (#4568/#4569).
type emitDefSigFn func(method, canonicalPath, framework, handlerKind, handlerName string, defLine int, sig nestSignature)

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
