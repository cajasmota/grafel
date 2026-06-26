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
	"fmt"
	"os"
	"time"

	tsofficial "github.com/tree-sitter/go-tree-sitter"

	"github.com/cajasmota/grafel/internal/treesitter/ts"
)

// adapterName is stamped on every Language this adapter produces.
const adapterName = "official"

// defaultParseTimeout bounds a single tree-sitter parse. Override per-process
// with GRAFEL_PARSE_TIMEOUT (a Go duration, e.g. "20s", "500ms"); "0" disables
// the watchdog entirely. A normal parse finishes in well under a second, so the
// deadline only ever fires on a pathological/runaway parse.
const defaultParseTimeout = 20 * time.Second

// parseTimeoutEnv is the env var that tunes the per-parse watchdog deadline.
const parseTimeoutEnv = "GRAFEL_PARSE_TIMEOUT"

// ErrParseDeadlineExceeded is returned by Parse when the per-parse watchdog
// halts a runaway tree-sitter parse before it completes (#5473). It is a
// distinct sentinel so callers (and tests) can tell a watchdog kill from a
// genuine empty/failed parse, and so the daemon logs a bounded error instead of
// freezing. See parseTimeout / Parse for the mechanism.
var ErrParseDeadlineExceeded = errors.New("treesitter/official: parse exceeded watchdog deadline")

var errWrongAdapter = errors.New("treesitter/ts/official: language was not produced by the official adapter")

// parseTimeout resolves the per-parse watchdog deadline from the environment,
// falling back to defaultParseTimeout. A non-negative value is honoured (0 ==
// disabled); a malformed or negative value falls back to the default rather
// than silently disabling the safety net.
func parseTimeout() time.Duration {
	v := os.Getenv(parseTimeoutEnv)
	if v == "" {
		return defaultParseTimeout
	}
	d, err := time.ParseDuration(v)
	if err != nil || d < 0 {
		return defaultParseTimeout
	}
	return d
}

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

// AbiVersion reports the grammar's tree-sitter ABI (its parser.c
// LANGUAGE_VERSION). The ABI-15 rollout (#5473 Phase 2) asserts this is 15 for
// every registered grammar: a single grammar left at ABI 14 is exactly the
// mixed-ABI pairing that produced the unbounded parse-error-recovery loop, so
// the invariant is "all grammars on the same ABI as the v0.25 runtime, no
// laggards".
func (l Language) AbiVersion() int { return int(l.lang.AbiVersion()) }

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

// Parse implements ts.Parser. It does a fresh parse (no oldTree reuse) bounded
// by a per-parse wall-clock watchdog so a single pathological input can never
// pin the daemon indefinitely (#5473).
//
// Why this matters. A runtime build was observed sitting ~26 min in one runaway
// C parse (ts_parser_parse / ts_node_child hot). Because the factory holds a
// process-wide parse mutex (issue #481) AND a parse slot across this call, that
// one hung parse froze ALL in-process parsing and the daemon went silent. The
// bare p.Parse(source, nil) is unbounded.
//
// Mechanism (v0.24-native). go-tree-sitter v0.24 exposes the C wall-clock
// deadline ts_parser_set_timeout_micros: tree-sitter checks the elapsed budget
// periodically from its main parse loop and, once exceeded, halts early and
// returns a nil tree. That is exactly the cancellation path the v0.25 progress
// callback would provide, without the goroutine/cancellation-flag plumbing. On
// a halt we return ErrParseDeadlineExceeded so the factory converts the freeze
// into a bounded, logged per-file failure and releases parseMu + the slot. A
// genuine empty parse (no halt) still maps to (nil, nil). The parser is
// single-use here (the factory closes it after this call), so no Reset of the
// halted state is needed.
//
// Caveat: a true tight C loop that never reaches a deadline-check site would
// still need an upstream fix; the observed runaway was in the main loop, which
// does check.
func (p *Parser) Parse(source []byte) (ts.Tree, error) {
	timeout := parseTimeout()
	// SetTimeoutMicros(0) is tree-sitter's "no deadline" default; a positive
	// budget arms the watchdog.
	p.p.SetTimeoutMicros(uint64(timeout.Microseconds()))

	start := time.Now()
	tree := p.p.Parse(source, nil)
	if tree == nil {
		// With a language set, the v0.24 binding returns a nil tree only when
		// the parse halted early on the wall-clock deadline (a genuine parse —
		// including of empty input — yields a real, possibly all-ERROR tree, not
		// nil). So a nil tree under an armed watchdog IS the deadline firing; we
		// report it directly rather than re-deriving it from elapsed wall-clock,
		// which races the C clock and is flaky under load. When the watchdog is
		// disabled (timeout == 0) there is no deadline to attribute a nil tree
		// to, so it maps to (nil, nil) as the bare binding did.
		if timeout > 0 {
			return nil, fmt.Errorf("%w of %s after %s (%s; possible pathological/runaway parse, #5473)",
				ErrParseDeadlineExceeded, timeout, time.Since(start).Round(time.Millisecond), parseTimeoutEnv)
		}
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
func (w node) FieldNameForChild(i int) string    { return w.n.FieldNameForChild(uint32(i)) }
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
