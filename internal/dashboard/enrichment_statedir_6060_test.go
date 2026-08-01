// enrichment_statedir_6060_test.go — issue #6060, writer half of the
// description side-table's reader/writer pairing.
//
// The reader (mcp.applyDescriptionOverlay via repoStateDir) resolves the sidecar
// from the directory the graph was DISCOVERED in, which under #3648's AnyRef
// fallback need not be the current-HEAD per-ref directory. The write-back
// endpoint must resolve the same way or the write is silently lost: it reports
// success, the in-memory PropSet makes it visible, and it is gone after the next
// reload with no error anywhere.
package dashboard

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/daemon"
	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/graph/descriptions"
	"github.com/cajasmota/grafel/internal/graph/fbwriter"
)

// HANDLER-level pin (#6060). The two tests below exercise enrichmentStateDir in
// isolation; this one drives the actual endpoint, which is where the fix has to
// take effect. Without it, reverting the single production line that IS the fix
// — `enrichmentStateDir(found.repoPath)` back to
// `daemon.StateDirForRepo(found.repoPath)` — leaves the whole package green.
//
// Proof the fixture can fail: that revert makes descriptions.json land in the
// current-HEAD "_unknown" dir and this test reports the sidecar missing from the
// directory the reader will look in.
func TestWriteback_PersistsSidecarIntoDiscoveredGraphDir(t *testing.T) {
	t.Setenv(daemon.EnvRoot, t.TempDir())
	repoDir := t.TempDir() // not a git checkout -> current ref is the _unknown sentinel

	currentHeadDir := daemon.StateDirForRepo(repoDir)
	indexedDir := daemon.StateDirForRepoRef(repoDir, "main")
	if currentHeadDir == indexedDir {
		t.Fatal("fixture degenerate: current-HEAD and indexed ref dirs are identical")
	}

	// The graph exists ONLY under the indexed ref, so the MCP reader resolves
	// indexedDir and the write must follow it there.
	if err := os.MkdirAll(indexedDir, 0o755); err != nil {
		t.Fatalf("mkdir indexed dir: %v", err)
	}
	const entityID = "aabbccddeeff0011"
	doc := &graph.Document{
		Version: graph.SchemaVersion,
		Entities: []graph.Entity{
			graph.Entity{
				ID: entityID, Name: "OrderCheckout", Kind: "http_endpoint",
				Language: "python", SourceFile: "api/views.py",
			}.WithProperties(map[string]string{}),
		},
	}
	if err := fbwriter.WriteAtomic(filepath.Join(indexedDir, "graph.fb"), doc); err != nil {
		t.Fatalf("write graph.fb: %v", err)
	}

	srv, err := NewServer(DefaultConfig(), newFakeStore())
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	srv.graphs.mu.Lock()
	srv.graphs.entries["g1"] = &cacheEntry{
		group: &DashGroup{
			Name:  "g1",
			Repos: map[string]*DashRepo{"repo1": {Slug: "repo1", Path: repoDir, Doc: doc}},
		},
		loadedAt: time.Now().Add(60 * time.Second),
	}
	srv.graphs.mu.Unlock()

	const desc = "Persists the order checkout request and returns a receipt."
	w := doWritebackRequest(t, srv, entityID, enrichmentWritebackRequest{Description: desc})
	if w.Code != http.StatusOK {
		t.Fatalf("write-back failed: %d %s", w.Code, w.Body.String())
	}

	// The durable assertion: the sidecar is where the reader looks.
	if sc, ok := descriptions.Read(indexedDir); !ok || sc.Results[entityID] != desc {
		stray := ""
		if _, strayOK := descriptions.Read(currentHeadDir); strayOK {
			stray = "\n  it landed in the current-HEAD dir instead: " + currentHeadDir
		}
		t.Errorf("description not persisted where the reader looks (%s)%s\n"+
			"  the next reload would find nothing and the write would be silently lost",
			indexedDir, stray)
	}
}

// enrichmentStateDir must follow the graph, not HEAD.
//
// The fixture builds the AnyRef-fallback population directly: a repo with NO
// graph under its current-HEAD ref dir (a non-git temp dir resolves to the
// "_unknown" sentinel) and a real graph.fb under an indexed "main" ref dir.
// FindGraphFileAnyRef serves the latter, so the sidecar belongs there too.
//
// Proof the fixture can fail: restoring the pre-#6060 writer
// (`daemon.StateDirForRepo(found.repoPath)`) returns the _unknown dir and this
// test fails naming both directories.
func TestEnrichmentStateDir_FollowsDiscoveredGraphNotCurrentHead(t *testing.T) {
	root := t.TempDir()
	t.Setenv(daemon.EnvRoot, root)

	repoDir := t.TempDir() // not a git checkout -> current ref is the _unknown sentinel

	currentHeadDir := daemon.StateDirForRepo(repoDir)
	indexedDir := daemon.StateDirForRepoRef(repoDir, "main")
	if currentHeadDir == indexedDir {
		t.Fatalf("fixture degenerate: current-HEAD and indexed ref dirs are both %s, "+
			"so it cannot tell the two resolutions apart", indexedDir)
	}

	// Graph exists ONLY under the indexed ref -> AnyRef fallback territory.
	if err := os.MkdirAll(indexedDir, 0o755); err != nil {
		t.Fatalf("mkdir indexed dir: %v", err)
	}
	doc := &graph.Document{Entities: []graph.Entity{{ID: "a", Name: "A", Kind: "Function"}}}
	if err := fbwriter.WriteAtomic(filepath.Join(indexedDir, "graph.fb"), doc); err != nil {
		t.Fatalf("write graph.fb: %v", err)
	}

	// Sanity: the loader really does fall back to the indexed dir. Without this
	// the test could pass for the wrong reason on a layout change.
	discovered, _ := daemon.FindGraphFileAnyRef(repoDir)
	if got := filepath.Dir(discovered); got != indexedDir {
		t.Fatalf("fixture cannot exercise the AnyRef fallback: loader discovered %q, want a graph under %s",
			discovered, indexedDir)
	}

	if got := enrichmentStateDir(repoDir); got != indexedDir {
		t.Errorf("write-back would persist the description where the reader never looks:\n"+
			"  writer resolved: %s\n  reader reads:    %s\n  (current-HEAD dir was %s)",
			got, indexedDir, currentHeadDir)
	}
}

// With no graph anywhere the writer must still resolve somewhere sane rather
// than returning "" and writing descriptions.json into the process CWD.
func TestEnrichmentStateDir_FallsBackWhenNoGraphDiscovered(t *testing.T) {
	root := t.TempDir()
	t.Setenv(daemon.EnvRoot, root)
	repoDir := t.TempDir()

	got := enrichmentStateDir(repoDir)
	if got == "" {
		t.Fatal("enrichmentStateDir returned empty for a repo with no graph")
	}
	if want := daemon.StateDirForRepo(repoDir); got != want {
		t.Errorf("fallback resolved %s, want the current-HEAD state dir %s", got, want)
	}
}
