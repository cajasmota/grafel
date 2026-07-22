// segment_serve_5912h_test.go — #5912 (h): the HARD FLIP GATE. The zero-copy
// mmap serve path must serve a SEGMENT-SET (graph.<gen>/ dir of N seg-NNNN.fb +
// manifest.json) DIRECTLY from fbreader.MultiReader, instead of collapsing it
// into a materialized graph.Document. Until this lands a large segment-set graph
// regresses read-RSS (the whole v0.1.9 mmap win) because the reload open branch
// left newRdr nil for a no-.fb-ext gen dir → Doc collapse.
//
// These tests drive the real reloadLocked open branch over an on-disk segment
// set and assert:
//   - lr.Reader is a *fbreader.MultiReader (served from segments, NOT Doc-collapsed);
//   - entity-by-id, by-kind scan, BM25 search, and adjacency all resolve correctly
//     across segments, INCLUDING a cross-segment edge (rel in seg A → id in seg B);
//   - the single-file path is byte-identical (served via the same ReaderForDir seam);
//   - reload/retire munmaps all N segment mmaps as ONE atomic drain, race-free.
package mcp

import (
	"fmt"
	"os"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/cajasmota/grafel/internal/daemon"
	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/graph/descriptions"
	"github.com/cajasmota/grafel/internal/graph/fbreader"
	"github.com/cajasmota/grafel/internal/graph/fbwriter"
	"github.com/cajasmota/grafel/internal/graph/groupalgo"
)

// segFixtureDoc builds a graph with n entities (zero-padded sortable ids, each
// padded so a small threshold forces MANY segments) and a CALLS chain
// ent[i]->ent[i+1] — so at least one relationship crosses a segment boundary.
func segFixtureDoc(repo string, n int) *graph.Document {
	doc := &graph.Document{Version: 1, Repo: repo}
	doc.Entities = make([]graph.Entity, 0, n)
	for i := 0; i < n; i++ {
		e := graph.Entity{
			ID:            fmt.Sprintf("ent-%08d", i),
			Name:          fmt.Sprintf("sym_%08d", i),
			QualifiedName: fmt.Sprintf("%s.sym_%08d", repo, i),
			Kind:          "function",
			SourceFile:    fmt.Sprintf("pkg/file_%04d.go", i),
			Language:      "go",
			StartLine:     1 + i,
		}
		e.PropSet("visibility", "public")
		e.PropSet("padding", fmt.Sprintf("payload-%08d-xxxxxxxxxxxxxxxxxxxxxxxxxxxx", i))
		doc.Entities = append(doc.Entities, e)
	}
	for i := 0; i+1 < n; i++ {
		doc.Relationships = append(doc.Relationships, graph.Relationship{
			ID:     fmt.Sprintf("rel-%08d", i),
			FromID: fmt.Sprintf("ent-%08d", i),
			ToID:   fmt.Sprintf("ent-%08d", i+1),
			Kind:   "CALLS",
		})
	}
	return doc
}

// writeSegmentSet writes doc as a segment-set (forced via a tiny threshold) into
// the daemon state dir for a fresh temp repo and returns (repoDir, stateDir,
// genDir, descriptor). It fails the test if the fixture did NOT split into a
// multi-segment set (the whole point of the test).
func writeSegmentSet(t *testing.T, doc *graph.Document) (repoDir, stateDir, genDir string, desc graph.GraphDescriptor) {
	t.Helper()
	t.Setenv("GRAFEL_STREAM_SEGMENTS", "1")
	t.Setenv("GRAFEL_SEGMENT_BYTES", "65536") // 64 KB — forces many segments
	repoDir = t.TempDir()
	stateDir = daemon.StateDirForRepo(repoDir)
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatalf("mkdir state dir: %v", err)
	}
	gp, err := fbwriter.WriteGraphGen(stateDir, doc)
	if err != nil {
		t.Fatalf("WriteGraphGen: %v", err)
	}
	genDir = gp
	desc, err = graph.CurrentGraphDescriptor(stateDir)
	if err != nil {
		t.Fatalf("CurrentGraphDescriptor: %v", err)
	}
	if desc.Kind != graph.GraphSegmentSet {
		t.Fatalf("fixture did not produce a segment-set (kind=%v) — raise entity count / lower threshold", desc.Kind)
	}
	if len(desc.Manifest.Segments) < 3 {
		t.Fatalf("fixture produced only %d segments; need multiple entity segments for a cross-segment edge", len(desc.Manifest.Segments))
	}
	return repoDir, stateDir, genDir, desc
}

