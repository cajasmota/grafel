package dbmap

import "testing"

// ---------------------------------------------------------------------------
// Knex (JS/TS)
// ---------------------------------------------------------------------------

func TestKnexInsertWrite(t *testing.T) {
	src := `const knex = require('knex')(config);
async function addUser(x) {
  return knex('users').insert(x);
}`
	recs := runExtract(t, "user.js", "javascript", src)
	r := findByOpTable(t, recs, OpInsert, "users")
	if r.Properties["orm"] != "knex" {
		t.Errorf("orm=%q, want knex", r.Properties["orm"])
	}
	if r.Properties["function_ref"] != "addUser" {
		t.Errorf("function_ref=%q, want addUser", r.Properties["function_ref"])
	}
	assertAccessesTableEdge(t, r)
}

func TestKnexWhereSelect(t *testing.T) {
	src := `import knex from 'knex';
function getUser(id) { return knex('users').where({ id }).first(); }`
	recs := runExtract(t, "u.ts", "typescript", src)
	r := findByOpTable(t, recs, OpSelect, "users")
	assertAccessesTableEdge(t, r)
}

func TestKnexFromSelect(t *testing.T) {
	src := `const knex = require('knex')(cfg);
function list() { return knex.select('*').from('orders'); }`
	recs := runExtract(t, "o.js", "javascript", src)
	r := findByOpTable(t, recs, OpSelect, "orders")
	if r.Properties["orm"] != "knex" {
		t.Errorf("orm=%q, want knex", r.Properties["orm"])
	}
	assertAccessesTableEdge(t, r)
}

func TestKnexUpdateWrite(t *testing.T) {
	src := `import knex from 'knex';
function rename(id) { return knex('users').where({ id }).update({ name: 'x' }); }`
	recs := runExtract(t, "u.ts", "typescript", src)
	r := findByOpTable(t, recs, OpUpdate, "users")
	assertAccessesTableEdge(t, r)
}

func TestKnexDeleteWrite(t *testing.T) {
	src := `import knex from 'knex';
function rm(id) { return knex('sessions').where({ id }).del(); }`
	recs := runExtract(t, "s.ts", "typescript", src)
	r := findByOpTable(t, recs, OpDelete, "sessions")
	assertAccessesTableEdge(t, r)
}

// Negative: dynamic table variable must NOT fabricate a table edge.
func TestKnexDynamicTableNoEdge(t *testing.T) {
	src := `import knex from 'knex';
function q(tableVar, x) { return knex(tableVar).insert(x); }`
	recs := runExtract(t, "d.ts", "typescript", src)
	for _, r := range recs {
		if r.Kind == KindDataAccess && r.Properties["orm"] == "knex" {
			t.Fatalf("fabricated knex table edge from dynamic var: %+v", r)
		}
	}
}

// ---------------------------------------------------------------------------
// Drizzle (TS)
// ---------------------------------------------------------------------------

func TestDrizzleSelectFromRead(t *testing.T) {
	src := `import { pgTable } from 'drizzle-orm/pg-core';
export const users = pgTable('users', { id: serial('id') });
function all() { return db.select().from(users); }`
	recs := runExtract(t, "q.ts", "typescript", src)
	r := findByOpTable(t, recs, OpSelect, "users")
	if r.Properties["orm"] != "drizzle" {
		t.Errorf("orm=%q, want drizzle", r.Properties["orm"])
	}
	assertAccessesTableEdge(t, r)
}

func TestDrizzleInsertWrite(t *testing.T) {
	src := `import { pgTable } from 'drizzle-orm/pg-core';
export const accounts = pgTable('accounts', {});
function add(v) { return db.insert(accounts).values(v); }`
	recs := runExtract(t, "a.ts", "typescript", src)
	r := findByOpTable(t, recs, OpInsert, "accounts")
	assertAccessesTableEdge(t, r)
}

// Table-name literal differs from object identifier — resolution must use the
// declared literal, not the variable name.
func TestDrizzleTableNameLiteralResolved(t *testing.T) {
	src := `import { pgTable } from 'drizzle-orm';
export const userTbl = pgTable('app_users', {});
function all() { return db.select().from(userTbl); }`
	recs := runExtract(t, "q.ts", "typescript", src)
	findByOpTable(t, recs, OpSelect, "app_users")
}

// Negative: unknown table object → no edge.
func TestDrizzleUnknownObjectNoEdge(t *testing.T) {
	src := `import { pgTable } from 'drizzle-orm';
export const users = pgTable('users', {});
function all() { return db.select().from(mysteryTbl); }`
	recs := runExtract(t, "q.ts", "typescript", src)
	for _, r := range recs {
		if r.Properties["table"] == "mysterytbl" || r.Properties["table"] == "mysteryTbl" {
			t.Fatalf("fabricated drizzle edge for unknown object: %+v", r)
		}
	}
}

// ---------------------------------------------------------------------------
// jOOQ (Java)
// ---------------------------------------------------------------------------

