package groupalgo

// union_presize_5909_test.go — #5909 (Refs #5954): the group union assembly
// must be PRESIZED, and the one-shot `group-algo --write` child must not retain
// a second full *AlgorithmResults in the process-local memo.
//
// Hazard 1 (assembly). AssembleGroupGraph appended entities one at a time and
// relationships repo-by-repo into UNPRESIZED slices. append() grows by
// doubling, so at the copy instant the process transiently holds both the old
// backing array and a new one ~2x its size — for a 427k-entity union that is
// the single largest allocation in the process. Presizing from the persisted
// per-repo header counts removes the growth copies entirely.
//
// Hazard 2 (memo). storeMemoizedGroupResult was called UNCONDITIONALLY. The
// memo exists so the long-lived daemon does not re-run Louvain + PageRank +
// betweenness for a group-version it already computed. The one-shot
// `group-algo --write` child computes exactly once and then exits — for it the
// memo is pure retention: a second reference to the PageRank / community /
// centrality maps over the whole union, held live across the entire
// WriteOverlayFromResult window (the phase where the child is already at its
// heap peak).
//
// Both changes must be strictly behaviour-preserving: same union ORDER (the
// order feeds graph.CommunityInputHash, and a changed hash silently invalidates
// every persisted overlay), same input_hash, same overlay.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/daemon"
	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/registry"
	"github.com/cajasmota/grafel/internal/testsupport"
)

// TestAssembleGroupGraph_PresizesUnionSlices is the core RED assertion for
// hazard 1: with every repo reporting persisted header stats, the union slices
// must be allocated at exactly the union size — no growth doubling, and no
// over-allocation either (over-allocating the union is the very thing we are
// trying to avoid). cap is asserted, not timing.
func TestAssembleGroupGraph_PresizesUnionSlices(t *testing.T) {
	group, _, _, _ := setupIncrGroup(t)

	ents, rels, _, _, err := AssembleGroupGraph(group)
	if err != nil {
		t.Fatalf("AssembleGroupGraph: %v", err)
	}
	if len(ents) == 0 || len(rels) == 0 {
		t.Fatalf("fixture degenerate: %d entities, %d rels", len(ents), len(rels))
	}
	if cap(ents) != len(ents) {
		t.Errorf("entities cap=%d len=%d — union slice is not presized from the persisted per-repo stats (append doubling transiently allocates ~1.5-2x the union)", cap(ents), len(ents))
	}
	if cap(rels) != len(rels) {
		t.Errorf("relationships cap=%d len=%d — union slice is not presized", cap(rels), len(rels))
	}
}

// writeFixtureRepoJSON writes a repo whose ONLY graph artifact is graph.json —
// no graph.fb — so graph.PersistedStatsFromDir reports ok=false for it while
// graph.LoadGraphFromDir still materializes it. This is the "stats unavailable"
// case the presize must handle explicitly.
func writeFixtureRepoJSON(t *testing.T, slug, repoPath string, doc *graph.Document) registry.Repo {
	t.Helper()
	stateDir := daemon.StateDirForRepo(repoPath)
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatalf("mkdir state dir for %s: %v", slug, err)
	}
	b, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal fixture doc for %s: %v", slug, err)
	}
	if err := os.WriteFile(filepath.Join(stateDir, "graph.json"), b, 0o644); err != nil {
		t.Fatalf("write fixture graph.json for %s: %v", slug, err)
	}
	return registry.Repo{Slug: slug, Path: repoPath}
}

