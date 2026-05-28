<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.jsts.orm.mikro-orm` — MikroORM

Auto-generated. Back to [summary](../summary.md).

- **Language:** [JS/TS](../by-language/jsts.md)
- **Category:** [orm](../by-category/orm.md)
- **Capability cells:** 3

## Capabilities

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|
| `migration_parsing` | ✅ `full` | `2026-05-28` | — | — | `internal/custom/javascript/extractors_coverage_test.go`<br>`internal/custom/javascript/mikroorm.go` | — |
| `model_extraction` | ✅ `full` | `2026-05-28` | — | — | `internal/custom/javascript/extractors_coverage_test.go`<br>`internal/custom/javascript/mikroorm.go`<br>`internal/engine/rules/javascript_typescript/orms/mikro_orm.yaml` | — |
| `query_attribution` | ⚠️ `partial` | `2026-05-28` | — | — | `internal/engine/rules/javascript_typescript/orms/mikro_orm.yaml` | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.jsts.orm.mikro-orm ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
