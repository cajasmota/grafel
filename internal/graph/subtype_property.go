package graph

// Issue #7105 — ONE FACT, TWO SPELLINGS, AND ONE OF THEM IS THE ONLY ONE TWO
// SURFACES CAN READ.
//
// `Entity.Subtype` is the canonical home of a sub-distinction (interface vs
// class, enum vs type_alias, import carrier, resource vs module, …).
// `Properties["subtype"]` is a second spelling of the same fact that 24
// producers stamp independently — 22 of them only for their `file` entity.
//
// Measured on all 38 golden fixtures through the real Pass-1 path (1069
// entities):
//
//	disagreement between the two spellings   0 / 211
//	Properties-only (no canonical field)     0 / 1069
//	canonical-only (no Properties twin)    796 / 1069  (74.5%)
//
// So `Subtype` is already the canonical source in practice and the duplicate
// never contradicts it — which is what makes deriving the duplicate from the
// canonical field behaviour-preserving rather than a merge.
//
// It matters because two consumers read the DUPLICATE ONLY and cannot see the
// canonical field at all:
//
//	internal/dashboard/graphstate.go   serializeEntity emits no `subtype` key;
//	                                   the wire shape's only route is `properties`
//	internal/docgen/llm_bundle.go      configEntryFromEntity reads props["subtype"]
//
// Those 796 rows are therefore invisible to the dashboard and to LLM-docgen
// today. Deriving closes that gap at one point instead of at 24 producers.
//
// # Why here
//
// DeriveSubtypeProperties is called from SortDocumentForEmission, which is the
// repo's existing pre-serialization funnel: "the final, post-everything
// [normalisation] applied immediately before the graph is serialized" — one
// implementation, reached by every production writer (fbwriter's flat and
// segmented producers, the daemon's incremental rewrite, and the determinism
// checker). Every graph written to disk goes through fbwriter.WriteGraphGen
// Report, and both of its branches call it.
//
// It mutates the in-memory document rather than the FlatBuffers leaf
// (fbwriter.buildEntity) on purpose. graph.json — the optional `--export-json`
// second encoding of the SAME index pass (cmd/grafel/index.go, written from
// the same `doc` AFTER the .fb write) — would otherwise disagree with the
// binary graph about the very field this issue is about. Normalising the
// document makes both encodings agree by construction.
//
// # What it must NOT do
//
// This is a WIDENING: it adds a key to three quarters of the corpus. The
// guard that matters is the empty one. An entity with an empty `Subtype` must
// stay ABSENT, not gain `subtype: ""`, because:
//
//   - to a Properties-only consumer `subtype: ""` is a value, not a silence;
//   - serializeEntity gates on `PropLen() > 0`, and several checks around the
//     indexer are written as `len(props) == 0`, so allocating a property map
//     for the majority of entities would change what they all see.
//
// Recall-style must-have assertions are blind to that direction, so it is
// graded by forbidden-row subtests in internal/dashboard and internal/docgen.

// DeriveSubtypeProperties stamps Properties["subtype"] from the canonical
// Entity.Subtype on every entity that has a non-empty Subtype and no existing
// `subtype` property, and returns how many entities it stamped.
//
// It never overwrites an existing `subtype` property (the two spellings never
// disagree on measured data, and the pre-existing value is the one consumers
// see today), never allocates a property map for an entity whose Subtype is
// empty, and is idempotent — a second call over the same document stamps
// nothing, which is what lets it sit in an idempotent normalisation funnel.
func DeriveSubtypeProperties(doc *Document) int {
	if doc == nil {
		return 0
	}
	stamped := 0
	for i := range doc.Entities {
		e := &doc.Entities[i]
		if e.Subtype == "" {
			continue
		}
		if _, ok := e.PropLookup("subtype"); ok {
			continue
		}
		e.PropSet("subtype", e.Subtype)
		stamped++
	}
	return stamped
}
