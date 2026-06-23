// observability.go — Issue #3762 (epic #3628, area #11): non-OpenTelemetry
// observability/instrumentation breadth for Python.
//
// Extends the wave-5 INSTRUMENTS edge (OpenTelemetry, issue #3689) to the
// dominant non-OTel Python instrumentation libraries, stamping the SAME edge
// contract used by tracing.go:
//
//	FROM = enclosing operation entity (function/method)
//	TO   = synthetic instrumentation stub
//	         "span:<name>"   for trace/transaction spans
//	         "metric:<name>" for metrics
//	Kind = INSTRUMENTS
//	Properties: library, api, line, traced=true (+ span_name|metric_name, dynamic)
//
// Libraries in scope (all decorator- or call-based, matched on library-reserved
// API names so the receiver identifier need not be resolved):
//
//	Datadog ddtrace
//	  @tracer.wrap()                       # span name defaults to the fn name
//	  @tracer.wrap(name="checkout")        # explicit name
//	  tracer.trace("db.query")             # manual span (body call)
//
//	Sentry
//	  @sentry_sdk.trace                    # bare decorator → span = fn name
//	  sentry_sdk.start_transaction(name="checkout")   # transaction (body call)
//	  sentry_sdk.start_span(description="db.query")
//
//	Prometheus client
//	  REQUEST_TIME = Summary("request_seconds", "...")   # module-level metric
//	  @REQUEST_TIME.time()                 # → metric:request_seconds
//	  @LATENCY.count_exceptions()
//	  COUNTER.inc() / HIST.observe(x)      # body calls on a known metric var
//
//	New Relic
//	  @newrelic.agent.function_trace()     # span name defaults to the fn name
//	  @function_trace(name="checkout")     # explicit name
//	  @background_task()
//
// Honest-partial rule (#3689/#3762): a dynamic span/metric name (variable,
// attribute, f-string, call) emits traced=true + dynamic=true keyed on the
// enclosing function ("span:<fn>" / "metric:<fn>") rather than fabricating a
// name. A Prometheus decorator/body-call on a variable that is NOT a known
// module-level metric is NOT emitted (could be any unrelated object).
//
// The walk is panic-tolerant so the primary extraction pipeline is unaffected.
package python

import (
	"strconv"

	"github.com/cajasmota/grafel/internal/treesitter/ts"

	"github.com/cajasmota/grafel/internal/types"
)

// pyInstrHit captures one non-OTel instrumentation site (span or metric).
type pyInstrHit struct {
	name    string // static span/metric name; "" when dynamic
	library string // "ddtrace" | "sentry" | "prometheus" | "newrelic"
	api     string // library-facing API token (e.g. "tracer.wrap", "Summary.time")
	kind    string // "span" | "metric"
	line    int    // 1-indexed source line
	dynamic bool   // true when the name is non-literal / unresolved
}

// pyMetricRegistry maps a module-level Python variable name to the metric name
// declared by a Prometheus metric constructor assigned to it, e.g.
// `REQUEST_TIME = Summary("request_seconds", ...)` → {"REQUEST_TIME":"request_seconds"}.
// A variable assigned to a metric constructor with a non-literal first arg maps
// to "" (known-metric, dynamic-name).
type pyMetricRegistry map[string]string

// pyPrometheusMetricCtors is the set of prometheus_client metric constructor
// names whose first positional argument is the metric name.
var pyPrometheusMetricCtors = map[string]bool{
	"Counter":   true,
	"Gauge":     true,
	"Summary":   true,
	"Histogram": true,
	"Info":      true,
	"Enum":      true,
}

// pyPrometheusInstrumentMethods is the set of prometheus_client metric methods
// that instrument code: the `.time()` / `.count_exceptions()` decorators and the
// `.inc()` / `.observe()` / `.set()` body calls. The bool value is true when the
// method is a decorator form (used as `@METRIC.time()`), false when it is a
// body-call form.
var pyPrometheusInstrumentMethods = map[string]bool{
	"time":             true, // decorator (Summary/Histogram)
	"count_exceptions": true, // decorator (Counter/Gauge)
	"track_inprogress": true, // decorator (Gauge)
	"inc":              true, // body call
	"dec":              true, // body call
	"observe":          true, // body call
	"set":              true, // body call
}

