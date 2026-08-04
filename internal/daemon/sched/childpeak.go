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
// daemon's own per-job sampler reads getpid() and therefore reports a flat
// (usually zero) delta no matter how large the index was — that is the
// dominant cause of peak_heap_mb=0 on ~99% of production runs.
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

// peakSrcDaemonDelta names the measure used when the index ran inside the
// daemon: the DAEMON's RSS growth over the run (peak observed RSS minus the
// RSS at job start), sampled. A delta, not an absolute — the daemon is
// resident for much else besides this index.
const peakSrcDaemonDelta = "daemon_rss_delta"

// peakSrcNone means no usable measurement was obtained for this run.
const peakSrcNone = "none"

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
// rusage (Windows) — the daemon sampler then remains the only source.
func recordChildPeakFromProcessState(repoPath string, ps *os.ProcessState) {
	b, ok := maxRSSBytes(ps)
	if !ok {
		return
	}
	recordChildPeakRSSMB(repoPath, int64(b/(1<<20)))
}
