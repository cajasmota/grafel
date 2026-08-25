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

// frameworkForHandler returns the `framework` property of the (single)
// http_endpoint_definition minted for handler, or "" if there is none. It is the
// premise guard for the multi-handler tests below: asserting only the endpoint
// ID would still pass if synthesizeAxumRoutes or synthesizeRocket had minted the
// route, leaving the branch under test unexecuted.
func frameworkForHandler(res *DetectResult, handler string) string {
	for _, e := range res.Entities {
		if e.Kind != httpEndpointDefinitionKind {
			continue
		}
		if e.Properties["source_handler"] == "Controller:"+handler {
			return e.Properties["framework"]
		}
	}
	return ""
}

// TestUtoipaAxum_MultiHandlerRoutesMacroMinted REPLACES
// TestUtoipaAxum_MultiHandlerRoutesMacroNotMinted, which asserted 0 definitions
// for both handlers of `routes!(a, b)`.
//
// That pin was honest when it was written: Arm A of #6560 deliberately handled
// only the single-handler form, because minting just the FIRST handler of N — a
// silent half-registration — is worse than minting nothing, and that is what a
// naive `[,)]` widening produces. The pin recorded that exclusion so it could not
// be half-lifted by accident.
//
// Arm B1 of #6560 lifts it properly: every argument of the macro is resolved
// against the same-file attribute map, exactly as the single-handler case
// already was, or the macro is skipped whole. So the expected behaviour is now
// the opposite of what the old test asserted, and the assertion is inverted here
// rather than deleted — a silently removed pin is indistinguishable from a
// regression.
//
// Still out of scope (Arm B2): path-qualified arguments and cross-file handler
// resolution — see TestUtoipaAxum_MultiHandlerPathQualifiedNotMinted.
func TestUtoipaAxum_MultiHandlerRoutesMacroMinted(t *testing.T) {
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
	requireContains(t, ids, []string{
		"http:GET:/items",
		"http:POST:/items",
	}, "utoipa-axum-multi-handler")

	for _, h := range []string{"list_items", "create_item"} {
		if got := countDefsForHandler(res, h); got != 1 {
			t.Errorf("utoipa-axum-multi-handler: want exactly 1 definition for %s, got %d", h, got)
		}
		// Premise guard: this fixture has no `.route(` mount and no Rocket
		// attribute, so both definitions must come from THIS pass. Without
		// this, the test would still pass if another producer had minted them
		// and utoipaRegisteredElsewhere had skipped the branch under test.
		if got := frameworkForHandler(res, h); got != "utoipa_axum" {
			t.Errorf("utoipa-axum-multi-handler: %s must be minted by the utoipa_axum pass, got framework=%q", h, got)
		}
	}
}

// TestUtoipaAxum_MultiHandlerVerbsAndPathsNotSwapped pins the pairing, not just
// the count: three handlers with three DISTINCT paths and three distinct verbs,
// so a widening that minted N definitions off one handler's contract, or that
// paired arguments with attributes positionally, is visible.
func TestUtoipaAxum_MultiHandlerVerbsAndPathsNotSwapped(t *testing.T) {
	src := `
use utoipa_axum::router::OpenApiRouter;
use utoipa_axum::routes;

#[utoipa::path(get, path = "/items")]
async fn list_items() -> &'static str { "[]" }

#[utoipa::path(post, path = "/orders")]
async fn create_order() -> &'static str { "{}" }

#[utoipa::path(delete, path = "/sessions/{id}")]
async fn drop_session() -> &'static str { "" }

pub fn router() -> OpenApiRouter {
    OpenApiRouter::new().routes(routes!(
        create_order,
        drop_session,
        list_items,
    ))
}
`
	ids, res := runDetect(t, "rust", "src/api.rs", src)
	requireContains(t, ids, []string{
		"http:GET:/items",
		"http:POST:/orders",
		"http:DELETE:/sessions/{id}",
	}, "utoipa-axum-multi-pairing")
	requireNotContains(t, ids, []string{
		"http:POST:/items",
		"http:GET:/orders",
		"http:DELETE:/items",
	}, "utoipa-axum-multi-pairing")

	for _, h := range []string{"list_items", "create_order", "drop_session"} {
		if got := countDefsForHandler(res, h); got != 1 {
			t.Errorf("utoipa-axum-multi-pairing: want exactly 1 definition for %s, got %d", h, got)
		}
		if got := frameworkForHandler(res, h); got != "utoipa_axum" {
			t.Errorf("utoipa-axum-multi-pairing: %s must be minted by the utoipa_axum pass, got framework=%q", h, got)
		}
	}
}

