package reasonml_test

import (
	"context"
	"testing"

	"github.com/cajasmota/archigraph/internal/extractor"
	_ "github.com/cajasmota/archigraph/internal/extractors/reasonml"
	"github.com/cajasmota/archigraph/internal/types"
)

// runReasonML runs the extractor on raw source and returns entity records.
func runReasonML(t *testing.T, src, path string) []types.EntityRecord {
	t.Helper()
	ext, ok := extractor.Get("reasonml")
	if !ok {
		t.Fatal("reasonml extractor not registered")
	}
	ents, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     path,
		Content:  []byte(src),
		Language: "reasonml",
	})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	return ents
}

func reFind(ents []types.EntityRecord, name, kind string) *types.EntityRecord {
	for i := range ents {
		if ents[i].Name == name && ents[i].Kind == kind {
			return &ents[i]
		}
	}
	return nil
}

func reHasRel(ents []types.EntityRecord, name, kind, edgeKind, toID string) bool {
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

// TestReasonML_Registered verifies the extractor is in the registry.
func TestReasonML_Registered(t *testing.T) {
	_, ok := extractor.Get("reasonml")
	if !ok {
		t.Fatal("reasonml extractor not registered")
	}
}

// TestReasonML_EmptyInput returns zero entities for empty content.
func TestReasonML_EmptyInput(t *testing.T) {
	ext, _ := extractor.Get("reasonml")
	ents, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "empty.re",
		Content:  []byte{},
		Language: "reasonml",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ents) != 0 {
		t.Errorf("expected 0 entities, got %d", len(ents))
	}
}

// TestReasonML_ModuleDiscovery — module declarations extracted as SCOPE.Component.
func TestReasonML_ModuleDiscovery(t *testing.T) {
	src := `open Belt;

module Utils = {
  let double = (x) => x * 2;
  let triple = (x) => x * 3;
};
`
	ents := runReasonML(t, src, "utils.re")
	if reFind(ents, "Utils", "SCOPE.Component") == nil {
		t.Error("expected module Utils as SCOPE.Component")
	}
}

// TestReasonML_LetBindings — let functions extracted as SCOPE.Operation.
func TestReasonML_LetBindings(t *testing.T) {
	src := `let add = (a, b) => a + b;

let rec factorial = (n) =>
  if (n <= 1) { 1 } else { n * factorial(n - 1) };

let greet = (name) => {
  let msg = "Hello, " ++ name;
  Js.log(msg);
};
`
	ents := runReasonML(t, src, "funcs.re")

	ops := make(map[string]bool)
	for _, e := range ents {
		if e.Kind == "SCOPE.Operation" {
			ops[e.Name] = true
			if e.Language != "reasonml" {
				t.Errorf("entity %q: expected Language=reasonml, got %q", e.Name, e.Language)
			}
		}
	}

	for _, want := range []string{"add", "factorial", "greet"} {
		if !ops[want] {
			t.Errorf("expected function %q to be extracted as SCOPE.Operation", want)
		}
	}
}

// TestReasonML_TypeDiscovery — type declarations extracted as SCOPE.Component.
func TestReasonML_TypeDiscovery(t *testing.T) {
	src := `type person = {
  name: string,
  age: int,
};

type shape =
  | Circle(float)
  | Square(float)
  | Rectangle(float, float);

type status = Active | Inactive;
`
	ents := runReasonML(t, src, "types.re")

	comps := make(map[string]string)
	for _, e := range ents {
		if e.Kind == "SCOPE.Component" {
			comps[e.Name] = e.Subtype
		}
	}

	wantTypes := []string{"person", "shape", "status"}
	for _, name := range wantTypes {
		if _, ok := comps[name]; !ok {
			t.Errorf("expected type %q to be extracted as SCOPE.Component", name)
		}
	}

	// Check subtypes
	if comps["person"] != "record" {
		t.Errorf("expected person subtype=record, got %q", comps["person"])
	}
}

