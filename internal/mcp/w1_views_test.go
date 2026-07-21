package mcp

import (
	"runtime"
	"sort"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
)

// W1 of ADR-0027: light up the F2 hot index on the production borrow snapshot
// (memoized per repo per captured MapHandle), fill the relationship view seam
// F2 left, and expose additive view getters. These tests assert the new getters
// are behavior-neutral (parity with the existing *graph.Entity seam), the hot
// index builds exactly once per handle (memoization), and a reload invalidates
// the memo (no read across a reload).

// w1RelDoc is a doc with both entities and relationships for the relationship
// seam / iterator parity tests (sampleDoc carries entities only).
func w1RelDoc() *graph.Document {
	return &graph.Document{
		Entities: []graph.Entity{
			{ID: "r::pkg.A", Name: "A", QualifiedName: "pkg.A", Kind: "type"},
			{ID: "r::pkg.B", Name: "B", QualifiedName: "pkg.B", Kind: "func"},
			{ID: "r::pkg.C", Name: "C", QualifiedName: "pkg.C", Kind: "func"},
		},
		Relationships: []graph.Relationship{
			{ID: "rel1", FromID: "r::pkg.B", ToID: "r::pkg.A", Kind: "CALLS"},
			{ID: "rel2", FromID: "r::pkg.C", ToID: "r::pkg.A", Kind: "REFERENCES"},
			{ID: "rel3", FromID: "r::pkg.C", ToID: "r::pkg.B", Kind: "CALLS"},
		},
	}
}

// TestW1EntityViewGetterParity asserts the additive view getters return the same
// entities as the existing LabelIndex / getByID seam over the same graph — the
// behavior-neutral guarantee. The views wrap the same &Doc.Entities[i] pointers.
func TestW1EntityViewGetterParity(t *testing.T) {
	doc := sampleDoc()
	s, lr := newDocRepoState(doc, &countingCloser{})
	lr.LabelIndex = BuildLabelIndex(doc)

	b := s.borrowGroup("g")
	if b == nil {
		t.Fatal("borrowGroup returned nil")
	}
	defer b.Release()

	// byID parity against LabelIndex.ByID (and getByID) for every entity.
	byID := lr.getByID()
	for id, want := range byID {
		got, ok := b.entityViewByID("r", id)
		if !ok {
			t.Fatalf("entityViewByID(%q) missing; want %q", id, want.ID)
		}
		if got.ID() != want.ID || got.Name() != want.Name ||
			got.Kind() != want.Kind || got.QualifiedName() != want.QualifiedName {
			t.Errorf("entityViewByID(%q) = (%q,%q,%q,%q), want (%q,%q,%q,%q)",
				id, got.ID(), got.Name(), got.Kind(), got.QualifiedName(),
				want.ID, want.Name, want.Kind, want.QualifiedName)
		}
	}

	// byQName parity against LabelIndex.ByQName (case-insensitive).
	for _, e := range doc.Entities {
		if e.QualifiedName == "" {
			continue
		}
		got, ok := b.entityViewByQName("r", e.QualifiedName)
		want := lr.LabelIndex.ByQName(e.QualifiedName)
		if !ok || want == nil || got.ID() != want.ID {
			t.Errorf("entityViewByQName(%q) = %v/%v, want %v", e.QualifiedName, got, ok, want)
		}
	}

	// byLabel parity against LabelIndex.ByLabel, including ambiguous labels and
	// order (both are built in Doc order).
	for lbl, wantIdxs := range lr.LabelIndex.byLabel {
		gotVs := b.entityViewsByLabel("r", lbl)
		if len(gotVs) != len(wantIdxs) {
			t.Fatalf("entityViewsByLabel(%q) len = %d, want %d", lbl, len(gotVs), len(wantIdxs))
		}
		for i := range wantIdxs {
			wantID := lr.LabelIndex.at(wantIdxs[i]).ID
			if gotVs[i].ID() != wantID {
				t.Errorf("entityViewsByLabel(%q)[%d] = %q, want %q", lbl, i, gotVs[i].ID(), wantID)
			}
		}
	}

	// forEachEntityView parity against Doc.Entities, in order.
	var gotIDs []string
	b.forEachEntityView("r", func(v graph.EntityView) { gotIDs = append(gotIDs, v.ID()) })
	if len(gotIDs) != len(doc.Entities) {
		t.Fatalf("forEachEntityView yielded %d, want %d", len(gotIDs), len(doc.Entities))
	}
	for i := range doc.Entities {
		if gotIDs[i] != doc.Entities[i].ID {
			t.Errorf("forEachEntityView[%d] = %q, want %q", i, gotIDs[i], doc.Entities[i].ID)
		}
	}
}

