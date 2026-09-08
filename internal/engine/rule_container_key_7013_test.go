package engine

import (
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"gopkg.in/yaml.v3"
)

// This file is the guard for step 1 of #7013.
//
// FrameworkRule models exactly one top-level container, `frameworks:`, and
// that container is the only route by which `detection.import_markers` reaches
// the engine (loader.go unmarshals into FrameworkRule; detector.go:153 copies
// fr.Frameworks.Detection.ImportMarkers into the compiled rule set, and
// frameworkPresent is the sole consumer). yaml.Unmarshal drops unknown
// top-level keys silently, so a file that spells the container `orms_database:`
// parses successfully and hands the engine no markers at all.
//
// The #7014 audit measured that: 247 of the 520 loader-scoped rule files keyed
// their container on one of 14 misspellings — `orms_database` (121), `orms`
// (35), `database_libraries` (14), `template_engines` (11), `orms_and_database`
// (11), `orm_db` (10), `orm_database` (9), `frameworks_and_dialects` (9),
// `database` (8), `orms_databases` (6), `framework_templates` (5), `mobile` (4),
// `framework` (3), `queues` (1) — and 52 of those 247 carried real, correct
// `detection.import_markers` the engine could not see. The markers were written;
// they were unreadable.
//
// The 247 files were reconciled onto `frameworks:`. The two assertions here are
// the artefact, not the count:
//
//   - specific named files' markers load through the loader's own decode path,
//     with their exact contents; and
//   - no loader-scoped rule file anywhere in the tree keeps `import_markers`
//     under a container the schema does not model.
//
// The second is what makes the fix non-recurring: re-introducing any of the 14
// spellings — or inventing a fifteenth — fails here by file name and key name,
// whether it arrives as a regression or as a new rule file written to the old
// habit.
//
// Deliberately out of scope: the rest of the metadata half. 465 unmodelled key
// paths remain (`category`, `notes`, `npm_packages`, `detection_signals.*`, …)
// and are still dropped at load; that is the tracked migration on #7014, not
// this guard. This guard is about the ONE key that gates a live mechanism.

// hiddenMarkerFile names one rule file that declares import_markers under a
// top-level container the schema does not model.
type hiddenMarkerFile struct {
	path string
	key  string
}

// importMarkerVisibility walks the loader-scoped rule files under rootDir and
// reports, for each, whether its declared import_markers are reachable by the
// engine.
//
// hidden lists files whose markers sit under an unmodelled top-level container
// (the #7013 defect). visible counts files whose markers sit under
// `frameworks:` and therefore load. examined counts loader-scoped files looked
// at, so a caller can tell "nothing hidden" from "nothing scanned" — a walk
// that finds no files would otherwise report a clean tree.
//
// It is a free function so the checker can be run against a synthetic tree; a
// checker that always answers "all visible" is worth nothing on the real one.
func importMarkerVisibility(fsys fs.FS, rootDir string) (hidden []hiddenMarkerFile, visible, examined int, err error) {
	walkErr := fs.WalkDir(fsys, rootDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if ext := filepath.Ext(path); ext != ".yaml" && ext != ".yml" {
			return nil
		}
		rel, relErr := filepath.Rel(rootDir, path)
		if relErr != nil {
			return nil
		}
		// Mirror the loader: only <lang>/<subdir>/<file>.yaml, supported subdir.
		parts := strings.Split(filepath.ToSlash(rel), "/")
		if len(parts) != 3 || !ruleSubdirs[parts[1]] {
			return nil
		}
		data, readErr := fs.ReadFile(fsys, path)
		if readErr != nil {
			return nil
		}
		var raw map[string]any
		if yaml.Unmarshal(data, &raw) != nil {
			// A parse failure is a different defect, caught by the #6341
			// inventory assertion. Not hidden — absent.
			return nil
		}
		examined++
		for key, val := range raw {
			if !declaresImportMarkers(val) {
				continue
			}
			if key == "frameworks" {
				visible++
				continue
			}
			hidden = append(hidden, hiddenMarkerFile{path: filepath.ToSlash(path), key: key})
		}
		return nil
	})
	return hidden, visible, examined, walkErr
}

