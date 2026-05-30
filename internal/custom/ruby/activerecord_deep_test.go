package ruby_test

// activerecord_deep_test.go — value-asserting tests for deep ActiveRecord
// extraction (TS/JS bar parity). These assert the *exact* columns, association
// type+target+options, foreign keys, and normalized migration ops emitted —
// not "≥1 entity".

import (
	"context"
	"testing"

	extreg "github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"

	_ "github.com/cajasmota/archigraph/internal/custom/ruby"
)

func arExtractRaw(t *testing.T, path, src string) []types.EntityRecord {
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
	return ents
}

// findEnt returns the entity with the given name (and optional subtype filter).
func findEnt(ents []types.EntityRecord, name string) *types.EntityRecord {
	for i := range ents {
		if ents[i].Name == name {
			return &ents[i]
		}
	}
	return nil
}

func mustProp(t *testing.T, e *types.EntityRecord, key, want string) {
	t.Helper()
	if e == nil {
		t.Fatalf("entity nil while checking prop %q", key)
	}
	if got := e.Properties[key]; got != want {
		t.Errorf("entity %q: prop %q = %q, want %q", e.Name, key, got, want)
	}
}

// ---------------------------------------------------------------------------
// Models / schema_extraction — exact columns from db/schema.rb, linked to model
// ---------------------------------------------------------------------------

func TestDeepSchema_ExactColumnsAndModelLink(t *testing.T) {
	src := `
ActiveRecord::Schema[7.1].define(version: 2024_01_01_000000) do
  create_table "users", force: :cascade do |t|
    t.string "email", null: false
    t.integer "age", default: 0
    t.string "name", limit: 100
    t.boolean "admin", default: false, null: false
    t.references "company", null: false
    t.timestamps
  end
end
`
	ents := arExtractRaw(t, "db/schema.rb", src)

	// Table → model link (User by convention).
	tbl := findEnt(ents, "ar_table:users")
	mustProp(t, tbl, "table_name", "users")
	mustProp(t, tbl, "model_class", "User")

	// Exact columns with types.
	email := findEnt(ents, "ar_col:users.email")
	mustProp(t, email, "column_type", "string")
	mustProp(t, email, "nullable", "false")
	mustProp(t, email, "model_class", "User")

	age := findEnt(ents, "ar_col:users.age")
	mustProp(t, age, "column_type", "integer")
	mustProp(t, age, "default", "0")

	name := findEnt(ents, "ar_col:users.name")
	mustProp(t, name, "column_type", "string")
	mustProp(t, name, "limit", "100")

	admin := findEnt(ents, "ar_col:users.admin")
	mustProp(t, admin, "column_type", "boolean")
	mustProp(t, admin, "default", "false")
	mustProp(t, admin, "nullable", "false")

	// t.references "company" → company_id bigint column + foreign key.
	companyCol := findEnt(ents, "ar_col:users.company_id")
	mustProp(t, companyCol, "column_type", "bigint")
	mustProp(t, companyCol, "is_reference", "true")

	companyFK := findEnt(ents, "ar_fk:users.company_id")
	if companyFK == nil {
		t.Fatal("expected FK ar_fk:users.company_id")
	}
	mustProp(t, companyFK, "target_model", "Company")
	mustProp(t, companyFK, "reference_name", "company")

	// t.timestamps → created_at / updated_at datetime columns.
	for _, c := range []string{"created_at", "updated_at"} {
		col := findEnt(ents, "ar_col:users."+c)
		mustProp(t, col, "column_type", "datetime")
	}
}

// Plural→singular irregulars used by classify().
func TestDeepSchema_IrregularModelLink(t *testing.T) {
	src := `
ActiveRecord::Schema[7.1].define(version: 1) do
  create_table "people" do |t|
    t.string "name"
  end
  create_table "categories" do |t|
    t.string "label"
  end
end
`
	ents := arExtractRaw(t, "db/schema.rb", src)
	mustProp(t, findEnt(ents, "ar_table:people"), "model_class", "Person")
	mustProp(t, findEnt(ents, "ar_table:categories"), "model_class", "Category")
}

