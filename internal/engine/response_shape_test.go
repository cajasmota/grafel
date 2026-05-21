// Tests for response shape extraction (#722).
//
// Each test exercises one framework's response_keys / response_schema /
// error_keys / status_codes / request_keys path. The cross-language
// parity test at the end verifies that frameworks NOT targeted by a
// given fixture do not pick up false-positive shape extraction.
package engine

import (
	"strings"
	"testing"
)

// shapeOf returns the http_endpoint synthetic with the given verb+path
// from a runDetect result. Failing the lookup is a test error.
// isHTTPEndpointKindLocal reports whether kind is any of the three HTTP
// endpoint kind strings for test helpers. #1217.
func isHTTPEndpointKindLocal(kind string) bool {
	return kind == httpEndpointKind || kind == httpEndpointDefinitionKind || kind == httpEndpointCallKind
}

func shapeOf(t *testing.T, res *DetectResult, id string) map[string]string {
	t.Helper()
	for _, e := range res.Entities {
		// #1217: accept all three http endpoint kind strings.
		if isHTTPEndpointKindLocal(e.Kind) && e.ID == id {
			return e.Properties
		}
	}
	t.Fatalf("expected http_endpoint %q in entities; got: %s", id, dumpEndpointIDs(res))
	return nil
}

func dumpEndpointIDs(res *DetectResult) string {
	var out []string
	for _, e := range res.Entities {
		// #1217: accept all three http endpoint kind strings.
		if isHTTPEndpointKindLocal(e.Kind) {
			out = append(out, e.ID)
		}
	}
	return strings.Join(out, ", ")
}

func assertProp(t *testing.T, props map[string]string, key, want string) {
	t.Helper()
	if got := props[key]; got != want {
		t.Errorf("property %q: got %q, want %q (all props: %v)", key, got, want, props)
	}
}

// ---------------------------------------------------------------------------
// Flask
// ---------------------------------------------------------------------------

func TestResponseShape_Flask_LiteralDict(t *testing.T) {
	src := `from flask import Flask, jsonify
app = Flask(__name__)

@app.route("/users/<int:id>", methods=["GET"])
def get_user(id):
    return jsonify({"id": id, "name": "alice", "active": True})

@app.route("/users", methods=["POST"])
def create_user():
    return {"id": 1, "name": "bob"}, 201

@app.route("/users/<int:id>", methods=["DELETE"])
def delete_user(id):
    return {"error": "not found"}, 404
`
	_, res := runDetect(t, "python", "app.py", src)
	props := shapeOf(t, res, "http:GET:/users/{id}")
	assertProp(t, props, "response_keys", "active,id,name")
	assertProp(t, props, "status_codes", "200")
	assertProp(t, props, "response_keys_known", "true")

	createProps := shapeOf(t, res, "http:POST:/users")
	assertProp(t, createProps, "response_keys", "id,name")
	assertProp(t, createProps, "status_codes", "201")

	delProps := shapeOf(t, res, "http:DELETE:/users/{id}")
	assertProp(t, delProps, "error_keys", "error")
	assertProp(t, delProps, "status_codes", "404")
}

// ---------------------------------------------------------------------------
// Django / DRF
// ---------------------------------------------------------------------------

func TestResponseShape_Django_Response(t *testing.T) {
	// Django views typically register through urls.py; we exercise the
	// per-file synth that walks `def handler` defs. The Flask-like test
	// covers the DRF Response(...) shape; the route is emitted by the
	// Django composed-route synth so the handler property names match.
	src := `from rest_framework.response import Response
from rest_framework.decorators import api_view

# A urls.py-like route registration that the Django composed-route synth
# would have picked up. We register through the Flask shape (which
# emits a Route entity into the file) so the test does not need urls.py.
@app.route("/api/users/<int:id>")
def detail(id):
    return Response({"id": id, "name": "alice", "email": "a@b"})

@app.route("/api/users")
def list_users():
    return Response({"items": [], "count": 0})

@app.route("/api/users/error")
def bad():
    return Response({"detail": "not found"}, status=404)
`
	_, res := runDetect(t, "python", "views.py", src)
	props := shapeOf(t, res, "http:GET:/api/users/{id}")
	assertProp(t, props, "response_keys", "email,id,name")
	assertProp(t, props, "response_keys_known", "true")

	listProps := shapeOf(t, res, "http:GET:/api/users")
	assertProp(t, listProps, "response_keys", "count,items")

	badProps := shapeOf(t, res, "http:GET:/api/users/error")
	assertProp(t, badProps, "error_keys", "detail")
	assertProp(t, badProps, "status_codes", "404")
}

