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
func (h *MapHandle) release() {
	if h.refs.Add(-1) == 0 && h.retired.Load() {
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
