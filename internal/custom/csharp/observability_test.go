package csharp_test

import (
	"testing"
)

// ---------------------------------------------------------------------------
// Observability — log_extraction
// ---------------------------------------------------------------------------

func TestObservabilityILoggerDecl(t *testing.T) {
	src := `
public class OrderService
{
    private readonly ILogger<OrderService> _logger;

    public OrderService(ILogger<OrderService> logger)
    {
        _logger = logger;
    }
}
`
	ents := extract(t, "custom_csharp_observability", fi("OrderService.cs", "csharp", src))
	found := false
	for _, e := range ents {
		if e.Kind == "SCOPE.Pattern" && e.Subtype == "log_extraction" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected log_extraction entity from ILogger<T> injection")
	}
}

func TestObservabilityILoggerFactoryDecl(t *testing.T) {
	src := `
public class App
{
    private readonly ILoggerFactory _factory;
    public App(ILoggerFactory factory) { _factory = factory; }
}
`
	ents := extract(t, "custom_csharp_observability", fi("App.cs", "csharp", src))
	found := false
	for _, e := range ents {
		if e.Kind == "SCOPE.Pattern" && e.Subtype == "log_extraction" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected log_extraction entity from ILoggerFactory injection")
	}
}

func TestObservabilityLoggerLogError(t *testing.T) {
	src := `
public void Handle(Exception ex)
{
    _logger.LogError(ex, "Failed to process order {OrderId}", orderId);
}
`
	ents := extract(t, "custom_csharp_observability", fi("Handler.cs", "csharp", src))
	found := false
	for _, e := range ents {
		if e.Kind == "SCOPE.Pattern" && e.Subtype == "log_extraction" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected log_extraction entity from _logger.LogError call")
	}
}

func TestObservabilityLoggerLogInformation(t *testing.T) {
	src := `_logger.LogInformation("Processing order {Id}", id);`
	ents := extract(t, "custom_csharp_observability", fi("Svc.cs", "csharp", src))
	found := false
	for _, e := range ents {
		if e.Kind == "SCOPE.Pattern" && e.Subtype == "log_extraction" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected log_extraction entity from _logger.LogInformation")
	}
}

func TestObservabilityLoggerLogWarning(t *testing.T) {
	src := `_logger.LogWarning("Retry attempt {N}", attempt);`
	ents := extract(t, "custom_csharp_observability", fi("Retry.cs", "csharp", src))
	found := false
	for _, e := range ents {
		if e.Kind == "SCOPE.Pattern" && e.Subtype == "log_extraction" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected log_extraction entity from _logger.LogWarning")
	}
}

func TestObservabilityLoggerLogDebug(t *testing.T) {
	src := `logger.LogDebug("cache miss for key={Key}", key);`
	ents := extract(t, "custom_csharp_observability", fi("Cache.cs", "csharp", src))
	found := false
	for _, e := range ents {
		if e.Kind == "SCOPE.Pattern" && e.Subtype == "log_extraction" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected log_extraction entity from logger.LogDebug")
	}
}

func TestObservabilityLoggerMessageDefine(t *testing.T) {
	src := `
private static readonly Action<ILogger, int, Exception?> _orderProcessed =
    LoggerMessage.Define<int>(LogLevel.Information, new EventId(1, "OrderProcessed"),
        "Order {OrderId} processed");
`
	ents := extract(t, "custom_csharp_observability", fi("Logging.cs", "csharp", src))
	found := false
	for _, e := range ents {
		if e.Kind == "SCOPE.Pattern" && e.Subtype == "log_extraction" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected log_extraction entity from LoggerMessage.Define")
	}
}

func TestObservabilitySerilogInformation(t *testing.T) {
	src := `Log.Information("User {UserId} logged in", userId);`
	ents := extract(t, "custom_csharp_observability", fi("Auth.cs", "csharp", src))
	found := false
	for _, e := range ents {
		if e.Kind == "SCOPE.Pattern" && e.Subtype == "log_extraction" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected log_extraction entity from Serilog Log.Information")
	}
}

func TestObservabilitySerilogError(t *testing.T) {
	src := `Log.Error(ex, "Unhandled exception in {Action}", actionName);`
	ents := extract(t, "custom_csharp_observability", fi("Ex.cs", "csharp", src))
	found := false
	for _, e := range ents {
		if e.Kind == "SCOPE.Pattern" && e.Subtype == "log_extraction" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected log_extraction entity from Serilog Log.Error")
	}
}

func TestObservabilityAddLoggingRegistration(t *testing.T) {
	src := `
builder.Services.AddLogging(cfg =>
{
    cfg.AddConsole();
    cfg.AddDebug();
});
`
	ents := extract(t, "custom_csharp_observability", fi("Program.cs", "csharp", src))
	found := false
	for _, e := range ents {
		if e.Kind == "SCOPE.Pattern" && e.Subtype == "log_extraction" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected log_extraction entity from services.AddLogging()")
	}
}

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

func TestObservabilityAddOpenTelemetryWithTracing(t *testing.T) {
	src := `
builder.Services.AddOpenTelemetry()
    .WithTracing(tracing =>
    {
        tracing.AddAspNetCoreInstrumentation();
        tracing.AddHttpClientInstrumentation();
        tracing.AddOtlpExporter();
    });
`
	ents := extract(t, "custom_csharp_observability", fi("Program.cs", "csharp", src))
	found := false
	for _, e := range ents {
		if e.Kind == "SCOPE.Pattern" && e.Subtype == "trace_extraction" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected trace_extraction entity from AddOpenTelemetry().WithTracing()")
	}
}

