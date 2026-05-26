package razor_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/cajasmota/archigraph/internal/extractor"
	_ "github.com/cajasmota/archigraph/internal/extractors/razor" // trigger init()
	"github.com/cajasmota/archigraph/internal/types"
)

// ---- helpers ----------------------------------------------------------------

func extract(t *testing.T, path string, src string) []types.EntityRecord {
	t.Helper()
	ext, ok := extractor.Get("razor")
	if !ok {
		t.Fatal("razor extractor not registered")
	}
	got, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     path,
		Content:  []byte(src),
		Language: "razor",
	})
	if err != nil {
		t.Fatalf("Extract returned error: %v", err)
	}
	return got
}

func extractFromFile(t *testing.T, path string) []types.EntityRecord {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture %s: %v", path, err)
	}
	return extract(t, filepath.Base(path), string(data))
}

func findByName(entities []types.EntityRecord, name string) *types.EntityRecord {
	for i := range entities {
		if entities[i].Name == name {
			return &entities[i]
		}
	}
	return nil
}

func countByKind(entities []types.EntityRecord, kind string) int {
	n := 0
	for _, e := range entities {
		if e.Kind == kind {
			n++
		}
	}
	return n
}

func countBySubtype(entities []types.EntityRecord, subtype string) int {
	n := 0
	for _, e := range entities {
		if e.Subtype == subtype {
			n++
		}
	}
	return n
}

// ---- Language/Registration --------------------------------------------------

func TestExtractor_Language(t *testing.T) {
	ext, ok := extractor.Get("razor")
	if !ok {
		t.Fatal("razor extractor not registered")
	}
	if ext.Language() != "razor" {
		t.Errorf("Language() = %q, want %q", ext.Language(), "razor")
	}
}

// ---- Component name ---------------------------------------------------------

func TestExtractor_ComponentName_FromPath(t *testing.T) {
	src := `<h1>Hello</h1>`
	entities := extract(t, "MyPage.razor", src)
	if len(entities) == 0 {
		t.Fatal("expected at least 1 entity")
	}
	if entities[0].Name != "MyPage" {
		t.Errorf("component name = %q, want %q", entities[0].Name, "MyPage")
	}
}

func TestExtractor_ComponentName_NestedPath(t *testing.T) {
	src := `<h1>Hello</h1>`
	entities := extract(t, "Pages/Counter.razor", src)
	if entities[0].Name != "Counter" {
		t.Errorf("component name = %q, want %q", entities[0].Name, "Counter")
	}
}

func TestExtractor_ComponentKind_IsUIComponent(t *testing.T) {
	src := `<h1>Hello</h1>`
	entities := extract(t, "MyComp.razor", src)
	if entities[0].Kind != "SCOPE.UIComponent" {
		t.Errorf("kind = %q, want SCOPE.UIComponent", entities[0].Kind)
	}
}

func TestExtractor_ComponentSubtype_IsComponent(t *testing.T) {
	src := `<h1>Hello</h1>`
	entities := extract(t, "MyComp.razor", src)
	if entities[0].Subtype != "component" {
		t.Errorf("subtype = %q, want component", entities[0].Subtype)
	}
}

// ---- Empty file / no @code block -------------------------------------------

func TestExtractor_EmptyContent_ReturnsDegraded(t *testing.T) {
	entities := extract(t, "Empty.razor", "")
	if len(entities) != 1 {
		t.Fatalf("len = %d, want 1", len(entities))
	}
	if entities[0].QualityScore != 0.3 {
		t.Errorf("quality_score = %f, want 0.3", entities[0].QualityScore)
	}
	if entities[0].EnrichmentStatus != types.StatusDegraded {
		t.Errorf("enrichment_status = %q, want degraded", entities[0].EnrichmentStatus)
	}
}

