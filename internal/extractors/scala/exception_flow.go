// exception_flow.go — supplemental pass that emits THROWS / CATCHES edges from
// Scala functions / methods to a shared SCOPE.ExceptionType node (epic #3628).
// It lets the graph answer "what can this function throw?" (outbound THROWS)
// and "where is NotFoundException handled?" (inbound CATCHES), with the SAME
// convergence-node shape as the Go / Java / JS-TS / Python flagships:
// node/edge construction lives in extractor.EmitExceptionEdges, so a Scala
// `throw new NotFoundException(...)` and a Java `throw new NotFoundException()`
// resolve to ONE exception:NotFoundException node.
//
// Detected shapes (typed only — honest-partial, precision-first):
//
//	throw new NotFoundException(...)                  → THROWS NotFoundException
//	try { ... } catch { case e: SqlException => ... } → CATCHES SqlException
//	  ... case _: TimeoutException => ...             → CATCHES TimeoutException
//	  ... case e: java.io.IOException => ...           → CATCHES IOException
//	Try { ... }.recover { case e: IllegalState => ... }→ CATCHES IllegalState
//	  (also .recoverWith { case e: X => ... })
//
// Deliberately NOT recorded (a single wrong edge would mislead error-contract
// analysis):
//
//	throw e                       — re-throw of a variable carries no NEW type
//	catch { case _ => ... }       — catch-all, no static type
//	catch { case NonFatal(e) => } — extractor pattern, not a typed exception
//
// FromName is the bare function leaf name so edges attach to the same
// SCOPE.Operation host that buildOperation emits (Scala operations are named by
// their unqualified def name, unlike Java's Class.method).

package scala

import (
	"strings"

	"github.com/cajasmota/grafel/internal/treesitter/ts"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
)

// emitExceptionFlowEdges scans every function for throw / catch / recover
// shapes and appends exception-type entities + THROWS / CATCHES edges.
//
// entities[0] MUST be the file entity. Mutates *entities in place. Safe with
// nil / empty input.
func emitExceptionFlowEdges(root ts.Node, file extractor.FileInput, entities *[]types.EntityRecord) {
	if root == nil || entities == nil || len(*entities) == 0 {
		return
	}
	src := file.Content

	var edges []extractor.ExceptionEdge

	var walk func(n ts.Node, enclosingFn string)
	walk = func(n ts.Node, enclosingFn string) {
		if n == nil {
			return
		}
		switch n.Type() {
		case "function_definition", "function_declaration":
			// The host name is the bare leaf def name (extractName), matching
			// buildOperation so the THROWS/CATCHES edge attaches to that
			// SCOPE.Operation entity. Nested defs re-bind enclosingFn.
			if name := extractName(n, src); name != "" {
				enclosingFn = name
			}
		case "throw_expression":
			if t := scalaThrowType(n, src); t != "" {
				edges = append(edges, extractor.ExceptionEdge{
					Type: t, FromName: enclosingFn, Pattern: "throw_new",
				})
			}
		case "catch_clause":
			for _, t := range scalaCaseBlockCatchTypes(n, src) {
				edges = append(edges, extractor.ExceptionEdge{
					Type: t, FromName: enclosingFn, Catch: true, Pattern: "catch_type",
				})
			}
		case "call_expression":
			// `expr.recover { case e: X => ... }` / `.recoverWith { ... }` —
			// the handler case_block is a direct child of the call_expression
			// whose field_expression trailing identifier is recover(With).
			if scalaIsRecoverCall(n, src) {
				for _, t := range scalaCaseBlockCatchTypes(n, src) {
					edges = append(edges, extractor.ExceptionEdge{
						Type: t, FromName: enclosingFn, Catch: true, Pattern: "recover",
					})
				}
			}
		}
		for i := 0; i < int(n.ChildCount()); i++ {
			walk(n.Child(i), enclosingFn)
		}
	}
	walk(root, "")

	extractor.EmitExceptionEdges(entities, "scala", edges)
}

// scalaThrowType returns the constructed exception type for
// `throw new X(...)` (the throw_expression's instance_expression child names
// the type), or "" for a re-throw of a variable (`throw e`) — which carries no
// NEW type — or any non-`new` thrown expression.
func scalaThrowType(throwNode ts.Node, src []byte) string {
	for i := 0; i < int(throwNode.ChildCount()); i++ {
		c := throwNode.Child(i)
		if c == nil || c.Type() != "instance_expression" {
			continue
		}
		// instance_expression: `new` <type_identifier|stable_type_identifier> [arguments]
		if t := scalaTypeNodeText(c, src); t != "" {
			return t
		}
	}
	return ""
}

