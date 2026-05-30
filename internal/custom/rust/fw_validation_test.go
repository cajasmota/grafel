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
	// Deep field + constraint assertions on the fixture (#3413).
	mustProp(t, ents, "dto_field:UpdateUserRequest.display_name",
		map[string]string{"field_name": "display_name", "wire_name": "displayName"})
	mustProp(t, ents, "validation:UpdateUserRequest.display_name:length",
		map[string]string{"min": "1", "max": "100"})
	mustProp(t, ents, "validation:UpdateUserRequest.age:range",
		map[string]string{"min": "0", "max": "150"})
	mustProp(t, ents, "dto_field:UpdateUserRequest.password",
		map[string]string{"serde_rename": "pwd", "wire_name": "pwd"})
	mustProp(t, ents, "validation:UpdateUserRequest.password:length",
		map[string]string{"min": "8"})
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

// ---------------------------------------------------------------------------
// Deep value-asserting tests (#3413): specific field + constraint + bound.
// These prove dto_extraction / request_validation to the TS/JS bar — every
// assertion names a concrete field and a concrete constraint with its bound.
// ---------------------------------------------------------------------------

// TestValidation_DTOFieldNamesAndTypes proves serde structs are decomposed into
// per-field entities carrying the concrete field name + Rust type.
func TestValidation_DTOFieldNamesAndTypes(t *testing.T) {
	src := `
use serde::Deserialize;

#[derive(Debug, Deserialize)]
pub struct CreateUserRequest {
    pub name: String,
    pub age: u32,
    pub email: String,
}
`
	ents := extract(t, "custom_rust_validation", fi("dto.rs", "rust", src))
	mustProp(t, ents, "dto_field:CreateUserRequest.name",
		map[string]string{"field_name": "name", "field_type": "String"})
	mustProp(t, ents, "dto_field:CreateUserRequest.age",
		map[string]string{"field_name": "age", "field_type": "u32"})
	mustProp(t, ents, "dto_field:CreateUserRequest.email",
		map[string]string{"field_name": "email", "field_type": "String"})
}

// TestValidation_LengthConstraintBounds proves #[validate(length(min=1,max=20))]
// is parsed into a constraint with the SPECIFIC min and max bounds — not len>0.
func TestValidation_LengthConstraintBounds(t *testing.T) {
	src := `
#[derive(Deserialize, Validate)]
pub struct SignupRequest {
    #[validate(length(min = 1, max = 20))]
    pub username: String,
    #[validate(email)]
    pub email: String,
}
`
	ents := extract(t, "custom_rust_validation", fi("signup.rs", "rust", src))
	mustProp(t, ents, "validation:SignupRequest.username:length",
		map[string]string{
			"constraint_kind": "length",
			"field_name":      "username",
			"min":             "1",
			"max":             "20",
		})
	mustProp(t, ents, "validation:SignupRequest.email:email",
		map[string]string{
			"constraint_kind": "email",
			"field_name":      "email",
		})
}

// TestValidation_RangeConstraintBounds proves #[validate(range(min=0,max=120))].
func TestValidation_RangeConstraintBounds(t *testing.T) {
	src := `
#[derive(Deserialize, Validate)]
pub struct Profile {
    #[validate(range(min = 0, max = 120))]
    pub age: u8,
}
`
	ents := extract(t, "custom_rust_validation", fi("profile.rs", "rust", src))
	mustProp(t, ents, "validation:Profile.age:range",
		map[string]string{
			"constraint_kind": "range",
			"field_name":      "age",
			"min":             "0",
			"max":             "120",
		})
}

// TestValidation_RegexAndCustom proves regex="..." and custom="fn" capture the
// concrete pattern path / function name.
func TestValidation_RegexAndCustom(t *testing.T) {
	src := `
#[derive(Deserialize, Validate)]
pub struct Creds {
    #[validate(regex = "RE_USERNAME")]
    pub handle: String,
    #[validate(custom = "validate_password_strength")]
    pub password: String,
}
`
	ents := extract(t, "custom_rust_validation", fi("creds.rs", "rust", src))
	mustProp(t, ents, "validation:Creds.handle:regex",
		map[string]string{"constraint_kind": "regex", "value": "RE_USERNAME"})
	mustProp(t, ents, "validation:Creds.password:custom",
		map[string]string{"constraint_kind": "custom", "value": "validate_password_strength"})
}

// TestValidation_NestedValidation proves a bare #[validate] / #[validate(nested)]
// on a field whose type is another DTO is captured as a nested constraint.
func TestValidation_NestedValidation(t *testing.T) {
	src := `
#[derive(Deserialize, Validate)]
pub struct Order {
    #[validate(length(min = 1))]
    pub id: String,
    #[validate(nested)]
    pub shipping: Address,
}
`
	ents := extract(t, "custom_rust_validation", fi("order.rs", "rust", src))
	mustProp(t, ents, "validation:Order.shipping:nested",
		map[string]string{"constraint_kind": "nested", "field_name": "shipping"})
}

