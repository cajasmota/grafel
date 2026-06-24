package crystal_test

import (
	"context"
	"testing"

	extreg "github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"

	_ "github.com/cajasmota/grafel/internal/custom/crystal"
)

// qmfi builds a query/migration-test FileInput.
func qmfi(path, src string) extreg.FileInput {
	return extreg.FileInput{Path: path, Language: "crystal", Content: []byte(src)}
}

// modelQueryOps returns the set of canonical SQL ops carried on the named
// model's QUERIES edges (each edge should also carry table + framework).
func modelQueryOps(t *testing.T, ents []types.EntityRecord, model, table, framework string) map[string]bool {
	t.Helper()
	ops := map[string]bool{}
	for _, en := range ents {
		if en.Subtype != "model" || en.Name != model {
			continue
		}
		for _, r := range en.Relationships {
			if r.Kind != "QUERIES" {
				continue
			}
			if r.ToID != table {
				t.Errorf("%s QUERIES edge ToID=%q want %q", model, r.ToID, table)
			}
			if r.Properties["framework"] != framework {
				t.Errorf("%s QUERIES edge framework=%q want %q", model, r.Properties["framework"], framework)
			}
			ops[r.Properties["operation"]] = true
		}
	}
	return ops
}

// migrationOps returns the set of "op:table" SCOPE.Evolution migration entities.
func migrationOps(ents []types.EntityRecord, framework string) map[string]bool {
	out := map[string]bool{}
	for _, en := range ents {
		if en.Kind == "SCOPE.Evolution" && en.Properties["framework"] == framework {
			out[en.Subtype+":"+en.Properties["table"]] = true
		}
	}
	return out
}

// hasTransaction reports whether a transaction_boundary entity was emitted.
func hasTransaction(ents []types.EntityRecord, framework string) bool {
	for _, en := range ents {
		if en.Kind == "SCOPE.Pattern" && en.Subtype == "transaction_boundary" &&
			en.Properties["framework"] == framework && en.Properties["transactional"] == "true" {
			return true
		}
	}
	return false
}

// assertORMQMT asserts query attribution (select+insert at least), a
// create_table + drop_table migration op, and a transaction boundary.
func assertORMQMT(t *testing.T, ents []types.EntityRecord, framework, model, table string) {
	t.Helper()
	assertORMQMTOps(t, ents, framework, model, table, map[string]bool{"select": true, "insert": true})
}

func assertORMQMTOps(t *testing.T, ents []types.EntityRecord, framework, model, table string, wantOps map[string]bool) {
	t.Helper()
	ops := modelQueryOps(t, ents, model, table, framework)
	for op := range wantOps {
		if !ops[op] {
			t.Errorf("%s: expected QUERIES op %q on %s→%s, got ops=%v", framework, op, model, table, ops)
		}
	}
	migs := migrationOps(ents, framework)
	if !migs["create_table:"+table] {
		t.Errorf("%s: expected create_table:%s migration op, got %v", framework, table, migs)
	}
	if !hasTransaction(ents, framework) {
		t.Errorf("%s: expected a transaction_boundary entity", framework)
	}
}

// TestCrystalAvramORM_QueryMigrationTransaction proves the #5366 deepening:
// query attribution (QUERIES edge model→table per op), migration schema ops
// (SCOPE.Evolution create/drop/alter_table), and a transaction boundary.
func TestCrystalAvramORM_QueryMigrationTransaction(t *testing.T) {
	src := `
require "avram"
abstract class BaseModel < Avram::Model
end

class User < BaseModel
  table :users do
    primary_key id : Int64
    column name : String
  end
end

def run
  User.all
  User.create(name: "x")
  User.where(...).first
  db.transaction do
    User.update(...)
  end
end

class CreateUsers
  def migrate
    create_table :users do |t|
      t.add :name, :text
    end
  end

  def rollback
    drop_table :users
  end
end
`
	e, _ := extreg.Get("custom_crystal_avram_orm")
	ents, err := e.Extract(context.Background(), qmfi("src/models.cr", src))
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	assertORMQMT(t, ents, "avram", "User", "users")
	if !migrationOps(ents, "avram")["drop_table:users"] {
		t.Error("avram: expected drop_table:users migration op")
	}
}

// TestCrystalClearORM_QueryMigrationTransaction — same deepening for Clear.
func TestCrystalClearORM_QueryMigrationTransaction(t *testing.T) {
	src := `
require "clear"
class User
  include Clear::Model
  self.table = "users"
  column id : Int64, primary: true
  column name : String
end

def run
  User.query.all
  User.create(name: "x")
  Clear::SQL.transaction do
    User.where { ... }
  end
end

class Migration1
  def change
    create_table "users" do |t|
      t.string "name"
    end
    drop_table "old_users"
  end
end
`
	e, _ := extreg.Get("custom_crystal_clear_orm")
	ents, err := e.Extract(context.Background(), qmfi("src/models.cr", src))
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	assertORMQMT(t, ents, "clear", "User", "users")
}

// TestCrystalJenniferORM_QueryMigrationTransaction — same deepening for Jennifer.
func TestCrystalJenniferORM_QueryMigrationTransaction(t *testing.T) {
	src := `
require "jennifer"
class User < Jennifer::Model::Base
  table_name "users"
  mapping(
    id: Primary32,
    name: String,
  )
end

def run
  User.all
  User.create(name: "x")
  Jennifer::Adapter.adapter.transaction do
    User.where { ... }
  end
end

class CreateUsers
  def up
    create_table :users do |t|
      t.string :name
    end
  end

  def down
    drop_table :users
  end
end
`
	e, _ := extreg.Get("custom_crystal_jennifer_orm")
	ents, err := e.Extract(context.Background(), qmfi("src/models.cr", src))
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	assertORMQMT(t, ents, "jennifer", "User", "users")
}

// TestCrystalCrectoORM_QueryMigrationTransaction — same deepening for Crecto.
// Crecto routes reads through the Repo with the model class as the leading arg.
func TestCrystalCrectoORM_QueryMigrationTransaction(t *testing.T) {
	src := `
require "crecto"
class User
  include Crecto::Schema
  schema "users" do
    field :name, String
  end
end

def run
  Repo.all(User)
  Repo.get(User, 1)
  Repo.insert(changeset)
  AppRepo.transaction do
    Repo.delete_all(User)
  end
end

class CreateUsers
  def up
    create_table :users do |t|
      t.add_column :name, :string
    end
  end
end
`
	e, _ := extreg.Get("custom_crystal_crecto_orm")
	ents, err := e.Extract(context.Background(), qmfi("src/models.cr", src))
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	// Crecto attributes select (Repo.all/get) + delete (Repo.delete_all); insert
	// passes a lowercase changeset (not a model name) → honest skip.
	assertORMQMTOps(t, ents, "crecto", "User", "users", map[string]bool{"select": true, "delete": true})
}
