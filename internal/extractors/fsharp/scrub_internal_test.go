package fsharp

import (
	"strings"
	"testing"
)

// TestScrubPreservesLengthAndNewlines pins the two invariants every caller of
// stripStringsAndComments depends on (#6336):
//
//   - the byte LENGTH is identical, so an offset taken in the scrub indexes the
//     same byte in the source (and vice versa);
//   - a `'\n'` in the source is a `'\n'` in the scrub at the same index, so a
//     newline count over any prefix of one agrees with the other.
//
// The second is what the `line` Property of a CALLS edge rides on; the first is
// what makes the offsets that line is computed from meaningful at all.
func TestScrubPreservesLengthAndNewlines(t *testing.T) {
	cases := map[string]string{
		"block comment":  "let a = 1\n(*\nx\ny\n*)\nlet b = 2\n",
		"nested comment": "let a = 1\n(*\n(*\nx\n*)\n*)\nlet b = 2\n",
		"triple quoted":  "let d = \"\"\"\nx\ny\n\"\"\"\nlet b = 2\n",
		"plain string":   "let s = \"ab\\\"cd\"\nlet b = 2\n",
		"line comment":   "let a = 1 // note\nlet b = 2\n",
		"unterminated":   "let s = \"oops\nlet b = 2\n",
		"no literals":    "let a = 1\nlet b = 2\n",
		"empty":          "",
	}
	for name, src := range cases {
		got := stripStringsAndComments(src)
		if len(got) != len(src) {
			t.Errorf("%s: len(scrub) = %d, want %d (offsets must stay valid)", name, len(got), len(src))
			continue
		}
		for i := range src {
			if (src[i] == '\n') != (got[i] == '\n') {
				t.Errorf("%s: byte %d: src %q vs scrub %q — newlines must map 1:1", name, i, src[i], got[i])
			}
		}
		if strings.Count(got, "\n") != strings.Count(src, "\n") {
			t.Errorf("%s: newline count = %d, want %d", name, strings.Count(got, "\n"), strings.Count(src, "\n"))
		}
	}
}
