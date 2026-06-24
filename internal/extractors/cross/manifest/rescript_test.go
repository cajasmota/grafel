package manifest

import "testing"

// realReScriptJSON is a representative rescript.json (ReScript v11+) manifest.
// It exercises the bs-dependencies (runtime), bs-dev-dependencies (dev),
// pinned-dependencies (runtime, flagged), and the jsx/module config block.
const realReScriptJSON = `{
  "name": "my-app",
  "version": "1.0.0",
  "sources": [{ "dir": "src", "subdirs": true }],
  "package-specs": [{ "module": "es6", "in-source": true }],
  "suffix": ".bs.js",
  "namespace": true,
  "bs-dependencies": [
    "@rescript/react",
    "rescript-webapi"
  ],
  "bs-dev-dependencies": [
    "@rescript/tools"
  ],
  "pinned-dependencies": [
    "my-local-lib"
  ],
  "jsx": { "version": 4, "mode": "classic" }
}`

func TestReScriptJSON_Deps(t *testing.T) {
	deps := depEntities(runExtract(t, "rescript.json", realReScriptJSON))
	// 2 bs-dependencies + 1 pinned (runtime) + 1 dev = 4.
	if len(deps) != 4 {
		t.Fatalf("expected 4 deps, got %d: %+v", len(deps), depNames(deps))
	}
	for _, d := range deps {
		if d.Properties["package_manager"] != "npm" {
			t.Errorf("%s: package_manager=%q want npm (ReScript deps ARE npm packages)", d.Name, d.Properties["package_manager"])
		}
	}

	// A runtime dep is is_dev=false.
	if d := depByName(deps, "@rescript/react"); d == nil {
		t.Error("@rescript/react runtime dep missing")
	} else if d.Properties["is_dev"] != "false" {
		t.Errorf("@rescript/react is_dev=%q want false", d.Properties["is_dev"])
	}

	// The bs-dev-dependency is flagged is_dev=true / dependency_kind=dev.
	if d := depByName(deps, "@rescript/tools"); d == nil {
		t.Error("@rescript/tools dev dep missing")
	} else {
		if d.Properties["is_dev"] != "true" {
			t.Errorf("@rescript/tools is_dev=%q want true", d.Properties["is_dev"])
		}
		if d.Properties["dependency_kind"] != "dev" {
			t.Errorf("@rescript/tools dependency_kind=%q want dev", d.Properties["dependency_kind"])
		}
	}

	// The pinned-dependency is a runtime dep.
	if d := depByName(deps, "my-local-lib"); d == nil {
		t.Error("my-local-lib pinned dep missing")
	} else if d.Properties["is_dev"] != "false" {
		t.Errorf("my-local-lib is_dev=%q want false (pinned = runtime)", d.Properties["is_dev"])
	}
}

// TestReScriptJSON_DependsOnEdges confirms the manifest emits DEPENDS_ON edges
// like every other ecosystem (the cross-manifest contract).
func TestReScriptJSON_DependsOnEdges(t *testing.T) {
	records := runExtract(t, "rescript.json", realReScriptJSON)
	var dependsOn int
	for _, r := range records {
		for _, rel := range r.Relationships {
			if rel.Kind == "DEPENDS_ON" {
				dependsOn++
				if rel.Properties["package_manager"] != "npm" {
					t.Errorf("DEPENDS_ON package_manager=%q want npm", rel.Properties["package_manager"])
				}
			}
		}
	}
	if dependsOn != 4 {
		t.Errorf("expected 4 DEPENDS_ON edges, got %d", dependsOn)
	}
}

// TestReScriptJSON_ConfigProperty confirms the JSX/module config is surfaced on
// the project anchor as the rescript_config property.
func TestReScriptJSON_ConfigProperty(t *testing.T) {
	records := runExtract(t, "rescript.json", realReScriptJSON)
	var cfg string
	for i := range records {
		r := records[i]
		if r.Kind == "SCOPE.Component" && r.Subtype == "project" {
			cfg = r.Properties["rescript_config"]
		}
	}
	if cfg == "" {
		t.Fatal("expected rescript_config property on the project anchor")
	}
	for _, want := range []string{"jsx_version=4", "jsx_mode=classic", "module=es6", "suffix=.bs.js"} {
		if !containsSub(cfg, want) {
			t.Errorf("rescript_config=%q missing %q", cfg, want)
		}
	}
}

// TestBsconfigJSON_LegacyName confirms the legacy bsconfig.json name parses
// identically (same schema as rescript.json).
func TestBsconfigJSON_LegacyName(t *testing.T) {
	if !IsManifest("frontend/bsconfig.json") {
		t.Error("bsconfig.json should be recognised as a manifest")
	}
	if pm := detectPackageManager("frontend/bsconfig.json"); pm != "npm" {
		t.Errorf("detectPackageManager(bsconfig.json)=%q want npm", pm)
	}
	deps := depEntities(runExtract(t, "bsconfig.json", realReScriptJSON))
	if len(deps) != 4 {
		t.Fatalf("bsconfig.json: expected 4 deps, got %d", len(deps))
	}
}

// TestReScriptJSON_IsManifest pins the dispatch wiring.
func TestReScriptJSON_IsManifest(t *testing.T) {
	if !IsManifest("frontend/rescript.json") {
		t.Error("rescript.json should be recognised as a manifest")
	}
	if pm := detectPackageManager("frontend/rescript.json"); pm != "npm" {
		t.Errorf("detectPackageManager(rescript.json)=%q want npm", pm)
	}
}

func containsSub(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
