package ruby_test

// activerecord_test.go — tests for the custom_ruby_activerecord extractor.
// Part of #3282.

import (
	"context"
	"testing"

	extreg "github.com/cajasmota/archigraph/internal/extractor"

	_ "github.com/cajasmota/archigraph/internal/custom/ruby"
)

// ---------------------------------------------------------------------------
// Helpers local to this file
// ---------------------------------------------------------------------------

func arExtract(t *testing.T, path, src string) []entitySummary {
	t.Helper()
	e, ok := extreg.Get("custom_ruby_activerecord")
	if !ok {
		t.Fatal("custom_ruby_activerecord extractor not registered")
	}
	ents, err := e.Extract(context.Background(), extreg.FileInput{
		Path:     path,
		Language: "ruby",
		Content:  []byte(src),
	})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	var out []entitySummary
	for _, ent := range ents {
		out = append(out, entitySummary{Kind: ent.Kind, Subtype: ent.Subtype, Name: ent.Name})
	}
	return out
}

func containsEntitySubtype(ents []entitySummary, kind, subtype, name string) bool {
	for _, e := range ents {
		if e.Kind == kind && e.Subtype == subtype && e.Name == name {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// 1. ActiveRecord associations
// ---------------------------------------------------------------------------

func TestARAssociations_BasicMacros(t *testing.T) {
	src := `
class Article < ApplicationRecord
  belongs_to :user
  has_many :comments
  has_one :profile
  has_and_belongs_to_many :tags
end
`
	ents := arExtract(t, "app/models/article.rb", src)
	wants := []string{
		"belongs_to:user",
		"has_many:comments",
		"has_one:profile",
		"has_and_belongs_to_many:tags",
	}
	for _, w := range wants {
		if !containsEntitySubtype(ents, "SCOPE.Pattern", "relation", w) {
			t.Errorf("missing association entity %q", w)
		}
	}
}

func TestARAssociations_HasManyThrough(t *testing.T) {
	src := `
class User < ApplicationRecord
  has_many :taggings
  has_many :tags, through: :taggings
end
`
	ents := arExtract(t, "app/models/user.rb", src)
	if !containsEntitySubtype(ents, "SCOPE.Pattern", "relation", "has_many_through:tags") {
		t.Error("expected has_many_through:tags relation entity")
	}
}

func TestARAssociations_HasManyThroughRocket(t *testing.T) {
	src := `
class Assembly < ApplicationRecord
  has_many :manifests
  has_many :parts, :through => :manifests
end
`
	ents := arExtract(t, "app/models/assembly.rb", src)
	if !containsEntitySubtype(ents, "SCOPE.Pattern", "relation", "has_many_through:parts") {
		t.Error("expected has_many_through:parts relation entity (hash-rocket syntax)")
	}
}

func TestARAssociations_ForeignKey(t *testing.T) {
	src := `
class Order < ApplicationRecord
  belongs_to :customer, foreign_key: "customer_id"
  has_many :line_items, foreign_key: "order_id"
end
`
	ents := arExtract(t, "app/models/order.rb", src)
	fkWants := []string{"fk:customer", "fk:line_items"}
	for _, w := range fkWants {
		if !containsEntitySubtype(ents, "SCOPE.Pattern", "foreign_key", w) {
			t.Errorf("missing foreign_key entity %q", w)
		}
	}
}

func TestARAssociations_SkippedOnSchemaFile(t *testing.T) {
	// Associations inside db/schema.rb should NOT be extracted as relation entities.
	src := `
ActiveRecord::Schema[7.0].define(version: 2023_01_01_000001) do
  create_table "articles" do |t|
    t.string "title"
    t.integer "user_id"
  end
end
`
	ents := arExtract(t, "db/schema.rb", src)
	for _, e := range ents {
		if e.Subtype == "relation" {
			t.Errorf("unexpected relation entity in schema.rb: %+v", e)
		}
	}
}

// ---------------------------------------------------------------------------
// 2. schema.rb extraction
// ---------------------------------------------------------------------------

func TestARSchema_TableAndColumns(t *testing.T) {
	src := `
ActiveRecord::Schema[7.0].define(version: 2023_01_01_000001) do
  create_table "users", force: :cascade do |t|
    t.string "email", null: false
    t.string "name"
    t.integer "age"
    t.boolean "active", default: true
    t.datetime "created_at", precision: 6, null: false
  end
end
`
	ents := arExtract(t, "db/schema.rb", src)

	if !containsEntitySubtype(ents, "SCOPE.Schema", "table", "table:users") {
		t.Error("expected table:users SCOPE.Schema entity")
	}
	cols := []string{"users.email", "users.name", "users.age", "users.active", "users.created_at"}
	for _, c := range cols {
		if !containsEntitySubtype(ents, "SCOPE.Schema", "column", c) {
			t.Errorf("expected column entity %q", c)
		}
	}
}

func TestARSchema_MultipleTablesNoMix(t *testing.T) {
	src := `
ActiveRecord::Schema[7.0].define do
  create_table "posts" do |t|
    t.string "title"
  end

  create_table "comments" do |t|
    t.text "body"
    t.integer "post_id"
  end
end
`
	ents := arExtract(t, "db/schema.rb", src)
	if !containsEntitySubtype(ents, "SCOPE.Schema", "table", "table:posts") {
		t.Error("expected table:posts")
	}
	if !containsEntitySubtype(ents, "SCOPE.Schema", "table", "table:comments") {
		t.Error("expected table:comments")
	}
	// Columns should be scoped correctly.
	if !containsEntitySubtype(ents, "SCOPE.Schema", "column", "posts.title") {
		t.Error("expected posts.title column")
	}
	if !containsEntitySubtype(ents, "SCOPE.Schema", "column", "comments.body") {
		t.Error("expected comments.body column")
	}
}

func TestARSchema_NoEntitiesForNonSchemaPath(t *testing.T) {
	// A model file with t.string should not trigger schema extraction.
	src := `
class Foo < ApplicationRecord
  def bar
    t.string "irrelevant"
  end
end
`
	ents := arExtract(t, "app/models/foo.rb", src)
	for _, e := range ents {
		if e.Subtype == "table" || e.Subtype == "column" {
			t.Errorf("unexpected schema entity in model file: %+v", e)
		}
	}
}

// ---------------------------------------------------------------------------
// 3. Migration extraction
// ---------------------------------------------------------------------------

func TestARMigration_CreateTable(t *testing.T) {
	src := `
class CreateArticles < ActiveRecord::Migration[7.0]
  def change
    create_table :articles do |t|
      t.string :title, null: false
      t.text :body
      t.timestamps
    end
  end
end
`
	ents := arExtract(t, "db/migrate/20230101000001_create_articles.rb", src)

	if !containsEntitySubtype(ents, "SCOPE.Schema", "migration", "migration:CreateArticles") {
		t.Error("expected migration:CreateArticles SCOPE.Schema entity")
	}
	if !containsEntitySubtype(ents, "SCOPE.Schema", "migration", "create_table:articles") {
		t.Error("expected create_table:articles SCOPE.Schema entity")
	}
}

func TestARMigration_AddColumnAndIndex(t *testing.T) {
	src := `
class AddSlugToArticles < ActiveRecord::Migration[7.0]
  def change
    add_column :articles, :slug, :string
    add_index :articles, :slug, unique: true
  end
end
`
	ents := arExtract(t, "db/migrate/20230102000001_add_slug_to_articles.rb", src)

	if !containsEntitySubtype(ents, "SCOPE.Schema", "migration", "add_column:articles.slug") {
		t.Error("expected add_column:articles.slug migration entity")
	}
	// add_index entity name includes table + column.
	found := false
	for _, e := range ents {
		if e.Kind == "SCOPE.Schema" && e.Subtype == "migration" &&
			len(e.Name) > 15 && e.Name[:15] == "add_index:artic" {
			found = true
		}
	}
	if !found {
		t.Error("expected add_index:articles.* migration entity")
	}
}

func TestARMigration_AddReference(t *testing.T) {
	src := `
class AddUserToOrders < ActiveRecord::Migration[7.0]
  def change
    add_reference :orders, :user, null: false, foreign_key: true
  end
end
`
	ents := arExtract(t, "db/migrate/20230103000001_add_user_to_orders.rb", src)
	if !containsEntitySubtype(ents, "SCOPE.Pattern", "foreign_key", "add_reference:orders.user") {
		t.Error("expected add_reference:orders.user foreign_key entity")
	}
}

func TestARMigration_SkippedOutsideMigrateDir(t *testing.T) {
	// Migration-style content in a non-migration path should not produce migration entities.
	src := `
class CreateArticles < ActiveRecord::Migration[7.0]
  def change
    create_table :articles do |t|
      t.string :title
    end
  end
end
`
	ents := arExtract(t, "app/models/article.rb", src)
	for _, e := range ents {
		if e.Subtype == "migration" {
			t.Errorf("unexpected migration entity outside db/migrate: %+v", e)
		}
	}
}

// ---------------------------------------------------------------------------
// 4. ROM associations
// ---------------------------------------------------------------------------

func TestROMAssociation(t *testing.T) {
	src := `
module Relations
  class Users < ROM::Relation[:sql]
    schema(:users, infer: true) do
      associations do
        has_many :posts
        has_many :comments, through: :posts
      end
    end
  end
end
`
	ents := arExtract(t, "app/relations/users.rb", src)
	if !containsEntitySubtype(ents, "SCOPE.Pattern", "relation", "rom_assoc:has_many:posts") {
		t.Error("expected rom_assoc:has_many:posts relation entity")
	}
}

// ---------------------------------------------------------------------------
// 5. Sequel associations
// ---------------------------------------------------------------------------

func TestSequelAssociation(t *testing.T) {
	src := `
class Post < Sequel::Model
  many_to_one :user
  one_to_many :comments
end
`
	ents := arExtract(t, "app/models/post.rb", src)
	if !containsEntitySubtype(ents, "SCOPE.Pattern", "relation", "sequel_assoc:many_to_one:user") {
		t.Error("expected sequel_assoc:many_to_one:user relation entity")
	}
	if !containsEntitySubtype(ents, "SCOPE.Pattern", "relation", "sequel_assoc:one_to_many:comments") {
		t.Error("expected sequel_assoc:one_to_many:comments relation entity")
	}
}

// ---------------------------------------------------------------------------
// 6. DataMapper properties and associations
// ---------------------------------------------------------------------------

func TestDataMapperPropertyAndAssoc(t *testing.T) {
	src := `
class User
  include DataMapper::Resource

  property :id,    Serial
  property :name,  String
  property :email, String

  has n, :posts
  belongs_to :group
end
`
	ents := arExtract(t, "app/models/user.rb", src)
	if !containsEntitySubtype(ents, "SCOPE.Schema", "column", "dm_prop:id") {
		t.Error("expected dm_prop:id schema entity")
	}
	if !containsEntitySubtype(ents, "SCOPE.Schema", "column", "dm_prop:name") {
		t.Error("expected dm_prop:name schema entity")
	}
	// DataMapper associations use 'has n' or 'belongs_to'.
	found := false
	for _, e := range ents {
		if e.Kind == "SCOPE.Pattern" && e.Subtype == "relation" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected at least one DataMapper relation entity")
	}
}

// ---------------------------------------------------------------------------
// 7. ActiveRecord lazy loading recognition
// ---------------------------------------------------------------------------

func TestARLazyLoading_EagerLoadMarkers(t *testing.T) {
	src := `
class PostsController < ApplicationController
  def index
    @posts = Post.includes(:author, :comments).where(published: true)
    @orders = Order.preload(:customer).eager_load(:line_items)
  end
end
`
	ents := arExtract(t, "app/controllers/posts_controller.rb", src)

	if !containsEntitySubtype(ents, "SCOPE.Pattern", "lazy_marker", "ar_eager:includes:author") {
		t.Error("expected ar_eager:includes:author lazy_marker entity")
	}
	if !containsEntitySubtype(ents, "SCOPE.Pattern", "lazy_marker", "ar_eager:preload:customer") {
		t.Error("expected ar_eager:preload:customer lazy_marker entity")
	}
	if !containsEntitySubtype(ents, "SCOPE.Pattern", "lazy_marker", "ar_eager:eager_load:line_items") {
		t.Error("expected ar_eager:eager_load:line_items lazy_marker entity")
	}
}

func TestARLazyLoading_LazyAssociationMarkers(t *testing.T) {
	src := `
class Post < ApplicationRecord
  belongs_to :author
  has_many :comments
  has_one :featured_image
end
`
	ents := arExtract(t, "app/models/post.rb", src)
	if !containsEntitySubtype(ents, "SCOPE.Pattern", "lazy_marker", "ar_lazy:belongs_to:author") {
		t.Error("expected ar_lazy:belongs_to:author lazy_marker")
	}
	if !containsEntitySubtype(ents, "SCOPE.Pattern", "lazy_marker", "ar_lazy:has_many:comments") {
		t.Error("expected ar_lazy:has_many:comments lazy_marker")
	}
	if !containsEntitySubtype(ents, "SCOPE.Pattern", "lazy_marker", "ar_lazy:has_one:featured_image") {
		t.Error("expected ar_lazy:has_one:featured_image lazy_marker")
	}
}

// ---------------------------------------------------------------------------
// 8. Mongoid associations + lazy loading
// ---------------------------------------------------------------------------

func TestMongoidAssociations(t *testing.T) {
	src := `
class Article
  include Mongoid::Document

  belongs_to :user
  has_many :comments
  embeds_many :tags
  has_and_belongs_to_many :categories
end
`
	ents := arExtract(t, "app/models/article.rb", src)
	if !containsEntitySubtype(ents, "SCOPE.Pattern", "relation", "mongoid_assoc:belongs_to:user") {
		t.Error("expected mongoid_assoc:belongs_to:user relation entity")
	}
	if !containsEntitySubtype(ents, "SCOPE.Pattern", "relation", "mongoid_assoc:has_many:comments") {
		t.Error("expected mongoid_assoc:has_many:comments relation entity")
	}
	if !containsEntitySubtype(ents, "SCOPE.Pattern", "relation", "mongoid_assoc:embeds_many:tags") {
		t.Error("expected mongoid_assoc:embeds_many:tags relation entity")
	}
}

func TestMongoidLazyLoading(t *testing.T) {
	src := `
class Post
  include Mongoid::Document

  has_many :comments
  belongs_to :author
end
`
	ents := arExtract(t, "app/models/post.rb", src)
	if !containsEntitySubtype(ents, "SCOPE.Pattern", "lazy_marker", "mongoid_lazy:has_many:comments") {
		t.Error("expected mongoid_lazy:has_many:comments lazy_marker")
	}
	if !containsEntitySubtype(ents, "SCOPE.Pattern", "lazy_marker", "mongoid_lazy:belongs_to:author") {
		t.Error("expected mongoid_lazy:belongs_to:author lazy_marker")
	}
}

// ---------------------------------------------------------------------------
// 9. Sequel migrations
// ---------------------------------------------------------------------------

func TestSequelMigration(t *testing.T) {
	src := `
Sequel.migration do
  change do
    create_table :users do
      primary_key :id
      String :name, null: false
    end
  end
end
`
	ents := arExtract(t, "db/migrations/001_create_users.rb", src)
	if !containsEntitySubtype(ents, "SCOPE.Schema", "migration", "sequel_migration") {
		t.Error("expected sequel_migration schema entity")
	}
}

func TestSequelLazyLoading(t *testing.T) {
	src := `
class Post < Sequel::Model
  many_to_one :author
  one_to_many :comments
end
`
	ents := arExtract(t, "app/models/post.rb", src)
	if !containsEntitySubtype(ents, "SCOPE.Pattern", "lazy_marker", "sequel_lazy:many_to_one:author") {
		t.Error("expected sequel_lazy:many_to_one:author lazy_marker")
	}
	if !containsEntitySubtype(ents, "SCOPE.Pattern", "lazy_marker", "sequel_lazy:one_to_many:comments") {
		t.Error("expected sequel_lazy:one_to_many:comments lazy_marker")
	}
}

// ---------------------------------------------------------------------------
// 10. DataMapper migrations
// ---------------------------------------------------------------------------

func TestDataMapperMigration(t *testing.T) {
	src := `
migration(1, :create_users) do
  up do
    create_table :users do
      column :id,   Integer, serial: true
      column :name, String,  length: 255
    end
  end
  down do
    drop_table :users
  end
end
`
	ents := arExtract(t, "db/migrate/001_create_users.rb", src)
	if !containsEntitySubtype(ents, "SCOPE.Schema", "migration", "dm_migration") {
		t.Error("expected dm_migration schema entity")
	}
}

func TestDataMapperLazyLoading(t *testing.T) {
	src := `
class User
  include DataMapper::Resource

  has n, :posts
  belongs_to :group
end
`
	ents := arExtract(t, "app/models/user.rb", src)
	foundLazy := false
	for _, e := range ents {
		if e.Kind == "SCOPE.Pattern" && e.Subtype == "lazy_marker" {
			foundLazy = true
			break
		}
	}
	if !foundLazy {
		t.Error("expected at least one DataMapper lazy_marker entity")
	}
}

// ---------------------------------------------------------------------------
// 11. ROM-rb lazy loading + migration
// ---------------------------------------------------------------------------

func TestROMLazyLoading(t *testing.T) {
	src := `
module Relations
  class Users < ROM::Relation[:sql]
    schema(:users, infer: true) do
      associations do
        has_many :posts
        belongs_to :account
      end
    end
  end
end
`
	ents := arExtract(t, "app/relations/users.rb", src)
	if !containsEntitySubtype(ents, "SCOPE.Pattern", "lazy_marker", "rom_lazy:has_many:posts") {
		t.Error("expected rom_lazy:has_many:posts lazy_marker")
	}
	if !containsEntitySubtype(ents, "SCOPE.Pattern", "lazy_marker", "rom_lazy:belongs_to:account") {
		t.Error("expected rom_lazy:belongs_to:account lazy_marker")
	}
}

// ---------------------------------------------------------------------------
// 12. Empty / non-Ruby file → no entities
// ---------------------------------------------------------------------------

func TestARNoMatch_EmptyFile(t *testing.T) {
	ents := arExtract(t, "app/models/empty.rb", "")
	if len(ents) != 0 {
		t.Errorf("expected no entities for empty file, got %d", len(ents))
	}
}

func TestARNoMatch_PlainRuby(t *testing.T) {
	ents := arExtract(t, "lib/utils.rb", `def hello; "world"; end`)
	if len(ents) != 0 {
		t.Errorf("expected no entities for plain Ruby, got %d", len(ents))
	}
}
