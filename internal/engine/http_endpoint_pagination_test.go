package engine

import "testing"

// pagProps reuses the deprecation/version test harness (deprecProps) which runs
// the full detection pipeline and keys synthetic http_endpoint_definition
// entities by "<VERB> <path>". The pagination pass runs in the same synthesis
// tail, so the stamped properties are present on the returned entities.

// ---------------------------------------------------------------------------
// DRF pagination_class (Python)
// ---------------------------------------------------------------------------

func TestPagination_DRFCursorClass(t *testing.T) {
	src := `
from rest_framework import generics
from rest_framework.pagination import CursorPagination

class OrderList(generics.ListAPIView):
    pagination_class = CursorPagination

    @app.get("/orders")
    def list(self):
        return []
`
	eps := deprecProps(t, "python", "app/views.py", src)
	e := mustEndpoint(t, eps, "GET /orders")
	if e.Properties["paginated"] != "true" {
		t.Fatalf("paginated=%q want true (props: %v)", e.Properties["paginated"], e.Properties)
	}
	if e.Properties["pagination_style"] != "cursor" {
		t.Fatalf("pagination_style=%q want cursor", e.Properties["pagination_style"])
	}
	if e.Properties["pagination_params"] != "cursor" {
		t.Fatalf("pagination_params=%q want cursor", e.Properties["pagination_params"])
	}
}

func TestPagination_DRFLimitOffsetClass(t *testing.T) {
	src := `
from rest_framework import generics
from rest_framework.pagination import LimitOffsetPagination

class ItemList(generics.ListAPIView):
    pagination_class = LimitOffsetPagination

    @app.get("/items")
    def list(self):
        return []
`
	eps := deprecProps(t, "python", "app/views.py", src)
	e := mustEndpoint(t, eps, "GET /items")
	if e.Properties["paginated"] != "true" {
		t.Fatalf("paginated=%q want true", e.Properties["paginated"])
	}
	if e.Properties["pagination_style"] != "offset" {
		t.Fatalf("pagination_style=%q want offset", e.Properties["pagination_style"])
	}
	if got := e.Properties["pagination_params"]; got != "limit,offset" {
		t.Fatalf("pagination_params=%q want limit,offset", got)
	}
}

func TestPagination_DRFDefaultSetting(t *testing.T) {
	// settings-level DEFAULT_PAGINATION_CLASS applies to endpoints with no
	// closer signal.
	src := `
from fastapi import FastAPI
app = FastAPI()

REST_FRAMEWORK = {
    "DEFAULT_PAGINATION_CLASS": "rest_framework.pagination.PageNumberPagination",
    "PAGE_SIZE": 20,
}

@app.get("/widgets")
def widgets():
    return []
`
	eps := deprecProps(t, "python", "app/settings_and_views.py", src)
	e := mustEndpoint(t, eps, "GET /widgets")
	if e.Properties["paginated"] != "true" {
		t.Fatalf("paginated=%q want true (props: %v)", e.Properties["paginated"], e.Properties)
	}
	if e.Properties["pagination_style"] != "page" {
		t.Fatalf("pagination_style=%q want page", e.Properties["pagination_style"])
	}
}

// ---------------------------------------------------------------------------
// Spring Pageable (Java)
// ---------------------------------------------------------------------------

func TestPagination_SpringPageable(t *testing.T) {
	src := `
package com.example;

import org.springframework.data.domain.Pageable;
import org.springframework.web.bind.annotation.*;

@RestController
@RequestMapping("/api/v1")
public class OrderController {

    @GetMapping("/orders")
    public List<Order> list(Pageable pageable) {
        return service.findAll(pageable);
    }
}
`
	eps := deprecProps(t, "java", "src/OrderController.java", src)
	e := mustEndpoint(t, eps, "GET /api/v1/orders")
	if e.Properties["paginated"] != "true" {
		t.Fatalf("paginated=%q want true (props: %v)", e.Properties["paginated"], e.Properties)
	}
	if e.Properties["pagination_style"] != "page" {
		t.Fatalf("pagination_style=%q want page", e.Properties["pagination_style"])
	}
}

// ---------------------------------------------------------------------------
// Express req.query limit+offset (JS)
// ---------------------------------------------------------------------------

func TestPagination_ExpressLimitOffset(t *testing.T) {
	src := `
const express = require('express');
const app = express();

app.get('/items', function getItems(req, res) {
  const limit = req.query.limit;
  const offset = req.query.offset;
  res.json([]);
});
`
	eps := deprecProps(t, "javascript", "src/routes.js", src)
	e := mustEndpoint(t, eps, "GET /items")
	if e.Properties["paginated"] != "true" {
		t.Fatalf("paginated=%q want true (props: %v)", e.Properties["paginated"], e.Properties)
	}
	if e.Properties["pagination_style"] != "offset" {
		t.Fatalf("pagination_style=%q want offset", e.Properties["pagination_style"])
	}
	if got := e.Properties["pagination_params"]; got != "limit,offset" {
		t.Fatalf("pagination_params=%q want limit,offset", got)
	}
}

