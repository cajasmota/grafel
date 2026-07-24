// issue3689_tracing_test.go — epic #3628, area #11: verifies OpenTelemetry
// span-creation extraction emits INSTRUMENTS edges from the enclosing Go
// function/method → a synthetic span stub carrying the span name.
package golang_test

import (
	"testing"

	"github.com/cajasmota/grafel/internal/types"
)

// findGoEnt returns the EntityRecord with the given Name from a []interface{}
// of records, or a zero value + false.
func findGoEnt(recs []interface{}, name string) (types.EntityRecord, bool) {
	for _, r := range recs {
		e, ok := r.(types.EntityRecord)
		if ok && e.Name == name {
			return e, true
		}
	}
	return types.EntityRecord{}, false
}

// goSpanEdge returns the first INSTRUMENTS edge on the named entity matching
// wantToID, or nil.
func goSpanEdge(recs []interface{}, entName, wantToID string) *types.RelationshipRecord {
	e, ok := findGoEnt(recs, entName)
	if !ok {
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

// TestTracing_Go_TracerStart_InstrumentsEnclosingFn verifies that
// `ctx, span := tracer.Start(ctx, "db.query")` inside a function produces a
// span "db.query" instrumenting that function.
func TestTracing_Go_TracerStart_InstrumentsEnclosingFn(t *testing.T) {
	src := `package svc

func query(ctx context.Context) {
	ctx, span := tracer.Start(ctx, "db.query")
	defer span.End()
	_ = ctx
}
`
	recs, err := extractFrom(src)
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	r := goSpanEdge(recs, "query", "span:db.query")
	if r == nil {
		if e, ok := findGoEnt(recs, "query"); ok {
			for _, rel := range e.Relationships {
				t.Logf("  %s → %s (props=%v)", rel.Kind, rel.ToID, rel.Properties)
			}
		}
		t.Fatal("INSTRUMENTS edge query → span:db.query not found")
	}
	if r.Properties.Get("span_name") != "db.query" {
		t.Errorf("span_name=%q, want db.query", r.Properties.Get("span_name"))
	}
	if r.Properties.Get("library") != "opentelemetry" {
		t.Errorf("library=%q, want opentelemetry", r.Properties.Get("library"))
	}
	if r.Properties.Get("api") != "tracer.Start" {
		t.Errorf("api=%q, want tracer.Start", r.Properties.Get("api"))
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

// TestTracing_Go_Method verifies a method receiver gets the edge keyed on the
// dotted method name and the stub on the span name.
func TestTracing_Go_Method(t *testing.T) {
	src := `package svc

func (s *Server) Handle(ctx context.Context) {
	ctx, span := s.tracer.Start(ctx, "server.handle")
	_ = span
	_ = ctx
}
`
	recs, err := extractFrom(src)
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	r := goSpanEdge(recs, "Server.Handle", "span:server.handle")
	if r == nil {
		t.Fatal("INSTRUMENTS edge Server.Handle → span:server.handle not found")
	}
	if r.Properties.Get("span_name") != "server.handle" {
		t.Errorf("span_name=%q, want server.handle", r.Properties.Get("span_name"))
	}
}

// TestTracing_Go_DynamicName_NoFabrication is the honest-partial negative: a
// variable span name emits traced+dynamic with NO fabricated span_name, keyed
// on the enclosing fn ("span:<fn>").
func TestTracing_Go_DynamicName_NoFabrication(t *testing.T) {
	src := `package svc

func run(ctx context.Context, opName string) {
	ctx, span := tracer.Start(ctx, opName)
	_ = span
	_ = ctx
}
`
	recs, err := extractFrom(src)
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	r := goSpanEdge(recs, "run", "span:run")
	if r == nil {
		if e, ok := findGoEnt(recs, "run"); ok {
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

// TestTracing_Go_NoSpan_NoEdge verifies no false positives on a plain fn and on
// an unrelated single-result `.Start` call (e.g. server.Start()).
func TestTracing_Go_NoSpan_NoEdge(t *testing.T) {
	src := `package svc

func boot() {
	server.Start()
	x := compute()
	_ = x
}
`
	recs, err := extractFrom(src)
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	e, ok := findGoEnt(recs, "boot")
	if !ok {
		t.Fatal("entity boot not found")
	}
	for _, r := range e.Relationships {
		if r.Kind == "INSTRUMENTS" {
			t.Errorf("unexpected INSTRUMENTS edge on boot: → %s", r.ToID)
		}
	}
}
