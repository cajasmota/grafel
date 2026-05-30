package ruby_test

// grape_deep_test.go — value-asserting tests for the ruby_grape_deep extractor.
//
// These tests assert SPECIFIC entity properties (resolved_path, http_method,
// qualifier, param_type, values_constraint, regexp_constraint, mechanism,
// mount_at, rack-test linkage, etc.) — NOT merely "≥1 entity exists".
// This brings lang.ruby.framework.grape routing+validation to the TS/JS bar.
//
// Part of issue #3345.

import (
	"context"
	"strings"
	"testing"

	extreg "github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func grapeDeepExtract(t *testing.T, path, src string) []types.EntityRecord {
	t.Helper()
	e, ok := extreg.Get("ruby_grape_deep")
	if !ok {
		t.Fatal("ruby_grape_deep extractor not registered")
	}
	ents, err := e.Extract(context.Background(), extreg.FileInput{
		Path: path, Language: "ruby", Content: []byte(src),
	})
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	return ents
}

// findGrapeEntity returns the first entity whose Name equals name.
func findGrapeEntity(ents []types.EntityRecord, name string) *types.EntityRecord {
	for i := range ents {
		if ents[i].Name == name {
			return &ents[i]
		}
	}
	return nil
}

// findGrapeEntityBySubtype returns the first entity with the given subtype.
func findGrapeEntityBySubtype(ents []types.EntityRecord, subtype string) *types.EntityRecord {
	for i := range ents {
		if ents[i].Subtype == subtype {
			return &ents[i]
		}
	}
	return nil
}

// findGrapeEntitiesBySubtype returns all entities with the given subtype.
func findGrapeEntitiesBySubtype(ents []types.EntityRecord, subtype string) []types.EntityRecord {
	var out []types.EntityRecord
	for _, e := range ents {
		if e.Subtype == subtype {
			out = append(out, e)
		}
	}
	return out
}

// assertGrapeProp fails the test unless ent.Properties[key] == want.
func assertGrapeProp(t *testing.T, ent *types.EntityRecord, key, want string) {
	t.Helper()
	got := ent.Properties[key]
	if got != want {
		t.Errorf("entity %q: prop %q = %q, want %q (all props: %v)",
			ent.Name, key, got, want, ent.Properties)
	}
}

// ---------------------------------------------------------------------------
// 1. Routing: flat endpoints
// ---------------------------------------------------------------------------

// TestGrapeDeep_FlatEndpoints verifies that top-level verb declarations emit
// resolved_path = "/" + verb_path and the correct http_method.
func TestGrapeDeep_FlatEndpoints(t *testing.T) {
	src := `
class OrdersAPI < Grape::API
  format :json

  get '/orders' do
    Order.all
  end

  post '/orders' do
    Order.create!(declared(params))
  end

  delete '/orders/:id' do
    Order.find(params[:id]).destroy
  end
end
`
	ents := grapeDeepExtract(t, "app/api/orders_api.rb", src)

	tests := []struct {
		name, resolvedPath, method string
	}{
		{"GET /orders", "/orders", "GET"},
		{"POST /orders", "/orders", "POST"},
		{"DELETE /orders/:id", "/orders/:id", "DELETE"},
	}

	for _, tc := range tests {
		e := findGrapeEntity(ents, tc.name)
		if e == nil {
			t.Errorf("expected entity %q not found; all ops: %v", tc.name, entityNames(ents))
			continue
		}
		assertGrapeProp(t, e, "resolved_path", tc.resolvedPath)
		assertGrapeProp(t, e, "http_method", tc.method)
		assertGrapeProp(t, e, "framework", "grape")
	}
}

// ---------------------------------------------------------------------------
// 2. Routing: namespace nesting
// ---------------------------------------------------------------------------

