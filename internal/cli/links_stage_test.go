package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/cajasmota/grafel/internal/daemon"
	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/graph/fbwriter"
	"github.com/cajasmota/grafel/internal/links"
	"github.com/cajasmota/grafel/internal/registry"
)

// #5904(e) PR-c (#5915 P2 gap): stageGraphsDir and the phantom-edge existence
// gate must not silently drop a SEGMENTED repo — a repo whose active
// generation is a multi-segment gen dir (graph.<gen>/seg-*.fb +
// manifest.json, no flat graph.fb ever written for it). These tests build
// such a repo by hand (mirroring internal/graph/segmentset_test.go's
// writeSegmentSet helper, reimplemented here since that helper lives in an
// external _test.go file and is not exported) alongside an ordinary
// single-file repo, and assert both are staged and resolvable.

// writeSingleFileRepo writes doc as the legacy flat graph.fb (GraphSingleFile,
// no `current` pointer) into repoPath's state dir — the pre-#5901 shape.
func writeSingleFileRepo(t *testing.T, repoPath string, doc *graph.Document) string {
	t.Helper()
	stateDir := daemon.StateDirForRepo(repoPath)
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatal(err)
	}
	fb := filepath.Join(stateDir, "graph.fb")
	if err := fbwriter.WriteAtomic(fb, doc); err != nil {
		t.Fatalf("write graph.fb: %v", err)
	}
	return stateDir
}

// writeSegmentSetRepo hand-builds a multi-segment generation on disk: each doc
// becomes its own seg-NNNN.fb inside <stateDir>/graph.<gen>/, with a matching
// manifest.json, and points <stateDir>/current at the gen dir.
func writeSegmentSetRepo(t *testing.T, repoPath string, gen uint64, docs []*graph.Document) string {
	t.Helper()
	stateDir := daemon.StateDirForRepo(repoPath)
	genDirName := graph.GenDirName(gen)
	genDir := filepath.Join(stateDir, genDirName)
	if err := os.MkdirAll(genDir, 0o755); err != nil {
		t.Fatal(err)
	}
	m := &graph.Manifest{FormatVersion: graph.ManifestFormatVersion}
	for i, doc := range docs {
		name := graph.SegmentFileName(i)
		if err := fbwriter.WriteAtomic(filepath.Join(genDir, name), doc); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
		seg := graph.SegmentMeta{
			File:        name,
			Kind:        graph.SegmentEntities,
			EntityCount: len(doc.Entities),
			RelCount:    len(doc.Relationships),
		}
		if len(doc.Entities) > 0 {
			ids := make([]string, 0, len(doc.Entities))
			for _, e := range doc.Entities {
				ids = append(ids, e.ID)
			}
			sort.Strings(ids)
			seg.MinKey, seg.MaxKey = ids[0], ids[len(ids)-1]
		} else {
			seg.Kind = graph.SegmentRelationships
		}
		m.Segments = append(m.Segments, seg)
	}
	if err := graph.WriteManifest(genDir, m); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	if err := graph.WriteCurrentPointerRaw(stateDir, genDirName); err != nil {
		t.Fatalf("write current pointer: %v", err)
	}
	return stateDir
}

// segRepoDocs returns two disjoint, key-sorted entity segments for "seg-repo".
func segRepoDocs() []*graph.Document {
	return []*graph.Document{
		{Repo: "seg-repo", Entities: []graph.Entity{
			{ID: "a", QualifiedName: "p.A", Kind: "function", Name: "A"},
		}},
		{Repo: "seg-repo", Entities: []graph.Entity{
			{ID: "b", QualifiedName: "p.B", Kind: "function", Name: "B"},
		}},
	}
}

// TestStageGraphsDir_SegmentSetAndSingleFile: a fleet with one single-file
// repo AND one SEGMENTED repo. Both must be staged; the segment-set repo's
// staged dir must resolve via graph.LoadGraphFromDir with its entities
// intact — the crux of the #5915 P2 fix.
func TestStageGraphsDir_SegmentSetAndSingleFile(t *testing.T) {
	t.Setenv("GRAFEL_DAEMON_ROOT", t.TempDir())

	singlePath := t.TempDir()
	writeSingleFileRepo(t, singlePath, &graph.Document{Repo: "single-repo", Entities: []graph.Entity{
		{ID: "x", QualifiedName: "p.X", Kind: "function", Name: "X"},
	}})

	segPath := t.TempDir()
	writeSegmentSetRepo(t, segPath, 7, segRepoDocs())

	cfg := &registry.GroupConfig{Repos: []registry.Repo{
		{Slug: "single-repo", Path: singlePath},
		{Slug: "seg-repo", Path: segPath},
	}}

	tmp, cleanup, err := stageGraphsDir(cfg)
	if err != nil {
		t.Fatalf("stageGraphsDir: %v", err)
	}
	defer cleanup()

	// single-file repo staged as graph.fb (byte-for-byte parity is asserted
	// in the dedicated parity test below).
	if _, err := os.Stat(filepath.Join(tmp, "single-repo", "graph.fb")); err != nil {
		t.Fatalf("single-repo graph.fb not staged: %v", err)
	}

	// segment-set repo: gen dir + segments + manifest + current pointer.
	segDstDir := filepath.Join(tmp, "seg-repo")
	genDirName := graph.GenDirName(7)
	stagedGenDir := filepath.Join(segDstDir, genDirName)
	if fi, err := os.Stat(stagedGenDir); err != nil || !fi.IsDir() {
		t.Fatalf("seg-repo gen dir not staged at %s: %v", stagedGenDir, err)
	}
	if _, err := os.Stat(filepath.Join(stagedGenDir, graph.ManifestFileName)); err != nil {
		t.Fatalf("seg-repo manifest not staged: %v", err)
	}
	for i := range segRepoDocs() {
		segFile := filepath.Join(stagedGenDir, graph.SegmentFileName(i))
		if _, err := os.Stat(segFile); err != nil {
			t.Fatalf("seg-repo segment %d not staged: %v", i, err)
		}
	}
	if _, err := os.Stat(filepath.Join(segDstDir, "current")); err != nil {
		t.Fatalf("seg-repo current pointer not staged: %v", err)
	}

	doc, err := graph.LoadGraphFromDir(segDstDir)
	if err != nil {
		t.Fatalf("LoadGraphFromDir(staged seg-repo): %v", err)
	}
	if len(doc.Entities) != 2 {
		t.Fatalf("staged seg-repo entities = %d, want 2", len(doc.Entities))
	}
	gotIDs := map[string]bool{}
	for _, e := range doc.Entities {
		gotIDs[e.ID] = true
	}
	if !gotIDs["a"] || !gotIDs["b"] {
		t.Fatalf("staged seg-repo entities = %v, want a and b present", gotIDs)
	}
}

