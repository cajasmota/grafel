package enrichers

import (
	"testing"

	"github.com/cajasmota/archigraph/internal/types"
)

func makeEntity(id, kind, subtype, sourceFile, name string) types.EntityRecord {
	return types.EntityRecord{
		ID:         id,
		Kind:       kind,
		Subtype:    subtype,
		SourceFile: sourceFile,
		Name:       name,
		Properties: make(map[string]string),
		Metadata:   make(map[string]interface{}),
	}
}

// complexity

func TestCyclomaticComplexity_BaseOne(t *testing.T) {
	if got := ComputeCyclomaticComplexity("x := 1"); got != 1 {
		t.Fatalf("expected 1, got %d", got)
	}
}

func TestCyclomaticComplexity_IfElse(t *testing.T) {
	src := "if x > 0 {\n  y = 1\n} else {\n  y = 2\n}\n"
	if got := ComputeCyclomaticComplexity(src); got < 3 {
		t.Fatalf("expected >=3, got %d", got)
	}
}

func TestCyclomaticComplexity_ForSwitch(t *testing.T) {
	src := "for i := 0; i < 10; i++ { switch i { case 0: x = 1 case 1: x = 2 } }"
	if got := ComputeCyclomaticComplexity(src); got < 5 {
		t.Fatalf("expected >=5, got %d", got)
	}
}

func TestHasConditionals_True(t *testing.T) {
	if !HasConditionals("if x > 0 { return x }") {
		t.Fatal("expected true")
	}
}

func TestHasConditionals_False(t *testing.T) {
	if HasConditionals("x := 1\ny := 2") {
		t.Fatal("expected false")
	}
}

func TestHasExternalCalls_HTTP(t *testing.T) {
	if !HasExternalCalls(`resp, err := http.Get("https://example.com")`) {
		t.Fatal("expected true for http.Get")
	}
}

func TestHasExternalCalls_None(t *testing.T) {
	if HasExternalCalls("x := 1 + 2") {
		t.Fatal("expected false")
	}
}

func TestComputeMaxCallDepth_Flat(t *testing.T) {
	if got := ComputeMaxCallDepth("x := 1\ny := 2\n"); got != 0 {
		t.Fatalf("expected 0, got %d", got)
	}
}

func TestComputeMaxCallDepth_Nested(t *testing.T) {
	src := "func f() {\n    if x {\n        if y {\n            z()\n        }\n    }\n}"
	if got := ComputeMaxCallDepth(src); got < 1 {
		t.Fatalf("expected >=1, got %d", got)
	}
}

// api_version_enricher

func TestEnrichAPIVersion_ApiVN(t *testing.T) {
	e := makeEntity("1", "endpoint", "endpoint", "r.go", "listUsers")
	e.Properties["path"] = "/api/v2/users"
	result := EnrichAPIVersion([]types.EntityRecord{e})
	if result[0].Properties["api_version"] != "2" {
		t.Fatalf("expected 2, got %q", result[0].Properties["api_version"])
	}
}

func TestEnrichAPIVersion_VN(t *testing.T) {
	e := makeEntity("1", "endpoint", "endpoint", "r.go", "listItems")
	e.Properties["path"] = "/v1/items"
	result := EnrichAPIVersion([]types.EntityRecord{e})
	if result[0].Properties["api_version"] != "1" {
		t.Fatalf("expected 1, got %q", result[0].Properties["api_version"])
	}
}

func TestEnrichAPIVersion_EndOfString(t *testing.T) {
	e := makeEntity("1", "endpoint", "operation", "r.go", "health")
	e.Properties["path"] = "/api/v3"
	result := EnrichAPIVersion([]types.EntityRecord{e})
	if result[0].Properties["api_version"] != "3" {
		t.Fatalf("expected 3, got %q", result[0].Properties["api_version"])
	}
}

func TestEnrichAPIVersion_OutOfRange(t *testing.T) {
	e := makeEntity("1", "endpoint", "endpoint", "r.go", "x")
	e.Properties["path"] = "/v100/items"
	result := EnrichAPIVersion([]types.EntityRecord{e})
	if _, ok := result[0].Properties["api_version"]; ok {
		t.Fatal("expected no api_version set for v100")
	}
}

