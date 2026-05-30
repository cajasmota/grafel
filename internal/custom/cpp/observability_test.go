package cpp_test

// observability_test.go — fixture tests for observability.go.
// Exercises spdlog, glog/LOG, prometheus-cpp, opentelemetry-cpp, and tracing.

import "testing"

// ---------------------------------------------------------------------------
// Log extraction
// ---------------------------------------------------------------------------

func TestCppObsSpdlogInfo(t *testing.T) {
	src := `
#include <spdlog/spdlog.h>
void process() {
    spdlog::info("Processing request id={}", req_id);
    spdlog::error("Failed with status={}", status);
}
`
	ents := extract(t, "custom_cpp_observability", fi("service.cpp", "cpp", src))
	found := false
	for _, e := range ents {
		if e.Kind == "SCOPE.Pattern" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected SCOPE.Pattern for spdlog::info, got %v", ents)
	}
}

func TestCppObsSpdlogLogger(t *testing.T) {
	src := `logger->debug("handler entered, path={}", path);`
	ents := extract(t, "custom_cpp_observability", fi("handler.cpp", "cpp", src))
	found := false
	for _, e := range ents {
		if e.Kind == "SCOPE.Pattern" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected SCOPE.Pattern for logger->debug, got %v", ents)
	}
}

func TestCppObsGlogLOG(t *testing.T) {
	src := `
#include <glog/logging.h>
void serve() {
    LOG(INFO) << "Server started on port " << port;
    LOG(WARNING) << "High memory usage";
}
`
	ents := extract(t, "custom_cpp_observability", fi("server.cpp", "cpp", src))
	found := false
	for _, e := range ents {
		if e.Kind == "SCOPE.Pattern" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected SCOPE.Pattern for LOG(INFO) glog, got %v", ents)
	}
}

func TestCppObsStdCerr(t *testing.T) {
	src := `std::cerr << "Error: " << msg << std::endl;`
	ents := extract(t, "custom_cpp_observability", fi("util.cpp", "cpp", src))
	found := false
	for _, e := range ents {
		if e.Kind == "SCOPE.Pattern" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected SCOPE.Pattern for std::cerr <<, got %v", ents)
	}
}

// ---------------------------------------------------------------------------
// Metric extraction
// ---------------------------------------------------------------------------

func TestCppObsPrometheusCounter(t *testing.T) {
	src := `
#include <prometheus/counter.h>
#include <prometheus/registry.h>
auto& counter = prometheus::BuildCounter()
    .Name("http_requests_total")
    .Help("Total HTTP requests")
    .Register(registry);
`
	ents := extract(t, "custom_cpp_observability", fi("metrics.cpp", "cpp", src))
	found := false
	for _, e := range ents {
		if e.Kind == "SCOPE.Pattern" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected SCOPE.Pattern for prometheus::BuildCounter, got %v", ents)
	}
}

func TestCppObsPrometheusType(t *testing.T) {
	src := `prometheus::Counter& req_counter = counter_family.Add({{"route", path}});`
	ents := extract(t, "custom_cpp_observability", fi("handler.cpp", "cpp", src))
	found := false
	for _, e := range ents {
		if e.Kind == "SCOPE.Pattern" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected SCOPE.Pattern for prometheus::Counter type, got %v", ents)
	}
}

func TestCppObsOtelMeter(t *testing.T) {
	src := `auto counter = meter->CreateCounter<uint64_t>("requests");`
	ents := extract(t, "custom_cpp_observability", fi("otel_metrics.cpp", "cpp", src))
	found := false
	for _, e := range ents {
		if e.Kind == "SCOPE.Pattern" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected SCOPE.Pattern for meter->CreateCounter, got %v", ents)
	}
}

// ---------------------------------------------------------------------------
// Trace extraction
// ---------------------------------------------------------------------------

func TestCppObsOtelStartSpan(t *testing.T) {
	src := `
#include <opentelemetry/trace/provider.h>
auto tracer = provider->GetTracer("my-service");
auto span = tracer->StartSpan("handle_request");
span->End();
`
	ents := extract(t, "custom_cpp_observability", fi("tracer.cpp", "cpp", src))
	found := false
	for _, e := range ents {
		if e.Kind == "SCOPE.Pattern" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected SCOPE.Pattern for tracer->StartSpan, got %v", ents)
	}
}

func TestCppObsOtelTraceNamespace(t *testing.T) {
	src := `opentelemetry::trace::Scope scope(span);`
	ents := extract(t, "custom_cpp_observability", fi("trace_scope.cpp", "cpp", src))
	found := false
	for _, e := range ents {
		if e.Kind == "SCOPE.Pattern" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected SCOPE.Pattern for opentelemetry::trace:: ns, got %v", ents)
	}
}

func TestCppObsJaeger(t *testing.T) {
	src := `
#include <jaegertracing/Tracer.h>
auto tracer = jaeger::Tracer::make("service-name", config);
`
	ents := extract(t, "custom_cpp_observability", fi("jaeger_tracer.cpp", "cpp", src))
	found := false
	for _, e := range ents {
		if e.Kind == "SCOPE.Pattern" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected SCOPE.Pattern for jaeger tracer, got %v", ents)
	}
}

// ---------------------------------------------------------------------------
// Negative / boundary tests
// ---------------------------------------------------------------------------

func TestCppObsNoMatch(t *testing.T) {
	src := `
#include <iostream>
int main() {
    int x = 42;
    return 0;
}
`
	ents := extract(t, "custom_cpp_observability", fi("main.cpp", "cpp", src))
	if len(ents) != 0 {
		t.Errorf("expected no entities for plain main, got %d", len(ents))
	}
}