// declaresImportMarkers reports whether a top-level container value carries a
// non-empty detection.import_markers list.
func declaresImportMarkers(val any) bool {
	container, ok := val.(map[string]any)
	if !ok {
		return false
	}
	detection, ok := container["detection"].(map[string]any)
	if !ok {
		return false
	}
	markers, ok := detection["import_markers"].([]any)
	return ok && len(markers) > 0
}

// TestRuleTree_NoImportMarkersHiddenUnderAnUnmodelledContainer is the assertion
// over the real embedded tree. Every rule file's import_markers must sit under
// `frameworks:`, the one container FrameworkRule models.
func TestRuleTree_NoImportMarkersHiddenUnderAnUnmodelledContainer(t *testing.T) {
	hidden, visible, examined, err := importMarkerVisibility(rulesFS, "rules")
	if err != nil {
		t.Fatalf("walking the embedded rules tree: %v", err)
	}

	// Non-vacuity, both halves. A walk that scanned nothing, or a tree whose
	// markers had been "reconciled" by deleting them, must not read as clean.
	if examined < 500 {
		t.Fatalf("examined %d loader-scoped rule files, want >= 500: the guard is "+
			"scanning a fraction of the tree, so its verdict is not about the tree", examined)
	}
	if visible < 147 {
		t.Errorf("%d rule files declare readable frameworks.detection.import_markers, want >= 147: "+
			"95 were readable before #7013 step 1 and 52 more were recovered from misspelled "+
			"containers; a drop means markers were deleted rather than reconciled", visible)
	}

	for _, h := range hidden {
		t.Errorf("%s declares detection.import_markers under top-level key %q, which "+
			"FrameworkRule does not model — the markers are dropped at load and "+
			"requires_framework cannot evaluate against them (#7013, #7014). "+
			"The container is spelled `frameworks:`.", h.path, h.key)
	}
}

// reconciledContainerSpellings are the 14 top-level keys the 247 files were
// keyed on before #7013 step 1, measured over the loader-scoped tree. They are
// listed as the closed set they are: a file using any of them again is a
// regression of this change, whether or not it happens to carry markers today.
//
// This is narrower than the strict top-level guard #7014 priced (247 red files,
// a migration): it forbids the spellings that were actually removed, and costs
// nothing.
var reconciledContainerSpellings = []string{
	"orms_database", "orms", "database_libraries", "template_engines",
	"orms_and_database", "orm_db", "orm_database", "frameworks_and_dialects",
	"database", "orms_databases", "framework_templates", "mobile",
	"framework", "queues",
}

