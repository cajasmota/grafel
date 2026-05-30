// observability.go — Lua observability extractor (log_extraction, metric_extraction, trace_extraction).
//
// Covers the Observability lane for Lua web frameworks:
//
//	Logging (log_extraction):
//	  - OpenResty: ngx.log(ngx.ERR/WARN/INFO/DEBUG, ...) — nginx-native logging
//	  - OpenResty: ngx.log with resty.log module
//	  - Lapis: logging via io.write / print / ngx.log inside handlers
//	  - require("resty.logger.socket") — async remote logging
//
//	Metrics (metric_extraction):
//	  - prometheus: require("resty.prometheus") / prometheus:counter / prometheus:histogram
//	  - statsd: require("resty.statsd") / statsd:increment / statsd:timing
//
//	Tracing (trace_extraction):
//	  - OpenTelemetry: require("opentelemetry") or require("opentelemetry.*")
//	  - Zipkin: require("resty.zipkin") or zipkin tracing patterns
//	  - Jaeger: require("resty.jaeger")
//	  - Kong tracing: kong.tracing.start_span / span:set_attribute
//
// All cells are partial: import + call-site heuristics, no cross-file dataflow.
package lua

import (
	"context"
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func init() {
	extractor.Register("lua_observability", &luaObservabilityExtractor{})
}

// luaObservabilityExtractor detects observability instrumentation in Lua source files.
type luaObservabilityExtractor struct{}

func (e *luaObservabilityExtractor) Language() string { return "lua_observability" }

// ---------------------------------------------------------------------------
// Compiled regexes
// ---------------------------------------------------------------------------

var (
	// ------------ log_extraction ------------

	// ngx.log(ngx.ERR, ...) / ngx.log(ngx.WARN, ...) / ngx.log(ngx.INFO, ...)
	reNgxLog = regexp.MustCompile(
		`(?m)\bngx\.log\s*\(\s*(ngx\.(?:STDERR|EMERG|ALERT|CRIT|ERR|WARN|NOTICE|INFO|DEBUG)|[0-9]+)\s*[,)]`)

	// require("resty.logger.socket")
	reLuaLoggerSocket = regexp.MustCompile(
		`(?m)\brequire\s*[("']resty\.logger\.socket["']?\)?`)

	// Generic Lua print/io.write for Lapis logging
	reLuaPrint = regexp.MustCompile(
		`(?m)(?:^|\s)(?:print|io\.write)\s*\(`)

	// ------------ metric_extraction ------------

	// require("resty.prometheus")
	reLuaPrometheusRequire = regexp.MustCompile(
		`(?m)\brequire\s*[("']resty\.prometheus["']?\)?`)

	// prometheus:counter / prometheus:histogram / prometheus:gauge
	reLuaPrometheusOp = regexp.MustCompile(
		`(?m)\bprometheus\s*:\s*(counter|histogram|gauge|summary)\s*\(`)

	// require("resty.statsd") or statsd-related patterns
	reLuaStatsdRequire = regexp.MustCompile(
		`(?m)\brequire\s*[("']resty\.statsd["']?\)?`)

	// statsd:increment / statsd:timing / statsd:gauge
	reLuaStatsdOp = regexp.MustCompile(
		`(?m)\bstatsd\s*:\s*(increment|timing|gauge|decrement|count)\s*\(`)

	// ------------ trace_extraction ------------

	// OpenTelemetry: require("opentelemetry") or require("opentelemetry.trace")
	reLuaOTelRequire = regexp.MustCompile(
		`(?m)\brequire\s*[("']opentelemetry(?:\.[a-z._]+)?["']?\)?`)

	// OpenTelemetry span operations
	reLuaOTelSpan = regexp.MustCompile(
		`(?m)\b(?:tracer|span)\s*[.:]\s*(?:start_span|set_attribute|end_span|record_error|add_event)\s*\(`)

	// Kong tracing: kong.tracing.start_span
	reLuaKongTracing = regexp.MustCompile(
		`(?m)\bkong\.tracing\s*\.\s*(?:start_span|new_span)\s*\(`)

	// Zipkin: require("resty.zipkin")
	reLuaZipkinRequire = regexp.MustCompile(
		`(?m)\brequire\s*[("']resty\.zipkin["']?\)?`)
)

// Extract implements extractor.Extractor.
func (e *luaObservabilityExtractor) Extract(_ context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	if len(file.Content) == 0 {
		return nil, nil
	}
	src := string(file.Content)

	hasObs := strings.Contains(src, "ngx.log") || strings.Contains(src, "resty.logger") ||
		strings.Contains(src, "prometheus") || strings.Contains(src, "statsd") ||
		strings.Contains(src, "opentelemetry") || strings.Contains(src, "kong.tracing") ||
		strings.Contains(src, "zipkin") || strings.Contains(src, "jaeger")
	if !hasObs {
		return nil, nil
	}

	var out []types.EntityRecord

	// --- log_extraction ---

	for _, idx := range reNgxLog.FindAllStringSubmatchIndex(src, -1) {
		level := src[idx[2]:idx[3]]
		ln := lineOf(src, idx[0])
		entity := makeEntity("ngx_log:"+level, string(types.EntityKindPattern), "log_call", file.Path, "lua", ln)
		setProps(&entity, "signal", "observability", "framework", "openresty", "kind", "log", "level", level)
		out = append(out, entity)
	}

	if reLuaLoggerSocket.MatchString(src) {
		idx := reLuaLoggerSocket.FindStringIndex(src)
		ln := lineOf(src, idx[0])
		entity := makeEntity("resty_logger_socket", string(types.EntityKindPattern), "log_config", file.Path, "lua", ln)
		setProps(&entity, "signal", "observability", "library", "resty.logger.socket", "kind", "async_log")
		out = append(out, entity)
	}

	for _, idx := range reLuaPrint.FindAllStringIndex(src, -1) {
		ln := lineOf(src, idx[0])
		entity := makeEntity("lua_print_log", string(types.EntityKindPattern), "log_call", file.Path, "lua", ln)
		setProps(&entity, "signal", "observability", "framework", "lapis", "kind", "print_log")
		out = append(out, entity)
	}

	// --- metric_extraction ---

	if reLuaPrometheusRequire.MatchString(src) {
		idx := reLuaPrometheusRequire.FindStringIndex(src)
		ln := lineOf(src, idx[0])
		entity := makeEntity("resty_prometheus_import", string(types.EntityKindPattern), "metric_config", file.Path, "lua", ln)
		setProps(&entity, "signal", "observability", "library", "resty.prometheus", "kind", "prometheus_import")
		out = append(out, entity)
	}

	for _, idx := range reLuaPrometheusOp.FindAllStringSubmatchIndex(src, -1) {
		metricType := src[idx[2]:idx[3]]
		ln := lineOf(src, idx[0])
		entity := makeEntity("prometheus_"+metricType, string(types.EntityKindPattern), "metric_call", file.Path, "lua", ln)
		setProps(&entity, "signal", "observability", "library", "resty.prometheus", "kind", metricType)
		out = append(out, entity)
	}

	if reLuaStatsdRequire.MatchString(src) {
		idx := reLuaStatsdRequire.FindStringIndex(src)
		ln := lineOf(src, idx[0])
		entity := makeEntity("resty_statsd_import", string(types.EntityKindPattern), "metric_config", file.Path, "lua", ln)
		setProps(&entity, "signal", "observability", "library", "resty.statsd", "kind", "statsd_import")
		out = append(out, entity)
	}

	for _, idx := range reLuaStatsdOp.FindAllStringSubmatchIndex(src, -1) {
		op := src[idx[2]:idx[3]]
		ln := lineOf(src, idx[0])
		entity := makeEntity("statsd_"+op, string(types.EntityKindPattern), "metric_call", file.Path, "lua", ln)
		setProps(&entity, "signal", "observability", "library", "resty.statsd", "kind", op)
		out = append(out, entity)
	}

	// --- trace_extraction ---

	if reLuaOTelRequire.MatchString(src) {
		idx := reLuaOTelRequire.FindStringIndex(src)
		ln := lineOf(src, idx[0])
		entity := makeEntity("lua_otel_import", string(types.EntityKindPattern), "trace_config", file.Path, "lua", ln)
		setProps(&entity, "signal", "observability", "library", "opentelemetry", "kind", "otel_import")
		out = append(out, entity)
	}

	for _, idx := range reLuaOTelSpan.FindAllStringIndex(src, -1) {
		ln := lineOf(src, idx[0])
		entity := makeEntity("otel_span_op", string(types.EntityKindPattern), "trace_call", file.Path, "lua", ln)
		setProps(&entity, "signal", "observability", "library", "opentelemetry", "kind", "span_op")
		out = append(out, entity)
	}

	if reLuaKongTracing.MatchString(src) {
		idx := reLuaKongTracing.FindStringIndex(src)
		ln := lineOf(src, idx[0])
		entity := makeEntity("kong_tracing_span", string(types.EntityKindPattern), "trace_call", file.Path, "lua", ln)
		setProps(&entity, "signal", "observability", "framework", "kong", "kind", "kong_tracing")
		out = append(out, entity)
	}

	if reLuaZipkinRequire.MatchString(src) {
		idx := reLuaZipkinRequire.FindStringIndex(src)
		ln := lineOf(src, idx[0])
		entity := makeEntity("resty_zipkin_import", string(types.EntityKindPattern), "trace_config", file.Path, "lua", ln)
		setProps(&entity, "signal", "observability", "library", "resty.zipkin", "kind", "zipkin_import")
		out = append(out, entity)
	}

	return out, nil
}
