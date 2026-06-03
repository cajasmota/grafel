package engine

import (
	"context"
	"regexp"
	"testing"
	"testing/fstest"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

// ginYAML is the gin.yaml rule file used for tests.
const ginYAML = `
file_conventions: []

source_patterns:
  - pattern: '\.\s*(?:GET|POST|PUT|PATCH|DELETE|HEAD|OPTIONS|Any)\s*\(\s*"([^"]+)"\s*,\s*(\w+)'
    entity_type: Route
    name_group: 1
    scope: file

  - pattern: '\.Group\s*\(\s*"([^"]+)"\s*\)'
    entity_type: Route
    name_group: 1
    scope: file

  - pattern: '\.Use\s*\(\s*(\w[\w.]*)\s*\)'
    entity_type: Middleware
    name_group: 1
    scope: file

  - pattern: 'func\s+(\w+)\s*\(\s*\w+\s+\*gin\.Context\s*\)'
    entity_type: Controller
    name_group: 1
    scope: file

relationship_rules:
  - pattern: '\.\s*(?:GET|POST|PUT|PATCH|DELETE|HEAD|OPTIONS|Any)\s*\(\s*"([^"]+)"\s*,\s*(\w+)'
    source_type: Route
    target_type: Controller
    relationship: ROUTES_TO
    source_group: 1
    target_group: 2

custom_extractors: []
`

// djangoYAML is a minimal Django-like rule file for cross-language testing.
const djangoYAML = `
file_conventions: []

source_patterns:
  - pattern: 'class\s+(\w+)\(.*View\)'
    entity_type: Controller
    name_group: 1
    scope: file

  - pattern: 'path\s*\(\s*[''"]([^''"]+)[''"]'
    entity_type: Route
    name_group: 1
    scope: file

  - pattern: 'class\s+(\w+)\(.*Model\)'
    entity_type: Model
    name_group: 1
    scope: file

relationship_rules: []
custom_extractors: []
`

// sampleGinCode is a realistic Go file using the gin framework.
const sampleGinCode = `package main

import "github.com/gin-gonic/gin"

func main() {
	r := gin.Default()

	r.Use(AuthMiddleware)
	r.Use(CORSMiddleware)

	api := r.Group("/api")

	api.GET("/users", ListUsers)
	api.POST("/users", CreateUser)
	api.DELETE("/users/:id", DeleteUser)
}

func ListUsers(c *gin.Context) {
	c.JSON(200, gin.H{"users": []string{}})
}

func CreateUser(c *gin.Context) {
	c.JSON(201, gin.H{"status": "created"})
}

func DeleteUser(c *gin.Context) {
	c.JSON(200, gin.H{"status": "deleted"})
}

func AuthMiddleware(c *gin.Context) {
	c.Next()
}

func CORSMiddleware(c *gin.Context) {
	c.Next()
}
`

// sampleDjangoCode is a minimal Django-like Python file.
const sampleDjangoCode = `from django.views import View
from django.db import models
from django.urls import path

class UserView(View):
    def get(self, request):
        pass

class ProductView(View):
    def get(self, request):
        pass

class User(Model):
    name = CharField(max_length=100)

urlpatterns = [
    path('users/', UserView.as_view()),
    path('products/', ProductView.as_view()),
]
`

func buildTestFS(lang, framework, content string) *fstest.MapFS {
	path := "rules/" + lang + "/frameworks/" + framework + ".yaml"
	return &fstest.MapFS{
		path: &fstest.MapFile{Data: []byte(content)},
	}
}

func buildMultiFS() *fstest.MapFS {
	return &fstest.MapFS{
		"rules/go/frameworks/gin.yaml":        &fstest.MapFile{Data: []byte(ginYAML)},
		"rules/python/frameworks/django.yaml": &fstest.MapFile{Data: []byte(djangoYAML)},
	}
}

func TestLoadAllRulesFromFS_Gin(t *testing.T) {
	fsys := buildTestFS("go", "gin", ginYAML)
	rules, err := LoadAllRulesFromFS(fsys, "rules")
	if err != nil {
		t.Fatalf("LoadAllRulesFromFS failed: %v", err)
	}
	goRules, ok := rules["go"]
	if !ok {
		t.Fatal("expected rules for 'go' language")
	}
	if len(goRules) != 1 {
		t.Fatalf("expected 1 rule set for go, got %d", len(goRules))
	}
	if len(goRules[0].SourcePatterns) != 4 {
		t.Errorf("expected 4 source patterns, got %d", len(goRules[0].SourcePatterns))
	}
	if len(goRules[0].RelationshipRules) != 1 {
		t.Errorf("expected 1 relationship rule, got %d", len(goRules[0].RelationshipRules))
	}
}

func TestLoadAllRulesFromFS_MultiLanguage(t *testing.T) {
	fsys := buildMultiFS()
	rules, err := LoadAllRulesFromFS(fsys, "rules")
	if err != nil {
		t.Fatalf("LoadAllRulesFromFS failed: %v", err)
	}
	if len(rules) != 2 {
		t.Fatalf("expected 2 languages, got %d", len(rules))
	}
	if _, ok := rules["go"]; !ok {
		t.Error("missing 'go' rules")
	}
	if _, ok := rules["python"]; !ok {
		t.Error("missing 'python' rules")
	}
}

func TestLoadAllRulesFromFS_EmptyFS(t *testing.T) {
	fsys := &fstest.MapFS{
		"rules/.gitkeep": &fstest.MapFile{Data: []byte{}},
	}
	rules, err := LoadAllRulesFromFS(fsys, "rules")
	if err != nil {
		t.Fatalf("LoadAllRulesFromFS failed: %v", err)
	}
	if len(rules) != 0 {
		t.Errorf("expected 0 languages, got %d", len(rules))
	}
}

func TestLoadAllRulesFromFS_MalformedYAML(t *testing.T) {
	fsys := &fstest.MapFS{
		"rules/go/frameworks/bad.yaml": &fstest.MapFile{Data: []byte("{{{{not yaml")},
	}
	rules, err := LoadAllRulesFromFS(fsys, "rules")
	if err != nil {
		t.Fatalf("LoadAllRulesFromFS should not error on malformed YAML, got: %v", err)
	}
	if goRules := rules["go"]; len(goRules) != 0 {
		t.Errorf("expected 0 rules for malformed YAML, got %d", len(goRules))
	}
}

func TestDetect_GinRoutes(t *testing.T) {
	fsys := buildTestFS("go", "gin", ginYAML)
	rules, err := LoadAllRulesFromFS(fsys, "rules")
	if err != nil {
		t.Fatalf("LoadAllRulesFromFS failed: %v", err)
	}

	det := New(rules)
	result, err := det.Detect(context.Background(), extractor.FileInput{
		Path:     "cmd/api/main.go",
		Content:  []byte(sampleGinCode),
		Language: "go",
	})
	if err != nil {
		t.Fatalf("Detect failed: %v", err)
	}

	// Expected entities: 3 Routes (GET, POST, DELETE), 1 Group Route (/api),
	// 2 Middleware (AuthMiddleware, CORSMiddleware),
	// 5 Controllers (ListUsers, CreateUser, DeleteUser, AuthMiddleware, CORSMiddleware)
	if len(result.Entities) == 0 {
		t.Fatal("expected entities, got 0")
	}

	// Check we got Route entities.
	routeCount := countByKind(result.Entities, "Route")
	if routeCount < 3 {
		t.Errorf("expected at least 3 Route entities, got %d", routeCount)
	}

	// Check we got Controller entities.
	controllerCount := countByKind(result.Entities, "Controller")
	if controllerCount < 3 {
		t.Errorf("expected at least 3 Controller entities, got %d", controllerCount)
	}

	// Check we got Middleware entities.
	middlewareCount := countByKind(result.Entities, "Middleware")
	if middlewareCount < 2 {
		t.Errorf("expected at least 2 Middleware entities, got %d", middlewareCount)
	}

	// Check relationships exist.
	if len(result.Relationships) == 0 {
		t.Error("expected relationships, got 0")
	}
}

func TestDetect_GinEntityProperties(t *testing.T) {
	fsys := buildTestFS("go", "gin", ginYAML)
	rules, err := LoadAllRulesFromFS(fsys, "rules")
	if err != nil {
		t.Fatalf("LoadAllRulesFromFS failed: %v", err)
	}

	det := New(rules)
	result, err := det.Detect(context.Background(), extractor.FileInput{
		Path:     "cmd/api/main.go",
		Content:  []byte(sampleGinCode),
		Language: "go",
	})
	if err != nil {
		t.Fatalf("Detect failed: %v", err)
	}

	for _, e := range result.Entities {
		if e.SourceFile != "cmd/api/main.go" {
			t.Errorf("entity %q: SourceFile = %q, want cmd/api/main.go", e.Name, e.SourceFile)
		}
		if e.Language != "go" {
			t.Errorf("entity %q: Language = %q, want go", e.Name, e.Language)
		}
		// Synthetic http_endpoint entities emitted by the response-shape
		// pass (#722) intentionally carry framework=gin / pattern_type=
		// http_endpoint_synthesis — those are not subject to the YAML
		// invariants below.
		// #1217: skip all three http endpoint kind variants (synthesis emits definition/call now).
		if e.Kind == httpEndpointKind || e.Kind == httpEndpointDefinitionKind || e.Kind == httpEndpointCallKind {
			continue
		}
		if e.Properties["framework"] != "go" {
			t.Errorf("entity %q: framework property = %q, want go", e.Name, e.Properties["framework"])
		}
		if e.Properties["pattern_type"] != "yaml_driven" {
			t.Errorf("entity %q: pattern_type = %q, want yaml_driven", e.Name, e.Properties["pattern_type"])
		}
	}
}

func TestDetect_EnrichmentRequired(t *testing.T) {
	fsys := buildTestFS("go", "gin", ginYAML)
	rules, err := LoadAllRulesFromFS(fsys, "rules")
	if err != nil {
		t.Fatalf("LoadAllRulesFromFS failed: %v", err)
	}

	det := New(rules)
	result, err := det.Detect(context.Background(), extractor.FileInput{
		Path:     "cmd/api/main.go",
		Content:  []byte(sampleGinCode),
		Language: "go",
	})
	if err != nil {
		t.Fatalf("Detect failed: %v", err)
	}

	for _, e := range result.Entities {
		switch e.Kind {
		case "Controller", "Middleware":
			if !e.EnrichmentRequired {
				t.Errorf("entity %q (kind=%s): EnrichmentRequired should be true", e.Name, e.Kind)
			}
		case "Route", "Config":
			if e.EnrichmentRequired {
				t.Errorf("entity %q (kind=%s): EnrichmentRequired should be false", e.Name, e.Kind)
			}
		}
	}
}

func TestDetect_UnknownLanguage(t *testing.T) {
	fsys := buildTestFS("go", "gin", ginYAML)
	rules, err := LoadAllRulesFromFS(fsys, "rules")
	if err != nil {
		t.Fatalf("LoadAllRulesFromFS failed: %v", err)
	}

	det := New(rules)
	result, err := det.Detect(context.Background(), extractor.FileInput{
		Path:     "app.rs",
		Content:  []byte("fn main() {}"),
		Language: "rust",
	})
	if err != nil {
		t.Fatalf("Detect failed: %v", err)
	}
	if len(result.Entities) != 0 {
		t.Errorf("expected 0 entities for unknown language, got %d", len(result.Entities))
	}
	if len(result.Relationships) != 0 {
		t.Errorf("expected 0 relationships for unknown language, got %d", len(result.Relationships))
	}
}

func TestDetect_DjangoViews(t *testing.T) {
	fsys := buildTestFS("python", "django", djangoYAML)
	rules, err := LoadAllRulesFromFS(fsys, "rules")
	if err != nil {
		t.Fatalf("LoadAllRulesFromFS failed: %v", err)
	}

	det := New(rules)
	result, err := det.Detect(context.Background(), extractor.FileInput{
		Path:     "views.py",
		Content:  []byte(sampleDjangoCode),
		Language: "python",
	})
	if err != nil {
		t.Fatalf("Detect failed: %v", err)
	}

	controllerCount := countByKind(result.Entities, "Controller")
	if controllerCount != 2 {
		t.Errorf("expected 2 Controller entities (UserView, ProductView), got %d", controllerCount)
	}

	routeCount := countByKind(result.Entities, "Route")
	if routeCount < 2 {
		t.Errorf("expected at least 2 Route entities, got %d", routeCount)
	}

	modelCount := countByKind(result.Entities, "Model")
	if modelCount != 1 {
		t.Errorf("expected 1 Model entity, got %d", modelCount)
	}
}

func TestDetect_InvalidRegex(t *testing.T) {
	badYAML := `
source_patterns:
  - pattern: '(?P<bad[unclosed'
    entity_type: Route
    name_group: 1
    scope: file
  - pattern: 'func\s+(\w+)\s*\('
    entity_type: Function
    name_group: 1
    scope: file
relationship_rules: []
custom_extractors: []
`
	fsys := buildTestFS("go", "broken", badYAML)
	rules, err := LoadAllRulesFromFS(fsys, "rules")
	if err != nil {
		t.Fatalf("LoadAllRulesFromFS failed: %v", err)
	}

	det := New(rules)
	result, err := det.Detect(context.Background(), extractor.FileInput{
		Path:     "main.go",
		Content:  []byte("func Hello() {}"),
		Language: "go",
	})
	if err != nil {
		t.Fatalf("Detect should not error on invalid regex, got: %v", err)
	}
	// The valid pattern should still produce results.
	if len(result.Entities) != 1 {
		t.Errorf("expected 1 entity from valid pattern (skipping invalid), got %d", len(result.Entities))
	}
}

func TestDetect_RelationshipProperties(t *testing.T) {
	fsys := buildTestFS("go", "gin", ginYAML)
	rules, err := LoadAllRulesFromFS(fsys, "rules")
	if err != nil {
		t.Fatalf("LoadAllRulesFromFS failed: %v", err)
	}

	det := New(rules)
	result, err := det.Detect(context.Background(), extractor.FileInput{
		Path:     "main.go",
		Content:  []byte(sampleGinCode),
		Language: "go",
	})
	if err != nil {
		t.Fatalf("Detect failed: %v", err)
	}

	for _, rel := range result.Relationships {
		if rel.Kind != "ROUTES_TO" {
			t.Errorf("relationship Kind = %q, want ROUTES_TO", rel.Kind)
		}
		if rel.Properties["framework"] != "go" {
			t.Errorf("relationship framework property = %q, want go", rel.Properties["framework"])
		}
		if rel.Properties["pattern_type"] != "yaml_driven" {
			t.Errorf("relationship pattern_type = %q, want yaml_driven", rel.Properties["pattern_type"])
		}
		if rel.FromID == "" || rel.ToID == "" {
			t.Errorf("relationship has empty FromID or ToID: from=%q to=%q", rel.FromID, rel.ToID)
		}
	}
}

func TestDetect_NoDuplicateEntities(t *testing.T) {
	fsys := buildTestFS("go", "gin", ginYAML)
	rules, err := LoadAllRulesFromFS(fsys, "rules")
	if err != nil {
		t.Fatalf("LoadAllRulesFromFS failed: %v", err)
	}

	// Source with the same route registered twice.
	code := `package main
import "github.com/gin-gonic/gin"
func main() {
	r := gin.Default()
	r.GET("/health", HealthCheck)
	r.GET("/health", HealthCheck)
}
func HealthCheck(c *gin.Context) {}
`
	det := New(rules)
	result, err := det.Detect(context.Background(), extractor.FileInput{
		Path:     "main.go",
		Content:  []byte(code),
		Language: "go",
	})
	if err != nil {
		t.Fatalf("Detect failed: %v", err)
	}

	routeCount := countByKind(result.Entities, "Route")
	if routeCount != 1 {
		t.Errorf("expected 1 deduplicated Route entity, got %d", routeCount)
	}
}

func TestDetect_EmptyContent(t *testing.T) {
	fsys := buildTestFS("go", "gin", ginYAML)
	rules, err := LoadAllRulesFromFS(fsys, "rules")
	if err != nil {
		t.Fatalf("LoadAllRulesFromFS failed: %v", err)
	}

	det := New(rules)
	result, err := det.Detect(context.Background(), extractor.FileInput{
		Path:     "empty.go",
		Content:  []byte(""),
		Language: "go",
	})
	if err != nil {
		t.Fatalf("Detect failed: %v", err)
	}
	if len(result.Entities) != 0 {
		t.Errorf("expected 0 entities for empty content, got %d", len(result.Entities))
	}
}

func TestDetectorLanguages(t *testing.T) {
	fsys := buildMultiFS()
	rules, err := LoadAllRulesFromFS(fsys, "rules")
	if err != nil {
		t.Fatalf("LoadAllRulesFromFS failed: %v", err)
	}

	det := New(rules)
	langs := det.Languages()
	if len(langs) != 2 {
		t.Fatalf("expected 2 languages, got %d", len(langs))
	}
}

func TestDetectorRuleCount(t *testing.T) {
	fsys := buildMultiFS()
	rules, err := LoadAllRulesFromFS(fsys, "rules")
	if err != nil {
		t.Fatalf("LoadAllRulesFromFS failed: %v", err)
	}

	det := New(rules)
	if det.RuleCount() != 2 {
		t.Errorf("expected 2 total rules, got %d", det.RuleCount())
	}
}

func TestIsComplexEntity(t *testing.T) {
	tests := []struct {
		entityType string
		want       bool
	}{
		{"Controller", true},
		{"Middleware", true},
		{"Service", true},
		{"Repository", true},
		{"Model", true},
		{"Route", false},
		{"Config", false},
		{"Unknown", false},
	}
	for _, tc := range tests {
		if got := isComplexEntity(tc.entityType); got != tc.want {
			t.Errorf("isComplexEntity(%q) = %v, want %v", tc.entityType, got, tc.want)
		}
	}
}

func TestExtractGroup(t *testing.T) {
	match := []string{"full", "group1", "group2"}
	if got := extractGroup(match, 0); got != "full" {
		t.Errorf("extractGroup(match, 0) = %q, want full", got)
	}
	if got := extractGroup(match, 1); got != "group1" {
		t.Errorf("extractGroup(match, 1) = %q, want group1", got)
	}
	if got := extractGroup(match, 5); got != "" {
		t.Errorf("extractGroup(match, 5) = %q, want empty", got)
	}
	if got := extractGroup(match, -1); got != "" {
		t.Errorf("extractGroup(match, -1) = %q, want empty", got)
	}
}

// countByKind counts entities with the given Kind.
func countByKind(entities []types.EntityRecord, kind string) int {
	n := 0
	for _, e := range entities {
		if e.Kind == kind {
			n++
		}
	}
	return n
}

// ---------------------------------------------------------------------------
// C# RE2-rewritten relationship rule tests
// ---------------------------------------------------------------------------

// aspNetMVCInjectedIntoYAML is the relationship rule for Controller DI (RE2 rewrite).
const aspNetMVCInjectedIntoYAML = `
file_conventions: []
source_patterns: []
relationship_rules:
  - pattern: "class\\s+(\\w+Controller)[^{]*\\{[\\s\\S]{0,400}?public\\s+\\w+Controller\\s*\\([^)]*?(I\\w+)\\s+\\w+"
    source_type: Dependency
    target_type: Controller
    relationship: INJECTED_INTO
    source_group: 2
    target_group: 1
custom_extractors: []
`

// netMAUIInjectedIntoYAML is the relationship rule for ViewModel DI (RE2 rewrite).
const netMAUIInjectedIntoYAML = `
file_conventions: []
source_patterns: []
relationship_rules:
  - pattern: "class\\s+(\\w+ViewModel)[^{]*\\{[\\s\\S]{0,600}?public\\s+\\w+ViewModel\\s*\\([^)]*?(I\\w+)\\s+\\w+"
    source_type: Dependency
    target_type: Controller
    relationship: INJECTED_INTO
    source_group: 2
    target_group: 1
custom_extractors: []
`

// sampleASPNetMVCCode is a representative ASP.NET MVC controller with constructor DI.
const sampleASPNetMVCCode = `using Microsoft.AspNetCore.Mvc;

namespace MyApp.Controllers
{
    public class OrdersController : ControllerBase
    {
        private readonly IOrderService _svc;

        public OrdersController(IOrderService orderService, ILogger<OrdersController> logger)
        {
            _svc = orderService;
        }

        [HttpGet]
        public IActionResult List() => Ok(_svc.GetAll());
    }
}`

// sampleNetMAUICode is a representative MAUI ViewModel with constructor DI.
const sampleNetMAUICode = `using CommunityToolkit.Mvvm.ComponentModel;

namespace MyApp.ViewModels
{
    public partial class OrdersViewModel : ObservableObject
    {
        private readonly IOrderService _svc;

        public OrdersViewModel(IOrderService orderService)
        {
            _svc = orderService;
        }
    }
}`

// sampleASPNetMVCNoMatch is a controller without a constructor — pattern must NOT match.
const sampleASPNetMVCNoMatch = `using Microsoft.AspNetCore.Mvc;

namespace MyApp.Controllers
{
    public class HealthController : ControllerBase
    {
        [HttpGet("/health")]
        public IActionResult Ping() => Ok("pong");
    }
}`

// sampleNetMAUINoMatch is a ViewModel without constructor DI — pattern must NOT match.
const sampleNetMAUINoMatch = `namespace MyApp.ViewModels
{
    public partial class SettingsViewModel : ObservableObject
    {
        public string Title => "Settings";
    }
}`

// TestDetect_CSharpASPNetMVC_InjectedInto verifies that the RE2-rewritten
// Controller INJECTED_INTO pattern detects constructor injection relationships.
func TestDetect_CSharpASPNetMVC_InjectedInto(t *testing.T) {
	fsys := buildTestFS("csharp", "asp_net_mvc", aspNetMVCInjectedIntoYAML)
	rules, err := LoadAllRulesFromFS(fsys, "rules")
	if err != nil {
		t.Fatalf("LoadAllRulesFromFS failed: %v", err)
	}

	det := New(rules)
	result, err := det.Detect(context.Background(), extractor.FileInput{
		Path:     "Controllers/OrdersController.cs",
		Content:  []byte(sampleASPNetMVCCode),
		Language: "csharp",
	})
	if err != nil {
		t.Fatalf("Detect failed: %v", err)
	}

	if len(result.Relationships) == 0 {
		t.Fatal("expected at least one INJECTED_INTO relationship, got 0")
	}

	var found bool
	for _, rel := range result.Relationships {
		if rel.Kind == "INJECTED_INTO" &&
			rel.FromID == "Dependency:IOrderService" &&
			rel.ToID == "Controller:OrdersController" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected INJECTED_INTO from Dependency:IOrderService to Controller:OrdersController; got: %v", result.Relationships)
	}
}

// TestDetect_CSharpASPNetMVC_InjectedInto_NoMatch verifies that a controller
// WITHOUT constructor DI does NOT produce an INJECTED_INTO relationship.
func TestDetect_CSharpASPNetMVC_InjectedInto_NoMatch(t *testing.T) {
	fsys := buildTestFS("csharp", "asp_net_mvc", aspNetMVCInjectedIntoYAML)
	rules, err := LoadAllRulesFromFS(fsys, "rules")
	if err != nil {
		t.Fatalf("LoadAllRulesFromFS failed: %v", err)
	}

	det := New(rules)
	result, err := det.Detect(context.Background(), extractor.FileInput{
		Path:     "Controllers/HealthController.cs",
		Content:  []byte(sampleASPNetMVCNoMatch),
		Language: "csharp",
	})
	if err != nil {
		t.Fatalf("Detect failed: %v", err)
	}

	for _, rel := range result.Relationships {
		if rel.Kind == "INJECTED_INTO" {
			t.Errorf("unexpected INJECTED_INTO relationship for controller without DI: %v", rel)
		}
	}
}

// TestDetect_NetMAUI_InjectedInto verifies that the RE2-rewritten
// ViewModel INJECTED_INTO pattern detects constructor injection relationships.
func TestDetect_NetMAUI_InjectedInto(t *testing.T) {
	fsys := buildTestFS("csharp", "net_maui", netMAUIInjectedIntoYAML)
	rules, err := LoadAllRulesFromFS(fsys, "rules")
	if err != nil {
		t.Fatalf("LoadAllRulesFromFS failed: %v", err)
	}

	det := New(rules)
	result, err := det.Detect(context.Background(), extractor.FileInput{
		Path:     "ViewModels/OrdersViewModel.cs",
		Content:  []byte(sampleNetMAUICode),
		Language: "csharp",
	})
	if err != nil {
		t.Fatalf("Detect failed: %v", err)
	}

	if len(result.Relationships) == 0 {
		t.Fatal("expected at least one INJECTED_INTO relationship, got 0")
	}

	var found bool
	for _, rel := range result.Relationships {
		if rel.Kind == "INJECTED_INTO" &&
			rel.FromID == "Dependency:IOrderService" &&
			rel.ToID == "Controller:OrdersViewModel" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected INJECTED_INTO from Dependency:IOrderService to Controller:OrdersViewModel; got: %v", result.Relationships)
	}
}

// TestDetect_NetMAUI_InjectedInto_NoMatch verifies that a ViewModel WITHOUT
// constructor DI does NOT produce an INJECTED_INTO relationship.
func TestDetect_NetMAUI_InjectedInto_NoMatch(t *testing.T) {
	fsys := buildTestFS("csharp", "net_maui", netMAUIInjectedIntoYAML)
	rules, err := LoadAllRulesFromFS(fsys, "rules")
	if err != nil {
		t.Fatalf("LoadAllRulesFromFS failed: %v", err)
	}

	det := New(rules)
	result, err := det.Detect(context.Background(), extractor.FileInput{
		Path:     "ViewModels/SettingsViewModel.cs",
		Content:  []byte(sampleNetMAUINoMatch),
		Language: "csharp",
	})
	if err != nil {
		t.Fatalf("Detect failed: %v", err)
	}

	for _, rel := range result.Relationships {
		if rel.Kind == "INJECTED_INTO" {
			t.Errorf("unexpected INJECTED_INTO relationship for ViewModel without DI: %v", rel)
		}
	}
}

// TestDetect_CSharpRE2Patterns_Compile verifies that the rewritten C# patterns
// compile under Go's RE2 engine without error (backreferences would fail here).
func TestDetect_CSharpRE2Patterns_Compile(t *testing.T) {
	// These are the literal Go regexp strings stored in the YAML files
	// (after YAML double-quote unescaping: \\ → \).
	patterns := []struct {
		name          string
		pattern       string
		expectFailure bool
	}{
		{
			name:    "asp_net_mvc INJECTED_INTO (RE2 rewrite)",
			pattern: `class\s+(\w+Controller)[^{]*\{[\s\S]{0,400}?public\s+\w+Controller\s*\([^)]*?(I\w+)\s+\w+`,
		},
		{
			name:    "net_maui INJECTED_INTO (RE2 rewrite)",
			pattern: `class\s+(\w+ViewModel)[^{]*\{[\s\S]{0,600}?public\s+\w+ViewModel\s*\([^)]*?(I\w+)\s+\w+`,
		},
		// Negative case: the original backreference patterns must fail to compile.
		{
			name:          "asp_net_mvc INJECTED_INTO (original RE2-incompatible backref)",
			pattern:       `class\s+(\w+Controller)[^{]*\{[\s\S]{0,400}?public\s+\1\s*\([^)]*?(I\w+)\s+\w+`,
			expectFailure: true,
		},
		{
			name:          "net_maui INJECTED_INTO (original RE2-incompatible backref)",
			pattern:       `class\s+(\w+ViewModel)[^{]*\{[\s\S]{0,600}?public\s+\1\s*\([^)]*?(I\w+)\s+\w+`,
			expectFailure: true,
		},
	}

	for _, tc := range patterns {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			_, err := regexp.Compile(tc.pattern)
			if tc.expectFailure {
				if err == nil {
					t.Errorf("expected compile failure for backref pattern %q, but it succeeded", tc.pattern)
				}
			} else {
				if err != nil {
					t.Errorf("expected RE2 compile success for %q, got: %v", tc.pattern, err)
				}
			}
		})
	}
}

