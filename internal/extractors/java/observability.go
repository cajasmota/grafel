// observability.go — Issue #3856 (epic #3854, area #11): Java/JVM-backend
// observability INSTRUMENTS extraction beyond the OpenTelemetry tracing pass
// (tracing.go, #3689).
//
// Extends the wave-5 INSTRUMENTS edge contract — shared byte-for-byte with the
// Python (#3762), Go (#3769) and JS/TS (#3689) observability extractors — to the
// dominant non-OTel JVM instrumentation libraries, stamping the SAME edge:
//
//	FROM = enclosing operation entity (method/constructor)
//	TO   = synthetic instrumentation stub
//	         "span:<name>"   for trace/transaction spans
//	         "metric:<name>" for metrics
//	         "log:<name>"    for structured/keyed log events
//	Kind = INSTRUMENTS
//	Properties: library, api, line, traced=true (+ span_name|metric_name|
//	            log_name, dynamic)
//
// Libraries in scope:
//
//	Micrometer (metrics)
//	  Counter c = meterRegistry.counter("orders.created");   // registry method
//	  meterRegistry.timer("db.query").record(d);
//	  Counter c = Counter.builder("orders.created").register(reg);  // builder
//	  @Timed("api.latency")                                   // method annotation
//	  @Counted("orders.created")
//
//	Dropwizard Metrics (metrics)
//	  registry.meter("requests");  registry.timer("db");  registry.counter("c");
//
//	Spring Sleuth / Brave (tracing)
//	  Span span = tracer.nextSpan().name("checkout").start();
//
//	SLF4J / Logback (structured logging)
//	  logger.atInfo().addKeyValue("orderId", id).log("order placed"); // fluent
//	  MarkerFactory.getMarker("AUDIT");                               // named marker
//	  logger.info(AUDIT, "...")                                       // marker-keyed
//
// Honest-partial rule (#3689/#3856): a dynamic span/metric/log name (a variable
// or non-literal expression) emits traced=true + dynamic=true keyed on the
// enclosing method ("span:<m>" / "metric:<m>" / "log:<m>") rather than
// fabricating a name. A registry metric call (`reg.counter(x)`) with a
// non-literal name on a known-style registry receiver is emitted dynamic; a
// metric method on an unknown receiver is NOT emitted (could be unrelated).
// Plain free-text logging (`log.info("some message")`) is intentionally NOT
// emitted as a log event — only fluent keyed logging and marker-keyed calls,
// where an event/marker name is resolvable, are honest log instrumentation.
//
// The walk is panic-tolerant so the primary extraction pipeline is unaffected.
package java

import (
	"strconv"
	"strings"

	"github.com/cajasmota/grafel/internal/treesitter/ts"

	"github.com/cajasmota/grafel/internal/types"
)

// javaObsHit captures one non-OTel-tracing instrumentation site.
type javaObsHit struct {
	name    string // static span/metric/log name; "" when dynamic
	library string // "micrometer" | "dropwizard" | "brave" | "slf4j"
	api     string // library-facing API token (e.g. "registry.counter", "Timed")
	kind    string // "span" | "metric" | "log"
	line    int    // 1-indexed source line
	dynamic bool   // true when the name is a non-literal expression
}

// javaMicrometerRegistryMethods is the set of MeterRegistry methods whose first
// string argument is the metric name (Micrometer). The receiver is checked to
// look like a meter registry so unrelated `.counter(...)` calls don't match.
var javaMicrometerRegistryMethods = map[string]bool{
	"counter":             true,
	"timer":               true,
	"gauge":               true,
	"summary":             true,
	"longTaskTimer":       true,
	"distributionSummary": true,
}

// javaDropwizardRegistryMethods is the set of Dropwizard MetricRegistry methods
// whose first string argument is the metric name.
var javaDropwizardRegistryMethods = map[string]bool{
	"meter":     true,
	"timer":     true,
	"counter":   true,
	"histogram": true,
}

// javaMetricAnnotations maps Micrometer metric annotations to their api token.
// @Timed / @Counted carry the metric name as the first string-literal value.
var javaMetricAnnotations = map[string]bool{
	"Timed":   true,
	"Counted": true,
}

// javaObsEdges scans a method/constructor node for non-OTel-tracing
// instrumentation sites (Micrometer/Dropwizard metrics, Brave/Sleuth spans,
// SLF4J structured logs) and returns the corresponding INSTRUMENTS edges.
// methodName keys dynamic-name stubs.
func javaObsEdges(methodNode ts.Node, methodName string, src []byte) []types.RelationshipRecord {
	if methodNode == nil {
		return nil
	}
	var hits []javaObsHit

	func() {
		defer func() { _ = recover() }()
		// Annotation-form metrics live in the method modifiers.
		hits = append(hits, javaMetricAnnotationHits(methodNode, src)...)
		// Call-form instrumentation lives in the body.
		body := methodNode.ChildByFieldName("body")
		hits = append(hits, javaBodyObsHits(body, src)...)
	}()

	if len(hits) == 0 {
		return nil
	}
	seen := make(map[string]bool)
	out := make([]types.RelationshipRecord, 0, len(hits))
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
			toID = prefix + methodName
		} else {
			switch h.kind {
			case "metric":
				props["metric_name"] = h.name
			case "log":
				props["log_name"] = h.name
			default:
				props["span_name"] = h.name
			}
			toID = prefix + h.name
		}
		// Dedup on (library, api, toID); dynamic sites also key on line so
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
			ToID:       toID,
			Kind:       string(types.RelationshipKindInstruments),
			Properties: props,
		})
	}
	return out
}

