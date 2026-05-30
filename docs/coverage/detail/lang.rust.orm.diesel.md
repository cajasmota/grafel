<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.rust.orm.diesel` — Diesel

Auto-generated. Back to [summary](../summary.md).

- **Language:** [rust](../by-language/rust.md)
- **Category:** [orm](../by-category/orm.md)
- **Subcategory:** ORM / Data Mapper
- **Capability cells:** 8

## Capabilities


### Models

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Model extraction | ✅ `full` | `2026-05-28` | — | `internal/engine/rules/rust/orms/diesel.yaml` | — |
| Schema extraction | ✅ `full` | `2026-05-30` | — | `internal/custom/rust/diesel.go`<br>`internal/custom/rust/orm_props_test.go`<br>`internal/custom/rust/testdata/diesel_schema.rs` | — |

### Relationships

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Association extraction | 🟢 `partial` | `2026-05-30` | backfill:dictionary-completeness | `internal/custom/rust/diesel.go`<br>`internal/custom/rust/testdata/diesel_models.rs` | — |
| Foreign key extraction | 🟢 `partial` | `2026-05-30` | backfill:dictionary-completeness | `internal/custom/rust/diesel.go`<br>`internal/custom/rust/diesel_seaorm_test.go` | Detects *_id columns in table! macro bodies as FK signals; joinable!() also captures FK relationships |
| Lazy loading recognition | — `not_applicable` | `2026-05-30` | — | — | Diesel is synchronous and does not support lazy loading; eager joins via joinable!/load() only |
| Relationship extraction | 🟢 `partial` | `2026-05-30` | backfill:dictionary-completeness | `internal/custom/rust/diesel.go`<br>`internal/custom/rust/testdata/diesel_models.rs`<br>`internal/custom/rust/testdata/diesel_schema.rs` | joinable!/belongs_to detected with from/to tables; resolving the target Rust model path to its table! schema requires cross-file import-graph analysis (diesel.toml schema path). |

### Queries

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Query attribution | ✅ `full` | `2026-05-28` | — | `internal/engine/rules/rust/orms/diesel.yaml` | — |

### Migrations

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Migration parsing | ✅ `full` | `2026-05-30` | — | `internal/custom/rust/diesel.go`<br>`internal/custom/rust/diesel_seaorm_test.go`<br>`internal/custom/rust/orm_props_test.go`<br>`internal/custom/rust/testdata/diesel_up.sql` | Detects embed_migrations!(), run_pending_migrations(), impl MigrationHarness patterns |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.rust.orm.diesel ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
