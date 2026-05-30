package rust

// orm_props_test.go — white-box (in-package) tests that assert specific
// property *values* produced by the Rust ORM extractors: resolved table names,
// column names, SQL types, and query target tables. These value-asserting
// tests back the `full` coverage status for the deepened capabilities
// (issue #3412).

import (
	"context"
	"os"
	"testing"

	"github.com/cajasmota/archigraph/internal/extractor"
)

func runExtract(t *testing.T, ext extractor.Extractor, path, src string) []propEnt {
	t.Helper()
	ents, err := ext.Extract(context.Background(), extractor.FileInput{
		Path: path, Language: "rust", Content: []byte(src),
	})
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	out := make([]propEnt, 0, len(ents))
	for _, e := range ents {
		out = append(out, propEnt{name: e.Name, props: e.Properties})
	}
	return out
}

type propEnt struct {
	name  string
	props map[string]string
}

// findProp returns the entity with the given name, or nil.
func findProp(ents []propEnt, name string) *propEnt {
	for i := range ents {
		if ents[i].name == name {
			return &ents[i]
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Diesel — schema column types
// ---------------------------------------------------------------------------

func TestDiesel_ColumnPropsValues(t *testing.T) {
	src := `
table! {
    accounts (id) {
        id -> Integer,
        balance -> Nullable<BigInt>,
        owner_id -> Integer,
    }
}
`
	ents := runExtract(t, &rustDieselExtractor{}, "schema.rs", src)

	bal := findProp(ents, "diesel:column:accounts.balance")
	if bal == nil {
		t.Fatal("missing diesel:column:accounts.balance")
	}
	if got := bal.props["sql_type"]; got != "Nullable<BigInt>" {
		t.Errorf("sql_type = %q, want Nullable<BigInt>", got)
	}
	if got := bal.props["table_name"]; got != "accounts" {
		t.Errorf("table_name = %q, want accounts", got)
	}
	if got := bal.props["column_name"]; got != "balance" {
		t.Errorf("column_name = %q, want balance", got)
	}
}

// ---------------------------------------------------------------------------
// Diesel — SQL migration REFERENCES resolves ref table+column
// ---------------------------------------------------------------------------

func TestDiesel_SQLReferencesPropsValues(t *testing.T) {
	src := `CREATE TABLE posts (
    id SERIAL PRIMARY KEY,
    user_id INTEGER NOT NULL REFERENCES users(id)
);`
	ents := runExtract(t, &rustDieselExtractor{}, "up.sql", src)

	fk := findProp(ents, "diesel:migration:fk:users.id")
	if fk == nil {
		t.Fatal("missing diesel:migration:fk:users.id")
	}
	if got := fk.props["ref_table"]; got != "users" {
		t.Errorf("ref_table = %q, want users", got)
	}
	if got := fk.props["ref_column"]; got != "id" {
		t.Errorf("ref_column = %q, want id", got)
	}
}

// ---------------------------------------------------------------------------
// SeaORM — model column rust types
// ---------------------------------------------------------------------------

func TestSeaORM_ColumnPropsValues(t *testing.T) {
	src := `
#[derive(Clone, Debug, PartialEq, DeriveEntityModel)]
#[sea_orm(table_name = "orders")]
pub struct Model {
    #[sea_orm(primary_key)]
    pub id: i32,
    pub total: f64,
    pub customer_id: i32,
}
`
	ents := runExtract(t, &rustSeaORMExtractor{}, "order.rs", src)

	tot := findProp(ents, "seaorm:column:orders.total")
	if tot == nil {
		t.Fatal("missing seaorm:column:orders.total")
	}
	if got := tot.props["rust_type"]; got != "f64" {
		t.Errorf("rust_type = %q, want f64", got)
	}
	if got := tot.props["table_name"]; got != "orders" {
		t.Errorf("table_name = %q, want orders", got)
	}
	if findProp(ents, "seaorm:column:orders.customer_id") == nil {
		t.Error("missing seaorm:column:orders.customer_id")
	}
}

// ---------------------------------------------------------------------------
// SQLx — query target table resolution
// ---------------------------------------------------------------------------

func TestSqlx_QueryTargetTableProps(t *testing.T) {
	src := `
let u = sqlx::query_as!(User, "SELECT id, name FROM users WHERE id = $1", id).fetch_one(p).await?;
let _ = sqlx::query!("UPDATE accounts SET balance = $1 WHERE id = $2", b, id).execute(p).await?;
`
	ents := runExtract(t, &rustSqlxExtractor{}, "repo.rs", src)

	q := findProp(ents, "sqlx:query:users")
	if q == nil {
		t.Fatal("missing sqlx:query:users")
	}
	if got := q.props["target_table"]; got != "users" {
		t.Errorf("target_table = %q, want users", got)
	}
	upd := findProp(ents, "sqlx:query:accounts")
	if upd == nil {
		t.Fatal("missing sqlx:query:accounts (UPDATE attribution)")
	}
	if got := upd.props["target_table"]; got != "accounts" {
		t.Errorf("target_table = %q, want accounts", got)
	}
}

// ---------------------------------------------------------------------------
// rbatis — query target table resolution from inline SQL
// ---------------------------------------------------------------------------

func TestRbatis_QueryTargetTableProps(t *testing.T) {
	src := `
#[py_sql("select * from biz_activity where delete_flag = 0")]
async fn list(rb: &Rbatis) -> Vec<BizActivity> { impled!() }

#[sql("select * from user_info where id = #{id}")]
async fn get(rb: &Rbatis, id: &str) -> UserInfo { impled!() }
`
	ents := runExtract(t, &rustRbatisExtractor{}, "mapper.rs", src)

	py := findProp(ents, "rbatis:py_sql:list")
	if py == nil {
		t.Fatal("missing rbatis:py_sql:list")
	}
	if got := py.props["target_table"]; got != "biz_activity" {
		t.Errorf("py_sql target_table = %q, want biz_activity", got)
	}
	sq := findProp(ents, "rbatis:sql:get")
	if sq == nil {
		t.Fatal("missing rbatis:sql:get")
	}
	if got := sq.props["target_table"]; got != "user_info" {
		t.Errorf("sql target_table = %q, want user_info", got)
	}
}

// ---------------------------------------------------------------------------
// rusqlite — raw SQL query attribution to target table
// ---------------------------------------------------------------------------

func TestRusqlite_QueryAttribution(t *testing.T) {
	src := `
use rusqlite::{Connection, params};

fn run(conn: &Connection) -> rusqlite::Result<()> {
    conn.execute("INSERT INTO person (name, age) VALUES (?1, ?2)", params![n, a])?;
    let mut stmt = conn.prepare("SELECT id, name FROM person WHERE age > ?1")?;
    let _ = stmt.query_map([18], |r| r.get::<_, i32>(0))?;
    Ok(())
}
`
	ents := runExtract(t, &rustRusqliteExtractor{}, "db.rs", src)

	q := findProp(ents, "rusqlite:query:person")
	if q == nil {
		t.Fatal("missing rusqlite:query:person")
	}
	if got := q.props["target_table"]; got != "person" {
		t.Errorf("target_table = %q, want person", got)
	}
	if got := q.props["framework"]; got != "rusqlite" {
		t.Errorf("framework = %q, want rusqlite", got)
	}
}

func TestRusqlite_CreateTableAttribution(t *testing.T) {
	src := `
use rusqlite::Connection;
fn setup(conn: &Connection) {
    conn.execute("CREATE TABLE person (id INTEGER PRIMARY KEY, name TEXT)", []).unwrap();
}
`
	ents := runExtract(t, &rustRusqliteExtractor{}, "setup.rs", src)
	if findProp(ents, "rusqlite:create_table:person") == nil {
		t.Error("missing rusqlite:create_table:person")
	}
}

func TestRusqlite_FixtureFile(t *testing.T) {
	data, err := os.ReadFile("testdata/rusqlite_queries.rs")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	ents := runExtract(t, &rustRusqliteExtractor{}, "rusqlite_queries.rs", string(data))
	if findProp(ents, "rusqlite:query:person") == nil {
		t.Error("expected rusqlite:query:person from fixture (INSERT/SELECT person)")
	}
	if findProp(ents, "rusqlite:connection:open") == nil {
		t.Error("expected rusqlite:connection:open from Connection::open in fixture")
	}
}

func TestRusqlite_NoSignalNoEmit(t *testing.T) {
	// A non-rusqlite .execute( call must not be attributed to rusqlite.
	src := `
let _ = sqlx::query!("SELECT 1 FROM widgets").execute(pool).await?;
`
	ents := runExtract(t, &rustRusqliteExtractor{}, "other.rs", src)
	if len(ents) != 0 {
		t.Errorf("expected no rusqlite entities without a rusqlite signal, got %d", len(ents))
	}
}
