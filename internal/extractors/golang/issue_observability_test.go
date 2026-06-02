// issue_observability_test.go — value-asserting tests for the non-OTel
// observability INSTRUMENTS breadth (epic #3628, area #11): Go ddtrace
// tracer.StartSpan / StartSpanFromContext, Sentry sentry.StartSpan, and
// Prometheus metric mutations on declared metric vars.
package golang_test

import (
	"testing"

	"github.com/cajasmota/archigraph/internal/types"
)

// obsEdges returns every INSTRUMENTS edge on the operation entity whose bare
// name == fnName.
func obsEdges(recs []interface{}, fnName string) []types.RelationshipRecord {
	e, ok := findGoEnt(recs, fnName)
	if !ok {
		return nil
	}
	var out []types.RelationshipRecord
	for _, r := range e.Relationships {
		if r.Kind == "INSTRUMENTS" {
			out = append(out, r)
		}
	}
	return out
}

func findObsEdge(edges []types.RelationshipRecord, toID string) (types.RelationshipRecord, bool) {
	for _, r := range edges {
		if r.ToID == toID {
			return r, true
		}
	}
	return types.RelationshipRecord{}, false
}

func TestObservability_GoDdtraceStartSpan(t *testing.T) {
	src := `package p

func handle() {
	span := tracer.StartSpan("web.request")
	_ = span
}
`
	recs, err := extractFrom(src)
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	edges := obsEdges(recs, "handle")
	e, ok := findObsEdge(edges, "span:web.request")
	if !ok {
		t.Fatalf("expected INSTRUMENTS(handle -> span:web.request), got %#v", edges)
	}
	if e.Properties["library"] != "ddtrace" {
		t.Errorf("library = %q, want ddtrace", e.Properties["library"])
	}
	if e.Properties["api"] != "tracer.StartSpan" {
		t.Errorf("api = %q, want tracer.StartSpan", e.Properties["api"])
	}
	if e.Properties["span_name"] != "web.request" {
		t.Errorf("span_name = %q, want web.request", e.Properties["span_name"])
	}
	if e.Properties["traced"] != "true" {
		t.Errorf("traced = %q, want true", e.Properties["traced"])
	}
	if e.Properties["dynamic"] == "true" {
		t.Errorf("static span must not be dynamic")
	}
}

func TestObservability_GoDdtraceStartSpanFromContext(t *testing.T) {
	src := `package p

func handle(ctx context.Context) {
	span, _ := tracer.StartSpanFromContext(ctx, "db.query")
	_ = span
}
`
	recs, err := extractFrom(src)
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	edges := obsEdges(recs, "handle")
	e, ok := findObsEdge(edges, "span:db.query")
	if !ok {
		t.Fatalf("expected INSTRUMENTS(handle -> span:db.query), got %#v", edges)
	}
	if e.Properties["api"] != "tracer.StartSpanFromContext" {
		t.Errorf("api = %q, want tracer.StartSpanFromContext", e.Properties["api"])
	}
	if e.Properties["span_name"] != "db.query" {
		t.Errorf("span_name = %q, want db.query", e.Properties["span_name"])
	}
}

func TestObservability_GoSentryStartSpan(t *testing.T) {
	src := `package p

func checkout(ctx context.Context) {
	span := sentry.StartSpan(ctx, "checkout.op")
	_ = span
}
`
	recs, err := extractFrom(src)
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	edges := obsEdges(recs, "checkout")
	e, ok := findObsEdge(edges, "span:checkout.op")
	if !ok {
		t.Fatalf("expected INSTRUMENTS(checkout -> span:checkout.op), got %#v", edges)
	}
	if e.Properties["library"] != "sentry" {
		t.Errorf("library = %q, want sentry", e.Properties["library"])
	}
	if e.Properties["api"] != "sentry.StartSpan" {
		t.Errorf("api = %q, want sentry.StartSpan", e.Properties["api"])
	}
	if e.Properties["span_name"] != "checkout.op" {
		t.Errorf("span_name = %q, want checkout.op", e.Properties["span_name"])
	}
}

