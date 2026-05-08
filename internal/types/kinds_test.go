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