// buildPyMetricRegistry scans module-level assignments for Prometheus metric
// constructors and returns a name→metric-name map. Only top-level (module-scope)
// assignments are considered; the registry is used to resolve `@METRIC.time()`
// decorators and `METRIC.inc()` body calls to a metric name. Returns nil when no
// metric is declared (zero cost for files that don't use Prometheus).
func buildPyMetricRegistry(root ts.Node, src []byte) pyMetricRegistry {
	if root == nil {
		return nil
	}
	var reg pyMetricRegistry
	defer func() { _ = recover() }()

	// Only scan direct module-level statements (expression_statement →
	// assignment). Nested/conditional metric definitions are out of honest scope.
	for i := 0; i < int(root.NamedChildCount()); i++ {
		stmt := root.NamedChild(i)
		if stmt == nil || stmt.Type() != "expression_statement" {
			continue
		}
		for j := 0; j < int(stmt.NamedChildCount()); j++ {
			asn := stmt.NamedChild(j)
			if asn == nil || asn.Type() != "assignment" {
				continue
			}
			left := asn.ChildByFieldName("left")
			right := asn.ChildByFieldName("right")
			if left == nil || right == nil || left.Type() != "identifier" {
				continue
			}
			if right.Type() != "call" {
				continue
			}
			ctor := pyCallFunctionName(right, src)
			if !pyPrometheusMetricCtors[ctor] {
				continue
			}
			varName := nodeText(left, src)
			// First positional arg of the metric ctor is the metric name.
			args := right.ChildByFieldName("arguments")
			nameNode := firstPositionalArg(args)
			metricName := ""
			if nameNode != nil && nameNode.Type() == "string" {
				metricName = pythonLiteralValue(nameNode, src)
			}
			if reg == nil {
				reg = make(pyMetricRegistry)
			}
			reg[varName] = metricName // "" when the name is dynamic
		}
	}
	return reg
}

// pyCallFunctionName returns the bare callee name of a `call` node, handling both
// a plain identifier callee (`Summary(...)`) and an attribute callee
// (`prom.Summary(...)` → "Summary"). Returns "" when the callee is neither.
func pyCallFunctionName(call ts.Node, src []byte) string {
	if call == nil || call.Type() != "call" {
		return ""
	}
	fn := call.ChildByFieldName("function")
	if fn == nil {
		return ""
	}
	switch fn.Type() {
	case "identifier":
		return nodeText(fn, src)
	case "attribute":
		if attr := fn.ChildByFieldName("attribute"); attr != nil {
			return nodeText(attr, src)
		}
	}
	return ""
}

// extractPyInstrHits collects every non-OTel instrumentation site for a function
// from its decorators (decoratorParent, when non-nil) and its body. metricReg
// resolves Prometheus metric variables to metric names.
func extractPyInstrHits(funcNode, decoratorParent ts.Node, enclosingName string, metricReg pyMetricRegistry, src []byte) []pyInstrHit {
	if funcNode == nil {
		return nil
	}
	var hits []pyInstrHit
	defer func() { _ = recover() }()

	// --- Decorator forms -----------------------------------------------------
	if decoratorParent != nil {
		for i := 0; i < int(decoratorParent.ChildCount()); i++ {
			ch := decoratorParent.Child(i)
			if ch == nil || ch.Type() != "decorator" {
				continue
			}
			hits = append(hits, pyDecoratorInstrHits(ch, enclosingName, metricReg, src)...)
		}
	}

	// --- Body call forms -----------------------------------------------------
	body := funcNode.ChildByFieldName("body")
	if body != nil {
		for _, call := range findAll(body, "call") {
			if h, ok := pyBodyInstrHit(call, metricReg, src); ok {
				hits = append(hits, h)
			}
		}
	}
	return hits
}

