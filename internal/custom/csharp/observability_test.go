package csharp_test

import (
	"testing"
)

// ---------------------------------------------------------------------------
// Observability — trace_extraction
// ---------------------------------------------------------------------------

func TestObservabilityActivitySourceNew(t *testing.T) {
	src := `
private static readonly ActivitySource _tracer = new ActivitySource("MyService.Orders");

public void ProcessOrder(int id)
{
    using var activity = _tracer.StartActivity("process-order");
    activity?.SetTag("order_id", id);
}
`
	ents := extract(t, "custom_csharp_observability", fi("OrderService.cs", "csharp", src))
	found := false
	for _, e := range ents {
		if e.Kind == "SCOPE.Pattern" && e.Subtype == "trace_extraction" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected at least one trace_extraction SCOPE.Pattern from ActivitySource")
	}
}

func TestObservabilityStartActivity(t *testing.T) {
	src := `
public void Handle()
{
    using var span = _activitySource.StartActivity("handle-request");
}
`
	ents := extract(t, "custom_csharp_observability", fi("Handler.cs", "csharp", src))
	found := false
	for _, e := range ents {
		if e.Kind == "SCOPE.Pattern" && e.Subtype == "trace_extraction" && e.Name == "otel:trace:StartActivity:handle-request" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected otel:trace:StartActivity:handle-request from StartActivity call")
	}
}

func TestObservabilityActivityCurrent(t *testing.T) {
	src := `Activity.Current?.SetTag("user_id", userId);`
	ents := extract(t, "custom_csharp_observability", fi("Middleware.cs", "csharp", src))
	found := false
	for _, e := range ents {
		if e.Kind == "SCOPE.Pattern" && e.Subtype == "trace_extraction" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected trace_extraction entity from Activity.Current usage")
	}
}

// ---------------------------------------------------------------------------
// Observability — metric_extraction
// ---------------------------------------------------------------------------

func TestObservabilityMeterNew(t *testing.T) {
	src := `var meter = new Meter("MyService.Metrics");`
	ents := extract(t, "custom_csharp_observability", fi("Metrics.cs", "csharp", src))
	found := false
	for _, e := range ents {
		if e.Kind == "SCOPE.Pattern" && e.Subtype == "metric_extraction" && e.Name == "otel:metric:Meter:MyService.Metrics" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected otel:metric:Meter:MyService.Metrics from new Meter()")
	}
}

func TestObservabilityCreateCounter(t *testing.T) {
	src := `
var meter = new Meter("MyApp");
var requestCounter = meter.CreateCounter<long>("http.requests", "requests", "Total HTTP requests");
`
	ents := extract(t, "custom_csharp_observability", fi("Instrumentation.cs", "csharp", src))
	counterFound := false
	for _, e := range ents {
		if e.Kind == "SCOPE.Pattern" && e.Subtype == "metric_extraction" && e.Name == "otel:metric:Counter:http.requests" {
			counterFound = true
			break
		}
	}
	if !counterFound {
		t.Error("expected otel:metric:Counter:http.requests from CreateCounter<long>")
	}
}

func TestObservabilityCreateHistogram(t *testing.T) {
	src := `var latency = meter.CreateHistogram<double>("request.duration", "ms", "Request latency");`
	ents := extract(t, "custom_csharp_observability", fi("Metrics.cs", "csharp", src))
	found := false
	for _, e := range ents {
		if e.Kind == "SCOPE.Pattern" && e.Subtype == "metric_extraction" && e.Name == "otel:metric:Histogram:request.duration" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected otel:metric:Histogram:request.duration from CreateHistogram")
	}
}

func TestObservabilityNoMatch(t *testing.T) {
	src := `namespace App { class Helper { public void DoWork() {} } }`
	ents := extract(t, "custom_csharp_observability", fi("Helper.cs", "csharp", src))
	if len(ents) != 0 {
		t.Errorf("expected 0 observability entities, got %d", len(ents))
	}
}

func TestObservabilityWrongLanguageSkipped(t *testing.T) {
	src := `var meter = new Meter("test");`
	ents := extract(t, "custom_csharp_observability", fi("file.py", "python", src))
	if len(ents) != 0 {
		t.Errorf("expected 0 entities for non-csharp language, got %d", len(ents))
	}
}
