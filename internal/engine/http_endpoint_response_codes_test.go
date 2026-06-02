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