// ---------------------------------------------------------------------------
// FastAPI
// ---------------------------------------------------------------------------

func TestResponseShape_FastAPI_PydanticResponseModel(t *testing.T) {
	src := `from fastapi import FastAPI, APIRouter
from pydantic import BaseModel

router = APIRouter()

class UserOut(BaseModel):
    id: int
    name: str
    email: str

class UserIn(BaseModel):
    name: str
    email: str

@router.get("/users/{id}", response_model=UserOut)
async def get_user(id: int):
    return {"id": id, "name": "x", "email": "y"}

@router.post("/users")
def create_user(body: UserIn):
    return {"id": 1, "name": body.name, "email": body.email}
`
	_, res := runDetect(t, "python", "main.py", src)
	props := shapeOf(t, res, "http:GET:/users/{id}")
	assertProp(t, props, "response_keys", "email,id,name")
	// response_schema is a stable JSON map sorted by key.
	if got := props["response_schema"]; !strings.Contains(got, "\"id\":\"int\"") {
		t.Errorf("response_schema missing typed id field: %q", got)
	}
	createProps := shapeOf(t, res, "http:POST:/users")
	assertProp(t, createProps, "request_keys", "email,name")
	if got := createProps["request_schema"]; !strings.Contains(got, "\"email\":\"str\"") {
		t.Errorf("request_schema missing email field: %q", got)
	}
}

// ---------------------------------------------------------------------------
// Express
// ---------------------------------------------------------------------------

func TestResponseShape_Express_ResJson(t *testing.T) {
	src := `const express = require('express');
const app = express();

function getUser(req, res) {
    res.json({id: 1, name: "alice", active: true});
}

function createUser(req, res) {
    res.status(201).json({id: 1, name: "bob"});
}

function bad(req, res) {
    res.status(404).json({error: "not found"});
}

app.get('/users/:id', getUser);
app.post('/users', createUser);
app.get('/users/:id/missing', bad);
`
	_, res := runDetect(t, "javascript", "app.js", src)
	props := shapeOf(t, res, "http:GET:/users/{id}")
	assertProp(t, props, "response_keys", "active,id,name")
	assertProp(t, props, "response_keys_known", "true")
	createProps := shapeOf(t, res, "http:POST:/users")
	assertProp(t, createProps, "response_keys", "id,name")
	// status_codes set includes the 201 from the chained status() call.
	if got := createProps["status_codes"]; !strings.Contains(got, "201") {
		t.Errorf("expected status_codes to include 201; got %q", got)
	}
	badProps := shapeOf(t, res, "http:GET:/users/{id}/missing")
	assertProp(t, badProps, "error_keys", "error")
	assertProp(t, badProps, "status_codes", "404")
}

// ---------------------------------------------------------------------------
// Spring MVC
// ---------------------------------------------------------------------------

func TestResponseShape_Spring_ResponseEntity(t *testing.T) {
	src := `package com.example;
import org.springframework.http.ResponseEntity;
import org.springframework.web.bind.annotation.*;

class UserDto {
    public Long id;
    public String name;
    public String email;
}

class CreateUserDto {
    public String name;
    public String email;
}

@RestController
public class UserController {
    @GetMapping("/users/{id}")
    public ResponseEntity<UserDto> get(@PathVariable Long id) {
        return ResponseEntity.ok(new UserDto());
    }

    @PostMapping("/users")
    public UserDto create(@RequestBody CreateUserDto body) {
        return new UserDto();
    }
}
`
	_, res := runDetect(t, "java", "UserController.java", src)
	// The single-file test uses the YAML composed-route path which emits
	// verb=ANY (the verb comes from the Spring AST pass in real builds).
	getProps := shapeOf(t, res, "http:ANY:/users/{id}")
	if rk := getProps["response_keys"]; !strings.Contains(rk, "id") || !strings.Contains(rk, "name") || !strings.Contains(rk, "email") {
		t.Errorf("expected response_keys with id,name,email; got %q (all: %v)", rk, getProps)
	}
	if s := getProps["response_schema"]; !strings.Contains(s, "\"id\":\"Long\"") {
		t.Errorf("expected response_schema with id:Long; got %q", s)
	}
	createProps := shapeOf(t, res, "http:ANY:/users")
	if rk := createProps["request_keys"]; !strings.Contains(rk, "name") || !strings.Contains(rk, "email") {
		t.Errorf("expected request_keys with name,email; got %q (all: %v)", rk, createProps)
	}
}