// TestW1RelationshipViewSourceParity asserts the W1 relationship seam
// (docRelationshipViewSource + relationshipViewByID + forEachRelationshipView)
// is byte-identical to iterating Doc.Relationships — the gap F2 left.
func TestW1RelationshipViewSourceParity(t *testing.T) {
	doc := w1RelDoc()
	s, _ := newDocRepoState(doc, &countingCloser{})

	b := s.borrowGroup("g")
	defer b.Release()

	// by-id parity.
	for i := range doc.Relationships {
		r := &doc.Relationships[i]
		got, ok := b.relationshipViewByID("r", r.ID)
		if !ok {
			t.Fatalf("relationshipViewByID(%q) missing", r.ID)
		}
		if got.ID() != r.ID || got.FromID() != r.FromID || got.ToID() != r.ToID || got.Kind() != r.Kind {
			t.Errorf("relationshipViewByID(%q) = (%q,%q,%q,%q), want (%q,%q,%q,%q)",
				r.ID, got.ID(), got.FromID(), got.ToID(), got.Kind(),
				r.ID, r.FromID, r.ToID, r.Kind)
		}
	}

	// full-scan iteration parity, in Doc order.
	var gotIDs []string
	b.forEachRelationshipView("r", func(v graph.RelationshipView) { gotIDs = append(gotIDs, v.ID()) })
	if len(gotIDs) != len(doc.Relationships) {
		t.Fatalf("forEachRelationshipView yielded %d, want %d", len(gotIDs), len(doc.Relationships))
	}
	for i := range doc.Relationships {
		if gotIDs[i] != doc.Relationships[i].ID {
			t.Errorf("forEachRelationshipView[%d] = %q, want %q", i, gotIDs[i], doc.Relationships[i].ID)
		}
	}

	// docRelationshipViewSource directly: yields every relationship, in order.
	var srcIDs []string
	docRelationshipViewSource{doc: doc}.forEachRelationshipView(func(v graph.RelationshipView) {
		srcIDs = append(srcIDs, v.ID())
	})
	sort.Strings(srcIDs)
	if len(srcIDs) != len(doc.Relationships) {
		t.Fatalf("docRelationshipViewSource yielded %d, want %d", len(srcIDs), len(doc.Relationships))
	}

	// nil-doc source yields nothing.
	got := false
	docRelationshipViewSource{doc: nil}.forEachRelationshipView(func(graph.RelationshipView) { got = true })
	if got {
		t.Error("docRelationshipViewSource{nil} yielded a relationship")
	}
}

// TestW1HotIndexMemoizedPerHandle asserts the hot index builds EXACTLY ONCE per
// captured handle: repeated view-getter uses for the same handle reuse the same
// *hotIndex (build func invoked once); a distinct handle rebuilds.
func TestW1HotIndexMemoizedPerHandle(t *testing.T) {
	doc := sampleDoc()
	s, lr := newDocRepoState(doc, &countingCloser{})

	b := s.borrowGroup("g")
	defer b.Release()
	h := b.Handle("r")

	var builds int32
	build := func(hi *MapHandle) *hotIndex {
		atomic.AddInt32(&builds, 1)
		return buildHotIndex(hi, docEntityViewSource{doc: doc})
	}

	i1 := lr.hotIndexFor(h, build)
	i2 := lr.hotIndexFor(h, build)
	i3 := lr.hotIndexFor(h, build)
	if got := atomic.LoadInt32(&builds); got != 1 {
		t.Fatalf("build count = %d after 3 same-handle lookups, want 1 (memoization)", got)
	}
	if i1 != i2 || i2 != i3 {
		t.Fatalf("memoized index pointers differ: %p %p %p", i1, i2, i3)
	}
	if i1.Handle() != h {
		t.Fatalf("memoized index handle = %p, want captured %p", i1.Handle(), h)
	}

	// The production borrow getters share the same memoized index.
	if p := b.hotIndexFor("r"); p == nil {
		t.Fatal("b.hotIndexFor returned nil")
	} else if p2 := b.hotIndexFor("r"); p != p2 {
		t.Fatalf("borrow hotIndexFor rebuilt: %p vs %p", p, p2)
	}
}

