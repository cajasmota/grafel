package java

import "regexp"

// Java observability custom extractor: structured logging, metrics, and
// distributed tracing instrumentation across the JVM backend frameworks
// (#3006, epic #2847).
//
// Covers the Observability lane:
//   - log_extraction: SLF4J / Log4j / java.util.logging logger acquisition
//     (@Slf4j, LoggerFactory.getLogger, LogManager.getLogger,
//     Logger.getLogger) and the log statement call surface
//     (log.info/debug/warn/error/trace(...)). Emits a SCOPE.Pattern
//     (subtype=log_statement) per log call carrying log_level + framework,
//     plus a SCOPE.Pattern(subtype=logger) per declared logger.
//   - metric_extraction: Micrometer (@Timed, Counter/Timer/Gauge.builder,
//     MeterRegistry) and MicroProfile Metrics (@Counted/@Timed/@Metered/
//     @Gauge). Emits a SCOPE.Pattern(subtype=metric) carrying metric_type +
//     framework.
//   - trace_extraction: OpenTelemetry (@WithSpan, tracer.spanBuilder,
//     span.setAttribute) and Micrometer Tracing (@Observed, Tracer.nextSpan).
//     Emits a SCOPE.Pattern(subtype=trace_span) carrying span_kind + framework.
//
// Reuses the SCOPE.Pattern entity Kind (matching spring_aop.go /
// transactional.go / microprofile.go) so no new entity Kind is registered, per
// the #2839 "prefer decorating over inventing Kinds" discipline. The
// observability role is recorded in the entity Subtype and `kind` property.
//
// This is regex-based, file-local detection (no cross-file dataflow), so the
// registry cells are flipped to `partial`, not `full` — honest about the limit
// (a logger imported in one file and used in another is not correlated, and we
// do not resolve the metric/span *name* expression when it is a non-literal).

// obsFrameworks gates the JVM backend frameworks for which observability
// extraction runs. These are the http_framework / microprofile records that
// carry the log/metric/trace cells in the coverage registry.
// Kotlin frameworks are included because SLF4J/Micrometer/OTel annotations
// and logger patterns are identical in Kotlin source.
var obsFrameworks = map[string]bool{
	"spring_boot": true, "spring-boot": true, "springboot": true,
	"spring_webflux": true, "spring-webflux": true, "springwebflux": true,
	"quarkus":      true,
	"micronaut":    true,
	"microprofile": true,
	"jakarta_ee":   true, "jakarta-ee": true, "jakartaee": true,
	"jaxrs": true, "jax-rs": true, "jax_rs": true,
	"dropwizard": true,
	"helidon":    true,
	"javalin":    true,
	"vertx":      true, "vert.x": true,
	"akka_http": true, "akka-http": true,
	"struts":       true,
	"open_liberty": true, "payara": true,
}

