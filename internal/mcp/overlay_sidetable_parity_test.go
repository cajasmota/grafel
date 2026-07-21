// overlay_sidetable_parity_test.go — ADR-0027 mmap cutover (overlay side-table,
// precondition for the PR7 loader flip).
//
// The side-table is a resident, entity-INDEX-keyed map holding the 5 group-algo
// overlay fields (CommunityID/PageRank/Centrality/IsGodNode/IsArticulationPt)
// that are NOT authoritative in graph.fb (per-repo Pass-4 was removed, so
// graph.fb's copies are permanent sentinels). LabelIndex.at()/getByID now
// MATERIALIZE the base entity from the mmap Reader (graph.MaterializeEntity) and
// merge the 5 fields from the side-table — removing the read path's dependence
// on the in-place-stamped lr.Doc for VALUES.
//
// This test is the behavior-neutral guard: with a real graph.fb (sentinel algo
// fields) + an applied group-algo overlay, the Reader+side-table materialized
// entity (via LabelIndex.at / getByID) must be byte-equal (reflect.DeepEqual) to
// the overlaid lr.Doc.Entities[idx] — ALL fields, including the 5 overlay fields
// AND non-view/non-overlay fields (StartLine/Subtype/Signature/QualifiedName).
//
// Mutation proof (performed during development, reverted): dropping the
// side-table merge in LabelIndex.at makes the 5 fields regress to the graph.fb
// sentinel, so the DeepEqual-vs-overlaid-Doc assertions FAIL.
package mcp

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/cajasmota/grafel/internal/daemon"
	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/graph/fbwriter"
	"github.com/cajasmota/grafel/internal/graph/groupalgo"
	"github.com/cajasmota/grafel/internal/registry"
	"github.com/cajasmota/grafel/internal/testsupport"
)

// richFixtureDoc builds a one-repo graph whose entities carry DISTINCT
// non-view/non-overlay fields (StartLine/Subtype/Signature/QualifiedName) so the
// parity assertion exercises fields beyond the 8 view fields + 5 overlay fields.
// The algo fields are left at the sentinel (nil / -2 community) so anything that
// shows up on the read path came from the overlay side-table, not graph.fb.
func richFixtureDoc(slug string, names ...string) *graph.Document {
	doc := &graph.Document{Version: 1, Repo: slug}
	for i, n := range names {
		doc.Entities = append(doc.Entities, graph.Entity{
			ID:            slug + ":" + n,
			Name:          n,
			QualifiedName: slug + "." + n,
			Kind:          "function",
			Subtype:       "handler",
			SourceFile:    slug + "/" + n + ".go",
			StartLine:     10 + i*7,
			Language:      "go",
			Signature:     "func " + n + "(ctx context.Context) error",
		})
	}
	return doc
}

// setupSideTableParity writes a single on-disk graph.fb repo with rich entities,
// registers the group, and returns the State + resolved overlay path + source
// mtimes + the three entity ids.
// forceServeFromMMap overrides the package-cached GRAFEL_SERVE_FROM_MMAP flag for
// the duration of a test and restores it. serveFromMMapEnabled is captured once
// at package load, so t.Setenv is too late — the cached var must be set directly.
// Safe under -race: callers are NOT t.Parallel(), so they run to completion
// (restoring the flag) before any parallel test that reads the flag is resumed.
func forceServeFromMMap(t *testing.T, v bool) {
	t.Helper()
	prev := serveFromMMapEnabled
	serveFromMMapEnabled = v
	t.Cleanup(func() { serveFromMMapEnabled = prev })
}

