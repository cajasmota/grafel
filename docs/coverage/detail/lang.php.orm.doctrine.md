<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.php.orm.doctrine` — Doctrine ORM

Auto-generated. Back to [summary](../summary.md).

- **Language:** [php](../by-language/php.md)
- **Category:** [orm](../by-category/orm.md)
- **Subcategory:** ORM / Data Mapper
- **Capability cells:** 8

## Capabilities


### Models

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Model extraction | ✅ `full` | `2026-05-28` | — | `internal/engine/rules/php/orms/doctrine_orm.yaml` | — |
| Schema extraction | ✅ `full` | `2026-05-30` | — | `internal/custom/php/orm_data.go` | Doctrine #[ORM\Column] attributes scanned forward to property names; association/FK/lazy/migration also extracted. |

### Relationships

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Association extraction | ✅ `full` | `2026-05-30` | — | `internal/custom/php/orm_data.go` | ORM relation type attributes scanned (OneToMany/ManyToMany). |
| Foreign key extraction | ✅ `full` | `2026-05-30` | — | `internal/custom/php/orm_data.go` | #[ORM\JoinColumn] attributes scanned. |
| Lazy loading recognition | ✅ `full` | `2026-05-30` | — | `internal/custom/php/orm_data.go` | fetch=LAZY / fetch='LAZY' annotation patterns scanned. |
| Relationship extraction | ✅ `full` | `2026-05-30` | — | `internal/custom/php/orm_data.go` | OneToMany/ManyToOne/ManyToMany/OneToOne Doctrine attributes scanned. |

### Queries

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Query attribution | ✅ `full` | `2026-05-28` | — | `internal/engine/orm_queries_other.go` | — |

### Migrations

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Migration parsing | ✅ `full` | `2026-05-30` | — | `internal/custom/php/orm_data.go` | AbstractMigration subclasses + up/down methods scanned. |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.php.orm.doctrine ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
