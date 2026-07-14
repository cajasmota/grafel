package wiztui

import (
	"strings"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/progress"
)

// TestRenderRow_DistinctModulesRenderDistinctLabels is the RED test for the
// readable-label bug: a monorepo's RepoSlug (e.g. "some-example-monorepo-main",
// 26 chars) filled the whole slug column, so truncate() cut every module row
// down to an identical truncated prefix — the distinguishing Module name was
// truncated away. Two rows sharing the same (long) RepoSlug but different
// Modules must render VISIBLY DIFFERENT labels.
func TestRenderRow_DistinctModulesRenderDistinctLabels(t *testing.T) {
	v := newIndexView("example", 2)
	rowA := Row{RepoSlug: "some-example-monorepo-main", Module: "billing-service", Phase: progress.PhaseExtractAST}
	rowB := Row{RepoSlug: "some-example-monorepo-main", Module: "billing-worker", Phase: progress.PhaseExtractAST}

	spinner := "*"
	lineA := v.renderRow(rowA, spinner)
	lineB := v.renderRow(rowB, spinner)

	if lineA == lineB {
		t.Fatalf("rows with the same RepoSlug but different Module rendered identically:\n%s", lineA)
	}
	if !strings.Contains(lineA, "billing-service") {
		t.Errorf("row A missing its distinguishing module name:\n%s", lineA)
	}
	if !strings.Contains(lineB, "billing-worker") {
		t.Errorf("row B missing its distinguishing module name:\n%s", lineB)
	}
}

// TestRenderRow_PlainRepoUsesRepoSlug asserts a plain group repo (Module=="")
// still labels itself with RepoSlug, unaffected by the module-label fix.
func TestRenderRow_PlainRepoUsesRepoSlug(t *testing.T) {
	v := newIndexView("grp", 1)
	row := Row{RepoSlug: "backend", Phase: progress.PhaseExtractAST}
	line := v.renderRow(row, "*")
	if !strings.Contains(line, "backend") {
		t.Errorf("plain repo row missing RepoSlug label:\n%s", line)
	}
}

// TestIndexView_HeaderShowsElapsedWhileIndexing: while indexing (not done),
// the header includes a live elapsed segment computed from startedAt to now.
func TestIndexView_HeaderShowsElapsedWhileIndexing(t *testing.T) {
	v := newIndexView("grp", 1)
	v.width = 100
	v.startedAt = time.Now().Add(-(2*time.Minute + 5*time.Second))
	v.foldEvent(progress.Event{RepoSlug: "backend", Phase: progress.PhaseExtractAST, TS: 1})

	out := v.view()
	if !strings.Contains(out, "2m05s") {
		t.Errorf("header missing live elapsed segment \"2m05s\":\n%s", out)
	}
}

// TestIndexView_HeaderFreezesElapsedOnDone: once terminal, the header shows
// the FROZEN elapsed (startedAt..finishedAt), not a value that keeps growing
// with wall-clock time.
func TestIndexView_HeaderFreezesElapsedOnDone(t *testing.T) {
	v := newIndexView("grp", 1)
	v.width = 100
	v.startedAt = time.Now().Add(-10 * time.Minute)  // long ago
	v.finishedAt = v.startedAt.Add(65 * time.Second) // but it only took 1m05s
	v.terminal = true

	out := v.view()
	if !strings.Contains(out, "1m05s") {
		t.Errorf("header missing frozen elapsed \"1m05s\":\n%s", out)
	}
	if strings.Contains(out, "10m00s") {
		t.Errorf("header shows live wall-clock elapsed instead of the frozen finishedAt-startedAt value:\n%s", out)
	}
}

// TestIndexView_QueryableBanner_ShownWhenQueryableNotTerminal: once the graph
// is queryable but the background enhancement pass hasn't acked yet, the view
// shows the "graph queryable" banner + the press-enter-or-wait hint instead of
// jumping straight to the Done summary.
func TestIndexView_QueryableBanner_ShownWhenQueryableNotTerminal(t *testing.T) {
	v := newIndexView("grp", 1)
	v.width = 100
	v.foldEvent(progress.Event{RepoSlug: "backend", Phase: progress.PhaseDone, TS: 1})
	v.queryable = true
	v.summaryEntities = 4200

	out := v.view()
	if !strings.Contains(out, "Graph queryable") {
		t.Errorf("queryable banner missing:\n%s", out)
	}
	if !strings.Contains(out, "4,200 entities") {
		t.Errorf("queryable banner missing commafied entity count:\n%s", out)
	}
	if !strings.Contains(out, "enhancing relationships in the background") {
		t.Errorf("queryable banner missing background-enhancement note:\n%s", out)
	}
	if !strings.Contains(out, "press enter to finish now") {
		t.Errorf("queryable hint missing:\n%s", out)
	}
	if strings.Contains(out, "installed") { // doneSummary's install line must NOT render pre-terminal
		t.Errorf("Done summary rendered while only queryable (not terminal):\n%s", out)
	}
}

// TestIndexView_DoneSummary_Commafied asserts doneSummary formats entities and
// relationships with thousands separators.
func TestIndexView_DoneSummary_Commafied(t *testing.T) {
	v := newIndexView("grp", 1)
	v.terminal = true
	v.summaryEntities = 3686488
	v.summaryRels = 1000
	out := v.doneSummary()
	if !strings.Contains(out, "3,686,488 entities") {
		t.Errorf("doneSummary missing commafied entities:\n%s", out)
	}
	if !strings.Contains(out, "1,000 relationships") {
		t.Errorf("doneSummary missing commafied relationships:\n%s", out)
	}
}

// TestRenderRow_FilesCountCommafied asserts the per-row files-done/total and
// entities-so-far counters render with thousands separators.
func TestRenderRow_FilesCountCommafied(t *testing.T) {
	v := newIndexView("grp", 1)
	row := Row{RepoSlug: "backend", Phase: progress.PhaseExtractAST, FilesDone: 4100, FilesTotal: 19450, EntitiesSoFar: 12345}
	line := v.renderRow(row, "*")
	if !strings.Contains(line, "4,100/19,450 files") {
		t.Errorf("renderRow missing commafied files count:\n%s", line)
	}
	if !strings.Contains(line, "12,345 entities") {
		t.Errorf("renderRow missing commafied entities count:\n%s", line)
	}
}
