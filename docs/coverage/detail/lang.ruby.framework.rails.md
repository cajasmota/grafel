<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.ruby.framework.rails` — Ruby on Rails

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
| Endpoint response codes | 🟢 `partial` | `2026-06-03` | 3818 | `internal/engine/http_endpoint_ruby_producer.go`<br>`internal/engine/route_synth_provenance_3842_test.go`<br>`internal/frameworks/routes/contracts.go`<br>`internal/frameworks/routes/contracts_test.go` | #3842 (T10): resources/resource synthesized routes now carry provenance=framework_synthesized + per-verb effective contract from frameworks/routes - create->201, destroy->204 No Content, index/show/update->200, with documented error_statuses (404/422). Stamped via emitResource on the canonical http:<VERB>:<path> synthetic. Value-asserting test TestRouteSynth_Rails_Resources_Provenance asserts exact verb+action+status. Honest-partial remainder: EXPLICIT verb routes (get/post to:'c#a') are not body-parsed for a controller render status: code; only the resourceful-macro family is contracted. |
| Endpoint synthesis | ✅ `full` | `2026-05-28` | — | `internal/engine/http_endpoint_ruby_producer.go`<br>`internal/engine/rules/ruby/frameworks/ruby_on_rails.yaml` | — |
| Handler attribution | ✅ `full` | `2026-05-28` | — | `internal/engine/http_endpoint_ruby_producer.go` | — |
| Route extraction | ✅ `full` | `2026-05-30` | — | `internal/custom/ruby/rails_routes.go`<br>`internal/custom/ruby/rails_routes_test.go`<br>`internal/engine/rules/ruby/frameworks/ruby_on_rails.yaml` | Deep config/routes.rb DSL extractor (custom_ruby_rails_routes): resources->7 RESTful routes + resource->6 singular, nested resources path composition (/photos/:photo_id/comments), namespace + scope + scope module: prefixing, member/collection (+inline on:), only:/except: filters, root, get/post/put/patch/delete to:'c#a', match via:, mount engines, concern/concerns: expansion. Each route emits resolved full path + HTTP method + controller#action handler with CALLS structural-ref to the action method. Value-asserting tests in rails_routes_test.go assert exact path+method+handler sets (TestRailsRoutes_ResourcesSevenRESTful, _NestedResources, _Namespace, _ScopePath/_ScopeModule, _MemberCollection, _OnlyFilter/_ExceptFilter, _Root, _MatchVia, _Mount, _Concerns, _RealisticCombined). Honest remainder: constraints blocks recorded but not expanded; direct/resolve URL helpers not modelled. |

### View

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| View rendering | 🟢 `partial` | `2026-06-02` | 3628 | `internal/extractor/template_render.go`<br>`internal/extractors/ruby/template_render.go`<br>`internal/extractors/ruby/template_render_test.go` | explicit render 'path' / template:/partial: -> RENDERS SCOPE.Template; symbol (render :index) + implicit-convention renders honest-skipped; dynamic names dropped (#3628) |

### Auth

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Auth coverage | ✅ `full` | `2026-05-30` | — | `internal/custom/ruby/auth.go`<br>`internal/custom/ruby/auth_deep_test.go`<br>`internal/custom/ruby/controller_auth.go`<br>`internal/custom/ruby/controller_auth_test.go` | #3734 endpoint-protection: controller_auth.go (custom_ruby_controller_auth) resolves a controller-level before_action :authenticate_user! (Devise) / :require_login / CanCanCan load_and_authorize_resource and stamps the #3696 flat contract (auth_required/auth_method/auth_guard/auth_confidence) on one SCOPE.Operation/endpoint per controller ACTION (controller#action handler); only:/except: + skip_before_action honoured; per-action Pundit authorize -> MEDIUM. Closes the prior honest remainder (which actions a controller-level before_action protects). Tests TestControllerAuth* incl negative _Unprotected. Plus deep Rails auth (Devise/Pundit/CanCanCan) extraction to TS/JS bar. Devise: devise_for route registration, authenticate_<model>! before_action with mechanism+auth_required, devise modules with authenticatable flag, <model>_signed_in? helpers, require_login. Pundit: class FooPolicy name extraction, per-action (update?/create?/show?) entity with action+policy_class properties, authorize calls with mechanism=pundit+auth_required=true, include Pundit::Authorization. CanCanCan: Ability class sentinel, per-rule can/cannot :action Resource with action+resource+permission+in_ability_class properties, authorize! and load_and_authorize_resource with mechanism=cancancan. General: before_action :require_auth/:check_authentication/:verify_auth. Value-asserting tests in auth_deep_test.go assert SPECIFIC properties (mechanism, action, resource, auth_required, authenticatable, policy_class) across 13 test cases covering all four frameworks plus combined scenarios. Honest remainder: cross-file dataflow (e.g. inferring which controller actions are protected by a controller-level before_action) and roles/scopes extraction not modelled. |

### Validation

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DTO extraction | ✅ `full` | `2026-05-30` | — | `internal/custom/ruby/validation.go`<br>`internal/custom/ruby/validation_deep.go`<br>`internal/custom/ruby/validation_deep_test.go` | Deep Rails dto_extraction: params.require(:model).permit(...) now emits per-field sp_field:<param>:<field> entities (scalar/array/nested) with permit_type prop. with_options blocks supported. Value-asserting tests in validation_deep_test.go assert exact param+field+permit_type. Closes #3340. |
| Request validation | ✅ `full` | `2026-05-30` | — | `internal/custom/ruby/validation.go`<br>`internal/custom/ruby/validation_deep.go`<br>`internal/custom/ruby/validation_deep_test.go` | Deep Rails request_validation: validates :field, validators with full option capture emits railsval:<field>:<validator> entities with validator_options prop. Classic validates_*_of with options → railsval_classic:<macro>:<field>. with_options blocks → railsval_wo:<field>:<validator> with inherited_options. Value-asserting tests assert exact attribute+validator+options. Closes #3340. |

### Middleware

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Middleware coverage | ✅ `full` | — | — | `internal/custom/ruby/middleware.go`<br>`internal/custom/ruby/middleware_test.go` | — |
| Rate limit stamping | ✅ `full` | `2026-06-03` | — | `internal/custom/ruby/rate_limit_endpoint.go`<br>`internal/custom/ruby/rate_limit_endpoint_test.go` | Rails 8 ActionController 'rate_limit to: N, within: T' stamped per controller action (rate_limited/rate_limit/rate_limit_scope=route/rate_limit_source=rate_limit), honouring only:/except:; literal window (1.minute->60) resolved to '<N>/<secs>s', config-driven window honest-partial (rate omitted). Plus app-wide rack-attack 'Rack::Attack.throttle(name, limit:, period:)' markers carrying limit/period/rate_limit_name. #4072 |

### Schema

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Type graph extraction | — `not_applicable` | — | — | — | GraphQL schema type→type graph (object-typed field -> referenced object type with list/nullable cardinality) is a GraphQL-only concept; this framework is not a GraphQL server, so it has no GraphQL object-type relationship graph. |

### Type System

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Enum extraction | 🟢 `partial` | `2026-06-02` | — | `internal/extractor/enum_valueset.go`<br>`internal/extractors/ruby/enum_valueset.go` | Ruby is dynamically typed (no enum keyword), but Rails ActiveRecord declares value-sets via the enum DSL. buildRailsEnumValueSet emits a value-carrying SCOPE.Enum node per declaration (kind_hint=rails_enum): enum status: { active: 0, archived: 1 } -> values active=0, archived=1; array form enum priority: [:low, :high] -> members value-less; Rails 7 positional enum :state, {...} form supported. Non-literal/dynamic args skipped (honest-partial). |
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
| Tests linkage | ✅ `full` | `2026-05-30` | — | `internal/extractors/cross/testmap/extractor.go`<br>`internal/extractors/cross/testmap/extractor_test.go`<br>`internal/extractors/cross/testmap/frameworks.go`<br>`internal/extractors/cross/testmap/resolver.go` | Deep RSpec+Minitest linkage (#3342): (1) RSpec — detectRSpec extracts describe-constant subject (rspecDescribeConstRE); each it/specify block carries describeSubject; resolveCalls Pass 3a emits medium-confidence TESTS edge to described class when no direct call; high wins on explicit calls. (2) Minitest/ActiveSupport::TestCase — detectMinitest handles DSL `test 'desc' do` and `def test_*`; railsMinitestSubjectFromClass strips Test suffix (UserTest→User). (3) Rails path conventions: spec/models/user_spec.rb→app/models/user.rb/User; spec/controllers/users_controller_spec.rb→UsersController; test/models/user_test.rb→app/models/user.rb/User via railsTestCamelCase. (4) Extended RSpec/Minitest stopwords (be_*, have_*, assert_*, Rails HTTP verbs). 16 value-asserting tests prove linkage. Remainder: shared_examples/it_behaves_like not modelled; request specs without describe constant fall back to low-confidence naming convention. |

### Observability

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Log extraction | 🟢 `partial` | `2026-05-30` | backfill:dictionary-completeness | `internal/custom/ruby/observability.go`<br>`internal/custom/ruby/observability_test.go` | Detects Rails.logger.{debug,info,warn,error,fatal}, logger.tagged tagged-block, ActiveSupport::TaggedLogging.new, lograge require+config. Import/call-site heuristic; no cross-file dataflow. Stays partial. |
| Metric extraction | 🟢 `partial` | `2026-05-30` | backfill:dictionary-completeness | `internal/custom/ruby/observability.go`<br>`internal/custom/ruby/observability_test.go` | Detects Yabeda.configure block + Yabeda.counter/gauge/histogram call-sites, prometheus-client Counter/Gauge/Histogram, Datadog::Statsd.new + call-sites, StatsD gem. Import/call-site heuristic; no cross-file dataflow. Stays partial. |
| Trace extraction | 🟢 `partial` | `2026-05-30` | backfill:dictionary-completeness | `internal/custom/ruby/observability.go`<br>`internal/custom/ruby/observability_test.go` | Detects ActiveSupport::Notifications.instrument+subscribe event names, OpenTelemetry in_span, ddtrace/Datadog::Tracing.trace, Skylight.instrument, OpenTracing. Import/call-site heuristic; no cross-file dataflow. Stays partial. |

### Data

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|

### Substrate

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Confidence overlay | ✅ `full` | `2026-05-28` | — | `internal/graph/graph.go`<br>`internal/mcp/tools.go`<br>`internal/types/confidence.go` | — |
| Config consumption | 🟢 `partial` | `2026-06-02` | 3641 | `internal/extractor/config_key.go`<br>`internal/extractors/ruby/config_consumer.go`<br>`internal/extractors/ruby/config_consumer_test.go` | ENV[...]/ENV.fetch -> config:<key> covered; Rails.application.credentials not yet extracted (issue #3641) |
| Constant propagation | ✅ `full` | `2026-05-27` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/ruby.go`<br>`internal/substrate/substrate.go` | — |
| DB effect | 🟢 `partial` | `2026-05-28` | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_ruby.go` | — |
| Dead code detection | 🟢 `partial` | `2026-05-28` | — | `internal/links/reachability.go`<br>`internal/substrate/entry_points_ruby.go` | — |
| Def use chain extraction | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/links/def_use_pass.go`<br>`internal/substrate/def_use_ruby.go` | — |
| Env fallback recognition | ✅ `full` | `2026-05-27` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/ruby.go`<br>`internal/substrate/substrate.go` | — |
| Error flow | ✅ `full` | `2026-06-03` | — | `internal/extractor/exception_flow.go`<br>`internal/extractors/ruby/exception_flow.go`<br>`internal/extractors/ruby/exception_flow_test.go` | raise X / raise Mod::X -> THROWS; rescue A, B => e / method-level rescue / Rails rescue_from A, B, with: -> CATCHES; bare rescue catch-all + string raise + bare re-raise dropped (#3628) |
| Feature flag gating | ✅ `full` | `2026-06-02` | 3706 | `internal/engine/feature_flag_edges.go`<br>`internal/engine/feature_flag_edges_test.go`<br>`internal/engine/orm_queries.go` | flag-check call sites -> feature:<key> + GATED_BY edge (LaunchDarkly/Unleash/Unleash-React/OpenFeature/Flipper/Flagsmith/Split.io/generic) |
| Fs effect | 🟢 `partial` | `2026-05-28` | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_ruby.go` | — |
| HTTP effect | 🟢 `partial` | `2026-05-28` | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_ruby.go` | — |
| Import resolution quality | 🟢 `partial` | `2026-05-27` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/ruby.go`<br>`internal/substrate/substrate.go` | — |
| Module cycle detection | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/links/module_cycle_pass.go` | — |
| Mutation effect | 🟢 `partial` | `2026-05-28` | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_ruby.go` | — |
| Pure function tagging | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/links/pure_function_pass.go` | — |
| Reachability analysis | 🟢 `partial` | `2026-05-28` | — | `internal/links/reachability.go`<br>`internal/substrate/entry_points_ruby.go` | — |
| Request shape extraction | ✅ `full` | `2026-05-28` | [link](https://github.com/cajasmota/archigraph/issues/2771) | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_ruby.go` | — |
| Request sink dataflow | 🟢 `partial` | `2026-06-03` | 3947 | `internal/links/dataflow_pass.go`<br>`internal/substrate/dataflow.go`<br>`internal/substrate/dataflow_ruby.go`<br>`internal/substrate/dataflow_ruby_test.go` | SCOPED request-input → sink DATA_FLOWS_TO (#3628 area #22, epic #3872): new Ruby sniffer (internal/substrate/dataflow_ruby.go) mirroring the python/jsts model — intra-method assignment tracking over `def…end` bodies + multi-hop (≤DataFlowMaxHops=3) local same-file method-call propagation, each bound by exact positional index, AND cross-file boundary emission continued by the links pass into resolved same-repo callees (continueDataFlowRuby). Sources: params[:x]/params['x'] (symbol/string keys), params.fetch(:x), Rails strong params params.require(:m).permit(:a,:b) with each permitted attribute as a field (recovered at user_params[:a] reads), request.body. Sinks: ActiveRecord write (Model.create/.update/.save/.new), raw SQL ActiveRecord::Base.connection.execute, response (render json:/plain:/redirect_to/send_*), outbound HTTP (Net::HTTP/Faraday/HTTParty/RestClient/Excon). HONEST-PARTIAL (precision-first): drops reassignment, dynamic keys (params[k]), embedded-arg, splat/keyword-arg call sites, recursion/cycle, the 4th hop, external/unresolved imports; whole-hash mass-assignment of a strong-params var flows with field="". DEPLOY-DEFERRED (daemon not rebuilt). Java/PHP request_sink_dataflow remain follow-up. Rails: params[:x]/strong-params permit + ActiveRecord/render sinks. |
| Response shape extraction | ✅ `full` | `2026-05-28` | [link](https://github.com/cajasmota/archigraph/issues/2771) | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_ruby.go` | — |
| Sanitizer recognition | 🟢 `partial` | `2026-05-28` | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_ruby.go` | — |
| Schema drift detection | ✅ `full` | `2026-05-28` | [link](https://github.com/cajasmota/archigraph/issues/2771) | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_ruby.go` | — |
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
(or use `go run ./tools/coverage update lang.ruby.framework.rails ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
