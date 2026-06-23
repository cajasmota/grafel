// tracing.go — Issue #3689 (epic #3628, area #11).
//
// Extracts OpenTelemetry distributed-tracing span-creation sites from JS/TS
// function/method/arrow bodies and stamps an INSTRUMENTS edge from the
// enclosing operation entity → a synthetic span stub ("span:<name>").
//
// Dominant OpenTelemetry JS/TS idioms in scope:
//
//	const span = tracer.startSpan('checkout');
//	tracer.startActiveSpan('db.query', (span) => { ... });
//
// We match a call_expression whose function is a member_expression
// `<recv>.startSpan` / `<recv>.startActiveSpan` and whose first argument is a
// string literal (the span name). startSpan/startActiveSpan are OTel-reserved
// Tracer API surface, so matching on the property name is precise; we do not
// resolve the receiver type (which would need cross-file flow analysis).
//
// Honest-partial rule (#3689): when the first argument is not a string literal
// the span name is dynamic — we emit traced=true + dynamic=true and key the
// stub on the enclosing function name ("span:<fn>") rather than fabricating one.
//
// The walk is tolerant: a panic inside the traversal is recovered so the
// primary extraction pipeline is unaffected.
package javascript

import (
	"strconv"

	"github.com/cajasmota/grafel/internal/treesitter/ts"

	"github.com/cajasmota/grafel/internal/types"
)

// jsSpanHit captures one OpenTelemetry span-creation site in a JS/TS body.
type jsSpanHit struct {
	spanName string // static span name; "" when dynamic
	api      string // "startSpan" | "startActiveSpan"
	line     int    // 1-indexed source line of the call
	dynamic  bool   // true when the span name is not a string literal
}

// jsTracingSpanMethods is the set of OpenTelemetry Tracer method names that
// create a span. These are OTel-reserved API names.
var jsTracingSpanMethods = map[string]bool{
	"startSpan":       true,
	"startActiveSpan": true,
}

// extractTracingSpanHits walks a function/method/arrow body and returns one
// jsSpanHit per OpenTelemetry span-creation call found.
func (x *extractor) extractTracingSpanHits(body ts.Node) []jsSpanHit {
	if body == nil {
		return nil
	}
	var hits []jsSpanHit
	seen := make(map[string]bool)

	defer func() { _ = recover() }()

	for _, call := range findAllNodes(body, "call_expression") {
		fn := call.ChildByFieldName("function")
		if fn == nil || fn.Type() != "member_expression" {
			continue
		}
		propNode := fn.ChildByFieldName("property")
		if propNode == nil {
			continue
		}
		method := x.nodeText(propNode)
		if !jsTracingSpanMethods[method] {
			continue
		}

		line := int(call.StartPoint().Row) + 1
		hit := jsSpanHit{api: method, line: line}

		args := call.ChildByFieldName("arguments")
		var nameNode ts.Node
		if args != nil {
			nameNode = firstMeaningfulArg(args)
		}
		if nameNode != nil && nameNode.Type() == "string" {
			name := stringLiteralValue(x.nodeText(nameNode))
			if name != "" {
				hit.spanName = name
			} else {
				hit.dynamic = true
			}
		} else {
			hit.dynamic = true
		}

		key := hit.api + "|" + hit.spanName + "|" + strconv.FormatBool(hit.dynamic)
		if hit.dynamic {
			key += "|" + strconv.Itoa(hit.line)
		}
		if seen[key] {
			continue
		}
		seen[key] = true
		hits = append(hits, hit)
	}
	return hits
}

// stampTracingSpans emits INSTRUMENTS edges on the last entity appended to
// x.entities for every OpenTelemetry span-creation site found in body. Called
// immediately after a function/method/arrow entity is emitted (alongside
// stampDiscriminators / stampBranchConditions).
func (x *extractor) stampTracingSpans(body ts.Node) {
	if body == nil || len(x.entities) == 0 {
		return
	}
	hits := x.extractTracingSpanHits(body)
	if len(hits) == 0 {
		return
	}
	last := &x.entities[len(x.entities)-1]
	enclosing := last.Name
	for _, h := range hits {
		props := map[string]string{
			"library": "opentelemetry",
			"api":     h.api,
			"line":    strconv.Itoa(h.line),
			"traced":  "true",
		}
		var toID string
		if h.dynamic {
			props["dynamic"] = "true"
			toID = "span:" + enclosing
		} else {
			props["span_name"] = h.spanName
			toID = "span:" + h.spanName
		}
		last.Relationships = append(last.Relationships, types.RelationshipRecord{
			ToID:       toID,
			Kind:       string(types.RelationshipKindInstruments),
			Properties: props,
		})
	}
}
