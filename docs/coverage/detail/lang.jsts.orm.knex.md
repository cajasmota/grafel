<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.jsts.orm.knex` — Knex (query builder)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [JS/TS](../by-language/jsts.md)
- **Category:** [orm](../by-category/orm.md)
- **Capability cells:** 3

## Capabilities

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Migration parsing | ✅ `full` | `2026-05-28` | — | `internal/custom/javascript/extractors_coverage_test.go`<br>`internal/custom/javascript/knex.go` | — |
| Model extraction | — `not_applicable` | — | — | — | Knex is a SQL query builder, not an ORM — it has no model/entity layer to extract. Persistent model_extraction belongs to Objection.js, which layers Active-Record models on top of Knex (see lang.jsts.orm.objection). |
| Query attribution | ✅ `full` | `2026-05-28` | — | `internal/engine/orm_queries_jsts_drivers.go`<br>`internal/engine/orm_queries_jsts_drivers_test.go` | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.jsts.orm.knex ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
