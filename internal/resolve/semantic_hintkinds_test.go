package resolve

import (
	"testing"

	"github.com/cajasmota/archigraph/internal/types"
)

// #3930 (epic #3929): the rewrite agent's #1 regression — NestJS/Spring/
// Angular DI edges (INJECTED_INTO/BINDS) and the rest of the new semantic-
// edge taxonomy ORPHANED when an endpoint name was ambiguous, because
// hintKinds() returned nil for those kinds. With a kind hint, an ambiguous
// endpoint resolves to the REAL source-bearing entity (Class/Component/
// Function/Method) rather than collapsing to statusAmbiguous → phantom node.
//
// These tests are VALUE-ASSERTING: they assert the resolved FromID/ToID is
// the specific real entity, and that the edge is reachable in adjacency
// (not left as a bare stub), not merely that some rewrite happened.

// devicesWriteServiceFixture reproduces the confirmed real-world collision:
// a real source-bearing Class:DevicesWriteService plus a synthetic spec /
// OpenAPI SCOPE.Component("module") placeholder of the SAME bare name. The
// controller and the service are the two consumers/providers in the DI graph.
func devicesWriteServiceFixture() []types.EntityRecord {
	return []types.EntityRecord{
		// The REAL service class (source-bearing).
		entAt("aaaaaaaaaaaaaaaa", "Class", "DevicesWriteService", "devices/devices-write.service.ts"),
		// The spec/OpenAPI `module` Component stub of the same name — the
		// phantom that previously won (or rather, made resolution ambiguous).
		entAt("bbbbbbbbbbbbbbbb", "SCOPE.Component", "DevicesWriteService", "openapi/spec.yaml"),
		// The DI consumer (controller) and a second consumer (another service).
		entAt("cccccccccccccccc", "Class", "DevicesController", "devices/devices.controller.ts"),
		entAt("dddddddddddddddd", "Class", "DevicesOrchestrator", "devices/devices.orchestrator.ts"),
	}
}

// TestHintKinds_InjectedInto_AmbiguousProviderResolvesToRealClass is the
// headline P0-A assertion: provider name DevicesWriteService is ambiguous
// (real Class + spec Component); the INJECTED_INTO edge must bind its
// provider endpoint to the REAL Class, NOT the spec stub, and the edge must
// be present in adjacency for BOTH consumers.
func TestHintKinds_InjectedInto_AmbiguousProviderResolvesToRealClass(t *testing.T) {
	idx := BuildIndex(devicesWriteServiceFixture())
	rels := []types.RelationshipRecord{
		// provider INJECTED_INTO consumer (FromID = provider service name).
		{FromID: "DevicesWriteService", ToID: "cccccccccccccccc", Kind: "INJECTED_INTO"},
		{FromID: "DevicesWriteService", ToID: "dddddddddddddddd", Kind: "INJECTED_INTO"},
	}
	stats := References(rels, idx)

	for i, r := range rels {
		if r.FromID != "aaaaaaaaaaaaaaaa" {
			t.Fatalf("edge %d: provider resolved to %q, want real Class aaaaaaaaaaaaaaaa (not spec stub bbbbbbbbbbbbbbbb / phantom)", i, r.FromID)
		}
		if r.FromID == "bbbbbbbbbbbbbbbb" {
			t.Fatalf("edge %d: provider bound to the spec Component placeholder", i)
		}
	}
	if stats.Ambiguous != 0 {
		t.Fatalf("expected 0 ambiguous, got %+v (edge would orphan to a phantom node)", stats)
	}

	// Reachability: both edges now share a real From endpoint, so a
	// provider→consumers adjacency keyed on the resolved real Class ID
	// contains both consumers — i.e. neither edge is orphaned under a
	// phantom bare-name key.
	adj := map[string][]string{}
	for _, r := range rels {
		adj[r.FromID] = append(adj[r.FromID], r.ToID)
	}
	got := adj["aaaaaaaaaaaaaaaa"]
	if len(got) != 2 {
		t.Fatalf("provider adjacency = %v, want both consumers reachable from the real Class", got)
	}
	if _, phantom := adj["DevicesWriteService"]; phantom {
		t.Fatalf("an edge remained keyed under the phantom bare-name 'DevicesWriteService'")
	}
}

