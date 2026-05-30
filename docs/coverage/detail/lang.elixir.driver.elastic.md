<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.elixir.driver.elastic` — elasticsearch-elixir

Auto-generated. Back to [summary](../summary.md).

- **Language:** [elixir](../by-language/elixir.md)
- **Category:** [orm](../by-category/orm.md)
- **Subcategory:** ORM / Data Mapper
- **Capability cells:** 8

## Capabilities


### Models

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Model extraction | — `not_applicable` | — | — | — | — |
| Schema extraction | — `not_applicable` | — | — | — | Raw Elixir DB driver; no schema/model definition. Schema belongs to Ecto ORM layer, not the driver. |

### Relationships

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Association extraction | — `not_applicable` | — | — | — | Raw driver has no association/relationship concept; Ecto handles associations independently. |
| Foreign key extraction | — `not_applicable` | — | — | — | Foreign key awareness lives in Ecto schema layer, not the raw driver. |
| Lazy loading recognition | — `not_applicable` | — | — | — | Raw driver; no lazy loading concept. |
| Relationship extraction | — `not_applicable` | — | — | — | Raw driver protocol; relationship modelling is Ecto's responsibility. |

### Queries

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Query attribution | 🟢 `partial` | `2026-05-28` | — | `internal/engine/rules/elixir/frameworks/elasticsearch_elixir.yaml` | — |

### Migrations

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Migration parsing | — `not_applicable` | — | — | — | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.elixir.driver.elastic ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
