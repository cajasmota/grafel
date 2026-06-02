<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.csharp.orm.nhibernate` — NHibernate

Auto-generated. Back to [summary](../summary.md).

- **Language:** [C#](../by-language/csharp.md)
- **Category:** [orm](../by-category/orm.md)
- **Subcategory:** ORM / Data Mapper
- **Capability cells:** 9

## Capabilities


### Models

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Model extraction | 🟢 `partial` | `2026-05-30` | 3263 | `internal/custom/csharp/dapper_models.go` | FluentNHibernate ClassMap<T> subclass declarations detected via regex; heuristic |
| Schema extraction | 🟢 `partial` | `2026-05-30` | 3263 | `internal/custom/csharp/dapper_models.go` | FluentNHibernate Map(x => x.Prop) column mapping detected via regex; heuristic |

### Relationships

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Association extraction | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/custom/csharp/orm_relationships.go` | FluentNHibernate References()/HasMany()/HasOne() fluent calls detected via reNHRef/reNHHM/reNHHasOne with cardinality tagging |
| Foreign key extraction | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/custom/csharp/orm_relationships.go` | .Column("fk_col") chained after References/HasMany detected via reNHColumn; FK column names extracted |
| Lazy loading recognition | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/custom/csharp/orm_relationships.go` | .LazyLoad() and .Not.LazyLoad() fluent calls detected via reNHLazyLoad/reNHNotLazyLoad |
| Relationship extraction | 🟢 `partial` | `2026-05-30` | 3263 | `internal/custom/csharp/dapper_models.go` | FluentNHibernate References/HasMany fluent relationship calls detected via regex; heuristic |

### Queries

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Query attribution | 🟢 `partial` | `2026-05-30` | 3263 | `internal/custom/csharp/dapper_models.go` | ISession.Query<T>/Get<T>/Load<T> calls detected via regex; heuristic |

### Migrations

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Migration parsing | — `not_applicable` | — | — | — | micro-ORM/query-lib — no built-in migration system |

### Transactions

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Transaction function stamping | 🔴 `missing` | — | 3628-transaction-function-stamping | — | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.csharp.orm.nhibernate ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
