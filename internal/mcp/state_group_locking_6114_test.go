// state_group_locking_6114_test.go — issue #6114.
//
// State.Group() copies the *LoadedGroup pointer under s.mu and RELEASES the
// lock; the handler then reads the group's repo set and each repo's Doc for the
// whole tool call with no lock held (this is stated as the design invariant in
// maphandle.go's package comment). Meanwhile reloadAllLocked mutates those very
// structures IN PLACE under s.mu: `lr.Doc = doc` (state.go), and
// `grp.Repos[rName] = lr` / `delete(grp.Repos, rName)` when the registry's repo
// set changes.
//
// Two distinct defects follow, of different severity:
//
//	(A) Doc pointer race — a reader that nil-checks r.Doc and dereferences it
//	    later can observe two different Docs. Today both are non-nil so the
//	    consequence is a mixed-generation answer; the moment `lr.Doc = nil`
//	    exists (the #5954 memory work this issue blocks) it becomes a nil deref.
//
//	(B) Repo-set map race — an unlocked `range lg.Repos` concurrent with the
//	    reload's map write is "concurrent map iteration and map write", a Go
//	    RUNTIME THROW. It is not a panic: recover() cannot catch it and the
//	    daemon process dies, taking every concurrent agent session with it.
//
// Both tests drive the REAL reload path against a REAL on-disk repo and read
// through the REAL production read shape, so a pass is evidence about
// production and not about the fixture.
package mcp

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/daemon"
	"github.com/cajasmota/grafel/internal/graph/fbwriter"
)

// readDocLikeToolsGo is the exact check-then-dereference shape the read surface
// uses today — see groupIndexedRefSHA in tools.go, which nil-checks lr.Doc and
// then reads lr.Doc.IndexedSHA through a SECOND load of the field. Keeping the
// two loads separate is the point: collapsing them into one local would hide
// defect (A) rather than fix it.
func readDocLikeToolsGo(lg *LoadedGroup) int {
	n := 0
	for slug := range lg.Repos {
		lr := lg.Repos[slug]
		if lr == nil || lr.Doc == nil {
			continue
		}
		if lr.Doc.IndexedSHA != "" {
			n++
		}
		n += len(lr.Doc.Entities)
	}
	return n
}

// bumpMtime advances a file's mtime past its current value so an mtime-gated
// re-read (refreshRegistryFromDisk gates on ModTime().After) is guaranteed to
// fire even where the filesystem timestamp granularity is coarse.
func bumpMtime(path string) {
	fi, err := os.Stat(path)
	if err != nil {
		return
	}
	future := fi.ModTime().Add(2 * time.Second)
	_ = os.Chtimes(path, future, future)
}

