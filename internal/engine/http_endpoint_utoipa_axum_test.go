package engine

import (
	"testing"
)

// countDefsForHandler returns how many http_endpoint_definition entities carry
// source_handler=Controller:<handler>. Used to prove the utoipa_axum promoter
// and synthesizeAxumRoutes never BOTH mint for the same handler (#6530's
// duplicate-producer hazard).
func countDefsForHandler(res *DetectResult, handler string) int {
	n := 0
	for _, e := range res.Entities {
		if e.Kind != httpEndpointDefinitionKind {
			continue
		}
		if e.Properties["source_handler"] == "Controller:"+handler {
			n++
		}
	}
	return n
}

// TestUtoipaAxum_SingleHandlerRoutesMacro is the assertion that was missing
// before #6560: a handler registered ONLY through `routes!(handler)` must
// produce a canonical http_endpoint_definition, using the verb and path
// declared on its same-file `#[utoipa::path(...)]` attribute.
func TestUtoipaAxum_SingleHandlerRoutesMacro(t *testing.T) {
	src := `
use utoipa_axum::router::OpenApiRouter;
use utoipa_axum::routes;

#[utoipa::path(get, path = "/items")]
async fn list_items() -> &'static str { "[]" }

#[utoipa::path(post, path = "/items")]
async fn create_item() -> &'static str { "{}" }

pub fn router() -> OpenApiRouter {
    OpenApiRouter::new()
        .routes(routes!(list_items))
        .routes(routes!(create_item))
}
`
	ids, res := runDetect(t, "rust", "src/api.rs", src)
	requireContains(t, ids, []string{
		"http:GET:/items",
		"http:POST:/items",
	}, "utoipa-axum-routes-macro")

	if got := countDefsForHandler(res, "list_items"); got != 1 {
		t.Errorf("utoipa-axum-routes-macro: want exactly 1 definition for list_items, got %d", got)
	}
	if got := countDefsForHandler(res, "create_item"); got != 1 {
		t.Errorf("utoipa-axum-routes-macro: want exactly 1 definition for create_item, got %d", got)
	}
}

// TestUtoipaAxum_PathParams covers the `{id}` curly-brace parameter form,
// which utoipa shares with axum.
func TestUtoipaAxum_PathParams(t *testing.T) {
	src := `
use utoipa_axum::router::OpenApiRouter;
use utoipa_axum::routes;

#[utoipa::path(get, path = "/items/{id}", responses((status = 200, body = Item)))]
async fn get_item() -> &'static str { "{}" }

pub fn router() -> OpenApiRouter {
    OpenApiRouter::new().routes(routes!(get_item))
}
`
	ids, _ := runDetect(t, "rust", "src/items.rs", src)
	requireContains(t, ids, []string{"http:GET:/items/{id}"}, "utoipa-axum-path-params")
}

// TestUtoipaAxum_HandlerRef verifies the promoted definition carries the
// source_handler stamp the resolver needs to emit an IMPLEMENTS edge.
func TestUtoipaAxum_HandlerRef(t *testing.T) {
	src := `
use utoipa_axum::router::OpenApiRouter;
use utoipa_axum::routes;

#[utoipa::path(delete, path = "/items/{id}")]
async fn delete_item() -> &'static str { "" }

pub fn router() -> OpenApiRouter {
    OpenApiRouter::new().routes(routes!(delete_item))
}
`
	_, res := runDetect(t, "rust", "src/items.rs", src)
	found := false
	for _, e := range res.Entities {
		if e.ID == "http:DELETE:/items/{id}" && e.Kind == httpEndpointDefinitionKind {
			if e.Properties["source_handler"] == "Controller:delete_item" &&
				e.Properties["framework"] == "utoipa_axum" {
				found = true
			}
		}
	}
	if !found {
		t.Errorf("utoipa-axum-handler-ref: expected http:DELETE:/items/{id} with source_handler=Controller:delete_item framework=utoipa_axum")
	}
}

