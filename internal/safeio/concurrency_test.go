package safeio

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// blockingOpener is the only honest way to test this layer on a GOOS where a
// blocking open cannot be created. Windows has no mkfifo, so without an
// injected opener the machinery that has to work on windows-latest would be
// asserted-safe by comment only — which is precisely what the review found.
func blockingOpener(gate <-chan struct{}, inFlight, peak *int64) func(string) (*os.File, error) {
	return func(string) (*os.File, error) {
		n := atomic.AddInt64(inFlight, 1)
		for {
			old := atomic.LoadInt64(peak)
			if n <= old || atomic.CompareAndSwapInt64(peak, old, n) {
				break
			}
		}
		defer atomic.AddInt64(inFlight, -1)
		<-gate
		return nil, errors.New("unblocked")
	}
}

// TestOpenDoesNotRefuseRegularFilesUnderConcurrency is the F2 regression. The
// acquire used to be a non-blocking select with a default arm; measured on 400
// ordinary regular files it refused 335 of them, and because internal/secrets
// maps an open error to "no findings", that surfaced as a scanner calling a
// tree clean after reading 16% of it. Not one refusal is acceptable here: a
// bounded-concurrency limiter that refuses work is not a limiter.
func TestOpenDoesNotRefuseRegularFilesUnderConcurrency(t *testing.T) {
	root := t.TempDir()
	const n = 400
	paths := make([]string, n)
	for i := range paths {
		paths[i] = filepath.Join(root, "f"+strconv.Itoa(i)+".txt")
		if err := os.WriteFile(paths[i], []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	var mu sync.Mutex
	var refused, failed int
	var wg sync.WaitGroup
	for _, p := range paths {
		wg.Add(1)
		go func(p string) {
			defer wg.Done()
			f, err := Open(p, FollowSymlinks)
			mu.Lock()
			defer mu.Unlock()
			switch {
			case errors.Is(err, ErrWouldBlock):
				refused++
			case err != nil:
				failed++
			default:
				_ = f.Close()
			}
		}(p)
	}
	wg.Wait()
	if refused != 0 || failed != 0 {
		t.Errorf("of %d regular files, %d refused with ErrWouldBlock and %d otherwise failed; want 0 and 0", n, refused, failed)
	}
}

// TestSemaphoreBoundsConcurrentOpens is the falsifiability the review asked
// for. The semaphore previously survived a mutant that deleted it outright
// (`_ = openSlots`) with safeio, install/detect and secrets all green, which
// made it a guard nobody could trust. This asserts the bound itself: with 4x
// more opens in flight than there are slots, the peak concurrency must equal
// the slot count, not the caller count.
func TestSemaphoreBoundsConcurrentOpens(t *testing.T) {
	gate := make(chan struct{})
	var inFlight, peak int64
	opener := blockingOpener(gate, &inFlight, &peak)

	slots := cap(openSlots)
	callers := slots * 4
	var wg sync.WaitGroup
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = openWithDeadline("x", opener, 2*time.Second)
		}()
	}

	// Let the first wave saturate before measuring.
	deadline := time.Now().Add(2 * time.Second)
	for atomic.LoadInt64(&inFlight) < int64(slots) && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	got := atomic.LoadInt64(&peak)
	close(gate)
	wg.Wait()

	if got > int64(slots) {
		t.Errorf("peak concurrent opens = %d with only %d slots: the semaphore is not bounding anything", got, slots)
	}
	if got < int64(slots) {
		t.Errorf("peak concurrent opens = %d with %d slots and %d callers: opens are being serialised far below the bound", got, slots, callers)
	}
}

