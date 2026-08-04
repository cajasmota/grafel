package sresolver

import (
	"fmt"
	"runtime"
	"runtime/debug"
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
)

// ─────────────────────────────────────────────────────────────────────────────
// Memory characterisation of the member tiers.
//
// The reason #6098 was left open rather than half-fixed is that the
// corpus-wide resolver's member tier is built over the ENTIRE entity set, and
// the whole point of the scoped path is that it does not hold a corpus-sized
// index (epic #5954; a corpus A/B measured peak HeapAlloc at ~3.0 GB).
//
// This file pins the property that makes the port safe: the index is built
// over the package directories of the CHANGED files only, so its retained size
// is a function of the edit, not of the repository. The structural assertion
// below is deterministic; the reported byte figures come from
// TestMemberIndexes_HeapCost, which is a measurement, not a threshold gate.
// ─────────────────────────────────────────────────────────────────────────────

// memFixture builds a synthetic corpus: dirs packages of filesPerDir files,
// each declaring one type with methodsPerType methods. Entity names are
// dotted (`T<d>_<f>.M<m>`) so every one of them is a member-tier candidate —
// the worst case for index size.
func memFixture(dirs, filesPerDir, methodsPerType int) []graph.Entity {
	ents := make([]graph.Entity, 0, dirs*filesPerDir*(methodsPerType+1))
	for d := 0; d < dirs; d++ {
		for f := 0; f < filesPerDir; f++ {
			file := fmt.Sprintf("pkg%04d/f%03d.go", d, f)
			typ := fmt.Sprintf("T%04d_%03d", d, f)
			ents = append(ents, graph.Entity{
				ID: fmt.Sprintf("%016x", len(ents)), Name: typ,
				Kind: "SCOPE.Component", SourceFile: file, Language: "go",
			})
			for m := 0; m < methodsPerType; m++ {
				ents = append(ents, graph.Entity{
					ID: fmt.Sprintf("%016x", len(ents)), Name: fmt.Sprintf("%s.M%02d", typ, m),
					Kind: "SCOPE.Operation", SourceFile: file, Language: "go",
				})
			}
		}
	}
	return ents
}

// countEntries returns the number of leaf map entries the index retains.
func (idx *memberIndexes) countEntries() int {
	if idx == nil {
		return 0
	}
	n := len(idx.callerLoc)
	for _, pkg := range idx.byPackageMember {
		for _, scope := range pkg {
			n += len(scope)
		}
	}
	for _, b := range idx.leafByFile {
		n += len(b)
	}
	for _, b := range idx.leafByPkg {
		n += len(b)
	}
	return n
}

// TestMemberIndexes_RetainedSizeIsDeltaBounded is the guard that keeps the
// corpus-sized index out. A one-package edit against a 200-package corpus must
// retain entries for that ONE package, and the count must not grow when the
// corpus around it grows.
//
// This is asserted structurally rather than in bytes so it cannot go flaky:
// deleting the probeDirs gate makes the retained count scale with the corpus
// and both assertions below fail immediately.
func TestMemberIndexes_RetainedSizeIsDeltaBounded(t *testing.T) {
	const filesPerDir, methods = 20, 5
	// The delta: one file's worth of freshly extracted entities, in pkg0000.
	fresh := memFixture(1, 1, methods)

	small := buildMemberIndexes(fresh, memFixture(50, filesPerDir, methods))
	large := buildMemberIndexes(fresh, memFixture(200, filesPerDir, methods))
	if small == nil || large == nil {
		t.Fatal("index was not built")
	}

	nSmall, nLarge := small.countEntries(), large.countEntries()
	if nSmall != nLarge {
		t.Errorf("retained entry count moved with corpus size (%d entries against 50 packages, "+
			"%d against 200) — the index is corpus-scoped, which is exactly the peak the scoped "+
			"path exists to avoid (#5954)", nSmall, nLarge)
	}
	if got, want := len(large.byPackageMember), 1; got != want {
		t.Errorf("byPackageMember spans %d package directories, want %d (only the changed file's)",
			got, want)
	}

	// Positive form: the index is not merely small, it is POPULATED — a
	// trivially empty index would satisfy the equality above. The one probed
	// package declares filesPerDir types × methods members, all of which must
	// be reachable by (scope, member).
	got := 0
	for _, scope := range large.byPackageMember["pkg0000"] {
		got += len(scope)
	}
	if want := filesPerDir * methods; got != want {
		t.Fatalf("index is inert: byPackageMember[pkg0000] holds %d (scope, member) entries, want %d",
			got, want)
	}
}