// TestStageGraphsDir_SingleFileParity: the single-file staging path must be
// byte-for-byte unchanged by the descriptor-aware rewrite.
func TestStageGraphsDir_SingleFileParity(t *testing.T) {
	t.Setenv("GRAFEL_DAEMON_ROOT", t.TempDir())

	repoPath := t.TempDir()
	stateDir := writeSingleFileRepo(t, repoPath, &graph.Document{Repo: "single-repo", Entities: []graph.Entity{
		{ID: "x", QualifiedName: "p.X", Kind: "function", Name: "X"},
	}})
	srcFB := filepath.Join(stateDir, "graph.fb")

	cfg := &registry.GroupConfig{Repos: []registry.Repo{{Slug: "single-repo", Path: repoPath}}}
	tmp, cleanup, err := stageGraphsDir(cfg)
	if err != nil {
		t.Fatalf("stageGraphsDir: %v", err)
	}
	defer cleanup()

	dstFB := filepath.Join(tmp, "single-repo", "graph.fb")
	if fi, err := os.Lstat(dstFB); err == nil && fi.Mode()&os.ModeSymlink != 0 {
		target, rerr := os.Readlink(dstFB)
		if rerr != nil || target != srcFB {
			t.Errorf("symlink target = %q (err %v), want %q", target, rerr, srcFB)
		}
	}
	wantBytes, err := os.ReadFile(srcFB)
	if err != nil {
		t.Fatal(err)
	}
	gotBytes, err := os.ReadFile(dstFB)
	if err != nil {
		t.Fatal(err)
	}
	if string(gotBytes) != string(wantBytes) {
		t.Errorf("staged graph.fb bytes differ from source")
	}

	// No segment-set artifacts should ever appear alongside single-file staging.
	entries, err := os.ReadDir(filepath.Join(tmp, "single-repo"))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name() != "graph.fb" {
			t.Errorf("unexpected extra staged entry for single-file repo: %s", e.Name())
		}
	}
}

// TestRunPhantomEdgePass_SegmentSetNotSkipped: the phantom-edge existence gate
// must not skip a SEGMENTED source repo. Builds a two-repo fleet (segment-set
// source, single-file target) with a links.json CALLS/http edge from the
// segment-set repo into the other, and asserts a phantom edge is actually
// added — proving loadGraphDocument was reached for the segment-set repo
// rather than skipped by the gate.
func TestRunPhantomEdgePass_SegmentSetNotSkipped(t *testing.T) {
	t.Setenv("GRAFEL_DAEMON_ROOT", t.TempDir())

	segPath := t.TempDir()
	writeSegmentSetRepo(t, segPath, 3, []*graph.Document{
		{Repo: "seg-repo", Entities: []graph.Entity{
			{ID: "a", QualifiedName: "p.A", Kind: "function", Name: "A"},
		}},
	})

	otherPath := t.TempDir()
	writeSingleFileRepo(t, otherPath, &graph.Document{Repo: "other-repo", Entities: []graph.Entity{
		{ID: "b", QualifiedName: "p.B", Kind: "function", Name: "B"},
	}})

	cfg := &registry.GroupConfig{Repos: []registry.Repo{
		{Slug: "seg-repo", Path: segPath},
		{Slug: "other-repo", Path: otherPath},
	}}

	linksDoc := links.Document{
		Version: links.SchemaVersion,
		Links: []links.Link{{
			ID:       "1",
			Source:   "seg-repo::a",
			Target:   "other-repo::b",
			Relation: links.RelationCalls,
			Method:   links.MethodHTTP,
		}},
	}
	linksPath := filepath.Join(t.TempDir(), "test-links.json")
	b, err := json.Marshal(linksDoc)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(linksPath, b, 0o644); err != nil {
		t.Fatal(err)
	}

	added, err := runPhantomEdgePass("test-group", cfg, linksPath)
	if err != nil {
		t.Fatalf("runPhantomEdgePass: %v", err)
	}
	if added != 1 {
		t.Fatalf("phantom edges added = %d, want 1 (segment-set source repo must not be skipped)", added)
	}
}
