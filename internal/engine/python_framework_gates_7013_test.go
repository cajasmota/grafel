// Package engine — #7013 variant D.
//
// Six one-line rules changes, measured on the 60-repo corpus at 762 over-fired
// entities removed against 9 true positives lost:
//
//	pytest.yaml     #1  TestClass   requires_framework   322 bad : 3 real
//	sqlalchemy.yaml #5  Constraint  requires_framework   246 bad : 4 real
//	flask.yaml      #6  Model       requires_framework    76 bad : 1 real
//	flask.yaml      #1  Route       requires_framework  \  75 bad : 0 real
//	quart.yaml      #1  Route       requires_framework  /  (one coupled change)
//	quart.yaml      #5  Middleware  requires_framework    43 bad : 0 here
//	flask.yaml          import_markers widened with `from flask import`,
//	                    `import flask` — rescues 5 of the losses, costs 0 bad
//
// This file grades BOTH directions for every one of them, because each
// direction is blind to the other's failure:
//
//   - FORBIDDEN: the gated pattern no longer mints in a file that does not
//     itself carry a marker. A recall-only test cannot see over-firing.
//   - RECALL: the gated pattern still mints in a file that does carry one. A
//     forbidden-only test is passed perfectly by a gate that kills everything.
//
// ...plus two properties that no count of either direction can observe:
//
//   - THE TWIN COUPLING (#7028). flask.yaml #1 and quart.yaml #1 are a
//     byte-identical pattern in two rule files. seenEntities (detector.go:367)
//     is per file and shared ACROSS rule sets, and rule sets load in fs.WalkDir
//     lexical order (loader.go:103), so `flask` wins every mint and `quart`'s
//     twin has never produced anything. Gating flask's copy alone therefore
//     removes ZERO entities: the mints TRANSFER to quart.yaml's twin rather
//     than disappearing (before {django: 75, flask: 5}; after flask {flask: 1}
//
//   - quart {django: 75, flask: 4} — the same 80 mints from a different rule
//     file, no over-firing removed). Totals, per-kind counts and the over-fire
//     rate all stay flat while that happens.
//
//     Nor does anything on the entity show the transfer: detector.go:451 sets
//     the `framework` property from `sp.framework`, which compile() fills at
//     :171 with the rule BUCKET key (`python`) and not the rule file, and
//     frameworks.name is parsed by schema.go and read by nothing in the mint
//     path. Both twins emit framework=python; nothing is relabelled, and there
//     is no per-framework field for a test to assert on. (The one difference
//     the rows could carry is the `file_convention` annotation — flask.yaml
//     declares 6 file_conventions, quart.yaml none — which is unmeasured and
//     deliberately not tested here.)
//
//     So the coupling is pinned twice, by the only two means available:
//     behaviourally, because a marker-less file mints a Route if EITHER twin
//     is ungated — the mint SURVIVES, which is exactly the "zero removed"
//     failure — and structurally, because the two files' flags must agree.
//
//   - THE WIDENING, in both of its halves: that it rescues the blueprint-module
//     case, and that it does not re-open the over-fire it was added alongside.
//
// Fixture discipline: every fixture below carries the SAME construct in the
// forbidden and the recall arm, so the only thing varied is marker presence.
// The varied and held-constant axes are listed on each test.
package engine

import (
	"context"
	"os"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/cajasmota/grafel/internal/extractor"
)

// ---------------------------------------------------------------------------
// Premise helpers — grounded in the rule FILES on disk, never in the compiled
// Go structs under test, so a loader bug cannot make a premise vacuously true.
// ---------------------------------------------------------------------------

// pgRuleFile is a minimal on-disk view of a python framework rule file.
type pgRuleFile struct {
	Frameworks struct {
		Detection struct {
			ImportMarkers []string `yaml:"import_markers"`
		} `yaml:"detection"`
	} `yaml:"frameworks"`
	SourcePatterns []struct {
		Pattern           string `yaml:"pattern"`
		EntityType        string `yaml:"entity_type"`
		NameGroup         int    `yaml:"name_group"`
		RequiresFramework bool   `yaml:"requires_framework"`
	} `yaml:"source_patterns"`
}

