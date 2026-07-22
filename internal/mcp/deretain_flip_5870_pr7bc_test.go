package mcp

// deretain_flip_5870_pr7bc_test.go — #5870 PR7bc, the deretain flip DROP.
//
// PR7a proved every read primitive serves correctly from the resident mmap
// Reader with an EMPTIED lr.Doc (header-only). PR7bc makes reloadLocked itself do
// the emptying for a reader-present, flag-ON repo (skeletonizeDocRows), so serve
// holds the graph ONCE (mmap) instead of twice (mmap + heap Doc). These tests are
// the moment of truth: the emptying is done by reloadLocked in production, not by
// the test harness.

import (
	"os"
	"reflect"
	"sort"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/cajasmota/grafel/internal/daemon"
	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/graph/fbwriter"
)

// reloadReaderRepoPR7bc writes a real single-file graph.fb, wires a State +
// registry + LoadedRepo exactly like production, and drives st.reloadLocked() —
// the SAME path the daemon takes. Returns the reloaded repo (reader-present,
// flag-ON, so skeletonizeDocRows has already run).
func reloadReaderRepoPR7bc(t *testing.T, doc *graph.Document) (*State, *LoadedRepo) {
	t.Helper()
	t.Setenv("GRAFEL_DAEMON_ROOT", t.TempDir())
	t.Setenv("GRAFEL_STREAM_SEGMENTS", "0") // force the flat single-file writer/reader
	repoDir := t.TempDir()
	stateDir := daemon.StateDirForRepo(repoDir)
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatalf("mkdir state dir: %v", err)
	}
	gp, err := fbwriter.WriteGraphGen(stateDir, doc)
	if err != nil {
		t.Fatalf("WriteGraphGen: %v", err)
	}
	reg := &Registry{Groups: map[string]RegistryGroup{
		"test": {Repos: map[string]RegistryRepo{"r": {Path: repoDir}}},
	}}
	st := NewState(reg)
	lr := &LoadedRepo{Repo: "r", Path: repoDir, GraphFile: gp}
	st.mu.Lock()
	st.groups["test"] = &LoadedGroup{Name: "test", Repos: map[string]*LoadedRepo{"r": lr}}
	st.mu.Unlock()
	t.Cleanup(st.Close)
	if _, _, err := st.reloadLocked(); err != nil {
		t.Fatalf("reloadLocked: %v", err)
	}
	return st, lr
}

// fullDocRefRepo builds a flag-independent reference: a full Document with a
// Doc-sourced LabelIndex (reader==nil), which serves every read from the Doc
// regardless of the serveFromMMap() flag (at()/getBM25/getAdjacency all short-
// circuit to the Doc when reader==nil). This is the oracle the skeletonized,
// reader-sourced subject must match row-for-row.
func fullDocRefRepo(doc *graph.Document) *LoadedRepo {
	ref := &LoadedRepo{Repo: "r", Doc: doc}
	ref.LabelIndex = BuildLabelIndex(doc)
	return ref
}

