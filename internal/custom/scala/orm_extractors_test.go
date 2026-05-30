package scala_test

// orm_extractors_test.go — fixture tests for Slick, Doobie, Quill,
// ScalikeJDBC, Scanamo, and elastic4s extractors.

import (
	"testing"
)

// ============================================================================
// Slick
// ============================================================================

func TestSlickTableClassExtraction(t *testing.T) {
	src := `
import slick.jdbc.PostgresProfile.api._

class Users(tag: Tag) extends Table[User](tag, "users") {
  def id   = column[Long]("id", O.PrimaryKey, O.AutoInc)
  def name = column[String]("name")
  def *    = (id, name) <> (User.tupled, User.unapply)
}

val users = TableQuery[Users]
`
	ents := extract(t, "custom_scala_slick", fi("Users.scala", "scala", src))
	if !containsEntity(ents, "SCOPE.Schema", "Users") {
		t.Error("expected Users table class as SCOPE.Schema")
	}
	if !containsEntity(ents, "SCOPE.Schema", "col:id") {
		t.Error("expected col:id column entity")
	}
	if !containsEntity(ents, "SCOPE.Schema", "col:name") {
		t.Error("expected col:name column entity")
	}
	if !containsEntity(ents, "SCOPE.Schema", "query:users") {
		t.Error("expected query:users TableQuery entity")
	}
}

func TestSlickForeignKeyExtraction(t *testing.T) {
	src := `
import slick.jdbc.PostgresProfile.api._

class Orders(tag: Tag) extends Table[Order](tag, "orders") {
  def userId  = column[Long]("user_id")
  def fkUser  = foreignKey("fk_orders_user_id", userId, userTable)(_.id)
}
`
	ents := extract(t, "custom_scala_slick", fi("Orders.scala", "scala", src))
	if !containsEntity(ents, "SCOPE.Schema", "fk:fk_orders_user_id") {
		t.Error("expected fk:fk_orders_user_id foreign key entity")
	}
}

func TestSlickMigrationExtraction(t *testing.T) {
	src := `
import slick.jdbc.PostgresProfile.api._

val setup = DBIO.seq(
  users.schema.create,
  orders.schema.create
)
`
	ents := extract(t, "custom_scala_slick", fi("Migration.scala", "scala", src))
	if !containsEntity(ents, "SCOPE.Schema", "migration:schema_ddl") {
		t.Error("expected migration:schema_ddl entity from DBIO.seq")
	}
}

func TestSlickNoMatch(t *testing.T) {
	src := `object Foo { def bar = 42 }`
	ents := extract(t, "custom_scala_slick", fi("Foo.scala", "scala", src))
	if len(ents) != 0 {
		t.Errorf("expected no entities for non-Slick file, got %d", len(ents))
	}
}

// --- value-asserting deep tests (full bar) ---

func TestSlickSchemaValues(t *testing.T) {
	src := `
import slick.jdbc.PostgresProfile.api._

class Users(tag: Tag) extends Table[User](tag, "app", "users") {
  def id   = column[Long]("id", O.PrimaryKey, O.AutoInc)
  def name = column[String]("full_name")
}
`
	ents := extract(t, "custom_scala_slick", fi("Users.scala", "scala", src))

	tbl := findEntity(ents, "SCOPE.Schema", "Users")
	if tbl == nil {
		t.Fatal("expected Users table entity")
	}
	if tbl.Props["table_name"] != "users" {
		t.Errorf("expected table_name=users, got %q", tbl.Props["table_name"])
	}
	if tbl.Props["row_type"] != "User" {
		t.Errorf("expected row_type=User, got %q", tbl.Props["row_type"])
	}

	id := findEntity(ents, "SCOPE.Schema", "col:id")
	if id == nil {
		t.Fatal("expected col:id entity")
	}
	if id.Props["column_name"] != "id" || id.Props["column_type"] != "Long" {
		t.Errorf("expected column_name=id type=Long, got name=%q type=%q",
			id.Props["column_name"], id.Props["column_type"])
	}
	if id.Props["primary_key"] != "true" || id.Props["auto_inc"] != "true" {
		t.Errorf("expected id primary_key=true auto_inc=true, got pk=%q ai=%q",
			id.Props["primary_key"], id.Props["auto_inc"])
	}

	name := findEntity(ents, "SCOPE.Schema", "col:full_name")
	if name == nil {
		t.Fatal("expected col:full_name entity (SQL column name, not def name)")
	}
	if name.Props["def_name"] != "name" || name.Props["primary_key"] != "false" {
		t.Errorf("expected def_name=name pk=false, got def=%q pk=%q",
			name.Props["def_name"], name.Props["primary_key"])
	}
}

