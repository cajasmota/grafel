package mcp

import (
	"sync"
	"sync/atomic"
	"testing"
)

// countingCloser is a fake readerCloser that counts munmaps so the exactly-once
// and no-leak invariants can be asserted without a real mapping.
type countingCloser struct{ n atomic.Int64 }

func (c *countingCloser) Close() error { c.n.Add(1); return nil }

// refCheckCloser additionally flags any close that happens while a borrow is
// still outstanding (refs>0) — the "never munmap while borrowed" invariant.
type refCheckCloser struct {
	h            *MapHandle
	n            atomic.Int64
	badWhileRefs atomic.Int64
}

func (c *refCheckCloser) Close() error {
	if c.h != nil && c.h.refs.Load() > 0 {
		c.badWhileRefs.Add(1)
	}
	c.n.Add(1)
	return nil
}

func newTestHandle(c readerCloser) *MapHandle { return &MapHandle{closer: c} }

// closerFunc adapts a func to readerCloser for the ordering-guard test.
type closerFunc func() error

func (f closerFunc) Close() error { return f() }

// Criterion 1/2 (regression): stale-refcount TOCTOU in release(). The adversarial
// review found that a releaser observing its own Add(-1)→0 does NOT imply refs is
// globally 0 — while the handle is still PUBLISHED a concurrent borrow can rebound
// refs 0→1 before the releaser reads retired. If the releaser then closes on that
// stale n==0, it munmaps while refs>0 (use-after-unmap).
//
// This test forces that exact interleaving DETERMINISTICALLY via the releaseGap
// seam (no -cpu sweep, default scheduling): it parks the releaser between its
// decrement and the close decision, injects a rebounding borrow + a retire, then
// resumes the releaser. It FAILS on the naive `Add(-1)==0 && retired.Load()`
// release (munmap while refs==1) and PASSES on the re-check-after-retired fix.
func TestMapHandleReleaseRefcountReboundDoesNotUnmap(t *testing.T) {
	cc := &refCheckCloser{}
	h := newTestHandle(cc)
	cc.h = h

	gapEntered := make(chan struct{})
	gapProceed := make(chan struct{})
	var armed atomic.Bool
	armed.Store(true)
	h.releaseGap = func() {
		// Fire the seam exactly once — only for the first releaser (R). Later
		// releases (the drain at the end) proceed without parking.
		if armed.CompareAndSwap(true, false) {
			close(gapEntered)
			<-gapProceed
		}
	}

	h.borrow() // refs=1: R's outstanding borrow
	done := make(chan struct{})
	go func() { defer close(done); h.release() }() // R: Add(-1)→0, then parks in the gap

	<-gapEntered // R is parked between the decrement and reading retired
	// A concurrent goroutine legally borrows the STILL-PUBLISHED (live) handle,
	// rebounding refs 0→1, then reload retires it (sees refs==1 → defers).
	h.borrow() // refs 0→1 on the still-live handle
	h.retire() // retired=true, refs==1 → defer, no close
	if got := cc.n.Load(); got != 0 {
		t.Fatalf("retire munmapped while borrowed: count=%d, want 0", got)
	}
	close(gapProceed) // resume R's close decision
	<-done

	// Buggy release() would munmap here on the stale n==0 while refs==1.
	// Fixed release() re-reads refs.Load()==1 and does NOT close.
	if bad := cc.badWhileRefs.Load(); bad != 0 {
		t.Fatalf("release munmapped while refs>0 (stale-refcount TOCTOU): %d faults", bad)
	}
	if got := cc.n.Load(); got != 0 {
		t.Fatalf("mapping unmapped while still borrowed: count=%d, want 0", got)
	}

	// Drain the rebounding borrow: the last releaser now unmaps exactly once.
	h.release()
	if got := cc.n.Load(); got != 1 {
		t.Fatalf("final munmap count = %d, want 1", got)
	}
	if bad := cc.badWhileRefs.Load(); bad != 0 {
		t.Fatalf("munmap observed refs>0 %d times, want 0", bad)
	}
}

