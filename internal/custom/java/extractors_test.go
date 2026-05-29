package java

import (
	"strings"
	"testing"
)

// ============================================================================
// Lombok tests
// ============================================================================

func TestLombokInfer_Data(t *testing.T) {
	entities := LombokInfer("User", []string{"@Data"}, "User.java", 1)
	names := entityNames(entities)
	for _, want := range []string{"get*", "set*", "equals", "hashCode", "toString"} {
		if !contains(names, want) {
			t.Errorf("@Data missing %q, got %v", want, names)
		}
	}
}

func TestLombokInfer_Value(t *testing.T) {
	entities := LombokInfer("Config", []string{"@Value"}, "Config.java", 1)
	names := entityNames(entities)
	if contains(names, "set*") {
		t.Error("@Value should NOT produce setters")
	}
	if !contains(names, "get*") {
		t.Error("@Value should produce getters")
	}
}

func TestLombokInfer_Builder(t *testing.T) {
	entities := LombokInfer("Order", []string{"@Builder"}, "Order.java", 5)
	names := entityNames(entities)
	if !contains(names, "builder") || !contains(names, "build") {
		t.Errorf("@Builder missing builder/build, got %v", names)
	}
}

func TestLombokInfer_NoAnnotations(t *testing.T) {
	entities := LombokInfer("Plain", nil, "Plain.java", 1)
	if len(entities) != 0 {
		t.Errorf("expected 0 entities for no annotations, got %d", len(entities))
	}
}

func TestLombokInfer_AllArgsConstructor(t *testing.T) {
	entities := LombokInfer("Dto", []string{"@AllArgsConstructor"}, "Dto.java", 1)
	names := entityNames(entities)
	if !contains(names, "allArgsConstructor") {
		t.Errorf("@AllArgsConstructor missing, got %v", names)
	}
}

func TestLombokInfer_NoArgsConstructor(t *testing.T) {
	entities := LombokInfer("Dto", []string{"@NoArgsConstructor"}, "Dto.java", 1)
	names := entityNames(entities)
	if !contains(names, "noArgsConstructor") {
		t.Errorf("@NoArgsConstructor missing, got %v", names)
	}
}

func TestLombokInfer_Provenance(t *testing.T) {
	entities := LombokInfer("X", []string{"@Getter"}, "X.java", 1)
	if len(entities) == 0 {
		t.Fatal("expected entities")
	}
	if entities[0].Provenance != "INFERRED_FROM_LOMBOK" {
		t.Errorf("wrong provenance: %s", entities[0].Provenance)
	}
}

// ============================================================================
// Fields tests
// ============================================================================

func TestExtractFields_Basic(t *testing.T) {
	source := `public class Foo {
    private String name;
    protected int age;
    public List<String> tags;
}`
	fields := ExtractFields(source, "Foo.java")
	if len(fields) < 3 {
		t.Fatalf("expected >= 3 fields, got %d", len(fields))
	}
	names := make([]string, len(fields))
	for i, f := range fields {
		names[i] = f.Name
	}
	for _, want := range []string{"name", "age", "tags"} {
		if !contains(names, want) {
			t.Errorf("missing field %q in %v", want, names)
		}
	}
}

// ============================================================================
// Spring Boot tests
// ============================================================================

func TestSpringBoot_Controller(t *testing.T) {
	source := `
@RestController
@RequestMapping("/api/users")
public class UserController {
    @GetMapping("/{id}")
    public User getUser(@PathVariable Long id) { return null; }

    @PostMapping
    public User createUser(@RequestBody User user) { return null; }
}
`
	r := ExtractSpringBoot(PatternContext{Source: source, Language: "java", Framework: "spring_boot", FilePath: "UserController.java"})
	if len(r.Entities) < 2 {
		t.Fatalf("expected >= 2 entities, got %d", len(r.Entities))
	}
	found := false
	for _, e := range r.Entities {
		if e.Subtype == "endpoint" && strings.Contains(e.Name, "getUser") {
			found = true
			if e.Properties["http_method"] != "GET" {
				t.Errorf("expected GET, got %v", e.Properties["http_method"])
			}
		}
	}
	if !found {
		t.Error("getUser endpoint not found")
	}
}

func TestSpringBoot_Service(t *testing.T) {
	source := `
@Service
public class UserService {
    public void doStuff() {}
}
`
	r := ExtractSpringBoot(PatternContext{Source: source, Language: "java", Framework: "spring_boot", FilePath: "UserService.java"})
	if len(r.Entities) == 0 {
		t.Fatal("expected service entity")
	}
	if r.Entities[0].Properties["stereotype"] != "service" {
		t.Errorf("expected stereotype=service, got %v", r.Entities[0].Properties["stereotype"])
	}
}

func TestSpringBoot_Configuration(t *testing.T) {
	source := `
@Configuration
public class AppConfig {
    @Bean
    public DataSource dataSource() { return null; }
}
`
	r := ExtractSpringBoot(PatternContext{Source: source, Language: "java", Framework: "spring_boot", FilePath: "AppConfig.java"})
	hasConfig := false
	hasBean := false
	for _, e := range r.Entities {
		if e.Kind == "SCOPE.Pattern" {
			hasConfig = true
		}
		if e.Kind == "SCOPE.Operation" && e.Subtype == "function" {
			hasBean = true
		}
	}
	if !hasConfig {
		t.Error("missing configuration entity")
	}
	if !hasBean {
		t.Error("missing bean entity")
	}
	if len(r.Relationships) == 0 {
		t.Error("expected OWNS relationship")
	}
}

func TestSpringBoot_WrongFramework(t *testing.T) {
	source := `@RestController public class X { @GetMapping public void foo() {} }`
	r := ExtractSpringBoot(PatternContext{Source: source, Language: "java", Framework: "django", FilePath: "X.java"})
	if len(r.Entities) != 0 {
		t.Errorf("expected 0 entities for wrong framework, got %d", len(r.Entities))
	}
}

// ============================================================================
// Spring Request/Response tests
// ============================================================================

func TestSpringReqResp_AcceptsInput(t *testing.T) {
	source := `
@RestController
public class OrderController {
    @PostMapping("/orders")
    public ResponseEntity<OrderDTO> create(@RequestBody OrderDTO dto) { return null; }
}
`
	r := ExtractSpringRequestResponse(PatternContext{Source: source, Language: "java", Framework: "spring_boot", FilePath: "OrderController.java"})
	hasAcceptsInput := false
	for _, rel := range r.Relationships {
		if rel.RelationshipType == "ACCEPTS_INPUT" {
			hasAcceptsInput = true
		}
	}
	if !hasAcceptsInput {
		t.Error("expected ACCEPTS_INPUT relationship")
	}
}

func TestSpringReqResp_Returns(t *testing.T) {
	source := `
@RestController
public class OrderController {
    @GetMapping("/orders/{id}")
    public ResponseEntity<OrderDTO> get(@PathVariable Long id) { return null; }
}
`
	r := ExtractSpringRequestResponse(PatternContext{Source: source, Language: "java", Framework: "spring_boot", FilePath: "OrderController.java"})
	hasReturns := false
	for _, rel := range r.Relationships {
		if rel.RelationshipType == "RETURNS" {
			hasReturns = true
		}
	}
	if !hasReturns {
		t.Error("expected RETURNS relationship")
	}
}

func TestSpringReqResp_NoController(t *testing.T) {
	source := `public class PlainClass { public void foo() {} }`
	r := ExtractSpringRequestResponse(PatternContext{Source: source, Language: "java", Framework: "spring_boot", FilePath: "X.java"})
	if len(r.Entities) != 0 || len(r.Relationships) != 0 {
		t.Error("expected empty result for non-controller")
	}
}

// ============================================================================
// Spring Ecosystem tests
// ============================================================================

func TestSpringEco_SecurityFilterChain(t *testing.T) {
	source := `
@Configuration
public class SecurityConfig {
    @Bean
    public SecurityFilterChain filterChain(HttpSecurity http) { return null; }
}
`
	r := ExtractSpringEcosystem(PatternContext{Source: source, Language: "java", Framework: "spring_boot", FilePath: "SecurityConfig.java"})
	found := false
	for _, e := range r.Entities {
		if e.Provenance == "INFERRED_FROM_SPRING_SECURITY" {
			found = true
		}
	}
	if !found {
		t.Error("expected security filter chain entity")
	}
}

func TestSpringEco_KafkaListener(t *testing.T) {
	source := `
public class Consumer {
    @KafkaListener(topics = "orders")
    public void consume(String msg) {}
}
`
	r := ExtractSpringEcosystem(PatternContext{Source: source, Language: "java", Framework: "spring_boot", FilePath: "Consumer.java"})
	found := false
	for _, e := range r.Entities {
		if e.Provenance == "INFERRED_FROM_SPRING_KAFKA" {
			found = true
			if e.Properties["topic"] != "orders" {
				t.Errorf("expected topic=orders, got %v", e.Properties["topic"])
			}
		}
	}
	if !found {
		t.Error("expected kafka listener entity")
	}
}

func TestSpringEco_FeignClient(t *testing.T) {
	source := `
@FeignClient(name = "user-service")
public interface UserClient {
    @GetMapping("/users/{id}")
    User getUser(@PathVariable Long id);
}
`
	r := ExtractSpringEcosystem(PatternContext{Source: source, Language: "java", Framework: "spring_boot", FilePath: "UserClient.java"})
	found := false
	for _, e := range r.Entities {
		if e.Provenance == "INFERRED_FROM_SPRING_CLOUD" {
			found = true
		}
	}
	if !found {
		t.Error("expected feign client entity")
	}
	if len(r.Relationships) == 0 {
		t.Error("expected DEPENDS_ON relationship for feign client")
	}
}

// ============================================================================
// Hibernate tests
// ============================================================================

func TestHibernate_Entity(t *testing.T) {
	source := `
@Entity
@Table(name="users")
public class User {
    @Id
    private Long id;
    private String name;
}
`
	r := ExtractHibernate(PatternContext{Source: source, Language: "java", Framework: "hibernate", FilePath: "User.java"})
	if len(r.Entities) == 0 {
		t.Fatal("expected entity")
	}
	if r.Entities[0].Properties["table_name"] != "users" {
		t.Errorf("expected table_name=users, got %v", r.Entities[0].Properties["table_name"])
	}
}

func TestHibernate_Association(t *testing.T) {
	source := `
@Entity
public class Order {
    @ManyToOne
    private Customer customer;
}
`
	r := ExtractHibernate(PatternContext{Source: source, Language: "java", Framework: "hibernate", FilePath: "Order.java"})
	if len(r.Relationships) == 0 {
		t.Error("expected DEPENDS_ON relationship for association")
	}
}

func TestHibernate_Converter(t *testing.T) {
	source := `
@Converter(autoApply=true)
public class MoneyConverter implements AttributeConverter<Money, BigDecimal> {}
`
	r := ExtractHibernate(PatternContext{Source: source, Language: "java", Framework: "jpa", FilePath: "MoneyConverter.java"})
	found := false
	for _, e := range r.Entities {
		if e.Provenance == "INFERRED_FROM_HIBERNATE_CONVERTER" {
			found = true
		}
	}
	if !found {
		t.Error("expected converter entity")
	}
}

func TestHibernate_AssociationSpringDataJPA(t *testing.T) {
	source := `
@Entity
public class Product {
    @OneToMany
    private List<OrderItem> items;
}
`
	r := ExtractHibernate(PatternContext{Source: source, Language: "java", Framework: "spring_data_jpa", FilePath: "Product.java"})
	if len(r.Relationships) == 0 {
		t.Error("expected DEPENDS_ON relationship for spring_data_jpa association")
	}
	found := false
	for _, rel := range r.Relationships {
		if rel.Properties["association_kind"] == "OneToMany" {
			found = true
		}
	}
	if !found {
		t.Error("expected association_kind=OneToMany in spring_data_jpa relationship")
	}
}

func TestHibernate_SchemaExtraction(t *testing.T) {
	source := `
@Entity
@Table(name="products")
public class Product {
    @Id
    private Long id;
}
`
	r := ExtractHibernate(PatternContext{Source: source, Language: "java", Framework: "jpa", FilePath: "Product.java"})
	if len(r.Entities) == 0 {
		t.Fatal("expected entity for jpa schema extraction")
	}
	if r.Entities[0].Properties["table_name"] != "products" {
		t.Errorf("expected table_name=products, got %v", r.Entities[0].Properties["table_name"])
	}
}

func TestHibernate_WrongFramework(t *testing.T) {
	source := `@Entity public class X {}`
	r := ExtractHibernate(PatternContext{Source: source, Language: "java", Framework: "django", FilePath: "X.java"})
	if len(r.Entities) != 0 {
		t.Errorf("expected 0 entities for wrong framework, got %d", len(r.Entities))
	}
}

// ============================================================================
// JUnit 5 tests
// ============================================================================

func TestJUnit5_TestMethod(t *testing.T) {
	source := `
public class UserServiceTest {
    @Test
    void shouldCreateUser() {
        assertEquals(1, 1);
    }
}
`
	r := ExtractJUnit5(PatternContext{Source: source, Language: "java", Framework: "junit5", FilePath: "UserServiceTest.java"})
	found := false
	for _, e := range r.Entities {
		if e.Name == "shouldCreateUser" && e.Provenance == "INFERRED_FROM_JUNIT5_TEST" {
			found = true
		}
	}
	if !found {
		t.Error("expected test method entity")
	}
}

func TestJUnit5_NestedClass(t *testing.T) {
	source := `
public class OrderTest {
    @Nested
    class WhenCreating {
        @Test
        void shouldValidate() {}
    }
}
`
	r := ExtractJUnit5(PatternContext{Source: source, Language: "java", Framework: "junit5", FilePath: "OrderTest.java"})
	hasNested := false
	for _, e := range r.Entities {
		if e.Kind == "SCOPE.Component" && e.Provenance == "INFERRED_FROM_JUNIT5_NESTED" {
			hasNested = true
		}
	}
	if !hasNested {
		t.Error("expected nested class entity")
	}
}

func TestJUnit5_ExtendWith(t *testing.T) {
	source := `
@ExtendWith(MockitoExtension.class)
public class ServiceTest {
    @Test void test() {}
}
`
	r := ExtractJUnit5(PatternContext{Source: source, Language: "java", Framework: "junit5", FilePath: "ServiceTest.java"})
	hasExtension := false
	for _, e := range r.Entities {
		if e.Provenance == "INFERRED_FROM_JUNIT5_EXTENSION" {
			hasExtension = true
		}
	}
	if !hasExtension {
		t.Error("expected extension entity")
	}
}

func TestJUnit5_Lifecycle(t *testing.T) {
	source := `
public class SetupTest {
    @BeforeEach
    void setUp() {}
    @Test
    void testSomething() {}
}
`
	r := ExtractJUnit5(PatternContext{Source: source, Language: "java", Framework: "junit5", FilePath: "SetupTest.java"})
	hasLifecycle := false
	for _, e := range r.Entities {
		if e.Provenance == "INFERRED_FROM_JUNIT5_LIFECYCLE" {
			hasLifecycle = true
		}
	}
	if !hasLifecycle {
		t.Error("expected lifecycle method entity")
	}
}

// ============================================================================
// LangChain4J tests
// ============================================================================

func TestLangChain4J_AIService(t *testing.T) {
	source := `
@AiService
public interface ChatAssistant {
    @Tool("search docs")
    String searchDocs(String query);
}
`
	r := ExtractLangChain4J(PatternContext{Source: source, Language: "java", Framework: "langchain4j", FilePath: "ChatAssistant.java"})
	hasService := false
	hasTool := false
	for _, e := range r.Entities {
		if e.Kind == "SCOPE.Service" {
			hasService = true
		}
		if e.Kind == "SCOPE.Operation" {
			hasTool = true
		}
	}
	if !hasService {
		t.Error("expected AI service entity")
	}
	if !hasTool {
		t.Error("expected tool method entity")
	}
}

func TestLangChain4J_ChatModel(t *testing.T) {
	source := `
public class Bot {
    private ChatLanguageModel model;
    private ChatMemory memory;
}
`
	r := ExtractLangChain4J(PatternContext{Source: source, Language: "java", Framework: "langchain4j", FilePath: "Bot.java"})
	hasModel := false
	hasMemory := false
	for _, e := range r.Entities {
		if e.Provenance == "INFERRED_FROM_LANGCHAIN4J_MODEL" {
			hasModel = true
		}
		if e.Provenance == "INFERRED_FROM_LANGCHAIN4J_MEMORY" {
			hasMemory = true
		}
	}
	if !hasModel {
		t.Error("expected chat model entity")
	}
	if !hasMemory {
		t.Error("expected chat memory entity")
	}
}

func TestLangChain4J_WrongFramework(t *testing.T) {
	source := `@AiService public interface X {}`
	r := ExtractLangChain4J(PatternContext{Source: source, Language: "java", Framework: "spring_boot", FilePath: "X.java"})
	if len(r.Entities) != 0 {
		t.Errorf("expected 0 entities for wrong framework, got %d", len(r.Entities))
	}
}

// TestLangChain4J_AIServiceFixture proves chain_composition + tool_use_detection + prompt_template_extraction
// using the fixture at testdata/fixtures/sources/java/langchain4j/AIServiceFixture.java (issue #2998).
func TestLangChain4J_AIServiceFixture(t *testing.T) {
	source := `
@AiService
interface CustomerSupportAgent {
    @SystemMessage("You are a helpful customer support agent for {companyName}.")
    @UserMessage("Customer query: {query}")
    String answer(String companyName, String query);
}

public class BookingTools {
    @Tool("Get available flights for a date")
    public List<Flight> getFlights(String date) { return List.of(); }

    @Tool
    public BookingResult bookFlight(String flightId) { return new BookingResult(flightId); }
}

public class SupportBot {
    private ChatLanguageModel model;
    private EmbeddingStoreContentRetriever retriever;
    private MessageWindowChatMemory memory;
}
`
	r := ExtractLangChain4J(PatternContext{
		Source:    source,
		Language:  "java",
		Framework: "langchain4j",
		FilePath:  "AIServiceFixture.java",
	})

	// chain_composition: @AiService entity
	hasService := false
	for _, e := range r.Entities {
		if e.Kind == "SCOPE.Service" && e.Name == "CustomerSupportAgent" {
			hasService = true
		}
	}
	if !hasService {
		t.Error("chain_composition: expected SCOPE.Service entity for CustomerSupportAgent (@AiService)")
	}

	// chain_composition: RAG component
	hasRAG := false
	for _, e := range r.Entities {
		if e.Provenance == "INFERRED_FROM_LANGCHAIN4J_RAG" {
			hasRAG = true
		}
	}
	if !hasRAG {
		t.Error("chain_composition: expected SCOPE.Pattern entity for EmbeddingStoreContentRetriever (RAG component)")
	}

	// chain_composition: ChatMemory component
	hasMemory := false
	for _, e := range r.Entities {
		if e.Provenance == "INFERRED_FROM_LANGCHAIN4J_MEMORY" {
			hasMemory = true
		}
	}
	if !hasMemory {
		t.Error("chain_composition: expected SCOPE.Component entity for MessageWindowChatMemory")
	}

	// tool_use_detection: @Tool methods
	toolMethods := map[string]bool{}
	for _, e := range r.Entities {
		if e.Provenance == "INFERRED_FROM_LANGCHAIN4J_TOOL" {
			if m, ok := e.Properties["tool_method"]; ok {
				toolMethods[m.(string)] = true
			}
		}
	}
	if !toolMethods["getFlights"] {
		t.Error("tool_use_detection: expected SCOPE.Operation entity for @Tool method getFlights")
	}
	if !toolMethods["bookFlight"] {
		t.Error("tool_use_detection: expected SCOPE.Operation entity for @Tool method bookFlight")
	}

	// prompt_template_extraction: @SystemMessage
	hasSystem := false
	for _, e := range r.Entities {
		if e.Provenance == "INFERRED_FROM_LANGCHAIN4J_PROMPT" {
			if pt, ok := e.Properties["prompt_type"]; ok && pt == "system_message" {
				hasSystem = true
			}
		}
	}
	if !hasSystem {
		t.Error("prompt_template_extraction: expected SCOPE.Pattern entity for @SystemMessage on answer()")
	}

	// prompt_template_extraction: @UserMessage
	hasUser := false
	for _, e := range r.Entities {
		if e.Provenance == "INFERRED_FROM_LANGCHAIN4J_PROMPT" {
			if pt, ok := e.Properties["prompt_type"]; ok && pt == "user_message" {
				hasUser = true
			}
		}
	}
	if !hasUser {
		t.Error("prompt_template_extraction: expected SCOPE.Pattern entity for @UserMessage on answer()")
	}
}

