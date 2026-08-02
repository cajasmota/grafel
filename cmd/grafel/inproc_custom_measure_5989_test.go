package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/graph"
)

// dualHeapSampler records BOTH heap metrics for the #5989 cost measurement.
//
// WHY TWO. next_gc/(1+GOGC/100) is the conventional live-heap estimate, but it
// is a LAGGING, GC-QUANTIZED proxy: it only updates when the GC sets a new
// target, so over a sub-second run with ~20 collections it cannot observe a
// peak that occurs BETWEEN marks. Peak HeapAlloc is sampled directly and has
// no such blind spot (though it includes not-yet-collected garbage). Recording
// both means the conclusion does not rest on the proxy alone — if they
// disagree, the proxy is the one to distrust.
//
// TotalAlloc is captured separately by the caller. It is cumulative
// allocation, i.e. the transient per-file garbage that dies before the next
// mark. That garbage is INVISIBLE in peak live heap at small scale but is real
// GC pressure at corpus scale, so it is the metric that actually tracks the
// cost of running ~340 extra extractors per file.
type dualHeapSampler struct {
	stop, done   chan struct{}
	gogc         float64
	peakNextGC   atomic.Uint64
	peakHeapUsed atomic.Uint64
}

func startDualHeapSampler() *dualHeapSampler {
	gogc := 100.0
	if v := os.Getenv("GOGC"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil && f > 0 {
			gogc = f
		}
	}
	h := &dualHeapSampler{stop: make(chan struct{}), done: make(chan struct{}), gogc: gogc}
	go func() {
		defer close(h.done)
		tk := time.NewTicker(5 * time.Millisecond)
		defer tk.Stop()
		var ms runtime.MemStats
		bump := func(a *atomic.Uint64, v uint64) {
			for {
				cur := a.Load()
				if v <= cur || a.CompareAndSwap(cur, v) {
					return
				}
			}
		}
		for {
			select {
			case <-h.stop:
				return
			case <-tk.C:
				runtime.ReadMemStats(&ms)
				bump(&h.peakNextGC, uint64(float64(ms.NextGC)/(1.0+h.gogc/100.0)))
				bump(&h.peakHeapUsed, ms.HeapAlloc)
			}
		}
	}()
	return h
}

func (h *dualHeapSampler) finish() (liveMB, heapAllocMB float64) {
	close(h.stop)
	<-h.done
	return float64(h.peakNextGC.Load()) / (1 << 20), float64(h.peakHeapUsed.Load()) / (1 << 20)
}

// TestInProcCustomExtractorsCost measures the cost of the #5989 gate for
// EXACTLY ONE ARM, selected by GRAFEL_CUSTOM_MEASURE_ARM=off|on.
//
// ONE ARM PER PROCESS IS THE POINT. An earlier version of this harness ran
// both arms inside a single test binary. That was wrong: the second arm always
// inherited a warmed heap and a grown GC target from the first, and reversing
// the within-rep order swung the reported heap delta from -6.6% to -1.3% — an
// order bias of the SAME MAGNITUDE as the effect being measured. A harness
// whose bias is as large as its signal cannot support a signed claim. Each arm
// now gets a pristine process; alternate arms by alternating invocations:
//
//	for i in 1 2 3; do
//	  for arm in off on; do
//	    GRAFEL_CUSTOM_MEASURE=1 GRAFEL_CUSTOM_MEASURE_ARM=$arm \
//	    GRAFEL_CUSTOM_MEASURE_REPO=/path/to/fixture \
//	      go test ./cmd/grafel/ -run TestInProcCustomExtractorsCost -count=1 -v
//	  done
//	done
//
// SCALE CAVEAT. Both fixtures used for #5989 peak under ~120 MB of live heap.
// Epic #5954 targets the GB-scale regime (24k-file corpora). Nothing here
// measures that regime, so a flat peak-heap result at this scale must NOT be
// read as "free at corpus scale" — see the TotalAlloc figure, which is where
// the per-file cost actually shows up.
func TestInProcCustomExtractorsCost(t *testing.T) {
	if os.Getenv("GRAFEL_CUSTOM_MEASURE") != "1" {
		t.Skip("set GRAFEL_CUSTOM_MEASURE=1 to run the #5989 cost measurement")
	}
	repo := os.Getenv("GRAFEL_CUSTOM_MEASURE_REPO")
	if repo == "" {
		t.Fatal("set GRAFEL_CUSTOM_MEASURE_REPO to the fixture repo path")
	}
	arm := os.Getenv("GRAFEL_CUSTOM_MEASURE_ARM")
	switch arm {
	case "off":
		t.Setenv("GRAFEL_INPROC_CUSTOM_EXTRACTORS", "")
	case "on":
		t.Setenv("GRAFEL_INPROC_CUSTOM_EXTRACTORS", "1")
	default:
		t.Fatal("set GRAFEL_CUSTOM_MEASURE_ARM to exactly one of: off, on")
	}

	state := t.TempDir()
	t.Setenv("GRAFEL_DAEMON_ROOT", t.TempDir())

	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)

	s := startDualHeapSampler()
	start := time.Now()
	err := Index(repo, filepath.Join(state, "graph.json"), "measure_repo", nil, false, false)
	wall := time.Since(start)
	liveMB, heapAllocMB := s.finish()
	runtime.ReadMemStats(&after)
	if err != nil {
		t.Fatalf("Index(arm=%s): %v", arm, err)
	}

	doc, err := graph.LoadGraphFromDir(state)
	if err != nil {
		t.Fatalf("LoadGraphFromDir(arm=%s): %v", arm, err)
	}
	kinds := map[string]int{}
	for i := range doc.Entities {
		kinds[doc.Entities[i].Kind]++
	}
	var ks []string
	for k := range kinds {
		ks = append(ks, k)
	}
	sort.Strings(ks)

	totalAllocMB := float64(after.TotalAlloc-before.TotalAlloc) / (1 << 20)

	// Single machine-readable line so an external driver can aggregate across
	// process invocations without parsing prose.
	fmt.Printf("MEASURE arm=%s wall_s=%.3f peak_live_mb=%.1f peak_heapalloc_mb=%.1f total_alloc_mb=%.1f num_gc=%d entities=%d rels=%d\n",
		arm, wall.Seconds(), liveMB, heapAllocMB, totalAllocMB,
		after.NumGC-before.NumGC, len(doc.Entities), len(doc.Relationships))
	for _, k := range ks {
		fmt.Printf("KIND arm=%s %s=%d\n", arm, k, kinds[k])
	}
}
