package extractors

import (
	"context"
	"strings"
	"testing"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/cajasmota/grafel/internal/types"
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
		"lua":        "lua_fixture",
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

// REWRITTEN FOR #6104. This test used to be
// TestMergeWithCustomOverridesBaseEntityByName and asserted that a custom
// SCOPE.View DESTROYS a base SCOPE.Class of the same Name. That is the defect
// itself: Name is not an identity, and two records that disagree on Kind are
// two different graph nodes (ComputeID includes Kind). The rewritten
// assertion is that BOTH survive and the custom one is still distinguishable.
func TestMergeWithCustomNameCollisionAcrossKindsKeepsBoth(t *testing.T) {
	base := []types.EntityRecord{
		{Name: "UserView", Kind: "SCOPE.Class", SourceFile: "v.py", Signature: "base"},
		{Name: "Helper", Kind: "SCOPE.Function", SourceFile: "v.py"},
	}
	custom := []types.EntityRecord{
		{Name: "UserView", Kind: "SCOPE.View", SourceFile: "v.py", Signature: "custom"},
	}
	got := MergeWithCustom(base, custom)

	if len(got) != 3 {
		t.Fatalf("expected 3 merged entities (nothing destroyed), got %d: %v", len(got), kindNames(got))
	}
	seen := map[string]types.EntityRecord{}
	for _, e := range got {
		seen[e.Kind+"|"+e.Name] = e
	}
	if e, ok := seen["SCOPE.Class|UserView"]; !ok || e.Signature != "base" {
		t.Errorf("base UserView was destroyed or mutated: %+v", e)
	}
	if e, ok := seen["SCOPE.View|UserView"]; !ok || e.Signature != "custom" {
		t.Errorf("custom UserView missing or mutated: %+v", e)
	}
	if e, ok := seen["SCOPE.Function|Helper"]; !ok || e.Kind != "SCOPE.Function" {
		t.Errorf("Helper should be unchanged, got %+v", e)
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
		{Name: "Second", Kind: "SCOPE.View"}, // name-twin of the middle entity
	}
	got := MergeWithCustom(base, custom)
	// #6104: the base entities carry no Kind, so the custom SCOPE.View is a
	// DIFFERENT graph node and is appended rather than replacing the middle
	// entity. Base order is still what this test is about.
	if len(got) != 4 {
		t.Fatalf("expected 4 entities, got %d: %v", len(got), kindNames(got))
	}
	if got[0].Name != "First" || got[1].Name != "Second" || got[2].Name != "Third" {
		t.Errorf("merge did not preserve base order: %v", extractNames(got))
	}
	if got[1].Kind != "" {
		t.Errorf("base Second must not be overridden, got kind=%s", got[1].Kind)
	}
	if got[3].Kind != "SCOPE.View" || got[3].Name != "Second" {
		t.Errorf("custom entity must be appended after the base run, got %+v", got[3])
	}
}

// TestMergeWithCustomPreservesBaseQualifiedName proves the #4402 property —
// a custom node that leaves QualifiedName empty inherits the base node's,
// which is what makes it resolvable by qualified name (#4379).
//
// REWRITTEN FOR #6104. #4402 obtained that state by DESTROYING the base node.
// It is now obtained by Tier B enrichment, with the base node still standing,
// so the assertions are per-Kind rather than per-Name and both nodes are
// checked. The QualifiedName rule itself is unchanged.
func TestMergeWithCustomPreservesBaseQualifiedName(t *testing.T) {
	base := []types.EntityRecord{
		{Name: "Contract", Kind: "SCOPE.Component", SourceFile: "m.py", QualifiedName: "app.models.Contract"},
		{Name: "Order", Kind: "SCOPE.Component", SourceFile: "m.py", QualifiedName: "app.models.Order"},
	}
	custom := []types.EntityRecord{
		{Name: "Contract", Kind: "SCOPE.Schema", SourceFile: "m.py", Subtype: "model"},                             // empty QName -> inherit
		{Name: "Order", Kind: "SCOPE.Schema", SourceFile: "m.py", Subtype: "model", QualifiedName: "custom.Order"}, // explicit QName -> keep
	}
	got := MergeWithCustom(base, custom)

	if len(got) != 4 {
		t.Fatalf("expected 4 entities (2 base + 2 custom, nothing destroyed), got %d: %v", len(got), kindNames(got))
	}
	byKey := map[string]types.EntityRecord{}
	for _, e := range got {
		byKey[e.Kind+"|"+e.Name] = e
	}
	if e := byKey["SCOPE.Schema|Contract"]; e.QualifiedName != "app.models.Contract" {
		t.Errorf("custom Contract should inherit base QualifiedName, got %q", e.QualifiedName)
	}
	if e := byKey["SCOPE.Schema|Order"]; e.QualifiedName != "custom.Order" {
		t.Errorf("custom Order QualifiedName must not be overridden, got %q", e.QualifiedName)
	}
	if e := byKey["SCOPE.Component|Contract"]; e.QualifiedName != "app.models.Contract" {
		t.Errorf("base Contract was destroyed or mutated, got %+v", e)
	}
	if e := byKey["SCOPE.Component|Order"]; e.QualifiedName != "app.models.Order" {
		t.Errorf("base Order was destroyed or mutated, got %+v", e)
	}
}

// TestMergeWithCustomUnionsBaseEdges proves base structural edges survive the
// merge (issue #4402): CONTAINS membership embedded on the base node (empty
// FromID = implicitly owned) is unioned onto the survivor, and an explicit base
// self-edge (FromID == base ID) is re-keyed to the survivor's ID. Duplicate
// edges already on the custom node are not double-added.
func TestMergeWithCustomUnionsBaseEdges(t *testing.T) {
	baseNode := types.EntityRecord{Name: "Contract", Kind: "SCOPE.Component", SourceFile: "m.py"}
	baseID := baseNode.ComputeID()
	baseNode.ID = baseID
	baseNode.Relationships = []types.RelationshipRecord{
		// Implicitly-owned membership edge (empty FromID).
		{ToID: "Contract.status", Kind: "CONTAINS"},
		// Explicit self-edge keyed to the base node ID — must be re-keyed.
		{FromID: baseID, ToID: "Contract.amount", Kind: "CONTAINS"},
	}
	base := []types.EntityRecord{baseNode}

	customNode := types.EntityRecord{Name: "Contract", Kind: "SCOPE.Schema", Subtype: "model", SourceFile: "m.py"}
	// A duplicate of the implicit membership edge — must not be double-added.
	customNode.Relationships = []types.RelationshipRecord{
		{ToID: "Contract.status", Kind: "CONTAINS"},
	}
	custom := []types.EntityRecord{customNode}

	got := MergeWithCustom(base, custom)
	// #6104: the base node is NO LONGER DESTROYED — a Kind disagreement means
	// two graph nodes. The edge-carry property this test exists for now
	// applies to the enriched custom node, and the base node keeps its own.
	if len(got) != 2 {
		t.Fatalf("expected 2 merged entities (base kept), got %d: %v", len(got), kindNames(got))
	}
	var surv types.EntityRecord
	for _, e := range got {
		if e.Kind == "SCOPE.Schema" {
			surv = e
		}
		if e.Kind == "SCOPE.Component" && len(e.Relationships) != 2 {
			t.Errorf("base node lost its own edges: %v", e.Relationships)
		}
	}
	if surv.Kind == "" {
		t.Fatalf("custom node missing from merge: %v", kindNames(got))
	}
	survID := surv.ID
	if survID == "" {
		survID = surv.ComputeID()
	}

	var status, amount int
	for _, r := range surv.Relationships {
		if r.Kind != "CONTAINS" {
			continue
		}
		switch r.ToID {
		case "Contract.status":
			status++
		case "Contract.amount":
			amount++
			if r.FromID != survID {
				t.Errorf("explicit base self-edge not re-keyed to survivor: FromID=%q want %q", r.FromID, survID)
			}
		}
	}
	if status != 1 {
		t.Errorf("Contract.status CONTAINS should appear exactly once (deduped), got %d", status)
	}
	if amount != 1 {
		t.Errorf("Contract.amount CONTAINS (base self-edge) should survive the merge, got %d", amount)
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
