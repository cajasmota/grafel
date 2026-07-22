package links

import (
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/graph/fbwriter"
)

// #5904(e) PR-c (#5915 P2 gap): loadAllGraphs discovery must recognize a
// SEGMENTED repo dir — one whose active generation is a graph.<gen>/ dir
// (seg-NNNN.fb + manifest.json) named by a `current` pointer, with NO flat
// graph.fb. Before the discovery fix, WalkDir matched only graph.json /
// graph.<gen>.fb, so such a repo was silently dropped and never contributed
// to any cross-repo pass. These tests exercise the REAL consumer
// (loadAllGraphs + RunAllPasses).

// writeSingleFileFBRepo writes doc as the legacy flat graph.fb (GraphSingleFile,
// no `current` pointer) into root/<slug>/. Mirrors the staged layout
// <graphsDir>/<slug>/graph.fb that stageGraphsDir produces for a single-file
// repo.
func writeSingleFileFBRepo(t *testing.T, root, slug string, doc *graph.Document) string {
	t.Helper()
	dir := filepath.Join(root, slug)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := fbwriter.WriteAtomic(filepath.Join(dir, "graph.fb"), doc); err != nil {
		t.Fatalf("write graph.fb for %s: %v", slug, err)
	}
	return dir
}

// writeSegmentSetFBRepo hand-builds a multi-segment generation for slug: each
// doc becomes its own seg-NNNN.fb inside root/<slug>/graph.<gen>/, with a
// manifest.json, and points root/<slug>/current at the gen dir. Mirrors the
// staged layout stageGraphsDir produces for a GraphSegmentSet repo.
func writeSegmentSetFBRepo(t *testing.T, root, slug string, gen uint64, docs []*graph.Document) string {
	t.Helper()
	dir := filepath.Join(root, slug)
	genDirName := graph.GenDirName(gen)
	genDir := filepath.Join(dir, genDirName)
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
	if err := graph.WriteCurrentPointerRaw(dir, genDirName); err != nil {
		t.Fatalf("write current pointer: %v", err)
	}
	return dir
}

// TestLoadAllGraphs_DiscoversSegmentSet: a graphs dir holding a single-file
// repo AND a segment-set repo must yield BOTH repoGraphs. Directly exercises
// the discovery WalkDir. Also asserts the gen dir does NOT register a phantom
// repo entry (dedupe guard).
func TestLoadAllGraphs_DiscoversSegmentSet(t *testing.T) {
	root := t.TempDir()

	writeSingleFileFBRepo(t, root, "single-repo", &graph.Document{
		Repo:     "single-repo",
		Entities: []graph.Entity{{ID: "b1", Name: "OrderBookService", Kind: "interface", SourceFile: "lib/order.py"}},
	})
	writeSegmentSetFBRepo(t, root, "seg-repo", 4, []*graph.Document{
		{Repo: "seg-repo", Entities: []graph.Entity{{ID: "a1", Name: "OrderBook", Kind: "class", SourceFile: "src/order.go"}}},
	})

	graphs, err := loadAllGraphs(root)
	if err != nil {
		t.Fatalf("loadAllGraphs: %v", err)
	}
	byRepo := map[string]repoGraph{}
	for _, g := range graphs {
		byRepo[g.Repo] = g
	}
	if _, ok := byRepo["single-repo"]; !ok {
		t.Errorf("single-repo not discovered: got repos %v", repoKeysOf(byRepo))
	}
	seg, ok := byRepo["seg-repo"]
	if !ok {
		t.Fatalf("seg-repo (segment-set) not discovered: got repos %v", repoKeysOf(byRepo))
	}
	if len(seg.Entities) != 1 || seg.Entities[0].ID != "a1" {
		t.Errorf("seg-repo entities = %+v, want single entity a1", seg.Entities)
	}
	// Dedupe guard: the graph.4/ gen dir must not register as its own repo.
	if g, bad := byRepo["graph.4"]; bad {
		t.Errorf("gen dir registered a phantom repo entry: %+v", g)
	}
	if len(graphs) != 2 {
		t.Errorf("discovered %d repos, want exactly 2 (single-repo + seg-repo): %v", len(graphs), repoKeysOf(byRepo))
	}
}

// TestRunAllPasses_SegmentSetContributesCrossRepoLink: end-to-end through the
// REAL consumer. seg-repo (segment-set) owns entity a1 with an IMPORTS edge to
// single-repo's entity b1. RunAllPasses must emit a cross-repo import link
// SOURCED FROM seg-repo — proving the segment-set repo is no longer dropped at
// discovery and actually contributes to a pass (the #5915 P2 goal).
func TestRunAllPasses_SegmentSetContributesCrossRepoLink(t *testing.T) {
	root := t.TempDir()

	writeSingleFileFBRepo(t, root, "single-repo", &graph.Document{
		Repo:     "single-repo",
		Entities: []graph.Entity{{ID: "b1", Name: "OrderBookService", Kind: "interface", SourceFile: "lib/order.py"}},
	})
	writeSegmentSetFBRepo(t, root, "seg-repo", 2, []*graph.Document{
		{
			Repo:          "seg-repo",
			Entities:      []graph.Entity{{ID: "a1", Name: "OrderBook", Kind: "class", SourceFile: "src/order.go"}},
			Relationships: []graph.Relationship{{ID: "e1", FromID: "a1", ToID: "b1", Kind: "imports"}},
		},
	})

	home := filepath.Join(t.TempDir(), "ag-home")
	res, err := RunAllPasses("g-seg-e2e", root, home)
	if err != nil {
		t.Fatalf("RunAllPasses: %v", err)
	}
	doc, err := readDoc(res.OutLinks)
	if err != nil {
		t.Fatalf("readDoc: %v", err)
	}
	var found bool
	for _, l := range doc.Links {
		if l.Method == MethodImport && l.Source == "seg-repo::a1" && l.Target == "single-repo::b1" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected cross-repo import link seg-repo::a1 → single-repo::b1 "+
			"(segment-set repo must contribute); got %+v", doc.Links)
	}
}

func repoKeysOf(m map[string]repoGraph) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
