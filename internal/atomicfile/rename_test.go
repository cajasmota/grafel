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
// by rename_windows_test.go when it runs on windows-latest.
//
// NO TEST IN THIS PACKAGE MAY CALL t.Parallel. TestWriteFile_RoutesThrough-
// RenameAtomic swaps the package global defaultRenameOps.rename without
// synchronisation, and other tests here run 8 concurrent WriteFile goroutines.
// That is only safe because Go runs a package's tests sequentially unless they
// opt into parallelism. TestNoParallelTestsInThisPackage enforces it.

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
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
	warns    []string
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
		warn:        func(p string) { f.warns = append(f.warns, p) },
	}
}

var errDenied = errors.New("access is denied")

func allRecoverable(err error) bool { return err != nil }
func noneRecoverable(error) bool    { return false }

// failN returns a script that fails n times with errDenied then succeeds.
func failN(n int) []error {
	out := make([]error, 0, n+1)
	for i := 0; i < n; i++ {
		out = append(out, errDenied)
	}
	return append(out, nil)
}

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
	if len(f.warns) != 0 {
		t.Fatalf("warns = %v, want none", f.warns)
	}
}

// TestRenameOver_NonRecoverableErrorReturnedImmediately pins that the driver
// does not turn every rename failure into a 200ms retry storm. Only the
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

// TestRenameOver_WarnsWhenItDefeatsReadOnly is the F3 contract. Clearing
// FILE_ATTRIBUTE_READONLY destroys a protection a HUMAN set — no production
// call site in this tree ever passes a read-only perm, so a read-only
// destination in the wild is always user intent. Doing it is the documented
// behaviour; doing it SILENTLY is not.
func TestRenameOver_WarnsWhenItDefeatsReadOnly(t *testing.T) {
	f := &fakeRename{results: []error{errDenied, nil}, readOnly: true}
	if err := f.ops(allRecoverable).renameOver("tmp", "/some/dst.json"); err != nil {
		t.Fatalf("renameOver: %v", err)
	}
	if len(f.warns) != 1 {
		t.Fatalf("warns = %v, want exactly one warning naming the destination", f.warns)
	}
	if f.warns[0] != "/some/dst.json" {
		t.Fatalf("warned about %q, want the destination path", f.warns[0])
	}
}

