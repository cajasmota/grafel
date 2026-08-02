package main

// Peak live-heap measurement for #5964 worktree graph seeding.
//
// Opt-in only: set GRAFEL_SEED_HEAP_MEASURE=1. It builds a several-hundred-file
// fixture and indexes it three times, which is far too expensive for the
// ordinary suite. It exists so the O(delta) claim is reproducible rather than
// asserted from memory.
//
// The metric is LIVE HEAP — next_gc / (1 + GOGC/100) — sampled on a ticker and
// reduced to its peak. It is NOT RSS: on macOS, RSS counts MADV_FREE pages the
// allocator has already returned logically but the kernel has not reclaimed, so
// RSS is an upper bound on footprint, not a measurement of it (a grafel engine
// measured at 2396 MB RSS fell to 1282 MB under memory pressure without doing
// any work). Report machine state alongside any number from this test:
//   vm_stat ; sysctl vm.swapusage
//
// MEASURED FINDING, recorded here so it is not re-inferred the other way.
// Seeding is O(delta) in WORK and WALL CLOCK, not in peak memory:
//
//   2500-file fixture, 3 changed files, 16 GB macOS laptop (swap 1877 MB used,
//   unchanged across the run):
//     A  full index of the worktree : peak live heap 105.3 MB, wall 7.252 s
//     B  seed + incremental delta   : peak live heap 138.8 MB, wall 1.245 s
//     ratio B/A                     : heap 1.32x, wall 0.17x
//   800-file fixture: heap 1.15x, wall 0.83x.
//
// The seeded pass re-extracts 3 of 2503 files, but it must MATERIALISE THE
// WHOLE PARENT GRAPH to merge the unchanged-file portion forward
// (graph.LoadGraphFromDir + mergeIncrementalPrevDoc + the carry-forward entity
// slice), and holds it alongside the document being built. So peak heap tracks
// GRAPH SIZE, not delta size, and comes out slightly ABOVE a full index rather
// than below it. Anyone sizing concurrent per-worktree indexing off this should
// budget for the graph, not for the delta.

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/daemon"
)

// heapSampler tracks the peak live heap across a window.
type heapSampler struct {
	peak  atomic.Uint64
	stop  chan struct{}
	done  chan struct{}
	gogc  float64
	every time.Duration
}

func startHeapSampler() *heapSampler {
	gogc := 100.0
	if v := os.Getenv("GOGC"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil && f > 0 {
			gogc = f
		}
	}
	h := &heapSampler{
		stop:  make(chan struct{}),
		done:  make(chan struct{}),
		gogc:  gogc,
		every: 20 * time.Millisecond,
	}
	go func() {
		defer close(h.done)
		t := time.NewTicker(h.every)
		defer t.Stop()
		var ms runtime.MemStats
		for {
			select {
			case <-h.stop:
				return
			case <-t.C:
				runtime.ReadMemStats(&ms)
				// Live heap implied by the GC's next-collection target.
				live := uint64(float64(ms.NextGC) / (1.0 + h.gogc/100.0))
				for {
					cur := h.peak.Load()
					if live <= cur || h.peak.CompareAndSwap(cur, live) {
						break
					}
				}
			}
		}
	}()
	return h
}

func (h *heapSampler) peakMB() float64 {
	close(h.stop)
	<-h.done
	return float64(h.peak.Load()) / (1 << 20)
}

