<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.java.framework.langchain4j` — LangChain4J (LLM agent framework)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [java](../by-language/java.md)
- **Category:** [http_framework](../by-category/http_framework.md)
- **Subcategory:** AI Integration
- **Capability cells:** 4

## Capabilities


### Prompts

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Prompt template extraction | 🔴 `missing` | `2026-05-29` | [link](https://github.com/cajasmota/archigraph/issues/3586) | `internal/custom/java/langchain4j.go` | @SystemMessage/@UserMessage annotation extraction: lc4jSystemMessageRE and lc4jUserMessageRE capture annotation-level prompt template strings. Runtime template variable resolution not captured. |

### Composition

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Chain composition | 🔴 `missing` | `2026-05-29` | [link](https://github.com/cajasmota/archigraph/issues/3586) | `internal/custom/java/langchain4j.go` | @AiService interface extraction plus RAG/ChatMemory component detection: structural composition captured (AiService, EmbeddingStoreContentRetriever, EmbeddingStoreIngestor, ChatMemory fields). Runtime chain wiring not traced. |
| Tool use detection | 🔴 `missing` | `2026-05-29` | [link](https://github.com/cajasmota/archigraph/issues/3586) | `internal/custom/java/langchain4j.go` | @Tool annotation extraction: lc4jToolMethodRE extracts @Tool-annotated methods, emitting SCOPE.Operation entities with tool_method property. Dynamic tool registration not captured. |

### Tracking

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Confidence overlay | 🟢 `partial` | `2026-05-29` | [link](https://github.com/cajasmota/archigraph/issues/3093) | `internal/links/constant_propagation.go`<br>`internal/links/effect_propagation.go`<br>`internal/links/taint_flow.go`<br>`internal/substrate/effect_sinks_java.go`<br>`internal/substrate/java.go`<br>`internal/substrate/taint_sites_java.go` | Framework-blind substrate: constant_propagation, effect_propagation, and taint_flow passes emit per-binding/per-finding Confidence values on Java entities via java.go sniffers. EntityRecord.Confidence not yet stamped by the Java extractor directly; MCP min_confidence filtering applies. Partial pending a dedicated confidence-scoring pass writing top-level EntityRecord.Confidence. |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.java.framework.langchain4j ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
