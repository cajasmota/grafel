// observability.go — Ruby observability extractor (log/metric/trace).
//
// Covers the Observability lane for all 8 Ruby http_backend frameworks:
//
//	log_extraction   — Rails.logger.*, Logger.new, logger.info, SemanticLogger,
//	                   logger.tagged, ActiveSupport::TaggedLogging, lograge config
//	metric_extraction — prometheus-client, Datadog::Statsd, Yabeda (configure +
//	                   counters/gauges/histograms), statsd-client
//	trace_extraction  — OpenTelemetry::SDK, tracer.in_span, OpenTracing, Skylight,
//	                   ddtrace/Datadog::Tracing, ActiveSupport::Notifications.instrument
//
// Detection is import/require-heuristic: the extractor recognises library
// requires and canonical call-site patterns but does NOT perform cross-file
// dataflow, so all cells remain `partial` rather than `full`. This
// matches the honesty discipline established by the Java and Python observability
// extractors (internal/custom/java/observability.go,
// internal/custom/python/observability.go).
//
// A single extractor key "ruby_observability" is registered; the extractor
// runs on any Ruby file regardless of framework.
//
// Part of issues #3282, #3343.
package ruby

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
	extractor.Register("ruby_observability", &rubyObservabilityExtractor{})
}

// rubyObservabilityExtractor detects log, metric, and trace instrumentation
// across Ruby source files.
type rubyObservabilityExtractor struct{}

func (e *rubyObservabilityExtractor) Language() string { return "ruby_observability" }

// ---------------------------------------------------------------------------
// Compiled regexes
// ---------------------------------------------------------------------------