// TestMemberIndexes_HeapCost reports the heap the index actually retains, on
// both metrics that matter here. Peak live heap (next_gc/(1+GOGC/100)) and
// HeapAlloc have disagreed by ~2x on this codebase, so both are reported and
// neither is inferred from RSS. This is a MEASUREMENT — it fails only on an
// order-of-magnitude regression, because a tight byte threshold here would be
// a flaky gate, not a guard.
func TestMemberIndexes_HeapCost(t *testing.T) {
	const dirs, filesPerDir, methods = 200, 20, 5
	corpus := memFixture(dirs, filesPerDir, methods)
	fresh := memFixture(1, 1, methods)

	// Two GC cycles: one pass can leave the previous round's garbage unswept,
	// which shows up as a NEGATIVE delta and makes the measurement
	// meaningless. Read GOGC ONCE, outside the measurement: debug.SetGCPercent itself runs
	// a collection, so calling it between ReadMemStats calls perturbs exactly
	// the number being measured.
	gogc := debug.SetGCPercent(-1)
	debug.SetGCPercent(gogc)
	if gogc <= 0 {
		gogc = 100
	}
	read := func() (heapAlloc, liveHeap uint64) {
		var ms runtime.MemStats
		runtime.GC()
		runtime.GC()
		runtime.ReadMemStats(&ms)
		return ms.HeapAlloc, ms.NextGC / uint64(1+gogc/100)
	}

	t.Logf("corpus=%d entities across %d packages (%d files/pkg, %d methods/type)",
		len(corpus), dirs, filesPerDir, methods)

	// A/B: the shipped delta-scoped build versus what the corpus-wide
	// resolver's tier costs — obtained by handing the WHOLE corpus in as the
	// delta, which makes probeDirs span every package.
	//
	// Both indexes are built and held ALIVE across all three reads and are
	// only released after the last one. Measuring them one at a time makes
	// the second reading include the first index becoming garbage, which is
	// how this first produced a nonsensical NEGATIVE cost for the larger
	// index.
	baseAlloc, baseLive := read()
	deltaIdx := buildMemberIndexes(fresh, corpus)
	midAlloc, midLive := read()
	corpusIdx := buildMemberIndexes(corpus, corpus)
	endAlloc, endLive := read()
	// KeepAlive(corpus) is LOAD-BEARING, not defensive. `corpus` has no
	// syntactic use after the call above, so the compiler lets the GC reclaim
	// its 24000 Entity structs during the final read — the index retains only
	// the STRINGS, not the structs. Without this the measurement reported the
	// larger index as costing -1.17 MB: a fixture-shaped artifact, not a
	// result. Recorded because it is the same class of mistake as a mutation
	// that turns out to be a no-op.
	runtime.KeepAlive(corpus)
	runtime.KeepAlive(fresh)

	deltaEntries, corpusEntries := deltaIdx.countEntries(), corpusIdx.countEntries()
	deltaAlloc := int64(midAlloc) - int64(baseAlloc)
	deltaLive := int64(midLive) - int64(baseLive)
	corpusAlloc := int64(endAlloc) - int64(midAlloc)
	corpusLive := int64(endLive) - int64(midLive)
	runtime.KeepAlive(deltaIdx)
	runtime.KeepAlive(corpusIdx)

	t.Logf("%-14s entries=%-7d HeapAlloc=%10d B   live-heap (next_gc/(1+GOGC/100))=%10d B",
		"delta-scoped", deltaEntries, deltaAlloc, deltaLive)
	t.Logf("%-14s entries=%-7d HeapAlloc=%10d B   live-heap (next_gc/(1+GOGC/100))=%10d B",
		"corpus-scoped", corpusEntries, corpusAlloc, corpusLive)

	if corpusEntries <= deltaEntries*10 {
		t.Fatalf("measurement is inert: the corpus-scoped variant retains %d entries and the "+
			"delta-scoped one %d — the A/B is not exercising the probeDirs gate",
			corpusEntries, deltaEntries)
	}
	t.Logf("ratio: corpus-scoped retains %.0fx the entries and %.0fx the HeapAlloc",
		float64(corpusEntries)/float64(deltaEntries),
		float64(corpusAlloc)/float64(max64(deltaAlloc, 1)))

	// Order-of-magnitude guard only — a tight byte threshold here would be a
	// flaky gate, not a guard.
	if deltaEntries > 4000 {
		t.Errorf("index retains %d entries for a one-file delta — corpus-scoped behaviour",
			deltaEntries)
	}
	if deltaAlloc > 4<<20 {
		t.Errorf("index cost %d B of HeapAlloc for a one-file delta, want < 4 MiB", deltaAlloc)
	}
}

func max64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