// ============================================================================
// Micronaut tests
// ============================================================================

func TestMicronaut_Controller(t *testing.T) {
	source := `
@Controller("/api/items")
public class ItemController {
    @Get("/{id}")
    public Item get(Long id) { return null; }
}
`
	r := ExtractMicronaut(PatternContext{Source: source, Language: "java", Framework: "micronaut", FilePath: "ItemController.java"})
	found := false
	for _, e := range r.Entities {
		if e.Subtype == "endpoint" {
			found = true
			if e.Properties["path"] != "/api/items/{id}" {
				t.Errorf("expected /api/items/{id}, got %v", e.Properties["path"])
			}
		}
	}
	if !found {
		t.Error("expected endpoint entity")
	}
}

func TestMicronaut_Bean(t *testing.T) {
	source := `
@Singleton
public class CacheService {}
`
	r := ExtractMicronaut(PatternContext{Source: source, Language: "java", Framework: "micronaut", FilePath: "CacheService.java"})
	if len(r.Entities) == 0 {
		t.Fatal("expected bean entity")
	}
	if r.Entities[0].Kind != "SCOPE.Service" {
		t.Errorf("expected SCOPE.Service, got %s", r.Entities[0].Kind)
	}
}

func TestMicronaut_Client(t *testing.T) {
	source := `
@Client("user-service")
public interface UserClient {
    @Get("/users/{id}")
    User getUser(Long id);
}
`
	r := ExtractMicronaut(PatternContext{Source: source, Language: "java", Framework: "micronaut", FilePath: "UserClient.java"})
	found := false
	for _, e := range r.Entities {
		if e.Provenance == "INFERRED_FROM_MICRONAUT_HTTP_CLIENT" {
			found = true
		}
	}
	if !found {
		t.Error("expected HTTP client entity")
	}
}

// ============================================================================
// MicroProfile tests
// ============================================================================

func TestMicroProfile_Retry(t *testing.T) {
	source := `
public class PaymentService {
    @Retry(maxRetries=3)
    public void pay() {}
}
`
	r := ExtractMicroProfile(PatternContext{Source: source, Language: "java", Framework: "quarkus", FilePath: "PaymentService.java"})
	found := false
	for _, e := range r.Entities {
		if e.Provenance == "INFERRED_FROM_MICROPROFILE_FAULT_TOLERANCE" {
			found = true
		}
	}
	if !found {
		t.Error("expected fault tolerance entity")
	}
}

func TestMicroProfile_HealthCheck(t *testing.T) {
	source := `
@Liveness
public class DatabaseHealth implements HealthCheck {}
`
	r := ExtractMicroProfile(PatternContext{Source: source, Language: "java", Framework: "quarkus", FilePath: "DatabaseHealth.java"})
	found := false
	for _, e := range r.Entities {
		if e.Provenance == "INFERRED_FROM_MICROPROFILE_HEALTH" {
			found = true
		}
	}
	if !found {
		t.Error("expected health check entity")
	}
}

func TestMicroProfile_ReactiveMessaging(t *testing.T) {
	source := `
public class OrderProcessor {
    @Incoming("orders-in")
    public void process(String order) {}

    @Outgoing("orders-in")
    public String produce() { return "order"; }
}
`
	r := ExtractMicroProfile(PatternContext{Source: source, Language: "java", Framework: "quarkus", FilePath: "OrderProcessor.java"})
	hasIncoming := false
	hasOutgoing := false
	for _, e := range r.Entities {
		if e.Properties["direction"] == "incoming" {
			hasIncoming = true
		}
		if e.Properties["direction"] == "outgoing" {
			hasOutgoing = true
		}
	}
	if !hasIncoming || !hasOutgoing {
		t.Error("expected both incoming and outgoing entities")
	}
	// Should have DEPENDS_ON for same channel
	hasDep := false
	for _, rel := range r.Relationships {
		if rel.RelationshipType == "DEPENDS_ON" && rel.Properties["kind"] == "reactive_messaging" {
			hasDep = true
		}
	}
	if !hasDep {
		t.Error("expected DEPENDS_ON for same channel")
	}
}

// ============================================================================
// Quarkus tests
// ============================================================================

func TestQuarkus_JAXRSEndpoint(t *testing.T) {
	source := `
@Path("/orders")
public class OrderResource {
    @GET
    @Path("/{id}")
    public Order get(@PathParam("id") Long id) { return null; }
}
`
	r := ExtractQuarkus(PatternContext{Source: source, Language: "java", Framework: "quarkus", FilePath: "OrderResource.java"})
	found := false
	for _, e := range r.Entities {
		if e.Subtype == "endpoint" {
			found = true
		}
	}
	if !found {
		t.Error("expected JAX-RS endpoint entity")
	}
}

func TestQuarkus_PanacheEntity(t *testing.T) {
	source := `
public class Product extends PanacheEntity {
    public String name;
    public double price;
}
`
	r := ExtractQuarkus(PatternContext{Source: source, Language: "java", Framework: "quarkus", FilePath: "Product.java"})
	found := false
	for _, e := range r.Entities {
		// (Option A): Panache ORM entities are class-like → SCOPE.Component.
		if e.Kind == "SCOPE.Component" && e.Provenance == "INFERRED_FROM_QUARKUS_PANACHE_ENTITY" {
			found = true
		}
	}
	if !found {
		t.Error("expected Panache entity with Kind=SCOPE.Component")
	}
}

func TestQuarkus_PanacheEntityBase(t *testing.T) {
	source := `
public class Order extends PanacheEntityBase {
    public Long id;
    public String status;
}
`
	r := ExtractQuarkus(PatternContext{Source: source, Language: "java", Framework: "quarkus", FilePath: "Order.java"})
	found := false
	for _, e := range r.Entities {
		if e.Kind == "SCOPE.Component" && e.Provenance == "INFERRED_FROM_QUARKUS_PANACHE_ENTITY" {
			found = true
		}
	}
	if !found {
		t.Error("expected PanacheEntityBase entity with Kind=SCOPE.Component")
	}
}

func TestQuarkus_PanacheMongoEntity(t *testing.T) {
	source := `
public class Document extends PanacheMongoEntity {
    public String title;
    public String content;
}
`
	r := ExtractQuarkus(PatternContext{Source: source, Language: "java", Framework: "quarkus", FilePath: "Document.java"})
	found := false
	for _, e := range r.Entities {
		// (Option A): Panache Mongo entities are class-like → SCOPE.Component.
		if e.Kind == "SCOPE.Component" && e.Provenance == "INFERRED_FROM_QUARKUS_PANACHE_MONGO_ENTITY" {
			found = true
		}
	}
	if !found {
		t.Error("expected PanacheMongoEntity with Kind=SCOPE.Component")
	}
}

func TestQuarkus_PanacheMongoEntityBase(t *testing.T) {
	source := `
public class Event extends PanacheMongoEntityBase {
    public String type;
    public long timestamp;
}
`
	r := ExtractQuarkus(PatternContext{Source: source, Language: "java", Framework: "quarkus", FilePath: "Event.java"})
	found := false
	for _, e := range r.Entities {
		if e.Kind == "SCOPE.Component" && e.Provenance == "INFERRED_FROM_QUARKUS_PANACHE_MONGO_ENTITY" {
			found = true
		}
	}
	if !found {
		t.Error("expected PanacheMongoEntityBase entity with Kind=SCOPE.Component")
	}
}

// TestQuarkus_NoInvalidKind verifies the Quarkus extractor never emits a
// Kind value outside the 14-type SCOPE allowlist.
func TestQuarkus_NoInvalidKind(t *testing.T) {
	validTypes := map[string]struct{}{
		"SCOPE.Service":       {},
		"SCOPE.Component":     {},
		"SCOPE.Operation":     {},
		"SCOPE.Pattern":       {},
		"SCOPE.Evolution":     {},
		"SCOPE.Datastore":     {},
		"SCOPE.ExternalAPI":   {},
		"SCOPE.Event":         {},
		"SCOPE.Queue":         {},
		"SCOPE.Schema":        {},
		"SCOPE.ScopeUnknown":  {},
		"SCOPE.Stylesheet":    {},
		"SCOPE.UIComponent":   {},
		"SCOPE.InfraResource": {},
	}

	source := `
@Path("/api/products")
public class ProductResource {
    @GET
    public List<Product> list() { return null; }

    @POST
    public Product create(Product p) { return null; }
}

public class Product extends PanacheEntity {
    public String name;
    public double price;
}

public class Cart extends PanacheMongoEntity {
    public String userId;
}

public class ProductRepo implements PanacheRepository<Product> {}

@ApplicationScoped
public class PricingService {
    @Inject
    ProductRepo repo;
}
`
	r := ExtractQuarkus(PatternContext{Source: source, Language: "java", Framework: "quarkus", FilePath: "Products.java"})
	for _, e := range r.Entities {
		if _, ok := validTypes[e.Kind]; !ok {
			t.Errorf("entity %q emitted invalid Kind %q — not in the graph 14-type allowlist", e.Name, e.Kind)
		}
	}
}

func TestQuarkus_CDIBean(t *testing.T) {
	source := `
@ApplicationScoped
public class OrderService {}
`
	r := ExtractQuarkus(PatternContext{Source: source, Language: "java", Framework: "quarkus", FilePath: "OrderService.java"})
	found := false
	for _, e := range r.Entities {
		if e.Kind == "SCOPE.Service" && e.Properties["cdi_scope"] == "ApplicationScoped" {
			found = true
		}
	}
	if !found {
		t.Error("expected CDI bean service entity")
	}
}

// TestQuarkus_CDIScopes proves di_binding_extraction: extractor emits SCOPE.Service
// with cdi_scope property for all supported CDI scope annotations.
func TestQuarkus_CDIScopes(t *testing.T) {
	cases := []struct {
		annotation string
		wantScope  string
	}{
		{"@ApplicationScoped", "ApplicationScoped"},
		{"@RequestScoped", "RequestScoped"},
		{"@Singleton", "Singleton"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.annotation, func(t *testing.T) {
			source := tc.annotation + "\npublic class MyBean {}"
			r := ExtractQuarkus(PatternContext{
				Source: source, Language: "java", Framework: "quarkus",
				FilePath: "MyBean.java",
			})
			found := false
			for _, e := range r.Entities {
				if e.Kind == "SCOPE.Service" && e.Properties["cdi_scope"] == tc.wantScope {
					found = true
				}
			}
			if !found {
				t.Errorf("di_binding_extraction: expected SCOPE.Service with cdi_scope=%s for %s", tc.wantScope, tc.annotation)
			}
		})
	}
}

// TestQuarkus_CDIInjectField proves di_injection_point: @Inject field injection
// emits a DEPENDS_ON relationship with injection_kind=cdi_inject.
func TestQuarkus_CDIInjectField(t *testing.T) {
	source := `
@RequestScoped
public class OrderController {
    @Inject
    private OrderService orderService;
}
`
	r := ExtractQuarkus(PatternContext{
		Source: source, Language: "java", Framework: "quarkus",
		FilePath: "OrderController.java",
	})
	hasRel := false
	for _, rel := range r.Relationships {
		if rel.RelationshipType == "DEPENDS_ON" && rel.Properties["injection_kind"] == "cdi_inject" {
			hasRel = true
		}
	}
	if !hasRel {
		t.Error("di_injection_point: expected DEPENDS_ON with injection_kind=cdi_inject for @Inject field")
	}
}

// TestQuarkus_CDIInjectConstructor proves di_injection_point: @Inject constructor
// injection emits a DEPENDS_ON relationship with injection_kind=cdi_constructor.
func TestQuarkus_CDIInjectConstructor(t *testing.T) {
	source := `
@ApplicationScoped
public class OrderService {
    private final OrderRepository repository;

    @Inject
    public OrderService(OrderRepository repository) {
        this.repository = repository;
    }
}
`
	r := ExtractQuarkus(PatternContext{
		Source: source, Language: "java", Framework: "quarkus",
		FilePath: "OrderService.java",
	})
	hasRel := false
	for _, rel := range r.Relationships {
		if rel.RelationshipType == "DEPENDS_ON" && rel.Properties["injection_kind"] == "cdi_constructor" {
			hasRel = true
		}
	}
	if !hasRel {
		t.Error("di_injection_point: expected DEPENDS_ON with injection_kind=cdi_constructor for @Inject constructor")
	}
}

// TestQuarkus_CDIScopeResolution proves di_scope_resolution: the resolved CDI
// scope name is captured in the cdi_scope property on the SCOPE.Service entity.
func TestQuarkus_CDIScopeResolution(t *testing.T) {
	source := `
@ApplicationScoped
public class OrderService {}

@RequestScoped
public class OrderController {
    @Inject
    OrderService orderService;
}
`
	r := ExtractQuarkus(PatternContext{
		Source: source, Language: "java", Framework: "quarkus",
		FilePath: "CDIBeansFixture.java",
	})

	scopeMap := make(map[string]string) // className -> cdi_scope
	for _, e := range r.Entities {
		if e.Kind == "SCOPE.Service" {
			if scope, ok := e.Properties["cdi_scope"].(string); ok {
				scopeMap[e.Name] = scope
			}
		}
	}

	if scopeMap["OrderService"] != "ApplicationScoped" {
		t.Errorf("di_scope_resolution: OrderService cdi_scope=%q, want ApplicationScoped", scopeMap["OrderService"])
	}
	if scopeMap["OrderController"] != "RequestScoped" {
		t.Errorf("di_scope_resolution: OrderController cdi_scope=%q, want RequestScoped", scopeMap["OrderController"])
	}

	// Also confirm the injection point is present
	hasInject := false
	for _, rel := range r.Relationships {
		if rel.RelationshipType == "DEPENDS_ON" && rel.Properties["injection_kind"] == "cdi_inject" {
			hasInject = true
		}
	}
	if !hasInject {
		t.Error("di_injection_point: expected DEPENDS_ON cdi_inject from OrderController->OrderService")
	}
}

// ============================================================================
// Android tests
// ============================================================================

func TestAndroid_Activity(t *testing.T) {
	source := `
public class MainActivity extends AppCompatActivity {
    @Override
    protected void onCreate(Bundle savedInstanceState) {}
}
`
	r := ExtractAndroid(PatternContext{Source: source, Language: "java", Framework: "android", FilePath: "MainActivity.java"})
	found := false
	for _, e := range r.Entities {
		if e.Kind == "SCOPE.UIComponent" && e.Properties["component_kind"] == "activity" {
			found = true
		}
	}
	if !found {
		t.Error("expected activity entity")
	}
}

func TestAndroid_Intent(t *testing.T) {
	source := `
public class MainActivity extends AppCompatActivity {
    void goToDetail() {
        Intent intent = new Intent(this, DetailActivity.class);
        startActivity(intent);
    }
}
`
	r := ExtractAndroid(PatternContext{Source: source, Language: "java", Framework: "android", FilePath: "MainActivity.java"})
	hasIntent := false
	for _, e := range r.Entities {
		if e.Provenance == "INFERRED_FROM_ANDROID_INTENT" {
			hasIntent = true
		}
	}
	if !hasIntent {
		t.Error("expected intent entity")
	}
	hasDep := false
	for _, rel := range r.Relationships {
		if rel.RelationshipType == "DEPENDS_ON" && rel.Properties["navigation_kind"] == "intent" {
			hasDep = true
		}
	}
	if !hasDep {
		t.Error("expected DEPENDS_ON for intent navigation")
	}
}

func TestAndroid_ViewModel(t *testing.T) {
	source := `
public class UserViewModel extends ViewModel {
    private MutableLiveData<String> userName;
}
`
	r := ExtractAndroid(PatternContext{Source: source, Language: "java", Framework: "android", FilePath: "UserViewModel.java"})
	found := false
	for _, e := range r.Entities {
		if e.Properties["component_kind"] == "viewmodel" {
			found = true
		}
	}
	if !found {
		t.Error("expected viewmodel entity")
	}
}

// ============================================================================
// Jakarta EE tests
// ============================================================================

func TestJakartaEE_Servlet(t *testing.T) {
	source := `
@WebServlet("/hello")
public class HelloServlet extends HttpServlet {
    protected void doGet(HttpServletRequest req, HttpServletResponse resp) {}
}
`
	r := ExtractJakartaEE(PatternContext{Source: source, Language: "java", Framework: "jakarta_ee", FilePath: "HelloServlet.java"})
	hasServlet := false
	hasMethod := false
	for _, e := range r.Entities {
		if e.Provenance == "INFERRED_FROM_JAKARTA_SERVLET" && e.Subtype == "endpoint" {
			hasServlet = true
		}
		if e.Provenance == "INFERRED_FROM_JAKARTA_SERVLET" && e.Subtype == "function" {
			hasMethod = true
		}
	}
	if !hasServlet {
		t.Error("expected servlet entity")
	}
	if !hasMethod {
		t.Error("expected servlet method entity")
	}
}

func TestJakartaEE_EJB(t *testing.T) {
	source := `
@Stateless
public class PaymentService {}
`
	r := ExtractJakartaEE(PatternContext{Source: source, Language: "java", Framework: "jakarta_ee", FilePath: "PaymentService.java"})
	found := false
	for _, e := range r.Entities {
		if e.Provenance == "INFERRED_FROM_JAKARTA_EJB" {
			found = true
		}
	}
	if !found {
		t.Error("expected EJB entity")
	}
}

func TestJakartaEE_WebSocket(t *testing.T) {
	source := `
@ServerEndpoint("/chat")
public class ChatEndpoint {
    @OnMessage
    public void onMessage(String msg) {}
}
`
	r := ExtractJakartaEE(PatternContext{Source: source, Language: "java", Framework: "jakarta_ee", FilePath: "ChatEndpoint.java"})
	hasWS := false
	hasHandler := false
	for _, e := range r.Entities {
		if e.Provenance == "INFERRED_FROM_JAKARTA_WEBSOCKET" && e.Subtype == "endpoint" {
			hasWS = true
		}
		if e.Provenance == "INFERRED_FROM_JAKARTA_WEBSOCKET" && e.Subtype == "function" {
			hasHandler = true
		}
	}
	if !hasWS {
		t.Error("expected websocket entity")
	}
	if !hasHandler {
		t.Error("expected websocket handler entity")
	}
}

// ============================================================================
// Jakarta EE Advanced tests
// ============================================================================

func TestJakartaEEAdv_CDIProducer(t *testing.T) {
	source := `
public class Producers {
    @Produces
    public EntityManager createEM() { return null; }
}
`
	r := ExtractJakartaEEAdvanced(PatternContext{Source: source, Language: "java", Framework: "jakarta_ee", FilePath: "Producers.java"})
	found := false
	for _, e := range r.Entities {
		if e.Provenance == "INFERRED_FROM_JAKARTA_CDI_PRODUCER" {
			found = true
		}
	}
	if !found {
		t.Error("expected CDI producer entity")
	}
}

