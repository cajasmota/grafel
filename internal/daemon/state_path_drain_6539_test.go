package daemon

// Tests for the #6539 handle leak: readDirBounded used to return on
// timeout and ABANDON the goroutine running os.ReadDir. os.ReadDir holds
// an open directory handle for its whole duration, so a timed-out read
// kept a directory open for an unbounded time after the call returned.
//
// Why these assertions are counter-based rather than handle-based: the
// leak is invisible on the platforms grafel is developed on. On Windows
// an open handle blocks deletion, so the leak surfaces as RemoveAll
// failing; on Linux and macOS unlinking an open directory is permitted,
// so nothing observable happens at all. outstandingReadCount() fails
// identically everywhere, which is the only guard that can hold this
// fix in place.
//
// Determinism: none of these tests races the clock. Every injected read
// returns the instant readDirBounded cancels it, and the assertion is
// made after readDirBounded has already returned — so the drain has
// either happened or the test fails. Nothing here depends on a
// goroutine winning a scheduling race.

import (
	"errors"
	"os"
	"path/filepath"
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

// requireNoOutstandingReads is the teardown assertion the issue asks for.
func requireNoOutstandingReads(t *testing.T, where string) {
	t.Helper()
	if got := outstandingReadCount(); got != 0 {
		t.Errorf("%s: outstandingReadCount() = %d, want 0 — a timed-out read was abandoned and is still holding its directory handle (#6539)", where, got)
	}
}

// TestReadDirBoundedTimeoutDrainsCancelledRead is the core regression.
// It also carries the positive control: while the read is in flight the
// counter must read 1, so a counter that is never incremented cannot
// pass by being trivially zero.
func TestReadDirBoundedTimeoutDrainsCancelledRead(t *testing.T) {
	if got := outstandingReadCount(); got != 0 {
		t.Fatalf("precondition: outstandingReadCount() = %d before any read, want 0", got)
	}
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
	if got := <-inFlight; got != 1 {
		t.Errorf("positive control: outstandingReadCount() during the read = %d, want 1 (the counter must actually count)", got)
	}
	requireNoOutstandingReads(t, "after a timed-out readDirBounded")
}

// TestReadDirBoundedTimeoutDrainsReadThatErrors varies the cancelled
// read's OUTCOME. A read that gives up with an error must be drained
// exactly like one that gives up with nil entries — the drain must not
// be conditional on what the abandoned read was about to return.
func TestReadDirBoundedTimeoutDrainsReadThatErrors(t *testing.T) {
	swapReadDir(t, func(_ string, cancel <-chan struct{}) ([]os.DirEntry, error) {
		<-cancel
		return nil, errors.New("read aborted by cancel")
	})

	if _, _, ok := readDirBounded(t.TempDir(), 20*time.Millisecond); ok {
		t.Fatalf("readDirBounded reported completion; the injected read blocks past the timeout")
	}
	requireNoOutstandingReads(t, "after a timed-out read that returned an error")
}

// TestReadDirBoundedFastReadLeavesNoOutstandingRead varies the TIMING
// axis in the other direction: a read that beats the timeout must also
// leave the counter at zero by the time readDirBounded returns. This
// pins the ordering choice in readDirBounded (decrement before the send)
// — without it the success path can return while the counter still
// reads 1.
func TestReadDirBoundedFastReadLeavesNoOutstandingRead(t *testing.T) {
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
	requireNoOutstandingReads(t, "after a readDirBounded that completed in time")
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
	requireNoOutstandingReads(t, "after canonicalizePath timed out on every ancestor")
}

// TestCancellableReadDirMatchesOsReadDir pins the shipped readDirFunc as
// a faithful stand-in for os.ReadDir. The #6539 fix replaces os.ReadDir
// with a hand-rolled open/read/close, and os.ReadDir's documented
// contract is entries sorted by filename — f.ReadDir(-1) is not sorted.
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

// TestReadDirBoundedDrainsRepeatedTimeouts varies the COUNT axis: the
// counter must return to zero after many timed-out reads, not merely
// after one. A drain that fires only on the first timeout, or that
// decrements once for several increments, leaves a growing backlog —
// exactly the shape canonicalizePath's per-ancestor walk produces.
func TestReadDirBoundedDrainsRepeatedTimeouts(t *testing.T) {
	swapReadDir(t, func(_ string, cancel <-chan struct{}) ([]os.DirEntry, error) {
		<-cancel
		return nil, nil
	})

	dir := t.TempDir()
	for i := 0; i < 5; i++ {
		if _, _, ok := readDirBounded(dir, 20*time.Millisecond); ok {
			t.Fatalf("iteration %d: readDirBounded reported completion", i)
		}
		if got := outstandingReadCount(); got != 0 {
			t.Fatalf("iteration %d: outstandingReadCount() = %d, want 0 (#6539)", i, got)
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

	before := outstandingReadCount()
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
	// to zero for the next test in this package.
	deadline := time.Now().Add(30 * time.Second)
	for outstandingReadCount() != before {
		if time.Now().After(deadline) {
			t.Fatalf("uncancellable read never returned; counter stuck at %d, want %d", outstandingReadCount(), before)
		}
		time.Sleep(time.Millisecond)
	}
}