func TestEnrichAPIVersion_NoPath(t *testing.T) {
	e := makeEntity("1", "endpoint", "endpoint", "r.go", "x")
	result := EnrichAPIVersion([]types.EntityRecord{e})
	if _, ok := result[0].Properties["api_version"]; ok {
		t.Fatal("expected no api_version when path absent")
	}
}

func TestEnrichAPIVersion_WrongSubtype(t *testing.T) {
	e := makeEntity("1", "class", "class", "r.go", "UserService")
	e.Properties["path"] = "/api/v1/users"
	result := EnrichAPIVersion([]types.EntityRecord{e})
	if _, ok := result[0].Properties["api_version"]; ok {
		t.Fatal("expected no api_version for non-endpoint subtype")
	}
}

// architecture_classifier

func TestClassifyArchitecture_Monolith(t *testing.T) {
	res := ClassifyArchitectureFastPath(ArchClassificationInput{DockerComposeServiceCount: 1, InterServiceCallCount: 0})
	if res.ArchitectureType != ArchMonolith || !res.IsFastPath {
		t.Fatalf("expected monolith fast path, got %+v", res)
	}
}

func TestClassifyArchitecture_Microservices(t *testing.T) {
	res := ClassifyArchitectureFastPath(ArchClassificationInput{DockerComposeServiceCount: 6, InterServiceCallCount: 4})
	if res.ArchitectureType != ArchMicroservices || !res.IsFastPath {
		t.Fatalf("expected microservices fast path, got %+v", res)
	}
}

func TestClassifyArchitecture_Unknown(t *testing.T) {
	res := ClassifyArchitectureFastPath(ArchClassificationInput{DockerComposeServiceCount: 3, InterServiceCallCount: 2})
	if res.ArchitectureType != ArchUnknown || res.IsFastPath {
		t.Fatalf("expected unknown, got %+v", res)
	}
}

// bounded_context

func TestExtractTopLevelSegment_DottedJava(t *testing.T) {
	if seg := ExtractTopLevelSegment("", "com.example.orders.OrderService"); seg != "orders" {
		t.Fatalf("expected orders, got %q", seg)
	}
}

func TestExtractTopLevelSegment_FilePath(t *testing.T) {
	if seg := ExtractTopLevelSegment("src/main/java/com/example/users/UserRepo.java", "UserRepo"); seg != "users" {
		t.Fatalf("expected users, got %q", seg)
	}
}

func TestEnrichBoundedContext_Grouping(t *testing.T) {
	entities := []types.EntityRecord{
		makeEntity("1", "class", "class", "src/orders/OrderService.java", "com.example.orders.OrderService"),
		makeEntity("2", "class", "class", "src/orders/OrderRepo.java", "com.example.orders.OrderRepo"),
		makeEntity("3", "class", "class", "src/users/UserService.java", "com.example.users.UserService"),
		makeEntity("4", "class", "class", "src/users/UserRepo.java", "com.example.users.UserRepo"),
	}
	result := EnrichBoundedContext(entities)
	if result[0].Metadata["bounded_context"] != "orders" {
		t.Fatalf("expected orders, got %v", result[0].Metadata["bounded_context"])
	}
	if result[2].Metadata["bounded_context"] != "users" {
		t.Fatalf("expected users, got %v", result[2].Metadata["bounded_context"])
	}
}

func TestEnrichBoundedContext_SingleEntityUnknown(t *testing.T) {
	result := EnrichBoundedContext([]types.EntityRecord{makeEntity("1", "class", "class", "util.go", "Util")})
	if result[0].Metadata["bounded_context"] != "unknown" {
		t.Fatalf("expected unknown, got %v", result[0].Metadata["bounded_context"])
	}
}

// config_consumer

func TestExtractConfigKeys_GoViper(t *testing.T) {
	keys := ExtractConfigKeys(`viper.GetString("database.url")`, "main.go")
	if len(keys) != 1 || keys[0].KeyName != "database.url" || keys[0].Pattern != "go_viper" {
		t.Fatalf("expected go_viper key, got %+v", keys)
	}
}

