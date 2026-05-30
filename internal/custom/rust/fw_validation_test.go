package rust_test

// fw_validation_test.go — tests for custom_rust_validation extractor.
// Proves dto_extraction and request_validation detection surface.

import (
	"testing"
)

func TestValidation_SerdeDeserializeDTO(t *testing.T) {
	src := `
use serde::Deserialize;

#[derive(Debug, Deserialize)]
pub struct CreateUserRequest {
    pub name: String,
    pub email: String,
}
`
	ents := extract(t, "custom_rust_validation", fi("handler.rs", "rust", src))
	if !containsEntity(ents, "SCOPE.Schema", "dto:CreateUserRequest") {
		t.Error("expected dto:CreateUserRequest from Deserialize derive")
	}
}

func TestValidation_ValidateDerive(t *testing.T) {
	src := `
use serde::Deserialize;
use validator::Validate;

#[derive(Debug, Deserialize, Validate)]
pub struct SignupRequest {
    pub username: String,
    pub email: String,
}
`
	ents := extract(t, "custom_rust_validation", fi("signup.rs", "rust", src))
	if !containsEntity(ents, "SCOPE.Schema", "dto:SignupRequest") {
		t.Error("expected dto:SignupRequest with Validate")
	}
}

func TestValidation_ValidateFieldAttr(t *testing.T) {
	src := `
#[derive(Deserialize, Validate)]
pub struct ChangePasswordRequest {
    #[validate(length(min = 8))]
    pub password: String,
    #[validate(email)]
    pub email: String,
}
`
	ents := extract(t, "custom_rust_validation", fi("change_pw.rs", "rust", src))
	if !containsEntitySubtype(ents, "SCOPE.Pattern", "field_validation") {
		t.Error("expected field_validation pattern from #[validate(...)] attribute")
	}
}

func TestValidation_ValidateCall(t *testing.T) {
	src := `
async fn handler(payload: Json<CreateUserRequest>) -> impl Responder {
    if payload.validate().is_err() {
        return HttpResponse::BadRequest().finish();
    }
    HttpResponse::Ok().finish()
}
`
	ents := extract(t, "custom_rust_validation", fi("handler.rs", "rust", src))
	if !containsEntitySubtype(ents, "SCOPE.Pattern", "request_validation") {
		t.Error("expected request_validation pattern from .validate() call")
	}
}

func TestValidation_ActixWebExtractor(t *testing.T) {
	src := `
use actix_web::web;

async fn create_user(
    body: web::Json<CreateUserRequest>,
    query: web::Query<PaginationQuery>,
) -> impl Responder {
    HttpResponse::Ok().finish()
}
`
	ents := extract(t, "custom_rust_validation", fi("handlers.rs", "rust", src))
	if !containsEntity(ents, "SCOPE.Schema", "actix_extractor:Json<CreateUserRequest>") {
		t.Error("expected actix_extractor:Json<CreateUserRequest>")
	}
	if !containsEntity(ents, "SCOPE.Schema", "actix_extractor:Query<PaginationQuery>") {
		t.Error("expected actix_extractor:Query<PaginationQuery>")
	}
}

func TestValidation_TideBodyJson(t *testing.T) {
	src := `
async fn handler(mut req: Request<State>) -> tide::Result {
    let body: CreateUserRequest = req.body_json::<CreateUserRequest>().await?;
    Ok(Response::new(200))
}
`
	ents := extract(t, "custom_rust_validation", fi("handler.rs", "rust", src))
	if !containsEntity(ents, "SCOPE.Schema", "tide_body_json:CreateUserRequest") {
		t.Error("expected tide_body_json:CreateUserRequest")
	}
}

func TestValidation_WarpBodyJson(t *testing.T) {
	src := `
let create = warp::path("users")
    .and(warp::post())
    .and(warp::body::json())
    .and_then(create_user_handler);
`
	ents := extract(t, "custom_rust_validation", fi("routes.rs", "rust", src))
	if !containsEntity(ents, "SCOPE.Pattern", "warp_body_json") {
		t.Error("expected warp_body_json pattern")
	}
}

func TestValidation_HyperBodyDeser(t *testing.T) {
	src := `
async fn handle(req: Request<Body>) -> Result<Response<Body>, Infallible> {
    let bytes = hyper::body::to_bytes(req.into_body()).await.unwrap();
    let payload: CreateUserRequest = serde_json::from_slice(&bytes).unwrap();
    Ok(Response::new(Body::from("ok")))
}
`
	ents := extract(t, "custom_rust_validation", fi("handler.rs", "rust", src))
	if !containsEntitySubtype(ents, "SCOPE.Pattern", "request_extractor") {
		t.Error("expected request_extractor pattern from hyper body deserialization")
	}
}

func TestValidation_SalvoExtractible(t *testing.T) {
	src := `
use salvo::prelude::*;

#[derive(Extractible, Debug)]
#[salvo(extract(default_source(from = "body")))]
pub struct CreateUserPayload {
    pub name: String,
    pub email: String,
}
`
	ents := extract(t, "custom_rust_validation", fi("handler.rs", "rust", src))
	if !containsEntity(ents, "SCOPE.Schema", "salvo_extractible:CreateUserPayload") {
		t.Error("expected salvo_extractible:CreateUserPayload")
	}
}

func TestValidation_NoMatch(t *testing.T) {
	src := `
fn main() {
    println!("hello world");
}
`
	ents := extract(t, "custom_rust_validation", fi("main.rs", "rust", src))
	if len(ents) != 0 {
		t.Errorf("expected no entities, got %d", len(ents))
	}
}

func TestValidation_FixtureFile(t *testing.T) {
	src := readFixture(t, "testdata/validation_dto.rs")
	ents := extract(t, "custom_rust_validation", fi("validation_dto.rs", "rust", src))
	if !containsEntity(ents, "SCOPE.Schema", "dto:CreateUserRequest") {
		t.Error("expected dto:CreateUserRequest")
	}
	if !containsEntity(ents, "SCOPE.Schema", "dto:UpdateUserRequest") {
		t.Error("expected dto:UpdateUserRequest (Validate derive)")
	}
	if !containsEntity(ents, "SCOPE.Schema", "actix_extractor:Json<CreateUserRequest>") {
		t.Error("expected actix Json extractor")
	}
	if !containsEntity(ents, "SCOPE.Pattern", "warp_body_json") {
		t.Error("expected warp body json pattern")
	}
}

// containsEntitySubtype checks for matching kind+subtype regardless of name.
func containsEntitySubtype(ents []entitySummary, kind, subtype string) bool {
	for _, e := range ents {
		if e.Kind == kind && e.Subtype == subtype {
			return true
		}
	}
	return false
}
