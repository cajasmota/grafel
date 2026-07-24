package resolve

import (
	"testing"

	"github.com/cajasmota/grafel/internal/extractors/bazel"
	"github.com/cajasmota/grafel/internal/types"
)

// makeTarget creates a minimal bazel_target entity for test fixtures.
func makeTarget(id, label, pkg string) types.EntityRecord {
	return types.EntityRecord{
		ID:         id,
		Name:       label,
		Kind:       string(types.EntityKindComponent),
		Subtype:    "bazel_target",
		SourceFile: pkg + "/BUILD",
		Language:   "bazel",
		Properties: map[string]string{
			"label":         label,
			"bazel_package": pkg,
		},
	}
}

// makeCallEnt creates a minimal language entity (function/class etc.) for tests.
func makeCallEnt(id, sourceFile string) types.EntityRecord {
	return types.EntityRecord{
		ID:         id,
		Name:       "SomeFunc",
		Kind:       string(types.EntityKindOperation),
		Subtype:    "function",
		SourceFile: sourceFile,
		Language:   "python",
	}
}

// makeBazelDep creates a BAZEL_DEPENDS_ON edge.
func makeBazelDep(fromID, toID, depLabel string) types.RelationshipRecord {
	return types.RelationshipRecord{
		FromID: fromID,
		ToID:   toID,
		Kind:   bazel.RelationshipKindBazelDependsOn,
		Properties: types.Props{
			{K: "dep_label", V: depLabel},
			{K: "source_rule", V: ""},
		},
	}
}

// makeCallEdge creates a CALLS edge.
func makeCallEdge(fromID, toID string) types.RelationshipRecord {
	return types.RelationshipRecord{
		FromID: fromID,
		ToID:   toID,
		Kind:   string(types.RelationshipKindCalls),
	}
}

// makeImportEdge creates an IMPORTS edge.
func makeImportEdge(fromID, toID string) types.RelationshipRecord {
	return types.RelationshipRecord{
		FromID: fromID,
		ToID:   toID,
		Kind:   string(types.RelationshipKindImports),
	}
}

// findStatus finds the BAZEL_DEP_STATUS edge for (fromID, toID) and returns its status property.
func findStatus(rels []types.RelationshipRecord, fromID, toID string) string {
	for _, r := range rels {
		if r.Kind == "BAZEL_DEP_STATUS" && r.FromID == fromID && r.ToID == toID {
			return r.Properties.Get("status")
		}
	}
	return ""
}

// TestBazelOverlay_DeclaredAndUsed verifies "declared+used" status when both
// a BAZEL_DEPENDS_ON edge and a CALLS edge exist between the same targets.
func TestBazelOverlay_DeclaredAndUsed(t *testing.T) {
	// Targets: auth → billing.
	authTarget := makeTarget("auth0000000000000", "//services/auth:auth_lib", "services/auth")
	billTarget := makeTarget("bill000000000000", "//services/billing:billing_lib", "services/billing")

	// Language entities living in those packages.
	authFunc := makeCallEnt("authfunc00000000", "services/auth/auth.py")
	billFunc := makeCallEnt("billfunc00000000", "services/billing/billing.py")

	entities := []types.EntityRecord{authTarget, billTarget, authFunc, billFunc}

	rels := []types.RelationshipRecord{
		// Declared dep.
		makeBazelDep("auth0000000000000", "bill000000000000", "//services/billing:billing_lib"),
		// Used: CALLS crosses package boundary.
		makeCallEdge("authfunc00000000", "billfunc00000000"),
	}

	result := RunBazelOverlay(entities, rels)

	status := findStatus(result.AnnotatedRels, "auth0000000000000", "bill000000000000")
	if status != "declared+used" {
		t.Errorf("status = %q, want %q", status, "declared+used")
	}
	if result.Stats.DeclaredUsed != 1 {
		t.Errorf("DeclaredUsed = %d, want 1", result.Stats.DeclaredUsed)
	}
	if result.Stats.DeclaredUnused != 0 {
		t.Errorf("DeclaredUnused = %d, want 0", result.Stats.DeclaredUnused)
	}
	if result.Stats.UndeclaredUsed != 0 {
		t.Errorf("UndeclaredUsed = %d, want 0", result.Stats.UndeclaredUsed)
	}
}

// TestBazelOverlay_DeclaredUnused verifies "declared_unused" when a
// BAZEL_DEPENDS_ON edge exists but no call-graph crossing exists.
func TestBazelOverlay_DeclaredUnused(t *testing.T) {
	authTarget := makeTarget("auth0000000000000", "//services/auth:auth_lib", "services/auth")
	billTarget := makeTarget("bill000000000000", "//services/billing:billing_lib", "services/billing")

	// No language entities with crossing calls.
	entities := []types.EntityRecord{authTarget, billTarget}

	rels := []types.RelationshipRecord{
		makeBazelDep("auth0000000000000", "bill000000000000", "//services/billing:billing_lib"),
	}

	result := RunBazelOverlay(entities, rels)

	status := findStatus(result.AnnotatedRels, "auth0000000000000", "bill000000000000")
	if status != "declared_unused" {
		t.Errorf("status = %q, want %q", status, "declared_unused")
	}
	if result.Stats.DeclaredUnused != 1 {
		t.Errorf("DeclaredUnused = %d, want 1", result.Stats.DeclaredUnused)
	}
	if result.Stats.DeclaredUsed != 0 {
		t.Errorf("DeclaredUsed = %d, want 0", result.Stats.DeclaredUsed)
	}
}

