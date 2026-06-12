<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.go.framework.net-http` — net/http (stdlib)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [go](../by-language/go.md)
- **Category:** [http_framework](../by-category/http_framework.md)
- **Subcategory:** Backend HTTP
- **Capability cells:** 50

## Capabilities


### Routing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Endpoint deprecation versioning | ✅ `full` | `2026-06-03` | — | `internal/engine/http_endpoint_deprecation.go`<br>`internal/engine/http_endpoint_deprecation_test.go`<br>`internal/engine/http_endpoint_synthesis.go` | #4094: deprecated/deprecation_source(+deprecated_since/deprecated_replacement)+path-derived api_version on Go endpoints. The handler func is resolved by source_handler ref (goDeprecationVerdict): a // Deprecated: godoc comment above the func -> deprecated=true (source="// Deprecated: godoc", since/replacement parsed from the message); a Sunset/Deprecation response header set in THAT func body -> deprecated=true (scoped, no cross-handler leak). api_version is path-derived from a /vN or /api/vN segment in the route literal. Honest-partial: a Group("/api/vN") prefix is not composed into the path, so group-prefix-only versioning is not yet resolved. Value-asserted TestDeprecation_GoGodocOnGinHandler (since=v2.4, repl=/api/v3/users, api_version=2), TestDeprecation_GoSunsetHeaderScopedToHandler (no leak), TestDeprecation_GoNetHTTPGodoc (api_version=1); negatives TestDeprecation_GoNoMarkersNoStamp + TestDeprecation_GoNonHandlerGodocDoesNotLeak. |
| Endpoint pagination posture | ✅ `full` | `2026-06-02` | — | `internal/engine/http_endpoint_pagination.go`<br>`internal/engine/http_endpoint_pagination_test.go`<br>`internal/engine/http_endpoint_synthesis.go`<br>`internal/engine/response_shape_go.go` | #3920: goPaginationVerdict scans the handler body (via source_handler) for query-param reads — gin c.Query/c.DefaultQuery/c.GetQuery, echo c.QueryParam, fiber c.Query/c.QueryInt, net-http/chi r.URL.Query().Get/r.FormValue + bracket index — and feeds the names to the shared classifyParamShape. limit+offset -> offset, page -> page, cursor -> cursor. Honest-partial: a lone limit is ambiguous (NOT stamped); a dynamically-named read (c.Query(name)) is skipped. Value-asserting tests: gin limit,offset->offset; echo page,per_page->page; chi cursor,limit->cursor; net-http page->page; negatives lone-limit + no-params -> absent. |
| Endpoint response codes | ✅ `full` | `2026-06-02` | — | `internal/engine/http_endpoint_response_codes.go`<br>`internal/engine/http_endpoint_response_codes_test.go`<br>`internal/engine/http_endpoint_synthesis.go`<br>`internal/engine/response_shape_go.go` | #3920: goResponseCodes resolves the literal status-code set from the handler body (located via source_handler; route registration and handler are separate funcs in Go). Idioms: c.JSON(http.StatusCreated,x)/c.JSON(201,x), c.Status/c.AbortWithStatus(NNN), w.WriteHeader(http.StatusXxx), http.Error(w,msg,code), echo.NewHTTPError(NNN), fiber.NewError(fiber.StatusXxx). Honest-partial: a dynamic status var (c.JSON(code,x)) is skipped — no framework-default 200 fabricated. Value-asserting tests assert the SPECIFIC set per framework (gin 201,400; gin numeric/abort 204,403; echo NewHTTPError 200,404; net-http WriteHeader+http.Error 201,400; fiber 200,500) + negative (dynamic var -> absent). |
| Endpoint synthesis | ✅ `full` | `2026-05-30` | — | `internal/custom/golang/nethttp.go`<br>`internal/engine/go_routes.go`<br>`internal/engine/rules/go/frameworks/net_http_stdlib.yaml` | — |
| Handler attribution | ✅ `full` | `2026-05-30` | — | `internal/custom/golang/nethttp.go`<br>`internal/engine/go_routes.go` | — |
| Route extraction | 🟢 `partial` | `2026-05-29` | backfill:dictionary-completeness | `internal/custom/golang/extractors_test.go`<br>`internal/custom/golang/nethttp.go` | regex-based: http.HandleFunc + http.Handle on DefaultServeMux + http.NewServeMux() + Go 1.22+ method-prefixed patterns (GET /path) tested; misses cross-file route splits and dynamic path construction |

### View

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| View rendering | 🔴 `missing` | — | view_rendering:#3628-not-yet-extracted | — | — |

### Auth

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Auth coverage | — `not_applicable` | — | — | — | net/http has no middleware-registration primitive; middleware is manual handler wrapping (func(http.Handler) http.Handler) with no .Use() chain to extract. |

### Validation

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DTO extraction | ✅ `full` | `2026-06-11` | — | `internal/custom/golang/dto.go`<br>`internal/custom/golang/dto_field_members.go`<br>`internal/custom/golang/dto_field_members_test.go`<br>`internal/custom/golang/dto_test.go` | #4715: each request/response DTO struct field is emitted as a SCOPE.Schema/field member (field_name from json tag, normalized field_type, parent_class, optional from omitempty/pointer/non-required validate|binding rule, validators as @rule markers + parseable Signature) with a CONTAINS edge to the struct — the SAME uniform shape as the JS (#4635) and Python/Java (#4613) DTO field members. emitGoDTOFieldMembers in dto_field_members.go; value-asserted by TestGoDTO_FieldMembers (type/optional/validators/CONTAINS). DTO struct resolution stays same-file heuristic (partial where not proven by a per-framework fixture). |
| Request validation | 🟢 `partial` | `2026-05-29` | 3213 | `internal/custom/golang/nethttp.go` | net/http has no binding primitive; r.Form/r.FormValue/r.PostForm call sites heuristic; no struct-tag validation chain analysis |

### Middleware

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Middleware coverage | — `not_applicable` | — | — | — | net/http has no middleware-registration primitive; middleware is manual handler wrapping (func(http.Handler) http.Handler) with no .Use() chain to extract. |
| Rate limit stamping | 🔴 `missing` | — | [link](https://github.com/cajasmota/archigraph/issues/3778) | — | endpoint rate-limit / throttle stamping not yet implemented for this framework; the #3628 child shipped express-rate-limit (JS/TS) + slowapi/django-ratelimit/flask-limiter/DRF (Python). express-slow-down-compatible / framework-native limiters for this framework are future work. |

### Schema

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Type graph extraction | — `not_applicable` | — | — | — | GraphQL schema type→type graph (object-typed field -> referenced object type with list/nullable cardinality) is a GraphQL-only concept; this framework is not a GraphQL server, so it has no GraphQL object-type relationship graph. |

### Type System

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Enum extraction | — `not_applicable` | `2026-05-29` | — | — | Go has no first-class enum keyword; the idiom is const(...iota). The Go extractor extracts no const/iota enum constructs, so this capability is not applicable. |
| Interface extraction | ✅ `full` | `2026-05-29` | — | `internal/extractors/golang/extractor.go`<br>`internal/extractors/golang/extractor_test.go` | — |
| Type alias extraction | 🟢 `partial` | `2026-05-29` | — | `internal/extractors/golang/extractor.go` | type X = Y alias declarations via tree-sitter base extractor; framework-specific type aliases (e.g. gin.HandlerFunc, echo.HandlerFunc) captured but not distinguished from user-defined aliases; no value-asserting framework-specific tests |
| Type extraction | ✅ `full` | `2026-05-29` | — | `internal/extractors/golang/extractor.go`<br>`internal/extractors/golang/extractor_test.go` | — |

### DI

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DI binding extraction | 🔴 `missing` | — | 3628 | — | — |
| DI injection point | 🔴 `missing` | — | 3628 | — | — |
| DI scope resolution | 🔴 `missing` | — | 3628 | — | — |

### Testing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Tests linkage | 🟢 `partial` | `2026-06-11` | 4683 | `internal/custom/golang/httptest_e2e.go`<br>`internal/extractors/cross/testmap/extractor.go`<br>`internal/extractors/cross/testmap/frameworks.go`<br>`internal/extractors/cross/testmap/resolver.go`<br>`internal/extractors/golang/extractor.go`<br>`internal/extractors/golang/issue4683_localvar_receiver_test.go`<br>`internal/graph/coverage.go` | Go test file + Test* func detection via tree-sitter base extractor. Test→endpoint linkage credited two ways: (1) stdlib httptest route-by-string e2e — httptest.NewRequest / http.NewRequest+ServeHTTP / httptest.NewServer+http.Get — stamped to e2e_route_calls and resolved by the shared engine route-map pass (#4371); (2) test→CALLS→handler/service crediting via local-variable receiver typing — svc := NewProposalService(); svc.GetCounts() types the local from the same-file constructor's struct return so the method CALLS edge carries receiver_type and ComputeCoverage credits the endpoint (#4683). Partial: framework-specific test clients (gin CreateTestContext, echo NewContext, fiber app.Test) and non-literal/built routes are not yet linked; interface-returning factories and multi-return constructors stay bare (honest exclusion). |

### Observability

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Log extraction | 🟢 `partial` | `2026-05-29` | 3215 | `internal/custom/golang/observability.go`<br>`internal/custom/golang/observability_test.go` | heuristic: logrus.New/WithFields, zap.NewProduction/New, slog.New/With, zerolog.New setup calls detected; does not trace log fields to handler context or correlate log entries to specific routes |
| Metric extraction | 🟢 `partial` | `2026-05-29` | 3215 | `internal/custom/golang/observability.go`<br>`internal/custom/golang/observability_test.go` | prometheus.NewCounter(Vec)/NewHistogram(Vec)/NewGauge(Vec)/NewSummary(Vec) + promauto.NewXxx declarations detected; metric Name: field extracted when adjacent; does not track Observe/Add/Inc call sites or bind metrics to routes |
| Trace extraction | ✅ `full` | `2026-05-29` | — | `internal/custom/golang/observability.go`<br>`internal/custom/golang/observability_test.go` | — |

### Data

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DB effect | 🟢 `partial` | `2026-05-28` | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_golang.go` | DB effect tracking is cross-cutting substrate analysis; current extractors capture ORM call sites (gorm/sqlx/pgx/bun) at call-site level but do not bind them to specific HTTP handlers via data-flow |

