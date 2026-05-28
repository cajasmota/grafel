// Payload-shape substrate (#2770 Phase 2A).
//
// For every HTTP endpoint we want to know the field shapes of the
// request and the response on both sides of the wire — the producer
// (server-side handler) and the consumer (client call-site). The
// generic drift detector at internal/links/payload_drift.go cross-
// references the two and emits SchemaDrift findings when fields
// observed on one side are missing on the other.
//
// Per the substrate split (mirrors Phase 0 / 1A / 1B):
//
//   - Per-language sniffers are pure functions over file content.
//     Stateless, deterministic, nil-safe. They emit PayloadShape
//     records pinned to a declaring function name (the handler or
//     the caller).
//   - The generic drift pass owns the cross-repo join. It walks the
//     cross-repo HTTP links produced by http_pass.go, resolves
//     producer / consumer handlers and callers to entities, and
//     pulls the payload shapes registered against each side.
//
// Storage model: no new persistent entity kind. Shapes live in a
// sidecar JSON document keyed by (repo, file, function, direction),
// and the drift findings live in their own sidecar
// <group>-links-payload-drift.json read by the new MCP tool
// archigraph_payload_drift.
//
// Adding a new language: implement a PayloadShapeSniffFn, register it
// via RegisterPayloadShapeSniffer("<lang>", fn) in init() in the
// per-language file (payload_shapes_<lang>.go). Both directions
// (request/response) and both sides (producer/consumer) are emitted
// from the same sniffer when the language has both client and server
// idioms — most modern languages do.
package substrate

import "sort"

// PayloadDirection labels a shape as request- or response-side.
type PayloadDirection string

const (
	// PayloadDirectionRequest is the body of an inbound request that
	// the producer reads, or the body of an outbound request that the
	// consumer constructs.
	PayloadDirectionRequest PayloadDirection = "request"

	// PayloadDirectionResponse is the body of a response that the
	// producer writes, or the body of a response that the consumer
	// reads.
	PayloadDirectionResponse PayloadDirection = "response"
)

// PayloadSide labels a shape as producer- or consumer-side.
type PayloadSide string

const (
	// PayloadSideProducer is the server-side handler reading the
	// request body or writing the response body.
	PayloadSideProducer PayloadSide = "producer"

	// PayloadSideConsumer is the client call-site building the request
	// body or destructuring the response body.
	PayloadSideConsumer PayloadSide = "consumer"
)

// PayloadField is one observed field name on a payload shape.
//
// Phase 2A is intentionally conservative on optional/required: most
// dynamic languages don't statically annotate optionality, so we leave
// Optional zero-valued by default. The Java sniffer (where @NotNull /
// Optional<T> are observable) is the exception.
type PayloadField struct {
	// Name is the field name exactly as observed in source. The drift
	// detector applies case-normalisation (#2703) to bridge camelCase
	// consumer fields against snake_case producer fields.
	Name string

	// Type is the observed static type when the language records one
	// (Java DTO members, TypeScript interface members), else empty.
	Type string

	// Optional is true when the language explicitly annotates the
	// field as optional (Java Optional<T>, TS `field?:`). Phase 2A
	// defaults to false — absence of annotation is not proof of
	// required-ness.
	Optional bool
}

// PayloadShape is one observed payload shape on one side of one
// endpoint, attributed to a declaring function.
//
// Producer-side shapes describe what the handler reads off the
// request / writes onto the response. Consumer-side shapes describe
// what the call-site constructs / destructures. The drift detector
// joins the two via the cross-repo HTTP links.
type PayloadShape struct {
	// Function is the declaring handler / caller name. The pass binds
	// it back to a graph entity by (repo, file, function-name) — the
	// same lexical attribution shape constant_propagation /
	// effect_propagation use.
	Function string

	// Line is the 1-indexed source line of the shape evidence (the
	// first field-access or first literal property). Used by the MCP
	// tool when reporting why a drift was flagged.
	Line int

	// Direction is request | response.
	Direction PayloadDirection

	// Side is producer | consumer.
	Side PayloadSide

	// Fields are the observed field names (deduplicated by Name in
	// source order; the sniffer guarantees no duplicates).
	Fields []PayloadField

	// Confidence is the per-shape confidence in [0, 1]. Direct
	// matches against an inline literal (`{ name, email }`) are 1.0;
	// heuristic shapes (assembled from multiple field-accesses inside
	// a handler body) drop to 0.8 to reflect missed-branch risk.
	Confidence float64

	// EndpointHint is an optional string the sniffer can use to bind
	// the shape to a specific URL when one is observable on the call
	// site (e.g. `axios.post("/api/users", {...})`). The drift
	// detector uses the hint to disambiguate when a single function
	// constructs multiple bodies for multiple endpoints. Empty when
	// the call site has no inline URL (the binding then falls back to
	// (function, declaring entity) which is unique in practice).
	EndpointHint string

	// VerbHint is an optional HTTP verb observable at the call site
	// (`axios.post` → "POST"). Used as a tiebreaker by the drift
	// detector when a function constructs both request and response
	// bodies for distinct endpoints.
	VerbHint string
}

// PayloadShapeSniffFn is the contract every per-language payload-
// shape sniffer satisfies. Deterministic: identical content yields
// identical slices in source order.
type PayloadShapeSniffFn func(content string) []PayloadShape

// payloadShapeRegistry holds the registered per-language sniffers.
// Populated by init() in each per-language file.
var payloadShapeRegistry = map[string]PayloadShapeSniffFn{}

// RegisterPayloadShapeSniffer installs a sniffer for a language slug.
// Last-wins on duplicate registration; intended for init()-time wiring.
func RegisterPayloadShapeSniffer(lang string, fn PayloadShapeSniffFn) {
	if lang == "" || fn == nil {
		return
	}
	payloadShapeRegistry[lang] = fn
}

// PayloadShapeSnifferFor returns the registered sniffer for lang, or
// nil when none is registered. Callers must nil-check.
func PayloadShapeSnifferFor(lang string) PayloadShapeSniffFn {
	return payloadShapeRegistry[lang]
}

// PayloadShapeLanguages returns the slugs of every language with a
// registered payload-shape sniffer in sorted order.
func PayloadShapeLanguages() []string {
	out := make([]string, 0, len(payloadShapeRegistry))
	for k := range payloadShapeRegistry {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// NormalizeFieldName lowercases and strips non-alphanumerics from a
// field name. The drift detector uses this to bridge camelCase vs
// snake_case differences (#2703 pattern). Exposed so the MCP tool can
// echo the normalised form back to callers for clarity.
func NormalizeFieldName(s string) string {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'A' && c <= 'Z':
			out = append(out, c+32)
		case (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9'):
			out = append(out, c)
		}
	}
	return string(out)
}

// DedupFields returns fields with duplicate Names removed, preserving
// source order. Sniffers call this to satisfy the no-duplicates
// guarantee in PayloadShape.Fields.
func DedupFields(in []PayloadField) []PayloadField {
	if len(in) <= 1 {
		return in
	}
	seen := make(map[string]bool, len(in))
	out := in[:0]
	for _, f := range in {
		if f.Name == "" || seen[f.Name] {
			continue
		}
		seen[f.Name] = true
		out = append(out, f)
	}
	return out
}
