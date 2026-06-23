// observability.go — Issue (epic #3628, area #11): non-OpenTelemetry
// observability/instrumentation breadth for JS/TS.
//
// Extends the wave-5 INSTRUMENTS edge (OpenTelemetry, #3689, tracing.go) to the
// dominant non-OTel JS/TS instrumentation libraries, stamping the SAME edge
// contract used by tracing.go / the Python+Go observability.go passes:
//
//	FROM = enclosing operation entity (function/method/arrow)
//	TO   = synthetic instrumentation stub
//	         "span:<name>"   for trace/transaction spans
//	         "metric:<name>" for metrics
//	Kind = INSTRUMENTS
//	Properties: library, api, line, traced=true (+ span_name|metric_name, dynamic)
//
// Libraries in scope (matched on library-reserved API names so the receiver
// identifier need not be type-resolved):
//
//	Sentry (@sentry/node, @sentry/browser, …)
//	  Sentry.startSpan({ name: 'checkout' }, cb)
//	  Sentry.startTransaction({ name: 'checkout' })
//	    → span name = the `name` property of the first object-literal argument.
//
//	Datadog dd-trace
//	  tracer.trace('op', cb)
//	  tracer.wrap('op', fn)
//	    → span name = the first string-literal argument.
//
//	prom-client
//	  const orders = new client.Counter({ name: 'orders_total' });
//	  orders.inc() / lat.observe(v) / end = hist.startTimer()
//	    → metric:<name> resolved from the `name` property of the metric
//	      constructor's options object.
//
// Honest-partial rule (#3689/#3762): a dynamic span/metric name (variable,
// expression, template literal) emits traced=true + dynamic=true keyed on the
// enclosing function ("span:<fn>" / "metric:<fn>") rather than fabricating a
// name. A prom-client body call (`.inc()` / `.observe()` / `.startTimer()`) on a
// variable that is NOT a known module-level metric is NOT emitted (could be any
// unrelated receiver).
//
// The walk is panic-tolerant so the primary extraction pipeline is unaffected.
package javascript

import (
	"strconv"

	"github.com/cajasmota/grafel/internal/treesitter/ts"

	"github.com/cajasmota/grafel/internal/types"
)

// jsInstrHit captures one non-OTel instrumentation site (span or metric).
type jsInstrHit struct {
	name    string // static span/metric name; "" when dynamic
	library string // "sentry" | "dd-trace" | "prom-client"
	api     string // library-facing API token (e.g. "Sentry.startSpan")
	kind    string // "span" | "metric"
	line    int    // 1-indexed source line
	dynamic bool   // true when the name is non-literal / unresolved
}

// jsSentrySpanMethods is the set of Sentry SDK methods that open a span /
// transaction whose name lives in the `name` property of the first object arg.
var jsSentrySpanMethods = map[string]bool{
	"startSpan":         true,
	"startTransaction":  true,
	"startInactiveSpan": true,
}

// jsPromClientCtors is the set of prom-client metric constructor names. The
// constructor's options object carries the `name` property.
var jsPromClientCtors = map[string]bool{
	"Counter":   true,
	"Gauge":     true,
	"Histogram": true,
	"Summary":   true,
}

// jsPromClientMethods is the set of prom-client metric mutation/observation
// methods that instrument code via a body call on a known metric variable.
var jsPromClientMethods = map[string]bool{
	"inc":        true,
	"dec":        true,
	"set":        true,
	"observe":    true,
	"startTimer": true,
}

