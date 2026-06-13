package java

import "testing"

// langchain4j_wiring_test.go — value-asserting tests for the #5155 java
// langchain4j runtime AiServices wiring (port of kotlin #5012/#5083). Assertions
// check the SEMANTIC wiring edge (service -> wired component, wire_role,
// arg_kind), never len>0.

func lc4jCtx(source string) PatternContext {
	return PatternContext{Source: source, Language: "java", Framework: "langchain4j", FilePath: "Config.java"}
}

// findService returns the assembled SCOPE.Service entity by name, or nil.
func lc4jFindService(r PatternResult, name string) *SecondaryEntity {
	for i := range r.Entities {
		if r.Entities[i].Kind == "SCOPE.Service" && r.Entities[i].Name == name {
			return &r.Entities[i]
		}
	}
	return nil
}

// lc4jWireEdges maps each USES edge's resolved target NAME -> wire_role for the
// service identified by svcRef.
func lc4jWireEdges(r PatternResult, svcRef string) (roles, kinds map[string]string) {
	roles = map[string]string{}
	kinds = map[string]string{}
	for _, rel := range r.Relationships {
		if rel.RelationshipType != "USES" || rel.SourceRef != svcRef {
			continue
		}
		to := refName(r.Entities, rel.TargetRef)
		roles[to] = rel.Properties["wire_role"]
		kinds[to] = rel.Properties["arg_kind"]
	}
	return roles, kinds
}

// ── happy path: AiServices.builder fluent chain ─────────────────────────────

func TestLc4jServiceWiringBuilder(t *testing.T) {
	src := `
class AssistantConfig {
    private final ChatLanguageModel model = OpenAiChatModel.builder().build();
    private final ChatMemory memory = MessageWindowChatMemory.withMaxMessages(10);
    private final ContentRetriever retriever = EmbeddingStoreContentRetriever.from(store);

    Assistant assistant() {
        Assistant assistant = AiServices.builder(Assistant.class)
            .chatLanguageModel(model)
            .tools(tools)
            .chatMemory(memory)
            .contentRetriever(retriever)
            .build();
        return assistant;
    }
}
`
	r := ExtractLangChain4J(lc4jCtx(src))
	svc := lc4jFindService(r, "assistant")
	if svc == nil {
		t.Fatal("expected assembled SCOPE.Service 'assistant' from AiServices.builder")
	}
	if svc.Provenance != "INFERRED_FROM_LANGCHAIN4J_AI_SERVICES_BUILDER" {
		t.Errorf("wrong provenance: %s", svc.Provenance)
	}
	if svc.Properties["assembly"] != "AiServices.builder" {
		t.Errorf("missing assembly property: %v", svc.Properties)
	}

	roles, kinds := lc4jWireEdges(r, svc.Ref)
	want := map[string]string{
		"model":     "chat_model",
		"tools":     "tools",
		"memory":    "chat_memory",
		"retriever": "content_retriever",
	}
	for target, role := range want {
		if roles[target] != role {
			t.Errorf("wiring edge to %q: got role %q want %q (edges=%v)", target, roles[target], role, roles)
		}
		if kinds[target] != "identifier" {
			t.Errorf("edge to %q: arg_kind=%q want identifier", target, kinds[target])
		}
	}
	// wire.* flags recorded on the service.
	for _, role := range want {
		if svc.Properties["wire."+role] != "true" {
			t.Errorf("missing wire.%s flag (props=%v)", role, svc.Properties)
		}
	}
}

// ── happy path: inline-constructed args materialize components ──────────────

func TestLc4jServiceWiringInlineArgs(t *testing.T) {
	src := `
class AssistantConfig {
    Assistant assistant() {
        Assistant assistant = AiServices.builder(Assistant.class)
            .chatLanguageModel(OpenAiChatModel.builder().apiKey("x").build())
            .tools(new MyTools())
            .build();
        return assistant;
    }
}
`
	r := ExtractLangChain4J(lc4jCtx(src))
	svc := lc4jFindService(r, "assistant")
	if svc == nil {
		t.Fatal("expected assembled SCOPE.Service 'assistant'")
	}
	roles, kinds := lc4jWireEdges(r, svc.Ref)
	want := map[string]string{"OpenAiChatModel": "chat_model", "MyTools": "tools"}
	for target, role := range want {
		if roles[target] != role {
			t.Errorf("inline edge to %q: got role %q want %q (edges=%v)", target, roles[target], role, roles)
		}
		if kinds[target] != "inline_component" {
			t.Errorf("edge to %q: arg_kind=%q want inline_component", target, kinds[target])
		}
		// the inline component must be materialized as a SCOPE.Component.
		var found bool
		for _, e := range r.Entities {
			if e.Kind == "SCOPE.Component" && e.Name == target &&
				e.Provenance == "INFERRED_FROM_LANGCHAIN4J_INLINE_COMPONENT" {
				found = true
			}
		}
		if !found {
			t.Errorf("expected materialized SCOPE.Component %q for inline arg", target)
		}
	}
}

