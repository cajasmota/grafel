package rust_test

import "testing"

// findEntity returns the first entity matching kind+name, or nil.
func findUtoipaEntity(ents []entitySummary, kind, name string) *entitySummary {
	for i := range ents {
		if ents[i].Kind == kind && ents[i].Name == name {
			return &ents[i]
		}
	}
	return nil
}

const utoipaSrc = `
use utoipa::{OpenApi, ToSchema, IntoParams};

#[derive(ToSchema)]
pub struct User {
    id: u64,
    #[serde(rename = "userName")]
    name: String,
    email: String,
}

#[derive(ToSchema)]
pub struct CreateUser {
    name: String,
    email: String,
}

#[derive(ToSchema)]
pub struct ApiError {
    message: String,
}

#[derive(IntoParams)]
pub struct ListParams {
    limit: u32,
    offset: u32,
}

#[utoipa::path(
    get,
    path = "/users/{id}",
    responses(
        (status = 200, body = User),
        (status = 404, body = ApiError)
    ),
    params(("id" = u64, Path, description = "user id"))
)]
async fn get_user(id: u64) -> User { todo!() }

#[utoipa::path(
    post,
    path = "/users",
    request_body = CreateUser,
    responses(
        (status = 201, body = User)
    )
)]
async fn create_user(body: CreateUser) -> User { todo!() }

#[derive(OpenApi)]
#[openapi(
    paths(get_user, create_user),
    components(schemas(User, CreateUser, ApiError))
)]
pub struct ApiDoc;
`

func TestUtoipaPostRouteContract(t *testing.T) {
	ents := extract(t, "custom_rust_utoipa", fi("api.rs", "rust", utoipaSrc))

	// POST /users operation with request + response contract.
	op := findUtoipaEntity(ents, "SCOPE.Operation", "POST /users")
	if op == nil {
		t.Fatalf("expected POST /users operation; got %+v", ents)
	}
	if op.Props["http_method"] != "POST" {
		t.Errorf("http_method = %q, want POST", op.Props["http_method"])
	}
	if op.Props["route_path"] != "/users" {
		t.Errorf("route_path = %q, want /users", op.Props["route_path"])
	}
	if op.Props["handler_name"] != "create_user" {
		t.Errorf("handler_name = %q, want create_user", op.Props["handler_name"])
	}
	if op.Props["request_body"] != "CreateUser" {
		t.Errorf("request_body = %q, want CreateUser", op.Props["request_body"])
	}
	if op.Props["response_bodies"] != "User" {
		t.Errorf("response_bodies = %q, want User", op.Props["response_bodies"])
	}
}

func TestUtoipaRequestDTOEmitted(t *testing.T) {
	ents := extract(t, "custom_rust_utoipa", fi("api.rs", "rust", utoipaSrc))

	req := findUtoipaEntity(ents, "SCOPE.Schema", "utoipa_request:CreateUser")
	if req == nil {
		t.Fatalf("expected CreateUser request DTO; got %+v", ents)
	}
	if req.Subtype != "request_dto" {
		t.Errorf("subtype = %q, want request_dto", req.Subtype)
	}
	if req.Props["type_param"] != "CreateUser" {
		t.Errorf("type_param = %q, want CreateUser", req.Props["type_param"])
	}
	if req.Props["route_path"] != "/users" || req.Props["http_method"] != "POST" {
		t.Errorf("request DTO not tied to POST /users: %+v", req.Props)
	}
}

func TestUtoipaResponseDTOsWithStatus(t *testing.T) {
	ents := extract(t, "custom_rust_utoipa", fi("api.rs", "rust", utoipaSrc))

	// GET /users/{id} has two responses: User (200) and ApiError (404).
	userResp := findUtoipaEntity(ents, "SCOPE.Schema", "utoipa_response:User")
	if userResp == nil {
		t.Fatalf("expected User response DTO; got %+v", ents)
	}
	if userResp.Subtype != "response_dto" {
		t.Errorf("subtype = %q, want response_dto", userResp.Subtype)
	}
	if userResp.Props["status_code"] != "200" {
		t.Errorf("User status_code = %q, want 200", userResp.Props["status_code"])
	}

	errResp := findUtoipaEntity(ents, "SCOPE.Schema", "utoipa_response:ApiError")
	if errResp == nil {
		t.Fatalf("expected ApiError response DTO; got %+v", ents)
	}
	if errResp.Props["status_code"] != "404" {
		t.Errorf("ApiError status_code = %q, want 404", errResp.Props["status_code"])
	}
}

