package graph

// #6779 — the entity-kind VOCABULARY has no version, so a kind rename
// (#6451's SCOPE.ExternalAPI split, #6499's class-qualified Kotlin operation
// names, and the ~530-site #6776 migration that is blocked on this) leaves
// every already-indexed graph speaking the old vocabulary with nothing
// anywhere saying so. The failure mode is silence: a query filtering on the
// new kind returns EMPTY against a graph that looks perfectly healthy.
//
// The mechanism under test is deliberately THREE-STATE. "Current" and "no
// graph at all" are not the same answer, and a check that collapses them
// reproduces the exact defect this issue exists to fix — a report that cannot
// tell "this graph is fine" from "there is nothing here".

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/types"
)

// seedStoredGraph6779 writes a REAL stored graph (graph.json, read back by the
// shipping LoadGraphFromDir resolution) plus a REAL graph-stats.json sidecar
// (graph.WriteSidecar) into a fresh state dir, stamping the sidecar with the
// given vocabulary version. stamp < 0 means "write the sidecar with no
// vocabulary field at all" — the shape every sidecar written before this
// mechanism existed has on disk.
func seedStoredGraph6779(t *testing.T, stamp int) string {
	t.Helper()
	dir := t.TempDir()
	doc := &Document{
		Version:     SchemaVersion,
		GeneratedAt: time.Now(),
		Repo:        "legacy",
		Entities: []Entity{{
			ID:   "e1",
			Name: "Widget",
			Kind: string(types.EntityKindComponent),
		}},
	}
	if err := WriteAtomic(filepath.Join(dir, "graph.json"), doc, false); err != nil {
		t.Fatalf("write graph.json: %v", err)
	}
	side := &GraphStatsSidecar{Version: 1, ComputedAt: time.Now(), TotalEntities: 1}
	if stamp >= 0 {
		side.KindVocabularyVersion = stamp
	}
	if err := WriteSidecar(SidecarPath(dir), side, false); err != nil {
		t.Fatalf("write sidecar: %v", err)
	}
	return dir
}

// TestKindVocabularyState_ThreeStates pins the three-state property: current,
// older-vocabulary and no-graph are three DISTINCT answers.
func TestKindVocabularyState_ThreeStates(t *testing.T) {
	if types.KindVocabularyVersion < 1 {
		t.Fatalf("premise: KindVocabularyVersion must be >= 1, got %d", types.KindVocabularyVersion)
	}

	t.Run("current", func(t *testing.T) {
		dir := seedStoredGraph6779(t, types.KindVocabularyVersion)
		state, stored := KindVocabularyStateForDir(dir)
		if state != KindVocabularyCurrent {
			t.Fatalf("state = %q, want %q", state, KindVocabularyCurrent)
		}
		if stored != types.KindVocabularyVersion {
			t.Fatalf("stored = %d, want %d", stored, types.KindVocabularyVersion)
		}
	})

	t.Run("older", func(t *testing.T) {
		dir := seedStoredGraph6779(t, types.KindVocabularyVersion-1)
		state, stored := KindVocabularyStateForDir(dir)
		if state != KindVocabularyOlder {
			t.Fatalf("state = %q, want %q", state, KindVocabularyOlder)
		}
		if stored != types.KindVocabularyVersion-1 {
			t.Fatalf("stored = %d, want %d", stored, types.KindVocabularyVersion-1)
		}
	})

	t.Run("older_unstamped_sidecar", func(t *testing.T) {
		// A sidecar written before this mechanism existed carries no
		// vocabulary field. That graph IS on an older vocabulary — it must not
		// be waved through as current, and it must not be reported as "no
		// graph": the graph is right there.
		dir := seedStoredGraph6779(t, -1)
		state, stored := KindVocabularyStateForDir(dir)
		if state != KindVocabularyOlder {
			t.Fatalf("state = %q, want %q", state, KindVocabularyOlder)
		}
		if stored != 0 {
			t.Fatalf("stored = %d, want 0", stored)
		}
	})

	t.Run("no_graph", func(t *testing.T) {
		dir := t.TempDir()
		state, _ := KindVocabularyStateForDir(dir)
		if state != KindVocabularyNoGraph {
			t.Fatalf("state = %q, want %q", state, KindVocabularyNoGraph)
		}
	})

	t.Run("no_graph_is_not_older", func(t *testing.T) {
		// The collapse this issue is about, stated as its own assertion: an
		// empty state dir has no stored vocabulary version either, so a check
		// keyed ONLY on "stored != current" would call it older-vocabulary and
		// tell the user to reindex a repo that was never indexed.
		empty := t.TempDir()
		older := seedStoredGraph6779(t, types.KindVocabularyVersion-1)
		es, _ := KindVocabularyStateForDir(empty)
		os_, _ := KindVocabularyStateForDir(older)
		if es == os_ {
			t.Fatalf("no-graph and older-vocabulary collapsed to the same state %q", es)
		}
	})

	t.Run("sidecar_without_graph_is_no_graph", func(t *testing.T) {
		// The mirror collapse: a stale sidecar left behind by a `reset` that
		// removed the graph must report no-graph, not older-vocabulary.
		dir := t.TempDir()
		side := &GraphStatsSidecar{Version: 1, ComputedAt: time.Now()}
		if err := WriteSidecar(SidecarPath(dir), side, false); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(filepath.Join(dir, "graph.json")); err == nil {
			t.Fatal("premise: no graph.json should exist here")
		}
		state, _ := KindVocabularyStateForDir(dir)
		if state != KindVocabularyNoGraph {
			t.Fatalf("state = %q, want %q", state, KindVocabularyNoGraph)
		}
	})

	t.Run("newer_than_this_build_is_not_older", func(t *testing.T) {
		// A graph written by a NEWER binary is not something this build can
		// ask the user to fix by reindexing with this build. Treat it as
		// current rather than nagging.
		dir := seedStoredGraph6779(t, types.KindVocabularyVersion+1)
		state, _ := KindVocabularyStateForDir(dir)
		if state != KindVocabularyCurrent {
			t.Fatalf("state = %q, want %q", state, KindVocabularyCurrent)
		}
	})
}

