package atomicfile

// rename_test.go — the recovery driver around rename, exercised on EVERY
// platform through injected primitives.
//
// The failure this driver exists for only occurs on Windows, and no CI job
// here can run Windows on demand. So the platform-specific parts are cut down
// to three tiny predicates (rename_windows.go / rename_other.go) and the whole
// decision logic lives in rename.go, where these tests drive it directly with
// fakes. What is NOT covered locally is the errno classification itself and
// the os.Chmod-clears-FILE_ATTRIBUTE_READONLY mapping; those are asserted only
// by the end-to-end tests when they run on windows-latest.

import (
	"errors"
	"testing"
	"time"
)

// fakeRename records calls and replays a scripted sequence of results.
type fakeRename struct {
	results  []error // consumed in order; the last one repeats forever
	calls    int
	sleeps   []time.Duration
	readOnly bool
	roSets   []bool // every setReadOnly(_, v) in order
	roSetErr error
	probes   int
}

func (f *fakeRename) rename(string, string) error {
	f.calls++
	if f.calls <= len(f.results) {
		return f.results[f.calls-1]
	}
	if len(f.results) == 0 {
		return nil
	}
	return f.results[len(f.results)-1]
}

func (f *fakeRename) isReadOnly(string) bool { f.probes++; return f.readOnly }

func (f *fakeRename) setReadOnly(_ string, ro bool) error {
	if f.roSetErr != nil {
		return f.roSetErr
	}
	f.roSets = append(f.roSets, ro)
	f.readOnly = ro
	return nil
}

func (f *fakeRename) ops(recoverable func(error) bool) renameOps {
	return renameOps{
		rename:      f.rename,
		isReadOnly:  f.isReadOnly,
		setReadOnly: f.setReadOnly,
		recoverable: recoverable,
		sleep:       func(d time.Duration) { f.sleeps = append(f.sleeps, d) },
	}
}

var errDenied = errors.New("access is denied")

func allRecoverable(err error) bool { return err != nil }
func noneRecoverable(error) bool    { return false }

func TestRenameOver_SucceedsFirstTry(t *testing.T) {
	f := &fakeRename{}
	if err := f.ops(allRecoverable).renameOver("tmp", "dst"); err != nil {
		t.Fatalf("renameOver: %v", err)
	}
	if f.calls != 1 {
		t.Fatalf("rename calls = %d, want 1", f.calls)
	}
	if f.probes != 0 {
		t.Fatalf("read-only probes = %d, want 0 (the happy path must not stat the destination)", f.probes)
	}
	if len(f.sleeps) != 0 {
		t.Fatalf("sleeps = %v, want none", f.sleeps)
	}
}

// TestRenameOver_NonRecoverableErrorReturnedImmediately pins that the driver
// does not turn every rename failure into a half-second retry storm. Only the
// platform-classified sharing/permission errnos are recoverable.
func TestRenameOver_NonRecoverableErrorReturnedImmediately(t *testing.T) {
	f := &fakeRename{results: []error{errDenied}}
	err := f.ops(noneRecoverable).renameOver("tmp", "dst")
	if !errors.Is(err, errDenied) {
		t.Fatalf("err = %v, want %v", err, errDenied)
	}
	if f.calls != 1 {
		t.Fatalf("rename calls = %d, want 1", f.calls)
	}
	if len(f.sleeps) != 0 {
		t.Fatalf("sleeps = %v, want none", f.sleeps)
	}
	if len(f.roSets) != 0 {
		t.Fatalf("setReadOnly calls = %v, want none", f.roSets)
	}
}

// TestRenameOver_ReadOnlyDestinationClearedNotSleptOn is the CLASS A read-only
// case. MoveFileEx(MOVEFILE_REPLACE_EXISTING) — which is exactly what Go's
// os.Rename is on Windows — fails with ERROR_ACCESS_DENIED forever while the
// destination carries FILE_ATTRIBUTE_READONLY. Retrying can never fix it, so
// the driver must clear the attribute and must do so WITHOUT first burning
// retry budget on sleeps.
func TestRenameOver_ReadOnlyDestinationClearedNotSleptOn(t *testing.T) {
	f := &fakeRename{results: []error{errDenied, nil}, readOnly: true}
	if err := f.ops(allRecoverable).renameOver("tmp", "dst"); err != nil {
		t.Fatalf("renameOver: %v", err)
	}
	if f.calls != 2 {
		t.Fatalf("rename calls = %d, want 2 (fail, clear read-only, succeed)", f.calls)
	}
	if len(f.sleeps) != 0 {
		t.Fatalf("sleeps = %v, want none — a read-only destination is deterministic, not transient", f.sleeps)
	}
	if len(f.roSets) != 1 || f.roSets[0] != false {
		t.Fatalf("setReadOnly calls = %v, want exactly [false]", f.roSets)
	}
}

// TestRenameOver_ReadOnlyRestoredWhenReplaceStillFails: clearing the attribute
// is a mutation of a file we do not own. If the replace still fails we must
// put it back rather than leave a file the user marked read-only writable.
func TestRenameOver_ReadOnlyRestoredWhenReplaceStillFails(t *testing.T) {
	f := &fakeRename{results: []error{errDenied}, readOnly: true}
	if err := f.ops(allRecoverable).renameOver("tmp", "dst"); err == nil {
		t.Fatal("renameOver: want error, got nil")
	}
	if len(f.roSets) != 2 || f.roSets[0] != false || f.roSets[1] != true {
		t.Fatalf("setReadOnly calls = %v, want [false true] (cleared, then restored)", f.roSets)
	}
	if !f.readOnly {
		t.Fatal("destination left writable after a failed replace")
	}
}

