// descriptions_overlay_test.go — DESCRIPTION side-table read-merge (#5904 PR-a).
//
// applyDescriptionOverlay ADDITIVELY stamps <stateDir>/descriptions.json onto a
// loaded group's entities: PropSet("description") onto lr.Doc.Entities (the
// flag-OFF Doc read path) and into LabelIndex.descOverlay for the flag-ON mmap
// read path (materializeFromReader). These tests prove:
//
//   - single-file graph: the description surfaces via PropGet on BOTH the Doc
//     path (flag-OFF) and the mmap materialize-from-reader path (flag-ON);
//   - segment-set graph: the description surfaces on the Doc path (the MCP serve
//     path serves a segment-set from the collapsed Doc — Reader is nil — so the
//     Doc stamp is the operative merge; the mmap segment serving is a later
//     slice of #5890);
//   - ADDITIVE: an entity with NO sidecar entry keeps its baked-in description;
//   - absence/staleness: no sidecar (or a sidecar gone stale after a reindex) is
//     a no-op — the entity keeps whatever graph.fb carried.
package mcp

import (
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/cajasmota/grafel/internal/daemon"
	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/graph/descriptions"
	"github.com/cajasmota/grafel/internal/graph/fbwriter"
	"github.com/cajasmota/grafel/internal/registry"
	"github.com/cajasmota/grafel/internal/testsupport"
)

// newDescState registers a one-repo group "acme" (repo slug "svc") whose graph is
// written by writeGraph into the repo's state dir, and returns the State + the
// repo's state dir.
func newDescState(t *testing.T, writeGraph func(t *testing.T, stateDir string)) (*State, string) {
	t.Helper()
	testsupport.IsolateHome(t)
	root := t.TempDir()
	home := filepath.Join(root, "home")
	t.Setenv("GRAFEL_HOME", home)
	t.Setenv("GRAFEL_DAEMON_ROOT", filepath.Join(root, "daemon"))

	pathA := filepath.Join(root, "svc")
	stateDir := daemon.StateDirForRepo(pathA)
	writeGraph(t, stateDir)

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
	st := NewState(inMem)
	t.Cleanup(st.Close)
	return st, stateDir
}

// descFixtureDoc builds a two-entity graph: "svc:withBaked" carries a baked-in
// description (extractor-native), "svc:noDesc" carries none.
func descFixtureDoc() *graph.Document {
	return &graph.Document{Version: 1, Repo: "svc", Entities: []graph.Entity{
		graph.Entity{ID: "svc:noDesc", Name: "NoDesc", QualifiedName: "svc.NoDesc", Kind: "function"}.
			WithProperties(map[string]string{}),
		graph.Entity{ID: "svc:withBaked", Name: "WithBaked", QualifiedName: "svc.WithBaked", Kind: "function"}.
			WithProperties(map[string]string{"description": "baked-in extractor description"}),
	}}
}

func writeSingleFileFlat(t *testing.T, stateDir string) {
	t.Helper()
	if err := fbwriter.WriteAtomic(filepath.Join(stateDir, "graph.fb"), descFixtureDoc()); err != nil {
		t.Fatalf("write graph.fb: %v", err)
	}
}

// TestDescOverlay_SingleFile_DocPath: flag-OFF (default) — the sidecar
// description surfaces via PropGet on the Doc read path.
func TestDescOverlay_SingleFile_DocPath(t *testing.T) {
	forceServeFromMMap(t, false)
	st, stateDir := newDescState(t, writeSingleFileFlat)
	if err := descriptions.Upsert(stateDir, "svc:noDesc", "written by writeback"); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if _, err := st.Reload(); err != nil {
		t.Fatalf("reload: %v", err)
	}
	lr := st.Group("acme").Repos["svc"]
	if lr == nil || lr.Doc == nil {
		t.Fatal("svc repo not loaded")
	}

	got := lr.LabelIndex.ByID("svc:noDesc")
	if got == nil || got.PropGet("description") != "written by writeback" {
		t.Fatalf("Doc-path description not applied: %#v", got)
	}
	// ADDITIVE: the entity that had a baked-in description but NO sidecar entry
	// keeps its baked-in value (never cleared).
	baked := lr.LabelIndex.ByID("svc:withBaked")
	if baked == nil || baked.PropGet("description") != "baked-in extractor description" {
		t.Fatalf("baked-in description clobbered: %#v", baked)
	}
}

// TestDescOverlay_SingleFile_MmapPath: flag-ON — the sidecar description surfaces
// via the mmap materialize-from-reader path (LabelIndex.at over the resident
// Reader), and the additive/no-clobber guarantee holds there too.
func TestDescOverlay_SingleFile_MmapPath(t *testing.T) {
	forceServeFromMMap(t, true)
	st, stateDir := newDescState(t, writeSingleFileFlat)
	if err := descriptions.Upsert(stateDir, "svc:noDesc", "mmap desc"); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if _, err := st.Reload(); err != nil {
		t.Fatalf("reload: %v", err)
	}
	lr := st.Group("acme").Repos["svc"]
	if lr == nil || lr.Reader == nil || lr.LabelIndex == nil {
		t.Fatal("svc repo requires a resident mmap Reader for this test")
	}

	idx, ok := lr.LabelIndex.byID["svc:noDesc"]
	if !ok {
		t.Fatal("svc:noDesc not in LabelIndex")
	}
	got := lr.LabelIndex.at(idx) // mmap materialize-from-reader path
	if got == nil || got.PropGet("description") != "mmap desc" {
		t.Fatalf("mmap-path description not applied: %#v", got)
	}
	// ADDITIVE on the mmap path: baked-in survives (no sidecar entry).
	bidx := lr.LabelIndex.byID["svc:withBaked"]
	baked := lr.LabelIndex.at(bidx)
	if baked == nil || baked.PropGet("description") != "baked-in extractor description" {
		t.Fatalf("mmap-path clobbered baked-in description: %#v", baked)
	}
}

