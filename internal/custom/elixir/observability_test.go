package elixir_test

// ---------------------------------------------------------------------------
// Observability extractor tests (#3474)
//
// Value-asserting: the telemetry event/metric names and log levels/messages
// are checked against the exact literal at the call site. These prove the
// per-call-site name capture that justifies flipping metric_extraction and
// trace_extraction from not_applicable -> partial. The cells stay PARTIAL
// (not full) because handler-attach / reporter / exporter binding spans
// multiple files and is NOT resolved here.
// ---------------------------------------------------------------------------

import "testing"

func TestObservabilityLoggerStatements(t *testing.T) {
	src := `
defmodule MyApp.Worker do
  require Logger

  def run do
    Logger.info("starting worker")
    Logger.debug("debug detail")
    Logger.warn("legacy warn alias")
    Logger.error("boom: " <> reason)
  end
end
`
	ents := extract(t, "custom_elixir_observability", fi("worker.ex", "elixir", src))

	info := findEntity(ents, "SCOPE.Pattern", "Logger.info")
	if info == nil {
		t.Fatal("expected Logger.info log_statement")
	}
	if info.Subtype != "log_statement" {
		t.Errorf("expected subtype log_statement, got %q", info.Subtype)
	}
	if got := info.Props["log_level"]; got != "info" {
		t.Errorf("expected log_level info, got %q", got)
	}
	if got := info.Props["message"]; got != "starting worker" {
		t.Errorf("expected message 'starting worker', got %q", got)
	}
	if got := info.Props["signal"]; got != "log" {
		t.Errorf("expected signal log, got %q", got)
	}

	// `Logger.warn` is the legacy alias of warning -> canonicalised to warning.
	if findEntity(ents, "SCOPE.Pattern", "Logger.warning") == nil {
		t.Error("expected Logger.warn to canonicalise to Logger.warning")
	}

	// A concatenated message records the leading string-literal segment; the
	// dynamic tail (<> reason) is not resolved (file-local, no dataflow).
	err := findEntity(ents, "SCOPE.Pattern", "Logger.error")
	if err == nil {
		t.Fatal("expected Logger.error log_statement")
	}
	if got := err.Props["message"]; got != "boom: " {
		t.Errorf("expected leading literal message 'boom: ', got %q", got)
	}
	if got := err.Props["log_level"]; got != "error" {
		t.Errorf("expected log_level error, got %q", got)
	}
}

func TestObservabilityLoggerMetadata(t *testing.T) {
	src := `
defmodule MyApp.Ctx do
  require Logger
  def tag, do: Logger.metadata(request_id: "abc")
end
`
	ents := extract(t, "custom_elixir_observability", fi("ctx.ex", "elixir", src))
	md := findEntity(ents, "SCOPE.Pattern", "Logger.metadata")
	if md == nil {
		t.Fatal("expected Logger.metadata pattern")
	}
	if md.Subtype != "log_metadata" {
		t.Errorf("expected subtype log_metadata, got %q", md.Subtype)
	}
}

// TestObservabilityTelemetryExecute proves the exact event-name atom list of a
// :telemetry.execute call is captured as a dotted metric name at the call site.
func TestObservabilityTelemetryExecute(t *testing.T) {
	src := `
defmodule MyApp.Requests do
  def stop(measurements, metadata) do
    :telemetry.execute([:my_app, :request, :stop], measurements, metadata)
  end
end
`
	ents := extract(t, "custom_elixir_observability", fi("requests.ex", "elixir", src))
	ev := findEntity(ents, "SCOPE.Pattern", "my_app.request.stop")
	if ev == nil {
		t.Fatal("expected my_app.request.stop telemetry metric")
	}
	if ev.Subtype != "metric" {
		t.Errorf("expected subtype metric, got %q", ev.Subtype)
	}
	if got := ev.Props["telemetry_event"]; got != "my_app.request.stop" {
		t.Errorf("expected telemetry_event my_app.request.stop, got %q", got)
	}
	if got := ev.Props["metric_type"]; got != "telemetry_event" {
		t.Errorf("expected metric_type telemetry_event, got %q", got)
	}
	if got := ev.Props["library"]; got != "telemetry" {
		t.Errorf("expected library telemetry, got %q", got)
	}
}

// TestObservabilityTelemetryMetrics proves the metric-name string literal of a
// Telemetry.Metrics reporter definition is captured along with its kind.
func TestObservabilityTelemetryMetrics(t *testing.T) {
	src := `
defmodule MyApp.Telemetry do
  def metrics do
    [
      counter("phoenix.endpoint.stop.duration"),
      summary("my_app.repo.query.total_time", unit: {:native, :millisecond}),
      last_value("vm.memory.total")
    ]
  end
end
`
	ents := extract(t, "custom_elixir_observability", fi("telemetry.ex", "elixir", src))

	c := findEntity(ents, "SCOPE.Pattern", "phoenix.endpoint.stop.duration")
	if c == nil {
		t.Fatal("expected counter metric phoenix.endpoint.stop.duration")
	}
	if got := c.Props["metric_type"]; got != "counter" {
		t.Errorf("expected metric_type counter, got %q", got)
	}
	if got := c.Props["library"]; got != "telemetry_metrics" {
		t.Errorf("expected library telemetry_metrics, got %q", got)
	}

	s := findEntity(ents, "SCOPE.Pattern", "my_app.repo.query.total_time")
	if s == nil {
		t.Fatal("expected summary metric my_app.repo.query.total_time")
	}
	if got := s.Props["metric_type"]; got != "summary" {
		t.Errorf("expected metric_type summary, got %q", got)
	}

	if lv := findEntity(ents, "SCOPE.Pattern", "vm.memory.total"); lv == nil {
		t.Error("expected last_value metric vm.memory.total")
	}
}

// TestObservabilityTelemetrySpan proves the event-prefix atom list of a
// :telemetry.span call is captured as a trace_span name at the call site.
func TestObservabilityTelemetrySpan(t *testing.T) {
	src := `
defmodule MyApp.Job do
  def perform(metadata) do
    :telemetry.span([:my_app, :worker, :job], metadata, fn ->
      {do_work(), %{}}
    end)
  end
end
`
	ents := extract(t, "custom_elixir_observability", fi("job.ex", "elixir", src))
	sp := findEntity(ents, "SCOPE.Pattern", "my_app.worker.job")
	if sp == nil {
		t.Fatal("expected my_app.worker.job trace_span")
	}
	if sp.Subtype != "trace_span" {
		t.Errorf("expected subtype trace_span, got %q", sp.Subtype)
	}
	if got := sp.Props["span_kind"]; got != "telemetry_span" {
		t.Errorf("expected span_kind telemetry_span, got %q", got)
	}
	if got := sp.Props["telemetry_event"]; got != "my_app.worker.job" {
		t.Errorf("expected telemetry_event my_app.worker.job, got %q", got)
	}
	if got := sp.Props["signal"]; got != "trace" {
		t.Errorf("expected signal trace, got %q", got)
	}
}

func TestObservabilityNoMatch(t *testing.T) {
	src := `defmodule MyApp.Plain do
  def add(a, b), do: a + b
end`
	ents := extract(t, "custom_elixir_observability", fi("plain.ex", "elixir", src))
	if len(ents) != 0 {
		t.Errorf("expected no entities, got %d", len(ents))
	}
}
