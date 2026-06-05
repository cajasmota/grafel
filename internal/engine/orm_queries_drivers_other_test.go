// Value-asserting tests for the cross-language driver-topology pass (#3645).
//
// Each test feeds a realistic driver call shape and asserts the exact
// collection / table / index the call touches surfaces as a QUERIES edge
// target (Class:<Resource>). These are NOT len>0 smoke tests: the asserted
// target name is the load-bearing claim. Dynamic-name negatives prove the
// pass does not hallucinate a target when the name is a runtime value.
package engine

import "testing"

// ---------------------------------------------------------------------------
// C#
// ---------------------------------------------------------------------------

func TestDriver_CSharpMongoGetCollection(t *testing.T) {
	src := `using MongoDB.Driver;
public class UserRepo {
    private readonly IMongoDatabase db;
    public async Task<User> GetUser(string id) {
        var col = db.GetCollection<User>("users");
        return await col.Find(x => x.Id == id).FirstOrDefaultAsync();
    }
}
`
	edges := detectORM(t, "csharp", "Repo.cs", src)
	e := assertEdgeExists(t, edges, "Function:GetUser", "Class:User", "find")
	if e.ORM != "mongodb" {
		t.Errorf("expected orm=mongodb, got %q", e.ORM)
	}
}

func TestDriver_CSharpDynamoTableName(t *testing.T) {
	src := `using Amazon.DynamoDBv2;
public class Store {
    public async Task Get() {
        var req = new GetItemRequest { TableName = "Products" };
        await client.GetItemAsync(req);
    }
}
`
	edges := detectORM(t, "csharp", "Store.cs", src)
	e := assertEdgeExists(t, edges, "Function:Get", "Class:Product", "find")
	if e.ORM != "dynamodb" {
		t.Errorf("expected orm=dynamodb, got %q", e.ORM)
	}
}

func TestDriver_CSharpElasticIndex(t *testing.T) {
	src := `using Nest;
public class Search {
    public async Task Run() {
        var r = await client.SearchAsync<Log>(s => s.Index("logs"));
    }
}
`
	edges := detectORM(t, "csharp", "Search.cs", src)
	e := assertEdgeExists(t, edges, "Function:Run", "Class:Log", "find")
	if e.ORM != "elastic" {
		t.Errorf("expected orm=elastic, got %q", e.ORM)
	}
}

func TestDriver_CSharpCassandraCQL(t *testing.T) {
	src := `using Cassandra;
public class Events {
    private ISession session;
    public void List() {
        session.Execute("SELECT id, name FROM events WHERE day = ?");
    }
}
`
	edges := detectORM(t, "csharp", "Events.cs", src)
	e := assertEdgeExists(t, edges, "Function:List", "Class:Event", "find")
	if e.ORM != "cassandra" {
		t.Errorf("expected orm=cassandra, got %q", e.ORM)
	}
}

func TestDriver_CSharpDynamoDynamicNameSkipped(t *testing.T) {
	src := `using Amazon.DynamoDBv2;
public class Store {
    public async Task Get(string tbl) {
        var req = new GetItemRequest { TableName = tbl };
        await client.GetItemAsync(req);
    }
}
`
	edges := detectORM(t, "csharp", "Store.cs", src)
	for _, e := range edges {
		if e.ORM == "dynamodb" {
			t.Errorf("expected no dynamodb edge for dynamic TableName, got %+v", e)
		}
	}
}

// ---------------------------------------------------------------------------
// PHP
// ---------------------------------------------------------------------------

func TestDriver_PHPSelectCollection(t *testing.T) {
	src := `<?php
use MongoDB\Client;
class OrderRepo {
    public function find() {
        $col = $mongo->selectCollection('shop', 'orders');
        return $col->find([]);
    }
}
`
	edges := detectORM(t, "php", "OrderRepo.php", src)
	e := assertEdgeExists(t, edges, "Function:find", "Class:Order", "find")
	if e.ORM != "mongodb" {
		t.Errorf("expected orm=mongodb, got %q", e.ORM)
	}
}

