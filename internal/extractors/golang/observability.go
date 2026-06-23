// observability.go — Issue (epic #3628, area #11): non-OpenTelemetry
// observability/instrumentation breadth for Go.
//
// Extends the wave-5 INSTRUMENTS edge (OpenTelemetry, #3689, tracing.go) to the
// dominant non-OTel Go instrumentation libraries, stamping the SAME edge
// contract used by tracing.go / the Python wave-9 observability.go:
//
//	FROM = enclosing operation entity (function/method)
//	TO   = synthetic instrumentation stub
//	         "span:<name>"   for trace/transaction spans
//	         "metric:<name>" for metrics
//	Kind = INSTRUMENTS
//	Properties: library, api, line, traced=true (+ span_name|metric_name, dynamic)
//
// Libraries in scope (matched on library-reserved API names so the receiver
// identifier need not be type-resolved):
//
//	Datadog dd-trace-go (gopkg.in/DataDog/dd-trace-go.v1/ddtrace/tracer)
//	  span := tracer.StartSpan("web.request")
//	  span, ctx := tracer.StartSpanFromContext(ctx, "web.request")
//	    → span name = first string-literal argument.
//
//	Sentry (github.com/getsentry/sentry-go)
//	  span := sentry.StartSpan(ctx, "operation")
//	    → span name = first string-literal argument after the ctx.
//
//	Prometheus client (github.com/prometheus/client_golang/prometheus)
//	  reqs := prometheus.NewCounter(prometheus.CounterOpts{Name: "reqs"})
//	  reqs.Inc() / lat.Observe(d)
//	    → metric:<Name> resolved from the Name field of the *Opts struct
//	      literal passed to the New<Kind> constructor.
//
// Honest-partial rule (#3689/#3762): a dynamic span/metric name (variable,
// expression, non-literal) emits traced=true + dynamic=true keyed on the
// enclosing function ("span:<fn>" / "metric:<fn>") rather than fabricating a
// name. A Prometheus body call (`.Inc()` / `.Observe()`) on a variable that is
// NOT a known function- or file-level metric is NOT emitted (could be any
// unrelated receiver).
//
// The walk is panic-tolerant so the primary extraction pipeline is unaffected.
package golang

import (
	"strconv"

	"github.com/cajasmota/grafel/internal/treesitter/ts"

	"github.com/cajasmota/grafel/internal/types"
)

// goInstrHit captures one non-OTel instrumentation site (span or metric).
type goInstrHit struct {
	name    string // static span/metric name; "" when dynamic
	library string // "ddtrace" | "sentry" | "prometheus"
	api     string // library-facing API token (e.g. "tracer.StartSpan")
	kind    string // "span" | "metric"
	line    int    // 1-indexed source line
	dynamic bool   // true when the name is non-literal / unresolved
}

// goMetricRegistry maps a Go variable name to the Prometheus metric name
// declared by a `prometheus.New<Kind>(...Opts{Name: "x"})` constructor assigned
// to it, e.g. `reqs := prometheus.NewCounter(prometheus.CounterOpts{Name:"reqs"})`
// → {"reqs":"reqs"}. A variable bound to a metric constructor whose Name field
// is non-literal (or absent) maps to "" (known-metric, dynamic-name).
type goMetricRegistry map[string]string

// goPrometheusMetricCtors is the set of prometheus client constructor function
// names whose argument is a *Opts struct literal carrying a `Name` field.
var goPrometheusMetricCtors = map[string]bool{
	"NewCounter":      true,
	"NewGauge":        true,
	"NewSummary":      true,
	"NewHistogram":    true,
	"NewCounterVec":   true,
	"NewGaugeVec":     true,
	"NewSummaryVec":   true,
	"NewHistogramVec": true,
}

// goPrometheusMetricMethods is the set of prometheus metric mutation methods
// that instrument code via a body call on a known metric variable.
var goPrometheusMetricMethods = map[string]bool{
	"Inc":              true,
	"Dec":              true,
	"Add":              true,
	"Sub":              true,
	"Observe":          true,
	"Set":              true,
	"SetToCurrentTime": true,
}

