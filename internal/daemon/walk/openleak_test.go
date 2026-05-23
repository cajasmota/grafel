// openleak_test.go — #1723 regression coverage.
//
// #1722 (cf113fd) fixed the *caller* hang by wrapping os.Open in a goroutine
// + select. But the wrapped worker goroutine itself stayed blocked forever
// on the wedged syscall.Open. In real reindexes thousands of those workers
// accumulated and the daemon eventually became unresponsive (no fds left,
// scheduler thrash, eventually a crash).
//
// These tests force ParseIgnoreFile to hit the deadline path repeatedly and
// confirm the goroutine count stays bounded — i.e. no leak per call.
package walk

import (
	"path/filepath"
	"runtime"
	"sync"
	"syscall"
	"testing"
	"time"
)

// TestOpenWithDeadline_NoGoroutineLeak — the headline #1723 regression.
//
// Strategy: point ParseIgnoreFile at a path that previously caused workers
// to leak (FIFO with no writer — see fsevents_hang_test.go). Run many
// iterations and verify the goroutine count returns to ~baseline. Under
// the old #1722 code each call leaked one goroutine forever; with the
// #1723 fix the lstat short-circuit + bounded semaphore guarantee zero
// permanent leaks.
func TestOpenWithDeadline_NoGoroutineLeak(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("FIFO test requires POSIX mkfifo; skipping on Windows")
	}

	dir := t.TempDir()
	fifoPath := filepath.Join(dir, ".gitignore")
	if err := syscall.Mkfifo(fifoPath, 0o600); err != nil {
		t.Fatalf("mkfifo: %v", err)
	}

	// Settle goroutine baseline.
	runtime.GC()
	time.Sleep(50 * time.Millisecond)
	baseline := runtime.NumGoroutine()

	const iterations = 200
	for i := 0; i < iterations; i++ {
		ig, err := ParseIgnoreFile("", fifoPath, ".gitignore")
		if err != ErrIgnoreFileTimeout {
			t.Fatalf("iter %d: expected ErrIgnoreFileTimeout, got %v", i, err)
		}
		if ig == nil {
			t.Fatalf("iter %d: expected non-nil IgnoreFile", i)
		}
	}

	// Give any short-lived helpers time to exit.
	runtime.GC()
	time.Sleep(100 * time.Millisecond)
	after := runtime.NumGoroutine()

	// Allow a small slack for the scheduler / pprof goroutines, but the
	// drift must NOT scale with iteration count. Under #1722 we would
	// see `after >= baseline + iterations`. Under the fix it should be
	// within a handful of goroutines of baseline.
	const slack = 5
	if after > baseline+slack {
		t.Fatalf("goroutine leak: baseline=%d after=%d iterations=%d slack=%d",
			baseline, after, iterations, slack)
	}
	t.Logf("baseline=%d after=%d iterations=%d (no leak)", baseline, after, iterations)
}

// TestOpenWithDeadline_NoLeakUnderConcurrency hammers the function from
// multiple goroutines simultaneously. Even concurrent timeouts must not
// accumulate workers permanently.
func TestOpenWithDeadline_NoLeakUnderConcurrency(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("FIFO test requires POSIX mkfifo; skipping on Windows")
	}

	dir := t.TempDir()
	fifoPath := filepath.Join(dir, ".gitignore")
	if err := syscall.Mkfifo(fifoPath, 0o600); err != nil {
		t.Fatalf("mkfifo: %v", err)
	}

	runtime.GC()
	time.Sleep(50 * time.Millisecond)
	baseline := runtime.NumGoroutine()

	const workers = 16
	const perWorker = 50
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < perWorker; i++ {
				_, _ = ParseIgnoreFile("", fifoPath, ".gitignore")
			}
		}()
	}
	wg.Wait()

	runtime.GC()
	time.Sleep(150 * time.Millisecond)
	after := runtime.NumGoroutine()

	const slack = 10
	if after > baseline+slack {
		t.Fatalf("goroutine leak under concurrency: baseline=%d after=%d total_calls=%d",
			baseline, after, workers*perWorker)
	}
	t.Logf("baseline=%d after=%d total_calls=%d (no leak)", baseline, after, workers*perWorker)
}

// TestOpenWithDeadline_RegularFileFastPath confirms that the lstat+nonblock
// open path returns a fully-readable file for a normal regular file (the
// 99.99% case). This guards against the fix accidentally breaking the
// happy path while solving the leak.
func TestOpenWithDeadline_RegularFileFastPath(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, ".gitignore")
	body := "node_modules/\n*.log\n"
	if err := writeFile(p, body); err != nil {
		t.Fatalf("write: %v", err)
	}

	ig, err := ParseIgnoreFile("", p, ".gitignore")
	if err != nil {
		t.Fatalf("ParseIgnoreFile: %v", err)
	}
	if ig == nil || len(ig.patterns) != 2 {
		t.Fatalf("expected 2 patterns, got %#v", ig)
	}
}

func writeFile(path, body string) error {
	// minimal helper, avoid pulling in os in the test file twice
	return syscallWriteFile(path, body)
}

func syscallWriteFile(path, body string) error {
	fd, err := syscall.Open(path, syscall.O_WRONLY|syscall.O_CREAT|syscall.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	defer syscall.Close(fd)
	_, err = syscall.Write(fd, []byte(body))
	return err
}
