<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.java.orm.neo4j` — Neo4j (Java driver)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [java](../by-language/java.md)
- **Category:** [orm](../by-category/orm.md)
- **Subcategory:** ORM / Data Mapper
- **Capability cells:** 8

## Capabilities


### Models

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Model extraction | 🟢 `partial` | `2026-05-28` | — | `internal/engine/rules/java/orms/neo4j.yaml` | — |
| Schema extraction | 🟢 `partial` | — | 3098 | `internal/custom/java/neo4j.go` | No Neo4j Java ORM extractor; @Node annotation for node entity extraction not implemented. |

### Relationships

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Association extraction | 🟢 `partial` | — | 3098 | `internal/custom/java/neo4j.go` | No Neo4j Java ORM extractor (Spring Data Neo4j @Node/@Relationship annotations not handled). Tracked in issue #3001. |
| Foreign key extraction | — `not_applicable` | — | 3098 | `internal/custom/java/neo4j.go` | Neo4j is a graph database with no foreign key concept; foreign_key_extraction is not applicable |
| Lazy loading recognition | — `not_applicable` | — | 3098 | `internal/custom/java/neo4j.go` | Neo4j Spring Data has no lazy-loading concept equivalent to relational ORMs; not applicable |
| Relationship extraction | 🟢 `partial` | — | 3098 | `internal/custom/java/neo4j.go` | Neo4j graph relationships require @Relationship annotation extraction from Spring Data Neo4j; no extractor exists. |

### Queries

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Query attribution | 🔴 `missing` | `2026-06-02` | [link](https://github.com/cajasmota/archigraph/issues/3645) | — | YAML detection-only; dead custom_extractor never ran in Go; no native query-topology extractor. |

### Migrations

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Migration parsing | — `not_applicable` | — | 3098 | `internal/custom/java/neo4j.go` | Neo4j graph database has no SQL migration files; migration_parsing is not applicable |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.java.orm.neo4j ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
