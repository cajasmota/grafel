package manifest

import "testing"

// realElmApplication is a representative elm.json application manifest (shape
// taken from `elm init` + `elm-test init`). It exercises the direct/indirect
// split, the test-dependencies block, and the exact-pinned versions that make
// elm.json its own lockfile.
const realElmApplication = `{
    "type": "application",
    "source-directories": [ "src" ],
    "elm-version": "0.19.1",
    "dependencies": {
        "direct": {
            "elm/browser": "1.0.2",
            "elm/core": "1.0.5",
            "elm/html": "1.0.0"
        },
        "indirect": {
            "elm/json": "1.1.3",
            "elm/virtual-dom": "1.0.3"
        }
    },
    "test-dependencies": {
        "direct": {
            "elm-explorations/test": "2.1.0"
        },
        "indirect": {}
    }
}`

// realElmPackage is a representative elm.json package manifest (a published
// library). Its dependencies are a FLAT map of version-RANGE constraints (no
// direct/indirect split).
const realElmPackage = `{
    "type": "package",
    "name": "elm/html",
    "summary": "Fast HTML, rendered with virtual DOM diffing",
    "license": "BSD-3-Clause",
    "version": "1.0.0",
    "exposed-modules": [ "Html", "Html.Attributes" ],
    "elm-version": "0.19.0 <= v < 0.20.0",
    "dependencies": {
        "elm/core": "1.0.0 <= v < 2.0.0",
        "elm/json": "1.0.0 <= v < 2.0.0",
        "elm/virtual-dom": "1.0.0 <= v < 2.0.0"
    },
    "test-dependencies": {
        "elm-explorations/test": "2.0.0 <= v < 3.0.0"
    }
}`

func TestElmJSON_ApplicationDeps(t *testing.T) {
	deps := depEntities(runExtract(t, "elm.json", realElmApplication))
	// 3 direct + 2 indirect runtime + 1 test = 6.
	if len(deps) != 6 {
		t.Fatalf("expected 6 deps, got %d: %+v", len(deps), depNames(deps))
	}
	for _, d := range deps {
		if d.Properties["package_manager"] != "elm" {
			t.Errorf("%s: package_manager=%q want elm", d.Name, d.Properties["package_manager"])
		}
	}

	// A direct runtime dep carries its exact version, is_dev=false, not indirect.
	if d := depByName(deps, "elm/browser"); d == nil {
		t.Error("elm/browser direct dep missing")
	} else {
		if d.Properties["version"] != "1.0.2" {
			t.Errorf("elm/browser version=%q want 1.0.2", d.Properties["version"])
		}
		if d.Properties["is_dev"] != "false" {
			t.Errorf("elm/browser is_dev=%q want false", d.Properties["is_dev"])
		}
		if d.Properties["indirect"] != "false" {
			t.Errorf("elm/browser indirect=%q want false", d.Properties["indirect"])
		}
	}

	// An indirect runtime dep is flagged indirect=true.
	if d := depByName(deps, "elm/virtual-dom"); d == nil {
		t.Error("elm/virtual-dom indirect dep missing")
	} else if d.Properties["indirect"] != "true" {
		t.Errorf("elm/virtual-dom indirect=%q want true", d.Properties["indirect"])
	}

	// The test dep is flagged is_dev=true / dependency_kind=dev.
	if d := depByName(deps, "elm-explorations/test"); d == nil {
		t.Error("elm-explorations/test dep missing")
	} else {
		if d.Properties["is_dev"] != "true" {
			t.Errorf("elm-explorations/test is_dev=%q want true", d.Properties["is_dev"])
		}
		if d.Properties["dependency_kind"] != "dev" {
			t.Errorf("elm-explorations/test dependency_kind=%q want dev", d.Properties["dependency_kind"])
		}
	}
}

func TestElmJSON_PackageDeps(t *testing.T) {
	deps := depEntities(runExtract(t, "elm.json", realElmPackage))
	// 3 runtime (flat map) + 1 test = 4.
	if len(deps) != 4 {
		t.Fatalf("expected 4 deps, got %d: %+v", len(deps), depNames(deps))
	}

	// Flat-map (package) deps carry the version-range constraint as version.
	if d := depByName(deps, "elm/core"); d == nil {
		t.Error("elm/core dep missing")
	} else if d.Properties["version"] != "1.0.0 <= v < 2.0.0" {
		t.Errorf("elm/core version=%q want range constraint", d.Properties["version"])
	}

	if d := depByName(deps, "elm-explorations/test"); d == nil {
		t.Error("elm-explorations/test dep missing")
	} else if d.Properties["is_dev"] != "true" {
		t.Errorf("elm-explorations/test should be is_dev=true")
	}
}

// TestElmJSON_DependsOnEdges confirms the manifest emits DEPENDS_ON edges + SBOM
// package nodes like every other ecosystem (the cross-manifest contract).
func TestElmJSON_DependsOnEdges(t *testing.T) {
	records := runExtract(t, "elm.json", realElmApplication)
	var dependsOn int
	for _, r := range records {
		for _, rel := range r.Relationships {
			if rel.Kind == "DEPENDS_ON" {
				dependsOn++
				if rel.Properties["package_manager"] != "elm" {
					t.Errorf("DEPENDS_ON package_manager=%q want elm", rel.Properties["package_manager"])
				}
			}
		}
	}
	if dependsOn != 6 {
		t.Errorf("expected 6 DEPENDS_ON edges, got %d", dependsOn)
	}
}

// TestElmJSON_IsManifest pins the dispatch wiring: elm.json is recognised and
// routed to the elm package manager.
func TestElmJSON_IsManifest(t *testing.T) {
	if !IsManifest("frontend/elm.json") {
		t.Error("elm.json should be recognised as a manifest")
	}
	if pm := detectPackageManager("frontend/elm.json"); pm != "elm" {
		t.Errorf("detectPackageManager(elm.json)=%q want elm", pm)
	}
}
