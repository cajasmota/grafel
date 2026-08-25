package engine

import (
	"strings"
	"testing"
)

// pythonMaskInertRegions walks a byte slice with several LOOK-AHEAD offsets
// (`b[i+1]`, `b[i+2]`, `b[j+1]`, `b[j+2]`) and with a backslash escape that
// advances the cursor by two. Each look-ahead is protected by an index bound,
// and each of those bounds is the only thing standing between an ordinary
// malformed input and a panic that takes the indexer down.
//
// Every Python fixture elsewhere in this package is well-formed, so no bound is
// ever approached and all four were unobserved: weakening any one of them left
// `go vet` at 0 and the full package suite at exit 0. A file does not have to
// be valid Python to be indexed — partially-written files, generated fragments
// and mid-edit incremental indexes all reach here — but it does have to not
// crash the indexer.
//
// This table pins the CRASH only, for all four bounds in one idiom. What the
// masker PRODUCES for a malformed literal's own line is #6614 and is
// deliberately not asserted here. A FIFTH clamp, `j > len(b)` in the
// single-line branch, is equivalent under this suite and deliberately not
// tested: `j += 2` from `j = len(b)-1` bounds j to len(b)+1, the only consumer
// is `i = j`, and the loop guard rejects both values before b is indexed.
//
// Each row carries its own premise guard. Asserting the fixture's SHAPE is not
// enough: a `#` comment or an earlier single quote can consume the line so the
// branch under test never runs, leaving the assertion passing while observing
// nothing. Every guard below therefore establishes that the branch being pinned
// is the one that actually EXECUTES, and that the fixture sits exactly on the
// boundary the bound defends.
//
// Clauses are annotated LOAD-BEARING where a concrete fixture is known that
// passes every OTHER clause and still leaves the mutant alive, and DECORATIVE
// where a per-clause drop analysis found no such fixture. Decorative clauses
// are kept as documentation of the fixture's intent; they are not claimed to
// carry weight.
//
// What the masker produces for a commented-out line is likewise not asserted
// here: that is a CONTENT claim, and it lives in
// http_endpoint_synthesis_python_mask_comment_test.go
// (TestPythonMaskInertRegions_QuotedSpanInsideCommentIsBlanked, #6623), which
// pins that a quoted span inside a `#` comment is blanked rather than escaping
// into the string branch. Add content claims about this function there, not as
// a row in this table, whose whole contract is the crash.
//
// Refs #6612 (triple-quote clamp), #6617 (the three look-ahead bounds),
// #6623 (the comment branch's content claim).
func TestPythonMaskInertRegions_MustNotPanic(t *testing.T) {
	// firstQuoteOrComment reports the offset of the first byte the scanner's
	// switch dispatches on. If the construct under test is not AT that offset,
	// an earlier `#` or quote consumes it and the intended branch never runs.
	firstQuoteOrComment := func(s string) int { return strings.IndexAny(s, "#\"'") }

	cases := []struct {
		name  string
		issue string
		src   string
		// why documents which bound the row scores, in the diff shape that
		// makes it panic.
		why string
		// premise fails loudly if a later fixture edit would make the row
		// observe nothing.
		premise func(t *testing.T, src string)
	}{
		{
			name:  "EmptySingleLineString",
			issue: "#6617",
			// `x = ""` — an ordinary empty-string assignment. The opening quote
			// sits at len-2, so the triple-quote probe's own look-ahead runs off
			// the end: with `i+2 <= len(b)` the guard admits i+2 == len(b),
			// `b[i+1] == c` holds (the closing quote), and `b[i+2]` panics.
			src: `x = ""`,
			why: "i+2 < len(b) at the triple-quote probe",
			premise: func(t *testing.T, src string) {
				t.Helper()
				q := firstQuoteOrComment(src)
				// DECORATIVE. A fixture with no quote or comment at all is
				// caught by the len-2 offset clause below; per-clause drop
				// analysis found no fixture this one alone rejects. Kept as a
				// clearer message for that case.
				if q < 0 {
					t.Fatalf("#6617 premise broken: fixture %q contains no quote or comment "+
						"character, so the quote branch never runs and this row is VACUOUS.", src)
				}
				// LOAD-BEARING. `firstQuoteOrComment` also matches `#`, and the
				// scanner dispatches `#` to the COMMENT branch, which eats the
				// rest of the line without ever evaluating the bound under test.
				// `x = ##` satisfies every other clause of this premise — the
				// first quote-or-comment character sits at len-2 and the last
				// byte equals it — yet leaves the mutant alive at exit 0. The
				// character found must actually be a quote.
				if src[q] != '"' && src[q] != '\'' {
					t.Fatalf("#6617 premise broken: the first quote-or-comment character must be a "+
						"QUOTE, not %q, or the scanner dispatches to the comment branch and the "+
						"quote branch that holds this bound never RUNS (`x = ##` passes every "+
						"other clause here and leaves the mutant alive); fixture %q. This row is "+
						"now VACUOUS.", src[q], src)
				}
				// The quote branch must be entered at a position where the
				// three-byte look-ahead lands exactly one past the end. Anywhere
				// earlier and `i+2 < len(b)` and `i+2 <= len(b)` agree.
				if q != len(src)-2 {
					t.Fatalf("#6617 premise broken: the first quote-or-comment character must be "+
						"at offset len-2 (%d) so that i+2 == len(b) exactly, which is the ONLY "+
						"place the `<` and `<=` forms of the bound disagree; it is at %d in %q. "+
						"This row is now VACUOUS — restore a fixture whose opening quote is the "+
						"second-to-last byte, or delete the row.", len(src)-2, q, src)
				}
				// `i+2 <= len(b) && b[i+1] == c && b[i+2] == c` short-circuits:
				// without a matching second quote the panic at b[i+2] is never
				// reached even with the bound weakened.
				if src[len(src)-1] != src[q] {
					t.Fatalf("#6617 premise broken: the byte after the opening quote must be the "+
						"SAME quote character so `b[i+1] == c` holds and evaluation reaches "+
						"`b[i+2]`; got %q after %q in %q. This row is now VACUOUS.",
						src[len(src)-1], src[q], src)
				}
				// DECORATIVE. The offset and matching-quote clauses already pin
				// this fixture to the boundary; no fixture is known that this
				// clause alone rejects. Kept to document the intent.
				if strings.Contains(src, `\`) {
					t.Fatalf("#6617 premise broken: fixture %q must contain no backslash, which "+
						"would change how the scan advances and could move the cursor off the "+
						"boundary this row exists to sit on. This row is now VACUOUS.", src)
				}
			},
		},
		{
			name:  "TripleQuoteClosedByTwoQuotes",
			issue: "#6617",
			// `x = """abc""` — a triple-quoted string closed with two quotes
			// instead of three: a typo, or a partially-written file. The scan
			// reaches the closing pair at len-2, so with `j+2 <= len(b)` the
			// guard admits j+2 == len(b), `b[j+1] == c` holds, and `b[j+2]`
			// panics.
			src: `x = """abc""`,
			why: "j+2 < len(b) at the triple-quote terminator probe",
			premise: func(t *testing.T, src string) {
				t.Helper()
				triple := strings.Index(src, `"""`)
				// DECORATIVE. Subsumed by the first-quote-or-comment clause
				// below, which cannot hold when there is no `"""` at all.
				if triple < 0 {
					t.Fatalf("#6617 premise broken: fixture %q contains no `\"\"\"`, so the "+
						"triple-quote branch — the only place this bound lives — never runs. "+
						"This row is VACUOUS.", src)
				}
				// LOAD-BEARING — this is #6615's trap closed. `# x = """abc""`
				// contains the right `"""` and the right closing pair, yet the
				// comment branch eats the line and the mutant survives.
				if q := firstQuoteOrComment(src); q != triple {
					t.Fatalf("#6617 premise broken: the `\"\"\"` must be the FIRST "+
						"quote-or-comment character so the triple-quote branch is the one that "+
						"RUNS; first such character is at %d, the `\"\"\"` is at %d, in %q. A `#` "+
						"comment or an earlier single quote consumes the line before that branch "+
						"is entered. This row is now VACUOUS.", q, triple, src)
				}
				// DECORATIVE. Per-clause drop analysis found no fixture that
				// passes the other clauses, carries a second `"""`, and still
				// leaves the mutant alive. Kept to document the intent.
				if n := strings.Count(src, `"""`); n != 1 {
					t.Fatalf("#6617 premise broken: fixture must contain exactly one `\"\"\"` — a "+
						"second one would TERMINATE the literal and the scan would stop before "+
						"the end; got %d in %q. This row is now VACUOUS.", n, src)
				}
				// The terminator probe must fire with j sitting at len-2, the
				// only offset at which `<` and `<=` disagree.
				if !strings.HasSuffix(src, `""`) {
					t.Fatalf("#6617 premise broken: fixture must END with exactly two quotes so "+
						"the terminator probe runs at j == len-2 and j+2 == len(b); got %q. This "+
						"row is now VACUOUS.", src)
				}
				if strings.Contains(src, `\`) {
					t.Fatalf("#6617 premise broken: fixture %q must contain no backslash — the "+
						"`j += 2` escape skip would step the cursor OVER the closing pair, so the "+
						"terminator probe would never fire at the boundary. This row is VACUOUS.", src)
				}
				// The literal body must be non-empty and quote-free, so the scan
				// walks one byte at a time and lands on the closing pair rather
				// than reaching it as part of the opener.
				if body := src[triple+3 : len(src)-2]; body == "" || strings.ContainsAny(body, `"`) {
					t.Fatalf("#6617 premise broken: the literal body between the opening `\"\"\"` "+
						"and the closing pair must be non-empty and contain no quote, so the "+
						"closing pair is reached by the scan as a fresh terminator probe rather "+
						"than being consumed by the opener; body is %q in %q. This row is now "+
						"VACUOUS.", body, src)
				}
			},
		},
		{
			name:  "UnterminatedTripleQuoteTrailingBackslash",
			issue: "#6612",
			// `x = """abc\` — an unterminated triple-quoted literal ending in a
			// backslash. The `j += 2` escape skip runs j to len(b)+1 with no
			// closing delimiter in sight; the clamp `if j > len(b) { j = len(b) }`
			// is the only thing keeping the following blank() loop in range.
			src: "x = \"\"\"abc\\",
			why: "the j > len(b) clamp after the triple-quote scan",
			premise: func(t *testing.T, src string) {
				t.Helper()
				triple := strings.Index(src, `"""`)
				// DECORATIVE. Subsumed by the first-quote-or-comment clause
				// below, which cannot hold when there is no `"""` at all.
				if triple < 0 {
					t.Fatalf("#6612 premise broken: fixture %q contains no `\"\"\"`, so the "+
						"triple-quote branch — where the clamp lives — never runs. VACUOUS.", src)
				}
				if q := firstQuoteOrComment(src); q != triple {
					t.Fatalf("#6612 premise broken: the `\"\"\"` must be the FIRST "+
						"quote-or-comment character so the triple-quote branch is the one that "+
						"RUNS; first such character is at %d, the `\"\"\"` is at %d, in %q. This "+
						"row is now VACUOUS.", q, triple, src)
				}
				if n := strings.Count(src, `"""`); n != 1 {
					t.Fatalf("#6612 premise broken: fixture must contain exactly one `\"\"\"` so "+
						"the literal is UNTERMINATED; a terminated literal never runs j past the "+
						"end. Got %d in %q. This row is now VACUOUS.", n, src)
				}
				if !strings.HasSuffix(src, `\`) {
					t.Fatalf("#6612 premise broken: fixture must END with a trailing backslash so "+
						"the `j += 2` escape skip carries j to len(b)+1; got %q. This row is now "+
						"VACUOUS.", src)
				}
				// LOAD-BEARING, and the hole in #6612's original guards: "ends
				// with a backslash" is not enough, the trailing RUN of
				// backslashes must be ODD. `x = """abc\\` — an ordinary escaped
				// backslash — satisfies every clause above, but the first `\`
				// consumes the second via `j += 2` and j lands exactly ON
				// len(b), never past it. The clamp is then never needed and the
				// mutant survives at exit 0.
				run := 0
				for run < len(src) && src[len(src)-1-run] == '\\' {
					run++
				}
				if run%2 == 0 {
					t.Fatalf("#6612 premise broken: the trailing run of backslashes must be ODD "+
						"so the final `\\` has no byte to consume and `j += 2` carries j PAST "+
						"len(b); the run is %d in %q. With an even run (an escaped backslash, "+
						"`\\\\`) j lands exactly ON len(b), the clamp is never needed and this "+
						"row is VACUOUS.", run, src)
				}
			},
		},
		{
			name:  "UnterminatedSingleLineStringAtEOF",
			issue: "#6617",
			// `x = "abc` — an unterminated single-line string running to EOF,
			// commoner in practice than either fixture above. The single-line
			// scan exits its loop with j == len(b); with `j <= len(b)` the
			// closing-quote probe then reads `b[j]` one past the end.
			src: `x = "abc`,
			why: "j < len(b) at the single-line closing-quote probe",
			premise: func(t *testing.T, src string) {
				t.Helper()
				q := firstQuoteOrComment(src)
				if q < 0 || (src[q] != '"' && src[q] != '\'') {
					t.Fatalf("#6617 premise broken: the first quote-or-comment character in %q "+
						"must be a QUOTE, or the scanner dispatches to the comment branch and the "+
						"quote branch that holds this bound never RUNS. This row is now VACUOUS.",
						src)
				}
				tail := src[q+1:]
				// The single-line branch runs only when the triple-quote probe
				// FAILS, which requires the byte after the opener to differ from
				// the opener. A `"""` here would take the other branch entirely.
				if tail == "" || tail[0] == src[q] {
					t.Fatalf("#6617 premise broken: the byte after the opening quote must exist "+
						"and differ from it, so the triple-quote probe fails and the SINGLE-LINE "+
						"branch — the one holding this bound — is what runs; got tail %q after "+
						"%q in %q. This row is now VACUOUS.", tail, src[q], src)
				}
				// The scan must walk the whole tail and stop only by running out
				// of bytes, so j == len(b) exactly: the one offset at which `<`
				// and `<=` disagree. A closing quote or a newline breaks the loop
				// early; a backslash steps `j += 2` and can overshoot.
				if bad := strings.IndexAny(tail, string(src[q])+"\n\\"); bad >= 0 {
					t.Fatalf("#6617 premise broken: the literal must be UNTERMINATED and run to "+
						"EOF — the tail may contain no %q, no newline and no backslash, so the "+
						"scan stops only by exhausting the input and j == len(b) exactly; found "+
						"%q at tail offset %d in %q. Any of those makes the probe fire below the "+
						"boundary and this row VACUOUS.", src[q], tail[bad], bad, src)
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tc.premise(t, tc.src)

			// Recover so a live mutant surfaces as a failing test rather than
			// taking the whole test binary down with it.
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("%s: pythonMaskInertRegions panicked on %q: %v — the bound `%s` is "+
						"load-bearing and must stay.", tc.issue, tc.src, r, tc.why)
				}
			}()

			got := pythonMaskInertRegions(tc.src)

			// The masker's contract is a same-length copy addressing the same
			// source offsets; a length change would mean a bound truncated the
			// output rather than merely keeping the walk in range.
			if len(got) != len(tc.src) {
				t.Fatalf("%s: pythonMaskInertRegions changed length: got %d (%q), want %d (%q)",
					tc.issue, len(got), got, len(tc.src), tc.src)
			}
		})
	}
}
