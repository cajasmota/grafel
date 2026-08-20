package mcp

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	mcpapi "github.com/mark3labs/mcp-go/mcp"
)

// core_merges_test.go — dispatch tests for the CORE-cluster canonical tools
// (#5546/#5549). Each test asserts that a discriminator value on the new
// canonical handler produces byte-identical output to the absorbed handler it
// routes to. We compare the bare handlers (not the wrapped tools) because the
// wrap() middleware injects a nondeterministic elapsed_ms trailer.

// coreTestServer builds a one-group/one-repo server over the standard fixture.
func coreTestServer(t *testing.T) *Server {
	t.Helper()
	dir := t.TempDir()
	repo := filepath.Join(dir, "r1")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	writeGraph(t, repo, fixtureDoc("r1"))
	regPath := makeRegistry(t, dir, map[string]map[string]string{"g": {"r1": repo}})
	srv, err := NewServer(Config{RegistryPath: regPath})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	t.Cleanup(srv.Close)
	return srv
}

// callBare invokes a bare *Server handler with the given args (no wrap()).
func callBare(t *testing.T, fn func(context.Context, mcpapi.CallToolRequest) (*mcpapi.CallToolResult, error), args map[string]any) string {
	t.Helper()
	req := mcpapi.CallToolRequest{}
	req.Params.Arguments = args
	res, err := fn(context.Background(), req)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	return resultText(res)
}

// normalizeForCompare makes a tool payload order-insensitive AND
// volatile-field-insensitive for comparison. Several absorbed handlers
// (handleGetNeighbors, handleQueryGraph, …) build their result from map
// iteration and emit rows in nondeterministic order; two independent calls
// return the same SET shuffled. We parse the payload and, if it is a JSON
// array, sort its elements by their canonical serialization so the dispatch
// comparison checks content equivalence, not row order. Before comparing, any
// "ts" field is stripped recursively (see stripVolatileFields) — some
// handlers (e.g. handleFeedbackEvent, handleMetaEvent) stamp
// time.Now().UTC().Format(time.RFC3339) independently on each call, and
// RFC3339's 1-second resolution means a second-boundary crossing between the
// canonical and absorbed-handler calls in assertSameDispatch would otherwise
// make an identical payload compare unequal (TestMetaEventDispatch flake).
// Non-JSON payloads are returned unchanged.
func normalizeForCompare(s string) string {
	var v any
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		return s // not JSON — compare verbatim
	}
	v = stripVolatileFields(v)

	if arr, ok := v.([]any); ok {
		keys := make([]string, len(arr))
		for i, e := range arr {
			b, err := json.Marshal(e)
			if err != nil {
				return s
			}
			keys[i] = string(b)
		}
		sort.Strings(keys)
		out, err := json.Marshal(keys)
		if err != nil {
			return s
		}
		return string(out)
	}

	out, err := json.Marshal(v)
	if err != nil {
		return s
	}
	return string(out)
}

// stripVolatileFields recursively removes any object key named "ts" — a
// timestamp stamped independently by each of the two calls
// assertSameDispatch makes, which legitimately differs run-to-run (or across
// a second boundary) without indicating a real dispatch mismatch. All other
// fields are preserved unchanged.
func stripVolatileFields(v any) any {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, val := range t {
			if k == "ts" {
				continue
			}
			out[k] = stripVolatileFields(val)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, e := range t {
			out[i] = stripVolatileFields(e)
		}
		return out
	default:
		return v
	}
}

// assertSameDispatch asserts the canonical dispatcher (with discriminator args)
// produces the same payload as the absorbed handler (with equivalent args),
// modulo row order (see normalizeForCompare).
func assertSameDispatch(t *testing.T, label string,
	canonical func(context.Context, mcpapi.CallToolRequest) (*mcpapi.CallToolResult, error), canonArgs map[string]any,
	old func(context.Context, mcpapi.CallToolRequest) (*mcpapi.CallToolResult, error), oldArgs map[string]any) {
	t.Helper()
	got := normalizeForCompare(callBare(t, canonical, canonArgs))
	want := normalizeForCompare(callBare(t, old, oldArgs))
	if got != want {
		t.Errorf("%s: canonical dispatch differs from absorbed handler\n got=%s\nwant=%s", label, got, want)
	}
}