func TestExtractConfigKeys_PythonOsGetenv(t *testing.T) {
	keys := ExtractConfigKeys(`os.getenv("DATABASE_URL")`, "app.py")
	if len(keys) != 1 || keys[0].KeyName != "DATABASE_URL" {
		t.Fatalf("expected DATABASE_URL, got %+v", keys)
	}
}

func TestExtractConfigKeys_NodeProcessEnv(t *testing.T) {
	keys := ExtractConfigKeys(`const url = process.env.API_URL;`, "app.js")
	if len(keys) != 1 || keys[0].KeyName != "API_URL" {
		t.Fatalf("expected API_URL, got %+v", keys)
	}
}

func TestExtractConfigKeys_Empty(t *testing.T) {
	if keys := ExtractConfigKeys("", "app.go"); len(keys) != 0 {
		t.Fatalf("expected empty, got %+v", keys)
	}
}

func TestExtractConfigKeys_SpringValue(t *testing.T) {
	keys := ExtractConfigKeys(`@Value("${spring.datasource.url}")`, "Service.java")
	if len(keys) != 1 || keys[0].Pattern != "spring_value" {
		t.Fatalf("expected spring_value, got %+v", keys)
	}
}

// config_prefix

func TestExtractConfigPrefixes_SpringProperties(t *testing.T) {
	entries := ExtractConfigPrefixes("server.servlet.context-path=/api/v1\n", "application.properties")
	if len(entries) != 1 || entries[0].Framework != "spring_boot" || entries[0].Value != "/api/v1" {
		t.Fatalf("expected spring_boot /api/v1, got %+v", entries)
	}
}

func TestExtractConfigPrefixes_DjangoSettings(t *testing.T) {
	entries := ExtractConfigPrefixes(`FORCE_SCRIPT_NAME = "/app"`, "settings.py")
	if len(entries) != 1 || entries[0].Framework != "django" {
		t.Fatalf("expected django, got %+v", entries)
	}
}

func TestExtractConfigPrefixes_ExpressApp(t *testing.T) {
	entries := ExtractConfigPrefixes(`app.use('/api', router);`, "app.js")
	if len(entries) != 1 || entries[0].Framework != "express" {
		t.Fatalf("expected express, got %+v", entries)
	}
}

func TestConfigPrefixAppliesToFile_Gated(t *testing.T) {
	if !ConfigPrefixAppliesToFile("src/main/resources/application.properties") {
		t.Fatal("expected true for application.properties")
	}
}

func TestConfigPrefixAppliesToFile_NotGated(t *testing.T) {
	if ConfigPrefixAppliesToFile("src/main/Service.java") {
		t.Fatal("expected false for non-config file")
	}
}

// config_profile

func TestParseYAMLFlat(t *testing.T) {
	m := ParseYAMLFlat("server:\n  port: 8080\n  host: localhost\n")
	if m["server.port"] != "8080" {
		t.Fatalf("expected server.port=8080, got %q", m["server.port"])
	}
}

func TestParseDotenv(t *testing.T) {
	m := ParseDotenv("DATABASE_URL=postgres://localhost/db\nexport SECRET_KEY=abc123\n")
	if m["DATABASE_URL"] != "postgres://localhost/db" {
		t.Fatalf("expected postgres URL, got %q", m["DATABASE_URL"])
	}
	if m["SECRET_KEY"] != "abc123" {
		t.Fatalf("expected abc123, got %q", m["SECRET_KEY"])
	}
}

func TestParseProperties(t *testing.T) {
	m := ParseProperties("spring.datasource.url=jdbc:postgresql://localhost/db\n# comment\nserver.port=8080\n")
	if m["server.port"] != "8080" {
		t.Fatalf("expected 8080, got %q", m["server.port"])
	}
}

func TestComputeDiffKeys(t *testing.T) {
	a := map[string]string{"key1": "val1", "key2": "val2", "key3": "same"}
	b := map[string]string{"key1": "changed", "key3": "same", "key4": "new"}
	diff := ComputeDiffKeys(a, b)
	if len(diff) != 3 {
		t.Fatalf("expected 3 diff keys, got %d: %v", len(diff), diff)
	}
}