var (
	// --------------- log_extraction ---------------

	// require 'logger' / require "logger"
	rbLoggerRequireRe = regexp.MustCompile(
		`(?m)\brequire\s+['"]logger['"]`)

	// Rails.logger.info / Rails.logger.error / etc.
	rbRailsLoggerRe = regexp.MustCompile(
		`(?m)\bRails\.logger\.(debug|info|warn|error|fatal|unknown)\s*[\({]`)

	// Logger.new(...) — stdlib logger instantiation
	rbLoggerNewRe = regexp.MustCompile(
		`(?m)\bLogger\.new\s*\(`)

	// logger.info / logger.warn / logger.error etc. (any receiver)
	rbLogCallRe = regexp.MustCompile(
		`(?m)\b(\w+)\.(debug|info|warn|warning|error|fatal|unknown)\s*[\({]`)

	// semantic_logger: require 'semantic_logger'
	rbSemanticLoggerRequireRe = regexp.MustCompile(
		`(?m)\brequire\s+['"]semantic_logger['"]`)

	// SemanticLogger.add_appender / include SemanticLogger
	rbSemanticLoggerUsageRe = regexp.MustCompile(
		`(?m)(?:SemanticLogger\.|include SemanticLogger)`)

	// --------------- metric_extraction ---------------

	// require 'prometheus/client' / require "prometheus-client"
	rbPrometheusRequireRe = regexp.MustCompile(
		`(?m)\brequire\s+['"]prometheus(?:/client|-client)['"]`)

	// Prometheus::Client::Counter/Gauge/Histogram/Summary.new(...)
	rbPrometheusMetricRe = regexp.MustCompile(
		`(?m)\bPrometheus::Client::(Counter|Gauge|Histogram|Summary|Meter)\b`)

	// require 'datadog/statsd' / require "dogstatsd-ruby"
	rbDatadogRequireRe = regexp.MustCompile(
		`(?m)\brequire\s+['"](?:datadog/statsd|dogstatsd-ruby|ddtrace)['"]`)

	// Datadog::Statsd.new(...)
	rbDatadogStatsdNewRe = regexp.MustCompile(
		`(?m)\bDatadog::Statsd\.new\s*\(`)

	// statsd.increment / statsd.gauge / statsd.histogram etc.
	rbStatsdCallRe = regexp.MustCompile(
		`(?m)\b(\w+)\.(increment|decrement|count|gauge|histogram|timing|set|event|service_check)\s*\(\s*['"]([^'"]+)['"]`)

	// Yabeda: require 'yabeda'
	rbYabedaRequireRe = regexp.MustCompile(
		`(?m)\brequire\s+['"]yabeda(?:/[a-z_-]+)?['"]`)

	// Yabeda.counter / Yabeda.gauge / Yabeda.histogram
	rbYabedaMetricRe = regexp.MustCompile(
		`(?m)\bYabeda\.(counter|gauge|histogram|summary|meter)\s*\(`)

	// statsd-client gem: StatsD.measure / StatsD.increment
	rbStatsDRubyRe = regexp.MustCompile(
		`(?m)\bStatsD\.(measure|increment|gauge|histogram|set|event|service_check)\s*\(`)

	// --------------- trace_extraction ---------------

	// require 'opentelemetry-sdk' / require 'opentelemetry/sdk'
	rbOtelRequireRe = regexp.MustCompile(
		`(?m)\brequire\s+['"]opentelemetry(?:-sdk|/sdk|/trace)?['"]`)

	// OpenTelemetry::SDK.configure / OpenTelemetry.tracer_provider
	rbOtelSDKRe = regexp.MustCompile(
		`(?m)\bOpenTelemetry(?:::SDK)?\.(?:configure|tracer_provider|logger)\b`)

	// tracer.in_span("name") do ... end
	rbOtelInSpanRe = regexp.MustCompile(
		`(?m)\b(\w+)\.in_span\s*\(\s*['"]([^'"]+)['"]`)

	// require 'ddtrace' / require "datadog"
	rbDdtraceRequireRe = regexp.MustCompile(
		`(?m)\brequire\s+['"](?:ddtrace|datadog)['"]`)

	// Datadog::Tracing.trace("name") / Datadog.configure
	rbDdtraceCallRe = regexp.MustCompile(
		`(?m)\bDatadog(?:::Tracing)?\.(?:trace|configure)\s*\(`)

	// require 'skylight'
	rbSkylightRequireRe = regexp.MustCompile(
		`(?m)\brequire\s+['"]skylight(?:/[a-z_-]+)?['"]`)

	// Skylight.instrument(title: "name") { }
	rbSkylightInstrumentRe = regexp.MustCompile(
		`(?m)\bSkylight\.instrument\s*\(`)

	// require 'opentracing'
	rbOpenTracingRequireRe = regexp.MustCompile(
		`(?m)\brequire\s+['"]opentracing['"]`)

	// OpenTracing.start_span("name") / OpenTracing.global_tracer
	rbOpenTracingCallRe = regexp.MustCompile(
		`(?m)\bOpenTracing\.(?:start_span|global_tracer|start_active_span)\s*\(`)

	// --------------- Rails-specific log patterns ---------------

	// logger.tagged("tag1", "tag2") { ... } — Rails tagged logging
	rbRailsLoggerTaggedRe = regexp.MustCompile(
		`(?m)\b(\w+)\.tagged\s*\(`)

	// ActiveSupport::TaggedLogging.new(logger)
	rbRailsTaggedLoggingRe = regexp.MustCompile(
		`(?m)\bActiveSupport::TaggedLogging\.new\s*\(`)

	// Lograge config: config.lograge.enabled = true / require 'lograge'
	rbLogrageRequireRe = regexp.MustCompile(
		`(?m)\brequire\s+['"]lograge['"]`)
	rbLogrageConfigRe = regexp.MustCompile(
		`(?m)\bconfig\.lograge\b`)

	// --------------- Rails-specific metric patterns ---------------

	// Yabeda.configure do ... end — configuration block
	rbYabedaConfigureRe = regexp.MustCompile(
		`(?m)\bYabeda\.configure\s*(?:do|\{)`)

	// --------------- Rails-specific trace patterns ---------------

	// ActiveSupport::Notifications.instrument("event.name") { ... }
	rbASNInstrumentRe = regexp.MustCompile(
		`(?m)\bActiveSupport::Notifications\.instrument\s*\(\s*['"]([^'"]+)['"]`)

	// ActiveSupport::Notifications.subscribe("event.name") { ... }
	rbASNSubscribeRe = regexp.MustCompile(
		`(?m)\bActiveSupport::Notifications\.subscribe\s*\(\s*['"]([^'"]+)['"]`)
)

