package daemon

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestIndexGateCapsConcurrency is the core #5493 guarantee: with cap=2, enqueue
// M index requests → at most 2 run concurrently, and all M eventually complete.
func TestIndexGateCapsConcurrency(t *testing.T) {
	const cap = 2
	const m = 30
	g := NewIndexGate(cap)

	var cur, peak int64
	var completed int64
	var wg sync.WaitGroup
	start := make(chan struct{}) // release all goroutines at once to maximize contention

	for i := 0; i < m; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			err := g.Run(context.Background(), false, func() error {
				n := atomic.AddInt64(&cur, 1)
				for {
					p := atomic.LoadInt64(&peak)
					if n <= p || atomic.CompareAndSwapInt64(&peak, p, n) {
						break
					}
				}
				// Hold the slot briefly so concurrency actually overlaps.
				time.Sleep(2 * time.Millisecond)
				atomic.AddInt64(&cur, -1)
				atomic.AddInt64(&completed, 1)
				return nil
			})
			if err != nil {
				t.Errorf("Run returned error: %v", err)
			}
		}()
	}
	close(start)
	wg.Wait()

	if got := atomic.LoadInt64(&peak); got > cap {
		t.Fatalf("peak concurrency = %d, want <= %d", got, cap)
	}
	if got := atomic.LoadInt64(&completed); got != m {
		t.Fatalf("completed = %d, want %d (all must eventually run)", got, m)
	}
	// Gate must be fully drained afterwards.
	active, queued := g.Stats()
	if active != 0 || queued != 0 {
		t.Fatalf("after drain: active=%d queued=%d, want 0/0", active, queued)
	}
}

// waitQueued blocks until the gate reports at least want queued waiters (or
// fails the test after a generous timeout). It is the deterministic alternative
// to sleeping for enqueues to "settle".
func waitQueued(t *testing.T, g *IndexGate, want int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		if _, queued := g.Stats(); queued >= want {
			return
		}
		if time.Now().After(deadline) {
			_, queued := g.Stats()
			t.Fatalf("timed out waiting for queued >= %d (got %d)", want, queued)
		}
		time.Sleep(100 * time.Microsecond)
	}
}

// TestIndexGateFIFOOrder verifies FIFO admission within a priority class: with
// cap=1, requests enqueued in order 0..N complete in that same order.
func TestIndexGateFIFOOrder(t *testing.T) {
	const n = 8
	g := NewIndexGate(1)

	// Acquire the only slot up front so every request below must queue.
	if err := g.Acquire(context.Background(), false); err != nil {
		t.Fatalf("initial acquire: %v", err)
	}

	var mu sync.Mutex
	var order []int
	var done sync.WaitGroup

	// Launch acquirers one at a time and confirm each is actually enqueued (queued
	// count reaches idx+1) before launching the next. Sleeps are NOT a reliable
	// ordering primitive — under loaded CI scheduling, goroutine i can append its
	// ticket to the gate's FIFO after goroutine i+1, scrambling the queue order
	// (observed flake: order=[0 1 3 2 5 4 6 7]). Gating on the published queued
	// count makes the enqueue order well-defined, so the gate's FIFO admission
	// guarantee is what's actually under test.
	for i := 0; i < n; i++ {
		done.Add(1)
		go func(idx int) {
			_ = g.Run(context.Background(), false, func() error {
				mu.Lock()
				order = append(order, idx)
				mu.Unlock()
				return nil
			})
			done.Done()
		}(i)
		// Wait until this acquirer's ticket is in the queue before launching the
		// next, so tickets enqueue strictly in 0..n-1 order.
		waitQueued(t, g, i+1)
	}
	// Release the held slot to drain the queue FIFO.
	g.Release()
	done.Wait()

	mu.Lock()
	defer mu.Unlock()
	if len(order) != n {
		t.Fatalf("completed %d, want %d", len(order), n)
	}
	for i, v := range order {
		if v != i {
			t.Fatalf("order = %v, want FIFO 0..%d", order, n-1)
		}
	}
}

// TestIndexGateForegroundNotStarved verifies the reserved-slot guarantee
// (#5328): a foreground acquirer is admitted promptly even while a background
// storm holds every non-reserved slot.
func TestIndexGateForegroundNotStarved(t *testing.T) {
	const cap = 2
	g := NewIndexGate(cap)

	// Background work fills the non-reserved slots and holds them.
	release := make(chan struct{})
	var bgHeld sync.WaitGroup
	// With cap=2 and a 1-slot foreground reservation, background may take cap=2
	// while no foreground waits; once a foreground waiter appears, background is
	// limited to cap-1=1. To make the test deterministic, hold cap slots first.
	for i := 0; i < cap; i++ {
		bgHeld.Add(1)
		go func() {
			_ = g.Acquire(context.Background(), false)
			bgHeld.Done()
			<-release
			g.Release()
		}()
	}
	bgHeld.Wait()

	// Now a foreground acquirer queues. Release ONE background slot; the reserved
	// slot must let the foreground acquirer in ahead of any further background.
	fgGot := make(chan struct{})
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := g.Acquire(ctx, true); err != nil {
			t.Errorf("foreground acquire failed/timed out: %v", err)
			return
		}
		close(fgGot)
		g.Release()
	}()

	// Free up one slot for the foreground acquirer.
	release <- struct{}{}

	select {
	case <-fgGot:
		// good — foreground got in promptly
	case <-time.After(2 * time.Second):
		t.Fatal("foreground acquirer was starved")
	}
	// Drain the rest.
	close(release)
}

// TestIndexGateContextCancelDequeues verifies a queued acquirer that is
// cancelled releases its ticket and holds no slot.
func TestIndexGateContextCancelDequeues(t *testing.T) {
	g := NewIndexGate(1)
	if err := g.Acquire(context.Background(), false); err != nil {
		t.Fatalf("initial acquire: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- g.Acquire(ctx, false) }()
	// Give the goroutine time to enqueue.
	time.Sleep(20 * time.Millisecond)
	if _, queued := g.Stats(); queued != 1 {
		t.Fatalf("queued = %d, want 1", queued)
	}
	cancel()
	if err := <-errCh; err == nil {
		t.Fatal("cancelled Acquire returned nil, want ctx error")
	}
	// The held slot is still ours; release it and the gate is empty.
	g.Release()
	if active, queued := g.Stats(); active != 0 || queued != 0 {
		t.Fatalf("after cancel+release: active=%d queued=%d, want 0/0", active, queued)
	}
}

// TestResolveIndexConcurrencyDefault verifies the default and env override.
func TestResolveIndexConcurrencyDefault(t *testing.T) {
	t.Setenv(IndexConcurrencyEnv, "")
	if got := resolveIndexConcurrency(); got != defaultIndexConcurrency {
		t.Fatalf("default = %d, want %d", got, defaultIndexConcurrency)
	}
	t.Setenv(IndexConcurrencyEnv, "5")
	if got := resolveIndexConcurrency(); got != 5 {
		t.Fatalf("env override = %d, want 5", got)
	}
	t.Setenv(IndexConcurrencyEnv, "0")
	if got := resolveIndexConcurrency(); got != defaultIndexConcurrency {
		t.Fatalf("non-positive env = %d, want default %d", got, defaultIndexConcurrency)
	}
	t.Setenv(IndexConcurrencyEnv, "garbage")
	if got := resolveIndexConcurrency(); got != defaultIndexConcurrency {
		t.Fatalf("garbage env = %d, want default %d", got, defaultIndexConcurrency)
	}
}