// setupMixedStatsGroup registers a 2-repo group where repo "svc" has a real
// graph.fb (stats available) and repo "web" has graph.json only (stats
// UNAVAILABLE). Returns the group name and the two source documents in
// registry order.
func setupMixedStatsGroup(t *testing.T) (group string, docs []*graph.Document) {
	t.Helper()
	resetGroupAlgoMemo()
	testsupport.IsolateHome(t)
	root := t.TempDir()
	t.Setenv("GRAFEL_HOME", filepath.Join(root, "home"))
	t.Setenv("GRAFEL_DAEMON_ROOT", filepath.Join(root, "daemon"))

	repoA, repoB, _ := fixtureGraphs()
	rA := writeFixtureRepo(t, "svc", filepath.Join(root, "repoA"), repoA)
	rB := writeFixtureRepoJSON(t, "web", filepath.Join(root, "repoB"), repoB)

	cfgPath, err := registry.ConfigPathFor("mixed")
	if err != nil {
		t.Fatalf("config path: %v", err)
	}
	if err := registry.SaveGroupConfig(cfgPath, &registry.GroupConfig{Name: "mixed", Repos: []registry.Repo{rA, rB}}); err != nil {
		t.Fatalf("save group config: %v", err)
	}
	if err := registry.AddGroup("mixed", cfgPath); err != nil {
		t.Fatalf("add group: %v", err)
	}
	return "mixed", []*graph.Document{repoA, repoB}
}

// TestAssembleGroupGraph_MissingStats_NoWrongCapacity: a repo whose persisted
// stats are unavailable (graph.json only) must contribute NOTHING to the
// presize — never a guessed capacity — and must not panic or lose content. The
// resulting capacity must never EXCEED the union size: an over-sized union
// allocation is worse than no presize at all.
func TestAssembleGroupGraph_MissingStats_NoWrongCapacity(t *testing.T) {
	group, docs := setupMixedStatsGroup(t)

	ents, rels, entityRepo, _, err := AssembleGroupGraph(group)
	if err != nil {
		t.Fatalf("AssembleGroupGraph: %v", err)
	}
	wantEnts := len(docs[0].Entities) + len(docs[1].Entities)
	wantRels := len(docs[0].Relationships) + len(docs[1].Relationships)
	if len(ents) != wantEnts {
		t.Fatalf("union entities=%d want=%d — a repo with unavailable stats was dropped", len(ents), wantEnts)
	}
	if len(rels) != wantRels {
		t.Fatalf("union rels=%d want=%d", len(rels), wantRels)
	}
	// An unavailable-stats repo must not fabricate capacity. The union may still
	// grow by ordinary append doubling for that repo's share (that is correct and
	// unavoidable), but capacity must never run away beyond that bound.
	if cap(ents) > 2*len(ents) {
		t.Errorf("entities cap=%d len=%d — an unavailable-stats repo produced a fabricated/over-sized union allocation", cap(ents), len(ents))
	}
	if cap(rels) > 2*len(rels) {
		t.Errorf("rels cap=%d len=%d — over-sized union allocation", cap(rels), len(rels))
	}
	if entityRepo["web:b1"] != "web" {
		t.Errorf("web:b1 attributed to %q want web", entityRepo["web:b1"])
	}
}

// TestUnionCapacityHint_MissingStatsContributesZero pins the presize hint
// itself: a repo whose persisted header stats are unavailable contributes
// EXACTLY ZERO to the hint — never a guess, never another repo's count. A wrong
// capacity is worse than no capacity, because over-allocating the union is the
// hazard being fixed.
func TestUnionCapacityHint_MissingStatsContributesZero(t *testing.T) {
	// All-stats-available group: the hint is exactly the union size.
	full, _, _, _ := setupIncrGroup(t)
	cfgFull, err := resolveGroup(full)
	if err != nil {
		t.Fatalf("resolve group: %v", err)
	}
	ents, rels, _, _, err := AssembleGroupGraph(full)
	if err != nil {
		t.Fatalf("AssembleGroupGraph: %v", err)
	}
	gotE, gotR := unionCapacityHint(cfgFull)
	if gotE != len(ents) || gotR != len(rels) {
		t.Errorf("hint for a fully-indexed group = (%d,%d), want the exact union size (%d,%d)", gotE, gotR, len(ents), len(rels))
	}

	// Mixed group: only the graph.fb repo contributes.
	mixed, docs := setupMixedStatsGroup(t)
	cfgMixed, err := resolveGroup(mixed)
	if err != nil {
		t.Fatalf("resolve mixed group: %v", err)
	}
	gotE, gotR = unionCapacityHint(cfgMixed)
	if gotE != len(docs[0].Entities) || gotR != len(docs[0].Relationships) {
		t.Errorf("hint for a mixed group = (%d,%d), want only the stats-bearing repo's counts (%d,%d) — the graph.json-only repo must contribute zero",
			gotE, gotR, len(docs[0].Entities), len(docs[0].Relationships))
	}
}

