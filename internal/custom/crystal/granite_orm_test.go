package crystal_test

import (
	"context"
	"testing"

	extreg "github.com/cajasmota/archigraph/internal/extractor"

	_ "github.com/cajasmota/archigraph/internal/custom/crystal"
)

// gfi builds a Granite-test FileInput (a distinct helper from extractors_test.go's
// fi to avoid a redeclaration in the same package).
func gfi(path, lang, src string) extreg.FileInput {
	return extreg.FileInput{Path: path, Language: lang, Content: []byte(src)}
}

// TestCrystalGraniteORM_ModelTableColumns proves a `class T < Granite::Base`
// model synthesises model + table + column + association SCOPE.Schema entities,
// honours an explicit `table` name, stamps the primary column, and emits a
// belongs_to FK edge.
func TestCrystalGraniteORM_ModelTableColumns(t *testing.T) {
	src := `
require "granite/adapter/pg"

class User < Granite::Base
  connection pg
  table users

  column id : Int64, primary: true
  column name : String
  column email : String

  has_many :posts
end

class Post < Granite::Base
  table posts

  column id : Int64, primary: true
  column title : String
  column body : String?

  belongs_to :user
end
`
	e, ok := extreg.Get("custom_crystal_granite_orm")
	if !ok {
		t.Fatal("custom_crystal_granite_orm not registered")
	}
	ents, err := e.Extract(context.Background(), gfi("src/models.cr", "crystal", src))
	if err != nil {
		t.Fatalf("extract: %v", err)
	}

	type key struct{ name, sub string }
	got := map[key]bool{}
	for _, en := range ents {
		if en.Kind != "SCOPE.Schema" {
			t.Errorf("unexpected kind %q for %q", en.Kind, en.Name)
			continue
		}
		if en.Properties["framework"] != "granite" {
			t.Errorf("entity %q missing framework=granite", en.Name)
		}
		got[key{en.Name, en.Subtype}] = true
	}

	// Models.
	for _, m := range []string{"User", "Post"} {
		if !got[key{m, "model"}] {
			t.Errorf("expected SCOPE.Schema/model %q", m)
		}
	}
	// Explicit table names (table users / table posts).
	for _, tbl := range []string{"users", "posts"} {
		if !got[key{tbl, "table"}] {
			t.Errorf("expected SCOPE.Schema/table %q (explicit table macro)", tbl)
		}
	}
	// Columns.
	for _, c := range []string{"id", "name", "email", "title", "body"} {
		if !got[key{c, "column"}] {
			t.Errorf("expected SCOPE.Schema/column %q", c)
		}
	}
	// Associations.
	if !got[key{"posts", "association"}] {
		t.Error("expected SCOPE.Schema/association posts (has_many)")
	}
	if !got[key{"user", "association"}] {
		t.Error("expected SCOPE.Schema/association user (belongs_to)")
	}

	// Primary-key column stamp + nilable type trim + FK edge.
	primaryStamped := false
	bodyTypeTrimmed := false
	fkFound := false
	assocKind := ""
	for _, en := range ents {
		if en.Name == "id" && en.Subtype == "column" && en.Properties["primary_key"] == "true" {
			primaryStamped = true
		}
		if en.Name == "body" && en.Subtype == "column" && en.Properties["column_type"] == "String" {
			bodyTypeTrimmed = true // `String?` nilable marker trimmed
		}
		if en.Name == "Post" && en.Subtype == "model" {
			for _, r := range en.Relationships {
				if r.Kind == "REFERENCES" && r.ToID == "User" && r.Properties["fk_field"] == "user" {
					fkFound = true
				}
			}
		}
		if en.Name == "user" && en.Subtype == "association" {
			assocKind = en.Properties["assoc_kind"]
		}
	}
	if !primaryStamped {
		t.Error("expected id column stamped primary_key=true")
	}
	if !bodyTypeTrimmed {
		t.Error("expected body column column_type=String (nilable `?` trimmed)")
	}
	if !fkFound {
		t.Error("expected REFERENCES edge Post→User (fk_field=user) from belongs_to :user")
	}
	if assocKind != "belongs_to" {
		t.Errorf("expected user association assoc_kind=belongs_to, got %q", assocKind)
	}
}