func TestPagination_ExpressCursor(t *testing.T) {
	src := `
const express = require('express');
const app = express();

app.get('/feed', (req, res) => {
  const cursor = req.query.cursor;
  res.json([]);
});
`
	eps := deprecProps(t, "javascript", "src/routes.js", src)
	e := mustEndpoint(t, eps, "GET /feed")
	if e.Properties["paginated"] != "true" {
		t.Fatalf("paginated=%q want true", e.Properties["paginated"])
	}
	if e.Properties["pagination_style"] != "cursor" {
		t.Fatalf("pagination_style=%q want cursor", e.Properties["pagination_style"])
	}
	if e.Properties["pagination_params"] != "cursor" {
		t.Fatalf("pagination_params=%q want cursor", e.Properties["pagination_params"])
	}
}

func TestPagination_PrismaCursor(t *testing.T) {
	src := `
const express = require('express');
const app = express();

app.get('/posts', async (req, res) => {
  const posts = await prisma.post.findMany({ take: 10, cursor: { id: lastId } });
  res.json(posts);
});
`
	eps := deprecProps(t, "javascript", "src/posts.js", src)
	e := mustEndpoint(t, eps, "GET /posts")
	if e.Properties["paginated"] != "true" {
		t.Fatalf("paginated=%q want true (props: %v)", e.Properties["paginated"], e.Properties)
	}
	if e.Properties["pagination_style"] != "cursor" {
		t.Fatalf("pagination_style=%q want cursor", e.Properties["pagination_style"])
	}
}

func TestPagination_SequelizeLimitOffset(t *testing.T) {
	src := `
const express = require('express');
const app = express();

app.get('/users', async (req, res) => {
  const users = await User.findAll({ limit: 25, offset: 50 });
  res.json(users);
});
`
	eps := deprecProps(t, "javascript", "src/users.js", src)
	e := mustEndpoint(t, eps, "GET /users")
	if e.Properties["paginated"] != "true" {
		t.Fatalf("paginated=%q want true (props: %v)", e.Properties["paginated"], e.Properties)
	}
	if e.Properties["pagination_style"] != "offset" {
		t.Fatalf("pagination_style=%q want offset", e.Properties["pagination_style"])
	}
}

// ---------------------------------------------------------------------------
// FastAPI skip+limit Query params (Python)
// ---------------------------------------------------------------------------

func TestPagination_FastAPISkipLimit(t *testing.T) {
	src := `
from fastapi import FastAPI, Query
app = FastAPI()

@app.get("/products")
def products(skip: int = Query(0), limit: int = Query(50)):
    return []
`
	eps := deprecProps(t, "python", "app/main.py", src)
	e := mustEndpoint(t, eps, "GET /products")
	if e.Properties["paginated"] != "true" {
		t.Fatalf("paginated=%q want true (props: %v)", e.Properties["paginated"], e.Properties)
	}
	if e.Properties["pagination_style"] != "offset" {
		t.Fatalf("pagination_style=%q want offset", e.Properties["pagination_style"])
	}
	if got := e.Properties["pagination_params"]; got != "limit,skip" {
		t.Fatalf("pagination_params=%q want limit,skip", got)
	}
}

// ---------------------------------------------------------------------------
// HONEST-PARTIAL negatives
// ---------------------------------------------------------------------------

func TestPagination_LoneLimitNotPaginated(t *testing.T) {
	// A `limit` used as a business cap with no offset/page/cursor companion is
	// ambiguous and must NOT be stamped.
	src := `
const express = require('express');
const app = express();

app.get('/throttle', (req, res) => {
  const limit = req.query.limit; // rate cap, not pagination
  res.json({ limit });
});
`
	eps := deprecProps(t, "javascript", "src/throttle.js", src)
	e := mustEndpoint(t, eps, "GET /throttle")
	if _, ok := e.Properties["paginated"]; ok {
		t.Fatalf("lone limit fabricated pagination, want absent (props: %v)", e.Properties)
	}
}

func TestPagination_NonListEndpointUnaffected(t *testing.T) {
	// A create endpoint reading no pagination params is untouched.
	src := `
const express = require('express');
const app = express();

app.post('/orders', (req, res) => {
  res.status(201).json({});
});
`
	eps := deprecProps(t, "javascript", "src/orders.js", src)
	e := mustEndpoint(t, eps, "POST /orders")
	if _, ok := e.Properties["paginated"]; ok {
		t.Fatalf("non-list endpoint stamped paginated, want absent (props: %v)", e.Properties)
	}
}

// ---------------------------------------------------------------------------
// Go — gin / echo / chi / net-http query-param reads (#3920)
//
// Route registration and handler are separate funcs; the resolver locates the
// handler via source_handler and scans its body for query-param reads. Assert
// the SPECIFIC style + params on the SPECIFIC endpoint.
// ---------------------------------------------------------------------------

