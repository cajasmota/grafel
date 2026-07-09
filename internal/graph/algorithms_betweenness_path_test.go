package graph

import "testing"

// TestChooseBetweennessPath is the #5692 gate: the pure selector must return
// exact-weighted for tiny graphs, exact-brandes for mid-size graphs, sampled
// above the sampling threshold, and must honour the force-exact opt-out (never
// sample when forced). Below the threshold the choice is IDENTICAL to the
// pre-#5692 behaviour (hard "small graphs unchanged" constraint).
func TestChooseBetweennessPath(t *testing.T) {
	const cutoff = 3000
	const thresh = 8000
	cases := []struct {
		name       string
		nodes      int
		forceExact bool
		want       betweennessPath
	}{
		{"tiny-exact-weighted", 100, false, betweennessPathExactWeighted},
		{"cutoff-boundary-weighted", 3000, false, betweennessPathExactWeighted},
		{"mid-exact-brandes", 5000, false, betweennessPathExactBrandes},
		{"threshold-boundary-not-sampled", 8000, false, betweennessPathExactBrandes},
		{"above-threshold-sampled", 9000, false, betweennessPathSampled},
		{"large-sampled", 291000, false, betweennessPathSampled},
		{"force-exact-overrides-sampling", 9000, true, betweennessPathExactBrandes},
		{"force-exact-tiny-still-weighted", 100, true, betweennessPathExactWeighted},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := chooseBetweennessPath(tc.nodes, cutoff, thresh, tc.forceExact)
			if got != tc.want {
				t.Fatalf("chooseBetweennessPath(%d, %d, %d, %v) = %s, want %s",
					tc.nodes, cutoff, thresh, tc.forceExact, got, tc.want)
			}
		})
	}
}

// TestChooseBetweennessPath_ThresholdDisabled verifies a non-positive sampling
// threshold disables sampling entirely (falls back to exact), so the operator
// can turn sampling off by setting a huge/zero threshold as well as via the
// force-exact flag.
func TestChooseBetweennessPath_ThresholdDisabled(t *testing.T) {
	if got := chooseBetweennessPath(1_000_000, 3000, 0, false); got != betweennessPathExactBrandes {
		t.Fatalf("sampleThreshold=0 should disable sampling: got %s, want exact-brandes", got)
	}
}

// TestBetweennessForceExactEnv verifies GRAFEL_BETWEENNESS_FORCE_EXACT parsing.
func TestBetweennessForceExactEnv(t *testing.T) {
	truthy := []string{"1", "true", "TRUE", "yes", "on", "T"}
	for _, v := range truthy {
		t.Setenv("GRAFEL_BETWEENNESS_FORCE_EXACT", v)
		if !betweennessForceExact() {
			t.Errorf("GRAFEL_BETWEENNESS_FORCE_EXACT=%q should be truthy", v)
		}
	}
	falsy := []string{"", "0", "false", "no", "off", "garbage"}
	for _, v := range falsy {
		t.Setenv("GRAFEL_BETWEENNESS_FORCE_EXACT", v)
		if betweennessForceExact() {
			t.Errorf("GRAFEL_BETWEENNESS_FORCE_EXACT=%q should be falsy", v)
		}
	}
}

// TestForceExactBypassesSamplingGate is the integration proof of the opt-out:
// with a deliberately tiny sampling threshold that WOULD sample a 200-node
// graph, setting GRAFEL_BETWEENNESS_FORCE_EXACT=1 makes ComputeCentrality
// produce EXACTLY the same betweenness as an exact run (threshold raised out of
// the way) — i.e. the force flag bypasses the gate and the sampled path never
// runs.
func TestForceExactBypassesSamplingGate(t *testing.T) {
	ents, rels := buildSyntheticGraph(200, 4, 3)
	g, idx := BuildGraph(ents, rels)

	// Forced exact, despite a threshold (10) far below the node count (200).
	t.Setenv("GRAFEL_BETWEENNESS_SAMPLE_THRESHOLD", "10")
	t.Setenv("GRAFEL_BETWEENNESS_FORCE_EXACT", "1")
	forced, _ := ComputeCentrality(g, idx)

	// Plain exact: threshold raised above the node count, no force.
	t.Setenv("GRAFEL_BETWEENNESS_SAMPLE_THRESHOLD", "100000")
	t.Setenv("GRAFEL_BETWEENNESS_FORCE_EXACT", "0")
	exact, _ := ComputeCentrality(g, idx)

	if len(forced) != len(exact) {
		t.Fatalf("key count mismatch: forced=%d exact=%d", len(forced), len(exact))
	}
	for id, want := range exact {
		if got := forced[id]; got != want {
			t.Fatalf("force-exact diverged from exact for %s: got %v want %v", id, got, want)
		}
	}
}
