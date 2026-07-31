//go:build perf

// Latency budget for the S3 incremental reindex path.
//
// Gated behind the `perf` build tag (see CONTRIBUTING.md, "Performance tests").
// The original 1s budget on this fixture was a 4-8x headroom claim — isolated
// runs of the 10-file repo land at 116-249ms — and it still went red at 1.073s
// inside a loaded full-suite run, and again at 1.081s locally with seven
// packages running in parallel. A budget that fails at 4x headroom is measuring
// the runner, not the code. It is re-budgeted to 3s here; see the assertion.
//
// The correctness half of this test (TryIncremental must take the incremental
// path and not fall back on a single-file edit) is asserted 30 other times in
// incremental_test.go, which stays in the release gate — see for instance
// TestIncremental_GraphFBReadableAfterWrite immediately below where this test
// used to sit, which does the same edit-then-TryIncremental and checks res.Done.
//
//	go test -tags perf ./internal/extractors/ -run Performance_SingleFileEdit -v

package extractors_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/extractors"
	"github.com/cajasmota/grafel/internal/graph"
)

func TestIncremental_Performance_SingleFileEdit(t *testing.T) {
	repo := t.TempDir()
	stateDir := t.TempDir()

	// Create a synthetic repo with 10 Go files (small but realistic for
	// the unit test; the 60 k-entity benchmark is an integration scenario).
	for i := 0; i < 10; i++ {
		src := fmt.Sprintf("package svc\n\nfunc Func%d() {}\nfunc Helper%d() {}\n", i, i)
		writeFile(t, repo, fmt.Sprintf("svc/svc%d.go", i), src)
	}

	// Build a baseline graph with placeholder entities.
	var entities []graph.Entity
	for i := 0; i < 10; i++ {
		for _, fn := range []string{fmt.Sprintf("Func%d", i), fmt.Sprintf("Helper%d", i)} {
			sf := fmt.Sprintf("svc/svc%d.go", i)
			entities = append(entities, graph.Entity{
				ID:         graph.EntityID("test-repo", "SCOPE.Operation", fn, sf),
				Name:       fn,
				Kind:       "SCOPE.Operation",
				SourceFile: sf,
				Language:   "go",
			})
		}
	}
	buildMinimalGraph(t, stateDir, entities, nil)
	seedManifest(t, repo, stateDir)

	// Mutate one file.
	writeFile(t, repo, "svc/svc0.go", "package svc\n\nfunc Func0Updated() {}\nfunc Helper0Updated() {}\n")

	t0 := time.Now()
	res := extractors.TryIncremental(context.Background(), repo, stateDir, nil, nil)
	dur := time.Since(t0)

	if !res.Done {
		t.Fatalf("incremental: unexpected fallback: %s", res.FallbackReason)
	}
	// Budget: 3s, not the original 1s.
	//
	// Measured on this fixture: 116-249ms with the package run in isolation;
	// 1.081s with seven packages running in parallel on the same host (that run
	// is what reproduced the CI red locally). 3s clears the contended number
	// with ~3x margin.
	//
	// Be honest about what this can and cannot detect. It is a SMOKE bound, not
	// a discriminator: on a 10-file fixture there is no measurable separation
	// between "incremental" and "degenerated into a full reindex", because a
	// full parse of 10 tiny files is itself fast. (The sibling
	// TestIncremental_AbsentGraphNonEmptyTree_ForcesFullParse takes 0.52s and
	// does not even perform a full reindex — it asserts TryIncremental returns
	// Done=false, i.e. the fallback SIGNAL.) The `!res.Done` check above is what
	// proves the incremental path was taken; this bound only catches it hanging.
	//
	// Making it a real perf assertion needs a fixture big enough for the two
	// paths to separate — a few thousand files, not ten. Until then do not
	// tighten it on the strength of the logged number.
	if dur > 3*time.Second {
		t.Errorf("incremental pass took %s on a 10-file repo — that is full-reindex cost, "+
			"the incremental path has degenerated", dur)
	}
	t.Logf("perf smoke: incremental pass took %s for 1 changed file in 10-file repo", dur.Truncate(time.Millisecond))
}
