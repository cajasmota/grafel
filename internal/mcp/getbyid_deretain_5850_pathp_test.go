// getbyid_deretain_5850_pathp_test.go — memory epic #5850, mmap-flip Path P.
//
// De-retain LoadedRepo.byID: on the GRAFEL_SERVE_FROM_MMAP-ON path the resident
// getByID cache must hold NO *graph.Entity — it stores only the entity-ID ->
// vector-INDEX map (map[string]int32) and resolves each entity ON DEMAND via the
// readerMu-guarded LabelIndex.at. Retaining the ~608 MB entity set here (as the
// pre-Path-P build did, `lr.byID[id] = ent`) would re-pin the whole Document
// flag-ON and defeat the mmap flip (Doc is emptied post-flip). This mirrors the
// BM25 no-retention prerequisite (TestBM25FromReaderNoEntityRetention_PR3b, L4).
//
// The flag-OFF default path is unchanged/byte-identical: it still memoizes a
// Doc-backed map[string]*graph.Entity in lr.byID, because Doc is retained
// flag-OFF anyway so pointer retention there is free.
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

// overlayForIdx is the deterministic overlay rule used by the parity test: every
// third entity gets a distinct CommunityID/PageRank; the rest stay at the
// graph.fb sentinel (nil) so BOTH flag paths must agree on the "no overlay" case.
func overlayForIdx(i int) (community *int, pr *float64) {
	if i%3 != 0 {
		return nil, nil
	}
	c := 1000 + i%50
	p := float64(i) * 0.001
	return &c, &p
}

// TestGetByIDNoEntityRetention_PathP is the load-bearing no-retention assertion:
// after getByID populates the cache flag-ON (resident Reader), the resident
// LoadedRepo cache holds only the map[string]int32 vector-index map — NO
// *graph.Entity. Mutation: reverting getByID flag-ON to store the materialized
// entities in lr.byID (map[string]*graph.Entity) makes byID non-nil and this
// test FAILs.
func TestGetByIDNoEntityRetention_PathP(t *testing.T) {
	withServeFromMMap(t, true)
	lr := newReaderBackedRepo(t, 400)

	// Populate the cache via the public getter (materialize-on-demand path).
	got := lr.getByID()
	if len(got) != lr.Reader.EntityCount() {
		t.Fatalf("getByID returned %d entities, want reader EntityCount=%d", len(got), lr.Reader.EntityCount())
	}

	// The RESIDENT flag-ON cache field must be a map[string]int32 (vector index),
	// populated one slot per reader row — proving it learned ID->index.
	if lr.byIDIdx == nil {
		t.Fatal("flag-ON getByID left byIDIdx nil — the int32 index cache was not built")
	}
	if _, ok := interface{}(lr.byIDIdx).(map[string]int32); !ok {
		t.Fatalf("byIDIdx is %T, want map[string]int32", lr.byIDIdx)
	}
	if len(lr.byIDIdx) != lr.Reader.EntityCount() {
		t.Fatalf("byIDIdx has %d entries, want reader EntityCount=%d", len(lr.byIDIdx), lr.Reader.EntityCount())
	}

	// The *graph.Entity resident cache MUST stay nil on the flag-ON path — no
	// entity pointers pinned resident (this is the whole memory point).
	if lr.byID != nil {
		t.Fatalf("flag-ON getByID retained a resident map[string]*graph.Entity (len=%d) — it must hold no entities", len(lr.byID))
	}

	// Structural sweep of the two cache fields: neither resident cache may be a
	// map/slice of *graph.Entity while populated on the flag-ON path. (byID is the
	// flag-OFF field and must be nil here; byIDIdx must be the int32 map.)
	entityPtrType := reflect.TypeOf((*graph.Entity)(nil))
	v := reflect.ValueOf(lr).Elem()
	if f := v.FieldByName("byIDIdx"); f.Type().Kind() == reflect.Map && f.Type().Elem() == entityPtrType {
		t.Fatal("byIDIdx is a map to *graph.Entity — the flag-ON cache must not retain entities")
	}
}

