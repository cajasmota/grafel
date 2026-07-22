// flows_overlay_test.go — FLOW side-table read-merge (#5904 PR-b).
//
// applyFlowOverlay REPLACE-merges <stateDir>/flows.json onto a loaded group: the
// forEach* iterators suppress the baked SCOPE.Process/SCOPE.EventFlow entities
// and inject the sidecar's cross-repo-aware ones; getStepAdj rebuilds from the
// sidecar's step edges. These tests DRIVE THE REAL grafel_traces tool and prove:
//
//   - grouped/cross-repo: the flow list shows the CROSS-REPO-AWARE flow, NOT
//     doubled (exact count) and NOT intra-only — the #1 failure mode;
//   - a double-count guard that FAILS if the overlay were additive;
//   - single-repo parity: no sidecar → baked intra flow shown exactly as today;
//   - staleness fallback: a stale sidecar → baked intra flow (never empty/doubled);
//   - the mmap (flag-ON) read path applies the same REPLACE;
//   - concurrent reads vs sidecar re-publish are race-free (-race).
package mcp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/daemon"
	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/graph/fbwriter"
	"github.com/cajasmota/grafel/internal/graph/flows"
)

// bakedFlowDoc models graph.fb as the index bakes it: an INTRA-repo SCOPE.Process
// "baked-proc" (cross_stack=false) with its STEP_IN_PROCESS chain, plus the
// ordinary functions it links.
func bakedFlowDoc(repo string) *graph.Document {
	return &graph.Document{
		Version: 1, GeneratedAt: time.Now(), Repo: repo,
		Entities: []graph.Entity{
			{ID: "fn1", Name: "handleSubmit", Kind: "SCOPE.Function", SourceFile: "a.go", StartLine: 1, EndLine: 5},
			{ID: "fn2", Name: "callService", Kind: "SCOPE.Function", SourceFile: "a.go", StartLine: 6, EndLine: 9},
			graph.Entity{ID: "baked-proc", Name: "handleSubmit -> callService", Kind: "SCOPE.Process",
				SourceFile: "a.go", StartLine: 1, EndLine: 5}.WithProperties(map[string]string{
				"entry_id": "fn1", "entry_name": "handleSubmit", "terminal_id": "fn2",
				"step_count": "2", "cross_stack": "false",
				"chain_labels": "handleSubmit -> callService",
			}),
		},
		Relationships: []graph.Relationship{
			{ID: "c1", FromID: "fn1", ToID: "fn2", Kind: "CALLS"},
			graph.Relationship{ID: "s1", FromID: "baked-proc", ToID: "fn1", Kind: "STEP_IN_PROCESS"}.WithProperties(map[string]string{"step_index": "0"}),
			graph.Relationship{ID: "s2", FromID: "baked-proc", ToID: "fn2", Kind: "STEP_IN_PROCESS"}.WithProperties(map[string]string{"step_index": "1"}),
			{ID: "e1", FromID: "fn1", ToID: "baked-proc", Kind: "ENTRY_POINT_OF"},
		},
	}
}

// crossRepoFlowDelta is the phantom-pass output: a re-synthesised CROSS-REPO
// SCOPE.Process "xrepo-proc" (cross_stack=true, 3 steps into a remote endpoint)
// with its step edges + the phantom cross_repo CALLS edge. It REPLACES the baked
// intra flow at read time.
func crossRepoFlowDelta() ([]graph.Entity, []graph.Relationship) {
	ents := []graph.Entity{
		graph.Entity{ID: "xrepo-proc", Name: "handleSubmit -> callService -> remoteEndpoint", Kind: "SCOPE.Process",
			SourceFile: "a.go", StartLine: 1, EndLine: 5}.WithProperties(map[string]string{
			"entry_id": "fn1", "entry_name": "handleSubmit", "terminal_id": "remote::ep",
			"step_count": "3", "cross_stack": "true",
			"chain_labels": "handleSubmit -> callService -> remoteEndpoint",
		}),
	}
	rels := []graph.Relationship{
		graph.Relationship{ID: "xs0", FromID: "xrepo-proc", ToID: "fn1", Kind: "STEP_IN_PROCESS"}.WithProperties(map[string]string{"step_index": "0"}),
		graph.Relationship{ID: "xs1", FromID: "xrepo-proc", ToID: "fn2", Kind: "STEP_IN_PROCESS"}.WithProperties(map[string]string{"step_index": "1"}),
		graph.Relationship{ID: "xs2", FromID: "xrepo-proc", ToID: "remote::ep", Kind: "STEP_IN_PROCESS"}.WithProperties(map[string]string{"step_index": "2"}),
		graph.Relationship{ID: "ph1", FromID: "fn2", ToID: "remote::ep", Kind: "CALLS"}.WithProperties(map[string]string{
			"cross_repo": "true", "target_repo": "remote", "link_method": "http", "via": "phantom_edge_pass_#769",
		}),
	}
	return ents, rels
}

