// exception_flow.go — supplemental pass that emits THROWS / CATCHES edges from
// PHP methods / functions to a shared SCOPE.ExceptionType node (epic #3628).
// It lets the graph answer "what can this operation throw?" (outbound THROWS)
// and "where is NotFoundException handled?" (inbound CATCHES), keeping
// error-contract parity with the C# / Java / Python / Go / JS flagships. Node /
// edge construction is delegated to extractor.EmitExceptionEdges so the
// convergence invariant (identical type names → ONE node) is identical
// everywhere.
//
// Detected shapes (typed only — honest-partial, precision-first):
//
//	throw new NotFoundException(...)          → THROWS NotFoundException
//	throw new \App\Errors\Boom(...)           → THROWS Boom          (qualified → bare)
//	} catch (NotFoundException $e) { ... }    → CATCHES NotFoundException
//	} catch (IOException | TimeoutException $e) { ... }
//	                                          → CATCHES IOException + CATCHES TimeoutException
//	                                            (PHP 8 union multi-catch — ONE edge per type)
//	} catch (\Throwable $e) { ... }           → CATCHES Throwable    (qualified → bare)
//
// Deliberately NOT recorded (would mislead error-contract analysis):
//
//	throw $e;          (re-throw of a variable — carries no NEW type)
//	throw $this->mk(); (computed throw — the thrown value is dynamic)
//
// Catch-all convention: PHP `catch` ALWAYS declares at least one type (there is
// no untyped `catch { }` form — the type_list is mandatory in the grammar), so
// unlike C#/JS there is no bare-catch to drop. The broad guards
// `catch (\Throwable $e)` and `catch (\Exception $e)` ARE statically-recoverable
// typed catches, so they are recorded as CATCHES Throwable / CATCHES Exception
// — honest and useful: "this handler swallows everything" is a real, queryable
// error-contract fact. This mirrors the flagship decision to record the
// declared catch TYPE verbatim.
//
// FromName mirrors the walk() operation naming (`<Class>.<method>` for methods,
// bare leaf for free functions) so THROWS / CATCHES edges attach to the same
// SCOPE.Operation host the rest of the extractor emits.

package php

import (
	"strings"

	"github.com/cajasmota/grafel/internal/treesitter/ts"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
)

// phpExceptionLeaf collapses a PHP type token to its bare class name by taking
// the last `\`-separated segment — the namespace separator the shared
// extractor.NormalizeExceptionType (which only knows `.` / `::`) does not
// handle. `\App\Errors\Boom` → "Boom", `\Throwable` → "Throwable",
// `NotFoundException` → "NotFoundException". NormalizeExceptionType then
// validates the leaf is a single identifier and drops anything dynamic.
func phpExceptionLeaf(raw string) string {
	raw = strings.TrimSpace(raw)
	if i := strings.LastIndex(raw, "\\"); i >= 0 {
		raw = raw[i+1:]
	}
	return raw
}

