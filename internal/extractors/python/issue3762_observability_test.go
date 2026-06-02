// Package python_test — issue #3762 (epic #3628, area #11): verifies the
// non-OpenTelemetry observability pass emits INSTRUMENTS edges for Datadog
// ddtrace, Sentry, Prometheus, and New Relic, carrying the span/metric name and
// the operation it instruments. Mirrors the OTel contract in
// issue3689_tracing_test.go.
package python_test

import (
	"testing"

	"github.com/cajasmota/archigraph/internal/types"
)

// instrEdge returns the first INSTRUMENTS edge on entity entName whose ToID
// matches wantToID, or nil.
func instrEdge(ents []types.EntityRecord, entName, wantToID string) *types.RelationshipRecord {
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

// dumpInstr logs every INSTRUMENTS edge on entName (test-failure aid).
func dumpInstr(t *testing.T, ents []types.EntityRecord, entName string) {
	t.Helper()
	if e := findEntPy(ents, entName); e != nil {
		for _, r := range e.Relationships {
			if r.Kind == "INSTRUMENTS" {
				t.Logf("  INSTRUMENTS → %s (props=%v)", r.ToID, r.Properties)
			}
		}
	}
}

// --- Datadog ddtrace --------------------------------------------------------

// TestObs_Ddtrace_WrapDecorator_DefaultsToFnName verifies `@tracer.wrap()` on
// `checkout()` instruments span:checkout (ddtrace defaults the span name to the
// decorated function name).
func TestObs_Ddtrace_WrapDecorator_DefaultsToFnName(t *testing.T) {
	src := `
@tracer.wrap()
def checkout(cart):
    return pay(cart)
`
	ents := extractPy(t, src, "shop.py")
	r := instrEdge(ents, "checkout", "span:checkout")
	if r == nil {
		dumpInstr(t, ents, "checkout")
		t.Fatal("INSTRUMENTS edge checkout → span:checkout not found")
	}
	if r.Properties["library"] != "ddtrace" {
		t.Errorf("library=%q, want ddtrace", r.Properties["library"])
	}
	if r.Properties["api"] != "tracer.wrap" {
		t.Errorf("api=%q, want tracer.wrap", r.Properties["api"])
	}
	if r.Properties["span_name"] != "checkout" {
		t.Errorf("span_name=%q, want checkout", r.Properties["span_name"])
	}
	if r.Properties["traced"] != "true" {
		t.Errorf("traced=%q, want true", r.Properties["traced"])
	}
	if r.Properties["dynamic"] != "" {
		t.Errorf("dynamic=%q, want empty", r.Properties["dynamic"])
	}
}

// TestObs_Ddtrace_WrapDecorator_ExplicitName verifies `@tracer.wrap(name="pay")`
// uses the explicit name over the fn name.
func TestObs_Ddtrace_WrapDecorator_ExplicitName(t *testing.T) {
	src := `
@tracer.wrap(name="pay.op")
def checkout(cart):
    return 1
`
	ents := extractPy(t, src, "shop.py")
	r := instrEdge(ents, "checkout", "span:pay.op")
	if r == nil {
		dumpInstr(t, ents, "checkout")
		t.Fatal("INSTRUMENTS edge checkout → span:pay.op not found")
	}
	if r.Properties["span_name"] != "pay.op" {
		t.Errorf("span_name=%q, want pay.op", r.Properties["span_name"])
	}
}

// TestObs_Ddtrace_ManualTrace_Body verifies the manual `tracer.trace("db.query")`
// body call instruments span:db.query.
func TestObs_Ddtrace_ManualTrace_Body(t *testing.T) {
	src := `
def fetch():
    with tracer.trace("db.query"):
        return run()
`
	ents := extractPy(t, src, "repo.py")
	r := instrEdge(ents, "fetch", "span:db.query")
	if r == nil {
		dumpInstr(t, ents, "fetch")
		t.Fatal("INSTRUMENTS edge fetch → span:db.query not found")
	}
	if r.Properties["api"] != "tracer.trace" {
		t.Errorf("api=%q, want tracer.trace", r.Properties["api"])
	}
	if r.Properties["library"] != "ddtrace" {
		t.Errorf("library=%q, want ddtrace", r.Properties["library"])
	}
}

// --- Sentry -----------------------------------------------------------------

// TestObs_Sentry_TraceDecorator verifies `@sentry_sdk.trace` on `handler()`
// instruments span:handler.
func TestObs_Sentry_TraceDecorator(t *testing.T) {
	src := `
@sentry_sdk.trace
def handler(req):
    return 1
`
	ents := extractPy(t, src, "h.py")
	r := instrEdge(ents, "handler", "span:handler")
	if r == nil {
		dumpInstr(t, ents, "handler")
		t.Fatal("INSTRUMENTS edge handler → span:handler not found")
	}
	if r.Properties["library"] != "sentry" {
		t.Errorf("library=%q, want sentry", r.Properties["library"])
	}
	if r.Properties["api"] != "trace" {
		t.Errorf("api=%q, want trace", r.Properties["api"])
	}
}

// TestObs_Sentry_StartTransaction_Body verifies `sentry_sdk.start_transaction(
// name="checkout")` instruments span:checkout.
func TestObs_Sentry_StartTransaction_Body(t *testing.T) {
	src := `
def process():
    with sentry_sdk.start_transaction(name="checkout", op="task"):
        work()
`
	ents := extractPy(t, src, "p.py")
	r := instrEdge(ents, "process", "span:checkout")
	if r == nil {
		dumpInstr(t, ents, "process")
		t.Fatal("INSTRUMENTS edge process → span:checkout not found")
	}
	if r.Properties["api"] != "start_transaction" {
		t.Errorf("api=%q, want start_transaction", r.Properties["api"])
	}
	if r.Properties["span_name"] != "checkout" {
		t.Errorf("span_name=%q, want checkout", r.Properties["span_name"])
	}
}

// --- Prometheus -------------------------------------------------------------

// TestObs_Prometheus_TimeDecorator_CapturesMetricName verifies a module-level
// `REQUEST_TIME = Summary("request_seconds", ...)` plus `@REQUEST_TIME.time()`
// on `handler()` instruments metric:request_seconds.
func TestObs_Prometheus_TimeDecorator_CapturesMetricName(t *testing.T) {
	src := `
REQUEST_TIME = Summary("request_seconds", "Time spent")

@REQUEST_TIME.time()
def handler(req):
    return 1
`
	ents := extractPy(t, src, "m.py")
	r := instrEdge(ents, "handler", "metric:request_seconds")
	if r == nil {
		dumpInstr(t, ents, "handler")
		t.Fatal("INSTRUMENTS edge handler → metric:request_seconds not found")
	}
	if r.Properties["library"] != "prometheus" {
		t.Errorf("library=%q, want prometheus", r.Properties["library"])
	}
	if r.Properties["api"] != "metric.time" {
		t.Errorf("api=%q, want metric.time", r.Properties["api"])
	}
	if r.Properties["metric_name"] != "request_seconds" {
		t.Errorf("metric_name=%q, want request_seconds", r.Properties["metric_name"])
	}
	if r.Properties["traced"] != "true" {
		t.Errorf("traced=%q, want true", r.Properties["traced"])
	}
}

// TestObs_Prometheus_CounterInc_Body verifies a module-level Counter plus a
// `REQUESTS.inc()` body call instruments metric:http_requests_total.
func TestObs_Prometheus_CounterInc_Body(t *testing.T) {
	src := `
REQUESTS = Counter("http_requests_total", "Total")

def handle(req):
    REQUESTS.inc()
    return 1
`
	ents := extractPy(t, src, "c.py")
	r := instrEdge(ents, "handle", "metric:http_requests_total")
	if r == nil {
		dumpInstr(t, ents, "handle")
		t.Fatal("INSTRUMENTS edge handle → metric:http_requests_total not found")
	}
	if r.Properties["api"] != "metric.inc" {
		t.Errorf("api=%q, want metric.inc", r.Properties["api"])
	}
	if r.Properties["metric_name"] != "http_requests_total" {
		t.Errorf("metric_name=%q, want http_requests_total", r.Properties["metric_name"])
	}
}

// TestObs_Prometheus_UnknownVar_NoEdge is the precision negative: `.inc()` on a
// variable that is NOT a known module-level metric produces NO edge.
func TestObs_Prometheus_UnknownVar_NoEdge(t *testing.T) {
	src := `
def handle(counter):
    counter.inc()
    return 1
`
	ents := extractPy(t, src, "u.py")
	e := findEntPy(ents, "handle")
	if e == nil {
		t.Fatal("entity handle not found")
	}
	for _, r := range e.Relationships {
		if r.Kind == "INSTRUMENTS" {
			t.Errorf("unexpected INSTRUMENTS edge on non-metric .inc(): → %s", r.ToID)
		}
	}
}

// --- New Relic --------------------------------------------------------------

// TestObs_NewRelic_FunctionTrace_DefaultsToFnName verifies
// `@newrelic.agent.function_trace()` on `worker()` instruments span:worker.
func TestObs_NewRelic_FunctionTrace_DefaultsToFnName(t *testing.T) {
	src := `
@newrelic.agent.function_trace()
def worker(job):
    return run(job)
`
	ents := extractPy(t, src, "nr.py")
	r := instrEdge(ents, "worker", "span:worker")
	if r == nil {
		dumpInstr(t, ents, "worker")
		t.Fatal("INSTRUMENTS edge worker → span:worker not found")
	}
	if r.Properties["library"] != "newrelic" {
		t.Errorf("library=%q, want newrelic", r.Properties["library"])
	}
	if r.Properties["api"] != "function_trace" {
		t.Errorf("api=%q, want function_trace", r.Properties["api"])
	}
	if r.Properties["span_name"] != "worker" {
		t.Errorf("span_name=%q, want worker", r.Properties["span_name"])
	}
}

// TestObs_NewRelic_FunctionTrace_ExplicitName verifies `@function_trace(
// name="batch")` uses the explicit name.
func TestObs_NewRelic_FunctionTrace_ExplicitName(t *testing.T) {
	src := `
@function_trace(name="batch")
def worker(job):
    return 1
`
	ents := extractPy(t, src, "nr2.py")
	r := instrEdge(ents, "worker", "span:batch")
	if r == nil {
		dumpInstr(t, ents, "worker")
		t.Fatal("INSTRUMENTS edge worker → span:batch not found")
	}
	if r.Properties["span_name"] != "batch" {
		t.Errorf("span_name=%q, want batch", r.Properties["span_name"])
	}
}

// --- Honest-partial + negatives ---------------------------------------------

// TestObs_Sentry_DynamicName_NoFabrication verifies a dynamic transaction name
// emits traced+dynamic, NO fabricated span_name, keyed on the enclosing fn.
func TestObs_Sentry_DynamicName_NoFabrication(t *testing.T) {
	src := `
def run(op_name):
    with sentry_sdk.start_transaction(name=op_name):
        work()
`
	ents := extractPy(t, src, "d.py")
	r := instrEdge(ents, "run", "span:run")
	if r == nil {
		dumpInstr(t, ents, "run")
		t.Fatal("INSTRUMENTS edge run → span:run not found for dynamic name")
	}
	if r.Properties["dynamic"] != "true" {
		t.Errorf("dynamic=%q, want true", r.Properties["dynamic"])
	}
	if _, ok := r.Properties["span_name"]; ok {
		t.Errorf("span_name must be absent for dynamic name; got %q", r.Properties["span_name"])
	}
}

// TestObs_NonInstrumentDecorator_NoEdge verifies an unrelated decorator (e.g.
// `@app.route`) produces NO INSTRUMENTS edge (no false positives).
func TestObs_NonInstrumentDecorator_NoEdge(t *testing.T) {
	src := `
@app.route("/health")
def health():
    return "ok"
`
	ents := extractPy(t, src, "r.py")
	e := findEntPy(ents, "health")
	if e == nil {
		t.Fatal("entity health not found")
	}
	for _, r := range e.Relationships {
		if r.Kind == "INSTRUMENTS" {
			t.Errorf("unexpected INSTRUMENTS edge on @app.route fn: → %s", r.ToID)
		}
	}
}

// TestObs_Plain_NoEdge verifies a plain undecorated function produces no edge.
func TestObs_Plain_NoEdge(t *testing.T) {
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