func TestDriver_PHPDynamoTableName(t *testing.T) {
	src := `<?php
use Aws\DynamoDb\DynamoDbClient;
class Store {
    public function get() {
        return $dynamodb->getItem(['TableName' => 'Products', 'Key' => $k]);
    }
}
`
	edges := detectORM(t, "php", "Store.php", src)
	e := assertEdgeExists(t, edges, "Function:get", "Class:Product", "find")
	if e.ORM != "dynamodb" {
		t.Errorf("expected orm=dynamodb, got %q", e.ORM)
	}
}

func TestDriver_PHPCassandraCQL(t *testing.T) {
	src := `<?php
use Cassandra\Cluster;
class Events {
    public function list() {
        return $session->execute("SELECT id FROM events WHERE day = ?");
    }
}
`
	edges := detectORM(t, "php", "Events.php", src)
	e := assertEdgeExists(t, edges, "Function:list", "Class:Event", "find")
	if e.ORM != "cassandra" {
		t.Errorf("expected orm=cassandra, got %q", e.ORM)
	}
}

func TestDriver_PHPElasticIndex(t *testing.T) {
	src := `<?php
use Elasticsearch\ClientBuilder;
class Search {
    public function run() {
        return $client->search(['index' => 'products', 'body' => $q]);
    }
}
`
	edges := detectORM(t, "php", "Search.php", src)
	e := assertEdgeExists(t, edges, "Function:run", "Class:Product", "find")
	if e.ORM != "elastic" {
		t.Errorf("expected orm=elastic, got %q", e.ORM)
	}
}

// Raw-SQL: PDO MySQL. `$pdo->query("SELECT ... FROM users")` with a `mysql:`
// DSN gate → QUERIES edge to Class:User with orm=mysql. Covers
// lang.php.driver.mysql.
func TestDriver_PHPPdoMySQLRawSQL(t *testing.T) {
	src := `<?php
class UserRepo {
    public function load() {
        $pdo = new PDO("mysql:host=localhost;dbname=app", $u, $p);
        $stmt = $pdo->query("SELECT id, name FROM users WHERE active = 1");
        return $stmt->fetchAll();
    }
}
`
	edges := detectORM(t, "php", "UserRepo.php", src)
	e := assertEdgeExists(t, edges, "Function:load", "Class:User", "find")
	if e.ORM != "mysql" {
		t.Errorf("expected orm=mysql, got %q", e.ORM)
	}
}

// Raw-SQL: mysqli procedural. `mysqli_query($conn, "INSERT INTO ...")` — the
// SQL is the SECOND arg, so firstStringLiteral picks it ($conn is unquoted).
// INSERT → create op, orm=mysql. Covers lang.php.driver.mysql (mysqli).
func TestDriver_PHPMysqliProceduralRawSQL(t *testing.T) {
	src := `<?php
function add_product($conn) {
    $conn = mysqli_connect("localhost", "u", "p", "shop");
    mysqli_query($conn, "INSERT INTO products (name) VALUES ('x')");
}
`
	edges := detectORM(t, "php", "products.php", src)
	e := assertEdgeExists(t, edges, "Function:add_product", "Class:Product", "create")
	if e.ORM != "mysql" {
		t.Errorf("expected orm=mysql, got %q", e.ORM)
	}
}

// Raw-SQL: PDO PostgreSQL. `$db->exec("DELETE FROM sessions")` with a `pgsql:`
// DSN gate → delete op, orm=postgres. Covers lang.php.driver.postgres.
func TestDriver_PHPPdoPostgresRawSQL(t *testing.T) {
	src := `<?php
class SessionRepo {
    public function purge() {
        $db = new PDO("pgsql:host=localhost;dbname=app");
        $db->exec("DELETE FROM sessions WHERE expired = true");
    }
}
`
	edges := detectORM(t, "php", "SessionRepo.php", src)
	e := assertEdgeExists(t, edges, "Function:purge", "Class:Session", "delete")
	if e.ORM != "postgres" {
		t.Errorf("expected orm=postgres, got %q", e.ORM)
	}
}

