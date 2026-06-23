// tracing.go — Issue #3689 (epic #3628, area #11).
//
// Extracts OpenTelemetry distributed-tracing span-creation sites from Python
// function/method bodies and decorators, and stamps an INSTRUMENTS edge from
// the enclosing operation entity → a synthetic span stub ("span:<name>").
//
// Dominant OpenTelemetry Python idioms in scope:
//
//	with tracer.start_as_current_span("checkout"):   # context-manager form
//	    ...
//	span = tracer.start_span("db.query")             # manual form
//
//	@tracer.start_as_current_span("handle")          # decorator form
//	def handle(): ...
//
// The method name must be start_as_current_span or start_span on a
// tracer-like receiver. We do NOT require the receiver to literally be named
// "tracer" — any `<recv>.start_as_current_span(...)` / `<recv>.start_span(...)`
// call qualifies, because OTel tracers are routinely bound to other names
// (self.tracer, _TRACER, get_tracer(__name__), etc.). The method name is the
// discriminating signal; OTel reserves these exact method names.
//
// Honest-partial rule (#3689): when the first positional argument is NOT a
// string literal (a variable, attribute, f-string, or call) the span name is
// dynamic. We emit the edge with traced=true + dynamic=true and key the stub
// on the enclosing function name ("span:<fn>") rather than fabricating a name.
//
// The walk is tolerant: a panic inside the traversal is recovered so the
// primary extraction pipeline is unaffected.
package python

import (
	"strconv"

	"github.com/cajasmota/grafel/internal/treesitter/ts"

	"github.com/cajasmota/grafel/internal/types"
)

// pySpanHit captures one OpenTelemetry span-creation site found in a Python
// function/method body or decorator list.
type pySpanHit struct {
	spanName string // static span name; "" when dynamic
	api      string // "start_as_current_span" | "start_span"
	line     int    // 1-indexed source line of the call
	dynamic  bool   // true when the span name is not a string literal
}

// pyTracingSpanMethods is the set of OpenTelemetry tracer method names that
// create a span. These names are OTel-reserved API surface, so matching on the
// method name (rather than the receiver identifier) is both precise and robust
// to the many ways a tracer object is bound.
var pyTracingSpanMethods = map[string]bool{
	"start_as_current_span": true,
	"start_span":            true,
}

// extractPythonSpanHits walks a function/method node and returns one pySpanHit
// per OpenTelemetry span-creation site found in its body OR in its decorators.
// The decoratorParent node, when non-nil, is the decorated_definition wrapping
// funcNode; its decorator children are scanned for the decorator form.
func extractPythonSpanHits(funcNode, decoratorParent ts.Node, src []byte) []pySpanHit {
	if funcNode == nil {
		return nil
	}
	var hits []pySpanHit
	seen := make(map[string]bool)

	defer func() { _ = recover() }()

	add := func(h pySpanHit) {
		// Dedup on (api, name, dynamic) — a function instrumented with the same
		// span twice (rare) collapses to one edge, matching discriminator
		// semantics. Dynamic hits dedup per line so two distinct dynamic spans
		// in the same function both survive.
		key := h.api + "|" + h.spanName + "|" + strconv.FormatBool(h.dynamic)
		if h.dynamic {
			key += "|" + strconv.Itoa(h.line)
		}
		if seen[key] {
			return
		}
		seen[key] = true
		hits = append(hits, h)
	}

	// Decorator form: scan decorator nodes attached to the decorated_definition.
	if decoratorParent != nil {
		for i := 0; i < int(decoratorParent.ChildCount()); i++ {
			ch := decoratorParent.Child(i)
			if ch == nil || ch.Type() != "decorator" {
				continue
			}
			// A decorator child wraps either an `attribute` (bare @x.y) or a
			// `call` (@x.y(...)). Only the call form carries a span-name arg.
			callNode := findFirstChildOfType(ch, "call")
			if callNode == nil {
				continue
			}
			if h, ok := pySpanFromCall(callNode, src); ok {
				add(h)
			}
		}
	}

	// Body forms: context-manager `with tracer.start_as_current_span(...)` and
	// manual `tracer.start_span(...)`. Both reduce to a `call` node anywhere in
	// the body whose function is `<recv>.<spanMethod>`.
	body := funcNode.ChildByFieldName("body")
	if body != nil {
		for _, callNode := range findAll(body, "call") {
			if h, ok := pySpanFromCall(callNode, src); ok {
				add(h)
			}
		}
	}

	return hits
}

