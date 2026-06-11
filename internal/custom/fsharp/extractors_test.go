package fsharp_test

import (
	"context"
	"strings"
	"testing"

	extreg "github.com/cajasmota/archigraph/internal/extractor"

	_ "github.com/cajasmota/archigraph/internal/custom/fsharp"
)

func fi(path, lang, src string) extreg.FileInput {
	return extreg.FileInput{Path: path, Language: lang, Content: []byte(src)}
}

// TestFSharpRouteE2E_Capture proves the TestServer HttpClient route helpers are
// captured onto a single test_suite's e2e_route_calls property.
func TestFSharpRouteE2E_Capture(t *testing.T) {
	src := `module UsersTests
open Expecto

let tests =
    testList "Users API" [
        testCase "lists users" <| fun _ ->
            let resp = client.GetAsync("/users").Result
            Expect.equal resp.StatusCode 200 "ok"

        testCase "creates a user" <| fun _ ->
            let resp = client.PostAsync("/users", content).Result
            Expect.equal resp.StatusCode 201 "created"

        testCase "deletes a user" <| fun _ ->
            let resp = client.DeleteAsync($"/users/{id}").Result
            ignore resp
    ]
`
	e, ok := extreg.Get("custom_fsharp_tests_route_e2e")
	if !ok {
		t.Fatal("custom_fsharp_tests_route_e2e not registered")
	}
	ents, err := e.Extract(context.Background(), fi("tests/UsersTests.fs", "fsharp", src))
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
	for _, want := range []string{"GET /users", "POST /users", "DELETE /users"} {
		if !strings.Contains(calls, want) {
			t.Errorf("expected route call %q in %q", want, calls)
		}
	}
}

// TestFSharpRouteE2E_RequestMessageForm covers the
// HttpRequestMessage(HttpMethod.X, "/path") construction shape.
func TestFSharpRouteE2E_RequestMessageForm(t *testing.T) {
	src := `module ApiTests
open Xunit

[<Fact>]
let ` + "``gets health``" + ` () =
    let req = new HttpRequestMessage(HttpMethod.Get, "/health")
    let resp = client.SendAsync(req).Result
    Assert.Equal(200, int resp.StatusCode)
`
	e, _ := extreg.Get("custom_fsharp_tests_route_e2e")
	ents, _ := e.Extract(context.Background(), fi("tests/ApiTests.fs", "fsharp", src))
	if len(ents) != 1 {
		t.Fatalf("expected 1 test_suite, got %d", len(ents))
	}
	if !strings.Contains(ents[0].Properties["e2e_route_calls"], "GET /health") {
		t.Errorf("expected GET /health, got %q", ents[0].Properties["e2e_route_calls"])
	}
}

// TestFSharpRouteE2E_NonTestExcluded proves a production route file (not a test)
// is NOT captured as a test_suite.
func TestFSharpRouteE2E_NonTestExcluded(t *testing.T) {
	src := `module App
open Giraffe

let webApp =
    choose [
        GET >=> route "/users" >=> listUsers
    ]
`
	e, _ := extreg.Get("custom_fsharp_tests_route_e2e")
	ents, _ := e.Extract(context.Background(), fi("src/App.fs", "fsharp", src))
	if len(ents) != 0 {
		t.Fatalf("expected no test_suite for a non-test file, got %d", len(ents))
	}
}

// TestFSharpRouteE2E_ShapeOnlyTestExcluded proves a unit test that never hits a
// route emits no suite.
func TestFSharpRouteE2E_ShapeOnlyTestExcluded(t *testing.T) {
	src := `module MathTests
open Expecto

let tests =
    testList "Math" [
        testCase "adds" <| fun _ ->
            Expect.equal (add 1 2) 3 "sum"
    ]
`
	e, _ := extreg.Get("custom_fsharp_tests_route_e2e")
	ents, _ := e.Extract(context.Background(), fi("tests/MathTests.fs", "fsharp", src))
	if len(ents) != 0 {
		t.Fatalf("expected no test_suite for a shape-only test, got %d", len(ents))
	}
}

// TestFSharpRouteE2E_WrongLanguageNoop proves the extractor gates on
// language=="fsharp".
func TestFSharpRouteE2E_WrongLanguageNoop(t *testing.T) {
	src := `client.GetAsync("/users")`
	e, _ := extreg.Get("custom_fsharp_tests_route_e2e")
	ents, _ := e.Extract(context.Background(), fi("tests/UsersTests.cs", "csharp", src))
	if len(ents) != 0 {
		t.Fatalf("expected no entities for non-fsharp language, got %d", len(ents))
	}
}
