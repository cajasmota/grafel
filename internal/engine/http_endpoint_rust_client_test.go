package engine

import (
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// reqwest module-level functions
// ---------------------------------------------------------------------------

// TestRustClient_ReqwestFnLiteral covers reqwest::get("url").await and
// reqwest::get("url").await? with an absolute URL.
func TestRustClient_ReqwestFnLiteral(t *testing.T) {
	src := `
use reqwest;

async fn fetch_users() -> Result<reqwest::Response, reqwest::Error> {
    let resp = reqwest::get("https://api.example.com/api/users").await?;
    Ok(resp)
}

async fn create_user(body: &str) -> Result<reqwest::Response, reqwest::Error> {
    let resp = reqwest::post("https://api.example.com/api/users").await?;
    Ok(resp)
}
`
	ids, rels := runDetectWithRels(t, "rust", "users_client.rs", src)
	want := []string{
		"http:GET:/api/users",
		"http:POST:/api/users",
	}
	requireContains(t, ids, want, "rust-reqwest-fn-literal")
	requireFetches(t, rels, "http:GET:/api/users", "rust-reqwest-fn-literal")
	requireFetches(t, rels, "http:POST:/api/users", "rust-reqwest-fn-literal")
}

// TestRustClient_ReqwestClientInstance covers client.get(url).send().await and
// client.post(url).json(&body).send().await patterns.
func TestRustClient_ReqwestClientInstance(t *testing.T) {
	src := `
use reqwest::Client;

async fn fetch_orders() -> Result<(), Box<dyn std::error::Error>> {
    let client = Client::new();
    let resp = client.get("https://api.example.com/api/orders")
        .send()
        .await?;
    Ok(())
}

async fn place_order(body: serde_json::Value) -> Result<(), Box<dyn std::error::Error>> {
    let client = Client::new();
    let resp = client.post("https://api.example.com/api/orders")
        .json(&body)
        .send()
        .await?;
    Ok(())
}
`
	ids, rels := runDetectWithRels(t, "rust", "orders_client.rs", src)
	want := []string{
		"http:GET:/api/orders",
		"http:POST:/api/orders",
	}
	requireContains(t, ids, want, "rust-reqwest-client-instance")
	requireFetches(t, rels, "http:GET:/api/orders", "rust-reqwest-client-instance")
	requireFetches(t, rels, "http:POST:/api/orders", "rust-reqwest-client-instance")
}

// ---------------------------------------------------------------------------
// hyper
// ---------------------------------------------------------------------------

// TestRustClient_HyperRequestBuilder covers Request::builder().method("POST").uri(url)
// and the enum form .method(Method::GET).
func TestRustClient_HyperRequestBuilder(t *testing.T) {
	src := `
use hyper::{Client, Request, Method, Body};

async fn submit_data(body: &str) -> Result<(), Box<dyn std::error::Error>> {
    let req = Request::builder()
        .method("POST")
        .uri("https://api.example.com/api/events")
        .body(Body::from(body.to_owned()))?;
    let client = Client::new();
    let _ = client.request(req).await?;
    Ok(())
}

async fn fetch_config() -> Result<(), Box<dyn std::error::Error>> {
    let req = Request::builder()
        .method(Method::GET)
        .uri("https://api.example.com/api/config")
        .body(Body::empty())?;
    let client = Client::new();
    let _ = client.request(req).await?;
    Ok(())
}
`
	ids, rels := runDetectWithRels(t, "rust", "hyper_client.rs", src)
	want := []string{
		"http:POST:/api/events",
		"http:GET:/api/config",
	}
	requireContains(t, ids, want, "rust-hyper-request-builder")
	requireFetches(t, rels, "http:POST:/api/events", "rust-hyper-request-builder")
	requireFetches(t, rels, "http:GET:/api/config", "rust-hyper-request-builder")
}

// ---------------------------------------------------------------------------
// ureq
// ---------------------------------------------------------------------------

// TestRustClient_UreqFunctions covers ureq::get(url).call() and
// ureq::post(url).send_json(json).
func TestRustClient_UreqFunctions(t *testing.T) {
	src := `
fn get_items() -> Result<String, ureq::Error> {
    let body: String = ureq::get("https://api.example.com/api/items")
        .call()?
        .into_string()?;
    Ok(body)
}

fn create_item(data: &str) -> Result<(), ureq::Error> {
    ureq::post("https://api.example.com/api/items")
        .send_string(data)?;
    Ok(())
}

fn update_item(id: u32, data: &str) -> Result<(), ureq::Error> {
    ureq::put("https://api.example.com/api/items/1")
        .send_string(data)?;
    Ok(())
}
`
	ids, rels := runDetectWithRels(t, "rust", "ureq_client.rs", src)
	want := []string{
		"http:GET:/api/items",
		"http:POST:/api/items",
		"http:PUT:/api/items/1",
	}
	requireContains(t, ids, want, "rust-ureq-functions")
	requireFetches(t, rels, "http:GET:/api/items", "rust-ureq-functions")
	requireFetches(t, rels, "http:POST:/api/items", "rust-ureq-functions")
}

// ---------------------------------------------------------------------------
// surf
// ---------------------------------------------------------------------------

// TestRustClient_SurfFunctions covers surf::get(url).await and
// surf::post(url).body_json(&body).await.
func TestRustClient_SurfFunctions(t *testing.T) {
	src := `
use surf;

async fn get_profile() -> surf::Result<()> {
    let mut res = surf::get("https://api.example.com/api/profile").await?;
    Ok(())
}

async fn update_profile(body: &serde_json::Value) -> surf::Result<()> {
    surf::post("https://api.example.com/api/profile")
        .body_json(body)?
        .await?;
    Ok(())
}
`
	ids, rels := runDetectWithRels(t, "rust", "surf_client.rs", src)
	want := []string{
		"http:GET:/api/profile",
		"http:POST:/api/profile",
	}
	requireContains(t, ids, want, "rust-surf-functions")
	requireFetches(t, rels, "http:GET:/api/profile", "rust-surf-functions")
	requireFetches(t, rels, "http:POST:/api/profile", "rust-surf-functions")
}

// TestRustClient_SurfClientNew covers surf::Client::new().get(url) builder form.
func TestRustClient_SurfClientNew(t *testing.T) {
	src := `
use surf;

async fn fetch_stats() -> surf::Result<()> {
    let mut res = surf::Client::new()
        .get("https://api.example.com/api/stats")
        .recv_json::<serde_json::Value>()
        .await?;
    Ok(())
}
`
	ids, rels := runDetectWithRels(t, "rust", "surf_client_new.rs", src)
	requireContains(t, ids, []string{"http:GET:/api/stats"}, "rust-surf-client-new")
	requireFetches(t, rels, "http:GET:/api/stats", "rust-surf-client-new")
}

// ---------------------------------------------------------------------------
// All HTTP verbs
// ---------------------------------------------------------------------------

// TestRustClient_VerbCoverage covers all HTTP verbs via reqwest module-level fns.
func TestRustClient_VerbCoverage(t *testing.T) {
	src := `
use reqwest;

async fn all_verbs() {
    let _ = reqwest::get("https://api.example.com/api/resources").await;
    let _ = reqwest::post("https://api.example.com/api/resources").await;
    let _ = reqwest::put("https://api.example.com/api/resources/1").await;
    let _ = reqwest::patch("https://api.example.com/api/resources/1").await;
    let _ = reqwest::delete("https://api.example.com/api/resources/1").await;
    let _ = reqwest::head("https://api.example.com/api/resources").await;
}
`
	ids, _ := runDetectWithRels(t, "rust", "all_verbs.rs", src)
	want := []string{
		"http:GET:/api/resources",
		"http:POST:/api/resources",
		"http:PUT:/api/resources/1",
		"http:PATCH:/api/resources/1",
		"http:DELETE:/api/resources/1",
		"http:HEAD:/api/resources",
	}
	requireContains(t, ids, want, "rust-verb-coverage")
}

// ---------------------------------------------------------------------------
// Env-var concatenation
// ---------------------------------------------------------------------------

// TestRustClient_EnvVarConcat covers reqwest::get(format!("{}/users", env::var("API_URL").unwrap()))
// → runtime_dynamic=true.
func TestRustClient_EnvVarConcat(t *testing.T) {
	src := `
use reqwest;
use std::env;

async fn call_remote() -> Result<(), reqwest::Error> {
    let resp = reqwest::get(format!("{}/users", env::var("API_URL").unwrap())).await?;
    Ok(())
}
`
	ids, rels := runDetectWithRels(t, "rust", "env_client.rs", src)
	requireContains(t, ids, []string{"http:GET:/users"}, "rust-env-var-concat")
	requireFetches(t, rels, "http:GET:/users", "rust-env-var-concat")

	// Verify runtime_dynamic=true is stamped on the entity.
	_, res := runDetect(t, "rust", "env_client.rs", src)
	found := false
	for _, e := range res.Entities {
		if e.ID == "http:GET:/users" && e.Properties["runtime_dynamic"] == "true" {
			found = true
		}
	}
	if !found {
		t.Errorf("rust-env-var-concat: expected runtime_dynamic=true on http:GET:/users")
	}
}

// ---------------------------------------------------------------------------
// Identifier resolution (symbol table)
// ---------------------------------------------------------------------------

// TestRustClient_IdentifierResolution covers the case where the URL is stored
// in a let-binding and resolved via the symbol table.
func TestRustClient_IdentifierResolution(t *testing.T) {
	src := `
use reqwest;

async fn fetch_reports() -> Result<(), reqwest::Error> {
    let url = "/api/reports";
    let _ = reqwest::get(url).await?;
    Ok(())
}
`
	ids, _ := runDetectWithRels(t, "rust", "ident_client.rs", src)
	requireContains(t, ids, []string{"http:GET:/api/reports"}, "rust-identifier-resolution")
}

// ---------------------------------------------------------------------------
// Negative case
// ---------------------------------------------------------------------------

// TestRustClient_Negative verifies that a non-HTTP Rust file does not emit
// any http_endpoint synthetics.
func TestRustClient_Negative(t *testing.T) {
	src := `
fn add(a: i32, b: i32) -> i32 {
    a + b
}

struct Config {
    timeout: u32,
    retries: u8,
}

impl Config {
    fn new(timeout: u32) -> Self {
        Config { timeout, retries: 3 }
    }
}
`
	ids, _ := runDetectWithRels(t, "rust", "math.rs", src)
	for _, id := range ids {
		if strings.HasPrefix(id, "http:") {
			t.Errorf("rust-negative: unexpected http_endpoint %q from non-HTTP file", id)
		}
	}
}
