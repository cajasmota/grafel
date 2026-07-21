// bykind_labelindex_5870_test.go — memory epic #5850 / mmap-flip #5870:
// by-Kind LabelIndex + forEachEntityOfKinds selective materialization.
//
// These tests pin the three load-bearing properties of the by-Kind flip:
//
//  1. byKind BUILD PARITY — the Reader-built byKind map is byte-identical
//     (reflect.DeepEqual) to the Document-built one, exactly like byID/byLabel/
//     byQName, and each per-kind list is the exact set of vector indices with
//     that raw Kind, in ascending index order.
//
//  2. forEachEntityOfKinds OUTPUT PARITY (set + ORDER) — for every predicate the
//     converted endpoint/flow/dashboard scanners use, forEachEntityOfKinds yields
//     EXACTLY the entities a forEachEntity full-scan + in-loop Kind filter would,
//     in the SAME (ascending vector-index) order, on BOTH the flag-OFF and flag-ON
//     read paths. Dropping the merge/sort in indicesForKinds makes the order
//     diverge from the vector order (map-iteration order is nondeterministic) and
//     fails the ordered-index assertion + the parity DeepEqual.
//
//  3. SELECTIVE MATERIALIZATION (flag-ON) — a converted scan materializes ONLY the
//     predicate-matching entities, NOT the whole set. Routing forEachEntityOfKinds
//     through the whole-scan forEach-all path (the mutation) makes the materialize
//     counter equal EntityCount and fails the selectivity assertion.
package mcp

import (
	"fmt"
	"path/filepath"
	"reflect"
	"sort"
	"sync/atomic"
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/graph/fbreader"
	"github.com/cajasmota/grafel/internal/graph/fbwriter"
)

// mixedKindDoc fabricates a Document whose entities cycle through every raw Kind
// the converted scanners branch on — endpoint definition/call/legacy, topic,
// process, pattern, plus plain code kinds — INTERLEAVED in vector order so a
// multi-kind predicate's index union must be MERGED (not concatenated) to stay
// ascending. This is what makes the order-preservation property observable.
func mixedKindDoc(n int) *graph.Document {
	kinds := []string{
		"http_endpoint_definition", // isDefinitionKind, isHTTPEndpointKind
		"http_endpoint_call",       // isCallKind, isHTTPEndpointKind
		"http_endpoint",            // legacy → isDefinitionKind, isHTTPEndpointKind
		"SCOPE.topic",              // isTopicKind
		"SCOPE.Process",            // processEntityKind
		"SCOPE.Pattern",            // pattern
		"FUNCTION",                 // none of the above
		"class",                    // none of the above
	}
	ents := make([]graph.Entity, n)
	for i := 0; i < n; i++ {
		k := kinds[i%len(kinds)]
		ents[i] = graph.Entity{
			ID:            fmt.Sprintf("e%04d", i),
			Name:          fmt.Sprintf("Name%d", i),
			QualifiedName: fmt.Sprintf("pkg.Name%d", i),
			Kind:          k,
			SourceFile:    fmt.Sprintf("src/f%d.go", i),
			Language:      "go",
			StartLine:     i,
			EndLine:       i + 3,
		}
	}
	return &graph.Document{Entities: ents}
}

// loadMixedKindFixture writes mixedKindDoc(n) to a temp graph.fb and returns the
// loader-materialized Document plus an open Reader over the same file.
func loadMixedKindFixture(t *testing.T, n int) (*graph.Document, *fbreader.Reader) {
	t.Helper()
	dir := t.TempDir()
	fbPath := filepath.Join(dir, "graph.fb")
	if err := fbwriter.WriteAtomic(fbPath, mixedKindDoc(n)); err != nil {
		t.Fatalf("WriteAtomic: %v", err)
	}
	doc, err := graph.LoadGraphFromDir(dir)
	if err != nil {
		t.Fatalf("LoadGraphFromDir: %v", err)
	}
	r, err := fbreader.Open(fbPath)
	if err != nil {
		t.Fatalf("fbreader.Open: %v", err)
	}
	t.Cleanup(func() { r.Close() })
	return doc, r
}

// mixedKindRepo builds a Reader-backed LoadedRepo with the readerMu-wired
// Reader-sourced LabelIndex — the shape the flag-ON read path exercises.
func mixedKindRepo(t *testing.T, n int) (*LoadedRepo, *graph.Document, *fbreader.Reader) {
	t.Helper()
	doc, r := loadMixedKindFixture(t, n)
	lr := &LoadedRepo{Repo: "r", Doc: doc, Reader: r}
	li := BuildLabelIndexFromReader(r, doc)
	li.readerMu = &lr.readerMu
	lr.LabelIndex = li
	return lr, doc, r
}

