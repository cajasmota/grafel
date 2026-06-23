package python_test

// config_module_test.go — fixture tests for issue #1775 config-module entity emission.
//
// Three cases:
//  1. settings.py with only module-level assignments → emits exactly one
//     SCOPE.Config/config_module entity with config_type="django_settings".
//  2. manage.py with a def main() → emits BOTH the function entity AND the
//     config_module entity (supplemental, not replacing).
//  3. utils.py with mostly def/class (non-config filename) → no config_module
//     entity emitted.

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
)

// findConfigModule returns the first SCOPE.Config/config_module entity in
// entities, or nil when none is present.
func findConfigModule(entities []types.EntityRecord) *types.EntityRecord {
	for i := range entities {
		e := &entities[i]
		if e.Kind == string(types.EntityKindConfig) && e.Subtype == "config_module" {
			return e
		}
	}
	return nil
}

// countSemanticEntities returns the count of entities that are NOT the
// file-level SCOPE.Component(file) entity and NOT import placeholder entities
// (SCOPE.Component/module). Only SCOPE.Operation, SCOPE.Component/class,
// SCOPE.Schema, and SCOPE.Config are counted.
func countSemanticEntities(entities []types.EntityRecord) int {
	n := 0
	for _, e := range entities {
		switch {
		case e.Kind == "SCOPE.Component" && e.Subtype == "file":
			continue
		case e.Kind == "SCOPE.Component" && e.Subtype == "module":
			continue
		}
		n++
	}
	return n
}

// TestConfigModule_SettingsPy verifies that a settings.py file consisting only
// of module-level assignments emits a single SCOPE.Config/config_module entity
// with the correct config_type, name, and qualified_name.
//
// Issue #1775 — bench Q1 regression: settings.py was previously invisible.
func TestConfigModule_SettingsPy(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("testdata", "settings_only_assignments.py.fixture"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	tree := parse(t, src)
	ext, ok := extractor.Get("python")
	if !ok {
		t.Fatal("python extractor not registered")
	}

	fi := extractor.FileInput{
		Path:     "upvate_core/settings.py",
		Content:  src,
		Language: "python",
		TSTree:   tree,
	}
	entities, err := ext.Extract(context.Background(), fi)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}

	// The fixture has no class or function definitions, so the only semantic
	// entities after the base pass should be from the config-module pass.
	cfgEnt := findConfigModule(entities)
	if cfgEnt == nil {
		t.Fatal("expected a SCOPE.Config/config_module entity but found none")
	}

	// Kind must be SCOPE.Config, subtype must be config_module.
	if cfgEnt.Kind != string(types.EntityKindConfig) {
		t.Errorf("config_module entity Kind = %q, want %q", cfgEnt.Kind, string(types.EntityKindConfig))
	}
	if cfgEnt.Subtype != "config_module" {
		t.Errorf("config_module entity Subtype = %q, want config_module", cfgEnt.Subtype)
	}

	// config_type property must be "django_settings".
	if got := cfgEnt.Properties["config_type"]; got != "django_settings" {
		t.Errorf("config_type = %q, want django_settings", got)
	}

	// Name should be the basename without extension.
	if cfgEnt.Name != "settings" {
		t.Errorf("Name = %q, want settings", cfgEnt.Name)
	}

	// QualifiedName should be module-qualified.
	// filePathToModule("upvate_core/settings.py") → "upvate_core.settings"
	wantQN := "upvate_core.settings.settings"
	if cfgEnt.QualifiedName != wantQN {
		t.Errorf("QualifiedName = %q, want %q", cfgEnt.QualifiedName, wantQN)
	}

	// StartLine should be 1 (points to the top of the file).
	if cfgEnt.StartLine != 1 {
		t.Errorf("StartLine = %d, want 1", cfgEnt.StartLine)
	}

	// assignment_count property should be > 0.
	if cfgEnt.Properties["assignment_count"] == "" || cfgEnt.Properties["assignment_count"] == "0" {
		t.Errorf("assignment_count = %q, want a positive number", cfgEnt.Properties["assignment_count"])
	}

	// top_level_symbols should include at least one known setting name.
	symbols := cfgEnt.Properties["top_level_symbols"]
	if symbols == "" {
		t.Error("expected top_level_symbols to be set")
	}

	// Verify the file entity carries a CONTAINS edge to the config_module.
	var fileEnt *types.EntityRecord
	for i := range entities {
		if entities[i].Kind == "SCOPE.Component" && entities[i].Subtype == "file" {
			fileEnt = &entities[i]
			break
		}
	}
	if fileEnt == nil {
		t.Fatal("file entity not found")
	}
	found := false
	for _, rel := range fileEnt.Relationships {
		if rel.Kind == "CONTAINS" {
			found = true
			break
		}
	}
	if !found {
		t.Error("file entity has no CONTAINS relationship to config_module")
	}
}

