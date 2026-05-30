package cpp

// observability.go — framework-agnostic observability scanner for C++ HTTP
// services.
//
// Detects three families of observability instrumentation:
//
//   - log_extraction: spdlog::info/warn/error/debug/critical(), LOG(INFO) <<
//     (glog), VLOG, printf-family format calls.  The spdlog and glog patterns
//     are a recording-win because internal/substrate/template_pattern_c_cpp.go
//     already sniffs these for template-pattern purposes; this extractor emits
//     SCOPE.Pattern entities so the capability flips at the framework level.
//
//   - metric_extraction: prometheus-cpp counter/gauge/histogram registration
//     (prometheus::BuildCounter/BuildGauge/BuildHistogram and
//     prometheus::Counter/Gauge/Histogram type usage), and opentelemetry-cpp
//     meter->CreateCounter / CreateHistogram.
//
//   - trace_extraction: opentelemetry-cpp tracer->StartSpan() /
//     tracer->StartActiveSpan(), jaeger/opentracing Tracer::StartSpan.
//
// Framework attribution: files are attributed to the first recognised C++
// HTTP framework marker (drogon/crow/pistache/cpprestsdk/oatpp/poco). Files
// with no recognized framework still emit entities with framework="" so they
// are captured without being credited to a per-framework cell.
//
// Honesty: partial — heuristic regex/substring match on source text. Does NOT
// perform import-resolution or data-flow analysis to confirm instrumentation is
// wired into a request handler. Fixtures prove the detection surface.

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

func init() {
	extractor.Register("custom_cpp_observability", &cppObsExtractor{})
}

type cppObsExtractor struct{}

func (e *cppObsExtractor) Language() string { return "custom_cpp_observability" }

// ---------------------------------------------------------------------------
// Signal catalog
// ---------------------------------------------------------------------------

type cppObsSignal struct {
	re        *regexp.Regexp
	otype     string // logging | metrics | tracing
	subtype   string
	nameGroup int // 0 = whole match, >0 = submatch index
}

var cppObsSignals = []cppObsSignal{
	// --- logging: spdlog ----------------------------------------------------
	// spdlog::info("...") / spdlog::error(...) etc.
	{regexp.MustCompile(`\bspdlog\s*::\s*(info|warn|error|debug|critical|trace)\s*\(`), "logging", "spdlog", 1},
	// spdlog logger instance: logger->info(...) / logger.info(...)
	{regexp.MustCompile(`\b(?:logger|log|spdlog_logger)\s*[-.]>?\s*(info|warn|error|debug|critical|trace)\s*\(`), "logging", "spdlog_instance", 1},

	// --- logging: glog / LOG macro ------------------------------------------
	// LOG(INFO) << "..."  / VLOG(1) << "..."
	{regexp.MustCompile(`\bLOG\s*\(\s*([A-Z_]+)\s*\)\s*<<`), "logging", "glog_LOG", 1},
	{regexp.MustCompile(`\bVLOG\s*\(\s*\d+\s*\)\s*<<`), "logging", "glog_VLOG", 0},
	// LOG_IF, DLOG
	{regexp.MustCompile(`\b(?:LOG_IF|DLOG|PLOG)\s*\(`), "logging", "glog_variant", 0},

	// --- logging: printf-family / std::cerr ---------------------------------
	// Already in template_pattern_c_cpp.go for template-pattern; emit here for
	// the observability capability cell (recording-win).
	{regexp.MustCompile(`\b(?:printf|fprintf|snprintf|puts)\s*\(`), "logging", "printf", 0},
	{regexp.MustCompile(`\bstd\s*::\s*(?:cerr|clog|cout)\s*<<`), "logging", "std_stream", 0},

	// --- metrics: prometheus-cpp --------------------------------------------
	// prometheus::BuildCounter().Name("...").Register(registry)
	{regexp.MustCompile(`\bprometheus\s*::\s*Build(?:Counter|Gauge|Histogram|Summary)\s*\(`), "metrics", "prometheus_build", 0},
	// prometheus::Counter / prometheus::Gauge / prometheus::Histogram type usage
	{regexp.MustCompile(`\bprometheus\s*::\s*(?:Counter|Gauge|Histogram|Summary|Family)\b`), "metrics", "prometheus_type", 0},
	// prometheus::Registry
	{regexp.MustCompile(`\bprometheus\s*::\s*Registry\b`), "metrics", "prometheus_registry", 0},
	// opentelemetry-cpp: meter->CreateCounter / CreateHistogram (with optional template arg)
	{regexp.MustCompile(`\bmeter\s*->\s*Create(?:Counter|Gauge|Histogram|ObservableGauge|ObservableCounter)(?:\s*<[^>]*>)?\s*\(`), "metrics", "otel_meter", 0},
	{regexp.MustCompile(`\bopentelemetry\s*::\s*metrics\s*::`), "metrics", "otel_metrics_ns", 0},

	// --- tracing: opentelemetry-cpp -----------------------------------------
	// auto tracer = provider->GetTracer(...)
	{regexp.MustCompile(`\bGetTracer\s*\(`), "tracing", "otel_get_tracer", 0},
	// tracer->StartSpan("name") / tracer->StartActiveSpan("name")
	{regexp.MustCompile(`\btracer\s*->\s*Start(?:Active)?Span\s*\(\s*"([^"]+)"`), "tracing", "otel_span_start", 1},
	// opentelemetry:: namespace usage (general)
	{regexp.MustCompile(`\bopentelemetry\s*::\s*trace\s*::`), "tracing", "otel_trace_ns", 0},
	// jaeger / opentracing: Tracer::StartSpan
	{regexp.MustCompile(`\bopentracing\s*::\s*Tracer\b`), "tracing", "opentracing_tracer", 0},
	{regexp.MustCompile(`\bjaeger\b`), "tracing", "jaeger", 0},
	// Zipkin client
	{regexp.MustCompile(`\bzipkin\b`), "tracing", "zipkin", 0},
}

