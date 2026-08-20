// Package resolve — #6369: an import placeholder must not poison the global
// bare-name index.
//
// A dozen extractors mint one `SCOPE.Component` per import statement, marked
// `Subtype:"import"` (css/scss/less, graphql, vue, kotlin, razor, proto,
// markdown, cpp, just, fish, javascript, cross/imports). BuildIndex indexed
// every one of them in `byName` exactly like a real declaration, so a
// placeholder whose name collides with a real type flipped that name
// AMBIGUOUS globally — and every bare-name edge to it, repo-wide, went
// unresolved. Measured on #6369: adding ONE file containing `open Acme.Animal`
// dropped both cross-file EXTENDS to `Animal` in a file that imported nothing.
//
// The fix is scoped, not a blanket skip: a placeholder may still OCCUPY an
// otherwise-unclaimed name, because for css `@import` chains and graphql
// federation stubs the placeholder is the only target the bare-name
// IMPORTS/FEDERATES edge has (TestImportPlaceholderStillResolvesOwnEdge_6369
// pins that). What it may never do is displace, or collide with, a real
// declaration.
package resolve

import (
	"testing"

	"github.com/cajasmota/grafel/internal/types"
)

const (
	realAnimalID6369   = "1860328bfaee8f13"
	dogID6369          = "aaaa000000000001"
	serviceID6369      = "aaaa000000000002"
	placeholderID6369  = "eca4ea80cdd6b84d"
	placeholder2ID6369 = "eca4ea80cdd6b84e"
)

// baseFixture6369 is the #6369 reproduction, transposed onto an extractor that
// actually stamps the marker (vue: placeholder named after the module's last
// segment, real component named after its own file).
//
//	src/domain/base.vue    declares component  Animal        (the real target)
//	src/app/impl.vue       Dog, Service        EXTENDS "Animal" by bare name
//
// Both EXTENDS resolve cross-file on this input. Nothing in src/app imports
// anything; the two edges are the "innocent bystanders" of the defect.
func baseFixture6369() []types.EntityRecord {
	return []types.EntityRecord{
		{
			ID: realAnimalID6369, Kind: "SCOPE.Component", Name: "Animal",
			Subtype: "class", SourceFile: "src/domain/base.vue", Language: "vue",
		},
		{
			ID: dogID6369, Kind: "SCOPE.Component", Name: "Dog",
			Subtype: "class", SourceFile: "src/app/impl.vue", Language: "vue",
			Relationships: []types.RelationshipRecord{
				{FromID: dogID6369, ToID: "Animal", Kind: "EXTENDS"},
			},
		},
		{
			ID: serviceID6369, Kind: "SCOPE.Component", Name: "Service",
			Subtype: "class", SourceFile: "src/app/impl.vue", Language: "vue",
			Relationships: []types.RelationshipRecord{
				{FromID: serviceID6369, ToID: "Animal", Kind: "EXTENDS"},
			},
		},
	}
}

// importPlaceholder6369 is the record shape emitted per import statement by
// every extractor that stamps the marker — e.g. vue/extractor.go:1361,
// css/css.go:253, graphql/graphql.go:402.
func importPlaceholder6369(id, name, file string) types.EntityRecord {
	return types.EntityRecord{
		ID: id, Kind: "SCOPE.Component", Name: name, Subtype: "import",
		SourceFile: file, Language: "vue",
		Relationships: []types.RelationshipRecord{
			{FromID: file, ToID: name, Kind: "IMPORTS"},
		},
	}
}

// resolveEdges6369 runs the production resolution path (BuildIndex →
// ReferencesEmbedded) and returns owner-name → resolved ToID for every
// embedded relationship of kind relKind.
func resolveEdges6369(t *testing.T, recs []types.EntityRecord, relKind string) map[string]string {
	t.Helper()
	idx := BuildIndex(recs)
	ReferencesEmbedded(recs, idx)
	out := map[string]string{}
	for i := range recs {
		for _, r := range recs[i].Relationships {
			if r.Kind == relKind {
				out[recs[i].Name] = r.ToID
			}
		}
	}
	return out
}

// TestImportPlaceholderDoesNotDropCrossFileEdges_6369 is the assertion that
// matters: a cross-file edge that resolves TODAY must still resolve after an
// unrelated file adds a colliding import. Baseline is asserted first so the
// test cannot pass vacuously on a fixture that never resolved.
func TestImportPlaceholderDoesNotDropCrossFileEdges_6369(t *testing.T) {
	base := resolveEdges6369(t, baseFixture6369(), "EXTENDS")
	for _, owner := range []string{"Dog", "Service"} {
		if base[owner] != realAnimalID6369 {
			t.Fatalf("baseline is vacuous: %s EXTENDS = %q, want %q",
				owner, base[owner], realAnimalID6369)
		}
	}

	cases := []struct {
		name string
		recs func() []types.EntityRecord
	}{
		{
			// The #6369 reproduction verbatim: the colliding import arrives
			// AFTER the real declaration.
			name: "placeholder after real declaration",
			recs: func() []types.EntityRecord {
				return append(baseFixture6369(),
					importPlaceholder6369(placeholderID6369, "Animal", "src/other/collide.vue"))
			},
		},
		{
			// Extraction order is not stable across runs; the placeholder
			// winning the slot first must not change the outcome.
			name: "placeholder before real declaration",
			recs: func() []types.EntityRecord {
				return append([]types.EntityRecord{
					importPlaceholder6369(placeholderID6369, "Animal", "src/other/collide.vue"),
				}, baseFixture6369()...)
			},
		},
		{
			// #6369 variant R3 — two importers. Two placeholders collide with
			// each other before the real declaration is ever seen.
			name: "two placeholders before real declaration",
			recs: func() []types.EntityRecord {
				return append([]types.EntityRecord{
					importPlaceholder6369(placeholderID6369, "Animal", "src/other/collide.vue"),
					importPlaceholder6369(placeholder2ID6369, "Animal", "src/other/collide2.vue"),
				}, baseFixture6369()...)
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			recs := tc.recs()
			got := resolveEdges6369(t, recs, "EXTENDS")
			for _, owner := range []string{"Dog", "Service"} {
				if got[owner] != realAnimalID6369 {
					t.Errorf("%s EXTENDS Animal = %q, want the real declaration %q "+
						"— an import placeholder in an unrelated file dropped a cross-file edge (#6369)",
						owner, got[owner], realAnimalID6369)
				}
			}
			// Both index construction paths are production-wired; the fix must
			// land on both (BuildIndexFromModulesOrdered mirrors BuildIndex).
			assertFullIndexParity(t, tc.recs())
		})
	}
}

