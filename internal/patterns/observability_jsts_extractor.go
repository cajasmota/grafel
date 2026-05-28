package patterns

import (
	"regexp"

	"github.com/cajasmota/archigraph/internal/types"
)

// observabilityJSTSExtractor detects JS/TS observability instrumentation —
// structured logging, distributed tracing, and metrics — emitted by the
// common Node.js backend ecosystems. It exists because the language-agnostic
// loggingConfigExtractor only catches CommonJS `require('winston'|'pino')` and
// nothing for tracing/metrics, leaving the http_backend Observability
// taxonomy column entirely untracked (issue #2905).
//
// Each detected signal becomes a SCOPE.Config entity (subtype "observability")
// carrying:
//   - signal:  one of "log", "trace", "metric"
//   - library: the instrumentation library detected (e.g. "pino", "winston",
//     "opentelemetry", "prom-client", "console")
//   - kind:    constant "observability" (so downstream consumers can filter)
//
// SCOPE.Config is an already-registered EntityKind (internal/types/kinds.go),
// so no new Kind is introduced — we decorate the existing config-entity lane
// per the #2839 "prefer decorating over inventing Kinds" discipline.
type observabilityJSTSExtractor struct{}

// importToken matches both ESM (`import ... from 'pkg'` / `import 'pkg'`) and
// CommonJS (`require('pkg')`) references to a package name. The package name is
// supplied as a fully-formed alternation by the caller; it is anchored on the
// quote so `pino-http` does not match a bare `pino` token by accident.
func importToken(pkgAlternation string) *regexp.Regexp {
	return regexp.MustCompile(
		`(?:require\s*\(\s*|from\s+|import\s+)['"](?:` + pkgAlternation + `)['"]`)
}

var (
	// Structured-logging libraries (import/require of the package).
	obsLogPino    = importToken(`pino|pino-http`)
	obsLogWinston = importToken(`winston|express-winston`)
	obsLogBunyan  = importToken(`bunyan`)
	obsLogMorgan  = importToken(`morgan`)
	obsLogRoarr   = importToken(`roarr`)
	obsLogLoglvl  = importToken(`loglevel`)

	// console.* used as a deliberate structured-logging sink. We require a
	// call form (console.info/log/warn/error/debug(...)) so a stray
	// `console` identifier in a comment/string does not trigger.
	obsLogConsole = regexp.MustCompile(`console\.(?:log|info|warn|error|debug)\s*\(`)

	// OpenTelemetry tracing: the API package, an SDK trace package, or the
	// canonical tracer/span call surface.
	obsTraceOtel = regexp.MustCompile(
		`['"]@opentelemetry/(?:api|sdk-trace-node|sdk-trace-base|auto-instrumentations-node)['"]` +
			`|\btrace\.getTracer\s*\(` +
			`|\.startActiveSpan\s*\(` +
			`|\.startSpan\s*\(`)

	// Sentry performance tracing (startTransaction / startSpan on the SDK).
	obsTraceSentry = regexp.MustCompile(
		`['"]@sentry/(?:node|tracing)['"]|Sentry\.startTransaction\s*\(`)

	// Metrics: prom-client, or the OTel metrics surface.
	obsMetricProm = importToken(`prom-client`)
	obsMetricOtel = regexp.MustCompile(
		`['"]@opentelemetry/(?:metrics|sdk-metrics)['"]` +
			`|\.getMeter\s*\(` +
			`|\.createCounter\s*\(` +
			`|\.createHistogram\s*\(` +
			`|\.createUpDownCounter\s*\(`)
)

func (o *observabilityJSTSExtractor) Category() string { return "observability" }

func (o *observabilityJSTSExtractor) AppliesTo(src string) bool {
	return obsLogPino.MatchString(src) ||
		obsLogWinston.MatchString(src) ||
		obsLogBunyan.MatchString(src) ||
		obsLogMorgan.MatchString(src) ||
		obsLogRoarr.MatchString(src) ||
		obsLogLoglvl.MatchString(src) ||
		obsLogConsole.MatchString(src) ||
		obsTraceOtel.MatchString(src) ||
		obsTraceSentry.MatchString(src) ||
		obsMetricProm.MatchString(src) ||
		obsMetricOtel.MatchString(src)
}

func (o *observabilityJSTSExtractor) Detect(filePath, language, src string) []types.EntityRecord {
	if language != "javascript" && language != "typescript" {
		return nil
	}

	var results []types.EntityRecord
	seen := map[string]bool{}

	emit := func(signal, library string) {
		key := signal + ":" + library
		if seen[key] {
			return
		}
		seen[key] = true
		results = append(results, makeEntity(filePath,
			"observability_"+signal+"_"+library,
			string(types.EntityKindConfig), "observability", language, 1,
			map[string]string{
				"kind":    "observability",
				"signal":  signal,
				"library": library,
			}))
	}

	// Logging signals. A file may wire more than one logger (e.g. morgan for
	// HTTP access + winston for app logs), so each library is emitted
	// independently rather than first-match-wins.
	if obsLogPino.MatchString(src) {
		emit("log", "pino")
	}
	if obsLogWinston.MatchString(src) {
		emit("log", "winston")
	}
	if obsLogBunyan.MatchString(src) {
		emit("log", "bunyan")
	}
	if obsLogMorgan.MatchString(src) {
		emit("log", "morgan")
	}
	if obsLogRoarr.MatchString(src) {
		emit("log", "roarr")
	}
	if obsLogLoglvl.MatchString(src) {
		emit("log", "loglevel")
	}
	if obsLogConsole.MatchString(src) {
		emit("log", "console")
	}

	// Tracing signals.
	if obsTraceOtel.MatchString(src) {
		emit("trace", "opentelemetry")
	}
	if obsTraceSentry.MatchString(src) {
		emit("trace", "sentry")
	}

	// Metric signals.
	if obsMetricProm.MatchString(src) {
		emit("metric", "prom-client")
	}
	if obsMetricOtel.MatchString(src) {
		emit("metric", "opentelemetry")
	}

	return results
}

func init() {
	Register(&observabilityJSTSExtractor{})
}