// javaMetricAnnotationHits returns a hit for each @Timed/@Counted annotation on
// the method. The metric name is the annotation's first string-literal value
// when present; a bare @Timed (no name) is emitted dynamic (Micrometer derives
// the name from the method/registry config — not statically resolvable here).
func javaMetricAnnotationHits(methodNode ts.Node, src []byte) []javaObsHit {
	var hits []javaObsHit
	for i := 0; i < int(methodNode.ChildCount()); i++ {
		child := methodNode.Child(i)
		if child == nil || child.Type() != "modifiers" {
			continue
		}
		for j := 0; j < int(child.ChildCount()); j++ {
			a := child.Child(j)
			if a == nil {
				continue
			}
			if a.Type() != "annotation" && a.Type() != "marker_annotation" {
				continue
			}
			nameNode := a.ChildByFieldName("name")
			if nameNode == nil {
				continue
			}
			leaf := lastIdent(nodeText(nameNode, src))
			if !javaMetricAnnotations[leaf] {
				continue
			}
			line := int(a.StartPoint().Row) + 1
			name := javaAnnotationStringValue(a.ChildByFieldName("arguments"), src)
			h := javaObsHit{library: "micrometer", api: leaf, kind: "metric", line: line}
			if name != "" {
				h.name = name
			} else {
				h.dynamic = true
			}
			hits = append(hits, h)
		}
	}
	return hits
}

// javaBodyObsHits scans a method body for call-form instrumentation: Micrometer
// / Dropwizard registry metric calls, Micrometer Counter.builder("name"),
// Brave/Sleuth tracer.nextSpan().name("op"), and SLF4J fluent keyed logging.
func javaBodyObsHits(body ts.Node, src []byte) []javaObsHit {
	if body == nil {
		return nil
	}
	var hits []javaObsHit
	for _, call := range findAllNodes(body, "method_invocation") {
		if h, ok := javaMetricCallHit(call, src); ok {
			hits = append(hits, h)
			continue
		}
		if h, ok := javaBraveSpanHit(call, src); ok {
			hits = append(hits, h)
			continue
		}
		if h, ok := javaSlf4jLogHit(call, src); ok {
			hits = append(hits, h)
		}
	}
	return hits
}

// javaMetricCallHit matches a Micrometer/Dropwizard metric-creation call. Two
// shapes:
//
//	<registry>.counter("name") / .timer / .gauge / .summary       (Micrometer)
//	<registry>.meter("name")   / .timer / .counter / .histogram   (Dropwizard)
//	Counter.builder("name") / Timer.builder / Gauge.builder ...   (Micrometer)
//
// Registry-method calls are gated on a receiver that looks like a meter/metric
// registry so unrelated `.counter(...)` calls don't match. Returns false when
// the call is not a metric-creation call.
func javaMetricCallHit(call ts.Node, src []byte) (javaObsHit, bool) {
	name := javaInvocationName(call, src)
	args := call.ChildByFieldName("arguments")
	obj := call.ChildByFieldName("object")
	line := int(call.StartPoint().Row) + 1

	// Builder form: Counter.builder("name") / Timer.builder(...) — receiver is a
	// metric type identifier and the method is `builder`.
	if name == "builder" && obj != nil && obj.Type() == "identifier" {
		switch nodeText(obj, src) {
		case "Counter", "Timer", "Gauge", "DistributionSummary", "LongTaskTimer", "FunctionCounter", "FunctionTimer":
			h := javaObsHit{library: "micrometer", api: "builder", kind: "metric", line: line}
			if mn := javaFirstStringArg(args, src); mn != "" {
				h.name = mn
			} else {
				h.dynamic = true
			}
			return h, true
		}
	}

	// Registry-method form: <recv>.<method>("name") where recv looks like a
	// registry. Micrometer and Dropwizard share several method names (timer,
	// counter); the receiver heuristic + library tag is the disambiguator.
	recv := javaMetricReceiverName(obj, src)
	if recv == "" {
		return javaObsHit{}, false
	}
	if javaMicrometerRegistryMethods[name] {
		h := javaObsHit{library: "micrometer", api: "registry." + name, kind: "metric", line: line}
		if mn := javaFirstStringArg(args, src); mn != "" {
			h.name = mn
		} else {
			h.dynamic = true
		}
		return h, true
	}
	if javaDropwizardRegistryMethods[name] {
		h := javaObsHit{library: "dropwizard", api: "registry." + name, kind: "metric", line: line}
		if mn := javaFirstStringArg(args, src); mn != "" {
			h.name = mn
		} else {
			h.dynamic = true
		}
		return h, true
	}
	return javaObsHit{}, false
}

