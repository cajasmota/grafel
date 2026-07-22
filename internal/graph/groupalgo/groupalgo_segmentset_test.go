package groupalgo

import (
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/cajasmota/grafel/internal/daemon"
	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/graph/fbwriter"
	"github.com/cajasmota/grafel/internal/registry"
	"github.com/cajasmota/grafel/internal/testsupport"
)

// writeFixtureSegmentSetRepo hand-builds a 1-segment gen dir (graph.<gen>/
// with seg-0000.fb + manifest.json) in repoPath's daemon state dir and points
// `current` at it — the same fixture shape used at
// internal/graph/segmentset_test.go / internal/daemon/mcp/graph_cache_segmentset_test.go.
// No producer emits this yet (#5901 dark read substrate), hence the direct
// construction. Returns the registry.Repo entry.
func writeFixtureSegmentSetRepo(t *testing.T, slug, repoPath string, doc *graph.Document, gen uint64) registry.Repo {
	t.Helper()
	stateDir := daemon.StateDirForRepo(repoPath)
	genDir := filepath.Join(stateDir, graph.GenDirName(gen))
	name := graph.SegmentFileName(0)
	if err := fbwriter.WriteAtomic(filepath.Join(genDir, name), doc); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	ids := make([]string, 0, len(doc.Entities))
	for _, e := range doc.Entities {
		ids = append(ids, e.ID)
	}
	sort.Strings(ids)
	m := &graph.Manifest{FormatVersion: graph.ManifestFormatVersion, Segments: []graph.SegmentMeta{{
		File: name, Kind: graph.SegmentEntities,
		EntityCount: len(doc.Entities), RelCount: len(doc.Relationships),
		MinKey: ids[0], MaxKey: ids[len(ids)-1],
	}}}
	if err := graph.WriteManifest(genDir, m); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	if err := graph.WriteCurrentPointerRaw(stateDir, graph.GenDirName(gen)); err != nil {
		t.Fatalf("write current: %v", err)
	}
	return registry.Repo{Slug: slug, Path: repoPath}
}

func segmentSetTestDoc(slug string) *graph.Document {
	mk := func(name string) graph.Entity {
		return graph.Entity{ID: slug + ":" + name, Name: name, Kind: "function", SourceFile: slug + "/" + name + ".go", Language: "go"}
	}
	return &graph.Document{Version: 1, Repo: slug, Entities: []graph.Entity{mk("A"), mk("B")},
		Relationships: []graph.Relationship{{ID: slug + ":A->B", FromID: slug + ":A", ToID: slug + ":B", Kind: "CALLS"}}}
}

// TestAssembleGroupGraph_SegmentSet is the RED test for #5915 J2 P2 slice 1:
// AssembleGroupGraph must record a segment-set repo's mtime and load its
// entities/relationships, instead of skipping it as "never indexed" because
// os.Stat(graph.CurrentGraphPath(stateDir)) finds no flat .fb.
func TestAssembleGroupGraph_SegmentSet(t *testing.T) {
	testsupport.IsolateHome(t)
	root := t.TempDir()
	t.Setenv("GRAFEL_HOME", filepath.Join(root, "home"))
	t.Setenv("GRAFEL_DAEMON_ROOT", filepath.Join(root, "daemon"))

	repoPath := filepath.Join(root, "repoSeg")
	doc := segmentSetTestDoc("seg")
	r := writeFixtureSegmentSetRepo(t, "seg", repoPath, doc, 5)

	cfgPath, err := registry.ConfigPathFor("segacme")
	if err != nil {
		t.Fatalf("config path: %v", err)
	}
	cfg := &registry.GroupConfig{Name: "segacme", Repos: []registry.Repo{r}}
	if err := registry.SaveGroupConfig(cfgPath, cfg); err != nil {
		t.Fatalf("save group config: %v", err)
	}
	if err := registry.AddGroup("segacme", cfgPath); err != nil {
		t.Fatalf("add group: %v", err)
	}

	ents, rels, entityRepo, srcMtimes, err := AssembleGroupGraph("segacme")
	if err != nil {
		t.Fatalf("AssembleGroupGraph: %v", err)
	}
	if len(ents) != len(doc.Entities) {
		t.Fatalf("union entities=%d, want %d (segment-set repo wrongly skipped as never-indexed)", len(ents), len(doc.Entities))
	}
	if len(rels) != len(doc.Relationships) {
		t.Errorf("union rels=%d, want %d", len(rels), len(doc.Relationships))
	}
	if entityRepo["seg:A"] != "seg" {
		t.Errorf("seg:A attributed to repo %q, want seg", entityRepo["seg:A"])
	}
	if _, ok := srcMtimes["seg"]; !ok {
		t.Fatal("srcMtimes missing seg (segment-set) mtime")
	}
}

// TestCurrentSourceMtimes_SegmentSet mirrors the above for CurrentSourceMtimes
// (the overlay.go sibling gate).
func TestCurrentSourceMtimes_SegmentSet(t *testing.T) {
	testsupport.IsolateHome(t)
	root := t.TempDir()
	t.Setenv("GRAFEL_HOME", filepath.Join(root, "home"))
	t.Setenv("GRAFEL_DAEMON_ROOT", filepath.Join(root, "daemon"))

	repoPath := filepath.Join(root, "repoSeg2")
	doc := segmentSetTestDoc("seg2")
	r := writeFixtureSegmentSetRepo(t, "seg2", repoPath, doc, 9)

	cfgPath, err := registry.ConfigPathFor("segacme2")
	if err != nil {
		t.Fatalf("config path: %v", err)
	}
	cfg := &registry.GroupConfig{Name: "segacme2", Repos: []registry.Repo{r}}
	if err := registry.SaveGroupConfig(cfgPath, cfg); err != nil {
		t.Fatalf("save group config: %v", err)
	}
	if err := registry.AddGroup("segacme2", cfgPath); err != nil {
		t.Fatalf("add group: %v", err)
	}

	mtimes, err := CurrentSourceMtimes("segacme2")
	if err != nil {
		t.Fatalf("CurrentSourceMtimes: %v", err)
	}
	if _, ok := mtimes["seg2"]; !ok {
		t.Fatal("CurrentSourceMtimes missing seg2 (segment-set) mtime")
	}

	// Sanity: the recorded mtime must match the manifest.json mtime (the
	// atomic flip point for a segment-set), not some other file.
	stateDir := daemon.StateDirForRepo(repoPath)
	desc, dErr := graph.CurrentGraphDescriptor(stateDir)
	if dErr != nil || desc.Kind != graph.GraphSegmentSet {
		t.Fatalf("descriptor not segment-set: kind=%v err=%v", desc.Kind, dErr)
	}
	fi, statErr := os.Stat(filepath.Join(desc.GenDir, graph.ManifestFileName))
	if statErr != nil {
		t.Fatal(statErr)
	}
	if mtimes["seg2"] != fi.ModTime().UnixNano() {
		t.Errorf("mtime=%d, want manifest.json mtime=%d", mtimes["seg2"], fi.ModTime().UnixNano())
	}
}