func TestSlickForeignKeyValues(t *testing.T) {
	src := `
import slick.jdbc.PostgresProfile.api._

class Orders(tag: Tag) extends Table[Order](tag, "orders") {
  def userId = column[Long]("user_id")
  def fkUser = foreignKey("fk_orders_user_id", userId, userTable)(_.id)
}
`
	ents := extract(t, "custom_scala_slick", fi("Orders.scala", "scala", src))
	fk := findEntity(ents, "SCOPE.Schema", "fk:fk_orders_user_id")
	if fk == nil {
		t.Fatal("expected fk:fk_orders_user_id entity")
	}
	if fk.Props["local_column"] != "userId" {
		t.Errorf("expected local_column=userId, got %q", fk.Props["local_column"])
	}
	if fk.Props["target_table"] != "userTable" {
		t.Errorf("expected target_table=userTable, got %q", fk.Props["target_table"])
	}
}

func TestSlickMigrationValues(t *testing.T) {
	src := `
import slick.jdbc.PostgresProfile.api._

val setup = DBIO.seq(
  users.schema.create,
  orders.schema.drop
)
`
	ents := extract(t, "custom_scala_slick", fi("Migration.scala", "scala", src))
	mc := findEntity(ents, "SCOPE.Schema", "migration:create:users")
	if mc == nil {
		t.Fatal("expected migration:create:users entity")
	}
	if mc.Props["table_query"] != "users" || mc.Props["ddl_verb"] != "create" {
		t.Errorf("expected table_query=users ddl_verb=create, got tq=%q verb=%q",
			mc.Props["table_query"], mc.Props["ddl_verb"])
	}
	if findEntity(ents, "SCOPE.Schema", "migration:drop:orders") == nil {
		t.Error("expected migration:drop:orders entity")
	}
}

// ============================================================================
// Doobie
// ============================================================================

func TestDoobieSQLFragment(t *testing.T) {
	src := `
import doobie._
import doobie.implicits._

def getUser(id: Long): ConnectionIO[User] =
  sql"SELECT id, name FROM users WHERE id = $id".query[User].unique
`
	ents := extract(t, "custom_scala_doobie", fi("UserRepo.scala", "scala", src))
	found := false
	for _, e := range ents {
		if e.Kind == "SCOPE.Operation" && e.Subtype == "query" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected a SCOPE.Operation/query entity from sql fragment")
	}
}

func TestDoobieRowTypeMapping(t *testing.T) {
	src := `
import doobie._
import doobie.implicits._

case class User(id: Long, name: String)

val q = sql"SELECT id, name FROM users".query[User]
`
	ents := extract(t, "custom_scala_doobie", fi("Repo.scala", "scala", src))
	if !containsEntity(ents, "SCOPE.Schema", "row_type:User") {
		t.Error("expected row_type:User schema entity from .query[User]")
	}
	if !containsEntity(ents, "SCOPE.Schema", "User") {
		t.Error("expected User case class schema entity")
	}
}

func TestDoobieTransactor(t *testing.T) {
	src := `
import doobie._
import doobie.hikari._

val xa = HikariTransactor.newHikariTransactor[IO](
  "org.postgresql.Driver", "jdbc:postgresql://localhost/mydb", "user", "pass"
)
`
	ents := extract(t, "custom_scala_doobie", fi("DB.scala", "scala", src))
	found := false
	for _, e := range ents {
		if e.Kind == "SCOPE.Service" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected SCOPE.Service for Transactor")
	}
}

