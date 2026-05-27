<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `ci.buildkite` — Buildkite

Auto-generated. Back to [summary](../summary.md).

- **Language:** [multi](../by-language/multi.md)
- **Category:** [configuration](../by-category/configuration.md)
- **Capability cells:** 2

## Capabilities

| Capability | Status | Verified at | Verified SHA | Issue | Cites |
|------------|--------|-------------|--------------|-------|-------|
| `env_resolution` | ⚠️ `partial` | `2026-05-28` | — | — | `internal/extractors/yaml/extractor.go` |
| `file_parsing` | ✅ `full` | `2026-05-28` | — | — | `internal/engine/rules/cicd/frameworks/buildkite.yaml` |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update ci.buildkite ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
