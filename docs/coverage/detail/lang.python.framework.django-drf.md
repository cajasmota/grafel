<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.python.framework.django-drf` — Django REST Framework

Auto-generated. Back to [summary](../summary.md).

- **Language:** [python](../by-language/python.md)
- **Category:** [http_framework](../by-category/http_framework.md)
- **Subcategory:** Backend HTTP
- **Capability cells:** 55

## Capabilities


### Routing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Endpoint deprecation versioning | 🔴 `missing` | — | 3628 | — | — |
| Endpoint pagination posture | ✅ `full` | `2026-06-02` | 3628 | `internal/engine/http_endpoint_pagination.go`<br>`internal/engine/http_endpoint_pagination_patterns.go`<br>`internal/engine/http_endpoint_pagination_test.go`<br>`internal/engine/http_endpoint_synthesis.go` | #3628: paginated/pagination_style(offset|page|cursor)/pagination_params/pagination_source on http_endpoint_definition via applyEndpointPagination. Direct signals: DRF pagination_class + DEFAULT_PAGINATION_CLASS, Django Paginator, FastAPI/fastapi-pagination, Spring Pageable/Page<>, Express req.query, Sequelize/Prisma take/skip/.cursor(). Honest-partial: lone limit not stamped. |
| Endpoint response codes | ✅ `full` | `2026-06-02` | 3818 | `internal/engine/http_endpoint_response_codes.go`<br>`internal/engine/http_endpoint_response_codes_test.go`<br>`internal/engine/http_endpoint_synthesis.go` | #3818: response_codes + success_code + response_codes_source via applyEndpointResponseCodes. DRF/Django signals: Response(status=status.HTTP_*), HttpResponse(status=NNN), and raised DRF exceptions mapped to codes (NotFound->404, PermissionDenied->403, NotAuthenticated/AuthenticationFailed->401, ValidationError/ParseError->400, MethodNotAllowed->405, NotAcceptable->406, Throttled->429, UnsupportedMediaType->415). Honest-partial: dynamic status skipped. |
| Endpoint synthesis | ✅ `full` | `2026-05-28` | — | `internal/engine/django_drf_actions.go`<br>`internal/extractors/python/django_drf_actions.go` | — |
| Handler attribution | ✅ `full` | `2026-05-28` | — | `internal/engine/django_drf_actions.go`<br>`internal/extractors/python/drf_serializer_fields.go` | — |
| Route extraction | ✅ `full` | `2026-05-29` | — | `internal/extractors/python/python_relational_bundle_test.go`<br>`internal/extractors/python/router_register.go` | — |

### View

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| View rendering | ✅ `full` | `2026-06-03` | 3914 | `internal/extractor/template_render.go`<br>`internal/extractors/python/template_render.go`<br>`internal/extractors/python/template_render_test.go` | DRF render paths: renderer_classes=[BrowsableAPIRenderer|TemplateHTMLRenderer|StaticHTMLRenderer|AdminRenderer|HTMLFormRenderer] -> RENDERS scope.template drf/<Renderer>; TemplateHTMLRenderer.template_name='x.html' -> RENDERS x.html (framework-agnostic detector); JSON-only renderer lists + dynamic/settings-derived renderer_classes emit nothing (#3914 over #3628) |

### Auth

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Auth coverage | ✅ `full` | `2026-06-02` | 2816 | `internal/authposture/django.go`<br>`internal/extractors/python/config_module.go`<br>`internal/extractors/python/django_drf_actions.go`<br>`internal/extractors/python/django_drf_actions_test.go`<br>`internal/extractors/python/django_drf_permissions.go`<br>`internal/mcp/auth_coverage.go` | #3628 area #6 (endpoint protection): class-level permission_classes / get_permissions() stamped on the ViewSet CLASS by django_drf_permissions.go (#2816); per-action protection now ALSO normalised onto the action Operation entity — django_drf_actions.go calls stampDRFActionAuth (django_drf_permissions.go) to turn the @action(permission_classes=[IsAdmin]) kwarg into auth_required/auth_method=permission_classes/auth_confidence/auth_guard, with [AllowAny] -> auth_required=false (explicit public). Value-asserting tests: TestDRFAction_BasicKwargs (auth_required=true, auth_guard=IsAdmin), TestDRFAction_AllowAnyIsPublic (auth_required=false), TestDRFAction_NoPermissionInherits (no stamp -> inherits class posture). #4675 (auth_posture_diff resolver, internal/authposture/django.go): EFFECTIVE permission precedence method/@action permission_classes then class permission_classes/get_permissions then global REST_FRAMEWORK DEFAULT_PERMISSION_CLASSES (drf_default_permission_classes); empty [] falls to global, [AllowAny] -> public. DRF analog of the NestJS #4667 effective-guard fix. |

### Validation

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DTO extraction | 🟢 `partial` | `2026-06-11` | 4474 | `internal/custom/python/django.go`<br>`internal/dashboard/shape_tree.go`<br>`internal/extractors/python/drf_serializer_fields.go`<br>`internal/extractors/python/field_validations.go`<br>`internal/extractors/python/field_validations_test.go` | django.go emits DRF ModelSerializer/Serializer/HyperlinkedModelSerializer class entities. drf_serializer_fields.go emits REFERENCES edges from PrimaryKeyRelatedField/NestedSerializer/source= fields. #4474 — the view<->serializer DTO linkage gap is closed: a DRF ViewSet/APIView/GenericAPIView CBV carries ACCEPTS_INPUT + RETURNS edges to its serializer via appendDRFSerializerEdges. #4871: each per-field SCOPE.Schema/field entity now carries a terse Properties["validations"] chip list parsed from the serializer field kwargs (max_length/min_length -> MaxLength:/MinLength:, min_value/max_value -> Min:/Max:, required=False -> Optional, allow_null=True -> AllowNull, read_only/write_only -> ReadOnly/WriteOnly), which the dashboard ShapeTree renders next to the field type. Partial because: SerializerMethodField return-type inference is not done; custom Field subclasses not tracked. Tests: TestDjango_DRFSerializer, TestPythonFieldValidations_DRF, TestShape_FieldValidationsChips, drf_serializer_fields_test.go. |
| Request validation | ✅ `full` | `2026-05-30` | — | `internal/custom/python/http_reqresp_generic.go`<br>`internal/extractors/python/drf_serializer_fields.go` | http_reqresp_generic.go detects DRF request.data, .validated_data, serializer.is_valid() in CBV/APIView post/put/patch handler bodies. drf_serializer_fields.go emits REFERENCES edges from serializer fields to model entities. Tests: TestGHR_DRF_RequestData, TestGHR_DRF_ValidatedData cover the validation evidence chain. |

### Middleware

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Middleware coverage | 🟢 `partial` | `2026-06-02` | — | `internal/engine/django_imports_rewrite.go`<br>`internal/engine/http_endpoint_python_middleware.go` | Django import rewriting covers stock middleware by class-name. #3628: applyPythonMiddlewareCoverage now BINDS DRF view permission_classes/authentication_classes/throttle_classes as the view-scope middleware_chain on the ViewSet's router-registered endpoints (same-file ViewSet, bound by router.register prefix), per the cross-stack {name,expr,scope,order,auth_kind?} contract; permission/authentication classes carry auth_kind=auth. Still partial: cross-file ViewSet (views.py separate from urls.py) is not joined; settings-level DEFAULT_*_CLASSES not parsed (honest-partial). Test: TestMiddleware_DRFView. |
| Rate limit stamping | ✅ `full` | `2026-06-03` | 4122 | `internal/engine/http_endpoint_python_ratelimit.go`<br>`internal/engine/http_endpoint_python_ratelimit_test.go`<br>`internal/engine/http_endpoint_synthesis.go` | DRF throttle stamping on synthesized endpoints (applyPythonRateLimit): throttle_classes=[UserRateThrottle/AnonRateThrottle/ScopedRateThrottle] resolves the scope from the built-in (user/anon/endpoint); throttle_scope='burst' stamps rate_limit_scope_name=burst (scope=endpoint). Router-registered ViewSets bind by URL prefix; rate resolved from a co-located custom throttle subclass rate='1000/day' (scope inherited from its built-in base), else honest-partial (DEFAULT_THROTTLE_RATES). Value-asserting + negative tests. |

### Schema

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Type graph extraction | — `not_applicable` | — | — | — | GraphQL schema type→type graph (object-typed field -> referenced object type with list/nullable cardinality) is a GraphQL-only concept; this framework is not a GraphQL server, so it has no GraphQL object-type relationship graph. |

### Type System

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Enum extraction | ✅ `full` | `2026-05-29` | 3049 | `internal/extractors/python/types.go` | — |
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

### Substrate

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Confidence overlay | 🟢 `partial` | `2026-05-29` | 3068 | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_python.go`<br>`internal/types/confidence.go` | — |
| Config consumption | ✅ `full` | `2026-06-02` | 3641 | `internal/extractors/python/config_consumer.go`<br>`internal/extractors/python/config_consumer_test.go` | settings.X / os.environ.get(k) -> DEPENDS_ON_CONFIG (live pre-#3641; config-blast-radius) |
| Constant propagation | ✅ `full` | `2026-05-28` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/python.go`<br>`internal/substrate/substrate.go` | — |
| DB effect | 🟢 `partial` | `2026-06-11` | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_python.go` | #4691 receiver-typed read reach for DRF generic views/ViewSets that delegate to a queryset (get_queryset()/self.queryset). Builds on #4668/#4694: pyDBReadRe bare-matches the distinctive queryset read verbs on any receiver; #4691 adds lightweight receiver typing in effect_sinks_python.go (collectQuerysetTypedNames + querysetReadMatches + pyInherentQSReadRe) so the builtin-colliding terminals (.get/.first/.last/.all/.exists/.count/.values/.values_list) are credited db_read ONLY on a queryset-typed receiver -- qs = Model.objects.filter(...) then qs.get(pk=...), self.get_queryset().first(), self.queryset.count(), chained reassignments to a fixpoint. get_object_or_404/get_list_or_404 covered. FALSE-POSITIVE GUARD preserved: dict.get / Mock.count (untyped receiver) stays non-read. This lifts the ~40 GET/list false-pure read endpoints stub_detector flagged. HONEST-PARTIAL: regex content-scan typing, not AST dataflow. DEPLOY-DEFERRED. |
| Dead code detection | 🟢 `partial` | `2026-05-29` | 3045 | `internal/links/reachability.go`<br>`internal/mcp/dead_code.go`<br>`internal/substrate/entry_points_python.go` | — |
| Def use chain extraction | 🟢 `partial` | `2026-05-29` | 3045 | `internal/links/def_use_pass.go`<br>`internal/substrate/def_use_python.go` | — |
| Env fallback recognition | ✅ `full` | `2026-05-28` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/python.go`<br>`internal/substrate/substrate.go` | — |
| Error flow | ✅ `full` | `2026-06-02` | 3628 | `internal/extractor/exception_flow.go`<br>`internal/extractors/python/exception_flow.go`<br>`internal/extractors/python/exception_flow_test.go` | raise X / raise mod.X -> THROWS; except (A,B) -> CATCHES; bare except + dynamic raise dropped (#3628) |
| Feature flag gating | ✅ `full` | `2026-06-02` | 3706 | `internal/engine/feature_flag_edges.go`<br>`internal/engine/feature_flag_edges_test.go`<br>`internal/engine/orm_queries.go` | flag-check call sites -> feature:<key> + GATED_BY edge (LaunchDarkly/Unleash/Unleash-React/OpenFeature/Flipper/Flagsmith/Split.io/generic) |
| Fs effect | 🟢 `partial` | `2026-05-29` | 3045 | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_python.go` | — |
| HTTP effect | 🟢 `partial` | `2026-05-29` | 3045 | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_python.go` | — |
| Import resolution quality | 🟢 `partial` | `2026-06-11` | 4705 | `internal/external/synth.go`<br>`internal/links/constant_propagation.go`<br>`internal/resolve/imports.go`<br>`internal/substrate/python.go` | #4705a: src-layout/namespace/Django source-root internal-import resolution (shared with django). DRF apps under src/ or apps/ containers resolve internal before external_package; reduces under-linking on the Django oracle. |
| Module cycle detection | 🟢 `partial` | `2026-05-29` | 3045 | `internal/links/module_cycle_pass.go` | — |
| Mutation effect | 🟢 `partial` | `2026-05-29` | 3045 | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_python.go` | — |
| Pure function tagging | 🟢 `partial` | `2026-05-29` | 3045 | `internal/links/effect_propagation.go`<br>`internal/links/pure_function_pass.go` | — |
| Reachability analysis | 🟢 `partial` | `2026-05-29` | 3045 | `internal/links/reachability.go`<br>`internal/substrate/entry_points_python.go` | — |
| Request shape extraction | ✅ `full` | `2026-05-27` | — | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_python.go` | — |
| Request sink dataflow | 🟢 `partial` | `2026-06-02` | 3740 | `internal/links/dataflow_pass.go`<br>`internal/substrate/dataflow.go`<br>`internal/substrate/dataflow_python.go` | SCOPED request-input → sink DATA_FLOWS_TO (#3628 area #22): intra-fn assignment tracking + multi-hop (≤DataFlowMaxHops=3) local-call propagation AND cross-file propagation into imported helpers. Multi-hop: value followed through nested module-level calls a→b→c, each bound by exact positional index (self/cls-aware); full chain in hop_path/hop_count props. Cross-file (#3772): when the callee resolves (via the CALLS graph) to exactly one same-repo function entity, that file is read and the bounded walk continues there (continueDataFlowPython); sink resolves to the callee-file entity. Sources request.data/json/GET/POST + DRF serializer.validated_data (static string keys only). Sinks Model.objects.create/.save/.insert, return Response/JsonResponse, requests/httpx.post. HONEST-PARTIAL (precision-first): drops reassignment, branch-merge, collection mutation, dynamic keys, embedded-arg, *args/**kwargs/keyword-arg call sites, recursion/entity-cycle, the 4th hop, and external/unresolved/ambiguous imports. DEPLOY-DEFERRED (daemon not rebuilt). |
| Response shape extraction | ✅ `full` | `2026-05-27` | — | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_python.go` | — |
| Sanitizer recognition | 🟢 `partial` | `2026-05-29` | 3045 | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_python.go` | — |
| Schema drift detection | ✅ `full` | `2026-05-27` | — | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_python.go` | — |
| Taint sink detection | 🟢 `partial` | `2026-05-29` | 3045 | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_python.go` | — |
| Taint source detection | 🟢 `partial` | `2026-05-29` | 3045 | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_python.go` | — |
| Template pattern catalog | 🟢 `partial` | `2026-05-29` | 3045 | `internal/links/template_pattern_pass.go`<br>`internal/substrate/template_pattern_python.go` | — |
| Vulnerability finding | 🟢 `partial` | `2026-05-29` | 3045 | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_python.go` | — |

### Uncategorized

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Drf default auth throttle settings | ✅ `full` | `2026-05-30` | — | `internal/custom/python/django.go`<br>`internal/custom/python/extractors_test.go` | — |
| Form field type introspection | ✅ `full` | `2026-05-30` | — | `internal/custom/python/django.go`<br>`internal/custom/python/extractors_test.go` | — |
| Middleware settings parser | ✅ `full` | `2026-05-30` | — | `internal/custom/python/django.go`<br>`internal/custom/python/extractors_test.go` | — |
| Serializer method field inference | ✅ `full` | `2026-05-30` | — | `internal/custom/python/django.go`<br>`internal/custom/python/extractors_test.go` | — |

## Framework-specific

### Django Internals

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Admin detection | 🟢 `partial` | `2026-05-29` | — | `internal/custom/python/django.go` | djangoAdminRegRe+djangoAdminDecorRe emit admin_class entities; django-drf extends django admin (#3182) |
| Signal handler attribution | 🟢 `partial` | `2026-06-11` | [link](https://github.com/cajasmota/archigraph/issues/2739) | `internal/custom/python/django.go`<br>`internal/custom/python/django_signal_connect_4789_test.go`<br>`internal/engine/django_signal_pubsub_edges.go` | #4789: BOTH signal-registration forms now wire a HANDLES_SIGNAL edge: the @receiver(post_save, sender=Model) decorator AND the imperative post_save.connect(receiver, sender=Model) call (incl. the dotted signals.post_delete.connect(...) form and the bare connect(receiver) no-sender form), the latter primarily registered in an AppConfig.ready(). The imperative path is gated on djangoKnownSignals so an unrelated <obj>.connect() is not mis-wired; the handler entity carries di_role=signal_handler + signal_type, and the edge targets Class:<Model> (or the signal name when no sender). Value-asserted TestDjango_SignalConnect_ReadyMethod/_DottedSignal/_NoSender + negative _NonSignalIgnored. Honest-partial: custom Signal() instances cannot be enumerated, so only the built-in Django signals are recognised in the imperative form. |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.python.framework.django-drf ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
