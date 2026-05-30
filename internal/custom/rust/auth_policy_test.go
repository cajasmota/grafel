package rust_test

// auth_policy_test.go — value-asserting tests for the deep auth + middleware
// extraction (issue #3414). These assert SPECIFIC guard/middleware/layer names
// and the recovered auth policy (auth_method + auth_required) — not len>0 — so
// they justify flipping auth_coverage / middleware_coverage to `full` for the
// flagship Rust frameworks.

import "testing"

// findEntity returns the first entity matching name (and kind, if non-empty).
func findEntity(ents []entitySummary, kind, name string) (entitySummary, bool) {
	for _, e := range ents {
		if e.Name == name && (kind == "" || e.Kind == kind) {
			return e, true
		}
	}
	return entitySummary{}, false
}

func assertProp(t *testing.T, e entitySummary, key, want string) {
	t.Helper()
	got := e.Props[key]
	if got != want {
		t.Errorf("entity %q prop %q = %q, want %q", e.Name, key, got, want)
	}
}

// ---------------------------------------------------------------------------
// axum — middleware::from_fn auth guard
// ---------------------------------------------------------------------------

func TestRustAuthPolicy_AxumFromFnGuard(t *testing.T) {
	src := `
use axum::{Router, middleware};
fn app() -> Router {
    Router::new()
        .route("/admin", get(admin))
        .route_layer(middleware::from_fn(jwt_auth));
}
`
	ents := extract(t, "custom_rust_auth", fi("app.rs", "rust", src))

	// from_fn binds the named middleware fn.
	e, ok := findEntity(ents, "SCOPE.Pattern", "middleware:from_fn:jwt_auth")
	if !ok {
		t.Fatal("expected middleware:from_fn:jwt_auth entity")
	}
	assertProp(t, e, "middleware_name", "jwt_auth")
	assertProp(t, e, "guard_fn", "jwt_auth")

	// And the auth policy for the auth-shaped fn.
	a, ok := findEntity(ents, "SCOPE.Pattern", "auth:from_fn:jwt_auth")
	if !ok {
		t.Fatal("expected auth:from_fn:jwt_auth entity")
	}
	assertProp(t, a, "guard_name", "jwt_auth")
	assertProp(t, a, "auth_method", "jwt")
	assertProp(t, a, "auth_required", "true")
}

// ---------------------------------------------------------------------------
// axum — .route_layer with auth-shaped layer
// ---------------------------------------------------------------------------

func TestRustAuthPolicy_AxumRouteLayer(t *testing.T) {
	src := `
use axum::Router;
fn app() -> Router {
    Router::new().route_layer(RequireBearerToken::new());
}
`
	ents := extract(t, "custom_rust_auth", fi("app.rs", "rust", src))
	e, ok := findEntity(ents, "SCOPE.Pattern", "middleware:route_layer:RequireBearerToken")
	if !ok {
		t.Fatal("expected middleware:route_layer:RequireBearerToken")
	}
	assertProp(t, e, "layer_scope", "route")

	a, ok := findEntity(ents, "SCOPE.Pattern", "auth:route_layer:RequireBearerToken")
	if !ok {
		t.Fatal("expected auth:route_layer:RequireBearerToken")
	}
	assertProp(t, a, "auth_method", "bearer")
	assertProp(t, a, "auth_required", "true")
}

// ---------------------------------------------------------------------------
// tower-http — ValidateRequestHeaderLayer::bearer
// ---------------------------------------------------------------------------

func TestRustAuthPolicy_ValidateRequestHeaderBearer(t *testing.T) {
	src := `
use tower_http::validate_request::ValidateRequestHeaderLayer;
fn app() {
    let layer = ValidateRequestHeaderLayer::bearer("secret-token");
}
`
	ents := extract(t, "custom_rust_auth", fi("app.rs", "rust", src))
	a, ok := findEntity(ents, "SCOPE.Pattern", "auth:validate_request_header:bearer")
	if !ok {
		t.Fatal("expected auth:validate_request_header:bearer")
	}
	assertProp(t, a, "guard_name", "ValidateRequestHeaderLayer::bearer")
	assertProp(t, a, "auth_method", "bearer")
	assertProp(t, a, "auth_required", "true")
}

