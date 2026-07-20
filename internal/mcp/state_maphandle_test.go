package mcp

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

// newStateWithHandle builds a State with a single group "g" holding one repo
// "r" whose mmap handle wraps c, installed through publishHandle (the real F1
// publish path) so lifetime is driven exactly as production does.
func newStateWithHandle(c readerCloser) (*State, *LoadedRepo) {
	s := NewState(&Registry{Groups: map[string]RegistryGroup{
		"g": {Repos: map[string]RegistryRepo{}}, // empty → reload evicts "r"
	}})
	lr := &LoadedRepo{Repo: "r"}
	s.groups["g"] = &LoadedGroup{
		Name:  "g",
		Repos: map[string]*LoadedRepo{"r": lr},
	}
	s.mu.Lock()
	lr.publishHandle(&MapHandle{closer: c})
	s.mu.Unlock()
	return s, lr
}

// Criterion 4: server Close() routes the munmap through retire()+conditional
// close, NOT a bare Reader.Close(). An in-flight borrow must drain before the
// mapping is unmapped.
func TestCloseDefersUnmapWhileBorrowed(t *testing.T) {
	cc := &countingCloser{}
	s, _ := newStateWithHandle(cc)

	b := s.borrowGroup("g") // borrow the handle (refs=1)
	if b == nil {
		t.Fatal("borrowGroup returned nil")
	}
	s.Close() // must retire, not munmap-in-place, because refs>0
	if got := cc.n.Load(); got != 0 {
		t.Fatalf("Close munmapped while borrowed: count=%d, want 0", got)
	}
	b.Release() // last releaser of the retired handle unmaps it
	if got := cc.n.Load(); got != 1 {
		t.Fatalf("munmap count after Release = %d, want 1", got)
	}
}

// Criterion 4: repo eviction (the reload drop-loop for repos no longer in the
// registry) routes through retireHandle, deferring the munmap under a borrow.
func TestEvictionDefersUnmapWhileBorrowed(t *testing.T) {
	cc := &countingCloser{}
	s, _ := newStateWithHandle(cc)

	b := s.borrowGroup("g") // refs=1 on the repo's handle
	if b == nil {
		t.Fatal("borrowGroup returned nil")
	}
	// Registry group "g" has no repos, so Reload evicts "r".
	if _, err := s.Reload(); err != nil {
		t.Fatalf("reload: %v", err)
	}
	if got := cc.n.Load(); got != 0 {
		t.Fatalf("eviction munmapped while borrowed: count=%d, want 0", got)
	}
	b.Release()
	if got := cc.n.Load(); got != 1 {
		t.Fatalf("munmap count after Release = %d, want 1", got)
	}
}

// Criterion 4 (baseline, no borrow): Close unmaps immediately when nothing is
// borrowed — behavior-identical to the pre-F1 bare Close().
func TestCloseUnmapsImmediatelyWhenIdle(t *testing.T) {
	cc := &countingCloser{}
	s, _ := newStateWithHandle(cc)
	s.Close()
	if got := cc.n.Load(); got != 1 {
		t.Fatalf("idle Close munmap count = %d, want 1", got)
	}
}

// Regression (deterministic, borrow-path): the stale-refcount TOCTOU in
// release(), forced through the REAL borrowGroup / publishHandle / release path
// (not just the bare MapHandle). A first borrow is released to refs==0 while the
// handle is STILL PUBLISHED; parked in the releaseGap seam, a second borrowGroup
// legally re-borrows the same live handle (refs 0→1) and a reload then retires
// it (sees refs==1 → defers). When the parked releaser resumes, a naive
// `Add(-1)==0 && retired.Load()` release munmaps on its stale n==0 while refs==1
// → use-after-unmap. This FAILS on the buggy release() and PASSES on the
// re-check-after-retired fix — deterministically, at default -cpu, -count=1.
func TestBorrowGroupReleaseDoesNotUnmapOnRefcountRebound(t *testing.T) {
	s := NewState(&Registry{Groups: map[string]RegistryGroup{
		"g": {Repos: map[string]RegistryRepo{}},
	}})
	lr := &LoadedRepo{Repo: "r"}
	s.groups["g"] = &LoadedGroup{Name: "g", Repos: map[string]*LoadedRepo{"r": lr}}

	cc := &refCheckCloser{}
	gapEntered := make(chan struct{})
	gapProceed := make(chan struct{})
	var armed atomic.Bool
	armed.Store(true)
	H := &MapHandle{closer: cc, releaseGap: func() {
		if armed.CompareAndSwap(true, false) {
			close(gapEntered)
			<-gapProceed
		}
	}}
	cc.h = H
	s.mu.Lock()
	lr.publishHandle(H)
	s.mu.Unlock()

	b1 := s.borrowGroup("g") // borrows H (refs=1)
	done := make(chan struct{})
	go func() { defer close(done); b1.Release() }() // H.release(): Add(-1)→0, parks in gap

	<-gapEntered
	// H is still lr.handle (published): a second borrow legally rebounds refs
	// 0→1, then a reload publishes a successor and retires H (defers, refs==1).
	b2 := s.borrowGroup("g")
	s.mu.Lock()
	lr.publishHandle(&MapHandle{closer: &countingCloser{}})
	s.mu.Unlock()
	if got := cc.n.Load(); got != 0 {
		t.Fatalf("reload munmapped H while borrowed: count=%d, want 0", got)
	}
	close(gapProceed)
	<-done

	if bad := cc.badWhileRefs.Load(); bad != 0 {
		t.Fatalf("release munmapped H while refs>0 (rebound TOCTOU): %d faults", bad)
	}
	if got := cc.n.Load(); got != 0 {
		t.Fatalf("H unmapped while still borrowed by b2: count=%d, want 0", got)
	}
	b2.Release() // drains H → unmap exactly once
	if got := cc.n.Load(); got != 1 {
		t.Fatalf("H final munmap count = %d, want 1", got)
	}
	if bad := cc.badWhileRefs.Load(); bad != 0 {
		t.Fatalf("munmap observed refs>0 %d times, want 0", bad)
	}
}

