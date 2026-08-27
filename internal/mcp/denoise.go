// denoise.go — MCP serving-layer result de-noising and re-ranking (#1614).
//
// The graph carries many synthetic / structural entities that are useful for
// traversal but pure noise in a ranked find/search result: file-and-module
// CONTAINER components, inferred class-hierarchy shadows, raw SCOPE.Pattern nodes
// (e.g. error_handling:try_catch:N), Schema field members, and Process nodes for
// array built-ins. Worse, because these often share the BM25 score of a real
// match (label substring), they frequently rank ABOVE the real lined entity the
// agent actually wants.
//
// This file classifies entities into noise buckets and provides a stable
// re-rank comparator so that real, lined, qualified entities sort first. It is
// purely a serving-layer concern — no extraction state is touched (other grinds
// own internal/extractors and internal/engine).
//
// Noise tiers (ascending = worse rank):
//
//	noiseNone (0)       — real entity; ranks by BM25 and start_line presence
//	noiseShadow (4)     — inferred class-hierarchy / implicit-method shadow
//	noiseContainer (5)  — file/module CONTAINER Component
//	noiseProcess (6)    — array/string built-in Process node
//	noiseSchemaField (7) — SCOPE.Schema subtype=field member (#1715)
//	noisePattern (8)    — SCOPE.Pattern structural node (#1733)
//	noiseLocalScope (9) — non-addressable function-body local binding (#1748)
package mcp

import (
	"sort"
	"strings"

	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/types"
)

// noiseKind enumerates the de-noise buckets. noiseNone means the entity is a
// real, surfacable result.
type noiseKind int

const (
	noiseNone noiseKind = iota
	// noiseContainer: a file/module CONTAINER Component — its label is the
	// source-file path and it has no body (start_line==0). Useful for
	// traversal, never a useful ranked hit.
	noiseContainer
	// noiseShadow: an inferred class-hierarchy / implicit-method shadow. Either
	// carries provenance==INFERRED_FROM_CLASS_HIERARCHY, or has an empty
	// qualified_name AND start_line==0 (e.g. drf_viewset_implicit_method
	// LoginViewSet.list / .retrieve / .update — bodies that don't exist in
	// source).
	noiseShadow
	// noisePattern: a raw structural Pattern node (SCOPE.Pattern), e.g.
	// error_handling:try_catch:N. Not the agent-learned AgentPattern kind.
	noisePattern
	// noiseProcess: a Process node for an array/string built-in call, e.g.
	// "Login → map", "Foo → trim". Identified by the proc: ID prefix and/or
	// the "X → builtin" label shape.
	noiseProcess
	// noiseSchemaField: a SCOPE.Schema entity with Subtype="field" — a
	// single field/attribute belonging to a serializer or schema class (e.g.
	// DeficiencyCreateSerializer.amount). These are member entities of their
	// parent class and clutter default results when ~25 fields accompany every
	// serializer. Suppressed by default (#1712); the parent class surfaces
	// normally. Reachable via include_noise:true.
	noiseSchemaField
	// noiseLocalScope: a non-addressable local binding emitted inside a
	// function/method body (#1748). Examples: `const { counts } = someData`
	// or `const [a, b] = arr` inside a React component. These are kept in
	// the graph so the resolver can bind REFERENCES/CALLS edges, but they
	// are never independently inspectable via grafel_inspect (the name
	// is not addressable as "Component.counts") so surfacing them in
	// grafel_find wastes tokens and violates "everything you see is
	// queryable". Identified by Properties["local_scope"]=="true".
	noiseLocalScope
)

// arrayBuiltins is the set of array/string built-in method names whose Process
// nodes (e.g. "Login → map") are pure noise in a ranked result.
var arrayBuiltins = map[string]bool{
	"map": true, "filter": true, "reduce": true, "forEach": true,
	"some": true, "every": true, "find": true, "findIndex": true,
	"includes": true, "indexOf": true, "join": true, "split": true,
	"trim": true, "slice": true, "splice": true, "concat": true,
	"push": true, "pop": true, "shift": true, "unshift": true,
	"sort": true, "reverse": true, "flat": true, "flatMap": true,
	"keys": true, "values": true, "entries": true, "toLowerCase": true,
	"toUpperCase": true, "replace": true, "padStart": true, "padEnd": true,
}

