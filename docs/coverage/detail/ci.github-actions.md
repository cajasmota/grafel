<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `ci.github-actions` — GitHub Actions

Auto-generated. Back to [summary](../summary.md).

- **Language:** [multi](../by-language/multi.md)
- **Category:** [ci_cd](../by-category/ci_cd.md)
- **Capability cells:** 2

## Capabilities

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Env resolution | 🟢 `partial` | `2026-05-28` | — | `internal/extractors/yaml/extractor.go` | — |
| File parsing | ✅ `full` | `2026-05-28` | — | `internal/engine/rules/cicd/frameworks/github_actions.yaml`<br>`internal/extractors/yaml/extractor.go` | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update ci.github-actions ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
