// Package csharp — observability extractor for C# source files.
//
// Detects OpenTelemetry tracing (ActivitySource / Activity) and metrics
// (Meter / Counter<T> / Histogram<T>) patterns, emitting SCOPE.Pattern
// entities so the coverage cells trace_extraction and metric_extraction
// light up for the C# backend frameworks.
//
// log_extraction is NOT duplicated here — it is already handled by the
// template_pattern sniffer in internal/substrate/template_pattern_csharp.go
// (csharpLogRe captures _logger.LogInformation etc.).
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
// Trace regexes — OpenTelemetry ActivitySource / Activity.Current
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
)

// ---------------------------------------------------------------------------
// Metric regexes — OpenTelemetry Meter / Counter / Histogram
// ---------------------------------------------------------------------------

var (
	// new Meter("name") — meter declaration
	reMeterNew = regexp.MustCompile(
		`new\s+Meter\s*\(\s*"([^"]+)"`,
	)
	// meter.CreateCounter<T>("name") / meter.CreateHistogram<T>("name")
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

	// Counter<T> / Histogram<T> type declarations
	if reMetricTypeDecl.MatchString(src) && !reMeterCreate.MatchString(src) && !reMeterCreateNoGeneric.MatchString(src) {
		name := "otel:metric:TypeDecl:" + file.Path
		ent := makeEntity(name, "SCOPE.Pattern", "metric_extraction", file.Path, "csharp", 1)
		setProps(&ent, "otel_signal", "metric", "pattern", "metric_type_decl")
		add(ent)
	}

	return entities, nil
}
