package kotlin_test

// orm_query_test.go — value-asserting tests for the Kotlin ORM query
// extractor (custom_kotlin_orm_query). Each assertion checks a SPECIFIC
// query name (derived-method name, @Query SQL, named .sq query, or DSL
// operation + table) rather than a bare len>0 count, per the deep-grind bar.
//
// Issue #3433 — Deep-grind Kotlin ORM extraction to the TS/JS bar. Epic #3431.

import (
	"testing"
)

// ---------------------------------------------------------------------------
// Exposed DSL queries
// ---------------------------------------------------------------------------

func TestOrmQuery_ExposedSelectAndWrite(t *testing.T) {
	src := `
import org.jetbrains.exposed.sql.*

fun listUsers() = Users.selectAll().toList()
fun findActive() = Users.select { Users.active eq true }
fun addUser(n: String) = Users.insert { it[name] = n }
fun deactivate(id: Int) = Users.update({ Users.id eq id }) { it[active] = false }
fun purge() = Users.deleteWhere { Users.active eq false }
`
	ents := extract(t, "custom_kotlin_orm_query", fi("UserDao.kt", "kotlin", src))
	for _, want := range []string{
		"exposed:Users.selectAll",
		"exposed:Users.select",
		"exposed:Users.insert",
		"exposed:Users.update",
		"exposed:Users.deleteWhere",
	} {
		if !containsEntity(ents, "SCOPE.Operation", want) {
			t.Errorf("exposed: expected query entity %q; got %v", want, ents)
		}
	}
}

// ---------------------------------------------------------------------------
// Ktorm queries
// ---------------------------------------------------------------------------

func TestOrmQuery_Ktorm(t *testing.T) {
	src := `
import org.ktorm.database.Database
import org.ktorm.dsl.*
import org.ktorm.entity.*

fun load(database: Database) {
    database.from(Employees).select().where { Employees.name eq "x" }
    database.insert(Employees) { set(it.name, "y") }
    database.update(Employees) { set(it.name, "z") }
    database.delete(Departments) { it.id eq 1 }
    database.sequenceOf(Employees).find { it.id eq 1 }
}
`
	ents := extract(t, "custom_kotlin_orm_query", fi("EmployeeDao.kt", "kotlin", src))
	for _, want := range []string{
		"ktorm:from(Employees)",
		"ktorm:insert(Employees)",
		"ktorm:update(Employees)",
		"ktorm:delete(Departments)",
		"ktorm:sequenceOf(Employees)",
	} {
		if !containsEntity(ents, "SCOPE.Operation", want) {
			t.Errorf("ktorm: expected query entity %q; got %v", want, ents)
		}
	}
}

// ---------------------------------------------------------------------------
// Room @Dao queries
// ---------------------------------------------------------------------------

func TestOrmQuery_RoomDao(t *testing.T) {
	src := `
import androidx.room.*

@Dao
interface UserDao {
    @Query("SELECT * FROM users WHERE id = :id")
    suspend fun getById(id: Int): User

    @Insert
    suspend fun insert(user: User)

    @Update
    suspend fun update(user: User)

    @Delete
    suspend fun delete(user: User)
}
`
	ents := extract(t, "custom_kotlin_orm_query", fi("UserDao.kt", "kotlin", src))
	if !containsEntity(ents, "SCOPE.Operation", "room:@Query:SELECT * FROM users WHERE id = :id") {
		t.Errorf("room: expected @Query SQL entity; got %v", ents)
	}
	for _, want := range []string{
		"room:@Insert:insert",
		"room:@Update:update",
		"room:@Delete:delete",
	} {
		if !containsEntity(ents, "SCOPE.Operation", want) {
			t.Errorf("room: expected DAO write entity %q; got %v", want, ents)
		}
	}
}

// ---------------------------------------------------------------------------
// Spring Data repositories + derived queries + @Query
// ---------------------------------------------------------------------------

func TestOrmQuery_SpringDataRepository(t *testing.T) {
	src := `
import org.springframework.data.jpa.repository.JpaRepository
import org.springframework.data.jpa.repository.Query

interface UserRepository : JpaRepository<User, Long> {
    fun findByEmailAndStatus(email: String, status: String): List<User>
    fun countByActive(active: Boolean): Long
    fun existsByUsername(username: String): Boolean

    @Query("SELECT u FROM User u WHERE u.age > :age")
    fun olderThan(age: Int): List<User>
}
`
	ents := extract(t, "custom_kotlin_orm_query", fi("UserRepository.kt", "kotlin", src))
	if !containsEntity(ents, "SCOPE.Operation", "spring-data:UserRepository<User,Long>") {
		t.Errorf("spring-data: expected repository entity; got %v", ents)
	}
	for _, want := range []string{
		"spring-data:derived:findByEmailAndStatus",
		"spring-data:derived:countByActive",
		"spring-data:derived:existsByUsername",
		"spring-data:@Query:SELECT u FROM User u WHERE u.age > :age",
	} {
		if !containsEntity(ents, "SCOPE.Operation", want) {
			t.Errorf("spring-data: expected query entity %q; got %v", want, ents)
		}
	}
}

