<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.java.framework.guice` — Google Guice (DI)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [java](../by-language/java.md)
- **Category:** [http_framework](../by-category/http_framework.md)
- **Subcategory:** JVM Backend
- **Capability cells:** 54

## Capabilities


### Routing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Endpoint deprecation versioning | 🔴 `missing` | — | 3628 | — | — |
| Endpoint pagination posture | 🔴 `missing` | `2026-06-02` | 3628 | `internal/engine/http_endpoint_pagination.go`<br>`internal/engine/http_endpoint_pagination_patterns.go`<br>`internal/engine/http_endpoint_pagination_test.go`<br>`internal/engine/http_endpoint_synthesis.go` | #3628: applyEndpointPagination stamps paginated/pagination_style/pagination_params via the cross-language parameters/parameter_schema fallback (limit+offset/page/cursor shape). No framework-specific pagination-class/ORM signal yet for this framework. |
| Endpoint response codes | 🔴 `missing` | — | 3818 | — | — |
| Endpoint synthesis | 🔴 `missing` | — | 3699 | — | — |
| Handler attribution | 🔴 `missing` | — | 3699 | — | — |
| Route extraction | 🔴 `missing` | — | 3699 | — | — |

### View

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| View rendering | 🔴 `missing` | — | view_rendering:#3628-not-yet-extracted | — | — |

### Auth

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Auth coverage | 🔴 `missing` | — | 3699 | — | — |

### Validation

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DTO extraction | 🔴 `missing` | — | 3699 | — | — |
| Request validation | 🔴 `missing` | — | 3699 | — | — |

### Middleware

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Middleware coverage | 🔴 `missing` | — | 3699 | — | — |
| Rate limit stamping | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |

### Testing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Tests linkage | 🔴 `missing` | — | 3699 | — | — |

### Type System

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Enum extraction | 🔴 `missing` | — | 3699 | — | — |
| Interface extraction | 🔴 `missing` | — | 3699 | — | — |
| Type alias extraction | 🔴 `missing` | — | 3699 | — | — |
| Type extraction | 🔴 `missing` | — | 3699 | — | — |

### DI

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DI binding extraction | ✅ `full` | `2026-06-02` | — | `internal/custom/java/di_graph.go`<br>`internal/custom/java/di_graph_test.go`<br>`internal/custom/java/patterns_dispatch.go` | #3699: ExtractGuiceDI emits the DI binding GRAPH: bind(Foo.class).to(FooImpl.class) in an AbstractModule emits Foo BINDS FooImpl; bind(...).toProvider(...) emits binding_kind=bind_provider; lifetime parsed from .in(Scopes.SINGLETON)/.asEagerSingleton(). Value-asserted in di_graph_test.go TestGuiceDI_BindTo_Binds (IFoo BINDS Foo, binding_kind=bind_to, lifetime=singleton). |
| DI injection point | ✅ `full` | `2026-06-02` | — | `internal/custom/java/di_graph.go`<br>`internal/custom/java/di_graph_test.go` | #3699: @Inject constructor params and @Inject fields emit INJECTED_INTO (provider type -> consumer class, via=guice_constructor|guice_field), primitives rejected. Value-asserted in di_graph_test.go TestGuiceDI_InjectConstructor_InjectedInto (PaymentGateway INJECTED_INTO BillingService; negative: String yields no edge). Under the shared jakarta_ee candidate token a bind()/AbstractModule signal is required to avoid @Inject false positives (self-gate, TestGuiceDI_SelfGate_NoBindNoModule). |
| DI scope resolution | 🟢 `partial` | `2026-06-02` | — | `internal/custom/java/di_graph.go`<br>`internal/custom/java/di_graph_test.go` | #3699: BINDS edges carry the Guice scope (singleton/eager_singleton/no_scope) parsed from the bind tail. PARTIAL: @Singleton on a class and custom scope annotations are not yet linked to the binding. |

### Transactions

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Transaction boundary extraction | 🔴 `missing` | — | 3699 | — | — |
| Transaction function stamping | 🔴 `missing` | — | 3628-transaction-function-stamping | — | — |
| Transaction propagation | 🔴 `missing` | — | 3699 | — | — |
| Transaction rollback rules | 🔴 `missing` | — | 3699 | — | — |

### AOP

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Advice attribution | 🔴 `missing` | — | 3699 | — | — |
| Aspect extraction | 🔴 `missing` | — | 3699 | — | — |
| Pointcut resolution | 🔴 `missing` | — | 3699 | — | — |

### Observability

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Log extraction | 🔴 `missing` | — | 3699 | — | — |
| Metric extraction | 🔴 `missing` | — | 3699 | — | — |
| Trace extraction | 🔴 `missing` | — | 3699 | — | — |

### Data

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DB effect | 🔴 `missing` | — | 3699 | — | — |

### Substrate

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Confidence overlay | 🔴 `missing` | — | 3699 | — | — |
| Config consumption | 🔴 `missing` | — | 3699 | — | — |
| Constant propagation | 🔴 `missing` | — | 3699 | — | — |
| Dead code detection | 🔴 `missing` | — | 3699 | — | — |
| Def use chain extraction | 🔴 `missing` | — | 3699 | — | — |
| Env fallback recognition | 🔴 `missing` | — | 3699 | — | — |
| Error flow | ✅ `full` | `2026-06-02` | 3628 | `internal/extractor/exception_flow.go`<br>`internal/extractors/java/exception_flow.go`<br>`internal/extractors/java/exception_flow_test.go` | throw new X + throws clause -> THROWS; catch (A|B e) -> CATCHES; checked-exception model (#3628) |
| Feature flag gating | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Fs effect | 🔴 `missing` | — | 3699 | — | — |
| HTTP effect | 🔴 `missing` | — | 3699 | — | — |
| Import resolution quality | 🔴 `missing` | — | 3699 | — | — |
| Module cycle detection | 🔴 `missing` | — | 3699 | — | — |
| Mutation effect | 🔴 `missing` | — | 3699 | — | — |
| Pure function tagging | 🔴 `missing` | — | 3699 | — | — |
| Reachability analysis | 🔴 `missing` | — | 3699 | — | — |
| Request shape extraction | 🔴 `missing` | — | 3699 | — | — |
| Response shape extraction | 🔴 `missing` | — | 3699 | — | — |
| Sanitizer recognition | 🔴 `missing` | — | 3699 | — | — |
| Schema drift detection | 🔴 `missing` | — | 3699 | — | — |
| Taint sink detection | 🔴 `missing` | — | 3699 | — | — |
| Taint source detection | 🔴 `missing` | — | 3699 | — | — |
| Template pattern catalog | 🔴 `missing` | — | 3699 | — | — |
| Vulnerability finding | 🔴 `missing` | — | 3699 | — | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.java.framework.guice ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