func TestObservability_GoPrometheusCounterInc(t *testing.T) {
	src := `package p

func serve() {
	c := prometheus.NewCounter(prometheus.CounterOpts{Name: "reqs"})
	c.Inc()
}
`
	recs, err := extractFrom(src)
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	edges := obsEdges(recs, "serve")
	e, ok := findObsEdge(edges, "metric:reqs")
	if !ok {
		t.Fatalf("expected INSTRUMENTS(serve -> metric:reqs), got %#v", edges)
	}
	if e.Properties["library"] != "prometheus" {
		t.Errorf("library = %q, want prometheus", e.Properties["library"])
	}
	if e.Properties["api"] != "metric.Inc" {
		t.Errorf("api = %q, want metric.Inc", e.Properties["api"])
	}
	if e.Properties["metric_name"] != "reqs" {
		t.Errorf("metric_name = %q, want reqs", e.Properties["metric_name"])
	}
}

func TestObservability_GoPrometheusFileLevelObserve(t *testing.T) {
	// Metric declared at package scope, observed inside a handler.
	src := `package p

var latency = prometheus.NewHistogram(prometheus.HistogramOpts{Name: "latency_seconds"})

func record(d float64) {
	latency.Observe(d)
}
`
	recs, err := extractFrom(src)
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	edges := obsEdges(recs, "record")
	e, ok := findObsEdge(edges, "metric:latency_seconds")
	if !ok {
		t.Fatalf("expected INSTRUMENTS(record -> metric:latency_seconds), got %#v", edges)
	}
	if e.Properties["api"] != "metric.Observe" {
		t.Errorf("api = %q, want metric.Observe", e.Properties["api"])
	}
	if e.Properties["metric_name"] != "latency_seconds" {
		t.Errorf("metric_name = %q, want latency_seconds", e.Properties["metric_name"])
	}
}

func TestObservability_GoDynamicSpanName(t *testing.T) {
	// Non-literal span name → dynamic stub keyed on the enclosing fn, no fabrication.
	src := `package p

func handle(op string) {
	span := tracer.StartSpan(op)
	_ = span
}
`
	recs, err := extractFrom(src)
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	edges := obsEdges(recs, "handle")
	e, ok := findObsEdge(edges, "span:handle")
	if !ok {
		t.Fatalf("expected dynamic INSTRUMENTS(handle -> span:handle), got %#v", edges)
	}
	if e.Properties["dynamic"] != "true" {
		t.Errorf("dynamic = %q, want true", e.Properties["dynamic"])
	}
	if e.Properties["span_name"] != "" {
		t.Errorf("dynamic span must NOT carry a fabricated span_name, got %q", e.Properties["span_name"])
	}
	if _, ok := findObsEdge(edges, "span:op"); ok {
		t.Errorf("must not fabricate span:op from a variable arg")
	}
}

func TestObservability_GoUnknownVarIncNoEdge(t *testing.T) {
	// .Inc() on a variable that is NOT a known metric → no edge (precision).
	src := `package p

func serve(notametric Widget) {
	notametric.Inc()
}
`
	recs, err := extractFrom(src)
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	edges := obsEdges(recs, "serve")
	if len(edges) != 0 {
		t.Fatalf("expected NO INSTRUMENTS edges for .Inc() on unknown var, got %#v", edges)
	}
}

func TestObservability_GoNonTracerStartSpanNoEdge(t *testing.T) {
	// .StartSpan on a receiver that is not `tracer`/`sentry` → no edge.
	src := `package p

func handle() {
	span := other.StartSpan("nope")
	_ = span
}
`
	recs, err := extractFrom(src)
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	edges := obsEdges(recs, "handle")
	if _, ok := findObsEdge(edges, "span:nope"); ok {
		t.Fatalf("must not emit span for StartSpan on a non-tracer/non-sentry receiver: %#v", edges)
	}
}