// TestHintKinds_Binds_AmbiguousImplResolvesToRealClass — a DI BINDS
// token→impl edge whose impl name is ambiguous (real Class + spec stub)
// resolves to the real impl Class.
func TestHintKinds_Binds_AmbiguousImplResolvesToRealClass(t *testing.T) {
	entities := []types.EntityRecord{
		entAt("1111111111111111", "Class", "DevicesWriteService", "impl/devices-write.service.ts"),
		entAt("2222222222222222", "SCOPE.Component", "DevicesWriteService", "openapi/spec.yaml"),
		entAt("3333333333333333", "Class", "DevicesWriteToken", "di/tokens.ts"),
	}
	idx := BuildIndex(entities)
	// token BINDS impl (ToID = impl class name, ambiguous).
	rels := []types.RelationshipRecord{
		{FromID: "3333333333333333", ToID: "DevicesWriteService", Kind: "BINDS"},
	}
	stats := References(rels, idx)
	if rels[0].ToID != "1111111111111111" {
		t.Fatalf("BINDS impl resolved to %q, want real Class 1111111111111111", rels[0].ToID)
	}
	if stats.Ambiguous != 0 {
		t.Fatalf("BINDS: expected 0 ambiguous, got %+v", stats)
	}
}

// TestHintKinds_DependsOnService_AmbiguousCallerResolvesToRealFunction —
// the FROM endpoint is a function whose bare name collides with a spec
// Component. operationKindFamily must pick the real Function.
func TestHintKinds_DependsOnService_AmbiguousCallerResolvesToRealFunction(t *testing.T) {
	entities := []types.EntityRecord{
		entAt("aaaa0000aaaa0000", "Function", "chargeCustomer", "billing/charge.ts"),
		// A same-named spec/OpenAPI Component stub that previously made the
		// caller endpoint ambiguous.
		entAt("bbbb0000bbbb0000", "SCOPE.Component", "chargeCustomer", "openapi/spec.yaml"),
		// The synthetic external-service target resolves by QualifiedName,
		// not via this bare-name path; modelled as a plain stub the resolver
		// leaves alone.
	}
	idx := BuildIndex(entities)
	rels := []types.RelationshipRecord{
		{FromID: "chargeCustomer", ToID: "service:stripe", Kind: "DEPENDS_ON_SERVICE"},
	}
	stats := References(rels, idx)
	if rels[0].FromID != "aaaa0000aaaa0000" {
		t.Fatalf("DEPENDS_ON_SERVICE caller resolved to %q, want real Function aaaa0000aaaa0000", rels[0].FromID)
	}
	_ = stats
}

// TestHintKinds_JoinsChannel_AmbiguousHandlerResolvesToRealMethod — confirms
// an operation-shaped semantic edge with an ambiguous handler endpoint
// resolves to the right (Method) kind rather than a colliding Component.
func TestHintKinds_JoinsChannel_AmbiguousHandlerResolvesToRealMethod(t *testing.T) {
	entities := []types.EntityRecord{
		entAt("cccc0000cccc0000", "Method", "onConnect", "ws/gateway.ts"),
		entAt("dddd0000dddd0000", "Component", "onConnect", "ui/OnConnect.tsx"),
	}
	idx := BuildIndex(entities)
	rels := []types.RelationshipRecord{
		{FromID: "onConnect", ToID: "channel:lobby", Kind: "JOINS_CHANNEL"},
	}
	stats := References(rels, idx)
	if rels[0].FromID != "cccc0000cccc0000" {
		t.Fatalf("JOINS_CHANNEL handler resolved to %q, want real Method cccc0000cccc0000", rels[0].FromID)
	}
	_ = stats
}

// TestHintKinds_GraphRelates_AmbiguousNodeResolvesToRealClass — Neo4j
// @Node→@Node edge: both endpoints are domain classes; an ambiguous target
// node name must bind to the real Class.
func TestHintKinds_GraphRelates_AmbiguousNodeResolvesToRealClass(t *testing.T) {
	entities := []types.EntityRecord{
		entAt("eeee0000eeee0000", "Class", "Movie", "graph/Movie.java"),
		entAt("ffff0000ffff0000", "SCOPE.Component", "Movie", "openapi/spec.yaml"),
		entAt("9999000099990000", "Class", "Person", "graph/Person.java"),
	}
	idx := BuildIndex(entities)
	rels := []types.RelationshipRecord{
		{FromID: "9999000099990000", ToID: "Movie", Kind: "GRAPH_RELATES"},
	}
	stats := References(rels, idx)
	if rels[0].ToID != "eeee0000eeee0000" {
		t.Fatalf("GRAPH_RELATES target resolved to %q, want real Class eeee0000eeee0000", rels[0].ToID)
	}
	if stats.Ambiguous != 0 {
		t.Fatalf("GRAPH_RELATES: expected 0 ambiguous, got %+v", stats)
	}
}

