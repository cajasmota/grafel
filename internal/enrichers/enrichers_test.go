package enrichers

import (
	"testing"

	"github.com/cajasmota/grafel/internal/types"
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

// complexity — the cyclomatic-complexity helpers (ComputeCyclomaticComplexity,
// HasConditionals, HasExternalCalls, ComputeMaxCallDepth) were dead code
// (referenced only by these tests) and have been retired (#4831, epic #4820).
// The single, validated complexity implementation now lives in
// substrate.ComputeFunctionComplexity and is persisted on EVERY function-like
// entity by internal/links/complexity_pass.go (runComplexityPass), covered by
// complexity_pass_test.go. There are no longer two divergent implementations.

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

// coupling_score — the orphaned coupling_score enricher was restored as a LIVE
// engine pass (internal/engine/structural_coupling.go, #3634 / epic #3625);
// its behaviour is now covered by structural_coupling_test.go, which asserts
// the real Ca/Ce/instability properties stamped on Module nodes from the
// materialized DEPENDS_ON dependency graph.

// deployment_topology — the orphaned deployment_topology_extractor was restored
// as a LIVE engine pass (internal/engine/deployment_topology_edges.go, #3633);
// its behaviour is now covered by deployment_topology_edges_test.go, which
// asserts the real graph edges it emits.

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

// TestAnnotateMigrationSequences_OrdinaryPythonModulesUnstamped pins the
// negative half of the Alembic discriminator (#6557): an ordinary Python module
// whose first underscore-delimited segment merely happens to be long must fall
// through to the unknown bucket, not be reported as an Alembic migration.
//
// Several cases below are rejected by only ONE of the three constraints (path
// component, hex charset, 12-character minimum), so this one test goes red if
// any single constraint is relaxed on its own — though all such failures land
// on this same test, differing in which case inside it fails.
func TestAnnotateMigrationSequences_OrdinaryPythonModulesUnstamped(t *testing.T) {
	cases := []string{
		// Reporter's own cases (#6557): 12- and 14-character first segments.
		"app/api/endpoints/notification_stream.py",
		"app/services/authentication_service.py",
		"app/core/configuration_loader.py",
		"app/db/subscription_manager.py",
		// Hex-looking revision id OUTSIDE a versions/ directory: only the path
		// half rejects this one.
		"app/models/abc123def456_helpers.py",
		// A versions/ directory that is not Alembic's (API versioning): only
		// the charset half rejects this one.
		"app/api/versions/notification_stream.py",
		// All-hex first segments that are too SHORT to be a revision id, in a
		// real Alembic directory: only the {12,} length bound rejects these.
		// `added` and `deface` are ordinary words spelled entirely in hex
		// letters, so relaxing the bound re-opens the reported defect inside
		// versions/ itself.
		"alembic/versions/added_field.py",
		"alembic/versions/deface_x.py",
		// A hex run that is NOT the leading segment. Pins the `^` anchor: the
		// revision-half is supposed to mean "the revision is the FIRST
		// underscore-delimited segment", and dropping the anchor stamps this as
		// revision=abc123def456 name=util. Contrived as a filename, but the
		// anchor encodes a load-bearing assumption about Alembic that nothing
		// else observes — see the file_template note on alembicMigrationRe.
		"alembic/versions/helpers_abc123def456_util.py",
	}
	for _, src := range cases {
		ann, unknown := AnnotateMigrationSequences([]MigrationEntity{{EntityID: "e1", SourceFile: src}})
		if len(ann) != 0 {
			t.Errorf("%s: expected no annotation, got %+v", src, ann[0])
		}
		if unknown != 1 {
			t.Errorf("%s: expected unknown=1, got %d", src, unknown)
		}
	}
}

// TestAnnotateMigrationSequences_VersionsMustBeAPathComponent pins that the
// path half of the Alembic discriminator (#6557) is a path-COMPONENT match, not
// a substring match on the directory. Every case below carries a hex-valid
// revision id, so the charset half accepts it and ONLY the path half can be
// doing the rejecting — replacing hasAlembicVersionsAncestor's split/EqualFold
// with strings.Contains(dir, "versions") turns all of them into migrations.
//
// The last case puts "versions" in the FILE name rather than an ancestor: the
// gate inspects filepath.Dir, so the basename must not be able to satisfy it.
// (`versions.py` on its own cannot reach this assertion — it has no underscore
// and so fails the basename regex before the path gate is consulted, which
// would make such a case vacuous.)
func TestAnnotateMigrationSequences_VersionsMustBeAPathComponent(t *testing.T) {
	cases := []string{
		"app/myversions/abc123def456_thing.py",
		"app/versions_old/abc123def456_thing.py",
		"app/oldversions/abc123def456_thing.py",
		"app/versionsbackup/abc123def456_thing.py",
		"app/abc123def456_versions.py",
	}
	for _, src := range cases {
		ann, unknown := AnnotateMigrationSequences([]MigrationEntity{{EntityID: "e1", SourceFile: src}})
		if len(ann) != 0 {
			t.Errorf("%s: directory is not literally `versions`, expected no annotation, got %+v", src, ann[0])
		}
		if unknown != 1 {
			t.Errorf("%s: expected unknown=1, got %d", src, unknown)
		}
	}
}

// TestAnnotateMigrationSequences_AlembicAbsolutePath pins the positive half of
// the same gate for an ABSOLUTE source path. ApplyMigrationSequence passes
// Entity.SourceFile straight through, which is repo-relative for indexed
// entities but absolute for some callers, so both shapes reach this function
// and both must be recognised.
func TestAnnotateMigrationSequences_AlembicAbsolutePath(t *testing.T) {
	ann, _ := AnnotateMigrationSequences([]MigrationEntity{
		{EntityID: "m1", SourceFile: "/srv/repo/alembic/versions/abc123def456_add_column.py"},
	})
	if len(ann) != 1 || ann[0].PatternMatched != MigrationPatternAlembic {
		t.Fatalf("expected alembic for an absolute path, got %+v", ann)
	}
	if ann[0].SequenceNumber.(string) != "abc123def456" {
		t.Fatalf("expected revision abc123def456, got %v", ann[0].SequenceNumber)
	}
}

// TestAnnotateMigrationSequences_VersionsComponentIsCaseInsensitive pins that
// the `versions` component is matched with EqualFold, so a case-preserving but
// case-insensitive filesystem (macOS, Windows) that reports `VERSIONS/` still
// resolves to an Alembic migration. This is deliberate, and unobserved until
// now: tightening it to a case-sensitive compare must be a visible change.
func TestAnnotateMigrationSequences_VersionsComponentIsCaseInsensitive(t *testing.T) {
	ann, _ := AnnotateMigrationSequences([]MigrationEntity{
		{EntityID: "m1", SourceFile: "app/alembic/VERSIONS/abc123def456_add_column.py"},
	})
	if len(ann) != 1 || ann[0].PatternMatched != MigrationPatternAlembic {
		t.Fatalf("expected alembic for a VERSIONS/ component, got %+v", ann)
	}
}

// TestAnnotateMigrationSequences_AlembicRequiresHexRevision states the charset
// policy (#6557): Alembic's generated rev_id is uuid4().hex[-12:], so the
// discriminator accepts hex only. A hand-set `--rev-id` carrying a non-hex
// letter is NOT recognised, even under versions/. That is a deliberate recall
// cost, pinned here so changing it means changing a test.
func TestAnnotateMigrationSequences_AlembicRequiresHexRevision(t *testing.T) {
	ann, unknown := AnnotateMigrationSequences([]MigrationEntity{
		{EntityID: "e1", SourceFile: "alembic/versions/zz1234567890_add_col.py"},
	})
	if len(ann) != 0 || unknown != 1 {
		t.Fatalf("expected non-hex rev-id to be unknown, got ann=%+v unknown=%d", ann, unknown)
	}
}

func TestAnnotateMigrationSequences_EmptySourceFile(t *testing.T) {
	entities := []MigrationEntity{{EntityID: "m1", SourceFile: ""}}
	ann, unknown := AnnotateMigrationSequences(entities)
	if len(ann) != 0 || unknown != 0 {
		t.Fatalf("expected 0/0, got %d/%d", len(ann), unknown)
	}
}

func TestParseAlembicRevisions_WithParent(t *testing.T) {
	src := "revision = 'abc123def456'\ndown_revision = 'parent000000'\n"
	rev, down := ParseAlembicRevisions(src)
	if rev != "abc123def456" || down != "parent000000" {
		t.Fatalf("got rev=%q down=%q", rev, down)
	}
}

func TestParseAlembicRevisions_BaseNone(t *testing.T) {
	src := "revision = \"root00000000\"\ndown_revision = None\n"
	rev, down := ParseAlembicRevisions(src)
	if rev != "root00000000" {
		t.Fatalf("expected rev=root00000000, got %q", rev)
	}
	if down != "" {
		t.Fatalf("expected empty down_revision for base, got %q", down)
	}
}

func TestParseAlembicRevisions_TypedAnnotation(t *testing.T) {
	// Newer Alembic templates annotate the module vars: `revision: str = "..."`.
	src := "revision: str = 'x12345678901'\ndown_revision: Union[str, None] = 'y98765432109'\n"
	rev, down := ParseAlembicRevisions(src)
	if rev != "x12345678901" || down != "y98765432109" {
		t.Fatalf("got rev=%q down=%q", rev, down)
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
