package engine

import (
	"context"
	"testing"
	"testing/fstest"

	"gopkg.in/yaml.v3"

	"github.com/cajasmota/archigraph/internal/extractor"
)

// ---------------------------------------------------------------------------
// Unit: FileConvention YAML round-trip
// ---------------------------------------------------------------------------

// TestFileConvention_YAMLParse verifies that the three new fields
// (glob, entity_type, name_from) are correctly parsed from YAML into the
// FileConvention struct. Prior to #2348 these fields were silently dropped.
func TestFileConvention_YAMLParse(t *testing.T) {
	raw := `
file_conventions:
  - glob: "*/migrations/0*.py"
    entity_type: Migration
    name_from: filename
    description: "Django numbered migration files"
  - glob: "*/models.py"
    entity_type: Model
    name_from: class_name
  - glob: "*/urls.py"
    entity_type: Route
    name_from: filename
source_patterns: []
relationship_rules: []
`
	var rule FrameworkRule
	if err := yaml.Unmarshal([]byte(raw), &rule); err != nil {
		t.Fatalf("yaml.Unmarshal: %v", err)
	}

	if got := len(rule.FileConventions); got != 3 {
		t.Fatalf("FileConventions: want 3, got %d", got)
	}

	cases := []struct {
		glob       string
		entityType string
		nameFrom   string
	}{
		{"*/migrations/0*.py", "Migration", "filename"},
		{"*/models.py", "Model", "class_name"},
		{"*/urls.py", "Route", "filename"},
	}
	for i, tc := range cases {
		fc := rule.FileConventions[i]
		if fc.Glob != tc.glob {
			t.Errorf("[%d] Glob: want %q, got %q", i, tc.glob, fc.Glob)
		}
		if fc.EntityType != tc.entityType {
			t.Errorf("[%d] EntityType: want %q, got %q", i, tc.entityType, fc.EntityType)
		}
		if fc.NameFrom != tc.nameFrom {
			t.Errorf("[%d] NameFrom: want %q, got %q", i, tc.nameFrom, fc.NameFrom)
		}
	}
}

// TestFileConvention_LegacyFields verifies that the legacy fields
// (pattern, description) are still parsed alongside the new fields.
func TestFileConvention_LegacyFields(t *testing.T) {
	raw := `
file_conventions:
  - pattern: "*.go"
    description: "Go source file"
    glob: "*/handlers/*.go"
    entity_type: Controller
    name_from: filename
source_patterns: []
relationship_rules: []
`
	var rule FrameworkRule
	if err := yaml.Unmarshal([]byte(raw), &rule); err != nil {
		t.Fatalf("yaml.Unmarshal: %v", err)
	}
	if len(rule.FileConventions) != 1 {
		t.Fatalf("want 1 convention, got %d", len(rule.FileConventions))
	}
	fc := rule.FileConventions[0]
	if fc.Pattern != "*.go" {
		t.Errorf("Pattern: want *.go, got %q", fc.Pattern)
	}
	if fc.Description != "Go source file" {
		t.Errorf("Description: want 'Go source file', got %q", fc.Description)
	}
	if fc.Glob != "*/handlers/*.go" {
		t.Errorf("Glob: want */handlers/*.go, got %q", fc.Glob)
	}
	if fc.EntityType != "Controller" {
		t.Errorf("EntityType: want Controller, got %q", fc.EntityType)
	}
}

// ---------------------------------------------------------------------------
// Unit: compiledFileConvention.matchesFile
// ---------------------------------------------------------------------------

