// exception_flow.go — supplemental pass that emits THROWS / CATCHES edges from
// Ruby methods (def / singleton def) to a shared SCOPE.ExceptionType node (epic
// #3628). It lets the graph answer "what can this method raise?" (outbound
// THROWS) and "where is NotFoundError handled?" (inbound CATCHES), matching the
// flagship convergence-node shape every other language emits (see
// internal/extractor/exception_flow.go): node/edge construction lives in
// extractor.EmitExceptionEdges, so a Ruby `raise NotFoundError` and an Elixir
// `raise NotFoundError` resolve to ONE exception:NotFoundError node.
//
// Detected shapes (typed only — honest-partial, precision-first):
//
//	raise NotFoundError, "msg"                  → THROWS NotFoundError
//	raise ArgumentError                         → THROWS ArgumentError
//	raise MyApp::NotFoundError, message: "x"    → THROWS NotFoundError (last segment)
//	begin … rescue NotFoundError => e … end     → CATCHES NotFoundError
//	rescue ArgumentError, TypeError => e         → CATCHES ArgumentError + TypeError
//	def m … rescue RuntimeError => e … end       → CATCHES RuntimeError (method-level rescue)
//	rescue_from RecordNotFound, with: :handler  → CATCHES RecordNotFound (Rails controller)
//	rescue_from ArgumentError, RangeError, …    → CATCHES ArgumentError + RangeError
//
// Intentionally DROPPED (would mislead error-contract analysis):
//
//	raise "just a message"      (string → implicit RuntimeError, no type token)
//	raise                       (bare re-raise — re-raises $! , carries no NEW type)
//	rescue => e  /  rescue       (untyped catch-all → StandardError, no class token —
//	                             matches the flagship catch-all convention: skip)
//	rescue_from with no leading constant (only a `with:` pair) — no type token
//
// All node/edge construction (convergence on one node per type name via a
// synthetic SourceFile) lives in extractor.EmitExceptionEdges, so the
// convergence invariant is identical across every language.

package ruby

import (
	"github.com/cajasmota/grafel/internal/treesitter/ts"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
)

// emitExceptionFlowEdges scans every method / singleton_method body for typed
// raise / rescue / rescue_from shapes and appends exception-type entities +
// THROWS / CATCHES edges.
//
// entities[0] MUST be the file entity (Extract appends it first). Mutates
// *entities in place. Safe with nil / empty input. raise / rescue / rescue_from
// outside any method (class or module scope, e.g. a controller-level
// `rescue_from`) attach to the file entity via EmitExceptionEdges's fallback.
func emitExceptionFlowEdges(root ts.Node, src []byte, entities *[]types.EntityRecord) {
	if root == nil || entities == nil || len(*entities) == 0 {
		return
	}

	var edges []extractor.ExceptionEdge

	// Track the nearest enclosing method/singleton_method name so each raise /
	// rescue / rescue_from is attributed to the right SCOPE.Operation (matched by
	// Name inside EmitExceptionEdges). The method NAME mirrors operation naming in
	// ruby.go (buildMethod uses the bare `name` field), so attribution is exact.
	var walk func(n ts.Node, current string)
	walk = func(n ts.Node, current string) {
		if n == nil {
			return
		}
		switch n.Type() {
		case "method", "singleton_method":
			fnName := childFieldText(n, "name", src)
			for i := 0; i < int(n.ChildCount()); i++ {
				walk(n.Child(i), fnName)
			}
			return
		case "call", "command":
			head := rubyCallTarget(n, src)
			switch head {
			case "raise":
				if t := rubyRaiseType(n, src); t != "" {
					edges = append(edges, extractor.ExceptionEdge{
						Type: t, FromName: current, Pattern: "raise",
					})
				}
			case "rescue_from":
				for _, t := range rubyRescueFromTypes(n, src) {
					edges = append(edges, extractor.ExceptionEdge{
						Type: t, FromName: current, Catch: true, Pattern: "rescue_from",
					})
				}
			}
		case "rescue":
			for _, t := range rubyRescueTypes(n, src) {
				edges = append(edges, extractor.ExceptionEdge{
					Type: t, FromName: current, Catch: true, Pattern: "rescue",
				})
			}
		}
		for i := 0; i < int(n.ChildCount()); i++ {
			walk(n.Child(i), current)
		}
	}
	walk(root, "")

	extractor.EmitExceptionEdges(entities, "ruby", edges)
}

