// exception_flow.go — supplemental pass that emits THROWS / CATCHES edges from
// Elixir functions (def/defp) to a shared SCOPE.ExceptionType node (epic
// #3628). It lets the graph answer "what can this function raise?" (outbound
// THROWS) and "where is X handled?" (inbound CATCHES), matching the flagship
// convergence-node shape every other language emits (see
// internal/extractor/exception_flow.go).
//
// Detected shapes (typed only — honest-partial, precision-first):
//
//	raise NotFoundError, "msg"          → THROWS NotFoundError
//	raise ArgumentError                 → THROWS ArgumentError
//	raise MyApp.NotFoundError, message: → THROWS NotFoundError   (last segment)
//	try do … rescue e in NotFoundError  → CATCHES NotFoundError
//	rescue e in [RuntimeError, ArgErr]  → CATCHES RuntimeError + ArgErr
//	rescue MyApp.BadError ->            → CATCHES BadError        (no binding)
//
// Intentionally DROPPED (would mislead error-contract analysis):
//
//	raise "just a message"              (string → implicit RuntimeError, no type token)
//	reraise err, __STACKTRACE__         (re-raise of a bound variable — not `raise`)
//	rescue _ -> …  /  rescue e -> …     (untyped catch-all — no exception type)
//	catch :throw, val -> …              (value/exit catch, not the exception model)
//	{:error, reason}                    (tuple convention is value-based, not error_flow)
//
// All node/edge construction (convergence on one node per type name via a
// synthetic SourceFile) lives in extractor.EmitExceptionEdges, so the
// convergence invariant is identical across every language.

package elixir

import (
	"github.com/cajasmota/grafel/internal/treesitter/ts"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
)

