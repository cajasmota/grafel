// adjacency_scans_2285_test.go — behavioural + benchmark coverage for the
// three Doc.Relationships linear scans replaced with adjacency-index lookups
// in #2285. Each test exercises one of the three handler sites:
//
//	(1) find-edge-scan        — handleFind compact-mode edge carry
//	(2) pickFallback (skip)   — NOT a Relationships scan; iterates Entities
//	                            for max-pagerank. Documented in PR body.
//	(3) agentResolvedEdges    — handleGetNode agent-repair attribution
//
// These are behaviour-identical refactors; the assertions match the pre-fix
// behaviour exactly so they pass on either side of the change.
package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/cajasmota/archigraph/internal/graph"
	mcpapi "github.com/mark3labs/mcp-go/mcp"
)

// ---------------------------------------------------------------------------
// Site 1 — find-edge-scan: edges between visible nodes in compact mode.
// ---------------------------------------------------------------------------

func TestPerf2285_FindCompactEmitsEdgesBetweenVisibleNodes(t *testing.T) {
	doc := &graph.Document{
		Entities: []graph.Entity{
			{ID: "root", Name: "Root", QualifiedName: "pkg.Root", Kind: "Function", SourceFile: "r.go", StartLine: 1},
			{ID: "a", Name: "A", QualifiedName: "pkg.A", Kind: "Function", SourceFile: "a.go", StartLine: 1},
			{ID: "b", Name: "B", QualifiedName: "pkg.B", Kind: "Function", SourceFile: "b.go", StartLine: 1},
			{ID: "orphan", Name: "Orphan", QualifiedName: "pkg.Orphan", Kind: "Function", SourceFile: "o.go", StartLine: 1},
		},
		Relationships: []graph.Relationship{
			{FromID: "root", ToID: "a", Kind: "CALLS"},
			{FromID: "a", ToID: "b", Kind: "CALLS"},
			// edge touching an out-of-subgraph node — MUST NOT be emitted
			{FromID: "b", ToID: "orphan", Kind: "CALLS"},
		},
	}
	srv := newTestServerWithDoc(t, doc)
	req := mcpapi.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"group":   "test",
		"query":   "Root",
		"context": "graph",
		"depth":   float64(2),
	}
	res, err := srv.handleQueryGraph(context.Background(), req)
	if err != nil {
		t.Fatalf("handleQueryGraph: %v", err)
	}
	if res == nil || res.IsError {
		t.Fatalf("handleQueryGraph returned error: %+v", res)
	}
	var body string
	for _, c := range res.Content {
		if tc, ok := c.(mcpapi.TextContent); ok {
			body = tc.Text
		}
	}
	// Visible-subgraph edges Root->A and A->B should be carried (two edges
	// inside the BFS-visited set). The B->Orphan edge straddles the visible
	// boundary at depth=2 (orphan is unreachable from Root within 2 hops via
	// the BFS) so the compact renderer should not see three. renderCompact
	// summarises the count as "edges-summary: available=N"; we assert N=2.
	if !strings.Contains(body, "edges-summary: available=2") {
		t.Errorf("expected 2 in-subgraph edges in compact output, got:\n%s", body)
	}
	// Orphan was unreachable: it must not appear in the rendered subgraph.
	if strings.Contains(body, "Orphan") {
		t.Errorf("Orphan was unreachable but appears in compact output:\n%s", body)
	}
}

// ---------------------------------------------------------------------------
// Site 3 — agentResolvedEdgesForEntity: filter out-edges by Properties.
// ---------------------------------------------------------------------------