// classifyNoise returns the noise bucket for an entity. noiseNone means the
// entity is a real, surfacable result.
func classifyNoise(e *graph.Entity) noiseKind {
	if e == nil {
		return noiseNone
	}
	bareKind := strings.ToLower(stripScopePrefix(e.Kind))

	// Process nodes for built-ins: the local ID carries a "proc:" segment and
	// the label has the "X → builtin" shape.
	if bareKind == "process" || strings.Contains(e.ID, "proc:") {
		if _, builtin := splitProcessBuiltin(e.Name); builtin {
			return noiseProcess
		}
	}

	// Raw structural Pattern nodes (NOT the agent-learned AgentPattern kind,
	// which has no SCOPE. prefix and is surfaced deliberately).
	// Match both the canonical "SCOPE.Pattern" kind and any bare "pattern"
	// variant that extractors may emit without the scope prefix.
	if e.Kind == string(types.EntityKindPattern) || bareKind == "pattern" {
		return noisePattern
	}

	// File/module container Component: label == source-file path, no body.
	//
	// #2015: also check the top-level Subtype field. The extractor stamps the
	// subtype into BOTH the EntityRecord.Subtype field and Properties["subtype"]
	// (extractor.FileEntity), but some load / conversion paths only repopulate
	// one of them. Checking both makes the classification robust regardless of
	// which side carried the value through to the loaded graph.Entity. We also
	// relax the StartLine==0 gate: the python extractor's #1964 finalize sweep
	// stamps StartLine=1 onto previously-zero entities — including file
	// containers — so a real file Component can arrive here with StartLine>0
	// yet still be the synthetic file node whose only role is anchoring
	// file-level IMPORTS/REFERENCES edges.
	subtype := e.Subtype
	if subtype == "" {
		subtype = e.PropGet("subtype")
	}
	if bareKind == "component" && (subtype == "file" || subtype == "module") {
		return noiseContainer
	}
	if bareKind == "component" && e.StartLine == 0 {
		if e.PropGet("subtype") == "file" || e.PropGet("subtype") == "module" {
			return noiseContainer
		}
		// Fallback: label literally equals the source file path.
		if e.SourceFile != "" && e.Name == e.SourceFile {
			return noiseContainer
		}
	}

	// Inferred class-hierarchy / implicit-method shadows.
	if prov := e.PropGet("provenance"); prov == "INFERRED_FROM_CLASS_HIERARCHY" {
		return noiseShadow
	}
	if e.StartLine == 0 && e.QualifiedName == "" {
		// Bodiless, unqualified entity. Endpoint kinds legitimately have
		// start_line==0 (they are route declarations, not source bodies) — keep
		// those. Same for external/datastore/queue/event kinds that model
		// non-source resources.
		if !isStructuralLineless(bareKind) {
			return noiseShadow
		}
	}

	// Schema field members (#1712): a SCOPE.Schema entity whose Subtype is
	// "field" is a single attribute of a parent serializer/schema class
	// (e.g. DeficiencyCreateSerializer.amount). ~25 of these accompany every
	// serializer class and pollute default ranked results. The parent class
	// entity (same Kind, no "field" Subtype) surfaces normally.
	if bareKind == "schema" && e.Subtype == "field" {
		return noiseSchemaField
	}

	// Non-addressable function-body locals (#1748): emitted at extraction
	// time for resolver use but not independently inspectable. The extractor
	// stamps Properties["local_scope"]="true" on these entities.
	//
	// #6472 — component_prop is excluded, and this is the bucket's own rule
	// applied correctly, NOT an exception to it. Membership in noiseLocalScope
	// is justified above by "not independently inspectable": a const_destructure
	// binding has no name you can address, so returning it from a search is
	// noise. A React component_prop fails that test — the emitter
	// (internal/extractors/javascript/dataflow_react.go) gives it a real
	// QualifiedName ("Component.prop"), a real StartLine/EndLine span, and a
	// ComputeID()-derived ID that grafel_inspect accepts. It is addressable, so
	// it is not in the bucket.
	//
	// The property is nonetheless stamped on props, because internal/resolve's
	// isLocalBindingKind needs a different fact from the same key: "must not
	// compete for the repository-wide byName slot". A props parameter is
	// addressable (denoise's question) AND callable-local (the resolver's
	// question); those two answers differ, and this carve-out is where they are
	// kept apart. Removing it would delete a component's whole prop surface
	// from grafel_find's default and kind_filter paths.
	if e.PropGet("local_scope") == "true" && e.Subtype != "component_prop" {
		return noiseLocalScope
	}

	return noiseNone
}

