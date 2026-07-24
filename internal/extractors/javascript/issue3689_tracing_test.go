// Package javascript_test — issue #3689 (epic #3628, area #11): verifies the
// OpenTelemetry tracing-span pass emits INSTRUMENTS edges from the enclosing
// JS/TS function/method → a synthetic span stub carrying the span name.
package javascript_test

import (
	"testing"

	"github.com/cajasmota/grafel/internal/types"
)

// jsSpanEdge returns the first INSTRUMENTS edge on the named entity matching
// wantToID, or nil.
func jsSpanEdge(ents []types.EntityRecord, entName, wantToID string) *types.RelationshipRecord {
	e := findByNameRel(ents, entName)
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

// TestTracing_JS_StartSpan_InstrumentsEnclosingFn verifies that
// `tracer.startSpan('checkout')` inside a function produces a span "checkout"
// instrumenting that function.
func TestTracing_JS_StartSpan_InstrumentsEnclosingFn(t *testing.T) {
	src := `
function process() {
  const span = tracer.startSpan('checkout');
  span.end();
}
`
	tree := parseTSRel(t, []byte(src))
	ents := runJS(t, src, "typescript", tree)
	r := jsSpanEdge(ents, "process", "span:checkout")
	if r == nil {
		if e := findByNameRel(ents, "process"); e != nil {
			for _, rel := range e.Relationships {
				t.Logf("  %s → %s (props=%v)", rel.Kind, rel.ToID, rel.Properties)
			}
		}
		t.Fatal("INSTRUMENTS edge process → span:checkout not found")
	}
	if r.Properties.Get("span_name") != "checkout" {
		t.Errorf("span_name=%q, want checkout", r.Properties.Get("span_name"))
	}
	if r.Properties.Get("library") != "opentelemetry" {
		t.Errorf("library=%q, want opentelemetry", r.Properties.Get("library"))
	}
	if r.Properties.Get("api") != "startSpan" {
		t.Errorf("api=%q, want startSpan", r.Properties.Get("api"))
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

// TestTracing_JS_StartActiveSpan verifies the startActiveSpan form.
func TestTracing_JS_StartActiveSpan(t *testing.T) {
	src := `
function query() {
  tracer.startActiveSpan('db.query', (span) => {
    span.end();
  });
}
`
	tree := parseTSRel(t, []byte(src))
	ents := runJS(t, src, "typescript", tree)
	r := jsSpanEdge(ents, "query", "span:db.query")
	if r == nil {
		t.Fatal("INSTRUMENTS edge query → span:db.query not found")
	}
	if r.Properties.Get("span_name") != "db.query" {
		t.Errorf("span_name=%q, want db.query", r.Properties.Get("span_name"))
	}
	if r.Properties.Get("api") != "startActiveSpan" {
		t.Errorf("api=%q, want startActiveSpan", r.Properties.Get("api"))
	}
}

// TestTracing_JS_DynamicName_NoFabrication is the honest-partial negative: a
// variable span name emits traced+dynamic with NO fabricated span_name, keyed
// on the enclosing fn ("span:<fn>").
func TestTracing_JS_DynamicName_NoFabrication(t *testing.T) {
	src := `
function run(opName) {
  const span = tracer.startSpan(opName);
  span.end();
}
`
	tree := parseTSRel(t, []byte(src))
	ents := runJS(t, src, "typescript", tree)
	r := jsSpanEdge(ents, "run", "span:run")
	if r == nil {
		if e := findByNameRel(ents, "run"); e != nil {
			for _, rel := range e.Relationships {
				t.Logf("  %s → %s (props=%v)", rel.Kind, rel.ToID, rel.Properties)
			}
		}
		t.Fatal("INSTRUMENTS edge run → span:run not found for dynamic span name")
	}
	if r.Properties.Get("dynamic") != "true" {
		t.Errorf("dynamic=%q, want true", r.Properties.Get("dynamic"))
	}
	if _, ok := r.Properties.Lookup("span_name"); ok {
		t.Errorf("span_name must be absent for dynamic name; got %q", r.Properties.Get("span_name"))
	}
}

// TestTracing_JS_NoSpan_NoEdge verifies no false positives on a plain function.
func TestTracing_JS_NoSpan_NoEdge(t *testing.T) {
	src := `
function plain(x) {
  return x + 1;
}
`
	tree := parseTSRel(t, []byte(src))
	ents := runJS(t, src, "typescript", tree)
	e := findByNameRel(ents, "plain")
	if e == nil {
		t.Fatal("entity plain not found")
	}
	for _, r := range e.Relationships {
		if r.Kind == "INSTRUMENTS" {
			t.Errorf("unexpected INSTRUMENTS edge on plain fn: → %s", r.ToID)
		}
	}
}
