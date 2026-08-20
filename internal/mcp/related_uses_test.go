// related_uses_test.go — grafel_related direction=uses/used_by must traverse
// the usage edge kinds it names, not only NAVIGATES_TO (#6314).
//
// Reported by @manuel1358000 as "direction=uses misses property/enum-member
// access". The underlying defect is wider: the route pinned the traversal to
// the single kind NAVIGATES_TO, so USES / USES_HOOK / REFERENCES / CALLS edges
// that genuinely exist in the model were unreachable from a parameter named
// "uses" — a silently-incomplete answer with nothing in the response to say so.
package mcp

import (
	"context"
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
	mcpapi "github.com/mark3labs/mcp-go/mcp"
)

// buildUsageDoc builds a fixture carrying one edge of every usage kind plus a
// NAVIGATES_TO edge (which must keep working) and one non-usage kind
// (CONTAINS) that must NOT be reported.
//
//	caller --CALLS-------> target
//	caller --USES--------> target
//	caller --REFERENCES--> enumMember   (the reporter's property/enum symptom)
//	caller --USES_HOOK---> hook
//	caller --NAVIGATES_TO-> route:/foo  (pre-existing behaviour)
//	pkg    --CONTAINS----> caller       (must never surface as "uses")
func buildUsageDoc() *graph.Document {
	doc := &graph.Document{Repo: "use-repo"}
	doc.Entities = []graph.Entity{
		{ID: "caller", Name: "Caller", Kind: "function", SourceFile: "caller.ts", StartLine: 1},
		{ID: "target", Name: "Target", Kind: "function", SourceFile: "target.ts", StartLine: 2},
		{ID: "enumMember", Name: "Status.Active", Kind: "enum_member", SourceFile: "status.ts", StartLine: 3},
		{ID: "hook", Name: "useThing", Kind: "function", SourceFile: "hook.ts", StartLine: 4},
		{ID: "pkg", Name: "pkg", Kind: "module", SourceFile: "index.ts", StartLine: 5},
	}
	doc.Relationships = []graph.Relationship{
		{ID: "u1", FromID: "caller", ToID: "target", Kind: "CALLS"},
		{ID: "u2", FromID: "caller", ToID: "target", Kind: "USES"},
		{ID: "u3", FromID: "caller", ToID: "enumMember", Kind: "REFERENCES"},
		{ID: "u4", FromID: "caller", ToID: "hook", Kind: "USES_HOOK"},
		graph.Relationship{
			ID: "u5", FromID: "caller", ToID: "route:/foo", Kind: "NAVIGATES_TO",
		}.WithProperties(map[string]string{"route": "/foo", "line": "9"}),
		{ID: "u6", FromID: "pkg", ToID: "caller", Kind: "CONTAINS"},
	}
	return doc
}

// callRelated invokes handleCoreRelated (the grafel_related entry point) and
// decodes the JSON payload.
func callRelated(t *testing.T, srv *Server, args map[string]any) map[string]any {
	t.Helper()
	req := mcpapi.CallToolRequest{}
	req.Params.Arguments = args
	res, err := srv.handleCoreRelated(context.Background(), req)
	if err != nil {
		t.Fatalf("handleCoreRelated error: %v", err)
	}
	if res == nil {
		t.Fatal("nil result")
	}
	if res.IsError {
		t.Fatalf("tool returned error: %v", res.Content)
	}
	return extractResultJSON(t, res)
}

// kindsInEdges collects the reported "kind" of every returned edge.
func kindsInEdges(t *testing.T, result map[string]any) map[string]int {
	t.Helper()
	got := map[string]int{}
	for _, e := range edgesFromResult(t, result) {
		k, _ := e["kind"].(string)
		got[k]++
	}
	return got
}