func TestEnrichConfigProfiles_SpringBoot(t *testing.T) {
	devE := makeEntity("dev", "config", "config_file", "src/application-dev.yml", "app-dev")
	devE.Metadata["content"] = "spring:\n  datasource:\n    url: jdbc:dev\n"
	prodE := makeEntity("prod", "config", "config_file", "src/application-prod.yml", "app-prod")
	prodE.Metadata["content"] = "spring:\n  datasource:\n    url: jdbc:prod\n"
	entities := EnrichConfigProfiles([]types.EntityRecord{devE, prodE})
	found := false
	for i := range entities {
		if _, ok := entities[i].Metadata["config_profile_enriched"]; ok {
			found = true
		}
	}
	if !found {
		t.Fatal("expected config_profile_enriched to be set")
	}
}

// consumes_api

func TestExtractURLPath_Full(t *testing.T) {
	if got := ExtractURLPath("https://svc.internal/api/users"); got != "/api/users" {
		t.Fatalf("expected /api/users, got %q", got)
	}
}

func TestExtractURLPath_Relative(t *testing.T) {
	if got := ExtractURLPath("/api/users"); got != "/api/users" {
		t.Fatalf("expected /api/users, got %q", got)
	}
}

func TestMethodMatches_Wildcard(t *testing.T) {
	if !MethodMatches("GET", "*") {
		t.Fatal("expected true for wildcard")
	}
}

func TestMethodMatches_Exact(t *testing.T) {
	if !MethodMatches("POST", "POST") {
		t.Fatal("expected true for matching methods")
	}
}

func TestMethodMatches_CaseInsensitive(t *testing.T) {
	if !MethodMatches("get", "GET") {
		t.Fatal("expected true for case-insensitive")
	}
}

func TestMethodMatches_Empty(t *testing.T) {
	if MethodMatches("", "GET") {
		t.Fatal("expected false for empty call method")
	}
}

func TestEnrichConsumesAPI_ExactMatch(t *testing.T) {
	calls := []HTTPClientCall{{CallerServiceID: "svcA", URLPattern: "/api/users", Method: "GET"}}
	endpoints := []EndpointInfo{{ServiceID: "svcB", EntityRef: "ref:users", Path: "/api/users", Method: "GET"}}
	edges := EnrichConsumesAPI(calls, endpoints)
	if len(edges) != 1 || edges[0].EndpointEntityID != "ref:users" {
		t.Fatalf("expected 1 edge, got %+v", edges)
	}
}

func TestEnrichConsumesAPI_NoMatch(t *testing.T) {
	calls := []HTTPClientCall{{CallerServiceID: "svcA", URLPattern: "/api/orders", Method: "GET"}}
	endpoints := []EndpointInfo{{ServiceID: "svcB", EntityRef: "ref:users", Path: "/api/users", Method: "GET"}}
	if edges := EnrichConsumesAPI(calls, endpoints); len(edges) != 0 {
		t.Fatalf("expected 0 edges, got %d", len(edges))
	}
}

func TestEnrichConsumesAPI_EmptyEndpoints(t *testing.T) {
	calls := []HTTPClientCall{{CallerServiceID: "svcA", URLPattern: "/api/users", Method: "GET"}}
	if edges := EnrichConsumesAPI(calls, nil); len(edges) != 0 {
		t.Fatalf("expected 0 edges for empty endpoints, got %d", len(edges))
	}
}

// coupling_score

func TestEnrichCouplingScore_Basic(t *testing.T) {
	comp1 := makeEntity("c1", "SCOPE.Component", "", "pkg/orders", "orders")
	comp2 := makeEntity("c2", "SCOPE.Component", "", "pkg/users", "users")
	comp2.Relationships = []types.RelationshipRecord{{FromID: "c2", ToID: "c1", Kind: "DEPENDS_ON"}}
	entities := EnrichCouplingScore([]types.EntityRecord{comp1, comp2})
	for _, e := range entities {
		switch e.ID {
		case "c1":
			if e.Properties["ca"] != "1" {
				t.Fatalf("comp1 ca expected 1, got %q", e.Properties["ca"])
			}
		case "c2":
			if e.Properties["ce"] != "1" {
				t.Fatalf("comp2 ce expected 1, got %q", e.Properties["ce"])
			}
		}
	}
}