// pgFrameworks is every rule file this change touches. Both flask AND quart are
// named everywhere they can both matter — enumerating one thoroughly is what
// makes its twin look covered.
var pgFrameworks = []string{"pytest", "sqlalchemy", "flask", "quart"}

// pgLoadRuleFile parses one python framework rule file straight off disk.
func pgLoadRuleFile(t *testing.T, framework string) pgRuleFile {
	t.Helper()
	path := "rules/python/frameworks/" + framework + ".yaml"
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var doc pgRuleFile
	if err := yaml.Unmarshal(data, &doc); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	return doc
}

// pgMarkers returns a framework's on-disk import_markers, failing if the list
// is empty — an empty list makes every "no marker present" premise trivial and
// every "marker present" premise unsatisfiable.
func pgMarkers(t *testing.T, framework string) []string {
	t.Helper()
	m := pgLoadRuleFile(t, framework).Frameworks.Detection.ImportMarkers
	if len(m) == 0 {
		t.Fatalf("%s declares no frameworks.detection.import_markers — the gate under test has "+
			"no signal, and every premise here would be vacuous", framework)
	}
	return m
}

// pgAssertNoMarkers asserts src carries no import marker for ANY framework
// whose gated pattern could mint from it. Without this the forbidden direction
// could pass for the wrong reason. `falcon` and `cherrypy` are included because
// their own gated bare-class patterns (#6152) reach the class fixtures too.
func pgAssertNoMarkers(t *testing.T, src string) {
	t.Helper()
	for _, fw := range append(append([]string{}, pgFrameworks...), "falcon", "cherrypy") {
		for _, m := range pgMarkers(t, fw) {
			if strings.Contains(src, m) {
				t.Fatalf("fixture premise broken: source contains %s marker %q, so it is not a "+
					"marker-free file and cannot grade the forbidden direction", fw, m)
			}
		}
	}
}

// pgAssertHasMarker asserts src carries at least one marker for framework, the
// recall direction's premise.
func pgAssertHasMarker(t *testing.T, framework, src string) {
	t.Helper()
	for _, m := range pgMarkers(t, framework) {
		if strings.Contains(src, m) {
			return
		}
	}
	t.Fatalf("fixture premise broken: source contains none of %s's import_markers %v, so a "+
		"mint here would not prove the gate lets real %s code through",
		framework, pgMarkers(t, framework), framework)
}

// pgAssertGated asserts on disk that the named framework file carries exactly
// one source_pattern producing entityType whose regex contains patternSubstr,
// and that it carries requires_framework.
//
// It identifies the pattern by a distinguishing fragment of its regex rather
// than by entity_type alone, because these files carry several patterns per
// kind (flask.yaml alone mints Route twice) and by ordinal, because an ordinal
// silently slides when a pattern is inserted above it.
//
// This pins the WIRING: without it, a forbidden-direction pass is equally
// consistent with "the gate holds" and "someone deleted the pattern".
func pgAssertGated(t *testing.T, framework, entityType, patternSubstr string) {
	t.Helper()
	doc := pgLoadRuleFile(t, framework)
	found := 0
	for _, sp := range doc.SourcePatterns {
		if sp.EntityType != entityType || !strings.Contains(sp.Pattern, patternSubstr) {
			continue
		}
		found++
		if !sp.RequiresFramework {
			t.Errorf("%s.yaml pattern %q produces %s but does NOT carry requires_framework — "+
				"#7013's gate is not wired for it", framework, sp.Pattern, entityType)
		}
	}
	switch {
	case found == 0:
		t.Fatalf("%s.yaml has no source_pattern producing %s whose regex contains %q: the "+
			"pattern this test grades was rewritten or deleted, and the forbidden direction "+
			"below would pass vacuously", framework, entityType, patternSubstr)
	case found > 1:
		t.Fatalf("%s.yaml has %d source_patterns producing %s and matching %q — the fragment no "+
			"longer identifies one pattern, so this assertion is ambiguous",
			framework, found, entityType, patternSubstr)
	}
}

