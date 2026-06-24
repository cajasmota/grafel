package nim_test

import (
	"context"
	"strings"
	"testing"

	extreg "github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"

	_ "github.com/cajasmota/grafel/internal/custom/nim"
)

func fi(path, lang, src string) extreg.FileInput {
	return extreg.FileInput{Path: path, Language: lang, Content: []byte(src)}
}

// TestNimRouteE2E_Capture proves the std/httpclient route helpers are captured
// onto a single test_suite's e2e_route_calls property.
func TestNimRouteE2E_Capture(t *testing.T) {
	src := `
import std/unittest
import std/httpclient

suite "Todos":
  test "lists":
    let client = newHttpClient()
    discard client.get("http://localhost:8080/todos")
  test "shows one":
    let client = newHttpClient()
    discard client.get(baseUrl & "/todos/42")
  test "creates":
    let client = newHttpClient()
    discard client.post("http://localhost:8080/todos", body = "{}")
  test "replaces":
    let client = newHttpClient()
    discard client.request("http://localhost:8080/todos/42", httpMethod = HttpPut)
`
	e, ok := extreg.Get("custom_nim_tests_route_e2e")
	if !ok {
		t.Fatal("custom_nim_tests_route_e2e not registered")
	}
	ents, err := e.Extract(context.Background(), fi("tests/tTodos.nim", "nim", src))
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if len(ents) != 1 {
		t.Fatalf("expected exactly 1 test_suite, got %d", len(ents))
	}
	rec := ents[0]
	if rec.Subtype != "test_suite" {
		t.Errorf("expected test_suite, got %q", rec.Subtype)
	}
	calls := rec.Properties["e2e_route_calls"]
	for _, want := range []string{"GET /todos", "GET /todos/42", "POST /todos", "PUT /todos/42"} {
		if !strings.Contains(calls, want) {
			t.Errorf("expected route call %q in %q", want, calls)
		}
	}
}

// TestNimRouteE2E_NonTestExcluded proves a non-test file (production route
// registration) is NOT captured as a test_suite.
func TestNimRouteE2E_NonTestExcluded(t *testing.T) {
	src := `
import jester
routes:
  get "/todos":
    resp "ok"
`
	e, _ := extreg.Get("custom_nim_tests_route_e2e")
	ents, _ := e.Extract(context.Background(), fi("src/routes.nim", "nim", src))
	if len(ents) != 0 {
		t.Fatalf("expected no test_suite for a non-test file, got %d", len(ents))
	}
}

// TestNimRouteE2E_ShapeOnlyTestExcluded proves a unit test that never hits a
// route emits no suite.
func TestNimRouteE2E_ShapeOnlyTestExcluded(t *testing.T) {
	src := `
import std/unittest

suite "Todo":
  test "validates title":
    let t = newTodo("")
    check(not t.valid())
`
	e, _ := extreg.Get("custom_nim_tests_route_e2e")
	ents, _ := e.Extract(context.Background(), fi("tests/tTodo.nim", "nim", src))
	if len(ents) != 0 {
		t.Fatalf("expected no test_suite for a shape-only test, got %d", len(ents))
	}
}

// TestNimRouteE2E_WrongLanguageNoop proves the extractor gates on
// language=="nim".
func TestNimRouteE2E_WrongLanguageNoop(t *testing.T) {
	src := `discard client.get("http://localhost/todos")`
	e, _ := extreg.Get("custom_nim_tests_route_e2e")
	ents, _ := e.Extract(context.Background(), fi("tests/tTodos.nim", "go", src))
	if len(ents) != 0 {
		t.Fatalf("expected no entities for non-nim language, got %d", len(ents))
	}
}

// --- Norm ORM (#4904) -------------------------------------------------------

// TestNimNormORM_ModelTableColumns proves a Norm `ref object of Model`
// declaration synthesises model + table + column SCOPE.Schema entities and an
// FK edge for a field typed as another model.
func TestNimNormORM_ModelTableColumns(t *testing.T) {
	src := `
import norm/model
import norm/sqlite

type
  User* = ref object of Model
    name*: string
    email*: string
    age*: int

  Post* = ref object of Model
    title*: string
    body*: string
    author*: User
`
	e, ok := extreg.Get("custom_nim_norm_orm")
	if !ok {
		t.Fatal("custom_nim_norm_orm not registered")
	}
	ents, err := e.Extract(context.Background(), fi("src/models.nim", "nim", src))
	if err != nil {
		t.Fatalf("extract: %v", err)
	}

	type key struct{ name, sub string }
	got := map[key]bool{}
	for _, en := range ents {
		if en.Kind != "SCOPE.Schema" {
			t.Errorf("unexpected kind %q for %q", en.Kind, en.Name)
			continue
		}
		if en.Properties["framework"] != "norm" {
			t.Errorf("entity %q missing framework=norm", en.Name)
		}
		got[key{en.Name, en.Subtype}] = true
	}

	// Models + tables.
	for _, m := range []string{"User", "Post"} {
		if !got[key{m, "model"}] {
			t.Errorf("expected SCOPE.Schema/model %q", m)
		}
		if !got[key{m, "table"}] {
			t.Errorf("expected SCOPE.Schema/table %q", m)
		}
	}
	// Columns.
	for _, c := range []string{"name", "email", "age", "title", "body", "author"} {
		if !got[key{c, "column"}] {
			t.Errorf("expected SCOPE.Schema/column %q", c)
		}
	}

	// FK edge: Post.author → User.
	fkFound := false
	authorFKCol := false
	for _, en := range ents {
		if en.Name == "Post" && en.Subtype == "model" {
			for _, r := range en.Relationships {
				if r.Kind == "REFERENCES" && r.ToID == "User" && r.Properties["fk_field"] == "author" {
					fkFound = true
				}
			}
		}
		if en.Name == "author" && en.Subtype == "column" {
			if en.Properties["foreign_key"] == "true" && en.Properties["column_type"] == "User" {
				authorFKCol = true
			}
		}
	}
	if !fkFound {
		t.Error("expected REFERENCES edge Post→User (fk_field=author)")
	}
	if !authorFKCol {
		t.Error("expected author column stamped foreign_key=true column_type=User")
	}
}

// TestNimNormORM_NonModelNoop proves a plain (non-Model) object is ignored.
func TestNimNormORM_NonModelNoop(t *testing.T) {
	src := `
type
  Config = object
    host: string
    port: int
`
	e, _ := extreg.Get("custom_nim_norm_orm")
	ents, _ := e.Extract(context.Background(), fi("src/config.nim", "nim", src))
	if len(ents) != 0 {
		t.Fatalf("expected no schema entities for a non-Model object, got %d", len(ents))
	}
}

// TestNimNormORM_WrongLanguageNoop gates on language=="nim".
func TestNimNormORM_WrongLanguageNoop(t *testing.T) {
	src := `type User* = ref object of Model
  name*: string`
	e, _ := extreg.Get("custom_nim_norm_orm")
	ents, _ := e.Extract(context.Background(), fi("src/models.nim", "go", src))
	if len(ents) != 0 {
		t.Fatalf("expected no entities for non-nim language, got %d", len(ents))
	}
}

