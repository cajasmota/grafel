// mmap_readermu_sigbus_test.go — ADR-0027 SIGBUS-safety (memory epic #5850,
// Option B) for the GRAFEL_SERVE_FROM_MMAP flag-ON read path.
//
// When the flag is ON the 4 read choke points (LabelIndex.at /
// buildAdjacencyFromReader / buildCallsAdjacencyFromReader /
// buildStepAdjacencyFromReader) dereference the mmap fbreader.Reader. A
// concurrent reload's synchronous munmap can race an in-flight read → read-after-
// munmap SIGBUS. The load-bearing mechanism guarded here is:
//
//   - LoadedRepo.readerMu (STRICTLY INNERMOST): every choke-point mmap deref and
//     every reload/evict munmap holds it, so a deref and the munmap of that same
//     mapping can never interleave.
//   - MapHandle.readRetired: set true UNDER readerMu immediately BEFORE the
//     munmap. A choke point holding a mapping retired out from under it (a stale
//     captured *LabelIndex) sees readRetired==true and takes a safe non-mmap path
//     instead of dereferencing the freed region — the adjacency builders fall back
//     to the Doc, while at() returns nil (post-#5870 PR7bc deretain flip: a
//     reader-present repo's Doc is skeletonized, so the retired generation has no
//     valid Doc row to hand back, and its index is stale against the successor
//     generation anyway).
//
// These tests run flag-ON with -race and must complete with NO SIGBUS/SIGSEGV/
// panic and correct data.
package mcp

import (
	"path/filepath"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/graph/fbreader"
	"github.com/cajasmota/grafel/internal/graph/fbwriter"
)

// sigbusFixtureDoc builds a small graph with n entities and CALLS +
// STEP_IN_PROCESS edges so all three adjacency builders (and at()) iterate real
// mmap-backed rows.
func sigbusFixtureDoc(n int) *graph.Document {
	doc := &graph.Document{Version: 1, Repo: "r"}
	for i := 0; i < n; i++ {
		id := "e" + itoa(i)
		doc.Entities = append(doc.Entities, graph.Entity{
			ID: id, Name: id, QualifiedName: "r." + id,
			Kind: "function", SourceFile: id + ".go", Language: "go",
			StartLine: 1 + i,
		})
	}
	for i := 0; i+1 < n; i++ {
		from, to := "e"+itoa(i), "e"+itoa(i+1)
		doc.Relationships = append(doc.Relationships,
			graph.Relationship{FromID: from, ToID: to, Kind: "CALLS"},
			graph.Relationship{FromID: from, ToID: to, Kind: "STEP_IN_PROCESS"},
		)
	}
	return doc
}

// wireReaderLabelIndex builds a fully-wired reader-sourced LabelIndex for lr's
// readerMu + the given handle, exactly as reloadLocked does in production.
func wireReaderLabelIndex(lr *LoadedRepo, rdr fbreader.GraphView, h *MapHandle, doc *graph.Document) *LabelIndex {
	li := BuildLabelIndexFromReader(rdr, doc)
	li.readerMu = &lr.readerMu
	li.handle = h
	return li
}

