// flagon_deadlock_5928_test.go — #5928: the four PRE-EXISTING flag-ON nested-rmu
// deadlocks on the default (GRAFEL_SERVE_FROM_MMAP) live-Reader read path.
//
// forEachEntity/forEachRelationship hold the repo's readerMu (rmu, a
// non-reentrant sync.Mutex) across the ENTIRE scan on the flag-ON path
// (Option-B, ADR-0027). Four handlers called an rmu-locking accessor
// (getByIDOne→LabelIndex.at, getAdjacency, getStepAdj) from INSIDE a forEach*
// closure, self-deadlocking whenever a Reader is mapped:
//
//  1. handleTopologyTopicDetail  — resolve()→getByIDOne in a forEachRelationship
//  2. handlePatternsGetGraph     — resolve()→getByIDOne in a forEachRelationship
//  3. endpointPostureScan        — buildPosturePayload→getAdjacency/getByIDOne in
//     a forEachEntity
//  4. handleTracesGet            — buildProcessStepsWithCrossRepo→getStepAdj/
//     getByIDOne in a forEachEntity
//
// Each test runs the REAL handler flag-ON with a LIVE Reader and an EMPTIED Doc
// (readerEmptiedRepo, the deretain-flip direction) under a hard timeout. Without
// the collect-then-process fix the in-scan accessor re-locks rmu and the handler
// HANGS (noHang → t.Fatal). With the fix it completes AND its output matches the
// flag-OFF full-Doc result byte-for-byte. noHang / readerEmptiedRepo / docFullRepo
// / topologyServer are shared with the PR7a guard suite.
package mcp

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/graph/fbreader"
	"github.com/cajasmota/grafel/internal/graph/fbwriter"
	mcpapi "github.com/mark3labs/mcp-go/mcp"
)

// deadlock5928Doc builds one fixture exercising all four #5928 sites:
//   - a Topic with a PUBLISHES_TO and a SUBSCRIBES_TO edge (topic detail),
//   - a Pattern with an EXEMPLIFIES edge (patterns get graph),
//   - an http endpoint with a THROWS edge to an ExceptionType + a rate_limit
//     property (endpoint posture scan),
//   - a Process with a `chain` step property (traces get).
func deadlock5928Doc() *graph.Document {
	mkEnt := func(id, name, kind string) graph.Entity {
		return graph.Entity{ID: id, Name: name, QualifiedName: name, Kind: kind, SourceFile: "src/" + id + ".go", Language: "go", StartLine: 1, EndLine: 5}
	}

	topic := graph.Entity{ID: "t_orders", Name: "orders.created", QualifiedName: "orders.created", Kind: "Topic", SourceFile: "events.go", Language: "go", StartLine: 1, EndLine: 2}
	pubSvc := mkEnt("pubsvc", "OrderService", "SCOPE.Operation")
	subSvc := mkEnt("subsvc", "ShipService", "SCOPE.Operation")

	pattern := mkEnt("pat1", "RepositoryPattern", "SCOPE.Pattern")
	exemplar := mkEnt("ex1", "UserRepo", "SCOPE.Component")

	endpoint := graph.Entity{ID: "ep1", Name: "GET /orders", QualifiedName: "GET /orders", Kind: "http_endpoint_definition", SourceFile: "src/orders.go", Language: "go", StartLine: 10, EndLine: 12}
	endpoint.PropSet("verb", "GET")
	endpoint.PropSet("path", "/orders")
	endpoint.PropSet("rate_limited", "true")
	exc := mkEnt("exc1", "exception:NotFound", "SCOPE.ExceptionType")

	proc := graph.Entity{ID: "proc1", Name: "OrderFlow", QualifiedName: "OrderFlow", Kind: "SCOPE.Process", SourceFile: "src/flow.go", Language: "go", StartLine: 1, EndLine: 9}
	proc.PropSet("entry_id", "step1")
	proc.PropSet("entry_name", "handleOrder")
	proc.PropSet("chain", "step1")
	step := mkEnt("step1", "handleOrder", "SCOPE.Operation")

	ents := []graph.Entity{topic, pubSvc, subSvc, pattern, exemplar, endpoint, exc, proc, step}

	mkRel := func(from, to, kind string) graph.Relationship {
		return graph.Relationship{FromID: from, ToID: to, Kind: kind}
	}
	rels := []graph.Relationship{
		mkRel("pubsvc", "t_orders", "PUBLISHES_TO"),
		mkRel("subsvc", "t_orders", "SUBSCRIBES_TO"),
		mkRel("ex1", "pat1", "EXEMPLIFIES"),
		mkRel("ep1", "exc1", "THROWS"),
	}
	return &graph.Document{Repo: "corpus", Entities: ents, Relationships: rels}
}

func loadDeadlock5928Fixture(t *testing.T) (*graph.Document, *fbreader.Reader) {
	t.Helper()
	dir := t.TempDir()
	fbPath := filepath.Join(dir, "graph.fb")
	if err := fbwriter.WriteAtomic(fbPath, deadlock5928Doc()); err != nil {
		t.Fatalf("WriteAtomic: %v", err)
	}
	doc, err := graph.LoadGraphFromDir(dir)
	if err != nil {
		t.Fatalf("LoadGraphFromDir: %v", err)
	}
	r, err := fbreader.Open(fbPath)
	if err != nil {
		t.Fatalf("fbreader.Open: %v", err)
	}
	t.Cleanup(func() { r.Close() })
	return doc, r
}

