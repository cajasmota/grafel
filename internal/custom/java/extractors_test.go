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