func TestExtractor_NoCodeBlock_ExactlyOneEntity(t *testing.T) {
	src := `<h1>Just Markup</h1>
<p>No C# here.</p>`
	entities := extract(t, "EmptyComponent.razor", src)
	if len(entities) != 1 {
		t.Errorf("len = %d, want 1 for no-@code-block file", len(entities))
	}
	if entities[0].Name != "EmptyComponent" {
		t.Errorf("name = %q, want EmptyComponent", entities[0].Name)
	}
}

func TestExtractor_NoCodeBlock_QualityScore(t *testing.T) {
	src := `<h1>Markup only</h1>`
	entities := extract(t, "PureMarkup.razor", src)
	if entities[0].QualityScore < 0.8 {
		t.Errorf("quality_score = %f, want >= 0.8 for component entity", entities[0].QualityScore)
	}
}

// ---- Fixture: EmptyComponent.razor -----------------------------------------

func TestFixture_EmptyComponent(t *testing.T) {
	entities := extractFromFile(t, "../../../testdata/fixtures/sources/razor/EmptyComponent.razor")
	if len(entities) != 1 {
		t.Errorf("EmptyComponent fixture: len = %d, want exactly 1", len(entities))
	}
	if entities[0].Name != "EmptyComponent" {
		t.Errorf("name = %q, want EmptyComponent", entities[0].Name)
	}
}

// ---- @inject ----------------------------------------------------------------

func TestExtractor_Inject_Detected(t *testing.T) {
	src := `@inject IWeatherService WeatherSvc
<h1>Hello</h1>`
	entities := extract(t, "Weather.razor", src)
	inj := findByName(entities, "WeatherSvc")
	if inj == nil {
		t.Fatal("expected inject entity WeatherSvc")
	}
	if inj.Subtype != "inject" {
		t.Errorf("subtype = %q, want inject", inj.Subtype)
	}
	if inj.Kind != "SCOPE.UIComponent" {
		t.Errorf("kind = %q, want SCOPE.UIComponent", inj.Kind)
	}
}

func TestExtractor_Inject_ServiceTypeInProperties(t *testing.T) {
	src := `@inject INavigationManager NavManager
<h1>Hello</h1>`
	entities := extract(t, "Nav.razor", src)
	inj := findByName(entities, "NavManager")
	if inj == nil {
		t.Fatal("inject entity NavManager not found")
	}
	if inj.Properties["service_type"] != "INavigationManager" {
		t.Errorf("service_type = %q, want INavigationManager", inj.Properties["service_type"])
	}
}

func TestExtractor_MultipleInjects(t *testing.T) {
	src := `@inject IUserService UserSvc
@inject ILoggerFactory LoggerFactory
<h1>Hello</h1>`
	entities := extract(t, "Multi.razor", src)
	if countBySubtype(entities, "inject") != 2 {
		t.Errorf("inject count = %d, want 2", countBySubtype(entities, "inject"))
	}
}

// ---- Fixture: WithInject.razor ---------------------------------------------

func TestFixture_WithInject(t *testing.T) {
	entities := extractFromFile(t, "../../../testdata/fixtures/sources/razor/WithInject.razor")
	if countBySubtype(entities, "inject") == 0 {
		t.Error("WithInject fixture: expected at least 1 inject entity")
	}
}

// ---- [Parameter] properties -------------------------------------------------

func TestExtractor_Parameter_Detected(t *testing.T) {
	src := `@code {
    [Parameter]
    public int Count { get; set; }
}`
	entities := extract(t, "Counter.razor", src)
	param := findByName(entities, "Count")
	if param == nil {
		t.Fatal("expected parameter entity Count")
	}
	if param.Kind != "SCOPE.Component" {
		t.Errorf("kind = %q, want SCOPE.Component", param.Kind)
	}
	if param.Subtype != "parameter" {
		t.Errorf("subtype = %q, want parameter", param.Subtype)
	}
}

func TestExtractor_Parameter_TypeInProperties(t *testing.T) {
	src := `@code {
    [Parameter]
    public string Title { get; set; }
}`
	entities := extract(t, "Widget.razor", src)
	param := findByName(entities, "Title")
	if param == nil {
		t.Fatal("Title parameter not found")
	}
	if param.Properties["property_type"] != "string" {
		t.Errorf("property_type = %q, want string", param.Properties["property_type"])
	}
}