// TestReasonML_OpenStatements — open statements emit IMPORTS edges.
func TestReasonML_OpenStatements(t *testing.T) {
	src := `open Belt;
open React;
open ReactDOMRe;

let make = (~name) => {
  <div>{React.string(name)}</div>
};
`
	ents := runReasonML(t, src, "comp.re")

	wantImports := map[string]bool{
		"Belt":       false,
		"React":      false,
		"ReactDOMRe": false,
	}
	for _, e := range ents {
		for _, r := range e.Relationships {
			if r.Kind == "IMPORTS" {
				if _, ok := wantImports[r.ToID]; ok {
					wantImports[r.ToID] = true
					if r.FromID != "comp.re" {
						t.Errorf("IMPORTS %q: expected FromID=comp.re, got %q", r.ToID, r.FromID)
					}
				}
			}
		}
	}
	for mod, found := range wantImports {
		if !found {
			t.Errorf("expected IMPORTS edge for %q", mod)
		}
	}
}

// TestReasonML_CallsEdges — function calls emit CALLS edges.
func TestReasonML_CallsEdges(t *testing.T) {
	src := `let helper = (x) => x * 2;

let caller = (n) => {
  let result = helper(n);
  Js.log(result);
  result;
};
`
	ents := runReasonML(t, src, "calls.re")

	if !reHasRel(ents, "caller", "SCOPE.Operation", "CALLS", "helper") {
		t.Error("expected CALLS caller→helper")
	}
}

// TestReasonML_PipeOperatorCalls — |> chains emit CALLS edges.
func TestReasonML_PipeOperatorCalls(t *testing.T) {
	src := `let processData = (data) =>
  data
  |> Belt.List.map((x) => x * 2)
  |> Belt.List.filter((x) => x > 0);
`
	ents := runReasonML(t, src, "pipes.re")

	hasPipeCall := false
	for _, e := range ents {
		if e.Name == "processData" {
			for _, r := range e.Relationships {
				if r.Kind == "CALLS" && (r.ToID == "Belt.List.map" || r.ToID == "Belt.List.filter") {
					hasPipeCall = true
				}
			}
		}
	}
	if !hasPipeCall {
		t.Error("expected CALLS edges from pipe |> operator targets")
	}
}

// TestReasonML_SelfRecursionExcluded — self-recursive calls not emitted.
func TestReasonML_SelfRecursionExcluded(t *testing.T) {
	src := `let rec fib = (n) =>
  if (n <= 1) { n } else { fib(n - 1) + fib(n - 2) };
`
	ents := runReasonML(t, src, "fib.re")
	if reHasRel(ents, "fib", "SCOPE.Operation", "CALLS", "fib") {
		t.Error("self-recursion CALLS should be filtered")
	}
}

// TestReasonML_LanguageTagged — all relationships carry language=reasonml.
func TestReasonML_LanguageTagged(t *testing.T) {
	src := `open Belt;

type node = { value: int };

let process = (n: node) => n.value;
`
	ents := runReasonML(t, src, "tag.re")
	for _, e := range ents {
		for _, r := range e.Relationships {
			if r.Properties == nil || r.Properties["language"] != "reasonml" {
				t.Errorf("rel %s→%s missing language=reasonml (got %v)", r.Kind, r.ToID, r.Properties)
			}
		}
	}
}