// Raw-SQL: pgsql procedural. `pg_query($conn, "UPDATE orders ...")` — SQL is
// the second arg; UPDATE → update op, orm=postgres. Covers
// lang.php.driver.postgres (pgsql extension).
func TestDriver_PHPPgQueryRawSQL(t *testing.T) {
	src := `<?php
function ship($conn) {
    $conn = pg_connect("host=localhost dbname=app");
    pg_query($conn, "UPDATE orders SET shipped = true WHERE id = 1");
}
`
	edges := detectORM(t, "php", "orders.php", src)
	e := assertEdgeExists(t, edges, "Function:ship", "Class:Order", "update")
	if e.ORM != "postgres" {
		t.Errorf("expected orm=postgres, got %q", e.ORM)
	}
}

// Raw-SQL: PDO SQLite. `$pdo->prepare("SELECT ... FROM logs")` with a `sqlite:`
// DSN gate → find op, orm=sqlite. Covers lang.php.driver.sqlite.
func TestDriver_PHPPdoSQLiteRawSQL(t *testing.T) {
	src := `<?php
class LogRepo {
    public function tail() {
        $pdo = new PDO("sqlite:/var/data/app.db");
        $stmt = $pdo->prepare("SELECT * FROM logs ORDER BY ts DESC");
        $stmt->execute();
    }
}
`
	edges := detectORM(t, "php", "LogRepo.php", src)
	e := assertEdgeExists(t, edges, "Function:tail", "Class:Log", "find")
	if e.ORM != "sqlite" {
		t.Errorf("expected orm=sqlite, got %q", e.ORM)
	}
}

// Non-vacuous proof: a `->query("...")` call WITHOUT any backend DSN / driver
// import must NOT emit a SQL QUERIES edge — the backend gate is load-bearing
// (otherwise an arbitrary `->query(` would hallucinate a datastore edge).
func TestDriver_PHPRawSQLNoBackendGateSkipped(t *testing.T) {
	src := `<?php
class Builder {
    public function load($qb) {
        return $qb->query("SELECT id FROM users")->getResult();
    }
}
`
	edges := detectORM(t, "php", "Builder.php", src)
	for _, e := range edges {
		if e.ORM == "mysql" || e.ORM == "postgres" || e.ORM == "sqlite" {
			t.Errorf("expected no SQL-driver edge without a backend gate, got %+v", e)
		}
	}
}

// Raw-SQL dynamic: an interpolated SQL string ("... FROM {$table}") has no
// static literal table for extractSQLTable → honest-skipped (no hallucinated
// edge), even though the mysql backend gate fires.
func TestDriver_PHPRawSQLDynamicSkipped(t *testing.T) {
	src := `<?php
class Repo {
    public function load($table) {
        $pdo = new PDO("mysql:host=localhost;dbname=app");
        $stmt = $pdo->query("SELECT * FROM {$table} WHERE id = 1");
        return $stmt->fetchAll();
    }
}
`
	edges := detectORM(t, "php", "dyn.php", src)
	for _, e := range edges {
		if e.ORM == "mysql" {
			t.Errorf("expected no mysql edge for interpolated SQL, got %+v", e)
		}
	}
}

// ---------------------------------------------------------------------------
// Rust
// ---------------------------------------------------------------------------

func TestDriver_RustMongoCollection(t *testing.T) {
	src := `use mongodb::Database;
async fn get_user(db: &Database) -> User {
    let col = db.collection::<User>("users");
    col.find_one(doc! {}, None).await.unwrap().unwrap()
}
`
	edges := detectORM(t, "rust", "repo.rs", src)
	e := assertEdgeExists(t, edges, "Function:get_user", "Class:User", "find")
	if e.ORM != "mongodb" {
		t.Errorf("expected orm=mongodb, got %q", e.ORM)
	}
}

func TestDriver_RustScyllaCQL(t *testing.T) {
	src := `use scylla::Session;
async fn list(session: &Session) {
    session.query("SELECT id FROM events WHERE day = ?", (1,)).await.unwrap();
}
`
	edges := detectORM(t, "rust", "events.rs", src)
	e := assertEdgeExists(t, edges, "Function:list", "Class:Event", "find")
	if e.ORM != "cassandra" {
		t.Errorf("expected orm=cassandra, got %q", e.ORM)
	}
}

