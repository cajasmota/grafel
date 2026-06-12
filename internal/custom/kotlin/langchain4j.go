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

	// #5012: runtime chain_composition wiring. Capture an
	// `AiServices.builder(IFace::class.java)` ... `.build()` (or
	// `.create(...)`) assembly assigned to a `val/var <svc> = ...`, so the
	// whole call chain — including newline-separated fluent calls — is one
	// match group we can scan for individual wiring methods.
	reLc4jKotlinServiceWiring = regexp.MustCompile(
		`(?s)(?:(?:private|protected|internal|public)\s+)?(?:val|var)\s+(\w+)\s*(?::[^=]+)?=\s*` +
			`AiServices\s*\.\s*(?:builder|create)\s*\((?:[^)]*)\)((?:\s*\.\s*\w+\s*\([^)]*\))*)`,
	)
	// Individual fluent builder steps inside the captured chain. The arg may be
	// an identifier reference (field/var we wired earlier), a class literal, or
	// an inline expression; we capture the leading identifier when present.
	reLc4jKotlinWireStep = regexp.MustCompile(
		`\.\s*(chatLanguageModel|streamingChatLanguageModel|tools|chatMemory|chatMemoryProvider|contentRetriever|retriever|retrievalAugmentor|systemMessageProvider|moderationModel|toolProvider)\s*\(\s*([A-Za-z_]\w*)?`,
	)
)

// lc4jWireKindToTarget maps a builder method to the wiring edge property that
// classifies the composed component. Kept here so the edge Properties carry a
// stable, queryable wire role parallel to the Java structural classification.
var lc4jWireRole = map[string]string{
	"chatLanguageModel":          "chat_model",
	"streamingChatLanguageModel": "streaming_chat_model",
	"tools":                      "tools",
	"toolProvider":               "tool_provider",
	"chatMemory":                 "chat_memory",
	"chatMemoryProvider":         "chat_memory_provider",
	"contentRetriever":           "content_retriever",
	"retriever":                  "content_retriever",
	"retrievalAugmentor":         "retrieval_augmentor",
	"systemMessageProvider":      "system_message_provider",
	"moderationModel":            "moderation_model",
}

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

	// 8. #5012: runtime AiServices builder chain_composition wiring.
	// `val assistant = AiServices.builder(Assistant::class.java)
	//      .chatLanguageModel(model).tools(tools).chatMemory(memory).build()`
	// emits a SCOPE.Service entity for the assembled service carrying USES
	// edges to each wired component (by referenced identifier name), giving the
	// runtime composition graph parity with Java langchain4j chain_composition.
	for _, m := range reLc4jKotlinServiceWiring.FindAllStringSubmatchIndex(src, -1) {
		svcName := src[m[2]:m[3]]
		chain := src[m[4]:m[5]]

		svc := makeEntity(svcName, "SCOPE.Service", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&svc, "framework", "langchain4j",
			"provenance", "INFERRED_FROM_LANGCHAIN4J_AI_SERVICES_BUILDER",
			"assembly", "AiServices.builder")

		var rels []types.RelationshipRecord
		wired := make(map[string]bool)
		for _, sm := range reLc4jKotlinWireStep.FindAllStringSubmatchIndex(chain, -1) {
			method := chain[sm[2]:sm[3]]
			role := lc4jWireRole[method]
			if role == "" {
				continue
			}
			setProps(&svc, "wire."+role, "true")

			// Capture the referenced component identifier when the argument is a
			// plain reference (model, tools, memory). Inline expressions /
			// class-literal-only args still record the wire role above but emit
			// no resolvable USES edge target.
			if sm[4] < 0 {
				continue
			}
			target := chain[sm[4]:sm[5]]
			edgeKey := role + "|" + target
			if wired[edgeKey] {
				continue
			}
			wired[edgeKey] = true
			rels = append(rels, types.RelationshipRecord{
				ToID: target,
				Kind: "USES",
				Properties: map[string]string{
					"framework": "langchain4j",
					"wire_role": role,
					"method":    method,
					"service":   svcName,
				},
				Confidence: regexConf,
			})
		}
		if len(rels) > 0 {
			svc.Relationships = append(svc.Relationships, rels...)
		}
		// Service entities keyed by Kind+Name; if a same-named @AiService
		// interface was already added, the wiring assembly is a distinct var so
		// dedupe on the (kind,name) key only collides when identical — acceptable.
		add(svc)
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}
