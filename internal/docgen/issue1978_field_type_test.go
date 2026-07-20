package docgen_test

// issue1978_field_type_test.go — regression tests for #1978: field_type and
// kwargs must be populated in class_manifest.fields[] entries when the child
// SCOPE.Schema field entity carries Properties["field_type"] and
// Properties["kwarg.*"] stamped by django_relational.go.
//
// Verify on a Django-like Model with:
//   - a CharField with kwarg.max_length
//   - a ForeignKey with kwarg.to + kwarg.on_delete
//   - a plain field (no Properties) that should still work (empty FieldType)

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/daemon"
	"github.com/cajasmota/grafel/internal/docgen"
	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/registry"
)

// issue1978Harness sets up an isolated test environment with a Django-like
// Model class entity that has SCOPE.Schema/field children carrying
// Properties["field_type"] and Properties["kwarg.*"].
func issue1978Harness(t *testing.T) (groupName, classEntityID string) {
	t.Helper()

	tmp := t.TempDir()
	homeDir := filepath.Join(tmp, "home")
	xdgDir := filepath.Join(tmp, "xdg")
	daemonRoot := filepath.Join(tmp, "daemon")
	repoPath := filepath.Join(tmp, "repo")

	for _, d := range []string{homeDir, xdgDir, daemonRoot, repoPath} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", d, err)
		}
	}

	t.Setenv("GRAFEL_HOME", homeDir)
	t.Setenv("XDG_CONFIG_HOME", xdgDir)
	t.Setenv(daemon.EnvRoot, daemonRoot)

	groupName = "issue-1978-field-type-test"

	cfgPath, err := registry.ConfigPathFor(groupName)
	if err != nil {
		t.Fatalf("ConfigPathFor: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o755); err != nil {
		t.Fatalf("mkdir fleet config dir: %v", err)
	}
	fleetJSON, _ := json.Marshal(map[string]interface{}{
		"name":  groupName,
		"repos": []map[string]interface{}{{"path": repoPath, "slug": "repo"}},
	})
	if err := os.WriteFile(cfgPath, fleetJSON, 0o644); err != nil {
		t.Fatalf("write fleet config: %v", err)
	}

	// Entities.
	modelID := graph.EntityID("repo", "SCOPE.Model", "Building", "models/building.py")
	charFieldID := graph.EntityID("repo", "SCOPE.Schema", "Building.name", "models/building.py")
	fkFieldID := graph.EntityID("repo", "SCOPE.Schema", "Building.client", "models/building.py")
	plainFieldID := graph.EntityID("repo", "SCOPE.Schema", "Building.active", "models/building.py")

	stateDir := daemon.StateDirForRepo(repoPath)
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatalf("mkdir state dir: %v", err)
	}

	doc := graph.Document{
		Version:        1,
		GeneratedAt:    time.Now().UTC(),
		Repo:           repoPath,
		IndexerVersion: "test",
		Entities: []graph.Entity{
			{
				ID:         modelID,
				Name:       "Building",
				Kind:       "SCOPE.Model",
				Subtype:    "model",
				SourceFile: "models/building.py",
				StartLine:  1,
				Language:   "python",
				Signature:  "class Building",
			},
			graph.Entity{
				ID:         charFieldID,
				Name:       "Building.name",
				Kind:       "SCOPE.Schema",
				Subtype:    "field",
				SourceFile: "models/building.py",
				StartLine:  5,
				Language:   "python",
			}.WithProperties(map[string]string{
				"field_type":       "CharField",
				"kwarg.max_length": "200",
			},
			),
			graph.Entity{
				ID:         fkFieldID,
				Name:       "Building.client",
				Kind:       "SCOPE.Schema",
				Subtype:    "field",
				SourceFile: "models/building.py",
				StartLine:  6,
				Language:   "python",
			}.WithProperties(map[string]string{
				"field_type":      "ForeignKey",
				"kwarg.to":        "Client",
				"kwarg.on_delete": "CASCADE",
			},
			),
			{
				ID:         plainFieldID,
				Name:       "Building.active",
				Kind:       "SCOPE.Schema",
				Subtype:    "field",
				SourceFile: "models/building.py",
				StartLine:  7,
				Language:   "python",
				Signature:  "active: bool",
				// No field_type / kwargs properties.
			},
		},
		Relationships: []graph.Relationship{
			{
				ID:     graph.RelationshipID(modelID, charFieldID, "CONTAINS"),
				FromID: modelID,
				ToID:   charFieldID,
				Kind:   "CONTAINS",
			},
			{
				ID:     graph.RelationshipID(modelID, fkFieldID, "CONTAINS"),
				FromID: modelID,
				ToID:   fkFieldID,
				Kind:   "CONTAINS",
			},
			{
				ID:     graph.RelationshipID(modelID, plainFieldID, "CONTAINS"),
				FromID: modelID,
				ToID:   plainFieldID,
				Kind:   "CONTAINS",
			},
		},
	}
	docJSON, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal doc: %v", err)
	}
	if err := os.WriteFile(filepath.Join(stateDir, "graph.json"), docJSON, 0o644); err != nil {
		t.Fatalf("write graph.json: %v", err)
	}

	return groupName, modelID
}

