// Process-flow entity + edge kind constants (#724).
//
// These constants are intentionally kept in their own file so the typed-
// kind allowlist in internal/types/kinds.go is not perturbed while the
// process-flow feature is rolled out append-only. They live in package
// engine because that is the package that emits them; downstream
// consumers (MCP, doc-gen) reference the string values directly.
package engine

// EntityKindProcess identifies a Process entity — a linearized call chain
// emitted by RunProcessFlow. Name: "<entry> → <terminal>".
const EntityKindProcess = "SCOPE.Process"

// DefaultFlowMinSteps is the minimum step_count for a Process to appear in
// the DEFAULT flows list (#1639). Flows shorter than this are most likely
// not meaningful end-to-end processes (a 2-3 step chain is usually a
// helper call, not a business flow). The engine still EMITS shorter flows
// — they remain queryable by passing a lower min_steps threshold to the
// flows list endpoints / MCP trace tool — but they are excluded from the
// default view to cut false-positive noise.
const DefaultFlowMinSteps = 4

// RelationshipKindStepInProcess identifies a Process → step edge.
// The step_index property (0-based) records position in the chain.
const RelationshipKindStepInProcess = "STEP_IN_PROCESS"

// RelationshipKindEntryPointOf identifies the entry-function → Process
// edge that marks a function as the entry for a Process.
const RelationshipKindEntryPointOf = "ENTRY_POINT_OF"

// RelationshipKindCalls re-exports the canonical CALLS string for use in
// this package without an internal/types import cycle.
const RelationshipKindCalls = "CALLS"

// RelationshipKindFetches is the consumer-side HTTP fetch edge: caller
// function → synthetic consumer http_endpoint. Emitted by
// http_endpoint_resolve.go for any consumer synthetic that carries a
// resolvable `source_caller` property (per-language extractor wave-1
// work in #721 will emit the same kind directly once it lands).
//
// The process-flow BFS treats FETCHES as a traversable edge AND as the
// canonical signal that a chain crosses a repo boundary: the consumer
// http_endpoint is the bridge node that the cross-repo HTTP linker
// pairs with a producer-side endpoint in another repo. Without a
// FETCHES edge into the consumer endpoint, the BFS has no way to reach
// the bridge, and the chain can never be (correctly) marked
// cross_stack=true. See issue #754.
const RelationshipKindFetches = "FETCHES"

// EntityKindEndpoint, EntityKindRoute, EntityKindExternalAPI are referenced
// by chainCrossesStack and match the canonical SCOPE.* names produced by
// the per-language extractors. Duplicated here as raw strings to avoid an
// internal/types import for a single-use lookup.
const (
	EntityKindEndpoint    = "SCOPE.Endpoint"
	EntityKindRoute       = "SCOPE.Route"
	EntityKindExternalAPI = "SCOPE.ExternalAPI"
)
