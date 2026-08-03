package cli

// Output-parity guard for `grafel status` (#5995).
//
// #5995 makes ComputeStatusSummary stop materialising each repo's graph. The
// hazard of that change is silent drift: a status command that quietly reports
// a different number than before is worse than a slow one. This test renders
// PrintStatusSummary over a fixture covering every graph state the status path
// distinguishes and compares it byte-for-byte with a golden file captured from
// the pre-change implementation.
//
// Regenerate deliberately (and only when a reported value is MEANT to change):
//
//	GRAFEL_UPDATE_GOLDEN=1 go test ./internal/cli/ -run StatusOutputParity

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/daemon"
	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/graph/fbwriter"
	"github.com/cajasmota/grafel/internal/registry"
)

// parityIndexedAge is how far in the past every fixture graph claims to have
// been indexed.
//
// Fixtures WITHOUT a graph-stats.json sidecar take their age from the graph.fb
// header's ComputedAt, or from the file mtime when that is unreadable — i.e.
// from wall-clock now. Leaving those at "now" pinned `indexed 0s ago` into the
// golden, and any run that crossed the one-second boundary between writing the
// fixture and rendering the summary produced `1s ago` and failed the byte-diff.
// That made this test flaky (measured ~25% at load ~4) AND made the golden a
// timing artefact of whichever implementation happened to be fast enough to
// stay under a second, which is the opposite of an implementation-independent
// parity guard.
//
// 90 minutes renders as the stable string "1h30m ago" — formatTimeSince
// truncates to whole minutes there, so the whole 90m00s–90m59s window (about
// 59 s of slack, versus the 1 s the old fixture had) maps to one output.
const parityIndexedAge = 90 * time.Minute

func parityDoc(repo string, ref, sha string, worktree bool) *graph.Document {
	return &graph.Document{
		Repo:        repo,
		GeneratedAt: time.Now().Add(-parityIndexedAge),
		IndexedRef:  ref,
		IndexedSHA:  sha,
		IsWorktree:  worktree,
		Stats:       graph.Stats{Files: 7},
		Entities: []graph.Entity{
			{ID: "e1", Name: "A", Kind: "function", SourceFile: "a.go", Language: "go"},
			{ID: "e2", Name: "B", Kind: "http_endpoint_definition", SourceFile: "b.go", Language: "go"},
			{ID: "e3", Name: "C", Kind: "http_endpoint_call", SourceFile: "b.go", Language: "go"},
			{ID: "e4", Name: "D", Kind: "http_endpoint", SourceFile: "c.go", Language: "go"},
			{ID: "e5", Name: "E", Kind: "process", SourceFile: "c.go", Language: "go"},
			{ID: "e6", Name: "F", Kind: "SCOPE.ProcessFlow", SourceFile: "d.go", Language: "go"},
		},
		Relationships: []graph.Relationship{
			{ID: "r1", FromID: "e1", ToID: "e2", Kind: "calls"},
			{ID: "r2", FromID: "e2", ToID: "e3", Kind: "calls"},
		},
	}
}

// backdateGraphArtefacts pins the mtime of every graph artefact under root to
// parityIndexedAge ago.
//
// The header ComputedAt covers fixtures whose header is READABLE. It does not
// cover the rest: a graph.fb that fails to open has no header to read, so
// ComputeStatusSummary falls back to daemon.FindGraphFile's mtime — wall-clock
// now — and pinned `indexed 0s ago` into the golden with a one-second flake
// window. Backdating closes that second path to the clock.
func backdateGraphArtefacts(t *testing.T, root string) {
	t.Helper()
	when := time.Now().Add(-parityIndexedAge)
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		switch filepath.Ext(path) {
		case ".fb", ".json":
			return os.Chtimes(path, when, when)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("backdate artefacts: %v", err)
	}
}

// parityRepo creates a repo dir under root and returns its path + state dir.
func parityRepo(t *testing.T, root, slug string) (string, string) {
	t.Helper()
	repoPath := filepath.Join(root, slug)
	if err := os.MkdirAll(repoPath, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", slug, err)
	}
	stateDir := daemon.StateDirForRepo(repoPath)
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatalf("mkdir state %s: %v", slug, err)
	}
	return repoPath, stateDir
}

func writeParitySidecar(t *testing.T, stateDir string, ents, rels, files int, age time.Duration) {
	t.Helper()
	side := graph.GraphStatsSidecar{
		Version:            1,
		ComputedAt:         time.Now().Add(-age),
		TotalFiles:         files,
		TotalEntities:      ents,
		TotalRelationships: rels,
	}
	data, _ := json.Marshal(side)
	if err := os.WriteFile(filepath.Join(stateDir, "graph-stats.json"), data, 0o644); err != nil {
		t.Fatalf("write sidecar: %v", err)
	}
}

// writeParitySegmentSet writes a 1-segment gen dir and points `current` at it.
func writeParitySegmentSet(t *testing.T, stateDir string, doc *graph.Document, gen uint64) {
	t.Helper()
	genDir := filepath.Join(stateDir, graph.GenDirName(gen))
	name := graph.SegmentFileName(0)
	if err := fbwriter.WriteAtomic(filepath.Join(genDir, name), doc); err != nil {
		t.Fatalf("write segment: %v", err)
	}
	ids := make([]string, 0, len(doc.Entities))
	for _, e := range doc.Entities {
		ids = append(ids, e.ID)
	}
	sort.Strings(ids)
	m := &graph.Manifest{FormatVersion: graph.ManifestFormatVersion, Segments: []graph.SegmentMeta{{
		File: name, Kind: graph.SegmentEntities, EntityCount: len(doc.Entities),
		MinKey: ids[0], MaxKey: ids[len(ids)-1],
	}}}
	if err := graph.WriteManifest(genDir, m); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	if err := graph.WriteCurrentPointerRaw(stateDir, graph.GenDirName(gen)); err != nil {
		t.Fatalf("write current: %v", err)
	}
}