// ---------------------------------------------------------------------------
// axum — custom extractor guard via FromRequestParts
// ---------------------------------------------------------------------------

func TestRustAuthPolicy_AxumExtractorGuard(t *testing.T) {
	src := `
use axum::extract::FromRequestParts;
#[async_trait]
impl<S> FromRequestParts<S> for AuthUser
where S: Send + Sync {
    type Rejection = StatusCode;
    async fn from_request_parts(parts: &mut Parts, _: &S) -> Result<Self, Self::Rejection> {
        // validate bearer token
        Ok(AuthUser{})
    }
}
`
	ents := extract(t, "custom_rust_auth", fi("guard.rs", "rust", src))
	a, ok := findEntity(ents, "SCOPE.Pattern", "auth:extractor_guard:AuthUser")
	if !ok {
		t.Fatal("expected auth:extractor_guard:AuthUser")
	}
	assertProp(t, a, "guard_name", "AuthUser")
	assertProp(t, a, "guard_kind", "from_request_parts")
	assertProp(t, a, "auth_required", "true")
}

// ---------------------------------------------------------------------------
// tower — ServiceBuilder ordered layer chain
// ---------------------------------------------------------------------------

func TestRustAuthPolicy_TowerLayerChainOrder(t *testing.T) {
	src := `
use tower::ServiceBuilder;
fn app() {
    let svc = ServiceBuilder::new()
        .layer(TraceLayer::new_for_http())
        .layer(CompressionLayer::new())
        .layer(RequireAuthorizationLayer::bearer("tok"));
}
`
	ents := extract(t, "custom_rust_auth", fi("svc.rs", "rust", src))

	// Each layer is enumerated with its source-order index.
	trace, ok := findEntity(ents, "SCOPE.Pattern", "middleware:tower_layer:TraceLayer")
	if !ok {
		t.Fatal("expected middleware:tower_layer:TraceLayer")
	}
	assertProp(t, trace, "layer_order", "0")

	comp, ok := findEntity(ents, "SCOPE.Pattern", "middleware:tower_layer:CompressionLayer")
	if !ok {
		t.Fatal("expected middleware:tower_layer:CompressionLayer")
	}
	assertProp(t, comp, "layer_order", "1")

	authL, ok := findEntity(ents, "SCOPE.Pattern", "middleware:tower_layer:RequireAuthorizationLayer")
	if !ok {
		t.Fatal("expected middleware:tower_layer:RequireAuthorizationLayer")
	}
	assertProp(t, authL, "layer_order", "2")

	// The whole chain is recorded in source order.
	chain, ok := findEntity(ents, "SCOPE.Pattern",
		"middleware:layer_chain:TraceLayer>CompressionLayer>RequireAuthorizationLayer")
	if !ok {
		t.Fatal("expected ordered layer_chain entity")
	}
	assertProp(t, chain, "layer_count", "3")
	assertProp(t, chain, "layer_order_list", "TraceLayer>CompressionLayer>RequireAuthorizationLayer")

	// The bearer authorization layer also yields an auth policy entity.
	a, ok := findEntity(ents, "SCOPE.Pattern", "auth:require_authorization:bearer")
	if !ok {
		t.Fatal("expected auth:require_authorization:bearer")
	}
	assertProp(t, a, "auth_method", "bearer")
	assertProp(t, a, "auth_required", "true")
}

// ---------------------------------------------------------------------------
// actix-web — HttpAuthentication::bearer(validator)
// ---------------------------------------------------------------------------

func TestRustAuthPolicy_ActixHttpAuthBearer(t *testing.T) {
	src := `
use actix_web_httpauth::middleware::HttpAuthentication;
fn app() {
    let auth = HttpAuthentication::bearer(validate_token);
    App::new().wrap(auth);
}
`
	ents := extract(t, "custom_rust_auth", fi("main.rs", "rust", src))
	a, ok := findEntity(ents, "SCOPE.Pattern", "auth:http_authentication:bearer")
	if !ok {
		t.Fatal("expected auth:http_authentication:bearer")
	}
	assertProp(t, a, "guard_name", "HttpAuthentication::bearer")
	assertProp(t, a, "validator_name", "validate_token")
	assertProp(t, a, "auth_method", "bearer")
	assertProp(t, a, "auth_required", "true")
}

