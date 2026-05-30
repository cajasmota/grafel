package ruby_test

// middleware_test.go — tests for the ruby_middleware extractor.
// Part of #3282 / #3341.

import (
	"context"
	"testing"

	extreg "github.com/cajasmota/archigraph/internal/extractor"
)

// mwExtractRaw returns raw EntityRecord values so tests can inspect Properties.
func mwExtractRaw(t *testing.T, path, src string) []interface{ GetName() string } {
	t.Helper()
	e, ok := extreg.Get("ruby_middleware")
	if !ok {
		t.Fatal("ruby_middleware extractor not registered")
	}
	ents, err := e.Extract(context.Background(), extreg.FileInput{
		Path: path, Language: "ruby", Content: []byte(src),
	})
	if err != nil {
		t.Fatalf("extract error: %v", err)
	}
	// Return as-is; callers use the raw slice directly.
	_ = ents
	return nil
}

// mwExtractProps returns raw entities so callers can access Properties.
func mwExtractProps(t *testing.T, path, src string) []map[string]string {
	t.Helper()
	e, ok := extreg.Get("ruby_middleware")
	if !ok {
		t.Fatal("ruby_middleware extractor not registered")
	}
	ents, err := e.Extract(context.Background(), extreg.FileInput{
		Path: path, Language: "ruby", Content: []byte(src),
	})
	if err != nil {
		t.Fatalf("extract error: %v", err)
	}
	out := make([]map[string]string, len(ents))
	for i, ent := range ents {
		m := make(map[string]string, len(ent.Properties)+2)
		m["__name"] = ent.Name
		m["__kind"] = ent.Kind
		for k, v := range ent.Properties {
			m[k] = v
		}
		out[i] = m
	}
	return out
}

// findByName returns the property map for the first entity with matching name.
func findByName(props []map[string]string, name string) map[string]string {
	for _, p := range props {
		if p["__name"] == name {
			return p
		}
	}
	return nil
}

func mwExtract(t *testing.T, path, src string) []entitySummary {
	t.Helper()
	return extract(t, "ruby_middleware", fi(path, "ruby", src))
}

// ---------------------------------------------------------------------------
// Rails
// ---------------------------------------------------------------------------

func TestMWRails_RackUse(t *testing.T) {
	src := `
class Application < Rails::Application
  config.middleware.use Rack::Deflater
  config.middleware.use Warden::Manager
end
`
	ents := mwExtract(t, "config/application.rb", src)
	if !containsEntity(ents, "SCOPE.Pattern", "config_mw:Rack::Deflater") {
		t.Error("expected config_mw:Rack::Deflater middleware entity")
	}
	if !containsEntity(ents, "SCOPE.Pattern", "config_mw:Warden::Manager") {
		t.Error("expected config_mw:Warden::Manager middleware entity")
	}
}

func TestMWRails_BeforeAfterAction(t *testing.T) {
	src := `
class ApplicationController < ActionController::Base
  before_action :authenticate_user!
  after_action :log_request
  around_action :wrap_transaction
end
`
	ents := mwExtract(t, "app/controllers/application_controller.rb", src)
	if !containsEntity(ents, "SCOPE.Pattern", "rails_filter:before_action:authenticate_user!") {
		t.Error("expected rails_filter:before_action:authenticate_user!")
	}
	if !containsEntity(ents, "SCOPE.Pattern", "rails_filter:after_action:log_request") {
		t.Error("expected rails_filter:after_action:log_request")
	}
	if !containsEntity(ents, "SCOPE.Pattern", "rails_filter:around_action:wrap_transaction") {
		t.Error("expected rails_filter:around_action:wrap_transaction")
	}
}

// ---------------------------------------------------------------------------
// Grape
// ---------------------------------------------------------------------------

func TestMWGrape_BeforeAfterHooks(t *testing.T) {
	src := `
class API < Grape::API
  before do
    authenticate!
  end

  after do
    log_response
  end
end
`
	ents := mwExtract(t, "app/api/api.rb", src)
	foundBefore := false
	foundAfter := false
	for _, e := range ents {
		if e.Kind == "SCOPE.Pattern" && e.Subtype == "middleware" {
			if e.Name == "grape_hook:before" {
				foundBefore = true
			}
			if e.Name == "grape_hook:after" {
				foundAfter = true
			}
		}
	}
	if !foundBefore {
		t.Error("expected grape_hook:before middleware entity")
	}
	if !foundAfter {
		t.Error("expected grape_hook:after middleware entity")
	}
}

func TestMWGrape_RackUse(t *testing.T) {
	src := `
class API < Grape::API
  use Rack::Cors
  use Rack::Logger
end
`
	ents := mwExtract(t, "app/api/api.rb", src)
	if !containsEntity(ents, "SCOPE.Pattern", "rack_use:Rack::Cors") {
		t.Error("expected rack_use:Rack::Cors middleware entity")
	}
	if !containsEntity(ents, "SCOPE.Pattern", "rack_use:Rack::Logger") {
		t.Error("expected rack_use:Rack::Logger middleware entity")
	}
}

