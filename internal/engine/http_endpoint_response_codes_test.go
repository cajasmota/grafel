package engine

import "testing"

// These tests reuse the deprecProps / mustEndpoint harness (the response-codes
// pass runs in the same synthesis tail), and assert the SPECIFIC status codes on
// the SPECIFIC endpoint — not just len>0.

// ---------------------------------------------------------------------------
// FastAPI (Python)
// ---------------------------------------------------------------------------

func TestResponseCodes_FastAPI_DecoratorAndHTTPException(t *testing.T) {
	src := `
from fastapi import FastAPI, HTTPException
app = FastAPI()

@app.post("/users", status_code=201)
def create_user(payload: dict):
    if not payload:
        raise HTTPException(status_code=404)
    return payload
`
	eps := deprecProps(t, "python", "app/main.py", src)
	e := mustEndpoint(t, eps, "POST /users")
	if got := e.Properties["response_codes"]; got != "201,404" {
		t.Fatalf("response_codes=%q want 201,404 (props: %v)", got, e.Properties)
	}
	if got := e.Properties["success_code"]; got != "201" {
		t.Fatalf("success_code=%q want 201", got)
	}
}

func TestResponseCodes_FastAPI_JSONResponseLiteral(t *testing.T) {
	src := `
from fastapi import FastAPI
from fastapi.responses import JSONResponse
app = FastAPI()

@app.get("/health")
def health():
    return JSONResponse(status_code=200, content={"ok": True})
`
	eps := deprecProps(t, "python", "app/main.py", src)
	e := mustEndpoint(t, eps, "GET /health")
	if got := e.Properties["response_codes"]; got != "200" {
		t.Fatalf("response_codes=%q want 200", got)
	}
	if got := e.Properties["success_code"]; got != "200" {
		t.Fatalf("success_code=%q want 200", got)
	}
}

// ---------------------------------------------------------------------------
// DRF / Django (Python)
// ---------------------------------------------------------------------------

func TestResponseCodes_DRF_StatusConstant(t *testing.T) {
	src := `
from rest_framework.response import Response
from rest_framework import status

@app.post("/widgets")
def create(self, request):
    return Response(data, status=status.HTTP_403_FORBIDDEN)
`
	eps := deprecProps(t, "python", "app/views.py", src)
	e := mustEndpoint(t, eps, "POST /widgets")
	if got := e.Properties["response_codes"]; got != "403" {
		t.Fatalf("response_codes=%q want 403 (props: %v)", got, e.Properties)
	}
	// 403 is not 2xx → no success_code.
	if got := e.Properties["success_code"]; got != "" {
		t.Fatalf("success_code=%q want empty", got)
	}
}

func TestResponseCodes_DRF_RaisedException(t *testing.T) {
	src := `
from rest_framework.exceptions import NotFound
from rest_framework.response import Response
from rest_framework import status

@app.get("/things/{id}")
def retrieve(self, request, id):
    if id == 0:
        raise NotFound()
    return Response(data, status=status.HTTP_200_OK)
`
	eps := deprecProps(t, "python", "app/views.py", src)
	e := mustEndpoint(t, eps, "GET /things/{id}")
	if got := e.Properties["response_codes"]; got != "200,404" {
		t.Fatalf("response_codes=%q want 200,404 (props: %v)", got, e.Properties)
	}
	if got := e.Properties["success_code"]; got != "200" {
		t.Fatalf("success_code=%q want 200", got)
	}
}

// ---------------------------------------------------------------------------
// Express / Nest (JS/TS)
// ---------------------------------------------------------------------------

func TestResponseCodes_Express_StatusAndSendStatus(t *testing.T) {
	src := `
const express = require('express');
const app = express();

app.post('/orders', (req, res) => {
    if (!req.body) {
        return res.sendStatus(204);
    }
    res.status(201).json({ ok: true });
});
`
	eps := deprecProps(t, "javascript", "routes/orders.js", src)
	e := mustEndpoint(t, eps, "POST /orders")
	if got := e.Properties["response_codes"]; got != "201,204" {
		t.Fatalf("response_codes=%q want 201,204 (props: %v)", got, e.Properties)
	}
	// Two 2xx codes → success_code ambiguous, omitted.
	if got := e.Properties["success_code"]; got != "" {
		t.Fatalf("success_code=%q want empty (two 2xx)", got)
	}
}