// TestKindVocabularyState_GraphWithNoReadableSidecar covers the state every
// fixture in the first cut of this file silently skipped: a graph is stored,
// but its sidecar is absent or unreadable.
//
// That is not a contrived shape. internal/daemon's worktree seeding copies the
// graph artifact and the diff manifest and NOT graph-stats.json, so every
// seeded worktree lands here. The branch's rule — an unreadable stamp cannot
// prove a graph current — was asserted by nothing, and flipping it to
// KindVocabularyCurrent kept every other test in the tree green.
func TestKindVocabularyState_GraphWithNoReadableSidecar(t *testing.T) {
	writeGraphOnly := func(t *testing.T) string {
		t.Helper()
		dir := t.TempDir()
		doc := &Document{
			Version:     SchemaVersion,
			GeneratedAt: time.Now(),
			Repo:        "legacy",
			Entities: []Entity{{
				ID:   "e1",
				Name: "Widget",
				Kind: string(types.EntityKindComponent),
			}},
		}
		if err := WriteAtomic(filepath.Join(dir, "graph.json"), doc, false); err != nil {
			t.Fatalf("write graph.json: %v", err)
		}
		return dir
	}

	t.Run("sidecar_absent", func(t *testing.T) {
		dir := writeGraphOnly(t)
		if _, err := os.Stat(SidecarPath(dir)); err == nil {
			t.Fatal("premise: this fixture must have NO sidecar")
		}
		state, stored := KindVocabularyStateForDir(dir)
		if state != KindVocabularyOlder {
			t.Fatalf("graph with no sidecar reads as %q, want %q — an unstamped graph cannot prove it is current",
				state, KindVocabularyOlder)
		}
		if stored != 0 {
			t.Fatalf("stored = %d, want 0", stored)
		}
	})

	t.Run("sidecar_unparseable", func(t *testing.T) {
		dir := writeGraphOnly(t)
		if err := os.WriteFile(SidecarPath(dir), []byte("{not json"), 0o644); err != nil {
			t.Fatal(err)
		}
		state, _ := KindVocabularyStateForDir(dir)
		if state != KindVocabularyOlder {
			t.Fatalf("graph with a corrupt sidecar reads as %q, want %q", state, KindVocabularyOlder)
		}
	})

	t.Run("sidecar_is_a_directory", func(t *testing.T) {
		dir := writeGraphOnly(t)
		if err := os.MkdirAll(SidecarPath(dir), 0o755); err != nil {
			t.Fatal(err)
		}
		state, _ := KindVocabularyStateForDir(dir)
		if state != KindVocabularyOlder {
			t.Fatalf("graph whose sidecar path is a directory reads as %q, want %q", state, KindVocabularyOlder)
		}
	})
}

// TestKindVocabularyStateFor_OlderIsNotTheSameAsUnstamped pins the distinction
// the exported entry point cannot express while KindVocabularyVersion is 1:
// a graph stamped with a genuinely older NON-ZERO version, versus one that
// was never stamped at all.
//
// Both answer "older" — the same advice applies — but the STORED number is
// reported back to the user by doctor ("graph v%d"), so conflating them would
// tell someone on v1 that their graph is on v0. #6776 takes the version to 2
// and makes this reachable; pinning it now means the reader is already right
// when that lands.
func TestKindVocabularyStateFor_OlderIsNotTheSameAsUnstamped(t *testing.T) {
	const futureCurrent = 2

	if state, stored := kindVocabularyStateFor(futureCurrent, 1, true); state != KindVocabularyOlder || stored != 1 {
		t.Errorf("a v1 graph under a v%d build = (%q, %d), want (%q, 1)",
			futureCurrent, state, stored, KindVocabularyOlder)
	}
	if state, stored := kindVocabularyStateFor(futureCurrent, 0, true); state != KindVocabularyOlder || stored != 0 {
		t.Errorf("an unstamped graph under a v%d build = (%q, %d), want (%q, 0)",
			futureCurrent, state, stored, KindVocabularyOlder)
	}
	// The two must not report the same stored version: same verdict, different
	// fact about the graph.
	_, olderStored := kindVocabularyStateFor(futureCurrent, 1, true)
	_, unstampedStored := kindVocabularyStateFor(futureCurrent, 0, true)
	if olderStored == unstampedStored {
		t.Errorf("a genuinely older stamp and an absent one both report stored=%d — doctor would misname the user's graph version", olderStored)
	}
	// And the boundary stays where it belongs.
	if state, _ := kindVocabularyStateFor(futureCurrent, futureCurrent, true); state != KindVocabularyCurrent {
		t.Errorf("a v%d graph under a v%d build = %q, want %q", futureCurrent, futureCurrent, state, KindVocabularyCurrent)
	}
}
