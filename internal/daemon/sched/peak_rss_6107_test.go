package sched

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// #6107 / #5954 — the "indexer: completed" peak field must carry a real,
// nonzero measurement on the paths production actually takes.
//
// Two production paths, two measurements:
//
//   - subprocess indexer (the DEFAULT, GRAFEL_SUBPROCESS_INDEXER unset): the
//     work happens in a `grafel index-internal` child, so the only honest
//     figure is the kernel's high-water RSS for THAT process (ru_maxrss).
//   - in-process indexer (GRAFEL_SUBPROCESS_INDEXER=0, and the incremental
//     path): the work happens in the daemon, which cannot attribute its own
//     memory to one of the jobs inside it. That arm reports no figure at all.
//
// These tests are behavioural: they measure a KNOWN allocation and assert the
// reported number stands in a plausible relationship to it. An assertion of
// "> 0" alone would survive a hard-coded constant, so every band below is
// two-sided and anchored on the allocation size.
// ---------------------------------------------------------------------------

// syncBuf is a mutex-guarded bytes.Buffer: slog writes from the worker path
// while the test body reads.
type syncBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuf) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuf) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

var (
	peakRSSRe    = regexp.MustCompile(`msg="indexer: completed".*\bpeak_rss_mb=(-?\d+)`)
	peakSrcRe    = regexp.MustCompile(`msg="indexer: completed".*\bpeak_rss_src=([a-z_]+)`)
	completedRe  = regexp.MustCompile(`msg="indexer: completed"`)
	testTouchMBs = 320
)

// completionPeak extracts (peak_rss_mb, peak_rss_src) from the single
// "indexer: completed" line in the captured log.
func completionPeak(t *testing.T, log string) (int64, string) {
	t.Helper()
	if !completedRe.MatchString(log) {
		t.Fatalf("no \"indexer: completed\" line in log:\n%s", log)
	}
	m := peakRSSRe.FindStringSubmatch(log)
	if m == nil {
		t.Fatalf("completion line has no peak_rss_mb field:\n%s", log)
	}
	v, err := strconv.ParseInt(m[1], 10, 64)
	if err != nil {
		t.Fatalf("peak_rss_mb=%q not an integer: %v", m[1], err)
	}
	src := ""
	if sm := peakSrcRe.FindStringSubmatch(log); sm != nil {
		src = sm[1]
	}
	return v, src
}

// newPeakTestScheduler wires a scheduler whose completion line lands in buf.
func newPeakTestScheduler(buf *syncBuf, index func(context.Context, string, string) error) *Scheduler {
	return newPeakTestSchedulerWithHistory(buf, nil, index)
}

// newPeakTestSchedulerWithHistory is newPeakTestScheduler with a live
// RSSHistory attached, so a test can observe what a completed run persists.
func newPeakTestSchedulerWithHistory(buf *syncBuf, h *RSSHistory, index func(context.Context, string, string) error) *Scheduler {
	return New(Config{
		Workers: 1,
		Logger:  slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelInfo})),
		Index:   index,
		History: h,
	})
}

// touchMB allocates n MiB and writes one byte per 4 KiB page so the pages are
// genuinely resident, not just reserved. The caller MUST keep the result alive
// (runtime.KeepAlive) for the window it wants measured: without that the
// compiler is free to let the GC reclaim it mid-measurement.
func touchMB(n int) [][]byte {
	blocks := make([][]byte, 0, n)
	for i := 0; i < n; i++ {
		b := make([]byte, 1<<20)
		for j := 0; j < len(b); j += 4096 {
			b[j] = byte(i + 1)
		}
		blocks = append(blocks, b)
	}
	return blocks
}

