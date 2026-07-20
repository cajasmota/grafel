package engine

import (
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
)

// depEntity builds an external_dependency entity the way the manifest
// extractor does, including the embedded DEPENDS_ON edge target ref.
func depEntity(name, pm string) graph.Entity {
	return graph.Entity{
		ID:         "ent:" + pm + ":" + name,
		Name:       name,
		Kind:       "SCOPE.Component",
		Subtype:    "external_dependency",
		SourceFile: "package.json",
	}.WithProperties(map[string]string{
		"package_manager":     pm,
		"external_dependency": "true",
		"dependency_kind":     "runtime",
	},
	)
}

// importEdge mimics the cross/imports DEPENDS_ON(kind=import) edge.
func importEdge(fromFile, pkg string) graph.Relationship {
	return graph.Relationship{
		ID:     fromFile + "->import:" + pkg,
		FromID: fromFile,
		ToID:   "scope:component:import:external:" + pkg,
		Kind:   "DEPENDS_ON",
	}.WithProperties(map[string]string{
		"kind": "import",
	},
	)
}

// dependsOnEdge mimics the manifest DEPENDS_ON(kind=external_dependency) edge
// whose ToID is the canonical dependency ref.
func dependsOnEdge(pm, name string) graph.Relationship {
	ref := "scope:component:external_dep:" + pm + ":" + name
	return graph.Relationship{
		ID:     "proj->" + ref,
		FromID: "scope:component:project:package.json",
		ToID:   ref,
		Kind:   "DEPENDS_ON",
	}.WithProperties(map[string]string{
		"kind":            "external_dependency",
		"package_manager": pm,
	},
	)
}

// TestApplyDependencyHygiene_UsedUnusedPhantom is the core value-asserting
// test for #3640. Fixture:
//
//	A (express)  — declared AND imported  → usage_status=used
//	B (lodash)   — declared, NOT imported → usage_status=unused
//	C (axios)    — imported, NOT declared → phantom (stat only, no entity)
//
// Asserts the SPECIFIC usage_status property persisted on each declared
// dependency entity AND on its DEPENDS_ON edge.
func TestApplyDependencyHygiene_UsedUnusedPhantom(t *testing.T) {
	doc := &graph.Document{
		Entities: []graph.Entity{
			depEntity("express", "npm"), // A: used
			depEntity("lodash", "npm"),  // B: unused
		},
		Relationships: []graph.Relationship{
			// manifest DEPENDS_ON edges for declared deps A and B
			dependsOnEdge("npm", "express"),
			dependsOnEdge("npm", "lodash"),
			// import edges: express (A) is imported, axios (C) is phantom
			importEdge("src/app.js", "express"),
			importEdge("src/http.js", "axios"),
		},
	}

	stats := ApplyDependencyHygiene(doc)

	// --- doc-level stats -------------------------------------------------
	if stats.Declared != 2 {
		t.Errorf("Declared=%d want 2", stats.Declared)
	}
	if stats.Used != 1 {
		t.Errorf("Used=%d want 1", stats.Used)
	}
	if stats.Unused != 1 {
		t.Errorf("Unused=%d want 1", stats.Unused)
	}
	if stats.Phantom != 1 {
		t.Errorf("Phantom=%d want 1 (axios)", stats.Phantom)
	}
	if stats.EntitiesAnnotated != 2 {
		t.Errorf("EntitiesAnnotated=%d want 2", stats.EntitiesAnnotated)
	}
	if stats.EdgesAnnotated != 2 {
		t.Errorf("EdgesAnnotated=%d want 2", stats.EdgesAnnotated)
	}

	// --- entity props: SPECIFIC status, not len>0 ------------------------
	got := map[string]string{}
	for _, e := range doc.Entities {
		if e.Subtype == "external_dependency" {
			got[e.Name] = e.PropGet(usageStatusProp)
		}
	}
	if got["express"] != "used" {
		t.Errorf("express usage_status=%q want \"used\"", got["express"])
	}
	if got["lodash"] != "unused" {
		t.Errorf("lodash usage_status=%q want \"unused\"", got["lodash"])
	}

	// --- phantom (axios) must NOT become a declared dep entity -----------
	for _, e := range doc.Entities {
		if e.Name == "axios" {
			t.Errorf("phantom axios should not be synthesised as an entity, found %+v", e)
		}
	}

	// --- edge props mirror the entity status -----------------------------
	edgeStatus := map[string]string{}
	for _, r := range doc.Relationships {
		if r.Kind == "DEPENDS_ON" && r.PropGet("kind") == "external_dependency" {
			name := depNameFromExternalRef(r.ToID)
			edgeStatus[name] = r.PropGet(usageStatusProp)
		}
	}
	if edgeStatus["express"] != "used" {
		t.Errorf("express edge usage_status=%q want \"used\"", edgeStatus["express"])
	}
	if edgeStatus["lodash"] != "unused" {
		t.Errorf("lodash edge usage_status=%q want \"unused\"", edgeStatus["lodash"])
	}
}