// buildGoMetricRegistry scans a function body and the file root for short-var /
// var declarations binding a Prometheus metric constructor to a variable, and
// returns a name→metric-name map. Both `reqs := prometheus.NewCounter(...)` and
// file-level `var reqs = prometheus.NewCounter(...)` are honoured so a metric
// declared once at package scope and mutated inside a handler resolves. Returns
// nil when no metric is declared (zero cost for non-Prometheus files).
func buildGoMetricRegistry(root ts.Node, src []byte) goMetricRegistry {
	if root == nil {
		return nil
	}
	var reg goMetricRegistry
	defer func() { _ = recover() }()

	add := func(varName string, call ts.Node) {
		if varName == "" || call == nil {
			return
		}
		ctor := goCallSelectorLeaf(call, src)
		if !goPrometheusMetricCtors[ctor] {
			return
		}
		metricName, ok := goPrometheusOptsName(call, src)
		if !ok {
			return // not a recognisable prometheus ctor call shape
		}
		if reg == nil {
			reg = make(goMetricRegistry)
		}
		reg[varName] = metricName // "" when the Name field is dynamic/absent
	}

	// Short-var declarations: `reqs := prometheus.NewCounter(...)` (any scope).
	for _, decl := range findAll(root, "short_var_declaration") {
		left := decl.ChildByFieldName("left")
		right := decl.ChildByFieldName("right")
		if left == nil || right == nil {
			continue
		}
		// Single LHS target bound to a single constructor call.
		if left.NamedChildCount() != 1 {
			continue
		}
		varName := nodeText(left.NamedChild(0), src)
		call := firstCallExpr(right)
		add(varName, call)
	}

	// var declarations: `var reqs = prometheus.NewCounter(...)` (any scope).
	for _, spec := range findAll(root, "var_spec") {
		nameNode := spec.ChildByFieldName("name")
		valueNode := spec.ChildByFieldName("value")
		if nameNode == nil || valueNode == nil {
			continue
		}
		// Only handle the single-name single-value form: `var x = New...(...)`.
		// A multi-name spec (`var a, b = ...`) exposes the names as an
		// identifier_list — skip it (ambiguous binding).
		if nameNode.Type() != "identifier" {
			continue
		}
		call := firstCallExpr(valueNode)
		if call == nil {
			continue
		}
		add(nodeText(nameNode, src), call)
	}

	return reg
}

// goCallSelectorLeaf returns the trailing method/function name of a
// call_expression whose function is a selector (`prometheus.NewCounter` →
// "NewCounter") or a bare identifier (`NewCounter(...)` → "NewCounter").
func goCallSelectorLeaf(call ts.Node, src []byte) string {
	if call == nil || call.Type() != "call_expression" {
		return ""
	}
	fn := call.ChildByFieldName("function")
	if fn == nil {
		return ""
	}
	switch fn.Type() {
	case "identifier":
		return nodeText(fn, src)
	case "selector_expression":
		if field := fn.ChildByFieldName("field"); field != nil {
			return nodeText(field, src)
		}
	}
	return ""
}

// goPrometheusOptsName extracts the `Name` field value from the *Opts struct
// literal passed as the (only) argument of a prometheus.New<Kind> constructor
// call. Returns (name, true) when the call shape is a recognised composite
// literal argument; (name="", true) when the Name field is present but
// non-literal or absent (dynamic); (..., false) when the argument is not a
// composite literal at all (not a recognisable ctor → skip).
func goPrometheusOptsName(call ts.Node, src []byte) (string, bool) {
	args := call.ChildByFieldName("arguments")
	if args == nil || args.NamedChildCount() < 1 {
		return "", false
	}
	arg := args.NamedChild(0)
	if arg == nil {
		return "", false
	}
	// Argument is a composite_literal: `prometheus.CounterOpts{Name: "x"}`.
	if arg.Type() != "composite_literal" {
		return "", false
	}
	// The composite literal's field set lives in its `literal_value` child.
	body := firstChildOfType(arg, "literal_value")
	if body == nil {
		return "", true // recognised ctor, but no field body → dynamic name
	}
	for _, kv := range findAll(body, "keyed_element") {
		// keyed_element: <key> ':' <value>. The grammar exposes two literal_element
		// children (key, value); take the first as key, second as value.
		if kv.NamedChildCount() < 2 {
			continue
		}
		keyNode := kv.NamedChild(0)
		valNode := kv.NamedChild(1)
		if keyNode == nil || valNode == nil {
			continue
		}
		if nodeText(unwrapLiteralElement(keyNode), src) != "Name" {
			continue
		}
		v := unwrapLiteralElement(valNode)
		if v != nil && v.Type() == "interpreted_string_literal" {
			return goStripStringLiteral(nodeText(v, src)), true
		}
		return "", true // Name present but dynamic
	}
	return "", true // recognised ctor with body but no Name field → dynamic
}

// unwrapLiteralElement returns the single meaningful child of a literal_element
// wrapper node (tree-sitter Go wraps composite-literal keys/values in
// literal_element), or the node itself when it is not a wrapper.
func unwrapLiteralElement(n ts.Node) ts.Node {
	if n == nil {
		return nil
	}
	if n.Type() == "literal_element" && n.NamedChildCount() == 1 {
		return n.NamedChild(0)
	}
	return n
}

