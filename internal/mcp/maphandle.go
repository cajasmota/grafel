// F1 of ADR-0027 (mmap + zero-copy resident graph): the deferred-unmap
// MapHandle lifetime. This is the safety keystone the whole epic rests on.
//
// Today the serve read path is lock-free by construction: State.Group() copies
// the *LoadedGroup pointer under s.mu and releases the lock, after which a
// handler reads the derived state for the whole tool call without holding any
// lock. That is safe ONLY because the materialized heap *graph.Document stays
// GC-reachable through the in-flight goroutine's pointers. Once queries alias
// bytes straight out of the mmap (F3, behind GRAFEL_SERVE_FROM_MMAP), GC
// reachability no longer covers them — a concurrent reload that munmaps while a
// query still aliases the mapping is a use-after-unmap → SIGSEGV/SIGBUS.
//
// MapHandle replaces GC-reachability with an explicit refcount-drain protocol
// (ADR-0027 §Lifetime design, Option A): reload never munmaps in place; it
// publishes the successor, retires the predecessor, and the LAST releaser of a
// retired handle performs the munmap. In F1 the read path is still DARK
// (handlers read the heap Doc); this file only makes the unmap safe to defer
// and rewires the existing munmap sites through the drain. The borrow protocol
// is present but INERT — nothing aliases the mapping for reads yet.
package mcp

import (
	"sync"
	"sync/atomic"

	"github.com/cajasmota/grafel/internal/graph/fbreader"
)

// readerCloser is the narrow close surface MapHandle drives. *fbreader.Reader
// satisfies it; tests substitute a fake that counts munmaps so the exactly-once
// and no-leak invariants can be asserted without a real mapping.
type readerCloser interface {
	Close() error
}

// MapHandle wraps one open mmap (an *fbreader.Reader) with the deferred-unmap
// lifetime from ADR-0027 §Lifetime design (Option A — refcount-drain).
//
//	refs    — number of in-flight borrows (queries aliasing this mapping).
//	retired — set once reload/evict/Close has published a successor; the
//	          mapping must be unmapped as soon as refs drains to 0.
//	closed  — the idempotent munmap guard. EXACTLY-ONCE rests ENTIRELY here,
//	          NOT on unique observation: both reload (via retire) and the last
//	          releaser (via release) CAN observe refs==0 && retired at the same
//	          time and both call closeOnce — sync.Once is what dedups the
//	          munmap. A plain non-atomic `if !closed` flag has a real
//	          double-munmap window and is a bug (proven by
//	          TestMapHandleCloseIsExactlyOnceUnderContention).
type MapHandle struct {
	// repo is the registry slug of the repo this mapping belongs to, set by
	// LoadedRepo.publishHandle so a groupBorrow snapshot can look a borrow up by
	// repo. Immutable after publish.
	repo    string
	reader  *fbreader.Reader
	closer  readerCloser
	refs    atomic.Int64
	retired atomic.Bool
	closed  sync.Once

	// readRetired is the ADR-0027 read-side SIGBUS-safety flag (memory epic
	// #5850, "Option B"), DISTINCT from the F1 refcount-drain `retired` atomic
	// above. It is set true — ALWAYS under the owning LoadedRepo.readerMu — in
	// publishHandle/retireHandle immediately BEFORE this handle's mapping is
	// munmapped. The GRAFEL_SERVE_FROM_MMAP flag-on read choke points check it
	// under that SAME readerMu: a stale captured *LabelIndex whose mapping was
	// retired out from under it reads readRetired==true and falls back to the
	// GC-safe Doc path instead of dereferencing the freed region. Plain bool
	// (not atomic): every read and write is serialized by readerMu.
	readRetired bool

	// releaseGap, when non-nil, is invoked inside release() in the window
	// BETWEEN the refcount decrement and the close decision. It is a test-only
	// scheduling seam used to force the stale-refcount rebound interleaving
	// deterministically (see TestMapHandleReleaseRefcountReboundDoesNotUnmap);
	// nil in production, so release() pays only a nil check. Set once at
	// construction, before the handle is published/borrowed — never mutated
	// concurrently, so it needs no atomic.
	releaseGap func()
}

