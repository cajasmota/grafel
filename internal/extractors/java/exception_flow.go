// exception_flow.go — supplemental pass that emits THROWS / CATCHES edges from
// Java methods / constructors to a shared SCOPE.ExceptionType node (epic
// #3628). It lets the graph answer "what can this method throw?" (outbound
// THROWS) and "where is IOException handled?" (inbound CATCHES).
//
// Detected shapes (typed only — honest-partial, precision-first):
//
//	throw new IllegalArgumentException(...)   → THROWS IllegalArgumentException
//	void read() throws IOException, SQLException { ... }
//	                                          → THROWS IOException + SQLException
//	} catch (IOException e) { ... }           → CATCHES IOException
//	} catch (IOException | SQLException e) {}  → CATCHES IOException + SQLException
//
// Java's checked-exception model makes both the `throws` clause and the typed
// `catch` highly reliable type signals (unlike JS/TS), so all three are
// recorded. Qualified names (`throw new com.x.Boom()`, `catch (java.io.IOException e)`)
// converge to their bare class via NormalizeExceptionType. Node/edge
// construction lives in extractor.EmitExceptionEdges.

package java

import (
	"strings"

	"github.com/cajasmota/grafel/internal/treesitter/ts"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
)

// emitExceptionFlowEdges scans every method / constructor for throw / throws /
// catch shapes and appends exception-type entities + THROWS / CATCHES edges.
//
// entities[0] MUST be the file entity. Mutates *entities in place. Safe with
// nil / empty input.
func emitExceptionFlowEdges(root ts.Node, file extractor.FileInput, entities *[]types.EntityRecord) {
	if root == nil || entities == nil || len(*entities) == 0 {
		return
	}
	src := file.Content

	var edges []extractor.ExceptionEdge

	var walk func(n ts.Node, enclosingClass, enclosingMethod string)
	walk = func(n ts.Node, enclosingClass, enclosingMethod string) {
		if n == nil {
			return
		}
		switch n.Type() {
		case "class_declaration", "record_declaration", "interface_declaration", "enum_declaration":
			cls := childFieldText(n, "name", src)
			childCls := cls
			if enclosingClass != "" && cls != "" {
				childCls = enclosingClass + "." + cls
			}
			if body := n.ChildByFieldName("body"); body != nil {
				for i := 0; i < int(body.ChildCount()); i++ {
					walk(body.Child(i), childCls, "")
				}
			}
			return
		case "method_declaration", "constructor_declaration":
			leaf := childFieldText(n, "name", src)
			method := leaf
			if enclosingClass != "" && leaf != "" {
				method = enclosingClass + "." + leaf
			}
			// `throws IOException, SQLException` clause → declared THROWS.
			for _, t := range javaThrowsClauseTypes(n, src) {
				edges = append(edges, extractor.ExceptionEdge{
					Type: t, FromName: method, Pattern: "throws_clause",
				})
			}
			if body := n.ChildByFieldName("body"); body != nil {
				walk(body, enclosingClass, method)
			}
			return
		case "throw_statement":
			if t := javaThrowType(n, src); t != "" {
				edges = append(edges, extractor.ExceptionEdge{
					Type: t, FromName: enclosingMethod, Pattern: "throw_new",
				})
			}
		case "catch_clause":
			for _, t := range javaCatchTypes(n, src) {
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

	extractor.EmitExceptionEdges(entities, "java", edges)
}

// javaThrowType returns the constructed exception class for `throw new X(...)`
// (including `throw new pkg.X(...)` → bare X), or "" for a re-throw of a
// variable / computed expression (`throw e;`) which carries no NEW type.
func javaThrowType(throwNode ts.Node, src []byte) string {
	var expr ts.Node
	for i := 0; i < int(throwNode.NamedChildCount()); i++ {
		c := throwNode.NamedChild(i)
		if c != nil {
			expr = c
			break
		}
	}
	if expr == nil || expr.Type() != "object_creation_expression" {
		return "" // only `throw new <Type>(...)` is identifiable
	}
	typeNode := expr.ChildByFieldName("type")
	if typeNode == nil {
		return ""
	}
	return lastIdent(strings.TrimSpace(nodeText(typeNode, src)))
}

// javaThrowsClauseTypes returns each type named in a method/constructor
// `throws A, B, C` clause. The grammar exposes this as a `throws` child node
// whose named children are the thrown types.
func javaThrowsClauseTypes(declNode ts.Node, src []byte) []string {
	var throwsNode ts.Node
	for i := 0; i < int(declNode.ChildCount()); i++ {
		c := declNode.Child(i)
		if c != nil && c.Type() == "throws" {
			throwsNode = c
			break
		}
	}
	if throwsNode == nil {
		return nil
	}
	var out []string
	for i := 0; i < int(throwsNode.NamedChildCount()); i++ {
		c := throwsNode.NamedChild(i)
		if c == nil {
			continue
		}
		if t := lastIdent(strings.TrimSpace(nodeText(c, src))); t != "" {
			out = append(out, t)
		}
	}
	return out
}

// javaCatchTypes returns each caught type from a catch clause, expanding a
// multi-catch `catch (A | B e)` into {A, B}. The grammar nests a
// catch_formal_parameter → catch_type whose named children are the union types.
func javaCatchTypes(catchNode ts.Node, src []byte) []string {
	param := findFirstChildOfType(catchNode, "catch_formal_parameter")
	if param == nil {
		return nil
	}
	catchType := findFirstChildOfType(param, "catch_type")
	if catchType == nil {
		return nil
	}
	var out []string
	seen := map[string]bool{}
	for i := 0; i < int(catchType.NamedChildCount()); i++ {
		c := catchType.NamedChild(i)
		if c == nil {
			continue
		}
		t := lastIdent(strings.TrimSpace(nodeText(c, src)))
		if t != "" && !seen[t] {
			seen[t] = true
			out = append(out, t)
		}
	}
	return out
}

// findFirstChildOfType returns the first (depth-first) descendant of n whose
// node type equals typ, or nil. Used to reach catch_formal_parameter /
// catch_type without hard-coding child indices across grammar versions.
func findFirstChildOfType(n ts.Node, typ string) ts.Node {
	if n == nil {
		return nil
	}
	for i := 0; i < int(n.ChildCount()); i++ {
		c := n.Child(i)
		if c == nil {
			continue
		}
		if c.Type() == typ {
			return c
		}
		if found := findFirstChildOfType(c, typ); found != nil {
			return found
		}
	}
	return nil
}