// TestDeretainFlip_ReaderRepoSkeletonized_PR7bc is the production moment of truth:
// after reloadLocked over a mapped graph.fb with the flag ON, lr.Doc.Entities /
// Relationships are dropped to nil (the flip), the header survives on the still-
// non-nil Doc, and a representative read surface returns CORRECT results sourced
// from the Reader — identical to a full-Doc oracle over the same graph.
func TestDeretainFlip_ReaderRepoSkeletonized_PR7bc(t *testing.T) {
	forceServeFromMMap(t, true)
	const n = 300
	doc := buildSyntheticDoc(n)
	doc.Version = 3
	doc.Repo = "r"

	st, lr := reloadReaderRepoPR7bc(t, doc)
	_ = st

	// (A) The flip actually happened: a resident Reader is present and the Doc row
	// bulk is gone, but the Doc itself stays non-nil.
	if lr.Reader == nil {
		t.Fatal("reader-present repo left lr.Reader nil (no mmap → nothing to skeletonize against)")
	}
	if lr.Doc == nil {
		t.Fatal("lr.Doc went nil — the skeleton MUST keep a non-nil header Document")
	}
	if len(lr.Doc.Entities) != 0 {
		t.Fatalf("lr.Doc.Entities len=%d after reload, want 0 (skeletonized)", len(lr.Doc.Entities))
	}
	if len(lr.Doc.Relationships) != 0 {
		t.Fatalf("lr.Doc.Relationships len=%d after reload, want 0 (skeletonized)", len(lr.Doc.Relationships))
	}
	if len(lr.Doc.Communities) != 0 || len(lr.Doc.SurpriseEdges) != 0 {
		t.Fatalf("lr.Doc row-bulk (Communities/SurpriseEdges) not skeletonized: %d/%d",
			len(lr.Doc.Communities), len(lr.Doc.SurpriseEdges))
	}

	// (B) Header metadata survives on the skeleton (Stats counts are sourced from
	// the graph.fb header; Version/Repo round-trip through the fb).
	if lr.Doc.Stats.Entities != n {
		t.Errorf("skeleton Stats.Entities=%d, want %d (header count must survive)", lr.Doc.Stats.Entities, n)
	}
	if lr.Reader.EntityCount() != n {
		t.Fatalf("reader EntityCount=%d, want %d", lr.Reader.EntityCount(), n)
	}
	if repoIndexedRef(lr) == "" {
		t.Error("repoIndexedRef returned empty on the skeleton (header read regressed)")
	}

	// Oracle: a full-Doc repo over the same graph. Reads run under the flag-ON
	// subject; the ref serves from its Doc (reader==nil) either way.
	ref := fullDocRefRepo(doc)

	// (C1) entity-by-id.
	for _, id := range []string{"ent-000000", "ent-000150", "ent-000299"} {
		gotE, gotOK := lr.getByIDOne(id)
		wantE, wantOK := ref.getByIDOne(id)
		if gotOK != wantOK {
			t.Fatalf("getByIDOne(%q) ok mismatch: subject=%v ref=%v", id, gotOK, wantOK)
		}
		if gotOK && (gotE.ID != wantE.ID || gotE.Name != wantE.Name || gotE.QualifiedName != wantE.QualifiedName ||
			gotE.Kind != wantE.Kind || gotE.SourceFile != wantE.SourceFile) {
			t.Errorf("getByIDOne(%q) mismatch:\n subject=%+v\n ref    =%+v", id, gotE, wantE)
		}
	}
	if _, ok := lr.getByIDOne("nope"); ok {
		t.Error("getByIDOne(missing) returned ok=true on the skeleton")
	}

	// (C2) by-kind scan (the enumerate-by-kind substrate: LabelIndex.byKind → at()).
	for _, kind := range []string{"function", "http_endpoint"} {
		got := sortedIDsForKind(lr.LabelIndex, kind)
		want := sortedIDsForKind(ref.LabelIndex, kind)
		if len(got) == 0 {
			t.Errorf("by-kind scan %q returned 0 ids on the skeleton (fell back to empty Doc?)", kind)
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("by-kind scan %q mismatch:\n subject=%v\n ref    =%v", kind, got, want)
		}
	}

	// (C3) full-set scan (coverage/topology/cycles substrate: forEachEntity).
	gotN := 0
	lr.forEachEntity(func(*graph.Entity) bool { gotN++; return true })
	if gotN != n {
		t.Errorf("forEachEntity counted %d on the skeleton, want %d (Reader-sourced scan)", gotN, n)
	}

	// (C4) BM25 search — same ranked ids, and the index was built FROM the Reader.
	query := "handleOrderRequest customer processor kafka fulfilment pipeline"
	gotHits := hitIDs(lr.getBM25().Search(query, 10))
	wantHits := hitIDs(ref.getBM25().Search(query, 10))
	if len(gotHits) == 0 {
		t.Error("BM25 search returned no hits on the skeleton — index likely built from the empty Doc")
	}
	if !reflect.DeepEqual(gotHits, wantHits) {
		t.Errorf("BM25 search mismatch:\n subject=%v\n ref    =%v", gotHits, wantHits)
	}

	// (C5) adjacency / traces substrate — outgoing neighbor sets must match.
	gotAdj := lr.getAdjacency()
	refAdj := ref.getAdjacency()
	for _, id := range []string{"ent-000000", "ent-000050", "ent-000200"} {
		if !reflect.DeepEqual(sortedEdgeTargets(gotAdj.Outgoing(id)), sortedEdgeTargets(refAdj.Outgoing(id))) {
			t.Errorf("adjacency Outgoing(%q) mismatch on the skeleton", id)
		}
	}

	// (C6) getTopKPageRank — same ordered id list.
	if !reflect.DeepEqual(lr.getTopKPageRank(), ref.getTopKPageRank()) {
		t.Error("getTopKPageRank mismatch on the skeleton")
	}
}

