package docgen_test

// llm_bundle_meta_2018_2020_test.go — bundle-side regression coverage for
// the Wave 9 meta-fix bundle.
//
//   #2018 — NeighbourBrief.Properties surfaces the per-edge Properties
//           map (and the convenience accessor fields DeadImport /
//           ReExport / IsAsync / Live) so docgen LLM prose can read
//           Bundle B/C/D annotations.
//   #2020 — Module-seeded BuildBundle no longer returns an empty
//           neighbour list. The extractor wires a CONTAINS edge from
//           every Module to its parallel SCOPE.Component(file) entity,
//           and loadEntityContext widens the walk so IMPORTS edges
//           attached to the file entity surface on the Module's bundle.

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

// metaBundleHarness seeds an isolated daemon root with a Module entity
// + parallel file SCOPE.Component + two IMPORTS edges (one re-exported,
// one dead) and returns (groupName, moduleEntityID).
//
// The shape mirrors the Wave-9 W9R1 production case (a Python
// __init__.py with `from .X import Y` re-exports and a dead `from .Z
// import W`) reduced to the minimum surface needed for the test —
// only the entities and edges loadEntityContext walks.
func metaBundleHarness(t *testing.T) (groupName, moduleID string) {
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

	groupName = "meta-bundle-test-group"

	cfgPath, err := registry.ConfigPathFor(groupName)
	if err != nil {
		t.Fatalf("ConfigPathFor: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o755); err != nil {
		t.Fatalf("mkdir fleet config dir: %v", err)
	}
	fleetJSON, _ := json.Marshal(map[string]any{
		"name":  groupName,
		"repos": []map[string]any{{"path": repoPath, "slug": "repo"}},
	})
	if err := os.WriteFile(cfgPath, fleetJSON, 0o644); err != nil {
		t.Fatalf("write fleet config: %v", err)
	}

	// Entity IDs.
	moduleID = graph.EntityID("repo", "Module", "client_fixture_x", "client_fixture_x/__init__.py")
	fileID := graph.EntityID("repo", "SCOPE.Component", "client_fixture_x/__init__.py", "client_fixture_x/__init__.py")
	importedReExportID := graph.EntityID("repo", "SCOPE.Component", "client_fixture_x.models.User", "client_fixture_x/models.py")
	importedDeadID := graph.EntityID("repo", "SCOPE.Component", "client_fixture_x.unused.Stale", "client_fixture_x/unused.py")

	stateDir := daemon.StateDirForRepo(repoPath)
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatalf("mkdir state dir: %v", err)
	}

	doc := graph.Document{
		Version:        1,
		GeneratedAt:    time.Now().UTC(),
		Repo:           repoPath,
		IndexerVersion: "test",
		Stats:          graph.Stats{Files: 2, Entities: 4, Relationships: 3},
		Entities: []graph.Entity{
			{
				ID:         moduleID,
				Name:       "client_fixture_x",
				Kind:       "Module",
				Subtype:    "package",
				Language:   "python",
				SourceFile: "client_fixture_x/__init__.py",
				StartLine:  1,
				EndLine:    5,
			},
			{
				ID:         fileID,
				Name:       "client_fixture_x/__init__.py",
				Kind:       "SCOPE.Component",
				Subtype:    "file",
				Language:   "python",
				SourceFile: "client_fixture_x/__init__.py",
				StartLine:  1,
				EndLine:    5,
			},
			{
				ID:         importedReExportID,
				Name:       "client_fixture_x.models.User",
				Kind:       "SCOPE.Component",
				Subtype:    "class",
				Language:   "python",
				SourceFile: "client_fixture_x/models.py",
				StartLine:  1,
				EndLine:    10,
			},
			{
				ID:         importedDeadID,
				Name:       "client_fixture_x.unused.Stale",
				Kind:       "SCOPE.Component",
				Subtype:    "class",
				Language:   "python",
				SourceFile: "client_fixture_x/unused.py",
				StartLine:  1,
				EndLine:    10,
			},
		},
		Relationships: []graph.Relationship{
			// #2020 — Module → file CONTAINS so Module-seeded bundles
			// reach the file's IMPORTS edges.
			{
				ID:     graph.RelationshipID(moduleID, fileID, "CONTAINS"),
				FromID: moduleID,
				ToID:   fileID,
				Kind:   "CONTAINS",
			},
			// IMPORTS edges attach to the file entity (per
			// internal/extractors/python/imports.go +
			// prune_import_placeholders.go), each carrying Bundle C
			// annotations.
			graph.Relationship{
				ID:     graph.RelationshipID(fileID, importedReExportID, "IMPORTS"),
				FromID: fileID,
				ToID:   importedReExportID,
				Kind:   "IMPORTS",
			}.WithProperties(map[string]string{
				"local_name":    "User",
				"source_module": "client_fixture_x.models",
				"imported_name": "User",
				"re_export":     "true",
				"package_init":  "true",
				"public":        "true",
			},
			),
			graph.Relationship{
				ID:     graph.RelationshipID(fileID, importedDeadID, "IMPORTS"),
				FromID: fileID,
				ToID:   importedDeadID,
				Kind:   "IMPORTS",
			}.WithProperties(map[string]string{
				"local_name":    "Stale",
				"source_module": "client_fixture_x.unused",
				"imported_name": "Stale",
				"dead_import":   "true",
				"live":          "false",
			},
			),
		},
	}
	docJSON, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal doc: %v", err)
	}
	if err := os.WriteFile(filepath.Join(stateDir, "graph.json"), docJSON, 0o644); err != nil {
		t.Fatalf("write graph.json: %v", err)
	}

	return groupName, moduleID
}

