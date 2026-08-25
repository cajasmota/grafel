package engine

import (
	"strings"
	"testing"
)

// TestSynth_FastAPI_MountPrefix_UnterminatedLiteralTailIsBlanked_6614 pins the
// #6614 behaviour change: the CONTENTS of an unterminated single-line literal
// are blanked, so the tail of a broken line never reaches the mount scan as
// live code.
//
// The reasoning, so a later reader does not have to take it on faith: once a
// quote opens and never closes, everything from that quote to the end of the
// line IS string content, by definition. There is no legitimate `prefix=`
// "after" it on the same line that blanking could swallow — anything past the
// opening quote is already inside the string. So leaving the tail live can
// only mint mount points that do not exist; blanking it can only remove them.
// One direction, no trade-off. This is the same direction #6598 already took
// for the regions AFTER an unterminated quote, finished for the runaway span
// itself.
//
// The load-bearing half of this test is the TERMINATED control. #6418 is the
// deliberate decision that a well-formed single-line literal is left INTACT,
// because the mount prefix itself lives inside one (`prefix="/x"`) and the
// scan has to be able to read it. #6614 must not move that path at all, so
// both a real `prefix="/x"` call and a terminated literal whose contents merely
// spell `include_router` are asserted here to still mint. Without them, the
// blanking assertion above would pass just as happily for an implementation
// that blanked every literal — which would break #6418 and the mount feature
// with it.
func TestSynth_FastAPI_MountPrefix_UnterminatedLiteralTailIsBlanked_6614(t *testing.T) {
	// The contested text sits INSIDE the runaway literal, on its own line —
	// that is what distinguishes this from #6598 (text after the line) and
	// from #6418 (text inside a closed literal). No `"` follows the opening
	// quote on that line, so the literal really is unterminated.
	const broken = `from fastapi import FastAPI

app = FastAPI()
BROKEN = "oops app.include_router(other.router, prefix='/tailofbroken')
`
	q := strings.Index(broken, `= "oops`)
	if q < 0 {
		t.Fatalf("#6614: fixture no longer carries the unterminated opening quote: %q", broken)
	}
	q += len(`= `)
	nl := strings.IndexByte(broken[q:], '\n')
	if nl < 0 {
		t.Fatalf("#6614: fixture's broken line is not newline-terminated: %q", broken)
	}
	line := broken[q : q+nl]
	if strings.Count(line, `"`) != 1 {
		t.Fatalf("#6614: the broken line must carry exactly ONE double quote, or it is terminated and this "+
			"case observes the #6418 path instead: %q", line)
	}
	if !strings.Contains(line, "include_router") {
		t.Fatalf("#6614: the contested include_router must sit INSIDE the runaway literal, on the same line, "+
			"or this case is vacuous: %q", line)
	}

	if masked := pythonMaskInertRegions(broken); strings.Contains(masked, "include_router") {
		t.Errorf("#6614: the tail of an unterminated single-line literal reached the masked copy as live code "+
			"— everything from the opening quote to the end of the line is string content and must be blanked. "+
			"masked=%q", masked)
	}
	if got := pythonMaskInertRegions(broken); len(got) != len(broken) {
		t.Fatalf("#6614: masking changed the byte length: got %d, want %d", len(got), len(broken))
	}

	_, res := runDetect(t, "python", "app/main.py", broken)
	if mounts := fastapiMountSynths(res); len(mounts) != 0 {
		t.Errorf("#6614: the inside of an unterminated string literal minted url_mount_point synthetics %v — "+
			"every one of them is a mount that does not exist", mounts)
	}

	// Control 1 — the whole of #6418, and the reason this change is narrow:
	// a well-formed `prefix="/x"` in ordinary code must still be readable out
	// of the masked copy. If #6614 blanked terminated literals too, the mount
	// feature would stop working entirely and only this arm would say so.
	const ordinary = `from fastapi import FastAPI

app = FastAPI()
app.include_router(other.router, prefix="/realprefix")
`
	if masked := pythonMaskInertRegions(ordinary); !strings.Contains(masked, `"/realprefix"`) {
		t.Fatalf("#6614 broke #6418: a TERMINATED single-line literal was blanked, so the mount prefix the scan "+
			"has to read is gone. masked=%q", masked)
	}
	_, ordRes := runDetect(t, "python", "app/main.py", ordinary)
	if _, ok := fastapiMountSynths(ordRes)["/realprefix"]; !ok {
		t.Fatalf("#6614 broke #6418: an ordinary include_router with a closed prefix literal minted no mount "+
			"synthetic (got %v).", fastapiMountSynths(ordRes))
	}

	// Control 2 — #6418's known limitation itself, asserted here so this
	// change is observably not the thing that flips it. A TERMINATED literal
	// whose contents spell a call still mints. That is not desirable; it is
	// pinned in ..._SingleLineStringIsAKnownFalsePositive_6418 and stays true
	// until a real Python tokenizer lands (deliberately out of scope for
	// #6614).
	const terminated = `from fastapi import FastAPI

app = FastAPI()
TEMPLATE = "app.include_router(other.router, prefix='/termmints')"
`
	_, termRes := runDetect(t, "python", "app/main.py", terminated)
	if _, ok := fastapiMountSynths(termRes)["/termmints"]; !ok {
		t.Fatalf("#6614: the #6418 known limitation moved — a TERMINATED single-line literal whose contents "+
			"spell include_router no longer mints (got %v). #6614 is only about the UNTERMINATED path; if you "+
			"just taught the masker to tokenize Python, flip this and #6418's own test together, deliberately.",
			fastapiMountSynths(termRes))
	}
}