func TestResponseCodes_Express_DynamicStatusSkipped(t *testing.T) {
	// res.status(dynamicVar) must NOT fabricate a code; the literal 200 still
	// records.
	src := `
const express = require('express');
const app = express();

app.get('/dyn', (req, res) => {
    const code = computeCode();
    if (req.query.ok) {
        return res.status(200).end();
    }
    res.status(code).end();
});
`
	eps := deprecProps(t, "javascript", "routes/dyn.js", src)
	e := mustEndpoint(t, eps, "GET /dyn")
	if got := e.Properties["response_codes"]; got != "200" {
		t.Fatalf("response_codes=%q want 200 (dynamic var skipped) (props: %v)", got, e.Properties)
	}
}

// ---------------------------------------------------------------------------
// Spring (Java)
// ---------------------------------------------------------------------------

func TestResponseCodes_Spring_ResponseStatusCreated(t *testing.T) {
	src := `
@RestController
@RequestMapping("/api/v1")
public class UserController {

    @PostMapping("/users")
    @ResponseStatus(HttpStatus.CREATED)
    public User create(@RequestBody User u) {
        return service.save(u);
    }
}
`
	eps := deprecProps(t, "java", "src/UserController.java", src)
	e := mustEndpoint(t, eps, "POST /api/v1/users")
	if got := e.Properties["response_codes"]; got != "201" {
		t.Fatalf("response_codes=%q want 201 (props: %v)", got, e.Properties)
	}
	if got := e.Properties["success_code"]; got != "201" {
		t.Fatalf("success_code=%q want 201", got)
	}
}

func TestResponseCodes_Spring_ResponseEntityNotFound(t *testing.T) {
	src := `
@RestController
@RequestMapping("/api/v1")
public class ItemController {

    @GetMapping("/items")
    public ResponseEntity<Item> get() {
        if (missing) {
            return ResponseEntity.notFound().build();
        }
        return ResponseEntity.ok(item);
    }
}
`
	eps := deprecProps(t, "java", "src/ItemController.java", src)
	e := mustEndpoint(t, eps, "GET /api/v1/items")
	if got := e.Properties["response_codes"]; got != "200,404" {
		t.Fatalf("response_codes=%q want 200,404 (props: %v)", got, e.Properties)
	}
	if got := e.Properties["success_code"]; got != "200" {
		t.Fatalf("success_code=%q want 200", got)
	}
}

// ---------------------------------------------------------------------------
// Negative: a status literal outside any endpoint handler is not attributed.
// ---------------------------------------------------------------------------

func TestResponseCodes_NoLiteralLeavesAbsent(t *testing.T) {
	src := `
const express = require('express');
const app = express();

app.get('/plain', (req, res) => {
    res.json({ ok: true });
});
`
	eps := deprecProps(t, "javascript", "routes/plain.js", src)
	e := mustEndpoint(t, eps, "GET /plain")
	if got := e.Properties["response_codes"]; got != "" {
		t.Fatalf("response_codes=%q want absent (no literal)", got)
	}
}

// ---------------------------------------------------------------------------
// Go — gin / echo / fiber / net-http (#3920)
//
// Go route registration and handler are separate functions; the resolver
// locates the handler via source_handler and scans its real body. Assert the
// SPECIFIC code set on the SPECIFIC endpoint.
// ---------------------------------------------------------------------------

func TestResponseCodes_Go_Gin_JSONStatusConstants(t *testing.T) {
	src := `
package main

import (
	"net/http"
	"github.com/gin-gonic/gin"
)

func CreateUser(c *gin.Context) {
	var payload map[string]any
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"id": 1})
}

func main() {
	r := gin.Default()
	r.POST("/users", CreateUser)
	r.Run()
}
`
	eps := deprecProps(t, "go", "main.go", src)
	e := mustEndpoint(t, eps, "POST /users")
	if got := e.Properties["response_codes"]; got != "201,400" {
		t.Fatalf("response_codes=%q want 201,400 (props: %v)", got, e.Properties)
	}
	if got := e.Properties["success_code"]; got != "201" {
		t.Fatalf("success_code=%q want 201", got)
	}
	if got := e.Properties["response_codes_source"]; got != "status call" {
		t.Fatalf("response_codes_source=%q want 'status call'", got)
	}
}