func TestExtractor_MultipleParameters(t *testing.T) {
	src := `@code {
    [Parameter]
    public int Count { get; set; }

    [Parameter]
    public string Title { get; set; }

    [Parameter]
    public bool IsVisible { get; set; }
}`
	entities := extract(t, "Params.razor", src)
	if countBySubtype(entities, "parameter") != 3 {
		t.Errorf("parameter count = %d, want 3", countBySubtype(entities, "parameter"))
	}
}

func TestExtractor_CascadingParameter_Detected(t *testing.T) {
	src := `@code {
    [CascadingParameter]
    public string Theme { get; set; }
}`
	entities := extract(t, "Themed.razor", src)
	param := findByName(entities, "Theme")
	if param == nil {
		t.Fatal("CascadingParameter Theme not detected")
	}
	if param.Subtype != "parameter" {
		t.Errorf("subtype = %q, want parameter", param.Subtype)
	}
}

// ---- Event handlers ---------------------------------------------------------

func TestExtractor_EventHandler_Void(t *testing.T) {
	src := `@code {
    private void IncrementCount()
    {
        currentCount++;
    }
}`
	entities := extract(t, "Counter.razor", src)
	h := findByName(entities, "IncrementCount")
	if h == nil {
		t.Fatal("expected event handler IncrementCount")
	}
	if h.Kind != "SCOPE.Operation" {
		t.Errorf("kind = %q, want SCOPE.Operation", h.Kind)
	}
	if h.Subtype != "event_handler" {
		t.Errorf("subtype = %q, want event_handler", h.Subtype)
	}
}

func TestExtractor_EventHandler_Async(t *testing.T) {
	src := `@code {
    private async Task HandleSubmit()
    {
        await Task.Delay(100);
    }
}`
	entities := extract(t, "Form.razor", src)
	h := findByName(entities, "HandleSubmit")
	if h == nil {
		t.Fatal("async Task handler not detected")
	}
	if h.Subtype != "event_handler" {
		t.Errorf("subtype = %q, want event_handler", h.Subtype)
	}
}

func TestExtractor_EventHandler_MultipleHandlers(t *testing.T) {
	src := `@code {
    private void OnClick()
    {
    }

    private void OnHover()
    {
    }

    private async Task OnLoad()
    {
    }
}`
	entities := extract(t, "Events.razor", src)
	if countBySubtype(entities, "event_handler") != 3 {
		t.Errorf("event_handler count = %d, want 3", countBySubtype(entities, "event_handler"))
	}
}

// ---- Fixture: Counter.razor -------------------------------------------------

func TestFixture_Counter_AtLeastFiveEntities(t *testing.T) {
	entities := extractFromFile(t, "../../../testdata/fixtures/sources/razor/Counter.razor")
	if len(entities) < 5 {
		t.Errorf("Counter fixture: len = %d, want >= 5", len(entities))
		for _, e := range entities {
			t.Logf("  entity: name=%s kind=%s subtype=%s", e.Name, e.Kind, e.Subtype)
		}
	}
}

func TestFixture_Counter_HasComponentEntity(t *testing.T) {
	entities := extractFromFile(t, "../../../testdata/fixtures/sources/razor/Counter.razor")
	comp := findByName(entities, "Counter")
	if comp == nil {
		t.Fatal("Counter component entity not found")
	}
	if comp.Kind != "SCOPE.UIComponent" {
		t.Errorf("kind = %q, want SCOPE.UIComponent", comp.Kind)
	}
}

func TestFixture_Counter_HasParameters(t *testing.T) {
	entities := extractFromFile(t, "../../../testdata/fixtures/sources/razor/Counter.razor")
	if countBySubtype(entities, "parameter") == 0 {
		t.Error("Counter fixture: expected at least 1 parameter entity")
	}
}