// TestInProcessRunIsReportedAsUnmeasured pins what the in-process index path
// may claim, and it is deliberately NOT "a plausible peak".
//
// The daemon-side sampler this replaces computed currentProcessRSSMB() minus
// the daemon's RSS at job start. That quantity is not the job's memory.
// Measured on darwin/arm64, four identical 320 MiB jobs run back to back in
// one process, sampled every 100 ms *while the allocation was still
// reachable*, reported:
//
//	job=1 rss_delta_mb=322   job=2 rss_delta_mb=259
//	job=3 rss_delta_mb=0     job=4 rss_delta_mb=0
//
// Nothing about that decay is a race with the garbage collector — an explicit
// runtime.GC() after a job did not move RSS at all, and sampling earlier or
// more often does not help. It is arena reuse: once the process holds enough
// free spans the job's allocation never faults in a new page, so the daemon's
// RSS does not grow and the delta is truthfully zero. A long-lived daemon is
// precisely the process that always holds free spans, so the metric was
// guaranteed to read 0 in steady state — exactly the symptom #6107 opened on.
// The same collapse defeats a live-heap delta, because the baseline taken at
// job start already counts the previous job's uncollected garbage.
//
// There is no sound repair. Attributing a share of one process's memory to one
// of several concurrent jobs inside it is not measurable from outside the
// allocator, which is why epic #5954 moved indexing into a child in the first
// place: a child has its own address space and the kernel keeps its high-water
// mark for free. So the in-process arm reports that it has no measurement, and
// the two properties below are what a caller may rely on.
func TestInProcessRunIsReportedAsUnmeasured(t *testing.T) {
	var peaks []int64
	var srcs []string
	// Repeat in ONE process: the first run leaves free spans behind, so a
	// delta-based sampler diverges from run to run. A measurement that changes
	// when nothing about the job changed is not a measurement.
	for i := 0; i < 3; i++ {
		var buf syncBuf
		s := newPeakTestScheduler(&buf, func(context.Context, string, string) error {
			blocks := touchMB(testTouchMBs)
			time.Sleep(50 * time.Millisecond)
			runtime.KeepAlive(blocks)
			return nil
		})
		s.runIndex(jobToken{repoPath: "/repo-shortjob-6107", ref: "main", commit: "c0"})
		peak, src := completionPeak(t, buf.String())
		peaks = append(peaks, peak)
		srcs = append(srcs, src)
	}

	for i := range peaks {
		if peaks[i] != peaks[0] || srcs[i] != srcs[0] {
			t.Fatalf("identical in-process jobs reported different figures across runs: "+
				"peaks=%v srcs=%v — a per-job peak derived from whole-process RSS is not "+
				"reproducible (arena reuse), so it must not be reported as one", peaks, srcs)
		}
	}
	if srcs[0] != peakSrcUnmeasured {
		t.Fatalf("peak_rss_src=%q for an in-process index; want %q — the daemon cannot "+
			"attribute its own RSS to one of the jobs running inside it", srcs[0], peakSrcUnmeasured)
	}
	if peaks[0] != 0 {
		t.Fatalf("peak_rss_mb=%d alongside src=%q; an unmeasured run must report no "+
			"figure at all rather than a number readers will treat as one", peaks[0], srcs[0])
	}
}

// TestUnmeasuredRunOmitsPeakVsPredicted guards the derived field.
// peak_vs_predicted_mb is observedPeak - predictedMB, so with no observed peak
// it degrades to -predictedMB and reads as a large negative "measurement" —
// this is the origin of the widely-quoted "peak_heap_mb=0 alloc_diff_mb=-1593"
// line, which measured nothing. When the peak is unmeasured the derived field
// must be absent, not zero-anchored.
func TestUnmeasuredRunOmitsPeakVsPredicted(t *testing.T) {
	var buf syncBuf
	s := newPeakTestScheduler(&buf, func(context.Context, string, string) error { return nil })
	s.runIndex(jobToken{repoPath: "/repo-nopeak-6107", ref: "main", commit: "c0", predictedMB: 1593})

	log := buf.String()
	if !completedRe.MatchString(log) {
		t.Fatalf("no completion line:\n%s", log)
	}
	if strings.Contains(log, "peak_vs_predicted_mb") {
		t.Fatalf("completion line carries peak_vs_predicted_mb with no measured peak — "+
			"it is just -predictedMB wearing a measurement's name:\n%s", log)
	}
}

// TestRunIndexPrefersChildMaxRSS asserts that when the index ran in a child
// process (the DEFAULT production path) the completion line reports the
// child's kernel high-water RSS, not the daemon's own near-flat RSS delta.
// This is the dominant half of #6107: the daemon samples getpid(), but since
// #5954 moved indexing into `grafel index-internal` the memory is spent in a
// process the daemon never measures.
func TestRunIndexPrefersChildMaxRSS(t *testing.T) {
	const repo = "/repo-childpeak-6107"
	const childMB int64 = 1477

	var buf syncBuf
	s := newPeakTestScheduler(&buf, func(context.Context, string, string) error {
		// Stands in for RunSubprocessIndex reaping its child and recording
		// ru_maxrss. Recorded DURING the run, as the real path does.
		recordChildPeakRSSMB(repo, childMB)
		return nil
	})

	s.runIndex(jobToken{repoPath: repo, ref: "main", commit: "c0"})

	peak, src := completionPeak(t, buf.String())
	if peak != childMB {
		t.Fatalf("peak_rss_mb=%d; want the child's %d MiB high-water RSS", peak, childMB)
	}
	if src != peakSrcChildMaxRSS {
		t.Fatalf("peak_rss_src=%q; want %q", src, peakSrcChildMaxRSS)
	}
	// The completed run must also propagate to the history/predictor input,
	// which was starved of samples for exactly the same reason.
	found := false
	for _, r := range s.Snapshot().IndexedRepos {
		if r.Path != repo {
			continue
		}
		found = true
		if r.LastPeakMB != childMB {
			t.Fatalf("repoStats.LastPeakMB=%d; want the child's %d MiB", r.LastPeakMB, childMB)
		}
	}
	if !found {
		t.Fatalf("repo %s absent from Snapshot().IndexedRepos", repo)
	}
	// The audit ring must not re-introduce the ambiguity the structured line
	// was restructured to remove. A bare "peak=N" cannot distinguish an
	// absolute child high-water mark from no measurement at all, and the two
	// streams disagreeing about that is worse than either on its own.
	var okEvent string
	for _, e := range s.Snapshot().RecentLog {
		if e.Kind == "index_ok" && e.Repo == repo {
			okEvent = e.Msg
		}
	}
	if okEvent == "" {
		t.Fatalf("no index_ok event for %s", repo)
	}
	if !strings.Contains(okEvent, "src="+peakSrcChildMaxRSS) {
		t.Fatalf("index_ok event %q carries a bare peak with no src", okEvent)
	}
}

