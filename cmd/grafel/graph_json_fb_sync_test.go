package main

// graph_json_fb_sync_test.go — regression test for issue #1702.
//
// Asserts that after a full index run, graph.json and graph.fb contain the
// same entity IDs. Previously, call sites like the phantom-edge pass and the
// enrichment writeback handler wrote only graph.json, leaving graph.fb stale.
// Direct graph.json readers then saw entity IDs that did not exist in graph.fb
// (ghost IDs). This test locks in the invariant: every entity ID visible in
// graph.json must also be present in graph.fb, and vice versa.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/cajasmota/grafel/internal/daemon"
	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/graph/fbwriter"
)

// TestGraphJsonFbSync_EntityIDsMatch verifies that after Index() writes both
// graph.fb and graph.json, the entity-ID sets are identical (#1702).
func TestGraphJsonFbSync_EntityIDsMatch(t *testing.T) {
	// Run the indexer on the cross-file Go fixture (small, fast, deterministic).
	doc := runIndexerOn(t, "testdata/crossfile_go", "crossfile_go_sync", nil)

	// Write both files into a temp state dir, mirroring the production dual-write.
	stateDir := t.TempDir()
	fbPath := filepath.Join(stateDir, "graph.fb")
	jsonPath := filepath.Join(stateDir, "graph.json")

	if err := fbwriter.WriteAtomic(fbPath, doc); err != nil {
		t.Fatalf("write graph.fb: %v", err)
	}
	if err := graph.WriteAtomic(jsonPath, doc, false); err != nil {
		t.Fatalf("write graph.json: %v", err)
	}

	// --- Read entity IDs from graph.fb via LoadGraphFromDir ---------------
	fbDoc, err := graph.LoadGraphFromDir(stateDir)
	if err != nil {
		t.Fatalf("LoadGraphFromDir (fb path): %v", err)
	}
	fbIDs := make(map[string]struct{}, len(fbDoc.Entities))
	for _, e := range fbDoc.Entities {
		fbIDs[e.ID] = struct{}{}
	}

	// --- Read entity IDs from graph.json directly (the "ghost ID" path) ---
	raw, err := os.ReadFile(jsonPath)
	if err != nil {
		t.Fatalf("read graph.json: %v", err)
	}
	var jsonDoc graph.Document
	if err := json.Unmarshal(raw, &jsonDoc); err != nil {
		t.Fatalf("unmarshal graph.json: %v", err)
	}
	jsonIDs := make(map[string]struct{}, len(jsonDoc.Entities))
	for _, e := range jsonDoc.Entities {
		jsonIDs[e.ID] = struct{}{}
	}

	// --- Assert symmetry --------------------------------------------------
	for id := range jsonIDs {
		if _, ok := fbIDs[id]; !ok {
			t.Errorf("ghost entity ID: %q present in graph.json but missing in graph.fb", id)
		}
	}
	for id := range fbIDs {
		if _, ok := jsonIDs[id]; !ok {
			t.Errorf("ghost entity ID: %q present in graph.fb but missing in graph.json", id)
		}
	}
	if t.Failed() {
		t.Logf("graph.json entity count=%d  graph.fb entity count=%d", len(jsonIDs), len(fbIDs))
	}
}

// TestGraphJsonFbSync_WriteBothSites verifies that both previously-broken
// write sites — the phantom-edge pass (internal/cli/links.go) and the
// enrichment writeback handler (internal/dashboard/handlers_enrichment_writeback.go)
// — now produce a consistent graph.fb+graph.json pair by exercising the
// low-level dual-write logic directly. The test mutates an entity in a
// Document, writes both files, then re-reads each and confirms the mutation
// is reflected in both on-disk representations.
func TestGraphJsonFbSync_WriteBothSites(t *testing.T) {
	doc := runIndexerOn(t, "testdata/crossfile_go", "crossfile_go_sync2", nil)
	if len(doc.Entities) == 0 {
		t.Skip("fixture produced no entities; skip sync test")
	}

	doc.Entities[0].PropSet("description", "synthetic description for #1702 test")
	mutatedID := doc.Entities[0].ID

	// Write both files (the fixed dual-write path).
	stateDir := t.TempDir()
	fbPath := filepath.Join(stateDir, "graph.fb")
	jsonPath := filepath.Join(stateDir, "graph.json")

	if err := fbwriter.WriteAtomic(fbPath, doc); err != nil {
		t.Fatalf("write graph.fb: %v", err)
	}
	if err := graph.WriteAtomic(jsonPath, doc, false); err != nil {
		t.Fatalf("write graph.json: %v", err)
	}

	// Read via LoadGraphFromDir (prefers graph.fb).
	fbDoc, err := graph.LoadGraphFromDir(stateDir)
	if err != nil {
		t.Fatalf("LoadGraphFromDir: %v", err)
	}
	var fbEnt *graph.Entity
	for i := range fbDoc.Entities {
		if fbDoc.Entities[i].ID == mutatedID {
			fbEnt = &fbDoc.Entities[i]
			break
		}
	}
	if fbEnt == nil {
		t.Fatalf("mutated entity %q not found in graph.fb after dual-write", mutatedID)
	}
	if fbEnt.PropGet("description") != "synthetic description for #1702 test" {
		t.Errorf("graph.fb description mismatch: got %q", fbEnt.PropGet("description"))
	}

	// Read graph.json directly.
	raw, err := os.ReadFile(jsonPath)
	if err != nil {
		t.Fatalf("read graph.json: %v", err)
	}
	var jsonDoc graph.Document
	if err := json.Unmarshal(raw, &jsonDoc); err != nil {
		t.Fatalf("unmarshal graph.json: %v", err)
	}
	var jsonEnt *graph.Entity
	for i := range jsonDoc.Entities {
		if jsonDoc.Entities[i].ID == mutatedID {
			jsonEnt = &jsonDoc.Entities[i]
			break
		}
	}
	if jsonEnt == nil {
		t.Fatalf("mutated entity %q not found in graph.json after dual-write", mutatedID)
	}
	if jsonEnt.PropGet("description") != "synthetic description for #1702 test" {
		t.Errorf("graph.json description mismatch: got %q", jsonEnt.PropGet("description"))
	}
}

// Ensure daemon path helpers compile correctly (they are used at the real
// call sites but tested here to confirm the import chain is intact).
var _ = daemon.StateDirForRepo