### Substrate

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Confidence overlay | ✅ `full` | `2026-05-29` | — | `internal/graph/graph.go`<br>`internal/mcp/tools.go`<br>`internal/types/confidence.go` | — |
| Config consumption | ✅ `full` | `2026-06-02` | 3641 | `internal/extractor/config_key.go`<br>`internal/extractors/golang/config_consumer.go`<br>`internal/extractors/golang/config_consumer_test.go` | os.Getenv/viper.GetString -> config:<key> DEPENDS_ON_CONFIG edges (issue #3641) |
| Constant propagation | ✅ `full` | `2026-05-28` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/golang.go`<br>`internal/substrate/substrate.go` | — |
| Dead code detection | 🟢 `partial` | `2026-05-29` | — | `internal/links/reachability.go`<br>`internal/mcp/dead_code.go`<br>`internal/substrate/entry_points.go`<br>`internal/substrate/entry_points_golang.go` | — |
| Def use chain extraction | 🟢 `partial` | `2026-05-29` | — | `internal/links/def_use_pass.go`<br>`internal/substrate/def_use.go`<br>`internal/substrate/def_use_golang.go` | — |
| Env fallback recognition | ✅ `full` | `2026-05-28` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/golang.go`<br>`internal/substrate/substrate.go` | — |
| Error flow | ✅ `full` | `2026-06-02` | 3628 | `internal/extractor/exception_flow.go`<br>`internal/extractors/golang/exception_flow.go`<br>`internal/extractors/golang/exception_flow_test.go` | return ErrX / fmt.Errorf %w -> THROWS; errors.Is/As -> CATCHES; named sentinels only (#3628) |
| Feature flag gating | 🟢 `partial` | `2026-06-03` | 3706 | `internal/engine/feature_flag_edges.go`<br>`internal/engine/feature_flag_edges_test.go`<br>`internal/engine/orm_queries.go` | flag-check call sites -> feature:<key> + GATED_BY (framework-agnostic Go engine pass, fires regardless of router). Honest-partial on Go: Unleash IsEnabled / Split GetTreatment / custom getFlag,featureEnabled / LD generic Variation fire & attribute to the enclosing handler; Go-canonical LD camelCase BoolVariation + OpenFeature context-first GetBooleanValue(ctx,key) miss. |
| Fs effect | 🟢 `partial` | `2026-05-28` | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_golang.go` | — |
| HTTP effect | 🟢 `partial` | `2026-05-28` | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_golang.go` | — |
| Import resolution quality | 🟢 `partial` | `2026-06-11` | 4705 | `internal/extractors/golang/gomod.go`<br>`internal/extractors/golang/imports.go`<br>`internal/resolve/imports.go`<br>`internal/substrate/golang.go` | #4705c: go.mod local-path 'replace old => ./dir' directives honored. The Go extractor stamps go_pkg_dir for a replaced import (and sub-packages) so ResolveGoInTreeImports binds it to the in-repo file entity before external_package. Network/version and ../escape replacements keep external disposition. |
| Module cycle detection | 🟢 `partial` | `2026-05-29` | — | `internal/links/module_cycle_pass.go` | — |
| Mutation effect | 🟢 `partial` | `2026-05-28` | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_golang.go` | — |
| Pure function tagging | 🟢 `partial` | `2026-05-29` | — | `internal/links/effect_propagation.go`<br>`internal/links/pure_function_pass.go` | — |
| Reachability analysis | 🟢 `partial` | `2026-05-29` | — | `internal/links/reachability.go`<br>`internal/substrate/entry_points.go`<br>`internal/substrate/entry_points_golang.go` | — |
| Request shape extraction | ✅ `full` | `2026-05-27` | — | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_golang.go` | — |
| Request sink dataflow | 🟢 `partial` | `2026-06-02` | 3943 | `internal/links/dataflow_pass.go`<br>`internal/substrate/dataflow.go`<br>`internal/substrate/dataflow_golang.go`<br>`internal/substrate/dataflow_golang_test.go` | SCOPED request-input → sink DATA_FLOWS_TO (#3628 area #22), added via #3943: a Go dataflow sniffer (dataflow_golang.go) is now registered on the "go" slug and dispatched by file extension through LanguageForPath (internal/links/dataflow_pass.go), mirroring the python/jsts sniffers. Sources: gin c.Query/PostForm/Param/ShouldBindJSON/Bind, echo c.QueryParam/FormValue/Param/Bind, chi chi.URLParam/r.FormValue/r.URL.Query().Get, net/http r.FormValue/r.URL.Query().Get/json.NewDecoder(r.Body).Decode. Bind sources (ShouldBindJSON(&dto)/Decode(&dto)) taint the pointed-to root; field lifted from dto.Field member access. Intra-fn assignment tracking (:= and =) + multi-hop (<=DataFlowMaxHops=3) local-call + cross-file propagation; sinks gorm Create/Save/Updates + database/sql/sqlx Exec/Query, c.JSON/w.Write/json.NewEncoder(w).Encode response, http.Post/client.Do outbound. HONEST-PARTIAL: drops reassignment, dynamic keys (->flow w/o field), embedded-arg, variadic spread, type-only/unnamed/multi-line params, recursion/cycle, 4th hop, external/ambiguous imports. Java/Ruby/PHP sniffers remain follow-up. DEPLOY-DEFERRED (daemon not rebuilt). |
| Response shape extraction | ✅ `full` | `2026-05-27` | — | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_golang.go` | — |
| Sanitizer recognition | 🟢 `partial` | `2026-05-29` | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_golang.go` | — |
| Schema drift detection | ✅ `full` | `2026-05-27` | — | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_golang.go` | — |
| Taint sink detection | 🟢 `partial` | `2026-05-29` | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_golang.go` | — |
| Taint source detection | 🟢 `partial` | `2026-05-29` | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_golang.go` | — |
| Template pattern catalog | 🟢 `partial` | `2026-05-29` | — | `internal/links/template_pattern_pass.go`<br>`internal/substrate/template_pattern.go`<br>`internal/substrate/template_pattern_golang.go` | — |
| Vulnerability finding | 🟢 `partial` | `2026-05-29` | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_golang.go` | — |

## Framework-specific

### Integration

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Client consumes API | 🟢 `partial` | `2026-06-02` | — | `internal/engine/http_endpoint_go_client.go`<br>`internal/engine/http_endpoint_go_client_test.go`<br>`internal/links/http_pass.go` | Outbound HTTP-client synthesis (synthesizeGoClient): per call site emits a consumer http_endpoint_call http:<VERB>:<path> + FETCHES edge, cross-repo-linked to server routes by links/http_pass.go on the byte-identical synthetic id. Covers net/http (http.Get/Post/Head/PostForm/NewRequest, client.<verb>), resty (.R().<verb>), and req=github.com/imroc/req (package-level req.Get/Post/Put/Patch/Delete/Head/Options + chained .R().<verb>); absolute URLs host-stripped to path; fmt.Sprintf + os.Getenv concat -> runtime_dynamic. Value-asserting tests TestGoClient_ReqPackageLevel/_ReqAbsoluteURL/_ReqChainedSharedWithResty/_ReqEnvConcat/_ReqDynamicNoLiteralNegative + cross-repo TestHTTPPass_GoReqClientCrossRepoMatch. Honest-partial: fully-dynamic URLs skipped (no fabricated path). |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.go.framework.net-http ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