// pgDetect runs the REAL embedded rule set, the same one Pass 2.5 runs.
func pgDetect(t *testing.T, path, src string) *DetectResult {
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

// pgHas reports whether the detector emitted an entity with this kind and name.
func pgHas(res *DetectResult, kind, name string) bool {
	for i := range res.Entities {
		if res.Entities[i].Kind == kind && res.Entities[i].Name == name {
			return true
		}
	}
	return false
}

// pgDump renders every emitted entity, for failure messages.
func pgDump(res *DetectResult) string {
	var b []string
	for i := range res.Entities {
		b = append(b, res.Entities[i].Kind+":"+res.Entities[i].Name)
	}
	if len(b) == 0 {
		return "(no entities)"
	}
	return strings.Join(b, ", ")
}

// ---------------------------------------------------------------------------
// The constructs. Each appears verbatim in both a marker-free and a marked
// fixture, so marker presence is the ONLY axis varied between the two arms.
// ---------------------------------------------------------------------------

const (
	// pytest.yaml #1 — `(?m)^class\s+(Test\w+)\s*`.
	pgTestClassConstruct = "class TestPgProbe:\n    def test_pg_probe(self):\n        assert True\n"
	// sqlalchemy.yaml #5 — `ForeignKey\s*\(\s*["']([^"']+)["']`.
	pgConstraintConstruct = "pg_probe_col = ForeignKey(\"pg_probe_table.id\")\n"
	// flask.yaml #6 — `class\s+(\w+)\s*\(\s*(?:db\.Model|Model)\s*\)`.
	pgModelConstruct = "class PgProbeModel(db.Model):\n    pass\n"
	// flask.yaml #1 / quart.yaml #1 — the byte-identical twin.
	pgRouteConstruct = "@pg_probe_bp.get(\"/pg-probe-route\")\ndef pg_probe_view():\n    return \"ok\"\n"
	// quart.yaml #5 — `async def` deliberately: flask.yaml #5's near-twin
	// requires `\ndef` and cannot reach an async hook, so this construct
	// isolates quart #5 instead of being masked by flask's ungated pattern.
	pgMiddlewareConstruct = "@pg_probe_app.before_request\nasync def pg_probe_hook():\n    return None\n"
)

// pgGateCase is one gated pattern, graded in both directions with the same
// construct on both sides.
type pgGateCase struct {
	name string
	// frameworks whose rule file must carry the gate for this kind. Two for
	// the Route twin — flask AND quart, named together everywhere.
	frameworks []string
	kind       string
	entity     string
	construct  string
	// patternSubstr identifies WHICH pattern in those files is under test, by
	// a fragment of its regex — see pgAssertGated.
	patternSubstr string
	// recallMarker is prepended to construct for the recall arm. It is a
	// marker of recallFramework and of nothing else.
	recallFramework string
	recallMarker    string
}

var pgGateCases = []pgGateCase{
	{
		name:            "pytest#1_TestClass",
		patternSubstr:   "^class\\s+(Test\\w+)",
		frameworks:      []string{"pytest"},
		kind:            "TestClass",
		entity:          "TestPgProbe",
		construct:       pgTestClassConstruct,
		recallFramework: "pytest",
		recallMarker:    "import pytest\n\n",
	},
	{
		name:            "sqlalchemy#5_Constraint",
		patternSubstr:   "ForeignKey",
		frameworks:      []string{"sqlalchemy"},
		kind:            "Constraint",
		entity:          "pg_probe_table.id",
		construct:       pgConstraintConstruct,
		recallFramework: "sqlalchemy",
		recallMarker:    "from sqlalchemy import ForeignKey\n\n",
	},
	{
		name:            "flask#6_Model",
		patternSubstr:   "db\\.Model",
		frameworks:      []string{"flask"},
		kind:            "Model",
		entity:          "PgProbeModel",
		construct:       pgModelConstruct,
		recallFramework: "flask",
		recallMarker:    "app = Flask(__name__)\n\n",
	},
	{
		// The twin, graded with a FLASK marker: flask sorts first and mints.
		name:            "flask#1_Route_flask_marked",
		patternSubstr:   "get|post|put|patch|delete",
		frameworks:      []string{"flask", "quart"},
		kind:            "Route",
		entity:          "/pg-probe-route",
		construct:       pgRouteConstruct,
		recallFramework: "flask",
		recallMarker:    "app = Flask(__name__)\n\n",
	},
	{
		// The SAME twin and the SAME construct, graded with a QUART marker.
		// Held constant: pattern, kind, entity name, construct. Varied: which
		// of the two identical rule files can see its framework. flask #1 is
		// gated off here, so this arm is quart #1's recall and nothing else.
		name:            "quart#1_Route_quart_marked",
		patternSubstr:   "get|post|put|patch|delete",
		frameworks:      []string{"flask", "quart"},
		kind:            "Route",
		entity:          "/pg-probe-route",
		construct:       pgRouteConstruct,
		recallFramework: "quart",
		recallMarker:    "from quart import Quart\n\n",
	},
	{
		name:            "quart#5_Middleware",
		patternSubstr:   "before_request",
		frameworks:      []string{"quart"},
		kind:            "Middleware",
		entity:          "pg_probe_hook",
		construct:       pgMiddlewareConstruct,
		recallFramework: "quart",
		recallMarker:    "from quart import Quart\n\n",
	},
}

// TestGatedPythonPatterns_DoNotMintWithoutMarkers_7013 is the FORBIDDEN
// direction — the 762 entities this change exists to remove.
//
// Varied: nothing but marker presence, against the recall test below. Held
// constant: the construct, the entity name, the file path, the language.
//
// This test is also the behavioural half of the flask/quart twin coupling: the
// Route row mints if EITHER twin is left ungated, because the ungated one still
// fires on this marker-free file.
func TestGatedPythonPatterns_DoNotMintWithoutMarkers_7013(t *testing.T) {
	for _, tc := range pgGateCases {
		t.Run(tc.name, func(t *testing.T) {
			for _, fw := range tc.frameworks {
				pgAssertGated(t, fw, tc.kind, tc.patternSubstr)
			}
			pgAssertNoMarkers(t, tc.construct)

			res := pgDetect(t, "pg_probe.py", tc.construct)

			if pgHas(res, tc.kind, tc.entity) {
				t.Errorf("%s: %s %q was minted from a file carrying no import marker for %v — "+
					"the requires_framework gate is not holding (#7013). Emitted: %s",
					tc.name, tc.kind, tc.entity, tc.frameworks, pgDump(res))
			}
		})
	}
}

// TestGatedPythonPatterns_StillMintWithMarkers_7013 is the RECALL direction. A
// gate that suppresses everything passes the forbidden test above perfectly.
//
// Varied: marker presence (and, for the two Route rows, WHICH of the twin's two
// frameworks is marked). Held constant: the construct, the entity name, the
// file path, the language.
func TestGatedPythonPatterns_StillMintWithMarkers_7013(t *testing.T) {
	for _, tc := range pgGateCases {
		t.Run(tc.name, func(t *testing.T) {
			src := tc.recallMarker + tc.construct
			pgAssertHasMarker(t, tc.recallFramework, src)

			res := pgDetect(t, "pg_probe.py", src)

			if !pgHas(res, tc.kind, tc.entity) {
				t.Errorf("%s: %s %q is GONE from a file that plainly uses %s — the #7013 gate "+
					"is too strict and has cost real recall. Emitted: %s",
					tc.name, tc.kind, tc.entity, tc.recallFramework, pgDump(res))
			}
		})
	}
}

// pgRouteDecoratorConstruct exercises the OTHER flask/quart twin — the `.route`
// pair (flask.yaml #0 / quart.yaml #0), #7028's group #3 at 178 mints. It is
// deliberately a different path string from pgRouteConstruct so the two twins
// cannot be confused in a failure message.
const pgRouteDecoratorConstruct = "@pg_probe_bp.route(\"/pg-probe-decorated\")\n" +
	"def pg_probe_decorated_view():\n    return \"ok\"\n"

// TestRouteDecoratorTwinIsUngated_7013 records the CURRENT state of the second
// twin rather than changing it.
//
// #7013 gates the flask#1/quart#1 HTTP-method pair. The flask#0/quart#0
// `.route` pair is the identical twin shape one entry over, is still ungated,
// and still over-fires — 178 mints, unmeasured for recall. Gating it is #7028's
// call and needs its own recall pass, so this change does NOT gate it.
//
// Without this test that state is unobservable: gating BOTH halves of the #0
// pair passes every other test in this file, so someone could gate it — or
// un-gate it later — with no test noticing and no recall measurement. Scored as
// a mutant and it was ALIVE; this is the kill.
//
// If you are here because this test went red: you changed the #0 pair. That is
// allowed, but (1) measure its recall cost first, the way #7013 did for #1,
// (2) gate BOTH files or neither — see TestFlaskAndQuartRouteTwinsGatedTogether_7013
// for why one alone removes nothing, and (3) replace this test with a
// forbidden+recall pair like the #1 rows above, rather than deleting it.
func TestRouteDecoratorTwinIsUngated_7013(t *testing.T) {
	// Structural premise: both halves exist and both are ungated on disk.
	seen := 0
	for _, fw := range []string{"flask", "quart"} {
		for _, sp := range pgLoadRuleFile(t, fw).SourcePatterns {
			if sp.EntityType != "Route" || !strings.Contains(sp.Pattern, "\\.route") {
				continue
			}
			seen++
			if sp.RequiresFramework {
				t.Errorf("%s.yaml's `.route` pattern %q now carries requires_framework. The "+
					"flask#0/quart#0 twin was left ungated by #7013 on purpose: 178 mints, "+
					"recall cost UNMEASURED. Measure it, gate both halves, and turn this test "+
					"into a forbidden+recall pair (#7028).", fw, sp.Pattern)
			}
		}
	}
	if seen != 2 {
		t.Fatalf("found %d `.route` Route patterns across flask.yaml and quart.yaml, want 2: the "+
			"twin this test tracks was edited away and the test is no longer observing it", seen)
	}

	// Behavioural half: ungated means it still mints with no marker present.
	// This is the same marker-free premise the forbidden table uses, so the
	// only thing separating this row from those is which pattern is gated.
	pgAssertNoMarkers(t, pgRouteDecoratorConstruct)

	res := pgDetect(t, "pg_probe.py", pgRouteDecoratorConstruct)

	if !pgHas(res, "Route", "/pg-probe-decorated") {
		t.Errorf("the ungated `.route` twin no longer mints on a marker-free file. Something "+
			"gated or narrowed flask#0/quart#0 without updating this test — see the doc "+
			"comment. Emitted: %s", pgDump(res))
	}
}

// TestFlaskAndQuartRouteTwinsGatedTogether_7013 is the structural half of the
// twin coupling (#7028), and the one that speaks to the next person to touch
// either file.
//
// It asserts the general property rather than the instance: any source_pattern
// that appears in two python rule files with the same regex, entity_type and
// name_group must agree on requires_framework. There are four such pairs today
// — all flask/quart: #0 Route (`.route`), #1 Route (the one this change gates),
// #2 Controller (Blueprint), #4 Middleware (errorhandler). Gating one member of
// any of them alone removes nothing and silently transfers its mints, including
// its TRUE positives, to the other framework's label.
//
// It must also see the twin at all, so it fails if no pair is found.
func TestFlaskAndQuartRouteTwinsGatedTogether_7013(t *testing.T) {
	type twinKey struct {
		pattern    string
		entityType string
		nameGroup  int
	}
	type site struct {
		framework string
		gated     bool
	}

	// Every python framework rule file, not just the four this change edits:
	// a twin introduced elsewhere in the tree is the same trap.
	dir := "rules/python/frameworks"
	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}
	twins := map[twinKey][]site{}
	for _, e := range ents {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		fw := strings.TrimSuffix(e.Name(), ".yaml")
		for _, sp := range pgLoadRuleFile(t, fw).SourcePatterns {
			k := twinKey{sp.Pattern, sp.EntityType, sp.NameGroup}
			twins[k] = append(twins[k], site{fw, sp.RequiresFramework})
		}
	}

	pairs := 0
	sawRouteTwin := false
	for k, sites := range twins {
		if len(sites) < 2 {
			continue
		}
		pairs++
		if k.entityType == "Route" && strings.Contains(k.pattern, "get|post|put|patch|delete") {
			sawRouteTwin = true
		}
		first := sites[0].gated
		for _, s := range sites[1:] {
			if s.gated == first {
				continue
			}
			t.Errorf("identical pattern %q (%s, name_group %d) disagrees on requires_framework "+
				"across rule files %v. Dedup (seenEntities, detector.go:367) is per file and "+
				"SHARED across rule sets, and rule sets load in lexical order (loader.go:103), "+
				"so the first file wins every mint: gating only one of these removes ZERO "+
				"entities — the mints TRANSFER to the other file's identical pattern instead "+
				"of disappearing, and no count moves. Measured on flask#1/quart#1 as 80 mints "+
				"before, 80 after, 0 over-firing removed. Gate both or neither (#7028).", k.pattern, k.entityType, k.nameGroup, sites)
			break
		}
	}
	if pairs == 0 {
		t.Error("no identical-twin source_pattern pair found in rules/python/frameworks: this " +
			"test's premise is gone and it is asserting nothing (#7028)")
	}
	if !sawRouteTwin {
		t.Error("the flask#1/quart#1 HTTP-method Route twin — the pair #7013 gates and the " +
			"reason this test exists — was not found; it was edited out of one of the two " +
			"files and the coupling is no longer being graded (#7028)")
	}
}

