package python

import (
	"context"
	"regexp"
	"strconv"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func init() {
	extractor.Register("python_langchain", &LangChainExtractor{})
}

// LangChainExtractor extracts LangChain patterns: LCEL chains, agents, tools,
// RAG, prompts, and memory components.
type LangChainExtractor struct{}

func (e *LangChainExtractor) Language() string { return "python_langchain" }

var (
	lcLCELChainRe = regexp.MustCompile(`(?m)^([a-zA-Z_]\w*)\s*=\s*(.+?\|.+?)$`)
	lcPipeStageRe = regexp.MustCompile(`\s*\|\s*`)

	lcLegacyChainNames = []string{
		"LLMChain", "SequentialChain", "SimpleSequentialChain",
		"ConversationalRetrievalChain", "ConversationChain", "TransformChain",
		"MapReduceChain", "MapRerankChain", "StuffDocumentsChain",
		"RefineDocumentsChain", "LLMRouterChain", "MultiPromptChain", "RouterChain",
	}

	lcAgentFactoryRe  = regexp.MustCompile(`(?m)([a-zA-Z_]\w*)\s*=\s*(create_(?:react|tool_calling|openai_functions|openai_tools|structured_chat|json_chat|xml)_agent)\s*\(`)
	lcAgentExecutorRe = regexp.MustCompile(`(?m)([a-zA-Z_]\w*)\s*=\s*AgentExecutor(?:\.from_agent_and_tools)?\s*\(`)
	lcToolDecoratorRe = regexp.MustCompile(`(?m)@tool\b[^\n]*\n\s*(?:async\s+)?def\s+(\w+)\s*\(`)
	lcToolClassRe     = regexp.MustCompile(`(?m)^class\s+([A-Z][A-Za-z0-9_]*)\s*\([^)]*BaseTool[^)]*\)\s*:`)
	lcStructToolRe    = regexp.MustCompile(`(?m)([a-zA-Z_]\w*)\s*=\s*StructuredTool\.from_function\s*\(`)
	lcToolConsRe      = regexp.MustCompile(`(?m)([a-zA-Z_]\w*)\s*=\s*Tool\s*\(\s*(?:name\s*=\s*)?["']([^"']+)["']`)

	lcRAGClassNames = []string{"RetrievalQA", "RetrievalQAWithSourcesChain", "ConversationalRetrievalChain"}

	lcRetrieverRe    = regexp.MustCompile(`(?m)([a-zA-Z_]\w*)\s*=\s*(?:(\w+)\.as_retriever\s*\(|VectorStoreRetriever\s*\()`)
	lcRAGFactoryRe   = regexp.MustCompile(`(?m)([a-zA-Z_]\w*)\s*=\s*(create_(?:stuff_documents|retrieval|map_reduce_documents)_chain)\s*\(`)
	lcChatPromptRe   = regexp.MustCompile(`(?m)([a-zA-Z_]\w*)\s*=\s*ChatPromptTemplate\.from_(?:messages|template)\s*\(`)
	lcPromptTmplRe   = regexp.MustCompile(`(?m)([a-zA-Z_]\w*)\s*=\s*PromptTemplate(?:\.from_template)?\s*\(`)
	lcFewShotRe      = regexp.MustCompile(`(?m)([a-zA-Z_]\w*)\s*=\s*FewShotPromptTemplate\s*\(`)
	lcRunnableHistRe = regexp.MustCompile(`(?m)([a-zA-Z_]\w*)\s*=\s*RunnableWithMessageHistory\s*\(`)

	lcMemoryClassNames = []string{
		"ConversationBufferMemory", "ConversationBufferWindowMemory",
		"ConversationSummaryMemory", "ConversationSummaryBufferMemory",
		"ConversationTokenBufferMemory", "ConversationEntityMemory",
		"VectorStoreRetrieverMemory",
	}
)

func buildLegacyChainRe() *regexp.Regexp {
	escaped := make([]string, len(lcLegacyChainNames))
	for i, n := range lcLegacyChainNames {
		escaped[i] = regexp.QuoteMeta(n)
	}
	return regexp.MustCompile(`(?m)([a-zA-Z_]\w*)\s*=\s*(` + strings.Join(escaped, "|") + `)\s*(?:\.from_\w+)?\s*\(`)
}

func buildRAGInstanceRe() *regexp.Regexp {
	escaped := make([]string, len(lcRAGClassNames))
	for i, n := range lcRAGClassNames {
		escaped[i] = regexp.QuoteMeta(n)
	}
	return regexp.MustCompile(`(?m)([a-zA-Z_]\w*)\s*=\s*(` + strings.Join(escaped, "|") + `)(?:\.from_chain_type|\.from_llm)?\s*\(`)
}

func buildMemoryInstanceRe() *regexp.Regexp {
	escaped := make([]string, len(lcMemoryClassNames))
	for i, n := range lcMemoryClassNames {
		escaped[i] = regexp.QuoteMeta(n)
	}
	return regexp.MustCompile(`(?m)([a-zA-Z_]\w*)\s*=\s*(` + strings.Join(escaped, "|") + `)\s*\(`)
}

var (
	lcLegacyChainRe = buildLegacyChainRe()
	lcRAGInstanceRe = buildRAGInstanceRe()
	lcMemoryInstRe  = buildMemoryInstanceRe()
)

func (e *LangChainExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("custom.python_langchain")
	_, span := tracer.Start(ctx, "custom.python_langchain")
	defer span.End()
	span.SetAttributes(attribute.String("file", file.Path))

	if len(file.Content) == 0 {
		return nil, nil
	}

	source := string(file.Content)
	var out []types.EntityRecord

	// 1. LCEL pipe chains
	for _, idx := range allMatchesIndex(lcLCELChainRe, source) {
		varName := source[idx[2]:idx[3]]
		pipeExpr := strings.TrimSpace(source[idx[4]:idx[5]])
		if strings.HasPrefix(pipeExpr, "#") || strings.HasPrefix(pipeExpr, "'") || strings.HasPrefix(pipeExpr, `"`) {
			continue
		}
		stages := lcPipeStageRe.Split(pipeExpr, -1)
		var trimmed []string
		for _, s := range stages {
			s = strings.TrimSpace(s)
			if s != "" {
				trimmed = append(trimmed, s)
			}
		}
		line := lineOf(source, idx[0])
		out = append(out, entity(varName, "SCOPE.Operation", "", file.Path, line,
			map[string]string{"framework": "langchain", "pattern_type": "lcel_chain", "stages": strings.Join(trimmed, "|"), "stage_count": intToStr(len(trimmed))}))
	}

	// 2. Legacy chains
	for _, idx := range allMatchesIndex(lcLegacyChainRe, source) {
		varName := source[idx[2]:idx[3]]
		chainClass := source[idx[4]:idx[5]]
		line := lineOf(source, idx[0])
		out = append(out, entity(varName, "SCOPE.Operation", "", file.Path, line,
			map[string]string{"framework": "langchain", "pattern_type": "legacy_chain", "chain_class": chainClass}))
	}

	// 3. Agents
	for _, idx := range allMatchesIndex(lcAgentFactoryRe, source) {
		varName := source[idx[2]:idx[3]]
		factoryFn := source[idx[4]:idx[5]]
		line := lineOf(source, idx[0])
		out = append(out, entity(varName, "SCOPE.Service", "", file.Path, line,
			map[string]string{"framework": "langchain", "pattern_type": "agent", "factory": factoryFn}))
	}
	for _, idx := range allMatchesIndex(lcAgentExecutorRe, source) {
		varName := source[idx[2]:idx[3]]
		line := lineOf(source, idx[0])
		out = append(out, entity(varName, "SCOPE.Service", "", file.Path, line,
			map[string]string{"framework": "langchain", "pattern_type": "agent_executor", "kind": "AgentExecutor"}))
	}

	// 4. Tools
	for _, idx := range allMatchesIndex(lcToolDecoratorRe, source) {
		fnName := source[idx[2]:idx[3]]
		line := lineOf(source, idx[0])
		out = append(out, entity(fnName, "SCOPE.Operation", "function", file.Path, line,
			map[string]string{"framework": "langchain", "pattern_type": "tool", "kind": "decorator"}))
	}
	for _, idx := range allMatchesIndex(lcToolClassRe, source) {
		className := source[idx[2]:idx[3]]
		line := lineOf(source, idx[0])
		out = append(out, entity(className, "SCOPE.Operation", "function", file.Path, line,
			map[string]string{"framework": "langchain", "pattern_type": "tool", "kind": "class"}))
	}
	for _, idx := range allMatchesIndex(lcStructToolRe, source) {
		varName := source[idx[2]:idx[3]]
		line := lineOf(source, idx[0])
		out = append(out, entity(varName, "SCOPE.Operation", "function", file.Path, line,
			map[string]string{"framework": "langchain", "pattern_type": "tool", "kind": "structured_tool"}))
	}
	for _, idx := range allMatchesIndex(lcToolConsRe, source) {
		varName := source[idx[2]:idx[3]]
		toolName := source[idx[4]:idx[5]]
		line := lineOf(source, idx[0])
		out = append(out, entity(varName, "SCOPE.Operation", "function", file.Path, line,
			map[string]string{"framework": "langchain", "pattern_type": "tool", "kind": "constructor", "tool_name": toolName}))
	}

	// 5. RAG
	for _, idx := range allMatchesIndex(lcRAGInstanceRe, source) {
		varName := source[idx[2]:idx[3]]
		ragClass := source[idx[4]:idx[5]]
		line := lineOf(source, idx[0])
		out = append(out, entity(varName, "SCOPE.Pattern", "", file.Path, line,
			map[string]string{"framework": "langchain", "pattern_type": "rag", "rag_class": ragClass}))
	}
	for _, idx := range allMatchesIndex(lcRetrieverRe, source) {
		varName := source[idx[2]:idx[3]]
		line := lineOf(source, idx[0])
		props := map[string]string{"framework": "langchain", "pattern_type": "retriever", "kind": "retriever"}
		if idx[4] != -1 {
			props["vector_store"] = source[idx[4]:idx[5]]
		}
		out = append(out, entity(varName, "SCOPE.Pattern", "", file.Path, line, props))
	}
	for _, idx := range allMatchesIndex(lcRAGFactoryRe, source) {
		varName := source[idx[2]:idx[3]]
		factoryFn := source[idx[4]:idx[5]]
		line := lineOf(source, idx[0])
		out = append(out, entity(varName, "SCOPE.Pattern", "", file.Path, line,
			map[string]string{"framework": "langchain", "pattern_type": "rag_factory", "factory": factoryFn}))
	}

	// 6. Prompts
	for _, idx := range allMatchesIndex(lcChatPromptRe, source) {
		varName := source[idx[2]:idx[3]]
		line := lineOf(source, idx[0])
		out = append(out, entity(varName, "SCOPE.Component", "", file.Path, line,
			map[string]string{"framework": "langchain", "pattern_type": "prompt", "kind": "ChatPromptTemplate"}))
	}
	for _, idx := range allMatchesIndex(lcPromptTmplRe, source) {
		varName := source[idx[2]:idx[3]]
		line := lineOf(source, idx[0])
		out = append(out, entity(varName, "SCOPE.Component", "", file.Path, line,
			map[string]string{"framework": "langchain", "pattern_type": "prompt", "kind": "PromptTemplate"}))
	}
	for _, idx := range allMatchesIndex(lcFewShotRe, source) {
		varName := source[idx[2]:idx[3]]
		line := lineOf(source, idx[0])
		out = append(out, entity(varName, "SCOPE.Component", "", file.Path, line,
			map[string]string{"framework": "langchain", "pattern_type": "prompt", "kind": "FewShotPromptTemplate"}))
	}

	// 7. Memory
	for _, idx := range allMatchesIndex(lcMemoryInstRe, source) {
		varName := source[idx[2]:idx[3]]
		memClass := source[idx[4]:idx[5]]
		line := lineOf(source, idx[0])
		out = append(out, entity(varName, "SCOPE.Component", "", file.Path, line,
			map[string]string{"framework": "langchain", "pattern_type": "memory", "memory_class": memClass}))
	}
	for _, idx := range allMatchesIndex(lcRunnableHistRe, source) {
		varName := source[idx[2]:idx[3]]
		line := lineOf(source, idx[0])
		out = append(out, entity(varName, "SCOPE.Component", "", file.Path, line,
			map[string]string{"framework": "langchain", "pattern_type": "memory", "memory_class": "RunnableWithMessageHistory"}))
	}

	span.SetAttributes(attribute.Int("entity_count", len(out)))
	return out, nil
}

func intToStr(n int) string {
	return strconv.Itoa(n)
}