var (
	// --- log_extraction ---

	// obsLoggerSlf4jAnnoRE matches the Lombok @Slf4j class-level annotation,
	// capturing the annotated class name. @Slf4j injects a `log` field, so the
	// declaring class is the logger holder.
	obsLoggerSlf4jAnnoRE = regexp.MustCompile(
		`(?s)@Slf4j\b[^{]*?\bclass\s+(\w+)`)

	// obsLoggerFactoryRE matches an explicit logger field acquired via SLF4J
	// LoggerFactory.getLogger, Log4j2 LogManager.getLogger, or JUL
	// Logger.getLogger. Captures the factory call site so we can derive the
	// framework, plus the field name (group 2).
	obsLoggerFactoryRE = regexp.MustCompile(
		`\b(LoggerFactory|LogManager|Logger)\.getLogger\s*\(`)

	// obsLoggerFieldRE matches `... Logger <name> = ...getLogger(...)` to name
	// the logger field. Used after a getLogger call site is found on a line.
	obsLoggerFieldRE = regexp.MustCompile(
		`\bLogger\s+(\w+)\s*=`)

	// obsLogStmtRE matches a log statement call: a receiver identifier ending in
	// a log level method. We anchor on a small set of receiver names commonly
	// used for loggers (log, logger, LOG, LOGGER, <Class>.log) to avoid matching
	// unrelated `.error(` style calls. Group 1 = receiver, group 2 = level.
	obsLogStmtRE = regexp.MustCompile(
		`\b([lL][oO][gG](?:[gG][eE][rR])?)\s*\.\s*(trace|debug|info|warn|error|fatal)\s*\(`)

	// --- metric_extraction ---

	// obsMicrometerBuilderRE matches Micrometer meter construction:
	// Counter.builder(...), Timer.builder(...), Gauge.builder(...),
	// DistributionSummary.builder(...). Group 1 = meter type.
	obsMicrometerBuilderRE = regexp.MustCompile(
		`\b(Counter|Timer|Gauge|DistributionSummary|LongTaskTimer)\.builder\s*\(\s*"([^"]*)"`)

	// obsMeterRegistryRE matches use of a Micrometer MeterRegistry: the type
	// reference or a `.counter(` / `.timer(` / `.gauge(` registry call.
	obsMeterRegistryRE = regexp.MustCompile(
		`\bMeterRegistry\b|\bmeterRegistry\s*\.\s*(?:counter|timer|gauge|summary)\s*\(`)

	// obsTimedAnnoRE matches the @Timed metric annotation (Micrometer or
	// MicroProfile) on a method, capturing the method name. Optional value
	// attribute string is captured as group 1 (metric name) when present.
	obsTimedAnnoRE = regexp.MustCompile(
		`(?s)@Timed\s*(?:\(\s*(?:value\s*=\s*)?(?:"([^"]*)")?[^)]*\))?\s*` +
			`(?:(?:@\w+(?:\s*\([^)]*\))?)\s*)*` +
			`(?:(?:public|protected|private)\s+)?(?:static\s+)?(?:final\s+)?(?:<[^>]*>\s*)?` +
			`(?:[\w.]+(?:\s*<[^>]*>)?(?:\[\])?\s+)(\w+)\s*\(`)

	// obsMetricAnnoRE matches the remaining MicroProfile / Micrometer metric
	// annotations (@Counted, @Metered, @Gauge) on a method. Group 1 = annotation
	// name, group 2 = optional metric-name string, group 3 = method name.
	obsMetricAnnoRE = regexp.MustCompile(
		`(?s)@(Counted|Metered|Gauge)\s*(?:\(\s*(?:value\s*=\s*)?(?:"([^"]*)")?[^)]*\))?\s*` +
			`(?:(?:@\w+(?:\s*\([^)]*\))?)\s*)*` +
			`(?:(?:public|protected|private)\s+)?(?:static\s+)?(?:final\s+)?(?:<[^>]*>\s*)?` +
			`(?:[\w.]+(?:\s*<[^>]*>)?(?:\[\])?\s+)(\w+)\s*\(`)

	// --- trace_extraction ---

	// obsWithSpanRE matches the OpenTelemetry @WithSpan annotation on a method,
	// capturing an optional span-name string (group 1) and the method name
	// (group 2). @WithSpan with no value defaults the span name to the method.
	obsWithSpanRE = regexp.MustCompile(
		`(?s)@WithSpan\s*(?:\(\s*(?:value\s*=\s*)?(?:"([^"]*)")?[^)]*\))?\s*` +
			`(?:(?:@\w+(?:\s*\([^)]*\))?)\s*)*` +
			`(?:(?:public|protected|private)\s+)?(?:static\s+)?(?:final\s+)?(?:<[^>]*>\s*)?` +
			`(?:[\w.]+(?:\s*<[^>]*>)?(?:\[\])?\s+)(\w+)\s*\(`)

	// obsObservedRE matches the Micrometer Tracing @Observed annotation on a
	// method (group 1 = optional name string, group 2 = method name).
	obsObservedRE = regexp.MustCompile(
		`(?s)@Observed\s*(?:\(\s*(?:name\s*=\s*)?(?:"([^"]*)")?[^)]*\))?\s*` +
			`(?:(?:@\w+(?:\s*\([^)]*\))?)\s*)*` +
			`(?:(?:public|protected|private)\s+)?(?:static\s+)?(?:final\s+)?(?:<[^>]*>\s*)?` +
			`(?:[\w.]+(?:\s*<[^>]*>)?(?:\[\])?\s+)(\w+)\s*\(`)

	// obsSpanBuilderRE matches a programmatic OpenTelemetry span:
	// tracer.spanBuilder("name") . Group 1 = span name.
	obsSpanBuilderRE = regexp.MustCompile(
		`\.\s*spanBuilder\s*\(\s*"([^"]*)"`)

	// obsNextSpanRE matches Micrometer Tracing programmatic span creation:
	// tracer.nextSpan() / Tracer.nextSpan().
	obsNextSpanRE = regexp.MustCompile(
		`\.\s*nextSpan\s*\(\s*\)`)
)

