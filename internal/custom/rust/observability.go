package rust

// observability.go — framework-agnostic observability scanner for Rust HTTP
// services (issues #3269, #3416). Detects three families of observability
// instrumentation across the 10 Rust HTTP frameworks and captures the
// *specific name* at each call site where the source provides a string
// literal:
//
//   - logging  : tracing crate info!/warn!/error!/debug!/trace! (qualified
//                `tracing::info!` and bare `info!`), `log` crate `log::info!`,
//                `event!(Level::INFO, ...)`, `slog::info!`, and the
//                #[instrument] attribute. Captures the static message head
//                when present.
//   - metrics  : `metrics` crate counter!/gauge!/histogram!("name", ...),
//                `prometheus` register_*!("name") macros and
//                IntCounter/Counter/Histogram::new("name") ctors / Opts::new,
//                and opentelemetry meter.u64_counter("name") builders.
//                Captures the metric NAME (first string-literal arg).
//   - tracing  : `tracing` span!(Level::INFO, "name") / info_span!("name"),
//                opentelemetry global::tracer("svc"), tracer.start("name")
//                and tracer.span_builder("name"), plus #[instrument]. Captures
//                the span / tracer NAME.
//
// Each entity carries observability_library, observability_kind (macro level /
// metric kind) and observability_name (the captured concrete value) props.
//
// Honesty (per-capability, see registry notes):
//
//	metric_extraction / trace_extraction — the metric name and span name are
//	literal string arguments at the call site, so they are captured exactly
//	and asserted by value-asserting tests. These do NOT require cross-file
//	resolution and are recorded `full`.
//
//	log_extraction — stays `partial`. The static message head is captured when
//	it is a leading string literal, but log messages are frequently format
//	strings with interpolated/structured fields, and binding a logger to its
//	subscriber/appender is a cross-file concern (the same limitation recorded
//	for PHP/Java/Ruby per-framework log cells). The detection surface is
//	proven by fixtures; wiring to subscribers requires semantic analysis
//	beyond this scanner.
//
//	No capability binds a logger/metric/span to a specific route or to its
//	exporter/subscriber across files — that cross-file dataflow stays out of
//	scope for this call-site scanner.
//
// Framework attribution: the scanner attributes files to a Rust HTTP
// framework using framework-specific import/usage markers (use actix_web, use
// axum, use rocket, etc.). A file with no recognised framework marker still
// emits entities but with framework="" so they are not credited to a
// per-framework cell.

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
	extractor.Register("custom_rust_observability", &rustObsExtractor{})
}

type rustObsExtractor struct{}

func (e *rustObsExtractor) Language() string { return "custom_rust_observability" }

// ---------------------------------------------------------------------------
// Framework detection
// ---------------------------------------------------------------------------

// rustObsFrameworkMarker maps a Rust HTTP framework name to a regex that,
// when it matches the file source, attributes the file to that framework.
var rustObsFrameworkMarkers = []struct {
	name string
	re   *regexp.Regexp
}{
	{"actix", regexp.MustCompile(`\buse\s+actix_web\b|actix_web::`)},
	{"axum", regexp.MustCompile(`\buse\s+axum\b|axum::`)},
	{"rocket", regexp.MustCompile(`\buse\s+rocket\b|rocket::`)},
	{"poem", regexp.MustCompile(`\buse\s+poem\b|poem::`)},
	{"salvo", regexp.MustCompile(`\buse\s+salvo\b|salvo::`)},
	{"tide", regexp.MustCompile(`\buse\s+tide\b|tide::`)},
	{"warp", regexp.MustCompile(`\buse\s+warp\b|warp::`)},
	{"tower", regexp.MustCompile(`\buse\s+tower\b|tower::`)},
	{"hyper", regexp.MustCompile(`\buse\s+hyper\b|hyper::`)},
	{"gotham", regexp.MustCompile(`\buse\s+gotham\b|gotham::`)},
	// Issue #3981 — tonic (gRPC) and async-graphql (GraphQL) services emit the
	// same framework-agnostic observability signals (tracing spans / metrics /
	// #[instrument]) recognised by rustObsSignals; adding these import markers
	// attributes those signals to the correct per-framework coverage cell
	// instead of leaving framework="". The signal regexes themselves are
	// unchanged — these markers only affect attribution.
	{"tonic", regexp.MustCompile(`\buse\s+tonic\b|tonic::`)},
	{"async-graphql", regexp.MustCompile(`\buse\s+async_graphql\b|async_graphql::`)},
}

func detectRustObsFramework(src string) string {
	for _, m := range rustObsFrameworkMarkers {
		if m.re.MatchString(src) {
			return m.name
		}
	}
	return ""
}