// scalaIsRecoverCall reports whether a call_expression is a `.recover` /
// `.recoverWith` invocation that carries a case_block handler — the Try / Future
// error-recovery idiom whose typed cases catch specific exceptions.
func scalaIsRecoverCall(call ts.Node, src []byte) bool {
	if call.ChildCount() == 0 {
		return false
	}
	// Must carry a case_block (the `{ case e: X => ... }` handler).
	hasCaseBlock := false
	for i := 0; i < int(call.ChildCount()); i++ {
		if call.Child(i).Type() == "case_block" {
			hasCaseBlock = true
			break
		}
	}
	if !hasCaseBlock {
		return false
	}
	first := call.Child(0)
	if first.Type() != "field_expression" {
		return false
	}
	// Trailing identifier of the field_expression is the method name.
	var last ts.Node
	for i := 0; i < int(first.ChildCount()); i++ {
		if first.Child(i).Type() == "identifier" {
			last = first.Child(i)
		}
	}
	if last == nil {
		return false
	}
	method := string(src[last.StartByte():last.EndByte()])
	return method == "recover" || method == "recoverWith"
}

// scalaCaseBlockCatchTypes returns each typed exception name caught by the
// case_block nested under a catch_clause or recover call. Only `case e: T =>`
// and `case _: T =>` (typed_pattern) contribute a type; catch-all `case _ =>`
// and extractor patterns `case NonFatal(e) =>` / `case Foo(x) =>` carry no
// static exception type and are skipped — precision over recall. Qualified
// types (`java.io.IOException`) are normalized to their bare class downstream
// by extractor.NormalizeExceptionType.
func scalaCaseBlockCatchTypes(parent ts.Node, src []byte) []string {
	caseBlock := firstChildOfType(parent, "case_block")
	if caseBlock == nil {
		return nil
	}
	var out []string
	seen := map[string]bool{}
	for i := 0; i < int(caseBlock.ChildCount()); i++ {
		cc := caseBlock.Child(i)
		if cc == nil || cc.Type() != "case_clause" {
			continue
		}
		t := scalaTypedPatternType(cc, src)
		if t == "" || seen[t] {
			continue
		}
		seen[t] = true
		out = append(out, t)
	}
	return out
}

// scalaTypedPatternType returns the type token of a case_clause's
// typed_pattern (`e: T` or `_: T`), or "" when the case is a catch-all
// (`case _ =>`), a stable-identifier value pattern, or an extractor pattern
// (`case NonFatal(e) =>`) — none of which name a statically-caught exception
// type.
func scalaTypedPatternType(caseClause ts.Node, src []byte) string {
	tp := firstChildOfType(caseClause, "typed_pattern")
	if tp == nil {
		return ""
	}
	// typed_pattern children: (identifier|wildcard) ":" (type_identifier|
	// stable_type_identifier). The type is the child following the ":".
	sawColon := false
	for i := 0; i < int(tp.ChildCount()); i++ {
		c := tp.Child(i)
		if c == nil {
			continue
		}
		if c.Type() == ":" {
			sawColon = true
			continue
		}
		if !sawColon {
			continue
		}
		if t := scalaTypeNodeText(c, src); t != "" {
			return t
		}
	}
	return ""
}

// scalaTypeNodeText returns the bare type name for a type-bearing node. A
// `type_identifier` yields its verbatim text; a `stable_type_identifier`
// (`java.io.IOException`) yields its full dotted text (normalized to the bare
// leaf downstream). An `instance_expression` (`new X(...)`) yields the type of
// its first type-bearing child. Returns "" for any other node.
func scalaTypeNodeText(n ts.Node, src []byte) string {
	switch n.Type() {
	case "type_identifier", "stable_type_identifier":
		return strings.TrimSpace(string(src[n.StartByte():n.EndByte()]))
	case "instance_expression":
		for i := 0; i < int(n.ChildCount()); i++ {
			c := n.Child(i)
			if c == nil {
				continue
			}
			if c.Type() == "type_identifier" || c.Type() == "stable_type_identifier" {
				return strings.TrimSpace(string(src[c.StartByte():c.EndByte()]))
			}
			// generic_type leaf (`new BoomException[T](...)`).
			if c.Type() == "generic_type" {
				if leaf := firstChildOfType(c, "type_identifier"); leaf != nil {
					return strings.TrimSpace(string(src[leaf.StartByte():leaf.EndByte()]))
				}
			}
		}
	}
	return ""
}

// firstChildOfType returns the first depth-first descendant of n whose Type()
// equals typ, or nil. Used to reach case_block / typed_pattern without
// hard-coding child indices across grammar versions.
func firstChildOfType(n ts.Node, typ string) ts.Node {
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
		if found := firstChildOfType(c, typ); found != nil {
			return found
		}
	}
	return nil
}