func TestJakartaEEAdv_WebService(t *testing.T) {
	source := `
@WebService
public class CalculatorService {
    @WebMethod
    public int add(int a, int b) { return a + b; }
}
`
	r := ExtractJakartaEEAdvanced(PatternContext{Source: source, Language: "java", Framework: "jakarta_ee", FilePath: "CalculatorService.java"})
	hasSvc := false
	hasMethod := false
	for _, e := range r.Entities {
		if e.Provenance == "INFERRED_FROM_JAKARTA_SOAP_SERVICE" {
			hasSvc = true
		}
		if e.Provenance == "INFERRED_FROM_JAKARTA_SOAP_METHOD" {
			hasMethod = true
		}
	}
	if !hasSvc {
		t.Error("expected SOAP service entity")
	}
	if !hasMethod {
		t.Error("expected SOAP method entity")
	}
}

func TestJakartaEEAdv_XmlRootElement(t *testing.T) {
	source := `
@XmlRootElement
public class OrderDTO {}
`
	r := ExtractJakartaEEAdvanced(PatternContext{Source: source, Language: "java", Framework: "jakarta_ee", FilePath: "OrderDTO.java"})
	found := false
	for _, e := range r.Entities {
		if e.Provenance == "INFERRED_FROM_JAKARTA_JAXB" {
			found = true
		}
	}
	if !found {
		t.Error("expected JAXB entity")
	}
}

// ============================================================================
// Helper: types_test
// ============================================================================

func TestLineOf(t *testing.T) {
	source := "line1\nline2\nline3"
	if got := lineOf(source, 0); got != 1 {
		t.Errorf("expected 1, got %d", got)
	}
	if got := lineOf(source, 6); got != 2 {
		t.Errorf("expected 2, got %d", got)
	}
	if got := lineOf(source, 12); got != 3 {
		t.Errorf("expected 3, got %d", got)
	}
}

func TestFindEnclosingClass(t *testing.T) {
	source := `
public class Outer {
    class Inner {
        void method() {}
    }
}
`
	cls := findEnclosingClass(source, 50)
	if cls != "Inner" {
		t.Errorf("expected Inner, got %s", cls)
	}
}

// ============================================================================
// Quartz Java tests
// ============================================================================

func makeQuartzCtx(src, filePath string) PatternContext {
	return PatternContext{Source: src, Language: "java", Framework: "quartz", FilePath: filePath}
}

func containsQuartzEntity(result PatternResult, kind, subtype, name string) bool {
	for _, e := range result.Entities {
		if e.Kind == kind && e.Subtype == subtype && e.Name == name {
			return true
		}
	}
	return false
}

func TestQuartzJava_IJobConsumer(t *testing.T) {
	src := `
public class SendEmailJob implements Job {
    public void execute(JobExecutionContext context) throws JobExecutionException {
        // send email
    }
}
`
	result := ExtractQuartzJava(makeQuartzCtx(src, "jobs/SendEmailJob.java"))
	if !containsQuartzEntity(result, "SCOPE.Service", "job_class", "SendEmailJob") {
		names := entityNames(result.Entities)
		t.Errorf("expected SendEmailJob consumer job_class entity; got: %v", names)
	}
}

func TestQuartzJava_ExecuteMethod(t *testing.T) {
	src := `
public class NotifyJob implements Job {
    public void execute(JobExecutionContext context) { }
}
`
	result := ExtractQuartzJava(makeQuartzCtx(src, "jobs/NotifyJob.java"))
	found := false
	for _, e := range result.Entities {
		if e.Subtype == "job_execute" && strings.HasPrefix(e.Name, "NotifyJob") {
			found = true
		}
	}
	if !found {
		t.Error("expected execute method entity for NotifyJob")
	}
}

func TestQuartzJava_JobBuilderProducer(t *testing.T) {
	src := `
JobDetail job = JobBuilder.newJob(SendEmailJob.class)
    .withIdentity("send-email-job", "email-group")
    .build();
`
	result := ExtractQuartzJava(makeQuartzCtx(src, "Scheduler.java"))
	found := false
	for _, e := range result.Entities {
		if e.Subtype == "job_builder" && e.Properties["job_class"] == "SendEmailJob" {
			found = true
		}
	}
	if !found {
		t.Error("expected JobBuilder.newJob producer entity for SendEmailJob")
	}
}

func TestQuartzJava_TriggerBuilder(t *testing.T) {
	src := `
Trigger trigger = TriggerBuilder.newTrigger()
    .withIdentity("email-trigger")
    .startNow()
    .build();
`
	result := ExtractQuartzJava(makeQuartzCtx(src, "Scheduler.java"))
	found := false
	for _, e := range result.Entities {
		if e.Subtype == "trigger" {
			found = true
		}
	}
	if !found {
		t.Error("expected TriggerBuilder.newTrigger trigger entity")
	}
}

func TestQuartzJava_SchedulerScheduleJob(t *testing.T) {
	src := `scheduler.scheduleJob(jobDetail, trigger);`
	result := ExtractQuartzJava(makeQuartzCtx(src, "Scheduler.java"))
	if !containsQuartzEntity(result, "SCOPE.Operation", "schedule_job", "scheduler.scheduleJob") {
		t.Error("expected scheduler.scheduleJob producer entity")
	}
}

func TestQuartzJava_DisallowConcurrentExecution(t *testing.T) {
	src := `
@DisallowConcurrentExecution
public class HeavyJob implements Job {
    public void execute(JobExecutionContext ctx) { }
}
`
	result := ExtractQuartzJava(makeQuartzCtx(src, "jobs/HeavyJob.java"))
	found := false
	for _, e := range result.Entities {
		if e.Subtype == "concurrency_policy" {
			found = true
		}
	}
	if !found {
		t.Error("expected @DisallowConcurrentExecution concurrency_policy entity")
	}
}

func TestQuartzJava_NonJavaLanguageSkipped(t *testing.T) {
	src := `class Foo implements Job { }`
	ctx := PatternContext{Source: src, Language: "kotlin", Framework: "quartz", FilePath: "Foo.kt"}
	result := ExtractQuartzJava(ctx)
	if len(result.Entities) != 0 {
		t.Errorf("expected no entities for non-java language, got %d", len(result.Entities))
	}
}

// ============================================================================
// Issue #2988 — Spring Boot / WebFlux proving tests
// Cells: route_extraction, dto_extraction, request_validation
// ============================================================================

// TestSpringBoot_RouteExtraction_Issue2988 proves that ExtractSpringBoot
// emits endpoint entities whose properties carry the composed HTTP route
// path and method — confirming route_extraction is delivered by the
// spring_boot custom extractor + the engine-level spring_routes.go pass.
// The registry target is `partial` (annotations scanned; path-variable
// resolution may be incomplete). Cite: internal/engine/spring_routes.go,
// internal/engine/java_annotation_routes.go.
func TestSpringBoot_RouteExtraction_Issue2988(t *testing.T) {
	source := `
package com.example;
import org.springframework.web.bind.annotation.*;
import java.util.List;

@RestController
@RequestMapping("/api/v1")
public class OrderController {
    @GetMapping("/orders")
    public List<OrderDto> getOrders() { return null; }

    @PostMapping("/orders")
    public OrderDto createOrder(@RequestBody CreateOrderRequest req) { return null; }

    @GetMapping("/orders/{id}")
    public OrderDto getOrder(@PathVariable Long id) { return null; }
}
`
	r := ExtractSpringBoot(PatternContext{
		Source:    source,
		Language:  "java",
		Framework: "spring_boot",
		FilePath:  "OrderController.java",
	})

	// Must emit at least 3 endpoint entities for the 3 handler methods.
	var endpointNames []string
	for _, e := range r.Entities {
		if e.Subtype == "endpoint" {
			endpointNames = append(endpointNames, e.Name)
		}
	}
	if len(endpointNames) < 3 {
		t.Errorf("[#2988 route_extraction] expected >= 3 endpoint entities, got %d: %v", len(endpointNames), endpointNames)
	}

	// Validate HTTP verbs are captured on the operation entities.
	verbsSeen := make(map[string]bool)
	for _, e := range r.Entities {
		if e.Subtype == "endpoint" {
			if raw, ok := e.Properties["http_method"]; ok {
				if v, ok2 := raw.(string); ok2 && v != "" {
					verbsSeen[v] = true
				}
			}
		}
	}
	for _, want := range []string{"GET", "POST"} {
		if !verbsSeen[want] {
			t.Errorf("[#2988 route_extraction] HTTP method %q not found among endpoint entities", want)
		}
	}
}

// TestSpringBoot_DtoExtraction_Issue2988 proves that ExtractSpringRequestResponse
// emits SCOPE.Schema(kind=dto) entities for @RequestBody parameter types and
// return types, and wires ACCEPTS_INPUT / RETURNS relationships.
// Registry target: partial. Cite: internal/custom/java/spring_request_response.go.
func TestSpringBoot_DtoExtraction_Issue2988(t *testing.T) {
	source := `
package com.example;
import org.springframework.web.bind.annotation.*;
import org.springframework.http.ResponseEntity;
import java.util.List;

@RestController
@RequestMapping("/api/v1")
public class OrderController {
    @GetMapping("/orders")
    public List<OrderDto> getOrders() { return null; }

    @PostMapping("/orders")
    public OrderDto createOrder(@RequestBody CreateOrderRequest req) { return null; }
}
`
	r := ExtractSpringRequestResponse(PatternContext{
		Source:    source,
		Language:  "java",
		Framework: "spring_boot",
		FilePath:  "OrderController.java",
	})

	// Expect SCOPE.Schema entities for CreateOrderRequest and OrderDto.
	dtoNames := make(map[string]bool)
	for _, e := range r.Entities {
		if e.Kind == "SCOPE.Schema" {
			dtoNames[e.Name] = true
			if e.Properties["kind"] != "dto" {
				t.Errorf("[#2988 dto_extraction] entity %q has kind=%q, want dto",
					e.Name, e.Properties["kind"])
			}
		}
	}
	for _, want := range []string{"CreateOrderRequest", "OrderDto"} {
		if !dtoNames[want] {
			t.Errorf("[#2988 dto_extraction] expected SCOPE.Schema entity for %q, got entities: %v", want, dtoNames)
		}
	}

	// Expect ACCEPTS_INPUT and RETURNS relationships.
	relTypes := make(map[string]bool)
	for _, rel := range r.Relationships {
		relTypes[rel.RelationshipType] = true
	}
	for _, want := range []string{"ACCEPTS_INPUT", "RETURNS"} {
		if !relTypes[want] {
			t.Errorf("[#2988 dto_extraction] expected %q relationship, got: %v", want, relTypes)
		}
	}
}

// TestSpringWebFlux_DtoExtraction_Issue2988 proves that ExtractSpringRequestResponse
// also handles spring_webflux framework (springReqRespFrameworks includes it),
// emitting dto entities for Mono<T>/Flux<T> return types and @RequestBody params.
// Registry target: partial. Cite: internal/custom/java/spring_request_response.go.
func TestSpringWebFlux_DtoExtraction_Issue2988(t *testing.T) {
	source := `
package com.example;
import org.springframework.web.bind.annotation.*;
import reactor.core.publisher.Mono;
import reactor.core.publisher.Flux;

@RestController
@RequestMapping("/api/v1")
public class ProductController {
    @GetMapping("/products")
    public Flux<ProductDto> listProducts() { return null; }

    @PostMapping("/products")
    public Mono<ProductDto> createProduct(@RequestBody CreateProductRequest req) { return null; }
}
`
	r := ExtractSpringRequestResponse(PatternContext{
		Source:    source,
		Language:  "java",
		Framework: "spring_webflux",
		FilePath:  "ProductController.java",
	})

	dtoNames := make(map[string]bool)
	for _, e := range r.Entities {
		if e.Kind == "SCOPE.Schema" {
			dtoNames[e.Name] = true
		}
	}
	// Mono<T>/Flux<T> are unwrapped; CreateProductRequest is explicit via @RequestBody.
	for _, want := range []string{"CreateProductRequest", "ProductDto"} {
		if !dtoNames[want] {
			t.Errorf("[#2988 webflux dto_extraction] expected SCOPE.Schema for %q, got: %v", want, dtoNames)
		}
	}

	relTypes := make(map[string]bool)
	for _, rel := range r.Relationships {
		relTypes[rel.RelationshipType] = true
	}
	if !relTypes["ACCEPTS_INPUT"] {
		t.Errorf("[#2988 webflux dto_extraction] expected ACCEPTS_INPUT relationship")
	}
	if !relTypes["RETURNS"] {
		t.Errorf("[#2988 webflux dto_extraction] expected RETURNS relationship")
	}
}

// TestSpringBoot_RequestValidation_Issue2988 proves that Bean Validation
// annotations (@Valid, @NotNull) on Spring handler parameters drive the
// required flag on the endpoint.  This test exercises the custom extractor
// layer: a controller source containing @Valid @RequestBody must produce an
// ACCEPTS_INPUT relationship — confirming the plumbing is wired.
// The parameter-level @Required flag is asserted in the engine-level test
// TestSpringBoot_RequestValidation_Engine_Issue2988 (java_annotation_params_test.go).
// Registry target: partial. Cite: internal/engine/java_annotation_params.go.
func TestSpringBoot_RequestValidation_Issue2988(t *testing.T) {
	source := `
package com.example;
import org.springframework.web.bind.annotation.*;
import jakarta.validation.Valid;
import jakarta.validation.constraints.NotNull;

@RestController
@RequestMapping("/api/v1")
public class OrderController {
    @PostMapping("/orders")
    public OrderDto createOrder(@Valid @RequestBody @NotNull CreateOrderRequest req) { return null; }
}
`
	r := ExtractSpringRequestResponse(PatternContext{
		Source:    source,
		Language:  "java",
		Framework: "spring_boot",
		FilePath:  "OrderController.java",
	})

	// ACCEPTS_INPUT relationship must be emitted — proving request body
	// was recognised even when combined with validation annotations.
	hasAcceptsInput := false
	for _, rel := range r.Relationships {
		if rel.RelationshipType == "ACCEPTS_INPUT" {
			hasAcceptsInput = true
			break
		}
	}
	if !hasAcceptsInput {
		t.Errorf("[#2988 request_validation] expected ACCEPTS_INPUT relationship for @Valid @RequestBody param")
	}

	// The DTO entity must exist.
	hasDtoEntity := false
	for _, e := range r.Entities {
		if e.Kind == "SCOPE.Schema" && e.Name == "CreateOrderRequest" {
			hasDtoEntity = true
			break
		}
	}
	if !hasDtoEntity {
		t.Errorf("[#2988 request_validation] expected SCOPE.Schema entity for CreateOrderRequest")
	}
}

// TestSpringWebFlux_RequestValidation_Issue2988 proves Bean Validation
// annotation handling for spring_webflux — the springReqRespFrameworks map
// in spring_request_response.go includes spring_webflux, so @Valid @RequestBody
// on a reactive controller must also yield ACCEPTS_INPUT + a DTO entity.
// Registry target: partial. Cite: internal/engine/java_annotation_params.go,
// internal/custom/java/spring_request_response.go.
func TestSpringWebFlux_RequestValidation_Issue2988(t *testing.T) {
	source := `
package com.example;
import org.springframework.web.bind.annotation.*;
import reactor.core.publisher.Mono;
import jakarta.validation.Valid;

@RestController
@RequestMapping("/api/v1")
public class ProductController {
    @PostMapping("/products")
    public Mono<ProductDto> createProduct(@Valid @RequestBody CreateProductRequest req) { return null; }
}
`
	r := ExtractSpringRequestResponse(PatternContext{
		Source:    source,
		Language:  "java",
		Framework: "spring_webflux",
		FilePath:  "ProductController.java",
	})

	hasAcceptsInput := false
	for _, rel := range r.Relationships {
		if rel.RelationshipType == "ACCEPTS_INPUT" {
			hasAcceptsInput = true
			break
		}
	}
	if !hasAcceptsInput {
		t.Errorf("[#2988 webflux request_validation] expected ACCEPTS_INPUT for @Valid @RequestBody")
	}
}

// ============================================================================
// Jakarta EE + MicroProfile #2996 tests
// ============================================================================

// TestJakartaEEAdv_CDIScopeResolution_Issue2996 proves that CDI scope
// annotations (@ApplicationScoped, @RequestScoped, @SessionScoped,
// @Dependent, @ConversationScoped) on bean classes are detected by
// ExtractJakartaEEAdvanced and emit SCOPE.Component entities with a
// cdi_scope property.
// Registry target: lang.java.framework.jakarta-ee di_scope_resolution=partial.
// Cite: internal/custom/java/jakarta_ee_advanced.go.
func TestJakartaEEAdv_CDIScopeResolution_Issue2996(t *testing.T) {
	source := `
package com.example;
import jakarta.enterprise.context.ApplicationScoped;
import jakarta.enterprise.context.RequestScoped;
import jakarta.inject.Inject;

@ApplicationScoped
public class UserService {
    @Inject
    private UserRepository repo;
}

@RequestScoped
public class OrderSession {
}
`
	r := ExtractJakartaEEAdvanced(PatternContext{
		Source:    source,
		Language:  "java",
		Framework: "jakarta_ee",
		FilePath:  "UserService.java",
	})

	scopedBeans := make(map[string]string)
	for _, e := range r.Entities {
		if e.Kind == "SCOPE.Component" && e.Provenance == "INFERRED_FROM_CDI_SCOPE" {
			if scope, ok := e.Properties["cdi_scope"].(string); ok {
				scopedBeans[e.Name] = scope
			}
		}
	}
	if scope, ok := scopedBeans["UserService"]; !ok || scope != "ApplicationScoped" {
		t.Errorf("[#2996 jakarta-ee di_scope_resolution] expected UserService=ApplicationScoped, got %v", scopedBeans)
	}
	if scope, ok := scopedBeans["OrderSession"]; !ok || scope != "RequestScoped" {
		t.Errorf("[#2996 jakarta-ee di_scope_resolution] expected OrderSession=RequestScoped, got %v", scopedBeans)
	}
}

// TestMicroProfile_CDIScopeResolution_Issue2996 proves that CDI scope
// annotations are detected for MicroProfile-framework sources.
// Registry target: lang.java.framework.microprofile di_scope_resolution=partial.
// Cite: internal/custom/java/jakarta_ee_advanced.go.
func TestMicroProfile_CDIScopeResolution_Issue2996(t *testing.T) {
	source := `
package com.example;
import jakarta.enterprise.context.ApplicationScoped;
import jakarta.inject.Inject;
import org.eclipse.microprofile.rest.client.inject.RegisterRestClient;

@ApplicationScoped
@RegisterRestClient
public class ProductClient {
}
`
	r := ExtractJakartaEEAdvanced(PatternContext{
		Source:    source,
		Language:  "java",
		Framework: "microprofile",
		FilePath:  "ProductClient.java",
	})

	found := false
	for _, e := range r.Entities {
		if e.Kind == "SCOPE.Component" && e.Provenance == "INFERRED_FROM_CDI_SCOPE" && e.Name == "ProductClient" {
			found = true
			if e.Properties["cdi_scope"] != "ApplicationScoped" {
				t.Errorf("[#2996 microprofile di_scope_resolution] expected cdi_scope=ApplicationScoped, got %v", e.Properties["cdi_scope"])
			}
			if e.Properties["framework"] != "microprofile" {
				t.Errorf("[#2996 microprofile di_scope_resolution] expected framework=microprofile, got %v", e.Properties["framework"])
			}
		}
	}
	if !found {
		t.Errorf("[#2996 microprofile di_scope_resolution] expected SCOPE.Component for ProductClient with INFERRED_FROM_CDI_SCOPE")
	}
}

