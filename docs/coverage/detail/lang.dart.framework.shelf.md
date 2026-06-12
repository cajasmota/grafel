<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.dart.framework.shelf` — shelf_router / dart_frog / conduit (Dart HTTP)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [dart](../by-language/dart.md)
- **Category:** [http_framework](../by-category/http_framework.md)
- **Subcategory:** Backend HTTP
- **Capability cells:** 49

## Capabilities


### Routing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Endpoint deprecation versioning | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Endpoint pagination posture | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Endpoint response codes | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Endpoint synthesis | ✅ `full` | `2026-06-11` | — | `internal/engine/http_endpoint_dart.go`<br>`internal/engine/httproutes/canonicalize.go` | Dart server-side routes synthesized to canonical http_endpoint_definition via synthesizeShelfRoutes/synthesizeConduitRoutes/synthesizeDartFrogRoutes (#4758). shelf_router Router()..get('/users/<id>', h)/router.post('/x', h) with <id>/<id|[0-9]+> angle-bracket params -> {id} (FrameworkShelf, |regex constraint dropped); dart_frog file-based routes (routes/users/[id]/index.dart + onRequest) -> /users/{id} ([id]->{id}, index.dart->parent, verb read from HttpMethod.* dispatch else ANY, FrameworkDartFrog); conduit router.route("/users/[:id]") -> {id} ([:id] optional + :id required, FrameworkConduit) emitted ANY. In the same shape axum/Vapor/Kemal/Jester emit; the base dart extractor stays structural-only. Value-asserting tests in http_endpoint_dart_test.go. Honest follow-ups: conduit @Operation verb-precise controller methods, angel3/jaguar producers, dart_frog dynamic verb dispatch. |
| Handler attribution | 🟢 `partial` | `2026-06-11` | 4758 | `internal/engine/http_endpoint_dart.go` | Endpoints emitted with handlerKind=Controller; shelf/dart_frog/conduit handlers are referenced inline or via a Controller class, so a same-named handler IMPLEMENTS edge is bound only when the resolver finds one -- full named-handler attribution is a follow-up. |
| Route extraction | ✅ `full` | `2026-06-11` | — | `internal/engine/http_endpoint_dart.go` | Static shelf_router/dart_frog/conduit verb+path routes recovered by synthesizeShelfRoutes/synthesizeDartFrogRoutes/synthesizeConduitRoutes (#4758); interpolated/concatenated paths ($-interpolation) dropped (honest). dart_frog path derived from the route file location under routes/. |

### View

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| View rendering | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |

### Auth

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Auth coverage | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |

### Validation

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DTO extraction | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Request validation | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |

### Middleware

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Middleware coverage | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Rate limit stamping | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |

### Schema

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Type graph extraction | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |

### Type System

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Enum extraction | 🟢 `partial` | — | 4912 | `internal/extractors/dart/types.go`<br>`internal/extractors/dart/types_test.go` | #4912: the base Dart extractor (types.go, dartEnums) now emits a SCOPE.Enum value-set node per `enum` declaration via extractor.EnumEntity(kind_hint=dart_enum) — plain enums and Dart-2.17 enhanced enums (constants-before-`;` kept as members, single-literal ctor args lifted to values). Value-asserted: TestDartTypes_PlainEnum (Color -> red,green,blue), TestDartTypes_EnhancedEnum (Planet -> mercury,earth, post-`;` fields/methods dropped). PARTIAL: regex over canonical declarations; computed/multi-arg ctor values are not resolved. |
| Interface extraction | 🟢 `partial` | — | 4912 | `internal/extractors/dart/types.go`<br>`internal/extractors/dart/types_test.go` | #4912: Dart-3 class modifiers (types.go, dartModifiedClasses) — `sealed`/`base`/`interface`/`final`/`mixin class` — are now extracted as SCOPE.Component(class) carrying Properties{class_modifier, dart_sealed, dart_interface}; the base classRE only matched plain/`abstract` classes so these (incl. `interface class`, Dart's nominal-interface form) were invisible. Value-asserted: TestDartTypes_SealedClass (sealed Shape -> dart_sealed=true; interface Drawable -> dart_interface=true). PARTIAL: subtype permits-clause / exhaustiveness graph wiring not modelled; Dart has no standalone `interface` keyword (interfaces are implicit via `implements`). |
| Type alias extraction | 🟢 `partial` | — | 4912 | `internal/extractors/dart/types.go`<br>`internal/extractors/dart/types_test.go` | #4912: `typedef` (types.go, dartTypedefs) now emits SCOPE.Schema(subtype=type_alias) with type_body — both the modern `typedef Name = <type>;` (Dart 2.13+) and the legacy function-type `typedef Ret Name(params);` spellings, parity with the python/rust/go type_alias shape. Value-asserted: TestDartTypes_TypedefAlias (JsonMap -> Map<String, dynamic>), TestDartTypes_TypedefFunc (Comparator). PARTIAL: regex over canonical one-line forms. |
| Type extraction | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |

### DI

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DI binding extraction | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| DI injection point | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| DI scope resolution | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |

### Testing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Tests linkage | ✅ `full` | `2026-06-11` | 4758 | `internal/custom/dart/tests_route_e2e.go`<br>`internal/engine/http_endpoint_dart.go`<br>`internal/engine/http_endpoint_e2e_testmap.go`<br>`internal/engine/httproutes/canonicalize.go`<br>`internal/extractors/custom_dispatch.go` | Dart slice of the test->endpoint coverage-linkage tail epic (#4749/#4615), closing the #4757 N/A now that a Dart server-side producer exists. PRODUCER: internal/engine/http_endpoint_dart.go synthesizes canonical http_endpoint_definition entities from shelf_router/dart_frog/conduit route registrations (see Routing.endpoint_synthesis). ROUTE-HIT linkage: custom/dart/tests_route_e2e.go emits one test_suite per Dart test file (*_test.dart / under test/ or integration_test/) carrying e2e_route_calls from package:test route hits -- shelf handler hits handler(Request('GET', Uri.parse('/path'))) and package:http/shelf client calls client.get(Uri.parse('/path')) (host stripped to path); the shared language-agnostic engine.linkE2ERouteTestsToEndpoints pass emits the TESTS edge to the exercised endpoint (proven by TestIssue4758_DartTestE2ERouteTestsLinkToEndpoints + the producer tests TestShelf_*/TestDartFrog_*/TestConduit_*). SCOPE-OWNER: package:test test('...', () {}) closures carry prose descriptions (not code symbols), so -- like JS #4680 / Ruby #4719 / Nim anonymous closures -- the suite-level test_suite is the scope-owner carrying the route hits. DISPATCH: dart's primary custom prefix is already custom_dart_, and custom_dart_tests_route_e2e is additionally locked into extraCustomPrefixesForLanguage["dart"] (the #4769 belt-and-suspenders guard) so CustomExtractorsFor("dart") cannot silently drop the tail extractor; asserted by TestTailCoverageLinkageExtractorsDispatch/dart. Local-variable/receiver typing (#4749 part a) is N/A: shelf route dispatch is keyed by the literal route string in Request('VERB', Uri.parse('/path')), not by an obj.method() receiver, so a receiver_type stamp would be a dead annotation (mirrors functional Nim/Crystal). Honest exclusions/follow-ups: conduit @Operation verb-precise methods, angel3/jaguar, dynamic/concatenated routes. |

### Observability

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Log extraction | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Metric extraction | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Trace extraction | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |

### Data

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DB effect | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |

### Substrate

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Confidence overlay | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Config consumption | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Constant propagation | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Dead code detection | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Def use chain extraction | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Env fallback recognition | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Error flow | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Feature flag gating | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Fs effect | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| HTTP effect | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Import resolution quality | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Module cycle detection | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Mutation effect | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Pure function tagging | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Reachability analysis | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Request shape extraction | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Request sink dataflow | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Response shape extraction | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Sanitizer recognition | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Schema drift detection | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Taint sink detection | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Taint source detection | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Template pattern catalog | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Vulnerability finding | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.dart.framework.shelf ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