// TestNimNormORM_Deepen_4932 proves the #4932 deepening: a {.tableName.}
// override keys the table entity and stamps table_name on the model; column
// pragmas ({.unique.}, {.dbType.}) stamp the column; an explicit {.fk: User.}
// pragma on a scalar field yields a REFERENCES edge + foreign_key column;
// db.<op>(Model) call sites yield QUERIES edges model→table per operation; and a
// db.transaction: block yields a SCOPE.Pattern/transaction_boundary.
func TestNimNormORM_Deepen_4932(t *testing.T) {
	src := `
import norm/model
import norm/sqlite

type
  User* {.tableName: "users".} = ref object of Model
    name* {.unique.}: string
    bio* {.dbType: "TEXT".}: string

  Post* = ref object of Model
    title*: string
    authorId* {.fk: User.}: int64

proc store(db: DbConn, p: Post) =
  db.transaction:
    db.insert(Post)
    db.select(User, "name = ?", "x")
    db.update(Post)
`
	e, _ := extreg.Get("custom_nim_norm_orm")
	ents, err := e.Extract(context.Background(), fi("src/models.nim", "nim", src))
	if err != nil {
		t.Fatalf("extract: %v", err)
	}

	var (
		userModel, postModel, usersTable, nameCol, bioCol, authorCol, txn *recView
	)
	views := make([]recView, len(ents))
	for i, en := range ents {
		views[i] = recView{en.Name, en.Subtype, en.Kind, en.Properties, en.Relationships}
	}
	pick := func(name, sub string) *recView {
		for i := range views {
			if views[i].name == name && views[i].sub == sub {
				return &views[i]
			}
		}
		return nil
	}
	userModel = pick("User", "model")
	postModel = pick("Post", "model")
	usersTable = pick("users", "table")
	nameCol = pick("name", "column")
	bioCol = pick("bio", "column")
	authorCol = pick("authorId", "column")
	txn = pick("db.transaction", "transaction_boundary")

	// Table-name override.
	if usersTable == nil {
		t.Error("expected SCOPE.Schema/table keyed by override name \"users\"")
	}
	if userModel == nil || userModel.props["table_name"] != "users" {
		t.Error("expected User model stamped table_name=users")
	}

	// Column pragmas.
	if nameCol == nil || nameCol.props["unique"] != "true" {
		t.Error("expected name column stamped unique=true")
	}
	if bioCol == nil || bioCol.props["db_type"] != "TEXT" {
		t.Error("expected bio column stamped db_type=TEXT")
	}

	// Explicit {.fk: User.} pragma on a scalar field.
	if authorCol == nil || authorCol.props["foreign_key"] != "true" || authorCol.props["fk_target"] != "User" {
		t.Error("expected authorId column foreign_key=true fk_target=User from {.fk: User.}")
	}
	fkEdge := false
	if postModel != nil {
		for _, r := range postModel.rels {
			if r.Kind == "REFERENCES" && r.ToID == "User" && r.Properties["fk_pragma"] == "true" {
				fkEdge = true
			}
		}
	}
	if !fkEdge {
		t.Error("expected REFERENCES Post→User (fk_pragma=true)")
	}

	// Query attribution: Post got insert+update → table Post; User got select → users.
	postOps := map[string]bool{}
	if postModel != nil {
		for _, r := range postModel.rels {
			if r.Kind == "QUERIES" {
				postOps[r.Properties["operation"]] = true
			}
		}
	}
	if !postOps["insert"] || !postOps["update"] {
		t.Errorf("expected Post QUERIES insert+update, got %v", postOps)
	}
	userSelect := false
	if userModel != nil {
		for _, r := range userModel.rels {
			if r.Kind == "QUERIES" && r.Properties["operation"] == "select" && r.ToID == "users" {
				userSelect = true
			}
		}
	}
	if !userSelect {
		t.Error("expected User QUERIES select → table users")
	}

	// Transaction boundary.
	if txn == nil || txn.kind != "SCOPE.Pattern" || txn.props["transactional"] != "true" || txn.props["framework"] != "norm" {
		t.Error("expected SCOPE.Pattern/transaction_boundary transactional=true framework=norm")
	}
}

// --- Norm migrations + lifecycle + cross-file/variable-handle (#4991) --------

// TestNimNormMigrations_4991 proves Norm schema-migration ops: a model-typed
// createTables/dropTables (`db.createTables(User())`), a variable-handle
// createTables resolved via its `var u = User()` binding, and raw-DDL
// db.exec(sql"CREATE/ALTER/DROP TABLE …") strings each yield a shared
// SCOPE.Evolution migration-op entity (framework=norm) the engine pass converges.
func TestNimNormMigrations_4991(t *testing.T) {
	src := `
import norm/[model, sqlite]

type
  User* = ref object of Model
    name*: string
  Post* = ref object of Model
    title*: string

proc setup(db: DbConn) =
  var u = User()
  db.createTables(u)
  db.createTables(Post())
  db.dropTables(Post())
  db.exec(sql"CREATE TABLE audit (id INTEGER PRIMARY KEY)")
  db.exec(sql"ALTER TABLE user ADD COLUMN bio TEXT")
  db.exec(sql"DROP TABLE audit")
`
	e, ok := extreg.Get("custom_nim_norm_migrations")
	if !ok {
		t.Fatal("custom_nim_norm_migrations not registered")
	}
	ents, err := e.Extract(context.Background(), fi("src/migrate.nim", "nim", src))
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	got := map[string]string{} // name -> subtype
	for _, en := range ents {
		if en.Kind != "SCOPE.Evolution" || en.Properties["framework"] != "norm" {
			t.Errorf("entity %q: expected SCOPE.Evolution framework=norm, got %s/%s", en.Name, en.Kind, en.Properties["framework"])
		}
		got[en.Name] = en.Subtype
	}
	want := map[string]string{
		"create_table:User":  "create_table", // variable handle u -> User
		"create_table:Post":  "create_table", // Post() constructor
		"drop_table:Post":    "drop_table",
		"create_table:audit": "create_table", // raw DDL
		"alter_table:user":   "alter_table",
		"drop_table:audit":   "drop_table",
	}
	for n, sub := range want {
		if got[n] != sub {
			t.Errorf("expected migration op %q subtype %q, got %q (all=%v)", n, sub, got[n], got)
		}
	}
}

// TestNimNormMigrations_NoMatchNoop proves a Norm file with no schema ops yields
// no migration entities, and TestNimNormMigrations_WrongLanguageNoop gates lang.
func TestNimNormMigrations_NoMatchNoop(t *testing.T) {
	src := `
import norm/model
type User* = ref object of Model
  name*: string
`
	e, _ := extreg.Get("custom_nim_norm_migrations")
	ents, _ := e.Extract(context.Background(), fi("src/models.nim", "nim", src))
	if len(ents) != 0 {
		t.Fatalf("expected no migration entities for a model-only file, got %d", len(ents))
	}
}

func TestNimNormMigrations_WrongLanguageNoop(t *testing.T) {
	src := "db.createTables(User())"
	e, _ := extreg.Get("custom_nim_norm_migrations")
	ents, _ := e.Extract(context.Background(), fi("src/migrate.nim", "go", src))
	if len(ents) != 0 {
		t.Fatalf("expected no entities for non-nim language, got %d", len(ents))
	}
}