// ---------------------------------------------------------------------------
// Sinatra
// ---------------------------------------------------------------------------

func TestMWSinatra_BeforeAfterBlocks(t *testing.T) {
	src := `
class MyApp < Sinatra::Base
  before do
    @user = current_user
  end

  after do
    db.close
  end
end
`
	ents := mwExtract(t, "app.rb", src)
	if !containsEntity(ents, "SCOPE.Pattern", "sinatra_filter:before") {
		t.Error("expected sinatra_filter:before middleware entity")
	}
	if !containsEntity(ents, "SCOPE.Pattern", "sinatra_filter:after") {
		t.Error("expected sinatra_filter:after middleware entity")
	}
}

func TestMWSinatra_PathFilter(t *testing.T) {
	src := `
class MyApp < Sinatra::Base
  before '/admin/*' do
    authenticate_admin!
  end
end
`
	ents := mwExtract(t, "app.rb", src)
	if !containsEntity(ents, "SCOPE.Pattern", "sinatra_filter:before:/admin/*") {
		t.Error("expected sinatra_filter:before:/admin/* middleware entity")
	}
}

// ---------------------------------------------------------------------------
// Roda
// ---------------------------------------------------------------------------

func TestMWRoda_Plugin(t *testing.T) {
	src := `
class App < Roda
  plugin :middleware
  plugin :all_verbs
  plugin :json
end
`
	ents := mwExtract(t, "app.rb", src)
	if !containsEntity(ents, "SCOPE.Pattern", "roda_plugin:middleware") {
		t.Error("expected roda_plugin:middleware entity")
	}
	if !containsEntity(ents, "SCOPE.Pattern", "roda_plugin:json") {
		t.Error("expected roda_plugin:json entity")
	}
}

// ---------------------------------------------------------------------------
// Cuba
// ---------------------------------------------------------------------------

func TestMWCuba_BeforeAfter(t *testing.T) {
	src := `
Cuba.define do
  before do
    @user = env["current_user"]
  end

  after do
    log_request
  end

  on "users" do
    run Users
  end
end
`
	ents := mwExtract(t, "app.rb", src)
	if !containsEntity(ents, "SCOPE.Pattern", "cuba_filter:before") {
		t.Error("expected cuba_filter:before middleware entity")
	}
	if !containsEntity(ents, "SCOPE.Pattern", "cuba_filter:after") {
		t.Error("expected cuba_filter:after middleware entity")
	}
}

// ---------------------------------------------------------------------------
// Non-Ruby / no signal → no entities
// ---------------------------------------------------------------------------

func TestMWNoMatch_NoSignal(t *testing.T) {
	src := `class Foo; def bar; end; end`
	ents := mwExtract(t, "plain.rb", src)
	if len(ents) != 0 {
		t.Errorf("expected no entities for plain ruby, got %d", len(ents))
	}
}

func TestMWNoMatch_EmptyFile(t *testing.T) {
	ents := mwExtract(t, "empty.rb", "")
	if len(ents) != 0 {
		t.Errorf("expected no entities for empty file, got %d", len(ents))
	}
}

// ---------------------------------------------------------------------------
// VALUE-ASSERTING TESTS — Rails Rack middleware stack (#3341)
// ---------------------------------------------------------------------------

// TestMWRails_ConfigMiddlewareStack_OrderAndOps verifies that a config/application.rb
// containing use + insert_before + insert_after + swap + delete emits:
//   - Exact entity names for each operation
//   - Correct middleware_op property per entity
//   - order_position values reflecting declaration order (1-based)
//   - anchor_class on insert_before/insert_after/swap ops
//
// This is the canonical value-asserting test for Rails Rack middleware stack
// extraction introduced in #3341.
func TestMWRails_ConfigMiddlewareStack_OrderAndOps(t *testing.T) {
	src := `
class Application < Rails::Application
  config.middleware.use Rack::Attack
  config.middleware.insert_before ActionDispatch::Static, Rack::Cors
  config.middleware.insert_after ActionDispatch::Executor, Rack::Logger
  config.middleware.swap ActionDispatch::ShowExceptions, CustomErrorMiddleware
  config.middleware.delete ActionDispatch::Static
end
`
	props := mwExtractProps(t, "config/application.rb", src)

	tests := []struct {
		name         string
		wantOp       string
		wantMWClass  string
		wantPosition string
		wantAnchor   string
	}{
		{
			name: "config_mw:Rack::Attack", wantOp: "use",
			wantMWClass: "Rack::Attack", wantPosition: "1", wantAnchor: "",
		},
		{
			name: "config_mw_insert_before:ActionDispatch::Static:Rack::Cors", wantOp: "insert_before",
			wantMWClass: "Rack::Cors", wantPosition: "2", wantAnchor: "ActionDispatch::Static",
		},
		{
			name: "config_mw_insert_after:ActionDispatch::Executor:Rack::Logger", wantOp: "insert_after",
			wantMWClass: "Rack::Logger", wantPosition: "3", wantAnchor: "ActionDispatch::Executor",
		},
		{
			name: "config_mw_swap:ActionDispatch::ShowExceptions:CustomErrorMiddleware", wantOp: "swap",
			wantMWClass: "CustomErrorMiddleware", wantPosition: "4", wantAnchor: "ActionDispatch::ShowExceptions",
		},
		{
			name: "config_mw_delete:ActionDispatch::Static", wantOp: "delete",
			wantMWClass: "ActionDispatch::Static", wantPosition: "5", wantAnchor: "",
		},
	}

	for _, tc := range tests {
		p := findByName(props, tc.name)
		if p == nil {
			t.Errorf("entity %q: not found (got %d entities)", tc.name, len(props))
			continue
		}
		if got := p["middleware_op"]; got != tc.wantOp {
			t.Errorf("entity %q: middleware_op = %q, want %q", tc.name, got, tc.wantOp)
		}
		if got := p["middleware_class"]; got != tc.wantMWClass {
			t.Errorf("entity %q: middleware_class = %q, want %q", tc.name, got, tc.wantMWClass)
		}
		if got := p["order_position"]; got != tc.wantPosition {
			t.Errorf("entity %q: order_position = %q, want %q", tc.name, got, tc.wantPosition)
		}
		if tc.wantAnchor != "" {
			if got := p["anchor_class"]; got != tc.wantAnchor {
				t.Errorf("entity %q: anchor_class = %q, want %q", tc.name, got, tc.wantAnchor)
			}
		}
	}
}