// TestBuildBundle_ClassManifest_FieldType verifies that class_manifest.fields
// entries carry field_type when Properties["field_type"] is set on the child
// SCOPE.Schema entity (#1978).
func TestBuildBundle_ClassManifest_FieldType(t *testing.T) {
	groupName, classID := issue1978Harness(t)

	opts := docgen.BuildBundleOpts{
		RunOpts: docgen.RunOpts{
			Group:        groupName,
			SeedEntityID: classID,
			Section:      "overview",
			NoCache:      true,
		},
		Tier:    0,
		NoCache: true,
	}

	bundle, err := docgen.BuildBundle(context.Background(), opts)
	if err != nil {
		t.Fatalf("BuildBundle: %v", err)
	}
	m := bundle.GraphContext.ClassManifest
	if m == nil {
		t.Fatal("class_manifest is nil — expected non-nil for model-like seed")
	}
	if len(m.Fields) != 3 {
		t.Fatalf("expected 3 fields, got %d: %+v", len(m.Fields), m.Fields)
	}

	// Find field entries by name.
	byName := make(map[string]docgen.ClassFieldEntry, len(m.Fields))
	for _, f := range m.Fields {
		byName[f.Name] = f
	}

	// CharField: field_type must be "CharField", kwargs must have max_length.
	nameField, ok := byName["name"]
	if !ok {
		t.Fatal("field 'name' not found in ClassManifest.Fields")
	}
	if nameField.FieldType != "CharField" {
		t.Errorf("field 'name' FieldType: got %q, want %q", nameField.FieldType, "CharField")
	}
	if nameField.Kwargs == nil {
		t.Error("field 'name' Kwargs is nil — expected map with max_length")
	} else if nameField.Kwargs["max_length"] != "200" {
		t.Errorf("field 'name' Kwargs[max_length]: got %q, want %q", nameField.Kwargs["max_length"], "200")
	}

	// ForeignKey: field_type must be "ForeignKey", kwargs must have to + on_delete.
	clientField, ok := byName["client"]
	if !ok {
		t.Fatal("field 'client' not found in ClassManifest.Fields")
	}
	if clientField.FieldType != "ForeignKey" {
		t.Errorf("field 'client' FieldType: got %q, want %q", clientField.FieldType, "ForeignKey")
	}
	if clientField.Kwargs == nil {
		t.Error("field 'client' Kwargs is nil — expected map with to + on_delete")
	} else {
		if clientField.Kwargs["to"] != "Client" {
			t.Errorf("field 'client' Kwargs[to]: got %q, want %q", clientField.Kwargs["to"], "Client")
		}
		if clientField.Kwargs["on_delete"] != "CASCADE" {
			t.Errorf("field 'client' Kwargs[on_delete]: got %q, want %q", clientField.Kwargs["on_delete"], "CASCADE")
		}
	}

	// Plain field (no field_type): FieldType must be empty, Kwargs must be nil.
	activeField, ok := byName["active"]
	if !ok {
		t.Fatal("field 'active' not found in ClassManifest.Fields")
	}
	if activeField.FieldType != "" {
		t.Errorf("field 'active' FieldType: got %q, want empty string (no field_type property)", activeField.FieldType)
	}
	if activeField.Kwargs != nil {
		t.Errorf("field 'active' Kwargs: got %v, want nil (no kwarg.* properties)", activeField.Kwargs)
	}
}

// TestBuildBundle_ClassManifest_Kwargs_NilWhenAbsent verifies that
// ClassFieldEntry.Kwargs is nil (omitted from JSON) when the child entity has
// no kwarg.* properties. Ensures omitempty contract is upheld (#1978).
func TestBuildBundle_ClassManifest_Kwargs_NilWhenAbsent(t *testing.T) {
	groupName, classID := issue1978Harness(t)

	opts := docgen.BuildBundleOpts{
		RunOpts: docgen.RunOpts{
			Group:        groupName,
			SeedEntityID: classID,
			Section:      "overview",
			NoCache:      true,
		},
		Tier:    0,
		NoCache: true,
	}

	bundle, err := docgen.BuildBundle(context.Background(), opts)
	if err != nil {
		t.Fatalf("BuildBundle: %v", err)
	}
	m := bundle.GraphContext.ClassManifest
	if m == nil {
		t.Fatal("class_manifest is nil")
	}

	for _, f := range m.Fields {
		if f.Name == "active" {
			if f.Kwargs != nil {
				t.Errorf("ClassFieldEntry[active].Kwargs is non-nil (%v) for a field with no kwarg.* properties — omitempty contract broken", f.Kwargs)
			}
			return
		}
	}
	t.Error("field 'active' not found in ClassManifest.Fields")
}