// javaMetricReceiverName returns the receiver identifier name when it looks like
// a meter/metric registry (Micrometer MeterRegistry or Dropwizard
// MetricRegistry), or "" otherwise. The heuristic gates on the lowercased
// identifier containing "registry" or being a conventional short name, so
// arbitrary `x.counter(...)` calls on unrelated objects are not misread as
// metric instrumentation.
func javaMetricReceiverName(obj ts.Node, src []byte) string {
	if obj == nil || obj.Type() != "identifier" {
		return ""
	}
	name := nodeText(obj, src)
	low := strings.ToLower(name)
	if strings.Contains(low, "registry") || low == "metrics" || low == "meter" {
		return name
	}
	return ""
}

// javaBraveSpanHit matches the Spring Sleuth / Brave fluent span form
// `tracer.nextSpan().name("op")` — anchored on the `.name("op")` call whose
// receiver chain includes a `nextSpan()` call. Returns false otherwise.
func javaBraveSpanHit(call ts.Node, src []byte) (javaObsHit, bool) {
	if javaInvocationName(call, src) != "name" {
		return javaObsHit{}, false
	}
	if !javaChainHasInvocation(call.ChildByFieldName("object"), "nextSpan", src) {
		return javaObsHit{}, false
	}
	line := int(call.StartPoint().Row) + 1
	h := javaObsHit{library: "brave", api: "tracer.nextSpan", kind: "span", line: line}
	if sn := javaFirstStringArg(call.ChildByFieldName("arguments"), src); sn != "" {
		h.name = sn
	} else {
		h.dynamic = true
	}
	return h, true
}

// javaChainHasInvocation reports whether the receiver (`object`) chain rooted at
// n contains a method_invocation named want.
func javaChainHasInvocation(n ts.Node, want string, src []byte) bool {
	for n != nil && n.Type() == "method_invocation" {
		if javaInvocationName(n, src) == want {
			return true
		}
		n = n.ChildByFieldName("object")
	}
	return false
}

// javaSlf4jLogHit matches SLF4J fluent structured logging:
// `logger.atInfo()...addKeyValue("k", v)...log("event")` — anchored on the
// terminal `.log(...)` call whose receiver chain includes a fluent level entry
// (`atInfo`/`atDebug`/...) AND at least one `addKeyValue`/`addMarker`. The log
// event name is the first string-literal argument to `.log(...)`; when that is
// non-literal but the call is a recognised structured-logging chain, the hit is
// emitted dynamic (keyed event with an unresolved message). Plain free-text
// `logger.info("text")` is intentionally NOT matched (no structured key).
func javaSlf4jLogHit(call ts.Node, src []byte) (javaObsHit, bool) {
	if javaInvocationName(call, src) != "log" {
		return javaObsHit{}, false
	}
	chain := call.ChildByFieldName("object")
	// Require a fluent level entry to anchor this as an SLF4J fluent builder.
	if !javaChainHasFluentLevel(chain, src) {
		return javaObsHit{}, false
	}
	// Require at least one structured key in the chain so we only count keyed
	// (structured) events, honest-partial per the ticket.
	if !javaChainHasAnyInvocation(chain, src, "addKeyValue", "addMarker", "addArgument") {
		return javaObsHit{}, false
	}
	line := int(call.StartPoint().Row) + 1
	h := javaObsHit{library: "slf4j", api: "fluent.log", kind: "log", line: line}
	if ln := javaFirstStringArg(call.ChildByFieldName("arguments"), src); ln != "" {
		h.name = ln
	} else {
		h.dynamic = true
	}
	return h, true
}

// javaSlf4jFluentLevels is the set of SLF4J 2.x fluent-API level entry points.
var javaSlf4jFluentLevels = map[string]bool{
	"atTrace": true,
	"atDebug": true,
	"atInfo":  true,
	"atWarn":  true,
	"atError": true,
	"atLevel": true,
}

// javaChainHasFluentLevel reports whether the receiver chain contains an SLF4J
// fluent level entry (atInfo()/atError()/...).
func javaChainHasFluentLevel(n ts.Node, src []byte) bool {
	for n != nil && n.Type() == "method_invocation" {
		if javaSlf4jFluentLevels[javaInvocationName(n, src)] {
			return true
		}
		n = n.ChildByFieldName("object")
	}
	return false
}

// javaChainHasAnyInvocation reports whether the receiver chain contains a
// method_invocation whose name is one of wants.
func javaChainHasAnyInvocation(n ts.Node, src []byte, wants ...string) bool {
	want := make(map[string]bool, len(wants))
	for _, w := range wants {
		want[w] = true
	}
	for n != nil && n.Type() == "method_invocation" {
		if want[javaInvocationName(n, src)] {
			return true
		}
		n = n.ChildByFieldName("object")
	}
	return false
}
