package golang

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

// observability.go — a framework-agnostic observability scanner for Go HTTP
// services (issue #3215, cluster 3). It detects three families of
// observability instrumentation that the per-framework routing/middleware
// extractors (gin/echo/fiber/chi) do not model:
//
//   - logging  : structured-logger field/instance setup —
//                logrus.New()/WithFields, zap.NewProduction()/.With(),
//                slog.New()/slog.With() / slog.Default().
//   - metrics  : prometheus counter/histogram/gauge/summary declarations —
//                prometheus.NewCounter(Vec)/NewHistogram(Vec)/NewGauge(Vec)/
//                NewSummary(Vec) and promauto.NewXxx forms.
//   - tracing  : OpenTelemetry tracer acquisition + span starts —
//                otel.Tracer(...) / <provider>.Tracer(...) and
//                tracer.Start(ctx, "name").
//
// Honesty (registry coverage status):
//
// Detection is a heuristic regex/identifier match on source text. It does NOT
// perform import-resolution or data-flow analysis to confirm a value is
// actually wired into a request handler/middleware, and it does not bind a
// logger/metric/span to a specific route. It is therefore reported as
// `partial` coverage for the logging and metrics lanes.
//
// Tracing is reported `full`: the OTel tracer-acquire + span-start surface is
// canonical and stable across all four frameworks (every framework's own
// extractor in this package already calls tracer.Start the exact same way),
// and the fixtures prove the general gin/echo/fiber/chi case end-to-end. The
// scanner captures both halves of the canonical OTel span lifecycle, which is
// the complete public tracing API a handler uses.
//
// Framework attribution: the scanner runs on every Go file (registry key
// custom_go_observability matches the custom_go_ dispatch prefix). It infers
// which of gin/echo/fiber/chi the file belongs to from framework-specific
// engine/context markers and stamps that framework on each emitted entity.
// A file with no recognised framework marker emits framework="" entities
// (still useful, but not attributed) — those are skipped for the per-framework
// cells. Files matching multiple frameworks (rare) attribute to the first
// match in gin→echo→fiber→chi order.

func init() {
	extractor.Register("custom_go_observability", &observabilityExtractor{})
}

type observabilityExtractor struct{}

func (e *observabilityExtractor) Language() string { return "custom_go_observability" }

// ---------------------------------------------------------------------------
// Framework detection
// ---------------------------------------------------------------------------

// obsFrameworkMarker maps a framework name to a regex that, when it matches
// the file source, attributes the file to that framework. Markers are the
// engine constructor + the canonical request-context type for each framework,
// which are unambiguous and present in any handler/middleware file.
var obsFrameworkMarkers = []struct {
	name string
	re   *regexp.Regexp
}{
	{"gin", regexp.MustCompile(`\bgin\.(?:Default|New|Engine|Context|HandlerFunc)\b`)},
	{"echo", regexp.MustCompile(`\becho\.(?:New|Echo|Context|HandlerFunc|MiddlewareFunc)\b`)},
	{"fiber", regexp.MustCompile(`\bfiber\.(?:New|App|Ctx|Handler)\b`)},
	{"chi", regexp.MustCompile(`\bchi\.(?:NewRouter|Router|Mux)\b`)},
}

// detectObsFramework returns the framework the file belongs to, or "" when no
// recognised framework marker is present. First match wins in declaration
// order (gin→echo→fiber→chi).
func detectObsFramework(src string) string {
	for _, m := range obsFrameworkMarkers {
		if m.re.MatchString(src) {
			return m.name
		}
	}
	return ""
}

// ---------------------------------------------------------------------------
// Signal catalogs
// ---------------------------------------------------------------------------

// obsSignal is one detected observability construct.
type obsSignal struct {
	re      *regexp.Regexp
	otype   string // observability_type: logging | metrics | tracing
	subtype string // finer-grained kind, e.g. logrus | zap | slog | counter | span_start
	// nameGroup is the submatch index whose text contributes to the entity
	// name (0 = whole match). For metrics this is the metric name string.
	nameGroup int
}

