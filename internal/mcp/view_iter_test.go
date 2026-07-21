// view_iter_test.go — TDD tests for the ADR-0027 mmap-cutover Path P PR1
// additive iterator primitive (memory epic #5850). forEachEntity/
// forEachRelationship replace the ubiquitous `for i := range lr.Doc.Entities`
// shape without migrating any caller in this PR — see view_iter.go.
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
	"github.com/cajasmota/grafel/internal/graph/groupalgo"
)

// ---- Test 1: flag-OFF parity ----

func TestForEachEntity_FlagOffParity(t *testing.T) {
	forceServeFromMMap(t, false)

	doc := sigbusFixtureDoc(6)
	lr := &LoadedRepo{Repo: "r", Doc: doc}

	var want []*graph.Entity
	for i := range lr.Doc.Entities {
		want = append(want, &lr.Doc.Entities[i])
	}

	var got []*graph.Entity
	lr.forEachEntity(func(e *graph.Entity) bool {
		got = append(got, e)
		return true
	})

	if len(got) != len(want) {
		t.Fatalf("forEachEntity yielded %d entities, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("entity[%d]: pointer mismatch got=%p (%+v) want=%p (%+v)", i, got[i], got[i], want[i], want[i])
		}
	}
}

func TestForEachRelationship_FlagOffParity(t *testing.T) {
	forceServeFromMMap(t, false)

	doc := sigbusFixtureDoc(6)
	lr := &LoadedRepo{Repo: "r", Doc: doc}

	var want []*graph.Relationship
	for i := range lr.Doc.Relationships {
		want = append(want, &lr.Doc.Relationships[i])
	}

	var got []*graph.Relationship
	lr.forEachRelationship(func(rel *graph.Relationship) bool {
		got = append(got, rel)
		return true
	})

	if len(got) != len(want) {
		t.Fatalf("forEachRelationship yielded %d relationships, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("relationship[%d]: pointer mismatch got=%p (%+v) want=%p (%+v)", i, got[i], got[i], want[i], want[i])
		}
	}
}

// ---- Test 2: flag-ON parity + overlay ----

func TestForEachEntity_FlagOnParityWithOverlay(t *testing.T) {
	forceServeFromMMap(t, true)
	st, overlayPath, cur, ids := setupSideTableParity(t)

	alpha, bravo := ids[0], ids[1] // charlie deliberately un-overlaid
	ov := &groupalgo.Overlay{
		Group:        "acme",
		SourceMtimes: cur,
		Results: map[string]groupalgo.EntityOverlay{
			alpha: {CommunityID: 39, PageRank: 0.0065, Centrality: 10415.99, IsGodNode: true},
			bravo: {CommunityID: 80, PageRank: 0.0012, Centrality: 6706.5, IsArticulationPoint: true},
		},
		Communities: []graph.CommunityResult{
			{ID: 39, Size: 1, AutoName: "alpha-cluster"},
			{ID: 80, Size: 1, AutoName: "bravo-cluster"},
		},
	}
	if err := groupalgo.WriteOverlayTo(overlayPath, ov); err != nil {
		t.Fatalf("write overlay: %v", err)
	}

	if _, err := st.Reload(); err != nil {
		t.Fatalf("reload: %v", err)
	}

	grp := st.Group("acme")
	lr := grp.Repos["svc"]
	if lr == nil || lr.Doc == nil || lr.Reader == nil || lr.LabelIndex == nil {
		t.Fatalf("svc repo not fully wired for mmap")
	}

	// Ground truth: overlaid Doc rows, by ID.
	wantByID := map[string]*graph.Entity{}
	for i := range lr.Doc.Entities {
		e := lr.Doc.Entities[i]
		wantByID[e.ID] = &e
	}

	var gotByID = map[string]*graph.Entity{}
	count := 0
	lr.forEachEntity(func(e *graph.Entity) bool {
		count++
		gotByID[e.ID] = e
		return true
	})

	if count != len(lr.Doc.Entities) {
		t.Fatalf("forEachEntity flag-on yielded %d entities, want %d", count, len(lr.Doc.Entities))
	}
	for id, want := range wantByID {
		got := gotByID[id]
		if got == nil {
			t.Fatalf("forEachEntity flag-on missing entity %s", id)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("forEachEntity flag-on parity mismatch for %s:\n got=%#v\nwant=%#v", id, got, want)
		}
	}
}

