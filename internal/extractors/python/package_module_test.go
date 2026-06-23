package python_test

// package_module_test.go — fixture tests for issue #1884 Module entity emission.
//
// Three core cases:
//  1. __init__.py with a class + function: emits one Module entity (Kind="Module",
//     Subtype="package", is_package="true") plus CONTAINS edges to class/function.
//  2. Sub-package __init__.py (core/views/__init__.py): Module with correct
//     parent_package="core", dottedName="core.views".
//  3. Plain .py file (core/tasks.py): Module with is_package="false".
//  4. Empty __init__.py (namespace / PEP 420 style): still emits Module entity.
//  5. Ignored paths (site-packages, vendor, .grafel): no Module emitted.
//  6. Django migration __init__.py: no Module emitted (pruned).

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
)

// findModuleEntities returns all Kind="Module" / Subtype="package" entities.
func findModuleEntities(entities []types.EntityRecord) []types.EntityRecord {
	var out []types.EntityRecord
	for _, e := range entities {
		if e.Kind == string(types.EntityKindModule) && e.Subtype == "package" {
			out = append(out, e)
		}
	}
	return out
}

// findModuleByName returns the first Module entity with the given Name, or nil.
func findModuleByName(entities []types.EntityRecord, name string) *types.EntityRecord {
	for i := range entities {
		e := &entities[i]
		if e.Kind == string(types.EntityKindModule) && e.Name == name {
			return e
		}
	}
	return nil
}

// containsEdgeTo reports whether the entity has a CONTAINS edge with the
// given suffix in its ToID.
func containsEdgeTo(e types.EntityRecord, toIDSuffix string) bool {
	for _, r := range e.Relationships {
		if r.Kind == "CONTAINS" && strings.Contains(r.ToID, toIDSuffix) {
			return true
		}
	}
	return false
}

// TestPackageModule_InitPyWithClassAndFunction verifies that an __init__.py
// file with a class and a top-level function emits exactly one Module entity
// with is_package="true", and that it carries CONTAINS edges to both the class
// and the function.
func TestPackageModule_InitPyWithClassAndFunction(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("testdata", "pkg_with_subpkg_init.py.fixture"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	tree := parse(t, src)
	ext, ok := extractor.Get("python")
	if !ok {
		t.Fatal("python extractor not registered")
	}

	fi := extractor.FileInput{
		Path:     "core/__init__.py",
		Content:  src,
		Language: "python",
		TSTree:   tree,
	}
	entities, err := ext.Extract(context.Background(), fi)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}

	mods := findModuleEntities(entities)
	if len(mods) != 1 {
		t.Fatalf("expected 1 Module entity, got %d (entities: %v)", len(mods), entitySummary(entities))
	}

	mod := mods[0]
	if mod.Name != "core" {
		t.Errorf("Module.Name = %q, want %q", mod.Name, "core")
	}
	if mod.QualifiedName != "core" {
		t.Errorf("Module.QualifiedName = %q, want %q", mod.QualifiedName, "core")
	}
	if mod.SourceFile != "core/__init__.py" {
		t.Errorf("Module.SourceFile = %q, want %q", mod.SourceFile, "core/__init__.py")
	}
	if got := mod.Properties["is_package"]; got != "true" {
		t.Errorf("Module.Properties[is_package] = %q, want %q", got, "true")
	}
	if got := mod.Properties["parent_package"]; got != "" {
		t.Errorf("Module.Properties[parent_package] = %q, want empty (top-level package)", got)
	}
	if mod.StartLine != 1 {
		t.Errorf("Module.StartLine = %d, want 1", mod.StartLine)
	}
	if mod.EndLine < 1 {
		t.Errorf("Module.EndLine = %d, want >= 1", mod.EndLine)
	}

	// CONTAINS edges: class CoreRegistry and function get_registry.
	if !containsEdgeTo(mod, "CoreRegistry") {
		t.Errorf("Module missing CONTAINS edge to CoreRegistry; relationships: %v", mod.Relationships)
	}
	if !containsEdgeTo(mod, "get_registry") {
		t.Errorf("Module missing CONTAINS edge to get_registry; relationships: %v", mod.Relationships)
	}
}

