<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.php.framework.laravel` — Laravel

Auto-generated. Back to [summary](../summary.md).

- **Language:** [php](../by-language/php.md)
- **Category:** [http_framework](../by-category/http_framework.md)
- **Subcategory:** Backend HTTP
- **Capability cells:** 42

## Capabilities


### Routing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Endpoint synthesis | ✅ `full` | `2026-05-28` | — | `internal/engine/http_endpoint_php_producer.go`<br>`internal/engine/rules/php/frameworks/laravel.yaml` | — |
| Handler attribution | ✅ `full` | `2026-05-28` | — | `internal/engine/http_endpoint_php_producer.go` | — |
| Route extraction | ✅ `full` | `2026-05-30` | — | `internal/engine/http_endpoint_php_producer.go`<br>`internal/engine/http_endpoint_php_producer_lrroute_test.go` | Regex-based per-framework route extraction covering HTTP method routes, resource routes, URL rules |

### Auth

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Auth coverage | ✅ `full` | — | — | `internal/custom/php/laravel_authval.go` | Deep Laravel auth extraction: route middleware (auth/sanctum/api guards), Gate::define/authorize/policy, $this->authorize(), Policy classes+methods, @can/@cannot/@auth/@guest Blade directives, auth() helper, Auth:: facade, Sanctum/Passport HasApiTokens traits |

### Validation

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DTO extraction | ✅ `full` | — | — | `internal/custom/php/laravel_authval.go` | FormRequest subclasses extracted with per-field validation rules from rules() method body; authorize()/messages()/prepareForValidation hooks detected |
| Request validation | ✅ `full` | — | — | `internal/custom/php/laravel_authval.go` | Inline $request->validate([field=>rules]) with per-field extraction, Validator::make(), $this->validate(); withValidator/prepareForValidation hooks |

### Middleware

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Middleware coverage | ✅ `full` | — | — | `internal/custom/php/laravel_authval.go` | Full Kernel.php coverage ($middleware/$middlewareGroups/$routeMiddleware/$middlewareAliases arrays with per-entry class+alias extraction); custom middleware class via handle(Closure $next) with terminate() detection; route ->middleware() attachment and ->withoutMiddleware() exclusion |

### Type System

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Enum extraction | ✅ `full` | — | — | `internal/extractors/php/php.go` | PHP 8.1+ backed and pure enums: tree-sitter enum_declaration → SCOPE.Schema/enum with case names (enum_members), backed values (enum_member_values), and backing type (enum_backing_type). Full language-level extraction, framework-independent. |
| Interface extraction | ✅ `full` | — | — | `internal/extractors/php/php.go` | interface_declaration → SCOPE.Component/interface with CONTAINS edges to all declared methods (dotted Interface.method naming). Framework-independent language-level extraction. |
| Type alias extraction | — `not_applicable` | — | — | `internal/extractors/php/php.go` | PHP has no native type alias syntax. @phpstan-type/@psalm-type docblock aliases exist as third-party static-analysis conventions only, not a language feature. not_applicable at the language level. |
| Type extraction | ✅ `full` | — | — | `internal/extractors/php/php.go` | class_declaration → SCOPE.Component/class and trait_declaration → SCOPE.Component/trait, both with CONTAINS edges to methods. Framework-independent language-level extraction via tree-sitter. |

### DI

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DI binding extraction | 🟢 `partial` | `2026-06-02` | — | `internal/custom/php/di_graph.go`<br>`internal/custom/php/di_graph_test.go` | Laravel service-container bindings $this->app->bind/singleton/scoped(Interface::class, Impl::class) (and app()/App/$app receiver forms) emit BINDS(Interface->Impl). Value-asserted TestPhpDI_LaravelBind (PaymentInterface->StripePayment, CacheInterface->RedisCache namespaced). Negative TestPhpDI_LaravelClosureBindNoEdge (closure-form bind => no edge). PARTIAL: closure/dynamic bindings + string-keyed abstracts skipped (honest-partial). |
| DI injection point | 🟢 `partial` | `2026-06-02` | — | `internal/custom/php/di_graph.go`<br>`internal/custom/php/di_graph_test.go` | Constructor type-hint autowiring: a class __construct(PaymentInterface $p, ...) emits INJECTED_INTO(PaymentInterface->ClassName) per class-typed param. Value-asserted TestPhpDI_ConstructorInjection (PaymentInterface->OrderController, LoggerInterface->OrderController). Negative TestPhpDI_ScalarParamNoEdge (string/int/array hints => no edge). PARTIAL: method/property injection + contextual bindings not linked; impl resolves cross-file via resolver. |
| DI scope resolution | 🟢 `partial` | `2026-06-02` | — | `internal/custom/php/di_graph.go`<br>`internal/custom/php/di_graph_test.go` | Binding lifetime captured as bind_kind (bind|singleton|scoped) property on the Laravel BINDS edge. PARTIAL: no separate scope-resolution graph; contextual/tagged bindings not modeled. |

### Testing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Tests linkage | ✅ `full` | — | — | `internal/extractors/cross/testmap/extractor_test.go`<br>`internal/extractors/cross/testmap/frameworks.go`<br>`internal/extractors/cross/testmap/resolver.go` | Deep PHPUnit/Pest linkage (#3399): test* prefix + #[Test] attribute + @test docblock detection; class-name subject derivation (UserServiceTest→UserService); instantiation body hints (new Foo()); Pest it()/test() blocks with uses(Class::class) subject extraction and describe() PascalCase subject fallback; Laravel feature test HTTP helpers (->get/post/assertStatus) filtered as stopwords; path hints for tests/ directory; TESTS edge emitted for all three PHPUnit forms and Pest DSL |

### Observability

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Log extraction | 🟢 `partial` | `2026-05-30` | backfill:dictionary-completeness | `internal/custom/php/frameworks.go`<br>`internal/custom/php/observability.go` | Per-call-site: Log::info/warning/error/debug/critical/alert/emergency/notice (facade + backslash form), Log::channel('name'), logger()->level() helper, logger('msg') shorthand, PSR-3/Monolog injected $logger->level(), $this->logger->level() Symfony-style injected LoggerInterface. Use-declaration fallback. Import/call-site heuristic; no cross-file dataflow binding logger to handler. Stays partial. |
| Metric extraction | 🟢 `partial` | `2026-05-30` | backfill:dictionary-completeness | `internal/custom/php/frameworks.go`<br>`internal/custom/php/observability.go` | prometheus_client_php: new Counter/Gauge/Histogram/Summary, registerCounter/Gauge/Histogram call-sites. StatsD (League StatsD, Domnikl Statsd): $statsd->increment/gauge/timing/histogram with metric names. Laravel Metrics facade: Metrics::counter/gauge/histogram. Use-declaration fallback. Import/call-site heuristic; no cross-file dataflow. Stays partial. |
| Trace extraction | 🟢 `partial` | `2026-05-30` | backfill:dictionary-completeness | `internal/custom/php/frameworks.go`<br>`internal/custom/php/observability.go` | OpenTelemetry PHP SDK: CachedInstrumentation/Globals::tracerProvider() bootstrap, $tracer->spanBuilder/startSpan('name'), $span->setAttribute/addEvent/setStatus/end lifecycle. Symfony Stopwatch: $stopwatch->start/stop/lap('name') event names. DDTrace: trace_function/trace_method. Use-declaration fallback. Import/call-site heuristic; no cross-file dataflow binding tracer to exporter. Stays partial. |

### Data

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|

### Substrate

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Confidence overlay | ✅ `full` | `2026-05-28` | — | `internal/graph/graph.go`<br>`internal/mcp/tools.go`<br>`internal/types/confidence.go` | — |
| Config consumption | ✅ `full` | `2026-06-02` | 3641 | `internal/extractor/config_key.go`<br>`internal/extractors/php/config_consumer.go`<br>`internal/extractors/php/config_consumer_test.go` | getenv/$_ENV + Laravel env()/config() -> config:<key> DEPENDS_ON_CONFIG edges (issue #3641) |
| Constant propagation | ✅ `full` | `2026-05-27` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/php.go`<br>`internal/substrate/substrate.go` | — |
| DB effect | 🟢 `partial` | `2026-05-28` | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_php.go` | — |
| Dead code detection | 🟢 `partial` | `2026-05-28` | — | `internal/links/reachability.go`<br>`internal/mcp/dead_code.go`<br>`internal/substrate/entry_points.go`<br>`internal/substrate/entry_points_php.go` | Framework-agnostic reachability pass via php open-tag and public-function entry points; lifecycle hooks matched by name heuristic only |
| Def use chain extraction | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/links/def_use_pass.go`<br>`internal/substrate/def_use_php.go` | PHP def-use sniffer registered in substrate; flows through language-agnostic def_use_pass |
| Env fallback recognition | ✅ `full` | `2026-05-27` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/php.go`<br>`internal/substrate/substrate.go` | — |
| Feature flag gating | 🔴 `missing` | — | feature_flag_gating:#3706-not-yet-extracted | — | — |
| Fs effect | 🟢 `partial` | `2026-05-28` | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_php.go` | — |
| HTTP effect | 🟢 `partial` | `2026-05-28` | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_php.go` | — |
| Import resolution quality | 🟢 `partial` | `2026-05-27` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/php.go`<br>`internal/substrate/substrate.go` | — |
| Module cycle detection | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/extractors/php/php.go`<br>`internal/links/module_cycle_pass.go` | Language-agnostic Tarjan SCC over IMPORTS edges; PHP namespace_use_declaration edges emitted by tree-sitter extractor |
| Mutation effect | 🟢 `partial` | `2026-05-28` | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_php.go` | — |
| Pure function tagging | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/links/pure_function_pass.go`<br>`internal/substrate/effect_sinks_php.go` | Language-agnostic pure-function tagging pass reads effect stamps from PHP effect_sinks substrate |
| Reachability analysis | 🟢 `partial` | `2026-05-28` | — | `internal/links/reachability.go`<br>`internal/substrate/entry_points_php.go` | — |
| Request shape extraction | ✅ `full` | `2026-05-28` | [link](https://github.com/cajasmota/archigraph/issues/2771) | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_php.go` | — |
| Request sink dataflow | 🔴 `missing` | — | 3740 | — | — |
| Response shape extraction | ✅ `full` | `2026-05-28` | [link](https://github.com/cajasmota/archigraph/issues/2771) | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_php.go` | — |
| Sanitizer recognition | 🟢 `partial` | `2026-05-28` | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_php.go` | — |
| Schema drift detection | ✅ `full` | `2026-05-28` | [link](https://github.com/cajasmota/archigraph/issues/2771) | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_php.go` | — |
| Taint sink detection | 🟢 `partial` | `2026-05-28` | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_php.go` | — |
| Taint source detection | 🟢 `partial` | `2026-05-28` | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_php.go` | — |
| Template pattern catalog | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/links/constant_propagation.go`<br>`internal/substrate/template_pattern_php.go` | PHP template-pattern sniffer registered; covers i18n trans(), log literals, SQL strings |
| Vulnerability finding | 🟢 `partial` | `2026-05-28` | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_php.go` | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.php.framework.laravel ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