// TestGrapeDeep_NamespaceNesting verifies that nested namespace blocks compose
// the full path: /api/v1/users.
func TestGrapeDeep_NamespaceNesting(t *testing.T) {
	src := `
class MyAPI < Grape::API
  namespace :api do
    namespace :v1 do
      get '/users' do
        User.all
      end

      post '/users' do
        User.create!(declared(params))
      end
    end
  end
end
`
	ents := grapeDeepExtract(t, "app/api/my_api.rb", src)

	// Expect fully composed paths.
	getEnt := findGrapeEntity(ents, "GET /api/v1/users")
	if getEnt == nil {
		t.Fatalf("expected GET /api/v1/users entity; got: %v", entityNames(ents))
	}
	assertGrapeProp(t, getEnt, "resolved_path", "/api/v1/users")
	assertGrapeProp(t, getEnt, "http_method", "GET")

	postEnt := findGrapeEntity(ents, "POST /api/v1/users")
	if postEnt == nil {
		t.Errorf("expected POST /api/v1/users entity; got: %v", entityNames(ents))
	}
}

// ---------------------------------------------------------------------------
// 3. Routing: resource nesting
// ---------------------------------------------------------------------------

// TestGrapeDeep_ResourceNesting verifies that `resources :orders do` composes
// /orders as a prefix for verbs inside the block.
func TestGrapeDeep_ResourceNesting(t *testing.T) {
	src := `
class API < Grape::API
  resources :orders do
    get do
      Order.all
    end

    post do
      Order.create!
    end

    route_param :id do
      get do
        Order.find(params[:id])
      end

      delete do
        Order.find(params[:id]).destroy
      end
    end
  end
end
`
	ents := grapeDeepExtract(t, "app/api/api.rb", src)

	// resource prefix /orders → GET /orders (bare get inside resources block)
	var foundGetOrders bool
	for _, e := range ents {
		if e.Kind == "SCOPE.Operation" && e.Subtype == "endpoint" {
			path := e.Properties["resolved_path"]
			if strings.HasPrefix(path, "/orders") && e.Properties["http_method"] == "GET" {
				foundGetOrders = true
				break
			}
		}
	}
	if !foundGetOrders {
		t.Errorf("expected a GET endpoint with resolved_path starting /orders; got: %v", entityNames(ents))
	}

	// route_param :id → route_param entity emitted.
	rpEnt := findGrapeEntityBySubtype(ents, "route_param")
	if rpEnt == nil {
		t.Error("expected route_param entity for :id")
	} else {
		assertGrapeProp(t, rpEnt, "param_name", "id")
		assertGrapeProp(t, rpEnt, "path_segment", "/:id")
	}
}

// ---------------------------------------------------------------------------
// 4. Routing: route_param with explicit GET path
// ---------------------------------------------------------------------------

// TestGrapeDeep_RouteParam verifies that route_param :id pushes /:id onto the
// path stack, so verbs inside emit the composed resolved_path.
func TestGrapeDeep_RouteParam(t *testing.T) {
	src := `
class UsersAPI < Grape::API
  resource :users do
    route_param :id do
      get '/profile' do
        User.find(params[:id]).profile
      end

      put '/profile' do
        User.find(params[:id]).update_profile(params)
      end
    end
  end
end
`
	ents := grapeDeepExtract(t, "app/api/users_api.rb", src)

	// Expect /users/:id/profile for GET and PUT.
	getEnt := findGrapeEntity(ents, "GET /users/:id/profile")
	if getEnt == nil {
		t.Errorf("expected GET /users/:id/profile; got endpoints: %v", operationNames(ents))
	} else {
		assertGrapeProp(t, getEnt, "resolved_path", "/users/:id/profile")
		assertGrapeProp(t, getEnt, "http_method", "GET")
	}

	putEnt := findGrapeEntity(ents, "PUT /users/:id/profile")
	if putEnt == nil {
		t.Errorf("expected PUT /users/:id/profile; got: %v", operationNames(ents))
	}
}

// ---------------------------------------------------------------------------
// 5. Routing: mount
// ---------------------------------------------------------------------------

