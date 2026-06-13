package java

import (
	"regexp"
	"strings"
)

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

	// #5155 (port of kotlin #5012/#5083): runtime chain_composition wiring.
	// Capture an `AiServices.builder(IFace.class)` ... `.build()` assembly
	// assigned to a `<Type> <svc> = ...` (or `var <svc> = ...`) local/field, so
	// the whole fluent call chain — including newline-separated calls — is one
	// match group we can scan for individual wiring methods. The Java interface
	// literal is `IFace.class` (vs kotlin `IFace::class.java`). Group 1 = the
	// assembled service binding name, group 2 = the captured fluent call chain.
	lc4jServiceWiringRE = regexp.MustCompile(
		`(?s)(?:(?:final|private|protected|public|static)\s+)*` +
			`(?:[\w.<>,\[\]?\s]+?\s+|var\s+)(\w+)\s*=\s*` +
			`AiServices\s*\.\s*builder\s*\((?:[^)]*)\)((?:\s*\.\s*\w+\s*\((?:[^()]|\([^()]*\))*\))*)`)
	// #5155: `AiServices.create(IFace.class, model, tools)` positional overload.
	// Group 1 = the binding name, group 2 = the raw argument list between the
	// create(...) parens (tolerating one level of nested parens so
	// `create(IFace.class, OpenAiChatModel.builder().build())` is captured whole).
	lc4jServiceCreateRE = regexp.MustCompile(
		`(?s)(?:(?:final|private|protected|public|static)\s+)*` +
			`(?:[\w.<>,\[\]?\s]+?\s+|var\s+)(\w+)\s*=\s*` +
			`AiServices\s*\.\s*create\s*\(((?:[^()]|\([^()]*\))*)\)`)
	// Individual fluent builder steps inside the captured chain. The whole
	// argument expression is captured in group 2 (tolerating one nested paren
	// level) so both bare identifiers and inline constructor/builder expressions
	// are recoverable; lc4jClassifyWireArg decides how to resolve it.
	lc4jWireStepRE = regexp.MustCompile(
		`\.\s*(chatLanguageModel|streamingChatLanguageModel|tools|chatMemory|chatMemoryProvider|contentRetriever|retriever|retrievalAugmentor|systemMessageProvider|moderationModel|toolProvider)\s*\(\s*((?:[^()]|\([^()]*\))*?)\s*\)`)
	// A bare identifier reference (whole arg is just `model`).
	lc4jBareIdentRE = regexp.MustCompile(`^[A-Za-z_]\w*$`)
	// An inline constructed component. Matches both the builder form
	// `OpenAiChatModel.builder()...build()` and the direct-constructor form
	// `new MyTools()` / `MyTools()`. Group 1 = the constructed/owning type name.
	lc4jInlineCtorRE = regexp.MustCompile(`^(?:new\s+)?([A-Z]\w*)\s*(?:\.\s*\w+\s*\(|\()`)
)

// lc4jWireRole maps a builder method to the wiring edge property that classifies
// the composed component, parallel to the kotlin extractor's lc4jWireRole table.
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

	// #5155: runtime AiServices builder chain_composition wiring.
	// `Assistant assistant = AiServices.builder(Assistant.class)
	//      .chatLanguageModel(model).tools(tools).chatMemory(memory).build();`
	// emits a SCOPE.Service entity for the assembled service carrying USES edges
	// to each wired component (by referenced identifier name OR materialized
	// inline component), giving the runtime composition graph parity with the
	// kotlin langchain4j chain_composition extractor.
	for _, m := range lc4jServiceWiringRE.FindAllStringSubmatchIndex(source, -1) {
		svcName := source[m[2]:m[3]]
		chain := source[m[4]:m[5]]
		ref := "scope:service:langchain4j_ai_services_builder:" + fp + ":" + svcName

		svc := SecondaryEntity{
			Name: svcName, Kind: "SCOPE.Service", SourceFile: fp,
			LineStart: lineOf(source, m[0]), LineEnd: lineOf(source, m[0]),
			Provenance: "INFERRED_FROM_LANGCHAIN4J_AI_SERVICES_BUILDER", Ref: ref,
			Properties: map[string]any{
				"framework": "langchain4j",
				"assembly":  "AiServices.builder",
			},
		}
		if !addEntity(&result, seenRefs, svc) {
			continue
		}

		wired := make(map[string]bool)
		for _, sm := range lc4jWireStepRE.FindAllStringSubmatchIndex(chain, -1) {
			method := chain[sm[2]:sm[3]]
			role := lc4jWireRole[method]
			if role == "" {
				continue
			}
			lc4jSetWireFlag(&result.Entities, ref, role)
			arg := ""
			if sm[4] >= 0 {
				arg = chain[sm[4]:sm[5]]
			}
			lc4jAddWireEdge(&result, seenRefs, seenRels, ref, svcName, role, method, arg, fp,
				lineOf(source, m[0]), wired)
		}
	}

	// #5155: `AiServices.create(IFace.class, model, tools)` positional overload.
	// langchain4j's `create` shorthand binds arg0 -> AI-service interface,
	// arg1 -> chat model, arg2 -> tools object. We trace the same wire roles as
	// the fluent builder so both assembly styles yield a comparable graph.
	posRoles := []string{"", "chat_model", "tools"}
	posMethods := []string{"", "chatLanguageModel", "tools"}
	for _, m := range lc4jServiceCreateRE.FindAllStringSubmatchIndex(source, -1) {
		svcName := source[m[2]:m[3]]
		argList := source[m[4]:m[5]]
		ref := "scope:service:langchain4j_ai_services_create:" + fp + ":" + svcName

		svc := SecondaryEntity{
			Name: svcName, Kind: "SCOPE.Service", SourceFile: fp,
			LineStart: lineOf(source, m[0]), LineEnd: lineOf(source, m[0]),
			Provenance: "INFERRED_FROM_LANGCHAIN4J_AI_SERVICES_CREATE", Ref: ref,
			Properties: map[string]any{
				"framework": "langchain4j",
				"assembly":  "AiServices.create",
			},
		}
		if !addEntity(&result, seenRefs, svc) {
			continue
		}

		args := lc4jSplitTopLevelArgs(argList)
		wired := make(map[string]bool)
		for i, a := range args {
			if i == 0 || i >= len(posRoles) || posRoles[i] == "" {
				continue // arg0 is the interface .class literal
			}
			lc4jSetWireFlag(&result.Entities, ref, posRoles[i])
			lc4jAddWireEdge(&result, seenRefs, seenRels, ref, svcName, posRoles[i], posMethods[i], a, fp,
				lineOf(source, m[0]), wired)
		}
	}

	return result
}

