// Package engine — #6152.
//
// Two Python framework rule sets carry a source_pattern that matches a BARE
// class declaration and types it `Controller`:
//
//	falcon.yaml    class\s+(\w+)\s*(?:\([^)]*\))?\s*:
//	cherrypy.yaml  class\s+(\w+)\s*(?:\(object\))?\s*:
//
// Detect resolves compiled rule sets by file.Language alone, so both fired on
// EVERY Python file in every repo, framework present or not — every bare class
// in the graph came out `Controller`. (internal/extractors/migration_prune.go
// exists to delete a slice of that output for migration files specifically;
// see #3173.)
//
// The fix gates those two patterns on the framework's own detection metadata —
// the `frameworks.detection.import_markers` list already sitting in each YAML
// file — via an explicit `requires_framework: true` opt-in on the pattern.
//
// This file asserts BOTH directions, because a gate that kills recall is worse
// than the bug:
//
//   - a bare class in a file with no falcon/cherrypy marker is NOT a Controller
//   - a real falcon resource / cherrypy app class still IS a Controller
//
// Every fixture's PREMISE is asserted against the YAML rule files themselves —
// not against the Go structs under test — so a fixture that silently stops
// exercising the gate fails loudly instead of passing through another path.
package engine

import (
	"context"
	"os"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/cajasmota/grafel/internal/extractor"
)

// cpMarkerYAML is a minimal view of a framework rule file, parsed straight off
// disk. It deliberately does NOT reuse the engine's own schema types: the
// premise assertions below must be grounded in the rule FILES, so that a bug in
// the loader/schema (the layer under test) cannot make them vacuously true.
type cpMarkerYAML struct {
	Frameworks struct {
		Detection struct {
			ImportMarkers []string `yaml:"import_markers"`
		} `yaml:"detection"`
	} `yaml:"frameworks"`
}

// cpImportMarkers reads the on-disk import_markers for one python framework
// rule file and fails if the list is empty — an empty list would make every
// "no marker present" premise below trivially true.
func cpImportMarkers(t *testing.T, framework string) []string {
	t.Helper()
	path := "rules/python/frameworks/" + framework + ".yaml"
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var doc cpMarkerYAML
	if err := yaml.Unmarshal(data, &doc); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	markers := doc.Frameworks.Detection.ImportMarkers
	if len(markers) == 0 {
		t.Fatalf("%s declares no frameworks.detection.import_markers — the gate this test "+
			"exercises has no signal to gate on, and every premise below would be vacuous", path)
	}
	return markers
}

// cpAssertNoMarker asserts the fixture genuinely contains NO detection marker
// for either gated framework. This is the negative test's premise: without it,
// "bare class is not a Controller" could pass because the fixture accidentally
// tripped some unrelated rule.
func cpAssertNoMarker(t *testing.T, src string) {
	t.Helper()
	for _, fw := range []string{"falcon", "cherrypy"} {
		for _, m := range cpImportMarkers(t, fw) {
			if strings.Contains(src, m) {
				t.Fatalf("fixture premise broken: source contains %s marker %q, so it is not a "+
					"bare no-framework file and cannot test the gate's negative direction", fw, m)
			}
		}
	}
}

// cpAssertHasMarker asserts the fixture genuinely contains at least one
// detection marker for the named framework — the positive test's premise.
func cpAssertHasMarker(t *testing.T, framework, src string) {
	t.Helper()
	for _, m := range cpImportMarkers(t, framework) {
		if strings.Contains(src, m) {
			return
		}
	}
	t.Fatalf("fixture premise broken: source contains none of %s's import_markers %q, so a "+
		"Controller verdict would not prove the gate lets a real %s class through",
		framework, cpImportMarkers(t, framework), framework)
}

// cpDetect runs the REAL embedded rule set — the same one Pass 2.5 runs.
func cpDetect(t *testing.T, path, src string) *DetectResult {
	t.Helper()
	rules, err := LoadAllRules()
	if err != nil {
		t.Fatalf("LoadAllRules: %v", err)
	}
	res, err := New(rules).Detect(context.Background(), extractor.FileInput{
		Path:     path,
		Language: "python",
		Content:  []byte(src),
	})
	if err != nil {
		t.Fatalf("detect: %v", err)
	}
	if res == nil {
		t.Fatal("detect returned nil result")
	}
	return res
}

// cpKindsFor returns every Kind the detector emitted for the named entity.
func cpKindsFor(res *DetectResult, name string) []string {
	var out []string
	for i := range res.Entities {
		if res.Entities[i].Name == name {
			out = append(out, res.Entities[i].Kind)
		}
	}
	return out
}