// convertedScannerPreds are the exact Kind predicates the converted endpoint /
// dashboard scanners pass to forEachEntityOfKinds. Running the parity + order
// tests over THESE is the primitive-level correctness oracle for every converted
// call site (each site's only change is forEachEntity+`if !pred{skip}` →
// forEachEntityOfKinds(pred, ...)).
func convertedScannerPreds() []struct {
	name string
	pred func(string) bool
} {
	return []struct {
		name string
		pred func(string) bool
	}{
		{"isDefinitionKind", isDefinitionKind},
		{"callOrDefinition", func(k string) bool { return isCallKind(k) || isDefinitionKind(k) }},
		{"isHTTPEndpointKind", isHTTPEndpointKind},
		{"isTopicKind", isTopicKind},
		{"process", func(k string) bool { return k == processEntityKind }},
		{"pattern", func(k string) bool { return k == "SCOPE.Pattern" || k == "Pattern" }},
		{"noMatch", func(k string) bool { return k == "does-not-exist" }},
		{"all", func(k string) bool { return true }},
	}
}

// TestLabelIndexByKind_ReaderDocParity_5870 pins byKind build parity (property 1).
func TestLabelIndexByKind_ReaderDocParity_5870(t *testing.T) {
	t.Parallel()
	doc, r := loadMixedKindFixture(t, 200)
	wantIdx := BuildLabelIndex(doc)
	gotIdx := BuildLabelIndexFromReader(r, doc)

	if !reflect.DeepEqual(gotIdx.byKind, wantIdx.byKind) {
		t.Fatalf("byKind map Reader-built != Document-built\n got=%v\nwant=%v", gotIdx.byKind, wantIdx.byKind)
	}

	// Every per-kind list is EXACTLY the vector indices with that raw kind, in
	// ascending order; and every entity appears exactly once across byKind.
	total := 0
	for kind, idxs := range gotIdx.byKind {
		var exp []int32
		for i := range doc.Entities {
			if doc.Entities[i].Kind == kind {
				exp = append(exp, int32(i))
			}
		}
		if !reflect.DeepEqual(idxs, exp) {
			t.Errorf("byKind[%q] = %v, want %v (exact vector indices, ascending)", kind, idxs, exp)
		}
		for j := 1; j < len(idxs); j++ {
			if idxs[j] <= idxs[j-1] {
				t.Errorf("byKind[%q] not strictly ascending at %d: %v", kind, j, idxs)
			}
		}
		total += len(idxs)
	}
	if total != len(doc.Entities) {
		t.Fatalf("byKind covers %d indices, want %d (one kind per entity, no gaps/dupes)", total, len(doc.Entities))
	}
}

// collectIDsForEach returns the entity IDs a forEachEntity full-scan yields for
// the entities matching pred, in visitation (vector) order — the pre-conversion
// forEach+filter oracle.
func collectIDsForEach(lr *LoadedRepo, pred func(string) bool) []string {
	var ids []string
	lr.forEachEntity(func(e *graph.Entity) bool {
		if pred(e.Kind) {
			ids = append(ids, e.ID)
		}
		return true
	})
	return ids
}

// collectIDsForKinds returns the entity IDs forEachEntityOfKinds yields, in
// visitation order.
func collectIDsForKinds(lr *LoadedRepo, pred func(string) bool) []string {
	var ids []string
	lr.forEachEntityOfKinds(pred, func(e *graph.Entity) bool {
		ids = append(ids, e.ID)
		return true
	})
	return ids
}

// TestForEachEntityOfKinds_Parity_FlagOff_5870 pins output parity (set + ORDER)
// vs forEachEntity+filter on the DEFAULT flag-off path, for every converted-
// scanner predicate.
func TestForEachEntityOfKinds_Parity_FlagOff_5870(t *testing.T) {
	withServeFromMMap(t, false)
	lr, _, _ := mixedKindRepo(t, 200)

	for _, tc := range convertedScannerPreds() {
		want := collectIDsForEach(lr, tc.pred)
		got := collectIDsForKinds(lr, tc.pred)
		if !reflect.DeepEqual(got, want) {
			t.Errorf("[flag-off %s] forEachEntityOfKinds != forEachEntity+filter\n got=%v\nwant=%v", tc.name, got, want)
		}
	}
}

// TestForEachEntityOfKinds_Parity_FlagOn_5870 pins the SAME output parity on the
// flag-ON (resident Reader) read path — where forEachEntityOfKinds materializes
// through the readerMu-guarded mmap path.
func TestForEachEntityOfKinds_Parity_FlagOn_5870(t *testing.T) {
	withServeFromMMap(t, true)
	lr, _, _ := mixedKindRepo(t, 200)

	for _, tc := range convertedScannerPreds() {
		want := collectIDsForEach(lr, tc.pred)
		got := collectIDsForKinds(lr, tc.pred)
		if !reflect.DeepEqual(got, want) {
			t.Errorf("[flag-on %s] forEachEntityOfKinds != forEachEntity+filter\n got=%v\nwant=%v", tc.name, got, want)
		}
	}
}