// TestDetect_CDKCfnOutput_OutputExportExtraction drives the real embedded
// rules/javascript_typescript/frameworks/aws_cdk.yaml against a CfnOutput
// statement and asserts the published-output entity is extracted. This is the
// value-asserting test backing the iac_output_export_extraction (#4195)
// capability credit for AWS CDK: a `new cdk.CfnOutput(this, 'ApiUrl', …)` must
// surface as a Config entity named by its OutputId literal ('ApiUrl').
func TestDetect_CDKCfnOutput_OutputExportExtraction(t *testing.T) {
	rules, err := LoadAllRules()
	if err != nil {
		t.Fatalf("LoadAllRules failed: %v", err)
	}
	det := New(rules)
	src := `import * as cdk from 'aws-cdk-lib';

export class ApiStack extends cdk.Stack {
  constructor(scope: cdk.App, id: string) {
    super(scope, id);
    new cdk.CfnOutput(this, 'ApiUrl', { value: 'https://example.com' });
  }
}
`
	result, err := det.Detect(context.Background(), extractor.FileInput{
		Path:     "lib/api-stack.ts",
		Content:  []byte(src),
		Language: "typescript",
	})
	if err != nil {
		t.Fatalf("Detect failed: %v", err)
	}

	// The aws_cdk.yaml CfnOutput entity rule (name_group=1) must surface the
	// OutputId literal 'ApiUrl' as an entity. (The downstream CDK normaliser
	// settles its Kind to SCOPE.InfraResource — what matters for #4195 is that
	// the published-output identifier is extracted as a first-class entity and
	// is not dropped.)
	var found bool
	for _, e := range result.Entities {
		if e.Name == "ApiUrl" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected CfnOutput entity named 'ApiUrl' (the OutputId literal), got %+v", result.Entities)
	}
}
