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
| Chain composition | ✅ `full` | `2026-06-12` | 5012 | `internal/custom/kotlin/extractors_test.go`<br>`internal/custom/kotlin/langchain4j.go` | Structural: @AiService interfaces -> SCOPE.Service; ChatLanguageModel/StreamingChatLanguageModel fields -> SCOPE.Component; EmbeddingStore/ContentRetriever/Ingestor RAG fields -> SCOPE.Component. Runtime (#5012): reLc4jKotlinServiceWiring traces a `val svc = AiServices.builder(IFace::class.java).chatLanguageModel(model).tools(tools).chatMemory(memory).contentRetriever(retriever).build()` assembly into a SCOPE.Service entity (provenance INFERRED_FROM_LANGCHAIN4J_AI_SERVICES_BUILDER) carrying USES edges to each wired component by referenced identifier, with wire_role property (chat_model/tools/chat_memory/content_retriever/retrieval_augmentor/etc) and wire.* role flags. Inline-expression / class-literal-only args record the wire role but emit no resolvable USES target. Proven by TestLangChain4jServiceWiring (parity with Java langchain4j chain_composition). |
| Tool use detection | ✅ `full` | `2026-06-12` | — | `internal/custom/kotlin/extractors_test.go`<br>`internal/custom/kotlin/langchain4j.go` | reLc4jKotlinTool extracts @Tool-annotated fun (with or without description arg) as SCOPE.Operation tool entities. Proven by TestLangChain4jTool. |

### Tracking

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Confidence overlay | ✅ `full` | `2026-06-12` | 4974 | `internal/custom/kotlin/extractors_test.go`<br>`internal/custom/kotlin/langchain4j.go`<br>`internal/types/confidence.go` | #4974 (parity with Java #3093): the langchain4j extractor now stamps a top-level EntityRecord.Confidence directly on every emitted entity (@AiService/@Tool/@SystemMessage/@UserMessage/ChatLanguageModel/ChatMemory/RAG). All entities are regex pattern matches so the stamped value is BaseConfidence(SourceRegexPattern)=0.7; the framework-blind per-binding/per-finding substrate overlay still applies on top. Proven by TestLangChain4jConfidenceStamp. |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.kotlin.framework.langchain4j ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
