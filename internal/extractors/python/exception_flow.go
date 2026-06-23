// exception_flow.go — supplemental pass that emits THROWS / CATCHES edges
// from Python functions / methods to a shared SCOPE.ExceptionType node (epic
// #3628). It lets the graph answer "what can this function raise?" (outbound
// THROWS) and "where is X handled?" (inbound CATCHES).
//
// Detected shapes (typed only — honest-partial, precision-first):
//
//	raise NotFound(...)            → THROWS NotFound
//	raise ValidationError          → THROWS ValidationError   (bare name re-raise of a class)
//	raise errors.NotFound()        → THROWS NotFound          (last dotted segment)
//	except NotFound:               → CATCHES NotFound
//	except (ValueError, KeyError): → CATCHES ValueError + KeyError
//	except mod.IOError as e:       → CATCHES IOError
//
// Intentionally DROPPED (would mislead error-contract analysis):
//
//	raise                          (bare re-raise — no type token)
//	raise exc_class()              (dynamic / computed type, see NormalizeExceptionType)
//	except:                        (bare except — no type)
//	except Exception as e: pass    (still recorded — Exception IS a type; only
//	                                bare `except:` with no type is dropped)
//
// All node/edge construction (convergence on one node per type name via a
// synthetic SourceFile) lives in extractor.EmitExceptionEdges.

package python

import (
	"strings"

	"github.com/cajasmota/grafel/internal/treesitter/ts"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
)

// emitExceptionFlowEdges scans every function / method body for typed raise /
// except shapes and appends exception-type entities + THROWS / CATCHES edges.
//
// entities[0] MUST be the file entity. Mutates *entities in place. Safe with
// nil / empty input. raise/except at module scope attach to the file entity.
func emitExceptionFlowEdges(root ts.Node, file extractor.FileInput, entities *[]types.EntityRecord) {
	if root == nil || entities == nil || len(*entities) == 0 {
		return
	}
	src := file.Content

	var edges []extractor.ExceptionEdge

	var stack []string // enclosing entity-Name stack; top = current scope
	current := func() string {
		if len(stack) == 0 {
			return "" // module scope → file entity
		}
		return stack[len(stack)-1]
	}

	var walk func(n ts.Node, parentClass string)
	walk = func(n ts.Node, parentClass string) {
		if n == nil {
			return
		}
		switch n.Type() {
		case "class_definition":
			cls := ""
			if nn := n.ChildByFieldName("name"); nn != nil {
				cls = nodeText(nn, src)
			}
			childCls := cls
			if parentClass != "" && cls != "" {
				childCls = parentClass + "." + cls
			}
			stack = append(stack, childCls)
			if body := n.ChildByFieldName("body"); body != nil {
				for i := 0; i < int(body.ChildCount()); i++ {
					walk(body.Child(i), childCls)
				}
			}
			stack = stack[:len(stack)-1]
			return
		case "function_definition":
			leaf := ""
			if nn := n.ChildByFieldName("name"); nn != nil {
				leaf = nodeText(nn, src)
			}
			emitted := leaf
			if parentClass != "" && leaf != "" {
				emitted = parentClass + "." + leaf
			}
			stack = append(stack, emitted)
			if body := n.ChildByFieldName("body"); body != nil {
				for i := 0; i < int(body.ChildCount()); i++ {
					walk(body.Child(i), parentClass)
				}
			}
			stack = stack[:len(stack)-1]
			return
		case "decorated_definition":
			if inner := n.ChildByFieldName("definition"); inner != nil {
				walk(inner, parentClass)
			}
			return
		case "raise_statement":
			if t := pyRaiseType(n, src); t != "" {
				edges = append(edges, extractor.ExceptionEdge{
					Type: t, FromName: current(), Pattern: "raise",
				})
			}
		case "except_clause":
			for _, t := range pyExceptTypes(n, src) {
				edges = append(edges, extractor.ExceptionEdge{
					Type: t, FromName: current(), Catch: true, Pattern: "except",
				})
			}
		}
		for i := 0; i < int(n.ChildCount()); i++ {
			walk(n.Child(i), parentClass)
		}
	}
	walk(root, "")

	extractor.EmitExceptionEdges(entities, "python", edges)
}