// newFlowServer registers a one-repo group "g" whose baked graph is written by
// writeGraph into the repo state dir, and returns the Server + that state dir.
func newFlowServer(t *testing.T) (*Server, string) {
	t.Helper()
	dir := t.TempDir()
	repo := filepath.Join(dir, "r1")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	writeGraph(t, repo, bakedFlowDoc("r1"))
	stateDir := daemon.StateDirForRepo(repo)
	reg := Registry{Groups: map[string]RegistryGroup{
		"g": {Repos: map[string]RegistryRepo{"r1": {Path: repo}}},
	}}
	regPath := filepath.Join(dir, "registry.json")
	d, _ := json.MarshalIndent(reg, "", "  ")
	_ = os.WriteFile(regPath, d, 0o644)
	srv, err := NewServer(Config{RegistryPath: regPath})
	if err != nil {
		t.Fatal(err)
	}
	return srv, stateDir
}

func listAll(t *testing.T, srv *Server) string {
	t.Helper()
	res := callTool(t, srv, "grafel_traces", map[string]any{"action": "list", "min_steps": 0})
	return resultText(res)
}

// TestFlowOverlay_List_ReplaceNotDoubled is the core grouped/cross-repo guard:
// with a fresh flow sidecar the list shows exactly ONE process — the cross-repo
// one — not two (double-count) and not the intra-only baked one.
func TestFlowOverlay_List_ReplaceNotDoubled(t *testing.T) {
	forceServeFromMMap(t, false)
	srv, stateDir := newFlowServer(t)
	ents, rels := crossRepoFlowDelta()
	if err := flows.Upsert(stateDir, ents, rels); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	txt := listAll(t, srv)

	if !strings.Contains(txt, `"count":1`) {
		t.Fatalf("REPLACE violated: want count=1 (cross-repo flow), got: %s", txt)
	}
	if strings.Contains(txt, "baked-proc") {
		t.Errorf("baked intra flow leaked through overlay (should be suppressed): %s", txt)
	}
	if !strings.Contains(txt, "remoteEndpoint") {
		t.Errorf("cross-repo flow missing from list: %s", txt)
	}
	// The double-count fingerprint: the baked process_id present alongside the
	// overlay one. (count=1 above already guards this; asserted explicitly here.)
	if strings.Contains(txt, `"process_id":"r1::baked-proc"`) {
		t.Errorf("DOUBLE-COUNT: baked flow present alongside overlay: %s", txt)
	}
}

// TestFlowOverlay_DoubleCountGuard fails if the overlay were made ADDITIVE: it
// asserts the baked SCOPE.Process is not present ALONGSIDE the cross-repo one.
func TestFlowOverlay_DoubleCountGuard(t *testing.T) {
	forceServeFromMMap(t, false)
	srv, stateDir := newFlowServer(t)
	ents, rels := crossRepoFlowDelta()
	if err := flows.Upsert(stateDir, ents, rels); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	txt := listAll(t, srv)
	hasBaked := strings.Contains(txt, "baked-proc")
	hasXrepo := strings.Contains(txt, "xrepo-proc")
	if hasBaked && hasXrepo {
		t.Fatalf("ADDITIVE double-count detected: both baked + overlay flows present: %s", txt)
	}
	if !hasXrepo {
		t.Fatalf("cross-repo flow missing (intra-only regression): %s", txt)
	}
}