// Raw-SQL: sqlx Postgres backend. `sqlx::query_as::<_, T>("SELECT ... FROM users")`
// → QUERIES edge to Class:User with orm=postgres (the PgPool/sqlx::Postgres gate
// selects the postgres backend tag). Covers lang.rust.driver.postgres.
func TestDriver_RustSqlxPostgresRawSQL(t *testing.T) {
	src := `use sqlx::PgPool;
async fn load_users(pool: &PgPool) -> Vec<User> {
    sqlx::query_as::<_, User>("SELECT id, name FROM users WHERE active = true")
        .fetch_all(pool)
        .await
        .unwrap()
}
`
	edges := detectORM(t, "rust", "users_repo.rs", src)
	e := assertEdgeExists(t, edges, "Function:load_users", "Class:User", "find")
	if e.ORM != "postgres" {
		t.Errorf("expected orm=postgres, got %q", e.ORM)
	}
}

// Raw-SQL: tokio-postgres `client.query("...")`. Covers postgres backend via the
// standalone driver gate.
func TestDriver_RustTokioPostgresRawSQL(t *testing.T) {
	src := `use tokio_postgres::Client;
async fn fetch(client: &Client) {
    let rows = client.query("SELECT * FROM orders WHERE id = $1", &[&1i32]).await.unwrap();
}
`
	edges := detectORM(t, "rust", "orders.rs", src)
	e := assertEdgeExists(t, edges, "Function:fetch", "Class:Order", "find")
	if e.ORM != "postgres" {
		t.Errorf("expected orm=postgres, got %q", e.ORM)
	}
}

// Raw-SQL: sqlx MySQL backend (`MySqlPool`). An INSERT statement → create op.
// Covers lang.rust.driver.mysql.
func TestDriver_RustSqlxMySQLRawSQL(t *testing.T) {
	src := `use sqlx::MySqlPool;
async fn add_product(pool: &MySqlPool) {
    sqlx::query("INSERT INTO products (name) VALUES (?)")
        .execute(pool)
        .await
        .unwrap();
}
`
	edges := detectORM(t, "rust", "products.rs", src)
	e := assertEdgeExists(t, edges, "Function:add_product", "Class:Product", "create")
	if e.ORM != "mysql" {
		t.Errorf("expected orm=mysql, got %q", e.ORM)
	}
}

// Raw-SQL: rusqlite SQLite. `conn.execute("...")`. Covers lang.rust.driver.sqlite.
func TestDriver_RustRusqliteSQLite(t *testing.T) {
	src := `use rusqlite::Connection;
fn delete_session(conn: &Connection) {
    conn.execute("DELETE FROM sessions WHERE expired = 1", []).unwrap();
}
`
	edges := detectORM(t, "rust", "sessions.rs", src)
	e := assertEdgeExists(t, edges, "Function:delete_session", "Class:Session", "delete")
	if e.ORM != "sqlite" {
		t.Errorf("expected orm=sqlite, got %q", e.ORM)
	}
}

// Non-vacuous proof for raw SQL: the SAME query call WITHOUT a backend-crate
// import must NOT emit a SQL QUERIES edge — the backend gate is load-bearing.
func TestDriver_RustRawSQLNoBackendGateSkipped(t *testing.T) {
	src := `async fn load_users(pool: &SomePool) {
    sqlx_like_query("SELECT id FROM users").fetch_all(pool).await.unwrap();
}
`
	edges := detectORM(t, "rust", "ungated.rs", src)
	for _, e := range edges {
		if e.ORM == "postgres" || e.ORM == "mysql" || e.ORM == "sqlite" {
			t.Errorf("expected no SQL-driver edge without a backend-crate gate, got %+v", e)
		}
	}
}

