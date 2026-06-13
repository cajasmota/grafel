package kotlin

import (
	"context"
	"regexp"
	"strings"

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

	// #5013: prompt_template_extraction template-variable resolution.
	//
	// Capture the FULL annotated declaration so we can read both the template
	// string literal carried by the annotation and the parameter list of the
	// fun it decorates. Group 1 = template string body (between the quotes),
	// group 2 = fun name, group 3 = raw parameter list. Supports both the
	// single-arg form `@UserMessage("...{{x}}...")` and Kotlin triple-quoted
	// strings. The two annotations share one shape so a small table drives both.
	// The param list `((?:[^()]|\([^()]*\))*)` tolerates one level of nested
	// parens so `@V("name")` annotations inside the signature don't truncate it.
	reLc4jKotlinSystemMsgTpl = regexp.MustCompile(
		`(?s)@SystemMessage\s*\(\s*(?:value\s*=\s*)?(?:"""(.*?)"""|"((?:[^"\\]|\\.)*)")\s*\)` +
			`[^f]*?fun\s+(\w+)\s*\(((?:[^()]|\([^()]*\))*)\)`,
	)
	reLc4jKotlinUserMsgTpl = regexp.MustCompile(
		`(?s)@UserMessage\s*\(\s*(?:value\s*=\s*)?(?:"""(.*?)"""|"((?:[^"\\]|\\.)*)")\s*\)` +
			`[^f]*?fun\s+(\w+)\s*\(((?:[^()]|\([^()]*\))*)\)`,
	)

	// `PromptTemplate.from("...{{var}}...")` (optionally assigned to a
	// `val/var <name> = ...`). Group 1 = optional binding name, group 2/3 =
	// triple-quoted / double-quoted template body.
	reLc4jKotlinPromptTemplate = regexp.MustCompile(
		`(?s)(?:(?:private|protected|internal|public)\s+)?(?:(?:val|var)\s+(\w+)\s*(?::[^=]+)?=\s*)?` +
			`PromptTemplate\s*\.\s*from\s*\(\s*(?:"""(.*?)"""|"((?:[^"\\]|\\.)*)")`,
	)

	// #5103: resource-loaded prompt template. `PromptTemplate.from(...)` where
	// the argument is NOT a string literal but a resource loader call —
	// `loadResource("x.txt")`, `readResource(...)`, classpath/Files/Paths reads,
	// or a `*Resource(...)` helper. The template body lives in an external file
	// we don't read, so it is captured structurally with template_source=resource
	// and the referenced resource path (when a string literal argument is
	// present). Group 1 = optional binding name, group 2 = loader call expression,
	// group 3 = first string-literal argument inside the loader (the resource
	// path, when present).
	reLc4jKotlinPromptTemplateResource = regexp.MustCompile(
		`(?s)(?:(?:private|protected|internal|public)\s+)?(?:(?:val|var)\s+(\w+)\s*(?::[^=]+)?=\s*)?` +
			`PromptTemplate\s*\.\s*from\s*\(\s*` +
			`(\w*(?:Resource|resource|getResourceAsStream|readText|readAllBytes|readString)\w*\s*\((?:[^()]|\([^()]*\))*\))`,
	)
	// First string-literal argument inside a resource-loader call (the resource
	// path). Group 1 = the path.
	reLc4jKotlinResourceArg = regexp.MustCompile(`"((?:[^"\\]|\\.)*)"`)

	// Template placeholders. langchain4j uses `{{var}}` (mustache-style) by
	// default; the older `{var}` single-brace form is also accepted by
	// PromptTemplate. We match both and normalize to the inner identifier.
	//
	// #5103: a placeholder may carry a dotted nested-field path —
	// `{{user.name}}` / `{{order.items.size}}`. The leading identifier is the
	// template variable that resolves to a fun parameter; the remaining segments
	// are a field access path on that parameter object. Group 1 = leading
	// identifier (the binding key), group 2 = the optional `.field.subfield`
	// remainder (nested-field path, empty for a plain `{{var}}`).
	reLc4jKotlinTplVarDouble = regexp.MustCompile(`\{\{\s*([A-Za-z_]\w*)((?:\s*\.\s*[A-Za-z_]\w*)*)\s*\}\}`)
	reLc4jKotlinTplVarSingle = regexp.MustCompile(`\{\s*([A-Za-z_]\w*)((?:\s*\.\s*[A-Za-z_]\w*)*)\s*\}`)
	// Strips interior whitespace from a captured `.a . b` nested-field remainder.
	reLc4jKotlinWS = regexp.MustCompile(`\s+`)

	// A single Kotlin parameter, optionally carrying an `@V("name")` /
	// `@V(value = "name")` binding annotation that renames it for the template.
	// Group 1 = @V bound name (optional), group 2 = kotlin param identifier.
	reLc4jKotlinParam = regexp.MustCompile(
		`(?s)(?:@V\s*\(\s*(?:value\s*=\s*)?"((?:[^"\\]|\\.)*)"\s*\)\s*)?` +
			`(?:vararg\s+)?(\w+)\s*:`,
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
	// `AiServices.builder(IFace::class.java)` ... `.build()` assembly assigned
	// to a `val/var <svc> = ...`, so the whole call chain — including
	// newline-separated fluent calls — is one match group we can scan for
	// individual wiring methods. The `.create(...)` positional overload is
	// handled by a dedicated pass below (#5083) so its positional args are
	// resolved rather than discarded.
	reLc4jKotlinServiceWiring = regexp.MustCompile(
		`(?s)(?:(?:private|protected|internal|public)\s+)?(?:val|var)\s+(\w+)\s*(?::[^=]+)?=\s*` +
			`AiServices\s*\.\s*builder\s*\((?:[^)]*)\)((?:\s*\.\s*\w+\s*\((?:[^()]|\([^()]*\))*\))*)`,
	)
	// #5083: `AiServices.create(IFace::class.java, model, tools)` positional
	// overload. Group 1 = optional binding name, group 2 = the raw argument
	// list between the create(...) parens (tolerating one level of nested
	// parens so `create(IFace::class.java, OpenAiChatModel.builder().build())`
	// is captured whole).
	reLc4jKotlinServiceCreate = regexp.MustCompile(
		`(?s)(?:(?:private|protected|internal|public)\s+)?(?:val|var)\s+(\w+)\s*(?::[^=]+)?=\s*` +
			`AiServices\s*\.\s*create\s*\(((?:[^()]|\([^()]*\))*)\)`,
	)
	// Individual fluent builder steps inside the captured chain. The whole
	// argument expression is captured in group 2 (tolerating one nested paren
	// level) so both bare identifiers and inline constructor/builder
	// expressions are recoverable; classifyWireArg decides how to resolve it.
	reLc4jKotlinWireStep = regexp.MustCompile(
		`\.\s*(chatLanguageModel|streamingChatLanguageModel|tools|chatMemory|chatMemoryProvider|contentRetriever|retriever|retrievalAugmentor|systemMessageProvider|moderationModel|toolProvider)\s*\(\s*((?:[^()]|\([^()]*\))*?)\s*\)`,
	)

	// #5083: a bare identifier reference (whole arg is just `model`).
	reLc4jKotlinBareIdent = regexp.MustCompile(`^[A-Za-z_]\w*$`)
	// #5083: an inline constructed component. Matches both the builder form
	// `OpenAiChatModel.builder()...build()` and the direct-constructor form
	// `MyTools()` / `MyTools(arg)`. Group 1 = the constructed/owning type name.
	reLc4jKotlinInlineCtor = regexp.MustCompile(
		`^([A-Z]\w*)\s*(?:\.\s*\w+\s*\(|\()`,
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

	// 3. @SystemMessage -> SCOPE.Pattern (template-variable resolution, #5013).
	// Pass 3a captures annotations that carry an inline template string so we
	// can resolve `{{var}}` placeholders against the fun's @V-bound params.
	tplMatched := make(map[string]bool) // fun names already handled with a template
	for _, m := range reLc4jKotlinSystemMsgTpl.FindAllStringSubmatchIndex(src, -1) {
		tpl := groupOr(src, m, 1, 2) // triple-quoted (1) or double-quoted (2)
		funName := src[m[6]:m[7]]
		params := src[m[8]:m[9]]
		ent := makeEntity(funName+".system_message", "SCOPE.Pattern", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "langchain4j", "provenance", "INFERRED_FROM_LANGCHAIN4J_PROMPT",
			"prompt_type", "system_message")
		addTemplateVarsAndEdges(&ent, tpl, params, regexConf)
		tplMatched["system:"+funName] = true
		add(ent)
	}
	// 3b. @SystemMessage without a resolvable inline template (e.g. resource ref).
	for _, m := range reLc4jKotlinSystemMsg.FindAllStringSubmatchIndex(src, -1) {
		funName := src[m[2]:m[3]]
		if tplMatched["system:"+funName] {
			continue
		}
		ent := makeEntity(funName+".system_message", "SCOPE.Pattern", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "langchain4j", "provenance", "INFERRED_FROM_LANGCHAIN4J_PROMPT",
			"prompt_type", "system_message")
		add(ent)
	}

	// 4. @UserMessage -> SCOPE.Pattern (template-variable resolution, #5013).
	for _, m := range reLc4jKotlinUserMsgTpl.FindAllStringSubmatchIndex(src, -1) {
		tpl := groupOr(src, m, 2, 4)
		funName := src[m[6]:m[7]]
		params := src[m[8]:m[9]]
		ent := makeEntity(funName+".user_message", "SCOPE.Pattern", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "langchain4j", "provenance", "INFERRED_FROM_LANGCHAIN4J_PROMPT",
			"prompt_type", "user_message")
		addTemplateVarsAndEdges(&ent, tpl, params, regexConf)
		tplMatched["user:"+funName] = true
		add(ent)
	}
	for _, m := range reLc4jKotlinUserMsg.FindAllStringSubmatchIndex(src, -1) {
		funName := src[m[2]:m[3]]
		if tplMatched["user:"+funName] {
			continue
		}
		ent := makeEntity(funName+".user_message", "SCOPE.Pattern", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "langchain4j", "provenance", "INFERRED_FROM_LANGCHAIN4J_PROMPT",
			"prompt_type", "user_message")
		add(ent)
	}

	// 4c. PromptTemplate.from("...{{var}}...") -> SCOPE.Pattern (#5013).
	// Programmatic templates have no surrounding fun param list, so placeholders
	// are recorded as template variables but bind to no resolvable param.
	ptIdx := 0
	for _, m := range reLc4jKotlinPromptTemplate.FindAllStringSubmatchIndex(src, -1) {
		tpl := groupOr(src, m, 2, 3) // triple-quoted (2) or double-quoted (3)
		name := src[m[2]:m[3]]
		if name == "" {
			ptIdx++
			name = "promptTemplate" + itoa(ptIdx)
		}
		ent := makeEntity(name+".prompt_template", "SCOPE.Pattern", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "langchain4j", "provenance", "INFERRED_FROM_LANGCHAIN4J_PROMPT_TEMPLATE",
			"prompt_type", "prompt_template")
		addTemplateVarsAndEdges(&ent, tpl, "", regexConf)
		add(ent)
	}

	// 4d. #5103: resource-loaded PromptTemplate.from(loadResource("x.txt")).
	// The template body lives in an external classpath/file resource we don't
	// read, so the placeholders cannot be resolved here. We still emit the
	// SCOPE.Pattern structurally with template_source=resource and the resource
	// path (when the loader carries a string-literal argument) so the graph
	// records that a resource-backed prompt template exists.
	rtIdx := 0
	for _, m := range reLc4jKotlinPromptTemplateResource.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		if name == "" {
			rtIdx++
			name = "resourceTemplate" + itoa(rtIdx)
		}
		loader := src[m[4]:m[5]]
		ent := makeEntity(name+".prompt_template", "SCOPE.Pattern", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "langchain4j",
			"provenance", "INFERRED_FROM_LANGCHAIN4J_PROMPT_TEMPLATE_RESOURCE",
			"prompt_type", "prompt_template",
			"template_source", "resource",
			"resource_loader", truncTemplate(loader))
		// The first string literal inside the loader call is the resource path.
		if pm := reLc4jKotlinResourceArg.FindStringSubmatch(loader); pm != nil {
			setProps(&ent, "template_resource", pm[1])
		}
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

		wired := make(map[string]bool)
		var inlineEnts []types.EntityRecord
		for _, sm := range reLc4jKotlinWireStep.FindAllStringSubmatchIndex(chain, -1) {
			method := chain[sm[2]:sm[3]]
			role := lc4jWireRole[method]
			if role == "" {
				continue
			}
			setProps(&svc, "wire."+role, "true")

			arg := ""
			if sm[4] >= 0 {
				arg = chain[sm[4]:sm[5]]
			}
			// #5083: resolve the wiring argument whether it is a bare
			// identifier (model), an inline-constructed component
			// (OpenAiChatModel.builder().build() / MyTools()), or empty.
			addWireEdge(&svc, &inlineEnts, role, method, svcName, arg, file.Path, file.Language, lineOf(src, m[0]), regexConf, wired)
		}
		// Service entities keyed by Kind+Name; if a same-named @AiService
		// interface was already added, the wiring assembly is a distinct var so
		// dedupe on the (kind,name) key only collides when identical — acceptable.
		add(svc)
		for i := range inlineEnts {
			add(inlineEnts[i])
		}
	}

	// 9. #5083: `AiServices.create(IFace::class.java, model, tools)` positional
	// overload. langchain4j's `create` shorthand binds the first positional arg
	// to the AI-service interface, the second to the chat model, and (when
	// present) the third to the tools object. We trace the same wire roles as
	// the fluent builder so both assembly styles yield a comparable composition
	// graph.
	for _, m := range reLc4jKotlinServiceCreate.FindAllStringSubmatchIndex(src, -1) {
		svcName := src[m[2]:m[3]]
		argList := src[m[4]:m[5]]

		svc := makeEntity(svcName, "SCOPE.Service", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&svc, "framework", "langchain4j",
			"provenance", "INFERRED_FROM_LANGCHAIN4J_AI_SERVICES_CREATE",
			"assembly", "AiServices.create")

		args := splitTopLevelArgs(argList)
		// Positional role schedule, mirroring AiServices.create overloads:
		//   create(iface)                       -> [iface]
		//   create(iface, model)                -> [_, chat_model]
		//   create(iface, model, tools)         -> [_, chat_model, tools]
		posRoles := []string{"", "chat_model", "tools"}
		posMethods := []string{"", "chatLanguageModel", "tools"}
		wired := make(map[string]bool)
		var inlineEnts []types.EntityRecord
		for i, a := range args {
			if i == 0 || i >= len(posRoles) || posRoles[i] == "" {
				continue // arg 0 is the interface ::class.java literal
			}
			setProps(&svc, "wire."+posRoles[i], "true")
			addWireEdge(&svc, &inlineEnts, posRoles[i], posMethods[i], svcName, a, file.Path, file.Language, lineOf(src, m[0]), regexConf, wired)
		}
		add(svc)
		for i := range inlineEnts {
			add(inlineEnts[i])
		}
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}

// groupOr returns the first non-empty capture among the given 1-based submatch
// group indices, reading from the FindAllStringSubmatchIndex pair slice m.
func groupOr(src string, m []int, groups ...int) string {
	for _, g := range groups {
		lo, hi := m[2*g], m[2*g+1]
		if lo >= 0 && hi > lo {
			return src[lo:hi]
		}
	}
	return ""
}

// itoa is a tiny local int->string for unnamed-template fallback names, avoiding
// a strconv import for a single use.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

// addTemplateVarsAndEdges records the langchain4j prompt template body, its
// resolved `{{var}}` / `{var}` placeholders, and DEPENDS_ON edges from the
// prompt-template pattern entity to the method parameters that bind each
// placeholder (#5013). params is the raw Kotlin parameter list (may be empty for
// PromptTemplate.from(...) which has no surrounding fun). Resolution rule,
// parity with Java langchain4j:
//   - a param annotated `@V("x")` binds template variable `x`;
//   - an un-annotated param `x: T` binds template variable `x` by name.
//
// Each distinct placeholder becomes a `template_var.<name>` property whose value
// is the bound parameter identifier ("" when no param resolves it). The full
// list is also stored as `template_vars` (comma-joined) plus the template body
// (truncated) for queryability.
func addTemplateVarsAndEdges(ent *types.EntityRecord, tpl, params string, conf float64) {
	// Build param binding map: template-var-name -> kotlin param identifier.
	bind := make(map[string]string)
	if params != "" {
		for _, pm := range reLc4jKotlinParam.FindAllStringSubmatchIndex(params, -1) {
			paramName := params[pm[4]:pm[5]]
			varName := paramName
			if pm[2] >= 0 && pm[3] > pm[2] {
				varName = params[pm[2]:pm[3]] // @V("...") override
			}
			if _, ok := bind[varName]; !ok {
				bind[varName] = paramName
			}
		}
	}

	// Collect placeholders in source order, dedup, preferring the {{var}} form.
	// #5103: each placeholder is keyed by its leading identifier (the binding
	// key) but may carry a nested-field path remainder (`{{user.name}}` ->
	// key=user, field path=user.name). We dedup on the leading identifier so a
	// param object referenced via several fields binds once, and we record the
	// distinct nested-field paths seen for that key.
	var vars []string
	seenVar := make(map[string]bool)
	nestedPaths := make(map[string][]string) // leading id -> dotted paths (e.g. user.name)
	seenPath := make(map[string]bool)
	collect := func(re *regexp.Regexp) {
		for _, vm := range re.FindAllStringSubmatch(tpl, -1) {
			name := vm[1]
			// vm[2] = optional ".field.sub" remainder; strip interior whitespace.
			if rest := reLc4jKotlinWS.ReplaceAllString(vm[2], ""); rest != "" {
				full := name + rest
				if !seenPath[full] {
					seenPath[full] = true
					nestedPaths[name] = append(nestedPaths[name], full)
				}
			}
			if seenVar[name] {
				continue
			}
			seenVar[name] = true
			vars = append(vars, name)
		}
	}
	collect(reLc4jKotlinTplVarDouble)
	// Single-brace fallback only for vars not already captured as {{ }}.
	collect(reLc4jKotlinTplVarSingle)

	if len(vars) == 0 {
		// Still record the template body so the entity is queryable.
		setProps(ent, "template", truncTemplate(tpl), "template_var_count", "0")
		return
	}

	joined := ""
	var allNested []string
	for i, v := range vars {
		if i > 0 {
			joined += ","
		}
		joined += v
		boundParam := bind[v]
		setProps(ent, "template_var."+v, boundParam)
		// #5103: record the nested-field path(s) referenced through this var so
		// queries can see `{{user.name}}` resolves field `name` on param `user`.
		paths := nestedPaths[v]
		edgeProps := map[string]string{
			"framework":     "langchain4j",
			"binding":       "prompt_template_variable",
			"template_var":  v,
			"resolved_from": "method_param",
		}
		if len(paths) > 0 {
			pj := strings.Join(paths, ",")
			setProps(ent, "template_var_path."+v, pj)
			edgeProps["nested_field"] = "true"
			edgeProps["field_path"] = pj
			allNested = append(allNested, paths...)
		}
		if boundParam != "" {
			ent.Relationships = append(ent.Relationships, types.RelationshipRecord{
				ToID:       boundParam,
				Kind:       "DEPENDS_ON",
				Properties: edgeProps,
				Confidence: conf,
			})
		}
	}
	setProps(ent, "template", truncTemplate(tpl),
		"template_vars", joined,
		"template_var_count", itoa(len(vars)))
	if len(allNested) > 0 {
		setProps(ent, "template_nested_fields", strings.Join(allNested, ","),
			"has_nested_fields", "true")
	}
}

// addWireEdge resolves one langchain4j wiring argument (#5083) and records the
// corresponding USES edge on the assembled service entity. The argument may be:
//
//   - a bare identifier ("model"): the edge points at the referenced
//     field/var by name, exactly as the original #5012 wiring did;
//   - an inline-constructed component ("OpenAiChatModel.builder().build()",
//     "MyTools()"): a synthetic SCOPE.Component entity is materialized for the
//     constructed type so the wiring has a resolvable target, and the USES edge
//     points at that type name;
//   - empty / unrecognized: only the `wire.<role>` flag (already set by the
//     caller) is recorded — no USES edge.
//
// `arg_kind` on the edge ("identifier" | "inline_component") records how the
// target was resolved so downstream queries can distinguish structural
// references from materialized inline components. Inline component entities are
// appended to inlineEnts for the caller to add() after the service.
func addWireEdge(svc *types.EntityRecord, inlineEnts *[]types.EntityRecord,
	role, method, svcName, arg, filePath, language string, line int, conf float64,
	wired map[string]bool) {

	target, argKind := classifyWireArg(arg)
	if target == "" {
		return // empty or unresolvable inline expression: wire.<role> flag only
	}
	edgeKey := role + "|" + target
	if wired[edgeKey] {
		return
	}
	wired[edgeKey] = true

	if argKind == "inline_component" {
		// Materialize the inline-constructed component so the USES edge has a
		// real target. Keyed by Kind+Name; the caller's add() dedupes against
		// an identically-named field/var component if one already exists.
		comp := makeEntity(target, "SCOPE.Component", "", filePath, language, line)
		setProps(&comp, "framework", "langchain4j",
			"provenance", "INFERRED_FROM_LANGCHAIN4J_INLINE_COMPONENT",
			"wire_role", role, "constructed", "true")
		*inlineEnts = append(*inlineEnts, comp)
	}

	svc.Relationships = append(svc.Relationships, types.RelationshipRecord{
		ToID: target,
		Kind: "USES",
		Properties: map[string]string{
			"framework": "langchain4j",
			"wire_role": role,
			"method":    method,
			"service":   svcName,
			"arg_kind":  argKind,
		},
		Confidence: conf,
	})
}

// classifyWireArg inspects a wiring argument expression and returns the
// resolved USES-edge target plus how it was resolved (#5083):
//
//   - "model"                          -> ("model", "identifier")
//   - "OpenAiChatModel.builder()..."   -> ("OpenAiChatModel", "inline_component")
//   - "MyTools()" / "MyTools(cfg)"      -> ("MyTools", "inline_component")
//   - anything else (literal, lambda)  -> ("", "")
func classifyWireArg(arg string) (target, kind string) {
	arg = trimSpace(arg)
	if arg == "" {
		return "", ""
	}
	if reLc4jKotlinBareIdent.MatchString(arg) {
		return arg, "identifier"
	}
	if m := reLc4jKotlinInlineCtor.FindStringSubmatch(arg); m != nil {
		return m[1], "inline_component"
	}
	return "", ""
}

// splitTopLevelArgs splits a comma-separated argument list at top-level commas
// only, so a nested call like `OpenAiChatModel.builder().build()` or a generic
// `listOf(a, b)` is not split apart. Tracks (), <>, and string literals.
func splitTopLevelArgs(s string) []string {
	var args []string
	depth := 0
	inStr := byte(0)
	start := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case inStr != 0:
			if c == '\\' {
				i++ // skip escaped char
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
			args = append(args, trimSpace(s[start:i]))
			start = i + 1
		}
	}
	if start < len(s) {
		if a := trimSpace(s[start:]); a != "" {
			args = append(args, a)
		}
	}
	return args
}

// trimSpace trims ASCII whitespace without pulling in the strings package for a
// single caller (helpers.go already imports strings, but this file does not).
func trimSpace(s string) string {
	lo := 0
	for lo < len(s) && (s[lo] == ' ' || s[lo] == '\t' || s[lo] == '\n' || s[lo] == '\r') {
		lo++
	}
	hi := len(s)
	for hi > lo && (s[hi-1] == ' ' || s[hi-1] == '\t' || s[hi-1] == '\n' || s[hi-1] == '\r') {
		hi--
	}
	return s[lo:hi]
}

// truncTemplate caps the stored template body so a large prompt does not bloat
// entity Properties; 240 bytes is enough to identify the template at a glance.
func truncTemplate(s string) string {
	const max = 240
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}
