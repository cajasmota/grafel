// prune_import_placeholders_test.go — Issue #693 acceptance tests.
//
// These tests verify that:
// 1. `import django.db.models` and `import os` do NOT produce orphan
//    SCOPE.Component/module placeholder entities.
// 2. The IMPORTS edge + Properties are still present (on the file entity).
// 3. Real class/function SCOPE.Component entities are NOT affected.
// 4. In-tree relative imports (where the placeholder might be "used") still
//    produce an IMPORTS edge with the original properties (not pruned — the
//    edge still exists; only the standalone module entity is removed).
// 5. The file entity (SCOPE.Component/file) carries the IMPORTS edges.

package python

import (
	"context"
	"testing"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
)

// noModulePlaceholders returns every SCOPE.Component entity with
// Subtype=="module" in ents. After issue #693 there must be none.
func modulePlaceholders(ents []types.EntityRecord) []types.EntityRecord {
	var out []types.EntityRecord
	for _, e := range ents {
		if e.Kind == "SCOPE.Component" && e.Subtype == "module" {
			out = append(out, e)
		}
	}
	return out
}

// findFileEntity returns the SCOPE.Component/file entity, or nil.
func findFileEntity(ents []types.EntityRecord) *types.EntityRecord {
	for i := range ents {
		if ents[i].Kind == "SCOPE.Component" && ents[i].Subtype == "file" {
			return &ents[i]
		}
	}
	return nil
}

// findImportOnAny returns the IMPORTS edge with the given source_module
// from ANY entity in ents, or nil when absent.
func findImportOnAny(ents []types.EntityRecord, sourceModule string) *types.RelationshipRecord {
	for i := range ents {
		for j := range ents[i].Relationships {
			r := &ents[i].Relationships[j]
			if r.Kind == "IMPORTS" && r.Properties != nil &&
				r.Properties.Get("source_module") == sourceModule {
				return r
			}
		}
	}
	return nil
}

func extractPython(t *testing.T, src string) []types.EntityRecord {
	t.Helper()
	ex := &Extractor{}
	ents, err := ex.Extract(context.Background(), extractor.FileInput{
		Path:     "test.py",
		Language: "python",
		Content:  []byte(src),
	})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	return ents
}

// TestNoPythonModulePlaceholderForDjangoImport — core acceptance test for #693.
// `import django.db.models` must NOT produce a SCOPE.Component/module entity.
// The IMPORTS edge must still exist (on the file entity) with correct Properties.
func TestNoPythonModulePlaceholderForDjangoImport(t *testing.T) {
	src := `import django.db.models

class Article(django.db.models.Model):
    title = django.db.models.CharField(max_length=200)
`
	ents := extractPython(t, src)

	// No import-placeholder SCOPE.Component/module entities.
	if placeholders := modulePlaceholders(ents); len(placeholders) > 0 {
		names := make([]string, len(placeholders))
		for i, p := range placeholders {
			names[i] = p.Name
		}
		t.Errorf("SCOPE.Component/module placeholder entities still emitted (#693): %v", names)
	}

	// The IMPORTS edge for django.db.models must still exist.
	r := findImportOnAny(ents, "django.db.models")
	if r == nil {
		t.Error("IMPORTS edge for django.db.models not found after #693 fix")
	}
}

// TestNoPythonModulePlaceholderForOsImport verifies that a plain `import os`
// also does not create a placeholder entity.
func TestNoPythonModulePlaceholderForOsImport(t *testing.T) {
	src := `import os

def get_env():
    return os.environ.get("HOME")
`
	ents := extractPython(t, src)

	if placeholders := modulePlaceholders(ents); len(placeholders) > 0 {
		names := make([]string, len(placeholders))
		for i, p := range placeholders {
			names[i] = p.Name
		}
		t.Errorf("SCOPE.Component/module placeholder entities still emitted for 'import os': %v", names)
	}

	// IMPORTS edge for "os" must still exist.
	r := findImportOnAny(ents, "os")
	if r == nil {
		t.Error("IMPORTS edge for source_module='os' not found after #693 fix")
	}
}

// TestNoPythonModulePlaceholderMultipleImports — multiple external imports
// produce zero placeholder entities; all IMPORTS edges present.
func TestNoPythonModulePlaceholderMultipleImports(t *testing.T) {
	src := `import os
import sys
from django.db import models
from rest_framework import serializers
from rest_framework.views import APIView
import pandas as pd
`
	ents := extractPython(t, src)

	if placeholders := modulePlaceholders(ents); len(placeholders) > 0 {
		names := make([]string, len(placeholders))
		for i, p := range placeholders {
			names[i] = p.Name
		}
		t.Errorf("SCOPE.Component/module placeholder entities emitted for multiple imports: %v", names)
	}

	// Every import must have a corresponding IMPORTS edge.
	wantModules := []string{
		"os",
		"sys",
		"django.db",
		"rest_framework",
		"rest_framework.views",
		"pandas",
	}
	for _, mod := range wantModules {
		if findImportOnAny(ents, mod) == nil {
			t.Errorf("IMPORTS edge for source_module=%q not found", mod)
		}
	}
}

