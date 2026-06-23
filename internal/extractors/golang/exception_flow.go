// exception_flow.go — supplemental pass that emits THROWS / CATCHES edges from
// Go functions / methods to a shared SCOPE.ExceptionType node for NAMED error
// values (epic #3628). Go has no exceptions; the error-contract signal is the
// set of named sentinel errors a function returns vs. the ones it tests for.
//
// Detected shapes (NAMED errors only — precision-first, honest-partial):
//
//	return ErrNotFound                      → THROWS ErrNotFound
//	return nil, ErrNotFound                 → THROWS ErrNotFound
//	return fmt.Errorf("x: %w", ErrNotFound) → THROWS ErrNotFound  (wrapped sentinel)
//	if errors.Is(err, ErrNotFound) { }      → CATCHES ErrNotFound
//	errors.As(err, &target)                 → CATCHES <TargetType>
//
// Intentionally DROPPED (would fabricate a contract that isn't named):
//
//	return errors.New("not found")          (anonymous inline error)
//	return fmt.Errorf("bad input: %d", n)   (bare formatted error, no %w sentinel)
//	return err                              (opaque pass-through of a local var)
//
// "Named" means a package-level / exported sentinel identifier following Go's
// `ErrXxx` convention (or any qualified `pkg.ErrXxx`). The local error variable
// `err` itself is NOT a type and is never emitted. Node/edge construction
// (convergence on one node per name) lives in extractor.EmitExceptionEdges.

package golang

import (
	"strings"

	"github.com/cajasmota/grafel/internal/treesitter/ts"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
)

// emitExceptionFlowEdges scans every function / method body for named-error
// return / errors.Is / errors.As shapes and appends exception-type entities +
// THROWS / CATCHES edges.
//
// records[0] MUST be the file entity. Mutates *records in place. Safe with
// nil / empty input.
func emitExceptionFlowEdges(root ts.Node, src []byte, records *[]types.EntityRecord) {
	if root == nil || records == nil || len(*records) == 0 {
		return
	}

	var edges []extractor.ExceptionEdge

	var walk func(n ts.Node, enclosing string)
	walk = func(n ts.Node, enclosing string) {
		if n == nil {
			return
		}
		switch n.Type() {
		case "function_declaration":
			name := ""
			if nn := n.ChildByFieldName("name"); nn != nil {
				name = nodeText(nn, src)
			}
			if body := n.ChildByFieldName("body"); body != nil {
				for i := 0; i < int(body.ChildCount()); i++ {
					walk(body.Child(i), name)
				}
			}
			return
		case "method_declaration":
			leaf := ""
			if nn := n.ChildByFieldName("name"); nn != nil {
				leaf = nodeText(nn, src)
			}
			recv := receiverTypeName(n.ChildByFieldName("receiver"), src)
			name := leaf
			if recv != "" {
				name = recv + "." + leaf
			}
			if body := n.ChildByFieldName("body"); body != nil {
				for i := 0; i < int(body.ChildCount()); i++ {
					walk(body.Child(i), name)
				}
			}
			return
		case "return_statement":
			for _, t := range goReturnedNamedErrors(n, src) {
				edges = append(edges, extractor.ExceptionEdge{
					Type: t, FromName: enclosing, Pattern: "return_named",
				})
			}
		case "call_expression":
			if t, catch := goErrorsIsAsType(n, src); t != "" {
				edges = append(edges, extractor.ExceptionEdge{
					Type: t, FromName: enclosing, Catch: catch, Pattern: "errors_is",
				})
			}
		}
		for i := 0; i < int(n.ChildCount()); i++ {
			walk(n.Child(i), enclosing)
		}
	}
	walk(root, "")

	extractor.EmitExceptionEdges(records, "go", edges)
}