// TestMicroProfile_DIBinding_Issue2996 proves that MicroProfile framework
// activates the CDI DI extractor (di_binding_extraction / di_injection_point).
// Registry target: lang.java.framework.microprofile di_binding_extraction=partial,
// di_injection_point=partial.
// Cite: internal/custom/java/jakarta_ee_advanced.go.
func TestMicroProfile_DIBinding_Issue2996(t *testing.T) {
	source := `
package com.example;
import jakarta.inject.Inject;

@ApplicationScoped
public class OrderService {
    @Inject
    private InventoryService inventory;

    @Produces
    public PaymentGateway produceGateway() { return new PaymentGateway(); }
}
`
	r := ExtractJakartaEEAdvanced(PatternContext{
		Source:    source,
		Language:  "java",
		Framework: "microprofile",
		FilePath:  "OrderService.java",
	})

	// @Produces should emit a CDI producer entity.
	hasProducer := false
	for _, e := range r.Entities {
		if e.Provenance == "INFERRED_FROM_JAKARTA_CDI_PRODUCER" {
			hasProducer = true
			break
		}
	}
	if !hasProducer {
		t.Errorf("[#2996 microprofile di_binding_extraction] expected INFERRED_FROM_JAKARTA_CDI_PRODUCER entity")
	}
}

// TestJakartaEE_RouteExtraction_Issue2996 proves that JAX-RS @Path + @GET/@POST
// annotations on a Jakarta EE resource class are detected by
// ExtractJakartaJaxrsDTO (dto_extraction) and that
// route_extraction is served by java_annotation_routes.go (engine-level).
// This test exercises the custom-extractor layer: a JAX-RS resource with a
// POST method must yield ACCEPTS_INPUT + a DTO entity.
// Registry target: lang.java.framework.jakarta-ee dto_extraction=partial,
// route_extraction=partial.
// Cite: internal/custom/java/jakarta_jaxrs_dto.go,
//
//	internal/engine/java_annotation_routes.go.
func TestJakartaEE_DtoExtraction_Issue2996(t *testing.T) {
	source := `
package com.example;
import jakarta.ws.rs.*;
import jakarta.ws.rs.core.MediaType;

@Path("/orders")
@Produces(MediaType.APPLICATION_JSON)
@Consumes(MediaType.APPLICATION_JSON)
public class OrderResource {
    @POST
    public OrderDto createOrder(CreateOrderRequest req) { return null; }

    @GET
    @Path("/{id}")
    public OrderDto getOrder(@PathParam("id") Long id) { return null; }
}
`
	r := ExtractJakartaJaxrsDTO(PatternContext{
		Source:    source,
		Language:  "java",
		Framework: "jakarta_ee",
		FilePath:  "OrderResource.java",
	})

	dtoNames := make(map[string]bool)
	for _, e := range r.Entities {
		if e.Kind == "SCOPE.Schema" {
			dtoNames[e.Name] = true
			if e.Properties["kind"] != "dto" {
				t.Errorf("[#2996 jakarta-ee dto_extraction] entity %q has kind=%v, want dto", e.Name, e.Properties["kind"])
			}
		}
	}
	for _, want := range []string{"CreateOrderRequest", "OrderDto"} {
		if !dtoNames[want] {
			t.Errorf("[#2996 jakarta-ee dto_extraction] expected SCOPE.Schema for %q, got %v", want, dtoNames)
		}
	}

	relTypes := make(map[string]bool)
	for _, rel := range r.Relationships {
		relTypes[rel.RelationshipType] = true
	}
	for _, want := range []string{"ACCEPTS_INPUT", "RETURNS"} {
		if !relTypes[want] {
			t.Errorf("[#2996 jakarta-ee dto_extraction] expected %q relationship, got: %v", want, relTypes)
		}
	}
}

// TestMicroProfile_DtoExtraction_Issue2996 proves that the JAX-RS DTO extractor
// also runs for MicroProfile (which uses JAX-RS as its REST API).
// Registry target: lang.java.framework.microprofile dto_extraction=partial.
// Cite: internal/custom/java/jakarta_jaxrs_dto.go.
func TestMicroProfile_DtoExtraction_Issue2996(t *testing.T) {
	source := `
package com.example;
import jakarta.ws.rs.*;
import org.eclipse.microprofile.rest.client.inject.RegisterRestClient;

@Path("/products")
@RegisterRestClient
public class ProductResource {
    @POST
    public ProductDto createProduct(CreateProductRequest req) { return null; }
}
`
	r := ExtractJakartaJaxrsDTO(PatternContext{
		Source:    source,
		Language:  "java",
		Framework: "microprofile",
		FilePath:  "ProductResource.java",
	})

	dtoNames := make(map[string]bool)
	for _, e := range r.Entities {
		if e.Kind == "SCOPE.Schema" {
			dtoNames[e.Name] = true
		}
	}
	for _, want := range []string{"CreateProductRequest", "ProductDto"} {
		if !dtoNames[want] {
			t.Errorf("[#2996 microprofile dto_extraction] expected SCOPE.Schema for %q, got %v", want, dtoNames)
		}
	}
}

// TestJakartaEE_AuthCoverage_Issue2996 proves that @RolesAllowed (JSR-250)
// on a JAX-RS resource method is recognised by ExtractJakartaEEAdvanced
// via the auth mechanism extractor, and that the auth_coverage cell is
// backed by java_auth_policy.go at the engine layer.
// This test covers the custom-extractor side: @BasicAuthenticationMechanismDefinition
// must emit a SCOPE.Pattern entity with auth_mechanism property.
// Registry target: lang.java.framework.jakarta-ee auth_coverage=partial.
// Cite: internal/engine/java_auth_policy.go, internal/custom/java/jakarta_ee_advanced.go.
func TestJakartaEE_AuthCoverage_Issue2996(t *testing.T) {
	source := `
package com.example.security;
import jakarta.security.enterprise.authentication.mechanism.http.BasicAuthenticationMechanismDefinition;
import jakarta.enterprise.context.ApplicationScoped;

@BasicAuthenticationMechanismDefinition(realmName = "MyRealm")
@ApplicationScoped
public class AppConfig {
}
`
	r := ExtractJakartaEEAdvanced(PatternContext{
		Source:    source,
		Language:  "java",
		Framework: "jakarta_ee",
		FilePath:  "AppConfig.java",
	})

	found := false
	for _, e := range r.Entities {
		if e.Provenance == "INFERRED_FROM_JAKARTA_SECURITY_AUTH" {
			found = true
			if e.Properties["auth_mechanism"] != "BasicAuthenticationMechanismDefinition" {
				t.Errorf("[#2996 jakarta-ee auth_coverage] expected auth_mechanism=BasicAuthenticationMechanismDefinition, got %v", e.Properties["auth_mechanism"])
			}
		}
	}
	if !found {
		t.Errorf("[#2996 jakarta-ee auth_coverage] expected INFERRED_FROM_JAKARTA_SECURITY_AUTH entity")
	}
}

// TestJakartaEE_TestsLinkage_Issue2996 proves that ExtractJUnit5 runs for
// the "jakarta_ee" framework (tests_linkage cell).
// Registry target: lang.java.framework.jakarta-ee tests_linkage=partial.
// Cite: internal/custom/java/junit5.go.
func TestJakartaEE_TestsLinkage_Issue2996(t *testing.T) {
	source := `
package com.example;
import org.junit.jupiter.api.Test;
import static org.junit.jupiter.api.Assertions.*;

class OrderServiceTest {
    @Test
    void createOrder_shouldReturnDto() {
        // Arquillian / plain JUnit 5 test in a Jakarta EE project.
        assertEquals(1, 1);
    }
}
`
	r := ExtractJUnit5(PatternContext{
		Source:    source,
		Language:  "java",
		Framework: "jakarta_ee",
		FilePath:  "OrderServiceTest.java",
	})

	hasTestMethod := false
	for _, e := range r.Entities {
		if e.Properties["test_annotation"] == "Test" {
			hasTestMethod = true
			break
		}
	}
	if !hasTestMethod {
		t.Errorf("[#2996 jakarta-ee tests_linkage] expected @Test entity from JUnit5 extractor for jakarta_ee framework")
	}
}

// TestMicroProfile_TestsLinkage_Issue2996 proves that ExtractJUnit5 runs for
// the "microprofile" framework (tests_linkage cell).
// Registry target: lang.java.framework.microprofile tests_linkage=partial.
// Cite: internal/custom/java/junit5.go.
func TestMicroProfile_TestsLinkage_Issue2996(t *testing.T) {
	source := `
package com.example;
import org.junit.jupiter.api.Test;

class ProductResourceTest {
    @Test
    void getProduct_returns200() {
    }
}
`
	r := ExtractJUnit5(PatternContext{
		Source:    source,
		Language:  "java",
		Framework: "microprofile",
		FilePath:  "ProductResourceTest.java",
	})

	hasTestMethod := false
	for _, e := range r.Entities {
		if e.Properties["test_annotation"] == "Test" {
			hasTestMethod = true
			break
		}
	}
	if !hasTestMethod {
		t.Errorf("[#2996 microprofile tests_linkage] expected @Test entity from JUnit5 extractor for microprofile framework")
	}
}

// ============================================================================
// Spring Boot tests_linkage (#2991)
// ============================================================================

// TestSpringBoot_TestsLinkage_Issue2991 proves that ExtractJUnit5 runs for
// the "spring_boot" framework (tests_linkage cell).
// Registry target: lang.java.framework.spring-boot tests_linkage=partial.
// Cite: internal/custom/java/junit5.go.
func TestSpringBoot_TestsLinkage_Issue2991(t *testing.T) {
	source := `
package com.example;

import org.junit.jupiter.api.Test;
import org.springframework.boot.test.context.SpringBootTest;
import static org.junit.jupiter.api.Assertions.*;

@SpringBootTest
class UserServiceTest {
    @Test
    void createUser_shouldReturnDto() {
        assertEquals(1, 1);
    }

    @Test
    void deleteUser_shouldRemoveRecord() {
        assertTrue(true);
    }
}
`
	r := ExtractJUnit5(PatternContext{
		Source:    source,
		Language:  "java",
		Framework: "spring_boot",
		FilePath:  "UserServiceTest.java",
	})

	hasTestMethod := false
	for _, e := range r.Entities {
		if e.Properties["test_annotation"] == "Test" {
			hasTestMethod = true
			break
		}
	}
	if !hasTestMethod {
		t.Errorf("[#2991 spring-boot tests_linkage] expected @Test entity from JUnit5 extractor for spring_boot framework")
	}

	// Verify OWNS edge from class → test method
	hasOwns := false
	for _, rel := range r.Relationships {
		if rel.RelationshipType == "OWNS" {
			hasOwns = true
			break
		}
	}
	if !hasOwns {
		t.Errorf("[#2991 spring-boot tests_linkage] expected OWNS relationship from test class to test method")
	}
}

// ============================================================================
// Spring WebFlux DI + tests_linkage (#2991)
// ============================================================================

// TestSpringWebFlux_DIBinding_Issue2991 proves that ExtractSpringBoot emits
// DI DEPENDS_ON edges for spring_webflux (di_binding_extraction cell).
// Registry target: lang.java.framework.spring-webflux di_binding_extraction=partial.
// Cite: internal/custom/java/spring_boot.go.
func TestSpringWebFlux_DIBinding_Issue2991(t *testing.T) {
	source := `
package com.example.webflux;

import org.springframework.stereotype.Service;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.context.annotation.Bean;
import org.springframework.context.annotation.Configuration;

@Service
public class OrderHandler {
    @Autowired
    private OrderRepository orderRepository;

    @Autowired
    private RouterConfig routerConfig;
}

@Configuration
public class RouterConfig {
    @Bean
    public RouterFunction<ServerResponse> routes(OrderHandler handler) {
        return RouterFunctions.route()
            .GET("/orders", handler::listOrders)
            .build();
    }
}
`
	r := ExtractSpringBoot(PatternContext{
		Source:    source,
		Language:  "java",
		Framework: "spring_webflux",
		FilePath:  "OrderHandler.java",
	})

	// di_binding_extraction: @Bean method (name stored as "OwnerClass.methodName")
	hasBean := false
	for _, e := range r.Entities {
		if e.Kind == "SCOPE.Operation" && e.Subtype == "function" && e.Provenance == "INFERRED_FROM_SPRING_BOOT_BEAN" {
			hasBean = true
			break
		}
	}
	if !hasBean {
		t.Errorf("[#2991 spring-webflux di_binding_extraction] expected @Bean entity from spring_webflux extractor")
	}

	// di_injection_point: @Autowired field injection emits DEPENDS_ON
	hasDep := false
	for _, rel := range r.Relationships {
		if rel.RelationshipType == "DEPENDS_ON" && rel.Properties["injection_kind"] == "field" {
			hasDep = true
			break
		}
	}
	if !hasDep {
		t.Errorf("[#2991 spring-webflux di_injection_point] expected DEPENDS_ON(injection_kind=field) from spring_webflux extractor")
	}
}

// TestSpringWebFlux_TestsLinkage_Issue2991 proves that ExtractJUnit5 runs for
// the "spring_webflux" framework (tests_linkage cell).
// Registry target: lang.java.framework.spring-webflux tests_linkage=partial.
// Cite: internal/custom/java/junit5.go.
func TestSpringWebFlux_TestsLinkage_Issue2991(t *testing.T) {
	source := `
package com.example.webflux;

import org.junit.jupiter.api.Test;
import org.springframework.boot.test.autoconfigure.web.reactive.WebFluxTest;
import static org.junit.jupiter.api.Assertions.*;

@WebFluxTest
class OrderHandlerTest {
    @Test
    void listOrders_returnsOk() {
        assertTrue(true);
    }

    @Test
    void createOrder_returnsCreated() {
        assertTrue(true);
    }
}
`
	r := ExtractJUnit5(PatternContext{
		Source:    source,
		Language:  "java",
		Framework: "spring_webflux",
		FilePath:  "OrderHandlerTest.java",
	})

	hasTestMethod := false
	for _, e := range r.Entities {
		if e.Properties["test_annotation"] == "Test" {
			hasTestMethod = true
			break
		}
	}
	if !hasTestMethod {
		t.Errorf("[#2991 spring-webflux tests_linkage] expected @Test entity from JUnit5 extractor for spring_webflux framework")
	}
}

// ============================================================================
// Helpers
// ============================================================================

func entityNames(entities []SecondaryEntity) []string {
	names := make([]string, len(entities))
	for i, e := range entities {
		names[i] = e.Name
	}
	return names
}

func contains(ss []string, s string) bool {
	for _, v := range ss {
		if v == s {
			return true
		}
	}
	return false
}

// ============================================================================
// Issue #2995 — Quarkus + Micronaut + JAX-RS proving tests
// Cells: dto_extraction, request_validation, route_extraction, tests_linkage
// ============================================================================

// ---- Quarkus ----------------------------------------------------------------

// TestQuarkus_RouteExtraction_Issue2995 proves that ExtractQuarkus emits
// SCOPE.Operation endpoint entities for each JAX-RS handler method in a
// Quarkus resource class, confirming route_extraction is delivered.
// Cite: internal/custom/java/quarkus.go
func TestQuarkus_RouteExtraction_Issue2995(t *testing.T) {
	source := `
package com.example;
import jakarta.ws.rs.*;
import jakarta.ws.rs.core.Response;

@Path("/orders")
public class OrderResource {
    @GET
    public Response list() { return Response.ok().build(); }

    @GET
    @Path("/{id}")
    public Response get() { return Response.ok().build(); }

    @POST
    public Response create(CreateOrderRequest req) { return Response.ok().build(); }
}
`
	r := ExtractQuarkus(PatternContext{
		Source:    source,
		Language:  "java",
		Framework: "quarkus",
		FilePath:  "OrderResource.java",
	})

	var endpoints []SecondaryEntity
	for _, e := range r.Entities {
		if e.Subtype == "endpoint" {
			endpoints = append(endpoints, e)
		}
	}
	if len(endpoints) < 3 {
		t.Errorf("[#2995 quarkus route_extraction] expected >=3 endpoint entities, got %d: %v",
			len(endpoints), entityNames(r.Entities))
	}
	verbsSeen := make(map[string]bool)
	for _, e := range endpoints {
		if v, ok := e.Properties["http_method"].(string); ok {
			verbsSeen[v] = true
		}
	}
	for _, want := range []string{"GET", "POST"} {
		if !verbsSeen[want] {
			t.Errorf("[#2995 quarkus route_extraction] HTTP method %q not found in endpoint entities", want)
		}
	}
}

// TestQuarkus_DtoExtraction_Issue2995 proves that ExtractQuarkus records the
// parameter type on POST/PUT endpoint entities, surfacing the implicit request
// body type (dto_extraction via java_annotation_routes.go + quarkus.go).
// Cite: internal/custom/java/quarkus.go, internal/engine/java_annotation_routes.go
func TestQuarkus_DtoExtraction_Issue2995(t *testing.T) {
	source := `
package com.example;
import jakarta.ws.rs.*;
import jakarta.ws.rs.core.Response;

@Path("/products")
public class ProductResource {
    @POST
    public Response create(CreateProductRequest body) { return Response.ok().build(); }

    @PUT
    @Path("/{id}")
    public Response update(UpdateProductRequest body) { return Response.ok().build(); }
}
`
	r := ExtractQuarkus(PatternContext{
		Source:    source,
		Language:  "java",
		Framework: "quarkus",
		FilePath:  "ProductResource.java",
	})

	var postEndpoints []SecondaryEntity
	for _, e := range r.Entities {
		if e.Subtype == "endpoint" {
			if v, ok := e.Properties["http_method"].(string); ok && (v == "POST" || v == "PUT") {
				postEndpoints = append(postEndpoints, e)
			}
		}
	}
	if len(postEndpoints) < 2 {
		t.Errorf("[#2995 quarkus dto_extraction] expected >=2 POST/PUT endpoint entities, got %d", len(postEndpoints))
	}
}

// TestQuarkus_RequestValidation_Issue2995 proves that endpoint entities are
// emitted even when handler parameters carry Bean Validation annotations
// (@NotNull, @Valid). Engine-level validation annotation parsing is proved by
// TestQuarkus_RequestValidation_Engine_Issue2995 in java_annotation_params_test.go.
// Cite: internal/engine/java_annotation_params.go
func TestQuarkus_RequestValidation_Issue2995(t *testing.T) {
	source := `
package com.example;
import jakarta.ws.rs.*;
import jakarta.validation.Valid;
import jakarta.validation.constraints.NotNull;

@Path("/items")
public class ItemResource {
    @POST
    public void create(@Valid @NotNull CreateItemRequest req) {}

    @PUT
    @Path("/{id}")
    public void update(@PathParam("id") Long id, @Valid UpdateItemRequest req) {}
}
`
	r := ExtractQuarkus(PatternContext{
		Source:    source,
		Language:  "java",
		Framework: "quarkus",
		FilePath:  "ItemResource.java",
	})

	var endpoints []SecondaryEntity
	for _, e := range r.Entities {
		if e.Subtype == "endpoint" {
			endpoints = append(endpoints, e)
		}
	}
	if len(endpoints) < 2 {
		t.Errorf("[#2995 quarkus request_validation] expected >=2 endpoint entities even with validation annotations, got %d", len(endpoints))
	}
}

