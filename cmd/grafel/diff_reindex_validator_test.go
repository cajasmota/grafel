// Package main — diff_reindex_validator_test.go
//
// Differential-reindex validator harness (epic #5376, layer-2 foundation #5309).
//
// This is the safety net that gates the upcoming incremental graph-wide phases
// (scoped resolution / communities / link-flow passes). It asserts the
// load-bearing invariant those phases must never break:
//
//	an incremental reindex must produce a graph STRUCTURALLY EQUIVALENT to a
//	full rebuild of the same end-state.
//
// Harness shape, per delta case
// ─────────────────────────────
//
//	(A) full-rebuild the start-state corpus            → graph A (on disk)
//	    + seed the incremental manifest
//	(B) apply a source change, incremental-reindex      → graph B (TryIncremental)
//	(C) full-rebuild the changed corpus from scratch    → graph C
//	    assert  B ≡ C  via internal/graph/parity.Compare
//
// Why B ≡ C and not B ≡ A: a change is supposed to alter the graph. The
// invariant is that the *incremental* path lands on the SAME graph a clean full
// rebuild of the end-state would — i.e. the incremental skip logic never drops
// or staleifies anything a full rebuild would have computed.
//
// These cases pass TODAY: the graph-wide phases are still full in both the
// incremental and the full path, so B and C already agree. That is precisely
// the point — this PR locks in the baseline so #5309's per-phase incrementalism
// cannot regress equivalence silently. Each future layer (resolution →
// communities → flows) lands behind these same assertions.
package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/cajasmota/grafel/internal/extractors"
	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/graph/parity"
	"github.com/cajasmota/grafel/internal/indexer/diff"
)

// ─────────────────────────── harness primitives ───────────────────────────

// dvWriteFile creates or overwrites repo/relPath with content.
func dvWriteFile(t *testing.T, repo, relPath, content string) {
	t.Helper()
	abs := filepath.Join(repo, filepath.FromSlash(relPath))
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", relPath, err)
	}
}

// dvFullRebuild runs the full indexer over repo, writing graph.fb (+ manifest)
// into a fresh state dir, and returns the loaded Document. It pins
// GRAFEL_DAEMON_ROOT so the run never touches the real store, and skips the
// graph-algo (Pass 4) pass so the baseline is deterministic and fast — the
// validator's structural assertions (entities/edges/communities) do not depend
// on betweenness/pagerank floats, which are legitimately non-deterministic
// under sampling.
func dvFullRebuild(t *testing.T, repo, stateDir string) *graph.Document {
	t.Helper()
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatalf("mkdir stateDir: %v", err)
	}
	t.Setenv("GRAFEL_DAEMON_ROOT", stateDir)
	fbPath := filepath.Join(stateDir, "graph.fb")
	// WithIncremental(stateDir) makes Index write the diff manifest alongside
	// graph.fb, which is exactly the baseline TryIncremental later loads.
	err := Index(repo, fbPath, "test-repo", []string{"graph-algo"}, false, false,
		WithIncremental(stateDir))
	if err != nil {
		t.Fatalf("full Index: %v", err)
	}
	doc, err := graph.LoadGraphFromDir(stateDir)
	if err != nil {
		t.Fatalf("load graph after full rebuild: %v", err)
	}
	return doc
}

// dvSeedManifest writes a fresh diff manifest covering every file currently in
// repo, so a subsequent TryIncremental detects only the post-seed change as
// "changed". (dvFullRebuild already seeds via WithIncremental, but call sites
// that mutate the corpus between the rebuild and the incremental pass re-seed
// to pin the exact baseline.)
func dvSeedManifest(t *testing.T, repo, stateDir string) {
	t.Helper()
	var paths []string
	_ = filepath.WalkDir(repo, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(repo, path)
		paths = append(paths, filepath.ToSlash(rel))
		return nil
	})
	m := diff.LoadManifest(stateDir)
	diff.UpdateManifest(repo, paths, m)
	if err := diff.SaveManifest(stateDir, repo, m); err != nil {
		t.Fatalf("seed manifest: %v", err)
	}
}

