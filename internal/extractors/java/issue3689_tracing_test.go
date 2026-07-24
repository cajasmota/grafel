// Package java_test — issue #3689 (epic #3628, area #11): verifies the
// OpenTelemetry tracing-span pass emits INSTRUMENTS edges from the enclosing
// Java method → a synthetic span stub carrying the span name.
package java_test

import (
	"testing"

	"github.com/cajasmota/grafel/internal/types"
)

// javaSpanEdge returns the first INSTRUMENTS edge on the named method entity
// matching wantToID, or nil.
func javaSpanEdge(ents []types.EntityRecord, methodName, wantToID string) *types.RelationshipRecord {
	e := javaFind(ents, methodName, "SCOPE.Operation")
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

// TestTracing_Java_WithSpan_DefaultName verifies that a bare `@WithSpan` on
// `void handle()` produces a span named after the method instrumenting handle.
func TestTracing_Java_WithSpan_DefaultName(t *testing.T) {
	src := `package svc;
import io.opentelemetry.instrumentation.annotations.WithSpan;
class Service {
    @WithSpan
    void handle() {
        doWork();
    }
}
`
	ents := runJava(t, src)
	r := javaSpanEdge(ents, "Service.handle", "span:handle")
	if r == nil {
		if e := javaFind(ents, "Service.handle", "SCOPE.Operation"); e != nil {
			for _, rel := range e.Relationships {
				t.Logf("  %s → %s (props=%v)", rel.Kind, rel.ToID, rel.Properties)
			}
		}
		t.Fatal("INSTRUMENTS edge Service.handle → span:handle not found")
	}
	if r.Properties.Get("span_name") != "handle" {
		t.Errorf("span_name=%q, want handle", r.Properties.Get("span_name"))
	}
	if r.Properties.Get("api") != "WithSpan" {
		t.Errorf("api=%q, want WithSpan", r.Properties.Get("api"))
	}
	if r.Properties.Get("library") != "opentelemetry" {
		t.Errorf("library=%q, want opentelemetry", r.Properties.Get("library"))
	}
	if r.Properties.Get("traced") != "true" {
		t.Errorf("traced=%q, want true", r.Properties.Get("traced"))
	}
	if r.Properties.Get("dynamic") != "" {
		t.Errorf("dynamic=%q, want empty for @WithSpan default name", r.Properties.Get("dynamic"))
	}
}

// TestTracing_Java_WithSpan_AnnotationValue verifies @WithSpan("custom") uses
// the annotation value as the span name.
func TestTracing_Java_WithSpan_AnnotationValue(t *testing.T) {
	src := `package svc;
class Service {
    @WithSpan("custom-op")
    void run() { }
}
`
	ents := runJava(t, src)
	r := javaSpanEdge(ents, "Service.run", "span:custom-op")
	if r == nil {
		t.Fatal("INSTRUMENTS edge Service.run → span:custom-op not found")
	}
	if r.Properties.Get("span_name") != "custom-op" {
		t.Errorf("span_name=%q, want custom-op", r.Properties.Get("span_name"))
	}
}

// TestTracing_Java_SpanBuilder verifies the
// `tracer.spanBuilder("db.query").startSpan()` builder form.
func TestTracing_Java_SpanBuilder(t *testing.T) {
	src := `package svc;
class Repo {
    void query() {
        Span span = tracer.spanBuilder("db.query").startSpan();
        span.end();
    }
}
`
	ents := runJava(t, src)
	r := javaSpanEdge(ents, "Repo.query", "span:db.query")
	if r == nil {
		if e := javaFind(ents, "Repo.query", "SCOPE.Operation"); e != nil {
			for _, rel := range e.Relationships {
				t.Logf("  %s → %s (props=%v)", rel.Kind, rel.ToID, rel.Properties)
			}
		}
		t.Fatal("INSTRUMENTS edge Repo.query → span:db.query not found")
	}
	if r.Properties.Get("span_name") != "db.query" {
		t.Errorf("span_name=%q, want db.query", r.Properties.Get("span_name"))
	}
	if r.Properties.Get("api") != "spanBuilder" {
		t.Errorf("api=%q, want spanBuilder", r.Properties.Get("api"))
	}
}

// TestTracing_Java_SpanBuilder_DynamicName_NoFabrication is the honest-partial
// negative: a non-literal spanBuilder argument emits traced+dynamic with NO
// fabricated span_name, keyed on the enclosing method ("span:<method>").
func TestTracing_Java_SpanBuilder_DynamicName_NoFabrication(t *testing.T) {
	src := `package svc;
class Repo {
    void run(String opName) {
        Span span = tracer.spanBuilder(opName).startSpan();
        span.end();
    }
}
`
	ents := runJava(t, src)
	r := javaSpanEdge(ents, "Repo.run", "span:run")
	if r == nil {
		if e := javaFind(ents, "Repo.run", "SCOPE.Operation"); e != nil {
			for _, rel := range e.Relationships {
				t.Logf("  %s → %s (props=%v)", rel.Kind, rel.ToID, rel.Properties)
			}
		}
		t.Fatal("INSTRUMENTS edge Repo.run → span:run not found for dynamic name")
	}
	if r.Properties.Get("dynamic") != "true" {
		t.Errorf("dynamic=%q, want true", r.Properties.Get("dynamic"))
	}
	if _, ok := r.Properties.Lookup("span_name"); ok {
		t.Errorf("span_name must be absent for dynamic name; got %q", r.Properties.Get("span_name"))
	}
}

// TestTracing_Java_NoSpan_NoEdge verifies no false positives on a plain method.
func TestTracing_Java_NoSpan_NoEdge(t *testing.T) {
	src := `package svc;
class Plain {
    int add(int x) { return x + 1; }
}
`
	ents := runJava(t, src)
	e := javaFind(ents, "Plain.add", "SCOPE.Operation")
	if e == nil {
		t.Fatal("entity Plain.add not found")
	}
	for _, r := range e.Relationships {
		if r.Kind == "INSTRUMENTS" {
			t.Errorf("unexpected INSTRUMENTS edge on Plain.add: → %s", r.ToID)
		}
	}
}
