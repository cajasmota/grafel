package cli

// #6115 — `grafel status` printed "0 files" for every .fb-backed repo.
//
// The file count is not encoded in ANY .fb layout (see internal/graph/schema/
// graph.fbs: the Graph root table has no file-count field), so a graph loaded
// back from .fb necessarily reports Stats.Files == 0 — GraphStream.DocStats
// says so explicitly. graph-stats.json IS the carrier of the real number, and
// ComputeStatusSummaryForRef already reads it into rs.Files... and then, a few
// lines later, assigned the graph's own (structurally zero) count over the top.
// The real value was in hand and discarded on the way to the screen.
//
// These tests assert on the RENDERED status line, not on the struct field: the
// defect is a discard on the render path, and an in-memory assertion would not
// have seen it.

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/daemon"
	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/graph/fbwriter"
	"github.com/cajasmota/grafel/internal/registry"
)

// assertFBBacked fails unless stateDir is genuinely served by a .fb layout and
// holds NO graph.json.
//
// This is the anti-vacuity guard for both tests below. graph.json is the ONE
// layout whose Stats.Files is already non-zero, so a fixture that silently fell
// back to it would make "7 files" appear on screen no matter what the
// production code does — the assertion would pass against the unfixed binary.
func assertFBBacked(t *testing.T, stateDir string) {
	t.Helper()
	if _, err := os.Stat(filepath.Join(stateDir, "graph.json")); err == nil {
		t.Fatalf("fixture is not .fb-backed: graph.json exists in %s", stateDir)
	}
	desc, err := graph.CurrentGraphDescriptor(stateDir)
	if err != nil {
		t.Fatalf("resolve graph descriptor: %v", err)
	}
	if desc.Kind != graph.GraphSingleFile && desc.Kind != graph.GraphSegmentSet {
		t.Fatalf("fixture is not .fb-backed: descriptor kind = %v", desc.Kind)
	}
	// And the .fb really does report zero files, which is what makes the
	// overwrite destructive. If this ever stops being true the fix below is
	// no longer load-bearing and this test should be revisited, not deleted.
	stream, err := graph.OpenGraphStream(stateDir)
	if err != nil {
		t.Fatalf("open graph stream: %v", err)
	}
	defer stream.Close()
	if got := stream.DocStats().Files; got != 0 {
		t.Fatalf("precondition changed: .fb graph now reports Files = %d, want 0", got)
	}
}

// files6115Doc is a minimal but real graph document. Stats.Files is set to a
// value that the .fb writer cannot persist — proving the number on screen came
// from the sidecar and not from the graph.
func files6115Doc() *graph.Document {
	return &graph.Document{
		Repo:        "fbrepo",
		GeneratedAt: time.Now().Add(-30 * time.Minute),
		IndexedRef:  "main",
		IndexedSHA:  "abcdef012345",
		Stats:       graph.Stats{Files: 999},
		Entities: []graph.Entity{
			{ID: "e1", Name: "A", Kind: "function", SourceFile: "a.go", Language: "go"},
		},
		Relationships: []graph.Relationship{},
	}
}

func files6115Sidecar(t *testing.T, stateDir string, files, ents, rels int) {
	t.Helper()
	side := graph.GraphStatsSidecar{
		Version:            1,
		ComputedAt:         time.Now().Add(-30 * time.Minute),
		TotalFiles:         files,
		TotalEntities:      ents,
		TotalRelationships: rels,
	}
	data, err := json.Marshal(side)
	if err != nil {
		t.Fatalf("marshal sidecar: %v", err)
	}
	if err := os.WriteFile(filepath.Join(stateDir, "graph-stats.json"), data, 0o644); err != nil {
		t.Fatalf("write sidecar: %v", err)
	}
}

func files6115Repo(t *testing.T, root, slug string) (string, string) {
	t.Helper()
	repoPath := filepath.Join(root, slug)
	if err := os.MkdirAll(repoPath, 0o755); err != nil {
		t.Fatalf("mkdir repo: %v", err)
	}
	stateDir := daemon.StateDirForRepo(repoPath)
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatalf("mkdir state: %v", err)
	}
	return repoPath, stateDir
}

// renderStatus returns the line of PrintStatusSummary output for slug.
func renderStatusLine(t *testing.T, group string, repos []registry.Repo, slug string) string {
	t.Helper()
	var buf bytes.Buffer
	PrintStatusSummary(&buf, ComputeStatusSummary(group, repos))
	for _, line := range strings.Split(buf.String(), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), slug+" ") {
			return line
		}
	}
	t.Fatalf("no status line for %q in:\n%s", slug, buf.String())
	return ""
}

