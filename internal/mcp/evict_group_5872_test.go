// evict_group_5872_test.go — tests for the per-group whole-graph eviction
// PRIMITIVE (memory epic #5850, issue #5872 PR1): State.EvictGroup + the
// reload cold-gate. No policy/LRU/knobs are exercised here — only the
// primitive's swap-out + retireHandle + keepReader + cold-gate + Close drain.
package mcp

import (
	"sort"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/graph/groupalgo"
)

// entityIDs returns the sorted entity-ID set a repo currently resolves, used to
// prove an evict-then-revive round-trip reconstructs the identical graph.
func entityIDs(lr *LoadedRepo) []string {
	ids := make([]string, 0)
	for id := range lr.getByID() {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// groupResident reports whether name is currently in s.groups (i.e. NOT evicted),
// read under s.mu so it never races an eviction.
func (s *State) groupResident(name string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.groups[name]
	return ok
}

func (s *State) gated(name string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.evicted[name]
	return ok
}

// Criterion 1 + 5: EvictGroup(g,false) removes the group from s.groups (heap
// released to GC), and the next State.Group(g) re-loads it FROM DISK with the
// identical entity set and working search.
func TestEvictGroup_FullEvictThenAccessRematerializes(t *testing.T) {
	doc := lazyTestDoc()
	st, lr, _ := seedRepoOnDisk(t, doc)

	before := entityIDs(lr)
	if len(before) == 0 {
		t.Fatal("fixture has no entities")
	}

	if !st.EvictGroup("test", false) {
		t.Fatal("EvictGroup returned false for a resident group")
	}
	// Criterion 5: the group is gone from the resident registry (heap dropped).
	if st.groupResident("test") {
		t.Fatal("group still resident in s.groups after full evict")
	}
	if !st.gated("test") {
		t.Fatal("group not recorded in the cold-gate after evict")
	}

	// Criterion 1: an explicit access re-materializes the group from disk.
	grp := st.Group("test")
	if grp == nil {
		t.Fatal("Group returned nil after evict — revive failed")
	}
	if st.gated("test") {
		t.Fatal("cold-gate not cleared after revive")
	}
	lr2 := grp.Repos["r"]
	if lr2 == nil {
		t.Fatal("revived group has no repo r")
	}
	after := entityIDs(lr2)
	if len(after) != len(before) {
		t.Fatalf("entity count changed across evict/revive: before=%d after=%d", len(before), len(after))
	}
	for i := range before {
		if before[i] != after[i] {
			t.Fatalf("entity set changed across evict/revive at %d: %q vs %q", i, before[i], after[i])
		}
	}
	// Search must work against the freshly re-materialized index.
	if hits := lr2.getBM25().Search("Proc", 10); len(hits) == 0 {
		t.Error("BM25 search returned no hits after revive — index not rebuilt")
	}
}

// Criterion 2: after an evict, a reload that is NOT targeting the group does NOT
// resurrect it — it stays evicted until explicitly accessed.
func TestEvictGroup_ReloadDoesNotResurrect(t *testing.T) {
	doc := lazyTestDoc()
	st, _, _ := seedRepoOnDisk(t, doc)

	if !st.EvictGroup("test", false) {
		t.Fatal("EvictGroup returned false")
	}

	// A plain reload (the per-call reloadBeforeCall hook drives exactly this) must
	// leave the evicted group cold — no eager resurrection.
	if _, _, err := st.reloadLocked(); err != nil {
		t.Fatalf("reload: %v", err)
	}
	if st.groupResident("test") {
		t.Fatal("reload eagerly resurrected an evicted group")
	}
	if !st.gated("test") {
		t.Fatal("reload cleared the cold-gate without an explicit access")
	}

	// Only an explicit access revives it.
	if grp := st.Group("test"); grp == nil {
		t.Fatal("explicit access did not revive the evicted group")
	}
}

// Criterion 3: keepReader=true keeps the mmap RESIDENT (handle not retired,
// no munmap); keepReader=false munmaps exactly once. Uses fake counting closers
// so the retire behavior is asserted without a real mapping.
func TestEvictGroup_KeepReaderControlsMunmap(t *testing.T) {
	newState := func(cc readerCloser) (*State, *LoadedRepo) {
		s := NewState(&Registry{Groups: map[string]RegistryGroup{
			"g": {Repos: map[string]RegistryRepo{"r": {}}},
		}})
		lr := &LoadedRepo{Repo: "r"}
		s.groups["g"] = &LoadedGroup{Name: "g", Repos: map[string]*LoadedRepo{"r": lr}}
		s.mu.Lock()
		lr.publishHandle(&MapHandle{closer: cc})
		s.mu.Unlock()
		return s, lr
	}

	t.Run("keepReader keeps it mapped", func(t *testing.T) {
		cc := &countingCloser{}
		s, _ := newState(cc)
		if !s.EvictGroup("g", true) {
			t.Fatal("EvictGroup(keepReader) returned false")
		}
		if got := cc.n.Load(); got != 0 {
			t.Fatalf("keepReader munmapped the reader: count=%d, want 0", got)
		}
		// The mapping lives on the cold shell, not retired.
		s.mu.Lock()
		shell := s.evicted["g"]
		s.mu.Unlock()
		if shell == nil {
			t.Fatal("keepReader did not retain a cold shell")
		}
		lrShell := shell.Repos["r"]
		if lrShell == nil || lrShell.handle == nil {
			t.Fatal("cold shell dropped the handle")
		}
		if lrShell.handle.readRetired {
			t.Error("cold shell handle marked readRetired under keepReader")
		}
	})

	t.Run("full evict munmaps once", func(t *testing.T) {
		cc := &countingCloser{}
		s, _ := newState(cc)
		if !s.EvictGroup("g", false) {
			t.Fatal("EvictGroup returned false")
		}
		if got := cc.n.Load(); got != 1 {
			t.Fatalf("full evict munmap count = %d, want 1", got)
		}
		s.mu.Lock()
		shell := s.evicted["g"]
		_, gated := s.evicted["g"]
		s.mu.Unlock()
		if !gated {
			t.Fatal("full evict did not record the cold-gate")
		}
		if shell != nil {
			t.Fatal("full evict retained a shell; want nil (fully cold)")
		}
	})
}

// Criterion 3 (real reader): keepReader keeps the SAME *fbreader.Reader mapped and
// revive re-materializes the LabelIndex from it — no fbreader.Open, no disk read.
func TestEvictGroup_KeepReaderSkipsReopen(t *testing.T) {
	doc := lazyTestDoc()
	st, lr, _ := seedRepoOnDisk(t, doc)

	// Force a reparse so reloadLocked opens a real mmap Reader (seedRepoOnDisk
	// primes mtime==file mtime, which the reload skips).
	st.mu.Lock()
	lr.mtime = time.Time{}
	lr.contentHash = 0 // force a real reparse (bypass the no-op content-hash skip)
	_, _, err := st.reloadAllLocked()
	st.mu.Unlock()
	if err != nil {
		t.Fatalf("reload to open reader: %v", err)
	}
	readerPtr := lr.Reader
	if readerPtr == nil {
		t.Fatal("expected a resident mmap Reader after reparse")
	}

	if !st.EvictGroup("test", true) {
		t.Fatal("EvictGroup(keepReader) returned false")
	}
	grp := st.Group("test")
	if grp == nil {
		t.Fatal("revive returned nil")
	}
	lr2 := grp.Repos["r"]
	if lr2.Reader != readerPtr {
		t.Errorf("keepReader revive re-Opened the reader: got %p want same %p (no re-Open expected)", lr2.Reader, readerPtr)
	}
	if lr2.LabelIndex == nil || !lr2.LabelIndex.HasID("a") {
		t.Error("LabelIndex not re-materialized from the retained reader on revive")
	}
}

// Criterion 4: evict under concurrent read — a reader hammering the group while
// another goroutine evicts+revives it must not SIGBUS / nil-deref. Run under
// -race. The in-flight reader holds its own repo pointer and finishes safely.
func TestEvictGroup_ConcurrentReadNoFault(t *testing.T) {
	doc := lazyTestDoc()
	st, lr, _ := seedRepoOnDisk(t, doc)
	// Open a real reader once so the read path exercises the mmap fallback under
	// retire (Doc fallback on the flag-off default path).
	st.mu.Lock()
	lr.mtime = time.Time{}
	lr.contentHash = 0
	_, _, _ = st.reloadAllLocked()
	st.mu.Unlock()

	var faults atomic.Int64
	stop := make(chan struct{})
	var wg sync.WaitGroup

	// Readers: capture the group, then read derived state lock-free (exactly the
	// production seam). A revive may have swapped the group out from under a prior
	// capture; the captured pointers must still resolve without a fault.
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				grp := st.Group("test")
				if grp == nil {
					continue // transiently mid-evict; revive on the next spin
				}
				r := grp.Repos["r"]
				if r == nil {
					faults.Add(1)
					continue
				}
				_ = r.getByID()
				_ = r.getBM25().Search("A", 5)
				_ = r.getAdjacency()
			}
		}()
	}

	// Evictor: alternate full and keepReader evictions, reviving via Group.
	go func() {
		defer close(stop)
		for i := 0; i < 400; i++ {
			st.EvictGroup("test", i%2 == 0)
			st.Group("test") // revive
		}
	}()

	wg.Wait()
	if f := faults.Load(); f != 0 {
		t.Fatalf("concurrent evict/read produced %d faults", f)
	}
}