// canonicalObsFramework normalises a framework alias to its canonical name for
// the entity `framework` property.
func canonicalObsFramework(framework string) string {
	switch framework {
	case "spring_boot", "spring-boot", "springboot":
		return "spring_boot"
	case "spring_webflux", "spring-webflux", "springwebflux":
		return "spring_webflux"
	case "jakarta_ee", "jakarta-ee", "jakartaee":
		return "jakarta_ee"
	case "jaxrs", "jax-rs", "jax_rs":
		return "jaxrs"
	case "akka_http", "akka-http":
		return "akka_http"
	case "vertx", "vert.x":
		return "vertx"
	default:
		return framework
	}
}

// ExtractObservability runs the Java/Kotlin observability extractor.
// Accepts both Java and Kotlin source: SLF4J, Micrometer, and OTel patterns
// (LoggerFactory.getLogger, @Timed, @WithSpan, etc.) are syntactically
// identical in Kotlin files.
func ExtractObservability(ctx PatternContext) PatternResult {
	var result PatternResult
	if (ctx.Language != "java" && ctx.Language != "kotlin") || !obsFrameworks[ctx.Framework] {
		return result
	}

	source := ctx.Source
	fp := ctx.FilePath
	fw := canonicalObsFramework(ctx.Framework)
	seenRefs := make(map[string]bool)

	extractLogging(&result, seenRefs, source, fp, fw)
	extractMetrics(&result, seenRefs, source, fp, fw)
	extractTracing(&result, seenRefs, source, fp, fw)

	return result
}

// logLibraryFor maps a logger factory call name to the backing framework.
func logLibraryFor(factory string) string {
	switch factory {
	case "LoggerFactory":
		return "slf4j"
	case "LogManager":
		return "log4j"
	case "Logger":
		return "jul"
	default:
		return "slf4j"
	}
}

