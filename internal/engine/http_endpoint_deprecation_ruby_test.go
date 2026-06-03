package engine

import "testing"

// ---------------------------------------------------------------------------
// Ruby (Rails / Sinatra) deprecation + api_version port (epic #3628).
//
// Mirrors the flagship property contract exactly: deprecated / deprecated_since
// / deprecated_replacement / deprecation_source / api_version. Uses the shared
// deprecProps/mustEndpoint harness from http_endpoint_deprecation_test.go.
// ---------------------------------------------------------------------------

// A Sinatra verb block with a YARD `# @deprecated use /api/v2/users` comment
// under an /api/v1 path → deprecated=true + replacement + source + api_version=1
// (path-derived). The canonical Ruby idiom.
func TestDeprecation_RubySinatraYardDeprecated(t *testing.T) {
	src := `require 'sinatra'

# @deprecated use /api/v2/users instead
get '/api/v1/users' do
  "users"
end

get '/api/v1/health' do
  "ok"
end
`
	eps := deprecProps(t, "ruby", "app.rb", src)

	dep := mustEndpoint(t, eps, "GET /api/v1/users")
	if dep.Properties["deprecated"] != "true" {
		t.Fatalf("GET /api/v1/users deprecated=%q, want true (props: %v)", dep.Properties["deprecated"], dep.Properties)
	}
	if got := dep.Properties["deprecation_source"]; got != "# @deprecated" {
		t.Errorf("deprecation_source=%q, want '# @deprecated'", got)
	}
	if got := dep.Properties["deprecated_replacement"]; got != "/api/v2/users" {
		t.Errorf("deprecated_replacement=%q, want /api/v2/users", got)
	}
	if got := dep.Properties["api_version"]; got != "1" {
		t.Errorf("api_version=%q, want 1 (path-derived)", got)
	}

	// Negative: the non-deprecated sibling carries no deprecation (and the
	// @deprecated comment above /api/v1/users does NOT leak onto it).
	live := mustEndpoint(t, eps, "GET /api/v1/health")
	if _, ok := live.Properties["deprecated"]; ok {
		t.Fatalf("GET /api/v1/health deprecation fabricated, want absent (props: %v)", live.Properties)
	}
	// But the version segment still pins api_version on the live endpoint.
	if got := live.Properties["api_version"]; got != "1" {
		t.Errorf("GET /api/v1/health api_version=%q, want 1", got)
	}
}

// `# @deprecated since 2.0 use /reports/v2 instead` resolves BOTH the
// since-version and the replacement out of the message via the shared parser.
func TestDeprecation_RubySinceAndReplacement(t *testing.T) {
	src := `require 'sinatra'

# @deprecated since 2.0 use /reports/v2 instead
get '/reports' do
  "reports"
end
`
	eps := deprecProps(t, "ruby", "app.rb", src)
	dep := mustEndpoint(t, eps, "GET /reports")
	if dep.Properties["deprecated"] != "true" {
		t.Fatalf("GET /reports deprecated=%q, want true", dep.Properties["deprecated"])
	}
	if got := dep.Properties["deprecated_since"]; got != "2.0" {
		t.Errorf("deprecated_since=%q, want 2.0", got)
	}
	if got := dep.Properties["deprecated_replacement"]; got != "/reports/v2" {
		t.Errorf("deprecated_replacement=%q, want /reports/v2", got)
	}
}

// A `# Deprecated: <msg>` plain-prose doc comment (the colon convention) marks
// the Sinatra block deprecated.
func TestDeprecation_RubyDeprecatedColonComment(t *testing.T) {
	src := `require 'sinatra'

# Deprecated: use /legacy/v2
get '/legacy' do
  "legacy"
end
`
	eps := deprecProps(t, "ruby", "app.rb", src)
	dep := mustEndpoint(t, eps, "GET /legacy")
	if dep.Properties["deprecated"] != "true" {
		t.Fatalf("GET /legacy deprecated=%q, want true (props: %v)", dep.Properties["deprecated"], dep.Properties)
	}
	if got := dep.Properties["deprecation_source"]; got != "# Deprecated:" {
		t.Errorf("deprecation_source=%q, want '# Deprecated:'", got)
	}
	if got := dep.Properties["deprecated_replacement"]; got != "/legacy/v2" {
		t.Errorf("deprecated_replacement=%q, want /legacy/v2", got)
	}
}

// A Sunset response header written in a Sinatra block body is the cross-language
// runtime deprecation signal (flagship path), proven to fire for Ruby — and
// proven NOT to leak across sibling blocks now that the @deprecated comment on a
// neighbouring block is resolved from its own (bounded-above) comment region.
func TestDeprecation_RubySunsetResponseHeader(t *testing.T) {
	src := `require 'sinatra'

get '/payments' do
  response.headers['Sunset'] = 'Sat, 31 Dec 2025 23:59:59 GMT'
  "paid"
end
`
	eps := deprecProps(t, "ruby", "app.rb", src)
	dep := mustEndpoint(t, eps, "GET /payments")
	if dep.Properties["deprecated"] != "true" {
		t.Fatalf("GET /payments deprecated=%q, want true (props: %v)", dep.Properties["deprecated"], dep.Properties)
	}
	if got := dep.Properties["deprecation_source"]; got != "Sunset response header" {
		t.Errorf("deprecation_source=%q, want 'Sunset response header'", got)
	}
}

