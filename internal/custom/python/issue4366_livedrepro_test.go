package python_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	extreg "github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/extractors"
	"github.com/cajasmota/grafel/internal/resolve"
	"github.com/cajasmota/grafel/internal/types"

	// Register the regex framework extractors under test (Django / SQLAlchemy /
	// Pydantic / the ORM-schema family).
	_ "github.com/cajasmota/grafel/internal/custom/python"
	// Register the tree-sitter base Python extractor so we run the REAL merged
	// pipeline (base #526 field-membership CONTAINS + custom field nodes).
	_ "github.com/cajasmota/grafel/internal/extractors/python"
)

// Issue #4366 LIVE-REPRO — Python ORM/validation field membership.
//
// Generalizes the JS/TS fix #4328 to Python. The regex framework extractors in
// internal/custom/python (Django serializer/form fields, SQLAlchemy
// relationship/column/FK fields, peewee/pony/mongoengine/tortoise/beanie
// columns) emit `<Class>.<field>` field entities WITHOUT a CONTAINS membership
// edge from the owning model/serializer class — and MergeWithCustom REPLACES
// the base-extractor field entity (which DID carry the #526 CONTAINS edge) with
// the custom node of the same Name, dropping membership entirely. The result is
// a forest of orphan field nodes.
//
// We run the ACTUAL base python extractor + the ACTUAL regex framework
// extractors, merge them exactly as the pipeline does (MergeWithCustom), assign
// real entity IDs, build the REAL resolver symbol table (resolve.BuildIndex)
// and assert that field entities are CONTAINS-members of their owning class and
// that relation/typed fields carry a resolving REFERENCES edge.

func loadRepro4366(t *testing.T, base, repoPath string) extreg.FileInput {
	t.Helper()
	p := filepath.Join("testdata", "issue4366", base)
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read repro %s: %v", p, err)
	}
	return extreg.FileInput{Path: repoPath, Language: "python", Content: b}
}

// runMergedPipeline runs the base python extractor + every registered custom
// python_* extractor over one file, merges them the way the real pipeline does,
// assigns deterministic IDs, and returns the merged entity slice.
func runMergedPipeline(t *testing.T, file extreg.FileInput) []types.EntityRecord {
	t.Helper()

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

	merged := extractors.MergeWithCustom(baseEnts, customEnts)
	for i := range merged {
		if merged[i].ID == "" {
			merged[i].ID = merged[i].ComputeID()
		}
	}
	return merged
}

// inboundContains reports whether some entity declares a CONTAINS edge whose
// ToID names the given member (by hex ID, by qualified Name, or via a
// Field:/structural-ref form), i.e. the member is owned by a class rather than
// floating free.
func inboundContains(ents []types.EntityRecord, memberID, memberName string) bool {
	for _, e := range ents {
		for _, r := range e.Relationships {
			if r.Kind != string(types.RelationshipKindContains) {
				continue
			}
			if r.ToID == memberID || r.ToID == memberName ||
				r.ToID == "Field:"+memberName {
				return true
			}
			// Format-A structural-ref class→field stub used by base #526.
			if len(r.ToID) > len(memberName) && r.ToID[len(r.ToID)-len(memberName):] == memberName {
				return true
			}
		}
	}
	return false
}

func fieldEntities(ents []types.EntityRecord) []types.EntityRecord {
	var out []types.EntityRecord
	for _, e := range ents {
		if e.Kind != "SCOPE.Schema" {
			continue
		}
		switch e.Subtype {
		case "column", "field", "serializer_field", "form_field", "":
			// Only count "<Class>.<attr>" qualified field-shaped nodes (skip the
			// bare model class node which is also SCOPE.Schema with subtype "").
			if hasDot(e.Name) {
				out = append(out, e)
			}
		}
	}
	return out
}

func hasDot(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] == '.' {
			return true
		}
	}
	return false
}

func countOrphans(t *testing.T, ents []types.EntityRecord) (orphans int, total int) {
	t.Helper()
	for _, fe := range fieldEntities(ents) {
		total++
		if !inboundContains(ents, fe.ID, fe.Name) {
			orphans++
			t.Logf("ORPHAN field (no inbound CONTAINS): kind=%s subtype=%q name=%q",
				fe.Kind, fe.Subtype, fe.Name)
		}
	}
	return orphans, total
}