// TestRenameOver_DoesNotWarnWhenNoReadOnlyDefeated: the transient path must
// stay quiet, or the warning becomes noise nobody reads.
func TestRenameOver_DoesNotWarnWhenNoReadOnlyDefeated(t *testing.T) {
	f := &fakeRename{results: failN(3)}
	if err := f.ops(allRecoverable).renameOver("tmp", "dst"); err != nil {
		t.Fatalf("renameOver: %v", err)
	}
	if len(f.warns) != 0 {
		t.Fatalf("warns = %v, want none on the transient path", f.warns)
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
// case: another handle (a concurrent replacer, a reader mid-ReadFile, an
// antivirus scanner, the Windows indexer) holds the destination or the temp
// open without FILE_SHARE_DELETE. It is genuinely transient, and a bounded
// retry is the only remedy available to us.
func TestRenameOver_TransientSharingViolationRetried(t *testing.T) {
	f := &fakeRename{results: failN(3)}
	if err := f.ops(allRecoverable).renameOver("tmp", "dst"); err != nil {
		t.Fatalf("renameOver: %v", err)
	}
	if f.calls != 4 {
		t.Fatalf("rename calls = %d, want 4", f.calls)
	}
	if len(f.sleeps) != 3 {
		t.Fatalf("sleeps = %v, want 3", f.sleeps)
	}
	for i, d := range f.sleeps {
		if d != renameRetryDelay {
			t.Fatalf("sleep %d = %v, want the fixed %v delay", i, d, renameRetryDelay)
		}
	}
}

// TestRenameOver_SurvivesLongContentionRun is the F1 regression.
//
// The budget is sized on ATTEMPT COUNT, not wall-clock: the tests this fix
// exists for are 8-way concurrent, so a writer must survive losing many
// consecutive races. An earlier draft allowed 8 retries with exponential
// backoff — a bigger wall-clock number and a much worse attempt count. This
// pins that a writer losing 30 races in a row still lands, which 8 retries
// cannot do at any delay.
func TestRenameOver_SurvivesLongContentionRun(t *testing.T) {
	const losses = 30
	f := &fakeRename{results: failN(losses)}
	if err := f.ops(allRecoverable).renameOver("tmp", "dst"); err != nil {
		t.Fatalf("renameOver after %d lost races: %v", losses, err)
	}
	if f.calls != losses+1 {
		t.Fatalf("rename calls = %d, want %d", f.calls, losses+1)
	}
}

// TestRenameBudget_MatchesInTreePrecedent pins the numbers themselves against
// internal/graph/groupalgo/atomicrename_windows.go (40 × 5ms ≈ 200ms), whose
// comment reasons against a FOUR-reader stress test. Eight concurrent writers
// is strictly heavier load, so dropping below that precedent would be a
// regression in disguise — and the sizing argument is invisible at the call
// site, so it is pinned here.
func TestRenameBudget_MatchesInTreePrecedent(t *testing.T) {
	if renameRetries < 40 {
		t.Errorf("renameRetries = %d, want >= 40 (the in-tree precedent, under lighter load)", renameRetries)
	}
	if renameRetryDelay > 5*time.Millisecond {
		t.Errorf("renameRetryDelay = %v, want <= 5ms (the in-tree precedent)", renameRetryDelay)
	}
	if total := time.Duration(renameRetries) * renameRetryDelay; total < 200*time.Millisecond || total > time.Second {
		t.Errorf("total budget = %v, want between 200ms and 1s", total)
	}
}

// TestRenameOver_RetryIsBounded: the loop must terminate and surface the last
// error rather than hang a link pass forever.
func TestRenameOver_RetryIsBounded(t *testing.T) {
	f := &fakeRename{results: []error{errDenied}}
	err := f.ops(allRecoverable).renameOver("tmp", "dst")
	if !errors.Is(err, errDenied) {
		t.Fatalf("err = %v, want it to wrap %v", err, errDenied)
	}
	if want := renameRetries + 1; f.calls != want {
		t.Fatalf("rename calls = %d, want %d", f.calls, want)
	}
	if len(f.sleeps) != renameRetries {
		t.Fatalf("sleeps = %d, want %d", len(f.sleeps), renameRetries)
	}
}

// TestRenameOver_ExhaustionErrorNamesTheAttemptCount is F9. Without it, a
// budget that is too small and a retry loop that never ran produce byte-
// identical CI output ("Access is denied"), and the next person debugging
// windows-latest cannot tell which they are looking at.
func TestRenameOver_ExhaustionErrorNamesTheAttemptCount(t *testing.T) {
	f := &fakeRename{results: []error{errDenied}}
	err := f.ops(allRecoverable).renameOver("tmp", "dst")
	if err == nil {
		t.Fatal("want an error")
	}
	if !errors.Is(err, errDenied) {
		t.Fatalf("err no longer unwraps to the cause: %v", err)
	}
	msg := err.Error()
	if !strings.Contains(msg, "attempts") {
		t.Fatalf("error %q does not report that a retry budget was exhausted", msg)
	}
	if !strings.Contains(msg, "41") {
		t.Fatalf("error %q does not name the attempt count (41)", msg)
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
	f := &fakeRename{results: failN(2), readOnly: true, roSetErr: errors.New("nope")}
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
	if len(f.warns) != 0 {
		t.Fatalf("warns = %v, want none — nothing was actually cleared", f.warns)
	}
}

// TestDefaultRenameOps_Wired guards against the driver being reachable only
// through the fakes: renameAtomic must actually run the real primitives.
func TestDefaultRenameOps_Wired(t *testing.T) {
	if defaultRenameOps.rename == nil || defaultRenameOps.isReadOnly == nil ||
		defaultRenameOps.setReadOnly == nil || defaultRenameOps.recoverable == nil ||
		defaultRenameOps.sleep == nil || defaultRenameOps.warn == nil {
		t.Fatal("defaultRenameOps has a nil primitive")
	}
}

// TestWriteFile_RoutesThroughRenameAtomic is the one test that can notice
// WriteFile going back to a bare os.Rename.
//
// On unix renameAtomic IS a single os.Rename — the recovery is compiled to
// constant-false predicates — so no black-box test off Windows can tell the
// two apart, and a revert would sail through the whole suite here and only
// resurface as lost writes on windows-latest. Intercepting the primitive is
// the only way to bind the wiring on the platform we can actually run.
//
// It mutates a package global without synchronisation. See the file header:
// nothing in this package may call t.Parallel, and
// TestNoParallelTestsInThisPackage enforces that.
func TestWriteFile_RoutesThroughRenameAtomic(t *testing.T) {
	orig := defaultRenameOps.rename
	t.Cleanup(func() { defaultRenameOps.rename = orig })

	calls := 0
	defaultRenameOps.rename = func(oldpath, newpath string) error {
		calls++
		return orig(oldpath, newpath)
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "out.json")
	if err := WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if calls != 1 {
		t.Fatalf("renameAtomic rename calls = %d, want 1 — WriteFile is not going through "+
			"renameAtomic, so the Windows recovery in rename.go is bypassed", calls)
	}
}

// TestNoParallelTestsInThisPackage protects the unsynchronised global swap in
// TestWriteFile_RoutesThroughRenameAtomic, which is the ONLY test that can
// catch WriteFile reverting to a bare os.Rename and therefore must not be
// weakened. Other tests in this package launch 8 concurrent WriteFile
// goroutines; `go test -race` is green only because Go runs a package's tests
// sequentially unless they opt in. The first parallel opt-in added here would
// make that silently untrue, so it is refused rather than merely documented.
//
// The needle is assembled at run time so this file — which is itself scanned —
// contains no literal occurrence of it, and so the guard cannot pass merely by
// exempting itself.
func TestNoParallelTestsInThisPackage(t *testing.T) {
	needle := "t.Paralle" + "l("

	ents, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	scanned, sawSelf := 0, false
	for _, e := range ents {
		if e.IsDir() || !strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		b, err := os.ReadFile(e.Name())
		if err != nil {
			t.Fatal(err)
		}
		scanned++
		if e.Name() == "rename_test.go" {
			sawSelf = true
		}
		if strings.Contains(string(b), needle) {
			t.Errorf("%s opts into parallelism: TestWriteFile_RoutesThroughRenameAtomic swaps "+
				"defaultRenameOps.rename unsynchronised while other tests run concurrent "+
				"WriteFile goroutines. Guard the global before adding parallelism.", e.Name())
		}
	}
	// A guard that scanned nothing — or that skipped the file holding the
	// global swap — proves nothing.
	if scanned == 0 || !sawSelf {
		t.Fatalf("guard scanned %d test files, saw rename_test.go = %v — wrong working directory?",
			scanned, sawSelf)
	}
}
