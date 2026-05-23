package python

import (
	"context"
	"testing"

	"github.com/cajasmota/archigraph/internal/extractor"
)

// Issue #1964 — every Operation, Method, and Class entity emitted by the
// Python extractor MUST carry non-zero start_line AND end_line so the
// docgen source_window helper can build a complete excerpt. The W1R1
// reproducer was a DRF ViewSet action whose end_line=0 sentinel clipped
// the method body before its return statements.
func TestPython_EndLine_NonZero_ForOperationsMethodsAndClasses(t *testing.T) {
	src := `# fixture: client_fixture_a module
import logging

class FixtureViewSet:
    """A DRF-like ViewSet whose actions and class boundaries must be
    fully captured by the extractor."""

    def assign_contacts(self, request, pk=None):
        log = logging.getLogger(__name__)
        log.info("starting assignment for pk=%s", pk)
        if request.data is None:
            return {"status": "missing"}
        result = {"ok": True}
        return result


def module_level_helper(a, b):
    return a + b
`

	e := &Extractor{}
	out, err := e.Extract(context.Background(), extractor.FileInput{
		Path:     "client_fixture_a/views.py",
		Content:  []byte(src),
		Language: "python",
	})
	if err != nil {
		t.Fatalf("Extract returned error: %v", err)
	}

	checkedClass := false
	checkedMethod := false
	checkedFunction := false

	for _, ent := range out {
		switch ent.Name {
		case "FixtureViewSet":
			checkedClass = true
			if ent.StartLine <= 0 {
				t.Errorf("class %q: start_line=%d (want > 0)", ent.Name, ent.StartLine)
			}
			if ent.EndLine <= 0 {
				t.Errorf("class %q: end_line=%d (want > 0)", ent.Name, ent.EndLine)
			}
			if ent.EndLine < ent.StartLine {
				t.Errorf("class %q: end_line(%d) < start_line(%d)", ent.Name, ent.EndLine, ent.StartLine)
			}
		case "FixtureViewSet.assign_contacts":
			checkedMethod = true
			if ent.StartLine <= 0 {
				t.Errorf("method %q: start_line=%d (want > 0)", ent.Name, ent.StartLine)
			}
			if ent.EndLine <= 0 {
				t.Errorf("method %q: end_line=%d (want > 0)", ent.Name, ent.EndLine)
			}
			if ent.EndLine < ent.StartLine {
				t.Errorf("method %q: end_line(%d) < start_line(%d)", ent.Name, ent.EndLine, ent.StartLine)
			}
		case "module_level_helper":
			checkedFunction = true
			if ent.StartLine <= 0 || ent.EndLine <= 0 {
				t.Errorf("function %q: start=%d end=%d (both must be > 0)", ent.Name, ent.StartLine, ent.EndLine)
			}
		}
	}

	if !checkedClass {
		t.Fatalf("class entity FixtureViewSet not emitted; out has %d entities", len(out))
	}
	if !checkedMethod {
		t.Fatalf("method entity FixtureViewSet.assign_contacts not emitted; out has %d entities", len(out))
	}
	if !checkedFunction {
		t.Fatalf("function entity module_level_helper not emitted; out has %d entities", len(out))
	}
}