// TestDescOverlay_Absent_NoClobber: with NO sidecar, entities keep their
// baked-in descriptions and un-described entities stay un-described.
func TestDescOverlay_Absent_NoClobber(t *testing.T) {
	forceServeFromMMap(t, false)
	st, _ := newDescState(t, writeSingleFileFlat)
	if _, err := st.Reload(); err != nil {
		t.Fatalf("reload: %v", err)
	}
	lr := st.Group("acme").Repos["svc"]
	if baked := lr.LabelIndex.ByID("svc:withBaked"); baked == nil ||
		baked.PropGet("description") != "baked-in extractor description" {
		t.Fatalf("absent sidecar clobbered baked-in description: %#v", baked)
	}
	if nd := lr.LabelIndex.ByID("svc:noDesc"); nd == nil || nd.PropGet("description") != "" {
		t.Fatalf("absent sidecar invented a description: %#v", nd)
	}
}

// TestDescOverlay_StaleAfterReindex_NoOp: a sidecar written for an older graph
// generation is stale (source-key mismatch) after a reindex and must NOT be
// applied — the entity keeps whatever the fresh graph.fb carried.
func TestDescOverlay_StaleAfterReindex_NoOp(t *testing.T) {
	forceServeFromMMap(t, false)
	st, stateDir := newDescState(t, writeSingleFileFlat)
	if err := descriptions.Upsert(stateDir, "svc:noDesc", "stale desc"); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	// Simulate a reindex: rewrite graph.fb (a NEW mtime → the flat single-file
	// source key changes → the sidecar goes stale).
	writeSingleFileFlat(t, stateDir)
	if _, err := st.Reload(); err != nil {
		t.Fatalf("reload: %v", err)
	}
	lr := st.Group("acme").Repos["svc"]
	if nd := lr.LabelIndex.ByID("svc:noDesc"); nd == nil || nd.PropGet("description") != "" {
		t.Fatalf("stale sidecar was applied after reindex: %#v", nd)
	}
}

// writeSegmentSetForDir hand-builds a two-segment generation at stateDir.
func writeSegmentSetForDir(t *testing.T, stateDir string) {
	t.Helper()
	gen := uint64(3)
	genDirName := graph.GenDirName(gen)
	genDir := filepath.Join(stateDir, genDirName)
	if err := os.MkdirAll(genDir, 0o755); err != nil {
		t.Fatalf("mkdir gen dir: %v", err)
	}
	docs := []*graph.Document{
		{Repo: "svc", Entities: []graph.Entity{
			graph.Entity{ID: "svc:aaa", Name: "Aaa", QualifiedName: "svc.Aaa", Kind: "function"}.
				WithProperties(map[string]string{}),
		}},
		{Repo: "svc", Entities: []graph.Entity{
			graph.Entity{ID: "svc:bbb", Name: "Bbb", QualifiedName: "svc.Bbb", Kind: "function"}.
				WithProperties(map[string]string{"description": "baked bbb"}),
		}},
	}
	m := &graph.Manifest{FormatVersion: graph.ManifestFormatVersion}
	for i, doc := range docs {
		name := graph.SegmentFileName(i)
		if err := fbwriter.WriteAtomic(filepath.Join(genDir, name), doc); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
		ids := make([]string, 0, len(doc.Entities))
		for _, e := range doc.Entities {
			ids = append(ids, e.ID)
		}
		sort.Strings(ids)
		m.Segments = append(m.Segments, graph.SegmentMeta{
			File:        name,
			Kind:        graph.SegmentEntities,
			EntityCount: len(doc.Entities),
			MinKey:      ids[0],
			MaxKey:      ids[len(ids)-1],
		})
	}
	if err := graph.WriteManifest(genDir, m); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	if err := graph.WriteCurrentPointerRaw(stateDir, genDirName); err != nil {
		t.Fatalf("write current pointer: %v", err)
	}
}

// TestDescOverlay_SegmentSet_DocPath: a segment-set graph (served from the
// collapsed Doc, Reader nil) surfaces the sidecar description on the Doc path,
// and the source key is segment-derived (so the sidecar is correctly matched to
// the segment-set generation, NOT a collapsed single-file path — #5915 P2).
func TestDescOverlay_SegmentSet_DocPath(t *testing.T) {
	forceServeFromMMap(t, false)
	st, stateDir := newDescState(t, writeSegmentSetForDir)

	// The sidecar source key must be segment-derived.
	if key := descriptions.CurrentSourceKey(stateDir); len(key) < 4 || key[:4] != "seg:" {
		t.Fatalf("segment-set source key = %q, want seg: prefix", key)
	}
	if err := descriptions.Upsert(stateDir, "svc:aaa", "seg desc for aaa"); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if _, err := st.Reload(); err != nil {
		t.Fatalf("reload: %v", err)
	}
	lr := st.Group("acme").Repos["svc"]
	if lr == nil || lr.Doc == nil {
		t.Fatal("svc segment-set repo not loaded")
	}
	if got := lr.LabelIndex.ByID("svc:aaa"); got == nil || got.PropGet("description") != "seg desc for aaa" {
		t.Fatalf("segment-set Doc-path description not applied: %#v", got)
	}
	// ADDITIVE: svc:bbb (baked, no sidecar entry) keeps its baked description.
	if bbb := lr.LabelIndex.ByID("svc:bbb"); bbb == nil || bbb.PropGet("description") != "baked bbb" {
		t.Fatalf("segment-set clobbered baked description: %#v", bbb)
	}
}