// TestNimNormQuery_VariableHandleAndRawSQL_4991 proves the deepened query
// attribution: a variable-handle query `db.select(u, …)` (u bound to User) and a
// raw-SQL query `db.select(objs, sql"… FROM posts …")` are both attributed to
// their model, not just the model-typed first-arg form.
func TestNimNormQuery_VariableHandleAndRawSQL_4991(t *testing.T) {
	src := `
import norm/[model, sqlite]

type
  User* = ref object of Model
    name*: string
  Post* = ref object of Model
    title*: string

proc run(db: DbConn) =
  var u = User()
  db.select(u, "name = ?", "x")
  db.update(u)
  var objs = @[Post()]
  db.select(objs, sql"SELECT * FROM posts WHERE title = ?", "t")
`
	e, _ := extreg.Get("custom_nim_norm_orm")
	ents, err := e.Extract(context.Background(), fi("src/q.nim", "nim", src))
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	ops := map[string]map[string]bool{} // model -> ops via QUERIES edges
	for _, en := range ents {
		if en.Subtype != "model" {
			continue
		}
		for _, r := range en.Relationships {
			if r.Kind == "QUERIES" {
				if ops[en.Name] == nil {
					ops[en.Name] = map[string]bool{}
				}
				ops[en.Name][r.Properties["operation"]] = true
			}
		}
	}
	if !ops["User"]["select"] || !ops["User"]["update"] {
		t.Errorf("expected User QUERIES select+update via variable handle, got %v", ops["User"])
	}
	if !ops["Post"]["select"] {
		t.Errorf("expected Post QUERIES select via raw-SQL FROM posts, got %v", ops["Post"])
	}
}

// TestNimNormTransaction_EnclosingProcAndWrites_4991 proves the transaction
// boundary is stamped with its enclosing proc and the write ops issued inside it.
func TestNimNormTransaction_EnclosingProcAndWrites_4991(t *testing.T) {
	src := `
import norm/[model, sqlite]

type
  Post* = ref object of Model
    title*: string

proc savePost(db: DbConn, p: Post) =
  db.transaction:
    db.insert(p)
    db.update(p)
`
	e, _ := extreg.Get("custom_nim_norm_orm")
	ents, _ := e.Extract(context.Background(), fi("src/tx.nim", "nim", src))
	var txn *recView
	views := viewsOf(ents)
	for i := range views {
		if views[i].sub == "transaction_boundary" {
			txn = &views[i]
		}
	}
	if txn == nil {
		t.Fatal("expected a transaction_boundary entity")
	}
	if txn.props["enclosing_proc"] != "savePost" {
		t.Errorf("expected enclosing_proc=savePost, got %q", txn.props["enclosing_proc"])
	}
	if txn.props["has_writes"] != "true" || txn.props["writes"] != "insert,update" {
		t.Errorf("expected writes=insert,update has_writes=true, got writes=%q has_writes=%q", txn.props["writes"], txn.props["has_writes"])
	}
}

// --- Allographer schema builder (#4933) -------------------------------------

// TestNimAllographerORM_TablesColumnsFK proves an Allographer
// schema().create(table("...", [Column()...])) declaration synthesises table +
// column SCOPE.Schema entities (framework=allographer), stamps column_type from
// the builder method, captures unique/nullable modifiers, and yields a
// REFERENCES edge table->referenced-table for a .foreign().reference().on()
// chain.
func TestNimAllographerORM_TablesColumnsFK(t *testing.T) {
	src := `
import allographer/schema_builder

schema().create(
  table("users", [
    Column().increments("id"),
    Column().string("name"),
    Column().string("email").unique(),
    Column().integer("age").nullable(),
  ]),
  table("posts", [
    Column().increments("id"),
    Column().string("title"),
    Column().foreign("user_id").reference("id").on("users").onDelete(SET_NULL),
  ]),
)
`
	e, ok := extreg.Get("custom_nim_allographer_orm")
	if !ok {
		t.Fatal("custom_nim_allographer_orm not registered")
	}
	ents, err := e.Extract(context.Background(), fi("src/schema.nim", "nim", src))
	if err != nil {
		t.Fatalf("extract: %v", err)
	}

	views := make([]recView, len(ents))
	for i, en := range ents {
		if en.Kind != "SCOPE.Schema" {
			t.Errorf("unexpected kind %q for %q", en.Kind, en.Name)
		}
		if en.Properties["framework"] != "allographer" {
			t.Errorf("entity %q missing framework=allographer", en.Name)
		}
		views[i] = recView{en.Name, en.Subtype, en.Kind, en.Properties, en.Relationships}
	}
	pick := func(name, sub string) *recView {
		for i := range views {
			if views[i].name == name && views[i].sub == sub {
				return &views[i]
			}
		}
		return nil
	}

	// Tables.
	for _, tbl := range []string{"users", "posts"} {
		if pick(tbl, "table") == nil {
			t.Errorf("expected SCOPE.Schema/table %q", tbl)
		}
	}
	// Columns + column_type.
	if c := pick("name", "column"); c == nil || c.props["column_type"] != "string" || c.props["table"] != "users" {
		t.Error("expected users.name column column_type=string")
	}
	if c := pick("id", "column"); c == nil || c.props["column_type"] != "increments" {
		t.Error("expected id column column_type=increments")
	}
	// Modifiers.
	if c := pick("email", "column"); c == nil || c.props["unique"] != "true" {
		t.Error("expected email column unique=true")
	}
	if c := pick("age", "column"); c == nil || c.props["nullable"] != "true" {
		t.Error("expected age column nullable=true")
	}
	// FK column + edge.
	fkCol := pick("user_id", "column")
	if fkCol == nil || fkCol.props["foreign_key"] != "true" || fkCol.props["fk_target"] != "users" || fkCol.props["fk_column"] != "id" {
		t.Errorf("expected user_id column foreign_key=true fk_target=users fk_column=id, got %+v", fkCol)
	}
	fkEdge := false
	if pt := pick("posts", "table"); pt != nil {
		for _, r := range pt.rels {
			if r.Kind == "REFERENCES" && r.ToID == "users" && r.Properties["fk_field"] == "user_id" && r.Properties["references"] == "id" {
				fkEdge = true
			}
		}
	}
	if !fkEdge {
		t.Error("expected REFERENCES edge posts->users (fk_field=user_id, references=id)")
	}
}

// TestNimAllographerORM_NonSchemaNoop proves an arbitrary Nim file with neither
// a schema builder nor Column() calls is ignored.
func TestNimAllographerORM_NonSchemaNoop(t *testing.T) {
	src := `
proc table(name: string) = discard
echo "no columns here"
`
	e, _ := extreg.Get("custom_nim_allographer_orm")
	ents, _ := e.Extract(context.Background(), fi("src/util.nim", "nim", src))
	if len(ents) != 0 {
		t.Fatalf("expected no schema entities for a non-Allographer file, got %d", len(ents))
	}
}

// TestNimAllographerORM_WrongLanguageNoop gates on language=="nim".
func TestNimAllographerORM_WrongLanguageNoop(t *testing.T) {
	src := `schema().create(table("users", [Column().string("name")]))`
	e, _ := extreg.Get("custom_nim_allographer_orm")
	ents, _ := e.Extract(context.Background(), fi("src/schema.nim", "go", src))
	if len(ents) != 0 {
		t.Fatalf("expected no entities for non-nim language, got %d", len(ents))
	}
}

// recView is a flattened entity view for table-driven assertions.
type recView struct {
	name, sub, kind string
	props           map[string]string
	rels            []types.RelationshipRecord
}

