package manifest

import "testing"

// realNimble is a representative nimble manifest (shape taken from real
// published packages such as jester / norm). It exercises the statement and
// call requires forms, multi-dep directives, the `nim` compiler floor, a bare
// (unconstrained) dep, and a per-task (dev) requirement.
const realNimble = `# Package

version       = "0.5.0"
author        = "Jane Doe"
description   = "A small Nim web service"
license       = "MIT"
srcDir        = "src"

# Dependencies

requires "nim >= 1.6.0"
requires "jester >= 0.5.0"
requires "norm", "debby >= 0.1.0"
requires("waterpark")

taskRequires "test", "balls >= 3.0"
`

func TestNimble_RuntimeDeps(t *testing.T) {
	deps := depEntities(runExtract(t, "webservice.nimble", realNimble))
	// 5 runtime (nim, jester, norm, debby, waterpark) + 1 dev (balls) = 6.
	if len(deps) != 6 {
		t.Fatalf("expected 6 deps, got %d: %+v", len(deps), depNames(deps))
	}
	for _, d := range deps {
		if d.Properties["package_manager"] != "nimble" {
			t.Errorf("%s: package_manager=%q want nimble", d.Name, d.Properties["package_manager"])
		}
	}

	// The nim compiler floor is a real runtime edge, with its constraint.
	if d := depByName(deps, "nim"); d == nil {
		t.Error("expected runtime dep 'nim' (compiler floor is a real edge)")
	} else if d.Properties["version"] != ">= 1.6.0" {
		t.Errorf("nim version=%q want '>= 1.6.0'", d.Properties["version"])
	} else if d.Properties["is_dev"] != "false" {
		t.Errorf("nim should be is_dev=false")
	}

	if d := depByName(deps, "jester"); d == nil || d.Properties["version"] != ">= 0.5.0" {
		t.Errorf("jester version=%v want '>= 0.5.0'", d)
	}
	// debby from a multi-dep `requires "norm", "debby >= 0.1.0"` directive.
	if d := depByName(deps, "debby"); d == nil || d.Properties["version"] != ">= 0.1.0" {
		t.Errorf("debby version=%v want '>= 0.1.0'", d)
	}
	if d := depByName(deps, "norm"); d == nil {
		t.Error("expected dep 'norm' from multi-dep requires directive")
	} else if d.Properties["version"] != "" {
		t.Errorf("norm version=%q want empty (bare)", d.Properties["version"])
	}
	// Call-syntax requires("waterpark"), bare → empty version.
	if d := depByName(deps, "waterpark"); d == nil {
		t.Error("expected dep 'waterpark' from call-syntax requires(...)")
	} else if d.Properties["version"] != "" {
		t.Errorf("waterpark version=%q want empty", d.Properties["version"])
	}
}

func TestNimble_TaskRequiresIsDev(t *testing.T) {
	deps := depEntities(runExtract(t, "webservice.nimble", realNimble))
	d := depByName(deps, "balls")
	if d == nil {
		t.Fatal("expected taskRequires dep 'balls'")
	}
	if d.Properties["is_dev"] != "true" {
		t.Errorf("balls should be is_dev=true (taskRequires dev dep)")
	}
	if d.Properties["dependency_kind"] != "dev" {
		t.Errorf("balls dependency_kind=%q want dev", d.Properties["dependency_kind"])
	}
	if d.Properties["version"] != ">= 3.0" {
		t.Errorf("balls version=%q want '>= 3.0'", d.Properties["version"])
	}
}

func TestNimble_DependsOnEdges(t *testing.T) {
	rels := dependsOnRels(runExtract(t, "lib.nimble",
		`version = "1.0.0"
requires "nim >= 2.0.0"
`))
	if len(rels) != 1 {
		t.Fatalf("expected 1 DEPENDS_ON edge, got %d", len(rels))
	}
	if rels[0].Properties["package_manager"] != "nimble" {
		t.Errorf("edge package_manager=%q want nimble", rels[0].Properties["package_manager"])
	}
}

func TestNimble_NoDependencies(t *testing.T) {
	deps := depEntities(runExtract(t, "empty.nimble",
		`version = "0.1.0"
author = "Nobody"
description = "Nothing here"
license = "MIT"
`))
	if len(deps) != 0 {
		t.Errorf("expected 0 deps, got %d: %+v", len(deps), depNames(deps))
	}
}
