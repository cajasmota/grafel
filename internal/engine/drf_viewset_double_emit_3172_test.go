package engine

// drf_viewset_double_emit_3172_test.go — regression test for issue #3172.
//
// Root cause: every Python framework's YAML rules apply to every Python file
// (they all share the "python" language key).  Falcon's catch-all
// `class\s+(\w+)...` source_pattern fires on Django/DRF files and emits a
// `Controller` entity for every class definition, including DRF ViewSet
// classes that the Django source_pattern already emits as `View`.  The
// result was two framework-typed nodes — View + Controller — for the same
// (Name, SourceFile), where the Controller carried zero edges.
//
// Fix: deduplicateViewControllerForPython drops `Controller` entities whose
// (Name, SourceFile) is already covered by a `View` entity.

import (
	"context"
	"strings"
	"testing"

	"github.com/cajasmota/archigraph/internal/extractor"
)

// sampleDRFViewsetFile is a minimal DRF ViewSet file that reproduces the
// double-emit: AocViewSet inherits from viewsets.ModelViewSet and therefore
// matches both the Django source_pattern (emits View) and the Falcon catch-all
// class pattern (emits Controller).
const sampleDRFViewsetFile = `from rest_framework import viewsets

class AocViewSet(viewsets.ModelViewSet):
    """DRF ViewSet for the Aoc model."""
    queryset = Aoc.objects.all()
    serializer_class = AocSerializer
    permission_classes = [IsAuthenticated]

class AocReadOnlyViewSet(viewsets.ReadOnlyModelViewSet):
    queryset = Aoc.objects.filter(active=True)
    serializer_class = AocSerializer
`

// TestDRFViewSet_NoDoubleEmit verifies that a DRF ViewSet class emits exactly
// ONE entity node — the View — and NOT a duplicate Controller.  Regression for
// issue #3172 ("~72 phantom Controller nodes in Upvate bench, AocViewSet being
// the canonical example").
func TestDRFViewSet_NoDoubleEmit(t *testing.T) {
	rules, err := LoadAllRules()
	if err != nil {
		t.Fatalf("LoadAllRules failed: %v", err)
	}

	det := New(rules)
	result, err := det.Detect(context.Background(), extractor.FileInput{
		Path:     "core/views/aoc_viewset.py",
		Content:  []byte(sampleDRFViewsetFile),
		Language: "python",
	})
	if err != nil {
		t.Fatalf("Detect failed: %v", err)
	}

	// Gather View and Controller entities by class name.
	type kindCount struct{ view, controller int }
	byName := make(map[string]*kindCount)
	for _, e := range result.Entities {
		if e.Kind != "View" && e.Kind != "Controller" {
			continue
		}
		if _, ok := byName[e.Name]; !ok {
			byName[e.Name] = &kindCount{}
		}
		switch e.Kind {
		case "View":
			byName[e.Name].view++
		case "Controller":
			byName[e.Name].controller++
		}
	}

	// Both ViewSet classes must be emitted as View, never as Controller.
	for _, className := range []string{"AocViewSet", "AocReadOnlyViewSet"} {
		counts, ok := byName[className]
		if !ok {
			t.Errorf("%s: expected a View entity, found none", className)
			continue
		}
		if counts.view != 1 {
			t.Errorf("%s: expected exactly 1 View entity, got %d", className, counts.view)
		}
		if counts.controller != 0 {
			t.Errorf("%s: expected 0 Controller entities (phantom), got %d — double-emit regression #3172",
				className, counts.controller)
		}
	}
}

// TestDRFViewSet_ControllerNotDroppedForNonViewClasses verifies that the dedup
// only drops a Controller when a View exists for the SAME name.  A class that
// is ONLY a Controller (e.g. an actual Falcon resource) must still be emitted.
func TestDRFViewSet_ControllerNotDroppedForNonViewClasses(t *testing.T) {
	// Minimal synthetic rule set: one rule emits Controller for every class,
	// no rule emits View at all.  deduplicateViewControllerForPython must keep
	// the Controller because there is no competing View.
	const syntheticFalconYAML = `
source_patterns:
  - pattern: "class\\s+(\\w+)"
    entity_type: Controller
    name_group: 1
    scope: class
`
	const syntheticCode = `class FalconResource:
    def on_get(self, req, resp):
        resp.media = {}
`

	fsys := buildTestFS("python", "falcon_only", syntheticFalconYAML)
	synRules, err := LoadAllRulesFromFS(fsys, "rules")
	if err != nil {
		t.Fatalf("LoadAllRulesFromFS failed: %v", err)
	}

	det := New(synRules)
	result, err := det.Detect(context.Background(), extractor.FileInput{
		Path:     "resources/falcon_resource.py",
		Content:  []byte(syntheticCode),
		Language: "python",
	})
	if err != nil {
		t.Fatalf("Detect failed: %v", err)
	}

	var foundController bool
	for _, e := range result.Entities {
		if e.Kind == "Controller" && strings.HasPrefix(e.Name, "FalconResource") {
			foundController = true
		}
	}
	if !foundController {
		t.Error("expected FalconResource to be emitted as Controller when no competing View exists, got none")
	}
}
