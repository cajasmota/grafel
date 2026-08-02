package main

// Peak live-heap profiling for the incremental path's prior-graph handling
// (epic #5954, item 16).
//
// Opt-in only: set GRAFEL_INC_HEAP_MEASURE=1. It builds an N-file fixture,
// indexes it fully, then runs a seeded incremental pass over a 3-file delta
// while sampling the live heap and dumping an inuse_space heap profile every
// time a new peak is observed. The last profile written therefore approximates
// the allocation shape AT the peak, which is the only thing that matters here:
// post-run profiles show the settled graph, not the transient double.
//
// Metric is LIVE HEAP — next_gc / (1 + GOGC/100) — NOT RSS. See
// worktree_seed_heap_test.go for why RSS is unusable on macOS.
//
// MEASURED FINDINGS from the change this file was written to justify
// (streaming the prior graph instead of materialising it — #5954 item 16).
// 16 GB macOS laptop, swap steady at 3.84 GB used across every run, 3-file
// delta.
//
// PROVENANCE — read before quoting any of these. Only the "run peak" and
// "inc/full" rows are reproducible from this tree: they are what
// TestIncrementalPriorGraphPeakHeapProfile logs when run with
// GRAFEL_INC_HEAP_MEASURE=1 and GRAFEL_INC_HEAP_FILES set to the stated file
// count. The "merge-end" rows are marked AD-HOC: they came from a temporary
// runtime.GC()+ReadMemStats probe hand-inserted at the last statement of the
// merge (prior graph still reachable), which was NOT committed. Nothing in
// this file emits them, and re-running the committed test will not reproduce
// them. They are retained because they are the lower-variance signal for the
// structural claim; they are not evidence this test produces.
//
//	files   metric              before      after      delta
//	 2500   merge-end (AD-HOC)  148.1 MB    140.6 MB   -7.5 MB  (-5.1%)
//	 2500   run peak            ~140 MB     ~141 MB    no significant change
//	 7000   merge-end (AD-HOC)  304.9 MB    284.9 MB   -20.0 MB (-6.6%)
//	 7000   run peak            309.8 MB    282.7 MB   -27.1 MB (-8.7%)
//	 7000   inc/full            1.56-1.65   1.50-1.52
//
// Read the 2500-file row carefully: the merge point is NOT the run's peak at
// that scale, so a real 7.5 MB saving at the merge moves the run peak by
// nothing. Only once the graph dominates the fixed cost (extraction buffers,
// resolver index, package-level regexp compilation) does the merge become the
// peak and the saving show up end-to-end. Anyone quoting a single number for
// this change must say which scale it came from.
//
// What was tried and REJECTED on measurement, recorded so it is not retried:
//
//   - Memoizing the entity walk in GraphStream to avoid decoding entities twice
//     (the indexer walks them once to seed the resolver, once at the merge).
//     7000 files: 302.2 MB with the cache vs 282.6 MB without. The cache array
//     costs more than the duplicated Name/property copies it saves.
//   - Replacing the merge's dedupe-key strings with a 128-bit digest. The
//     10.2 MB attributed to mergeIncrementalPrevDoc at the peak turned out to
//     be doc.Entities/doc.Relationships APPEND GROWTH, not the key map; the
//     digest saved nothing measurable and was dropped rather than carry
//     re-derivation risk over #6085's dedupe semantics for no gain.

import (
	"os"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/daemon"
)

// profilingHeapSampler is heapSampler plus a heap-profile dump on every new
// peak. Kept separate so the plain sampler stays allocation-free.
type profilingHeapSampler struct {
	peak     atomic.Uint64
	stop     chan struct{}
	done     chan struct{}
	gogc     float64
	profPath string
}

func startProfilingHeapSampler(profPath string) *profilingHeapSampler {
	gogc := 100.0
	if v := os.Getenv("GOGC"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil && f > 0 {
			gogc = f
		}
	}
	h := &profilingHeapSampler{
		stop:     make(chan struct{}),
		done:     make(chan struct{}),
		gogc:     gogc,
		profPath: profPath,
	}
	go func() {
		defer close(h.done)
		t := time.NewTicker(50 * time.Millisecond)
		defer t.Stop()
		var ms runtime.MemStats
		for {
			select {
			case <-h.stop:
				return
			case <-t.C:
				runtime.ReadMemStats(&ms)
				live := uint64(float64(ms.NextGC) / (1.0 + h.gogc/100.0))
				if live > h.peak.Load() {
					h.peak.Store(live)
					if h.profPath != "" {
						if f, err := os.Create(h.profPath); err == nil {
							_ = pprof.Lookup("heap").WriteTo(f, 0)
							_ = f.Close()
						}
					}
				}
			}
		}
	}()
	return h
}

func (h *profilingHeapSampler) peakMB() float64 {
	close(h.stop)
	<-h.done
	return float64(h.peak.Load()) / (1 << 20)
}

func TestIncrementalPriorGraphPeakHeapProfile(t *testing.T) {
	if os.Getenv("GRAFEL_INC_HEAP_MEASURE") != "1" {
		t.Skip("set GRAFEL_INC_HEAP_MEASURE=1 to run the incremental peak-heap profile")
	}
	n := 1200
	if v := os.Getenv("GRAFEL_INC_HEAP_FILES"); v != "" {
		if k, err := strconv.Atoi(v); err == nil && k > 0 {
			n = k
		}
	}
	outDir := os.Getenv("GRAFEL_INC_HEAP_OUT")
	if outDir == "" {
		outDir = t.TempDir()
	}

	root := t.TempDir()
	t.Setenv("GRAFEL_DAEMON_ROOT", filepath.Join(root, "daemonroot"))

	repo := filepath.Join(root, "repo")
	writeBigFixture(t, repo, "main", n)
	sd := daemon.StateDirForRepo(repo)

	// --- A: full index, profiled ---
	runtime.GC()
	sA := startProfilingHeapSampler(filepath.Join(outDir, "full.heap"))
	tA := time.Now()
	if err := Index(repo, "", parityRepoTag, []string{"graph-algo"}, false, false,
		WithIncremental(sd)); err != nil {
		t.Fatalf("full index: %v", err)
	}
	durA := time.Since(tA)
	peakA := sA.peakMB()

	// --- B: incremental over a 3-file delta, profiled ---
	applyBigDelta(t, repo)
	runtime.GC()
	sB := startProfilingHeapSampler(filepath.Join(outDir, "incremental.heap"))
	tB := time.Now()
	if err := Index(repo, "", parityRepoTag, []string{"graph-algo"}, false, false,
		WithIncremental(sd)); err != nil {
		t.Fatalf("incremental index: %v", err)
	}
	durB := time.Since(tB)
	peakB := sB.peakMB()

	t.Logf("fixture files=%d  profiles in %s", n, outDir)
	t.Logf("A full        : peak_live_heap=%.1f MB  wall=%s", peakA, durA.Round(time.Millisecond))
	t.Logf("B incremental : peak_live_heap=%.1f MB  wall=%s", peakB, durB.Round(time.Millisecond))
	t.Logf("ratio B/A     : heap=%.2f  wall=%.2f", peakB/peakA, float64(durB)/float64(durA))
}
