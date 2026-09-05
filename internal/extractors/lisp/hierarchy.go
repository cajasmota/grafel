// hierarchy.go — Common Lisp CLOS inheritance topology:
// `(defclass name (super ...) ...)` → EXTENDS (#6370).
//
// Before this, lisp emitted NO hierarchy edge by any of the three paths a
// language can get one. All three were re-verified for lisp specifically
// rather than inherited from the issue's list, because two of the issue's
// original nine turned out to be wrong about exactly this:
//
//  1. this package contained no "EXTENDS"/"IMPLEMENTS" literal at all;
//  2. none of the three tokens this package registers — "commonlisp",
//     "scheme", "racket" — is in `supportedLanguages` in
//     `internal/extractors/cross/hierarchy/extractor.go`;
//  3. `internal/engine/rules/` has no lisp/commonlisp/scheme/racket directory,
//     and no rule pack anywhere in that tree mentions any of those tokens.
//     This is the path #6370's original audit could not see — it grepped the
//     extractor tree only, which is how graphql was mis-listed as absent.
//
// `defclassRE` stopped at the declared name, so "what extends this class"
// returned empty — indistinguishable from "nothing does".
//
// # Why EXTENDS, and why there is no kind ladder
//
// CLOS has ONE superclass relation. Every name in a defclass's superclass list
// is a superclass in the same sense; there is no interface/class distinction to
// select between, no `implements` form, and the standard-class metaobject
// protocol does not separate the two. So there is nothing here a ladder could
// discriminate, and — unlike `internal/extractors/csharp/hierarchy.go`, whose
// ladder is pinned as wrong in both directions — no ladder to get wrong.
//
// # Why the edge is emitted HERE and not by registering lisp in cross/hierarchy
//
// That pass invents its own graph nodes: for every type it addEntity's a
// SCOPE.Component for the type AND another for each named parent. extractLisp
// already emits one SCOPE.Component per defclass, so registering lisp there
// would mint a duplicate component per class with the edges anchored on the
// pass's own node rather than the one the rest of the lisp graph uses. That is
// why #6335 emitted F#'s edges from the F# extractor, #6437 groovy's, #6804
// nim's and #6810 pony's. TestLispHierarchy_NoDuplicateComponentPerClass
// guards both halves.
//
// # THE hard part: telling the superclass list from the slot list
//
// A defclass form is multi-line and nests:
//
//	(defclass poodle
//	    (dog pet)                  ; superclass list
//	  ((size :initarg :size)       ; slot list
//	   (age  :initform 0)))
//
// Both are parenthesised groups on their own lines, and a producer that
// searches forward for "the next parenthesised group" reads the slot list as
// superclasses whenever the superclass list is absent or unusual.
//
// superclassEdges NEVER searches. It is handed the byte offset at which
// defclassRE's NAME capture ended and requires, at exactly that position after
// whitespace only, an opening `(`. CLOS fixes the superclass list as the form
// immediately after the name — it is mandatory and positional, `()` when there
// are none — so in a WELL-FORMED defclass the FIRST group at that anchor is the
// superclass list by the grammar, the slot list is out of reach because it is
// the SECOND group, and every other parenthesised group in the file is excluded
// by position rather than by a blocklist.
//
// "Well-formed" is load-bearing and is not an aside. Position decides only
// while the mandatory list is actually written. `(defclass flat (name legs))`
// omits it and writes FLAT slots, and that group is textually identical to a
// two-parent superclass list — a flat group of plain symbols in the position
// CLOS reserves for parents. Nothing in the text distinguishes them, so this
// producer emits `flat EXTENDS name` and `flat EXTENDS legs`. That is left as
// is on purpose: the alternative is a heuristic on malformed input that would
// cost real edges on well-formed input.
// TestLispHierarchy_OmittedSuperclassListWithFlatSlotsEmitsWrongEdges_KnownDivergence
// pins it with a positive control, so the limit is measured rather than
// claimed away. Note that the golden fixture is NOT evidence the stronger
// claim holds: its `lonely` case dodges this shape by using a NESTED slot
// list, which the nesting guard catches for an unrelated reason.
//
// TWO further guards back that up, and which of them covers a given input
// matters, because mutant scoring showed they MASK each other:
//
//   - lispBalancedGroup rejects a NESTED group. A superclass list contains only
//     symbols; a slot list is a list OF LISTS, so any inner `(` means the
//     anchored group is not a superclass list and nothing is emitted.
//   - lispSuperclassNameRE is anchored at both ends, so a token that is not a
//     plain symbol — `(name`, `:initarg`, `:name)` — is dropped on its own.
//     Its TWO anchors are a masking pair of the same kind one level further
//     in, and were found the same way: dropping `^` alone, or `$` alone, left
//     the whole unit suite and the golden gate green, because every token in
//     every other input is either a whole symbol or is rejected by the other
//     anchor too. Each is now graded on an input where only it decides —
//     see the regex's own comment below.
//
// On the ordinary malformed case `(defclass lonely ((name :initarg :name)))`
// EITHER guard alone already yields zero edges, so removing one changes
// nothing observable and a test written on that input grades neither. The one
// input that separates them is a group mixing a symbol with a sub-list,
// `(defclass weird (base (extra)) ())`: without the nesting guard that emits
// `weird EXTENDS base`. TestLispHierarchy_NestedFormInsideTheGroupEmitsNothing
// is written on exactly that input for exactly that reason, and
// TestLispHierarchy_SlotListIsNeverReadAsSuperclasses covers the ordinary case
// as an outcome test without claiming to grade a particular guard.
//
// The same masking decides the golden fixture's shape: its root classes are
// written `(defclass animal () (name legs))` with FLAT slot lists, because
// with `((name :initarg :name))` a producer that skipped the empty `()` and
// read the next group would still emit nothing and the forbidden row would be
// decorative.
//
// # What the anchor does and does NOT guard against
//
// Stated rather than left to be discovered, because two earlier languages in
// this series shipped a header claiming more than the code did:
//
//   - it DOES bound the search to the single group at the anchor. The scan
//     between the name and the `(` accepts whitespace ONLY, so no other form,
//     symbol or reader macro may intervene. `(defclass odd)` followed by a
//     later `(defclass dog (animal) ())` produces exactly one edge, not a
//     drift from `odd` into `dog`'s list
//     (TestLispHierarchy_NonListAfterNameEmitsNothing). That test alone does
//     NOT grade the anchor, though, and saying so is the point: replacing the
//     anchor with "find the next `(` ahead" left it green, because the next
//     group ahead is itself nested and the nesting guard catches it. The test
//     that grades the anchor is
//     TestLispHierarchy_AnchorDoesNotSearchForwardIntoALaterForm, whose later
//     form is FLAT (`(list a b)`); the searching producer emits three wrong
//     edges there. It was added after scoring, not before.
//   - the whitespace skip DOES cross newlines, deliberately and necessarily:
//     `(defclass poodle\n    (dog pet)\n  (…))` is ordinary Lisp formatting.
//     So the bound is "the next non-whitespace byte", NOT "the rest of the
//     declaration line" — the narrower claim pony could make does not hold
//     here and is not made.
//   - it does NOT do its own comment or string handling, and claims none.
//     What excludes a `; (defclass ghost (phantom) ())` or a defclass inside a
//     string literal is stripLispStringsAndComments, which is PRE-EXISTING,
//     blanks those bytes to spaces offset-for-offset, and removes the class
//     ENTITY along with the edge. The two negative-control tests for those
//     shapes say so in their own doc comments rather than taking credit.
//     The useful consequence is the reverse case: because a `;` comment is
//     blanked to spaces, a comment BETWEEN the name and the superclass list is
//     transparently skipped by the whitespace scan
//     (TestLispHierarchy_CommentBetweenNameAndSuperclassList).
//   - it has no opinion on whether the enclosing form is well-formed Lisp; it
//     reads a group, not a parse tree.
//
// # Dialect: Common Lisp only, deliberately
//
// This extractor serves three registered languages. `defclass` is Common Lisp;
// the gate is structural rather than a flag — superclassEdges is called only
// from the defclass loop, which already sits inside `if dialect ==
// "commonlisp"`. Scheme's Racket `struct` spells its parent as a bare symbol
// in a different position and is a different construct. GOOPS/Guile
// `(define-class <foo> (<bar>) …)` DOES have the same shape and could reuse
// this function verbatim, but it is a Guile extension rather than Scheme, the
// scheme fixture corpus has none, and widening a producer on an unmeasured
// dialect is how over-firing ships. It is left out on purpose and PINNED
// rather than merely mentioned, by
// TestLispHierarchy_SchemeDefineClassEmitsNothing_KnownDivergence, which turns
// red the moment someone wires it — with a positive control so it cannot decay
// into a test of nothing.
package lisp

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/cajasmota/grafel/internal/types"
)

