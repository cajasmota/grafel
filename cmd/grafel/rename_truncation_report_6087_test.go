package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
)

// ─── #6087 — a truncated rename scan must be REPORTED, not silently dropped ──
//
// The bound itself is guarded in internal/algorithms. What is guarded here is
// the half nobody can see from inside that package: that a truncated pass is
// actually surfaced to the two audiences that consume an index —
//
//   - a human watching the indexer, via the stderr warning, and
//   - every programmatic consumer (MCP, the dashboard, `grafel doctor`), via
//     the graph-stats.json sidecar, which is the only machine-readable
//     artefact they read. Without the sidecar flag, truncation is non-silent
//     to a human tailing a terminal and completely silent to everything else.
//
// Deleting either report leaves the package building and every other test
// green, which is exactly why these exist.

// writeRenameFixtureRepo writes a tiny Go repo whose function names are all
// derived from nameFn, so two successive indexes with different nameFns
// produce a large added/deleted entity delta.
func writeRenameFixtureRepo(t *testing.T, dir string, n int, nameFn func(i int) string) {
	t.Helper()
	var b strings.Builder
	b.WriteString("package fixture\n\n")
	for i := 0; i < n; i++ {
		fmt.Fprintf(&b, "func %s() int { return %d }\n\n", nameFn(i), i)
	}
	if err := os.WriteFile(filepath.Join(dir, "fixture.go"), []byte(b.String()), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module fixture\n\ngo 1.21\n"), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
}

// indexCapturingStderr runs Index and returns everything it wrote to stderr.
//
// NOTE: this mutates the process-global os.Stderr handle, matching the
// existing convention in index_test.go. Do not run it in parallel.
func indexCapturingStderr(t *testing.T, repo, out string) string {
	t.Helper()

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	orig := os.Stderr
	os.Stderr = w

	done := make(chan string, 1)
	go func() {
		b, _ := io.ReadAll(r)
		done <- string(b)
	}()

	idxErr := Index(repo, out, "rename-fixture", nil, false, false)

	os.Stderr = orig
	_ = w.Close()
	captured := <-done
	_ = r.Close()

	if idxErr != nil {
		t.Fatalf("index: %v", idxErr)
	}
	return captured
}

// TestRenameDetect_TruncationIsReported drives a real two-pass index through
// the shipped CLI entry point with a work budget small enough to guarantee
// truncation, and asserts BOTH reports fire.
func TestRenameDetect_TruncationIsReported(t *testing.T) {
	t.Setenv("GRAFEL_DAEMON_ROOT", t.TempDir())
	// A budget of 1 unit cannot admit any candidate bucket, so any non-empty
	// delta truncates. The env override exists precisely so this path is
	// reachable in milliseconds instead of needing a four-billion-unit fixture.
	t.Setenv("GRAFEL_RENAME_WORK_BUDGET", "1")

	repo := t.TempDir()
	state := t.TempDir()
	out := filepath.Join(state, "graph.json")

	const n = 40
	// Pass 1 — establishes the prior graph.
	writeRenameFixtureRepo(t, repo, n, func(i int) string { return fmt.Sprintf("AlphaHandler%03d", i) })
	if err := Index(repo, out, "rename-fixture", nil, false, false); err != nil {
		t.Fatalf("first index: %v", err)
	}

	// Pass 2 — every function renamed, so every entity is both a deletion and
	// an addition: a delta the (tiny) budget cannot possibly cover.
	writeRenameFixtureRepo(t, repo, n, func(i int) string { return fmt.Sprintf("AlphaHandlers%03d", i) })
	stderr := indexCapturingStderr(t, repo, out)

	if !strings.Contains(stderr, "rename-detect: TRUNCATED") {
		t.Errorf("no truncation warning on stderr; a partial rename scan was reported as a complete one.\nstderr:\n%s", stderr)
	}
	if !strings.Contains(stderr, "INCOMPLETE") {
		t.Errorf("truncation warning does not say the result is incomplete.\nstderr:\n%s", stderr)
	}

	// The machine-readable half: every non-human consumer reads this file.
	var side graph.GraphStatsSidecar
	sideBytes, err := os.ReadFile(filepath.Join(state, "graph-stats.json"))
	if err != nil {
		t.Fatalf("read sidecar: %v", err)
	}
	if err := json.Unmarshal(sideBytes, &side); err != nil {
		t.Fatalf("parse sidecar: %v", err)
	}
	if !side.RenameDetectTruncated {
		t.Errorf("sidecar rename_detect_truncated=false after a truncated pass — "+
			"MCP/dashboard/doctor cannot tell the rename data is partial.\nsidecar: %s", sideBytes)
	}
	if side.RenameDetectAddedSkipped <= 0 {
		t.Errorf("sidecar rename_detect_added_skipped=%d, want >0", side.RenameDetectAddedSkipped)
	}
}

// TestRenameDetect_CompleteRunReportsNoTruncation is the other half: an
// ordinary index under the real default budget must not raise the alarm, or
// the flag means nothing.
func TestRenameDetect_CompleteRunReportsNoTruncation(t *testing.T) {
	t.Setenv("GRAFEL_DAEMON_ROOT", t.TempDir())

	repo := t.TempDir()
	state := t.TempDir()
	out := filepath.Join(state, "graph.json")

	const n = 40
	writeRenameFixtureRepo(t, repo, n, func(i int) string { return fmt.Sprintf("AlphaHandler%03d", i) })
	if err := Index(repo, out, "rename-fixture", nil, false, false); err != nil {
		t.Fatalf("first index: %v", err)
	}
	writeRenameFixtureRepo(t, repo, n, func(i int) string { return fmt.Sprintf("AlphaHandlers%03d", i) })
	stderr := indexCapturingStderr(t, repo, out)

	if strings.Contains(stderr, "TRUNCATED") {
		t.Errorf("ordinary index reported truncation under the default budget.\nstderr:\n%s", stderr)
	}

	var side graph.GraphStatsSidecar
	sideBytes, err := os.ReadFile(filepath.Join(state, "graph-stats.json"))
	if err != nil {
		t.Fatalf("read sidecar: %v", err)
	}
	if err := json.Unmarshal(sideBytes, &side); err != nil {
		t.Fatalf("parse sidecar: %v", err)
	}
	if side.RenameDetectTruncated {
		t.Errorf("sidecar reports truncation on a complete run: %s", sideBytes)
	}

	// And the renames themselves must actually be in the graph — a run that
	// reports "not truncated" while detecting nothing is the same wrong answer
	// wearing a different hat.
	doc, err := graph.LoadGraphFromDir(state)
	if err != nil {
		t.Fatalf("load graph: %v", err)
	}
	renames := 0
	for _, r := range doc.Relationships {
		if r.Kind == "RENAMED_FROM" {
			renames++
		}
	}
	if renames == 0 {
		t.Errorf("no RENAMED_FROM edges after renaming %d functions in place", n)
	}
}
