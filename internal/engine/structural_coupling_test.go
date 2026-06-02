package engine

import (
	"testing"

	"github.com/cajasmota/archigraph/internal/graph"
)

// mod builds a synthetic Module entity for the fixture graph.
func mod(id, name string) graph.Entity {
	return graph.Entity{
		ID:         id,
		Name:       name,
		Kind:       "Module",
		SourceFile: "",
		Properties: map[string]string{"module": name, "synthetic": "true"},
	}
}

// dep builds a Module→Module DEPENDS_ON edge for the fixture graph.
func dep(from, to string) graph.Relationship {
	return graph.Relationship{
		ID:     from + "->" + to,
		FromID: from,
		ToID:   to,
		Kind:   "DEPENDS_ON",
	}
}

// TestApplyStructuralCoupling_FanOutFixture pins the exact Ca/Ce/instability
// values for a known module graph:
//
//	A DEPENDS_ON B, A DEPENDS_ON C, C DEPENDS_ON B   (B depends on nothing)
//
// Expected (Robert C. Martin's metrics):
//
//	A: Ce=2 (→B,→C), Ca=0 (nobody depends on A)        → I = 2/(0+2) = 1.00
//	B: Ce=0,         Ca=2 (A→B, C→B)                    → I = 0/(2+0) = 0.00
//	C: Ce=1 (→B),    Ca=1 (A→C)                         → I = 1/(1+1) = 0.50
func TestApplyStructuralCoupling_FanOutFixture(t *testing.T) {
	doc := &graph.Document{
		Repo: "acme/widgets",
		Entities: []graph.Entity{
			mod("modA", "core/a"),
			mod("modB", "core/b"),
			mod("modC", "core/c"),
		},
		Relationships: []graph.Relationship{
			dep("modA", "modB"),
			dep("modA", "modC"),
			dep("modC", "modB"),
		},
	}

	stats := ApplyStructuralCoupling(doc)

	if stats.Skipped {
		t.Fatalf("expected pass to run, got Skipped=true")
	}
	if stats.ModulesAnnotated != 3 {
		t.Errorf("ModulesAnnotated = %d, want 3", stats.ModulesAnnotated)
	}
	if stats.DependsOnEdges != 3 {
		t.Errorf("DependsOnEdges = %d, want 3", stats.DependsOnEdges)
	}

	byID := make(map[string]graph.Entity, len(doc.Entities))
	for _, e := range doc.Entities {
		byID[e.ID] = e
	}

	cases := []struct {
		id              string
		wantCa, wantCe  string
		wantInstability string
	}{
		{"modA", "0", "2", "1.00"},
		{"modB", "2", "0", "0.00"},
		{"modC", "1", "1", "0.50"},
	}
	for _, tc := range cases {
		e := byID[tc.id]
		if e.Properties["coupling_computed"] != "true" {
			t.Errorf("%s: coupling_computed = %q, want \"true\"", tc.id, e.Properties["coupling_computed"])
		}
		if got := e.Properties["ca"]; got != tc.wantCa {
			t.Errorf("%s: ca = %q, want %q", tc.id, got, tc.wantCa)
		}
		if got := e.Properties["ce"]; got != tc.wantCe {
			t.Errorf("%s: ce = %q, want %q", tc.id, got, tc.wantCe)
		}
		if got := e.Properties["instability"]; got != tc.wantInstability {
			t.Errorf("%s: instability = %q, want %q", tc.id, got, tc.wantInstability)
		}
	}
}

// TestApplyStructuralCoupling_BriefMinimalFixture asserts the exact scenario in
// the restoration brief: module A imports B and C, B imports nothing.
//
//	A.Ce=2, A.Ca=0, A.instability=1.0
//	B.Ca=1, B.Ce=0, B.instability=0.0
func TestApplyStructuralCoupling_BriefMinimalFixture(t *testing.T) {
	doc := &graph.Document{
		Repo: "acme/widgets",
		Entities: []graph.Entity{
			mod("A", "a"),
			mod("B", "b"),
			mod("C", "c"),
		},
		Relationships: []graph.Relationship{
			dep("A", "B"),
			dep("A", "C"),
		},
	}

	ApplyStructuralCoupling(doc)

	byID := make(map[string]graph.Entity, len(doc.Entities))
	for _, e := range doc.Entities {
		byID[e.ID] = e
	}

	a := byID["A"].Properties
	if a["ce"] != "2" || a["ca"] != "0" || a["instability"] != "1.00" {
		t.Errorf("A: got ca=%s ce=%s instability=%s; want ca=0 ce=2 instability=1.00",
			a["ca"], a["ce"], a["instability"])
	}

	b := byID["B"].Properties
	if b["ca"] != "1" || b["ce"] != "0" || b["instability"] != "0.00" {
		t.Errorf("B: got ca=%s ce=%s instability=%s; want ca=1 ce=0 instability=0.00",
			b["ca"], b["ce"], b["instability"])
	}
}

// TestApplyStructuralCoupling_IsolatedModuleConvention pins the I=0.0 boundary
// convention for a module with no coupling edges at all.
func TestApplyStructuralCoupling_IsolatedModuleConvention(t *testing.T) {
	doc := &graph.Document{
		Repo:     "acme/widgets",
		Entities: []graph.Entity{mod("lonely", "lonely")},
	}

	ApplyStructuralCoupling(doc)

	p := doc.Entities[0].Properties
	if p["ca"] != "0" || p["ce"] != "0" || p["instability"] != "0.00" {
		t.Errorf("isolated module: got ca=%s ce=%s instability=%s; want ca=0 ce=0 instability=0.00",
			p["ca"], p["ce"], p["instability"])
	}
}

// TestApplyStructuralCoupling_SkipsWithoutModules confirms the honest no-op:
// when the module-aggregation pass has not run (no Module entities), nothing is
// stamped and the run is marked Skipped.
func TestApplyStructuralCoupling_SkipsWithoutModules(t *testing.T) {
	doc := &graph.Document{
		Repo: "acme/widgets",
		Entities: []graph.Entity{
			{ID: "fn1", Name: "f", Kind: "SCOPE.Function", SourceFile: "a.go"},
		},
		Relationships: []graph.Relationship{
			{ID: "e1", FromID: "fn1", ToID: "fn1", Kind: "DEPENDS_ON"},
		},
	}

	stats := ApplyStructuralCoupling(doc)

	if !stats.Skipped {
		t.Errorf("expected Skipped=true with no Module entities, got %+v", stats)
	}
	if _, ok := doc.Entities[0].Properties["coupling_computed"]; ok {
		t.Errorf("non-module entity must not be annotated")
	}
}
