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
| Prompt template extraction | ✅ `full` | `2026-06-13` | 5013 | `internal/custom/kotlin/extractors_test.go`<br>`internal/custom/kotlin/langchain4j.go` | #5013: @SystemMessage/@UserMessage inline templates and PromptTemplate.from("...{{var}}...") are captured as SCOPE.Pattern prompt-template entities (provenance INFERRED_FROM_LANGCHAIN4J_PROMPT / INFERRED_FROM_LANGCHAIN4J_PROMPT_TEMPLATE). reLc4jKotlin{SystemMsg,UserMsg}Tpl read the annotation's template string body AND the decorated fun's parameter list; reLc4jKotlinTplVar{Double,Single} extract {{var}} (and legacy {var}) placeholders; reLc4jKotlinParam resolves each placeholder to the binding param — an @V("x")-annotated param binds template variable x, an un-annotated param binds by name. Each resolved placeholder emits a template_var.<name> property (value = bound kotlin param identifier) plus a DEPENDS_ON edge from the prompt pattern to that param (Properties binding=prompt_template_variable, template_var, resolved_from=method_param); the full template body (truncated 240b), template_vars (comma-joined), and template_var_count are stored as Properties. Programmatic PromptTemplate.from(...) has no surrounding fun params so placeholders are recorded as unbound template vars with no edge. Proven by TestLangChain4jPromptTemplateVars + TestLangChain4jPromptTemplateFrom (parity with Java langchain4j prompt_template_extraction). |

### Composition

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Chain composition | ✅ `full` | `2026-06-13` | 5083 | `internal/custom/kotlin/extractors_test.go`<br>`internal/custom/kotlin/langchain4j.go` | Structural: @AiService interfaces -> SCOPE.Service; ChatLanguageModel/StreamingChatLanguageModel fields -> SCOPE.Component; EmbeddingStore/ContentRetriever/Ingestor RAG fields -> SCOPE.Component. Runtime (#5012): reLc4jKotlinServiceWiring traces a `val svc = AiServices.builder(IFace::class.java).chatLanguageModel(model).tools(tools).chatMemory(memory).contentRetriever(retriever).build()` assembly into a SCOPE.Service entity (provenance INFERRED_FROM_LANGCHAIN4J_AI_SERVICES_BUILDER) carrying USES edges to each wired component by referenced identifier, with wire_role property (chat_model/tools/chat_memory/content_retriever/retrieval_augmentor/etc) and wire.* role flags. #5083 closes the inline/positional gaps: (1) inline-expression args .chatLanguageModel(OpenAiChatModel.builder().build()) / .tools(MyTools()) now materialize a synthetic SCOPE.Component (provenance INFERRED_FROM_LANGCHAIN4J_INLINE_COMPONENT, constructed=true) for the constructed type and point the USES edge at it; reLc4jKotlinWireStep captures the whole arg expression (one nested-paren level) and classifyWireArg resolves bare-identifier (arg_kind=identifier) vs inline-constructor (arg_kind=inline_component). (2) The positional AiServices.create(IFace::class.java, model, tools) overload is traced by reLc4jKotlinServiceCreate (provenance INFERRED_FROM_LANGCHAIN4J_AI_SERVICES_CREATE); splitTopLevelArgs splits at top-level commas only and a positional schedule binds arg0=interface(skipped)/arg1=chat_model/arg2=tools, resolving identifier or inline args identically. Proven by TestLangChain4jServiceWiring + TestLangChain4jServiceWiringInlineArgs + TestLangChain4jServiceCreatePositional + TestLangChain4jServiceCreateInlineModel (parity with Java langchain4j chain_composition). Remaining: cross-file/DI-injected wired identifiers resolve structurally (by-name) only, not regex-resolvable. |
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