// ---------------------------------------------------------------------------
// The marker widening, graded in both halves.
// ---------------------------------------------------------------------------

// pgFlaskMarkersBeforeWidening is flask.yaml's marker list as it stood before
// #7013. Hard-coded on purpose: the widening test must compare against the OLD
// list, and reading it from the file would compare it against itself.
var pgFlaskMarkersBeforeWidening = []string{"from flask import Flask", "Flask(__name__)"}

// pgBlueprintModule is the shape the widening exists for: a real Flask
// blueprint module (flask/examples/celery/src/task_app/views.py) that imports
// Blueprint and never constructs the app. Under the old marker list this file
// carried no Flask evidence at all, and the gate above cost it 4 real routes.
const pgBlueprintModule = "from flask import Blueprint\n\n" +
	"pg_probe_bp = Blueprint(\"pg_probe\", __name__)\n\n" + pgRouteConstruct

// TestWidenedFlaskMarkers_RescueBlueprintModule_7013 is the widening's first
// half: it must actually rescue.
//
// Varied: the marker LINE only (`from flask import Blueprint` here versus
// `Flask(__name__)` in the recall table above). Held constant: the construct,
// the entity name, the gate.
func TestWidenedFlaskMarkers_RescueBlueprintModule_7013(t *testing.T) {
	// Premise: this fixture is invisible to the PRE-widening marker list, so a
	// Route here is evidence about the widening and not about the old markers.
	for _, m := range pgFlaskMarkersBeforeWidening {
		if strings.Contains(pgBlueprintModule, m) {
			t.Fatalf("fixture premise broken: blueprint module contains pre-widening marker %q, "+
				"so it would have been rescued without the widening and grades nothing", m)
		}
	}
	pgAssertHasMarker(t, "flask", pgBlueprintModule)

	res := pgDetect(t, "task_app/views.py", pgBlueprintModule)

	if !pgHas(res, "Route", "/pg-probe-route") {
		t.Errorf("blueprint module lost its Route: flask.yaml's import_markers no longer reach "+
			"`from flask import` / `import flask`, and the #7013 gate is costing the 4 real "+
			"routes the widening was measured to rescue. Emitted: %s", pgDump(res))
	}
	// The Blueprint pattern is ungated and must be unaffected either way — if
	// this stops holding, the fixture has stopped being a blueprint module.
	if !pgHas(res, "Controller", "pg_probe") {
		t.Errorf("fixture no longer registers as a blueprint module (no Controller): "+
			"emitted %s", pgDump(res))
	}
}

