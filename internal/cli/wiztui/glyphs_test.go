package wiztui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/spinner"
)

// fakeEnv builds an envLookup from a map for deterministic selector tests.
func fakeEnv(m map[string]string) envLookup {
	return func(k string) (string, bool) {
		v, ok := m[k]
		return v, ok
	}
}

// TestPickGlyphs covers the ascii-vs-unicode decision matrix (#5340): legacy
// Windows defaults to ASCII, Windows Terminal stays Unicode, GRAFEL_ASCII forces
// ASCII anywhere, GRAFEL_TUI_UNICODE forces Unicode, non-Windows defaults Unicode.
func TestPickGlyphs(t *testing.T) {
	cases := []struct {
		name      string
		goos      string
		env       map[string]string
		wantASCII bool
	}{
		{"windows legacy CMD -> ASCII", "windows", nil, true},
		{"windows terminal -> Unicode", "windows", map[string]string{"WT_SESSION": "abc"}, false},
		{"windows + GRAFEL_TUI_UNICODE -> Unicode", "windows", map[string]string{"GRAFEL_TUI_UNICODE": "1"}, false},
		{"windows + GRAFEL_ASCII -> ASCII", "windows", map[string]string{"GRAFEL_ASCII": "1", "WT_SESSION": "x"}, true},
		{"linux default -> Unicode", "linux", nil, false},
		{"darwin default -> Unicode", "darwin", nil, false},
		{"linux + GRAFEL_ASCII -> ASCII", "linux", map[string]string{"GRAFEL_ASCII": "1"}, true},
		{"empty WT_SESSION on windows -> ASCII", "windows", map[string]string{"WT_SESSION": ""}, true},
		{"empty GRAFEL_ASCII ignored on linux -> Unicode", "linux", map[string]string{"GRAFEL_ASCII": ""}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := pickGlyphs(tc.goos, fakeEnv(tc.env))
			if got.ascii != tc.wantASCII {
				t.Errorf("pickGlyphs(%s, %v).ascii = %v, want %v", tc.goos, tc.env, got.ascii, tc.wantASCII)
			}
		})
	}
}

// TestGlyphSetsAreAllASCII asserts every glyph in the fallback set is pure ASCII
// (so it can never mojibake on legacy conhost).
func TestAsciiGlyphsArePureASCII(t *testing.T) {
	vals := []string{
		asciiGlyphs.Cursor, asciiGlyphs.RailSep, asciiGlyphs.Check, asciiGlyphs.Cross,
		asciiGlyphs.ArrowUp, asciiGlyphs.ArrowDown, asciiGlyphs.MidDot, asciiGlyphs.Ellipsis,
		asciiGlyphs.Warn, asciiGlyphs.BoxOn, asciiGlyphs.BoxOff, asciiGlyphs.Caret,
	}
	for _, v := range vals {
		for _, r := range v {
			if r > 127 {
				t.Errorf("ascii glyph %q contains non-ASCII rune %q", v, r)
			}
		}
	}
}

// TestSpinnerSelection asserts the ASCII set uses the Line spinner and the
// Unicode set the braille Dot spinner.
func TestSpinnerSelection(t *testing.T) {
	if got := asciiGlyphs.Spinner(); got.Frames[0] != spinner.Line.Frames[0] {
		t.Errorf("ascii spinner = %v, want Line", got.Frames)
	}
	if got := unicodeGlyphs.Spinner(); got.Frames[0] != spinner.Dot.Frames[0] {
		t.Errorf("unicode spinner = %v, want Dot", got.Frames)
	}
}

// withGlyphs runs fn with the active glyph set swapped to gs, restoring after.
func withGlyphs(gs glyphSet, fn func()) {
	prev := g
	g = gs
	defer func() { g = prev }()
	fn()
}

// TestViewsRenderWithActiveSet asserts that when the ASCII set is active, the
// rendered views contain NO raw Unicode glyphs (the mojibake risk) and DO
// contain their ASCII equivalents (#5340).
func TestViewsRenderWithActiveSet(t *testing.T) {
	unicodeRunes := []string{"›", "✓", "✗", "↑", "↓", "·", "…", "⚠", "█"}

	withGlyphs(asciiGlyphs, func() {
		// Header step rail (rail separator + done check).
		h := header(StepSelect, 80)
		assertNoUnicode(t, "header", h, unicodeRunes)

		// List view (cursor) + multiselect view (checkboxes + cursor).
		lm := newListModel("pick", []Candidate{{Label: "a"}, {Label: "b"}})
		assertNoUnicode(t, "listModel.view", lm.view(10), unicodeRunes)

		mm := newMultiListModel("pick", []Candidate{{Label: "a", Selected: true}, {Label: "b"}})
		mv := mm.view(10)
		assertNoUnicode(t, "multiListModel.view", mv, unicodeRunes)
		if !strings.Contains(mv, "[x] ") {
			t.Errorf("ascii multiselect missing [x] checkbox:\n%s", mv)
		}

		// Hint strings.
		for _, hint := range []string{hintList(), hintMulti(), hintInput(), hintInputOpt(), hintIndex(), hintDone()} {
			assertNoUnicode(t, "hint", hint, unicodeRunes)
		}

		// Index view done summary (check + middot + warning).
		iv := newIndexView("grp", 1)
		iv.terminal = true
		iv.summaryEntities = 5
		iv.install = InstallSummary{Applied: true, Hooks: 1, WatcherWarnings: []string{"watcher not active"}}
		// Exclude █: the bubbles/progress bar uses block chars by design (they
		// render fine on Windows) and are intentionally left as-is (#5340).
		assertNoUnicode(t, "indexView.view", iv.view(), unicodeRunesNoBlock(unicodeRunes))
	})

	// Unicode set must include the rich glyphs.
	withGlyphs(unicodeGlyphs, func() {
		h := header(StepSelect, 80)
		if !strings.Contains(h, "✓") {
			t.Errorf("unicode header missing ✓:\n%s", h)
		}
		lm := newListModel("pick", []Candidate{{Label: "a"}})
		if !strings.Contains(lm.view(10), "›") {
			t.Errorf("unicode list missing › cursor")
		}
	})
}

// unicodeRunesNoBlock returns the rune list without the █ block char (the
// progress bar legitimately uses it).
func unicodeRunesNoBlock(runes []string) []string {
	out := make([]string, 0, len(runes))
	for _, r := range runes {
		if r == "█" {
			continue
		}
		out = append(out, r)
	}
	return out
}

func assertNoUnicode(t *testing.T, where, s string, runes []string) {
	t.Helper()
	for _, r := range runes {
		if strings.Contains(s, r) {
			t.Errorf("%s rendered raw Unicode glyph %q in ASCII mode:\n%s", where, r, s)
		}
	}
}
