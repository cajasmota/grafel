// Tests for the Phase 2A payload-shape sniffers — T2 languages (#2771).
// One canonical test per language verifying the inline-literal cases the
// drift detector relies on. Mirrors payload_shapes_test.go (T1).
package substrate

import (
	"reflect"
	"testing"
)

func TestPayloadShapesRuby_PermitAndRender(t *testing.T) {
	const src = `
class UsersController < ApplicationController
  def create
    user_params = params.require(:user).permit(:name, :email, :phone)
    render json: { id: 1, name: user_params[:name] }
  end
end
`
	shapes := sniffPayloadShapesRuby(src)
	req := findShape(shapes, "create", PayloadDirectionRequest, PayloadSideProducer)
	if req == nil {
		t.Fatalf("expected ruby permit request shape; got %+v", shapes)
	}
	wantReq := []string{"email", "name", "phone"}
	if got := sortedNames(req.Fields); !reflect.DeepEqual(got, wantReq) {
		t.Errorf("ruby permit fields: want %v got %v", wantReq, got)
	}
	resp := findShape(shapes, "create", PayloadDirectionResponse, PayloadSideProducer)
	if resp == nil {
		t.Fatalf("expected ruby render response shape; got %+v", shapes)
	}
	wantResp := []string{"id", "name"}
	if got := sortedNames(resp.Fields); !reflect.DeepEqual(got, wantResp) {
		t.Errorf("ruby render fields: want %v got %v", wantResp, got)
	}
}

func TestPayloadShapesRuby_ConsumerHTTParty(t *testing.T) {
	const src = `
def push
  HTTParty.post("/api/users", body: { name: "x", email: "y" }.to_json)
end
`
	shapes := sniffPayloadShapesRuby(src)
	cs := findShape(shapes, "push", PayloadDirectionRequest, PayloadSideConsumer)
	if cs == nil {
		t.Fatalf("expected ruby consumer shape; got %+v", shapes)
	}
	if cs.EndpointHint != "/api/users" || cs.VerbHint != "POST" {
		t.Errorf("ruby consumer hint: got %q %q", cs.EndpointHint, cs.VerbHint)
	}
	want := []string{"email", "name"}
	if got := sortedNames(cs.Fields); !reflect.DeepEqual(got, want) {
		t.Errorf("ruby consumer fields: want %v got %v", want, got)
	}
}

func TestPayloadShapesPHP_FormRequestAndGuzzle(t *testing.T) {
	const src = `
<?php
class CreateUserRequest extends FormRequest {
  public function rules() {
    return [ 'name' => 'required', 'email' => 'email', 'phone' => 'nullable' ];
  }
}
class UserClient {
  public function push() {
    return $this->client->request('POST', '/api/users', [ 'json' => [ 'name' => 'x', 'email' => 'y' ] ]);
  }
}
`
	shapes := sniffPayloadShapesPHP(src)
	req := findShape(shapes, "rules", PayloadDirectionRequest, PayloadSideProducer)
	if req == nil {
		t.Fatalf("expected php rules() shape; got %+v", shapes)
	}
	want := []string{"email", "name", "phone"}
	if got := sortedNames(req.Fields); !reflect.DeepEqual(got, want) {
		t.Errorf("php rules fields: want %v got %v", want, got)
	}
	cs := findShape(shapes, "push", PayloadDirectionRequest, PayloadSideConsumer)
	if cs == nil {
		t.Fatalf("expected php guzzle consumer shape; got %+v", shapes)
	}
	if cs.EndpointHint != "/api/users" || cs.VerbHint != "POST" {
		t.Errorf("php consumer hint: got %q %q", cs.EndpointHint, cs.VerbHint)
	}
}