// TestDeretainFlip_JSONOnlyRepoKeepsFullDoc_PR7bc pins the split: a JSON-only
// repo (no graph.fb → nil Reader) is NEVER skeletonized — its Doc is the only
// read source, so reloadLocked must leave it FULL.
func TestDeretainFlip_JSONOnlyRepoKeepsFullDoc_PR7bc(t *testing.T) {
	forceServeFromMMap(t, true) // flag ON, yet a nil-reader repo must keep its Doc
	const n = 120
	doc := buildSyntheticDoc(n)

	t.Setenv("GRAFEL_DAEMON_ROOT", t.TempDir())
	repoDir := t.TempDir()
	jsonPath := writeGraph(t, repoDir, doc) // writes graph.json (NO graph.fb)

	reg := &Registry{Groups: map[string]RegistryGroup{
		"test": {Repos: map[string]RegistryRepo{"r": {Path: repoDir}}},
	}}
	st := NewState(reg)
	lr := &LoadedRepo{Repo: "r", Path: repoDir, GraphFile: jsonPath}
	st.mu.Lock()
	st.groups["test"] = &LoadedGroup{Name: "test", Repos: map[string]*LoadedRepo{"r": lr}}
	st.mu.Unlock()
	t.Cleanup(st.Close)
	if _, _, err := st.reloadLocked(); err != nil {
		t.Fatalf("reloadLocked: %v", err)
	}

	if lr.Reader != nil {
		t.Fatal("JSON-only repo opened a Reader — expected nil (no graph.fb)")
	}
	if lr.Doc == nil || len(lr.Doc.Entities) != n {
		t.Fatalf("JSON-only Doc must stay FULL: Doc=%v Entities=%d, want %d", lr.Doc, len(lr.Doc.Entities), n)
	}
	if len(lr.Doc.Relationships) == 0 {
		t.Fatal("JSON-only repo lost its Relationships (must NOT be skeletonized)")
	}
	// Reads still work, served from the full Doc.
	if e, ok := lr.getByIDOne("ent-000010"); !ok || e.ID != "ent-000010" {
		t.Fatalf("JSON-only getByIDOne(ent-000010) = %#v, ok=%v", e, ok)
	}
}

