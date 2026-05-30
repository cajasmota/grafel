package ruby_test

import (
	"context"
	"testing"

	extreg "github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

// extractFull returns raw types.EntityRecord values (with Properties).
func extractFull(t *testing.T, name string, file extreg.FileInput) []types.EntityRecord {
	t.Helper()
	e, ok := extreg.Get(name)
	if !ok {
		t.Fatalf("extractor %q not registered", name)
	}
	ents, err := e.Extract(context.Background(), file)
	if err != nil {
		t.Fatalf("extract error: %v", err)
	}
	return ents
}

// ---------------------------------------------------------------------------
// Observability — log_extraction
// ---------------------------------------------------------------------------

func TestRubyObsRailsLogger(t *testing.T) {
	src := `
class PostsController < ApplicationController
  def index
    Rails.logger.info("Loading posts")
    Rails.logger.error("Something went wrong")
    @posts = Post.all
  end
end
`
	ents := extract(t, "ruby_observability", fi("app/controllers/posts_controller.rb", "ruby", src))
	if !containsEntity(ents, "SCOPE.Pattern", "Rails.logger.info") {
		t.Error("expected Rails.logger.info entity")
	}
	if !containsEntity(ents, "SCOPE.Pattern", "Rails.logger.error") {
		t.Error("expected Rails.logger.error entity")
	}
}

func TestRubyObsLoggerNew(t *testing.T) {
	src := `
require 'logger'
logger = Logger.new(STDOUT)
logger.info "Application started"
`
	ents := extract(t, "ruby_observability", fi("config/initializer.rb", "ruby", src))
	if !containsEntity(ents, "SCOPE.Pattern", "Logger.new") {
		t.Error("expected Logger.new entity")
	}
}

func TestRubyObsSemanticLogger(t *testing.T) {
	src := `
require 'semantic_logger'
SemanticLogger.add_appender(file_name: 'development.log', formatter: :color)
`
	ents := extract(t, "ruby_observability", fi("config/initializer.rb", "ruby", src))
	if !containsEntity(ents, "SCOPE.Pattern", "SemanticLogger") {
		t.Error("expected SemanticLogger entity")
	}
}

func TestRubyObsLoggerRequireOnly(t *testing.T) {
	src := `require 'logger'`
	ents := extract(t, "ruby_observability", fi("lib/app.rb", "ruby", src))
	if !containsEntity(ents, "SCOPE.Pattern", "logger") {
		t.Error("expected logger require entity")
	}
}

// ---------------------------------------------------------------------------
// Observability — metric_extraction
// ---------------------------------------------------------------------------

func TestRubyObsPrometheus(t *testing.T) {
	src := `
require 'prometheus/client'
prometheus = Prometheus::Client.registry
counter = Prometheus::Client::Counter.new(:http_requests, docstring: 'A counter')
gauge = Prometheus::Client::Gauge.new(:cpu_usage, docstring: 'CPU gauge')
`
	ents := extract(t, "ruby_observability", fi("lib/metrics.rb", "ruby", src))
	if !containsEntity(ents, "SCOPE.Pattern", "Prometheus::Client::Counter") {
		t.Error("expected Prometheus::Client::Counter entity")
	}
	if !containsEntity(ents, "SCOPE.Pattern", "Prometheus::Client::Gauge") {
		t.Error("expected Prometheus::Client::Gauge entity")
	}
}

func TestRubyObsDatadogStatsd(t *testing.T) {
	src := `
require 'datadog/statsd'
statsd = Datadog::Statsd.new('localhost', 8125)
statsd.increment('page.views', tags: ['page:home'])
statsd.gauge('account.balance', 1000.0)
`
	ents := extract(t, "ruby_observability", fi("lib/metrics.rb", "ruby", src))
	if !containsEntity(ents, "SCOPE.Pattern", "Datadog::Statsd.new") {
		t.Error("expected Datadog::Statsd.new entity")
	}
}

func TestRubyObsYabeda(t *testing.T) {
	src := `
require 'yabeda'
Yabeda.counter(:http_requests_total, comment: "Total HTTP requests")
Yabeda.histogram(:request_duration, comment: "Duration", buckets: [0.1, 0.5, 1.0])
`
	ents := extract(t, "ruby_observability", fi("config/metrics.rb", "ruby", src))
	if !containsEntity(ents, "SCOPE.Pattern", "Yabeda.counter") {
		t.Error("expected Yabeda.counter entity")
	}
	if !containsEntity(ents, "SCOPE.Pattern", "Yabeda.histogram") {
		t.Error("expected Yabeda.histogram entity")
	}
}

func TestRubyObsStatsDRuby(t *testing.T) {
	src := `
StatsD.measure('request.time') { do_work }
StatsD.increment('request.count')
`
	ents := extract(t, "ruby_observability", fi("lib/tracking.rb", "ruby", src))
	if !containsEntity(ents, "SCOPE.Pattern", "StatsD.measure") {
		t.Error("expected StatsD.measure entity")
	}
	if !containsEntity(ents, "SCOPE.Pattern", "StatsD.increment") {
		t.Error("expected StatsD.increment entity")
	}
}

// ---------------------------------------------------------------------------
// Observability — trace_extraction
// ---------------------------------------------------------------------------

func TestRubyObsOpenTelemetry(t *testing.T) {
	src := `
require 'opentelemetry-sdk'
OpenTelemetry::SDK.configure do |c|
  c.service_name = 'my-app'
end
tracer = OpenTelemetry.tracer_provider.tracer('my-tracer')
tracer.in_span("process_order") do |span|
  span.set_attribute('order.id', order_id)
end
`
	ents := extract(t, "ruby_observability", fi("config/initializer.rb", "ruby", src))
	if !containsEntity(ents, "SCOPE.Pattern", "process_order") {
		t.Error("expected process_order trace_span entity from in_span")
	}
}

func TestRubyObsDdtrace(t *testing.T) {
	src := `
require 'ddtrace'
Datadog::Tracing.trace("web.request") do |span|
  span.resource = '/users'
end
Datadog.configure do |c|
  c.service = 'my-service'
end
`
	ents := extract(t, "ruby_observability", fi("config/ddtrace.rb", "ruby", src))
	if len(ents) == 0 {
		t.Error("expected at least one trace entity from ddtrace")
	}
}

func TestRubyObsSkylight(t *testing.T) {
	src := `
require 'skylight'
Skylight.instrument(title: "perform_query") do
  run_query
end
`
	ents := extract(t, "ruby_observability", fi("lib/query.rb", "ruby", src))
	if !containsEntity(ents, "SCOPE.Pattern", "Skylight.instrument") {
		t.Error("expected Skylight.instrument entity")
	}
}

func TestRubyObsOpenTracing(t *testing.T) {
	src := `
require 'opentracing'
OpenTracing.start_active_span('operation_name') do |scope|
  scope.span.set_tag('user', user_id)
end
`
	ents := extract(t, "ruby_observability", fi("lib/tracing.rb", "ruby", src))
	if len(ents) == 0 {
		t.Error("expected at least one entity from opentracing")
	}
}

// ---------------------------------------------------------------------------
// Rails-specific log patterns
// ---------------------------------------------------------------------------

func TestRailsObsLoggerTagged(t *testing.T) {
	src := `
class ApplicationController < ActionController::Base
  def index
    logger.tagged("RequestID", request.uuid) do
      Rails.logger.info "Processing request"
    end
  end
end
`
	ents := extractFull(t, "ruby_observability", fi("app/controllers/application_controller.rb", "ruby", src))
	found := false
	for _, e := range ents {
		if e.Properties["kind"] == "tagged_block" && e.Properties["library"] == "rails_tagged_logging" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected logger.tagged entity with library=rails_tagged_logging")
	}
}

func TestRailsObsTaggedLogging(t *testing.T) {
	src := `
require 'active_support/tagged_logging'
logger = ActiveSupport::TaggedLogging.new(Logger.new(STDOUT))
logger.tagged("BCX") { logger.info "Stuff" }
`
	ents := extract(t, "ruby_observability", fi("config/application.rb", "ruby", src))
	if !containsEntity(ents, "SCOPE.Pattern", "ActiveSupport::TaggedLogging") {
		t.Error("expected ActiveSupport::TaggedLogging entity")
	}
}

func TestRailsObsLograge(t *testing.T) {
	src := `
require 'lograge'
Rails.application.configure do
  config.lograge.enabled = true
  config.lograge.formatter = Lograge::Formatters::Json.new
end
`
	ents := extractFull(t, "ruby_observability", fi("config/initializers/lograge.rb", "ruby", src))
	// Expect at least the require entity or config entity
	found := false
	for _, e := range ents {
		if e.Properties["library"] == "lograge" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected lograge entity")
	}
}

// ---------------------------------------------------------------------------
// Rails-specific metric patterns
// ---------------------------------------------------------------------------

func TestRailsObsYabedaConfigure(t *testing.T) {
	src := `
require 'yabeda'
require 'yabeda-rails'

Yabeda.configure do
  group :api do
    counter :requests_total, comment: "Total API requests"
    histogram :response_time, comment: "Response time", buckets: [0.1, 0.5, 1.0]
  end
end
`
	ents := extract(t, "ruby_observability", fi("config/initializers/yabeda.rb", "ruby", src))
	if !containsEntity(ents, "SCOPE.Pattern", "Yabeda.configure") {
		t.Error("expected Yabeda.configure entity with kind=configure_block")
	}
}

// ---------------------------------------------------------------------------
// Rails-specific trace patterns (ActiveSupport::Notifications)
// ---------------------------------------------------------------------------

func TestRailsObsASNInstrument(t *testing.T) {
	src := `
class OrderService
  def process(order)
    ActiveSupport::Notifications.instrument("process_order.order_service", order_id: order.id) do
      do_processing(order)
    end
  end
end
`
	ents := extractFull(t, "ruby_observability", fi("app/services/order_service.rb", "ruby", src))
	found := false
	for _, e := range ents {
		if e.Name == "process_order.order_service" {
			found = true
			if e.Properties["signal"] != "trace" {
				t.Errorf("expected signal=trace, got %q", e.Properties["signal"])
			}
			if e.Properties["library"] != "active_support_notifications" {
				t.Errorf("expected library=active_support_notifications, got %q", e.Properties["library"])
			}
			if e.Properties["kind"] != "instrument" {
				t.Errorf("expected kind=instrument, got %q", e.Properties["kind"])
			}
		}
	}
	if !found {
		t.Error("expected process_order.order_service entity from AS::Notifications.instrument")
	}
}

func TestRailsObsASNSubscribe(t *testing.T) {
	src := `
ActiveSupport::Notifications.subscribe("sql.active_record") do |name, start, finish, id, payload|
  Rails.logger.debug "SQL: #{payload[:sql]}"
end
`
	ents := extractFull(t, "ruby_observability", fi("config/initializers/notifications.rb", "ruby", src))
	found := false
	for _, e := range ents {
		if e.Name == "sql.active_record" {
			found = true
			if e.Properties["kind"] != "subscribe" {
				t.Errorf("expected kind=subscribe, got %q", e.Properties["kind"])
			}
		}
	}
	if !found {
		t.Error("expected sql.active_record entity from AS::Notifications.subscribe")
	}
}

func TestRailsObsASNInstrumentLogError(t *testing.T) {
	src := `
# Verify that Rails.logger.error call-site entities are emitted
class PaymentController < ApplicationController
  def create
    Rails.logger.error("Payment failed for user #{current_user.id}")
    head :unprocessable_entity
  end
end
`
	ents := extractFull(t, "ruby_observability", fi("app/controllers/payment_controller.rb", "ruby", src))
	found := false
	for _, e := range ents {
		if e.Name == "Rails.logger.error" {
			found = true
			if e.Properties["signal"] != "log" {
				t.Errorf("expected signal=log, got %q", e.Properties["signal"])
			}
			if e.Properties["log_level"] != "error" {
				t.Errorf("expected log_level=error, got %q", e.Properties["log_level"])
			}
		}
	}
	if !found {
		t.Error("expected Rails.logger.error call-site entity")
	}
}

// ---------------------------------------------------------------------------
// No match: plain Ruby file
// ---------------------------------------------------------------------------

func TestRubyObsNoMatch(t *testing.T) {
	src := `
class User
  def name
    "Alice"
  end
end
`
	ents := extract(t, "ruby_observability", fi("app/models/user.rb", "ruby", src))
	if len(ents) != 0 {
		t.Errorf("expected no entities, got %d", len(ents))
	}
}