func (e *cppObsExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/cpp")
	_, span := tracer.Start(ctx, "indexer.cpp_obs_extractor.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("file_path", file.Path),
		),
	)
	defer span.End()

	if len(file.Content) == 0 {
		return nil, nil
	}
	if file.Language != "cpp" {
		return nil, nil
	}

	src := string(file.Content)
	framework := detectCPPFramework(src)

	var entities []types.EntityRecord
	seen := make(map[string]bool)
	add := func(ent types.EntityRecord) {
		key := ent.Kind + ":" + ent.Name
		if seen[key] {
			return
		}
		seen[key] = true
		entities = append(entities, ent)
	}

	for _, sig := range cppObsSignals {
		for _, m := range sig.re.FindAllStringSubmatchIndex(src, -1) {
			detail := ""
			if sig.nameGroup > 0 && len(m) >= (sig.nameGroup+1)*2 {
				s := m[sig.nameGroup*2]
				en := m[sig.nameGroup*2+1]
				if s >= 0 && en >= 0 {
					detail = src[s:en]
				}
			}
			if detail == "" {
				detail = src[m[0]:m[1]]
			}
			detail = strings.TrimSpace(detail)
			if len(detail) > 80 {
				detail = detail[:80]
			}

			name := "obs:" + sig.otype + ":" + sig.subtype + ":" + detail
			ent := makeEntity(name, "SCOPE.Pattern", "", file.Path, file.Language, lineOf(src, m[0]))
			setProps(&ent,
				"framework", framework,
				"provenance", cppObsProvenance(framework, sig.otype),
				"pattern_kind", "observability",
				"observability_type", sig.otype,
				"observability_subtype", sig.subtype,
			)
			add(ent)
		}
	}

	span.SetAttributes(
		attribute.String("framework", framework),
		attribute.Int("entity_count", len(entities)),
	)
	return entities, nil
}

func cppObsProvenance(framework, otype string) string {
	fw := strings.ToUpper(framework)
	if fw == "" {
		fw = "CPP"
	}
	return "INFERRED_FROM_" + fw + "_" + strings.ToUpper(otype)
}