// TestDeretainFlip_SkeletonizeDocRows_HeaderSurvives is the direct unit contract
// for the helper: row-bulk slices go nil, the Document stays non-nil, and every
// header field is preserved.
func TestDeretainFlip_SkeletonizeDocRows_HeaderSurvives(t *testing.T) {
	pr := 0.5
	doc := &graph.Document{
		Version:        7,
		Repo:           "acme",
		IndexerVersion: "1.2.3",
		Stats:          graph.Stats{Files: 4, Entities: 2, Relationships: 1},
		IndexedRef:     "refs/heads/main",
		IndexedSHA:     "deadbeef",
		IsWorktree:     true,
		CoverageStatus: "partial",
		AlgorithmStats: &graph.AlgorithmStats{},
		Entities:       []graph.Entity{{ID: "a", PageRank: &pr}, {ID: "b"}},
		Relationships:  []graph.Relationship{{FromID: "a", ToID: "b", Kind: "CALLS"}},
		Communities:    []graph.CommunityResult{{}},
		SurpriseEdges:  []graph.SurpriseEdge{{}},
	}
	gen := doc.GeneratedAt

	skeletonizeDocRows(doc)

	if doc == nil {
		t.Fatal("skeletonizeDocRows nil'd the Document itself")
	}
	if doc.Entities != nil || doc.Relationships != nil || doc.Communities != nil || doc.SurpriseEdges != nil {
		t.Fatalf("row-bulk not fully dropped: e=%v r=%v c=%v s=%v",
			doc.Entities, doc.Relationships, doc.Communities, doc.SurpriseEdges)
	}
	if doc.Version != 7 || doc.Repo != "acme" || doc.IndexerVersion != "1.2.3" ||
		doc.IndexedRef != "refs/heads/main" || doc.IndexedSHA != "deadbeef" || !doc.IsWorktree ||
		doc.CoverageStatus != "partial" || doc.AlgorithmStats == nil || !doc.GeneratedAt.Equal(gen) {
		t.Fatalf("header field lost after skeleton: %+v", doc)
	}
	if doc.Stats.Entities != 2 || doc.Stats.Relationships != 1 || doc.Stats.Files != 4 {
		t.Fatalf("Stats lost after skeleton: %+v", doc.Stats)
	}

	// nil-safe.
	skeletonizeDocRows(nil)
}

// TestDeretainFlip_ConcurrentReloadSkeletonRace_PR7bc drives repeated
// reloadLocked (each skeletonizing lr.Doc) while readers hammer the mmap read
// path. Under -race it must finish with no read-after-munmap SIGBUS/race and no
// wrong data.
func TestDeretainFlip_ConcurrentReloadSkeletonRace_PR7bc(t *testing.T) {
	forceServeFromMMap(t, true)
	const n = 200
	doc := buildSyntheticDoc(n)
	st, lr := reloadReaderRepoPR7bc(t, doc)

	if len(lr.Doc.Entities) != 0 {
		t.Fatalf("first reload did not skeletonize: Entities=%d", len(lr.Doc.Entities))
	}

	stop := make(chan struct{})
	var faults atomic.Int64
	var wg sync.WaitGroup

	// Reloader: re-run reloadLocked in a loop. The content-hash skip means the
	// graph.fb bytes are identical so most iterations no-op, but each genuine
	// reload re-opens the reader, rebuilds the LabelIndex, and re-skeletonizes —
	// exercising the publish/retire path under load.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 60; i++ {
			select {
			case <-stop:
				return
			default:
			}
			if _, _, err := st.reloadLocked(); err != nil {
				faults.Add(1)
				return
			}
		}
	}()

	for g := 0; g < 6; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 400; j++ {
				select {
				case <-stop:
					return
				default:
				}
				if e, ok := lr.getByIDOne("ent-000100"); ok && (e == nil || e.ID != "ent-000100") {
					faults.Add(1)
				}
				_ = lr.getBM25().Search("kafka fulfilment", 5)
				_ = lr.getAdjacency()
			}
		}()
	}

	wg.Wait()
	close(stop)

	if f := faults.Load(); f != 0 {
		t.Fatalf("concurrent reload+skeleton produced %d faults (wrong data / reload error)", f)
	}
}

// sortedIDsForKind resolves every vector index registered under kind in the
// LabelIndex to its entity id via at(), returning them sorted. A skeletonized,
// reader-sourced index must resolve these off the Reader.
func sortedIDsForKind(li *LabelIndex, kind string) []string {
	idxs := li.byKind[kind]
	out := make([]string, 0, len(idxs))
	for _, i := range idxs {
		if e := li.at(i); e != nil {
			out = append(out, e.ID)
		}
	}
	sort.Strings(out)
	return out
}
