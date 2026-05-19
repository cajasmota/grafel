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
func shapeOf(t *testing.T, res *DetectResult, id string) map[string]string {
	t.Helper()
	for _, e := range res.Entities {
		if e.Kind == httpEndpointKind && e.ID == id {
			return e.Properties
		}
	}
	t.Fatalf("expected http_endpoint %q in entities; got: %s", id, dumpEndpointIDs(res))
	return nil
}

func dumpEndpointIDs(res *DetectResult) string {
	var out []string
	for _, e := range res.Entities {
		if e.Kind == httpEndpointKind {
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