// lispSuperclassNameRE matches ONE superclass name, whole. Group 1 is an
// optional package qualifier (`sb-mop:` or `sb-mop::` — BOTH forms are graded,
// and separately) and group 2 the bare symbol. Being anchored at both ends is what rejects anything that is not a
// plain symbol — a stray reader macro, a keyword-only token — instead of
// half-matching it into a wrong edge.
//
// The two anchors are graded SEPARATELY, because they mask each other on every
// ordinary token and a one-character deletion of either one was ALIVE against
// the whole unit suite and the golden gate:
//
//   - `^` alone decides `('quoted)`: without it the match simply starts at `q`
//     and emits `x EXTENDS quoted`
//     (TestLispHierarchy_QuoteReaderMacroIsNotHalfMatched).
//
//   - `$` alone decides a token with an interior non-symbol byte: `foo,bar`
//     half-matches to `foo` without `$` and to `bar` without `^`, so that one
//     input kills each deletion in a different direction and neither anchor
//     can hide behind the other
//     (TestLispHierarchy_TokenWithAnInteriorSeparatorIsRejectedWhole).
//
//   - the LEADING character class of group 2 excludes `:`, which is the only
//     thing that rejects a keyword-only token such as `:initarg` written
//     DIRECTLY in the superclass list
//     (TestLispHierarchy_KeywordInTheSuperclassListIsDropped, with
//     TestLispHierarchy_KeywordBesideARealNameDropsOnlyTheKeyword asserting the
//     rejection is per TOKEN, not per group). This clause was ungraded for two
//     rounds: `:initarg` appeared only inside NESTED slot lists, where
//     lispBalancedGroup rejects the group before this regex is consulted, so
//     the nesting guard masked it the same way it masked the anchor.
//
//   - the `?` in `::?` is what accepts the internal-symbol form `pkg::sym`
//     alongside `pkg:sym`. Narrowing it to `:` LOSES the edge entirely, and the
//     single-colon row does not reach it
//     (TestLispHierarchy_DoubleColonQualifierIsErasedToo). The opposite
//     narrowing — requiring two colons — is graded by
//     TestLispHierarchy_PackageQualifierErasedIntoBaseProperty.
//
//   - `'` is absent from the leading class but PRESENT in the trailing one, so
//     `foo'bar` is taken as ONE symbol. That is now graded, but it is probably
//     the WRONG answer — `'` terminates a token in the CL reader — so it is
//     pinned as a known divergence rather than quietly widened or quietly
//     narrowed
//     (TestLispHierarchy_InteriorApostropheIsKeptInTheSymbol_KnownDivergence).
//
// The token splitter above it is graded on its own axis too: `strings.Fields`
// accepts ANY whitespace run inside the group, and `(alpha\tbeta\n gamma)` is
// the only input that says so
// (TestLispHierarchy_WhitespaceWithinTheGroupIsAnySequence). Every other test
// and every fixture row keeps the list on one line, single-space separated.
var lispSuperclassNameRE = regexp.MustCompile(
	`^(?:([\w\-\?!\*+<>=./]+)::?)?([\w\-\?!\*+<>=.][\w\-\?!\*'+<>=./]*)$`)

