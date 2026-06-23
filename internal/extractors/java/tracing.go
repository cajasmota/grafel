// tracing.go — Issue #3689 (epic #3628, area #11).
//
// Extracts OpenTelemetry distributed-tracing span-creation sites from Java
// method declarations and emits an INSTRUMENTS edge from the enclosing
// operation entity → a synthetic span stub ("span:<name>").
//
// Dominant OpenTelemetry Java idioms in scope:
//
//	@WithSpan                              // span name = method name
//	void handle() { ... }
//
//	@WithSpan("custom-op")                 // span name = annotation value
//	void handle() { ... }
//
//	Span span = tracer.spanBuilder("db.query").startSpan();   // builder form
//
// The @WithSpan annotation (io.opentelemetry.instrumentation.annotations.WithSpan)
// auto-instruments the method; per the OTel spec the span name defaults to
// "<SimpleClassName>.<method>" but the extractor records the developer-facing
// name — the annotation's value when present, otherwise the bare method name.
//
// The builder form is matched as a method_invocation named `startSpan` whose
// receiver chain includes a `spanBuilder("name")` call carrying the span name.
//
// Honest-partial rule (#3689): a non-literal span name (a variable/expression
// passed to spanBuilder) emits traced=true + dynamic=true keyed on the method
// name ("span:<method>") rather than a fabricated name.
//
// The walk is tolerant: a panic inside the traversal is recovered so the
// primary extraction pipeline is unaffected.
package java

import (
	"strconv"
	"strings"

	"github.com/cajasmota/grafel/internal/treesitter/ts"

	"github.com/cajasmota/grafel/internal/types"
)

// javaSpanHit captures one OpenTelemetry span-creation site for a Java method.
type javaSpanHit struct {
	spanName string // static span name; "" when dynamic
	api      string // "WithSpan" | "spanBuilder"
	line     int    // 1-indexed source line of the annotation / call
	dynamic  bool   // true when the span name is a non-literal expression
}

// javaTracingSpanEdges scans a method_declaration node for OpenTelemetry
// span-creation sites — the @WithSpan annotation in its modifiers and
// `spanBuilder(...).startSpan()` calls in its body — and returns the
// corresponding INSTRUMENTS edges. methodName is the bare method name used as
// the @WithSpan default span name and to key dynamic-name stubs.
func javaTracingSpanEdges(methodNode ts.Node, methodName string, src []byte) []types.RelationshipRecord {
	if methodNode == nil {
		return nil
	}
	var hits []javaSpanHit

	func() {
		defer func() { _ = recover() }()
		hits = append(hits, javaWithSpanHits(methodNode, methodName, src)...)
		hits = append(hits, javaSpanBuilderHits(methodNode.ChildByFieldName("body"), src)...)
	}()

	if len(hits) == 0 {
		return nil
	}
	seen := make(map[string]bool)
	out := make([]types.RelationshipRecord, 0, len(hits))
	for _, h := range hits {
		key := h.api + "|" + h.spanName + "|" + strconv.FormatBool(h.dynamic) + "|" + strconv.Itoa(h.line)
		if seen[key] {
			continue
		}
		seen[key] = true
		props := map[string]string{
			"library": "opentelemetry",
			"api":     h.api,
			"line":    strconv.Itoa(h.line),
			"traced":  "true",
		}
		var toID string
		if h.dynamic {
			props["dynamic"] = "true"
			toID = "span:" + methodName
		} else {
			props["span_name"] = h.spanName
			toID = "span:" + h.spanName
		}
		out = append(out, types.RelationshipRecord{
			ToID:       toID,
			Kind:       string(types.RelationshipKindInstruments),
			Properties: props,
		})
	}
	return out
}

