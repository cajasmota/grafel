<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.java.framework.langchain4j` — LangChain4J (LLM agent framework)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [java](../by-language/java.md)
- **Category:** [http_framework](../by-category/http_framework.md)
- **Subcategory:** AI Integration
- **Capability cells:** 3

## Capabilities


### Prompts

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Prompt template extraction | ⚠️ `partial` | `2026-05-29` | — | `internal/custom/java/langchain4j.go` | @SystemMessage/@UserMessage annotation extraction: lc4jSystemMessageRE and lc4jUserMessageRE capture annotation-level prompt template strings. Runtime template variable resolution not captured. |

### Composition

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Chain composition | ⚠️ `partial` | `2026-05-29` | — | `internal/custom/java/langchain4j.go` | @AiService interface extraction plus RAG/ChatMemory component detection: structural composition captured (AiService, EmbeddingStoreContentRetriever, EmbeddingStoreIngestor, ChatMemory fields). Runtime chain wiring not traced. |
| Tool use detection | ⚠️ `partial` | `2026-05-29` | — | `internal/custom/java/langchain4j.go` | @Tool annotation extraction: lc4jToolMethodRE extracts @Tool-annotated methods, emitting SCOPE.Operation entities with tool_method property. Dynamic tool registration not captured. |

### Tracking

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.java.framework.langchain4j ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
