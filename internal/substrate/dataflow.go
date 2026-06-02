// SCOPED request-input → sink dataflow substrate (#3628 roadmap area #22).
//
// Per-language sniffers lift, per source file, a set of DataFlow records:
// a value read from an HTTP request input that reaches a recognised sink
// (DB write / outbound HTTP call / response body) through up to
// DataFlowMaxHops inter-procedural hops via positional argument binding,
// AND a set of DataFlowBoundary records for tainted values that escape the
// current file into an imported callee (resolved + continued by the links
// pass, which owns the import graph — see internal/links/dataflow_pass.go).
//
// This is deliberately a SCOPED def→use tracker, NOT a full taint engine.
// The propagation model is "simple assignment tracking, last-write-wins":
//
//   - Source: a request-input access (`req.body.x`, `request.GET.get('x')`,
//     DRF `serializer.validated_data['x']`, …). The accessed field name is
//     captured when statically knowable.
//   - Propagation: `const y = <source>` taints y; a direct pass-through
//     `sink(<source>)` flows; `helper(y)` where helper is a local function
//     binds y into helper's matching positional parameter and continues the
//     walk, up to DataFlowMaxHops levels deep. The chain of callees is
//     recorded in HopPath.
//   - Sink: DB write, an argument to a CONSUMES_API outbound call, or a
//     response-body emission.
//
// MULTI-HOP (≤ DataFlowMaxHops): the value is followed through nested local
// calls A→B→C, each binding by exact positional index. The set of callees
// already on the active call path is tracked so a recursion / cycle stops
// the walk (drop, never loop). A hop beyond the bound is dropped.
//
// CROSS-FILE: when a tainted value reaches a call whose callee is NOT a
// function defined in the current file, the sniffer cannot resolve the
// import (it has no import graph). Instead it records a DataFlowBoundary —
// the originating handler, the escaping callee name, the tainted positional
// argument index, the source field, and the hops already consumed. The
// links pass resolves that callee through the IMPORTS/CALLS edge to a real
// same-repo entity, reads the defining file, binds the value into the
// matching parameter, and CONTINUES the same bounded walk there. Only a
// callee that resolves to a concrete same-repo entity is followed; an
// external / unresolved import is dropped.
//
// HONEST-PARTIAL boundary (precision over recall — this is the bar for a
// dataflow product). The sniffer DROPS, never fabricates, when it cannot
// soundly follow a value:
//   - reassignment that breaks the chain (`y = somethingElse` after taint)
//   - branch/merge of differently-tainted values
//   - collection / object mutation (`obj.x = tainted`, `arr.push(tainted)`)
//   - a call whose argument position is ambiguous (spread `...args`,
//     destructured / rest parameters) — position cannot be bound exactly
//   - dynamic field access (`req.body[dynamicKey]`)
//   - more than DataFlowMaxHops hops of inter-procedural depth
//   - recursion / a cycle in the local call graph
//
// Per-language sniffers are pure functions over file content, stateless
// and deterministic, mirroring the def-use / effect-sink substrate.
package substrate

import "sort"

// DataFlowMaxHops bounds inter-procedural depth (number of call hops a
// tainted value is followed through, counting cross-file hops). Beyond this
// bound the walk is dropped — honest-partial. A direct intra-function sink
// is 0 hops; handler→helper→sink is 1 hop; handler→A→B→sink is 2 hops.
const DataFlowMaxHops = 3

// DataFlowSinkKind classifies the terminal of a flow.
type DataFlowSinkKind string

const (
	// DataFlowSinkDBWrite is a database write (ORM create/save/insert).
	DataFlowSinkDBWrite DataFlowSinkKind = "db_write"
	// DataFlowSinkHTTPCall is an outbound HTTP call argument (CONSUMES_API).
	DataFlowSinkHTTPCall DataFlowSinkKind = "http_call"
	// DataFlowSinkResponse is a response-body emission (res.json/Response).
	DataFlowSinkResponse DataFlowSinkKind = "response"
)

// DataFlow is one resolved source→sink flow originating in a handler,
// optionally crossing up to DataFlowMaxHops local-call hops.
type DataFlow struct {
	// Function is the request handler function the flow ORIGINATES in —
	// i.e. the function that reads the request input. For a multi-hop flow
	// the sink physically appears inside a callee, but the flow is
	// attributed to the originating handler so the emitted edge starts at
	// the entity that owns the untrusted input. The callee chain is carried
	// in HopPath.
	Function string

	// SourceField is the request-input field name when statically known
	// (e.g. "name" for `req.body.name`). Empty when the whole request
	// object flows or the field is not a static identifier.
	SourceField string

	// SourceLine is the 1-indexed line of the request-input read.
	SourceLine int

	// SinkKind classifies the terminal.
	SinkKind DataFlowSinkKind

	// SinkName is the recognised sink callee/expression as written
	// (e.g. "User.create", "res.json", "repo.insert"). Used to bind the
	// edge target and to render the flow for review.
	SinkName string

	// SinkLine is the 1-indexed line of the sink.
	SinkLine int

	// HopVia, when non-empty, is the name of the FIRST local function the
	// value was passed into. Retained for backward compatibility; it equals
	// HopPath[0] when HopPath is non-empty. Empty for intra-fn flows.
	HopVia string

	// HopPath is the ordered chain of callee function names the tainted
	// value traversed to reach the sink (empty for an intra-fn flow). For a
	// flow handler→A→B→sink it is ["A","B"]. len(HopPath) ≤ DataFlowMaxHops.
	HopPath []string
}

