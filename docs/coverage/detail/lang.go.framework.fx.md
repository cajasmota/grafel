<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.go.framework.fx` — uber/fx (DI)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [go](../by-language/go.md)
- **Category:** [http_framework](../by-category/http_framework.md)
- **Subcategory:** Backend HTTP
- **Capability cells:** 46

## Capabilities


### Routing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Endpoint deprecation versioning | 🔴 `missing` | — | 3628 | — | — |
| Endpoint pagination posture | 🔴 `missing` | `2026-06-02` | 3628 | `internal/engine/http_endpoint_pagination.go`<br>`internal/engine/http_endpoint_pagination_patterns.go`<br>`internal/engine/http_endpoint_pagination_test.go`<br>`internal/engine/http_endpoint_synthesis.go` | #3628: applyEndpointPagination stamps paginated/pagination_style/pagination_params via the cross-language parameters/parameter_schema fallback (limit+offset/page/cursor shape). No framework-specific pagination-class/ORM signal yet for this framework. |
| Endpoint synthesis | 🔴 `missing` | — | 3628 | — | — |
| Handler attribution | 🔴 `missing` | — | 3628 | — | — |
| Route extraction | 🔴 `missing` | — | 3628 | — | — |

### Auth

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Auth coverage | 🔴 `missing` | — | 3628 | — | — |

### Validation

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DTO extraction | 🔴 `missing` | — | 3628 | — | — |
| Request validation | 🔴 `missing` | — | 3628 | — | — |

### Middleware

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Middleware coverage | 🔴 `missing` | — | 3628 | — | — |
| Rate limit stamping | 🔴 `missing` | — | [link](https://github.com/cajasmota/archigraph/issues/3778) | — | endpoint rate-limit / throttle stamping not yet implemented for this framework; the #3628 child shipped express-rate-limit (JS/TS) + slowapi/django-ratelimit/flask-limiter/DRF (Python). express-slow-down-compatible / framework-native limiters for this framework are future work. |

### Type System

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Enum extraction | 🔴 `missing` | — | 3628 | — | — |
| Interface extraction | 🔴 `missing` | — | 3628 | — | — |
| Type alias extraction | 🔴 `missing` | — | 3628 | — | — |
| Type extraction | 🔴 `missing` | — | 3628 | — | — |

### DI

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DI binding extraction | 🟢 `partial` | `2026-06-02` | 3628 | `internal/custom/golang/di_graph.go`<br>`internal/custom/golang/di_graph_test.go` | uber/fx: constructors in fx.Provide(...) emit BINDS(constructor -> produced-type) via the shared Go provider pass (func NewService(...) *Service => BINDS NewService->Service). Value-asserted TestGoDI_FxProvide (NewService->Service). Negatives shared with wire (unresolved/unregistered/error-only). PARTIAL: fx.Annotate/ParamTags/ResultTags + value groups not modeled; cross-file return types unresolved (honest-partial). |
| DI injection point | 🟢 `partial` | `2026-06-02` | 3628 | `internal/custom/golang/di_graph.go`<br>`internal/custom/golang/di_graph_test.go` | uber/fx: an fx-provided constructors parameter types are injected into the produced type: func NewService(cfg *Config) *Service emits INJECTED_INTO(Config->Service). Value-asserted TestGoDI_FxProvide (Config->Service). PARTIAL: fx.Invoke target params + fx.In/fx.Out struct-tag injection not yet modeled. |
| DI scope resolution | — `not_applicable` | `2026-06-02` | — | — | uber/fx provides singletons within an App by construction; there are no per-binding scope annotations to resolve. Not_applicable. |

### Testing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Tests linkage | 🔴 `missing` | — | 3628 | — | — |

### Observability

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Log extraction | 🔴 `missing` | — | 3628 | — | — |
| Metric extraction | 🔴 `missing` | — | 3628 | — | — |
| Trace extraction | 🔴 `missing` | — | 3628 | — | — |

### Data

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DB effect | 🔴 `missing` | — | 3628 | — | — |

### Substrate

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Confidence overlay | 🔴 `missing` | — | 3628 | — | — |
| Config consumption | 🔴 `missing` | — | 3628 | — | — |
| Constant propagation | 🔴 `missing` | — | 3628 | — | — |
| Dead code detection | 🔴 `missing` | — | 3628 | — | — |
| Def use chain extraction | 🔴 `missing` | — | 3628 | — | — |
| Env fallback recognition | 🔴 `missing` | — | 3628 | — | — |
| Error flow | ✅ `full` | `2026-06-02` | 3628 | `internal/extractor/exception_flow.go`<br>`internal/extractors/golang/exception_flow.go`<br>`internal/extractors/golang/exception_flow_test.go` | return ErrX / fmt.Errorf %w -> THROWS; errors.Is/As -> CATCHES; named sentinels only (#3628) |
| Feature flag gating | 🔴 `missing` | — | 3628 | — | — |
| Fs effect | 🔴 `missing` | — | 3628 | — | — |
| HTTP effect | 🔴 `missing` | — | 3628 | — | — |
| Import resolution quality | 🔴 `missing` | — | 3628 | — | — |
| Module cycle detection | 🔴 `missing` | — | 3628 | — | — |
| Mutation effect | 🔴 `missing` | — | 3628 | — | — |
| Pure function tagging | 🔴 `missing` | — | 3628 | — | — |
| Reachability analysis | 🔴 `missing` | — | 3628 | — | — |
| Request shape extraction | 🔴 `missing` | — | 3628 | — | — |
| Request sink dataflow | 🔴 `missing` | — | 3628 | — | — |
| Response shape extraction | 🔴 `missing` | — | 3628 | — | — |
| Sanitizer recognition | 🔴 `missing` | — | 3628 | — | — |
| Schema drift detection | 🔴 `missing` | — | 3628 | — | — |
| Taint sink detection | 🔴 `missing` | — | 3628 | — | — |
| Taint source detection | 🔴 `missing` | — | 3628 | — | — |
| Template pattern catalog | 🔴 `missing` | — | 3628 | — | — |
| Vulnerability finding | 🔴 `missing` | — | 3628 | — | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.go.framework.fx ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
