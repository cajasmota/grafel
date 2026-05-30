// Package csharp — observability extractor for C# source files.
//
// Detects log, metric, and trace patterns for ASP.NET Core / MVC, emitting
// SCOPE.Pattern entities so the coverage cells log_extraction,
// metric_extraction, and trace_extraction light up.
//
// # log_extraction (partial — call-site heuristic, no cross-file DI binding)
//
//   - ILogger<T> / ILoggerFactory field/constructor injection declarations
//   - _logger.LogInformation/LogError/LogWarning/LogDebug/LogTrace/LogCritical
//   - LoggerMessage.Define<...>("EventName", ...) high-perf logging
//   - Serilog Log.Information/Log.Error/Log.Warning/Log.Debug/Log.Fatal
//   - Microsoft.Extensions.Logging via AddLogging() service registration
//
// NOTE: We do NOT cross-file-bind ILogger<T> to its concrete handler; that
// requires dataflow analysis beyond our regex heuristic. Status: partial.
//
// # metric_extraction (partial)
//
//   - System.Diagnostics.Metrics: new Meter(...), CreateCounter/CreateHistogram/
//     CreateObservableGauge/CreateObservableCounter/CreateUpDownCounter
//   - IMeterFactory injection declaration
//   - prometheus-net: Metrics.CreateCounter/CreateHistogram/CreateGauge
//   - App.Metrics: Metrics.Measure.Counter/Histogram/Timer/Gauge
//
// # trace_extraction (partial)
//
//   - System.Diagnostics.ActivitySource / Activity / StartActivity
//   - OpenTelemetry AddOpenTelemetry().WithTracing(...) service registration
//   - Activity.Current usage
package csharp

