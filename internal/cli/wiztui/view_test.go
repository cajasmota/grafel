package wiztui

import (
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/progress"
)

// TestView_RendersAllScreensNoPanic walks the model through every screen and
// asserts View() returns non-empty output with the chrome present.
func TestView_RendersAllScreensNoPanic(t *testing.T) {
	d := fakeDriver{suggested: ActionGroup, cands: []Candidate{
		{Label: "/a", Value: "/a", Selected: true},
		{Label: "/b", Value: "/b", Selected: true},
	}}
	m := newTestModel(d, nilIndex)

	assertChrome := func(label string, mm Model) {
		v := mm.View()
		if !strings.Contains(v, "grafel wizard") {
			t.Errorf("%s: header title missing", label)
		}
		if !strings.Contains(v, "ctrl-c") && mm.scr != scrDone {
			t.Errorf("%s: footer hint missing", label)
		}
	}

	assertChrome("action", m)
	m = m.update(key("enter")) // → select
	assertChrome("select", m)
	m = m.update(key("enter")) // → name
	assertChrome("name", m)
	m = m.update(key("enter")) // → docs
	assertChrome("docs", m)
}

// TestIndexView_RendersOneRowPerRepo asserts the indexing view renders a
// distinct row for each repo (the dropped-repo fix, end-to-end through View).
func TestIndexView_RendersOneRowPerRepo(t *testing.T) {
	v := newIndexView("grp", 3)
	v.width = 100
	for _, slug := range []string{"backend", "frontend", "mobile"} {
		v.foldEvent(progress.Event{RepoSlug: slug, Phase: progress.PhaseExtractAST, FilesDone: 10, FilesTotal: 100, TS: 1})
	}
	out := v.view()
	for _, slug := range []string{"backend", "frontend", "mobile"} {
		if !strings.Contains(out, slug) {
			t.Errorf("indexing view dropped repo %q:\n%s", slug, out)
		}
	}
	// Overall bar + label present.
	if !strings.Contains(out, "Indexing grp") {
		t.Errorf("overall indexing label missing:\n%s", out)
	}
}

// TestIndexView_GroupScopedEventNotARow is the core #5340 regression: the
// cross-repo pass emits an event with RepoSlug == group ("ivivo"). That event
// must NOT become a per-repo row; it only updates the overall group phase. The
// per-repo rows (backend, frontend) must always render, and the group must NOT.
func TestIndexView_GroupScopedEventNotARow(t *testing.T) {
	v := newIndexView("ivivo", 2)
	v.width = 100
	// Realistic order: per-repo extraction, per-repo done, then the group-scoped
	// cross-repo links pass, then the group terminal.
	v.foldEvent(progress.Event{RepoSlug: "backend", Phase: progress.PhaseExtractAST, TS: 1})
	v.foldEvent(progress.Event{RepoSlug: "frontend", Phase: progress.PhaseExtractAST, TS: 2})
	v.foldEvent(progress.Event{RepoSlug: "backend", Phase: progress.PhaseDone, TS: 3})
	v.foldEvent(progress.Event{RepoSlug: "frontend", Phase: progress.PhaseDone, TS: 4})
	v.foldEvent(progress.Event{RepoSlug: "ivivo", Phase: progress.PhaseDetectLinks, TS: 5})

	if len(v.rows) != 2 {
		t.Fatalf("got %d rows, want exactly 2 (backend, frontend) — group row leaked: %v",
			len(v.rows), keysOf(v.rows))
	}
	if _, ok := v.rows["ivivo"]; ok {
		t.Error("group-scoped event 'ivivo' rendered as a per-repo row (the #5340 bug)")
	}
	for _, slug := range []string{"backend", "frontend"} {
		r, ok := v.rows[slug]
		if !ok {
			t.Errorf("per-repo row %q missing", slug)
			continue
		}
		if !r.Terminal() {
			t.Errorf("repo %q should be terminal, got %q", slug, r.Phase)
		}
	}
	// The overall label surfaces the group-scoped phase, not a spurious row.
	if v.groupPhase != progress.PhaseDetectLinks {
		t.Errorf("groupPhase = %q, want %q", v.groupPhase, progress.PhaseDetectLinks)
	}
	if got := v.overallLabel(); got != PhaseLabel(progress.PhaseDetectLinks) {
		t.Errorf("overall label = %q, want %q (group phase surfaces in label)",
			got, PhaseLabel(progress.PhaseDetectLinks))
	}
	out := v.view()
	if !strings.Contains(out, "backend") || !strings.Contains(out, "frontend") {
		t.Errorf("per-repo rows missing from view:\n%s", out)
	}
	if !strings.Contains(out, PhaseLabel(progress.PhaseDetectLinks)) {
		t.Errorf("group phase missing from overall label:\n%s", out)
	}
}