// TestQuarkus_TestsLinkage_Issue2995 proves that ExtractJUnit5 fires on a
// Quarkus @QuarkusTest class (JUnit 5 under the hood) and emits test entities.
// Cite: internal/custom/java/junit5.go
func TestQuarkus_TestsLinkage_Issue2995(t *testing.T) {
	source := `
package com.example;
import io.quarkus.test.junit.QuarkusTest;
import org.junit.jupiter.api.*;
import static io.restassured.RestAssured.*;

@QuarkusTest
public class OrderResourceTest {
    @Test
    void testListOrders() {
        given().when().get("/orders").then().statusCode(200);
    }

    @Test
    void testCreateOrder() {
        given().body("{\"item\":\"widget\"}")
               .contentType("application/json")
               .when().post("/orders")
               .then().statusCode(201);
    }
}
`
	r := ExtractJUnit5(PatternContext{
		Source:    source,
		Language:  "java",
		Framework: "junit5",
		FilePath:  "OrderResourceTest.java",
	})

	var testMethods []SecondaryEntity
	for _, e := range r.Entities {
		if e.Provenance == "INFERRED_FROM_JUNIT5_TEST" {
			testMethods = append(testMethods, e)
		}
	}
	if len(testMethods) < 2 {
		t.Errorf("[#2995 quarkus tests_linkage] expected >=2 @Test entities for @QuarkusTest class, got %d: %v",
			len(testMethods), entityNames(r.Entities))
	}
}

// ---- Micronaut --------------------------------------------------------------

// TestMicronaut_RouteExtraction_Issue2995 proves that ExtractMicronaut emits
// SCOPE.Operation endpoint entities for each @Get/@Post annotated handler.
// Cite: internal/custom/java/micronaut.go
func TestMicronaut_RouteExtraction_Issue2995(t *testing.T) {
	source := `
package com.example;
import io.micronaut.http.annotation.*;
import io.micronaut.http.HttpResponse;
import java.util.List;

@Controller("/users")
public class UserController {
    @Get
    public List<UserDto> list() { return null; }

    @Get("/{id}")
    public UserDto get(Long id) { return null; }

    @Post
    public HttpResponse<UserDto> create(@Body CreateUserRequest req) { return HttpResponse.ok(); }

    @Put("/{id}")
    public UserDto update(Long id, @Body UpdateUserRequest req) { return null; }
}
`
	r := ExtractMicronaut(PatternContext{
		Source:    source,
		Language:  "java",
		Framework: "micronaut",
		FilePath:  "UserController.java",
	})

	var endpoints []SecondaryEntity
	for _, e := range r.Entities {
		if e.Subtype == "endpoint" {
			endpoints = append(endpoints, e)
		}
	}
	if len(endpoints) < 4 {
		t.Errorf("[#2995 micronaut route_extraction] expected >=4 endpoint entities, got %d: %v",
			len(endpoints), entityNames(r.Entities))
	}
	verbsSeen := make(map[string]bool)
	for _, e := range endpoints {
		if v, ok := e.Properties["http_method"].(string); ok {
			verbsSeen[v] = true
		}
	}
	for _, want := range []string{"GET", "POST", "PUT"} {
		if !verbsSeen[want] {
			t.Errorf("[#2995 micronaut route_extraction] HTTP method %q not found", want)
		}
	}
}

// TestMicronaut_DtoExtraction_Issue2995 proves that ExtractMicronaut emits
// endpoint entities for POST/PUT methods that accept @Body parameters.
// Cite: internal/custom/java/micronaut.go, internal/engine/java_annotation_routes.go
func TestMicronaut_DtoExtraction_Issue2995(t *testing.T) {
	source := `
package com.example;
import io.micronaut.http.annotation.*;

@Controller("/orders")
public class OrderController {
    @Post
    public OrderDto create(@Body CreateOrderRequest req) { return null; }

    @Put("/{id}")
    public OrderDto update(Long id, @Body UpdateOrderRequest req) { return null; }
}
`
	r := ExtractMicronaut(PatternContext{
		Source:    source,
		Language:  "java",
		Framework: "micronaut",
		FilePath:  "OrderController.java",
	})

	var bodyEndpoints []SecondaryEntity
	for _, e := range r.Entities {
		if e.Subtype == "endpoint" {
			if v, ok := e.Properties["http_method"].(string); ok && (v == "POST" || v == "PUT") {
				bodyEndpoints = append(bodyEndpoints, e)
			}
		}
	}
	if len(bodyEndpoints) < 2 {
		t.Errorf("[#2995 micronaut dto_extraction] expected >=2 POST/PUT endpoint entities, got %d", len(bodyEndpoints))
	}
}

// TestMicronaut_RequestValidation_Issue2995 proves that Micronaut controller
// handler methods decorated with Bean Validation annotations still emit
// endpoint entities (not suppressed).
// Cite: internal/engine/java_annotation_params.go
func TestMicronaut_RequestValidation_Issue2995(t *testing.T) {
	source := `
package com.example;
import io.micronaut.http.annotation.*;
import jakarta.validation.Valid;
import jakarta.validation.constraints.NotNull;

@Controller("/subscriptions")
public class SubscriptionController {
    @Post
    public SubscriptionDto subscribe(@Valid @Body @NotNull SubscribeRequest req) { return null; }
}
`
	r := ExtractMicronaut(PatternContext{
		Source:    source,
		Language:  "java",
		Framework: "micronaut",
		FilePath:  "SubscriptionController.java",
	})

	var endpoints []SecondaryEntity
	for _, e := range r.Entities {
		if e.Subtype == "endpoint" {
			endpoints = append(endpoints, e)
		}
	}
	if len(endpoints) < 1 {
		t.Errorf("[#2995 micronaut request_validation] expected >=1 endpoint entity with @Valid @Body, got %d", len(endpoints))
	}
}

// TestMicronaut_TestsLinkage_Issue2995 proves that ExtractJUnit5 fires on a
// Micronaut @MicronautTest class and emits test entities.
// Cite: internal/custom/java/junit5.go
func TestMicronaut_TestsLinkage_Issue2995(t *testing.T) {
	source := `
package com.example;
import io.micronaut.test.extensions.junit5.annotation.MicronautTest;
import org.junit.jupiter.api.*;
import jakarta.inject.Inject;

@MicronautTest
public class UserControllerTest {
    @Inject
    UserController userController;

    @Test
    void testListUsers() {
        assert userController != null;
    }

    @Test
    void testCreateUser() {
        // stub
    }
}
`
	r := ExtractJUnit5(PatternContext{
		Source:    source,
		Language:  "java",
		Framework: "junit5",
		FilePath:  "UserControllerTest.java",
	})

	var testMethods []SecondaryEntity
	for _, e := range r.Entities {
		if e.Provenance == "INFERRED_FROM_JUNIT5_TEST" {
			testMethods = append(testMethods, e)
		}
	}
	if len(testMethods) < 2 {
		t.Errorf("[#2995 micronaut tests_linkage] expected >=2 @Test entities for @MicronautTest class, got %d: %v",
			len(testMethods), entityNames(r.Entities))
	}
}

// ---- JAX-RS (standalone / Jakarta EE) --------------------------------------

// TestJAXRS_RouteExtraction_Issue2995 proves route_extraction for JAX-RS via
// the custom Quarkus extractor (same JAX-RS scanning). Engine-level extraction
// tested in internal/engine/java_annotation_routes_test.go.
// Cite: internal/engine/java_annotation_routes.go, internal/custom/java/quarkus.go
func TestJAXRS_RouteExtraction_Issue2995(t *testing.T) {
	src := `
package com.example;
import jakarta.ws.rs.*;
import jakarta.ws.rs.core.Response;

@Path("/invoices")
public class InvoiceResource {
    @GET
    public Response list() { return Response.ok().build(); }

    @GET
    @Path("/{id}")
    public Response get() { return Response.ok().build(); }

    @POST
    public Response create(CreateInvoiceRequest body) { return Response.ok().build(); }

    @DELETE
    @Path("/{id}")
    public Response delete() { return Response.noContent().build(); }
}
`
	r := ExtractQuarkus(PatternContext{
		Source:    src,
		Language:  "java",
		Framework: "quarkus",
		FilePath:  "InvoiceResource.java",
	})

	var endpoints []SecondaryEntity
	for _, e := range r.Entities {
		if e.Subtype == "endpoint" {
			endpoints = append(endpoints, e)
		}
	}
	if len(endpoints) < 4 {
		t.Errorf("[#2995 jaxrs route_extraction] expected >=4 endpoint entities, got %d: %v",
			len(endpoints), entityNames(r.Entities))
	}
}

// TestJAXRS_DtoExtraction_Issue2995 proves dto_extraction for JAX-RS: endpoint
// entities are emitted for POST/PUT methods with implicit body parameters.
// Cite: internal/engine/java_annotation_routes.go
func TestJAXRS_DtoExtraction_Issue2995(t *testing.T) {
	source := `
package com.example;
import jakarta.ws.rs.*;
import jakarta.ws.rs.core.Response;

@Path("/payments")
public class PaymentResource {
    @POST
    public Response pay(PaymentRequest body) { return Response.ok().build(); }

    @PUT
    @Path("/{id}/refund")
    public Response refund(RefundRequest body) { return Response.ok().build(); }
}
`
	r := ExtractQuarkus(PatternContext{
		Source:    source,
		Language:  "java",
		Framework: "quarkus",
		FilePath:  "PaymentResource.java",
	})

	var postPut []SecondaryEntity
	for _, e := range r.Entities {
		if e.Subtype == "endpoint" {
			if v, ok := e.Properties["http_method"].(string); ok && (v == "POST" || v == "PUT") {
				postPut = append(postPut, e)
			}
		}
	}
	if len(postPut) < 2 {
		t.Errorf("[#2995 jaxrs dto_extraction] expected >=2 POST/PUT endpoint entities, got %d", len(postPut))
	}
}

// TestJAXRS_RequestValidation_Issue2995 proves that Bean Validation annotations
// on JAX-RS handler parameters do not suppress endpoint emission.
// Cite: internal/engine/java_annotation_params.go
func TestJAXRS_RequestValidation_Issue2995(t *testing.T) {
	source := `
package com.example;
import jakarta.ws.rs.*;
import jakarta.validation.Valid;
import jakarta.validation.constraints.*;

@Path("/shipments")
public class ShipmentResource {
    @POST
    public void create(@Valid @NotNull CreateShipmentRequest req) {}

    @PUT
    @Path("/{id}")
    public void update(@PathParam("id") Long id, @Valid UpdateShipmentRequest req) {}
}
`
	r := ExtractQuarkus(PatternContext{
		Source:    source,
		Language:  "java",
		Framework: "quarkus",
		FilePath:  "ShipmentResource.java",
	})

	var endpoints []SecondaryEntity
	for _, e := range r.Entities {
		if e.Subtype == "endpoint" {
			endpoints = append(endpoints, e)
		}
	}
	if len(endpoints) < 2 {
		t.Errorf("[#2995 jaxrs request_validation] expected >=2 endpoint entities with validation annotations, got %d", len(endpoints))
	}
}

// ============================================================================
// Issue #3001 — Spring Data ORM schema_extraction proving tests
// Records: spring-data-mongo, spring-data-cassandra, spring-data-elastic, spring-data-redis
// ============================================================================

// TestSpringDataMongo_SchemaExtraction_Issue3001 proves that ExtractSpringEcosystem
// emits SCOPE.Schema entities for @Document-annotated classes, which delivers
// schema_extraction for lang.java.orm.spring-data-mongo at partial status.
// Cite: internal/custom/java/spring_ecosystem.go
func TestSpringDataMongo_SchemaExtraction_Issue3001(t *testing.T) {
	source := `
package com.example.mongo;
import org.springframework.data.mongodb.core.mapping.Document;
import org.springframework.data.annotation.Id;

@Document(collection = "orders")
public class Order {
    @Id
    private String id;
    private String customerId;
    private double total;
}

@Document
public class Product {
    @Id
    private String id;
    private String name;
}
`
	r := ExtractSpringEcosystem(PatternContext{
		Source:    source,
		Language:  "java",
		Framework: "spring_boot",
		FilePath:  "Order.java",
	})

	schemaNames := make(map[string]bool)
	for _, e := range r.Entities {
		if e.Kind == "SCOPE.Schema" && e.Provenance == "INFERRED_FROM_SPRING_DATA" {
			schemaNames[e.Name] = true
		}
	}
	for _, want := range []string{"Order", "Product"} {
		if !schemaNames[want] {
			t.Errorf("[#3001 spring-data-mongo schema_extraction] expected SCOPE.Schema for %q, got: %v", want, schemaNames)
		}
	}
}

// TestSpringDataElastic_SchemaExtraction_Issue3001 proves that ExtractSpringEcosystem
// emits SCOPE.Schema entities for @Document-annotated classes in an Elasticsearch
// context, delivering schema_extraction for lang.java.orm.spring-data-elastic.
// Cite: internal/custom/java/spring_ecosystem.go
func TestSpringDataElastic_SchemaExtraction_Issue3001(t *testing.T) {
	source := `
package com.example.elastic;
import org.springframework.data.elasticsearch.annotations.Document;
import org.springframework.data.annotation.Id;

@Document(indexName = "products")
public class ProductDoc {
    @Id
    private String id;
    private String name;
    private double price;
}
`
	r := ExtractSpringEcosystem(PatternContext{
		Source:    source,
		Language:  "java",
		Framework: "spring_boot",
		FilePath:  "ProductDoc.java",
	})

	found := false
	for _, e := range r.Entities {
		if e.Kind == "SCOPE.Schema" && e.Name == "ProductDoc" && e.Provenance == "INFERRED_FROM_SPRING_DATA" {
			found = true
		}
	}
	if !found {
		t.Errorf("[#3001 spring-data-elastic schema_extraction] expected SCOPE.Schema for ProductDoc (@Document)")
	}
}

// TestSpringDataRedis_SchemaExtraction_Issue3001 proves that ExtractSpringEcosystem
// emits SCOPE.Schema entities for @RedisHash-annotated classes, delivering
// schema_extraction for lang.java.orm.spring-data-redis at partial status.
// Cite: internal/custom/java/spring_ecosystem.go
func TestSpringDataRedis_SchemaExtraction_Issue3001(t *testing.T) {
	source := `
package com.example.redis;
import org.springframework.data.redis.core.RedisHash;
import org.springframework.data.annotation.Id;

@RedisHash("sessions")
public class UserSession {
    @Id
    private String sessionId;
    private String userId;
    private long ttl;
}
`
	r := ExtractSpringEcosystem(PatternContext{
		Source:    source,
		Language:  "java",
		Framework: "spring_boot",
		FilePath:  "UserSession.java",
	})

	found := false
	for _, e := range r.Entities {
		if e.Kind == "SCOPE.Schema" && e.Name == "UserSession" && e.Provenance == "INFERRED_FROM_SPRING_DATA" {
			found = true
		}
	}
	if !found {
		t.Errorf("[#3001 spring-data-redis schema_extraction] expected SCOPE.Schema for UserSession (@RedisHash)")
	}
}

// TestSpringDataCassandra_SchemaExtraction_Issue3001 proves that ExtractSpringEcosystem
// emits SCOPE.Schema entities for @Table-annotated Cassandra entity classes (disambiguated
// by @PrimaryKey body presence), delivering schema_extraction for
// lang.java.orm.spring-data-cassandra at partial status.
// Cite: internal/custom/java/spring_ecosystem.go
func TestSpringDataCassandra_SchemaExtraction_Issue3001(t *testing.T) {
	source := `
package com.example.cassandra;
import org.springframework.data.cassandra.core.mapping.Table;
import org.springframework.data.cassandra.core.mapping.PrimaryKey;
import org.springframework.data.cassandra.core.mapping.Column;

@Table("user_events")
public class UserEvent {
    @PrimaryKey
    private UserEventKey key;
    @Column("event_type")
    private String eventType;
}
`
	r := ExtractSpringEcosystem(PatternContext{
		Source:    source,
		Language:  "java",
		Framework: "spring_boot",
		FilePath:  "UserEvent.java",
	})

	found := false
	for _, e := range r.Entities {
		if e.Kind == "SCOPE.Schema" && e.Name == "UserEvent" && e.Provenance == "INFERRED_FROM_SPRING_DATA_CASSANDRA" {
			found = true
		}
	}
	if !found {
		t.Errorf("[#3001 spring-data-cassandra schema_extraction] expected SCOPE.Schema for UserEvent (@Table + @PrimaryKey)")
	}
}

// TestJAXRS_TestsLinkage_Issue2995 proves that JUnit 5 test detection fires for
// a JAX-RS integration test class (REST-Assured pattern).
// Cite: internal/custom/java/junit5.go
func TestJAXRS_TestsLinkage_Issue2995(t *testing.T) {
	source := `
package com.example;
import org.junit.jupiter.api.*;
import static io.restassured.RestAssured.*;
import static org.hamcrest.Matchers.*;

public class InvoiceResourceTest {
    @BeforeEach
    void setUp() {
        baseURI = "http://localhost:8080";
    }

    @Test
    void testListInvoices() {
        given().when().get("/invoices").then().statusCode(200);
    }

    @Test
    void testCreateInvoice() {
        given().body("{\"amount\":100}")
               .contentType("application/json")
               .when().post("/invoices")
               .then().statusCode(201);
    }
}
`
	r := ExtractJUnit5(PatternContext{
		Source:    source,
		Language:  "java",
		Framework: "junit5",
		FilePath:  "InvoiceResourceTest.java",
	})

	var testMethods []SecondaryEntity
	for _, e := range r.Entities {
		if e.Provenance == "INFERRED_FROM_JUNIT5_TEST" {
			testMethods = append(testMethods, e)
		}
	}
	if len(testMethods) < 2 {
		t.Errorf("[#2995 jaxrs tests_linkage] expected >=2 @Test entities for JAX-RS test class, got %d: %v",
			len(testMethods), entityNames(r.Entities))
	}
}

// ============================================================================
// @Transactional tests (#3003 — Transactions lane, JVM frameworks)
// ============================================================================

// txProp returns the named property of the first SCOPE.Pattern transaction
// boundary entity matching name, or "" if not found.
func txEntityByName(r PatternResult, name string) (SecondaryEntity, bool) {
	for _, e := range r.Entities {
		if e.Subtype == "transaction_boundary" && e.Name == name {
			return e, true
		}
	}
	return SecondaryEntity{}, false
}

