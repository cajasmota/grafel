package treesitter

import (
	"sort"
	"strings"

	"github.com/cajasmota/grafel/internal/treesitter/ts"
)

// Kotlin multi-annotation misparse repair (#6736).
//
// THE DEFECT. fwcd/tree-sitter-kotlin 0.3.8 (the vendored grammar — see
// internal/treesitter/ts/grammars/kotlin) resolves an ambiguity the wrong way
// when a top-level declaration carries TWO OR MORE consecutive annotations AND
// is not the last construct in the file. The annotations are read as a chain of
// `prefix_expression` operators rather than as the declaration's `modifiers`,
// and the declaration itself is swallowed into the expression — a Kotlin class
// header degrades to `infix_expression (simple_identifier) (simple_identifier)`
// plus the class body as a trailing `lambda_literal`:
//
//	@RestController                    (source_file
//	@RequestMapping("/api")              (prefix_expression (annotation …)
//	class RealController {                 (prefix_expression (annotation …)
//	    @GetMapping("/kept")                 (infix_expression (simple_identifier)
//	    fun kept(): String = "ok"              (simple_identifier)
//	}                                          (lambda_literal (statements …)))))
//	                                     (function_declaration …))
//	fun looseHelper() = "x"
//
// CRITICALLY, THIS IS NOT AN ERROR RECOVERY. The tree carries ZERO ERROR nodes
// and parses at error_ratio 0.0000, so:
//
//   - the whole-file acceptance gate (maxErrorRatio) passes it silently, and
//   - the #6360 error-skipping wrapper is never even applied (it is gated on
//     errNodes > 0), so it is NOT the cause. Measured, not assumed: the raw
//     `official.Tree` root S-expression is identical to pr.TSTree's for this
//     shape.
//
// The blast radius is every Kotlin consumer, not just Spring routes. The class
// is absent from the tree as a `class_declaration`, so `walkKotlinClasses`
// (internal/engine/spring_routes_kotlin.go) finds nothing to process AND the
// Kotlin extractor loses the SCOPE.Component for the class, demoting its methods
// from `RealController.kept` to a bare top-level `kept`. Since a real Kotlin
// file routinely follows a controller with a top-level extension function, a
// constant `val`, or a second class, this made the Kotlin Spring pass emit
// almost nothing on realistic input.
//
// THE REPAIR. Inserting an explicit statement terminator `;` at the end of the
// misparsed construct disambiguates in favour of the declaration reading —
// measured across class / object / interface / fun carriers, 2 and 3
// annotations, with a fun / val / class / comment following. So: parse, detect
// the signature, insert one `;` per misparse, re-parse, and present the result
// with byte offsets mapped BACK to the original source, so callers keep slicing
// their own unmodified buffer.
//
// SCOPE AND COST. Kotlin only, and only when a top-level `prefix_expression`
// whose first child is an `annotation` is actually present — a check over the
// root's direct children, so a clean file pays one cheap scan and is returned
// completely untouched (same tree, no wrapper, no re-parse). No other grammar
// is reachable from here, which is deliberately unlike the shared #6360 wrapper.
//
// WHY THE `language == "kotlin"` GATE IS UNTESTABLE, AND KEPT ANYWAY. Removing
// it survives the whole suite, and that mutant is reported ALIVE rather than
// killed with a manufactured fixture: a symbol sweep of all 27 vendored grammars
// found that ONLY kotlin carries all three of `prefix_expression`, `annotation`
// and `source_file` (Groovy, the nearest miss, has two of the three). So no
// other grammar can produce the detector's signature, no natural fixture can
// distinguish gated from ungated, and the gate is defence-in-depth against a
// future grammar bump rather than a load-bearing condition.
//
// NOT EVERY SHAPE IS REPAIRABLE, AND THAT IS HANDLED EXPLICITLY. A single-line
// class body — `class One { fun a() = 1 }`, which is more idiomatic Kotlin than
// the multi-line shape — does NOT respond to the terminator at all: the body is
// already being read as a trailing lambda, and appending `;` never converges.
// Re-running the insertion on such a file just accumulates `};;;` and, from the
// third semicolon on, MANUFACTURES ERROR nodes in a file that had none
// (measured: ratio 0.0000 on the raw parse, 0.0347 after three blind rounds on a
// 20-class file). With maxErrorRatio at 0.10 that is a path to dropping a whole
// file the unrepaired parser accepts, so a repair that does not converge must be
// thrown away, not kept.
//
// The guard that does this is the STRICT PER-ROUND PROGRESS check in
// repairKotlinAnnotationMisparse: a round that fails to reduce the misparse
// count is discarded, and rounds that DID make progress are kept, so one
// unrepairable declaration no longer costs a repairable one in the same file. A
// second, never-worse error-count check follows it as defence in depth; it is
// currently unreachable, and says so at its own site rather than claiming to be
// load-bearing.
//
// COLUMN SHIFT IS A NON-ISSUE, for a stronger reason than "nothing follows on
// that line". `ts.Point.Column` has ZERO consumers in this repository — the only
// occurrences construct it, in internal/treesitter/ts/official.go:212,216 — and
// grafel's Kotlin consumers read `StartPoint().Row` only. Rows are exact by
// construction because no newline is ever inserted, and byte offsets are mapped
// exactly. So the one field an inserted `;` could perturb is one nothing reads.