func TestCompiledFileConvention_MatchesFile(t *testing.T) {
	cases := []struct {
		glob    string
		path    string
		matches bool
	}{
		// Migration glob from django.yaml
		{"*/migrations/0*.py", "core/migrations/0042_device_serial_number.py", true},
		{"*/migrations/0*.py", "app/migrations/0001_initial.py", true},
		{"*/migrations/0*.py", "core/migrations/__init__.py", false},
		{"*/migrations/0*.py", "core/models.py", false},
		// Models glob
		{"*/models.py", "app/models.py", true},
		{"*/models.py", "app/admin.py", false},
		// Multi-segment glob
		{"*/models/*.py", "myapp/models/base.py", true},
		{"*/models/*.py", "myapp/views/base.py", false},
	}
	for _, tc := range cases {
		cfc := compiledFileConvention{glob: tc.glob, entityType: "Test", nameFrom: "filename"}
		got := cfc.matchesFile(tc.path)
		if got != tc.matches {
			t.Errorf("matchesFile(glob=%q, path=%q): want %v, got %v", tc.glob, tc.path, tc.matches, got)
		}
	}
}

// ---------------------------------------------------------------------------
// Unit: fileConventionName helper
// ---------------------------------------------------------------------------

func TestFileConventionName(t *testing.T) {
	cases := []struct {
		path     string
		nameFrom string
		want     string
	}{
		{"core/migrations/0042_device_serial_number.py", "filename", "0042_device_serial_number"},
		{"app/migrations/0001_initial.py", "filename", "0001_initial"},
		{"myapp/models/base.py", "parent_dir", "models"},
		{"pkg/handlers/auth.go", "filename", "auth"},
		{"config.yaml", "filename", "config"},
	}
	for _, tc := range cases {
		got := fileConventionName(tc.path, tc.nameFrom)
		if got != tc.want {
			t.Errorf("fileConventionName(%q, %q) = %q, want %q", tc.path, tc.nameFrom, got, tc.want)
		}
	}
}

// ---------------------------------------------------------------------------
// Integration: Detector dispatches Migration entity via file_convention glob
// ---------------------------------------------------------------------------

// migrationConventionYAML is a minimal rule set with just the Migration
// file_convention (the glob that was silently ignored before #2348).
const migrationConventionYAML = `
file_conventions:
  - glob: "*/migrations/0*.py"
    entity_type: Migration
    name_from: filename
source_patterns: []
relationship_rules: []
`

// TestDetector_FileConvention_MigrationGlob verifies that the detector emits
// a Migration entity (name = filename stem) for a file whose path matches the
// "*/migrations/0*.py" glob, and does NOT emit one for a non-matching path.
func TestDetector_FileConvention_MigrationGlob(t *testing.T) {
	// Load the rule via our test FS helper so we exercise LoadAllRulesFromFS.
	fsys := fstest.MapFS{
		"rules/python/frameworks/test_migration.yaml": &fstest.MapFile{
			Data: []byte(migrationConventionYAML),
		},
	}
	rules, err := LoadAllRulesFromFS(fsys, "rules")
	if err != nil {
		t.Fatalf("LoadAllRulesFromFS: %v", err)
	}

	det := New(rules)
	ctx := context.Background()

	t.Run("matching file emits Migration entity", func(t *testing.T) {
		result, err := det.Detect(ctx, extractor.FileInput{
			Path:     "core/migrations/0042_device_serial_number.py",
			Language: "python",
			Content:  []byte("# auto-generated migration"),
		})
		if err != nil {
			t.Fatalf("Detect: %v", err)
		}

		var migrationEntities []string
		for _, e := range result.Entities {
			if e.Kind == "Migration" {
				migrationEntities = append(migrationEntities, e.Name)
			}
		}
		if len(migrationEntities) == 0 {
			t.Fatalf("want at least 1 Migration entity, got 0 (entities: %+v)", result.Entities)
		}
		if migrationEntities[0] != "0042_device_serial_number" {
			t.Errorf("Migration name: want 0042_device_serial_number, got %q", migrationEntities[0])
		}
	})

	t.Run("non-matching file does not emit Migration entity", func(t *testing.T) {
		result, err := det.Detect(ctx, extractor.FileInput{
			Path:     "core/models.py",
			Language: "python",
			Content:  []byte("from django.db import models"),
		})
		if err != nil {
			t.Fatalf("Detect: %v", err)
		}
		for _, e := range result.Entities {
			if e.Kind == "Migration" {
				t.Errorf("unexpected Migration entity on non-migration file: %+v", e)
			}
		}
	})

	t.Run("__init__.py in migrations/ does not match 0* glob", func(t *testing.T) {
		result, err := det.Detect(ctx, extractor.FileInput{
			Path:     "core/migrations/__init__.py",
			Language: "python",
			Content:  []byte(""),
		})
		if err != nil {
			t.Fatalf("Detect: %v", err)
		}
		for _, e := range result.Entities {
			if e.Kind == "Migration" {
				t.Errorf("unexpected Migration entity for __init__.py: %+v", e)
			}
		}
	})
}

