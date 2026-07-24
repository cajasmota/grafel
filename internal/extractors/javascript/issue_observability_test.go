// issue_observability_test.go — value-asserting tests for the non-OTel
// observability INSTRUMENTS breadth (epic #3628, area #11): JS/TS Sentry
// startSpan/startTransaction, dd-trace tracer.trace/wrap, and prom-client
// metric mutations on declared metric vars.
package javascript_test

import (
	"testing"

	"github.com/cajasmota/grafel/internal/types"
)

// jsObsEdge returns the first INSTRUMENTS edge on the named entity matching
// wantToID, or nil.
func jsObsEdge(ents []types.EntityRecord, entName, wantToID string) *types.RelationshipRecord {
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

// jsObsEdges returns all INSTRUMENTS edges on the named entity.
func jsObsEdges(ents []types.EntityRecord, entName string) []types.RelationshipRecord {
	e := findByNameRel(ents, entName)
	if e == nil {
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

func TestObservability_JSSentryStartSpan(t *testing.T) {
	src := `
function process() {
  Sentry.startSpan({ name: 'checkout' }, () => {});
}
`
	tree := parseTSRel(t, []byte(src))
	ents := runJS(t, src, "typescript", tree)
	r := jsObsEdge(ents, "process", "span:checkout")
	if r == nil {
		t.Fatalf("expected INSTRUMENTS(process -> span:checkout), got %#v", jsObsEdges(ents, "process"))
	}
	if r.Properties.Get("library") != "sentry" {
		t.Errorf("library=%q, want sentry", r.Properties.Get("library"))
	}
	if r.Properties.Get("api") != "Sentry.startSpan" {
		t.Errorf("api=%q, want Sentry.startSpan", r.Properties.Get("api"))
	}
	if r.Properties.Get("span_name") != "checkout" {
		t.Errorf("span_name=%q, want checkout", r.Properties.Get("span_name"))
	}
	if r.Properties.Get("traced") != "true" {
		t.Errorf("traced=%q, want true", r.Properties.Get("traced"))
	}
}

func TestObservability_JSSentryStartTransaction(t *testing.T) {
	src := `
function pay() {
  const tx = Sentry.startTransaction({ name: 'payment' });
}
`
	tree := parseTSRel(t, []byte(src))
	ents := runJS(t, src, "typescript", tree)
	r := jsObsEdge(ents, "pay", "span:payment")
	if r == nil {
		t.Fatalf("expected INSTRUMENTS(pay -> span:payment), got %#v", jsObsEdges(ents, "pay"))
	}
	if r.Properties.Get("api") != "Sentry.startTransaction" {
		t.Errorf("api=%q, want Sentry.startTransaction", r.Properties.Get("api"))
	}
	if r.Properties.Get("span_name") != "payment" {
		t.Errorf("span_name=%q, want payment", r.Properties.Get("span_name"))
	}
}

func TestObservability_JSDatadogTrace(t *testing.T) {
	src := `
function handle() {
  tracer.trace('web.request', () => {});
}
`
	tree := parseTSRel(t, []byte(src))
	ents := runJS(t, src, "typescript", tree)
	r := jsObsEdge(ents, "handle", "span:web.request")
	if r == nil {
		t.Fatalf("expected INSTRUMENTS(handle -> span:web.request), got %#v", jsObsEdges(ents, "handle"))
	}
	if r.Properties.Get("library") != "dd-trace" {
		t.Errorf("library=%q, want dd-trace", r.Properties.Get("library"))
	}
	if r.Properties.Get("api") != "tracer.trace" {
		t.Errorf("api=%q, want tracer.trace", r.Properties.Get("api"))
	}
	if r.Properties.Get("span_name") != "web.request" {
		t.Errorf("span_name=%q, want web.request", r.Properties.Get("span_name"))
	}
}

func TestObservability_JSPromClientCounterInc(t *testing.T) {
	src := `
const orders = new client.Counter({ name: 'orders' });
function serve() {
  orders.inc();
}
`
	tree := parseTSRel(t, []byte(src))
	ents := runJS(t, src, "typescript", tree)
	r := jsObsEdge(ents, "serve", "metric:orders")
	if r == nil {
		t.Fatalf("expected INSTRUMENTS(serve -> metric:orders), got %#v", jsObsEdges(ents, "serve"))
	}
	if r.Properties.Get("library") != "prom-client" {
		t.Errorf("library=%q, want prom-client", r.Properties.Get("library"))
	}
	if r.Properties.Get("api") != "metric.inc" {
		t.Errorf("api=%q, want metric.inc", r.Properties.Get("api"))
	}
	if r.Properties.Get("metric_name") != "orders" {
		t.Errorf("metric_name=%q, want orders", r.Properties.Get("metric_name"))
	}
}

func TestObservability_JSPromClientHistogramStartTimer(t *testing.T) {
	src := `
const lat = new Histogram({ name: 'http_latency' });
function record() {
  const end = lat.startTimer();
}
`
	tree := parseTSRel(t, []byte(src))
	ents := runJS(t, src, "typescript", tree)
	r := jsObsEdge(ents, "record", "metric:http_latency")
	if r == nil {
		t.Fatalf("expected INSTRUMENTS(record -> metric:http_latency), got %#v", jsObsEdges(ents, "record"))
	}
	if r.Properties.Get("api") != "metric.startTimer" {
		t.Errorf("api=%q, want metric.startTimer", r.Properties.Get("api"))
	}
	if r.Properties.Get("metric_name") != "http_latency" {
		t.Errorf("metric_name=%q, want http_latency", r.Properties.Get("metric_name"))
	}
}

func TestObservability_JSDynamicSpanNameNoFabrication(t *testing.T) {
	// Dynamic span name (variable in `name`) → dynamic stub on the enclosing fn.
	src := `
function handle(op) {
  Sentry.startSpan({ name: op }, () => {});
}
`
	tree := parseTSRel(t, []byte(src))
	ents := runJS(t, src, "typescript", tree)
	r := jsObsEdge(ents, "handle", "span:handle")
	if r == nil {
		t.Fatalf("expected dynamic INSTRUMENTS(handle -> span:handle), got %#v", jsObsEdges(ents, "handle"))
	}
	if r.Properties.Get("dynamic") != "true" {
		t.Errorf("dynamic=%q, want true", r.Properties.Get("dynamic"))
	}
	if r.Properties.Get("span_name") != "" {
		t.Errorf("dynamic span must NOT carry a fabricated span_name, got %q", r.Properties.Get("span_name"))
	}
	if jsObsEdge(ents, "handle", "span:op") != nil {
		t.Errorf("must not fabricate span:op from a variable name")
	}
}

func TestObservability_JSUnknownVarIncNoEdge(t *testing.T) {
	// .inc() on a variable that is NOT a known prom-client metric → no edge.
	src := `
function serve() {
  counter.inc();
}
`
	tree := parseTSRel(t, []byte(src))
	ents := runJS(t, src, "typescript", tree)
	if edges := jsObsEdges(ents, "serve"); len(edges) != 0 {
		t.Fatalf("expected NO INSTRUMENTS edges for .inc() on unknown var, got %#v", edges)
	}
}

func TestObservability_JSNonTracerTraceNoEdge(t *testing.T) {
	// `.trace` on a receiver that is not `tracer` → no edge (precision).
	src := `
function handle() {
  logger.trace('not a span');
}
`
	tree := parseTSRel(t, []byte(src))
	ents := runJS(t, src, "typescript", tree)
	if jsObsEdge(ents, "handle", "span:not a span") != nil {
		t.Fatalf("must not emit a span for logger.trace(...): %#v", jsObsEdges(ents, "handle"))
	}
}
