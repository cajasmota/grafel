package dashboard

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cajasmota/archigraph/internal/graph"
)

// ---------------------------------------------------------------------------
// Unit tests — helpers
// ---------------------------------------------------------------------------

func TestBuildOperationID(t *testing.T) {
	cases := []struct {
		method string
		path   string
		want   string
	}{
		{"GET", "/users", "getUsers"},
		{"POST", "/api/users", "postApiUsers"},
		{"DELETE", "/api/users/{id}", "deleteApiUsersById"},
		{"GET", "/", "get"},
		{"GET", "/health", "getHealth"},
	}
	for _, tc := range cases {
		got := buildOperationID(tc.method, tc.path)
		if got != tc.want {
			t.Errorf("buildOperationID(%q, %q) = %q, want %q", tc.method, tc.path, got, tc.want)
		}
	}
}

func TestHttpStatusText(t *testing.T) {
	if httpStatusText(200) != "OK" {
		t.Error("200 should map to OK")
	}
	if httpStatusText(404) != "Not Found" {
		t.Error("404 should map to Not Found")
	}
	if httpStatusText(999) != "HTTP 999" {
		t.Errorf("unknown code should return 'HTTP 999', got %q", httpStatusText(999))
	}
}

func TestOpenAPIPathItem_setOperation(t *testing.T) {
	item := &openAPIPathItem{}
	op := &openAPIOperation{OperationID: "getTest"}
	item.setOperation("GET", op)
	if item.Get != op {
		t.Error("setOperation GET should set Get field")
	}
	item.setOperation("POST", op)
	if item.Post != op {
		t.Error("setOperation POST should set Post field")
	}
	item.setOperation("unknown", op)
	// unknown method silently ignored — no panic
}

func TestCloneOperation(t *testing.T) {
	op := &openAPIOperation{OperationID: "original", Summary: "test"}
	cloned := cloneOperation(op)
	if cloned == op {
		t.Error("cloneOperation should return a different pointer")
	}
	if cloned.OperationID != op.OperationID {
		t.Error("cloned operation should have the same OperationID")
	}
	cloned.OperationID = "mutated"
	if op.OperationID != "original" {
		t.Error("mutating clone should not affect original")
	}
}

func TestCloneOperation_nil(t *testing.T) {
	if cloneOperation(nil) != nil {
		t.Error("cloneOperation(nil) should return nil")
	}
}

// ---------------------------------------------------------------------------
// HTTP handler tests
// ---------------------------------------------------------------------------

// openAPITestGroup wires a DashGroup with HTTP endpoint entities.
func openAPITestGroup(name string, entities []graph.Entity) *DashGroup {
	return &DashGroup{
		Name: name,
		Repos: map[string]*DashRepo{
			"repo1": {
				Slug: "repo1",
				Doc: &graph.Document{
					Entities: entities,
				},
			},
		},
	}
}

// newOpenAPITestServer creates a Server and injects a named group.
func newOpenAPITestServer(t *testing.T, groupName string, entities []graph.Entity) *Server {
	t.Helper()
	srv, err := NewServer(DefaultConfig(), newFakeStore())
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	grp := openAPITestGroup(groupName, entities)
	srv.graphs.mu.Lock()
	srv.graphs.entries[groupName] = &cacheEntry{group: grp, loadedAt: time.Now()}
	srv.graphs.mu.Unlock()
	return srv
}

// sampleEndpointEntities returns a set of graph.Entity records that look like
// real http_endpoint_definition extractions.
func sampleEndpointEntities() []graph.Entity {
	return []graph.Entity{
		{
			ID:   "ep1",
			Name: "GET /api/users",
			Kind: "http_endpoint_definition",
			Properties: map[string]string{
				"method":         "GET",
				"path":           "/api/users",
				"framework":      "gin",
				"owning_backend": "user-service",
				"status_codes":   "200,404",
				"response_keys":  "id,name,email",
			},
		},
		{
			ID:   "ep2",
			Name: "POST /api/users",
			Kind: "http_endpoint_definition",
			Properties: map[string]string{
				"method":         "POST",
				"path":           "/api/users",
				"framework":      "gin",
				"owning_backend": "user-service",
				"status_codes":   "201,400",
			},
		},
		{
			ID:   "ep3",
			Name: "GET /api/users/{id}",
			Kind: "http_endpoint_definition",
			Properties: map[string]string{
				"method":         "GET",
				"path":           "/api/users/{id}",
				"framework":      "gin",
				"owning_backend": "user-service",
				"status_codes":   "200,404",
			},
		},
		{
			// Should be excluded — call-site synthetic.
			ID:   "call1",
			Name: "fetch /api/users",
			Kind: "http_endpoint_call",
			Properties: map[string]string{
				"path":   "/api/users",
				"method": "GET",
			},
		},
	}
}