// extractLogging handles the log_extraction cell.
func extractLogging(result *PatternResult, seen map[string]bool, source, fp, fw string) {
	// @Slf4j-annotated classes declare an injected `log` logger.
	for _, m := range obsLoggerSlf4jAnnoRE.FindAllStringSubmatchIndex(source, -1) {
		className := source[m[2]:m[3]]
		ref := "scope:pattern:obs_logger:" + fp + ":" + className
		addEntity(result, seen, SecondaryEntity{
			Name: className, Kind: "SCOPE.Pattern", Subtype: "logger",
			SourceFile: fp,
			LineStart:  lineOf(source, m[0]), LineEnd: lineOf(source, m[0]),
			Provenance: "INFERRED_FROM_OBSERVABILITY_LOG", Ref: ref,
			Properties: map[string]any{
				"kind":      "logger",
				"signal":    "log",
				"library":   "slf4j",
				"holder":    className,
				"framework": fw,
			},
		})
	}

	// Explicit logger fields via *.getLogger(...).
	for _, m := range obsLoggerFactoryRE.FindAllStringSubmatchIndex(source, -1) {
		factory := source[m[2]:m[3]]
		library := logLibraryFor(factory)
		// Resolve the logger field name from a bounded window around the call.
		name := obsFieldNameNear(source, m[0])
		entName := name
		if entName == "" {
			entName = library + "_logger"
		}
		ref := "scope:pattern:obs_logger:" + fp + ":" + library + ":" + entName
		props := map[string]any{
			"kind":      "logger",
			"signal":    "log",
			"library":   library,
			"framework": fw,
		}
		if name != "" {
			props["field"] = name
		}
		addEntity(result, seen, SecondaryEntity{
			Name: entName, Kind: "SCOPE.Pattern", Subtype: "logger",
			SourceFile: fp,
			LineStart:  lineOf(source, m[0]), LineEnd: lineOf(source, m[0]),
			Provenance: "INFERRED_FROM_OBSERVABILITY_LOG", Ref: ref,
			Properties: props,
		})
	}

	// Log statement calls: log.info(...), logger.error(...), etc.
	for _, m := range obsLogStmtRE.FindAllStringSubmatchIndex(source, -1) {
		receiver := source[m[2]:m[3]]
		level := source[m[4]:m[5]]
		line := lineOf(source, m[0])
		// Distinguish multiple statements on the same logger by line number.
		ref := "scope:pattern:obs_log_statement:" + fp + ":" + receiver + ":" + level + ":" + itoa(line)
		addEntity(result, seen, SecondaryEntity{
			Name: receiver + "." + level, Kind: "SCOPE.Pattern", Subtype: "log_statement",
			SourceFile: fp,
			LineStart:  line, LineEnd: line,
			Provenance: "INFERRED_FROM_OBSERVABILITY_LOG", Ref: ref,
			Properties: map[string]any{
				"kind":      "log_statement",
				"signal":    "log",
				"log_level": level,
				"receiver":  receiver,
				"framework": fw,
			},
		})
	}
}