func TestDoobieNoMatch(t *testing.T) {
	src := `object Foo { def bar = 42 }`
	ents := extract(t, "custom_scala_doobie", fi("Foo.scala", "scala", src))
	if len(ents) != 0 {
		t.Errorf("expected no entities for non-doobie file, got %d", len(ents))
	}
}

// --- value-asserting deep tests (full bar) ---

func TestDoobieSQLTableAndVerb(t *testing.T) {
	src := `
import doobie._
import doobie.implicits._

val q = sql"SELECT id, name FROM users WHERE id = $id".query[User]
val ins = sql"INSERT INTO accounts (id) VALUES ($id)".update
`
	ents := extract(t, "custom_scala_doobie", fi("Repo.scala", "scala", src))

	var sel, ins *entitySummary
	for i := range ents {
		if ents[i].Kind != "SCOPE.Operation" {
			continue
		}
		switch ents[i].Props["sql_verb"] {
		case "select":
			sel = &ents[i]
		case "insert":
			ins = &ents[i]
		}
	}
	if sel == nil {
		t.Fatal("expected a SELECT sql fragment operation")
	}
	if sel.Props["table_name"] != "users" {
		t.Errorf("expected SELECT table_name=users, got %q", sel.Props["table_name"])
	}
	if ins == nil {
		t.Fatal("expected an INSERT sql fragment operation")
	}
	if ins.Props["table_name"] != "accounts" {
		t.Errorf("expected INSERT table_name=accounts, got %q", ins.Props["table_name"])
	}
}

func TestDoobieRowTypeValues(t *testing.T) {
	src := `
import doobie._
case class User(id: Long, name: String)
val q = sql"SELECT id, name FROM users".query[User]
`
	ents := extract(t, "custom_scala_doobie", fi("Repo.scala", "scala", src))
	rt := findEntity(ents, "SCOPE.Schema", "row_type:User")
	if rt == nil || rt.Props["row_type"] != "User" {
		t.Fatalf("expected row_type:User with row_type=User, got %+v", rt)
	}
}

// ============================================================================
// Quill
// ============================================================================

func TestQuillQuerySchema(t *testing.T) {
	src := `
import io.getquill._

val ctx = new PostgresJdbcContext(SnakeCase, "ctx")
import ctx._

case class Person(id: Long, name: String)

val people = querySchema[Person]("persons", _.id -> "person_id")
`
	ents := extract(t, "custom_scala_quill", fi("QuillRepo.scala", "scala", src))
	if !containsEntity(ents, "SCOPE.Schema", "schema:Person") {
		t.Error("expected schema:Person from querySchema[Person]")
	}
}

func TestQuillQuoteBlock(t *testing.T) {
	src := `
import io.getquill._

val ctx = new PostgresJdbcContext(SnakeCase, "ctx")
import ctx._

case class Order(id: Long, total: Double)

val q = quote { query[Order].filter(o => o.total > 100) }
ctx.run(q)
`
	ents := extract(t, "custom_scala_quill", fi("QuillOrders.scala", "scala", src))
	if !containsEntity(ents, "SCOPE.Operation", "quote:Order") {
		t.Error("expected quote:Order operation from quote block")
	}
}

func TestQuillNoMatch(t *testing.T) {
	src := `object Foo { def bar = 42 }`
	ents := extract(t, "custom_scala_quill", fi("Foo.scala", "scala", src))
	if len(ents) != 0 {
		t.Errorf("expected no entities for non-quill file, got %d", len(ents))
	}
}

// --- value-asserting deep tests (full bar) ---

func TestQuillQuerySchemaValues(t *testing.T) {
	src := `
import io.getquill._
case class Person(id: Long, name: String)
val people = querySchema[Person]("persons", _.id -> "person_id", _.name -> "full_name")
`
	ents := extract(t, "custom_scala_quill", fi("QuillRepo.scala", "scala", src))
	sc := findEntity(ents, "SCOPE.Schema", "schema:Person")
	if sc == nil {
		t.Fatal("expected schema:Person entity")
	}
	if sc.Props["table_name"] != "persons" {
		t.Errorf("expected table_name=persons, got %q", sc.Props["table_name"])
	}
	if sc.Props["column_remaps"] != "id=person_id,name=full_name" {
		t.Errorf("expected column_remaps=id=person_id,name=full_name, got %q",
			sc.Props["column_remaps"])
	}
}