// pySpanFromCall inspects a `call` node and returns a pySpanHit when it is an
// OpenTelemetry span-creation call (`<recv>.start_as_current_span(...)` or
// `<recv>.start_span(...)`).
func pySpanFromCall(call ts.Node, src []byte) (pySpanHit, bool) {
	if call == nil || call.Type() != "call" {
		return pySpanHit{}, false
	}
	fn := call.ChildByFieldName("function")
	if fn == nil || fn.Type() != "attribute" {
		return pySpanHit{}, false
	}
	attr := fn.ChildByFieldName("attribute")
	if attr == nil {
		return pySpanHit{}, false
	}
	method := nodeText(attr, src)
	if !pyTracingSpanMethods[method] {
		return pySpanHit{}, false
	}

	line := int(call.StartPoint().Row) + 1
	hit := pySpanHit{api: method, line: line}

	// First positional argument is the span name. A string literal yields a
	// static name; anything else is dynamic (honest-partial).
	args := call.ChildByFieldName("arguments")
	nameNode := firstPositionalArg(args)
	if nameNode != nil && nameNode.Type() == "string" {
		name := pythonLiteralValue(nameNode, src)
		if name != "" {
			hit.spanName = name
			return hit, true
		}
	}
	// Dynamic / missing literal name → traced flag without a fabricated name.
	hit.dynamic = true
	return hit, true
}

// firstPositionalArg returns the first positional argument node inside an
// argument_list, skipping the structural "(" "," ")" tokens and keyword
// arguments. Returns nil when there is no positional argument.
func firstPositionalArg(args ts.Node) ts.Node {
	if args == nil {
		return nil
	}
	for i := 0; i < int(args.ChildCount()); i++ {
		ch := args.Child(i)
		if ch == nil {
			continue
		}
		switch ch.Type() {
		case "(", ")", ",":
			continue
		case "keyword_argument":
			// e.g. name="..." — OTel's first positional is the name; a
			// keyword-only call (name=expr) is treated as dynamic by falling
			// through (we do not parse the keyword value here).
			continue
		default:
			return ch
		}
	}
	return nil
}

// findFirstChildOfType returns the first descendant of root whose Type() equals
// kind (depth-first), or nil. Used to locate the `call` inside a `decorator`.
func findFirstChildOfType(root ts.Node, kind string) ts.Node {
	if root == nil {
		return nil
	}
	all := findAll(root, kind)
	if len(all) == 0 {
		return nil
	}
	return all[0]
}

// stampPythonTracingSpans emits INSTRUMENTS edges on the entity at index idx in
// out for every OpenTelemetry span-creation site found in funcNode's body or
// decorators. Called immediately after the function/method entity is appended.
//
// enclosingName is the bare function/method name, used to key the synthetic
// stub for dynamic span names ("span:<fn>").
func stampPythonTracingSpans(funcNode, decoratorParent ts.Node, enclosingName string, src []byte, out *[]types.EntityRecord, idx int) {
	if funcNode == nil || out == nil || idx < 0 || idx >= len(*out) {
		return
	}
	hits := extractPythonSpanHits(funcNode, decoratorParent, src)
	if len(hits) == 0 {
		return
	}
	e := &(*out)[idx]
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
			toID = "span:" + enclosingName
		} else {
			props["span_name"] = h.spanName
			toID = "span:" + h.spanName
		}
		e.Relationships = append(e.Relationships, types.RelationshipRecord{
			ToID:       toID,
			Kind:       string(types.RelationshipKindInstruments),
			Properties: props,
		})
	}
}