func TestUtoipaGetRouteNormalisedPath(t *testing.T) {
	ents := extract(t, "custom_rust_utoipa", fi("api.rs", "rust", utoipaSrc))

	op := findUtoipaEntity(ents, "SCOPE.Operation", "GET /users/{id}")
	if op == nil {
		t.Fatalf("expected GET /users/{id} operation; got %+v", ents)
	}
	if op.Props["handler_name"] != "get_user" {
		t.Errorf("handler_name = %q, want get_user", op.Props["handler_name"])
	}
}

func TestUtoipaToSchemaDTOWithFields(t *testing.T) {
	ents := extract(t, "custom_rust_utoipa", fi("api.rs", "rust", utoipaSrc))

	schema := findUtoipaEntity(ents, "SCOPE.Schema", "utoipa_schema:User")
	if schema == nil {
		t.Fatalf("expected User schema; got %+v", ents)
	}
	if schema.Subtype != "schema" {
		t.Errorf("subtype = %q, want schema", schema.Subtype)
	}
	if schema.Props["field_count"] != "3" {
		t.Errorf("field_count = %q, want 3", schema.Props["field_count"])
	}

	// Field id: u64
	idField := findUtoipaEntity(ents, "SCOPE.Schema", "utoipa_field:User.id")
	if idField == nil {
		t.Fatalf("expected User.id field; got %+v", ents)
	}
	if idField.Props["field_type"] != "u64" {
		t.Errorf("id field_type = %q, want u64", idField.Props["field_type"])
	}

	// Field name carries serde wire name override.
	nameField := findUtoipaEntity(ents, "SCOPE.Schema", "utoipa_field:User.name")
	if nameField == nil {
		t.Fatalf("expected User.name field; got %+v", ents)
	}
	if nameField.Props["wire_name"] != "userName" {
		t.Errorf("name wire_name = %q, want userName", nameField.Props["wire_name"])
	}
}

func TestUtoipaIntoParamsSchema(t *testing.T) {
	ents := extract(t, "custom_rust_utoipa", fi("api.rs", "rust", utoipaSrc))

	p := findUtoipaEntity(ents, "SCOPE.Schema", "utoipa_schema:ListParams")
	if p == nil {
		t.Fatalf("expected ListParams params schema; got %+v", ents)
	}
	if p.Subtype != "params_schema" {
		t.Errorf("subtype = %q, want params_schema", p.Subtype)
	}
	if p.Props["field_count"] != "2" {
		t.Errorf("field_count = %q, want 2", p.Props["field_count"])
	}
}

func TestUtoipaOpenAPIAggregator(t *testing.T) {
	ents := extract(t, "custom_rust_utoipa", fi("api.rs", "rust", utoipaSrc))

	agg := findUtoipaEntity(ents, "SCOPE.Component", "utoipa_openapi:ApiDoc")
	if agg == nil {
		t.Fatalf("expected ApiDoc OpenApi aggregator; got %+v", ents)
	}
	if agg.Props["registered_paths"] != "get_user,create_user" {
		t.Errorf("registered_paths = %q, want get_user,create_user", agg.Props["registered_paths"])
	}
	if agg.Props["registered_schemas"] != "User,CreateUser,ApiError" {
		t.Errorf("registered_schemas = %q, want User,CreateUser,ApiError", agg.Props["registered_schemas"])
	}
	if agg.Props["path_count"] != "2" || agg.Props["schema_count"] != "3" {
		t.Errorf("counts wrong: %+v", agg.Props)
	}
}

func TestUtoipaNoMatch(t *testing.T) {
	src := `fn main() { println!("hi"); }`
	ents := extract(t, "custom_rust_utoipa", fi("main.rs", "rust", src))
	if len(ents) != 0 {
		t.Fatalf("expected no entities, got %+v", ents)
	}
}
