package mcp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/cajasmota/grafel/internal/daemon"
	"github.com/cajasmota/grafel/internal/indexstate"
)

// TestIndexStatusDiskFallback verifies grafel_index_status no longer reports
// repos:[] for a repo that has a fully materialized graph on disk but no
// LIVE indexstate entry — the situation that arises when a repo was indexed
// by a PREVIOUS daemon lifetime, or via `grafel rebuild` (which bypasses
// Scheduler.runIndex and therefore never populates indexstate) (#5710).
//
// Three repos exercise the three cases in one call:
//   - alpha: HAS a live indexstate entry (state=indexing) → must report the
//     LIVE state verbatim; the disk fallback must never mask an in-flight repo.
//   - beta: graph.json on disk, NO live indexstate entry → must synthesize a
//     `current` row from disk (the bug: previously this repo vanished).
//   - gamma: registered, NEITHER a live entry NOR a graph on disk → must
//     still be absent (genuinely never-indexed; no row is fabricated).
func TestIndexStatusDiskFallback(t *testing.T) {
	t.Cleanup(func() { indexstate.SetRepoStates(nil) })

	dir := t.TempDir()
	setTestHome(t, filepath.Join(dir, "home"))
	rAlpha := filepath.Join(dir, "alpha")
	rBeta := filepath.Join(dir, "beta")
	rGamma := filepath.Join(dir, "gamma")
	_ = os.MkdirAll(rAlpha, 0o755)
	_ = os.MkdirAll(rBeta, 0o755)
	_ = os.MkdirAll(rGamma, 0o755)

	// alpha and beta both have a materialized graph on disk. gamma does not.
	// beta is written as graph.fb carrying an IndexedRef in its header so the
	// test asserts the disk fallback reads that ref via the CHEAP header path
	// (fbreader.LoadGraphMeta) — no full entity/relationship decode (#5710).
	writeGraph(t, rAlpha, fixtureDoc("alpha"))
	betaDoc := fixtureDoc("beta")
	betaDoc.IndexedRef = "main"
	writeGraphFB(t, rBeta, betaDoc)

	regPath := makeRegistry(t, dir, map[string]map[string]string{
		"g": {"alpha": rAlpha, "beta": rBeta, "gamma": rGamma},
	})
	srv, err := NewServer(Config{RegistryPath: regPath})
	if err != nil {
		t.Fatal(err)
	}

	// Only alpha has a LIVE indexstate entry. beta and gamma have none — beta
	// must be filled in from disk, gamma must stay absent.
	indexstate.SetRepoStates([]indexstate.RepoState{
		{Path: rAlpha, State: indexstate.StateIndexing, HeadRef: "h-alpha", IndexedRef: "i-alpha"},
	})
	// #5729 PR3: only alpha has live indexstate — write ONLY its statusfile so
	// beta continues to exercise the disk-only fallback path (no statusfile
	// exists for beta, exactly like a repo indexed by `grafel rebuild` or a
	// prior daemon lifetime).
	daemon.WriteRepoStatusFileForTest(rAlpha)

	res := callTool(t, srv, "grafel_index_status", map[string]any{"group": "g"})
	if res == nil || res.IsError {
		t.Fatalf("grafel_index_status errored: %s", resultText(res))
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(resultText(res)), &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	repos, _ := m["repos"].([]any)

	byPath := map[string]map[string]any{}
	for _, r := range repos {
		row := r.(map[string]any)
		byPath[row["repo"].(string)] = row
	}

	// alpha: live state must win, unmasked by the disk fallback.
	aRow, ok := byPath[rAlpha]
	if !ok {
		t.Fatalf("alpha row missing entirely: %v", repos)
	}
	if aRow["state"] != indexstate.StateIndexing {
		t.Errorf("alpha state = %v, want indexing (live state must not be masked by disk fallback)", aRow["state"])
	}
	if aRow["head_ref"] != "h-alpha" || aRow["indexed_ref"] != "i-alpha" {
		t.Errorf("alpha refs = head:%v indexed:%v, want h-alpha/i-alpha", aRow["head_ref"], aRow["indexed_ref"])
	}

	// beta: THE BUG — no live entry, but a graph exists on disk. Must be
	// synthesized as a `current` row, not silently dropped.
	bRow, ok := byPath[rBeta]
	if !ok {
		t.Fatalf("beta row missing — disk-backed fallback did not fire (repos: %v)", repos)
	}
	if bRow["state"] != indexstate.StateCurrent {
		t.Errorf("beta state = %v, want current", bRow["state"])
	}
	// The IndexedRef must be read from the graph.fb header via the cheap path.
	if bRow["indexed_ref"] != "main" {
		t.Errorf("beta indexed_ref = %v, want main (from fb header, cheap read)", bRow["indexed_ref"])
	}

	// gamma: no live entry AND nothing on disk → must remain absent.
	if _, ok := byPath[rGamma]; ok {
		t.Fatalf("gamma row present but repo was never indexed on disk or live: %v", byPath[rGamma])
	}

	// any_indexing must still be true (alpha is indexing) — the disk-only
	// beta row must not itself mark any_indexing, but must not suppress it
	// either.
	if v, _ := m["any_indexing"].(bool); !v {
		t.Fatalf("any_indexing = %v, want true (alpha is indexing)", m["any_indexing"])
	}
}
