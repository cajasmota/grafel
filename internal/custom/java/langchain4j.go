package java

import "regexp"

// LangChain4J custom extractor: AI services, tools, prompts, RAG, memory.
// Ported from: langchain4j_extractor.py

var langchain4jFrameworks = map[string]bool{"langchain4j": true}

var (
	lc4jAIServiceRE = regexp.MustCompile(
		`(?s)@AiService\b[^{]*?(?:public\s+)?interface\s+(\w+)`)
	lc4jToolMethodRE = regexp.MustCompile(
		`(?s)@Tool\b\s*(?:\(\s*"[^"]*"\s*\)\s*|\(\s*\)\s*|\s*)` +
			`(?:(?:public|protected|private)\s+)?(?:static\s+)?(?:<[^>]*>\s*)?` +
			`(\w+(?:\s*<[^>]*>)?)\s+(\w+)\s*\(`)
	lc4jSystemMessageRE = regexp.MustCompile(
		`(?s)@SystemMessage\b\s*(?:\(\s*(?:\"([^\"]*)\"[^)]*)\s*\))?` +
			`(?:\s*@\w+\s*(?:\([^)]*\)\s*)?)*\s*(?:(?:public|protected|private)\s+)?` +
			`(?:<[^>]*>\s*)?(\w+(?:\s*<[^>]*>)?)\s+(\w+)\s*\(`)
	lc4jUserMessageRE = regexp.MustCompile(
		`(?s)@UserMessage\b\s*(?:\(\s*(?:\"([^\"]*)\"[^)]*)\s*\))?` +
			`(?:\s*@\w+\s*(?:\([^)]*\)\s*)?)*\s*(?:(?:public|protected|private)\s+)?` +
			`(?:<[^>]*>\s*)?(\w+(?:\s*<[^>]*>)?)\s+(\w+)\s*\(`)
	lc4jChatModelFieldRE = regexp.MustCompile(
		`(?m)(?:private|protected|public|)\s+(?:final\s+)?` +
			`(ChatLanguageModel|StreamingChatLanguageModel)(?:\s*<[^>]*>)?\s+(\w+)\s*[;=]`)
	lc4jRAGComponentRE = regexp.MustCompile(
		`(?m)(?:private|protected|public|)\s+(?:final\s+)?` +
			`(EmbeddingStoreContentRetriever|EmbeddingStoreIngestor|EmbeddingStore|ContentRetriever)` +
			`(?:\s*<[^>]*>)?\s+(\w+)\s*[;=]`)
	lc4jChatMemoryRE = regexp.MustCompile(
		`(?m)(?:private|protected|public|)\s+(?:final\s+)?` +
			`(ChatMemory|ChatMemoryProvider|MessageWindowChatMemory|TokenWindowChatMemory)` +
			`(?:\s*<[^>]*>)?\s+(\w+)\s*[;=]`)
)

