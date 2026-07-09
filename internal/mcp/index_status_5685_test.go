package mcp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/indexstate"
)

// TestIndexStatusNoGroupResolvesError verifies grafel_index_status gates on
// group resolution exactly like grafel_orient (#5685). With no cwd/group and a
// registry that has multiple groups (so the group is genuinely ambiguous), the
// tool must return the resolveGroup error VERBATIM ("...pass `group=<name>`...")
// instead of a silent, empty {"repos":[]} that reads as "nothing indexed".
func TestIndexStatusNoGroupResolvesError(t *testing.T) {
	t.Cleanup(func() { indexstate.SetRepoStates(nil) })

	dir := t.TempDir()
	rAlpha := filepath.Join(dir, "alpha")
	rBeta := filepath.Join(dir, "beta")
	_ = os.MkdirAll(rAlpha, 0o755)
	_ = os.MkdirAll(rBeta, 0o755)
	writeGraph(t, rAlpha, fixtureDoc("alpha"))
	writeGraph(t, rBeta, fixtureDoc("beta"))
	// Two distinct groups → no singleton fallback; an out-of-tree cwd cannot
	// resolve → resolveGroup returns the "ambiguous group; pass `group=`" error.
	regPath := makeRegistry(t, dir, map[string]map[string]string{
		"g1": {"alpha": rAlpha},
		"g2": {"beta": rBeta},
	})
	srv, err := NewServer(Config{RegistryPath: regPath})
	if err != nil {
		t.Fatal(err)
	}

	// Populate the scheduler snapshot so that WITHOUT the fix the handler would
	// happily return a non-error repos array — making the assertion below a true
	// red→green signal rather than a coincidence of an empty snapshot.
	indexstate.SetRepoStates([]indexstate.RepoState{
		{Path: rAlpha, State: indexstate.StateCurrent, IndexedRef: "i-alpha"},
		{Path: rBeta, State: indexstate.StateCurrent, IndexedRef: "i-beta"},
	})

	// cwd outside every registered repo → the group cannot be inferred from cwd.
	out := filepath.Join(t.TempDir(), "elsewhere")
	_ = os.MkdirAll(out, 0o755)
	res := callTool(t, srv, "grafel_index_status", map[string]any{"cwd": out})

	if res == nil || !res.IsError {
		t.Fatalf("want an error result when no group resolves, got: %s", resultText(res))
	}
	msg := resultText(res)
	if !strings.Contains(msg, "pass `group=") {
		t.Fatalf("error message should tell the caller to pass group=, got: %q", msg)
	}
}

// TestIndexStatusExplicitGroupStillReturnsRepos verifies the success path is
// unchanged: an explicit group= resolves cleanly and the per-repo rows are
// returned exactly as before (#5685).
func TestIndexStatusExplicitGroupStillReturnsRepos(t *testing.T) {
	t.Cleanup(func() { indexstate.SetRepoStates(nil) })

	dir := t.TempDir()
	rAlpha := filepath.Join(dir, "alpha")
	rBeta := filepath.Join(dir, "beta")
	_ = os.MkdirAll(rAlpha, 0o755)
	_ = os.MkdirAll(rBeta, 0o755)
	writeGraph(t, rAlpha, fixtureDoc("alpha"))
	writeGraph(t, rBeta, fixtureDoc("beta"))
	regPath := makeRegistry(t, dir, map[string]map[string]string{
		"g1": {"alpha": rAlpha},
		"g2": {"beta": rBeta},
	})
	srv, err := NewServer(Config{RegistryPath: regPath})
	if err != nil {
		t.Fatal(err)
	}
	indexstate.SetRepoStates([]indexstate.RepoState{
		{Path: rAlpha, State: indexstate.StateCurrent, IndexedRef: "i-alpha"},
		{Path: rBeta, State: indexstate.StateCurrent, IndexedRef: "i-beta"},
	})

	// Explicit group= resolves via resolveGroup's explicit branch → no error,
	// and the existing group filter narrows the rows to that group.
	res := callTool(t, srv, "grafel_index_status", map[string]any{"group": "g1"})
	if res == nil || res.IsError {
		t.Fatalf("explicit group= must not error, got: %s", resultText(res))
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(resultText(res)), &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	repos, _ := m["repos"].([]any)
	if len(repos) != 1 {
		t.Fatalf("group=g1 want 1 row, got %d: %v", len(repos), m["repos"])
	}
	if repos[0].(map[string]any)["repo"] != rAlpha {
		t.Fatalf("group=g1 returned wrong repo: %v", repos[0])
	}
}
