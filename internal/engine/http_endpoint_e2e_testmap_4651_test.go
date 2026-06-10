package engine

import (
	"testing"

	"github.com/cajasmota/archigraph/internal/graph"
	"github.com/cajasmota/archigraph/internal/types"
)

// Issue #4651 — upvate-v3 (NestJS) coverage lever: duplicate-name endpoint
// disambiguation.
//
// upvate-v3 has the same handler name across many modules (`create`, `update`,
// `getCounts`, `list`, …). When two http_endpoint_definition entities
// synthesize the SAME endpoint Name (because the route shape folds to the same
// key — here a `getCounts` handler in BOTH the inspections and proposals
// modules whose synthesized route collides), the e2e route-test linker hit two
// failure modes:
//
//  1. resolveRouteTestToDefinition saw len(tier1) > 1 and bailed (no edge), so
//     EACH endpoint dangled and read UNCOVERED — even though each spec names
//     exactly one module's route. (#4628 root-cause.)
//  2. Even when the right candidate was picked, the TESTS edge carried a
//     `Kind:Name` stub ToID, which resolve.BuildIndex blanks on the colliding
//     Name → resolve.References can't bind it → the edge dangles anyway.
//
// This fix disambiguates same-route candidates by spec↔controller module/file
// affinity AND emits the edge with the matched definition's UNIQUE entity ID
// (graph.EntityID over Kind+Name+SourceFile — distinct per source file even when
// the Name collides), so each endpoint is credited to ITS OWN spec.

const repo4651 = "cajasmota/upvate-v3"

// dupDef is def() with an explicit controller source file so the two same-route
// definitions get DISTINCT deterministic entity IDs (graph.EntityID folds in
// SourceFile) while sharing the colliding endpoint Name.
func dupDef(verb, path, sourceFile string) types.EntityRecord {
	d := def(verb, path)
	d.SourceFile = sourceFile
	return d
}

// TestIssue4651_DupNameEndpointsCreditedToOwnSpec is the RED→GREEN proof. Two
// modules expose a same-named `getCounts` route that synthesizes to the SAME
// endpoint Name; each has its own e2e spec. BEFORE: both dangle (0 edges).
// AFTER: each endpoint gains exactly one TESTS edge from ITS OWN spec, targeting
// that endpoint's unique entity ID.
func TestIssue4651_DupNameEndpointsCreditedToOwnSpec(t *testing.T) {
	// Two definitions with the SAME synthesized Name (http:GET:/api/v1/counts)
	// but DIFFERENT controllers/modules — exactly the upvate-v3 collision.
	inspDef := dupDef("GET", "/api/v1/counts", "src/inspections/inspections.controller.ts")
	propDef := dupDef("GET", "/api/v1/counts", "src/proposals/proposals.controller.ts")
	if inspDef.Name != propDef.Name {
		t.Fatalf("precondition: expected colliding endpoint Name, got %q vs %q", inspDef.Name, propDef.Name)
	}

	// One e2e spec per module, each issuing the (now-colliding) route. The
	// distinguishing signal is the spec's source file / module token.
	inspSpec := e2eSuite("GET /api/v1/counts")
	inspSpec.Name = "spec:inspections.e2e:Inspections"
	inspSpec.SourceFile = "test/inspections.e2e-spec.ts"
	propSpec := e2eSuite("GET /api/v1/counts")
	propSpec.Name = "spec:proposals.e2e:Proposals"
	propSpec.SourceFile = "test/proposals.e2e-spec.ts"

	in := []types.EntityRecord{inspDef, propDef, inspSpec, propSpec}
	out, stats := ResolveHTTPEndpointHandlersWithRepo(in, repo4651)

	// Expected unique per-file endpoint IDs.
	inspID := graph.EntityID(repo4651, inspDef.Kind, inspDef.Name, inspDef.SourceFile)
	propID := graph.EntityID(repo4651, propDef.Kind, propDef.Name, propDef.SourceFile)
	if inspID == propID {
		t.Fatalf("precondition: same-Name defs must still get distinct entity IDs (source-file folds in)")
	}

	// Each spec must produce exactly one TESTS edge, to its OWN module's endpoint.
	got := map[string]string{} // spec FromID -> endpoint ToID
	for _, e := range out {
		for _, r := range e.Relationships {
			if r.Kind == string(types.RelationshipKindTests) && r.Properties["match_source"] == "e2e_supertest_route" {
				got[r.FromID] = r.ToID
			}
		}
	}
	if stats.E2ERouteTestEdges != 2 {
		t.Fatalf("expected 2 TESTS edges (one per module spec), got %d (edges=%v)", stats.E2ERouteTestEdges, got)
	}

	wantInsp := "test_suite:" + inspSpec.Name
	wantProp := "test_suite:" + propSpec.Name
	if got[wantInsp] != inspID {
		t.Errorf("inspections spec must cover the inspections endpoint: from %q -> %q, want %q", wantInsp, got[wantInsp], inspID)
	}
	if got[wantProp] != propID {
		t.Errorf("proposals spec must cover the proposals endpoint: from %q -> %q, want %q", wantProp, got[wantProp], propID)
	}
}

