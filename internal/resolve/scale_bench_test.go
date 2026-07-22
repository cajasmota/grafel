// Package resolve — M5 scale benchmarks and correctness tests.
//
// Tests verify:
//  1. BuildIndexFromModules produces the same resolved edges as BuildIndex on a
//     small golden fixture (correctness parity).
//  2. BuildIndexFromModules scales sub-quadratically as module count grows
//     (100 → 500 → 1000 modules), measured via Go benchmarks.
//  3. LazyEdgeSet deduplicates stubs correctly and resolves them in bulk.
//
// Run benchmarks:
//
//	go test -run='^$' -bench=. -benchtime=3s ./internal/resolve/
package resolve

import (
	"fmt"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/types"
)

// timeNow is a thin wrapper around time.Now used by nanoTime so tests can
// remain free of direct time-package imports at call sites.
var timeNow = time.Now

// ─────────────────────────────────────────────────────────────────────────────
// Synthetic fixture helpers
// ─────────────────────────────────────────────────────────────────────────────

// syntheticModules generates a realistic cross-module monorepo fixture.
//
//   - numModules modules, each with entitiesPerModule entities.
//   - Each module declares entitiesPerModule/2 SCOPE.Operation (functions) and
//     entitiesPerModule/2 SCOPE.Component (structs).
//   - Each entity carries a cross-module CALLS relationship whose target lives
//     in the *next* module (ring topology) — this gives every module's callers
//     a cross-module stub to resolve.
//
// The function returns (modules map, relationships slice).  The relationships
// slice uses unresolved stubs so the caller can run the resolver and measure
// resolution quality.
func syntheticModules(numModules, entitiesPerModule int) (map[ModuleKey][]types.EntityRecord, []types.RelationshipRecord) {
	modules := make(map[ModuleKey][]types.EntityRecord, numModules)
	var allRels []types.RelationshipRecord

	hexChars := "0123456789abcdef"
	makeID := func(mod, entity int) string {
		// Produce a deterministic 16-char hex ID that encodes both the module
		// and entity index.  Format: 4 hex digits of module + 12 hex digits
		// of entity.  Both values are trimmed/padded to stay in the 16-char
		// budget.
		b := make([]byte, 16)
		for i := 0; i < 4; i++ {
			shift := (3 - i) * 4
			b[i] = hexChars[(mod>>shift)&0xf]
		}
		for i := 0; i < 12; i++ {
			shift := (11 - i) * 4
			b[4+i] = hexChars[(entity>>shift)&0xf]
		}
		return string(b)
	}

	for m := 0; m < numModules; m++ {
		key := ModuleKey(fmt.Sprintf("github.com/acme/svc%04d", m))
		entities := make([]types.EntityRecord, 0, entitiesPerModule)

		half := entitiesPerModule / 2

		for i := 0; i < half; i++ {
			entityIdx := m*entitiesPerModule + i
			name := fmt.Sprintf("Func%04d_%04d", m, i)
			id := makeID(m*2, entityIdx)
			sf := fmt.Sprintf("svc%04d/handler.go", m)
			entities = append(entities, types.EntityRecord{
				ID:         id,
				Kind:       "SCOPE.Operation",
				Name:       name,
				SourceFile: sf,
				Language:   "go",
			})
			// Cross-module CALLS: target a function in the next module.
			nextMod := (m + 1) % numModules
			targetName := fmt.Sprintf("Func%04d_%04d", nextMod, i)
			allRels = append(allRels, types.RelationshipRecord{
				FromID: id,
				ToID:   "SCOPE.Operation:" + targetName,
				Kind:   "CALLS",
				Properties: map[string]string{
					"language": "go",
				},
			})
		}

		for i := 0; i < entitiesPerModule-half; i++ {
			entityIdx := m*entitiesPerModule + half + i
			name := fmt.Sprintf("Struct%04d_%04d", m, i)
			id := makeID(m*2+1, entityIdx)
			sf := fmt.Sprintf("svc%04d/model.go", m)
			entities = append(entities, types.EntityRecord{
				ID:         id,
				Kind:       "SCOPE.Component",
				Name:       name,
				SourceFile: sf,
				Language:   "go",
			})
		}

		modules[key] = entities
	}
	return modules, allRels
}