// ---------------------------------------------------------------------------
// actix-web — custom Transform middleware
// ---------------------------------------------------------------------------

func TestRustAuthPolicy_ActixTransform(t *testing.T) {
	src := `
use actix_web::dev::{Service, ServiceRequest, Transform};
impl<S, B> Transform<S, ServiceRequest> for JwtAuthMiddleware
where S: Service<ServiceRequest> {
    type Transform = JwtAuthMiddlewareService<S>;
    fn new_transform(&self, service: S) -> Self::Future { todo!() }
}
`
	ents := extract(t, "custom_rust_auth", fi("mw.rs", "rust", src))
	e, ok := findEntity(ents, "SCOPE.Pattern", "middleware:transform:JwtAuthMiddleware")
	if !ok {
		t.Fatal("expected middleware:transform:JwtAuthMiddleware")
	}
	assertProp(t, e, "middleware_trait", "Transform")

	a, ok := findEntity(ents, "SCOPE.Pattern", "auth:transform:JwtAuthMiddleware")
	if !ok {
		t.Fatal("expected auth:transform:JwtAuthMiddleware")
	}
	assertProp(t, a, "auth_method", "jwt")
	assertProp(t, a, "auth_required", "true")
}

// ---------------------------------------------------------------------------
// rocket — fairing + request guard
// ---------------------------------------------------------------------------

func TestRustAuthPolicy_RocketFairingAndGuard(t *testing.T) {
	src := `
use rocket::fairing::{Fairing, Info};
use rocket::request::FromRequest;

#[rocket::async_trait]
impl Fairing for AuthFairing {
    fn info(&self) -> Info { todo!() }
}

#[rocket::async_trait]
impl<'r> FromRequest<'r> for BearerToken {
    type Error = ();
    async fn from_request(req: &'r Request<'_>) -> Outcome<Self, Self::Error> { todo!() }
}
`
	ents := extract(t, "custom_rust_auth", fi("guards.rs", "rust", src))

	mw, ok := findEntity(ents, "SCOPE.Pattern", "middleware:fairing:AuthFairing")
	if !ok {
		t.Fatal("expected middleware:fairing:AuthFairing")
	}
	assertProp(t, mw, "middleware_trait", "Fairing")

	af, ok := findEntity(ents, "SCOPE.Pattern", "auth:fairing:AuthFairing")
	if !ok {
		t.Fatal("expected auth:fairing:AuthFairing")
	}
	assertProp(t, af, "auth_required", "true")

	g, ok := findEntity(ents, "SCOPE.Pattern", "auth:request_guard:BearerToken")
	if !ok {
		t.Fatal("expected auth:request_guard:BearerToken")
	}
	assertProp(t, g, "guard_name", "BearerToken")
	assertProp(t, g, "auth_method", "bearer")
	assertProp(t, g, "auth_required", "true")
}

// ---------------------------------------------------------------------------
// negative — a body extractor is NOT mistaken for an auth guard
// ---------------------------------------------------------------------------

func TestRustAuthPolicy_NonAuthExtractorIgnored(t *testing.T) {
	src := `
impl<S> FromRequest<S> for JsonBody {
    type Rejection = StatusCode;
}
`
	ents := extract(t, "custom_rust_auth", fi("body.rs", "rust", src))
	if _, ok := findEntity(ents, "SCOPE.Pattern", "auth:extractor_guard:JsonBody"); ok {
		t.Error("non-auth extractor JsonBody should not be an auth guard")
	}
	if _, ok := findEntity(ents, "SCOPE.Pattern", "auth:request_guard:JsonBody"); ok {
		t.Error("non-auth extractor JsonBody should not be a request guard")
	}
}
