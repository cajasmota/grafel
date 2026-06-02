package engine

import (
	"strings"
	"testing"

	"github.com/cajasmota/archigraph/internal/engine/httproutes"
	"github.com/cajasmota/archigraph/internal/types"
)

// findEndpoint returns the first http_endpoint_definition entity with the
// given synthetic ID, or nil.
func findEndpoint(ents []types.EntityRecord, id string) *types.EntityRecord {
	for i := range ents {
		if ents[i].ID == id &&
			(ents[i].Kind == httpEndpointDefinitionKind || ents[i].Kind == httpEndpointKind) {
			return &ents[i]
		}
	}
	return nil
}

func runOpenAPISynth(t *testing.T, path, src string) []types.EntityRecord {
	t.Helper()
	lang := "yaml"
	if strings.HasSuffix(path, ".json") {
		lang = "json"
	}
	res := applyHTTPEndpointSynthesis(DetectorPassArgs{
		Lang: lang, Path: path, Content: []byte(src),
	})
	return res.Entities
}

// TestOpenAPISynthesis_CanonicalEndpointAndOperationID asserts the core
// contract: `paths./users/{id}.get` with operationId getUser emits a
// canonical http_endpoint_definition `http:GET:/users/{id}` carrying the
// operationId, source=openapi_spec, and provenance=spec.
func TestOpenAPISynthesis_CanonicalEndpointAndOperationID(t *testing.T) {
	const spec = `
openapi: 3.0.3
info:
  title: Users API
  version: 1.0.0
paths:
  /users/{id}:
    get:
      operationId: getUser
      summary: Fetch a user by id
      responses:
        '200':
          description: ok
`
	ents := runOpenAPISynth(t, "openapi.yaml", spec)

	ep := findEndpoint(ents, "http:GET:/users/{id}")
	if ep == nil {
		t.Fatalf("expected endpoint http:GET:/users/{id}; got entities: %+v", oapiEndpointIDs(ents))
	}
	if got := ep.Properties["operation_id"]; got != "getUser" {
		t.Errorf("operation_id = %q, want getUser", got)
	}
	if got := ep.Properties["summary"]; got != "Fetch a user by id" {
		t.Errorf("summary = %q, want \"Fetch a user by id\"", got)
	}
	if got := ep.Properties["source"]; got != "openapi_spec" {
		t.Errorf("source = %q, want openapi_spec", got)
	}
	if got := ep.Properties["provenance"]; got != "spec" {
		t.Errorf("provenance = %q, want spec", got)
	}
	if got := ep.Properties["verb"]; got != "GET" {
		t.Errorf("verb = %q, want GET", got)
	}
	if got := ep.Properties["path"]; got != "/users/{id}" {
		t.Errorf("path = %q, want /users/{id}", got)
	}
}

// TestOpenAPISynthesis_ConvergesWithCodeRoute is the parity-oracle assertion:
// the spec's GET /users/{id} produces the EXACT synthetic ID that a code
// extractor would produce for FastAPI GET /users/{id}, so the spec endpoint
// converges on (does not duplicate) the code-extracted endpoint.
func TestOpenAPISynthesis_ConvergesWithCodeRoute(t *testing.T) {
	const spec = `
openapi: 3.0.3
info: {title: X, version: 1.0.0}
paths:
  /users/{id}:
    get:
      operationId: getUser
      responses: {'200': {description: ok}}
`
	ents := runOpenAPISynth(t, "api/openapi.yaml", spec)

	// The code-route ID a FastAPI GET /users/{id} extractor yields:
	codeRouteID := httproutes.SyntheticID("GET", httproutes.Canonicalize(httproutes.FrameworkFastAPI, "/users/{id}"))
	if codeRouteID != "http:GET:/users/{id}" {
		t.Fatalf("sanity: code-route id = %q", codeRouteID)
	}
	if findEndpoint(ents, codeRouteID) == nil {
		t.Fatalf("spec endpoint ID did not converge with code-route ID %q; got %v",
			codeRouteID, oapiEndpointIDs(ents))
	}
}

// TestOpenAPISynthesis_RequestBodySchemaRef asserts a POST with a requestBody
// $ref surfaces the DTO schema name on request_schema, and response $refs on
// response_schemas.
func TestOpenAPISynthesis_RequestBodySchemaRef(t *testing.T) {
	const spec = `
openapi: 3.0.3
info: {title: X, version: 1.0.0}
paths:
  /users:
    post:
      operationId: createUser
      requestBody:
        content:
          application/json:
            schema:
              $ref: '#/components/schemas/CreateUser'
      responses:
        '201':
          content:
            application/json:
              schema:
                $ref: '#/components/schemas/User'
components:
  schemas:
    CreateUser: {type: object}
    User: {type: object}
`
	ents := runOpenAPISynth(t, "openapi.yaml", spec)

	ep := findEndpoint(ents, "http:POST:/users")
	if ep == nil {
		t.Fatalf("expected http:POST:/users; got %v", oapiEndpointIDs(ents))
	}
	if got := ep.Properties["request_schema"]; got != "CreateUser" {
		t.Errorf("request_schema = %q, want CreateUser", got)
	}
	if got := ep.Properties["response_schemas"]; got != "User" {
		t.Errorf("response_schemas = %q, want User", got)
	}
}

// TestOpenAPISynthesis_Swagger2 asserts Swagger 2.0 specs (swagger: "2.0",
// basePath-less, definitions/$ref) are recognised and emit the same canonical
// shape.
func TestOpenAPISynthesis_Swagger2(t *testing.T) {
	const spec = `
swagger: "2.0"
info: {title: Legacy, version: 1.0.0}
paths:
  /orders/{orderId}:
    delete:
      operationId: deleteOrder
      responses:
        '204': {description: gone}
`
	ents := runOpenAPISynth(t, "swagger.json", spec)
	ep := findEndpoint(ents, "http:DELETE:/orders/{orderId}")
	if ep == nil {
		t.Fatalf("expected http:DELETE:/orders/{orderId}; got %v", oapiEndpointIDs(ents))
	}
	if ep.Properties["operation_id"] != "deleteOrder" {
		t.Errorf("operation_id = %q, want deleteOrder", ep.Properties["operation_id"])
	}
}

// TestOpenAPISynthesis_NegativeNonSpecYAML asserts an ordinary YAML config
// file (no openapi:/swagger: key) emits NO endpoints, even when it contains a
// "paths"-like key.
func TestOpenAPISynthesis_NegativeNonSpecYAML(t *testing.T) {
	const notASpec = `
version: "3.8"
services:
  web:
    image: nginx
    volumes:
      - ./paths:/etc/nginx/paths
`
	ents := runOpenAPISynth(t, "docker-compose.yaml", notASpec)
	for _, e := range ents {
		if e.Kind == httpEndpointDefinitionKind || e.Kind == httpEndpointKind {
			t.Fatalf("non-spec YAML emitted endpoint %q (props %v)", e.ID, e.Properties)
		}
	}
}

func oapiEndpointIDs(ents []types.EntityRecord) []string {
	var out []string
	for _, e := range ents {
		if e.Kind == httpEndpointDefinitionKind || e.Kind == httpEndpointKind {
			out = append(out, e.ID)
		}
	}
	return out
}
