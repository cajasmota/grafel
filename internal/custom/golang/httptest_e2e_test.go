package golang

import (
	"context"
	"strings"
	"testing"

	"github.com/cajasmota/archigraph/internal/extractor"
)

// Issue #4371 (Go extractor side): the Go httptest extractor must capture every
// stdlib httptest / http route-by-string call in a *_test.go file and stamp the
// `VERB route` pairs onto a one-per-file test_suite's e2e_route_calls property.

const goHTTPTestSrc4371 = `package web

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCreateItem(t *testing.T) {
	body := strings.NewReader("{}")
	req := httptest.NewRequest(http.MethodPost, "/api/v1/inspections/123/items", body)
	rec := httptest.NewRecorder()
	NewRouter().ServeHTTP(rec, req)
}

func TestGetOne(t *testing.T) {
	req, _ := http.NewRequest("GET", "/api/v1/inspections/1", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
}

func TestDelete(t *testing.T) {
	req := httptest.NewRequestWithContext(ctx, http.MethodDelete, "/inspections/9", nil)
	_ = req
}

func TestServerGet(t *testing.T) {
	srv := httptest.NewServer(NewRouter())
	defer srv.Close()
	resp, _ := http.Get(srv.URL + "/inspections/42")
	_ = resp
}

func TestBuiltURLDropped(t *testing.T) {
	// Variable-built route — no static leading-slash literal → must be dropped.
	path := fmt.Sprintf("/inspections/%d", id)
	req := httptest.NewRequest(http.MethodGet, path, nil)
	_ = req
}
`

func TestGoHTTPTestE2E_CapturesRoutes(t *testing.T) {
	ex := &goHTTPTestE2EExtractor{}
	ents, err := ex.Extract(context.Background(), extractor.FileInput{
		Path:     "internal/web/handler_test.go",
		Language: "go",
		Content:  []byte(goHTTPTestSrc4371),
	})
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if len(ents) != 1 {
		t.Fatalf("want exactly 1 test_suite, got %d", len(ents))
	}
	suite := ents[0]
	if suite.Subtype != "test_suite" {
		t.Fatalf("entity subtype = %q, want test_suite", suite.Subtype)
	}
	calls := suite.Properties["e2e_route_calls"]
	if calls == "" {
		t.Fatal("suite carries no e2e_route_calls")
	}
	got := map[string]bool{}
	for _, l := range strings.Split(calls, "\n") {
		got[l] = true
	}
	want := []string{
		"POST /api/v1/inspections/123/items", // httptest.NewRequest + http.MethodPost
		"GET /api/v1/inspections/1",          // http.NewRequest + "GET" literal
		"DELETE /inspections/9",              // NewRequestWithContext + http.MethodDelete
		"GET /inspections/42",                // http.Get(srv.URL + "/...")
	}
	for _, w := range want {
		if !got[w] {
			t.Errorf("missing route call %q (got: %v)", w, got)
		}
	}
	// Conservative negative: the fmt.Sprintf-built path must NOT appear.
	for l := range got {
		if strings.Contains(l, "%d") || strings.HasPrefix(l, "GET path") {
			t.Errorf("unexpected non-literal route captured: %q", l)
		}
	}
	if len(got) != len(want) {
		t.Errorf("captured %d routes, want %d: %v", len(got), len(want), got)
	}
}

// Conservative: a non-test file (no _test.go suffix) must produce nothing.
func TestGoHTTPTestE2E_NonTestFileNoOp(t *testing.T) {
	ex := &goHTTPTestE2EExtractor{}
	ents, err := ex.Extract(context.Background(), extractor.FileInput{
		Path:     "internal/web/handler.go",
		Language: "go",
		Content:  []byte(goHTTPTestSrc4371),
	})
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if len(ents) != 0 {
		t.Fatalf("non-test file must emit 0 entities, got %d", len(ents))
	}
}
