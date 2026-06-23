// Package python — unit tests for issue #2548: gate Django migration entity
// emission behind GRAFEL_EMIT_MIGRATION_ENTITIES.
//
// Three invariants are tested:
//
//  1. Default-off: Django migration files (.py files in migrations/ directories)
//     emit zero Migration entities by default.
//  2. Opt-in (GRAFEL_EMIT_MIGRATION_ENTITIES=1): Migration entities ARE emitted.
//  3. Non-migration files are unaffected by the flag.
package python

import (
	"context"
	"testing"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/treesitter/ts"
	tssmacker "github.com/cajasmota/grafel/internal/treesitter/ts/smacker"
	"github.com/cajasmota/grafel/internal/types"
	tspython "github.com/smacker/go-tree-sitter/python"
)

// parsePython parses Python source into a ts.Tree via the smacker adapter (B2 #5418).
func parsePython(t *testing.T, src []byte) ts.Tree {
	t.Helper()
	parser, err := tssmacker.New().NewParser(tssmacker.WrapLanguage(tspython.GetLanguage()))
	if err != nil {
		t.Fatalf("parser init: %v", err)
	}
	defer parser.Close()
	tree, err := parser.Parse(src)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if tree == nil {
		t.Fatal("parse returned nil tree")
	}
	return tree
}

// extractPythonWithPath extracts entities from Python source with the given file path.
func extractPythonWithPath(t *testing.T, src []byte, path string, tree ts.Tree) []types.EntityRecord {
	t.Helper()
	ext := &Extractor{}
	file := extractor.FileInput{
		Path:     path,
		Language: "python",
		Content:  src,
		TSTree:   tree,
		RepoRoot: "/test",
	}
	entities, err := ext.Extract(context.Background(), file)
	if err != nil {
		t.Fatalf("extract error: %v", err)
	}
	return entities
}

// stripFileEntity filters out infrastructure-only entities (file-level
// SCOPE.Component and package-level Module entities) so tests can focus
// on semantic entities.
func stripFileEntity(entities []types.EntityRecord) []types.EntityRecord {
	out := entities[:0:0]
	for _, e := range entities {
		switch {
		case e.Kind == "SCOPE.Component" && e.Subtype == "file":
			continue
		case e.Kind == string(types.EntityKindModule) && e.Subtype == "package":
			continue
		}
		out = append(out, e)
	}
	return out
}

// migrationFileSrc is a minimal Django migration file (0001_initial.py).
const migrationFileSrc = `
from django.db import migrations, models

class Migration(migrations.Migration):
    initial = True
    dependencies = []
    operations = [
        migrations.CreateModel(
            name='User',
            fields=[
                ('id', models.AutoField(primary_key=True)),
                ('name', models.CharField(max_length=100)),
            ],
        ),
    ]
`

// nonMigrationFileSrc is a regular Python file that should not be affected.
const nonMigrationFileSrc = `
from django.db import models

class User(models.Model):
    name = models.CharField(max_length=100)

    class Meta:
        app_label = 'users'
`

// ---------------------------------------------------------------------------
// 1. Default-off: Django migration files emit zero Migration entities
// ---------------------------------------------------------------------------

// TestPythonExtractor_PrunesMigrationFiles verifies that by default,
// Django migration files in migrations/ directories emit no entities (beyond
// infrastructure entities like the file-level SCOPE.Component).
func TestPythonExtractor_PrunesMigrationFiles(t *testing.T) {
	t.Setenv("GRAFEL_EMIT_MIGRATION_ENTITIES", "")

	src := []byte(migrationFileSrc)
	tree := parsePython(t, src)
	entities := extractPythonWithPath(t, src, "core/migrations/0001_initial.py", tree)

	// Strip file-level infrastructure entities.
	semanticEntities := stripFileEntity(entities)

	if len(semanticEntities) > 0 {
		t.Errorf("default-off: migration file emitted %d semantic entities, expected 0; env var GRAFEL_EMIT_MIGRATION_ENTITIES must be set to emit",
			len(semanticEntities))
		for _, e := range semanticEntities {
			t.Logf("  - %s (%s/%s)", e.Name, e.Kind, e.Subtype)
		}
	}
}

// ---------------------------------------------------------------------------
// 2. Opt-in: GRAFEL_EMIT_MIGRATION_ENTITIES=1 emits Migration entities
// ---------------------------------------------------------------------------

// TestPythonExtractor_EmitsMigrationsOptIn verifies that with
// GRAFEL_EMIT_MIGRATION_ENTITIES=1, Migration entities ARE emitted.
func TestPythonExtractor_EmitsMigrationsOptIn(t *testing.T) {
	t.Setenv("GRAFEL_EMIT_MIGRATION_ENTITIES", "1")

	src := []byte(migrationFileSrc)
	tree := parsePython(t, src)
	entities := extractPythonWithPath(t, src, "core/migrations/0001_initial.py", tree)

	semanticEntities := stripFileEntity(entities)

	if len(semanticEntities) == 0 {
		t.Errorf("opt-in: migration file emitted 0 semantic entities, expected at least 1 (Migration)")
	}

	hasMigration := false
	for _, e := range semanticEntities {
		if e.Subtype == "django" {
			hasMigration = true
			break
		}
	}
	if !hasMigration {
		t.Errorf("opt-in: no Migration entity found; entities: %v", semanticEntities)
	}
}

