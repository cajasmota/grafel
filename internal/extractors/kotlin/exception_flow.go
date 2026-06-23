// exception_flow.go — supplemental pass that emits THROWS / CATCHES edges from
// Kotlin functions to a shared SCOPE.ExceptionType node (epic #3628). It lets
// the graph answer "what can this function raise?" (outbound THROWS) and
// "where is NotFoundException handled?" (inbound CATCHES), cross-language
// consistent with the Java / Python / Go / JS flagships — same entity Kind
// (SCOPE.ExceptionType, subtype="exception_type", synthetic "<exception>"
// SourceFile so identical type names converge to ONE node) and same edge Kinds
// (THROWS / CATCHES), all built by extractor.EmitExceptionEdges.
//
// Detected shapes (typed only — honest-partial, precision-first):
//
//	throw NotFoundException(...)                  → THROWS NotFoundException
//	throw ResponseStatusException(HttpStatus...)  → THROWS ResponseStatusException
//	try { } catch (e: SqlException) { }           → CATCHES SqlException
//	@ExceptionHandler(NotFoundException::class)
//	  fun handle(e): ResponseEntity<..> { }       → CATCHES NotFoundException
//	  (Spring @ControllerAdvice / @RestControllerAdvice handler method)
//	install(StatusPages) {
//	  exception<AuthException> { call, cause -> } } → CATCHES AuthException
//	  (Ktor StatusPages exception handler)
//
// Intentionally DROPPED (would mislead error-contract analysis — a single
// wrong THROWS/CATCHES edge corrupts the contract):
//
//	throw e                       (bare re-throw of a variable — no NEW type)
//	throw makeError()             (factory call — dynamic/computed type)
//	try { } finally { }           (no catch → no handler edge)
//	@ExceptionHandler  fun h()     (no class argument → no resolvable type)
//
// Node/edge construction (convergence + dedup) lives in
// extractor.EmitExceptionEdges; this file owns only the Kotlin CST detection.

package kotlin

import (
	"strings"

	"github.com/cajasmota/grafel/internal/treesitter/ts"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
)

// emitExceptionFlowEdges scans every function for throw / typed-catch /
// @ExceptionHandler / Ktor StatusPages shapes and appends exception-type
// entities + THROWS / CATCHES edges.
//
// entities[0] MUST be the file entity. The enclosing-function Name used as
// each edge's FromName matches the bare function Name emitted by
// buildOperation, so edges attach to the function entity; throws/catches at
// file scope (or in a function whose entity was not emitted) fall back to the
// file entity inside EmitExceptionEdges. Mutates *entities in place. Safe with
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
		case "function_declaration":
			fn := kotlinFunctionName(n, src)
			// A Spring @ExceptionHandler(X::class) method is itself the handler
			// for X — emit CATCHES on the method entity, mirroring a typed catch.
			for _, t := range kotlinExceptionHandlerTypes(n, src) {
				edges = append(edges, extractor.ExceptionEdge{
					Type: t, FromName: fn, Catch: true, Pattern: "exception_handler",
				})
			}
			for i := 0; i < int(n.ChildCount()); i++ {
				walk(n.Child(i), fn)
			}
			return
		case "jump_expression":
			if t := kotlinThrowType(n, src); t != "" {
				edges = append(edges, extractor.ExceptionEdge{
					Type: t, FromName: enclosingFn, Pattern: "throw_new",
				})
			}
		case "catch_block":
			if t := kotlinCatchType(n, src); t != "" {
				edges = append(edges, extractor.ExceptionEdge{
					Type: t, FromName: enclosingFn, Catch: true, Pattern: "catch_type",
				})
			}
		case "call_expression":
			// Ktor StatusPages: `exception<AuthException> { ... }` — a call to
			// `exception` with a single type argument registers a handler for
			// that exception type. Emit CATCHES.
			if t := kotlinKtorStatusPagesType(n, src); t != "" {
				edges = append(edges, extractor.ExceptionEdge{
					Type: t, FromName: enclosingFn, Catch: true, Pattern: "ktor_status_pages",
				})
			}
		}
		for i := 0; i < int(n.ChildCount()); i++ {
			walk(n.Child(i), enclosingFn)
		}
	}
	walk(root, "")

	extractor.EmitExceptionEdges(entities, "kotlin", edges)
}

