package golang_test

import (
	"testing"
)

// Tests for the framework-agnostic observability scanner (issue #3215):
// logging / metrics / tracing detection within gin/echo/fiber/chi context.

// findObs returns the first SCOPE.Pattern observability entity matching the
// given observability_type and observability_subtype, or nil.
func findObs(ents []fullEntity, otype, subtype string) *fullEntity {
	for i := range ents {
		if ents[i].Kind != "SCOPE.Pattern" {
			continue
		}
		p := ents[i].Props
		if p["pattern_kind"] == "observability" &&
			p["observability_type"] == otype &&
			p["observability_subtype"] == subtype {
			return &ents[i]
		}
	}
	return nil
}

func obsEntities(ents []fullEntity) []fullEntity {
	var out []fullEntity
	for _, e := range ents {
		if e.Kind == "SCOPE.Pattern" && e.Props["pattern_kind"] == "observability" {
			out = append(out, e)
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// Framework attribution: a file with no recognised framework marker emits
// nothing (the scanner only attributes observability to gin/echo/fiber/chi).
// ---------------------------------------------------------------------------

func TestObservabilityNoFrameworkNoEmit(t *testing.T) {
	src := `package x
func f() {
	log := logrus.New()
	_ = log
}`
	ents := extractFull(t, "custom_go_observability", fi("main.go", "go", src))
	if got := obsEntities(ents); len(got) != 0 {
		t.Fatalf("expected no observability entities without a framework marker, got %d: %+v", len(got), got)
	}
}

func TestObservabilityNonGoNoEmit(t *testing.T) {
	src := `r := gin.Default(); log := logrus.New()`
	ents := extractFull(t, "custom_go_observability", fi("main.py", "python", src))
	if len(ents) != 0 {
		t.Fatalf("non-go file should yield nothing, got %d", len(ents))
	}
}

// ---------------------------------------------------------------------------
// Per-signal detection (inline, framework-gated).
// ---------------------------------------------------------------------------

func TestObservabilityLoggingFamilies(t *testing.T) {
	cases := []struct {
		name, src, subtype string
	}{
		{"logrus", "r := gin.Default()\nlog := logrus.New()", "logrus"},
		{"zap", "e := echo.New()\nl, _ := zap.NewProduction()", "zap"},
		{"slog", "app := fiber.New()\nl := slog.New(h)", "slog"},
		{"zerolog", "r := chi.NewRouter()\nl := zerolog.New(os.Stderr)", "zerolog"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ents := extractFull(t, "custom_go_observability", fi("main.go", "go", c.src))
			if findObs(ents, "logging", c.subtype) == nil {
				t.Fatalf("missing logging/%s entity; got %+v", c.subtype, obsEntities(ents))
			}
		})
	}
}

func TestObservabilityMetricsPrometheus(t *testing.T) {
	src := `r := gin.Default()
var c = prometheus.NewCounterVec(prometheus.CounterOpts{Name: "http_requests_total"}, []string{"path"})`
	ents := extractFull(t, "custom_go_observability", fi("main.go", "go", src))
	m := findObs(ents, "metrics", "prometheus")
	if m == nil {
		t.Fatalf("missing metrics/prometheus entity; got %+v", obsEntities(ents))
	}
	if m.Props["metric_name"] != "http_requests_total" {
		t.Errorf("metric_name=%q want http_requests_total", m.Props["metric_name"])
	}
	if m.Props["framework"] != "gin" {
		t.Errorf("framework=%q want gin", m.Props["framework"])
	}
}

func TestObservabilityTracingSpanStart(t *testing.T) {
	src := `e := echo.New()
tracer := otel.Tracer("svc")
ctx, span := tracer.Start(ctx, "handler.do")`
	ents := extractFull(t, "custom_go_observability", fi("main.go", "go", src))
	if findObs(ents, "tracing", "tracer_setup") == nil {
		t.Errorf("missing tracing/tracer_setup; got %+v", obsEntities(ents))
	}
	span := findObs(ents, "tracing", "span_start")
	if span == nil {
		t.Fatalf("missing tracing/span_start; got %+v", obsEntities(ents))
	}
	if span.Props["span_name"] != "handler.do" {
		t.Errorf("span_name=%q want handler.do", span.Props["span_name"])
	}
}

// provenance is framework + type derived.
func TestObservabilityProvenance(t *testing.T) {
	src := `r := gin.Default()
tracer := otel.Tracer("svc")`
	ents := extractFull(t, "custom_go_observability", fi("main.go", "go", src))
	tr := findObs(ents, "tracing", "tracer_setup")
	if tr == nil {
		t.Fatal("missing tracer_setup")
	}
	if tr.Props["provenance"] != "INFERRED_FROM_GIN_TRACING" {
		t.Errorf("provenance=%q want INFERRED_FROM_GIN_TRACING", tr.Props["provenance"])
	}
}

// ---------------------------------------------------------------------------
// Fixture-driven end-to-end checks: each framework fixture carries all three
// observability families. These prove the general case (esp. the `full`
// tracing lane).
// ---------------------------------------------------------------------------

func TestObservabilityFixtures(t *testing.T) {
	cases := []struct {
		fixture, framework, logSubtype string
	}{
		{"gin_observability.go", "gin", "logrus"},
		{"echo_observability.go", "echo", "zap"},
		{"fiber_observability.go", "fiber", "slog"},
		{"chi_observability.go", "chi", "logrus"},
	}
	for _, c := range cases {
		t.Run(c.framework, func(t *testing.T) {
			ents := extractFull(t, "custom_go_observability", fixtureFile(t, c.fixture))

			// tracing: both tracer setup and span start (full lane).
			if findObs(ents, "tracing", "tracer_setup") == nil {
				t.Errorf("%s: missing tracer_setup", c.framework)
			}
			span := findObs(ents, "tracing", "span_start")
			if span == nil {
				t.Errorf("%s: missing span_start", c.framework)
			}

			// metrics: a prometheus collector with a metric name.
			m := findObs(ents, "metrics", "prometheus")
			if m == nil {
				t.Errorf("%s: missing prometheus metric", c.framework)
			} else if m.Props["metric_name"] == "" {
				t.Errorf("%s: prometheus metric has no metric_name", c.framework)
			}

			// logging: the framework's chosen logger family.
			if findObs(ents, "logging", c.logSubtype) == nil {
				t.Errorf("%s: missing logging/%s", c.framework, c.logSubtype)
			}

			// every entity must be attributed to this framework.
			for _, e := range obsEntities(ents) {
				if e.Props["framework"] != c.framework {
					t.Errorf("%s: entity %q framework=%q", c.framework, e.Name, e.Props["framework"])
				}
			}
		})
	}
}