// TestUtoipaAxum_SiblingRouteCall is the mixed-file case from #6560: a
// `routes!(...)` registration and a plain `.route(...)` in the SAME function.
// Both handlers must yield exactly one endpoint each — the `.route(` half was
// already covered, the `routes!` half was not.
func TestUtoipaAxum_SiblingRouteCall(t *testing.T) {
	src := `
use axum::routing::get;
use utoipa_axum::router::OpenApiRouter;
use utoipa_axum::routes;

#[utoipa::path(get, path = "/items")]
async fn list_items() -> &'static str { "[]" }

async fn health() -> &'static str { "ok" }

pub fn router() -> OpenApiRouter {
    OpenApiRouter::new()
        .routes(routes!(list_items))
        .route("/health", get(health))
}
`
	ids, res := runDetect(t, "rust", "src/api.rs", src)
	requireContains(t, ids, []string{
		"http:GET:/items",
		"http:GET:/health",
	}, "utoipa-axum-sibling-route")

	if got := countDefsForHandler(res, "list_items"); got != 1 {
		t.Errorf("utoipa-axum-sibling-route: want exactly 1 definition for list_items, got %d", got)
	}
	if got := countDefsForHandler(res, "health"); got != 1 {
		t.Errorf("utoipa-axum-sibling-route: want exactly 1 definition for health, got %d", got)
	}
}

// TestUtoipaAxum_DedupeAgainstRouteCall pins the dedupe KEY (handler name).
// The handler is registered BOTH ways, and the `.route(` registration mounts it
// at a different path than its attribute declares. Only the `.route(` endpoint
// may be minted: the registration in code is authoritative, and minting the
// attribute path as well would invent a second endpoint for one handler.
func TestUtoipaAxum_DedupeAgainstRouteCall(t *testing.T) {
	src := `
use axum::routing::get;
use utoipa_axum::router::OpenApiRouter;
use utoipa_axum::routes;

#[utoipa::path(get, path = "/items")]
async fn list_items() -> &'static str { "[]" }

pub fn router() -> OpenApiRouter {
    OpenApiRouter::new()
        .routes(routes!(list_items))
        .route("/v1/items", get(list_items))
}
`
	ids, res := runDetect(t, "rust", "src/api.rs", src)
	requireContains(t, ids, []string{"http:GET:/v1/items"}, "utoipa-axum-dedupe")
	requireNotContains(t, ids, []string{"http:GET:/items"}, "utoipa-axum-dedupe")

	if got := countDefsForHandler(res, "list_items"); got != 1 {
		t.Errorf("utoipa-axum-dedupe: want exactly 1 definition for list_items, got %d", got)
	}
}

// TestUtoipaAxum_UnknownHandlerNotMinted pins the same-file attribute
// requirement: a `routes!(x)` naming something with NO `#[utoipa::path]`
// attribute in this file has no statically-known verb or path, so nothing may
// be minted for it. This is the permissive direction that must stay closed —
// Arm B (cross-module handler resolution) is explicitly out of scope for #6560
// Arm A, and a fabricated endpoint is worse than a missing one.
func TestUtoipaAxum_UnknownHandlerNotMinted(t *testing.T) {
	src := `
use utoipa_axum::router::OpenApiRouter;
use utoipa_axum::routes;

#[utoipa::path(get, path = "/items")]
async fn list_items() -> &'static str { "[]" }

pub fn router() -> OpenApiRouter {
    OpenApiRouter::new()
        .routes(routes!(list_items))
        .routes(routes!(imported_from_elsewhere))
}
`
	_, res := runDetect(t, "rust", "src/api.rs", src)
	if got := countDefsForHandler(res, "imported_from_elsewhere"); got != 0 {
		t.Errorf("utoipa-axum-unknown-handler: want 0 definitions for an unannotated handler, got %d", got)
	}
	if got := countDefsForHandler(res, "list_items"); got != 1 {
		t.Errorf("utoipa-axum-unknown-handler: want exactly 1 definition for list_items, got %d", got)
	}
}