// TestPackageModule_SubPackageInitPy verifies that a nested __init__.py emits
// a Module entity with the correct dotted name and parent_package.
func TestPackageModule_SubPackageInitPy(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("testdata", "subpkg_init.py.fixture"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	tree := parse(t, src)
	ext, ok := extractor.Get("python")
	if !ok {
		t.Fatal("python extractor not registered")
	}

	fi := extractor.FileInput{
		Path:     "core/views/__init__.py",
		Content:  src,
		Language: "python",
		TSTree:   tree,
	}
	entities, err := ext.Extract(context.Background(), fi)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}

	mods := findModuleEntities(entities)
	if len(mods) != 1 {
		t.Fatalf("expected 1 Module entity, got %d (entities: %v)", len(mods), entitySummary(entities))
	}

	mod := mods[0]
	if mod.Name != "core.views" {
		t.Errorf("Module.Name = %q, want %q", mod.Name, "core.views")
	}
	if got := mod.Properties["is_package"]; got != "true" {
		t.Errorf("Module.Properties[is_package] = %q, want %q", got, "true")
	}
	if got := mod.Properties["parent_package"]; got != "core" {
		t.Errorf("Module.Properties[parent_package] = %q, want %q", got, "core")
	}
}

// TestPackageModule_PlainPyFile verifies that a plain .py file (not __init__.py)
// emits a Module entity with is_package="false" and the correct dotted name.
func TestPackageModule_PlainPyFile(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("testdata", "plain_module.py.fixture"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	tree := parse(t, src)
	ext, ok := extractor.Get("python")
	if !ok {
		t.Fatal("python extractor not registered")
	}

	fi := extractor.FileInput{
		Path:     "core/tasks.py",
		Content:  src,
		Language: "python",
		TSTree:   tree,
	}
	entities, err := ext.Extract(context.Background(), fi)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}

	mods := findModuleEntities(entities)
	if len(mods) != 1 {
		t.Fatalf("expected 1 Module entity, got %d (entities: %v)", len(mods), entitySummary(entities))
	}

	mod := mods[0]
	if mod.Name != "core.tasks" {
		t.Errorf("Module.Name = %q, want %q", mod.Name, "core.tasks")
	}
	if got := mod.Properties["is_package"]; got != "false" {
		t.Errorf("Module.Properties[is_package] = %q, want %q", got, "false")
	}
}

// TestPackageModule_EmptyInitPy verifies that an empty __init__.py (namespace
// package / PEP 420 style) still emits a Module entity — it is a valid package
// boundary even with zero content.
func TestPackageModule_EmptyInitPy(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("testdata", "namespace_pkg_init.py.fixture"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	tree := parse(t, src)
	ext, ok := extractor.Get("python")
	if !ok {
		t.Fatal("python extractor not registered")
	}

	fi := extractor.FileInput{
		Path:     "mypkg/__init__.py",
		Content:  src,
		Language: "python",
		TSTree:   tree,
	}
	entities, err := ext.Extract(context.Background(), fi)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}

	mods := findModuleEntities(entities)
	if len(mods) != 1 {
		t.Fatalf("expected 1 Module entity for empty __init__.py, got %d", len(mods))
	}

	mod := mods[0]
	if mod.Name != "mypkg" {
		t.Errorf("Module.Name = %q, want %q", mod.Name, "mypkg")
	}
	if got := mod.Properties["is_package"]; got != "true" {
		t.Errorf("Module.Properties[is_package] = %q, want %q", got, "true")
	}
}

// TestPackageModule_IgnoredPaths verifies that paths inside known ignored
// directories (site-packages, vendor, .grafel) do NOT emit Module entities.
func TestPackageModule_IgnoredPaths(t *testing.T) {
	ext, ok := extractor.Get("python")
	if !ok {
		t.Fatal("python extractor not registered")
	}

	ignoredPaths := []string{
		"site-packages/requests/__init__.py",
		"vendor/django/__init__.py",
		".grafel/store/something/__init__.py",
		".venv/lib/python3.11/site-packages/foo/__init__.py",
	}

	for _, path := range ignoredPaths {
		t.Run(path, func(t *testing.T) {
			fi := extractor.FileInput{
				Path:     path,
				Content:  []byte("# ignored\n"),
				Language: "python",
			}
			entities, err := ext.Extract(context.Background(), fi)
			if err != nil {
				t.Fatalf("Extract: %v", err)
			}
			mods := findModuleEntities(entities)
			if len(mods) != 0 {
				t.Errorf("path %q: expected 0 Module entities, got %d: %v", path, len(mods), mods)
			}
		})
	}
}