// TestCrystalGraniteORM_QueryAttribution proves Granite's class-method query DSL
// (`Model.all/find/where/create/delete`) emits QUERIES edges model → its table
// stamped with the canonical SQL operation, attributed only to known models.
func TestCrystalGraniteORM_QueryAttribution(t *testing.T) {
	src := `
class User < Granite::Base
  table users
  column id : Int64, primary: true
  column name : String
end

def handlers
  all_users = User.all
  u = User.find(1)
  found = User.find_by(name: "x")
  fresh = User.create(name: "y")
  User.where(name: "z").first
  User.clear
  Unknown.find(7)
end
`
	e, _ := extreg.Get("custom_crystal_granite_orm")
	ents, err := e.Extract(context.Background(), gfi("src/user.cr", "crystal", src))
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	ops := map[string]string{} // operation -> table, collected off the User model
	for _, en := range ents {
		if en.Name == "User" && en.Subtype == "model" {
			for _, r := range en.Relationships {
				if r.Kind == "QUERIES" {
					ops[r.Properties["operation"]] = r.Properties["table"]
				}
			}
		}
	}
	for _, want := range []string{"select", "insert", "delete"} {
		if ops[want] != "users" {
			t.Errorf("expected QUERIES edge op=%q table=users, got table=%q", want, ops[want])
		}
	}
	// `Unknown.find(7)` must NOT produce any edge (not a known model).
	for _, en := range ents {
		if en.Name == "Unknown" {
			t.Errorf("Unknown.find must not be attributed (not a model): got entity %q/%s", en.Name, en.Subtype)
		}
	}
}

// TestCrystalGraniteORM_Timestamps proves the `timestamps` macro synthesises the
// conventional created_at/updated_at Time columns stamped auto_timestamp=true.
func TestCrystalGraniteORM_Timestamps(t *testing.T) {
	src := `
class Article < Granite::Base
  table articles
  column id : Int64, primary: true
  column title : String
  timestamps
end
`
	e, _ := extreg.Get("custom_crystal_granite_orm")
	ents, err := e.Extract(context.Background(), gfi("src/article.cr", "crystal", src))
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	auto := map[string]bool{}
	for _, en := range ents {
		if en.Subtype == "column" && en.Properties["auto_timestamp"] == "true" {
			if en.Properties["column_type"] != "Time" {
				t.Errorf("timestamp column %q expected column_type=Time, got %q", en.Name, en.Properties["column_type"])
			}
			auto[en.Name] = true
		}
	}
	for _, n := range []string{"created_at", "updated_at"} {
		if !auto[n] {
			t.Errorf("expected auto-timestamp column %q from the timestamps macro", n)
		}
	}
}

// TestCrystalGraniteORM_Transaction proves a `db.transaction do … end` block
// emits a SCOPE.Pattern/transaction_boundary entity (transactional=true).
func TestCrystalGraniteORM_Transaction(t *testing.T) {
	src := `
class Account < Granite::Base
  table accounts
  column id : Int64, primary: true
  column balance : Int64
end

def transfer(db)
  db.transaction do
    Account.create(balance: 100)
  end
end
`
	e, _ := extreg.Get("custom_crystal_granite_orm")
	ents, err := e.Extract(context.Background(), gfi("src/account.cr", "crystal", src))
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	txFound := false
	for _, en := range ents {
		if en.Kind == "SCOPE.Pattern" && en.Subtype == "transaction_boundary" {
			if en.Properties["transactional"] != "true" {
				t.Errorf("transaction boundary missing transactional=true")
			}
			if en.Properties["framework"] != "granite" {
				t.Errorf("transaction boundary missing framework=granite")
			}
			if en.Properties["db_handle"] != "db" {
				t.Errorf("expected db_handle=db, got %q", en.Properties["db_handle"])
			}
			txFound = true
		}
	}
	if !txFound {
		t.Error("expected a SCOPE.Pattern/transaction_boundary for db.transaction do")
	}
}

// TestCrystalGraniteORM_ImplicitTableName proves a model without an explicit
// `table` macro keys the table by the class name.
func TestCrystalGraniteORM_ImplicitTableName(t *testing.T) {
	src := `
class Widget < Granite::Base
  column id : Int64, primary: true
  column label : String
end
`
	e, _ := extreg.Get("custom_crystal_granite_orm")
	ents, err := e.Extract(context.Background(), gfi("src/widget.cr", "crystal", src))
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	tableByName := false
	for _, en := range ents {
		if en.Subtype == "table" && en.Name == "Widget" {
			tableByName = true
		}
	}
	if !tableByName {
		t.Error("expected table keyed by class name Widget when no explicit table macro")
	}
}

