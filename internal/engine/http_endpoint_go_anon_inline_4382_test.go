package engine

import (
	"testing"
)

// #4382 — Go HTTP routes whose handler is an ANONYMOUS / INLINE function literal
// (`func(w http.ResponseWriter, r *http.Request) {...}`, `func(c *gin.Context)
// {...}`, `func(c echo.Context) error {...}`, …) must be captured as endpoints
// AND linked to a handler node — exactly like the JS fix #4324.
//
// Before this fix the Go producer synthesizers required the handler argument to
// be a bare/qualified IDENTIFIER (`[\w.]+`). An inline func literal did not match
// the registration regex at all, so the endpoint was DROPPED entirely (a strictly
// worse failure than JS, where the endpoint was emitted but left an island).
//
// The fix is handler-shape-agnostic: the Go synthesizers now also match func
// literals and signal refKind="InlineHandler" (empty refName). makeEmit then
// synthesizes a stable inline-handler Operation entity (Name derived purely from
// verb+canonical path → merge-stable) plus a file-scoped structural IMPLEMENTS
// bridge that the central resolver binds post-merge — the #4324 / #4319 mechanism
// reused for Go. This test runs the REAL extract+synthesis+merge+resolve pipeline.

// TestInline4382_NetHTTPInlineHandlers covers net/http stdlib inline func
// literals: http.HandleFunc, mux.HandleFunc (Go 1.22 method prefix), and
// http.Handle(http.HandlerFunc(func(...){...})).
func TestInline4382_NetHTTPInlineHandlers(t *testing.T) {
	src := `package main

import "net/http"

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	})
	http.HandleFunc("/legacy", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	http.ListenAndServe(":8080", mux)
}
`
	ents, rels := detectInline(t, "go", "cmd/server/main.go", src)
	assertInlineEndpointBridged(t, ents, rels, "GET", "/health", "nethttp")
	assertInlineEndpointBridged(t, ents, rels, "ANY", "/legacy", "nethttp")
}

// TestInline4382_GorillaMuxInlineHandlers covers gorilla/mux
// r.HandleFunc("/x", func(...){...}).Methods("GET").
func TestInline4382_GorillaMuxInlineHandlers(t *testing.T) {
	src := `package main

import (
	"net/http"

	"github.com/gorilla/mux"
)

func register(r *mux.Router) {
	r.HandleFunc("/widgets", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("[]"))
	}).Methods("GET")
}
`
	ents, rels := detectInline(t, "go", "internal/api/gorilla.go", src)
	assertInlineEndpointBridged(t, ents, rels, "GET", "/widgets", "gorilla")
}

// TestInline4382_GinInlineHandlers covers gin r.GET("/x", func(c *gin.Context){...})
// including a route group.
func TestInline4382_GinInlineHandlers(t *testing.T) {
	src := `package main

import "github.com/gin-gonic/gin"

func main() {
	r := gin.Default()
	r.GET("/ping", func(c *gin.Context) {
		c.JSON(200, gin.H{"pong": true})
	})
	v1 := r.Group("/v1")
	v1.POST("/items", func(c *gin.Context) {
		c.JSON(201, gin.H{"ok": true})
	})
	r.Run()
}
`
	ents, rels := detectInline(t, "go", "cmd/gin/main.go", src)
	assertInlineEndpointBridged(t, ents, rels, "GET", "/ping", "gin")
	// NOTE: the http_endpoint synthesis layer (synthesizeGoRouters) does not
	// resolve gin r.Group("/v1") prefixes — it emits the route's own path
	// ("/items"), for BOTH named and inline handlers alike. That group-prefix
	// gap is pre-existing and handler-shape-INDEPENDENT (see PR follow-up); this
	// #4382 assertion deliberately tracks the layer's actual (verb,path) so it
	// isolates the inline-handler regression it is here to guard.
	assertInlineEndpointBridged(t, ents, rels, "POST", "/items", "gin")
}

// TestInline4382_EchoInlineHandlers covers echo e.GET("/x", func(c echo.Context) error {...}).
func TestInline4382_EchoInlineHandlers(t *testing.T) {
	src := `package main

import (
	"net/http"

	"github.com/labstack/echo/v4"
)

func main() {
	e := echo.New()
	e.GET("/status", func(c echo.Context) error {
		return c.String(http.StatusOK, "ok")
	})
	e.Logger.Fatal(e.Start(":1323"))
}
`
	ents, rels := detectInline(t, "go", "cmd/echo/main.go", src)
	assertInlineEndpointBridged(t, ents, rels, "GET", "/status", "echo")
}

// TestInline4382_ChiInlineHandlers covers chi r.Get("/x", func(w, r){...}).
func TestInline4382_ChiInlineHandlers(t *testing.T) {
	src := `package main

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

func main() {
	r := chi.NewRouter()
	r.Get("/items", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("[]"))
	})
	http.ListenAndServe(":3000", r)
}
`
	ents, rels := detectInline(t, "go", "cmd/chi/main.go", src)
	assertInlineEndpointBridged(t, ents, rels, "GET", "/items", "chi")
}

// TestInline4382_NamedHandlerStillNamedBridge guards no regression: a Go NAMED
// handler reference must still take the named #4319 bridge and must NOT
// synthesize an inline-handler stand-in.
func TestInline4382_NamedHandlerStillNamedBridge(t *testing.T) {
	src := `package main

import "github.com/gin-gonic/gin"

func listUsers(c *gin.Context) { c.JSON(200, gin.H{}) }

func main() {
	r := gin.Default()
	r.GET("/users", listUsers)
	r.Run()
}
`
	ents, _ := detectInline(t, "go", "cmd/named/main.go", src)
	if endpointByVerbPath(ents, "GET", "/users") == nil {
		t.Fatal("named-handler endpoint missing")
	}
	if inlineHandlerEntity(ents, "GET", "/users") != nil {
		t.Error("named Go handler must NOT synthesize an inline-handler stand-in")
	}
}