// ---------------------------------------------------------------------------
// Relationships / association + relationship + foreign_key extraction
// ---------------------------------------------------------------------------

func TestDeepAssoc_AllMacrosWithOptions(t *testing.T) {
	src := `
class Article < ApplicationRecord
  belongs_to :user
  belongs_to :author, class_name: "User", foreign_key: "author_id"
  has_many :comments
  has_one :featured_image, class_name: "Image"
  has_many :taggings
  has_many :tags, through: :taggings, source: :tag
  has_and_belongs_to_many :categories
  belongs_to :commentable, polymorphic: true
end
`
	ents := arExtractRaw(t, "app/models/article.rb", src)

	// Model → table link.
	mustProp(t, findEnt(ents, "model:Article"), "table_name", "articles")

	// belongs_to :user → target User, convention FK user_id.
	user := findEnt(ents, "assoc:belongs_to:user")
	mustProp(t, user, "association_type", "belongs_to")
	mustProp(t, user, "target_model", "User")
	mustProp(t, user, "owner_model", "Article")
	userFK := findEnt(ents, "ar_fk:user:user_id")
	mustProp(t, userFK, "foreign_key", "user_id")
	mustProp(t, userFK, "convention", "true")

	// belongs_to :author with class_name + explicit foreign_key.
	author := findEnt(ents, "assoc:belongs_to:author")
	mustProp(t, author, "target_model", "User")
	mustProp(t, author, "class_name", "User")
	mustProp(t, author, "foreign_key", "author_id")
	authorFK := findEnt(ents, "ar_fk:author:author_id")
	mustProp(t, authorFK, "foreign_key", "author_id")

	// has_many :comments → target Comment.
	comments := findEnt(ents, "assoc:has_many:comments")
	mustProp(t, comments, "association_type", "has_many")
	mustProp(t, comments, "target_model", "Comment")

	// has_one :featured_image with class_name override.
	img := findEnt(ents, "assoc:has_one:featured_image")
	mustProp(t, img, "association_type", "has_one")
	mustProp(t, img, "target_model", "Image")

	// has_many :through with :source.
	tags := findEnt(ents, "assoc:has_many:tags")
	mustProp(t, tags, "through", "taggings")
	mustProp(t, tags, "source", "tag")
	mustProp(t, tags, "target_model", "Tag")

	// HABTM → singularized target.
	cats := findEnt(ents, "assoc:has_and_belongs_to_many:categories")
	mustProp(t, cats, "association_type", "has_and_belongs_to_many")
	mustProp(t, cats, "target_model", "Category")

	// Polymorphic belongs_to.
	poly := findEnt(ents, "assoc:belongs_to:commentable")
	mustProp(t, poly, "polymorphic", "true")
	polyFK := findEnt(ents, "ar_fk:commentable:commentable_id")
	mustProp(t, polyFK, "polymorphic", "true")
	mustProp(t, polyFK, "type_column", "commentable_type")
}

// ---------------------------------------------------------------------------
// Migrations / migration_parsing — normalized SCOPE.Evolution ops + columns
// ---------------------------------------------------------------------------

