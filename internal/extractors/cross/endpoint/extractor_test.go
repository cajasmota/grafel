package endpoint

import (
	"context"
	"strings"
	"testing"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func runExtract(t *testing.T, path, lang, source string) []types.EntityRecord {
	t.Helper()
	e := &Extractor{}
	recs, err := e.Extract(context.Background(), extractor.FileInput{
		Path:     path,
		Content:  []byte(source),
		Language: lang,
	})
	if err != nil {
		t.Fatalf("Extract error: %v", err)
	}
	return recs
}

// endpointRecords filters runExtract output down to entities produced by the
// endpoint extractor itself. changed the entity Kind from
// "SCOPE.Endpoint" (not in the graph's allowlist) to "SCOPE.Operation", so the
// filter now keys on the provenance marker that only this extractor sets.
func endpointRecords(recs []types.EntityRecord) []types.EntityRecord {
	var out []types.EntityRecord
	for _, r := range recs {
		if r.Kind == "SCOPE.Operation" && r.Properties["provenance"] == "INFERRED_FROM_FRAMEWORK_ROUTER" {
			out = append(out, r)
		}
	}
	return out
}

func findByMethodPath(t *testing.T, recs []types.EntityRecord, method, path string) types.EntityRecord {
	t.Helper()
	for _, r := range recs {
		if r.Properties["method"] == method && r.Properties["path"] == path {
			return r
		}
	}
	t.Fatalf("no endpoint found with method=%s path=%s", method, path)
	return types.EntityRecord{}
}

// ---------------------------------------------------------------------------
// Interface / registration
// ---------------------------------------------------------------------------

func TestLanguageKey(t *testing.T) {
	e := &Extractor{}
	if got := e.Language(); got != "_cross_endpoint" {
		t.Errorf("Language()=%q, want _cross_endpoint", got)
	}
}

func TestEmptyFileReturnsNil(t *testing.T) {
	recs := runExtract(t, "empty.go", "go", "")
	if len(recs) != 0 {
		t.Errorf("expected 0 entities, got %d", len(recs))
	}
}

func TestNoFrameworkSkipsFile(t *testing.T) {
	// Pure stdlib Go file — no web framework import present.
	src := `package main
import "fmt"
func main() { fmt.Println("hi") }`
	recs := runExtract(t, "main.go", "go", src)
	if len(recs) != 0 {
		t.Errorf("expected 0 entities from non-web file, got %d", len(recs))
	}
}

// ---------------------------------------------------------------------------
// Path normalisation
// ---------------------------------------------------------------------------

func TestNormalisePath_Express(t *testing.T) {
	p, params, ok := normalisePath("/users/:id/posts/:postId")
	if !ok || p != "/users/{id}/posts/{postId}" {
		t.Errorf("got %q params=%v ok=%v", p, params, ok)
	}
	if len(params) != 2 || params[0] != "id" || params[1] != "postId" {
		t.Errorf("params=%v", params)
	}
}

func TestNormalisePath_Flask(t *testing.T) {
	p, params, _ := normalisePath("/users/<int:user_id>")
	if p != "/users/{user_id}" {
		t.Errorf("got %q", p)
	}
	if len(params) != 1 || params[0] != "user_id" {
		t.Errorf("params=%v", params)
	}
}

func TestNormalisePath_FastAPI(t *testing.T) {
	p, params, _ := normalisePath("/items/{item_id}")
	if p != "/items/{item_id}" {
		t.Errorf("got %q", p)
	}
	if len(params) != 1 || params[0] != "item_id" {
		t.Errorf("params=%v", params)
	}
}

func TestNormalisePath_TrailingSlashStripped(t *testing.T) {
	p, _, _ := normalisePath("/users/")
	if p != "/users" {
		t.Errorf("got %q, want /users", p)
	}
}

func TestNormalisePath_RootPreserved(t *testing.T) {
	p, _, _ := normalisePath("/")
	if p != "/" {
		t.Errorf("got %q, want /", p)
	}
}

func TestNormalisePath_NoLeadingSlashFixed(t *testing.T) {
	p, _, _ := normalisePath("users/{id}")
	if p != "/users/{id}" {
		t.Errorf("got %q", p)
	}
}

func TestNormalisePath_EmptyFails(t *testing.T) {
	_, _, ok := normalisePath("")
	if ok {
		t.Error("expected ok=false for empty input")
	}
}

// ---------------------------------------------------------------------------
// REST — Gin
// ---------------------------------------------------------------------------

func TestGin_GET(t *testing.T) {
	src := `package api
import "github.com/gin-gonic/gin"
func setup(router *gin.Engine) {
  router.GET("/users/:id", getUser)
}`
	recs := endpointRecords(runExtract(t, "routes.go", "go", src))
	if len(recs) != 1 {
		t.Fatalf("expected 1 endpoint, got %d", len(recs))
	}
	ep := recs[0]
	if ep.Properties["method"] != "GET" {
		t.Errorf("method=%q", ep.Properties["method"])
	}
	if ep.Properties["path"] != "/users/{id}" {
		t.Errorf("path=%q", ep.Properties["path"])
	}
	if ep.Properties["framework"] != "gin" {
		t.Errorf("framework=%q", ep.Properties["framework"])
	}
	if len(ep.Relationships) != 1 || ep.Relationships[0].Kind != "SERVES" {
		t.Errorf("expected SERVES edge, got %+v", ep.Relationships)
	}
}

func TestGin_AllVerbs(t *testing.T) {
	src := `package api
import "github.com/gin-gonic/gin"
func setup(r *gin.Engine) {
  r.GET("/a", h1)
  r.POST("/b", h2)
  r.PUT("/c", h3)
  r.DELETE("/d", h4)
  r.PATCH("/e", h5)
  r.HEAD("/f", h6)
  r.OPTIONS("/g", h7)
}`
	recs := endpointRecords(runExtract(t, "routes.go", "go", src))
	if len(recs) != 7 {
		t.Fatalf("expected 7 endpoints, got %d", len(recs))
	}
	methods := map[string]bool{}
	for _, r := range recs {
		methods[r.Properties["method"]] = true
	}
	for _, want := range []string{"GET", "POST", "PUT", "DELETE", "PATCH", "HEAD", "OPTIONS"} {
		if !methods[want] {
			t.Errorf("missing method %s", want)
		}
	}
}

// ---------------------------------------------------------------------------
// REST — Express
// ---------------------------------------------------------------------------

func TestExpress_GET(t *testing.T) {
	src := `const express = require('express');
const app = express();
app.get('/api/users/:id', getUser);
app.post('/api/users', createUser);`
	recs := endpointRecords(runExtract(t, "server.js", "javascript", src))
	if len(recs) != 2 {
		t.Fatalf("expected 2 endpoints, got %d", len(recs))
	}
	ep := findByMethodPath(t, recs, "GET", "/api/users/{id}")
	if ep.Properties["framework"] != "express" {
		t.Errorf("framework=%q", ep.Properties["framework"])
	}
	if ep.Properties["handler_ref"] != "getUser" {
		t.Errorf("handler_ref=%q", ep.Properties["handler_ref"])
	}
}

// TestExpress_MiddlewareNotHandler — issue #126.
//
// `router.METHOD(path, middleware..., handler)` must NOT capture middleware
// (`auth.required`, `auth.optional`) or the inline-arrow keyword (`async`)
// as the handler. Pre-fix the express regex took the first identifier after
// the path string, producing SERVES targets like
// `scope:operation:src/.../auth.controller.ts#auth.required` /
// `...#async` that no extractor ever emits — they dominated the
// express-realworld bug-extractor disposition.
func TestExpress_MiddlewareNotHandler(t *testing.T) {
	src := `import express from 'express';
import { auth } from './auth';
const router = express.Router();
router.get('/user', auth.required, async (req, res) => { res.json({}); });
router.post('/user', auth.required, auth.audit, function (req, res) { res.json({}); });
router.put('/x', wrap(handler));
router.delete('/y', namedHandler);
`
	recs := endpointRecords(runExtract(t, "auth.controller.ts", "typescript", src))
	if len(recs) != 4 {
		t.Fatalf("expected 4 endpoints, got %d", len(recs))
	}
	for _, ep := range recs {
		h := ep.Properties["handler_ref"]
		switch ep.Properties["method"] {
		case "GET", "POST":
			if h != "" {
				t.Errorf("%s %s: handler_ref=%q want empty (inline arrow / fn after middleware)",
					ep.Properties["method"], ep.Properties["path"], h)
			}
		case "PUT":
			if h != "" {
				t.Errorf("PUT: handler_ref=%q want empty (call expression)", h)
			}
		case "DELETE":
			if h != "namedHandler" {
				t.Errorf("DELETE: handler_ref=%q want namedHandler", h)
			}
		}
	}
}

func TestExpress_All_NormalisesToANY(t *testing.T) {
	src := `import express from 'express';
const router = express.Router();
router.all('/health', healthHandler);`
	recs := endpointRecords(runExtract(t, "r.js", "javascript", src))
	if len(recs) != 1 {
		t.Fatalf("expected 1 endpoint, got %d", len(recs))
	}
	if recs[0].Properties["method"] != "ANY" {
		t.Errorf("method=%q, want ANY", recs[0].Properties["method"])
	}
}

// ---------------------------------------------------------------------------
// REST — FastAPI
// ---------------------------------------------------------------------------

func TestFastAPI_GET(t *testing.T) {
	src := `from fastapi import FastAPI
app = FastAPI()

@app.get("/users/{user_id}")
async def read_user(user_id: int):
    return {"id": user_id}
`
	recs := endpointRecords(runExtract(t, "main.py", "python", src))
	if len(recs) != 1 {
		t.Fatalf("expected 1 endpoint, got %d", len(recs))
	}
	ep := recs[0]
	if ep.Properties["method"] != "GET" || ep.Properties["path"] != "/users/{user_id}" {
		t.Errorf("method=%s path=%s", ep.Properties["method"], ep.Properties["path"])
	}
	if ep.Properties["framework"] != "fastapi" {
		t.Errorf("framework=%s", ep.Properties["framework"])
	}
	if ep.Properties["handler_ref"] != "read_user" {
		t.Errorf("handler_ref=%s", ep.Properties["handler_ref"])
	}
	if ep.Properties["params_csv"] != "user_id" {
		t.Errorf("params_csv=%s", ep.Properties["params_csv"])
	}
}

// ---------------------------------------------------------------------------
// REST — Flask
// ---------------------------------------------------------------------------

func TestFlask_DefaultGET(t *testing.T) {
	src := `from flask import Flask
app = Flask(__name__)

@app.route("/health")
def health():
    return "ok"
`
	recs := endpointRecords(runExtract(t, "app.py", "python", src))
	if len(recs) != 1 {
		t.Fatalf("expected 1 endpoint, got %d", len(recs))
	}
	if recs[0].Properties["method"] != "GET" {
		t.Errorf("method=%s", recs[0].Properties["method"])
	}
}

func TestFlask_ExplicitMethods(t *testing.T) {
	src := `from flask import Flask
app = Flask(__name__)

@app.route("/users/<int:uid>", methods=["GET", "POST"])
def users(uid):
    pass
`
	recs := endpointRecords(runExtract(t, "app.py", "python", src))
	if len(recs) != 2 {
		t.Fatalf("expected 2 endpoints (GET+POST), got %d", len(recs))
	}
	seen := map[string]bool{}
	for _, r := range recs {
		seen[r.Properties["method"]] = true
		if r.Properties["path"] != "/users/{uid}" {
			t.Errorf("path=%s", r.Properties["path"])
		}
	}
	if !seen["GET"] || !seen["POST"] {
		t.Errorf("methods=%v", seen)
	}
}

// ---------------------------------------------------------------------------
// REST — Spring (acceptance criteria)
// ---------------------------------------------------------------------------

func TestSpring_AcceptanceCriteria(t *testing.T) {
	src := `package com.example.api;
import org.springframework.web.bind.annotation.*;

@RestController
public class UserController {
    @GetMapping("/api/users/{id}")
    public User getUser(@PathVariable String id) {
        return userService.find(id);
    }
}`
	recs := endpointRecords(runExtract(t, "UserController.java", "java", src))
	if len(recs) != 1 {
		t.Fatalf("expected 1 endpoint, got %d", len(recs))
	}
	ep := recs[0]
	if ep.Properties["method"] != "GET" {
		t.Errorf("method=%s, want GET", ep.Properties["method"])
	}
	if ep.Properties["path"] != "/api/users/{id}" {
		t.Errorf("path=%s, want /api/users/{id}", ep.Properties["path"])
	}
	if ep.Properties["params_csv"] != "id" {
		t.Errorf("params_csv=%s, want id", ep.Properties["params_csv"])
	}
	if ep.Properties["framework"] != "spring" {
		t.Errorf("framework=%s, want spring", ep.Properties["framework"])
	}
	if ep.Properties["handler_ref"] != "getUser" {
		t.Errorf("handler_ref=%s, want getUser", ep.Properties["handler_ref"])
	}
	// SERVES edge must exist and link to getUser
	if len(ep.Relationships) != 1 {
		t.Fatalf("expected 1 SERVES edge, got %d", len(ep.Relationships))
	}
	rel := ep.Relationships[0]
	if rel.Kind != "SERVES" {
		t.Errorf("edge kind=%s", rel.Kind)
	}
	if !strings.Contains(rel.ToID, "getUser") {
		t.Errorf("edge to_id=%s should reference getUser", rel.ToID)
	}
}

func TestSpring_RequestMappingNoMethodDefaultsGET(t *testing.T) {
	src := `package com.example;
import org.springframework.web.bind.annotation.RequestMapping;

public class C {
    @RequestMapping("/legacy")
    public String legacy() { return "x"; }
}`
	recs := endpointRecords(runExtract(t, "C.java", "java", src))
	if len(recs) != 1 {
		t.Fatalf("got %d", len(recs))
	}
	if recs[0].Properties["method"] != "GET" {
		t.Errorf("method=%s", recs[0].Properties["method"])
	}
}

// ---------------------------------------------------------------------------
// REST — Django
// ---------------------------------------------------------------------------

func TestDjango_UrlsPy(t *testing.T) {
	src := `from django.urls import path
from . import views

urlpatterns = [
    path('users/<int:user_id>/', views.user_detail, name='user_detail'),
    path('health/', views.health),
]`
	recs := endpointRecords(runExtract(t, "urls.py", "python", src))
	if len(recs) != 2 {
		t.Fatalf("expected 2, got %d", len(recs))
	}
}

// ---------------------------------------------------------------------------
// REST — Phoenix
// ---------------------------------------------------------------------------

func TestPhoenix_Router(t *testing.T) {
	src := `defmodule MyApp.Router do
  use Phoenix.Router
  import Plug.Conn

  scope "/api" do
    get "/users/:id", UserController, :show
    post "/users", UserController, :create
  end
end`
	recs := endpointRecords(runExtract(t, "router.ex", "elixir", src))
	if len(recs) != 2 {
		t.Fatalf("expected 2, got %d", len(recs))
	}
	ep := findByMethodPath(t, recs, "GET", "/users/{id}")
	if ep.Properties["handler_ref"] != "UserController.show" {
		t.Errorf("handler_ref=%s", ep.Properties["handler_ref"])
	}
}

// ---------------------------------------------------------------------------
// REST — ASP.NET
// ---------------------------------------------------------------------------

func TestASPNet_Attribute(t *testing.T) {
	src := `using Microsoft.AspNetCore.Mvc;

namespace Api.Controllers {
    public class UserController : ControllerBase {
        [HttpGet("/api/users/{id}")]
        public async Task<IActionResult> GetUser(string id) { return Ok(); }
    }
}`
	recs := endpointRecords(runExtract(t, "UserController.cs", "csharp", src))
	if len(recs) != 1 {
		t.Fatalf("got %d", len(recs))
	}
	ep := recs[0]
	if ep.Properties["method"] != "GET" || ep.Properties["path"] != "/api/users/{id}" {
		t.Errorf("got method=%s path=%s", ep.Properties["method"], ep.Properties["path"])
	}
	if ep.Properties["handler_ref"] != "GetUser" {
		t.Errorf("handler_ref=%s", ep.Properties["handler_ref"])
	}
}

// ---------------------------------------------------------------------------
// REST — Rails
// ---------------------------------------------------------------------------

func TestRails_RoutesRb(t *testing.T) {
	src := `Rails.application.routes.draw do
  get '/users/:id', to: 'users#show'
  post '/users', to: 'users#create'
end`
	recs := endpointRecords(runExtract(t, "routes.rb", "ruby", src))
	if len(recs) != 2 {
		t.Fatalf("expected 2, got %d", len(recs))
	}
	ep := findByMethodPath(t, recs, "GET", "/users/{id}")
	if ep.Properties["handler_ref"] != "users#show" {
		t.Errorf("handler_ref=%s", ep.Properties["handler_ref"])
	}
}

// ---------------------------------------------------------------------------
// gRPC — .proto
// ---------------------------------------------------------------------------

func TestGRPC_Unary(t *testing.T) {
	src := `syntax = "proto3";
package svc;

service UserService {
    rpc GetUser(GetUserRequest) returns (GetUserResponse);
}`
	recs := endpointRecords(runExtract(t, "user.proto", "proto", src))
	if len(recs) != 1 {
		t.Fatalf("expected 1, got %d", len(recs))
	}
	ep := recs[0]
	if ep.Properties["method"] != "UNARY" {
		t.Errorf("method=%s, want UNARY", ep.Properties["method"])
	}
	if ep.Properties["path"] != "/UserService/GetUser" {
		t.Errorf("path=%s", ep.Properties["path"])
	}
	if ep.Properties["framework"] != "grpc" {
		t.Errorf("framework=%s", ep.Properties["framework"])
	}
	if ep.Subtype != "grpc" {
		t.Errorf("subtype=%s", ep.Subtype)
	}
}

func TestGRPC_AllStreamKinds(t *testing.T) {
	src := `syntax = "proto3";
service S {
    rpc Unary(Req) returns (Resp);
    rpc ServerStream(Req) returns (stream Resp);
    rpc ClientStream(stream Req) returns (Resp);
    rpc BidiStream(stream Req) returns (stream Resp);
}`
	recs := endpointRecords(runExtract(t, "s.proto", "proto", src))
	if len(recs) != 4 {
		t.Fatalf("expected 4, got %d", len(recs))
	}
	kinds := map[string]bool{}
	for _, r := range recs {
		kinds[r.Properties["method"]] = true
	}
	for _, want := range []string{"UNARY", "SERVER_STREAM", "CLIENT_STREAM", "BIDI_STREAM"} {
		if !kinds[want] {
			t.Errorf("missing kind %s", want)
		}
	}
}

// ---------------------------------------------------------------------------
// GraphQL SDL
// ---------------------------------------------------------------------------

func TestGraphQL_SDL(t *testing.T) {
	src := `type Query {
  user(id: ID!): User
  users: [User!]!
}

type Mutation {
  createUser(input: UserInput!): User
}

type Subscription {
  userCreated: User
}`
	recs := endpointRecords(runExtract(t, "schema.graphql", "graphql", src))
	if len(recs) != 4 {
		t.Fatalf("expected 4 endpoints, got %d", len(recs))
	}
	byOp := map[string]int{}
	for _, r := range recs {
		byOp[r.Properties["method"]]++
	}
	if byOp["QUERY"] != 2 || byOp["MUTATION"] != 1 || byOp["SUBSCRIPTION"] != 1 {
		t.Errorf("op counts=%v", byOp)
	}
}

func TestGraphQL_GQLExtension(t *testing.T) {
	src := `type Query {
  health: String
}`
	recs := endpointRecords(runExtract(t, "schema.gql", "graphql", src))
	if len(recs) != 1 {
		t.Fatalf("got %d", len(recs))
	}
}

// ---------------------------------------------------------------------------
// Handler-missing case (SERVES edge omitted)
// ---------------------------------------------------------------------------

func TestServesEdgeOmittedWhenHandlerUnknown(t *testing.T) {
	// FastAPI decorator with no subsequent def → handler cannot be resolved.
	src := `from fastapi import FastAPI
app = FastAPI()

@app.get("/orphan")
`
	recs := endpointRecords(runExtract(t, "x.py", "python", src))
	if len(recs) != 1 {
		t.Fatalf("expected 1 endpoint, got %d", len(recs))
	}
	ep := recs[0]
	if len(ep.Relationships) != 0 {
		t.Errorf("expected 0 SERVES edges, got %d", len(ep.Relationships))
	}
}

// ---------------------------------------------------------------------------
// Ambiguous frameworks → first in order wins
// ---------------------------------------------------------------------------

func TestAmbiguousFrameworks_DeterministicOrder(t *testing.T) {
	// File imports both express and fastapi (nonsense, but simulates ambiguity).
	// frameworkOrder puts gin first, then express, then fastapi — so express wins.
	src := `import express from 'express';
import fastapi from 'fastapi';
app.get('/x', handleX);`
	recs := endpointRecords(runExtract(t, "x.js", "javascript", src))
	if len(recs) != 1 {
		t.Fatalf("got %d", len(recs))
	}
	if recs[0].Properties["framework"] != "express" {
		t.Errorf("framework=%s, want express (first in order)", recs[0].Properties["framework"])
	}
}

// ---------------------------------------------------------------------------
// Import token extraction
// ---------------------------------------------------------------------------

func TestExtractImportTokens(t *testing.T) {
	src := `import { x } from 'express'
from fastapi import FastAPI
require('gin-gonic/gin')`
	tokens := extractImportTokens(src)
	if !tokens["express"] {
		t.Errorf("missing express")
	}
	if !tokens["fastapi"] {
		t.Errorf("missing fastapi")
	}
	// gin-gonic/gin should register both the full token and "gin-gonic" prefix
	if !tokens["gin-gonic/gin"] && !tokens["gin-gonic"] {
		t.Errorf("missing gin-gonic")
	}
}

func TestMatchesAnyImport_Substring(t *testing.T) {
	tokens := map[string]bool{"org.springframework.web.bind": true}
	if !matchesAnyImport(tokens, []string{"springframework"}) {
		t.Error("substring match failed")
	}
	if matchesAnyImport(tokens, []string{"django"}) {
		t.Error("false positive")
	}
}

// ---------------------------------------------------------------------------
// File-extension hints
// ---------------------------------------------------------------------------

func TestForceFrameworkFromExt(t *testing.T) {
	cases := map[string]string{
		"a.proto":          "grpc",
		"b.graphql":        "graphql",
		"c.gql":            "graphql",
		"d.graphqls":       "graphql",
		"e.go":             "",
		"f.py":             "",
		"PATH/UPPER.PROTO": "grpc",
	}
	for in, want := range cases {
		if got := forceFrameworkFromExt(in); got != want {
			t.Errorf("forceFrameworkFromExt(%q)=%q, want %q", in, got, want)
		}
	}
}

// ---------------------------------------------------------------------------
// Entity ID stability
// ---------------------------------------------------------------------------

func TestEndpointEntityID_Deterministic(t *testing.T) {
	a := endpointEntityID("f.go", "GET", "/x")
	b := endpointEntityID("f.go", "GET", "/x")
	if a != b {
		t.Errorf("non-deterministic: %s vs %s", a, b)
	}
	c := endpointEntityID("f.go", "POST", "/x")
	if a == c {
		t.Errorf("different methods must yield different IDs")
	}
}

func TestHandlerRef_EmptyQNameReturnsEmpty(t *testing.T) {
	if r := handlerRef("f.go", ""); r != "" {
		t.Errorf("expected empty, got %q", r)
	}
	if r := handlerRef("f.go", "foo"); r == "" {
		t.Errorf("expected non-empty")
	}
}