// TestUtoipaAxum_RocketRoutesMacroUnaffected guards the pre-filter: Rocket's
// `routes![...]` / `routes!(...)` mount macro must not be read as a utoipa_axum
// registration. Rocket handlers are already covered by synthesizeRocket, and
// the framework stamp is what observes it: asserting only the ID and the count
// would still pass if this pass minted the endpoint itself, because the two
// producers agree on the ID here.
func TestUtoipaAxum_RocketRoutesMacroUnaffected(t *testing.T) {
	src := `
#[macro_use] extern crate rocket;

#[get("/hello")]
fn hello() -> &'static str { "world" }

#[launch]
fn rocket() -> _ {
    rocket::build().mount("/", routes!(hello))
}
`
	ids, res := runDetect(t, "rust", "src/main.rs", src)
	requireContains(t, ids, []string{"http:GET:/hello"}, "utoipa-axum-rocket-guard")
	if got := countDefsForHandler(res, "hello"); got != 1 {
		t.Errorf("utoipa-axum-rocket-guard: want exactly 1 definition for hello, got %d", got)
	}
	for _, e := range res.Entities {
		if e.Kind == httpEndpointDefinitionKind && e.Properties["source_handler"] == "Controller:hello" {
			if e.Properties["framework"] != "rocket" {
				t.Errorf("utoipa-axum-rocket-guard: hello endpoint %q must come from synthesizeRocket, got framework=%q",
					e.ID, e.Properties["framework"])
			}
		}
	}
}

// TestUtoipaAxum_RocketAttributeSameFileNotDoubleMinted is the #6530 shape the
// first cut of this pass missed: a handler carrying BOTH a `#[utoipa::path]`
// attribute and a Rocket verb attribute, mounted with the paren form
// `routes!(h)`. `#[utoipa::path]` beside Rocket attribute macros is a shipped
// utoipa combination. synthesizeRocket already registers such a handler, so this
// pass must not mint a second, differently-pathed definition for it — the dedupe
// key has to mean "already registered by ANY producer in this file", not "by
// axumRouteRe".
//
// The framework assertion matters independently: this pass runs BEFORE
// synthesizeRocket, so a double mint whose IDs happened to agree would not add
// an entity — it would silently relabel a Rocket endpoint as utoipa_axum.
func TestUtoipaAxum_RocketAttributeSameFileNotDoubleMinted(t *testing.T) {
	src := `
#[macro_use] extern crate rocket;
use utoipa::OpenApi;

#[utoipa::path(get, path = "/hello")]
#[get("/v1/hello")]
fn hello() -> &'static str { "world" }

#[launch]
fn rocket() -> _ {
    rocket::build().mount("/", routes!(hello))
}
`
	ids, res := runDetect(t, "rust", "src/main.rs", src)
	requireContains(t, ids, []string{"http:GET:/v1/hello"}, "utoipa-axum-rocket-attr")
	requireNotContains(t, ids, []string{"http:GET:/hello"}, "utoipa-axum-rocket-attr")

	if got := countDefsForHandler(res, "hello"); got != 1 {
		t.Errorf("utoipa-axum-rocket-attr: want exactly 1 definition for hello, got %d", got)
	}
	for _, e := range res.Entities {
		if e.Kind == httpEndpointDefinitionKind && e.Properties["source_handler"] == "Controller:hello" {
			if e.Properties["framework"] != "rocket" {
				t.Errorf("utoipa-axum-rocket-attr: hello endpoint %q should stay framework=rocket, got %q",
					e.ID, e.Properties["framework"])
			}
		}
	}
}