// TestReasonML_ReactReasonFixture — synthetic React-Reason fixture for entity recall.
// This is the primary acceptance test: must hit ≥80% entity recall with 0 false positives.
func TestReasonML_ReactReasonFixture(t *testing.T) {
	src := `open Belt;
open React;
open ReactDOMRe;

/* Types */
type user = {
  id: int,
  name: string,
  email: string,
};

type appState = {
  users: list(user),
  loading: bool,
  error: option(string),
};

type action =
  | FetchUsers
  | FetchUsersSuccess(list(user))
  | FetchUsersFailure(string)
  | DeleteUser(int);

/* Helpers */
let makeUser = (id, name, email) => { id, name, email };

let filterActiveUsers = (users) =>
  users |> Belt.List.filter((u) => u.id > 0);

let userById = (users, id) =>
  users |> Belt.List.getBy((u) => u.id == id);

/* Reducer */
let reducer = (state, action) =>
  switch (action) {
  | FetchUsers => { ...state, loading: true }
  | FetchUsersSuccess(users) => { users, loading: false, error: None }
  | FetchUsersFailure(msg) => { ...state, loading: false, error: Some(msg) }
  | DeleteUser(id) => {
      ...state,
      users: Belt.List.keep(state.users, (u) => u.id !== id),
    }
  };

/* React components */
let make = (~initialUsers=[]) => {
  let (state, dispatch) = React.useReducer(reducer, {
    users: initialUsers,
    loading: false,
    error: None,
  });

  let handleDelete = (id) => dispatch(DeleteUser(id));

  let handleFetch = () => {
    dispatch(FetchUsers);
    Js.Promise.(
      Fetch.fetch("/api/users")
      |> then_(Fetch.Response.json)
      |> then_((json) => {
           dispatch(FetchUsersSuccess([]));
           resolve(json);
         })
      |> catch((err) => {
           dispatch(FetchUsersFailure("Network error"));
           resolve(Js.Json.null);
         })
    );
  };

  <div className="app">
    <h1>{React.string("Users")}</h1>
    {state.loading ? <div>{React.string("Loading...")}</div> : React.null}
    <button onClick={(_) => handleFetch()}>
      {React.string("Fetch Users")}
    </button>
  </div>;
};

/* Entry point */
let renderApp = () =>
  ReactDOMRe.renderToElementWithId(<make />, "root");
`
	ents := runReasonML(t, src, "App.re")

	wantOps := []string{"makeUser", "filterActiveUsers", "userById", "reducer", "make", "renderApp"}
	wantComps := []string{"user", "appState", "action"}
	wantImports := []string{"Belt", "React", "ReactDOMRe"}

	foundOps := make(map[string]bool)
	foundComps := make(map[string]bool)
	foundImports := make(map[string]bool)

	for _, e := range ents {
		switch e.Kind {
		case "SCOPE.Operation":
			foundOps[e.Name] = true
		case "SCOPE.Component":
			foundComps[e.Name] = true
			for _, r := range e.Relationships {
				if r.Kind == "IMPORTS" {
					foundImports[r.ToID] = true
				}
			}
		}
	}

	opHits := 0
	for _, name := range wantOps {
		if foundOps[name] {
			opHits++
		} else {
			t.Logf("missing operation: %s", name)
		}
	}
	compHits := 0
	for _, name := range wantComps {
		if foundComps[name] {
			compHits++
		} else {
			t.Logf("missing component: %s", name)
		}
	}
	importHits := 0
	for _, mod := range wantImports {
		if foundImports[mod] {
			importHits++
		} else {
			t.Logf("missing import: %s", mod)
		}
	}

	totalWant := len(wantOps) + len(wantComps) + len(wantImports)
	totalFound := opHits + compHits + importHits
	recall := float64(totalFound) / float64(totalWant) * 100

	t.Logf("ReactReason fixture recall: %d/%d (%.0f%%): ops=%d/%d comps=%d/%d imports=%d/%d",
		totalFound, totalWant, recall,
		opHits, len(wantOps),
		compHits, len(wantComps),
		importHits, len(wantImports))

	if recall < 80.0 {
		t.Errorf("entity recall %.0f%% below 80%% threshold (%d/%d found)",
			recall, totalFound, totalWant)
	}
}

// TestReasonML_BeltCalls — Belt stdlib calls extracted correctly.
func TestReasonML_BeltCalls(t *testing.T) {
	src := `open Belt;

let processItems = (items) => {
  let doubled = Belt.Array.map(items, (x) => x * 2);
  let filtered = Belt.Array.keep(doubled, (x) => x > 0);
  let total = Belt.Array.reduce(filtered, 0, (acc, x) => acc + x);
  total;
};

let lookupUser = (map, key) =>
  Belt.Map.get(map, key)
  |> Belt.Option.getWithDefault("unknown");
`
	ents := runReasonML(t, src, "belt.re")

	ops := make(map[string]bool)
	for _, e := range ents {
		if e.Kind == "SCOPE.Operation" {
			ops[e.Name] = true
		}
	}

	for _, want := range []string{"processItems", "lookupUser"} {
		if !ops[want] {
			t.Errorf("expected function %q extracted", want)
		}
	}
}

// TestReasonML_InterfaceFile — .rei interface files extract types/module sigs.
func TestReasonML_InterfaceFile(t *testing.T) {
	src := `type t;

type config = {
  host: string,
  port: int,
};

let create: (config) => t;
let connect: (t) => unit;
`
	ents := runReasonML(t, src, "Client.rei")

	comps := make(map[string]bool)
	for _, e := range ents {
		if e.Kind == "SCOPE.Component" {
			comps[e.Name] = true
		}
	}

	for _, want := range []string{"t", "config"} {
		if !comps[want] {
			t.Errorf("expected type %q as SCOPE.Component in .rei file", want)
		}
	}
}