// ExtractLangChain4J runs the LangChain4J extractor.
func ExtractLangChain4J(ctx PatternContext) PatternResult {
	var result PatternResult
	if ctx.Language != "java" || !langchain4jFrameworks[ctx.Framework] {
		return result
	}

	source := ctx.Source
	fp := ctx.FilePath
	seenRefs := make(map[string]bool)
	seenRels := make(map[relKey]bool)

	// AI Services
	type svcInfo struct {
		name   string
		ref    string
		offset int
	}
	var services []svcInfo
	for _, m := range lc4jAIServiceRE.FindAllStringSubmatchIndex(source, -1) {
		name := source[m[2]:m[3]]
		ref := "scope:service:langchain4j_ai_service:" + fp + ":" + name
		services = append(services, svcInfo{name, ref, m[0]})
		addEntity(&result, seenRefs, SecondaryEntity{
			Name: name, Kind: "SCOPE.Service", SourceFile: fp,
			LineStart: lineOf(source, m[0]), LineEnd: lineOf(source, m[0]),
			Provenance: "INFERRED_FROM_LANGCHAIN4J_AI_SERVICE", Ref: ref,
			Properties: map[string]any{"framework": "langchain4j"},
		})
	}

	findOwningService := func(offset int) (string, string) {
		var name, ref string
		for _, s := range services {
			if s.offset <= offset {
				name = s.name
				ref = s.ref
			}
		}
		return name, ref
	}

	// @Tool methods
	for _, m := range lc4jToolMethodRE.FindAllStringSubmatchIndex(source, -1) {
		methodName := source[m[4]:m[5]]
		svcName, svcRef := findOwningService(m[0])
		if svcName == "" {
			svcName = findEnclosingClass(source, m[0])
			if svcName == "" {
				svcName = "Unknown"
			}
			svcRef = "scope:service:langchain4j_ai_service:" + fp + ":" + svcName
		}
		toolRef := "scope:operation:langchain4j_tool:" + fp + ":" + svcName + "." + methodName
		if addEntity(&result, seenRefs, SecondaryEntity{
			Name: svcName + "." + methodName, Kind: "SCOPE.Operation",
			Subtype: "function", SourceFile: fp,
			LineStart: lineOf(source, m[0]), LineEnd: lineOf(source, m[0]),
			Provenance: "INFERRED_FROM_LANGCHAIN4J_TOOL", Ref: toolRef,
			Properties: map[string]any{
				"tool_method": methodName, "owner_class": svcName,
				"framework": "langchain4j",
			},
		}) {
			addRel(&result, seenRels, Relationship{
				SourceRef: svcRef, TargetRef: toolRef, RelationshipType: "OWNS",
			})
		}
	}

	// @SystemMessage
	for _, m := range lc4jSystemMessageRE.FindAllStringSubmatchIndex(source, -1) {
		methodName := source[m[6]:m[7]]
		svcName, _ := findOwningService(m[0])
		if svcName == "" {
			svcName = "Unknown"
		}
		ref := "scope:pattern:langchain4j_prompt:" + fp + ":" + svcName + "." + methodName + ".system"
		addEntity(&result, seenRefs, SecondaryEntity{
			Name: svcName + "." + methodName + ".system_message",
			Kind: "SCOPE.Pattern", SourceFile: fp,
			LineStart: lineOf(source, m[0]), LineEnd: lineOf(source, m[0]),
			Provenance: "INFERRED_FROM_LANGCHAIN4J_PROMPT", Ref: ref,
			Properties: map[string]any{
				"prompt_type": "system_message", "method": methodName,
				"framework": "langchain4j",
			},
		})
	}

	// @UserMessage
	for _, m := range lc4jUserMessageRE.FindAllStringSubmatchIndex(source, -1) {
		methodName := source[m[6]:m[7]]
		svcName, _ := findOwningService(m[0])
		if svcName == "" {
			svcName = "Unknown"
		}
		ref := "scope:pattern:langchain4j_prompt:" + fp + ":" + svcName + "." + methodName + ".user"
		addEntity(&result, seenRefs, SecondaryEntity{
			Name: svcName + "." + methodName + ".user_message",
			Kind: "SCOPE.Pattern", SourceFile: fp,
			LineStart: lineOf(source, m[0]), LineEnd: lineOf(source, m[0]),
			Provenance: "INFERRED_FROM_LANGCHAIN4J_PROMPT", Ref: ref,
			Properties: map[string]any{
				"prompt_type": "user_message", "method": methodName,
				"framework": "langchain4j",
			},
		})
	}

	// ChatLanguageModel fields
	for _, m := range lc4jChatModelFieldRE.FindAllStringSubmatchIndex(source, -1) {
		modelType := source[m[2]:m[3]]
		fieldName := source[m[4]:m[5]]
		ref := "scope:component:langchain4j_model:" + fp + ":" + fieldName
		addEntity(&result, seenRefs, SecondaryEntity{
			Name: fieldName, Kind: "SCOPE.Component", SourceFile: fp,
			LineStart: lineOf(source, m[0]), LineEnd: lineOf(source, m[0]),
			Provenance: "INFERRED_FROM_LANGCHAIN4J_MODEL", Ref: ref,
			Properties: map[string]any{"model_type": modelType, "framework": "langchain4j"},
		})
	}

	// RAG components
	for _, m := range lc4jRAGComponentRE.FindAllStringSubmatchIndex(source, -1) {
		ragType := source[m[2]:m[3]]
		fieldName := source[m[4]:m[5]]
		ref := "scope:pattern:langchain4j_rag:" + fp + ":" + fieldName
		addEntity(&result, seenRefs, SecondaryEntity{
			Name: fieldName, Kind: "SCOPE.Pattern", SourceFile: fp,
			LineStart: lineOf(source, m[0]), LineEnd: lineOf(source, m[0]),
			Provenance: "INFERRED_FROM_LANGCHAIN4J_RAG", Ref: ref,
			Properties: map[string]any{"rag_type": ragType, "framework": "langchain4j"},
		})
	}

	// ChatMemory
	for _, m := range lc4jChatMemoryRE.FindAllStringSubmatchIndex(source, -1) {
		memType := source[m[2]:m[3]]
		fieldName := source[m[4]:m[5]]
		ref := "scope:component:langchain4j_memory:" + fp + ":" + fieldName
		addEntity(&result, seenRefs, SecondaryEntity{
			Name: fieldName, Kind: "SCOPE.Component", SourceFile: fp,
			LineStart: lineOf(source, m[0]), LineEnd: lineOf(source, m[0]),
			Provenance: "INFERRED_FROM_LANGCHAIN4J_MEMORY", Ref: ref,
			Properties: map[string]any{"memory_type": memType, "framework": "langchain4j"},
		})
	}

	_ = seenRels
	return result
}
