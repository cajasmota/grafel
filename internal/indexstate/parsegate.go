// parsegate.go — the in-process tree-sitter parse CPU/concurrency cap (#5630).
//
// PROBLEM. The reactive scheduler's incremental reindex (extractors.
// TryIncremental) and the opt-out in-process full index re-parse changed files
// INSIDE the daemon process. That parse goes through neither of the existing
// throttles:
//
//   - the IndexGate (#5493) — only rebuildWorkerPool acquires it, so the
//     reactive/incremental path never registers in concurrency.indexing; and
//   - the #5602 reindex CPU ceiling — that sets GOMAXPROCS only on the
//     index-internal SUBPROCESS, so an in-process parse runs at the daemon's
//     own GOMAXPROCS (= host core count).
//
// Result: a daemon parsing in-process can monopolise the box (the multi-hour
// 5–7 core burn in #5630) while index_status reports idle.
//
// FIX. A single process-wide gate bounds how many in-process tree-sitter
// parses run CONCURRENTLY. treesitter.ParserFactory.Parse acquires a slot
// around every parse, so ALL in-process parsing — regardless of caller — is
// bounded by one daemon-wide ceiling. Excess parses block until a slot frees.
//
// The gate is a leaf-package primitive (indexstate imports nothing) so the
// low-level treesitter package can depend on it without an import cycle. It is
// off by default (cap 0 = unbounded) so non-daemon callers — plain `grafel
// index`, the extract subprocesses, tests — are unaffected; the daemon installs
// a real cap at startup via SetParseConcurrency.

package indexstate

import "sync"

var (
	parseGateMu sync.Mutex
	// parseGateCap is the max concurrent in-process parses. 0 means unbounded
	// (the default for non-daemon processes). The daemon sets a positive cap.
	parseGateCap int
	// parseGateActive is the number of parse slots currently held.
	parseGateActive int
	// parseGateWaiters is a FIFO of blocked acquirers, each woken with a slot
	// already charged to it.
	parseGateWaiters []chan struct{}
	// parseGateForeground counts live FOREGROUND holds (#5970). While it is
	// > 0 the gate is unbounded regardless of parseGateCap: the configured cap
	// expresses the BACKGROUND core budget, and user-awaited work is exempt
	// from it by policy. See BeginForegroundParse.
	parseGateForeground int
)

// SetParseConcurrency installs the daemon-wide cap on concurrent in-process
// tree-sitter parses (#5630). A cap <= 0 disables the gate (unbounded), which
// is the default for non-daemon processes. The daemon calls this once at
// startup with a resource-safe value (≈ the reindex CPU budget). Safe to call
// from any goroutine; lowering the cap does not preempt parses already running.
func SetParseConcurrency(cap int) {
	parseGateMu.Lock()
	if cap < 0 {
		cap = 0
	}
	parseGateCap = cap
	// Raising the cap may free slots for queued waiters; wake any now-eligible.
	wakeParseWaitersLocked()
	parseGateMu.Unlock()
}

// ParseConcurrencyCap returns the CONFIGURED in-process parse cap
// (0 = unbounded). It deliberately ignores any live foreground hold: callers
// like cmd/grafel's ensureParseConcurrencyDefault use it to answer "has a cap
// already been installed in this process?", and a transient foreground lift
// must not make them re-install one. Use EffectiveParseConcurrencyCap for the
// cap actually in force right now.
func ParseConcurrencyCap() int {
	parseGateMu.Lock()
	defer parseGateMu.Unlock()
	return parseGateCap
}

// EffectiveParseConcurrencyCap returns the cap acquirers are gated on RIGHT NOW
// (0 = unbounded): the configured cap, or 0 while any foreground hold is live.
func EffectiveParseConcurrencyCap() int {
	parseGateMu.Lock()
	defer parseGateMu.Unlock()
	return effectiveParseCapLocked()
}

// effectiveParseCapLocked is the one place the foreground exemption is applied.
// MUST hold parseGateMu.
func effectiveParseCapLocked() int {
	if parseGateForeground > 0 {
		return 0 // unbounded: a human is waiting on this parse
	}
	return parseGateCap
}

