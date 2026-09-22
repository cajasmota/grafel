package daemon

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/daemon/proto"
)

// TestGroupRebuildContext_CancelStopsWork verifies the package-level rebuild
// cancel registry: a registered group-rebuild context is cancelled by
// CancelGroupRebuild (so the long-running rebuild loop observes ctx.Done and
// unwinds), and a second cancel / an ended group report "nothing to cancel".
func TestGroupRebuildContext_CancelStopsWork(t *testing.T) {
	drainRegistry("gA")
	ctx, _, end := GroupRebuildContext("gA")
	defer end()

	started := make(chan struct{})
	stopped := make(chan struct{})
	go func() {
		close(started)
		<-ctx.Done() // the rebuild loop's cancellation checkpoint
		close(stopped)
	}()
	<-started

	if !CancelGroupRebuild("gA") {
		t.Fatal("CancelGroupRebuild(gA) = false, want true for a registered rebuild")
	}
	select {
	case <-stopped:
	case <-time.After(2 * time.Second):
		t.Fatal("rebuild context was not cancelled")
	}

	// Second cancel is a no-op (already removed).
	if CancelGroupRebuild("gA") {
		t.Fatal("second CancelGroupRebuild(gA) = true, want false")
	}
}

// TestEndGroupRebuild_DoesNotCancel verifies end() deregisters WITHOUT
// cancelling (normal completion path) and that a subsequently-deleted group is
// then uncancellable.
func TestEndGroupRebuild_DoesNotCancel(t *testing.T) {
	drainRegistry("gB")
	ctx, _, end := GroupRebuildContext("gB")
	end()
	if ctx.Err() != nil {
		t.Fatalf("end() must not cancel the context; got err=%v", ctx.Err())
	}
	if CancelGroupRebuild("gB") {
		t.Fatal("CancelGroupRebuild after end() = true, want false")
	}
}

// TestDeleteThenRecreate_RebuildNotCancelled is the regression guard for
// FINDING 2: deleting an already-idle group (nothing registered) must NOT leave
// any state that spuriously cancels a rebuild of a RECREATED same-name group —
// the "delete api; recreate api" loop. With the tombstone removed,
// CancelGroupRebuild on an idle group is a pure no-op, so the recreate's rebuild
// context is live.
func TestDeleteThenRecreate_RebuildNotCancelled(t *testing.T) {
	drainRegistry("api")

	// Delete an idle group: nothing is registered, so this is a no-op miss.
	if CancelGroupRebuild("api") {
		t.Fatal("CancelGroupRebuild on an idle group = true, want false")
	}

	// Recreate + rebuild immediately: its context MUST be live (not cancelled).
	ctx, _, end := GroupRebuildContext("api")
	defer end()
	if ctx.Err() != nil {
		t.Fatalf("recreate's rebuild was spuriously cancelled after an idle delete: %v", ctx.Err())
	}
	// And it is still cancellable by a real subsequent delete.
	if !CancelGroupRebuild("api") {
		t.Fatal("live recreate rebuild should be cancellable")
	}
	if ctx.Err() == nil {
		t.Fatal("recreate rebuild context not cancelled by its own delete")
	}
}

// TestEndGroupRebuild_OnlyDeletesOwnEntry covers the stale-end() hazard: a rapid
// delete→recreate reuses the group name, so the OLD rebuild's deferred end()
// must not delete the NEW rebuild's registered cancel. After the stale end(),
// the new rebuild must still be cancellable.
func TestEndGroupRebuild_OnlyDeletesOwnEntry(t *testing.T) {
	drainRegistry("gDup")

	// Rebuild #1 registers, then (simulating recreate) rebuild #2 registers under
	// the SAME name. #2's registration defensively cancels #1's stale context.
	_, _, end1 := GroupRebuildContext("gDup")
	ctx2, _, end2 := GroupRebuildContext("gDup")
	defer end2()

	// #1's deferred end() fires (its goroutine finished) — it must NOT evict #2.
	end1()

	if ctx2.Err() != nil {
		t.Fatalf("rebuild #2 should still be live after #1's end(); got err=%v", ctx2.Err())
	}
	if !CancelGroupRebuild("gDup") {
		t.Fatal("rebuild #2 was un-cancellable — #1's end() blind-deleted its entry (leak re-opened)")
	}
	select {
	case <-ctx2.Done():
	case <-time.After(time.Second):
		t.Fatal("rebuild #2 context not cancelled")
	}
}