// extractMetrics handles the metric_extraction cell.
func extractMetrics(result *PatternResult, seen map[string]bool, source, fp, fw string) {
	// Micrometer builder calls: Counter.builder("name"), Timer.builder("name").
	for _, m := range obsMicrometerBuilderRE.FindAllStringSubmatchIndex(source, -1) {
		meterType := source[m[2]:m[3]]
		metricName := source[m[4]:m[5]]
		ref := "scope:pattern:obs_metric:" + fp + ":micrometer:" + metricName
		addEntity(result, seen, SecondaryEntity{
			Name: metricName, Kind: "SCOPE.Pattern", Subtype: "metric",
			SourceFile: fp,
			LineStart:  lineOf(source, m[0]), LineEnd: lineOf(source, m[0]),
			Provenance: "INFERRED_FROM_OBSERVABILITY_METRIC", Ref: ref,
			Properties: map[string]any{
				"kind":        "metric",
				"signal":      "metric",
				"metric_type": obsMeterType(meterType),
				"metric_name": metricName,
				"library":     "micrometer",
				"framework":   fw,
			},
		})
	}

	// MeterRegistry usage (type ref or registry call) — a file-level signal that
	// metrics are wired even when no literal builder name is present.
	if obsMeterRegistryRE.MatchString(source) {
		loc := obsMeterRegistryRE.FindStringIndex(source)
		ref := "scope:pattern:obs_metric:" + fp + ":micrometer:meter_registry"
		addEntity(result, seen, SecondaryEntity{
			Name: "MeterRegistry", Kind: "SCOPE.Pattern", Subtype: "metric",
			SourceFile: fp,
			LineStart:  lineOf(source, loc[0]), LineEnd: lineOf(source, loc[0]),
			Provenance: "INFERRED_FROM_OBSERVABILITY_METRIC", Ref: ref,
			Properties: map[string]any{
				"kind":        "metric",
				"signal":      "metric",
				"metric_type": "registry",
				"library":     "micrometer",
				"framework":   fw,
			},
		})
	}

	// @Timed annotation (Micrometer + MicroProfile share the name).
	for _, m := range obsTimedAnnoRE.FindAllStringSubmatchIndex(source, -1) {
		metricName := ""
		if m[2] >= 0 {
			metricName = source[m[2]:m[3]]
		}
		methodName := source[m[4]:m[5]]
		name := metricName
		if name == "" {
			name = methodName
		}
		ref := "scope:pattern:obs_metric:" + fp + ":timed:" + methodName
		props := map[string]any{
			"kind":        "metric",
			"signal":      "metric",
			"metric_type": "timer",
			"library":     "annotation",
			"method":      methodName,
			"framework":   fw,
		}
		if metricName != "" {
			props["metric_name"] = metricName
		}
		addEntity(result, seen, SecondaryEntity{
			Name: name, Kind: "SCOPE.Pattern", Subtype: "metric",
			SourceFile: fp,
			LineStart:  lineOf(source, m[0]), LineEnd: lineOf(source, m[0]),
			Provenance: "INFERRED_FROM_OBSERVABILITY_METRIC", Ref: ref,
			Properties: props,
		})
	}

	// @Counted / @Metered / @Gauge annotations.
	for _, m := range obsMetricAnnoRE.FindAllStringSubmatchIndex(source, -1) {
		anno := source[m[2]:m[3]]
		metricName := ""
		if m[4] >= 0 {
			metricName = source[m[4]:m[5]]
		}
		methodName := source[m[6]:m[7]]
		name := metricName
		if name == "" {
			name = methodName
		}
		ref := "scope:pattern:obs_metric:" + fp + ":" + obsMetricAnnoType(anno) + ":" + methodName
		props := map[string]any{
			"kind":        "metric",
			"signal":      "metric",
			"metric_type": obsMetricAnnoType(anno),
			"library":     "annotation",
			"method":      methodName,
			"framework":   fw,
		}
		if metricName != "" {
			props["metric_name"] = metricName
		}
		addEntity(result, seen, SecondaryEntity{
			Name: name, Kind: "SCOPE.Pattern", Subtype: "metric",
			SourceFile: fp,
			LineStart:  lineOf(source, m[0]), LineEnd: lineOf(source, m[0]),
			Provenance: "INFERRED_FROM_OBSERVABILITY_METRIC", Ref: ref,
			Properties: props,
		})
	}
}

