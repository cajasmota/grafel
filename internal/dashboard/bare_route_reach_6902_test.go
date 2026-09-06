package dashboard

import (
	"encoding/json"
	"testing"

	"github.com/cajasmota/grafel/internal/coverage"
	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/types"
)

// ---------------------------------------------------------------------------
// #6902 — the mirror of #6894, one site over and in the opposite direction.
//
// isEndpointReachKind switches on the RAW kind and accepts SCOPE.Endpoint,
// SCOPE.Route and http_endpoint_definition. It therefore drops bare "Route"
// (types.EntityKindRouteBare) — the spelling every Java route extractor and
// internal/engine/{spring,django}_routes.go emits, and the spelling four
// golden fixtures (java-spring-mini, go-chi-mini, python-fastapi-mini,
// python-django-mini) assert with must_exist:true on an always-on gate.
//
// So entities CI guarantees exist were excluded from the coverage-reach
// roll-up: not counted in TotalEndpoints, and never listed as orphans even
// when no test reaches them.
//
// Both Route spellings mean the SAME concept — an HTTP route (#6894 §1b,
// #6776 arm B7: "Both members of each pair are live"). Unlike the Endpoint
// pair, where bare "Endpoint" is Electron IPC and "SCOPE.Endpoint" is HTTP,
// there is no reading on which this view wants one spelling and not the
// other. The fix is a WIDENING.
//
// THE POSITIVE CONTROL, and the trap that caught #6893. Its control was an
// http_endpoint_definition — a kind the predicate under test already matched
// — so control and arm only ever fired together and the arm sat ALIVE.
// The control here is the bare-"Route" entity, and the predicate's other
// three arms are exact string comparisons:
//
//	kind      == SCOPE.Endpoint  == SCOPE.Route  == http_endpoint_definition
//	"Route"   no                 no              no
//
// so nothing else in isEndpointReachKind can match it. The
// http_endpoint_definition below is present ONLY as a vacuity guard.
//
// Every assertion reads the EMITTED ARTEFACT: the JSON of the
// ReachabilitySummary that handleGroupCoverage embeds verbatim under
// "reachability" in GET /api/quality/coverage/{group}.
// ---------------------------------------------------------------------------

const (
	// bareRouteReachName grades the RECALL direction — the #6902 arm.
	bareRouteReachName = "/bare/route-6902"
	// scopeRouteReachName grades the direction a repoint-style "fix" would
	// break: SCOPE.Route was already accepted and must stay accepted.
	scopeRouteReachName = "/scope/route-6902"
	// decoyReachName grades the FORBIDDEN direction. SCOPE.Class is matched by
	// no arm of the predicate, before or after the fix; it carries the same
	// test_reachable prop as everything else so nothing upstream filters it
	// out for us — a decoy something else rejects would grade nothing.
	decoyReachName = "/decoy/class-6902"
	// vacuityReachName is the vacuity guard only, never a control.
	vacuityReachName = "/real/http-6902"
)

// reach6902Doc returns one repo document whose four entities all carry
// test_reachable=false, so every entity the predicate admits must appear in
// the emitted orphan list.
func reach6902Doc() *graph.Document {
	mk := func(id, name, kind string) graph.Entity {
		return graph.Entity{ID: id, Name: name, Kind: kind, SourceFile: "src/routes.go"}.
			WithProperties(map[string]string{coverage.PropTestReachable: "false"})
	}
	return &graph.Document{Entities: []graph.Entity{
		mk("bare-route-6902", bareRouteReachName, string(types.EntityKindRouteBare)),
		mk("scope-route-6902", scopeRouteReachName, string(types.EntityKindRoute)),
		mk("decoy-class-6902", decoyReachName, string(types.EntityKindClass)),
		mk("real-http-6902", vacuityReachName, "http_endpoint_definition"),
	}}
}

// orphanNamesFromPayload marshals the summary exactly as the coverage handler
// does and reads the names back out of the wire bytes.
func orphanNamesFromPayload(t *testing.T, s *ReachabilitySummary) []string {
	t.Helper()
	buf, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("marshal ReachabilitySummary: %v", err)
	}
	var wire struct {
		TotalEndpoints int `json:"total_endpoints"`
		Orphans        []struct {
			Name string `json:"name"`
		} `json:"orphans"`
	}
	if err := json.Unmarshal(buf, &wire); err != nil {
		t.Fatalf("unmarshal ReachabilitySummary: %v", err)
	}
	out := make([]string, 0, len(wire.Orphans))
	for _, o := range wire.Orphans {
		out = append(out, o.Name)
	}
	return out
}

func contains6902(hay []string, needle string) bool {
	for _, h := range hay {
		if h == needle {
			return true
		}
	}
	return false
}

func TestCoverageReachSummary_AcceptsBareRouteKind_6902(t *testing.T) {
	var a reachAccumulator
	a.accumulate(reach6902Doc(), "repoA")
	s := a.summarize()

	names := orphanNamesFromPayload(t, s)

	// 1. vacuity guard — if the pane emits nothing, nothing below grades.
	if !contains6902(names, vacuityReachName) {
		t.Fatalf("orphans = %v, missing the plain http_endpoint_definition %q; "+
			"every assertion below would be vacuous", names, vacuityReachName)
	}
	// 2. the #6902 recall direction.
	if !contains6902(names, bareRouteReachName) {
		t.Errorf("orphans = %v, missing the bare-\"Route\" entity %q. "+
			"isEndpointReachKind switches on the raw kind and has no "+
			"types.EntityKindRouteBare arm, so every Java/Spring/Django route "+
			"entity — the spelling four golden fixtures assert with "+
			"must_exist:true — is excluded from the coverage-reach view.",
			names, bareRouteReachName)
	}
	// 3. the direction a repoint would break: SCOPE.Route must stay.
	if !contains6902(names, scopeRouteReachName) {
		t.Errorf("orphans = %v, missing the %q entity %q. Both Route spellings "+
			"name the same concept; accepting the bare one must not cost the "+
			"prefixed one.", names, types.EntityKindRoute, scopeRouteReachName)
	}
	// 4. the FORBIDDEN direction.
	if contains6902(names, decoyReachName) {
		t.Errorf("orphans = %v contains %q, a %q entity. Widening this predicate "+
			"to bare \"Route\" must not widen it to non-route kinds.",
			names, decoyReachName, types.EntityKindClass)
	}
	// The roll-up counters must move with the list, not just the list.
	if s.TotalEndpoints != 3 {
		t.Errorf("total_endpoints = %d, want 3 (bare Route, SCOPE.Route, "+
			"http_endpoint_definition — and NOT the SCOPE.Class decoy)",
			s.TotalEndpoints)
	}
}