func setupSideTableParity(t *testing.T) (st *State, overlayPath string, cur map[string]int64, ids [3]string) {
	t.Helper()
	testsupport.IsolateHome(t)
	root := t.TempDir()
	home := filepath.Join(root, "home")
	t.Setenv("GRAFEL_HOME", home)
	t.Setenv("GRAFEL_DAEMON_ROOT", filepath.Join(root, "daemon"))

	pathA := filepath.Join(root, "svc")
	doc := richFixtureDoc("svc", "Alpha", "Bravo", "Charlie")

	stateDir := daemon.StateDirForRepo(pathA)
	if err := fbwriter.WriteAtomic(filepath.Join(stateDir, "graph.fb"), doc); err != nil {
		t.Fatalf("write graph.fb: %v", err)
	}

	cfgPath, err := registry.ConfigPathFor("acme")
	if err != nil {
		t.Fatalf("config path: %v", err)
	}
	cfg := &registry.GroupConfig{Name: "acme", Repos: []registry.Repo{{Slug: "svc", Path: pathA}}}
	if err := registry.SaveGroupConfig(cfgPath, cfg); err != nil {
		t.Fatalf("save group config: %v", err)
	}
	if err := registry.AddGroup("acme", cfgPath); err != nil {
		t.Fatalf("add group: %v", err)
	}

	inMem := &Registry{
		Path: filepath.Join(home, "registry.json"),
		Groups: map[string]RegistryGroup{
			"acme": {Repos: map[string]RegistryRepo{"svc": {Path: pathA}}},
		},
	}
	st = NewState(inMem)
	t.Cleanup(st.Close)

	overlayPath, err = groupalgo.OverlayPath("acme")
	if err != nil {
		t.Fatalf("overlay path: %v", err)
	}
	cur, err = groupalgo.CurrentSourceMtimes("acme")
	if err != nil {
		t.Fatalf("current mtimes: %v", err)
	}
	return st, overlayPath, cur, [3]string{"svc:Alpha", "svc:Bravo", "svc:Charlie"}
}

// TestOverlaySideTable_ReaderMaterializeByteEqualsOverlaidDoc is the mandatory
// overlay-aware parity guard. It applies an overlay setting DISTINCT
// CommunityID/PageRank/Centrality/god/articulation on two of three entities
// (the third is left un-overlaid to prove the "no overlay entry → graph.fb
// values survive" path), then asserts the Reader+side-table materialized entity
// equals the overlaid Doc row for EVERY entity, via BOTH LabelIndex.at and
// getByID.
func TestOverlaySideTable_ReaderMaterializeByteEqualsOverlaidDoc(t *testing.T) {
	forceServeFromMMap(t, true) // exercise the flip path (default is OFF)
	st, overlayPath, cur, ids := setupSideTableParity(t)

	alpha, bravo := ids[0], ids[1] // charlie (ids[2]) deliberately un-overlaid
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
	if lr == nil || lr.Doc == nil {
		t.Fatalf("svc repo not loaded")
	}
	if lr.Reader == nil {
		t.Fatalf("svc repo has nil Reader — the parity test requires the mmap read path")
	}
	if lr.LabelIndex == nil {
		t.Fatalf("svc repo has nil LabelIndex")
	}

	// Ground truth: the overlaid live Doc rows (what today's queries return).
	docByID := map[string]*graph.Entity{}
	for i := range lr.Doc.Entities {
		e := lr.Doc.Entities[i]
		docByID[e.ID] = &e
	}

	byIDMap := lr.getByID()

	for _, id := range ids {
		idx, ok := lr.LabelIndex.byID[id]
		if !ok {
			t.Fatalf("id %s not in LabelIndex", id)
		}
		want := docByID[id]
		if want == nil {
			t.Fatalf("id %s not in Doc", id)
		}

		// Non-view/non-overlay fields must survive the Reader materialization
		// (proves MaterializeEntity carries them, not just the empty default).
		gotAt := lr.LabelIndex.at(idx)
		if gotAt == nil {
			t.Fatalf("at(%d) returned nil for %s", idx, id)
		}
		if gotAt.StartLine != want.StartLine || gotAt.StartLine == 0 {
			t.Fatalf("%s StartLine: got %d want %d (must be non-zero)", id, gotAt.StartLine, want.StartLine)
		}
		if gotAt.Subtype != "handler" || gotAt.Signature == "" || gotAt.QualifiedName == "" {
			t.Fatalf("%s non-view fields dropped by Reader materialize: subtype=%q sig=%q qname=%q",
				id, gotAt.Subtype, gotAt.Signature, gotAt.QualifiedName)
		}

		// Full byte-equality: Reader+side-table == overlaid Doc row.
		if !reflect.DeepEqual(gotAt, want) {
			t.Fatalf("LabelIndex.at parity mismatch for %s:\n got=%#v\nwant=%#v", id, gotAt, want)
		}
		if got := byIDMap[id]; !reflect.DeepEqual(got, want) {
			t.Fatalf("getByID parity mismatch for %s:\n got=%#v\nwant=%#v", id, got, want)
		}
	}

	// Explicit spot-checks on the 5 overlay fields sourced from the side-table.
	if a := byIDMap[alpha]; a == nil || a.CommunityID == nil || *a.CommunityID != 39 ||
		a.PageRank == nil || *a.PageRank != 0.0065 || a.Centrality == nil || *a.Centrality != 10415.99 ||
		!a.IsGodNode || a.IsArticulationPt {
		t.Fatalf("alpha overlay fields not sourced from side-table: %#v", a)
	}
	if b := byIDMap[bravo]; b == nil || b.CommunityID == nil || *b.CommunityID != 80 ||
		!b.IsArticulationPt || b.IsGodNode {
		t.Fatalf("bravo overlay fields not sourced from side-table: %#v", b)
	}
	// Charlie has no overlay entry: the 5 fields must equal the graph.fb sentinel
	// (nil pointers / false flags), identical to the un-stamped Doc row.
	if c := byIDMap[ids[2]]; c == nil || c.CommunityID != nil || c.PageRank != nil ||
		c.Centrality != nil || c.IsGodNode || c.IsArticulationPt {
		t.Fatalf("charlie (un-overlaid) leaked overlay values: %#v", c)
	}
}