func TestCppObsWrongLanguage(t *testing.T) {
	src := `spdlog::info("test");`
	ents := extract(t, "custom_cpp_observability", fi("test.c", "c", src))
	if len(ents) != 0 {
		t.Errorf("wrong language should return no entities, got %d", len(ents))
	}
}

// ---------------------------------------------------------------------------
// Value-asserting tests — prove the SPECIFIC call-site literal (span/metric
// name, log level) is captured into its named property. These are the cells
// flipped to full: name is a literal at the call site, no cross-file binding.
// ---------------------------------------------------------------------------

// findByProp returns the first entity whose property `prop` equals `val`.
func findByProp(ents []entitySummary, prop, val string) *entitySummary {
	for i := range ents {
		if ents[i].Props[prop] == val {
			return &ents[i]
		}
	}
	return nil
}

func TestCppObsTraceSpanNameValue(t *testing.T) {
	src := `auto span = tracer->StartSpan("handle_checkout");`
	ents := extract(t, "custom_cpp_observability", fi("trace.cpp", "cpp", src))
	e := findByProp(ents, "span_name", "handle_checkout")
	if e == nil {
		t.Fatalf("expected span_name=handle_checkout, got %v", ents)
	}
	if e.Props["observability_type"] != "tracing" {
		t.Errorf("span_name entity observability_type = %q, want tracing", e.Props["observability_type"])
	}
}

func TestCppObsTraceActiveSpanNameValue(t *testing.T) {
	src := `auto s = tracer->StartActiveSpan("db.query");`
	ents := extract(t, "custom_cpp_observability", fi("trace.cpp", "cpp", src))
	if findByProp(ents, "span_name", "db.query") == nil {
		t.Fatalf("expected span_name=db.query, got %v", ents)
	}
}

func TestCppObsJaegerSpanNameValue(t *testing.T) {
	src := `auto span = some_tracer.StartSpan("rpc.invoke");`
	ents := extract(t, "custom_cpp_observability", fi("trace.cpp", "cpp", src))
	if findByProp(ents, "span_name", "rpc.invoke") == nil {
		t.Fatalf("expected span_name=rpc.invoke, got %v", ents)
	}
}

func TestCppObsPrometheusMetricNameValue(t *testing.T) {
	src := `
auto& counter = prometheus::BuildCounter()
    .Name("http_requests_total")
    .Help("Total HTTP requests")
    .Register(registry);
`
	ents := extract(t, "custom_cpp_observability", fi("metrics.cpp", "cpp", src))
	e := findByProp(ents, "metric_name", "http_requests_total")
	if e == nil {
		t.Fatalf("expected metric_name=http_requests_total, got %v", ents)
	}
	if e.Props["observability_type"] != "metrics" {
		t.Errorf("metric_name entity observability_type = %q, want metrics", e.Props["observability_type"])
	}
}

func TestCppObsOtelMeterMetricNameValue(t *testing.T) {
	src := `auto counter = meter->CreateCounter<uint64_t>("requests_handled");`
	ents := extract(t, "custom_cpp_observability", fi("otel_metrics.cpp", "cpp", src))
	if findByProp(ents, "metric_name", "requests_handled") == nil {
		t.Fatalf("expected metric_name=requests_handled, got %v", ents)
	}
}

func TestCppObsStatsdMetricNameValue(t *testing.T) {
	src := `statsd_client.increment("api.calls");`
	ents := extract(t, "custom_cpp_observability", fi("statsd.cpp", "cpp", src))
	if findByProp(ents, "metric_name", "api.calls") == nil {
		t.Fatalf("expected metric_name=api.calls, got %v", ents)
	}
}

func TestCppObsSpdlogLevelValue(t *testing.T) {
	src := `spdlog::error("oops {}", code);`
	ents := extract(t, "custom_cpp_observability", fi("svc.cpp", "cpp", src))
	if findByProp(ents, "log_level", "error") == nil {
		t.Fatalf("expected log_level=error, got %v", ents)
	}
}

func TestCppObsGlogLevelValue(t *testing.T) {
	src := `LOG(WARNING) << "high memory";`
	ents := extract(t, "custom_cpp_observability", fi("svc.cpp", "cpp", src))
	if findByProp(ents, "log_level", "WARNING") == nil {
		t.Fatalf("expected log_level=WARNING, got %v", ents)
	}
}

// Negative: a metric name built at runtime is NOT pinned (honest partial).
func TestCppObsOtelMeterUnnamedNoMetricName(t *testing.T) {
	src := `auto counter = meter->CreateCounter<uint64_t>(name_var);`
	ents := extract(t, "custom_cpp_observability", fi("otel_metrics.cpp", "cpp", src))
	for _, e := range ents {
		if e.Props["metric_name"] != "" {
			t.Errorf("runtime-bound metric should not pin metric_name, got %q", e.Props["metric_name"])
		}
	}
	// but it should still be detected as a metric pattern
	found := false
	for _, e := range ents {
		if e.Props["observability_type"] == "metrics" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected a metrics pattern for unnamed CreateCounter, got %v", ents)
	}
}

func TestCppObsDrogonFrameworkDetection(t *testing.T) {
	src := `
#include <drogon/drogon.h>
#include <spdlog/spdlog.h>
void handler() {
    spdlog::info("Drogon request handled");
}
`
	ents := extract(t, "custom_cpp_observability", fi("drogon_handler.cpp", "cpp", src))
	found := false
	for _, e := range ents {
		if e.Kind == "SCOPE.Pattern" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected SCOPE.Pattern with drogon framework detection, got %v", ents)
	}
}