// maxKotlinRepairRounds bounds the re-parse loop. It is a ceiling, not the
// termination condition: the loop's real exit is the strict-progress check in
// repairKotlinAnnotationMisparse, which discards any round that fails to reduce
// the misparse count. Every repairable shape measured converges in ONE round.
const maxKotlinRepairRounds = 3

// repairKotlinAnnotationMisparse returns a tree in which top-level declarations
// carrying two or more annotations are `class_declaration` / `object_declaration`
// / `function_declaration` nodes again, with their annotations back in
// `modifiers`, and with every byte offset expressed in terms of `source`.
//
// It returns (tree, true) when a repair was made — the caller then owns the
// returned tree INSTEAD of `tree`, which this function has already closed. It
// returns (nil, false) when nothing needed repairing or the repair did not help,
// in which case `tree` is untouched and still owned by the caller.
func repairKotlinAnnotationMisparse(p ts.Parser, source []byte, tree ts.Tree) (ts.Tree, bool) {
	if p == nil || tree == nil {
		return nil, false
	}
	pts := kotlinMisparsePoints(tree.RootNode())
	if len(pts) == 0 {
		return nil, false
	}

	cur := source
	curTree := tree
	// insertions holds, in REPAIRED-buffer coordinates, the offset of every `;`
	// this repair has added, in ascending order.
	var insertions []int
	// remaining is the misparse count of the tree currently held in curTree.
	// Every accepted round must STRICTLY reduce it.
	remaining := len(pts)

	for round := 0; round < maxKotlinRepairRounds; round++ {
		next, added := insertTerminators(cur, pts)
		repaired, err := p.Parse(next)
		if err != nil || repaired == nil {
			// A failed re-parse is not a reason to lose the original tree.
			if curTree != tree {
				curTree.Close()
			}
			return nil, false
		}

		newPts := kotlinMisparsePoints(repaired.RootNode())
		if len(newPts) >= remaining {
			// STRICT-PROGRESS GUARD. This round did not reduce the misparse
			// count, so the terminator is not disambiguating this shape and
			// never will — more rounds only pile up semicolons and eventually
			// invent ERROR nodes (the single-line class body does exactly
			// this). Drop THIS round's work and keep whatever earlier rounds
			// legitimately achieved; if there was none, the caller gets its
			// original tree back untouched.
			repaired.Close()
			break
		}

		if curTree != tree {
			curTree.Close()
		}
		curTree = repaired
		cur = next
		insertions = mergeInsertions(insertions, added)
		remaining = len(newPts)
		pts = newPts
		if remaining == 0 {
			break
		}
	}

	if len(insertions) == 0 {
		// Nothing survived the progress guard: `curTree` is still `tree`.
		return nil, false
	}

	// NEVER-WORSE INVARIANT. Strict progress is about the misparse count; this
	// is about what actually reaches users. A repaired tree carrying more ERROR
	// nodes than the one we were handed pushes the file toward the maxErrorRatio
	// gate and toward the #6360 wrapper hiding real subtrees — strictly worse
	// than doing nothing.
	//
	// HONESTY NOTE, so this comment does not outrun its evidence: with the
	// strict-progress guard above in place, this check is DEFENCE IN DEPTH and
	// is not currently reachable. Deleting it keeps the whole suite green, and
	// that mutant is reported ALIVE rather than killed with a manufactured
	// fixture. A search for an input on which a strictly-progressing round also
	// RAISES the error count — the annotated-class repair followed by ten
	// different malformed tails — found none; the error count was identical
	// before and after in every case. It is kept because it is the property that
	// actually protects users, stated independently of how the loop happens to
	// terminate today, and because a future change to the insertion strategy
	// would reach it. TestKotlinAnnotRepair_NeverIncreasesErrorCount pins the
	// PROPERTY across every path; it does not pin this branch.
	_, wasErrs := countNodesTS(tree.RootNode())
	_, nowErrs := countNodesTS(curTree.RootNode())
	if nowErrs > wasErrs {
		curTree.Close()
		return nil, false
	}

	tree.Close()
	return &shiftedTree{inner: curTree, insertions: insertions, pin: cur}, true
}