// superclassEdges returns the EXTENDS relationships declared by one defclass
// form's superclass list, or nil when there is none.
//
// `src` is the SCRUBBED source (strings and comments already blanked to spaces
// offset-for-offset), `afterName` the byte offset just past defclassRE's name
// capture, `owner` that name and `line` the 1-based line of the defclass form
// itself — not of the superclass list, which may sit lines below
// (TestLispHierarchy_LineNumberFollowsTheEntitysOwnStartLine).
//
// The records are meant to be EMBEDDED on the owning class's EntityRecord, not
// appended to a standalone slice: only resolve.ReferencesEmbedded supplies the
// parent's file and package dir, which the locality tiers rank on.
//
// FromID is deliberately left EMPTY. The assembly loop stamps the owning
// record's own entity id, the only value that anchors the edge on the CLASS. A
// non-empty non-hex FromID (e.g. the file path) would be rewritten by
// ReferencesEmbedded onto the FILE entity, merging every class in a multi-class
// file onto one node — the defect fixed in #6295 (Solidity) and #6298
// (Verilog, Astro), now guarded by
// internal/extractors/file_anchored_rels_guard_test.go (#6367).
//
// ToID is the bare symbol with any package qualifier erased, matching groovy,
// nim and pony, and right for Common Lisp on its own terms: the classes most
// often inherited from (`standard-object`, `error`,
// `sb-mop:funcallable-standard-object`) live in the implementation's own
// packages outside any indexed tree, so a file-pinned structural ref would
// never bind. When the written form differs from the bare name a `base`
// property carries it, so the qualifier is recorded rather than lost.
//
// A class naming itself yields no edge: a self-edge is never information and is
// the signature of a mis-attributed owner (#6369). A name repeated in one list
// yields ONE edge.
func superclassEdges(src string, afterName int, owner string, line int) []types.RelationshipRecord {
	pos := skipLispWhitespace(src, afterName)
	if pos >= len(src) || src[pos] != '(' {
		return nil
	}
	inner, ok := lispBalancedGroup(src, pos)
	if !ok {
		return nil
	}

	var out []types.RelationshipRecord
	seen := make(map[string]bool)
	for _, tok := range strings.Fields(inner) {
		m := lispSuperclassNameRE.FindStringSubmatch(tok)
		if m == nil {
			continue
		}
		bare := m[2]
		if bare == owner || seen[bare] {
			continue
		}
		seen[bare] = true

		// types.Props is a key-SORTED slice and Get binary-searches it (#6802);
		// a literal in non-alphabetical key order reads a PRESENT key as absent.
		// Set maintains the order.
		var props types.Props
		props.Set("line", strconv.Itoa(line))
		props.Set("provenance", "lisp_defclass_superclass_list")
		if tok != bare {
			props.Set("base", tok)
		}
		out = append(out, types.RelationshipRecord{
			// FromID intentionally empty — see the doc comment above.
			ToID:       bare,
			Kind:       "EXTENDS",
			Properties: props,
		})
	}
	return out
}