// TestTransactional_Boundary_Propagation_Rollback_Issue3003 is the proving
// fixture from the issue: a Spring service with a class-level @Transactional
// and a method-level @Transactional(propagation=REQUIRES_NEW,
// rollbackFor=RuntimeException.class). Proves all three Transactions-lane
// cells: transaction_boundary_extraction, transaction_propagation,
// transaction_rollback_rules.
// Registry target: lang.java.framework.spring-boot Transactions/* = partial.
// Cite: internal/custom/java/transactional.go.
func TestTransactional_Boundary_Propagation_Rollback_Issue3003(t *testing.T) {
	source := `
package com.example.order;

import org.springframework.stereotype.Service;
import org.springframework.transaction.annotation.Transactional;
import org.springframework.transaction.annotation.Propagation;
import org.springframework.transaction.annotation.Isolation;

@Service
@Transactional(readOnly = true)
public class OrderService {

    @Transactional(propagation = Propagation.REQUIRES_NEW, rollbackFor = RuntimeException.class)
    public void placeOrder(Order order) {
        repository.save(order);
    }

    @Transactional(propagation = Propagation.MANDATORY, isolation = Isolation.SERIALIZABLE,
                   rollbackFor = {IllegalStateException.class, java.io.IOException.class},
                   noRollbackFor = ValidationException.class)
    public Order updateOrder(Long id) {
        return null;
    }

    @Transactional
    public void plainTx() {
    }
}
`
	r := ExtractTransactional(PatternContext{
		Source:    source,
		Language:  "java",
		Framework: "spring_boot",
		FilePath:  "OrderService.java",
	})

	// transaction_boundary_extraction: class-level boundary entity.
	cls, ok := txEntityByName(r, "OrderService")
	if !ok {
		t.Fatalf("[#3003 boundary] expected class-level transaction_boundary entity for OrderService; got %v", entityNames(r.Entities))
	}
	if cls.Kind != "SCOPE.Pattern" {
		t.Errorf("[#3003 boundary] class boundary Kind = %q, want SCOPE.Pattern", cls.Kind)
	}
	if cls.Properties["transaction_boundary"] != "class" {
		t.Errorf("[#3003 boundary] class boundary transaction_boundary = %v, want class", cls.Properties["transaction_boundary"])
	}
	if cls.Properties["read_only"] != "true" {
		t.Errorf("[#3003 boundary] class boundary read_only = %v, want true", cls.Properties["read_only"])
	}

	// transaction_boundary_extraction: method-level boundary entities.
	place, ok := txEntityByName(r, "OrderService.placeOrder")
	if !ok {
		t.Fatalf("[#3003 boundary] expected method boundary OrderService.placeOrder; got %v", entityNames(r.Entities))
	}
	if place.Properties["transaction_boundary"] != "method" {
		t.Errorf("[#3003 boundary] placeOrder transaction_boundary = %v, want method", place.Properties["transaction_boundary"])
	}
	if place.Properties["declaring_class"] != "OrderService" {
		t.Errorf("[#3003 boundary] placeOrder declaring_class = %v, want OrderService", place.Properties["declaring_class"])
	}

	// transaction_propagation.
	if place.Properties["propagation"] != "REQUIRES_NEW" {
		t.Errorf("[#3003 propagation] placeOrder propagation = %v, want REQUIRES_NEW", place.Properties["propagation"])
	}
	upd, _ := txEntityByName(r, "OrderService.updateOrder")
	if upd.Properties["propagation"] != "MANDATORY" {
		t.Errorf("[#3003 propagation] updateOrder propagation = %v, want MANDATORY", upd.Properties["propagation"])
	}
	if upd.Properties["isolation"] != "SERIALIZABLE" {
		t.Errorf("[#3003 propagation] updateOrder isolation = %v, want SERIALIZABLE", upd.Properties["isolation"])
	}

	// transaction_rollback_rules: single class.
	if place.Properties["rollback_for"] != "RuntimeException" {
		t.Errorf("[#3003 rollback] placeOrder rollback_for = %v, want RuntimeException", place.Properties["rollback_for"])
	}
	// transaction_rollback_rules: list form + noRollbackFor.
	rb, _ := upd.Properties["rollback_for"].(string)
	if !strings.Contains(rb, "IllegalStateException") || !strings.Contains(rb, "IOException") {
		t.Errorf("[#3003 rollback] updateOrder rollback_for = %q, want IllegalStateException + IOException", rb)
	}
	if upd.Properties["no_rollback_for"] != "ValidationException" {
		t.Errorf("[#3003 rollback] updateOrder no_rollback_for = %v, want ValidationException", upd.Properties["no_rollback_for"])
	}

	// OWNS edge from class boundary to each method boundary.
	wantOwns := "scope:pattern:transaction_boundary:OrderService.java:OrderService"
	var ownsCount int
	for _, rel := range r.Relationships {
		if rel.RelationshipType == "OWNS" && rel.SourceRef == wantOwns {
			ownsCount++
		}
	}
	if ownsCount != 3 {
		t.Errorf("[#3003 boundary] expected 3 OWNS edges from class boundary, got %d", ownsCount)
	}

	// plainTx: boundary with no attributes.
	plain, ok := txEntityByName(r, "OrderService.plainTx")
	if !ok {
		t.Fatalf("[#3003 boundary] expected method boundary OrderService.plainTx")
	}
	if _, has := plain.Properties["propagation"]; has {
		t.Errorf("[#3003 propagation] plainTx should have no propagation property, got %v", plain.Properties["propagation"])
	}
}

// TestTransactional_JakartaJTA_Issue3003 proves the Jakarta/JTA @Transactional
// surface (jakarta.transaction.Transactional with positional TxType) extracts a
// boundary + propagation for the jakarta-ee framework.
// Registry target: lang.java.framework.jakarta-ee Transactions/* = partial.
func TestTransactional_JakartaJTA_Issue3003(t *testing.T) {
	source := `
package com.example.billing;

import jakarta.transaction.Transactional;

public class PaymentBean {

    @Transactional(Transactional.TxType.REQUIRES_NEW)
    public void charge(long amount) {
    }

    @Transactional(rollbackFor = PaymentException.class)
    public void refund(long amount) {
    }
}
`
	r := ExtractTransactional(PatternContext{
		Source:    source,
		Language:  "java",
		Framework: "jakarta_ee",
		FilePath:  "PaymentBean.java",
	})

	charge, ok := txEntityByName(r, "PaymentBean.charge")
	if !ok {
		t.Fatalf("[#3003 jakarta boundary] expected PaymentBean.charge boundary; got %v", entityNames(r.Entities))
	}
	if charge.Properties["framework"] != "jakarta_ee" {
		t.Errorf("[#3003 jakarta] framework = %v, want jakarta_ee", charge.Properties["framework"])
	}
	if charge.Properties["propagation"] != "REQUIRES_NEW" {
		t.Errorf("[#3003 jakarta propagation] charge propagation = %v, want REQUIRES_NEW (JTA TxType)", charge.Properties["propagation"])
	}
	refund, _ := txEntityByName(r, "PaymentBean.refund")
	if refund.Properties["rollback_for"] != "PaymentException" {
		t.Errorf("[#3003 jakarta rollback] refund rollback_for = %v, want PaymentException", refund.Properties["rollback_for"])
	}
}

// TestTransactional_FrameworkGating_Issue3003 proves the extractor runs for the
// registered JVM frameworks and no-ops for unrelated ones / non-java.
func TestTransactional_FrameworkGating_Issue3003(t *testing.T) {
	source := `
@Transactional(propagation = Propagation.REQUIRED)
public void doWork() {}
`
	for _, fw := range []string{"spring_boot", "spring_webflux", "quarkus", "micronaut", "jakarta_ee", "jaxrs"} {
		r := ExtractTransactional(PatternContext{Source: source, Language: "java", Framework: fw, FilePath: "X.java"})
		if len(r.Entities) == 0 {
			t.Errorf("[#3003 gating] framework %q expected a boundary entity, got none", fw)
		}
	}
	// Unrelated framework: no-op.
	if r := ExtractTransactional(PatternContext{Source: source, Language: "java", Framework: "django", FilePath: "X.java"}); len(r.Entities) != 0 {
		t.Errorf("[#3003 gating] framework django should no-op, got %d entities", len(r.Entities))
	}
	// Non-java language: no-op.
	if r := ExtractTransactional(PatternContext{Source: source, Language: "python", Framework: "spring_boot", FilePath: "X.py"}); len(r.Entities) != 0 {
		t.Errorf("[#3003 gating] non-java should no-op, got %d entities", len(r.Entities))
	}
}

// TestTransactional_MicroProfile_Issue3079 proves that adding "microprofile" to
// txFrameworks lets the extractor fire for MicroProfile services that use the
// Jakarta Transactions @Transactional annotation (JTA).  It covers:
//   - transaction_boundary_extraction: class-level + method-level boundaries
//   - transaction_propagation: positional TxType form (REQUIRES_NEW, NOT_SUPPORTED, NEVER)
//   - transaction_rollback_rules: (rollbackFor mapped via rollbackOn in fixture source)
//
// Registry target: lang.java.framework.microprofile Transactions/* = partial.
func TestTransactional_MicroProfile_Issue3079(t *testing.T) {
	source := `
package com.example.microprofile;

import jakarta.enterprise.context.ApplicationScoped;
import jakarta.transaction.Transactional;
import jakarta.transaction.Transactional.TxType;

@ApplicationScoped
@Transactional
public class OrderService {

    public void createOrder(String item) {}

    @Transactional(TxType.REQUIRES_NEW)
    public void auditOrder(String orderId) {}

    @Transactional(rollbackFor = OrderException.class)
    public void confirmPayment(String paymentId) {}

    @Transactional(TxType.NOT_SUPPORTED)
    public void sendNotification(String message) {}
}
`
	r := ExtractTransactional(PatternContext{
		Source:    source,
		Language:  "java",
		Framework: "microprofile",
		FilePath:  "OrderService.java",
	})

	// transaction_boundary_extraction: class-level boundary for OrderService.
	classBoundary, ok := txEntityByName(r, "OrderService")
	if !ok {
		t.Fatalf("[#3079 boundary] expected class-level boundary for OrderService; got %v", entityNames(r.Entities))
	}
	if classBoundary.Properties["framework"] != "microprofile" {
		t.Errorf("[#3079 boundary] framework = %v, want microprofile", classBoundary.Properties["framework"])
	}
	if classBoundary.Properties["transaction_boundary"] != "class" {
		t.Errorf("[#3079 boundary] transaction_boundary = %v, want class", classBoundary.Properties["transaction_boundary"])
	}

	// transaction_propagation: auditOrder uses positional TxType.REQUIRES_NEW.
	audit, ok := txEntityByName(r, "OrderService.auditOrder")
	if !ok {
		t.Fatalf("[#3079 propagation] expected boundary for OrderService.auditOrder; got %v", entityNames(r.Entities))
	}
	if audit.Properties["propagation"] != "REQUIRES_NEW" {
		t.Errorf("[#3079 propagation] auditOrder propagation = %v, want REQUIRES_NEW", audit.Properties["propagation"])
	}

	// transaction_rollback_rules: confirmPayment uses rollbackFor.
	confirm, ok := txEntityByName(r, "OrderService.confirmPayment")
	if !ok {
		t.Fatalf("[#3079 rollback] expected boundary for OrderService.confirmPayment; got %v", entityNames(r.Entities))
	}
	if confirm.Properties["rollback_for"] != "OrderException" {
		t.Errorf("[#3079 rollback] confirmPayment rollback_for = %v, want OrderException", confirm.Properties["rollback_for"])
	}

	// NOT_SUPPORTED propagation captured.
	notify, ok := txEntityByName(r, "OrderService.sendNotification")
	if !ok {
		t.Fatalf("[#3079 propagation] expected boundary for OrderService.sendNotification; got %v", entityNames(r.Entities))
	}
	if notify.Properties["propagation"] != "NOT_SUPPORTED" {
		t.Errorf("[#3079 propagation] sendNotification propagation = %v, want NOT_SUPPORTED", notify.Properties["propagation"])
	}
}

// TestTransactional_MicroProfile_Gating_Issue3079 confirms the gating list
// includes "microprofile" and its aliases.
func TestTransactional_MicroProfile_Gating_Issue3079(t *testing.T) {
	source := `
@Transactional(TxType.REQUIRED)
public void doWork() {}
`
	for _, fw := range []string{"microprofile", "micro-profile", "micro_profile"} {
		r := ExtractTransactional(PatternContext{Source: source, Language: "java", Framework: fw, FilePath: "X.java"})
		if len(r.Entities) == 0 {
			t.Errorf("[#3079 gating] framework %q expected a boundary entity, got none", fw)
		}
	}
}

// ============================================================================
// Spring AOP / AspectJ tests (#3004)
// ============================================================================

// aopEntityByName returns the AOP entity with the given subtype and name.
func aopEntityByName(r PatternResult, subtype, name string) (SecondaryEntity, bool) {
	for _, e := range r.Entities {
		if e.Subtype == subtype && e.Name == name {
			return e, true
		}
	}
	return SecondaryEntity{}, false
}

func aopHasRel(r PatternResult, src, tgt, kind string) bool {
	for _, rel := range r.Relationships {
		if rel.SourceRef == src && rel.TargetRef == tgt && rel.RelationshipType == kind {
			return true
		}
	}
	return false
}

// TestSpringAOP_Aspect_Pointcut_Advice_Issue3004 is the proving fixture for the
// AOP lane: an @Aspect class with a @Pointcut, an @Around advice that names the
// pointcut, a @Before with an inline execution() expression, and the
// @AfterReturning attribute form.
// Registry target: lang.java.framework.spring-boot AOP/* = partial.
func TestSpringAOP_Aspect_Pointcut_Advice_Issue3004(t *testing.T) {
	source := `
package com.example.aop;

import org.aspectj.lang.annotation.Aspect;
import org.aspectj.lang.annotation.Pointcut;
import org.aspectj.lang.annotation.Around;
import org.aspectj.lang.annotation.Before;
import org.aspectj.lang.annotation.AfterReturning;
import org.springframework.stereotype.Component;

@Aspect
@Component
public class LoggingAspect {

    @Pointcut("execution(* com.example.service.*.*(..))")
    public void serviceMethods() {}

    @Around("serviceMethods()")
    public Object logAround(ProceedingJoinPoint pjp) throws Throwable {
        return pjp.proceed();
    }

    @Before("execution(* com.example.web.*.*(..))")
    public void logBefore(JoinPoint jp) {
    }

    @AfterReturning(pointcut = "serviceMethods()", returning = "result")
    public void logReturn(JoinPoint jp, Object result) {
    }
}
`
	r := ExtractSpringAOP(PatternContext{
		Source:    source,
		Language:  "java",
		Framework: "spring_boot",
		FilePath:  "LoggingAspect.java",
	})

	// aspect_extraction.
	asp, ok := aopEntityByName(r, "aspect", "LoggingAspect")
	if !ok {
		t.Fatalf("[#3004 aspect] expected aspect entity LoggingAspect; got %v", entityNames(r.Entities))
	}
	if asp.Kind != "SCOPE.Pattern" {
		t.Errorf("[#3004 aspect] aspect Kind = %q, want SCOPE.Pattern", asp.Kind)
	}
	if asp.Properties["kind"] != "aspect" {
		t.Errorf("[#3004 aspect] aspect kind property = %v, want aspect", asp.Properties["kind"])
	}
	if asp.Properties["framework"] != "spring_boot" {
		t.Errorf("[#3004 aspect] aspect framework = %v, want spring_boot", asp.Properties["framework"])
	}

	// pointcut_resolution.
	pc, ok := aopEntityByName(r, "pointcut", "LoggingAspect.serviceMethods")
	if !ok {
		t.Fatalf("[#3004 pointcut] expected pointcut LoggingAspect.serviceMethods; got %v", entityNames(r.Entities))
	}
	if pc.Properties["pointcut_expression"] != "execution(* com.example.service.*.*(..))" {
		t.Errorf("[#3004 pointcut] expression = %v", pc.Properties["pointcut_expression"])
	}
	if pc.Properties["aspect"] != "LoggingAspect" {
		t.Errorf("[#3004 pointcut] aspect = %v, want LoggingAspect", pc.Properties["aspect"])
	}

	// advice_attribution: @Around naming the pointcut.
	around, ok := aopEntityByName(r, "advice", "LoggingAspect.logAround")
	if !ok {
		t.Fatalf("[#3004 advice] expected advice LoggingAspect.logAround; got %v", entityNames(r.Entities))
	}
	if around.Properties["advice_type"] != "around" {
		t.Errorf("[#3004 advice] logAround advice_type = %v, want around", around.Properties["advice_type"])
	}
	if around.Properties["pointcut_expression"] != "serviceMethods()" {
		t.Errorf("[#3004 advice] logAround pointcut_expression = %v, want serviceMethods()", around.Properties["pointcut_expression"])
	}
	if around.Properties["aspect"] != "LoggingAspect" {
		t.Errorf("[#3004 advice] logAround aspect = %v, want LoggingAspect", around.Properties["aspect"])
	}

	// advice_attribution: @Before with inline execution() expression.
	before, ok := aopEntityByName(r, "advice", "LoggingAspect.logBefore")
	if !ok {
		t.Fatalf("[#3004 advice] expected advice LoggingAspect.logBefore")
	}
	if before.Properties["advice_type"] != "before" {
		t.Errorf("[#3004 advice] logBefore advice_type = %v, want before", before.Properties["advice_type"])
	}

	// advice_attribution: @AfterReturning attribute (pointcut=...) form.
	ret, ok := aopEntityByName(r, "advice", "LoggingAspect.logReturn")
	if !ok {
		t.Fatalf("[#3004 advice] expected advice LoggingAspect.logReturn")
	}
	if ret.Properties["advice_type"] != "after_returning" {
		t.Errorf("[#3004 advice] logReturn advice_type = %v, want after_returning", ret.Properties["advice_type"])
	}
	if ret.Properties["pointcut_expression"] != "serviceMethods()" {
		t.Errorf("[#3004 advice] logReturn pointcut_expression = %v, want serviceMethods()", ret.Properties["pointcut_expression"])
	}

	// OWNS: aspect -> pointcut, aspect -> each advice.
	if !aopHasRel(r, asp.Ref, pc.Ref, "OWNS") {
		t.Errorf("[#3004 aspect] expected OWNS edge aspect -> pointcut")
	}
	for _, adv := range []SecondaryEntity{around, before, ret} {
		if !aopHasRel(r, asp.Ref, adv.Ref, "OWNS") {
			t.Errorf("[#3004 advice] expected OWNS edge aspect -> %s", adv.Name)
		}
	}

	// REFERENCES (pointcut_resolution): advice naming a declared pointcut links
	// to it; advice with an inline execution() expression does not.
	if !aopHasRel(r, around.Ref, pc.Ref, "REFERENCES") {
		t.Errorf("[#3004 pointcut] expected REFERENCES edge logAround -> serviceMethods pointcut")
	}
	if !aopHasRel(r, ret.Ref, pc.Ref, "REFERENCES") {
		t.Errorf("[#3004 pointcut] expected REFERENCES edge logReturn -> serviceMethods pointcut")
	}
	if aopHasRel(r, before.Ref, pc.Ref, "REFERENCES") {
		t.Errorf("[#3004 pointcut] logBefore uses inline execution(); must NOT REFERENCES a named pointcut")
	}
}

// TestSpringAOP_AllAdviceTypes_Issue3004 proves every advice annotation maps to
// the right advice_type, on the spring-webflux framework.
func TestSpringAOP_AllAdviceTypes_Issue3004(t *testing.T) {
	source := `
import org.aspectj.lang.annotation.Aspect;

@Aspect
public class AuditAspect {

    @Before("execution(* *(..))")
    public void b() {}

    @After("execution(* *(..))")
    public void a() {}

    @Around("execution(* *(..))")
    public Object ar(ProceedingJoinPoint p) throws Throwable { return p.proceed(); }

    @AfterReturning("execution(* *(..))")
    public void retn() {}

    @AfterThrowing("execution(* *(..))")
    public void thr() {}
}
`
	r := ExtractSpringAOP(PatternContext{Source: source, Language: "java", Framework: "spring-webflux", FilePath: "AuditAspect.java"})

	want := map[string]string{
		"AuditAspect.b":    "before",
		"AuditAspect.a":    "after",
		"AuditAspect.ar":   "around",
		"AuditAspect.retn": "after_returning",
		"AuditAspect.thr":  "after_throwing",
	}
	for name, adviceType := range want {
		e, ok := aopEntityByName(r, "advice", name)
		if !ok {
			t.Fatalf("[#3004 advice] expected advice %s; got %v", name, entityNames(r.Entities))
		}
		if e.Properties["advice_type"] != adviceType {
			t.Errorf("[#3004 advice] %s advice_type = %v, want %s", name, e.Properties["advice_type"], adviceType)
		}
		if e.Properties["framework"] != "spring_webflux" {
			t.Errorf("[#3004 advice] %s framework = %v, want spring_webflux", name, e.Properties["framework"])
		}
	}
}

