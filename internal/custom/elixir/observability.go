package elixir

import (
	"context"
	"regexp"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

// Elixir observability custom extractor: structured logging (Logger),
// metrics + traces via :telemetry and Telemetry.Metrics (#3474, epic #3467).
//
// Covers the Observability lane, file-local and regex-based:
//   - log_extraction: `Logger.debug/info/warn/warning/error(...)` statements
//     and `Logger.metadata(...)` calls. Emits a SCOPE.Pattern(subtype=
//     log_statement) per Logger call carrying log_level (+ the message string
//     literal when present), and a SCOPE.Pattern(subtype=log_metadata) per
//     Logger.metadata call. Stays PARTIAL: the logger require/import and the
//     message binding are not correlated across files, and interpolated /
//     non-literal messages are not resolved.
//   - metric_extraction: `:telemetry.execute([:a, :b], ...)` event emission and
//     Telemetry.Metrics reporter definitions —
//     counter/summary/last_value/distribution/sum("event.name"). Emits a
//     SCOPE.Pattern(subtype=metric) carrying metric_name + telemetry_event when
//     the name is a literal at the call site. Stays PARTIAL: the metric/event
//     name → handler-attach (`:telemetry.attach`) → reporter wiring spans
//     multiple files and is NOT resolved here.
//   - trace_extraction: `:telemetry.span([:a, :b], ...)` span emission. Emits a
//     SCOPE.Pattern(subtype=trace_span) carrying telemetry_event. Stays
//     PARTIAL: there is no statically-resolvable OpenTelemetry span/exporter
//     binding in idiomatic Elixir — spans are bridged from :telemetry events by
//     a separate handler (e.g. OpentelemetryPhoenix) attached at runtime.
//
// Reuses the SCOPE.Pattern entity Kind (matching the Java observability
// extractor and the Plug/Phoenix elixir extractors) so no new entity Kind is
// registered, per the #2839 "prefer decorating over inventing Kinds"
// discipline. The observability role is recorded in the entity Subtype.

func init() {
	extractor.Register("custom_elixir_observability", &observabilityExtractor{})
}

type observabilityExtractor struct{}

func (e *observabilityExtractor) Language() string { return "custom_elixir_observability" }

var (
	// obsLoggerStmtRE matches a Logger statement call:
	//   Logger.info("msg") / Logger.warning("msg") / Logger.error(...) etc.
	// Group 1 = level. The level set matches the Elixir Logger API
	// (warn is the legacy alias of warning).
	obsLoggerStmtRE = regexp.MustCompile(
		`\bLogger\s*\.\s*(emergency|alert|critical|error|warning|warn|notice|info|debug)\s*\(`)

	// obsLoggerMetadataRE matches `Logger.metadata(...)` calls that attach
	// structured fields to the logging context.
	obsLoggerMetadataRE = regexp.MustCompile(
		`\bLogger\s*\.\s*metadata\s*\(`)

	// obsTelemetryExecuteRE matches a :telemetry.execute call and captures the
	// event-name atom list literal, e.g.
	//   :telemetry.execute([:my_app, :request, :stop], measurements, metadata)
	// Group 1 = the raw bracketed atom list (`:my_app, :request, :stop`).
	obsTelemetryExecuteRE = regexp.MustCompile(
		`:telemetry\s*\.\s*execute\s*\(\s*\[([^\]]*)\]`)

	// obsTelemetrySpanRE matches a :telemetry.span call and captures the
	// event-prefix atom list literal, e.g.
	//   :telemetry.span([:my_app, :worker], metadata, fn -> ... end)
	// Group 1 = the raw bracketed atom list.
	obsTelemetrySpanRE = regexp.MustCompile(
		`:telemetry\s*\.\s*span\s*\(\s*\[([^\]]*)\]`)

	// obsTelemetryMetricRE matches a Telemetry.Metrics reporter definition:
	//   counter("phoenix.endpoint.stop.duration")
	//   summary("my_app.repo.query.total_time", unit: ...)
	//   last_value("vm.memory.total")
	//   distribution("http.request.duration")
	//   sum("my_app.events.count")
	// Group 1 = metric kind, group 2 = the metric name string literal.
	obsTelemetryMetricRE = regexp.MustCompile(
		`\b(counter|summary|last_value|distribution|sum)\s*\(\s*"([^"]*)"`)

	// obsLeadingStringRE pulls a leading double-quoted string argument off the
	// remainder of a Logger.<level>( call so the message literal can be
	// recorded when present (interpolated/non-literal messages yield "").
	obsLeadingStringRE = regexp.MustCompile(`^\s*"((?:[^"\\]|\\.)*)"`)
)

// obsAtomList normalises a raw bracketed atom list (`:my_app, :request, :stop`)
// into a dotted event name (`my_app.request.stop`). Non-atom / interpolated
// entries (e.g. a bare variable) are kept verbatim so the name is honest about
// what was at the call site. Returns "" when no segment is recoverable.
func obsAtomList(raw string) string {
	parts := strings.Split(raw, ",")
	segs := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		p = strings.TrimPrefix(p, ":")
		segs = append(segs, p)
	}
	return strings.Join(segs, ".")
}