// TestUtoipaAxum_MultiHandlerPathQualifiedNotMinted keeps the Arm B2 boundary
// where the ruling on #6560 put it. A path-qualified argument names a handler
// whose `#[utoipa::path]` attribute is in ANOTHER file, and this pass's attribute
// map is FILE-SCOPED — that file scope is what currently prevents the cross-file
// double-mint hazard of #6530. Resolving `crate::items::create_item` needs a
// dedupe-scope decision, not a wider regex.
//
// So a macro carrying ANY qualified argument is skipped WHOLE. Minting only its
// plain arguments would be the same silent half-registration the Arm A pin was
// written to forbid, just at a different boundary.
func TestUtoipaAxum_MultiHandlerPathQualifiedNotMinted(t *testing.T) {
	src := `
use utoipa_axum::router::OpenApiRouter;
use utoipa_axum::routes;

#[utoipa::path(get, path = "/items")]
async fn list_items() -> &'static str { "[]" }

pub fn router() -> OpenApiRouter {
    OpenApiRouter::new().routes(routes!(list_items, crate::items::create_item))
}
`
	ids, res := runDetect(t, "rust", "src/api.rs", src)
	requireNotContains(t, ids, []string{"http:GET:/items"}, "utoipa-axum-multi-qualified")
	if got := countDefsForHandler(res, "list_items"); got != 0 {
		t.Errorf("utoipa-axum-multi-qualified: want 0 definitions for list_items, got %d", got)
	}
	if got := countDefsForHandler(res, "create_item"); got != 0 {
		t.Errorf("utoipa-axum-multi-qualified: want 0 definitions for create_item, got %d", got)
	}
}

// TestUtoipaAxum_SingleHandlerPathQualifiedNotMinted is the same B2 boundary in
// the one-argument form, which Arm A excluded by requiring a bare identifier
// before the closing paren. Widening for the multi form must not lose it.
func TestUtoipaAxum_SingleHandlerPathQualifiedNotMinted(t *testing.T) {
	src := `
use utoipa_axum::router::OpenApiRouter;
use utoipa_axum::routes;

#[utoipa::path(get, path = "/items")]
async fn list_items() -> &'static str { "[]" }

pub fn router() -> OpenApiRouter {
    OpenApiRouter::new().routes(routes!(crate::items::list_items))
}
`
	ids, res := runDetect(t, "rust", "src/api.rs", src)
	requireNotContains(t, ids, []string{"http:GET:/items"}, "utoipa-axum-single-qualified")
	if got := countDefsForHandler(res, "list_items"); got != 0 {
		t.Errorf("utoipa-axum-single-qualified: want 0 definitions for list_items, got %d", got)
	}
}

