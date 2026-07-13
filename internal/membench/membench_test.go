package membench

import (
	"os"
	"runtime"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/external"
	"github.com/cajasmota/grafel/internal/graph"
)

// specFromEnv returns the fixture spec, scaled down by default so a plain
// `go test ./internal/membench` runs in bounded time/heap, but overridable to
// the full #5681 monorepo scale via GRAFEL_MEMBENCH_ENTITIES=220000 (and the
// companion knobs) for the real measurement run.
func specFromEnv() FixtureSpec {
	s := FixtureSpec{
		Entities:         40_000,
		Files:            4_000,
		CallEdges:        200_000,
		ImportsPerFile:   16,
		ExternalPackages: 2_000,
		Seed:             0x5681,
	}
	if v := os.Getenv("GRAFEL_MEMBENCH_ENTITIES"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			s = DefaultLargeSpec()
			s.Entities = n
			// Scale companion counts proportionally to keep a realistic density.
			s.Files = n / 12
			s.CallEdges = n * 5
		}
	}
	return s
}

// runPipeline executes the in-process post-extraction pipeline the split-mode
// engine runs per repo: external synthesis + the group-scope algorithm pass.
func runPipeline(doc *graph.Document) *graph.AlgorithmResults {
	external.Synthesize(doc)
	return graph.RunAlgorithms(doc.Entities, doc.Relationships)
}

// TestSingleRunPeak measures the transient peak heap of ONE pipeline run and
// whether memory is RELEASED (bounded) or RETAINED (leak) after the result is
// dropped. It also asserts the sampled-betweenness path is taken so an
// accidental regression back to exact O(V*E) Brandes is caught.
func TestSingleRunPeak(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping heavy memory bench in -short")
	}
	spec := specFromEnv()
	doc := BuildSyntheticDocument(spec)

	nEntities := len(doc.Entities)
	if got, thr := nEntities, graph.BetweennessSampleThreshold(); got <= thr {
		t.Fatalf("fixture too small to exercise sampled-betweenness path: entities=%d threshold=%d", got, thr)
	}

	base := snapshot() // settled heap holding just the fixture doc
	sampler := startPeakSampler(2 * time.Millisecond)
	res := runPipeline(doc)
	if res == nil || len(res.PageRank) == 0 {
		t.Fatalf("pipeline produced no results")
	}
	peakInuse, peakAlloc := sampler.Stop()

	// Drop every live reference to the pipeline output + input and force GC,
	// then measure what the process RETAINS. Bounded pipeline => retained ~=
	// baseline; a leak => retained stays near peak.
	res = nil
	after := snapshot()

	// Peak ATTRIBUTABLE to the pipeline (above the resting fixture heap).
	pipelinePeak := int64(peakInuse) - int64(base.HeapInuse)
	retainedDelta := int64(after.HeapInuse) - int64(base.HeapInuse)

	t.Logf("SINGLE-RUN entities=%d rels=%d", nEntities, len(doc.Relationships))
	t.Logf("  baseline  HeapInuse=%d MB HeapAlloc=%d MB", mb(base.HeapInuse), mb(base.HeapAlloc))
	t.Logf("  PEAK      HeapInuse=%d MB HeapAlloc=%d MB", mb(peakInuse), mb(peakAlloc))
	t.Logf("  after-GC  HeapInuse=%d MB HeapAlloc=%d MB", mb(after.HeapInuse), mb(after.HeapAlloc))
	t.Logf("  pipeline-attributable peak = %d MB", pipelinePeak/(1024*1024))
	t.Logf("  retained-after-run delta   = %d MB", retainedDelta/(1024*1024))

	// Correctness/bounded assertion: the single run must RELEASE its heap.
	// After dropping the result the retained delta must be a small fraction of
	// the transient peak (allow generous slack for GC fragmentation).
	if pipelinePeak > 0 && retainedDelta > pipelinePeak/2 {
		t.Errorf("pipeline heap NOT released: retained=%d MB is >50%% of transient peak=%d MB (possible leak)",
			retainedDelta/(1024*1024), pipelinePeak/(1024*1024))
	}
	runtime.KeepAlive(doc)
}

// TestOverlapPeak fires TWO concurrent pipeline runs on the same synthetic
// input and measures peak heap. If the peak is ~2x a single run, it confirms
// that OVERLAPPING (un-single-flighted) index runs are the unbounded RSS
// driver: each coexisting run adds its full multi-GB heap. This is the
// behaviour the P0 single-flight fix must prevent at the daemon level.
func TestOverlapPeak(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping heavy memory bench in -short")
	}
	spec := specFromEnv()

	// Two independent docs so both runs hold live heap simultaneously (mirrors
	// two coexisting engine index runs, each with its own assembled doc).
	docA := BuildSyntheticDocument(spec)
	docB := BuildSyntheticDocument(spec)

	base := snapshot()
	sampler := startPeakSampler(2 * time.Millisecond)

	var wg sync.WaitGroup
	wg.Add(2)
	var rA, rB *graph.AlgorithmResults
	go func() { defer wg.Done(); rA = runPipeline(docA) }()
	go func() { defer wg.Done(); rB = runPipeline(docB) }()
	wg.Wait()

	peakInuse, peakAlloc := sampler.Stop()
	if rA == nil || rB == nil {
		t.Fatalf("concurrent pipelines produced nil results")
	}
	overlapPeak := int64(peakInuse) - int64(base.HeapInuse)

	t.Logf("OVERLAP(2x) entities=%d each", len(docA.Entities))
	t.Logf("  baseline HeapInuse=%d MB", mb(base.HeapInuse))
	t.Logf("  PEAK     HeapInuse=%d MB HeapAlloc=%d MB", mb(peakInuse), mb(peakAlloc))
	t.Logf("  overlap-attributable peak = %d MB", overlapPeak/(1024*1024))

	runtime.KeepAlive(docA)
	runtime.KeepAlive(docB)
}
