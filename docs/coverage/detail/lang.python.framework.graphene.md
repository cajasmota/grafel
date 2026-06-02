<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.python.framework.graphene` — Graphene GraphQL

Auto-generated. Back to [summary](../summary.md).

- **Language:** [python](../by-language/python.md)
- **Category:** [http_framework](../by-category/http_framework.md)
- **Subcategory:** Backend HTTP
- **Capability cells:** 37

## Capabilities


### Routing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Endpoint synthesis | ✅ `full` | `2026-06-02` | 3620 | `internal/engine/http_endpoint_synthesis.go`<br>`internal/engine/http_endpoint_synthesis_graphene_ariadne_3620_test.go`<br>`internal/engine/httproutes/canonicalize.go` | graphene.ObjectType Query/Mutation/Subscription root classes; resolve_<field> methods and graphene.<X>(...) class-attribute fields -> http:GRAPHQL:/graphql/<Root>/<field>, identical shape to Strawberry/gqlgen. synthesizeGraphene. |
| Handler attribution | ✅ `full` | `2026-06-02` | 3620 | `internal/engine/http_endpoint_synthesis.go`<br>`internal/engine/http_endpoint_synthesis_graphene_ariadne_3620_test.go`<br>`internal/engine/httproutes/canonicalize.go` | resolve_<field> method is the handler; source_handler=SCOPE.Operation:<Root>.resolve_<field> rebinds to a HANDLES edge. Default-resolver fields (no method) emitted honest-partial with the conventional resolver name. |
| Route extraction | 🟢 `partial` | `2026-06-02` | 3620 | `internal/engine/http_endpoint_synthesis.go`<br>`internal/engine/http_endpoint_synthesis_graphene_ariadne_3620_test.go`<br>`internal/engine/httproutes/canonicalize.go` | Operation endpoints synthesised from root-class fields; field name is the snake_case attribute (no name mangling). Dynamic/programmatically-named fields not resolved (honest-partial). |

### Auth

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Auth coverage | 🔴 `missing` | — | 3620 | — | — |

### Validation

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DTO extraction | 🔴 `missing` | — | 3620 | — | — |
| Request validation | 🔴 `missing` | — | 3620 | — | — |

### Middleware

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Middleware coverage | 🔴 `missing` | — | 3620 | — | — |

### Type System

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Enum extraction | 🔴 `missing` | — | 3620 | — | — |
| Interface extraction | 🔴 `missing` | — | 3620 | — | — |
| Type alias extraction | 🔴 `missing` | — | 3620 | — | — |
| Type extraction | 🔴 `missing` | — | 3620 | — | — |

### Testing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Tests linkage | 🔴 `missing` | — | 3620 | — | — |

### Observability

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Log extraction | 🔴 `missing` | — | 3620 | — | — |
| Metric extraction | 🔴 `missing` | — | 3620 | — | — |
| Trace extraction | 🔴 `missing` | — | 3620 | — | — |

### Data

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DB effect | 🔴 `missing` | — | 3620 | — | — |

### Substrate

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Confidence overlay | 🔴 `missing` | — | 3620 | — | — |
| Config consumption | 🔴 `missing` | — | 3620 | — | — |
| Constant propagation | 🔴 `missing` | — | 3620 | — | — |
| Dead code detection | 🔴 `missing` | — | 3620 | — | — |
| Def use chain extraction | 🔴 `missing` | — | 3620 | — | — |
| Env fallback recognition | 🔴 `missing` | — | 3620 | — | — |
| Fs effect | 🔴 `missing` | — | 3620 | — | — |
| HTTP effect | 🔴 `missing` | — | 3620 | — | — |
| Import resolution quality | 🔴 `missing` | — | 3620 | — | — |
| Module cycle detection | 🔴 `missing` | — | 3620 | — | — |
| Mutation effect | 🔴 `missing` | — | 3620 | — | — |
| Pure function tagging | 🔴 `missing` | — | 3620 | — | — |
| Reachability analysis | 🔴 `missing` | — | 3620 | — | — |
| Request shape extraction | 🔴 `missing` | — | 3620 | — | — |
| Response shape extraction | 🔴 `missing` | — | 3620 | — | — |
| Sanitizer recognition | 🔴 `missing` | — | 3620 | — | — |
| Schema drift detection | 🔴 `missing` | — | 3620 | — | — |
| Taint sink detection | 🔴 `missing` | — | 3620 | — | — |
| Taint source detection | 🔴 `missing` | — | 3620 | — | — |
| Template pattern catalog | 🔴 `missing` | — | 3620 | — | — |
| Vulnerability finding | 🔴 `missing` | — | 3620 | — | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.python.framework.graphene ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
