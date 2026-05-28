<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `build.turborepo` — Turborepo

Auto-generated. Back to [summary](../summary.md).

- **Language:** [JS/TS](../by-language/jsts.md)
- **Category:** [build_system](../by-category/build_system.md)
- **Capability cells:** 2

## Capabilities

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|
| `dependency_graph` | ✅ `full` | `2026-05-28` | — | — | `internal/engine/rules/javascript_typescript/build_tools.yaml`<br>`internal/extractors/config/discover.go`<br>`internal/extractors/config/discover_test.go` | — |
| `target_extraction` | ✅ `full` | `2026-05-28` | — | — | `internal/engine/rules/javascript_typescript/build_tools.yaml`<br>`internal/extractors/config/discover.go`<br>`internal/extractors/config/discover_test.go` | — |

## Framework-specific

### Monorepo Task Graph

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|
| `task_pipeline_graph` | ✅ `full` | `2026-05-28` | — | — | `internal/extractors/config/discover.go`<br>`internal/extractors/config/discover_test.go` | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update build.turborepo ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