// ---------------------------------------------------------------------------
// Extract
// ---------------------------------------------------------------------------

func (e *rubyObservabilityExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("custom.ruby_observability")
	_, span := tracer.Start(ctx, "custom.ruby_observability")
	defer span.End()
	span.SetAttributes(attribute.String("file", file.Path))

	if len(file.Content) == 0 {
		return nil, nil
	}
	src := string(file.Content)

	// Fast guard: skip files that contain none of the observability library tokens.
	hasLog := strings.Contains(src, "logger") || strings.Contains(src, "Logger") ||
		strings.Contains(src, "SemanticLogger") || strings.Contains(src, "Rails.logger") ||
		strings.Contains(src, "TaggedLogging") || strings.Contains(src, "lograge")
	hasMetric := strings.Contains(src, "prometheus") || strings.Contains(src, "Prometheus") ||
		strings.Contains(src, "Datadog::Statsd") || strings.Contains(src, "Yabeda") ||
		strings.Contains(src, "StatsD") || strings.Contains(src, "statsd")
	hasTrace := strings.Contains(src, "OpenTelemetry") || strings.Contains(src, "opentelemetry") ||
		strings.Contains(src, "ddtrace") || strings.Contains(src, "Datadog::Tracing") ||
		strings.Contains(src, "Skylight") || strings.Contains(src, "OpenTracing") ||
		strings.Contains(src, "opentracing") ||
		strings.Contains(src, "ActiveSupport::Notifications")

	if !hasLog && !hasMetric && !hasTrace {
		return nil, nil
	}

	var out []types.EntityRecord
	out = append(out, extractRubyLogging(src, file.Path)...)
	out = append(out, extractRubyMetrics(src, file.Path)...)
	out = append(out, extractRubyTracing(src, file.Path)...)
	out = append(out, extractRailsObsLogging(src, file.Path)...)
	out = append(out, extractRailsObsMetrics(src, file.Path)...)
	out = append(out, extractRailsObsTracing(src, file.Path)...)
	return out, nil
}

// ---------------------------------------------------------------------------
// log_extraction
// ---------------------------------------------------------------------------

func extractRubyLogging(src, fp string) []types.EntityRecord {
	var out []types.EntityRecord

	// Rails.logger.<level> call sites
	for _, idx := range rbRailsLoggerRe.FindAllStringSubmatchIndex(src, -1) {
		level := src[idx[2]:idx[3]]
		ln := lineOf(src, idx[0])
		e := makeEntity("Rails.logger."+level, string(types.EntityKindPattern), "log_statement", fp, "ruby", ln)
		setProps(&e, "signal", "log", "library", "rails_logger", "log_level", level, "receiver", "Rails.logger")
		out = append(out, e)
	}

	// stdlib Logger.new
	for _, idx := range rbLoggerNewRe.FindAllStringSubmatchIndex(src, -1) {
		ln := lineOf(src, idx[0])
		e := makeEntity("Logger.new", string(types.EntityKindPattern), "logger", fp, "ruby", ln)
		setProps(&e, "signal", "log", "library", "ruby_stdlib_logger", "kind", "instantiation")
		out = append(out, e)
	}

	// require 'logger' — file-level signal
	if rbLoggerRequireRe.MatchString(src) && !rbLoggerNewRe.MatchString(src) && !rbRailsLoggerRe.MatchString(src) {
		loc := rbLoggerRequireRe.FindStringIndex(src)
		ln := lineOf(src, loc[0])
		e := makeEntity("logger", string(types.EntityKindPattern), "logger", fp, "ruby", ln)
		setProps(&e, "signal", "log", "library", "ruby_stdlib_logger", "kind", "require")
		out = append(out, e)
	}

	// SemanticLogger
	if rbSemanticLoggerRequireRe.MatchString(src) || rbSemanticLoggerUsageRe.MatchString(src) {
		var loc []int
		if rbSemanticLoggerRequireRe.MatchString(src) {
			loc = rbSemanticLoggerRequireRe.FindStringIndex(src)
		} else {
			loc = rbSemanticLoggerUsageRe.FindStringIndex(src)
		}
		ln := lineOf(src, loc[0])
		e := makeEntity("SemanticLogger", string(types.EntityKindPattern), "logger", fp, "ruby", ln)
		setProps(&e, "signal", "log", "library", "semantic_logger", "kind", "logger")
		out = append(out, e)
	}

	// Generic logger.<level> calls (when a log library is present)
	hasLogLib := rbLoggerRequireRe.MatchString(src) || rbRailsLoggerRe.MatchString(src) ||
		rbSemanticLoggerRequireRe.MatchString(src) || rbSemanticLoggerUsageRe.MatchString(src) ||
		strings.Contains(src, "Rails.logger")
	if hasLogLib {
		for _, idx := range rbLogCallRe.FindAllStringSubmatchIndex(src, -1) {
			receiver := src[idx[2]:idx[3]]
			level := src[idx[4]:idx[5]]
			// Skip common false positives
			if receiver == "response" || receiver == "resp" || receiver == "res" ||
				receiver == "request" || receiver == "req" || receiver == "Rails" {
				continue
			}
			ln := lineOf(src, idx[0])
			e := makeEntity(receiver+"."+level, string(types.EntityKindPattern), "log_statement", fp, "ruby", ln)
			setProps(&e, "signal", "log", "library", "ruby_logger", "log_level", level, "receiver", receiver)
			out = append(out, e)
		}
	}

	return out
}

