package java

import (
	"sort"
	"testing"

	"github.com/cajasmota/archigraph/internal/types"
)

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

func entityNameSet(entities []types.EntityRecord) map[string]bool {
	out := make(map[string]bool, len(entities))
	for _, e := range entities {
		out[e.Name] = true
	}
	return out
}

func sortedEntityNames(entities []types.EntityRecord) []string {
	names := make([]string, 0, len(entities))
	for _, e := range entities {
		names = append(names, e.Name)
	}
	sort.Strings(names)
	return names
}

func findEntity(entities []types.EntityRecord, name string) (types.EntityRecord, bool) {
	for _, e := range entities {
		if e.Name == name {
			return e, true
		}
	}
	return types.EntityRecord{}, false
}

// ---------------------------------------------------------------------------
// detectedAnnotations
// ---------------------------------------------------------------------------

func TestDetectedAnnotations_Builder(t *testing.T) {
	src := "@Builder\npublic class Order {"
	anns := detectedAnnotations(src)
	if !anns["Builder"] {
		t.Errorf("expected Builder in %v", anns)
	}
}

func TestDetectedAnnotations_LombokPackagePrefix(t *testing.T) {
	src := "@lombok.Builder\npublic class Foo {"
	anns := detectedAnnotations(src)
	if !anns["Builder"] {
		t.Errorf("expected Builder (stripped from lombok.Builder) in %v", anns)
	}
}

func TestDetectedAnnotations_Multiple(t *testing.T) {
	src := "@Data\n@Builder\n@NoArgsConstructor\npublic class Dto {"
	anns := detectedAnnotations(src)
	for _, want := range []string{"Data", "Builder", "NoArgsConstructor"} {
		if !anns[want] {
			t.Errorf("expected %s in %v", want, anns)
		}
	}
}

func TestDetectedAnnotations_BuilderDefault(t *testing.T) {
	src := "@Builder.Default\nprivate String status = \"active\";"
	anns := detectedAnnotations(src)
	if !anns["Builder"] {
		t.Errorf("expected Builder (via Builder.Default) in %v", anns)
	}
	if !anns["Builder.Default"] {
		t.Errorf("expected Builder.Default in %v", anns)
	}
}

func TestDetectedAnnotations_NonLombok(t *testing.T) {
	src := "@Entity\n@Table(name=\"orders\")\npublic class Order {"
	anns := detectedAnnotations(src)
	if len(anns) != 0 {
		t.Errorf("expected no Lombok annotations, got %v", anns)
	}
}

func TestDetectedAnnotations_SuperBuilder(t *testing.T) {
	src := "@SuperBuilder\npublic class Invoice extends Base {"
	anns := detectedAnnotations(src)
	if !anns["SuperBuilder"] {
		t.Errorf("expected SuperBuilder in %v", anns)
	}
}

// ---------------------------------------------------------------------------
// isAccessorsChain
// ---------------------------------------------------------------------------

func TestIsAccessorsChain_True(t *testing.T) {
	if !isAccessorsChain("@Accessors(chain = true)\npublic class Foo {") {
		t.Error("expected chain=true to be detected")
	}
}

func TestIsAccessorsChain_TrueNoSpaces(t *testing.T) {
	if !isAccessorsChain("@Accessors(chain=true)\npublic class Foo {") {
		t.Error("expected chain=true to be detected without spaces")
	}
}

func TestIsAccessorsChain_False_NoChain(t *testing.T) {
	if isAccessorsChain("@Accessors(fluent = true)\npublic class Foo {") {
		t.Error("expected chain=true not detected when absent")
	}
}

func TestIsAccessorsChain_False_NoAccessors(t *testing.T) {
	if isAccessorsChain("@Data\npublic class Foo {") {
		t.Error("expected chain=false when @Accessors not present")
	}
}

// ---------------------------------------------------------------------------
// collectLombokFields / parseFieldLine
// ---------------------------------------------------------------------------