// TestRuleTree_NoReconciledContainerSpellingReturns keeps the tree at one
// spelling. TestRuleTree_NoImportMarkersHiddenUnderAnUnmodelledContainer only
// sees a file once it carries markers; a file re-keyed on `orms_database:` with
// no markers yet is invisible to it, and is exactly how the next 52 would
// accumulate.
func TestRuleTree_NoReconciledContainerSpellingReturns(t *testing.T) {
	banned := make(map[string]bool, len(reconciledContainerSpellings))
	for _, k := range reconciledContainerSpellings {
		banned[k] = true
	}

	examined := 0
	err := fs.WalkDir(rulesFS, "rules", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		if ext := filepath.Ext(path); ext != ".yaml" && ext != ".yml" {
			return nil
		}
		rel, relErr := filepath.Rel("rules", path)
		if relErr != nil {
			return nil
		}
		parts := strings.Split(filepath.ToSlash(rel), "/")
		if len(parts) != 3 || !ruleSubdirs[parts[1]] {
			return nil
		}
		data, readErr := fs.ReadFile(rulesFS, path)
		if readErr != nil {
			return nil
		}
		var raw map[string]any
		if yaml.Unmarshal(data, &raw) != nil {
			return nil
		}
		examined++
		for key := range raw {
			if banned[key] {
				t.Errorf("%s keys its detection metadata on %q — one of the 14 spellings "+
					"reconciled onto `frameworks:` in #7013 step 1. The engine models "+
					"`frameworks:` and nothing else, so everything under %q is dropped at "+
					"load, including any import_markers added later.",
					filepath.ToSlash(path), key, key)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking the embedded rules tree: %v", err)
	}
	if examined < 500 {
		t.Fatalf("examined %d loader-scoped rule files, want >= 500", examined)
	}
}

// TestRuleTree_RecoveredImportMarkersLoad pins the artefact this change exists
// to produce: the named files whose markers were unreadable before the
// reconciliation now decode, with their exact contents, through the same
// yaml.Unmarshal-into-FrameworkRule step the loader performs.
//
// One file per formerly-wrong spelling that carried markers.
func TestRuleTree_RecoveredImportMarkersLoad(t *testing.T) {
	cases := []struct {
		path        string // embedded path
		wasKeyedOn  string // the container spelling it used before #7013 step 1
		name        string // frameworks.name, also unreadable before
		wantMarkers []string
	}{
		{
			path:       "rules/csharp/orms/mongodb_csharp.yaml",
			wasKeyedOn: "orms_database",
			name:       "MongoDB.Driver (C# official driver)",
			wantMarkers: []string{
				"using MongoDB.Driver",
				"using MongoDB.Bson",
			},
		},
		{
			path:       "rules/csharp/orms/redis_stackexchange.yaml",
			wasKeyedOn: "orms",
			name:       "StackExchange.Redis",
			wantMarkers: []string{
				"using StackExchange.Redis",
				"using ServiceStack.Redis",
			},
		},
		{
			path:       "rules/go/orms/bun.yaml",
			wasKeyedOn: "database_libraries",
			name:       "bun",
			wantMarkers: []string{
				`"github.com/uptrace/bun"`,
				`"github.com/uptrace/bun/dialect/pgdialect"`,
				`"github.com/uptrace/bun/dialect/mysqldialect"`,
			},
		},
		{
			path:       "rules/lua/orms/lua_resty_mysql.yaml",
			wasKeyedOn: "database",
			name:       "lua-resty-mysql",
			wantMarkers: []string{
				`require "resty.mysql"`,
				`require("resty.mysql")`,
				"resty.mysql",
				"db:connect",
				"db:query",
				"db:close",
				"mysql:new()",
			},
		},
		{
			path:       "rules/zig/orms/zig_sqlite.yaml",
			wasKeyedOn: "orm_database",
			name:       "zig-sqlite",
			wantMarkers: []string{
				`@import("sqlite")`,
				`const sqlite = @import("sqlite")`,
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			data, err := fs.ReadFile(rulesFS, tc.path)
			if err != nil {
				t.Fatalf("reading embedded rule file: %v", err)
			}
			var rule FrameworkRule
			if err := yaml.Unmarshal(data, &rule); err != nil {
				t.Fatalf("unmarshalling into FrameworkRule (the loader's own step): %v", err)
			}
			if rule.Frameworks.Name != tc.name {
				t.Errorf("frameworks.name = %q, want %q (it was keyed on %q and unreadable before #7013)",
					rule.Frameworks.Name, tc.name, tc.wasKeyedOn)
			}
			got := rule.Frameworks.Detection.ImportMarkers
			if len(got) != len(tc.wantMarkers) {
				t.Fatalf("import_markers = %q (%d), want %q (%d): these markers were "+
					"written under %q and reached the engine as an empty slice",
					got, len(got), tc.wantMarkers, len(tc.wantMarkers), tc.wasKeyedOn)
			}
			for i := range got {
				if got[i] != tc.wantMarkers[i] {
					t.Errorf("import_markers[%d] = %q, want %q", i, got[i], tc.wantMarkers[i])
				}
			}
		})
	}
}

// TestLoader_DoesNotAdoptAnUnmodelledContainer records the decision this change
// made: the FILES were reconciled onto one spelling, the LOADER was not taught
// the other fourteen.
//
// Aliasing was the small diff — a handful of extra yaml tags, no rule files
// touched — and it is the one that cannot be undone, because it makes every
// spelling correct forever and reads to the next author as an invitation to add
// a fifteenth. Without this test that choice is unpinned: a loader that quietly
// adopts markers from whatever container it finds passes every other assertion
// here, since after the reconciliation no rule file offers it such a container
// to adopt.
func TestLoader_DoesNotAdoptAnUnmodelledContainer(t *testing.T) {
	fsys := fstest.MapFS{
		"rules/csharp/orms/mongodb_csharp.yaml": {Data: []byte(
			"orms_database:\n  name: MongoDB\n  detection:\n    import_markers:\n    - \"using MongoDB.Driver\"\n")},
	}
	rules, err := LoadAllRulesFromFS(fsys, "rules")
	if err != nil {
		t.Fatalf("LoadAllRulesFromFS: %v", err)
	}
	got, ok := rules["csharp"]
	if !ok || len(got) != 1 {
		t.Fatalf("loaded %d csharp rule sets, want 1", len(got))
	}
	if n := len(got[0].Frameworks.Detection.ImportMarkers); n != 0 {
		t.Errorf("the loader adopted %d import_markers from a container it does not model "+
			"(%q): #7013 step 1 reconciled the rule files onto `frameworks:` rather than "+
			"teaching the loader the 14 misspellings, so that the tree keeps one spelling. "+
			"An adopting loader blesses all of them and the guards above go quiet.",
			n, "orms_database")
	}
	if got[0].Frameworks.Name != "" {
		t.Errorf("the loader adopted frameworks.name = %q from an unmodelled container",
			got[0].Frameworks.Name)
	}
}

// TestImportMarkerVisibility_FindsAHiddenContainer proves the checker fires.
// Without it, an importMarkerVisibility that returned nil unconditionally would
// make the tree assertion pass forever.
func TestImportMarkerVisibility_FindsAHiddenContainer(t *testing.T) {
	fsys := fstest.MapFS{
		// The real shape of the defect, in two of the 14 spellings.
		"rules/csharp/orms/mongodb_csharp.yaml": {Data: []byte(
			"orms_database:\n  name: MongoDB\n  detection:\n    import_markers:\n    - \"using MongoDB.Driver\"\n")},
		"rules/lua/orms/lsqlite3.yaml": {Data: []byte(
			"database:\n  name: lsqlite3\n  detection:\n    import_markers:\n    - 'require \"lsqlite3\"'\n")},
		// Reconciled: readable, and must not be flagged.
		"rules/go/orms/bun.yaml": {Data: []byte(
			"frameworks:\n  name: bun\n  detection:\n    import_markers:\n    - '\"github.com/uptrace/bun\"'\n")},
		// A wrong container with no markers at all: out of this guard's scope,
		// it is part of the #7014 metadata migration, not a hidden gate.
		"rules/go/orms/ent.yaml": {Data: []byte(
			"orms_database:\n  name: ent\n  category: orm\n")},
		// Not loader-scoped: two path segments, and an unsupported subdir.
		"rules/csharp/_manifest.yaml": {Data: []byte(
			"orms_database:\n  detection:\n    import_markers:\n    - x\n")},
		"rules/csharp/notes/reference.yaml": {Data: []byte(
			"orms_database:\n  detection:\n    import_markers:\n    - x\n")},
	}

	hidden, visible, examined, err := importMarkerVisibility(fsys, "rules")
	if err != nil {
		t.Fatalf("importMarkerVisibility: %v", err)
	}
	if examined != 4 {
		t.Errorf("examined = %d, want 4 (only <lang>/<subdir>/*.yaml in a supported subdir)", examined)
	}
	if visible != 1 {
		t.Errorf("visible = %d, want 1", visible)
	}
	got := make([]string, 0, len(hidden))
	for _, h := range hidden {
		got = append(got, h.path+":"+h.key)
	}
	want := []string{
		"rules/csharp/orms/mongodb_csharp.yaml:orms_database",
		"rules/lua/orms/lsqlite3.yaml:database",
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("hidden = %v, want %v", got, want)
	}
}

// TestImportMarkerVisibility_EmptyMarkerListIsNotHidden pins the other
// direction. `detection.import_markers:` present but empty carries nothing for
// the engine to lose, so flagging it would make the guard noisy about files
// that have no gate to hide.
func TestImportMarkerVisibility_EmptyMarkerListIsNotHidden(t *testing.T) {
	fsys := fstest.MapFS{
		"rules/go/orms/ent.yaml": {Data: []byte(
			"orms_database:\n  name: ent\n  detection:\n    import_markers: []\n")},
	}
	hidden, visible, examined, err := importMarkerVisibility(fsys, "rules")
	if err != nil {
		t.Fatalf("importMarkerVisibility: %v", err)
	}
	if examined != 1 || visible != 0 {
		t.Fatalf("examined = %d, visible = %d, want 1 and 0", examined, visible)
	}
	if len(hidden) != 0 {
		t.Errorf("an empty import_markers list was reported hidden: %v", hidden)
	}
}
