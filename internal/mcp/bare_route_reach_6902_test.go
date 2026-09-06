package mcp

import (
	"context"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/coverage"
	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/types"
	mcpapi "github.com/mark3labs/mcp-go/mcp"
)

// ---------------------------------------------------------------------------
// #6902 — two MCP sites drop bare "Route" (types.EntityKindRouteBare).
//
//  1. reachability_tools.go isEndpointKind — a switch on the RAW kind whose
//     body is BYTE-IDENTICAL to the dashboard's isEndpointReachKind
//     (handlers_coverage_reach.go), which the dashboard's own comment says is
//     a deliberate mirror. Both accept SCOPE.Endpoint, SCOPE.Route and
//     http_endpoint_definition, and neither accepts bare "Route".
//
//  2. dead_code.go frameworkEntryKindsMCP — the framework entry-point SEED set,
//     documented as a mirror of internal/links' frameworkEntryKinds. It holds
//     "SCOPE.Route" and not "Route", so a bare-"Route" entity is not seeded and
//     is reported as DEAD CODE — the most user-visible form of this defect,
//     since the graph's HTTP entrypoints are exactly what must never be dead.
//
// Both Route spellings mean the same concept (an HTTP route; #6894 §1b,
// #6776 arm B7), so both belong at both sites. The fix is a widening.
//
// POSITIVE CONTROLS, checked against the #6893 trap — a control another clause
// already matches grades nothing:
//
//	site 1, isEndpointKind: the three arms are exact string comparisons
//	  against SCOPE.Endpoint / SCOPE.Route / http_endpoint_definition. Bare
//	  "Route" equals none of them.
//	site 2, computeDeadCodeLive: seeding is frameworkEntryKindsMCP[e.Kind] ||
//	  isFrameworkOrHandler(e), plus HANDLES/HANDLES_SIGNAL/NAVIGATES_TO/
//	  ROUTES_TO/REGISTERS edge targets. The control below has NO inbound edge,
//	  and isFrameworkOrHandler cannot match it: its name "/orders-6902" fails
//	  routeNameRe (^(get|post|…)[\s:]), is not a lifecycle name, is not a
//	  constructor (no "X.X"), does not start with "on"+upper or "handle", and
//	  mcp's isHTTPEndpointKind classifies "Route" as endpointKindNone.
//
// ---------------------------------------------------------------------------

const (
	bareRouteName6902  = "/orders-6902"
	scopeRouteName6902 = "/invoices-6902"
	decoyName6902      = "LedgerReconciler6902"
	vacuityName6902    = "GET /health-6902"
)

// reach6902Doc builds one repo whose entities carry the #5061 reachability
// props. All four are unreachable so each one the predicate admits shows up
// in the emitted endpoints listing.
func reach6902Doc() *graph.Document {
	mk := func(id, name, kind, file string) graph.Entity {
		return graph.Entity{ID: id, Name: name, Kind: kind, SourceFile: file, StartLine: 3}.
			WithProperties(map[string]string{coverage.PropTestReachable: "false"})
	}
	return &graph.Document{
		Repo: "svc",
		Entities: []graph.Entity{
			mk("bare-route-6902", bareRouteName6902, string(types.EntityKindRouteBare), "src/routes.go"),
			mk("scope-route-6902", scopeRouteName6902, string(types.EntityKindRoute), "src/routes.go"),
			mk("decoy-6902", decoyName6902, string(types.EntityKindClass), "src/ledger.go"),
			mk("real-http-6902", vacuityName6902, "http_endpoint_definition", "src/health.go"),
		},
	}
}

func callReach6902(t *testing.T, srv *Server, args map[string]any) string {
	t.Helper()
	args["group"] = "test"
	req := mcpapi.CallToolRequest{}
	req.Params.Arguments = args
	res, err := srv.handleTestReachability(context.Background(), req)
	if err != nil {
		t.Fatalf("handleTestReachability: %v", err)
	}
	return resultText(res)
}