// pyDecoratorInstrHits returns instrumentation hits for a single decorator node
// (@... attached to a function). Handles ddtrace `@tracer.wrap`, Sentry
// `@sentry_sdk.trace`, New Relic `@function_trace`/`@background_task`/
// `@newrelic.agent.function_trace`, and Prometheus `@METRIC.time()`.
func pyDecoratorInstrHits(dec ts.Node, enclosingName string, metricReg pyMetricRegistry, src []byte) []pyInstrHit {
	// A decorator child is either an `attribute`/`identifier` (bare @x.y / @x)
	// or a `call` (@x(...)). Resolve the dotted path of the decorator callee and
	// its argument list (when present).
	var calleeNode ts.Node // identifier/attribute being applied
	var argsNode ts.Node   // argument_list when the decorator is a call

	for i := 0; i < int(dec.ChildCount()); i++ {
		ch := dec.Child(i)
		if ch == nil {
			continue
		}
		switch ch.Type() {
		case "call":
			calleeNode = ch.ChildByFieldName("function")
			argsNode = ch.ChildByFieldName("arguments")
		case "attribute", "identifier":
			if calleeNode == nil {
				calleeNode = ch
			}
		}
	}
	if calleeNode == nil {
		return nil
	}

	leaf := pyAttrLeaf(calleeNode, src)     // last dotted segment
	dotted := pyDottedPath(calleeNode, src) // full dotted path
	line := int(dec.StartPoint().Row) + 1

	// Prometheus: @METRIC.time() / @METRIC.count_exceptions() — the receiver of
	// the leaf method must be a known module-level metric variable.
	if pyPrometheusInstrumentMethods[leaf] {
		if recv := pyAttrReceiverName(calleeNode, src); recv != "" {
			if metricName, known := metricReg[recv]; known {
				h := pyInstrHit{library: "prometheus", api: "metric." + leaf, kind: "metric", line: line}
				if metricName != "" {
					h.name = metricName
				} else {
					h.dynamic = true
				}
				return []pyInstrHit{h}
			}
		}
		return nil
	}

	// Datadog ddtrace: @tracer.wrap(...) — span name from name= kwarg else fn.
	if leaf == "wrap" {
		h := pyInstrHit{library: "ddtrace", api: "tracer.wrap", kind: "span", line: line}
		if name := pyKeywordStringArg(argsNode, "name", src); name != "" {
			h.name = name
		} else if name := pyKeywordStringArg(argsNode, "resource", src); name != "" {
			h.name = name
		} else {
			h.name = enclosingName // ddtrace defaults the span name to the fn name
		}
		return []pyInstrHit{h}
	}

	// New Relic: @newrelic.agent.function_trace(...) / @function_trace(...) /
	// @background_task(...). Span name from name= kwarg else fn.
	if leaf == "function_trace" || leaf == "background_task" || leaf == "web_transaction" {
		h := pyInstrHit{library: "newrelic", api: leaf, kind: "span", line: line}
		if name := pyKeywordStringArg(argsNode, "name", src); name != "" {
			h.name = name
		} else {
			h.name = enclosingName
		}
		return []pyInstrHit{h}
	}

	// Sentry: @sentry_sdk.trace / @sentry.trace (bare decorator) → span = fn.
	if leaf == "trace" && (dotted == "sentry_sdk.trace" || dotted == "sentry.trace") {
		return []pyInstrHit{{library: "sentry", api: "trace", kind: "span", name: enclosingName, line: line}}
	}

	return nil
}

// pyBodyInstrHit inspects a body `call` node for a manual span/transaction start
// (ddtrace `tracer.trace(...)`, Sentry `start_transaction`/`start_span`) or a
// Prometheus metric mutation (`METRIC.inc()` / `.observe()` / `.set()` on a known
// metric var). Returns false when the call is not an instrumentation call.
func pyBodyInstrHit(call ts.Node, metricReg pyMetricRegistry, src []byte) (pyInstrHit, bool) {
	if call == nil || call.Type() != "call" {
		return pyInstrHit{}, false
	}
	fn := call.ChildByFieldName("function")
	if fn == nil || fn.Type() != "attribute" {
		return pyInstrHit{}, false
	}
	leaf := pyAttrLeaf(fn, src)
	dotted := pyDottedPath(fn, src)
	args := call.ChildByFieldName("arguments")
	line := int(call.StartPoint().Row) + 1

	// Datadog ddtrace manual span: tracer.trace("op") — first positional arg.
	if leaf == "trace" && dotted != "sentry_sdk.trace" && dotted != "sentry.trace" {
		recv := pyAttrReceiverName(fn, src)
		if recv == "tracer" || recv == "_tracer" {
			h := pyInstrHit{library: "ddtrace", api: "tracer.trace", kind: "span", line: line}
			if name := pyFirstPositionalString(args, src); name != "" {
				h.name = name
			} else {
				h.dynamic = true
			}
			return h, true
		}
		return pyInstrHit{}, false
	}

	// Sentry transactions/spans: name= or description= keyword carries the name.
	if leaf == "start_transaction" || leaf == "start_span" {
		h := pyInstrHit{library: "sentry", api: leaf, kind: "span", line: line}
		name := pyKeywordStringArg(args, "name", src)
		if name == "" {
			name = pyKeywordStringArg(args, "op", src)
		}
		if name == "" {
			name = pyKeywordStringArg(args, "description", src)
		}
		if name != "" {
			h.name = name
		} else {
			h.dynamic = true
		}
		return h, true
	}

	// Prometheus body mutation: METRIC.inc()/dec()/observe()/set() — only when
	// the leaf is a body-call method AND the receiver is a known metric var.
	if pyPrometheusInstrumentMethods[leaf] && leaf != "time" && leaf != "count_exceptions" && leaf != "track_inprogress" {
		if recv := pyAttrReceiverName(fn, src); recv != "" {
			if metricName, known := metricReg[recv]; known {
				h := pyInstrHit{library: "prometheus", api: "metric." + leaf, kind: "metric", line: line}
				if metricName != "" {
					h.name = metricName
				} else {
					h.dynamic = true
				}
				return h, true
			}
		}
	}

	return pyInstrHit{}, false
}

