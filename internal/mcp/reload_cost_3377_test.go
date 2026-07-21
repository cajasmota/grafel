// reload_cost_3377_test.go — tests for the reload-cost fixes (#3377):
//   - BM25 is built LAZILY on first search-tool use (not eagerly per reload)
//   - reload skips the reparse when the graph.fb content hash is unchanged
//
// Companion to lazy_reload_3367_test.go (which covers the traversal indexes).
package mcp

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/daemon"
	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/graph/fbwriter"
)

// TestBM25_LazyBuiltOnSearch asserts BM25 is nil after reset (the reload state)
// and is built only when getBM25() is called — never by the traversal getters.
func TestBM25_LazyBuiltOnSearch(t *testing.T) {
	doc := lazyTestDoc()
	lr := &LoadedRepo{Repo: "r", Doc: doc, LabelIndex: BuildLabelIndex(doc)}
	lr.resetIndexes()

	if lr.BM25 != nil {
		t.Fatal("BM25 built eagerly after resetIndexes — reload regressed")
	}

	// A non-search getter must NOT build BM25.
	_ = lr.getByID()
	_ = lr.getAdjacency()
	if lr.BM25 != nil {
		t.Error("a non-search getter built BM25 — laziness leaked")
	}

	// First search-tool access builds it.
	got := lr.getBM25()
	if got == nil || lr.BM25 == nil {
		t.Fatal("getBM25 did not build the BM25 index")
	}

	// Cached: a second call returns the same instance (built at most once).
	if lr.getBM25() != got {
		t.Error("getBM25 rebuilt on the second call — sync.Once not caching")
	}
}

// TestBM25_LazyMatchesEager confirms the lazily-built BM25 returns the same
// search results as the old eager build for the same Document.
func TestBM25_LazyMatchesEager(t *testing.T) {
	doc := lazyTestDoc()
	lr := &LoadedRepo{Repo: "r", Doc: doc, LabelIndex: BuildLabelIndex(doc)}
	lr.resetIndexes()

	eager := BuildBM25(doc)
	lazy := lr.getBM25()

	for _, q := range []string{"Proc", "A", "B", "Function"} {
		wantHits := eager.Search(q, 10)
		gotHits := lazy.Search(q, 10)
		if len(wantHits) != len(gotHits) {
			t.Fatalf("query %q: lazy %d hits, eager %d", q, len(gotHits), len(wantHits))
		}
		for i := range wantHits {
			gid, wid := "", ""
			if gotHits[i].Entity != nil {
				gid = gotHits[i].Entity.ID
			}
			if wantHits[i].Entity != nil {
				wid = wantHits[i].Entity.ID
			}
			if gid != wid {
				t.Errorf("query %q hit %d: lazy %q, eager %q", q, i, gid, wid)
			}
		}
	}
}

// TestBM25_ResetRearmsAgainstFreshDoc simulates a reload: build BM25, swap Doc,
// resetIndexes, and confirm the next getBM25 reflects the new Document.
func TestBM25_ResetRearmsAgainstFreshDoc(t *testing.T) {
	doc1 := lazyTestDoc()
	lr := &LoadedRepo{Repo: "r", Doc: doc1, LabelIndex: BuildLabelIndex(doc1)}
	lr.resetIndexes()
	if hits := lr.getBM25().Search("Newentity", 10); len(hits) != 0 {
		t.Fatalf("doc1 unexpectedly matched 'Newentity'")
	}

	doc2 := lazyTestDoc()
	doc2.Entities = append(doc2.Entities, graph.Entity{ID: "z", Name: "Newentity", Kind: "Function", SourceFile: "z.go"})
	lr.Doc = doc2
	lr.LabelIndex = BuildLabelIndex(doc2)
	lr.resetIndexes()

	if hits := lr.getBM25().Search("Newentity", 10); len(hits) == 0 {
		t.Error("after reload+reset, BM25 served a stale index (missed the new entity)")
	}
}

// TestBM25_ConcurrentBuildOnce hammers getBM25 from many goroutines; with -race
// this guards the bm25Once + idxMu correctness.
func TestBM25_ConcurrentBuildOnce(t *testing.T) {
	doc := lazyTestDoc()
	lr := &LoadedRepo{Repo: "r", Doc: doc, LabelIndex: BuildLabelIndex(doc)}
	lr.resetIndexes()

	const goroutines = 32
	var wg sync.WaitGroup
	results := make([]*BM25Index, goroutines)
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func(idx int) {
			defer wg.Done()
			results[idx] = lr.getBM25()
		}(i)
	}
	wg.Wait()
	for i := 1; i < goroutines; i++ {
		if results[i] != results[0] {
			t.Fatalf("goroutine %d saw a different BM25 instance — Once raced", i)
		}
	}
}

// --- content-hash skip ---------------------------------------------------