// dvIncremental runs TryIncremental against repo using the on-disk baseline in
// stateDir, then returns the resulting (re-written) Document. A fallback to
// full reindex is a test failure: the harness exists to validate the
// incremental *path*, so a silent fallback would hide a regression in it.
func dvIncremental(t *testing.T, repo, stateDir string) *graph.Document {
	t.Helper()
	t.Setenv("GRAFEL_DAEMON_ROOT", stateDir)
	res := extractors.TryIncremental(context.Background(), repo, stateDir, nil, nil)
	if !res.Done {
		t.Fatalf("TryIncremental fell back to full reindex (reason=%q) — the harness "+
			"requires the incremental path to run so it can be validated", res.FallbackReason)
	}
	doc, err := graph.LoadGraphFromDir(stateDir)
	if err != nil {
		t.Fatalf("load graph after incremental: %v", err)
	}
	return doc
}

// dvBaselineProfile is the tolerance profile that captures the KNOWN
// incremental-vs-full gaps that exist TODAY, before #5309 makes the graph-wide
// phases incremental. Each tolerance is a documented gap a later layer of #5309
// will close; until then the validator normalizes it rather than false-alarming,
// while staying strict on everything else (entity set, resolved call/reference
// edges, communities). As each layer lands, the corresponding tolerance is
// removed and the validator tightens — that is how this harness ratchets the
// incremental path toward full-rebuild parity.
//
// The gaps, surfaced empirically by running the harness with Strict():
//   - module-aggregation edges (CONTAINS / DEPENDS_ON): the incremental path
//     does not re-run the module-agg pass (#5309 link/flow layer).
//   - enrichment-only entity properties (`module`, `test_reachable`): produced
//     by passes the incremental path defers (module-agg + test-reachability).
//
// RESOLUTION PARITY — closed (#5309 layer 1). The scoped resolver previously
// left a freshly-extracted edge's endpoint in stub form rather than the hashed
// entity id the full resolver assigns; the validator normalized it via
// NormalizeStubEndpoints. The scoped resolver now re-resolves the full blast
// radius (both endpoints of every affected edge) via the same Format A ladder
// the full resolver uses, so that tolerance is removed — the harness asserts
// STRICT resolution parity on resolved call/reference endpoints.
func dvBaselineProfile() parity.Options {
	return parity.Options{
		IgnoreRelKinds:    map[string]bool{"CONTAINS": true, "DEPENDS_ON": true},
		IgnoreEntityProps: map[string]bool{"module": true, "test_reachable": true},
	}
}

// dvAssertParity runs the comparator under the baseline tolerance profile and
// fails with its precise diff on mismatch. b is the incremental result, c the
// full rebuild of the end-state.
func dvAssertParity(t *testing.T, b, c *graph.Document) {
	t.Helper()
	// A = full-rebuild reference, B = incremental candidate.
	rep := parity.CompareWithOptions(c, b, dvBaselineProfile())
	if !rep.Equivalent {
		t.Fatalf("incremental ≠ full-rebuild of end-state (under documented baseline tolerances):\n%s", rep.String())
	}
}

// ───────────────────────────── delta cases ─────────────────────────────────
//
// Start-state corpus shared by the cases below: a tiny multi-file Go package
// where caller.go calls into target.go, giving us a cross-file CALLS edge to
// exercise inbound-edge handling.

func dvStartCorpus(t *testing.T, repo string) {
	dvWriteFile(t, repo, "target.go", "package svc\n\n"+
		"func Target() int { return 1 }\n\n"+
		"func Sibling() int { return 2 }\n")
	dvWriteFile(t, repo, "caller.go", "package svc\n\n"+
		"func Caller() int { return Target() + Sibling() }\n")
}

