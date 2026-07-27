package graph

import (
	"fmt"
	"sort"
	"testing"
)

// legacyIdentifyGodNodes is the pre-#5954 implementation, verbatim. It is the
// oracle for the parity tests below: before #5954 ComputeCentrality pre-seeded
// BOTH result maps with a 0 for every entity, so this ran over dense maps.
func legacyIdentifyGodNodes(betw, pr map[string]float64) map[string]bool {
	out := make(map[string]bool)
	if len(betw) == 0 && len(pr) == 0 {
		return out
	}
	pickTop5 := func(m map[string]float64) []string {
		type pair struct {
			id string
			v  float64
		}
		ps := make([]pair, 0, len(m))
		for k, v := range m {
			ps = append(ps, pair{k, v})
		}
		sort.SliceStable(ps, func(i, j int) bool {
			if ps[i].v != ps[j].v {
				return ps[i].v > ps[j].v
			}
			return ps[i].id < ps[j].id
		})
		k := len(ps) / 20 // 5%
		if k == 0 && len(ps) > 0 {
			k = 1
		}
		out := make([]string, 0, k)
		for i := 0; i < k; i++ {
			out = append(out, ps[i].id)
		}
		return out
	}
	for _, id := range pickTop5(betw) {
		out[id] = true
	}
	for _, id := range pickTop5(pr) {
		out[id] = true
	}
	return out
}

// densify reproduces the removed zero pre-seed: every entity id present with a
// score, defaulting to 0.
func densify(m map[string]float64, ents []Entity) map[string]float64 {
	out := make(map[string]float64, len(ents))
	for _, e := range ents {
		out[e.ID] = 0
	}
	for k, v := range m {
		out[k] = v
	}
	return out
}

// sparseGraph wires edges among only the first `wired` entities, leaving the
// rest isolated. With few non-zero betweenness scores and a 5% cut taken over
// ALL entities, the god-node selection must fall through to the zero tail —
// the case the removed pre-seed used to cover implicitly.
func sparseGraph(n, wired int) ([]Entity, []Relationship) {
	ents := make([]Entity, n)
	for i := 0; i < n; i++ {
		ents[i] = Entity{
			ID:         fmt.Sprintf("s%05d", i),
			Name:       fmt.Sprintf("S%d", i),
			Kind:       "function",
			SourceFile: "pkg/s.go",
			Language:   "go",
		}
	}
	var rels []Relationship
	for i := 0; i+1 < wired; i++ {
		rels = append(rels, Relationship{
			ID:     RelationshipID(ents[i].ID, ents[i+1].ID, "CALLS"),
			FromID: ents[i].ID,
			ToID:   ents[i+1].ID,
			Kind:   "CALLS",
		})
	}
	return ents, rels
}

// matchingGraph pairs each of the first n/2 entities with one of the last n/2
// (a perfect matching). No path has an interior node, so EVERY betweenness score
// is zero and the 5% cut is filled entirely from the zero tail — ordered by id,
// which is where the removed pre-seed's explicit zeros sorted. The matched
// targets carry the higher PageRank, so the PageRank half of the god-node union
// is disjoint from the betweenness half: dropping the tail is observable.
func matchingGraph(n int) ([]Entity, []Relationship) {
	ents, _ := sparseGraph(n, 0)
	half := n / 2
	rels := make([]Relationship, 0, half)
	for i := 0; i < half; i++ {
		from, to := ents[i].ID, ents[half+i].ID
		rels = append(rels, Relationship{
			ID:     RelationshipID(from, to, "CALLS"),
			FromID: from,
			ToID:   to,
			Kind:   "CALLS",
		})
	}
	return ents, rels
}

// TestGodNodesParityWithZeroPreSeed proves the #5954 removal of the zero
// pre-seed is output-neutral: ranking the sparse betweenness map over the
// entity universe selects exactly the god nodes the dense pre-seeded maps did.
func TestGodNodesParityWithZeroPreSeed(t *testing.T) {
	cases := []struct {
		name string
		ents []Entity
		rels []Relationship
	}{
		{"dense", nil, nil},       // filled below
		{"sparse-tail", nil, nil}, // filled below
		{"tiny", nil, nil},
		{"zero-tail-only", nil, nil},
	}
	cases[0].ents, cases[0].rels = buildSyntheticGraph(600, 4, 11)
	cases[1].ents, cases[1].rels = sparseGraph(400, 12)
	cases[2].ents, cases[2].rels = sparseGraph(7, 4)
	cases[3].ents, cases[3].rels = matchingGraph(400)

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			g, idx := BuildGraph(tc.ents, tc.rels)
			betw, pr := ComputeCentrality(g, idx)

			want := legacyIdentifyGodNodes(densify(betw, tc.ents), densify(pr, tc.ents))
			got := identifyGodNodesFrom(betw, pr, idx)

			if len(want) != len(got) {
				t.Fatalf("god-node count: legacy=%d new=%d", len(want), len(got))
			}
			for id := range want {
				if !got[id] {
					t.Errorf("god node %q selected by the legacy pre-seeded ranking but not by the new one", id)
				}
			}
			for id := range got {
				if !want[id] {
					t.Errorf("god node %q selected by the new ranking but not by the legacy pre-seeded one", id)
				}
			}
			t.Logf("%s: entities=%d betw-keys=%d (dense would be %d) gods=%d",
				tc.name, len(tc.ents), len(betw), len(tc.ents), len(got))
		})
	}
}

// TestEntityCentralityAttachmentParity proves the serialisation boundary is
// preserved: every entity that took part in the pass still receives an explicit
// centrality value (0 when it has no betweenness), so graph.json keeps the same
// key set it had when the pre-seed existed. It mirrors the attachment loops in
// cmd/grafel/index.go and dashboard.applyAlgorithmResults.
func TestEntityCentralityAttachmentParity(t *testing.T) {
	ents, rels := sparseGraph(300, 10)
	res := RunAlgorithms(ents, rels)

	attached := 0
	for _, e := range ents {
		pr, ok := res.PageRank[e.ID]
		if !ok {
			t.Fatalf("entity %q missing from PageRank: the attachment loops key centrality off it", e.ID)
		}
		_ = pr
		attached++
	}
	if attached != len(ents) {
		t.Fatalf("attached=%d want=%d", attached, len(ents))
	}
	if len(res.Centrality) >= len(ents) {
		t.Errorf("Centrality map has %d entries for %d entities — it must be sparse", len(res.Centrality), len(ents))
	}
	for id, v := range res.Centrality {
		if v == 0 {
			t.Errorf("Centrality holds an explicit zero for %q; zeros must be absent", id)
			break
		}
	}
}