// TestBazelOverlay_UndeclaredUsed verifies "undeclared_used" when a call
// crosses two Bazel target boundaries but no BUILD dep was declared.
func TestBazelOverlay_UndeclaredUsed(t *testing.T) {
	authTarget := makeTarget("auth0000000000000", "//services/auth:auth_lib", "services/auth")
	billTarget := makeTarget("bill000000000000", "//services/billing:billing_lib", "services/billing")

	authFunc := makeCallEnt("authfunc00000000", "services/auth/auth.py")
	billFunc := makeCallEnt("billfunc00000000", "services/billing/billing.py")

	entities := []types.EntityRecord{authTarget, billTarget, authFunc, billFunc}

	// No BAZEL_DEPENDS_ON — only the CALLS edge.
	rels := []types.RelationshipRecord{
		makeCallEdge("authfunc00000000", "billfunc00000000"),
	}

	result := RunBazelOverlay(entities, rels)

	// Find the undeclared_used status edge.
	found := false
	for _, r := range result.AnnotatedRels {
		if r.Kind == "BAZEL_DEP_STATUS" && r.Properties.Get("status") == "undeclared_used" {
			found = true
		}
	}
	if !found {
		t.Error("expected BAZEL_DEP_STATUS undeclared_used edge not found")
	}
	if result.Stats.UndeclaredUsed != 1 {
		t.Errorf("UndeclaredUsed = %d, want 1", result.Stats.UndeclaredUsed)
	}
}

// TestBazelOverlay_NoBazelTargets verifies that the overlay is a no-op when
// there are no bazel_target entities in the graph.
func TestBazelOverlay_NoBazelTargets(t *testing.T) {
	entities := []types.EntityRecord{
		{ID: "abc123", Name: "SomeFunc", Kind: "SCOPE.Operation", Subtype: "function", SourceFile: "main.go"},
	}
	rels := []types.RelationshipRecord{
		{FromID: "abc123", ToID: "def456", Kind: "CALLS"},
	}

	result := RunBazelOverlay(entities, rels)
	if len(result.AnnotatedRels) != 0 {
		t.Errorf("expected 0 annotated rels, got %d", len(result.AnnotatedRels))
	}
}

// TestBazelOverlay_ImportEdgeCountsAsUsed verifies that IMPORTS edges (not just
// CALLS) are treated as "used" for the purposes of the overlay.
func TestBazelOverlay_ImportEdgeCountsAsUsed(t *testing.T) {
	authTarget := makeTarget("auth0000000000000", "//services/auth:auth_lib", "services/auth")
	billTarget := makeTarget("bill000000000000", "//services/billing:billing_lib", "services/billing")

	authFunc := makeCallEnt("authfunc00000000", "services/auth/auth.py")
	billFunc := makeCallEnt("billfunc00000000", "services/billing/billing.py")

	entities := []types.EntityRecord{authTarget, billTarget, authFunc, billFunc}
	rels := []types.RelationshipRecord{
		makeBazelDep("auth0000000000000", "bill000000000000", "//services/billing:billing_lib"),
		makeImportEdge("authfunc00000000", "billfunc00000000"),
	}

	result := RunBazelOverlay(entities, rels)

	status := findStatus(result.AnnotatedRels, "auth0000000000000", "bill000000000000")
	if status != "declared+used" {
		t.Errorf("IMPORTS edge should count as used: status = %q, want declared+used", status)
	}
}

// TestFindOwnerLabel verifies the package-boundary lookup heuristic.
func TestFindOwnerLabel(t *testing.T) {
	dirToLabel := map[string]string{
		"services/auth":    "//services/auth:auth_lib",
		"services/billing": "//services/billing:billing_lib",
		"common/utils":     "//common/utils:utils",
	}

	cases := []struct {
		sourceFile string
		want       string
	}{
		{"services/auth/auth.py", "//services/auth:auth_lib"},
		{"services/auth/subdir/deep.py", "//services/auth:auth_lib"},
		{"services/billing/Main.java", "//services/billing:billing_lib"},
		{"common/utils/strings.go", "//common/utils:utils"},
		{"orphan/file.go", ""},
	}

	for _, tc := range cases {
		got := findOwnerLabel(tc.sourceFile, dirToLabel)
		if got != tc.want {
			t.Errorf("findOwnerLabel(%q) = %q, want %q", tc.sourceFile, got, tc.want)
		}
	}
}