func TestFixture_Counter_HasEventHandlers(t *testing.T) {
	entities := extractFromFile(t, "../../../testdata/fixtures/sources/razor/Counter.razor")
	if countBySubtype(entities, "event_handler") == 0 {
		t.Error("Counter fixture: expected at least 1 event_handler entity")
	}
}

// ---- Allowlist compliance ---------------------------------------------------

func TestExtractor_AllKindsInAllowlist(t *testing.T) {
	validKinds := map[string]bool{
		"SCOPE.Service":       true,
		"SCOPE.Component":     true,
		"SCOPE.Operation":     true,
		"SCOPE.Pattern":       true,
		"SCOPE.Evolution":     true,
		"SCOPE.Datastore":     true,
		"SCOPE.ExternalAPI":   true,
		"SCOPE.Event":         true,
		"SCOPE.Queue":         true,
		"SCOPE.Schema":        true,
		"SCOPE.ScopeUnknown":  true,
		"SCOPE.Stylesheet":    true,
		"SCOPE.UIComponent":   true,
		"SCOPE.InfraResource": true,
	}

	src := `@inject IService Svc
<h1>Hello</h1>
@code {
    [Parameter]
    public int Value { get; set; }

    private void OnClick() { }

    private async Task OnLoad() { }
}`
	entities := extract(t, "AllKinds.razor", src)
	for _, e := range entities {
		if !validKinds[e.Kind] {
			t.Errorf("entity %q has non-allowlist Kind %q", e.Name, e.Kind)
		}
	}
}

// ---- QualityScore -----------------------------------------------------------

func TestExtractor_QualityScore_InRange(t *testing.T) {
	src := `@inject IService Svc
<h1>Hello</h1>
@code {
    [Parameter]
    public int Count { get; set; }

    private void OnClick() { }
}`
	entities := extract(t, "Score.razor", src)
	for _, e := range entities {
		if e.QualityScore < 0 || e.QualityScore > 1 {
			t.Errorf("entity %q quality_score %f out of [0,1]", e.Name, e.QualityScore)
		}
	}
}

// ---- SourceFile / Language --------------------------------------------------

func TestExtractor_SourceFileSet(t *testing.T) {
	src := `<h1>Hello</h1>`
	entities := extract(t, "MyComp.razor", src)
	for _, e := range entities {
		if e.SourceFile == "" {
			t.Errorf("entity %q has empty SourceFile", e.Name)
		}
	}
}

func TestExtractor_LanguageIsRazor(t *testing.T) {
	src := `<h1>Hello</h1>`
	entities := extract(t, "MyComp.razor", src)
	for _, e := range entities {
		if e.Language != "razor" {
			t.Errorf("entity %q Language = %q, want razor", e.Name, e.Language)
		}
	}
}

// ---- Degraded marker --------------------------------------------------------

func TestExtractor_DegradedEntity_HasMetadata(t *testing.T) {
	entities := extract(t, "Bad.razor", "")
	if len(entities) != 1 {
		t.Fatalf("len = %d, want 1", len(entities))
	}
	e := entities[0]
	if e.Metadata == nil {
		t.Fatal("degraded entity has nil Metadata")
	}
	if e.Metadata["extraction_status"] != "degraded" {
		t.Errorf("extraction_status = %v, want degraded", e.Metadata["extraction_status"])
	}
}

// ---- QualifiedName ----------------------------------------------------------

func TestExtractor_QualifiedName_ContainsComponentName(t *testing.T) {
	src := `@code {
    [Parameter]
    public int Count { get; set; }
}`
	entities := extract(t, "Counter.razor", src)
	param := findByName(entities, "Count")
	if param == nil {
		t.Fatal("Count entity not found")
	}
	if !contains(param.QualifiedName, "Counter") {
		t.Errorf("QualifiedName %q should contain Counter", param.QualifiedName)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && containsStr(s, sub))
}

func containsStr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// ---- countByKind used in AllowlistCompliance test --------------------------

var _ = countByKind // suppress unused warning