func TestCollectLombokFields_Basic(t *testing.T) {
	body := "\n    private String id;\n    private BigDecimal total;\n    private String status;\n"
	fields := collectLombokFields(body)
	if len(fields) != 3 {
		t.Fatalf("expected 3 fields, got %d: %+v", len(fields), fields)
	}
	want := []struct{ name, typ string }{
		{"id", "String"},
		{"total", "BigDecimal"},
		{"status", "String"},
	}
	for i, w := range want {
		if fields[i].name != w.name || fields[i].typeName != w.typ {
			t.Errorf("field[%d]: got name=%q typ=%q want name=%q typ=%q",
				i, fields[i].name, fields[i].typeName, w.name, w.typ)
		}
	}
}

func TestCollectLombokFields_Generic(t *testing.T) {
	body := "    private List<String> tags;"
	fields := collectLombokFields(body)
	if len(fields) != 1 {
		t.Fatalf("expected 1 field, got %d", len(fields))
	}
	if fields[0].typeName != "List" {
		t.Errorf("expected leaf type List, got %q", fields[0].typeName)
	}
	if fields[0].rawType != "List<String>" {
		t.Errorf("expected rawType List<String>, got %q", fields[0].rawType)
	}
}

func TestCollectLombokFields_WithAnnotations(t *testing.T) {
	body := "\n    @Getter\n    @Setter\n    private String name;\n    @Singular\n    private List<String> tags;\n"
	fields := collectLombokFields(body)
	if len(fields) != 2 {
		t.Fatalf("expected 2 fields, got %d: %+v", len(fields), fields)
	}
	if !fields[0].annotations["Getter"] || !fields[0].annotations["Setter"] {
		t.Errorf("field name: expected Getter+Setter annotations, got %v", fields[0].annotations)
	}
	if !fields[1].isSingular {
		t.Errorf("field tags: expected isSingular=true")
	}
}

func TestCollectLombokFields_SkipsMethodLines(t *testing.T) {
	body := "\n    private String name;\n    public String getName() { return name; }\n    private int age;\n"
	fields := collectLombokFields(body)
	if len(fields) != 2 {
		t.Fatalf("expected 2 fields (skipping method), got %d: %+v", len(fields), fields)
	}
}

func TestCollectLombokFields_FinalField(t *testing.T) {
	body := "    private final Repo repo;"
	fields := collectLombokFields(body)
	if len(fields) != 1 || fields[0].name != "repo" {
		t.Fatalf("expected field repo, got %+v", fields)
	}
}

// ---------------------------------------------------------------------------
// leafTypeFromString
// ---------------------------------------------------------------------------

func TestLeafTypeFromString(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"String", "String"},
		{"List<String>", "List"},
		{"Map<String, Integer>", "Map"},
		{"String[]", "String"},
		{"com.example.Foo", "Foo"},
		{"BigDecimal", "BigDecimal"},
	}
	for _, c := range cases {
		got := leafTypeFromString(c.in)
		if got != c.want {
			t.Errorf("leafTypeFromString(%q) = %q want %q", c.in, got, c.want)
		}
	}
}

// ---------------------------------------------------------------------------
// capitalise
// ---------------------------------------------------------------------------

func TestCapitalise(t *testing.T) {
	cases := []struct{ in, want string }{
		{"id", "Id"},
		{"name", "Name"},
		{"Name", "Name"},
		{"", ""},
		{"x", "X"},
	}
	for _, c := range cases {
		if got := capitalise(c.in); got != c.want {
			t.Errorf("capitalise(%q) = %q want %q", c.in, got, c.want)
		}
	}
}

// ---------------------------------------------------------------------------
// synthesizeLombokEntities — @Builder
// ---------------------------------------------------------------------------

func TestSynthesizeLombok_Builder_BasicEntities(t *testing.T) {
	decl := "@Builder\npublic class Order"
	body := "\n    private String id;\n    private BigDecimal total;\n"
	out := synthesizeLombokEntities("Order", decl, body, "Order.java")

	names := entityNameSet(out)
	wantNames := []string{
		"OrderBuilder",      // class entity
		"Order.builder",     // static factory
		"OrderBuilder.id",   // fluent setter
		"OrderBuilder.total",
		"OrderBuilder.build",
	}
	for _, want := range wantNames {
		if !names[want] {
			t.Errorf("@Builder: missing entity %q in %v", want, sortedEntityNames(out))
		}
	}
}

