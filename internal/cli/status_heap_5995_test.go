package cli

// Memory characterisation + regression guard for `grafel status` (#5995,
// epic #5954).
//
// ComputeStatusSummary used to call graph.LoadGraphFromDir per repo, which
// materialises EVERY entity and EVERY relationship of the repo's graph onto
// the Go heap. It used four header fields (ref/sha/worktree/files) plus two
// entity-kind tallies, and walked the relationship vector to fill a
// `hasIncoming` map that nothing ever read. On a corpus-sized graph that is
// hundreds of megabytes materialised to print two counts.
//
// Metric discipline: RSS is NOT used here. macOS RSS counts MADV_FREE pages
// and is an upper bound, not a footprint (see cmd/grafel/worktree_seed_heap_test.go).
// The guard below asserts on TotalAlloc — cumulative bytes allocated, which is
// deterministic and immune to GC timing — and the opt-in characterisation test
// additionally reports peak LIVE heap (next_gc / (1 + GOGC/100)) and peak
// HeapAlloc.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/daemon"
	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/graph/fbwriter"
	"github.com/cajasmota/grafel/internal/registry"
)

// buildStatusHeapFixture writes a repo state dir containing a flat graph.fb
// with nEnt entities and nRel relationships, plus the graph-stats.json sidecar
// the status path reads first. Returns the repo path.
func buildStatusHeapFixture(t *testing.T, root string, slug string, nEnt, nRel int) string {
	t.Helper()

	repoPath := filepath.Join(root, slug)
	if err := os.MkdirAll(repoPath, 0o755); err != nil {
		t.Fatalf("mkdir repo: %v", err)
	}
	stateDir := daemon.StateDirForRepo(repoPath)
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatalf("mkdir state: %v", err)
	}

	doc := &graph.Document{
		Repo:       slug,
		IndexedRef: "main",
		IndexedSHA: "abcdef123456",
		IsWorktree: false,
		Entities:   make([]graph.Entity, 0, nEnt),
	}
	for i := 0; i < nEnt; i++ {
		kind := "function"
		switch i % 10 {
		case 0:
			kind = "http_endpoint_definition"
		case 1:
			kind = "process"
		}
		doc.Entities = append(doc.Entities, graph.Entity{
			ID:            fmt.Sprintf("ent-%08d", i),
			Name:          fmt.Sprintf("Symbol%08d", i),
			QualifiedName: fmt.Sprintf("pkg/mod/sub.Symbol%08d", i),
			Kind:          kind,
			SourceFile:    fmt.Sprintf("src/pkg%03d/file%05d.go", i%200, i%5000),
			Language:      "go",
			Signature:     fmt.Sprintf("func Symbol%08d(ctx context.Context, in *Request) (*Response, error)", i),
			StartLine:     i % 900,
			EndLine:       i%900 + 12,
		})
	}
	doc.Relationships = make([]graph.Relationship, 0, nRel)
	for i := 0; i < nRel; i++ {
		doc.Relationships = append(doc.Relationships, graph.Relationship{
			ID:     fmt.Sprintf("rel-%08d", i),
			FromID: fmt.Sprintf("ent-%08d", i%nEnt),
			ToID:   fmt.Sprintf("ent-%08d", (i*7+3)%nEnt),
			Kind:   "calls",
		})
	}
	if err := fbwriter.WriteAtomic(filepath.Join(stateDir, "graph.fb"), doc); err != nil {
		t.Fatalf("write graph.fb: %v", err)
	}
	// Free the fixture document before any measurement starts.
	doc = nil
	_ = doc
	runtime.GC()

	side := graph.GraphStatsSidecar{
		Version:            1,
		ComputedAt:         time.Now().Add(-3 * time.Minute),
		TotalFiles:         5000,
		TotalEntities:      nEnt,
		TotalRelationships: nRel,
	}
	sideData, _ := json.Marshal(side)
	if err := os.WriteFile(filepath.Join(stateDir, "graph-stats.json"), sideData, 0o644); err != nil {
		t.Fatalf("write sidecar: %v", err)
	}
	return repoPath
}

