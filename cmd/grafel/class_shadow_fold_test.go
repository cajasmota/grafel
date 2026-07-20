package main

import (
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
)

// TestClassShadowFold_NoLine0Shadows asserts the #1613 fold invariant on the
// Django fixture: every class resolves to a single node with a real start_line
// and qualified_name, and NO line-0 INFERRED_FROM_CLASS_HIERARCHY shadow
// remains. The fixture's models.py declares `Article(models.Model)` and
// `Author(models.Model)`, each of which the pipeline would otherwise emit as
// up to three nodes (Model framework node + SCOPE.Component/class AST node +
// INFERRED class-hierarchy shadow).
func TestClassShadowFold_NoLine0Shadows(t *testing.T) {
	doc := runIndexerOn(t, "testdata/django_app", "django_app", nil)

	var inferred, classLikeLine0 int
	byName := map[string][]graph.Entity{}
	for _, e := range doc.Entities {
		byName[e.Name] = append(byName[e.Name], e)
		if e.PropGet("provenance") == "INFERRED_FROM_CLASS_HIERARCHY" {
			inferred++
		}
		if classLikeComponentSubtypes[e.Subtype] && e.StartLine == 0 {
			classLikeLine0++
		}
	}

	if inferred != 0 {
		t.Errorf("expected 0 INFERRED_FROM_CLASS_HIERARCHY shadows after fold, got %d", inferred)
	}
	if classLikeLine0 != 0 {
		t.Errorf("expected 0 class-like entities at start_line=0 after fold, got %d", classLikeLine0)
	}

	// Each declared class must resolve to exactly ONE class-declaration node
	// with a real line span and a qualified_name. The ORM table sentinel
	// (SCOPE.Component/orm_model_sentinel) is an intentional, search-excluded
	// edge anchor named after the table — it is NOT a class shadow and is out
	// of scope for #1613, so we exclude it from the per-class node count.
	for _, name := range []string{"Article", "Author"} {
		var decls []graph.Entity
		for _, e := range byName[name] {
			if e.Subtype == "orm_model_sentinel" {
				continue
			}
			decls = append(decls, e)
		}
		if len(decls) != 1 {
			t.Errorf("%s: expected exactly 1 class-declaration node after fold, got %d: %+v", name, len(decls), kinds(decls))
			continue
		}
		e := decls[0]
		if e.StartLine == 0 {
			t.Errorf("%s: surviving node has start_line=0 (want real line)", name)
		}
		if e.QualifiedName == "" {
			t.Errorf("%s: surviving node has empty qualified_name", name)
		}
		if e.PropGet("provenance") == "INFERRED_FROM_CLASS_HIERARCHY" {
			t.Errorf("%s: surviving node still carries the inference provenance", name)
		}
	}
}

// TestClassShadowFold_NoNewDanglingEdges asserts the fold does not introduce
// dangling hex-id edge endpoints relative to the unfolded graph (folding only
// re-points endpoints onto a surviving node that is always present).
func TestClassShadowFold_NoNewDanglingEdges(t *testing.T) {
	t.Setenv("GRAFEL_DISABLE_1613_FOLD", "1")
	unfolded := runIndexerOn(t, "testdata/django_app", "django_app", nil)
	t.Setenv("GRAFEL_DISABLE_1613_FOLD", "")
	folded := runIndexerOn(t, "testdata/django_app", "django_app", nil)

	if d := danglingHexEndpoints(folded); d > danglingHexEndpoints(unfolded) {
		t.Errorf("fold introduced new dangling hex endpoints: folded=%d unfolded=%d",
			d, danglingHexEndpoints(unfolded))
	}
}

func danglingHexEndpoints(doc *graph.Document) int {
	ids := make(map[string]bool, len(doc.Entities))
	for _, e := range doc.Entities {
		ids[e.ID] = true
	}
	n := 0
	for _, r := range doc.Relationships {
		if isHex16(r.FromID) && !ids[r.FromID] {
			n++
		}
		if isHex16(r.ToID) && !ids[r.ToID] {
			n++
		}
	}
	return n
}

func kinds(es []graph.Entity) []string {
	out := make([]string, len(es))
	for i, e := range es {
		out[i] = e.Kind + "/" + e.Subtype
	}
	return out
}