// TestMWRails_CustomRackMiddlewareClass verifies that a Ruby class implementing
// the Rack middleware interface (initialize(app) + call(env)) is emitted as a
// rack_middleware_class entity.
func TestMWRails_CustomRackMiddlewareClass(t *testing.T) {
	src := `
class RequestTimingMiddleware
  def initialize(app)
    @app = app
  end

  def call(env)
    start = Time.now
    status, headers, body = @app.call(env)
    duration = Time.now - start
    headers['X-Runtime'] = duration.to_s
    [status, headers, body]
  end
end
`
	ents := mwExtract(t, "lib/request_timing_middleware.rb", src)
	if !containsEntity(ents, "SCOPE.Pattern", "rack_middleware_class:RequestTimingMiddleware") {
		t.Error("expected rack_middleware_class:RequestTimingMiddleware entity")
	}

	props := mwExtractProps(t, "lib/request_timing_middleware.rb", src)
	p := findByName(props, "rack_middleware_class:RequestTimingMiddleware")
	if p == nil {
		t.Fatal("rack_middleware_class:RequestTimingMiddleware not found in props")
	}
	if got := p["rack_interface"]; got != "initialize(app)+call(env)" {
		t.Errorf("rack_interface = %q, want initialize(app)+call(env)", got)
	}
	if got := p["provenance"]; got != "INFERRED_FROM_RACK_MIDDLEWARE_CLASS" {
		t.Errorf("provenance = %q, want INFERRED_FROM_RACK_MIDDLEWARE_CLASS", got)
	}
}

// TestMWRails_FilterScoping verifies that before_action/after_action/around_action
// with :only/:except scoping emits filter_scope_kind and filter_scope properties
// with the exact scope values.
func TestMWRails_FilterScoping(t *testing.T) {
	src := `
class PostsController < ActionController::Base
  before_action :authenticate!, only: [:show, :edit, :update, :destroy]
  after_action  :log_request, except: [:index]
  around_action :wrap_transaction
end
`
	props := mwExtractProps(t, "app/controllers/posts_controller.rb", src)

	tests := []struct {
		entityName    string
		wantScopeKind string
		wantScopeVal  string // empty string means no scope expected
	}{
		{
			entityName:    "rails_filter:before_action:authenticate!",
			wantScopeKind: "only", wantScopeVal: "[:show, :edit, :update, :destroy]",
		},
		{
			entityName:    "rails_filter:after_action:log_request",
			wantScopeKind: "except", wantScopeVal: "[:index]",
		},
		{
			entityName:    "rails_filter:around_action:wrap_transaction",
			wantScopeKind: "", wantScopeVal: "",
		},
	}

	for _, tc := range tests {
		p := findByName(props, tc.entityName)
		if p == nil {
			t.Errorf("entity %q: not found", tc.entityName)
			continue
		}
		if tc.wantScopeKind == "" {
			if got := p["filter_scope_kind"]; got != "" {
				t.Errorf("entity %q: expected no scope, got filter_scope_kind=%q", tc.entityName, got)
			}
		} else {
			if got := p["filter_scope_kind"]; got != tc.wantScopeKind {
				t.Errorf("entity %q: filter_scope_kind = %q, want %q", tc.entityName, got, tc.wantScopeKind)
			}
			if got := p["filter_scope"]; got != tc.wantScopeVal {
				t.Errorf("entity %q: filter_scope = %q, want %q", tc.entityName, got, tc.wantScopeVal)
			}
		}
	}
}