// TestHintKinds_Renders_MixedFamilyResolvesUnambiguousUnion — RENDERS uses
// the component∪operation union. When the union has exactly one match
// (a real Component, with only a spec-stub collision) it resolves; the
// next test asserts the honest negative.
func TestHintKinds_Renders_MixedFamilyResolvesUnambiguousUnion(t *testing.T) {
	entities := []types.EntityRecord{
		entAt("aabb0000aabb0000", "Component", "UserCard", "ui/UserCard.tsx"),
		entAt("ccdd0000ccdd0000", "SCOPE.Component", "UserCard", "openapi/spec.yaml"),
		entAt("eeff0000eeff0000", "Method", "renderProfile", "ui/Profile.tsx"),
	}
	idx := BuildIndex(entities)
	rels := []types.RelationshipRecord{
		{FromID: "eeff0000eeff0000", ToID: "UserCard", Kind: "RENDERS"},
	}
	stats := References(rels, idx)
	if rels[0].ToID != "aabb0000aabb0000" {
		t.Fatalf("RENDERS target resolved to %q, want real Component aabb0000aabb0000", rels[0].ToID)
	}
	_ = stats
}

// TestHintKinds_GenuinelyUnresolvable_StaysUnresolved is the HONEST negative:
// when a name is ambiguous across TWO real source-bearing entities of kinds
// the hint cannot separate (two real Classes), the edge must NOT be forced
// to a wrong single resolution — it stays a stub.
func TestHintKinds_GenuinelyUnresolvable_StaysUnresolved(t *testing.T) {
	entities := []types.EntityRecord{
		entAt("a1a1a1a1a1a1a1a1", "Class", "Repo", "a/Repo.ts"),
		entAt("b2b2b2b2b2b2b2b2", "Class", "Repo", "b/Repo.ts"),
	}
	idx := BuildIndex(entities)
	rels := []types.RelationshipRecord{
		{FromID: "Repo", ToID: "cccccccccccccccc", Kind: "INJECTED_INTO"},
	}
	stats := References(rels, idx)
	if rels[0].FromID == "a1a1a1a1a1a1a1a1" || rels[0].FromID == "b2b2b2b2b2b2b2b2" {
		t.Fatalf("two-real-class ambiguity was silently forced to %q", rels[0].FromID)
	}
	if rels[0].FromID != "Repo" {
		t.Fatalf("FromID mutated unexpectedly to %q (want preserved stub 'Repo')", rels[0].FromID)
	}
	if stats.Ambiguous != 1 {
		t.Fatalf("expected 1 honest ambiguous, got %+v", stats)
	}
}

// TestHintKinds_NonAmbiguous_StillResolves guards against a regression where
// adding the hint would somehow change the unambiguous path: a DI edge whose
// endpoint is unique still resolves (this is the DataSource-ctor-dep case the
// diagnosis said already survived; it must keep surviving).
func TestHintKinds_NonAmbiguous_StillResolves(t *testing.T) {
	entities := []types.EntityRecord{
		entAt("1212121212121212", "Class", "DataSource", "db/data-source.ts"),
		entAt("3434343434343434", "Class", "RepoModule", "db/repo.module.ts"),
	}
	idx := BuildIndex(entities)
	rels := []types.RelationshipRecord{
		{FromID: "DataSource", ToID: "3434343434343434", Kind: "INJECTED_INTO"},
	}
	stats := References(rels, idx)
	if rels[0].FromID != "1212121212121212" {
		t.Fatalf("non-ambiguous DI provider resolved to %q, want 1212121212121212", rels[0].FromID)
	}
	if stats.Rewritten < 1 {
		t.Fatalf("expected the unambiguous provider edge to rewrite, got %+v", stats)
	}
}
