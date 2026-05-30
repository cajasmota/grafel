<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.elixir.orm.ecto` — Ecto

Auto-generated. Back to [summary](../summary.md).

- **Language:** [elixir](../by-language/elixir.md)
- **Category:** [orm](../by-category/orm.md)
- **Subcategory:** ORM / Data Mapper
- **Capability cells:** 8

## Capabilities


### Models

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Model extraction | ✅ `full` | `2026-05-28` | — | `internal/engine/rules/elixir/frameworks/ecto_standalone.yaml` | — |
| Schema extraction | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/custom/elixir/ecto.go`<br>`internal/extractors/elixir/elixir.go` | schema "table_name" do blocks extracted as SCOPE.Schema; tree-sitter extractor also emits schema entities; field :name, :type declarations captured |

### Relationships

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Association extraction | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/custom/elixir/ecto.go` | has_one/has_many/many_to_many association macros extracted as SCOPE.Component with association_type+association_name properties |
| Foreign key extraction | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/custom/elixir/ecto.go` | belongs_to associations extracted; Ecto implies FK via belongs_to :field, Schema; explicit foreign_key option not yet parsed |
| Lazy loading recognition | — `not_applicable` | — | — | — | Ecto has no lazy loading; all associations must be explicitly preloaded via Repo.preload/2. Not_applicable by design. |
| Relationship extraction | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/custom/elixir/ecto.go` | Ecto association macros (has_one/has_many/belongs_to/many_to_many) extracted; relationship type preserved in properties |

### Queries

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Query attribution | ✅ `full` | `2026-05-28` | — | `internal/engine/orm_queries_other.go` | — |

### Migrations

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Migration parsing | 🟢 `partial` | — | — | `internal/custom/elixir/ecto.go` | create table(:name) migration macros extracted as SCOPE.Schema/migration; add/remove column not yet tracked |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.elixir.orm.ecto ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