// reloadSegmentRepo builds a State whose sole repo points at a segment-set gen
// dir, runs the real reloadLocked, and returns the resident LoadedRepo. The flag
// is forced ON so the mmap serve path is exercised.
func reloadSegmentRepo(t *testing.T, doc *graph.Document) (*State, *LoadedRepo, graph.GraphDescriptor) {
	t.Helper()
	forceServeFromMMap(t, true)
	repoDir, _, genDir, desc := writeSegmentSet(t, doc)

	reg := &Registry{Groups: map[string]RegistryGroup{
		"test": {Repos: map[string]RegistryRepo{"r": {Path: repoDir}}},
	}}
	st := NewState(reg)
	// Pre-seed GraphFile = the gen dir so reload's short-circuit stats it directly
	// (no git subprocesses); Doc nil + zero mtime forces the reparse+open branch.
	lr := &LoadedRepo{Repo: "r", Path: repoDir, GraphFile: genDir}
	st.mu.Lock()
	st.groups["test"] = &LoadedGroup{Name: "test", Repos: map[string]*LoadedRepo{"r": lr}}
	st.mu.Unlock()
	t.Cleanup(st.Close)

	if _, _, err := st.reloadLocked(); err != nil {
		t.Fatalf("reloadLocked: %v", err)
	}
	return st, lr, desc
}

// TestSegmentSet_ServedFromMultiReaderNotDocCollapsed is THE flip-gate test.
func TestSegmentSet_ServedFromMultiReaderNotDocCollapsed(t *testing.T) {
	const n = 600
	doc := segFixtureDoc("r", n)
	_, lr, desc := reloadSegmentRepo(t, doc)

	// (1) Served from a MultiReader — NOT the newRdr-nil Doc-collapse branch.
	if lr.Reader == nil {
		t.Fatal("lr.Reader is nil — segment-set was Doc-collapsed (the RSS regression this gate blocks)")
	}
	mr, ok := lr.Reader.(*fbreader.MultiReader)
	if !ok {
		t.Fatalf("lr.Reader is %T, want *fbreader.MultiReader (segment-set must serve from segments)", lr.Reader)
	}
	if mr.SegmentCount() < 2 {
		t.Fatalf("MultiReader has %d segments; want the multi-segment set", mr.SegmentCount())
	}
	if lr.handle == nil || lr.handle.Reader() != lr.Reader {
		t.Fatal("handle invariant broken: handle==nil or handle.Reader() != lr.Reader")
	}
	if got := lr.Reader.EntityCount(); got != n {
		t.Fatalf("EntityCount across segments = %d, want %d", got, n)
	}

	// (2) entity-by-id resolves across segments via the reader-backed LabelIndex.
	for _, i := range []int{0, 1, n / 2, n - 2, n - 1} {
		id := fmt.Sprintf("ent-%08d", i)
		e := lr.LabelIndex.ByID(id)
		if e == nil || e.ID != id || e.QualifiedName != fmt.Sprintf("r.sym_%08d", i) {
			t.Fatalf("LabelIndex.ByID(%s) = %#v; want id/qname for entity %d (cross-segment resolve failed)", id, e, i)
		}
	}

	// (3) by-kind scan: every entity is "function" and the flag-ON scan visits all.
	kindCount := 0
	lr.forEachEntityOfKinds(func(k string) bool { return k == "function" }, func(e *graph.Entity) bool {
		kindCount++
		return true
	})
	if kindCount != n {
		t.Fatalf("by-kind scan visited %d entities, want %d", kindCount, n)
	}
	if fk := lr.Reader.FilterEntitiesByKind("function"); len(fk) != n {
		t.Fatalf("Reader.FilterEntitiesByKind(function) = %d, want %d", len(fk), n)
	}

	// (4) BM25 search resolves a specific symbol off the segment-backed index.
	hits := lr.getBM25().Search("sym_00000042", 5)
	foundBM := false
	for _, h := range hits {
		if h.Entity != nil && h.Entity.ID == "ent-00000042" {
			foundBM = true
		}
	}
	if !foundBM {
		t.Fatalf("BM25 search over segment-set did not resolve ent-00000042; hits=%d", len(hits))
	}

	// (5) adjacency + CROSS-SEGMENT edge. The first entity segment's MaxKey entity
	// has a CALLS edge to the first entity of the next segment (ids are the
	// contiguous sorted chain), so from and to live in DIFFERENT segments.
	fromID, toID := crossSegmentEdge(t, desc)
	adj := lr.getAdjacency()
	if adj == nil {
		t.Fatal("getAdjacency returned nil")
	}
	// The reader-sourced adjacency must be byte-identical to a direct
	// reader-sourced build (proves it is served from the MultiReader, not the Doc).
	if !reflect.DeepEqual(adj, buildAdjacencyFromReader(lr.Reader, "r")) {
		t.Fatal("getAdjacency (flag-on) != buildAdjacencyFromReader — not served from the segment reader")
	}
	if !hasOutNeighbor(adj, fromID, toID) {
		t.Fatalf("cross-segment edge %s->%s missing from out-adjacency", fromID, toID)
	}
	// The cross-segment endpoint resolves by id through the MultiReader (seg B).
	if e := lr.Reader.LookupEntityByID(toID); e == nil || string(e.Id()) != toID {
		t.Fatalf("cross-segment LookupEntityByID(%s) failed — segment routing broken", toID)
	}
}