// TestRunIndexIgnoresStaleChildPeak guards the lifecycle failure mode the
// issue points at from the other side: a peak recorded BEFORE this job started
// (e.g. by a foreground group-rebuild's own child) must not be attributed to
// this run. Reporting a stale figure is worse than reporting none.
func TestRunIndexIgnoresStaleChildPeak(t *testing.T) {
	const repo = "/repo-stalepeak-6107"

	recordChildPeakRSSMB(repo, 9999)
	// Ensure the stale record is strictly older than the job start.
	time.Sleep(5 * time.Millisecond)

	var buf syncBuf
	s := newPeakTestScheduler(&buf, func(context.Context, string, string) error {
		return nil
	})

	s.runIndex(jobToken{repoPath: repo, ref: "main", commit: "c0"})

	peak, _ := completionPeak(t, buf.String())
	if peak == 9999 {
		t.Fatalf("completion line reported a child peak recorded before the job started (%d)", peak)
	}
}

// TestChildMaxRSSMatchesKnownAllocation exercises the kernel-rusage reader
// against a REAL child process that touches a known amount of memory. This is
// the measurement the subprocess path depends on; a mistake in the platform
// unit (Linux reports ru_maxrss in KiB, Darwin in bytes) would be a 1024x
// error that a one-sided "> 0" assertion would never see.
func TestChildMaxRSSMatchesKnownAllocation(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("ru_maxrss is POSIX; the Windows build reports no child peak")
	}
	const childMB = 256

	cmd := exec.Command(os.Args[0], "-test.run=^TestChildAllocHelper6107$", "-test.v")
	cmd.Env = append(os.Environ(), "GRAFEL_TEST_CHILD_ALLOC_MB="+strconv.Itoa(childMB))
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("child helper failed: %v\n%s", err, out)
	}

	got, ok := maxRSSBytes(cmd.ProcessState)
	if !ok {
		t.Fatalf("maxRSSBytes reported nothing for a reaped child on %s", runtime.GOOS)
	}
	gotMB := int64(got / (1 << 20))
	// The band is tight ON PURPOSE. A 0.5x-6x band admits both a 2x
	// over-report and a 2x under-report, which is exactly the size of error
	// that matters here: Darwin RSS counts MADV_FREE pages, so ru_maxrss can
	// exceed the live requirement, and whether this figure is safe to feed
	// admission turns on that ratio. A band that cannot resolve 2x cannot
	// answer the question it exists to answer.
	//
	// The only hazard the band must tolerate is the child's own runtime and
	// test binary (~10-30 MiB on top of 256), and this child HOLDS its
	// allocation rather than churning it — measured 261 MiB for 256 MiB
	// touched, i.e. 1.02x. 0.8x..1.5x leaves ~4x the observed headroom while
	// still rejecting the unit errors: a KiB-as-bytes read lands at 0.25x and
	// a bytes-as-KiB read at 1024x.
	lo, hi := int64(childMB)*8/10, int64(childMB)*15/10
	if gotMB < lo || gotMB > hi {
		t.Fatalf("child ru_maxrss=%d MiB for a child that held %d MiB (want %d..%d) — check the platform unit",
			gotMB, childMB, lo, hi)
	}
}

// TestChildAllocHelper6107 is not a test: it is the child body re-exec'd by
// TestChildMaxRSSMatchesKnownAllocation. It is inert unless the env var is set.
func TestChildAllocHelper6107(t *testing.T) {
	v := os.Getenv("GRAFEL_TEST_CHILD_ALLOC_MB")
	if v == "" {
		t.Skip("helper process body; runs only when GRAFEL_TEST_CHILD_ALLOC_MB is set")
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		t.Fatalf("bad GRAFEL_TEST_CHILD_ALLOC_MB=%q: %v", v, err)
	}
	blocks := touchMB(n)
	runtime.KeepAlive(blocks)
}
