package mcp

// doc_nodes_4307_test.go — #4307 (Layer 1 of epic #4294): surface the ingested
// markdown documentation graph (SCOPE.MarkdownDocument + SCOPE.Section nodes, the
// CONTAINS hierarchy, and the Section--MENTIONS-->code edge) in the kind-agnostic
// MCP read tools.
//
// #4306 added internal/ingest, which (behind the opt-in --ingest-docs flag) emits
// those nodes/edges into the graph. The daemon does NOT auto-run ingestion, so
// these tests build the doc graph the same way the indexer would — by running the
// real ingest.Ingest pass over a fixture .md alongside a tiny code-entity set —
// and then exercise the MCP handlers directly.
//
// WHAT WAS ALREADY GENERIC (locked in here, no handler change needed):
//   - search_entities / find: kind-agnostic name+kind match, so they return the
//     Document and Section nodes (matchesKindFilter does a leaf compare, so
//     kind_filter="Section" matches "SCOPE.Section").
//   - neighbors(Section, out) / find_callees: the outbound BFS walks every
//     out-edge, so it already reached the MENTIONS target and labelled it.
//   - get_source(Section): keys off the entity's SourceFile + StartLine/EndLine,
//     which the ingest pass set to the .md path and the section's heading span —
//     so it quotes the section markdown with no Section-specific code.
//   - neighbors(expand) CONTAINS traversal: BFS follows CONTAINS like any edge.
//
// WHAT #4307 FIXED:
//   - MENTIONS was absent from semanticEdgeKinds, so the INBOUND projection
//     dropped it: find_callers / neighbors(direction=in) on a code entity did NOT
//     surface the documenting Section (isInboundNeighborKind gates on
//     isSemanticEdgeKind), and inspect did not list it under semantic_edges.
//     Adding MENTIONS to semanticEdgeKinds fixes all three at once.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/ingest"
	"github.com/cajasmota/grafel/internal/types"
	mcpapi "github.com/mark3labs/mcp-go/mcp"
)

// docFixtureMarkdown is a small markdown file whose "Order placement" section
// body mentions the OrderService.placeOrder code entity by exact name, so the
// ingest linker emits a Section--MENTIONS-->placeOrder edge.
const docFixtureMarkdown = `# Architecture Guide

Intro text.

## Order placement

The placeOrder operation validates the cart and persists the order.
See OrderService for the surrounding service.

### Edge cases

Empty carts are rejected.
`

// buildDocGraph runs the real ingest pass over docFixtureMarkdown placed under a
// temp repo dir, returns a graph.Document containing the code entities plus the
// emitted Document/Section nodes and CONTAINS/MENTIONS edges, and the entity IDs
// of interest. The Section/Document SourceFile fields are rewritten to ABSOLUTE
// paths so get_source can read the .md without depending on the test LoadedRepo's
// Path (mirrors the #4272 get_source test convention).
func buildDocGraph(t *testing.T) (doc *graph.Document, placeOrderID, sectionID, documentID string) {
	t.Helper()

	dir := t.TempDir()
	const rel = "docs/architecture.md"
	abs := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(abs, []byte(docFixtureMarkdown), 0o644); err != nil {
		t.Fatalf("write md: %v", err)
	}

	// Code entities the doc can mention. placeOrder is the exact-match target.
	code := []graph.Entity{
		{
			ID: "ent_place_order", Name: "placeOrder",
			QualifiedName: "core.order.OrderService.placeOrder",
			Kind:          "SCOPE.Operation", SourceFile: "order.go", StartLine: 10, EndLine: 20,
		},
		{
			ID: "ent_order_service", Name: "OrderService",
			QualifiedName: "core.order.OrderService",
			Kind:          "SCOPE.Component", SourceFile: "order.go", StartLine: 1, EndLine: 40,
		},
	}

	res := ingest.Ingest(dir, "repo1", []string{rel}, code)
	if res.Documents != 1 {
		t.Fatalf("expected 1 Document, got %d", res.Documents)
	}
	if res.Sections < 2 {
		t.Fatalf("expected >=2 Sections, got %d", res.Sections)
	}
	if res.Mentions < 1 {
		t.Fatalf("ingest emitted no MENTIONS edge (fixture/linker drift): %+v", res)
	}

	// Rewrite doc-node SourceFile to absolute so get_source reads the temp .md.
	entities := append([]graph.Entity(nil), code...)
	for _, e := range res.Entities {
		if e.SourceFile == rel {
			e.SourceFile = abs
		}
		entities = append(entities, e)
		switch e.Kind {
		case string(types.EntityKindMarkdownDocument):
			documentID = e.ID
		case string(types.EntityKindSection):
			// The "Order placement" section is the one that MENTIONS placeOrder.
			if strings.Contains(e.PropGet("heading"), "Order placement") {
				sectionID = e.ID
			}
		}
	}
	if documentID == "" || sectionID == "" {
		t.Fatalf("did not locate Document/Section ids (doc=%q sec=%q)", documentID, sectionID)
	}

	doc = &graph.Document{Repo: "repo1", Entities: entities, Relationships: res.Relationships}
	return doc, "ent_place_order", sectionID, documentID
}