func TestJOOQSelectFrom(t *testing.T) {
	src := `import org.jooq.DSLContext;
import static com.app.Tables.USERS;
class Repo {
  void list(DSLContext dsl) { dsl.selectFrom(USERS).fetch(); }
}`
	recs := runExtract(t, "Repo.java", "java", src)
	r := findByOpTable(t, recs, OpSelect, "users")
	if r.Properties["orm"] != "jooq" {
		t.Errorf("orm=%q, want jooq", r.Properties["orm"])
	}
	assertAccessesTableEdge(t, r)
}

func TestJOOQInsertInto(t *testing.T) {
	src := `import org.jooq.DSLContext;
class Repo { void add(DSLContext dsl) { dsl.insertInto(Tables.ACCOUNTS).execute(); } }`
	recs := runExtract(t, "Repo.java", "java", src)
	findByOpTable(t, recs, OpInsert, "accounts")
}

func TestJOOQDeleteFrom(t *testing.T) {
	src := `import org.jooq.DSLContext;
class Repo { void rm(DSLContext dsl) { dsl.deleteFrom(SESSIONS).execute(); } }`
	recs := runExtract(t, "Repo.java", "java", src)
	findByOpTable(t, recs, OpDelete, "sessions")
}

// ---------------------------------------------------------------------------
// QueryDSL (Java)
// ---------------------------------------------------------------------------

func TestQueryDSLSelectFrom(t *testing.T) {
	src := `import com.querydsl.jpa.impl.JPAQueryFactory;
class Repo {
  void list(JPAQueryFactory queryFactory) {
    queryFactory.selectFrom(user).fetch();
  }
}`
	recs := runExtract(t, "Repo.java", "java", src)
	r := findByOpTable(t, recs, OpSelect, "users")
	if r.Properties["orm"] != "querydsl" {
		t.Errorf("orm=%q, want querydsl", r.Properties["orm"])
	}
	assertAccessesTableEdge(t, r)
}

func TestQueryDSLQPrefixInstance(t *testing.T) {
	src := `import com.querydsl.core.types.dsl.PathBuilder;
class Repo { void list(JPAQueryFactory qf) { qf.selectFrom(qOrder).fetch(); } }`
	recs := runExtract(t, "Repo.java", "java", src)
	findByOpTable(t, recs, OpSelect, "orders")
}

// ---------------------------------------------------------------------------
// SQLAlchemy Core (Python)
// ---------------------------------------------------------------------------

func TestSQLAlchemyCoreSelectRead(t *testing.T) {
	src := `from sqlalchemy import Table, select, MetaData
metadata = MetaData()
users = Table('users', metadata)
def all_users(conn):
    return conn.execute(select(users))`
	recs := runExtract(t, "q.py", "python", src)
	r := findByOpTable(t, recs, OpSelect, "users")
	if r.Properties["orm"] != "sqlalchemy_core" {
		t.Errorf("orm=%q, want sqlalchemy_core", r.Properties["orm"])
	}
	assertAccessesTableEdge(t, r)
}

func TestSQLAlchemyCoreUpdateWrite(t *testing.T) {
	src := `from sqlalchemy import Table, MetaData
metadata = MetaData()
users = Table('users', metadata)
def touch(conn):
    return conn.execute(users.update().values(seen=True))`
	recs := runExtract(t, "q.py", "python", src)
	r := findByOpTable(t, recs, OpUpdate, "users")
	if r.Properties["orm"] != "sqlalchemy_core" {
		t.Errorf("orm=%q, want sqlalchemy_core", r.Properties["orm"])
	}
	assertAccessesTableEdge(t, r)
}

func TestSQLAlchemyCoreInsertWrite(t *testing.T) {
	src := `from sqlalchemy import Table, MetaData
accounts = Table('accounts', MetaData())
def add(conn, v):
    return conn.execute(accounts.insert().values(v))`
	recs := runExtract(t, "q.py", "python", src)
	findByOpTable(t, recs, OpInsert, "accounts")
}

// Table-name literal differs from the python variable — resolution uses the
// declared literal.
func TestSQLAlchemyCoreLiteralResolved(t *testing.T) {
	src := `from sqlalchemy import Table, select, MetaData
user_tbl = Table('app_users', MetaData())
def all(conn):
    return conn.execute(select(user_tbl))`
	recs := runExtract(t, "q.py", "python", src)
	findByOpTable(t, recs, OpSelect, "app_users")
}

// ---------------------------------------------------------------------------
// ActiveRecord association joins (Ruby)
// ---------------------------------------------------------------------------

func TestActiveRecordJoinsAssociation(t *testing.T) {
	src := `require 'activerecord'
class User < ApplicationRecord
  def with_orders
    User.joins(:orders).where(active: true)
  end
end`
	recs := runExtract(t, "user.rb", "ruby", src)
	// Association table pulled in by joins(:orders).
	r := findByOpTable(t, recs, OpSelect, "orders")
	if r.Properties["orm"] != "activerecord" {
		t.Errorf("orm=%q, want activerecord", r.Properties["orm"])
	}
	assertAccessesTableEdge(t, r)
}
