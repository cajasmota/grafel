// Value-asserting tests for the datastore-infra resource attribution pass
// (Neo4j / ClickHouse / Snowflake — sibling parity with cassandra/mongo/etc).
//
// Each test feeds a realistic driver call shape and asserts the EXACT resource
// (graph node label / table) the call touches surfaces as a QUERIES edge target
// `Class:<Resource>` from the connecting function. These are NOT len>0 smoke
// tests: both the dependent (FromID) and the resource (ToID) are the
// load-bearing claims. Negative tests prove the pass does not hallucinate a
// resource for an unrelated image/string or an unparseable runtime query.
package engine

import "testing"

// ---------------------------------------------------------------------------
// Neo4j — bolt:// driver + session.run("MATCH (n:Label) ...")
// ---------------------------------------------------------------------------

func TestInfra_Neo4jPythonBoltSessionRun(t *testing.T) {
	src := `from neo4j import GraphDatabase

driver = GraphDatabase.driver("bolt://graphdb:7687", auth=("neo4j", "pw"))

def list_people():
    with driver.session() as session:
        return session.run("MATCH (p:Person) RETURN p")
`
	edges := detectORM(t, "python", "graph/people.py", src)
	// Resource node = the Person graph label; dependent = list_people.
	e := assertEdgeExists(t, edges, "Function:list_people", "Class:Person", "find")
	if e.ORM != "neo4j" {
		t.Errorf("expected orm=neo4j, got %q", e.ORM)
	}
}

func TestInfra_Neo4jGoSessionRunCreate(t *testing.T) {
	src := `package store

import "github.com/neo4j/neo4j-go-driver/v5/neo4j"

func CreateMovie(session neo4j.Session) {
	session.Run("CREATE (m:Movie {title: $title}) RETURN m", nil)
}
`
	edges := detectORM(t, "go", "store/movie.go", src)
	e := assertEdgeExists(t, edges, "Function:CreateMovie", "Class:Movie", "create")
	if e.ORM != "neo4j" {
		t.Errorf("expected orm=neo4j, got %q", e.ORM)
	}
}

func TestInfra_Neo4jJavaGraphDatabase(t *testing.T) {
	src := `import org.neo4j.driver.GraphDatabase;
public class GraphRepo {
    public void load() {
        session.run("MATCH (u:User)-[:FOLLOWS]->(f:User) RETURN f");
    }
}
`
	edges := detectORM(t, "java", "GraphRepo.java", src)
	// First node label in the pattern is the attributed resource.
	e := assertEdgeExists(t, edges, "Function:load", "Class:User", "find")
	if e.ORM != "neo4j" {
		t.Errorf("expected orm=neo4j, got %q", e.ORM)
	}
}

func TestInfra_Neo4jNoDriverNoEmit(t *testing.T) {
	// A `session.run(...)` with NO neo4j/bolt import must NOT fire — the
	// import gate keeps the broad `.run(` surface from over-firing.
	src := `def go():
    session.run("MATCH (p:Person) RETURN p")
`
	edges := detectORM(t, "python", "runner.py", src)
	for _, e := range edges {
		if e.ORM == "neo4j" {
			t.Errorf("expected no neo4j edge without driver import, got %+v", e)
		}
	}
}

// ---------------------------------------------------------------------------
// ClickHouse — clickhouse-driver client.execute("SELECT ... FROM events")
// ---------------------------------------------------------------------------

func TestInfra_ClickHousePythonExecute(t *testing.T) {
	src := `from clickhouse_driver import Client

client = Client(host="clickhouse")

def recent_events():
    return client.execute("SELECT event_id, ts FROM events WHERE ts > now() - 3600")
`
	edges := detectORM(t, "python", "analytics/events.py", src)
	e := assertEdgeExists(t, edges, "Function:recent_events", "Class:Event", "find")
	if e.ORM != "clickhouse" {
		t.Errorf("expected orm=clickhouse, got %q", e.ORM)
	}
}

func TestInfra_ClickHouseGoQuery(t *testing.T) {
	src := `package metrics

import _ "github.com/ClickHouse/clickhouse-go/v2"

func CountHits(db *sql.DB) {
	db.Query("SELECT count() FROM page_hits WHERE day = ?")
}
`
	edges := detectORM(t, "go", "metrics/hits.go", src)
	e := assertEdgeExists(t, edges, "Function:CountHits", "Class:Page_hit", "find")
	if e.ORM != "clickhouse" {
		t.Errorf("expected orm=clickhouse, got %q", e.ORM)
	}
}

func TestInfra_ClickHouseNoClientNoEmit(t *testing.T) {
	// An execute() call with no clickhouse signal must not be attributed to
	// clickhouse (it would be a plain SQL driver handled elsewhere).
	src := `def q():
    conn.execute("SELECT id FROM widgets")
`
	edges := detectORM(t, "python", "plain.py", src)
	for _, e := range edges {
		if e.ORM == "clickhouse" {
			t.Errorf("expected no clickhouse edge without client import, got %+v", e)
		}
	}
}

// ---------------------------------------------------------------------------
// Snowflake — snowflake-connector cursor.execute("SELECT ... FROM orders")
// ---------------------------------------------------------------------------

func TestInfra_SnowflakePythonCursorExecute(t *testing.T) {
	src := `import snowflake.connector

conn = snowflake.connector.connect(account="acme.snowflakecomputing.com")

def daily_orders():
    cur = conn.cursor()
    return cur.execute("SELECT order_id, total FROM orders WHERE day = current_date")
`
	edges := detectORM(t, "python", "warehouse/orders.py", src)
	e := assertEdgeExists(t, edges, "Function:daily_orders", "Class:Order", "find")
	if e.ORM != "snowflake" {
		t.Errorf("expected orm=snowflake, got %q", e.ORM)
	}
}

func TestInfra_SnowflakeSQLAlchemyDialectInsert(t *testing.T) {
	src := `from sqlalchemy import create_engine

engine = create_engine("snowflake://user:pw@acme/db/schema")

def load_fact():
    conn.execute("INSERT INTO sales_fact (amount) VALUES (1)")
`
	edges := detectORM(t, "python", "etl/load.py", src)
	e := assertEdgeExists(t, edges, "Function:load_fact", "Class:Sales_fact", "create")
	if e.ORM != "snowflake" {
		t.Errorf("expected orm=snowflake, got %q", e.ORM)
	}
}

func TestInfra_SnowflakeNoConnectorNoEmit(t *testing.T) {
	src := `def q():
    cur.execute("SELECT id FROM orders")
`
	edges := detectORM(t, "python", "plain.py", src)
	for _, e := range edges {
		if e.ORM == "snowflake" {
			t.Errorf("expected no snowflake edge without connector import, got %+v", e)
		}
	}
}

// ---------------------------------------------------------------------------
// Negative: an unrelated string in a clickhouse/snowflake file must not
// produce a resource when the SQL carries no parseable table.
// ---------------------------------------------------------------------------

func TestInfra_UnparseableQuerySkipped(t *testing.T) {
	src := `from clickhouse_driver import Client
client = Client(host="clickhouse")

def ping():
    client.execute("SELECT 1")
`
	edges := detectORM(t, "python", "health.py", src)
	for _, e := range edges {
		if e.ORM == "clickhouse" {
			t.Errorf("expected no clickhouse resource for tableless SELECT 1, got %+v", e)
		}
	}
}