// Raw-SQL dynamic: an interpolated `format!` SQL string has no static literal
// table for extractSQLTable → honest-skipped (no hallucinated edge).
func TestDriver_RustRawSQLDynamicSkipped(t *testing.T) {
	src := `use sqlx::PgPool;
async fn load(pool: &PgPool, table: &str) {
    let q = format!("SELECT * FROM {}", table);
    sqlx::query(&q).fetch_all(pool).await.unwrap();
}
`
	edges := detectORM(t, "rust", "dyn.rs", src)
	for _, e := range edges {
		if e.ORM == "postgres" {
			t.Errorf("expected no postgres edge for interpolated SQL, got %+v", e)
		}
	}
}

// DynamoDB: aws-sdk-rust fluent builder `.table_name("Products")`. Covers
// lang.rust.driver.dynamodb.
func TestDriver_RustDynamoTableName(t *testing.T) {
	src := `use aws_sdk_dynamodb::Client;
async fn get_item(client: &Client) {
    let resp = client
        .get_item()
        .table_name("Products")
        .key("id", AttributeValue::S("1".into()))
        .send()
        .await
        .unwrap();
}
`
	edges := detectORM(t, "rust", "store.rs", src)
	e := assertEdgeExists(t, edges, "Function:get_item", "Class:Product", "find")
	if e.ORM != "dynamodb" {
		t.Errorf("expected orm=dynamodb, got %q", e.ORM)
	}
}

// DynamoDB dynamic: a table name bound to a variable is not a literal — the
// `.table_name(tbl)` form is honest-skipped.
func TestDriver_RustDynamoDynamicTableSkipped(t *testing.T) {
	src := `use aws_sdk_dynamodb::Client;
async fn get_item(client: &Client, tbl: &str) {
    client.get_item().table_name(tbl).send().await.unwrap();
}
`
	edges := detectORM(t, "rust", "store.rs", src)
	for _, e := range edges {
		if e.ORM == "dynamodb" {
			t.Errorf("expected no dynamodb edge for dynamic table_name, got %+v", e)
		}
	}
}

// Elasticsearch: elasticsearch-rs lowercase `.index("products")` fluent builder.
// Covers lang.rust.driver.elastic.
func TestDriver_RustElasticIndexBuilder(t *testing.T) {
	src := `use elasticsearch::Elasticsearch;
async fn run(client: &Elasticsearch) {
    let resp = client.index(IndexParts::Index("products")).body(json!({})).send().await.unwrap();
}
`
	edges := detectORM(t, "rust", "search.rs", src)
	e := assertEdgeExists(t, edges, "Function:run", "Class:Product", "find")
	if e.ORM != "elastic" {
		t.Errorf("expected orm=elastic, got %q", e.ORM)
	}
}

// Elasticsearch: request-path enum `SearchParts::Index(&["logs"])`.
func TestDriver_RustElasticSearchParts(t *testing.T) {
	src := `use elasticsearch::{Elasticsearch, SearchParts};
async fn search(client: &Elasticsearch) {
    let resp = client.search(SearchParts::Index(&["logs"])).send().await.unwrap();
}
`
	edges := detectORM(t, "rust", "search.rs", src)
	e := assertEdgeExists(t, edges, "Function:search", "Class:Log", "find")
	if e.ORM != "elastic" {
		t.Errorf("expected orm=elastic, got %q", e.ORM)
	}
}

// Elasticsearch dynamic / ungated: an index bound to a variable is honest-skipped,
// AND without the elasticsearch gate no edge fires (non-vacuous proof).
func TestDriver_RustElasticDynamicAndUngatedSkipped(t *testing.T) {
	dyn := `use elasticsearch::Elasticsearch;
async fn search(client: &Elasticsearch, idx: &str) {
    client.search(SearchParts::Index(&[idx])).send().await.unwrap();
}
`
	for _, e := range detectORM(t, "rust", "dyn.rs", dyn) {
		if e.ORM == "elastic" {
			t.Errorf("expected no elastic edge for dynamic index, got %+v", e)
		}
	}
	ungated := `async fn search(client: &Foo) {
    client.index("products").send();
}
`
	for _, e := range detectORM(t, "rust", "ungated.rs", ungated) {
		if e.ORM == "elastic" {
			t.Errorf("expected no elastic edge without elasticsearch gate, got %+v", e)
		}
	}
}