func viewsOf(ents []types.EntityRecord) []recView {
	views := make([]recView, len(ents))
	for i, en := range ents {
		views[i] = recView{en.Name, en.Subtype, en.Kind, en.Properties, en.Relationships}
	}
	return views
}

func pickView(views []recView, name, sub string) *recView {
	for i := range views {
		if views[i].name == name && views[i].sub == sub {
			return &views[i]
		}
	}
	return nil
}

// --- Debby ORM (#5028) ------------------------------------------------------

// TestNimDebbyORM_ModelTableColumns proves a plain Debby `object` registered via
// a Debby db op synthesises model + table + column SCOPE.Schema entities
// (framework=debby), an FK edge for a field typed as another registered model and
// for an explicit {.fk.} pragma, and QUERIES edges for insert/get usage.
func TestNimDebbyORM_ModelTableColumns(t *testing.T) {
	src := `
import debby/sqlite

type
  User = object
    id: int
    name: string
    email: string

  Post = object
    id: int
    title: string
    author: User
    authorId {.fk: User.}: int

db.createTable(User)
db.createTable(Post)
discard db.insert(Post)
discard db.get(User, 1)
`
	e, ok := extreg.Get("custom_nim_debby_orm")
	if !ok {
		t.Fatal("custom_nim_debby_orm not registered")
	}
	ents, err := e.Extract(context.Background(), fi("src/models.nim", "nim", src))
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	views := viewsOf(ents)
	for _, en := range ents {
		if en.Kind != "SCOPE.Schema" {
			t.Errorf("unexpected kind %q for %q", en.Kind, en.Name)
		}
		if en.Properties["framework"] != "debby" {
			t.Errorf("entity %q missing framework=debby", en.Name)
		}
	}

	for _, m := range []string{"User", "Post"} {
		if pickView(views, m, "model") == nil {
			t.Errorf("expected SCOPE.Schema/model %q", m)
		}
		if pickView(views, m, "table") == nil {
			t.Errorf("expected SCOPE.Schema/table %q", m)
		}
	}
	for _, c := range []string{"id", "name", "email", "title", "author", "authorId"} {
		if pickView(views, c, "column") == nil {
			t.Errorf("expected SCOPE.Schema/column %q", c)
		}
	}

	// FK edge for the model-typed field author -> User.
	post := pickView(views, "Post", "model")
	if post == nil {
		t.Fatal("missing Post model")
	}
	fkTyped, fkPragma, postInsert := false, false, false
	for _, r := range post.rels {
		if r.Kind == "REFERENCES" && r.ToID == "User" && r.Properties["fk_field"] == "author" {
			fkTyped = true
		}
		if r.Kind == "REFERENCES" && r.ToID == "User" && r.Properties["fk_field"] == "authorId" && r.Properties["fk_pragma"] == "true" {
			fkPragma = true
		}
		if r.Kind == "QUERIES" && r.Properties["operation"] == "insert" {
			postInsert = true
		}
	}
	if !fkTyped {
		t.Error("expected REFERENCES Post->User (fk_field=author)")
	}
	if !fkPragma {
		t.Error("expected REFERENCES Post->User (fk_field=authorId, fk_pragma=true)")
	}
	if !postInsert {
		t.Error("expected Post QUERIES insert")
	}

	// author column stamped foreign_key.
	if c := pickView(views, "author", "column"); c == nil || c.props["foreign_key"] != "true" || c.props["fk_target"] != "User" {
		t.Error("expected author column foreign_key=true fk_target=User")
	}

	// User got a get query edge.
	user := pickView(views, "User", "model")
	userGet := false
	if user != nil {
		for _, r := range user.rels {
			if r.Kind == "QUERIES" && r.Properties["operation"] == "get" {
				userGet = true
			}
		}
	}
	if !userGet {
		t.Error("expected User QUERIES get")
	}
}

// TestNimDebbyORM_UnregisteredObjectNoop proves a plain object that is never
// registered/used with a Debby db op is NOT treated as a model.
func TestNimDebbyORM_UnregisteredObjectNoop(t *testing.T) {
	src := `
import debby/sqlite

type
  User = object
    id: int
    name: string

  Config = object
    host: string

db.createTable(User)
`
	e, _ := extreg.Get("custom_nim_debby_orm")
	ents, _ := e.Extract(context.Background(), fi("src/models.nim", "nim", src))
	for _, en := range ents {
		if en.Name == "Config" {
			t.Error("expected unregistered Config object to be ignored")
		}
	}
	if pickView(viewsOf(ents), "User", "model") == nil {
		t.Error("expected registered User to be a model")
	}
}

// TestNimDebbyORM_NoDebbyNoop proves a file without Debby is ignored.
func TestNimDebbyORM_NoDebbyNoop(t *testing.T) {
	src := `
type User = object
  id: int
`
	e, _ := extreg.Get("custom_nim_debby_orm")
	ents, _ := e.Extract(context.Background(), fi("src/models.nim", "nim", src))
	if len(ents) != 0 {
		t.Fatalf("expected no entities for a non-Debby file, got %d", len(ents))
	}
}

// TestNimDebbyORM_WrongLanguageNoop gates on language=="nim".
func TestNimDebbyORM_WrongLanguageNoop(t *testing.T) {
	src := `import debby/sqlite
type User = object
  id: int
db.createTable(User)`
	e, _ := extreg.Get("custom_nim_debby_orm")
	ents, _ := e.Extract(context.Background(), fi("src/models.nim", "go", src))
	if len(ents) != 0 {
		t.Fatalf("expected no entities for non-nim language, got %d", len(ents))
	}
}

// --- ormin ORM (#5028) ------------------------------------------------------

// TestNimOrminORM_ImportModel proves a Nim `importModel(DbBackend.x, "file")`
// call yields a SCOPE.Schema/model_import entity stamping backend + model_file.
func TestNimOrminORM_ImportModel(t *testing.T) {
	src := `
import ormin

importModel(DbBackend.postgre, "model")
`
	e, ok := extreg.Get("custom_nim_ormin_orm")
	if !ok {
		t.Fatal("custom_nim_ormin_orm not registered")
	}
	ents, err := e.Extract(context.Background(), fi("src/db.nim", "nim", src))
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	imp := pickView(viewsOf(ents), "model", "model_import")
	if imp == nil {
		t.Fatal("expected SCOPE.Schema/model_import keyed by model file")
	}
	if imp.props["framework"] != "ormin" || imp.props["backend"] != "postgre" || imp.props["model_file"] != "model" {
		t.Errorf("expected model_import framework=ormin backend=postgre model_file=model, got %+v", imp.props)
	}
}

