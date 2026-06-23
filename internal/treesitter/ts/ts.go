// Package ts defines grafel's binding-agnostic tree-sitter abstraction.
//
// grafel historically parsed every grammar through the single, now-unmaintained
// dependency github.com/smacker/go-tree-sitter. B2 (#5418, ADR 0023) migrates,
// one grammar at a time, to the official github.com/tree-sitter/go-tree-sitter
// runtime plus per-language grammar modules. This package is the façade that
// makes that incremental migration possible: extractors traverse the interfaces
// defined here instead of a concrete vendor type, so an individual language can
// flip from the smacker adapter (see ./smacker) to the official adapter (see
// ./official) without touching the extractor that consumes its tree.
//
// Surface scope. The interface is deliberately minimal — it mirrors only the
// CST methods grafel actually calls (verified by grepping the extractor layer:
// Type/Kind, Child, ChildByFieldName, NamedChild, ChildCount, NamedChildCount,
// StartByte/EndByte, StartPoint/EndPoint, Parent, PrevSibling, IsNamed,
// IsError, String).
// grafel does not use the native query engine (Query/QueryCursor) anywhere, nor
// tree cursors, so those are intentionally absent. Text extraction is done by
// the callers via byte-slicing on StartByte/EndByte, so no Content/Utf8Text
// method is needed.
//
// Integer widths. StartByte/EndByte/ChildCount/NamedChildCount are typed uint32
// to match the existing extractor call sites (which cast via int(...)). The
// official binding returns uint; the official adapter narrows. This keeps the
// migration mechanical and compiler-checked at the adapter boundary rather than
// rippling an int-width change across every traversal site.
//
// Nil semantics. A missing node (e.g. ChildByFieldName with no match, Parent of
// the root, an out-of-range Child index) is returned as an untyped-nil Node
// interface value — NOT a non-nil interface wrapping a nil pointer. Callers rely
// on `if node == nil` throughout, so adapters MUST return a bare nil interface
// in those cases. The helper wrap* functions in each adapter enforce this.
package ts

// Point is a (row, column) source position. Rows and columns are 0-based, as in
// both tree-sitter bindings; callers add 1 for human-facing 1-based line numbers.
type Point struct {
	Row    uint32
	Column uint32
}

// Node is a binding-agnostic view of a concrete-syntax-tree node. Implementations
// wrap either a *smacker/go-tree-sitter Node or an official
// *tree-sitter/go-tree-sitter Node. See the package doc for nil semantics.
type Node interface {
	// Type returns the node's grammar symbol name (e.g. "function_declaration").
	// This maps to smacker's Node.Type() and the official binding's Node.Kind();
	// grafel keeps the historical name "Type" at the façade.
	Type() string

	// Child returns the i-th child (named or anonymous), or nil if out of range.
	Child(i int) Node
	// ChildCount returns the total number of children (named + anonymous).
	ChildCount() uint32
	// NamedChild returns the i-th named child, or nil if out of range.
	NamedChild(i int) Node
	// NamedChildCount returns the number of named children.
	NamedChildCount() uint32
	// ChildByFieldName returns the child bound to the given grammar field, or nil.
	ChildByFieldName(field string) Node
	// FieldNameForChild returns the grammar field name of the child at index i,
	// or "" if that child is unnamed or i is out of range. Maps to both bindings'
	// Node.FieldNameForChild() (smacker takes int, the official binding uint32;
	// the adapters reconcile the width).
	FieldNameForChild(i int) string
	// Parent returns the parent node, or nil for the root.
	Parent() Node
	// PrevSibling returns the node's previous sibling (named or anonymous), or
	// nil when the node is the first child of its parent. Maps to both bindings'
	// Node.PrevSibling().
	PrevSibling() Node

	// StartByte / EndByte are byte offsets into the source buffer.
	StartByte() uint32
	EndByte() uint32
	// StartPoint / EndPoint are (row, column) positions. These map to the official
	// binding's StartPosition()/EndPosition().
	StartPoint() Point
	EndPoint() Point

	// IsNamed reports whether the node corresponds to a named rule.
	IsNamed() bool
	// IsError reports whether the node is a syntax ERROR node.
	IsError() bool

	// String returns the S-expression for the node (debug only).
	String() string
}

// SameNode reports whether a and b refer to the same node in the same tree.
//
// It MUST NOT be expressed as `a == b` on the interface values: the official
// binding's adapter wraps each *tree-sitter Node in a fresh wrapper struct on
// every accessor call (Child, ChildByFieldName, NamedChild, …), so two handles
// to the same underlying node are not `==`-equal. (The retired smacker binding
// happened to return comparable values, which is why several extractors used
// `==`; the cutover to the official binding broke that assumption — B2 #5418.)
//
// Identity is established by node Type plus byte span: within a single parse
// tree no two distinct nodes share both their start/end byte offsets and their
// grammar symbol, so (Type, StartByte, EndByte) is a stable identity key.
// nil on either side reports false unless both are nil.
func SameNode(a, b Node) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return a.StartByte() == b.StartByte() &&
		a.EndByte() == b.EndByte() &&
		a.Type() == b.Type()
}

// Tree is a binding-agnostic parse tree. It owns C memory; callers that obtain a
// Tree directly (rather than via a pooled ParseResult) should Close it.
type Tree interface {
	// RootNode returns the root of the syntax tree.
	RootNode() Node
	// Close releases the underlying C resources. Safe to call once; idempotent
	// implementations are not required, so callers should Close exactly once.
	Close()
}

// Language is an opaque, binding-specific grammar handle. It is produced by an
// adapter's grammar accessor and consumed only by that same adapter's Parser.
// Mixing a Language from one adapter with a Parser from another is a programming
// error (and, given the ABI hazard in ADR 0023 §6, potentially a crash).
type Language interface {
	// Adapter identifies the binding that produced this Language ("smacker" or
	// "official"), so the registry and ABI guard can detect cross-adapter misuse.
	Adapter() string
}

// Parser is a binding-agnostic parser. Implementations are NOT safe for
// concurrent use; the factory serialises parses (see parser.go, issue #481).
type Parser interface {
	// Parse parses source under the previously-set language and returns a Tree.
	// Returns nil if the binding produced no tree.
	Parse(source []byte) (Tree, error)
	// Close releases the parser's C resources.
	Close()
}

// Adapter is a binding implementation. The factory holds one Adapter per binding
// (smacker, official) and routes each language to its adapter via the registry.
type Adapter interface {
	// Name identifies the binding ("smacker" or "official"). Must match the value
	// returned by the Languages this adapter produces (used by the ABI guard).
	Name() string
	// NewParser constructs a parser bound to lang. lang MUST have been produced by
	// this same adapter.
	NewParser(lang Language) (Parser, error)
}