func TestSynthesizeLombok_Builder_OrderBuilderIsComponent(t *testing.T) {
	out := synthesizeLombokEntities("Order", "@Builder\npublic class Order", "    private String id;\n", "Order.java")
	e, ok := findEntity(out, "OrderBuilder")
	if !ok {
		t.Fatal("OrderBuilder entity not found")
	}
	if e.Kind != "SCOPE.Component" {
		t.Errorf("OrderBuilder kind: got %q want SCOPE.Component", e.Kind)
	}
	if e.Subtype != "class" {
		t.Errorf("OrderBuilder subtype: got %q want class", e.Subtype)
	}
}

func TestSynthesizeLombok_Builder_SynthesizedFromProperty(t *testing.T) {
	out := synthesizeLombokEntities("Order", "@Builder\npublic class Order", "    private String id;\n", "Order.java")
	for _, e := range out {
		if e.Properties["synthesized_from"] == "" {
			t.Errorf("entity %q missing synthesized_from property", e.Name)
		}
	}
}

func TestSynthesizeLombok_Builder_PatternTypeProperty(t *testing.T) {
	out := synthesizeLombokEntities("Order", "@Builder\npublic class Order", "    private String id;\n", "Order.java")
	for _, e := range out {
		if e.Properties["pattern_type"] == "" {
			t.Errorf("entity %q missing pattern_type property", e.Name)
		}
	}
}

func TestSynthesizeLombok_Builder_BuildReturnsClass(t *testing.T) {
	out := synthesizeLombokEntities("Order", "@Builder\npublic class Order", "    private String id;\n", "Order.java")
	e, ok := findEntity(out, "OrderBuilder.build")
	if !ok {
		t.Fatal("OrderBuilder.build not found")
	}
	if e.Properties["returns"] != "Order" {
		t.Errorf("build returns: got %q want Order", e.Properties["returns"])
	}
}

func TestSynthesizeLombok_Builder_FactoryIsStatic(t *testing.T) {
	out := synthesizeLombokEntities("Order", "@Builder\npublic class Order", "    private String id;\n", "Order.java")
	e, ok := findEntity(out, "Order.builder")
	if !ok {
		t.Fatal("Order.builder not found")
	}
	if e.Properties["is_static"] != "true" {
		t.Errorf("builder factory: expected is_static=true, got %q", e.Properties["is_static"])
	}
}

func TestSynthesizeLombok_Builder_QualityScore(t *testing.T) {
	out := synthesizeLombokEntities("Order", "@Builder\npublic class Order", "    private String id;\n", "Order.java")
	for _, e := range out {
		if e.QualityScore != lombokSynthQuality {
			t.Errorf("entity %q: quality_score=%v want %v", e.Name, e.QualityScore, lombokSynthQuality)
		}
	}
}

func TestSynthesizeLombok_Builder_Language(t *testing.T) {
	out := synthesizeLombokEntities("Order", "@Builder\npublic class Order", "    private String id;\n", "Order.java")
	for _, e := range out {
		if e.Language != "java" {
			t.Errorf("entity %q: language=%q want java", e.Name, e.Language)
		}
	}
}

func TestSynthesizeLombok_Builder_SourceFile(t *testing.T) {
	out := synthesizeLombokEntities("Order", "@Builder\npublic class Order", "    private String id;\n", "src/Order.java")
	for _, e := range out {
		if e.SourceFile != "src/Order.java" {
			t.Errorf("entity %q: source_file=%q want src/Order.java", e.Name, e.SourceFile)
		}
	}
}

// ---------------------------------------------------------------------------
// synthesizeLombokEntities — @SuperBuilder
// ---------------------------------------------------------------------------