func TestHandleExportOpenAPI_groupNotFound(t *testing.T) {
	srv, _ := NewServer(DefaultConfig(), newFakeStore())
	req := httptest.NewRequest(http.MethodGet, "/api/export/nogroup/openapi", nil)
	req.SetPathValue("group", "nogroup")
	w := httptest.NewRecorder()
	srv.handleExportOpenAPI(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("want 404 for unknown group, got %d", w.Code)
	}
}

func TestHandleExportOpenAPI_missingGroup(t *testing.T) {
	srv, _ := NewServer(DefaultConfig(), newFakeStore())
	req := httptest.NewRequest(http.MethodGet, "/api/export//openapi", nil)
	req.SetPathValue("group", "")
	w := httptest.NewRecorder()
	srv.handleExportOpenAPI(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("want 400 for missing group, got %d", w.Code)
	}
}

func TestHandleExportOpenAPI_invalidFormat(t *testing.T) {
	srv := newOpenAPITestServer(t, "g", sampleEndpointEntities())
	req := httptest.NewRequest(http.MethodGet, "/api/export/g/openapi?format=xml", nil)
	req.SetPathValue("group", "g")
	w := httptest.NewRecorder()
	srv.handleExportOpenAPI(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("want 400 for format=xml, got %d", w.Code)
	}
}

func TestHandleExportOpenAPI_yaml(t *testing.T) {
	srv := newOpenAPITestServer(t, "mygroup", sampleEndpointEntities())
	req := httptest.NewRequest(http.MethodGet, "/api/export/mygroup/openapi?format=yaml", nil)
	req.SetPathValue("group", "mygroup")
	w := httptest.NewRecorder()
	srv.handleExportOpenAPI(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}

	ct := w.Header().Get("Content-Type")
	if !strings.Contains(ct, "yaml") {
		t.Errorf("expected yaml content-type, got %q", ct)
	}

	cd := w.Header().Get("Content-Disposition")
	if !strings.Contains(cd, "openapi.yaml") {
		t.Errorf("expected Content-Disposition with openapi.yaml, got %q", cd)
	}

	body := w.Body.String()
	// Must start with openapi version field.
	if !strings.Contains(body, "openapi: 3.0.3") {
		t.Errorf("yaml body should contain 'openapi: 3.0.3'; got:\n%s", body)
	}
	// Should contain known paths.
	if !strings.Contains(body, "/api/users") {
		t.Errorf("yaml body should contain /api/users path; got:\n%s", body)
	}
	// Path parameters.
	if !strings.Contains(body, "id") {
		t.Errorf("yaml body should contain param 'id'; got:\n%s", body)
	}
	// Tag from owning_backend.
	if !strings.Contains(body, "user-service") {
		t.Errorf("yaml body should contain tag 'user-service'; got:\n%s", body)
	}
	// Call-site entity should NOT appear as a path.
	if strings.Contains(body, "http_endpoint_call") {
		t.Error("yaml body must not include call-site entities")
	}
}

func TestHandleExportOpenAPI_json(t *testing.T) {
	srv := newOpenAPITestServer(t, "mygroup", sampleEndpointEntities())
	req := httptest.NewRequest(http.MethodGet, "/api/export/mygroup/openapi?format=json", nil)
	req.SetPathValue("group", "mygroup")
	w := httptest.NewRecorder()
	srv.handleExportOpenAPI(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}

	ct := w.Header().Get("Content-Type")
	if !strings.Contains(ct, "json") {
		t.Errorf("expected json content-type, got %q", ct)
	}

	cd := w.Header().Get("Content-Disposition")
	if !strings.Contains(cd, "openapi.json") {
		t.Errorf("expected Content-Disposition with openapi.json, got %q", cd)
	}

	var doc openAPIDoc
	if err := json.Unmarshal(w.Body.Bytes(), &doc); err != nil {
		t.Fatalf("response is not valid JSON: %v\nbody: %s", err, w.Body.String())
	}

	if doc.OpenAPI != "3.0.3" {
		t.Errorf("openapi version should be 3.0.3, got %q", doc.OpenAPI)
	}
	if len(doc.Paths) == 0 {
		t.Error("expected at least one path in the spec")
	}

	// /api/users/{id} must have a path parameter named "id".
	pathItem, ok := doc.Paths["/api/users/{id}"]
	if !ok {
		t.Fatal("expected /api/users/{id} in paths")
	}
	if pathItem.Get == nil {
		t.Fatal("expected GET operation on /api/users/{id}")
	}
	foundIDParam := false
	for _, p := range pathItem.Get.Parameters {
		if p.Name == "id" && p.In == "path" && p.Required {
			foundIDParam = true
			break
		}
	}
	if !foundIDParam {
		t.Error("expected path parameter 'id' (in: path, required: true)")
	}

	// /api/users POST should exist.
	usersItem, ok := doc.Paths["/api/users"]
	if !ok {
		t.Fatal("expected /api/users in paths")
	}
	if usersItem.Post == nil {
		t.Error("expected POST operation on /api/users")
	}
	if usersItem.Get == nil {
		t.Error("expected GET operation on /api/users")
	}

	// Tags should include user-service.
	foundTag := false
	for _, tag := range doc.Tags {
		if tag.Name == "user-service" {
			foundTag = true
			break
		}
	}
	if !foundTag {
		t.Error("expected tag 'user-service' in the spec")
	}
}