// TestGetByIDParity_FlagOnVsFlagOff_PathP proves getByID returns entities with
// identical fields (ID/Name/QualifiedName/SourceFile + overlay PageRank/
// CommunityID) on BOTH flags, over an overlaid fixture. The flag-ON path
// resolves via LabelIndex.at (Reader base + overlay side-table); the flag-OFF
// path copies from the overlay-stamped live Doc. They must agree field-for-field.
func TestGetByIDParity_FlagOnVsFlagOff_PathP(t *testing.T) {
	const n = 300

	// --- Flag-OFF result: overlay stamped directly onto the Doc rows. ---
	var offMap map[string]*graph.Entity
	func() {
		withServeFromMMap(t, false)
		doc, _ := loadBM25RichFixture(t, n)
		for i := range doc.Entities {
			c, p := overlayForIdx(i)
			doc.Entities[i].CommunityID = c
			doc.Entities[i].PageRank = p
		}
		lr := &LoadedRepo{Repo: "corpus", Doc: doc}
		offMap = lr.getByID()
	}()

	// --- Flag-ON result: overlay carried in the LabelIndex side-table. ---
	withServeFromMMap(t, true)
	lr := newReaderBackedRepo(t, n)
	table := make(map[int32]entityOverlay)
	for id, idx := range lr.LabelIndex.byID {
		_ = id
		c, p := overlayForIdx(int(idx))
		if c == nil && p == nil {
			continue // no overlay entry → reader sentinel (nil) survives
		}
		table[idx] = entityOverlay{CommunityID: c, PageRank: p}
	}
	lr.LabelIndex.overlay = table
	onMap := lr.getByID()

	if len(onMap) != len(offMap) {
		t.Fatalf("map length differs: flag-on=%d flag-off=%d", len(onMap), len(offMap))
	}

	derefI := func(p *int) string {
		if p == nil {
			return "<nil>"
		}
		return itoa(*p)
	}
	derefF := func(p *float64) float64 {
		if p == nil {
			return -1
		}
		return *p
	}

	for id, off := range offMap {
		on, ok := onMap[id]
		if !ok {
			t.Fatalf("flag-on getByID missing id %q", id)
		}
		if on.ID != off.ID || on.Name != off.Name || on.QualifiedName != off.QualifiedName || on.SourceFile != off.SourceFile {
			t.Fatalf("base-field mismatch for %q:\n on={ID:%q Name:%q QN:%q SF:%q}\noff={ID:%q Name:%q QN:%q SF:%q}",
				id, on.ID, on.Name, on.QualifiedName, on.SourceFile, off.ID, off.Name, off.QualifiedName, off.SourceFile)
		}
		if derefI(on.CommunityID) != derefI(off.CommunityID) {
			t.Fatalf("CommunityID mismatch for %q: on=%s off=%s", id, derefI(on.CommunityID), derefI(off.CommunityID))
		}
		if derefF(on.PageRank) != derefF(off.PageRank) {
			t.Fatalf("PageRank mismatch for %q: on=%v off=%v", id, derefF(on.PageRank), derefF(off.PageRank))
		}
	}
}

// TestGetByIDFlagOnEmptiedDoc_PathP is the post-flip-shape test: flag-ON, a
// resident Reader, and an EMPTIED lr.Doc.Entities. getByID must still resolve
// every valid id (bound + resolved via the Reader, not the empty Doc) and return
// nil for an unknown id. Mutation: bounding/resolving getByID against lr.Doc
// instead of the Reader collapses the map to empty → this FAILs.
func TestGetByIDFlagOnEmptiedDoc_PathP(t *testing.T) {
	withServeFromMMap(t, true)

	full := newReaderBackedRepo(t, 250)
	wantIDs := make([]string, 0, full.Reader.EntityCount())
	for id := range full.LabelIndex.byID {
		wantIDs = append(wantIDs, id)
	}

	// Simulate the PR7 Doc-emptying: drop every Doc row while the Reader keeps
	// them. at() reads l.doc, so keep it consistent with the emptied lr.Doc.
	full.Doc = emptiedParityDoc(full.Doc)
	full.LabelIndex.doc = full.Doc

	got := full.getByID()
	if len(got) != len(wantIDs) {
		t.Fatalf("getByID with emptied Doc: got %d entities, want %d (must bind against the Reader)", len(got), len(wantIDs))
	}
	for _, id := range wantIDs {
		e, ok := got[id]
		if !ok || e == nil {
			t.Fatalf("getByID missing id %q with emptied Doc", id)
			continue
		}
		if e.ID != id {
			t.Errorf("getByID[%q].ID = %q", id, e.ID)
		}
	}
	if _, ok := got["this-id-does-not-exist"]; ok {
		t.Fatal("getByID returned an entry for an unknown id")
	}
}

