<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.jsts.orm.mongoose` — Mongoose

Auto-generated. Back to [summary](../summary.md).

- **Language:** [JS/TS](../by-language/jsts.md)
- **Category:** [orm](../by-category/orm.md)
- **Subcategory:** ORM / Data Mapper
- **Capability cells:** 8

## Capabilities


### Models

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Model extraction | ✅ `full` | `2026-05-28` | — | `internal/engine/rules/javascript_typescript/orms/mongoose.yaml` | — |
| Schema extraction | ✅ `full` | — | 3064 | `internal/custom/javascript/extractors_test.go`<br>`internal/custom/javascript/mongoose.go` | — |

### Relationships

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Association extraction | 🟢 `partial` | — | 3064 | `internal/custom/javascript/extractors_coverage_test.go`<br>`internal/custom/javascript/mongoose.go` | reMongoosePopulate captures .populate() traversal calls (navigate-to-ref); the definition-side ref: field in Schema() is not extracted, so reference declarations are missing |
| Foreign key extraction | — `not_applicable` | — | 3064 | — | MongoDB is a document-oriented database; there is no relational FK concept |
| Lazy loading recognition | — `not_applicable` | — | 3064 | — | Mongoose/MongoDB has no lazy-loading mechanism |
| Relationship extraction | 🟢 `partial` | — | 3064 | `internal/custom/javascript/extractors_coverage_test.go`<br>`internal/custom/javascript/mongoose.go` | .populate() traversals are captured as query entities; schema-level ref: declarations (the definition side of Mongoose associations) are not parsed |

### Queries

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Query attribution | ✅ `full` | `2026-05-28` | — | `internal/engine/orm_queries_jsts.go` | — |

### Migrations

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Migration parsing | — `not_applicable` | — | — | — | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.jsts.orm.mongoose ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
