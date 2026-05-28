package extractors

import (
	"context"
	"strings"
	"testing"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/cajasmota/archigraph/internal/types"
)

// ---- CustomExtractorsFor -----------------------------------------------------

func TestCustomExtractorsForPythonReturnsPythonPrefixedKeys(t *testing.T) {
	cleanRegistry(t)

	// Base python extractor should NOT be returned (exact key match excluded).
	Register("python", &mockExtractor{language: "python"})
	// Custom python framework extractors (prefixed) SHOULD be returned.
	Register("python_django", &mockExtractor{language: "python_django"})
	Register("python_flask", &mockExtractor{language: "python_flask"})
	// A non-python custom extractor must NOT leak in.
	Register("custom_go_gin", &mockExtractor{language: "custom_go_gin"})

	got := CustomExtractorsFor("python")
	if len(got) != 2 {
		t.Fatalf("expected 2 python custom extractors, got %d", len(got))
	}
	// Sorted dispatch order: django < flask.
	if got[0].Language() != "python_django" {
		t.Errorf("position 0: expected python_django, got %s", got[0].Language())
	}
	if got[1].Language() != "python_flask" {
		t.Errorf("position 1: expected python_flask, got %s", got[1].Language())
	}
}

func TestCustomExtractorsForGoReturnsCustomGoPrefixedKeys(t *testing.T) {
	cleanRegistry(t)

	Register("go", &mockExtractor{language: "go"})
	Register("custom_go_gin", &mockExtractor{language: "custom_go_gin"})
	Register("custom_go_echo", &mockExtractor{language: "custom_go_echo"})
	Register("python_django", &mockExtractor{language: "python_django"})

	got := CustomExtractorsFor("go")
	if len(got) != 2 {
		t.Fatalf("expected 2 go custom extractors, got %d", len(got))
	}
	if got[0].Language() != "custom_go_echo" {
		t.Errorf("position 0: expected custom_go_echo, got %s", got[0].Language())
	}
	if got[1].Language() != "custom_go_gin" {
		t.Errorf("position 1: expected custom_go_gin, got %s", got[1].Language())
	}
}

func TestCustomExtractorsForTypescriptSharesJavascriptPrefix(t *testing.T) {
	cleanRegistry(t)
	Register("custom_js_react", &mockExtractor{language: "custom_js_react"})
	Register("custom_js_nextjs", &mockExtractor{language: "custom_js_nextjs"})

	got := CustomExtractorsFor("typescript")
	if len(got) != 2 {
		t.Fatalf("expected 2 custom extractors for typescript (via js prefix), got %d", len(got))
	}
}

func TestCustomExtractorsForUnknownLanguageReturnsEmpty(t *testing.T) {
	cleanRegistry(t)
	Register("custom_go_gin", &mockExtractor{language: "custom_go_gin"})

	got := CustomExtractorsFor("fortran")
	if got == nil {
		t.Fatal("expected non-nil empty slice")
	}
	if len(got) != 0 {
		t.Errorf("expected empty list for unknown language, got %d entries", len(got))
	}
}

func TestCustomExtractorsForLanguageWithoutRegisteredCustomsReturnsEmpty(t *testing.T) {
	cleanRegistry(t)
	// Only base extractor registered, no prefix hits.
	Register("swift", &mockExtractor{language: "swift"})

	got := CustomExtractorsFor("swift")
	if len(got) != 0 {
		t.Errorf("expected 0 custom extractors for swift, got %d", len(got))
	}
}

// Covers every language listed in customPrefixForLanguage to guarantee the
// mapping stays in sync with the internal/custom/ directory structure. If a
// new language is added to the map without fixture registration here, the
// test helps catch drift.
func TestCustomExtractorsForEveryMappedLanguageIsReachable(t *testing.T) {
	cleanRegistry(t)

	// Register exactly one custom extractor per prefix.
	fixtures := map[string]string{
		"python":     "python_fixture",
		"go":         "custom_go_fixture",
		"javascript": "custom_js_fixture",
		"java":       "custom_java_fixture",
		"kotlin":     "custom_kotlin_fixture",
		"scala":      "custom_scala_fixture",
		"ruby":       "custom_ruby_fixture",
		"php":        "custom_php_fixture",
		"rust":       "custom_rust_fixture",
		"swift":      "custom_swift_fixture",
		"dart":       "custom_dart_fixture",
		"elixir":     "custom_elixir_fixture",
		"csharp":     "custom_csharp_fixture",
		"cpp":        "custom_cpp_fixture",
	}
	for _, key := range fixtures {
		Register(key, &mockExtractor{language: key})
	}

	for lang := range fixtures {
		got := CustomExtractorsFor(lang)
		if len(got) == 0 {
			t.Errorf("language %q: expected at least one custom extractor, got 0", lang)
		}
	}
	// Typescript shares the js prefix — should find the js fixture.
	if got := CustomExtractorsFor("typescript"); len(got) == 0 {
		t.Error("typescript: expected at least one custom extractor via js prefix, got 0")
	}
}

