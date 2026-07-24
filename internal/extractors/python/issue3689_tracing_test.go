// Package python_test — issue #3689 (epic #3628, area #11): verifies that the
// OpenTelemetry tracing-span pass emits INSTRUMENTS edges from the enclosing
// function/method to a synthetic span stub, carrying the span name + the
// operation it instruments.
package python_test

import (
	"testing"

	"github.com/cajasmota/grafel/internal/types"
)

// spanEdge returns the first INSTRUMENTS edge on the named entity whose ToID
// matches wantToID, or nil.
func spanEdge(ents []types.EntityRecord, entName, wantToID string) *types.RelationshipRecord {
	e := findEntPy(ents, entName)
	if e == nil {
		return nil
	}
	for i := range e.Relationships {
		r := &e.Relationships[i]
		if r.Kind == "INSTRUMENTS" && r.ToID == wantToID {
			return r
		}
	}
	return nil
}

// TestTracing_Python_ContextManager_InstrumentsEnclosingFn verifies that
// `with tracer.start_as_current_span("checkout"):` inside `def process()`
// produces a span "checkout" instrumenting process.
func TestTracing_Python_ContextManager_InstrumentsEnclosingFn(t *testing.T) {
	src := `
def process(order):
    with tracer.start_as_current_span("checkout"):
        do_work(order)
`
	ents := extractPy(t, src, "shop/service.py")
	r := spanEdge(ents, "process", "span:checkout")
	if r == nil {
		e := findEntPy(ents, "process")
		if e != nil {
			for _, rel := range e.Relationships {
				t.Logf("  %s → %s (props=%v)", rel.Kind, rel.ToID, rel.Properties)
			}
		}
		t.Fatal("INSTRUMENTS edge process → span:checkout not found")
	}
	if r.Properties.Get("span_name") != "checkout" {
		t.Errorf("span_name=%q, want %q", r.Properties.Get("span_name"), "checkout")
	}
	if r.Properties.Get("library") != "opentelemetry" {
		t.Errorf("library=%q, want opentelemetry", r.Properties.Get("library"))
	}
	if r.Properties.Get("api") != "start_as_current_span" {
		t.Errorf("api=%q, want start_as_current_span", r.Properties.Get("api"))
	}
	if r.Properties.Get("traced") != "true" {
		t.Errorf("traced=%q, want true", r.Properties.Get("traced"))
	}
	if r.Properties.Get("line") == "" {
		t.Error("line property is empty")
	}
	if r.Properties.Get("dynamic") != "" {
		t.Errorf("dynamic=%q, want empty for static span name", r.Properties.Get("dynamic"))
	}
}

// TestTracing_Python_ManualStartSpan verifies the manual `tracer.start_span`
// form binds the span to the enclosing method.
func TestTracing_Python_ManualStartSpan(t *testing.T) {
	src := `
class Repo:
    def fetch(self):
        span = self.tracer.start_span("db.query")
        return span
`
	ents := extractPy(t, src, "repo.py")
	r := spanEdge(ents, "Repo.fetch", "span:db.query")
	if r == nil {
		t.Fatal("INSTRUMENTS edge Repo.fetch → span:db.query not found")
	}
	if r.Properties.Get("span_name") != "db.query" {
		t.Errorf("span_name=%q, want db.query", r.Properties.Get("span_name"))
	}
	if r.Properties.Get("api") != "start_span" {
		t.Errorf("api=%q, want start_span", r.Properties.Get("api"))
	}
}

// TestTracing_Python_DecoratorForm verifies the decorator
// `@tracer.start_as_current_span("handle")` instruments the decorated function.
func TestTracing_Python_DecoratorForm(t *testing.T) {
	src := `
@tracer.start_as_current_span("handle")
def handle(req):
    return 1
`
	ents := extractPy(t, src, "h.py")
	r := spanEdge(ents, "handle", "span:handle")
	if r == nil {
		e := findEntPy(ents, "handle")
		if e != nil {
			for _, rel := range e.Relationships {
				t.Logf("  %s → %s (props=%v)", rel.Kind, rel.ToID, rel.Properties)
			}
		}
		t.Fatal("INSTRUMENTS edge handle → span:handle not found")
	}
	if r.Properties.Get("span_name") != "handle" {
		t.Errorf("span_name=%q, want handle", r.Properties.Get("span_name"))
	}
}

// TestTracing_Python_DynamicName_NoFabrication is the honest-partial negative:
// a variable span name emits traced+dynamic but NO fabricated span_name, and
// keys the stub on the enclosing function ("span:<fn>").
func TestTracing_Python_DynamicName_NoFabrication(t *testing.T) {
	src := `
def run(op_name):
    with tracer.start_as_current_span(op_name):
        work()
`
	ents := extractPy(t, src, "d.py")
	// Dynamic span keys on the enclosing fn name.
	r := spanEdge(ents, "run", "span:run")
	if r == nil {
		e := findEntPy(ents, "run")
		if e != nil {
			for _, rel := range e.Relationships {
				t.Logf("  %s → %s (props=%v)", rel.Kind, rel.ToID, rel.Properties)
			}
		}
		t.Fatal("INSTRUMENTS edge run → span:run not found for dynamic span name")
	}
	if r.Properties.Get("dynamic") != "true" {
		t.Errorf("dynamic=%q, want true", r.Properties.Get("dynamic"))
	}
	if r.Properties.Get("traced") != "true" {
		t.Errorf("traced=%q, want true", r.Properties.Get("traced"))
	}
	if _, ok := r.Properties.Lookup("span_name"); ok {
		t.Errorf("span_name must be absent for dynamic name; got %q", r.Properties.Get("span_name"))
	}
}

// TestTracing_Python_NoSpan_NoEdge verifies a plain function with no OTel call
// produces no INSTRUMENTS edge (no false positives).
func TestTracing_Python_NoSpan_NoEdge(t *testing.T) {
	src := `
def plain(x):
    return x + 1
`
	ents := extractPy(t, src, "p.py")
	e := findEntPy(ents, "plain")
	if e == nil {
		t.Fatal("entity plain not found")
	}
	for _, r := range e.Relationships {
		if r.Kind == "INSTRUMENTS" {
			t.Errorf("unexpected INSTRUMENTS edge on plain fn: → %s", r.ToID)
		}
	}
}
