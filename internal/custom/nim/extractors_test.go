package nim_test

import (
	"context"
	"strings"
	"testing"

	extreg "github.com/cajasmota/archigraph/internal/extractor"

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
