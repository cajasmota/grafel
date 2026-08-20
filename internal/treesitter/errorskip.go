package treesitter

import "github.com/cajasmota/grafel/internal/treesitter/ts"

// Error-subtree skipping (#6360, direction 1).
//
// maxErrorRatio is a whole-file average, so it conflates two very different
// facts: "this file is not the language I think it is" (proto2 parsed as
// proto3 collapses to ratio 0.20-0.28 and is correctly rejected whole) and
// "this file has one typo" (a 12-message proto3 file with one malformed
// message parses at ratio 0.0033, sails under the gate, and is accepted IN
// FULL — including the part tree-sitter could not read).
//
// The second case is the dangerous one: the broken region survives as an ERROR
// node whose children are whatever tokens the parser managed to salvage, and
// every extractor traversal walks straight into it. Whatever an extractor
// builds from those salvaged tokens is not a reading of the source; it is a
// reading of tree-sitter's error recovery.
//
// This file makes the ERROR node and everything below it invisible to
// extractors, so the emitted entity set is a true SUBSET of reality rather than
// a wrong description of it. It deliberately does NOT mark the surviving
// entities as degraded (direction 2 of the issue) — that is a separate decision
// about what consumers see.
//
// Blast radius. The gate is shared by every tree-sitter grammar, so this
// wrapper is on the common path for all of them. It is therefore applied ONLY
// when the tree actually contains at least one ERROR node (errNodes > 0 in
// ParserFactory.Parse). A clean parse — the overwhelming majority — is handed
// back completely unwrapped: same tree, same nodes, zero allocation, zero
// behaviour change.
//
// Known limit: MISSING nodes are NOT hidden. tree-sitter has two recovery
// shapes — an ERROR node wrapping tokens it could not fit, and a zero-width
// MISSING node inserted where the grammar required a token the source did not
// supply. Only the first is filtered here, because IsMissing is not part of the
// ts.Node façade (see internal/treesitter/ts/ts.go — the interface mirrors only
// the methods grafel actually calls). So a file whose sole defect is a dropped
// terminator still parses at error_ratio 0.0 and is extracted in full: #6360's
// third fixture variant, the missing `;`, is exactly this case, and the
// protobuf grammar happens to recover the field correctly there. This is a
// known limit rather than a regression — the pre-#6360 behaviour for MISSING is
// unchanged — but it means "no ERROR reachable" is not the same claim as "the
// tree is a faithful reading of the source".
//
// Cost. Each wrap of a node scans that node's direct children once, so a full
// traversal of a wrapped tree costs O(n) extra work in total. Wrappers are
// immutable once constructed (children are resolved eagerly in newFilteredNode
// and never mutated), so they are safe to hand to a concurrent traversal.

// newErrorSkippingTree wraps t so no ERROR node, and nothing below one, is
// reachable through the ts.Node accessors. Closing the wrapper closes t.
func newErrorSkippingTree(t ts.Tree) ts.Tree {
	if t == nil {
		return nil
	}
	return &filteredTree{inner: t}
}

type filteredTree struct{ inner ts.Tree }

func (t *filteredTree) RootNode() ts.Node {
	// The root itself is never hidden. If the ROOT is an ERROR the file is
	// garbage end to end, which is the whole-file gate's job, not this one —
	// hiding the root would hand callers a nil tree they do not expect.
	return newFilteredNode(t.inner.RootNode())
}

func (t *filteredTree) Close() { t.inner.Close() }

// filteredNode is an immutable view of a node whose ERROR children (and, by
// construction, their whole subtrees) have been removed from every child list.
type filteredNode struct {
	inner ts.Node

	// kids / kidIdx are the non-ERROR children in order, paired with their
	// index in the UNDERLYING child list so FieldNameForChild can be answered
	// against the real tree.
	kids   []ts.Node
	kidIdx []int

	// named is the non-ERROR subset of the underlying named children.
	named []ts.Node
}

func newFilteredNode(n ts.Node) ts.Node {
	if n == nil {
		return nil
	}
	f := &filteredNode{inner: n}
	for i := range int(n.ChildCount()) {
		c := n.Child(i)
		if c == nil || c.IsError() {
			continue
		}
		f.kids = append(f.kids, c)
		f.kidIdx = append(f.kidIdx, i)
	}
	for i := range int(n.NamedChildCount()) {
		c := n.NamedChild(i)
		if c == nil || c.IsError() {
			continue
		}
		f.named = append(f.named, c)
	}
	return f
}

func (f *filteredNode) Type() string { return f.inner.Type() }

func (f *filteredNode) Child(i int) ts.Node {
	if i < 0 || i >= len(f.kids) {
		return nil
	}
	return newFilteredNode(f.kids[i])
}

func (f *filteredNode) ChildCount() uint32 { return uint32(len(f.kids)) }

func (f *filteredNode) NamedChild(i int) ts.Node {
	if i < 0 || i >= len(f.named) {
		return nil
	}
	return newFilteredNode(f.named[i])
}

func (f *filteredNode) NamedChildCount() uint32 { return uint32(len(f.named)) }

func (f *filteredNode) ChildByFieldName(field string) ts.Node {
	c := f.inner.ChildByFieldName(field)
	if c == nil || c.IsError() {
		return nil
	}
	return newFilteredNode(c)
}

// FieldNameForChild takes an index into the FILTERED child list and answers it
// against the underlying tree. Answering the raw index instead would silently
// mis-pair field names with children on any node that lost a child.
func (f *filteredNode) FieldNameForChild(i int) string {
	if i < 0 || i >= len(f.kidIdx) {
		return ""
	}
	return f.inner.FieldNameForChild(f.kidIdx[i])
}

func (f *filteredNode) Parent() ts.Node {
	p := f.inner.Parent()
	// A visible node cannot sit under an ERROR, so this is defensive: if it
	// somehow does, report no parent rather than leak a hidden node upward.
	if p == nil || p.IsError() {
		return nil
	}
	return newFilteredNode(p)
}

// PrevSibling skips over hidden siblings so a caller stepping backwards never
// lands inside the unreadable region.
func (f *filteredNode) PrevSibling() ts.Node {
	for s := f.inner.PrevSibling(); s != nil; s = s.PrevSibling() {
		if !s.IsError() {
			return newFilteredNode(s)
		}
	}
	return nil
}

func (f *filteredNode) StartByte() uint32    { return f.inner.StartByte() }
func (f *filteredNode) EndByte() uint32      { return f.inner.EndByte() }
func (f *filteredNode) StartPoint() ts.Point { return f.inner.StartPoint() }
func (f *filteredNode) EndPoint() ts.Point   { return f.inner.EndPoint() }
func (f *filteredNode) IsNamed() bool        { return f.inner.IsNamed() }
func (f *filteredNode) IsError() bool        { return f.inner.IsError() }
func (f *filteredNode) String() string       { return f.inner.String() }
