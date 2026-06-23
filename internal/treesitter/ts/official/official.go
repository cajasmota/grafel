// Package official implements the ts abstraction over the maintained
// github.com/tree-sitter/go-tree-sitter binding (the migration target of B2,
// ADR 0023, #5418). In Phase 0 only the Go grammar is wired through here; every
// other grammar stays on the smacker adapter. Adding a language is a one-liner
// in the factory's registry (a grammar module's Language() pointer wrapped by
// WrapLanguage) — no extractor change, because extractors consume the ts façade.
//
// ABI hazard. The runtime supports a RANGE of grammar LANGUAGE_VERSIONs; pairing
// a too-new grammar with this runtime compiles but SIGSEGVs at RootNode (ADR
// 0023 §6: runtime v0.24.0 + tree-sitter-go v0.25.0 crashed; v0.23.4 works).
// SetLanguage returns an error for a detectable ABI mismatch, which NewParser
// surfaces; the factory's smoke-parse guard (parser.go) catches the rest before
// production by parsing trivial source and asserting a sane, non-error root.
package official

import (
	"errors"

	tsofficial "github.com/tree-sitter/go-tree-sitter"

	"github.com/cajasmota/grafel/internal/treesitter/ts"
)

// adapterName is stamped on every Language this adapter produces.
const adapterName = "official"

var errWrongAdapter = errors.New("treesitter/ts/official: language was not produced by the official adapter")

// Adapter implements ts.Adapter over tree-sitter/go-tree-sitter.
type Adapter struct{}

// New returns the official adapter.
func New() *Adapter { return &Adapter{} }

// Name implements ts.Adapter.
func (a *Adapter) Name() string { return adapterName }

// Language wraps a *tsofficial.Language. Construct it via WrapLanguage, passing
// the unsafe.Pointer from a grammar module's Language() accessor wrapped by the
// runtime's NewLanguage.
type Language struct {
	lang *tsofficial.Language
}

// WrapLanguage adapts an official *tsofficial.Language into a ts.Language.
// Callers build the argument as tsofficial.NewLanguage(tsgo.Language()).
func WrapLanguage(lang *tsofficial.Language) ts.Language { return Language{lang: lang} }

// Adapter implements ts.Language.
func (Language) Adapter() string { return adapterName }

// NewParser implements ts.Adapter. It returns an error if lang came from a
// different adapter, or if SetLanguage rejects the grammar (a detectable ABI
// mismatch).
func (a *Adapter) NewParser(lang ts.Language) (ts.Parser, error) {
	ol, ok := lang.(Language)
	if !ok {
		return nil, errWrongAdapter
	}
	p := tsofficial.NewParser()
	if err := p.SetLanguage(ol.lang); err != nil {
		p.Close()
		return nil, err
	}
	return &Parser{p: p}, nil
}

// Parser implements ts.Parser over a *tsofficial.Parser.
type Parser struct {
	p *tsofficial.Parser
}

// Parse implements ts.Parser. The official binding does not return an error;
// a nil tree maps to (nil, nil).
func (p *Parser) Parse(source []byte) (ts.Tree, error) {
	tree := p.p.Parse(source, nil)
	if tree == nil {
		return nil, nil
	}
	return &Tree{t: tree}, nil
}

// Close implements ts.Parser.
func (p *Parser) Close() { p.p.Close() }

// Tree implements ts.Tree over a *tsofficial.Tree.
type Tree struct {
	t *tsofficial.Tree
}

// RootNode implements ts.Tree.
func (t *Tree) RootNode() ts.Node { return wrapNode(t.t.RootNode()) }

// Close implements ts.Tree.
func (t *Tree) Close() { t.t.Close() }

// node wraps a *tsofficial.Node. Value type — traversal does not heap-allocate.
type node struct {
	n *tsofficial.Node
}

// wrapNode returns an untyped-nil ts.Node when n is nil (see ts package nil
// semantics), otherwise a node wrapper.
func wrapNode(n *tsofficial.Node) ts.Node {
	if n == nil {
		return nil
	}
	return node{n: n}
}

// Type maps the façade's historical Type() onto the official Kind().
func (w node) Type() string { return w.n.Kind() }

// Child/NamedChild take a uint in the official API; the façade keeps int indices
// matching the extractor call sites.
func (w node) Child(i int) ts.Node      { return wrapNode(w.n.Child(uint(i))) }
func (w node) NamedChild(i int) ts.Node { return wrapNode(w.n.NamedChild(uint(i))) }

// ChildCount/NamedChildCount/StartByte/EndByte are uint in the official API;
// narrowed to uint32 at the boundary to match the façade and the extractor casts.
func (w node) ChildCount() uint32      { return uint32(w.n.ChildCount()) }
func (w node) NamedChildCount() uint32 { return uint32(w.n.NamedChildCount()) }
func (w node) StartByte() uint32       { return uint32(w.n.StartByte()) }
func (w node) EndByte() uint32         { return uint32(w.n.EndByte()) }

func (w node) ChildByFieldName(f string) ts.Node { return wrapNode(w.n.ChildByFieldName(f)) }
func (w node) Parent() ts.Node                   { return wrapNode(w.n.Parent()) }
func (w node) PrevSibling() ts.Node              { return wrapNode(w.n.PrevSibling()) }

// StartPoint/EndPoint map the official StartPosition()/EndPosition() and narrow
// the uint Row/Column to uint32 to match ts.Point.
func (w node) StartPoint() ts.Point {
	p := w.n.StartPosition()
	return ts.Point{Row: uint32(p.Row), Column: uint32(p.Column)}
}
func (w node) EndPoint() ts.Point {
	p := w.n.EndPosition()
	return ts.Point{Row: uint32(p.Row), Column: uint32(p.Column)}
}

func (w node) IsNamed() bool  { return w.n.IsNamed() }
func (w node) IsError() bool  { return w.n.IsError() }
func (w node) String() string { return w.n.ToSexp() }