// TestAssembleGroupGraph_UnionMatchesPlainConcatenation is the behaviour lock:
// the presized assembly must produce the union in EXACTLY the same order as the
// plain per-element/per-repo appends it replaces, element for element, and
// therefore the same graph.CommunityInputHash. The expected value is computed
// here from the source documents by the very appends the implementation no
// longer performs, so this is a direct pre-change vs post-change comparison
// (the method #5992 used to prove its reorder hash-neutral).
func TestAssembleGroupGraph_UnionMatchesPlainConcatenation(t *testing.T) {
	group, pathA, pathB, _ := setupIncrGroup(t)

	// Pre-change assembly, verbatim, over the SAME loaded documents the
	// implementation reads: per-element entity append, whole-slice rel append,
	// in registry repo order (svc then web), into unpresized slices.
	var wantEnts []graph.Entity
	var wantRels []graph.Relationship
	wantRepo := map[string]string{}
	for _, rd := range []struct {
		slug string
		path string
	}{{"svc", pathA}, {"web", pathB}} {
		doc, err := graph.LoadGraphFromDir(daemon.StateDirForRepo(rd.path))
		if err != nil {
			t.Fatalf("load %s: %v", rd.slug, err)
		}
		for i := range doc.Entities {
			wantEnts = append(wantEnts, doc.Entities[i])
			wantRepo[doc.Entities[i].ID] = rd.slug
		}
		wantRels = append(wantRels, doc.Relationships...)
	}

	gotEnts, gotRels, gotRepo, _, err := AssembleGroupGraph(group)
	if err != nil {
		t.Fatalf("AssembleGroupGraph: %v", err)
	}
	if len(gotEnts) != len(wantEnts) {
		t.Fatalf("entity count %d want %d", len(gotEnts), len(wantEnts))
	}
	for i := range wantEnts {
		if gotEnts[i].ID != wantEnts[i].ID {
			t.Fatalf("union entity order changed at %d: got %q want %q — the union order feeds CommunityInputHash", i, gotEnts[i].ID, wantEnts[i].ID)
		}
	}
	if len(gotRels) != len(wantRels) {
		t.Fatalf("rel count %d want %d", len(gotRels), len(wantRels))
	}
	for i := range wantRels {
		if gotRels[i].ID != wantRels[i].ID || gotRels[i].FromID != wantRels[i].FromID || gotRels[i].ToID != wantRels[i].ToID {
			t.Fatalf("union rel order changed at %d: got %+v want %+v", i, gotRels[i], wantRels[i])
		}
	}
	if !reflect.DeepEqual(gotRepo, wantRepo) {
		t.Error("entityRepo attribution changed")
	}
	if got, want := graph.CommunityInputHash(gotEnts, gotRels), graph.CommunityInputHash(wantEnts, wantRels); got != want {
		t.Fatalf("CommunityInputHash changed: got %s want %s — every persisted overlay memo would be silently invalidated", got, want)
	}
}