func TestHandleExportOpenAPI_defaultFormatIsYAML(t *testing.T) {
	srv := newOpenAPITestServer(t, "g", sampleEndpointEntities())
	req := httptest.NewRequest(http.MethodGet, "/api/export/g/openapi", nil)
	req.SetPathValue("group", "g")
	w := httptest.NewRecorder()
	srv.handleExportOpenAPI(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
	ct := w.Header().Get("Content-Type")
	if !strings.Contains(ct, "yaml") {
		t.Errorf("default format should be yaml, got content-type %q", ct)
	}
}

func TestHandleExportOpenAPI_emptyGroup(t *testing.T) {
	// Group exists but has no endpoints — should still return a valid (empty paths) spec.
	srv := newOpenAPITestServer(t, "empty", []graph.Entity{})
	req := httptest.NewRequest(http.MethodGet, "/api/export/empty/openapi?format=json", nil)
	req.SetPathValue("group", "empty")
	w := httptest.NewRecorder()
	srv.handleExportOpenAPI(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}
	var doc openAPIDoc
	if err := json.Unmarshal(w.Body.Bytes(), &doc); err != nil {
		t.Fatalf("response is not valid JSON: %v", err)
	}
	if doc.OpenAPI != "3.0.3" {
		t.Errorf("openapi version should be 3.0.3, got %q", doc.OpenAPI)
	}
}

func TestHandleExportOpenAPI_responseKeys(t *testing.T) {
	entities := []graph.Entity{
		{
			ID:   "ep1",
			Name: "GET /items",
			Kind: "http_endpoint_definition",
			Properties: map[string]string{
				"method":        "GET",
				"path":          "/items",
				"status_codes":  "200",
				"response_keys": "id,name,price",
			},
		},
	}
	srv := newOpenAPITestServer(t, "shop", entities)
	req := httptest.NewRequest(http.MethodGet, "/api/export/shop/openapi?format=json", nil)
	req.SetPathValue("group", "shop")
	w := httptest.NewRecorder()
	srv.handleExportOpenAPI(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}
	var doc openAPIDoc
	if err := json.Unmarshal(w.Body.Bytes(), &doc); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	item, ok := doc.Paths["/items"]
	if !ok {
		t.Fatal("expected /items path")
	}
	if item.Get == nil {
		t.Fatal("expected GET on /items")
	}
	resp200, ok := item.Get.Responses["200"]
	if !ok {
		t.Fatal("expected 200 response on GET /items")
	}
	media, ok := resp200.Content["application/json"]
	if !ok {
		t.Fatal("expected application/json media type in 200 response")
	}
	for _, field := range []string{"id", "name", "price"} {
		if _, hasField := media.Schema.Properties[field]; !hasField {
			t.Errorf("expected response schema property %q", field)
		}
	}
}

func TestHandleExportOpenAPI_anyVerbExpanded(t *testing.T) {
	entities := []graph.Entity{
		{
			ID:   "ep1",
			Name: "ANY /catch-all",
			Kind: "http_endpoint_definition",
			Properties: map[string]string{
				"path": "/catch-all",
				// method deliberately empty → resolves to "ANY"
			},
		},
	}
	srv := newOpenAPITestServer(t, "any-test", entities)
	req := httptest.NewRequest(http.MethodGet, "/api/export/any-test/openapi?format=json", nil)
	req.SetPathValue("group", "any-test")
	w := httptest.NewRecorder()
	srv.handleExportOpenAPI(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}
	var doc openAPIDoc
	if err := json.Unmarshal(w.Body.Bytes(), &doc); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	item, ok := doc.Paths["/catch-all"]
	if !ok {
		t.Fatal("expected /catch-all path")
	}
	// ANY → GET, POST, PUT, PATCH, DELETE all present.
	if item.Get == nil {
		t.Error("ANY should expand to GET")
	}
	if item.Post == nil {
		t.Error("ANY should expand to POST")
	}
	if item.Delete == nil {
		t.Error("ANY should expand to DELETE")
	}
}
