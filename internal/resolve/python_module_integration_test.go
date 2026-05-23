package resolve

import (
	"context"
	"os"
	"testing"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/extractors"
	pyextr "github.com/cajasmota/archigraph/internal/extractors/python"
	"github.com/cajasmota/archigraph/internal/graph"
	"github.com/cajasmota/archigraph/internal/types"
)

// stampTestIDs simulates the indexer's stampEntityIDs pass so BuildImportTable
// Pass 2 (which skips entities with empty ID) can index file-level SCOPE.Component
// entities into moduleFileEntity.
func stampTestIDs(records []types.EntityRecord) {
	for k := range records {
		r := &records[k]
		if r.Name == "" {
			continue
		}
		r.ID = graph.EntityID("test-repo", r.Kind, r.Name, r.SourceFile)
	}
}

func TestPythonModuleImport_DjangoIntegration(t *testing.T) {
	fixtureDir := "../../internal/quality/golden/python-django-mini/src"

	relFiles := []string{
		"users/urls.py",
		"users/views.py",
		"users/apps.py",
		"users/signals.py",
		"users/models.py",
		"users/admin.py",
		"myproject/urls.py",
		"myproject/settings.py",
		"manage.py",
	}

	// Check fixture exists.
	if _, err := os.Stat(fixtureDir + "/users/urls.py"); err != nil {
		t.Skip("fixture not found: " + fixtureDir)
	}

	pyextr.ClearPythonClassRegistry()
	for _, rel := range relFiles {
		abs := fixtureDir + "/" + rel
		if content, err := os.ReadFile(abs); err == nil {
			pyextr.ScanPythonClassRegistry(rel, string(content))
		}
	}

	var records []types.EntityRecord
	for _, rel := range relFiles {
		abs := fixtureDir + "/" + rel
		content, err := os.ReadFile(abs)
		if err != nil {
			continue
		}
		file := extractor.FileInput{
			Path:     rel,
			Content:  content,
			Language: "python",
		}
		ents, err := extractors.Extract(context.Background(), file)
		if err != nil {
			t.Logf("extract error for %s: %v", rel, err)
			continue
		}
		records = append(records, ents...)
	}

	t.Logf("Total entity records: %d", len(records))

	// Simulate the indexer's stampEntityIDs pass — required so BuildImportTable
	// Pass 2's `e.ID == ""` guard doesn't skip file-level SCOPE.Component entities.
	stampTestIDs(records)

	// Find IMPORTS edges before ResolveImports.
	moduleImports := 0
	for i := range records {
		e := &records[i]
		for j := range e.Relationships {
			r := &e.Relationships[j]
			if r.Kind == "IMPORTS" && (r.ToID == "users.views" || r.ToID == "users.signals") {
				t.Logf("BEFORE: entity=%q (kind=%s subtype=%s lang=%s) has IMPORTS to=%q props=%v",
					e.Name, e.Kind, e.Subtype, e.Language, r.ToID, r.Properties)
				moduleImports++
			}
		}
	}
	t.Logf("Found %d module IMPORTS edges before resolution", moduleImports)
	if moduleImports == 0 {
		t.Error("expected at least 1 'users.views' or 'users.signals' IMPORTS edge before resolution")
	}

	tbl := BuildImportTable(records)
	viewsID := tbl.moduleFileEntity["users.views"]
	signalsID := tbl.moduleFileEntity["users.signals"]
	t.Logf("moduleFileEntity['users.views'] = %q", viewsID)
	t.Logf("moduleFileEntity['users.signals'] = %q", signalsID)
	if viewsID == "" {
		t.Error("expected moduleFileEntity to have entry for 'users.views'")
	}
	if signalsID == "" {
		t.Error("expected moduleFileEntity to have entry for 'users.signals'")
	}

	stats := ResolveImports(records, tbl)
	t.Logf("ImportsConsidered=%d ImportsRewritten=%d", stats.ImportsConsidered, stats.ImportsRewritten)

	// Verify no module IMPORTS edges remain.
	remaining := 0
	for i := range records {
		e := &records[i]
		for j := range e.Relationships {
			r := &e.Relationships[j]
			if r.Kind == "IMPORTS" && (r.ToID == "users.views" || r.ToID == "users.signals") {
				t.Logf("AFTER (still unresolved): entity=%q IMPORTS to=%q", e.Name, r.ToID)
				remaining++
			}
		}
	}
	if remaining > 0 {
		t.Errorf("expected 0 unresolved module IMPORTS edges after resolution, got %d", remaining)
	}
	if stats.ImportsRewritten == 0 {
		t.Error("expected at least one IMPORTS edge rewritten")
	}
}