// extractTracing handles the trace_extraction cell.
func extractTracing(result *PatternResult, seen map[string]bool, source, fp, fw string) {
	// @WithSpan (OpenTelemetry).
	for _, m := range obsWithSpanRE.FindAllStringSubmatchIndex(source, -1) {
		spanName := ""
		if m[2] >= 0 {
			spanName = source[m[2]:m[3]]
		}
		methodName := source[m[4]:m[5]]
		name := spanName
		if name == "" {
			name = methodName
		}
		ref := "scope:pattern:obs_trace_span:" + fp + ":withspan:" + methodName
		props := map[string]any{
			"kind":      "trace_span",
			"signal":    "trace",
			"span_kind": "annotation",
			"library":   "otel",
			"method":    methodName,
			"framework": fw,
		}
		if spanName != "" {
			props["span_name"] = spanName
		}
		addEntity(result, seen, SecondaryEntity{
			Name: name, Kind: "SCOPE.Pattern", Subtype: "trace_span",
			SourceFile: fp,
			LineStart:  lineOf(source, m[0]), LineEnd: lineOf(source, m[0]),
			Provenance: "INFERRED_FROM_OBSERVABILITY_TRACE", Ref: ref,
			Properties: props,
		})
	}

	// @Observed (Micrometer Tracing).
	for _, m := range obsObservedRE.FindAllStringSubmatchIndex(source, -1) {
		spanName := ""
		if m[2] >= 0 {
			spanName = source[m[2]:m[3]]
		}
		methodName := source[m[4]:m[5]]
		name := spanName
		if name == "" {
			name = methodName
		}
		ref := "scope:pattern:obs_trace_span:" + fp + ":observed:" + methodName
		props := map[string]any{
			"kind":      "trace_span",
			"signal":    "trace",
			"span_kind": "annotation",
			"library":   "micrometer",
			"method":    methodName,
			"framework": fw,
		}
		if spanName != "" {
			props["span_name"] = spanName
		}
		addEntity(result, seen, SecondaryEntity{
			Name: name, Kind: "SCOPE.Pattern", Subtype: "trace_span",
			SourceFile: fp,
			LineStart:  lineOf(source, m[0]), LineEnd: lineOf(source, m[0]),
			Provenance: "INFERRED_FROM_OBSERVABILITY_TRACE", Ref: ref,
			Properties: props,
		})
	}

	// Programmatic OTel spans: tracer.spanBuilder("name").
	for _, m := range obsSpanBuilderRE.FindAllStringSubmatchIndex(source, -1) {
		spanName := source[m[2]:m[3]]
		ref := "scope:pattern:obs_trace_span:" + fp + ":otel:" + spanName
		addEntity(result, seen, SecondaryEntity{
			Name: spanName, Kind: "SCOPE.Pattern", Subtype: "trace_span",
			SourceFile: fp,
			LineStart:  lineOf(source, m[0]), LineEnd: lineOf(source, m[0]),
			Provenance: "INFERRED_FROM_OBSERVABILITY_TRACE", Ref: ref,
			Properties: map[string]any{
				"kind":      "trace_span",
				"signal":    "trace",
				"span_kind": "programmatic",
				"library":   "otel",
				"span_name": spanName,
				"framework": fw,
			},
		})
	}

	// Programmatic Micrometer Tracing spans: tracer.nextSpan().
	if obsNextSpanRE.MatchString(source) {
		loc := obsNextSpanRE.FindStringIndex(source)
		ref := "scope:pattern:obs_trace_span:" + fp + ":micrometer:next_span"
		addEntity(result, seen, SecondaryEntity{
			Name: "nextSpan", Kind: "SCOPE.Pattern", Subtype: "trace_span",
			SourceFile: fp,
			LineStart:  lineOf(source, loc[0]), LineEnd: lineOf(source, loc[0]),
			Provenance: "INFERRED_FROM_OBSERVABILITY_TRACE", Ref: ref,
			Properties: map[string]any{
				"kind":      "trace_span",
				"signal":    "trace",
				"span_kind": "programmatic",
				"library":   "micrometer",
				"framework": fw,
			},
		})
	}
}

// obsMeterType normalises a Micrometer builder type to a metric_type value.
func obsMeterType(meterType string) string {
	switch meterType {
	case "Counter":
		return "counter"
	case "Timer", "LongTaskTimer":
		return "timer"
	case "Gauge":
		return "gauge"
	case "DistributionSummary":
		return "summary"
	default:
		return meterType
	}
}

// obsMetricAnnoType normalises a metric annotation name to a metric_type value.
func obsMetricAnnoType(anno string) string {
	switch anno {
	case "Counted":
		return "counter"
	case "Metered":
		return "meter"
	case "Gauge":
		return "gauge"
	default:
		return anno
	}
}

// obsFieldNameNear resolves the logger field name declared on the same
// statement as a *.getLogger(...) call site. It scans backward to the start of
// the statement (previous `;`, `{`, or `}` or start of file) and looks for a
// `Logger <name> =` declaration. Returns "" when the call is not part of a
// field declaration (e.g. an inline `LoggerFactory.getLogger(x).info(...)`).
func obsFieldNameNear(source string, callOffset int) string {
	start := callOffset
	for start > 0 {
		c := source[start-1]
		if c == ';' || c == '{' || c == '}' || c == '\n' {
			break
		}
		start--
	}
	stmt := source[start:callOffset]
	if m := obsLoggerFieldRE.FindStringSubmatch(stmt); m != nil {
		return m[1]
	}
	return ""
}