// buildMetricVars scans the file for `<var> = new <ctor>({ name: '...' })`
// prom-client metric declarations and returns a var→metric-name map. A metric
// whose `name` property is non-literal (or absent) maps to "" (known-metric,
// dynamic-name). Returns nil when no prom-client metric is declared (fast-path).
func (x *extractor) buildMetricVars(root ts.Node) map[string]string {
	if root == nil {
		return nil
	}
	var reg map[string]string
	defer func() { _ = recover() }()

	for _, vd := range findAllNodes(root, "variable_declarator") {
		nameNode := vd.ChildByFieldName("name")
		valNode := vd.ChildByFieldName("value")
		if nameNode == nil || valNode == nil || nameNode.Type() != "identifier" {
			continue
		}
		if valNode.Type() != "new_expression" {
			continue
		}
		ctor := x.newExprConstructorName(valNode)
		if !jsPromClientCtors[ctor] {
			continue
		}
		metricName, ok := x.metricCtorName(valNode)
		if !ok {
			continue // not a recognisable {name: ...} options shape
		}
		if reg == nil {
			reg = make(map[string]string)
		}
		reg[x.nodeText(nameNode)] = metricName // "" when name is dynamic/absent
	}
	return reg
}

// newExprConstructorName returns the trailing name of a new_expression's
// constructor: `new client.Counter(...)` → "Counter", `new Counter(...)` →
// "Counter". Returns "" when the constructor is neither an identifier nor a
// member_expression.
func (x *extractor) newExprConstructorName(newExpr ts.Node) string {
	if newExpr == nil || newExpr.Type() != "new_expression" {
		return ""
	}
	ctor := newExpr.ChildByFieldName("constructor")
	if ctor == nil {
		// Fall back to the first non-arguments named child for grammars that do
		// not expose the constructor as a field.
		for i := 0; i < int(newExpr.NamedChildCount()); i++ {
			ch := newExpr.NamedChild(i)
			if ch != nil && ch.Type() != "arguments" {
				ctor = ch
				break
			}
		}
	}
	if ctor == nil {
		return ""
	}
	switch ctor.Type() {
	case "identifier":
		return x.nodeText(ctor)
	case "member_expression":
		if p := ctor.ChildByFieldName("property"); p != nil {
			return x.nodeText(p)
		}
	}
	return ""
}

// metricCtorName extracts the metric name from the `name` property of the
// options object passed to a prom-client metric constructor. Returns
// (name, true) when the options object is present; (name="", true) when the
// `name` property is absent or non-literal (dynamic); (..., false) when there is
// no object argument at all.
func (x *extractor) metricCtorName(newExpr ts.Node) (string, bool) {
	args := newExpr.ChildByFieldName("arguments")
	if args == nil {
		return "", false
	}
	obj := firstMeaningfulArg(args)
	if obj == nil || obj.Type() != "object" {
		return "", false
	}
	if name, ok := x.objectStringProp(obj, "name"); ok {
		return name, true
	}
	return "", true // options object present but no literal `name` → dynamic
}

// objectStringProp returns the string-literal value of the property named key in
// an object literal. The bool is true when a property with that key exists; when
// true and the value is non-literal the returned string is "".
func (x *extractor) objectStringProp(obj ts.Node, key string) (string, bool) {
	if obj == nil || obj.Type() != "object" {
		return "", false
	}
	for i := 0; i < int(obj.NamedChildCount()); i++ {
		pair := obj.NamedChild(i)
		if pair == nil || pair.Type() != "pair" {
			continue
		}
		k := pair.ChildByFieldName("key")
		v := pair.ChildByFieldName("value")
		if k == nil || v == nil {
			continue
		}
		if x.nodeText(k) != key {
			continue
		}
		if v.Type() == "string" {
			return stringLiteralValue(x.nodeText(v)), true
		}
		return "", true // key present but value is non-literal → dynamic
	}
	return "", false
}

