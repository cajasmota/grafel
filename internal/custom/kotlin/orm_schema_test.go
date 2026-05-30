package kotlin_test

// orm_schema_test.go — tests for Exposed, Ktorm, Room, and SQLDelight
// schema extractors.
//
// Issue #3275 — Part of Kotlin routing + ORM-depth builds.

import (
	"testing"
)

// ---------------------------------------------------------------------------
// Exposed
// ---------------------------------------------------------------------------

func TestExposed_TableAndColumns(t *testing.T) {
	src := `
import org.jetbrains.exposed.sql.*

object Users : Table() {
    val id = integer("id").autoIncrement()
    val name = varchar("name", 50)
    val email = varchar("email", 200)
}
`
	ents := extract(t, "custom_kotlin_exposed_schema", fi("Users.kt", "kotlin", src))
	if !containsEntity(ents, "SCOPE.Schema", "Users") {
		t.Error("exposed: expected Users table entity")
	}
	if !containsEntity(ents, "SCOPE.Schema", "name") {
		t.Error("exposed: expected 'name' column entity")
	}
	if !containsEntity(ents, "SCOPE.Schema", "email") {
		t.Error("exposed: expected 'email' column entity")
	}
}

func TestExposed_IntIdTable(t *testing.T) {
	src := `
object Products : IntIdTable("products") {
    val title = varchar("title", 100)
    val price = decimal("price", 10, 2)
}
`
	ents := extract(t, "custom_kotlin_exposed_schema", fi("Products.kt", "kotlin", src))
	if !containsEntity(ents, "SCOPE.Schema", "Products") {
		t.Error("exposed: expected Products table from IntIdTable")
	}
	if !containsEntity(ents, "SCOPE.Schema", "title") {
		t.Error("exposed: expected 'title' column")
	}
}

func TestExposed_ForeignKey(t *testing.T) {
	src := `
object Orders : IntIdTable("orders") {
    val userId = reference("user_id", Users)
    val status = varchar("status", 20)
}
`
	ents := extract(t, "custom_kotlin_exposed_schema", fi("Orders.kt", "kotlin", src))
	// Should have a foreign_key relationship.
	hasFk := false
	for _, e := range ents {
		if e.Subtype == "foreign_key" {
			hasFk = true
			break
		}
	}
	if !hasFk {
		t.Errorf("exposed: expected foreign_key entity for reference; got %v", ents)
	}
}

func TestExposed_SchemaUtils(t *testing.T) {
	src := `
fun createTables() {
    SchemaUtils.create(Users, Orders)
}
`
	ents := extract(t, "custom_kotlin_exposed_schema", fi("Schema.kt", "kotlin", src))
	hasMigration := false
	for _, e := range ents {
		if e.Subtype == "migration" {
			hasMigration = true
			break
		}
	}
	if !hasMigration {
		t.Error("exposed: expected migration entity for SchemaUtils.create")
	}
}

func TestExposed_EmptyContent(t *testing.T) {
	ents := extract(t, "custom_kotlin_exposed_schema", fi("Empty.kt", "kotlin", ""))
	if len(ents) != 0 {
		t.Errorf("exposed: expected no entities for empty content, got %d", len(ents))
	}
}

func TestExposed_WrongLanguage(t *testing.T) {
	src := `object Users : Table() { val id = integer("id") }`
	ents := extract(t, "custom_kotlin_exposed_schema", fi("Users.java", "java", src))
	if len(ents) != 0 {
		t.Errorf("exposed: expected no entities for java language, got %d", len(ents))
	}
}

// ---------------------------------------------------------------------------
// Ktorm
// ---------------------------------------------------------------------------

func TestKtorm_TableAndColumns(t *testing.T) {
	src := `
import org.ktorm.schema.*

object Employees : Table<Employee>("t_employee") {
    val id = int("id").primaryKey().bindTo { it.id }
    val name = varchar("name").bindTo { it.name }
    val hireDate = date("hire_date").bindTo { it.hireDate }
}
`
	ents := extract(t, "custom_kotlin_ktorm_schema", fi("Employees.kt", "kotlin", src))
	if !containsEntity(ents, "SCOPE.Schema", "Employees") {
		t.Error("ktorm: expected Employees table entity")
	}
	if !containsEntity(ents, "SCOPE.Schema", "name") {
		t.Error("ktorm: expected 'name' column entity")
	}
}