func TestPerf2285_AgentResolvedEdgesUsesAdjacency(t *testing.T) {
	doc := &graph.Document{
		Entities: []graph.Entity{
			{ID: "src", Name: "Src", QualifiedName: "pkg.Src", Kind: "Function", SourceFile: "s.go", StartLine: 1},
			{ID: "auto", Name: "Auto", QualifiedName: "pkg.Auto", Kind: "Function", SourceFile: "a.go", StartLine: 1},
			{ID: "manual", Name: "Manual", QualifiedName: "pkg.Manual", Kind: "Function", SourceFile: "m.go", StartLine: 1},
			{ID: "other", Name: "Other", QualifiedName: "pkg.Other", Kind: "Function", SourceFile: "o.go", StartLine: 1},
		},
		Relationships: []graph.Relationship{
			// agent-repair out-edge from src — must be included
			{FromID: "src", ToID: "auto", Kind: "CALLS", Properties: map[string]string{
				"resolved_by":       "agent-repair",
				"resolved_by_agent": "claude-3-5",
				"repair_reasoning":  "matched on qualified name",
			}},
			// non-agent out-edge from src — must be excluded
			{FromID: "src", ToID: "manual", Kind: "CALLS"},
			// agent-repair edge NOT from src — must be excluded (direction)
			{FromID: "other", ToID: "src", Kind: "CALLS", Properties: map[string]string{
				"resolved_by": "agent-repair",
			}},
		},
	}
	srv := newTestServerWithDoc(t, doc)
	req := mcpapi.CallToolRequest{}
	req.Params.Arguments = map[string]any{"group": "test", "entity_id": "src"}
	res, err := srv.handleGetNode(context.Background(), req)
	if err != nil {
		t.Fatalf("handleGetNode: %v", err)
	}
	if res == nil || res.IsError {
		t.Fatalf("handleGetNode error: %+v", res)
	}
	var body string
	for _, c := range res.Content {
		if tc, ok := c.(mcpapi.TextContent); ok {
			body = tc.Text
		}
	}
	var decoded map[string]any
	if err := json.Unmarshal([]byte(body), &decoded); err != nil {
		t.Fatalf("decode: %v\nraw: %s", err, body)
	}
	raw, ok := decoded["agent_resolved_edges"]
	if !ok {
		t.Fatalf("expected agent_resolved_edges in response, got: %v", decoded)
	}
	edges, ok := raw.([]any)
	if !ok {
		t.Fatalf("agent_resolved_edges not a list: %T", raw)
	}
	if len(edges) != 1 {
		t.Fatalf("expected exactly 1 agent-resolved edge from src, got %d: %+v", len(edges), edges)
	}
	e := edges[0].(map[string]any)
	if e["target"] != "auto" {
		t.Errorf("wrong target: %v", e["target"])
	}
	if e["kind"] != "CALLS" {
		t.Errorf("wrong kind: %v", e["kind"])
	}
	if e["resolved_by"] != "agent-repair" {
		t.Errorf("wrong resolved_by: %v", e["resolved_by"])
	}
	if e["resolved_by_agent"] != "claude-3-5" {
		t.Errorf("expected resolved_by_agent passthrough, got: %v", e["resolved_by_agent"])
	}
	if e["repair_reasoning"] != "matched on qualified name" {
		t.Errorf("expected repair_reasoning passthrough, got: %v", e["repair_reasoning"])
	}
}

// TestPerf2285_AgentResolvedEdgesEmptyWhenNoAgentEdges ensures the function
// returns nil (omits the key) when the entity has out-edges but none from
// the agent-repair layer. This exercises the early-return path where the
// adjacency lookup hits but the Properties filter rejects everything.
func TestPerf2285_AgentResolvedEdgesEmptyWhenNoAgentEdges(t *testing.T) {
	doc := &graph.Document{
		Entities: []graph.Entity{
			{ID: "src", Name: "Src", QualifiedName: "pkg.Src", Kind: "Function", SourceFile: "s.go", StartLine: 1},
			{ID: "dst", Name: "Dst", QualifiedName: "pkg.Dst", Kind: "Function", SourceFile: "d.go", StartLine: 1},
		},
		Relationships: []graph.Relationship{
			{FromID: "src", ToID: "dst", Kind: "CALLS"}, // no Properties
		},
	}
	srv := newTestServerWithDoc(t, doc)
	req := mcpapi.CallToolRequest{}
	req.Params.Arguments = map[string]any{"group": "test", "entity_id": "src"}
	res, _ := srv.handleGetNode(context.Background(), req)
	var body string
	for _, c := range res.Content {
		if tc, ok := c.(mcpapi.TextContent); ok {
			body = tc.Text
		}
	}
	if strings.Contains(body, "agent_resolved_edges") {
		t.Errorf("expected agent_resolved_edges absent when no agent edges; got:\n%s", body)
	}
}