func TestResponseCodes_Go_Gin_NumericAndAbort(t *testing.T) {
	src := `
package main

import "github.com/gin-gonic/gin"

func DeleteUser(c *gin.Context) {
	if !ok(c) {
		c.AbortWithStatus(403)
		return
	}
	c.Status(204)
}

func reg() {
	r := gin.Default()
	r.DELETE("/users/:id", DeleteUser)
}
`
	eps := deprecProps(t, "go", "main.go", src)
	e := mustEndpoint(t, eps, "DELETE /users/{id}")
	if got := e.Properties["response_codes"]; got != "204,403" {
		t.Fatalf("response_codes=%q want 204,403 (props: %v)", got, e.Properties)
	}
	if got := e.Properties["success_code"]; got != "204" {
		t.Fatalf("success_code=%q want 204", got)
	}
}

func TestResponseCodes_Go_Echo_NewHTTPError(t *testing.T) {
	src := `
package main

import (
	"net/http"
	"github.com/labstack/echo/v4"
)

func GetUser(c echo.Context) error {
	if missing(c) {
		return echo.NewHTTPError(http.StatusNotFound, "no user")
	}
	return c.JSON(http.StatusOK, user{})
}

func reg(e *echo.Echo) {
	e.GET("/users/:id", GetUser)
}
`
	eps := deprecProps(t, "go", "main.go", src)
	e := mustEndpoint(t, eps, "GET /users/{id}")
	if got := e.Properties["response_codes"]; got != "200,404" {
		t.Fatalf("response_codes=%q want 200,404 (props: %v)", got, e.Properties)
	}
	if got := e.Properties["success_code"]; got != "200" {
		t.Fatalf("success_code=%q want 200", got)
	}
}

func TestResponseCodes_Go_Fiber_StatusAndNewError(t *testing.T) {
	src := `
package main

import (
	"github.com/gofiber/fiber/v2"
)

func ListItems(c *fiber.Ctx) error {
	if broken(c) {
		return fiber.NewError(fiber.StatusInternalServerError, "boom")
	}
	return c.Status(fiber.StatusOK).JSON(fiber.Map{"items": nil})
}

func reg(app *fiber.App) {
	app.Get("/items", ListItems)
}
`
	eps := deprecProps(t, "go", "main.go", src)
	e := mustEndpoint(t, eps, "GET /items")
	if got := e.Properties["response_codes"]; got != "200,500" {
		t.Fatalf("response_codes=%q want 200,500 (props: %v)", got, e.Properties)
	}
}

func TestResponseCodes_Go_NetHTTP_WriteHeaderAndError(t *testing.T) {
	src := `
package main

import "net/http"

func createThing(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "bad", http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusCreated)
}

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /things", createThing)
	http.ListenAndServe(":8080", mux)
}
`
	eps := deprecProps(t, "go", "main.go", src)
	e := mustEndpoint(t, eps, "POST /things")
	if got := e.Properties["response_codes"]; got != "201,400" {
		t.Fatalf("response_codes=%q want 201,400 (props: %v)", got, e.Properties)
	}
	if got := e.Properties["success_code"]; got != "201" {
		t.Fatalf("success_code=%q want 201", got)
	}
}

// Negative: a Go handler with NO explicit status literal leaves response_codes
// absent — the framework default 200 is not fabricated. (Honest-partial.)
func TestResponseCodes_Go_NoLiteralLeavesAbsent(t *testing.T) {
	src := `
package main

import "github.com/gin-gonic/gin"

func health(c *gin.Context) {
	code := pick()
	c.JSON(code, gin.H{"ok": true})
}

func reg() {
	r := gin.Default()
	r.GET("/health", health)
}
`
	eps := deprecProps(t, "go", "main.go", src)
	e := mustEndpoint(t, eps, "GET /health")
	if got := e.Properties["response_codes"]; got != "" {
		t.Fatalf("response_codes=%q want absent (dynamic status var, no literal)", got)
	}
}

