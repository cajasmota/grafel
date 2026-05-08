package javascript

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	extreg "github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func init() {
	extreg.Register("custom_js_langchain", &langchainExtractor{})
}

type langchainExtractor struct{}

func (e *langchainExtractor) Language() string { return "custom_js_langchain" }

var (
	reLangchainRunnableSeq = regexp.MustCompile(
		`RunnableSequence\s*\.\s*from\s*\(\s*\[`,
	)
	reLangchainMultiPipe = regexp.MustCompile(
		`(\w+)\s*\.\s*pipe\s*\(`,
	)
	reLangchainPipeCall = regexp.MustCompile(
		`\.pipe\s*\(\s*(\w+)\s*\)`,
	)
	reLangchainAgentFactory = regexp.MustCompile(
		`create\w*Agent\s*\(`,
	)
	reLangchainAgentExecutor = regexp.MustCompile(
		`AgentExecutor\s*\.\s*(?:from\w*|create)\s*\(`,
	)
	reLangchainChain = regexp.MustCompile(
		`new\s+((?:\w+\.)*(?:LLMChain|ConversationChain|RetrievalQAChain|MapReduceDocumentsChain|StuffDocumentsChain|AnalyzeDocumentChain|SequentialChain|SimpleSequentialChain|TransformChain|RouterChain|MultiPromptChain|APIChain|SQLDatabaseChain|ConstitutionalChain))\s*\(`,
	)
	reLangchainLLM = regexp.MustCompile(
		`new\s+(ChatOpenAI|ChatAnthropic|ChatGoogleGenerativeAI|OpenAI|Anthropic|Ollama|HuggingFaceInference|ChatMistralAI|ChatGroq)\s*\(`,
	)
	reLangchainVectorStore = regexp.MustCompile(
		`(?:new\s+(\w*(?:VectorStore|Vectorstore|FAISS|Chroma|Pinecone|Weaviate|Qdrant|Milvus|Supabase)\w*)\s*\(|(\w*(?:VectorStore|FAISS|Chroma|Pinecone)\w*)\s*\.\s*(?:from\w*|load\w*)\s*\()`,
	)
	reLangchainTool = regexp.MustCompile(
		`new\s+(DynamicTool|DynamicStructuredTool|Tool|SerpAPI|Calculator|WebBrowser|WikipediaQueryRun|GoogleCustomSearch)\s*\(`,
	)
	reLangchainPrompt = regexp.MustCompile(
		`(?:ChatPromptTemplate|PromptTemplate|FewShotPromptTemplate|SystemMessagePromptTemplate|HumanMessagePromptTemplate)\s*\.\s*(?:from\w*)\s*\(`,
	)
)

