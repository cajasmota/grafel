<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.ocaml.orm.caqti` — Caqti (OCaml DB)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [OCaml](../by-language/ocaml.md)
- **Category:** [orm](../by-category/orm.md)
- **Subcategory:** ORM / Data Mapper
- **Capability cells:** 11

## Capabilities


### Models

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Model extraction | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Model lifecycle extraction | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Schema extraction | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |

### Relationships

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Association extraction | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Foreign key extraction | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Lazy loading recognition | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Relationship extraction | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |

### Queries

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Query attribution | 🟢 `partial` | `2026-06-24` | 5374 | `internal/extractors/ocaml/extractor.go` | extractCaqtiQueries recognises Caqti query definitions via the Caqti_request.Infix arity operators and ppx_rapper quotations, emitting one SCOPE.Operation(subtype=db_query) per query with db_library=caqti, query_verb and the SQL signature. A SQL verb is required (honest reject of non-SQL strings). Honest partial: typed row encoder/decoder spec, connection-module dispatch (Db.exec/find) and the bound table/model graph are not modelled. Proven by TestCaqti_InfixQueries + TestCaqti_Rapper. |

### Migrations

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Migration parsing | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Migration schema ops | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |

### Transactions

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Transaction function stamping | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.ocaml.orm.caqti ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
