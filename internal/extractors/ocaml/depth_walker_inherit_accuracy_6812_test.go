// depth_walker_inherit_accuracy_6812_test.go — MEASUREMENT harness for
// `extractModuleBody`, the one structural judgement the OCaml extractor owns.
//
// #6812 states plainly that nobody had measured how often the hand-rolled
// depth walker got `inherit` context wrong, and that the number had to exist
// before committing to a tree-sitter migration. Arm 1 of this file was that
// measurement. ARM 2 IS THE MIGRATION, and this file is now a BEFORE-AND-AFTER
// rather than a before: the retired walker is preserved verbatim as
// legacyDepthWalkerBody and scored alongside the CST-backed
// `extractModuleBody`, over the same inputs and the same reference, in a
// single run. Without that, every figure #6812 rests on would be a claim in a
// PR body that nothing in the repo can reproduce.
//
// It still adds no pattern to the extractor.
//
// # What "inherit context" means here, operationally
//
// In OCaml `inherit` denotes class inheritance only inside `object … end`.
// Deciding that requires knowing where the enclosing block ends — which is
// exactly the question `extractModuleBody` answers, and the only structural
// judgement the extractor owns. So:
//
//	walker span   = [openerStart, openerStart+len(extractModuleBody(src, openerStart)))
//	truth span    = [openerStart, offset of the block's real matching `end`)
//	an `inherit` is MIS-ATTRIBUTED when exactly one of the two spans contains it.
//
// Both producers are driven the way production drives them: they are handed a
// position from which the block's OPENING keyword is still ahead (extractor.go
// passes the end of the `module Foo =` match, with `struct` still to come).
// For the walker that is what makes the opener increment depth; for the
// CST-backed implementation it is the "first opener at or after `from`"
// contract in blocks.go. That is not a detail — see the divergence table
// below.
//
// # Ground truth
//
// refMatchEnd below is a small OCaml block matcher written for this test only.
// It differs from the RETIRED walker in four ways that are each an independent
// defect of that walker, so a positive control matters:
// TestReferenceBlockMatcher_PositiveControls pins it against hand-computed
// answers, including every hazard class it is supposed to survive. A
// measurement whose reference is unvalidated measures nothing.
//
// Its role changed in arm 2 and the change is stated in full on
// TestDepthWalker_InheritAttribution_CorpusMeasurement: against a CST-backed
// producer it is no longer an independent oracle (arm 1 established that
// refMatchEnd and the grammar agree, 595/595), but it is not vacuous either —
// it shares no line with blocks.go and still grades everything the wiring
// added on top of the grammar. Read that note before quoting an agreement
// figure from this file as a confirmation.
//
// # Corpus
//
// TestDepthWalker_InheritAttribution_CorpusMeasurement runs over a real OCaml
// tree when GRAFEL_OCAML_CORPUS points at one, and skips otherwise — the repo
// vendors no OCaml source (zero *.ml files outside this fixture set), so the
// figure quoted in the PR that added this file was taken against a clone of
// github.com/ocaml/ocaml and is named as such rather than implied to be
// in-repo. The constructed cases in TestDepthWalker_ObjectBlockEnd_Divergence
// run unconditionally, are labelled constructed, and are what makes the
// finding reproducible without a network.
package ocaml

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// The retired depth walker, kept as the #6812 baseline
// ---------------------------------------------------------------------------

// legacyDepthWalkerBody is `extractModuleBody` EXACTLY as it stood at
// 2de485618, the commit that measured it. It was deleted from production when
// the vendored tree-sitter grammar was wired in (arm 2), and it lives on here
// as test-only code for one reason: without it every figure in #6812 becomes a
// claim in a PR body that nothing in the repo can reproduce.
//
// Keeping it means the enumeration and the corpus measurement below grade TWO
// producers against one independent reference in a single run, so "the CST
// implementation is better" is a measurement taken here rather than a
// before/after taken across two checkouts. It is never called by production
// code; `extractModuleBody` is.
func legacyDepthWalkerBody(src string, afterPos int) string {
	if afterPos >= len(src) {
		return ""
	}
	rest := src[afterPos:]

	depth := 0
	found := false
	endPos := 0

	openKW := regexp.MustCompile(`\b(struct|sig|object|begin)\b`)
	closeKW := regexp.MustCompile(`\bend\b`)

	i := 0
	for i < len(rest) {
		if i+1 < len(rest) && rest[i] == '(' && rest[i+1] == '*' {
			i += 2
			for i < len(rest) {
				if i+1 < len(rest) && rest[i] == '*' && rest[i+1] == ')' {
					i += 2
					break
				}
				i++
			}
			continue
		}
		if rest[i] == '"' {
			i++
			for i < len(rest) && rest[i] != '"' {
				if rest[i] == '\\' {
					i++
				}
				i++
			}
			i++
			continue
		}
		if rest[i] == '\'' && i+2 < len(rest) && rest[i+2] == '\'' {
			i += 3
			continue
		}

		remaining := rest[i:]
		if om := openKW.FindStringIndex(remaining); om != nil && om[0] == 0 {
			depth++
			i += om[1]
			continue
		}
		if cm := closeKW.FindStringIndex(remaining); cm != nil && cm[0] == 0 {
			if depth == 0 {
				endPos = i
				found = true
				break
			}
			depth--
			i += cm[1]
			continue
		}
		i++
	}

	if found {
		return rest[:endPos]
	}
	return extractLetBody(src, afterPos)
}