// TestCrystalGraniteORM_NonModelNoop proves a plain (non-Granite) class is
// ignored.
func TestCrystalGraniteORM_NonModelNoop(t *testing.T) {
	src := `
class Config
  def initialize(@host : String, @port : Int32)
  end
end
`
	e, _ := extreg.Get("custom_crystal_granite_orm")
	ents, _ := e.Extract(context.Background(), gfi("src/config.cr", "crystal", src))
	if len(ents) != 0 {
		t.Fatalf("expected no schema entities for a non-Granite class, got %d", len(ents))
	}
}

// TestCrystalGraniteORM_AssociationOptions proves `has_many … through:`,
// polymorphic (`as:`/`polymorphic: true`), and an explicit `foreign_key:`/
// `class_name:` override are read onto the association entity + FK edge (#5032).
func TestCrystalGraniteORM_AssociationOptions(t *testing.T) {
	src := `
class Membership < Granite::Base
  table memberships
  column id : Int64, primary: true
end

class Team < Granite::Base
  table teams
  column id : Int64, primary: true

  has_many :memberships
  has_many :users, through: :memberships
  has_many :comments, as: :commentable
end

class Comment < Granite::Base
  table comments
  column id : Int64, primary: true

  belongs_to :commentable, polymorphic: true
  belongs_to :author, class_name: Account, foreign_key: author_id
end
`
	e, _ := extreg.Get("custom_crystal_granite_orm")
	ents, err := e.Extract(context.Background(), gfi("src/team.cr", "crystal", src))
	if err != nil {
		t.Fatalf("extract: %v", err)
	}

	// through: on has_many :users.
	through := ""
	usersTarget := ""
	polyAs := ""
	polyFlag := ""
	fkField := ""
	classNameTarget := ""
	for _, en := range ents {
		if en.Subtype != "association" {
			continue
		}
		switch en.Name {
		case "users":
			through = en.Properties["through"]
			usersTarget = en.Properties["target"]
		case "comments":
			polyAs = en.Properties["poly_as"]
		case "commentable":
			polyFlag = en.Properties["polymorphic"]
		case "author":
			fkField = en.Properties["foreign_key"]
			classNameTarget = en.Properties["target"]
		}
	}
	if through != "memberships" {
		t.Errorf("expected has_many :users through=memberships, got %q", through)
	}
	if usersTarget != "User" {
		t.Errorf("expected has_many :users target=User (singularised), got %q", usersTarget)
	}
	if polyAs != "commentable" {
		t.Errorf("expected has_many :comments poly_as=commentable, got %q", polyAs)
	}
	if polyFlag != "true" {
		t.Errorf("expected belongs_to :commentable polymorphic=true, got %q", polyFlag)
	}
	if fkField != "author_id" {
		t.Errorf("expected belongs_to :author foreign_key=author_id, got %q", fkField)
	}
	if classNameTarget != "Account" {
		t.Errorf("expected belongs_to :author class_name target Account, got %q", classNameTarget)
	}

	// FK edge for belongs_to :author honours the override.
	fkEdge := false
	for _, en := range ents {
		if en.Name == "Comment" && en.Subtype == "model" {
			for _, r := range en.Relationships {
				if r.Kind == "REFERENCES" && r.ToID == "Account" && r.Properties["fk_field"] == "author_id" {
					fkEdge = true
				}
			}
		}
	}
	if !fkEdge {
		t.Error("expected REFERENCES edge Comment→Account (fk_field=author_id) from the foreign_key/class_name override")
	}
}

// TestCrystalGraniteORM_ColumnOptions proves `default:`, `converter:`, and
// `unique: true` column options are stamped (#5032 schema_extraction).
func TestCrystalGraniteORM_ColumnOptions(t *testing.T) {
	src := `
class Setting < Granite::Base
  table settings
  column id : Int64, primary: true
  column status : String, default: "active"
  column data : JSON::Any, converter: Granite::Converters::Json(JSON::Any, String)
  column slug : String, unique: true
end
`
	e, _ := extreg.Get("custom_crystal_granite_orm")
	ents, _ := e.Extract(context.Background(), gfi("src/setting.cr", "crystal", src))
	gotDefault, gotConverter, gotUnique := "", "", ""
	for _, en := range ents {
		if en.Subtype != "column" {
			continue
		}
		switch en.Name {
		case "status":
			gotDefault = en.Properties["column_default"]
		case "data":
			gotConverter = en.Properties["converter"]
		case "slug":
			gotUnique = en.Properties["unique"]
		}
	}
	if gotDefault != `"active"` {
		t.Errorf("expected status column_default=\"active\", got %q", gotDefault)
	}
	if gotConverter == "" {
		t.Errorf("expected data column converter stamped, got empty")
	}
	if gotUnique != "true" {
		t.Errorf("expected slug column unique=true, got %q", gotUnique)
	}
}

