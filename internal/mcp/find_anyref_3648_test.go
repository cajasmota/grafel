// find_anyref_3648_test.go reproduces and guards the #3648 fix: archigraph_find
// (and every other repo-scoped tool) must work on a group whose graph was
// indexed at a ref that is no longer HEAD — the failure mode for groups
// registered via `archigraph group add --index` with watchers off, which index
// once at the then-HEAD ref and never reindex when HEAD subsequently moves.
package mcp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cajasmota/archigraph/internal/daemon"
)

// writeGraphAtRef writes doc into the per-ref state dir for (repoDir, ref),
// rather than the current-HEAD ref dir writeGraph targets. This simulates an
// indexed graph stranded under a ref that is no longer HEAD.
func writeGraphAtRef(t *testing.T, repoDir, ref string, doc *struct{ raw []byte }) string {
	t.Helper()
	dir := daemon.StateDirForRepoRef(repoDir, ref)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "graph.json")
	if err := os.WriteFile(path, doc.raw, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestFind_AnyRefFallback_NoReposLoadedRegression is the end-to-end guard. A
// repo registered the `group add --index` way has its graph under a non-HEAD
// ref dir; find must still resolve it and return a hit, not the
// "# no repos loaded for this group" sentinel.
func TestFind_AnyRefFallback_NoReposLoadedRegression(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ARCHIGRAPH_DAEMON_ROOT", t.TempDir())

	repoPath := filepath.Join(dir, "scriptable-repo")
	if err := os.MkdirAll(repoPath, 0o755); err != nil {
		t.Fatal(err)
	}

	// Serialize the shared fixture and stash it ONLY under a non-HEAD ref dir.
	// repoPath is a plain (non-git) dir, so the MCP server's current-ref
	// resolution lands on the "_unknown" ref dir — which we leave empty.
	raw, err := json.MarshalIndent(fixtureDoc("scriptable-repo"), "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	writeGraphAtRef(t, repoPath, "feature/indexed-long-ago", &struct{ raw []byte }{raw})

	// Sanity: nothing under the current (HEAD) ref dir.
	if p, _ := daemon.FindGraphFile(repoPath); p != "" {
		t.Fatalf("precondition: current-ref graph should be empty, got %q", p)
	}

	regPath := makeRegistry(t, dir, map[string]map[string]string{
		"scriptable": {"scriptable-repo": repoPath},
	})
	srv, err := NewServer(Config{RegistryPath: regPath})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	res := callTool(t, srv, "archigraph_find", map[string]any{
		"group": "scriptable",
		"query": "rareUniqueWidget",
	})
	out := resultText(res)
	if strings.Contains(out, "no repos loaded") {
		t.Fatalf("find returned the #3648 sentinel; AnyRef fallback did not engage:\n%s", out)
	}
	if !strings.Contains(out, "rareUniqueWidget") {
		t.Fatalf("find did not surface the indexed entity; got:\n%s", out)
	}
}
