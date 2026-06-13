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
| Chain composition | ✅ `full` | `2026-06-14` | [link](https://github.com/cajasmota/archigraph/issues/5155) | `internal/custom/java/langchain4j.go`<br>`internal/custom/java/langchain4j_wiring_test.go` | Structural: @AiService interfaces -> SCOPE.Service; ChatLanguageModel/StreamingChatLanguageModel fields -> SCOPE.Component; EmbeddingStore/ContentRetriever/Ingestor RAG fields -> SCOPE.Pattern; ChatMemory fields -> SCOPE.Component. Runtime (#5155, port of kotlin #5012/#5083): lc4jServiceWiringRE traces a `Assistant assistant = AiServices.builder(Assistant.class).chatLanguageModel(model).tools(tools).chatMemory(memory).contentRetriever(retriever).build();` assembly into a SCOPE.Service entity (provenance INFERRED_FROM_LANGCHAIN4J_AI_SERVICES_BUILDER) carrying USES edges to each wired component, with wire_role property (chat_model/tools/chat_memory/content_retriever/retrieval_augmentor/etc) and wire.* role flags. The inline/positional gaps are closed too: (1) inline-expression args .chatLanguageModel(OpenAiChatModel.builder().build()) / .tools(new MyTools()) materialize a synthetic SCOPE.Component (provenance INFERRED_FROM_LANGCHAIN4J_INLINE_COMPONENT, constructed=true) for the constructed type and point the USES edge at it; lc4jWireStepRE captures the whole arg expression (one nested-paren level) and lc4jClassifyWireArg resolves bare-identifier (arg_kind=identifier) vs inline-constructor (arg_kind=inline_component, java `new T()` / `T.builder()`). (2) The positional AiServices.create(IFace.class, model, tools) overload is traced by lc4jServiceCreateRE (provenance INFERRED_FROM_LANGCHAIN4J_AI_SERVICES_CREATE); lc4jSplitTopLevelArgs splits at top-level commas only and a positional schedule binds arg0=interface(skipped)/arg1=chat_model/arg2=tools, resolving identifier or inline args identically. Bare-identifier USES targets resolve to an emitted component/field by name via findRefForType, else a synthetic scope:dependency:langchain4j_wired ref. Proven by TestLc4jServiceWiringBuilder + TestLc4jServiceWiringInlineArgs + TestLc4jServiceCreatePositional + TestLc4jServiceCreateInlineModel + TestLc4jServiceWiringWrongLanguageNoOp + TestLc4jServiceWiringNoMatchNoOp (parity with kotlin langchain4j chain_composition). Residual: cross-file/DI-injected wired identifiers resolve structurally (by-name) only — inherent regex limit, not resolved across files. |
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