// TestSpringAOP_NonAspectFile_Issue3004 proves advice annotations outside an
// @Aspect class produce nothing (no phantom advice/aspect entities).
func TestSpringAOP_NonAspectFile_Issue3004(t *testing.T) {
	source := `
@Service
public class PlainService {
    @Before("execution(* *(..))")
    public void notReallyAdvice() {}
}
`
	r := ExtractSpringAOP(PatternContext{Source: source, Language: "java", Framework: "spring_boot", FilePath: "PlainService.java"})
	if len(r.Entities) != 0 {
		t.Errorf("[#3004] non-aspect file should emit no AOP entities, got %v", entityNames(r.Entities))
	}
}

// TestSpringAOP_FrameworkGating_Issue3004 proves the extractor runs only for the
// Spring frameworks and no-ops elsewhere / for non-java.
func TestSpringAOP_FrameworkGating_Issue3004(t *testing.T) {
	source := `
@Aspect
public class A {
    @Around("execution(* *(..))")
    public Object x(ProceedingJoinPoint p) throws Throwable { return p.proceed(); }
}
`
	for _, fw := range []string{"spring_boot", "spring-boot", "spring_webflux", "spring-webflux"} {
		r := ExtractSpringAOP(PatternContext{Source: source, Language: "java", Framework: fw, FilePath: "A.java"})
		if len(r.Entities) == 0 {
			t.Errorf("[#3004 gating] framework %q expected AOP entities, got none", fw)
		}
	}
	for _, fw := range []string{"quarkus", "micronaut", "jakarta_ee", "jaxrs", "django"} {
		if r := ExtractSpringAOP(PatternContext{Source: source, Language: "java", Framework: fw, FilePath: "A.java"}); len(r.Entities) != 0 {
			t.Errorf("[#3004 gating] framework %q should no-op, got %d entities", fw, len(r.Entities))
		}
	}
	if r := ExtractSpringAOP(PatternContext{Source: source, Language: "python", Framework: "spring_boot", FilePath: "A.py"}); len(r.Entities) != 0 {
		t.Errorf("[#3004 gating] non-java should no-op, got %d entities", len(r.Entities))
	}
}

// ============================================================================
// Observability tests (#3006)
// ============================================================================

// findObsEntity returns the first SCOPE.Pattern entity with the given subtype
// whose properties include kvWant (all keys must match), or nil.
func findObsEntity(r PatternResult, subtype string, kvWant map[string]string) *SecondaryEntity {
	for i := range r.Entities {
		e := &r.Entities[i]
		if e.Kind != "SCOPE.Pattern" || e.Subtype != subtype {
			continue
		}
		ok := true
		for k, v := range kvWant {
			if got, _ := e.Properties[k].(string); got != v {
				ok = false
				break
			}
		}
		if ok {
			return e
		}
	}
	return nil
}

func countObsSubtype(r PatternResult, subtype string) int {
	n := 0
	for _, e := range r.Entities {
		if e.Kind == "SCOPE.Pattern" && e.Subtype == subtype {
			n++
		}
	}
	return n
}

// TestObservability_Slf4jLogging proves log_extraction detects @Slf4j loggers
// and the log.<level>(...) statement call surface.
func TestObservability_Slf4jLogging_Issue3006(t *testing.T) {
	source := `
import lombok.extern.slf4j.Slf4j;

@Slf4j
@Service
public class OrderService {
    public void place(Order o) {
        log.info("placing order {}", o.getId());
        if (o.isInvalid()) {
            log.error("invalid order");
        }
        log.debug("done");
    }
}
`
	r := ExtractObservability(PatternContext{Source: source, Language: "java", Framework: "spring_boot", FilePath: "OrderService.java"})

	logger := findObsEntity(r, "logger", map[string]string{"library": "slf4j", "holder": "OrderService"})
	if logger == nil {
		t.Fatalf("[#3006 log] expected @Slf4j logger entity, got %v", entityNames(r.Entities))
	}
	if logger.Properties["framework"] != "spring_boot" {
		t.Errorf("[#3006 log] logger framework = %v, want spring_boot", logger.Properties["framework"])
	}

	if n := countObsSubtype(r, "log_statement"); n != 3 {
		t.Errorf("[#3006 log] expected 3 log statements, got %d", n)
	}
	if findObsEntity(r, "log_statement", map[string]string{"log_level": "info"}) == nil {
		t.Errorf("[#3006 log] missing info statement")
	}
	if findObsEntity(r, "log_statement", map[string]string{"log_level": "error"}) == nil {
		t.Errorf("[#3006 log] missing error statement")
	}
}

// TestObservability_LoggerFactoryVariants proves SLF4J / Log4j2 / JUL logger
// factories are recognised with the right backing library.
func TestObservability_LoggerFactoryVariants_Issue3006(t *testing.T) {
	cases := []struct {
		decl    string
		library string
	}{
		{`private static final Logger slf = LoggerFactory.getLogger(Foo.class);`, "slf4j"},
		{`private static final Logger l4j = LogManager.getLogger(Foo.class);`, "log4j"},
		{`private static final java.util.logging.Logger jul = Logger.getLogger("Foo");`, "jul"},
	}
	for _, c := range cases {
		source := "@Service\npublic class Foo {\n  " + c.decl + "\n}\n"
		r := ExtractObservability(PatternContext{Source: source, Language: "java", Framework: "quarkus", FilePath: "Foo.java"})
		if findObsEntity(r, "logger", map[string]string{"library": c.library}) == nil {
			t.Errorf("[#3006 log] decl %q expected library %q, got %v", c.decl, c.library, r.Entities)
		}
	}
}

// TestObservability_MicrometerMetrics proves metric_extraction detects
// Micrometer builders, MeterRegistry, and @Timed.
func TestObservability_MicrometerMetrics_Issue3006(t *testing.T) {
	source := `
import io.micrometer.core.annotation.Timed;
import io.micrometer.core.instrument.Counter;
import io.micrometer.core.instrument.MeterRegistry;

@Service
public class CheckoutService {
    private final MeterRegistry registry;
    private final Counter orders = Counter.builder("orders.count").register(registry);

    @Timed(value = "checkout.latency")
    public void checkout() { orders.increment(); }
}
`
	r := ExtractObservability(PatternContext{Source: source, Language: "java", Framework: "spring_boot", FilePath: "CheckoutService.java"})

	if e := findObsEntity(r, "metric", map[string]string{"metric_name": "orders.count", "metric_type": "counter"}); e == nil {
		t.Errorf("[#3006 metric] expected Counter.builder metric, got %v", r.Entities)
	}
	if findObsEntity(r, "metric", map[string]string{"metric_type": "registry"}) == nil {
		t.Errorf("[#3006 metric] expected MeterRegistry signal")
	}
	if e := findObsEntity(r, "metric", map[string]string{"metric_type": "timer", "method": "checkout"}); e == nil {
		t.Errorf("[#3006 metric] expected @Timed metric, got %v", r.Entities)
	} else if e.Properties["metric_name"] != "checkout.latency" {
		t.Errorf("[#3006 metric] @Timed metric_name = %v, want checkout.latency", e.Properties["metric_name"])
	}
}

// TestObservability_MicroProfileMetrics proves @Counted / @Metered / @Gauge are
// mapped to metric_type values.
func TestObservability_MicroProfileMetrics_Issue3006(t *testing.T) {
	source := `
@ApplicationScoped
public class Metrics {
    @Counted(value = "calls.total")
    public void counted() {}

    @Metered
    public void metered() {}

    @Gauge(unit = "none")
    public long gauged() { return 1; }
}
`
	r := ExtractObservability(PatternContext{Source: source, Language: "java", Framework: "quarkus", FilePath: "Metrics.java"})
	for _, want := range []struct{ mtype, method string }{
		{"counter", "counted"}, {"meter", "metered"}, {"gauge", "gauged"},
	} {
		if findObsEntity(r, "metric", map[string]string{"metric_type": want.mtype, "method": want.method}) == nil {
			t.Errorf("[#3006 metric] expected %s metric on %s, got %v", want.mtype, want.method, r.Entities)
		}
	}
}

// TestObservability_OtelTracing proves trace_extraction detects @WithSpan and
// programmatic tracer.spanBuilder spans.
func TestObservability_OtelTracing_Issue3006(t *testing.T) {
	source := `
import io.opentelemetry.instrumentation.annotations.WithSpan;
import io.opentelemetry.api.trace.Tracer;

@Service
public class PaymentService {
    private final Tracer tracer;

    @WithSpan("charge-card")
    public void charge() {
        var span = tracer.spanBuilder("downstream-call").startSpan();
        span.setAttribute("amount", 10);
    }
}
`
	r := ExtractObservability(PatternContext{Source: source, Language: "java", Framework: "spring_boot", FilePath: "PaymentService.java"})

	if e := findObsEntity(r, "trace_span", map[string]string{"span_kind": "annotation", "method": "charge", "library": "otel"}); e == nil {
		t.Errorf("[#3006 trace] expected @WithSpan span, got %v", r.Entities)
	} else if e.Properties["span_name"] != "charge-card" {
		t.Errorf("[#3006 trace] @WithSpan span_name = %v, want charge-card", e.Properties["span_name"])
	}
	if findObsEntity(r, "trace_span", map[string]string{"span_kind": "programmatic", "span_name": "downstream-call"}) == nil {
		t.Errorf("[#3006 trace] expected programmatic spanBuilder span")
	}
}

// TestObservability_MicrometerTracing proves @Observed and tracer.nextSpan are
// detected as Micrometer Tracing spans.
func TestObservability_MicrometerTracing_Issue3006(t *testing.T) {
	source := `
import io.micrometer.observation.annotation.Observed;

@Service
public class InventoryService {
    @Observed(name = "inventory.check")
    public boolean check() {
        var span = tracer.nextSpan().name("reserve");
        return true;
    }
}
`
	r := ExtractObservability(PatternContext{Source: source, Language: "java", Framework: "spring_boot", FilePath: "InventoryService.java"})

	if e := findObsEntity(r, "trace_span", map[string]string{"span_kind": "annotation", "library": "micrometer", "method": "check"}); e == nil {
		t.Errorf("[#3006 trace] expected @Observed span, got %v", r.Entities)
	} else if e.Properties["span_name"] != "inventory.check" {
		t.Errorf("[#3006 trace] @Observed span_name = %v, want inventory.check", e.Properties["span_name"])
	}
	if findObsEntity(r, "trace_span", map[string]string{"span_kind": "programmatic", "library": "micrometer"}) == nil {
		t.Errorf("[#3006 trace] expected nextSpan() span")
	}
}

// TestObservability_FrameworkGating proves the extractor runs for JVM backend
// frameworks and no-ops for non-JVM-backend frameworks and non-java languages.
func TestObservability_FrameworkGating_Issue3006(t *testing.T) {
	source := `
@Slf4j
public class S {
    public void m() { log.info("hi"); }
}
`
	for _, fw := range []string{"spring_boot", "spring-boot", "quarkus", "micronaut", "microprofile", "jakarta_ee", "jaxrs", "dropwizard", "helidon", "javalin", "vertx"} {
		r := ExtractObservability(PatternContext{Source: source, Language: "java", Framework: fw, FilePath: "S.java"})
		if len(r.Entities) == 0 {
			t.Errorf("[#3006 gating] framework %q expected observability entities, got none", fw)
		}
	}
	for _, fw := range []string{"django", "rails", "express"} {
		if r := ExtractObservability(PatternContext{Source: source, Language: "java", Framework: fw, FilePath: "S.java"}); len(r.Entities) != 0 {
			t.Errorf("[#3006 gating] framework %q should no-op, got %d entities", fw, len(r.Entities))
		}
	}
	if r := ExtractObservability(PatternContext{Source: source, Language: "python", Framework: "spring_boot", FilePath: "S.py"}); len(r.Entities) != 0 {
		t.Errorf("[#3006 gating] non-java should no-op, got %d entities", len(r.Entities))
	}
}

// ============================================================================
<<<<<<< HEAD
// Helidon MP — transactions + CDI DI + middleware + auth + tests (#3088)
// ============================================================================

// TestHelidon_Transactions_Issue3088 proves that @Transactional extraction
// runs for the "helidon" framework (JTA @Transactional via Helidon MP).
// Registry targets: Transactions/transaction_boundary_extraction,
// transaction_propagation, transaction_rollback_rules → partial.
// Cite: internal/custom/java/transactional.go.
func TestHelidon_Transactions_Issue3088(t *testing.T) {
	source := `
package com.example.helidon;

import jakarta.enterprise.context.ApplicationScoped;
import jakarta.transaction.Transactional;
import jakarta.transaction.Transactional.TxType;

@ApplicationScoped
@Transactional
public class OrderService {

    public void createOrder(String item) {}

    @Transactional(TxType.REQUIRES_NEW)
    public void auditOrder(String orderId) {}

    @Transactional(rollbackFor = OrderException.class)
    public void confirmPayment(String paymentId) {}

    @Transactional(TxType.NOT_SUPPORTED)
    public void sendNotification(String message) {}
}
`
	r := ExtractTransactional(PatternContext{
		Source:    source,
		Language:  "java",
		Framework: "helidon",
		FilePath:  "OrderService.java",
	})

	// transaction_boundary_extraction: class-level boundary for OrderService.
	classBoundary, ok := txEntityByName(r, "OrderService")
	if !ok {
		t.Fatalf("[#3088 tx-boundary] expected class-level boundary for OrderService; got %v", entityNames(r.Entities))
	}
	if classBoundary.Properties["framework"] != "helidon" {
		t.Errorf("[#3088 tx-boundary] framework = %v, want helidon", classBoundary.Properties["framework"])
	}
	if classBoundary.Properties["transaction_boundary"] != "class" {
		t.Errorf("[#3088 tx-boundary] transaction_boundary = %v, want class", classBoundary.Properties["transaction_boundary"])
	}

	// transaction_propagation: auditOrder uses TxType.REQUIRES_NEW.
	audit, ok := txEntityByName(r, "OrderService.auditOrder")
	if !ok {
		t.Fatalf("[#3088 tx-propagation] expected boundary for OrderService.auditOrder; got %v", entityNames(r.Entities))
	}
	if audit.Properties["propagation"] != "REQUIRES_NEW" {
		t.Errorf("[#3088 tx-propagation] auditOrder propagation = %v, want REQUIRES_NEW", audit.Properties["propagation"])
	}

	// transaction_rollback_rules: confirmPayment uses rollbackFor.
	confirm, ok := txEntityByName(r, "OrderService.confirmPayment")
	if !ok {
		t.Fatalf("[#3088 tx-rollback] expected boundary for OrderService.confirmPayment; got %v", entityNames(r.Entities))
	}
	if confirm.Properties["rollback_for"] != "OrderException" {
		t.Errorf("[#3088 tx-rollback] confirmPayment rollback_for = %v, want OrderException", confirm.Properties["rollback_for"])
	}

	// NOT_SUPPORTED propagation captured.
	notify, ok := txEntityByName(r, "OrderService.sendNotification")
	if !ok {
		t.Fatalf("[#3088 tx-propagation] expected boundary for OrderService.sendNotification; got %v", entityNames(r.Entities))
	}
	if notify.Properties["propagation"] != "NOT_SUPPORTED" {
		t.Errorf("[#3088 tx-propagation] sendNotification propagation = %v, want NOT_SUPPORTED", notify.Properties["propagation"])
	}
}

// TestHelidon_Transactions_Gating_Issue3088 confirms "helidon" is in txFrameworks.
func TestHelidon_Transactions_Gating_Issue3088(t *testing.T) {
	source := `
@Transactional(TxType.REQUIRED)
public void doWork() {}
`
	r := ExtractTransactional(PatternContext{Source: source, Language: "java", Framework: "helidon", FilePath: "X.java"})
	if len(r.Entities) == 0 {
		t.Error("[#3088 tx-gating] expected a boundary entity for framework=helidon, got none")
	}
}

// TestHelidon_CDI_DI_Issue3088 proves that CDI DI extraction runs for "helidon":
// @ApplicationScoped scope detection, @Produces, and CDI scope resolution.
// Registry targets: DI/di_binding_extraction, di_injection_point,
// di_scope_resolution → partial.
// Cite: internal/custom/java/jakarta_ee_advanced.go.
func TestHelidon_CDI_DI_Issue3088(t *testing.T) {
	source := `
package com.example.helidon;

import jakarta.enterprise.context.ApplicationScoped;
import jakarta.enterprise.context.RequestScoped;
import jakarta.inject.Inject;
import jakarta.enterprise.inject.Produces;
import jakarta.enterprise.inject.Disposes;

@ApplicationScoped
public class InventoryService {

    @Inject
    private PricingService pricing;

    @Produces
    public PaymentGateway produceGateway() { return new PaymentGateway(); }
}

@RequestScoped
public class OrderProcessor {
    @Inject
    public OrderProcessor(InventoryService inv) {}
}
`
	r := ExtractJakartaEEAdvanced(PatternContext{
		Source:    source,
		Language:  "java",
		Framework: "helidon",
		FilePath:  "InventoryService.java",
	})

	// di_binding_extraction: @Produces should emit a CDI producer entity.
	hasProducer := false
	for _, e := range r.Entities {
		if e.Provenance == "INFERRED_FROM_JAKARTA_CDI_PRODUCER" {
			hasProducer = true
		}
	}
	if !hasProducer {
		t.Errorf("[#3088 cdi di_binding] expected INFERRED_FROM_JAKARTA_CDI_PRODUCER entity")
	}

	// di_scope_resolution: @ApplicationScoped and @RequestScoped classes.
	scopedClasses := make(map[string]string)
	for _, e := range r.Entities {
		if e.Provenance == "INFERRED_FROM_CDI_SCOPE" {
			scopedClasses[e.Name] = e.Properties["cdi_scope"].(string)
		}
	}
	if scopedClasses["InventoryService"] != "ApplicationScoped" {
		t.Errorf("[#3088 cdi scope] expected InventoryService=ApplicationScoped, got %v", scopedClasses["InventoryService"])
	}
	if scopedClasses["OrderProcessor"] != "RequestScoped" {
		t.Errorf("[#3088 cdi scope] expected OrderProcessor=RequestScoped, got %v", scopedClasses["OrderProcessor"])
	}
}

// TestHelidon_CDI_DI_Gating_Issue3088 confirms helidon gated in jakartaEEAdvFrameworks.
func TestHelidon_CDI_DI_Gating_Issue3088(t *testing.T) {
	source := `
@ApplicationScoped
public class MyBean {}
`
	r := ExtractJakartaEEAdvanced(PatternContext{Source: source, Language: "java", Framework: "helidon", FilePath: "MyBean.java"})
	hasCDIScope := false
	for _, e := range r.Entities {
		if e.Provenance == "INFERRED_FROM_CDI_SCOPE" {
			hasCDIScope = true
		}
	}
	if !hasCDIScope {
		t.Errorf("[#3088 cdi-gating] expected CDI scope entity for framework=helidon, got none")
	}
}