// ---------------------------------------------------------------------------
// JAX-RS
// ---------------------------------------------------------------------------

func TestResponseShape_JAXRS_TypedReturn(t *testing.T) {
	src := `package com.example;
import javax.ws.rs.*;
import javax.ws.rs.core.Response;

class ProductDto {
    public Long id;
    public String sku;
    public Double price;
}

@Path("/products")
public class ProductResource {
    @GET
    @Path("/{id}")
    public ProductDto get(@PathParam("id") long id) {
        return new ProductDto();
    }
}
`
	_, res := runDetect(t, "java", "ProductResource.java", src)
	props := shapeOf(t, res, "http:GET:/products/{id}")
	if rk := props["response_keys"]; !strings.Contains(rk, "id") || !strings.Contains(rk, "sku") || !strings.Contains(rk, "price") {
		t.Errorf("expected response_keys with id,sku,price; got %q (all props: %v)", rk, props)
	}
}

// ---------------------------------------------------------------------------
// Go (Gin)
// ---------------------------------------------------------------------------

func TestResponseShape_Gin_JSONMap(t *testing.T) {
	src := "package main\n" +
		"\n" +
		"import (\n" +
		"\t\"github.com/gin-gonic/gin\"\n" +
		"\t\"net/http\"\n" +
		")\n" +
		"\n" +
		"type User struct {\n" +
		"\tID    int    `json:\"id\"`\n" +
		"\tName  string `json:\"name\"`\n" +
		"\tEmail string `json:\"email\"`\n" +
		"}\n" +
		"\n" +
		"func getUser(c *gin.Context) {\n" +
		"\tc.JSON(http.StatusOK, gin.H{\"id\": 1, \"name\": \"alice\"})\n" +
		"}\n" +
		"\n" +
		"func getUserTyped(c *gin.Context) {\n" +
		"\tc.JSON(http.StatusOK, &User{})\n" +
		"}\n" +
		"\n" +
		"func notFound(c *gin.Context) {\n" +
		"\tc.JSON(http.StatusNotFound, gin.H{\"error\": \"missing\"})\n" +
		"}\n" +
		"\n" +
		"func main() {\n" +
		"\tr := gin.Default()\n" +
		"\tr.GET(\"/users/:id\", getUser)\n" +
		"\tr.GET(\"/users/:id/typed\", getUserTyped)\n" +
		"\tr.GET(\"/users/:id/missing\", notFound)\n" +
		"}\n"
	_, res := runDetect(t, "go", "main.go", src)
	props := shapeOf(t, res, "http:GET:/users/{id}")
	assertProp(t, props, "response_keys", "id,name")
	assertProp(t, props, "status_codes", "200")

	typedProps := shapeOf(t, res, "http:GET:/users/{id}/typed")
	if rk := typedProps["response_keys"]; !strings.Contains(rk, "id") || !strings.Contains(rk, "name") || !strings.Contains(rk, "email") {
		t.Errorf("expected response_keys with id,name,email from struct; got %q", rk)
	}
	if s := typedProps["response_schema"]; !strings.Contains(s, "\"id\":") {
		t.Errorf("expected response_schema with id field; got %q", s)
	}
	errProps := shapeOf(t, res, "http:GET:/users/{id}/missing")
	assertProp(t, errProps, "error_keys", "error")
	assertProp(t, errProps, "status_codes", "404")
}

// ---------------------------------------------------------------------------
// Cross-language false-positive guard
// ---------------------------------------------------------------------------