// pyAttrLeaf returns the last dotted segment of an identifier/attribute node:
// `tracer.wrap` → "wrap", `newrelic.agent.function_trace` → "function_trace",
// a bare `foo` → "foo".
func pyAttrLeaf(n ts.Node, src []byte) string {
	if n == nil {
		return ""
	}
	switch n.Type() {
	case "identifier":
		return nodeText(n, src)
	case "attribute":
		if a := n.ChildByFieldName("attribute"); a != nil {
			return nodeText(a, src)
		}
	}
	return ""
}

// pyAttrReceiverName returns the identifier name of the immediate receiver of an
// attribute node: `REQUEST_TIME.time` → "REQUEST_TIME". Returns "" when the
// receiver is not a bare identifier (e.g. a nested attribute or a call).
func pyAttrReceiverName(n ts.Node, src []byte) string {
	if n == nil || n.Type() != "attribute" {
		return ""
	}
	obj := n.ChildByFieldName("object")
	if obj == nil || obj.Type() != "identifier" {
		return ""
	}
	return nodeText(obj, src)
}

// pyDottedPath renders the full dotted path of an identifier/attribute node:
// `sentry_sdk.trace` → "sentry_sdk.trace". Non-identifier receivers terminate
// the path. Returns the leaf alone when the path cannot be fully rendered.
func pyDottedPath(n ts.Node, src []byte) string {
	if n == nil {
		return ""
	}
	switch n.Type() {
	case "identifier":
		return nodeText(n, src)
	case "attribute":
		obj := n.ChildByFieldName("object")
		attr := n.ChildByFieldName("attribute")
		if attr == nil {
			return ""
		}
		left := pyDottedPath(obj, src)
		if left == "" {
			return nodeText(attr, src)
		}
		return left + "." + nodeText(attr, src)
	}
	return ""
}

// pyFirstPositionalString returns the value of the first positional argument when
// it is a string literal, else "".
func pyFirstPositionalString(args ts.Node, src []byte) string {
	nameNode := firstPositionalArg(args)
	if nameNode != nil && nameNode.Type() == "string" {
		return pythonLiteralValue(nameNode, src)
	}
	return ""
}

// pyKeywordStringArg returns the string-literal value of the keyword argument
// named key in args (`name="checkout"` → "checkout"), or "" when absent / not a
// string literal.
func pyKeywordStringArg(args ts.Node, key string, src []byte) string {
	if args == nil {
		return ""
	}
	for i := 0; i < int(args.ChildCount()); i++ {
		ch := args.Child(i)
		if ch == nil || ch.Type() != "keyword_argument" {
			continue
		}
		nameNode := ch.ChildByFieldName("name")
		valNode := ch.ChildByFieldName("value")
		if nameNode == nil || valNode == nil {
			continue
		}
		if nodeText(nameNode, src) != key {
			continue
		}
		if valNode.Type() == "string" {
			return pythonLiteralValue(valNode, src)
		}
		return "" // keyword present but non-literal → caller treats as absent
	}
	return ""
}

// stampPythonObservability emits INSTRUMENTS edges on the entity at index idx for
// every non-OTel instrumentation site (ddtrace, Sentry, Prometheus, New Relic)
// found in funcNode's decorators and body. metricReg resolves Prometheus metric
// variables to metric names. enclosingName keys dynamic stubs and supplies the
// default span name for name-defaulting decorators (ddtrace/New Relic/Sentry).
func stampPythonObservability(funcNode, decoratorParent ts.Node, enclosingName string, metricReg pyMetricRegistry, src []byte, out *[]types.EntityRecord, idx int) {
	if funcNode == nil || out == nil || idx < 0 || idx >= len(*out) {
		return
	}
	hits := extractPyInstrHits(funcNode, decoratorParent, enclosingName, metricReg, src)
	if len(hits) == 0 {
		return
	}
	e := &(*out)[idx]
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
		// Dedup on (library, api, toID) so repeated identical sites collapse; a
		// dynamic site keys on line so distinct dynamic sites both survive.
		key := h.library + "|" + h.api + "|" + toID
		if h.dynamic || h.name == "" {
			key += "|" + strconv.Itoa(h.line)
		}
		if seen[key] {
			continue
		}
		seen[key] = true
		e.Relationships = append(e.Relationships, types.RelationshipRecord{
			ToID:       toID,
			Kind:       string(types.RelationshipKindInstruments),
			Properties: props,
		})
	}
}
