package mcp

import (
	"testing"

	"github.com/cajasmota/archigraph/internal/graph"
)

// docWithJoinEdge builds a one-repo Document whose DataAccess node reaches a
// joined collection ONLY via a JOINS_COLLECTION graph edge (the #4299 repro:
// a Mongo $lookup recorded on the graph but absent from the taint sidecar).
func docWithJoinEdge(repo, kind string) *graph.Document {
	return &graph.Document{
		Repo: repo,
		Entities: []graph.Entity{
			{ID: "da1", Name: "scheduleLookup", Kind: "DataAccess", SourceFile: "schedule_viewset.py"},
			{ID: "Class:Inspection", Name: "Inspection", Kind: "Class", SourceFile: "models.py"},
		},
		Relationships: []graph.Relationship{
			{ID: "edge1", FromID: "da1", ToID: "Class:Inspection", Kind: kind, Confidence: 0.9},
		},
	}
}

// TestDataFlows_JoinsCollectionProjectedAsDBSink is the #4299 RED→GREEN: a flow
// whose only DB signal is a JOINS_COLLECTION graph edge must appear under
// data_flows(sink_kind=db). Before the fix handleDataFlows read only the taint
// sidecar, so this returned 0.
func TestDataFlows_JoinsCollectionProjectedAsDBSink(t *testing.T) {
	t.Setenv("HOME", t.TempDir()) // no sidecar — pure graph-edge projection
	srv := newTestServer(t, docWithJoinEdge("repo-a", "JOINS_COLLECTION"))

	out := callFlowTool(t, srv.handleDataFlows, map[string]any{"sink_kind": "db"})

	flows, ok := out["data_flows"].([]any)
	if !ok || len(flows) != 1 {
		t.Fatalf("data_flows = %v, want exactly one JOINS_COLLECTION db sink", out["data_flows"])
	}
	rec := flows[0].(map[string]any)
	if rec["from"] != "repo-a::da1" {
		t.Errorf("from = %v, want repo-a::da1", rec["from"])
	}
	if rec["to"] != "repo-a::Class:Inspection" {
		t.Errorf("to = %v, want repo-a::Class:Inspection", rec["to"])
	}
	if rec["relation"] != "JOINS_COLLECTION" {
		t.Errorf("relation = %v, want JOINS_COLLECTION", rec["relation"])
	}
	if rec["sink_kind"] != "db_read" {
		t.Errorf("sink_kind = %v, want db_read", rec["sink_kind"])
	}
	if rec["sink"] != "Inspection" {
		t.Errorf("sink = %v, want Inspection (target entity name)", rec["sink"])
	}
	if rec["source"] != "graph-edge" {
		t.Errorf("flow source = %v, want graph-edge", rec["source"])
	}
	if src, _ := out["source"].(string); src != "graph-edge" {
		t.Errorf("envelope source = %v, want graph-edge", src)
	}
}

// TestDataFlows_DBEdgeSiblingsProjected covers the sibling DB-access edge kinds
// added alongside JOINS_COLLECTION, each mapped to the right db_read/db_write
// effect sink_kind.
func TestDataFlows_DBEdgeSiblingsProjected(t *testing.T) {
	cases := []struct {
		kind     string
		sinkKind string
	}{
		{"READS_FROM", "db_read"},
		{"WRITES_TO", "db_write"},
		{"QUERIES", "db_read"},
		{"ACCESSES_TABLE", "db_read"},
		{"MODIFIES_TABLE", "db_write"},
		{"GRAPH_RELATES", "db_read"},
	}
	for _, c := range cases {
		t.Run(c.kind, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			srv := newTestServer(t, docWithJoinEdge("repo-a", c.kind))
			out := callFlowTool(t, srv.handleDataFlows, map[string]any{"sink_kind": "db"})
			flows, _ := out["data_flows"].([]any)
			if len(flows) != 1 {
				t.Fatalf("%s: data_flows len = %d, want 1", c.kind, len(flows))
			}
			if sk := flows[0].(map[string]any)["sink_kind"]; sk != c.sinkKind {
				t.Errorf("%s: sink_kind = %v, want %v", c.kind, sk, c.sinkKind)
			}
		})
	}
}

// TestDataFlows_NonDBEdgeNotProjected is the negative: a non-DB semantic edge of
// the same node shape (THROWS) must NOT be projected as a db sink.
func TestDataFlows_NonDBEdgeNotProjected(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	srv := newTestServer(t, docWithJoinEdge("repo-a", "THROWS"))
	out := callFlowTool(t, srv.handleDataFlows, map[string]any{"sink_kind": "db"})
	flows, _ := out["data_flows"].([]any)
	if len(flows) != 0 {
		t.Fatalf("THROWS must not be a db sink; got %d flows: %v", len(flows), flows)
	}
	// With no sidecar and no DB edges, the envelope reports the honest missing
	// contract rather than fabricating a flow.
	if src, _ := out["source"].(string); src != "missing" {
		t.Errorf("source = %v, want missing", src)
	}
}

// TestDataFlows_NonDBSinkKindFilterExcludesGraphEdges asserts a non-db sink_kind
// filter (http) excludes the graph-edge DB projection entirely.
func TestDataFlows_NonDBSinkKindFilterExcludesGraphEdges(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	srv := newTestServer(t, docWithJoinEdge("repo-a", "JOINS_COLLECTION"))
	out := callFlowTool(t, srv.handleDataFlows, map[string]any{"sink_kind": "http"})
	flows, _ := out["data_flows"].([]any)
	if len(flows) != 0 {
		t.Fatalf("sink_kind=http must exclude graph DB edges; got %d: %v", len(flows), flows)
	}
}

// TestDataFlows_DBReadFilterMatchesProjectedKind asserts the concrete effect
// filter db_read selects the JOINS_COLLECTION projection (which maps to db_read)
// and db_write excludes it.
func TestDataFlows_DBReadFilterMatchesProjectedKind(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	srv := newTestServer(t, docWithJoinEdge("repo-a", "JOINS_COLLECTION"))

	got := callFlowTool(t, srv.handleDataFlows, map[string]any{"sink_kind": "db_read"})
	if f, _ := got["data_flows"].([]any); len(f) != 1 {
		t.Fatalf("db_read filter: want 1 flow, got %d", len(f))
	}
	got = callFlowTool(t, srv.handleDataFlows, map[string]any{"sink_kind": "db_write"})
	if f, _ := got["data_flows"].([]any); len(f) != 0 {
		t.Fatalf("db_write filter: JOINS_COLLECTION maps to db_read, want 0, got %d", len(f))
	}
}