func TestEnrichCouplingScore_NoComponents(t *testing.T) {
	result := EnrichCouplingScore([]types.EntityRecord{makeEntity("1", "class", "", "pkg/x", "X")})
	if _, ok := result[0].Properties["coupling_computed"]; ok {
		t.Fatal("expected no coupling_computed for non-component")
	}
}

func TestEnrichCouplingScore_Isolated(t *testing.T) {
	result := EnrichCouplingScore([]types.EntityRecord{makeEntity("c1", "SCOPE.Component", "", "pkg/iso", "iso")})
	if result[0].Properties["instability"] != "0.00" {
		t.Fatalf("expected 0.00, got %q", result[0].Properties["instability"])
	}
}

// deployment_topology

func TestDeploymentTopologyAppliesToFile_Nginx(t *testing.T) {
	if !DeploymentTopologyAppliesToFile("config/nginx.conf") {
		t.Fatal("expected true for nginx.conf")
	}
}

func TestDeploymentTopologyAppliesToFile_K8s(t *testing.T) {
	if !DeploymentTopologyAppliesToFile("k8s/ingress.yml") {
		t.Fatal("expected true for k8s/ingress.yml")
	}
}

func TestExtractDeploymentTopology_Nginx(t *testing.T) {
	src := "server {\n  location /api/ {\n    proxy_pass http://backend:8080/;\n  }\n  location /health {\n    proxy_pass http://backend:8080/health;\n  }\n}\n"
	if entries := ExtractDeploymentTopology(src, "nginx.conf"); len(entries) < 2 {
		t.Fatalf("expected >=2 nginx entries, got %d", len(entries))
	}
}

func TestExtractDeploymentTopology_DockerCompose(t *testing.T) {
	src := "version: \"3\"\nservices:\n  web:\n    image: nginx\n  api:\n    image: myapp\n"
	entries := ExtractDeploymentTopology(src, "docker-compose.yml")
	names := map[string]bool{}
	for _, e := range entries {
		names[e.Name] = true
	}
	if !names["web"] || !names["api"] {
		t.Fatalf("expected web and api, got %+v", entries)
	}
}

// event_flow

func TestEnrichEventFlow_ExactMatch(t *testing.T) {
	pubs := []PublishesToEdge{{ProducerServiceID: "svcA", TopicName: "orders.created", EdgeID: "e1"}}
	subs := []SubscribesToEdge{{ConsumerServiceID: "svcB", TopicName: "orders.created", EdgeID: "e2"}}
	chains := EnrichEventFlow(pubs, subs)
	if len(chains) != 1 || chains[0].Topic != "orders.created" || chains[0].Confidence != "exact" {
		t.Fatalf("expected 1 exact chain, got %+v", chains)
	}
}

func TestEnrichEventFlow_NoMatch(t *testing.T) {
	pubs := []PublishesToEdge{{ProducerServiceID: "svcA", TopicName: "orders.created", EdgeID: "e1"}}
	subs := []SubscribesToEdge{{ConsumerServiceID: "svcB", TopicName: "users.updated", EdgeID: "e2"}}
	if chains := EnrichEventFlow(pubs, subs); len(chains) != 0 {
		t.Fatalf("expected 0 chains, got %d", len(chains))
	}
}

func TestEnrichEventFlow_WildcardSkipped(t *testing.T) {
	pubs := []PublishesToEdge{{ProducerServiceID: "svcA", TopicName: "orders.*", EdgeID: "e1"}}
	subs := []SubscribesToEdge{{ConsumerServiceID: "svcB", TopicName: "orders.created", EdgeID: "e2"}}
	if chains := EnrichEventFlow(pubs, subs); len(chains) != 0 {
		t.Fatalf("expected 0 chains for wildcard, got %d", len(chains))
	}
}

func TestEnrichEventFlow_MultipleProducersConsumers(t *testing.T) {
	pubs := []PublishesToEdge{
		{ProducerServiceID: "svcA", TopicName: "payments", EdgeID: "e1"},
		{ProducerServiceID: "svcB", TopicName: "payments", EdgeID: "e2"},
	}
	subs := []SubscribesToEdge{
		{ConsumerServiceID: "svcC", TopicName: "payments", EdgeID: "e3"},
		{ConsumerServiceID: "svcD", TopicName: "payments", EdgeID: "e4"},
	}
	if chains := EnrichEventFlow(pubs, subs); len(chains) != 4 {
		t.Fatalf("expected 4 chains, got %d", len(chains))
	}
}

