<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.go.framework.gqlgen` — gqlgen (GraphQL)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [go](../by-language/go.md)
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
| Endpoint synthesis | ✅ `full` | `2026-06-02` | 3613 | `internal/engine/http_endpoint_synthesis.go`<br>`internal/engine/http_endpoint_synthesis_gqlgen_3613_test.go`<br>`internal/engine/httproutes/canonicalize.go`<br>`internal/engine/rules/graphql/frameworks/gqlgen_go.yaml` | — |
| Handler attribution | ✅ `full` | `2026-06-02` | 3613 | `internal/engine/http_endpoint_synthesis.go`<br>`internal/engine/http_endpoint_synthesis_gqlgen_3613_test.go`<br>`internal/engine/httproutes/canonicalize.go`<br>`internal/engine/rules/graphql/frameworks/gqlgen_go.yaml` | Resolver method on generated *queryResolver/*mutationResolver/*subscriptionResolver -> http:GRAPHQL:/graphql/<Root>/<field>; source_handler=SCOPE.Operation:<receiver>.<Method> rebinds to a HANDLES edge. |
| Route extraction | 🟢 `partial` | `2026-06-02` | 3613 | `internal/engine/http_endpoint_synthesis.go`<br>`internal/engine/http_endpoint_synthesis_gqlgen_3613_test.go`<br>`internal/engine/httproutes/canonicalize.go`<br>`internal/engine/rules/graphql/frameworks/gqlgen_go.yaml`<br>`internal/extractors/graphql/graphql.go` | Operation endpoints synthesised from Go resolver receivers; SDL schema types parsed by the shared graphql extractor. Field-name mapping is gqlgen default lowerCamel and does not yet read gqlgen.yml overrides or @goField directives. |

### View

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| View rendering | 🔴 `missing` | — | view_rendering:#3628-not-yet-extracted | — | — |

### Auth

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Auth coverage | 🟢 `partial` | `2026-06-03` | 4006 | `internal/extractors/graphql/gqlgen_typegraph_auth_4006_test.go`<br>`internal/extractors/graphql/graphql.go` | #4006 (verify-first): the SDL pass now extracts field-level auth directives — @hasRole(role: ADMIN)/@hasRoles/@hasScope → auth_required+auth_roles=ADMIN; bare @auth/@isAuthenticated → auth_required (no roles); auth_method=graphql_directive, auth_confidence=0.9 — stamped on the SCOPE.Component field node. Proven by TestGqlgen_DirectiveAuth_4006 (Query.adminUsers→ADMIN, Query.me bare @auth, negatives Query.publicStats/User.id), and a non-auth-directive negative (@deprecated/@goField → no auth, TestGqlgen_NonAuthDirective_NoAuth_4006). partial: the schema-directive form is statically recoverable; resolver-body ctx-based checks (auth.ForContext(ctx)) and gqlgen generated directive-runtime wiring are not modelled. |

### Validation

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DTO extraction | 🔴 `missing` | — | 3613 | — | — |
| Request validation | 🔴 `missing` | — | 3613 | — | — |

### Middleware

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Middleware coverage | 🔴 `missing` | — | 3613 | — | — |
| Rate limit stamping | 🔴 `missing` | — | [link](https://github.com/cajasmota/archigraph/issues/3778) | — | endpoint rate-limit / throttle stamping not yet implemented for this framework; the #3628 child shipped express-rate-limit (JS/TS) + slowapi/django-ratelimit/flask-limiter/DRF (Python). express-slow-down-compatible / framework-native limiters for this framework are future work. |

### Schema

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Type graph extraction | ✅ `full` | `2026-06-03` | 4006 | `internal/classifier/classifier.go`<br>`internal/extractors/graphql/gqlgen_typegraph_auth_4006_test.go`<br>`internal/extractors/graphql/graphql.go`<br>`internal/extractors/graphql/type_graph.go` | gqlgen is SDL-driven: the schema (object types + object-typed fields) is declared in *.graphqls files, so the SDL type→type graph pass (internal/extractors/graphql/type_graph.go, #3805) emits the GRAPH_RELATES object-type→type edges with list/nullable cardinality between the SCOPE.Schema type nodes. gqlgen's generated Go resolvers carry no additional type refs (operation glue only), so no code-first Go extractor is required; the SDL pass is the source of truth for the gqlgen schema relationship graph. #4006 (verify-first) fixed the canonical gqlgen drop: classifier extensionLanguageMap mapped .graphql/.gql but NOT gqlgen's canonical .graphqls, so the pass silently never fired on graph/schema.graphqls (probe: schema.graphqls→lang=""). Added .graphqls→graphql; proven by TestGqlgen_TypeGraph_4006 (User.orders→to_many, User.account→to_one nullable) + classifier TestExtensionCoverage(graph/schema.graphqls→graphql). |

### Type System

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Enum extraction | 🔴 `missing` | — | 3613 | — | — |
| Interface extraction | 🔴 `missing` | — | 3613 | — | — |
| Type alias extraction | 🟢 `partial` | `2026-06-04` | 3613 | `internal/extractors/golang/extractor.go` | #3872: Go `type X = Y` alias declarations are lifted by the tree-sitter base Go extractor regardless of framework; a gqlgen app's ordinary .go files carry such aliases and are extracted identically to gin/echo. PARTIAL (mirrors all Go siblings): framework runtime aliases are captured but not distinguished from user-defined ones; no gqlgen-specific type-alias test. |
| Type extraction | 🔴 `missing` | — | 3613 | — | — |

### DI

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DI binding extraction | 🔴 `missing` | — | 3628 | — | — |
| DI injection point | 🔴 `missing` | — | 3628 | — | — |
| DI scope resolution | 🔴 `missing` | — | 3628 | — | — |

### Testing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Tests linkage | 🔴 `missing` | — | 3613 | — | — |

### Observability

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Log extraction | 🟢 `partial` | `2026-06-03` | — | `internal/custom/golang/observability.go`<br>`internal/custom/golang/observability_gqlgen_attribution_test.go` | heuristic: logrus.New/WithFields, zap.NewProduction/New, slog.New/With, zerolog.New setup calls detected; does not trace log fields to handler context or correlate log entries to specific routes |
| Metric extraction | 🟢 `partial` | `2026-06-03` | — | `internal/custom/golang/observability.go`<br>`internal/custom/golang/observability_gqlgen_attribution_test.go` | prometheus.NewCounter(Vec)/NewHistogram(Vec)/NewGauge(Vec)/NewSummary(Vec) + promauto.NewXxx declarations detected; metric Name: field extracted when adjacent; does not track Observe/Add/Inc call sites or bind metrics to routes |
| Trace extraction | ✅ `full` | `2026-06-03` | — | `internal/custom/golang/observability.go`<br>`internal/custom/golang/observability_gqlgen_attribution_test.go` | — |

### Data

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DB effect | 🟢 `partial` | `2026-06-11` | 3918 | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_cross_orm_read_4692_test.go`<br>`internal/substrate/effect_sinks_golang.go`<br>`internal/substrate/substrate_golang_graphql_gqlgen_test.go` | #3918 (verify-first): the per-LANGUAGE effect_sinks_golang.go db_read/db_write detectors (goDBReadRe: .Query/.First/.Find/.Take/.Scan incl. GORM+sqlx; goDBWriteRe: .Exec/.Create/.Save/.Update/.Delete) match on any receiver and fire on gqlgen resolver bodies, attributed to the enclosing resolver method. Proven by TestSubstrate_Go_Gqlgen_EffectsAttribute (db_read+db_write attributed to CreateTodo, db_read to Todos via gorm First/Create/Find). partial: method-name heuristic (conf 0.85), no gqlgen-dataloader-aware batching modelled. #4692 read-reach: GORM .Where now bare-matched; ambiguous sqlx/gorm terminals (.Get/.Select/.Query/.First/.Find/.Take) credited db_read on a DB-typed handle (*gorm.DB/*sqlx.DB field/param/local, goDBTypedReadMatches) so r.db.Get(ctx,id) reaches; a cache/map .Get on a non-DB receiver stays pure. |

