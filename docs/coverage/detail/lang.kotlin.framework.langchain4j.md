<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.kotlin.framework.langchain4j` — LangChain4J (Kotlin)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [kotlin](../by-language/kotlin.md)
- **Category:** [http_framework](../by-category/http_framework.md)
- **Subcategory:** AI Integration
- **Capability cells:** 4

## Capabilities


### Prompts

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Prompt template extraction | 🟢 `partial` | `2026-06-12` | 4924 | `internal/custom/kotlin/extractors_test.go`<br>`internal/custom/kotlin/langchain4j.go` | reLc4jKotlinSystemMsg / reLc4jKotlinUserMsg capture @SystemMessage/@UserMessage-annotated fun as SCOPE.Pattern prompt-template entities. Runtime template-variable resolution not traced (parity with Java langchain4j). |

### Composition

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Chain composition | 🟢 `partial` | `2026-06-12` | 4924 | `internal/custom/kotlin/extractors_test.go`<br>`internal/custom/kotlin/langchain4j.go` | @AiService interfaces -> SCOPE.Service; ChatLanguageModel/StreamingChatLanguageModel fields -> SCOPE.Component; EmbeddingStore/ContentRetriever/Ingestor RAG fields -> SCOPE.Component. Structural composition captured; runtime chain wiring not traced. Proven by TestLangChain4jAiService / TestLangChain4jChatModel. |
| Tool use detection | ✅ `full` | `2026-06-12` | — | `internal/custom/kotlin/extractors_test.go`<br>`internal/custom/kotlin/langchain4j.go` | reLc4jKotlinTool extracts @Tool-annotated fun (with or without description arg) as SCOPE.Operation tool entities. Proven by TestLangChain4jTool. |

### Tracking

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Confidence overlay | 🟢 `partial` | `2026-06-12` | 3093 | `internal/links/constant_propagation.go`<br>`internal/links/taint_flow.go` | Framework-blind substrate stamps per-binding/per-finding Confidence on Kotlin entities; the langchain4j extractor does not yet stamp top-level EntityRecord.Confidence directly. Parity with Java langchain4j. |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.kotlin.framework.langchain4j ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
