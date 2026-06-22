package daemon

// Tests for the canonicalizePath ReadDir timeout guard (#5330).
//
// Root cause: canonicalizePath walks each path ancestor and does a
// blocking os.ReadDir on it with no timeout. If one ancestor's FS call
// hangs (an iCloud/Spotlight/TCC stall, a slow/unresponsive mount, or a
// launchd-context permission stall) the ENTIRE daemon startup deadlocks
// forever. The fix bounds each ReadDir with a timeout and, on timeout,
// degrades to preserving the input casing — the exact same fallback the
// code already takes on a read error.
//
// These tests inject a slow/blocking readDirFunc so the timeout path is
// exercised deterministically, with no real stuck filesystem required.

import (
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// withReadDirFunc swaps readDirFunc for the duration of a test and
// restores it afterwards.
//
// The injected function f is given a `stop` channel and is wrapped so
// every in-flight invocation is tracked by a WaitGroup. On a timeout the
// production readDirBounded abandons the goroutine running readDirFunc,
// so that goroutine can outlive the test body. Restoring the package var
// (or letting the test mutate shared state) while such an abandoned
// goroutine is still executing readDirFunc is an unsynchronized data
// race (#5346).
//
// The single cleanup registered here closes `stop` FIRST (releasing any
// closure that parks waiting for teardown) and only THEN drains the
// WaitGroup before restoring readDirFunc. Owning both the release signal
// and the drain in one cleanup avoids any LIFO-ordering footgun between
// separate cleanups: by the time readDirFunc is written, no abandoned
// goroutine can still be reading it.
func withReadDirFunc(t *testing.T, f func(stop <-chan struct{}, dir string) ([]os.DirEntry, error)) {
	t.Helper()
	var wg sync.WaitGroup
	stop := make(chan struct{})
	prev := readDirFunc
	readDirFunc = func(dir string) ([]os.DirEntry, error) {
		wg.Add(1)
		defer wg.Done()
		return f(stop, dir)
	}
	t.Cleanup(func() {
		close(stop) // release any closure parked until teardown
		wg.Wait()   // drain all in-flight invocations before the write below
		readDirFunc = prev
	})
}

// TestCanonicalizePathTimesOutAndDegrades verifies that when os.ReadDir
// blocks for far longer than the timeout, canonicalizePath returns
// promptly (well under the block duration) with the casing-preserving
// fallback rather than hanging.
func TestCanonicalizePathTimesOutAndDegrades(t *testing.T) {
	clearCanonicalCache()
	// 20ms timeout; ReadDir blocks for 10s. If the guard works the call
	// returns in ~20ms, not 10s.
	t.Setenv("GRAFEL_CANONICALIZE_TIMEOUT_MS", "20")
	blockStarted := make(chan struct{}, 1)
	withReadDirFunc(t, func(stop <-chan struct{}, _ string) ([]os.DirEntry, error) {
		select {
		case blockStarted <- struct{}{}:
		default:
		}
		// Block far longer than the 20ms timeout so the guard fires, but
		// wake promptly at teardown via stop.
		select {
		case <-stop:
		case <-time.After(10 * time.Second):
		}
		return nil, nil
	})

	const input = "/tmp/grafel-5330/SlowMount/Repo"
	done := make(chan string, 1)
	start := time.Now()
	go func() { done <- canonicalizePath(input) }()

	select {
	case got := <-done:
		elapsed := time.Since(start)
		if elapsed > 2*time.Second {
			t.Fatalf("canonicalizePath took %v; expected prompt return under the 10s block", elapsed)
		}
		// On timeout we preserve input casing → output equals the input.
		if got != input {
			t.Errorf("canonicalizePath(%q) = %q, want casing-preserving fallback %q", input, got, input)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("canonicalizePath did not return within 3s — it deadlocked on the slow ReadDir (#5330 regression)")
	}
	<-blockStarted // sanity: the blocking ReadDir actually ran
}

// TestCanonicalizePathFastReadDirCanonicalizes verifies the normal path:
// a fast ReadDir that returns the real on-disk entry name canonicalizes
// the segment's casing.
func TestCanonicalizePathFastReadDirCanonicalizes(t *testing.T) {
	clearCanonicalCache()
	withReadDirFunc(t, func(_ <-chan struct{}, dir string) ([]os.DirEntry, error) {
		// Defer to the real ReadDir; this is fast and well under any timeout.
		return os.ReadDir(dir)
	})

	dir := t.TempDir()
	got := canonicalizePath(dir)
	gotInfo, err := os.Stat(got)
	if err != nil {
		t.Fatalf("canonicalized path %q does not exist: %v", got, err)
	}
	wantInfo, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("original path %q does not exist: %v", dir, err)
	}
	if !os.SameFile(gotInfo, wantInfo) {
		t.Errorf("canonicalizePath(%q) = %q, not the same inode", dir, got)
	}
}

// TestCanonicalizePathTimeoutResultIsCached verifies the cache still
// works after a timeout: a second call returns the cached fallback
// without invoking ReadDir again (so it can't block twice).
func TestCanonicalizePathTimeoutResultIsCached(t *testing.T) {
	clearCanonicalCache()
	t.Setenv("GRAFEL_CANONICALIZE_TIMEOUT_MS", "20")

	// calls is read by the test body while abandoned timeout goroutines
	// may still be incrementing it, so it must be atomic (#5346).
	var calls atomic.Int64
	withReadDirFunc(t, func(stop <-chan struct{}, _ string) ([]os.DirEntry, error) {
		calls.Add(1)
		<-stop // block until teardown releases us
		return nil, nil
	})

	const input = "/tmp/grafel-5330-cache/SlowMount/Repo"
	first := canonicalizePath(input)
	if _, ok := canonicalCache.Load(input); !ok {
		t.Fatal("expected timeout result to be cached")
	}
	callsAfterFirst := calls.Load()
	second := canonicalizePath(input)
	if first != second {
		t.Errorf("cached call returned different value: first=%q second=%q", first, second)
	}
	if got := calls.Load(); got != callsAfterFirst {
		t.Errorf("second call invoked readDirFunc again (%d → %d); cache not used", callsAfterFirst, got)
	}
}

// TestCanonicalizeTimeoutEnvOverride verifies the env-override parsing
// and that zero / negative / invalid values fall back to the default.
func TestCanonicalizeTimeoutEnvOverride(t *testing.T) {
	cases := []struct {
		name string
		env  string
		want time.Duration
	}{
		{"unset", "", defaultCanonicalizeTimeout},
		{"valid", "1500", 1500 * time.Millisecond},
		{"zero", "0", defaultCanonicalizeTimeout},
		{"negative", "-5", defaultCanonicalizeTimeout},
		{"garbage", "abc", defaultCanonicalizeTimeout},
		{"empty", "", defaultCanonicalizeTimeout},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.env == "" {
				os.Unsetenv("GRAFEL_CANONICALIZE_TIMEOUT_MS")
			} else {
				t.Setenv("GRAFEL_CANONICALIZE_TIMEOUT_MS", tc.env)
			}
			if got := canonicalizeTimeout(); got != tc.want {
				t.Errorf("canonicalizeTimeout() with env %q = %v, want %v", tc.env, got, tc.want)
			}
		})
	}
}