// goReturnedNamedErrors returns each NAMED sentinel error returned by a
// return_statement: a bare `return ErrX` operand, or a sentinel wrapped in a
// `fmt.Errorf(..., %w, ErrX)` call. Anonymous `errors.New(...)` / bare
// `fmt.Errorf` and opaque `return err` pass-throughs yield nothing.
func goReturnedNamedErrors(retNode ts.Node, src []byte) []string {
	var out []string
	seen := map[string]bool{}
	add := func(name string) {
		if name != "" && !seen[name] {
			seen[name] = true
			out = append(out, name)
		}
	}

	// A return_statement's operands are in an expression_list (or a single
	// expression). Inspect each operand.
	var operands []ts.Node
	for i := 0; i < int(retNode.NamedChildCount()); i++ {
		c := retNode.NamedChild(i)
		if c == nil {
			continue
		}
		if c.Type() == "expression_list" {
			for j := 0; j < int(c.NamedChildCount()); j++ {
				if el := c.NamedChild(j); el != nil {
					operands = append(operands, el)
				}
			}
		} else {
			operands = append(operands, c)
		}
	}

	for _, op := range operands {
		switch op.Type() {
		case "identifier":
			if name := goSentinelName(nodeText(op, src)); name != "" {
				add(name)
			}
		case "selector_expression":
			// pkg.ErrNotFound — trailing field must look like a sentinel.
			if field := op.ChildByFieldName("field"); field != nil {
				if name := goSentinelName(nodeText(field, src)); name != "" {
					add(name)
				}
			}
		case "call_expression":
			// fmt.Errorf("...: %w", ErrNotFound) — record every NAMED sentinel
			// argument; the anonymous-format-string arg is not a sentinel and
			// is skipped by goSentinelName.
			for _, name := range goWrappedSentinels(op, src) {
				add(name)
			}
		}
	}
	return out
}

// goWrappedSentinels returns the named sentinel identifiers passed as arguments
// to a fmt.Errorf / errors.Wrap-style call (the `%w` operands). Only emitted
// for calls whose callee is `fmt.Errorf` or `errors.Wrap*`; a plain
// `errors.New("...")` has no sentinel argument and yields nothing.
func goWrappedSentinels(call ts.Node, src []byte) []string {
	fn := call.ChildByFieldName("function")
	if fn == nil || fn.Type() != "selector_expression" {
		return nil
	}
	callee := strings.TrimSpace(nodeText(fn, src))
	if callee != "fmt.Errorf" && !strings.HasPrefix(callee, "errors.Wrap") {
		return nil
	}
	args := call.ChildByFieldName("arguments")
	if args == nil {
		return nil
	}
	var out []string
	for i := 0; i < int(args.NamedChildCount()); i++ {
		a := args.NamedChild(i)
		if a == nil {
			continue
		}
		switch a.Type() {
		case "identifier":
			if name := goSentinelName(nodeText(a, src)); name != "" {
				out = append(out, name)
			}
		case "selector_expression":
			if field := a.ChildByFieldName("field"); field != nil {
				if name := goSentinelName(nodeText(field, src)); name != "" {
					out = append(out, name)
				}
			}
		}
	}
	return out
}

// goErrorsIsAsType returns the named error type/value tested by errors.Is /
// errors.As, plus catch=true. errors.Is(err, ErrX) → ("ErrX", true);
// errors.As(err, &target) → ("<TargetType>", true) when the target's type is
// statically recoverable from a sentinel-named identifier. Returns ("", false)
// for non-matching calls.
func goErrorsIsAsType(call ts.Node, src []byte) (string, bool) {
	fn := call.ChildByFieldName("function")
	if fn == nil || fn.Type() != "selector_expression" {
		return "", false
	}
	callee := strings.TrimSpace(nodeText(fn, src))
	if callee != "errors.Is" && callee != "errors.As" {
		return "", false
	}
	args := call.ChildByFieldName("arguments")
	if args == nil || args.NamedChildCount() < 2 {
		return "", false
	}
	// Second arg is the target sentinel (errors.Is) or &target (errors.As).
	target := args.NamedChild(1)
	if target == nil {
		return "", false
	}
	// errors.As passes a pointer: &target — unwrap the unary_expression.
	if target.Type() == "unary_expression" {
		if inner := target.NamedChild(0); inner != nil {
			target = inner
		}
	}
	switch target.Type() {
	case "identifier":
		if name := goSentinelName(nodeText(target, src)); name != "" {
			return name, true
		}
	case "selector_expression":
		if field := target.ChildByFieldName("field"); field != nil {
			if name := goSentinelName(nodeText(field, src)); name != "" {
				return name, true
			}
		}
	}
	return "", false
}

// goSentinelName returns name when it is a NAMED Go error sentinel by
// convention — an exported/sentinel identifier matching `Err...` (e.g.
// ErrNotFound, ErrConflict) — or "" otherwise. The local error variable `err`
// and lowercase locals are NOT sentinels (they hold opaque values, not a named
// type), so a `return err` pass-through never fabricates an edge.
func goSentinelName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	// Canonical sentinel form: starts with "Err" (exported) or "err" followed
	// by an uppercase letter (unexported package sentinel, e.g. errNotFound).
	if strings.HasPrefix(name, "Err") {
		return name
	}
	if strings.HasPrefix(name, "err") && len(name) > 3 {
		c := name[3]
		if c >= 'A' && c <= 'Z' {
			return name
		}
	}
	return ""
}