// TestNimOrminORM_SQLSchema proves an ormin SQL DSL `create table` file
// synthesises table + column entities (framework=ormin), stamps column_type,
// primary_key / not_null, and yields a REFERENCES edge for an inline
// `references Other(col)` foreign key.
func TestNimOrminORM_SQLSchema(t *testing.T) {
	src := `
create table User(
  id integer primary key,
  name string not null,
  email string not null
);

create table Post(
  id integer primary key,
  title string not null,
  author integer references User(id)
);
`
	e, _ := extreg.Get("custom_nim_ormin_orm")
	ents, err := e.Extract(context.Background(), fi("model.sql", "sql", src))
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	views := viewsOf(ents)
	for _, en := range ents {
		if en.Properties["framework"] != "ormin" {
			t.Errorf("entity %q missing framework=ormin", en.Name)
		}
	}
	for _, tbl := range []string{"User", "Post"} {
		if pickView(views, tbl, "table") == nil {
			t.Errorf("expected SCOPE.Schema/table %q", tbl)
		}
	}
	if c := pickView(views, "name", "column"); c == nil || c.props["column_type"] != "string" || c.props["table"] != "User" || c.props["not_null"] != "true" {
		t.Errorf("expected name column column_type=string not_null=true table=User, got %+v", c)
	}
	if c := pickView(views, "id", "column"); c == nil || c.props["primary_key"] != "true" {
		t.Error("expected id column primary_key=true")
	}
	// FK column + edge.
	fkCol := pickView(views, "author", "column")
	if fkCol == nil || fkCol.props["foreign_key"] != "true" || fkCol.props["fk_target"] != "User" || fkCol.props["fk_column"] != "id" {
		t.Errorf("expected author column foreign_key=true fk_target=User fk_column=id, got %+v", fkCol)
	}
	fkEdge := false
	if pt := pickView(views, "Post", "table"); pt != nil {
		for _, r := range pt.rels {
			if r.Kind == "REFERENCES" && r.ToID == "User" && r.Properties["fk_field"] == "author" && r.Properties["references"] == "id" {
				fkEdge = true
			}
		}
	}
	if !fkEdge {
		t.Error("expected REFERENCES edge Post->User (fk_field=author, references=id)")
	}
}

// TestNimOrminORM_NonSQLNoop proves a non-.sql file without importModel is
// ignored, and a plain Nim file without ormin is ignored.
func TestNimOrminORM_NonSQLNoop(t *testing.T) {
	e, _ := extreg.Get("custom_nim_ormin_orm")
	// A .sql file with no create table.
	ents, _ := e.Extract(context.Background(), fi("data.sql", "sql", "select 1;"))
	if len(ents) != 0 {
		t.Fatalf("expected no entities for a non-DDL sql file, got %d", len(ents))
	}
	// A Nim file with no ormin import.
	ents2, _ := e.Extract(context.Background(), fi("src/x.nim", "nim", "echo \"hi\""))
	if len(ents2) != 0 {
		t.Fatalf("expected no entities for a non-ormin nim file, got %d", len(ents2))
	}
}

// TestNimNormORM_OptionWrappedFK proves an Option[Model]/seq[Model] field is
// unwrapped and recognised as a foreign key.
func TestNimNormORM_OptionWrappedFK(t *testing.T) {
	src := `
import norm/model
import std/options

type
  User* = ref object of Model
    name*: string

  Comment* = ref object of Model
    text*: string
    author*: Option[User]
`
	e, _ := extreg.Get("custom_nim_norm_orm")
	ents, err := e.Extract(context.Background(), fi("src/models.nim", "nim", src))
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	fk := false
	for _, en := range ents {
		if en.Name == "Comment" && en.Subtype == "model" {
			for _, r := range en.Relationships {
				if r.Kind == "REFERENCES" && r.ToID == "User" {
					fk = true
				}
			}
		}
	}
	if !fk {
		t.Error("expected Option[User] field to yield REFERENCES Comment→User")
	}
}

// --- Allographer alter()/drop() migrations (#5029) --------------------------

// TestNimAllographerMigrations_AlterDropOps proves Allographer schema().alter()
// and schema().drop() ops synthesise SCOPE.Evolution migration-op entities
// (framework=allographer) with the normalised op subtype + table/column props
// the engine migration-schema-ops pass keys on.
func TestNimAllographerMigrations_AlterDropOps(t *testing.T) {
	src := `
import allographer/schema_builder

schema().alter(
  table("users").add(Column().string("bio")),
  table("users").change(Column().string("name")),
  table("users").renameColumn("name", "full_name"),
  table("posts").deleteColumn("legacy"),
  renameTable("posts", "articles"),
)

schema().drop("comments")
schema().drop(table("tags"))
`
	e, ok := extreg.Get("custom_nim_allographer_migrations")
	if !ok {
		t.Fatal("custom_nim_allographer_migrations not registered")
	}
	ents, err := e.Extract(context.Background(), fi("src/migrate.nim", "nim", src))
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	views := viewsOf(ents)
	for _, en := range ents {
		if en.Kind != "SCOPE.Evolution" {
			t.Errorf("unexpected kind %q for %q", en.Kind, en.Name)
		}
		if en.Properties["framework"] != "allographer" {
			t.Errorf("entity %q missing framework=allographer", en.Name)
		}
	}

	// add_column users.bio
	if v := pickView(views, "add_column:users.bio", "add_column"); v == nil ||
		v.props["table"] != "users" || v.props["column"] != "bio" || v.props["migration_op"] != "add_column" {
		t.Errorf("expected add_column users.bio, got %+v", v)
	}
	// alter_column users.name (change)
	if v := pickView(views, "alter_column:users.name", "alter_column"); v == nil ||
		v.props["table"] != "users" || v.props["column"] != "name" {
		t.Errorf("expected alter_column users.name, got %+v", v)
	}
	// rename_column users.name
	if v := pickView(views, "rename_column:users.name", "rename_column"); v == nil ||
		v.props["table"] != "users" || v.props["column"] != "name" {
		t.Errorf("expected rename_column users.name, got %+v", v)
	}
	// drop_column posts.legacy
	if v := pickView(views, "drop_column:posts.legacy", "drop_column"); v == nil ||
		v.props["table"] != "posts" || v.props["column"] != "legacy" {
		t.Errorf("expected drop_column posts.legacy, got %+v", v)
	}
	// rename_table posts
	if v := pickView(views, "rename_table:posts", "rename_table"); v == nil ||
		v.props["table"] != "posts" {
		t.Errorf("expected rename_table posts, got %+v", v)
	}
	// drop_table comments (string form) + tags (table()-wrapped form)
	if v := pickView(views, "drop_table:comments", "drop_table"); v == nil ||
		v.props["table"] != "comments" {
		t.Errorf("expected drop_table comments, got %+v", v)
	}
	if v := pickView(views, "drop_table:tags", "drop_table"); v == nil ||
		v.props["table"] != "tags" {
		t.Errorf("expected drop_table tags, got %+v", v)
	}
}