// layer_classifier

func TestClassifyLayer_Controller(t *testing.T) {
	if res := ClassifyLayer("src/main/java/UserController.java"); res.Layer != "controller" {
		t.Fatalf("expected controller, got %q", res.Layer)
	}
}

func TestClassifyLayer_Service(t *testing.T) {
	if res := ClassifyLayer("src/services/OrderService.go"); res.Layer != "service" {
		t.Fatalf("expected service, got %q", res.Layer)
	}
}

func TestClassifyLayer_Repository(t *testing.T) {
	if res := ClassifyLayer("internal/repository/user_repo.go"); res.Layer != "repository" {
		t.Fatalf("expected repository, got %q", res.Layer)
	}
}

func TestClassifyLayer_Unknown(t *testing.T) {
	if res := ClassifyLayer("utils/helpers.go"); res.Layer != "unknown" {
		t.Fatalf("expected unknown, got %q", res.Layer)
	}
}

func TestEnrichLayerClassifier_SetsMetadata(t *testing.T) {
	e := makeEntity("1", "class", "", "src/controllers/UserController.go", "UserController")
	result := EnrichLayerClassifier([]types.EntityRecord{e})
	if result[0].Metadata["layer"] != "controller" {
		t.Fatalf("expected controller, got %v", result[0].Metadata["layer"])
	}
}

// lib_boundary

func TestAnnotateLibBoundaries_FirstParty(t *testing.T) {
	edges := []DependsOnEdge{{EdgeID: "e1", SourceEntityID: "svc", TargetPackageName: "com.example.orders"}}
	ann := AnnotateLibBoundaries(edges, []string{"com.example"})
	if len(ann) != 1 || ann[0].Boundary != "first_party" || ann[0].MatchedPrefix != "com.example" {
		t.Fatalf("expected first_party, got %+v", ann)
	}
}

func TestAnnotateLibBoundaries_ThirdParty(t *testing.T) {
	edges := []DependsOnEdge{{EdgeID: "e1", SourceEntityID: "svc", TargetPackageName: "github.com/gin-gonic/gin"}}
	ann := AnnotateLibBoundaries(edges, []string{"com.example"})
	if len(ann) != 1 || ann[0].Boundary != "third_party" {
		t.Fatalf("expected third_party, got %+v", ann)
	}
}

func TestAnnotateLibBoundaries_EmptyNamespaces(t *testing.T) {
	edges := []DependsOnEdge{{EdgeID: "e1", SourceEntityID: "svc", TargetPackageName: "com.example.orders"}}
	ann := AnnotateLibBoundaries(edges, nil)
	if len(ann) != 1 || ann[0].Boundary != "third_party" {
		t.Fatalf("expected third_party, got %+v", ann)
	}
}

func TestAnnotateLibBoundaries_EmptyPackageName(t *testing.T) {
	edges := []DependsOnEdge{{EdgeID: "e1", SourceEntityID: "svc", TargetPackageName: ""}}
	if ann := AnnotateLibBoundaries(edges, []string{"com.example"}); len(ann) != 0 {
		t.Fatalf("expected empty, got %+v", ann)
	}
}

// migration_sequence

func TestAnnotateMigrationSequences_Rails(t *testing.T) {
	entities := []MigrationEntity{{EntityID: "m1", SourceFile: "db/migrate/20230101120000_create_users.rb"}}
	ann, unknown := AnnotateMigrationSequences(entities)
	if len(ann) != 1 || unknown != 0 || ann[0].PatternMatched != MigrationPatternRails {
		t.Fatalf("expected rails, got %+v %d", ann, unknown)
	}
	if ann[0].MigrationName != "create users" {
		t.Fatalf("expected 'create users', got %q", ann[0].MigrationName)
	}
}

func TestAnnotateMigrationSequences_Django(t *testing.T) {
	entities := []MigrationEntity{{EntityID: "m1", SourceFile: "app/migrations/0001_initial.py"}}
	ann, _ := AnnotateMigrationSequences(entities)
	if len(ann) != 1 || ann[0].SequenceNumber.(int) != 1 {
		t.Fatalf("expected seq=1, got %+v", ann)
	}
}

