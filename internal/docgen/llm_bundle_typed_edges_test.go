package docgen_test

// llm_bundle_typed_edges_test.go — unit tests for typed NeighbourBrief
// relationships (#1879).
//
// Before this fix, BuildBundle collapsed every 1-hop neighbour edge to the
// literal "RELATED" string, even though the underlying graph already carried
// typed edge kinds (CALLS, IMPORTS, CONTAINS, REFERENCES, DEPENDS_ON, FK_TO,
// ...). These tests build a fixture graph with a seed entity connected to
// distinct neighbours via different edge kinds and assert that each kind
// surfaces verbatim in the corresponding NeighbourBrief.Relationship value.

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cajasmota/archigraph/internal/daemon"
	"github.com/cajasmota/archigraph/internal/docgen"
	"github.com/cajasmota/archigraph/internal/graph"
	"github.com/cajasmota/archigraph/internal/registry"
)

// typedEdgesHarness sets up an isolated group containing a single repo with
// one seed entity wired to several neighbours via DIFFERENT typed edge kinds.
// Returns the group name and the seed entity ID.
//
// Fixture topology (seed at center):
//
//	seed --CALLS-->        callee
//	seed --IMPORTS-->      importedPkg
//	seed --CONTAINS-->     childFn
//	seed --REFERENCES-->   refTarget
//	seed --DEPENDS_ON-->   depTarget
//	seed --FK_TO-->        fkTarget
//	parentMod --CONTAINS--> seed   (inbound — kind should still surface)
func typedEdgesHarness(t *testing.T) (groupName, seedID string) {
	t.Helper()

	tmp := t.TempDir()

	homeDir := filepath.Join(tmp, "home")
	xdgDir := filepath.Join(tmp, "xdg")
	daemonRoot := filepath.Join(tmp, "daemon")
	repoPath := filepath.Join(tmp, "myrepo")

	for _, d := range []string{homeDir, xdgDir, daemonRoot, repoPath} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", d, err)
		}
	}

	t.Setenv("ARCHIGRAPH_HOME", homeDir)
	t.Setenv("XDG_CONFIG_HOME", xdgDir)
	t.Setenv(daemon.EnvRoot, daemonRoot)

	groupName = "typed-edges-test-group"

	cfgPath, err := registry.ConfigPathFor(groupName)
	if err != nil {
		t.Fatalf("ConfigPathFor: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o755); err != nil {
		t.Fatalf("mkdir fleet config dir: %v", err)
	}
	fleetJSON, _ := json.Marshal(map[string]interface{}{
		"name": groupName,
		"repos": []map[string]interface{}{
			{"path": repoPath, "slug": "myrepo"},
		},
	})
	if err := os.WriteFile(cfgPath, fleetJSON, 0o644); err != nil {
		t.Fatalf("write fleet config: %v", err)
	}

	// Entity IDs — fixed hex so assertions can reference them by name.
	seedID = "1111111111111111"
	calleeID := "2222222222222222"
	importedID := "3333333333333333"
	childID := "4444444444444444"
	refID := "5555555555555555"
	depID := "6666666666666666"
	fkID := "7777777777777777"
	parentID := "8888888888888888"

	doc := graph.Document{
		Version:        1,
		GeneratedAt:    time.Now().UTC(),
		Repo:           repoPath,
		IndexerVersion: "test",
		Entities: []graph.Entity{
			{ID: seedID, Name: "seedFn", Kind: "Function", SourceFile: "seed.go", Language: "go"},
			{ID: calleeID, Name: "callee", Kind: "Function", SourceFile: "callee.go", Language: "go"},
			{ID: importedID, Name: "fmt", Kind: "Module", SourceFile: "", Language: "go"},
			{ID: childID, Name: "innerFn", Kind: "Function", SourceFile: "seed.go", Language: "go"},
			{ID: refID, Name: "RefType", Kind: "Class", SourceFile: "ref.go", Language: "go"},
			{ID: depID, Name: "depTarget", Kind: "Module", SourceFile: "", Language: "go"},
			{ID: fkID, Name: "fkTable", Kind: "Table", SourceFile: "schema.sql", Language: "sql"},
			{ID: parentID, Name: "ParentModule", Kind: "Module", SourceFile: "seed.go", Language: "go"},
		},
		Relationships: []graph.Relationship{
			{ID: "r1", FromID: seedID, ToID: calleeID, Kind: "CALLS"},
			{ID: "r2", FromID: seedID, ToID: importedID, Kind: "IMPORTS"},
			{ID: "r3", FromID: seedID, ToID: childID, Kind: "CONTAINS"},
			{ID: "r4", FromID: seedID, ToID: refID, Kind: "REFERENCES"},
			{ID: "r5", FromID: seedID, ToID: depID, Kind: "DEPENDS_ON"},
			{ID: "r6", FromID: seedID, ToID: fkID, Kind: "FK_TO"},
			// Inbound edge: parent contains seed. The seed→parent neighbour
			// should still surface the "CONTAINS" kind (preserved verbatim
			// regardless of direction).
			{ID: "r7", FromID: parentID, ToID: seedID, Kind: "CONTAINS"},
		},
	}
	doc.Stats = graph.Stats{
		Files:         3,
		Entities:      len(doc.Entities),
		Relationships: len(doc.Relationships),
	}

	stateDir := daemon.StateDirForRepo(repoPath)
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatalf("mkdir state dir: %v", err)
	}
	docJSON, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal graph doc: %v", err)
	}
	if err := os.WriteFile(filepath.Join(stateDir, "graph.json"), docJSON, 0o644); err != nil {
		t.Fatalf("write graph.json: %v", err)
	}

	return groupName, seedID
}

