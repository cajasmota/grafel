package ruby_test

// middleware_test.go — tests for the ruby_middleware extractor.
// Part of #3282.

import (
	"testing"
)

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