// crossSegmentEdge returns a (fromID,toID) CALLS edge whose endpoints are in two
// DIFFERENT entity segments: the last id of entity-segment 0 and its chain
// successor (the first id of entity-segment 1).
func crossSegmentEdge(t *testing.T, desc graph.GraphDescriptor) (fromID, toID string) {
	t.Helper()
	var firstEntSeg *graph.SegmentMeta
	for i := range desc.Manifest.Segments {
		s := &desc.Manifest.Segments[i]
		if s.Kind == graph.SegmentEntities && s.EntityCount > 0 {
			firstEntSeg = s
			break
		}
	}
	if firstEntSeg == nil {
		t.Fatal("no entity segment in manifest")
	}
	// MaxKey is ent-XXXXXXXX; its chain successor is ent-(XXXXXXXX+1), which — being
	// the next id in sorted order past this segment's max — lives in the next segment.
	var idx int
	if _, err := fmt.Sscanf(firstEntSeg.MaxKey, "ent-%08d", &idx); err != nil {
		t.Fatalf("parse MaxKey %q: %v", firstEntSeg.MaxKey, err)
	}
	return fmt.Sprintf("ent-%08d", idx), fmt.Sprintf("ent-%08d", idx+1)
}

func hasOutNeighbor(adj *adjacency, fromID, toID string) bool {
	for _, e := range adj.Outgoing(fromID) {
		if e.target == toID {
			return true
		}
	}
	return false
}

