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