// pgPlainImportModule is the OTHER half of the widening: a module that reaches
// Flask through `import flask` and the qualified `flask.Blueprint` spelling,
// never through a `from … import` line. `from flask import` cannot see it.
//
// Added after mutant scoring: dropping `import flask` and keeping only
// `from flask import` left every other test in this file green, so the second
// marker was carried by nothing.
const pgPlainImportModule = "import flask\n\n" +
	"pg_probe_bp = flask.Blueprint(\"pg_probe\", __name__)\n\n" + pgRouteConstruct

// TestWidenedFlaskMarkers_RescuePlainImportModule_7013 grades `import flask`
// specifically.
//
// Varied: the marker SPELLING only (`import flask` here, `from flask import
// Blueprint` in the sibling test above, `Flask(__name__)` in the recall table).
// Held constant: the construct, the entity name, the gate.
func TestWidenedFlaskMarkers_RescuePlainImportModule_7013(t *testing.T) {
	// Premise: neither the pre-widening markers NOR the other widened marker
	// can see this file, so a Route here is evidence about `import flask`
	// alone.
	for _, m := range append(append([]string{}, pgFlaskMarkersBeforeWidening...), "from flask import") {
		if strings.Contains(pgPlainImportModule, m) {
			t.Fatalf("fixture premise broken: module contains marker %q, so it does not "+
				"isolate `import flask` and grades nothing about it", m)
		}
	}
	pgAssertHasMarker(t, "flask", pgPlainImportModule)

	res := pgDetect(t, "pg_app/views.py", pgPlainImportModule)

	if !pgHas(res, "Route", "/pg-probe-route") {
		t.Errorf("module using `import flask` lost its Route: flask.yaml's import_markers no "+
			"longer carry the bare `import flask` spelling, so the #7013 gate suppresses "+
			"qualified-import Flask code. Emitted: %s", pgDump(res))
	}
}

