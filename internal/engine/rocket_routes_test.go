package engine

import (
	"testing"
)

// TestRocket_Basic covers the canonical Rocket attribute-macro form.
func TestRocket_Basic(t *testing.T) {
	src := `
#[macro_use] extern crate rocket;

#[get("/hello")]
fn hello() -> &'static str { "world" }

#[post("/users")]
fn create_user() -> &'static str { "{}" }
`
	ids, _ := runDetect(t, "rust", "main.rs", src)
	want := []string{
		"http:GET:/hello",
		"http:POST:/users",
	}
	requireContains(t, ids, want, "rocket-basic")
}

// TestRocket_AngleBracketParam covers Rocket's `<id>` path parameter
// syntax and verifies it canonicalises to `{id}`.
func TestRocket_AngleBracketParam(t *testing.T) {
	src := `
#[macro_use] extern crate rocket;

#[get("/users/<id>")]
fn show(id: u32) -> String { format!("{}", id) }

#[delete("/users/<id>")]
fn destroy(id: u32) {}
`
	ids, _ := runDetect(t, "rust", "users.rs", src)
	want := []string{
		"http:GET:/users/{id}",
		"http:DELETE:/users/{id}",
	}
	requireContains(t, ids, want, "rocket-angle-bracket")
}

// TestRocket_AllVerbs covers get/post/put/patch/delete/head/options.
func TestRocket_AllVerbs(t *testing.T) {
	src := `
#[macro_use] extern crate rocket;

#[get("/r")]     fn g()  {}
#[post("/r")]    fn p()  {}
#[put("/r/<id>")]    fn u(id: u32) {}
#[patch("/r/<id>")]  fn pa(id: u32) {}
#[delete("/r/<id>")] fn d(id: u32) {}
#[head("/r")]    fn h()  {}
#[options("/r")] fn o()  {}
`
	ids, _ := runDetect(t, "rust", "main.rs", src)
	want := []string{
		"http:GET:/r",
		"http:POST:/r",
		"http:PUT:/r/{id}",
		"http:PATCH:/r/{id}",
		"http:DELETE:/r/{id}",
		"http:HEAD:/r",
		"http:OPTIONS:/r",
	}
	requireContains(t, ids, want, "rocket-all-verbs")
}

// TestRocket_TrailingMacroArgs covers macros with trailing kwargs such as
// `data = "..."` and `format = "..."`.
func TestRocket_TrailingMacroArgs(t *testing.T) {
	src := `
#[macro_use] extern crate rocket;

#[post("/users", data = "<body>")]
fn create_user(body: String) -> String { body }

#[get("/items", format = "json")]
fn list_items() -> &'static str { "[]" }
`
	ids, _ := runDetect(t, "rust", "main.rs", src)
	want := []string{
		"http:POST:/users",
		"http:GET:/items",
	}
	requireContains(t, ids, want, "rocket-trailing-args")
}

// TestRocket_HandlerRef verifies source_handler property carries the
// function name (compatible with the resolverKindEquivalents fallback).
func TestRocket_HandlerRef(t *testing.T) {
	src := `
#[macro_use] extern crate rocket;

#[post("/quote")]
fn quote() -> &'static str { "{}" }
`
	_, res := runDetect(t, "rust", "pricing.rs", src)
	found := false
	for _, e := range res.Entities {
		if e.ID == "http:POST:/quote" && e.Properties["source_handler"] == "Controller:quote" {
			found = true
		}
	}
	if !found {
		t.Errorf("rocket-handler-ref: expected http:POST:/quote with source_handler=Controller:quote")
	}
}