// ---------------------------------------------------------------------------
// metric_extraction
// ---------------------------------------------------------------------------

func extractRubyMetrics(src, fp string) []types.EntityRecord {
	var out []types.EntityRecord

	// --- prometheus-client ---
	if rbPrometheusRequireRe.MatchString(src) || rbPrometheusMetricRe.MatchString(src) {
		for _, idx := range rbPrometheusMetricRe.FindAllStringSubmatchIndex(src, -1) {
			meterType := src[idx[2]:idx[3]]
			ln := lineOf(src, idx[0])
			e := makeEntity("Prometheus::Client::"+meterType, string(types.EntityKindPattern), "metric", fp, "ruby", ln)
			setProps(&e, "signal", "metric", "library", "prometheus_client", "metric_type", strings.ToLower(meterType))
			out = append(out, e)
		}
		if !rbPrometheusMetricRe.MatchString(src) {
			loc := rbPrometheusRequireRe.FindStringIndex(src)
			ln := lineOf(src, loc[0])
			e := makeEntity("prometheus_client", string(types.EntityKindPattern), "metric", fp, "ruby", ln)
			setProps(&e, "signal", "metric", "library", "prometheus_client", "kind", "require")
			out = append(out, e)
		}
	}

	// --- Datadog::Statsd ---
	if rbDatadogRequireRe.MatchString(src) || rbDatadogStatsdNewRe.MatchString(src) {
		// Always emit instantiation entity when Datadog::Statsd.new is present
		if rbDatadogStatsdNewRe.MatchString(src) {
			loc := rbDatadogStatsdNewRe.FindStringIndex(src)
			ln := lineOf(src, loc[0])
			e := makeEntity("Datadog::Statsd.new", string(types.EntityKindPattern), "metric", fp, "ruby", ln)
			setProps(&e, "signal", "metric", "library", "datadog_statsd", "kind", "instantiation")
			out = append(out, e)
		}
		// Emit per-call metric entities
		for _, idx := range rbStatsdCallRe.FindAllStringSubmatchIndex(src, -1) {
			receiver := src[idx[2]:idx[3]]
			method := src[idx[4]:idx[5]]
			metricName := src[idx[6]:idx[7]]
			ln := lineOf(src, idx[0])
			e := makeEntity(metricName, string(types.EntityKindPattern), "metric", fp, "ruby", ln)
			setProps(&e, "signal", "metric", "library", "datadog_statsd",
				"metric_type", rubyStatsdType(method), "metric_name", metricName, "receiver", receiver)
			out = append(out, e)
		}
		// File-level signal when only require and nothing else
		if !rbDatadogStatsdNewRe.MatchString(src) && !rbStatsdCallRe.MatchString(src) {
			loc := rbDatadogRequireRe.FindStringIndex(src)
			ln := lineOf(src, loc[0])
			e := makeEntity("datadog_statsd", string(types.EntityKindPattern), "metric", fp, "ruby", ln)
			setProps(&e, "signal", "metric", "library", "datadog_statsd", "kind", "require")
			out = append(out, e)
		}
	}

	// --- Yabeda ---
	if rbYabedaRequireRe.MatchString(src) || rbYabedaMetricRe.MatchString(src) {
		for _, idx := range rbYabedaMetricRe.FindAllStringSubmatchIndex(src, -1) {
			meterType := src[idx[2]:idx[3]]
			ln := lineOf(src, idx[0])
			e := makeEntity("Yabeda."+meterType, string(types.EntityKindPattern), "metric", fp, "ruby", ln)
			setProps(&e, "signal", "metric", "library", "yabeda", "metric_type", meterType)
			out = append(out, e)
		}
		if !rbYabedaMetricRe.MatchString(src) {
			loc := rbYabedaRequireRe.FindStringIndex(src)
			ln := lineOf(src, loc[0])
			e := makeEntity("yabeda", string(types.EntityKindPattern), "metric", fp, "ruby", ln)
			setProps(&e, "signal", "metric", "library", "yabeda", "kind", "require")
			out = append(out, e)
		}
	}

	// --- StatsD ruby gem ---
	for _, idx := range rbStatsDRubyRe.FindAllStringSubmatchIndex(src, -1) {
		method := src[idx[2]:idx[3]]
		ln := lineOf(src, idx[0])
		e := makeEntity("StatsD."+method, string(types.EntityKindPattern), "metric", fp, "ruby", ln)
		setProps(&e, "signal", "metric", "library", "statsd_ruby", "metric_type", rubyStatsdType(method))
		out = append(out, e)
	}

	return out
}

