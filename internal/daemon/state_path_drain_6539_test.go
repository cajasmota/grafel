package daemon

// Tests for the #6539 handle leak: readDirBounded used to return on
// timeout and ABANDON the goroutine running os.ReadDir. os.ReadDir holds
// an open directory handle for its whole duration, so a timed-out read
// kept a directory open for an unbounded time after the call returned.
//
// There are two separate things to hold down here, and conflating them
// is how the first cut of this fix shipped a no-op:
//
//   - the DRAIN PROTOCOL — readDirBounded cancels and waits, so the
//     counter is back to its baseline when it returns. Exercised with
//     injected reads that return on <-cancel.
//   - the HANDLE ITSELF — cancellableReadDir must actually stop reading
//     and let go of the fd. A counter can reach zero while a descriptor
//     stays open, so this needs assertions on the REAL primitive, not on
//     fakes that honour cancel by construction.
//
// Why counter-based assertions at all: the leak is invisible on the
// platforms grafel is developed on. On Windows an open handle blocks
// deletion, so it surfaces as RemoveAll failing; on Linux and macOS
// unlinking an open directory is permitted, so nothing observable
// happens. The counter fails identically everywhere.
//
// Determinism: nothing here races the clock. Every injected read returns
// the instant readDirBounded cancels it; every cancellation test on the
// real primitive uses an ALREADY-CLOSED channel; and every assertion is
// made after the call under test has returned.

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"
)

// swapReadDir installs f as readDirFunc for the test, with no WaitGroup
// backstop: these tests assert that readDirBounded itself drains the
// read, so borrowing the drain from the harness would assert nothing.
func swapReadDir(t *testing.T, f func(dir string, cancel <-chan struct{}) ([]os.DirEntry, error)) {
	t.Helper()
	prev := readDirFunc
	readDirFunc = f
	t.Cleanup(func() { readDirFunc = prev })
}

// readsBaseline captures outstandingReadCount at the start of a test.
//
// Every assertion below is RELATIVE to it. outstandingReads is a package
// global, so an absolute "want 0" makes each test report leaks that some
// other test caused — and it would flake outright the moment anything in
// this package calls t.Parallel() while touching canonicalizePath. A
// relative assertion pins the only thing this test is responsible for:
// the read IT started got drained.
func readsBaseline(t *testing.T) int64 {
	t.Helper()
	return outstandingReadCount()
}

// requireDrainedTo is the teardown assertion the issue asks for.
func requireDrainedTo(t *testing.T, before int64, where string) {
	t.Helper()
	if got := outstandingReadCount(); got != before {
		t.Errorf("%s: outstandingReadCount() = %d, want %d — a timed-out read was abandoned and is still holding its directory handle (#6539)", where, got, before)
	}
}

