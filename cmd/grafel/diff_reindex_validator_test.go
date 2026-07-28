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
// These cases lock in the invariant as #5309 makes the graph-wide phases
// incremental one layer at a time: resolution (layer 1) and module-aggregation +
// enrichment (layer 2) are now blast-radius-scoped on the incremental path yet
// still land on the same graph a full rebuild computes, so B and C agree under a
// now-EMPTY tolerance profile (see dvBaselineProfile). Remaining graph-wide
// phases (communities, link/flow passes) land behind these same assertions.
package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/cajasmota/grafel/internal/extractors"
	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/graph/fbwriter"
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

// dvBaselineProfile is the tolerance profile for the incremental-vs-full
// comparison. It is now EMPTY: every documented #5309 gap has been closed, so
// the validator asserts STRICT parity on every dimension (entity set, resolved
// call/reference edges, communities, module-aggregation edges, and all
// enrichment properties). As each layer of #5309 landed, the corresponding
// tolerance was removed and the validator tightened — this empty profile is the
// end state of that ratchet for the dimensions exercised here.
//
// History of the closed tolerances:
//   - RESOLUTION PARITY — closed (#5309 layer 1). The scoped resolver now
//     re-resolves the full blast radius (both endpoints of every affected edge)
//     via the same Format A ladder the full resolver uses, so resolved
//     call/reference endpoints match a full rebuild exactly.
//   - MODULE-AGGREGATION + ENRICHMENT — closed (#5309 layer 2). The incremental
//     path now re-runs module-aggregation scoped to the affected modules
//     (module.AggregateIncremental → the same CONTAINS / DEPENDS_ON edges +
//     `module` labels a full rebuild emits), plus the structural-coupling and
//     static test-reachability re-stamps (`ca`/`ce`/`instability`/
//     `coupling_computed`, `test_reachable`/`reaching_tests`/`reach_depth`), so
//     those edges and properties match too. The previously-tolerated
//     IgnoreRelKinds{CONTAINS,DEPENDS_ON} and IgnoreEntityProps{module,
//     test_reachable} are removed.
func dvBaselineProfile() parity.Options {
	return parity.Options{}
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

// ─────────────────────── flow-pass delta cases (#5309 layer 3) ──────────────
//
// The start-state corpus above (caller.go → target.go) is a 2-node chain, below
// the process-flow MinSteps threshold, so it emits no Process entities — the
// flow-pass parity is exercised trivially there. These cases use a deeper call
// chain that DOES emit a Process flow, so the incremental flow pass
// (engine.RunFlowsIncremental) is actually compared against a full rebuild's
// Pass 7 / 7.5 output.

// dvFlowCorpus writes a 4-deep call chain (Handler → StepB → StepC → StepD) that
// the process-flow walker materialises into one Process entity (+ its
// ENTRY_POINT_OF / STEP_IN_PROCESS edges) — plus a separate plain helper so a
// docs-only/unrelated change has something inert to touch.
func dvFlowCorpus(t *testing.T, repo string) {
	dvWriteFile(t, repo, "chain.go", "package svc\n\n"+
		"func Handler() int { return StepB() }\n"+
		"func StepB() int { return StepC() }\n"+
		"func StepC() int { return StepD() }\n"+
		"func StepD() int { return 1 }\n")
	dvWriteFile(t, repo, "README.md", "# svc\n\nInitial docs.\n")
}

// Case 5 — call-chain change ripples into the process flow. Lengthening the
// chain (StepD now calls a new StepE) changes the Process entity's chain hash +
// step edges. The incremental flow pass must strip the stale Process and re-emit
// exactly the Process a full rebuild's Pass 7 computes for the new chain.
func TestDiffReindex_FlowChainChange(t *testing.T) {
	repo := t.TempDir()
	stateDir := t.TempDir()
	dvFlowCorpus(t, repo)

	dvFullRebuild(t, repo, stateDir)
	dvSeedManifest(t, repo, stateDir)

	// Extend the chain: StepD now calls a new StepE, lengthening the flow.
	dvWriteFile(t, repo, "chain.go", "package svc\n\n"+
		"func Handler() int { return StepB() }\n"+
		"func StepB() int { return StepC() }\n"+
		"func StepC() int { return StepD() }\n"+
		"func StepD() int { return StepE() }\n"+
		"func StepE() int { return 1 }\n")

	b := dvIncremental(t, repo, stateDir)

	endDir := t.TempDir()
	c := dvFullRebuild(t, repo, endDir)

	dvAssertParity(t, b, c)
}

// Case 6 — docs-only change with a live process flow. Editing README.md touches
// no call structure, so the blast radius cannot affect the flow; the incremental
// path keeps the prior Process verbatim. It must still match a full rebuild,
// which recomputes the identical Process from the unchanged chain — proving the
// "keep flows verbatim when unaffected" branch is byte-correct.
func TestDiffReindex_FlowDocsOnlyChange(t *testing.T) {
	repo := t.TempDir()
	stateDir := t.TempDir()
	dvFlowCorpus(t, repo)

	dvFullRebuild(t, repo, stateDir)
	dvSeedManifest(t, repo, stateDir)

	// Docs-only edit — no call/HTTP/pub-sub structure changes.
	dvWriteFile(t, repo, "README.md", "# svc\n\nUpdated docs — no code change.\n")

	b := dvIncremental(t, repo, stateDir)

	endDir := t.TempDir()
	c := dvFullRebuild(t, repo, endDir)

	dvAssertParity(t, b, c)
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

// ─────────────── multi-module delta cases (#6033 blast-radius) ──────────────
//
// WHY THESE EXIST. Every case above runs on dvStartCorpus: a single Go package
// in one directory, i.e. exactly ONE module. Cross-module DEPENDS_ON scoping is
// therefore never exercised by them, so nothing pinned the module blast radius
// or the DEPENDS_ON weights it produces. `services/…` directories make
// detect.DetectMonorepo report a true monorepo, so entities get per-package
// module labels rather than one repo-wide label.
//
// What these cases actually catch (each verified by mutation):
//   - M1/M2 catch the #6033 duplication's effect on the module layer: duplicate
//     relationships are counted twice when aggregateModules sums edge weights,
//     so re-introducing the duplication yields DEPENDS_ON weight=2 where a full
//     rebuild computes 1. Under the pre-fix code these cases fail on weight.
//   - M3 catches the loss of the inbound-fix blast-radius signal
//     (scopedResult.MutatedExistingRelationships).
//
// A NOTE ON WHAT THEY DO NOT CATCH, so nobody assumes otherwise. Dropping
// `newRels` itself from `aggNewRels` is INERT: it changes no observable output
// in this suite. The reason is that module.AggregateIncremental's re-emit guard
// (aggregate.go:273) is `!include(from) && !include(to)` — SYMMETRIC — while
// that same function's comment directly above it claims "emit iff the
// from-module is in scope". The comment is stale. Because the guard is
// symmetric, and because every freshly-extracted edge has its from-entity in a
// changed file (hence an already-affected module), the to-side modules
// `newRels` contributes are redundant. It is kept as a conservative input, not
// because anything here binds it. The one case the symmetry does NOT cover is
// M3: a SURVIVOR edge rewritten to point at a SURVIVOR entity, where neither
// endpoint module is otherwise affected.

// dvMultiModuleCorpus writes a two-module monorepo with a MUTUAL cross-module
// dependency: alpha calls into beta and beta calls back into alpha. The mutual
// shape is what makes the strip/re-emit asymmetry observable — editing alpha
// affects the alpha→beta edge on its FROM side (re-emitted from alpha) and the
// beta→alpha edge on its TO side (re-emittable only if beta is ALSO affected,
// which happens only via the relationship endpoints of the changed file).
func dvMultiModuleCorpus(t *testing.T, repo string) {
	dvWriteFile(t, repo, "services/alpha/a.go", "package alpha\n\n"+
		"import \"x/services/beta\"\n\n"+
		"func AlphaFn() int { return beta.BetaFn() }\n\n"+
		"func AlphaHelper() int { return 3 }\n")
	dvWriteFile(t, repo, "services/beta/b.go", "package beta\n\n"+
		"import \"x/services/alpha\"\n\n"+
		"func BetaFn() int { return 1 }\n\n"+
		"func BetaBack() int { return alpha.AlphaHelper() }\n")
}

// dvCountDependsOnBetweenModules counts Module→Module DEPENDS_ON edges (both
// endpoints are synthetic Module nodes). Used as a fixture precondition so a
// multi-module case can never pass vacuously on a single-module corpus.
func dvCountDependsOnBetweenModules(doc *graph.Document) int {
	isModule := make(map[string]bool)
	for i := range doc.Entities {
		if doc.Entities[i].Kind == "Module" {
			isModule[doc.Entities[i].ID] = true
		}
	}
	n := 0
	for i := range doc.Relationships {
		r := &doc.Relationships[i]
		if r.Kind == "DEPENDS_ON" && isModule[r.FromID] && isModule[r.ToID] {
			n++
		}
	}
	return n
}

// Case M1 — edit one module of a mutually-dependent pair.
//
// This is the case that dies when `newRels` is dropped from the module blast
// radius: `affected` collapses to {services/alpha}, the strip removes BOTH
// DEPENDS_ON edges, and only alpha→beta is re-emitted — beta→alpha is silently
// lost relative to a full rebuild.
func TestDiffReindex_MultiModule_MutualDependencyEdit(t *testing.T) {
	repo := t.TempDir()
	stateDir := t.TempDir()
	dvMultiModuleCorpus(t, repo)

	dvFullRebuild(t, repo, stateDir)
	dvSeedManifest(t, repo, stateDir)

	// Precondition: the baseline really does hold BOTH cross-module edges, so a
	// green result below cannot come from the fixture being single-module.
	base, err := graph.LoadGraphFromDir(stateDir)
	if err != nil {
		t.Fatalf("load baseline: %v", err)
	}
	if n := dvCountDependsOnBetweenModules(base); n < 2 {
		t.Fatalf("fixture is not exercising cross-module DEPENDS_ON: found %d module→module edges, want ≥2", n)
	}

	// Edit ONLY services/alpha.
	dvWriteFile(t, repo, "services/alpha/a.go", "package alpha\n\n"+
		"import \"x/services/beta\"\n\n"+
		"func AlphaFn() int { return beta.BetaFn() + 1 }\n\n"+
		"func AlphaHelper() int { return 4 }\n")

	b := dvIncremental(t, repo, stateDir)

	endDir := t.TempDir()
	c := dvFullRebuild(t, repo, endDir)

	dvAssertModuleLayerParity(t, b, c)
}

// Case M2 — a NEW cross-module edge appears in the edited module. The changed
// file is in beta, so beta is affected via its entities; alpha enters the
// affected set only through the relationship endpoints of the freshly
// extracted edges.
func TestDiffReindex_MultiModule_NewCrossModuleEdge(t *testing.T) {
	repo := t.TempDir()
	stateDir := t.TempDir()
	dvMultiModuleCorpus(t, repo)

	dvFullRebuild(t, repo, stateDir)
	dvSeedManifest(t, repo, stateDir)

	// beta gains a second cross-module call, changing the beta→alpha weight.
	dvWriteFile(t, repo, "services/beta/b.go", "package beta\n\n"+
		"import \"x/services/alpha\"\n\n"+
		"func BetaFn() int { return 1 }\n\n"+
		"func BetaBack() int { return alpha.AlphaHelper() }\n\n"+
		"func BetaBack2() int { return alpha.AlphaHelper() + alpha.AlphaFn() }\n")

	b := dvIncremental(t, repo, stateDir)

	endDir := t.TempDir()
	c := dvFullRebuild(t, repo, endDir)

	dvAssertModuleLayerParity(t, b, c)
}

// dvAssertModuleLayerParity compares ONLY the module layer — the Module nodes
// and the Module→Module DEPENDS_ON edges (including their `weight` property) —
// between the incremental result b and the full rebuild c.
//
// WHY NOT dvAssertParity HERE. Full parity on a multi-module corpus currently
// fails for a reason that has nothing to do with the module blast radius: when
// a changed file carries a Go import, the incremental path re-emits the
// file→import-placeholder REFERENCES edge with an EMPTY FromID, where the full
// path emits it from the file component (the full path's
// import-placeholder-prune hoists those edges; the incremental path has no
// equivalent). Reproduced on the pre-#6033 baseline too, so it is pre-existing
// and independent — it is being tracked separately. Asserting full parity here
// would land a permanently-red test; asserting the module layer pins exactly
// the property these cases exist to protect.
//
// The narrower assertion is still sharp: it is what dies when the module
// blast-radius input is wrong.
func dvAssertModuleLayerParity(t *testing.T, b, c *graph.Document) {
	t.Helper()
	got := dvModuleLayer(b)
	want := dvModuleLayer(c)

	for k, wv := range want {
		gv, ok := got[k]
		if !ok {
			t.Errorf("module-layer edge missing from the incremental graph: %s (full rebuild has it, weight=%s)", k, wv)
			continue
		}
		if gv != wv {
			t.Errorf("module-layer edge %s: incremental weight=%s, full rebuild weight=%s", k, gv, wv)
		}
	}
	for k, gv := range got {
		if _, ok := want[k]; !ok {
			t.Errorf("module-layer edge present ONLY in the incremental graph: %s (weight=%s)", k, gv)
		}
	}
	if t.Failed() {
		t.Fatalf("module layer diverged from a full rebuild\nincremental: %v\nfull rebuild: %v", got, want)
	}
}

// dvModuleLayer projects a document's Module→Module DEPENDS_ON edges into a
// "fromModuleName→toModuleName" → weight map, resolving the synthetic Module
// node IDs to their names so the result is readable and stable across runs.
func dvModuleLayer(doc *graph.Document) map[string]string {
	name := make(map[string]string)
	for i := range doc.Entities {
		if doc.Entities[i].Kind == "Module" {
			name[doc.Entities[i].ID] = doc.Entities[i].Name
		}
	}
	out := make(map[string]string)
	for i := range doc.Relationships {
		r := &doc.Relationships[i]
		if r.Kind != "DEPENDS_ON" {
			continue
		}
		from, fok := name[r.FromID]
		to, tok := name[r.ToID]
		if !fok || !tok {
			continue
		}
		w := ""
		if v, ok := r.PropLookup("weight"); ok {
			w = v
		}
		out[from+"→"+to] = w
	}
	return out
}

// Case M3 — an inbound stub bound by the scoped resolver creates a
// cross-module dependency between two modules that are BOTH unaffected.
//
// This is the one shape where the module blast radius genuinely cannot be
// derived from the changed file alone. module.AggregateIncremental strips a
// Module→Module DEPENDS_ON when EITHER endpoint module is affected and
// re-emits it when EITHER endpoint is in scope (aggregate.go:273 — note the
// function's own comment there says "iff the from-module is in scope", which is
// stale: the guard is symmetric). That symmetry means a changed file's own
// module covers almost everything. The exception is here: when ResolveScoped
// binds a SURVIVOR edge's stub ToID to an entity that is ALSO a survivor, the
// resulting module pair can involve neither the changed file's module nor any
// re-extracted entity — so nothing in newEntities/newRels names it.
//
// The baseline is hand-corrupted into exactly that state: a resolved
// cross-module CALLS edge is pushed back to an unresolved bare-name stub and
// its derived Module→Module DEPENDS_ON removed, reproducing a graph where the
// full resolver had left the ref unbound. An edit to an UNRELATED third module
// then triggers the scoped resolver, which binds it — and the module layer must
// converge on what a full rebuild computes.
//
// Fails when scopedResult.MutatedExistingRelationships is not folded into
// aggNewRels: the m2→m3 edge is never derived.
func TestDiffReindex_MultiModule_InboundStubBindCreatesCrossModuleEdge(t *testing.T) {
	repo := t.TempDir()
	stateDir := t.TempDir()

	dvWriteFile(t, repo, "services/m3/three.go", "package m3\n\n"+
		"func Foo() int { return 7 }\n")
	dvWriteFile(t, repo, "services/m2/two.go", "package m2\n\n"+
		"import \"x/services/m3\"\n\n"+
		"func Bar() int { return m3.Foo() }\n")
	dvWriteFile(t, repo, "services/m1/one.go", "package m1\n\n"+
		"func One() int { return 1 }\n")

	dvFullRebuild(t, repo, stateDir)

	// ── Corrupt the baseline into the "unbound cross-module ref" state ───────
	base, err := graph.LoadGraphFromDir(stateDir)
	if err != nil {
		t.Fatalf("load baseline: %v", err)
	}
	fooID, barID := "", ""
	modName := map[string]string{}
	for i := range base.Entities {
		e := &base.Entities[i]
		if e.Kind == "Module" {
			modName[e.ID] = e.Name
			continue
		}
		switch e.Name {
		case "Foo":
			fooID = e.ID
		case "Bar":
			barID = e.ID
		}
	}
	if fooID == "" || barID == "" {
		t.Fatalf("fixture: could not locate Foo/Bar entities (foo=%q bar=%q)", fooID, barID)
	}

	pushedBack := false
	var kept []graph.Relationship
	for _, r := range base.Relationships {
		// Drop the derived Module(m2)→Module(m3) DEPENDS_ON so ONLY the
		// re-emit path can bring it back.
		if r.Kind == "DEPENDS_ON" &&
			modName[r.FromID] == "services/m2" && modName[r.ToID] == "services/m3" {
			continue
		}
		// Push the resolved Bar→Foo CALLS edge back to a bare-name stub.
		if r.FromID == barID && r.ToID == fooID {
			r.ToID = "Foo"
			r.ID = graph.RelationshipID(r.FromID, r.ToID, r.Kind)
			pushedBack = true
		}
		kept = append(kept, r)
	}
	if !pushedBack {
		t.Fatal("fixture: no resolved Bar→Foo edge found to push back to a stub")
	}
	base.Relationships = kept
	base.Stats.Relationships = len(kept)
	if _, err := fbwriter.WriteGraphGen(stateDir, base); err != nil {
		t.Fatalf("rewrite corrupted baseline: %v", err)
	}

	// Precondition: the corrupted baseline really lacks the m2→m3 edge.
	if _, ok := dvModuleLayer(base)["services/m2→services/m3"]; ok {
		t.Fatal("fixture: the corrupted baseline still holds the m2→m3 DEPENDS_ON edge")
	}
	dvSeedManifest(t, repo, stateDir)

	// Edit an UNRELATED third module. Neither m2 nor m3 is touched.
	dvWriteFile(t, repo, "services/m1/one.go", "package m1\n\n"+
		"func One() int { return 2 }\n")

	b := dvIncremental(t, repo, stateDir)

	endDir := t.TempDir()
	c := dvFullRebuild(t, repo, endDir)

	// Sanity: the full rebuild does derive the edge, so the assertion is real.
	if _, ok := dvModuleLayer(c)["services/m2→services/m3"]; !ok {
		t.Fatalf("fixture: the full rebuild does not derive m2→m3; layer=%v", dvModuleLayer(c))
	}

	dvAssertModuleLayerParity(t, b, c)
}