// ---------------------------------------------------------------------------
// Python
// ---------------------------------------------------------------------------

func TestDriver_PythonPymongoGetCollection(t *testing.T) {
	src := `from pymongo import MongoClient

def fetch():
    col = db.get_collection("users")
    return col.find_one({})
`
	edges := detectORM(t, "python", "repo.py", src)
	e := assertEdgeExists(t, edges, "Function:fetch", "Class:User", "find")
	if e.ORM != "pymongo" {
		t.Errorf("expected orm=pymongo, got %q", e.ORM)
	}
}

func TestDriver_PythonBoto3DynamoTable(t *testing.T) {
	src := `import boto3

def get_item():
    table = dynamodb.Table('Products')
    return table.get_item(Key={'id': 1})
`
	edges := detectORM(t, "python", "store.py", src)
	e := assertEdgeExists(t, edges, "Function:get_item", "Class:Product", "find")
	if e.ORM != "dynamodb" {
		t.Errorf("expected orm=dynamodb, got %q", e.ORM)
	}
}

func TestDriver_PythonCassandraCQL(t *testing.T) {
	src := `from cassandra.cluster import Cluster

def list_events():
    return session.execute("SELECT id FROM events WHERE day = %s", (1,))
`
	edges := detectORM(t, "python", "events.py", src)
	e := assertEdgeExists(t, edges, "Function:list_events", "Class:Event", "find")
	if e.ORM != "cassandra" {
		t.Errorf("expected orm=cassandra, got %q", e.ORM)
	}
}

func TestDriver_PythonElasticIndex(t *testing.T) {
	src := `from elasticsearch import Elasticsearch

def search():
    return es.search(index='logs', body={})
`
	edges := detectORM(t, "python", "search.py", src)
	e := assertEdgeExists(t, edges, "Function:search", "Class:Log", "find")
	if e.ORM != "elastic" {
		t.Errorf("expected orm=elastic, got %q", e.ORM)
	}
}

// ---------------------------------------------------------------------------
// Java
// ---------------------------------------------------------------------------

func TestDriver_JavaMongoGetCollection(t *testing.T) {
	src := `import com.mongodb.client.MongoDatabase;
public class UserRepo {
    private MongoDatabase db;
    public Document find() {
        return db.getCollection("users").find().first();
    }
}
`
	edges := detectORM(t, "java", "UserRepo.java", src)
	e := assertEdgeExists(t, edges, "Function:find", "Class:User", "find")
	if e.ORM != "mongodb" {
		t.Errorf("expected orm=mongodb, got %q", e.ORM)
	}
}

func TestDriver_JavaCassandraCQL(t *testing.T) {
	src := `import com.datastax.oss.driver.api.core.CqlSession;
public class Events {
    private CqlSession session;
    public void list() {
        session.execute("SELECT id FROM events WHERE day = ?");
    }
}
`
	edges := detectORM(t, "java", "Events.java", src)
	e := assertEdgeExists(t, edges, "Function:list", "Class:Event", "find")
	if e.ORM != "cassandra" {
		t.Errorf("expected orm=cassandra, got %q", e.ORM)
	}
}

// ---------------------------------------------------------------------------
// Java — Spring Data (Cassandra / Elasticsearch / MongoDB)
// ---------------------------------------------------------------------------

func TestDriver_JavaSpringCassandraQueryAnnotation(t *testing.T) {
	src := `import org.springframework.data.cassandra.repository.Query;
import org.springframework.data.cassandra.repository.CassandraRepository;
public interface EventRepository extends CassandraRepository<Event, UUID> {
    @Query("SELECT id, name FROM events WHERE day = ?0")
    List<Event> findByDay(String day);
}
`
	edges := detectORM(t, "java", "EventRepository.java", src)
	e := assertEdgeExists(t, edges, "Function:findByDay", "Class:Event", "find")
	if e.ORM != "cassandra" {
		t.Errorf("expected orm=cassandra, got %q", e.ORM)
	}
}