// flattenModules converts a modules map to a single entity slice, equivalent
// to what a naive BuildIndex call would receive.
func flattenModules(modules map[ModuleKey][]types.EntityRecord) []types.EntityRecord {
	total := 0
	for _, es := range modules {
		total += len(es)
	}
	flat := make([]types.EntityRecord, 0, total)
	for _, es := range modules {
		flat = append(flat, es...)
	}
	return flat
}

// ─────────────────────────────────────────────────────────────────────────────
// Correctness parity test
// ─────────────────────────────────────────────────────────────────────────────

// TestBuildIndexFromModules_Parity verifies that BuildIndexFromModules produces
// exactly the same resolution outcomes as BuildIndex on the same entity set.
// Uses a 10-module fixture with 20 entities per module.
func TestBuildIndexFromModules_Parity(t *testing.T) {
	modules, rels := syntheticModules(10, 20)
	flat := flattenModules(modules)

	idxFlat := BuildIndex(flat)
	idxMod := BuildIndexFromModules(modules, 0)

	// Verify that every relationship resolves identically under both indexes.
	relsCopy := make([]types.RelationshipRecord, len(rels))
	copy(relsCopy, rels)
	relsFlat := make([]types.RelationshipRecord, len(rels))
	copy(relsFlat, rels)

	References(relsCopy, idxMod)
	References(relsFlat, idxFlat)

	mismatches := 0
	for i := range relsCopy {
		if relsCopy[i].ToID != relsFlat[i].ToID {
			t.Errorf("rel[%d]: BuildIndexFromModules resolved %q, BuildIndex resolved %q (stub %q)",
				i, relsCopy[i].ToID, relsFlat[i].ToID, rels[i].ToID)
			mismatches++
			if mismatches >= 10 {
				t.Fatal("too many mismatches — truncating")
			}
		}
	}
}

// TestBuildIndexFromModules_ResolutionRate verifies that the M5 index resolves
// cross-module CALLS stubs at the same rate as the flat BuildIndex on a 100-
// module fixture.  A mismatch here means the pre-sizing or batch logic is
// silently dropping entities.
func TestBuildIndexFromModules_ResolutionRate(t *testing.T) {
	const numModules = 100
	const entPerMod = 40

	modules, rels := syntheticModules(numModules, entPerMod)
	flat := flattenModules(modules)

	idxFlat := BuildIndex(flat)
	idxMod := BuildIndexFromModules(modules, 0)

	// Count resolved stubs under each index.
	countResolved := func(idx Index, rels []types.RelationshipRecord) int {
		cp := make([]types.RelationshipRecord, len(rels))
		copy(cp, rels)
		stats := References(cp, idx)
		return stats.Rewritten
	}

	flatResolved := countResolved(idxFlat, rels)
	modResolved := countResolved(idxMod, rels)

	if flatResolved != modResolved {
		t.Errorf("resolution rate mismatch: BuildIndex resolved %d, BuildIndexFromModules resolved %d (out of %d stubs)",
			flatResolved, modResolved, len(rels))
	}
}

// TestBuildModuleSymbols_Empty verifies that an empty entity slice produces a
// valid, empty ModuleSymbols.
func TestBuildModuleSymbols_Empty(t *testing.T) {
	ms := BuildModuleSymbols("empty", nil)
	if ms == nil {
		t.Fatal("BuildModuleSymbols returned nil")
	}
	if len(ms.entries) != 0 {
		t.Fatalf("expected 0 entries, got %d", len(ms.entries))
	}
}

