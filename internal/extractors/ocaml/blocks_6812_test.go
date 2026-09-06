// blocks_6812_test.go — unit grading for the CST-backed block index (#6812
// arm 2). The before/after MEASUREMENT lives in
// depth_walker_inherit_accuracy_6812_test.go; this file grades the wiring
// itself — the parts of blocks.go that sit between the grammar and the answer
// and that the grammar therefore cannot be credited for.
package ocaml

import (
	"strings"
	"testing"
)

// TestBlockIndex_AllFourOpeners is the coverage assertion for blockOpeners.
//
// The map has four entries and the two measurement tests exercise exactly one
// of them: the enumeration generates `object` and nothing else. Deleting
// "begin", "sig" or "struct" therefore left the entire package green, which
// makes three quarters of the table ungraded — and `sig` in particular is the
// one production actually reaches most often, via `module type T = sig … end`.
//
// Each case pins the exact body text rather than a length, so a span that is
// off by the width of the opener keyword (the class of defect that lived
// undetected in the harness's own reference for a week, as `i += 6`) fails
// here rather than passing a byte count.
func TestBlockIndex_AllFourOpeners(t *testing.T) {
	cases := []struct {
		name string
		src  string
		from int
		want string
	}{
		{"struct", "module M = struct\n  let a = 1\n", 10, " struct\n  let a = 1\n"},
		{"sig", "module type T = sig\n  val f : int\n", 15, " sig\n  val f : int\n"},
		{"object", "let c = object\n  method a = 1\n", 7, " object\n  method a = 1\n"},
		{"begin", "let x = begin\n  1\n", 7, " begin\n  1\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// The `end` is appended here so the `want` above reads as the body
			// and the test cannot accidentally assert the closer's position.
			src := tc.src + "end\n"
			bi := blockIndexFor(src)
			if !bi.parsed {
				t.Fatalf("the grammar produced no tree for %q", src)
			}
			if len(bi.openers) != 1 {
				t.Fatalf("want exactly 1 block opener indexed, got %d: %v", len(bi.openers), bi.openers)
			}
			got := extractModuleBody(src, tc.from)
			if got != tc.want {
				t.Fatalf("body mismatch\n got: %q\nwant: %q", got, tc.want)
			}
			// And it came from the index, not from the indentation fallback:
			// the fallback would stop at the column-0 `end` line and so would
			// return the same text here. Asserting bodyEnd directly is what
			// separates "the answer is right" from "the answer is right for
			// the reason this file claims".
			if _, ok := bi.bodyEnd(tc.from); !ok {
				t.Fatalf("bodyEnd declined — %q was answered by the indentation fallback, "+
					"so this opener is not in blockOpeners", tc.name)
			}
		})
	}
}

// TestBlockIndex_NestedBlocksPairWithTheirOwnEnd pins the opener-to-`end`
// pairing rule: an `end` closes the block opened by the SAME parent node, not
// the nearest one in byte order. A `collect` that recorded the last `end` it
// walked past — the obvious way to write it — gives the outer block the inner
// block's closer.
func TestBlockIndex_NestedBlocksPairWithTheirOwnEnd(t *testing.T) {
	src := "module M = struct\n" +
		"  let a = object\n" +
		"    method x = 1\n" +
		"  end\n" +
		"  let b = 2\n" +
		"end\n"
	bi := blockIndexFor(src)
	if len(bi.openers) != 2 {
		t.Fatalf("want the struct and the object indexed, got %d: %v", len(bi.openers), bi.openers)
	}
	outer, ok := bi.bodyEnd(bi.openers[0])
	if !ok {
		t.Fatal("the outer struct has no end")
	}
	inner, ok := bi.bodyEnd(bi.openers[1])
	if !ok {
		t.Fatal("the inner object has no end")
	}
	if inner >= outer {
		t.Fatalf("the inner object's end (%d) is not before the outer struct's (%d) — "+
			"the two blocks share a closer", inner, outer)
	}
	if !strings.Contains(src[bi.openers[0]:outer], "let b = 2") {
		t.Fatalf("the outer struct's body stops before `let b = 2`; it took the object's `end`: %q",
			src[bi.openers[0]:outer])
	}
}

// TestBlockIndex_ParenthesesAreNotBlocks pins why blockOpeners keys on the
// TOKEN and not on the node type: `begin … end` and `( … )` are both
// `parenthesized_expression` in this grammar, so a rule written over node
// types would index every parenthesised expression in the file as a block.
func TestBlockIndex_ParenthesesAreNotBlocks(t *testing.T) {
	src := "let x = (1 + 2)\nlet y = (f (g 3))\n"
	bi := blockIndexFor(src)
	if !bi.parsed {
		t.Fatal("the grammar produced no tree")
	}
	if len(bi.openers) != 0 {
		t.Fatalf("parenthesised expressions were indexed as blocks: %v", bi.openers)
	}
}

