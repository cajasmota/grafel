// Tests for the corpus-wide response-shape post-pass (#753).
//
// These tests cover the four real-world scenarios that the per-file
// pass cannot handle and that PR #744's empirical validation flagged
// on every backend fixture:
//
//  1. Django composed URLconf — handler is a view in views.py while the
//     route is registered in urls.py.
//  2. DRF ViewSet — list/create/etc. method on a class registered with
//     router.register(prefix, ViewSet); handler is a method on the
//     ViewSet class in viewsets.py.
//  3. Express imported controller — `router.get("/x", users.list)`
//     where `users` is imported from another module.
//  4. JAX-RS handler — class-level @Path with method-level @GET in a
//     Java file separate from any URL dispatcher.
//
// In every case the per-file pass produces empty response_keys because
// either source_handler is empty or the handler entity lives in
// another file. The corpus pass resolves the handler via the global
// (Kind, Name) index plus ROUTES_TO edge follow-through and reads the
// real source file.
package engine

import (
	"testing"

	"github.com/cajasmota/archigraph/internal/types"
)

// TestCorpusResponseShape_DjangoComposedViewsetCrossFile verifies that a
// composed Django route whose ViewSet lives in a separate file gets its
// response_keys populated via the cross-file (Kind, Name) handler index
// + ROUTES_TO edge.
func TestCorpusResponseShape_DjangoComposedViewsetCrossFile(t *testing.T) {
	viewsContent := []byte(`from rest_framework.response import Response
from rest_framework.viewsets import ModelViewSet

class UserViewSet(ModelViewSet):
    def list(self, request):
        return Response({"users": [], "total": 0})
    def create(self, request):
        return Response({"id": 1, "email": "a@b"}, status=201)
`)

	entities := []types.EntityRecord{
		// View entity (Python YAML rule emits this for the class).
		{
			Kind:       "View",
			Name:       "UserViewSet",
			SourceFile: "myapp/views.py",
			Language:   "python",
			Properties: map[string]string{"framework": "python"},
		},
		// Composed Django route, emitted from urls.py — handler lives
		// elsewhere so source_handler is empty per the production code
		// in synthesizeDjangoFromComposed.
		{
			Kind:       httpEndpointKind,
			Name:       "http:ANY:/api/users",
			SourceFile: "myapp/urls.py",
			Language:   "python",
			Properties: map[string]string{
				"framework":    "django",
				"verb":         "ANY",
				"path":         "/api/users",
				"pattern_type": "http_endpoint_synthesis",
			},
		},
	}
	// ROUTES_TO edge from the composed Route (Name=path without
	// leading slash) to the View. This is what django_routes.go emits.
	rels := []types.RelationshipRecord{
		{
			FromID: "Route:api/users",
			ToID:   "View:UserViewSet",
			Kind:   "ROUTES_TO",
		},
	}

	reader := func(p string) []byte {
		if p == "myapp/views.py" {
			return viewsContent
		}
		return nil
	}

	stats := ApplyResponseShapesCorpus(entities, rels, reader)
	if stats.HandlerResolved != 1 {
		t.Fatalf("HandlerResolved=%d want 1; stats=%+v", stats.HandlerResolved, stats)
	}
	if stats.ShapeExtracted != 1 {
		t.Fatalf("ShapeExtracted=%d want 1; stats=%+v", stats.ShapeExtracted, stats)
	}
	got := entities[1].Properties["response_keys"]
	if got == "" {
		t.Fatalf("expected response_keys populated; got empty. properties=%+v", entities[1].Properties)
	}
	// Union of list() + create() keys: id, email, total, users.
	want := []string{"id", "email", "total", "users"}
	for _, k := range want {
		if !csvContains(got, k) {
			t.Errorf("response_keys=%q missing %q", got, k)
		}
	}
}

