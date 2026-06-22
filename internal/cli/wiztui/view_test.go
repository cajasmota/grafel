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