func TestPagination_Go_Gin_LimitOffset(t *testing.T) {
	src := `
package main

import "github.com/gin-gonic/gin"

func ListUsers(c *gin.Context) {
	limit := c.DefaultQuery("limit", "20")
	offset := c.Query("offset")
	_ = limit
	_ = offset
	c.JSON(200, gin.H{})
}

func reg() {
	r := gin.Default()
	r.GET("/users", ListUsers)
}
`
	eps := deprecProps(t, "go", "main.go", src)
	e := mustEndpoint(t, eps, "GET /users")
	if got := e.Properties["paginated"]; got != "true" {
		t.Fatalf("paginated=%q want true (props: %v)", got, e.Properties)
	}
	if got := e.Properties["pagination_style"]; got != "offset" {
		t.Fatalf("pagination_style=%q want offset", got)
	}
	if got := e.Properties["pagination_params"]; got != "limit,offset" {
		t.Fatalf("pagination_params=%q want limit,offset", got)
	}
}

func TestPagination_Go_Echo_PageParam(t *testing.T) {
	src := `
package main

import "github.com/labstack/echo/v4"

func ListPosts(c echo.Context) error {
	page := c.QueryParam("page")
	size := c.QueryParam("per_page")
	_ = page
	_ = size
	return c.JSON(200, nil)
}

func reg(e *echo.Echo) {
	e.GET("/posts", ListPosts)
}
`
	eps := deprecProps(t, "go", "main.go", src)
	e := mustEndpoint(t, eps, "GET /posts")
	if got := e.Properties["pagination_style"]; got != "page" {
		t.Fatalf("pagination_style=%q want page (props: %v)", got, e.Properties)
	}
	if got := e.Properties["pagination_params"]; got != "page,per_page" {
		t.Fatalf("pagination_params=%q want page,per_page", got)
	}
}

func TestPagination_Go_Chi_CursorQuery(t *testing.T) {
	src := `
package main

import (
	"net/http"
	"github.com/go-chi/chi/v5"
)

func ListEvents(w http.ResponseWriter, r *http.Request) {
	cursor := r.URL.Query().Get("cursor")
	limit := r.URL.Query().Get("limit")
	_ = cursor
	_ = limit
	w.WriteHeader(http.StatusOK)
}

func routes() {
	r := chi.NewRouter()
	r.GET("/events", ListEvents)
}
`
	eps := deprecProps(t, "go", "main.go", src)
	e := mustEndpoint(t, eps, "GET /events")
	if got := e.Properties["pagination_style"]; got != "cursor" {
		t.Fatalf("pagination_style=%q want cursor (props: %v)", got, e.Properties)
	}
	if got := e.Properties["pagination_params"]; got != "cursor,limit" {
		t.Fatalf("pagination_params=%q want cursor,limit", got)
	}
}

func TestPagination_Go_NetHTTP_PageQuery(t *testing.T) {
	src := `
package main

import "net/http"

func listThings(w http.ResponseWriter, r *http.Request) {
	page := r.URL.Query().Get("page")
	_ = page
	w.WriteHeader(http.StatusOK)
}

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /things", listThings)
	http.ListenAndServe(":8080", mux)
}
`
	eps := deprecProps(t, "go", "main.go", src)
	e := mustEndpoint(t, eps, "GET /things")
	if got := e.Properties["pagination_style"]; got != "page" {
		t.Fatalf("pagination_style=%q want page (props: %v)", got, e.Properties)
	}
	if got := e.Properties["pagination_params"]; got != "page" {
		t.Fatalf("pagination_params=%q want page", got)
	}
}

// Negative: a lone limit query read is ambiguous (could be a business cap) and
// is NOT stamped as pagination. (Honest-partial.)
func TestPagination_Go_LoneLimitNotPaginated(t *testing.T) {
	src := `
package main

import "github.com/gin-gonic/gin"

func search(c *gin.Context) {
	limit := c.Query("limit")
	_ = limit
	c.JSON(200, gin.H{})
}

func reg() {
	r := gin.Default()
	r.GET("/search", search)
}
`
	eps := deprecProps(t, "go", "main.go", src)
	e := mustEndpoint(t, eps, "GET /search")
	if got := e.Properties["paginated"]; got != "" {
		t.Fatalf("paginated=%q want absent (lone limit is ambiguous)", got)
	}
}

// Negative: a handler that reads no pagination params is not stamped.
func TestPagination_Go_NoParamsNotPaginated(t *testing.T) {
	src := `
package main

import "github.com/gin-gonic/gin"

func getUser(c *gin.Context) {
	id := c.Param("id")
	_ = id
	c.JSON(200, gin.H{})
}

func reg() {
	r := gin.Default()
	r.GET("/users/:id", getUser)
}
`
	eps := deprecProps(t, "go", "main.go", src)
	e := mustEndpoint(t, eps, "GET /users/{id}")
	if got := e.Properties["paginated"]; got != "" {
		t.Fatalf("paginated=%q want absent (no pagination params)", got)
	}
}
