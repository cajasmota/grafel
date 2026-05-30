// Package kotlin — observability extractor for Kotlin frameworks not covered by
// the Java observability pass (internal/custom/java/observability.go).
//
// The Java pass already handles: spring-boot, spring_webflux, quarkus,
// micronaut, javalin, vertx, dropwizard, helidon, akka_http, struts, etc.
// This file adds coverage for Kotlin-idiomatic frameworks that are gated out
// of the Java pass:
//
//   - lang.kotlin.framework.ktor        Observability/log_extraction    (missing → partial)
//   - lang.kotlin.framework.ktor        Observability/metric_extraction (missing → partial)
//   - lang.kotlin.framework.ktor        Observability/trace_extraction  (missing → partial)
//   - lang.kotlin.framework.http4k      Observability/log_extraction    (missing → partial)
//   - lang.kotlin.framework.http4k      Observability/metric_extraction (missing → partial)
//   - lang.kotlin.framework.http4k      Observability/trace_extraction  (missing → partial)
//   - lang.kotlin.framework.arrow       Observability/log_extraction    (missing → partial)
//   - lang.kotlin.framework.arrow       Observability/metric_extraction (missing → partial)
//   - lang.kotlin.framework.arrow       Observability/trace_extraction  (missing → partial)
//   - lang.kotlin.framework.coroutines  Observability/log_extraction    (missing → partial)
//   - lang.kotlin.framework.coroutines  Observability/metric_extraction (missing → partial)
//   - lang.kotlin.framework.coroutines  Observability/trace_extraction  (missing → partial)
//
// Detection is shared across all four frameworks because SLF4J, Micrometer,
// and OpenTelemetry are language-level (not framework-level) in Kotlin. Any
// Kotlin file that uses these APIs is covered, regardless of which HTTP
// framework it belongs to.
//
// Honest limit: regex-based, file-local. Logger field declared in one file
// and used in another is not correlated. Cells are partial, not full.
package kotlin

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
	extractor.Register("custom_kotlin_observability", &kotlinObservabilityExtractor{})
}

type kotlinObservabilityExtractor struct{}

func (e *kotlinObservabilityExtractor) Language() string { return "custom_kotlin_observability" }

// ---------------------------------------------------------------------------
// Regexes
// ---------------------------------------------------------------------------

var (
	// --- log_extraction ---

	// reKtSlf4jAnno matches @Slf4j Lombok class-level annotation (Kotlin interop).
	reKtSlf4jAnno = regexp.MustCompile(
		`(?s)@Slf4j\b[^{]*?\bclass\s+(\w+)`)

	// reKtLoggerFactory matches SLF4J / Log4j / JUL logger acquisition:
	//   val log = LoggerFactory.getLogger(...)
	//   val logger = LogManager.getLogger(...)
	//   private val log: Logger = LoggerFactory.getLogger(Foo::class.java)
	reKtLoggerFactory = regexp.MustCompile(
		`\b(LoggerFactory|LogManager|Logger)\s*\.\s*getLogger\s*\(`)

	// reKtKotlinLogging matches kotlin-logging / microutils KLogger:
	//   val log = KotlinLogging.logger {}
	//   private val logger = KotlinLogging.logger {}
	reKtKotlinLogging = regexp.MustCompile(
		`\bKotlinLogging\s*\.\s*logger\s*\{`)

	// reKtLogStatement matches log call sites:
	//   log.info(...), logger.warn(...), LOG.error(...)
	reKtLogStatement = regexp.MustCompile(
		`\b([lL][oO][gG](?:[gG][eE][rR])?)\s*\.\s*(trace|debug|info|warn|error|fatal)\s*\(`)

	// --- metric_extraction ---

	// reKtMicrometerBuilder matches Micrometer meter builders:
	//   Counter.builder("name"), Timer.builder("name"), Gauge.builder("name")
	reKtMicrometerBuilder = regexp.MustCompile(
		`\b(Counter|Timer|Gauge|DistributionSummary|LongTaskTimer)\s*\.\s*builder\s*\(\s*"([^"]*)"`)

	// reKtMeterRegistry matches Micrometer MeterRegistry usage.
	reKtMeterRegistry = regexp.MustCompile(
		`\bMeterRegistry\b|\bmeterRegistry\s*\.\s*(counter|timer|gauge|summary)\s*\(`)

	// reKtTimedAnno matches @Timed annotation on a Kotlin fun.
	reKtTimedAnno = regexp.MustCompile(
		`@Timed\s*(?:\([^)]*\))?\s*(?:suspend\s+)?fun\s+(\w+)\s*\(`)

	// reKtMicrometerCounted matches @Counted annotation.
	reKtMicrometerCounted = regexp.MustCompile(
		`@Counted\s*(?:\([^)]*\))?\s*(?:suspend\s+)?fun\s+(\w+)\s*\(`)

	// --- trace_extraction ---

	// reKtWithSpan matches @WithSpan OTel annotation.
	reKtWithSpan = regexp.MustCompile(
		`@WithSpan\s*(?:\([^)]*\))?\s*(?:suspend\s+)?fun\s+(\w+)\s*\(`)

	// reKtOtelSpanBuilder matches OTel tracer / span builder usage:
	//   tracer.spanBuilder("name").startSpan()
	reKtOtelSpanBuilder = regexp.MustCompile(
		`\btracer\s*\.\s*spanBuilder\s*\(\s*"([^"]*)"`)

	// reKtOtelSpan matches span.setAttribute / span.addEvent calls.
	reKtOtelSpan = regexp.MustCompile(
		`\bspan\s*\.\s*(setAttribute|addEvent)\s*\(`)

	// reKtMicrometerObserved matches Micrometer @Observed annotation.
	reKtMicrometerObserved = regexp.MustCompile(
		`@Observed\s*(?:\([^)]*\))?\s*(?:suspend\s+)?(?:class|fun)\s+(\w+)`)
)