// ---------------------------------------------------------------------------
// trace_extraction
// ---------------------------------------------------------------------------

func extractRubyTracing(src, fp string) []types.EntityRecord {
	var out []types.EntityRecord

	// --- OpenTelemetry ---
	if rbOtelRequireRe.MatchString(src) || rbOtelSDKRe.MatchString(src) {
		// tracer.in_span("name") do … end
		for _, idx := range rbOtelInSpanRe.FindAllStringSubmatchIndex(src, -1) {
			tracerVar := src[idx[2]:idx[3]]
			spanName := src[idx[4]:idx[5]]
			ln := lineOf(src, idx[0])
			e := makeEntity(spanName, string(types.EntityKindPattern), "trace_span", fp, "ruby", ln)
			setProps(&e, "signal", "trace", "library", "opentelemetry",
				"span_kind", "block", "span_name", spanName, "tracer_var", tracerVar)
			out = append(out, e)
		}
		if rbOtelSDKRe.MatchString(src) && !rbOtelInSpanRe.MatchString(src) {
			loc := rbOtelSDKRe.FindStringIndex(src)
			ln := lineOf(src, loc[0])
			e := makeEntity("OpenTelemetry::SDK", string(types.EntityKindPattern), "trace_span", fp, "ruby", ln)
			setProps(&e, "signal", "trace", "library", "opentelemetry", "kind", "sdk_configure")
			out = append(out, e)
		}
		if !rbOtelSDKRe.MatchString(src) && !rbOtelInSpanRe.MatchString(src) {
			loc := rbOtelRequireRe.FindStringIndex(src)
			ln := lineOf(src, loc[0])
			e := makeEntity("opentelemetry", string(types.EntityKindPattern), "trace_span", fp, "ruby", ln)
			setProps(&e, "signal", "trace", "library", "opentelemetry", "kind", "require")
			out = append(out, e)
		}
	}

	// --- ddtrace / Datadog::Tracing ---
	if rbDdtraceRequireRe.MatchString(src) || rbDdtraceCallRe.MatchString(src) {
		for _, idx := range rbDdtraceCallRe.FindAllStringSubmatchIndex(src, -1) {
			ln := lineOf(src, idx[0])
			callSite := src[idx[0]:idx[1]]
			name := strings.TrimSpace(callSite)
			if len(name) > 40 {
				name = name[:40]
			}
			e := makeEntity(name, string(types.EntityKindPattern), "trace_span", fp, "ruby", ln)
			setProps(&e, "signal", "trace", "library", "ddtrace", "kind", "trace_call")
			out = append(out, e)
		}
		if !rbDdtraceCallRe.MatchString(src) {
			loc := rbDdtraceRequireRe.FindStringIndex(src)
			ln := lineOf(src, loc[0])
			e := makeEntity("ddtrace", string(types.EntityKindPattern), "trace_span", fp, "ruby", ln)
			setProps(&e, "signal", "trace", "library", "ddtrace", "kind", "require")
			out = append(out, e)
		}
	}

	// --- Skylight ---
	if rbSkylightRequireRe.MatchString(src) || rbSkylightInstrumentRe.MatchString(src) {
		for _, idx := range rbSkylightInstrumentRe.FindAllStringSubmatchIndex(src, -1) {
			ln := lineOf(src, idx[0])
			e := makeEntity("Skylight.instrument", string(types.EntityKindPattern), "trace_span", fp, "ruby", ln)
			setProps(&e, "signal", "trace", "library", "skylight", "kind", "instrument")
			out = append(out, e)
		}
		if !rbSkylightInstrumentRe.MatchString(src) {
			loc := rbSkylightRequireRe.FindStringIndex(src)
			ln := lineOf(src, loc[0])
			e := makeEntity("skylight", string(types.EntityKindPattern), "trace_span", fp, "ruby", ln)
			setProps(&e, "signal", "trace", "library", "skylight", "kind", "require")
			out = append(out, e)
		}
	}

	// --- OpenTracing ---
	if rbOpenTracingRequireRe.MatchString(src) || rbOpenTracingCallRe.MatchString(src) {
		for _, idx := range rbOpenTracingCallRe.FindAllStringSubmatchIndex(src, -1) {
			ln := lineOf(src, idx[0])
			callSite := src[idx[0]:idx[1]]
			name := strings.TrimSpace(callSite)
			if len(name) > 50 {
				name = name[:50]
			}
			e := makeEntity(name, string(types.EntityKindPattern), "trace_span", fp, "ruby", ln)
			setProps(&e, "signal", "trace", "library", "opentracing", "kind", "span_call")
			out = append(out, e)
		}
		if !rbOpenTracingCallRe.MatchString(src) {
			loc := rbOpenTracingRequireRe.FindStringIndex(src)
			ln := lineOf(src, loc[0])
			e := makeEntity("opentracing", string(types.EntityKindPattern), "trace_span", fp, "ruby", ln)
			setProps(&e, "signal", "trace", "library", "opentracing", "kind", "require")
			out = append(out, e)
		}
	}

	return out
}

