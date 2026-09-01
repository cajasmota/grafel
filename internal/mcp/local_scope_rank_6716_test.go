package mcp

import (
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/types"
)

// #6716 — noiseLocalScope had no case in tierFor, so a non-addressable
// function-body local fell through into the "Real entity." block below the
// switch and was scored on the authored/lineless/generated tiers. Under
// include_noise:true — the one path where the tier matters, because
// handleQueryGraph drops noise BEFORE ranking otherwise — a local binding was
// ranked as a real answer and could take a top slot away from one.
//
// THE PROPERTY PINNED HERE IS NOT "a local sorts low". Owner's decision on
// #6716: noise is opt-in context, never an answer, so turning on include_noise
// may only ADD material at the BOTTOM of the ranking. Concretely: the ranked
// list with noise must begin with the ranked list without noise, identical and
// in the same order. A weaker "the local appears near the bottom" assertion
// would stay green while noise reordered or displaced the real entities above
// it, which is the actual harm.
//
// None of the assertions below read the tier constant. They observe the
// ORDERING that the constant produces, so mutating the constant (to equal, or
// better than, a real entity's tier) is visible to them.

// localBinding builds the shape classifyNoise puts in noiseLocalScope (#1748):
// a lined, named destructure binding carrying local_scope=true and a subtype
// that is NOT component_prop. It is lined on purpose — a lineless one would
// land in the shadow bucket and the test would pass for the wrong reason.
func localBinding(name string, line int, score float64) scored {
	e := graph.EntityPtr(graph.Entity{
		Kind:       "SCOPE.Component",
		Subtype:    "const_destructure",
		Name:       name,
		SourceFile: "src/features/ContractProposals.jsx",
		StartLine:  line,
	}.WithProperties(map[string]string{
		"kind":        "SCOPE.Component",
		"subtype":     "const_destructure",
		"local_scope": "true",
	}))
	if classifyNoise(e) != noiseLocalScope {
		panic("fixture drift: localBinding is not classified noiseLocalScope")
	}
	return scored{hit: Hit{Entity: e, Score: score}}
}

// dropNoise reproduces handleQueryGraph's include_noise:false filter
// (tools.go), which runs BEFORE rerankScored.
func dropNoise(all []scored) []scored {
	out := make([]scored, 0, len(all))
	for _, sc := range all {
		if isNoise(sc.hit.Entity) {
			continue
		}
		out = append(out, sc)
	}
	return out
}

func names(all []scored) []string {
	out := make([]string, 0, len(all))
	for _, sc := range all {
		out = append(out, sc.hit.Entity.Name)
	}
	return out
}

// TestIncludeNoise_OnlyAppends_LocalScopeNeverDisplacesARealEntity_6716 is the
// property. The input is built so that BM25 score alone would put a local
// binding at the very top (score 9) and a second one in the middle (score 2),
// which is exactly what the missing tier case allowed.
func TestIncludeNoise_OnlyAppends_LocalScopeNeverDisplacesARealEntity_6716(t *testing.T) {
	// A lineless-but-legitimate real entity (tier 1) so the "real prefix"
	// spans more than one real tier and a mutant that merges the local into
	// any of them is caught.
	endpoint := scored{hit: Hit{Entity: &graph.Entity{
		Name:       "GET /proposals",
		Kind:       string(types.EntityKindEndpoint),
		SourceFile: "src/routes.js",
	}, Score: 4}}

	withNoise := []scored{
		// Ranked order as the searcher emits it: descending score.
		localBinding("counts", 48, 9),
		authoredEntity("ProposalCounts", 12, 5),
		endpoint,
		authoredEntity("ProposalService", 30, 3),
		localBinding("rows", 51, 2),
		authoredEntity("ProposalRow", 77, 1),
	}
	withoutNoise := dropNoise(withNoise)
	if len(withoutNoise) != 4 {
		t.Fatalf("fixture: expected 4 real hits after the include_noise:false filter, got %d (%v)",
			len(withoutNoise), names(withoutNoise))
	}

	got := rerank(withNoise)
	want := rerank(withoutNoise)

	if len(got) < len(want) {
		t.Fatalf("include_noise:true returned fewer hits (%d) than include_noise:false (%d)", len(got), len(want))
	}
	// The ranked prefix must be IDENTICAL — same entities, same order, same
	// positions. Anything the noise adds must sit strictly after it.
	for i := range want {
		if got[i].hit.Entity != want[i].hit.Entity {
			t.Fatalf("include_noise:true changed the ranked prefix of real entities at position %d:\n"+
				" with noise: %v\n  no noise: %v\n"+
				"noise is opt-in context and may only be APPENDED; it must never displace or reorder a real entity (#6716)",
				i, names(got), names(want))
		}
	}
	// And everything past the prefix is noise, i.e. nothing real was pushed
	// out of the prefix either.
	for i := len(want); i < len(got); i++ {
		if !isNoise(got[i].hit.Entity) {
			t.Fatalf("real entity %q sorted below the noise tail at position %d: %v",
				got[i].hit.Entity.Name, i, names(got))
		}
	}
}