func TestAnnotateMigrationSequences_Flyway(t *testing.T) {
	entities := []MigrationEntity{{EntityID: "m1", SourceFile: "db/V1.2__create_orders_table.sql"}}
	ann, _ := AnnotateMigrationSequences(entities)
	if len(ann) != 1 || ann[0].PatternMatched != MigrationPatternFlyway || ann[0].SequenceNumber.(string) != "1.2" {
		t.Fatalf("expected flyway 1.2, got %+v", ann)
	}
}

func TestAnnotateMigrationSequences_GolangMigrate(t *testing.T) {
	entities := []MigrationEntity{{EntityID: "m1", SourceFile: "migrations/000001_create_users.up.sql"}}
	ann, _ := AnnotateMigrationSequences(entities)
	if len(ann) != 1 || ann[0].PatternMatched != MigrationPatternGolangMigrate {
		t.Fatalf("expected golang_migrate, got %+v", ann)
	}
}

func TestAnnotateMigrationSequences_Alembic(t *testing.T) {
	entities := []MigrationEntity{{EntityID: "m1", SourceFile: "alembic/versions/abc123def456_add_column.py"}}
	ann, _ := AnnotateMigrationSequences(entities)
	if len(ann) != 1 || ann[0].PatternMatched != MigrationPatternAlembic {
		t.Fatalf("expected alembic, got %+v", ann)
	}
}

func TestAnnotateMigrationSequences_UnknownPattern(t *testing.T) {
	entities := []MigrationEntity{{EntityID: "m1", SourceFile: "migrations/unknown_file.sql"}}
	ann, unknown := AnnotateMigrationSequences(entities)
	if len(ann) != 0 || unknown != 1 {
		t.Fatalf("expected 0/1, got %d/%d", len(ann), unknown)
	}
}

func TestAnnotateMigrationSequences_EmptySourceFile(t *testing.T) {
	entities := []MigrationEntity{{EntityID: "m1", SourceFile: ""}}
	ann, unknown := AnnotateMigrationSequences(entities)
	if len(ann) != 0 || unknown != 0 {
		t.Fatalf("expected 0/0, got %d/%d", len(ann), unknown)
	}
}

// pagination

func TestEnrichPagination_ParametersList(t *testing.T) {
	e := makeEntity("1", "endpoint", "endpoint", "routes.go", "listUsers")
	e.Properties["parameters"] = "user_id,page,sort"
	result := EnrichPagination([]types.EntityRecord{e})
	if result[0].Properties["paginated"] != "true" {
		t.Fatalf("expected paginated=true, got %q", result[0].Properties["paginated"])
	}
}

func TestEnrichPagination_ParameterSchema(t *testing.T) {
	e := makeEntity("1", "endpoint", "operation", "routes.go", "searchItems")
	e.Properties["parameter_schema"] = `{"limit": 10, "offset": 0}`
	result := EnrichPagination([]types.EntityRecord{e})
	if result[0].Properties["paginated"] != "true" {
		t.Fatalf("expected paginated=true, got %q", result[0].Properties["paginated"])
	}
}

func TestEnrichPagination_EntityName_CamelCase(t *testing.T) {
	e := makeEntity("1", "endpoint", "endpoint", "routes.go", "listUsersPage")
	result := EnrichPagination([]types.EntityRecord{e})
	if result[0].Properties["paginated"] != "true" {
		t.Fatalf("expected paginated=true, got %q", result[0].Properties["paginated"])
	}
}

func TestEnrichPagination_NoPagination(t *testing.T) {
	e := makeEntity("1", "endpoint", "endpoint", "routes.go", "getUser")
	result := EnrichPagination([]types.EntityRecord{e})
	if result[0].Properties["paginated"] != "" {
		t.Fatalf("expected empty paginated, got %q", result[0].Properties["paginated"])
	}
}

func TestEnrichPagination_WrongSubtype(t *testing.T) {
	e := makeEntity("1", "class", "class", "routes.go", "listUsersPage")
	e.Properties["parameters"] = "page"
	result := EnrichPagination([]types.EntityRecord{e})
	if result[0].Properties["paginated"] != "" {
		t.Fatal("expected no pagination for non-endpoint subtype")
	}
}
