package mcp

import (
	"context"
	"strings"
	"testing"

	mcpapi "github.com/mark3labs/mcp-go/mcp"
)

// Issue #6010 follow-up: coldShellRepo drops loadErr (and the sibling
// reindexRequired/reindexReason). A repo whose graph failed to load is reported
// as unavailable WITH its reason before an eviction; after a keepReader
// evict -> revive cycle the LoadedRepo is rebuilt from the cold shell, which
// never carried loadErr, so both real consumers degrade:
//
//   - handleGraphStats (tools.go) renders totals.unavailable entries as
//     name + ": " + r.loadErr -> "broken: " with nothing after the colon.
//   - handleDiagnostics (dashboard_tools.go) sets LoadError: lr.loadErr, which
//     is `json:"load_error,omitempty"` -> the field vanishes from the payload.
//
// This is a BEHAVIOURAL test: it drives the real revive path
// (State.Group -> reviveEvictedLocked -> rematerializeFromReader) and asserts
// on what the two user-facing tool handlers actually emit.
//
// MUTATION ORACLE: delete the `loadErr: lr.loadErr` line from coldShellRepo and
// both assertions below red again.
func TestEvictRevive_PreservesLoadErrorForConsumers_6010(t *testing.T) {
	const wantErr = "read graph.fb: unexpected EOF"

	// Isolate the status-plane scan handleGraphStats performs: without this it
	// walks the developer's real $GRAFEL_HOME (seconds of I/O, and results that
	// vary per machine). Nothing this test asserts on comes from there.
	t.Setenv("GRAFEL_HOME", t.TempDir())

	reg := &Registry{Groups: map[string]RegistryGroup{
		"test": {Repos: map[string]RegistryRepo{"broken": {Path: t.TempDir()}}},
	}}
	st := NewState(reg)
	st.mu.Lock()
	st.groups["test"] = &LoadedGroup{Name: "test", Repos: map[string]*LoadedRepo{
		// Exactly the shape reloadLocked leaves behind when readDocumentFromDir
		// fails: no Doc, no Reader, a recorded load error (+ the version-skew
		// observability pair, which travels with the same failed reload).
		"broken": {
			Repo:            "broken",
			loadErr:         wantErr,
			reindexRequired: true,
			reindexReason:   "graph.fb format v3 < required v5",
		},
	}}
	st.mu.Unlock()
	srv := &Server{State: st, Tel: NewTelemetry(0)}

	// Baseline: before any eviction both consumers surface the reason.
	assertLoadErrSurfaced(t, srv, "pre-evict", wantErr)

	if !st.EvictGroup("test", true) {
		t.Fatal("EvictGroup(keepReader) returned false")
	}
	// Revive on demand: State.Group -> reviveEvictedLocked -> rematerializeFromReader.
	if lg := st.Group("test"); lg == nil {
		t.Fatal("revive returned a nil group")
	}

	// The regression: the same two consumers must still surface the SAME reason.
	assertLoadErrSurfaced(t, srv, "post-revive", wantErr)

	// The sibling version-skew observability must survive the cycle too.
	st.mu.Lock()
	rr := st.groups["test"].Repos["broken"]
	st.mu.Unlock()
	if !rr.reindexRequired || rr.reindexReason == "" {
		t.Errorf("post-revive: reindexRequired=%v reindexReason=%q, want true + non-empty",
			rr.reindexRequired, rr.reindexReason)
	}
}

// assertLoadErrSurfaced drives both real consumers and checks each reports the
// broken repo's load error verbatim.
func assertLoadErrSurfaced(t *testing.T, srv *Server, phase, wantErr string) {
	t.Helper()

	// Consumer 1: grafel_diagnostics -> repos[].load_error.
	diag := callDashboardTool(t, srv.handleDiagnostics, map[string]any{"group": "test"})
	repos, _ := diag["repos"].([]any)
	if len(repos) != 1 {
		t.Fatalf("%s: diagnostics returned %d repos, want 1", phase, len(repos))
	}
	r0, _ := repos[0].(map[string]any)
	if got, _ := r0["load_error"].(string); got != wantErr {
		t.Errorf("%s: diagnostics load_error = %q, want %q", phase, got, wantErr)
	}

	// Consumer 2: grafel_stats -> totals.unavailable[] ("<repo>: <reason>").
	req := mcpapi.CallToolRequest{}
	req.Params.Arguments = map[string]any{"group": "test"}
	res, err := srv.handleGraphStats(context.Background(), req)
	if err != nil {
		t.Fatalf("%s: handleGraphStats: %v", phase, err)
	}
	// handleGraphStats returns the totals map itself as the JSON body.
	totals := extractResultJSON(t, res)
	unavail, _ := totals["unavailable"].([]any)
	if len(unavail) != 1 {
		t.Fatalf("%s: stats unavailable = %v, want 1 entry", phase, totals["unavailable"])
	}
	entry, _ := unavail[0].(string)
	if want := "broken: " + wantErr; entry != want {
		t.Errorf("%s: stats unavailable[0] = %q, want %q", phase, entry, want)
	}
	if strings.HasSuffix(entry, ": ") {
		t.Errorf("%s: stats unavailable[0] = %q — reason lost, colon with nothing after it", phase, entry)
	}
}
