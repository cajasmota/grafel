<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.python.framework.django` — Django

Auto-generated. Back to [summary](../summary.md).

- **Language:** [python](../by-language/python.md)
- **Category:** [http_framework](../by-category/http_framework.md)
- **Subcategory:** Backend HTTP
- **Capability cells:** 49

## Capabilities


### Routing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Endpoint deprecation versioning | 🔴 `missing` | — | 3628 | — | — |
| Endpoint pagination posture | ✅ `full` | `2026-06-02` | 3628 | `internal/engine/http_endpoint_pagination.go`<br>`internal/engine/http_endpoint_pagination_patterns.go`<br>`internal/engine/http_endpoint_pagination_test.go`<br>`internal/engine/http_endpoint_synthesis.go` | #3628: paginated/pagination_style(offset|page|cursor)/pagination_params/pagination_source on http_endpoint_definition via applyEndpointPagination. Direct signals: DRF pagination_class + DEFAULT_PAGINATION_CLASS, Django Paginator, FastAPI/fastapi-pagination, Spring Pageable/Page<>, Express req.query, Sequelize/Prisma take/skip/.cursor(). Honest-partial: lone limit not stamped. |
| Endpoint response codes | 🔴 `missing` | — | 3818 | — | — |
| Endpoint synthesis | ✅ `full` | `2026-05-28` | — | `internal/engine/django_routes.go`<br>`internal/engine/django_urlconf_nested.go`<br>`internal/engine/rules/python/frameworks/django.yaml` | — |
| Handler attribution | ✅ `full` | `2026-05-28` | — | `internal/engine/django_admin_routes.go`<br>`internal/engine/django_routes.go` | — |
| Route extraction | ✅ `full` | `2026-05-29` | — | `internal/engine/django_routes.go`<br>`internal/engine/django_urlconf_nested.go` | — |

### View

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| View rendering | ✅ `full` | `2026-06-02` | 3628 | `internal/extractor/template_render.go`<br>`internal/extractors/python/template_render.go`<br>`internal/extractors/python/template_render_test.go` | render(request,'x.html') + TemplateView.template_name -> RENDERS SCOPE.Template; dynamic names dropped (#3628) |

### Auth

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Auth coverage | ✅ `full` | `2026-05-29` | 3052 | `internal/custom/python/django.go`<br>`internal/extractors/python/django_drf_permissions.go`<br>`internal/mcp/auth_coverage.go` | @login_required and @permission_required decorators explicitly extracted by django.go; DRF permission_classes + get_permissions() per-class + repo DEFAULT_PERMISSION_CLASSES analysed by auth_coverage.go; comprehensive multi-signal detection |

### Validation

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DTO extraction | 🟢 `partial` | `2026-05-30` | backfill:dictionary-completeness | `internal/custom/python/django.go`<br>`internal/custom/python/http_reqresp_generic.go` | django.go emits DRF Serializer/ModelSerializer class entities and Form/ModelForm admin class entities. http_reqresp_generic.go emits djangoFormClassRe-matched Form/ModelForm classes as request_dto entities. Partial because: individual form field types (CharField, IntegerField) are not introspected per-field; FormSet is not detected; inline forms not handled. Tests: TestDjango_DRFSerializer, TestGHR_Django_FormIsValid, TestGHR_Django_ModelForm. |
| Request validation | ✅ `full` | `2026-05-30` | — | `internal/custom/python/http_reqresp_generic.go` | http_reqresp_generic.go: djangoIsValidRe detects form.is_valid() calls, djangoCleanedDataRe detects .cleaned_data access, drfRequestDataRe detects request.data/.query_params/.FILES, drfValidatedDataRe detects serializer.validated_data, drfIsValidRe detects serializer.is_valid() — all in Django FBV/CBV handler bodies. Tests: TestGHR_Django_FormIsValid, TestGHR_Django_CleanedData, TestGHR_DRF_RequestData, TestGHR_DRF_ValidatedData. |

### Middleware

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Middleware coverage | 🟢 `partial` | `2026-06-02` | — | `internal/custom/python/django.go`<br>`internal/engine/http_endpoint_python_middleware.go` | django.go extracts *Middleware class definitions + process_* hooks. #3628: applyPythonMiddlewareCoverage now BINDS the ordered settings.MIDDLEWARE list (static list literal, declaration order) as the global-scope middleware_chain on every same-file Django route op, matching the Go (#3777) / JS-TS (#2853) endpoint contract {name,expr,scope,order,auth_kind?} (auth middleware tagged auth_kind, not double-modeled). Still partial: cross-file settings.py MIDDLEWARE (routes in urls.py, list in settings.py) is not joined; dynamically-assembled MIDDLEWARE lists are skipped (honest-partial). Tests: TestMiddleware_DjangoGlobal, TestMiddleware_DjangoDynamicSkipped. |
| Rate limit stamping | 🟢 `partial` | `2026-06-03` | 4122 | `internal/engine/http_endpoint_python_ratelimit.go`<br>`internal/engine/http_endpoint_python_ratelimit_test.go`<br>`internal/engine/http_endpoint_synthesis.go` | django-ratelimit @ratelimit(key='ip', rate='10/m') on a function view / CBV method is bound and the rate+scope resolved via applyPythonRateLimit. DRF view-class throttles are credited under the django-drf lane. Partial: non-decorator middleware limiters are future work. Value-asserting + negative tests. |

### Schema

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Type graph extraction | — `not_applicable` | — | — | — | GraphQL schema type→type graph (object-typed field -> referenced object type with list/nullable cardinality) is a GraphQL-only concept; this framework is not a GraphQL server, so it has no GraphQL object-type relationship graph. |

### Type System

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Enum extraction | ✅ `full` | `2026-06-02` | 3049 | `internal/extractor/enum_valueset.go`<br>`internal/extractors/python/types.go` | pattern_type=enum + enum_members stamped on the class AND a value-carrying SCOPE.Enum value-set node (internal/extractor/enum_valueset.go) capturing each member's literal value (RED=1, OPEN=open; Django TextChoices/IntegerChoices stored-value tuple element). Value-less for auto()/computed members (honest-partial). |
| Interface extraction | ✅ `full` | `2026-05-29` | 3049 | `internal/extractors/python/types.go` | — |
| Type alias extraction | ✅ `full` | `2026-05-29` | 3049 | `internal/extractors/python/types.go` | — |
| Type extraction | ✅ `full` | `2026-05-29` | 3049 | `internal/extractors/python/types.go` | — |

### DI

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DI binding extraction | 🔴 `missing` | — | 3628 | — | — |
| DI injection point | 🔴 `missing` | — | 3628 | — | — |
| DI scope resolution | 🔴 `missing` | — | 3628 | — | — |

### Testing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Tests linkage | ✅ `full` | `2026-05-29` | 3051 | `internal/custom/python/pytest.go`<br>`internal/engine/http_endpoint_e2e_testmap.go`<br>`internal/engine/tests_edges.go`<br>`internal/extractors/python/extractor.go` | pytest.go extracts test funcs; multi-hop TESTS pass (#2987) links test-client calls through ROUTES_TO to handlers; framework fixture tests in tests_edges_test.go. #4681 (epic #4615) — test→endpoint coverage-linkage for Python: (1) local-variable receiver typing in extractor.go (localReceiverTypes/receiverClass) types `v = ProposalViewSet(); v.get_counts(...)` so the enclosing named test_* function emits a resolved CALLS edge to ProposalViewSet.get_counts (mirrors TS/JS #4671); Python test_* funcs are already named call-bearing entities so NO synthetic test-scope owner is needed (unlike TS/JS). (2) route-hit test-client linkage (DRF self.client.get(url) / APIClient().post(url) / FastAPI TestClient(app).get / Flask app.test_client().get / pytest client.get) already lands a TESTS edge to the matching http_endpoint_definition via pytest.go e2e_route_calls -> http_endpoint_e2e_testmap.go (#4369). ComputeCoverage credits the endpoint via test->CALLS->handler + handler<-definition (no coverage-side change). Honest exclusion: shape-only assertion specs that never call the handler or hit a route get NO edge. Tests: issue4681_localvar_receiver_test.go (ViewSet/CBV/serializer + factory & shape-only negatives). |

### Observability

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Log extraction | 🟢 `partial` | `2026-05-30` | 3063 | `internal/custom/python/observability.go` | observability.go: import-heuristic detection of stdlib logging (logging.getLogger + call sites), loguru (from loguru import logger + bind/opt/contextualize), and structlog (structlog.get_logger + structlog.configure). Emits SCOPE.Pattern/logger + SCOPE.Pattern/log_statement entities per file. Partial by design: no cross-file dataflow — a logger declared in utils.py and used in views.py produces entities only in the file where the call site lives. Tests: TestObservability_StdlibLogging, TestObservability_Loguru, TestObservability_Structlog, TestObservability_FixtureLogging. |
| Metric extraction | 🟢 `partial` | `2026-05-30` | 3063 | `internal/custom/python/observability.go` | observability.go: import-heuristic detection of prometheus_client (Counter/Gauge/Histogram/Summary construction + push_to_gateway), statsd (incr/decr/gauge/timing/histogram calls), and datadog DogStatsd (increment/gauge/histogram/timing). Emits SCOPE.Pattern/metric entities with metric_type and metric_name properties. Partial by design: no cross-file dataflow; prometheus_client REGISTRY custom collector classes not detected; StatsD pipelines not followed. Tests: TestObservability_PrometheusClient, TestObservability_Statsd, TestObservability_Datadog, TestObservability_FixtureMetrics. |
| Trace extraction | 🟢 `partial` | `2026-05-30` | 3063 | `internal/custom/python/observability.go` | observability.go: import-heuristic detection of OpenTelemetry (tracer.start_as_current_span decorator + context-manager + start_span), ddtrace (@tracer.wrap decorator + tracer.trace context-manager), and jaeger_client (Config(service_name=) + tracer.start_span). Emits SCOPE.Pattern/trace_span entities with span_name, span_kind, and library properties. Partial by design: no cross-file dataflow; OTel Resource/TracerProvider setup not tracked; auto-instrumentation via opentelemetry-instrument not detected. Tests: TestObservability_OpenTelemetry, TestObservability_DDTrace, TestObservability_JaegerClient, TestObservability_FixtureTracing. |

### Data

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DB effect | 🟢 `partial` | `2026-06-11` | 2972 | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_python.go` | #4668/#4694/#4691 read-sink reach for layered Django repositories. pyDBReadRe bare-matches the DISTINCTIVE queryset read verbs (.filter/.exclude/.annotate/.select_related/.prefetch_related/.values_list/.only/.defer/.distinct/.order_by/.get_queryset) on ANY receiver, mirroring the write side, so a repository read held on a variable/attribute (self.queryset.filter(...), qs.exclude(...)) propagates db_read up the controller->service->repo CALLS chain. #4691 closes the builtin-colliding terminal gap with lightweight receiver typing in effect_sinks_python.go (collectQuerysetTypedNames + querysetReadMatches + pyInherentQSReadRe): the ambiguous read terminals (.get/.first/.last/.all/.exists/.count/.values/.values_list) are credited db_read ONLY on a queryset-typed receiver -- a name assigned from a queryset-producing expr (qs = Model.objects.filter(...), self.get_queryset(), self.queryset), chained reassignments (qs = qs.exclude(...)) propagated to a fixpoint, or the inherent self.get_queryset()/self.queryset handles chained directly to a terminal. get_object_or_404/get_list_or_404 are credited as distinctive shortcut reads. FALSE-POSITIVE GUARD preserved: .get on a dict / .count on a Mock/list (untyped receiver) stays non-read. HONEST-PARTIAL: regex content-scan typing, not full AST dataflow; cross-scope name reuse is not segmented. DEPLOY-DEFERRED (live-daemon reindex is a separate coordinated step). |