func TestPayloadShapesRust_JsonExtractor(t *testing.T) {
	const src = `
#[derive(Deserialize)]
pub struct CreateUser {
    pub name: String,
    pub email: String,
    pub phone: Option<String>,
}

async fn create_user(Json(body): Json<CreateUser>) -> impl IntoResponse {
    let _ = body;
}
`
	shapes := sniffPayloadShapesRust(src)
	req := findShape(shapes, "create_user", PayloadDirectionRequest, PayloadSideProducer)
	if req == nil {
		t.Fatalf("expected rust Json<T> shape; got %+v", shapes)
	}
	want := []string{"email", "name", "phone"}
	if got := sortedNames(req.Fields); !reflect.DeepEqual(got, want) {
		t.Errorf("rust fields: want %v got %v", want, got)
	}
	// phone is Option<String> → Optional=true.
	for _, f := range req.Fields {
		if f.Name == "phone" && !f.Optional {
			t.Errorf("phone should be Optional=true; got %+v", f)
		}
	}
}

func TestPayloadShapesRust_ConsumerReqwest(t *testing.T) {
	const src = `
async fn push() {
    let _ = client.post("/api/users").json(&serde_json::json!({"name": "x", "email": "y"})).send().await;
}
`
	shapes := sniffPayloadShapesRust(src)
	cs := findShape(shapes, "push", PayloadDirectionRequest, PayloadSideConsumer)
	if cs == nil {
		t.Fatalf("expected rust consumer shape; got %+v", shapes)
	}
	if cs.EndpointHint != "/api/users" || cs.VerbHint != "POST" {
		t.Errorf("rust consumer hint: got %q %q", cs.EndpointHint, cs.VerbHint)
	}
}

func TestPayloadShapesCSharp_FromBodyDTO(t *testing.T) {
	const src = `
public class CreateUserDto {
  public string Name { get; set; }
  public string Email { get; set; }
  public int? Age { get; set; }
}
public class UsersController {
  [HttpPost]
  public IActionResult Create([FromBody] CreateUserDto dto) {
    return Ok(new { id = 1, name = dto.Name });
  }
}
`
	shapes := sniffPayloadShapesCSharp(src)
	req := findShape(shapes, "Create", PayloadDirectionRequest, PayloadSideProducer)
	if req == nil {
		t.Fatalf("expected csharp [FromBody] shape; got %+v", shapes)
	}
	want := []string{"Age", "Email", "Name"}
	if got := sortedNames(req.Fields); !reflect.DeepEqual(got, want) {
		t.Errorf("csharp DTO fields: want %v got %v", want, got)
	}
	for _, f := range req.Fields {
		if f.Name == "Age" && !f.Optional {
			t.Errorf("Age should be Optional=true (int?); got %+v", f)
		}
	}
	resp := findShape(shapes, "Create", PayloadDirectionResponse, PayloadSideProducer)
	if resp == nil {
		t.Fatalf("expected csharp anonymous response shape; got %+v", shapes)
	}
}

func TestPayloadShapesKotlin_RequestBodyAndMapOf(t *testing.T) {
	const src = `
data class CreateUser(val name: String, val email: String, val phone: String? = null)

class UsersController {
  @PostMapping("/users")
  fun create(@RequestBody dto: CreateUser): ResponseEntity<Map<String, Any>> {
    return ResponseEntity.ok(mapOf("id" to 1, "name" to dto.name))
  }
}
`
	shapes := sniffPayloadShapesKotlin(src)
	req := findShape(shapes, "create", PayloadDirectionRequest, PayloadSideProducer)
	if req == nil {
		t.Fatalf("expected kotlin @RequestBody shape; got %+v", shapes)
	}
	want := []string{"email", "name", "phone"}
	if got := sortedNames(req.Fields); !reflect.DeepEqual(got, want) {
		t.Errorf("kotlin data class fields: want %v got %v", want, got)
	}
	for _, f := range req.Fields {
		if f.Name == "phone" && !f.Optional {
			t.Errorf("phone should be Optional=true; got %+v", f)
		}
	}
	resp := findShape(shapes, "create", PayloadDirectionResponse, PayloadSideProducer)
	if resp == nil {
		t.Fatalf("expected kotlin mapOf response shape; got %+v", shapes)
	}
	wantR := []string{"id", "name"}
	if got := sortedNames(resp.Fields); !reflect.DeepEqual(got, wantR) {
		t.Errorf("kotlin mapOf fields: want %v got %v", wantR, got)
	}
}