// TestSingleFile_ServedViaReaderForDirParity_5912h pins the non-negotiable
// single-file parity: a single-file repo, served through the SAME ReaderForDir
// open seam, resolves to a *fbreader.Reader (NOT a MultiReader) and its reads
// are byte-identical to a direct reader-sourced build — the byte-for-byte
// preservation of the single-file mmap serve path.
func TestSingleFile_ServedViaReaderForDirParity_5912h(t *testing.T) {
	forceServeFromMMap(t, true)
	t.Setenv("GRAFEL_STREAM_SEGMENTS", "0") // force the flat single-file writer
	const n = 40
	doc := segFixtureDoc("s", n)

	repoDir := t.TempDir()
	stateDir := daemon.StateDirForRepo(repoDir)
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatalf("mkdir state dir: %v", err)
	}
	gp, err := fbwriter.WriteGraphGen(stateDir, doc)
	if err != nil {
		t.Fatalf("WriteGraphGen: %v", err)
	}
	desc, err := graph.CurrentGraphDescriptor(stateDir)
	if err != nil {
		t.Fatalf("CurrentGraphDescriptor: %v", err)
	}
	if desc.Kind != graph.GraphSingleFile {
		t.Fatalf("expected GraphSingleFile, got %v", desc.Kind)
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

	if lr.Reader == nil {
		t.Fatal("single-file repo left lr.Reader nil (regressed mmap serve)")
	}
	if _, isMulti := lr.Reader.(*fbreader.MultiReader); isMulti {
		t.Fatal("single-file repo resolved to a *MultiReader — must be a single-file *Reader")
	}
	if _, isSingle := lr.Reader.(*fbreader.Reader); !isSingle {
		t.Fatalf("single-file repo lr.Reader is %T, want *fbreader.Reader", lr.Reader)
	}
	if got := lr.Reader.EntityCount(); got != n {
		t.Fatalf("single-file EntityCount = %d, want %d", got, n)
	}
	for _, i := range []int{0, n / 2, n - 1} {
		id := fmt.Sprintf("ent-%08d", i)
		if e := lr.LabelIndex.ByID(id); e == nil || e.ID != id {
			t.Fatalf("single-file ByID(%s) = %#v", id, e)
		}
	}
	if !reflect.DeepEqual(lr.getAdjacency(), buildAdjacencyFromReader(lr.Reader, "s")) {
		t.Fatal("single-file getAdjacency (flag-on) != reader build — parity broken")
	}
}