// Site 1 — grafel_test_reachability's endpoints listing and endpoint roll-up.
func TestTestReachability_AcceptsBareRouteKind_6902(t *testing.T) {
	srv := newTestServer(t, reach6902Doc())
	out := callReach6902(t, srv, map[string]any{"endpoints_only": true})

	// 1. vacuity guard.
	if !strings.Contains(out, vacuityName6902) {
		t.Fatalf("endpoints listing lacks the plain http_endpoint_definition %q; "+
			"every assertion below would be vacuous. Got:\n%s", vacuityName6902, out)
	}
	// 2. the #6902 recall direction.
	if !strings.Contains(out, bareRouteName6902) {
		t.Errorf("endpoints listing lacks the bare-\"Route\" entity %q. "+
			"isEndpointKind has no types.EntityKindRouteBare arm, so every "+
			"Java/Spring/Django route entity is invisible to this tool. Got:\n%s",
			bareRouteName6902, out)
	}
	// 3. SCOPE.Route must stay accepted.
	if !strings.Contains(out, scopeRouteName6902) {
		t.Errorf("endpoints listing lacks the %q entity %q. Got:\n%s",
			types.EntityKindRoute, scopeRouteName6902, out)
	}
	// 4. the FORBIDDEN direction — endpoints_only must still exclude a
	//    SCOPE.Class, which no arm of isEndpointKind matches.
	if strings.Contains(out, decoyName6902) {
		t.Errorf("endpoints listing contains %q, a %q entity. Widening to bare "+
			"\"Route\" must not widen to non-route kinds. Got:\n%s",
			decoyName6902, types.EntityKindClass, out)
	}
	// The roll-up counter must move with the listing.
	if !strings.Contains(out, "Endpoints           : 3") {
		t.Errorf("endpoint roll-up should count 3 endpoint kinds (bare Route, "+
			"SCOPE.Route, http_endpoint_definition) and not the SCOPE.Class "+
			"decoy. Got:\n%s", out)
	}
}

// deadCode6902Doc has NO edges at all: every entity's reachability verdict is
// decided purely by the seed set, which is what this test grades.
func deadCode6902Doc() *graph.Document {
	mk := func(id, name, kind, file string) graph.Entity {
		return graph.Entity{ID: id, Name: name, Kind: kind, SourceFile: file, StartLine: 3}
	}
	return &graph.Document{
		Repo: "svc",
		Entities: []graph.Entity{
			mk("bare-route-6902", bareRouteName6902, string(types.EntityKindRouteBare), "src/routes.go"),
			mk("scope-route-6902", scopeRouteName6902, string(types.EntityKindRoute), "src/routes.go"),
			mk("decoy-6902", decoyName6902, string(types.EntityKindClass), "src/ledger.go"),
		},
	}
}

// Site 2 — grafel_dead_code's reported dead list.
func TestDeadCode_BareRouteIsAFrameworkEntryPoint_6902(t *testing.T) {
	srv := newTestServer(t, deadCode6902Doc())
	res := callFlowTool(t, srv.handleDeadCode, map[string]any{"limit": float64(100)})

	items, ok := res["dead_code"].([]any)
	if !ok {
		t.Fatalf("dead_code is not an array: %T (%v)", res["dead_code"], res)
	}
	names := make([]string, 0, len(items))
	for _, it := range items {
		m, _ := it.(map[string]any)
		n, _ := m["name"].(string)
		names = append(names, n)
	}

	has := func(n string) bool {
		for _, g := range names {
			if g == n {
				return true
			}
		}
		return false
	}

	// 1. vacuity guard — the tool must actually report something dead here,
	//    otherwise the two absence assertions below are free.
	if !has(decoyName6902) {
		t.Fatalf("dead_code = %v does not contain the unseeded %q entity %q; "+
			"the absence assertions below would be vacuous",
			names, types.EntityKindClass, decoyName6902)
	}
	// 2. the #6902 recall direction — an HTTP route is never dead code.
	if has(bareRouteName6902) {
		t.Errorf("dead_code = %v reports the bare-\"Route\" entity %q as dead. "+
			"frameworkEntryKindsMCP holds \"SCOPE.Route\" but not \"Route\", so "+
			"every Java/Spring/Django route is unseeded and reported as dead code.",
			names, bareRouteName6902)
	}
	// 3. SCOPE.Route must stay seeded.
	if has(scopeRouteName6902) {
		t.Errorf("dead_code = %v reports the %q entity %q as dead.",
			names, types.EntityKindRoute, scopeRouteName6902)
	}
}
