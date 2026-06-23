// tracing.go — Issue #3689 (epic #3628, area #11).
//
// Extracts OpenTelemetry distributed-tracing span-creation sites from Go
// function/method bodies and emits an INSTRUMENTS edge from the enclosing
// operation entity → a synthetic span stub ("span:<name>").
//
// Dominant OpenTelemetry Go idiom in scope:
//
//	ctx, span := tracer.Start(ctx, "db.query")
//
// The OTel Go API surface is `Tracer.Start(ctx context.Context, spanName string,
// opts ...SpanStartOption) (context.Context, Span)`. We match a call_expression
// whose function is a selector `<recv>.Start` and whose SECOND argument is a
// string literal (the span name). The first argument is always the context.
//
// We match on the `.Start` method name with a string-literal second argument
// rather than resolving the receiver type (which would need cross-file flow
// analysis). To keep this precise — `.Start` is a common method name — we
// additionally require the call to be in the two-result short-var form
// `ctx, span := <recv>.Start(...)` (the canonical OTel idiom returns
// (Context, Span)); a bare `x.Start("foo")` with one result is NOT matched.
//
// Honest-partial rule (#3689): when the second argument is not a string literal
// the span name is dynamic — we emit traced=true + dynamic=true and key the
// stub on the enclosing function name ("span:<fn>") rather than fabricating one.
//
// The walk is tolerant: a panic inside the traversal is recovered so the
// primary extraction pipeline is unaffected.
package golang

import (
	"strconv"

	"github.com/cajasmota/grafel/internal/treesitter/ts"

	"github.com/cajasmota/grafel/internal/types"
)

// goSpanHit captures one OpenTelemetry span-creation site in a Go function body.
type goSpanHit struct {
	spanName string // static span name; "" when dynamic
	line     int    // 1-indexed source line of the call
	dynamic  bool   // true when the span name is not a string literal
}

// extractGoSpanHits walks a function/method body and returns one goSpanHit per
// `<recv>.Start(ctx, "name")` OpenTelemetry span-creation call found in the
// canonical two-result short-var form.
func extractGoSpanHits(body ts.Node, src []byte) []goSpanHit {
	if body == nil {
		return nil
	}
	var hits []goSpanHit
	seen := make(map[string]bool)

	defer func() { _ = recover() }()

	// Walk short var declarations of the form `ctx, span := tracer.Start(...)`.
	// In tree-sitter Go these are short_var_declaration nodes whose right side
	// is an expression_list containing the call_expression.
	for _, decl := range findAll(body, "short_var_declaration") {
		left := decl.ChildByFieldName("left")
		right := decl.ChildByFieldName("right")
		if left == nil || right == nil {
			continue
		}
		// Canonical OTel form returns (Context, Span) — exactly two LHS targets.
		if left.NamedChildCount() != 2 {
			continue
		}
		call := firstCallExpr(right)
		if call == nil {
			continue
		}
		h, ok := goSpanFromStartCall(call, src)
		if !ok {
			continue
		}
		key := h.spanName + "|" + strconv.FormatBool(h.dynamic)
		if h.dynamic {
			key += "|" + strconv.Itoa(h.line)
		}
		if seen[key] {
			continue
		}
		seen[key] = true
		hits = append(hits, h)
	}
	return hits
}

// firstCallExpr returns the first call_expression directly inside node's
// expression list (or node itself when it is a call_expression). Returns nil
// when no direct call_expression is present.
func firstCallExpr(node ts.Node) ts.Node {
	if node == nil {
		return nil
	}
	if node.Type() == "call_expression" {
		return node
	}
	for i := 0; i < int(node.NamedChildCount()); i++ {
		ch := node.NamedChild(i)
		if ch != nil && ch.Type() == "call_expression" {
			return ch
		}
	}
	return nil
}

// goSpanFromStartCall inspects a call_expression and returns a goSpanHit when it
// is an OpenTelemetry `<recv>.Start(ctx, <name>)` span-creation call.
func goSpanFromStartCall(call ts.Node, src []byte) (goSpanHit, bool) {
	if call == nil || call.Type() != "call_expression" {
		return goSpanHit{}, false
	}
	fn := call.ChildByFieldName("function")
	args := call.ChildByFieldName("arguments")
	if fn == nil || args == nil || fn.Type() != "selector_expression" {
		return goSpanHit{}, false
	}
	field := fn.ChildByFieldName("field")
	if field == nil || nodeText(field, src) != "Start" {
		return goSpanHit{}, false
	}
	// OTel Tracer.Start takes (ctx, name, ...opts): the span name is the second
	// argument. Require at least two args.
	if args.NamedChildCount() < 2 {
		return goSpanHit{}, false
	}
	nameArg := args.NamedChild(1)
	line := int(call.StartPoint().Row) + 1
	if nameArg != nil && nameArg.Type() == "interpreted_string_literal" {
		name := goStripStringLiteral(nodeText(nameArg, src))
		if name != "" {
			return goSpanHit{spanName: name, line: line}, true
		}
	}
	// Dynamic / non-literal span name → honest-partial flag, no fabricated name.
	return goSpanHit{dynamic: true, line: line}, true
}

// goTracingSpanEdges returns the INSTRUMENTS edges for every OpenTelemetry
// span-creation site found in body. enclosingName is the bare function/method
// name used to key the synthetic stub for dynamic span names ("span:<fn>").
func goTracingSpanEdges(body ts.Node, enclosingName, fromID string, src []byte) []types.RelationshipRecord {
	hits := extractGoSpanHits(body, src)
	if len(hits) == 0 {
		return nil
	}
	out := make([]types.RelationshipRecord, 0, len(hits))
	for _, h := range hits {
		props := map[string]string{
			"library": "opentelemetry",
			"api":     "tracer.Start",
			"line":    strconv.Itoa(h.line),
			"traced":  "true",
		}
		var toID string
		if h.dynamic {
			props["dynamic"] = "true"
			toID = "span:" + enclosingName
		} else {
			props["span_name"] = h.spanName
			toID = "span:" + h.spanName
		}
		out = append(out, types.RelationshipRecord{
			FromID:     fromID,
			ToID:       toID,
			Kind:       string(types.RelationshipKindInstruments),
			Properties: props,
		})
	}
	return out
}