// makeDirWithEntries builds a directory holding n files, named so that
// creation order is not lexical order.
func makeDirWithEntries(t *testing.T, n int) string {
	t.Helper()
	dir := t.TempDir()
	for i := 0; i < n; i++ {
		name := fmt.Sprintf("f%06d", (i*7919)%n) // scrambled, still unique
		if err := os.WriteFile(filepath.Join(dir, name), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// ---------------------------------------------------------------------
// The drain protocol
// ---------------------------------------------------------------------

// TestReadDirBoundedTimeoutDrainsCancelledRead is the core regression.
// It also carries the positive control: while the read is in flight the
// counter must read one ABOVE the baseline, so a counter that is never
// incremented cannot pass by being trivially unchanged.
func TestReadDirBoundedTimeoutDrainsCancelledRead(t *testing.T) {
	before := readsBaseline(t)
	inFlight := make(chan int64, 1)
	swapReadDir(t, func(_ string, cancel <-chan struct{}) ([]os.DirEntry, error) {
		inFlight <- outstandingReadCount()
		<-cancel // honour the production cancel signal
		return nil, nil
	})

	entries, err, ok := readDirBounded(t.TempDir(), 20*time.Millisecond)
	if ok {
		t.Fatalf("readDirBounded reported completion; the injected read blocks past the timeout")
	}
	if entries != nil || err != nil {
		t.Errorf("timed-out read returned entries=%v err=%v, want nil/nil", entries, err)
	}
	if got := <-inFlight; got != before+1 {
		t.Errorf("positive control: outstandingReadCount() during the read = %d, want %d (the counter must actually count)", got, before+1)
	}
	requireDrainedTo(t, before, "after a timed-out readDirBounded")
}

// TestReadDirBoundedTimeoutDrainsReadThatErrors varies the cancelled
// read's OUTCOME. A read that gives up with an error must be drained
// exactly like one that gives up with nil entries — the drain must not
// be conditional on what the abandoned read was about to return.
func TestReadDirBoundedTimeoutDrainsReadThatErrors(t *testing.T) {
	before := readsBaseline(t)
	swapReadDir(t, func(_ string, cancel <-chan struct{}) ([]os.DirEntry, error) {
		<-cancel
		return nil, errors.New("read aborted by cancel")
	})

	if _, _, ok := readDirBounded(t.TempDir(), 20*time.Millisecond); ok {
		t.Fatalf("readDirBounded reported completion; the injected read blocks past the timeout")
	}
	requireDrainedTo(t, before, "after a timed-out read that returned an error")
}

// TestReadDirBoundedFastReadLeavesNoOutstandingRead varies the TIMING
// axis in the other direction: a read that beats the timeout must also
// be accounted for by the time readDirBounded returns.
//
// What this does NOT pin, stated so nobody reads more into it: it does
// not pin readDirBounded's choice to decrement BEFORE the channel send.
// That ordering is a real guarantee — a receive from ch happens-after
// the decrement under the Go memory model — but moving the decrement
// after the send only widens a window this test has no way to force,
// so such a mutant SURVIVES. The claim is documented at the production
// site and deliberately not asserted here.
func TestReadDirBoundedFastReadLeavesNoOutstandingRead(t *testing.T) {
	before := readsBaseline(t)
	want := []os.DirEntry{}
	swapReadDir(t, func(dir string, cancel <-chan struct{}) ([]os.DirEntry, error) {
		return want, nil
	})

	entries, err, ok := readDirBounded(t.TempDir(), 5*time.Second)
	if !ok {
		t.Fatalf("readDirBounded timed out on an instant read")
	}
	if err != nil {
		t.Fatalf("readDirBounded returned err = %v, want nil", err)
	}
	if len(entries) != 0 {
		t.Errorf("readDirBounded returned %d entries, want 0", len(entries))
	}
	requireDrainedTo(t, before, "after a readDirBounded that completed in time")
}

// TestCanonicalizePathTimeoutLeavesNoOutstandingReads varies the ENTRY
// POINT: the leak's blast radius comes from canonicalizePath's
// per-segment ancestor walk, where every ancestor gets its own bounded
// read. A drain that works when readDirBounded is called directly but
// leaks somewhere in the walk would still pin handles in production.
//
// It also re-asserts the #5330 degrade contract, so a "fix" that drains
// by removing the timeout altogether cannot pass.
func TestCanonicalizePathTimeoutLeavesNoOutstandingReads(t *testing.T) {
	clearCanonicalCache()
	t.Setenv("GRAFEL_CANONICALIZE_TIMEOUT_MS", "20")
	before := readsBaseline(t)

	swapReadDir(t, func(_ string, cancel <-chan struct{}) ([]os.DirEntry, error) {
		<-cancel
		return nil, nil
	})

	input := filepath.Join(t.TempDir(), "SlowMount", "Repo")
	start := time.Now()
	got := canonicalizePath(input)
	elapsed := time.Since(start)

	if want := filepath.Clean(input); got != want {
		t.Errorf("canonicalizePath(%q) = %q, want the casing-preserving fallback %q (#5330)", input, got, want)
	}
	if elapsed > 30*time.Second {
		t.Errorf("canonicalizePath took %v; the bounded read is no longer bounded", elapsed)
	}
	requireDrainedTo(t, before, "after canonicalizePath timed out on every ancestor")
}

// TestReadDirBoundedDrainsRepeatedTimeouts varies the COUNT axis: the
// counter must return to baseline after many timed-out reads, not merely
// after one. A drain that fires only on the first timeout, or that
// decrements once for several increments, leaves a growing backlog —
// exactly the shape canonicalizePath's per-ancestor walk produces.
func TestReadDirBoundedDrainsRepeatedTimeouts(t *testing.T) {
	before := readsBaseline(t)
	swapReadDir(t, func(_ string, cancel <-chan struct{}) ([]os.DirEntry, error) {
		<-cancel
		return nil, nil
	})

	dir := t.TempDir()
	for i := 0; i < 5; i++ {
		if _, _, ok := readDirBounded(dir, 20*time.Millisecond); ok {
			t.Fatalf("iteration %d: readDirBounded reported completion", i)
		}
		if got := outstandingReadCount(); got != before {
			t.Fatalf("iteration %d: outstandingReadCount() = %d, want %d (#6539)", i, got, before)
		}
	}
}

// TestReadDirBoundedDrainGraceIsBounded pins the OTHER half of the
// contract, and it is the reason the drain is a grace period rather than
// an unconditional join: a read that ignores cancel (an uninterruptible
// syscall on a wedged mount) must NOT be waited on forever, or the
// #5330 startup deadlock is back. readDirBounded must still return.
func TestReadDirBoundedDrainGraceIsBounded(t *testing.T) {
	release := make(chan struct{})
	var releaseOnce sync.Once
	releaseRead := func() { releaseOnce.Do(func() { close(release) }) }
	t.Cleanup(releaseRead)
	swapReadDir(t, func(_ string, _ <-chan struct{}) ([]os.DirEntry, error) {
		<-release // deliberately ignores cancel
		return nil, nil
	})

	before := readsBaseline(t)
	done := make(chan struct{})
	go func() {
		defer close(done)
		readDirBounded(t.TempDir(), 20*time.Millisecond)
	}()
	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("readDirBounded did not return; the post-cancel drain is unbounded (#5330 regression)")
	}
	// The uncancellable read is still outstanding by construction; that is
	// the honest limit of the fix, and it is released at cleanup below.
	if got := outstandingReadCount(); got != before+1 {
		t.Errorf("outstandingReadCount() = %d, want %d — the read that ignored cancel should still be counted, not silently forgotten", got, before+1)
	}
	releaseRead()
	// Wait for the ignored read to actually finish so the counter is back
	// to baseline for the next test in this package.
	deadline := time.Now().Add(30 * time.Second)
	for outstandingReadCount() != before {
		if time.Now().After(deadline) {
			t.Fatalf("uncancellable read never returned; counter stuck at %d, want %d", outstandingReadCount(), before)
		}
		time.Sleep(time.Millisecond)
	}
}

// ---------------------------------------------------------------------
// The real primitive: cancellableReadDir
// ---------------------------------------------------------------------

// TestCancellableReadDirStopsOnCancel is the test whose absence let the
// first cut of this fix ship a no-op.
//
// Every drain test above injects a fake that returns on <-cancel, so it
// proves the PROTOCOL and never the MECHANISM. The original mechanism —
// a watchdog goroutine closing the *os.File — does not interrupt an
// in-flight ReadDir on darwin at all (Fdopendir/readdir_r owns the fd),
// and measured over an 80,000-entry directory a read with cancel already
// closed still returned all 80,000 entries. Nothing in the suite noticed.
//
// So: cancel a REAL cancellableReadDir over a directory of more than one
// chunk and require the contract this can actually observe — that a
// cancelled read yields the sentinel error and NO listing. Deterministic:
// the channel is closed before the call, so no scheduling is involved.
//
// Precisely what it does NOT observe, because the first version of this
// comment claimed it did: WHERE in the loop the cancel is noticed. A
// variant checking cancel at the BOTTOM of the loop reads a full chunk
// before noticing and still returns (0 entries, errReadDirCancelled), so
// it passes here. That property is pinned separately, by
// TestCancellableReadDirChecksCancelOnEveryChunk below.
func TestCancellableReadDirStopsOnCancel(t *testing.T) {
	dir := makeDirWithEntries(t, readDirChunk+3)

	cancel := make(chan struct{})
	close(cancel)

	entries, err := cancellableReadDir(dir, cancel)
	if !errors.Is(err, errReadDirCancelled) {
		t.Fatalf("cancellableReadDir on a cancelled read = (%d entries, %v), want errReadDirCancelled — the cancel signal is being ignored and the read runs to completion holding its handle (#6539)", len(entries), err)
	}
	if len(entries) != 0 {
		t.Errorf("cancellableReadDir returned %d entries for a cancelled read, want 0: a partial listing would let canonicalizePath recover a casing it never confirmed", len(entries))
	}
}

// TestCancellableReadDirChecksCancelOnEveryChunk pins the cancel check
// INSIDE the loop, in the permissive direction: hoisting it above the
// loop leaves only the first chunk guarded, so any directory larger than
// readDirChunk becomes uncancellable after its first 512 entries — the
// #6539 leak again, merely postponed by one chunk.
//
// Distinguishing that requires a cancel that lands MID-loop, which a
// pre-closed channel cannot do. Rather than race a sleep against a read
// (~3ms window, flaky under -race on a loaded box), the loop is observed
// directly: readDirChunkHook fires the cancel after the first chunk, and
// the shipped loop must notice on its next iteration. No sleeps, no
// scheduling — the hook is called synchronously by the code under test.
func TestCancellableReadDirChecksCancelOnEveryChunk(t *testing.T) {
	const n = readDirChunk + 3 // two chunks: 512 then 3
	dir := makeDirWithEntries(t, n)

	cancel := make(chan struct{})
	var once sync.Once
	prev := readDirChunkHook
	readDirChunkHook = func() { once.Do(func() { close(cancel) }) }
	t.Cleanup(func() { readDirChunkHook = prev })

	entries, err := cancellableReadDir(dir, cancel)
	if !errors.Is(err, errReadDirCancelled) {
		t.Fatalf("cancellableReadDir = (%d entries, %v), want errReadDirCancelled: cancel was fired after the first chunk and never noticed, so it is only checked before the loop rather than on every chunk (#6539)", len(entries), err)
	}
	if len(entries) != 0 {
		t.Errorf("cancellableReadDir returned %d entries for a cancelled read, want 0", len(entries))
	}
}

// TestCancellableReadDirReadsBeyondOneChunk pins the loop, in the
// permissive direction that chunking newly makes possible: a read that
// stops after its first chunk returns a TRUNCATED directory rather than
// an error, so canonicalizePath would silently fail to find a segment
// that is really there. Any directory larger than readDirChunk is
// affected and nothing else in the suite would notice.
func TestCancellableReadDirReadsBeyondOneChunk(t *testing.T) {
	const n = readDirChunk + 3
	dir := makeDirWithEntries(t, n)

	entries, err := cancellableReadDir(dir, make(chan struct{}))
	if err != nil {
		t.Fatalf("cancellableReadDir(%q) = err %v, want nil", dir, err)
	}
	if len(entries) != n {
		t.Fatalf("cancellableReadDir returned %d entries, want %d — the chunk loop stops early and truncates directories larger than one chunk (readDirChunk = %d)", len(entries), n, readDirChunk)
	}
	// Sorted across the chunk boundary, not merely within a chunk.
	for i := 1; i < len(entries); i++ {
		if entries[i-1].Name() >= entries[i].Name() {
			t.Fatalf("entries out of order at %d: %q then %q — os.ReadDir's contract is sorted output", i, entries[i-1].Name(), entries[i].Name())
		}
	}
}

// TestCancellableReadDirMatchesOsReadDir pins the shipped readDirFunc as
// a faithful stand-in for os.ReadDir. The #6539 fix replaces os.ReadDir
// with a hand-rolled open/read/close, and os.ReadDir's documented
// contract is entries sorted by filename — f.ReadDir(n) is not sorted.
func TestCancellableReadDirMatchesOsReadDir(t *testing.T) {
	dir := t.TempDir()
	// Created out of lexical order so an unsorted read is detectable.
	for _, name := range []string{"zeta", "alpha", "Mike", "beta"} {
		if err := os.WriteFile(filepath.Join(dir, name), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(dir, "delta"), 0o755); err != nil {
		t.Fatal(err)
	}

	want, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	got, err := cancellableReadDir(dir, make(chan struct{}))
	if err != nil {
		t.Fatalf("cancellableReadDir(%q) = err %v, want nil", dir, err)
	}
	if len(got) != len(want) {
		t.Fatalf("cancellableReadDir returned %d entries, os.ReadDir returned %d", len(got), len(want))
	}
	for i := range want {
		if got[i].Name() != want[i].Name() {
			t.Errorf("entry %d: cancellableReadDir = %q, os.ReadDir = %q (order or content diverges)", i, got[i].Name(), want[i].Name())
		}
		if got[i].IsDir() != want[i].IsDir() {
			t.Errorf("entry %q: IsDir = %v, os.ReadDir says %v", got[i].Name(), got[i].IsDir(), want[i].IsDir())
		}
	}
}

// TestCancellableReadDirReportsOpenError varies the ERROR axis of the
// shipped primitive: a directory that cannot be opened must surface the
// error rather than an empty listing, because canonicalizePath's degrade
// path is selected by that error.
func TestCancellableReadDirReportsOpenError(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "does-not-exist")
	entries, err := cancellableReadDir(missing, make(chan struct{}))
	if err == nil {
		t.Fatalf("cancellableReadDir(%q) = %v, nil; want an error", missing, entries)
	}
	if !errors.Is(err, os.ErrNotExist) {
		t.Errorf("cancellableReadDir(%q) err = %v, want os.ErrNotExist", missing, err)
	}
	if entries != nil {
		t.Errorf("cancellableReadDir returned %d entries alongside an error, want nil", len(entries))
	}
}

// TestCancellableReadDirCancelledReadsLeakNoDescriptors observes the
// HANDLE rather than the counter, which is the whole subject of #6539.
// Every other assertion in this file would stay green if cancellableReadDir
// returned promptly while leaving its fd open — that is precisely the
// gap the watchdog version fell through.
//
// /dev/fd is the process's own open-descriptor table on darwin and
// linux; it does not exist on windows, where CI covers the same code
// through the tests above.
func TestCancellableReadDirCancelledReadsLeakNoDescriptors(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skipf("no /dev/fd on %s", runtime.GOOS)
	}
	// ReadDirnames, not ReadDir: /dev/fd entries cannot be stat'd
	// individually on darwin (the descriptor a stat would use is the one
	// doing the listing), so only the names are readable.
	countFDs := func() int {
		t.Helper()
		f, err := os.Open("/dev/fd")
		if err != nil {
			t.Skipf("cannot open /dev/fd on %s: %v", runtime.GOOS, err)
		}
		defer f.Close()
		names, err := f.Readdirnames(-1)
		if err != nil {
			t.Skipf("cannot list /dev/fd on %s: %v", runtime.GOOS, err)
		}
		return len(names)
	}

	dir := makeDirWithEntries(t, readDirChunk+3)
	cancel := make(chan struct{})
	close(cancel)

	// Warm up so one-off allocations are not counted as growth.
	for i := 0; i < 8; i++ {
		cancellableReadDir(dir, cancel)
	}
	before := countFDs()
	const rounds = 64
	for i := 0; i < rounds; i++ {
		if _, err := cancellableReadDir(dir, cancel); !errors.Is(err, errReadDirCancelled) {
			t.Fatalf("round %d: err = %v, want errReadDirCancelled", i, err)
		}
	}
	after := countFDs()
	// Any per-call leak shows as growth proportional to rounds; a small
	// constant slack absorbs unrelated runtime descriptors.
	if after > before+4 {
		t.Errorf("open descriptors went %d → %d across %d cancelled reads: cancellableReadDir is leaking a directory handle per cancelled read (#6539)", before, after, rounds)
	}
}