// kotlinFunctionName returns the bare declared name of a function_declaration,
// matching the Name buildOperation emits (Kotlin function entities are not
// class-qualified), or "" so the edge falls back to the file entity.
func kotlinFunctionName(fn ts.Node, src []byte) string {
	if name := strings.TrimSpace(childFieldText(fn, "name", src)); name != "" {
		return name
	}
	return strings.TrimSpace(firstChildOfType(fn, src, "simple_identifier"))
}

// kotlinThrowType returns the constructed exception class for a
// `throw <Type>(...)` jump expression, or "" for a non-throw jump
// (`return`/`break`/`continue`), a bare re-throw of a variable (`throw e`), or
// a factory call (`throw makeError()` — lowercase callee). Only an explicit
// `throw <Constructor>(...)` whose callee is a PascalCase type token is taken.
func kotlinThrowType(jump ts.Node, src []byte) string {
	// jump_expression covers return/throw/break/continue. Require a leading
	// `throw` keyword token among the (unnamed) children.
	if !jumpIsThrow(jump) {
		return ""
	}
	// The thrown expression is the first named child (a call_expression for
	// `throw X(...)`).
	var expr ts.Node
	for i := 0; i < int(jump.NamedChildCount()); i++ {
		if c := jump.NamedChild(i); c != nil {
			expr = c
			break
		}
	}
	if expr == nil || expr.Type() != "call_expression" {
		return "" // `throw e` (re-throw of a variable) — no NEW type token
	}
	// The callee is the first child; accept a bare `simple_identifier`
	// constructor (`throw NotFoundException(...)`) or a dotted
	// `navigation_expression` (`throw pkg.Boom(...)` → trailing segment).
	callee := expr.NamedChild(0)
	if callee == nil {
		return ""
	}
	var name string
	switch callee.Type() {
	case "simple_identifier":
		name = strings.TrimSpace(nodeText(callee, src))
	case "navigation_expression":
		name = lastIdent(strings.TrimSpace(nodeText(callee, src)))
	default:
		return ""
	}
	// Precision guard: a Kotlin exception CLASS is PascalCase. A lowercase
	// callee is a factory function (`throw makeError()`), not a constructor —
	// drop so NormalizeExceptionType never fabricates a node.
	if !startsUpperKt(name) {
		return ""
	}
	return name
}

// jumpIsThrow reports whether a jump_expression is a `throw ...` (vs
// return/break/continue) by inspecting its leading keyword token.
func jumpIsThrow(jump ts.Node) bool {
	for i := 0; i < int(jump.ChildCount()); i++ {
		c := jump.Child(i)
		if c == nil {
			continue
		}
		if c.Type() == "throw" {
			return true
		}
		// First token decides; if it is a named expression child we have passed
		// the keyword without seeing `throw`.
		if c.IsNamed() {
			return false
		}
	}
	return false
}

// kotlinCatchType returns the caught exception type of a catch_block
// (`catch (e: SqlException) { ... }`), taken from its user_type child, or "".
// Kotlin catch clauses are single-type (no Java-style `A | B` multi-catch), so
// at most one type is returned.
func kotlinCatchType(catchBlock ts.Node, src []byte) string {
	ut := firstNamedChildOfType(catchBlock, "user_type")
	if ut == nil {
		return ""
	}
	return lastIdent(strings.TrimSpace(nodeText(ut, src)))
}