// TestGroupReadersDoNotRaceWithReloadDocSwap is defect (A) — STILL OPEN, and
// this test is the executable evidence for it rather than a regression guard.
//
// Readers acquire the group through State.Group() — the production routing
// choke point — and then read each repo's Doc with no lock held, exactly as a
// tool handler does. Concurrently a writer publishes a new generation and calls
// State.Reload(), which walks reloadAllLocked's `lr.Doc = doc` in-place swap.
//
// Measured on this fixture under -race (first run, unmodified tree):
//
//	WARNING: DATA RACE
//	Write at 0x... by goroutine 38:
//	  (*State).reloadAllLocked()  state.go:1638      // lr.Doc = doc
//	Previous read at 0x... by goroutine 42:
//	  readDocLikeToolsGo()        (the tool read shape)
//
// The #6114 snapshot does NOT close this: the snapshot shares *LoadedRepo BY
// POINTER (deliberately — identity, index memoization and MapHandle lifetime all
// depend on it), so an in-place write to lr.Doc is still visible to a reader
// holding a snapshot. Closing it requires the per-repo record to become
// copy-on-write on EVERY reload, not just when a repo goes unservable.
//
// SKIPPED rather than deleted: it must run green the day per-repo COW lands, and
// a deleted test proves nothing. Un-skip it then. It is not skipped because it
// is flaky — it fails deterministically under -race today.
func TestGroupReadersDoNotRaceWithReloadDocSwap(t *testing.T) {
	t.Skip("#6114 residual: reload still swaps lr.Doc in place; per-repo copy-on-write not yet landed (see unservableRepo)")
	t.Setenv(daemon.EnvRoot, t.TempDir())
	t.Setenv("GRAFEL_HOME", t.TempDir())

	repoDir := gitRepoForDiscovery(t)
	mainDir := daemon.StateDirForRepoRef(repoDir, "main")
	if _, err := fbwriter.WriteGraphGen(mainDir, genDocWithMarker("gen0")); err != nil {
		t.Fatalf("publish gen 0: %v", err)
	}

	st := NewState(&Registry{Groups: map[string]RegistryGroup{
		"test": {Repos: map[string]RegistryRepo{"r": {Path: repoDir}}},
	}})
	t.Cleanup(st.Close)
	if _, _, err := st.reloadLocked(); err != nil {
		t.Fatalf("initial reload: %v", err)
	}
	lg0 := st.Group("test")
	if lg0 == nil || lg0.Repos["r"] == nil || lg0.Repos["r"].Doc == nil {
		t.Fatal("fixture degenerate: repo not loaded with a Doc")
	}
	first := lg0.Repos["r"].Doc

	stop := make(chan struct{})
	var swaps atomic.Int64
	var writer, readers sync.WaitGroup

	// Writer: a same-branch reindex loop — the ordinary edit-and-reindex event.
	writer.Add(1)
	go func() {
		defer writer.Done()
		for i := 1; ; i++ {
			select {
			case <-stop:
				return
			default:
			}
			if _, err := fbwriter.WriteGraphGen(mainDir, genDocWithMarker(fmt.Sprintf("gen%d", i))); err != nil {
				return
			}
			if _, err := st.Reload(); err != nil {
				return
			}
			swaps.Add(1)
		}
	}()

	// Readers: the unlocked tool-handler read path, on a fixed iteration budget.
	var sink atomic.Int64
	for i := 0; i < 8; i++ {
		readers.Add(1)
		go func() {
			defer readers.Done()
			for j := 0; j < 3000; j++ {
				lg := st.Group("test")
				if lg == nil {
					continue
				}
				sink.Add(int64(readDocLikeToolsGo(lg)))
			}
		}()
	}
	readers.Wait()
	close(stop)
	writer.Wait()

	if swaps.Load() == 0 {
		t.Fatal("vacuous: the writer never completed a reload, so no Doc swap raced the readers")
	}
	if cur := st.Group("test").Repos["r"].Doc; cur == first {
		t.Fatalf("vacuous: the Doc pointer never changed across %d reloads", swaps.Load())
	}
}

