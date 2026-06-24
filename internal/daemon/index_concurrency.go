// index_concurrency.go — the daemon-wide index-concurrency gate (#5493).
//
// PROBLEM. When a group/monorepo with many modules is registered or rebuilt,
// the rebuild fan-out (cmd/grafel: rebuildWorkerPool) used to index every module
// up to the memory-auto-tuned rebuild cap (8 on a 16GB host, 16 on 32GB). The
// per-index core cap (GRAFEL_EXTRACT_GOMAXPROCS, default 2) bounds cores PER
// index but NOT the number of concurrent indexes — so a 30-module group spun up
// ~8-16 indexers at once × 2 cores = 16-32 cores of pressure and pinned the
// machine (real user report: 30-module monorepo storm).
//
// FIX. A single process-wide IndexGate bounds the number of module/repo index
// operations that may run CONCURRENTLY to N (default 2, env
// GRAFEL_INDEX_CONCURRENCY). Excess index operations block in Acquire and run as
// slots free, so a 30-module group drains 2-at-a-time instead of 30-at-once.
// Acquisition is FIFO (a ticket queue) so a long backlog cannot starve early
// arrivals, and one slot is RESERVED for foreground/interactive work so a burst
// of background reindexes can never lock an interactive index out (#5328).
//
// The gate publishes its active/queued counts to the indexstate leaf package so
// `grafel status` and grafel_index_status can show "indexing 2, queued 28".
package daemon

import (
	"context"
	"os"
	"strconv"
	"sync"

	"github.com/cajasmota/grafel/internal/indexstate"
)

// IndexConcurrencyEnv is the env var that overrides the default concurrency cap.
const IndexConcurrencyEnv = "GRAFEL_INDEX_CONCURRENCY"

// defaultIndexConcurrency is the safe default for the foreground+background mix:
// 2 concurrent indexes × ~2 cores each (GRAFEL_EXTRACT_GOMAXPROCS) ≈ 4 cores of
// pressure, which leaves headroom on a typical machine. The old rebuild cap of 8
// (× 2 cores = 16) oversubscribed; 2 is the storm-safe default.
const defaultIndexConcurrency = 2

// resolveIndexConcurrency returns the effective concurrency cap, honoring
// GRAFEL_INDEX_CONCURRENCY (a positive integer). An unset, non-numeric, or
// non-positive value falls back to defaultIndexConcurrency.
func resolveIndexConcurrency() int {
	if v := os.Getenv(IndexConcurrencyEnv); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 1 {
			return n
		}
	}
	return defaultIndexConcurrency
}

// IndexGate is a fair, FIFO, daemon-wide semaphore bounding how many module/repo
// index operations run concurrently (#5493). It is the single throttle for the
// group/monorepo module fan-out: callers wrap each per-module index in
// Acquire/Release (or the Run helper) and the gate ensures at most cap run at
// once, the rest queueing.
//
// Foreground priority: foreground (interactive) acquisitions are served ahead of
// background ones at the head of the queue, and one slot is reserved so the
// background fan-out can never consume every slot and lock foreground out. With
// the default cap of 2 this means background uses at most cap-1=1 slot until a
// foreground acquirer is satisfied, then both can run.
type IndexGate struct {
	cap int

	mu     sync.Mutex
	active int             // slots currently held
	fg     []chan struct{} // FIFO of waiting foreground tickets
	bg     []chan struct{} // FIFO of waiting background tickets
}

// NewIndexGate constructs a gate with the given cap. A cap <= 0 is coerced to 1
// (fully serial) so the gate is never a no-op.
func NewIndexGate(cap int) *IndexGate {
	if cap < 1 {
		cap = 1
	}
	g := &IndexGate{cap: cap}
	g.publish()
	return g
}

// NewIndexGateFromEnv constructs a gate sized from GRAFEL_INDEX_CONCURRENCY
// (default 2). This is the production constructor.
func NewIndexGateFromEnv() *IndexGate {
	return NewIndexGate(resolveIndexConcurrency())
}

// Cap returns the configured concurrency limit.
func (g *IndexGate) Cap() int { return g.cap }

// Stats returns the current (active, queued) counts. Queued is the total of
// foreground + background waiters.
func (g *IndexGate) Stats() (active, queued int) {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.active, len(g.fg) + len(g.bg)
}

// reserveForForeground is how many slots are held back from background work so a
// foreground acquirer is never fully locked out by a background storm (#5328).
// With cap>=2 we reserve exactly one; with cap==1 we cannot reserve (serial).
func (g *IndexGate) reserveForForeground() int {
	if g.cap >= 2 {
		return 1
	}
	return 0
}

