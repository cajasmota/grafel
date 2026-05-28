<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.jsts.orm.objection` — Objection.js

Auto-generated. Back to [summary](../summary.md).

- **Language:** [JS/TS](../by-language/jsts.md)
- **Category:** [orm](../by-category/orm.md)
- **Capability cells:** 4

## Capabilities

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Migration parsing | ✅ `full` | `2026-05-28` | — | `internal/custom/javascript/extractors_coverage_test.go`<br>`internal/custom/javascript/objection.go` | — |
| Model extraction | ✅ `full` | `2026-05-28` | — | `internal/custom/javascript/extractors_coverage_test.go`<br>`internal/custom/javascript/objection.go`<br>`internal/engine/rules/javascript_typescript/orms/objection.yaml` | — |
| Query attribution | ✅ `full` | — | — | `internal/engine/orm_queries_jsts_drivers.go`<br>`internal/engine/orm_queries_jsts_drivers_test.go` | — |

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
