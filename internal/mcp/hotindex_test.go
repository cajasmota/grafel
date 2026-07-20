package mcp

import (
	"runtime"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
)

// F2 of ADR-0027: the handle-keyed hot index. These tests actually RUN the
// handle-fed builder (build the index from a captured MapHandle, query it, get
// correct results) and prove the read-through-captured-handle invariant: the
// index is keyed off the handle captured under s.mu in borrowGroup, and stays
// valid (never munmapped) across a concurrent reload while a borrow is held.

func newDocRepoState(doc *graph.Document, c readerCloser) (*State, *LoadedRepo) {
	s := NewState(&Registry{Groups: map[string]RegistryGroup{
		"g": {Repos: map[string]RegistryRepo{}},
	}})
	lr := &LoadedRepo{Repo: "r", Doc: doc}
	s.groups["g"] = &LoadedGroup{Name: "g", Repos: map[string]*LoadedRepo{"r": lr}}
	s.mu.Lock()
	lr.publishHandle(&MapHandle{closer: c})
	s.mu.Unlock()
	return s, lr
}

func sampleDoc() *graph.Document {
	return &graph.Document{
		Entities: []graph.Entity{
			{ID: "r::pkg.Foo", Name: "Foo", QualifiedName: "pkg.Foo", Kind: "type"},
			{ID: "r::pkg.Bar", Name: "Bar", QualifiedName: "pkg.Bar", Kind: "func"},
			{ID: "r::pkg.foo2", Name: "Foo", QualifiedName: "pkg.Foo2", Kind: "func"},
		},
	}
}

// TestBuildHotIndexFromCapturedHandle runs the handle-fed builder end to end: it
// captures a handle under s.mu via borrowGroup, feeds the builder a view source,
// and asserts (a) the index is keyed off the captured handle and (b) byID /
// byLabel / byQName return the correct EntityViews.
func TestBuildHotIndexFromCapturedHandle(t *testing.T) {
	s, _ := newDocRepoState(sampleDoc(), &countingCloser{})

	b := s.borrowGroup("g")
	if b == nil {
		t.Fatal("borrowGroup returned nil")
	}
	defer b.Release()

	captured := b.Handle("r")
	if captured == nil {
		t.Fatal("no handle captured for repo r")
	}

	hi := b.buildHotIndex("r", docEntityViewSource{doc: b.Group.Repos["r"].Doc})
	if hi == nil {
		t.Fatal("buildHotIndex returned nil")
	}

	// Blocking F2 criterion: the hot index is keyed off the SAME handle the call
	// borrowed under s.mu, not a live lr.handle re-deref.
	if hi.Handle() != captured {
		t.Fatalf("hot index handle = %p, want captured handle %p", hi.Handle(), captured)
	}

	v, ok := hi.entityByID("r::pkg.Bar")
	if !ok {
		t.Fatal("entityByID(r::pkg.Bar) missing")
	}
	if v.Name() != "Bar" || v.Kind() != "func" {
		t.Errorf("byID view = (%q,%q), want (Bar,func)", v.Name(), v.Kind())
	}

	q, ok := hi.entityByQName("pkg.foo2") // qname lookup is case-insensitive
	if !ok || q.ID() != "r::pkg.foo2" {
		t.Errorf("entityByQName(pkg.foo2) = %v/%v, want r::pkg.foo2", q, ok)
	}

	// "Foo" is an ambiguous label (two entities share Name=="Foo").
	foos := hi.entitiesByLabel("foo") // label lookup is case-insensitive
	if len(foos) != 2 {
		t.Fatalf("entitiesByLabel(foo) returned %d, want 2", len(foos))
	}
	ids := map[string]bool{}
	for _, e := range foos {
		ids[e.ID()] = true
	}
	if !ids["r::pkg.Foo"] || !ids["r::pkg.foo2"] {
		t.Errorf("entitiesByLabel(foo) ids = %v, want both Foo entities", ids)
	}
}

// TestBuildHotIndexIsHandleAgnosticSource proves the builder depends only on the
// (handle, view-source) seam — a nil source yields an empty index still keyed
// off the captured handle. This is what lets F3 swap an mmap-backed source in
// without touching the builder or its consumers.
func TestBuildHotIndexEmptySourceKeepsHandle(t *testing.T) {
	s, _ := newDocRepoState(sampleDoc(), &countingCloser{})
	b := s.borrowGroup("g")
	defer b.Release()

	hi := buildHotIndex(b.Handle("r"), nil)
	if hi.Handle() != b.Handle("r") {
		t.Fatal("empty-source index lost its captured handle")
	}
	if _, ok := hi.entityByID("r::pkg.Foo"); ok {
		t.Error("empty-source index returned an entity")
	}
}

// TestHotIndexReadsBindToCapturedHandleAcrossReload is the F2 captured-handle
// race test (reuses F1's reload-loop harness shape). N borrowers each capture a
// handle under s.mu, build a hot index KEYED OFF that captured handle, and read
// through the index while a reloader repoints lr.handle in a tight loop. Under
// -race it asserts: the captured handle is never munmapped while its borrow is
// held (reads bind to the captured handle, not a live lr.handle re-deref), the
// index answers correctly, and every retired mapping is unmapped exactly once.
func TestHotIndexReadsBindToCapturedHandleAcrossReload(t *testing.T) {
	doc := sampleDoc()
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
				// Build the hot index keyed off the captured handle and read
				// through it. The view source is the stable Doc; the load-bearing
				// assertion is that the index's handle stays mapped for the read.
				hi := b.buildHotIndex("r", docEntityViewSource{doc: b.Group.Repos["r"].Doc})
				h := hi.Handle()
				v, ok := hi.entityByID("r::pkg.Foo")
				if !ok || v.Name() != "Foo" {
					readFault.Add(1)
				}
				if rc, isRC := h.closer.(*refCheckCloser); isRC && rc.n.Load() > 0 {
					// The captured handle was munmapped while we still hold the
					// borrow and read through its index — a use-after-unmap.
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
		t.Fatalf("read faults (captured handle munmapped under borrow, or wrong result): %d", f)
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
