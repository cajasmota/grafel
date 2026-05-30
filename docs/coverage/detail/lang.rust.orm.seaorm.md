<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.rust.orm.seaorm` — SeaORM

Auto-generated. Back to [summary](../summary.md).

- **Language:** [rust](../by-language/rust.md)
- **Category:** [orm](../by-category/orm.md)
- **Subcategory:** ORM / Data Mapper
- **Capability cells:** 8

## Capabilities


### Models

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Model extraction | ✅ `full` | `2026-05-28` | — | `internal/engine/rules/rust/orms/seaorm.yaml` | — |
| Schema extraction | ✅ `full` | `2026-05-30` | — | `internal/custom/rust/orm_props_test.go`<br>`internal/custom/rust/seaorm.go`<br>`internal/custom/rust/testdata/seaorm_entity.rs` | — |

### Relationships

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Association extraction | 🟢 `partial` | `2026-05-30` | backfill:dictionary-completeness | `internal/custom/rust/seaorm.go`<br>`internal/custom/rust/testdata/seaorm_entity.rs` | — |
| Foreign key extraction | 🟢 `partial` | `2026-05-30` | backfill:dictionary-completeness | `internal/custom/rust/diesel_seaorm_test.go`<br>`internal/custom/rust/seaorm.go` | Detects #[sea_orm(belongs_to=..., from=..., to=...)] with explicit FK column references |
| Lazy loading recognition | 🟢 `partial` | `2026-05-30` | backfill:dictionary-completeness | `internal/custom/rust/diesel_seaorm_test.go`<br>`internal/custom/rust/seaorm.go` | Detects .find_related(), .find_linked(), LoaderTrait::load_many/load_one patterns |
| Relationship extraction | 🟢 `partial` | `2026-05-30` | backfill:dictionary-completeness | `internal/custom/rust/seaorm.go`<br>`internal/custom/rust/testdata/seaorm_entity.rs` | DeriveRelation variants + impl Related resolved to target Entity path string; linking 'super::user::Entity' to its concrete Model entity needs cross-module resolution. |

### Queries

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Query attribution | ✅ `full` | `2026-05-28` | — | `internal/engine/rules/rust/orms/seaorm.yaml` | — |

### Migrations

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Migration parsing | 🟢 `partial` | `2026-05-30` | — | `internal/custom/rust/seaorm.go`<br>`internal/custom/rust/testdata/seaorm_migration.rs` | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.rust.orm.seaorm ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