func TestDeepMigration_CreateTableColumnsAndOps(t *testing.T) {
	src := `
class CreateProducts < ActiveRecord::Migration[7.1]
  def change
    create_table :products do |t|
      t.string :name, null: false
      t.decimal :price
      t.references :category, foreign_key: true
      t.timestamps
    end
    add_column :products, :sku, :string, limit: 32
    add_index :products, :sku, unique: true
    add_reference :products, :supplier
    add_foreign_key :products, :suppliers
    change_column :products, :price, :float
    remove_column :products, :legacy_code
    drop_table :obsolete
  end
end
`
	ents := arExtractRaw(t, "db/migrate/20240101_create_products.rb", src)

	// Normalized SCOPE.Evolution ops.
	type opCheck struct{ name, subtype string }
	for _, oc := range []opCheck{
		{"ar_op:create_table:products", "create_table"},
		{"ar_op:add_column:products.sku", "add_column"},
		{"ar_op:create_index:products.sku", "create_index"},
		{"ar_op:add_reference:products.supplier", "add_reference"},
		{"ar_op:add_foreign_key:products->suppliers", "add_foreign_key"},
		{"ar_op:alter_column:products.price", "alter_column"},
		{"ar_op:drop_column:products.legacy_code", "drop_column"},
		{"ar_op:drop_table:obsolete", "drop_table"},
	} {
		e := findEnt(ents, oc.name)
		if e == nil {
			t.Errorf("missing migration op entity %q", oc.name)
			continue
		}
		if e.Kind != "SCOPE.Evolution" {
			t.Errorf("%q kind = %q, want SCOPE.Evolution", oc.name, e.Kind)
		}
		if e.Subtype != oc.subtype {
			t.Errorf("%q subtype = %q, want %q", oc.name, e.Subtype, oc.subtype)
		}
	}

	// Columns declared inside the create_table block, with exact types + opts.
	nameCol := findEnt(ents, "ar_migcol:products.name")
	mustProp(t, nameCol, "column_type", "string")
	mustProp(t, nameCol, "nullable", "false")
	priceCol := findEnt(ents, "ar_migcol:products.price")
	mustProp(t, priceCol, "column_type", "decimal")

	// add_column with limit option.
	skuCol := findEnt(ents, "ar_migcol:products.sku")
	mustProp(t, skuCol, "column_type", "string")
	mustProp(t, skuCol, "limit", "32")

	// t.references :category inside create_table → category_id + FK.
	catCol := findEnt(ents, "ar_col:products.category_id")
	mustProp(t, catCol, "column_type", "bigint")
	catFK := findEnt(ents, "ar_fk:products.category_id")
	mustProp(t, catFK, "target_model", "Category")

	// add_foreign_key :products, :suppliers → FK entity products→suppliers.
	fk := findEnt(ents, "ar_fk:products->suppliers")
	mustProp(t, fk, "from_table", "products")
	mustProp(t, fk, "to_table", "suppliers")
	mustProp(t, fk, "target_model", "Supplier")

	// t.timestamps inside create_table.
	for _, c := range []string{"created_at", "updated_at"} {
		col := findEnt(ents, "ar_migcol:products."+c)
		mustProp(t, col, "column_type", "datetime")
	}
}

// ---------------------------------------------------------------------------
// Lazy loading — both eager (includes/preload/eager_load) and lazy default
// ---------------------------------------------------------------------------

func TestDeepLazyLoading_EagerAndLazyBothRecognized(t *testing.T) {
	src := `
class PostsController
  def index
    @posts = Post.includes(:comments).preload(:author).eager_load(:tags)
  end
end
class Post < ApplicationRecord
  has_many :comments
  belongs_to :author
end
`
	ents := arExtractRaw(t, "app/models/post.rb", src)
	// Eager markers.
	for _, n := range []string{
		"ar_eager:includes:comments",
		"ar_eager:preload:author",
		"ar_eager:eager_load:tags",
	} {
		e := findEnt(ents, n)
		if e == nil {
			t.Errorf("missing eager marker %q", n)
			continue
		}
		mustProp(t, e, "loading_strategy", "eager")
	}
	// Lazy default markers.
	for _, n := range []string{"ar_lazy:has_many:comments", "ar_lazy:belongs_to:author"} {
		e := findEnt(ents, n)
		if e == nil {
			t.Errorf("missing lazy marker %q", n)
			continue
		}
		mustProp(t, e, "loading_strategy", "lazy")
	}
}