// TestCorpusResponseShape_DRFViewSetMethodCrossFile covers the DRF
// action expander path: each emitted detail/action endpoint carries
// `drf_view_method = "ViewSet.method"` and the corpus pass should
// resolve View:ViewSet then look up the specific method.
func TestCorpusResponseShape_DRFViewSetMethodCrossFile(t *testing.T) {
	viewsContent := []byte(`class OrderViewSet(ModelViewSet):
    def retrieve(self, request, pk=None):
        return Response({"id": pk, "total": 100, "items": []})
`)
	entities := []types.EntityRecord{
		{Kind: "View", Name: "OrderViewSet", SourceFile: "shop/views.py", Language: "python"},
		{
			Kind:       httpEndpointKind,
			Name:       "http:GET:/api/orders/{pk}",
			SourceFile: "shop/urls.py",
			Language:   "python",
			Properties: map[string]string{
				"framework":       "django",
				"verb":            "GET",
				"path":            "/api/orders/{pk}",
				"pattern_type":    "drf_router_expanded",
				"drf_view_method": "OrderViewSet.retrieve",
			},
		},
	}
	reader := func(p string) []byte {
		if p == "shop/views.py" {
			return viewsContent
		}
		return nil
	}
	stats := ApplyResponseShapesCorpus(entities, nil, reader)
	if stats.ShapeExtracted != 1 {
		t.Fatalf("ShapeExtracted=%d want 1; %+v", stats.ShapeExtracted, stats)
	}
	got := entities[1].Properties["response_keys"]
	for _, k := range []string{"id", "items", "total"} {
		if !csvContains(got, k) {
			t.Errorf("response_keys=%q missing %q", got, k)
		}
	}
}

// TestCorpusResponseShape_ExpressImportedController checks that an
// Express synthetic with `source_handler = "Controller:<name>"` whose
// Controller entity lives in another file resolves cross-file.
func TestCorpusResponseShape_ExpressImportedController(t *testing.T) {
	controllerContent := []byte(`exports.listUsers = function listUsers(req, res) {
  res.json({ users: [], page: 1, total: 0 });
};
`)
	entities := []types.EntityRecord{
		{Kind: "Controller", Name: "listUsers", SourceFile: "src/controllers/users.js", Language: "javascript"},
		{
			Kind:       httpEndpointKind,
			Name:       "http:GET:/users",
			SourceFile: "src/routes.js",
			Language:   "javascript",
			Properties: map[string]string{
				"framework":      "express",
				"verb":           "GET",
				"path":           "/users",
				"pattern_type":   "http_endpoint_synthesis",
				"source_handler": "Controller:listUsers",
			},
		},
	}
	reader := func(p string) []byte {
		if p == "src/controllers/users.js" {
			return controllerContent
		}
		return nil
	}
	stats := ApplyResponseShapesCorpus(entities, nil, reader)
	if stats.ShapeExtracted != 1 {
		t.Fatalf("ShapeExtracted=%d want 1; %+v", stats.ShapeExtracted, stats)
	}
	got := entities[1].Properties["response_keys"]
	for _, k := range []string{"page", "total", "users"} {
		if !csvContains(got, k) {
			t.Errorf("response_keys=%q missing %q", got, k)
		}
	}
}

// TestCorpusResponseShape_NoHandlerFound increments NoHandlerFound for
// endpoints whose references don't resolve anywhere and leaves
// Properties untouched.
func TestCorpusResponseShape_NoHandlerFound(t *testing.T) {
	entities := []types.EntityRecord{
		{
			Kind:       httpEndpointKind,
			Name:       "http:GET:/orphan",
			SourceFile: "x.py",
			Language:   "python",
			Properties: map[string]string{
				"framework":      "django",
				"source_handler": "View:GhostViewSet",
			},
		},
	}
	stats := ApplyResponseShapesCorpus(entities, nil, func(string) []byte { return nil })
	if stats.NoHandlerFound != 1 {
		t.Fatalf("NoHandlerFound=%d want 1; %+v", stats.NoHandlerFound, stats)
	}
	if entities[0].Properties["response_keys"] != "" {
		t.Errorf("response_keys should remain empty on no-handler-found")
	}
}

// TestCorpusResponseShape_SkipsAlreadyPopulated ensures the corpus pass
// is a no-op for endpoints whose shape was already extracted by the
// per-file pass. This guarantees byte-stable output on Flask /
// JAX-RS / FastAPI fixtures where the same-file pass already wins.
func TestCorpusResponseShape_SkipsAlreadyPopulated(t *testing.T) {
	entities := []types.EntityRecord{
		{Kind: "Controller", Name: "h", SourceFile: "a.py", Language: "python"},
		{
			Kind:       httpEndpointKind,
			Name:       "http:GET:/x",
			SourceFile: "a.py",
			Properties: map[string]string{
				"framework":      "flask",
				"source_handler": "Controller:h",
				"response_keys":  "pre-populated",
			},
		},
	}
	stats := ApplyResponseShapesCorpus(entities, nil, func(string) []byte {
		return []byte(`def h(): return jsonify({"different": 1})`)
	})
	if stats.Endpoints != 0 {
		t.Fatalf("expected Endpoints=0 (already populated), got %d", stats.Endpoints)
	}
	if entities[1].Properties["response_keys"] != "pre-populated" {
		t.Errorf("pre-populated response_keys should be preserved")
	}
}

