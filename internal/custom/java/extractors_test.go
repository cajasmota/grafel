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
		// MX-1096 (Option A): Panache ORM entities are class-like → SCOPE.Component.
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
		// MX-1096 (Option A): Panache Mongo entities are class-like → SCOPE.Component.
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
// Kind value outside the 14-type SCOPE allowlist (MX-1092).
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
