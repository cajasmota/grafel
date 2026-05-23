package ruby_test

import (
	"context"
	"testing"

	extreg "github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"

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

// TestRailsBeforeActionCallsEdge verifies that each before_action / after_action
// / around_action filter entity carries a CALLS relationship (structural-ref)
// that points at the named filter method in the same controller file.
// This closes the orphan gap where filter-pattern entities existed but had
// no outbound edges, making them disconnected from the actual SCOPE.Operation
// nodes the tree-sitter extractor emits for the same methods.
func TestRailsBeforeActionCallsEdge(t *testing.T) {
	src := `
class UsersController < ApplicationController
  before_action :authenticate_user!
  before_action :set_user, only: [:show, :update, :destroy]
  after_action :log_action

  def show; end
  def set_user; end
  def authenticate_user!; end
  def log_action; end
end
`
	filePath := "app/controllers/users_controller.rb"
	e, ok := extreg.Get("custom_ruby_rails")
	if !ok {
		t.Fatal("custom_ruby_rails extractor not registered")
	}
	ents, err := e.Extract(context.Background(), extreg.FileInput{
		Path:     filePath,
		Language: "ruby",
		Content:  []byte(src),
	})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}

	// findFilterEntity returns the entity for the given filter name, or nil.
	findFilterEntity := func(name string) *types.EntityRecord {
		for i := range ents {
			if ents[i].Name == name && ents[i].Kind == "SCOPE.Pattern" {
				return &ents[i]
			}
		}
		return nil
	}

	// hasCallsEdge reports whether ent has a CALLS edge with the given toID.
	hasCallsEdge := func(ent *types.EntityRecord, toID string) bool {
		for _, r := range ent.Relationships {
			if r.Kind == "CALLS" && r.ToID == toID {
				return true
			}
		}
		return false
	}

	tests := []struct {
		filterName   string // SCOPE.Pattern entity name, e.g. "before_action:set_user"
		targetMethod string // method name the CALLS edge must point at
	}{
		{"before_action:authenticate_user!", "authenticate_user!"},
		{"before_action:set_user", "set_user"},
		{"after_action:log_action", "log_action"},
	}

	for _, tc := range tests {
		ent := findFilterEntity(tc.filterName)
		if ent == nil {
			t.Errorf("expected SCOPE.Pattern entity %q not found", tc.filterName)
			continue
		}
		wantRef := "scope:operation:method:ruby:" + filePath + ":" + tc.targetMethod
		if !hasCallsEdge(ent, wantRef) {
			t.Errorf("entity %q: missing CALLS edge to %q; got rels=%+v",
				tc.filterName, wantRef, ent.Relationships)
		}
	}
}

// TestRailsBeforeActionCallsEdge_CountDropsUnresolved verifies the proportion
// of bare-name CALLS edges emitted for a controller with before_action filters.
// Before this fix the filter methods produced zero CALLS edges; now each one
// produces a structural-ref edge that the resolver can bind, reducing the
// unresolved-edge count for this pattern class.
func TestRailsBeforeActionCallsEdge_ThreeFilters(t *testing.T) {
	src := `
class PostsController < ApplicationController
  before_action :auth!
  before_action :set_post, only: [:show]
  around_action :wrap_transaction

  def show; end
end
`
	filePath := "app/controllers/posts_controller.rb"
	e, _ := extreg.Get("custom_ruby_rails")
	ents, _ := e.Extract(context.Background(), extreg.FileInput{
		Path:     filePath,
		Language: "ruby",
		Content:  []byte(src),
	})

	wantRefs := map[string]string{
		"before_action:auth!":            "scope:operation:method:ruby:app/controllers/posts_controller.rb:auth!",
		"before_action:set_post":         "scope:operation:method:ruby:app/controllers/posts_controller.rb:set_post",
		"around_action:wrap_transaction": "scope:operation:method:ruby:app/controllers/posts_controller.rb:wrap_transaction",
	}

	for filterName, wantRef := range wantRefs {
		found := false
		for _, ent := range ents {
			if ent.Name == filterName && ent.Kind == "SCOPE.Pattern" {
				for _, r := range ent.Relationships {
					if r.Kind == "CALLS" && r.ToID == wantRef {
						found = true
					}
				}
				break
			}
		}
		if !found {
			t.Errorf("filter %q: missing CALLS structural-ref %q", filterName, wantRef)
		}
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