// DataFlowBoundary is a tainted value that ESCAPES the current file: it is
// passed (by exact positional index) into a callee that is not defined in
// this file. The sniffer cannot resolve the import — the links pass does,
// then continues the bounded walk in the resolved file. A boundary is only
// emitted when the argument position is unambiguous; spread / destructured
// call sites are dropped, never recorded.
type DataFlowBoundary struct {
	// Function is the originating handler (same meaning as DataFlow.Function).
	Function string
	// SourceField is the request-input field (may be "").
	SourceField string
	// SourceLine is the 1-indexed line of the request-input read.
	SourceLine int
	// Callee is the bare name of the escaping function call.
	Callee string
	// ArgIndex is the 0-based positional index of the tainted argument at the
	// call site (already self/cls-agnostic at the call side; the resolver maps
	// it to the callee's matching parameter).
	ArgIndex int
	// HopPath is the chain of local callees already traversed before this
	// boundary call (the call into Callee is the NEXT hop, counted by the
	// resolver). For a handler that directly calls an imported fn it is empty.
	HopPath []string
	// CallLine is the 1-indexed line of the escaping call (for determinism).
	CallLine int
}

// DataFlowResult bundles the in-file flows and the cross-file boundaries a
// sniffer found.
type DataFlowResult struct {
	Flows      []DataFlow
	Boundaries []DataFlowBoundary
}

// DataFlowSnifferFn is the contract for per-language dataflow sniffers.
// Returns every soundly-followed in-file source→sink flow in the file, in
// source order. Must be deterministic so the pass output is byte-stable.
// (In-file flows only; cross-file boundaries are exposed via the *Ex form.)
type DataFlowSnifferFn func(content string) []DataFlow

// DataFlowSnifferExFn is the extended contract: returns both in-file flows
// and the cross-file boundaries for the links pass to resolve. Languages
// that implement cross-file register one of these.
type DataFlowSnifferExFn func(content string) DataFlowResult

// DataFlowContinueFn continues a bounded hop walk inside a resolved file,
// starting from a parameter already bound to a tainted value. Used by the
// links pass for cross-file propagation. fnName is the callee whose body to
// enter; paramIndex is the 0-based parameter the tainted value binds to;
// field is the carried source field; hopsUsed is the number of hops already
// consumed reaching this file (so the continuation respects DataFlowMaxHops).
// It returns the in-file flows reachable from that binding, plus any further
// cross-file boundaries (for chained cross-file hops). Deterministic.
type DataFlowContinueFn func(content, fnName string, paramIndex int, field string, hopsUsed int) DataFlowResult

var dataFlowRegistry = map[string]DataFlowSnifferFn{}
var dataFlowExRegistry = map[string]DataFlowSnifferExFn{}
var dataFlowContinueRegistry = map[string]DataFlowContinueFn{}

// RegisterDataFlowSniffer installs a per-language dataflow sniffer.
func RegisterDataFlowSniffer(lang string, fn DataFlowSnifferFn) {
	if lang == "" || fn == nil {
		return
	}
	dataFlowRegistry[lang] = fn
}

// RegisterDataFlowSnifferEx installs a per-language extended sniffer (in-file
// flows + cross-file boundaries) AND a continuation function. The in-file
// flows are also exposed via the legacy DataFlowSnifferFor path so existing
// callers are unaffected.
func RegisterDataFlowSnifferEx(lang string, ex DataFlowSnifferExFn, cont DataFlowContinueFn) {
	if lang == "" || ex == nil || cont == nil {
		return
	}
	dataFlowExRegistry[lang] = ex
	dataFlowContinueRegistry[lang] = cont
	// Bridge to the legacy registry so DataFlowSnifferFor keeps working.
	dataFlowRegistry[lang] = func(content string) []DataFlow { return ex(content).Flows }
}

// DataFlowSnifferFor returns the registered (in-file) sniffer for lang, or nil.
func DataFlowSnifferFor(lang string) DataFlowSnifferFn {
	return dataFlowRegistry[lang]
}

// DataFlowSnifferExFor returns the extended sniffer for lang, or nil.
func DataFlowSnifferExFor(lang string) DataFlowSnifferExFn {
	return dataFlowExRegistry[lang]
}

// DataFlowContinueFor returns the cross-file continuation fn for lang, or nil.
func DataFlowContinueFor(lang string) DataFlowContinueFn {
	return dataFlowContinueRegistry[lang]
}

// DataFlowLanguages returns the slugs of every registered dataflow
// sniffer, sorted.
func DataFlowLanguages() []string {
	out := make([]string, 0, len(dataFlowRegistry))
	for k := range dataFlowRegistry {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