// kotlinMisparsePoints returns the byte offsets, in the tree's own coordinates,
// at which a `;` must be inserted to disambiguate each misparsed construct.
//
// The signature is a DIRECT child of source_file that is a `prefix_expression`
// whose own first child is an `annotation`. That combination cannot occur in
// well-formed top-level Kotlin — a top-level prefix expression is not a
// declaration, and annotations are not prefix operators — so it is specific to
// the misparse rather than a heuristic over plausible code.
//
// Two damage shapes exist and are handled together:
//
//   - SWALLOWED (class, interface, fun): the prefix_expression's span covers the
//     annotations AND the declaration, so its own end is the insertion point.
//   - DETACHED (object): the prefix_expression covers only the annotations and
//     the declaration survives as the immediately following sibling, stripped of
//     its modifiers. The terminator then belongs after THAT sibling, otherwise
//     the annotations stay detached and the declaration is still unannotated.
func kotlinMisparsePoints(root ts.Node) []int {
	if root == nil || root.Type() != "source_file" {
		return nil
	}
	var out []int
	n := int(root.ChildCount())
	for i := 0; i < n; i++ {
		c := root.Child(i)
		if c == nil || c.Type() != "prefix_expression" ||
			c.ChildCount() == 0 || c.Child(0) == nil || c.Child(0).Type() != "annotation" {
			continue
		}
		end := int(c.EndByte())
		// DETACHED vs SWALLOWED is decided STRUCTURALLY, not by a byte gap:
		// swallowsDeclaration asks whether this expression absorbed a
		// declaration. If it did not, the declaration it belongs to is the
		// following sibling and the terminator goes after THAT. Deciding it by
		// proximity instead mis-fires on the swallowed shape, whose next sibling
		// is an unrelated declaration one blank line away — it would terminate
		// the wrong declaration and leave the controller misparsed.
		if !swallowsDeclaration(c) && i+1 < n {
			if nx := root.Child(i + 1); nx != nil && isKotlinDeclType(nx.Type()) &&
				int(nx.StartByte()) >= end {
				end = int(nx.EndByte())
			}
		}
		out = append(out, end)
	}
	return out
}

// swallowsDeclaration reports whether the misparsed expression n has absorbed a
// declaration, rather than merely being the declaration's stranded annotations.
//
// It is a POSITIVE test for the absorbed declaration's remains — a body
// (`lambda_literal`, `class_body`, `function_body`, …) or a nested
// `*_declaration`. The complementary "is it only annotations?" phrasing does NOT
// work: in the detached shape the grammar splits an annotation's own argument
// list off as a sibling `parenthesized_expression`, so the stranded expression
// is NOT annotation nodes end to end and a naive purity check misreads it as
// swallowed. Measured on the fwcd 0.3.8 grammar; both shapes are pinned by
// TestKotlinMisparsePoints_DetachedVsSwallowed.
func swallowsDeclaration(n ts.Node) bool {
	if n == nil {
		return false
	}
	switch t := n.Type(); {
	case t == "lambda_literal", t == "class_body", t == "function_body",
		t == "enum_class_body", t == "statements":
		return true
	case strings.HasSuffix(t, "_declaration"):
		return true
	}
	for i := 0; i < int(n.ChildCount()); i++ {
		if swallowsDeclaration(n.Child(i)) {
			return true
		}
	}
	return false
}

func isKotlinDeclType(t string) bool {
	switch t {
	case "class_declaration", "object_declaration", "function_declaration",
		"property_declaration", "type_alias":
		return true
	}
	return false
}

