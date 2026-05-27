<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.python.orm.sqlalchemy` — SQLAlchemy

Auto-generated. Back to [summary](../summary.md).

- **Language:** [python](../by-language/python.md)
- **Category:** [orm](../by-category/orm.md)
- **Capability cells:** 3

## Capabilities

| Capability | Status | Verified at | Verified SHA | Issue | Cites |
|------------|--------|-------------|--------------|-------|-------|
| `migration_parsing` | ⚠️ `partial` | — | — | [link](https://github.com/cajasmota/archigraph/issues/2720) | — |
| `model_extraction` | ✅ `full` | `2026-05-28` | — | — | `internal/extractors/cross/ormlink/extractor.go` |
| `query_attribution` | ✅ `full` | `2026-05-28` | — | — | `internal/engine/orm_queries_python.go` |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.python.orm.sqlalchemy ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