// TestDetector_FileConvention_PatternType verifies that entities emitted by a
// file_convention carry pattern_type="file_convention" in their Properties.
func TestDetector_FileConvention_PatternType(t *testing.T) {
	fsys := fstest.MapFS{
		"rules/python/frameworks/test_migration.yaml": &fstest.MapFile{
			Data: []byte(migrationConventionYAML),
		},
	}
	rules, err := LoadAllRulesFromFS(fsys, "rules")
	if err != nil {
		t.Fatalf("LoadAllRulesFromFS: %v", err)
	}

	det := New(rules)
	ctx := context.Background()
	result, err := det.Detect(ctx, extractor.FileInput{
		Path:     "app/migrations/0001_initial.py",
		Language: "python",
		Content:  []byte("# migration"),
	})
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}

	for _, e := range result.Entities {
		if e.Kind == "Migration" {
			if e.Properties["pattern_type"] != "file_convention" {
				t.Errorf("Migration entity pattern_type: want file_convention, got %q", e.Properties["pattern_type"])
			}
			return
		}
	}
	t.Fatal("no Migration entity found")
}

// ---------------------------------------------------------------------------
// Integration: django.yaml embedded rules load and contain Migration convention
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// Integration: bridge #2383 — file_convention property on source-pattern entities
// ---------------------------------------------------------------------------

// bridgeConventionYAML defines a rule set with BOTH a file_convention (class_name)
// and source_patterns — the combination that #2383 targets.
const bridgeConventionYAML = `
file_conventions:
  - glob: "*/migrations/0*.py"
    entity_type: Migration
    name_from: class_name
source_patterns:
  - pattern: "class (\\w+Migration)\\b"
    entity_type: Migration
    name_group: 1
relationship_rules: []
`

// bridgeTwoConventionsYAML exercises the multi-convention comma-join path.
const bridgeTwoConventionsYAML = `
file_conventions:
  - glob: "*/migrations/0*.py"
    entity_type: Migration
    name_from: class_name
  - glob: "*/migrations/*.py"
    entity_type: Migration
    name_from: class_name
source_patterns:
  - pattern: "class (\\w+Migration)\\b"
    entity_type: Migration
    name_group: 1
relationship_rules: []
`