// TestW1ReloadInvalidatesHotIndex asserts a reload (publishing a successor
// handle) invalidates the memo: the next lookup rebuilds against the fresh
// handle, and the index is never read across the reload. Covers both the
// handle-identity rebuild and the resetIndexes explicit clear.
func TestW1ReloadInvalidatesHotIndex(t *testing.T) {
	doc := sampleDoc()
	s, lr := newDocRepoState(doc, &countingCloser{})

	b1 := s.borrowGroup("g")
	first := b1.hotIndexFor("r")
	h1 := b1.Handle("r")
	if first == nil || first.Handle() != h1 {
		t.Fatalf("first index handle = %v, want %p", first, h1)
	}
	b1.Release()

	// Reload: publish a successor handle (mirrors the F1/F2 reload swap).
	s.mu.Lock()
	lr.publishHandle(&MapHandle{closer: &countingCloser{}})
	s.mu.Unlock()

	b2 := s.borrowGroup("g")
	defer b2.Release()
	second := b2.hotIndexFor("r")
	h2 := b2.Handle("r")
	if h2 == h1 {
		t.Fatal("reload did not change the captured handle")
	}
	if second == first {
		t.Fatal("hot index was reused across a reload (stale handle)")
	}
	if second.Handle() != h2 {
		t.Fatalf("rebuilt index handle = %p, want fresh %p", second.Handle(), h2)
	}

	// resetIndexes (Doc replacement) also clears the memo, independent of the
	// handle-identity check — the load-bearing invalidation for a no-mmap reload.
	_ = lr.hotIndexFor(h2, func(h *MapHandle) *hotIndex {
		return buildHotIndex(h, docEntityViewSource{doc: doc})
	})
	lr.resetIndexes()
	lr.hotIdxMu.Lock()
	cleared := lr.hotIdx == nil
	lr.hotIdxMu.Unlock()
	if !cleared {
		t.Fatal("resetIndexes did not clear the memoized hot index")
	}
}

// TestW1ViewGettersBindToCapturedHandleAcrossReload is the W1 captured-handle
// race test (reuses F2's reload-loop harness shape, now driving the MEMOIZED
// production view getters). N borrowers each capture a handle under s.mu, resolve
// ids through the borrow's view getters while a reloader repoints lr.handle in a
// tight loop. Under -race it asserts: the captured handle is never munmapped
// while its borrow is held, the getters answer correctly, and every retired
// mapping is unmapped exactly once.
func TestW1ViewGettersBindToCapturedHandleAcrossReload(t *testing.T) {
	doc := w1RelDoc()
	s := NewState(&Registry{Groups: map[string]RegistryGroup{
		"g": {Repos: map[string]RegistryRepo{}},
	}})
	lr := &LoadedRepo{Repo: "r", Doc: doc}
	s.groups["g"] = &LoadedGroup{Name: "g", Repos: map[string]*LoadedRepo{"r": lr}}

	var closersMu sync.Mutex
	var closers []*refCheckCloser
	newHandle := func() *MapHandle {
		c := &refCheckCloser{}
		h := &MapHandle{closer: c, releaseGap: runtime.Gosched}
		c.h = h
		closersMu.Lock()
		closers = append(closers, c)
		closersMu.Unlock()
		return h
	}

	s.mu.Lock()
	lr.publishHandle(newHandle())
	s.mu.Unlock()

	var readFault atomic.Int64
	stop := make(chan struct{})

	var wgR sync.WaitGroup
	wgR.Add(1)
	go func() {
		defer wgR.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			s.mu.Lock()
			lr.publishHandle(newHandle())
			s.mu.Unlock()
		}
	}()

	var wgB sync.WaitGroup
	for i := 0; i < 8; i++ {
		wgB.Add(1)
		go func() {
			defer wgB.Done()
			for j := 0; j < 2000; j++ {
				b := s.borrowGroup("g")
				if b == nil {
					readFault.Add(1)
					continue
				}
				captured := b.Handle("r")
				// Resolve through the memoized production getters; the index is
				// keyed off the captured handle, which stays mapped for the read.
				v, ok := b.entityViewByID("r", "r::pkg.A")
				if !ok || v.Name() != "A" {
					readFault.Add(1)
				}
				rv, rok := b.relationshipViewByID("r", "rel1")
				if !rok || rv.FromID() != "r::pkg.B" {
					readFault.Add(1)
				}
				if hi := b.hotIndexFor("r"); hi.Handle() != captured {
					readFault.Add(1)
				}
				if rc, isRC := captured.closer.(*refCheckCloser); isRC && rc.n.Load() > 0 {
					readFault.Add(1)
				}
				b.Release()
			}
		}()
	}

	wgB.Wait()
	close(stop)
	wgR.Wait()

	s.mu.Lock()
	lr.retireHandle()
	s.mu.Unlock()

	if f := readFault.Load(); f != 0 {
		t.Fatalf("read faults (captured handle munmapped under borrow, wrong result, or handle mismatch): %d", f)
	}
	closersMu.Lock()
	defer closersMu.Unlock()
	for i, c := range closers {
		if got := c.n.Load(); got != 1 {
			t.Fatalf("handle %d: munmap count = %d, want exactly 1 (leak or double-munmap)", i, got)
		}
		if bad := c.badWhileRefs.Load(); bad != 0 {
			t.Fatalf("handle %d: munmap observed refs>0 %d times, want 0", i, bad)
		}
	}
}
