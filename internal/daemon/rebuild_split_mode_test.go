package daemon

// Mutual-exclusion + round-trip regression for the ADR-0024 PR6 prerequisite
// (epic #5729, gap #1 from the PR4 review): Service.Rebuild must use EXACTLY
// ONE of the two rebuild-trigger paths — never both, never neither —
// depending on GRAFEL_SPLIT_MODE:
//
//   - flag OFF (monolith, the default): calls s.rebuild(*args) directly,
//     in-process, exactly as before this change. No requests/ file is
//     written.
//   - flag ON (split mode): writes a KindRebuild request file instead of
//     ever calling s.rebuild in-serve (s.rebuild is deliberately wired
//     non-nil in this test harness, mirroring the real split-mode serve
//     process — see service.go's Rebuild doc comment — to prove it is NOT
//     invoked).

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/daemon/proto"
	"github.com/cajasmota/grafel/internal/daemon/requests"
)

func TestRebuild_SplitModeOff_CallsRebuildDirectly_NoRequestFile(t *testing.T) {
	t.Setenv(SplitModeEnvVar, "")
	root := t.TempDir()
	t.Setenv(EnvRoot, root)

	rebuilt := make(chan string, 1)
	svc := newService(nil, func(args proto.RebuildArgs) ([]string, string, error) {
		rebuilt <- args.Group
		return []string{"/some/repo"}, "", nil
	}, nil, "", make(chan struct{}, 1), nil, 1)

	var reply proto.RebuildReply
	if err := svc.Rebuild(&proto.RebuildArgs{Group: "mygroup"}, &reply); err != nil {
		t.Fatalf("Rebuild: %v", err)
	}

	select {
	case group := <-rebuilt:
		if group != "mygroup" {
			t.Fatalf("rebuilt wrong group: got %q want %q", group, "mygroup")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("expected s.rebuild to be called directly (monolith path)")
	}
	if len(reply.Repos) != 1 || reply.Repos[0] != "/some/repo" {
		t.Fatalf("expected synchronous reply to carry the rebuilt repos, got %+v", reply)
	}

	dir := requestsDirForGroup("mygroup")
	recs, err := requests.ListPending(dir)
	if err != nil {
		t.Fatalf("ListPending: %v", err)
	}
	if len(recs) != 0 {
		t.Fatalf("split mode OFF must not queue a rebuild request; got %d pending", len(recs))
	}
}

func TestRebuild_SplitModeOn_WritesRequestFile_NeverCallsRebuildDirectly(t *testing.T) {
	t.Setenv(SplitModeEnvVar, "1")
	root := t.TempDir()
	t.Setenv(EnvRoot, root)

	rebuilt := make(chan string, 1)
	svc := newService(nil, func(args proto.RebuildArgs) ([]string, string, error) {
		rebuilt <- args.Group
		return []string{"/some/repo"}, "", nil
	}, nil, "", make(chan struct{}, 1), nil, 1)

	var reply proto.RebuildReply
	if err := svc.Rebuild(&proto.RebuildArgs{Group: "mygroup", Wipe: true}, &reply); err != nil {
		t.Fatalf("Rebuild: %v", err)
	}

	select {
	case group := <-rebuilt:
		t.Fatalf("split mode ON must NOT call s.rebuild directly, but got a call for group %q", group)
	case <-time.After(300 * time.Millisecond):
		// Expected: no direct call.
	}

	dir := requestsDirForGroup("mygroup")
	recs, err := requests.ListPending(dir)
	if err != nil {
		t.Fatalf("ListPending: %v", err)
	}
	if len(recs) != 1 {
		t.Fatalf("expected exactly 1 queued rebuild request, got %d", len(recs))
	}
	if recs[0].Kind != requests.KindRebuild {
		t.Fatalf("unexpected record kind: %+v", recs[0])
	}
	var gotArgs proto.RebuildArgs
	if err := json.Unmarshal(recs[0].Payload, &gotArgs); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if gotArgs.Group != "mygroup" || !gotArgs.Wipe {
		t.Fatalf("unexpected rebuild args in payload: %+v", gotArgs)
	}
}

// TestRebuild_SplitModeOn_DrainInvokesEngineRebuildLogic is the round-trip
// regression: a KindRebuild request written by Service.Rebuild in split mode
// (simulated end-to-end via the real RPC call) is drained by the engine and
// turned into a real call to the SAME RebuildFunc the monolith/engine calls
// in-process today — via drainRequestsOnce/applyRequest (requests_drain.go).
func TestRebuild_SplitModeOn_DrainInvokesEngineRebuildLogic(t *testing.T) {
	t.Setenv(SplitModeEnvVar, "1")
	root := t.TempDir()
	t.Setenv(EnvRoot, root)

	rebuilt := make(chan proto.RebuildArgs, 1)
	rebuildFn := func(args proto.RebuildArgs) ([]string, string, error) {
		rebuilt <- args
		return []string{"/some/repo"}, "", nil
	}

	svc := newService(nil, rebuildFn, nil, "", make(chan struct{}, 1), nil, 1)

	var reply proto.RebuildReply
	if err := svc.Rebuild(&proto.RebuildArgs{Group: "mygroup", Incremental: true}, &reply); err != nil {
		t.Fatalf("Rebuild: %v", err)
	}

	dir := requestsDirForGroup("mygroup")
	if err := drainRequestsOnce(requestsRoot(), nil, rebuildFn, nil); err != nil {
		t.Fatalf("drainRequestsOnce: %v", err)
	}

	select {
	case args := <-rebuilt:
		if args.Group != "mygroup" || !args.Incremental {
			t.Fatalf("engine rebuild invoked with unexpected args: %+v", args)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("expected the drained request to invoke the engine's rebuild logic")
	}

	recs, err := requests.ListPending(dir)
	if err != nil {
		t.Fatalf("ListPending: %v", err)
	}
	if len(recs) != 0 {
		t.Fatalf("expected the rebuild request to be consumed, still pending: %+v", recs)
	}

	// Ack-GC: no orphaned ack file should remain after a successful drain
	// (PR6 prerequisite gap #2).
	entries, err := os.ReadDir(dir)
	if err != nil && !os.IsNotExist(err) {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".ack.json") {
			t.Fatalf("expected no orphaned ack files after successful drain, found %q", e.Name())
		}
	}
}

// TestRebuild_SplitModeOn_RedrainedRebuildIsHarmless proves a rebuild
// redrained after a simulated crash (ack written, request not yet deleted)
// does not double-apply: draining twice must invoke the engine rebuild logic
// at most once for the same request.
func TestRebuild_SplitModeOn_RedrainedRebuildIsHarmless(t *testing.T) {
	t.Setenv(SplitModeEnvVar, "1")
	root := t.TempDir()
	t.Setenv(EnvRoot, root)

	var calls int
	rebuildFn := func(args proto.RebuildArgs) ([]string, string, error) {
		calls++
		return []string{"/some/repo"}, "", nil
	}

	dir := requestsDirForGroup("mygroup")
	payload, err := json.Marshal(proto.RebuildArgs{Group: "mygroup"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if _, err := requests.Write(dir, requests.Record{Kind: requests.KindRebuild, Payload: payload}); err != nil {
		t.Fatalf("Write: %v", err)
	}

	if err := drainRequestsOnce(requestsRoot(), nil, rebuildFn, nil); err != nil {
		t.Fatalf("drainRequestsOnce (1st): %v", err)
	}
	if err := drainRequestsOnce(requestsRoot(), nil, rebuildFn, nil); err != nil {
		t.Fatalf("drainRequestsOnce (2nd): %v", err)
	}

	if calls != 1 {
		t.Fatalf("expected the rebuild to run exactly once across both drains (ack-guard), got %d", calls)
	}
}