// lc4jSetWireFlag records a `wire.<role>=true` property on the assembled service
// entity identified by ref, so a query can see which slots were wired even when
// the argument itself does not resolve to a USES target.
func lc4jSetWireFlag(entities *[]SecondaryEntity, ref, role string) {
	for i := range *entities {
		if (*entities)[i].Ref == ref {
			if (*entities)[i].Properties == nil {
				(*entities)[i].Properties = map[string]any{}
			}
			(*entities)[i].Properties["wire."+role] = "true"
			return
		}
	}
}

// lc4jAddWireEdge resolves one langchain4j wiring argument (#5155) and records
// the corresponding USES edge from the assembled service to the wired component.
// The argument may be:
//   - a bare identifier ("model"): the edge points at the referenced
//     field/var by name (TargetRef resolves to an emitted component when one
//     exists, else a synthetic dependency ref);
//   - an inline-constructed component ("OpenAiChatModel.builder().build()",
//     "new MyTools()", "MyTools()"): a synthetic SCOPE.Component entity is
//     materialized for the constructed type so the wiring has a resolvable
//     target, and the USES edge points at that type name;
//   - empty / unrecognized: only the `wire.<role>` flag (already set) — no edge.
//
// `arg_kind` on the edge ("identifier" | "inline_component") records how the
// target was resolved.
func lc4jAddWireEdge(result *PatternResult, seenRefs map[string]bool, seenRels map[relKey]bool,
	svcRef, svcName, role, method, arg, fp string, line int, wired map[string]bool) {

	target, argKind := lc4jClassifyWireArg(arg)
	if target == "" {
		return
	}
	edgeKey := role + "|" + target
	if wired[edgeKey] {
		return
	}
	wired[edgeKey] = true

	var targetRef string
	if argKind == "inline_component" {
		// Materialize the inline-constructed component so the USES edge has a real
		// target. addEntity dedupes against an identically-named component if one
		// already exists.
		targetRef = "scope:component:langchain4j_inline_component:" + fp + ":" + target
		addEntity(result, seenRefs, SecondaryEntity{
			Name: target, Kind: "SCOPE.Component", SourceFile: fp,
			LineStart: line, LineEnd: line,
			Provenance: "INFERRED_FROM_LANGCHAIN4J_INLINE_COMPONENT", Ref: targetRef,
			Properties: map[string]any{
				"framework":   "langchain4j",
				"wire_role":   role,
				"constructed": "true",
			},
		})
	} else {
		// Bare identifier: bind to an existing emitted component/field by name
		// when present, else a synthetic cross-binding dependency ref.
		targetRef = findRefForType(target, fp, "langchain4j_wired", result)
	}

	addRel(result, seenRels, Relationship{
		SourceRef: svcRef, TargetRef: targetRef, RelationshipType: "USES",
		Properties: map[string]string{
			"framework": "langchain4j",
			"wire_role": role,
			"method":    method,
			"service":   svcName,
			"arg_kind":  argKind,
		},
	})
}

// lc4jClassifyWireArg inspects a wiring argument expression and returns the
// resolved USES-edge target plus how it was resolved (#5155):
//
//   - "model"                          -> ("model", "identifier")
//   - "OpenAiChatModel.builder()..."   -> ("OpenAiChatModel", "inline_component")
//   - "new MyTools()" / "MyTools(cfg)"  -> ("MyTools", "inline_component")
//   - anything else (literal, lambda)  -> ("", "")
func lc4jClassifyWireArg(arg string) (target, kind string) {
	arg = strings.TrimSpace(arg)
	if arg == "" {
		return "", ""
	}
	if lc4jBareIdentRE.MatchString(arg) {
		return arg, "identifier"
	}
	if m := lc4jInlineCtorRE.FindStringSubmatch(arg); m != nil {
		return m[1], "inline_component"
	}
	return "", ""
}

// lc4jSplitTopLevelArgs splits a comma-separated argument list at top-level
// commas only, so a nested call like `OpenAiChatModel.builder().build()` or a
// generic `List.of(a, b)` is not split apart. Tracks (), <>, [] and strings.
func lc4jSplitTopLevelArgs(s string) []string {
	var args []string
	depth := 0
	inStr := byte(0)
	start := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case inStr != 0:
			if c == '\\' {
				i++
			} else if c == inStr {
				inStr = 0
			}
		case c == '"' || c == '\'':
			inStr = c
		case c == '(' || c == '<' || c == '[':
			depth++
		case c == ')' || c == '>' || c == ']':
			if depth > 0 {
				depth--
			}
		case c == ',' && depth == 0:
			args = append(args, strings.TrimSpace(s[start:i]))
			start = i + 1
		}
	}
	if start < len(s) {
		if a := strings.TrimSpace(s[start:]); a != "" {
			args = append(args, a)
		}
	}
	return args
}