func TestDriver_JavaSpringCassandraTableEntity(t *testing.T) {
	src := `import org.springframework.data.cassandra.core.mapping.Table;
@Table("sensor_readings")
public class SensorReading {
    @PrimaryKey private UUID id;
}
`
	edges := detectORM(t, "java", "SensorReading.java", src)
	e := assertEdgeExists(t, edges, "Function:SensorReading", "Class:Sensor_reading", "find")
	if e.ORM != "cassandra" {
		t.Errorf("expected orm=cassandra, got %q", e.ORM)
	}
}

func TestDriver_JavaSpringCassandraDynamicCQLSkipped(t *testing.T) {
	src := `import org.springframework.data.cassandra.core.CassandraTemplate;
public class EventDao {
    private CassandraTemplate cassandraTemplate;
    public void run(String cql) {
        cassandraTemplate.getCqlOperations().execute(cql);
    }
}
`
	edges := detectORM(t, "java", "EventDao.java", src)
	for _, e := range edges {
		if e.ORM == "cassandra" {
			t.Errorf("expected no cassandra edge for dynamic CQL, got %+v", e)
		}
	}
}

func TestDriver_JavaSpringElasticDocumentIndex(t *testing.T) {
	src := `import org.springframework.data.elasticsearch.annotations.Document;
@Document(indexName = "products")
public class Product {
    @Id private String id;
}
`
	edges := detectORM(t, "java", "Product.java", src)
	e := assertEdgeExists(t, edges, "Function:Product", "Class:Product", "find")
	if e.ORM != "elastic" {
		t.Errorf("expected orm=elastic, got %q", e.ORM)
	}
}

func TestDriver_JavaSpringElasticQueryAnnotation(t *testing.T) {
	src := `import org.springframework.data.elasticsearch.annotations.Document;
import org.springframework.data.elasticsearch.annotations.Query;
import org.springframework.data.elasticsearch.repository.ElasticsearchRepository;

@Document(indexName = "orders")
class Order { @Id String id; }

interface OrderRepository extends ElasticsearchRepository<Order, String> {
    @Query("{\"match\": {\"status\": \"?0\"}}")
    List<Order> findByStatus(String status);
}
`
	edges := detectORM(t, "java", "OrderRepository.java", src)
	// @Document → index "orders"
	assertEdgeExists(t, edges, "Function:Order", "Class:Order", "find")
	// @Query method attributed to the file's resolved index "orders".
	e := assertEdgeExists(t, edges, "Function:findByStatus", "Class:Order", "find")
	if e.ORM != "elastic" {
		t.Errorf("expected orm=elastic, got %q", e.ORM)
	}
}

func TestDriver_JavaSpringElasticDynamicIndexSkipped(t *testing.T) {
	src := `import org.springframework.data.elasticsearch.core.ElasticsearchOperations;
public class SearchSvc {
    private ElasticsearchOperations ops;
    public void run(String idx) {
        var req = IndexCoordinates.of(idx);
        ops.search(query, Doc.class, req);
    }
}
`
	edges := detectORM(t, "java", "SearchSvc.java", src)
	for _, e := range edges {
		if e.ORM == "elastic" {
			t.Errorf("expected no elastic edge for dynamic index, got %+v", e)
		}
	}
}

func TestDriver_JavaSpringMongoDocumentCollection(t *testing.T) {
	src := `import org.springframework.data.mongodb.core.mapping.Document;
@Document(collection = "books")
public class Book {
    @Id private String id;
}
`
	edges := detectORM(t, "java", "Book.java", src)
	e := assertEdgeExists(t, edges, "Function:Book", "Class:Book", "find")
	if e.ORM != "mongodb" {
		t.Errorf("expected orm=mongodb, got %q", e.ORM)
	}
}

func TestDriver_JavaSpringMongoQueryAnnotation(t *testing.T) {
	src := `import org.springframework.data.mongodb.core.mapping.Document;
import org.springframework.data.mongodb.repository.Query;
import org.springframework.data.mongodb.repository.MongoRepository;

@Document("orders")
class OrderDoc { @Id String id; }

interface OrderRepo extends MongoRepository<OrderDoc, String> {
    @Query("{ 'status': ?0 }")
    List<OrderDoc> findByStatus(String status);
}
`
	edges := detectORM(t, "java", "OrderRepo.java", src)
	// @Document("orders") → collection.
	assertEdgeExists(t, edges, "Function:OrderDoc", "Class:Order", "find")
	// @Query method attributed to the file's resolved collection "orders".
	e := assertEdgeExists(t, edges, "Function:findByStatus", "Class:Order", "find")
	if e.ORM != "mongodb" {
		t.Errorf("expected orm=mongodb, got %q", e.ORM)
	}
}

