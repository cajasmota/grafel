<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.kotlin.orm.room` — Room (Android)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [kotlin](../by-language/kotlin.md)
- **Category:** [orm](../by-category/orm.md)
- **Subcategory:** ORM / Data Mapper
- **Capability cells:** 8

## Capabilities


### Models

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Model extraction | 🟢 `partial` | `2026-05-28` | — | `internal/engine/rules/kotlin/orms/room_android.yaml` | — |
| Schema extraction | ✅ `full` | `2026-05-30` | — | `internal/custom/kotlin/orm_schema.go`<br>`internal/custom/kotlin/orm_schema_test.go` | — |

### Relationships

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Association extraction | ✅ `full` | `2026-05-30` | — | `internal/custom/kotlin/orm_schema.go`<br>`internal/custom/kotlin/orm_schema_test.go` | — |
| Foreign key extraction | ✅ `full` | `2026-05-30` | — | `internal/custom/kotlin/orm_schema.go`<br>`internal/custom/kotlin/orm_schema_test.go` | — |
| Lazy loading recognition | — `not_applicable` | — | — | — | Room does not support lazy loading; all queries are explicit (suspend/LiveData/Flow) |
| Relationship extraction | ✅ `full` | `2026-05-30` | — | `internal/custom/kotlin/orm_schema.go`<br>`internal/custom/kotlin/orm_schema_test.go` | — |

### Queries

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Query attribution | ✅ `full` | `2026-05-30` | — | `internal/custom/kotlin/orm_query.go`<br>`internal/custom/kotlin/orm_query_test.go` | — |

### Migrations

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Migration parsing | ✅ `full` | `2026-05-30` | — | `internal/custom/kotlin/orm_schema.go`<br>`internal/custom/kotlin/orm_schema_test.go` | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.kotlin.orm.room ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