func TestKtorm_ForeignKey(t *testing.T) {
	src := `
object Departments : Table<Department>("t_department") {
    val id = int("id").primaryKey().bindTo { it.id }
    val name = varchar("name").bindTo { it.name }
}

object Employees : Table<Employee>("t_employee") {
    val id = int("id").primaryKey().bindTo { it.id }
    val deptId = int("dept_id").references(Departments) { it.department }
}
`
	ents := extract(t, "custom_kotlin_ktorm_schema", fi("Schema.kt", "kotlin", src))
	hasFk := false
	for _, e := range ents {
		if e.Subtype == "foreign_key" {
			hasFk = true
			break
		}
	}
	if !hasFk {
		t.Errorf("ktorm: expected foreign_key entity for .references(); got %v", ents)
	}
}

func TestKtorm_NoTableGenericBracket(t *testing.T) {
	// A file with Table<> but no ktorm keyword should not match.
	src := `
class MyGenericTable<T> {
    val items: List<T> = emptyList()
}
`
	ents := extract(t, "custom_kotlin_ktorm_schema", fi("Gen.kt", "kotlin", src))
	// The extractor gates on "Table<" which is present, but the object form won't match.
	tableCount := 0
	for _, e := range ents {
		if e.Subtype == "table" {
			tableCount++
		}
	}
	if tableCount != 0 {
		t.Errorf("ktorm: expected no table entities for non-ktorm file, got %d", tableCount)
	}
}

func TestKtorm_EmptyContent(t *testing.T) {
	ents := extract(t, "custom_kotlin_ktorm_schema", fi("Empty.kt", "kotlin", ""))
	if len(ents) != 0 {
		t.Errorf("ktorm: expected no entities for empty content, got %d", len(ents))
	}
}

// ---------------------------------------------------------------------------
// Room
// ---------------------------------------------------------------------------

func TestRoom_BasicEntity(t *testing.T) {
	src := `
import androidx.room.*

@Entity(tableName = "users")
data class User(
    @PrimaryKey val id: Int,
    val name: String,
    val email: String,
)
`
	ents := extract(t, "custom_kotlin_room_schema", fi("User.kt", "kotlin", src))
	if !containsEntity(ents, "SCOPE.Schema", "users") {
		t.Error("room: expected 'users' table entity")
	}
}

func TestRoom_ForeignKey(t *testing.T) {
	src := `
@Entity(
    tableName = "orders",
    foreignKeys = [ForeignKey(
        entity = User::class,
        parentColumns = ["id"],
        childColumns = ["user_id"]
    )]
)
data class Order(
    @PrimaryKey val id: Int,
    val userId: Int,
)
`
	ents := extract(t, "custom_kotlin_room_schema", fi("Order.kt", "kotlin", src))
	hasFk := false
	for _, e := range ents {
		if e.Subtype == "foreign_key" {
			hasFk = true
			break
		}
	}
	if !hasFk {
		t.Errorf("room: expected foreign_key for ForeignKey(entity=User::class); got %v", ents)
	}
}

func TestRoom_Relation(t *testing.T) {
	src := `
data class UserWithOrders(
    @Embedded val user: User,
    @Relation(
        parentColumn = "id",
        entityColumn = "user_id"
    )
    val orders: List<Order>,
)
`
	ents := extract(t, "custom_kotlin_room_schema", fi("UserWithOrders.kt", "kotlin", src))
	hasAssoc := false
	for _, e := range ents {
		if e.Subtype == "association" {
			hasAssoc = true
			break
		}
	}
	if !hasAssoc {
		t.Errorf("room: expected association entity for @Relation; got %v", ents)
	}
}

func TestRoom_DatabaseVersion(t *testing.T) {
	src := `
@Database(entities = [User::class, Order::class], version = 3)
abstract class AppDatabase : RoomDatabase()
`
	ents := extract(t, "custom_kotlin_room_schema", fi("AppDatabase.kt", "kotlin", src))
	hasMigration := false
	for _, e := range ents {
		if e.Subtype == "migration" {
			hasMigration = true
			break
		}
	}
	if !hasMigration {
		t.Error("room: expected migration entity for @Database version")
	}
}

