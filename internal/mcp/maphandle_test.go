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
