package engine

import (
	"testing"
)

// #4385 — Sinatra route blocks (`get '/x' do ... end`) are inherently anonymous:
// the handler is ALWAYS a block, never a named method. Before the fix
// synthesizeSinatra emitted the endpoint with empty refKind/refName, leaving it a
// graph ISLAND with no endpoint→handler IMPLEMENTS bridge (invisible to flow /
// IMPLEMENTS traversal).
//
// The fix generalises the #4324/#4382/#4384 inline-handler mechanism to Ruby:
// every Sinatra verb-block route signals refKind=inlineHandlerRefKind, so
// makeEmit synthesizes a stable `<inline VERB /path>` handler entity (Name from
// verb+canonical path → merge-stable) + a same-file IMPLEMENTS bridge bound by
// the central resolver post-merge. sinatra-contrib `namespace '/api' do ... end`
// path prefixes compose onto the route before canonicalization.
//
// These tests run the REAL extract+synthesis+merge+resolve pipeline (via
// detectInline / assertInlineEndpointBridged from the #4324 test file) on
// faithful Sinatra fixtures and prove the endpoints are present AND
// handler-linked (not islands).

// TestInline4385_SinatraClassicRoutes covers classic top-level Sinatra routes
// (`get '/path' do`) across verbs, plus a `:param`-templated route.
func TestInline4385_SinatraClassicRoutes(t *testing.T) {
	src := `require 'sinatra'

get '/health' do
  'ok'
end

post '/items' do
  create_item(params)
end

put '/items/:id' do |id|
  update_item(id, params)
end

delete '/items/:id' do
  destroy_item(params[:id])
end
`
	ents, rels := detectInline(t, "ruby", "app.rb", src)
	assertInlineEndpointBridged(t, ents, rels, "GET", "/health", "sinatra")
	assertInlineEndpointBridged(t, ents, rels, "POST", "/items", "sinatra")
	assertInlineEndpointBridged(t, ents, rels, "PUT", "/items/{id}", "sinatra")
	assertInlineEndpointBridged(t, ents, rels, "DELETE", "/items/{id}", "sinatra")
}

// TestInline4385_SinatraModularRoutes covers modular style
// (`class App < Sinatra::Base`) — the verb routes live inside a class body but
// the block-handler shape is identical, so the inline-handler synth must fire.
func TestInline4385_SinatraModularRoutes(t *testing.T) {
	src := `require 'sinatra/base'

class App < Sinatra::Base
  get '/users' do
    json User.all
  end

  patch '/users/:id' do |id|
    User.update(id, params)
  end
end
`
	ents, rels := detectInline(t, "ruby", "lib/app.rb", src)
	assertInlineEndpointBridged(t, ents, rels, "GET", "/users", "sinatra")
	assertInlineEndpointBridged(t, ents, rels, "PATCH", "/users/{id}", "sinatra")
}

// TestInline4385_SinatraNamespacePrefix covers sinatra-contrib namespace blocks:
// `namespace '/api' do ... end` composes "/api" onto each enclosed route path.
func TestInline4385_SinatraNamespacePrefix(t *testing.T) {
	src := `require 'sinatra/base'
require 'sinatra/contrib'

class App < Sinatra::Base
  register Sinatra::Namespace

  get '/ping' do
    'pong'
  end

  namespace '/api' do
    get '/widgets' do
      json Widget.all
    end

    post '/widgets/:id' do |id|
      Widget.touch(id)
    end
  end
end
`
	ents, rels := detectInline(t, "ruby", "lib/api_app.rb", src)
	// Root-scope route keeps its bare path.
	assertInlineEndpointBridged(t, ents, rels, "GET", "/ping", "sinatra")
	// Namespaced routes get the "/api" prefix composed in.
	assertInlineEndpointBridged(t, ents, rels, "GET", "/api/widgets", "sinatra")
	assertInlineEndpointBridged(t, ents, rels, "POST", "/api/widgets/{id}", "sinatra")
}

// TestInline4385_SinatraNamespaceNoDoubleEmit guards that a namespaced route is
// NOT also emitted at the un-prefixed parent scope (stripSinatraNamespaceBlocks
// must blank the namespace body for the parent-level pass).
func TestInline4385_SinatraNamespaceNoDoubleEmit(t *testing.T) {
	src := `require 'sinatra/base'

class App < Sinatra::Base
  namespace '/api' do
    get '/things' do
      'things'
    end
  end
end
`
	ents, _ := detectInline(t, "ruby", "lib/nodup.rb", src)
	if endpointByVerbPath(ents, "GET", "/api/things") == nil {
		t.Fatal("namespaced route GET /api/things missing")
	}
	if endpointByVerbPath(ents, "GET", "/things") != nil {
		t.Error("namespaced route must NOT also be emitted un-prefixed as GET /things")
	}
}