// TestUtoipaAxum_MultiHandlerDedupeAgainstRouteCall checks the #6530 dedupe
// still holds per-ARGUMENT when the macro carries N of them: one handler of the
// pair is also mounted with `.route(...)`, so synthesizeAxumRoutes owns it and
// this pass must skip that argument WITHOUT skipping its sibling.
func TestUtoipaAxum_MultiHandlerDedupeAgainstRouteCall(t *testing.T) {
	src := `
use axum::routing::get;
use utoipa_axum::router::OpenApiRouter;
use utoipa_axum::routes;

#[utoipa::path(get, path = "/documented/items")]
async fn list_items() -> &'static str { "[]" }

#[utoipa::path(post, path = "/items")]
async fn create_item() -> &'static str { "{}" }

pub fn router() -> OpenApiRouter {
    OpenApiRouter::new()
        .routes(routes!(list_items, create_item))
        .route("/mounted/items", get(list_items))
}
`
	ids, res := runDetect(t, "rust", "src/api.rs", src)
	requireContains(t, ids, []string{
		"http:GET:/mounted/items",
		"http:POST:/items",
	}, "utoipa-axum-multi-dedupe")
	requireNotContains(t, ids, []string{"http:GET:/documented/items"}, "utoipa-axum-multi-dedupe")

	if got := countDefsForHandler(res, "list_items"); got != 1 {
		t.Errorf("utoipa-axum-multi-dedupe: want exactly 1 definition for list_items, got %d", got)
	}
	if got := frameworkForHandler(res, "list_items"); got != "axum" {
		t.Errorf("utoipa-axum-multi-dedupe: list_items must stay owned by synthesizeAxumRoutes, got framework=%q", got)
	}
	if got := countDefsForHandler(res, "create_item"); got != 1 {
		t.Errorf("utoipa-axum-multi-dedupe: want exactly 1 definition for create_item, got %d", got)
	}
	if got := frameworkForHandler(res, "create_item"); got != "utoipa_axum" {
		t.Errorf("utoipa-axum-multi-dedupe: create_item must be minted by the utoipa_axum pass, got framework=%q", got)
	}
}

// TestUtoipaAxum_HandlerRepeatedAcrossMacrosMintedOnce pins that the
// once-per-handler guard survives N arguments: a handler named twice inside one
// macro, and again in a second macro, is still one definition.
func TestUtoipaAxum_HandlerRepeatedAcrossMacrosMintedOnce(t *testing.T) {
	src := `
use utoipa_axum::router::OpenApiRouter;
use utoipa_axum::routes;

#[utoipa::path(get, path = "/items")]
async fn list_items() -> &'static str { "[]" }

#[utoipa::path(post, path = "/items")]
async fn create_item() -> &'static str { "{}" }

pub fn router() -> OpenApiRouter {
    OpenApiRouter::new()
        .routes(routes!(list_items, create_item, list_items))
        .routes(routes!(list_items))
}
`
	_, res := runDetect(t, "rust", "src/api.rs", src)
	if got := countDefsForHandler(res, "list_items"); got != 1 {
		t.Errorf("utoipa-axum-repeat: want exactly 1 definition for list_items, got %d", got)
	}
	if got := countDefsForHandler(res, "create_item"); got != 1 {
		t.Errorf("utoipa-axum-repeat: want exactly 1 definition for create_item, got %d", got)
	}
}

// TestUtoipaAxum_AdjacentMacrosNotSlurped pins the macro boundary. The widened
// argument list must not run past its own closing paren into the next
// `routes!(...)` on the following line — a capture that spanned both would read
// one malformed argument list and register NEITHER handler.
func TestUtoipaAxum_AdjacentMacrosNotSlurped(t *testing.T) {
	src := `
use utoipa_axum::router::OpenApiRouter;
use utoipa_axum::routes;

#[utoipa::path(get, path = "/items")]
async fn list_items() -> &'static str { "[]" }

#[utoipa::path(post, path = "/orders")]
async fn create_order() -> &'static str { "{}" }

#[utoipa::path(delete, path = "/sessions")]
async fn drop_session() -> &'static str { "" }

pub fn router() -> OpenApiRouter {
    OpenApiRouter::new()
        .routes(routes!(list_items, create_order))
        .routes(routes!(drop_session))
}
`
	ids, res := runDetect(t, "rust", "src/api.rs", src)
	requireContains(t, ids, []string{
		"http:GET:/items",
		"http:POST:/orders",
		"http:DELETE:/sessions",
	}, "utoipa-axum-adjacent-macros")
	for _, h := range []string{"list_items", "create_order", "drop_session"} {
		if got := countDefsForHandler(res, h); got != 1 {
			t.Errorf("utoipa-axum-adjacent-macros: want exactly 1 definition for %s, got %d", h, got)
		}
		if got := frameworkForHandler(res, h); got != "utoipa_axum" {
			t.Errorf("utoipa-axum-adjacent-macros: %s must be minted by the utoipa_axum pass, got framework=%q", h, got)
		}
	}
}

