package types

import "testing"

// TestIsHTTPEndpointKind_ForbiddenRows puts the guard beside the thing it
// guards.
//
// Every dashboard predicate that selects HTTP entrypoints is written as
//
//	IsHTTPEndpointKind(kind) || <a handful of extra kind clauses>
//
// and each of those extra clauses is graded only by a dashboard test. Before
// this file, internal/types asserted the ACCEPTING direction of
// IsHTTPEndpointKind and nothing at all about the rejecting one: widening the
// switch to admit bare "Endpoint" left ./internal/types green while the
// dashboard suite went red (measured while preparing #6893). A predicate whose
// permissive direction no test in its own package observes is a predicate one
// careless `case` away from swallowing every extra clause built on top of it —
// at which point the dashboard tests that grade those clauses stop grading
// anything, because IsHTTPEndpointKind already matched.
//
// The rows below are the kinds that MUST stay outside it, each for a reason:
//
//   - "Endpoint" (EntityKindEndpoint's bare twin) is the Electron rule pack's
//     IPC-channel kind (#6820). Admitting it would put IPC channels back in the
//     HTTP panes AND silently un-grade the SCOPE.Endpoint arm at ten sites.
//   - "SCOPE.Endpoint", "Route" and "SCOPE.Route" are all real HTTP-surface
//     kinds, but they are NOT http_endpoint kinds: they are carried by the
//     separate clauses beside this call, which is what the dashboard's #6894
//     controls key on. If IsHTTPEndpointKind started matching them, those
//     controls would fire for the wrong reason and the Route arm could be
//     deleted at every site with the suite still green — the exact
//     compound-masking failure #6893 shipped and had to fix.
//   - "SCOPE.Class" stands in for "any ordinary kind at all".
func TestIsHTTPEndpointKind_ForbiddenRows(t *testing.T) {
	// Positive control FIRST: without it, every rejection below would pass for
	// a predicate that returns false unconditionally.
	for _, accept := range []string{"http_endpoint", "http_endpoint_definition", "http_endpoint_call"} {
		if !IsHTTPEndpointKind(accept) {
			t.Fatalf("IsHTTPEndpointKind(%q) = false, want true; the rejection rows below "+
				"would be vacuous", accept)
		}
	}

	for _, reject := range []string{
		// The Electron IPC spelling. It deliberately has no constant — #6776
		// arm B8 left it off the enum on purpose (#6820).
		"Endpoint",
		string(EntityKindEndpoint),
		string(EntityKindRouteBare),
		string(EntityKindRoute),
		string(EntityKindClass),
		"",
	} {
		if IsHTTPEndpointKind(reject) {
			t.Errorf("IsHTTPEndpointKind(%q) = true, want false. Only the three http_endpoint "+
				"kinds belong here; every other HTTP-surface kind is carried by its own "+
				"clause beside this call, and folding one in here un-grades that clause "+
				"at every site (#6820, #6894).", reject)
		}
	}
}