// TestRelatedUses_TraversesUsageEdgeKinds is the #6314 regression:
// direction=uses from an entity must surface its CALLS / USES / USES_HOOK /
// REFERENCES edges alongside NAVIGATES_TO.
func TestRelatedUses_TraversesUsageEdgeKinds(t *testing.T) {
	srv := newTestServer(t, buildUsageDoc())
	result := callRelated(t, srv, map[string]any{
		"direction": "uses",
		"entity_id": "use-repo::caller",
		"group":     "test",
	})

	got := kindsInEdges(t, result)
	for _, want := range []string{"CALLS", "USES", "USES_HOOK", "REFERENCES", "NAVIGATES_TO"} {
		if got[want] == 0 {
			t.Errorf("direction=uses did not surface a %s edge; kinds returned: %v", want, got)
		}
	}
	if got["CONTAINS"] != 0 {
		t.Errorf("direction=uses surfaced a CONTAINS edge, which is containment not usage: %v", got)
	}
}

// TestRelatedUsedBy_TraversesUsageEdgeKinds is the inbound half: asking what
// USES the enum member must find the REFERENCES edge pointing at it.
func TestRelatedUsedBy_TraversesUsageEdgeKinds(t *testing.T) {
	srv := newTestServer(t, buildUsageDoc())
	result := callRelated(t, srv, map[string]any{
		"direction": "used_by",
		"entity_id": "use-repo::enumMember",
		"group":     "test",
	})

	edges := edgesFromResult(t, result)
	if len(edges) != 1 {
		t.Fatalf("direction=used_by on the enum member: got %d edges, want 1: %v", len(edges), edges)
	}
	if k, _ := edges[0]["kind"].(string); k != "REFERENCES" {
		t.Errorf("expected the REFERENCES edge, got kind=%q", k)
	}
	if from, _ := edges[0]["from_id"].(string); from != "use-repo::caller" {
		t.Errorf("expected from_id=use-repo::caller, got %q", from)
	}
}

// TestNavigatesTool_StaysNavigationOnly guards the blast radius: widening
// grafel_related must not turn the dedicated grafel_navigates tool into a
// generic usage-edge dump.
func TestNavigatesTool_StaysNavigationOnly(t *testing.T) {
	srv := newTestServer(t, buildUsageDoc())
	result := callNavTool(t, srv, map[string]any{
		"group":     "test",
		"direction": "outgoing",
	})

	got := kindsInEdges(t, result)
	if got["NAVIGATES_TO"] != 1 {
		t.Errorf("grafel_navigates should return the single NAVIGATES_TO edge, got %v", got)
	}
	for k, n := range got {
		if k != "NAVIGATES_TO" {
			t.Errorf("grafel_navigates leaked %d %s edge(s); it is navigation-only", n, k)
		}
	}
}

// buildRendersDoc adds the JS/TS component-uses-component edge to the picture.
// RENDERS is emitted by the javascript, angular, svelte, vue, astro, rescript
// and fsharp-elmish extractors — it is *the* "this component uses that
// component" edge in the corpus the reporter works in, so a used_by query on a
// child component that omitted it would return the same confidently-empty
// answer #6314 is about, one kind over.
//
//	page --RENDERS--> summary
func buildRendersDoc() *graph.Document {
	doc := &graph.Document{Repo: "ui-repo"}
	doc.Entities = []graph.Entity{
		{ID: "page", Name: "Page", Kind: "component", SourceFile: "Page.tsx", StartLine: 1},
		{ID: "summary", Name: "Summary", Kind: "component", SourceFile: "Summary.tsx", StartLine: 2},
	}
	doc.Relationships = []graph.Relationship{
		{ID: "r1", FromID: "page", ToID: "summary", Kind: "RENDERS"},
	}
	return doc
}

// TestRelatedUsedBy_SurfacesRendersEdge pins RENDERS in the usage kind set:
// asking what uses a child component must name the parent that renders it.
func TestRelatedUsedBy_SurfacesRendersEdge(t *testing.T) {
	srv := newTestServer(t, buildRendersDoc())
	result := callRelated(t, srv, map[string]any{
		"direction": "used_by",
		"entity_id": "ui-repo::summary",
		"group":     "test",
	})

	edges := edgesFromResult(t, result)
	if len(edges) != 1 {
		t.Fatalf("used_by on a rendered component: got %d edges, want 1 (the RENDERS parent): %v", len(edges), edges)
	}
	if k, _ := edges[0]["kind"].(string); k != "RENDERS" {
		t.Errorf("expected the RENDERS edge, got kind=%q", k)
	}
	if from, _ := edges[0]["from_id"].(string); from != "ui-repo::page" {
		t.Errorf("expected from_id=ui-repo::page, got %q", from)
	}
}

