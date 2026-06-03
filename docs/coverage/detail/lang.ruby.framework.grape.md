<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.ruby.framework.grape` — Grape

Auto-generated. Back to [summary](../summary.md).

- **Language:** [ruby](../by-language/ruby.md)
- **Category:** [http_framework](../by-category/http_framework.md)
- **Subcategory:** Backend HTTP
- **Capability cells:** 49

## Capabilities


### Routing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Endpoint deprecation versioning | 🔴 `missing` | — | 3628 | — | #3628 Ruby port (out of reach today): Grape's idioms — `version 'v2'` and `desc 'x', deprecated: true` — are extracted as SCOPE.Operation `endpoint` entities by the custom Grape extractor (internal/custom/ruby/grape_deep.go), NOT as the synthesised http_endpoint_definition producers the language-agnostic deprecation/api_version passes stamp. Crediting them requires promoting Grape to the synthesised http_endpoint_definition shape first (a separate route-synthesis feature), after which the existing rubyDeprecationVerdict + path-derived api_version would apply. |
| Endpoint pagination posture | 🔴 `missing` | `2026-06-02` | 3628 | `internal/engine/http_endpoint_pagination.go`<br>`internal/engine/http_endpoint_pagination_patterns.go`<br>`internal/engine/http_endpoint_pagination_test.go`<br>`internal/engine/http_endpoint_synthesis.go` | #3628: applyEndpointPagination stamps paginated/pagination_style/pagination_params via the cross-language parameters/parameter_schema fallback (limit+offset/page/cursor shape). No framework-specific pagination-class/ORM signal yet for this framework. |
| Endpoint response codes | 🔴 `missing` | — | 3818 | — | — |
| Endpoint synthesis | ✅ `full` | `2026-05-28` | — | `internal/engine/rules/ruby/frameworks/grape.yaml` | — |
| Handler attribution | ✅ `full` | `2026-05-28` | — | `internal/engine/rules/ruby/frameworks/grape.yaml` | — |
| Route extraction | ✅ `full` | `2026-05-30` | — | `internal/custom/ruby/grape_deep.go` | — |

### View

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| View rendering | 🔴 `missing` | — | view_rendering:#3628-not-yet-extracted | — | — |

### Auth

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Auth coverage | 🟢 `partial` | `2026-05-30` | — | `internal/custom/ruby/auth.go` | — |

### Validation

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DTO extraction | ✅ `full` | `2026-05-30` | — | `internal/custom/ruby/grape_deep.go`<br>`internal/custom/ruby/validation.go` | — |
| Request validation | ✅ `full` | `2026-05-30` | — | `internal/custom/ruby/grape_deep.go`<br>`internal/custom/ruby/validation.go` | — |

### Middleware

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Middleware coverage | 🟢 `partial` | — | — | `internal/custom/ruby/middleware.go` | Rack use, Rails before/after_action, Grape before/after hooks, Sinatra before/after blocks, Roda plugins, Cuba before/after. Part of #3282. |
| Rate limit stamping | 🟢 `partial` | `2026-06-03` | — | `internal/custom/ruby/rate_limit_endpoint.go`<br>`internal/custom/ruby/rate_limit_endpoint_test.go` | rack-attack 'Rack::Attack.throttle(name, limit: N, period: T)' detected and stamped as a SCOPE.Pattern/rate_limit marker (rate_limited/limit/period/rate_limit_name/rate_limit_source=rack-attack; literal period 1.minute->60 -> rate_limit '<N>/<secs>s'). Rack middleware applies to this Rack-based framework. PARTIAL: rack-attack throttles bind to a request discriminator (the block), not a named route, so the per-route binding is heuristic (rate_limit_scope=request); blocklist/safelist are not stamped as limits. Framework-native limiter idioms are future work. #4072 |

### Schema

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Type graph extraction | — `not_applicable` | — | — | — | GraphQL schema type→type graph (object-typed field -> referenced object type with list/nullable cardinality) is a GraphQL-only concept; this framework is not a GraphQL server, so it has no GraphQL object-type relationship graph. |

### Type System

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Enum extraction | — `not_applicable` | — | — | — | Ruby is dynamically typed — no enum keyword (duck typing idiom) |
| Interface extraction | — `not_applicable` | — | — | — | Ruby is dynamically typed — no interface keyword (duck typing idiom) |
| Type alias extraction | — `not_applicable` | — | — | — | Ruby is dynamically typed — no type keyword (duck typing idiom) |
| Type extraction | — `not_applicable` | — | — | — | Ruby dynamically typed; framework exposes no static type DSL |

### DI

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DI binding extraction | 🔴 `missing` | — | 3628 | — | — |
| DI injection point | 🔴 `missing` | — | 3628 | — | — |
| DI scope resolution | 🔴 `missing` | — | 3628 | — | — |

### Testing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Tests linkage | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/extractors/cross/testmap/frameworks.go` | — |