func TestQuillJoinAssociation(t *testing.T) {
	src := `
import io.getquill._
case class Person(id: Long)
case class Address(personId: Long)
val q = quote {
  query[Person].join(query[Address]).on((p, a) => p.id == a.personId)
}
`
	ents := extract(t, "custom_scala_quill", fi("Join.scala", "scala", src))
	j := findEntity(ents, "SCOPE.Schema", "join:Address")
	if j == nil || j.Props["joined_entity"] != "Address" {
		t.Fatalf("expected join:Address with joined_entity=Address, got %+v", j)
	}
}

// ============================================================================
// ScalikeJDBC
// ============================================================================

func TestScalikeJDBCSyntaxSupport(t *testing.T) {
	src := `
import scalikejdbc._

case class Member(id: Long, name: String)

object Member extends SQLSyntaxSupport[Member] {
  override val tableName = "members"
  def apply(rs: WrappedResultSet) = new Member(rs.long(1), rs.string(2))
}
`
	ents := extract(t, "custom_scala_scalikejdbc", fi("Member.scala", "scala", src))
	if !containsEntity(ents, "SCOPE.Schema", "Member") {
		t.Error("expected Member schema entity from SQLSyntaxSupport")
	}
}

func TestScalikeJDBCRelationship(t *testing.T) {
	src := `
import scalikejdbc._

object Order extends SQLSyntaxSupport[Order] {
  val defaultJoinAlias = hasManyThrough[OrderItem](
    OrderItem -> OrderItem.defaultAlias,
    (o, items) => o.copy(items = items)
  )
  val owner = belongsTo[User](User -> User.defaultAlias, (o, u) => o.copy(user = u))
}
`
	ents := extract(t, "custom_scala_scalikejdbc", fi("Order.scala", "scala", src))
	found := false
	for _, e := range ents {
		if e.Kind == "SCOPE.Schema" && len(e.Name) > 4 && e.Name[:4] == "rel:" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected rel:... relationship schema entity")
	}
}

func TestScalikeJDBCNoMatch(t *testing.T) {
	src := `object Foo { def bar = 42 }`
	ents := extract(t, "custom_scala_scalikejdbc", fi("Foo.scala", "scala", src))
	if len(ents) != 0 {
		t.Errorf("expected no entities for non-scalikejdbc file, got %d", len(ents))
	}
}

// --- value-asserting deep tests (full bar) ---

func TestScalikeJDBCTableNameValue(t *testing.T) {
	src := `
import scalikejdbc._
case class Member(id: Long, name: String)
object Member extends SQLSyntaxSupport[Member] {
  override val tableName = "members"
}
`
	ents := extract(t, "custom_scala_scalikejdbc", fi("Member.scala", "scala", src))
	m := findEntity(ents, "SCOPE.Schema", "Member")
	if m == nil {
		t.Fatal("expected Member schema entity")
	}
	if m.Props["table_name"] != "members" {
		t.Errorf("expected table_name=members, got %q", m.Props["table_name"])
	}
	if m.Props["model_type"] != "Member" {
		t.Errorf("expected model_type=Member, got %q", m.Props["model_type"])
	}
}

func TestScalikeJDBCRelationshipValues(t *testing.T) {
	src := `
import scalikejdbc._
object Order extends SQLSyntaxSupport[Order] {
  val items = hasMany[OrderItem](OrderItem -> OrderItem.defaultAlias, (o, is) => o.copy(items = is))
  val owner = belongsTo[User](User -> User.defaultAlias, (o, u) => o.copy(user = u))
}
`
	ents := extract(t, "custom_scala_scalikejdbc", fi("Order.scala", "scala", src))
	hm := findEntity(ents, "SCOPE.Schema", "rel:hasMany:OrderItem")
	if hm == nil {
		t.Fatal("expected rel:hasMany:OrderItem entity")
	}
	if hm.Props["rel_kind"] != "hasMany" || hm.Props["target_model"] != "OrderItem" {
		t.Errorf("expected rel_kind=hasMany target_model=OrderItem, got kind=%q target=%q",
			hm.Props["rel_kind"], hm.Props["target_model"])
	}
	if findEntity(ents, "SCOPE.Schema", "rel:belongsTo:User") == nil {
		t.Error("expected rel:belongsTo:User entity")
	}
}