// newMapHandle wraps a freshly-opened reader for the production path. Called
// only with a non-nil reader (reloadLocked guards on fbreader.Open success).
func newMapHandle(r *fbreader.Reader) *MapHandle {
	return &MapHandle{reader: r, closer: r}
}

// Reader returns the wrapped reader (the future F3 read cursor). Nil-safe so a
// caller can chain h.Reader() on a repo with no mmap.
func (h *MapHandle) Reader() *fbreader.Reader {
	if h == nil {
		return nil
	}
	return h.reader
}

// borrow increments the refcount and returns the handle for reads.
//
// borrow runs UNDER s.mu (from the group borrow), so it cannot race reload's
// publish+retire. It deliberately needs NO "refuse retired" check and there is
// NO negative sentinel: reload repoints the published handle to the successor
// BEFORE it retires the predecessor (see LoadedRepo.publishHandle), and a fresh
// borrow only ever targets the currently-published handle (the
// read-through-captured-handle invariant). So a retired handle can never gain a
// new borrow — the structural ordering is the guarantee, not a runtime guard.
func (h *MapHandle) borrow() *MapHandle {
	h.refs.Add(1)
	return h
}

// release drops one borrow. If it drains the last borrow of a retired handle,
// it performs the munmap. Runs LOCK-FREE after the handler returns. The close
// here may race reload's close in retire() — closeOnce dedups them.
//
// Stale-refcount TOCTOU (the bug the adversarial review caught): a naive
//
//	if h.refs.Add(-1) == 0 && h.retired.Load() { h.closeOnce() }
//
// acts on a CACHED n==0. While this handle is still PUBLISHED (not yet retired),
// a concurrent goroutine may legally borrow it, rebounding refs 0→1 AFTER our
// decrement but BEFORE we read retired. If reload then retires (sees refs==1 →
// defers) and we re-read retired==true, the naive form munmaps on the stale
// n==0 while refs==1 → use-after-unmap. The "no re-borrow of a retired handle"
// invariant does NOT cover this: the rebounding borrow hit the handle while it
// was still LIVE, which is legal.
//
// Fix: re-check refs AFTER observing retired. Once retired is visible the handle
// is already unpublished (retire runs strictly after publishHandle repoints
// lr.handle to the successor), so no new borrow can target it and refs is
// monotonically non-increasing from there — a Load()==0 after retired==true is
// authoritative.
func (h *MapHandle) release() {
	if h.refs.Add(-1) != 0 {
		return
	}
	if h.releaseGap != nil {
		h.releaseGap()
	}
	if h.retired.Load() && h.refs.Load() == 0 {
		h.closeOnce()
	}
}

// retire marks this handle superseded and, if no borrow is outstanding,
// unmaps it now; otherwise the last release() unmaps it. This is the
// reload/evict/Close side of the drain. Runs UNDER s.mu.
//
// No-leak cross-check (ADR-0027 §Correctness): retire does retired.Store THEN
// refs.Load; release does refs.Add(-1) THEN retired.Load. Under Go's
// sequentially-consistent atomics whichever performs its second op last
// observes the other's first, so at least one side sees refs==0 && retired and
// calls closeOnce — there is no interleaving where neither closes.
func (h *MapHandle) retire() {
	h.retired.Store(true)
	if h.refs.Load() == 0 {
		h.closeOnce()
	}
}

// closeOnce performs the munmap exactly once, however many callers reach it.
// The sync.Once is load-bearing — see the MapHandle.closed doc comment.
func (h *MapHandle) closeOnce() {
	h.closed.Do(func() {
		if h.closer != nil {
			_ = h.closer.Close()
		}
	})
}