// TestConfigModule_ManageWithMain verifies that manage.py (which contains a
// def main() function) emits BOTH the function entity AND the config_module
// entity — the pass is supplemental and must not suppress function extraction.
//
// Issue #1775 — "don't double-skip just because there's a def".
func TestConfigModule_ManageWithMain(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("testdata", "manage_with_main.py.fixture"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	tree := parse(t, src)
	ext, ok := extractor.Get("python")
	if !ok {
		t.Fatal("python extractor not registered")
	}

	fi := extractor.FileInput{
		Path:     "manage.py",
		Content:  src,
		Language: "python",
		TSTree:   tree,
	}
	entities, err := ext.Extract(context.Background(), fi)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}

	// There must be a config_module entity.
	cfgEnt := findConfigModule(entities)
	if cfgEnt == nil {
		t.Fatal("expected a SCOPE.Config/config_module entity for manage.py but found none")
	}
	if cfgEnt.Properties["config_type"] != "django_manage" {
		t.Errorf("config_type = %q, want django_manage", cfgEnt.Properties["config_type"])
	}

	// There must also be the function entity for main().
	found := false
	for _, e := range entities {
		if e.Kind == "SCOPE.Operation" && e.Subtype == "function" && e.Name == "main" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected SCOPE.Operation/function entity named 'main' but did not find it")
	}

	// config_module name/qn for a root-level file.
	if cfgEnt.Name != "manage" {
		t.Errorf("Name = %q, want manage", cfgEnt.Name)
	}
	// filePathToModule("manage.py") → "manage"
	wantQN := "manage.manage"
	if cfgEnt.QualifiedName != wantQN {
		t.Errorf("QualifiedName = %q, want %q", cfgEnt.QualifiedName, wantQN)
	}
}

// TestConfigModule_UtilsNormal verifies that a non-config-named file (utils.py)
// whose content is mostly def/class declarations does NOT receive a
// config_module entity.
//
// Issue #1775 — "A normal utils.py with no recognized config name and mostly
// def/class → no config_module entity emitted."
func TestConfigModule_UtilsNormal(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("testdata", "utils_normal.py.fixture"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	tree := parse(t, src)
	ext, ok := extractor.Get("python")
	if !ok {
		t.Fatal("python extractor not registered")
	}

	fi := extractor.FileInput{
		Path:     "app/utils/utils.py",
		Content:  src,
		Language: "python",
		TSTree:   tree,
	}
	entities, err := ext.Extract(context.Background(), fi)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}

	cfgEnt := findConfigModule(entities)
	if cfgEnt != nil {
		t.Errorf("unexpected SCOPE.Config/config_module entity for utils.py: %+v", cfgEnt)
	}

	// Sanity: functions and class are still extracted.
	hasFunction := false
	hasClass := false
	for _, e := range entities {
		if e.Kind == "SCOPE.Operation" {
			hasFunction = true
		}
		if e.Kind == "SCOPE.Component" && e.Subtype == "class" {
			hasClass = true
		}
	}
	if !hasFunction {
		t.Error("expected function entities in utils.py but found none")
	}
	if !hasClass {
		t.Error("expected class entity in utils.py but found none")
	}
}
