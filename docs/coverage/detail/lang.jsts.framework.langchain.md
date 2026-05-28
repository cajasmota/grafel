<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.jsts.framework.langchain` — LangChain.js

Auto-generated. Back to [summary](../summary.md).

- **Language:** [JS/TS](../by-language/jsts.md)
- **Category:** [http_framework](../by-category/http_framework.md)
- **Subcategory:** AI Integration
- **Capability cells:** 3

## Capabilities


### Prompts

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|
| `prompt_template_extraction` | ✅ `full` | `2026-05-28` | — | [link](2865) | `internal/engine/langchain_detect_test.go`<br>`internal/engine/rules/javascript_typescript/frameworks/langchain.yaml`<br>`testdata/fixtures/typescript/langchain_chain.ts` | — |

### Composition

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|
| `chain_composition` | ✅ `full` | `2026-05-28` | — | [link](2865) | `internal/engine/langchain_detect_test.go`<br>`internal/engine/rules/javascript_typescript/frameworks/langchain.yaml`<br>`testdata/fixtures/typescript/langchain_chain.ts` | — |
| `tool_use_detection` | ✅ `full` | — | — | [link](2865) | `internal/engine/langchain_detect_test.go`<br>`internal/engine/rules/javascript_typescript/frameworks/langchain.yaml`<br>`testdata/fixtures/typescript/langchain_chain.ts` | — |

### Tracking

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.jsts.framework.langchain ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
