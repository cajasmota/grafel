package types

import "testing"

// --- EntityKind ---

func TestEntityKind_StringValues(t *testing.T) {
	cases := map[EntityKind]string{
		EntityKindOperation:     "SCOPE.Operation",
		EntityKindComponent:     "SCOPE.Component",
		EntityKindFunction:      "SCOPE.Function",
		EntityKindClass:         "SCOPE.Class",
		EntityKindExternal:      "SCOPE.External",
		EntityKindProject:       "SCOPE.Project",
		EntityKindConfig:        "SCOPE.Config",
		EntityKindModel:         "SCOPE.Model",
		EntityKindScopeUnknown:  "SCOPE.ScopeUnknown",
		EntityKindInfraResource: "SCOPE.InfraResource",
	}
	for k, want := range cases {
		if string(k) != want {
			t.Errorf("EntityKind %v = %q, want %q", k, string(k), want)
		}
	}
}

func TestAllEntityKinds_ContainsExpected(t *testing.T) {
	all := AllEntityKinds()
	if len(all) < 20 {
		t.Fatalf("AllEntityKinds returned %d kinds, want >= 20", len(all))
	}
	// Spot-check that the four reconciled kinds are present.
	must := []EntityKind{
		EntityKindExternal,
		EntityKindProject,
		EntityKindConfig,
		EntityKindModel,
	}
	for _, m := range must {
		found := false
		for _, k := range all {
			if k == m {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("AllEntityKinds missing %v", m)
		}
	}
}

func TestIsValidEntityKind(t *testing.T) {
	if !IsValidEntityKind("SCOPE.Function") {
		t.Error("SCOPE.Function should be valid")
	}
	if IsValidEntityKind("SCOPE.IMPORTS") {
		t.Error("SCOPE.IMPORTS (historical typo) must NOT be valid")
	}
	if IsValidEntityKind("SCOPE.component") {
		t.Error("SCOPE.component (lowercase typo) must NOT be valid")
	}
	if IsValidEntityKind("SCOPE.TestCoverage") {
		t.Error("SCOPE.TestCoverage has no producer; must NOT be valid")
	}
	if IsValidEntityKind("SCOPE.DeprecationAnnotation") {
		t.Error("SCOPE.DeprecationAnnotation has no producer; must NOT be valid")
	}
}

// --- RelationshipKind ---

func TestRelationshipKind_StringValues(t *testing.T) {
	cases := map[RelationshipKind]string{
		RelationshipKindCalls:      "CALLS",
		RelationshipKindImports:    "IMPORTS",
		RelationshipKindExtends:    "EXTENDS",
		RelationshipKindImplements: "IMPLEMENTS",
		RelationshipKindUses:       "USES",
		RelationshipKindRenders:    "RENDERS",
		RelationshipKindReturns:    "RETURNS",
		RelationshipKindServes:     "SERVES",
	}
	for k, want := range cases {
		if string(k) != want {
			t.Errorf("RelationshipKind %v = %q, want %q", k, string(k), want)
		}
	}
}

func TestIsValidRelationshipKind(t *testing.T) {
	if !IsValidRelationshipKind("RENDERS") {
		t.Error("RENDERS should be valid (Issue #77 reconciliation)")
	}
	if !IsValidRelationshipKind("RETURNS") {
		t.Error("RETURNS should be valid (Issue #77 reconciliation)")
	}
	if IsValidRelationshipKind("CONSUMES_QUEUE") {
		t.Error("CONSUMES_QUEUE has no producer; must NOT be valid")
	}
	if IsValidRelationshipKind("TRIGGERS_LAMBDA") {
		t.Error("TRIGGERS_LAMBDA has no producer; must NOT be valid")
	}
	if IsValidRelationshipKind("READS_TABLE") {
		t.Error("READS_TABLE has no producer; must NOT be valid")
	}
	if IsValidRelationshipKind("WRITES_TABLE") {
		t.Error("WRITES_TABLE has no producer; must NOT be valid")
	}
}

func TestAllRelationshipKinds_NonEmpty(t *testing.T) {
	if len(AllRelationshipKinds()) < 15 {
		t.Errorf("AllRelationshipKinds returned %d, want >= 15", len(AllRelationshipKinds()))
	}
}

// TestChannelBindingKinds_Registered guards the #5782 (ADR-0025) additions:
// the SCOPE.ChannelBinding entity kind and the BINDS / BINDS_TOPIC edge kinds
// must be registered in the AllEntityKinds / AllRelationshipKinds allow-lists
// (and therefore accepted by IsValid*Kind), so no producer leaks a free-form
// kind string.
func TestChannelBindingKinds_Registered(t *testing.T) {
	if string(EntityKindChannelBinding) != "SCOPE.ChannelBinding" {
		t.Errorf("EntityKindChannelBinding = %q, want SCOPE.ChannelBinding", EntityKindChannelBinding)
	}
	if !IsValidEntityKind("SCOPE.ChannelBinding") {
		t.Error("SCOPE.ChannelBinding must be a valid entity kind (missing from AllEntityKinds)")
	}
	found := false
	for _, k := range AllEntityKinds() {
		if k == EntityKindChannelBinding {
			found = true
			break
		}
	}
	if !found {
		t.Error("AllEntityKinds missing EntityKindChannelBinding")
	}

	if string(RelationshipKindBinds) != "BINDS" {
		t.Errorf("RelationshipKindBinds = %q, want BINDS", RelationshipKindBinds)
	}
	if string(RelationshipKindBindsTopic) != "BINDS_TOPIC" {
		t.Errorf("RelationshipKindBindsTopic = %q, want BINDS_TOPIC", RelationshipKindBindsTopic)
	}
	if !IsValidRelationshipKind("BINDS") {
		t.Error("BINDS must be a valid relationship kind (missing from AllRelationshipKinds)")
	}
	if !IsValidRelationshipKind("BINDS_TOPIC") {
		t.Error("BINDS_TOPIC must be a valid relationship kind (missing from AllRelationshipKinds)")
	}
}

// TestProducesConsumesRelationshipKinds_Registered pins #6741 Arm 1.
//
// PRODUCES shipped as an undeclared literal string in
// internal/custom/java/quartz.go, and four consumers (mcp/flow_tools.go,
// mcp/dead_code.go, links/reachability.go, dashboard/topology_compound.go)
// hard-code both PRODUCES and CONSUMES in their edge-kind tables. Neither was
// in the vocabulary, so IsValidRelationshipKind reported them invalid — which
// stayed harmless only because that validator has no non-test callers (#6757).
//
// Declaring the constant is not enough: the realistic mistake is adding the
// constant and forgetting the AllRelationshipKinds entry, which leaves
// IsValidRelationshipKind — and so #6757's enforcement — rejecting a kind the
// graph really carries. This asserts both halves. Semantics: ADR-0028.
func TestProducesConsumesRelationshipKinds_Registered(t *testing.T) {
	wantValues := map[RelationshipKind]string{
		RelationshipKindProduces: "PRODUCES",
		RelationshipKindConsumes: "CONSUMES",
	}
	for k, want := range wantValues {
		if string(k) != want {
			t.Errorf("RelationshipKind %v = %q, want %q", k, string(k), want)
		}
	}

	inEnum := map[RelationshipKind]bool{}
	for _, k := range AllRelationshipKinds() {
		inEnum[k] = true
	}
	for k := range wantValues {
		if !inEnum[k] {
			t.Errorf("%q is declared but missing from AllRelationshipKinds(); "+
				"IsValidRelationshipKind would reject an edge kind the graph emits", string(k))
		}
		if !IsValidRelationshipKind(string(k)) {
			t.Errorf("IsValidRelationshipKind(%q) = false, want true (#6741 Arm 1)", string(k))
		}
	}

	// Declaring CONSUMES must not smuggle in the never-implemented
	// CONSUMES_QUEUE from ADR-0003's sketch; see ADR-0028.
	if IsValidRelationshipKind("CONSUMES_QUEUE") {
		t.Error("CONSUMES_QUEUE still has no producer; declaring CONSUMES must not make it valid")
	}
}