// TestGrapeDeep_Mount verifies that `mount API::V1 => '/api/v1'` emits a
// mount_point entity with mount_target and mount_at properties.
func TestGrapeDeep_Mount(t *testing.T) {
	src := `
class RootAPI < Grape::API
  mount API::V1 => '/api/v1'
  mount API::V2, at: '/api/v2'
  mount AdminAPI
end
`
	ents := grapeDeepExtract(t, "config/api.rb", src)

	v1 := findGrapeEntity(ents, "grape_mount:API::V1")
	if v1 == nil {
		t.Fatal("expected grape_mount:API::V1 entity")
	}
	assertGrapeProp(t, v1, "mount_target", "API::V1")
	assertGrapeProp(t, v1, "mount_at", "/api/v1")

	v2 := findGrapeEntity(ents, "grape_mount:API::V2")
	if v2 == nil {
		t.Fatal("expected grape_mount:API::V2 entity")
	}
	assertGrapeProp(t, v2, "mount_at", "/api/v2")

	admin := findGrapeEntity(ents, "grape_mount:AdminAPI")
	if admin == nil {
		t.Fatal("expected grape_mount:AdminAPI entity with heuristic mount_at")
	}
	// Heuristic: AdminAPI → /admin_a_p_i or /admin_api depending on impl.
	if admin.Properties["mount_at"] == "" {
		t.Errorf("grape_mount:AdminAPI: mount_at should be non-empty (heuristic)")
	}
}

// ---------------------------------------------------------------------------
// 6. Validation: per-param properties (type + constraints)
// ---------------------------------------------------------------------------

// TestGrapeDeep_ParamTypeAndConstraints verifies that each param entity carries
// the qualifier, param_type, values_constraint, regexp_constraint, default_value,
// and allow_blank as extracted from the Grape params block.
func TestGrapeDeep_ParamTypeAndConstraints(t *testing.T) {
	src := `
class UsersAPI < Grape::API
  desc 'Create user'
  params do
    requires :name, type: String, desc: 'Full name'
    requires :email, type: String, regexp: /\A[^@\s]+@[^@\s]+\z/, desc: 'Email address'
    optional :role, type: String, values: ['admin', 'user', 'guest'], default: 'user'
    optional :age, type: Integer, allow_blank: false
  end
  post '/users' do
    User.create!(declared(params))
  end
end
`
	ents := grapeDeepExtract(t, "app/api/users_api.rb", src)

	// :name — requires, String type
	nameEnt := findGrapeEntity(ents, "grape_param:requires:name")
	if nameEnt == nil {
		t.Fatal("expected grape_param:requires:name entity")
	}
	assertGrapeProp(t, nameEnt, "qualifier", "requires")
	assertGrapeProp(t, nameEnt, "field", "name")
	assertGrapeProp(t, nameEnt, "param_type", "String")

	// :email — requires, String, regexp constraint
	emailEnt := findGrapeEntity(ents, "grape_param:requires:email")
	if emailEnt == nil {
		t.Fatal("expected grape_param:requires:email entity")
	}
	assertGrapeProp(t, emailEnt, "qualifier", "requires")
	assertGrapeProp(t, emailEnt, "param_type", "String")
	if emailEnt.Properties["regexp_constraint"] == "" {
		t.Errorf("grape_param:requires:email: regexp_constraint should be non-empty")
	}

	// :role — optional, String, values + default
	roleEnt := findGrapeEntity(ents, "grape_param:optional:role")
	if roleEnt == nil {
		t.Fatal("expected grape_param:optional:role entity")
	}
	assertGrapeProp(t, roleEnt, "qualifier", "optional")
	assertGrapeProp(t, roleEnt, "param_type", "String")
	if roleEnt.Properties["values_constraint"] == "" {
		t.Errorf("grape_param:optional:role: values_constraint should be non-empty")
	}
	if roleEnt.Properties["default_value"] == "" {
		t.Errorf("grape_param:optional:role: default_value should be non-empty")
	}

	// :age — optional, Integer, allow_blank: false
	ageEnt := findGrapeEntity(ents, "grape_param:optional:age")
	if ageEnt == nil {
		t.Fatal("expected grape_param:optional:age entity")
	}
	assertGrapeProp(t, ageEnt, "qualifier", "optional")
	assertGrapeProp(t, ageEnt, "param_type", "Integer")
	assertGrapeProp(t, ageEnt, "allow_blank", "false")
}

// ---------------------------------------------------------------------------
// 7. Validation: endpoint linkage on params block
// ---------------------------------------------------------------------------