// drainRegistry clears any entry left for a group so tests that reuse a name
// across the shared package-level registry start clean.
func drainRegistry(group string) {
	groupRebuildCancels.mu.Lock()
	delete(groupRebuildCancels.m, group)
	groupRebuildCancels.mu.Unlock()
}

// TestDeleteGroup_CancelsInFlightRebuild is the end-to-end assertion for the
// v0.1.8 leak fix: a `grafel delete <group>` (Service.DeleteGroup) cancels the
// group's in-flight rebuild context so its indexing/enrichment goroutines stop,
// and DeleteGroup returns without hanging. The rebuild is modelled by a
// registered GroupRebuildContext + a goroutine blocked on ctx.Done, so the test
// is deterministic (no wall-clock enrichment).
func TestDeleteGroup_CancelsInFlightRebuild(t *testing.T) {
	home := t.TempDir()
	t.Setenv("GRAFEL_HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))

	repo := t.TempDir()
	registerCleanupGroup(t, "live", repo)

	// Simulate an in-flight group rebuild that will keep burning CPU until its
	// context is cancelled.
	ctx, _, end := GroupRebuildContext("live")
	defer end()
	stopped := make(chan struct{})
	go func() {
		<-ctx.Done()
		close(stopped)
	}()

	s := &Service{}
	done := make(chan error, 1)
	go func() {
		var reply proto.DeleteGroupReply
		done <- s.DeleteGroup(&proto.DeleteGroupArgs{Group: "live"}, &reply)
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("DeleteGroup: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("DeleteGroup hung — cancellation must not block the delete")
	}

	select {
	case <-stopped:
	case <-time.After(2 * time.Second):
		t.Fatal("in-flight rebuild context was NOT cancelled by DeleteGroup")
	}
}

// TestRegisterSameGroupTwice_CancelsPredecessor grades the REGISTER path's
// defensive branch in GroupRebuildContext — `if prev, ok := r.m[group]; ok {
// prev.cancel() }` — which nothing asserted before (#7307). Its sibling
// TestEndGroupRebuild_OnlyDeletesOwnEntry asserts only that the SUCCESSOR stays
// live, which passes identically whether the predecessor was cancelled or
// merely forgotten; that half is the one this test pins. Kept separate from
// that test on purpose: this is the register path, that one is the delete path,
// and one test asserting both could not say which mechanism produced the
// outcome.
//
// The first select is a positive control, not decoration: a Done() channel that
// is closed because the context was never properly created would satisfy the
// post-condition on its own, so #1 is asserted LIVE before the second
// registration and CANCELLED after it.
func TestRegisterSameGroupTwice_CancelsPredecessor(t *testing.T) {
	drainRegistry("gPrev")

	ctx1, _, end1 := GroupRebuildContext("gPrev")
	defer end1()

	// Positive control: #1 must be live BEFORE the re-registration.
	select {
	case <-ctx1.Done():
		t.Fatalf("rebuild #1 was already done before the second registration (err=%v) — "+
			"the post-condition below would then hold for a context that was never live", ctx1.Err())
	default:
	}
	if err := ctx1.Err(); err != nil {
		t.Fatalf("rebuild #1 ctx.Err() before re-registration = %v, want nil", err)
	}

	// Re-register under the SAME group name (rapid delete→recreate).
	_, _, end2 := GroupRebuildContext("gPrev")
	defer end2()

	select {
	case <-ctx1.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("stale predecessor was NOT cancelled by a re-registration under the same name — " +
			"its rebuild goroutines keep churning with no registered cancel (orphaned)")
	}
	if !errors.Is(ctx1.Err(), context.Canceled) {
		t.Fatalf("predecessor ctx.Err() = %v, want context.Canceled", ctx1.Err())
	}
}
