package deplinker

import (
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
)

func makeEntity(name, kind, subtype, sourceFile string, props map[string]string) graph.Entity {
	return graph.Entity{
		ID:         name + "-id",
		Name:       name,
		Kind:       kind,
		Subtype:    subtype,
		SourceFile: sourceFile,
	}.WithProperties(props)
}

func makeRel(fromID, toID, kind string, props map[string]string) graph.Relationship {
	return graph.Relationship{
		ID:     fromID + "->" + toID,
		FromID: fromID,
		ToID:   toID,
		Kind:   kind,
	}.WithProperties(props)
}

// ---------------------------------------------------------------------------
// TestUsedDep: a declared dep with a matching IMPORTS edge → "used"
// ---------------------------------------------------------------------------

func TestUsedDep(t *testing.T) {
	entities := []graph.Entity{
		makeEntity("express", "SCOPE.Component", "external_dependency", "package.json", map[string]string{
			"package_manager": "npm",
			"version":         "^4.18.0",
			"dependency_kind": "runtime",
		}),
	}
	rels := []graph.Relationship{
		makeRel("src/app.js", "scope:component:import:external:express", "DEPENDS_ON", map[string]string{
			"kind": "import",
		}),
	}

	rep := analyzeEntities(entities, rels)

	if rep.Declared != 1 {
		t.Errorf("declared=%d want 1", rep.Declared)
	}
	if rep.Used != 1 {
		t.Errorf("used=%d want 1", rep.Used)
	}
	if rep.Unused != 0 {
		t.Errorf("unused=%d want 0", rep.Unused)
	}
	if len(rep.Packages) != 1 {
		t.Fatalf("packages len=%d want 1", len(rep.Packages))
	}
	if rep.Packages[0].Status != StatusUsed {
		t.Errorf("status=%q want %q", rep.Packages[0].Status, StatusUsed)
	}
}

// ---------------------------------------------------------------------------
// TestUnusedDep: declared dep with no matching import → "unused"
// ---------------------------------------------------------------------------

func TestUnusedDep(t *testing.T) {
	entities := []graph.Entity{
		makeEntity("lodash", "SCOPE.Component", "external_dependency", "package.json", map[string]string{
			"package_manager": "npm",
			"version":         "4.17.21",
			"dependency_kind": "runtime",
		}),
	}
	rels := []graph.Relationship{
		makeRel("src/app.js", "scope:component:import:external:express", "DEPENDS_ON", map[string]string{
			"kind": "import",
		}),
	}

	rep := analyzeEntities(entities, rels)

	if rep.Unused != 1 {
		t.Errorf("unused=%d want 1", rep.Unused)
	}
	if rep.Packages[0].Status != StatusUnused {
		t.Errorf("status=%q want %q", rep.Packages[0].Status, StatusUnused)
	}
}

// ---------------------------------------------------------------------------
// TestPhantomDep: imported package not in any manifest → "phantom"
// ---------------------------------------------------------------------------

func TestPhantomDep(t *testing.T) {
	// No declared external_dependency entities.
	entities := []graph.Entity{}
	rels := []graph.Relationship{
		makeRel("src/foo.js", "scope:component:import:external:axios", "DEPENDS_ON", map[string]string{
			"kind": "import",
		}),
	}

	rep := analyzeEntities(entities, rels)

	if rep.Phantom != 1 {
		t.Errorf("phantom=%d want 1", rep.Phantom)
	}
	if len(rep.Packages) != 1 {
		t.Fatalf("packages len=%d want 1", len(rep.Packages))
	}
	if rep.Packages[0].Status != StatusPhantom {
		t.Errorf("status=%q want %q", rep.Packages[0].Status, StatusPhantom)
	}
	if rep.Packages[0].Name != "axios" {
		t.Errorf("name=%q want axios", rep.Packages[0].Name)
	}
}

// ---------------------------------------------------------------------------
// TestGoModulePrefixMatch: import "github.com/gin-gonic/gin/ginS" matches
// declared "github.com/gin-gonic/gin" via prefix.
// ---------------------------------------------------------------------------

func TestGoModulePrefixMatch(t *testing.T) {
	entities := []graph.Entity{
		makeEntity("github.com/gin-gonic/gin", "SCOPE.Component", "external_dependency", "go.mod", map[string]string{
			"package_manager": "go_modules",
			"version":         "v1.9.1",
			"dependency_kind": "runtime",
		}),
	}
	rels := []graph.Relationship{
		makeRel("cmd/main.go", "scope:component:import:external:github.com/gin-gonic/gin/ginS", "DEPENDS_ON", map[string]string{
			"kind": "import",
		}),
	}

	rep := analyzeEntities(entities, rels)

	if rep.Used != 1 {
		t.Errorf("used=%d want 1 (prefix match failed)", rep.Used)
	}
}

// ---------------------------------------------------------------------------
// TestMixedUsage: multi-dep scenario.
// ---------------------------------------------------------------------------

func TestMixedUsage(t *testing.T) {
	entities := []graph.Entity{
		makeEntity("express", "SCOPE.Component", "external_dependency", "package.json", map[string]string{
			"package_manager": "npm", "version": "4.x", "dependency_kind": "runtime",
		}),
		makeEntity("lodash", "SCOPE.Component", "external_dependency", "package.json", map[string]string{
			"package_manager": "npm", "version": "4.x", "dependency_kind": "runtime",
		}),
		makeEntity("jest", "SCOPE.Component", "external_dependency", "package.json", map[string]string{
			"package_manager": "npm", "version": "29.x", "dependency_kind": "dev",
		}),
	}
	rels := []graph.Relationship{
		// express is used
		makeRel("src/app.js", "scope:component:import:external:express", "DEPENDS_ON", map[string]string{"kind": "import"}),
		// lodash is unused (no import edge)
		// jest is unused
		// axios is a phantom
		makeRel("src/http.js", "scope:component:import:external:axios", "DEPENDS_ON", map[string]string{"kind": "import"}),
	}

	rep := analyzeEntities(entities, rels)

	if rep.Declared != 3 {
		t.Errorf("declared=%d want 3", rep.Declared)
	}
	if rep.Used != 1 {
		t.Errorf("used=%d want 1", rep.Used)
	}
	if rep.Unused != 2 {
		t.Errorf("unused=%d want 2", rep.Unused)
	}
	if rep.Phantom != 1 {
		t.Errorf("phantom=%d want 1", rep.Phantom)
	}
}

// ---------------------------------------------------------------------------
// TestNilDoc: Analyze(nil) returns empty Report.
// ---------------------------------------------------------------------------

func TestNilDoc(t *testing.T) {
	rep := Analyze(nil)
	if rep.Declared != 0 || rep.Phantom != 0 {
		t.Error("expected zero report for nil doc")
	}
}

// ---------------------------------------------------------------------------
// TestDependencyKindPreserved: dependency_kind flows through to PackageEntry.
// ---------------------------------------------------------------------------

func TestDependencyKindPreserved(t *testing.T) {
	entities := []graph.Entity{
		makeEntity("react", "SCOPE.Component", "external_dependency", "package.json", map[string]string{
			"package_manager": "npm", "dependency_kind": "peer",
		}),
	}
	rep := analyzeEntities(entities, nil)

	if len(rep.Packages) != 1 {
		t.Fatalf("packages len=%d want 1", len(rep.Packages))
	}
	if rep.Packages[0].DependencyKind != "peer" {
		t.Errorf("dependency_kind=%q want peer", rep.Packages[0].DependencyKind)
	}
}