func TestScalikeJDBCSQLAndDDLValues(t *testing.T) {
	src := `
import scalikejdbc._
def setup() = DB autoCommit { implicit s =>
  sql"CREATE TABLE members (id BIGINT)".execute.apply()
}
val q = sql"SELECT id FROM members WHERE id = ?"
`
	ents := extract(t, "custom_scala_scalikejdbc", fi("Setup.scala", "scala", src))

	ddl := findEntity(ents, "SCOPE.Schema", "ddl:create:members")
	if ddl == nil {
		t.Fatal("expected ddl:create:members migration entity")
	}
	if ddl.Props["table_name"] != "members" || ddl.Props["ddl_verb"] != "create" {
		t.Errorf("expected DDL table_name=members verb=create, got table=%q verb=%q",
			ddl.Props["table_name"], ddl.Props["ddl_verb"])
	}

	var sel *entitySummary
	for i := range ents {
		if ents[i].Kind == "SCOPE.Operation" && ents[i].Props["sql_verb"] == "select" {
			sel = &ents[i]
		}
	}
	if sel == nil || sel.Props["table_name"] != "members" {
		t.Fatalf("expected SELECT operation on members, got %+v", sel)
	}
}

// ============================================================================
// Scanamo
// ============================================================================

func TestScanamoTableDef(t *testing.T) {
	src := `
import org.scanamo._
import org.scanamo.syntax._
import software.amazon.awssdk.services.dynamodb.DynamoDbClient

case class Farm(animals: List[Animal])
case class Animal(name: String)

val client = DynamoDbClient.create()
val scanamo = Scanamo(client)
val farmTable = Table[Farm]("farm")
`
	ents := extract(t, "custom_scala_scanamo", fi("FarmRepo.scala", "scala", src))
	if !containsEntity(ents, "SCOPE.Schema", "table:farm") {
		t.Error("expected table:farm schema entity")
	}
	if !containsEntity(ents, "SCOPE.Service", "client:Scanamo") {
		t.Error("expected client:Scanamo service entity")
	}
}

func TestScanamoDynamoFormat(t *testing.T) {
	src := `
import org.scanamo._

case class Fruit(name: String, colour: String)
implicit val fruitFormat: DynamoFormat[Fruit] = DynamoFormat.derived[Fruit]
`
	ents := extract(t, "custom_scala_scanamo", fi("Fruit.scala", "scala", src))
	if !containsEntity(ents, "SCOPE.Schema", "format:Fruit") {
		t.Error("expected format:Fruit schema entity for DynamoFormat[Fruit]")
	}
}

func TestScanamoNoMatch(t *testing.T) {
	src := `object Foo { def bar = 42 }`
	ents := extract(t, "custom_scala_scanamo", fi("Foo.scala", "scala", src))
	if len(ents) != 0 {
		t.Errorf("expected no entities for non-scanamo file, got %d", len(ents))
	}
}

// --- value-asserting deep tests (full bar) ---

func TestScanamoTableValues(t *testing.T) {
	src := `
import org.scanamo._
case class Farm(name: String)
val farmTable = Table[Farm]("farm")
`
	ents := extract(t, "custom_scala_scanamo", fi("FarmRepo.scala", "scala", src))
	tbl := findEntity(ents, "SCOPE.Schema", "table:farm")
	if tbl == nil {
		t.Fatal("expected table:farm entity")
	}
	if tbl.Props["table_name"] != "farm" || tbl.Props["item_type"] != "Farm" {
		t.Errorf("expected table_name=farm item_type=Farm, got name=%q type=%q",
			tbl.Props["table_name"], tbl.Props["item_type"])
	}
}