// cpBareClassSource is a Python file with no framework anywhere: no import, no
// decorator, no base class, no responder method name. Method and class names
// are prefixed so they cannot collide with any other rule's vocabulary.
const cpBareClassSource = `class CpPlainProbe:
    def cp_probe_noop(self):
        return 1
`

// TestBarePythonClass_IsNotController_6152 is the gate's negative direction.
//
// Before the fix this reported kind=Controller (twice — once from falcon.yaml,
// once from cherrypy.yaml, deduplicated to one row by entity key).
func TestBarePythonClass_IsNotController_6152(t *testing.T) {
	cpAssertNoMarker(t, cpBareClassSource)

	res := cpDetect(t, "cp_plain.py", cpBareClassSource)

	for _, k := range cpKindsFor(res, "CpPlainProbe") {
		if k == "Controller" {
			t.Errorf("bare Python class CpPlainProbe typed %q by the YAML engine: the "+
				"falcon/cherrypy bare-class patterns are firing on a file with no framework "+
				"marker (#6152)", k)
		}
	}
}

// cpFalconSource is a genuine Falcon resource module: it carries the `import
// falcon` marker, registers a route, and defines an on_get responder.
const cpFalconSource = `import falcon


class CpFalconProbe:
    def on_get(self, req, resp):
        resp.text = "ok"


cp_app = falcon.App()
cp_app.add_route('/cp-probe', CpFalconProbe())
`

// TestFalconResourceClass_StaysController_6152 is the gate's positive
// direction: the recall the falcon rule exists to provide must survive.
func TestFalconResourceClass_StaysController_6152(t *testing.T) {
	cpAssertHasMarker(t, "falcon", cpFalconSource)

	res := cpDetect(t, "cp_falcon.py", cpFalconSource)

	kinds := cpKindsFor(res, "CpFalconProbe")
	found := false
	for _, k := range kinds {
		if k == "Controller" {
			found = true
		}
	}
	if !found {
		t.Errorf("falcon resource class CpFalconProbe lost its Controller kind (got %q): the "+
			"#6152 gate is too strict and has cost real recall", kinds)
	}
}

// cpCherryPySource is a genuine CherryPy application module.
const cpCherryPySource = `import cherrypy


class CpCherryProbe(object):
    @cherrypy.expose
    def cp_index(self):
        return "ok"


cherrypy.quickstart(CpCherryProbe())
`

// TestCherryPyAppClass_StaysController_6152 is the second positive direction.
func TestCherryPyAppClass_StaysController_6152(t *testing.T) {
	cpAssertHasMarker(t, "cherrypy", cpCherryPySource)

	res := cpDetect(t, "cp_cherry.py", cpCherryPySource)

	kinds := cpKindsFor(res, "CpCherryProbe")
	found := false
	for _, k := range kinds {
		if k == "Controller" {
			found = true
		}
	}
	if !found {
		t.Errorf("cherrypy app class CpCherryProbe lost its Controller kind (got %q): the "+
			"#6152 gate is too strict and has cost real recall", kinds)
	}
}

// TestGatedPatternsAreLoaded_6152 pins the wiring itself: the two bare-class
// patterns must actually carry requires_framework, and their rule sets must
// actually carry import markers into the compiled form. Without this, a future
// edit that drops the YAML flag would silently reopen #6152 while the negative
// test above kept passing for the wrong reason (no marker → no match either way
// is indistinguishable from no gate → …no, it is not: it would REGRESS. This
// test makes the wiring itself observable rather than inferred).
func TestGatedPatternsAreLoaded_6152(t *testing.T) {
	rules, err := LoadAllRules()
	if err != nil {
		t.Fatalf("LoadAllRules: %v", err)
	}
	d := New(rules)
	d.once.Do(d.compile)

	gated := 0
	markerBearing := 0
	for _, cs := range d.compiled["python"] {
		if len(cs.importMarkers) > 0 {
			markerBearing++
		}
		for _, sp := range cs.sourcePatterns {
			if sp.requiresFramework {
				gated++
				if len(cs.importMarkers) == 0 {
					t.Errorf("source pattern %q is marked requires_framework but its rule set "+
						"carries no import_markers — the gate would suppress it unconditionally",
						sp.regex.String())
				}
			}
		}
	}
	if gated == 0 {
		t.Error("no python source_pattern carries requires_framework: the #6152 gate is not wired")
	}
	if markerBearing == 0 {
		t.Error("no python rule set carries import_markers: detection metadata is not being loaded")
	}
}