// TestGetByIDReloadRace_PathP runs getByID concurrently with a reload that
// retires+munmaps the mapping the resident LabelIndex points at. Under -race it
// must finish with no SIGBUS/SIGSEGV: getByID resolves via the readerMu-guarded
// at(), so a lookup whose mapping was retired takes a safe non-mmap path instead
// of dereferencing the freed region. Post-#5870 PR7bc that safe path is a
// graceful MISS (at() returns nil for a retired generation, no Doc fallback), so
// a retired-generation entity may be ABSENT — never wrong, never a freed-region
// deref. In production reload also reassigns lr.LabelIndex to the fresh
// generation; this test pins the field to the retired index on purpose to
// exercise at()'s retired branch, so absence here is the expected steady state
// after the retire lands.
func TestGetByIDReloadRace_PathP(t *testing.T) {
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

	// Install a single generation; getByID reads lr.LabelIndex, which stays fixed
	// for the whole test (the reloader retires the handle it points at, it does not
	// reassign the field — avoiding an unrelated field race).
	s.mu.Lock()
	h0 := newMapHandle(open())
	lr.LabelIndex = wireReaderLabelIndex(lr, h0.reader, h0, doc)
	lr.publishHandle(h0)
	s.mu.Unlock()

	var faults atomic.Int64
	start := make(chan struct{})
	var wg sync.WaitGroup

	// Readers: hammer getByID. e5 is either PRESENT and correct (pre-retire mmap
	// read) or ABSENT (post-retire graceful miss — at() returns nil for the retired
	// generation). The invariant is "never wrong data, never a freed-region deref";
	// a present entry that is nil or mismatched is a fault, absence is not.
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for j := 0; j < 600; j++ {
				m := lr.getByID()
				if e, ok := m["e5"]; ok && (e == nil || e.ID != "e5" || e.QualifiedName != "r.e5") {
					faults.Add(1)
				}
			}
		}()
	}

	// Reloader: retire+munmap the mapping mid-flight (once), racing in-flight
	// getByID resolutions. publishHandle sets readRetired under readerMu before the
	// munmap, so at() sees it and falls back.
	wg.Add(1)
	go func() {
		defer wg.Done()
		<-start
		nh := newMapHandle(open())
		s.mu.Lock()
		lr.publishHandle(nh) // retire+munmap h0 (the generation lr.LabelIndex holds)
		s.mu.Unlock()
	}()

	close(start)
	wg.Wait()

	s.mu.Lock()
	lr.retireHandle()
	s.mu.Unlock()

	if f := faults.Load(); f != 0 {
		t.Fatalf("getByID returned wrong/nil entity under concurrent retire: %d faults", f)
	}
}

