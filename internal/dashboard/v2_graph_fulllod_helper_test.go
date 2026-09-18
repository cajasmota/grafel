package dashboard

import "testing"

// v2_graph_fulllod_helper_test.go — the shared `full`-LoD test helper (#7157).
//
// This helper used to live in v2_graph.go as `buildV2Graph`, a five-arg wrapper
// over buildV2GraphWithLimits that hard-coded lodLimits("full"). It had ZERO
// production callers — both production entry points (serveV2Graph in
// v2_graph.go and the stream handler in v2_graph_stream.go) call
// buildV2GraphWithLimits directly with caps from lodLimits(options.LOD) — while
// carrying a doc comment that read as a live production description. What the
// tests actually need from it is the `full` tier pin, so it belongs here, in
// test code, with the tier stated in its name.

// fullLoDLimits returns the caps of the `full` LoD tier. It is the single place
// the tests resolve that tier, so TestFullLoDHelperPinsFullTierCaps grades it:
// if this ever stopped naming "full" it would hand back some other tier's caps
// — `normal` via lodLimits' default branch for any unknown string, or an
// explicitly named tier — and every caller below would keep passing while
// grading the wrong tier.
func fullLoDLimits(tb testing.TB) (nodeCap, edgeCap int) {
	tb.Helper()
	return lodLimits("full")
}

// buildV2GraphFullLoD builds the v2 graph payload at the `full` LoD. Tests use
// it to pin that tier; production derives its caps from the request's LoD
// parameter instead and calls buildV2GraphWithLimits directly.
func (s *Server) buildV2GraphFullLoD(tb testing.TB, repos []*DashRepo, grp *DashGroup, filterKind string, includeExternal, includeModules bool) v2GraphResponse {
	tb.Helper()
	nodeCap, edgeCap := fullLoDLimits(tb)
	return s.buildV2GraphWithLimits(repos, grp, filterKind, includeExternal, includeModules, nodeCap, edgeCap)
}

// TestFullLoDHelperPinsFullTierCaps asserts the caps the helper ACTUALLY uses,
// not merely that they are non-zero (which is all TestLodLimitsAreFinite
// proves). Without this row, a helper that resolved to any other tier — most
// easily lodLimits' `normal` default branch, reached by any string that is not
// one of the four known tiers — would leave the LoD-compact test comparing a
// mis-tiered "full" baseline against itself and still passing.
func TestFullLoDHelperPinsFullTierCaps(t *testing.T) {
	nodeCap, edgeCap := fullLoDLimits(t)

	if nodeCap != fullLodNodeCap {
		t.Errorf("full-LoD helper nodeCap = %d; want fullLodNodeCap %d", nodeCap, fullLodNodeCap)
	}
	if edgeCap != fullEdgeCap {
		t.Errorf("full-LoD helper edgeCap = %d; want fullEdgeCap %d", edgeCap, fullEdgeCap)
	}

	// No other tier may be indistinguishable from full on BOTH axes at once,
	// otherwise the two assertions above could be satisfied by a tier that
	// merely happens to share the whole cap pair. Sharing ONE cap is
	// deliberately allowed: this fires only on a full pair match, so e.g.
	// highLodNodeCap == fullLodNodeCap with a different edge cap passes
	// (scored — it does). One differing axis is all the assertions above need,
	// and requiring both would forbid a tier pairing that is a legitimate
	// design choice rather than a defect.
	for _, tier := range []string{"overview", "low", "high", "normal"} {
		otherNodes, otherEdges := lodLimits(tier)
		if otherNodes == nodeCap && otherEdges == edgeCap {
			t.Errorf("lodLimits(%q) = (%d, %d), indistinguishable from the full tier — this test can no longer tell the tiers apart", tier, otherNodes, otherEdges)
		}
	}

	// And the default branch (an unknown tier) must not be the full tier
	// either — that is the exact silent-fallback this row exists to catch.
	defNodes, defEdges := lodLimits("not-a-tier")
	if defNodes == nodeCap && defEdges == edgeCap {
		t.Errorf("lodLimits' default branch returns the full caps (%d, %d) — a mistyped tier would be undetectable", defNodes, defEdges)
	}

	// The assertions above grade fullLoDLimits ONLY. They do not grade that
	// buildV2GraphFullLoD routes through it, because a mutant that replaces its
	// `fullLoDLimits(tb)` call with a direct lodLimits(<other tier>) leaves
	// fullLoDLimits untouched and every assertion above passing. Scored at that
	// site for `normal` AND for `high`: both were ALIVE before this subtest
	// covered their tier.
	//
	// That is why the fixture straddles the caps of the LARGEST non-full tier
	// (`high`), not the `normal` ones: a fixture sized only above `normal`
	// leaves every tier between `normal` and `full` untruncated, so it grades
	// the `normal` end of the axis and nothing else. Sized above `high` it
	// covers the whole tier axis at once — `overview`/`low` and `normal` are
	// below `high` on both axes, so any tier the build helper could resolve to
	// except `full` truncates this fixture.
	t.Run("PayloadUntruncatedAtASizeOnlyFullAdmits", func(t *testing.T) {
		const entityCount, relCount = 30_000, 150_000

		// Vacuity guard, per axis: if the fixture ever drops to or below the
		// `high` caps on EITHER axis it stops discriminating `high` from
		// `full` on that axis while still passing. `high` is the largest
		// non-full tier, so clearing it clears `normal`, `low` and `overview`
		// too.
		if entityCount <= highLodNodeCap || relCount <= highEdgeCap {
			t.Fatalf("fixture (%d nodes, %d edges) is not above the HIGH caps (%d, %d) — it can no longer tell the high tier from the full one", entityCount, relCount, highLodNodeCap, highEdgeCap)
		}
		if entityCount >= fullLodNodeCap || relCount >= fullEdgeCap {
			t.Fatalf("fixture (%d nodes, %d edges) is at or above the FULL caps (%d, %d) — the full tier would truncate it too", entityCount, relCount, fullLodNodeCap, fullEdgeCap)
		}

		grp := makeCompactLODFixture(entityCount, relCount)
		resp := (&Server{}).buildV2GraphFullLoD(t, sortedRepos(grp), grp, "", false, false)

		if resp.NodeTruncated {
			t.Errorf("NodeTruncated = true for %d nodes; the full tier caps nodes at %d, so the helper is not building at the full tier (served %d of %d)", entityCount, fullLodNodeCap, len(resp.Nodes), resp.TotalNodeCount)
		}
		if resp.EdgeTruncated {
			t.Errorf("EdgeTruncated = true for %d edges; the full tier caps edges at %d, so the helper is not building at the full tier (served %d of %d)", relCount, fullEdgeCap, len(resp.Edges), resp.TotalEdgeCount)
		}
		if len(resp.Nodes) != entityCount {
			t.Errorf("served nodes = %d; want all %d", len(resp.Nodes), entityCount)
		}
	})
}