func (e *observabilityExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/elixir")
	_, span := tracer.Start(ctx, "indexer.observability_extractor.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("framework", "observability"),
			attribute.String("file_path", file.Path),
		),
	)
	defer span.End()

	if len(file.Content) == 0 {
		return nil, nil
	}
	if file.Language != "elixir" {
		return nil, nil
	}

	src := string(file.Content)
	var entities []types.EntityRecord
	seen := make(map[string]bool)

	add := func(ent types.EntityRecord) {
		key := ent.Kind + ":" + ent.Subtype + ":" + ent.Name + ":" +
			ent.Properties["telemetry_event"] + ":" + ent.Properties["log_level"]
		if seen[key] {
			return
		}
		seen[key] = true
		entities = append(entities, ent)
	}

	// --- log_extraction -----------------------------------------------------

	// Logger.<level>(...) statements.
	for _, m := range obsLoggerStmtRE.FindAllStringSubmatchIndex(src, -1) {
		level := src[m[2]:m[3]]
		if level == "warn" {
			level = "warning" // canonicalise the legacy alias
		}
		// Recover a leading string-literal message argument when present.
		message := ""
		if sm := obsLeadingStringRE.FindStringSubmatch(src[m[1]:]); sm != nil {
			message = sm[1]
		}
		name := "Logger." + level
		ent := makeEntity(name, "SCOPE.Pattern", "log_statement", file.Path, file.Language, lineOf(src, m[0]))
		props := []string{
			"signal", "log",
			"provenance", "INFERRED_FROM_OBSERVABILITY_LOG",
			"library", "elixir_logger",
			"log_level", level,
		}
		if message != "" {
			props = append(props, "message", message)
		}
		setProps(&ent, props...)
		add(ent)
	}

	// Logger.metadata(...) calls.
	for _, m := range obsLoggerMetadataRE.FindAllStringIndex(src, -1) {
		ent := makeEntity("Logger.metadata", "SCOPE.Pattern", "log_metadata", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent,
			"signal", "log",
			"provenance", "INFERRED_FROM_OBSERVABILITY_LOG",
			"library", "elixir_logger",
		)
		add(ent)
	}

	// --- metric_extraction --------------------------------------------------

	// :telemetry.execute([:a, :b], ...) event emission.
	for _, m := range obsTelemetryExecuteRE.FindAllStringSubmatchIndex(src, -1) {
		event := obsAtomList(src[m[2]:m[3]])
		if event == "" {
			continue
		}
		ent := makeEntity(event, "SCOPE.Pattern", "metric", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent,
			"signal", "metric",
			"provenance", "INFERRED_FROM_OBSERVABILITY_METRIC",
			"library", "telemetry",
			"metric_type", "telemetry_event",
			"metric_name", event,
			"telemetry_event", event,
		)
		add(ent)
	}

	// Telemetry.Metrics reporter definitions: counter/summary/last_value/...
	for _, m := range obsTelemetryMetricRE.FindAllStringSubmatchIndex(src, -1) {
		metricType := src[m[2]:m[3]]
		metricName := src[m[4]:m[5]]
		if metricName == "" {
			continue
		}
		ent := makeEntity(metricName, "SCOPE.Pattern", "metric", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent,
			"signal", "metric",
			"provenance", "INFERRED_FROM_OBSERVABILITY_METRIC",
			"library", "telemetry_metrics",
			"metric_type", metricType,
			"metric_name", metricName,
			"telemetry_event", metricName,
		)
		add(ent)
	}

	// --- trace_extraction ---------------------------------------------------

	// :telemetry.span([:a, :b], ...) span emission.
	for _, m := range obsTelemetrySpanRE.FindAllStringSubmatchIndex(src, -1) {
		event := obsAtomList(src[m[2]:m[3]])
		if event == "" {
			continue
		}
		ent := makeEntity(event, "SCOPE.Pattern", "trace_span", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent,
			"signal", "trace",
			"provenance", "INFERRED_FROM_OBSERVABILITY_TRACE",
			"library", "telemetry",
			"span_kind", "telemetry_span",
			"span_name", event,
			"telemetry_event", event,
		)
		add(ent)
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}