// pgDjangoRouteModule is the over-firing shape the gate removes: a Django
// module using a decorator with an HTTP-verb method name. 75 of flask #1's 80
// corpus mints were this. It names flask nowhere — including under the WIDENED
// markers, which is the property this test grades.
const pgDjangoRouteModule = "from django.db import models\n" +
	"from django.urls import path\n\n" +
	"pg_probe_bp = models.Manager()\n\n" + pgRouteConstruct

// TestWidenedFlaskMarkers_DoNotReopenOverfire_7013 is the widening's second
// half. A widening that rescues the blueprint module by also matching every
// non-Flask file would pass the rescue test above and quietly undo the 762.
//
// Varied: the surrounding imports (Django rather than none). Held constant: the
// construct and the entity name, both identical to the forbidden table's Route
// row — so this is the same assertion against a realistic over-firer rather
// than a bare fixture.
func TestWidenedFlaskMarkers_DoNotReopenOverfire_7013(t *testing.T) {
	pgAssertNoMarkers(t, pgDjangoRouteModule)

	res := pgDetect(t, "pg_app/views.py", pgDjangoRouteModule)

	if pgHas(res, "Route", "/pg-probe-route") {
		t.Errorf("a Django module with no flask or quart token minted Route \"/pg-probe-route\": "+
			"flask.yaml's widened import_markers now reach ordinary non-Flask files and have "+
			"re-opened the over-firing #7013 closed. Emitted: %s", pgDump(res))
	}
}