// TestGetByIDFlagOffByteIdentical_PathP pins that the flag-OFF default path is
// unchanged: getByID memoizes a Doc-backed map[string]*graph.Entity in lr.byID
// (byIDIdx stays nil), returns every Doc row, and the returned pointers are
// distinct heap copies (not aliases into Doc.Entities' backing array).
func TestGetByIDFlagOffByteIdentical_PathP(t *testing.T) {
	withServeFromMMap(t, false)

	doc, _ := loadBM25RichFixture(t, 200)
	lr := &LoadedRepo{Repo: "corpus", Doc: doc}

	got := lr.getByID()
	if len(got) != len(doc.Entities) {
		t.Fatalf("flag-off getByID: got %d entities, want %d", len(got), len(doc.Entities))
	}
	if lr.byID == nil {
		t.Fatal("flag-off getByID did not memoize the *graph.Entity map in lr.byID")
	}
	if lr.byIDIdx != nil {
		t.Fatalf("flag-off getByID built the int32 index cache (len=%d); it must stay nil off-flip", len(lr.byIDIdx))
	}
	// Second call returns the SAME memoized map (stable within a reload).
	if got2 := lr.getByID(); reflect.ValueOf(got2).Pointer() != reflect.ValueOf(got).Pointer() {
		t.Fatal("flag-off getByID is not memoized: two calls returned different maps")
	}
	for i := range doc.Entities {
		want := &doc.Entities[i]
		e, ok := got[want.ID]
		if !ok || e.ID != want.ID {
			t.Fatalf("flag-off getByID[%q] = %+v", want.ID, e)
		}
		if e == want {
			t.Fatalf("flag-off getByID[%q] aliases Doc.Entities backing array (must be a heap copy)", want.ID)
		}
	}
}

// TestGetByIDOneSingleMaterialization_PathP is the load-bearing perf test for
// getByIDOne: on the flag-ON mmap path it must materialize EXACTLY ONE entity
// (the requested id), never the whole set. Mutation: routing getByIDOne through
// the whole-map getByID() (return getByID()[id]) materializes every row → the
// count assertion FAILs. Guards against the 62%-of-callers per-call whole-repo
// materialization the reviewer flagged.
func TestGetByIDOneSingleMaterialization_PathP(t *testing.T) {
	withServeFromMMap(t, true)
	lr := newReaderBackedRepo(t, 500)

	var someID string
	for id := range lr.LabelIndex.byID {
		someID = id
		break
	}

	var count atomic.Int64
	atMaterializeHook = func() { count.Add(1) }
	t.Cleanup(func() { atMaterializeHook = nil })

	// Warm the int32 index cache first so its build (which materializes nothing)
	// is not conflated with the single resolve we measure.
	_, _ = lr.getByIDOne(someID)
	if lr.byIDIdx == nil {
		t.Fatal("getByIDOne did not build the int32 index cache")
	}

	count.Store(0)
	e, ok := lr.getByIDOne(someID)
	if !ok || e == nil || e.ID != someID {
		t.Fatalf("getByIDOne(%q) = (%v, %v)", someID, e, ok)
	}
	if got := count.Load(); got != 1 {
		t.Fatalf("getByIDOne materialized %d entities, want exactly 1 (whole-map path?)", got)
	}

	// A whole-map getByID by contrast materializes every row — proves the counter
	// is live and that the two paths differ.
	count.Store(0)
	_ = lr.getByID()
	if got := count.Load(); got != int64(lr.Reader.EntityCount()) {
		t.Fatalf("getByID materialized %d entities, want all %d", got, lr.Reader.EntityCount())
	}

	// getByIDOne must not build the flag-OFF whole-entity resident cache.
	if lr.byID != nil {
		t.Fatalf("getByIDOne built the whole-map resident cache lr.byID")
	}
}