// TestLazyEdgeSet_ResolveAll verifies the lazy edge materialization path.
func TestLazyEdgeSet_ResolveAll(t *testing.T) {
	entities := []types.EntityRecord{
		{ID: "aaaaaaaaaaaaaaaa", Kind: "SCOPE.Operation", Name: "Hello", SourceFile: "a/a.go"},
		{ID: "bbbbbbbbbbbbbbbb", Kind: "SCOPE.Operation", Name: "World", SourceFile: "b/b.go"},
	}
	idx := BuildIndex(entities)

	les := NewLazyEdgeSet()
	r1 := &types.RelationshipRecord{FromID: "cccccccccccccccc", ToID: "SCOPE.Operation:Hello", Kind: "CALLS"}
	r2 := &types.RelationshipRecord{FromID: "dddddddddddddddd", ToID: "SCOPE.Operation:Hello", Kind: "CALLS"}
	r3 := &types.RelationshipRecord{FromID: "eeeeeeeeeeeeeeee", ToID: "SCOPE.Operation:World", Kind: "CALLS"}
	r4 := &types.RelationshipRecord{FromID: "ffffffffffffffff", ToID: "SCOPE.Operation:Missing", Kind: "CALLS"}

	les.Register("modA", "modB", "CALLS", r1)
	les.Register("modA", "modB", "CALLS", r2) // same stub as r1 — should deduplicate lookup
	les.Register("modA", "modC", "CALLS", r3)
	les.Register("modA", "modD", "CALLS", r4) // unresolvable

	if les.Size() != 3 {
		t.Fatalf("expected 3 unique keys, got %d", les.Size())
	}

	n := les.ResolveAll(idx)
	if n != 3 { // r1, r2, r3 resolved; r4 not
		t.Errorf("expected 3 resolved, got %d", n)
	}
	if r1.ToID != "aaaaaaaaaaaaaaaa" {
		t.Errorf("r1.ToID = %q, want aaaaaaaaaaaaaaaa", r1.ToID)
	}
	if r2.ToID != "aaaaaaaaaaaaaaaa" {
		t.Errorf("r2.ToID = %q, want aaaaaaaaaaaaaaaa", r2.ToID)
	}
	if r3.ToID != "bbbbbbbbbbbbbbbb" {
		t.Errorf("r3.ToID = %q, want bbbbbbbbbbbbbbbb", r3.ToID)
	}
	if r4.ToID != "SCOPE.Operation:Missing" {
		t.Errorf("r4.ToID should be unchanged, got %q", r4.ToID)
	}
}