func TestPayloadShapesElixir_DestructureAndJSON(t *testing.T) {
	const src = `
defmodule UsersController do
  def create(conn, %{"name" => name, "email" => email}) do
    json(conn, %{"id" => 1, "name" => name})
  end
end
`
	shapes := sniffPayloadShapesElixir(src)
	req := findShape(shapes, "create", PayloadDirectionRequest, PayloadSideProducer)
	if req == nil {
		t.Fatalf("expected elixir destructure shape; got %+v", shapes)
	}
	want := []string{"email", "name"}
	if got := sortedNames(req.Fields); !reflect.DeepEqual(got, want) {
		t.Errorf("elixir destructure fields: want %v got %v", want, got)
	}
	resp := findShape(shapes, "create", PayloadDirectionResponse, PayloadSideProducer)
	if resp == nil {
		t.Fatalf("expected elixir json response shape; got %+v", shapes)
	}
	wantR := []string{"id", "name"}
	if got := sortedNames(resp.Fields); !reflect.DeepEqual(got, wantR) {
		t.Errorf("elixir json fields: want %v got %v", wantR, got)
	}
}

func TestPayloadShapesScala_CaseClassAndJsonObj(t *testing.T) {
	const src = `
case class CreateUser(name: String, email: String, phone: Option[String])

class UsersController {
  def create(request: Request): Future[Response] = {
    val dto = request.decodeJson[CreateUser]
    Ok(Json.obj("id" -> 1, "name" -> "x"))
  }
}
`
	shapes := sniffPayloadShapesScala(src)
	req := findShape(shapes, "create", PayloadDirectionRequest, PayloadSideProducer)
	if req == nil {
		t.Fatalf("expected scala decodeJson shape; got %+v", shapes)
	}
	want := []string{"email", "name", "phone"}
	if got := sortedNames(req.Fields); !reflect.DeepEqual(got, want) {
		t.Errorf("scala case class fields: want %v got %v", want, got)
	}
	for _, f := range req.Fields {
		if f.Name == "phone" && !f.Optional {
			t.Errorf("phone should be Optional=true; got %+v", f)
		}
	}
	resp := findShape(shapes, "create", PayloadDirectionResponse, PayloadSideProducer)
	if resp == nil {
		t.Fatalf("expected scala Json.obj response shape; got %+v", shapes)
	}
}

func TestPayloadShapesCCPP_BodyAccessAndCurl(t *testing.T) {
	const src = `
void handle_create(http_request request) {
    auto body = request.extract_json().get();
    auto name = body[U("name")].as_string();
    auto email = body[U("email")].as_string();
    json::value result;
    result[U("id")] = json::value::number(1);
    result[U("name")] = json::value::string(name);
}

void push() {
    curl_easy_setopt(curl, CURLOPT_POSTFIELDS, "name=x&email=y");
}
`
	shapes := sniffPayloadShapesCCPP(src)
	req := findShape(shapes, "handle_create", PayloadDirectionRequest, PayloadSideProducer)
	if req == nil {
		t.Fatalf("expected cpp body access shape; got %+v", shapes)
	}
	want := []string{"email", "name"}
	if got := sortedNames(req.Fields); !reflect.DeepEqual(got, want) {
		t.Errorf("cpp body fields: want %v got %v", want, got)
	}
	resp := findShape(shapes, "handle_create", PayloadDirectionResponse, PayloadSideProducer)
	if resp == nil {
		t.Fatalf("expected cpp result assign shape; got %+v", shapes)
	}
	wantR := []string{"id", "name"}
	if got := sortedNames(resp.Fields); !reflect.DeepEqual(got, wantR) {
		t.Errorf("cpp result fields: want %v got %v", wantR, got)
	}
	cs := findShape(shapes, "push", PayloadDirectionRequest, PayloadSideConsumer)
	if cs == nil {
		t.Fatalf("expected cpp curl consumer shape; got %+v", shapes)
	}
	wantC := []string{"email", "name"}
	if got := sortedNames(cs.Fields); !reflect.DeepEqual(got, wantC) {
		t.Errorf("cpp curl fields: want %v got %v", wantC, got)
	}
}