// TestRankTier_LocalScopeBelowEveryRealDisposition_6716 states the "always"
// half of the decision directly: below every real entity, whatever real
// disposition it holds — lined, lineless-legitimate, or machine-generated.
// Generated is included deliberately: it is NOT a noise bucket (#6329
// requires generated declarations to stay findable), so a local must rank
// below it too.
func TestRankTier_LocalScopeBelowEveryRealDisposition_6716(t *testing.T) {
	local := localBinding("counts", 48, 1).hit.Entity

	reals := map[string]*graph.Entity{
		"authored lined": authoredEntity("ProposalService", 30, 1).hit.Entity,
		"lineless legitimate": {
			Name:       "GET /proposals",
			Kind:       string(types.EntityKindEndpoint),
			SourceFile: "src/routes.js",
		},
		"machine-generated": genEntity("User", 17, 1).hit.Entity,
	}
	for label, real := range reals {
		if rankTier(local) <= rankTier(real) {
			t.Errorf("local binding tier %d does not rank below %s tier %d",
				rankTier(local), label, rankTier(real))
		}
	}
}

// TestRankTier_LocalScopeDemotionIsNotOverBroad_6716 is the permissive-side
// guard. The demotion must key off the noiseLocalScope CLASSIFICATION and
// nothing wider — in particular a React component_prop carries local_scope=true
// for internal/resolve's benefit but is addressable, so classifyNoise
// deliberately excludes it (#6472). A tier rule written against the raw
// local_scope property instead of the bucket would demote a component's whole
// prop surface below every noise bucket.
func TestRankTier_LocalScopeDemotionIsNotOverBroad_6716(t *testing.T) {
	prop := graph.EntityPtr(graph.Entity{
		Kind:          "SCOPE.Component",
		Subtype:       "component_prop",
		Name:          "ProposalCard.title",
		QualifiedName: "ProposalCard.title",
		SourceFile:    "src/features/ProposalCard.jsx",
		StartLine:     9,
	}.WithProperties(map[string]string{
		"kind":        "SCOPE.Component",
		"subtype":     "component_prop",
		"local_scope": "true",
	}))
	if classifyNoise(prop) != noiseNone {
		t.Fatalf("fixture drift: component_prop must not be a noise bucket (#6472)")
	}
	authored := authoredEntity("ProposalCard", 3, 1).hit.Entity
	if rankTier(prop) != rankTier(authored) {
		t.Errorf("component_prop tier %d != authored lined tier %d; the #6716 demotion must key off "+
			"the noiseLocalScope bucket, not the raw local_scope property",
			rankTier(prop), rankTier(authored))
	}

	// A module-scope binding of the same subtype carries no local_scope
	// property at all and must keep the authored tier.
	moduleScope := &graph.Entity{
		Kind: "SCOPE.Component", Subtype: "const_destructure",
		Name: "foo", SourceFile: "src/features/Widget.jsx", StartLine: 3,
	}
	if rankTier(moduleScope) != rankTier(authored) {
		t.Errorf("module-scope binding tier %d != authored lined tier %d", rankTier(moduleScope), rankTier(authored))
	}
}