// ---------------------------------------------------------------------------
// Reference OCaml block matcher (test-only ground truth)
// ---------------------------------------------------------------------------

func isIdentByte(c byte) bool {
	return c == '_' || c == '\'' || (c >= '0' && c <= '9') ||
		(c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

// tokenAt reports the keyword starting at src[i], or "" when src[i] does not
// begin a keyword token. Unlike the production walker it requires a real
// identifier boundary on BOTH sides, computed against the whole source rather
// than against a slice — the production walker re-runs `\b(struct|…)\b` on
// src[i:], where `\b` at offset 0 of the slice is satisfied by start-of-string,
// so every identifier ENDING in one of the keywords matches.
func tokenAt(src string, i int) string {
	if i > 0 && isIdentByte(src[i-1]) {
		return ""
	}
	for _, kw := range []string{"struct", "object", "begin", "sig", "end"} {
		if strings.HasPrefix(src[i:], kw) {
			j := i + len(kw)
			if j < len(src) && isIdentByte(src[j]) {
				return ""
			}
			return kw
		}
	}
	return ""
}

// skipTrivia advances past a comment, string, quoted string or char literal
// beginning at src[i]; it returns i unchanged when src[i] starts none of them.
func skipTrivia(src string, i int) int {
	switch {
	// OCaml block comments NEST. The production walker's comment skipper stops
	// at the first `*)`, so `(* (* x *) end *)` leaks an `end` to it.
	case strings.HasPrefix(src[i:], "(*"):
		depth, j := 1, i+2
		for j < len(src) && depth > 0 {
			switch {
			case strings.HasPrefix(src[j:], "(*"):
				depth++
				j += 2
			case strings.HasPrefix(src[j:], "*)"):
				depth--
				j += 2
			case src[j] == '"':
				j = skipTrivia(src, j)
			default:
				j++
			}
		}
		return j
	// Quoted string literal {| … |} / {id| … |id}. The production walker does
	// not know this form at all, so `{|" end "|}` is scanned as code.
	case src[i] == '{':
		j := i + 1
		for j < len(src) && (src[j] == '_' || (src[j] >= 'a' && src[j] <= 'z')) {
			j++
		}
		if j < len(src) && src[j] == '|' {
			tag := src[i+1 : j]
			closer := "|" + tag + "}"
			if k := strings.Index(src[j+1:], closer); k >= 0 {
				return j + 1 + k + len(closer)
			}
			return len(src)
		}
		return i
	case src[i] == '"':
		j := i + 1
		for j < len(src) && src[j] != '"' {
			if src[j] == '\\' {
				j++
			}
			j++
		}
		return j + 1
	case src[i] == '\'':
		// A char literal is 'c' or '\n' / '\\' / '\123' / '\xNN'. Anything else
		// beginning with ' is a type variable and must NOT be skipped.
		if i+2 < len(src) && src[i+1] != '\\' && src[i+2] == '\'' {
			return i + 3
		}
		if i+1 < len(src) && src[i+1] == '\\' {
			for j := i + 2; j < len(src) && j <= i+6; j++ {
				if src[j] == '\'' {
					return j + 1
				}
			}
		}
		return i
	}
	return i
}

// refMatchEnd returns the offset of the `end` token closing the block whose
// opening keyword starts at or after `from`, or -1 when the source has no
// matching end. `from` is the position production hands the walker, i.e. the
// opener is still ahead of it.
func refMatchEnd(src string, from int) int {
	depth := 0
	for i := from; i < len(src); {
		if j := skipTrivia(src, i); j != i {
			i = j
			continue
		}
		switch kw := tokenAt(src, i); kw {
		case "struct", "object", "begin", "sig":
			depth++
			// Advance by the token's OWN length. `i += 6` (the longest opener)
			// was wrong and its comment — "exact length is irrelevant" — was
			// false: `sig` is 3 bytes and `begin` is 5, so the scan skipped 3
			// resp. 1 bytes of real source, and a comment or string opener
			// landing in that window was read as code. `module type T = sig end`
			// returned -1 and `sig (* end *) … end` returned the `end` INSIDE
			// the comment. Invisible to the controls below before this fix,
			// because every comment control opened with the 6-byte `object`.
			i += len(kw)
			continue
		case "end":
			depth--
			if depth == 0 {
				return i
			}
			i += len(kw)
			continue
		}
		i++
	}
	return -1
}

// inheritOffsets returns the offsets of every real `inherit` keyword token.
func inheritOffsets(src string) []int {
	var out []int
	for i := 0; i < len(src); {
		if j := skipTrivia(src, i); j != i {
			i = j
			continue
		}
		if !(i > 0 && isIdentByte(src[i-1])) && strings.HasPrefix(src[i:], "inherit") {
			j := i + len("inherit")
			if j >= len(src) || !isIdentByte(src[j]) {
				out = append(out, i)
				i = j
				continue
			}
		}
		i++
	}
	return out
}

// objectOffsets returns the offsets of every real `object` keyword token.
func objectOffsets(src string) []int {
	var out []int
	for i := 0; i < len(src); {
		if j := skipTrivia(src, i); j != i {
			i = j
			continue
		}
		if tokenAt(src, i) == "object" {
			out = append(out, i)
			i += len("object")
			continue
		}
		i++
	}
	return out
}

// endsAgree reports whether the walker's span end and the reference's matching
// `end` offset delimit the same block content. Only whitespace may separate
// them: the walker returns BODY TEXT (so it stops before the `end` token and
// before any newline preceding it) while the reference returns the offset OF
// that token, and treating that gap as an error would count formatting as a
// defect. Every difference this reports is therefore a real difference in what
// the block contains.
func endsAgree(src string, walkEnd, truth int) bool {
	lo, hi := walkEnd, truth
	if lo > hi {
		lo, hi = hi, lo
	}
	if lo < 0 || hi > len(src) {
		return false
	}
	return strings.TrimSpace(src[lo:hi]) == ""
}

// ---------------------------------------------------------------------------
// Positive controls for the reference matcher
// ---------------------------------------------------------------------------

func TestReferenceBlockMatcher_PositiveControls(t *testing.T) {
	// Each case marks the expected matching `end` with the sentinel §, which is
	// stripped before matching; its byte offset is the answer. A case with no §
	// expects -1.
	cases := []struct {
		name string
		src  string
	}{
		{"plain object", "let x = object method a = 1 §end"},
		{"nested begin", "let x = object method a = begin 1 end §end"},
		{"nested object", "let x = object inherit object method b = 2 end method a = 1 §end"},
		{"sig", "module type T = sig val f : int §end"},
		{"identifier ending in end", "let x = object method append = 1 method backend = 2 §end"},
		{"identifier ending in object", "let x = object method myobject = 1 §end"},
		{"identifier ending in sig", "let x = object method design = 1 §end"},
		// The three above exercise only the LEADING identifier boundary. These
		// five exercise the TRAILING one — an identifier that STARTS with a
		// keyword — which no control covered until a mutant deleting tokenAt's
		// trailing check survived the whole package (review N1).
		{"identifier starting with end", "let x = object method endian = 1 §end"},
		{"identifier starting with sig", "let x = object method signature = 1 §end"},
		{"identifier starting with object", "let x = object method objects = 1 §end"},
		{"identifier starting with begin", "let x = object method beginning = 1 §end"},
		{"identifier starting with struct", "let x = object method structure = 1 §end"},
		// And these three pin `i += len(kw)`: they open with a 3- or 5-byte
		// keyword, so a scan advancing by a fixed 6 steps over the bytes that
		// follow it. Every other control opens with the 6-byte `object`, which
		// is exactly why that defect was invisible.
		{"empty sig block", "module type T = sig §end"},
		{"comment immediately after sig", "module type T = sig (* end *) val f : int §end"},
		{"comment immediately after begin", "let x = object method a = begin(* end *) 1 end §end"},
		{"end inside string", "let x = object method a = \"end end\" §end"},
		{"end inside comment", "let x = object (* end *) method a = 1 §end"},
		{"end inside NESTED comment", "let x = object (* (* end *) end *) method a = 1 §end"},
		{"end inside quoted string", "let x = object method a = {|end end|} §end"},
		{"type variable is not a char literal", "let x = object method a : 'a -> 'a = fun y -> y §end"},
		{"char literal holding a quote", "let x = object method a = '\\'' §end"},
		{"unterminated block", "let x = object method a = 1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			want := strings.Index(tc.src, "§")
			src := strings.Replace(tc.src, "§", "", 1)
			got := refMatchEnd(src, 0)
			if want < 0 {
				if got != -1 {
					t.Fatalf("want no match, got %d (%q)", got, src[got:])
				}
				return
			}
			if got != want {
				t.Fatalf("want end at %d (%q), got %d (%q)", want, src[want:], got, tail(src, got))
			}
		})
	}
}

func tail(src string, i int) string {
	if i < 0 || i >= len(src) {
		return "<none>"
	}
	if len(src)-i > 20 {
		return src[i : i+20]
	}
	return src[i:]
}

// ---------------------------------------------------------------------------
// The measurement, on constructed inputs (always runs)
// ---------------------------------------------------------------------------

// TestDepthWalker_ObjectBlockEnd_EnumeratedSpace enumerates the input space
// rather than hand-picking cases, because hand-picked cases were tried first
// here and every one of them AGREED with the reference — the indentation
// fallback rescues a canonically formatted single block, so a toy input
// measures the fallback and not the depth scan.
//
// Five independent binary axes are crossed exhaustively (32 inputs):
//
//	A closeAtCol0  — the block's `end` at column 0 vs indented
//	B endIdent     — the body contains an identifier CONTAINING `end`
//	C nestedComment— the body contains a nested (* (* … *) … *) comment
//	D quotedString — the body contains a {|…|} quoted string holding `end`
//	E trailer      — a further top-level declaration follows the block
//
// WHAT THIS DOES AND DOES NOT SHOW, stated because the first version of this
// test claimed more than it measured: every input holds exactly one
// `object … end` block with exactly one `inherit`, and what the enumeration
// varies is where the block ENDS. It does NOT produce a single `inherit`
// mis-attribution — 0 of 32 — because the `inherit` is always the first body
// line and every disagreeing span still contains it. A divergence here is
// therefore block-end divergence, which is the walker's structural judgement
// and the mechanism the attribution would rest on, but it is a PROXY for
// mis-attribution and not an instance of one. The instances live in
// TestDepthWalker_InheritIsMisAttributed_Constructed (a sibling pair inside a
// module, where one block's span swallows the other's `inherit`) and in the
// corpus measurement's 230 pairs. The guard inside the loop pins the 0.
//
// The DIVERGING SET is recorded below as a literal; a change to the walker
// that moves any combination in or out of it fails this test, which is what
// makes the #6812 finding reproducible without a network.
//
// # Re-derived for the grammar wiring (#6812 arm 2)
//
// The enumeration now runs BOTH producers over the same 32 inputs against the
// same reference:
//
//   - legacyDepthWalkerBody — the retired regex depth walker, verbatim. Its
//     diverging set is UNCHANGED at the recorded 18/32, so arm 1's finding is
//     still reproducible in-repo rather than only in a PR body.
//   - extractModuleBody — now CST-backed. Its diverging set is EMPTY, and that
//     is recorded as its own literal.
//
// An empty set is a weak-looking assertion, so be precise about what it can
// still catch: `endsAgree` fails on ANY disagreement in either direction, so a
// producer that truncates, over-reaches, returns "" or silently falls back to
// the indentation heuristic all land in cstDivergingCombinations6812 and fail
// here. The direction that would be invisible — a producer that agrees for the
// wrong reason — is exactly what running the legacy walker alongside it
// answers: if the two sets ever coincide, the wiring has been reverted or
// bypassed, and the 18-entry assertion fails first.
//
// Axis-E inertness and the E-symmetry count are asserted against the LEGACY
// set, where they are non-vacuous. Asserting them over an empty set would be
// two guards that pass by having nothing to look at.
func TestDepthWalker_ObjectBlockEnd_EnumeratedSpace(t *testing.T) {
	type axes struct{ closeAtCol0, endIdent, nestedComment, quotedString, trailer bool }
	build := func(a axes) string {
		var b strings.Builder
		b.WriteString("let c = object\n")
		b.WriteString("  inherit base\n")
		if a.endIdent {
			// `append`, NOT `append_all`. Go's `\b` treats `_` as a word
			// character, so `\bend\b` never matches inside `end_all` and the
			// axis was INERT — it changed no outcome in any of the 16 pairs
			// (review). It exists to grade defect (2), the slice-relative `\b`,
			// and with the underscore it graded nothing at all.
			b.WriteString("  method append x = x\n")
		}
		if a.nestedComment {
			b.WriteString("  (* (* q *) end *)\n")
		}
		if a.quotedString {
			b.WriteString("  method sql = {|SELECT end|}\n")
		}
		b.WriteString("  method a = 1\n")
		if a.closeAtCol0 {
			b.WriteString("end\n")
		} else {
			b.WriteString("  end\n")
		}
		if a.trailer {
			b.WriteString("\nlet d = 2\n")
		}
		return b.String()
	}
	label := func(a axes) string {
		flag := func(on bool, c string) string {
			if on {
				return c
			}
			return "-"
		}
		return flag(a.closeAtCol0, "A") + flag(a.endIdent, "B") +
			flag(a.nestedComment, "C") + flag(a.quotedString, "D") + flag(a.trailer, "E")
	}

	producers := []struct {
		name string
		body func(string, int) string
		got  []string
		want []string
	}{
		{name: "legacy regex depth walker", body: legacyDepthWalkerBody, want: divergingCombinations6812},
		{name: "CST-backed extractModuleBody", body: extractModuleBody, want: cstDivergingCombinations6812},
	}

	for i := 0; i < 32; i++ {
		a := axes{
			closeAtCol0:   i&1 != 0,
			endIdent:      i&2 != 0,
			nestedComment: i&4 != 0,
			quotedString:  i&8 != 0,
			trailer:       i&16 != 0,
		}
		src := build(a)
		truth := refMatchEnd(src, 0)
		if truth < 0 {
			t.Fatalf("%s: reference could not match a generated case — fix the generator, not the reference:\n%s", label(a), src)
		}
		inh := inheritOffsets(src)
		if len(inh) != 1 {
			t.Fatalf("%s: generator premise broken, want exactly 1 inherit, got %d", label(a), len(inh))
		}
		inTruth := inh[0] < truth
		if !inTruth {
			t.Fatalf("%s: generator premise broken, the inherit is not inside the block", label(a))
		}
		for p := range producers {
			walkEnd := len(producers[p].body(src, 0))
			inWalker := inh[0] < walkEnd
			// The criterion is block-end disagreement ALONE. It used to read
			// `!endsAgree(...) || inTruth != inWalker`, and the second term was
			// dead: a mutant deleting the first left the test unable to find any
			// divergence at all, because NO generated input mis-attributes its
			// `inherit` (review N2). The generator always writes the `inherit` as
			// the first body line, so every span that disagrees still contains it.
			// Asserting that positively, here, is what keeps the dead term from
			// coming back as a claim nothing checks. It is asserted for BOTH
			// producers: a CST-backed span that started BEFORE the `inherit`
			// would break the premise just as surely as one that stopped short.
			if inTruth != inWalker {
				t.Fatalf("%s [%s]: this generator is not supposed to be able to produce an "+
					"inherit MIS-ATTRIBUTION — it varies where the block ENDS, and the "+
					"inherit is always the first body line. If it now can, the enumerated "+
					"finding changed and both write-ups need re-deriving.", label(a), producers[p].name)
			}
			if !endsAgree(src, walkEnd, truth) {
				producers[p].got = append(producers[p].got, label(a))
				t.Logf("%s [%s] BLOCK-END DIVERGES: span end=%d, true end=%d (inherit@%d is inside both spans)",
					label(a), producers[p].name, walkEnd, truth, inh[0])
			}
		}
	}
	for p := range producers {
		if strings.Join(producers[p].got, ",") != strings.Join(producers[p].want, ",") {
			t.Fatalf("diverging set changed for %s.\n got: %v\nwant: %v",
				producers[p].name, producers[p].got, producers[p].want)
		}
	}
	// The whole point of running both is that they differ. Without this the two
	// set assertions above could BOTH be satisfied by a producers table whose
	// second entry had been pointed back at the first — the `want` literals
	// would then simply be wrong together, and nothing would say so.
	if len(producers[0].got) <= len(producers[1].got) {
		t.Fatalf("the retired walker (%d diverging) is not doing worse than the CST-backed one (%d) — "+
			"the wiring has been reverted or the table is pointed at one producer twice",
			len(producers[0].got), len(producers[1].got))
	}
	got := producers[0].got
	// Axis E is INERT, and that is asserted rather than left as padding. The
	// review found E (and, before it was respelled, B) changed no outcome in
	// any of the 16 pairs, which meant "32 combinations" overstated what was
	// being crossed. B is now live; E is not, and the reason is worth pinning:
	// the fallback's stop condition is the next column-0 line, so whether a
	// further top-level declaration follows the block cannot change where the
	// span ends. An E that ever starts mattering is a real change in the
	// fallback and should fail here rather than quietly widen the table.
	diverging := map[string]bool{}
	for _, k := range got {
		diverging[k] = true
	}
	for _, k := range got {
		if !strings.HasSuffix(k, "E") {
			continue
		}
		if withoutE := k[:4] + "-"; !diverging[withoutE] {
			t.Fatalf("axis E is documented INERT but %q diverges while %q does not", k, withoutE)
		}
	}
	if len(got)%2 != 0 {
		t.Fatalf("axis E is documented INERT, so the diverging set must be E-symmetric, got %d entries", len(got))
	}
	t.Logf("retired regex depth walker: %d/32 enumerated combinations put the WRONG END on the block; "+
		"CST-backed extractModuleBody: %d/32. "+
		"0/32 mis-attribute the `inherit` for EITHER producer — see the guard above; inherit "+
		"mis-attribution is carried by TestDepthWalker_InheritIsMisAttributed_Constructed "+
		"and by the corpus measurement, not by this enumeration.",
		len(producers[0].got), len(producers[1].got))
}

// TestDepthWalker_InheritIsMisAttributed_Constructed is the smallest input
// that shows the failure the corpus figure is mostly made of: two sibling
// objects inside a module, where the FIRST block's span swallows the SECOND
// block's `inherit` and so would attribute it to the wrong owner.
//
// The sibling pair has to sit inside a module for this to be reachable, and
// that is the finding rather than an inconvenience: at top level the
// indentation fallback stops at the next column-0 `let`, so the over-reach is
// invisible in flat one-declaration inputs and appears as soon as the code is
// nested — which real OCaml is.
func TestDepthWalker_InheritIsMisAttributed_Constructed(t *testing.T) {
	src := "module M = struct\n" +
		"  let a = object\n" +
		"    inherit parent_a\n" +
		"    method x = 1\n" +
		"    end\n" +
		"  let b = object\n" +
		"    inherit parent_b\n" +
		"    method y = 2\n" +
		"    end\n" +
		"end\n"

	inherits := inheritOffsets(src)
	if len(inherits) != 2 {
		t.Fatalf("premise: want 2 inherit tokens, got %d", len(inherits))
	}
	objs := objectOffsets(src)
	if len(objs) != 2 {
		t.Fatalf("premise: want 2 object tokens, got %d", len(objs))
	}

	count := func(body func(string, int) string) int {
		var mis int
		for i, o := range objs {
			truth := refMatchEnd(src, o)
			if truth < 0 {
				t.Fatalf("premise: reference failed on a constructed case")
			}
			walkEnd := o + len(body(src, o))
			for j, in := range inherits {
				inTruth := in >= o && in < truth
				inWalker := in >= o && in < walkEnd
				if inTruth != inWalker {
					mis++
					t.Logf("object #%d span [%d,%d) (true end %d) mis-attributes inherit #%d at %d: in_truth=%v in_walker=%v",
						i, o, walkEnd, truth, j, in, inTruth, inWalker)
				}
			}
		}
		return mis
	}

	// #6812 arm 2: the input is unchanged and so is the legacy verdict. What is
	// asserted now is a PAIR — the retired walker still mis-attributes here, and
	// the CST-backed producer does not. Asserting only the second would be an
	// absence over a population that a broken generator could empty; asserting
	// both means the fix is graded against a demonstrated failure on the same
	// bytes rather than against a claim about them.
	legacyMis := count(legacyDepthWalkerBody)
	if legacyMis == 0 {
		t.Fatalf("the RETIRED walker no longer mis-attributes on this input — legacyDepthWalkerBody " +
			"is supposed to be extractModuleBody verbatim as of 2de485618. Either it was edited or " +
			"the reference moved; either way the #6812 figure needs re-deriving, not this test deleting.")
	}
	cstMis := count(extractModuleBody)
	if cstMis != 0 {
		t.Fatalf("the CST-backed extractModuleBody mis-attributes %d/%d (object, inherit) pairs on the "+
			"input the grammar wiring exists to fix", cstMis, len(objs)*len(inherits))
	}
	t.Logf("constructed input: retired walker %d/%d (object, inherit) attributions wrong, CST-backed %d/%d",
		legacyMis, len(objs)*len(inherits), cstMis, len(objs)*len(inherits))
}

// ---------------------------------------------------------------------------
// The measurement, on a real corpus (env-gated)
// ---------------------------------------------------------------------------

// TestDepthWalker_InheritAttribution_CorpusMeasurement is #6812's headline
// figure, and since arm 2 it is a BEFORE and AFTER taken in one run: the
// retired regex depth walker and the CST-backed extractModuleBody are scored
// over the same files, the same blocks and the same reference.
//
// # What the reference is still for, now that the producer is CST-backed
//
// This deserves saying plainly rather than letting a tautology read as a pass.
// Arm 1's reviewer cross-checked refMatchEnd against THIS repo's vendored
// tree-sitter OCaml grammar over these same files: 595 blocks compared, 595
// agree. So refMatchEnd and the grammar are known to answer the same question
// the same way, and "the CST-backed producer agrees with refMatchEnd" is
// therefore NOT the independent confirmation it was when the producer was a
// regex walker.
//
// It is not vacuous either, and the distinction is the code in between.
// refMatchEnd is a hand-written matcher that shares no line with blocks.go;
// what it still grades is everything the wiring added on top of the grammar —
// which node types count as openers, pairing an opener with the `end` of the
// SAME parent node, rejecting zero-width MISSING closers, the binary search
// for "first opener at or after `from`", and the per-source memo handing back
// the right file's index. Every one of those is a place this arm could have
// been wrong, and none of them is in the grammar.
//
// What the reference can no longer do is arbitrate a disagreement. If the two
// ever differ, this test says so but cannot say which is right; that verdict
// needs the grammar's own S-expression, not another run of this test.
func TestDepthWalker_InheritAttribution_CorpusMeasurement(t *testing.T) {
	root := os.Getenv("GRAFEL_OCAML_CORPUS")
	if root == "" {
		t.Skip("set GRAFEL_OCAML_CORPUS to a checkout of real OCaml source to run the measurement")
	}

	type score struct {
		name            string
		body            func(string, int) string
		blockEndWrong   int
		blockEndWrongOK int // …of which sit in a file the grammar parses CLEANLY
		inheritEnclosed int
		inheritMissed   int
		inheritSpurious int
	}
	producers := []*score{
		{name: "retired regex depth walker", body: legacyDepthWalkerBody},
		{name: "CST-backed extractModuleBody", body: extractModuleBody},
	}

	var (
		files, blocks, unmatched int
		inheritTotal             int
		filesWithInherit         int
		filesWithErrors          int
		unmatchedFiles           []string
	)
	err := filepath.Walk(root, func(p string, fi os.FileInfo, err error) error {
		if err != nil || fi.IsDir() || filepath.Ext(p) != ".ml" {
			return nil //nolint:nilerr // a corpus read error skips one file, it does not fail the measurement
		}
		b, rerr := os.ReadFile(p) //nolint:gosec // path comes from the operator's own corpus
		if rerr != nil {
			return nil
		}
		src := string(b)
		files++
		inh := inheritOffsets(src)
		if len(inh) == 0 {
			// The question is about `inherit` context, so the population is the
			// files that contain one. Scoped rather than sampled: EVERY such
			// file in the corpus is measured. (It is also what keeps the run
			// finishable — the retired walker re-runs two regexes at every byte
			// of the remaining file, so it is quadratic per block.)
			return nil
		}
		inheritTotal += len(inh)
		filesWithInherit++
		// Whether the GRAMMAR had to recover on this file, taken from the tree
		// rather than from the file's name. errors.ml is the corpus's one
		// deliberately-malformed file, and hard-coding its path here would make
		// the assertion below true by naming rather than by measurement.
		recovered := blockIndexFor(src).hasError
		if recovered {
			filesWithErrors++
		}
		for _, o := range objectOffsets(src) {
			truth := refMatchEnd(src, o)
			if truth < 0 {
				unmatched++
				if len(unmatchedFiles) < 10 {
					unmatchedFiles = append(unmatchedFiles, p)
				}
				continue
			}
			blocks++
			for _, s := range producers {
				walkEnd := o + len(s.body(src, o))
				if !endsAgree(src, walkEnd, truth) {
					s.blockEndWrong++
					if !recovered {
						s.blockEndWrongOK++
					}
				}
				for _, in := range inh {
					inTruth := in >= o && in < truth
					inWalker := in >= o && in < walkEnd
					switch {
					case inTruth && inWalker:
						s.inheritEnclosed++
					case inTruth && !inWalker:
						// The span stops SHORT of an inherit that is really
						// inside the block: a real inheritance edge this
						// producer would not attribute.
						s.inheritEnclosed++
						s.inheritMissed++
					case !inTruth && inWalker:
						// The span runs PAST its end and swallows an inherit
						// belonging to a later block: an edge attributed to the
						// wrong owner. Recall assertions are structurally blind
						// to this direction (#6902), which is why it is counted
						// separately rather than folded into one error figure.
						s.inheritSpurious++
					}
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	if blocks == 0 {
		t.Fatalf("corpus %q produced no `object … end` blocks — a zero here is a corpus problem, not a result", root)
	}
	pct := func(n, d int) string {
		if d == 0 {
			return "n/a"
		}
		return fmt.Sprintf("%.1f%%", 100*float64(n)/float64(d))
	}
	t.Logf("corpus=%s files=%d files_with_inherit=%d", root, files, filesWithInherit)
	t.Logf("object blocks matched by reference=%d (unmatched, excluded=%d)", blocks, unmatched)
	t.Logf("inherit tokens in those files=%d; files the grammar had to RECOVER on=%d", inheritTotal, filesWithErrors)
	for _, s := range producers {
		t.Logf("--- %s ---", s.name)
		t.Logf("BLOCK-END WRONG: %d/%d (%s); of those, %d are in files the grammar parses cleanly",
			s.blockEndWrong, blocks, pct(s.blockEndWrong, blocks), s.blockEndWrongOK)
		// Recall and precision are reported separately and never summed into
		// one rate. Adding them gave 230/216 = 106.5%, which is not a rate at
		// all: `spurious` is drawn from every (block, inherit) pair in the file
		// while `enclosed` counts only the truly-enclosed ones, so the two have
		// different denominators (review).
		correct := s.inheritEnclosed - s.inheritMissed
		attributed := correct + s.inheritSpurious
		t.Logf("(block,inherit) pairs truly enclosed=%d", s.inheritEnclosed)
		t.Logf("INHERIT RECALL: %d/%d truly-enclosed pairs are inside the span (%s); %d MISSED",
			correct, s.inheritEnclosed, pct(correct, s.inheritEnclosed), s.inheritMissed)
		t.Logf("INHERIT PRECISION: %d/%d pairs attributed are right (%s); %d SPURIOUS",
			correct, attributed, pct(correct, attributed), s.inheritSpurious)
		t.Logf("attributes %d pairs, %d of them wrong", attributed, s.inheritSpurious+s.inheritMissed)
	}
	if len(unmatchedFiles) > 0 {
		t.Logf("sample files holding a block the REFERENCE could not match (excluded from every "+
			"figure above, so they neither help nor hurt either producer): %v", unmatchedFiles)
	}
	legacy, cst := producers[0], producers[1]
	if legacy.blockEndWrong == 0 {
		t.Fatalf("the RETIRED walker made zero block-end errors over %d blocks — that contradicts "+
			"the constructed cases; suspect the harness before believing it", blocks)
	}
	// The acceptance criterion for #6812 arm 2, asserted rather than left to a
	// reader of the log. Written as a strict improvement in BOTH directions
	// because a producer can buy precision by attributing less: a span that
	// stopped at the opener would have perfect precision and no recall.
	if cst.blockEndWrong >= legacy.blockEndWrong {
		t.Fatalf("the CST-backed producer put the wrong end on %d/%d blocks against the retired "+
			"walker's %d — the wiring bought nothing", cst.blockEndWrong, blocks, legacy.blockEndWrong)
	}
	if cst.inheritMissed > legacy.inheritMissed {
		t.Fatalf("the CST-backed producer MISSES more truly-enclosed inherits (%d) than the retired "+
			"walker (%d) — precision was bought with recall", cst.inheritMissed, legacy.inheritMissed)
	}
	if cst.inheritSpurious >= legacy.inheritSpurious {
		t.Fatalf("the CST-backed producer attributes %d spurious inherits against the retired "+
			"walker's %d", cst.inheritSpurious, legacy.inheritSpurious)
	}
	// The strongest thing this corpus can say about the wiring, and much
	// stronger than a bounded error count: on VALID OCaml the CST-backed
	// producer and the independent reference do not disagree at all. Every
	// residual disagreement is in a file tree-sitter itself reports as broken,
	// which is where the two are entitled to differ — recovery is a heuristic
	// and refMatchEnd is not the same heuristic.
	//
	// Asserted as an exact zero rather than a threshold. A threshold would let
	// a real regression on valid source hide under a budget the malformed file
	// already fills.
	if cst.blockEndWrongOK != 0 {
		t.Fatalf("the CST-backed producer disagrees with the reference on %d blocks in files the "+
			"grammar parses WITHOUT error — those are not recovery artefacts and need explaining",
			cst.blockEndWrongOK)
	}
	if filesWithErrors == 0 {
		t.Fatalf("no file in the corpus needed recovery, so the assertion above was vacuous — " +
			"this corpus is supposed to contain testsuite/tests/generated-parse-errors/errors.ml")
	}
}

// divergingCombinations6812 is the recorded diverging set for
// TestDepthWalker_ObjectBlockEnd_EnumeratedSpace. See that test for the axis
// letters.
var divergingCombinations6812 = []string{
	// Listed in the enumeration's own iteration order (bitmask ascending), so a
	// failure reads as a diff rather than a set comparison. 18 of 32.
	//
	// A absent — the block's `end` INDENTED, which is how an object inside a
	// `let` is conventionally written — diverges on its own: the indentation
	// fallback consumes past the `end` line. That is the corpus's commonest
	// shape.
	//
	// B (an identifier ENDING in `end`, spelled `append` — see the generator
	// for why the underscore mattered) is live in both directions, which is the
	// whole reason it was respelled. `-B---` does NOT diverge: the phantom
	// `end` inside `append` takes depth 1->0 early, so the block's real `end`
	// is then seen AT depth 0 and terminates the scan at the right place — the
	// defect accidentally cancels defect (1). Add a comment or a quoted string
	// on top (`-BC--`, `-B-D-`) and it stops cancelling and starts truncating.
	//
	// C and D each diverge only in company. Neither `--C--` nor `---D-` is
	// here; `--CD-` is. Testing them in isolation finds nothing, which is why
	// this table is enumerated and not hand-picked.
	"-----", "-BC--", "ABC--", "-B-D-", "AB-D-", "--CD-", "A-CD-", "-BCD-", "ABCD-",
	"----E", "-BC-E", "ABC-E", "-B-DE", "AB-DE", "--CDE", "A-CDE", "-BCDE", "ABCDE",
}

// cstDivergingCombinations6812 is the re-derived diverging set for the
// CST-backed extractModuleBody (#6812 arm 2). It is EMPTY: none of the 32
// enumerated combinations puts the wrong end on the block once the span comes
// from the grammar.
//
// Each of the four axes the enumeration crosses is one of the walker's four
// measured defects, and the empty set is the per-axis account of why:
//
//	A  an INDENTED `end` — defect (1), the scan never terminating on a balanced
//	   block, so every well-formed block fell through to the indentation
//	   fallback and the fallback consumed past the `end` line. A node span has
//	   no counter to fail to terminate.
//	B  an identifier ENDING in `end` (`append`) — defect (2), `\b` evaluated at
//	   offset 0 of a re-sliced string. The grammar tokenises, so `append` is a
//	   method_name and never a closing keyword. Note this axis is where the
//	   walker accidentally CANCELLED defect (1) on its own (`-B---` did not
//	   diverge); the CST does not need the cancellation.
//	C  a NESTED comment — defect (3). `(* (* q *) end *)` is one `comment` node.
//	D  a `{|…|}` quoted string holding `end` — defect (4). It is a
//	   `quoted_string` node with a `quoted_string_content` child.
//
// Recorded as a named empty literal rather than as `len(got) == 0` so that the
// two producers are asserted the same way, and so the day a combination starts
// diverging the failure reads as a diff against a recorded set.
var cstDivergingCombinations6812 = []string{}