// ---------------------------------------------------------------------------
// Go HTTP-framework siblings: buffalo / hertz / fasthttp (#3818)
//
// The goResponseCodes idiom set (status-first-arg call verbs + the stdlib code
// table) already covers these frameworks once the constant family / verb is in
// scope — route synthesis sets source_handler the same way for every Go
// framework, so the handler body is reachable identically. These are
// value-asserting parity tests for the trailing siblings the coverage parity
// probe flagged as MISSING against the flagship gin/echo/chi cohort.
// ---------------------------------------------------------------------------

// buffalo renders with the stdlib http.Status* constant via c.Render — already
// matched by the Render verb + http.Status* family (CREDIT, no new idiom). The
// happy path is 201, the validation branch 400.
func TestResponseCodes_Go_Buffalo_RenderStatus(t *testing.T) {
	src := `
package actions

import (
	"net/http"

	"github.com/gobuffalo/buffalo"
	"github.com/gobuffalo/buffalo/render"
)

var r = render.New(render.Options{})

func UsersCreate(c buffalo.Context) error {
	if c.Param("bad") != "" {
		return c.Render(http.StatusBadRequest, r.JSON(map[string]string{"e": "bad"}))
	}
	return c.Render(http.StatusCreated, r.JSON(map[string]int{"id": 1}))
}

func setup(app *buffalo.App) {
	app.POST("/users", UsersCreate)
}
`
	eps := deprecProps(t, "go", "main.go", src)
	e := mustEndpoint(t, eps, "POST /users")
	if got := e.Properties["response_codes"]; got != "201,400" {
		t.Fatalf("response_codes=%q want 201,400 (props: %v)", got, e.Properties)
	}
	if got := e.Properties["success_code"]; got != "201" {
		t.Fatalf("success_code=%q want 201", got)
	}
	if got := e.Properties["response_codes_source"]; got != "status call" {
		t.Fatalf("response_codes_source=%q want 'status call'", got)
	}
}

// hertz (CloudWeGo) returns via c.JSON(consts.StatusXxx, x) and
// c.SetStatusCode(consts.StatusXxx). The consts.Status* family mirrors the
// net/http code values; resolving it is the #3818 build for hertz. Mix a numeric
// literal in too (also valid hertz) to assert both paths land in the set.
func TestResponseCodes_Go_Hertz_ConstsStatus(t *testing.T) {
	src := `
package main

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
)

func CreateUser(ctx context.Context, c *app.RequestContext) {
	if bad(c) {
		c.JSON(400, map[string]string{"e": "bad"})
		return
	}
	c.SetStatusCode(consts.StatusNoContent)
	c.JSON(consts.StatusCreated, map[string]int{"id": 1})
}

func main() {
	h := server.Default()
	h.POST("/users", CreateUser)
	h.Spin()
}
`
	eps := deprecProps(t, "go", "main.go", src)
	e := mustEndpoint(t, eps, "POST /users")
	if got := e.Properties["response_codes"]; got != "201,204,400" {
		t.Fatalf("response_codes=%q want 201,204,400 (props: %v)", got, e.Properties)
	}
	// Two distinct 2xx codes (201 + 204) ⇒ success_code is ambiguous, omitted.
	if got := e.Properties["success_code"]; got != "" {
		t.Fatalf("success_code=%q want absent (two 2xx codes ⇒ ambiguous)", got)
	}
	if got := e.Properties["response_codes_source"]; got != "status call" {
		t.Fatalf("response_codes_source=%q want 'status call'", got)
	}
}

