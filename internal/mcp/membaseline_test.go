package mcp

// Reusable before/after resident-memory measurement harness for grafel's
// served graph. Given a path to a graph.fb, it loads the graph through the
// SAME code path the daemon/MCP uses to build the resident structure —
// graph.LoadGraphFromDir -> graph.Document, then the mcp-side LoadedRepo with
// its eager LabelIndex and the lazily-built derived indexes (byID, adjacency,
// calls/step adjacency, top-K PageRank) that a served repo materializes — then
// reports on-disk size, resident HeapInuse/HeapAlloc (after forced GCs), the
// process RSS, entity/relationship counts, and the derived per-record and
// inflation ratios.
//
// It is GATED behind the GRAFEL_MEM_BASELINE_FB env var so it never runs in a
// normal `go test ./...`; it only executes when explicitly pointed at a
// graph.fb. This makes it re-runnable against a "before" and later an "after"
// graph.fb for an apples-to-apples delta.
//
// RUN (point GRAFEL_MEM_BASELINE_FB at any graph.fb):
//
//	GRAFEL_MEM_BASELINE_FB=/path/to/graph.fb \
//	  go test ./internal/mcp/ -run TestMemBaseline -v -count=1 -timeout 30m
//
// Output is numeric aggregates only (sizes, counts, MB, per-record bytes).

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
)

// rssFromPS reads the current process RSS via `ps -o rss= -p <pid>`, which
// reports resident-set size in KiB on darwin/BSD/linux. Returns 0 on failure.
func rssFromPS() uint64 {
	out, err := exec.Command("ps", "-o", "rss=", "-p", strconv.Itoa(os.Getpid())).Output()
	if err != nil {
		return 0
	}
	kib, err := strconv.ParseUint(strings.TrimSpace(string(out)), 10, 64)
	if err != nil {
		return 0
	}
	return kib * 1024
}

// rssBytes returns the current process resident-set size in bytes, or 0 if it
// cannot be read on this platform. macOS/BSD `ps -o rss=` reports KiB; Linux
// /proc/self/statm reports pages. We prefer /proc when present (Linux) and fall
// back to a self-contained rusage read that works on darwin.
func rssBytes(t *testing.T) uint64 {
	// Linux: /proc/self/statm — field 2 is resident pages.
	if b, err := os.ReadFile("/proc/self/statm"); err == nil {
		var size, resident uint64
		if _, serr := fmt.Sscanf(string(b), "%d %d", &size, &resident); serr == nil {
			return resident * uint64(os.Getpagesize())
		}
	}
	// Darwin/BSD: getrusage ru_maxrss is in BYTES on darwin, KiB on linux.
	// We already handled linux above; on darwin ru_maxrss is bytes. Use it via
	// a tiny syscall-free path through runtime is not possible, so shell out to
	// `ps` which is universally available and reports RSS in KiB on darwin.
	if v := rssFromPS(); v > 0 {
		return v
	}
	return 0
}

func mbf(b uint64) float64 { return float64(b) / (1024 * 1024) }