func TestForEachRelationship_FlagOnParity(t *testing.T) {
	forceServeFromMMap(t, true)

	doc := sigbusFixtureDoc(6)
	fbPath := filepath.Join(t.TempDir(), "graph.fb")
	if err := fbwriter.WriteAtomic(fbPath, doc); err != nil {
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

	s := NewState(&Registry{Groups: map[string]RegistryGroup{"g": {Repos: map[string]RegistryRepo{}}}})
	s.groups["g"] = &LoadedGroup{Name: "g", Repos: map[string]*LoadedRepo{"r": lr}}
	t.Cleanup(func() {
		s.mu.Lock()
		lr.retireHandle()
		s.mu.Unlock()
	})

	var got []*graph.Relationship
	lr.forEachRelationship(func(rel *graph.Relationship) bool {
		got = append(got, rel)
		return true
	})

	if len(got) != len(doc.Relationships) {
		t.Fatalf("forEachRelationship flag-on yielded %d, want %d", len(got), len(doc.Relationships))
	}
	for i := range doc.Relationships {
		want := doc.Relationships[i]
		if got[i].FromID != want.FromID || got[i].ToID != want.ToID || got[i].Kind != want.Kind {
			t.Fatalf("relationship[%d]: got=%+v want=%+v", i, got[i], want)
		}
	}
}

// ---- Test 3: early-stop, both flag states ----

func TestForEachEntity_EarlyStop(t *testing.T) {
	for _, flag := range []bool{false, true} {
		flag := flag
		t.Run(boolLabel(flag), func(t *testing.T) {
			forceServeFromMMap(t, flag)

			doc := sigbusFixtureDoc(10)
			var lr *LoadedRepo
			var cleanup func()
			if flag {
				lr, cleanup = wireMMapRepo(t, doc)
				defer cleanup()
			} else {
				lr = &LoadedRepo{Repo: "r", Doc: doc}
			}

			const stopAt = 3
			n := 0
			lr.forEachEntity(func(e *graph.Entity) bool {
				n++
				return n < stopAt
			})
			if n != stopAt {
				t.Fatalf("forEachEntity early-stop: yielded %d, want %d", n, stopAt)
			}
		})
	}
}

func TestForEachRelationship_EarlyStop(t *testing.T) {
	for _, flag := range []bool{false, true} {
		flag := flag
		t.Run(boolLabel(flag), func(t *testing.T) {
			forceServeFromMMap(t, flag)

			doc := sigbusFixtureDoc(10)
			var lr *LoadedRepo
			var cleanup func()
			if flag {
				lr, cleanup = wireMMapRepo(t, doc)
				defer cleanup()
			} else {
				lr = &LoadedRepo{Repo: "r", Doc: doc}
			}

			const stopAt = 2
			n := 0
			lr.forEachRelationship(func(rel *graph.Relationship) bool {
				n++
				return n < stopAt
			})
			if n != stopAt {
				t.Fatalf("forEachRelationship early-stop: yielded %d, want %d", n, stopAt)
			}
		})
	}
}

func boolLabel(b bool) string {
	if b {
		return "flag-on"
	}
	return "flag-off"
}

// wireMMapRepo builds a fully-wired flag-on LoadedRepo (real mmap Reader +
// handle + LabelIndex) for doc, and returns a cleanup func that unmaps it.
func wireMMapRepo(t *testing.T, doc *graph.Document) (*LoadedRepo, func()) {
	t.Helper()
	fbPath := filepath.Join(t.TempDir(), "graph.fb")
	if err := fbwriter.WriteAtomic(fbPath, doc); err != nil {
		t.Fatalf("write graph.fb: %v", err)
	}
	rdr, err := fbreader.Open(fbPath)
	if err != nil {
		t.Fatalf("open reader: %v", err)
	}
	lr := &LoadedRepo{Repo: "r", Doc: doc}
	h := newMapHandle(rdr)
	lr.LabelIndex = wireReaderLabelIndex(lr, rdr, h, doc)
	lr.publishHandle(h)

	s := NewState(&Registry{Groups: map[string]RegistryGroup{"g": {Repos: map[string]RegistryRepo{}}}})
	s.groups["g"] = &LoadedGroup{Name: "g", Repos: map[string]*LoadedRepo{"r": lr}}

	return lr, func() {
		s.mu.Lock()
		lr.retireHandle()
		s.mu.Unlock()
	}
}

// ---- Test 4: flag-ON concurrent-reload-race, -race, no SIGBUS ----

// TestForEachEntity_FlagOnConcurrentReloadRaceNoSIGBUS is a SIGBUS/fallback
// smoke test: it hammers forEachEntity flag-on while a goroutine swaps+retires+
// munmaps the reader, proving the readerMu/readRetired substrate keeps the scan
// SIGBUS-free. It reimplements the guarded publish INLINE, so it does NOT prove
// the PRODUCTION state.go wraps are load-bearing — that is
// TestForEachEntity_RealReloadOverlayRestampRace's job (it drives the real
// reloadLocked/applyGroupAlgoOverlay write path).
func TestForEachEntity_FlagOnConcurrentReloadRaceNoSIGBUS(t *testing.T) {
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

	stop := make(chan struct{})
	var faults atomic.Int64

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
			lr.resetIndexes()
			// readerMu-guarded LabelIndex publish, mirroring reloadLocked
			// (state.go) since forEachEntity reads lr.LabelIndex under readerMu
			// across its whole flag-on scan (Path P PR1).
			lr.readerMu.Lock()
			lr.LabelIndex = li
			lr.readerMu.Unlock()
			lr.publishHandle(nh)
			s.mu.Unlock()
		}
	}()

	var wgB sync.WaitGroup
	for g := 0; g < 6; g++ {
		wgB.Add(1)
		go func() {
			defer wgB.Done()
			for j := 0; j < 300; j++ {
				n := 0
				lr.forEachEntity(func(e *graph.Entity) bool {
					if e == nil || e.ID == "" {
						faults.Add(1)
					}
					n++
					return true
				})
				if n != 24 {
					faults.Add(1)
				}
				m := 0
				lr.forEachRelationship(func(rel *graph.Relationship) bool {
					if rel == nil || rel.FromID == "" {
						faults.Add(1)
					}
					m++
					return true
				})
			}
		}()
	}

	wgB.Wait()
	close(stop)
	wgR.Wait()

	s.mu.Lock()
	lr.retireHandle()
	s.mu.Unlock()

	if f := faults.Load(); f != 0 {
		t.Fatalf("forEach* read faults (wrong/nil entity or relationship, or short scan): %d", f)
	}
}

