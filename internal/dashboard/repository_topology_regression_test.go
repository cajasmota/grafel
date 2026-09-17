package dashboard

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRepositoryTopologyRegressionPreservesEstablishedRoutes(t *testing.T) {
	handler := newRepositoryTopologyTestServer(repositoryHandlerFixture()).routes()
	tests := []struct {
		name        string
		path        string
		contentType string
		contains    string
	}{
		{name: "graph", path: "/api/v2/graph/test?lod=low", contentType: "application/json", contains: `"ok":true`},
		{name: "stream", path: "/api/v2/graph/test/stream?lod=low", contentType: "text/event-stream", contains: "event: meta"},
		{name: "dubbo", path: "/api/v2/groups/test/dubbo", contentType: "application/json", contains: `"summary":`},
		{name: "topology", path: "/api/v2/topology/test", contentType: "application/json", contains: `"ok":true`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, test.path, nil))
			if recorder.Code != http.StatusOK {
				t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
			}
			if !strings.Contains(recorder.Header().Get("Content-Type"), test.contentType) {
				t.Fatalf("content-type=%q", recorder.Header().Get("Content-Type"))
			}
			if !strings.Contains(recorder.Body.String(), test.contains) {
				t.Fatalf("missing %q in body=%s", test.contains, recorder.Body.String())
			}
		})
	}
}