// TestServeFromMMapOn_ConcurrentReloadRaceNoSIGBUS is THE SIGBUS test: flag-ON,
// many goroutines hammer all 4 choke points while a reloader repeatedly swaps the
// reader (open a fresh real mmap, publish the successor, retire+munmap the
// predecessor). Run with -race it must finish without a read-after-munmap
// SIGBUS/SIGSEGV/panic. Without readerMu, a choke-point deref races the munmap.
func TestServeFromMMapOn_ConcurrentReloadRaceNoSIGBUS(t *testing.T) {
	forceServeFromMMap(t, true)

	doc := sigbusFixtureDoc(24)
	fbPath := filepath.Join(t.TempDir(), "graph.fb")
	if err := fbwriter.WriteAtomic(fbPath, doc); err != nil {
		t.Fatalf("write graph.fb: %v", err)
	}
	open := func() *fbreader.Reader {
		r, err := fbreader.Open(fbPath)
		if err != nil {
			t.Fatalf("open reader: %v", err)
		}
		return r
	}

	s := NewState(&Registry{Groups: map[string]RegistryGroup{"g": {Repos: map[string]RegistryRepo{}}}})
	lr := &LoadedRepo{Repo: "r", Doc: doc}
	s.groups["g"] = &LoadedGroup{Name: "g", Repos: map[string]*LoadedRepo{"r": lr}}

	// Install the first generation.
	s.mu.Lock()
	h0 := newMapHandle(open())
	lr.LabelIndex = wireReaderLabelIndex(lr, h0.reader, h0, doc)
	lr.publishHandle(h0)
	s.mu.Unlock()

	stop := make(chan struct{})
	var faults atomic.Int64

	// Reloader: swap the reader in a tight loop, always under s.mu, retiring the
	// predecessor (real munmap) via publishHandle.
	var wgR sync.WaitGroup
	wgR.Add(1)
	go func() {
		defer wgR.Done()
		for i := 0; i < 400; i++ {
			select {
			case <-stop:
				return
			default:
			}
			nh := newMapHandle(open())
			li := wireReaderLabelIndex(lr, nh.reader, nh, doc)
			s.mu.Lock()
			lr.resetIndexes() // re-arm the adjacency Once so the next getter rebuilds from the new reader
			lr.LabelIndex = li
			lr.publishHandle(nh) // retire+munmap predecessor under readerMu
			s.mu.Unlock()
		}
	}()

	// Readers: hammer all 4 choke points. LabelIndex.at via a pointer snapshotted
	// under s.mu (race-free vs the reloader's write); the 3 adjacency builders read
	// lr.Reader/lr.handle under readerMu inside their getters.
	//
	// The snapshotted li may be retired by the reloader BETWEEN the snapshot and the
	// ByID call (publishHandle retires the predecessor generation). Post-#5870
	// PR7bc, at() on a retired generation returns nil (a graceful miss) rather than
	// falling back to the Doc, so ByID may legitimately return nil here. The
	// load-bearing invariant this test pins is "no read-after-munmap SIGBUS (run
	// -race) and never WRONG data" — a non-nil entity must be the correct one;
	// absence for a mid-flight-retired generation is expected, not a fault.
	var wgB sync.WaitGroup
	for g := 0; g < 6; g++ {
		wgB.Add(1)
		go func() {
			defer wgB.Done()
			for j := 0; j < 800; j++ {
				s.mu.Lock()
				li := lr.LabelIndex
				s.mu.Unlock()
				if e := li.ByID("e5"); e != nil && e.ID != "e5" {
					faults.Add(1)
				}
				_ = lr.getAdjacency()
				_ = lr.getCallsAdj()
				_ = lr.getStepAdj()
			}
		}()
	}

	wgB.Wait()
	close(stop)
	wgR.Wait()

	s.mu.Lock()
	lr.retireHandle() // munmap the final generation
	s.mu.Unlock()

	if f := faults.Load(); f != 0 {
		t.Fatalf("choke-point read faults (wrong entity from a live generation): %d", f)
	}
}