func (e *langchainExtractor) Extract(ctx context.Context, file extreg.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/javascript")
	_, span := tracer.Start(ctx, "indexer.langchain_ts_extractor.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("framework", "langchain"),
			attribute.String("file_path", file.Path),
		),
	)
	defer span.End()

	if len(file.Content) == 0 {
		return nil, nil
	}
	src := string(file.Content)
	lang := strings.ToLower(file.Language)
	if lang != "typescript" && lang != "javascript" {
		return nil, nil
	}

	var entities []types.EntityRecord
	seen := make(map[string]bool)
	addEntity := func(ent types.EntityRecord) {
		key := fmt.Sprintf("%s:%s:%s", ent.Kind, ent.Name, ent.SourceFile)
		if seen[key] {
			return
		}
		seen[key] = true
		entities = append(entities, ent)
	}

	// RunnableSequence.from([...]) chains
	for _, m := range reLangchainRunnableSeq.FindAllStringIndex(src, -1) {
		ent := makeEntity("RunnableSequence", "SCOPE.Operation", "lcel_chain", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "langchain", "chain_type", "RunnableSequence",
			"provenance", "INFERRED_FROM_LANGCHAIN_RUNNABLE_SEQ")
		addEntity(ent)
	}

	// .pipe() chain stages
	pipeTargets := make(map[string]bool)
	for _, m := range reLangchainPipeCall.FindAllStringSubmatchIndex(src, -1) {
		target := src[m[2]:m[3]]
		pipeTargets[target] = true
		name := "pipe:" + target
		ent := makeEntity(name, "SCOPE.Operation", "pipe_stage", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "langchain", "target", target,
			"provenance", "INFERRED_FROM_LANGCHAIN_PIPE")
		addEntity(ent)
	}

	// Multi-pipe sources (var.pipe(...))
	for _, m := range reLangchainMultiPipe.FindAllStringSubmatchIndex(src, -1) {
		source := src[m[2]:m[3]]
		if pipeTargets[source] {
			continue
		}
		name := "chain_source:" + source
		ent := makeEntity(name, "SCOPE.Component", "chain_source", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "langchain", "source_var", source,
			"provenance", "INFERRED_FROM_LANGCHAIN_CHAIN_SOURCE")
		addEntity(ent)
	}

	// Agent factories
	for _, m := range reLangchainAgentFactory.FindAllStringIndex(src, -1) {
		segment := src[m[0]:m[1]]
		name := "agent:" + strings.TrimSuffix(segment, "(")
		ent := makeEntity(name, "SCOPE.Component", "agent", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "langchain", "provenance", "INFERRED_FROM_LANGCHAIN_AGENT")
		addEntity(ent)
	}

	// AgentExecutor
	for _, m := range reLangchainAgentExecutor.FindAllStringIndex(src, -1) {
		ent := makeEntity("AgentExecutor", "SCOPE.Component", "agent_executor", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "langchain", "provenance", "INFERRED_FROM_LANGCHAIN_AGENT_EXECUTOR")
		addEntity(ent)
	}

	// Chain instantiation
	for _, m := range reLangchainChain.FindAllStringSubmatchIndex(src, -1) {
		chainType := src[m[2]:m[3]]
		ent := makeEntity(chainType, "SCOPE.Component", "chain", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "langchain", "chain_type", chainType,
			"provenance", "INFERRED_FROM_LANGCHAIN_CHAIN")
		addEntity(ent)
	}

	// LLM instantiation
	for _, m := range reLangchainLLM.FindAllStringSubmatchIndex(src, -1) {
		llmType := src[m[2]:m[3]]
		ent := makeEntity(llmType, "SCOPE.Component", "llm", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "langchain", "llm_type", llmType,
			"provenance", "INFERRED_FROM_LANGCHAIN_LLM")
		addEntity(ent)
	}

	// Vector stores
	for _, m := range reLangchainVectorStore.FindAllStringSubmatchIndex(src, -1) {
		name := ""
		if m[2] >= 0 {
			name = src[m[2]:m[3]]
		} else if m[4] >= 0 {
			name = src[m[4]:m[5]]
		}
		if name == "" {
			continue
		}
		ent := makeEntity(name, "SCOPE.Component", "vector_store", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "langchain", "store_type", name,
			"provenance", "INFERRED_FROM_LANGCHAIN_VECTOR_STORE")
		addEntity(ent)
	}

	// Tools
	for _, m := range reLangchainTool.FindAllStringSubmatchIndex(src, -1) {
		toolType := src[m[2]:m[3]]
		ent := makeEntity(toolType, "SCOPE.Component", "tool", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "langchain", "tool_type", toolType,
			"provenance", "INFERRED_FROM_LANGCHAIN_TOOL")
		addEntity(ent)
	}

	// Prompt templates
	for _, m := range reLangchainPrompt.FindAllStringIndex(src, -1) {
		segment := src[m[0]:m[1]]
		templateType := strings.Split(segment, ".")[0]
		ent := makeEntity(templateType, "SCOPE.Component", "prompt_template", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "langchain", "template_type", templateType,
			"provenance", "INFERRED_FROM_LANGCHAIN_PROMPT")
		addEntity(ent)
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}