// TestRenameOver_ReadOnlyNotRestoredAfterSuccess: the destination is gone —
// replaced by the temp file — so re-marking it read-only would apply the old
// flag to the NEW content, which is not what the caller asked for.
func TestRenameOver_ReadOnlyNotRestoredAfterSuccess(t *testing.T) {
	f := &fakeRename{results: []error{errDenied, nil}, readOnly: true}
	if err := f.ops(allRecoverable).renameOver("tmp", "dst"); err != nil {
		t.Fatalf("renameOver: %v", err)
	}
	for i, v := range f.roSets {
		if v {
			t.Fatalf("setReadOnly(true) at call %d — must not re-protect a destination we replaced", i)
		}
	}
}

// TestRenameOver_TransientSharingViolationRetried is the CLASS A concurrency
// case: another handle (a concurrent replacer, an antivirus scanner, the
// Windows indexer) holds the destination or the temp open without
// FILE_SHARE_DELETE. It is genuinely transient, and a bounded retry is the
// only remedy available to us.
func TestRenameOver_TransientSharingViolationRetried(t *testing.T) {
	f := &fakeRename{results: []error{errDenied, errDenied, errDenied, nil}}
	if err := f.ops(allRecoverable).renameOver("tmp", "dst"); err != nil {
		t.Fatalf("renameOver: %v", err)
	}
	if f.calls != 4 {
		t.Fatalf("rename calls = %d, want 4", f.calls)
	}
	if len(f.sleeps) != 3 {
		t.Fatalf("sleeps = %v, want 3", f.sleeps)
	}
	for i := 1; i < len(f.sleeps); i++ {
		if f.sleeps[i] <= f.sleeps[i-1] {
			t.Fatalf("backoff is not increasing: %v", f.sleeps)
		}
	}
}

// TestRenameOver_RetryIsBounded: the loop must terminate and surface the last
// error rather than hang a link pass forever.
func TestRenameOver_RetryIsBounded(t *testing.T) {
	f := &fakeRename{results: []error{errDenied}}
	err := f.ops(allRecoverable).renameOver("tmp", "dst")
	if !errors.Is(err, errDenied) {
		t.Fatalf("err = %v, want %v", err, errDenied)
	}
	if want := renameRetries + 1; f.calls != want {
		t.Fatalf("rename calls = %d, want %d", f.calls, want)
	}
	if len(f.sleeps) != renameRetries {
		t.Fatalf("sleeps = %d, want %d", len(f.sleeps), renameRetries)
	}
	var total time.Duration
	for _, d := range f.sleeps {
		total += d
	}
	if total > 2*time.Second {
		t.Fatalf("total backoff %v exceeds the 2s budget a write may block for", total)
	}
}

// TestRenameOver_RetryStopsOnNonRecoverableError: once the error class changes
// (say the directory was removed underneath us) the driver must stop.
func TestRenameOver_RetryStopsOnNonRecoverableError(t *testing.T) {
	other := errors.New("no such directory")
	f := &fakeRename{results: []error{errDenied, errDenied, other}}
	err := f.ops(func(e error) bool { return errors.Is(e, errDenied) }).renameOver("tmp", "dst")
	if !errors.Is(err, other) {
		t.Fatalf("err = %v, want %v", err, other)
	}
	if f.calls != 3 {
		t.Fatalf("rename calls = %d, want 3", f.calls)
	}
}

// TestRenameOver_ReadOnlyClearFailureFallsThroughToRetry: if we cannot clear
// the attribute (someone else's file, ACL denies us) we must not abandon the
// write — the failure may still have been the transient kind.
func TestRenameOver_ReadOnlyClearFailureFallsThroughToRetry(t *testing.T) {
	f := &fakeRename{results: []error{errDenied, errDenied, nil}, readOnly: true, roSetErr: errors.New("nope")}
	if err := f.ops(allRecoverable).renameOver("tmp", "dst"); err != nil {
		t.Fatalf("renameOver: %v", err)
	}
	if f.calls != 3 {
		t.Fatalf("rename calls = %d, want 3", f.calls)
	}
	// The read-only branch is skipped entirely when the clear fails, so both
	// remaining attempts come from the retry loop and each is preceded by a
	// sleep.
	if len(f.sleeps) != 2 {
		t.Fatalf("sleeps = %v, want 2", f.sleeps)
	}
	if len(f.roSets) != 0 {
		t.Fatalf("setReadOnly recorded %v despite failing", f.roSets)
	}
}

// TestDefaultRenameOps_Wired guards against the driver being reachable only
// through the fakes: renameAtomic must actually run the real primitives.
func TestDefaultRenameOps_Wired(t *testing.T) {
	if defaultRenameOps.rename == nil || defaultRenameOps.isReadOnly == nil ||
		defaultRenameOps.setReadOnly == nil || defaultRenameOps.recoverable == nil ||
		defaultRenameOps.sleep == nil {
		t.Fatal("defaultRenameOps has a nil primitive")
	}
}