// TestServeFromMMapOn_StaleCapturedLabelIndexAtReturnsNil is the load-bearing
// test for the readRetired flag AFTER the #5870 PR7bc deretain flip. It captures
// a *LabelIndex BEFORE a reload, the reload retires+munmaps that generation's
// mapping, and then a lookup on the STALE captured index must return nil — NOT a
// SIGBUS on the freed region, and NOT an out-of-range panic when the Doc has been
// skeletonized (Entities dropped to length 0 by reloadLocked). Before the flip
// at() fell back to the Doc row; now the retired generation's Doc is empty and
// its index is stale against the successor, so nil (skip-the-row) is the contract.
func TestServeFromMMapOn_StaleCapturedLabelIndexAtReturnsNil(t *testing.T) {
	forceServeFromMMap(t, true)

	doc := sigbusFixtureDoc(8)
	fbPath := filepath.Join(t.TempDir(), "graph.fb")
	if err := fbwriter.WriteAtomic(fbPath, doc); err != nil {
		t.Fatalf("write graph.fb: %v", err)
	}
	open := func() *fbreader.Reader {
		r, err := fbreader.Open(fbPath)
		if err != nil {
			t.Fatalf("open reader: %v", err)
		}
		return r
	}

	s := NewState(&Registry{Groups: map[string]RegistryGroup{"g": {Repos: map[string]RegistryRepo{}}}})
	lr := &LoadedRepo{Repo: "r", Doc: doc}
	s.groups["g"] = &LoadedGroup{Name: "g", Repos: map[string]*LoadedRepo{"r": lr}}

	s.mu.Lock()
	h0 := newMapHandle(open())
	stale := wireReaderLabelIndex(lr, h0.reader, h0, doc)
	lr.LabelIndex = stale
	lr.publishHandle(h0)
	s.mu.Unlock()

	// Sanity: before the reload the stale index reads through the mmap correctly.
	if e := stale.ByID("e3"); e == nil || e.ID != "e3" || e.QualifiedName != "r.e3" {
		t.Fatalf("pre-reload stale.ByID(e3) = %#v", e)
	}

	// Reload: publish a fresh generation and retire+munmap the mapping the stale
	// index still holds.
	s.mu.Lock()
	nh := newMapHandle(open())
	lr.LabelIndex = wireReaderLabelIndex(lr, nh.reader, nh, doc)
	lr.publishHandle(nh) // retires h0 → munmap the region `stale` points at
	s.mu.Unlock()

	if h0.readRetired != true {
		t.Fatalf("predecessor handle not marked readRetired after reload")
	}

	// The retired-generation Doc is now skeletonized in production; emulate that
	// here so at()'s retired branch is exercised against an EMPTY Entities slice —
	// the branch must NOT index l.doc.Entities (out-of-range panic) and must NOT
	// fall back to Doc rows. It returns nil.
	stale.doc.Entities = nil

	for _, id := range []string{"e0", "e3", "e7"} {
		if got := stale.ByID(id); got != nil {
			t.Fatalf("stale.ByID(%s) after retire+skeleton = %#v, want nil", id, got)
		}
	}
	// Direct at() with an in-range-for-the-old-generation index must also return
	// nil (no out-of-range on the emptied Doc, no freed-region deref).
	if got := stale.at(5); got != nil {
		t.Fatalf("stale.at(5) after retire+skeleton = %#v, want nil", got)
	}

	s.mu.Lock()
	lr.retireHandle()
	s.mu.Unlock()
}

// TestServeFromMMapOn_BM25ResolverRetiredRowsSkipped proves the BM25 search
// result assembly tolerates at() returning nil for a retired generation: with the
// resolver wired to a STALE (retired-mapping, skeletonized-Doc) LabelIndex, every
// row resolves to nil and Search drops them all — no panic, no nil-deref — while a
// LIVE resolver over the same index shape returns the rows. This is the caller
// side of COMMIT 1 (scoring.go Search: `if ent == nil { continue }`).
func TestServeFromMMapOn_BM25ResolverRetiredRowsSkipped(t *testing.T) {
	forceServeFromMMap(t, true)

	doc := sigbusFixtureDoc(8)
	fbPath := filepath.Join(t.TempDir(), "graph.fb")
	if err := fbwriter.WriteAtomic(fbPath, doc); err != nil {
		t.Fatalf("write graph.fb: %v", err)
	}
	open := func() *fbreader.Reader {
		r, err := fbreader.Open(fbPath)
		if err != nil {
			t.Fatalf("open reader: %v", err)
		}
		return r
	}

	s := NewState(&Registry{Groups: map[string]RegistryGroup{"g": {Repos: map[string]RegistryRepo{}}}})
	lr := &LoadedRepo{Repo: "r", Doc: doc}
	s.groups["g"] = &LoadedGroup{Name: "g", Repos: map[string]*LoadedRepo{"r": lr}}

	s.mu.Lock()
	h0 := newMapHandle(open())
	stale := wireReaderLabelIndex(lr, h0.reader, h0, doc)
	lr.LabelIndex = stale
	lr.publishHandle(h0)

	// A BM25 index over the SAME mmap, resolver captured against the stale index
	// (exactly as getBM25 wires idx.resolve = func(vi){ return li.at(vi) }).
	bm := BuildBM25FromReader(h0.reader)
	bm.resolve = func(vi int32) *graph.Entity { return stale.at(vi) }
	s.mu.Unlock()

	// Live: the resolver returns rows, so a query matching every entity name (all
	// entities are "eN", sharing no token; query each id) resolves.
	if hits := bm.Search("e3", 10); len(hits) == 0 {
		t.Fatalf("pre-retire BM25 Search(e3) returned no hits; resolver should be live")
	}

	// Retire the generation the resolver's captured index holds, and skeletonize
	// its Doc — every at() now returns nil.
	s.mu.Lock()
	nh := newMapHandle(open())
	lr.LabelIndex = wireReaderLabelIndex(lr, nh.reader, nh, doc)
	lr.publishHandle(nh)
	s.mu.Unlock()
	stale.doc.Entities = nil

	// Search must not panic and must skip every retired-resolved row → empty slice.
	hits := bm.Search("e3", 10)
	if len(hits) != 0 {
		t.Fatalf("post-retire BM25 Search(e3) = %d hits, want 0 (all rows retired → nil → skipped)", len(hits))
	}

	s.mu.Lock()
	lr.retireHandle()
	s.mu.Unlock()
}