// TestDetector_Bridge_FileConvention_AnnotatesSourcePatternEntities verifies that
// a source-pattern entity emitted for a file that ALSO matches a file_convention
// glob (name_from=class_name) carries Properties["file_convention"] = <glob>.
func TestDetector_Bridge_FileConvention_AnnotatesSourcePatternEntities(t *testing.T) {
	fsys := fstest.MapFS{
		"rules/python/frameworks/test_bridge.yaml": &fstest.MapFile{
			Data: []byte(bridgeConventionYAML),
		},
	}
	rules, err := LoadAllRulesFromFS(fsys, "rules")
	if err != nil {
		t.Fatalf("LoadAllRulesFromFS: %v", err)
	}

	det := New(rules)
	ctx := context.Background()

	t.Run("matching file: source-pattern entity carries file_convention property", func(t *testing.T) {
		result, err := det.Detect(ctx, extractor.FileInput{
			Path:     "core/migrations/0042_add_device.py",
			Language: "python",
			Content:  []byte("class AddDeviceMigration(Migration):\n    pass\n"),
		})
		if err != nil {
			t.Fatalf("Detect: %v", err)
		}

		found := false
		for _, e := range result.Entities {
			if e.Kind == "Migration" && e.Properties["pattern_type"] == "yaml_driven" {
				found = true
				got := e.Properties["file_convention"]
				if got != "*/migrations/0*.py" {
					t.Errorf("file_convention property: want %q, got %q", "*/migrations/0*.py", got)
				}
			}
		}
		if !found {
			t.Fatalf("no yaml_driven Migration entity found; entities: %+v", result.Entities)
		}
	})

	t.Run("non-matching file: source-pattern entities have no file_convention property", func(t *testing.T) {
		result, err := det.Detect(ctx, extractor.FileInput{
			Path:     "core/models.py",
			Language: "python",
			Content:  []byte("class UserMigration(Migration):\n    pass\n"),
		})
		if err != nil {
			t.Fatalf("Detect: %v", err)
		}

		for _, e := range result.Entities {
			if e.Properties["pattern_type"] == "yaml_driven" {
				if v, ok := e.Properties["file_convention"]; ok {
					t.Errorf("unexpected file_convention=%q on entity from non-matching file: %+v", v, e)
				}
			}
		}
	})
}

// TestDetector_Bridge_MultipleConventions_CommaJoined verifies that when a file
// matches multiple file_convention globs, the file_convention property on source-
// pattern entities is a comma-joined string of all matched globs.
func TestDetector_Bridge_MultipleConventions_CommaJoined(t *testing.T) {
	fsys := fstest.MapFS{
		"rules/python/frameworks/test_bridge_multi.yaml": &fstest.MapFile{
			Data: []byte(bridgeTwoConventionsYAML),
		},
	}
	rules, err := LoadAllRulesFromFS(fsys, "rules")
	if err != nil {
		t.Fatalf("LoadAllRulesFromFS: %v", err)
	}

	det := New(rules)
	ctx := context.Background()

	result, err := det.Detect(ctx, extractor.FileInput{
		Path:     "app/migrations/0001_initial.py",
		Language: "python",
		Content:  []byte("class InitialMigration(Migration):\n    pass\n"),
	})
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}

	for _, e := range result.Entities {
		if e.Kind == "Migration" && e.Properties["pattern_type"] == "yaml_driven" {
			got := e.Properties["file_convention"]
			// Both globs match; expect comma-joined in definition order.
			want := "*/migrations/0*.py,*/migrations/*.py"
			if got != want {
				t.Errorf("file_convention (multi): want %q, got %q", want, got)
			}
			return
		}
	}
	t.Fatalf("no yaml_driven Migration entity found; entities: %+v", result.Entities)
}

// ---------------------------------------------------------------------------
// Integration: django.yaml embedded rules load and contain Migration convention
// ---------------------------------------------------------------------------

// TestDjangoYAML_HasMigrationConvention verifies that the embedded
// django.yaml actually contains the Migration file_convention with the
// correct glob/entity_type/name_from — confirming the YAML fields are
// wired end-to-end from disk through to the struct.
func TestDjangoYAML_HasMigrationConvention(t *testing.T) {
	rules, err := LoadAllRules()
	if err != nil {
		t.Fatalf("LoadAllRules: %v", err)
	}

	pythonRules, ok := rules["python"]
	if !ok {
		t.Fatal("no python rules loaded")
	}

	found := false
	for _, fr := range pythonRules {
		for _, fc := range fr.FileConventions {
			if fc.Glob == "*/migrations/0*.py" && fc.EntityType == "Migration" && fc.NameFrom == "filename" {
				found = true
				break
			}
		}
	}
	if !found {
		t.Error("django.yaml: */migrations/0*.py Migration file_convention not found in loaded rules")
	}
}
