package kotlin_test

import (
	"testing"
)

// observability_test.go: tests for custom_kotlin_observability extractor.
// Registry targets (all missing → partial):
//   lang.kotlin.framework.ktor       Observability/{log,metric,trace}_extraction
//   lang.kotlin.framework.http4k     Observability/{log,metric,trace}_extraction
//   lang.kotlin.framework.arrow      Observability/{log,metric,trace}_extraction
//   lang.kotlin.framework.coroutines Observability/{log,metric,trace}_extraction

const obsLogSrc = `
package com.example

import org.slf4j.LoggerFactory
import io.github.microutils.kotlinlogging.KotlinLogging

private val log = LoggerFactory.getLogger(UserService::class.java)
private val logger = KotlinLogging.logger {}

class UserService {
    fun findUser(id: Long) {
        log.info("finding user {}", id)
        log.warn("slow query for user {}", id)
        logger.debug { "debug detail: $id" }
    }
}
`

const obsMetricSrc = `
package com.example

import io.micrometer.core.instrument.Counter
import io.micrometer.core.instrument.Timer
import io.micrometer.core.annotation.Timed

class MetricService(private val meterRegistry: io.micrometer.core.instrument.MeterRegistry) {
    private val counter = Counter.builder("api.requests").register(meterRegistry)
    private val timer   = Timer.builder("api.latency").register(meterRegistry)

    @Timed("api.findUser")
    fun findUser(id: Long): String = "user_$id"
}
`

const obsTraceSrc = `
package com.example

import io.opentelemetry.instrumentation.annotations.WithSpan
import io.opentelemetry.api.trace.Tracer

class TraceService(private val tracer: Tracer) {
    @WithSpan("processOrder")
    suspend fun processOrder(id: Long) {
        val span = tracer.spanBuilder("inner_span").startSpan()
        span.setAttribute("order_id", id)
        span.end()
    }
}
`

const obsNoMatchSrc = `
package com.example
data class Foo(val x: Int)
fun hello() = "world"
`

func TestKotlinObservability_LogExtraction(t *testing.T) {
	// Registry target: log_extraction → partial
	ents := extract(t, "custom_kotlin_observability", fi("UserService.kt", "kotlin", obsLogSrc))
	if len(ents) == 0 {
		t.Fatal("[obs] expected log entities, got none")
	}
	loggerCount := 0
	logStmtCount := 0
	for _, e := range ents {
		if e.Subtype == "logger" {
			loggerCount++
		}
		if e.Subtype == "log_statement" {
			logStmtCount++
		}
	}
	if loggerCount == 0 {
		t.Errorf("[obs] expected logger entity (LoggerFactory / KotlinLogging), got 0")
	}
	if logStmtCount == 0 {
		t.Errorf("[obs] expected log_statement entities (log.info/warn/debug), got 0")
	}
}

func TestKotlinObservability_MetricExtraction(t *testing.T) {
	// Registry target: metric_extraction → partial
	ents := extract(t, "custom_kotlin_observability", fi("MetricService.kt", "kotlin", obsMetricSrc))
	if len(ents) == 0 {
		t.Fatal("[obs] expected metric entities, got none")
	}
	metricCount := 0
	for _, e := range ents {
		if e.Subtype == "metric" {
			metricCount++
		}
	}
	if metricCount == 0 {
		t.Errorf("[obs] expected metric entities (Counter.builder / @Timed), got 0; all: %v", ents)
	}
}

func TestKotlinObservability_TraceExtraction(t *testing.T) {
	// Registry target: trace_extraction → partial
	ents := extract(t, "custom_kotlin_observability", fi("TraceService.kt", "kotlin", obsTraceSrc))
	if len(ents) == 0 {
		t.Fatal("[obs] expected trace entities, got none")
	}
	traceCount := 0
	for _, e := range ents {
		if e.Subtype == "trace_span" {
			traceCount++
		}
	}
	if traceCount == 0 {
		t.Errorf("[obs] expected trace_span entities (@WithSpan / tracer.spanBuilder), got 0; all: %v", ents)
	}
}

func TestKotlinObservability_NoMatch(t *testing.T) {
	ents := extract(t, "custom_kotlin_observability", fi("Foo.kt", "kotlin", obsNoMatchSrc))
	if len(ents) != 0 {
		t.Errorf("[obs] expected no entities for plain Kotlin, got %d", len(ents))
	}
}

func TestKotlinObservability_EmptyFile(t *testing.T) {
	ents := extract(t, "custom_kotlin_observability", fi("Empty.kt", "kotlin", ""))
	if len(ents) != 0 {
		t.Errorf("[obs] expected no entities for empty file, got %d", len(ents))
	}
}

