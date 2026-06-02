package engine

import (
	"testing"

	"github.com/cajasmota/archigraph/internal/graph"
)

// TestApplyLibBoundary_FirstVsThirdParty is the core value-asserting test for
// #3638. Fixture: module A imports internal pkg B (a local/relative import →
// first_party) and external lib C (a manifest external dependency → third_party).
// We assert the A→B edge gets boundary=first_party and the A→C edge gets
// boundary=third_party — not merely that some annotation happened.
func TestApplyLibBoundary_FirstVsThirdParty(t *testing.T) {
	const (
		fileA = "scope:component:file:src/a.go"
		pkgB  = "scope:component:import:local:./b"
		libC  = "scope:component:external_dep:gomod:github.com/external/c"
	)

	doc := &graph.Document{
		Entities: []graph.Entity{
			// External library C — the manifest external_dependency carrier.
			{
				ID:      libC,
				Name:    "github.com/external/c",
				Kind:    "SCOPE.Component",
				Subtype: "external_dependency",
				Properties: map[string]string{
					"package_manager":     "gomod",
					"external_dependency": "true",
				},
			},
		},
		Relationships: []graph.Relationship{
			// A → B: local/internal import → first_party.
			{
				ID:     "a->b",
				FromID: fileA,
				ToID:   pkgB,
				Kind:   "DEPENDS_ON",
				Properties: map[string]string{
					"kind":                "import",
					"is_local":            "true",
					"external_dependency": "false",
				},
			},
			// A → C: manifest external dependency edge → third_party.
			{
				ID:     "a->c",
				FromID: "scope:component:project:go.mod",
				ToID:   libC,
				Kind:   "DEPENDS_ON",
				Properties: map[string]string{
					"kind":            "external_dependency",
					"package_manager": "gomod",
				},
			},
		},
	}

	stats := ApplyLibBoundary(doc)

	// Edge A→B must be first_party.
	abEdge := findRel(t, doc, "a->b")
	if got := abEdge.Properties[boundaryProp]; got != boundaryFirstParty {
		t.Errorf("A->B (internal import) boundary=%q, want %q", got, boundaryFirstParty)
	}

	// Edge A→C must be third_party.
	acEdge := findRel(t, doc, "a->c")
	if got := acEdge.Properties[boundaryProp]; got != boundaryThirdParty {
		t.Errorf("A->C (external dep) boundary=%q, want %q", got, boundaryThirdParty)
	}

	// The external_dependency entity C must also be stamped third_party.
	cEnt := findEnt(t, doc, libC)
	if got := cEnt.Properties[boundaryProp]; got != boundaryThirdParty {
		t.Errorf("entity C boundary=%q, want %q", got, boundaryThirdParty)
	}

	if stats.FirstParty != 1 {
		t.Errorf("FirstParty=%d, want 1", stats.FirstParty)
	}
	if stats.ThirdParty != 1 {
		t.Errorf("ThirdParty=%d, want 1", stats.ThirdParty)
	}
	if stats.EntitiesAnnotated != 1 {
		t.Errorf("EntitiesAnnotated=%d, want 1", stats.EntitiesAnnotated)
	}
}

// TestApplyLibBoundary_CodeToCodeAndAmbiguous covers the remaining branches:
// a repo-internal code-to-code DEPENDS_ON (both endpoints owned, neither
// external) is first_party, an import edge resolved external is third_party, and
// an edge with no locality/kind signal and an unresolved target is left
// UNANNOTATED (honest-partial).
func TestApplyLibBoundary_CodeToCodeAndAmbiguous(t *testing.T) {
	doc := &graph.Document{
		Entities: []graph.Entity{
			{ID: "ent:order", Kind: "SCOPE.Component", Subtype: "class"},
			{ID: "ent:store", Kind: "SCOPE.Component", Subtype: "class"},
		},
		Relationships: []graph.Relationship{
			// Code-to-code: both endpoints owned, neither external → first_party.
			{ID: "code", FromID: "ent:order", ToID: "ent:store", Kind: "DEPENDS_ON"},
			// External import → third_party.
			{
				ID:     "ext-import",
				FromID: "scope:component:file:src/a.go",
				ToID:   "scope:component:import:external:lodash",
				Kind:   "DEPENDS_ON",
				Properties: map[string]string{
					"kind":     "import",
					"is_local": "false",
				},
			},
			// Ambiguous: no kind/locality, target not in this document.
			{ID: "ambig", FromID: "ent:order", ToID: "unresolved:target", Kind: "DEPENDS_ON"},
		},
	}

	stats := ApplyLibBoundary(doc)

	if got := findRel(t, doc, "code").Properties[boundaryProp]; got != boundaryFirstParty {
		t.Errorf("code-to-code boundary=%q, want %q", got, boundaryFirstParty)
	}
	if got := findRel(t, doc, "ext-import").Properties[boundaryProp]; got != boundaryThirdParty {
		t.Errorf("external import boundary=%q, want %q", got, boundaryThirdParty)
	}
	// The ambiguous edge must NOT be annotated.
	if ambig := findRel(t, doc, "ambig"); ambig.Properties != nil {
		if _, ok := ambig.Properties[boundaryProp]; ok {
			t.Errorf("ambiguous edge was annotated, want unannotated (honest-partial)")
		}
	}
	if stats.Ambiguous != 1 {
		t.Errorf("Ambiguous=%d, want 1", stats.Ambiguous)
	}
	if stats.FirstParty != 1 || stats.ThirdParty != 1 {
		t.Errorf("FirstParty=%d ThirdParty=%d, want 1/1", stats.FirstParty, stats.ThirdParty)
	}
}

// TestApplyLibBoundary_NilSafe asserts the pass is safe on an empty document.
func TestApplyLibBoundary_NilSafe(t *testing.T) {
	if got := ApplyLibBoundary(nil); got.EdgesConsidered != 0 {
		t.Errorf("nil doc EdgesConsidered=%d, want 0", got.EdgesConsidered)
	}
}

func findRel(t *testing.T, doc *graph.Document, id string) *graph.Relationship {
	t.Helper()
	for i := range doc.Relationships {
		if doc.Relationships[i].ID == id {
			return &doc.Relationships[i]
		}
	}
	t.Fatalf("relationship %q not found", id)
	return nil
}

func findEnt(t *testing.T, doc *graph.Document, id string) *graph.Entity {
	t.Helper()
	for i := range doc.Entities {
		if doc.Entities[i].ID == id {
			return &doc.Entities[i]
		}
	}
	t.Fatalf("entity %q not found", id)
	return nil
}