// 1. grafel_orient view= → orient/whoami/clusters/topology/modules/stats.
func TestCoreOrientDispatch(t *testing.T) {
	srv := coreTestServer(t)
	base := map[string]any{"group": "g"}
	with := func(extra map[string]any) map[string]any {
		m := map[string]any{"group": "g"}
		for k, v := range extra {
			m[k] = v
		}
		return m
	}
	assertSameDispatch(t, "view=overview", srv.handleCoreOrient, with(map[string]any{"view": "overview"}), srv.handleOrient, base)
	assertSameDispatch(t, "view=default", srv.handleCoreOrient, base, srv.handleOrient, base)
	assertSameDispatch(t, "view=me", srv.handleCoreOrient, with(map[string]any{"view": "me"}), srv.handleWhoami, base)
	assertSameDispatch(t, "view=clusters", srv.handleCoreOrient, with(map[string]any{"view": "clusters"}), srv.handleListCommunities, base)
	assertSameDispatch(t, "view=modules", srv.handleCoreOrient, with(map[string]any{"view": "modules"}), srv.handleModuleAnalysis, base)
	assertSameDispatch(t, "view=stats", srv.handleCoreOrient, with(map[string]any{"view": "stats"}), srv.handleGraphStats, base)
	// topology: dispatcher defaults action=channels (#5781); compare with the
	// same explicit action against the absorbed handler.
	assertSameDispatch(t, "view=topology", srv.handleCoreOrient,
		with(map[string]any{"view": "topology"}),
		srv.handleTopology, with(map[string]any{"action": "channels"}))
}

// 2. grafel_find search= → query_graph (bm25) / search_entities (substring).
// Use the corpus-unique term "rareUniqueWidget" so BM25 returns a single
// unambiguous top hit — avoids tie-ordering flakiness when comparing two
// independent BM25 runs (rows with equal score may shuffle between calls).
func TestCoreFindDispatch(t *testing.T) {
	srv := coreTestServer(t)
	q := map[string]any{"group": "g", "query": "rareUniqueWidget"}
	bm := map[string]any{"group": "g", "query": "rareUniqueWidget", "search": "bm25"}
	sub := map[string]any{"group": "g", "query": "rareUniqueWidget", "search": "substring"}
	assertSameDispatch(t, "search=bm25", srv.handleCoreFind, bm, srv.handleQueryGraph, q)
	assertSameDispatch(t, "search=default", srv.handleCoreFind, q, srv.handleQueryGraph, q)
	assertSameDispatch(t, "search=substring", srv.handleCoreFind, sub, srv.handleSearchEntities, q)
}

// 3. grafel_related direction= → callers/callees/neighbors/uses/used_by.
func TestCoreRelatedDispatch(t *testing.T) {
	srv := coreTestServer(t)
	ent := func(dir string) map[string]any {
		m := map[string]any{"group": "g", "entity_id": "r1::a2"}
		if dir != "" {
			m["direction"] = dir
		}
		return m
	}
	bare := map[string]any{"group": "g", "entity_id": "r1::a2"}
	assertSameDispatch(t, "direction=callers", srv.handleCoreRelated, ent("callers"), srv.handleFindCallers, bare)
	assertSameDispatch(t, "direction=default", srv.handleCoreRelated, bare, srv.handleFindCallers, bare)
	assertSameDispatch(t, "direction=callees", srv.handleCoreRelated, ent("callees"), srv.handleFindCallees, bare)
	// neighbors: dispatcher rewrites the inner direction to "both".
	assertSameDispatch(t, "direction=neighbors", srv.handleCoreRelated,
		ent("neighbors"), srv.handleNeighbors, map[string]any{"group": "g", "entity_id": "r1::a2", "direction": "both"})
	// uses/used_by route to the shared navigates traversal with the USAGE kind
	// set (#6314) and outgoing/incoming inner direction — not to
	// handleNavigates, which is pinned to NAVIGATES_TO for its own tool.
	usage := func(ctx context.Context, req mcpapi.CallToolRequest) (*mcpapi.CallToolResult, error) {
		return srv.handleNavigatesKinds(ctx, req, usageEdgeKinds)
	}
	assertSameDispatch(t, "direction=uses", srv.handleCoreRelated,
		ent("uses"), usage, map[string]any{"group": "g", "entity_id": "r1::a2", "direction": "outgoing"})
	assertSameDispatch(t, "direction=used_by", srv.handleCoreRelated,
		ent("used_by"), usage, map[string]any{"group": "g", "entity_id": "r1::a2", "direction": "incoming"})
}