// TestIssue2020_ModuleSeededBundleSurfacesFileImports verifies that
// BuildBundle seeded with a Module entity no longer returns an empty
// neighbour list — IMPORTS edges attached to the parallel file
// SCOPE.Component now reach the bundle via the CONTAINS-traversal
// widening in loadEntityContext.
func TestIssue2020_ModuleSeededBundleSurfacesFileImports(t *testing.T) {
	groupName, moduleID := metaBundleHarness(t)

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
	briefs := bundle.GraphContext.NeighbourBriefs
	if len(briefs) == 0 {
		t.Fatalf("Module-seeded bundle returned 0 neighbours (#2020 regression: phantom Module node)")
	}

	// Expect at least the two IMPORTS targets to appear as neighbours.
	var sawReExport, sawDead int
	for _, b := range briefs {
		if b.Relationship == "IMPORTS" {
			if b.ReExport {
				sawReExport++
			}
			if b.DeadImport {
				sawDead++
			}
		}
	}
	if sawReExport == 0 {
		t.Errorf("expected at least one IMPORTS neighbour with ReExport=true, got 0; briefs=%+v", briefs)
	}
	if sawDead == 0 {
		t.Errorf("expected at least one IMPORTS neighbour with DeadImport=true, got 0; briefs=%+v", briefs)
	}
}

// TestIssue2018_NeighbourBriefSurfacesEdgeProperties verifies that
// BuildBundle populates NeighbourBrief.Properties (and the convenience
// accessor fields) from the underlying graph Relationship.Properties.
// Without this every Bundle B/C/D edge annotation (re_export,
// dead_import, live, is_async, cross_repo, ...) is invisible to docgen.
func TestIssue2018_NeighbourBriefSurfacesEdgeProperties(t *testing.T) {
	groupName, moduleID := metaBundleHarness(t)

	bundle, err := docgen.BuildBundle(context.Background(), docgen.BuildBundleOpts{
		RunOpts: docgen.RunOpts{
			Group:        groupName,
			SeedEntityID: moduleID,
			Section:      "overview",
			NoCache:      true,
		},
		Tier:    0,
		NoCache: true,
	})
	if err != nil {
		t.Fatalf("BuildBundle: %v", err)
	}

	var (
		sawReExportProp bool
		sawDeadProp     bool
		sawLiveFalse    bool
		sawLiveTrue     bool
	)
	for _, b := range bundle.GraphContext.NeighbourBriefs {
		if b.Relationship != "IMPORTS" {
			continue
		}
		if b.Properties["re_export"] == "true" {
			sawReExportProp = true
		}
		if b.Properties["dead_import"] == "true" {
			sawDeadProp = true
		}
		// Convenience accessor: Live should be false for the dead edge
		// (live=false stamped) and true for the re-export edge (live
		// property absent → default true).
		if b.DeadImport && !b.Live {
			sawLiveFalse = true
		}
		if b.ReExport && b.Live {
			sawLiveTrue = true
		}
	}
	if !sawReExportProp {
		t.Errorf("NeighbourBrief.Properties missing re_export=true (extractor stamped, bundle dropped — #2018)")
	}
	if !sawDeadProp {
		t.Errorf("NeighbourBrief.Properties missing dead_import=true (extractor stamped, bundle dropped — #2018)")
	}
	if !sawLiveFalse {
		t.Errorf("NeighbourBrief.Live convenience accessor wrong for dead edge — expected false, got true")
	}
	if !sawLiveTrue {
		t.Errorf("NeighbourBrief.Live convenience accessor wrong for re-export edge — expected true (default), got false")
	}
}
