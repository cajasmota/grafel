package kotlin_test

import (
	"context"
	"testing"

	extreg "github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"

	_ "github.com/cajasmota/archigraph/internal/custom/kotlin"
)

func fi(path, lang, src string) extreg.FileInput {
	return extreg.FileInput{Path: path, Language: lang, Content: []byte(src)}
}

func extract(t *testing.T, name string, file extreg.FileInput) []entitySummary {
	t.Helper()
	e, ok := extreg.Get(name)
	if !ok {
		t.Fatalf("extractor %q not registered", name)
	}
	ents, err := e.Extract(context.Background(), file)
	if err != nil {
		t.Fatalf("extract error: %v", err)
	}
	var out []entitySummary
	for _, ent := range ents {
		out = append(out, entitySummary{Kind: ent.Kind, Subtype: ent.Subtype, Name: ent.Name, Props: ent.Properties})
	}
	return out
}

type entitySummary struct {
	Kind, Subtype, Name string
	Props               map[string]string
}

// findEntity returns the first entity matching kind+name, or nil.
func findEntity(ents []entitySummary, kind, name string) *entitySummary {
	for i := range ents {
		if ents[i].Kind == kind && ents[i].Name == name {
			return &ents[i]
		}
	}
	return nil
}

func containsEntity(ents []entitySummary, kind, name string) bool {
	for _, e := range ents {
		if e.Kind == kind && e.Name == name {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Compose
// ---------------------------------------------------------------------------

func TestComposeUIComponent(t *testing.T) {
	src := `
@Composable
fun UserProfile(name: String) {
    Text(name)
}
`
	ents := extract(t, "custom_kotlin_compose", fi("Profile.kt", "kotlin", src))
	if !containsEntity(ents, "SCOPE.UIComponent", "UserProfile") {
		t.Error("expected UserProfile SCOPE.UIComponent")
	}
}

func TestComposeNavRoute(t *testing.T) {
	src := `
NavHost(navController, startDestination = "home") {
    composable("home") { HomeScreen() }
    composable("detail/{id}") { DetailScreen() }
}
`
	ents := extract(t, "custom_kotlin_compose", fi("Nav.kt", "kotlin", src))
	if !containsEntity(ents, "SCOPE.Operation", "home") {
		t.Error("expected home nav route")
	}
}

func TestComposeBuiltinSkipped(t *testing.T) {
	src := `
@Composable
fun Text(text: String) {}
`
	ents := extract(t, "custom_kotlin_compose", fi("Builtins.kt", "kotlin", src))
	// Text is a builtin — should be skipped
	if containsEntity(ents, "SCOPE.UIComponent", "Text") {
		t.Error("builtin Text should be skipped")
	}
}

func TestComposeNoMatch(t *testing.T) {
	src := `fun plainFunction() = println("hello")`
	ents := extract(t, "custom_kotlin_compose", fi("Plain.kt", "kotlin", src))
	if len(ents) != 0 {
		t.Errorf("expected no entities, got %d", len(ents))
	}
}

func TestComposeWrongLanguage(t *testing.T) {
	src := `@Composable fun Screen() {}`
	ents := extract(t, "custom_kotlin_compose", fi("Screen.java", "java", src))
	if len(ents) != 0 {
		t.Errorf("wrong language should return no entities, got %d", len(ents))
	}
}

// ---------------------------------------------------------------------------
// Ktor
// ---------------------------------------------------------------------------

func TestKtorRoute(t *testing.T) {
	src := `
routing {
    get("/users") { call.respond(users) }
    post("/users") { call.respond(HttpStatusCode.Created) }
}
`
	ents := extract(t, "custom_kotlin_ktor", fi("Routing.kt", "kotlin", src))
	if !containsEntity(ents, "SCOPE.Operation", "GET /users") {
		t.Error("expected GET /users route")
	}
	if !containsEntity(ents, "SCOPE.Operation", "POST /users") {
		t.Error("expected POST /users route")
	}
}

func TestKtorPlugin(t *testing.T) {
	src := `
fun Application.configureHTTP() {
    install(ContentNegotiation) { json() }
    install(CORS) { anyHost() }
}
`
	ents := extract(t, "custom_kotlin_ktor", fi("Plugins.kt", "kotlin", src))
	// Plugin entity name = plugin name directly
	if !containsEntity(ents, "SCOPE.Pattern", "ContentNegotiation") {
		t.Error("expected ContentNegotiation plugin")
	}
}

func TestKtorAuthenticate(t *testing.T) {
	src := `
authenticate("jwt") {
    get("/protected") { call.respond("ok") }
}
`
	ents := extract(t, "custom_kotlin_ktor", fi("Auth.kt", "kotlin", src))
	if !containsEntity(ents, "SCOPE.Pattern", "authenticate:jwt") {
		t.Error("expected jwt authenticate pattern")
	}
}

func TestKtorNoMatch(t *testing.T) {
	src := `val x = 42`
	ents := extract(t, "custom_kotlin_ktor", fi("Const.kt", "kotlin", src))
	if len(ents) != 0 {
		t.Errorf("expected no entities, got %d", len(ents))
	}
}

// ---------------------------------------------------------------------------
// LangChain4j
// ---------------------------------------------------------------------------

func TestLangChain4jAiService(t *testing.T) {
	src := `
@AiService
interface AssistantService {
    fun chat(message: String): String
}
`
	ents := extract(t, "custom_kotlin_langchain4j", fi("Service.kt", "kotlin", src))
	if !containsEntity(ents, "SCOPE.Service", "AssistantService") {
		t.Error("expected AssistantService SCOPE.Service")
	}
}

func TestLangChain4jTool(t *testing.T) {
	src := `
class Tools {
    @Tool("Search the web")
    fun webSearch(query: String): String = ""
}
`
	ents := extract(t, "custom_kotlin_langchain4j", fi("Tools.kt", "kotlin", src))
	if !containsEntity(ents, "SCOPE.Operation", "webSearch") {
		t.Error("expected webSearch SCOPE.Operation")
	}
}

func TestLangChain4jChatModel(t *testing.T) {
	src := `
class MyAgent {
    private val model: ChatLanguageModel = OpenAiChatModel.builder().build()
}
`
	ents := extract(t, "custom_kotlin_langchain4j", fi("Agent.kt", "kotlin", src))
	if !containsEntity(ents, "SCOPE.Component", "model") {
		t.Error("expected model ChatLanguageModel component")
	}
}

func TestLangChain4jNoMatch(t *testing.T) {
	src := `data class User(val name: String)`
	ents := extract(t, "custom_kotlin_langchain4j", fi("User.kt", "kotlin", src))
	if len(ents) != 0 {
		t.Errorf("expected no entities, got %d", len(ents))
	}
}

// chain_composition wiring (#5012): runtime AiServices.builder() assembly is
// traced into a SCOPE.Service entity carrying USES edges to each wired
// component (chatLanguageModel / tools / chatMemory / contentRetriever).
func TestLangChain4jServiceWiring(t *testing.T) {
	src := `
class AssistantConfig {
    private val model: ChatLanguageModel = OpenAiChatModel.builder().build()
    private val memory: ChatMemory = MessageWindowChatMemory.withMaxMessages(10)
    private val retriever: ContentRetriever = EmbeddingStoreContentRetriever.from(store)

    fun assistant(tools: Any): Assistant {
        val assistant = AiServices.builder(Assistant::class.java)
            .chatLanguageModel(model)
            .tools(tools)
            .chatMemory(memory)
            .contentRetriever(retriever)
            .build()
        return assistant
    }
}
`
	e, ok := extreg.Get("custom_kotlin_langchain4j")
	if !ok {
		t.Fatal("extractor custom_kotlin_langchain4j not registered")
	}
	ents, err := e.Extract(context.Background(), fi("Config.kt", "kotlin", src))
	if err != nil {
		t.Fatalf("extract error: %v", err)
	}

	var svc *types.EntityRecord
	for i := range ents {
		if ents[i].Kind == "SCOPE.Service" && ents[i].Name == "assistant" {
			svc = &ents[i]
		}
	}
	if svc == nil {
		t.Fatal("expected assembled SCOPE.Service 'assistant' from AiServices.builder")
	}
	if svc.Properties["provenance"] != "INFERRED_FROM_LANGCHAIN4J_AI_SERVICES_BUILDER" {
		t.Errorf("wrong provenance: %s", svc.Properties["provenance"])
	}

	want := map[string]string{
		"model":     "chat_model",
		"tools":     "tools",
		"memory":    "chat_memory",
		"retriever": "content_retriever",
	}
	got := make(map[string]string)
	for _, r := range svc.Relationships {
		if r.Kind != "USES" {
			t.Errorf("expected USES edge, got %s", r.Kind)
		}
		got[r.ToID] = r.Properties["wire_role"]
	}
	for target, role := range want {
		if got[target] != role {
			t.Errorf("wiring edge to %q: got role %q, want %q (edges=%v)", target, got[target], role, got)
		}
	}
}

// chain_composition inline-arg resolution (#5083): a fluent builder wired with
// inline-constructed components (`OpenAiChatModel.builder().build()`,
// `MyTools()`) materializes a synthetic SCOPE.Component target for the
// constructed type and points the USES edge at it (arg_kind=inline_component),
// instead of dropping the edge for lack of a bare identifier.
func TestLangChain4jServiceWiringInlineArgs(t *testing.T) {
	src := `
class AssistantConfig {
    fun assistant(): Assistant {
        val assistant = AiServices.builder(Assistant::class.java)
            .chatLanguageModel(OpenAiChatModel.builder().apiKey("x").build())
            .tools(MyTools())
            .build()
        return assistant
    }
}
`
	e, ok := extreg.Get("custom_kotlin_langchain4j")
	if !ok {
		t.Fatal("extractor custom_kotlin_langchain4j not registered")
	}
	ents, err := e.Extract(context.Background(), fi("Config.kt", "kotlin", src))
	if err != nil {
		t.Fatalf("extract error: %v", err)
	}

	var svc *types.EntityRecord
	comps := map[string]*types.EntityRecord{}
	for i := range ents {
		switch {
		case ents[i].Kind == "SCOPE.Service" && ents[i].Name == "assistant":
			svc = &ents[i]
		case ents[i].Kind == "SCOPE.Component":
			comps[ents[i].Name] = &ents[i]
		}
	}
	if svc == nil {
		t.Fatal("expected assembled SCOPE.Service 'assistant'")
	}

	// USES edges should target the constructed TYPE names, classified inline.
	want := map[string]string{"OpenAiChatModel": "chat_model", "MyTools": "tools"}
	got := map[string]string{}
	for _, r := range svc.Relationships {
		if r.Kind != "USES" {
			continue
		}
		got[r.ToID] = r.Properties["wire_role"]
		if r.Properties["arg_kind"] != "inline_component" {
			t.Errorf("edge to %q: arg_kind=%q want inline_component", r.ToID, r.Properties["arg_kind"])
		}
	}
	for target, role := range want {
		if got[target] != role {
			t.Errorf("inline wiring edge to %q: got role %q want %q (edges=%v)", target, got[target], role, got)
		}
		if comps[target] == nil {
			t.Errorf("expected materialized SCOPE.Component %q for inline arg", target)
			continue
		}
		if comps[target].Properties["provenance"] != "INFERRED_FROM_LANGCHAIN4J_INLINE_COMPONENT" {
			t.Errorf("component %q wrong provenance: %s", target, comps[target].Properties["provenance"])
		}
	}
}

// chain_composition AiServices.create() positional overload (#5083): the
// positional `create(IFace, model, tools)` shorthand has its model/tools args
// traced into USES edges with the same wire roles as the fluent builder.
func TestLangChain4jServiceCreatePositional(t *testing.T) {
	src := `
class Factory {
    fun build(model: ChatLanguageModel, tools: Any): Assistant {
        val assistant = AiServices.create(Assistant::class.java, model, tools)
        return assistant
    }
}
`
	e, ok := extreg.Get("custom_kotlin_langchain4j")
	if !ok {
		t.Fatal("extractor custom_kotlin_langchain4j not registered")
	}
	ents, err := e.Extract(context.Background(), fi("Factory.kt", "kotlin", src))
	if err != nil {
		t.Fatalf("extract error: %v", err)
	}

	var svc *types.EntityRecord
	for i := range ents {
		if ents[i].Kind == "SCOPE.Service" && ents[i].Name == "assistant" {
			svc = &ents[i]
		}
	}
	if svc == nil {
		t.Fatal("expected SCOPE.Service 'assistant' from AiServices.create")
	}
	if svc.Properties["provenance"] != "INFERRED_FROM_LANGCHAIN4J_AI_SERVICES_CREATE" {
		t.Errorf("wrong provenance: %s", svc.Properties["provenance"])
	}
	want := map[string]string{"model": "chat_model", "tools": "tools"}
	got := map[string]string{}
	for _, r := range svc.Relationships {
		if r.Kind != "USES" {
			continue
		}
		got[r.ToID] = r.Properties["wire_role"]
		if r.Properties["arg_kind"] != "identifier" {
			t.Errorf("edge to %q: arg_kind=%q want identifier", r.ToID, r.Properties["arg_kind"])
		}
	}
	for target, role := range want {
		if got[target] != role {
			t.Errorf("create wiring edge to %q: got %q want %q (edges=%v)", target, got[target], role, got)
		}
	}
	// The interface ::class.java literal (arg 0) must NOT become a USES edge.
	if _, bad := got["Assistant"]; bad {
		t.Errorf("interface positional arg leaked into a USES edge: %v", got)
	}
}

// create() with an inline-constructed model arg (#5083): the positional model
// slot resolves an inline constructor to a synthetic component target too.
func TestLangChain4jServiceCreateInlineModel(t *testing.T) {
	src := `
val assistant = AiServices.create(Assistant::class.java, OpenAiChatModel.builder().build())
`
	e, _ := extreg.Get("custom_kotlin_langchain4j")
	ents, err := e.Extract(context.Background(), fi("Top.kt", "kotlin", src))
	if err != nil {
		t.Fatalf("extract error: %v", err)
	}
	var svc *types.EntityRecord
	var haveComp bool
	for i := range ents {
		if ents[i].Kind == "SCOPE.Service" && ents[i].Name == "assistant" {
			svc = &ents[i]
		}
		if ents[i].Kind == "SCOPE.Component" && ents[i].Name == "OpenAiChatModel" {
			haveComp = true
		}
	}
	if svc == nil {
		t.Fatal("expected SCOPE.Service 'assistant'")
	}
	if !haveComp {
		t.Error("expected materialized SCOPE.Component 'OpenAiChatModel' for inline create arg")
	}
	var found bool
	for _, r := range svc.Relationships {
		if r.Kind == "USES" && r.ToID == "OpenAiChatModel" && r.Properties["wire_role"] == "chat_model" {
			found = true
			if r.Properties["arg_kind"] != "inline_component" {
				t.Errorf("arg_kind=%q want inline_component", r.Properties["arg_kind"])
			}
		}
	}
	if !found {
		t.Errorf("expected USES->OpenAiChatModel chat_model edge (rels=%v)", svc.Relationships)
	}
}

// prompt_template_extraction template-variable resolution (#5013, parity with
// Java langchain4j): @SystemMessage/@UserMessage inline templates have their
// {{var}} placeholders resolved against @V("var") / un-annotated fun params,
// emitting template_var.* properties + DEPENDS_ON edges to the bound params.
func TestLangChain4jPromptTemplateVars(t *testing.T) {
	src := `
interface Assistant {
    @SystemMessage("You are a {{role}} assistant for {{company}}.")
    @UserMessage("Answer the question {{question}} in {{lang}}.")
    fun chat(@V("role") position: String, @V("company") org: String, question: String, @V("lang") language: String): String
}
`
	e, ok := extreg.Get("custom_kotlin_langchain4j")
	if !ok {
		t.Fatal("extractor custom_kotlin_langchain4j not registered")
	}
	ents, err := e.Extract(context.Background(), fi("Assistant.kt", "kotlin", src))
	if err != nil {
		t.Fatalf("extract error: %v", err)
	}

	find := func(name string) *types.EntityRecord {
		for i := range ents {
			if ents[i].Kind == "SCOPE.Pattern" && ents[i].Name == name {
				return &ents[i]
			}
		}
		return nil
	}

	sys := find("chat.system_message")
	if sys == nil {
		t.Fatal("expected chat.system_message prompt pattern")
	}
	// {{role}} -> @V("role") position ; {{company}} -> @V("company") org
	if got := sys.Properties["template_var.role"]; got != "position" {
		t.Errorf("system role binding: got %q, want %q", got, "position")
	}
	if got := sys.Properties["template_var.company"]; got != "org" {
		t.Errorf("system company binding: got %q, want %q", got, "org")
	}
	if got := sys.Properties["template_var_count"]; got != "2" {
		t.Errorf("system template_var_count: got %q, want 2", got)
	}
	sysEdges := map[string]string{}
	for _, r := range sys.Relationships {
		if r.Kind != "DEPENDS_ON" {
			t.Errorf("expected DEPENDS_ON edge, got %s", r.Kind)
		}
		sysEdges[r.Properties["template_var"]] = r.ToID
	}
	if sysEdges["role"] != "position" || sysEdges["company"] != "org" {
		t.Errorf("system DEPENDS_ON edges = %v, want role->position, company->org", sysEdges)
	}

	usr := find("chat.user_message")
	if usr == nil {
		t.Fatal("expected chat.user_message prompt pattern")
	}
	// {{question}} -> un-annotated param question (bind by name)
	if got := usr.Properties["template_var.question"]; got != "question" {
		t.Errorf("user question binding: got %q, want %q", got, "question")
	}
	// {{lang}} -> @V("lang") language
	if got := usr.Properties["template_var.lang"]; got != "language" {
		t.Errorf("user lang binding: got %q, want %q", got, "language")
	}
}

// PromptTemplate.from("...{{var}}...") programmatic templates (#5013) record
// placeholders as template variables with no resolvable param binding.
func TestLangChain4jPromptTemplateFrom(t *testing.T) {
	src := `
class PromptFactory {
    fun build(): PromptTemplate {
        val greeting = PromptTemplate.from("Hello {{name}}, welcome to {{place}}!")
        return greeting
    }
}
`
	e, _ := extreg.Get("custom_kotlin_langchain4j")
	ents, err := e.Extract(context.Background(), fi("PromptFactory.kt", "kotlin", src))
	if err != nil {
		t.Fatalf("extract error: %v", err)
	}
	var pt *types.EntityRecord
	for i := range ents {
		if ents[i].Kind == "SCOPE.Pattern" && ents[i].Name == "greeting.prompt_template" {
			pt = &ents[i]
		}
	}
	if pt == nil {
		t.Fatal("expected greeting.prompt_template SCOPE.Pattern")
	}
	if pt.Properties["provenance"] != "INFERRED_FROM_LANGCHAIN4J_PROMPT_TEMPLATE" {
		t.Errorf("wrong provenance: %s", pt.Properties["provenance"])
	}
	if got := pt.Properties["template_vars"]; got != "name,place" {
		t.Errorf("template_vars: got %q, want %q", got, "name,place")
	}
	// No surrounding fun params, so placeholders bind to nothing and emit no edges.
	if got := pt.Properties["template_var.name"]; got != "" {
		t.Errorf("expected unbound name var, got %q", got)
	}
	if len(pt.Relationships) != 0 {
		t.Errorf("expected no DEPENDS_ON edges for programmatic template, got %d", len(pt.Relationships))
	}
}

// confidence_overlay (#4974, parity with Java #3093): the langchain4j extractor
// stamps a top-level EntityRecord.Confidence directly. All entities are regex
// pattern matches, so the stamped value is BaseConfidence(SourceRegexPattern)=0.7.
func TestLangChain4jConfidenceStamp(t *testing.T) {
	src := `
@AiService
interface AssistantService {
    @SystemMessage("You are helpful")
    fun chat(message: String): String
}

class Tools {
    @Tool("Search the web")
    fun webSearch(query: String): String = ""
}

class MyAgent {
    private val model: ChatLanguageModel = OpenAiChatModel.builder().build()
}
`
	e, ok := extreg.Get("custom_kotlin_langchain4j")
	if !ok {
		t.Fatal("extractor custom_kotlin_langchain4j not registered")
	}
	ents, err := e.Extract(context.Background(), fi("Mixed.kt", "kotlin", src))
	if err != nil {
		t.Fatalf("extract error: %v", err)
	}
	if len(ents) == 0 {
		t.Fatal("expected langchain4j entities, got none")
	}
	want := types.BaseConfidence(types.SourceRegexPattern)
	for _, ent := range ents {
		if ent.Confidence != want {
			t.Errorf("entity %s/%s: Confidence = %v, want %v (regex_pattern)",
				ent.Kind, ent.Name, ent.Confidence, want)
		}
	}
}
