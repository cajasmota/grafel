package mcp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/cajasmota/grafel/internal/daemon"
)

// meta_merges_test.go — dispatch tests for the WORKFLOW/META-cluster canonical
// tools (#5546/#5551). Each test asserts that a discriminator value on the new
// canonical handler produces the same output as the absorbed handler it routes
// to. Helpers coreTestServer / assertSameDispatch are shared from
// core_merges_test.go. The docgen members return deterministic errors for
// unknown run_ids in the test registry; routing is verified by canonical and
// absorbed handlers producing the identical result for the same args.

// 1. grafel_docgen action= → start/status/list/promote/abort/validate.
func TestWorkflowDocgenDispatch(t *testing.T) {
	// #6075: isolate the home resolvers and pin the project root into a temp
	// dir. action=start below really does create a staging run, and with
	// neither of these the run landed in <repo>/.grafel/staging/<date>-* in
	// whatever working tree the suite happened to run from — the same
	// "a test resolves to a real user path" root cause as the promote defect.
	sandboxGrafelHome(t)
	project := t.TempDir()

	srv := coreTestServer(t)
	runID := "2026-05-26-testid01"
	start := map[string]any{"group": "g", "no_git": true, "cwd": project}
	status := map[string]any{"run_id": runID, "no_git": true, "cwd": project}
	validate := map[string]any{"run_id": runID, "no_git": true, "cwd": project}
	promote := map[string]any{"run_id": runID, "group": "g", "no_git": true, "cwd": project}
	abort := map[string]any{"run_id": runID, "group": "g", "no_git": true, "cwd": project}
	list := map[string]any{"group": "g"}

	with := func(base map[string]any, action string) map[string]any {
		out := map[string]any{"action": action}
		for k, v := range base {
			out[k] = v
		}
		return out
	}

	// start mutates persistent staging state (a second identical call resumes
	// rather than creates), so the double-call assertSameDispatch comparison
	// doesn't apply. Verify routing directly: action=start must reach
	// handleDocgenStartRun and return a fresh run (resumed=false, has run_id).
	startOut := callBare(t, srv.handleWorkflowDocgen, with(start, "start"))
	if !contains(startOut, `"run_id"`) || !contains(startOut, `"resumed":false`) {
		t.Errorf("action=start did not route to handleDocgenStartRun: %s", startOut)
	}
	assertSameDispatch(t, "action=status", srv.handleWorkflowDocgen, with(status, "status"), srv.handleDocgenStatus, status)
	// default action=status.
	assertSameDispatch(t, "action=default", srv.handleWorkflowDocgen, status, srv.handleDocgenStatus, status)
	assertSameDispatch(t, "action=list", srv.handleWorkflowDocgen, with(list, "list"), srv.handleDocgenList, list)
	// NOTE (#6075): runID above is deliberately unresolvable, so promote/abort
	// compare two identical "run_id not found" errors. That is a routing
	// assertion only — it says nothing about promote's behaviour, and it is
	// the reason this test never reached the rotate branch. Do NOT "improve"
	// it by making the run ID resolvable here; that would turn a dispatch test
	// into a destructive one. The behavioural coverage for promote/rotate
	// lives in docgen_promote_containment_6075_test.go, against an injected
	// docs root.
	assertSameDispatch(t, "action=promote", srv.handleWorkflowDocgen, with(promote, "promote"), srv.handleDocgenPromote, promote)
	assertSameDispatch(t, "action=abort", srv.handleWorkflowDocgen, with(abort, "abort"), srv.handleDocgenAbort, abort)
	assertSameDispatch(t, "action=validate", srv.handleWorkflowDocgen, with(validate, "validate"), srv.handleDocgenValidate, validate)
}