// TestNimAllographerMigrations_FKAndColumnTypeAndDynamicTable proves the #5111
// follow-up: (1) an FK chain added/dropped inside alter() yields a REFERENCES
// edge + foreign_key props (add) or a drop_foreign op (drop); (2) add()/change()
// re-extract the new column TYPE into new_column_type; (3) a const/let/var-bound
// string-literal table name in drop(IDENT)/table(IDENT) is resolved to the
// literal, while a truly dynamic (unbound) identifier is skipped.
func TestNimAllographerMigrations_FKAndColumnTypeAndDynamicTable(t *testing.T) {
	src := `
import allographer/schema_builder

const usersTbl = "users"
let postsTbl = "posts"

schema().alter(
  table("posts").add(Column().integer("author_id").foreign("author_id").reference("id").on("users")),
  table("posts").change(Column().string("title")),
  table(postsTbl).dropForeign("author_id"),
)

schema().drop(usersTbl)
schema().drop(unknownDynamicTbl)
`
	e, ok := extreg.Get("custom_nim_allographer_migrations")
	if !ok {
		t.Fatal("custom_nim_allographer_migrations not registered")
	}
	ents, err := e.Extract(context.Background(), fi("src/migrate.nim", "nim", src))
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	views := viewsOf(ents)

	// (1a) add_column posts.author_id with FK -> users REFERENCES edge + props.
	v := pickView(views, "add_column:posts.author_id", "add_column")
	if v == nil {
		t.Fatal("expected add_column posts.author_id op")
	}
	if v.props["foreign_key"] != "true" || v.props["fk_target"] != "users" || v.props["fk_column"] != "id" {
		t.Errorf("expected FK props (foreign_key/fk_target=users/fk_column=id), got %+v", v.props)
	}
	if v.props["new_column_type"] != "integer" {
		t.Errorf("expected new_column_type=integer, got %q", v.props["new_column_type"])
	}
	foundRef := false
	for _, r := range v.rels {
		if r.Kind == "REFERENCES" && r.ToID == "users" &&
			r.Properties["fk_field"] == "author_id" && r.Properties["references"] == "id" {
			foundRef = true
		}
	}
	if !foundRef {
		t.Errorf("expected REFERENCES edge posts->users (fk_field=author_id, references=id), got %+v", v.rels)
	}

	// (1b) drop_foreign posts.author_id op.
	if v := pickView(views, "drop_foreign:posts.author_id", "drop_foreign"); v == nil ||
		v.props["table"] != "posts" || v.props["column"] != "author_id" {
		t.Errorf("expected drop_foreign posts.author_id, got %+v", v)
	}

	// (2) change() re-extracts new column type.
	if v := pickView(views, "alter_column:posts.title", "alter_column"); v == nil ||
		v.props["new_column_type"] != "string" {
		t.Errorf("expected alter_column posts.title new_column_type=string, got %+v", v)
	}

	// (3) dynamic table name bound to a const string literal is resolved.
	if v := pickView(views, "drop_table:users", "drop_table"); v == nil ||
		v.props["table"] != "users" {
		t.Errorf("expected drop_table users (resolved from const usersTbl), got %+v", v)
	}
	// (3b) an unbound dynamic identifier yields NO op (no fabrication).
	for _, en := range ents {
		if en.Properties["table"] == "unknownDynamicTbl" {
			t.Errorf("unbound dynamic table name should be skipped, got entity %q", en.Name)
		}
	}
}

// TestNimAllographerMigrations_NonMigrationNoop proves a create-only schema (no
// alter/drop) and arbitrary Nim are ignored by the migration extractor.
func TestNimAllographerMigrations_NonMigrationNoop(t *testing.T) {
	e, _ := extreg.Get("custom_nim_allographer_migrations")
	createOnly := `
import allographer/schema_builder
schema().create(table("users", [Column().string("name")]))
`
	if ents, _ := e.Extract(context.Background(), fi("src/schema.nim", "nim", createOnly)); len(ents) != 0 {
		t.Fatalf("expected no migration entities for a create-only schema, got %d", len(ents))
	}
	arbitrary := `proc alter() = discard` + "\n" + `echo "no schema here"`
	if ents, _ := e.Extract(context.Background(), fi("src/util.nim", "nim", arbitrary)); len(ents) != 0 {
		t.Fatalf("expected no entities for arbitrary nim, got %d", len(ents))
	}
}

// TestNimAllographerMigrations_WrongLanguageNoop gates on language=="nim".
func TestNimAllographerMigrations_WrongLanguageNoop(t *testing.T) {
	src := `schema().drop("users")`
	e, _ := extreg.Get("custom_nim_allographer_migrations")
	if ents, _ := e.Extract(context.Background(), fi("src/migrate.nim", "go", src)); len(ents) != 0 {
		t.Fatalf("expected no entities for non-nim language, got %d", len(ents))
	}
}

// --- Allographer rdb() query-builder attribution (#5030) --------------------

// TestNimAllographerQuery_AttributesOps proves rdb().table("t")...<op>() chains
// synthesise a SCOPE.Schema/table (framework=allographer) carrying a QUERIES edge
// table->table per distinct operation (select/insert/update/delete), and stamps
// transaction=true on queries inside an rdb().transaction(...) block.
func TestNimAllographerQuery_AttributesOps(t *testing.T) {
	src := `
import allographer/query_builder

let users = rdb().table("users").select("id", "name").where("age", ">", 18).get()
let first = rdb().table("users").where("id", "=", 1).first()
discard rdb().table("users").insert(%*{"name": "Ada"})
discard rdb().table("posts").where("id", "=", 1).update(%*{"title": "x"})
discard rdb().table("posts").where("draft", "=", true).delete()

rdb().transaction(proc() =
  discard rdb().table("accounts").where("id", "=", 1).update(%*{"bal": 0})
  discard rdb().table("ledger").insert(%*{"acct": 1})
)
`
	e, ok := extreg.Get("custom_nim_allographer_query")
	if !ok {
		t.Fatal("custom_nim_allographer_query not registered")
	}
	ents, err := e.Extract(context.Background(), fi("src/repo.nim", "nim", src))
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	views := viewsOf(ents)
	for _, en := range ents {
		// SCOPE.Schema tables carry QUERIES edges; #5116 also synthesises a
		// standalone SCOPE.Operation/transaction boundary entity per
		// rdb().transaction(...) block.
		if en.Kind != "SCOPE.Schema" && !(en.Kind == "SCOPE.Operation" && en.Subtype == "transaction") {
			t.Errorf("unexpected kind %q for %q", en.Kind, en.Name)
		}
		if en.Properties["framework"] != "allographer" {
			t.Errorf("entity %q missing framework=allographer", en.Name)
		}
	}

	// hasOp asserts a QUERIES edge table->table with operation op (and optional
	// transaction stamp) exists on the table entity.
	hasOp := func(table, op string, txn bool) bool {
		v := pickView(views, table, "table")
		if v == nil {
			return false
		}
		for _, r := range v.rels {
			if r.Kind != "QUERIES" || r.ToID != table {
				continue
			}
			if r.Properties["operation"] != op || r.Properties["table"] != table {
				continue
			}
			if txn != (r.Properties["transaction"] == "true") {
				continue
			}
			return true
		}
		return false
	}

	if !hasOp("users", "select", false) {
		t.Error("expected users QUERIES select")
	}
	if !hasOp("users", "insert", false) {
		t.Error("expected users QUERIES insert")
	}
	if !hasOp("posts", "update", false) {
		t.Error("expected posts QUERIES update")
	}
	if !hasOp("posts", "delete", false) {
		t.Error("expected posts QUERIES delete")
	}
	// Transaction-stamped queries.
	if !hasOp("accounts", "update", true) {
		t.Error("expected accounts QUERIES update transaction=true")
	}
	if !hasOp("ledger", "insert", true) {
		t.Error("expected ledger QUERIES insert transaction=true")
	}
}