// TestBuildBundle_NeighbourBrief_TypedRelationships asserts that each typed
// edge in the fixture surfaces in NeighbourBrief.Relationship verbatim, and
// that NO neighbour is reported with the legacy "RELATED" placeholder when
// the graph has explicit kinds (#1879).
func TestBuildBundle_NeighbourBrief_TypedRelationships(t *testing.T) {
	groupName, seedID := typedEdgesHarness(t)

	opts := docgen.BuildBundleOpts{
		RunOpts: docgen.RunOpts{
			Group:        groupName,
			SeedEntityID: seedID,
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
		t.Fatalf("expected non-empty neighbour_briefs, got 0")
	}

	// Build a lookup: neighbour name → relationship kind.
	byName := make(map[string]string, len(briefs))
	for _, b := range briefs {
		byName[b.Name] = b.Relationship
	}

	wantByName := map[string]string{
		"callee":       docgen.NeighbourRelationshipCalls,
		"fmt":          docgen.NeighbourRelationshipImports,
		"innerFn":      docgen.NeighbourRelationshipContains,
		"RefType":      docgen.NeighbourRelationshipReferences,
		"depTarget":    docgen.NeighbourRelationshipDependsOn,
		"fkTable":      docgen.NeighbourRelationshipFKTo,
		"ParentModule": docgen.NeighbourRelationshipContains, // inbound CONTAINS edge
	}

	for name, wantKind := range wantByName {
		got, ok := byName[name]
		if !ok {
			t.Errorf("neighbour %q missing from neighbour_briefs", name)
			continue
		}
		if got != wantKind {
			t.Errorf("neighbour %q: Relationship=%q want %q", name, got, wantKind)
		}
	}

	// Defensive: nothing should be the legacy "RELATED" placeholder when the
	// graph has explicit kinds for every edge.
	for _, b := range briefs {
		if b.Relationship == docgen.NeighbourRelationshipRelated {
			t.Errorf("neighbour %q collapsed to RELATED — typed edge kind was not preserved", b.Name)
		}
	}
}

// TestBuildBundle_NeighbourBrief_FallbackToRELATED asserts that when the
// graph stores an edge with an empty Kind, NeighbourBrief.Relationship falls
// back to "RELATED" rather than emitting an empty string. This is the
// defensive path documented on BuildBundle.
func TestBuildBundle_NeighbourBrief_FallbackToRELATED(t *testing.T) {
	tmp := t.TempDir()

	homeDir := filepath.Join(tmp, "home")
	xdgDir := filepath.Join(tmp, "xdg")
	daemonRoot := filepath.Join(tmp, "daemon")
	repoPath := filepath.Join(tmp, "myrepo")

	for _, d := range []string{homeDir, xdgDir, daemonRoot, repoPath} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", d, err)
		}
	}

	t.Setenv("ARCHIGRAPH_HOME", homeDir)
	t.Setenv("XDG_CONFIG_HOME", xdgDir)
	t.Setenv(daemon.EnvRoot, daemonRoot)

	groupName := "typed-edges-fallback-group"

	cfgPath, err := registry.ConfigPathFor(groupName)
	if err != nil {
		t.Fatalf("ConfigPathFor: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o755); err != nil {
		t.Fatalf("mkdir fleet config dir: %v", err)
	}
	fleetJSON, _ := json.Marshal(map[string]interface{}{
		"name": groupName,
		"repos": []map[string]interface{}{
			{"path": repoPath, "slug": "myrepo"},
		},
	})
	if err := os.WriteFile(cfgPath, fleetJSON, 0o644); err != nil {
		t.Fatalf("write fleet config: %v", err)
	}

	seedID := "aaaaaaaaaaaaaaaa"
	otherID := "bbbbbbbbbbbbbbbb"

	doc := graph.Document{
		Version:        1,
		GeneratedAt:    time.Now().UTC(),
		Repo:           repoPath,
		IndexerVersion: "test",
		Entities: []graph.Entity{
			{ID: seedID, Name: "seedFn", Kind: "Function", SourceFile: "x.go", Language: "go"},
			{ID: otherID, Name: "other", Kind: "Function", SourceFile: "y.go", Language: "go"},
		},
		Relationships: []graph.Relationship{
			// Intentionally empty Kind.
			{ID: "r1", FromID: seedID, ToID: otherID, Kind: ""},
		},
	}
	doc.Stats = graph.Stats{Files: 2, Entities: 2, Relationships: 1}

	stateDir := daemon.StateDirForRepo(repoPath)
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatalf("mkdir state dir: %v", err)
	}
	docJSON, _ := json.Marshal(doc)
	if err := os.WriteFile(filepath.Join(stateDir, "graph.json"), docJSON, 0o644); err != nil {
		t.Fatalf("write graph.json: %v", err)
	}

	bundle, err := docgen.BuildBundle(context.Background(), docgen.BuildBundleOpts{
		RunOpts: docgen.RunOpts{
			Group:        groupName,
			SeedEntityID: seedID,
			Section:      "overview",
			NoCache:      true,
		},
		Tier:    0,
		NoCache: true,
	})
	if err != nil {
		t.Fatalf("BuildBundle: %v", err)
	}

	briefs := bundle.GraphContext.NeighbourBriefs
	if len(briefs) != 1 {
		t.Fatalf("expected 1 neighbour brief, got %d", len(briefs))
	}
	if briefs[0].Relationship != docgen.NeighbourRelationshipRelated {
		t.Errorf("empty-kind edge: Relationship=%q want %q",
			briefs[0].Relationship, docgen.NeighbourRelationshipRelated)
	}
}
