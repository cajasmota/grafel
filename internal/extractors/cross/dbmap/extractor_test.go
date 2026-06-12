package dbmap

import (
	"context"
	"strings"
	"testing"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func runExtract(t *testing.T, path, lang, source string) []types.EntityRecord {
	t.Helper()
	e := &Extractor{}
	recs, err := e.Extract(context.Background(), extractor.FileInput{
		Path:     path,
		Content:  []byte(source),
		Language: lang,
	})
	if err != nil {
		t.Fatalf("Extract error: %v", err)
	}
	return recs
}

func findByOpTable(t *testing.T, recs []types.EntityRecord, op, table string) types.EntityRecord {
	t.Helper()
	for _, r := range recs {
		if r.Kind != KindDataAccess {
			continue
		}
		if r.Properties["operation"] == op && r.Properties["table"] == table {
			return r
		}
	}
	t.Fatalf("no SCOPE.DataAccess found with operation=%s table=%s; got=%v", op, table, recs)
	return types.EntityRecord{}
}

func assertAccessesTableEdge(t *testing.T, rec types.EntityRecord) {
	t.Helper()
	if len(rec.Relationships) == 0 {
		t.Fatalf("entity has no relationships: %+v", rec)
	}
	for _, rel := range rec.Relationships {
		if rel.Kind == RelAccessesTable {
			return
		}
	}
	t.Fatalf("no ACCESSES_TABLE edge on entity: %+v", rec)
}

// ---------------------------------------------------------------------------
// Interface / registration
// ---------------------------------------------------------------------------

func TestLanguageKey(t *testing.T) {
	e := &Extractor{}
	if got := e.Language(); got != "_cross_dbmap" {
		t.Errorf("Language()=%q, want _cross_dbmap", got)
	}
}

func TestExtractEmptyFileSkipped(t *testing.T) {
	recs := runExtract(t, "empty.go", "go", "")
	if len(recs) != 0 {
		t.Errorf("expected no records for empty file, got %d", len(recs))
	}
}

func TestExtractNoORMImportSkipped(t *testing.T) {
	src := `package main

import "fmt"

func main() { fmt.Println("hello") }`
	recs := runExtract(t, "plain.go", "go", src)
	if len(recs) != 0 {
		t.Errorf("expected no records when no ORM import present, got %d", len(recs))
	}
}

// ---------------------------------------------------------------------------
// GORM
// ---------------------------------------------------------------------------

func TestGORMFindSelect(t *testing.T) {
	src := `package main

import "gorm.io/gorm"

func GetUser(db *gorm.DB) {
    var users []User
    db.Where("age > ?", 18).Find(&users)
}`
	recs := runExtract(t, "user.go", "go", src)
	r := findByOpTable(t, recs, OpSelect, "users")
	if r.Properties["orm"] != "gorm" {
		t.Errorf("orm=%q, want gorm", r.Properties["orm"])
	}
	if r.Properties["function_ref"] != "GetUser" {
		t.Errorf("function_ref=%q, want GetUser", r.Properties["function_ref"])
	}
	assertAccessesTableEdge(t, r)
}

func TestGORMCreateInsert(t *testing.T) {
	src := `package main
import "gorm.io/gorm"
func Create(db *gorm.DB) { db.Create(&Order{}) }`
	recs := runExtract(t, "o.go", "go", src)
	findByOpTable(t, recs, OpInsert, "orders")
}

func TestGORMDeleteDelete(t *testing.T) {
	src := `package main
import "gorm.io/gorm"
func Kill(db *gorm.DB) { db.Delete(&User{}) }`
	recs := runExtract(t, "k.go", "go", src)
	findByOpTable(t, recs, OpDelete, "users")
}

func TestGORMUpdatesUpdate(t *testing.T) {
	src := `package main
import "gorm.io/gorm"
func Touch(db *gorm.DB) { db.Model(&User{}).Updates(u) }`
	recs := runExtract(t, "t.go", "go", src)
	findByOpTable(t, recs, OpUpdate, "users")
}

func TestGORMSaveUpsert(t *testing.T) {
	src := `package main
import "gorm.io/gorm"
func Store(db *gorm.DB) { db.Save(&Order{}) }`
	recs := runExtract(t, "s.go", "go", src)
	findByOpTable(t, recs, OpUpsert, "orders")
}

func TestGORMTableOverride(t *testing.T) {
	src := `package main
import "gorm.io/gorm"
func Many(db *gorm.DB) { db.Table("people_t").Find(&users) }`
	recs := runExtract(t, "m.go", "go", src)
	findByOpTable(t, recs, OpSelect, "people_t")
}

func TestGORMModelNamePluralisation(t *testing.T) {
	cases := []struct {
		in, out string
	}{
		{"User", "users"},
		{"OrderItem", "order_items"},
		{"Person", "persons"},
	}
	for _, c := range cases {
		if got := modelNameToTable(c.in); got != c.out {
			t.Errorf("modelNameToTable(%q)=%q, want %q", c.in, got, c.out)
		}
	}
	if got := modelNameToTable(""); got != "" {
		t.Errorf("modelNameToTable(empty) want empty, got %q", got)
	}
}

// ---------------------------------------------------------------------------
// database/sql raw SQL
// ---------------------------------------------------------------------------

func TestDatabaseSQLRaw(t *testing.T) {
	src := `package main

import "database/sql"

func GetRows(db *sql.DB) {
    db.Query("SELECT * FROM customers WHERE id = $1")
    db.Exec("INSERT INTO orders (id) VALUES ($1)")
    db.Exec("UPDATE users SET name = 'x' WHERE id = 1")
    db.Exec("DELETE FROM sessions")
    db.Exec("TRUNCATE TABLE audit_log")
}`
	recs := runExtract(t, "raw.go", "go", src)

	findByOpTable(t, recs, OpSelect, "customers")
	findByOpTable(t, recs, OpInsert, "orders")
	findByOpTable(t, recs, OpUpdate, "users")
	findByOpTable(t, recs, OpDelete, "sessions")
	findByOpTable(t, recs, OpTruncate, "audit_log")

	for _, r := range recs {
		if r.Properties["orm"] != "database_sql" {
			t.Errorf("orm=%q, want database_sql for %q", r.Properties["orm"], r.Name)
		}
	}
}

func TestDatabaseSQLJoinMultipleTables(t *testing.T) {
	src := `package main
import "database/sql"
func X(db *sql.DB) { db.Query("SELECT * FROM orders JOIN customers ON orders.cid = customers.id") }`
	recs := runExtract(t, "join.go", "go", src)
	findByOpTable(t, recs, OpSelect, "orders")
	findByOpTable(t, recs, OpSelect, "customers")
}

// ---------------------------------------------------------------------------
// F# data drivers (#5000) — Npgsql.FSharp + Dapper raw-SQL table attribution
// ---------------------------------------------------------------------------

func TestNpgsqlFSharpRawSQLTables(t *testing.T) {
	src := "module Db\n" +
		"open Npgsql.FSharp\n" +
		"\n" +
		"let getUsers connStr =\n" +
		"    connStr\n" +
		"    |> Sql.connect\n" +
		"    |> Sql.query \"SELECT id, name FROM users WHERE active = true\"\n" +
		"    |> Sql.execute (fun read -> read.int \"id\")\n" +
		"\n" +
		"let addOrder connStr =\n" +
		"    connStr\n" +
		"    |> Sql.connect\n" +
		"    |> Sql.query \"INSERT INTO orders (id, total) VALUES (@id, @total)\"\n" +
		"    |> Sql.executeNonQuery\n" +
		"\n" +
		"let touch connStr =\n" +
		"    connStr |> Sql.connect |> Sql.query \"UPDATE accounts SET balance = 0\" |> Sql.executeNonQuery\n" +
		"\n" +
		"let purge connStr =\n" +
		"    connStr |> Sql.connect |> Sql.query \"DELETE FROM sessions\" |> Sql.executeNonQuery\n"
	recs := runExtract(t, "Db.fs", "fsharp", src)

	for _, tc := range []struct {
		op, table string
	}{
		{OpSelect, "users"},
		{OpInsert, "orders"},
		{OpUpdate, "accounts"},
		{OpDelete, "sessions"},
	} {
		rec := findByOpTable(t, recs, tc.op, tc.table)
		assertAccessesTableEdge(t, rec)
		if rec.Properties["orm"] != "npgsql_fsharp" {
			t.Errorf("orm=%q, want npgsql_fsharp for %q", rec.Properties["orm"], rec.Name)
		}
	}
}

func TestNpgsqlFSharpTripleQuotedSQL(t *testing.T) {
	src := "module Db\n" +
		"open Npgsql.FSharp\n" +
		"let report connStr =\n" +
		"    connStr\n" +
		"    |> Sql.connect\n" +
		"    |> Sql.query \"\"\"SELECT * FROM invoices JOIN customers ON invoices.cid = customers.id\"\"\"\n" +
		"    |> Sql.execute id\n"
	recs := runExtract(t, "Report.fs", "fsharp", src)
	a := findByOpTable(t, recs, OpSelect, "invoices")
	assertAccessesTableEdge(t, a)
	b := findByOpTable(t, recs, OpSelect, "customers")
	assertAccessesTableEdge(t, b)
}

func TestDapperFSharpRawSQLTables(t *testing.T) {
	src := "module Repo\n" +
		"open Dapper\n" +
		"open System.Data\n" +
		"let getProducts (conn: IDbConnection) =\n" +
		"    conn.Query<Product>(\"SELECT id, name FROM products\")\n" +
		"let removeStale (conn: IDbConnection) =\n" +
		"    conn.Execute(\"DELETE FROM stale_jobs\") |> ignore\n"
	recs := runExtract(t, "Repo.fs", "fsharp", src)

	sel := findByOpTable(t, recs, OpSelect, "products")
	assertAccessesTableEdge(t, sel)
	if sel.Properties["orm"] != "dapper_fsharp" {
		t.Errorf("orm=%q, want dapper_fsharp", sel.Properties["orm"])
	}
	del := findByOpTable(t, recs, OpDelete, "stale_jobs")
	assertAccessesTableEdge(t, del)
}

func TestNoFSharpDBImportSkipped(t *testing.T) {
	// A plain SQL-looking string with no F# data-driver import must not
	// produce SCOPE.DataAccess entities (import-gated, Rule #1).
	src := "module Pure\n" +
		"let q = \"SELECT * FROM nope\"\n"
	recs := runExtract(t, "Pure.fs", "fsharp", src)
	for _, r := range recs {
		if r.Kind == KindDataAccess {
			t.Errorf("unexpected data-access entity without DB import: %+v", r)
		}
	}
}

// ---------------------------------------------------------------------------
// SQLAlchemy
// ---------------------------------------------------------------------------

func TestSQLAlchemyQuery(t *testing.T) {
	src := `from sqlalchemy import select
class User(Base):
    __tablename__ = "users"
def list_users(session):
    return session.query(User).all()`
	recs := runExtract(t, "u.py", "python", src)
	r := findByOpTable(t, recs, OpSelect, "users")
	assertAccessesTableEdge(t, r)
}

func TestSQLAlchemyAddInsert(t *testing.T) {
	src := `from sqlalchemy.orm import Session
class Order(Base):
    __tablename__ = "orders"
def create(session): session.add(Order())`
	recs := runExtract(t, "o.py", "python", src)
	findByOpTable(t, recs, OpInsert, "orders")
}

func TestSQLAlchemyDelete(t *testing.T) {
	src := `from sqlalchemy import delete
class Audit(Base): __tablename__ = "audits"
def prune(session): session.delete(Audit)`
	recs := runExtract(t, "a.py", "python", src)
	findByOpTable(t, recs, OpDelete, "audits")
}

func TestSQLAlchemyMergeUpsert(t *testing.T) {
	src := `from sqlalchemy.orm import Session
class Item(Base): __tablename__ = "items"
def put(session): session.merge(Item)`
	recs := runExtract(t, "i.py", "python", src)
	findByOpTable(t, recs, OpUpsert, "items")
}

// ---------------------------------------------------------------------------
// psycopg2
// ---------------------------------------------------------------------------

func TestPsycopg2Raw(t *testing.T) {
	src := `import psycopg2
def fetch():
    conn = psycopg2.connect("")
    cur = conn.cursor()
    cur.execute("SELECT * FROM metrics WHERE ts > %s", (t,))`
	recs := runExtract(t, "m.py", "python", src)
	r := findByOpTable(t, recs, OpSelect, "metrics")
	if r.Properties["orm"] != "psycopg2" {
		t.Errorf("orm=%q, want psycopg2", r.Properties["orm"])
	}
}

// TestDataAccess_QualifiedNameMatchesStub guards issue #507. The
// SCOPE.DataAccess entity must set QualifiedName = the stub form so the
// resolver's byQualifiedName index can rewrite the ACCESSES_TABLE edge
// toID (which is the same stub) to this entity's hex ID. Without this
// the edge falls through to bug-extractor even though the entity exists.
func TestDataAccess_QualifiedNameMatchesStub(t *testing.T) {
	src := `import psycopg2
def fetch():
    conn = psycopg2.connect("")
    cur = conn.cursor()
    cur.execute("SELECT * FROM metrics WHERE ts > %s", (t,))`
	recs := runExtract(t, "m.py", "python", src)
	r := findByOpTable(t, recs, OpSelect, "metrics")
	wantStub := dataAccessEntityID("m.py", "psycopg2", OpSelect, "metrics")
	if r.QualifiedName != wantStub {
		t.Errorf("QualifiedName=%q, want %q (entity must match its ACCESSES_TABLE edge stub so byQualifiedName resolves it — issue #507)", r.QualifiedName, wantStub)
	}
	// Sanity: the relationship the extractor emits MUST point at the
	// same stub the entity now indexes under.
	if len(r.Relationships) != 1 {
		t.Fatalf("expected 1 ACCESSES_TABLE rel, got %d", len(r.Relationships))
	}
	if r.Relationships[0].ToID != wantStub {
		t.Errorf("ACCESSES_TABLE ToID=%q, want %q", r.Relationships[0].ToID, wantStub)
	}
}

// ---------------------------------------------------------------------------
// Hibernate / JPA
// ---------------------------------------------------------------------------

func TestHibernateJPQLFromEntity(t *testing.T) {
	src := `import javax.persistence.Entity;
import javax.persistence.Table;

@Entity
@Table(name="users")
public class User { }

public class UserDao {
    public List<User> all() {
        return em.createQuery("FROM User u WHERE u.age > 18").getResultList();
    }
}`
	recs := runExtract(t, "Dao.java", "java", src)
	findByOpTable(t, recs, OpSelect, "users")
}

func TestHibernateNativeQueryInsert(t *testing.T) {
	src := `import javax.persistence.EntityManager;
public class OrderDao {
    public void create() {
        em.createNativeQuery("INSERT INTO orders (id, total) VALUES (?, ?)").executeUpdate();
    }
}`
	recs := runExtract(t, "OrderDao.java", "java", src)
	findByOpTable(t, recs, OpInsert, "orders")
}

// ---------------------------------------------------------------------------
// ActiveRecord
// ---------------------------------------------------------------------------

func TestActiveRecordFind(t *testing.T) {
	src := `require 'activerecord'
class User < ApplicationRecord
end
def show(id)
  User.find(id)
end`
	recs := runExtract(t, "u.rb", "ruby", src)
	r := findByOpTable(t, recs, OpSelect, "users")
	if r.Properties["orm"] != "activerecord" {
		t.Errorf("orm=%q, want activerecord", r.Properties["orm"])
	}
}

func TestActiveRecordCreateDestroy(t *testing.T) {
	src := `require 'activerecord'
class Post < ApplicationRecord
end
def build() Post.create(title: 't') end
def kill(p) Post.destroy(p) end`
	recs := runExtract(t, "p.rb", "ruby", src)
	findByOpTable(t, recs, OpInsert, "posts")
	findByOpTable(t, recs, OpDelete, "posts")
}

// ---------------------------------------------------------------------------
// Ecto
// ---------------------------------------------------------------------------

func TestEctoRepoAllSelect(t *testing.T) {
	src := `defmodule MyApp.User do
  use Ecto.Schema
  schema "users" do
    field :name, :string
  end
end
def list_users() do
  Repo.all(User)
end`
	recs := runExtract(t, "u.ex", "elixir", src)
	findByOpTable(t, recs, OpSelect, "users")
}

func TestEctoInsert(t *testing.T) {
	src := `use Ecto.Schema
schema "orders" do end
def place() do Repo.insert(%Order{}) end`
	recs := runExtract(t, "o.ex", "elixir", src)
	findByOpTable(t, recs, OpInsert, "orders")
}

// ---------------------------------------------------------------------------
// Prisma
// ---------------------------------------------------------------------------

func TestPrismaFindMany(t *testing.T) {
	src := `import { PrismaClient } from '@prisma/client'
const prisma = new PrismaClient()
export async function listUsers() {
  return prisma.user.findMany()
}`
	recs := runExtract(t, "list.ts", "typescript", src)
	findByOpTable(t, recs, OpSelect, "user")
}

func TestPrismaUpsert(t *testing.T) {
	src := `import { PrismaClient } from '@prisma/client'
async function x() { prisma.order.upsert({}) }`
	recs := runExtract(t, "o.ts", "typescript", src)
	findByOpTable(t, recs, OpUpsert, "order")
}

// ---------------------------------------------------------------------------
// TypeORM
// ---------------------------------------------------------------------------

func TestTypeORMEntityFind(t *testing.T) {
	src := `import { Entity, Column } from 'typeorm'
@Entity({ name: 'users' })
export class User { }
async function all() { return getRepository(User).find() }`
	recs := runExtract(t, "u.ts", "typescript", src)
	findByOpTable(t, recs, OpSelect, "users")
}

func TestTypeORMSaveUpsert(t *testing.T) {
	src := `import { Entity } from 'typeorm'
@Entity("orders")
export class Order { }
async function put() { getRepository(Order).save(o) }`
	recs := runExtract(t, "o.ts", "typescript", src)
	findByOpTable(t, recs, OpUpsert, "orders")
}

// ---------------------------------------------------------------------------
// Sequelize
// ---------------------------------------------------------------------------

func TestSequelizeFindAll(t *testing.T) {
	src := `const Sequelize = require('sequelize')
const sequelize = new Sequelize()
const User = sequelize.define("users", {})
async function all() { return User.findAll() }`
	recs := runExtract(t, "s.js", "javascript", src)
	findByOpTable(t, recs, OpSelect, "user")
}

func TestSequelizeCreateInsert(t *testing.T) {
	src := `const { Sequelize } = require('sequelize')
const Order = sequelize.define("orders", {})
async function place() { return Order.create({}) }`
	recs := runExtract(t, "o.js", "javascript", src)
	findByOpTable(t, recs, OpInsert, "order")
}

// ---------------------------------------------------------------------------
// Diesel
// ---------------------------------------------------------------------------

func TestDieselLoad(t *testing.T) {
	src := `use diesel::prelude::*;
table! {
    users (id) { id -> Integer, }
}
fn all(conn: &PgConnection) {
    users::table.load::<User>(conn).unwrap();
}`
	recs := runExtract(t, "u.rs", "rust", src)
	findByOpTable(t, recs, OpSelect, "users")
}

// ---------------------------------------------------------------------------
// Raw SQL with UNKNOWN table
// ---------------------------------------------------------------------------

func TestRawSQLUnknownTable(t *testing.T) {
	src := `package main
import "database/sql"
func X(db *sql.DB, where string) {
    db.Query("SELECT * " + where)
}`
	recs := runExtract(t, "dyn.go", "go", src)
	// The concatenated literal is "SELECT * " — starts with SELECT but no FROM.
	findByOpTable(t, recs, OpSelect, UnknownTable)
}

// ---------------------------------------------------------------------------
// Query pattern sanitisation
// ---------------------------------------------------------------------------

func TestSanitisePatternLiterals(t *testing.T) {
	in := "SELECT * FROM users WHERE id = 42 AND name = 'Bob'"
	got := sanitisePattern(in)
	if !strings.Contains(got, "?") {
		t.Errorf("expected ? placeholders, got %q", got)
	}
	if strings.Contains(got, "'Bob'") || strings.Contains(got, "42") {
		t.Errorf("literals not replaced: %q", got)
	}
}

func TestSanitisePatternEmpty(t *testing.T) {
	if got := sanitisePattern(""); got != "" {
		t.Errorf("sanitisePattern(empty)=%q, want empty", got)
	}
}

func TestSanitisePatternTruncates(t *testing.T) {
	in := strings.Repeat("x", queryPatternMax+50)
	got := sanitisePattern(in)
	if len(got) > queryPatternMax {
		t.Errorf("sanitised length=%d, want <= %d", len(got), queryPatternMax)
	}
}

// ---------------------------------------------------------------------------
// sqlOperationOf coverage (all enum values + unknown)
// ---------------------------------------------------------------------------

func TestSQLOperationOf(t *testing.T) {
	cases := map[string]string{
		"SELECT * FROM x":          OpSelect,
		"WITH cte AS (SELECT 1)":   OpSelect,
		"INSERT INTO x VALUES (1)": OpInsert,
		"UPDATE x SET a = 1":       OpUpdate,
		"DELETE FROM x":            OpDelete,
		"TRUNCATE TABLE x":         OpTruncate,
		"UPSERT INTO x VALUES (1)": OpUpsert,
		"EXPLAIN SELECT * FROM x":  "",
	}
	for in, want := range cases {
		if got := sqlOperationOf(in); got != want {
			t.Errorf("sqlOperationOf(%q)=%q, want %q", in, got, want)
		}
	}
}

// ---------------------------------------------------------------------------
// Enclosing function discovery
// ---------------------------------------------------------------------------

func TestEnclosingFuncEmptyWhenNone(t *testing.T) {
	src := `package main
db.Query("SELECT * FROM users")`
	if got := enclosingFunc(src, len(src)); got != "" {
		t.Errorf("enclosingFunc=%q, want empty", got)
	}
}

func TestEnclosingFuncGoFunc(t *testing.T) {
	src := `func GetByID(id int) { db.Query("SELECT 1") }`
	if got := enclosingFunc(src, len(src)-10); got != "GetByID" {
		t.Errorf("enclosingFunc=%q, want GetByID", got)
	}
}

// ---------------------------------------------------------------------------
// Builder invariants
// ---------------------------------------------------------------------------

func TestBuildEntityCarriesProvenanceAndQuality(t *testing.T) {
	rec := buildEntity("x.go", "go", access{
		table: "users", operation: OpSelect, orm: "gorm", functionQName: "Fn",
	})
	if rec.Kind != KindDataAccess {
		t.Errorf("kind=%q", rec.Kind)
	}
	if rec.QualityScore < 0 || rec.QualityScore > 1 {
		t.Errorf("quality_score=%.3f out of range", rec.QualityScore)
	}
	if rec.Properties["provenance"] != "INFERRED_FROM_DB_ACCESS" {
		t.Errorf("provenance=%q", rec.Properties["provenance"])
	}
	if err := rec.Validate(); err != nil {
		t.Errorf("validate: %v", err)
	}
}

func TestAccessesTableEdgeFallbackFunctionRef(t *testing.T) {
	rec := buildEntity("x.go", "go", access{
		table: "users", operation: OpSelect, orm: "raw", functionQName: "",
	})
	found := false
	for _, rel := range rec.Relationships {
		if rel.Kind == RelAccessesTable {
			if !strings.HasSuffix(rel.FromID, "#_file_scope") {
				t.Errorf("expected _file_scope fallback ref, got %q", rel.FromID)
			}
			found = true
		}
	}
	if !found {
		t.Fatalf("no ACCESSES_TABLE edge")
	}
}

// ---------------------------------------------------------------------------
// Import token extraction
// ---------------------------------------------------------------------------

func TestExtractImportTokensGo(t *testing.T) {
	tokens := extractImportTokens(`import "gorm.io/gorm"`)
	if !tokens["gorm.io/gorm"] {
		t.Errorf("missing gorm.io/gorm in %v", tokens)
	}
}

func TestExtractImportTokensRequire(t *testing.T) {
	tokens := extractImportTokens(`const x = require('sequelize')`)
	if !tokens["sequelize"] {
		t.Errorf("missing sequelize in %v", tokens)
	}
}

func TestMatchesAnyImportSubstring(t *testing.T) {
	tokens := map[string]bool{"gorm.io/gorm": true}
	if !matchesAnyImport(tokens, []string{"gorm"}) {
		t.Errorf("expected gorm to match gorm.io/gorm")
	}
	if matchesAnyImport(tokens, []string{"typeorm"}) {
		t.Errorf("typeorm should not match gorm.io/gorm")
	}
}

// ---------------------------------------------------------------------------
// End-to-end acceptance criterion (Go + GORM db.Where(...).Find(&users))
// ---------------------------------------------------------------------------

func TestAcceptanceGORMFindUsers(t *testing.T) {
	src := `package main
import "gorm.io/gorm"

func ListAdults(db *gorm.DB) {
    var users []User
    db.Where("age > ?", 18).Find(&users)
}`
	recs := runExtract(t, "list.go", "go", src)
	r := findByOpTable(t, recs, OpSelect, "users")
	if r.Properties["orm"] != "gorm" {
		t.Errorf("orm=%q, want gorm", r.Properties["orm"])
	}
	assertAccessesTableEdge(t, r)
	// Edge must reference the enclosing function ListAdults.
	var edge types.RelationshipRecord
	for _, rel := range r.Relationships {
		if rel.Kind == RelAccessesTable {
			edge = rel
		}
	}
	if edge.Properties["function_qname"] != "ListAdults" {
		t.Errorf("edge function_qname=%q, want ListAdults", edge.Properties["function_qname"])
	}
}

// ---------------------------------------------------------------------------
// Raw-SQL table topology for high-traffic drivers (#3644)
// Python DB-API (sqlite3/pymysql/...), Go raw driver, plain JDBC.
// ---------------------------------------------------------------------------

// findEdge returns the ACCESSES_TABLE edge on rec (fatals if absent).
func findEdge(t *testing.T, rec types.EntityRecord) types.RelationshipRecord {
	t.Helper()
	for _, rel := range rec.Relationships {
		if rel.Kind == RelAccessesTable {
			return rel
		}
	}
	t.Fatalf("no ACCESSES_TABLE edge on entity: %+v", rec)
	return types.RelationshipRecord{}
}

// --- Python stdlib sqlite3: cursor.execute("SELECT id FROM users WHERE x=1")
func TestPyDBAPISqlite3SelectReadsUsers(t *testing.T) {
	src := `import sqlite3
def load(db):
    cur = db.cursor()
    cur.execute("SELECT id FROM users WHERE x=1")`
	recs := runExtract(t, "load.py", "python", src)
	r := findByOpTable(t, recs, OpSelect, "users")
	if r.Properties["orm"] != "dbapi" {
		t.Errorf("orm=%q, want dbapi", r.Properties["orm"])
	}
	edge := findEdge(t, r)
	if edge.Properties["table"] != "users" {
		t.Errorf("edge table=%q, want users", edge.Properties["table"])
	}
	if edge.Properties["operation"] != OpSelect {
		t.Errorf("edge operation=%q, want %s", edge.Properties["operation"], OpSelect)
	}
	if edge.Properties["function_qname"] != "load" {
		t.Errorf("edge function_qname=%q, want load", edge.Properties["function_qname"])
	}
}

// --- Python pymysql write: INSERT INTO orders -> table orders (write/INSERT)
func TestPyDBAPIPymysqlInsertWritesOrders(t *testing.T) {
	src := `import pymysql
def add(conn):
    cur = conn.cursor()
    cur.execute("INSERT INTO orders (id) VALUES (1)")`
	recs := runExtract(t, "add.py", "python", src)
	r := findByOpTable(t, recs, OpInsert, "orders")
	edge := findEdge(t, r)
	if edge.Properties["table"] != "orders" || edge.Properties["operation"] != OpInsert {
		t.Errorf("edge=%v, want table=orders op=INSERT", edge.Properties)
	}
}

// --- Negative: dynamic/concatenated SQL must not fabricate a table.
func TestPyDBAPIDynamicSQLNoFabricatedTable(t *testing.T) {
	src := `import sqlite3
def run(db, q):
    db.cursor().execute(q)`
	recs := runExtract(t, "dyn.py", "python", src)
	for _, r := range recs {
		if r.Kind == KindDataAccess && r.Properties["table"] != UnknownTable && r.Properties["table"] != "" {
			t.Errorf("fabricated table %q from dynamic SQL", r.Properties["table"])
		}
	}
}

// --- Negative: a Python file with no recognised driver import emits nothing.
func TestPyDBAPINoDriverImportSkipped(t *testing.T) {
	src := `def run(cur):
    cur.execute("SELECT id FROM users")`
	recs := runExtract(t, "nodriver.py", "python", src)
	if len(recs) != 0 {
		t.Errorf("expected no records without a driver import, got %d: %+v", len(recs), recs)
	}
}

// --- Go raw driver imported directly (lib/pq), no "database/sql" token.
func TestGoSQLDriverLibPQSelect(t *testing.T) {
	src := `package main
import _ "github.com/lib/pq"
func Fetch(db *DB) {
    db.Query("SELECT name FROM accounts WHERE id = $1")
}`
	recs := runExtract(t, "fetch.go", "go", src)
	r := findByOpTable(t, recs, OpSelect, "accounts")
	if r.Properties["orm"] != "go_sql_driver" {
		t.Errorf("orm=%q, want go_sql_driver", r.Properties["orm"])
	}
	edge := findEdge(t, r)
	if edge.Properties["table"] != "accounts" {
		t.Errorf("edge table=%q, want accounts", edge.Properties["table"])
	}
}

// --- Go: importing BOTH database/sql and a driver must not double-emit.
func TestGoSQLDriverNoDoubleEmitWithDatabaseSQL(t *testing.T) {
	src := `package main
import (
    "database/sql"
    _ "github.com/lib/pq"
)
func Fetch(db *sql.DB) { db.Query("SELECT name FROM accounts") }`
	recs := runExtract(t, "both.go", "go", src)
	count := 0
	for _, r := range recs {
		if r.Kind == KindDataAccess && r.Properties["table"] == "accounts" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected exactly 1 accounts access, got %d: %+v", count, recs)
	}
}

// --- Java plain JDBC: stmt.executeQuery("SELECT ... FROM t")
func TestJDBCExecuteQueryReadsTable(t *testing.T) {
	src := `import java.sql.Connection;
import java.sql.Statement;
class Dao {
    void load(Connection c) throws Exception {
        Statement st = c.createStatement();
        st.executeQuery("SELECT id FROM employees WHERE active = 1");
    }
}`
	recs := runExtract(t, "Dao.java", "java", src)
	r := findByOpTable(t, recs, OpSelect, "employees")
	if r.Properties["orm"] != "jdbc" {
		t.Errorf("orm=%q, want jdbc", r.Properties["orm"])
	}
	edge := findEdge(t, r)
	if edge.Properties["table"] != "employees" || edge.Properties["operation"] != OpSelect {
		t.Errorf("edge=%v, want table=employees op=SELECT", edge.Properties)
	}
}

// --- Java JDBC write + JOIN: INSERT writes, JOIN yields both tables.
func TestJDBCJoinYieldsBothTables(t *testing.T) {
	src := `import java.sql.Statement;
class Rep {
    void j(Statement st) throws Exception {
        st.executeQuery("SELECT * FROM orders JOIN customers ON orders.cid = customers.id");
    }
}`
	recs := runExtract(t, "Rep.java", "java", src)
	findByOpTable(t, recs, OpSelect, "orders")
	findByOpTable(t, recs, OpSelect, "customers")
}

// --- Negative: JDBC dynamic SQL string variable -> no fabricated table.
func TestJDBCDynamicSQLNoFabricatedTable(t *testing.T) {
	src := `import java.sql.Statement;
class D {
    void run(Statement st, String q) throws Exception { st.executeQuery(q); }
}`
	recs := runExtract(t, "D.java", "java", src)
	for _, r := range recs {
		if r.Kind == KindDataAccess && r.Properties["table"] != UnknownTable && r.Properties["table"] != "" {
			t.Errorf("fabricated table %q from dynamic JDBC SQL", r.Properties["table"])
		}
	}
}