func TestSynthesizeLombok_SuperBuilder(t *testing.T) {
	out := synthesizeLombokEntities("Invoice", "@SuperBuilder\npublic class Invoice extends BaseEntity", "    private String ref;\n", "Invoice.java")
	names := entityNameSet(out)
	if !names["InvoiceBuilder"] {
		t.Error("missing InvoiceBuilder class for @SuperBuilder")
	}
	e, ok := findEntity(out, "InvoiceBuilder")
	if ok && e.Properties["synthesized_from"] != "lombok_super_builder" {
		t.Errorf("InvoiceBuilder synthesized_from: got %q want lombok_super_builder",
			e.Properties["synthesized_from"])
	}
}

// ---------------------------------------------------------------------------
// synthesizeLombokEntities — @Singular
// ---------------------------------------------------------------------------

func TestSynthesizeLombok_Singular(t *testing.T) {
	decl := "@Builder\npublic class Request"
	body := "\n    @Singular\n    private List<String> tags;\n"
	out := synthesizeLombokEntities("Request", decl, body, "Request.java")
	names := entityNameSet(out)
	// @Singular on "tags" (strips trailing 's') → addTag + tags(Iterable)
	if !names["RequestBuilder.addTag"] {
		t.Errorf("missing RequestBuilder.addTag, got %v", sortedEntityNames(out))
	}
	if !names["RequestBuilder.tags"] {
		t.Errorf("missing RequestBuilder.tags (Iterable form), got %v", sortedEntityNames(out))
	}
}

// ---------------------------------------------------------------------------
// synthesizeLombokEntities — @Data
// ---------------------------------------------------------------------------

func TestSynthesizeLombok_Data(t *testing.T) {
	out := synthesizeLombokEntities("User", "@Data\npublic class User",
		"    private String name;\n    private int age;\n", "User.java")
	names := entityNameSet(out)
	wantNames := []string{
		"User.User",    // constructor
		"User.getName",
		"User.setName",
		"User.getAge",
		"User.setAge",
		"User.equals",
		"User.hashCode",
		"User.toString",
	}
	for _, want := range wantNames {
		if !names[want] {
			t.Errorf("@Data: missing %q in %v", want, sortedEntityNames(out))
		}
	}
}

func TestSynthesizeLombok_Data_ConstructorIsConstructorSubtype(t *testing.T) {
	out := synthesizeLombokEntities("User", "@Data\npublic class User", "    private String name;\n", "User.java")
	e, ok := findEntity(out, "User.User")
	if !ok {
		t.Fatal("User.User not found")
	}
	if e.Subtype != "constructor" {
		t.Errorf("User.User subtype: got %q want constructor", e.Subtype)
	}
}

// ---------------------------------------------------------------------------
// synthesizeLombokEntities — @Value
// ---------------------------------------------------------------------------

func TestSynthesizeLombok_Value(t *testing.T) {
	out := synthesizeLombokEntities("Point", "@Value\npublic class Point",
		"    private int x;\n    private int y;\n", "Point.java")
	names := entityNameSet(out)
	if !names["Point.Point"] {
		t.Errorf("@Value: missing constructor Point.Point in %v", sortedEntityNames(out))
	}
	if !names["Point.getX"] {
		t.Errorf("@Value: missing Point.getX in %v", sortedEntityNames(out))
	}
	if !names["Point.getY"] {
		t.Errorf("@Value: missing Point.getY in %v", sortedEntityNames(out))
	}
	// @Value must NOT generate setters (immutable class)
	if names["Point.setX"] {
		t.Error("@Value must not generate setters")
	}
	if names["Point.setY"] {
		t.Error("@Value must not generate setters")
	}
}

// ---------------------------------------------------------------------------
// synthesizeLombokEntities — @Getter / @Setter (class level)
// ---------------------------------------------------------------------------

func TestSynthesizeLombok_ClassLevelGetter(t *testing.T) {
	out := synthesizeLombokEntities("Product", "@Getter\npublic class Product",
		"    private String sku;\n    private double price;\n", "Product.java")
	names := entityNameSet(out)
	if !names["Product.getSku"] {
		t.Errorf("@Getter: missing Product.getSku in %v", sortedEntityNames(out))
	}
	if !names["Product.getPrice"] {
		t.Errorf("@Getter: missing Product.getPrice in %v", sortedEntityNames(out))
	}
	// No setters for @Getter alone
	if names["Product.setSku"] {
		t.Error("@Getter alone must not generate setters")
	}
}