### Observability

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Log extraction | 🟢 `partial` | `2026-05-30` | backfill:dictionary-completeness | `internal/custom/ruby/observability.go` | — |
| Metric extraction | 🟢 `partial` | `2026-05-30` | backfill:dictionary-completeness | `internal/custom/ruby/observability.go` | — |
| Trace extraction | 🟢 `partial` | `2026-05-30` | backfill:dictionary-completeness | `internal/custom/ruby/observability.go` | — |

### Data

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|

### Substrate

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Confidence overlay | ✅ `full` | `2026-05-28` | — | `internal/graph/graph.go`<br>`internal/mcp/tools.go`<br>`internal/types/confidence.go` | — |
| Config consumption | ✅ `full` | `2026-06-02` | 3641 | `internal/extractor/config_key.go`<br>`internal/extractors/ruby/config_consumer.go`<br>`internal/extractors/ruby/config_consumer_test.go` | ENV[...], ENV.fetch -> config:<key> DEPENDS_ON_CONFIG edges (issue #3641) |
| Constant propagation | ✅ `full` | `2026-05-27` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/ruby.go`<br>`internal/substrate/substrate.go` | — |
| DB effect | 🟢 `partial` | `2026-05-28` | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_ruby.go` | — |
| Dead code detection | 🟢 `partial` | `2026-05-28` | — | `internal/links/reachability.go`<br>`internal/mcp/dead_code.go`<br>`internal/substrate/entry_points.go`<br>`internal/substrate/entry_points_ruby.go` | — |
| Def use chain extraction | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/links/def_use_pass.go`<br>`internal/substrate/def_use_ruby.go` | — |
| Env fallback recognition | ✅ `full` | `2026-05-27` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/ruby.go`<br>`internal/substrate/substrate.go` | — |
| Error flow | ✅ `full` | `2026-06-03` | — | `internal/extractor/exception_flow.go`<br>`internal/extractors/ruby/exception_flow.go`<br>`internal/extractors/ruby/exception_flow_test.go` | raise X / raise Mod::X -> THROWS; rescue A, B => e / method-level rescue / Rails rescue_from A, B, with: -> CATCHES; bare rescue catch-all + string raise + bare re-raise dropped (#3628) |
| Feature flag gating | 🔴 `missing` | — | feature_flag_gating:#3706-not-yet-extracted | — | — |
| Fs effect | 🟢 `partial` | `2026-05-28` | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_ruby.go` | — |
| HTTP effect | 🟢 `partial` | `2026-05-28` | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_ruby.go` | — |
| Import resolution quality | 🟢 `partial` | `2026-05-27` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/ruby.go`<br>`internal/substrate/substrate.go` | — |
| Module cycle detection | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/links/module_cycle_pass.go` | — |
| Mutation effect | 🟢 `partial` | `2026-05-28` | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_ruby.go` | — |
| Pure function tagging | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/links/pure_function_pass.go` | — |
| Reachability analysis | 🟢 `partial` | `2026-05-28` | — | `internal/links/reachability.go`<br>`internal/substrate/entry_points.go`<br>`internal/substrate/entry_points_ruby.go` | — |
| Request shape extraction | 🟢 `partial` | `2026-06-03` | 3951 | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_ruby.go`<br>`internal/substrate/payload_shapes_t2_test.go` | GENUINE-PARTIAL (#3951, was aspirational off #2771): the framework-agnostic Ruby payload-shape sniffer now binds Grape handlers via DSL anchors (scanRubyShapeHeaders) instead of dropping them at module scope (Grape uses no `def`). Request shape = Grape `params do; requires :name / optional :age end` (each leading symbol is a field, bound to the `params`/API-class header); also bare `params[:x]` reads inside route blocks. Test TestPayloadShapesRuby_GrapeRequiresAndExpose asserts requires/optional {name,age}. PARTIAL vs Rails full: no type→field-type mapping, no nested/group params, no shared `params` reuse across routes. DEPLOY-DEFERRED (daemon not rebuilt). |
| Request sink dataflow | 🟢 `partial` | `2026-06-03` | 3947 | `internal/links/dataflow_pass.go`<br>`internal/substrate/dataflow.go`<br>`internal/substrate/dataflow_ruby.go`<br>`internal/substrate/dataflow_ruby_test.go` | SCOPED request-input → sink DATA_FLOWS_TO (#3628 area #22, epic #3872): new Ruby sniffer (internal/substrate/dataflow_ruby.go) mirroring the python/jsts model — intra-method assignment tracking over `def…end` bodies + multi-hop (≤DataFlowMaxHops=3) local same-file method-call propagation, each bound by exact positional index, AND cross-file boundary emission continued by the links pass into resolved same-repo callees (continueDataFlowRuby). Sources: params[:x]/params['x'] (symbol/string keys), params.fetch(:x), Rails strong params params.require(:m).permit(:a,:b) with each permitted attribute as a field (recovered at user_params[:a] reads), request.body. Sinks: ActiveRecord write (Model.create/.update/.save/.new), raw SQL ActiveRecord::Base.connection.execute, response (render json:/plain:/redirect_to/send_*), outbound HTTP (Net::HTTP/Faraday/HTTParty/RestClient/Excon). HONEST-PARTIAL (precision-first): drops reassignment, dynamic keys (params[k]), embedded-arg, splat/keyword-arg call sites, recursion/cycle, the 4th hop, external/unresolved imports; whole-hash mass-assignment of a strong-params var flows with field="". DEPLOY-DEFERRED (daemon not rebuilt). Java/PHP request_sink_dataflow remain follow-up. Grape: params[:x] sources + DB/response/HTTP sinks (shared sniffer). |
| Response shape extraction | 🟢 `partial` | `2026-06-03` | 3951 | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_ruby.go`<br>`internal/substrate/payload_shapes_t2_test.go` | GENUINE-PARTIAL (#3951, was aspirational off #2771): Grape response shape = `expose :id / :name` declarations inside a `class X < Grape::Entity` body (bound to the Entity-class header via scanRubyShapeHeaders) + `json({...})` route-block helper. Test TestPayloadShapesRuby_GrapeRequiresAndExpose asserts expose {id,name}. PARTIAL vs Rails full: present/expose pairing is per-enclosing-block (no cross-file Entity resolution), no nested `expose … do` blocks, no conditional/if-exposed fields. DEPLOY-DEFERRED. |
| Sanitizer recognition | 🟢 `partial` | `2026-05-28` | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_ruby.go` | — |
| Schema drift detection | 🟢 `partial` | `2026-06-03` | 3951 | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_ruby.go` | Drift detection (internal/links/payload_drift.go) is framework-agnostic and now has genuine Grape request (requires/optional) + response (expose) shapes to compare (#3951) — previously the cell was credited but the Grape shapes never surfaced. PARTIAL: no Grape-specific drift fixture asserted end-to-end yet. |
| Taint sink detection | 🟢 `partial` | `2026-05-28` | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_ruby.go` | — |
| Taint source detection | 🟢 `partial` | `2026-05-28` | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_ruby.go` | — |
| Template pattern catalog | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/links/template_pattern_pass.go`<br>`internal/substrate/template_pattern_ruby.go` | — |
| Vulnerability finding | 🟢 `partial` | `2026-05-28` | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_ruby.go` | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.ruby.framework.grape ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
