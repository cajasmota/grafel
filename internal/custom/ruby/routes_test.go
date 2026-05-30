package ruby_test

// routes_test.go — tests for the ruby_routes and ruby_driver_schema extractors.
// Part of #3282.

import (
	"testing"
)

func routeExtract(t *testing.T, path, src string) []entitySummary {
	t.Helper()
	return extract(t, "ruby_routes", fi(path, "ruby", src))
}

func driverSchemaExtract(t *testing.T, path, src string) []entitySummary {
	t.Helper()
	return extract(t, "ruby_driver_schema", fi(path, "ruby", src))
}

// ---------------------------------------------------------------------------
// Grape routes
// ---------------------------------------------------------------------------

func TestRoutes_GrapeResourceBlock(t *testing.T) {
	src := `
class UsersAPI < Grape::API
  version 'v1', using: :header, vendor: 'myapp'

  resource :users do
    desc 'List all users'
    get do
      User.all
    end

    desc 'Create user'
    post do
      User.create!(name: params[:name])
    end
  end
end
`
	ents := routeExtract(t, "app/api/users_api.rb", src)
	if !containsEntity(ents, "SCOPE.Component", "grape_resource:users") {
		t.Error("expected grape_resource:users component")
	}
	if !containsEntity(ents, "SCOPE.Operation", "GET do") {
		// Grape `get do` without a path gets the block token; be permissive.
		// The extractor emits the token as the path.
		found := false
		for _, e := range ents {
			if e.Kind == "SCOPE.Operation" && e.Subtype == "endpoint" {
				found = true
				break
			}
		}
		if !found {
			t.Error("expected at least one SCOPE.Operation endpoint from Grape verbs")
		}
	}
}

func TestRoutes_GrapeExplicitPath(t *testing.T) {
	src := `
class OrdersAPI < Grape::API
  get '/orders' do
    Order.all
  end

  post '/orders' do
    Order.create!(params)
  end

  delete '/orders/:id' do
    Order.find(params[:id]).destroy
  end
end
`
	ents := routeExtract(t, "app/api/orders_api.rb", src)
	if !containsEntity(ents, "SCOPE.Operation", "GET /orders") {
		t.Error("expected GET /orders endpoint")
	}
	if !containsEntity(ents, "SCOPE.Operation", "POST /orders") {
		t.Error("expected POST /orders endpoint")
	}
	if !containsEntity(ents, "SCOPE.Operation", "DELETE /orders/:id") {
		t.Error("expected DELETE /orders/:id endpoint")
	}
}

// ---------------------------------------------------------------------------
// Sinatra routes
// ---------------------------------------------------------------------------

func TestRoutes_SinatraVerbs(t *testing.T) {
	src := `
require 'sinatra'
require 'sinatra/base'

class MyApp < Sinatra::Base
  get '/hello' do
    "Hello!"
  end

  post '/users' do
    User.create(params)
  end

  put '/users/:id' do
    User.find(params[:id]).update(params)
  end

  delete '/users/:id' do
    User.find(params[:id]).destroy
  end
end
`
	ents := routeExtract(t, "app.rb", src)
	wants := []string{
		"GET /hello",
		"POST /users",
		"PUT /users/:id",
		"DELETE /users/:id",
	}
	for _, w := range wants {
		if !containsEntity(ents, "SCOPE.Operation", w) {
			t.Errorf("expected Sinatra route %q", w)
		}
	}
}

// ---------------------------------------------------------------------------
// Hanami routes
// ---------------------------------------------------------------------------

func TestRoutes_HanamiVerbs(t *testing.T) {
	src := `
Hanami::Routes.new do
  get '/users', to: 'users.index'
  post '/users', to: 'users.create'
  get '/users/:id', to: 'users.show'
  patch '/users/:id', to: 'users.update'
  delete '/users/:id', to: 'users.destroy'
end
`
	ents := routeExtract(t, "config/routes.rb", src)
	wants := []string{
		"GET /users",
		"POST /users",
		"GET /users/:id",
		"PATCH /users/:id",
		"DELETE /users/:id",
	}
	for _, w := range wants {
		if !containsEntity(ents, "SCOPE.Operation", w) {
			t.Errorf("expected Hanami route %q", w)
		}
	}
}