// TestEvictGroup_ConcurrentReadNoFault_FlagOn is the GRAFEL_SERVE_FROM_MMAP=ON
// twin of TestEvictGroup_ConcurrentReadNoFault (issue #5872, epic #5850). Flag-ON
// the read choke points DEREFERENCE the mmap under readerMu (not the GC-safe Doc),
// so the keepReader eviction handoff must keep every *LoadedRepo generation that
// shares one *MapHandle serialized on ONE readerMu. Before the fix, coldShellRepo
// gave the shell its OWN independent readerMu while sharing the old lr's handle:
// a later retire of that shared mapping munmapped under the shell's readerMu while
// an in-flight OLD-generation reader still dereferenced it under the old lr's
// readerMu → data race on MapHandle.readRetired + SIGSEGV on the munmapped region.
//
// The flag is forced ON explicitly so the guarantee is proven regardless of the
// package default. Run with -race -count=10 to exercise the handoff window.
//
// MUTATION ORACLE: revert coldShellRepo to a per-shell independent readerMu (drop
// the sharedReaderMu handoff) → this test data-races / SIGSEGVs under -race.
func TestEvictGroup_ConcurrentReadNoFault_FlagOn(t *testing.T) {
	forceServeFromMMap(t, true)
	doc := lazyTestDoc()
	st, lr, _ := seedRepoOnDisk(t, doc)
	// Force a reparse so the flag-ON path opens a real mmap Reader and wires a
	// reader-sourced LabelIndex (at()/getBM25/getAdjacency then deref the mapping
	// under readerMu, exercising the reads-vs-retire serialization).
	st.mu.Lock()
	lr.mtime = time.Time{}
	lr.contentHash = 0
	_, _, _ = st.reloadAllLocked()
	st.mu.Unlock()

	var faults atomic.Int64
	stop := make(chan struct{})
	var wg sync.WaitGroup

	// Readers: capture the group, then read derived state lock-free (exactly the
	// production seam). Flag-ON these getters materialize entities out of the mmap
	// via the readerMu-guarded LabelIndex.at, so a mishandled retire faults here.
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				grp := st.Group("test")
				if grp == nil {
					continue // transiently mid-evict; revive on the next spin
				}
				r := grp.Repos["r"]
				if r == nil {
					faults.Add(1)
					continue
				}
				_ = r.getByID()
				_ = r.getBM25().Search("A", 5)
				_ = r.getAdjacency()
			}
		}()
	}

	// Evictor: alternate full and keepReader evictions, reviving via Group. The
	// keepReader path is the one that hands the shared *MapHandle to a cold shell.
	go func() {
		defer close(stop)
		for i := 0; i < 400; i++ {
			st.EvictGroup("test", i%2 == 0)
			st.Group("test") // revive
		}
	}()

	wg.Wait()
	if f := faults.Load(); f != 0 {
		t.Fatalf("concurrent flag-on evict/read produced %d faults", f)
	}
}