import (
	"context"
	"regexp"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func init() {
	extractor.Register("custom_csharp_observability", &csharpObservabilityExtractor{})
}

type csharpObservabilityExtractor struct{}

func (e *csharpObservabilityExtractor) Language() string { return "custom_csharp_observability" }

// ---------------------------------------------------------------------------
// Log regexes
// ---------------------------------------------------------------------------

var (
	// ILogger<T> or ILoggerFactory field/constructor/parameter — DI injection site.
	// Matches: ILogger<OrderService>, ILoggerFactory, ILogger<T> (any T).
	reILoggerDecl = regexp.MustCompile(
		`\bILogger(?:Factory|<[^>]+>)?\b`,
	)

	// _logger.LogInformation/LogError/LogWarning/LogDebug/LogTrace/LogCritical(...)
	// Also matches: logger.LogXxx, _Logger.LogXxx (case-insensitive prefix).
	reLoggerCall = regexp.MustCompile(
		`\b_?[Ll]ogger\s*\.\s*(Log(?:Information|Error|Warning|Debug|Trace|Critical))\s*\(`,
	)

	// LoggerMessage.Define<...>("EventId-name", ...) high-perf compile-time log.
	reLoggerMessageDefine = regexp.MustCompile(
		`\bLoggerMessage\s*\.\s*Define\s*(?:<[^>]+>)?\s*\(`,
	)

	// Serilog: Log.Information/Log.Error/Log.Warning/Log.Debug/Log.Fatal/Log.Verbose
	reSerilogCall = regexp.MustCompile(
		`\bLog\s*\.\s*(Information|Error|Warning|Debug|Fatal|Verbose)\s*\(`,
	)

	// services.AddLogging() / builder.Logging.AddConsole() — service registration.
	reAddLogging = regexp.MustCompile(
		`\bAddLogging\s*\(`,
	)
)

// ---------------------------------------------------------------------------
// Trace regexes — OpenTelemetry ActivitySource / Activity
// ---------------------------------------------------------------------------

var (
	// new ActivitySource("name") or new ActivitySource(nameof(T)) — producer declaration
	reActivitySourceNew = regexp.MustCompile(
		`new\s+ActivitySource\s*\(\s*"([^"]+)"`,
	)
	// activitySource.StartActivity("operationName") — span start
	reStartActivity = regexp.MustCompile(
		`\.\s*StartActivity\s*\(\s*"([^"]+)"`,
	)
	// Activity.Current?.SetTag / AddTag / SetStatus — usage site
	reActivityCurrent = regexp.MustCompile(
		`\bActivity\s*\.\s*Current\b`,
	)
	// field/var typed as ActivitySource — declaration
	reActivitySourceDecl = regexp.MustCompile(
		`\bActivitySource\b`,
	)
	// AddOpenTelemetry().WithTracing(...) or .AddOpenTelemetryTracing(...)
	reAddOtelTracing = regexp.MustCompile(
		`\b(?:AddOpenTelemetry\s*\(\s*\)\s*\.\s*WithTracing|AddOpenTelemetryTracing)\s*\(`,
	)
)

// ---------------------------------------------------------------------------
// Metric regexes — System.Diagnostics.Metrics, prometheus-net, App.Metrics
// ---------------------------------------------------------------------------

var (
	// new Meter("name") — meter declaration
	reMeterNew = regexp.MustCompile(
		`new\s+Meter\s*\(\s*"([^"]+)"`,
	)
	// meter.CreateCounter<T>("name") / meter.CreateHistogram<T>("name") etc.
	reMeterCreate = regexp.MustCompile(
		`\.\s*Create(Counter|Histogram|ObservableGauge|ObservableCounter|ObservableUpDownCounter|UpDownCounter)\s*<[^>]+>\s*\(\s*"([^"]+)"`,
	)
	// Counter<T> or Histogram<T> field/var declaration
	reMetricTypeDecl = regexp.MustCompile(
		`\b(Counter|Histogram|ObservableGauge|ObservableCounter|UpDownCounter)\s*<`,
	)
	// meter.CreateCounter / meter.CreateHistogram without generic — also valid
	reMeterCreateNoGeneric = regexp.MustCompile(
		`\.\s*Create(Counter|Histogram|UpDownCounter)\s*\(\s*"([^"]+)"`,
	)
	// IMeterFactory injection declaration
	reIMeterFactory = regexp.MustCompile(
		`\bIMeterFactory\b`,
	)
	// prometheus-net: Metrics.CreateCounter/CreateHistogram/CreateGauge("name", ...)
	rePrometheusCreate = regexp.MustCompile(
		`\bMetrics\s*\.\s*Create(Counter|Histogram|Gauge|Summary)\s*\(\s*"([^"]+)"`,
	)
	// App.Metrics: Metrics.Measure.Counter/Histogram/Timer/Gauge.Increment/Record...
	reAppMetricsMeasure = regexp.MustCompile(
		`\bMetrics\s*\.\s*Measure\s*\.\s*(Counter|Histogram|Timer|Gauge)\b`,
	)
)

// ---------------------------------------------------------------------------
// Extract
// ---------------------------------------------------------------------------

func (e *csharpObservabilityExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/csharp")
	_, span := tracer.Start(ctx, "indexer.csharp_observability_extractor.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("file_path", file.Path),
		),
	)
	defer span.End()

	if len(file.Content) == 0 {
		return nil, nil
	}
	if file.Language != "csharp" {
		return nil, nil
	}

	src := string(file.Content)
	var entities []types.EntityRecord
	seen := make(map[string]bool)

	add := func(ent types.EntityRecord) {
		key := ent.Kind + ":" + ent.Subtype + ":" + ent.Name
		if seen[key] {
			return
		}
		seen[key] = true
		entities = append(entities, ent)
	}

	// --- log_extraction -------------------------------------------------------

	// ILogger<T> / ILoggerFactory injection declaration (one per file)
	if reILoggerDecl.MatchString(src) {
		name := "log:ilogger:decl:" + file.Path
		ent := makeEntity(name, "SCOPE.Pattern", "log_extraction", file.Path, "csharp", 1)
		setProps(&ent, "log_framework", "microsoft.extensions.logging", "pattern", "ILogger.injection")
		add(ent)
	}

	// _logger.LogXxx call sites
	for _, m := range reLoggerCall.FindAllStringSubmatchIndex(src, -1) {
		level := src[m[2]:m[3]]
		name := "log:call:" + level + ":" + file.Path
		line := lineOf(src, m[0])
		ent := makeEntity(name, "SCOPE.Pattern", "log_extraction", file.Path, "csharp", line)
		setProps(&ent, "log_framework", "microsoft.extensions.logging", "pattern", "logger."+level, "log_level", level)
		add(ent)
	}

	// LoggerMessage.Define high-perf logging (one per file)
	if reLoggerMessageDefine.MatchString(src) {
		name := "log:loggermessage:define:" + file.Path
		ent := makeEntity(name, "SCOPE.Pattern", "log_extraction", file.Path, "csharp", 1)
		setProps(&ent, "log_framework", "microsoft.extensions.logging", "pattern", "LoggerMessage.Define")
		add(ent)
	}

	// Serilog call sites
	for _, m := range reSerilogCall.FindAllStringSubmatchIndex(src, -1) {
		level := src[m[2]:m[3]]
		name := "log:serilog:" + level + ":" + file.Path
		line := lineOf(src, m[0])
		ent := makeEntity(name, "SCOPE.Pattern", "log_extraction", file.Path, "csharp", line)
		setProps(&ent, "log_framework", "serilog", "pattern", "Log."+level, "log_level", level)
		add(ent)
	}

	// AddLogging() service registration (one per file)
	if reAddLogging.MatchString(src) {
		name := "log:addlogging:reg:" + file.Path
		ent := makeEntity(name, "SCOPE.Pattern", "log_extraction", file.Path, "csharp", 1)
		setProps(&ent, "log_framework", "microsoft.extensions.logging", "pattern", "AddLogging")
		add(ent)
	}

	// --- trace_extraction ---------------------------------------------------

	// ActivitySource declaration
	for _, m := range reActivitySourceNew.FindAllStringSubmatchIndex(src, -1) {
		name := "otel:trace:ActivitySource:" + src[m[2]:m[3]]
		line := lineOf(src, m[0])
		ent := makeEntity(name, "SCOPE.Pattern", "trace_extraction", file.Path, "csharp", line)
		setProps(&ent, "otel_signal", "trace", "pattern", "ActivitySource.new")
		add(ent)
	}

	// StartActivity call sites
	for _, m := range reStartActivity.FindAllStringSubmatchIndex(src, -1) {
		name := "otel:trace:StartActivity:" + src[m[2]:m[3]]
		line := lineOf(src, m[0])
		ent := makeEntity(name, "SCOPE.Pattern", "trace_extraction", file.Path, "csharp", line)
		setProps(&ent, "otel_signal", "trace", "pattern", "StartActivity")
		add(ent)
	}

	// Activity.Current usage (emit one per file, not per usage)
	if reActivityCurrent.MatchString(src) {
		name := "otel:trace:Activity.Current:" + file.Path
		ent := makeEntity(name, "SCOPE.Pattern", "trace_extraction", file.Path, "csharp", 1)
		setProps(&ent, "otel_signal", "trace", "pattern", "Activity.Current")
		add(ent)
	}

	// ActivitySource type declaration (covers field: private static readonly ActivitySource _src = ...)
	if reActivitySourceDecl.MatchString(src) && !reActivitySourceNew.MatchString(src) {
		name := "otel:trace:ActivitySource:decl:" + file.Path
		ent := makeEntity(name, "SCOPE.Pattern", "trace_extraction", file.Path, "csharp", 1)
		setProps(&ent, "otel_signal", "trace", "pattern", "ActivitySource.decl")
		add(ent)
	}

	// AddOpenTelemetry().WithTracing / AddOpenTelemetryTracing service registration
	if reAddOtelTracing.MatchString(src) {
		name := "otel:trace:AddOtelTracing:reg:" + file.Path
		ent := makeEntity(name, "SCOPE.Pattern", "trace_extraction", file.Path, "csharp", 1)
		setProps(&ent, "otel_signal", "trace", "pattern", "AddOpenTelemetry.WithTracing")
		add(ent)
	}

	// --- metric_extraction --------------------------------------------------

	// Meter declaration
	for _, m := range reMeterNew.FindAllStringSubmatchIndex(src, -1) {
		name := "otel:metric:Meter:" + src[m[2]:m[3]]
		line := lineOf(src, m[0])
		ent := makeEntity(name, "SCOPE.Pattern", "metric_extraction", file.Path, "csharp", line)
		setProps(&ent, "otel_signal", "metric", "pattern", "Meter.new")
		add(ent)
	}

	// meter.CreateCounter / CreateHistogram etc. (generic form)
	for _, m := range reMeterCreate.FindAllStringSubmatchIndex(src, -1) {
		kind := src[m[2]:m[3]]
		mname := src[m[4]:m[5]]
		name := "otel:metric:" + kind + ":" + mname
		line := lineOf(src, m[0])
		ent := makeEntity(name, "SCOPE.Pattern", "metric_extraction", file.Path, "csharp", line)
		setProps(&ent, "otel_signal", "metric", "pattern", "Create"+kind, "metric_name", mname)
		add(ent)
	}

	// meter.CreateCounter etc. (non-generic form)
	for _, m := range reMeterCreateNoGeneric.FindAllStringSubmatchIndex(src, -1) {
		kind := src[m[2]:m[3]]
		mname := src[m[4]:m[5]]
		name := "otel:metric:" + kind + ":" + mname
		line := lineOf(src, m[0])
		ent := makeEntity(name, "SCOPE.Pattern", "metric_extraction", file.Path, "csharp", line)
		setProps(&ent, "otel_signal", "metric", "pattern", "Create"+kind, "metric_name", mname)
		add(ent)
	}

	// Counter<T> / Histogram<T> type declarations (only when no Create* call present)
	if reMetricTypeDecl.MatchString(src) && !reMeterCreate.MatchString(src) && !reMeterCreateNoGeneric.MatchString(src) {
		name := "otel:metric:TypeDecl:" + file.Path
		ent := makeEntity(name, "SCOPE.Pattern", "metric_extraction", file.Path, "csharp", 1)
		setProps(&ent, "otel_signal", "metric", "pattern", "metric_type_decl")
		add(ent)
	}

	// IMeterFactory injection declaration (one per file)
	if reIMeterFactory.MatchString(src) {
		name := "otel:metric:IMeterFactory:decl:" + file.Path
		ent := makeEntity(name, "SCOPE.Pattern", "metric_extraction", file.Path, "csharp", 1)
		setProps(&ent, "otel_signal", "metric", "pattern", "IMeterFactory.injection")
		add(ent)
	}

	// prometheus-net Metrics.CreateCounter/CreateHistogram/CreateGauge
	for _, m := range rePrometheusCreate.FindAllStringSubmatchIndex(src, -1) {
		kind := src[m[2]:m[3]]
		mname := src[m[4]:m[5]]
		name := "prometheus:metric:" + kind + ":" + mname
		line := lineOf(src, m[0])
		ent := makeEntity(name, "SCOPE.Pattern", "metric_extraction", file.Path, "csharp", line)
		setProps(&ent, "metric_framework", "prometheus-net", "pattern", "Metrics.Create"+kind, "metric_name", mname)
		add(ent)
	}

	// App.Metrics Metrics.Measure.Counter/Histogram/Timer/Gauge (one per signal type per file)
	for _, m := range reAppMetricsMeasure.FindAllStringSubmatchIndex(src, -1) {
		kind := src[m[2]:m[3]]
		name := "appmetrics:metric:" + kind + ":" + file.Path
		line := lineOf(src, m[0])
		ent := makeEntity(name, "SCOPE.Pattern", "metric_extraction", file.Path, "csharp", line)
		setProps(&ent, "metric_framework", "app.metrics", "pattern", "Metrics.Measure."+kind)
		add(ent)
	}

	return entities, nil
}
