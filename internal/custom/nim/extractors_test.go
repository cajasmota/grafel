package nim_test

import (
	"context"
	"strings"
	"testing"

	extreg "github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"

	_ "github.com/cajasmota/archigraph/internal/custom/nim"
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
