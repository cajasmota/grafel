// Package java_test — issue #3856 (epic #3854, area #11): verifies the Java
// non-OTel-tracing observability pass emits INSTRUMENTS edges from the enclosing
// Java method → synthetic metric:/span:/log: stubs for Micrometer & Dropwizard
// metrics, Spring Sleuth / Brave spans, and SLF4J fluent structured logging.
package java_test

import (
	"testing"

	"github.com/cajasmota/grafel/internal/types"
)

// javaObsEdge returns the first INSTRUMENTS edge on the named method matching
// wantToID, or nil.
func javaObsEdge(ents []types.EntityRecord, methodName, wantToID string) *types.RelationshipRecord {
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

func dumpEdges(t *testing.T, ents []types.EntityRecord, method string) {
	if e := javaFind(ents, method, "SCOPE.Operation"); e != nil {
		for _, rel := range e.Relationships {
			t.Logf("  %s → %s (props=%v)", rel.Kind, rel.ToID, rel.Properties)
		}
	}
}

// --- Micrometer metrics -----------------------------------------------------

// TestObs_Java_Micrometer_RegistryCounter verifies a Micrometer registry
// counter call stamps metric:orders.created with the resolved name.
func TestObs_Java_Micrometer_RegistryCounter(t *testing.T) {
	src := `package svc;
class OrderService {
    private final MeterRegistry meterRegistry;
    void create() {
        Counter c = meterRegistry.counter("orders.created");
        c.increment();
    }
}
`
	ents := runJava(t, src)
	r := javaObsEdge(ents, "OrderService.create", "metric:orders.created")
	if r == nil {
		dumpEdges(t, ents, "OrderService.create")
		t.Fatal("INSTRUMENTS edge OrderService.create → metric:orders.created not found")
	}
	if r.Properties.Get("metric_name") != "orders.created" {
		t.Errorf("metric_name=%q, want orders.created", r.Properties.Get("metric_name"))
	}
	if r.Properties.Get("library") != "micrometer" {
		t.Errorf("library=%q, want micrometer", r.Properties.Get("library"))
	}
	if r.Properties.Get("api") != "registry.counter" {
		t.Errorf("api=%q, want registry.counter", r.Properties.Get("api"))
	}
	if r.Properties.Get("traced") != "true" {
		t.Errorf("traced=%q, want true", r.Properties.Get("traced"))
	}
	if r.Properties.Get("dynamic") != "" {
		t.Errorf("dynamic=%q, want empty for literal name", r.Properties.Get("dynamic"))
	}
}

// TestObs_Java_Micrometer_Builder verifies the Counter.builder("name") form.
func TestObs_Java_Micrometer_Builder(t *testing.T) {
	src := `package svc;
class M {
    void wire(MeterRegistry reg) {
        Counter c = Counter.builder("payments.failed").register(reg);
    }
}
`
	ents := runJava(t, src)
	r := javaObsEdge(ents, "M.wire", "metric:payments.failed")
	if r == nil {
		dumpEdges(t, ents, "M.wire")
		t.Fatal("INSTRUMENTS edge M.wire → metric:payments.failed not found")
	}
	if r.Properties.Get("api") != "builder" {
		t.Errorf("api=%q, want builder", r.Properties.Get("api"))
	}
	if r.Properties.Get("metric_name") != "payments.failed" {
		t.Errorf("metric_name=%q, want payments.failed", r.Properties.Get("metric_name"))
	}
}

// TestObs_Java_Micrometer_Timed verifies the @Timed("api.latency") annotation.
func TestObs_Java_Micrometer_Timed(t *testing.T) {
	src := `package svc;
class Api {
    @Timed("api.latency")
    void handle() { }
}
`
	ents := runJava(t, src)
	r := javaObsEdge(ents, "Api.handle", "metric:api.latency")
	if r == nil {
		dumpEdges(t, ents, "Api.handle")
		t.Fatal("INSTRUMENTS edge Api.handle → metric:api.latency not found")
	}
	if r.Properties.Get("api") != "Timed" {
		t.Errorf("api=%q, want Timed", r.Properties.Get("api"))
	}
	if r.Properties.Get("metric_name") != "api.latency" {
		t.Errorf("metric_name=%q, want api.latency", r.Properties.Get("metric_name"))
	}
	if r.Properties.Get("library") != "micrometer" {
		t.Errorf("library=%q, want micrometer", r.Properties.Get("library"))
	}
}

// TestObs_Java_Micrometer_Timed_NoName_Dynamic verifies a bare @Timed (no name)
// is honest-partial: dynamic, keyed on the method, no fabricated metric_name.
func TestObs_Java_Micrometer_Timed_NoName_Dynamic(t *testing.T) {
	src := `package svc;
class Api {
    @Timed
    void run() { }
}
`
	ents := runJava(t, src)
	r := javaObsEdge(ents, "Api.run", "metric:run")
	if r == nil {
		dumpEdges(t, ents, "Api.run")
		t.Fatal("INSTRUMENTS edge Api.run → metric:run not found for bare @Timed")
	}
	if r.Properties.Get("dynamic") != "true" {
		t.Errorf("dynamic=%q, want true", r.Properties.Get("dynamic"))
	}
	if _, ok := r.Properties.Lookup("metric_name"); ok {
		t.Errorf("metric_name must be absent for bare @Timed; got %q", r.Properties.Get("metric_name"))
	}
}

// TestObs_Java_Micrometer_DynamicName_NoFabrication is the honest-partial
// negative: a non-literal registry metric name emits dynamic with NO fabricated
// metric_name.
func TestObs_Java_Micrometer_DynamicName_NoFabrication(t *testing.T) {
	src := `package svc;
class M {
    void rec(MeterRegistry meterRegistry, String name) {
        meterRegistry.counter(name).increment();
    }
}
`
	ents := runJava(t, src)
	r := javaObsEdge(ents, "M.rec", "metric:rec")
	if r == nil {
		dumpEdges(t, ents, "M.rec")
		t.Fatal("INSTRUMENTS edge M.rec → metric:rec not found for dynamic name")
	}
	if r.Properties.Get("dynamic") != "true" {
		t.Errorf("dynamic=%q, want true", r.Properties.Get("dynamic"))
	}
	if _, ok := r.Properties.Lookup("metric_name"); ok {
		t.Errorf("metric_name must be absent for dynamic name; got %q", r.Properties.Get("metric_name"))
	}
}

// --- Dropwizard metrics -----------------------------------------------------

// TestObs_Java_Dropwizard_Meter verifies a Dropwizard registry.meter("name").
func TestObs_Java_Dropwizard_Meter(t *testing.T) {
	src := `package svc;
class Endpoint {
    private final MetricRegistry registry;
    void serve() {
        registry.meter("http.requests").mark();
    }
}
`
	ents := runJava(t, src)
	r := javaObsEdge(ents, "Endpoint.serve", "metric:http.requests")
	if r == nil {
		dumpEdges(t, ents, "Endpoint.serve")
		t.Fatal("INSTRUMENTS edge Endpoint.serve → metric:http.requests not found")
	}
	if r.Properties.Get("library") != "dropwizard" {
		t.Errorf("library=%q, want dropwizard", r.Properties.Get("library"))
	}
	if r.Properties.Get("api") != "registry.meter" {
		t.Errorf("api=%q, want registry.meter", r.Properties.Get("api"))
	}
}

// --- Brave / Sleuth tracing -------------------------------------------------

// TestObs_Java_Brave_NextSpan verifies tracer.nextSpan().name("checkout").
func TestObs_Java_Brave_NextSpan(t *testing.T) {
	src := `package svc;
class Checkout {
    void checkout(Tracer tracer) {
        Span span = tracer.nextSpan().name("checkout").start();
        span.finish();
    }
}
`
	ents := runJava(t, src)
	r := javaObsEdge(ents, "Checkout.checkout", "span:checkout")
	if r == nil {
		dumpEdges(t, ents, "Checkout.checkout")
		t.Fatal("INSTRUMENTS edge Checkout.checkout → span:checkout not found")
	}
	if r.Properties.Get("library") != "brave" {
		t.Errorf("library=%q, want brave", r.Properties.Get("library"))
	}
	if r.Properties.Get("span_name") != "checkout" {
		t.Errorf("span_name=%q, want checkout", r.Properties.Get("span_name"))
	}
	if r.Properties.Get("api") != "tracer.nextSpan" {
		t.Errorf("api=%q, want tracer.nextSpan", r.Properties.Get("api"))
	}
}

// --- SLF4J structured logging -----------------------------------------------

// TestObs_Java_Slf4j_FluentKeyed verifies SLF4J fluent keyed logging stamps a
// log: edge with the event message as the name.
func TestObs_Java_Slf4j_FluentKeyed(t *testing.T) {
	src := `package svc;
class Orders {
    void place(Logger logger, String id) {
        logger.atInfo().addKeyValue("orderId", id).log("order placed");
    }
}
`
	ents := runJava(t, src)
	r := javaObsEdge(ents, "Orders.place", "log:order placed")
	if r == nil {
		dumpEdges(t, ents, "Orders.place")
		t.Fatal("INSTRUMENTS edge Orders.place → log:order placed not found")
	}
	if r.Properties.Get("library") != "slf4j" {
		t.Errorf("library=%q, want slf4j", r.Properties.Get("library"))
	}
	if r.Properties.Get("log_name") != "order placed" {
		t.Errorf("log_name=%q, want 'order placed'", r.Properties.Get("log_name"))
	}
	if r.Properties.Get("api") != "fluent.log" {
		t.Errorf("api=%q, want fluent.log", r.Properties.Get("api"))
	}
}

// TestObs_Java_Slf4j_PlainLog_NoEdge is the honest negative: plain free-text
// logging (no structured key) must NOT emit a log edge.
func TestObs_Java_Slf4j_PlainLog_NoEdge(t *testing.T) {
	src := `package svc;
class Plain {
    void run(Logger logger) {
        logger.info("just a message");
    }
}
`
	ents := runJava(t, src)
	e := javaFind(ents, "Plain.run", "SCOPE.Operation")
	if e == nil {
		t.Fatal("entity Plain.run not found")
	}
	for _, r := range e.Relationships {
		if r.Kind == "INSTRUMENTS" {
			t.Errorf("unexpected INSTRUMENTS edge on plain log: → %s", r.ToID)
		}
	}
}

// TestObs_Java_NoInstrumentation_NoEdge verifies no false positives on a method
// with an unrelated .counter()/.timer() call on a non-registry object.
func TestObs_Java_NoInstrumentation_NoEdge(t *testing.T) {
	src := `package svc;
class Plain {
    int add(int x) {
        widget.counter("nope");
        return x + 1;
    }
}
`
	ents := runJava(t, src)
	e := javaFind(ents, "Plain.add", "SCOPE.Operation")
	if e == nil {
		t.Fatal("entity Plain.add not found")
	}
	for _, r := range e.Relationships {
		if r.Kind == "INSTRUMENTS" {
			t.Errorf("unexpected INSTRUMENTS edge on non-registry call: → %s", r.ToID)
		}
	}
}