// ---------------------------------------------------------------------------
// Roda routes
// ---------------------------------------------------------------------------

func TestRoutes_RodaVerbs(t *testing.T) {
	src := `
class MyApp < Roda
  route do |r|
    r.get 'users' do
      @users = User.all
    end

    r.post 'users' do
      User.create(r.params)
    end
  end
end
`
	ents := routeExtract(t, "app.rb", src)
	if !containsEntity(ents, "SCOPE.Operation", "GET users") {
		t.Error("expected GET users Roda route")
	}
	if !containsEntity(ents, "SCOPE.Operation", "POST users") {
		t.Error("expected POST users Roda route")
	}
}

// ---------------------------------------------------------------------------
// Cuba routes
// ---------------------------------------------------------------------------

func TestRoutes_CubaOn(t *testing.T) {
	src := `
App = Cuba.define do
  on('users') do
    on('new') do
      res.write render('users/new')
    end
  end

  on('login') do
    res.write render('login')
  end
end
`
	ents := routeExtract(t, "app.rb", src)
	if !containsEntity(ents, "SCOPE.Operation", "cuba_on:users") {
		t.Error("expected cuba_on:users endpoint")
	}
	if !containsEntity(ents, "SCOPE.Operation", "cuba_on:login") {
		t.Error("expected cuba_on:login endpoint")
	}
}

// ---------------------------------------------------------------------------
// dry-rb HTTP router
// ---------------------------------------------------------------------------

func TestRoutes_DryHTTPRouter(t *testing.T) {
	src := `
router = HTTP.router.get '/users' do |req|
  HTTP.router.post '/users' do |req|
  end
end
`
	ents := routeExtract(t, "config/routes.rb", src)
	if !containsEntity(ents, "SCOPE.Operation", "GET /users") {
		t.Error("expected GET /users from dry-rb HTTP.router")
	}
	if !containsEntity(ents, "SCOPE.Operation", "POST /users") {
		t.Error("expected POST /users from dry-rb HTTP.router")
	}
}

// ---------------------------------------------------------------------------
// No-match / empty
// ---------------------------------------------------------------------------

func TestRoutes_EmptyFile(t *testing.T) {
	ents := routeExtract(t, "lib/utils.rb", "")
	if len(ents) != 0 {
		t.Errorf("expected 0 entities for empty file, got %d", len(ents))
	}
}

func TestRoutes_NoRoutingSignal(t *testing.T) {
	src := `class User < ApplicationRecord; end`
	ents := routeExtract(t, "app/models/user.rb", src)
	if len(ents) != 0 {
		t.Errorf("expected 0 entities for plain model file, got %d", len(ents))
	}
}

// ---------------------------------------------------------------------------
// Driver schema — Mongoid
// ---------------------------------------------------------------------------

func TestDriverSchema_MongoidFields(t *testing.T) {
	src := `
class Article
  include Mongoid::Document

  field :title,   type: String
  field :body,    type: String
  field :views,   type: Integer
  field :active,  type: Boolean
  field :tags,    type: Array

  embeds_many :comments
  belongs_to :author, class_name: 'User'
end
`
	ents := driverSchemaExtract(t, "app/models/article.rb", src)
	wants := []string{"mongoid_field:title", "mongoid_field:body", "mongoid_field:views"}
	for _, w := range wants {
		if !containsEntitySubtype(ents, "SCOPE.Schema", "column", w) {
			t.Errorf("expected Mongoid field entity %q", w)
		}
	}
	if !containsEntitySubtype(ents, "SCOPE.Pattern", "relation", "mongoid_embeds_many:comments") {
		t.Error("expected mongoid_embeds_many:comments relation entity")
	}
}

