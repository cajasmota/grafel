<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `security.sql-injection` — SQL injection heuristic (f-string / .format() / % interpolation into SQL)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [multi](../by-language/multi.md)
- **Category:** [security](../by-category/security.md)
- **Capability cells:** 1

## Capabilities

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| SQL injection | ✅ `full` | `2026-05-28` | — | `internal/engine/rules/_engine/sql_injection_detector.yaml` | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update security.sql-injection ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