// --- value-asserting tests: prove SPECIFIC literal metric/span names are
//     captured at the call site (the basis for metric/trace → full). ---

const obsMetricNamesSrc = `
package com.example

import io.micrometer.core.instrument.Counter
import io.micrometer.core.instrument.Timer
import io.micrometer.core.annotation.Timed
import io.micrometer.core.annotation.Counted

class MetricService(private val meterRegistry: io.micrometer.core.instrument.MeterRegistry) {
    private val counter = Counter.builder("api.requests").register(meterRegistry)
    private val timer   = Timer.builder("api.latency").register(meterRegistry)

    fun touch() {
        meterRegistry.counter("registry.hits")
        meterRegistry.timer("registry.latency")
    }

    @Timed("api.findUser")
    fun findUser(id: Long): String = "user_$id"

    @Counted(value = "api.createUser")
    suspend fun createUser(name: String) {}

    @Timed
    fun untitled(): Int = 0
}
`

func TestKotlinObservability_MetricNamesCaptured(t *testing.T) {
	ents := extract(t, "custom_kotlin_observability", fi("MetricService.kt", "kotlin", obsMetricNamesSrc))

	wantNames := map[string]bool{
		"api.requests":     false, // Counter.builder
		"api.latency":      false, // Timer.builder
		"registry.hits":    false, // meterRegistry.counter("...")
		"registry.latency": false, // meterRegistry.timer("...")
		"api.findUser":     false, // @Timed("...")
		"api.createUser":   false, // @Counted(value = "...")
	}
	for _, e := range ents {
		if e.Subtype != "metric" {
			continue
		}
		if mn := e.Props["metric_name"]; mn != "" {
			if _, ok := wantNames[mn]; ok {
				wantNames[mn] = true
			}
		}
	}
	for name, seen := range wantNames {
		if !seen {
			t.Errorf("[obs] expected metric_name %q captured at call site, not found", name)
		}
	}

	// @Timed with no literal name must fall back to the fun name, flagged.
	if e := findMetricByName(ents, "untitled"); e == nil {
		t.Error("[obs] expected @Timed fallback metric_name=untitled")
	} else if src := e.Props["metric_name_source"]; src != "defaulted_to_decl" {
		t.Errorf("[obs] @Timed no-arg should be defaulted_to_decl, got %q", src)
	}
	// And a literal name must be flagged literal.
	if e := findMetricByName(ents, "api.findUser"); e == nil {
		t.Error("[obs] expected @Timed literal metric_name=api.findUser")
	} else if src := e.Props["metric_name_source"]; src != "literal" {
		t.Errorf("[obs] @Timed(\"api.findUser\") should be literal, got %q", src)
	}
}

const obsTraceNamesSrc = `
package com.example

import io.opentelemetry.instrumentation.annotations.WithSpan
import io.opentelemetry.api.trace.Tracer
import io.micrometer.observation.annotation.Observed
import org.springframework.cloud.sleuth.annotation.NewSpan

class TraceService(private val appTracer: Tracer) {
    @WithSpan("processOrder")
    suspend fun processOrder(id: Long) {
        val span = appTracer.spanBuilder("inner_span").startSpan()
        span.setAttribute("order_id", id)
    }

    @NewSpan("ship.order")
    fun ship(id: Long) {}

    @Observed(name = "order.audit")
    fun audit() {}

    @WithSpan
    fun defaulted() {}
}
`

func TestKotlinObservability_TraceNamesCaptured(t *testing.T) {
	ents := extract(t, "custom_kotlin_observability", fi("TraceService.kt", "kotlin", obsTraceNamesSrc))

	wantSpans := map[string]bool{
		"processOrder": false, // @WithSpan("...")
		"inner_span":   false, // tracer.spanBuilder("...")
		"ship.order":   false, // @NewSpan("...")
		"order.audit":  false, // @Observed(name = "...")
	}
	for _, e := range ents {
		if e.Subtype != "trace_span" {
			continue
		}
		if sn := e.Props["span_name"]; sn != "" {
			if _, ok := wantSpans[sn]; ok {
				wantSpans[sn] = true
			}
		}
	}
	for name, seen := range wantSpans {
		if !seen {
			t.Errorf("[obs] expected span_name %q captured at call site, not found", name)
		}
	}

	// @WithSpan with no literal name falls back to fun name, flagged.
	if e := findSpanByName(ents, "defaulted"); e == nil {
		t.Error("[obs] expected @WithSpan fallback span_name=defaulted")
	} else if src := e.Props["span_name_source"]; src != "defaulted_to_decl" {
		t.Errorf("[obs] @WithSpan no-arg should be defaulted_to_decl, got %q", src)
	}
	if e := findSpanByName(ents, "inner_span"); e == nil {
		t.Error("[obs] expected spanBuilder span_name=inner_span")
	} else if src := e.Props["span_name_source"]; src != "literal" {
		t.Errorf("[obs] spanBuilder(\"inner_span\") should be literal, got %q", src)
	}
}

