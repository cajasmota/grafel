package engine

import (
	"fmt"
	"strings"
	"testing"
	"testing/fstest"
)

// Tests for #7014: unknown keys inside the three EXECUTABLE rule containers
// (`source_patterns[]`, `relationship_rules[]`, `file_conventions[]`) must fail
// at load; unknown keys anywhere else must keep loading.
//
// Vacuity. A "scan and assert absence" guard has five independent ways to be a
// no-op, and a candidate-count floor closes only the first. Which test closes
// which mode:
//
//  1. it inspects no files            -> the candidate floor in
//     Test7014_AllEmbeddedRuleFilesStillLoad.
//  2. it inspects the files but never reaches the containers (every list is
//     empty, or the strict decoder is never on the decode path)
//     -> the ENTRY floor in Test7014_AllEmbeddedRuleFilesStillLoad: those
//     entries exist only because the strict UnmarshalYAML produced them.
//  3. it reaches the containers but does not detect a bad key
//     -> the positive control Test7014_UnknownKeyInsideExecutableContainerFailsLoad,
//     one leg per container.
//  4. it detects but does not act (logs and loads the file anyway)
//     -> the same test asserts the file is absent from rules AND from
//     rep.Loaded, not merely that a failure was recorded.
//  5. it acts, but the assertion is so broad anything satisfies it
//     -> the same test asserts the offending KEY NAME and the FILE PATH appear
//     in the failure, and Test7014_UnknownKeyOutsideExecutableContainersStillLoads
//     asserts the guard does NOT fire outside the three containers.

// strict7014MinContainerEntries is the non-vacuity floor for mode 2: the number
// of executable-container entries the strict decoder must actually have built
// from the real embedded tree. The tree currently yields ~445
// (329 source_patterns + 78 relationship_rules + 38 file_conventions); the floor
// is set below that so ordinary rule authoring does not trip it, but far enough
// above zero that a guard which never runs on real data cannot pass.
const strict7014MinContainerEntries = 350

// goodRuleYAML is a rule file exercising all three executable containers with
// only modelled keys. Legs below mutate one key of it.
const goodRuleYAML = `
frameworks:
  name: Widget
  detection:
    import_markers:
      - "import widget"
file_conventions:
  - glob: "**/models/*.py"
    entity_type: Model
    name_from: filename
source_patterns:
  - pattern: "widget\\.route\\((\\w+)"
    entity_type: Route
    name_group: 1
    scope: line
    requires_framework: true
relationship_rules:
  - pattern: "(\\w+)\\s*->\\s*(\\w+)"
    source_type: Route
    target_type: Handler
    relationship: ROUTES_TO
    source_group: 1
    target_group: 2
`

func loadOne(t *testing.T, name, body string) (map[string][]FrameworkRule, RuleLoadReport) {
	t.Helper()
	fsys := fstest.MapFS{
		"rules/py/frameworks/" + name: {Data: []byte(body)},
		// A known-good sibling, so "nothing loaded" cannot be mistaken for
		// "the bad file was rejected".
		"rules/py/frameworks/sibling.yaml": {Data: []byte(goodRuleYAML)},
	}
	rules, rep, err := LoadAllRulesFromFSReport(fsys, "rules")
	if err != nil {
		t.Fatalf("LoadAllRulesFromFSReport returned a hard error: %v", err)
	}
	return rules, rep
}

// Test7014_UnknownKeyInsideExecutableContainerFailsLoad is the positive
// control: one leg per guarded container. Closes vacuity modes 3, 4 and 5.
func Test7014_UnknownKeyInsideExecutableContainerFailsLoad(t *testing.T) {
	cases := []struct {
		name      string
		container string
		old, new  string
		badKey    string
	}{
		{
			name:      "source_patterns typo",
			container: "source_patterns",
			old:       "    name_group: 1", new: "    name_grup: 1",
			badKey: "name_grup",
		},
		{
			name:      "relationship_rules typo",
			container: "relationship_rules",
			old:       "    target_group: 2", new: "    targt_group: 2",
			badKey: "targt_group",
		},
		{
			name:      "file_conventions typo",
			container: "file_conventions",
			old:       "    name_from: filename", new: "    name_form: filename",
			badKey: "name_form",
		},
		{
			// The key that produced #7014, moved into an executable position.
			name:      "unmodelled metadata key inside source_patterns",
			container: "source_patterns",
			old:       "    scope: line", new: "    scope: line\n    package_json_deps: [widget]",
			badKey: "package_json_deps",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := strings.Replace(goodRuleYAML, tc.old, tc.new, 1)
			if body == goodRuleYAML {
				t.Fatalf("fixture did not change: %q not found in goodRuleYAML", tc.old)
			}
			rules, rep := loadOne(t, "typo.yaml", body)

			// Mode 4: it must not merely complain — the file must not load.
			for _, p := range rep.Loaded {
				if strings.Contains(p, "typo.yaml") {
					t.Errorf("typo.yaml was loaded despite an unknown key in %s[]", tc.container)
				}
			}
			if got := len(rules["py"]); got != 1 {
				t.Errorf("loaded %d py rules, want 1 (the good sibling only)", got)
			}

			if len(rep.Failures) != 1 {
				t.Fatalf("failures = %d, want 1: %v", len(rep.Failures), rep.Failures)
			}
			f := rep.Failures[0]
			msg := f.String()
			// Mode 5: name the file AND the key, not just "something failed".
			if !strings.Contains(f.Path, "typo.yaml") {
				t.Errorf("failure does not name the offending file: %s", msg)
			}
			if !strings.Contains(msg, tc.badKey) {
				t.Errorf("failure does not name the offending key %q: %s", tc.badKey, msg)
			}
			if !strings.Contains(msg, tc.container) {
				t.Errorf("failure does not name the container %q: %s", tc.container, msg)
			}
		})
	}
}