// ---------------------------------------------------------------------------
// Signal catalogs
// ---------------------------------------------------------------------------

type rustObsSignal struct {
	re      *regexp.Regexp
	otype   string // logging | metrics | tracing
	subtype string // e.g. tracing_macro | instrument | prometheus | otel_span
	library string // tracing | log | slog | metrics | prometheus | opentelemetry
	// kindGroup: submatch carrying the macro/level distinguisher (0=none).
	kindGroup int
	// valueGroup: submatch carrying the specific captured VALUE — the log
	// message, metric name, or span name (0=none). This is the per-call-site
	// detail that lets metric/trace extraction assert a concrete name.
	valueGroup int
}

// rustObsSignals is the per-call-site signal catalog. For each family the
// regexes try to capture the *specific name* (metric name / span name) or the
// *static message head* (logging) as a submatch, so the emitted entity carries
// a concrete value rather than just a macro kind.
var rustObsSignals = []rustObsSignal{
	// ===================================================================
	// logging
	// ===================================================================
	// tracing crate, fully-qualified: tracing::info!("msg"...) — capture
	// level (group 1) + leading string-literal message head (group 2, opt).
	{regexp.MustCompile(`\btracing::(info|warn|error|debug|trace)!\s*\(\s*(?:target\s*:\s*"[^"]*"\s*,\s*)?(?:"([^"]*)")?`), "logging", "tracing_macro", "tracing", 1, 2},
	// tracing crate, bare macro after `use tracing::...`: info!("msg").
	{regexp.MustCompile(`(?m)(?:^|[;{}\s])(info|warn|error|debug|trace)!\s*\(\s*(?:target\s*:\s*"[^"]*"\s*,\s*)?(?:"([^"]*)")?`), "logging", "tracing_macro_bare", "tracing", 1, 2},
	// log crate: log::info!("msg") / log::error!(target: "t", "msg").
	{regexp.MustCompile(`\blog::(info|warn|error|debug|trace)!\s*\(\s*(?:target\s*:\s*"[^"]*"\s*,\s*)?(?:"([^"]*)")?`), "logging", "log_macro", "log", 1, 2},
	// tracing event! macro: event!(Level::INFO, "msg") — capture level + msg.
	{regexp.MustCompile(`\b(?:tracing::)?event!\s*\(\s*(?:tracing::)?Level::([A-Z]+)\s*,\s*(?:target\s*:\s*"[^"]*"\s*,\s*)?(?:"([^"]*)")?`), "logging", "tracing_event", "tracing", 1, 2},
	// slog crate: info!(logger, "msg") / slog::info!(log, "msg").
	{regexp.MustCompile(`\bslog::(info|warn|error|debug|trace)!\s*\(\s*[A-Za-z_]\w*\s*,\s*(?:"([^"]*)")?`), "logging", "slog_macro", "slog", 1, 2},
	// #[instrument] attribute — serves both logging and tracing.
	{regexp.MustCompile(`#\[(?:tracing::)?instrument(?:\s*\([^)]*\))?\]`), "logging", "instrument", "tracing", 0, 0},

	// ===================================================================
	// metrics — capture the metric NAME (first string-literal positional arg)
	// ===================================================================
	// metrics crate: counter!/gauge!/histogram!("name", ...).
	{regexp.MustCompile(`\b(?:metrics::)?(counter|gauge|histogram)!\s*\(\s*"([^"]+)"`), "metrics", "metrics_macro", "metrics", 1, 2},
	// prometheus register_* macros: register_counter!("name", "help").
	{regexp.MustCompile(`\b(?:prometheus::)?register_(counter|gauge|histogram|int_counter|int_gauge)(?:_vec)?(?:_with_registry)?\s*!\s*\(\s*"([^"]+)"`), "metrics", "prometheus_macro", "prometheus", 1, 2},
	// prometheus typed ctor: IntCounter::new("name", "help") / Counter::new(...).
	{regexp.MustCompile(`\b(?:prometheus::)?(IntCounter|IntGauge|Counter|Gauge|Histogram)(?:Vec)?::new\s*\(\s*"([^"]+)"`), "metrics", "prometheus_ctor", "prometheus", 1, 2},
	// prometheus Opts::new("name", "help") — name-bearing opts builder.
	{regexp.MustCompile(`\b(?:prometheus::)?(?:Opts|HistogramOpts)::new\s*\(\s*"([^"]+)"`), "metrics", "prometheus_opts", "prometheus", 0, 1},
	// opentelemetry metrics: meter.u64_counter("name") / f64_histogram("name").
	{regexp.MustCompile(`\b(?:meter|metrics?)\.(?:[uif]64_)?(counter|gauge|histogram|up_down_counter|observable_counter|observable_gauge)\s*\(\s*"([^"]+)"`), "metrics", "otel_meter", "opentelemetry", 1, 2},

	// ===================================================================
	// tracing — capture the span NAME
	// ===================================================================
	// tracing span!(Level::INFO, "name") — capture level + span name.
	{regexp.MustCompile(`\b(?:tracing::)?span!\s*\(\s*(?:tracing::)?Level::([A-Z]+)\s*,\s*"([^"]+)"`), "tracing", "tracing_span", "tracing", 1, 2},
	// level-specific span macros: info_span!("name") / error_span!("name").
	{regexp.MustCompile(`\b(?:tracing::)?(info|warn|error|debug|trace)_span!\s*\(\s*"([^"]+)"`), "tracing", "tracing_level_span", "tracing", 1, 2},
	// opentelemetry tracer: global::tracer("svc") — capture tracer name.
	{regexp.MustCompile(`\bopentelemetry(?:_sdk)?::global::tracer\s*\(\s*"([^"]+)"`), "tracing", "otel_tracer", "opentelemetry", 0, 1},
	{regexp.MustCompile(`\bopentelemetry(?:_sdk)?::[a-z_]+::tracer\s*\(\s*"([^"]+)"`), "tracing", "otel_tracer", "opentelemetry", 0, 1},
	// otel span start: tracer.start("name") / start_with_context(cx, "name").
	{regexp.MustCompile(`\btracer\.start(?:_with_context)?\s*\(\s*(?:[^,)"]+,\s*)?"([^"]+)"`), "tracing", "otel_span_start", "opentelemetry", 0, 1},
	// otel span builder: tracer.span_builder("name").
	{regexp.MustCompile(`\.span_builder\s*\(\s*"([^"]+)"`), "tracing", "otel_span_builder", "opentelemetry", 0, 1},
	// #[instrument] again — also a tracing signal (span name derived from fn).
	{regexp.MustCompile(`#\[(?:tracing::)?instrument(?:\s*\([^)]*\))?\]`), "tracing", "instrument", "tracing", 0, 0},
}

