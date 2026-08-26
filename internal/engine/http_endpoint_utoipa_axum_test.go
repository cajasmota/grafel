package engine

import (
	"strings"
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
	for _, h := range []string{"list_items", "create_item"} {
		if got := countDefsForHandler(res, h); got != 1 {
			t.Errorf("utoipa-axum-repeat: want exactly 1 definition for %s, got %d", h, got)
		}
		// Premise guard. Without it "exactly 1" would also be satisfied by this
		// pass minting NONE and another producer minting one — which is the
		// reading that would make this test worthless as evidence that the
		// once-per-handler bound belongs to the utoipa_axum path.
		if got := frameworkForHandler(res, h); got != "utoipa_axum" {
			t.Errorf("utoipa-axum-repeat: %s must be minted by the utoipa_axum pass, got framework=%q", h, got)
		}
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

// TestUtoipaAxum_MixedResolvableAndImportedArguments is the fixture the first
// cut of B1 was missing, and it observes the ONLY behaviour B1 actually
// introduces: arguments are resolved INDEPENDENTLY of one another.
//
// This is the idiomatic utoipa_axum layout, not a corner case — a router module
// aggregates handlers imported by bare name from sibling modules, so a single
// macro routinely mixes a handler whose `#[utoipa::path]` attribute is in THIS
// file with handlers whose attributes are in another. The imported ones are
// unresolvable here (the attribute map is file-scoped; that is Arm B2) and must
// be skipped one by one, leaving every same-file sibling minted.
//
// The unresolvable arguments are placed FIRST and in the MIDDLE deliberately: an
// early-exit on the first unknown handler — `continue` weakened to `break` —
// leaves the whole macro unminted, and every other fixture in this file has a
// uniformly resolvable or uniformly unresolvable argument list, so none of them
// can see it.
func TestUtoipaAxum_MixedResolvableAndImportedArguments(t *testing.T) {
	src := `
use utoipa_axum::router::OpenApiRouter;
use utoipa_axum::routes;

// Attributes for these two live in their own modules, not in this file.
use crate::orders::create_order;
use crate::sessions::drop_session;

#[utoipa::path(get, path = "/items")]
async fn list_items() -> &'static str { "[]" }

#[utoipa::path(post, path = "/items")]
async fn create_item() -> &'static str { "{}" }

pub fn router() -> OpenApiRouter {
    OpenApiRouter::new().routes(routes!(create_order, list_items, drop_session, create_item))
}
`
	ids, res := runDetect(t, "rust", "src/api.rs", src)
	requireContains(t, ids, []string{
		"http:GET:/items",
		"http:POST:/items",
	}, "utoipa-axum-mixed-arguments")

	for _, h := range []string{"list_items", "create_item"} {
		if got := countDefsForHandler(res, h); got != 1 {
			t.Errorf("utoipa-axum-mixed-arguments: want exactly 1 definition for %s, got %d", h, got)
		}
		if got := frameworkForHandler(res, h); got != "utoipa_axum" {
			t.Errorf("utoipa-axum-mixed-arguments: %s must be minted by the utoipa_axum pass, got framework=%q", h, got)
		}
	}
	// The imported handlers stay unminted: fabricating a path for a handler
	// whose contract this pass cannot read is worse than the gap.
	for _, h := range []string{"create_order", "drop_session"} {
		if got := countDefsForHandler(res, h); got != 0 {
			t.Errorf("utoipa-axum-mixed-arguments: want 0 definitions for imported %s, got %d", h, got)
		}
	}
}

// ---------------------------------------------------------------------------
// #6560 Arm B2a — OpenApiRouter::nest("/prefix", …) prefix composition.
//
// Before B2a the utoipa_axum pass minted the bare attribute path regardless of
// any nest that mounted the router, so a service mounted at /api reported its
// routes at the root. These fixtures pin the composed path AND the producer:
// every one asserts framework=utoipa_axum, because utoipaRegisteredElsewhere
// hands a handler to synthesizeAxumRoutes or synthesizeRocket whenever they
// also register it, and a fixture that let that happen would observe the axum
// pass's nest handling rather than this one's.
// ---------------------------------------------------------------------------

// requireUtoipaDef asserts that id exists as an http_endpoint_definition minted
// by THIS pass (framework=utoipa_axum) for the named handler. The framework
// stamp is the premise guard: without it the assertion passes when
// synthesizeAxumRoutes or synthesizeRocket minted the same ID.
func requireUtoipaDef(t *testing.T, res *DetectResult, id, handler, label string) {
	t.Helper()
	for _, e := range res.Entities {
		if e.ID != id || e.Kind != httpEndpointDefinitionKind {
			continue
		}
		if e.Properties["framework"] != "utoipa_axum" {
			t.Errorf("%s: %s was minted by framework=%q, want utoipa_axum — this fixture is not observing the pass under test",
				label, id, e.Properties["framework"])
			return
		}
		if e.Properties["source_handler"] != "Controller:"+handler {
			t.Errorf("%s: %s has source_handler=%q, want Controller:%s",
				label, id, e.Properties["source_handler"], handler)
			return
		}
		return
	}
	var got []string
	for _, e := range res.Entities {
		if e.Kind == httpEndpointDefinitionKind {
			got = append(got, e.ID)
		}
	}
	t.Errorf("%s: want http_endpoint_definition %s from framework=utoipa_axum, got %v", label, id, got)
}

// TestUtoipaAxum_NestPrefixInlineChain covers the inline-chain strategy: the
// .nest("/api", …) precedes the routes!() it mounts inside the same builder
// expression.
func TestUtoipaAxum_NestPrefixInlineChain(t *testing.T) {
	src := `
use utoipa_axum::router::OpenApiRouter;
use utoipa_axum::routes;

#[utoipa::path(get, path = "/items")]
async fn list_items() -> &'static str { "[]" }

#[utoipa::path(post, path = "/items")]
async fn create_item() -> &'static str { "{}" }

pub fn router() -> OpenApiRouter {
    OpenApiRouter::new()
        .nest("/api", OpenApiRouter::new()
            .routes(routes!(list_items, create_item)))
}
`
	ids, res := runDetect(t, "rust", "src/api.rs", src)
	requireUtoipaDef(t, res, "http:GET:/api/items", "list_items", "utoipa-nest-inline")
	requireUtoipaDef(t, res, "http:POST:/api/items", "create_item", "utoipa-nest-inline")
	// The unprefixed path is the pre-B2a output and must be gone, not merely
	// accompanied: an additive fix would double-mint one handler.
	requireNotContains(t, ids, []string{"http:GET:/items", "http:POST:/items"}, "utoipa-nest-inline")
	if got := countDefsForHandler(res, "list_items"); got != 1 {
		t.Errorf("utoipa-nest-inline: want exactly 1 definition for list_items, got %d", got)
	}
}

// TestUtoipaAxum_NestPrefixOuterFunction covers the outer-function strategy:
// the routes!() lives in a helper function written BEFORE the .nest() that
// mounts it.
func TestUtoipaAxum_NestPrefixOuterFunction(t *testing.T) {
	src := `
use utoipa_axum::router::OpenApiRouter;
use utoipa_axum::routes;

#[utoipa::path(get, path = "/{id}")]
async fn get_item() -> &'static str { "{}" }

fn items_router() -> OpenApiRouter {
    OpenApiRouter::new().routes(routes!(get_item))
}

pub fn router() -> OpenApiRouter {
    OpenApiRouter::new().nest("/items", items_router())
}
`
	ids, res := runDetect(t, "rust", "src/items.rs", src)
	requireUtoipaDef(t, res, "http:GET:/items/{id}", "get_item", "utoipa-nest-outer")
	requireNotContains(t, ids, []string{"http:GET:/{id}"}, "utoipa-nest-outer")
}

// TestUtoipaAxum_NestPrefixNearestOfTwo pins WHICH nest is chosen when two are
// both in range and both pass the Router::new() scope test. The nearer one by
// byte distance wins; picking the first, the last, or the outermost yields
// /admin/items and fails here.
func TestUtoipaAxum_NestPrefixNearestOfTwo(t *testing.T) {
	src := `
use utoipa_axum::router::OpenApiRouter;
use utoipa_axum::routes;

#[utoipa::path(get, path = "/items")]
async fn list_items() -> &'static str { "[]" }

fn admin_router() -> OpenApiRouter { OpenApiRouter::new() }

pub fn router() -> OpenApiRouter {
    OpenApiRouter::new()
        .nest("/admin", admin_router())
        .nest("/api", OpenApiRouter::new().routes(routes!(list_items)))
}
`
	ids, res := runDetect(t, "rust", "src/api.rs", src)
	requireUtoipaDef(t, res, "http:GET:/api/items", "list_items", "utoipa-nest-nearest")
	requireNotContains(t, ids, []string{"http:GET:/admin/items", "http:GET:/items"}, "utoipa-nest-nearest")
}

// utoipaNestWindowSrc builds an outer-function fixture whose routes!() and
// .nest() are separated by roughly padBytes of filler, so the rustNestWindow
// bound can be observed from both sides.
func utoipaNestWindowSrc(padBytes int) string {
	const line = "// filler line to space the nest away from the routes! macro\n"
	pad := strings.Repeat(line, padBytes/len(line)+1)
	return `
use utoipa_axum::router::OpenApiRouter;
use utoipa_axum::routes;

#[utoipa::path(get, path = "/items")]
async fn list_items() -> &'static str { "[]" }

fn items_router() -> OpenApiRouter {
    OpenApiRouter::new().routes(routes!(list_items))
}

` + pad + `
pub fn router() -> OpenApiRouter {
    OpenApiRouter::new().nest("/api", items_router())
}
`
}

// utoipaNestGap measures the byte distance the pass actually sees between the
// routes!() macro and the .nest() call in src. The window tests assert this
// bracket explicitly so a fixture that drifts across rustNestWindow fails as a
// broken premise rather than silently stopping to test anything.
func utoipaNestGap(t *testing.T, src string) int {
	t.Helper()
	r := strings.Index(src, "routes!(")
	n := strings.Index(src, ".nest(")
	if r < 0 || n < 0 || n < r {
		t.Fatalf("utoipa-nest-window: malformed fixture (routes!=%d .nest=%d)", r, n)
	}
	return n - r
}

// TestUtoipaAxum_NestPrefixWithinWindow: a nest just INSIDE rustNestWindow
// still mounts the route. Narrowing the window drops the prefix and fails.
func TestUtoipaAxum_NestPrefixWithinWindow(t *testing.T) {
	src := utoipaNestWindowSrc(1750)
	if gap := utoipaNestGap(t, src); gap <= 1500 || gap >= rustNestWindow {
		t.Fatalf("utoipa-nest-window-in: fixture gap %d must sit in (1500, %d) to bracket the window", gap, rustNestWindow)
	}
	_, res := runDetect(t, "rust", "src/wide.rs", src)
	requireUtoipaDef(t, res, "http:GET:/api/items", "list_items", "utoipa-nest-window-in")
}

// TestUtoipaAxum_NestPrefixBeyondWindow: a nest just OUTSIDE rustNestWindow
// does NOT mount the route — the endpoint stays unprefixed rather than being
// captured by an unrelated nest elsewhere in the file. Widening the window
// yields /api/items and fails.
func TestUtoipaAxum_NestPrefixBeyondWindow(t *testing.T) {
	src := utoipaNestWindowSrc(2250)
	if gap := utoipaNestGap(t, src); gap <= rustNestWindow || gap >= 2600 {
		t.Fatalf("utoipa-nest-window-out: fixture gap %d must sit in (%d, 2600) to bracket the window", gap, rustNestWindow)
	}
	ids, res := runDetect(t, "rust", "src/far.rs", src)
	requireUtoipaDef(t, res, "http:GET:/items", "list_items", "utoipa-nest-window-out")
	requireNotContains(t, ids, []string{"http:GET:/api/items"}, "utoipa-nest-window-out")
}

// TestUtoipaAxum_NestPrefixNotAcrossRouterScope pins the "at most one
// Router::new() between" clause: two independent routers, each built with its
// own OpenApiRouter::new(), must not have the second one's nest reach back to
// the first one's routes!.
func TestUtoipaAxum_NestPrefixNotAcrossRouterScope(t *testing.T) {
	src := `
use utoipa_axum::router::OpenApiRouter;
use utoipa_axum::routes;

#[utoipa::path(get, path = "/health")]
async fn health() -> &'static str { "ok" }

pub fn public_router() -> OpenApiRouter {
    OpenApiRouter::new().routes(routes!(health))
}

fn admin_inner() -> OpenApiRouter {
    OpenApiRouter::new()
}

pub fn admin_router() -> OpenApiRouter {
    OpenApiRouter::new().nest("/admin", admin_inner())
}
`
	ids, res := runDetect(t, "rust", "src/scopes.rs", src)
	requireUtoipaDef(t, res, "http:GET:/health", "health", "utoipa-nest-scope")
	requireNotContains(t, ids, []string{"http:GET:/admin/health"}, "utoipa-nest-scope")
}

// TestUtoipaAxum_NoNestUnchanged is the control: with no .nest() anywhere, the
// attribute path is minted verbatim, exactly as Arm A shipped it.
func TestUtoipaAxum_NoNestUnchanged(t *testing.T) {
	src := `
use utoipa_axum::router::OpenApiRouter;
use utoipa_axum::routes;

#[utoipa::path(get, path = "/items")]
async fn list_items() -> &'static str { "[]" }

pub fn router() -> OpenApiRouter {
    OpenApiRouter::new().routes(routes!(list_items))
}
`
	_, res := runDetect(t, "rust", "src/plain.rs", src)
	requireUtoipaDef(t, res, "http:GET:/items", "list_items", "utoipa-nest-none")
}

// TestUtoipaAxum_NestPrefixNotAcrossRouterScope_WithOpenapi is the twin of
// TestUtoipaAxum_NestPrefixNotAcrossRouterScope written in utoipa-axum's
// CANONICAL constructor spelling.
//
// `OpenApiRouter::with_openapi(ApiDoc::openapi())` is how a utoipa_axum service
// that actually serves an OpenAPI document builds its router — `::new()` is the
// spelling that omits the document. The scope guard originally counted the
// literal substring `Router::new()`, which `with_openapi` does not contain, so
// the two independent routers below read as ONE scope: the /admin nest reached
// back across `admin_inner` to `routes!(health)` and minted http:GET:/admin/health
// while http:GET:/health disappeared. A phantom path replacing a real one, on
// input the pre-B2a code handled correctly.
//
// The sibling test above is deliberately KEPT in the `::new()` spelling. Pinning
// the guard in only one spelling is what let this through, so both are pinned.
func TestUtoipaAxum_NestPrefixNotAcrossRouterScope_WithOpenapi(t *testing.T) {
	src := `
use utoipa_axum::router::OpenApiRouter;
use utoipa_axum::routes;

#[utoipa::path(get, path = "/health")]
async fn health() -> &'static str { "ok" }

pub fn public_router() -> OpenApiRouter {
    OpenApiRouter::with_openapi(ApiDoc::openapi()).routes(routes!(health))
}

fn admin_inner() -> OpenApiRouter {
    OpenApiRouter::with_openapi(ApiDoc::openapi())
}

pub fn admin_router() -> OpenApiRouter {
    OpenApiRouter::with_openapi(ApiDoc::openapi()).nest("/admin", admin_inner())
}
`
	ids, res := runDetect(t, "rust", "src/scopes_openapi.rs", src)
	requireUtoipaDef(t, res, "http:GET:/health", "health", "utoipa-nest-scope-with-openapi")
	requireNotContains(t, ids, []string{"http:GET:/admin/health"}, "utoipa-nest-scope-with-openapi")
}

// TestUtoipaAxum_NestPrefixNotAcrossRouterScope_Default covers the third
// constructor in the stated inventory above rustRouterCtorRe: both axum's
// Router and utoipa-axum's OpenApiRouter implement Default, so
// `OpenApiRouter::default()` builds an empty router with neither `new` nor
// `with_openapi` in the text.
func TestUtoipaAxum_NestPrefixNotAcrossRouterScope_Default(t *testing.T) {
	src := `
use utoipa_axum::router::OpenApiRouter;
use utoipa_axum::routes;

#[utoipa::path(get, path = "/health")]
async fn health() -> &'static str { "ok" }

pub fn public_router() -> OpenApiRouter {
    OpenApiRouter::default().routes(routes!(health))
}

fn admin_inner() -> OpenApiRouter {
    OpenApiRouter::default()
}

pub fn admin_router() -> OpenApiRouter {
    OpenApiRouter::default().nest("/admin", admin_inner())
}
`
	ids, res := runDetect(t, "rust", "src/scopes_default.rs", src)
	requireUtoipaDef(t, res, "http:GET:/health", "health", "utoipa-nest-scope-default")
	requireNotContains(t, ids, []string{"http:GET:/admin/health"}, "utoipa-nest-scope-default")
}

// TestUtoipaAxum_NestPrefixTurbofishScope covers the turbofish spelling
// `OpenApiRouter::<AppState>::new()`, which a stateful router requires when the
// state type cannot be inferred. The literal substring `Router::new()` is absent
// there too — the type argument sits between `Router` and `::new()`.
func TestUtoipaAxum_NestPrefixTurbofishScope(t *testing.T) {
	src := `
use utoipa_axum::router::OpenApiRouter;
use utoipa_axum::routes;

#[utoipa::path(get, path = "/health")]
async fn health() -> &'static str { "ok" }

pub fn public_router() -> OpenApiRouter<AppState> {
    OpenApiRouter::<AppState>::new().routes(routes!(health))
}

fn admin_inner() -> OpenApiRouter<AppState> {
    OpenApiRouter::<AppState>::new()
}

pub fn admin_router() -> OpenApiRouter<AppState> {
    OpenApiRouter::<AppState>::new().nest("/admin", admin_inner())
}
`
	ids, res := runDetect(t, "rust", "src/scopes_turbofish.rs", src)
	requireUtoipaDef(t, res, "http:GET:/health", "health", "utoipa-nest-scope-turbofish")
	requireNotContains(t, ids, []string{"http:GET:/admin/health"}, "utoipa-nest-scope-turbofish")
}

// TestUtoipaAxum_NestPrefixWithOpenapiStillComposes is the positive control for
// the fix: widening the constructor inventory must not cost the ordinary
// single-scope case its prefix. One router, built with with_openapi, nesting a
// helper — the prefix still applies.
func TestUtoipaAxum_NestPrefixWithOpenapiStillComposes(t *testing.T) {
	src := `
use utoipa_axum::router::OpenApiRouter;
use utoipa_axum::routes;

#[utoipa::path(get, path = "/items")]
async fn list_items() -> &'static str { "[]" }

pub fn router() -> OpenApiRouter {
    OpenApiRouter::with_openapi(ApiDoc::openapi())
        .nest("/api", OpenApiRouter::with_openapi(ApiDoc::openapi())
            .routes(routes!(list_items)))
}
`
	ids, res := runDetect(t, "rust", "src/compose_openapi.rs", src)
	requireUtoipaDef(t, res, "http:GET:/api/items", "list_items", "utoipa-nest-openapi-composes")
	requireNotContains(t, ids, []string{"http:GET:/items"}, "utoipa-nest-openapi-composes")
}

// utoipaNestExactGapSrc builds the outer-function fixture with the byte gap
// between `routes!(` and `.nest(` set EXACTLY to gap, by measuring the
// unpadded source and inserting precisely the shortfall as comment filler.
// The caller asserts the achieved gap, so a drift in the surrounding source
// fails as a broken premise rather than silently testing a different distance.
func utoipaNestExactGapSrc(t *testing.T, gap int) string {
	t.Helper()
	build := func(pad string) string {
		return `
use utoipa_axum::router::OpenApiRouter;
use utoipa_axum::routes;

#[utoipa::path(get, path = "/items")]
async fn list_items() -> &'static str { "[]" }

fn items_router() -> OpenApiRouter {
    OpenApiRouter::new().routes(routes!(list_items))
}
` + pad + `
pub fn router() -> OpenApiRouter {
    OpenApiRouter::new().nest("/api", items_router())
}
`
	}
	base := utoipaNestGap(t, build(""))
	short := gap - base
	if short < 4 {
		t.Fatalf("utoipa-nest-exact-gap: unpadded gap %d already exceeds target %d", base, gap)
	}
	// "//" + (short-3) filler chars + "\n" is exactly `short` bytes.
	return build("//" + strings.Repeat("x", short-3) + "\n")
}

// TestUtoipaAxum_NestPrefixAtExactWindow pins the BOUNDARY BYTE of the window
// comparison, not just the constant: at a gap of exactly rustNestWindow the
// nest still mounts the route, because the test is `dist > rustNestWindow`.
//
// This exists because `>` → `>=` survived the first round of scoring — the
// bracketing fixtures either side of the window pin its magnitude but leave the
// boundary itself unobserved. The `== rustNestWindow` assertion below is the
// premise guard: if the fixture ever drifts off the boundary it fails loudly
// rather than quietly re-testing the interior.
func TestUtoipaAxum_NestPrefixAtExactWindow(t *testing.T) {
	src := utoipaNestExactGapSrc(t, rustNestWindow)
	if gap := utoipaNestGap(t, src); gap != rustNestWindow {
		t.Fatalf("utoipa-nest-window-exact: fixture gap %d, want exactly %d", gap, rustNestWindow)
	}
	_, res := runDetect(t, "rust", "src/exact.rs", src)
	requireUtoipaDef(t, res, "http:GET:/api/items", "list_items", "utoipa-nest-window-exact")
}

// ---------------------------------------------------------------------------
// #6643 — the three narrower shapes the file header records (at
// http_endpoint_utoipa_axum.go:94-105) as failing to match utoipaRoutesMacroRe.
//
// These are DELIBERATE Arm-A/B1 boundaries, not defects. All three fail in the
// safe direction: they mint nothing rather than minting a guess, and none is a
// regression from Arm A. #6643 does not ask for any of them to be fixed — it
// asks for them to be OBSERVED, because an unobserved limitation cannot be
// told apart from an unobserved regression. Whoever widens the pattern to
// admit one of these shapes will fail the matching case here and can then
// decide deliberately, with a review, instead of discovering it from a
// phantom endpoint in a real graph.
//
// The permissive direction is the dangerous one. For a fail-to-mint shape the
// mutant that survives silently is the one that STARTS minting, so every case
// below is written to be killed by a mint: the offending macro names handlers
// that DO have a same-file `#[utoipa::path]` contract, so a widened pattern
// mints them immediately and the assertion fires.
// ---------------------------------------------------------------------------

// utoipaFailToMintSrc wraps the macro spelling under test in a file that also
// carries a CONTROL registration — `routes!(health)`, the adjacent-bang
// bare-identifier form this pass does read.
//
// The control is the premise guard, and it is load-bearing twice over:
//
//   - utoipaHasRoutesMacro returns early unless the literal substring
//     `routes!` appears anywhere in the file. The `routes ! ( a , b )` case
//     does not contain it, so WITHOUT the control that fixture would observe
//     the early return and would keep passing even if the regex were widened
//     to match whitespace before the bang. The control is what forces the
//     macro-matching branch to actually run.
//   - Asserting the control's endpoint through requireUtoipaDef proves this
//     pass — framework=utoipa_axum, source_handler=Controller:health — is the
//     producer, so a green result cannot come from the file failing to parse,
//     from an empty attribute map, or from another synthesiser.
//
// list_items and create_item both carry a same-file attribute, so they are
// unminted here for exactly one reason: the macro naming them did not match.
func utoipaFailToMintSrc(macro string) string {
	return `
use utoipa_axum::router::OpenApiRouter;
use utoipa_axum::routes;

#[utoipa::path(get, path = "/health")]
async fn health() -> &'static str { "ok" }

#[utoipa::path(get, path = "/items")]
async fn list_items() -> &'static str { "[]" }

#[utoipa::path(post, path = "/items")]
async fn create_item() -> &'static str { "{}" }

pub fn router() -> OpenApiRouter {
    OpenApiRouter::new()
        .routes(routes!(health))
        .routes(` + macro + `)
}
`
}

func TestUtoipaAxum_HeaderRecordedShapesMintNothing(t *testing.T) {
	cases := []struct {
		name string
		// macro is the spelling under test, substituted into the fixture.
		macro string
		// mustContain / mustNotContain pin the incidental bytes the kill
		// depends on. Each of these shapes fails to match because of ONE
		// character or one space; if a later edit normalises that byte away
		// the fixture would go on passing while observing nothing at all.
		mustContain    []string
		mustNotContain []string
	}{
		{
			// A block comment between arguments. The argument list admits
			// only `ident (, ident)*`, so `/*` cannot appear anywhere inside
			// it and the WHOLE macro fails — not just the argument it
			// precedes.
			name:           "block-comment-between-arguments",
			macro:          "routes!(list_items, /* mount both */ create_item)",
			mustContain:    []string{"/* mount both */"},
			mustNotContain: []string{"routes!(list_items, create_item)"},
		},
		{
			// The line-comment spelling of the same shape, in the rustfmt
			// multi-line layout where it actually occurs. `\s` spans newlines,
			// so the multi-line form is otherwise read normally — the comment
			// is the only reason this one fails, and a widening aimed at the
			// block form must not quietly admit this one either.
			name: "line-comment-between-arguments",
			macro: `routes!(
            list_items, // the list endpoint
            create_item,
        )`,
			mustContain:    []string{"// the list endpoint"},
			mustNotContain: []string{"/*"},
		},
		{
			// A raw identifier. `#` is outside `\w`, so `r#type` is not a bare
			// identifier and the macro carrying it fails WHOLE — which is why
			// its bare siblings list_items and create_item stay unminted too.
			name:           "raw-identifier-argument",
			macro:          "routes!(r#type, list_items, create_item)",
			mustContain:    []string{"r#type"},
			mustNotContain: []string{"routes!(list_items"},
		},
		{
			// Whitespace before the bang. `routes ! ( a , b )` is Rust-legal
			// but the pattern requires `routes!` adjacent. Note this fixture
			// contains NO `routes!(` of its own: the control macro is the only
			// reason the pass gets past utoipaHasRoutesMacro at all.
			name:           "whitespace-before-bang",
			macro:          "routes ! ( list_items , create_item )",
			mustContain:    []string{"routes ! ( list_items , create_item )"},
			mustNotContain: []string{"routes!(list_items", "routes!( list_items"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			label := "utoipa-fail-to-mint-" + tc.name
			src := utoipaFailToMintSrc(tc.macro)

			for _, want := range tc.mustContain {
				if !strings.Contains(src, want) {
					t.Fatalf("%s: fixture lost the byte sequence its result depends on: %q missing", label, want)
				}
			}
			for _, unwanted := range tc.mustNotContain {
				if strings.Contains(src, unwanted) {
					t.Fatalf("%s: fixture contains %q, which this shape must not spell — it would mint for an unrelated reason", label, unwanted)
				}
			}

			ids, res := runDetect(t, "rust", "src/api.rs", src)

			// Premise guard: the pass ran, read this file's attribute map and
			// minted through the branch under test.
			requireUtoipaDef(t, res, "http:GET:/health", "health", label)

			// The shape under test mints nothing. Both handlers have a
			// same-file contract, so a non-zero count here means the pattern
			// started matching a shape the header says it does not.
			requireNotContains(t, ids, []string{"http:GET:/items", "http:POST:/items"}, label)
			for _, h := range []string{"list_items", "create_item"} {
				if got := countDefsForHandler(res, h); got != 0 {
					t.Errorf("%s: want 0 definitions for %s, got %d — this shape is documented as minting nothing (http_endpoint_utoipa_axum.go:94-105); if that changed deliberately, change the header and the coverage doc with it",
						label, h, got)
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// #6672 — the macro-NAME boundary: the leading `\b` of utoipaRoutesMacroRe.
//
// This is a FOURTH fail-to-mint shape, and the only one of the four whose loss
// would be permissive rather than conservative. Dropping the `\b` leaves the
// pattern matching any macro whose name merely ENDS in `routes!`, and
// `my_routes!` / `app_routes!` / `admin_routes!` are ordinary user-defined
// wrapper macros. Every one of them would then mint one endpoint per argument
// for a registration this pass never saw — the phantom-endpoint direction, and
// the one this pass is otherwise most careful about.
//
// The control macro `routes!(health)` plays a DIFFERENT role here than it does
// in TestUtoipaAxum_HeaderRecordedShapesMintNothing, and carrying that
// rationale across would be wrong. There the control exists to defeat
// utoipaHasRoutesMacro's early return: `routes ! ( a , b )` does not contain
// the substring "routes!", so without a control that fixture would never reach
// the regex at all. Here the control does NOT play that role — `my_routes!`
// does contain the substring "routes!", and utoipaHasRoutesMacro is a plain
// strings.Contains, so the pre-filter passes and the macro-matching branch runs
// whether or not a control is present. The control is kept for the other reason
// only: it is the PRODUCER premise. Asserting http:GET:/health through
// requireUtoipaDef proves the file parsed, the attribute map was built from it,
// and framework=utoipa_axum is what minted — so "0 definitions for list_items"
// is a decision this pass made, not the silence of a fixture that never ran.
//
// The rows vary the PREFIX rather than repeating one literal spelling: the pin
// is on the family (any word character immediately before `routes`), so a
// widening that special-cased `my_` could not pass while `app_`/`admin_` fail.
// ---------------------------------------------------------------------------
func TestUtoipaAxum_PrefixedRoutesMacroMintsNothing(t *testing.T) {
	for _, prefix := range []string{"my_", "app_", "admin_"} {
		t.Run(prefix+"routes", func(t *testing.T) {
			label := "utoipa-prefixed-macro-" + prefix + "routes"
			src := utoipaFailToMintSrc(prefix + "routes!(list_items, create_item)")

			// The incidental byte the whole kill rests on is the prefix. If a
			// later edit normalised it away the fixture would keep reporting
			// green while observing nothing at all.
			if want := prefix + "routes!(list_items"; !strings.Contains(src, want) {
				t.Fatalf("%s: fixture lost the prefix its result depends on: %q missing", label, want)
			}
			// …and the only BARE `routes!(` in the fixture must be the
			// control's. `.routes(` followed by a prefixed macro never spells
			// `(routes!(`, so a count above 1 would mean the shape under test
			// had smuggled in a genuine registration and the 0-counts below
			// would be trivially true.
			if n := strings.Count(src, "(routes!("); n != 1 {
				t.Fatalf("%s: want exactly 1 bare `routes!(` in the fixture (the control), got %d", label, n)
			}

			ids, res := runDetect(t, "rust", "src/api.rs", src)

			// Producer premise — see the block comment above: this is what the
			// control is for here, and it is NOT about the early return.
			requireUtoipaDef(t, res, "http:GET:/health", "health", label)

			// Both handlers carry a same-file `#[utoipa::path]`, so the only
			// thing keeping them unminted is the macro's NAME.
			requireNotContains(t, ids, []string{"http:GET:/items", "http:POST:/items"}, label)
			for _, h := range []string{"list_items", "create_item"} {
				if got := countDefsForHandler(res, h); got != 0 {
					t.Errorf("%s: %sroutes!(…) minted %d definition(s) for %s, want 0 — a macro whose name merely ends in `routes!` is a user-defined wrapper, not a utoipa registration; the leading word boundary in utoipaRoutesMacroRe is what says so",
						label, prefix, got, h)
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// #6656 — the three router-construction spellings rustRouterCtorRe's stated
// inventory (http_endpoint_axum.go:83-96) records as DELIBERATELY NOT matched.
//
// The decision was prose and nothing observed it: adding `from` to the
// alternation — or dropping the `Router` suffix requirement so an alias counts,
// or teaching the count to see a helper call — survived the whole package. Which
// means a later reader who reasonably decides `Api::new()` "is obviously a
// router construction" would move a settled trade-off with nothing objecting.
//
// These are known-limitation tests. They assert TODAY'S output, not the ideal
// one: for the alias and the helper shapes that output is a PREFIX REACHING
// ACROSS two genuinely separate routers, which the header calls out as the
// permissive miss. They are pinned anyway because the direction makes it cheap —
// counting more constructors only makes the guard stricter, so moving any of
// these lines costs a prefix and can never invent an endpoint. Whoever moves one
// should do it with a review, not discover it from a vanished prefix.
//
// Every row is written so the SAME assertion is both the pin and the mutant
// detector: each fixture sits exactly one construction away from the
// `rustRouterCtorCount(between) > 1` threshold, so the shape under test flipping
// from uncounted to counted flips the emitted ID.
// ---------------------------------------------------------------------------

// utoipaCtorWindow returns the byte span rustNestPrefixFor actually inspects for
// this fixture — the text between the `routes!` registration and the `.nest(`
// that may mount it — after proving the fixture is shaped the way the row
// assumes.
//
// This is the premise guard the rows share. Without it a fixture that drifted
// (the nest moving before the macro, past rustNestWindow, or the constructor
// spelling landing OUTSIDE the inspected span) would go on passing while
// observing the window clause rather than the construction count.
func utoipaCtorWindow(t *testing.T, src, label string) string {
	t.Helper()
	route := strings.Index(src, "routes!(")
	nest := strings.Index(src, `.nest("`)
	if route < 0 || nest < 0 {
		t.Fatalf("%s: malformed fixture (routes!=%d .nest=%d)", label, route, nest)
	}
	if nest <= route {
		t.Fatalf("%s: fixture puts .nest( at %d before routes!( at %d — these rows are all outer-mount shaped", label, nest, route)
	}
	if gap := nest - route; gap > rustNestWindow {
		t.Fatalf("%s: fixture gap %d exceeds rustNestWindow %d — the window clause, not the construction count, would decide this row", label, gap, rustNestWindow)
	}
	return src[route:nest]
}

func TestUtoipaAxum_UncountedRouterCtorShapes(t *testing.T) {
	// windowLit pins one incidental byte sequence inside the inspected span,
	// by exact count. A row's result rests entirely on WHICH constructions
	// fall in that span, so an edit that adds or removes one must fail here
	// rather than silently leave the row observing nothing.
	type windowLit struct {
		lit string
		n   int
	}
	cases := []struct {
		name string
		src  string
		// window pins the inspected span's contents, in source order.
		window []windowLit
		// srcMustContain pins bytes the row depends on that live OUTSIDE the
		// inspected span.
		srcMustContain []string
		wantID         string
		wantHandler    string
		notWantIDs     []string
	}{
		{
			// `OpenApiRouter::from(axum_router)` CONVERTS an existing router
			// rather than starting a chain, so counting it would split a scope
			// that is genuinely one. The span below holds exactly one counted
			// construction, so the prefix applies; counting `from` too makes it
			// two and /api/items becomes /items.
			name: "from-conversion",
			src: `
use utoipa_axum::router::OpenApiRouter;
use utoipa_axum::routes;

#[utoipa::path(get, path = "/items")]
async fn list_items() -> &'static str { "[]" }

fn items_router() -> OpenApiRouter {
    OpenApiRouter::new().routes(routes!(list_items))
}

pub fn router() -> OpenApiRouter {
    OpenApiRouter::new()
        .merge(OpenApiRouter::from(legacy_axum_router()))
        .nest("/api", items_router())
}
`,
			window: []windowLit{
				{"OpenApiRouter::from(", 1},
				{"OpenApiRouter::new()", 1},
			},
			wantID:      "http:GET:/api/items",
			wantHandler: "list_items",
			notWantIDs:  []string{"http:GET:/items"},
		},
		{
			// A rename-import erases the `Router` suffix, so nothing in the
			// text identifies `Api` as a router without resolving the import.
			// The two routers below are genuinely separate and the span holds
			// two constructions — but zero COUNTED ones, so the /admin nest
			// reaches back across admin_inner and replaces the real
			// GET /health with GET /admin/health.
			//
			// That phantom is what is pinned here. It is a real miss, recorded
			// deliberately: see the header note above for why it is left.
			name: "rename-import-alias",
			src: `
use utoipa_axum::router::OpenApiRouter as Api;
use utoipa_axum::routes;

#[utoipa::path(get, path = "/health")]
async fn health() -> &'static str { "ok" }

pub fn public_router() -> Api {
    Api::new().routes(routes!(health))
}

fn admin_inner() -> Api {
    Api::new()
}

pub fn admin_router() -> Api {
    Api::new().nest("/admin", admin_inner())
}
`,
			window: []windowLit{
				{"Api::new()", 2},
				// The alias is the whole point: if the fixture ever spells a
				// counted constructor in the span the row stops observing the
				// alias and starts observing the ordinary scope guard.
				{"Router::new", 0},
				{"Router::default", 0},
				{"Router::with_openapi", 0},
			},
			srcMustContain: []string{"OpenApiRouter as Api"},
			wantID:         "http:GET:/admin/health",
			wantHandler:    "health",
			notWantIDs:     []string{"http:GET:/health"},
		},
		{
			// A constructor returned by a user helper. `base()` builds the
			// router, but the only counted spelling in the file sits INSIDE
			// base's body, above the registration and therefore outside the
			// inspected span — so the span again holds zero counted
			// constructions and the /admin nest reaches across.
			name: "constructor-behind-user-helper",
			src: `
use utoipa_axum::router::OpenApiRouter;
use utoipa_axum::routes;

#[utoipa::path(get, path = "/health")]
async fn health() -> &'static str { "ok" }

fn base() -> OpenApiRouter {
    OpenApiRouter::new()
}

pub fn public_router() -> OpenApiRouter {
    base().routes(routes!(health))
}

fn admin_inner() -> OpenApiRouter {
    base().merge(extras())
}

pub fn admin_router() -> OpenApiRouter {
    base()
        .layer(auth_layer())
        .nest("/admin", admin_inner())
}
`,
			window: []windowLit{
				{"base()", 2},
				// The counted construction is in base's body, ABOVE the
				// registration — srcMustContain proves it exists, this proves
				// it is not in the span.
				{"OpenApiRouter::new()", 0},
			},
			srcMustContain: []string{"fn base() -> OpenApiRouter {\n    OpenApiRouter::new()\n}"},
			wantID:         "http:GET:/admin/health",
			wantHandler:    "health",
			notWantIDs:     []string{"http:GET:/health"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			label := "utoipa-uncounted-ctor-" + tc.name

			for _, want := range tc.srcMustContain {
				if !strings.Contains(tc.src, want) {
					t.Fatalf("%s: fixture lost %q, which its result depends on", label, want)
				}
			}
			between := utoipaCtorWindow(t, tc.src, label)
			for _, w := range tc.window {
				if got := strings.Count(between, w.lit); got != w.n {
					t.Fatalf("%s: inspected span contains %q %d time(s), want %d — the fixture is no longer one construction from the scope threshold",
						label, w.lit, got, w.n)
				}
			}

			ids, res := runDetect(t, "rust", "src/ctor_shapes.rs", tc.src)

			// Premise guard: the ID below was minted by THIS pass from this
			// file's attribute map, not by synthesizeAxumRoutes or Rocket.
			requireUtoipaDef(t, res, tc.wantID, tc.wantHandler, label)
			requireNotContains(t, ids, tc.notWantIDs, label)
			if got := countDefsForHandler(res, tc.wantHandler); got != 1 {
				t.Errorf("%s: want exactly 1 definition for %s, got %d", label, tc.wantHandler, got)
			}
		})
	}
}