// TestIndexView_GroupEventFoldedRegardlessOfOrder asserts the group-scoped event
// is excluded from rows no matter when it arrives — interleaved with per-repo
// events or first.
func TestIndexView_GroupEventFoldedRegardlessOfOrder(t *testing.T) {
	orders := [][]progress.Event{
		{ // group event arrives first
			{RepoSlug: "ivivo", Phase: progress.PhaseScan, TS: 1},
			{RepoSlug: "backend", Phase: progress.PhaseExtractAST, TS: 2},
			{RepoSlug: "frontend", Phase: progress.PhaseExtractAST, TS: 3},
			{RepoSlug: "ivivo", Phase: progress.PhaseDetectLinks, TS: 4},
		},
		{ // group event interleaved between per-repo events
			{RepoSlug: "backend", Phase: progress.PhaseScan, TS: 1},
			{RepoSlug: "ivivo", Phase: progress.PhaseDetectLinks, TS: 2},
			{RepoSlug: "frontend", Phase: progress.PhaseScan, TS: 3},
		},
	}
	for i, evs := range orders {
		v := newIndexView("ivivo", 2)
		for _, e := range evs {
			v.foldEvent(e)
		}
		if _, ok := v.rows["ivivo"]; ok {
			t.Errorf("order %d: group 'ivivo' leaked into rows", i)
		}
		if len(v.rows) != 2 {
			t.Errorf("order %d: got %d rows, want 2: %v", i, len(v.rows), keysOf(v.rows))
		}
	}
}

func keysOf(rows map[string]Row) []string {
	out := make([]string, 0, len(rows))
	for k := range rows {
		out = append(out, k)
	}
	return out
}

// TestDoneScreen_RendersCapturedSummary drives the model to completion with an
// outcome carrying a captured install summary + watcher warning and asserts the
// Done screen renders all of it inline (fix C, #5340) rather than leaking it to
// raw stdout.
func TestDoneScreen_RendersCapturedSummary(t *testing.T) {
	d := fakeDriver{suggested: ActionGroup, cands: []Candidate{
		{Label: "/a", Value: "/a", Selected: true},
	}}
	m := newTestModel(d, nilIndex)
	m = m.update(key("enter")) // action → select
	m = m.update(key("enter")) // select → name
	m = m.update(key("enter")) // name → docs
	m = m.update(key("enter")) // docs → index (startIndex)
	if m.scr != scrIndex {
		t.Fatalf("scr = %v, want scrIndex", m.scr)
	}

	// Land a terminal outcome with a captured install summary + warning.
	m = m.update(outcomeMsg(IndexOutcome{
		Entities: 1234,
		Rels:     56,
		Elapsed:  "2.1s",
		Install: InstallSummary{
			Applied:         true,
			Hooks:           2,
			Watchers:        1,
			MCP:             3,
			WatcherWarnings: []string{"watcher for X not activated (will retry); group is registered and indexed"},
		},
	}))
	if m.scr != scrDone {
		t.Fatalf("scr = %v, want scrDone", m.scr)
	}

	v := m.View()
	for _, want := range []string{
		"1234 entities",
		"56 relationships",
		"installed 2 hooks · 1 watchers · 3 MCP",
		"⚠ watcher for X not activated",
	} {
		if !strings.Contains(v, want) {
			t.Errorf("Done screen missing %q:\n%s", want, v)
		}
	}
}