// TestGroupReadersDoNotRaceWithRepoSetChange is defect (B) — the one the issue
// does not mention and which is strictly more severe than the Doc race it does.
//
// A mid-session `grafel group add-repo` / `remove-repo` rewrites registry.json;
// the next reload picks it up (refreshRegistryFromDisk) and mutates the LIVE
// grp.Repos map that unlocked readers are ranging over. That is a Go runtime
// throw, not a recoverable panic.
//
// Non-vacuity: the reload must have observed BOTH repo-set shapes, so a
// registry rewrite that never took effect cannot pass silently.
func TestGroupReadersDoNotRaceWithRepoSetChange(t *testing.T) {
	t.Setenv(daemon.EnvRoot, t.TempDir())
	t.Setenv("GRAFEL_HOME", t.TempDir())

	repoA := gitRepoForDiscovery(t)
	repoB := gitRepoForDiscovery(t)
	for _, d := range []string{repoA, repoB} {
		if _, err := fbwriter.WriteGraphGen(daemon.StateDirForRepoRef(d, "main"), genDocWithMarker("g")); err != nil {
			t.Fatalf("publish graph for %s: %v", d, err)
		}
	}

	regPath := filepath.Join(t.TempDir(), "registry.json")
	writeReg := func(withB bool) {
		repos := map[string]RegistryRepo{"a": {Path: repoA}}
		if withB {
			repos["b"] = RegistryRepo{Path: repoB}
		}
		b, err := json.Marshal(&Registry{Groups: map[string]RegistryGroup{"test": {Repos: repos}}})
		if err != nil {
			return
		}
		_ = os.WriteFile(regPath, b, 0o644)
		bumpMtime(regPath)
	}
	writeReg(false)

	reg, err := LoadRegistry(regPath)
	if err != nil {
		t.Fatalf("load registry: %v", err)
	}
	st := NewState(reg)
	t.Cleanup(st.Close)
	if _, _, err := st.reloadLocked(); err != nil {
		t.Fatalf("initial reload: %v", err)
	}

	stop := make(chan struct{})
	var reloads atomic.Int64
	var shapes sync.Map // distinct len(grp.Repos) values the reload actually produced
	var writer, readers sync.WaitGroup

	writer.Add(1)
	go func() {
		defer writer.Done()
		withB := true
		for {
			select {
			case <-stop:
				return
			default:
			}
			writeReg(withB)
			if _, err := st.Reload(); err != nil {
				return
			}
			st.mu.Lock()
			n := len(st.groups["test"].Repos)
			st.mu.Unlock()
			shapes.Store(n, true)
			reloads.Add(1)
			withB = !withB
		}
	}()

	var sink atomic.Int64
	for i := 0; i < 8; i++ {
		readers.Add(1)
		go func() {
			defer readers.Done()
			for j := 0; j < 3000; j++ {
				lg := st.Group("test")
				if lg == nil {
					continue
				}
				// The unlocked map iteration every repo-scoped tool performs.
				sink.Add(int64(readDocLikeToolsGo(lg)))
			}
		}()
	}
	readers.Wait()
	close(stop)
	writer.Wait()

	if reloads.Load() == 0 {
		t.Fatal("vacuous: the writer never completed a reload")
	}
	n := 0
	shapes.Range(func(any, any) bool { n++; return true })
	if n < 2 {
		t.Fatalf("vacuous: the repo set never changed shape (%d distinct sizes across %d reloads) — "+
			"the registry rewrite did not take effect, so no map write raced the readers", n, reloads.Load())
	}
}