// TestFlowOverlay_Get_CrossRepoChain: `get` on the injected cross-repo process
// returns its full 3-step chain assembled from the sidecar's STEP edges.
func TestFlowOverlay_Get_CrossRepoChain(t *testing.T) {
	forceServeFromMMap(t, false)
	srv, stateDir := newFlowServer(t)
	ents, rels := crossRepoFlowDelta()
	if err := flows.Upsert(stateDir, ents, rels); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	res := callTool(t, srv, "grafel_traces", map[string]any{"action": "get", "process_id": "xrepo-proc"})
	txt := resultText(res)
	if !strings.Contains(txt, `"found":true`) {
		t.Fatalf("cross-repo process not found via get: %s", txt)
	}
	if !strings.Contains(txt, `"cross_stack":true`) {
		t.Errorf("cross_stack not surfaced: %s", txt)
	}
	// Steps come from the sidecar's STEP_IN_PROCESS adjacency (REPLACE).
	if !strings.Contains(txt, "handleSubmit") || !strings.Contains(txt, "callService") {
		t.Errorf("cross-repo chain steps missing: %s", txt)
	}
}

// TestFlowOverlay_SingleRepoParity_NoSidecar: with NO sidecar the baked intra
// flow is shown exactly as today (parity).
func TestFlowOverlay_SingleRepoParity_NoSidecar(t *testing.T) {
	forceServeFromMMap(t, false)
	srv, _ := newFlowServer(t)
	txt := listAll(t, srv)
	if !strings.Contains(txt, `"count":1`) {
		t.Fatalf("single-repo parity: want the baked intra flow (count=1), got: %s", txt)
	}
	if !strings.Contains(txt, "baked-proc") {
		t.Errorf("single-repo parity: baked intra flow must be shown unchanged: %s", txt)
	}
}

// TestFlowOverlay_StaleFallback: a sidecar written for an older graph generation
// is stale (source-key mismatch) after a reindex and must NOT be applied — the
// baked intra flow is shown (never empty, never doubled).
func TestFlowOverlay_StaleFallback(t *testing.T) {
	forceServeFromMMap(t, false)
	srv, stateDir := newFlowServer(t)
	ents, rels := crossRepoFlowDelta()
	if err := flows.Upsert(stateDir, ents, rels); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	// Prime: the fresh overlay is applied.
	if txt := listAll(t, srv); !strings.Contains(txt, "xrepo-proc") {
		t.Fatalf("precondition: fresh overlay must be applied, got: %s", txt)
	}
	// Simulate a reindex: rewrite graph.json with a strictly-newer mtime so the
	// json: source key changes (sidecar goes stale) AND the repo reparses on
	// Reload (advancing lr.mtime → applyFlowOverlay re-evaluates the sidecar).
	future := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(filepath.Join(stateDir, "graph.json"), future, future); err != nil {
		t.Fatalf("chtimes: %v", err)
	}
	if _, err := srv.State.Reload(); err != nil {
		t.Fatalf("reload: %v", err)
	}
	txt := listAll(t, srv)
	if !strings.Contains(txt, "baked-proc") {
		t.Fatalf("stale fallback must show baked intra flow, got: %s", txt)
	}
	if strings.Contains(txt, "xrepo-proc") {
		t.Errorf("stale sidecar was applied: %s", txt)
	}
}

