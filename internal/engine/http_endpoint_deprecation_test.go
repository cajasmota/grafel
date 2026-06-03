package engine

import (
	"context"
	"testing"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

// deprecProps runs the full detection pipeline on a fixture and returns the
// synthetic http_endpoint_definition entities keyed by "<VERB> <path>". It is
// the deprecation/version analog of authProps.
func deprecProps(t *testing.T, language, path, content string) map[string]types.EntityRecord {
	t.Helper()
	rules, err := LoadAllRules()
	if err != nil {
		t.Fatalf("LoadAllRules: %v", err)
	}
	det := New(rules)
	res, err := det.Detect(context.Background(), extractor.FileInput{
		Path:     path,
		Content:  []byte(content),
		Language: language,
	})
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	out := map[string]types.EntityRecord{}
	for _, e := range res.Entities {
		if e.Kind != httpEndpointDefinitionKind {
			continue
		}
		key := e.Properties["verb"] + " " + e.Properties["path"]
		out[key] = e
	}
	return out
}

func mustEndpoint(t *testing.T, eps map[string]types.EntityRecord, key string) types.EntityRecord {
	t.Helper()
	e, ok := eps[key]
	if !ok {
		keys := make([]string, 0, len(eps))
		for k := range eps {
			keys = append(keys, k)
		}
		t.Fatalf("endpoint %q not synthesised (got: %v)", key, keys)
	}
	return e
}

// ---------------------------------------------------------------------------
// api_version (path-derived)
// ---------------------------------------------------------------------------

func TestAPIVersion_PathV2(t *testing.T) {
	// Express route under /api/v2 → api_version=2 on the endpoint id.
	src := `
const express = require('express');
const app = express();
app.get('/api/v2/orders', (req, res) => res.json([]));
`
	eps := deprecProps(t, "javascript", "src/routes.js", src)
	e := mustEndpoint(t, eps, "GET /api/v2/orders")
	if got := e.Properties["api_version"]; got != "2" {
		t.Fatalf("api_version=%q, want 2 (props: %v)", got, e.Properties)
	}
}

func TestAPIVersion_PathV1Bare(t *testing.T) {
	src := `
const express = require('express');
const app = express();
app.get('/v1/users', (req, res) => res.json([]));
`
	eps := deprecProps(t, "javascript", "src/routes.js", src)
	e := mustEndpoint(t, eps, "GET /v1/users")
	if got := e.Properties["api_version"]; got != "1" {
		t.Fatalf("api_version=%q, want 1", got)
	}
}

// Negative: `/apiv2something` is NOT a version segment — no api_version.
func TestAPIVersion_NoFalseSegment(t *testing.T) {
	src := `
const express = require('express');
const app = express();
app.get('/apiv2something/x', (req, res) => res.json([]));
app.get('/health', (req, res) => res.json({}));
`
	eps := deprecProps(t, "javascript", "src/routes.js", src)
	e := mustEndpoint(t, eps, "GET /apiv2something/x")
	if got, ok := e.Properties["api_version"]; ok {
		t.Fatalf("api_version=%q stamped for non-version path, want absent", got)
	}
	h := mustEndpoint(t, eps, "GET /health")
	if _, ok := h.Properties["api_version"]; ok {
		t.Fatalf("api_version stamped for /health, want absent")
	}
}

// ---------------------------------------------------------------------------
// deprecation — JS/TS JSDoc @deprecated
// ---------------------------------------------------------------------------

func TestDeprecation_JSDocOnRouteHandler(t *testing.T) {
	src := `
const express = require('express');
const app = express();

/**
 * @deprecated since v2 use /users/profile instead
 */
app.get('/users', (req, res) => res.json([]));

app.get('/posts', (req, res) => res.json([]));
`
	eps := deprecProps(t, "javascript", "src/routes.js", src)
	dep := mustEndpoint(t, eps, "GET /users")
	if dep.Properties["deprecated"] != "true" {
		t.Fatalf("GET /users deprecated=%q, want true (props: %v)", dep.Properties["deprecated"], dep.Properties)
	}
	if got := dep.Properties["deprecated_since"]; got != "v2" {
		t.Errorf("deprecated_since=%q, want v2", got)
	}
	if got := dep.Properties["deprecated_replacement"]; got != "/users/profile" {
		t.Errorf("deprecated_replacement=%q, want /users/profile", got)
	}
	// Negative: the sibling route carries no deprecation marker → absent.
	live := mustEndpoint(t, eps, "GET /posts")
	if _, ok := live.Properties["deprecated"]; ok {
		t.Fatalf("GET /posts deprecated fabricated, want absent (props: %v)", live.Properties)
	}
}

// ---------------------------------------------------------------------------
// deprecation — Spring @Deprecated @GetMapping
// ---------------------------------------------------------------------------

func TestDeprecation_SpringDeprecatedMapping(t *testing.T) {
	src := `package com.example.api;

import org.springframework.web.bind.annotation.*;

@RestController
@RequestMapping("/api/v1")
public class OrderController {

    @Deprecated
    @GetMapping("/old")
    public String old() { return "x"; }

    @GetMapping("/new")
    public String current() { return "y"; }
}
`
	eps := deprecProps(t, "java", "src/OrderController.java", src)
	dep := mustEndpoint(t, eps, "GET /api/v1/old")
	if dep.Properties["deprecated"] != "true" {
		t.Fatalf("GET /api/v1/old deprecated=%q, want true (props: %v)", dep.Properties["deprecated"], dep.Properties)
	}
	// /api/v1 prefix also yields api_version=1 (path-derived).
	if got := dep.Properties["api_version"]; got != "1" {
		t.Errorf("api_version=%q, want 1", got)
	}
	live := mustEndpoint(t, eps, "GET /api/v1/new")
	if _, ok := live.Properties["deprecated"]; ok {
		t.Fatalf("GET /api/v1/new deprecated fabricated, want absent")
	}
}

// Negative: a @Deprecated method that is NOT a route handler must not leak
// onto an unrelated endpoint.
func TestDeprecation_NonRouteDeprecatedDoesNotLeak(t *testing.T) {
	src := `package com.example.api;

import org.springframework.web.bind.annotation.*;

@RestController
@RequestMapping("/api/v1")
public class OrderController {

    @Deprecated
    private String legacyHelper() { return "x"; }

    @GetMapping("/orders")
    public String orders() { return "y"; }
}
`
	eps := deprecProps(t, "java", "src/OrderController.java", src)
	e := mustEndpoint(t, eps, "GET /api/v1/orders")
	if _, ok := e.Properties["deprecated"]; ok {
		t.Fatalf("non-route @Deprecated leaked onto GET /api/v1/orders (props: %v)", e.Properties)
	}
}

// ---------------------------------------------------------------------------
// deprecation — Python DRF deprecated=True / @deprecated decorator
// ---------------------------------------------------------------------------

func TestDeprecation_PythonDRFExtendSchema(t *testing.T) {
	// drf-spectacular @extend_schema(deprecated=True) on a FastAPI-shaped route
	// handler. (FastAPI synthesis gives us a clean endpoint id to assert on.)
	src := `
from fastapi import FastAPI
from drf_spectacular.utils import extend_schema

app = FastAPI()

@extend_schema(deprecated=True)
@app.get("/legacy")
def legacy():
    return {}

@app.get("/fresh")
def fresh():
    return {}
`
	eps := deprecProps(t, "python", "app/main.py", src)
	dep := mustEndpoint(t, eps, "GET /legacy")
	if dep.Properties["deprecated"] != "true" {
		t.Fatalf("GET /legacy deprecated=%q, want true (props: %v)", dep.Properties["deprecated"], dep.Properties)
	}
	live := mustEndpoint(t, eps, "GET /fresh")
	if _, ok := live.Properties["deprecated"]; ok {
		t.Fatalf("GET /fresh deprecated fabricated, want absent")
	}
}

func TestDeprecation_PythonDecorator(t *testing.T) {
	src := `
from fastapi import FastAPI
from typing_extensions import deprecated

app = FastAPI()

@deprecated("since 2.0 use /reports/v2 instead")
@app.get("/reports")
def reports():
    return {}
`
	eps := deprecProps(t, "python", "app/main.py", src)
	dep := mustEndpoint(t, eps, "GET /reports")
	if dep.Properties["deprecated"] != "true" {
		t.Fatalf("GET /reports deprecated=%q, want true", dep.Properties["deprecated"])
	}
	if got := dep.Properties["deprecated_since"]; got != "2.0" {
		t.Errorf("deprecated_since=%q, want 2.0", got)
	}
	if got := dep.Properties["deprecated_replacement"]; got != "/reports/v2" {
		t.Errorf("deprecated_replacement=%q, want /reports/v2", got)
	}
}

// ---------------------------------------------------------------------------
// deprecation — cross-language response-header signal
// ---------------------------------------------------------------------------

func TestDeprecation_SunsetResponseHeader(t *testing.T) {
	src := `
const express = require('express');
const app = express();

app.get('/billing', (req, res) => {
  res.set('Sunset', 'Sat, 31 Dec 2025 23:59:59 GMT');
  res.json({});
});
`
	eps := deprecProps(t, "javascript", "src/routes.js", src)
	dep := mustEndpoint(t, eps, "GET /billing")
	if dep.Properties["deprecated"] != "true" {
		t.Fatalf("GET /billing deprecated=%q, want true (props: %v)", dep.Properties["deprecated"], dep.Properties)
	}
}

// ---------------------------------------------------------------------------
// deprecation — Go (#4094): // Deprecated: godoc + scoped Sunset header
// ---------------------------------------------------------------------------

func TestDeprecation_GoGodocOnGinHandler(t *testing.T) {
	// Gin route whose handler func carries a `// Deprecated:` godoc comment.
	// The marker lives above the FUNC (not the registration), and the endpoint
	// emits with StartLine=0 — so this exercises the source_handler-anchored
	// Go resolver, not the path/decorator fallback.
	src := `package handlers

import "github.com/gin-gonic/gin"

func RegisterRoutes(r *gin.Engine) {
	r.GET("/api/v2/users", listUsers)
	r.GET("/api/v2/posts", listPosts)
}

// Deprecated: use /api/v3/users instead, since v2.4
func listUsers(c *gin.Context) {
	c.JSON(200, nil)
}

func listPosts(c *gin.Context) {
	c.JSON(200, nil)
}
`
	eps := deprecProps(t, "go", "internal/handlers/users.go", src)
	dep := mustEndpoint(t, eps, "GET /api/v2/users")
	if dep.Properties["deprecated"] != "true" {
		t.Fatalf("GET /api/v2/users deprecated=%q, want true (props: %v)", dep.Properties["deprecated"], dep.Properties)
	}
	if got := dep.Properties["deprecation_source"]; got != "// Deprecated: godoc" {
		t.Errorf("deprecation_source=%q, want // Deprecated: godoc", got)
	}
	if got := dep.Properties["deprecated_since"]; got != "v2.4" {
		t.Errorf("deprecated_since=%q, want v2.4", got)
	}
	if got := dep.Properties["deprecated_replacement"]; got != "/api/v3/users" {
		t.Errorf("deprecated_replacement=%q, want /api/v3/users", got)
	}
	// Path-derived api_version (in the route literal).
	if got := dep.Properties["api_version"]; got != "2" {
		t.Errorf("api_version=%q, want 2", got)
	}
	// Negative: the sibling handler carries no marker → no deprecation.
	live := mustEndpoint(t, eps, "GET /api/v2/posts")
	if _, ok := live.Properties["deprecated"]; ok {
		t.Fatalf("GET /api/v2/posts deprecated fabricated, want absent (props: %v)", live.Properties)
	}
}

// A Sunset response header set inside ONE Go handler must credit only that
// endpoint — it must not leak to a sibling route registered next to it.
func TestDeprecation_GoSunsetHeaderScopedToHandler(t *testing.T) {
	src := `package handlers

import "github.com/labstack/echo/v4"

func Register(e *echo.Echo) {
	e.GET("/billing", billing)
	e.GET("/invoices", invoices)
}

func billing(c echo.Context) error {
	c.Response().Header().Set("Sunset", "Sat, 31 Dec 2025 23:59:59 GMT")
	return c.JSON(200, nil)
}

func invoices(c echo.Context) error {
	return c.JSON(200, nil)
}
`
	eps := deprecProps(t, "go", "internal/handlers/billing.go", src)
	dep := mustEndpoint(t, eps, "GET /billing")
	if dep.Properties["deprecated"] != "true" {
		t.Fatalf("GET /billing deprecated=%q, want true (props: %v)", dep.Properties["deprecated"], dep.Properties)
	}
	if got := dep.Properties["deprecation_source"]; got != "Sunset response header" {
		t.Errorf("deprecation_source=%q, want Sunset response header", got)
	}
	// The header is in billing's body, NOT invoices' — no leak.
	live := mustEndpoint(t, eps, "GET /invoices")
	if _, ok := live.Properties["deprecated"]; ok {
		t.Fatalf("GET /invoices deprecated leaked from sibling Sunset, want absent (props: %v)", live.Properties)
	}
}

// net/http stdlib (Go 1.22 method-prefix) handler with a // Deprecated: godoc.
func TestDeprecation_GoNetHTTPGodoc(t *testing.T) {
	src := `package main

import "net/http"

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/orders", listOrders)
	mux.HandleFunc("GET /v1/customers", listCustomers)
}

// Deprecated: superseded by the v2 orders service.
func listOrders(w http.ResponseWriter, r *http.Request) {}

func listCustomers(w http.ResponseWriter, r *http.Request) {}
`
	eps := deprecProps(t, "go", "cmd/server/main.go", src)
	dep := mustEndpoint(t, eps, "GET /v1/orders")
	if dep.Properties["deprecated"] != "true" {
		t.Fatalf("GET /v1/orders deprecated=%q, want true (props: %v)", dep.Properties["deprecated"], dep.Properties)
	}
	if got := dep.Properties["api_version"]; got != "1" {
		t.Errorf("api_version=%q, want 1", got)
	}
	live := mustEndpoint(t, eps, "GET /v1/customers")
	if _, ok := live.Properties["deprecated"]; ok {
		t.Fatalf("GET /v1/customers deprecated fabricated, want absent")
	}
}

// Negative: a non-deprecated, versionless Go route carries neither property.
func TestDeprecation_GoNoMarkersNoStamp(t *testing.T) {
	src := `package handlers

import "github.com/gin-gonic/gin"

func Register(r *gin.Engine) {
	r.GET("/health", health)
}

func health(c *gin.Context) {
	c.JSON(200, gin.H{"ok": true})
}
`
	eps := deprecProps(t, "go", "internal/handlers/health.go", src)
	e := mustEndpoint(t, eps, "GET /health")
	if _, ok := e.Properties["deprecated"]; ok {
		t.Fatalf("GET /health deprecated fabricated, want absent (props: %v)", e.Properties)
	}
	if _, ok := e.Properties["api_version"]; ok {
		t.Fatalf("GET /health api_version fabricated for versionless route, want absent")
	}
}

// Negative: a `// Deprecated:` godoc on a NON-handler func must not leak onto an
// unrelated endpoint whose own handler carries no marker.
func TestDeprecation_GoNonHandlerGodocDoesNotLeak(t *testing.T) {
	src := `package handlers

import "github.com/gin-gonic/gin"

func Register(r *gin.Engine) {
	r.GET("/api/v1/orders", listOrders)
}

// Deprecated: internal helper, do not use.
func legacyHelper() string { return "x" }

func listOrders(c *gin.Context) {
	c.JSON(200, nil)
}
`
	eps := deprecProps(t, "go", "internal/handlers/orders.go", src)
	e := mustEndpoint(t, eps, "GET /api/v1/orders")
	if _, ok := e.Properties["deprecated"]; ok {
		t.Fatalf("non-handler // Deprecated: leaked onto GET /api/v1/orders (props: %v)", e.Properties)
	}
}

func TestGoHandlerName(t *testing.T) {
	cases := []struct {
		ref  string
		want string
	}{
		{"Controller:listUsers", "listUsers"},
		{"Controller:h.Create", "Create"},
		{"listUsers", "listUsers"},
		{"", ""},
	}
	for _, c := range cases {
		e := &types.EntityRecord{Properties: map[string]string{"source_handler": c.ref}}
		if got := goHandlerName(e); got != c.want {
			t.Errorf("goHandlerName(%q)=%q, want %q", c.ref, got, c.want)
		}
	}
}

// ---------------------------------------------------------------------------
// unit-level: helpers
// ---------------------------------------------------------------------------

func TestAPIVersionFromPath(t *testing.T) {
	cases := []struct {
		path string
		want int
		ok   bool
	}{
		{"/api/v2/orders", 2, true},
		{"/v1/users", 1, true},
		{"/api/v3", 3, true},
		{"/apiv2something", 0, false},
		{"/health", 0, false},
		{"/api/v100/x", 0, false}, // out of range
	}
	for _, c := range cases {
		got, ok := apiVersionFromPath(c.path)
		if ok != c.ok || got != c.want {
			t.Errorf("apiVersionFromPath(%q)=(%d,%v), want (%d,%v)", c.path, got, ok, c.want, c.ok)
		}
	}
}

func TestParseDeprecationMessage(t *testing.T) {
	since, repl := parseDeprecationMessage("since v2.1 use /users/profile instead")
	if since != "v2.1" {
		t.Errorf("since=%q, want v2.1", since)
	}
	if repl != "/users/profile" {
		t.Errorf("replacement=%q, want /users/profile", repl)
	}
	// Honest-partial: no signals → empty, never fabricated.
	since, repl = parseDeprecationMessage("this endpoint is going away")
	if since != "" || repl != "" {
		t.Errorf("expected no since/replacement, got (%q,%q)", since, repl)
	}
}