// TestUtoipaAxum_UnterminatedRoutesMacroNotMinted pins the trailing-paren
// anchor. `routes!(` with no closing paren on the argument list is not a
// registration, and dropping the anchor from the regex would let the capture run
// to the end of the file and mint from whatever identifiers it swept up.
func TestUtoipaAxum_UnterminatedRoutesMacroNotMinted(t *testing.T) {
	src := `
use utoipa_axum::router::OpenApiRouter;
use utoipa_axum::routes;

#[utoipa::path(get, path = "/items")]
async fn list_items() -> &'static str { "[]" }

#[utoipa::path(post, path = "/orders")]
async fn create_order() -> &'static str { "{}" }

// The macro below is never closed, and the file ends mid-expression.
pub fn router() -> OpenApiRouter {
    OpenApiRouter::new().routes(routes!(list_items, create_order
`
	ids, res := runDetect(t, "rust", "src/api.rs", src)
	requireNotContains(t, ids, []string{"http:GET:/items", "http:POST:/orders"},
		"utoipa-axum-unterminated")
	for _, h := range []string{"list_items", "create_order"} {
		if got := countDefsForHandler(res, h); got != 0 {
			t.Errorf("utoipa-axum-unterminated: want 0 definitions for %s, got %d", h, got)
		}
	}
}

// TestUtoipaAxum_EmptyRoutesMacroNotMinted pins that `routes!()` registers
// nothing — the argument list must require at least one identifier, so a
// widening to "zero or more" cannot make an empty macro a live registration.
func TestUtoipaAxum_EmptyRoutesMacroNotMinted(t *testing.T) {
	src := `
use utoipa_axum::router::OpenApiRouter;
use utoipa_axum::routes;

#[utoipa::path(get, path = "/items")]
async fn list_items() -> &'static str { "[]" }

pub fn router() -> OpenApiRouter {
    OpenApiRouter::new().routes(routes!())
}
`
	ids, res := runDetect(t, "rust", "src/api.rs", src)
	requireNotContains(t, ids, []string{"http:GET:/items"}, "utoipa-axum-empty-macro")
	if got := countDefsForHandler(res, "list_items"); got != 0 {
		t.Errorf("utoipa-axum-empty-macro: want 0 definitions for list_items, got %d", got)
	}
}

// TestUtoipaAxum_RocketMultiHandlerRoutesMacroUnaffected extends the Rocket
// guard to the form the B1 widening newly matches. Rocket's own mount macro is
// most often `routes![a, b]`, but the paren form `routes!(a, b)` is legal and is
// now a shape this pass recognises, so the dedupe — not the regex — has to be
// what keeps synthesizeRocket the owner. The framework stamp is the assertion
// that observes it: this pass runs BEFORE synthesizeRocket, so a double mint
// whose IDs agreed would relabel the Rocket endpoints rather than add entities.
func TestUtoipaAxum_RocketMultiHandlerRoutesMacroUnaffected(t *testing.T) {
	src := `
#[macro_use] extern crate rocket;

#[get("/hello")]
fn hello() -> &'static str { "world" }

#[post("/goodbye")]
fn goodbye() -> &'static str { "bye" }

#[launch]
fn rocket() -> _ {
    rocket::build().mount("/", routes!(hello, goodbye))
}
`
	ids, res := runDetect(t, "rust", "src/main.rs", src)
	requireContains(t, ids, []string{"http:GET:/hello", "http:POST:/goodbye"},
		"utoipa-axum-rocket-multi-guard")
	for _, h := range []string{"hello", "goodbye"} {
		if got := countDefsForHandler(res, h); got != 1 {
			t.Errorf("utoipa-axum-rocket-multi-guard: want exactly 1 definition for %s, got %d", h, got)
		}
		if got := frameworkForHandler(res, h); got != "rocket" {
			t.Errorf("utoipa-axum-rocket-multi-guard: %s must come from synthesizeRocket, got framework=%q", h, got)
		}
	}
}