// javaWithSpanHits returns a single hit when the method carries a @WithSpan
// annotation. The span name is the annotation's first string-literal argument
// when present, otherwise the bare method name.
func javaWithSpanHits(methodNode ts.Node, methodName string, src []byte) []javaSpanHit {
	var hits []javaSpanHit
	// The modifiers child carries the annotations preceding the method.
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
			if nameNode == nil || lastIdent(nodeText(nameNode, src)) != "WithSpan" {
				continue
			}
			line := int(a.StartPoint().Row) + 1
			// @WithSpan("name") or @WithSpan(value="name"): pull a string literal.
			name := javaAnnotationStringValue(a.ChildByFieldName("arguments"), src)
			if name != "" {
				hits = append(hits, javaSpanHit{spanName: name, api: "WithSpan", line: line})
			} else {
				// Bare @WithSpan → span name defaults to the method name. This is
				// a known static name, NOT dynamic.
				hits = append(hits, javaSpanHit{spanName: methodName, api: "WithSpan", line: line})
			}
		}
	}
	return hits
}

// javaAnnotationStringValue returns the first string-literal value found in an
// annotation_argument_list — handling both @X("v") and @X(value="v") — or ""
// when none is present.
func javaAnnotationStringValue(args ts.Node, src []byte) string {
	if args == nil {
		return ""
	}
	for _, lit := range findAllNodes(args, "string_literal") {
		v := javaStripString(nodeText(lit, src))
		if v != "" {
			return v
		}
	}
	return ""
}

// javaSpanBuilderHits scans a method body for fluent
// `<recv>.spanBuilder("name").…​.startSpan()` chains, returning one hit per
// chain. It anchors on the terminal `startSpan()` invocation and walks down its
// receiver chain to find the `spanBuilder(...)` call that carries the span name.
// Anchoring on startSpan (the OTel call that actually creates the span) avoids
// matching a bare `spanBuilder(...)` whose span is never started.
func javaSpanBuilderHits(body ts.Node, src []byte) []javaSpanHit {
	if body == nil {
		return nil
	}
	var hits []javaSpanHit
	for _, call := range findAllNodes(body, "method_invocation") {
		if javaInvocationName(call, src) != "startSpan" {
			continue
		}
		builder := javaFindSpanBuilderInChain(call, src)
		if builder == nil {
			continue
		}
		line := int(builder.StartPoint().Row) + 1
		name := javaFirstStringArg(builder.ChildByFieldName("arguments"), src)
		if name != "" {
			hits = append(hits, javaSpanHit{spanName: name, api: "spanBuilder", line: line})
		} else {
			hits = append(hits, javaSpanHit{api: "spanBuilder", line: line, dynamic: true})
		}
	}
	return hits
}

// javaFindSpanBuilderInChain walks the receiver (`object`) chain of a startSpan
// invocation looking for the `spanBuilder(...)` call. Returns the spanBuilder
// method_invocation node, or nil when the chain does not contain one.
func javaFindSpanBuilderInChain(startSpanCall ts.Node, src []byte) ts.Node {
	obj := startSpanCall.ChildByFieldName("object")
	for obj != nil && obj.Type() == "method_invocation" {
		if javaInvocationName(obj, src) == "spanBuilder" {
			return obj
		}
		obj = obj.ChildByFieldName("object")
	}
	return nil
}

// javaFirstStringArg returns the first string-literal argument value in an
// argument_list, or "" when the first argument is non-literal / absent.
func javaFirstStringArg(args ts.Node, src []byte) string {
	if args == nil {
		return ""
	}
	for i := 0; i < int(args.NamedChildCount()); i++ {
		arg := args.NamedChild(i)
		if arg == nil {
			continue
		}
		if arg.Type() == "string_literal" {
			return javaStripString(nodeText(arg, src))
		}
		// First positional arg is non-literal → dynamic.
		break
	}
	return ""
}

// javaInvocationName returns the bare name of a method_invocation node.
func javaInvocationName(call ts.Node, src []byte) string {
	name := call.ChildByFieldName("name")
	if name == nil {
		return ""
	}
	return strings.TrimSpace(nodeText(name, src))
}
