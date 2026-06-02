<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.php.framework.symfony` — Symfony

Auto-generated. Back to [summary](../summary.md).

- **Language:** [php](../by-language/php.md)
- **Category:** [http_framework](../by-category/http_framework.md)
- **Subcategory:** Backend HTTP
- **Capability cells:** 38

## Capabilities


### Routing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Endpoint synthesis | ✅ `full` | `2026-05-28` | — | `internal/engine/http_endpoint_php_producer.go`<br>`internal/engine/rules/php/frameworks/symfony.yaml` | — |
| Handler attribution | ✅ `full` | `2026-05-28` | — | `internal/engine/http_endpoint_php_producer.go` | — |
| Route extraction | ✅ `full` | `2026-05-30` | — | `internal/custom/php/symfony.go` | PHP8 attribute routes with methods/name, @Route annotations, YAML routes (config/routes.yaml) with path+method extraction; class-level prefix support |

### Auth

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Auth coverage | ✅ `full` | `2026-05-30` | — | `internal/custom/php/symfony.go` | #[IsGranted] attribute, denyAccessUnlessGranted(), Voter/VoterInterface classes with voter attribute extraction, security.yaml access_control entries |

### Validation

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DTO extraction | ✅ `full` | `2026-05-30` | — | `internal/custom/php/symfony.go` | #[Assert\NotBlank]/#[Assert\Length]/#[Assert\Email] PHP8 attribute constraints on entity/DTO properties; DTO class detection (Request/DTO/Input/Data suffix) |
| Request validation | ✅ `full` | `2026-05-30` | — | `internal/custom/php/symfony.go` | Assert constraint extraction (PHP8 attr + annotation), ->validate($obj) programmatic call detection |

### Middleware

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Middleware coverage | ✅ `full` | `2026-05-30` | — | `internal/custom/php/symfony.go` | EventSubscriberInterface implementors with getSubscribedEvents() event name extraction; addListener/addSubscriber kernel event registration; kernel event names emitted as SCOPE.Pattern |

### Type System

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Enum extraction | ✅ `full` | — | — | `internal/extractors/php/php.go` | PHP 8.1+ backed and pure enums: tree-sitter enum_declaration → SCOPE.Schema/enum with case names (enum_members), backed values (enum_member_values), and backing type (enum_backing_type). Full language-level extraction, framework-independent. |
| Interface extraction | ✅ `full` | — | — | `internal/extractors/php/php.go` | interface_declaration → SCOPE.Component/interface with CONTAINS edges to all declared methods (dotted Interface.method naming). Framework-independent language-level extraction. |
| Type alias extraction | — `not_applicable` | — | — | `internal/extractors/php/php.go` | PHP has no native type alias syntax. @phpstan-type/@psalm-type docblock aliases exist as third-party static-analysis conventions only, not a language feature. not_applicable at the language level. |
| Type extraction | ✅ `full` | — | — | `internal/extractors/php/php.go` | class_declaration → SCOPE.Component/class and trait_declaration → SCOPE.Component/trait, both with CONTAINS edges to methods. Framework-independent language-level extraction via tree-sitter. |

### Testing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Tests linkage | ✅ `full` | — | — | `internal/extractors/cross/testmap/extractor_test.go`<br>`internal/extractors/cross/testmap/frameworks.go`<br>`internal/extractors/cross/testmap/resolver.go` | Deep PHPUnit/Pest linkage (#3399): test* prefix + #[Test] attribute + @test docblock detection; class-name subject derivation (UserServiceTest→UserService); instantiation body hints (new Foo()); Pest it()/test() blocks with uses(Class::class) subject extraction; TESTS edge emitted for all three PHPUnit forms and Pest DSL; Symfony-specific TestCase subclasses covered by phpunit class regex |

### Observability

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Log extraction | 🟢 `partial` | `2026-05-30` | backfill:dictionary-completeness | `internal/custom/php/frameworks.go`<br>`internal/custom/php/observability.go` | Per-call-site: PSR-3/Monolog injected $logger->level() / $this->logger->level() (LoggerInterface). Log::level() Laravel facade patterns also captured. Symfony-specific: $this->logger->log(LogLevel::X) calls. Use-declaration fallback for Monolog\Logger and Psr\Log\LoggerInterface use-statements. Import/call-site heuristic; no cross-file dataflow binding logger to handler. Stays partial. |
| Metric extraction | 🟢 `partial` | `2026-05-30` | backfill:dictionary-completeness | `internal/custom/php/frameworks.go`<br>`internal/custom/php/observability.go` | prometheus_client_php: new Counter/Gauge/Histogram/Summary, registerCounter/Gauge/Histogram call-sites. StatsD (League\StatsD, Domnikl\Statsd): $statsd->increment/gauge/timing/histogram with metric names. Import/call-site heuristic; no cross-file dataflow. Stays partial. |
| Trace extraction | 🟢 `partial` | `2026-05-30` | backfill:dictionary-completeness | `internal/custom/php/frameworks.go`<br>`internal/custom/php/observability.go` | OpenTelemetry PHP SDK: CachedInstrumentation/Globals::tracerProvider() bootstrap, $tracer->spanBuilder/startSpan('name') call-sites, $span lifecycle markers (setAttribute/addEvent/setStatus/end). Symfony Stopwatch: $stopwatch->start/stop/lap('name') event names. DDTrace: trace_function/trace_method. Import/call-site heuristic; no cross-file dataflow binding tracer to exporter. Stays partial. |

### Data

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|

### Substrate

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Confidence overlay | ✅ `full` | `2026-05-28` | — | `internal/graph/graph.go`<br>`internal/mcp/tools.go`<br>`internal/types/confidence.go` | — |
| Config consumption | 🔴 `missing` | — | 3641 | — | — |
| Constant propagation | ✅ `full` | `2026-05-27` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/php.go`<br>`internal/substrate/substrate.go` | — |
| DB effect | 🟢 `partial` | `2026-05-28` | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_php.go` | — |
| Dead code detection | 🟢 `partial` | `2026-05-28` | — | `internal/links/reachability.go`<br>`internal/mcp/dead_code.go`<br>`internal/substrate/entry_points.go`<br>`internal/substrate/entry_points_php.go` | Framework-agnostic reachability pass via php open-tag and public-function entry points; lifecycle hooks matched by name heuristic only — unified status across all PHP frameworks |
| Def use chain extraction | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/links/def_use_pass.go`<br>`internal/substrate/def_use_php.go` | PHP def-use sniffer registered in substrate; flows through language-agnostic def_use_pass |
| Env fallback recognition | ✅ `full` | `2026-05-27` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/php.go`<br>`internal/substrate/substrate.go` | — |
| Feature flag gating | 🔴 `missing` | — | feature_flag_gating:#3706-not-yet-extracted | — | — |
| Fs effect | 🟢 `partial` | `2026-05-28` | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_php.go` | — |
| HTTP effect | 🟢 `partial` | `2026-05-28` | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_php.go` | — |
| Import resolution quality | 🟢 `partial` | `2026-05-27` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/php.go`<br>`internal/substrate/substrate.go` | — |
| Module cycle detection | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/extractors/php/php.go`<br>`internal/links/module_cycle_pass.go` | Language-agnostic Tarjan SCC over IMPORTS edges; PHP namespace_use_declaration edges emitted by tree-sitter extractor |
| Mutation effect | 🟢 `partial` | `2026-05-28` | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_php.go` | — |
| Pure function tagging | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/links/pure_function_pass.go`<br>`internal/substrate/effect_sinks_php.go` | Language-agnostic pure-function tagging pass reads effect stamps from PHP effect_sinks substrate |
| Reachability analysis | 🟢 `partial` | `2026-05-28` | — | `internal/links/reachability.go`<br>`internal/substrate/entry_points.go`<br>`internal/substrate/entry_points_php.go` | — |
| Request shape extraction | 🟢 `partial` | `2026-05-28` | [link](https://github.com/cajasmota/archigraph/issues/2771) | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_php.go` | — |
| Response shape extraction | 🟢 `partial` | `2026-05-28` | [link](https://github.com/cajasmota/archigraph/issues/2771) | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_php.go` | — |
| Sanitizer recognition | 🟢 `partial` | `2026-05-28` | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_php.go` | — |
| Schema drift detection | 🟢 `partial` | `2026-05-28` | [link](https://github.com/cajasmota/archigraph/issues/2771) | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_php.go` | — |
| Taint sink detection | 🟢 `partial` | `2026-05-28` | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_php.go` | — |
| Taint source detection | 🟢 `partial` | `2026-05-28` | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_php.go` | — |
| Template pattern catalog | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/links/constant_propagation.go`<br>`internal/substrate/template_pattern_php.go` | PHP template-pattern sniffer registered; covers i18n trans(), log literals, SQL strings |
| Vulnerability finding | 🟢 `partial` | `2026-05-28` | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_php.go` | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.php.framework.symfony ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
