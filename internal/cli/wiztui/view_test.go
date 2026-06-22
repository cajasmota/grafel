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