// ---------------------------------------------------------------------------
// extractRailsObsLogging — Rails-specific log patterns
// ---------------------------------------------------------------------------

// extractRailsObsLogging handles logger.tagged, ActiveSupport::TaggedLogging,
// and lograge configuration — patterns specific to the Rails ecosystem.
func extractRailsObsLogging(src, fp string) []types.EntityRecord {
	var out []types.EntityRecord

	// logger.tagged("RequestID", request_id) { ... }
	for _, idx := range rbRailsLoggerTaggedRe.FindAllStringSubmatchIndex(src, -1) {
		receiver := src[idx[2]:idx[3]]
		// Skip non-logging receivers (common false positives)
		if receiver == "response" || receiver == "resp" || receiver == "request" ||
			receiver == "req" || receiver == "scope" || receiver == "result" {
			continue
		}
		ln := lineOf(src, idx[0])
		e := makeEntity(receiver+".tagged", string(types.EntityKindPattern), "log_statement", fp, "ruby", ln)
		setProps(&e, "signal", "log", "library", "rails_tagged_logging",
			"kind", "tagged_block", "receiver", receiver)
		out = append(out, e)
	}

	// ActiveSupport::TaggedLogging.new(logger)
	for _, idx := range rbRailsTaggedLoggingRe.FindAllStringSubmatchIndex(src, -1) {
		ln := lineOf(src, idx[0])
		e := makeEntity("ActiveSupport::TaggedLogging", string(types.EntityKindPattern), "logger", fp, "ruby", ln)
		setProps(&e, "signal", "log", "library", "active_support_tagged_logging",
			"kind", "instantiation")
		out = append(out, e)
	}

	// lograge: require 'lograge'
	if rbLogrageRequireRe.MatchString(src) {
		loc := rbLogrageRequireRe.FindStringIndex(src)
		ln := lineOf(src, loc[0])
		e := makeEntity("lograge", string(types.EntityKindPattern), "logger", fp, "ruby", ln)
		setProps(&e, "signal", "log", "library", "lograge", "kind", "require")
		out = append(out, e)
	}

	// lograge: config.lograge.enabled / config.lograge.formatter etc.
	for _, idx := range rbLogrageConfigRe.FindAllStringSubmatchIndex(src, -1) {
		// Skip duplicates if we already emitted a require entity
		ln := lineOf(src, idx[0])
		e := makeEntity("config.lograge", string(types.EntityKindPattern), "logger", fp, "ruby", ln)
		setProps(&e, "signal", "log", "library", "lograge", "kind", "config")
		out = append(out, e)
	}

	return out
}

