<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.go.driver.mysql` — go-sql-driver/mysql

Auto-generated. Back to [summary](../summary.md).

- **Language:** [go](../by-language/go.md)
- **Category:** [orm](../by-category/orm.md)
- **Subcategory:** ORM / Data Mapper
- **Capability cells:** 8

## Capabilities


### Models

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Model extraction | — `not_applicable` | — | — | — | — |
| Schema extraction | 🟢 `partial` | `2026-05-29` | 3214 | `internal/custom/golang/sql_drivers.go`<br>`internal/custom/golang/sql_drivers_test.go` | — |

### Relationships

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Association extraction | — `not_applicable` | `2026-05-29` | — | — | Raw database/sql driver: archigraph models SQL query effects, not an ORM object-relationship graph. Relationship/foreign-key/association/lazy-loading are ORM-mapper concepts with no surface in a bare driver (consistent with sqlite/pgx/sqlx/mongodb sibling drivers). |
| Foreign key extraction | — `not_applicable` | `2026-05-29` | — | — | Raw database/sql driver: archigraph models SQL query effects, not an ORM object-relationship graph. Relationship/foreign-key/association/lazy-loading are ORM-mapper concepts with no surface in a bare driver (consistent with sqlite/pgx/sqlx/mongodb sibling drivers). |
| Lazy loading recognition | — `not_applicable` | `2026-05-29` | — | — | Raw database/sql driver: archigraph models SQL query effects, not an ORM object-relationship graph. Relationship/foreign-key/association/lazy-loading are ORM-mapper concepts with no surface in a bare driver (consistent with sqlite/pgx/sqlx/mongodb sibling drivers). |
| Relationship extraction | — `not_applicable` | `2026-05-29` | — | — | Raw database/sql driver: archigraph models SQL query effects, not an ORM object-relationship graph. Relationship/foreign-key/association/lazy-loading are ORM-mapper concepts with no surface in a bare driver (consistent with sqlite/pgx/sqlx/mongodb sibling drivers). |

### Queries

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Query attribution | 🟢 `partial` | `2026-05-28` | — | `internal/engine/rules/go/orms/mysql_go.yaml` | — |

### Migrations

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Migration parsing | — `not_applicable` | — | — | — | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.go.driver.mysql ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
