package sresolver_test

import (
	"testing"

	"github.com/cajasmota/grafel/internal/extractors/sresolver"
	"github.com/cajasmota/grafel/internal/graph"
)

// #6129 — a synthesized SCOPE.External placeholder must never shadow a real
// in-repo entity of the same name in the scoped name index.
//
// Why this can only happen on the incremental path: `external.Synthesize` runs
// AFTER resolution on a full rebuild, so the corpus-wide resolver's name index
// never contains an `ext:` placeholder. The scoped resolver, by contrast, builds
// its index over `existingEntities` loaded from the PREVIOUS PERSISTED GRAPH —
// which is post-synthesis and therefore does contain them. With a plain
// last-writer-wins index, `ext:pkgbeta` overwrites the real `Module` named
// "pkgbeta" and every IMPORTS edge naming that module binds to the placeholder,
// asserting an external dependency where the source imports a local package.
//
// The assertion is on the BOUND TARGET, not on counts: the mis-bind produces
// the same number of entities and edges as a full rebuild (and even improves
// the dangling-endpoint metric), so no count-shaped check can see it.
func TestResolveScoped_ExternalPlaceholderDoesNotShadowRealEntity(t *testing.T) {
	// Existing (previous persisted graph) — real module first, placeholder
	// second, which is the order that makes last-writer-wins pick the wrong one.
	existing := []graph.Entity{
		{ID: "aaaa1111", Name: "pkgbeta", Kind: "Module", SourceFile: "pkgbeta/__init__.py"},
		{ID: "ext:pkgbeta", Name: "pkgbeta", Kind: "SCOPE.External", SourceFile: ""},
	}
	// …and the reverse order, which must land on the same answer.
	reversed := []graph.Entity{
		{ID: "ext:pkgbeta", Name: "pkgbeta", Kind: "SCOPE.External", SourceFile: ""},
		{ID: "aaaa1111", Name: "pkgbeta", Kind: "Module", SourceFile: "pkgbeta/__init__.py"},
	}

	for _, tc := range []struct {
		name string
		ents []graph.Entity
	}{
		{"real-then-placeholder", existing},
		{"placeholder-then-real", reversed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			newRels := []graph.Relationship{{ID: "r1", FromID: "bbbb2222", ToID: "pkgbeta", Kind: "IMPORTS"}}
			res := sresolver.ResolveScoped(
				[]graph.Entity{{ID: "bbbb2222", Name: "betamain.py", Kind: "SCOPE.Component", SourceFile: "betamain.py"}},
				tc.ents, newRels, nil, nil,
			)
			if res.FallbackRequired {
				t.Fatalf("unexpected fallback")
			}
			got := res.ResolvedNewRelationships[0].ToID
			if got != "aaaa1111" {
				t.Fatalf("IMPORTS bound to %q, want the real in-repo Module %q "+
					"(a SCOPE.External placeholder shadowed it)", got, "aaaa1111")
			}
		})
	}
}

// A name that has ONLY a SCOPE.External placeholder must still bind to it —
// the placeholder is deliberate for genuinely external references (#6129 is
// about a LOCAL module being classified external, not about the fallback
// existing at all).
func TestResolveScoped_ExternalPlaceholderStillBindsWhenNoRealEntity(t *testing.T) {
	existing := []graph.Entity{
		{ID: "ext:requests", Name: "requests", Kind: "SCOPE.External", SourceFile: ""},
	}
	newRels := []graph.Relationship{{ID: "r1", FromID: "bbbb2222", ToID: "requests", Kind: "IMPORTS"}}
	res := sresolver.ResolveScoped(
		[]graph.Entity{{ID: "bbbb2222", Name: "betamain.py", Kind: "SCOPE.Component", SourceFile: "betamain.py"}},
		existing, newRels, nil, nil,
	)
	if got := res.ResolvedNewRelationships[0].ToID; got != "ext:requests" {
		t.Fatalf("IMPORTS bound to %q, want the external placeholder %q", got, "ext:requests")
	}
}

// The rank added for #6129 orders candidates ACROSS ranks only. WITHIN a rank
// the index must stay last-writer-wins, exactly as it was before the change.
//
// This is pinned because the #6129 comment now ASSERTS the property, and
// because it was previously untested in either direction: the mutation
// `rank >= nameRank[name]` — first-writer-wins across the board — survived
// ./internal/extractors/..., ./internal/resolve/... and ./cmd/grafel/ with
// every suite exiting 0. Two same-rank entities sharing a name is the only
// shape that separates the two policies, and nothing in the corpus produced
// one, so the guard has to be written directly.
//
// Direction matters: `existingEntities` are indexed before `newEntities`, so a
// freshly extracted entity is the LAST writer and must win. That is what lets
// a re-extracted file's entity displace its own stale predecessor under the
// same name.
func TestResolveScoped_SameRankKeepsLastWriterWins(t *testing.T) {
	// Both real entities (rank 0), same name, different IDs.
	existing := []graph.Entity{
		{ID: "aaaa1111", Name: "Shadowed", Kind: "SCOPE.Operation", SourceFile: "first.py"},
		{ID: "bbbb2222", Name: "Shadowed", Kind: "SCOPE.Operation", SourceFile: "second.py"},
	}
	// A new entity under the same name is written last of all and must win over
	// BOTH existing ones.
	newEnts := []graph.Entity{
		{ID: "cccc3333", Name: "Caller", Kind: "SCOPE.Operation", SourceFile: "caller.py"},
		{ID: "dddd4444", Name: "Shadowed", Kind: "SCOPE.Operation", SourceFile: "third.py"},
	}
	newRels := []graph.Relationship{{ID: "r1", FromID: "cccc3333", ToID: "Shadowed", Kind: "CALLS"}}

	res := sresolver.ResolveScoped(newEnts, existing, newRels, nil, nil)
	if res.FallbackRequired {
		t.Fatalf("unexpected fallback")
	}
	if got := res.ResolvedNewRelationships[0].ToID; got != "dddd4444" {
		t.Fatalf("same-rank collision bound to %q, want the LAST writer %q — "+
			"within a rank the index must remain last-writer-wins, and a newly "+
			"extracted entity must be able to displace a stale same-named one",
			got, "dddd4444")
	}

	// The same policy among EXISTING entities alone: later wins.
	res2 := sresolver.ResolveScoped(
		[]graph.Entity{{ID: "cccc3333", Name: "Caller", Kind: "SCOPE.Operation", SourceFile: "caller.py"}},
		existing,
		[]graph.Relationship{{ID: "r1", FromID: "cccc3333", ToID: "Shadowed", Kind: "CALLS"}},
		nil, nil,
	)
	if got := res2.ResolvedNewRelationships[0].ToID; got != "bbbb2222" {
		t.Fatalf("same-rank collision among existing entities bound to %q, want "+
			"the LAST writer %q", got, "bbbb2222")
	}
}