// isStructuralLineless reports whether a bare (scope-stripped, lowercased) kind
// is one that legitimately has start_line==0 and is NOT a shadow — i.e. it
// models a route/resource rather than a source body.
func isStructuralLineless(bareKind string) bool {
	switch bareKind {
	case "http_endpoint", "http_endpoint_definition", "http_endpoint_call",
		"endpoint", "route", "externalapi", "datastore", "dataaccess",
		"queue", "event", "infraresource", "messagetopic", "service",
		"externalpackage", "external", "config":
		return true
	}
	return false
}

// splitProcessBuiltin parses a Process label of the form "Caller → builtin" and
// reports whether the right-hand side is a known array/string built-in.
func splitProcessBuiltin(label string) (string, bool) {
	// Labels use a unicode right-arrow; tolerate "->" as well.
	for _, sep := range []string{" → ", "→", " -> ", "->"} {
		if i := strings.Index(label, sep); i >= 0 {
			rhs := strings.TrimSpace(label[i+len(sep):])
			return rhs, arrayBuiltins[rhs]
		}
	}
	return "", false
}

// isNoise reports whether the entity is in any noise bucket.
func isNoise(e *graph.Entity) bool { return classifyNoise(e) != noiseNone }

// rankTier returns a coarse ranking tier for an entity; LOWER is better. The
// caller combines tier with BM25 score so that within a tier the BM25 order is
// preserved, but a real entity always outranks a shadow/container/pattern.
//
// THE PROSE HERE USED TO CONTRADICT THE MAP BELOW IT — it said "structural
// lineless ... tier 2" and "every noise bucket tier 3+" while the map has
// lineless at 1, generated at 2 and the first noise bucket at 4. The map is
// authoritative and is the only description kept:
//
//	0 — real lined entity (start_line > 0)
//	1 — lineless but legitimate (endpoint/resource)
//	2 — machine-generated source (#6329)
//	4 — noiseShadow (inferred class-hierarchy / implicit-method shadow)
//	5 — noiseContainer (file/module CONTAINER Component)
//	6 — noiseProcess (array/string built-in Process node)
//	7 — noiseSchemaField (SCOPE.Schema subtype=field member, #1712)
//	8 — noisePattern (SCOPE.Pattern structural node, #1733)
func rankTier(e *graph.Entity) int { return tierFor(e, true) }

// generatedTier is the tier assigned to machine-generated source. Named
// because rerankScored has to be able to ask "is this hit demoted?" without
// re-deriving the number.
const generatedTier = 2

