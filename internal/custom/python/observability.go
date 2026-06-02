// observability.go — Python observability extractor (log/metric/trace).
//
// Covers the Observability lane for all 17 Python http_backend frameworks:
//
//	log_extraction   — stdlib logging, loguru, structlog
//	metric_extraction — prometheus_client, statsd, datadog
//	trace_extraction  — opentelemetry-sdk, ddtrace, jaeger-client
//
// Detection is import-heuristic: the extractor recognises library imports and
// canonical call-site patterns (logger.info, Counter("name").inc(), tracer.start_as_current_span)
// but does NOT perform cross-file dataflow, so all cells are flipped to
// `partial` rather than `full`. This matches the honesty discipline established
// by the Java observability extractor (internal/custom/java/observability.go).
//
// A single extractor key "python_observability" is registered; the extractor
// runs on any Python file regardless of framework. Framework attribution in
// the emitted entity Properties is left blank when the file does not contain
// a recognisable framework import — the engine-layer enrichment pass fills it
// from the project manifest.
//
// Issue #3063.
package python

import (
	"context"
	"regexp"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func init() {
	extractor.Register("python_observability", &ObservabilityExtractor{})
}

// ObservabilityExtractor detects log, metric, and trace instrumentation
// across all Python HTTP-backend framework files.
type ObservabilityExtractor struct{}

func (e *ObservabilityExtractor) Language() string { return "python_observability" }

// ---------------------------------------------------------------------------
// Compiled regexes
// ---------------------------------------------------------------------------

var (
	// --------------- log_extraction ---------------

	// stdlib logging: import logging / from logging import ...
	obsLogImportRe = regexp.MustCompile(
		`(?m)^(?:import logging\b|from logging import)`)

	// loguru: import loguru / from loguru import logger
	obsLoguruImportRe = regexp.MustCompile(
		`(?m)^(?:import loguru\b|from loguru import)`)

	// structlog: import structlog / from structlog import ...
	obsStructlogImportRe = regexp.MustCompile(
		`(?m)^(?:import structlog\b|from structlog import)`)

	// logging.getLogger("name") or logging.getLogger(__name__)
	obsGetLoggerRe = regexp.MustCompile(
		`(?m)\blogging\.getLogger\s*\(\s*(?:"([^"]*?)"|'([^']*?)'|([^)]+?))\s*\)`)

	// logger = structlog.get_logger() / structlog.getLogger()
	obsStructlogGetLoggerRe = regexp.MustCompile(
		`(?m)\bstructlog\.(?:get_logger|getLogger)\s*\(`)

	// structlog.configure(...) — configures the logging pipeline
	obsStructlogConfigureRe = regexp.MustCompile(
		`(?m)\bstructlog\.configure\s*\(`)

	// loguru: logger.bind(...) / logger.opt(...) — loguru-specific enrichment calls
	obsLoguruBindRe = regexp.MustCompile(
		`(?m)\blogger\.(?:bind|opt|patch|contextualize)\s*\(`)

	// Log call sites: <logger>.debug/info/warning/warn/error/critical/exception(...)
	// Matches: log.info(...), logger.error(...), LOG.warning(...), app_log.debug(...)
	obsLogCallRe = regexp.MustCompile(
		`(?m)\b(\w+)\.(debug|info|warning|warn|error|critical|exception|trace)\s*\(`)

	// --------------- metric_extraction ---------------

	// prometheus_client: import prometheus_client / from prometheus_client import ...
	obsPrometheusImportRe = regexp.MustCompile(
		`(?m)^(?:import prometheus_client\b|from prometheus_client import)`)

	// statsd: import statsd / from statsd import ...
	obsStatsdImportRe = regexp.MustCompile(
		`(?m)^(?:import statsd\b|from statsd import)`)

	// datadog: import datadog / from datadog import / from ddsketch import
	obsDatadogMetricImportRe = regexp.MustCompile(
		`(?m)^(?:import datadog\b|from datadog(?:\.dogstatsd)? import)`)

	// prometheus_client metric construction:
	// Counter("name", ...) / Gauge(...) / Histogram(...) / Summary(...)
	obsPromMetricRe = regexp.MustCompile(
		`(?m)\b(Counter|Gauge|Histogram|Summary|Info|Enum|REGISTRY)\s*\(\s*["']([^"']*)["']`)

	// prometheus_client push / generate:
	// push_to_gateway(...) / generate_latest(...)
	obsPromGatewayRe = regexp.MustCompile(
		`(?m)\b(?:push_to_gateway|generate_latest|write_to_textfile)\s*\(`)

	// statsd calls: statsd.incr / statsd.gauge / statsd.timing / statsd.histogram
	obsStatsdCallRe = regexp.MustCompile(
		`(?m)\b(\w+)\.(incr|decr|gauge|timing|histogram|set|event)\s*\(\s*["']([^"']*)["']`)

	// datadog API: statsd.increment / statsd.gauge / api.Metric.send
	obsDatadogCallRe = regexp.MustCompile(
		`(?m)\b(?:statsd|DogStatsd)\.(increment|decrement|gauge|histogram|timing|set|distribution|event|service_check)\s*\(`)

	// --------------- trace_extraction ---------------

	// opentelemetry: import opentelemetry / from opentelemetry import ... / from opentelemetry.trace import
	obsOtelImportRe = regexp.MustCompile(
		`(?m)^(?:import opentelemetry\b|from opentelemetry(?:\.\w+)* import)`)

	// ddtrace: import ddtrace / from ddtrace import ...
	obsDdtraceImportRe = regexp.MustCompile(
		`(?m)^(?:import ddtrace\b|from ddtrace import)`)

	// jaeger-client: import jaeger_client / from jaeger_client import ...
	obsJaegerImportRe = regexp.MustCompile(
		`(?m)^(?:import jaeger_client\b|from jaeger_client import)`)

	// OTel tracer.start_as_current_span("name") or tracer.start_span("name")
	obsOtelStartSpanRe = regexp.MustCompile(
		`(?m)\b(\w+)\.start_as_current_span\s*\(\s*["']([^"']*)["']`)
	obsOtelStartSpanPlainRe = regexp.MustCompile(
		`(?m)\b(\w+)\.start_span\s*\(\s*["']([^"']*)["']`)

	// OTel @tracer.start_as_current_span("name") decorator
	obsOtelSpanDecoratorRe = regexp.MustCompile(
		`(?m)@(\w+)\.start_as_current_span\s*\(\s*["']([^"']*)["']`)

	// ddtrace: @tracer.wrap("name") or ddtrace.tracer.trace("name")
	obsDdtraceWrapRe = regexp.MustCompile(
		`(?m)@(?:tracer|ddtrace\.tracer)\.wrap\s*\(\s*(?:["']([^"']*)["'])?`)
	obsDdtraceTraceRe = regexp.MustCompile(
		`(?m)\b(?:tracer|ddtrace\.tracer)\.trace\s*\(\s*["']([^"']*)["']`)

	// jaeger: opentracing.tracer.start_span("name") / config.initialize_tracer()
	obsJaegerStartSpanRe = regexp.MustCompile(
		`(?m)\b(?:opentracing\.tracer|tracer)\.start_span\s*\(\s*["']([^"']*)["']`)
	obsJaegerInitRe = regexp.MustCompile(
		`(?m)\bConfig\s*\([^)]*service_name\s*=\s*["']([^"']*)["']`)
)

// ---------------------------------------------------------------------------
// Extract
// ---------------------------------------------------------------------------

func (e *ObservabilityExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("custom.python_observability")
	_, span := tracer.Start(ctx, "custom.python_observability")
	defer span.End()
	span.SetAttributes(attribute.String("file", file.Path))

	if len(file.Content) == 0 {
		return nil, nil
	}
	src := string(file.Content)
	var out []types.EntityRecord

	// Guard: skip files that contain none of the observability library tokens.
	// This is a fast heuristic to avoid running all regexes on every Python file.
	hasLog := strings.Contains(src, "logging") || strings.Contains(src, "loguru") || strings.Contains(src, "structlog")
	hasMetric := strings.Contains(src, "prometheus_client") || strings.Contains(src, "statsd") ||
		strings.Contains(src, "datadog") || strings.Contains(src, "DogStatsd")
	hasTrace := strings.Contains(src, "opentelemetry") || strings.Contains(src, "ddtrace") ||
		strings.Contains(src, "jaeger_client") || strings.Contains(src, "opentracing")

	if !hasLog && !hasMetric && !hasTrace {
		return nil, nil
	}

	out = append(out, extractPyLogging(src, file.Path)...)
	out = append(out, extractPyMetrics(src, file.Path)...)
	out = append(out, extractPyTracing(src, file.Path)...)

	return out, nil
}

// ---------------------------------------------------------------------------
// log_extraction
// ---------------------------------------------------------------------------

func extractPyLogging(src, fp string) []types.EntityRecord {
	var out []types.EntityRecord

	// --- stdlib logging ---
	if obsLogImportRe.MatchString(src) {
		// logging.getLogger("name") declarations
		for _, idx := range allMatchesIndex(obsGetLoggerRe, src) {
			// capture groups: 2=double-quoted, 4=single-quoted, 6=bare (e.g. __name__)
			name := ""
			for _, gi := range []int{2, 4, 6} {
				if idx[gi] >= 0 {
					name = src[idx[gi]:idx[gi+1]]
					break
				}
			}
			if name == "" {
				name = "logger"
			}
			line := lineOf(src, idx[0])
			out = append(out, entity(name, string(types.EntityKindPattern), "logger", fp, line,
				map[string]string{
					"signal":      "log",
					"library":     "logging",
					"kind":        "logger",
					"logger_name": name,
				}))
		}
		// if there's no explicit getLogger but logging is imported, emit a file-level signal
		if !obsGetLoggerRe.MatchString(src) {
			loc := obsLogImportRe.FindStringIndex(src)
			line := lineOf(src, loc[0])
			out = append(out, entity("logging", string(types.EntityKindPattern), "logger", fp, line,
				map[string]string{
					"signal":  "log",
					"library": "logging",
					"kind":    "import",
				}))
		}
	}

	// --- loguru ---
	if obsLoguruImportRe.MatchString(src) {
		loc := obsLoguruImportRe.FindStringIndex(src)
		line := lineOf(src, loc[0])
		out = append(out, entity("loguru.logger", string(types.EntityKindPattern), "logger", fp, line,
			map[string]string{
				"signal":  "log",
				"library": "loguru",
				"kind":    "logger",
			}))
		// loguru bind/opt calls — enrichment hints
		for _, idx := range allMatchesIndex(obsLoguruBindRe, src) {
			line := lineOf(src, idx[0])
			out = append(out, entity("logger.bind", string(types.EntityKindPattern), "log_statement", fp, line,
				map[string]string{
					"signal":  "log",
					"library": "loguru",
					"kind":    "log_statement",
				}))
		}
	}

	// --- structlog ---
	if obsStructlogImportRe.MatchString(src) {
		// structlog.get_logger() calls
		for _, idx := range allMatchesIndex(obsStructlogGetLoggerRe, src) {
			line := lineOf(src, idx[0])
			out = append(out, entity("structlog.get_logger", string(types.EntityKindPattern), "logger", fp, line,
				map[string]string{
					"signal":  "log",
					"library": "structlog",
					"kind":    "logger",
				}))
		}
		// structlog.configure(...)
		if obsStructlogConfigureRe.MatchString(src) {
			loc := obsStructlogConfigureRe.FindStringIndex(src)
			line := lineOf(src, loc[0])
			out = append(out, entity("structlog.configure", string(types.EntityKindPattern), "logger", fp, line,
				map[string]string{
					"signal":  "log",
					"library": "structlog",
					"kind":    "configure",
				}))
		}
		// file-level signal when no explicit get_logger
		if !obsStructlogGetLoggerRe.MatchString(src) && !obsStructlogConfigureRe.MatchString(src) {
			loc := obsStructlogImportRe.FindStringIndex(src)
			line := lineOf(src, loc[0])
			out = append(out, entity("structlog", string(types.EntityKindPattern), "logger", fp, line,
				map[string]string{
					"signal":  "log",
					"library": "structlog",
					"kind":    "import",
				}))
		}
	}

	// --- log call sites (any logger variable) ---
	// Only emit when we have a recognised log library to avoid false positives.
	hasLogLib := obsLogImportRe.MatchString(src) || obsLoguruImportRe.MatchString(src) || obsStructlogImportRe.MatchString(src)
	if hasLogLib {
		for _, idx := range allMatchesIndex(obsLogCallRe, src) {
			receiver := src[idx[2]:idx[3]]
			level := src[idx[4]:idx[5]]
			// Basic heuristic: skip common false positives (test assertions, HTTP client methods)
			if receiver == "response" || receiver == "resp" || receiver == "res" || receiver == "request" || receiver == "req" {
				continue
			}
			line := lineOf(src, idx[0])
			library := "logging"
			if obsLoguruImportRe.MatchString(src) && !obsLogImportRe.MatchString(src) {
				library = "loguru"
			} else if obsStructlogImportRe.MatchString(src) && !obsLogImportRe.MatchString(src) {
				library = "structlog"
			}
			out = append(out, entity(receiver+"."+level, string(types.EntityKindPattern), "log_statement", fp, line,
				map[string]string{
					"signal":    "log",
					"library":   library,
					"kind":      "log_statement",
					"log_level": level,
					"receiver":  receiver,
				}))
		}
	}

	return out
}

// ---------------------------------------------------------------------------
// metric_extraction
// ---------------------------------------------------------------------------

func extractPyMetrics(src, fp string) []types.EntityRecord {
	var out []types.EntityRecord

	// --- prometheus_client ---
	if obsPrometheusImportRe.MatchString(src) {
		// Counter/Gauge/Histogram/Summary construction
		for _, idx := range allMatchesIndex(obsPromMetricRe, src) {
			meterType := src[idx[2]:idx[3]]
			metricName := src[idx[4]:idx[5]]
			line := lineOf(src, idx[0])
			out = append(out, entity(metricName, string(types.EntityKindPattern), "metric", fp, line,
				map[string]string{
					"signal":      "metric",
					"library":     "prometheus_client",
					"kind":        "metric",
					"metric_type": strings.ToLower(meterType),
					"metric_name": metricName,
				}))
		}
		// push / generate as file-level metric signal
		if obsPromGatewayRe.MatchString(src) {
			loc := obsPromGatewayRe.FindStringIndex(src)
			line := lineOf(src, loc[0])
			out = append(out, entity("prometheus_client.export", string(types.EntityKindPattern), "metric", fp, line,
				map[string]string{
					"signal":      "metric",
					"library":     "prometheus_client",
					"kind":        "export",
					"metric_type": "registry",
				}))
		}
		// file-level signal when only import found
		if !obsPromMetricRe.MatchString(src) && !obsPromGatewayRe.MatchString(src) {
			loc := obsPrometheusImportRe.FindStringIndex(src)
			line := lineOf(src, loc[0])
			out = append(out, entity("prometheus_client", string(types.EntityKindPattern), "metric", fp, line,
				map[string]string{
					"signal":  "metric",
					"library": "prometheus_client",
					"kind":    "import",
				}))
		}
	}

	// --- statsd ---
	if obsStatsdImportRe.MatchString(src) {
		for _, idx := range allMatchesIndex(obsStatsdCallRe, src) {
			receiver := src[idx[2]:idx[3]]
			method := src[idx[4]:idx[5]]
			metricName := src[idx[6]:idx[7]]
			line := lineOf(src, idx[0])
			out = append(out, entity(metricName, string(types.EntityKindPattern), "metric", fp, line,
				map[string]string{
					"signal":      "metric",
					"library":     "statsd",
					"kind":        "metric",
					"metric_type": obsStatsdType(method),
					"metric_name": metricName,
					"receiver":    receiver,
				}))
		}
		// file-level signal when no call sites
		if !obsStatsdCallRe.MatchString(src) {
			loc := obsStatsdImportRe.FindStringIndex(src)
			line := lineOf(src, loc[0])
			out = append(out, entity("statsd", string(types.EntityKindPattern), "metric", fp, line,
				map[string]string{
					"signal":  "metric",
					"library": "statsd",
					"kind":    "import",
				}))
		}
	}

	// --- datadog ---
	if obsDatadogMetricImportRe.MatchString(src) {
		for _, idx := range allMatchesIndex(obsDatadogCallRe, src) {
			method := src[idx[2]:idx[3]]
			line := lineOf(src, idx[0])
			out = append(out, entity("datadog."+method, string(types.EntityKindPattern), "metric", fp, line,
				map[string]string{
					"signal":      "metric",
					"library":     "datadog",
					"kind":        "metric",
					"metric_type": obsDatadogType(method),
				}))
		}
		// file-level signal when no call sites
		if !obsDatadogCallRe.MatchString(src) {
			loc := obsDatadogMetricImportRe.FindStringIndex(src)
			line := lineOf(src, loc[0])
			out = append(out, entity("datadog", string(types.EntityKindPattern), "metric", fp, line,
				map[string]string{
					"signal":  "metric",
					"library": "datadog",
					"kind":    "import",
				}))
		}
	}

	return out
}

// ---------------------------------------------------------------------------
// trace_extraction
// ---------------------------------------------------------------------------

func extractPyTracing(src, fp string) []types.EntityRecord {
	var out []types.EntityRecord

	// --- opentelemetry ---
	if obsOtelImportRe.MatchString(src) {
		// @tracer.start_as_current_span("name") decorator
		for _, idx := range allMatchesIndex(obsOtelSpanDecoratorRe, src) {
			tracerVar := src[idx[2]:idx[3]]
			spanName := src[idx[4]:idx[5]]
			line := lineOf(src, idx[0])
			out = append(out, entity(spanName, string(types.EntityKindPattern), "trace_span", fp, line,
				map[string]string{
					"signal":     "trace",
					"library":    "opentelemetry",
					"kind":       "trace_span",
					"span_kind":  "decorator",
					"span_name":  spanName,
					"tracer_var": tracerVar,
				}))
		}
		// tracer.start_as_current_span("name") context-manager
		for _, idx := range allMatchesIndex(obsOtelStartSpanRe, src) {
			tracerVar := src[idx[2]:idx[3]]
			spanName := src[idx[4]:idx[5]]
			line := lineOf(src, idx[0])
			out = append(out, entity(spanName, string(types.EntityKindPattern), "trace_span", fp, line,
				map[string]string{
					"signal":     "trace",
					"library":    "opentelemetry",
					"kind":       "trace_span",
					"span_kind":  "context_manager",
					"span_name":  spanName,
					"tracer_var": tracerVar,
				}))
		}
		// tracer.start_span("name")
		for _, idx := range allMatchesIndex(obsOtelStartSpanPlainRe, src) {
			tracerVar := src[idx[2]:idx[3]]
			spanName := src[idx[4]:idx[5]]
			line := lineOf(src, idx[0])
			out = append(out, entity(spanName, string(types.EntityKindPattern), "trace_span", fp, line,
				map[string]string{
					"signal":     "trace",
					"library":    "opentelemetry",
					"kind":       "trace_span",
					"span_kind":  "programmatic",
					"span_name":  spanName,
					"tracer_var": tracerVar,
				}))
		}
		// file-level signal when only import
		if !obsOtelSpanDecoratorRe.MatchString(src) && !obsOtelStartSpanRe.MatchString(src) && !obsOtelStartSpanPlainRe.MatchString(src) {
			loc := obsOtelImportRe.FindStringIndex(src)
			line := lineOf(src, loc[0])
			out = append(out, entity("opentelemetry", string(types.EntityKindPattern), "trace_span", fp, line,
				map[string]string{
					"signal":  "trace",
					"library": "opentelemetry",
					"kind":    "import",
				}))
		}
	}

	// --- ddtrace ---
	if obsDdtraceImportRe.MatchString(src) {
		// @tracer.wrap("name") decorator
		for _, idx := range allMatchesIndex(obsDdtraceWrapRe, src) {
			spanName := ""
			if idx[2] >= 0 {
				spanName = src[idx[2]:idx[3]]
			}
			if spanName == "" {
				spanName = "ddtrace.wrap"
			}
			line := lineOf(src, idx[0])
			out = append(out, entity(spanName, string(types.EntityKindPattern), "trace_span", fp, line,
				map[string]string{
					"signal":    "trace",
					"library":   "ddtrace",
					"kind":      "trace_span",
					"span_kind": "decorator",
					"span_name": spanName,
				}))
		}
		// tracer.trace("name") context-manager
		for _, idx := range allMatchesIndex(obsDdtraceTraceRe, src) {
			spanName := src[idx[2]:idx[3]]
			line := lineOf(src, idx[0])
			out = append(out, entity(spanName, string(types.EntityKindPattern), "trace_span", fp, line,
				map[string]string{
					"signal":    "trace",
					"library":   "ddtrace",
					"kind":      "trace_span",
					"span_kind": "context_manager",
					"span_name": spanName,
				}))
		}
		// file-level signal when only import
		if !obsDdtraceWrapRe.MatchString(src) && !obsDdtraceTraceRe.MatchString(src) {
			loc := obsDdtraceImportRe.FindStringIndex(src)
			line := lineOf(src, loc[0])
			out = append(out, entity("ddtrace", string(types.EntityKindPattern), "trace_span", fp, line,
				map[string]string{
					"signal":  "trace",
					"library": "ddtrace",
					"kind":    "import",
				}))
		}
	}

	// --- jaeger-client ---
	if obsJaegerImportRe.MatchString(src) {
		// Config(service_name="name") tracer initialization
		for _, idx := range allMatchesIndex(obsJaegerInitRe, src) {
			svcName := src[idx[2]:idx[3]]
			line := lineOf(src, idx[0])
			out = append(out, entity(svcName, string(types.EntityKindPattern), "trace_span", fp, line,
				map[string]string{
					"signal":       "trace",
					"library":      "jaeger_client",
					"kind":         "trace_span",
					"span_kind":    "init",
					"service_name": svcName,
				}))
		}
		// tracer.start_span("name")
		for _, idx := range allMatchesIndex(obsJaegerStartSpanRe, src) {
			spanName := src[idx[2]:idx[3]]
			line := lineOf(src, idx[0])
			out = append(out, entity(spanName, string(types.EntityKindPattern), "trace_span", fp, line,
				map[string]string{
					"signal":    "trace",
					"library":   "jaeger_client",
					"kind":      "trace_span",
					"span_kind": "programmatic",
					"span_name": spanName,
				}))
		}
		// file-level signal when only import
		if !obsJaegerInitRe.MatchString(src) && !obsJaegerStartSpanRe.MatchString(src) {
			loc := obsJaegerImportRe.FindStringIndex(src)
			line := lineOf(src, loc[0])
			out = append(out, entity("jaeger_client", string(types.EntityKindPattern), "trace_span", fp, line,
				map[string]string{
					"signal":  "trace",
					"library": "jaeger_client",
					"kind":    "import",
				}))
		}
	}

	return out
}

// ---------------------------------------------------------------------------
// Helper: metric-type normalisation
// ---------------------------------------------------------------------------

// obsStatsdType normalises a statsd method name to a metric_type value.
func obsStatsdType(method string) string {
	switch method {
	case "incr", "decr":
		return "counter"
	case "gauge":
		return "gauge"
	case "timing":
		return "timer"
	case "histogram":
		return "histogram"
	case "set":
		return "set"
	case "event":
		return "event"
	default:
		return method
	}
}

// obsDatadogType normalises a datadog statsd method name to a metric_type value.
func obsDatadogType(method string) string {
	switch method {
	case "increment", "decrement":
		return "counter"
	case "gauge":
		return "gauge"
	case "histogram":
		return "histogram"
	case "timing":
		return "timer"
	case "distribution":
		return "distribution"
	case "set":
		return "set"
	case "event":
		return "event"
	case "service_check":
		return "service_check"
	default:
		return method
	}
}