func TestRoom_ExplicitMigration(t *testing.T) {
	src := `
val MIGRATION_1_2 = object : Migration(1, 2) {
    override fun migrate(database: SupportSQLiteDatabase) {
        database.execSQL("ALTER TABLE users ADD COLUMN bio TEXT")
    }
}
`
	ents := extract(t, "custom_kotlin_room_schema", fi("Migrations.kt", "kotlin", src))
	hasMigration := false
	for _, e := range ents {
		if e.Subtype == "migration" && e.Name == "migration:1_to_2" {
			hasMigration = true
			break
		}
	}
	if !hasMigration {
		t.Errorf("room: expected 'migration:1_to_2' entity; got %v", ents)
	}
}

func TestRoom_EmptyContent(t *testing.T) {
	ents := extract(t, "custom_kotlin_room_schema", fi("Empty.kt", "kotlin", ""))
	if len(ents) != 0 {
		t.Errorf("room: expected no entities for empty content, got %d", len(ents))
	}
}

func TestRoom_WrongLanguage(t *testing.T) {
	src := `@Entity(tableName = "users") data class User(val id: Int)`
	ents := extract(t, "custom_kotlin_room_schema", fi("User.java", "java", src))
	if len(ents) != 0 {
		t.Errorf("room: expected no entities for java language, got %d", len(ents))
	}
}

// ---------------------------------------------------------------------------
// SQLDelight
// ---------------------------------------------------------------------------

func TestSQLDelight_CreateTable(t *testing.T) {
	src := `
CREATE TABLE IF NOT EXISTS users (
    id INTEGER PRIMARY KEY,
    name TEXT NOT NULL,
    email TEXT
);
`
	ents := extract(t, "custom_kotlin_sqldelight_schema", fi("users.sq", "sql", src))
	if !containsEntity(ents, "SCOPE.Schema", "users") {
		t.Error("sqldelight: expected 'users' table entity")
	}
}

func TestSQLDelight_ForeignKey(t *testing.T) {
	src := `
CREATE TABLE orders (
    id INTEGER PRIMARY KEY,
    user_id INTEGER NOT NULL,
    FOREIGN KEY(user_id) REFERENCES users(id)
);
`
	ents := extract(t, "custom_kotlin_sqldelight_schema", fi("orders.sq", "sql", src))
	hasFk := false
	for _, e := range ents {
		if e.Subtype == "foreign_key" {
			hasFk = true
			break
		}
	}
	if !hasFk {
		t.Errorf("sqldelight: expected foreign_key entity; got %v", ents)
	}
}

func TestSQLDelight_AlterTableMigration(t *testing.T) {
	src := `
ALTER TABLE users ADD COLUMN bio TEXT;
`
	ents := extract(t, "custom_kotlin_sqldelight_schema", fi("1.sqm", "sql", src))
	hasMigration := false
	for _, e := range ents {
		if e.Subtype == "migration" {
			hasMigration = true
			break
		}
	}
	if !hasMigration {
		t.Errorf("sqldelight: expected migration entity for ALTER TABLE; got %v", ents)
	}
}

func TestSQLDelight_VersionMarker(t *testing.T) {
	src := `-- migration: 2
ALTER TABLE users RENAME TO user_accounts;
`
	ents := extract(t, "custom_kotlin_sqldelight_schema", fi("2.sqm", "sql", src))
	hasMigration := false
	for _, e := range ents {
		if e.Subtype == "migration" && e.Name == "migration:version:2" {
			hasMigration = true
			break
		}
	}
	if !hasMigration {
		t.Errorf("sqldelight: expected 'migration:version:2' entity; got %v", ents)
	}
}

func TestSQLDelight_EmptyContent(t *testing.T) {
	ents := extract(t, "custom_kotlin_sqldelight_schema", fi("empty.sq", "sql", ""))
	if len(ents) != 0 {
		t.Errorf("sqldelight: expected no entities for empty content, got %d", len(ents))
	}
}

func TestSQLDelight_KotlinFileWithImport(t *testing.T) {
	// A Kotlin file that contains sqldelight references should also trigger.
	src := `
import com.squareup.sqldelight.db.SqlDriver

val db = Database(driver)
`
	ents := extract(t, "custom_kotlin_sqldelight_schema", fi("Db.kt", "kotlin", src))
	// No tables, but the extractor should not panic.
	_ = ents
}