// TestStatusPrintsSidecarFileCountForFBRepo is the #6115 regression: a
// .fb-backed repo with a graph-stats.json must print the sidecar's TotalFiles,
// not zero.
func TestStatusPrintsSidecarFileCountForFBRepo(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv(daemon.EnvRoot, tmpDir)

	repoPath, stateDir := files6115Repo(t, tmpDir, "fbrepo")
	if err := fbwriter.WriteAtomic(filepath.Join(stateDir, "graph.fb"), files6115Doc()); err != nil {
		t.Fatalf("write graph.fb: %v", err)
	}
	// 4321 is deliberately unlike every other number in this fixture (1 entity,
	// 0 rels, doc.Stats.Files 999) so the digits on screen can only have come
	// from the sidecar.
	files6115Sidecar(t, stateDir, 4321, 1, 0)
	assertFBBacked(t, stateDir)

	line := renderStatusLine(t, "g6115", []registry.Repo{{Slug: "fbrepo", Path: repoPath}}, "fbrepo")
	// Rendered with fmtInt's thousands separator; spelled out literally rather
	// than via fmtInt so the expectation cannot drift with the formatter.
	if !strings.Contains(line, "4,321 files") {
		t.Fatalf("status line lost the sidecar file count (#6115)\n got: %q\nwant it to contain %q", line, "4,321 files")
	}
	if strings.Contains(line, "999") {
		t.Fatalf("status line reported the graph document's file count, not the sidecar's: %q", line)
	}
}

// TestStatusReportsZeroFilesWhenNoSidecar is the negative control for the fix
// above: with no sidecar there is no real file count anywhere, and status must
// keep saying 0 rather than inventing one. Without this, "always keep whatever
// rs.Files already holds" and "hardcode a number" both pass the test above.
func TestStatusReportsZeroFilesWhenNoSidecar(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv(daemon.EnvRoot, tmpDir)

	repoPath, stateDir := files6115Repo(t, tmpDir, "nosidecar")
	if err := fbwriter.WriteAtomic(filepath.Join(stateDir, "graph.fb"), files6115Doc()); err != nil {
		t.Fatalf("write graph.fb: %v", err)
	}
	assertFBBacked(t, stateDir)

	line := renderStatusLine(t, "g6115", []registry.Repo{{Slug: "nosidecar", Path: repoPath}}, "nosidecar")
	if !strings.Contains(line, "0 files") {
		t.Fatalf("a .fb repo with no sidecar has no known file count; want %q in %q", "0 files", line)
	}
}

// TestStatusPrintsDocumentFileCountForJSONRepoWithoutSidecar pins the OTHER
// direction of the #6115 guard.
//
// graph.json is the one layout that DOES carry a real Stats.Files, and a
// sidecar-less graph.json repo has nowhere else to get the number from. A fix
// that simply deleted the assignment — "the sidecar is always right" — would
// regress this repo from 7 files to 0 and no other test in the package would
// notice, because every parity fixture on the graph.json path also has a
// sidecar.
func TestStatusPrintsDocumentFileCountForJSONRepoWithoutSidecar(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv(daemon.EnvRoot, tmpDir)

	repoPath, stateDir := files6115Repo(t, tmpDir, "jsonrepo")
	doc := files6115Doc()
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal doc: %v", err)
	}
	if err := os.WriteFile(filepath.Join(stateDir, "graph.json"), raw, 0o644); err != nil {
		t.Fatalf("write graph.json: %v", err)
	}
	// No sidecar, and no .fb: the document is the ONLY source of the count.
	if _, statErr := os.Stat(filepath.Join(stateDir, "graph-stats.json")); statErr == nil {
		t.Fatalf("fixture invalid: a sidecar exists")
	}

	line := renderStatusLine(t, "g6115", []registry.Repo{{Slug: "jsonrepo", Path: repoPath}}, "jsonrepo")
	if !strings.Contains(line, "999 files") {
		t.Fatalf("graph.json repo lost its document file count\n got: %q", line)
	}
}

// TestStatusPrintsSidecarFileCountForSegmentSetRepo covers the OTHER .fb
// layout. The segment-set path resolves through a different descriptor branch
// (graph.<gen>/ + manifest.json, no flat .fb), and it is the layout a
// corpus-sized repo actually uses — a fix verified only against the flat file
// would leave the big repos reading zero.
func TestStatusPrintsSidecarFileCountForSegmentSetRepo(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv(daemon.EnvRoot, tmpDir)

	repoPath, stateDir := files6115Repo(t, tmpDir, "segrepo")
	doc := files6115Doc()
	genDir := filepath.Join(stateDir, graph.GenDirName(3))
	segName := graph.SegmentFileName(0)
	if err := fbwriter.WriteAtomic(filepath.Join(genDir, segName), doc); err != nil {
		t.Fatalf("write segment: %v", err)
	}
	m := &graph.Manifest{FormatVersion: graph.ManifestFormatVersion, Segments: []graph.SegmentMeta{{
		File: segName, Kind: graph.SegmentEntities, EntityCount: len(doc.Entities),
		MinKey: doc.Entities[0].ID, MaxKey: doc.Entities[0].ID,
	}}}
	if err := graph.WriteManifest(genDir, m); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	if err := graph.WriteCurrentPointerRaw(stateDir, graph.GenDirName(3)); err != nil {
		t.Fatalf("write current pointer: %v", err)
	}
	files6115Sidecar(t, stateDir, 8765, 1, 0)
	assertFBBacked(t, stateDir)

	line := renderStatusLine(t, "g6115", []registry.Repo{{Slug: "segrepo", Path: repoPath}}, "segrepo")
	if !strings.Contains(line, "8,765 files") {
		t.Fatalf("segment-set status line lost the sidecar file count (#6115)\n got: %q", line)
	}
}
