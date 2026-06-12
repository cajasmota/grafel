<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.elixir.framework.tesla` — Tesla

Auto-generated. Back to [summary](../summary.md).

- **Language:** [elixir](../by-language/elixir.md)
- **Category:** [http_framework](../by-category/http_framework.md)
- **Subcategory:** Backend HTTP
- **Capability cells:** 49

## Capabilities


### Routing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Endpoint deprecation versioning | 🔴 `missing` | — | 3628 | — | — |
| Endpoint pagination posture | 🔴 `missing` | `2026-06-02` | 3628 | `internal/engine/http_endpoint_pagination.go`<br>`internal/engine/http_endpoint_pagination_patterns.go`<br>`internal/engine/http_endpoint_pagination_test.go`<br>`internal/engine/http_endpoint_synthesis.go` | #3628: applyEndpointPagination stamps paginated/pagination_style/pagination_params via the cross-language parameters/parameter_schema fallback (limit+offset/page/cursor shape). No framework-specific pagination-class/ORM signal yet for this framework. |
| Endpoint response codes | 🔴 `missing` | — | 3818 | — | — |
| Endpoint synthesis | ✅ `full` | `2026-05-30` | — | `internal/engine/http_endpoint_elixir_tesla.go`<br>`internal/engine/http_endpoint_elixir_tesla_test.go`<br>`internal/engine/http_endpoint_synthesis.go` | Tesla use-module + BaseUrl middleware + Tesla.<verb>/bare-verb client calls -> outbound http_endpoint_call with verb+canonical path (#3511) |
| Handler attribution | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Route extraction | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |

### View

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| View rendering | 🔴 `missing` | — | view_rendering:#3628-not-yet-extracted | — | — |

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
| Middleware coverage | 🟢 `partial` | `2026-05-30` | 3511 | `internal/engine/http_endpoint_elixir_tesla.go`<br>`internal/engine/http_endpoint_elixir_tesla_test.go`<br>`internal/engine/http_endpoint_synthesis.go` | BaseUrl/JSON Tesla plug middleware parsed for base-url only; full middleware-chain modeling pending (#3511) |
| Rate limit stamping | 🔴 `missing` | — | [link](https://github.com/cajasmota/archigraph/issues/3778) | — | endpoint rate-limit / throttle stamping not yet implemented for this framework; the #3628 child shipped express-rate-limit (JS/TS) + slowapi/django-ratelimit/flask-limiter/DRF (Python). express-slow-down-compatible / framework-native limiters for this framework are future work. |

### Schema

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Type graph extraction | — `not_applicable` | — | — | — | GraphQL schema type→type graph (object-typed field -> referenced object type with list/nullable cardinality) is a GraphQL-only concept; this framework is not a GraphQL server, so it has no GraphQL object-type relationship graph. |

### Type System

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Enum extraction | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Interface extraction | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Type alias extraction | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Type extraction | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |

### DI

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DI binding extraction | 🔴 `missing` | — | 3628 | — | — |
| DI injection point | 🔴 `missing` | — | 3628 | — | — |
| DI scope resolution | 🔴 `missing` | — | 3628 | — | — |

### Testing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Tests linkage | ✅ `full` | `2026-06-03` | — | `internal/extractors/cross/testmap/frameworks.go` | Deep testmap Elixir TESTS linkage (framework-agnostic; keys on .exs + use ExUnit.Case): ExUnit (test "..." do leaves, describe groups via balanced do/end body walk) + StreamData (property "..." do) with subject-from-module-name + body call resolution (Foo.bar(...) promoted high); Elixir assertion stopwords (assert/refute/assert_raise/...). Value-asserting test in extractor_test.go (TestElixir_Tesla_TestsLinkage) asserts the tesla-idiom ExUnit test->target edge to MyApp.ApiClient.create_order. Sibling of the flagship full status (#4027). |

### Observability

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Log extraction | 🟢 `partial` | `2026-06-03` | backfill:dictionary-completeness | `internal/custom/elixir/observability.go`<br>`internal/custom/elixir/observability_test.go` | Logger.debug/info/warning/error(...) statements captured by framework-agnostic observabilityExtractor (fires on any .ex) as SCOPE.Pattern/log_statement with log_level + leading string-literal message; Logger.metadata(...) captured. Value-asserting test (TestObservability_Tesla_Trailing) asserts the tesla-idiom Logger.info artifact. PARTIAL: logger require/import and message binding not correlated cross-file; interpolated/concatenated message tails not resolved. Sibling of flagship partial (#4027). |
| Metric extraction | 🟢 `partial` | `2026-06-03` | [link](https://github.com/cajasmota/archigraph/issues/3474) | `internal/custom/elixir/observability.go`<br>`internal/custom/elixir/observability_test.go` | :telemetry.execute([:a,:b],...) event names and Telemetry.Metrics counter/summary/last_value/distribution/sum("name") captured by framework-agnostic observabilityExtractor as SCOPE.Pattern/metric (metric_name + telemetry_event) when literal at call site. Value-asserting test (TestObservability_Tesla_Trailing) proves the exact tesla-idiom :telemetry.execute event name. PARTIAL: metric/event name -> :telemetry.attach handler -> reporter/exporter wiring spans multiple files and is not resolved. Sibling of flagship partial (#4027). |
| Trace extraction | 🟢 `partial` | `2026-06-03` | [link](https://github.com/cajasmota/archigraph/issues/3474) | `internal/custom/elixir/observability.go`<br>`internal/custom/elixir/observability_test.go` | :telemetry.span([:a,:b],...) event-prefix captured by framework-agnostic observabilityExtractor as SCOPE.Pattern/trace_span (span_name + telemetry_event) when literal at call site. Value-asserting test (TestObservability_Tesla_Trailing) proves the exact tesla-idiom :telemetry.span name. PARTIAL: idiomatic Elixir has no static OTel span/exporter binding; spans are bridged from :telemetry events by a handler attached at runtime (cross-file). Sibling of flagship partial (#4027). |

### Data

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DB effect | 🟢 `partial` | — | 4941 | `internal/substrate/effect_sinks_fsharp.go`<br>`internal/substrate/effect_sinks_fsharp_test.go` | #4941 (follow-up #4906): sniffEffectsFSharp adds the FIRST db_effect coverage for the F# data stack (F# had none after #4906). Classifies the high-value F# data drivers into db_read / db_write EffectMatch records attributed to the enclosing `let [rec/inline]` binding or `member this/_/x.Name` (scanFSharpEffectHeaders — richer than the payload-shapes let-only header scan, so member-bound repositories attribute correctly). Drivers: (1) EF Core (F#) — DbSet LINQ reads ctx.T.Find/Where/Single/First/FirstOrDefault/Any/Count/ToList(Async)/AsNoTracking/Include/FromSqlRaw (fsharpEFReadRe) + the F# `query { for x in ctx.T ... }` computation expression (fsharpEFQueryCERe) -> db_read; ctx.SaveChanges(Async)/ctx.T.Add(Async)/AddRange/Update/Remove/ExecuteUpdate/ExecuteDelete (fsharpEFWriteRe) -> db_write. (2) Dapper / Dapper.FSharp — conn.Query*/QueryFirst*/QuerySingle*/QueryMultiple*/SelectAsync + `select { for ... }` CE -> db_read; conn.Execute*/ExecuteScalar*/InsertAsync/UpdateAsync/DeleteAsync + `insert`/`update`/`delete` CEs -> db_write. (3) Npgsql.FSharp — Sql.query "<sql>" literal classified by its leading SQL verb (SELECT/WITH -> db_read; INSERT/UPDATE/DELETE/CREATE/DROP/ALTER/TRUNCATE/MERGE -> db_write), so a SELECT is never misclassified as a write. Also classifies http_out for System.Net.Http HttpClient.*Async + FsHttp `http { GET ... }`/Http.get. Emitted EffectMatch records flow through internal/links/effect_propagation.go like every other language sink (table attribution / ACCESSES_TABLE is a separate concern, not done by the sink sniffer). Value-asserted by effect_sinks_fsharp_test.go (EFCore/EFQueryCE/Dapper/Npgsql-verb-discrimination/HTTP/NonDataNoop/Empty/Registered). PARTIAL (honest): regex receiver-name heuristic (conn/ctx/db/connection identifiers), no cross-binding type resolution; SQLProvider type-provider queries are NOT yet captured (no static call shape — best-effort deferred); raw-SQL table extraction + ACCESSES_TABLE attribution and Dapper ambiguous-Execute-without-literal refinement are documented follow-ups. |

### Substrate

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Confidence overlay | ✅ `full` | `2026-06-04` | — | `internal/graph/graph.go`<br>`internal/mcp/tools.go`<br>`internal/substrate/elixir_confidence_overlay_test.go`<br>`internal/types/confidence.go` | universal confidence overlay (internal/types/confidence.go: not framework-gated); per-language data feed sniffEffectsElixir emits Confidence>0 on this framework idiom (elixir_confidence_overlay_test.go). epic #3872 parity-grind-elixir |
| Config consumption | 🔴 `missing` | — | 3641 | — | — |
| Constant propagation | ✅ `full` | `2026-05-30` | — | `internal/engine/http_endpoint_elixir_tesla.go`<br>`internal/engine/http_endpoint_elixir_tesla_test.go`<br>`internal/engine/http_endpoint_jsts_client_1483.go`<br>`internal/engine/http_endpoint_synthesis.go` | Tesla.Middleware.BaseUrl literal + @module-attr resolution into canonical path (#3511) |
| Dead code detection | 🟢 `partial` | `2026-06-03` | — | `internal/links/reachability.go`<br>`internal/mcp/dead_code.go`<br>`internal/substrate/entry_points.go`<br>`internal/substrate/entry_points_elixir.go` | — |
| Def use chain extraction | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/links/def_use_pass.go`<br>`internal/substrate/def_use.go`<br>`internal/substrate/def_use_elixir.go` | Elixir def-use sniffer registered; intra-procedural def-use chains over .ex/.exs |
| Env fallback recognition | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Error flow | ✅ `full` | `2026-06-03` | — | `internal/extractor/exception_flow.go`<br>`internal/extractors/elixir/exception_flow.go`<br>`internal/extractors/elixir/exception_flow_test.go` | raise X / raise mod.X -> THROWS; rescue e in [A,B] / unbound typed rescue -> CATCHES; bare rescue + string raise + reraise + catch :throw + {:error,_} tuple dropped (#3628) |
| Feature flag gating | 🟢 `partial` | `2026-06-03` | 4149 | `internal/engine/feature_flag_edges.go`<br>`internal/engine/feature_flag_edges_test.go` | Elixir flag-check call sites -> feature:<key> + GATED_BY (framework-agnostic engine pass, fires regardless of framework). Verified to fire & attribute: FunWithFlags.enabled?(:key/"key", for: actor) with atom key normalized (leading : stripped, like Ruby symbols), Flippant.enabled?("key", actor), Unleash.enabled?("key") bare predicate, plus Unleash is_enabled? predicate. Honest-partial: dynamic keys (FunWithFlags.enabled?(flag)) + non-FF receivers (record.enabled?, SomeMod.enabled?(:x)) emit nothing; no Elixir enclosing-function index so edges are file-scope-anchored (File:<path>). |
| Fs effect | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| HTTP effect | ✅ `full` | `2026-05-30` | — | `internal/engine/http_endpoint_elixir_tesla.go`<br>`internal/engine/http_endpoint_elixir_tesla_test.go`<br>`internal/engine/http_endpoint_synthesis.go` | Tesla verb calls modeled as outbound HTTP effects (#3511) |
| Import resolution quality | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Module cycle detection | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/links/module_cycle_pass.go` | Language-agnostic Tarjan SCC over IMPORTS edges; Elixir use/alias/import edges flow through extractor |
| Mutation effect | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Pure function tagging | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/links/effect_propagation.go`<br>`internal/links/pure_function_pass.go`<br>`internal/substrate/effect_sinks_elixir.go` | Elixir effect sniffer registered; functions with no elixir effect matches tagged pure=true; immutable semantics make Elixir especially suitable |
| Reachability analysis | 🟢 `partial` | `2026-06-03` | — | `internal/links/reachability.go`<br>`internal/substrate/entry_points.go`<br>`internal/substrate/entry_points_elixir.go` | — |
| Request shape extraction | 🟢 `partial` | `2026-06-03` | [link](https://github.com/cajasmota/archigraph/issues/2771) | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_elixir.go` | — |
| Request sink dataflow | 🔴 `missing` | — | 3740 | — | — |
| Response shape extraction | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Sanitizer recognition | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Schema drift detection | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Taint sink detection | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Taint source detection | 🟢 `partial` | `2026-06-03` | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_elixir.go` | — |
| Template pattern catalog | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/links/template_pattern_pass.go`<br>`internal/substrate/template_pattern.go`<br>`internal/substrate/template_pattern_elixir.go` | Elixir template-pattern sniffer registered: i18n (gettext/dgettext), log_format (Logger.*), SQL literals via Ecto.Adapters.SQL |
| Vulnerability finding | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.elixir.framework.tesla ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
