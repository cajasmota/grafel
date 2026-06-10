package python_test

import (
	"context"
	"strings"
	"testing"

	extreg "github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/extractors"
	"github.com/cajasmota/archigraph/internal/resolve"
	"github.com/cajasmota/archigraph/internal/types"

	// Register the regex framework extractors (Django model/middleware).
	_ "github.com/cajasmota/archigraph/internal/custom/python"
	// Register the tree-sitter base Python extractor so the base class node
	// carries its real module-qualified QualifiedName + the #526 class→field
	// CONTAINS membership edges that MergeWithCustom used to drop.
	_ "github.com/cajasmota/archigraph/internal/extractors/python"
)

// Issue #4402 — MERGE-BOUNDARY ROOT-FIX PROOF.
//
// These tests assert the supersede semantics of extractors.MergeWithCustom
// DIRECTLY at the merge boundary, BEFORE any downstream late-binding pass runs,
// and with the per-language workarounds (#4366 model-node CONTAINS re-emit /
// #4379 settings late-binding rebind) DISABLED in the assertion so we prove the
// ROOT fix — not the prior workarounds — carries QualifiedName + CONTAINS across
// the merge.
//
// Background: the base Python tree-sitter extractor emits a model class as a
// rich SCOPE.Component node carrying (a) its module-qualified QualifiedName
// (e.g. core.models.contract.Contract) and (b) ~45 class→field CONTAINS edges
// (#526). The Django custom extractor emits the SAME class Name as a
// SCOPE.Schema/model node with NO QualifiedName and NO base membership. The old
// MergeWithCustom replaced the base node wholesale, dropping both. This was
// patched per-language (re-emit CONTAINS, late-bind QName). #4402 fixes it at
// the boundary: supersedeBase carries base QualifiedName onto the survivor when
// the custom node left it empty, and unions the base CONTAINS edges (re-keyed to
// the survivor ID) onto the merged entity.

func mergeBoundary4402(t *testing.T, path string) []types.EntityRecord {
	t.Helper()
	file := loadRepro4366(t, "contract_models.py.txt", path)

	base, ok := extreg.Get("python")
	if !ok {
		t.Fatal("base python extractor not registered")
	}
	baseEnts, err := base.Extract(context.Background(), file)
	if err != nil {
		t.Fatalf("base extract: %v", err)
	}
	customEnts, errs := extractors.RunCustomExtractors(context.Background(), file)
	for _, e := range errs {
		t.Fatalf("custom extract: %v", e)
	}

	// Assign deterministic IDs to the INPUTS the same way the pipeline does,
	// so MergeWithCustom re-keys base self-edges against real survivor IDs.
	for i := range baseEnts {
		if baseEnts[i].ID == "" {
			baseEnts[i].ID = baseEnts[i].ComputeID()
		}
	}
	merged := extractors.MergeWithCustom(baseEnts, customEnts)
	for i := range merged {
		if merged[i].ID == "" {
			merged[i].ID = merged[i].ComputeID()
		}
	}
	return merged
}

// findMergedClass returns the single surviving entity named `name`. After the
// merge there must be exactly ONE node per class Name (custom won the identity).
func findMergedClass(t *testing.T, ents []types.EntityRecord, name string) types.EntityRecord {
	t.Helper()
	var found []types.EntityRecord
	for _, e := range ents {
		if e.Name == name {
			found = append(found, e)
		}
	}
	if len(found) != 1 {
		t.Fatalf("expected exactly 1 merged entity named %q, got %d", name, len(found))
	}
	return found[0]
}

// TestIssue4402_QualifiedNameSurvivesMerge proves the merged model node keeps
// the base node's module-qualified QualifiedName even though the Django custom
// node emitted none. This is what makes the settings→middleware dotted path
// resolve via byQualifiedName WITHOUT the #4379 late-binding pass.
func TestIssue4402_QualifiedNameSurvivesMerge(t *testing.T) {
	merged := mergeBoundary4402(t, "core/models/contract.py")

	contract := findMergedClass(t, merged, "Contract")

	// Identity is the CUSTOM node's (Django model), proving the custom node won.
	if contract.Subtype != "model" {
		t.Errorf("merged Contract should keep custom identity subtype=model, got %q", contract.Subtype)
	}
	if contract.Properties["framework"] != "django" {
		t.Errorf("merged Contract should keep custom Django properties, got %v", contract.Properties)
	}

	// ROOT FIX: QualifiedName carried over from the base node.
	want := "core.models.contract.Contract"
	if contract.QualifiedName != want {
		t.Fatalf("merged Contract lost QualifiedName: got %q want %q (base-node QName dropped by merge)", contract.QualifiedName, want)
	}

	// And it resolves via the REAL resolver's byQualifiedName tier with NO
	// late-binding pass run — the #4379 workaround is unnecessary for this.
	idx := resolve.BuildIndex(merged)
	if id, ok := idx.Lookup(want); !ok || id == "" {
		t.Errorf("byQualifiedName lookup of %q failed after merge (id=%q ok=%v)", want, id, ok)
	}
}

