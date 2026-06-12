package coverage

import (
	"testing"

	"github.com/cajasmota/archigraph/internal/types"
)

// reachFixture is a synthetic entity+edge set exercising the reachability
// passes:
//
//	test1 --TESTS--> handlerA --CALLS--> serviceB        (both reachable: 1, 2)
//	handlerC                                             (orphan, unreachable)
//	endpointPosted    <--HANDLES-- handlerA              (reachable via handler)
//	endpointOrphan    <--HANDLES-- handlerC              (unreachable handler)
//	endpointE2E       <--TESTS---- test2                 (reachable directly)
//	schemaX, configY                                     (non-production, excluded)
func reachFixture() []types.EntityRecord {
	return []types.EntityRecord{
		// Test entity: SCOPE.Pattern + test_suite subtype. TESTS handlerA.
		{
			ID: "test1", Kind: string(types.EntityKindPattern), Subtype: subtypeTestSuite,
			Name: "HandlerATest", SourceFile: "tests/a_test.go",
			Relationships: []types.RelationshipRecord{
				{FromID: "test1", ToID: "handlerA", Kind: string(types.RelationshipKindTests)},
			},
		},
		// e2e test (tag-marked) that TESTS an endpoint directly.
		{
			ID: "test2", Kind: string(types.EntityKindPattern), Tags: []string{"test"},
			Name: "E2EFlow", SourceFile: "tests/e2e_test.go",
			Relationships: []types.RelationshipRecord{
				{FromID: "test2", ToID: "endpointE2E", Kind: string(types.RelationshipKindTests)},
			},
		},
		// Production: handlerA CALLS serviceB and HANDLES endpointPosted.
		{
			ID: "handlerA", Kind: string(types.EntityKindFunction),
			Name: "CreateInspection", SourceFile: "api/inspections.go", StartLine: 10, EndLine: 30,
			Relationships: []types.RelationshipRecord{
				{FromID: "handlerA", ToID: "serviceB", Kind: string(types.RelationshipKindCalls)},
				{FromID: "handlerA", ToID: "endpointPosted", Kind: string(types.RelationshipKindHandles)},
			},
		},
		// Production: serviceB, reached transitively at depth 2.
		{
			ID: "serviceB", Kind: string(types.EntityKindService),
			Name: "InspectionService", SourceFile: "svc/inspection.go", StartLine: 1, EndLine: 50,
		},
		// Production orphan: handlerC, no test path, HANDLES endpointOrphan.
		{
			ID: "handlerC", Kind: string(types.EntityKindFunction),
			Name: "DeleteInspection", SourceFile: "api/inspections.go", StartLine: 40, EndLine: 60,
			Relationships: []types.RelationshipRecord{
				{FromID: "handlerC", ToID: "endpointOrphan", Kind: string(types.RelationshipKindHandles)},
			},
		},
		// Endpoints.
		{ID: "endpointPosted", Kind: string(types.EntityKindEndpoint), Name: "POST /inspections", SourceFile: "api/inspections.go"},
		{ID: "endpointOrphan", Kind: string(types.EntityKindEndpoint), Name: "DELETE /inspections/:id", SourceFile: "api/inspections.go"},
		{ID: "endpointE2E", Kind: string(types.EntityKindEndpoint), Name: "GET /health", SourceFile: "api/health.go"},
		// Non-production: excluded from reachability + roll-ups.
		{ID: "schemaX", Kind: string(types.EntityKindSchema), Name: "InspectionSchema", SourceFile: "api/schema.go"},
		{ID: "configY", Kind: string(types.EntityKindConfig), Name: "appconfig", SourceFile: "config.go"},
	}
}

func indexReach(rs []Reachability) map[string]Reachability {
	m := make(map[string]Reachability, len(rs))
	for _, r := range rs {
		m[r.EntityID] = r
	}
	return m
}

func TestComputeReachability(t *testing.T) {
	reach := ComputeReachability(reachFixture())
	got := indexReach(reach)

	// Non-production entities must be absent.
	for _, id := range []string{"schemaX", "configY", "test1", "test2"} {
		if _, ok := got[id]; ok {
			t.Errorf("non-production %q should not appear in reachability output", id)
		}
	}

	// handlerA: reachable, depth 1, by test1.
	if a := got["handlerA"]; !a.Reachable || a.ReachDepth != 1 {
		t.Errorf("handlerA: want reachable depth 1, got %+v", a)
	} else if len(a.ReachingTests) != 1 || a.ReachingTests[0] != "test1" {
		t.Errorf("handlerA: want reaching test [test1], got %v", a.ReachingTests)
	}

	// serviceB: reachable transitively, depth 2.
	if b := got["serviceB"]; !b.Reachable || b.ReachDepth != 2 {
		t.Errorf("serviceB: want reachable depth 2, got %+v", b)
	}

	// handlerC: orphan, unreachable.
	if c := got["handlerC"]; c.Reachable {
		t.Errorf("handlerC: want unreachable, got %+v", c)
	}

	// endpointE2E reachable directly via TESTS (depth 1).
	if e := got["endpointE2E"]; !e.Reachable || e.ReachDepth != 1 {
		t.Errorf("endpointE2E: want reachable depth 1, got %+v", e)
	}
}