// TestForEachEntityOfKinds_AscendingOrder_5870 is the ORDER-preservation
// mutation guard. indicesForKinds must return the union of the matching kinds'
// index lists in ASCENDING order. The multi-kind predicates (callOrDefinition,
// isHTTPEndpointKind) span interleaved kinds, so a build that merely CONCATENATES
// the per-kind lists (dropping the sort in indicesForKinds) produces a
// non-ascending, map-iteration-order-dependent sequence — caught here.
func TestForEachEntityOfKinds_AscendingOrder_5870(t *testing.T) {
	t.Parallel()
	doc, r := loadMixedKindFixture(t, 200)
	li := BuildLabelIndexFromReader(r, doc)

	for _, tc := range convertedScannerPreds() {
		idxs := li.indicesForKinds(tc.pred)
		// Strictly ascending.
		for j := 1; j < len(idxs); j++ {
			if idxs[j] <= idxs[j-1] {
				t.Fatalf("[%s] indicesForKinds not strictly ascending at %d: %v (merge/sort dropped?)", tc.name, j, idxs)
			}
		}
		// Exactly the vector indices whose kind matches pred.
		var exp []int32
		for i := range doc.Entities {
			if tc.pred(doc.Entities[i].Kind) {
				exp = append(exp, int32(i))
			}
		}
		if !reflect.DeepEqual(idxs, exp) {
			t.Fatalf("[%s] indicesForKinds = %v, want %v (ascending union of matching kinds)", tc.name, idxs, exp)
		}
		// Independent sanity: exp is itself already sorted (vector order).
		if !sort.SliceIsSorted(idxs, func(a, b int) bool { return idxs[a] < idxs[b] }) {
			t.Fatalf("[%s] result not sorted", tc.name)
		}
	}
}

// TestForEachEntityOfKinds_SelectiveMaterialization_5870 pins property 3: flag-ON,
// a forEachEntityOfKinds scan materializes ONLY the predicate-matching entities,
// NOT the whole set. Mutation: routing forEachEntityOfKinds through the whole-scan
// forEach-all path makes the counter equal EntityCount and this FAILs.
func TestForEachEntityOfKinds_SelectiveMaterialization_5870(t *testing.T) {
	withServeFromMMap(t, true)
	lr, doc, r := mixedKindRepo(t, 240)

	// Expected number of definition-kind (endpoint) entities.
	wantDef := 0
	for i := range doc.Entities {
		if isDefinitionKind(doc.Entities[i].Kind) {
			wantDef++
		}
	}
	if wantDef == 0 || wantDef == len(doc.Entities) {
		t.Fatalf("fixture sanity: definition-kind count=%d of %d (must be a proper subset)", wantDef, len(doc.Entities))
	}

	var count atomic.Int64
	atMaterializeHook = func() { count.Add(1) }
	t.Cleanup(func() { atMaterializeHook = nil })

	// Converted endpoint scan: materialize count == definition-kind count.
	count.Store(0)
	visited := 0
	lr.forEachEntityOfKinds(isDefinitionKind, func(e *graph.Entity) bool {
		visited++
		return true
	})
	if visited != wantDef {
		t.Fatalf("forEachEntityOfKinds visited %d, want %d", visited, wantDef)
	}
	if got := count.Load(); got != int64(wantDef) {
		t.Fatalf("forEachEntityOfKinds materialized %d entities, want exactly %d (matching-kind count) — NOT EntityCount %d (whole-scan path?)",
			got, wantDef, r.EntityCount())
	}

	// Contrast: a forEach-all scan materializes EVERY row — proves the counter is
	// live and that the by-Kind path is genuinely narrower.
	count.Store(0)
	lr.forEachEntity(func(e *graph.Entity) bool { return true })
	if got := count.Load(); got != int64(r.EntityCount()) {
		t.Fatalf("forEachEntity materialized %d entities, want all %d", got, r.EntityCount())
	}
}

// TestForEachEntityOfKinds_NoByKindFallback_5870 pins that a repo whose
// LabelIndex has no byKind index (directly-constructed / JSON-only) still yields
// output-identical results via the filtered full-scan fallback.
func TestForEachEntityOfKinds_NoByKindFallback_5870(t *testing.T) {
	withServeFromMMap(t, false)
	doc := mixedKindDoc(120)
	lr := &LoadedRepo{Repo: "r", Doc: doc} // no LabelIndex → fallback path

	for _, tc := range convertedScannerPreds() {
		want := collectIDsForEach(lr, tc.pred)
		got := collectIDsForKinds(lr, tc.pred)
		if !reflect.DeepEqual(got, want) {
			t.Errorf("[fallback %s] forEachEntityOfKinds != forEachEntity+filter\n got=%v\nwant=%v", tc.name, got, want)
		}
	}
}
