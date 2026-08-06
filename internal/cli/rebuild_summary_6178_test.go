// Regression test for issue #6178 (rebuild-summary read-side twin):
// loadCrossRepoEdgeCount resolved via daemon.DefaultLayout().Root, which
// honors GRAFEL_DAEMON_ROOT (a DIFFERENT override governing the daemon's
// own runtime layout) and otherwise falls back to plain
// os.UserHomeDir()+".grafel" — never consulting GRAFEL_HOME. Under an
// isolated GRAFEL_HOME, `grafel rebuild`'s own post-rebuild summary would
// read the wrong (or no) links file and silently print
// cross_repo_edges: 0, even though the link pass in the SAME rebuild
// invocation just wrote its output under GRAFEL_HOME.
package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadCrossRepoEdgeCount_HonorsGRAFELHomeEnv_6178(t *testing.T) {
	sandboxHome := t.TempDir()
	grafelHome := t.TempDir()
	t.Setenv("HOME", sandboxHome)
	t.Setenv("USERPROFILE", sandboxHome) // os.UserHomeDir() reads this on Windows
	t.Setenv("GRAFEL_HOME", grafelHome)
	// Explicitly unset so this test also proves loadCrossRepoEdgeCount is
	// keyed on GRAFEL_HOME, not GRAFEL_DAEMON_ROOT.
	t.Setenv("GRAFEL_DAEMON_ROOT", "")

	const group = "g6178-rebuild-summary"

	// Correct location: <GRAFEL_HOME>/groups/<group>-links.json, with 2
	// link entries.
	correctDir := filepath.Join(grafelHome, "groups")
	if err := os.MkdirAll(correctDir, 0o755); err != nil {
		t.Fatal(err)
	}
	correctDoc := struct {
		Links []json.RawMessage `json:"links"`
	}{Links: []json.RawMessage{json.RawMessage(`{"a":1}`), json.RawMessage(`{"a":2}`)}}
	correctBytes, err := json.Marshal(correctDoc)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(correctDir, group+"-links.json"), correctBytes, 0o644); err != nil {
		t.Fatal(err)
	}

	// Decoy at the old (HOME-derived / GRAFEL_DAEMON_ROOT-agnostic)
	// fallback location with a DIFFERENT count, so a regression is caught
	// by a wrong number, not just a crash.
	decoyDir := filepath.Join(sandboxHome, ".grafel", "groups")
	if err := os.MkdirAll(decoyDir, 0o755); err != nil {
		t.Fatal(err)
	}
	decoyDoc := struct {
		Links []json.RawMessage `json:"links"`
	}{Links: []json.RawMessage{json.RawMessage(`{"a":1}`)}}
	decoyBytes, err := json.Marshal(decoyDoc)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(decoyDir, group+"-links.json"), decoyBytes, 0o644); err != nil {
		t.Fatal(err)
	}

	got := loadCrossRepoEdgeCount(group)
	if got != 2 {
		t.Fatalf("loadCrossRepoEdgeCount = %d, want 2 (the GRAFEL_HOME links, not the %d-entry decoy)", got, 1)
	}
}