// 4. grafel_subgraph mode= → subgraph (hops) / get_neighbors (expand).
func TestCoreSubgraphDispatch(t *testing.T) {
	srv := coreTestServer(t)
	ent := map[string]any{"group": "g", "entity_id": "r1::a2"}
	assertSameDispatch(t, "mode=hops", srv.handleCoreSubgraph, map[string]any{"group": "g", "entity_id": "r1::a2", "mode": "hops"}, srv.handleSubgraph, ent)
	assertSameDispatch(t, "mode=default", srv.handleCoreSubgraph, ent, srv.handleSubgraph, ent)
	assertSameDispatch(t, "mode=expand", srv.handleCoreSubgraph, map[string]any{"group": "g", "entity_id": "r1::a2", "mode": "expand"}, srv.handleGetNeighbors, ent)
}

// 4b. Regression for #5784 bug 4: unlike every other CORE canonical tool
// (trace kind=, related direction=, impact_radius scope=, find search=),
// handleCoreSubgraph's mode= switch has no validateDiscriminator call — a
// bogus mode= silently falls through the switch's default branch (the hops
// path) instead of erroring. mode=neighbors (the accepted alias for expand)
// must keep working post-fix.
func TestCoreSubgraphModeValidation(t *testing.T) {
	srv := coreTestServer(t)
	ent := map[string]any{"group": "g", "entity_id": "r1::a2"}

	bogus := map[string]any{"group": "g", "entity_id": "r1::a2", "mode": "bogus"}
	res, err := srv.handleCoreSubgraph(context.Background(), mcpapi.CallToolRequest{Params: mcpapi.CallToolParams{Arguments: bogus}})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if res == nil || !res.IsError {
		t.Fatalf("mode=bogus should return a validation error, got: %v", res)
	}
	msg := resultText(res)
	if !strings.Contains(msg, "hops") || !strings.Contains(msg, "expand") {
		t.Errorf("mode=bogus error should name the valid modes (hops, expand), got: %s", msg)
	}

	// mode=neighbors (the expand alias) must still route to handleGetNeighbors.
	assertSameDispatch(t, "mode=neighbors", srv.handleCoreSubgraph,
		map[string]any{"group": "g", "entity_id": "r1::a2", "mode": "neighbors"}, srv.handleGetNeighbors, ent)
}

// 5. grafel_trace kind= → path/data/control/def_use/effects/flows/process.
func TestCoreTraceDispatch(t *testing.T) {
	srv := coreTestServer(t)
	g := map[string]any{"group": "g"}
	ent := map[string]any{"group": "g", "entity_id": "r1::a2"}
	path := map[string]any{"group": "g", "source": "r1::a1", "target": "r1::a3"}
	assertSameDispatch(t, "kind=path", srv.handleCoreTrace, map[string]any{"group": "g", "kind": "path", "source": "r1::a1", "target": "r1::a3"}, srv.handleShortestPath, path)
	assertSameDispatch(t, "kind=default", srv.handleCoreTrace, path, srv.handleShortestPath, path)
	assertSameDispatch(t, "kind=data", srv.handleCoreTrace, map[string]any{"group": "g", "kind": "data"}, srv.handleDataFlows, g)
	assertSameDispatch(t, "kind=control", srv.handleCoreTrace, map[string]any{"group": "g", "kind": "control", "entity_id": "r1::a2"}, srv.handleControlFlow, ent)
	assertSameDispatch(t, "kind=def_use", srv.handleCoreTrace, map[string]any{"group": "g", "kind": "def_use"}, srv.handleDefUse, g)
	assertSameDispatch(t, "kind=effects", srv.handleCoreTrace, map[string]any{"group": "g", "kind": "effects", "entity_id": "r1::a2"}, srv.handleEffects, ent)
	// flows: dispatcher defaults action=dead_ends.
	assertSameDispatch(t, "kind=flows", srv.handleCoreTrace,
		map[string]any{"group": "g", "kind": "flows"}, srv.handleFlows, map[string]any{"group": "g", "action": "dead_ends"})
	// process: handleTraces defaults action=list internally.
	assertSameDispatch(t, "kind=process", srv.handleCoreTrace, map[string]any{"group": "g", "kind": "process"}, srv.handleTraces, g)
}

