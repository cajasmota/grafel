// SCOPED request-input → sink dataflow substrate (#3628 roadmap area #22).
//
// Per-language sniffers lift, per source file, a set of DataFlow records:
// a value read from an HTTP request input that reaches a recognised sink
// (DB write / outbound HTTP call / response body) within a single
// function body, plus ONE hop into a directly-called local function via
// positional argument binding.
//
// This is deliberately a SCOPED def→use tracker, NOT a full taint engine.
// The propagation model is "simple assignment tracking, last-write-wins":
//
//   - Source: a request-input access (`req.body.x`, `request.GET.get('x')`,
//     DRF `serializer.validated_data['x']`, …). The accessed field name is
//     captured when statically knowable.
//   - Propagation: `const y = <source>` taints y; a direct pass-through
//     `sink(<source>)` flows; `helper(y)` where helper is a local function
//     binds y into helper's matching positional parameter and continues
//     exactly ONE level deeper.
//   - Sink: DB write, an argument to a CONSUMES_API outbound call, or a
//     response-body emission.
//
// HONEST-PARTIAL boundary (precision over recall — this is the bar for a
// dataflow product). The sniffer DROPS, never fabricates, when it cannot
// soundly follow a value:
//   - reassignment that breaks the chain (`y = somethingElse` after taint)
//   - branch/merge of differently-tainted values
//   - collection / object mutation (`obj.x = tainted`, `arr.push(tainted)`)
//   - cross-file flow beyond the single one-hop local call
//   - dynamic field access (`req.body[dynamicKey]`)
//   - more than one hop of inter-procedural depth
//
// Per-language sniffers are pure functions over file content, stateless
// and deterministic, mirroring the def-use / effect-sink substrate.
package substrate

import "sort"

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

// DataFlow is one resolved source→sink flow within a function body
// (optionally crossing exactly one local-call hop).
type DataFlow struct {
	// Function is the request handler function the flow ORIGINATES in —
	// i.e. the function that reads the request input. For a one-hop flow
	// the sink physically appears inside the callee, but the flow is
	// attributed to the originating handler so the emitted edge starts at
	// the entity that owns the untrusted input. The callee name is carried
	// in HopVia.
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

	// HopVia, when non-empty, is the name of the local function the value
	// was passed into (one-hop inter-procedural). Empty for intra-fn flows.
	HopVia string
}

// DataFlowSnifferFn is the contract for per-language dataflow sniffers.
// Returns every soundly-followed source→sink flow in the file, in source
// order. Must be deterministic so the pass output is byte-stable.
type DataFlowSnifferFn func(content string) []DataFlow

var dataFlowRegistry = map[string]DataFlowSnifferFn{}

// RegisterDataFlowSniffer installs a per-language dataflow sniffer.
func RegisterDataFlowSniffer(lang string, fn DataFlowSnifferFn) {
	if lang == "" || fn == nil {
		return
	}
	dataFlowRegistry[lang] = fn
}

// DataFlowSnifferFor returns the registered sniffer for lang, or nil.
func DataFlowSnifferFor(lang string) DataFlowSnifferFn {
	return dataFlowRegistry[lang]
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