func TestScanamoDynamoFormatValues(t *testing.T) {
	src := `
import org.scanamo._
case class Fruit(name: String)
implicit val f: DynamoFormat[Fruit] = DynamoFormat.derived[Fruit]
`
	ents := extract(t, "custom_scala_scanamo", fi("Fruit.scala", "scala", src))
	fm := findEntity(ents, "SCOPE.Schema", "format:Fruit")
	if fm == nil || fm.Props["type_name"] != "Fruit" {
		t.Fatalf("expected format:Fruit with type_name=Fruit, got %+v", fm)
	}
}

// ============================================================================
// elastic4s
// ============================================================================

func TestElastic4sIndexCreation(t *testing.T) {
	src := `
import com.sksamuel.elastic4s.ElasticClient
import com.sksamuel.elastic4s.ElasticDsl._

val client = ElasticClient(JavaClient(ElasticProperties("localhost:9200")))
client.execute { createIndex("movies") }
`
	ents := extract(t, "custom_scala_elastic4s", fi("MoviesRepo.scala", "scala", src))
	if !containsEntity(ents, "SCOPE.Service", "elastic_client") {
		t.Error("expected elastic_client service entity")
	}
	if !containsEntity(ents, "SCOPE.Schema", "index:movies") {
		t.Error("expected index:movies schema entity")
	}
}

func TestElastic4sHitReader(t *testing.T) {
	src := `
import com.sksamuel.elastic4s.ElasticDsl._

case class Movie(title: String, year: Int)
implicit val hitReader: HitReader[Movie] = ???
`
	ents := extract(t, "custom_scala_elastic4s", fi("Movie.scala", "scala", src))
	if !containsEntity(ents, "SCOPE.Schema", "hit_type:Movie") {
		t.Error("expected hit_type:Movie schema entity from HitReader[Movie]")
	}
}

func TestElastic4sSearch(t *testing.T) {
	src := `
import com.sksamuel.elastic4s.ElasticDsl._

val resp = client.execute {
  search("movies").query(termQuery("title", "Inception"))
}
`
	ents := extract(t, "custom_scala_elastic4s", fi("Search.scala", "scala", src))
	if !containsEntity(ents, "SCOPE.Operation", "search:movies") {
		t.Error("expected search:movies operation entity")
	}
}

func TestElastic4sNoMatch(t *testing.T) {
	src := `object Foo { def bar = 42 }`
	ents := extract(t, "custom_scala_elastic4s", fi("Foo.scala", "scala", src))
	if len(ents) != 0 {
		t.Errorf("expected no entities for non-elastic4s file, got %d", len(ents))
	}
}

// --- value-asserting deep tests (full bar) ---

func TestElastic4sIndexValues(t *testing.T) {
	src := `
import com.sksamuel.elastic4s.ElasticDsl._
client.execute { createIndex("movies") }
`
	ents := extract(t, "custom_scala_elastic4s", fi("Movies.scala", "scala", src))
	idx := findEntity(ents, "SCOPE.Schema", "index:movies")
	if idx == nil || idx.Props["index_name"] != "movies" {
		t.Fatalf("expected index:movies with index_name=movies, got %+v", idx)
	}
}

func TestElastic4sSearchAndHitValues(t *testing.T) {
	src := `
import com.sksamuel.elastic4s.ElasticDsl._
case class Movie(title: String)
implicit val hr: HitReader[Movie] = ???
val r = client.execute { search("movies").query(termQuery("title", "x")) }
`
	ents := extract(t, "custom_scala_elastic4s", fi("Search.scala", "scala", src))
	s := findEntity(ents, "SCOPE.Operation", "search:movies")
	if s == nil || s.Props["index_name"] != "movies" {
		t.Fatalf("expected search:movies with index_name=movies, got %+v", s)
	}
	h := findEntity(ents, "SCOPE.Schema", "hit_type:Movie")
	if h == nil || h.Props["type_name"] != "Movie" {
		t.Fatalf("expected hit_type:Movie with type_name=Movie, got %+v", h)
	}
}