// Guard: EvictGroup on an unknown group is a no-op returning false, and does not
// create a phantom cold-gate entry.
func TestEvictGroup_UnknownGroupNoop(t *testing.T) {
	st := NewState(&Registry{Groups: map[string]RegistryGroup{}})
	if st.EvictGroup("nope", false) {
		t.Fatal("EvictGroup returned true for an unknown group")
	}
	if st.gated("nope") {
		t.Fatal("EvictGroup gated an unknown group")
	}
}

// Guard: a non-evicted group is completely untouched by EvictGroup activity on a
// DIFFERENT group — no cross-group behavior change (the PR1 invariant).
func TestEvictGroup_LeavesOtherGroupsUntouched(t *testing.T) {
	a := lazyTestDoc()
	a.Repo = "a"
	b := lazyTestDoc()
	b.Repo = "b"
	srv := newTestServer(t, a) // group "test", repo "a"
	st := srv.State
	// Add a second in-memory group "other".
	st.mu.Lock()
	st.groups["other"] = &LoadedGroup{Name: "other", Repos: map[string]*LoadedRepo{
		"b": {Repo: "b", Doc: b, LabelIndex: BuildLabelIndex(b)},
	}}
	st.mu.Unlock()

	otherBefore := st.Group("other")
	if !st.EvictGroup("test", false) {
		t.Fatal("EvictGroup(test) returned false")
	}
	otherAfter := st.Group("other")
	if otherBefore != otherAfter {
		t.Fatal("evicting 'test' swapped the 'other' group pointer")
	}
	if !st.groupResident("other") {
		t.Fatal("evicting 'test' disturbed residency of 'other'")
	}
	if _, ok := otherAfter.Repos["b"]; !ok {
		t.Fatal("evicting 'test' mutated 'other' repos")
	}
}