// Criterion 3: read-through-captured-handle. A borrow taken before a reload
// binds to the handle it captured and stays valid (not munmapped) across the
// reload, until the borrower releases. Stress: N borrowers reading through
// their captured handles while a reloader repoints lr.handle in a tight loop.
// Under -race, asserts zero faults (no munmap while borrowed, no double-munmap)
// and zero handle leaks (every retired mapping eventually unmapped exactly once).
func TestBorrowGroupSurvivesReload(t *testing.T) {
	s := NewState(&Registry{Groups: map[string]RegistryGroup{
		"g": {Repos: map[string]RegistryRepo{}},
	}})
	lr := &LoadedRepo{Repo: "r"}
	s.groups["g"] = &LoadedGroup{Name: "g", Repos: map[string]*LoadedRepo{"r": lr}}

	var closersMu sync.Mutex
	var closers []*refCheckCloser
	newHandle := func() *MapHandle {
		c := &refCheckCloser{}
		// releaseGap = runtime.Gosched yields inside release() in the exact window
		// between the refcount decrement and the close decision, widening the
		// stale-refcount rebound window for broad -race / -cpu-sweep coverage.
		// This is the stochastic stress catcher (many handles, N borrowers, tight
		// reload loop); the DETERMINISTIC, default-scheduling guarantee for the
		// rebound TOCTOU lives in TestMapHandleReleaseRefcountReboundDoesNotUnmap
		// and TestBorrowGroupReleaseDoesNotUnmapOnRefcountRebound.
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

	// Reloader: publish-then-retire in a tight loop, always under s.mu (the same
	// lock borrowGroup takes), matching reloadLocked's discipline.
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

	// Borrowers: capture a handle under the borrow, read through the captured
	// reference (never a live lr.handle re-deref), and assert it is not
	// munmapped while the borrow is held.
	var wgB sync.WaitGroup
	for i := 0; i < 8; i++ {
		wgB.Add(1)
		go func() {
			defer wgB.Done()
			for j := 0; j < 3000; j++ {
				b := s.borrowGroup("g")
				if b == nil {
					readFault.Add(1)
					continue
				}
				h := b.Handle("r")
				if h != nil {
					// Dark read: the captured handle must still be mapped. Its
					// counting closer must not have fired while we hold the borrow.
					if rc, ok := h.closer.(*refCheckCloser); ok && rc.n.Load() > 0 {
						readFault.Add(1)
					}
				}
				b.Release()
			}
		}()
	}

	wgB.Wait()
	close(stop)
	wgR.Wait()

	// Retire the final still-published handle so every created mapping is retired
	// and should now be drained+unmapped exactly once.
	s.mu.Lock()
	lr.retireHandle()
	s.mu.Unlock()

	if f := readFault.Load(); f != 0 {
		t.Fatalf("read faults (handle munmapped under an outstanding borrow): %d", f)
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

// Guard (F2-facing): publishHandle must repoint lr.handle to the SUCCESSOR
// before it retires the predecessor. This ordering is what guarantees a fresh
// borrow can never target a retired handle. It is currently REDUNDANT in F1
// (s.mu serializes borrow-vs-publish), but becomes load-bearing in F2 when
// release()/reads move off s.mu — so guard it now. We observe the ordering at
// the exact instant the predecessor is unmapped: lr.handle must already be the
// successor.
func TestPublishHandlePublishesSuccessorBeforeRetiringPredecessor(t *testing.T) {
	lr := &LoadedRepo{Repo: "r"}
	var successor *MapHandle
	closedPredecessor := false
	old := &MapHandle{closer: closerFunc(func() error {
		closedPredecessor = true
		if lr.handle != successor {
			t.Errorf("predecessor unmapped before successor published: lr.handle=%p, want successor %p",
				lr.handle, successor)
		}
		return nil
	})}
	lr.handle = old
	successor = &MapHandle{closer: closerFunc(func() error { return nil })}

	lr.publishHandle(successor) // publishes successor, then retires+closes old (idle → immediate)

	if !closedPredecessor {
		t.Fatal("publishHandle did not retire/close the predecessor")
	}
	if lr.handle != successor {
		t.Fatalf("after publishHandle lr.handle=%p, want successor %p", lr.handle, successor)
	}
}

// Criterion 5: serve (internal/mcp) must never vend/borrow a Reader from the
// unsafe internal/daemon/mcp LRU (graph_cache.go closes without draining refs).
// This is enforced today by the non-import; guard it so a future refactor
// cannot silently re-enter the close-without-drain path through serve's query
// surface. Serve's mmap handles come only from reloadLocked's own fbreader.Open
// under the F1 MapHandle protocol.
func TestServeDoesNotImportUnsafeDaemonMCPLRU(t *testing.T) {
	const forbidden = "github.com/cajasmota/grafel/internal/daemon/mcp"
	fset := token.NewFileSet()
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("no .go files found in package dir")
	}
	for _, f := range files {
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		af, err := parser.ParseFile(fset, f, src, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parse %s: %v", f, err)
		}
		for _, imp := range af.Imports {
			p := strings.Trim(imp.Path.Value, `"`)
			if p == forbidden || strings.HasPrefix(p, forbidden+"/") {
				t.Fatalf("%s imports %q — serve must not source a Reader from the "+
					"unsafe daemon/mcp LRU (close-without-drain); see ADR-0027 invariant", f, p)
			}
		}
	}
}