func TestObservabilityAddOpenTelemetryTracingLegacy(t *testing.T) {
	src := `services.AddOpenTelemetryTracing(builder => builder.AddAspNetCoreInstrumentation());`
	ents := extract(t, "custom_csharp_observability", fi("Startup.cs", "csharp", src))
	found := false
	for _, e := range ents {
		if e.Kind == "SCOPE.Pattern" && e.Subtype == "trace_extraction" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected trace_extraction entity from AddOpenTelemetryTracing()")
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

func TestObservabilityIMeterFactory(t *testing.T) {
	src := `
public class OrderMetrics
{
    private readonly IMeterFactory _meterFactory;

    public OrderMetrics(IMeterFactory meterFactory)
    {
        _meterFactory = meterFactory;
        var meter = meterFactory.Create("orders");
    }
}
`
	ents := extract(t, "custom_csharp_observability", fi("OrderMetrics.cs", "csharp", src))
	found := false
	for _, e := range ents {
		if e.Kind == "SCOPE.Pattern" && e.Subtype == "metric_extraction" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected metric_extraction entity from IMeterFactory injection")
	}
}

func TestObservabilityPrometheusNetCreateCounter(t *testing.T) {
	src := `
private static readonly Counter _requestsTotal =
    Metrics.CreateCounter("http_requests_total", "Total HTTP requests");
`
	ents := extract(t, "custom_csharp_observability", fi("PrometheusMetrics.cs", "csharp", src))
	found := false
	for _, e := range ents {
		if e.Kind == "SCOPE.Pattern" && e.Subtype == "metric_extraction" && e.Name == "prometheus:metric:Counter:http_requests_total" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected prometheus:metric:Counter:http_requests_total from prometheus-net Metrics.CreateCounter")
	}
}

func TestObservabilityPrometheusNetCreateHistogram(t *testing.T) {
	src := `var histogram = Metrics.CreateHistogram("request_duration_seconds", "Request duration in seconds");`
	ents := extract(t, "custom_csharp_observability", fi("Prom.cs", "csharp", src))
	found := false
	for _, e := range ents {
		if e.Kind == "SCOPE.Pattern" && e.Subtype == "metric_extraction" && e.Name == "prometheus:metric:Histogram:request_duration_seconds" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected prometheus:metric:Histogram from prometheus-net Metrics.CreateHistogram")
	}
}

func TestObservabilityAppMetricsMeasureCounter(t *testing.T) {
	src := `Metrics.Measure.Counter.Increment(MyMetrics.RequestsCounter);`
	ents := extract(t, "custom_csharp_observability", fi("AppM.cs", "csharp", src))
	found := false
	for _, e := range ents {
		if e.Kind == "SCOPE.Pattern" && e.Subtype == "metric_extraction" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected metric_extraction entity from App.Metrics Metrics.Measure.Counter")
	}
}

func TestObservabilityAppMetricsMeasureHistogram(t *testing.T) {
	src := `Metrics.Measure.Histogram.Update(MyMetrics.DurationHistogram, elapsed.Milliseconds);`
	ents := extract(t, "custom_csharp_observability", fi("AppM2.cs", "csharp", src))
	found := false
	for _, e := range ents {
		if e.Kind == "SCOPE.Pattern" && e.Subtype == "metric_extraction" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected metric_extraction entity from App.Metrics Metrics.Measure.Histogram")
	}
}

// ---------------------------------------------------------------------------
// Cross-capability: full program with all three signals
// ---------------------------------------------------------------------------

func TestObservabilityFullProgram(t *testing.T) {
	src := `
using Microsoft.Extensions.Logging;
using System.Diagnostics;
using System.Diagnostics.Metrics;

public class OrderProcessor
{
    private static readonly ActivitySource _tracer = new ActivitySource("MyApp.Orders");
    private readonly ILogger<OrderProcessor> _logger;
    private readonly Counter<long> _processed;

    public OrderProcessor(ILogger<OrderProcessor> logger, IMeterFactory mf)
    {
        _logger = logger;
        var meter = mf.Create("orders");
        _processed = meter.CreateCounter<long>("orders.processed");
    }

    public async Task ProcessAsync(int orderId)
    {
        using var activity = _tracer.StartActivity("process-order");
        _logger.LogInformation("Processing order {Id}", orderId);
        try
        {
            // ... business logic ...
            _processed.Add(1);
            _logger.LogInformation("Order {Id} done", orderId);
        }
        catch (Exception ex)
        {
            _logger.LogError(ex, "Order {Id} failed", orderId);
            Activity.Current?.SetStatus(ActivityStatusCode.Error);
            throw;
        }
    }
}
`
	ents := extract(t, "custom_csharp_observability", fi("OrderProcessor.cs", "csharp", src))

	var logEnts, traceEnts, metricEnts []entitySummary
	for _, e := range ents {
		switch e.Subtype {
		case "log_extraction":
			logEnts = append(logEnts, e)
		case "trace_extraction":
			traceEnts = append(traceEnts, e)
		case "metric_extraction":
			metricEnts = append(metricEnts, e)
		}
	}

	if len(logEnts) == 0 {
		t.Error("expected log_extraction entities in full program")
	}
	if len(traceEnts) == 0 {
		t.Error("expected trace_extraction entities in full program")
	}
	if len(metricEnts) == 0 {
		t.Error("expected metric_extraction entities in full program")
	}

	// Specific entity assertions
	if !containsEntity(ents, "SCOPE.Pattern", "otel:trace:StartActivity:process-order") {
		t.Error("expected otel:trace:StartActivity:process-order entity")
	}
	if !containsEntity(ents, "SCOPE.Pattern", "otel:metric:Counter:orders.processed") {
		t.Error("expected otel:metric:Counter:orders.processed entity")
	}
}

// ---------------------------------------------------------------------------
// Negative / boundary cases
// ---------------------------------------------------------------------------

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
