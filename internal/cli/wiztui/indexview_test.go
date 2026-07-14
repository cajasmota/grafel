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

// --- Dropped-row regression coverage (#seed-rows fix) ---
//
// Live bug: a 3-repo group completed with only 2 rows ever rendered because
// per-repo rows were created ONLY when a progress event for that repo
// arrived. A repo whose events were missed/dropped/raced never got a row,
// even though it indexed successfully and its watcher was installed. The fix
// is two-part: (1) seed a "queued" row for every selected repo up front via a
// PhaseQueued event that merges-by-slug with real events, and (2) overlay the
// split-mode classify's authoritative per-repo stats on completion so a
// silent repo still shows its true count + Done.

// TestFoldEvent_SeededQueuedRowRendersDistinctly: a PhaseQueued seed event
// creates a row immediately, rendered as a muted "Queued…" — not the active
// spinner (which would misleadingly suggest work in progress).
func TestFoldEvent_SeededQueuedRowRendersDistinctly(t *testing.T) {
	v := newIndexView("grp", 3)
	v.foldEvent(progress.Event{RepoSlug: "core-mobile", Phase: PhaseQueued, TS: 0})

	row, ok := v.rows["core-mobile"]
	if !ok {
		t.Fatal("seeded PhaseQueued event did not create a row")
	}
	line := v.renderRow(row, "*SPINNER*")
	if !strings.Contains(line, "Queued") {
		t.Errorf("queued row missing the Queued label:\n%s", line)
	}
	if strings.Contains(line, "*SPINNER*") {
		t.Errorf("queued row rendered the active spinner (should be suppressed):\n%s", line)
	}
}

// TestFoldEvent_SeededRowMergesWithRealEvent: a seeded row and a subsequent
// real progress event for the SAME slug fold into ONE row (no duplicate),
// and the row advances past PhaseQueued.
func TestFoldEvent_SeededRowMergesWithRealEvent(t *testing.T) {
	v := newIndexView("grp", 1)
	v.foldEvent(progress.Event{RepoSlug: "backend", Phase: PhaseQueued, TS: 0})
	if len(v.rows) != 1 {
		t.Fatalf("after seed: len(rows) = %d, want 1", len(v.rows))
	}
	v.foldEvent(progress.Event{RepoSlug: "backend", Phase: progress.PhaseExtractAST, FilesDone: 5, FilesTotal: 10, TS: 1})

	if len(v.rows) != 1 {
		t.Fatalf("after real event: len(rows) = %d, want 1 (no duplicate row)", len(v.rows))
	}
	row := v.rows["backend"]
	if row.Phase != progress.PhaseExtractAST {
		t.Errorf("Phase = %q, want %q (real event must advance past queued)", row.Phase, progress.PhaseExtractAST)
	}
	if row.FilesDone != 5 {
		t.Errorf("FilesDone = %d, want 5", row.FilesDone)
	}
}

// TestFoldEvent_SeededRowSurvivesGroupSlugCollision: when a repo's slug
// equals the group name (the common single-repo-group default), the
// PhaseQueued seed must still create a row — it must NOT be swallowed by the
// group-scoped-event guard the way a same-named real event would be.
func TestFoldEvent_SeededRowSurvivesGroupSlugCollision(t *testing.T) {
	v := newIndexView("myrepo", 1) // group name == repo slug
	v.foldEvent(progress.Event{RepoSlug: "myrepo", Phase: PhaseQueued, TS: 0})

	if _, ok := v.rows["myrepo"]; !ok {
		t.Fatal("seeded row for a repo whose slug matches the group name was swallowed by the group-scope guard")
	}
	if v.groupPhase != "" {
		t.Errorf("groupPhase = %q, want empty (a seed event must never be treated as a group-scoped event)", v.groupPhase)
	}
}

// TestApplyRepoStats_PopulatesRowThatNeverReportedProgress is the exact
// regression for the live bug: a selected repo that emits ZERO progress
// events must still end up with its real entity count and a Done row once
// applyRepoStats runs — sourced from the classify, not from folded SSE.
func TestApplyRepoStats_PopulatesRowThatNeverReportedProgress(t *testing.T) {
	v := newIndexView("grp", 3)
	// Seed all three (part 1) — only two ever report real progress.
	for _, slug := range []string{"core-mobile", "upvate_core", "upvate_core_frontend"} {
		v.foldEvent(progress.Event{RepoSlug: slug, Phase: PhaseQueued, TS: 0})
	}
	v.foldEvent(progress.Event{RepoSlug: "core-mobile", Phase: progress.PhaseDone, EntitiesSoFar: 8383, TS: 1})
	v.foldEvent(progress.Event{RepoSlug: "upvate_core", Phase: progress.PhaseDone, EntitiesSoFar: 6039, TS: 1})
	// upvate_core_frontend NEVER reports — stays queued until the classify lands.

	if len(v.rows) != 3 {
		t.Fatalf("len(rows) = %d, want 3 (a row must exist for every selected repo)", len(v.rows))
	}
	frozen := v.rows["upvate_core_frontend"]
	if frozen.Phase != PhaseQueued {
		t.Fatalf("precondition: silent repo's row should still be queued before the classify lands, got phase %q", frozen.Phase)
	}

	// Part 2: the split-mode classify's authoritative stats land on completion.
	v.applyRepoStats([]RepoStat{
		{Slug: "core-mobile", Entities: 8383},
		{Slug: "upvate_core", Entities: 6039},
		{Slug: "upvate_core_frontend", Entities: 17270},
	})
	v.finalizeRows()
	v.terminal = true

	row, ok := v.rows["upvate_core_frontend"]
	if !ok {
		t.Fatal("silent repo's row disappeared instead of being populated")
	}
	if row.Phase != progress.PhaseDone {
		t.Errorf("Phase = %q, want Done", row.Phase)
	}
	if row.EntitiesSoFar != 17270 {
		t.Errorf("EntitiesSoFar = %d, want 17270 (the repo's TRUE count, sourced from classify)", row.EntitiesSoFar)
	}

	// The Done summary aggregate must equal the sum of the per-repo rows — no
	// silent shortfall (the exact live symptom: rows summed to 14,422 but the
	// reported total was 31,692).
	var sum int64
	for _, r := range v.rows {
		sum += int64(r.EntitiesSoFar)
	}
	const wantTotal = 8383 + 6039 + 17270
	if sum != wantTotal {
		t.Errorf("sum of per-repo EntitiesSoFar = %d, want %d (matches the aggregate total)", sum, wantTotal)
	}
}

