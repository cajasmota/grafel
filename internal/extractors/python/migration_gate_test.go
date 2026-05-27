// Package python — unit tests for issue #2548: gate Django migration entity
// emission behind ARCHIGRAPH_EMIT_MIGRATION_ENTITIES.
//
// Three invariants are tested:
//
//  1. Default-off: Django migration files (.py files in migrations/ directories)
//     emit zero Migration entities by default.
//  2. Opt-in (ARCHIGRAPH_EMIT_MIGRATION_ENTITIES=1): Migration entities ARE emitted.
//  3. Non-migration files are unaffected by the flag.
package python

import (
	"context"
	"testing"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
	sitter "github.com/smacker/go-tree-sitter"
	tspython "github.com/smacker/go-tree-sitter/python"
)

// parsePython parses Python source using tree-sitter and returns the parse tree.
func parsePython(t *testing.T, src []byte) *sitter.Tree {
	t.Helper()
	parser := sitter.NewParser()
	defer parser.Close()
	parser.SetLanguage(tspython.GetLanguage())
	tree, err := parser.ParseCtx(context.Background(), nil, src)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if tree == nil {
		t.Fatal("parse returned nil tree")
	}
	return tree
}

// extractPythonWithPath extracts entities from Python source with the given file path.
func extractPythonWithPath(t *testing.T, src []byte, path string, tree *sitter.Tree) []types.EntityRecord {
	t.Helper()
	ext := &Extractor{}
	file := extractor.FileInput{
		Path:     path,
		Language: "python",
		Content:  src,
		Tree:     tree,
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
	t.Setenv("ARCHIGRAPH_EMIT_MIGRATION_ENTITIES", "")

	src := []byte(migrationFileSrc)
	tree := parsePython(t, src)
	entities := extractPythonWithPath(t, src, "core/migrations/0001_initial.py", tree)

	// Strip file-level infrastructure entities.
	semanticEntities := stripFileEntity(entities)

	if len(semanticEntities) > 0 {
		t.Errorf("default-off: migration file emitted %d semantic entities, expected 0; env var ARCHIGRAPH_EMIT_MIGRATION_ENTITIES must be set to emit",
			len(semanticEntities))
		for _, e := range semanticEntities {
			t.Logf("  - %s (%s/%s)", e.Name, e.Kind, e.Subtype)
		}
	}
}

// ---------------------------------------------------------------------------
// 2. Opt-in: ARCHIGRAPH_EMIT_MIGRATION_ENTITIES=1 emits Migration entities
// ---------------------------------------------------------------------------

// TestPythonExtractor_EmitsMigrationsOptIn verifies that with
// ARCHIGRAPH_EMIT_MIGRATION_ENTITIES=1, Migration entities ARE emitted.
func TestPythonExtractor_EmitsMigrationsOptIn(t *testing.T) {
	t.Setenv("ARCHIGRAPH_EMIT_MIGRATION_ENTITIES", "1")

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
// ARCHIGRAPH_EMIT_MIGRATION_ENTITIES=true also works (truthy variant).
func TestPythonExtractor_EmitsMigrationsOptInTrue(t *testing.T) {
	t.Setenv("ARCHIGRAPH_EMIT_MIGRATION_ENTITIES", "true")

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
	t.Setenv("ARCHIGRAPH_EMIT_MIGRATION_ENTITIES", "")

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
