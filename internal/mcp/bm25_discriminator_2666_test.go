package mcp

// bm25_discriminator_2666_test.go — verifies that BM25 doc terms include the
// discriminator variable names and literal values from Properties["discriminators"]
// (#2666). A query that mentions a discriminator variable should rank the
// entity higher than an entity that does not discriminate on that variable.

import (
	"testing"

	"github.com/cajasmota/archigraph/internal/graph"
)

// TestBM25_DiscriminatorBoost_VarNameSurfacesEntity verifies that querying for
// a discriminator's variable name boosts the entity that compares against it,
// even when the entity's own name does not contain the variable name.
func TestBM25_DiscriminatorBoost_VarNameSurfacesEntity(t *testing.T) {
	doc := &graph.Document{
		Entities: []graph.Entity{
			{
				ID: "with_disc", Name: "processData",
				Kind: "SCOPE.Operation", SourceFile: "a.ts",
				Properties: map[string]string{
					"discriminators": "checklistType=2",
				},
			},
			{
				ID: "no_disc", Name: "processData2",
				Kind: "SCOPE.Operation", SourceFile: "b.ts",
			},
		},
	}
	idx := BuildBM25(doc)
	hits := idx.Search("checklistType", 10)
	if len(hits) == 0 {
		t.Fatalf("expected at least one hit for 'checklistType'; got none")
	}
	// The entity with the discriminator must be the top hit (or the only hit).
	if hits[0].Entity.ID != "with_disc" {
		t.Errorf("expected top hit to be 'with_disc'; got %q (full ranking=%v)",
			hits[0].Entity.ID, hitIDs(hits))
	}
	// The non-discriminator entity must not outrank the discriminator one.
	for _, h := range hits {
		if h.Entity.ID == "no_disc" {
			if h.Score >= hits[0].Score {
				t.Errorf("no_disc score %v >= with_disc score %v",
					h.Score, hits[0].Score)
			}
		}
	}
}

// TestBM25_DiscriminatorBoost_LiteralStringSurfacesEntity verifies that
// querying for a string literal value boosts the discriminating entity.
// Numeric literals like "2" fall below the 2-char min-token-length of
// tokenize() and are not exercised here; the variable name carries the
// numeric-literal case (see TestBM25_DiscriminatorBoost_VarNameSurfacesEntity).
func TestBM25_DiscriminatorBoost_LiteralStringSurfacesEntity(t *testing.T) {
	doc := &graph.Document{
		Entities: []graph.Entity{
			{
				ID: "with_disc", Name: "handler",
				Kind: "SCOPE.Operation", SourceFile: "a.ts",
				Properties: map[string]string{
					"discriminators": "status=periodic",
				},
			},
			{
				ID: "no_disc", Name: "otherHandler",
				Kind: "SCOPE.Operation", SourceFile: "b.ts",
			},
		},
	}
	idx := BuildBM25(doc)
	hits := idx.Search("periodic", 10)
	if len(hits) == 0 {
		t.Fatalf("expected at least one hit for 'periodic'; got none")
	}
	if hits[0].Entity.ID != "with_disc" {
		t.Errorf("expected top hit to be 'with_disc' (matches literal 'periodic'); got %q ranking=%v",
			hits[0].Entity.ID, hitIDs(hits))
	}
}

// TestBM25_NoDiscriminator_NoBoost verifies that the discriminator boost is
// only applied when Properties["discriminators"] is present. An empty or
// missing property must not affect the doc terms.
func TestBM25_NoDiscriminator_NoBoost(t *testing.T) {
	doc := &graph.Document{
		Entities: []graph.Entity{
			{ID: "a", Name: "foo", Kind: "SCOPE.Operation", SourceFile: "a.ts"},
		},
	}
	idx := BuildBM25(doc)
	hits := idx.Search("checklistType", 10)
	if len(hits) != 0 {
		t.Errorf("expected no hits for 'checklistType' on docs without discriminators; got %v",
			hitIDs(hits))
	}
}

func hitIDs(hits []Hit) []string {
	out := make([]string, 0, len(hits))
	for _, h := range hits {
		out = append(out, h.Entity.ID)
	}
	return out
}