// TestNimAllographerQuery_NonQueryNoop proves a schema-only file (no rdb()) and
// arbitrary Nim are ignored by the query extractor.
func TestNimAllographerQuery_NonQueryNoop(t *testing.T) {
	e, _ := extreg.Get("custom_nim_allographer_query")
	schemaOnly := `
import allographer/schema_builder
schema().create(table("users", [Column().string("name")]))
`
	if ents, _ := e.Extract(context.Background(), fi("src/schema.nim", "nim", schemaOnly)); len(ents) != 0 {
		t.Fatalf("expected no query entities for a schema-only file, got %d", len(ents))
	}
	arbitrary := `proc rdb() = discard` + "\n" + `echo "no table here"`
	if ents, _ := e.Extract(context.Background(), fi("src/util.nim", "nim", arbitrary)); len(ents) != 0 {
		t.Fatalf("expected no entities for arbitrary nim, got %d", len(ents))
	}
}

// TestNimAllographerQuery_WrongLanguageNoop gates on language=="nim".
func TestNimAllographerQuery_WrongLanguageNoop(t *testing.T) {
	src := `discard rdb().table("users").get()`
	e, _ := extreg.Get("custom_nim_allographer_query")
	if ents, _ := e.Extract(context.Background(), fi("src/repo.nim", "go", src)); len(ents) != 0 {
		t.Fatalf("expected no entities for non-nim language, got %d", len(ents))
	}
}

// TestNimAllographerQuery_JoinsRawDynamicTxn proves the #5116 deepening:
//
//	(a) join targets get a second QUERIES edge (join=true) against the joined table;
//	(b) raw SQL via rdb().raw("...") attributes its FROM/JOIN/INTO/UPDATE table(s)
//	    (raw=true), op classified from the SQL verb;
//	(c) a .table(ident) bound to a const/let/var string literal resolves to it,
//	    while an unbound identifier is skipped (no fabricated query);
//	(d) a standalone SCOPE.Operation/transaction boundary entity is synthesised
//	    per rdb().transaction(...) block.
func TestNimAllographerQuery_JoinsRawDynamicTxn(t *testing.T) {
	src := `
import allographer/query_builder

const ordersTbl = "orders"

# (a) join target
let rows = rdb().table("users").join("posts", "users.id", "=", "posts.user_id").get()

# (b) raw SQL
let report = rdb().raw("SELECT * FROM analytics JOIN sessions ON analytics.sid = sessions.id").get()
discard rdb().raw("INSERT INTO audit_log (msg) VALUES ('x')")

# (c) dynamic table (bound) + unbound (skipped)
let ords = rdb().table(ordersTbl).get()
let mystery = rdb().table(unboundVar).get()

# (d) transaction boundary entity
rdb().transaction(proc() =
  discard rdb().table("accounts").where("id", "=", 1).update(%*{"bal": 0})
)
`
	e, _ := extreg.Get("custom_nim_allographer_query")
	ents, err := e.Extract(context.Background(), fi("src/repo.nim", "nim", src))
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	views := viewsOf(ents)

	// (a) join: users primary select + posts join=true select.
	hasEdge := func(table string, want map[string]string) bool {
		v := pickView(views, table, "table")
		if v == nil {
			return false
		}
		for _, r := range v.rels {
			if r.Kind != "QUERIES" || r.ToID != table {
				continue
			}
			ok := true
			for k, val := range want {
				if r.Properties[k] != val {
					ok = false
					break
				}
			}
			if ok {
				return true
			}
		}
		return false
	}

	if !hasEdge("users", map[string]string{"operation": "select", "table": "users"}) {
		t.Error("(a) expected users primary select")
	}
	if !hasEdge("posts", map[string]string{"operation": "select", "join": "true"}) {
		t.Error("(a) expected posts join=true select")
	}

	// (b) raw SQL: analytics + sessions (select, raw) and audit_log (insert, raw).
	if !hasEdge("analytics", map[string]string{"operation": "select", "raw": "true"}) {
		t.Error("(b) expected analytics select raw=true")
	}
	if !hasEdge("sessions", map[string]string{"operation": "select", "raw": "true"}) {
		t.Error("(b) expected sessions select raw=true (raw JOIN)")
	}
	if !hasEdge("audit_log", map[string]string{"operation": "insert", "raw": "true"}) {
		t.Error("(b) expected audit_log insert raw=true")
	}

	// (c) dynamic table: orders resolved from const binding; unboundVar skipped.
	if !hasEdge("orders", map[string]string{"operation": "select", "table": "orders"}) {
		t.Error("(c) expected orders select (resolved from const binding)")
	}
	if pickView(views, "unboundVar", "table") != nil {
		t.Error("(c) expected unboundVar to be skipped (no fabricated query)")
	}

	// (d) standalone transaction-boundary entity + accounts stamped transaction=true.
	var txnEnt *recView
	for i := range views {
		if views[i].kind == "SCOPE.Operation" && views[i].sub == "transaction" {
			txnEnt = &views[i]
			break
		}
	}
	if txnEnt == nil {
		t.Fatal("(d) expected a standalone SCOPE.Operation/transaction entity")
	}
	if txnEnt.props["framework"] != "allographer" || txnEnt.props["transaction"] != "true" {
		t.Errorf("(d) transaction entity missing framework/transaction props: %v", txnEnt.props)
	}
	if !hasEdge("accounts", map[string]string{"operation": "update", "transaction": "true"}) {
		t.Error("(d) expected accounts update transaction=true (enclosed query stamp preserved)")
	}
}

// TestNimAllographerQuery_RawNoMatchNoop proves an rdb().raw(...) whose SQL has no
// parseable FROM/JOIN/INTO/UPDATE target yields no fabricated table, and a file
// with no rdb() at all is ignored even if it mentions raw/join tokens.
func TestNimAllographerQuery_RawNoMatchNoop(t *testing.T) {
	e, _ := extreg.Get("custom_nim_allographer_query")
	// rdb().raw with no table-bearing clause -> no tables, no txn entity.
	noTable := `
import allographer/query_builder
discard rdb().raw("PRAGMA foreign_keys = ON")
`
	if ents, _ := e.Extract(context.Background(), fi("src/a.nim", "nim", noTable)); len(ents) != 0 {
		t.Fatalf("expected no entities for tableless raw SQL, got %d", len(ents))
	}
	// No rdb() head at all (pre-filter miss).
	noRdb := `let s = "SELECT * FROM users join posts"` + "\n" + `echo s`
	if ents, _ := e.Extract(context.Background(), fi("src/b.nim", "nim", noRdb)); len(ents) != 0 {
		t.Fatalf("expected no entities for non-rdb file, got %d", len(ents))
	}
}

// ---------------------------------------------------------------------------
// Debby migrations + transactions (#5367)
// ---------------------------------------------------------------------------

// TestNimDebbyMigrations proves createTable/dropTable(Model) emit SCOPE.Evolution
// migration ops (framework=debby) targeting the model name (= Debby's table).
func TestNimDebbyMigrations(t *testing.T) {
	src := `
import debby/sqlite

type
  User = object
    id: int
    name: string
  Post = object
    id: int
    title: string

proc setup(db: Db) =
  db.createTable(User)
  db.createTable(Post)
  db.dropTable(Post)
`
	e, ok := extreg.Get("custom_nim_debby_migrations")
	if !ok {
		t.Fatal("custom_nim_debby_migrations not registered")
	}
	ents, err := e.Extract(context.Background(), fi("src/migrate.nim", "nim", src))
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	got := map[string]string{}
	for _, en := range ents {
		if en.Kind != "SCOPE.Evolution" {
			continue
		}
		if en.Properties["framework"] != "debby" {
			t.Errorf("entity %q: framework=%q want debby", en.Name, en.Properties["framework"])
		}
		got[en.Name] = en.Subtype
	}
	want := map[string]string{
		"create_table:User": "create_table",
		"create_table:Post": "create_table",
		"drop_table:Post":   "drop_table",
	}
	for n, sub := range want {
		if got[n] != sub {
			t.Errorf("expected migration op %q subtype %q, got %q (all=%v)", n, sub, got[n], got)
		}
	}
}