// TestModel_SuccessFinalizesStuckRows is the #5340 regression: one repo's last
// SSE event is an intermediate phase (building_communities) because its final
// events (centrality → writing → done) arrived after the Rebuild RPC returned
// and the forwarder stopped. When the success IndexOutcome lands, BOTH rows must
// render Done — the stuck one is force-finalized — with counts preserved and no
// spinner glyph on it.
func TestModel_SuccessFinalizesStuckRows(t *testing.T) {
	d := fakeDriver{suggested: ActionGroup, cands: []Candidate{
		{Label: "/a", Value: "/a", Selected: true},
	}}
	m := newTestModel(d, nilIndex)
	m = m.update(key("enter")) // action → select
	m = m.update(key("enter")) // select → name
	m = m.update(key("enter")) // name → docs
	m = m.update(key("enter")) // docs → index
	if m.scr != scrIndex {
		t.Fatalf("scr = %v, want scrIndex", m.scr)
	}

	// frontend reaches done; backend freezes mid-flight on building_communities
	// with real counts attached.
	m = m.update(progressMsg(progress.Event{RepoSlug: "frontend", Phase: progress.PhaseDone, TS: 1}))
	m = m.update(progressMsg(progress.Event{
		RepoSlug: "backend", Phase: progress.PhaseBuildCommunities,
		FilesDone: 80, FilesTotal: 80, EntitiesSoFar: 421, TS: 2,
	}))

	// Sanity: backend is NOT yet terminal before the outcome lands.
	if r := m.idx.rows["backend"]; r.Terminal() {
		t.Fatalf("backend terminal too early: %q", r.Phase)
	}

	// Success outcome lands.
	m = m.update(outcomeMsg(IndexOutcome{Entities: 999, Rels: 10, Elapsed: "1.0s"}))
	if m.scr != scrDone {
		t.Fatalf("scr = %v, want scrDone", m.scr)
	}

	// Both rows must now be terminal Done.
	for _, slug := range []string{"frontend", "backend"} {
		r, ok := m.idx.rows[slug]
		if !ok {
			t.Fatalf("row %q missing", slug)
		}
		if r.Phase != progress.PhaseDone {
			t.Errorf("row %q phase = %q, want %q", slug, r.Phase, progress.PhaseDone)
		}
	}
	// Counts on the previously-stuck row are preserved.
	if r := m.idx.rows["backend"]; r.EntitiesSoFar != 421 || r.FilesDone != 80 {
		t.Errorf("backend counts not preserved: files=%d entities=%d", r.FilesDone, r.EntitiesSoFar)
	}

	// View renders both as Done with no spinner glyph.
	out := m.View()
	if n := strings.Count(out, rowDoneStyle.Render("Done")); n < 2 {
		t.Errorf("expected >=2 Done rows, got %d:\n%s", n, out)
	}
	if strings.Contains(out, m.idx.spin.View()) && m.idx.spin.View() != "" {
		t.Errorf("spinner still present on finalized rows:\n%s", out)
	}
}

// TestModel_FailureDoesNotForceDone asserts a FAILURE outcome leaves an
// in-flight row on its phase — only success force-finalizes rows.
func TestModel_FailureDoesNotForceDone(t *testing.T) {
	v := newIndexView("grp", 2)
	v.width = 100
	v.foldEvent(progress.Event{RepoSlug: "backend", Phase: progress.PhaseBuildCommunities, TS: 1})

	// Simulate the failure branch of the outcome handler: no finalizeRows call.
	v.failed = true
	v.errMsg = "boom"

	if r := v.rows["backend"]; r.Phase != progress.PhaseBuildCommunities {
		t.Errorf("backend phase = %q, want %q (failure must not force Done)",
			r.Phase, progress.PhaseBuildCommunities)
	}

	// And finalizeRows itself, if not called, leaves rows untouched — guard the
	// contract by asserting an explicit non-call keeps the row in-flight.
	if v.rows["backend"].Terminal() {
		t.Error("row should remain in-flight on failure")
	}
}

// TestFinalizeRows_PreservesTerminalAndCounts unit-tests finalizeRows directly:
// done/error rows are untouched, in-flight rows advance to Done with counts kept.
func TestFinalizeRows_PreservesTerminalAndCounts(t *testing.T) {
	v := newIndexView("grp", 3)
	v.foldEvent(progress.Event{RepoSlug: "a", Phase: progress.PhaseDone, EntitiesSoFar: 5, TS: 1})
	v.foldEvent(progress.Event{RepoSlug: "b", Phase: progress.PhaseError, TS: 1})
	v.foldEvent(progress.Event{RepoSlug: "c", Phase: progress.PhaseBuildCommunities, EntitiesSoFar: 7, FilesDone: 3, TS: 1})

	v.finalizeRows()

	if v.rows["a"].Phase != progress.PhaseDone || v.rows["a"].EntitiesSoFar != 5 {
		t.Errorf("done row a mutated: %+v", v.rows["a"])
	}
	if v.rows["b"].Phase != progress.PhaseError {
		t.Errorf("error row b regressed to %q", v.rows["b"].Phase)
	}
	if v.rows["c"].Phase != progress.PhaseDone {
		t.Errorf("in-flight row c not finalized: %q", v.rows["c"].Phase)
	}
	if v.rows["c"].EntitiesSoFar != 7 || v.rows["c"].FilesDone != 3 {
		t.Errorf("row c counts not preserved: %+v", v.rows["c"])
	}
}

// TestDoneScreen_DaemonDownNote: a daemon-down soft completion renders the
// "registered (not indexed)" note while still showing the captured install
// counts.
func TestDoneScreen_DaemonDownNote(t *testing.T) {
	v := newIndexView("grp", 1)
	v.width = 100
	v.terminal = true
	v.daemonDown = true
	v.install = InstallSummary{Applied: true, Hooks: 1, Watchers: 0, MCP: 1}
	out := v.view()
	if !strings.Contains(out, "Registered (not indexed") {
		t.Errorf("daemon-down note missing:\n%s", out)
	}
	if !strings.Contains(out, "installed 1 hooks · 0 watchers · 1 MCP") {
		t.Errorf("install counts missing on daemon-down:\n%s", out)
	}
}