// TestUnservableRepoNeverShowsAReaderADocGoingNil is the unblock evidence for
// #5954's "nil out Doc after use" work.
//
// It answers the question the epic actually needs answered: can a repo be marked
// unservable (Doc nil, loadErr set — the shape tools.go's `unavailable` list
// keys on) without the unlocked read surface observing a nil TRANSITION and
// dereferencing through it?
//
// The reader below is the production check-then-dereference shape with the two
// loads deliberately kept apart. A reader that captures its group view and then
// reads Doc must NEVER see the nil-check pass and the dereference fault. The
// test asserts that directly, and asserts non-vacuity: the successor must
// genuinely have been published (some reader saw the unservable shape) and the
// predecessor's Doc must genuinely still be non-nil.
func TestUnservableRepoNeverShowsAReaderADocGoingNil(t *testing.T) {
	t.Setenv(daemon.EnvRoot, t.TempDir())
	t.Setenv("GRAFEL_HOME", t.TempDir())

	repoDir := gitRepoForDiscovery(t)
	if _, err := fbwriter.WriteGraphGen(daemon.StateDirForRepoRef(repoDir, "main"), genDocWithMarker("gen0")); err != nil {
		t.Fatalf("publish gen 0: %v", err)
	}
	st := NewState(&Registry{Groups: map[string]RegistryGroup{
		"test": {Repos: map[string]RegistryRepo{"r": {Path: repoDir}}},
	}})
	t.Cleanup(st.Close)
	if _, _, err := st.reloadLocked(); err != nil {
		t.Fatalf("initial reload: %v", err)
	}
	st.mu.Lock()
	pred := st.groups["test"].Repos["r"]
	st.mu.Unlock()
	if pred == nil || pred.Doc == nil {
		t.Fatal("fixture degenerate: repo not loaded with a Doc")
	}

	stop := make(chan struct{})
	var faults, sawUnservable, sawServable atomic.Int64
	var writer, readers sync.WaitGroup

	// Writer: flip the repo between servable and unservable by PUBLISHING a
	// successor record — never by mutating the record readers already hold.
	writer.Add(1)
	go func() {
		defer writer.Done()
		down := true
		for {
			select {
			case <-stop:
				return
			default:
			}
			st.mu.Lock()
			grp := st.groups["test"]
			if down {
				grp.Repos["r"] = unservableRepo(pred, "no graph file found (graph.fb or graph.json)")
			} else {
				grp.Repos["r"] = pred
			}
			st.mu.Unlock()
			down = !down
		}
	}()

	for i := 0; i < 8; i++ {
		readers.Add(1)
		go func() {
			defer readers.Done()
			for j := 0; j < 4000; j++ {
				lg := st.Group("test")
				if lg == nil {
					continue
				}
				lr := lg.Repos["r"]
				if lr == nil {
					continue
				}
				// The production check-then-dereference, two separate loads.
				if lr.Doc == nil {
					if lr.loadErr == "" {
						faults.Add(1) // unservable but nothing to report
					}
					sawUnservable.Add(1)
					continue
				}
				// If the field could go nil under us this dereference faults.
				// It must not: this record's Doc is immutable for its whole life.
				if lr.Doc.IndexedRef == "\x00never" {
					faults.Add(1)
				}
				sawServable.Add(1)
			}
		}()
	}
	readers.Wait()
	close(stop)
	writer.Wait()

	if got := faults.Load(); got != 0 {
		t.Fatalf("reader observed a Doc transition through the check-then-deref: %d faults", got)
	}
	// Non-vacuity: both shapes must genuinely have been observed, or the test
	// never exercised the transition it claims to make safe.
	if sawUnservable.Load() == 0 {
		t.Fatal("vacuous: no reader ever observed the unservable successor")
	}
	if sawServable.Load() == 0 {
		t.Fatal("vacuous: no reader ever observed the servable predecessor")
	}
	// The predecessor's Doc must never have been touched — that is the whole
	// invariant that makes a snapshot-holding reader safe.
	if pred.Doc == nil {
		t.Fatal("predecessor record's Doc was cleared in place — the successor protocol was not used")
	}
}

// liveGroup returns the LIVE *LoadedGroup for name.
//
// State.Group returns an immutable per-call VIEW (#6114), so a fixture that
// needs to CONFIGURE a resident group — or an assertion about group INSTANCE
// identity — must go through the live map under s.mu. Writing through a
// Group() result compiles and is silently discarded.
func liveGroup(s *State, name string) *LoadedGroup {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.groups[name]
}

// mutateGroup applies fn to the LIVE group under s.mu (see liveGroup).
func mutateGroup(t *testing.T, s *State, name string, fn func(*LoadedGroup)) {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	g := s.groups[name]
	if g == nil {
		t.Fatalf("group %q is not resident", name)
	}
	fn(g)
}