func TestMemBaseline(t *testing.T) {
	fbPath := os.Getenv("GRAFEL_MEM_BASELINE_FB")
	if fbPath == "" {
		t.Skip("set GRAFEL_MEM_BASELINE_FB=/path/to/graph.fb to run the resident-memory baseline harness")
	}

	fi, err := os.Stat(fbPath)
	if err != nil {
		t.Fatalf("stat graph.fb: %v", err)
	}
	if fi.IsDir() {
		fbPath = filepath.Join(fbPath, "graph.fb")
		if fi, err = os.Stat(fbPath); err != nil {
			t.Fatalf("stat graph.fb in dir: %v", err)
		}
	}
	onDisk := uint64(fi.Size())
	stateDir := filepath.Dir(fbPath)

	// --- Baseline heap: settle the process before we load anything. ---
	runtime.GC()
	runtime.GC()
	var mBase runtime.MemStats
	runtime.ReadMemStats(&mBase)

	// --- Stage 1: materialize graph.Document via the real loader. ---
	doc, err := graph.LoadGraphFromDir(stateDir)
	if err != nil {
		t.Fatalf("LoadGraphFromDir: %v", err)
	}
	nEnt := len(doc.Entities)
	nRel := len(doc.Relationships)
	if nEnt == 0 {
		t.Fatalf("graph loaded 0 entities — wrong path?")
	}

	runtime.GC()
	runtime.GC()
	var mDoc runtime.MemStats
	runtime.ReadMemStats(&mDoc)

	// --- Stage 2: build the mcp-side resident structures a served repo holds.
	// LabelIndex is built eagerly at reload (state.go reloadLocked); the derived
	// traversal indexes are built lazily on first tool use — we force them all
	// so the number reflects a fully-warmed served repo, not a cold one. ---
	lr := &LoadedRepo{
		Repo:      "corpus",
		Doc:       doc,
		GraphFile: fbPath,
	}
	lr.LabelIndex = BuildLabelIndex(doc) // eager, as in reloadLocked
	// Force every lazy derived index the served repo materializes on use.
	_ = lr.getByID()
	_ = lr.getAdjacency()
	_ = lr.getCallsAdj()
	_ = lr.getStepAdj()
	_ = lr.getTopKPageRank()
	// BM25 is the heavy search index also held resident once search is used.
	bm25 := lr.getBM25()

	// Drop transient garbage from index construction, settle, and measure the
	// fully-warmed resident footprint.
	runtime.GC()
	runtime.GC()
	var mFull runtime.MemStats
	runtime.ReadMemStats(&mFull)

	rss := rssBytes(t)

	// Keep everything alive across the measurement so GC can't reclaim it.
	runtime.KeepAlive(doc)
	runtime.KeepAlive(lr)
	runtime.KeepAlive(bm25)

	// --- Derived aggregates. ---
	// Resident heap attributable to Document alone (stage1 - baseline).
	docHeapInuse := int64(mDoc.HeapInuse) - int64(mBase.HeapInuse)
	docHeapAlloc := int64(mDoc.HeapAlloc) - int64(mBase.HeapAlloc)
	// Resident heap attributable to the mcp index maps (stage2 - stage1).
	idxHeapInuse := int64(mFull.HeapInuse) - int64(mDoc.HeapInuse)
	idxHeapAlloc := int64(mFull.HeapAlloc) - int64(mDoc.HeapAlloc)
	// Full resident (Document + all served indexes), above the empty-process base.
	fullHeapInuse := int64(mFull.HeapInuse) - int64(mBase.HeapInuse)
	fullHeapAlloc := int64(mFull.HeapAlloc) - int64(mBase.HeapAlloc)

	nRecords := nEnt + nRel
	perEntInuse := float64(fullHeapInuse) / float64(nEnt)
	perRelInuse := float64(fullHeapInuse) / float64(nRel)
	perRecordInuse := float64(fullHeapInuse) / float64(nRecords)
	perRecordOnDisk := float64(onDisk) / float64(nRecords)
	inflation := float64(fullHeapInuse) / float64(onDisk)

	t.Logf("=== grafel resident-memory baseline (numbers only) ===")
	t.Logf("on-disk graph.fb bytes        : %d (%.1f MB)", onDisk, mbf(onDisk))
	t.Logf("entities                      : %d", nEnt)
	t.Logf("relationships                 : %d", nRel)
	t.Logf("records (ent+rel)             : %d", nRecords)
	t.Logf("--- absolute MemStats snapshots (HeapInuse / HeapAlloc, MB) ---")
	t.Logf("  empty-process baseline      : %.1f / %.1f MB", mbf(mBase.HeapInuse), mbf(mBase.HeapAlloc))
	t.Logf("  after Document load         : %.1f / %.1f MB", mbf(mDoc.HeapInuse), mbf(mDoc.HeapAlloc))
	t.Logf("  after full LoadedRepo warm  : %.1f / %.1f MB", mbf(mFull.HeapInuse), mbf(mFull.HeapAlloc))
	t.Logf("--- attributable resident deltas (bytes / MB) ---")
	t.Logf("  Document alone   HeapInuse  : %d (%.1f MB)", docHeapInuse, float64(docHeapInuse)/(1024*1024))
	t.Logf("  Document alone   HeapAlloc  : %d (%.1f MB)", docHeapAlloc, float64(docHeapAlloc)/(1024*1024))
	t.Logf("  mcp indexes only HeapInuse  : %d (%.1f MB)", idxHeapInuse, float64(idxHeapInuse)/(1024*1024))
	t.Logf("  mcp indexes only HeapAlloc  : %d (%.1f MB)", idxHeapAlloc, float64(idxHeapAlloc)/(1024*1024))
	t.Logf("  FULL resident    HeapInuse  : %d (%.1f MB)", fullHeapInuse, float64(fullHeapInuse)/(1024*1024))
	t.Logf("  FULL resident    HeapAlloc  : %d (%.1f MB)", fullHeapAlloc, float64(fullHeapAlloc)/(1024*1024))
	t.Logf("--- process RSS ---")
	if rss > 0 {
		t.Logf("  process RSS (warmed)        : %d (%.1f MB)", rss, mbf(rss))
	} else {
		t.Logf("  process RSS (warmed)        : unavailable on this platform")
	}
	t.Logf("--- derived per-record (resident FULL HeapInuse basis) ---")
	t.Logf("  bytes / entity  (resident)  : %.1f", perEntInuse)
	t.Logf("  bytes / rel     (resident)  : %.1f", perRelInuse)
	t.Logf("  bytes / record  (resident)  : %.1f", perRecordInuse)
	t.Logf("  bytes / record  (on-disk)   : %.1f", perRecordOnDisk)
	t.Logf("  resident / on-disk inflation: %.2fx", inflation)
}