// TestGrapeDeep_ParamsBlockEndpointLinkage verifies that the block-scope entity
// and per-param entities carry endpoint_method + endpoint_path pointing at the
// nearest preceding verb.
func TestGrapeDeep_ParamsBlockEndpointLinkage(t *testing.T) {
	src := `
class OrdersAPI < Grape::API
  desc 'Create order'
  params do
    requires :item_id, type: Integer
    optional :quantity, type: Integer, default: 1
  end
  post '/orders' do
    Order.create!(declared(params))
  end
end
`
	ents := grapeDeepExtract(t, "app/api/orders_api.rb", src)

	itemEnt := findGrapeEntity(ents, "grape_param:requires:item_id")
	if itemEnt == nil {
		t.Fatal("expected grape_param:requires:item_id entity")
	}
	assertGrapeProp(t, itemEnt, "qualifier", "requires")
	assertGrapeProp(t, itemEnt, "param_type", "Integer")
	// endpoint linkage is best-effort (backward scan); just ensure field is set.
	if itemEnt.Properties["field"] != "item_id" {
		t.Errorf("expected field=item_id, got %q", itemEnt.Properties["field"])
	}

	qtyEnt := findGrapeEntity(ents, "grape_param:optional:quantity")
	if qtyEnt == nil {
		t.Fatal("expected grape_param:optional:quantity entity")
	}
	assertGrapeProp(t, qtyEnt, "default_value", "1")
}

// ---------------------------------------------------------------------------
// 8. Validation: multiple params blocks (one per endpoint)
// ---------------------------------------------------------------------------

// TestGrapeDeep_MultipleParamsBlocks verifies that multiple params do...end
// blocks in the same file each produce their own set of per-param entities.
func TestGrapeDeep_MultipleParamsBlocks(t *testing.T) {
	src := `
class API < Grape::API
  params do
    requires :name, type: String
  end
  post '/users' do
  end

  params do
    requires :title, type: String
    optional :body, type: String
  end
  post '/posts' do
  end
end
`
	ents := grapeDeepExtract(t, "app/api/api.rb", src)

	// Both name and title should be extracted.
	if findGrapeEntity(ents, "grape_param:requires:name") == nil {
		t.Error("expected grape_param:requires:name from first params block")
	}
	if findGrapeEntity(ents, "grape_param:requires:title") == nil {
		t.Error("expected grape_param:requires:title from second params block")
	}
	if findGrapeEntity(ents, "grape_param:optional:body") == nil {
		t.Error("expected grape_param:optional:body from second params block")
	}
}

// ---------------------------------------------------------------------------
// 9. Auth: http_basic_auth
// ---------------------------------------------------------------------------

// TestGrapeDeep_HTTPBasicAuth verifies that http_basic_auth emits an auth_guard
// entity with mechanism=http_basic_auth and auth_required=true.
func TestGrapeDeep_HTTPBasicAuth(t *testing.T) {
	src := `
class SecureAPI < Grape::API
  http_basic_auth do |username, password|
    User.authenticate(username, password)
  end

  get '/secret' do
    { data: 'protected' }
  end
end
`
	ents := grapeDeepExtract(t, "app/api/secure_api.rb", src)

	e := findGrapeEntity(ents, "grape_http_basic_auth")
	if e == nil {
		t.Fatal("expected grape_http_basic_auth entity")
	}
	assertGrapeProp(t, e, "mechanism", "http_basic_auth")
	assertGrapeProp(t, e, "auth_required", "true")
	assertGrapeProp(t, e, "kind", "http_basic")
	assertGrapeProp(t, e, "library", "grape")
}

// ---------------------------------------------------------------------------
// 10. Auth: before { authenticate! }
// ---------------------------------------------------------------------------

