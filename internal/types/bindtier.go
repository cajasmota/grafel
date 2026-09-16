package types

// PropBindTier is the RelationshipRecord property key under which the
// reference resolver records WHICH TIER chose the edge's ToID, and it is
// written ONLY when that tier was a lexical guess: the target was picked
// from a bare name plus a locality (same file, same package directory,
// same crate, "the only one in the graph"), with no type evidence from the
// extractor.
//
// Issue #7071. Before this key existed, a CALLS edge bound because
// `Process` happened to be the only same-named method in the caller's file
// was byte-identical to one bound on a qualified name the extractor
// actually resolved. Every recall instrument we own — bind rate, orphan
// rate, dangle count, the golden ratchet's relationship_found — scores both
// as an equal success, so an extractor regression that stops typing
// receivers moves none of them.
//
// WHY NOT THE KEY "confidence". internal/graph/algorithms.go:572-586
// (edgeWeight) reads props["confidence"] as a float multiplier on the edge
// weight that feeds PageRank, betweenness, Louvain communities AND the
// graph-algorithm determinism hash. Writing a marker under that key — with
// any parseable sub-1.0 value — would silently re-weight every centrality
// and community number in every indexed corpus. That is a separate,
// separately-measured decision; this key is deliberately disjoint from it.
//
// WHY A PROPERTY AND NOT RelationshipRecord.Confidence. The FlatBuffers
// `Relationship` table (internal/graph/schema/graph.fbs:104-109) carries
// from_id / to_id / kind / properties and nothing else. A Confidence float
// is dropped at write time and reads back as 0 in the daemon, so a marker
// stored there could never reach the MCP surface, the dashboard, or
// `grafel quality`. Properties are in the schema and survive the round
// trip.
//
// ABSENCE MEANS "not a guess", which covers two different things: the edge
// was bound by an evidence tier (exact qualified name, a structural address
// the extractor minted, a resolved qualifier, a stamped receiver type), or
// it was never bound at all. Consumers that care about the difference must
// also look at whether ToID is an entity ID.
//
// SCOPE: the key describes the TO endpoint only, which is why it is named
// to_*. The same tiers can fire on a FROM endpoint rewrite
// (internal/resolve/refs.go's rewriteOneWithCaller is called for FromID
// too); those binds are deliberately NOT marked here, because one key per
// edge cannot carry two answers. That is a known, named gap, not an
// oversight.
const PropBindTier = "to_bind_tier"
