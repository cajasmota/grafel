package sched

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

// TestSchedulerClone_SuccessSkipsIndex verifies that when CloneFn returns
// Done=true, IndexFn is NOT called for that job.
func TestSchedulerClone_SuccessSkipsIndex(t *testing.T) {
	var indexCalled atomic.Bool
	var cloneCalled atomic.Bool

	s := New(Config{
		Workers: 1,
		Clone: func(_ context.Context, _ string, ref string) CloneResult {
			cloneCalled.Store(true)
			return CloneResult{Done: true, ParentRef: "main", ChangedFiles: 5}
		},
		Index: func(_ context.Context, _ string, _ string) error {
			indexCalled.Store(true)
			return nil
		},
	})
	s.Start()
	defer s.Stop()

	s.EnqueueRef("/repo/fixture-a", "feat/ph7-test")

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cloneCalled.Load() {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if !cloneCalled.Load() {
		t.Fatal("CloneFn was never called")
	}
	if indexCalled.Load() {
		t.Error("IndexFn was called even though CloneFn returned Done=true — should have been skipped")
	}
}

// TestSchedulerClone_FailureFallsBackToIndex verifies that when CloneFn
// returns Done=false (abort), IndexFn is called as the full-reindex fallback.
func TestSchedulerClone_FailureFallsBackToIndex(t *testing.T) {
	var indexCalled atomic.Bool
	var cloneCalled atomic.Bool

	s := New(Config{
		Workers: 1,
		Clone: func(_ context.Context, _ string, _ string) CloneResult {
			cloneCalled.Store(true)
			return CloneResult{Done: false} // simulate precondition failure
		},
		Index: func(_ context.Context, _ string, _ string) error {
			indexCalled.Store(true)
			return nil
		},
	})
	s.Start()
	defer s.Stop()

	s.EnqueueRef("/repo/fixture-b", "feat/no-parent")

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if indexCalled.Load() {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if !cloneCalled.Load() {
		t.Fatal("CloneFn was never called")
	}
	if !indexCalled.Load() {
		t.Error("IndexFn was not called as fallback after CloneFn returned Done=false")
	}
}

// TestSchedulerClone_NilCloneFn verifies that when no CloneFn is configured,
// the scheduler behaves exactly as before (IndexFn is always called).
func TestSchedulerClone_NilCloneFn(t *testing.T) {
	var indexCalled atomic.Bool

	s := New(Config{
		Workers: 1,
		Clone:   nil, // no clone configured
		Index: func(_ context.Context, _ string, _ string) error {
			indexCalled.Store(true)
			return nil
		},
	})
	s.Start()
	defer s.Stop()

	s.EnqueueRef("/repo/fixture-c", "main")

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if indexCalled.Load() {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if !indexCalled.Load() {
		t.Error("IndexFn not called when Clone is nil — expected normal index path")
	}
}

// TestSchedulerClone_EmptyRefSkipsClone verifies that when a job has an
// empty ref, CloneFn is NOT attempted (we don't know which per-ref store to
// check).
func TestSchedulerClone_EmptyRefSkipsClone(t *testing.T) {
	var cloneCalled atomic.Bool
	var indexCalled atomic.Bool

	s := New(Config{
		Workers: 1,
		Clone: func(_ context.Context, _ string, _ string) CloneResult {
			cloneCalled.Store(true)
			return CloneResult{Done: true}
		},
		Index: func(_ context.Context, _ string, _ string) error {
			indexCalled.Store(true)
			return nil
		},
	})
	s.Start()
	defer s.Stop()

	// EnqueueRef with empty ref — clone must be skipped.
	s.EnqueueRef("/repo/fixture-d", "")

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if indexCalled.Load() {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if cloneCalled.Load() {
		t.Error("CloneFn was called for empty ref — should be skipped")
	}
	if !indexCalled.Load() {
		t.Error("IndexFn was not called as fallback for empty ref")
	}
}

// TestSchedulerClone_SnapshotLogsCloneOk verifies that a successful clone
// event appears in the recent-log snapshot.
func TestSchedulerClone_SnapshotLogsCloneOk(t *testing.T) {
	done := make(chan struct{})

	s := New(Config{
		Workers: 1,
		Clone: func(_ context.Context, _ string, ref string) CloneResult {
			defer func() {
				select {
				case done <- struct{}{}:
				default:
				}
			}()
			return CloneResult{Done: true, ParentRef: "main", ChangedFiles: 3}
		},
		Index: func(_ context.Context, _ string, _ string) error {
			return nil
		},
	})
	s.Start()
	defer s.Stop()

	s.EnqueueRef("/repo/fixture-e", "feat/log-test")

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("CloneFn never called")
	}
	// Give the log a moment to flush.
	time.Sleep(20 * time.Millisecond)

	snap := s.Snapshot()
	foundCloneOk := false
	for _, e := range snap.RecentLog {
		if e.Kind == "clone_ok" {
			foundCloneOk = true
			break
		}
	}
	if !foundCloneOk {
		t.Errorf("clone_ok not found in snapshot log; entries: %v", snap.RecentLog)
	}
}