func writeBigFixture(t *testing.T, dir, branch string, n int) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, "svc"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module bigfixture\n\ngo 1.21\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < n; i++ {
		src := fmt.Sprintf(`package svc

type Svc%[1]d struct{ Name string }

func (s *Svc%[1]d) Handle%[1]d(in string) string { return s.help%[1]d(in) }
func (s *Svc%[1]d) help%[1]d(in string) string   { return in + s.Name }

func Dispatch%[1]d(in string) string {
	p := &Svc%[2]d{Name: "p"}
	return p.Handle%[2]d(in)
}

func Fan%[1]dA(in string) string { return Dispatch%[1]d(in) }
func Fan%[1]dB(in string) string { return Fan%[1]dA(in) }
`, i, (i+n-1)%n)
		if err := os.WriteFile(filepath.Join(dir, "svc", fmt.Sprintf("s%04d.go", i)), []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	seedGitRun(t, dir, "init", "-q", "-b", branch)
	seedGitRun(t, dir, "add", "-A")
	seedGitRun(t, dir, "commit", "-q", "-m", "big fixture")
}

func TestWorktreeSeedPeakHeapAndWallClock(t *testing.T) {
	if os.Getenv("GRAFEL_SEED_HEAP_MEASURE") != "1" {
		t.Skip("set GRAFEL_SEED_HEAP_MEASURE=1 to run the peak live-heap measurement")
	}
	n := 800
	if v := os.Getenv("GRAFEL_SEED_HEAP_FILES"); v != "" {
		if k, err := strconv.Atoi(v); err == nil && k > 0 {
			n = k
		}
	}
	root := t.TempDir()
	t.Setenv("GRAFEL_DAEMON_ROOT", filepath.Join(root, "daemonroot"))

	parentPath := filepath.Join(root, "parent")
	const parentRef = "release/2026-07"
	writeBigFixture(t, parentPath, parentRef, n)
	parentSD := daemon.StateDirForRepo(parentPath)
	if err := Index(parentPath, "", parityRepoTag, []string{"graph-algo"}, false, false,
		WithIncremental(parentSD)); err != nil {
		t.Fatalf("index parent: %v", err)
	}

	childPath := filepath.Join(root, "wt-measure")
	const childRef = "feat/measure"
	addFixtureWorktree(t, parentPath, childPath, childRef)
	applyBigDelta(t, childPath)
	childSD := daemon.StateDirForRepo(childPath)

	// --- A: full index of the worktree, no seed ---
	runtime.GC()
	sA := startHeapSampler()
	tA := time.Now()
	if err := Index(childPath, "", parityRepoTag, []string{"graph-algo"}, false, false); err != nil {
		t.Fatalf("full index: %v", err)
	}
	durA := time.Since(tA)
	peakA := sA.peakMB()

	// --- B: seed + incremental over the delta ---
	if err := os.RemoveAll(childSD); err != nil {
		t.Fatal(err)
	}
	out := daemon.SeedWorktreeGraph(daemon.SeedRequest{
		ParentPath: parentPath, ParentRef: parentRef,
		ChildPath: childPath, ChildRef: childRef, RepoTag: parityRepoTag,
	})
	if !out.Seeded {
		t.Fatalf("seed failed: %s — %s", out.Reason, out.Detail)
	}
	if _, reason, err := daemon.VerifySeededGraph(childSD); err != nil || reason != "" {
		t.Fatalf("verify: %q %v", reason, err)
	}
	runtime.GC()
	sB := startHeapSampler()
	tB := time.Now()
	if err := Index(childPath, "", parityRepoTag, []string{"graph-algo"}, false, false,
		WithIncremental(childSD)); err != nil {
		t.Fatalf("seeded incremental index: %v", err)
	}
	durB := time.Since(tB)
	peakB := sB.peakMB()

	t.Logf("fixture files=%d  seed_bytes_copied=%d", n, out.BytesCopied)
	t.Logf("A full index      : peak_live_heap=%.1f MB  wall=%s", peakA, durA.Round(time.Millisecond))
	t.Logf("B seeded+delta    : peak_live_heap=%.1f MB  wall=%s", peakB, durB.Round(time.Millisecond))
	t.Logf("ratio B/A         : heap=%.2f  wall=%.2f", peakB/peakA, float64(durB)/float64(durA))
	t.Log("live heap = next_gc / (1 + GOGC/100); NOT RSS (macOS RSS counts MADV_FREE pages)")
}

// applyBigDelta changes exactly three files: one committed, one uncommitted,
// one untracked — the same three shapes the parity test uses.
func applyBigDelta(t *testing.T, dir string) {
	t.Helper()
	p0 := filepath.Join(dir, "svc", "s0000.go")
	b, err := os.ReadFile(p0)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p0, append(b, []byte("\nfunc CommittedOnChild(in string) string { return in }\n")...), 0o644); err != nil {
		t.Fatal(err)
	}
	seedGitRun(t, dir, "add", "-A")
	seedGitRun(t, dir, "commit", "-q", "-m", "delta")

	p1 := filepath.Join(dir, "svc", "s0001.go")
	b1, err := os.ReadFile(p1)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p1, append(b1, []byte("\nfunc UncommittedOnChild(in string) string { return in }\n")...), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "svc", "untracked.go"),
		[]byte("package svc\n\nfunc UntrackedHelper(in string) string { return in }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}