// TestResponseShape_NoFalsePositive_GoFile verifies that scanning a Go
// file with Express-shaped code in a string literal does not produce
// shape extraction (the synth pass restricts JS extraction to JS files).
func TestResponseShape_NoFalsePositive_GoFile(t *testing.T) {
	// A Go file with a string literal that contains an Express-looking
	// substring. The Python/JS shape extractors must NOT fire.
	src := "package main\n\nconst sample = \"app.get('/users/:id', (req,res) => res.json({id:1}))\"\n"
	_, res := runDetect(t, "go", "main.go", src)
	for _, e := range res.Entities {
		if e.Kind == httpEndpointKind {
			if rk := e.Properties["response_keys"]; rk != "" {
				t.Errorf("unexpected response_keys on go entity %s: %q", e.ID, rk)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Regression: bounds checking for files without trailing newline (#773)
// ---------------------------------------------------------------------------

// TestResponseShape_Python_NoTrailingNewline verifies that a Python file
// without a trailing newline does not cause a panic in findHandlerBody when
// the framework pass is enabled. This is a regression test for #773:
// "slice bounds out of range [:1505] with length 1504".
func TestResponseShape_Python_NoTrailingNewline(t *testing.T) {
	// Construct a source that ends without a newline, and has a handler
	// body that extends to near the end of the file. This exercises the
	// bodyEnd bounds check in findHandlerBody (lines 111-115).
	src := `from flask import Flask, jsonify
app = Flask(__name__)

@app.route("/api/items")
def list_items():
    return jsonify({"items": [], "count": 0})`
	// Deliberately omit the trailing newline to trigger the bug.

	_, res := runDetect(t, "python", "app.py", src)
	props := shapeOf(t, res, "http:GET:/api/items")
	assertProp(t, props, "response_keys", "count,items")
	assertProp(t, props, "status_codes", "200")
	assertProp(t, props, "response_keys_known", "true")
}

// TestFindHandlerBody_NoTrailingNewline verifies that findHandlerBody does not
// panic when the source file ends without a trailing newline and the function
// body contains the last line of the file (issue #699b panic fix: slice bounds
// out of range [:N+1] with length N when the last line had no newline).
func TestFindHandlerBody_NoTrailingNewline(t *testing.T) {
	// Deliberately omit the trailing newline — this was the crash trigger.
	src := "def my_view(request):\n    return JsonResponse({'ok': True})"
	// Must not panic.
	body := findHandlerBody(src, "my_view")
	if body == "" {
		t.Error("findHandlerBody returned empty body for valid function; want non-empty")
	}
	if !strings.Contains(body, "JsonResponse") {
		t.Errorf("findHandlerBody body missing expected content; got %q", body)
	}
}

// TestFindHandlerBody_MultiLineNoTrailingNewline verifies the same no-panic
// guarantee when multiple indented lines exist and the file ends mid-body.
func TestFindHandlerBody_MultiLineNoTrailingNewline(t *testing.T) {
	src := "def process(request):\n    data = request.POST\n    result = data.get('key')\n    return JsonResponse({'result': result})"
	body := findHandlerBody(src, "process")
	if body == "" {
		t.Error("findHandlerBody returned empty body for multi-line function; want non-empty")
	}
}

// ---------------------------------------------------------------------------
// DRF Serializer walking (Python)
// ---------------------------------------------------------------------------

// TestResponseShape_DRF_SerializerData_LocalVar exercises the most common DRF
// pattern: a local variable assigned from a Serializer class whose fields are
// declared as class attributes, then returned as `serializer.data`.
func TestResponseShape_DRF_SerializerData_LocalVar(t *testing.T) {
	src := `from rest_framework import serializers
from rest_framework.response import Response
from rest_framework.decorators import api_view

class UserSerializer(serializers.Serializer):
    id = serializers.IntegerField(read_only=True)
    name = serializers.CharField(max_length=100)
    email = serializers.EmailField()

@app.route("/api/users/<int:id>", methods=["GET"])
def get_user(id):
    serializer = UserSerializer(instance)
    return Response(serializer.data)
`
	_, res := runDetect(t, "python", "views.py", src)
	props := shapeOf(t, res, "http:GET:/api/users/{id}")
	for _, want := range []string{"id", "name", "email"} {
		if !strings.Contains(props["response_keys"], want) {
			t.Errorf("DRF local var: expected response_keys to contain %q; got %q", want, props["response_keys"])
		}
	}
	assertProp(t, props, "response_keys_known", "true")
	assertProp(t, props, "response_keys_source", "drf_serializer")
}

// TestResponseShape_DRF_SerializerClass_ViewSet exercises a DRF ViewSet that
// declares `serializer_class = UserSerializer` as a class attribute. The
// extractor resolves the class-level attribute and walks the serializer.
func TestResponseShape_DRF_SerializerClass_ViewSet(t *testing.T) {
	src := `from rest_framework import serializers, viewsets
from rest_framework.response import Response

class UserSerializer(serializers.Serializer):
    id = serializers.IntegerField(read_only=True)
    name = serializers.CharField()
    email = serializers.EmailField()

class UserViewSet(viewsets.ModelViewSet):
    serializer_class = UserSerializer

    @app.route("/api/users/<int:pk>", methods=["GET"])
    def retrieve(self, request, pk=None):
        serializer = self.get_serializer(self.get_object())
        return Response(serializer.data)
`
	_, res := runDetect(t, "python", "views.py", src)
	props := shapeOf(t, res, "http:GET:/api/users/{pk}")
	for _, want := range []string{"id", "name", "email"} {
		if !strings.Contains(props["response_keys"], want) {
			t.Errorf("DRF ViewSet serializer_class: expected response_keys to contain %q; got %q", want, props["response_keys"])
		}
	}
	assertProp(t, props, "response_keys_source", "drf_serializer")
}

// TestResponseShape_DRF_ModelSerializer_MetaFields exercises a ModelSerializer
// that declares `Meta.fields = ['id', 'name', 'email']`.
func TestResponseShape_DRF_ModelSerializer_MetaFields(t *testing.T) {
	src := `from rest_framework import serializers
from rest_framework.response import Response

class UserSerializer(serializers.ModelSerializer):
    class Meta:
        model = User
        fields = ['id', 'name', 'email']

@app.route("/api/users/<int:id>", methods=["GET"])
def get_user(id):
    serializer = UserSerializer(User.objects.get(pk=id))
    return Response(serializer.data)
`
	_, res := runDetect(t, "python", "views.py", src)
	props := shapeOf(t, res, "http:GET:/api/users/{id}")
	for _, want := range []string{"id", "name", "email"} {
		if !strings.Contains(props["response_keys"], want) {
			t.Errorf("DRF ModelSerializer Meta.fields: expected response_keys to contain %q; got %q", want, props["response_keys"])
		}
	}
	assertProp(t, props, "response_keys_source", "drf_serializer")
}

// TestResponseShape_DRF_NestedSerializer exercises a Serializer that contains
// a nested serializer field. The nested field name itself becomes a response_key.
func TestResponseShape_DRF_NestedSerializer(t *testing.T) {
	src := `from rest_framework import serializers
from rest_framework.response import Response

class AddressSerializer(serializers.Serializer):
    street = serializers.CharField()
    city = serializers.CharField()

class UserSerializer(serializers.Serializer):
    id = serializers.IntegerField()
    name = serializers.CharField()
    address = AddressSerializer()

@app.route("/api/users/<int:id>", methods=["GET"])
def get_user(id):
    serializer = UserSerializer(instance)
    return Response(serializer.data)
`
	_, res := runDetect(t, "python", "views.py", src)
	props := shapeOf(t, res, "http:GET:/api/users/{id}")
	for _, want := range []string{"id", "name", "address"} {
		if !strings.Contains(props["response_keys"], want) {
			t.Errorf("DRF nested serializer: expected response_keys to contain %q; got %q", want, props["response_keys"])
		}
	}
}

// TestResponseShape_DRF_ModelSerializer_MetaFields_Tuple exercises
// Meta.fields declared with a tuple instead of a list.
func TestResponseShape_DRF_ModelSerializer_MetaFields_Tuple(t *testing.T) {
	src := `from rest_framework import serializers
from rest_framework.response import Response

class ProductSerializer(serializers.ModelSerializer):
    class Meta:
        model = Product
        fields = ('sku', 'price', 'stock')

@app.route("/api/products/<int:pk>", methods=["GET"])
def get_product(pk):
    serializer = ProductSerializer(Product.objects.get(pk=pk))
    return Response(serializer.data)
`
	_, res := runDetect(t, "python", "views.py", src)
	props := shapeOf(t, res, "http:GET:/api/products/{pk}")
	for _, want := range []string{"sku", "price", "stock"} {
		if !strings.Contains(props["response_keys"], want) {
			t.Errorf("DRF Meta.fields tuple: expected response_keys to contain %q; got %q", want, props["response_keys"])
		}
	}
}

// TestResponseShape_DRF_ResponseData_Bare exercises `return serializer.data`
// (without a Response(...) wrapper) — a common shorthand in DRF APIViews.
func TestResponseShape_DRF_ResponseData_Bare(t *testing.T) {
	src := `from rest_framework import serializers
from rest_framework.views import APIView

class CommentSerializer(serializers.Serializer):
    id = serializers.IntegerField()
    body = serializers.CharField()
    author = serializers.CharField()

@app.route("/api/comments/<int:id>", methods=["GET"])
def get_comment(id):
    serializer = CommentSerializer(Comment.objects.get(pk=id))
    return serializer.data
`
	_, res := runDetect(t, "python", "views.py", src)
	props := shapeOf(t, res, "http:GET:/api/comments/{id}")
	for _, want := range []string{"id", "body", "author"} {
		if !strings.Contains(props["response_keys"], want) {
			t.Errorf("DRF bare .data: expected response_keys to contain %q; got %q", want, props["response_keys"])
		}
	}
	assertProp(t, props, "response_keys_source", "drf_serializer")
}

// ---------------------------------------------------------------------------
// Java DTO class walking (Spring / JAX-RS)
// ---------------------------------------------------------------------------

// TestResponseShape_Java_Record exercises a Java 17+ record DTO.
// Records declare their components in the constructor parameter list;
// each component becomes a response_key.
func TestResponseShape_Java_Record(t *testing.T) {
	src := `package com.example;
import org.springframework.web.bind.annotation.*;

public record UserDTO(Long id, String name, String email) {}

@RestController
public class UserController {
    @GetMapping("/users/{id}")
    public UserDTO getUser(@PathVariable Long id) {
        return new UserDTO(id, "alice", "a@b");
    }
}
`
	_, res := runDetect(t, "java", "UserController.java", src)
	props := shapeOf(t, res, "http:ANY:/users/{id}")
	for _, want := range []string{"id", "name", "email"} {
		if !strings.Contains(props["response_keys"], want) {
			t.Errorf("Java record: expected response_keys to contain %q; got %q (all: %v)", want, props["response_keys"], props)
		}
	}
	assertProp(t, props, "response_keys_source", "java_dto")
}

// TestResponseShape_Java_LombokValue exercises a Lombok @Value class.
// All declared fields (private final) are included as response_keys.
func TestResponseShape_Java_LombokValue(t *testing.T) {
	src := `package com.example;
import lombok.Value;
import org.springframework.web.bind.annotation.*;

@Value
public class OrderDTO {
    Long id;
    String status;
    Double total;
}

@RestController
public class OrderController {
    @GetMapping("/orders/{id}")
    public OrderDTO getOrder(@PathVariable Long id) {
        return new OrderDTO(id, "PENDING", 99.99);
    }
}
`
	_, res := runDetect(t, "java", "OrderController.java", src)
	props := shapeOf(t, res, "http:ANY:/orders/{id}")
	for _, want := range []string{"id", "status", "total"} {
		if !strings.Contains(props["response_keys"], want) {
			t.Errorf("Lombok @Value: expected response_keys to contain %q; got %q (all: %v)", want, props["response_keys"], props)
		}
	}
	assertProp(t, props, "response_keys_source", "java_dto")
}

// TestResponseShape_Java_LombokData exercises a Lombok @Data class.
func TestResponseShape_Java_LombokData(t *testing.T) {
	src := `package com.example;
import lombok.Data;
import org.springframework.web.bind.annotation.*;

@Data
public class ProductDTO {
    private Long id;
    private String sku;
    private Double price;
}

@RestController
public class ProductController {
    @GetMapping("/products/{id}")
    public ProductDTO getProduct(@PathVariable Long id) {
        return new ProductDTO();
    }
}
`
	_, res := runDetect(t, "java", "ProductController.java", src)
	props := shapeOf(t, res, "http:ANY:/products/{id}")
	for _, want := range []string{"id", "sku", "price"} {
		if !strings.Contains(props["response_keys"], want) {
			t.Errorf("Lombok @Data: expected response_keys to contain %q; got %q (all: %v)", want, props["response_keys"], props)
		}
	}
	assertProp(t, props, "response_keys_source", "java_dto")
}

// TestResponseShape_Java_JsonProperty exercises a plain Java class where
// fields are annotated with @JsonProperty. The annotation string value
// is used as the response_key (supporting alias names).
func TestResponseShape_Java_JsonProperty(t *testing.T) {
	src := `package com.example;
import com.fasterxml.jackson.annotation.JsonProperty;
import org.springframework.web.bind.annotation.*;

public class CustomerDTO {
    @JsonProperty("customer_id")
    private Long id;

    @JsonProperty("full_name")
    private String name;

    @JsonProperty("email_address")
    private String email;
}

@RestController
public class CustomerController {
    @GetMapping("/customers/{id}")
    public CustomerDTO getCustomer(@PathVariable Long id) {
        return new CustomerDTO();
    }
}
`
	_, res := runDetect(t, "java", "CustomerController.java", src)
	props := shapeOf(t, res, "http:ANY:/customers/{id}")
	for _, want := range []string{"customer_id", "full_name", "email_address"} {
		if !strings.Contains(props["response_keys"], want) {
			t.Errorf("@JsonProperty: expected response_keys to contain %q; got %q (all: %v)", want, props["response_keys"], props)
		}
	}
	assertProp(t, props, "response_keys_source", "java_dto")
}

// TestResponseShape_Java_ResponseEntity_DTO exercises a Spring controller that
// returns ResponseEntity<SomeDTO> with a Lombok @Value DTO.
func TestResponseShape_Java_ResponseEntity_DTO(t *testing.T) {
	src := `package com.example;
import lombok.Value;
import org.springframework.http.ResponseEntity;
import org.springframework.web.bind.annotation.*;

@Value
public class InvoiceDTO {
    Long invoiceId;
    String description;
    Double amount;
}

@RestController
public class InvoiceController {
    @GetMapping("/invoices/{id}")
    public ResponseEntity<InvoiceDTO> getInvoice(@PathVariable Long id) {
        return ResponseEntity.ok(new InvoiceDTO(id, "Service fee", 120.0));
    }
}
`
	_, res := runDetect(t, "java", "InvoiceController.java", src)
	props := shapeOf(t, res, "http:ANY:/invoices/{id}")
	for _, want := range []string{"invoiceId", "description", "amount"} {
		if !strings.Contains(props["response_keys"], want) {
			t.Errorf("ResponseEntity<DTO> Lombok: expected response_keys to contain %q; got %q (all: %v)", want, props["response_keys"], props)
		}
	}
	assertProp(t, props, "response_keys_source", "java_dto")
}

// TestResponseShape_Java_Record_WithAnnotations exercises a Java record whose
// components have annotations (common with validation or Jackson annotations).
func TestResponseShape_Java_Record_WithAnnotations(t *testing.T) {
	src := `package com.example;
import com.fasterxml.jackson.annotation.JsonProperty;
import org.springframework.web.bind.annotation.*;

public record PaymentDTO(
    @JsonProperty("payment_id") Long id,
    String currency,
    Double amount
) {}

@RestController
public class PaymentController {
    @GetMapping("/payments/{id}")
    public PaymentDTO getPayment(@PathVariable Long id) {
        return new PaymentDTO(id, "USD", 50.0);
    }
}
`
	_, res := runDetect(t, "java", "PaymentController.java", src)
	props := shapeOf(t, res, "http:ANY:/payments/{id}")
	// The record has "id", "currency", "amount" as component names.
	for _, want := range []string{"currency", "amount"} {
		if !strings.Contains(props["response_keys"], want) {
			t.Errorf("record with annotations: expected response_keys to contain %q; got %q (all: %v)", want, props["response_keys"], props)
		}
	}
}