// TestServeFromMMapOff_ReaderMuWiredStillReadsDoc guards that wiring readerMu +
// a live handle does NOT change flag-OFF behavior: with the flag OFF every read
// path is Document-sourced and byte-identical, even though the readerMu machinery
// is present. The graph.fb backing the reader DIVERGES from the Doc, so any
// Reader sourcing would be observable.
func TestServeFromMMapOff_ReaderMuWiredStillReadsDoc(t *testing.T) {
	forceServeFromMMap(t, false)

	doc := &graph.Document{Version: 1, Repo: "r"}
	doc.Entities = []graph.Entity{
		{ID: "A", Name: "A", QualifiedName: "r.A", Kind: "function", SourceFile: "a.go", Language: "go", StartLine: 3},
		{ID: "B", Name: "B", QualifiedName: "r.B", Kind: "function", SourceFile: "b.go", Language: "go", StartLine: 4},
	}
	doc.Relationships = []graph.Relationship{{FromID: "A", ToID: "B", Kind: "CALLS"}}

	// Divergent graph.fb: different ids, no relationships.
	rdrDoc := &graph.Document{Version: 1, Repo: "r"}
	rdrDoc.Entities = []graph.Entity{
		{ID: "X", Name: "X", QualifiedName: "r.X", Kind: "function", SourceFile: "x.go", Language: "go"},
		{ID: "Y", Name: "Y", QualifiedName: "r.Y", Kind: "function", SourceFile: "y.go", Language: "go"},
	}
	fbPath := filepath.Join(t.TempDir(), "graph.fb")
	if err := fbwriter.WriteAtomic(fbPath, rdrDoc); err != nil {
		t.Fatalf("write graph.fb: %v", err)
	}
	rdr, err := fbreader.Open(fbPath)
	if err != nil {
		t.Fatalf("open reader: %v", err)
	}
	t.Cleanup(func() { _ = rdr.Close() })

	lr := &LoadedRepo{Repo: "r", Doc: doc}
	h := newMapHandle(rdr)
	lr.LabelIndex = wireReaderLabelIndex(lr, rdr, h, doc)
	lr.publishHandle(h)

	// at(idx): must surface the Document rows (A/B), not the Reader's divergent
	// X/Y — even though a handle + readerMu are wired, flag-off stays Doc-sourced.
	// (byID is reader-keyed, so index the rows directly like the gating test.)
	if a := lr.LabelIndex.at(0); a == nil || a.ID != "A" {
		t.Fatalf("flag-off wired at(0) = %#v; must be Document entity A (Reader-sourced?)", a)
	}
	if b := lr.LabelIndex.at(1); b == nil || b.ID != "B" {
		t.Fatalf("flag-off wired at(1) = %#v; must be Document entity B (Reader-sourced?)", b)
	}
	// adjacency must equal the Document build and differ from the Reader build
	// (the two diverge), proving Doc-sourcing.
	got := lr.getAdjacency()
	if !reflect.DeepEqual(got, buildAdjacency(doc, "r")) {
		t.Fatalf("flag-off wired getAdjacency != Document build (Reader-sourced?)")
	}
	if reflect.DeepEqual(got, buildAdjacencyFromReader(rdr, "r")) {
		t.Fatalf("flag-off wired getAdjacency == Reader build — it dereferenced the mmap")
	}

	s := NewState(&Registry{Groups: map[string]RegistryGroup{"g": {Repos: map[string]RegistryRepo{}}}})
	s.groups["g"] = &LoadedGroup{Name: "g", Repos: map[string]*LoadedRepo{"r": lr}}
	s.mu.Lock()
	lr.retireHandle()
	s.mu.Unlock()
}