// TestCorpusResponseShape_JAXRSCrossClass verifies a Java handler in a
// separate class still gets resolved when the synthetic carries a
// source_handler reference whose entity lives in a different file.
func TestCorpusResponseShape_JAXRSCrossClass(t *testing.T) {
	javaContent := []byte(`package com.x;
public class UserResource {
    @GET
    @Path("/users")
    public UserDto getUsers() {
        return new UserDto();
    }
}
class UserDto {
    private String email;
    private long id;
}
`)
	entities := []types.EntityRecord{
		{Kind: "SCOPE.Operation", Name: "UserResource.getUsers", SourceFile: "src/UserResource.java", Language: "java"},
		{
			Kind:       httpEndpointKind,
			Name:       "http:GET:/users",
			SourceFile: "src/routes.java",
			Language:   "java",
			Properties: map[string]string{
				"framework":      "jaxrs",
				"source_handler": "SCOPE.Operation:UserResource.getUsers",
			},
		},
	}
	reader := func(p string) []byte {
		if p == "src/UserResource.java" {
			return javaContent
		}
		return nil
	}
	stats := ApplyResponseShapesCorpus(entities, nil, reader)
	if stats.HandlerResolved != 1 {
		t.Fatalf("HandlerResolved=%d want 1; %+v", stats.HandlerResolved, stats)
	}
	// JAX-RS DTO walk should produce response_schema/keys with email + id.
	if entities[1].Properties["response_schema"] == "" && entities[1].Properties["response_keys"] == "" {
		t.Fatalf("expected response_schema or response_keys populated; got %+v", entities[1].Properties)
	}
}

// TestCorpusResponseShape_GoDefinitionKindCrossFile is a regression for
// #1553: the corpus pass previously only matched the legacy http_endpoint
// kind, so post-#1217 http_endpoint_definition entities (e.g. a Go gin route
// whose handler + struct live in a sibling file) never had their response
// shapes resolved cross-file. This exercises the http_endpoint_definition
// path end-to-end.
func TestCorpusResponseShape_GoDefinitionKindCrossFile(t *testing.T) {
	handlerContent := []byte(`package internal

import "github.com/gin-gonic/gin"
import "net/http"

type Shipment struct {
    OrderID  string ` + "`json:\"order_id\"`" + `
    Carrier  string ` + "`json:\"carrier\"`" + `
    Status   string ` + "`json:\"status\"`" + `
}

func GetShipment(c *gin.Context) {
    c.JSON(http.StatusOK, Shipment{OrderID: "x", Status: "IN_TRANSIT"})
}
`)

	entities := []types.EntityRecord{
		{
			Kind:       "Function",
			Name:       "GetShipment",
			SourceFile: "shipping/internal/handlers.go",
			Language:   "go",
		},
		{
			Kind:       httpEndpointDefinitionKind,
			Name:       "http:GET:/shipments/{orderId}",
			SourceFile: "shipping/main.go",
			Language:   "go",
			Properties: map[string]string{
				"framework":      "gin",
				"verb":           "GET",
				"path":           "/shipments/{orderId}",
				"pattern_type":   "http_endpoint_synthesis",
				"source_handler": "Function:GetShipment",
			},
		},
	}

	reader := func(p string) []byte {
		if p == "shipping/internal/handlers.go" {
			return handlerContent
		}
		return nil
	}

	stats := ApplyResponseShapesCorpus(entities, nil, reader)
	if stats.Endpoints != 1 {
		t.Fatalf("Endpoints=%d want 1 (definition kind must be considered); stats=%+v", stats.Endpoints, stats)
	}
	if stats.ShapeExtracted != 1 {
		t.Fatalf("ShapeExtracted=%d want 1; stats=%+v", stats.ShapeExtracted, stats)
	}
	got := entities[1].Properties["response_keys"]
	for _, k := range []string{"order_id", "carrier", "status"} {
		if !csvContains(got, k) {
			t.Errorf("response_keys=%q missing %q", got, k)
		}
	}
}

// csvContains is a tiny helper that checks if a comma-joined string
// contains a value (used to keep test assertions readable).
func csvContains(csv, want string) bool {
	for _, part := range splitCSV(csv) {
		if part == want {
			return true
		}
	}
	return false
}

func splitCSV(s string) []string {
	if s == "" {
		return nil
	}
	out := []string{}
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == ',' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	out = append(out, s[start:])
	return out
}