// TestIssue4651_LegacyStubPathStillPicksRightDef proves the affinity tie-break
// alone (no repoTag → legacy Kind:Name stub) still selects the CORRECT
// definition index among same-route collisions, so the existing engine-test
// contract (stub ToID) is preserved while the ambiguity is resolved.
func TestIssue4651_LegacyStubPathStillPicksRightDef(t *testing.T) {
	inspDef := dupDef("GET", "/api/v1/counts", "src/inspections/inspections.controller.ts")
	propDef := dupDef("GET", "/api/v1/counts", "src/proposals/proposals.controller.ts")

	spec := e2eSuite("GET /api/v1/counts")
	spec.Name = "spec:proposals.e2e:Proposals"
	spec.SourceFile = "test/proposals.e2e-spec.ts"

	in := []types.EntityRecord{inspDef, propDef, spec}
	out, stats := ResolveHTTPEndpointHandlers(in) // empty repoTag → legacy stub
	if stats.E2ERouteTestEdges != 1 {
		t.Fatalf("expected 1 TESTS edge from the proposals spec, got %d", stats.E2ERouteTestEdges)
	}
	// Legacy ToID is the Kind:Name stub; the value is shared by both defs, but
	// the disambiguation must have selected the proposals candidate. We assert
	// the edge exists and carries the proposals spec as origin.
	var fromID string
	for _, e := range out {
		for _, r := range e.Relationships {
			if r.Kind == string(types.RelationshipKindTests) && r.Properties["match_source"] == "e2e_supertest_route" {
				fromID = r.FromID
			}
		}
	}
	if fromID != "test_suite:"+spec.Name {
		t.Fatalf("TESTS edge origin = %q, want %q", fromID, "test_suite:"+spec.Name)
	}
}

// TestIssue4651_AmbiguousNoModuleSignalSkips guards the no-guessing posture:
// when same-route candidates collide AND the spec carries no module token that
// favors one, the linker must emit NO edge rather than picking arbitrarily.
func TestIssue4651_AmbiguousNoModuleSignalSkips(t *testing.T) {
	d1 := dupDef("GET", "/api/v1/counts", "src/inspections/inspections.controller.ts")
	d2 := dupDef("GET", "/api/v1/counts", "src/proposals/proposals.controller.ts")

	// Spec file shares NO module token with either controller.
	spec := e2eSuite("GET /api/v1/counts")
	spec.Name = "spec:smoke.e2e:Smoke"
	spec.SourceFile = "test/smoke.e2e-spec.ts"

	_, stats := ResolveHTTPEndpointHandlersWithRepo([]types.EntityRecord{d1, d2, spec}, repo4651)
	if stats.E2ERouteTestEdges != 0 {
		t.Fatalf("ambiguous match with no module signal must emit 0 edges, got %d", stats.E2ERouteTestEdges)
	}
}