func TestIssue4366_DjangoModelFields_AreMembers(t *testing.T) {
	file := loadRepro4366(t, "contract_models.py.txt", "core/models/contract.py")
	ents := runMergedPipeline(t, file)
	orphans, total := countOrphans(t, ents)
	t.Logf("Django contract.py: %d/%d field entities orphaned", orphans, total)
	if total == 0 {
		t.Fatal("no Django model field entities extracted")
	}
	if orphans > 0 {
		t.Errorf("Django model has %d/%d orphan field entities (want 0)", orphans, total)
	}

	// Relational fields carry a REFERENCES edge to the target model. contract.py
	// has `group = models.ForeignKey(Group, ...)` and
	// `recipients = models.ManyToManyField("User", ...)`.
	//
	// #6986 moved the target address from the bare-name `Class:<Target>` stub to
	// the structural `scope:component:class:python:<file>:<Target>` form. What
	// this pins is unchanged — BOTH relational fields still emit a target edge,
	// and each still names the same target model — only the dialect moved.
	groupRef := extreg.BuildComponentStructuralRef("python", "core/models/contract.py", "Group")
	userRef := extreg.BuildComponentStructuralRef("python", "core/models/contract.py", "User")
	if !hasReferencesTo(ents, groupRef) {
		t.Errorf("expected REFERENCES edge to %s (ForeignKey target)", groupRef)
	}
	if !hasReferencesTo(ents, userRef) {
		t.Errorf("expected REFERENCES edge to %s (ManyToManyField string target)", userRef)
	}
}

func TestIssue4366_SQLAlchemyFields_AreMembers(t *testing.T) {
	file := loadRepro4366(t, "orders_sqlalchemy.py.txt", "app/models/orders.py")
	ents := runMergedPipeline(t, file)
	orphans, total := countOrphans(t, ents)
	t.Logf("SQLAlchemy orders.py: %d/%d field entities orphaned", orphans, total)
	if total == 0 {
		t.Fatal("no SQLAlchemy field entities extracted")
	}
	if orphans > 0 {
		t.Errorf("SQLAlchemy model has %d/%d orphan field entities (want 0)", orphans, total)
	}

	// Relationship target must carry a RESOLVING REFERENCES edge. That is the
	// property this has always pinned — the old form asserted it by looking the
	// `Class:Order` stub up in the symbol table. #6986 re-addressed the edge to
	// `scope:component:class:python:<file>:<Target>`, which the bare-name
	// `Index.Lookup` tier does not serve, so the same property is now pinned one
	// layer lower and more strictly: drive the REAL production path
	// (BuildIndex → ReferencesEmbedded) and require the emitted edge's ToID to
	// come back REWRITTEN to a real entity ID. Weaker would be to drop the
	// resolution half and keep only "an edge exists"; this keeps both.
	orderRef := extreg.BuildComponentStructuralRef("python", "app/models/orders.py", "Order")
	customerRef := extreg.BuildComponentStructuralRef("python", "app/models/orders.py", "Customer")
	if !hasReferencesTo(ents, orderRef) && !hasReferencesTo(ents, customerRef) {
		t.Errorf("expected a REFERENCES edge to a related SQLAlchemy model (%s / %s)", orderRef, customerRef)
	}
	ids := map[string]bool{}
	for i := range ents {
		ids[ents[i].ID] = true
	}
	idx := resolve.BuildIndex(ents)
	resolve.ReferencesEmbedded(ents, idx)
	boundTargets := 0
	for i := range ents {
		for _, r := range ents[i].Relationships {
			if r.Kind != string(types.RelationshipKindReferences) {
				continue
			}
			isFieldTarget := false
			for _, p := range r.Properties {
				if p.K == "ref_kind" && p.V == "field_target_type" {
					isFieldTarget = true
				}
			}
			if !isFieldTarget {
				continue
			}
			if !ids[r.ToID] {
				t.Errorf("%s -[REFERENCES]-> %q did NOT bind to an entity ID (dangling stub)",
					ents[i].Name, r.ToID)
				continue
			}
			boundTargets++
		}
	}
	if boundTargets == 0 {
		t.Error("no field_target_type edge bound: the resolution half of this assertion is vacuous")
	}
}

func TestIssue4366_PydanticFields_AreMembers(t *testing.T) {
	file := loadRepro4366(t, "account_pydantic.py.txt", "app/schemas/account.py")
	ents := runMergedPipeline(t, file)
	orphans, total := countOrphans(t, ents)
	t.Logf("Pydantic account.py: %d/%d field entities orphaned", orphans, total)
	if total == 0 {
		t.Fatal("no Pydantic field entities extracted")
	}
	if orphans > 0 {
		t.Errorf("Pydantic model has %d/%d orphan field entities (want 0)", orphans, total)
	}
}

func hasReferencesTo(ents []types.EntityRecord, toID string) bool {
	for _, e := range ents {
		for _, r := range e.Relationships {
			if r.Kind == string(types.RelationshipKindReferences) && r.ToID == toID {
				return true
			}
		}
	}
	return false
}
