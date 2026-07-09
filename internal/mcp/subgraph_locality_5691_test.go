// subgraph_locality_5691_test.go — regression tests for #5691.
//
// Bug: grafel_subgraph mode=expand on a node ~2 hops from a high-degree Module
// hub (degree ~157k) returned ~157k neighbours dominated by unrelated
// cross-module/mixed-kind nodes — it inherited the hub's entire fan-out.
//
//   - bfsBounded expanded adj.out/adj.in in Go MAP-ITERATION order with NO
//     ranking; when it hit maxNodes it stopped arbitrarily, so a hub reached
//     mid-walk dumped its whole neighbourhood and truncation kept whatever
//     iterated first.
//   - subgraphMarkdown had NO maxNodes bound at all — a fully unbounded walk.
//
// Fix: locality-first ranked frontier + hub-aware stop-and-annotate in the
// bounded expansion, and give the markdown path the same maxNodes bound as raw.
package mcp

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
)

// buildHubRepo builds a repo where `root` sits in a small LOCAL cluster
// (same file/module) but is ALSO one hop from a high-degree hub whose fan-out
// would swamp the result if inherited.
//
//	root ──CALLS──▶ localA ──CALLS──▶ localB       (all in src/app/…)
//	root ──CALLS──▶ hub ──CALLS──▶ leaf0..leafN    (hub in src/hub, leaves in src/other)
func buildHubRepo(hubFanout int) *graph.Document {
	doc := &graph.Document{Repo: "app"}
	doc.Entities = []graph.Entity{
		{ID: "root", Name: "RootFn", Kind: "SCOPE.Function", SourceFile: "src/app/root.ts", StartLine: 1},
		{ID: "localA", Name: "LocalA", Kind: "SCOPE.Function", SourceFile: "src/app/util.ts", StartLine: 1},
		{ID: "localB", Name: "LocalB", Kind: "SCOPE.Function", SourceFile: "src/app/util.ts", StartLine: 10},
		{ID: "hub", Name: "PaymentHub", Kind: "SCOPE.Module", SourceFile: "src/hub/hub.ts", StartLine: 1},
	}
	doc.Relationships = []graph.Relationship{
		{ID: "r1", FromID: "root", ToID: "localA", Kind: "CALLS"},
		{ID: "r2", FromID: "localA", ToID: "localB", Kind: "CALLS"},
		{ID: "r3", FromID: "root", ToID: "hub", Kind: "CALLS"},
	}
	for i := 0; i < hubFanout; i++ {
		id := fmt.Sprintf("leaf%d", i)
		doc.Entities = append(doc.Entities, graph.Entity{
			ID: id, Name: id, Kind: "SCOPE.Function",
			SourceFile: fmt.Sprintf("src/other/leaf%d.ts", i), StartLine: 1,
		})
		doc.Relationships = append(doc.Relationships, graph.Relationship{
			ID: "rh" + id, FromID: "hub", ToID: id, Kind: "CALLS",
		})
	}
	return doc
}

// TestSubgraph_ExpandStopsAtHub_5691 is the core regression: expanding `root`
// at depth 2 must return the LOCAL cluster and NOT inherit the hub's fan-out;
// the hub crossing must be annotated so the caller can narrow.
//
// RED on current code: the unranked/uncapped walk expands the hub and returns
// all leaf* nodes with no hub annotation.
func TestSubgraph_ExpandStopsAtHub_5691(t *testing.T) {
	const fanout = 1200 // > the hub-degree cutoff, well under max_nodes=1500
	srv := newTestServer(t, buildHubRepo(fanout))

	out := callFlowTool(t, srv.handleSubgraph, map[string]any{
		"group":     "test",
		"entity_id": "root",
		"depth":     float64(2),
		"format":    "raw",
	})
	if out == nil {
		t.Fatal("expected JSON output for format=raw")
	}

	nodes, _ := out["nodes"].([]any)
	var names []string
	for _, n := range nodes {
		m, _ := n.(map[string]any)
		if m == nil {
			continue
		}
		names = append(names, fmt.Sprint(m["name"]))
	}
	joined := strings.Join(names, ",")

	// 1) The local cluster must survive.
	for _, want := range []string{"LocalA", "LocalB"} {
		if !strings.Contains(joined, want) {
			t.Errorf("local cluster node %q missing from expansion:\n%s", want, joined)
		}
	}
	// 2) The hub's fan-out must NOT be inherited. A handful is tolerable; the
	//    whole 1200-leaf neighbourhood is the bug.
	leafCount := strings.Count(joined, "leaf")
	if leafCount > 5 {
		t.Errorf("hub fan-out inherited: %d leaf* nodes in expansion (want ~0). "+
			"The bounded walk expanded the hub instead of stopping at it.\n%.400s",
			leafCount, joined)
	}
	// 3) The hub crossing must be annotated so the caller can narrow.
	note, _ := out["truncation_note"].(string)
	_, hasBoundaries := out["hub_boundaries"]
	if !hasBoundaries && !strings.Contains(strings.ToLower(note), "hub") {
		t.Errorf("hub crossing not annotated: expected hub_boundaries or a hub note, got:\n%#v / %q",
			out["hub_boundaries"], note)
	}
	if !strings.Contains(note+fmt.Sprint(out["hub_boundaries"]), "PaymentHub") {
		t.Errorf("hub annotation should name the hub (PaymentHub); got note=%q boundaries=%v",
			note, out["hub_boundaries"])
	}
}

// TestSubgraph_MarkdownRespectsMaxNodes_5691 asserts the markdown path is now
// bounded by max_nodes just like the raw path.
//
// RED on current code: subgraphMarkdown walks the whole neighbourhood and lists
// all 2000 callees regardless of max_nodes.
func TestSubgraph_MarkdownRespectsMaxNodes_5691(t *testing.T) {
	doc := &graph.Document{Repo: "app"}
	doc.Entities = []graph.Entity{
		{ID: "root", Name: "RootFn", Kind: "SCOPE.Function", SourceFile: "src/app/root.ts", StartLine: 1},
	}
	const fanout = 2000
	for i := 0; i < fanout; i++ {
		id := fmt.Sprintf("callee%d", i)
		doc.Entities = append(doc.Entities, graph.Entity{
			ID: id, Name: id, Kind: "SCOPE.Function",
			SourceFile: fmt.Sprintf("src/other/callee%d.ts", i), StartLine: 1,
		})
		doc.Relationships = append(doc.Relationships, graph.Relationship{
			ID: "rc" + id, FromID: "root", ToID: id, Kind: "CALLS",
		})
	}
	srv := newTestServer(t, doc)

	const cap = 50
	text := callFlowToolText(t, srv.handleSubgraph, map[string]any{
		"group":     "test",
		"entity_id": "root",
		"depth":     float64(1),
		"format":    "markdown",
		"max_nodes": float64(cap),
	})

	// Parse "## Calls (N entities within …)".
	re := regexp.MustCompile(`## Calls \((\d+) ent`)
	m := re.FindStringSubmatch(text)
	if m == nil {
		t.Fatalf("could not find Calls header in markdown:\n%.400s", text)
	}
	n, _ := strconv.Atoi(m[1])
	if n > cap {
		t.Fatalf("markdown callee count %d exceeds max_nodes cap %d — the markdown path is unbounded", n, cap)
	}
	// Sanity: it should still list a healthy chunk of the neighbourhood.
	if n == 0 {
		t.Fatalf("markdown listed no callees; expected up to %d", cap)
	}
}