// TestCrystalGraniteORM_Lifecycle proves lifecycle callbacks (before_save,
// after_create) and the validate macro emit SCOPE.Operation/function entities
// stamped callback_type + model (#5032 model_lifecycle_extraction).
func TestCrystalGraniteORM_Lifecycle(t *testing.T) {
	src := `
class User < Granite::Base
  table users
  column id : Int64, primary: true
  column email : String

  before_save :normalize_email
  after_create :send_welcome
  validate :email, "is required"
end
`
	e, _ := extreg.Get("custom_crystal_granite_orm")
	ents, _ := e.Extract(context.Background(), gfi("src/user.cr", "crystal", src))
	cbTypes := map[string]string{} // callback_type -> method
	for _, en := range ents {
		if en.Kind == "SCOPE.Operation" && en.Properties["framework"] == "granite" &&
			en.Properties["provenance"] == "INFERRED_FROM_GRANITE_CALLBACK" {
			if en.Properties["model"] != "User" {
				t.Errorf("callback %q expected model=User, got %q", en.Name, en.Properties["model"])
			}
			cbTypes[en.Properties["callback_type"]] = en.Properties["callback_method"]
		}
	}
	if cbTypes["before_save"] != "normalize_email" {
		t.Errorf("expected before_save :normalize_email, got %q", cbTypes["before_save"])
	}
	if cbTypes["after_create"] != "send_welcome" {
		t.Errorf("expected after_create :send_welcome, got %q", cbTypes["after_create"])
	}
	if _, ok := cbTypes["validate"]; !ok {
		t.Error("expected a validate lifecycle entity")
	}
}

// TestCrystalGraniteORM_Migrations proves `<Model>.migrator.create`/`.drop` and
// raw CREATE/DROP TABLE exec SQL emit SCOPE.Evolution migration-op entities
// (#5032 migration_parsing / migration_schema_ops).
func TestCrystalGraniteORM_Migrations(t *testing.T) {
	src := `
class User < Granite::Base
  table users
  column id : Int64, primary: true
end

def setup
  User.migrator.create
  User.migrator.drop
  Granite::Base.exec("CREATE TABLE audits (id BIGINT)")
  Granite::Base.exec("DROP TABLE IF EXISTS legacy")
  Unknown.migrator.create
end
`
	e, _ := extreg.Get("custom_crystal_granite_orm")
	ents, _ := e.Extract(context.Background(), gfi("src/migrate.cr", "crystal", src))
	ops := map[string]string{} // "op:table" -> migration_op
	for _, en := range ents {
		if en.Kind == "SCOPE.Evolution" && en.Properties["framework"] == "granite" {
			ops[en.Properties["migration_op"]+":"+en.Properties["table"]] = en.Subtype
		}
	}
	for _, want := range []string{"create_table:User", "drop_table:User", "create_table:audits", "drop_table:legacy"} {
		if _, ok := ops[want]; !ok {
			t.Errorf("expected SCOPE.Evolution migration op %q, got %v", want, ops)
		}
	}
	// Unknown.migrator.create is NOT a declared model → not attributed.
	if _, bad := ops["create_table:Unknown"]; bad {
		t.Error("Unknown.migrator.create must not be attributed (not a declared model)")
	}
}

// TestCrystalGraniteORM_MigrationNoMatchNoop proves a Granite model file with no
// migration/exec calls emits no SCOPE.Evolution entity (honest no-match no-op).
func TestCrystalGraniteORM_MigrationNoMatchNoop(t *testing.T) {
	src := `
class User < Granite::Base
  table users
  column id : Int64, primary: true
end
`
	e, _ := extreg.Get("custom_crystal_granite_orm")
	ents, _ := e.Extract(context.Background(), gfi("src/user.cr", "crystal", src))
	for _, en := range ents {
		if en.Kind == "SCOPE.Evolution" {
			t.Errorf("expected no migration entity for a model with no schema ops, got %q", en.Name)
		}
	}
}

// TestCrystalGraniteORM_WrongLanguageNoop gates on language=="crystal".
func TestCrystalGraniteORM_WrongLanguageNoop(t *testing.T) {
	src := `class User < Granite::Base
  column id : Int64, primary: true
end`
	e, _ := extreg.Get("custom_crystal_granite_orm")
	ents, _ := e.Extract(context.Background(), gfi("src/models.cr", "ruby", src))
	if len(ents) != 0 {
		t.Fatalf("expected no entities for non-crystal language, got %d", len(ents))
	}
}