func TestSynthesizeLombok_ClassLevelSetter(t *testing.T) {
	out := synthesizeLombokEntities("Config", "@Setter\npublic class Config",
		"    private String host;\n", "Config.java")
	names := entityNameSet(out)
	if !names["Config.setHost"] {
		t.Errorf("@Setter: missing Config.setHost in %v", sortedEntityNames(out))
	}
	if names["Config.getHost"] {
		t.Error("@Setter alone must not generate getters")
	}
}

func TestSynthesizeLombok_BooleanGetter(t *testing.T) {
	out := synthesizeLombokEntities("Flag", "@Getter\npublic class Flag",
		"    private boolean active;\n", "Flag.java")
	names := entityNameSet(out)
	if !names["Flag.isActive"] {
		t.Errorf("boolean @Getter: expected isActive for boolean field, got %v", sortedEntityNames(out))
	}
}

// ---------------------------------------------------------------------------
// synthesizeLombokEntities — @AllArgsConstructor / @RequiredArgsConstructor / @NoArgsConstructor
// ---------------------------------------------------------------------------

func TestSynthesizeLombok_AllArgsConstructor(t *testing.T) {
	out := synthesizeLombokEntities("Payload", "@AllArgsConstructor\npublic class Payload",
		"    private String a;\n    private int b;\n", "Payload.java")
	names := entityNameSet(out)
	if !names["Payload.Payload"] {
		t.Errorf("@AllArgsConstructor: missing Payload.Payload in %v", sortedEntityNames(out))
	}
	e, ok := findEntity(out, "Payload.Payload")
	if ok && e.Subtype != "constructor" {
		t.Errorf("expected subtype=constructor, got %q", e.Subtype)
	}
	if ok && e.Properties["constructor_kind"] != "all" {
		t.Errorf("expected constructor_kind=all, got %q", e.Properties["constructor_kind"])
	}
}

func TestSynthesizeLombok_NoArgsConstructor(t *testing.T) {
	out := synthesizeLombokEntities("Entity", "@NoArgsConstructor\npublic class Entity",
		"    private Long id;\n", "Entity.java")
	names := entityNameSet(out)
	if !names["Entity.Entity"] {
		t.Errorf("@NoArgsConstructor: missing Entity.Entity in %v", sortedEntityNames(out))
	}
	e, ok := findEntity(out, "Entity.Entity")
	if ok && e.Properties["constructor_kind"] != "none" {
		t.Errorf("expected constructor_kind=none, got %q", e.Properties["constructor_kind"])
	}
}

func TestSynthesizeLombok_RequiredArgsConstructor(t *testing.T) {
	out := synthesizeLombokEntities("Service", "@RequiredArgsConstructor\npublic class Service",
		"    private final Repo repo;\n", "Service.java")
	names := entityNameSet(out)
	if !names["Service.Service"] {
		t.Errorf("@RequiredArgsConstructor: missing Service.Service in %v", sortedEntityNames(out))
	}
}

// ---------------------------------------------------------------------------
// synthesizeLombokEntities — @With
// ---------------------------------------------------------------------------

func TestSynthesizeLombok_With(t *testing.T) {
	out := synthesizeLombokEntities("Record", "@With\npublic class Record",
		"    private String name;\n    private int version;\n", "Record.java")
	names := entityNameSet(out)
	if !names["Record.withName"] {
		t.Errorf("@With: missing Record.withName in %v", sortedEntityNames(out))
	}
	if !names["Record.withVersion"] {
		t.Errorf("@With: missing Record.withVersion in %v", sortedEntityNames(out))
	}
	e, ok := findEntity(out, "Record.withName")
	if ok && e.Properties["returns"] != "Record" {
		t.Errorf("withName: expected returns=Record, got %q", e.Properties["returns"])
	}
}

// ---------------------------------------------------------------------------
// synthesizeLombokEntities — @Accessors(chain=true)
// ---------------------------------------------------------------------------