// extractGoInstrHits collects every non-OTel instrumentation site (span or
// metric) from a function/method body. metricReg resolves Prometheus metric
// variables to metric names. enclosingName keys dynamic stubs.
func extractGoInstrHits(body ts.Node, enclosingName string, metricReg goMetricRegistry, src []byte) []goInstrHit {
	if body == nil {
		return nil
	}
	var hits []goInstrHit
	defer func() { _ = recover() }()

	for _, call := range findAll(body, "call_expression") {
		if h, ok := goBodyInstrHit(call, enclosingName, metricReg, src); ok {
			hits = append(hits, h)
		}
	}
	return hits
}

// goBodyInstrHit inspects a call_expression for a ddtrace / Sentry span-start or
// a Prometheus metric mutation. Returns false when the call is not an
// instrumentation call.
func goBodyInstrHit(call ts.Node, enclosingName string, metricReg goMetricRegistry, src []byte) (goInstrHit, bool) {
	if call == nil || call.Type() != "call_expression" {
		return goInstrHit{}, false
	}
	fn := call.ChildByFieldName("function")
	if fn == nil || fn.Type() != "selector_expression" {
		return goInstrHit{}, false
	}
	field := fn.ChildByFieldName("field")
	if field == nil {
		return goInstrHit{}, false
	}
	leaf := nodeText(field, src)
	recv := ""
	if op := fn.ChildByFieldName("operand"); op != nil && op.Type() == "identifier" {
		recv = nodeText(op, src)
	}
	args := call.ChildByFieldName("arguments")
	line := int(call.StartPoint().Row) + 1

	// Datadog ddtrace: tracer.StartSpan("web.request") — first string arg is the
	// span name. tracer.StartSpanFromContext(ctx, "web.request") — span name is
	// the FIRST string-literal argument (ctx is first positional but not a string).
	if recv == "tracer" && (leaf == "StartSpan" || leaf == "StartSpanFromContext") {
		h := goInstrHit{library: "ddtrace", api: "tracer." + leaf, kind: "span", line: line}
		if name := goFirstStringArg(args, src); name != "" {
			h.name = name
		} else {
			h.dynamic = true
		}
		return h, true
	}

	// Sentry: sentry.StartSpan(ctx, "operation") — span name is the first
	// string-literal argument after the context.
	if recv == "sentry" && leaf == "StartSpan" {
		h := goInstrHit{library: "sentry", api: "sentry.StartSpan", kind: "span", line: line}
		if name := goFirstStringArg(args, src); name != "" {
			h.name = name
		} else {
			h.dynamic = true
		}
		return h, true
	}

	// Prometheus body mutation: reqs.Inc() / lat.Observe(d) — only when the
	// receiver is a known metric variable.
	if goPrometheusMetricMethods[leaf] && recv != "" {
		if metricName, known := metricReg[recv]; known {
			h := goInstrHit{library: "prometheus", api: "metric." + leaf, kind: "metric", line: line}
			if metricName != "" {
				h.name = metricName
			} else {
				h.dynamic = true
			}
			return h, true
		}
	}

	return goInstrHit{}, false
}

// goFirstStringArg returns the value of the first string-literal positional
// argument in args, or "" when none is a string literal.
func goFirstStringArg(args ts.Node, src []byte) string {
	if args == nil {
		return ""
	}
	for i := 0; i < int(args.NamedChildCount()); i++ {
		ch := args.NamedChild(i)
		if ch != nil && ch.Type() == "interpreted_string_literal" {
			return goStripStringLiteral(nodeText(ch, src))
		}
	}
	return ""
}

// goObservabilityEdges returns the INSTRUMENTS edges for every non-OTel
// instrumentation site (ddtrace, Sentry, Prometheus) found in body. metricReg
// resolves Prometheus metric variables to metric names. enclosingName keys
// dynamic stubs. fromID is the enclosing operation entity ID.
func goObservabilityEdges(body ts.Node, enclosingName, fromID string, metricReg goMetricRegistry, src []byte) []types.RelationshipRecord {
	hits := extractGoInstrHits(body, enclosingName, metricReg, src)
	if len(hits) == 0 {
		return nil
	}
	out := make([]types.RelationshipRecord, 0, len(hits))
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
			toID = prefix + enclosingName
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
		out = append(out, types.RelationshipRecord{
			FromID:     fromID,
			ToID:       toID,
			Kind:       string(types.RelationshipKindInstruments),
			Properties: props,
		})
	}
	return out
}