// insertTerminators returns src with a `;` inserted at each offset in pts, and
// the offsets those semicolons occupy in the RESULT buffer.
func insertTerminators(src []byte, pts []int) ([]byte, []int) {
	sorted := append([]int(nil), pts...)
	sort.Ints(sorted)

	out := make([]byte, 0, len(src)+len(sorted))
	added := make([]int, 0, len(sorted))
	prev := 0
	for _, p := range sorted {
		if p < prev || p > len(src) {
			continue
		}
		out = append(out, src[prev:p]...)
		added = append(added, len(out))
		out = append(out, ';')
		prev = p
	}
	out = append(out, src[prev:]...)
	return out, added
}

// mergeInsertions folds a fresh round's insertion offsets (which are in the NEW
// buffer's coordinates) into the running list (which is in the PREVIOUS
// buffer's coordinates) so the result is entirely in the new buffer's
// coordinates.
func mergeInsertions(prev, added []int) []int {
	if len(prev) == 0 {
		return added
	}
	// Each earlier insertion shifts right by the number of new insertions that
	// land at or before it.
	out := make([]int, 0, len(prev)+len(added))
	shifted := make([]int, len(prev))
	for i, q := range prev {
		shifted[i] = q + sort.SearchInts(added, q+1)
	}
	out = append(out, shifted...)
	out = append(out, added...)
	sort.Ints(out)
	return out
}

// --- offset-mapping tree view -----------------------------------------------

// shiftedTree presents a tree parsed over a repaired buffer as though it had
// been parsed over the original source: every byte offset has the inserted
// terminators subtracted back out.
type shiftedTree struct {
	inner      ts.Tree
	insertions []int // ascending, in repaired-buffer coordinates
	pin        []byte
}

func (t *shiftedTree) RootNode() ts.Node {
	return newShiftedNode(t.inner.RootNode(), t.insertions)
}

func (t *shiftedTree) Close() { t.inner.Close() }

type shiftedNode struct {
	inner      ts.Node
	insertions []int
}

func newShiftedNode(n ts.Node, ins []int) ts.Node {
	if n == nil {
		return nil
	}
	return &shiftedNode{inner: n, insertions: ins}
}

// unshift maps a repaired-buffer offset back to the original source. It
// subtracts the number of inserted terminators that lie strictly before the
// offset, which is exact for both start (inclusive) and end (exclusive) bounds:
// a node ending immediately after an insertion ends, in the original, exactly
// where that insertion was made.
func unshift(off uint32, ins []int) uint32 {
	n := sort.SearchInts(ins, int(off))
	if uint32(n) > off {
		return 0
	}
	return off - uint32(n)
}

func (s *shiftedNode) Type() string { return s.inner.Type() }

func (s *shiftedNode) Child(i int) ts.Node {
	return newShiftedNode(s.inner.Child(i), s.insertions)
}
func (s *shiftedNode) ChildCount() uint32 { return s.inner.ChildCount() }
func (s *shiftedNode) NamedChild(i int) ts.Node {
	return newShiftedNode(s.inner.NamedChild(i), s.insertions)
}
func (s *shiftedNode) NamedChildCount() uint32 { return s.inner.NamedChildCount() }
func (s *shiftedNode) ChildByFieldName(f string) ts.Node {
	return newShiftedNode(s.inner.ChildByFieldName(f), s.insertions)
}
func (s *shiftedNode) FieldNameForChild(i int) string { return s.inner.FieldNameForChild(i) }
func (s *shiftedNode) Parent() ts.Node {
	return newShiftedNode(s.inner.Parent(), s.insertions)
}
func (s *shiftedNode) PrevSibling() ts.Node {
	return newShiftedNode(s.inner.PrevSibling(), s.insertions)
}
func (s *shiftedNode) StartByte() uint32 { return unshift(s.inner.StartByte(), s.insertions) }
func (s *shiftedNode) EndByte() uint32   { return unshift(s.inner.EndByte(), s.insertions) }

// StartPoint / EndPoint pass through. No newline is ever inserted, so rows are
// exact; columns are exact for every node that does not share a line with an
// inserted terminator, which is every node in a file whose declarations end at
// end-of-line.
func (s *shiftedNode) StartPoint() ts.Point { return s.inner.StartPoint() }
func (s *shiftedNode) EndPoint() ts.Point   { return s.inner.EndPoint() }

func (s *shiftedNode) IsNamed() bool  { return s.inner.IsNamed() }
func (s *shiftedNode) IsError() bool  { return s.inner.IsError() }
func (s *shiftedNode) String() string { return s.inner.String() }