// Criterion 1: exactly-once close is idempotent, NOT unique-observation. Both
// reload (via retire) and the last releaser (via release) can observe
// refs==0 && retired and both reach closeOnce; the sync.Once is what dedups.
//
// Each iteration seeds one outstanding borrow (refs=1, retired=false) and fires
// retire() and release() simultaneously. retire does retired.Store THEN
// refs.Load; release does refs.Add(-1) THEN retired.Load. The interleaving
// where BOTH observe refs==0 && retired (release drops to 0 and sees retired
// already stored; retire's refs.Load reads the post-decrement 0) is exactly the
// double-munmap window the sync.Once must close. A plain non-atomic
// `if !closed` flag both data-races here (caught under -race) and can count 2.
func TestMapHandleCloseIsExactlyOnceUnderContention(t *testing.T) {
	t.Parallel()
	const iters = 5000
	for i := 0; i < iters; i++ {
		cc := &countingCloser{}
		h := newTestHandle(cc)
		h.refs.Store(1) // one in-flight borrow
		start := make(chan struct{})
		var wg sync.WaitGroup
		wg.Add(2)
		go func() { defer wg.Done(); <-start; h.retire() }()  // reload side
		go func() { defer wg.Done(); <-start; h.release() }() // last-releaser side
		close(start)
		wg.Wait()
		if got := cc.n.Load(); got != 1 {
			t.Fatalf("iter %d: munmap count = %d, want exactly 1", i, got)
		}
		// The handle is retired and fully drained: refs must be 0.
		if r := h.refs.Load(); r != 0 {
			t.Fatalf("iter %d: refs = %d after drain, want 0", i, r)
		}
	}
}

// Criterion 2 (borrow-wins): a borrow taken before retire keeps the mapping
// alive; retire must DEFER the munmap, and the draining release performs it —
// exactly once, and never while refs>0.
func TestMapHandleBorrowWinsDefersUnmap(t *testing.T) {
	t.Parallel()
	cc := &refCheckCloser{}
	h := newTestHandle(cc)
	cc.h = h

	h.borrow() // refs=1
	h.retire() // retired, but refs>0 → must NOT unmap now
	if got := cc.n.Load(); got != 0 {
		t.Fatalf("retire munmapped while borrowed: count=%d, want 0", got)
	}
	h.release() // refs→0 on a retired handle → unmap now
	if got := cc.n.Load(); got != 1 {
		t.Fatalf("munmap count after drain = %d, want 1", got)
	}
	if bad := cc.badWhileRefs.Load(); bad != 0 {
		t.Fatalf("munmap observed refs>0 %d times, want 0", bad)
	}
}

// Criterion 2 (reload-wins): with no borrow outstanding, retire unmaps
// immediately (the common reload/evict/Close case), and a second retire is a
// no-op — the sync.Once makes closeOnce idempotent.
func TestMapHandleReloadWinsClosesImmediatelyAndIsIdempotent(t *testing.T) {
	t.Parallel()
	cc := &refCheckCloser{}
	h := newTestHandle(cc)
	cc.h = h

	h.retire() // refs==0 → unmap now
	if got := cc.n.Load(); got != 1 {
		t.Fatalf("retire with no borrows: count=%d, want 1", got)
	}
	h.retire() // idempotent
	if got := cc.n.Load(); got != 1 {
		t.Fatalf("second retire re-munmapped: count=%d, want 1", got)
	}
	if bad := cc.badWhileRefs.Load(); bad != 0 {
		t.Fatalf("munmap observed refs>0 %d times, want 0", bad)
	}
}

// Criterion 2 (no-leak cross-check, randomized interleave): a borrow taken just
// before vs just after retire, run under -race, must ALWAYS end unmapped
// exactly once and NEVER unmapped while refs>0. This exercises the sequential-
// consistency argument that at least one of {retire, release} observes
// refs==0 && retired.
func TestMapHandleNoLeakUnderRacingBorrowAndRetire(t *testing.T) {
	t.Parallel()
	const iters = 5000
	for i := 0; i < iters; i++ {
		cc := &refCheckCloser{}
		h := newTestHandle(cc)
		cc.h = h
		h.borrow() // the in-flight borrow that will race retire

		start := make(chan struct{})
		var wg sync.WaitGroup
		wg.Add(2)
		go func() { defer wg.Done(); <-start; h.retire() }()  // reload
		go func() { defer wg.Done(); <-start; h.release() }() // handler returns
		close(start)
		wg.Wait()

		if got := cc.n.Load(); got != 1 {
			t.Fatalf("iter %d: munmap count = %d, want exactly 1 (no leak, no double)", i, got)
		}
		if bad := cc.badWhileRefs.Load(); bad != 0 {
			t.Fatalf("iter %d: munmap observed refs>0 (%d), want 0", i, bad)
		}
	}
}
