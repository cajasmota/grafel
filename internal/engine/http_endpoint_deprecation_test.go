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
