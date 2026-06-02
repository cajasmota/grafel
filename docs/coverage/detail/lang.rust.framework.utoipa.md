<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.rust.framework.utoipa` — utoipa

Auto-generated. Back to [summary](../summary.md).

- **Language:** [rust](../by-language/rust.md)
- **Category:** [http_framework](../by-category/http_framework.md)
- **Subcategory:** Backend HTTP
- **Capability cells:** 38

## Capabilities


### Routing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Endpoint synthesis | ✅ `full` | `2026-05-30` | — | `internal/custom/rust/helpers.go`<br>`internal/custom/rust/utoipa.go`<br>`internal/custom/rust/utoipa_test.go` | each #[utoipa::path] attribute -> SCOPE.Operation endpoint (verb + canonical path) |
| Handler attribution | ✅ `full` | `2026-05-30` | — | `internal/custom/rust/helpers.go`<br>`internal/custom/rust/utoipa.go`<br>`internal/custom/rust/utoipa_test.go` | handler fn name captured from the fn following each #[utoipa::path] attribute |
| Route extraction | ✅ `full` | `2026-05-30` | — | `internal/custom/rust/helpers.go`<br>`internal/custom/rust/utoipa.go`<br>`internal/custom/rust/utoipa_test.go` | utoipa::path(verb, path=...) yields canonical verb+path (normalises {id}/:id/<id>); captures handler fn; enriches bare axum/actix routes with documented contract |

### Auth

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Auth coverage | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |

### Validation

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DTO extraction | ✅ `full` | `2026-05-30` | — | `internal/custom/rust/helpers.go`<br>`internal/custom/rust/utoipa.go`<br>`internal/custom/rust/utoipa_test.go` | #[derive(ToSchema)]/IntoParams structs -> SCOPE.Schema DTO with deep fields; request_body=/responses(body=) refs emitted as request/response DTOs |
| Request validation | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |

### Middleware

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Middleware coverage | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |

### Type System

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Enum extraction | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Interface extraction | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Type alias extraction | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Type extraction | ✅ `full` | `2026-05-30` | — | `internal/custom/rust/helpers.go`<br>`internal/custom/rust/utoipa.go`<br>`internal/custom/rust/utoipa_test.go` | ToSchema struct fields parsed (name/type/wire_name) -> schema_field entities |

### Testing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Tests linkage | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |

### Observability

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Log extraction | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Metric extraction | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Trace extraction | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |

### Data

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DB effect | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |

### Substrate

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Confidence overlay | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Config consumption | 🔴 `missing` | — | 3641 | — | — |
| Constant propagation | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Dead code detection | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Def use chain extraction | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Env fallback recognition | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Feature flag gating | 🔴 `missing` | — | feature_flag_gating:#3706-not-yet-extracted | — | — |
| Fs effect | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| HTTP effect | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Import resolution quality | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Module cycle detection | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Mutation effect | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Pure function tagging | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Reachability analysis | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Request shape extraction | ✅ `full` | `2026-05-30` | — | `internal/custom/rust/helpers.go`<br>`internal/custom/rust/utoipa.go`<br>`internal/custom/rust/utoipa_test.go` | request_body = <DTO> (incl inline()/content=) -> request_dto tied to verb+path |
| Response shape extraction | ✅ `full` | `2026-05-30` | — | `internal/custom/rust/helpers.go`<br>`internal/custom/rust/utoipa.go`<br>`internal/custom/rust/utoipa_test.go` | responses((status=N, body=<DTO>)) -> response_dto per status, tied to verb+path |
| Sanitizer recognition | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Schema drift detection | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Taint sink detection | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Taint source detection | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Template pattern catalog | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Vulnerability finding | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.rust.framework.utoipa ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
