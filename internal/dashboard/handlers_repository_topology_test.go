package dashboard

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestHandleV2RepositoryTopologyReturnsFilteredResponse(t *testing.T) {
	srv := newRepositoryTopologyTestServer(repositoryHandlerFixture())
	req := httptest.NewRequest(http.MethodGet, "/api/v2/repository-topology/test?channels=dubbo&focus=client&direction=outbound&depth=2", nil)
	req.SetPathValue("group", "test")
	rec := httptest.NewRecorder()
	srv.handleV2RepositoryTopology(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		OK   bool                       `json:"ok"`
		Data repositoryTopologyResponse `json:"data"`
	}
	decodeRepositoryTopologyJSON(t, rec, &body)
	if !body.OK {
		t.Fatal("expected ok response")
	}
	assertRepositoryNodeIDs(t, body.Data.Nodes, "client", "rule", "storage")
	if strings.Contains(rec.Body.String(), `"nodes":null`) || strings.Contains(rec.Body.String(), `"edges":null`) {
		t.Fatalf("nil slices leaked to wire: %s", rec.Body.String())
	}
}

func TestHandleV2RepositoryTopologyRejectsInvalidAndUnknownRepositories(t *testing.T) {
	srv := newRepositoryTopologyTestServer(repositoryHandlerFixture())
	for _, rawURL := range []string{
		"/api/v2/repository-topology/test?direction=sideways",
		"/api/v2/repository-topology/test?focus=missing",
		"/api/v2/repository-topology/test?repos=client,missing",
		"/api/v2/repository-topology/test?source=client&target=missing",
	} {
		t.Run(rawURL, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, rawURL, nil)
			req.SetPathValue("group", "test")
			rec := httptest.NewRecorder()
			srv.handleV2RepositoryTopology(rec, req)
			if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), `"code":"bad_request"`) {
				t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestHandleV2RepositoryTopologyReturnsNotFoundWhenGraphLoadFails(t *testing.T) {
	srv := &Server{graphs: NewGraphCache(time.Minute)}
	req := httptest.NewRequest(http.MethodGet, "/api/v2/repository-topology/missing", nil)
	req.SetPathValue("group", "missing")
	rec := httptest.NewRecorder()
	srv.handleV2RepositoryTopology(rec, req)
	if rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), `"code":"not_found"`) {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandleV2RepositoryTopologyEdgePaginatesAndFilters(t *testing.T) {
	grp := repositoryTopologyTestGroup("client", "rule")
	for index := 0; index < 30; index++ {
		confidence := "resolved"
		if index%2 == 1 {
			confidence = "inferred"
		}
		grp.Links = append(grp.Links, CrossRepoLink{
			Source: "client::c" + fmt.Sprint(index), Target: "rule::p" + fmt.Sprint(index),
			Channel: "dubbo", Identifier: fmt.Sprintf("dubbo:OrderService%02d", index),
			Properties: map[string]string{"confidence": confidence},
		})
	}
	srv := newRepositoryTopologyTestServer(grp)
	req := httptest.NewRequest(http.MethodGet, "/api/v2/repository-topology/test/edge?source=client&target=rule&channel=dubbo", nil)
	req.SetPathValue("group", "test")
	rec := httptest.NewRecorder()
	srv.handleV2RepositoryTopologyEdge(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		OK         bool                       `json:"ok"`
		Data       []repositoryTopologyDetail `json:"data"`
		Pagination V2Pagination               `json:"pagination"`
	}
	decodeRepositoryTopologyJSON(t, rec, &body)
	if len(body.Data) != 25 || body.Pagination.Total != 30 || body.Pagination.Limit != 25 || body.Pagination.Offset != 0 {
		t.Fatalf("unexpected page: rows=%d pagination=%#v", len(body.Data), body.Pagination)
	}
	if body.Data[0].Identifier != "dubbo:OrderService00" || body.Data[24].Identifier != "dubbo:OrderService24" {
		t.Fatalf("detail ordering is unstable: first=%q last=%q", body.Data[0].Identifier, body.Data[24].Identifier)
	}

	filteredReq := httptest.NewRequest(http.MethodGet, "/api/v2/repository-topology/test/edge?source=client&target=rule&channel=dubbo&evidence=inferred&q=orderservice1&page_size=200", nil)
	filteredReq.SetPathValue("group", "test")
	filteredRec := httptest.NewRecorder()
	srv.handleV2RepositoryTopologyEdge(filteredRec, filteredReq)
	if filteredRec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", filteredRec.Code, filteredRec.Body.String())
	}
	decodeRepositoryTopologyJSON(t, filteredRec, &body)
	if body.Pagination.Limit != 100 || body.Pagination.Total != 5 {
		t.Fatalf("filtered pagination=%#v rows=%#v", body.Pagination, body.Data)
	}
}

func TestHandleV2RepositoryTopologyEdgeRejectsInvalidSelection(t *testing.T) {
	srv := newRepositoryTopologyTestServer(repositoryHandlerFixture())
	for _, suffix := range []string{
		"?source=client&target=rule",
		"?source=client&target=rule&channel=smtp",
		"?source=client&target=missing&channel=dubbo",
		"?source=client&target=rule&channel=dubbo&page=0",
	} {
		req := httptest.NewRequest(http.MethodGet, "/api/v2/repository-topology/test/edge"+suffix, nil)
		req.SetPathValue("group", "test")
		rec := httptest.NewRecorder()
		srv.handleV2RepositoryTopologyEdge(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("suffix=%s status=%d body=%s", suffix, rec.Code, rec.Body.String())
		}
	}
}

func newRepositoryTopologyTestServer(grp *DashGroup) *Server {
	cache := NewGraphCache(time.Minute)
	cache.entries["test"] = &cacheEntry{group: grp, loadedAt: time.Now()}
	return &Server{graphs: cache}
}

func repositoryHandlerFixture() *DashGroup {
	grp := repositoryTopologyTestGroup("client", "rule", "storage", "other")
	grp.Links = []CrossRepoLink{
		{Source: "client::client", Target: "rule::api", Channel: "dubbo", Identifier: "dubbo:RuleFacade"},
		{Source: "rule::client", Target: "storage::api", Channel: "dubbo", Identifier: "dubbo:StorageFacade"},
		{Source: "other::client", Target: "client::api", Method: "http", Identifier: "http:GET:/client"},
	}
	return grp
}

func decodeRepositoryTopologyJSON(t *testing.T, rec *httptest.ResponseRecorder, target any) {
	t.Helper()
	if err := json.Unmarshal(rec.Body.Bytes(), target); err != nil {
		t.Fatalf("decode response: %v body=%s", err, rec.Body.String())
	}
}
