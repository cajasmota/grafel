package mcp

import (
	"testing"
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
	srv := coreTestServer(t)
	runID := "2026-05-26-testid01"
	start := map[string]any{"group": "g", "no_git": true}
	status := map[string]any{"run_id": runID, "no_git": true}
	validate := map[string]any{"run_id": runID, "no_git": true}
	promote := map[string]any{"run_id": runID, "group": "g", "no_git": true}
	abort := map[string]any{"run_id": runID, "group": "g", "no_git": true}
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