// TestApplyRepoStats_FailedRepoRendersError: a classify-reported failure
// marks the row Error/failed rather than a false Done.
func TestApplyRepoStats_FailedRepoRendersError(t *testing.T) {
	v := newIndexView("grp", 1)
	v.foldEvent(progress.Event{RepoSlug: "docs-only", Phase: PhaseQueued, TS: 0})
	v.applyRepoStats([]RepoStat{{Slug: "docs-only", Failed: true, Error: "produced no graph"}})

	row := v.rows["docs-only"]
	if row.Phase != progress.PhaseError {
		t.Errorf("Phase = %q, want Error", row.Phase)
	}
	if row.Error != "produced no graph" {
		t.Errorf("Error = %q, want %q", row.Error, "produced no graph")
	}
}

// TestApplyRepoStats_DoesNotDowngradeDoneRowToError: a repo that already
// reported Done over SSE must NOT be flipped to Error by a classify overlay
// that (e.g. on a status-plane mtime/ack race) transiently reports it failed.
// The live SSE stream that saw it finish is more authoritative.
func TestApplyRepoStats_DoesNotDowngradeDoneRowToError(t *testing.T) {
	v := newIndexView("grp", 1)
	// The repo reported Done via SSE, with a real entity count.
	v.foldEvent(progress.Event{RepoSlug: "backend", Phase: progress.PhaseDone, EntitiesSoFar: 4200, TS: 1})
	// A racy classify claims it failed.
	v.applyRepoStats([]RepoStat{{Slug: "backend", Failed: true, Error: "produced no graph"}})

	row := v.rows["backend"]
	if row.Phase != progress.PhaseDone {
		t.Errorf("Phase = %q, want Done (must not downgrade an SSE-reported Done to Error)", row.Phase)
	}
	if row.Error != "" {
		t.Errorf("Error = %q, want empty (no error stamped on a Done row)", row.Error)
	}
	if row.EntitiesSoFar != 4200 {
		t.Errorf("EntitiesSoFar = %d, want 4200 (preserved from the SSE Done)", row.EntitiesSoFar)
	}
}

// TestMonorepo_PerModuleRowsSumToAggregate_NoSpuriousRepoRow documents the
// intended monorepo rendering the cli-layer gate protects: per-module rows
// (keyed monorepoSlug/module) render individually, finalizeRows flips them to
// Done, and NO bare repo-level row (key == monorepoSlug) is ever created — so
// the visible rows sum to the aggregate exactly ONCE, never 2×. applyRepoStats
// is intentionally NOT called here (the cli layer passes nil RepoStats for a
// monorepo).
func TestMonorepo_PerModuleRowsSumToAggregate_NoSpuriousRepoRow(t *testing.T) {
	const mono = "some-monorepo"
	v := newIndexView("some-monorepo-group", 1)
	// Per-module progress (the #5751 model): distinct Module, shared RepoSlug.
	v.foldEvent(progress.Event{RepoSlug: mono, Module: "services/auth", Phase: progress.PhaseDone, EntitiesSoFar: 1200, TS: 1})
	v.foldEvent(progress.Event{RepoSlug: mono, Module: "packages/ui", Phase: progress.PhaseDone, EntitiesSoFar: 800, TS: 1})
	v.foldEvent(progress.Event{RepoSlug: mono, Module: "services/billing", Phase: progress.PhaseExtractAST, EntitiesSoFar: 0, TS: 1})

	// Monorepo path passes NO RepoStats — the aggregate lives only in the header.
	v.applyRepoStats(nil)
	v.finalizeRows()
	v.terminal = true

	if _, spurious := v.rows[mono]; spurious {
		t.Fatalf("a spurious bare repo-level row (key %q) was created — this doubles the entity total", mono)
	}
	if len(v.rows) != 3 {
		t.Fatalf("len(rows) = %d, want 3 (one per module, no repo-level row)", len(v.rows))
	}
	var sum int
	for _, r := range v.rows {
		if r.Phase != progress.PhaseDone {
			t.Errorf("module row %q not finalized to Done: %q", r.Key, r.Phase)
		}
		sum += r.EntitiesSoFar
	}
	if sum != 2000 {
		t.Errorf("sum of module rows = %d, want 2000 (aggregate counted exactly once, not doubled)", sum)
	}
}
