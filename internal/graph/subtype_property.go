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
// # Why here — and the mechanism, stated precisely
//
// DeriveSubtypeProperties is called from SortDocumentForEmission, the repo's
// existing pre-serialization normaliser: "the final, post-everything sort
// applied immediately before the graph is serialized", idempotent by
// construction so callers may run it more than once.
//
// THE GUARANTEE IS IN-PLACE NORMALISATION OF THE SHARED DOCUMENT BEFORE ANY
// ENCODER RUNS — NOT a property of the writers. An earlier version of this
// comment claimed "every graph written to disk goes through
// fbwriter.WriteGraphGenReport, and both of its branches call it", which is
// false for the second encoder and false in the one direction that matters:
//
//	graph.fb    fbwriter.WriteGraphGenReport → writeGraphGenFlat /
//	            WriteGraphGenSegmented, both of which call
//	            SortDocumentForEmission.
//	graph.json  graph.WriteAtomic (graph.go), which normalises NOTHING.
//
// The two encodings agree because the producers normalise the shared `doc`
// BEFORE either encoder is reached:
//
//	cmd/grafel/index.go:999             sortDocumentForEmission(doc), then the
//	                                    .fb write (:1006) and the optional
//	                                    graph.json write (:1094) from the SAME doc
//	internal/extractors/incremental.go:1700  sortGraphDocumentForEmission(doc)
//	                                    before the daemon's rewrite
//
// Those two call sites ARE the guarantee. index.go:999 is unconditional and
// independent of whether the .fb write succeeded — graph.json is still
// attempted after an .fb failure — so the normalised document is what both
// encoders see either way. fbwriter's own SortDocumentForEmission calls are
// belt-and-braces for callers that arrive un-normalised (and for Marshal /
// WriteAtomic used directly by tests); they are not what makes graph.json
// carry the key.
//
// That is also why the derivation mutates the DOCUMENT rather than the
// FlatBuffers entity leaf (fbwriter.buildEntity), which the repo documents as
// the sole entity-serialization leaf and which would otherwise be the stronger
// chokepoint: it is .fb-only, so graph.json — deliberately mtime-matched to
// graph.fb as "two encodings of the SAME index pass" (#1626) — would disagree
// with the binary graph about the very field this issue is about. The
// graph.json half of that agreement is graded end-to-end by
// cmd/grafel/subtype_property_json_parity_7105_test.go, which removing
// index.go:999 fails; without it the claim in this paragraph is unobserved.
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

// SubtypeConflict records one entity whose pre-existing Properties["subtype"]
// disagrees with its canonical Entity.Subtype. Measured disagreement across
// the 38 golden fixtures is 0/211, so this is empty on every known input; it
// exists so that a future producer stamping a second, different answer is
// visible rather than silently resolved.
type SubtypeConflict struct {
	EntityID  string
	Canonical string
	Property  string
}

// DeriveSubtypeProperties stamps Properties["subtype"] from the canonical
// Entity.Subtype on every entity that has a non-empty Subtype and no existing
// `subtype` property, and returns the entities whose existing property
// DISAGREES with the canonical field.
//
// It never overwrites an existing `subtype` property — the pre-existing value
// is the one Properties-only consumers see today, so preserving it is what
// makes this a derivation rather than a merge — never allocates a property map
// for an entity whose Subtype is empty, and is idempotent: a second call over
// the same document stamps nothing, which is what lets it sit in a funnel the
// producers invoke more than once per write.
//
// The returned conflicts are reported by the caller rather than counted and
// dropped. A disagreement resolved in silence is the shape that would be
// hardest to find later, and the report is the only artefact that can be
// observed; see SortDocumentForEmission.
func DeriveSubtypeProperties(doc *Document) []SubtypeConflict {
	if doc == nil {
		return nil
	}
	var conflicts []SubtypeConflict
	for i := range doc.Entities {
		e := &doc.Entities[i]
		if e.Subtype == "" {
			continue
		}
		if existing, ok := e.PropLookup("subtype"); ok {
			if existing != e.Subtype {
				conflicts = append(conflicts, SubtypeConflict{
					EntityID:  e.ID,
					Canonical: e.Subtype,
					Property:  existing,
				})
			}
			continue
		}
		e.PropSet("subtype", e.Subtype)
	}
	return conflicts
}
