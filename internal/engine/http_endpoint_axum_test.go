package engine

import (
	"testing"
)

// TestAxum_BasicRoute covers Router::new().route("/path", get(handler))
// and Router::new().route("/path", post(handler)) — the most common axum pattern.
func TestAxum_BasicRoute(t *testing.T) {
	src := `
use axum::{routing::{get, post}, Router};

async fn list_users() -> &'static str { "[]" }
async fn create_user() -> &'static str { "{}" }

pub fn router() -> Router {
    Router::new()
        .route("/users", get(list_users))
        .route("/users", post(create_user))
}
`
	ids, _ := runDetect(t, "rust", "users.rs", src)
	want := []string{
		"http:GET:/users",
		"http:POST:/users",
	}
	requireContains(t, ids, want, "axum-basic-route")
}

// TestAxum_NestPrefix covers .nest("/api", router()) path composition.
func TestAxum_NestPrefix(t *testing.T) {
	src := `
use axum::{routing::get, Router};

async fn health() -> &'static str { "ok" }

pub fn app() -> Router {
    let api = Router::new().route("/health", get(health));
    Router::new().nest("/api", api)
}
`
	ids, _ := runDetect(t, "rust", "app.rs", src)
	want := []string{
		"http:GET:/api/health",
	}
	requireContains(t, ids, want, "axum-nest-prefix")
}

// TestAxum_CurlyBraceParam covers axum path params: /users/{id}.
func TestAxum_CurlyBraceParam(t *testing.T) {
	src := `
use axum::{routing::{get, delete}, Router};

async fn get_user() -> &'static str { "{}" }
async fn delete_user() -> &'static str { "{}" }

pub fn router() -> Router {
    Router::new()
        .route("/users/{id}", get(get_user))
        .route("/users/{id}", delete(delete_user))
}
`
	ids, _ := runDetect(t, "rust", "users.rs", src)
	want := []string{
		"http:GET:/users/{id}",
		"http:DELETE:/users/{id}",
	}
	requireContains(t, ids, want, "axum-curly-param")
}

// TestAxum_AllVerbs covers get/post/put/patch/delete/head/options.
func TestAxum_AllVerbs(t *testing.T) {
	src := `
use axum::{routing::{get, post, put, patch, delete, head, options}, Router};

async fn noop() {}

pub fn router() -> Router {
    Router::new()
        .route("/res", get(noop))
        .route("/res", post(noop))
        .route("/res/{id}", put(noop))
        .route("/res/{id}", patch(noop))
        .route("/res/{id}", delete(noop))
        .route("/res", head(noop))
        .route("/res", options(noop))
}
`
	ids, _ := runDetect(t, "rust", "res.rs", src)
	want := []string{
		"http:GET:/res",
		"http:POST:/res",
		"http:PUT:/res/{id}",
		"http:PATCH:/res/{id}",
		"http:DELETE:/res/{id}",
		"http:HEAD:/res",
		"http:OPTIONS:/res",
	}
	requireContains(t, ids, want, "axum-all-verbs")
}

// TestAxum_HandlerRef verifies source_handler property points at the handler fn.
func TestAxum_HandlerRef(t *testing.T) {
	src := `
use axum::{routing::post, Router};

async fn quote() -> &'static str { "{}" }

pub fn app() -> Router {
    Router::new().route("/quote", post(quote))
}
`
	_, res := runDetect(t, "rust", "pricing.rs", src)
	found := false
	for _, e := range res.Entities {
		if e.ID == "http:POST:/quote" {
			// synthesizeAxumRoutes emits "Controller" as the handler kind so the
			// http-endpoint-resolve pass can map it to SCOPE.Operation (the kind
			// the Rust extractor uses for functions) via resolverKindEquivalents.
			if e.Properties["source_handler"] == "Controller:quote" {
				found = true
			}
		}
	}
	if !found {
		t.Errorf("axum-handler-ref: expected http:POST:/quote with source_handler=Controller:quote")
	}
}

// TestAxum_NestMultipleRoutes covers a nest with multiple inner routes,
// verifying all paths are composed.
func TestAxum_NestMultipleRoutes(t *testing.T) {
	src := `
use axum::{routing::{get, post}, Router};

async fn list() -> &'static str { "[]" }
async fn create() -> &'static str { "{}" }

pub fn orders_router() -> Router {
    Router::new()
        .route("/orders", get(list))
        .route("/orders", post(create))
}

pub fn app() -> Router {
    Router::new().nest("/v1", orders_router())
}
`
	ids, _ := runDetect(t, "rust", "main.rs", src)
	want := []string{
		"http:GET:/v1/orders",
		"http:POST:/v1/orders",
	}
	requireContains(t, ids, want, "axum-nest-multiple")
}

// TestAxum_PricingService is an integration-level test using the ShipFast
// pricing service's main.rs content.
func TestAxum_PricingService(t *testing.T) {
	src := `
use axum::{routing::post, Json, Router};

async fn quote() -> &'static str { "{}" }

#[tokio::main]
async fn main() {
    let app = Router::new().route("/quote", post(quote));
}
`
	ids, _ := runDetect(t, "rust", "main.rs", src)
	want := []string{"http:POST:/quote"}
	requireContains(t, ids, want, "axum-pricing-service")
}