// Case 1 — rename across files. Target() → Renamed(); caller.go is updated to
// match. Both files change; the old entity must vanish and the new one appear
// with the caller edge re-pointed — exactly as a full rebuild would compute.
func TestDiffReindex_RenameAcrossFiles(t *testing.T) {
	repo := t.TempDir()
	stateDir := t.TempDir()
	dvStartCorpus(t, repo)

	dvFullRebuild(t, repo, stateDir) // graph A + manifest baseline
	dvSeedManifest(t, repo, stateDir)

	// Apply the rename across both files.
	dvWriteFile(t, repo, "target.go", "package svc\n\n"+
		"func Renamed() int { return 1 }\n\n"+
		"func Sibling() int { return 2 }\n")
	dvWriteFile(t, repo, "caller.go", "package svc\n\n"+
		"func Caller() int { return Renamed() + Sibling() }\n")

	b := dvIncremental(t, repo, stateDir)

	endDir := t.TempDir()
	c := dvFullRebuild(t, repo, endDir)

	dvAssertParity(t, b, c)
}

// Case 2 — deleted entity with inbound edges. Remove Sibling() from target.go
// and drop its call in caller.go. The deleted entity AND any stale inbound edge
// to it must be gone — a full rebuild has neither, so the incremental path
// must not leave a dangling edge behind.
func TestDiffReindex_DeletedEntityWithInboundEdges(t *testing.T) {
	repo := t.TempDir()
	stateDir := t.TempDir()
	dvStartCorpus(t, repo)

	dvFullRebuild(t, repo, stateDir)
	dvSeedManifest(t, repo, stateDir)

	// Delete Sibling() and the call to it.
	dvWriteFile(t, repo, "target.go", "package svc\n\n"+
		"func Target() int { return 1 }\n")
	dvWriteFile(t, repo, "caller.go", "package svc\n\n"+
		"func Caller() int { return Target() }\n")

	b := dvIncremental(t, repo, stateDir)

	endDir := t.TempDir()
	c := dvFullRebuild(t, repo, endDir)

	dvAssertParity(t, b, c)
}

// Case 3 — signature change rippling to callers. Target() grows a parameter and
// caller.go passes it. The signature-change detection + scoped re-resolution
// must land the same entity signature and the same caller edge a full rebuild
// would.
func TestDiffReindex_SignatureChangeRipplesToCallers(t *testing.T) {
	repo := t.TempDir()
	stateDir := t.TempDir()
	dvStartCorpus(t, repo)

	dvFullRebuild(t, repo, stateDir)
	dvSeedManifest(t, repo, stateDir)

	// Target gains a parameter; caller updated to match.
	dvWriteFile(t, repo, "target.go", "package svc\n\n"+
		"func Target(n int) int { return n }\n\n"+
		"func Sibling() int { return 2 }\n")
	dvWriteFile(t, repo, "caller.go", "package svc\n\n"+
		"func Caller() int { return Target(3) + Sibling() }\n")

	b := dvIncremental(t, repo, stateDir)

	endDir := t.TempDir()
	c := dvFullRebuild(t, repo, endDir)

	dvAssertParity(t, b, c)
}