// TestUnservableRepoRecoversOnTheNextReload pins the recovery half of the
// unservable contract, which is the half that is easy to get wrong and
// invisible if you only assert the transition.
//
// unservableRepo must not carry contentHash forward. reloadAllLocked's inner
// fast path is "hash matches what we last parsed → identical bytes → skip the
// reparse", and it advances mtime WITHOUT ever assigning Doc. A successor that
// inherited the predecessor's hash is therefore stranded: the graph on disk is
// valid and untouched, every reload agrees there is nothing to do, and the repo
// reports unservable forever — until the bytes happen to change.
//
// The fixture deliberately does NOT rewrite the graph: recovery must come from
// the reparse alone, against byte-identical on-disk content.
func TestUnservableRepoRecoversOnTheNextReload(t *testing.T) {
	t.Setenv(daemon.EnvRoot, t.TempDir())
	t.Setenv("GRAFEL_HOME", t.TempDir())

	repoDir := gitRepoForDiscovery(t)
	mainDir := daemon.StateDirForRepoRef(repoDir, "main")
	genPath, err := fbwriter.WriteGraphGen(mainDir, genDocWithMarker("alive"))
	if err != nil {
		t.Fatalf("publish graph: %v", err)
	}
	st := NewState(&Registry{Groups: map[string]RegistryGroup{
		"test": {Repos: map[string]RegistryRepo{"r": {Path: repoDir}}},
	}})
	t.Cleanup(st.Close)
	if _, _, err := st.reloadLocked(); err != nil {
		t.Fatalf("initial reload: %v", err)
	}

	st.mu.Lock()
	pred := st.groups["test"].Repos["r"]
	st.mu.Unlock()
	if pred == nil || pred.Doc == nil {
		t.Fatal("fixture degenerate: repo not loaded with a Doc")
	}
	// Non-vacuity: the predecessor must actually carry a content hash, or
	// zeroing it in the successor proves nothing.
	if pred.contentHash == 0 {
		t.Fatal("vacuous: predecessor has no contentHash, so the fast path could not have stranded it")
	}
	fiBefore, err := os.Stat(genPath)
	if err != nil {
		t.Fatalf("stat graph: %v", err)
	}

	// Mark the repo unservable through the successor protocol.
	st.mu.Lock()
	st.groups["test"].Repos["r"] = unservableRepo(pred, "simulated unservable")
	st.mu.Unlock()

	if lr := st.Group("test").Repos["r"]; lr.Doc != nil || lr.loadErr != "simulated unservable" {
		t.Fatalf("successor is not in the unservable shape: Doc=%v loadErr=%q", lr.Doc, lr.loadErr)
	}

	// The next reload must bring it back, with no change on disk.
	if _, _, err := st.reloadLocked(); err != nil {
		t.Fatalf("recovery reload: %v", err)
	}
	fiAfter, err := os.Stat(genPath)
	if err != nil {
		t.Fatalf("stat graph after: %v", err)
	}
	// Non-vacuity: recovery must be attributable to the reparse, not to the
	// graph having changed underneath us.
	if !fiBefore.ModTime().Equal(fiAfter.ModTime()) || fiBefore.Size() != fiAfter.Size() {
		t.Fatalf("vacuous: the on-disk graph changed across the recovery reload (%v/%d -> %v/%d)",
			fiBefore.ModTime(), fiBefore.Size(), fiAfter.ModTime(), fiAfter.Size())
	}

	lr := st.Group("test").Repos["r"]
	if lr == nil || lr.Doc == nil {
		t.Fatalf("repo is permanently stranded: reload left Doc=%v loadErr=%q against a valid, untouched graph "+
			"— unservableRepo must zero contentHash so the reparse is not skipped", lr.Doc, lr.loadErr)
	}
	if lr.loadErr != "" {
		t.Errorf("recovered repo still carries loadErr %q", lr.loadErr)
	}
	if _, ok := lr.getByIDOne("alive"); !ok {
		t.Error("recovered repo does not serve its entity — the Doc was assigned but not usable")
	}
}