// TestValidation_SerdeRename proves #[serde(rename="...")] and container
// rename_all="camelCase" set the effective wire_name.
func TestValidation_SerdeRename(t *testing.T) {
	src := `
#[derive(Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct ApiRequest {
    pub user_id: u64,
    #[serde(rename = "EmailAddress")]
    pub email: String,
    #[serde(default)]
    pub is_active: bool,
}
`
	ents := extract(t, "custom_rust_validation", fi("api.rs", "rust", src))
	// rename_all camelCase: user_id -> userId
	mustProp(t, ents, "dto_field:ApiRequest.user_id",
		map[string]string{"field_name": "user_id", "wire_name": "userId"})
	// explicit rename wins over rename_all
	mustProp(t, ents, "dto_field:ApiRequest.email",
		map[string]string{"wire_name": "EmailAddress", "serde_rename": "EmailAddress"})
	// is_active -> isActive, plus serde_default flag
	mustProp(t, ents, "dto_field:ApiRequest.is_active",
		map[string]string{"wire_name": "isActive", "serde_default": "true"})
}

// TestValidation_SerdeFlattenSkip proves flatten/skip flags are captured.
func TestValidation_SerdeFlattenSkip(t *testing.T) {
	src := `
#[derive(Deserialize)]
pub struct Page {
    #[serde(flatten)]
    pub paging: Pagination,
    #[serde(skip)]
    pub internal: String,
}
`
	ents := extract(t, "custom_rust_validation", fi("page.rs", "rust", src))
	mustProp(t, ents, "dto_field:Page.paging",
		map[string]string{"serde_flatten": "true"})
	mustProp(t, ents, "dto_field:Page.internal",
		map[string]string{"serde_skip": "true"})
}

// TestValidation_AxumActixRocketPayloadTie proves DTO fields + constraints are
// extracted alongside the flagship extractor payload that carries them.
func TestValidation_AxumActixRocketPayloadTie(t *testing.T) {
	// axum: Json<T>
	axum := `
#[derive(Deserialize, Validate)]
pub struct LoginForm {
    #[validate(email)]
    pub email: String,
    #[validate(length(min = 8))]
    pub password: String,
}
async fn login(Json(form): Json<LoginForm>) -> impl IntoResponse { todo!() }
`
	ents := extract(t, "custom_rust_validation", fi("axum_login.rs", "rust", axum))
	mustEntity(t, ents, "SCOPE.Schema", "axum_extractor:Json<LoginForm>")
	mustProp(t, ents, "validation:LoginForm.email:email",
		map[string]string{"constraint_kind": "email"})
	mustProp(t, ents, "validation:LoginForm.password:length",
		map[string]string{"min": "8"})

	// actix: web::Json<T>
	actix := `
#[derive(Deserialize, Validate)]
pub struct CreatePost {
    #[validate(length(min = 1, max = 280))]
    pub body: String,
}
async fn create(item: web::Json<CreatePost>) -> impl Responder { todo!() }
`
	ents2 := extract(t, "custom_rust_validation", fi("actix_post.rs", "rust", actix))
	mustEntity(t, ents2, "SCOPE.Schema", "actix_extractor:Json<CreatePost>")
	mustProp(t, ents2, "validation:CreatePost.body:length",
		map[string]string{"min": "1", "max": "280"})

	// rocket: Form<T>
	rocket := `
#[derive(FromForm, Validate)]
pub struct Contact {
    #[validate(email)]
    pub email: String,
}
#[post("/contact", data = "<form>")]
fn submit(form: Form<Contact>) -> Status { Status::Ok }
`
	ents3 := extract(t, "custom_rust_validation", fi("rocket_contact.rs", "rust", rocket))
	mustEntity(t, ents3, "SCOPE.Schema", "rocket_extractor:Contact")
	mustProp(t, ents3, "validation:Contact.email:email",
		map[string]string{"constraint_kind": "email"})
}

// --- property-asserting helpers ---

func mustEntity(t *testing.T, ents []entitySummary, kind, name string) {
	t.Helper()
	if !containsEntity(ents, kind, name) {
		t.Errorf("expected entity %s %q", kind, name)
	}
}

func mustProp(t *testing.T, ents []entitySummary, name string, want map[string]string) {
	t.Helper()
	for _, e := range ents {
		if e.Name != name {
			continue
		}
		for k, v := range want {
			got, ok := e.Props[k]
			if !ok {
				t.Errorf("entity %q: missing prop %q (want %q)", name, k, v)
				continue
			}
			if got != v {
				t.Errorf("entity %q: prop %q = %q, want %q", name, k, got, v)
			}
		}
		return
	}
	t.Errorf("entity %q not found", name)
}