// TestAbandonedWorkerReturnsItsSlot is the F3 regression, and it is the one
// that bricked the package. The release lived after the send to the result
// channel on a path a timed-out worker never reaches, so the comment claiming
// "the semaphore slot is released by the worker if it ever unblocks" described
// a release that by construction never happened. With the slots exhausted by
// abandoned workers, a perfectly regular file failed with ErrWouldBlock —
// safeio was dead process-wide, silently, permanently.
func TestAbandonedWorkerReturnsItsSlot(t *testing.T) {
	gate := make(chan struct{})
	var inFlight, peak int64
	opener := blockingOpener(gate, &inFlight, &peak)

	// Abandon every slot: each caller times out while its worker is still
	// parked in the fake open.
	var wg sync.WaitGroup
	for i := 0; i < cap(openSlots); i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := openWithDeadline("x", opener, 50*time.Millisecond); !errors.Is(err, ErrWouldBlock) {
				t.Errorf("abandoned open = %v, want ErrWouldBlock", err)
			}
		}()
	}
	wg.Wait()

	// Now let the workers unblock. Every one of them must hand its slot back.
	close(gate)

	root := t.TempDir()
	p := filepath.Join(root, "ok.txt")
	if err := os.WriteFile(p, []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		f, err := Open(p, FollowSymlinks)
		if f != nil {
			_ = f.Close()
		}
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("a regular file failed with %v after the abandoned workers unblocked: safeio is bricked process-wide", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Open blocked after the abandoned workers unblocked: their slots were never returned")
	}
}

// TestCallerIsBoundedEvenWhenTheOpenNeverReturns pins what the deadline
// actually buys, which is not what the package used to claim. The worker
// cannot be rescued — nothing can interrupt a thread parked in open(2) — so
// the only guarantee on offer is that the CALLER returns. This asserts that
// guarantee directly, on every GOOS.
func TestCallerIsBoundedEvenWhenTheOpenNeverReturns(t *testing.T) {
	gate := make(chan struct{})
	defer close(gate) // let the worker exit so the slot comes back
	var inFlight, peak int64
	opener := blockingOpener(gate, &inFlight, &peak)

	done := make(chan error, 1)
	start := time.Now()
	go func() {
		_, err := openWithDeadline("x", opener, 100*time.Millisecond)
		done <- err
	}()
	select {
	case err := <-done:
		if !errors.Is(err, ErrWouldBlock) {
			t.Errorf("open that never returns = %v, want ErrWouldBlock", err)
		}
		if elapsed := time.Since(start); elapsed > 3*time.Second {
			t.Errorf("caller took %v to give up on a 100ms deadline", elapsed)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the caller was not bounded: openWithDeadline never returned")
	}
}

// TestDescriptorTypeGateIsUnraceable pins layer 2 on EVERY GOOS, including the
// windows-latest job in .github/workflows/test.yml, where the only test that
// covered it was //go:build unix. It drives nonBlockingOpen directly, past the
// stat gate, which is the only way to reach the TOCTOU residual.
//
// A directory is the vehicle because it is the one non-regular entry every
// platform can create, and because os.Open and O_RDONLY|O_NONBLOCK both SUCCEED
// on one — so a refusal here can only have come from asking the descriptor
// what it is holding.
func TestDescriptorTypeGateIsUnraceable(t *testing.T) {
	dir := t.TempDir()
	f, err := nonBlockingOpen(dir)
	if f != nil {
		_ = f.Close()
	}
	if !errors.Is(err, ErrNotRegular) {
		t.Errorf("nonBlockingOpen on a directory = %v, want ErrNotRegular; the descriptor-level type check is the TOCTOU layer and it is not running", err)
	}
	if err == nil || !contains(err.Error(), "directory") {
		t.Errorf("refusal %v does not name the kind it refused", err)
	}

	reg := filepath.Join(dir, "ok.txt")
	if werr := os.WriteFile(reg, []byte("hi"), 0o644); werr != nil {
		t.Fatal(werr)
	}
	g, gerr := nonBlockingOpen(reg)
	if gerr != nil {
		t.Fatalf("nonBlockingOpen on a regular file = %v, want success", gerr)
	}
	b := make([]byte, 2)
	if n, rerr := g.Read(b); n != 2 || rerr != nil {
		t.Errorf("read from the returned descriptor = %d, %v; want 2, nil", n, rerr)
	}
	_ = g.Close()
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
