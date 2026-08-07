package mcp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
)

// writeDataFlowSidecar writes a data-flow sidecar for group "test" under a
// temp HOME so handleDataFlows (which resolves via os.UserHomeDir) reads it.
func writeDataFlowSidecar(t *testing.T, doc dataFlowSidecarDoc) {
	t.Helper()
	home := sandboxGrafelHome(t)
	dir := filepath.Join(home, ".grafel", "groups")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	buf, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "test-links-data-flow.json"), buf, 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestDataFlowsTool_ProjectsEdge is the #3867 MCP-projection value assertion:
// the grafel_data_flows tool surfaces the SPECIFIC DATA_FLOWS_TO edge from
// the sidecar — with its field / sink_kind / sink provenance — so the rewrite
// agent can query payload-shape flows. Before #3867 no tool projected it.
func TestDataFlowsTool_ProjectsEdge(t *testing.T) {
	writeDataFlowSidecar(t, dataFlowSidecarDoc{
		Version: 1, Method: "data_flow", Total: 1,
		Links: []dataFlowLinkSidecar{{
			ID:         "df01",
			Source:     "repo-a::createUser",
			Target:     "repo-a::create",
			Relation:   "DATA_FLOWS_TO",
			Method:     "data_flow",
			Confidence: 0.85,
			Properties: map[string]string{
				"field": "name", "sink_kind": "db_write", "sink": "User.create",
			},
		}},
	})

	srv := newTestServer(t, &graph.Document{Repo: "repo-a"})
	out := callFlowTool(t, srv.handleDataFlows, map[string]any{})

	if src, _ := out["source"].(string); src != "sidecar" {
		t.Fatalf("source = %q, want sidecar (sidecar must be read)", src)
	}
	flows, ok := out["data_flows"].([]any)
	if !ok || len(flows) != 1 {
		t.Fatalf("data_flows = %v, want exactly one edge", out["data_flows"])
	}
	rec := flows[0].(map[string]any)
	if rec["from"] != "repo-a::createUser" {
		t.Errorf("from = %v, want repo-a::createUser", rec["from"])
	}
	if rec["to"] != "repo-a::create" {
		t.Errorf("to = %v, want repo-a::create", rec["to"])
	}
	if rec["relation"] != "DATA_FLOWS_TO" {
		t.Errorf("relation = %v, want DATA_FLOWS_TO", rec["relation"])
	}
	if rec["field"] != "name" {
		t.Errorf("field = %v, want name", rec["field"])
	}
	if rec["sink_kind"] != "db_write" {
		t.Errorf("sink_kind = %v, want db_write", rec["sink_kind"])
	}
	if rec["sink"] != "User.create" {
		t.Errorf("sink = %v, want User.create", rec["sink"])
	}
}

// TestDataFlowsTool_SinkKindFilter asserts the sink_kind filter narrows the
// projection to matching edges only.
func TestDataFlowsTool_SinkKindFilter(t *testing.T) {
	writeDataFlowSidecar(t, dataFlowSidecarDoc{
		Version: 1, Method: "data_flow", Total: 2,
		Links: []dataFlowLinkSidecar{
			{ID: "a", Source: "repo-a::h1", Target: "repo-a::s1", Relation: "DATA_FLOWS_TO",
				Properties: map[string]string{"sink_kind": "db_write", "sink": "X.create"}},
			{ID: "b", Source: "repo-a::h2", Target: "repo-a::s2", Relation: "DATA_FLOWS_TO",
				Properties: map[string]string{"sink_kind": "http", "sink": "fetch"}},
		},
	})
	srv := newTestServer(t, &graph.Document{Repo: "repo-a"})
	out := callFlowTool(t, srv.handleDataFlows, map[string]any{"sink_kind": "http"})
	flows, _ := out["data_flows"].([]any)
	if len(flows) != 1 {
		t.Fatalf("expected 1 http-sink flow, got %d: %v", len(flows), flows)
	}
	if flows[0].(map[string]any)["sink"] != "fetch" {
		t.Errorf("filtered flow sink = %v, want fetch", flows[0].(map[string]any)["sink"])
	}
}

// TestDataFlowsTool_MissingSidecar asserts the honest empty/missing contract.
func TestDataFlowsTool_MissingSidecar(t *testing.T) {
	sandboxGrafelHome(t) // no sidecar written
	srv := newTestServer(t, &graph.Document{Repo: "repo-a"})
	out := callFlowTool(t, srv.handleDataFlows, map[string]any{})
	if src, _ := out["source"].(string); src != "missing" {
		t.Errorf("source = %v, want missing", out["source"])
	}
	if c, _ := out["count"].(float64); c != 0 {
		t.Errorf("count = %v, want 0", out["count"])
	}
}