// TestMergeModuleBatch_SingleBatch verifies that a single-batch merge produces
// the same Index as BuildIndexFromModules for a small fixture.
func TestMergeModuleBatch_SingleBatch(t *testing.T) {
	modules, rels := syntheticModules(4, 10)

	si := &SymbolIndex{}
	for k, es := range modules {
		si.Add(BuildModuleSymbols(k, es))
	}

	idx, nextOff := MergeModuleBatch(si, 0, si.Len())
	if nextOff != si.Len() {
		t.Errorf("expected nextOff=%d, got %d", si.Len(), nextOff)
	}

	// Verify resolution against the flat index.
	flat := flattenModules(modules)
	idxFlat := BuildIndex(flat)

	cp1 := make([]types.RelationshipRecord, len(rels))
	copy(cp1, rels)
	cp2 := make([]types.RelationshipRecord, len(rels))
	copy(cp2, rels)

	References(cp1, idx)
	References(cp2, idxFlat)

	for i := range cp1 {
		if cp1[i].ToID != cp2[i].ToID {
			t.Errorf("rel[%d]: batch resolved %q, flat resolved %q", i, cp1[i].ToID, cp2[i].ToID)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// O(N log N) scaling assertion
// ─────────────────────────────────────────────────────────────────────────────

// TestBuildIndexFromModules_SubQuadratic times BuildIndexFromModules at two
// corpus sizes and asserts the build does not scale quadratically in module
// count.
//
// This test is intentionally NOT a benchmark (b.N loop) so it runs in the
// normal test suite with a deterministic pass/fail gate.
//
// De-flaking rationale (#5636): the original compared 100 vs 500 modules (only
// a 5x corpus) against a 12x bound. For an O(N log N) build the expected ratio
// is ~5 × log(500)/log(100) ≈ 6.5, so the headroom to the 12x bound was barely
// ~1.85x — and ordinary CI jitter (GC pauses, CPU throttling, co-scheduled
// load) routinely ate that headroom with no real complexity regression
// (12.25x observed on a loaded macOS runner). It was the third perf-ratio
// scaling test to flake this way after #5607 and #5628.
//
// We apply the #5607 pattern: a wide 10x corpus gap. The wide gap is what
// makes the bound robust — it places a large window between the expected
// complexity and the next-worse one:
//
//   - O(N log N): 1000/100 × log(1000)/log(100) ≈ 10 × 1.5 = 15x
//   - O(N²):      1000²/100²                     = 100x
//
// Aggregation across samples was originally the MEDIAN of several timing
// runs per size, but that still flaked on noisy CI runners (see
// internal/engine/response_shape_python_perf_test.go's #5751 Windows
// follow-up: a median-of-5 got dragged up by runner contention with no
// algorithmic change). Wall-clock noise (GC pauses, scheduler contention,
// co-scheduled load) only ever ADDS time to a sample, never subtracts it, so
// a handful of contended runs can drag the median upward with no ceiling —
// the median doesn't actually bound the noise contribution. We now take the
// MINIMUM of the per-size samples instead: it is the least-contended
// observation, the best available estimate of true algorithmic cost, and a
// genuine O(N²) code path is still ~100x slower at N=1000 vs N=100 on every
// run including the cleanest one, so switching to min loses none of the
// regression-detection power. samples is bumped 5→8 to give the minimum more
// draws at a clean run while staying fast.
//
// A 40x bound sits ~2.7x above the n-log-n expectation (generous slack for
// fixed overhead and CI noise) yet ~2.5x below the quadratic signal, so a
// genuine O(N²) reintroduction still fails loudly (~100x) while CI jitter
// never does. With min-based timing this margin is if anything more
// comfortable than before, since min filters out upward noise directly
// rather than relying on the bound alone to absorb it.
func TestBuildIndexFromModules_SubQuadratic(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping scaling test in short mode")
	}

	const (
		entPerMod = 50
		smallN    = 100
		largeN    = 1000 // 10x corpus → n-log-n ≈ 15x time, quadratic ≈ 100x
		samples   = 8    // min of N runs damps CI outliers without masking true cost
		bound     = 40.0 // ~2.7x over n-log-n (15x), ~2.5x under quadratic (100x)
	)

	// min times BuildIndexFromModules `samples` times at the given module
	// count and returns the minimum, which is far more stable than the
	// median under CI noise: a GC pause or co-scheduled spike can only ever
	// inflate a sample, so the smallest sample is the one least corrupted by
	// noise and the best estimate of true algorithmic cost.
	minRun := func(numMods int) int64 {
		modules, _ := syntheticModules(numMods, entPerMod)
		best := int64(-1)
		for i := 0; i < samples; i++ {
			t0 := nanoTime()
			_ = BuildIndexFromModules(modules, 0)
			d := nanoTime() - t0
			if best < 0 || d < best {
				best = d
			}
		}
		return best
	}

	tSmall := minRun(smallN)
	tLarge := minRun(largeN)

	// +1µs floor on the denominator guards against a 0ns small measurement.
	ratio := float64(tLarge) / float64(tSmall+1000)
	t.Logf("%d-mod min: %dns  %d-mod min: %dns  ratio: %.2fx (n-log-n≈15.0, quadratic≈100.0)",
		smallN, tSmall, largeN, tLarge, ratio)

	if ratio > bound {
		t.Errorf("scaling ratio %.2fx exceeds %.0fx threshold (%dx corpus) — possible O(N²) regression",
			ratio, bound, largeN/smallN)
	}
}

// nanoTime returns a monotonic nanosecond timestamp.  Using
// time.Now().UnixNano() is accurate enough for the loose scaling assertion in
// TestBuildIndexFromModules_SubQuadratic since we compare ratios, not wall-
// clock times.
func nanoTime() int64 {
	return timeNow().UnixNano()
}

// ─────────────────────────────────────────────────────────────────────────────
// Benchmarks
// ─────────────────────────────────────────────────────────────────────────────

// BenchmarkBuildIndex_100mod measures the baseline (flat BuildIndex) at 100 modules.
func BenchmarkBuildIndex_100mod(b *testing.B) {
	modules, _ := syntheticModules(100, 50)
	flat := flattenModules(modules)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = BuildIndex(flat)
	}
}

// BenchmarkBuildIndexFromModules_100mod measures M5 at 100 modules.
func BenchmarkBuildIndexFromModules_100mod(b *testing.B) {
	modules, _ := syntheticModules(100, 50)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = BuildIndexFromModules(modules, 0)
	}
}