// TestOverlaySideTable_FlagOffBuildsNoSideTableAndReadsDoc is the safety guard
// for the DEFAULT (GRAFEL_SERVE_FROM_MMAP OFF) path: applyGroupAlgoOverlay must
// NOT build the side-table (no resident cost pre-flip) and the read path must
// source the overlay values from the in-place-stamped Doc — never the mmap
// Reader — so there is no handler-path mmap read to race a reload's munmap.
func TestOverlaySideTable_FlagOffBuildsNoSideTableAndReadsDoc(t *testing.T) {
	forceServeFromMMap(t, false) // the production default
	st, overlayPath, cur, ids := setupSideTableParity(t)

	alpha := ids[0]
	ov := &groupalgo.Overlay{
		Group:        "acme",
		SourceMtimes: cur,
		Results: map[string]groupalgo.EntityOverlay{
			alpha: {CommunityID: 39, PageRank: 0.0065, Centrality: 10415.99, IsGodNode: true},
		},
		Communities: []graph.CommunityResult{{ID: 39, Size: 1, AutoName: "alpha-cluster"}},
	}
	if err := groupalgo.WriteOverlayTo(overlayPath, ov); err != nil {
		t.Fatalf("write overlay: %v", err)
	}
	if _, err := st.Reload(); err != nil {
		t.Fatalf("reload: %v", err)
	}

	grp := st.Group("acme")
	lr := grp.Repos["svc"]
	if lr == nil || lr.Doc == nil || lr.LabelIndex == nil {
		t.Fatalf("svc repo not loaded")
	}

	// No side-table built on the flag-off path → no resident memory paid.
	if lr.LabelIndex.overlay != nil {
		t.Fatalf("flag-off built a side-table (len=%d); it must stay nil pre-flip", len(lr.LabelIndex.overlay))
	}

	// Values still surface — sourced from the in-place-stamped Doc, not the Reader.
	if a := lr.LabelIndex.ByID(alpha); a == nil || a.CommunityID == nil || *a.CommunityID != 39 ||
		a.PageRank == nil || *a.PageRank != 0.0065 || !a.IsGodNode {
		t.Fatalf("flag-off ByID did not surface the overlaid Doc values: %#v", a)
	}
	// The Doc-sourced result must byte-equal the overlaid Doc row.
	for i := range lr.Doc.Entities {
		want := lr.Doc.Entities[i]
		got := lr.LabelIndex.ByID(want.ID)
		if !reflect.DeepEqual(got, &want) {
			t.Fatalf("flag-off ByID(%s) != overlaid Doc row:\n got=%#v\nwant=%#v", want.ID, got, &want)
		}
	}
}