const obsKotlinLoggingValSrc = `
package com.example
import io.github.oshai.kotlinlogging.KotlinLogging

private val auditLog = KotlinLogging.logger {}
private val logger = KotlinLogging.logger {}

class AuditService {
    fun record(id: Long) {
        logger.info { "recorded entity head" }
    }
}
`

func TestKotlinObservability_KotlinLoggingNameAndLambda(t *testing.T) {
	ents := extract(t, "custom_kotlin_observability", fi("AuditService.kt", "kotlin", obsKotlinLoggingValSrc))
	// logger val name captured
	gotLoggerName := false
	gotLambdaMsg := false
	for _, e := range ents {
		if e.Subtype == "logger" && e.Props["logger_name"] == "auditLog" {
			gotLoggerName = true
		}
		if e.Subtype == "log_statement" && e.Props["message"] == "recorded entity head" {
			gotLambdaMsg = true
		}
	}
	if !gotLoggerName {
		t.Error("[obs] expected kotlin-logging logger_name=auditLog captured")
	}
	if !gotLambdaMsg {
		t.Error("[obs] expected lazy-lambda log message head captured")
	}
}

// --- Micronaut (Kotlin) observability probes (issue #4015) ---
//
// Micronaut observability uses Micrometer (@Timed/@Counted, meterRegistry),
// Micronaut Tracing (@NewSpan/@ContinueSpan over OTel/Brave), and SLF4J
// (LoggerFactory.getLogger). These are detected language-level by
// custom_kotlin_observability — the same extractor that covers ktor/http4k —
// so a Micronaut .kt file flows through it unchanged. These probes assert the
// SPECIFIC named metric/span/logger fires for Micronaut idioms.
const obsMicronautSrc = `
package com.example.micronaut

import io.micronaut.http.annotation.Controller
import io.micronaut.http.annotation.Get
import io.micronaut.tracing.annotation.NewSpan
import io.micrometer.core.annotation.Timed
import io.micrometer.core.annotation.Counted
import io.micrometer.core.instrument.MeterRegistry
import org.slf4j.LoggerFactory

@Controller("/orders")
class OrderController(private val meterRegistry: MeterRegistry) {

    private val log = LoggerFactory.getLogger(OrderController::class.java)

    @Get
    @Timed("orders.count")
    fun list(): List<String> {
        log.info("listing orders")
        meterRegistry.counter("orders.listed")
        return emptyList()
    }

    @Counted("orders.created")
    fun create(name: String) {}

    @NewSpan("load")
    fun load(id: Long) {}
}
`

func TestKotlinObservability_Micronaut_Issue4015(t *testing.T) {
	ents := extract(t, "custom_kotlin_observability", fi("OrderController.kt", "kotlin", obsMicronautSrc))
	if len(ents) == 0 {
		t.Fatal("[obs][micronaut] expected entities, got none")
	}

	// metric: @Timed("orders.count") — literal metric name asserted.
	if e := findMetricByName(ents, "orders.count"); e == nil {
		t.Error("[obs][micronaut] expected @Timed metric_name=orders.count")
	} else if src := e.Props["metric_name_source"]; src != "literal" {
		t.Errorf("[obs][micronaut] @Timed(\"orders.count\") should be literal, got %q", src)
	}
	// metric: @Counted("orders.created").
	if e := findMetricByName(ents, "orders.created"); e == nil {
		t.Error("[obs][micronaut] expected @Counted metric_name=orders.created")
	} else if src := e.Props["metric_name_source"]; src != "literal" {
		t.Errorf("[obs][micronaut] @Counted(\"orders.created\") should be literal, got %q", src)
	}
	// metric: meterRegistry.counter("orders.listed") — literal registry meter name.
	if e := findMetricByName(ents, "orders.listed"); e == nil {
		t.Error("[obs][micronaut] expected meterRegistry.counter metric_name=orders.listed")
	}
	// trace: @NewSpan("load") — literal span name asserted (Micronaut Tracing).
	if e := findSpanByName(ents, "load"); e == nil {
		t.Error("[obs][micronaut] expected @NewSpan span_name=load")
	} else if src := e.Props["span_name_source"]; src != "literal" {
		t.Errorf("[obs][micronaut] @NewSpan(\"load\") should be literal, got %q", src)
	}
	// log: SLF4J LoggerFactory.getLogger + log.info — partial signal present.
	gotLogger := false
	gotLogStmt := false
	for _, e := range ents {
		if e.Subtype == "logger" {
			gotLogger = true
		}
		if e.Subtype == "log_statement" {
			gotLogStmt = true
		}
	}
	if !gotLogger {
		t.Error("[obs][micronaut] expected SLF4J logger entity")
	}
	if !gotLogStmt {
		t.Error("[obs][micronaut] expected log.info statement entity")
	}
}