// tierFor computes the tier. demoteGenerated is false only for the single
// strongest-match exemption in rerankScored — see there for the argument.
func tierFor(e *graph.Entity, demoteGenerated bool) int {
	switch classifyNoise(e) {
	case noiseContainer:
		return 5
	case noiseShadow:
		return 4
	case noiseProcess:
		return 6
	case noiseSchemaField:
		return 7
	case noisePattern:
		// SCOPE.Pattern nodes (e.g. error_handling:try_catch:N) rank below all
		// other noise tiers — they are structural enrichment signals, never
		// direct answers to a user search query (#1733).
		return 8
	}
	// Real entity. Lined entities (whether or not they carry a qualified_name)
	// share the top tier so that BM25 relevance — not the mere presence of a
	// qualified_name — orders them. Lineless-but-legitimate entities (routes /
	// resources, e.g. endpoint definitions) sit just below.
	// Machine-generated source (#6329) ranks below BOTH authored tiers.
	//
	// A lineless authored entity is still something a person wrote and may be
	// exactly what the user was looking for; a generated declaration is
	// derivable from something else in the repository. When both match a
	// query, the authored one is more likely to be the answer.
	//
	// This is checked AFTER the noise switch above, so a generated entity that
	// is also a shadow keeps its noise tier — the demotion must never PROMOTE
	// a node out of a noise bucket.
	//
	// It is deliberately NOT a noise bucket. Noise is hidden behind
	// include_noise, and #6329 exists precisely because generated declarations
	// have to stay in the graph and stay findable: the WinForms case is
	// `Handles btnSave.Click` in the authored half resolving against
	// `Friend WithEvents btnSave As Button` in the designer half.
	//
	// Tier 2 leaves 3 free on the noise side and sits one clear step below the
	// authored tiers, so a further disposition can be inserted on either side
	// without renumbering the noise buckets.
	//
	// A score penalty was rejected: FuseRRF replaces BM25 magnitudes with rank
	// reciprocals, so any multiplicative penalty inside Search is erased on
	// every repository that has an embeddings sidecar — correct-looking, green
	// on repos without embeddings, and inert on the ones that matter.
	if demoteGenerated && e.PropGet(types.EntityGeneratedProperty) == "true" {
		return generatedTier
	}

	if e.StartLine > 0 {
		return 0
	}
	return 1 // lineless but legitimate (endpoint/resource)
}