// TestApplyDependencyHygiene_GoModulePrefix verifies a Go-style import path
// that extends the declared module (gin/ginS) classifies the declared module
// as used — exercising deplinker's prefix match through the persisted layer.
func TestApplyDependencyHygiene_GoModulePrefix(t *testing.T) {
	doc := &graph.Document{
		Entities: []graph.Entity{
			depEntity("github.com/gin-gonic/gin", "go_modules"),
		},
		Relationships: []graph.Relationship{
			dependsOnEdge("go_modules", "github.com/gin-gonic/gin"),
			importEdge("cmd/main.go", "github.com/gin-gonic/gin/ginS"),
		},
	}

	ApplyDependencyHygiene(doc)

	if got := doc.Entities[0].PropGet(usageStatusProp); got != "used" {
		t.Errorf("gin usage_status=%q want \"used\" (prefix match)", got)
	}
}

// TestApplyDependencyHygiene_NoManifest is the honest-partial guard: a doc
// with no external_dependency entities yields zero annotations and no panic.
func TestApplyDependencyHygiene_NoManifest(t *testing.T) {
	doc := &graph.Document{
		Entities: []graph.Entity{
			{ID: "f1", Name: "Foo", Kind: "SCOPE.Function", SourceFile: "a.go"},
		},
		Relationships: []graph.Relationship{
			importEdge("a.go", "axios"),
		},
	}

	stats := ApplyDependencyHygiene(doc)

	if stats.EntitiesAnnotated != 0 {
		t.Errorf("EntitiesAnnotated=%d want 0 (no declared deps)", stats.EntitiesAnnotated)
	}
	// axios is still a phantom even with no declared deps.
	if stats.Phantom != 1 {
		t.Errorf("Phantom=%d want 1", stats.Phantom)
	}
}

// TestApplyDependencyHygiene_NilDoc must not panic.
func TestApplyDependencyHygiene_NilDoc(t *testing.T) {
	if got := ApplyDependencyHygiene(nil); got.Declared != 0 {
		t.Errorf("nil doc Declared=%d want 0", got.Declared)
	}
}

// TestDepNameFromExternalRef covers the ref parser, including Maven-style
// names that themselves contain ':' (group:artifact).
func TestDepNameFromExternalRef(t *testing.T) {
	cases := map[string]string{
		"scope:component:external_dep:npm:express":               "express",
		"scope:component:external_dep:go_modules:github.com/x/y": "github.com/x/y",
		"scope:component:external_dep:maven:org.example:my-lib":  "org.example:my-lib",
		"not-a-dep-ref":                    "",
		"scope:component:external_dep:npm": "",
	}
	for ref, want := range cases {
		if got := depNameFromExternalRef(ref); got != want {
			t.Errorf("depNameFromExternalRef(%q)=%q want %q", ref, got, want)
		}
	}
}