// TestStatusSummaryDoesNotMaterialiseGraph is the behavioural regression guard
// for #5995. It measures the cumulative bytes ComputeStatusSummary allocates
// over a graph with a large relationship vector and fails if that figure is
// consistent with materialising the graph.
//
// Why TotalAlloc and not peak heap: TotalAlloc is deterministic (no GC-timing
// dependence), and it is the metric a reintroduced LoadGraphFromDir cannot
// hide from — materialising N relationships must allocate them, whether or not
// the GC has reclaimed them by the time the call returns.
//
// The ceiling has to kill BOTH regressions, which is why it is not merely
// "under what materialising the relationships would cost":
//
//   - full re-materialisation (LoadGraphFromDir again): 62.9 MB on this
//     fixture. A relationship-derived ceiling catches this easily — 300k
//     relationships are >30 MB of Relationship structs alone.
//   - the PARTIAL regression: swapping EachEntityKind for a full EachEntity
//     walk, i.e. reinstating the entity decode but not the relationship one.
//     That is 6.6 MB — a 9.4x rise that a relationship-scaled ceiling sails
//     straight past, because 20k entities are only ~34x smaller than the
//     bound that catches 300k relationships.
//
// So the bound is set from the measured floor instead: 0.7 MB with the kind-
// only walk, ceiling 4 MB. That kills the partial mutant with room to spare
// and still leaves ~5x headroom for interner/GC noise and fixture drift. It
// remains hazard-consistent — 4 MB is nowhere near "on the order of the
// graph" for any corpus-sized repo.
func TestStatusSummaryDoesNotMaterialiseGraph(t *testing.T) {
	if testing.Short() {
		t.Skip("allocation measurement; skipped under -short")
	}
	tmpDir := t.TempDir()
	t.Setenv(daemon.EnvRoot, tmpDir)

	const nEnt = 20000
	const nRel = 300000
	repoPath := buildStatusHeapFixture(t, tmpDir, "heapfix", nEnt, nRel)
	repos := []registry.Repo{{Slug: "heapfix", Path: repoPath}}

	// Warm any lazily-initialised package state so it is not attributed to
	// the measured call.
	_ = ComputeStatusSummary("grp", repos)
	runtime.GC()

	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	summary := ComputeStatusSummary("grp", repos)
	runtime.ReadMemStats(&after)
	runtime.KeepAlive(summary)

	allocated := after.TotalAlloc - before.TotalAlloc
	const ceiling = 4 << 20 // 4 MB — see the ceiling rationale above

	t.Logf("ComputeStatusSummary allocated %.1f MB over a %d-entity / %d-relationship graph",
		float64(allocated)/(1<<20), nEnt, nRel)

	if allocated > ceiling {
		t.Fatalf("ComputeStatusSummary allocated %.1f MB (ceiling %.1f MB): the graph is being "+
			"materialised to answer a status poll (#5995)",
			float64(allocated)/(1<<20), float64(ceiling)/(1<<20))
	}

	// The cheap path must still report the real numbers.
	rs := summary.RepoStats["heapfix"]
	if rs == nil {
		t.Fatal("heapfix missing from RepoStats")
	}
	if rs.Entities != nEnt || rs.Relationships != nRel {
		t.Fatalf("counts = %d/%d, want %d/%d", rs.Entities, rs.Relationships, nEnt, nRel)
	}
	if rs.IndexedRef != "main" || rs.IndexedSHA != "abcdef123456" {
		t.Fatalf("git metadata = %q/%q, want main/abcdef123456", rs.IndexedRef, rs.IndexedSHA)
	}
	if summary.HTTPEndpoints != nEnt/10 {
		t.Fatalf("HTTPEndpoints = %d, want %d", summary.HTTPEndpoints, nEnt/10)
	}
	if summary.ProcessFlows != nEnt/10 {
		t.Fatalf("ProcessFlows = %d, want %d", summary.ProcessFlows, nEnt/10)
	}
	if rs.GraphLoadError != "" {
		t.Fatalf("GraphLoadError = %q, want empty", rs.GraphLoadError)
	}
}

