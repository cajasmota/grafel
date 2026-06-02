<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.jsts.orm.objection` — Objection.js

Auto-generated. Back to [summary](../summary.md).

- **Language:** [JS/TS](../by-language/jsts.md)
- **Category:** [orm](../by-category/orm.md)
- **Subcategory:** ORM / Data Mapper
- **Capability cells:** 10

## Capabilities


### Models

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Model extraction | ✅ `full` | `2026-05-28` | — | `internal/custom/javascript/extractors_coverage_test.go`<br>`internal/custom/javascript/objection.go`<br>`internal/engine/rules/javascript_typescript/orms/objection.yaml` | — |
| Schema extraction | ✅ `full` | — | 3064 | `internal/custom/javascript/extractors_coverage_test.go`<br>`internal/custom/javascript/objection.go` | — |

### Relationships

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Association extraction | ✅ `full` | — | 3064 | `internal/custom/javascript/extractors_coverage_test.go`<br>`internal/custom/javascript/objection.go` | — |
| Foreign key extraction | 🟢 `partial` | — | 3064 | `internal/custom/javascript/extractors_coverage_test.go`<br>`internal/custom/javascript/objection.go` | reObjectionRelation captures named relation entries (relation_type+field_name) from relationMappings, which encode FK join topology implicitly; the from/to column-level FK fields are not explicitly parsed |
| Lazy loading recognition | — `not_applicable` | — | 3071 | — | Objection.js uses withGraphFetched/withGraphJoined for eager loading; there is no built-in lazy-loading mechanism. The library loads related models explicitly on demand via separate queries, not through a lazy proxy or decorator pattern. lazy_loading_recognition is not applicable. |
| Relationship extraction | ✅ `full` | — | 3064 | `internal/custom/javascript/extractors_coverage_test.go`<br>`internal/custom/javascript/objection.go` | — |

### Queries

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Query attribution | ✅ `full` | — | — | `internal/engine/orm_queries_jsts_drivers.go`<br>`internal/engine/orm_queries_jsts_drivers_test.go` | — |

### Migrations

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Migration parsing | ✅ `full` | `2026-05-28` | — | `internal/custom/javascript/extractors_coverage_test.go`<br>`internal/custom/javascript/objection.go` | — |

### Transactions

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Transaction function stamping | 🔴 `missing` | — | 3628-transaction-function-stamping | — | — |

## Framework-specific

### Objection Relation Graph

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Relation graph extraction | ✅ `full` | `2026-05-28` | — | `internal/custom/javascript/extractors_coverage_test.go`<br>`internal/custom/javascript/objection.go` | Objection's bespoke `static relationMappings` declaration (BelongsToOneRelation / HasManyRelation / ManyToManyRelation / HasOneThroughRelation) drives its eager-load + nested-mutation graph API (withGraphFetched / upsertGraph). No standard ORM cell (model_extraction / query_attribution / migration_parsing) captures this relation-graph topology, so it is recorded as a framework-specific capability. Each relation entry is emitted as a SCOPE.Component relation entity tagged with its relation_type. |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.jsts.orm.objection ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
