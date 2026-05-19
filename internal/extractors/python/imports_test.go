// imports_test.go — coverage for the IMPORTS ToID resolveImportToIDs
// pass (analog of #642 for Python).

package python

import (
	"context"
	"strings"
	"testing"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

// findImportEdge returns the IMPORTS edge whose source_module matches
// the supplied dotted module path, or nil when no such edge exists.
func findImportEdge(ents []types.EntityRecord, sourceModule string) *types.RelationshipRecord {
	for i := range ents {
		e := &ents[i]
		if e.Kind != "SCOPE.Component" || e.Subtype != "module" {
			continue
		}
		for j := range e.Relationships {
			r := &e.Relationships[j]
			if r.Kind != "IMPORTS" {
				continue
			}
			if r.Properties != nil && r.Properties["source_module"] == sourceModule {
				return r
			}
		}
	}
	return nil
}

// Known external root package: `from django.db import models` →
// ToID="ext:django:models". The resolver's IsKnownExternalPackage
// allowlist will then classify this as ExternalKnown directly.
func TestImportsRewriteKnownExternal(t *testing.T) {
	ex := &Extractor{}
	ents, err := ex.Extract(context.Background(), extractor.FileInput{
		Path:     "demo.py",
		Language: "python",
		Content:  []byte("from django.db import models\nimport requests\n"),
	})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	r := findImportEdge(ents, "django.db")
	if r == nil {
		t.Fatalf("missing IMPORTS edge for django.db")
	}
	if !strings.HasPrefix(r.ToID, "ext:django") {
		t.Fatalf("django.db import ToID = %q, want prefix ext:django", r.ToID)
	}
	r2 := findImportEdge(ents, "requests")
	if r2 == nil {
		t.Fatalf("missing IMPORTS edge for requests")
	}
	if r2.ToID != "ext:requests" {
		t.Fatalf("requests import ToID = %q, want ext:requests", r2.ToID)
	}
}

// Unknown external / in-tree imports are left untouched: the resolver's
// downstream ResolveDottedImportTarget path needs the original dotted
// shape to bind in-tree modules.
func TestImportsLeavesUnknownAlone(t *testing.T) {
	ex := &Extractor{}
	ents, err := ex.Extract(context.Background(), extractor.FileInput{
		Path:     "demo.py",
		Language: "python",
		Content:  []byte("from myapp.users import models\n"),
	})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	r := findImportEdge(ents, "myapp.users")
	if r == nil {
		t.Fatalf("missing IMPORTS edge for myapp.users")
	}
	if strings.HasPrefix(r.ToID, "ext:") {
		t.Fatalf("myapp.users import ToID = %q, must not be ext: form", r.ToID)
	}
}

// Relative imports are never rewritten — `from .foo import bar` carries
// a source_module starting with "." which is never an external package.
func TestImportsSkipsRelative(t *testing.T) {
	ex := &Extractor{}
	ents, err := ex.Extract(context.Background(), extractor.FileInput{
		Path:     "demo.py",
		Language: "python",
		Content:  []byte("from .helpers import shape\n"),
	})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	for _, e := range ents {
		if e.Kind != "SCOPE.Component" || e.Subtype != "module" {
			continue
		}
		for _, r := range e.Relationships {
			if r.Kind != "IMPORTS" {
				continue
			}
			if strings.HasPrefix(r.ToID, "ext:") {
				t.Fatalf("relative import got ext: ToID = %q", r.ToID)
			}
		}
	}
}
