<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.go.framework.wire` — google/wire (DI)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [go](../by-language/go.md)
- **Category:** [http_framework](../by-category/http_framework.md)
- **Subcategory:** Backend HTTP
- **Capability cells:** 43

## Capabilities


### Routing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Endpoint deprecation versioning | 🔴 `missing` | — | 3628 | — | — |
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
| DI binding extraction | 🟢 `partial` | `2026-06-02` | 3628 | `internal/custom/golang/di_graph.go`<br>`internal/custom/golang/di_graph_test.go` | google/wire: providers enumerated in wire.Build(...) / wire.NewSet(...) emit BINDS(provider-func -> produced-type), resolving the constructor return type (e.g. func NewService(...) *Service => BINDS NewService->Service). Value-asserted TestGoDI_WireBuild (NewService->Service, NewRepo->Repo) and TestGoDI_WireNewSet (NewMailer->Mailer, (*Mailer,error) return). Negatives: TestGoDI_UnresolvedProviderNoEdge (provider defined in another file), TestGoDI_UnregisteredFuncNoEdge (bare NewX not in a wire/fx site), TestGoDI_ErrorOnlyReturnNoBinds. PARTIAL: cross-file provider return types unresolved (honest-partial); wire.Bind interface-binding + wire.Value not yet modeled. |
| DI injection point | 🟢 `partial` | `2026-06-02` | 3628 | `internal/custom/golang/di_graph.go`<br>`internal/custom/golang/di_graph_test.go` | google/wire: a providers parameter types are injected into the produced type: func NewService(repo *Repo) *Service emits INJECTED_INTO(Repo->Service). Value-asserted TestGoDI_WireBuild (Repo->Service). Built-in/context/error param types rejected. PARTIAL: only providers registered in a wire site are processed; cross-file return types unresolved. |
| DI scope resolution | — `not_applicable` | `2026-06-02` | — | — | google/wire is a compile-time DI codegen tool with no runtime scopes/lifetimes to resolve (a singleton-per-graph by construction). Scope resolution is not_applicable. |

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
(or use `go run ./tools/coverage update lang.go.framework.wire ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