// Test7014_UnknownKeyOutsideExecutableContainersStillLoads pins the BOUNDARY.
//
// This is the scoping decision, and without it the next person tidying up
// extends the guard to the top level and turns 254 of 520 rule files red. Each
// key below is real and present in the tree today.
func Test7014_UnknownKeyOutsideExecutableContainersStillLoads(t *testing.T) {
	cases := []struct {
		name string
		yaml string
	}{
		{"top-level metadata", "category: web\ngithub_stars_2025: 42000\nnotes: hello\n" + goodRuleYAML},
		{"misspelled top-level container", "orms_database:\n  name: Widget\n" + goodRuleYAML},
		{"detection_signals.package_json_deps", goodRuleYAML + "detection_signals:\n  package_json_deps:\n    - widget\n"},
		{"unmodelled key under frameworks", strings.Replace(goodRuleYAML,
			"  name: Widget", "  name: Widget\n  npm_packages: [widget]", 1)},
		{"unmodelled key under frameworks.detection", strings.Replace(goodRuleYAML,
			"  detection:", "  detection:\n    package_json_deps: [widget]", 1)},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rules, rep := loadOne(t, "meta.yaml", tc.yaml)
			if len(rep.Failures) != 0 {
				t.Fatalf("the guard fired OUTSIDE the three executable containers, "+
					"which turns 254 real rule files red: %v", rep.Failures)
			}
			if got := len(rep.Loaded); got != 2 {
				t.Fatalf("loaded = %d, want 2: %v", got, rep.Loaded)
			}
			if got := len(rules["py"]); got != 2 {
				t.Fatalf("loaded %d py rules, want 2", got)
			}
			// And the executable half still decoded, so "it loaded" is not
			// "it loaded empty".
			for _, r := range rules["py"] {
				if len(r.SourcePatterns) != 1 || len(r.RelationshipRules) != 1 || len(r.FileConventions) != 1 {
					t.Errorf("rule decoded with empty containers: %+v", r)
				}
				if r.SourcePatterns[0].NameGroup != 1 || r.SourcePatterns[0].Scope != "line" ||
					!r.SourcePatterns[0].RequiresFramework {
					t.Errorf("source pattern lost its modelled fields: %+v", r.SourcePatterns[0])
				}
				if r.RelationshipRules[0].TargetGroup != 2 || r.RelationshipRules[0].Relationship != "ROUTES_TO" {
					t.Errorf("relationship rule lost its modelled fields: %+v", r.RelationshipRules[0])
				}
				if r.FileConventions[0].NameFrom != "filename" || r.FileConventions[0].EntityType != "Model" {
					t.Errorf("file convention lost its modelled fields: %+v", r.FileConventions[0])
				}
			}
		})
	}
}

// Test7014_AllEmbeddedRuleFilesStillLoad is the 0-red measurement expressed as
// a test rather than a one-off: every real rule file must still load under the
// scoped strict guard. Closes vacuity modes 1 and 2.
func Test7014_AllEmbeddedRuleFilesStillLoad(t *testing.T) {
	rules, rep, err := LoadAllRulesReport()
	if err != nil {
		t.Fatalf("LoadAllRulesReport: %v", err)
	}

	// Mode 1: a guard that inspected no files proves nothing.
	if len(rep.Candidates) < expectedMinRuleFiles {
		t.Fatalf("only %d rule files found, want >= %d: the measurement is vacuous",
			len(rep.Candidates), expectedMinRuleFiles)
	}
	if len(rep.Failures) != 0 {
		t.Fatalf("the scoped strict guard turned %d real rule files RED, which makes it a "+
			"migration rather than the free guard #7014 sized:\n%s",
			len(rep.Failures), failureLines(rep))
	}
	if len(rep.Loaded) != len(rep.Candidates) {
		t.Fatalf("loaded %d of %d rule files", len(rep.Loaded), len(rep.Candidates))
	}

	// Mode 2: every entry counted here was built BY the strict UnmarshalYAML —
	// it is the only decode path into these types. A guard bypassed on the real
	// tree cannot produce them.
	entries := 0
	for _, rs := range rules {
		for _, r := range rs {
			entries += len(r.SourcePatterns) + len(r.RelationshipRules) + len(r.FileConventions)
		}
	}
	if entries < strict7014MinContainerEntries {
		t.Fatalf("strict decoder produced only %d executable-container entries from the real "+
			"rule tree, want >= %d: the guard is not on the real decode path",
			entries, strict7014MinContainerEntries)
	}
	t.Logf("0 red: %d/%d rule files loaded, %d executable-container entries decoded strictly",
		len(rep.Loaded), len(rep.Candidates), entries)
}

func failureLines(rep RuleLoadReport) string {
	var b strings.Builder
	for _, f := range rep.Failures {
		fmt.Fprintf(&b, "  %s\n", f)
	}
	return b.String()
}