// withParseCounter swaps readDocumentFromDir for a counting wrapper and
// restores it on cleanup. Returns a pointer to the live counter.
func withParseCounter(t *testing.T) *int {
	t.Helper()
	orig := readDocumentFromDir
	var n int
	readDocumentFromDir = func(stateDir string) (*graph.Document, error) {
		n++
		return orig(stateDir)
	}
	t.Cleanup(func() { readDocumentFromDir = orig })
	return &n
}

// seedRepoOnDisk writes doc as a graph.fb under a temp repo dir and returns a
// State with that repo pre-loaded (GraphFile set, content hash primed) so a
// subsequent reloadLocked exercises the mtime/content-hash fast path.
func seedRepoOnDisk(t *testing.T, doc *graph.Document) (*State, *LoadedRepo, string) {
	t.Helper()
	repoDir := t.TempDir()
	// graph.fb must live where reloadLocked reparses from: the daemon's
	// state dir for this repo (readDocumentFromDir(stateDir)). The cheap
	// stat/hash fast-path uses lr.GraphFile, which we also point here.
	stateDir := daemon.StateDirForRepo(repoDir)
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatalf("mkdir state dir: %v", err)
	}
	fbPath := filepath.Join(stateDir, "graph.fb")
	if err := fbwriter.WriteAtomic(fbPath, doc); err != nil {
		t.Fatalf("write fb: %v", err)
	}
	fi, err := os.Stat(fbPath)
	if err != nil {
		t.Fatalf("stat fb: %v", err)
	}
	hash, err := hashGraphFile(fbPath)
	if err != nil {
		t.Fatalf("hash fb: %v", err)
	}

	reg := &Registry{Groups: map[string]RegistryGroup{
		"test": {Repos: map[string]RegistryRepo{"r": {Path: repoDir}}},
	}}
	st := NewState(reg)
	lr := &LoadedRepo{
		Repo:        "r",
		Path:        repoDir,
		GraphFile:   fbPath,
		Doc:         doc,
		LabelIndex:  BuildLabelIndex(doc),
		mtime:       fi.ModTime(),
		contentHash: hash,
	}
	st.mu.Lock()
	st.groups["test"] = &LoadedGroup{Name: "test", Repos: map[string]*LoadedRepo{"r": lr}}
	st.mu.Unlock()
	// A reparse on this State opens an mmap'd graph.fb Reader under repoDir's
	// temp state dir. Release it before TempDir cleanup so Windows RemoveAll
	// can unlink graph.fb (mmap locks the file on Windows; #4285). No-op when
	// no reparse happened (Reader stays nil).
	t.Cleanup(st.Close)
	return st, lr, fbPath
}

// TestContentHashSkip_IdenticalContentNoReparse bumps graph.fb's mtime without
// changing its bytes and asserts reloadLocked does NOT reparse the document.
func TestContentHashSkip_IdenticalContentNoReparse(t *testing.T) {
	doc := lazyTestDoc()
	st, lr, fbPath := seedRepoOnDisk(t, doc)
	parses := withParseCounter(t)

	// Bump mtime into the future without touching the bytes (a no-op reindex
	// / `touch` — the exact churn that produced the ~400ms floor).
	future := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(fbPath, future, future); err != nil {
		t.Fatalf("chtimes: %v", err)
	}

	if _, _, err := st.reloadLocked(); err != nil {
		t.Fatalf("reload: %v", err)
	}
	if *parses != 0 {
		t.Errorf("content-hash skip failed: reparsed %d time(s) for identical bytes", *parses)
	}
	if !lr.mtime.Equal(future) {
		t.Errorf("mtime not advanced on skip: got %v want %v", lr.mtime, future)
	}
	// Doc identity must be preserved (not swapped) on the skip path.
	if lr.Doc != doc {
		t.Error("Doc pointer changed on content-hash skip — unexpected swap")
	}
}

// TestContentHashSkip_ChangedContentReparses confirms a genuine content change
// (different bytes, new mtime) DOES trigger a reparse + index refresh.
func TestContentHashSkip_ChangedContentReparses(t *testing.T) {
	doc := lazyTestDoc()
	st, lr, fbPath := seedRepoOnDisk(t, doc)
	parses := withParseCounter(t)

	// Rewrite graph.fb with an extra entity → different bytes.
	doc2 := lazyTestDoc()
	doc2.Entities = append(doc2.Entities, graph.Entity{ID: "z", Name: "Zeta", Kind: "Function", SourceFile: "z.go"})
	if err := fbwriter.WriteAtomic(fbPath, doc2); err != nil {
		t.Fatalf("rewrite fb: %v", err)
	}
	future := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(fbPath, future, future); err != nil {
		t.Fatalf("chtimes: %v", err)
	}

	if _, _, err := st.reloadLocked(); err != nil {
		t.Fatalf("reload: %v", err)
	}
	if *parses != 1 {
		t.Errorf("changed content: expected exactly 1 reparse, got %d", *parses)
	}
	// The fresh Doc must contain the new entity.
	if !lr.LabelIndex.HasID("z") {
		t.Error("reparse did not pick up the new entity 'z'")
	}
}