// pyRaiseType returns the raised exception-type token from a raise_statement,
// or "" for a bare `raise` (re-raise) or a dynamic/computed expression.
//
// The grammar shapes we accept:
//
//	raise Name              → identifier child            → "Name"
//	raise Name(...)         → call(function=identifier)   → "Name"
//	raise mod.Name(...)     → call(function=attribute)    → "Name" (last segment)
//	raise Name(...) from e  → first child is the call; the `from` cause ignored
//
// Anything else (subscript, lambda, conditional, a call whose function is not
// a plain identifier/attribute) returns "" so NormalizeExceptionType never
// fabricates a node.
//
// Precision guard: a BARE identifier (`raise e`) is almost always a variable
// holding a caught exception instance (a re-raise), NOT a class — Python /
// PEP-8 spells exception CLASSES in PascalCase. We therefore require a
// bare-identifier raise to start with an uppercase letter before treating it
// as a type. A call (`raise Foo()`) or qualified attribute (`raise mod.Foo`)
// is an explicit construction/reference, so no case guard is applied there.
func pyRaiseType(raiseNode ts.Node, src []byte) string {
	// The exception expression is the first named child; subsequent named
	// children (e.g. the `from <cause>`) are ignored.
	var expr ts.Node
	for i := 0; i < int(raiseNode.NamedChildCount()); i++ {
		c := raiseNode.NamedChild(i)
		if c == nil {
			continue
		}
		expr = c
		break
	}
	if expr == nil {
		return "" // bare `raise`
	}
	t := pyTypeFromExpr(expr, src)
	// Disambiguate class-vs-variable for UNQUALIFIED forms via the PascalCase
	// class convention:
	//   raise e          (bare identifier)     → class only if PascalCase
	//   raise exc_class() (call on identifier)  → class only if PascalCase
	// Qualified forms (`raise mod.Foo` / `raise mod.Foo()`) are explicit
	// references to a class and are kept regardless of case.
	switch expr.Type() {
	case "identifier":
		if !startsUpper(t) {
			return "" // `raise e` — variable re-raise, not a class. Drop.
		}
	case "call":
		if fn := expr.ChildByFieldName("function"); fn != nil && fn.Type() == "identifier" && !startsUpper(t) {
			return "" // `raise exc_class()` — factory/variable, not a class. Drop.
		}
	}
	return t
}

// startsUpper reports whether s begins with an ASCII uppercase letter (the
// PascalCase class-name convention used to disambiguate a bare-identifier
// raise of a CLASS from a re-raise of an exception VARIABLE).
func startsUpper(s string) bool {
	return s != "" && s[0] >= 'A' && s[0] <= 'Z'
}

// pyExceptTypes returns the caught exception-type tokens from an except_clause.
// `except:` (bare) yields none; `except E:` yields {E}; `except (A, B):` yields
// {A, B}. Each token is the bare class name (last dotted segment); dynamic
// entries are dropped by NormalizeExceptionType downstream.
func pyExceptTypes(exceptNode ts.Node, src []byte) []string {
	// The type expression is the first named child of the except_clause that
	// is NOT the `as <name>` binding and NOT the body block. In the tree-sitter
	// Python grammar the clause children are: [type_expr] [as identifier] block.
	var typeExpr ts.Node
	for i := 0; i < int(exceptNode.NamedChildCount()); i++ {
		c := exceptNode.NamedChild(i)
		if c == nil {
			continue
		}
		switch c.Type() {
		case "block", "identifier_pattern", "comment":
			continue
		case "identifier", "attribute", "tuple", "parenthesized_expression", "expression_list":
			typeExpr = c
		}
		if typeExpr != nil {
			break
		}
	}
	if typeExpr == nil {
		return nil // bare `except:`
	}
	var out []string
	switch typeExpr.Type() {
	case "identifier", "attribute":
		if t := pyTypeFromExpr(typeExpr, src); t != "" {
			out = append(out, t)
		}
	case "tuple", "parenthesized_expression", "expression_list":
		for i := 0; i < int(typeExpr.NamedChildCount()); i++ {
			el := typeExpr.NamedChild(i)
			if el == nil {
				continue
			}
			if t := pyTypeFromExpr(el, src); t != "" {
				out = append(out, t)
			}
		}
	}
	return out
}

// pyTypeFromExpr extracts the bare exception class name from an expression that
// is an identifier, a dotted attribute, or a call on either. Returns "" for any
// other shape (so dynamic raises drop). The bare name is returned verbatim;
// NormalizeExceptionType (called inside EmitExceptionEdges) strips qualification
// and rejects non-identifier tokens.
func pyTypeFromExpr(n ts.Node, src []byte) string {
	switch n.Type() {
	case "identifier":
		return strings.TrimSpace(nodeText(n, src))
	case "attribute":
		// mod.Name → take the trailing attribute leaf.
		if attr := n.ChildByFieldName("attribute"); attr != nil {
			return strings.TrimSpace(nodeText(attr, src))
		}
		return ""
	case "call":
		if fn := n.ChildByFieldName("function"); fn != nil {
			return pyTypeFromExpr(fn, src)
		}
	}
	return ""
}