// callWithArgs drives a real MCP handler with the given argument map and returns
// its first TextContent payload.
func callWithArgs(t *testing.T, s *Server, fn func(context.Context, mcpapi.CallToolRequest) (*mcpapi.CallToolResult, error), args map[string]any) string {
	t.Helper()
	req := mcpapi.CallToolRequest{}
	req.Params.Arguments = args
	res, err := fn(context.Background(), req)
	if err != nil {
		t.Fatalf("handler err: %v", err)
	}
	if res == nil || len(res.Content) == 0 {
		return ""
	}
	if tc, ok := res.Content[0].(mcpapi.TextContent); ok {
		return tc.Text
	}
	return ""
}

// Site 1: handleTopologyTopicDetail — resolve()→getByIDOne inside a
// forEachRelationship scan.
func TestTopologyTopicDetail_flagON_noDeadlock_5928(t *testing.T) {
	doc, r := loadDeadlock5928Fixture(t)
	args := map[string]any{"group": "g", "topic_id": "t_orders"}

	withServeFromMMap(t, false)
	sOff := topologyServer(t, docFullRepo(doc))
	want := callWithArgs(t, sOff, sOff.handleTopologyTopicDetail, args)
	if want == "" {
		t.Fatal("fixture must produce a non-empty topic-detail result flag-OFF")
	}

	withServeFromMMap(t, true)
	sOn := topologyServer(t, readerEmptiedRepo(t, doc, r))
	var got string
	noHang(t, 5*time.Second, "handleTopologyTopicDetail", func() {
		got = callWithArgs(t, sOn, sOn.handleTopologyTopicDetail, args)
	})
	if got != want {
		t.Fatalf("topic detail flag-ON(emptied Doc) != flag-OFF\n got=%s\nwant=%s", got, want)
	}
}

// Site 2: handlePatternsGetGraph — resolve()→getByIDOne inside a
// forEachRelationship scan.
func TestPatternsGetGraph_flagON_noDeadlock_5928(t *testing.T) {
	doc, r := loadDeadlock5928Fixture(t)
	args := map[string]any{"group": "g", "pattern_id": "pat1"}

	withServeFromMMap(t, false)
	sOff := topologyServer(t, docFullRepo(doc))
	want := callWithArgs(t, sOff, sOff.handlePatternsGetGraph, args)
	if want == "" {
		t.Fatal("fixture must produce a non-empty pattern-graph result flag-OFF")
	}

	withServeFromMMap(t, true)
	sOn := topologyServer(t, readerEmptiedRepo(t, doc, r))
	var got string
	noHang(t, 5*time.Second, "handlePatternsGetGraph", func() {
		got = callWithArgs(t, sOn, sOn.handlePatternsGetGraph, args)
	})
	if got != want {
		t.Fatalf("patterns get graph flag-ON(emptied Doc) != flag-OFF\n got=%s\nwant=%s", got, want)
	}
}

// Site 3: endpointPostureScan — buildPosturePayload→getAdjacency/getByIDOne
// inside a forEachEntity scan.
func TestEndpointPostureScan_flagON_noDeadlock_5928(t *testing.T) {
	doc, r := loadDeadlock5928Fixture(t)
	args := map[string]any{"group": "g"} // no entity_id → repo-wide scan

	withServeFromMMap(t, false)
	sOff := topologyServer(t, docFullRepo(doc))
	want := callWithArgs(t, sOff, sOff.handleEndpointPosture, args)
	if want == "" {
		t.Fatal("fixture must produce a non-empty endpoint-posture result flag-OFF")
	}

	withServeFromMMap(t, true)
	sOn := topologyServer(t, readerEmptiedRepo(t, doc, r))
	var got string
	noHang(t, 5*time.Second, "handleEndpointPosture(scan)", func() {
		got = callWithArgs(t, sOn, sOn.handleEndpointPosture, args)
	})
	if got != want {
		t.Fatalf("endpoint posture scan flag-ON(emptied Doc) != flag-OFF\n got=%s\nwant=%s", got, want)
	}
}

// Site 4: handleTracesGet — buildProcessStepsWithCrossRepo→getStepAdj/getByIDOne
// inside a forEachEntity scan.
func TestTracesGet_flagON_noDeadlock_5928(t *testing.T) {
	doc, r := loadDeadlock5928Fixture(t)
	args := map[string]any{"group": "g", "process_id": "proc1"}

	withServeFromMMap(t, false)
	sOff := topologyServer(t, docFullRepo(doc))
	want := callWithArgs(t, sOff, sOff.handleTracesGet, args)
	if want == "" {
		t.Fatal("fixture must produce a non-empty traces-get result flag-OFF")
	}

	withServeFromMMap(t, true)
	sOn := topologyServer(t, readerEmptiedRepo(t, doc, r))
	var got string
	noHang(t, 5*time.Second, "handleTracesGet", func() {
		got = callWithArgs(t, sOn, sOn.handleTracesGet, args)
	})
	if got != want {
		t.Fatalf("traces get flag-ON(emptied Doc) != flag-OFF\n got=%s\nwant=%s", got, want)
	}
}