// ---- RunCustomExtractors -----------------------------------------------------

func TestRunCustomExtractorsDispatchesAllMatchingExtractors(t *testing.T) {
	cleanRegistry(t)

	Register("python_django", &mockExtractor{
		language: "python_django",
		records:  []types.EntityRecord{{Name: "UserView", Kind: "SCOPE.View"}},
	})
	Register("python_flask", &mockExtractor{
		language: "python_flask",
		records:  []types.EntityRecord{{Name: "login_route", Kind: "SCOPE.Route"}},
	})
	// Unrelated language — must not fire.
	Register("custom_go_gin", &mockExtractor{
		language: "custom_go_gin",
		records:  []types.EntityRecord{{Name: "should_not_appear", Kind: "SCOPE.Route"}},
	})

	ctx := context.Background()
	entities, errs := RunCustomExtractors(ctx, FileInput{
		Path:     "views.py",
		Language: "python",
	})
	if len(errs) != 0 {
		t.Errorf("expected no errors, got %d: %v", len(errs), errs)
	}
	if len(entities) != 2 {
		t.Fatalf("expected 2 entities, got %d", len(entities))
	}

	names := map[string]bool{}
	for _, e := range entities {
		names[e.Name] = true
	}
	if !names["UserView"] || !names["login_route"] {
		t.Errorf("expected UserView and login_route, got %v", names)
	}
	if names["should_not_appear"] {
		t.Error("go extractor leaked into python dispatch")
	}
}

func TestRunCustomExtractorsRecoversFromPanic(t *testing.T) {
	cleanRegistry(t)

	Register("python_django", &mockExtractor{
		language: "python_django",
		panic:    true,
	})
	Register("python_flask", &mockExtractor{
		language: "python_flask",
		records:  []types.EntityRecord{{Name: "survivor", Kind: "SCOPE.Route"}},
	})

	ctx := context.Background()
	entities, errs := RunCustomExtractors(ctx, FileInput{
		Path:     "app.py",
		Language: "python",
	})

	// Survivor must still emit even though django panicked.
	if len(entities) != 1 {
		t.Fatalf("expected 1 surviving entity, got %d", len(entities))
	}
	if entities[0].Name != "survivor" {
		t.Errorf("expected survivor, got %s", entities[0].Name)
	}
	// Panic must surface as an error entry identifying the extractor.
	if len(errs) != 1 {
		t.Fatalf("expected 1 error from panic, got %d", len(errs))
	}
	if !strings.Contains(errs[0].Error(), "python_django") {
		t.Errorf("error should mention python_django, got: %v", errs[0])
	}
	if !strings.Contains(errs[0].Error(), "panicked") {
		t.Errorf("error should mention panic, got: %v", errs[0])
	}
}

func TestRunCustomExtractorsWithNoMatchingExtractorsReturnsEmpty(t *testing.T) {
	cleanRegistry(t)
	Register("python_django", &mockExtractor{language: "python_django"})

	ctx := context.Background()
	entities, errs := RunCustomExtractors(ctx, FileInput{
		Path:     "main.go",
		Language: "go",
	})
	if entities != nil {
		t.Errorf("expected nil entities, got %v", entities)
	}
	if errs != nil {
		t.Errorf("expected nil errors, got %v", errs)
	}
}

func TestRunCustomExtractorsEmitsOTelSpanWithCustomExtractorCount(t *testing.T) {
	cleanRegistry(t)
	Register("python_django", &mockExtractor{
		language: "python_django",
		records:  []types.EntityRecord{{Name: "V1"}},
	})
	Register("python_flask", &mockExtractor{
		language: "python_flask",
		records:  []types.EntityRecord{{Name: "V2"}, {Name: "V3"}},
	})

	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	SetTracer(tp.Tracer("test"))
	defer SetTracer(nil)

	ctx := context.Background()
	_, _ = RunCustomExtractors(ctx, FileInput{
		Path:     "views.py",
		Language: "python",
	})

	spans := rec.Ended()
	var dispatchSpan sdktrace.ReadOnlySpan
	for _, s := range spans {
		if s.Name() == "extractor.custom_dispatch" {
			dispatchSpan = s
			break
		}
	}
	if dispatchSpan == nil {
		t.Fatal("expected extractor.custom_dispatch span to be emitted")
	}

	attrs := spanAttrMap(dispatchSpan.Attributes())
	if v, ok := attrs["custom_extractor_count"]; !ok || v.AsInt64() != 2 {
		t.Errorf("expected custom_extractor_count=2, got %v", v)
	}
	if v, ok := attrs["entity_count"]; !ok || v.AsInt64() != 3 {
		t.Errorf("expected entity_count=3, got %v", v)
	}
	checkAttr(t, attrs, "language", "python")
	checkAttr(t, attrs, "file", "views.py")
	if _, ok := attrs["duration_ms"]; !ok {
		t.Error("expected duration_ms attribute on span")
	}
}

