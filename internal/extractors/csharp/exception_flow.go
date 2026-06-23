// exception_flow.go — supplemental pass that emits THROWS / CATCHES edges from
// C# methods / constructors to a shared SCOPE.ExceptionType node (epic #3628).
// It lets the graph answer "what can this method throw?" (outbound THROWS) and
// "where is NotFoundException handled?" (inbound CATCHES), keeping error-contract
// parity cross-language with the Java / Python / Go / JS flagships. Node/edge
// construction is delegated to extractor.EmitExceptionEdges so the convergence
// invariant (identical type names → ONE node) is identical everywhere.
//
// Detected shapes (typed only — honest-partial, precision-first):
//
//	throw new NotFoundException(...)          → THROWS NotFoundException
//	throw new System.IO.IOException(...)      → THROWS IOException  (qualified → bare)
//	} catch (SqlException ex) { ... }         → CATCHES SqlException
//	} catch (System.IO.IOException) { ... }   → CATCHES IOException  (no var name)
//
// Deliberately NOT recorded (would mislead error-contract analysis):
//
//	throw;            (re-throw, carries no NEW type)
//	throw ex;         (re-throw of a variable, no NEW type)
//	catch { ... }     (bare catch, untyped — no catch_declaration)
//	catch (X ex) when (cond) — the `when` filter is a sibling clause; only the
//	                  caught TYPE X is recorded, never the filter expression.
//
// Unlike Java, C# has no `throws` clause and no `|` multi-catch (a `catch (A | B)`
// parses as an ERROR node, so it is never extracted). FromName mirrors the
// `buildOperation` naming (`<immediateClass>.<method>`) so THROWS / CATCHES edges
// attach to the same SCOPE.Operation host the rest of the extractor emits.

package csharp

import (
	"github.com/cajasmota/grafel/internal/treesitter/ts"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
)

// emitExceptionFlowEdges scans every method / constructor body for `throw new`
// and typed `catch` shapes and appends exception-type entities + THROWS /
// CATCHES edges to *entities.
//
// entities[0] MUST be the file entity. Mutates *entities in place. Safe with
// nil / empty input.
func emitExceptionFlowEdges(root ts.Node, src []byte, entities *[]types.EntityRecord) {
	if root == nil || entities == nil || len(*entities) == 0 {
		return
	}

	var edges []extractor.ExceptionEdge

	// enclosingClass is the innermost type name (matching buildOperation, which
	// prefixes only the immediate parent type). enclosingMethod is the host
	// SCOPE.Operation Name (`<class>.<method>`), or "" at file scope.
	var walk func(n ts.Node, enclosingClass, enclosingMethod string)
	walk = func(n ts.Node, enclosingClass, enclosingMethod string) {
		if n == nil {
			return
		}
		switch n.Type() {
		case "class_declaration", "interface_declaration", "struct_declaration", "record_declaration":
			cls := childFieldText(n, "name", src)
			body := findTypeBody(n)
			if body != nil {
				for i := 0; i < int(body.ChildCount()); i++ {
					walk(body.Child(i), cls, "")
				}
			}
			return
		case "method_declaration", "constructor_declaration":
			leaf := childFieldText(n, "name", src)
			method := leaf
			if enclosingClass != "" && leaf != "" {
				method = enclosingClass + "." + leaf
			}
			if body := n.ChildByFieldName("body"); body != nil {
				walk(body, enclosingClass, method)
			}
			return
		case "throw_statement":
			if t := csharpThrowType(n, src); t != "" {
				edges = append(edges, extractor.ExceptionEdge{
					Type: t, FromName: enclosingMethod, Pattern: "throw_new",
				})
			}
		case "catch_clause":
			if t := csharpCatchType(n, src); t != "" {
				edges = append(edges, extractor.ExceptionEdge{
					Type: t, FromName: enclosingMethod, Catch: true, Pattern: "catch_type",
				})
			}
		}
		for i := 0; i < int(n.ChildCount()); i++ {
			walk(n.Child(i), enclosingClass, enclosingMethod)
		}
	}
	walk(root, "", "")

	extractor.EmitExceptionEdges(entities, "csharp", edges)
}

// csharpThrowType returns the constructed exception type for
// `throw new X(...)` / `throw new pkg.X(...)`, or "" for a bare re-throw
// (`throw;`) or a re-throw of a variable (`throw ex;`) which carries no NEW
// type. The raw (possibly qualified) type text is returned verbatim;
// extractor.NormalizeExceptionType collapses it to the bare class name.
func csharpThrowType(throwNode ts.Node, src []byte) string {
	creation := findChildByType(throwNode, "object_creation_expression")
	if creation == nil {
		return "" // `throw;` or `throw ex;` — no NEW type
	}
	if typeNode := creation.ChildByFieldName("type"); typeNode != nil {
		return nodeText(typeNode, src)
	}
	// Fallback when the grammar exposes the type un-fielded: first
	// identifier / qualified_name child after the `new` keyword.
	for i := 0; i < int(creation.NamedChildCount()); i++ {
		c := creation.NamedChild(i)
		if c == nil {
			continue
		}
		if c.Type() == "identifier" || c.Type() == "qualified_name" {
			return nodeText(c, src)
		}
	}
	return ""
}

// csharpCatchType returns the caught type from a catch clause's
// `catch_declaration`, or "" for a bare untyped `catch { }` (no declaration).
// The `when (...)` filter is a separate catch_filter_clause sibling and is
// never inspected, so only the declared type is recorded. C# has no `|`
// multi-catch (that shape parses as an ERROR node and yields no type here).
func csharpCatchType(catchNode ts.Node, src []byte) string {
	decl := findChildByType(catchNode, "catch_declaration")
	if decl == nil {
		return "" // bare `catch { }` — untyped, honest drop
	}
	// First named child of catch_declaration is the exception TYPE
	// (`identifier` or `qualified_name`); a following identifier, when present,
	// is the bound variable name and must be ignored.
	for i := 0; i < int(decl.NamedChildCount()); i++ {
		c := decl.NamedChild(i)
		if c == nil {
			continue
		}
		if c.Type() == "identifier" || c.Type() == "qualified_name" {
			return nodeText(c, src)
		}
	}
	return ""
}