// rerankScored applies the tier ordering to a ranked result set in place.
//
// THIS IS THE PRODUCTION COMPARATOR AND TESTS CALL IT DIRECTLY. It used to be
// an anonymous sort.SliceStable closure inside handleQueryGraph, with the
// #6329 tests carrying their own copy of it — so a change to one could pass
// while the other stayed green on a paraphrase.
//
// # The strongest-match exemption
//
// The demotion is a partition: every authored hit sorts before every generated
// one, whatever the scores. The package doc for internal/generated claimed
// that a wrong detection therefore "costs the file some ranking position, not
// its entities". IN THE DEFAULT OUTPUT PATH THAT WAS FALSE. Downstream of this
// sort the default views keep only the first few rows — per-repo top 3 in a
// group, the first 10 in single-repo compact mode — so an absolute partition
// means three weak authored matches silently delete a generated hit from the
// default view entirely. Combined with a false positive, a wrongly-flagged
// hand-written file does not get demoted, it DISAPPEARS. That is the #6338
// failure mode this change set out to avoid.
//
// So the partition is relaxed in exactly one place: the single generated hit
// that outscores EVERY authored hit in the result set keeps its authored tier.
// The adversarial shape it answers is a protobuf message name that exists only
// in user.pb.go — weightFileStem = 1.5 puts it top on BM25, and the partition
// would drop it below every weak authored match for a query that has no other
// answer.
//
// THE TRADE-OFF, STATED. Exempting every generated hit that outranks the best
// authored one would restore #6314 wholesale: that issue IS the case where a
// crowd of generated declarations outranks the authored answer. Exempting only
// the top-ranked hit gives up one row and keeps the other N demoted, so a
// query with 50 generated matches still surfaces the authored ones from row 2.
// The rule is "the demotion may cost a generated entity ranking positions, but
// it may never cost a repo's best match its place".
//
// # The exemption is POSITIONAL, and per repo
//
// The first version of it compared hit.Score, and that walked straight back
// into the trap that killed the score-penalty design one paragraph above:
// hit.Score is an RRF reciprocal on every repo that has an embeddings sidecar.
// With two-list fusion the generated and authored hits routinely land at
// mirrored ranks, which makes the fused scores EXACTLY equal (1/(k+1) +
// 1/(k+2) on both sides), so "strictly outscores" never held and the exemption
// was green without embeddings and inert with them — measured on the same
// query, 0.281182 vs 0.215672 without a sidecar, 0.032522 vs 0.032522 with one.
//
// Worse, `all` mixes score SCALES: handleQueryGraph appends raw BM25 scores for
// repos without a sidecar and RRF reciprocals for repos with one, into one
// slice. A global score comparison over that slice compares incomparable
// numbers, and it got the answer wrong in both directions — denying the
// exemption to a generated hit that tops its own repo because an unrelated repo
// was on a bigger scale, and granting it to one its own repo ranks below an
// authored hit.
//
// So the decision is made on POSITION within each repo's own ranked run, where
// the scale is uniform by construction: a generated hit is exempt exactly when
// it is the top-ranked non-noise hit in its repo. Position is what fusion
// preserves; magnitude is not. At most one hit per repo is exempt, so the
// #6314 crowd is still demoted.
//
// TIE-BREAK, STATED. Scores are never compared, so an exact RRF tie is not
// resolved by an accident of insertion or map iteration: the exempt hit is the
// EARLIER of the tied hits in the ranker's own order, which is deterministic
// (BM25 tie-breaks on ascending doc index; FuseRRF sorts stably over insertion
// order). When the ranker puts the authored hit first, no generated hit in
// that repo is exempt.
//
// Cross-repo ordering of the final list still mixes scales — two authored hits
// from a sidecar repo and a non-sidecar repo were already sorted against each
// other by raw magnitude before this change, and the default view sections its
// output per repo. The exemption no longer contributes to that.
func rerankScored(all []scored) {
	tiers := make(map[*graph.Entity]int, len(all))
	for i := range all {
		tiers[all[i].hit.Entity] = rankTier(all[i].hit.Entity)
	}
	for e := range exemptGeneratedHits(all, tiers) {
		tiers[e] = tierFor(e, false)
	}
	sort.SliceStable(all, func(i, j int) bool {
		ti, tj := tiers[all[i].hit.Entity], tiers[all[j].hit.Entity]
		if ti != tj {
			return ti < tj
		}
		return all[i].hit.Score > all[j].hit.Score
	})
}

// exemptGeneratedHits returns the generated entities that hold the
// strongest-match exemption: at most ONE per repo — that repo's top-ranked
// non-noise hit, when that hit is generated. See rerankScored for why the rule
// is positional and per repo rather than a score comparison.
//
// Each repo's hits arrive in `all` in that repo's own ranked order (Search and
// FuseRRF both return highest-first, and the handler appends per repo), so
// slice position IS rank and no re-sort is needed to read it.
//
// Noise (tier > generatedTier) is skipped entirely: a shadow must not be able
// to hold the exemption, and it must not be able to BLOCK one either by
// occupying the top slot of a run.
func exemptGeneratedHits(all []scored, tiers map[*graph.Entity]int) map[*graph.Entity]bool {
	type runState struct {
		gen          *graph.Entity
		haveAuthored bool
	}
	byRepo := map[*LoadedRepo]*runState{}
	for i := range all {
		e := all[i].hit.Entity
		t := tiers[e]
		if t > generatedTier {
			continue
		}
		st := byRepo[all[i].repo]
		if st == nil {
			st = &runState{}
			byRepo[all[i].repo] = st
		}
		if t < generatedTier {
			st.haveAuthored = true
			continue
		}
		// Generated. It holds the exemption only if it is the first non-noise
		// hit seen in this repo's run — i.e. no authored hit outranks it — and
		// only the first such hit, so a tie is decided by the ranker's order.
		if st.gen == nil && !st.haveAuthored {
			st.gen = e
		}
	}
	out := map[*graph.Entity]bool{}
	for _, st := range byRepo {
		if st.gen != nil {
			out[st.gen] = true
		}
	}
	return out
}
