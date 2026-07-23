package daemon

// #5937 — run-scoped retained-terminal invalidation.
//
// internal/dashboard's SSE handler replays whatever terminal (PhaseDone /
// PhaseError) the progress.Broker retains for a group on connect (the #5326
// guarantee). Before this fix nothing ever cleared that retained terminal, so
// a stale entry from a PRIOR run could be replayed to a client watching a NEW
// run and the stream closed before any live event for that new run arrived.
//
// The fix invalidates at the single choke point every rebuild trigger passes
// through in this process, in EITHER mode: Service.Rebuild. These tests prove
// that Rebuild clears a group's retained terminal at the very start of the
// call, in both split mode (where the actual work is enqueued for a separate
// engine process) and monolith mode (where s.rebuild runs in-process
// synchronously) — mirroring the existing split/monolith mutual-exclusion
// tests in rebuild_split_mode_test.go.

import (
	"testing"

	"github.com/cajasmota/grafel/internal/daemon/proto"
	"github.com/cajasmota/grafel/internal/progress"
)

func TestRebuild_ClearsRetainedTerminal_MonolithMode(t *testing.T) {
	t.Setenv(SplitModeEnvVar, "0")
	root := t.TempDir()
	t.Setenv(EnvRoot, root)

	broker := progress.NewBroker()
	// Simulate a PRIOR run's retained terminal for this group, as if it had
	// completed before this Rebuild call.
	broker.Publish(progress.Event{GroupSlug: "mygroup", RepoSlug: "mygroup", Phase: progress.PhaseDone})
	if _, ok := broker.LastTerminal("mygroup"); !ok {
		t.Fatal("setup: expected a retained terminal before Rebuild")
	}

	svc := newService(nil, func(args proto.RebuildArgs) ([]string, string, error) {
		return []string{"/some/repo"}, "", nil
	}, nil, "", make(chan struct{}, 1), nil, 1)
	svc.SetProgressBroker(broker)

	var reply proto.RebuildReply
	if err := svc.Rebuild(&proto.RebuildArgs{Group: "mygroup"}, &reply); err != nil {
		t.Fatalf("Rebuild: %v", err)
	}

	if _, ok := broker.LastTerminal("mygroup"); ok {
		t.Error("Rebuild (monolith mode) did not clear the group's retained terminal at run start")
	}
}

func TestRebuild_ClearsRetainedTerminal_SplitMode(t *testing.T) {
	t.Setenv(SplitModeEnvVar, "1")
	root := t.TempDir()
	t.Setenv(EnvRoot, root)

	broker := progress.NewBroker()
	broker.Publish(progress.Event{GroupSlug: "mygroup", RepoSlug: "mygroup", Phase: progress.PhaseDone})
	if _, ok := broker.LastTerminal("mygroup"); !ok {
		t.Fatal("setup: expected a retained terminal before Rebuild")
	}

	// s.rebuild is wired non-nil (mirroring the real split-mode serve process
	// per service.go's Rebuild doc comment) but must NOT be invoked directly —
	// split mode only enqueues a request file. See
	// TestRebuild_SplitModeOn_WritesRequestFile_NeverCallsRebuildDirectly for
	// the existing mutual-exclusion assertion this test complements.
	svc := newService(nil, func(args proto.RebuildArgs) ([]string, string, error) {
		return []string{"/some/repo"}, "", nil
	}, nil, "", make(chan struct{}, 1), nil, 1)
	svc.SetProgressBroker(broker)

	var reply proto.RebuildReply
	if err := svc.Rebuild(&proto.RebuildArgs{Group: "mygroup"}, &reply); err != nil {
		t.Fatalf("Rebuild: %v", err)
	}

	if _, ok := broker.LastTerminal("mygroup"); ok {
		t.Error("Rebuild (split mode enqueue) did not clear the group's retained terminal at run start")
	}
}

// TestRebuild_NilProgressBroker_DoesNotPanic guards the nil-safety of the
// ClearTerminal call: test wiring (and any serve process without a dashboard)
// leaves progressBroker unset, and Rebuild must tolerate that silently.
func TestRebuild_NilProgressBroker_DoesNotPanic(t *testing.T) {
	t.Setenv(SplitModeEnvVar, "0")
	root := t.TempDir()
	t.Setenv(EnvRoot, root)

	svc := newService(nil, func(args proto.RebuildArgs) ([]string, string, error) {
		return []string{"/some/repo"}, "", nil
	}, nil, "", make(chan struct{}, 1), nil, 1)
	// svc.progressBroker deliberately left nil (no SetProgressBroker call).

	var reply proto.RebuildReply
	if err := svc.Rebuild(&proto.RebuildArgs{Group: "mygroup"}, &reply); err != nil {
		t.Fatalf("Rebuild: %v", err)
	}
}
