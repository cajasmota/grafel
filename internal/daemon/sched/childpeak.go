package sched

import (
	"os"
	"sync"
	"time"
)

// ---------------------------------------------------------------------------
// Child peak-RSS handoff (#6107, epic #5954)
//
// Since #5954 the default index path forks `grafel index-internal`, so the
// memory an index costs is spent in a process the daemon never measures. The
// per-job sampler this replaces read getpid() and so reported a flat (usually
// zero) delta no matter how large the index was — the dominant cause of
// peak_heap_mb=0 on ~99% of production runs. It has been removed rather than
// re-timed; see "why there is no in-daemon sampler" below.
//
// RunSubprocessIndex reaps the child and knows its kernel high-water RSS.
// runIndex, two layers up, is what writes the completion line. The two are in
// this package but connected only through the caller-supplied Config.Index
// callback (which lives in package main and returns nothing but an error), so
// the peak is handed over through this small keyed registry rather than by
// widening a public callback signature that four call sites implement.
//
// Every record carries the wall time it was taken. runIndex only accepts a
// record stamped at or after the job's own start, so a value left behind by a
// foreground group-rebuild (which forks its own child through the same
// function) can never be misattributed to a later scheduler run whose index
// took the incremental in-process path. Reporting a stale peak would be worse
// than reporting none.
// ---------------------------------------------------------------------------

// peakSrcChildMaxRSS names the measure used when the index ran in a child:
// the child's ru_maxrss, i.e. its ABSOLUTE peak resident set over its whole
// lifetime, kernel-tracked.
const peakSrcChildMaxRSS = "child_maxrss"

// peakSrcUnmeasured means this run produced NO peak figure, and the completion
// line says so rather than emitting a number nobody can defend. Two ways here:
//
//   - The index ran inside the daemon (the incremental path, or
//     GRAFEL_SUBPROCESS_INDEXER=0). See "why there is no in-daemon sampler".
//   - The index ran in a child on a platform that exposes no rusage (Windows),
//     so no high-water mark came back with the reaped process.
const peakSrcUnmeasured = "unmeasured"

// ---------------------------------------------------------------------------
// Why there is no in-daemon sampler (#6107 review).
//
// An earlier shape of this fix kept a sampler reporting the daemon's own RSS
// growth across a job — currentProcessRSSMB() minus the RSS at job start —
// under the name daemon_rss_delta. That quantity is not the job's memory, and
// no amount of sampling earlier, more often, or while the allocation is still
// reachable repairs it. Measured on darwin/arm64: four identical 320 MiB jobs
// run back to back in one process, sampled every 100 ms DURING each job while
// the allocation was still reachable, reported 322, 259, 0, 0 MiB.
//
// None of that decay is a race with the garbage collector — an explicit
// runtime.GC() after a job did not move RSS at all. It is arena reuse: once
// the process holds enough free spans the allocation faults in no new page, so
// the daemon's RSS does not grow and the delta is truthfully zero. A
// long-lived daemon is precisely the process that always holds free spans, so
// in steady state the metric was guaranteed to read 0 — the symptom #6107
// opened on. A live-heap delta fails identically, because the baseline taken
// at job start already counts the previous job's uncollected garbage.
//
// The deeper problem: the daemon runs several jobs, an MCP server and mmap'd
// graphs in one address space, so no figure taken from outside the allocator
// can be attributed to one job. That is why epic #5954 moved indexing into a
// child — a child has its own address space and the kernel keeps its
// high-water mark for free. So the in-process arm reports peakSrcUnmeasured,
// and dropping the sampler also stops the daemon forking `ps` every 2s per
// in-flight job on Darwin.
// ---------------------------------------------------------------------------

type childPeakRecord struct {
	mb int64
	at time.Time
}

var childPeaks = struct {
	mu sync.Mutex
	m  map[string]childPeakRecord
}{m: make(map[string]childPeakRecord)}

// recordChildPeakRSSMB stores the high-water RSS of the index child that just
// exited for repoPath. Non-positive values are dropped: "no measurement" must
// stay distinguishable from "measured zero".
//
// The mb <= 0 half of that guard is currently UNREACHABLE — maxRSSBytes already
// rejects Maxrss <= 0, so no caller can get here with a non-positive figure, and
// a mutation removing it kills nothing. It stays because it is what enforces the
// honesty invariant at this layer rather than at a caller's: a recorded 0 would
// surface as "peak_rss_mb=0 peak_rss_src=child_maxrss", which is precisely the
// false number — a confident zero — that #6107 exists to eliminate. Deleting it
// would make that outcome one refactor away instead of impossible.
func recordChildPeakRSSMB(repoPath string, mb int64) {
	if repoPath == "" || mb <= 0 {
		return
	}
	childPeaks.mu.Lock()
	defer childPeaks.mu.Unlock()
	childPeaks.m[repoPath] = childPeakRecord{mb: mb, at: time.Now()}
}

// takeChildPeakRSSMB removes and returns the child peak recorded for repoPath
// at or after notBefore. A record older than notBefore belongs to an earlier
// run and is discarded rather than returned.
func takeChildPeakRSSMB(repoPath string, notBefore time.Time) (int64, bool) {
	childPeaks.mu.Lock()
	defer childPeaks.mu.Unlock()
	rec, ok := childPeaks.m[repoPath]
	if !ok {
		return 0, false
	}
	delete(childPeaks.m, repoPath)
	if rec.at.Before(notBefore) {
		return 0, false
	}
	return rec.mb, true
}

// recordChildPeakFromProcessState is the single call site that turns a reaped
// child into a recorded peak. Silent no-op when the platform supplies no
// rusage (Windows) — the run is then reported as peakSrcUnmeasured.
func recordChildPeakFromProcessState(repoPath string, ps *os.ProcessState) {
	b, ok := maxRSSBytes(ps)
	if !ok {
		return
	}
	recordChildPeakRSSMB(repoPath, int64(b/(1<<20)))
}