// Rails namespace-versioned routes pin api_version from the path the namespace
// stack composes (`namespace :api do; namespace :v1 do`), with no Ruby-specific
// code — the path-derived flagship version reads the canonical path.
func TestAPIVersion_RailsNamespaceVersion(t *testing.T) {
	src := `Rails.application.routes.draw do
  namespace :api do
    namespace :v2 do
      get '/users', to: 'users#index'
    end
  end
end
`
	eps := deprecProps(t, "ruby", "config/routes.rb", src)
	e := mustEndpoint(t, eps, "GET /api/v2/users")
	if got := e.Properties["api_version"]; got != "2" {
		t.Fatalf("api_version=%q, want 2 (Rails namespace-derived path)", got)
	}
}

// A Rails `scope '/api/v3'` block likewise pins api_version from the composed
// path.
func TestAPIVersion_RailsScopeVersion(t *testing.T) {
	src := `Rails.application.routes.draw do
  scope '/api/v3' do
    get '/orders', to: 'orders#index'
  end
end
`
	eps := deprecProps(t, "ruby", "config/routes.rb", src)
	e := mustEndpoint(t, eps, "GET /api/v3/orders")
	if got := e.Properties["api_version"]; got != "3" {
		t.Fatalf("api_version=%q, want 3 (Rails scope path)", got)
	}
}

// Honest-partial: a versionless Sinatra route with no deprecation marker carries
// NEITHER api_version NOR deprecated (never fabricated).
func TestDeprecation_RubyVersionlessNonDeprecated(t *testing.T) {
	src := `require 'sinatra'

get '/status' do
  "ok"
end
`
	eps := deprecProps(t, "ruby", "app.rb", src)
	e := mustEndpoint(t, eps, "GET /status")
	if got, ok := e.Properties["api_version"]; ok {
		t.Fatalf("api_version=%q fabricated on versionless route, want absent", got)
	}
	if got, ok := e.Properties["deprecated"]; ok {
		t.Fatalf("deprecated=%q fabricated on plain route, want absent", got)
	}
}

// Honest-partial: a Rails endpoint whose deprecation lives in a separate
// controller-action file is NOT credited from routes.rb synthesis (the per-file
// pass never sees the controller). The route still synthesises (and pins
// api_version) — only `deprecated` is honestly absent.
func TestDeprecation_RailsControllerCommentIsPartial(t *testing.T) {
	// routes.rb names the endpoint; the `# @deprecated` comment would live in
	// app/controllers/api/v1/users_controller.rb, which this file does not carry.
	src := `Rails.application.routes.draw do
  namespace :api do
    namespace :v1 do
      get '/users', to: 'users#index'
    end
  end
end
`
	eps := deprecProps(t, "ruby", "config/routes.rb", src)
	e := mustEndpoint(t, eps, "GET /api/v1/users")
	if _, ok := e.Properties["deprecated"]; ok {
		t.Fatalf("Rails cross-file controller comment fabricated deprecation (props: %v)", e.Properties)
	}
	if got := e.Properties["api_version"]; got != "1" {
		t.Errorf("api_version=%q, want 1 (still pinned from namespace path)", got)
	}
}

// ---------------------------------------------------------------------------
// unit-level: rubyDeprecationVerdict
// ---------------------------------------------------------------------------

func TestRubyDeprecationVerdict(t *testing.T) {
	cases := []struct {
		name       string
		region     string
		wantDep    bool
		wantSource string
		wantRepl   string
		wantSince  string
	}{
		{"yard bare", "# @deprecated", true, "# @deprecated", "", ""},
		{"yard msg replacement", "# @deprecated use /api/v2/x instead", true, "# @deprecated", "/api/v2/x", ""},
		{"yard since+repl", "# @deprecated since 1.5 use /v2/y", true, "# @deprecated", "/v2/y", "1.5"},
		{"deprecated colon", "# Deprecated: use /v2/z", true, "# Deprecated:", "/v2/z", ""},
		{"deprecated colon nocase", "# DEPRECATED: gone", true, "# Deprecated:", "", ""},
		{"no marker", "# just a regular comment\nget '/x' do", false, "", "", ""},
		{"deprecated word in prose no colon", "# this is not deprecated yet", false, "", "", ""},
	}
	for _, c := range cases {
		v, ok := rubyDeprecationVerdict(c.region)
		if ok != c.wantDep {
			t.Errorf("%s: ok=%v, want %v", c.name, ok, c.wantDep)
			continue
		}
		if !c.wantDep {
			continue
		}
		if v.source != c.wantSource {
			t.Errorf("%s: source=%q, want %q", c.name, v.source, c.wantSource)
		}
		if v.replacement != c.wantRepl {
			t.Errorf("%s: replacement=%q, want %q", c.name, v.replacement, c.wantRepl)
		}
		if v.since != c.wantSince {
			t.Errorf("%s: since=%q, want %q", c.name, v.since, c.wantSince)
		}
	}
}