// ---------------------------------------------------------------------------
// Ruby
// ---------------------------------------------------------------------------

func TestDriver_RubyMongoCollection(t *testing.T) {
	src := `require 'mongo'
class UserRepo
  def find_all
    client[:users].find.to_a
  end
end
`
	edges := detectORM(t, "ruby", "user_repo.rb", src)
	e := assertEdgeExists(t, edges, "Function:find_all", "Class:User", "find")
	if e.ORM != "mongodb" {
		t.Errorf("expected orm=mongodb, got %q", e.ORM)
	}
}

func TestDriver_RubyCassandraCQL(t *testing.T) {
	src := `require 'cassandra'
class Events
  def list
    session.execute("SELECT id FROM events WHERE day = ?")
  end
end
`
	edges := detectORM(t, "ruby", "events.rb", src)
	e := assertEdgeExists(t, edges, "Function:list", "Class:Event", "find")
	if e.ORM != "cassandra" {
		t.Errorf("expected orm=cassandra, got %q", e.ORM)
	}
}

func TestDriver_RubyDynamoTableName(t *testing.T) {
	src := `require 'aws-sdk-dynamodb'
class Store
  def get
    dynamodb.get_item(table_name: 'Products', key: { id: 1 })
  end
end
`
	edges := detectORM(t, "ruby", "store.rb", src)
	e := assertEdgeExists(t, edges, "Function:get", "Class:Product", "find")
	if e.ORM != "dynamodb" {
		t.Errorf("expected orm=dynamodb, got %q", e.ORM)
	}
}

func TestDriver_RubyElasticIndex(t *testing.T) {
	src := `require 'elasticsearch'
class LogSearch
  def search
    client = Elasticsearch::Client.new
    client.search(index: 'logs', body: { query: { match_all: {} } })
  end
end
`
	edges := detectORM(t, "ruby", "log_search.rb", src)
	e := assertEdgeExists(t, edges, "Function:search", "Class:Log", "find")
	if e.ORM != "elastic" {
		t.Errorf("expected orm=elastic, got %q", e.ORM)
	}
}

// Dynamic-index negative: an index name bound to a variable is not a static
// literal, so esIndexKeyRe must not capture it — no hallucinated edge.
func TestDriver_RubyElasticDynamicIndexSkipped(t *testing.T) {
	src := `require 'elasticsearch'
class LogSearch
  def search(idx)
    client = Elasticsearch::Client.new
    client.search(index: idx, body: {})
  end
end
`
	edges := detectORM(t, "ruby", "log_search.rb", src)
	for _, e := range edges {
		if e.ORM == "elastic" {
			t.Errorf("expected no elastic edge for dynamic index, got %+v", e)
		}
	}
}

// Neo4j Ruby: the language-agnostic datastore-infra pass (scanDatastoreInfraDrivers,
// run for Ruby in applyORMQueries) attributes `session.run("MATCH (n:Label) ...")`
// Cypher to the node label. This asserts the Ruby neo4j-ruby-driver call shape
// genuinely fires the cross-language Cypher emitter — the basis for crediting
// lang.ruby.driver.neo4j query_attribution.
func TestDriver_RubyNeo4jCypher(t *testing.T) {
	src := `require 'neo4j/driver'
class UserGraph
  def find_users
    session.run("MATCH (u:User) RETURN u")
  end
end
`
	edges := detectORM(t, "ruby", "user_graph.rb", src)
	e := assertEdgeExists(t, edges, "Function:find_users", "Class:User", "find")
	if e.ORM != "neo4j" {
		t.Errorf("expected orm=neo4j, got %q", e.ORM)
	}
}
