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

// ---------------------------------------------------------------------------
// Elixir trailing-framework Observability credit (#4027, epic #3872, from
// Elixir audit #3885).
//
// custom_elixir_observability is framework-AGNOSTIC: it fires on ANY .ex
// dispatched as language "elixir" with no framework gate, capturing Logger.*
// (log), :telemetry.execute / Telemetry.Metrics (metric) and :telemetry.span
// (trace). The flagship Elixir frameworks carry Observability.{log,metric,
// trace}_extraction at `partial`; these tests prove the SAME artifacts fire on
// the trailing frameworks guardian/finch/tesla/req — justifying the registry
// re-stamp to the flagship `partial` sibling status.
//
// Each is value-asserting: it proves a SPECIFIC named artifact on each
// framework's real idiom (a Logger.info message, a dotted :telemetry.execute
// metric name, and a dotted :telemetry.span trace name). The cells stay
// PARTIAL for the same reason as the flagships (handler-attach / reporter /
// exporter binding spans multiple files and is not resolved here).
// ---------------------------------------------------------------------------

func assertLog(t *testing.T, ents []entitySummary, level, message string) {
	t.Helper()
	e := findEntity(ents, "SCOPE.Pattern", "Logger."+level)
	if e == nil {
		t.Fatalf("expected Logger.%s log_statement", level)
	}
	if e.Subtype != "log_statement" {
		t.Errorf("expected subtype log_statement, got %q", e.Subtype)
	}
	if got := e.Props["message"]; got != message {
		t.Errorf("expected message %q, got %q", message, got)
	}
	if got := e.Props["signal"]; got != "log" {
		t.Errorf("expected signal log, got %q", got)
	}
}

func assertMetric(t *testing.T, ents []entitySummary, event string) {
	t.Helper()
	e := findEntity(ents, "SCOPE.Pattern", event)
	if e == nil {
		t.Fatalf("expected metric %s", event)
	}
	if e.Subtype != "metric" {
		t.Errorf("expected subtype metric, got %q", e.Subtype)
	}
	if got := e.Props["telemetry_event"]; got != event {
		t.Errorf("expected telemetry_event %s, got %q", event, got)
	}
	if got := e.Props["signal"]; got != "metric" {
		t.Errorf("expected signal metric, got %q", got)
	}
}

func assertTrace(t *testing.T, ents []entitySummary, event string) {
	t.Helper()
	e := findEntity(ents, "SCOPE.Pattern", event)
	if e == nil {
		t.Fatalf("expected trace_span %s", event)
	}
	if e.Subtype != "trace_span" {
		t.Errorf("expected subtype trace_span, got %q", e.Subtype)
	}
	if got := e.Props["span_name"]; got != event {
		t.Errorf("expected span_name %s, got %q", event, got)
	}
	if got := e.Props["signal"]; got != "trace" {
		t.Errorf("expected signal trace, got %q", got)
	}
}

// TestObservability_Guardian_Trailing — a Guardian auth pipeline that logs a
// verification result, emits a :telemetry.execute auth metric and a
// :telemetry.span around verification.
func TestObservability_Guardian_Trailing(t *testing.T) {
	src := `defmodule MyApp.Auth.Pipeline do
  use Guardian.Plug.Pipeline, otp_app: :my_app
  require Logger

  def authenticate(token) do
    Logger.info("guardian authenticated user")
    :telemetry.execute([:my_app, :guardian, :verify], %{count: 1}, %{})
    :telemetry.span([:my_app, :guardian, :pipeline], %{}, fn ->
      {Guardian.decode_and_verify(token), %{}}
    end)
  end
end`
	ents := extract(t, "custom_elixir_observability", fi("pipeline.ex", "elixir", src))
	assertLog(t, ents, "info", "guardian authenticated user")
	assertMetric(t, ents, "my_app.guardian.verify")
	assertTrace(t, ents, "my_app.guardian.pipeline")
}

// TestObservability_Finch_Trailing — a Finch HTTP client that logs the fetch,
// emits a request metric and wraps the call in a :telemetry.span.
func TestObservability_Finch_Trailing(t *testing.T) {
	src := `defmodule MyApp.HttpClient do
  require Logger

  def fetch_user(id) do
    Logger.info("finch fetched user")
    :telemetry.execute([:my_app, :finch, :request, :stop], %{duration: 1}, %{})
    :telemetry.span([:my_app, :finch, :request], %{}, fn ->
      {Finch.request(req, MyApp.Finch), %{}}
    end)
  end
end`
	ents := extract(t, "custom_elixir_observability", fi("http_client.ex", "elixir", src))
	assertLog(t, ents, "info", "finch fetched user")
	assertMetric(t, ents, "my_app.finch.request.stop")
	assertTrace(t, ents, "my_app.finch.request")
}

// TestObservability_Tesla_Trailing — a Tesla API client that logs the call,
// emits a request metric and wraps the post in a :telemetry.span.
func TestObservability_Tesla_Trailing(t *testing.T) {
	src := `defmodule MyApp.ApiClient do
  use Tesla
  require Logger

  def create_order(client, payload) do
    Logger.info("tesla creating order")
    :telemetry.execute([:my_app, :tesla, :call, :stop], %{duration: 2}, %{})
    :telemetry.span([:my_app, :tesla, :call], %{}, fn ->
      {Tesla.post(client, "/orders", payload), %{}}
    end)
  end
end`
	ents := extract(t, "custom_elixir_observability", fi("api_client.ex", "elixir", src))
	assertLog(t, ents, "info", "tesla creating order")
	assertMetric(t, ents, "my_app.tesla.call.stop")
	assertTrace(t, ents, "my_app.tesla.call")
}

// TestObservability_Req_Trailing — a Req HTTP client that logs the post, emits
// a request metric and wraps the post in a :telemetry.span.
func TestObservability_Req_Trailing(t *testing.T) {
	src := `defmodule MyApp.ReqClient do
  require Logger

  def post_event(payload) do
    Logger.info("req posting event")
    :telemetry.execute([:my_app, :req, :post, :stop], %{duration: 3}, %{})
    :telemetry.span([:my_app, :req, :post], %{}, fn ->
      {Req.post(base, json: payload), %{}}
    end)
  end
end`
	ents := extract(t, "custom_elixir_observability", fi("req_client.ex", "elixir", src))
	assertLog(t, ents, "info", "req posting event")
	assertMetric(t, ents, "my_app.req.post.stop")
	assertTrace(t, ents, "my_app.req.post")
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