// TestRealClassEntityNotPrunedByImportFix — regression: a genuine
// SCOPE.Component/class entity must NOT be removed. Only module
// placeholder entities (Subtype=="module") are dropped.
func TestRealClassEntityNotPrunedByImportFix(t *testing.T) {
	src := `import os
from django.db import models

class UserProfile(models.Model):
    username = models.CharField(max_length=100)

    def __str__(self):
        return self.username
`
	ents := extractPython(t, src)

	// No module placeholder entities.
	if placeholders := modulePlaceholders(ents); len(placeholders) > 0 {
		names := make([]string, len(placeholders))
		for i, p := range placeholders {
			names[i] = p.Name
		}
		t.Errorf("SCOPE.Component/module placeholder entities still emitted: %v", names)
	}

	// UserProfile class must still exist.
	foundClass := false
	for _, e := range ents {
		if e.Kind == "SCOPE.Component" && e.Subtype == "class" && e.Name == "UserProfile" {
			foundClass = true
		}
	}
	if !foundClass {
		t.Error("UserProfile class entity was pruned — regression")
	}
}

// TestImportsEdgeAttachedToFileEntity — IMPORTS edges must be on the
// file-level entity (SCOPE.Component/file), not standalone entities.
func TestImportsEdgeAttachedToFileEntity(t *testing.T) {
	src := `from django.db import models
import requests
`
	ents := extractPython(t, src)

	fileEnt := findFileEntity(ents)
	if fileEnt == nil {
		t.Fatal("file entity (SCOPE.Component/file) not found")
	}

	// The file entity must carry the IMPORTS edges.
	wantSrcModules := map[string]bool{
		"django.db": false,
		"requests":  false,
	}
	for _, r := range fileEnt.Relationships {
		if r.Kind != "IMPORTS" || r.Properties == nil {
			continue
		}
		mod := r.Properties.Get("source_module")
		if _, ok := wantSrcModules[mod]; ok {
			wantSrcModules[mod] = true
		}
	}
	for mod, found := range wantSrcModules {
		if !found {
			t.Errorf("IMPORTS edge for source_module=%q not found on file entity (rels count=%d)", mod, len(fileEnt.Relationships))
		}
	}
}

// TestRelativeImportNoPlaceholderStillHasEdge — `from .models import Profile`
// (in-tree relative import) must not produce a module placeholder AND must
// still produce an IMPORTS edge so the resolver can bind it.
func TestRelativeImportNoPlaceholderStillHasEdge(t *testing.T) {
	src := `from .models import Profile

class ArticleSerializer:
    pass
`
	ents := extractPython(t, src)

	// No module placeholders.
	if placeholders := modulePlaceholders(ents); len(placeholders) > 0 {
		names := make([]string, len(placeholders))
		for i, p := range placeholders {
			names[i] = p.Name
		}
		t.Errorf("SCOPE.Component/module placeholder entities emitted for relative import: %v", names)
	}

	// IMPORTS edge for the relative import must still exist.
	found := false
	for _, e := range ents {
		for _, r := range e.Relationships {
			if r.Kind != "IMPORTS" || r.Properties == nil {
				continue
			}
			// Relative import: source_module starts with "." or
			// imported_name is "Profile" from ".models"
			if r.Properties.Get("imported_name") == "Profile" ||
				r.Properties.Get("source_module") == ".models" {
				found = true
			}
		}
	}
	if !found {
		t.Error("IMPORTS edge for relative import '.models.Profile' not found")
	}
}

// TestWildcardImportNoPlaceholder — `from x import *` also must not produce
// a module placeholder entity; the IMPORTS edge with wildcard="1" must exist.
func TestWildcardImportNoPlaceholder(t *testing.T) {
	src := `from django.utils import *
`
	ents := extractPython(t, src)

	if placeholders := modulePlaceholders(ents); len(placeholders) > 0 {
		names := make([]string, len(placeholders))
		for i, p := range placeholders {
			names[i] = p.Name
		}
		t.Errorf("SCOPE.Component/module placeholder entities emitted for wildcard import: %v", names)
	}

	// Wildcard IMPORTS edge must exist.
	found := false
	for _, e := range ents {
		for _, r := range e.Relationships {
			if r.Kind == "IMPORTS" && r.Properties != nil &&
				r.Properties.Get("wildcard") == "1" {
				found = true
			}
		}
	}
	if !found {
		t.Error("wildcard IMPORTS edge not found after #693 fix")
	}
}