// ---------------------------------------------------------------------------
// SQLDelight named queries
// ---------------------------------------------------------------------------

func TestOrmQuery_SQLDelightNamed(t *testing.T) {
	src := `CREATE TABLE users (
    id INTEGER PRIMARY KEY,
    name TEXT NOT NULL
);

selectAllUsers:
SELECT * FROM users;

insertUser:
INSERT INTO users(id, name) VALUES (?, ?);

deleteById:
DELETE FROM users WHERE id = ?;
`
	ents := extract(t, "custom_kotlin_orm_query", fi("Users.sq", "sql", src))
	for _, want := range []string{
		"sqldelight:selectAllUsers",
		"sqldelight:insertUser",
		"sqldelight:deleteById",
	} {
		if !containsEntity(ents, "SCOPE.Operation", want) {
			t.Errorf("sqldelight: expected named-query entity %q; got %v", want, ents)
		}
	}
}

func TestOrmQuery_SQLDelightIgnoresNonQueryLabels(t *testing.T) {
	// A bare "word:" line that is not followed by SQL must not be emitted.
	src := `import com.example.Thing
note:
this is not sql
`
	ents := extract(t, "custom_kotlin_orm_query", fi("notes.sq", "sql", src))
	for _, e := range ents {
		if e.Name == "sqldelight:note" {
			t.Errorf("sqldelight: 'note' should not be a query; got %v", ents)
		}
	}
}

// ---------------------------------------------------------------------------
// MongoDB (KMongo + spring-data-mongo)
// ---------------------------------------------------------------------------

func TestOrmQuery_MongoDocumentAndRepo(t *testing.T) {
	src := `
import org.springframework.data.mongodb.core.mapping.Document
import org.springframework.data.mongodb.repository.MongoRepository

@Document("orders")
data class Order(val id: String, val total: Double)

interface OrderRepository : MongoRepository<Order, String> {
    fun findByTotalGreaterThan(total: Double): List<Order>
}
`
	ents := extract(t, "custom_kotlin_orm_query", fi("Order.kt", "kotlin", src))
	if !containsEntity(ents, "SCOPE.Model", "mongodb:@Document:orders") {
		t.Errorf("mongodb: expected @Document collection model; got %v", ents)
	}
	if !containsEntity(ents, "SCOPE.Operation", "mongodb:OrderRepository<Order>") {
		t.Errorf("mongodb: expected MongoRepository entity; got %v", ents)
	}
}

func TestOrmQuery_MongoKMongoOps(t *testing.T) {
	src := `
import org.litote.kmongo.*

class OrderService(private val collection: MongoCollection<Order>) {
    fun byId(id: String) = collection.findOne(Order::id eq id)
    fun all() = collection.find().toList()
    fun add(o: Order) = collection.insertOne(o)
    fun touch(o: Order) = collection.updateOne(Order::id eq o.id, o)
    fun remove(id: String) = collection.deleteOne(Order::id eq id)
}
`
	ents := extract(t, "custom_kotlin_orm_query", fi("OrderService.kt", "kotlin", src))
	for _, want := range []string{
		"mongodb:op:findOne",
		"mongodb:op:find",
		"mongodb:op:insertOne",
		"mongodb:op:updateOne",
		"mongodb:op:deleteOne",
	} {
		if !containsEntity(ents, "SCOPE.Operation", want) {
			t.Errorf("mongodb: expected op entity %q; got %v", want, ents)
		}
	}
}

// ---------------------------------------------------------------------------
// Guards
// ---------------------------------------------------------------------------

func TestOrmQuery_EmptyContent(t *testing.T) {
	ents := extract(t, "custom_kotlin_orm_query", fi("Empty.kt", "kotlin", ""))
	if len(ents) != 0 {
		t.Errorf("expected no entities for empty content, got %d", len(ents))
	}
}

func TestOrmQuery_WrongLanguage(t *testing.T) {
	src := `interface UserRepository extends JpaRepository<User, Long> {}`
	ents := extract(t, "custom_kotlin_orm_query", fi("UserRepository.java", "java", src))
	if len(ents) != 0 {
		t.Errorf("expected no entities for java language, got %d", len(ents))
	}
}

func TestOrmQuery_NoMatch(t *testing.T) {
	src := `fun add(a: Int, b: Int) = a + b`
	ents := extract(t, "custom_kotlin_orm_query", fi("Math.kt", "kotlin", src))
	if len(ents) != 0 {
		t.Errorf("expected no entities, got %d", len(ents))
	}
}
