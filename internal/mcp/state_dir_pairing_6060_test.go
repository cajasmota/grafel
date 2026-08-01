// state_dir_pairing_6060_test.go — issue #6060, reader/writer pairing.
//
// The description side-table has two halves that must name the SAME directory:
// the writer (dashboard enrichment write-back) and the reader
// (applyDescriptionOverlay). They used to resolve independently — writer via
// current-HEAD, reader via current-HEAD — which agreed by accident. Moving the
// reader onto the discovered graph file (#3648 AnyRef) without moving the writer
// breaks that agreement, and the symptom is the worst kind: a durable write that
// reports success, shows up in memory, and is gone after the next reload.
//
// TestDescriptionSidecar_SurvivesReloadUnderAnyRefFallback is the round-trip
// that a reader-only change could never pass.
package mcp

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cajasmota/grafel/internal/daemon"
	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/graph/descriptions"
	"github.com/cajasmota/grafel/internal/graph/fbwriter"
	"github.com/cajasmota/grafel/internal/graph/flows"
)

// The pairing round-trip. Builds the exact population the reader change targets
// — a repo whose graph lives under an INDEXED ref directory while HEAD points
// somewhere else — writes a description the way the dashboard write-back does,
// and requires the SERVING path to find it.
//
// Scope, stated so this is not mistaken for more than it is: the read here goes
// through State.Group -> touchGroupLocked -> applyDescriptionOverlay. That is
// the per-call overlay refresh, NOT a reload — no rediscovery happens, and the
// group's resident Doc is the one seeded on disk. Coverage of discovery
// re-resolving after a HEAD move lives in
// graph_discovery_staleness_6060_test.go.
//
// Proof the fixture can exhibit the failure: writing the sidecar into
// currentHeadDir instead of indexedDir (the pre-#6060 writer) makes the final
// read return "", which is the silently-lost-write bug.
func TestDescriptionSidecar_SurvivesReloadUnderAnyRefFallback(t *testing.T) {
	doc := lazyTestDoc()
	st, lr, _ := seedRepoOnDisk(t, doc)

	currentHeadDir := daemon.StateDirForRepo(lr.Path)

	// The AnyRef fallback population: the graph the loader actually serves lives
	// in a DIFFERENT per-ref directory than the current-HEAD one.
	indexedDir := t.TempDir()
	indexedFB := filepath.Join(indexedDir, "graph.fb")
	if err := fbwriter.WriteAtomic(indexedFB, doc); err != nil {
		t.Fatalf("write indexed-ref graph: %v", err)
	}
	if indexedDir == currentHeadDir {
		t.Fatal("fixture degenerate: indexed and current-HEAD dirs are the same, " +
			"so it cannot tell the two resolutions apart")
	}

	st.mu.Lock()
	lr.GraphFile = indexedFB // what FindGraphFileAnyRef would have discovered
	lr.descApplied = false
	st.mu.Unlock()

	// Write the description through the same call the dashboard write-back makes,
	// into the directory the WRITER resolves. enrichmentStateDir (dashboard) and
	// repoStateDir (here) must both land on indexedDir.
	if err := descriptions.Upsert(indexedDir, "a", "durable description"); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	// Read it back off the serving path — the durable sidecar, not an in-memory
	// PropSet left behind by a writer.
	if grp := st.Group("test"); grp == nil {
		t.Fatal("Group returned nil")
	}

	got := ""
	for i := range lr.Doc.Entities {
		if lr.Doc.Entities[i].ID == "a" {
			got = lr.Doc.Entities[i].PropGet("description")
		}
	}
	if got != "durable description" {
		t.Errorf("description not visible on the serving path: got %q want %q\n"+
			"  reader resolved a different directory than the writer\n"+
			"  indexed(graph) dir: %s\n  current-HEAD dir:   %s",
			got, "durable description", indexedDir, currentHeadDir)
	}
}

// The reader must not consult a sidecar sitting in the current-HEAD directory
// when the graph it loaded came from elsewhere. This is the other direction of
// the same pairing: reading the wrong ref's descriptions is how a stale or
// foreign description gets stamped onto entities.
func TestDescriptionSidecar_IgnoresCurrentHeadDirWhenGraphIsElsewhere(t *testing.T) {
	doc := lazyTestDoc()
	st, lr, _ := seedRepoOnDisk(t, doc)

	currentHeadDir := daemon.StateDirForRepo(lr.Path)
	indexedDir := t.TempDir()
	indexedFB := filepath.Join(indexedDir, "graph.fb")
	if err := fbwriter.WriteAtomic(indexedFB, doc); err != nil {
		t.Fatalf("write indexed-ref graph: %v", err)
	}

	// A sidecar belonging to the OTHER ref.
	if err := descriptions.Upsert(currentHeadDir, "a", "wrong-ref description"); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	st.mu.Lock()
	lr.GraphFile = indexedFB
	lr.descApplied = false
	st.mu.Unlock()

	if grp := st.Group("test"); grp == nil {
		t.Fatal("Group returned nil")
	}
	for i := range lr.Doc.Entities {
		if lr.Doc.Entities[i].ID == "a" {
			if d := lr.Doc.Entities[i].PropGet("description"); d == "wrong-ref description" {
				t.Error("reader stamped a description from the current-HEAD directory " +
					"onto a graph loaded from a different ref")
			}
		}
	}
}