// TestStatusOutputParity renders every graph state the status path
// distinguishes and compares against testdata/status_parity_5995.golden.
func TestStatusOutputParity(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv(daemon.EnvRoot, tmpDir)

	var repos []registry.Repo
	add := func(slug, path string) { repos = append(repos, registry.Repo{Slug: slug, Path: path}) }

	// 1. Healthy flat graph.fb WITH the graph-stats.json sidecar.
	p, sd := parityRepo(t, tmpDir, "healthy")
	if err := fbwriter.WriteAtomic(filepath.Join(sd, "graph.fb"), parityDoc("healthy", "main", "abc123def456", false)); err != nil {
		t.Fatalf("write graph.fb: %v", err)
	}
	writeParitySidecar(t, sd, 6, 2, 7, 3*time.Minute)
	add("healthy", p)

	// 2. Flat graph.fb, NO sidecar (the PersistedStats header path, #5442).
	p, sd = parityRepo(t, tmpDir, "nosidecar")
	if err := fbwriter.WriteAtomic(filepath.Join(sd, "graph.fb"), parityDoc("nosidecar", "feat/x", "0badc0ffee11", true)); err != nil {
		t.Fatalf("write graph.fb: %v", err)
	}
	add("nosidecar", p)

	// 3. Never indexed: state dir exists, no graph artefact at all.
	p, _ = parityRepo(t, tmpDir, "neverindexed")
	add("neverindexed", p)

	// 4. Truncated graph.fb — the reader PANICS (#6013).
	p, sd = parityRepo(t, tmpDir, "truncated")
	fbPath := filepath.Join(sd, "graph.fb")
	if err := fbwriter.WriteAtomic(fbPath, parityDoc("truncated", "main", "aaaaaaaaaaaa", false)); err != nil {
		t.Fatalf("write graph.fb: %v", err)
	}
	raw, err := os.ReadFile(fbPath)
	if err != nil {
		t.Fatalf("read graph.fb: %v", err)
	}
	if err := os.WriteFile(fbPath, raw[:len(raw)/2], 0o644); err != nil {
		t.Fatalf("truncate graph.fb: %v", err)
	}
	add("truncated", p)

	// 5. graph.fb that fails to OPEN with a returned error (#6013).
	p, sd = parityRepo(t, tmpDir, "unreadable")
	if err := os.WriteFile(filepath.Join(sd, "graph.fb"), make([]byte, 512), 0o644); err != nil {
		t.Fatalf("write graph.fb: %v", err)
	}
	add("unreadable", p)

	// 6. Segment-set layout (graph.<gen>/ + manifest.json, no flat .fb).
	p, sd = parityRepo(t, tmpDir, "segmentset")
	writeParitySegmentSet(t, sd, parityDoc("segmentset", "main", "5e65e75e7000", false), 4)
	writeParitySidecar(t, sd, 6, 2, 7, 10*time.Minute)
	add("segmentset", p)

	// 7. graph.json only (the non-mmap fallback).
	p, sd = parityRepo(t, tmpDir, "jsononly")
	jdoc := parityDoc("jsononly", "main", "111122223333", false)
	jraw, _ := json.Marshal(jdoc)
	if err := os.WriteFile(filepath.Join(sd, "graph.json"), jraw, 0o644); err != nil {
		t.Fatalf("write graph.json: %v", err)
	}
	writeParitySidecar(t, sd, 6, 2, 7, time.Hour)
	add("jsononly", p)

	// 8. Healthy graph with a COLD ref slot alongside the hot one.
	p, sd = parityRepo(t, tmpDir, "coldrefs")
	if err := fbwriter.WriteAtomic(filepath.Join(sd, "graph.fb"), parityDoc("coldrefs", "main", "c01dc01dc01d", false)); err != nil {
		t.Fatalf("write graph.fb: %v", err)
	}
	writeParitySidecar(t, sd, 6, 2, 7, 25*time.Hour)
	coldDir := filepath.Join(filepath.Dir(sd), "develop")
	if err := os.MkdirAll(coldDir, 0o755); err != nil {
		t.Fatalf("mkdir cold: %v", err)
	}
	if err := fbwriter.WriteAtomic(filepath.Join(coldDir, "graph.fb"), parityDoc("coldrefs", "develop", "dddddddddddd", false)); err != nil {
		t.Fatalf("write cold graph.fb: %v", err)
	}
	add("coldrefs", p)

	// Every reported age must now come from a fixed offset, never from "now".
	backdateGraphArtefacts(t, tmpDir)

	summary := ComputeStatusSummary("parity", repos)
	var buf bytes.Buffer
	PrintStatusSummary(&buf, summary)
	got := buf.String()

	goldenPath := filepath.Join("testdata", "status_parity_5995.golden")
	if os.Getenv("GRAFEL_UPDATE_GOLDEN") == "1" {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatalf("mkdir testdata: %v", err)
		}
		if err := os.WriteFile(goldenPath, []byte(got), 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
		t.Logf("golden updated:\n%s", got)
		return
	}
	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden (regenerate with GRAFEL_UPDATE_GOLDEN=1): %v", err)
	}
	if got != string(want) {
		t.Fatalf("status output changed (#5995 must not alter reported values)\n--- want ---\n%s\n--- got ---\n%s", want, got)
	}
}
