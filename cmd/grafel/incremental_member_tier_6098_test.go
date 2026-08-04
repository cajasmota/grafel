// Package main — incremental_member_tier_6098_test.go
//
// #6098 / #6090-residual — the scoped resolver's receiver-type tier, measured
// end-to-end on a persisted graph.
//
// #6121 closed the #6090 loss class by REPLAYING the prior graph's binding for
// an edge the single-file re-extraction could not re-bind. That mechanism is
// structurally unable to help a call the edit ITSELF introduces: there is no
// prior edge to consult, so the stub is permanent until a full reindex.
// Measured in that PR as `[Helper07, STUB:Do11, STUB:Do23]` incrementally
// versus `[Helper07, T11.Do11, T23.Do23]` on a full rebuild.
//
// Only a resolver TIER can bind it, which is what
// internal/extractors/sresolver/pkgmember.go adds: the corpus-wide resolver's
// package-scoped member index (internal/resolve/refs.go:5684, refs #148/#364),
// restricted on the scoped path to the package directories actually probed.
//
// The fixture below is the exact complement of
// incremental_prior_resolution_6090_test.go: there the cross-file calls exist
// in the BASELINE (so replay can bind them), here they do NOT (so replay
// cannot).
package main

import (
	"fmt"
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
)

// mt6098Baseline is the pass-0 content of the file that will be edited. It has
// NO cross-file calls — that is the whole point. Compare pr6090Edited(0),
// which deliberately does.
func mt6098Baseline() string {
	return `package svc

type T07 struct{ N int }

func (t *T07) Do07(x int) int { return x + t.N + 7 }

func Helper07(x int) int { return x + 7 }

func Local07(x int) int { return Helper07(x) }
`
}

// mt6098Introduces adds two cross-file method calls that did not exist in any
// prior graph. `a.Do11` / `b.Do23` are bare-name stubs at the call site while
// the callee entities are named `T11.Do11` / `T23.Do23` in sibling files of
// the same package directory — the shape only the receiver-type tier binds.
func mt6098Introduces(pass int) string {
	return fmt.Sprintf(`package svc

type T07 struct{ N int }

func (t *T07) Do07(x int) int { return x + t.N + 7 }

func Helper07(x int) int { return x + 7 }

func Local07(x int) int {
	a := &T11{N: 1}
	b := &T23{N: 2}
	return Helper07(x) + a.Do11(x) + b.Do23(x) + %d
}
`, pass)
}

// TestIncremental_NewlyIntroducedCrossFileCall_6098 is the #6090 residual gate.
//
// Every basename in the corpus is unique: diff.Filter cross-invalidates
// same-basename files, which would turn the one-file delta into an N-file
// change and trip the too-many-changed full-reindex fallback, making the case
// vacuous. dvIncremental fails the test if TryIncremental falls back, so a
// silent full reindex cannot fake a pass either.
func TestIncremental_NewlyIntroducedCrossFileCall_6098(t *testing.T) {
	repo := t.TempDir()
	stateDir := t.TempDir()
	pr6090WriteCorpus(t, repo)
	// Overwrite r07.go with the call-free baseline, replacing the variant
	// pr6090WriteCorpus installs. Replay must have NOTHING to replay.
	dvWriteFile(t, repo, "r07.go", mt6098Baseline())
	dvFullRebuild(t, repo, stateDir)

	base, err := graph.LoadGraphFromDir(stateDir)
	if err != nil {
		t.Fatalf("load baseline: %v", err)
	}
	// Precondition, not a result: if the baseline already held these edges the
	// test would be measuring #6121's replay, not the tier.
	if n := pr6090CountResolvedCrossFileCalls(base, "r07.go"); n != 0 {
		t.Fatalf("fixture is not testing the residual: the baseline already holds %d resolved "+
			"cross-file CALLS edge(s) out of r07.go, so prior-resolution replay could bind them "+
			"and the receiver-type tier is not what is under test", n)
	}

	fullRepo := t.TempDir()
	pr6090WriteCorpus(t, fullRepo)
	dvWriteFile(t, fullRepo, "r07.go", mt6098Baseline())

	const wantCrossFile = 2
	for pass := 1; pass <= 3; pass++ {
		dvSeedManifest(t, repo, stateDir)
		dvWriteFile(t, repo, "r07.go", mt6098Introduces(pass))
		dvIncremental(t, repo, stateDir)

		b, err := graph.LoadGraphFromDir(stateDir)
		if err != nil {
			t.Fatalf("pass %d: load persisted incremental graph: %v", pass, err)
		}

		dvWriteFile(t, fullRepo, "r07.go", mt6098Introduces(pass))
		c := dvFullRebuild(t, fullRepo, t.TempDir())

		// Positive baseline — without this the assertions below are satisfied
		// by a graph that never held the edges at all.
		if n := pr6090CountResolvedCrossFileCalls(c, "r07.go"); n != wantCrossFile {
			t.Fatalf("pass %d: fixture is inert — the full rebuild holds %d resolved cross-file "+
				"CALLS edge(s) out of r07.go, want %d", pass, n, wantCrossFile)
		}
		if n := pr6090CountResolvedCrossFileCalls(b, "r07.go"); n != wantCrossFile {
			t.Errorf("pass %d: the incremental graph holds %d resolved cross-file CALLS edge(s) out "+
				"of r07.go, want %d — a call the edit ITSELF introduced has no prior binding to "+
				"replay, so it stays a bare-name stub unless the scoped resolver's receiver-type "+
				"tier binds it (#6098 / #6090 residual)", pass, n, wantCrossFile)
		}

		// Bidirectional multiset parity (#6037): asserting only on `lost`
		// accepts a graph that replaced a correct edge with a wrong one, and
		// each stub here is paired with an INVENTED `…→Do11:CALLS` row, so a
		// one-way comparison reports nothing missing.
		pr6090AssertCallsParity(t, fmt.Sprintf("pass %d", pass), b, c)
	}
}
