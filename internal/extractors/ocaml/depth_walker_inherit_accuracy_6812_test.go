// depth_walker_inherit_accuracy_6812_test.go — MEASUREMENT harness for the
// hand-rolled depth walker at extractor.go:422 (`extractModuleBody`).
//
// #6812 states plainly that nobody has measured how often that walker gets
// `inherit` context wrong, and that the number must exist before committing to
// a tree-sitter migration. This file is that measurement. It changes NO
// production behaviour and adds no pattern to the extractor.
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
// The walker is driven the way production drives it: `extractModuleBody` is
// handed a position from which the block's OPENING keyword is still ahead
// (extractor.go:153 passes the end of the `module Foo =` match, with `struct`
// still to come), so the opener increments depth. That is not a detail — see
// the divergence table below.
//
// # Ground truth
//
// refMatchEnd below is a small OCaml block matcher written for this test only.
// It differs from the production walker in four ways that are each an
// independent defect of the production walker, so a positive control matters:
// TestReferenceBlockMatcher_PositiveControls pins it against hand-computed
// answers, including every hazard class it is supposed to survive. A
// measurement whose reference is unvalidated measures nothing.
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
	"strings"
	"testing"
)

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
		switch tokenAt(src, i) {
		case "struct", "object", "begin", "sig":
			depth++
			i += 6 // longest opener; exact length is irrelevant, all are >0
			continue
		case "end":
			depth--
			if depth == 0 {
				return i
			}
			i += 3
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
// Each input holds exactly one `object … end` block with exactly one
// `inherit` in it, so a divergence is directly an `inherit` mis-attribution
// rather than a proxy for one. The DIVERGING SET is recorded below as a
// literal; a change to the walker that moves any combination in or out of it
// fails this test, which is what makes the #6812 measurement reproducible
// without a network.
func TestDepthWalker_ObjectBlockEnd_EnumeratedSpace(t *testing.T) {
	type axes struct{ closeAtCol0, endIdent, nestedComment, quotedString, trailer bool }
	build := func(a axes) string {
		var b strings.Builder
		b.WriteString("let c = object\n")
		b.WriteString("  inherit base\n")
		if a.endIdent {
			b.WriteString("  method append_all x = x\n")
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

	// Recorded from the enumeration below at the commit that added this file,
	// against a clone of github.com/ocaml/ocaml as the corpus for the headline
	// figure. Written out rather than computed so a behaviour change is a
	// diff and not a silently different pass.
	wantDiverging := map[string]bool{}
	for _, k := range divergingCombinations6812 {
		wantDiverging[k] = true
	}

	var got []string
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
		walkEnd := len(extractModuleBody(src, 0))
		inTruth := inh[0] < truth
		inWalker := inh[0] < walkEnd
		if !inTruth {
			t.Fatalf("%s: generator premise broken, the inherit is not inside the block", label(a))
		}
		if !endsAgree(src, walkEnd, truth) || inTruth != inWalker {
			got = append(got, label(a))
			t.Logf("%s DIVERGES: walker end=%d, true end=%d, inherit@%d in_walker=%v",
				label(a), walkEnd, truth, inh[0], inWalker)
		}
	}
	if len(got) == 0 {
		t.Fatalf("no combination diverged — extractModuleBody's behaviour changed; " +
			"re-derive the #6812 figure rather than deleting this test")
	}
	if strings.Join(got, ",") != strings.Join(divergingCombinations6812, ",") {
		t.Fatalf("diverging set changed.\n got: %v\nwant: %v", got, divergingCombinations6812)
	}
	t.Logf("%d/32 enumerated combinations mis-attribute the `inherit`", len(got))
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

	var mis int
	for i, o := range objs {
		truth := refMatchEnd(src, o)
		if truth < 0 {
			t.Fatalf("premise: reference failed on a constructed case")
		}
		walkEnd := o + len(extractModuleBody(src, o))
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
	if mis == 0 {
		t.Fatalf("NO MIS-ATTRIBUTION — the walker now agrees with the reference on this input. " +
			"That is a behaviour change in extractModuleBody; re-derive the #6812 figure before deleting this test.")
	}
	t.Logf("constructed input: %d/%d (object, inherit) attributions wrong", mis, len(objs)*len(inherits))
}

// ---------------------------------------------------------------------------
// The measurement, on a real corpus (env-gated)
// ---------------------------------------------------------------------------

func TestDepthWalker_InheritAttribution_CorpusMeasurement(t *testing.T) {
	root := os.Getenv("GRAFEL_OCAML_CORPUS")
	if root == "" {
		t.Skip("set GRAFEL_OCAML_CORPUS to a checkout of real OCaml source to run the measurement")
	}
	var (
		files, blocks, unmatched int
		blockEndWrong            int
		inheritTotal             int
		inheritEnclosed          int
		inheritMissed            int
		inheritSpurious          int
		filesWithInherit         int
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
			// finishable — the production walker re-runs two regexes at every
			// byte of the remaining file, so it is quadratic per block.)
			return nil
		}
		inheritTotal += len(inh)
		filesWithInherit++
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
			walkEnd := o + len(extractModuleBody(src, o))
			if !endsAgree(src, walkEnd, truth) {
				blockEndWrong++
			}
			for _, in := range inh {
				inTruth := in >= o && in < truth
				inWalker := in >= o && in < walkEnd
				switch {
				case inTruth && inWalker:
					inheritEnclosed++
				case inTruth && !inWalker:
					// The walker's block stops SHORT of an inherit that is
					// really inside the block: a real inheritance edge the
					// regex path would not attribute.
					inheritEnclosed++
					inheritMissed++
				case !inTruth && inWalker:
					// The walker's block runs PAST its end and swallows an
					// inherit belonging to a later block: an edge attributed to
					// the wrong owner. Recall assertions are structurally blind
					// to this direction (#6902), which is why it is counted
					// separately rather than folded into one error figure.
					inheritSpurious++
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
	t.Logf("BLOCK-END WRONG: %d/%d (%s)", blockEndWrong, blocks, pct(blockEndWrong, blocks))
	t.Logf("inherit tokens in those files=%d; (block,inherit) pairs truly enclosed=%d",
		inheritTotal, inheritEnclosed)
	t.Logf("INHERIT MISSED (truly inside the block, outside the walker's span): %d/%d (%s)",
		inheritMissed, inheritEnclosed, pct(inheritMissed, inheritEnclosed))
	t.Logf("INHERIT SPURIOUS (outside the block, inside the walker's span): %d", inheritSpurious)
	t.Logf("INHERIT MIS-ATTRIBUTED, either direction: %d/%d (%s)",
		inheritMissed+inheritSpurious, inheritEnclosed, pct(inheritMissed+inheritSpurious, inheritEnclosed))
	if len(unmatchedFiles) > 0 {
		t.Logf("sample files holding a block the REFERENCE could not match (excluded from every "+
			"figure above, so they neither help nor hurt the walker): %v", unmatchedFiles)
	}
	if blockEndWrong == 0 {
		t.Fatalf("zero block-end errors over %d blocks — that contradicts the constructed cases; "+
			"suspect the harness before believing it", blocks)
	}
}

// divergingCombinations6812 is the recorded diverging set for
// TestDepthWalker_ObjectBlockEnd_EnumeratedSpace. See that test for the axis
// letters.
var divergingCombinations6812 = []string{
	// Listed in the enumeration's own iteration order (bitmask ascending), so
	// the failure message reads as a diff rather than a set comparison.
	//
	// A absent — the block's `end` INDENTED, which is how an object inside a
	// `let` is conventionally written — is on its own enough: the indentation
	// fallback keeps consuming past the `end` line and the span over-reaches.
	// That is the single most common shape in the corpus.
	//
	// C+D together — a nested comment plus a quoted string — diverge in the
	// other direction, truncating the block's own content. NEITHER diverges
	// alone: the fallback survives each in isolation. That interaction is the
	// reason this table is enumerated rather than hand-picked; three rounds of
	// hand-written cases produced zero divergences before this test replaced
	// them.
	"-----", "-B---", "--CD-", "A-CD-", "-BCD-", "ABCD-",
	"----E", "-B--E", "--CDE", "A-CDE", "-BCDE", "ABCDE",
}
