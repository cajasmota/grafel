package ruby_test

import (
	"context"
	"testing"

	extreg "github.com/cajasmota/archigraph/internal/extractor"

	_ "github.com/cajasmota/archigraph/internal/custom/ruby"
)

func fi(path, lang, src string) extreg.FileInput {
	return extreg.FileInput{Path: path, Language: lang, Content: []byte(src)}
}

func extract(t *testing.T, name string, file extreg.FileInput) []entitySummary {
	t.Helper()
	e, ok := extreg.Get(name)
	if !ok {
		t.Fatalf("extractor %q not registered", name)
	}
	ents, err := e.Extract(context.Background(), file)
	if err != nil {
		t.Fatalf("extract error: %v", err)
	}
	var out []entitySummary
	for _, ent := range ents {
		out = append(out, entitySummary{Kind: ent.Kind, Subtype: ent.Subtype, Name: ent.Name})
	}
	return out
}

type entitySummary struct{ Kind, Subtype, Name string }

func containsEntity(ents []entitySummary, kind, name string) bool {
	for _, e := range ents {
		if e.Kind == kind && e.Name == name {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Rails
// ---------------------------------------------------------------------------

func TestRailsResourcesRoutes(t *testing.T) {
	src := `
Rails.application.routes.draw do
  resources :articles
end
`
	ents := extract(t, "custom_ruby_rails", fi("routes.rb", "ruby", src))
	if !containsEntity(ents, "SCOPE.Operation", "GET /articles") {
		t.Error("expected GET /articles route from resources")
	}
	if !containsEntity(ents, "SCOPE.Operation", "POST /articles") {
		t.Error("expected POST /articles route from resources")
	}
	if !containsEntity(ents, "SCOPE.Operation", "DELETE /articles/:id") {
		t.Error("expected DELETE /articles/:id route from resources")
	}
}

func TestRailsExplicitRoute(t *testing.T) {
	src := `
Rails.application.routes.draw do
  get '/dashboard', to: 'dashboard#index'
  post '/login', to: 'sessions#create'
end
`
	ents := extract(t, "custom_ruby_rails", fi("routes.rb", "ruby", src))
	if !containsEntity(ents, "SCOPE.Operation", "GET /dashboard") {
		t.Error("expected GET /dashboard")
	}
	if !containsEntity(ents, "SCOPE.Operation", "POST /login") {
		t.Error("expected POST /login")
	}
}

func TestRailsNamespace(t *testing.T) {
	src := `
namespace :api do
  resources :users
end
`
	ents := extract(t, "custom_ruby_rails", fi("routes.rb", "ruby", src))
	if !containsEntity(ents, "SCOPE.Component", "api") {
		t.Error("expected api namespace component")
	}
}

func TestRailsAssociation(t *testing.T) {
	src := `
class Article < ApplicationRecord
  belongs_to :user
  has_many :comments
end
`
	ents := extract(t, "custom_ruby_rails", fi("article.rb", "ruby", src))
	if !containsEntity(ents, "SCOPE.Component", "belongs_to:user") {
		t.Error("expected belongs_to:user association")
	}
}

func TestRailsNoMatch(t *testing.T) {
	src := `x = 42`
	ents := extract(t, "custom_ruby_rails", fi("plain.rb", "ruby", src))
	if len(ents) != 0 {
		t.Errorf("expected no entities, got %d", len(ents))
	}
}

// ---------------------------------------------------------------------------
// RSpec
// ---------------------------------------------------------------------------

func TestRSpecDescribe(t *testing.T) {
	src := `
RSpec.describe User do
  context "when active" do
    it "returns true" do
      expect(subject.active?).to be true
    end
  end
end
`
	ents := extract(t, "custom_ruby_rspec", fi("user_spec.rb", "ruby", src))
	if !containsEntity(ents, "SCOPE.Component", "User") {
		t.Error("expected User describe component")
	}
	if !containsEntity(ents, "SCOPE.Component", "when active") {
		t.Error("expected 'when active' context component")
	}
}

func TestRSpecExample(t *testing.T) {
	src := `
RSpec.describe Calc do
  it "adds numbers" do
    expect(Calc.add(1, 2)).to eq(3)
  end
  it "subtracts numbers" do
    expect(Calc.sub(3, 1)).to eq(2)
  end
end
`
	ents := extract(t, "custom_ruby_rspec", fi("calc_spec.rb", "ruby", src))
	// Example names are deduped as "label#index"
	if !containsEntity(ents, "SCOPE.Operation", "adds numbers#0") {
		t.Error("expected 'adds numbers#0' example")
	}
}

func TestRSpecLetBinding(t *testing.T) {
	src := `
RSpec.describe Order do
  let(:order) { Order.new }
  let!(:user) { create(:user) }
end
`
	ents := extract(t, "custom_ruby_rspec", fi("order_spec.rb", "ruby", src))
	// let entity name = variable name directly
	if !containsEntity(ents, "SCOPE.Pattern", "order") {
		t.Error("expected 'order' let pattern")
	}
}

func TestRSpecSharedExamples(t *testing.T) {
	src := `
shared_examples "a publishable resource" do
  it "can be published" do
    expect(subject).to respond_to(:publish)
  end
end
`
	ents := extract(t, "custom_ruby_rspec", fi("shared.rb", "ruby", src))
	if !containsEntity(ents, "SCOPE.Component", "a publishable resource") {
		t.Error("expected shared_examples component")
	}
}

func TestRSpecNoMatch(t *testing.T) {
	src := `puts "hello"`
	ents := extract(t, "custom_ruby_rspec", fi("plain.rb", "ruby", src))
	if len(ents) != 0 {
		t.Errorf("expected no entities, got %d", len(ents))
	}
}

// ---------------------------------------------------------------------------
// Sidekiq
// ---------------------------------------------------------------------------

func TestSidekiqWorker(t *testing.T) {
	src := `
class EmailWorker
  include Sidekiq::Worker

  def perform(user_id)
    User.find(user_id).send_email
  end
end
`
	ents := extract(t, "custom_ruby_sidekiq", fi("email_worker.rb", "ruby", src))
	if !containsEntity(ents, "SCOPE.Service", "EmailWorker") {
		t.Error("expected EmailWorker SCOPE.Service")
	}
	if !containsEntity(ents, "SCOPE.Operation", "perform") {
		t.Error("expected perform SCOPE.Operation")
	}
}

func TestSidekiqPerformAsync(t *testing.T) {
	src := `EmailWorker.perform_async(user.id)`
	ents := extract(t, "custom_ruby_sidekiq", fi("controller.rb", "ruby", src))
	// dispatch entity name = ClassName.method
	if !containsEntity(ents, "SCOPE.Operation", "EmailWorker.perform_async") {
		t.Error("expected EmailWorker.perform_async dispatch operation")
	}
}

func TestSidekiqConfigureServer(t *testing.T) {
	src := `
Sidekiq.configure_server do |config|
  config.redis = { url: ENV['REDIS_URL'] }
end
`
	ents := extract(t, "custom_ruby_sidekiq", fi("initializer.rb", "ruby", src))
	// configure entity name = "sidekiq." + config_type
	if !containsEntity(ents, "SCOPE.Pattern", "sidekiq.configure_server") {
		t.Error("expected sidekiq.configure_server pattern")
	}
}

func TestSidekiqNoMatch(t *testing.T) {
	src := `class User; end`
	ents := extract(t, "custom_ruby_sidekiq", fi("user.rb", "ruby", src))
	if len(ents) != 0 {
		t.Errorf("expected no entities, got %d", len(ents))
	}
}
