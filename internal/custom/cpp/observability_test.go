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
