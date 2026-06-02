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