// ---------------------------------------------------------------------------
// extractRailsObsMetrics — Rails-specific metric patterns
// ---------------------------------------------------------------------------

// extractRailsObsMetrics handles Yabeda.configure blocks — the main way
// Rails apps wire up yabeda instrumentation.
func extractRailsObsMetrics(src, fp string) []types.EntityRecord {
	var out []types.EntityRecord

	// Yabeda.configure do ... end
	for _, idx := range rbYabedaConfigureRe.FindAllStringSubmatchIndex(src, -1) {
		ln := lineOf(src, idx[0])
		e := makeEntity("Yabeda.configure", string(types.EntityKindPattern), "metric", fp, "ruby", ln)
		setProps(&e, "signal", "metric", "library", "yabeda", "kind", "configure_block")
		out = append(out, e)
	}

	return out
}

// ---------------------------------------------------------------------------
// extractRailsObsTracing — Rails-specific trace patterns
// ---------------------------------------------------------------------------

// extractRailsObsTracing handles ActiveSupport::Notifications.instrument and
// ActiveSupport::Notifications.subscribe — the Rails built-in instrumentation
// bus used for request lifecycle events, cache events, SQL queries, etc.
func extractRailsObsTracing(src, fp string) []types.EntityRecord {
	var out []types.EntityRecord

	// ActiveSupport::Notifications.instrument("sql.active_record") { ... }
	for _, idx := range rbASNInstrumentRe.FindAllStringSubmatchIndex(src, -1) {
		eventName := src[idx[2]:idx[3]]
		ln := lineOf(src, idx[0])
		e := makeEntity(eventName, string(types.EntityKindPattern), "trace_span", fp, "ruby", ln)
		setProps(&e, "signal", "trace", "library", "active_support_notifications",
			"kind", "instrument", "event_name", eventName)
		out = append(out, e)
	}

	// ActiveSupport::Notifications.subscribe("sql.active_record") { ... }
	for _, idx := range rbASNSubscribeRe.FindAllStringSubmatchIndex(src, -1) {
		eventName := src[idx[2]:idx[3]]
		ln := lineOf(src, idx[0])
		e := makeEntity(eventName, string(types.EntityKindPattern), "trace_span", fp, "ruby", ln)
		setProps(&e, "signal", "trace", "library", "active_support_notifications",
			"kind", "subscribe", "event_name", eventName)
		out = append(out, e)
	}

	return out
}

// ---------------------------------------------------------------------------
// Helper: metric-type normalisation
// ---------------------------------------------------------------------------

func rubyStatsdType(method string) string {
	switch method {
	case "increment", "decrement", "count":
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
	case "service_check":
		return "service_check"
	case "measure":
		return "timer"
	default:
		return method
	}
}
