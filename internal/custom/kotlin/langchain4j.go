package kotlin

import (
	"context"
	"regexp"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func init() {
	extractor.Register("custom_kotlin_langchain4j", &langchain4jKotlinExtractor{})
}

type langchain4jKotlinExtractor struct{}

func (e *langchain4jKotlinExtractor) Language() string { return "custom_kotlin_langchain4j" }

var (
	reLc4jKotlinAiService = regexp.MustCompile(
		`(?s)@AiService\b[^{]*?(?:public\s+|private\s+|internal\s+|protected\s+)?interface\s+(\w+)`,
	)
	reLc4jKotlinTool = regexp.MustCompile(
		`(?s)@Tool\b\s*(?:\(\s*"[^"]*"\s*\)\s*|\(\s*\)\s*|\s*)` +
			`(?:(?:public|private|internal|protected|override|suspend)\s+)*fun\s+(\w+)\s*\(`,
	)
	reLc4jKotlinSystemMsg = regexp.MustCompile(
		`(?s)@SystemMessage\b[^(]*(?:\([^)]*\))?[^f]*fun\s+(\w+)\s*\(`,
	)
	reLc4jKotlinUserMsg = regexp.MustCompile(
		`(?s)@UserMessage\b[^(]*(?:\([^)]*\))?[^f]*fun\s+(\w+)\s*\(`,
	)
	reLc4jKotlinChatModel = regexp.MustCompile(
		`(?m)(?:private\s+|protected\s+|internal\s+|public\s+)?(?:val|var)\s+(\w+)\s*:\s*(ChatLanguageModel|StreamingChatLanguageModel)`,
	)
	reLc4jKotlinMemory = regexp.MustCompile(
		`(?m)(?:private\s+|protected\s+|internal\s+|public\s+)?(?:val|var)\s+(\w+)\s*:\s*(ChatMemory|ChatMemoryProvider|MessageWindowChatMemory|TokenWindowChatMemory)`,
	)
	reLc4jKotlinRAG = regexp.MustCompile(
		`(?m)(?:private\s+|protected\s+|internal\s+|public\s+)?(?:val|var)\s+(\w+)\s*:\s*(EmbeddingStoreContentRetriever|EmbeddingStoreIngestor|EmbeddingStore|ContentRetriever)`,
	)
)

func (e *langchain4jKotlinExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/kotlin")
	_, span := tracer.Start(ctx, "indexer.langchain4j_kotlin_extractor.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("framework", "langchain4j"),
			attribute.String("file_path", file.Path),
		),
	)
	defer span.End()

	if len(file.Content) == 0 {
		return nil, nil
	}
	if file.Language != "kotlin" {
		return nil, nil
	}

	src := string(file.Content)
	var entities []types.EntityRecord
	seen := make(map[string]bool)

	// confidence_overlay (#4974, parity with Java #3093): every langchain4j
	// entity below is produced by regex pattern match over Kotlin source, so the
	// extractor stamps a top-level EntityRecord.Confidence directly rather than
	// relying solely on the framework-blind per-binding substrate overlay.
	regexConf := types.BaseConfidence(types.SourceRegexPattern)

	add := func(ent types.EntityRecord) {
		key := ent.Kind + ":" + ent.Name
		if seen[key] {
			return
		}
		if ent.Confidence == 0 {
			ent.Confidence = regexConf
		}
		seen[key] = true
		entities = append(entities, ent)
	}

	// 1. @AiService interfaces -> SCOPE.Service
	for _, m := range reLc4jKotlinAiService.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		ent := makeEntity(name, "SCOPE.Service", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "langchain4j", "provenance", "INFERRED_FROM_LANGCHAIN4J_AI_SERVICE")
		add(ent)
	}

	// 2. @Tool methods -> SCOPE.Operation/function
	for _, m := range reLc4jKotlinTool.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		ent := makeEntity(name, "SCOPE.Operation", "function", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "langchain4j", "provenance", "INFERRED_FROM_LANGCHAIN4J_TOOL",
			"tool_method", name)
		add(ent)
	}

	// 3. @SystemMessage -> SCOPE.Pattern
	for _, m := range reLc4jKotlinSystemMsg.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]] + ".system_message"
		ent := makeEntity(name, "SCOPE.Pattern", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "langchain4j", "provenance", "INFERRED_FROM_LANGCHAIN4J_PROMPT",
			"prompt_type", "system_message")
		add(ent)
	}

	// 4. @UserMessage -> SCOPE.Pattern
	for _, m := range reLc4jKotlinUserMsg.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]] + ".user_message"
		ent := makeEntity(name, "SCOPE.Pattern", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "langchain4j", "provenance", "INFERRED_FROM_LANGCHAIN4J_PROMPT",
			"prompt_type", "user_message")
		add(ent)
	}

	// 5. ChatLanguageModel fields -> SCOPE.Component
	for _, m := range reLc4jKotlinChatModel.FindAllStringSubmatchIndex(src, -1) {
		fieldName := src[m[2]:m[3]]
		modelType := src[m[4]:m[5]]
		ent := makeEntity(fieldName, "SCOPE.Component", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "langchain4j", "provenance", "INFERRED_FROM_LANGCHAIN4J_MODEL",
			"model_type", modelType)
		add(ent)
	}

	// 6. ChatMemory fields -> SCOPE.Component
	for _, m := range reLc4jKotlinMemory.FindAllStringSubmatchIndex(src, -1) {
		fieldName := src[m[2]:m[3]]
		memType := src[m[4]:m[5]]
		ent := makeEntity(fieldName, "SCOPE.Component", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "langchain4j", "provenance", "INFERRED_FROM_LANGCHAIN4J_MEMORY",
			"memory_type", memType)
		add(ent)
	}

	// 7. RAG components -> SCOPE.Pattern
	for _, m := range reLc4jKotlinRAG.FindAllStringSubmatchIndex(src, -1) {
		fieldName := src[m[2]:m[3]]
		ragType := src[m[4]:m[5]]
		ent := makeEntity(fieldName, "SCOPE.Pattern", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "langchain4j", "provenance", "INFERRED_FROM_LANGCHAIN4J_RAG",
			"rag_type", ragType)
		add(ent)
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}