### Substrate

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Confidence overlay | 🟢 `partial` | `2026-05-29` | 3068 | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_python.go`<br>`internal/types/confidence.go` | — |
| Config consumption | ✅ `full` | `2026-06-02` | 3641 | `internal/extractors/python/config_consumer.go`<br>`internal/extractors/python/config_consumer_test.go` | settings.X / os.environ.get(k) -> DEPENDS_ON_CONFIG (live pre-#3641; config-blast-radius) |
| Constant propagation | ✅ `full` | `2026-05-28` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/python.go`<br>`internal/substrate/substrate.go` | — |
| Dead code detection | 🟢 `partial` | `2026-05-29` | 3045 | `internal/links/reachability.go`<br>`internal/mcp/dead_code.go`<br>`internal/substrate/entry_points_python.go` | — |
| Def use chain extraction | 🟢 `partial` | `2026-05-29` | 3045 | `internal/links/def_use_pass.go`<br>`internal/substrate/def_use_python.go` | — |
| Env fallback recognition | ✅ `full` | `2026-05-28` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/python.go`<br>`internal/substrate/substrate.go` | — |
| Error flow | ✅ `full` | `2026-06-02` | 3628 | `internal/extractor/exception_flow.go`<br>`internal/extractors/python/exception_flow.go`<br>`internal/extractors/python/exception_flow_test.go` | raise X / raise mod.X -> THROWS; except (A,B) -> CATCHES; bare except + dynamic raise dropped (#3628) |
| Feature flag gating | 🟢 `partial` | `2026-06-03` | 4106 | `internal/engine/feature_flag_edges.go`<br>`internal/engine/feature_flag_edges_test.go`<br>`internal/engine/orm_queries.go` | flag-check call sites -> feature:<key> + GATED_BY (framework-agnostic engine pass, fires regardless of router/task decorator). Honest-partial on Python: LaunchDarkly variation/bool_variation, Unleash is_enabled, OpenFeature get_boolean_value, Flagsmith has_feature, Split getTreatment, custom getFlag/feature_enabled fire & attribute to the enclosing handler/task/resolver. Miss: OpenFeature kwarg form get_boolean_value(flag_key=...) and plain env-var gating os.environ.get('FEATURE_X') (config consumption, not SDK flag). |
| Fs effect | 🟢 `partial` | `2026-05-29` | 3045 | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_python.go` | — |
| HTTP effect | 🟢 `partial` | `2026-05-29` | 3045 | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_python.go` | — |
| Import resolution quality | 🟢 `partial` | `2026-06-11` | 4705 | `internal/external/synth.go`<br>`internal/links/constant_propagation.go`<br>`internal/resolve/imports.go`<br>`internal/substrate/python.go` | #4705a: src-layout + namespace-package + Django source-root resolution. modulesForPythonFile strips a leading src./lib./app. root AND an interior apps./src./app. container segment (boundary-anchored, single-strip) so 'from core.models import X' / 'from users.views import V' under src/ or server/apps/ bind to the internal file before external_package. Bare project containers with no convention marker (e.g. backend/) stay unresolved (honest). |
| Module cycle detection | 🟢 `partial` | `2026-05-29` | 3045 | `internal/links/module_cycle_pass.go` | — |
| Mutation effect | 🟢 `partial` | `2026-05-29` | 3045 | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_python.go` | — |
| Pure function tagging | 🟢 `partial` | `2026-05-29` | 3045 | `internal/links/effect_propagation.go`<br>`internal/links/pure_function_pass.go` | — |
| Reachability analysis | 🟢 `partial` | `2026-05-29` | 3045 | `internal/links/reachability.go`<br>`internal/substrate/entry_points_python.go` | — |
| Request shape extraction | ✅ `full` | `2026-05-27` | — | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_python.go` | — |
| Request sink dataflow | 🟢 `partial` | `2026-06-02` | 3740 | `internal/links/dataflow_pass.go`<br>`internal/substrate/dataflow.go`<br>`internal/substrate/dataflow_python.go`<br>`internal/substrate/dataflow_python_framework_sources_test.go` | SCOPED request-input → sink DATA_FLOWS_TO (#3628 area #22): intra-fn assignment tracking + multi-hop (≤DataFlowMaxHops=3) local-call propagation AND cross-file propagation into imported helpers. Multi-hop: value followed through nested module-level calls a→b→c, each bound by exact positional index (self/cls-aware); full chain in hop_path/hop_count props. Cross-file (#3772): when the callee resolves (via the CALLS graph) to exactly one same-repo function entity, that file is read and the bounded walk continues there (continueDataFlowPython); sink resolves to the callee-file entity. Sources request.data/json/GET/POST + DRF serializer.validated_data (static string keys only). Sinks Model.objects.create/.save/.insert, return Response/JsonResponse, requests/httpx.post. HONEST-PARTIAL (precision-first): drops reassignment, branch-merge, collection mutation, dynamic keys, embedded-arg, *args/**kwargs/keyword-arg call sites, recursion/entity-cycle, the 4th hop, and external/unresolved/ambiguous imports. DEPLOY-DEFERRED (daemon not rebuilt). #3927: Django subscript sources added — request.POST['x'] (form) + request.GET['x'] (query) alongside the existing .get('x') forms. |
| Response shape extraction | ✅ `full` | `2026-05-27` | — | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_python.go` | — |
| Sanitizer recognition | 🟢 `partial` | `2026-05-29` | 3045 | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_python.go` | — |
| Schema drift detection | ✅ `full` | `2026-05-27` | — | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_python.go` | — |
| Taint sink detection | 🟢 `partial` | `2026-05-29` | 3045 | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_python.go` | — |
| Taint source detection | 🟢 `partial` | `2026-05-29` | 3045 | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_python.go` | — |
| Template pattern catalog | 🟢 `partial` | `2026-05-29` | 3045 | `internal/links/template_pattern_pass.go`<br>`internal/substrate/template_pattern_python.go` | — |
| Vulnerability finding | 🟢 `partial` | `2026-05-29` | 3045 | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_python.go` | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.python.framework.django ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
