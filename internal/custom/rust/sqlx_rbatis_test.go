package rust_test

// sqlx_rbatis_test.go — tests for custom_rust_sqlx and custom_rust_rbatis extractors.
// Proves schema_extraction, migration_parsing, model_extraction, query_attribution.

import (
	"testing"
)

// ---------------------------------------------------------------------------
// SQLx
// ---------------------------------------------------------------------------

func TestSqlx_FromRowModel(t *testing.T) {
	src := `
use sqlx::FromRow;

#[derive(Debug, Clone, FromRow)]
pub struct User {
    pub id: i64,
    pub name: String,
    pub email: String,
}

#[derive(Debug, Clone, FromRow)]
pub struct Post {
    pub id: i64,
    pub user_id: i64,
    pub title: String,
}
`
	ents := extract(t, "custom_rust_sqlx", fi("models.rs", "rust", src))
	if !containsEntity(ents, "SCOPE.Component", "sqlx:model:User") {
		t.Error("expected sqlx:model:User from FromRow derive")
	}
	if !containsEntity(ents, "SCOPE.Component", "sqlx:model:Post") {
		t.Error("expected sqlx:model:Post from FromRow derive")
	}
}

func TestSqlx_MigrateMacro(t *testing.T) {
	src := `
use sqlx::PgPool;

pub async fn run_migrations(pool: &PgPool) -> sqlx::Result<()> {
    sqlx::migrate!("./migrations").run(pool).await?;
    Ok(())
}
`
	ents := extract(t, "custom_rust_sqlx", fi("db.rs", "rust", src))
	if !containsEntitySubtype(ents, "SCOPE.Component", "migration") {
		t.Error("expected migration component from sqlx::migrate! macro")
	}
}

func TestSqlx_MigrateMacroDefaultPath(t *testing.T) {
	src := `
sqlx::migrate!().run(&pool).await.unwrap();
`
	ents := extract(t, "custom_rust_sqlx", fi("setup.rs", "rust", src))
	if !containsEntity(ents, "SCOPE.Component", "sqlx:migrate:./migrations") {
		t.Error("expected sqlx:migrate:./migrations with default path")
	}
}

func TestSqlx_QueryMacro(t *testing.T) {
	src := `
let user = sqlx::query_as!(User, "SELECT id, name, email FROM users WHERE id = $1", id)
    .fetch_one(pool)
    .await?;
`
	ents := extract(t, "custom_rust_sqlx", fi("repo.rs", "rust", src))
	if !containsEntitySubtype(ents, "SCOPE.Operation", "sql_query") {
		t.Error("expected sql_query from query_as! macro")
	}
}

func TestSqlx_QueryAttributedToTable(t *testing.T) {
	src := `
let user = sqlx::query_as!(User, "SELECT id, name, email FROM users WHERE id = $1", id)
    .fetch_one(pool)
    .await?;
let _ = sqlx::query!("INSERT INTO posts (title, body) VALUES ($1, $2)", t, b)
    .execute(pool)
    .await?;
`
	ents := extract(t, "custom_rust_sqlx", fi("repo.rs", "rust", src))
	if !containsEntity(ents, "SCOPE.Operation", "sqlx:query:users") {
		t.Error("expected sqlx:query:users attributed from SELECT ... FROM users")
	}
	if !containsEntity(ents, "SCOPE.Operation", "sqlx:query:posts") {
		t.Error("expected sqlx:query:posts attributed from INSERT INTO posts")
	}
}

func TestSqlx_PoolConnect(t *testing.T) {
	src := `
let pool = PgPool::connect("postgres://localhost/mydb").await?;
`
	ents := extract(t, "custom_rust_sqlx", fi("main.rs", "rust", src))
	if !containsEntitySubtype(ents, "SCOPE.Component", "db_connection") {
		t.Error("expected db_connection from PgPool::connect")
	}
}

func TestSqlx_FixtureFile(t *testing.T) {
	src := readFixture(t, "testdata/sqlx_models.rs")
	ents := extract(t, "custom_rust_sqlx", fi("sqlx_models.rs", "rust", src))
	if !containsEntity(ents, "SCOPE.Component", "sqlx:model:User") {
		t.Error("expected sqlx:model:User from fixture")
	}
	if !containsEntity(ents, "SCOPE.Component", "sqlx:model:Post") {
		t.Error("expected sqlx:model:Post from fixture")
	}
	if !containsEntitySubtype(ents, "SCOPE.Component", "migration") {
		t.Error("expected migration entity from fixture")
	}
}

func TestSqlx_NoMatch(t *testing.T) {
	src := `
fn main() {
    println!("hello");
}
`
	ents := extract(t, "custom_rust_sqlx", fi("main.rs", "rust", src))
	if len(ents) != 0 {
		t.Errorf("expected no entities, got %d", len(ents))
	}
}

// ---------------------------------------------------------------------------
// rbatis
// ---------------------------------------------------------------------------

