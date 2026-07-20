package docgen_test

// issue1969_synthetic_module_test.go — regression tests for #1969: synthetic
// Module entities (aggregate containers from module/aggregate.go with
// Properties["synthetic"]="true") should:
//   - set gc.SyntheticModule = true
//   - populate ModuleManifest from CONTAINS children when present
//   - not set ModuleReadme / ModuleConfigs (no source file)
//
// Also verifies that SCOPE.Component(file) proxy entities are correctly skipped
// from the Classes bucket in ModuleManifest so a module whose only CONTAINS
// child is a file proxy doesn't return a non-nil manifest with a stale entry.

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

// issue1969SyntheticHarness builds a graph with a synthetic aggregate Module
// entity (Kind="Module", Properties["synthetic"]="true", SourceFile="") and
// CONTAINS children of varying kinds.
//
//   - funcID: SCOPE.Operation/function child
//   - classID: SCOPE.Component/class child
func issue1969SyntheticHarness(t *testing.T) (groupName, synthModuleID string) {
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

	groupName = "issue-1969-synthetic-module-test"

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

	// Synthetic aggregate module: no SourceFile, synthetic=true in Properties.
	// This mirrors the output of internal/module/aggregate.go.
	synthModID := graph.EntityID("repo", "Module", "myapp.services", "")
	funcID := graph.EntityID("repo", "SCOPE.Operation", "do_work", "myapp/services/tasks.py")
	classID := graph.EntityID("repo", "SCOPE.Component", "TaskQueue", "myapp/services/queue.py")

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
			graph.Entity{
				ID:         synthModID,
				Name:       "myapp.services",
				Kind:       "Module",
				SourceFile: "", // synthetic: no source file

			}.WithProperties(map[string]string{
				"synthetic": "true",
				"module":    "myapp.services",
				"repo":      "repo",
			},
			),
			graph.Entity{
				ID:         funcID,
				Name:       "do_work",
				Kind:       "SCOPE.Operation",
				Subtype:    "function",
				SourceFile: "myapp/services/tasks.py",
				StartLine:  10,
				Language:   "python",
			}.WithProperties(map[string]string{"module": "myapp.services"}),
			graph.Entity{
				ID:         classID,
				Name:       "TaskQueue",
				Kind:       "SCOPE.Component",
				Subtype:    "class",
				SourceFile: "myapp/services/queue.py",
				StartLine:  5,
				Language:   "python",
			}.WithProperties(map[string]string{"module": "myapp.services"}),
		},
		Relationships: []graph.Relationship{
			{
				ID:     graph.RelationshipID(synthModID, funcID, "CONTAINS"),
				FromID: synthModID,
				ToID:   funcID,
				Kind:   "CONTAINS",
			},
			{
				ID:     graph.RelationshipID(synthModID, classID, "CONTAINS"),
				FromID: synthModID,
				ToID:   classID,
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

	return groupName, synthModID
}

// TestBuildBundle_SyntheticModule_Flag verifies that gc.SyntheticModule is set
// to true when the seed Module entity has Properties["synthetic"]="true" and
// SourceFile="" (#1969).
func TestBuildBundle_SyntheticModule_Flag(t *testing.T) {
	groupName, modID := issue1969SyntheticHarness(t)

	opts := docgen.BuildBundleOpts{
		RunOpts: docgen.RunOpts{
			Group:        groupName,
			SeedEntityID: modID,
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

	if !bundle.GraphContext.SyntheticModule {
		t.Error("gc.SyntheticModule is false — expected true for synthetic aggregate module")
	}
}

// TestBuildBundle_SyntheticModule_NoReadme verifies that gc.ModuleReadme is nil
// for a synthetic aggregate module (no source file to look for README near) (#1969).
func TestBuildBundle_SyntheticModule_NoReadme(t *testing.T) {
	groupName, modID := issue1969SyntheticHarness(t)

	opts := docgen.BuildBundleOpts{
		RunOpts: docgen.RunOpts{
			Group:        groupName,
			SeedEntityID: modID,
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

	if bundle.GraphContext.ModuleReadme != nil {
		t.Errorf("gc.ModuleReadme is non-nil for synthetic module — expected nil: %+v", bundle.GraphContext.ModuleReadme)
	}
}

// TestBuildBundle_SyntheticModule_ManifestFromContainsChildren verifies that
// gc.ModuleManifest is populated from the CONTAINS children of a synthetic
// aggregate module when those children are present (#1969).
func TestBuildBundle_SyntheticModule_ManifestFromContainsChildren(t *testing.T) {
	groupName, modID := issue1969SyntheticHarness(t)

	opts := docgen.BuildBundleOpts{
		RunOpts: docgen.RunOpts{
			Group:        groupName,
			SeedEntityID: modID,
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

	mm := bundle.GraphContext.ModuleManifest
	if mm == nil {
		t.Fatal("gc.ModuleManifest is nil — expected non-nil: synthetic module has CONTAINS children that should populate the manifest")
	}

	// Should have a function from do_work.
	if len(mm.Functions) == 0 {
		t.Errorf("ModuleManifest.Functions is empty — expected do_work to appear")
	}
	foundFunc := false
	for _, f := range mm.Functions {
		if f.Name == "do_work" {
			foundFunc = true
			break
		}
	}
	if !foundFunc {
		t.Errorf("do_work not found in ModuleManifest.Functions: %+v", mm.Functions)
	}

	// Should have a class from TaskQueue.
	if len(mm.Classes) == 0 {
		t.Errorf("ModuleManifest.Classes is empty — expected TaskQueue to appear")
	}
	foundClass := false
	for _, c := range mm.Classes {
		if c.Name == "TaskQueue" {
			foundClass = true
			break
		}
	}
	if !foundClass {
		t.Errorf("TaskQueue not found in ModuleManifest.Classes: %+v", mm.Classes)
	}
}

// TestBuildBundle_SyntheticModule_Flag_False_ForRealModule verifies that
// gc.SyntheticModule is NOT set for a regular (non-synthetic) Module entity.
func TestBuildBundle_SyntheticModule_Flag_False_ForRealModule(t *testing.T) {
	// Re-use the moduleManifestHarness which builds a real SCOPE.Module entity
	// with SourceFile and children.
	groupName, moduleID := moduleManifestHarness(t)

	opts := docgen.BuildBundleOpts{
		RunOpts: docgen.RunOpts{
			Group:        groupName,
			SeedEntityID: moduleID,
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

	if bundle.GraphContext.SyntheticModule {
		t.Error("gc.SyntheticModule is true for a real (non-synthetic) module — should be false")
	}
}

// TestBuildBundle_ModuleManifest_FileProxySkipped verifies that SCOPE.Component
// entities with Subtype="file" are NOT counted in the Classes bucket of
// ModuleManifest, so they don't pollute the manifest with proxy entities (#1969).
func TestBuildBundle_ModuleManifest_FileProxySkipped(t *testing.T) {
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

	groupName := "issue-1969-file-proxy-test"
	cfgPath, err := registry.ConfigPathFor(groupName)
	if err != nil {
		t.Fatalf("ConfigPathFor: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	fleetJSON, _ := json.Marshal(map[string]interface{}{
		"name":  groupName,
		"repos": []map[string]interface{}{{"path": repoPath, "slug": "repo"}},
	})
	if err := os.WriteFile(cfgPath, fleetJSON, 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	moduleID := graph.EntityID("repo", "Module", "myapp.views", "myapp/views.py")
	// This is the file-proxy entity emitted by the Python extractor (#2020).
	fileProxyID := graph.EntityID("repo", "SCOPE.Component", "myapp/views.py", "myapp/views.py")
	realClassID := graph.EntityID("repo", "SCOPE.Component", "HomeView", "myapp/views.py")

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
				ID:         moduleID,
				Name:       "myapp.views",
				Kind:       "Module",
				SourceFile: "myapp/views.py",
				Language:   "python",
			},
			{
				ID:         fileProxyID,
				Name:       "myapp/views.py",
				Kind:       "SCOPE.Component",
				Subtype:    "file", // file proxy — should be skipped
				SourceFile: "myapp/views.py",
				StartLine:  1,
				Language:   "python",
			},
			{
				ID:         realClassID,
				Name:       "HomeView",
				Kind:       "SCOPE.Component",
				Subtype:    "class", // real class — should appear in Classes bucket
				SourceFile: "myapp/views.py",
				StartLine:  10,
				Language:   "python",
			},
		},
		Relationships: []graph.Relationship{
			{
				ID:     graph.RelationshipID(moduleID, fileProxyID, "CONTAINS"),
				FromID: moduleID,
				ToID:   fileProxyID,
				Kind:   "CONTAINS",
			},
			{
				ID:     graph.RelationshipID(moduleID, realClassID, "CONTAINS"),
				FromID: moduleID,
				ToID:   realClassID,
				Kind:   "CONTAINS",
			},
		},
	}
	docJSON, _ := json.Marshal(doc)
	if err := os.WriteFile(filepath.Join(stateDir, "graph.json"), docJSON, 0o644); err != nil {
		t.Fatalf("write graph.json: %v", err)
	}

	opts := docgen.BuildBundleOpts{
		RunOpts: docgen.RunOpts{
			Group:        groupName,
			SeedEntityID: moduleID,
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

	mm := bundle.GraphContext.ModuleManifest
	if mm == nil {
		t.Fatal("ModuleManifest is nil — expected non-nil since module has a real class child")
	}

	// Verify the file proxy is NOT in Classes.
	for _, c := range mm.Classes {
		if c.KindSubtype == "file" {
			t.Errorf("file proxy entity found in ModuleManifest.Classes: %+v — file proxies should be skipped", c)
		}
	}

	// Verify the real class IS in Classes.
	found := false
	for _, c := range mm.Classes {
		if c.Name == "HomeView" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("HomeView not found in ModuleManifest.Classes: %+v — real class should be present", mm.Classes)
	}

	// Classes bucket should have exactly 1 entry (HomeView only, not the file proxy).
	if len(mm.Classes) != 1 {
		t.Errorf("ModuleManifest.Classes has %d entries, want 1 (file proxy must be excluded): %+v", len(mm.Classes), mm.Classes)
	}
}