// fasthttp sets the status with ctx.SetStatusCode(fasthttp.StatusXxx). The
// fasthttp.Status* family also mirrors net/http; this is the #3818 build for
// fasthttp (the SetStatusCode verb + fasthttp.Status* family).
func TestResponseCodes_Go_Fasthttp_SetStatusCode(t *testing.T) {
	src := `
package main

import (
	"github.com/fasthttp/router"
	"github.com/valyala/fasthttp"
)

func CreateUser(ctx *fasthttp.RequestCtx) {
	if !valid(ctx) {
		ctx.SetStatusCode(fasthttp.StatusUnprocessableEntity)
		return
	}
	ctx.SetStatusCode(fasthttp.StatusCreated)
}

func main() {
	r := router.New()
	r.POST("/users", CreateUser)
	fasthttp.ListenAndServe(":8080", r.Handler)
}
`
	eps := deprecProps(t, "go", "main.go", src)
	e := mustEndpoint(t, eps, "POST /users")
	if got := e.Properties["response_codes"]; got != "201,422" {
		t.Fatalf("response_codes=%q want 201,422 (props: %v)", got, e.Properties)
	}
	if got := e.Properties["success_code"]; got != "201" {
		t.Fatalf("success_code=%q want 201", got)
	}
}

// Negative: a hertz handler whose status is a dynamic variable
// (c.SetStatusCode(code)) yields NO literal — response_codes stays absent, the
// framework default 200 is not fabricated. Asserts the consts.Status* addition
// did not loosen the honest-partial boundary.
func TestResponseCodes_Go_Hertz_DynamicStatusAbsent(t *testing.T) {
	src := `
package main

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/app/server"
)

func Status(ctx context.Context, c *app.RequestContext) {
	code := pick()
	c.SetStatusCode(code)
	c.JSON(code, map[string]bool{"ok": true})
}

func main() {
	h := server.Default()
	h.GET("/status", Status)
	h.Spin()
}
`
	eps := deprecProps(t, "go", "main.go", src)
	e := mustEndpoint(t, eps, "GET /status")
	if got := e.Properties["response_codes"]; got != "" {
		t.Fatalf("response_codes=%q want absent (dynamic status var, no literal)", got)
	}
}

// ---------------------------------------------------------------------------
// Async siblings: sanic / starlette / quart / litestar (#3913)
// ---------------------------------------------------------------------------

// Sanic: `return json(..., status=NNN)` literals in the handler body, with the
// route decorator sitting directly above the handler (Flask-shaped).
func TestResponseCodes_Sanic_JsonStatusLiterals(t *testing.T) {
	src := `
from sanic import Sanic, json
app = Sanic("x")

@app.get("/items")
async def list_items(request):
    if request.args.get("bad"):
        return json({"err": "bad"}, status=400)
    return json({"items": []}, status=200)
`
	eps := deprecProps(t, "python", "app/api.py", src)
	e := mustEndpoint(t, eps, "GET /items")
	if got := e.Properties["response_codes"]; got != "200,400" {
		t.Fatalf("response_codes=%q want 200,400 (props: %v)", got, e.Properties)
	}
	if got := e.Properties["success_code"]; got != "200" {
		t.Fatalf("success_code=%q want 200", got)
	}
}

// Starlette: the handler is a SEPARATE function referenced positionally in
// `Route("/path", handler, methods=[...])`. The synthesiser must resolve the
// positional handler so its body (with JSONResponse status_code= literals) is
// in scope — the route-line fallback would miss them entirely.
func TestResponseCodes_Starlette_PositionalRouteHandler(t *testing.T) {
	src := `
from starlette.applications import Starlette
from starlette.responses import JSONResponse
from starlette.routing import Route

async def create(request):
    data = await request.json()
    if not data:
        return JSONResponse({"err": "bad"}, status_code=400)
    return JSONResponse(data, status_code=201)

app = Starlette(routes=[Route("/items", create, methods=["POST"])])
`
	eps := deprecProps(t, "python", "app/main.py", src)
	e := mustEndpoint(t, eps, "POST /items")
	if got := e.Properties["response_codes"]; got != "201,400" {
		t.Fatalf("response_codes=%q want 201,400 (props: %v)", got, e.Properties)
	}
	if got := e.Properties["success_code"]; got != "201" {
		t.Fatalf("success_code=%q want 201", got)
	}
}