// The FLOW side-table is deliberately NOT converted to the discovered-graph dir
// (its writer, the phantom-edge pass, is current-HEAD on both its load and its
// write — see applyFlowOverlay). This pins that pairing behaviourally, so the
// half-conversion this issue is about cannot be reintroduced silently: before
// this test, reverting applyFlowOverlay's state dir failed nothing in the whole
// package.
//
// Proof the fixture can fail: pointing applyFlowOverlay at repoStateDir(lr)
// (the description convention) makes it read the discovered dir instead and the
// overlay comes back nil.
func TestFlowSidecar_ReadFromCurrentHeadDirMatchingItsWriter(t *testing.T) {
	doc := lazyTestDoc()
	st, lr, _ := seedRepoOnDisk(t, doc)

	writerDir := daemon.StateDirForRepo(lr.Path) // what the phantom pass writes to

	// Put the graph somewhere else so the two conventions are distinguishable —
	// exactly the AnyRef shape the description overlay follows.
	discoveredDir := t.TempDir()
	discoveredFB := filepath.Join(discoveredDir, "graph.fb")
	if err := fbwriter.WriteAtomic(discoveredFB, doc); err != nil {
		t.Fatalf("write discovered graph: %v", err)
	}
	if discoveredDir == writerDir {
		t.Fatal("fixture degenerate: writer dir and discovered dir are identical")
	}

	// A flow sidecar written the way the phantom pass writes it.
	ents := []graph.Entity{{ID: "flow::x", Name: "X", Kind: "process_flow"}}
	rels := []graph.Relationship{{FromID: "flow::x", ToID: "a", Kind: stepInProcessEdge}}
	if err := flows.Upsert(writerDir, ents, rels); err != nil {
		t.Fatalf("flows.Upsert: %v", err)
	}

	st.mu.Lock()
	lr.GraphFile = discoveredFB
	lr.flowApplied = false
	st.mu.Unlock()

	if grp := st.Group("test"); grp == nil {
		t.Fatal("Group returned nil")
	}
	if fo := lr.flowOverlaySnapshot(); fo == nil {
		t.Errorf("flow overlay not applied: the reader looked somewhere other than\n"+
			"its writer's directory (%s), so the phantom pass's flows are invisible", writerDir)
	}
}

// The mmap read path. serveFromMMap() defaults ON for this OS, and on that path
// descriptions are NOT read off lr.Doc.Entities — they come from the
// entity-INDEX-keyed LabelIndex.descOverlay table that applyDescriptionOverlay
// builds from the resident Reader. The two tests above use a hand-built Doc with
// a nil Reader, so they only cover the flag-OFF path. This one drives a REAL
// reload (Reader opened, index built from the mmap) with the flag forced ON, so
// production's actual read path is what consumes the resolved sidecar directory.
func TestDescriptionSidecar_MMapPathReadsDiscoveredDir(t *testing.T) {
	forceServeFromMMap(t, true)

	t.Setenv(daemon.EnvRoot, t.TempDir())
	repoDir := t.TempDir()

	// AnyRef layout: the graph lives under an indexed ref, not current-HEAD.
	indexedDir := daemon.StateDirForRepoRef(repoDir, "main")
	if err := os.MkdirAll(indexedDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := fbwriter.WriteAtomic(filepath.Join(indexedDir, "graph.fb"), lazyTestDoc()); err != nil {
		t.Fatalf("write graph.fb: %v", err)
	}
	if daemon.StateDirForRepo(repoDir) == indexedDir {
		t.Fatal("fixture degenerate: current-HEAD and indexed dirs are identical")
	}

	const want = "described via the mmap read path"
	if err := descriptions.Upsert(indexedDir, "a", want); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	reg := &Registry{Groups: map[string]RegistryGroup{
		"test": {Repos: map[string]RegistryRepo{"r": {Path: repoDir}}},
	}}
	st := NewState(reg)
	t.Cleanup(st.Close)
	if _, _, err := st.reloadLocked(); err != nil {
		t.Fatalf("reload: %v", err)
	}

	grp := st.Group("test")
	if grp == nil {
		t.Fatal("Group returned nil")
	}
	lr := grp.Repos["r"]
	if lr == nil || lr.Reader == nil {
		t.Fatal("fixture cannot exercise the mmap path: no Reader was opened")
	}
	if lr.LabelIndex == nil {
		t.Fatal("no LabelIndex built")
	}

	lr.rmu().Lock()
	table := lr.LabelIndex.descOverlay
	lr.rmu().Unlock()

	found := false
	for _, d := range table {
		if d == want {
			found = true
		}
	}
	if !found {
		t.Errorf("mmap descOverlay table does not carry the sidecar description\n"+
			"  table size: %d\n  sidecar dir the reader must use: %s", len(table), indexedDir)
	}
}