### Substrate

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Confidence overlay | ✅ `full` | `2026-06-04` | — | `internal/graph/graph.go`<br>`internal/mcp/tools.go`<br>`internal/types/confidence.go` | #3872: the per-LANGUAGE sniffGo sniffer (Register("go")) gates only on file content with zero per-framework branching, so the graph-wide confidence overlay (#2769) consumes the SAME per-Binding Confidence for gqlgen files as flagship siblings. Value-asserting test drives the generated GraphQL resolver (.go) idiom and asserts the EXACT Confidence (literal 1.0 / env-fallback 0.85 / cross-file import 0.6). |
| Config consumption | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Constant propagation | ✅ `full` | `2026-06-04` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/golang.go`<br>`internal/substrate/substrate.go`<br>`internal/substrate/substrate_cap_gjj_sweep_test.go` | #3872: the framework-blind go sniffGo sniffer extracts top-level string literals regardless of framework; gqlgen dispatches it identically. Test asserts the EXACT literal value (GqlgenSchemaPath="/query" literal) + ProvenanceLiteral + Confidence 1.0 on the generated GraphQL resolver (.go) idiom. |
| Dead code detection | 🟢 `partial` | `2026-06-04` | 3613 | `internal/links/reachability.go`<br>`internal/mcp/dead_code.go`<br>`internal/substrate/entry_points.go`<br>`internal/substrate/entry_points_golang.go` | #3872: dead-code identification is the whole-GRAPH Phase-1B reachability pass (reachability.go) with zero per-language code; a gqlgen generated resolver is an ordinary Go entity, so one never reached from an entry-point is flagged a dead-code candidate exactly as for gin/echo. PARTIAL (mirrors all Go siblings): gqlgen generated ExecutableSchema dispatch the static entry-point seeder does not model, so a provider/resolver reached only that way can be a false dead-code positive. |
| Def use chain extraction | 🟢 `partial` | `2026-06-02` | 3918 | `internal/links/def_use_pass.go`<br>`internal/substrate/def_use.go`<br>`internal/substrate/def_use_golang.go`<br>`internal/substrate/substrate_golang_graphql_gqlgen_test.go` | #3918 (verify-first): def_use_golang.go is registered per-LANGUAGE via RegisterDefUseSniffer("go", …) — file-extension dispatch (LanguageForPath: .go→go), zero framework refs. sniffDefUseGo extracts intra-procedural defs/uses and attributes them to the enclosing function via scanGoFuncHeaders, which strips the gqlgen generated `(r *mutationResolver)` receiver and binds to the bare method name. Proven by TestSubstrate_Go_Gqlgen_DefUseAttributes (def+use of a local in CreateTodo). partial: standard local-binding chains attribute; full reaching-defs across the receiver/field graph not modelled. |
| Env fallback recognition | ✅ `full` | `2026-06-04` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/golang.go`<br>`internal/substrate/substrate.go`<br>`internal/substrate/substrate_cap_gjj_sweep_test.go` | #3872: the framework-blind go substrate sniffer recognises the env-fallback idiom regardless of framework; gqlgen dispatches it identically. Test asserts the EXACT env-var name + default literal (GQLGEN_PLAYGROUND_URL+default "http://localhost:8080/") + ProvenanceEnvFallback + Confidence 0.85 on the generated GraphQL resolver (.go) idiom. |
| Error flow | ✅ `full` | `2026-06-02` | 3628 | `internal/extractor/exception_flow.go`<br>`internal/extractors/golang/exception_flow.go`<br>`internal/extractors/golang/exception_flow_test.go` | return ErrX / fmt.Errorf %w -> THROWS; errors.Is/As -> CATCHES; named sentinels only (#3628) |
| Feature flag gating | 🟢 `partial` | `2026-06-03` | 3706 | `internal/engine/feature_flag_edges.go`<br>`internal/engine/feature_flag_edges_test.go`<br>`internal/engine/orm_queries.go` | flag-check call sites -> feature:<key> + GATED_BY (framework-agnostic Go engine pass, fires regardless of router). Honest-partial on Go: Unleash IsEnabled / Split GetTreatment / custom getFlag,featureEnabled / LD generic Variation fire & attribute to the enclosing handler; Go-canonical LD camelCase BoolVariation + OpenFeature context-first GetBooleanValue(ctx,key) miss. |
| Fs effect | 🔴 `missing` | — | 3613 | — | — |
| HTTP effect | 🟢 `partial` | `2026-06-02` | 3918 | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_golang.go`<br>`internal/substrate/substrate_golang_graphql_gqlgen_test.go` | #3918 (verify-first): the per-LANGUAGE effect_sinks_golang.go http_out detector (http.Get/Post, client.Do) fires on gqlgen resolver .go bodies and attributes to the enclosing resolver method (receiver stripped by scanGoFuncHeaders). Proven by TestSubstrate_Go_Gqlgen_EffectsAttribute (http_out attributed to CreateTodo). partial: detector + attribution on the standard call forms; no gqlgen-specific dataloader/HTTP-client modelling. |
| Import resolution quality | 🟢 `partial` | `2026-06-04` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/golang.go`<br>`internal/substrate/substrate.go`<br>`internal/substrate/substrate_cap_gjj_sweep_test.go` | #3872: the go cross-file import sniffer is framework-blind; gqlgen dispatches it identically. PARTIAL (mirrors all siblings): single-segment binding, no transitive/re-export graph. Test asserts the EXACT ImportSource (github.com/99designs/gqlgen/graphql) + ProvenanceCrossFile + Confidence 0.6 on the generated GraphQL resolver (.go) idiom. |
| Module cycle detection | 🟢 `partial` | `2026-06-04` | 3613 | `internal/links/module_cycle_pass.go` | #3872: module-cycle detection is the whole-GRAPH module_cycle_pass over the Go IMPORTS edge graph; a gqlgen app is composed of ≥2 ordinary Go packages with import edges, so import cycles among them are detected exactly as for gin/echo. PARTIAL (mirrors all Go siblings): package-level import cycles only; gqlgen runtime/DI wiring is not an import edge and is out of scope. |
| Mutation effect | 🟢 `partial` | `2026-06-02` | 3918 | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_golang.go`<br>`internal/substrate/substrate_golang_graphql_gqlgen_test.go` | #3918 (verify-first): the per-LANGUAGE effect_sinks_golang.go mutation detector (goMutationRe: recv.field = …) fires on gqlgen resolver bodies and attributes to the enclosing method. Proven by TestSubstrate_Go_Gqlgen_EffectsAttribute (mutation `created.Done = true` attributed to CreateTodo). partial: single-identifier-receiver field writes only (conf 0.6). |
| Pure function tagging | 🟢 `partial` | `2026-06-04` | 3613 | `internal/links/effect_propagation.go`<br>`internal/links/pure_function_pass.go` | #3872: pure-function tagging is the whole-GRAPH Phase-3A pass (pure_function_pass.go) with zero per-language code — it tags any function-like entity the effect pass left effect-free. A gqlgen func/resolver with no stamped effect is tagged a pure candidate exactly as for gin/echo handlers. PARTIAL (mirrors all Go siblings): tagging is absence-of-detected-effect, confidence floor 0.30, not a proof of purity. |
| Reachability analysis | 🟢 `partial` | `2026-06-04` | 3613 | `internal/links/reachability.go`<br>`internal/substrate/entry_points.go`<br>`internal/substrate/entry_points_golang.go` | #3872: reachability is the whole-GRAPH Phase-1B BFS from the Go entry-point set across CALLS/IMPORTS/etc; a gqlgen generated resolver reached transitively from a Go main is marked reachable exactly as for gin/echo. PARTIAL (mirrors all Go siblings): gqlgen generated ExecutableSchema dispatch the static seeder does not follow, so entities reached only that way can be under-reached. |
| Request shape extraction | 🔴 `missing` | — | 3613 | — | — |
| Request sink dataflow | 🔴 `missing` | `2026-06-04` | 3918 | `internal/substrate/dataflow.go`<br>`internal/substrate/dataflow_golang.go`<br>`internal/substrate/substrate_golang_graphql_gqlgen_test.go` | #3943 (verify-first NEGATIVE, stays missing — note corrected): a Go dataflow sniffer IS now registered — dataflow_golang.go calls RegisterDataFlowSnifferEx("go", …) (commit 7e6a9ba4b, #3943), so DataFlowSnifferFor("go") is non-nil and the request→sink flow fires for the request-receiver Go frameworks (gin/echo/chi/net-http). gqlgen still stays missing because a gqlgen resolver reads TYPED ARGS (function parameters) + ctx, NOT a request receiver (no req.body / c.Query / r.FormValue), so no source is seeded and no DATA_FLOWS_TO is produced. Proven by TestSubstrate_Go_Gqlgen_DataFlowSnifferRegistered (sniffer registered) + TestSubstrate_Go_Gqlgen_NoDataFlowForResolver (no flow for the resolver idiom). |
| Response shape extraction | 🔴 `missing` | — | 3613 | — | — |
| Sanitizer recognition | 🟢 `partial` | `2026-06-04` | 3872 | `internal/links/taint_flow.go`<br>`internal/substrate/substrate_golang_graphql_gqlgen_test.go`<br>`internal/substrate/taint_sites_golang.go` | #3872 (verify-first, vuln-finding sibling sweep): the per-LANGUAGE taint_sites_golang.go sanitizer detectors are framework-blind and fire on gqlgen resolver bodies — html.EscapeString as an XSS sanitizer (goSanitizerHTMLRe) and parameterised db.Query(sql, ?args) as a SQL sanitizer (goSanitizerSQLRe) — both attributing to the `func (r *mutationResolver) CreateTodo(…)` resolver method. Proven by TestSubstrate_Go_Gqlgen_SanitizerFires (asserts sanitizer/xss AND sanitizer/sql_injection both attributed to CreateTodo). Same verify-first basis as the #3918 taint_sink credit. partial: sanitizer primitives are detected per-LANGUAGE regardless of framework; the gqlgen request-input (resolver typed args) source is not seeded, so a full source→sanitizer→sink flow is not modelled (see vulnerability_finding, honest-missing). |
| Schema drift detection | 🟢 `partial` | `2026-06-04` | — | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_golang.go` | #3872: the framework-agnostic payload-drift pass dispatches sniffPayloadShapesGolang by LANGUAGE slug (LanguageForPath->PayloadShapeSnifferFor), so gqlgen producer/consumer shapes feed the same drift join as siblings. PARTIAL (mirrors siblings): no gqlgen-specific drift fixture asserted end-to-end yet. |
| Taint sink detection | 🟢 `partial` | `2026-06-02` | 3918 | `internal/links/taint_flow.go`<br>`internal/substrate/substrate_golang_graphql_gqlgen_test.go`<br>`internal/substrate/taint_sites_golang.go` | #3918 (verify-first): the per-LANGUAGE taint_sites_golang.go SQL-injection sink (goSinkSQLRe: db/tx/stmt/conn .Query/Exec with fmt.Sprintf or ident-concat) fires on gqlgen resolver bodies. Proven by TestSubstrate_Go_Gqlgen_TaintSinkFires (db.Query(fmt.Sprintf(…)) flagged sql_injection). partial: anchors on a bare receiver token db|tx|stmt|conn, so the common `db := r.DB; db.Query(...)` handle form fires but the field-receiver `r.DB.Query(...)` form and the `"literal"+var` concat shape do NOT — documented in the test. |
| Taint source detection | 🔴 `missing` | `2026-06-02` | 3918 | `internal/substrate/substrate_golang_graphql_gqlgen_test.go`<br>`internal/substrate/taint_sites_golang.go` | #3918 (verify-first NEGATIVE, stays missing): the per-LANGUAGE Go taint SOURCE regexes key on net/http request accessors (r.URL.Query/Form/Body) and gin/chi/echo/fiber context getters. A gqlgen resolver receives untrusted input via typed resolver args (function parameters) + ctx, NOT those accessors, so no taint source fires. Proven by TestSubstrate_Go_Gqlgen_TaintSourceDoesNotFire (zero sources). Crediting would require a gqlgen-arg-aware source model (future work). |
| Template pattern catalog | 🟢 `partial` | — | — | `internal/links/template_pattern_pass.go`<br>`internal/substrate/template_pattern_golang.go` | #3872: sniffTemplatePatternsGolang is registered on the go language slug and gates only on file content (no per-framework branch), so gqlgen dispatches it identically. PARTIAL: mirrors all siblings. |
| Vulnerability finding | 🔴 `missing` | — | 3613 | — | — |

## Related extraction records

This record provides code-level coverage for the
[`protocol.graphql`](./protocol.graphql.md) hub record (GraphQL),
which tracks the same technology at a higher level.

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.go.framework.gqlgen ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