// 6. grafel_endpoints detail= → list/contract/posture.
func TestCoreEndpointsDispatch(t *testing.T) {
	srv := coreTestServer(t)
	// list: dispatcher defaults action=definitions.
	assertSameDispatch(t, "detail=list", srv.handleCoreEndpoints,
		map[string]any{"group": "g", "detail": "list"},
		srv.handleEndpoints, map[string]any{"group": "g", "action": "definitions"})
	assertSameDispatch(t, "detail=default", srv.handleCoreEndpoints,
		map[string]any{"group": "g"},
		srv.handleEndpoints, map[string]any{"group": "g", "action": "definitions"})
	// contract + posture operate on an entity_id.
	ent := map[string]any{"group": "g", "entity_id": "r1::a3"}
	assertSameDispatch(t, "detail=contract", srv.handleCoreEndpoints, map[string]any{"group": "g", "detail": "contract", "entity_id": "r1::a3"}, srv.handleEffectiveContract, ent)
	assertSameDispatch(t, "detail=posture", srv.handleCoreEndpoints, map[string]any{"group": "g", "detail": "posture", "entity_id": "r1::a3"}, srv.handleEndpointPosture, ent)
}

// TestCoreEndpointsContractRequiresEntityID asserts detail=contract fails
// with a clear, aspect-aware message when neither entity_id nor
// qualified_name is supplied — the #5784 pre-flight check mirroring
// grafel_diff's requireArgs pattern (core_merges.go handleCoreEndpoints).
func TestCoreEndpointsContractRequiresEntityID(t *testing.T) {
	srv := coreTestServer(t)
	req := mcpapi.CallToolRequest{}
	req.Params.Arguments = map[string]any{"group": "g", "detail": "contract"}
	res, err := srv.handleCoreEndpoints(context.Background(), req)
	if err != nil {
		t.Fatalf("handleCoreEndpoints: %v", err)
	}
	if !res.IsError {
		t.Fatalf("expected an error result when entity_id/qualified_name are both missing, got: %s", resultText(res))
	}
	if msg := resultText(res); !strings.Contains(msg, "entity_id") || !strings.Contains(msg, "qualified_name") {
		t.Errorf("error message should mention both entity_id and qualified_name, got: %s", msg)
	}

	// qualified_name alone should satisfy the pre-flight check (may still
	// fail downstream if it doesn't resolve, but must not hit our guard).
	req2 := mcpapi.CallToolRequest{}
	req2.Params.Arguments = map[string]any{"group": "g", "detail": "contract", "qualified_name": "r1.a3"}
	res2, err := srv.handleCoreEndpoints(context.Background(), req2)
	if err != nil {
		t.Fatalf("handleCoreEndpoints: %v", err)
	}
	if res2.IsError && strings.Contains(resultText(res2), "requires entity_id or qualified_name") {
		t.Errorf("qualified_name alone should satisfy the pre-flight check, got: %s", resultText(res2))
	}
}

// 7. grafel_impact_radius scope= → entity / changeset.
func TestCoreImpactRadiusDispatch(t *testing.T) {
	srv := coreTestServer(t)
	ent := map[string]any{"group": "g", "entity_id": "r1::a3"}
	assertSameDispatch(t, "scope=entity", srv.handleCoreImpactRadius, map[string]any{"group": "g", "scope": "entity", "entity_id": "r1::a3"}, srv.handleImpactRadius, ent)
	assertSameDispatch(t, "scope=default", srv.handleCoreImpactRadius, ent, srv.handleImpactRadius, ent)
	// changeset routes to handlePRImpact (which requires repo).
	pr := map[string]any{"group": "g", "repo": "r1"}
	assertSameDispatch(t, "scope=changeset", srv.handleCoreImpactRadius, map[string]any{"group": "g", "scope": "changeset", "repo": "r1"}, srv.handlePRImpact, pr)
}

// 8. All eight CORE canonical tools are registered (#5546/#5549).
func TestCoreCanonicalToolsRegistered(t *testing.T) {
	srv := coreTestServer(t)
	registered := map[string]bool{}
	for _, st := range srv.MCP.ListTools() {
		registered[st.Tool.Name] = true
	}
	for _, n := range []string{
		"grafel_orient", "grafel_find", "grafel_related", "grafel_find_paths",
		"grafel_subgraph", "grafel_trace", "grafel_endpoints", "grafel_impact_radius",
	} {
		if !registered[n] {
			t.Errorf("CORE canonical tool %q not registered", n)
		}
	}
}