// extractObservabilityHits walks a function/method/arrow body and returns one
// jsInstrHit per non-OTel instrumentation site (Sentry span/transaction,
// dd-trace tracer.trace/wrap, prom-client metric mutation).
func (x *extractor) extractObservabilityHits(body ts.Node) []jsInstrHit {
	if body == nil {
		return nil
	}
	var hits []jsInstrHit
	defer func() { _ = recover() }()

	for _, call := range findAllNodes(body, "call_expression") {
		fn := call.ChildByFieldName("function")
		if fn == nil || fn.Type() != "member_expression" {
			continue
		}
		propNode := fn.ChildByFieldName("property")
		objNode := fn.ChildByFieldName("object")
		if propNode == nil {
			continue
		}
		method := x.nodeText(propNode)
		recv := ""
		if objNode != nil && objNode.Type() == "identifier" {
			recv = x.nodeText(objNode)
		}
		args := call.ChildByFieldName("arguments")
		line := int(call.StartPoint().Row) + 1

		// Sentry: Sentry.startSpan({name:'op'}, cb) / startTransaction({name:'op'}).
		if recv == "Sentry" && jsSentrySpanMethods[method] {
			h := jsInstrHit{library: "sentry", api: "Sentry." + method, kind: "span", line: line}
			if name, ok := x.firstArgObjectName(args); ok && name != "" {
				h.name = name
			} else {
				h.dynamic = true
			}
			hits = append(hits, h)
			continue
		}

		// Datadog dd-trace: tracer.trace('op', cb) / tracer.wrap('op', fn).
		if recv == "tracer" && (method == "trace" || method == "wrap") {
			h := jsInstrHit{library: "dd-trace", api: "tracer." + method, kind: "span", line: line}
			if name := x.firstArgString(args); name != "" {
				h.name = name
			} else {
				h.dynamic = true
			}
			hits = append(hits, h)
			continue
		}

		// prom-client metric mutation: <metricVar>.inc()/.observe()/.startTimer().
		if jsPromClientMethods[method] && recv != "" {
			if metricName, known := x.metricVars[recv]; known {
				h := jsInstrHit{library: "prom-client", api: "metric." + method, kind: "metric", line: line}
				if metricName != "" {
					h.name = metricName
				} else {
					h.dynamic = true
				}
				hits = append(hits, h)
			}
		}
	}
	return hits
}

// firstArgObjectName returns the string `name` property of the first
// object-literal argument in args. The bool is false when the first arg is not
// an object literal at all.
func (x *extractor) firstArgObjectName(args ts.Node) (string, bool) {
	if args == nil {
		return "", false
	}
	first := firstMeaningfulArg(args)
	if first == nil || first.Type() != "object" {
		return "", false
	}
	return x.objectStringProp(first, "name")
}

// firstArgString returns the value of the first string-literal argument in args,
// or "" when the first meaningful argument is not a string literal.
func (x *extractor) firstArgString(args ts.Node) string {
	if args == nil {
		return ""
	}
	first := firstMeaningfulArg(args)
	if first != nil && first.Type() == "string" {
		return stringLiteralValue(x.nodeText(first))
	}
	return ""
}

// stampObservability emits INSTRUMENTS edges on the last entity appended to
// x.entities for every non-OTel instrumentation site found in body. Called
// immediately after a function/method/arrow entity is emitted, alongside
// stampTracingSpans (the OTel pass).
func (x *extractor) stampObservability(body ts.Node) {
	if body == nil || len(x.entities) == 0 {
		return
	}
	hits := x.extractObservabilityHits(body)
	if len(hits) == 0 {
		return
	}
	last := &x.entities[len(x.entities)-1]
	enclosing := last.Name
	seen := make(map[string]bool)
	for _, h := range hits {
		props := map[string]string{
			"library": h.library,
			"api":     h.api,
			"line":    strconv.Itoa(h.line),
			"traced":  "true",
		}
		prefix := h.kind + ":"
		var toID string
		if h.dynamic || h.name == "" {
			props["dynamic"] = "true"
			toID = prefix + enclosing
		} else {
			if h.kind == "metric" {
				props["metric_name"] = h.name
			} else {
				props["span_name"] = h.name
			}
			toID = prefix + h.name
		}
		// Dedup on (library, api, toID); dynamic sites additionally key on line so
		// distinct dynamic sites both survive.
		key := h.library + "|" + h.api + "|" + toID
		if h.dynamic || h.name == "" {
			key += "|" + strconv.Itoa(h.line)
		}
		if seen[key] {
			continue
		}
		seen[key] = true
		last.Relationships = append(last.Relationships, types.RelationshipRecord{
			ToID:       toID,
			Kind:       string(types.RelationshipKindInstruments),
			Properties: props,
		})
	}
}