// TestFlowOverlay_MmapPath_Replace: the REPLACE merge also holds on the flag-ON
// mmap read path (baked flow in graph.fb, cross-repo flow in the sidecar).
func TestFlowOverlay_MmapPath_Replace(t *testing.T) {
	forceServeFromMMap(t, true)
	dir := t.TempDir()
	repo := filepath.Join(dir, "r1")
	stateDir := daemon.StateDirForRepo(repo)
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Flat graph.fb so the MCP loader opens a resident mmap Reader.
	if err := fbwriter.WriteAtomic(filepath.Join(stateDir, "graph.fb"), bakedFlowDoc("r1")); err != nil {
		t.Fatalf("write graph.fb: %v", err)
	}
	reg := Registry{Groups: map[string]RegistryGroup{"g": {Repos: map[string]RegistryRepo{"r1": {Path: repo}}}}}
	regPath := filepath.Join(dir, "registry.json")
	d, _ := json.MarshalIndent(reg, "", "  ")
	_ = os.WriteFile(regPath, d, 0o644)
	srv, err := NewServer(Config{RegistryPath: regPath})
	if err != nil {
		t.Fatal(err)
	}
	ents, rels := crossRepoFlowDelta()
	if err := flows.Upsert(stateDir, ents, rels); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	txt := listAll(t, srv)
	if !strings.Contains(txt, `"count":1`) || !strings.Contains(txt, "xrepo-proc") {
		t.Fatalf("mmap REPLACE failed: want single cross-repo flow, got: %s", txt)
	}
	if strings.Contains(txt, "baked-proc") {
		t.Errorf("mmap path leaked baked flow: %s", txt)
	}
}

// TestFlowOverlay_StepAdjReplace_Mmap proves getStepAdj is REPLACE (not ADD) on
// the flag-ON mmap path: when the sidecar is applied, the baked SCOPE.Process's
// STEP_IN_PROCESS adjacency must be GONE and only the sidecar's step edges
// present. An ADD-instead-of-REPLACE regression leaves the baked "baked-proc"
// key in the adjacency and this test fails.
func TestFlowOverlay_StepAdjReplace_Mmap(t *testing.T) {
	forceServeFromMMap(t, true)
	dir := t.TempDir()
	repo := filepath.Join(dir, "r1")
	stateDir := daemon.StateDirForRepo(repo)
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := fbwriter.WriteAtomic(filepath.Join(stateDir, "graph.fb"), bakedFlowDoc("r1")); err != nil {
		t.Fatalf("write graph.fb: %v", err)
	}
	reg := Registry{Groups: map[string]RegistryGroup{"g": {Repos: map[string]RegistryRepo{"r1": {Path: repo}}}}}
	regPath := filepath.Join(dir, "registry.json")
	d, _ := json.MarshalIndent(reg, "", "  ")
	_ = os.WriteFile(regPath, d, 0o644)
	srv, err := NewServer(Config{RegistryPath: regPath})
	if err != nil {
		t.Fatal(err)
	}
	ents, rels := crossRepoFlowDelta()
	if err := flows.Upsert(stateDir, ents, rels); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	lr := srv.State.Group("g").Repos["r1"]
	if lr == nil {
		t.Fatal("repo r1 not loaded")
	}
	if lr.Reader == nil {
		t.Fatal("precondition: flag-ON mmap requires a resident Reader")
	}
	adj := lr.getStepAdj()
	if _, present := adj["baked-proc"]; present {
		t.Errorf("REPLACE violated: baked-proc step adjacency survived (ADD instead of REPLACE): %#v", adj)
	}
	if steps := adj["xrepo-proc"]; len(steps) != 3 {
		t.Errorf("want 3 sidecar step edges for xrepo-proc, got %d: %#v", len(steps), adj["xrepo-proc"])
	}
}

// TestFlowOverlay_RaceConcurrentRebuild exercises concurrent tool reads against
// a sidecar that is re-published mid-flight (fresh mtime forces re-apply on the
// Group() serving path). Run with -race.
func TestFlowOverlay_RaceConcurrentRebuild(t *testing.T) {
	forceServeFromMMap(t, false)
	srv, stateDir := newFlowServer(t)
	ents, rels := crossRepoFlowDelta()
	if err := flows.Upsert(stateDir, ents, rels); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	var wg sync.WaitGroup
	stop := make(chan struct{})
	// Writers: keep bumping the sidecar mtime so Group() re-applies the overlay.
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					_ = flows.Upsert(stateDir, ents, rels)
					time.Sleep(time.Millisecond)
				}
			}
		}()
	}
	// Readers: drive the real tool concurrently.
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				_ = listAll(t, srv)
			}
		}()
	}
	// Let readers finish, then stop writers.
	done := make(chan struct{})
	go func() {
		time.Sleep(200 * time.Millisecond)
		close(stop)
		close(done)
	}()
	<-done
	wg.Wait()
}