// BeginForegroundParse marks the start of a USER-AWAITED unit of in-process
// parsing and returns its release closure (#5970).
//
// READ THIS BEFORE ADDING A CALL SITE. The lift is PROCESS-WIDE, not scoped to
// the caller. While any hold is live the gate is unbounded for EVERY acquirer,
// including background parsing that has nothing to do with the work the hold
// was taken for — a watcher-driven incremental reindex running concurrently
// with a held rebuild parses uncapped for the whole window. Because releasing
// does not preempt parses already admitted, an overshoot outlives the release
// until those parses drain. A measured probe reached 20 concurrent parses
// against a background cap of 2.
//
// The consequence for callers: a hold is only justified where the caller
// ITSELF parses in-process, and it should span the narrowest region that does.
// Taking one "just in case", or across a long window in a config where the
// caller forks its parsing to a child, buys nothing and suspends the background
// budget for everyone. The gate has no caller identity to do better; a
// per-caller exemption (acquire that bypasses the semaphore without touching
// parseGateCap) is the correct end state and is tracked in #6022.
//
// WHY THIS EXISTS. The daemon installs its parse cap ONCE at startup, sized
// from the BACKGROUND core budget (25% of the machine) because the work that
// gate was built for — the watcher-driven incremental reindex — is background
// by definition. But the same process also runs parsing a human is sitting and
// waiting for: the synchronous `grafel index` RPC, and the rebuild path's
// indexFn when the subprocess indexer is opted out (GRAFEL_SUBPROCESS_INDEXER=0).
// The standing policy caps background work only; throttling those to a quarter
// of the box would make the user wait for no benefit. Since the gate is a
// single process-wide semaphore, the exemption cannot be a second cap — it has
// to be a scoped lift of the one that exists.
//
// Refcounted, so concurrent foreground units do not un-lift each other, and the
// returned closure is idempotent, so `defer` at the top of a function is safe on
// every exit path. Lifting wakes any parses already queued behind the
// background cap; releasing does not preempt parses already running (same
// contract as SetParseConcurrency).
//
// Scope: this changes how MUCH of the machine in-process parsing may use, never
// whether it runs. It mirrors the foreground/background split the child-spawn
// paths already make (--interactive on the index child, sched's per-group
// foreground registry) for the one path that never forks.
func BeginForegroundParse() (release func()) {
	parseGateMu.Lock()
	parseGateForeground++
	wakeParseWaitersLocked()
	parseGateMu.Unlock()

	var once sync.Once
	return func() {
		once.Do(func() {
			parseGateMu.Lock()
			if parseGateForeground > 0 {
				parseGateForeground--
			}
			parseGateMu.Unlock()
		})
	}
}

// AcquireParseSlot blocks until an in-process parse slot is free, then returns.
// The caller MUST call ReleaseParseSlot exactly once when the parse completes.
// When the gate is unbounded (cap 0) it returns immediately without queueing.
// It also brackets the in-process parse accounting (ParseBegin/ParseEnd) so a
// single call site — treesitter.ParserFactory.Parse — makes every in-process
// parse both observable (busy signal) and bounded (CPU ceiling).
func AcquireParseSlot() {
	ParseBegin()
	parseGateMu.Lock()
	if cap := effectiveParseCapLocked(); cap <= 0 || parseGateActive < cap {
		parseGateActive++
		parseGateMu.Unlock()
		return
	}
	ticket := make(chan struct{})
	parseGateWaiters = append(parseGateWaiters, ticket)
	parseGateMu.Unlock()
	<-ticket // woken with a slot already charged to us
}

// ReleaseParseSlot frees one parse slot and wakes the next waiter (FIFO), and
// records the parse as complete. MUST be paired with a successful
// AcquireParseSlot.
func ReleaseParseSlot() {
	parseGateMu.Lock()
	if parseGateActive > 0 {
		parseGateActive--
	}
	wakeParseWaitersLocked()
	parseGateMu.Unlock()
	ParseEnd()
}

// wakeParseWaitersLocked promotes queued waiters into free slots (FIFO). Each
// promoted ticket has a slot charged to it before being signalled. MUST hold
// parseGateMu. With an effective cap of 0 (unbounded — no cap configured, or a
// live foreground hold) every waiter is drained.
func wakeParseWaitersLocked() {
	for cap := effectiveParseCapLocked(); len(parseGateWaiters) > 0 && (cap <= 0 || parseGateActive < cap); cap = effectiveParseCapLocked() {
		t := parseGateWaiters[0]
		parseGateWaiters = parseGateWaiters[1:]
		parseGateActive++
		close(t)
	}
}
