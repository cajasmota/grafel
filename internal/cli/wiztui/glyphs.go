package wiztui

// glyphs.go centralizes every non-ASCII display glyph the wizard TUI renders so
// the whole package can switch between a Unicode set (rendered on capable
// terminals) and an ASCII-safe fallback set (legacy Windows CMD / conhost,
// which would otherwise show mojibake) from a single decision point (#5340).
//
// The active set is chosen ONCE at process start by pickGlyphs (see below) and
// exposed as the package-level `g`. Tests override `g` directly.

import (
	"os"
	"runtime"

	"github.com/charmbracelet/bubbles/spinner"
)

// glyphSet is the full set of display glyphs used across the wizard TUI. Every
// renderer references this set instead of hardcoding a rune, so flipping
// ascii-vs-unicode is a single swap.
type glyphSet struct {
	ascii bool // true for the ASCII fallback set

	Cursor    string // selection cursor / input prompt ("› " vs "> ")
	RailSep   string // step-rail separator ("›" vs ">")
	Check     string // done/selected mark ("✓" vs "v")
	Cross     string // error mark ("✗" vs "x")
	ArrowUp   string // hint up arrow ("↑" vs "^")
	ArrowDown string // hint down arrow ("↓" vs "v")
	MidDot    string // separator between hint/summary segments ("·" vs "-")
	Ellipsis  string // trailing ellipsis ("…" vs "...")
	Warn      string // watcher-warning mark ("⚠" vs "!")
	BoxOn     string // checked multiselect box ("[✓] " vs "[x] ")
	BoxOff    string // unchecked multiselect box ("[ ] ")
	Caret     string // text-input caret block ("█" vs "_")
}

// unicodeGlyphs is the rich set used on capable terminals.
var unicodeGlyphs = glyphSet{
	ascii:     false,
	Cursor:    "› ",
	RailSep:   "›",
	Check:     "✓",
	Cross:     "✗",
	ArrowUp:   "↑",
	ArrowDown: "↓",
	MidDot:    "·",
	Ellipsis:  "…",
	Warn:      "⚠",
	BoxOn:     "[✓] ",
	BoxOff:    "[ ] ",
	Caret:     "█",
}

// asciiGlyphs is the legacy-CMD-safe fallback. Every glyph is a plain ASCII
// byte (or short ASCII run) that renders identically on conhost with any font.
var asciiGlyphs = glyphSet{
	ascii:     true,
	Cursor:    "> ",
	RailSep:   ">",
	Check:     "v",
	Cross:     "x",
	ArrowUp:   "^",
	ArrowDown: "v",
	MidDot:    "-",
	Ellipsis:  "...",
	Warn:      "!",
	BoxOn:     "[x] ",
	BoxOff:    "[ ] ",
	Caret:     "_",
}

// Spinner returns the bubbles spinner appropriate for the set: the ASCII
// line spinner (|/-\) in fallback mode, else the braille Dot spinner.
func (s glyphSet) Spinner() spinner.Spinner {
	if s.ascii {
		return spinner.Line
	}
	return spinner.Dot
}

// g is the active glyph set, selected once at package init from the environment
// and OS. Tests may reassign it (restore with the value they captured).
var g = pickGlyphs(runtime.GOOS, envLookup(os.LookupEnv))

// envLookup adapts os.LookupEnv (or a test fake) into the predicate pickGlyphs
// needs: whether a variable is set to a non-empty value.
type envLookup func(string) (string, bool)

func (e envLookup) has(key string) bool {
	v, ok := e(key)
	return ok && v != ""
}

// SetupConsole prepares the host console for the wizard TUI and returns a
// restore func the caller must defer. On Windows it switches the console to the
// UTF-8 code page (so Unicode glyphs render on a modern CMD/conhost) and
// restores the previous code page on return; on every other OS it is a no-op.
// It MUST be called before the alt-screen starts and its result deferred so the
// console is restored on exit (#5340).
func SetupConsole() func() { return enableUTF8Console() }

// pickGlyphs is the single, unit-testable decision for ascii-vs-unicode glyphs:
//
//   - GRAFEL_ASCII=1 (or any non-empty value) forces ASCII on ANY OS.
//   - GRAFEL_TUI_UNICODE forces Unicode (overriding the Windows-legacy default).
//   - On Windows, default to ASCII UNLESS a known-Unicode terminal is present
//     (WT_SESSION ⇒ Windows Terminal). Legacy CMD/conhost ⇒ ASCII.
//   - On every other OS, default to Unicode.
func pickGlyphs(goos string, env envLookup) glyphSet {
	if env.has("GRAFEL_ASCII") {
		return asciiGlyphs
	}
	if env.has("GRAFEL_TUI_UNICODE") {
		return unicodeGlyphs
	}
	if goos == "windows" {
		if env.has("WT_SESSION") {
			return unicodeGlyphs
		}
		return asciiGlyphs
	}
	return unicodeGlyphs
}
