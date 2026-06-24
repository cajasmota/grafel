// tracing.go — Issue #3689 (epic #3628, area #11); extended in #5500
// (epic #5479, JS/TS stack coverage).
//
// Extracts OpenTelemetry distributed-tracing span-creation sites from JS/TS
// function/method/arrow bodies and stamps an INSTRUMENTS edge from the
// enclosing operation entity → a synthetic span stub ("span:<name>"). This is
// the JS/TS parity of the Python tracing pass (extractors/python/tracing.go):
// same edge direction (enclosing fn → span), same prop shape.
//
// Dominant OpenTelemetry JS/TS idioms in scope:
//
//	const span = tracer.startSpan('checkout');
//	tracer.startActiveSpan('db.query', (span) => { ... });
//	trace.getTracer('svc').startSpan('name');           // inline tracer
//	registerOTel('my-app');                             // @vercel/otel setup
//	context.with(trace.setSpan(ctx, span), () => {...}); // manual span scope
//
// We match a call_expression whose function is a member_expression
// `<recv>.startSpan` / `<recv>.startActiveSpan` and whose first argument is a
// string literal (the span name). startSpan/startActiveSpan are OTel-reserved
// Tracer API surface, so matching on the property name is precise; we do not
// resolve the receiver type (which would need cross-file flow analysis).
//
// Attribution gate (#5500): the call is accepted only when the file imports an
// `@opentelemetry/*` (or `@vercel/otel`) package OR the receiver is a tracer-
// like expression (`tracer`, `*Tracer`, `trace.getTracer(...)`). This keeps an
// unrelated `.startSpan(` from a non-OTel library (e.g. a date-range picker)
// from being misattributed, mirroring the Python pass's OTel-import scoping.
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
	"strings"

	"github.com/cajasmota/grafel/internal/treesitter/ts"

	"github.com/cajasmota/grafel/internal/types"
)

// fileHasOTelImport reports whether the file imports an OpenTelemetry or
// @vercel/otel package — the canonical OTEL attribution signal. Computed from
// the already-collected import bindings (x.imports), so it is import-rooted and
// does not depend on a regex over the raw source.
func (x *extractor) fileHasOTelImport() bool {
	for i := range x.imports {
		p := x.imports[i].importPath
		if strings.HasPrefix(p, "@opentelemetry/") || p == "@vercel/otel" {
			return true
		}
	}
	return false
}

// isTracerLikeReceiver reports whether a member-expression receiver text looks
// like an OpenTelemetry Tracer — a conventional `tracer` binding, a name ending
// in `Tracer`, or an inline `trace.getTracer(...)` chain. Used as the second
// arm of the attribution gate (import OR tracer-receiver) so a span call on a
// clearly-tracer receiver is honoured even in a file whose OTel import the
// resolver did not capture (e.g. a re-exported wrapper), while an unrelated
// `.startSpan(` on a non-tracer receiver in a non-OTel file is dropped.
func isTracerLikeReceiver(recv string) bool {
	r := strings.TrimSpace(recv)
	if r == "" {
		return false
	}
	if strings.Contains(r, "getTracer") {
		return true
	}
	// Last dotted segment: `this.tracer`, `svc.appTracer`, bare `tracer`.
	last := r
	if i := strings.LastIndexByte(r, '.'); i >= 0 {
		last = r[i+1:]
	}
	return last == "tracer" || strings.HasSuffix(last, "Tracer")
}

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

	// Attribution gate (#5500): require an OTEL import at file scope, OR a
	// tracer-like receiver on the individual call. The import is the primary
	// filter; the receiver discriminator rescues genuine OTel calls in files
	// whose import the resolver did not capture, while still dropping a stray
	// `.startSpan(` on an unrelated receiver in a non-OTel file.
	hasImport := x.fileHasOTelImport()

	add := func(h jsSpanHit) {
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

	for _, call := range findAllNodes(body, "call_expression") {
		fn := call.ChildByFieldName("function")
		if fn == nil {
			continue
		}

		// @vercel/otel registerOTel('service'?) — app-level instrumentation
		// setup. Gated on the OTEL import (no receiver to discriminate on).
		if fn.Type() == "identifier" && x.nodeText(fn) == "registerOTel" {
			if !hasImport {
				continue
			}
			line := int(call.StartPoint().Row) + 1
			h := jsSpanHit{api: "registerOTel", line: line, dynamic: true}
			if args := call.ChildByFieldName("arguments"); args != nil {
				if nameNode := firstMeaningfulArg(args); nameNode != nil && nameNode.Type() == "string" {
					if name := stringLiteralValue(x.nodeText(nameNode)); name != "" {
						h.spanName = name
						h.dynamic = false
					}
				}
			}
			add(h)
			continue
		}

		if fn.Type() != "member_expression" {
			continue
		}
		propNode := fn.ChildByFieldName("property")
		if propNode == nil {
			continue
		}
		method := x.nodeText(propNode)

		// Manual context scope: context.with(trace.setSpan(...), cb). The span
		// object carries its own (runtime) name, so the site is dynamic and keyed
		// on the enclosing function. Gated on the OTEL import + a `trace.setSpan`
		// arg so a generic `<x>.with(...)` is not matched.
		if method == "with" && hasImport {
			if obj := fn.ChildByFieldName("object"); obj != nil && x.nodeText(obj) == "context" {
				if callMentions(x, call, "setSpan") {
					add(jsSpanHit{api: "context.with", line: int(call.StartPoint().Row) + 1, dynamic: true})
				}
			}
			continue
		}

		if !jsTracingSpanMethods[method] {
			continue
		}

		// Receiver discriminator. Accept when the file imports OTEL, or the
		// receiver itself is tracer-like (`tracer`, `*Tracer`, getTracer chain).
		recv := ""
		if obj := fn.ChildByFieldName("object"); obj != nil {
			recv = x.nodeText(obj)
		}
		if !hasImport && !isTracerLikeReceiver(recv) {
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

		add(hit)
	}
	return hits
}

// callMentions reports whether the call_expression's argument subtree contains a
// member-expression whose property is `prop` (e.g. `trace.setSpan(...)` inside a
// `context.with(...)`). A small bounded descendant scan — used to confirm a
// manual span scope before emitting its INSTRUMENTS edge.
func callMentions(x *extractor, call ts.Node, prop string) bool {
	args := call.ChildByFieldName("arguments")
	if args == nil {
		return false
	}
	for _, m := range findAllNodes(args, "member_expression") {
		if p := m.ChildByFieldName("property"); p != nil && x.nodeText(p) == prop {
			return true
		}
	}
	return false
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
