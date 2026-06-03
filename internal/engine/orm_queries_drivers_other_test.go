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