// emitExceptionFlowEdges scans every method / function body (and file scope)
// for `throw new` and typed `catch` shapes and appends exception-type entities
// + THROWS / CATCHES edges to *entities.
//
// (*entities)[0] MUST be the file entity. Mutates *entities in place. Safe with
// nil / empty input.
func emitExceptionFlowEdges(root ts.Node, file extractor.FileInput, entities *[]types.EntityRecord) {
	if root == nil || entities == nil || len(*entities) == 0 {
		return
	}
	src := file.Content

	var edges []extractor.ExceptionEdge

	// enclosingClass is the innermost class/interface/trait name (matching
	// walk(), which dots only the immediate parent). enclosing is the host
	// SCOPE.Operation Name (`<class>.<method>` for methods, bare leaf for free
	// functions), or "" at file scope.
	var walk func(n ts.Node, enclosingClass, enclosing string)
	walk = func(n ts.Node, enclosingClass, enclosing string) {
		if n == nil {
			return
		}
		switch n.Type() {
		case "class_declaration", "interface_declaration", "trait_declaration":
			cls := childFieldText(n, "name", src)
			for i := 0; i < int(n.ChildCount()); i++ {
				walk(n.Child(i), cls, enclosing)
			}
			return
		case "method_declaration":
			leaf := childFieldText(n, "name", src)
			name := leaf
			if enclosingClass != "" && leaf != "" {
				name = enclosingClass + "." + leaf
			}
			for i := 0; i < int(n.ChildCount()); i++ {
				walk(n.Child(i), enclosingClass, name)
			}
			return
		case "function_definition":
			leaf := childFieldText(n, "name", src)
			for i := 0; i < int(n.ChildCount()); i++ {
				walk(n.Child(i), enclosingClass, leaf)
			}
			return
		case "throw_expression":
			if t := phpThrowType(n, src); t != "" {
				edges = append(edges, extractor.ExceptionEdge{
					Type: t, FromName: enclosing, Pattern: "throw_new",
				})
			}
		case "catch_clause":
			for _, t := range phpCatchTypes(n, src) {
				edges = append(edges, extractor.ExceptionEdge{
					Type: t, FromName: enclosing, Catch: true, Pattern: "catch_type",
				})
			}
		}
		for i := 0; i < int(n.ChildCount()); i++ {
			walk(n.Child(i), enclosingClass, enclosing)
		}
	}
	walk(root, "", "")

	extractor.EmitExceptionEdges(entities, "php", edges)
}

// phpThrowType returns the constructed exception type for `throw new X(...)` /
// `throw new \Ns\X(...)`, or "" for a re-throw of a variable (`throw $e;`) or a
// computed throw (`throw mk();`) which carries no NEW static type. The raw
// (possibly qualified) type text is returned verbatim; NormalizeExceptionType
// collapses it to the bare class name.
//
// Grammar: a `throw_expression` has an `object_creation_expression` child only
// when the thrown value is `new X(...)`. The type leaf is the first
// `name` / `qualified_name` child after the `new` keyword (the `type` field is
// not consistently set across grammar revisions).
func phpThrowType(throwNode ts.Node, src []byte) string {
	creation := findFirstChildOfType(throwNode, "object_creation_expression")
	if creation == nil {
		return "" // `throw $e;` or `throw mk();` — no NEW type
	}
	for i := 0; i < int(creation.ChildCount()); i++ {
		ch := creation.Child(i)
		if ch.Type() == "name" || ch.Type() == "qualified_name" {
			return phpExceptionLeaf(string(src[ch.StartByte():ch.EndByte()]))
		}
	}
	return ""
}

// phpCatchTypes returns every caught type declared in a catch clause's
// `type_list`. PHP 8 union multi-catch (`catch (A | B | C $e)`) lists multiple
// `named_type` children — one CATCHES per type so each guarded type converges
// on its own exception node. Each `named_type` wraps either a `name` (bare) or
// a `qualified_name` (`\Ns\X` / `\Throwable`); the raw text is returned
// verbatim and NormalizeExceptionType collapses it to the bare class name.
//
// PHP has no untyped `catch { }` (the type_list is mandatory), so this never
// needs to drop a bare catch — every clause yields at least one type.
func phpCatchTypes(catchNode ts.Node, src []byte) []string {
	typeList := catchNode.ChildByFieldName("type")
	if typeList == nil {
		typeList = findFirstChildOfType(catchNode, "type_list")
	}
	if typeList == nil {
		return nil
	}
	var out []string
	for i := 0; i < int(typeList.ChildCount()); i++ {
		ch := typeList.Child(i)
		if ch.Type() != "named_type" {
			continue // skip the `|` separators
		}
		// named_type wraps a single name / qualified_name leaf.
		for j := 0; j < int(ch.ChildCount()); j++ {
			leaf := ch.Child(j)
			if leaf.Type() == "name" || leaf.Type() == "qualified_name" {
				out = append(out, phpExceptionLeaf(string(src[leaf.StartByte():leaf.EndByte()])))
				break
			}
		}
	}
	return out
}