// TestIssue4402_BaseContainsSurvivesMerge_WorkaroundDisabled proves the base
// #526 class→field CONTAINS membership survives the merge on its own. We
// STRIP the #4366 workaround edges (the model-node re-emitted CONTAINS, marked
// provenance=INFERRED_FROM_MODEL_FIELD_MEMBERSHIP) from the merged node and
// assert that fields are STILL members via the base edges the merge preserved.
func TestIssue4402_BaseContainsSurvivesMerge_WorkaroundDisabled(t *testing.T) {
	merged := mergeBoundary4402(t, "core/models/contract.py")

	contract := findMergedClass(t, merged, "Contract")

	// Partition the surviving CONTAINS edges into base (#526) vs workaround
	// (#4366 re-emit). The workaround edges are tagged with the model-field
	// membership provenance; everything else CONTAINS is base structural.
	var baseContains, workaroundContains int
	baseToIDs := map[string]bool{}
	for _, r := range contract.Relationships {
		if r.Kind != string(types.RelationshipKindContains) {
			continue
		}
		if r.Properties["provenance"] == "INFERRED_FROM_MODEL_FIELD_MEMBERSHIP" {
			workaroundContains++
			continue
		}
		baseContains++
		baseToIDs[r.ToID] = true
	}

	t.Logf("merged Contract CONTAINS: base(#526)=%d workaround(#4366)=%d", baseContains, workaroundContains)

	// ROOT FIX: the base structural CONTAINS edges survived the merge. Without
	// the #4402 union these would all be gone (the old merge replaced the base
	// node entirely), leaving only the #4366 re-emitted edges.
	if baseContains == 0 {
		t.Fatal("no base #526 CONTAINS edges survived the merge — root fix not carrying base membership")
	}

	// The base CONTAINS edges now hang off the merged Contract node itself.
	// The base extractor embeds these edges in the class node's Relationships
	// with an empty FromID (implicitly owned by the containing entity), so on
	// the survivor they remain implicitly-owned (empty FromID) or, where the
	// base set an explicit self-FromID, are re-keyed to the survivor's ID.
	// Either way they must NOT dangle to the now-gone base node ID.
	baseClassProbe := types.EntityRecord{
		Name: "Contract", Kind: "SCOPE.Component", Subtype: "class",
		SourceFile: contract.SourceFile,
	}
	baseClassID := baseClassProbe.ComputeID()
	containsOwnedBySurvivor := 0
	for _, r := range contract.Relationships {
		if r.Kind != string(types.RelationshipKindContains) ||
			r.Properties["provenance"] == "INFERRED_FROM_MODEL_FIELD_MEMBERSHIP" {
			continue
		}
		if r.FromID == baseClassID {
			t.Errorf("base CONTAINS edge still points FROM the gone base node ID %s (dangling)", baseClassID)
		}
		if r.FromID == "" || r.FromID == contract.ID {
			containsOwnedBySurvivor++
		}
	}
	if containsOwnedBySurvivor == 0 {
		t.Error("base CONTAINS edges are not owned by the surviving Contract node")
	}

	// WORKAROUND-DISABLED membership check: build a view of the merged graph
	// with the #4366 re-emitted CONTAINS edges removed, then confirm the base
	// edges alone still make the model's scalar fields members of Contract.
	stripped := stripWorkaroundContains(merged)

	// Sample a few concrete scalar fields that the base extractor emits as
	// `Contract.<attr>` SCOPE.Schema field nodes and verify each has an inbound
	// base CONTAINS edge from the surviving Contract node.
	for _, field := range []string{
		"Contract.status", "Contract.start_date", "Contract.contract_number",
	} {
		if !baseInboundContains(stripped, field) {
			t.Errorf("field %q lost base CONTAINS membership after merge (workaround disabled)", field)
		}
	}
}

// stripWorkaroundContains returns a copy of ents with every #4366
// workaround-provenance CONTAINS edge removed, so membership assertions rely
// solely on the base #526 edges that survived MergeWithCustom.
func stripWorkaroundContains(ents []types.EntityRecord) []types.EntityRecord {
	out := make([]types.EntityRecord, len(ents))
	copy(out, ents)
	for i := range out {
		if len(out[i].Relationships) == 0 {
			continue
		}
		kept := out[i].Relationships[:0:0]
		for _, r := range out[i].Relationships {
			if r.Kind == string(types.RelationshipKindContains) &&
				r.Properties["provenance"] == "INFERRED_FROM_MODEL_FIELD_MEMBERSHIP" {
				continue
			}
			kept = append(kept, r)
		}
		out[i].Relationships = kept
	}
	return out
}

// baseInboundContains reports whether some entity declares a base (#526)
// CONTAINS edge whose ToID names the given field by its structural-ref form
// (scope:schema:field:...:<owner>.<attr>) or by qualified Name.
func baseInboundContains(ents []types.EntityRecord, fieldQName string) bool {
	for _, e := range ents {
		for _, r := range e.Relationships {
			if r.Kind != string(types.RelationshipKindContains) {
				continue
			}
			if r.ToID == fieldQName || strings.HasSuffix(r.ToID, ":"+fieldQName) ||
				strings.HasSuffix(r.ToID, fieldQName) {
				return true
			}
		}
	}
	return false
}
