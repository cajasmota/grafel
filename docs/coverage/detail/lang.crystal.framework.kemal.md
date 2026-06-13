<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.crystal.framework.kemal` — Kemal (Crystal HTTP)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [crystal](../by-language/crystal.md)
- **Category:** [http_framework](../by-category/http_framework.md)
- **Subcategory:** Backend HTTP
- **Capability cells:** 50

## Capabilities


### Routing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Endpoint deprecation versioning | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Endpoint pagination posture | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Endpoint response codes | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Endpoint synthesis | ✅ `full` | `2026-06-12` | — | `internal/engine/http_endpoint_kemal.go`<br>`internal/engine/httproutes/canonicalize.go` | synthesizeKemalRoutes scans a Crystal source for Kemal `get "/x" do ... end` verb blocks, Amber `get "/path", Ctrl, :action` routes-block registrations, and Lucky inline-path Action macros, synthesising one canonical http_endpoint_definition each, canonicalised through httproutes.FrameworkKemal into the shared `{param}` form (colon-params `/users/:id` -> `/users/{id}`). Gated by kemalHasRoute (framework marker + verb macro pre-filter) so arbitrary files are skipped. Honest exclusions: interpolated/variable paths, ws "/..." websocket routes, and Lucky name-derived (no inline path) Action routes — follow-up #4937. |
| Handler attribution | 🟢 `partial` | `2026-06-12` | 4937 | `internal/engine/http_endpoint_kemal.go` | Synthesised endpoints carry a handler kind; Kemal `do ... end` route blocks and Amber/Lucky inline handlers are anonymous bodies, so a named-handler IMPLEMENTS edge is bound only where a same-named handler is resolvable. The Amber controller form `get "/x", Ctrl, :action` names the controller+action — full named-handler attribution across all three frameworks is follow-up #4937. |
| Route extraction | ✅ `full` | `2026-06-12` | — | `internal/engine/http_endpoint_kemal.go` | Static Kemal/Amber/Lucky verb+path routes recovered by synthesizeKemalRoutes (#4749); interpolated/variable and websocket routes dropped (honest, see endpoint_synthesis). |
| Websocket route extraction | 🔴 `missing` | `2026-06-14` | — | — | #4965: dedicated websocket_route_extraction not yet implemented for this framework. The capability key was introduced for the rust axum/actix/warp WS extractor (internal/custom/rust/websocket_routes.go); this framework's WebSocket-upgrade idiom is not yet recognised and is a follow-up gap. |

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
| Type graph extraction | 🟢 `partial` | — | 4937 | `internal/extractors/crystal/extractor.go`<br>`internal/extractors/crystal/extractor_test.go` | The base extractor emits class/struct/module/lib SCOPE.Component entities and EXTENDS edges for `class Foo < Bar` inheritance (classRE group 2), giving a partial type graph (inheritance spine). Partial (honest): include/extend module-mixin edges, generic type-params, and enum/alias nodes are not yet in the graph — follow-up #4937. |

### Type System

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Enum extraction | ✅ `full` | — | 4937 | `internal/extractor/enum_valueset.go`<br>`internal/extractors/crystal/depth.go`<br>`internal/extractors/crystal/depth_test.go` | enumRE matches `enum Name` / `enum Name : Int32` headers; extractEnums spans the body via the nesting-aware findEndKeyword scanner and parseEnumMembers walks each member line (bare `Red` value-less member, `Green = 2` value-carrying), stopping at the first `def`/`macro`/`private` so member-methods are not miscounted. Members are handed to the shared cross-language extractor.EnumEntity builder, so Crystal enums converge on the SAME SCOPE.Enum value-set node model as Python/TS/Java/Go/Ruby/C# — carrying members/values/member_count/members_json Properties (kind_hint=crystal_enum). Honest-partial: a member whose literal value is a non-literal expression records the name with an empty value. Proven by TestCrystalEnum_HappyPath plus the no-match and wrong-language no-op tests in depth_test.go. |
| Interface extraction | 🔴 `missing` | — | 4937 | — | Crystal has no interface keyword; the idiomatic analog is a `module` used as a mixin (include/extend). Modules ARE emitted as SCOPE.Component, but include/extend mixin edges (the interface-implements signal) are not yet recorded — follow-up #4937. |
| Type alias extraction | ✅ `full` | — | 4937 | `internal/extractors/crystal/depth.go`<br>`internal/extractors/crystal/depth_test.go` | aliasRE matches `alias Name = Type` (target may be a union/generic/proc type); extractAliases emits one SCOPE.Component(subtype=alias) per alias carrying the aliased target as an `alias_target` Property plus a REFERENCES edge to the primary named target type (primaryTypeName isolates the first PascalCase identifier, e.g. `Array(String)` -> Array, `Int32 | String` -> Int32). Honest-partial: a primitive-only target still records the `alias_target` Property but emits no REFERENCES edge; the Ruby `alias a b` (no `=`) form is correctly NOT matched. Proven by TestCrystalAlias_HappyPath plus no-match / wrong-language no-op tests in depth_test.go. |
| Type extraction | ✅ `full` | — | — | `internal/extractors/crystal/extractor.go`<br>`internal/extractors/crystal/extractor_test.go` | class/abstract class/struct (classRE), module (moduleRE) and lib C-binding blocks (libRE) are emitted as SCOPE.Component with the correct subtype (class/module/lib), StartLine/EndLine and a first-line Signature; EXTENDS edges capture `class Foo < Bar` inheritance. Proven by the TestCrystal_* class/struct/module/lib discovery assertions in extractor_test.go. enum/alias are now emitted (see enum_extraction / type_alias_extraction, #4937); generic type-params `class Foo(T)` remain dropped (deferred follow-up). |

### DI

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DI binding extraction | — `not_applicable` | — | — | — | Crystal has no DI-container idiom (no Spring/NestJS-style binding registry); dependencies are wired by plain constructor/initialize args, so there is no binding declaration to extract. |
| DI injection point | — `not_applicable` | — | — | — | No DI container — no injection-point annotation/decorator to recognise (Crystal uses plain `initialize` constructor args). |
| DI scope resolution | — `not_applicable` | — | — | — | No DI container — there is no scope/lifetime resolver to model. |

### Testing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Tests linkage | ✅ `full` | `2026-06-13` | — | `internal/custom/crystal/tests_route_e2e.go`<br>`internal/engine/http_endpoint_kemal.go`<br>`internal/engine/httproutes/canonicalize.go`<br>`internal/extractors/crystal/depth.go`<br>`internal/extractors/crystal/depth_test.go` | Test->endpoint route-hit linkage (#4749, Crystal slice of tail epic; mirrors Swift/Vapor #4755): http_endpoint_kemal.go synthesizes canonical http_endpoint_definition entities from Crystal web route registrations — Kemal `get "/users/:id" do ... end`, Amber `get "/users", Ctrl, :action` (routes block) and Lucky inline-path Action macros — with colon-param canonicalisation (FrameworkKemal). custom/crystal/tests_route_e2e.go emits one test_suite per `*_spec.cr` carrying e2e_route_calls from spec-kemal request helpers (`get "/path"` / `post "/path"`); the shared language-agnostic engine.linkE2ERouteTestsToEndpoints pass then emits the TESTS edge to the exercised endpoint (proven by TestKemal_E2ERouteTestLinkage + TestIssue4749_CrystalSpecKemalE2ERouteTestsLinkToEndpoints + TestCrystalRouteE2E_*). Crystal spec uses anonymous describe/it closure blocks (Ruby-like), so the test_suite is the scope-owner carrying the route hits (Crystal analog of Ruby #4684 / JS #4680). Local-variable/receiver typing (#4749 part a) is N/A: the crystal base extractor names defs bare (not Type.method) with no class-qualified receiver resolver to consume a receiver_type stamp, so route-string linkage is the coverage mechanism (mirrors functional Elixir #4688 / Swift #4755). Honest exclusions: interpolated/variable routes, ws "/..." websocket routes, and Lucky name-derived (no inline path) Action routes are documented follow-ups. NAMED-SYMBOL subject linkage added by #4937: crystal/depth.go::extractSpecSuite emits one test_suite per `*_spec.cr` whose describe/it/context blocks name a subject — resolved from the outermost `describe Const` (`Class:User`) else the spec-file-stem convention (`order_service_spec.cr` -> OrderService) — and emits a name-affinity TESTS edge to that subject class (mirrors Ruby RSpec #4398; both Spectator and the built-in `spec` DSL share the describe/it/context surface). An example-less spec and a non-spec file emit no suite; receiver typing is now stamped on Crystal CALLS edges (receiver_type Property, #4937). Proven by TestCrystalSpecSuite_* in depth_test.go. |

### Observability

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Log extraction | 🟢 `partial` | — | 4937 | `internal/substrate/template_pattern_crystal.go` | sniffTemplatePatternsCrystal recognises log_format payloads — puts/print/p "..." and Log.debug/info/notice/warn/error literal messages (crystalLogRe) — emitting TemplatePattern entities attributed to the enclosing def header. Partial (honest): only literal log payloads are catalogued (no structured-field/severity-level extraction); metric/trace have no Crystal producer — follow-up #4937. |
| Metric extraction | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Trace extraction | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |

### Data

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DB effect | ✅ `full` | — | — | `internal/substrate/effect_sinks_crystal.go` | sniffEffectsCrystal classifies crystal-db calls into db_read (db.query/query_one/scalar/query_each — crystalDBReadRe) and db_write (db.exec with INSERT/UPDATE/DELETE/CREATE/DROP/ALTER write SQL — crystalDBWriteRe), each emitted as an EffectMatch attributed to the enclosing def with a confidence weight. Proven by the substrate effect-sweep tests. |

### Substrate

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Confidence overlay | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Config consumption | ✅ `full` | — | — | `internal/substrate/crystal.go` | sniffCrystal recognises config bindings: `NAME = ENV["Y"]? || "default"` (crystalEnvOrRe) and `NAME = ENV.fetch("Y", "default")` (crystalEnvFetchRe), emitting a Binding tagged with ProvenanceEnvFallback so env-driven config consumption is modelled. |
| Constant propagation | ✅ `full` | — | — | `internal/substrate/crystal.go` | sniffCrystal recognises Crystal SCREAMING_CASE constant literal bindings `NAME = "value"` (crystalLiteralRe, uppercase-first-letter rule per Crystal constant semantics — same as Ruby) and propagates them as Bindings. |
| Dead code detection | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Def use chain extraction | ✅ `full` | — | — | `internal/substrate/def_use_crystal.go` | sniffDefUseCrystal emits VarDef/VarUse pairs for Crystal local-variable assignments and uses, with a crystalReservedDefUse denylist filtering keywords, building the def-use chain consumed by the shared dataflow passes. |
| Env fallback recognition | ✅ `full` | — | — | `internal/substrate/crystal.go` | The `ENV["Y"]? || "default"` and `ENV.fetch("Y", "default")` forms (crystalEnvOrRe/crystalEnvFetchRe in sniffCrystal) are tagged ProvenanceEnvFallback with a confidence weight — the env-fallback default value is recovered. |
| Error flow | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Feature flag gating | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Fs effect | ✅ `full` | — | — | `internal/substrate/effect_sinks_crystal.go` | sniffEffectsCrystal classifies fs_read (File.read/read_lines/open read-mode, Dir.entries — crystalFSReadRe) and fs_write (File.write, File.open(...,"w"), File.delete, Dir.mkdir — crystalFSWriteRe) as EffectMatch fs effects attributed to the enclosing def. |
| HTTP effect | ✅ `full` | — | — | `internal/substrate/effect_sinks_crystal.go` | sniffEffectsCrystal recognises outbound HTTP calls — HTTP::Client.get/post/put/patch/delete/exec and Crest client calls to absolute/relative URLs (crystalHTTPRe) — as http_out EffectMatch entities. |
| Import resolution quality | 🟢 `partial` | — | 4905 | `internal/extractors/crystal/extractor.go` | require/require_relative IMPORTS edges are emitted to path-string placeholders (see lang.crystal.core import_resolution_quality); not resolved to concrete module entities — partial. Follow-up #4937. |
| Module cycle detection | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Mutation effect | ✅ `full` | — | — | `internal/substrate/effect_sinks_crystal.go` | sniffEffectsCrystal recognises instance-variable assignment `@field = ...` (crystalMutationRe) as a mutation EffectMatch (confidence 0.7) attributed to the enclosing def. |
| Pure function tagging | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Reachability analysis | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Request shape extraction | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Request sink dataflow | ✅ `full` | — | — | `internal/substrate/taint_sites_crystal.go` | sniffTaintCrystal recognises request-derived taint sources (Kemal env.params.url/query, env.request.body; Lucky/Amber params[:name], request.body — crystalSourceParamsRe) that feed the shared request-sink dataflow pass. |
| Response shape extraction | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Sanitizer recognition | 🟢 `partial` | — | 4937 | `internal/substrate/taint_sites_crystal.go` | sniffTaintCrystal recognises Crystal sanitizer primitives (e.g. HTML escaping / typed param coercion) so tainted->sanitized transitions are modelled. Partial (honest): Crystal `params.read(Type)` / Lucky typed-action coercion is documented as a type-safe sanitizer exclusion — follow-up #4937. |
| Schema drift detection | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Taint sink detection | ✅ `full` | — | — | `internal/substrate/taint_sites_crystal.go` | sniffTaintCrystal recognises dangerous sinks for Kemal/Lucky/Amber and crystal-db (SQL execution, command/eval, response render) as taint sinks consumed by the shared taint engine. |
| Taint source detection | ✅ `full` | — | — | `internal/substrate/taint_sites_crystal.go` | sniffTaintCrystal recognises request-derived taint sources (params/query/body across Kemal/Lucky/Amber — crystalSourceParamsRe) as taint origins. |
| Template pattern catalog | ✅ `full` | — | — | `internal/substrate/template_pattern_crystal.go` | sniffTemplatePatternsCrystal catalogues i18n keys (I18n.translate/t/t("key") — crystalI18nRe), log_format payloads (crystalLogRe) and raw SQL string literals whose first token is a SQL verb (crystalSQLRe, for crystal-db/jennifer raw queries), each as a TemplatePattern. |
| Vulnerability finding | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.crystal.framework.kemal ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