// kotlinExceptionHandlerTypes returns each exception type named in a Spring
// `@ExceptionHandler(A::class, B::class)` annotation on a function_declaration
// (the @ControllerAdvice / @RestControllerAdvice handler idiom). An
// @ExceptionHandler with no class argument (the type is then inferred from the
// method's parameter) yields none here — precision-first, no fabricated node.
func kotlinExceptionHandlerTypes(fn ts.Node, src []byte) []string {
	mods := firstNamedChildOfType(fn, "modifiers")
	if mods == nil {
		return nil
	}
	var out []string
	seen := map[string]bool{}
	for i := 0; i < int(mods.NamedChildCount()); i++ {
		ann := mods.NamedChild(i)
		if ann == nil || ann.Type() != "annotation" {
			continue
		}
		ci := firstNamedChildOfType(ann, "constructor_invocation")
		if ci == nil {
			continue
		}
		nameNode := firstNamedChildOfType(ci, "user_type")
		if nameNode == nil || lastIdent(strings.TrimSpace(nodeText(nameNode, src))) != "ExceptionHandler" {
			continue
		}
		args := firstNamedChildOfType(ci, "value_arguments")
		if args == nil {
			continue
		}
		// Each argument is `X::class` → callable_reference whose type_identifier
		// is the exception class.
		for j := 0; j < int(args.NamedChildCount()); j++ {
			arg := args.NamedChild(j)
			if arg == nil {
				continue
			}
			ref := firstNamedChildOfType(arg, "callable_reference")
			if ref == nil {
				continue
			}
			ti := firstNamedChildOfType(ref, "type_identifier")
			if ti == nil {
				continue
			}
			t := strings.TrimSpace(nodeText(ti, src))
			if t != "" && !seen[t] {
				seen[t] = true
				out = append(out, t)
			}
		}
	}
	return out
}

// kotlinKtorStatusPagesType returns the exception type of a Ktor StatusPages
// handler registration `exception<AuthException> { ... }`, or "" for any other
// call. The shape is a call_expression whose callee simple_identifier is
// `exception` and whose call_suffix carries exactly one type argument — the
// handled exception class.
func kotlinKtorStatusPagesType(call ts.Node, src []byte) string {
	callee := call.NamedChild(0)
	if callee == nil || callee.Type() != "simple_identifier" {
		return ""
	}
	if strings.TrimSpace(nodeText(callee, src)) != "exception" {
		return ""
	}
	suffix := firstNamedChildOfType(call, "call_suffix")
	if suffix == nil {
		return ""
	}
	typeArgs := firstNamedChildOfType(suffix, "type_arguments")
	if typeArgs == nil {
		return ""
	}
	ut := firstNamedChildOfType(typeArgs, "user_type")
	if ut == nil {
		return ""
	}
	return lastIdent(strings.TrimSpace(nodeText(ut, src)))
}

// firstNamedChildOfType returns the first (depth-first) descendant of n whose
// node type equals typ, or nil.
func firstNamedChildOfType(n ts.Node, typ string) ts.Node {
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
		if found := firstNamedChildOfType(c, typ); found != nil {
			return found
		}
	}
	return nil
}

// nodeText returns the source text spanned by a node.
func nodeText(n ts.Node, src []byte) string {
	if n == nil {
		return ""
	}
	return string(src[n.StartByte():n.EndByte()])
}

// lastIdent returns the trailing dotted/scoped segment of a token
// (`pkg.Boom` -> `Boom`, `a::B` -> `B`, `Boom` -> `Boom`), stripping any
// generic suffix (`List<X>` -> `List`).
func lastIdent(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '<'); i >= 0 {
		s = strings.TrimSpace(s[:i])
	}
	s = strings.ReplaceAll(s, "::", ".")
	if i := strings.LastIndexByte(s, '.'); i >= 0 {
		s = s[i+1:]
	}
	return strings.TrimSpace(s)
}

// startsUpperKt reports whether s begins with an ASCII uppercase letter (the
// PascalCase exception-class convention used to drop lowercase factory-call
// throws like `throw makeError()`).
func startsUpperKt(s string) bool {
	return s != "" && s[0] >= 'A' && s[0] <= 'Z'
}