// --- search / find -----------------------------------------------------------

// TestSearchEntities_ReturnsDocNodes_4307 asserts search_entities surfaces the
// Document and Section nodes by name, with kind_filter accepting the short leaf
// form ("Section" → "SCOPE.Section"). This already worked generically; the test
// locks it in.
func TestSearchEntities_ReturnsDocNodes_4307(t *testing.T) {
	doc, _, _, _ := buildDocGraph(t)
	srv := newTestServer(t, doc)

	// Section by leaf kind filter.
	out := callSearchEntities(t, srv, map[string]any{
		"group": "test", "query": "Order placement", "kind_filter": "Section",
	})
	if !searchHasKind(out, "SCOPE.Section", "Order placement") {
		t.Fatalf("search_entities did not return the Section: %v", out["results"])
	}

	// Document by leaf kind filter.
	out2 := callSearchEntities(t, srv, map[string]any{
		"group": "test", "query": "architecture", "kind_filter": "MarkdownDocument",
	})
	if !searchHasKind(out2, "SCOPE.MarkdownDocument", "architecture.md") {
		t.Fatalf("search_entities did not return the Document: %v", out2["results"])
	}
}

// --- inspect ------------------------------------------------------------------

// TestInspect_SurfacesMentions_BothSides_4307 asserts inspect lists the MENTIONS
// edge under semantic_edges from BOTH endpoints: outbound from the Section, and
// inbound on the code entity. The inbound projection is the #4307 fix (MENTIONS
// was missing from semanticEdgeKinds).
func TestInspect_SurfacesMentions_BothSides_4307(t *testing.T) {
	doc, placeOrderID, sectionID, _ := buildDocGraph(t)
	srv := newTestServer(t, doc)

	// Section side: outbound MENTIONS to the code entity.
	secOut := callInspectWithArgs(t, srv, map[string]any{"group": "test", "entity_id": sectionID})
	if !semanticEdgePresent(secOut, "MENTIONS", "outbound", placeOrderID) {
		t.Fatalf("inspect(Section) missing outbound MENTIONS->placeOrder; semantic_edges=%v keys=%v",
			secOut["semantic_edges"], mapKeys(secOut))
	}

	// Code side: inbound MENTIONS from the Section (the fixed direction).
	codeOut := callInspectWithArgs(t, srv, map[string]any{"group": "test", "entity_id": placeOrderID})
	if !semanticEdgePresent(codeOut, "MENTIONS", "inbound", sectionID) {
		t.Fatalf("inspect(code) missing inbound MENTIONS<-Section (the #4307 fix); semantic_edges=%v keys=%v",
			codeOut["semantic_edges"], mapKeys(codeOut))
	}
}

// --- neighbors (find_callers / find_callees) ----------------------------------

// TestNeighbors_InboundMentions_4307 is the headline assertion: neighbors(code,
// direction=in) / find_callers returns the documenting Section labelled
// edge_kind=MENTIONS. FAILS on pre-fix code (isInboundNeighborKind rejected
// MENTIONS, so the Section was dropped). This is what makes "documented in
// <Section>" answerable from the code entity.
func TestNeighbors_InboundMentions_4307(t *testing.T) {
	doc, placeOrderID, sectionID, _ := buildDocGraph(t)
	srv := newTestServer(t, doc)

	out := callFlowTool(t, srv.handleFindCallers, map[string]any{
		"entity_id": placeOrderID, "depth": float64(1),
	})
	callers, ok := out["callers"].([]any)
	if !ok {
		t.Fatalf("expected callers array, got %T", out["callers"])
	}
	row := findNeighborByID(callers, "id", sectionID)
	if row == nil {
		t.Fatalf("inbound MENTIONS Section missing from callers (the #4307 bug): %v", callers)
	}
	if row["edge_kind"] != "MENTIONS" {
		t.Errorf("expected edge_kind=MENTIONS on the documenting Section, got %v", row["edge_kind"])
	}
}

// TestNeighbors_OutboundMentions_4307 asserts the symmetric out side:
// neighbors(Section, direction=out) / find_callees returns the code entity it
// MENTIONS, labelled edge_kind=MENTIONS. The outbound BFS already traversed every
// out-edge; this locks in the label.
func TestNeighbors_OutboundMentions_4307(t *testing.T) {
	doc, placeOrderID, sectionID, _ := buildDocGraph(t)
	srv := newTestServer(t, doc)

	out := callFlowTool(t, srv.handleFindCallees, map[string]any{
		"entity_id": sectionID, "depth": float64(1),
	})
	callees, ok := out["callees"].([]any)
	if !ok {
		t.Fatalf("expected callees array, got %T", out["callees"])
	}
	row := findNeighborByID(callees, "id", placeOrderID)
	if row == nil {
		t.Fatalf("outbound MENTIONS target placeOrder missing from callees: %v", callees)
	}
	if row["edge_kind"] != "MENTIONS" {
		t.Errorf("expected edge_kind=MENTIONS on the mentioned code entity, got %v", row["edge_kind"])
	}
}

