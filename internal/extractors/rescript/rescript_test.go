package rescript_test

import (
	"context"
	"testing"

	"github.com/cajasmota/grafel/internal/extractor"
	_ "github.com/cajasmota/grafel/internal/extractors/rescript"
	"github.com/cajasmota/grafel/internal/types"
)

// runReScript runs the extractor on raw source and returns entity records.
func runReScript(t *testing.T, src, path string) []types.EntityRecord {
	t.Helper()
	ext, ok := extractor.Get("rescript")
	if !ok {
		t.Fatal("rescript extractor not registered")
	}
	ents, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     path,
		Content:  []byte(src),
		Language: "rescript",
	})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	return ents
}

func rsFind(ents []types.EntityRecord, name, kind string) *types.EntityRecord {
	for i := range ents {
		if ents[i].Name == name && ents[i].Kind == kind {
			return &ents[i]
		}
	}
	return nil
}

func rsHasRel(ents []types.EntityRecord, name, kind, edgeKind, toID string) bool {
	for i := range ents {
		if ents[i].Name != name || ents[i].Kind != kind {
			continue
		}
		for _, r := range ents[i].Relationships {
			if r.Kind == edgeKind && r.ToID == toID {
				return true
			}
		}
	}
	return false
}

// TestReScript_Registered verifies the extractor is in the registry.
func TestReScript_Registered(t *testing.T) {
	_, ok := extractor.Get("rescript")
	if !ok {
		t.Fatal("rescript extractor not registered")
	}
}

// TestReScript_EmptyInput returns zero entities for empty content.
func TestReScript_EmptyInput(t *testing.T) {
	ext, _ := extractor.Get("rescript")
	ents, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "empty.res",
		Content:  []byte{},
		Language: "rescript",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ents) != 0 {
		t.Errorf("expected 0 entities, got %d", len(ents))
	}
}

// TestReScript_ModuleDiscovery verifies module declarations → SCOPE.Component.
func TestReScript_ModuleDiscovery(t *testing.T) {
	src := `
module UserDomain = {
  let create = (name) => { name: name }
}

module OrderService = {
  let process = (order) => order
}
`
	ents := runReScript(t, src, "domain.res")

	wantModules := []string{"UserDomain", "OrderService"}
	for _, name := range wantModules {
		if rsFind(ents, name, "SCOPE.Component") == nil {
			t.Errorf("expected module %q as SCOPE.Component", name)
		}
	}
}

// TestReScript_LetBindings verifies let function bindings → SCOPE.Operation.
func TestReScript_LetBindings(t *testing.T) {
	src := `
open Belt

let add = (a, b) => a + b

let rec factorial = (n) =>
  if n <= 1 {
    1
  } else {
    n * factorial(n - 1)
  }

let processItems = (items) =>
  items->Belt.Array.map(x => x * 2)

let greet: string => string = (name) => "Hello, " ++ name
`
	ents := runReScript(t, src, "utils.res")

	ops := make(map[string]bool)
	for _, e := range ents {
		if e.Kind == "SCOPE.Operation" {
			ops[e.Name] = true
			if e.Language != "rescript" {
				t.Errorf("entity %q: expected Language=rescript, got %q", e.Name, e.Language)
			}
		}
	}

	for _, want := range []string{"add", "factorial", "processItems", "greet"} {
		if !ops[want] {
			t.Errorf("expected function %q to be extracted", want)
		}
	}
}

// TestReScript_TypeDiscovery verifies type declarations → SCOPE.Component.
func TestReScript_TypeDiscovery(t *testing.T) {
	src := `
type userId = string

type user = {
  id: userId,
  name: string,
  age: int,
}

type status =
  | Active
  | Inactive
  | Pending(string)

type result<'a, 'e> =
  | Ok('a)
  | Error('e)
`
	ents := runReScript(t, src, "types.res")

	comps := make(map[string]string)
	for _, e := range ents {
		if e.Kind == "SCOPE.Component" {
			comps[e.Name] = e.Subtype
		}
	}

	for _, want := range []string{"userId", "user", "status", "result"} {
		if _, ok := comps[want]; !ok {
			t.Errorf("expected type %q as SCOPE.Component, got: %v", want, comps)
		} else if comps[want] != "type" {
			t.Errorf("type %q: expected subtype=type, got %q", want, comps[want])
		}
	}
}

// TestReScript_OpenStatements verifies open → IMPORTS edges.
func TestReScript_OpenStatements(t *testing.T) {
	src := `
open Belt
open React
open Js.Promise

let helper = () => ()
`
	ents := runReScript(t, src, "imports.res")

	wantImports := []string{"Belt", "React", "Js.Promise"}
	for _, mod := range wantImports {
		found := false
		for _, e := range ents {
			for _, r := range e.Relationships {
				if r.Kind == "IMPORTS" && r.ToID == mod {
					found = true
					break
				}
			}
			if found {
				break
			}
		}
		if !found {
			t.Errorf("expected IMPORTS edge to %q", mod)
		}
	}
}

