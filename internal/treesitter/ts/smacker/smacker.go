// Package smacker implements the ts abstraction over the (unmaintained)
// github.com/smacker/go-tree-sitter binding. This is the no-behavior-change
// adapter: every grammar that has NOT yet been migrated to the official binding
// keeps running through here, so the migration can proceed one language at a
// time (ADR 0023, B2 Phase 0, #5418).
package smacker

import (
	"context"
	"errors"

	sitter "github.com/smacker/go-tree-sitter"

	"github.com/cajasmota/grafel/internal/treesitter/ts"
)

// errWrongAdapter is returned when a Language from a different adapter is handed
// to this adapter's NewParser.
var errWrongAdapter = errors.New("treesitter/ts/smacker: language was not produced by the smacker adapter")

// adapterName is stamped on every Language this adapter produces, so the ABI
// guard can detect a Language being handed to the wrong adapter.
const adapterName = "smacker"

// Adapter implements ts.Adapter over smacker/go-tree-sitter.
type Adapter struct{}

// New returns the smacker adapter.
func New() *Adapter { return &Adapter{} }

// Name implements ts.Adapter.
func (a *Adapter) Name() string { return adapterName }

// Language wraps a *sitter.Language acquired from a smacker grammar's
// GetLanguage(). Construct it via WrapLanguage so the factory can register the
// 28 bundled grammars without importing this package's internals.
type Language struct {
	lang *sitter.Language
}

// WrapLanguage adapts a smacker *sitter.Language into a ts.Language.
func WrapLanguage(lang *sitter.Language) ts.Language { return Language{lang: lang} }

// Adapter implements ts.Language.
func (Language) Adapter() string { return adapterName }

// NewParser implements ts.Adapter.
func (a *Adapter) NewParser(lang ts.Language) (ts.Parser, error) {
	sl, ok := lang.(Language)
	if !ok {
		return nil, errWrongAdapter
	}
	p := sitter.NewParser()
	p.SetLanguage(sl.lang)
	return &Parser{p: p}, nil
}

// Parser implements ts.Parser over a *sitter.Parser.
type Parser struct {
	p *sitter.Parser
}

// Parse implements ts.Parser.
func (p *Parser) Parse(source []byte) (ts.Tree, error) {
	tree, err := p.p.ParseCtx(context.Background(), nil, source)
	if err != nil {
		return nil, err
	}
	if tree == nil {
		return nil, nil
	}
	return &Tree{t: tree}, nil
}

// Close implements ts.Parser.
func (p *Parser) Close() { p.p.Close() }

// Tree implements ts.Tree over a *sitter.Tree.
type Tree struct {
	t *sitter.Tree
}

// WrapTree adapts an existing smacker *sitter.Tree into a ts.Tree. Used by the
// factory's ParseResult so the pipeline can keep producing smacker trees while
// extractors consume the abstraction.
func WrapTree(t *sitter.Tree) ts.Tree {
	if t == nil {
		return nil
	}
	return &Tree{t: t}
}

// RootNode implements ts.Tree.
func (t *Tree) RootNode() ts.Node { return wrapNode(t.t.RootNode()) }

// Close implements ts.Tree.
func (t *Tree) Close() { t.t.Close() }

// node wraps a *sitter.Node. It is a value type so traversal does not allocate
// on the heap when the result is consumed immediately.
type node struct {
	n *sitter.Node
}

// wrapNode returns an untyped-nil ts.Node when n is nil (see ts package nil
// semantics), otherwise a node wrapper.
func wrapNode(n *sitter.Node) ts.Node {
	if n == nil {
		return nil
	}
	return node{n: n}
}

func (w node) Type() string                      { return w.n.Type() }
func (w node) Child(i int) ts.Node               { return wrapNode(w.n.Child(i)) }
func (w node) ChildCount() uint32                { return w.n.ChildCount() }
func (w node) NamedChild(i int) ts.Node          { return wrapNode(w.n.NamedChild(i)) }
func (w node) NamedChildCount() uint32           { return w.n.NamedChildCount() }
func (w node) ChildByFieldName(f string) ts.Node { return wrapNode(w.n.ChildByFieldName(f)) }
func (w node) Parent() ts.Node                   { return wrapNode(w.n.Parent()) }
func (w node) PrevSibling() ts.Node              { return wrapNode(w.n.PrevSibling()) }
func (w node) StartByte() uint32                 { return w.n.StartByte() }
func (w node) EndByte() uint32                   { return w.n.EndByte() }
func (w node) StartPoint() ts.Point {
	p := w.n.StartPoint()
	return ts.Point{Row: p.Row, Column: p.Column}
}
func (w node) EndPoint() ts.Point { p := w.n.EndPoint(); return ts.Point{Row: p.Row, Column: p.Column} }
func (w node) IsNamed() bool      { return w.n.IsNamed() }
func (w node) IsError() bool      { return w.n.IsError() }
func (w node) String() string     { return w.n.String() }