// statusHeapSampler samples the live heap (next_gc / (1 + GOGC/100)) and
// HeapAlloc, tracking the peak of each.
type statusHeapSampler struct {
	peakLive  atomic.Uint64
	peakAlloc atomic.Uint64
	stop      chan struct{}
	done      chan struct{}
	gogc      float64
}

func startStatusHeapSampler() *statusHeapSampler {
	gogc := 100.0
	if v := os.Getenv("GOGC"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil && f > 0 {
			gogc = f
		}
	}
	h := &statusHeapSampler{stop: make(chan struct{}), done: make(chan struct{}), gogc: gogc}
	go func() {
		defer close(h.done)
		t := time.NewTicker(5 * time.Millisecond)
		defer t.Stop()
		var ms runtime.MemStats
		for {
			select {
			case <-h.stop:
				return
			case <-t.C:
				runtime.ReadMemStats(&ms)
				live := uint64(float64(ms.NextGC) / (1.0 + h.gogc/100.0))
				if live > h.peakLive.Load() {
					h.peakLive.Store(live)
				}
				if ms.HeapAlloc > h.peakAlloc.Load() {
					h.peakAlloc.Store(ms.HeapAlloc)
				}
			}
		}
	}()
	return h
}

func (h *statusHeapSampler) stopAndReport() (liveMB, allocMB float64) {
	close(h.stop)
	<-h.done
	return float64(h.peakLive.Load()) / (1 << 20), float64(h.peakAlloc.Load()) / (1 << 20)
}

// TestStatusSummaryPeakHeapMeasure is the opt-in characterisation run behind
// GRAFEL_STATUS_HEAP_MEASURE=1. It reports peak live heap AND peak HeapAlloc
// for ComputeStatusSummary over a fixture whose size is controlled by
// GRAFEL_STATUS_HEAP_ENTS (default 100000; relationships = 8x entities, the
// corpus mean degree). It asserts nothing — it exists so the before/after
// figures quoted on #5995 are reproducible from this tree.
func TestStatusSummaryPeakHeapMeasure(t *testing.T) {
	if os.Getenv("GRAFEL_STATUS_HEAP_MEASURE") != "1" {
		t.Skip("set GRAFEL_STATUS_HEAP_MEASURE=1 to run the status peak-heap measurement")
	}
	nEnt := 100000
	if v := os.Getenv("GRAFEL_STATUS_HEAP_ENTS"); v != "" {
		if k, err := strconv.Atoi(v); err == nil && k > 0 {
			nEnt = k
		}
	}
	nRel := nEnt * 8

	tmpDir := t.TempDir()
	t.Setenv(daemon.EnvRoot, tmpDir)
	repoPath := buildStatusHeapFixture(t, tmpDir, "measure", nEnt, nRel)
	repos := []registry.Repo{{Slug: "measure", Path: repoPath}}

	runtime.GC()
	var base runtime.MemStats
	runtime.ReadMemStats(&base)
	baseLive := float64(base.NextGC) / 2.0 / (1 << 20)

	s := startStatusHeapSampler()
	summary := ComputeStatusSummary("grp", repos)
	runtime.KeepAlive(summary)
	liveMB, allocMB := s.stopAndReport()

	var after runtime.MemStats
	runtime.ReadMemStats(&after)

	t.Logf("#5995 status heap: entities=%d relationships=%d", nEnt, nRel)
	t.Logf("  baseline live heap : %.1f MB", baseLive)
	t.Logf("  PEAK live heap     : %.1f MB", liveMB)
	t.Logf("  PEAK HeapAlloc     : %.1f MB", allocMB)
	t.Logf("  TotalAlloc delta   : %.1f MB", float64(after.TotalAlloc-base.TotalAlloc)/(1<<20))
	t.Logf("  reported: %d entities, %d rels, %d endpoints, %d flows",
		summary.TotalEntities, summary.TotalRelationships, summary.HTTPEndpoints, summary.ProcessFlows)
}