// TestNavigatesIncoming_MatchesBareRouteID pins the *bare* half of the
// incoming entity_id compare. Route destinations carry a synthetic bare ID
// ("route:/foo") that is never prefixed, so dropping the bare compare would
// silently break `grafel_navigates direction=incoming entity_id=/route` and
// the navigation_route fallback documented in docs/mcp-tools.md.
func TestNavigatesIncoming_MatchesBareRouteID(t *testing.T) {
	srv := newTestServer(t, buildUsageDoc())
	result := callNavTool(t, srv, map[string]any{
		"group":     "test",
		"direction": "incoming",
		"entity_id": "route:/foo",
	})

	edges := edgesFromResult(t, result)
	if len(edges) != 1 {
		t.Fatalf("incoming on the bare route ID: got %d edges, want 1: %v", len(edges), edges)
	}
	if k, _ := edges[0]["kind"].(string); k != "NAVIGATES_TO" {
		t.Errorf("expected NAVIGATES_TO, got kind=%q", k)
	}
	if from, _ := edges[0]["from_id"].(string); from != "use-repo::caller" {
		t.Errorf("expected from_id=use-repo::caller, got %q", from)
	}
}

// buildForeignBareIDDoc is a second repo whose *bare* relationship target
// literally spells another repo's prefixed ID. The bare compare is
// repo-agnostic, so without a guard it would match a caller's prefixed
// entity_id and union a foreign repo's edge into the answer.
func buildForeignBareIDDoc() *graph.Document {
	doc := &graph.Document{Repo: "other-repo"}
	doc.Entities = []graph.Entity{
		{ID: "foreignCaller", Name: "ForeignCaller", Kind: "function", SourceFile: "other.ts", StartLine: 1},
		{ID: "use-repo::target", Name: "Impostor", Kind: "function", SourceFile: "other.ts", StartLine: 2},
	}
	doc.Relationships = []graph.Relationship{
		{ID: "f1", FromID: "foreignCaller", ToID: "use-repo::target", Kind: "CALLS"},
	}
	return doc
}

// TestRelatedUsedBy_PrefixedIDSkipsBareCompare: when the caller supplies a
// prefixed "repo::id", only the prefixed form may match. The bare compare
// carries no repo, so leaving it live for a prefixed entity_id lets a foreign
// repo's edge in — the code comment asserts callers hold the prefixed form but
// nothing enforced it.
func TestRelatedUsedBy_PrefixedIDSkipsBareCompare(t *testing.T) {
	srv := newTestServer(t, buildUsageDoc(), buildForeignBareIDDoc())
	result := callRelated(t, srv, map[string]any{
		"direction": "used_by",
		"entity_id": "use-repo::target",
		"group":     "test",
	})

	for _, e := range edgesFromResult(t, result) {
		if repo, _ := e["from_repo"].(string); repo != "use-repo" {
			t.Errorf("used_by on a prefixed ID leaked an edge from %q: %v", repo, e)
		}
	}
}

// TestRelatedUses_FlowModeCarriesEdgeKind pins the per-edge "kind" on the
// *flow* path. mode is reachable on grafel_related (undeclared but live), so a
// caller can get flow-mode edges; a constant kind there would mislabel every
// one of them and no list-mode test would notice.
func TestRelatedUses_FlowModeCarriesEdgeKind(t *testing.T) {
	srv := newTestServer(t, buildUsageDoc())
	result := callRelated(t, srv, map[string]any{
		"direction": "uses",
		"entity_id": "use-repo::caller",
		"mode":      "flow",
		"group":     "test",
	})

	if m, _ := result["mode"].(string); m != "flow" {
		t.Fatalf("expected mode=flow in the response, got %q", m)
	}
	got := kindsInEdges(t, result)
	for _, want := range []string{"CALLS", "USES", "USES_HOOK", "REFERENCES", "NAVIGATES_TO"} {
		if got[want] == 0 {
			t.Errorf("mode=flow did not report a %s edge kind; kinds returned: %v", want, got)
		}
	}
}