func TestRunCustomExtractorsCollectsErrorsButPreservesPartialOutput(t *testing.T) {
	cleanRegistry(t)

	Register("python_django", &mockExtractor{
		language: "python_django",
		records:  []types.EntityRecord{{Name: "A"}},
		err:      errorExtractor("django failed mid-run"),
	})
	Register("python_flask", &mockExtractor{
		language: "python_flask",
		records:  []types.EntityRecord{{Name: "B"}},
	})

	ctx := context.Background()
	entities, errs := RunCustomExtractors(ctx, FileInput{
		Path:     "app.py",
		Language: "python",
	})

	// Both entities must be kept — partial results on error are preserved.
	if len(entities) != 2 {
		t.Fatalf("expected 2 entities (partial+success), got %d", len(entities))
	}
	if len(errs) != 1 {
		t.Fatalf("expected 1 error, got %d: %v", len(errs), errs)
	}
}

// ---- MergeWithCustom ---------------------------------------------------------

func TestMergeWithCustomReturnsBaseWhenCustomEmpty(t *testing.T) {
	base := []types.EntityRecord{{Name: "A"}, {Name: "B"}}
	got := MergeWithCustom(base, nil)
	if len(got) != 2 {
		t.Fatalf("expected 2 entities, got %d", len(got))
	}
}

func TestMergeWithCustomOverridesBaseEntityByName(t *testing.T) {
	base := []types.EntityRecord{
		{Name: "UserView", Kind: "SCOPE.Class", Signature: "base"},
		{Name: "Helper", Kind: "SCOPE.Function"},
	}
	custom := []types.EntityRecord{
		{Name: "UserView", Kind: "SCOPE.View", Signature: "custom"},
	}
	got := MergeWithCustom(base, custom)

	if len(got) != 2 {
		t.Fatalf("expected 2 merged entities, got %d", len(got))
	}
	// UserView must be the custom version; Helper must be the base version.
	for _, e := range got {
		switch e.Name {
		case "UserView":
			if e.Kind != "SCOPE.View" || e.Signature != "custom" {
				t.Errorf("UserView should be custom, got kind=%s signature=%s", e.Kind, e.Signature)
			}
		case "Helper":
			if e.Kind != "SCOPE.Function" {
				t.Errorf("Helper should be unchanged, got kind=%s", e.Kind)
			}
		default:
			t.Errorf("unexpected entity %s", e.Name)
		}
	}
}

func TestMergeWithCustomAppendsNewCustomEntities(t *testing.T) {
	base := []types.EntityRecord{{Name: "A", Kind: "SCOPE.Function"}}
	custom := []types.EntityRecord{
		{Name: "B", Kind: "SCOPE.View"},
		{Name: "C", Kind: "SCOPE.Route"},
	}
	got := MergeWithCustom(base, custom)
	if len(got) != 3 {
		t.Fatalf("expected 3 entities, got %d", len(got))
	}
	if got[0].Name != "A" || got[1].Name != "B" || got[2].Name != "C" {
		t.Errorf("unexpected merge order: %v", extractNames(got))
	}
}

func TestMergeWithCustomPreservesBaseOrder(t *testing.T) {
	base := []types.EntityRecord{
		{Name: "First"},
		{Name: "Second"},
		{Name: "Third"},
	}
	custom := []types.EntityRecord{
		{Name: "Second", Kind: "SCOPE.View"}, // overrides middle
	}
	got := MergeWithCustom(base, custom)
	if len(got) != 3 {
		t.Fatalf("expected 3 entities, got %d", len(got))
	}
	if got[0].Name != "First" || got[1].Name != "Second" || got[2].Name != "Third" {
		t.Errorf("merge did not preserve base order: %v", extractNames(got))
	}
	if got[1].Kind != "SCOPE.View" {
		t.Errorf("Second was not overridden, got kind=%s", got[1].Kind)
	}
}

// ---- helpers ----------------------------------------------------------------

// errorExtractor adapts a string into an error for inline test use.
type errorExtractor string

func (e errorExtractor) Error() string { return string(e) }

func extractNames(recs []types.EntityRecord) []string {
	names := make([]string, len(recs))
	for i, r := range recs {
		names[i] = r.Name
	}
	return names
}

// Guard: assert the registry testing hook exists (catch refactors that remove it).
// Uses cleanRegistry so the snapshot/restore cycle is also exercised here.
func TestClearForTestingExists(t *testing.T) {
	cleanRegistry(t)
}
