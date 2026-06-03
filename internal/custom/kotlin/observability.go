// Package kotlin — observability extractor for Kotlin source.
//
// The Java observability pass (internal/custom/java/observability.go) accepts
// Kotlin sources in principle, but is only dispatched under custom_java_patterns
// which hard-skips any non-java file (patterns_dispatch.go: language != "java").
// So for Kotlin, ALL HTTP-framework observability flows through THIS extractor.
//
// Covered cells (ktor / http4k / arrow / coroutines / micronaut / quarkus):
//   - Observability/log_extraction    → partial (see honest limit below)
//   - Observability/metric_extraction → full    (literal meter names captured)
//   - Observability/trace_extraction  → full    (literal span names captured)
//
// Detection is shared across ALL Kotlin frameworks because SLF4J, Micrometer,
// and OpenTelemetry are language-level (not framework-level) in Kotlin. Any
// Kotlin file that uses these APIs is covered, regardless of which HTTP
// framework it belongs to:
//   - Micronaut: Micrometer @Timed/@Counted + meterRegistry.counter(...),
//     Micronaut Tracing @NewSpan/@ContinueSpan, SLF4J LoggerFactory.getLogger.
//   - Quarkus: Micrometer / MicroProfile Metrics @Timed/@Counted,
//     OpenTelemetry @WithSpan, SLF4J/JBoss logging.
//
// Name capture (the deepened part — CORE issue #3438):
//
//	metric: Counter/Timer/Gauge.builder("name"), <registry>.counter/timer/
//	        gauge/summary("name"), @Timed("name")/@Counted("name") (literal
//	        arg captured; falls back to fun name with name_source provenance).
//	trace:  tracer.spanBuilder("name"), @WithSpan("name"), @NewSpan("name"),
//	        @Observed(name="name") (literal arg captured; falls back to
//	        fun/class name with name_source provenance).
//
// Because the meter/span NAME is a literal at the call site, no cross-file
// resolution is needed to assert it — so metric/trace are honest-full, on the
// same bar as Java spring-boot (internal/custom/java/observability.go).
//
// Honest limit (log stays partial): regex-based, file-local. A logger field
// declared in one file and used in another is not correlated, and log message
// strings are frequently interpolated/dynamic ("user {}", $id). This matches
// the cross-file dataflow gap held partial for Java/PHP/Rust observability.
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

	// reKtLogLambda matches kotlin-logging lazy-message lambda call sites:
	//   log.info { "..." }, logger.debug { "..." }
	// Group 1 = level, group 2 = literal message head (if present).
	reKtLogLambda = regexp.MustCompile(
		`\b[lL][oO][gG](?:[gG][eE][rR])?\s*\.\s*(trace|debug|info|warn|error)\s*\{\s*"([^"$]*)`)

	// reKtKotlinLoggingVal matches the kotlin-logging val binding so the logger
	// name (the val identifier) is captured, not just the factory:
	//   private val log = KotlinLogging.logger {}
	//   val auditLogger = KotlinLogging.logger {}
	reKtKotlinLoggingVal = regexp.MustCompile(
		`\bval\s+(\w+)\s*(?::[^=]+)?=\s*KotlinLogging\s*\.\s*logger\s*\{`)

	// --- metric_extraction ---

	// reKtMicrometerBuilder matches Micrometer meter builders:
	//   Counter.builder("name"), Timer.builder("name"), Gauge.builder("name")
	reKtMicrometerBuilder = regexp.MustCompile(
		`\b(Counter|Timer|Gauge|DistributionSummary|LongTaskTimer)\s*\.\s*builder\s*\(\s*"([^"]*)"`)

	// reKtMeterRegistryName matches a Micrometer registry meter call WITH a
	// literal metric name at the call site:
	//   meterRegistry.counter("api.requests"), registry.timer("api.latency")
	// Group 1 = meter kind, group 2 = literal metric name.
	reKtMeterRegistryName = regexp.MustCompile(
		`\b\w*[rR]egistry\s*\.\s*(counter|timer|gauge|summary)\s*\(\s*"([^"]*)"`)

	// reKtMeterRegistry matches bare Micrometer MeterRegistry usage (no literal
	// name available — partial signal only).
	reKtMeterRegistry = regexp.MustCompile(
		`\bMeterRegistry\b|\bmeterRegistry\s*\.\s*(counter|timer|gauge|summary)\s*\(`)

	// reKtTimedAnno matches @Timed annotation on a Kotlin fun, capturing the
	// optional literal metric name from the annotation argument:
	//   @Timed("api.findUser") fun findUser(...)
	//   @Timed(value = "api.findUser") suspend fun findUser(...)
	//   @Timed fun findUser(...)   (no literal name → falls back to fun name)
	// Group 1 = literal metric name (may be empty), group 2 = fun name.
	reKtTimedAnno = regexp.MustCompile(
		`(?s)@Timed\s*(?:\(\s*(?:value\s*=\s*)?(?:"([^"]*)")?[^)]*\))?\s*` +
			`(?:@\w+\s*(?:\([^)]*\))?\s*)*(?:suspend\s+)?fun\s+(\w+)\s*\(`)

	// reKtMicrometerCounted matches @Counted annotation, capturing the optional
	// literal metric name. Group 1 = literal name (may be empty), group 2 = fun.
	reKtMicrometerCounted = regexp.MustCompile(
		`(?s)@Counted\s*(?:\(\s*(?:value\s*=\s*)?(?:"([^"]*)")?[^)]*\))?\s*` +
			`(?:@\w+\s*(?:\([^)]*\))?\s*)*(?:suspend\s+)?fun\s+(\w+)\s*\(`)

	// --- trace_extraction ---

	// reKtWithSpan matches the OTel @WithSpan annotation, capturing the optional
	// literal span name:
	//   @WithSpan("processOrder") fun processOrder(...)
	//   @WithSpan(value = "processOrder") suspend fun processOrder(...)
	//   @WithSpan fun processOrder(...)   (defaults span name to fun name)
	// Group 1 = literal span name (may be empty), group 2 = fun name.
	reKtWithSpan = regexp.MustCompile(
		`(?s)@WithSpan\s*(?:\(\s*(?:value\s*=\s*)?(?:"([^"]*)")?[^)]*\))?\s*` +
			`(?:@\w+\s*(?:\([^)]*\))?\s*)*(?:suspend\s+)?fun\s+(\w+)\s*\(`)

	// reKtNewSpan matches the Spring Cloud Sleuth / Micrometer @NewSpan
	// annotation, capturing the optional literal span name.
	// Group 1 = literal span name (may be empty), group 2 = fun name.
	reKtNewSpan = regexp.MustCompile(
		`(?s)@NewSpan\s*(?:\(\s*(?:(?:name|value)\s*=\s*)?(?:"([^"]*)")?[^)]*\))?\s*` +
			`(?:@\w+\s*(?:\([^)]*\))?\s*)*(?:suspend\s+)?fun\s+(\w+)\s*\(`)

	// reKtOtelSpanBuilder matches OTel tracer / span builder usage:
	//   tracer.spanBuilder("name").startSpan()
	// Accepts any *tracer receiver so injected tracer fields are covered.
	reKtOtelSpanBuilder = regexp.MustCompile(
		`\b\w*[tT]racer\s*\.\s*spanBuilder\s*\(\s*"([^"]*)"`)

	// reKtOtelSpan matches span.setAttribute / span.addEvent calls.
	reKtOtelSpan = regexp.MustCompile(
		`\bspan\s*\.\s*(setAttribute|addEvent)\s*\(`)

	// reKtMicrometerObserved matches the Micrometer Tracing @Observed
	// annotation, capturing the optional literal observation name:
	//   @Observed(name = "order.process") fun process(...)
	// Group 1 = literal name (may be empty), group 2 = class/fun name.
	reKtMicrometerObserved = regexp.MustCompile(
		`(?s)@Observed\s*(?:\(\s*(?:name\s*=\s*)?(?:"([^"]*)")?[^)]*\))?\s*` +
			`(?:@\w+\s*(?:\([^)]*\))?\s*)*(?:suspend\s+)?(?:class|fun)\s+(\w+)`)
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
		reKtLogLambda.MatchString(src) ||
		reKtMicrometerBuilder.MatchString(src) ||
		reKtMeterRegistry.MatchString(src) ||
		reKtTimedAnno.MatchString(src) ||
		reKtWithSpan.MatchString(src) ||
		reKtNewSpan.MatchString(src) ||
		reKtOtelSpanBuilder.MatchString(src) ||
		reKtMicrometerObserved.MatchString(src) ||
		reKtSlf4jAnno.MatchString(src)
	if !hasObs {
		return nil, nil
	}

	var entities []types.EntityRecord
	seen := make(map[string]bool)

	add := func(name, subtype, obsType string, line int, extra ...string) {
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
		setProps(&ent, extra...)
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
	// kotlin-logging val binding: capture the logger's val identifier as name.
	loggerVals := reKtKotlinLoggingVal.FindAllStringSubmatchIndex(src, -1)
	for _, m := range loggerVals {
		valName := src[m[2]:m[3]]
		add("logger:"+valName, "logger", "kotlin_logging", lineOf(src, m[0]),
			"logger_name", valName)
	}
	// Bare KotlinLogging.logger {} acquisitions without a captured val binding.
	if len(loggerVals) == 0 {
		for _, m := range reKtKotlinLogging.FindAllStringSubmatchIndex(src, -1) {
			add("logger:kotlin_logging", "logger", "kotlin_logging", lineOf(src, m[0]))
		}
	}
	cnt := 0
	for _, m := range reKtLogStatement.FindAllStringSubmatchIndex(src, -1) {
		level := src[m[4]:m[5]]
		cnt++
		add("log_stmt:"+level+"#"+string(rune('a'+cnt%26)), "log_statement", level, lineOf(src, m[0]))
	}
	// kotlin-logging lazy lambda log sites: log.info { "literal head..." }.
	for _, m := range reKtLogLambda.FindAllStringSubmatchIndex(src, -1) {
		level := src[m[2]:m[3]]
		msg := src[m[4]:m[5]]
		cnt++
		extra := []string{}
		if msg != "" {
			extra = append(extra, "message", msg)
		}
		add("log_lambda:"+level+"#"+string(rune('a'+cnt%26)), "log_statement", level+"_lambda", lineOf(src, m[0]), extra...)
	}

	// --- metric_extraction ---

	for _, m := range reKtMicrometerBuilder.FindAllStringSubmatchIndex(src, -1) {
		meterType := src[m[2]:m[3]]
		name := src[m[4]:m[5]]
		add(name+":"+meterType, "metric", "micrometer_"+meterType, lineOf(src, m[0]),
			"metric_name", name)
	}
	// Registry meter calls WITH a literal name: meterRegistry.counter("name").
	for _, m := range reKtMeterRegistryName.FindAllStringSubmatchIndex(src, -1) {
		meterKind := src[m[2]:m[3]]
		name := src[m[4]:m[5]]
		add(name+":registry_"+meterKind, "metric", "micrometer_"+meterKind, lineOf(src, m[0]),
			"metric_name", name)
	}
	for _, m := range reKtMeterRegistry.FindAllStringSubmatchIndex(src, -1) {
		add("meter_registry_usage", "metric", "micrometer_registry", lineOf(src, m[0]))
	}
	for _, m := range reKtTimedAnno.FindAllStringSubmatchIndex(src, -1) {
		metricName := grp(src, m, 1)
		funcName := grp(src, m, 2)
		name := metricName
		if name == "" {
			name = funcName
		}
		add(name+":@Timed", "metric", "micrometer_timed", lineOf(src, m[0]),
			"metric_name", name, "metric_name_source", nameSource(metricName))
	}
	for _, m := range reKtMicrometerCounted.FindAllStringSubmatchIndex(src, -1) {
		metricName := grp(src, m, 1)
		funcName := grp(src, m, 2)
		name := metricName
		if name == "" {
			name = funcName
		}
		add(name+":@Counted", "metric", "micrometer_counted", lineOf(src, m[0]),
			"metric_name", name, "metric_name_source", nameSource(metricName))
	}

	// --- trace_extraction ---

	for _, m := range reKtWithSpan.FindAllStringSubmatchIndex(src, -1) {
		spanName := grp(src, m, 1)
		funcName := grp(src, m, 2)
		name := spanName
		if name == "" {
			name = funcName
		}
		add(name+":@WithSpan", "trace_span", "otel_with_span", lineOf(src, m[0]),
			"span_name", name, "span_name_source", nameSource(spanName))
	}
	for _, m := range reKtNewSpan.FindAllStringSubmatchIndex(src, -1) {
		spanName := grp(src, m, 1)
		funcName := grp(src, m, 2)
		name := spanName
		if name == "" {
			name = funcName
		}
		add(name+":@NewSpan", "trace_span", "sleuth_new_span", lineOf(src, m[0]),
			"span_name", name, "span_name_source", nameSource(spanName))
	}
	for _, m := range reKtOtelSpanBuilder.FindAllStringSubmatchIndex(src, -1) {
		spanName := src[m[2]:m[3]]
		add(spanName+":span_builder", "trace_span", "otel_span_builder", lineOf(src, m[0]),
			"span_name", spanName, "span_name_source", "literal")
	}
	cnt = 0
	for _, m := range reKtOtelSpan.FindAllStringSubmatchIndex(src, -1) {
		op := src[m[2]:m[3]]
		cnt++
		add("span:"+op+"#"+string(rune('a'+cnt%26)), "trace_span", "otel_span_op", lineOf(src, m[0]))
	}
	for _, m := range reKtMicrometerObserved.FindAllStringSubmatchIndex(src, -1) {
		obsName := grp(src, m, 1)
		declName := grp(src, m, 2)
		name := obsName
		if name == "" {
			name = declName
		}
		add(name+":@Observed", "trace_span", "micrometer_observed", lineOf(src, m[0]),
			"span_name", name, "span_name_source", nameSource(obsName))
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}

// grp returns submatch group g for match m, or "" when the group did not
// participate (FindAllStringSubmatchIndex returns -1,-1 for optional groups).
func grp(src string, m []int, g int) string {
	if 2*g+1 >= len(m) || m[2*g] < 0 || m[2*g+1] < 0 {
		return ""
	}
	return src[m[2*g]:m[2*g+1]]
}

// nameSource reports whether a metric/span name was a literal annotation
// argument ("literal") or was defaulted to the enclosing fun/class name
// ("defaulted_to_decl"). Used to keep provenance honest for downstream
// consumers: only "literal" names are call-site-asserted.
func nameSource(literal string) string {
	if literal == "" {
		return "defaulted_to_decl"
	}
	return "literal"
}