// TestNimDebbyTransactions proves db.withTransaction: blocks emit a
// SCOPE.Pattern/transaction_boundary with the in-block writes stamped.
func TestNimDebbyTransactions(t *testing.T) {
	src := `
import debby/sqlite

proc work(db: Db) =
  db.withTransaction:
    db.insert(user)
    db.update(post)
`
	e, _ := extreg.Get("custom_nim_debby_migrations")
	ents, _ := e.Extract(context.Background(), fi("src/tx.nim", "nim", src))
	var txn *types.EntityRecord
	for i := range ents {
		if ents[i].Kind == "SCOPE.Pattern" && ents[i].Subtype == "transaction_boundary" {
			txn = &ents[i]
		}
	}
	if txn == nil {
		t.Fatal("expected a transaction_boundary entity")
	}
	if txn.Properties["framework"] != "debby" {
		t.Errorf("framework=%q want debby", txn.Properties["framework"])
	}
	if txn.Properties["transactional"] != "true" {
		t.Error("expected transactional=true")
	}
	if txn.Properties["db_handle"] != "db" {
		t.Errorf("db_handle=%q want db", txn.Properties["db_handle"])
	}
	if w := txn.Properties["writes"]; !strings.Contains(w, "insert") || !strings.Contains(w, "update") {
		t.Errorf("writes=%q want insert+update", w)
	}
}

func TestNimDebbyMigrations_NoDebbyNoop(t *testing.T) {
	src := "proc x() =\n  db.createTable(User)\n"
	e, _ := extreg.Get("custom_nim_debby_migrations")
	if ents, _ := e.Extract(context.Background(), fi("src/x.nim", "nim", src)); len(ents) != 0 {
		t.Fatalf("expected no entities without a debby import marker, got %d", len(ents))
	}
}

func TestNimDebbyMigrations_WrongLanguageNoop(t *testing.T) {
	src := "import debby/sqlite\ndb.createTable(User)"
	e, _ := extreg.Get("custom_nim_debby_migrations")
	if ents, _ := e.Extract(context.Background(), fi("src/x.nim", "go", src)); len(ents) != 0 {
		t.Fatalf("expected no entities for non-nim language, got %d", len(ents))
	}
}

// ---------------------------------------------------------------------------
// ormin query DSL attribution (#5367)
// ---------------------------------------------------------------------------

// TestNimOrminQueries proves the ormin `query:` DSL emits QUERIES edges with the
// verb→operation mapping and the table target.
func TestNimOrminQueries(t *testing.T) {
	src := `
import ormin
importModel(DbBackend.postgre, "model")

proc fetch() =
  let users = query:
    select id, name
    from User
    where id == 1
  let p = query:
    insert into Post
  query:
    update User
  discard users
`
	e, _ := extreg.Get("custom_nim_ormin_orm")
	ents, err := e.Extract(context.Background(), fi("src/queries.nim", "nim", src))
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	var q *types.EntityRecord
	for i := range ents {
		if ents[i].Kind == "SCOPE.Schema" && ents[i].Subtype == "query" {
			q = &ents[i]
		}
	}
	if q == nil {
		t.Fatal("expected a SCOPE.Schema/query entity")
	}
	got := map[string]string{} // table -> operation
	for _, r := range q.Relationships {
		if r.Kind == "QUERIES" {
			got[r.Properties["table"]] = r.Properties["operation"]
		}
	}
	want := map[string]string{
		"User": "update", // last-write-wins is fine; both select+update target User
		"Post": "insert",
	}
	if got["Post"] != want["Post"] {
		t.Errorf("Post operation=%q want insert (all=%v)", got["Post"], got)
	}
	// User must be attributed (select and/or update); assert it exists.
	if _, ok := got["User"]; !ok {
		t.Errorf("expected a QUERIES edge to User (all=%v)", got)
	}
}

// ---------------------------------------------------------------------------
// Jester / Prologue middleware coverage (#5367)
// ---------------------------------------------------------------------------

// TestNimJesterFilters proves Jester before:/after: filter blocks emit
// SCOPE.Pattern middleware entities with the phase + path scope stamped.
func TestNimJesterFilters(t *testing.T) {
	src := `
import jester

routes:
  before:
    resp Http200, "hi"
  before re"/admin/.*":
    checkAuth()
  get "/":
    resp "home"
  after:
    logRequest()
`
	e, ok := extreg.Get("custom_nim_jester_middleware")
	if !ok {
		t.Fatal("custom_nim_jester_middleware not registered")
	}
	ents, err := e.Extract(context.Background(), fi("src/app.nim", "nim", src))
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	var before, after, scoped int
	for _, en := range ents {
		if en.Kind != "SCOPE.Pattern" || en.Properties["framework"] != "jester" {
			continue
		}
		switch en.Properties["phase"] {
		case "before":
			before++
			if en.Properties["path_scope"] == "/admin/.*" {
				scoped++
			}
		case "after":
			after++
		}
	}
	if before != 2 {
		t.Errorf("expected 2 before filters, got %d", before)
	}
	if after != 1 {
		t.Errorf("expected 1 after filter, got %d", after)
	}
	if scoped != 1 {
		t.Errorf("expected 1 path-scoped before filter, got %d", scoped)
	}
}

// TestNimPrologueMiddleware proves app.use(...) and newApp(middlewares = @[...])
// emit middleware entities (framework=prologue) gated to *Middleware names.
func TestNimPrologueMiddleware(t *testing.T) {
	src := `
import prologue

let app = newApp(settings = settings, middlewares = @[debugRequestMiddleware()])
app.use(staticFileMiddleware("static"))
app.use(@[corsMiddleware(), sessionMiddleware()])
app.use(plainHelper())
`
	e, _ := extreg.Get("custom_nim_jester_middleware")
	ents, _ := e.Extract(context.Background(), fi("src/main.nim", "nim", src))
	got := map[string]bool{}
	for _, en := range ents {
		if en.Properties["framework"] == "prologue" {
			got[en.Properties["middleware_type"]] = true
		}
	}
	for _, want := range []string{"debugRequestMiddleware", "staticFileMiddleware", "corsMiddleware", "sessionMiddleware"} {
		if !got[want] {
			t.Errorf("expected prologue middleware %q (all=%v)", want, got)
		}
	}
	// The honest *Middleware gate must reject a plain helper call.
	if got["plainHelper"] {
		t.Error("plainHelper is not middleware and must not be emitted")
	}
}

func TestNimJesterMiddleware_WrongLanguageNoop(t *testing.T) {
	src := "routes:\n  before:\n    x()"
	e, _ := extreg.Get("custom_nim_jester_middleware")
	if ents, _ := e.Extract(context.Background(), fi("src/x.nim", "go", src)); len(ents) != 0 {
		t.Fatalf("expected no entities for non-nim language, got %d", len(ents))
	}
}