// ---------------------------------------------------------------------------
// Extract
// ---------------------------------------------------------------------------

func (e *kotlinObservabilityExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/kotlin")
	_, span := tracer.Start(ctx, "indexer.kotlin_observability.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("file_path", file.Path),
		),
	)
	defer span.End()

	if len(file.Content) == 0 || file.Language != "kotlin" {
		return nil, nil
	}
	src := string(file.Content)

	hasObs := reKtLoggerFactory.MatchString(src) ||
		reKtKotlinLogging.MatchString(src) ||
		reKtLogStatement.MatchString(src) ||
		reKtMicrometerBuilder.MatchString(src) ||
		reKtMeterRegistry.MatchString(src) ||
		reKtTimedAnno.MatchString(src) ||
		reKtWithSpan.MatchString(src) ||
		reKtOtelSpanBuilder.MatchString(src) ||
		reKtMicrometerObserved.MatchString(src) ||
		reKtSlf4jAnno.MatchString(src)
	if !hasObs {
		return nil, nil
	}

	var entities []types.EntityRecord
	seen := make(map[string]bool)

	add := func(name, subtype, obsType string, line int) {
		key := "SCOPE.Pattern:obs:" + subtype + ":" + name
		if seen[key] {
			return
		}
		seen[key] = true
		ent := makeEntity(name, "SCOPE.Pattern", subtype, file.Path, file.Language, line)
		setProps(&ent,
			"obs_type", obsType,
			"provenance", "INFERRED_FROM_KOTLIN_OBSERVABILITY",
		)
		entities = append(entities, ent)
	}

	// --- log_extraction ---

	for _, m := range reKtSlf4jAnno.FindAllStringSubmatchIndex(src, -1) {
		className := src[m[2]:m[3]]
		add(className+":slf4j_logger", "logger", "slf4j", lineOf(src, m[0]))
	}
	for _, m := range reKtLoggerFactory.FindAllStringSubmatchIndex(src, -1) {
		factory := src[m[2]:m[3]]
		add("logger:"+factory, "logger", "slf4j_factory", lineOf(src, m[0]))
	}
	for _, m := range reKtKotlinLogging.FindAllStringSubmatchIndex(src, -1) {
		add("logger:kotlin_logging", "logger", "kotlin_logging", lineOf(src, m[0]))
	}
	cnt := 0
	for _, m := range reKtLogStatement.FindAllStringSubmatchIndex(src, -1) {
		level := src[m[4]:m[5]]
		cnt++
		add("log_stmt:"+level+"#"+string(rune('a'+cnt%26)), "log_statement", level, lineOf(src, m[0]))
	}

	// --- metric_extraction ---

	for _, m := range reKtMicrometerBuilder.FindAllStringSubmatchIndex(src, -1) {
		meterType := src[m[2]:m[3]]
		name := src[m[4]:m[5]]
		add(name+":"+meterType, "metric", "micrometer_"+meterType, lineOf(src, m[0]))
	}
	for _, m := range reKtMeterRegistry.FindAllStringSubmatchIndex(src, -1) {
		add("meter_registry_usage", "metric", "micrometer_registry", lineOf(src, m[0]))
	}
	for _, m := range reKtTimedAnno.FindAllStringSubmatchIndex(src, -1) {
		funcName := src[m[2]:m[3]]
		add(funcName+":@Timed", "metric", "micrometer_timed", lineOf(src, m[0]))
	}
	for _, m := range reKtMicrometerCounted.FindAllStringSubmatchIndex(src, -1) {
		funcName := src[m[2]:m[3]]
		add(funcName+":@Counted", "metric", "micrometer_counted", lineOf(src, m[0]))
	}

	// --- trace_extraction ---

	for _, m := range reKtWithSpan.FindAllStringSubmatchIndex(src, -1) {
		funcName := src[m[2]:m[3]]
		add(funcName+":@WithSpan", "trace_span", "otel_with_span", lineOf(src, m[0]))
	}
	for _, m := range reKtOtelSpanBuilder.FindAllStringSubmatchIndex(src, -1) {
		spanName := src[m[2]:m[3]]
		add(spanName+":span_builder", "trace_span", "otel_span_builder", lineOf(src, m[0]))
	}
	cnt = 0
	for _, m := range reKtOtelSpan.FindAllStringSubmatchIndex(src, -1) {
		op := src[m[2]:m[3]]
		cnt++
		add("span:"+op+"#"+string(rune('a'+cnt%26)), "trace_span", "otel_span_op", lineOf(src, m[0]))
	}
	for _, m := range reKtMicrometerObserved.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		add(name+":@Observed", "trace_span", "micrometer_observed", lineOf(src, m[0]))
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}