// TestForEachEntity_RealReloadOverlayRestampRace is the MUTATION ORACLE for the
// two state.go readerMu wraps added alongside view_iter.go: the
// `lr.LabelIndex = li` publish in reloadLocked (site A) and the
// `lr.LabelIndex.overlay = table` re-stamp in applyGroupAlgoOverlay (site B).
//
// Unlike TestForEachEntity_FlagOnConcurrentReloadRaceNoSIGBUS (which
// reimplements the guarded publish INLINE and so cannot detect a revert of the
// PRODUCTION wraps), this test drives the REAL write path end-to-end: a writer
// goroutine repeatedly rewrites graph.fb (forcing a real reparse →
// reloadLocked republishes lr.LabelIndex, site A) and the group-algo overlay
// (advancing its mtime → applyGroupAlgoOverlay re-stamps lr.LabelIndex.overlay,
// site B), via the actual State.Reload / State.Group entry points — never an
// inline copy. Flag-on forEachEntity readers concurrently read lr.LabelIndex +
// .overlay under readerMu.
//
// With the state.go wraps IN PLACE: -race clean. With EITHER wrap REVERTED
// (write drops back to s.mu-only, out of sync with the readerMu-holding
// reader): -race reports DATA RACE — proving the wraps are load-bearing.
func TestForEachEntity_RealReloadOverlayRestampRace(t *testing.T) {
	forceServeFromMMap(t, true)
	st, overlayPath, cur, ids := setupSideTableParity(t)
	alpha, bravo := ids[0], ids[1]

	writeOverlay := func(mtimes map[string]int64, seed int) {
		ov := &groupalgo.Overlay{
			Group:        "acme",
			SourceMtimes: mtimes,
			Results: map[string]groupalgo.EntityOverlay{
				alpha: {CommunityID: 39 + seed, PageRank: 0.0065, Centrality: 10415.99, IsGodNode: true},
				bravo: {CommunityID: 80 + seed, PageRank: 0.0012, Centrality: 6706.5, IsArticulationPoint: true},
			},
			Communities: []graph.CommunityResult{
				{ID: 39 + seed, Size: 1, AutoName: "alpha-cluster"},
				{ID: 80 + seed, Size: 1, AutoName: "bravo-cluster"},
			},
		}
		if err := groupalgo.WriteOverlayTo(overlayPath, ov); err != nil {
			t.Errorf("write overlay: %v", err)
		}
	}

	writeOverlay(cur, 0)
	if _, err := st.Reload(); err != nil {
		t.Fatalf("initial reload: %v", err)
	}

	grp := st.Group("acme")
	lr := grp.Repos["svc"]
	if lr == nil || lr.Doc == nil || lr.Reader == nil || lr.LabelIndex == nil {
		t.Fatalf("svc repo not fully wired for the mmap read path")
	}
	graphFile := lr.GraphFile
	if graphFile == "" {
		t.Fatalf("lr.GraphFile empty — cannot drive a real reparse")
	}
	wantEnts := len(lr.Doc.Entities)

	stop := make(chan struct{})
	var faults atomic.Int64

	// Writer: drive the REAL reloadLocked (site A) + applyGroupAlgoOverlay
	// (site B) via State.Reload/State.Group.
	var wgW sync.WaitGroup
	wgW.Add(1)
	go func() {
		defer wgW.Done()
		for i := 1; i <= 200; i++ {
			select {
			case <-stop:
				return
			default:
			}
			// Toggle a byte-affecting field so the graph.fb hash changes and
			// reloadLocked takes the reparse branch (republishing lr.LabelIndex).
			d := richFixtureDoc("svc", "Alpha", "Bravo", "Charlie")
			d.Entities[0].StartLine = 100 + i
			if err := fbwriter.WriteAtomic(graphFile, d); err != nil {
				t.Errorf("rewrite graph.fb: %v", err)
				return
			}
			m, err := groupalgo.CurrentSourceMtimes("acme")
			if err != nil {
				m = cur
			}
			// Advance the overlay (new mtime + matching source mtimes) so
			// applyGroupAlgoOverlay re-stamps lr.LabelIndex.overlay (site B).
			writeOverlay(m, i)
			if _, err := st.Reload(); err != nil {
				t.Errorf("reload: %v", err)
				return
			}
			_ = st.Group("acme") // real refreshGroupAlgoOverlayLocked → applyGroupAlgoOverlay
		}
	}()

	// Readers: flag-on forEachEntity reads lr.LabelIndex + .overlay under readerMu.
	var wgR sync.WaitGroup
	for g := 0; g < 6; g++ {
		wgR.Add(1)
		go func() {
			defer wgR.Done()
			for j := 0; j < 400; j++ {
				n := 0
				lr.forEachEntity(func(e *graph.Entity) bool {
					if e == nil || e.ID == "" {
						faults.Add(1)
					}
					n++
					return true
				})
				if n != wantEnts {
					faults.Add(1)
				}
			}
		}()
	}

	wgR.Wait()
	close(stop)
	wgW.Wait()

	if f := faults.Load(); f != 0 {
		t.Fatalf("forEachEntity read faults (wrong/nil entity or short scan): %d", f)
	}
}