func TestComputeEndpointReachability(t *testing.T) {
	ents := reachFixture()
	reach := ComputeReachability(ents)
	eps := ComputeEndpointReachability(ents, reach)

	got := make(map[string]EndpointReachability, len(eps))
	for _, e := range eps {
		got[e.EndpointID] = e
	}
	if len(got) != 3 {
		t.Fatalf("want 3 endpoints, got %d: %+v", len(got), eps)
	}

	// endpointPosted: reachable via handlerA.
	if e := got["endpointPosted"]; !e.Reachable || !e.ViaHandler || e.HandlerID != "handlerA" {
		t.Errorf("endpointPosted: want reachable via handlerA, got %+v", e)
	}
	// endpointOrphan: handlerC unreachable → endpoint unreachable.
	if e := got["endpointOrphan"]; e.Reachable {
		t.Errorf("endpointOrphan: want unreachable, got %+v", e)
	}
	// endpointE2E: reachable directly (not via handler).
	if e := got["endpointE2E"]; !e.Reachable || e.ViaHandler {
		t.Errorf("endpointE2E: want reachable directly, got %+v", e)
	}
}

func TestComputeRollUps(t *testing.T) {
	ents := reachFixture()
	reach := ComputeReachability(ents)
	ru := ComputeRollUps(ents, reach)

	// Production entities: handlerA, serviceB, handlerC, endpointPosted,
	// endpointOrphan, endpointE2E = 6 total. Reachable: handlerA, serviceB,
	// endpointE2E = 3. (endpointPosted/Orphan are not stamped reachable at the
	// per-entity level — their reachability is the endpoint-level derivation.)
	if ru.Group.Total != 6 {
		t.Errorf("group total: want 6, got %d", ru.Group.Total)
	}
	if ru.Group.Reachable != 3 {
		t.Errorf("group reachable: want 3, got %d", ru.Group.Reachable)
	}
	if ru.Group.Pct != 50.0 {
		t.Errorf("group pct: want 50.0, got %.1f", ru.Group.Pct)
	}

	// Per-module: api/inspections.go entities bucket under "api".
	if m := ru.Modules["api"]; m.Total == 0 {
		t.Errorf("expected an 'api' module bucket, got %+v", ru.Modules)
	}
}

func TestApplyReachabilityProperties(t *testing.T) {
	ents := reachFixture()
	reach := ComputeReachability(ents)
	out := ApplyReachability(ents, reach)

	byID := make(map[string]types.EntityRecord, len(out))
	for _, e := range out {
		byID[entityID(e)] = e
	}

	// Inputs not mutated.
	for _, e := range ents {
		if e.Properties != nil {
			t.Fatalf("input entity %s was mutated", e.ID)
		}
	}

	hA := byID["handlerA"].Properties
	if hA[PropTestReachable] != "true" || hA[PropReachDepth] != "1" {
		t.Errorf("handlerA props: want reachable/depth 1, got %v", hA)
	}
	if hA[PropReachingTests] != "test1" || hA[PropReachingTestCount] != "1" {
		t.Errorf("handlerA reaching-tests props wrong: %v", hA)
	}

	hC := byID["handlerC"].Properties
	if hC[PropTestReachable] != "false" {
		t.Errorf("handlerC: want test_reachable=false, got %v", hC)
	}
	if _, ok := hC[PropReachDepth]; ok {
		t.Errorf("handlerC: unreachable entity should not carry reach_depth, got %v", hC)
	}
}

func TestCrossSignal(t *testing.T) {
	cases := []struct {
		name  string
		props map[string]string
		want  CrossSignalVerdict
	}{
		{"reachable+lines", map[string]string{PropTestReachable: "true", PropCoveragePct: "82.0"}, CrossSignalTestedAndRun},
		{"reachable+0lines", map[string]string{PropTestReachable: "true", PropCoveragePct: "0.0"}, CrossSignalReachableNoLines},
		{"unreachable", map[string]string{PropTestReachable: "false"}, CrossSignalUntested},
		{"reachable-no-lcov", map[string]string{PropTestReachable: "true"}, CrossSignalUnknown},
		{"no-signal", map[string]string{}, CrossSignalUnknown},
	}
	for _, c := range cases {
		if got := CrossSignal(c.props); got != c.want {
			t.Errorf("%s: want %s, got %s", c.name, c.want, got)
		}
	}
}