// Starlette: the `endpoint=` kwarg form must also resolve the handler.
func TestResponseCodes_Starlette_EndpointKwarg(t *testing.T) {
	src := `
from starlette.responses import JSONResponse
from starlette.routing import Route

async def show(request):
    return JSONResponse({"ok": True}, status_code=200)

routes = [Route("/items/{id}", endpoint=show, methods=["GET"])]
`
	eps := deprecProps(t, "python", "app/r.py", src)
	e := mustEndpoint(t, eps, "GET /items/{id}")
	if got := e.Properties["response_codes"]; got != "200" {
		t.Fatalf("response_codes=%q want 200 (props: %v)", got, e.Properties)
	}
}

// Quart: the Flask-shaped tuple-return status idiom `return jsonify(...), 201`.
func TestResponseCodes_Quart_TupleReturnStatus(t *testing.T) {
	src := `
from quart import Quart, jsonify, request
app = Quart(__name__)

@app.route("/things", methods=["POST"])
async def make_thing():
    if not await request.get_json():
        return jsonify({"err": "bad"}), 400
    return jsonify({"ok": True}), 201
`
	eps := deprecProps(t, "python", "app/q.py", src)
	e := mustEndpoint(t, eps, "POST /things")
	if got := e.Properties["response_codes"]; got != "201,400" {
		t.Fatalf("response_codes=%q want 201,400 (props: %v)", got, e.Properties)
	}
	if got := e.Properties["success_code"]; got != "201" {
		t.Fatalf("success_code=%q want 201", got)
	}
}

// Litestar: the `@get(status_code=200)` / `@post(status_code=201)` decorator.
func TestResponseCodes_Litestar_DecoratorStatus(t *testing.T) {
	src := `
from litestar import get, post

@get("/health", status_code=200)
async def health() -> dict:
    return {"ok": True}

@post("/users", status_code=201)
async def create_user(data: dict) -> dict:
    return data
`
	eps := deprecProps(t, "python", "app/ls.py", src)
	h := mustEndpoint(t, eps, "GET /health")
	if got := h.Properties["response_codes"]; got != "200" {
		t.Fatalf("health response_codes=%q want 200", got)
	}
	c := mustEndpoint(t, eps, "POST /users")
	if got := c.Properties["response_codes"]; got != "201" {
		t.Fatalf("create response_codes=%q want 201", got)
	}
}

// Negative (honest-partial): a sanic handler whose status is a dynamic variable
// (`status=code`) resolves to NO literal → response_codes absent.
func TestResponseCodes_Sanic_DynamicStatusAbsent(t *testing.T) {
	src := `
from sanic import Sanic, json
app = Sanic("x")

@app.get("/dyn")
async def dyn(request):
    code = compute()
    return json({"x": 1}, status=code)
`
	eps := deprecProps(t, "python", "app/dyn.py", src)
	e := mustEndpoint(t, eps, "GET /dyn")
	if got := e.Properties["response_codes"]; got != "" {
		t.Fatalf("response_codes=%q want absent (dynamic status var)", got)
	}
}

// Negative: `return foo(a, 200)` is an int ARGUMENT inside a call, not a tuple
// status — it must NOT be read as a response code (#3913 paren-balance guard).
func TestResponseCodes_Quart_IntArgNotTupleStatus(t *testing.T) {
	src := `
from quart import Quart
app = Quart(__name__)

@app.route("/calc", methods=["GET"])
async def calc():
    return compute(value, 200)
`
	eps := deprecProps(t, "python", "app/c.py", src)
	e := mustEndpoint(t, eps, "GET /calc")
	if got := e.Properties["response_codes"]; got != "" {
		t.Fatalf("response_codes=%q want absent (200 is a call arg, not a tuple status)", got)
	}
}

// Quart tuple status with trailing headers: `return body, 201, {...}`.
func TestResponseCodes_Quart_TupleStatusWithHeaders(t *testing.T) {
	src := `
from quart import Quart, jsonify
app = Quart(__name__)

@app.route("/h", methods=["POST"])
async def h():
    return jsonify({"ok": True}), 201, {"X-Trace": "1"}
`
	eps := deprecProps(t, "python", "app/h.py", src)
	e := mustEndpoint(t, eps, "POST /h")
	if got := e.Properties["response_codes"]; got != "201" {
		t.Fatalf("response_codes=%q want 201 (tuple status w/ headers)", got)
	}
}