// ---------------------------------------------------------------------------
// Driver schema — Elasticsearch
// ---------------------------------------------------------------------------

func TestDriverSchema_ElasticsearchMappings(t *testing.T) {
	src := `
class Article < ActiveRecord::Base
  include Elasticsearch::Model
  include Elasticsearch::Model::Callbacks

  mappings dynamic: 'false' do
    indexes :title,   type: 'text', analyzer: 'english'
    indexes :body,    type: 'text'
    indexes :created_at, type: 'date'
    indexes :views,   type: 'integer'
  end
end
`
	ents := driverSchemaExtract(t, "app/models/article.rb", src)
	if !containsEntitySubtype(ents, "SCOPE.Schema", "table", "es_mappings") {
		t.Error("expected es_mappings SCOPE.Schema entity")
	}
	wants := []string{"es_index:title", "es_index:body", "es_index:created_at"}
	for _, w := range wants {
		if !containsEntitySubtype(ents, "SCOPE.Schema", "column", w) {
			t.Errorf("expected ES index entity %q", w)
		}
	}
}

// ---------------------------------------------------------------------------
// Driver schema — ROM-rb
// ---------------------------------------------------------------------------

func TestDriverSchema_ROMSchema(t *testing.T) {
	src := `
module Relations
  class Users < ROM::Relation[:sql]
    schema(:users, infer: false) do
      attribute :id,    Types::Integer
      attribute :name,  Types::String
      attribute :email, Types::String
    end
  end
end
`
	ents := driverSchemaExtract(t, "app/relations/users.rb", src)
	if !containsEntitySubtype(ents, "SCOPE.Schema", "table", "rom_schema:users") {
		t.Error("expected rom_schema:users SCOPE.Schema entity")
	}
	if !containsEntitySubtype(ents, "SCOPE.Schema", "column", "rom_attr:id") {
		t.Error("expected rom_attr:id column entity")
	}
	if !containsEntitySubtype(ents, "SCOPE.Schema", "column", "rom_attr:name") {
		t.Error("expected rom_attr:name column entity")
	}
}

// ---------------------------------------------------------------------------
// Driver schema — Sequel
// ---------------------------------------------------------------------------

func TestDriverSchema_SequelCreateTable(t *testing.T) {
	src := `
Sequel.migration do
  change do
    create_table :users do
      primary_key :id
      String :name, null: false
      String :email, null: false, unique: true
      Integer :age
      DateTime :created_at
    end
  end
end
`
	ents := driverSchemaExtract(t, "db/migrations/001_create_users.rb", src)
	if !containsEntitySubtype(ents, "SCOPE.Schema", "table", "sequel_table:users") {
		t.Error("expected sequel_table:users SCOPE.Schema entity")
	}
	if !containsEntitySubtype(ents, "SCOPE.Schema", "column", "sequel_col:name") {
		t.Error("expected sequel_col:name column entity")
	}
	if !containsEntitySubtype(ents, "SCOPE.Schema", "column", "sequel_col:email") {
		t.Error("expected sequel_col:email column entity")
	}
}

// ---------------------------------------------------------------------------
// Driver schema — no match
// ---------------------------------------------------------------------------

func TestDriverSchema_EmptyFile(t *testing.T) {
	ents := driverSchemaExtract(t, "app/models/empty.rb", "")
	if len(ents) != 0 {
		t.Errorf("expected 0 entities for empty file, got %d", len(ents))
	}
}

func TestDriverSchema_PlainModel(t *testing.T) {
	src := `class User < ApplicationRecord; end`
	ents := driverSchemaExtract(t, "app/models/user.rb", src)
	if len(ents) != 0 {
		t.Errorf("expected 0 entities for plain AR model, got %d", len(ents))
	}
}