func (e *rustObsExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/rust")
	_, span := tracer.Start(ctx, "indexer.rust_obs_extractor.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("file_path", file.Path),
		),
	)
	defer span.End()

	if len(file.Content) == 0 {
		return nil, nil
	}
	if file.Language != "rust" {
		return nil, nil
	}

	src := string(file.Content)
	framework := detectRustObsFramework(src)

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

	grp := func(m []int, g int) string {
		if g <= 0 || len(m) < (g+1)*2 {
			return ""
		}
		s, e := m[g*2], m[g*2+1]
		if s < 0 || e < 0 {
			return ""
		}
		return strings.TrimSpace(src[s:e])
	}

	for _, sig := range rustObsSignals {
		for _, m := range sig.re.FindAllStringSubmatchIndex(src, -1) {
			kind := grp(m, sig.kindGroup)   // macro level / metric kind
			value := grp(m, sig.valueGroup) // log message / metric name / span name

			// The disambiguating detail in the entity name prefers the macro
			// kind (info/counter/...); the captured value is carried as a
			// dedicated property so downstream consumers can assert on the
			// concrete metric/span name without parsing the entity name.
			detail := kind
			if detail == "" {
				detail = src[m[0]:m[1]]
				detail = strings.TrimSpace(detail)
			}

			name := "obs:" + sig.otype + ":" + sig.subtype + ":" + detail
			// Append the captured concrete name so two metrics/spans that
			// share a macro kind but differ by name remain distinct entities
			// (and survive the Kind+Name dedup below).
			if value != "" {
				name += ":" + value
			}
			ent := makeEntity(name, "SCOPE.Pattern", "", file.Path, file.Language, lineOf(src, m[0]))
			setProps(&ent,
				"framework", framework,
				"provenance", rustObsProvenance(framework, sig.otype),
				"pattern_kind", "observability",
				"observability_type", sig.otype,
				"observability_subtype", sig.subtype,
				"observability_library", sig.library,
			)
			if kind != "" {
				setProps(&ent, "observability_kind", kind)
			}
			// observability_name carries the captured concrete value:
			//   metrics  -> metric name     (e.g. "requests_total")
			//   tracing  -> span/tracer name (e.g. "db_query")
			//   logging  -> static message head (heuristic, may be empty)
			if value != "" {
				setProps(&ent, "observability_name", value)
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

func rustObsProvenance(framework, otype string) string {
	fw := strings.ToUpper(framework)
	if fw == "" {
		fw = "RUST"
	}
	return "INFERRED_FROM_" + fw + "_" + strings.ToUpper(otype)
}
