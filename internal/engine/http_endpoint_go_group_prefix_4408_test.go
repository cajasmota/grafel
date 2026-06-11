package engine

import (
	"sort"
	"strings"
	"testing"
)

// #4408 — gin/echo `r.Group("/v1")` route-group prefixes must be resolved and
// prepended to every route registered on the group variable, including nested
// groups. Before this fix synthesizeGoRouters emitted the route's own path
// (`/items`) without the enclosing group prefix (`/v1/items`).
//
// These tests call synthesizeGoRouters directly with a capturing emit so they
// assert the exact (verb, canonical-path) pairs the synthesis layer produces,
// independent of the downstream merge/resolve pipeline.

func collectGoRoutes(src string) map[string]string {
	out := map[string]string{}
	emit := func(method, canonicalPath, framework, handlerKind, handlerName string) {
		out[method+" "+canonicalPath] = framework
	}
	synthesizeGoRouters(src, emit)
	return out
}

func assertRoute(t *testing.T, routes map[string]string, key string) {
	t.Helper()
	if _, ok := routes[key]; !ok {
		keys := make([]string, 0, len(routes))
		for k := range routes {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		t.Errorf("expected route %q, got: [%s]", key, strings.Join(keys, ", "))
	}
}

func assertNoRoute(t *testing.T, routes map[string]string, key string) {
	t.Helper()
	if _, ok := routes[key]; ok {
		t.Errorf("did not expect route %q, but it was emitted", key)
	}
}

// Fixture A: single group → route prefixed with the group path.
func TestGroupPrefix4408_SingleGroup(t *testing.T) {
	src := `package main

import "github.com/gin-gonic/gin"

func main() {
	r := gin.Default()
	v1 := r.Group("/api/v1")
	v1.GET("/users", listUsers)
	r.Run()
}
`
	routes := collectGoRoutes(src)
	assertRoute(t, routes, "GET /api/v1/users")
	assertNoRoute(t, routes, "GET /users")
}

// Fixture B: nested group → route prefixed with the composed parent+child path.
func TestGroupPrefix4408_NestedGroup(t *testing.T) {
	src := `package main

import "github.com/gin-gonic/gin"

func main() {
	r := gin.Default()
	v1 := r.Group("/api/v1")
	admin := v1.Group("/admin")
	admin.POST("/ban", banUser)
	r.Run()
}
`
	routes := collectGoRoutes(src)
	assertRoute(t, routes, "POST /api/v1/admin/ban")
	assertNoRoute(t, routes, "POST /ban")
	assertNoRoute(t, routes, "POST /admin/ban")
}

// Fixture C: group with extra middleware args → prefix still resolved.
func TestGroupPrefix4408_GroupWithMiddleware(t *testing.T) {
	src := `package main

import "github.com/gin-gonic/gin"

func authMW() gin.HandlerFunc { return func(c *gin.Context) {} }

func main() {
	r := gin.Default()
	p := r.Group("/p", authMW())
	p.GET("/x", getX)
	r.Run()
}
`
	routes := collectGoRoutes(src)
	assertRoute(t, routes, "GET /p/x")
	assertNoRoute(t, routes, "GET /x")
}

// Regression: a route registered directly on the root router (not a group)
// keeps its own path unchanged.
func TestGroupPrefix4408_RootRouteUnchanged(t *testing.T) {
	src := `package main

import "github.com/gin-gonic/gin"

func main() {
	r := gin.Default()
	r.GET("/health", healthCheck)
	v1 := r.Group("/v1")
	v1.GET("/users", listUsers)
	r.Run()
}
`
	routes := collectGoRoutes(src)
	assertRoute(t, routes, "GET /health")
	assertRoute(t, routes, "GET /v1/users")
}

// Echo groups use the same `.Group("/x")` spelling and must resolve too.
func TestGroupPrefix4408_EchoGroup(t *testing.T) {
	src := `package main

import "github.com/labstack/echo/v4"

func main() {
	e := echo.New()
	g := e.Group("/api")
	g.GET("/ping", pingHandler)
	e.Start(":1323")
}
`
	routes := collectGoRoutes(src)
	assertRoute(t, routes, "GET /api/ping")
	assertNoRoute(t, routes, "GET /ping")
}