// TestEvictGroup_CloseDrainsKeptReaderShell proves Close() drains the mmap of a
// kept-reader cold shell — the mapping lives on s.evicted (NOT s.groups), so the
// Close loop over s.groups alone would leak it.
//
// MUTATION ORACLE: delete the `for _, g := range s.evicted { ... }` drain loop in
// State.Close() → this test fails (munmap count 0, leaked mapping).
func TestEvictGroup_CloseDrainsKeptReaderShell(t *testing.T) {
	s := NewState(&Registry{Groups: map[string]RegistryGroup{
		"g": {Repos: map[string]RegistryRepo{"r": {}}},
	}})
	lr := &LoadedRepo{Repo: "r"}
	s.groups["g"] = &LoadedGroup{Name: "g", Repos: map[string]*LoadedRepo{"r": lr}}
	cc := &countingCloser{}
	s.mu.Lock()
	lr.publishHandle(&MapHandle{closer: cc})
	s.mu.Unlock()

	if !s.EvictGroup("g", true) {
		t.Fatal("EvictGroup(keepReader) returned false")
	}
	if got := cc.n.Load(); got != 0 {
		t.Fatalf("keepReader munmapped before Close: count=%d, want 0", got)
	}

	s.Close()
	if got := cc.n.Load(); got != 1 {
		t.Fatalf("Close did not drain the kept-reader cold shell: munmap count=%d, want 1 (leaked mapping)", got)
	}
}

// TestEvictGroup_ReviveKeepsOverlay proves a keepReader evict+revive preserves the
// group-algo overlay. Flag-ON, so the overlay lives ONLY in the LabelIndex
// side-table (dropped with the heap on evict) — the revive must re-apply it.
//
// MUTATION ORACLE: set the cold shell's algoApplied=true in EvictGroup (so revive
// skips refreshGroupAlgoOverlayLocked's re-apply) → this test fails (overlay lost:
// PageRank/CommunityID revert to the graph.fb sentinel).
func TestEvictGroup_ReviveKeepsOverlay(t *testing.T) {
	forceServeFromMMap(t, true)
	st, overlayPath, cur, serviceID, _ := setupApplyGroup(t)

	ov := &groupalgo.Overlay{
		Group:        "acme",
		SourceMtimes: cur,
		Results: map[string]groupalgo.EntityOverlay{
			serviceID: {CommunityID: 42, PageRank: 0.77, Centrality: 0.5, IsGodNode: true},
		},
		Communities: []graph.CommunityResult{{ID: 42, Size: 1, AutoName: "core"}},
	}
	if err := groupalgo.WriteOverlayTo(overlayPath, ov); err != nil {
		t.Fatalf("write overlay: %v", err)
	}
	if _, err := st.Reload(); err != nil {
		t.Fatalf("reload: %v", err)
	}

	// Read the overlaid entity through the flag-ON path (LabelIndex.at merges the
	// side-table); Doc is header-only under flag-ON so entityByID cannot be used.
	svcRepo := func(grp *LoadedGroup) *LoadedRepo {
		for _, lr := range grp.Repos {
			if lr != nil {
				if _, ok := lr.getByIDOne(serviceID); ok {
					return lr
				}
			}
		}
		return nil
	}

	grp := st.Group("acme")
	lr := svcRepo(grp)
	if lr == nil {
		t.Fatalf("%s not resolvable pre-evict", serviceID)
	}
	before, _ := lr.getByIDOne(serviceID)
	if before.PageRank == nil || *before.PageRank != 0.77 || before.CommunityID == nil || *before.CommunityID != 42 {
		t.Fatalf("overlay not applied pre-evict: pr=%v cid=%v", before.PageRank, before.CommunityID)
	}

	if !st.EvictGroup("acme", true) {
		t.Fatal("EvictGroup(keepReader) returned false")
	}
	grp2 := st.Group("acme")
	if grp2 == nil {
		t.Fatal("revive returned nil")
	}
	lr2 := svcRepo(grp2)
	if lr2 == nil {
		t.Fatalf("%s not resolvable post-revive", serviceID)
	}
	after, _ := lr2.getByIDOne(serviceID)
	if after.PageRank == nil || *after.PageRank != 0.77 {
		t.Errorf("overlay PageRank lost across keepReader evict/revive: got %v want 0.77", after.PageRank)
	}
	if after.CommunityID == nil || *after.CommunityID != 42 {
		t.Errorf("overlay CommunityID lost across keepReader evict/revive: got %v want 42", after.CommunityID)
	}
	if !after.IsGodNode {
		t.Error("overlay IsGodNode lost across keepReader evict/revive")
	}
}