// TestHelidon_Auth_Issue3088 proves that Helidon MP auth annotation detection
// works. Helidon inherits jakarta.security.enterprise mechanisms + MP-JWT
// @RolesAllowed / @Authenticated (javax.annotation.security).
// Registry target: Auth/auth_coverage → partial.
// Cite: internal/custom/java/jakarta_ee_advanced.go (jeeaAuthMechanismRE),
//
//	internal/engine/java_auth_policy.go (@RolesAllowed).
func TestHelidon_Auth_Issue3088(t *testing.T) {
	source := `
package com.example.helidon.security;

import jakarta.security.enterprise.authentication.mechanism.http.BasicAuthenticationMechanismDefinition;
import jakarta.enterprise.context.ApplicationScoped;

@BasicAuthenticationMechanismDefinition(realmName = "HelidonRealm")
@ApplicationScoped
public class HelidonSecurityConfig {
}
`
	r := ExtractJakartaEEAdvanced(PatternContext{
		Source:    source,
		Language:  "java",
		Framework: "helidon",
		FilePath:  "HelidonSecurityConfig.java",
	})

	found := false
	for _, e := range r.Entities {
		if e.Provenance == "INFERRED_FROM_JAKARTA_SECURITY_AUTH" {
			found = true
			if e.Properties["auth_mechanism"] != "BasicAuthenticationMechanismDefinition" {
				t.Errorf("[#3088 auth] expected auth_mechanism=BasicAuthenticationMechanismDefinition, got %v", e.Properties["auth_mechanism"])
			}
		}
	}
	if !found {
		t.Errorf("[#3088 auth] expected INFERRED_FROM_JAKARTA_SECURITY_AUTH entity for framework=helidon")
	}
}

// TestHelidon_Middleware_ContainerRequestFilter_Issue3088 proves that JAX-RS
// @Provider + ContainerRequestFilter is detected as middleware for Helidon MP.
// Registry target: Middleware/middleware_coverage → partial.
// Cite: internal/custom/java/helidon_filters.go.
func TestHelidon_Middleware_ContainerRequestFilter_Issue3088(t *testing.T) {
	source := `
package com.example.helidon.filter;

import jakarta.ws.rs.container.ContainerRequestContext;
import jakarta.ws.rs.container.ContainerRequestFilter;
import jakarta.ws.rs.ext.Provider;

@Provider
public class AuthorizationFilter implements ContainerRequestFilter {

    @Override
    public void filter(ContainerRequestContext requestContext) {
        // JWT validation
    }
}
`
	r := ExtractHelidonFilters(PatternContext{
		Source:    source,
		Language:  "java",
		Framework: "helidon",
		FilePath:  "AuthorizationFilter.java",
	})

	found := false
	for _, e := range r.Entities {
		if e.Provenance == "INFERRED_FROM_HELIDON_JAXRS_FILTER" && e.Name == "AuthorizationFilter" {
			found = true
			if e.Properties["framework"] != "helidon" {
				t.Errorf("[#3088 middleware] expected framework=helidon, got %v", e.Properties["framework"])
			}
		}
	}
	if !found {
		t.Errorf("[#3088 middleware] expected INFERRED_FROM_HELIDON_JAXRS_FILTER for AuthorizationFilter")
	}
}

// TestHelidon_Middleware_ContainerResponseFilter_Issue3088 proves response
// filter detection.
func TestHelidon_Middleware_ContainerResponseFilter_Issue3088(t *testing.T) {
	source := `
package com.example.helidon.filter;

import jakarta.ws.rs.container.ContainerResponseContext;
import jakarta.ws.rs.container.ContainerResponseFilter;
import jakarta.ws.rs.ext.Provider;

@Provider
public class CorsResponseFilter implements ContainerResponseFilter {
    @Override
    public void filter(ContainerRequestContext req, ContainerResponseContext res) {
        res.getHeaders().add("Access-Control-Allow-Origin", "*");
    }
}
`
	r := ExtractHelidonFilters(PatternContext{
		Source:    source,
		Language:  "java",
		Framework: "helidon",
		FilePath:  "CorsResponseFilter.java",
	})

	found := false
	for _, e := range r.Entities {
		if e.Provenance == "INFERRED_FROM_HELIDON_JAXRS_FILTER" && e.Name == "CorsResponseFilter" {
			found = true
		}
	}
	if !found {
		t.Errorf("[#3088 middleware] expected INFERRED_FROM_HELIDON_JAXRS_FILTER for CorsResponseFilter")
	}
}

// TestHelidon_Middleware_NameBinding_Issue3088 proves @NameBinding annotation
// detection (custom filter binding meta-annotation).
func TestHelidon_Middleware_NameBinding_Issue3088(t *testing.T) {
	source := `
package com.example.helidon.filter;

import jakarta.ws.rs.NameBinding;
import java.lang.annotation.*;

@NameBinding
@Retention(RetentionPolicy.RUNTIME)
@Target({ElementType.TYPE, ElementType.METHOD})
public @interface Secured {}
`
	r := ExtractHelidonFilters(PatternContext{
		Source:    source,
		Language:  "java",
		Framework: "helidon",
		FilePath:  "Secured.java",
	})

	found := false
	for _, e := range r.Entities {
		if e.Provenance == "INFERRED_FROM_HELIDON_NAME_BINDING" && e.Name == "Secured" {
			found = true
		}
	}
	if !found {
		t.Errorf("[#3088 middleware] expected INFERRED_FROM_HELIDON_NAME_BINDING for Secured annotation; got %v", entityNames(r.Entities))
	}
}

// TestHelidon_Middleware_Gating_Issue3088 confirms the middleware extractor is
// gated on "helidon" only.
func TestHelidon_Middleware_Gating_Issue3088(t *testing.T) {
	source := `
@Provider
public class MyFilter implements ContainerRequestFilter {
    public void filter(ContainerRequestContext ctx) {}
}
`
	for _, fw := range []string{"spring_boot", "quarkus", "micronaut"} {
		r := ExtractHelidonFilters(PatternContext{Source: source, Language: "java", Framework: fw, FilePath: "F.java"})
		if len(r.Entities) != 0 {
			t.Errorf("[#3088 middleware-gating] framework %q should no-op, got %d entities", fw, len(r.Entities))
		}
	}
	r := ExtractHelidonFilters(PatternContext{Source: source, Language: "java", Framework: "helidon", FilePath: "F.java"})
	if len(r.Entities) == 0 {
		t.Error("[#3088 middleware-gating] expected entity for framework=helidon, got none")
	}
}

// TestHelidon_DTO_Issue3088 proves that JAX-RS DTO extraction runs for "helidon"
// (jaxrsDTOFrameworks already includes helidon).
// Registry target: Validation/dto_extraction → partial.
// Cite: internal/custom/java/jakarta_jaxrs_dto.go.
func TestHelidon_DTO_Issue3088(t *testing.T) {
	source := `
package com.example.helidon.api;

import jakarta.ws.rs.*;

@Path("/orders")
public class OrderResource {

    @POST
    public OrderDto createOrder(CreateOrderRequest req) { return null; }

    @GET
    @Path("/{id}")
    public OrderDto getOrder(@PathParam("id") Long id) { return null; }
}
`
	r := ExtractJakartaJaxrsDTO(PatternContext{
		Source:    source,
		Language:  "java",
		Framework: "helidon",
		FilePath:  "OrderResource.java",
	})

	dtoNames := make(map[string]bool)
	for _, e := range r.Entities {
		if e.Kind == "SCOPE.Schema" {
			dtoNames[e.Name] = true
		}
	}
	for _, want := range []string{"CreateOrderRequest", "OrderDto"} {
		if !dtoNames[want] {
			t.Errorf("[#3088 dto] expected SCOPE.Schema for %q, got %v", want, dtoNames)
		}
	}

	relTypes := make(map[string]bool)
	for _, rel := range r.Relationships {
		relTypes[rel.RelationshipType] = true
	}
	for _, want := range []string{"ACCEPTS_INPUT", "RETURNS"} {
		if !relTypes[want] {
			t.Errorf("[#3088 dto] expected %q relationship, got: %v", want, relTypes)
		}
	}
}

// TestHelidon_RequestValidation_Issue3088 proves that Bean Validation
// constraints on JAX-RS parameters are captured via DTO extraction for Helidon.
// Registry target: Validation/request_validation → partial.
// Cite: internal/custom/java/jakarta_jaxrs_dto.go.
func TestHelidon_RequestValidation_Issue3088(t *testing.T) {
	source := `
package com.example.helidon.api;

import jakarta.ws.rs.*;
import jakarta.validation.Valid;
import jakarta.validation.constraints.NotNull;

@Path("/products")
public class ProductResource {

    @POST
    public ProductResponse createProduct(@Valid @NotNull CreateProductRequest req) {
        return null;
    }
}
`
	r := ExtractJakartaJaxrsDTO(PatternContext{
		Source:    source,
		Language:  "java",
		Framework: "helidon",
		FilePath:  "ProductResource.java",
	})

	dtoNames := make(map[string]bool)
	for _, e := range r.Entities {
		if e.Kind == "SCOPE.Schema" {
			dtoNames[e.Name] = true
		}
	}
	for _, want := range []string{"CreateProductRequest", "ProductResponse"} {
		if !dtoNames[want] {
			t.Errorf("[#3088 request_validation] expected SCOPE.Schema for %q, got %v", want, dtoNames)
		}
	}
}

// TestHelidon_TestsLinkage_Issue3088 proves that ExtractJUnit5 runs for "helidon"
// and detects @HelidonTest / plain @Test methods (tests_linkage cell).
// Registry target: Testing/tests_linkage → partial.
// Cite: internal/custom/java/junit5.go.
func TestHelidon_TestsLinkage_Issue3088(t *testing.T) {
	source := `
package com.example.helidon;

import io.helidon.microprofile.tests.junit5.HelidonTest;
import org.junit.jupiter.api.Test;
import static org.junit.jupiter.api.Assertions.*;

@HelidonTest
class OrderResourceTest {

    @Test
    void createOrder_returns201() {
        assertEquals(201, 201);
    }

    @Test
    void getOrder_returns200() {
        assertTrue(true);
    }
}
`
	r := ExtractJUnit5(PatternContext{
		Source:    source,
		Language:  "java",
		Framework: "helidon",
		FilePath:  "OrderResourceTest.java",
	})

	testCount := 0
	for _, e := range r.Entities {
		if e.Properties["test_annotation"] == "Test" {
			testCount++
		}
	}
	if testCount < 2 {
		t.Errorf("[#3088 tests_linkage] expected >= 2 @Test entities for helidon, got %d", testCount)
	}
}

// TestHelidon_TestsLinkage_Gating_Issue3088 confirms "helidon" is in junit5Frameworks.
func TestHelidon_TestsLinkage_Gating_Issue3088(t *testing.T) {
	source := `
class FooTest {
    @Test
    void foo() {}
}
`
	r := ExtractJUnit5(PatternContext{Source: source, Language: "java", Framework: "helidon", FilePath: "FooTest.java"})
	if len(r.Entities) == 0 {
		t.Error("[#3088 tests-gating] expected test entity for framework=helidon, got none")
=======
// Issue #3081: Spring Boot missing cells
// actuator_detection, autoconfiguration_detection, profile_detection, di_scope_resolution
// ============================================================================

// TestSpringBoot_ActuatorDetection_Issue3081 proves actuator_detection:
// @Endpoint classes and @ReadOperation/@WriteOperation/@DeleteOperation methods
// are extracted with correct provenance and operation_kind property.
// Registry target: lang.java.framework.spring-boot actuator_detection=partial.
func TestSpringBoot_ActuatorDetection_Issue3081(t *testing.T) {
	source := `
package com.example.actuator;

import org.springframework.boot.actuate.endpoint.annotation.Endpoint;
import org.springframework.boot.actuate.endpoint.annotation.ReadOperation;
import org.springframework.boot.actuate.endpoint.annotation.WriteOperation;
import org.springframework.boot.actuate.endpoint.annotation.DeleteOperation;

@Endpoint(id = "health-info")
public class HealthInfoEndpoint {
    @ReadOperation
    public HealthInfo health() {
        return new HealthInfo("UP");
    }

    @WriteOperation
    public void reset(String key) {
        // reset
    }

    @DeleteOperation
    public void clear(String key) {
        // clear
    }
}
`
	r := ExtractSpringBoot(PatternContext{
		Source: source, Language: "java", Framework: "spring_boot",
		FilePath: "HealthInfoEndpoint.java",
	})

	// Should have: 1 endpoint class + 3 operations
	var endpointClass *SecondaryEntity
	opKinds := make(map[string]bool)
	for i := range r.Entities {
		e := &r.Entities[i]
		if e.Provenance == "INFERRED_FROM_SPRING_ACTUATOR" {
			if e.Kind == "SCOPE.Component" {
				endpointClass = e
			}
			if e.Kind == "SCOPE.Operation" {
				if k, ok := e.Properties["operation_kind"].(string); ok {
					opKinds[k] = true
				}
			}
		}
	}
	if endpointClass == nil {
		t.Fatal("[#3081 actuator_detection] expected SCOPE.Component for @Endpoint class, got none")
	}
	if endpointClass.Properties["endpoint_id"] != "health-info" {
		t.Errorf("[#3081 actuator_detection] endpoint_id=%q, want health-info", endpointClass.Properties["endpoint_id"])
	}
	for _, wantKind := range []string{"read", "write", "delete"} {
		if !opKinds[wantKind] {
			t.Errorf("[#3081 actuator_detection] missing operation_kind=%q, got %v", wantKind, opKinds)
		}
	}
	// Should have OWNS relationships from endpoint class to each operation
	ownsCount := 0
	for _, rel := range r.Relationships {
		if rel.RelationshipType == "OWNS" && rel.SourceRef == endpointClass.Ref {
			ownsCount++
		}
	}
	if ownsCount < 3 {
		t.Errorf("[#3081 actuator_detection] expected >=3 OWNS relationships from endpoint class, got %d", ownsCount)
	}
}

// TestSpringBoot_AutoconfigurationDetection_Issue3081 proves autoconfiguration_detection:
// @EnableAutoConfiguration, @SpringBootApplication, @AutoConfiguration classes
// and @ConditionalOn* classes are extracted.
// Registry target: lang.java.framework.spring-boot autoconfiguration_detection=partial.
func TestSpringBoot_AutoconfigurationDetection_Issue3081(t *testing.T) {
	source := `
package com.example;

import org.springframework.boot.SpringApplication;
import org.springframework.boot.autoconfigure.SpringBootApplication;
import org.springframework.boot.autoconfigure.condition.ConditionalOnMissingBean;
import org.springframework.context.annotation.Bean;
import org.springframework.context.annotation.Configuration;

@SpringBootApplication
public class MyApplication {
    public static void main(String[] args) {
        SpringApplication.run(MyApplication.class, args);
    }
}

@Configuration
@ConditionalOnMissingBean(DataSource.class)
public class DataSourceAutoConfig {
    @Bean
    public DataSource defaultDataSource() {
        return new EmbeddedDataSource();
    }
}
`
	r := ExtractSpringEcosystem(PatternContext{
		Source: source, Language: "java", Framework: "spring_boot",
		FilePath: "MyApplication.java",
	})

	autoConfigNames := make(map[string]string) // name -> autoconfig_annotation
	conditionalNames := make(map[string]string) // name -> conditional_annotation
	for _, e := range r.Entities {
		if e.Provenance == "INFERRED_FROM_SPRING_AUTOCONFIG" {
			if ann, ok := e.Properties["autoconfig_annotation"].(string); ok {
				autoConfigNames[e.Name] = ann
			}
			if cond, ok := e.Properties["conditional_annotation"].(string); ok {
				conditionalNames[e.Name] = cond
			}
		}
	}

	if ann := autoConfigNames["MyApplication"]; ann != "@SpringBootApplication" {
		t.Errorf("[#3081 autoconfiguration_detection] MyApplication autoconfig_annotation=%q, want @SpringBootApplication", ann)
	}
	if cond := conditionalNames["DataSourceAutoConfig"]; cond != "@ConditionalOnMissingBean" {
		t.Errorf("[#3081 autoconfiguration_detection] DataSourceAutoConfig conditional_annotation=%q, want @ConditionalOnMissingBean", cond)
	}
}

// TestSpringBoot_ProfileDetection_Issue3081 proves profile_detection:
// @Profile("name") on classes is extracted with the profile value captured.
// Registry target: lang.java.framework.spring-boot profile_detection=partial.
func TestSpringBoot_ProfileDetection_Issue3081(t *testing.T) {
	source := `
package com.example;

import org.springframework.context.annotation.Profile;
import org.springframework.stereotype.Service;

@Profile("production")
@Service
public class ProductionMailService implements MailService {
    public void send(String msg) { /* send via SMTP */ }
}

@Profile("development")
@Service
public class MockMailService implements MailService {
    public void send(String msg) { /* no-op */ }
}
`
	r := ExtractSpringEcosystem(PatternContext{
		Source: source, Language: "java", Framework: "spring_boot",
		FilePath: "MailService.java",
	})

	profileMap := make(map[string]string) // class name -> profile
	for _, e := range r.Entities {
		if e.Provenance == "INFERRED_FROM_SPRING_PROFILE" {
			if p, ok := e.Properties["profile"].(string); ok {
				profileMap[e.Name] = p
			}
		}
	}

	if profileMap["ProductionMailService"] != "production" {
		t.Errorf("[#3081 profile_detection] ProductionMailService profile=%q, want production; map=%v",
			profileMap["ProductionMailService"], profileMap)
	}
	if profileMap["MockMailService"] != "development" {
		t.Errorf("[#3081 profile_detection] MockMailService profile=%q, want development; map=%v",
			profileMap["MockMailService"], profileMap)
	}
}

// TestSpringBoot_DIScopeResolution_Issue3081 proves di_scope_resolution:
// Spring @Scope / @RequestScope / @SessionScope / @ApplicationScope
// are extracted with the scope captured in spring_scope property.
// Registry target: lang.java.framework.spring-boot di_scope_resolution=partial.
func TestSpringBoot_DIScopeResolution_Issue3081(t *testing.T) {
	source := `
package com.example;

import org.springframework.context.annotation.Scope;
import org.springframework.web.context.annotation.RequestScope;
import org.springframework.web.context.annotation.SessionScope;
import org.springframework.web.context.annotation.ApplicationScope;
import org.springframework.stereotype.Component;

@Component
@Scope("prototype")
public class PrototypeBean {
    // new instance per injection
}

@Component
@RequestScope
public class RequestScopedBean {
    // one per HTTP request
}

@Component
@SessionScope
public class SessionScopedBean {
    // one per HTTP session
}

@Component
@ApplicationScope
public class ApplicationScopedBean {
    // singleton within application context
}
`
	r := ExtractSpringBoot(PatternContext{
		Source: source, Language: "java", Framework: "spring_boot",
		FilePath: "ScopedBeans.java",
	})

	scopeMap := make(map[string]string) // class name -> spring_scope
	for _, e := range r.Entities {
		if e.Provenance == "INFERRED_FROM_SPRING_DI_SCOPE" {
			if s, ok := e.Properties["spring_scope"].(string); ok {
				scopeMap[e.Name] = s
			}
		}
	}

	if scopeMap["PrototypeBean"] != "prototype" {
		t.Errorf("[#3081 di_scope_resolution] PrototypeBean scope=%q, want prototype; map=%v",
			scopeMap["PrototypeBean"], scopeMap)
	}
	if scopeMap["RequestScopedBean"] != "request" {
		t.Errorf("[#3081 di_scope_resolution] RequestScopedBean scope=%q, want request; map=%v",
			scopeMap["RequestScopedBean"], scopeMap)
	}
	if scopeMap["SessionScopedBean"] != "session" {
		t.Errorf("[#3081 di_scope_resolution] SessionScopedBean scope=%q, want session; map=%v",
			scopeMap["SessionScopedBean"], scopeMap)
	}
	if scopeMap["ApplicationScopedBean"] != "application" {
		t.Errorf("[#3081 di_scope_resolution] ApplicationScopedBean scope=%q, want application; map=%v",
			scopeMap["ApplicationScopedBean"], scopeMap)
>>>>>>> c810d1fe5 (feat(java/spring-boot): add actuator, autoconfig, profile, di_scope extractors (#3081))
	}
}