// rubyRaiseType returns the raised exception-class token from a `raise` call /
// command node, or "" for a message-only / bare / variable raise.
//
// Accepted shape: the first argument is a `constant` (or scoped constant, which
// tree-sitter exposes as a `scope_resolution` whose verbatim text — e.g.
// `MyApp::NotFoundError` — NormalizeExceptionType reduces to the last segment).
// `raise NotFoundError, "x"`, `raise ArgumentError`, `raise MyApp::Err, message:`.
//
// Returns "" for `raise "msg"` (string → implicit RuntimeError), bare `raise`
// (re-raise — which the grammar exposes as a lone `identifier`, never a call
// with arguments, so it never reaches here), and `raise var` / computed types.
func rubyRaiseType(raiseCall ts.Node, src []byte) string {
	args := raiseCall.ChildByFieldName("arguments")
	if args == nil {
		return "" // bare `raise` re-raise (no arguments) — no static type
	}
	first := firstNamedChildRuby(args)
	if first == nil {
		return ""
	}
	switch first.Type() {
	case "constant", "scope_resolution":
		// `raise NotFoundError` / `raise MyApp::NotFoundError`. The verbatim text
		// (possibly `::`-qualified) is normalized to the last segment downstream.
		return string(src[first.StartByte():first.EndByte()])
	}
	// `raise "msg"` (string), `raise var` (identifier), computed — no static type.
	return ""
}

// rubyRescueTypes returns the caught exception-class tokens from a `rescue`
// node. The grammar exposes the rescued type list as the `exceptions` field (an
// `exceptions` node holding one `constant`/`scope_resolution` per class):
//
//	rescue NotFoundError => e           exceptions{constant} → {NotFoundError}
//	rescue ArgumentError, TypeError => e exceptions{constant,constant} → {ArgumentError,TypeError}
//	rescue MyApp::BadError               exceptions{scope_resolution} → {BadError}
//	rescue => e  /  rescue               (no `exceptions` field — catch-all) → {}
//
// Untyped catch-alls (bare `rescue`) carry no static class → dropped, matching
// the flagship catch-all convention (precision over recall).
func rubyRescueTypes(rescueNode ts.Node, src []byte) []string {
	exc := rescueNode.ChildByFieldName("exceptions")
	if exc == nil {
		return nil // bare `rescue => e` / `rescue` — catch-all, no type
	}
	var out []string
	for i := 0; i < int(exc.NamedChildCount()); i++ {
		el := exc.NamedChild(i)
		if el == nil {
			continue
		}
		switch el.Type() {
		case "constant", "scope_resolution":
			out = append(out, string(src[el.StartByte():el.EndByte()]))
		}
		// `splat` / `assignment` / non-constant rescued expressions: no static type.
	}
	return out
}

// rubyRescueFromTypes returns the caught exception-class tokens from a Rails
// controller `rescue_from` call / command node. The leading positional
// arguments are the rescued classes; a trailing `with:`/handler `pair` (and any
// block) is ignored:
//
//	rescue_from RecordNotFound, with: :not_found        → {RecordNotFound}
//	rescue_from ArgumentError, RangeError, with: :bad   → {ArgumentError, RangeError}
//	rescue_from SomeError do |e| … end                  → {SomeError}
//
// Only `constant` / `scope_resolution` arguments yield a type; `pair`
// (keyword args like `with:`) and anything else are skipped (honest-partial).
func rubyRescueFromTypes(call ts.Node, src []byte) []string {
	args := call.ChildByFieldName("arguments")
	if args == nil {
		return nil
	}
	var out []string
	for i := 0; i < int(args.NamedChildCount()); i++ {
		el := args.NamedChild(i)
		if el == nil {
			continue
		}
		switch el.Type() {
		case "constant", "scope_resolution":
			out = append(out, string(src[el.StartByte():el.EndByte()]))
		}
		// `pair` (with: :handler), `simple_symbol`, blocks: not a rescued type.
	}
	return out
}

// firstNamedChildRuby returns the first named child of n, or nil.
func firstNamedChildRuby(n ts.Node) ts.Node {
	if n == nil || n.NamedChildCount() == 0 {
		return nil
	}
	return n.NamedChild(0)
}
