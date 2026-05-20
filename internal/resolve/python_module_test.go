package resolve

import (
	"testing"

	"github.com/cajasmota/archigraph/internal/types"
)

func TestResolvePythonModuleImport_Basic(t *testing.T) {
	records := []types.EntityRecord{
		{
			ID:         "file-views-id",
			Name:       "users/views.py",
			SourceFile: "users/views.py",
			Kind:       "SCOPE.Component",
			Language:   "python",
		},
	}
	tbl := BuildImportTable(records)
	id, ok := tbl.ResolvePythonModuleImport("users.views")
	if !ok {
		t.Fatal("expected ResolvePythonModuleImport to resolve 'users.views', got false")
	}
	if id != "file-views-id" {
		t.Fatalf("expected id='file-views-id', got %q", id)
	}
}

func TestResolvePythonModuleImport_ResolveImports(t *testing.T) {
	records := []types.EntityRecord{
		{
			ID:         "file-views-id",
			Name:       "users/views.py",
			SourceFile: "users/views.py",
			Kind:       "SCOPE.Component",
			Language:   "python",
		},
		{
			ID:         "file-urls-id",
			Name:       "users/urls.py",
			SourceFile: "users/urls.py",
			Kind:       "SCOPE.Component",
			Language:   "python",
			Relationships: []types.RelationshipRecord{
				{
					Kind:   "IMPORTS",
					ToID:   "users.views",
					FromID: "file-urls-id",
					Properties: map[string]string{
						"source_module": "users",
						"imported_name": "views",
						"local_name":    "views",
						"language":      "python",
					},
				},
			},
		},
	}
	tbl := BuildImportTable(records)
	stats := ResolveImports(records, tbl)
	// Should have rewritten at least one IMPORTS edge.
	if stats.ImportsRewritten == 0 {
		t.Error("expected at least one IMPORTS edge rewritten, got 0")
	}
	// Verify the actual edge is now the hex ID of the views.py file entity.
	for i := range records {
		e := &records[i]
		for j := range e.Relationships {
			r := &e.Relationships[j]
			if r.Kind == "IMPORTS" {
				if r.ToID != "file-views-id" {
					t.Errorf("expected to_id='file-views-id' after rewrite, got %q", r.ToID)
				}
			}
		}
	}
}

// TestResolvePythonModuleImport_OnlyPython verifies that a non-Python IMPORTS
// edge with the same dotted ToID is NOT rewritten by the Python module path
// (the language gate prevents it).
func TestResolvePythonModuleImport_OnlyPython(t *testing.T) {
	records := []types.EntityRecord{
		{
			ID:         "file-views-id",
			Name:       "users/views.py",
			SourceFile: "users/views.py",
			Kind:       "SCOPE.Component",
			Language:   "python",
		},
		{
			ID:         "file-caller-id",
			Name:       "callers/foo.go",
			SourceFile: "callers/foo.go",
			Kind:       "SCOPE.Component",
			Language:   "go",
			Relationships: []types.RelationshipRecord{
				{
					Kind:   "IMPORTS",
					ToID:   "users.views",
					FromID: "file-caller-id",
					Properties: map[string]string{
						"source_module": "users",
						"imported_name": "views",
						"local_name":    "views",
						// no "language" property → should not trigger Python path
					},
				},
			},
		},
	}
	tbl := BuildImportTable(records)
	ResolveImports(records, tbl)
	// The Go IMPORTS edge should NOT have been rewritten by the Python path.
	for i := range records {
		e := &records[i]
		if e.Language != "go" {
			continue
		}
		for j := range e.Relationships {
			r := &e.Relationships[j]
			if r.Kind == "IMPORTS" {
				if r.ToID != "users.views" {
					t.Errorf("go IMPORTS edge should not be rewritten by Python path, got %q", r.ToID)
				}
			}
		}
	}
}
