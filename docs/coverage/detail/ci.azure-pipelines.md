<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `ci.azure-pipelines` — Azure Pipelines

Auto-generated. Back to [summary](../summary.md).

- **Language:** [multi](../by-language/multi.md)
- **Category:** [ci_cd](../by-category/ci_cd.md)
- **Capability cells:** 2

## Capabilities

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Env resolution | 🟢 `partial` | `2026-05-28` | — | `internal/extractors/yaml/extractor.go` | — |
| File parsing | ✅ `full` | `2026-05-28` | — | `internal/engine/rules/cicd/frameworks/azure_pipelines.yaml` | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update ci.azure-pipelines ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