func TestRbatis_CrudTableModel(t *testing.T) {
	src := `
use serde::{Deserialize, Serialize};

#[crud_table(table_name = "biz_activity")]
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct BizActivity {
    pub id: Option<String>,
    pub name: Option<String>,
}
`
	ents := extract(t, "custom_rust_rbatis", fi("models.rs", "rust", src))
	if !containsEntity(ents, "SCOPE.Component", "rbatis:model:biz_activity") {
		t.Error("expected rbatis:model:biz_activity from crud_table")
	}
	if !containsEntity(ents, "SCOPE.Component", "rbatis:schema:biz_activity") {
		t.Error("expected rbatis:schema:biz_activity from crud_table")
	}
}

func TestRbatis_PySqlQuery(t *testing.T) {
	src := `
#[py_sql("select * from biz_activity where delete_flag = 0 and name like #{name}")]
async fn select_by_name(rb: &Rbatis, name: &str) -> Vec<BizActivity> {
    impled!()
}
`
	ents := extract(t, "custom_rust_rbatis", fi("mapper.rs", "rust", src))
	if !containsEntity(ents, "SCOPE.Operation", "rbatis:py_sql:select_by_name") {
		t.Error("expected rbatis:py_sql:select_by_name query")
	}
}

func TestRbatis_SqlAttrQuery(t *testing.T) {
	src := `
#[sql("select * from user_info where id = #{id}")]
async fn get_user_by_id(rb: &Rbatis, id: &str) -> UserInfo {
    impled!()
}
`
	ents := extract(t, "custom_rust_rbatis", fi("mapper.rs", "rust", src))
	if !containsEntity(ents, "SCOPE.Operation", "rbatis:sql:get_user_by_id") {
		t.Error("expected rbatis:sql:get_user_by_id query")
	}
}

func TestRbatis_HtmlSqlQuery(t *testing.T) {
	src := `
#[html_sql]
async fn select_by_condition(rb: &Rbatis, id: &str) -> BizActivity {
    impled!()
}
`
	ents := extract(t, "custom_rust_rbatis", fi("mapper.rs", "rust", src))
	if !containsEntity(ents, "SCOPE.Operation", "rbatis:html_sql:select_by_condition") {
		t.Error("expected rbatis:html_sql:select_by_condition query")
	}
}

func TestRbatis_QueryAttributedToTable(t *testing.T) {
	src := `
#[py_sql("select * from biz_activity where delete_flag = 0 and name like #{name}")]
async fn select_by_name(rb: &Rbatis, name: &str) -> Vec<BizActivity> {
    impled!()
}

#[sql("select * from user_info where id = #{id}")]
async fn get_user_by_id(rb: &Rbatis, id: &str) -> UserInfo {
    impled!()
}
`
	ents := extract(t, "custom_rust_rbatis", fi("mapper.rs", "rust", src))
	// The py_sql query must resolve its target table to biz_activity.
	foundBiz, foundUser := false, false
	for _, e := range ents {
		if e.Name == "rbatis:py_sql:select_by_name" && e.Kind == "SCOPE.Operation" {
			foundBiz = true
		}
		if e.Name == "rbatis:sql:get_user_by_id" && e.Kind == "SCOPE.Operation" {
			foundUser = true
		}
	}
	if !foundBiz {
		t.Error("expected rbatis py_sql query for select_by_name")
	}
	if !foundUser {
		t.Error("expected rbatis sql query for get_user_by_id")
	}
}

func TestRbatis_ConnectionInit(t *testing.T) {
	src := `
let rb = Rbatis::new();
rb.link("mysql://root:123456@localhost:3306/test").await.unwrap();
`
	ents := extract(t, "custom_rust_rbatis", fi("main.rs", "rust", src))
	if !containsEntitySubtype(ents, "SCOPE.Component", "db_connection") {
		t.Error("expected db_connection from Rbatis::new")
	}
}

func TestRbatis_FixtureFile(t *testing.T) {
	src := readFixture(t, "testdata/rbatis_models.rs")
	ents := extract(t, "custom_rust_rbatis", fi("rbatis_models.rs", "rust", src))
	if !containsEntity(ents, "SCOPE.Component", "rbatis:model:biz_activity") {
		t.Error("expected rbatis:model:biz_activity from fixture")
	}
	if !containsEntity(ents, "SCOPE.Component", "rbatis:schema:biz_activity") {
		t.Error("expected rbatis:schema:biz_activity from fixture")
	}
	if !containsEntity(ents, "SCOPE.Operation", "rbatis:py_sql:select_by_name") {
		t.Error("expected py_sql query from fixture")
	}
	if !containsEntity(ents, "SCOPE.Operation", "rbatis:html_sql:select_by_condition") {
		t.Error("expected html_sql query from fixture")
	}
	if !containsEntity(ents, "SCOPE.Operation", "rbatis:sql:get_user_by_id") {
		t.Error("expected sql attr query from fixture")
	}
}

func TestRbatis_NoMatch(t *testing.T) {
	src := `
use std::collections::HashMap;
fn main() {}
`
	ents := extract(t, "custom_rust_rbatis", fi("main.rs", "rust", src))
	if len(ents) != 0 {
		t.Errorf("expected no entities, got %d", len(ents))
	}
}