// TestSegmentSet_ReloadRetireDrainNoRace_5912h is the concurrency drain proof:
// a reloader repeatedly opens a fresh N-segment MultiReader, publishes it, and
// retires the predecessor (munmapping ALL N segment mmaps as ONE atomic drain)
// while goroutines hammer the flag-ON read choke points. Under -race it must
// finish with NO read-after-munmap SIGBUS/SIGSEGV/race and correct data — the
// whole-MultiReader-as-one-retire-unit invariant.
func TestSegmentSet_ReloadRetireDrainNoRace_5912h(t *testing.T) {
	forceServeFromMMap(t, true)
	const n = 500
	doc := segFixtureDoc("r", n)
	_, stateDir, _, desc := writeSegmentSet(t, doc)
	if len(desc.Segments) < 3 {
		t.Fatalf("need a multi-segment set for the N-segment drain, got %d segments", len(desc.Segments))
	}

	openMulti := func() *fbreader.MultiReader {
		v, err := graph.ReaderForDir(stateDir)
		if err != nil {
			t.Fatalf("ReaderForDir: %v", err)
		}
		mr, ok := v.(*fbreader.MultiReader)
		if !ok {
			t.Fatalf("ReaderForDir returned %T, want *fbreader.MultiReader", v)
		}
		return mr
	}

	s := NewState(&Registry{Groups: map[string]RegistryGroup{"g": {Repos: map[string]RegistryRepo{}}}})
	lr := &LoadedRepo{Repo: "r", Doc: doc}
	s.groups["g"] = &LoadedGroup{Name: "g", Repos: map[string]*LoadedRepo{"r": lr}}

	s.mu.Lock()
	h0 := newMapHandle(openMulti())
	lr.LabelIndex = wireReaderLabelIndex(lr, h0.reader, h0, doc)
	lr.publishHandle(h0)
	s.mu.Unlock()

	stop := make(chan struct{})
	var faults atomic.Int64

	var wgR sync.WaitGroup
	wgR.Add(1)
	go func() {
		defer wgR.Done()
		for i := 0; i < 200; i++ {
			select {
			case <-stop:
				return
			default:
			}
			nh := newMapHandle(openMulti())
			li := wireReaderLabelIndex(lr, nh.reader, nh, doc)
			s.mu.Lock()
			lr.resetIndexes()
			lr.LabelIndex = li
			lr.publishHandle(nh) // retire+munmap all N segments of the predecessor
			s.mu.Unlock()
		}
	}()

	var wgB sync.WaitGroup
	for g := 0; g < 6; g++ {
		wgB.Add(1)
		go func() {
			defer wgB.Done()
			for j := 0; j < 400; j++ {
				s.mu.Lock()
				li := lr.LabelIndex
				s.mu.Unlock()
				// e00000123 lives past the first segment boundary → exercises
				// cross-segment materialization under the concurrent munmap.
				if e := li.ByID("ent-00000123"); e == nil || e.ID != "ent-00000123" {
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
	lr.retireHandle() // munmap the final generation's N segments
	s.mu.Unlock()

	if f := faults.Load(); f != 0 {
		t.Fatalf("choke-point read faults across the segment-set drain: %d", f)
	}
}

// TestSegmentSet_OverlayMergeOverMultiReader_5912h asserts the desc + group-algo
// side-table overlays still merge correctly on the segment-set mmap path — they
// key on the SAME global concat index the MultiReader assigns (EntityAt /
// IterateEntities order), so an overlay entry for an entity in segment B is
// surfaced at that entity's global index just like a single-file graph.
func TestSegmentSet_OverlayMergeOverMultiReader_5912h(t *testing.T) {
	const n = 500
	doc := segFixtureDoc("r", n)
	_, stateDir, _, desc := writeSegmentSet(t, doc)

	v, err := graph.ReaderForDir(stateDir)
	if err != nil {
		t.Fatalf("ReaderForDir: %v", err)
	}
	mr, ok := v.(*fbreader.MultiReader)
	if !ok {
		t.Fatalf("ReaderForDir returned %T", v)
	}
	defer mr.Close()

	// A cross-segment pair: from in segment A, to (its chain successor) in segment
	// B. ids are the dense sorted chain, so the global concat index == the numeric
	// suffix (MultiReader concatenates segments in sorted manifest order).
	fromID, toID := crossSegmentEdge(t, desc)
	var fromIdx, toIdx int
	fmt.Sscanf(fromID, "ent-%08d", &fromIdx)
	fmt.Sscanf(toID, "ent-%08d", &toIdx)
	if toIdx != fromIdx+1 {
		t.Fatalf("cross-segment pair not contiguous: %d,%d", fromIdx, toIdx)
	}

	ov := &groupalgo.Overlay{Results: map[string]groupalgo.EntityOverlay{
		fromID: {CommunityID: 111, Centrality: 0.5, IsGodNode: true},
		toID:   {CommunityID: 222},
	}}
	sc := &descriptions.Sidecar{Results: map[string]string{
		fromID: "desc-from",
		toID:   "desc-to",
	}}

	otab := buildOverlayTableFromReader(mr, ov)
	dtab := buildDescTableFromReader(mr, sc)

	// The side-tables key on the global concat index across segments.
	if eo, has := otab[int32(fromIdx)]; !has || eo.CommunityID == nil || *eo.CommunityID != 111 || !eo.IsGodNode {
		t.Fatalf("overlay table missing/incorrect at cross-segment index %d: %#v (has=%v)", fromIdx, eo, has)
	}
	if dtab[int32(fromIdx)] != "desc-from" || dtab[int32(toIdx)] != "desc-to" {
		t.Fatalf("desc table not keyed on concat index: [%d]=%q [%d]=%q", fromIdx, dtab[int32(fromIdx)], toIdx, dtab[int32(toIdx)])
	}

	// materializeEntityOverlay merges base (mmap) + overlays at the concat index —
	// byte-identical to a single-file at() lookup for the same entity.
	efrom := materializeEntityOverlay(mr, otab, dtab, int32(fromIdx))
	if efrom == nil || efrom.ID != fromID {
		t.Fatalf("materializeEntityOverlay(%d) = %#v, want %s", fromIdx, efrom, fromID)
	}
	if efrom.CommunityID == nil || *efrom.CommunityID != 111 || !efrom.IsGodNode {
		t.Fatalf("overlay not merged onto segment-B entity: %#v", efrom)
	}
	if d := efrom.PropGet("description"); d != "desc-from" {
		t.Fatalf("desc overlay not merged: PropGet(description)=%q, want desc-from", d)
	}
	eto := materializeEntityOverlay(mr, otab, dtab, int32(toIdx))
	if eto == nil || eto.ID != toID || eto.CommunityID == nil || *eto.CommunityID != 222 {
		t.Fatalf("cross-segment successor overlay wrong: %#v", eto)
	}
}