// TestNeighbors_ContainsHierarchyTraverses_4307 asserts the Document→Section
// CONTAINS hierarchy traverses via the generic expand/neighbors BFS, annotated
// with semantic_kind only where applicable (CONTAINS stays unlabelled structural
// scaffolding, but the Section is still reachable). Proves goal #4.
func TestNeighbors_ContainsHierarchyTraverses_4307(t *testing.T) {
	doc, _, sectionID, documentID := buildDocGraph(t)
	srv := newTestServer(t, doc)

	rows := callNeighbors(t, srv, map[string]any{
		"group": "test", "entity_id": documentID, "depth": float64(2),
	})
	if findNeighborByID(rows, "id", sectionID) == nil {
		t.Fatalf("Document's CONTAINS-child Section not reachable via neighbors: %v", rows)
	}
}

// --- get_source ---------------------------------------------------------------

// TestGetSource_SectionQuotesMarkdownSpan_4307 asserts get_source on a Section
// returns the markdown of that section's heading span (StartLine..EndLine the
// ingest pass recorded) — including nested subsection text, per the #4306 span
// contract — and NOT the document preamble above the heading. This already worked
// through the generic SourceFile+span path; the test locks it in.
func TestGetSource_SectionQuotesMarkdownSpan_4307(t *testing.T) {
	doc, _, sectionID, _ := buildDocGraph(t)
	srv := newTestServer(t, doc)

	req := mcpapi.CallToolRequest{}
	req.Params.Arguments = map[string]any{"group": "test", "entity_id": sectionID, "context_lines": float64(0)}
	res, err := srv.handleGetNodeSource(context.Background(), req)
	if err != nil {
		t.Fatalf("get_source error: %v", err)
	}
	if res.IsError {
		t.Fatalf("get_source tool error: %s", extractResultText(t, res))
	}
	text := extractResultText(t, res)

	if !strings.Contains(text, "## Order placement") {
		t.Errorf("Section span missing its own heading:\n%s", text)
	}
	if !strings.Contains(text, "placeOrder operation validates") {
		t.Errorf("Section span missing its body text:\n%s", text)
	}
	// The hierarchical span includes the nested "### Edge cases" subsection.
	if !strings.Contains(text, "Edge cases") {
		t.Errorf("Section span should include the nested subsection (#4306 span contract):\n%s", text)
	}
	// It must NOT include the document preamble above the heading.
	if strings.Contains(text, "# Architecture Guide") {
		t.Errorf("Section span leaked the document preamble:\n%s", text)
	}
}

// --- helpers ------------------------------------------------------------------

func callSearchEntities(t *testing.T, srv *Server, args map[string]any) map[string]any {
	t.Helper()
	req := mcpapi.CallToolRequest{}
	req.Params.Arguments = args
	res, err := srv.handleSearchEntities(context.Background(), req)
	if err != nil {
		t.Fatalf("handleSearchEntities error: %v", err)
	}
	if res == nil || res.IsError {
		t.Fatalf("search_entities tool error: %v", res)
	}
	return extractResultJSON(t, res)
}

// searchHasKind reports whether the search_entities result contains an item with
// the given kind whose name contains nameSub.
func searchHasKind(out map[string]any, kind, nameSub string) bool {
	results, _ := out["results"].([]any)
	for _, r := range results {
		m, ok := r.(map[string]any)
		if !ok {
			continue
		}
		if m["kind"] == kind && strings.Contains(asString(m["name"]), nameSub) {
			return true
		}
	}
	return false
}

// semanticEdgePresent reports whether the inspect envelope's semantic_edges
// section contains an edge of the given kind/direction whose `other` endpoint is
// otherID (the test runs single-repo so `other` is the bare local id).
func semanticEdgePresent(out map[string]any, kind, direction, otherID string) bool {
	edges, _ := out["semantic_edges"].([]any)
	for _, e := range edges {
		m, ok := e.(map[string]any)
		if !ok {
			continue
		}
		if m["kind"] == kind && m["direction"] == direction && m["other"] == otherID {
			return true
		}
	}
	return false
}

// findNeighborByID returns the row whose key field equals id (tolerating a
// "<repo>::" prefix on the row value — neighbors/find_callers emit prefixed ids
// while the test holds the bare local id), or nil.
func findNeighborByID(list []any, key, id string) map[string]any {
	for _, item := range list {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		v := asString(m[key])
		if v == id || strings.HasSuffix(v, "::"+id) {
			return m
		}
	}
	return nil
}

func asString(v any) string {
	s, _ := v.(string)
	return s
}
