package engine

import (
	"testing"
)

// #4417 — Grape (`get ':id' do ... end` inside `resource :users do ... end`) and
// Roda (`r.get Integer do |id| ... end` inside `r.on "users" do ... end`) route
// blocks are inherently anonymous: the handler is ALWAYS a block, never a named
// method — exactly like Sinatra (#4385). Each route must signal
// refKind=inlineHandlerRefKind so makeEmit synthesizes a stable
// `<inline VERB /path>` handler entity + a same-file IMPLEMENTS bridge, instead
// of leaving the endpoint a handler-less graph island.
//
// These tests run the REAL extract+synthesis+merge+resolve pipeline (via
// detectInline / assertInlineEndpointBridged from the #4324 test file) on
// faithful Grape and Roda fixtures and prove the endpoints are present AND
// handler-linked.

// TestInline4417_GrapeResourceNesting covers a Grape API class with
// resource/namespace prefix nesting. `resource :users do ... end` contributes
// "/users"; nested `namespace :v1 do ... end` composes "/v1"; the verb block's
// own `:id`/`'/profile'` path is joined at the leaf. `:id` → `{id}`.
func TestInline4417_GrapeResourceNesting(t *testing.T) {
	src := `require 'grape'

class API < Grape::API
  format :json

  resource :users do
    get do
      User.all
    end

    get ':id' do
      User.find(params[:id])
    end

    post '/invite' do
      User.invite(params)
    end

    namespace :v1 do
      get do
        User.legacy
      end
    end
  end
end
`
	ents, rels := detectInline(t, "ruby", "app/api.rb", src)
	assertInlineEndpointBridged(t, ents, rels, "GET", "/users", "grape")
	assertInlineEndpointBridged(t, ents, rels, "GET", "/users/{id}", "grape")
	assertInlineEndpointBridged(t, ents, rels, "POST", "/users/invite", "grape")
	assertInlineEndpointBridged(t, ents, rels, "GET", "/users/v1", "grape")
}

// TestInline4417_GrapeNoDoubleEmit guards that a route inside a resource block is
// NOT also emitted at the un-prefixed parent scope.
func TestInline4417_GrapeNoDoubleEmit(t *testing.T) {
	src := `require 'grape'

class API < Grape::API
  resource :things do
    get ':id' do
      Thing.find(params[:id])
    end
  end
end
`
	ents, _ := detectInline(t, "ruby", "app/things_api.rb", src)
	if endpointByVerbPath(ents, "GET", "/things/{id}") == nil {
		t.Fatal("nested route GET /things/{id} missing")
	}
	if endpointByVerbPath(ents, "GET", "/{id}") != nil {
		t.Error("nested route must NOT also be emitted un-prefixed as GET /{id}")
	}
}

// TestInline4417_RodaRoutingTree covers a Roda routing-tree app. `r.on "users"
// do ... end` contributes "/users"; the leaf `r.get`/`r.post` verbs emit there.
// A class-matcher capture (`r.get Integer do |id|`) → "/users/{param}"; a nested
// `r.is "profile" do r.get do ... end end` → "/users/profile".
func TestInline4417_RodaRoutingTree(t *testing.T) {
	src := `require 'roda'

class App < Roda
  route do |r|
    r.on "users" do
      r.get do
        User.all
      end

      r.get Integer do |id|
        User.find(id)
      end

      r.is "profile" do
        r.post do
          User.update_profile(r.params)
        end
      end
    end
  end
end
`
	ents, rels := detectInline(t, "ruby", "app/roda_app.rb", src)
	assertInlineEndpointBridged(t, ents, rels, "GET", "/users", "roda")
	assertInlineEndpointBridged(t, ents, rels, "GET", "/users/{param}", "roda")
	assertInlineEndpointBridged(t, ents, rels, "POST", "/users/profile", "roda")
}

// TestInline4417_RodaInlineVerbMatcher covers the `r.get Integer do |id| ... end`
// leaf-with-inline-capture form directly at the routing-tree root.
func TestInline4417_RodaInlineVerbMatcher(t *testing.T) {
	src := `require 'roda'

class App < Roda
  route do |r|
    r.on "items" do
      r.get String do |slug|
        Item.by_slug(slug)
      end
    end
  end
end
`
	ents, rels := detectInline(t, "ruby", "app/items.rb", src)
	assertInlineEndpointBridged(t, ents, rels, "GET", "/items/{param}", "roda")
}

// TestInline4417_NonRouteRubyUnaffected is the regression guard: a plain Ruby
// class with ordinary methods (no Grape::API / Roda superclass, no route DSL)
// must NOT yield any synthesized endpoints.
func TestInline4417_NonRouteRubyUnaffected(t *testing.T) {
	src := `class Calculator
  def add(a, b)
    a + b
  end

  def get(key)
    @store[key]
  end
end
`
	ents, _ := detectInline(t, "ruby", "lib/calculator.rb", src)
	for _, e := range ents {
		if e.Kind == httpEndpointDefinitionKind {
			p := e.Properties
			if p != nil && (p["framework"] == "grape" || p["framework"] == "roda") {
				t.Errorf("non-route Ruby produced a grape/roda endpoint: %v", p)
			}
		}
	}
}