// emitExceptionFlowEdges scans every def/defp body for typed raise / rescue
// shapes and appends exception-type entities + THROWS / CATCHES edges.
//
// entities[0] MUST be the file entity (Extract appends it first). Mutates
// *entities in place. Safe with nil / empty input. raise / rescue outside any
// def (module scope) attach to the file entity via EmitExceptionEdges's
// fallback.
func emitExceptionFlowEdges(root ts.Node, file extractor.FileInput, entities *[]types.EntityRecord) {
	if root == nil || entities == nil || len(*entities) == 0 {
		return
	}
	src := file.Content

	var edges []extractor.ExceptionEdge

	// In tree-sitter-elixir every form is a `call`; def/defp introduce the
	// enclosing-function scope. We track the nearest def/defp name so each
	// raise / rescue is attributed to the right SCOPE.Operation (matched by
	// Name inside EmitExceptionEdges).
	var walk func(n ts.Node, current string)
	walk = func(n ts.Node, current string) {
		if n == nil {
			return
		}
		if n.Type() == "call" {
			head := callHeadName(n, src)
			switch head {
			case "def", "defp":
				// Enter a new function scope for the body; the head/args
				// (the function signature) keep the outer scope.
				fnName := extractFunctionName(n, src)
				body := findDefBody(n)
				for i := 0; i < int(n.ChildCount()); i++ {
					ch := n.Child(i)
					if ts.SameNode(ch, body) {
						walk(ch, fnName)
					} else {
						walk(ch, current)
					}
				}
				return
			case "raise":
				if t := elixirRaiseType(n, src); t != "" {
					edges = append(edges, extractor.ExceptionEdge{
						Type: t, FromName: current, Pattern: "raise",
					})
				}
				// raise takes no nested def/raise of interest; fall through to
				// descend anyway is harmless but unnecessary — descend for safety.
			}
		}
		if n.Type() == "rescue_block" {
			for _, t := range elixirRescueTypes(n, src) {
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

	extractor.EmitExceptionEdges(entities, "elixir", edges)
}

// elixirRaiseType returns the raised exception-module token from a `raise`
// call node, or "" for a message-only raise (`raise "msg"` → implicit
// RuntimeError, no static type token) or any non-alias first argument.
//
// Accepted shape: the first argument is an `alias` (the exception module),
// e.g. `raise NotFoundError, "x"`, `raise ArgumentError`,
// `raise MyApp.NotFoundError, message: "x"`. The verbatim (possibly dotted)
// alias text is returned; NormalizeExceptionType strips qualification and
// rejects non-identifier tokens downstream.
//
// NOTE: `reraise err, __STACKTRACE__` is a DIFFERENT call head ("reraise"),
// never reaches this function, and so emits no edge — honest, since the type
// is a bound variable.
func elixirRaiseType(raiseCall ts.Node, src []byte) string {
	args := childOfType(raiseCall, "arguments")
	if args == nil {
		return ""
	}
	first := firstNamedChild(args)
	if first == nil || first.Type() != "alias" {
		return "" // `raise "msg"` / `raise var` / computed — no static type
	}
	return nodeSrc(first, src)
}

// elixirRescueTypes returns the caught exception-module tokens from a
// rescue_block. Each `stab_clause` head is one of:
//
//	e in NotFoundError -> …            binary_operator(in); RHS alias        → {NotFoundError}
//	e in [RuntimeError, ArgError] -> … binary_operator(in); RHS list(alias)  → {RuntimeError, ArgError}
//	MyApp.BadError -> …                bare alias (no binding)               → {BadError}
//	_ -> …  /  e -> …                  identifier (untyped catch-all)        → {}
//
// Untyped catch-alls and non-alias shapes yield nothing (honest-partial).
func elixirRescueTypes(rescueBlock ts.Node, src []byte) []string {
	var out []string
	for i := 0; i < int(rescueBlock.NamedChildCount()); i++ {
		stab := rescueBlock.NamedChild(i)
		if stab == nil || stab.Type() != "stab_clause" {
			continue
		}
		args := childOfType(stab, "arguments")
		if args == nil {
			continue
		}
		head := firstNamedChild(args)
		if head == nil {
			continue
		}
		switch head.Type() {
		case "binary_operator":
			// `e in <type-or-list>` — the rescued type(s) are the RHS, the
			// second named child. Only the `in` operator carries a type list;
			// any other binary operator is not a typed rescue.
			if rhs := nthNamedChild(head, 1); rhs != nil {
				out = append(out, aliasTokens(rhs, src)...)
			}
		case "alias":
			// `MyApp.BadError ->` — typed rescue without a binding.
			out = append(out, nodeSrc(head, src))
		}
		// identifier / `_` (untyped catch-all) and anything else: no type.
	}
	return out
}

// aliasTokens collects exception-module tokens from a rescue RHS that is
// either a single `alias` or a `list` of `alias` nodes. Non-alias elements
// are skipped.
func aliasTokens(n ts.Node, src []byte) []string {
	switch n.Type() {
	case "alias":
		return []string{nodeSrc(n, src)}
	case "list":
		var out []string
		for i := 0; i < int(n.NamedChildCount()); i++ {
			el := n.NamedChild(i)
			if el != nil && el.Type() == "alias" {
				out = append(out, nodeSrc(el, src))
			}
		}
		return out
	}
	return nil
}

// childOfType returns the first direct child of n whose Type() == t, or nil.
func childOfType(n ts.Node, t string) ts.Node {
	for i := 0; i < int(n.ChildCount()); i++ {
		ch := n.Child(i)
		if ch != nil && ch.Type() == t {
			return ch
		}
	}
	return nil
}

// firstNamedChild returns the first named child of n, or nil.
func firstNamedChild(n ts.Node) ts.Node {
	if n.NamedChildCount() == 0 {
		return nil
	}
	return n.NamedChild(0)
}

// nthNamedChild returns the i-th named child of n (0-based), or nil.
func nthNamedChild(n ts.Node, i int) ts.Node {
	if i < 0 || i >= int(n.NamedChildCount()) {
		return nil
	}
	return n.NamedChild(i)
}

// nodeSrc returns the verbatim source text spanned by n.
func nodeSrc(n ts.Node, src []byte) string {
	return string(src[n.StartByte():n.EndByte()])
}
