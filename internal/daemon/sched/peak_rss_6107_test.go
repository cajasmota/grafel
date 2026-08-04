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
//   - in-process indexer (GRAFEL_SUBPROCESS_INDEXER=0): the work happens in
//     the daemon, so the figure is the daemon's own RSS growth across the run.
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
	return New(Config{
		Workers: 1,
		Logger:  slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelInfo})),
		Index:   index,
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

// TestRunIndexReportsInProcessPeakForShortJob is the direct reproduction of
// #6107 on the in-process path. The job allocates a known 320 MiB and finishes
// in well under a second — the shape of ~99% of production reindexes. Before
// the fix the sampler only observed on a 5s ticker, so a sub-5s job produced
// ZERO samples and the completion line reported 0.
func TestRunIndexReportsInProcessPeakForShortJob(t *testing.T) {
	var buf syncBuf
	s := newPeakTestScheduler(&buf, func(context.Context, string, string) error {
		blocks := touchMB(testTouchMBs)
		time.Sleep(300 * time.Millisecond)
		runtime.KeepAlive(blocks)
		return nil
	})

	s.runIndex(jobToken{repoPath: "/repo-shortjob-6107", ref: "main", commit: "c0"})

	peak, src := completionPeak(t, buf.String())
	// Two-sided band anchored on the 320 MiB the job actually touched. The
	// lower bound is deliberately loose (the allocator may reuse already
	// resident spans) but far above any plausible constant; the upper bound
	// catches a figure that is not a per-run delta at all (e.g. absolute
	// process RSS, or bytes reported as MB).
	if peak < int64(testTouchMBs)/4 || peak > int64(testTouchMBs)*8 {
		t.Fatalf("peak_rss_mb=%d is not plausible for a job that touched %d MiB (want %d..%d)",
			peak, testTouchMBs, testTouchMBs/4, testTouchMBs*8)
	}
	if src != peakSrcDaemonDelta {
		t.Fatalf("peak_rss_src=%q; the in-process path must name itself %q", src, peakSrcDaemonDelta)
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
	// The child is a Go test binary (~10-30 MiB of its own) that touched an
	// additional 256 MiB. Two-sided: the lower bound rejects a value scaled
	// down by 1024 (KiB read as bytes), the upper rejects one scaled up.
	if gotMB < childMB/2 || gotMB > childMB*6 {
		t.Fatalf("child ru_maxrss=%d MiB for a child that touched %d MiB (want %d..%d) — check the platform unit",
			gotMB, childMB, childMB/2, childMB*6)
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
