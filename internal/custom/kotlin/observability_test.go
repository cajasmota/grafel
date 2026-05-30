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
