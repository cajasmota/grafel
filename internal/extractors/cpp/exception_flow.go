// exception_flow.go — supplemental pass that emits THROWS / CATCHES edges from
// C / C++ functions and methods to a shared SCOPE.ExceptionType node (epic
// #3628). It lets the graph answer "what can this function throw?" (outbound
// THROWS) and "where is NotFoundException handled?" (inbound CATCHES), keeping
// error-contract parity cross-language with the Java / Python / Go / JS / C# /
// PHP flagships. Node/edge construction is delegated to
// extractor.EmitExceptionEdges so the convergence invariant (identical type
// names → ONE node) is identical everywhere.
//
// Detected shapes (typed only — honest-partial, precision-first):
//
//	throw NotFoundException("msg")           → THROWS NotFoundException  (throw-by-value, call_expression)
//	throw MyNs::Boom{}                        → THROWS Boom               (qualified compound literal → bare)
//	throw std::runtime_error("x")            → THROWS runtime_error      (std:: → bare last segment)
//	throw new Foo()                           → THROWS Foo                (rare in C++, but valid; new_expression)
//	} catch (const NotFoundException& e) { }  → CATCHES NotFoundException (const-ref qualified)
//	} catch (std::runtime_error& e) { }       → CATCHES runtime_error     (std:: ref → bare)
//	} catch (MyError* p) { }                   → CATCHES MyError           (pointer)
//	} catch (ValueErr e) { }                   → CATCHES ValueErr          (by value)
//
// Reference (`const X&`), pointer (`X*`), and by-value (`X`) catches all yield
// the bare type X because the catch parameter_declaration's `type` field always
// names the unadorned type; `const` qualifiers and `&`/`*` declarators are
// sibling nodes, not part of the type.
//
// Deliberately NOT recorded (would mislead error-contract analysis):
//
//	throw;            (bare re-throw — carries no NEW type)
//	throw ex;         (re-throw of a variable — identifier, no NEW type)
//	catch (...) { }   (catch-all — untyped, no parameter_declaration)
//
// std:: / namespace normalization: extractor.NormalizeExceptionType collapses
// any `::`-qualified token to its last segment, so `std::runtime_error` →
// `runtime_error` and `MyNs::Boom` → `Boom`. This matches the flagship
// last-segment convention (a `std::exception` raised in one TU and caught in
// another converge on ONE `exception:exception` node).
//
// FromName is the bare enclosing function/method Name, mirroring the C/C++
// extractor's operation naming (extractFunction emits the unqualified leaf name,
// e.g. `find`, `handle`), so THROWS / CATCHES edges attach to the same
// SCOPE.Operation host the rest of the extractor emits. Throws/catches at file
// scope (outside any function) fall back to the file entity via
// extractor.EmitExceptionEdges.

package cpp

import (
	"github.com/cajasmota/grafel/internal/treesitter/ts"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
)

// emitExceptionFlowEdges scans every function/method body for `throw` and typed
// `catch` shapes and appends exception-type entities + THROWS / CATCHES edges to
// *entities.
//
// entities[0] MUST be the file entity. Mutates *entities in place. Safe with
// nil / empty input.
func emitExceptionFlowEdges(root ts.Node, src []byte, entities *[]types.EntityRecord) {
	if root == nil || entities == nil || len(*entities) == 0 {
		return
	}

	var edges []extractor.ExceptionEdge

	// enclosingFn is the bare Name of the innermost function/method
	// (matching extractFunction's resolveFunctionName leaf), or "" at file
	// scope. Lambdas inside a function keep the outer function's name so the
	// edge stays attributed to the host operation.
	var walk func(n ts.Node, enclosingFn string)
	walk = func(n ts.Node, enclosingFn string) {
		if n == nil {
			return
		}
		switch n.Type() {
		case "function_definition":
			if decl := n.ChildByFieldName("declarator"); decl != nil {
				if name := resolveFunctionName(decl, src); name != "" {
					enclosingFn = name
				}
			}
		case "throw_statement":
			if t := cppThrowType(n, src); t != "" {
				edges = append(edges, extractor.ExceptionEdge{
					Type: t, FromName: enclosingFn, Pattern: "throw",
				})
			}
		case "catch_clause":
			if t := cppCatchType(n, src); t != "" {
				edges = append(edges, extractor.ExceptionEdge{
					Type: t, FromName: enclosingFn, Catch: true, Pattern: "catch_type",
				})
			}
		}
		for i := 0; i < int(n.ChildCount()); i++ {
			walk(n.Child(i), enclosingFn)
		}
	}
	walk(root, "")

	extractor.EmitExceptionEdges(entities, "cpp", edges)
}

// cppThrowType returns the constructed exception type for a throw statement, or
// "" for a bare re-throw (`throw;`) or a re-throw of a variable (`throw ex;`)
// which carry no NEW type. The raw (possibly `::`-qualified) type text is
// returned verbatim; extractor.NormalizeExceptionType collapses it to the bare
// class name.
//
// Handled throw expression shapes:
//
//	call_expression               throw Foo(...)   / throw ns::Foo(...)   (throw-by-value, the C++ idiom)
//	compound_literal_expression   throw Foo{}      / throw ns::Foo{}      (brace-init temporary)
//	new_expression                throw new Foo()                          (rare; heap-allocated, valid)
//
// An `identifier` child (`throw ex;`) or no expression child (`throw;`) returns
// "" — a re-throw introduces no new type.
func cppThrowType(throwNode ts.Node, src []byte) string {
	for i := 0; i < int(throwNode.NamedChildCount()); i++ {
		c := throwNode.NamedChild(i)
		if c == nil {
			continue
		}
		switch c.Type() {
		case "call_expression":
			if fn := c.ChildByFieldName("function"); fn != nil {
				return nodeText(fn, src)
			}
		case "compound_literal_expression", "new_expression":
			if ty := c.ChildByFieldName("type"); ty != nil {
				return nodeText(ty, src)
			}
		}
		// First named child is the throw operand; if it isn't one of the
		// constructed-exception shapes above (e.g. a bare identifier for
		// `throw ex;`), there is no NEW type to record.
		return ""
	}
	return "" // `throw;` — bare re-throw, no operand
}

// cppCatchType returns the caught type from a catch clause's parameter, or "" for
// the untyped catch-all `catch (...) { }` (which has no parameter_declaration).
//
// The catch parameter is `(const X& e)` / `(X* p)` / `(X e)` etc.; the
// parameter_declaration's `type` field always names the bare type X regardless
// of const-qualification or reference/pointer declarators (those are sibling
// nodes), so reference, pointer, and by-value catches all yield X.
func cppCatchType(catchNode ts.Node, src []byte) string {
	params := catchNode.ChildByFieldName("parameters")
	if params == nil {
		return ""
	}
	for i := 0; i < int(params.NamedChildCount()); i++ {
		c := params.NamedChild(i)
		if c == nil || c.Type() != "parameter_declaration" {
			continue // `catch (...)` has a `...` child, not a declaration
		}
		if ty := c.ChildByFieldName("type"); ty != nil {
			return nodeText(ty, src)
		}
	}
	return ""
}
