package process

import "runtime"

// indexCoreBudgetDivisor expresses the project's background-indexing core rule:
// background (non-user-awaited) indexing may draw at most 1/4 of the machine's
// logical cores. See IndexCoreBudget for the rationale.
const indexCoreBudgetDivisor = 4

// IndexCoreBudget is the CANONICAL core budget for BACKGROUND indexing work
// (#5960). It is the single source of truth for "how much of this machine may
// grafel's background indexing use"; any code that needs that number — the
// extract subprocess fanout, the in-process tree-sitter parse gate, future
// analytics passes — must call this helper rather than recomputing 25% inline.
//
// Policy (decided by the user, #5960):
//
//	budget = max(1, runtime.NumCPU()/4)   // 25% of machine capacity
//
//	 1-core →  1      4-core →  1     12-core →  3
//	 8-core →  2     32-core →  8     64-core → 16
//
// Why a *proportion* and not a static cap: the previous rule was a flat "never
// more than 3 cores", which is right on a 12-core laptop but punishes a 32-core
// workstation with the same wall-clock as a laptop. Scaling with the host means
// bigger machines get proportionally better index times while every machine
// keeps 75% of its capacity for the human sitting in front of it.
//
// This budget applies ONLY to background work. Interactive / foreground
// rebuilds (a user typed `grafel rebuild` and is waiting) are deliberately
// UNCAPPED and must not consult this function.
//
// It is also a *default*, not a ceiling on operators: an explicit override
// (GRAFEL_EXTRACT_CONCURRENCY, GRAFEL_EXTRACT_GOMAXPROCS, cpu.json, an explicit
// config field) is an escape hatch and is honored as-is, never silently clamped
// to this budget.
//
// Note (#5960): GOMAXPROCS is NOT a valid way to enforce this budget on its
// own. A goroutine inside a cgo call (tree-sitter parsing is cgo) parks in
// _Gsyscall and the Go runtime hands its P to another goroutine, so N
// concurrent cgo calls occupy N OS threads regardless of GOMAXPROCS. The budget
// must therefore be enforced on things the process actually controls: the
// number of concurrent child processes, and an explicit parse semaphore
// (indexstate.SetParseConcurrency / AcquireParseSlot) inside each of them.
//
// That is the live situation, not a forecast: #5954/#5972 removed the
// process-wide treesitter.parseMu that used to serialise factory-routed parses,
// so N goroutines really can sit inside ts_parser_parse at once. The counting
// semaphore held ACROSS the cgo call is the only thing that bounds it.
func IndexCoreBudget() int {
	return IndexCoreBudgetFor(runtime.NumCPU())
}

// IndexCoreBudgetFor is the pure form of IndexCoreBudget, taking the host core
// count explicitly so the policy stays unit-testable across core counts.
// Returns at least 1 for any input, including 0 and negative values (some
// platforms/containers report a bogus core count).
func IndexCoreBudgetFor(numCPU int) int {
	n := numCPU / indexCoreBudgetDivisor
	if n < 1 {
		n = 1
	}
	return n
}