// TestIncremental_OneShot_DoesNotMemoize is the RED assertion for hazard 2: the
// one-shot entrypoint must NOT leave a second full *AlgorithmResults in the
// process-local memo, because the process is about to exit and the memo can
// never be read again — it is pure retention across the overlay write.
func TestIncremental_OneShot_DoesNotMemoize(t *testing.T) {
	group, _, _, _ := setupIncrGroup(t)

	res, err := RunGroupAlgorithmsIncrementalOneShot(group)
	if err != nil {
		t.Fatalf("one-shot incremental run: %v", err)
	}
	if res.Skipped {
		t.Fatal("first run must NOT skip (nothing computed yet)")
	}
	if _, ok := loadMemoizedGroupResult(group, res.InputHash); ok {
		t.Fatal("one-shot group-algo stored the full AlgorithmResults in the process-local memo — the short-lived --write child retains a second copy of the PageRank/community/centrality maps for the whole overlay-write window")
	}
}

// TestIncremental_Daemon_StillMemoizes is the other half of the mutation pair:
// the in-process/daemon entrypoint MUST still memoize — that memo is the guard
// that bounds the O(V*E) betweenness recompute to once per group-version when
// the overlay cannot be persisted (#5309 / the group-scope CPU spin). Inverting
// the condition must fail exactly one of these two tests.
func TestIncremental_Daemon_StillMemoizes(t *testing.T) {
	group, _, _, _ := setupIncrGroup(t)

	res, err := RunGroupAlgorithmsIncremental(group)
	if err != nil {
		t.Fatalf("daemon incremental run: %v", err)
	}
	if _, ok := loadMemoizedGroupResult(group, res.InputHash); !ok {
		t.Fatal("daemon-path group-algo did NOT memoize — the compute-once-per-version guard is gone")
	}
}

// TestOneShot_ResultAndOverlayIdenticalToDaemonPath proves the memo skip is
// behaviour-neutral end to end: the one-shot path must produce the same
// input_hash, the same algorithm results, and the same on-disk overlay bytes as
// the memoizing daemon path (modulo the two intentionally non-deterministic
// fields: the wall-clock computed_at and stats.runtime_ms).
func TestOneShot_ResultAndOverlayIdenticalToDaemonPath(t *testing.T) {
	group, _, _, _ := setupIncrGroup(t)
	path, perr := OverlayPath(group)
	if perr != nil {
		t.Fatalf("overlay path: %v", perr)
	}

	writeAndRead := func(res *GroupAlgoResult) []byte {
		t.Helper()
		if err := WriteOverlayFromResult(res); err != nil {
			t.Fatalf("write overlay: %v", err)
		}
		var ov Overlay
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read overlay: %v", err)
		}
		if err := json.Unmarshal(b, &ov); err != nil {
			t.Fatalf("decode overlay: %v", err)
		}
		ov.ComputedAt = time.Time{}
		ov.Stats.RuntimeMS = 0
		norm, err := json.Marshal(&ov)
		if err != nil {
			t.Fatalf("re-encode overlay: %v", err)
		}
		return norm
	}

	oneShot, err := RunGroupAlgorithmsIncrementalOneShot(group)
	if err != nil {
		t.Fatalf("one-shot run: %v", err)
	}
	oneShotBytes := writeAndRead(oneShot)

	// Fresh process-equivalent state: clear the memo AND the overlay so the
	// daemon path takes the same full-compute branch.
	resetGroupAlgoMemo()
	if err := os.Remove(path); err != nil {
		t.Fatalf("remove overlay: %v", err)
	}

	daemonRes, err := RunGroupAlgorithmsIncremental(group)
	if err != nil {
		t.Fatalf("daemon run: %v", err)
	}
	daemonBytes := writeAndRead(daemonRes)

	if oneShot.InputHash != daemonRes.InputHash {
		t.Fatalf("input_hash differs between one-shot and daemon path: %s != %s", oneShot.InputHash, daemonRes.InputHash)
	}
	resultsEqual(t, daemonRes.Results, oneShot.Results)
	if string(oneShotBytes) != string(daemonBytes) {
		t.Fatal("overlay bytes differ between the one-shot and daemon paths")
	}
}