// Acquire blocks until a slot is free (respecting foreground priority and the
// reserved foreground slot), then returns. The caller MUST call Release exactly
// once when its index operation completes. It honors ctx: if ctx is cancelled
// while waiting, Acquire returns ctx.Err() and holds no slot.
//
// foreground=true requests an interactive/priority acquisition: it jumps ahead
// of queued background work and may use the reserved slot.
func (g *IndexGate) Acquire(ctx context.Context, foreground bool) error {
	g.mu.Lock()
	if g.canAdmitLocked(foreground) {
		g.active++
		g.publishLocked()
		g.mu.Unlock()
		return nil
	}
	// No slot: enqueue a ticket and wait.
	ticket := make(chan struct{})
	if foreground {
		g.fg = append(g.fg, ticket)
	} else {
		g.bg = append(g.bg, ticket)
	}
	g.publishLocked()
	g.mu.Unlock()

	select {
	case <-ticket:
		// Woken with a slot already charged to us by Release.
		return nil
	case <-ctx.Done():
		// Cancelled while waiting. Remove our ticket if it's still queued; if it
		// was already signalled (slot charged), hand the slot back via Release.
		g.mu.Lock()
		if g.removeTicketLocked(ticket, foreground) {
			// Still queued — we never got a slot.
			g.publishLocked()
			g.mu.Unlock()
			return ctx.Err()
		}
		// Already signalled between ctx.Done and acquiring the lock: a slot was
		// charged to us. Release it so it isn't leaked.
		g.mu.Unlock()
		g.Release()
		return ctx.Err()
	}
}

// canAdmitLocked reports whether an acquirer of the given priority may take a
// slot right now. Background acquirers must leave the reserved foreground slot
// free; foreground acquirers may use the full cap. MUST hold g.mu.
func (g *IndexGate) canAdmitLocked(foreground bool) bool {
	limit := g.cap
	if !foreground {
		// Background can only use slots beyond the foreground reservation IF a
		// foreground acquirer is actually waiting; otherwise background may use
		// the whole cap (no point idling a slot when no foreground wants it).
		if len(g.fg) > 0 {
			limit -= g.reserveForForeground()
		}
	}
	return g.active < limit
}

// Release frees one slot and wakes the next eligible waiter (foreground first,
// FIFO within a class). MUST be paired with a successful Acquire.
func (g *IndexGate) Release() {
	g.mu.Lock()
	if g.active > 0 {
		g.active--
	}
	g.wakeLocked()
	g.publishLocked()
	g.mu.Unlock()
}

// wakeLocked promotes queued waiters into freed slots: all eligible foreground
// tickets first (FIFO), then background tickets (FIFO) subject to the reserved
// slot. MUST hold g.mu. Each promoted ticket has a slot charged to it (active++)
// before being signalled, so the woken goroutine returns holding the slot.
func (g *IndexGate) wakeLocked() {
	for len(g.fg) > 0 && g.active < g.cap {
		t := g.fg[0]
		g.fg = g.fg[1:]
		g.active++
		close(t)
	}
	for len(g.bg) > 0 && g.canAdmitLocked(false) {
		t := g.bg[0]
		g.bg = g.bg[1:]
		g.active++
		close(t)
	}
}

// removeTicketLocked removes a still-queued ticket. Returns true if the ticket
// was found in the queue (i.e. it had NOT been signalled yet). MUST hold g.mu.
func (g *IndexGate) removeTicketLocked(ticket chan struct{}, foreground bool) bool {
	q := &g.bg
	if foreground {
		q = &g.fg
	}
	for i, t := range *q {
		if t == ticket {
			*q = append((*q)[:i], (*q)[i+1:]...)
			return true
		}
	}
	return false
}

// Run acquires a slot, invokes fn, and releases the slot — the convenience
// wrapper around Acquire/Release for the common case. If Acquire fails (ctx
// cancelled while queued) it returns that error and does NOT call fn.
func (g *IndexGate) Run(ctx context.Context, foreground bool, fn func() error) error {
	if err := g.Acquire(ctx, foreground); err != nil {
		return err
	}
	defer g.Release()
	return fn()
}

// publish mirrors the current counts to indexstate (no lock held).
func (g *IndexGate) publish() {
	g.mu.Lock()
	g.publishLocked()
	g.mu.Unlock()
}

// publishLocked mirrors the current counts to the indexstate leaf package so the
// MCP/status surfaces can read "indexing N, queued M". MUST hold g.mu.
func (g *IndexGate) publishLocked() {
	indexstate.SetIndexConcurrency(g.active, len(g.fg)+len(g.bg), g.cap)
}