// --- Quarkus (Kotlin) observability probes (issue #4015) ---
//
// Quarkus observability uses Micrometer/MicroProfile Metrics (@Timed/@Counted),
// OpenTelemetry (@WithSpan), and JBoss/SLF4J logging. Detected language-level
// by custom_kotlin_observability.
const obsQuarkusSrc = `
package com.example.quarkus

import jakarta.ws.rs.GET
import jakarta.ws.rs.Path
import io.opentelemetry.instrumentation.annotations.WithSpan
import org.eclipse.microprofile.metrics.annotation.Timed
import org.eclipse.microprofile.metrics.annotation.Counted
import org.slf4j.LoggerFactory

@Path("/checkout")
class CheckoutResource {

    private val log = LoggerFactory.getLogger(CheckoutResource::class.java)

    @GET
    @WithSpan
    fun handle() {
        log.info("handling checkout")
    }

    @Counted("checkout.attempts")
    fun attempt() {}

    @Timed("checkout.duration")
    fun finalize(id: Long) {}
}
`

func TestKotlinObservability_Quarkus_Issue4015(t *testing.T) {
	ents := extract(t, "custom_kotlin_observability", fi("CheckoutResource.kt", "kotlin", obsQuarkusSrc))
	if len(ents) == 0 {
		t.Fatal("[obs][quarkus] expected entities, got none")
	}

	// trace: @WithSpan (no literal) — span name defaults to fun name `handle`.
	if e := findSpanByName(ents, "handle"); e == nil {
		t.Error("[obs][quarkus] expected @WithSpan span_name=handle (defaulted to fun)")
	} else if src := e.Props["span_name_source"]; src != "defaulted_to_decl" {
		t.Errorf("[obs][quarkus] bare @WithSpan should be defaulted_to_decl, got %q", src)
	}
	// metric: @Counted("checkout.attempts").
	if e := findMetricByName(ents, "checkout.attempts"); e == nil {
		t.Error("[obs][quarkus] expected @Counted metric_name=checkout.attempts")
	} else if src := e.Props["metric_name_source"]; src != "literal" {
		t.Errorf("[obs][quarkus] @Counted(\"checkout.attempts\") should be literal, got %q", src)
	}
	// metric: @Timed("checkout.duration").
	if e := findMetricByName(ents, "checkout.duration"); e == nil {
		t.Error("[obs][quarkus] expected @Timed metric_name=checkout.duration")
	} else if src := e.Props["metric_name_source"]; src != "literal" {
		t.Errorf("[obs][quarkus] @Timed(\"checkout.duration\") should be literal, got %q", src)
	}
	// log: SLF4J logger + log.info partial.
	gotLogger := false
	gotLogStmt := false
	for _, e := range ents {
		if e.Subtype == "logger" {
			gotLogger = true
		}
		if e.Subtype == "log_statement" {
			gotLogStmt = true
		}
	}
	if !gotLogger {
		t.Error("[obs][quarkus] expected SLF4J logger entity")
	}
	if !gotLogStmt {
		t.Error("[obs][quarkus] expected log.info statement entity")
	}
}

// Negative: a plain Micronaut/Quarkus-shaped controller with NO observability
// APIs must yield no obs entities (framework presence alone is not a signal).
const obsMicronautNoObsSrc = `
package com.example.micronaut

import io.micronaut.http.annotation.Controller
import io.micronaut.http.annotation.Get

@Controller("/ping")
class PingController {
    @Get
    fun ping(): String = "pong"
}
`

func TestKotlinObservability_MicronautNoObs_Issue4015(t *testing.T) {
	ents := extract(t, "custom_kotlin_observability", fi("PingController.kt", "kotlin", obsMicronautNoObsSrc))
	if len(ents) != 0 {
		t.Errorf("[obs][micronaut] plain controller with no obs APIs should yield 0 entities, got %d", len(ents))
	}
}

func findMetricByName(ents []entitySummary, metricName string) *entitySummary {
	for i := range ents {
		if ents[i].Subtype == "metric" && ents[i].Props["metric_name"] == metricName {
			return &ents[i]
		}
	}
	return nil
}

func findSpanByName(ents []entitySummary, spanName string) *entitySummary {
	for i := range ents {
		if ents[i].Subtype == "trace_span" && ents[i].Props["span_name"] == spanName {
			return &ents[i]
		}
	}
	return nil
}