// TestDiffReindex_CatchesInjectedMismatch is the validator's self-check: it
// proves the harness FAILS when the incremental graph is deliberately wrong. We
// take a real incremental result, corrupt it the way a buggy incremental skip
// would (drop an entity and an edge, drift a community label), and assert the
// comparator reports it under the SAME baseline tolerance profile the green
// cases use — i.e. the tolerances don't mask real corruption.
func TestDiffReindex_CatchesInjectedMismatch(t *testing.T) {
	repo := t.TempDir()
	stateDir := t.TempDir()
	dvStartCorpus(t, repo)

	dvFullRebuild(t, repo, stateDir)
	dvSeedManifest(t, repo, stateDir)

	dvWriteFile(t, repo, "caller2.go", "package svc\n\nfunc Caller2() int { return Target() * 2 }\n")
	b := dvIncremental(t, repo, stateDir)

	endDir := t.TempDir()
	c := dvFullRebuild(t, repo, endDir)

	// Sanity: the honest comparison passes.
	if rep := parity.CompareWithOptions(c, b, dvBaselineProfile()); !rep.Equivalent {
		t.Fatalf("precondition: honest incremental should match full rebuild:\n%s", rep.String())
	}

	// Inject corruption into the incremental graph the way a buggy incremental
	// skip would: drop a real (non-tolerated) entity, re-point a CALLS edge at a
	// bogus target (an edge the full rebuild does not have), and drift a
	// community label on a survivor. All three must be reported even under the
	// baseline tolerance profile — the tolerances must not mask real corruption.
	if len(b.Entities) < 2 {
		t.Fatalf("fixture too small to corrupt: %d entities", len(b.Entities))
	}
	corruptEnts := append([]graph.Entity(nil), b.Entities...)
	corruptRels := append([]graph.Relationship(nil), b.Relationships...)

	// (a) drop a CALLS-bearing entity (find one to make the entity-set diverge).
	dropIdx := -1
	for i, e := range corruptEnts {
		if e.Kind != "Module" { // keep it a real source entity, not the synthetic module node
			dropIdx = i
			break
		}
	}
	if dropIdx < 0 {
		t.Fatal("no non-module entity to drop")
	}
	corruptEnts = append(corruptEnts[:dropIdx], corruptEnts[dropIdx+1:]...)

	// (b) re-point a CALLS edge at a fabricated target id absent from the full rebuild.
	rewired := false
	for i := range corruptRels {
		if corruptRels[i].Kind == "CALLS" {
			corruptRels[i].ToID = "deadbeefdeadbeef" // 16-hex, not a real entity → only-in-B edge
			rewired = true
			break
		}
	}
	if !rewired {
		t.Fatal("no CALLS edge to re-point")
	}

	// (c) drift a community label on a survivor.
	drift := 999999
	corruptEnts[0].CommunityID = &drift

	corrupt := &graph.Document{Entities: corruptEnts, Relationships: corruptRels}

	rep := parity.CompareWithOptions(c, corrupt, dvBaselineProfile())
	if rep.Equivalent {
		t.Fatal("validator FAILED to catch an injected mismatch — the safety net is not actually checking structure")
	}
	if len(rep.EntitiesOnlyInA) == 0 {
		t.Errorf("expected the dropped entity to be reported as only-in-A; report:\n%s", rep.String())
	}
	if len(rep.RelsOnlyInB) == 0 {
		t.Errorf("expected the re-pointed bogus edge to be reported as only-in-B; report:\n%s", rep.String())
	}
	if len(rep.CommunityAssignmentDiffs) == 0 {
		t.Errorf("expected the community drift to be reported; report:\n%s", rep.String())
	}
	t.Logf("validator correctly caught injected mismatch:\n%s", rep.String())
}

// Case 4 — new cross-file call edge. Add a brand-new caller file that calls into
// the existing target. The new entity AND the new cross-file CALLS edge must
// match a full rebuild — this exercises the scoped resolver's inbound-edge
// creation on the incremental path.
func TestDiffReindex_NewCrossFileCall(t *testing.T) {
	repo := t.TempDir()
	stateDir := t.TempDir()
	dvStartCorpus(t, repo)

	dvFullRebuild(t, repo, stateDir)
	dvSeedManifest(t, repo, stateDir)

	// Add a second caller file that also calls Target().
	dvWriteFile(t, repo, "caller2.go", "package svc\n\n"+
		"func Caller2() int { return Target() * 2 }\n")

	b := dvIncremental(t, repo, stateDir)

	endDir := t.TempDir()
	c := dvFullRebuild(t, repo, endDir)

	dvAssertParity(t, b, c)
}