// BenchmarkBuildIndex_500mod measures the baseline at 500 modules.
func BenchmarkBuildIndex_500mod(b *testing.B) {
	modules, _ := syntheticModules(500, 50)
	flat := flattenModules(modules)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = BuildIndex(flat)
	}
}

// BenchmarkBuildIndexFromModules_500mod measures M5 at 500 modules.
func BenchmarkBuildIndexFromModules_500mod(b *testing.B) {
	modules, _ := syntheticModules(500, 50)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = BuildIndexFromModules(modules, 0)
	}
}

// BenchmarkBuildIndex_1000mod measures the baseline at 1000 modules.
func BenchmarkBuildIndex_1000mod(b *testing.B) {
	modules, _ := syntheticModules(1000, 50)
	flat := flattenModules(modules)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = BuildIndex(flat)
	}
}

// BenchmarkBuildIndexFromModules_1000mod measures M5 at 1000 modules.
func BenchmarkBuildIndexFromModules_1000mod(b *testing.B) {
	modules, _ := syntheticModules(1000, 50)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = BuildIndexFromModules(modules, 0)
	}
}

// BenchmarkReferences_CrossModule_100mod measures the full pipeline
// (build index + resolve) for 100 modules with cross-module CALLS stubs.
func BenchmarkReferences_CrossModule_100mod(b *testing.B) {
	modules, rels := syntheticModules(100, 50)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		idx := BuildIndexFromModules(modules, 0)
		cp := make([]types.RelationshipRecord, len(rels))
		copy(cp, rels)
		_ = References(cp, idx)
	}
}

// BenchmarkReferences_CrossModule_500mod measures the full pipeline at 500 modules.
func BenchmarkReferences_CrossModule_500mod(b *testing.B) {
	modules, rels := syntheticModules(500, 50)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		idx := BuildIndexFromModules(modules, 0)
		cp := make([]types.RelationshipRecord, len(rels))
		copy(cp, rels)
		_ = References(cp, idx)
	}
}

// BenchmarkLazyEdgeSet_ResolveAll_10k benchmarks lazy resolution of 10 k
// unique stubs (simulating a 200-module fixture with 50 edges/module).
func BenchmarkLazyEdgeSet_ResolveAll_10k(b *testing.B) {
	modules, _ := syntheticModules(200, 50)
	flat := flattenModules(modules)
	idx := BuildIndex(flat)

	// Build a LazyEdgeSet with 10 k unique stubs.
	les := NewLazyEdgeSet()
	for m := 0; m < 200; m++ {
		nextMod := (m + 1) % 200
		for i := 0; i < 50; i++ {
			stub := fmt.Sprintf("SCOPE.Operation:Func%04d_%04d", nextMod, i)
			r := &types.RelationshipRecord{
				FromID: fmt.Sprintf("%016x", m*50+i),
				ToID:   stub,
				Kind:   "CALLS",
			}
			les.Register(ModuleKey(fmt.Sprintf("m%d", m)), ModuleKey(fmt.Sprintf("m%d", nextMod)), "CALLS", r)
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Reset ToIDs to stubs each iteration.
		for k := range les.entries {
			for _, r := range les.entries[k] {
				r.ToID = k.Stub
			}
		}
		_ = les.ResolveAll(idx)
	}
}