// TestUtoipaAxum_CommentedOutAttributeNotMinted pins the bound on the
// attribute→fn association. The attribute is COMMENTED OUT, so it decorates
// nothing; binding it to whatever `fn` happens to follow would stamp a route on
// an unrelated function — the worst shape of false positive.
func TestUtoipaAxum_CommentedOutAttributeNotMinted(t *testing.T) {
	src := `
use utoipa_axum::router::OpenApiRouter;
use utoipa_axum::routes;

// #[utoipa::path(delete, path = "/legacy/{id}")]

async fn purge_everything() -> &'static str { "" }

#[utoipa::path(get, path = "/items")]
async fn list_items() -> &'static str { "[]" }

pub fn router() -> OpenApiRouter {
    OpenApiRouter::new()
        .routes(routes!(list_items))
        .routes(routes!(purge_everything))
}
`
	ids, res := runDetect(t, "rust", "src/api.rs", src)
	requireNotContains(t, ids, []string{"http:DELETE:/legacy/{id}"}, "utoipa-axum-commented-attr")
	if got := countDefsForHandler(res, "purge_everything"); got != 0 {
		t.Errorf("utoipa-axum-commented-attr: want 0 definitions for purge_everything, got %d", got)
	}
	if got := countDefsForHandler(res, "list_items"); got != 1 {
		t.Errorf("utoipa-axum-commented-attr: want exactly 1 definition for list_items, got %d", got)
	}
}

// TestUtoipaAxum_AttributeBindsOnlyToTheFunctionItDecorates pins the same bound
// from the other side: an attribute separated from the next `fn` by unrelated
// code is not that function's contract.
func TestUtoipaAxum_AttributeBindsOnlyToTheFunctionItDecorates(t *testing.T) {
	src := `
use utoipa_axum::router::OpenApiRouter;
use utoipa_axum::routes;

#[utoipa::path(get, path = "/orphan")]
struct NotAHandler { id: u32 }

async fn unrelated() -> &'static str { "" }

pub fn router() -> OpenApiRouter {
    OpenApiRouter::new().routes(routes!(unrelated))
}
`
	ids, res := runDetect(t, "rust", "src/api.rs", src)
	requireNotContains(t, ids, []string{"http:GET:/orphan"}, "utoipa-axum-attr-binding")
	if got := countDefsForHandler(res, "unrelated"); got != 0 {
		t.Errorf("utoipa-axum-attr-binding: want 0 definitions for unrelated, got %d", got)
	}
}

// TestUtoipaAxum_MultiHandlerRoutesMacroNotMinted observes the Arm B exclusion
// this pass claims: `routes!(a, b)` is out of scope for Arm A, so it must emit
// NOTHING. Minting only the first handler would be a half-registration — the
// second route silently missing — which is worse than the whole-form gap, and is
// exactly what a `[,)]` widening of utoipaRoutesMacroRe produces.
func TestUtoipaAxum_MultiHandlerRoutesMacroNotMinted(t *testing.T) {
	src := `
use utoipa_axum::router::OpenApiRouter;
use utoipa_axum::routes;

#[utoipa::path(get, path = "/items")]
async fn list_items() -> &'static str { "[]" }

#[utoipa::path(post, path = "/items")]
async fn create_item() -> &'static str { "{}" }

pub fn router() -> OpenApiRouter {
    OpenApiRouter::new().routes(routes!(list_items, create_item))
}
`
	ids, res := runDetect(t, "rust", "src/api.rs", src)
	requireNotContains(t, ids, []string{"http:GET:/items", "http:POST:/items"},
		"utoipa-axum-multi-handler")
	if got := countDefsForHandler(res, "list_items"); got != 0 {
		t.Errorf("utoipa-axum-multi-handler: want 0 definitions for list_items, got %d", got)
	}
	if got := countDefsForHandler(res, "create_item"); got != 0 {
		t.Errorf("utoipa-axum-multi-handler: want 0 definitions for create_item, got %d", got)
	}
}