// 2. grafel_docgen_apply kind= → semantics/repairs(apply|queue)/enrichments.
func TestWorkflowDocgenApplyDispatch(t *testing.T) {
	srv := coreTestServer(t)
	g := map[string]any{"group": "g", "dry_run": true}

	// semantics → handleApplyDocSemantics.
	assertSameDispatch(t, "kind=semantics", srv.handleWorkflowDocgenApply,
		map[string]any{"group": "g", "dry_run": true, "kind": "semantics"}, srv.handleApplyDocSemantics, g)
	// default kind=semantics.
	assertSameDispatch(t, "kind=default", srv.handleWorkflowDocgenApply, g, srv.handleApplyDocSemantics, g)
	// repairs (no action) → handleApplyDocgenRepairs (docgen→graph apply step).
	assertSameDispatch(t, "kind=repairs", srv.handleWorkflowDocgenApply,
		map[string]any{"group": "g", "dry_run": true, "kind": "repairs"}, srv.handleApplyDocgenRepairs, g)
	// repairs WITH action → handleRepairs (residual-repair queue).
	queue := map[string]any{"group": "g", "action": "list"}
	assertSameDispatch(t, "kind=repairs,action=list", srv.handleWorkflowDocgenApply,
		map[string]any{"group": "g", "kind": "repairs", "action": "list"}, srv.handleRepairs, queue)
	// enrichments → handleEnrichments (candidate queue; reads its own action=).
	enr := map[string]any{"group": "g", "action": "list"}
	assertSameDispatch(t, "kind=enrichments", srv.handleWorkflowDocgenApply,
		map[string]any{"group": "g", "kind": "enrichments", "action": "list"}, srv.handleEnrichments, enr)
}

// 2b. Regression for #5784 bug 2: grafel_docgen_apply kind=enrichments must
// not clobber handleListEnrichmentCandidates's own `kind` candidate-kind
// filter. The umbrella discriminator ("enrichments") and the inner filter
// share the param name `kind`; passed straight through, the inner filter
// reads back "enrichments" and never matches a real candidate kind, so a
// caller can never narrow the enrichments list by kind through the
// canonical tool. The canonical schema exposes this as `candidate_kind`.
func TestWorkflowDocgenApplyEnrichmentsKindNotClobbered(t *testing.T) {
	dir := t.TempDir()
	repo := filepath.Join(dir, "r1")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	writeGraph(t, repo, fixtureDoc("r1"))
	cands := []EnrichmentCandidate{
		{ID: "e1", NodeID: "a1", Kind: "purpose"},
		{ID: "e2", NodeID: "a2", Kind: "risk"},
	}
	candPath := filepath.Join(daemon.StateDirForRepo(repo), "enrichment-candidates.json")
	if err := os.MkdirAll(filepath.Dir(candPath), 0o755); err != nil {
		t.Fatal(err)
	}
	d, err := json.MarshalIndent(cands, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(candPath, d, 0o644); err != nil {
		t.Fatal(err)
	}
	regPath := makeRegistry(t, dir, map[string]map[string]string{"g": {"r1": repo}})
	srv, err := NewServer(Config{RegistryPath: regPath})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	t.Cleanup(srv.Close)

	out := callBare(t, srv.handleWorkflowDocgenApply, map[string]any{
		"group": "g", "kind": "enrichments", "action": "list", "candidate_kind": "risk",
	})
	var arr []map[string]any
	if err := json.Unmarshal([]byte(out), &arr); err != nil {
		t.Fatalf("result not a JSON array: %v (%s)", err, out)
	}
	if len(arr) != 1 || arr[0]["id"] != "e2" {
		t.Fatalf("candidate_kind=risk did not filter to just e2, got: %s", out)
	}
}

// 3. grafel_event kind= → feedback/persona.
func TestMetaEventDispatch(t *testing.T) {
	srv := coreTestServer(t)
	feedback := map[string]any{"outcome": "helped"}
	persona := map[string]any{"persona": "architect", "event_type": "invoke"}

	assertSameDispatch(t, "kind=feedback", srv.handleMetaEvent,
		map[string]any{"outcome": "helped", "kind": "feedback"}, srv.handleFeedbackEvent, feedback)
	// default kind=feedback.
	assertSameDispatch(t, "kind=default", srv.handleMetaEvent, feedback, srv.handleFeedbackEvent, feedback)
	assertSameDispatch(t, "kind=persona", srv.handleMetaEvent,
		map[string]any{"persona": "architect", "event_type": "invoke", "kind": "persona"}, srv.handlePersonaEvent, persona)
}

// 4. All three WORKFLOW/META canonical tools plus the two kept-as-is tools are
// registered (#5546/#5551).
func TestMetaCanonicalToolsRegistered(t *testing.T) {
	srv := coreTestServer(t)
	registered := map[string]bool{}
	for _, st := range srv.MCP.ListTools() {
		registered[st.Tool.Name] = true
	}
	for _, n := range []string{
		"grafel_docgen", "grafel_docgen_apply", "grafel_event",
		// kept-as-is canonical tools the epic confirms remain registered.
		"grafel_mcp_metrics", "grafel_index_status",
	} {
		if !registered[n] {
			t.Errorf("WORKFLOW/META canonical tool %q not registered", n)
		}
	}
}
