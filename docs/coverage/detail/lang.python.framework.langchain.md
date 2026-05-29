<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.python.framework.langchain` — LangChain (LLM agent framework)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [python](../by-language/python.md)
- **Category:** [http_framework](../by-category/http_framework.md)
- **Subcategory:** AI Integration
- **Capability cells:** 4

## Capabilities


### Prompts

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Prompt template extraction | 🟢 `partial` | — | — | `internal/custom/python/langchain.go` | lcChatPromptRe+lcPromptTmplRe+lcFewShotRe detect ChatPromptTemplate/PromptTemplate/FewShotPromptTemplate (#3181) |

### Composition

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Chain composition | 🟢 `partial` | — | — | `internal/custom/python/langchain.go` | lcLCELChainRe+lcLegacyChainRe detect LCEL pipe chains and legacy LLMChain/SequentialChain/etc (#3181) |
| Tool use detection | 🟢 `partial` | — | — | `internal/custom/python/langchain.go` | lcToolDecoratorRe+lcToolClassRe+lcStructToolRe+lcToolConsRe detect @tool decorated fns, BaseTool subclasses, StructuredTool, Tool constructor (#3181) |

### Tracking

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Confidence overlay | 🟢 `partial` | `2026-05-29` | [link](https://github.com/cajasmota/archigraph/issues/3093) | `internal/links/constant_propagation.go`<br>`internal/links/effect_propagation.go`<br>`internal/links/taint_flow.go`<br>`internal/substrate/effect_sinks_python.go`<br>`internal/substrate/python.go`<br>`internal/substrate/taint_sites_python.go` | Framework-blind substrate: constant_propagation, effect_propagation, and taint_flow passes emit per-binding/per-finding Confidence values on Python entities via python.go sniffers. Partial: top-level EntityRecord.Confidence not yet written by the extractor directly. |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.python.framework.langchain ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