// ---------------------------------------------------------------------------
// Adjacency helpers themselves — Outgoing/Incoming added in #2285.
// ---------------------------------------------------------------------------

func TestPerf2285_AdjacencyOutgoingIncomingDirectionality(t *testing.T) {
	doc := &graph.Document{
		Entities: []graph.Entity{
			{ID: "x"}, {ID: "y"}, {ID: "z"},
		},
		Relationships: []graph.Relationship{
			{FromID: "x", ToID: "y", Kind: "CALLS"},
			{FromID: "z", ToID: "x", Kind: "CALLS"},
		},
	}
	a := buildAdjacency(doc, "repo1")
	out := a.Outgoing("x")
	if len(out) != 1 || out[0].target != "y" {
		t.Errorf("Outgoing(x) = %+v; want [y]", out)
	}
	in := a.Incoming("x")
	if len(in) != 1 || in[0].target != "z" {
		t.Errorf("Incoming(x) = %+v; want [z]", in)
	}
	// nil-safe receiver
	var na *adjacency
	if got := na.Outgoing("x"); got != nil {
		t.Errorf("nil.Outgoing should be nil, got %+v", got)
	}
	if got := na.Incoming("x"); got != nil {
		t.Errorf("nil.Incoming should be nil, got %+v", got)
	}
	// relIdx round-trips into Doc.Relationships
	if out[0].relIdx < 0 || out[0].relIdx >= len(doc.Relationships) {
		t.Fatalf("relIdx out of range: %d", out[0].relIdx)
	}
	if doc.Relationships[out[0].relIdx].ToID != "y" {
		t.Errorf("relIdx does not point back to source relationship")
	}
}

// ---------------------------------------------------------------------------
// Benchmarks — confirm the adjacency-indexed sites scale by deg(v) rather
// than |Relationships|.
// ---------------------------------------------------------------------------

func benchDoc(nRels int) *graph.Document {
	ents := make([]graph.Entity, 0, nRels+2)
	ents = append(ents,
		graph.Entity{ID: "src", Name: "Src", QualifiedName: "pkg.Src", Kind: "Function", SourceFile: "s.go", StartLine: 1},
		graph.Entity{ID: "agent_target", Name: "Agent", QualifiedName: "pkg.Agent", Kind: "Function", SourceFile: "a.go", StartLine: 1},
	)
	rels := make([]graph.Relationship, 0, nRels+1)
	// One agent-repair edge from src
	rels = append(rels, graph.Relationship{
		FromID: "src", ToID: "agent_target", Kind: "CALLS",
		Properties: map[string]string{"resolved_by": "agent-repair"},
	})
	// nRels filler edges between distinct unrelated entities so the linear
	// scan must walk them all.
	for i := 0; i < nRels; i++ {
		id := "n" + itoa(i)
		ents = append(ents, graph.Entity{ID: id, Name: id, Kind: "Function", SourceFile: "f.go", StartLine: 1})
		rels = append(rels, graph.Relationship{FromID: id, ToID: "src", Kind: "REFERENCES"})
	}
	return &graph.Document{Entities: ents, Relationships: rels}
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b [20]byte
	n := len(b)
	for i > 0 {
		n--
		b[n] = byte('0' + i%10)
		i /= 10
	}
	return string(b[n:])
}

func BenchmarkArchigraphAgentResolvedEdges(b *testing.B) {
	doc := benchDoc(10000)
	byID := make(map[string]*graph.Entity, len(doc.Entities))
	for i := range doc.Entities {
		byID[doc.Entities[i].ID] = &doc.Entities[i]
	}
	lr := &LoadedRepo{
		Repo:       "repo1",
		Doc:        doc,
		LabelIndex: BuildLabelIndex(doc),
		Adjacency:  buildAdjacency(doc, "repo1"),
		ByID:       byID,
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = agentResolvedEdgesForEntity(lr, "src", true)
	}
}