// TestGetByIDOneParity_FlagOnVsFlagOff_PathP proves getByIDOne returns an entity
// with identical fields (base + overlay PageRank/CommunityID) on both flags for
// a given id — the per-id analogue of the whole-map parity test.
func TestGetByIDOneParity_FlagOnVsFlagOff_PathP(t *testing.T) {
	const n = 300

	// Flag-OFF: overlay stamped onto the Doc rows.
	var offRepo *LoadedRepo
	var offIDs []string
	func() {
		withServeFromMMap(t, false)
		doc, _ := loadBM25RichFixture(t, n)
		for i := range doc.Entities {
			c, p := overlayForIdx(i)
			doc.Entities[i].CommunityID = c
			doc.Entities[i].PageRank = p
			offIDs = append(offIDs, doc.Entities[i].ID)
		}
		offRepo = &LoadedRepo{Repo: "corpus", Doc: doc}
	}()
	offVals := make(map[string]*graph.Entity, len(offIDs))
	withServeFromMMap(t, false)
	for _, id := range offIDs {
		e, ok := offRepo.getByIDOne(id)
		if !ok {
			t.Fatalf("flag-off getByIDOne missing %q", id)
		}
		offVals[id] = e
	}

	// Flag-ON: same overlay carried in the LabelIndex side-table.
	withServeFromMMap(t, true)
	lr := newReaderBackedRepo(t, n)
	table := make(map[int32]entityOverlay)
	for _, idx := range lr.LabelIndex.byID {
		c, p := overlayForIdx(int(idx))
		if c == nil && p == nil {
			continue
		}
		table[idx] = entityOverlay{CommunityID: c, PageRank: p}
	}
	lr.LabelIndex.overlay = table

	derefI := func(p *int) string {
		if p == nil {
			return "<nil>"
		}
		return itoa(*p)
	}
	derefF := func(p *float64) float64 {
		if p == nil {
			return -1
		}
		return *p
	}
	for _, id := range offIDs {
		on, ok := lr.getByIDOne(id)
		if !ok {
			t.Fatalf("flag-on getByIDOne missing %q", id)
		}
		off := offVals[id]
		if on.ID != off.ID || on.Name != off.Name || on.QualifiedName != off.QualifiedName || on.SourceFile != off.SourceFile {
			t.Fatalf("base-field mismatch for %q: on=%+v off=%+v", id, on, off)
		}
		if derefI(on.CommunityID) != derefI(off.CommunityID) {
			t.Fatalf("CommunityID mismatch for %q: on=%s off=%s", id, derefI(on.CommunityID), derefI(off.CommunityID))
		}
		if derefF(on.PageRank) != derefF(off.PageRank) {
			t.Fatalf("PageRank mismatch for %q: on=%v off=%v", id, derefF(on.PageRank), derefF(off.PageRank))
		}
	}

	// Unknown id → (nil, false) on both flags.
	if e, ok := lr.getByIDOne("no-such-id"); ok || e != nil {
		t.Fatalf("flag-on getByIDOne(unknown) = (%v, %v), want (nil, false)", e, ok)
	}
	withServeFromMMap(t, false)
	if e, ok := offRepo.getByIDOne("no-such-id"); ok || e != nil {
		t.Fatalf("flag-off getByIDOne(unknown) = (%v, %v), want (nil, false)", e, ok)
	}
}

// TestGetByIDOneReloadRace_PathP runs getByIDOne concurrently with a reload that
// retires+munmaps the mapping the resident LabelIndex points at. Under -race it
// must finish with no SIGBUS: getByIDOne resolves the single index via the
// readerMu-guarded at(). Post-#5870 PR7bc at() returns nil for a retired
// generation (no Doc fallback), so getByIDOne reports (nil,false) — a graceful
// miss — once the retire lands; the invariant is "no freed-region deref, never
// WRONG data", so a present result must be correct and a miss is acceptable.
func TestGetByIDOneReloadRace_PathP(t *testing.T) {
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

	s.mu.Lock()
	h0 := newMapHandle(open())
	lr.LabelIndex = wireReaderLabelIndex(lr, h0.reader, h0, doc)
	lr.publishHandle(h0)
	s.mu.Unlock()

	var faults atomic.Int64
	start := make(chan struct{})
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for j := 0; j < 600; j++ {
				e, ok := lr.getByIDOne("e5")
				if ok && (e == nil || e.ID != "e5" || e.QualifiedName != "r.e5") {
					faults.Add(1)
				}
			}
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		<-start
		nh := newMapHandle(open())
		s.mu.Lock()
		lr.publishHandle(nh)
		s.mu.Unlock()
	}()

	close(start)
	wg.Wait()

	s.mu.Lock()
	lr.retireHandle()
	s.mu.Unlock()

	if f := faults.Load(); f != 0 {
		t.Fatalf("getByIDOne returned wrong/nil entity under concurrent retire: %d faults", f)
	}
}
