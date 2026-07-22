package cli

import (
	"bytes"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/daemon"
	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/graph/fbwriter"
	"github.com/cajasmota/grafel/internal/registry"
)

// writeSegmentSetFixtureForDoctorTest hand-builds a 1-segment gen dir
// (graph.<gen>/ with seg-0000.fb + manifest.json) under stateDir and points
// `current` at it — same shape as internal/graph/segmentset_test.go.
func writeSegmentSetFixtureForDoctorTest(t *testing.T, stateDir string, gen uint64) {
	t.Helper()
	doc := &graph.Document{Repo: "seg", Entities: []graph.Entity{
		{ID: "aa1", QualifiedName: "p.A", Kind: "function", Name: "A"},
	}}
	genDir := filepath.Join(stateDir, graph.GenDirName(gen))
	name := graph.SegmentFileName(0)
	if err := fbwriter.WriteAtomic(filepath.Join(genDir, name), doc); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	ids := []string{"aa1"}
	sort.Strings(ids)
	m := &graph.Manifest{FormatVersion: graph.ManifestFormatVersion, Segments: []graph.SegmentMeta{{
		File: name, Kind: graph.SegmentEntities, EntityCount: 1,
		MinKey: ids[0], MaxKey: ids[0],
	}}}
	if err := graph.WriteManifest(genDir, m); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	if err := graph.WriteCurrentPointerRaw(stateDir, graph.GenDirName(gen)); err != nil {
		t.Fatalf("write current: %v", err)
	}
}

// TestCheckRepo_SegmentSet is the RED test for #5915 J2 P2 slice 1: checkRepo
// must report a segment-set repo (graph.<gen>/ dir + manifest.json, no flat
// graph.fb) as having a graph, not "no graph found". Before the fix,
// checkRepo's hasFB stat'd daemon.GraphFBPathForRepo(r.Path) — the flat .fb
// path, absent for a segment-set.
func TestCheckRepo_SegmentSet(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv(daemon.EnvRoot, tmpDir)

	repoPath := filepath.Join(tmpDir, "seg-repo")
	mustMkdir(t, filepath.Join(repoPath, ".git"))

	stateDir := daemon.StateDirForRepo(repoPath)
	mustMkdir(t, stateDir)
	writeSegmentSetFixtureForDoctorTest(t, stateDir, 6)

	var buf bytes.Buffer
	checkRepo(&buf, registry.Repo{Slug: "seg-repo", Path: repoPath, Stack: registry.StackList{"go"}})
	out := buf.String()
	if strings.Contains(out, "no graph found") {
		t.Fatalf("checkRepo(segment-set repo) reported 'no graph found':\n%s", out)
	}
	if !strings.Contains(out, "graph.fb present") {
		t.Errorf("checkRepo(segment-set repo) did not report graph.fb present:\n%s", out)
	}
}