// TestPythonExtractor_EmitsMigrationsOptInTrue verifies that
// GRAFEL_EMIT_MIGRATION_ENTITIES=true also works (truthy variant).
func TestPythonExtractor_EmitsMigrationsOptInTrue(t *testing.T) {
	t.Setenv("GRAFEL_EMIT_MIGRATION_ENTITIES", "true")

	src := []byte(migrationFileSrc)
	tree := parsePython(t, src)
	entities := extractPythonWithPath(t, src, "core/migrations/0001_initial.py", tree)

	semanticEntities := stripFileEntity(entities)

	if len(semanticEntities) == 0 {
		t.Errorf("opt-in (true): migration file emitted 0 semantic entities, expected at least 1 (Migration)")
	}
}

// ---------------------------------------------------------------------------
// 3. Non-migration files unaffected
// ---------------------------------------------------------------------------

// TestPythonExtractor_NonMigrationUnaffected verifies that non-migration files
// continue to extract entities regardless of the migration flag.
func TestPythonExtractor_NonMigrationUnaffected(t *testing.T) {
	t.Setenv("GRAFEL_EMIT_MIGRATION_ENTITIES", "")

	src := []byte(nonMigrationFileSrc)
	tree := parsePython(t, src)
	entities := extractPythonWithPath(t, src, "core/models.py", tree)

	semanticEntities := stripFileEntity(entities)

	if len(semanticEntities) == 0 {
		t.Errorf("non-migration file in core/models.py emitted 0 entities, expected User class")
	}

	hasUserClass := false
	for _, e := range semanticEntities {
		if e.Name == "User" && e.Kind == "SCOPE.Component" {
			hasUserClass = true
			break
		}
	}
	if !hasUserClass {
		t.Errorf("non-migration file did not emit User class entity")
	}
}

// ---------------------------------------------------------------------------
// Issue #2587: Verify Migration kind is correctly set (not SCOPE.Component)
// ---------------------------------------------------------------------------

// TestPythonExtractor_MigrationEntitiesHaveCorrectKind verifies that when
// GRAFEL_EMIT_MIGRATION_ENTITIES=1, emitted entities have kind="Migration"
// (not "SCOPE.Component"), so that kind-based filters work correctly.
func TestPythonExtractor_MigrationEntitiesHaveCorrectKind(t *testing.T) {
	t.Setenv("GRAFEL_EMIT_MIGRATION_ENTITIES", "1")

	src := []byte(migrationFileSrc)
	tree := parsePython(t, src)
	entities := extractPythonWithPath(t, src, "core/migrations/0001_initial.py", tree)

	semanticEntities := stripFileEntity(entities)

	if len(semanticEntities) == 0 {
		t.Fatal("no semantic entities emitted with GRAFEL_EMIT_MIGRATION_ENTITIES=1")
	}

	// Verify exactly one entity with kind="Migration"
	migrationCount := 0
	for _, e := range semanticEntities {
		if e.Kind == "Migration" {
			migrationCount++
			if e.Subtype != "django" {
				t.Errorf("Migration entity has subtype=%q, want 'django'", e.Subtype)
			}
		} else {
			t.Errorf("migration file emitted entity with kind=%q (subtype=%q), want kind='Migration'",
				e.Kind, e.Subtype)
		}
	}

	if migrationCount != 1 {
		t.Errorf("expected exactly 1 Migration entity, got %d", migrationCount)
	}
}

// TestPythonExtractor_MigrationFileEmitZeroByDefault verifies that across
// multiple migration files, zero Migration class entities escape when the
// emission flag is off.
func TestPythonExtractor_MigrationFileEmitZeroByDefault(t *testing.T) {
	t.Setenv("GRAFEL_EMIT_MIGRATION_ENTITIES", "")

	// Multiple migration files
	migrations := []struct {
		path string
		src  string
	}{
		{
			path: "core/migrations/0001_initial.py",
			src: `from django.db import migrations, models
class Migration(migrations.Migration):
    dependencies = []
    operations = [migrations.CreateModel(name='User')]`,
		},
		{
			path: "core/migrations/0002_add_field.py",
			src: `from django.db import migrations
class Migration(migrations.Migration):
    dependencies = [('core', '0001_initial')]
    operations = [migrations.AddField(model_name='User', name='email')]`,
		},
	}

	totalSemanticEntities := 0
	for _, mig := range migrations {
		tree := parsePython(t, []byte(mig.src))
		entities := extractPythonWithPath(t, []byte(mig.src), mig.path, tree)
		semanticEntities := stripFileEntity(entities)
		totalSemanticEntities += len(semanticEntities)

		// Each migration file should emit zero semantic entities when flag is off
		if len(semanticEntities) > 0 {
			t.Errorf("%s: emitted %d semantic entities (expected 0); migration pruning failed",
				mig.path, len(semanticEntities))
			for _, e := range semanticEntities {
				t.Logf("  - %s (%s/%s)", e.Name, e.Kind, e.Subtype)
			}
		}
	}

	if totalSemanticEntities > 0 {
		t.Errorf("total semantic entities from 2 migration files: %d (expected 0)",
			totalSemanticEntities)
	}
}