// TestBlockIndex_MalformedInput_FallsBack grades #6812's ERROR-node decision.
//
// The corpus that produced the headline figure contains exactly one file the
// reference matcher could not read — testsuite/tests/generated-parse-errors/
// errors.ml, generated deliberately-malformed OCaml with 256 `object` against
// 38 `end`. tree-sitter does not refuse such input; it recovers, producing
// ERROR subtrees and zero-width MISSING tokens. So the grammar WILL hand back
// an answer for an unclosed block, and the decision is what to do with it.
//
// The decision: an opener whose `end` is absent or zero-width is reported as
// unclosed, and extractModuleBody hands back to the indentation heuristic —
// the same fallback the old code already used, and the one it in fact used for
// the majority of real blocks. It does NOT adopt the next block's `end`, which
// is the failure mode that would fabricate a span, and it does NOT return
// nothing.
func TestBlockIndex_MalformedInput_FallsBack(t *testing.T) {
	// Two unclosed `object`s and a single `end`, the shape errors.ml is full of.
	src := "let a = object\n" +
		"  inherit parent_a\n" +
		"  method x = 1\n" +
		"let b = object\n" +
		"  inherit parent_b\n" +
		"end\n"

	bi := blockIndexFor(src)
	if !bi.parsed {
		t.Fatal("premise: the grammar produced no tree for recoverable input — it is supposed to recover")
	}
	if len(bi.openers) != 2 {
		t.Fatalf("premise: want both `object` openers indexed, got %d: %v", len(bi.openers), bi.openers)
	}
	first := bi.openers[0]
	if e, ok := bi.bodyEnd(first); ok {
		t.Fatalf("the unclosed first block was given an end at %d (%q) — that span is fabricated "+
			"from a LATER block's `end` or from a zero-width MISSING token",
			e, src[first:min(e+3, len(src))])
	}

	// And the caller falls back rather than returning nothing: the indentation
	// heuristic stops at the next column-0 declaration, so the body is the two
	// indented lines and not the whole file.
	body := extractModuleBody(src, first)
	if body == "" {
		t.Fatal("extractModuleBody returned nothing for a block the CST could not close — " +
			"silently returning nothing was explicitly rejected in #6812")
	}
	if strings.Contains(body, "parent_b") {
		t.Fatalf("the fallback ran past the next column-0 `let` and swallowed the second block's "+
			"inherit; body=%q", body)
	}
	if !strings.Contains(body, "parent_a") {
		t.Fatalf("the fallback dropped the block's own inherit; body=%q", body)
	}
}

// TestBlockIndex_UnparseableInput_FallsBack pins the other arm of the same
// decision: when the grammar yields nothing usable at all, extractModuleBody
// still answers from the indentation heuristic rather than returning "".
// Asserted through a blockIndex with parsed=false rather than by hunting for
// input tree-sitter refuses, because tree-sitter's recovery means such input
// may not exist — and a guard that can only be reached by input nobody can
// produce is a guard nothing grades.
func TestBlockIndex_UnparseableInput_FallsBack(t *testing.T) {
	bi := &blockIndex{end: map[int]int{0: 10}, openers: []int{0}}
	if _, ok := bi.bodyEnd(0); ok {
		t.Fatal("bodyEnd answered from an index whose parse failed")
	}
	bi.parsed = true
	if e, ok := bi.bodyEnd(0); !ok || e != 10 {
		t.Fatalf("the SAME index answers once parsed is set: got (%d,%v), want (10,true) — "+
			"without this the check above could be passing for the wrong reason", e, ok)
	}
}

// TestBlockIndex_HasErrorTracksRecovery grades the flag the corpus measurement
// leans on, in BOTH directions. A hasError that were stuck true would make the
// corpus's "zero disagreements on cleanly-parsed files" assertion vacuous by
// classifying every file as malformed, and nothing in the env-gated test could
// tell.
func TestBlockIndex_HasErrorTracksRecovery(t *testing.T) {
	clean := "module M = struct\n  let a = 1\nend\n"
	if bi := blockIndexFor(clean); bi.hasError {
		t.Fatalf("hasError is set on well-formed source: %q", clean)
	}
	// Input the grammar genuinely cannot recover from without an ERROR node.
	// Worth recording what does NOT reach here, because the first four
	// candidates tried were all parsed CLEANLY: `let a = object end while ;;`,
	// an unclosed `object`, an unclosed `struct`, and errors.ml's own
	// `if UIdent then with` line. tree-sitter-ocaml is a GLR parser and
	// recovers most malformed OCaml into a plausible tree with MISSING tokens
	// and no ERROR node at all — which is exactly why blockIndex declines on a
	// zero-width `end` rather than trusting hasError to have flagged the file.
	broken := "let ) = ( ;; %%%\n"
	if bi := blockIndexFor(broken); !bi.hasError {
		t.Fatalf("hasError is NOT set on source tree-sitter has to recover from: %q", broken)
	}
}

// TestBlockIndex_EndBeforeFromIsRefused pins the lower bound in bodyEnd's
// `e < from` guard, as distinct from `e < 0`.
//
// The two spellings are equivalent through buildBlockIndex — the binary search
// returns an opener at or after `from`, and a real `end` is strictly after its
// opener, so an end in [0, from) cannot arise. That made a mutant reducing the
// guard to `e < 0` ALIVE against the whole package, and "unreachable through
// today's constructor" is an argument, not a fence: bodyEnd is a method, the
// guard is what stops `src[afterPos:end]` being a negative-length slice, and
// the day the search changes the panic is what tells you.
//
// Cost of the kill, since "too expensive" is a claim that deserves a number:
// nine lines and no production change.
func TestBlockIndex_EndBeforeFromIsRefused(t *testing.T) {
	bi := &blockIndex{parsed: true, openers: []int{5}, end: map[int]int{5: 2}}
	if _, ok := bi.bodyEnd(5); ok {
		t.Fatal("bodyEnd accepted an end that precedes `from`; the caller would slice src[5:2]")
	}
	// Positive control: the same index with a plausible end answers, so the
	// refusal above is the guard firing and not the lookup missing.
	bi.end[5] = 9
	if e, ok := bi.bodyEnd(5); !ok || e != 9 {
		t.Fatalf("got (%d,%v), want (9,true)", e, ok)
	}
}