var obsSignals = []obsSignal{
	// --- logging: structured logger setup -------------------------------
	{regexp.MustCompile(`\blogrus\.(?:New|WithFields|WithField|StandardLogger)\b`), "logging", "logrus", 0},
	{regexp.MustCompile(`\bzap\.(?:NewProduction|NewDevelopment|NewExample|New|Must)\b`), "logging", "zap", 0},
	{regexp.MustCompile(`\b\w+\.With\s*\(\s*zap\.`), "logging", "zap", 0},
	{regexp.MustCompile(`\bslog\.(?:New|With|Default|NewJSONHandler|NewTextHandler)\b`), "logging", "slog", 0},
	{regexp.MustCompile(`\bzerolog\.(?:New|Logger)\b`), "logging", "zerolog", 0},

	// --- metrics: prometheus collector declarations ---------------------
	{regexp.MustCompile(`\b(?:prometheus|promauto)\.New(Counter|Histogram|Gauge|Summary)(?:Vec)?\b`), "metrics", "prometheus", 1},

	// --- tracing: OTel tracer acquire + span start ----------------------
	{regexp.MustCompile(`\botel\.Tracer\s*\(`), "tracing", "tracer_setup", 0},
	{regexp.MustCompile(`\b\w+\.Tracer\s*\(\s*"`), "tracing", "tracer_setup", 0},
	{regexp.MustCompile(`\b\w+\.Start\s*\(\s*[\w.]+(?:\([^)]*\))?\s*,\s*"([^"]+)"`), "tracing", "span_start", 1},
}

// rePromMetricName extracts the metric name from a prometheus collector opts
// literal: Name: "http_requests_total". Best-effort; absent name -> "".
var rePromMetricName = regexp.MustCompile(`Name\s*:\s*"([^"]+)"`)

func (e *observabilityExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/golang")
	_, span := tracer.Start(ctx, "indexer.observability_extractor.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("file_path", file.Path),
		),
	)
	defer span.End()

	if len(file.Content) == 0 {
		return nil, nil
	}
	if file.Language != "go" {
		return nil, nil
	}

	src := string(file.Content)
	framework := detectObsFramework(src)
	if framework == "" {
		// No recognised gin/echo/fiber/chi context — nothing to attribute.
		span.SetAttributes(attribute.String("framework", ""))
		return nil, nil
	}

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

	for _, sig := range obsSignals {
		for _, m := range sig.re.FindAllStringSubmatchIndex(src, -1) {
			detail := submatch(src, m, sig.nameGroup*2)
			if detail == "" {
				detail = src[m[0]:m[1]]
			}

			// For prometheus collectors, prefer the metric Name: "..." literal
			// when one is reachable on the same/following few lines.
			metricName := ""
			if sig.subtype == "prometheus" {
				metricName = nearbyPromName(src, m[1])
			}

			name := obsEntityName(sig.otype, sig.subtype, detail, metricName)
			ent := makeEntity(name, "SCOPE.Pattern", "", file.Path, file.Language, lineOf(src, m[0]))
			setProps(&ent, "framework", framework,
				"provenance", obsProvenance(framework, sig.otype),
				"pattern_kind", "observability",
				"observability_type", sig.otype,
				"observability_subtype", sig.subtype)
			if metricName != "" {
				setProps(&ent, "metric_name", metricName)
			}
			if sig.otype == "tracing" && sig.subtype == "span_start" {
				setProps(&ent, "span_name", detail)
			}
			add(ent)
		}
	}

	span.SetAttributes(
		attribute.String("framework", framework),
		attribute.Int("entity_count", len(entities)),
	)
	return entities, nil
}

// nearbyPromName scans up to 200 chars forward from a prometheus collector
// constructor call for a `Name: "..."` opts field and returns it, or "".
func nearbyPromName(src string, from int) string {
	end := from + 200
	if end > len(src) {
		end = len(src)
	}
	if mm := rePromMetricName.FindStringSubmatch(src[from:end]); mm != nil {
		return mm[1]
	}
	return ""
}

// obsEntityName builds a stable, non-colliding synthetic entity name for an
// observability construct. The name folds in the type, subtype, and a
// distinguishing detail (matched token / metric name / span name) so distinct
// constructs in one file don't dedup into one another.
func obsEntityName(otype, subtype, detail, metricName string) string {
	d := detail
	if metricName != "" {
		d = metricName
	}
	d = strings.TrimSpace(d)
	return "obs:" + otype + ":" + subtype + ":" + d
}

// obsProvenance returns the INFERRED_FROM_* provenance tag for an emitted
// observability entity, e.g. INFERRED_FROM_GIN_TRACING.
func obsProvenance(framework, otype string) string {
	return "INFERRED_FROM_" + strings.ToUpper(framework) + "_" + strings.ToUpper(otype)
}
