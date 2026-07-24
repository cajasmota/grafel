package resolve

import (
	"testing"

	"github.com/cajasmota/grafel/internal/types"
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
					Properties: types.Props{
						{K: "imported_name", V: "views"},
						{K: "language", V: "python"},
						{K: "local_name", V: "views"},
						{K: "source_module", V: "users"},
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

// TestResolvePythonModuleImport_InitReExportsModuleBinding reproduces #1991.
// `from .celery import app` inside acme_core/__init__.py is normalised
// (post-#2026 relative-import resolution) to ToID="acme_core.celery.app".
// `app` is a module-level *binding* (`app = Celery(...)`) rather than a
// top-level function/class entity, so the (module, leaf) lookup returns
// nothing. The whole-path probe also misses because moduleFileEntity
// indexes the module path "acme_core.celery", not the ".app" tail.
// Without #1991 the edge then falls through to external-synthesis and
// produces an unresolved EXTERNAL synthetic — breaking the re-export chain.
// After the fix the resolver strips the leaf and binds the IMPORTS edge to
// the celery module's file entity ID, keeping the re-export chain in-graph.
func TestResolvePythonModuleImport_InitReExportsModuleBinding(t *testing.T) {
	records := []types.EntityRecord{
		// Target module: acme_core/celery.py.
		{
			ID:         "celery-module-id",
			Name:       "acme_core/celery.py",
			SourceFile: "acme_core/celery.py",
			Kind:       "SCOPE.Component",
			Language:   "python",
		},
		// The `__init__.py` carrier with the re-export IMPORTS edge.
		{
			ID:         "init-id",
			Name:       "acme_core/__init__.py",
			SourceFile: "acme_core/__init__.py",
			Kind:       "SCOPE.Component",
			Language:   "python",
			Relationships: []types.RelationshipRecord{
				{
					Kind:   "IMPORTS",
					ToID:   "acme_core.celery.app",
					FromID: "init-id",
					Properties: types.Props{
						{K: "imported_name", V: "app"},
						{K: "language", V: "python"},
						{K: "local_name", V: "app"},
						{K: "source_module", V: "acme_core.celery"},
					},
				},
			},
		},
	}
	tbl := BuildImportTable(records)
	ResolveImports(records, tbl)
	for i := range records {
		e := &records[i]
		for j := range e.Relationships {
			r := &e.Relationships[j]
			if r.Kind != "IMPORTS" {
				continue
			}
			if r.ToID != "celery-module-id" {
				t.Errorf("expected IMPORTS edge rewritten to celery module entity, got ToID=%q", r.ToID)
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
					Properties: types.PropsFromMap(map[string]string{
						"source_module": "users",
						"imported_name": "views",
						"local_name":    "views",
						// no "language" property → should not trigger Python path
					}),
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