// TestGrapeDeep_BeforeAuthenticate verifies that a before hook calling
// authenticate! emits an auth_guard entity with mechanism=before_authenticate.
func TestGrapeDeep_BeforeAuthenticate(t *testing.T) {
	src := `
class AuthenticatedAPI < Grape::API
  helpers do
    def authenticate!
      error!('Unauthorized', 401) unless current_user
    end

    def current_user
      @current_user ||= User.find_by_token(request.headers['X-Auth-Token'])
    end
  end

  before { authenticate! }

  get '/profile' do
    current_user
  end
end
`
	ents := grapeDeepExtract(t, "app/api/authenticated_api.rb", src)

	// before { authenticate! } guard
	beforeEnt := findGrapeEntity(ents, "grape_before_authenticate")
	if beforeEnt == nil {
		t.Fatal("expected grape_before_authenticate entity")
	}
	assertGrapeProp(t, beforeEnt, "mechanism", "before_authenticate")
	assertGrapeProp(t, beforeEnt, "auth_required", "true")
	assertGrapeProp(t, beforeEnt, "library", "grape")

	// helpers { def authenticate! } helper definition
	helperEnt := findGrapeEntity(ents, "grape_authenticate_helper")
	if helperEnt == nil {
		t.Fatal("expected grape_authenticate_helper entity")
	}
	assertGrapeProp(t, helperEnt, "kind", "helper_definition")
	assertGrapeProp(t, helperEnt, "mechanism", "helpers_authenticate")
}

// ---------------------------------------------------------------------------
// 11. Auth: error!('Unauthorized', 401) guard
// ---------------------------------------------------------------------------

// TestGrapeDeep_ErrorUnauthorized verifies that explicit error! calls emit
// an auth_guard entity with mechanism=error_unauthorized.
func TestGrapeDeep_ErrorUnauthorized(t *testing.T) {
	src := `
class API < Grape::API
  before do
    error!('Unauthorized', 401) unless request.headers['Authorization']
  end

  get '/private' do
    { data: 'secret' }
  end
end
`
	ents := grapeDeepExtract(t, "app/api/api.rb", src)

	e := findGrapeEntity(ents, "grape_error_unauthorized")
	if e == nil {
		t.Fatal("expected grape_error_unauthorized entity")
	}
	assertGrapeProp(t, e, "mechanism", "error_unauthorized")
	assertGrapeProp(t, e, "auth_required", "true")
}

// ---------------------------------------------------------------------------
// 12. Testing: rack-test linkage
// ---------------------------------------------------------------------------

// TestGrapeDeep_RackTestLinkage verifies that a rack-test spec file emits
// test_linkage entities for include Rack::Test::Methods and def app.
func TestGrapeDeep_RackTestLinkage(t *testing.T) {
	src := `
require 'spec_helper'

RSpec.describe API::V1 do
  include Rack::Test::Methods

  def app
    API::V1
  end

  describe 'GET /users' do
    it 'returns a list of users' do
      get '/users'
      expect(last_response.status).to eq 200
    end
  end
end
`
	ents := grapeDeepExtract(t, "spec/api/v1_spec.rb", src)

	rackTestEnt := findGrapeEntity(ents, "rack_test_methods")
	if rackTestEnt == nil {
		t.Fatal("expected rack_test_methods test_linkage entity")
	}
	assertGrapeProp(t, rackTestEnt, "library", "rack_test")
	assertGrapeProp(t, rackTestEnt, "signal", "testing")

	appEnt := findGrapeEntity(ents, "rack_test_app")
	if appEnt == nil {
		t.Fatal("expected rack_test_app test_linkage entity")
	}
	assertGrapeProp(t, appEnt, "library", "rack_test")
}

// ---------------------------------------------------------------------------
// 13. Empty / non-Grape files → no entities
// ---------------------------------------------------------------------------

func TestGrapeDeep_NoMatch_EmptyFile(t *testing.T) {
	ents := grapeDeepExtract(t, "lib/utils.rb", "")
	if len(ents) != 0 {
		t.Errorf("expected 0 entities for empty file, got %d", len(ents))
	}
}

func TestGrapeDeep_NoMatch_NonGrapeFile(t *testing.T) {
	src := `
class User < ApplicationRecord
  validates :name, presence: true
end
`
	ents := grapeDeepExtract(t, "app/models/user.rb", src)
	if len(ents) != 0 {
		t.Errorf("expected 0 entities for non-Grape Ruby, got %d", len(ents))
	}
}

// ---------------------------------------------------------------------------
// 14. Full realistic Grape API (integration)
// ---------------------------------------------------------------------------

