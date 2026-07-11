package mcp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/cajasmota/grafel/internal/daemon"
	"github.com/cajasmota/grafel/internal/indexstate"
)

// TestIndexStatusSerializationAndFilter verifies grafel_index_status reads the
// per-repo scheduler snapshot (via indexstate), attaches the group from the
// registry, serializes the {repo,group,state,indexed_ref,head_ref,dirty} shape,
// honours the repo= substring filter, and sets any_indexing (#5433).
func TestIndexStatusSerializationAndFilter(t *testing.T) {
	t.Cleanup(func() { indexstate.SetRepoStates(nil) })

	dir := t.TempDir()
	setTestHome(t, filepath.Join(dir, "home"))
	rAlpha := filepath.Join(dir, "alpha")
	rBeta := filepath.Join(dir, "beta")
	_ = os.MkdirAll(rAlpha, 0o755)
	_ = os.MkdirAll(rBeta, 0o755)
	writeGraph(t, rAlpha, fixtureDoc("alpha"))
	writeGraph(t, rBeta, fixtureDoc("beta"))
	regPath := makeRegistry(t, dir, map[string]map[string]string{
		"g": {"alpha": rAlpha, "beta": rBeta},
	})
	srv, err := NewServer(Config{RegistryPath: regPath})
	if err != nil {
		t.Fatal(err)
	}

	indexstate.SetRepoStates([]indexstate.RepoState{
		{Path: rAlpha, State: indexstate.StateIndexing, HeadRef: "h-alpha", IndexedRef: "i-alpha"},
		{Path: rBeta, State: indexstate.StateCurrent, IndexedRef: "i-beta"},
	})
	// #5729 PR3: grafel_index_status now reads the status-plane sidecar, not
	// indexstate.RepoStates() directly — write the sidecar the production
	// statusWriter goroutine would produce from the same indexstate we just set.
	daemon.WriteRepoStatusFileForTest(rAlpha)
	daemon.WriteRepoStatusFileForTest(rBeta)

	parse := func(args map[string]any) map[string]any {
		t.Helper()
		res := callTool(t, srv, "grafel_index_status", args)
		var m map[string]any
		if err := json.Unmarshal([]byte(resultText(res)), &m); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		return m
	}

	// No filter: both repos, any_indexing=true (alpha is indexing).
	all := parse(nil)
	repos, _ := all["repos"].([]any)
	if len(repos) != 2 {
		t.Fatalf("want 2 repos, got %d: %v", len(repos), all["repos"])
	}
	if v, _ := all["any_indexing"].(bool); !v {
		t.Fatalf("any_indexing = %v, want true", all["any_indexing"])
	}
	// Verify the alpha row shape including group attribution.
	var foundAlpha bool
	for _, r := range repos {
		row := r.(map[string]any)
		if row["repo"] == rAlpha {
			foundAlpha = true
			if row["group"] != "g" {
				t.Errorf("alpha group = %v, want g", row["group"])
			}
			if row["state"] != indexstate.StateIndexing {
				t.Errorf("alpha state = %v, want indexing", row["state"])
			}
			if row["head_ref"] != "h-alpha" || row["indexed_ref"] != "i-alpha" {
				t.Errorf("alpha refs = head:%v indexed:%v", row["head_ref"], row["indexed_ref"])
			}
		}
	}
	if !foundAlpha {
		t.Fatal("alpha row missing")
	}

	// repo= substring filter narrows to beta only; any_indexing=false now.
	beta := parse(map[string]any{"repo": "beta"})
	bRepos, _ := beta["repos"].([]any)
	if len(bRepos) != 1 {
		t.Fatalf("repo=beta want 1 row, got %d", len(bRepos))
	}
	if bRepos[0].(map[string]any)["repo"] != rBeta {
		t.Fatalf("repo=beta returned wrong repo: %v", bRepos[0])
	}
	if v, _ := beta["any_indexing"].(bool); v {
		t.Fatalf("repo=beta any_indexing = true, want false (beta is current)")
	}

	// group filter that matches nothing → empty.
	none := parse(map[string]any{"group": "nope"})
	nRepos, _ := none["repos"].([]any)
	if len(nRepos) != 0 {
		t.Fatalf("group=nope want 0 rows, got %d", len(nRepos))
	}
}