// ── happy path: AiServices.create positional overload ───────────────────────

func TestLc4jServiceCreatePositional(t *testing.T) {
	src := `
class Factory {
    Assistant build(ChatLanguageModel model, Object tools) {
        Assistant assistant = AiServices.create(Assistant.class, model, tools);
        return assistant;
    }
}
`
	r := ExtractLangChain4J(lc4jCtx(src))
	svc := lc4jFindService(r, "assistant")
	if svc == nil {
		t.Fatal("expected SCOPE.Service 'assistant' from AiServices.create")
	}
	if svc.Provenance != "INFERRED_FROM_LANGCHAIN4J_AI_SERVICES_CREATE" {
		t.Errorf("wrong provenance: %s", svc.Provenance)
	}
	roles, kinds := lc4jWireEdges(r, svc.Ref)
	want := map[string]string{"model": "chat_model", "tools": "tools"}
	for target, role := range want {
		if roles[target] != role {
			t.Errorf("create edge to %q: got %q want %q (edges=%v)", target, roles[target], role, roles)
		}
		if kinds[target] != "identifier" {
			t.Errorf("edge to %q: arg_kind=%q want identifier", target, kinds[target])
		}
	}
	// the interface .class literal (arg0) must NOT become a USES edge.
	if _, bad := roles["Assistant"]; bad {
		t.Errorf("interface positional arg leaked into a USES edge: %v", roles)
	}
}

// ── happy path: create with inline-constructed model arg ────────────────────

func TestLc4jServiceCreateInlineModel(t *testing.T) {
	src := `
class Top {
    Assistant assistant = AiServices.create(Assistant.class, OpenAiChatModel.builder().build());
}
`
	r := ExtractLangChain4J(lc4jCtx(src))
	svc := lc4jFindService(r, "assistant")
	if svc == nil {
		t.Fatal("expected SCOPE.Service 'assistant'")
	}
	roles, kinds := lc4jWireEdges(r, svc.Ref)
	if roles["OpenAiChatModel"] != "chat_model" {
		t.Errorf("expected USES->OpenAiChatModel chat_model edge (edges=%v)", roles)
	}
	if kinds["OpenAiChatModel"] != "inline_component" {
		t.Errorf("arg_kind=%q want inline_component", kinds["OpenAiChatModel"])
	}
	var found bool
	for _, e := range r.Entities {
		if e.Kind == "SCOPE.Component" && e.Name == "OpenAiChatModel" {
			found = true
		}
	}
	if !found {
		t.Error("expected materialized SCOPE.Component 'OpenAiChatModel' for inline create arg")
	}
}

// ── wrong-language no-op ─────────────────────────────────────────────────────

func TestLc4jServiceWiringWrongLanguageNoOp(t *testing.T) {
	src := `
val assistant = AiServices.builder(Assistant::class.java).chatLanguageModel(model).build()
`
	ctx := PatternContext{Source: src, Language: "kotlin", Framework: "langchain4j", FilePath: "Config.kt"}
	r := ExtractLangChain4J(ctx)
	if len(r.Entities) != 0 || len(r.Relationships) != 0 {
		t.Errorf("expected no-op for non-java language, got %d entities %d rels",
			len(r.Entities), len(r.Relationships))
	}
}

// ── no-match no-op (java + langchain4j but no AiServices assembly) ───────────

func TestLc4jServiceWiringNoMatchNoOp(t *testing.T) {
	src := `
class Plain {
    int add(int a, int b) { return a + b; }
}
`
	r := ExtractLangChain4J(lc4jCtx(src))
	for _, e := range r.Entities {
		if e.Provenance == "INFERRED_FROM_LANGCHAIN4J_AI_SERVICES_BUILDER" ||
			e.Provenance == "INFERRED_FROM_LANGCHAIN4J_AI_SERVICES_CREATE" {
			t.Errorf("expected no AiServices wiring entity, got %q", e.Name)
		}
	}
	for _, rel := range r.Relationships {
		if rel.RelationshipType == "USES" {
			t.Errorf("expected no USES wiring edge, got %v", rel)
		}
	}
}