// TestImportPlaceholderStillResolvesOwnEdge_6369 guards the load-bearing case
// the blanket "never index placeholders by name" version of this fix breaks.
//
// css `@import "theme.css"` and graphql `extend type User` both emit a
// bare-name edge (IMPORTS / FEDERATES) whose ToID is the placeholder's own
// name; when the module is external there is NO other entity by that name in
// the graph. rewriteOneWithCaller only consults the locality tiers on
// statusAmbiguous, so dropping the placeholder from byName leaves these edges
// permanently unresolved.
func TestImportPlaceholderStillResolvesOwnEdge_6369(t *testing.T) {
	t.Run("css @import to an external module", func(t *testing.T) {
		recs := []types.EntityRecord{
			{
				ID: placeholderID6369, Kind: "SCOPE.Component", Name: "theme.css",
				Subtype: "import", SourceFile: "src/app.css", Language: "css",
				Relationships: []types.RelationshipRecord{
					{FromID: "src/app.css", ToID: "theme.css", Kind: "IMPORTS"},
				},
			},
		}
		got := resolveEdges6369(t, recs, "IMPORTS")
		if got["theme.css"] != placeholderID6369 {
			t.Errorf("css @import edge = %q, want the placeholder %q — the placeholder is "+
				"the only target this edge has (#6369)", got["theme.css"], placeholderID6369)
		}
	})

	t.Run("graphql federation stub with no in-repo owner", func(t *testing.T) {
		recs := []types.EntityRecord{
			{
				ID: placeholderID6369, Kind: "SCOPE.Component", Name: "User",
				Subtype: "import", SourceFile: "subgraph/orders.graphql", Language: "graphql",
				Relationships: []types.RelationshipRecord{
					{FromID: "subgraph/orders.graphql", ToID: "User", Kind: "FEDERATES"},
				},
			},
		}
		got := resolveEdges6369(t, recs, "FEDERATES")
		if got["User"] != placeholderID6369 {
			t.Errorf("FEDERATES edge = %q, want the federation stub %q (#6369)",
				got["User"], placeholderID6369)
		}
	})

	t.Run("federation stub yields to the in-repo owning type", func(t *testing.T) {
		recs := []types.EntityRecord{
			{
				ID: realAnimalID6369, Kind: "SCOPE.Component", Name: "User",
				Subtype: "type", SourceFile: "subgraph/users.graphql", Language: "graphql",
			},
			{
				ID: placeholderID6369, Kind: "SCOPE.Component", Name: "User",
				Subtype: "import", SourceFile: "subgraph/orders.graphql", Language: "graphql",
				Relationships: []types.RelationshipRecord{
					{FromID: "subgraph/orders.graphql", ToID: "User", Kind: "FEDERATES"},
				},
			},
		}
		got := resolveEdges6369(t, recs, "FEDERATES")
		if got["User"] != realAnimalID6369 {
			t.Errorf("FEDERATES edge = %q, want the canonical type %q (#6369)",
				got["User"], realAnimalID6369)
		}
	})
}

// TestImportPlaceholderAmbiguityStillHonoured_6369 pins the boundary: the fix
// suppresses ambiguity between a placeholder and a real declaration, NOT
// ambiguity between two real declarations, and not the placeholder-only case
// where no real declaration exists at all.
func TestImportPlaceholderAmbiguityStillHonoured_6369(t *testing.T) {
	t.Run("two real declarations stay ambiguous", func(t *testing.T) {
		recs := append(baseFixture6369(), types.EntityRecord{
			ID: placeholder2ID6369, Kind: "SCOPE.Component", Name: "Animal",
			Subtype: "class", SourceFile: "src/other/base2.vue", Language: "vue",
		})
		idx := BuildIndex(recs)
		if !idx.ambigName["Animal"] {
			t.Errorf("two real declarations of Animal must stay ambiguous (#6369)")
		}
		if _, ok := idx.byName["Animal"]; ok {
			t.Errorf("byName must not hold an arbitrary winner for an ambiguous name")
		}
	})

	t.Run("two placeholders with no real declaration stay ambiguous", func(t *testing.T) {
		recs := []types.EntityRecord{
			importPlaceholder6369(placeholderID6369, "Animal", "src/other/collide.vue"),
			importPlaceholder6369(placeholder2ID6369, "Animal", "src/other/collide2.vue"),
		}
		idx := BuildIndex(recs)
		if !idx.ambigName["Animal"] {
			t.Errorf("two distinct placeholders must not silently pick a winner (#6369)")
		}
	})
}