// TestReScript_PipeFirstCalls verifies pipe-first `->` chains → CALLS edges.
func TestReScript_PipeFirstCalls(t *testing.T) {
	src := `
open Belt

let transformList = (items) =>
  items
  ->Belt.List.map(x => x * 2)
  ->Belt.List.filter(x => x > 0)
  ->Belt.List.reduce(0, (acc, x) => acc + x)
`
	ents := runReScript(t, src, "pipes.res")

	fn := rsFind(ents, "transformList", "SCOPE.Operation")
	if fn == nil {
		t.Fatal("expected transformList as SCOPE.Operation")
	}

	wantCalls := []string{"Belt.List.map", "Belt.List.filter", "Belt.List.reduce"}
	for _, call := range wantCalls {
		found := false
		for _, r := range fn.Relationships {
			if r.Kind == "CALLS" && r.ToID == call {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected CALLS edge to %q from transformList", call)
		}
	}
}

// TestReScript_JSXRenders verifies JSX component usage → RENDERS edges.
func TestReScript_JSXRenders(t *testing.T) {
	src := `
open React

@react.component
let make = (~name: string) => {
  <div>
    <Header title="My App" />
    <UserCard name=name />
    <Footer />
  </div>
}
`
	ents := runReScript(t, src, "app.res")

	// @react.component re-kinds `make` to SCOPE.UIComponent (#5378).
	fn := rsFind(ents, "make", "SCOPE.UIComponent")
	if fn == nil {
		t.Fatal("expected 'make' as SCOPE.UIComponent")
	}

	wantRenders := []string{"Header", "UserCard", "Footer"}
	for _, comp := range wantRenders {
		found := false
		for _, r := range fn.Relationships {
			if r.Kind == "RENDERS" && r.ToID == comp {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected RENDERS edge to %q from make", comp)
		}
	}
}

// TestReScript_Language verifies all entities carry Language="rescript".
func TestReScript_Language(t *testing.T) {
	src := `
open Belt

module Utils = {
  let helper = (x) => x + 1
}

type point = { x: float, y: float }

let main = () => {
  let result = Utils.helper(42)
  result
}
`
	ents := runReScript(t, src, "main.res")
	for _, e := range ents {
		if e.Language != "" && e.Language != "rescript" {
			t.Errorf("entity %q: expected Language=rescript, got %q", e.Name, e.Language)
		}
	}
}

// TestReScript_SyntheticFixture is the synthetic React-ReScript fixture that
// exercises all entity types and relationship kinds together.
// Acceptance criterion: ≥80% entity recall, 0 false positives.
func TestReScript_SyntheticFixture(t *testing.T) {
	// Synthetic fixture covering:
	// - module declarations (2)
	// - let functions (5)
	// - type declarations (4)
	// - open statements → IMPORTS (3)
	// - pipe-first chains → CALLS (Belt.Array.map, Belt.Option.getWithDefault)
	// - JSX component usage → RENDERS (TodoItem, LoadingSpinner)
	src := `
open Belt
open React
open Js.Promise

type todoId = string

type priority =
  | High
  | Medium
  | Low

type todo = {
  id: todoId,
  title: string,
  priority: priority,
  completed: bool,
}

type state = {
  todos: array<todo>,
  loading: bool,
}

module TodoUtils = {
  let filterCompleted = (todos) =>
    todos->Belt.Array.keep(t => t.completed)

  let sortByPriority = (todos) =>
    todos->Belt.Array.sortBy(t => switch t.priority {
    | High => 0
    | Medium => 1
    | Low => 2
    })
}

module Api = {
  let fetchTodos = () =>
    Js.Promise.make((~resolve, ~reject as _) => {
      resolve(. [])
    })
}

@react.component
let make = () => {
  let (state, setState) = React.useState(() => {
    todos: [],
    loading: true,
  })

  React.useEffect(() => {
    let _ = Api.fetchTodos()
      ->Js.Promise.then_(todos => {
          setState(prev => {...prev, todos: todos, loading: false})
          Js.Promise.resolve()
        })
    None
  }, [])

  if state.loading {
    <LoadingSpinner />
  } else {
    <div className="todo-list">
      {state.todos
        ->Belt.Array.map(todo => <TodoItem key=todo.id todo=todo />)
        ->React.array}
    </div>
  }
}
`
	ents := runReScript(t, src, "TodoApp.res")

	// --- Entity recall checks ---
	// `make` carries @react.component → re-kinded SCOPE.UIComponent (#5378); the
	// rest are plain SCOPE.Operation let bindings.
	wantOps := []string{"filterCompleted", "sortByPriority", "fetchTodos"}
	wantTypes := []string{"todoId", "priority", "todo", "state"}
	wantModules := []string{"TodoUtils", "Api"}

	missingOps := 0
	for _, name := range wantOps {
		if rsFind(ents, name, "SCOPE.Operation") == nil {
			t.Errorf("synthetic fixture: expected SCOPE.Operation %q", name)
			missingOps++
		}
	}
	if rsFind(ents, "make", "SCOPE.UIComponent") == nil {
		t.Error("synthetic fixture: expected SCOPE.UIComponent \"make\" (@react.component)")
		missingOps++
	}

	missingTypes := 0
	for _, name := range wantTypes {
		if rsFind(ents, name, "SCOPE.Component") == nil {
			t.Errorf("synthetic fixture: expected SCOPE.Component (type) %q", name)
			missingTypes++
		}
	}

	missingModules := 0
	for _, name := range wantModules {
		if rsFind(ents, name, "SCOPE.Component") == nil {
			t.Errorf("synthetic fixture: expected SCOPE.Component (module) %q", name)
			missingModules++
		}
	}

	totalWanted := len(wantOps) + 1 /*make UIComponent*/ + len(wantTypes) + len(wantModules)
	totalMissing := missingOps + missingTypes + missingModules
	totalFound := totalWanted - totalMissing
	recall := float64(totalFound) / float64(totalWanted)
	if recall < 0.80 {
		t.Errorf("synthetic fixture recall %.0f%% below 80%% threshold (%d/%d entities found)",
			recall*100, totalFound, totalWanted)
	}

	// --- IMPORTS edges ---
	wantImports := []string{"Belt", "React", "Js.Promise"}
	for _, mod := range wantImports {
		found := false
		for _, e := range ents {
			for _, r := range e.Relationships {
				if r.Kind == "IMPORTS" && r.ToID == mod {
					found = true
					break
				}
			}
			if found {
				break
			}
		}
		if !found {
			t.Errorf("synthetic fixture: expected IMPORTS edge to %q", mod)
		}
	}

	// --- Pipe-first CALLS edges on filterCompleted ---
	if !rsHasRel(ents, "filterCompleted", "SCOPE.Operation", "CALLS", "Belt.Array.keep") {
		t.Error("synthetic fixture: expected filterCompleted CALLS Belt.Array.keep")
	}

	// --- RENDERS edges on make ---
	wantRenders := []string{"LoadingSpinner", "TodoItem"}
	for _, comp := range wantRenders {
		if !rsHasRel(ents, "make", "SCOPE.UIComponent", "RENDERS", comp) {
			t.Errorf("synthetic fixture: expected make RENDERS %q", comp)
		}
	}

	// --- 0 false positives: no entity should have a blank name ---
	for _, e := range ents {
		if e.Kind != "" && e.Name == "" {
			t.Errorf("false positive: entity with kind=%q has empty name", e.Kind)
		}
	}
}

// TestReScript_InterfaceFile verifies .resi (interface) files are handled.
func TestReScript_InterfaceFile(t *testing.T) {
	src := `
type t

let create: string => t
let getName: t => string
`
	ents := runReScript(t, src, "User.resi")
	// Should extract type and let bindings from interface files too.
	if rsFind(ents, "t", "SCOPE.Component") == nil {
		t.Error("expected type 't' in .resi interface file")
	}
}

// TestReScript_NoFalsePositivesInStrings verifies tokens inside strings don't
// generate spurious CALLS edges.
func TestReScript_NoFalsePositivesInStrings(t *testing.T) {
	src := `
let greet = (name) => {
  let msg = "Hello, " ++ name ++ "! Call someFunc() to continue."
  // This is a comment mentioning anotherFunc()
  Js.log(msg)
}
`
	ents := runReScript(t, src, "strings.res")
	fn := rsFind(ents, "greet", "SCOPE.Operation")
	if fn == nil {
		t.Fatal("expected greet as SCOPE.Operation")
	}

	// "someFunc" and "anotherFunc" appear only in string/comment — must not be CALLS
	for _, r := range fn.Relationships {
		if r.Kind == "CALLS" && (r.ToID == "someFunc" || r.ToID == "anotherFunc") {
			t.Errorf("false positive CALLS to %q from string/comment content", r.ToID)
		}
	}

	// Js.log should be a real CALLS
	found := false
	for _, r := range fn.Relationships {
		if r.Kind == "CALLS" && r.ToID == "Js.log" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected CALLS to Js.log from greet")
	}
}
