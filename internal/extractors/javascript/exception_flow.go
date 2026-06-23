// exception_flow.go — supplemental pass that emits THROWS / CATCHES edges from
// JS/TS functions / methods to a shared SCOPE.ExceptionType node (epic #3628).
// It lets the graph answer "what can this function throw?" (outbound THROWS)
// and "where is ValidationError handled?" (inbound CATCHES).
//
// Detected shapes (typed only — honest-partial, precision-first):
//
//	throw new ValidationError(...)        → THROWS ValidationError
//	throw new Error("...")                → THROWS Error
//	} catch (e) { if (e instanceof X) }   → CATCHES X
//	} catch (e) { if (!(e instanceof X))  → CATCHES X   (rethrow filter)
//
// Intentionally DROPPED (would mislead error-contract analysis):
//
//	throw err / throw something()         (no identifiable NEW type)
//	throw {code: 500}                     (object literal, not a class)
//	} catch (e) { ... }                   (untyped catch — no instanceof)
//	} catch (e) { throw e }               (untyped re-throw)
//
// TypeScript has no typed catch binding (the binding is always `any`/`unknown`),
// so the ONLY informative catch signal is an `instanceof <Type>` test inside
// the catch body — that is what we key on. All node/edge construction
// (convergence on one node per type name) lives in extractor.EmitExceptionEdges.

package javascript

import (
	"github.com/cajasmota/grafel/internal/treesitter/ts"

	extreg "github.com/cajasmota/grafel/internal/extractor"
)

// emitExceptionFlowEdges scans the AST for typed throw / instanceof-catch
// shapes and appends exception-type entities + THROWS / CATCHES edges to
// x.entities. x.entities[0] MUST be the file entity. Safe with an empty tree.
func (x *extractor) emitExceptionFlowEdges(root ts.Node) {
	if root == nil || len(x.entities) == 0 {
		return
	}

	var edges []extreg.ExceptionEdge

	var walk func(n ts.Node, enclosing string)
	walk = func(n ts.Node, enclosing string) {
		if n == nil {
			return
		}
		switch n.Type() {
		case "function_declaration", "generator_function_declaration":
			name := x.nodeText(n.ChildByFieldName("name"))
			if body := n.ChildByFieldName("body"); body != nil {
				walk(body, name)
			}
			return
		case "method_definition":
			name := x.nodeText(n.ChildByFieldName("name"))
			if body := n.ChildByFieldName("body"); body != nil {
				walk(body, name)
			}
			return
		case "variable_declarator":
			nameNode := n.ChildByFieldName("name")
			valNode := n.ChildByFieldName("value")
			if nameNode != nil && valNode != nil {
				switch valNode.Type() {
				case "arrow_function", "function", "function_expression":
					name := x.nodeText(nameNode)
					if body := valNode.ChildByFieldName("body"); body != nil {
						walk(body, name)
						return
					}
				}
			}
		case "throw_statement":
			if t := jsThrowType(x, n); t != "" {
				edges = append(edges, extreg.ExceptionEdge{
					Type: t, FromName: enclosing, Pattern: "throw_new",
				})
			}
		case "catch_clause":
			for _, t := range jsCatchInstanceofTypes(x, n) {
				edges = append(edges, extreg.ExceptionEdge{
					Type: t, FromName: enclosing, Catch: true, Pattern: "instanceof",
				})
			}
		}
		for i := 0; i < int(n.ChildCount()); i++ {
			walk(n.Child(i), enclosing)
		}
	}
	walk(root, "")

	extreg.EmitExceptionEdges(&x.entities, x.language, edges)
}

// jsThrowType returns the thrown exception-type name for `throw new X(...)`
// (including `throw new mod.X(...)` → bare X), or "" for any non-`new` throw
// (re-throw of a variable, object literal, computed expression) — those carry
// no identifiable type and are dropped.
func jsThrowType(x *extractor, throwNode ts.Node) string {
	// The thrown expression is the first named child of throw_statement.
	var expr ts.Node
	for i := 0; i < int(throwNode.NamedChildCount()); i++ {
		c := throwNode.NamedChild(i)
		if c != nil {
			expr = c
			break
		}
	}
	if expr == nil || expr.Type() != "new_expression" {
		return "" // only `throw new <Type>(...)` is identifiable
	}
	ctor := expr.ChildByFieldName("constructor")
	if ctor == nil {
		return ""
	}
	switch ctor.Type() {
	case "identifier", "type_identifier":
		return x.nodeText(ctor)
	case "member_expression":
		// new mod.ValidationError(...) → trailing property leaf.
		if prop := ctor.ChildByFieldName("property"); prop != nil {
			return x.nodeText(prop)
		}
	}
	return ""
}

// jsCatchInstanceofTypes returns every type X appearing in an
// `<binding> instanceof X` test inside the catch body. Because JS/TS catch
// bindings are untyped, the instanceof test is the only informative catch
// signal. Returns nil when the catch body contains no instanceof test (untyped
// catch → no CATCHES edge, honest-partial).
func jsCatchInstanceofTypes(x *extractor, catchNode ts.Node) []string {
	body := catchNode.ChildByFieldName("body")
	if body == nil {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	var scan func(n ts.Node)
	scan = func(n ts.Node) {
		if n == nil {
			return
		}
		if n.Type() == "binary_expression" {
			// tree-sitter exposes the operator as an anonymous child token; the
			// right operand is the type. Match `<x> instanceof <Type>`.
			if op := n.ChildByFieldName("operator"); op != nil && x.nodeText(op) == "instanceof" {
				if right := n.ChildByFieldName("right"); right != nil {
					if t := jsInstanceofTypeName(x, right); t != "" && !seen[t] {
						seen[t] = true
						out = append(out, t)
					}
				}
			}
		}
		for i := 0; i < int(n.ChildCount()); i++ {
			scan(n.Child(i))
		}
	}
	scan(body)
	return out
}

// jsInstanceofTypeName extracts the bare type name from the right-hand side of
// an instanceof test: an identifier, or a qualified `mod.Type` member.
func jsInstanceofTypeName(x *extractor, right ts.Node) string {
	switch right.Type() {
	case "identifier", "type_identifier":
		return x.nodeText(right)
	case "member_expression":
		if prop := right.ChildByFieldName("property"); prop != nil {
			return x.nodeText(prop)
		}
	}
	return ""
}