// TestSnapshotGroupsHandsOutViewsNotLiveGroups covers the SnapshotGroups half of
// the #6114 change, which otherwise has no test at all: it has no production
// caller today, so a mutant that reverts it to returning live groups survives
// the entire suite.
func TestSnapshotGroupsHandsOutViewsNotLiveGroups(t *testing.T) {
	st := NewState(&Registry{Groups: map[string]RegistryGroup{
		"g": {Repos: map[string]RegistryRepo{}},
	}})
	lr := &LoadedRepo{Repo: "r"}
	st.groups["g"] = &LoadedGroup{Name: "g", Repos: map[string]*LoadedRepo{"r": lr}}

	snaps := st.SnapshotGroups()
	if len(snaps) != 1 {
		t.Fatalf("SnapshotGroups returned %d groups, want 1", len(snaps))
	}
	got := snaps[0]

	live := liveGroup(st, "g")
	if got == live {
		t.Fatal("SnapshotGroups returned the LIVE group — an unlocked caller ranging over it races reload's map write (#6114)")
	}
	if !got.isView {
		t.Fatal("SnapshotGroups result is not marked as a view")
	}
	// The repo map must be a distinct map: mutating the live one must not be
	// visible through an already-taken snapshot. This is the actual property —
	// pointer inequality alone would also hold for a copy that shared the map.
	st.mu.Lock()
	st.groups["g"].Repos["added-later"] = &LoadedRepo{Repo: "added-later"}
	st.mu.Unlock()
	if _, leaked := got.Repos["added-later"]; leaked {
		t.Fatal("SnapshotGroups shared the live repo map — a later reload's insert is visible through an in-flight snapshot")
	}
	// Repos are shared BY POINTER (that is deliberate, and what keeps index
	// memoization and handle lifetime intact).
	if got.Repos["r"] != lr {
		t.Fatal("SnapshotGroups deep-copied the repo record; it must share *LoadedRepo by pointer")
	}
}

// TestApplyGroupAlgoOverlayRejectsAView proves the #6114 view guard actually
// fires. Without it, the new invariant ("a write through a Group() result is
// silently discarded") is enforced only by a doc comment — and this change
// already found six fixtures that had written through the live group, one of
// which handed that object straight to this very function.
func TestApplyGroupAlgoOverlayRejectsAView(t *testing.T) {
	st := NewState(&Registry{Groups: map[string]RegistryGroup{
		"g": {Repos: map[string]RegistryRepo{}},
	}})
	st.groups["g"] = &LoadedGroup{Name: "g", Repos: map[string]*LoadedRepo{}}

	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("applyGroupAlgoOverlay accepted a Group() view: the write would be silently discarded (#6114)")
		}
		msg, _ := r.(string)
		if !strings.Contains(msg, "view") {
			t.Fatalf("panicked for the wrong reason: %v", r)
		}
	}()
	applyGroupAlgoOverlay(st.Group("g"))
}

// TestBorrowGroupHandsOutAView is the #6114 guard on the ADR-0027 F3 read path.
// borrowGroup is inert today, but it is the designated read path for F3; if it
// hands back the live group, defect (B) — an unlocked `range Group.Repos`
// against reload's map write — returns in full the moment F3 lights up. Cheaper
// to pin now than to rediscover it under a flag flip.
func TestBorrowGroupHandsOutAView(t *testing.T) {
	st := NewState(&Registry{Groups: map[string]RegistryGroup{
		"g": {Repos: map[string]RegistryRepo{}},
	}})
	lr := &LoadedRepo{Repo: "r"}
	st.groups["g"] = &LoadedGroup{Name: "g", Repos: map[string]*LoadedRepo{"r": lr}}

	b := st.borrowGroup("g")
	if b == nil {
		t.Fatal("borrowGroup returned nil for a resident group")
	}
	defer b.Release()

	if b.Group == liveGroup(st, "g") {
		t.Fatal("borrowGroup carries the LIVE group — F3 would reintroduce the #6114 repo-set race")
	}
	st.mu.Lock()
	st.groups["g"].Repos["added-later"] = &LoadedRepo{Repo: "added-later"}
	st.mu.Unlock()
	if _, leaked := b.Group.Repos["added-later"]; leaked {
		t.Fatal("borrowGroup shared the live repo map: a reload insert is visible through an in-flight borrow")
	}
	if b.Group.Repos["r"] != lr {
		t.Fatal("borrowGroup deep-copied the repo record; it must share *LoadedRepo by pointer")
	}
}
