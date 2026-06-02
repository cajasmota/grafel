<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.ruby.framework.sinatra` — Sinatra

Auto-generated. Back to [summary](../summary.md).

- **Language:** [ruby](../by-language/ruby.md)
- **Category:** [http_framework](../by-category/http_framework.md)
- **Subcategory:** Backend HTTP
- **Capability cells:** 50

## Capabilities


### Routing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Endpoint deprecation versioning | 🔴 `missing` | — | 3628 | — | — |
| Endpoint pagination posture | 🔴 `missing` | `2026-06-02` | 3628 | `internal/engine/http_endpoint_pagination.go`<br>`internal/engine/http_endpoint_pagination_patterns.go`<br>`internal/engine/http_endpoint_pagination_test.go`<br>`internal/engine/http_endpoint_synthesis.go` | #3628: applyEndpointPagination stamps paginated/pagination_style/pagination_params via the cross-language parameters/parameter_schema fallback (limit+offset/page/cursor shape). No framework-specific pagination-class/ORM signal yet for this framework. |
| Endpoint response codes | 🔴 `missing` | — | 3818 | — | — |
| Endpoint synthesis | ✅ `full` | `2026-05-28` | — | `internal/engine/http_endpoint_ruby_producer.go`<br>`internal/engine/rules/ruby/frameworks/sinatra.yaml` | — |
| Handler attribution | ✅ `full` | `2026-05-28` | — | `internal/engine/http_endpoint_ruby_producer.go` | — |
| Route extraction | ✅ `full` | `2026-05-30` | — | `internal/custom/ruby/routes.go`<br>`internal/custom/ruby/routes_test.go`<br>`internal/custom/ruby/sinatra_deep.go`<br>`internal/custom/ruby/sinatra_deep_test.go` | Extracts all Sinatra verb blocks (get/post/put/patch/delete/head/options) with exact route path and HTTP method. Covers class-based Sinatra::Base/Sinatra::Application and standalone apps (require 'sinatra'). Named params /:id, splat /*path, regex routes all emitted. Full parity with TS/JS Express route_extraction. Closes #3344. |

### View

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| View rendering | 🔴 `missing` | — | view_rendering:#3628-not-yet-extracted | — | — |

### Auth

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Auth coverage | 🟢 `partial` | `2026-05-30` | — | `internal/custom/ruby/auth.go`<br>`internal/custom/ruby/sinatra_deep.go`<br>`internal/custom/ruby/sinatra_deep_test.go` | Detects Sinatra-idiomatic auth: before+halt 4xx guard, protected! helper, halt status code call-sites. Warden::Manager and Rack::Auth::Basic/Digest covered via shared auth.go. Heuristic regex; no cross-file dataflow. Closes #3344. |

### Validation

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DTO extraction | 🟢 `partial` | `2026-05-30` | backfill:dictionary-completeness | `internal/custom/ruby/validation.go` | — |
| Request validation | 🟢 `partial` | `2026-05-30` | backfill:dictionary-completeness | `internal/custom/ruby/sinatra_deep.go`<br>`internal/custom/ruby/sinatra_deep_test.go`<br>`internal/custom/ruby/validation.go` | Detects sinatra-param gem param :name declarations with type annotation. Generic params[:x] access covered by validation.go. No dry-validation or schema-level validation. Closes #3344. |

### Middleware

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Middleware coverage | ✅ `full` | `2026-05-30` | — | `internal/custom/ruby/middleware.go`<br>`internal/custom/ruby/middleware_test.go`<br>`internal/custom/ruby/sinatra_deep.go`<br>`internal/custom/ruby/sinatra_deep_test.go` | Covers: use Rack::X Rack middleware, before do / after do filters, before '/path' do scoped filters, helpers do blocks, custom Rack middleware class detection (initialize(app)+call(env)). Full idiomatic Sinatra middleware surface. Closes #3344. |
| Rate limit stamping | 🔴 `missing` | — | [link](https://github.com/cajasmota/archigraph/issues/3778) | — | endpoint rate-limit / throttle stamping not yet implemented for this framework; the #3628 child shipped express-rate-limit (JS/TS) + slowapi/django-ratelimit/flask-limiter/DRF (Python). express-slow-down-compatible / framework-native limiters for this framework are future work. |

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
| Tests linkage | 🟢 `partial` | `2026-05-30` | backfill:dictionary-completeness | `internal/custom/ruby/sinatra_deep.go`<br>`internal/custom/ruby/sinatra_deep_test.go`<br>`internal/extractors/cross/testmap/frameworks.go` | Detects rack-test specs via include Rack::Test::Methods, emits test_framework signal entity and per-call-site test_call entities (get '/path', post '/path' inside specs). RSpec+Minitest both supported. No cross-file TESTS edge resolution to production route entities (same limitation as Rails tests_linkage). Closes #3344. |

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
| Error flow | 🔴 `missing` | — | 3628 | — | — |
| Feature flag gating | 🔴 `missing` | — | feature_flag_gating:#3706-not-yet-extracted | — | — |
| Fs effect | 🟢 `partial` | `2026-05-28` | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_ruby.go` | — |
| HTTP effect | 🟢 `partial` | `2026-05-28` | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_ruby.go` | — |
| Import resolution quality | 🟢 `partial` | `2026-05-27` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/ruby.go`<br>`internal/substrate/substrate.go` | — |
| Module cycle detection | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/links/module_cycle_pass.go` | — |
| Mutation effect | 🟢 `partial` | `2026-05-28` | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_ruby.go` | — |
| Pure function tagging | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/links/pure_function_pass.go` | — |
| Reachability analysis | 🟢 `partial` | `2026-05-28` | — | `internal/links/reachability.go`<br>`internal/substrate/entry_points.go`<br>`internal/substrate/entry_points_ruby.go` | — |
| Request shape extraction | 🟢 `partial` | `2026-05-28` | [link](https://github.com/cajasmota/archigraph/issues/2771) | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_ruby.go` | — |
| Request sink dataflow | 🟢 `partial` | `2026-06-03` | 3947 | `internal/links/dataflow_pass.go`<br>`internal/substrate/dataflow.go`<br>`internal/substrate/dataflow_ruby.go`<br>`internal/substrate/dataflow_ruby_test.go` | SCOPED request-input → sink DATA_FLOWS_TO (#3628 area #22, epic #3872): new Ruby sniffer (internal/substrate/dataflow_ruby.go) mirroring the python/jsts model — intra-method assignment tracking over `def…end` bodies + multi-hop (≤DataFlowMaxHops=3) local same-file method-call propagation, each bound by exact positional index, AND cross-file boundary emission continued by the links pass into resolved same-repo callees (continueDataFlowRuby). Sources: params[:x]/params['x'] (symbol/string keys), params.fetch(:x), Rails strong params params.require(:m).permit(:a,:b) with each permitted attribute as a field (recovered at user_params[:a] reads), request.body. Sinks: ActiveRecord write (Model.create/.update/.save/.new), raw SQL ActiveRecord::Base.connection.execute, response (render json:/plain:/redirect_to/send_*), outbound HTTP (Net::HTTP/Faraday/HTTParty/RestClient/Excon). HONEST-PARTIAL (precision-first): drops reassignment, dynamic keys (params[k]), embedded-arg, splat/keyword-arg call sites, recursion/cycle, the 4th hop, external/unresolved imports; whole-hash mass-assignment of a strong-params var flows with field="". DEPLOY-DEFERRED (daemon not rebuilt). Java/PHP request_sink_dataflow remain follow-up. Sinatra: params['x'] sources + render/erb response and DB/HTTP sinks (shared sniffer). |
| Response shape extraction | 🟢 `partial` | `2026-05-28` | [link](https://github.com/cajasmota/archigraph/issues/2771) | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_ruby.go` | — |
| Sanitizer recognition | 🟢 `partial` | `2026-05-28` | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_ruby.go` | — |
| Schema drift detection | 🟢 `partial` | `2026-05-28` | [link](https://github.com/cajasmota/archigraph/issues/2771) | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_ruby.go` | — |
| Taint sink detection | 🟢 `partial` | `2026-05-28` | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_ruby.go` | — |
| Taint source detection | 🟢 `partial` | `2026-05-28` | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_ruby.go` | — |
| Template pattern catalog | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/links/template_pattern_pass.go`<br>`internal/substrate/template_pattern_ruby.go` | — |
| Vulnerability finding | 🟢 `partial` | `2026-05-28` | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_ruby.go` | — |

## Framework-specific

### Integration

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Client consumes API | 🟢 `partial` | `2026-06-02` | — | `internal/engine/http_endpoint_ruby_client.go`<br>`internal/engine/http_endpoint_ruby_client_test.go`<br>`internal/links/http_pass.go` | Outbound HTTP-client synthesis (synthesizeRubyClient), language-wide (not framework-specific to this record): per call site emits a consumer http_endpoint_call http:<VERB>:<path> + FETCHES edge, cross-repo-linked to server routes by links/http_pass.go on the byte-identical synthetic id. Covers Net::HTTP (class + instance + start-block forms), Faraday (Faraday.<verb> + conn.<verb>), HTTParty (class + include-mixin form), RestClient (class + Resource.new(url).<verb>); absolute URLs host-stripped to path; ENV['X']+'/path' concat -> runtime_dynamic. Value-asserting tests in http_endpoint_ruby_client_test.go. Honest-partial: fully-dynamic URLs skipped (no fabricated path). |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.ruby.framework.sinatra ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