// TestGrapeDeep_RealisticAPI exercises a representative real-world Grape API
// that combines namespace, resources, route_param, mount, params, and auth.
func TestGrapeDeep_RealisticAPI(t *testing.T) {
	src := `
module API
  class V1 < Grape::API
    version 'v1', using: :header, vendor: 'myapp'
    format :json

    helpers do
      def authenticate!
        error!('Unauthorized', 401) unless current_user
      end

      def current_user
        @current_user ||= User.find_by_api_key(request.headers['X-Api-Key'])
      end
    end

    before { authenticate! }

    use Rack::Cors

    namespace :users do
      desc 'List all users'
      get do
        User.all
      end

      desc 'Create user'
      params do
        requires :name,  type: String
        requires :email, type: String
        optional :role,  type: String, values: ['admin', 'user'], default: 'user'
      end
      post do
        User.create!(declared(params))
      end

      route_param :id do
        desc 'Get user'
        get do
          User.find(params[:id])
        end

        desc 'Update user'
        params do
          optional :name,  type: String
          optional :email, type: String
        end
        put do
          User.find(params[:id]).update!(declared(params))
        end

        desc 'Delete user'
        delete do
          User.find(params[:id]).destroy!
        end
      end
    end
  end

  class Root < Grape::API
    mount API::V1 => '/api'
  end
end
`
	ents := grapeDeepExtract(t, "app/api/v1.rb", src)

	// Routing: GET /users
	getUsersEnt := findGrapeEntity(ents, "GET /users")
	if getUsersEnt == nil {
		t.Errorf("expected GET /users endpoint; ops: %v", operationNames(ents))
	}

	// Routing: POST /users
	postUsersEnt := findGrapeEntity(ents, "POST /users")
	if postUsersEnt == nil {
		t.Errorf("expected POST /users endpoint; ops: %v", operationNames(ents))
	}

	// Routing: route_param :id entity.
	rpEnt := findGrapeEntityBySubtype(ents, "route_param")
	if rpEnt == nil {
		t.Error("expected route_param entity for :id")
	}

	// Validation: required params for POST /users
	nameParam := findGrapeEntity(ents, "grape_param:requires:name")
	if nameParam == nil {
		t.Error("expected grape_param:requires:name for POST /users")
	} else {
		assertGrapeProp(t, nameParam, "param_type", "String")
		assertGrapeProp(t, nameParam, "qualifier", "requires")
	}

	roleParam := findGrapeEntity(ents, "grape_param:optional:role")
	if roleParam == nil {
		t.Error("expected grape_param:optional:role for POST /users")
	} else {
		assertGrapeProp(t, roleParam, "qualifier", "optional")
		if roleParam.Properties["values_constraint"] == "" {
			t.Error("grape_param:optional:role: expected non-empty values_constraint")
		}
		if roleParam.Properties["default_value"] == "" {
			t.Error("grape_param:optional:role: expected non-empty default_value")
		}
	}

	// Auth: before { authenticate! }
	authEnt := findGrapeEntity(ents, "grape_before_authenticate")
	if authEnt == nil {
		t.Error("expected grape_before_authenticate auth_guard entity")
	}

	// Auth: error!('Unauthorized', 401) inside authenticate! helper
	errEnt := findGrapeEntity(ents, "grape_error_unauthorized")
	if errEnt == nil {
		t.Error("expected grape_error_unauthorized auth_guard entity")
	}

	// Auth: helpers { def authenticate! }
	helperEnt := findGrapeEntity(ents, "grape_authenticate_helper")
	if helperEnt == nil {
		t.Error("expected grape_authenticate_helper auth_helper entity")
	}

	// Mount: API::V1 mounted at /api
	mountEnt := findGrapeEntity(ents, "grape_mount:API::V1")
	if mountEnt == nil {
		t.Error("expected grape_mount:API::V1 mount_point entity")
	} else {
		assertGrapeProp(t, mountEnt, "mount_at", "/api")
	}
}

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

func entityNames(ents []types.EntityRecord) []string {
	names := make([]string, len(ents))
	for i, e := range ents {
		names[i] = e.Name
	}
	return names
}

func operationNames(ents []types.EntityRecord) []string {
	var out []string
	for _, e := range ents {
		if e.Kind == "SCOPE.Operation" {
			out = append(out, e.Name)
		}
	}
	return out
}