// TestPackageModule_DjangoMigrationInitPy verifies that an __init__.py inside
// a migrations/ package does NOT emit a Module entity (same pruning rule as
// migration .py files — they are machine-generated, not architectural signal).
func TestPackageModule_DjangoMigrationInitPy(t *testing.T) {
	ext, ok := extractor.Get("python")
	if !ok {
		t.Fatal("python extractor not registered")
	}

	fi := extractor.FileInput{
		Path:     "core/migrations/__init__.py",
		Content:  []byte("# auto-generated\n"),
		Language: "python",
	}
	entities, err := ext.Extract(context.Background(), fi)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}

	mods := findModuleEntities(entities)
	if len(mods) != 0 {
		t.Errorf("migrations __init__.py: expected 0 Module entities, got %d: %v", len(mods), mods)
	}
}

// TestPackageModule_ThreeLevelHierarchy verifies that three Module entities
// (repo-level package, sub-package, sub-sub-package) carry correct dotted
// names and parent_package links when extracted in isolation. The
// parent→child CONTAINS wiring is done at graph-build time (the resolver
// stitches the hierarchy via parent_package); this test only verifies the
// per-file extraction output.
func TestPackageModule_ThreeLevelHierarchy(t *testing.T) {
	ext, ok := extractor.Get("python")
	if !ok {
		t.Fatal("python extractor not registered")
	}

	cases := []struct {
		path          string
		wantName      string
		wantParent    string
		wantIsPackage string
	}{
		{"core/__init__.py", "core", "", "true"},
		{"core/api/__init__.py", "core.api", "core", "true"},
		{"core/api/v1/__init__.py", "core.api.v1", "core.api", "true"},
	}

	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			fi := extractor.FileInput{
				Path:     tc.path,
				Content:  []byte("# empty\n"),
				Language: "python",
			}
			entities, err := ext.Extract(context.Background(), fi)
			if err != nil {
				t.Fatalf("Extract: %v", err)
			}
			mods := findModuleEntities(entities)
			if len(mods) != 1 {
				t.Fatalf("path %q: expected 1 Module entity, got %d", tc.path, len(mods))
			}
			mod := mods[0]
			if mod.Name != tc.wantName {
				t.Errorf("Name = %q, want %q", mod.Name, tc.wantName)
			}
			if got := mod.Properties["parent_package"]; got != tc.wantParent {
				t.Errorf("parent_package = %q, want %q", got, tc.wantParent)
			}
			if got := mod.Properties["is_package"]; got != tc.wantIsPackage {
				t.Errorf("is_package = %q, want %q", got, tc.wantIsPackage)
			}
		})
	}
}

// TestPackageModule_SrcPrefixStripped verifies that the standard src/ source-
// root prefix is stripped when deriving the dotted package name, matching the
// behaviour of filePathToModule.
func TestPackageModule_SrcPrefixStripped(t *testing.T) {
	ext, ok := extractor.Get("python")
	if !ok {
		t.Fatal("python extractor not registered")
	}

	fi := extractor.FileInput{
		Path:     "src/app/models/__init__.py",
		Content:  []byte("# empty\n"),
		Language: "python",
	}
	entities, err := ext.Extract(context.Background(), fi)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}

	mods := findModuleEntities(entities)
	if len(mods) != 1 {
		t.Fatalf("expected 1 Module entity, got %d", len(mods))
	}
	if got := mods[0].Name; got != "app.models" {
		t.Errorf("Module.Name = %q, want %q", got, "app.models")
	}
}

// entitySummary returns a compact string listing entity kinds and names for
// test error messages.
func entitySummary(entities []types.EntityRecord) string {
	var sb strings.Builder
	for _, e := range entities {
		sb.WriteString("[" + e.Kind + "/" + e.Subtype + " " + e.Name + "] ")
	}
	return sb.String()
}