// skipLispWhitespace returns the offset of the first non-whitespace byte at or
// after `pos`. It accepts whitespace ONLY — no comment syntax, no other form —
// which is what confines the anchor to the group CLOS puts immediately after
// the class name. It does cross newlines, because a multi-line defclass is
// ordinary formatting; see the package header for why that is the true bound
// and what it costs.
func skipLispWhitespace(src string, pos int) int {
	for pos < len(src) {
		switch src[pos] {
		case ' ', '\t', '\n', '\r', '\f', '\v':
			pos++
		default:
			return pos
		}
	}
	return pos
}

// lispBalancedGroup returns the interior of the parenthesised group starting at
// `pos` (which must be an open paren), and whether that group is a SUPERCLASS
// list rather than something else.
//
// It reports false in two cases, both load-bearing:
//
//   - the group is unterminated. Running to end of file would swallow every
//     later form's symbols into one class's parent list
//     (TestLispHierarchy_UnterminatedSuperclassListEmitsNothing).
//   - the group NESTS. A CLOS superclass list is a flat list of symbols; a slot
//     list is a list of lists. Any inner `(` therefore means the anchored group
//     is not a superclass list, and emitting nothing beats guessing.
//     The test that GRADES this guard is
//     TestLispHierarchy_NestedFormInsideTheGroupEmitsNothing, not
//     TestLispHierarchy_SlotListIsNeverReadAsSuperclasses: on an ordinary
//     nested slot list lispSuperclassNameRE rejects every token anyway, so
//     that input grades neither guard. See the package header.
//
// The caller passes the scrubbed source, so a paren inside a string or comment
// cannot unbalance the count.
func lispBalancedGroup(src string, pos int) (string, bool) {
	depth := 0
	for i := pos; i < len(src); i++ {
		switch src[i] {
		case '(':
			depth++
			if depth > 1 {
				return "", false
			}
		case ')':
			depth--
			if depth == 0 {
				return src[pos+1 : i], true
			}
		}
	}
	return "", false
}
