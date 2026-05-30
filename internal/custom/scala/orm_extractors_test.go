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