func TestSynthesizeLombok_AccessorsChain(t *testing.T) {
	out := synthesizeLombokEntities("FluentBuilder", "@Accessors(chain = true)\npublic class FluentBuilder",
		"    private String name;\n", "FluentBuilder.java")
	names := entityNameSet(out)
	if !names["FluentBuilder.setName"] {
		t.Errorf("@Accessors chain: missing FluentBuilder.setName in %v", sortedEntityNames(out))
	}
	e, ok := findEntity(out, "FluentBuilder.setName")
	if ok && e.Properties["chain"] != "true" {
		t.Errorf("setName: expected chain=true property, got %q", e.Properties["chain"])
	}
}

// ---------------------------------------------------------------------------
// synthesizeFieldLevelLombok
// ---------------------------------------------------------------------------

func TestSynthesizeFieldLevel_GetterAndSetter(t *testing.T) {
	body := "\n    @Getter\n    @Setter\n    private String email;\n    private String other;\n"
	out := synthesizeFieldLevelLombok("Customer", body, "Customer.java")
	names := entityNameSet(out)
	if !names["Customer.getEmail"] {
		t.Errorf("field-level @Getter: missing Customer.getEmail in %v", sortedEntityNames(out))
	}
	if !names["Customer.setEmail"] {
		t.Errorf("field-level @Setter: missing Customer.setEmail in %v", sortedEntityNames(out))
	}
	// 'other' has no annotation — must NOT be synthesized
	if names["Customer.getOther"] {
		t.Error("field without @Getter must not get getter")
	}
	if names["Customer.setOther"] {
		t.Error("field without @Setter must not get setter")
	}
}

func TestSynthesizeFieldLevel_With(t *testing.T) {
	body := "\n    @With\n    private String status;\n"
	out := synthesizeFieldLevelLombok("Event", body, "Event.java")
	names := entityNameSet(out)
	if !names["Event.withStatus"] {
		t.Errorf("field-level @With: missing Event.withStatus in %v", sortedEntityNames(out))
	}
}

func TestSynthesizeFieldLevel_LevelProperty(t *testing.T) {
	body := "\n    @Getter\n    private String name;\n"
	out := synthesizeFieldLevelLombok("Foo", body, "Foo.java")
	e, ok := findEntity(out, "Foo.getName")
	if !ok {
		t.Fatal("Foo.getName not found")
	}
	if e.Properties["level"] != "field" {
		t.Errorf("expected level=field, got %q", e.Properties["level"])
	}
}

// ---------------------------------------------------------------------------
// No synthesis for non-Lombok classes
// ---------------------------------------------------------------------------

func TestSynthesizeLombok_NoAnnotation(t *testing.T) {
	out := synthesizeLombokEntities("Plain", "public class Plain",
		"    private String x;\n", "Plain.java")
	if len(out) != 0 {
		t.Errorf("expected no synthesis for un-annotated class, got %d entities: %v",
			len(out), sortedEntityNames(out))
	}
}

func TestSynthesizeLombok_NonLombokAnnotations(t *testing.T) {
	out := synthesizeLombokEntities("Order", "@Entity\n@Table(name=\"orders\")\npublic class Order",
		"    private Long id;\n", "Order.java")
	if len(out) != 0 {
		t.Errorf("expected no synthesis for non-Lombok annotations, got %d entities: %v",
			len(out), sortedEntityNames(out))
	}
}

// ---------------------------------------------------------------------------
// Combined annotations
// ---------------------------------------------------------------------------

func TestSynthesizeLombok_BuilderAndNoArgsConstructor(t *testing.T) {
	// @Builder + @NoArgsConstructor is a common Lombok combo
	decl := "@Builder\n@NoArgsConstructor\n@AllArgsConstructor\npublic class Dto"
	body := "    private String value;\n"
	out := synthesizeLombokEntities("Dto", decl, body, "Dto.java")
	names := entityNameSet(out)
	if !names["DtoBuilder"] {
		t.Error("missing DtoBuilder")
	}
	if !names["Dto.builder"] {
		t.Error("missing Dto.builder")
	}
	if !names["Dto.Dto"] {
		t.Error("missing constructor Dto.Dto")
	}
}
