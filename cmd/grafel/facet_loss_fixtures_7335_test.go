package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// Issue #7335, the real-index half. facet_loss_7335_test.go drives
// buildDocument with hand-built records, which grades the detector but cannot
// grade whether it ever fires on a DEFAULT-ON index — the gap that made this
// defect survivable for years. These tests run the full indexer over shipped
// golden fixtures (no custom-extractor gate, no hand-built EntityRecord) and
// pin BOTH directions:
//
//   - fixtures where a facet is genuinely destroyed: the diagnostic names the
//     pair, and the issue's own predictions are confirmed as live populations;
//   - fixtures that DO reach the dedup branch and lose nothing: the diagnostic
//     is silent. That is the noise test.
//
// THE NOISE TEST IS GATED ON Drops > 0, AND THAT GATE IS THE WHOLE POINT.
// The first version of this file rostered five fixtures for the silent
// direction, and four of them (python-django, csharp-aspnet-core,
// kotlin-spring, vbnet) never enter the dedup branch AT ALL — measured
// drops=0. Their `Conflicts == 0` was satisfied by an empty loop: it would
// have passed with the classification inverted, or with the detector deleted,
// and the per-fixture comments justifying them ("partial-class country",
// "class-heavy") asserted mechanisms that produce zero collisions. Requiring
// Drops > 0 turns each roster entry into a claim that can fail: a fixture that
// silently stops colliding now FAILS instead of passing quietly, which is the
// only version of an absence assertion worth writing.
//
// These numbers settle what the #7335 scoping comment listed as unverified —
// "that the second record is destroyed at index.go:6304 specifically rather
// than by an earlier fold is unverified for every pair except ormlink", and
// for swift "which mechanism collapses it is undetermined". It is this
// mechanism, and these are the pairs.
//
// THE FIVE NAMES BELOW ARE NO LONGER THE POPULATION. #7335 arm 2 added
// facet_loss_population_7335_test.go, which derives the fixture list from disk
// and pins every fixture's measurement — so a fixture outside this roster that
// starts or stops losing a facet now fails there. What stays here is what a
// derived baseline cannot carry: the per-pair prose saying WHICH producers
// collide and why that shape matters.

func facetLossOnFixture(t *testing.T, fixture string) facetLossStats {
	t.Helper()
	abs, err := filepath.Abs(filepath.Join("../../internal/quality/golden", fixture, "src"))
	if err != nil {
		t.Fatalf("abs: %v", err)
	}
	if _, serr := os.Stat(abs); serr != nil {
		t.Skipf("fixture %s not found at %s: %v", fixture, abs, serr)
	}
	idx := newTestIndexer(t, fixture, nil, "")
	if _, err := idx.Run(context.Background(), abs); err != nil {
		t.Fatalf("Run(%s): %v", fixture, err)
	}
	return idx.facetLoss
}

// hasConflict reports whether the stats attribute a loss with exactly this
// content. Matching on content, not on a count, is the point: a count floor is
// satisfied by the wrong pair.
func hasConflict(s facetLossStats, name, file, field, survivor, dropped string) bool {
	for _, r := range s.Records {
		if r.Name == name && r.SourceFile == file && r.Field == field &&
			r.Survivor == survivor && r.Dropped == dropped {
			return true
		}
	}
	return false
}

// swift `struct User` at line 8 and `extension User` at line 44 in one shipped
// fixture file. swiftDeclSubtype has no `extension` case so both land as
// SCOPE.Component/User/User.swift, and the fixture's expected.json expects
// exactly ONE such node — the extension's subtype and its whole 44-54 span are
// destroyed here.
func TestFacetLossOnFixture_SwiftStructVsExtension(t *testing.T) {
	s := facetLossOnFixture(t, "swift-swiftui-mini")
	const file = "Sources/App/User.swift"
	if !hasConflict(s, "User", file, "subtype", "struct", "class") {
		t.Errorf("no subtype loss attributed for User in %s; got %+v", file, s.Records)
	}
	if !hasConflict(s, "User", file, "start_line", "8", "44") {
		t.Errorf("the destroyed record's line 44 is not named; the report must point at the "+
			"second declaration or it is unactionable. got %+v", s.Records)
	}
	// ONE record destroyed, raising THREE conflicts. Pinning both numbers is
	// what keeps the headline honest: a conflict count reads as a count of
	// destroyed entities and is not one.
	if s.LostRecords != 1 || s.LostEntities != 1 || s.Conflicts != 3 {
		t.Errorf("lost_records=%d lost_entities=%d conflicts=%d; want 1/1/3 — one destroyed "+
			"declaration raising three field conflicts", s.LostRecords, s.LostEntities, s.Conflicts)
	}
}

// `struct MemoryStore` plus its `impl` blocks — the most common Rust file
// layout there is. buildImpl deliberately names the impl entity after the bare
// implementing type, so every impl block collides with the struct.
func TestFacetLossOnFixture_RustStructVsImpl(t *testing.T) {
	s := facetLossOnFixture(t, "rust-tokio-mini")
	if !hasConflict(s, "MemoryStore", "store.rs", "subtype", "struct", "impl") {
		t.Errorf("no struct-vs-impl subtype loss attributed for MemoryStore; got %+v", s.Records)
	}
	// TWO impl blocks collide with the one struct (inherent + trait impl), so
	// the signature of each is destroyed separately. Naming both is what tells
	// a reader this is not a single accident.
	if !hasConflict(s, "MemoryStore", "store.rs", "signature", "pub struct MemoryStore", "impl MemoryStore") ||
		!hasConflict(s, "MemoryStore", "store.rs", "signature", "pub struct MemoryStore", "impl UserStore for MemoryStore") {
		t.Errorf("both impl blocks must be attributed separately; got %+v", s.Records)
	}
	// This is the case that forces LostRecords and LostEntities apart: TWO
	// records destroyed under ONE entity id. A distinct-id count alone would
	// report 1 where 2 died.
	if s.LostRecords != 2 || s.LostEntities != 1 {
		t.Errorf("lost_records=%d lost_entities=%d; want 2/1 — two impl blocks destroyed under "+
			"one colliding id", s.LostRecords, s.LostEntities)
	}
}

// elixir multi-clause function heads: `def handle_cast({:put, key, value},
// state)` at line 39 and `def handle_cast(:flush, _state)` at line 44 in one
// module. This is the #6440 shape — the shape whose prose the incremental
// port already enumerates — reproduced on a default-ON index in a language
// nobody had checked. It was missed by the first pass of this work because
// that pass hand-picked fixtures instead of sweeping them.
func TestFacetLossOnFixture_ElixirMultiClauseFunctionHead(t *testing.T) {
	s := facetLossOnFixture(t, "elixir-phoenix-mini")
	const file = "lib/demo/cache_server.ex"
	if !hasConflict(s, "handle_cast", file, "signature",
		"def handle_cast({:put, key, value}, state)", "def handle_cast(:flush, _state)") {
		t.Errorf("the destroyed second clause's signature is not named; got %+v", s.Records)
	}
	if !hasConflict(s, "handle_cast", file, "start_line", "39", "44") {
		t.Errorf("the destroyed clause's line 44 is not named; got %+v", s.Records)
	}
	if s.LostRecords != 1 || s.Conflicts != 3 {
		t.Errorf("lost_records=%d conflicts=%d; want 1/3", s.LostRecords, s.Conflicts)
	}
}

// THE NOISE TEST. Every fixture below MEASURABLY reaches the #4406 dedup
// branch (Drops > 0) and loses nothing: its drops are by-design or fully
// repaired by the gap-fill. If the diagnostic were dominated by these it would
// be ignored, and an ignored instrument is worse than none because it reads as
// "we looked".
//
// The Drops assertion is load-bearing. Without it a fixture that stops
// colliding — a producer fix, a new fold, a fixture edit — turns this into a
// vacuous pass, which is precisely how the previous version of this roster
// graded nothing for four of its five entries.
//
// The positive controls in facet_loss_7335_test.go (sentinel repair defeated;
// GRAFEL_DISABLE_1613_FOLD set) are what keep the silence here from being the
// silence of an instrument that never ran. So is the mutant evidence, stated
// precisely because the imprecise version was the review finding: making the
// detector report equal values (M2) fails ALL FIVE subtests; deleting the drop
// counter (M13) or counting drops only when lossy (M16) fails all five at the
// gate. Dropping the survivor-non-empty guard (M4) fails exactly ONE —
// java-spring-mini — so M4 alone would not have graded this roster.
func TestFacetLossOnFixture_SilentWhereDropsAreByDesign(t *testing.T) {
	// fixture → drops measured at the time of writing. The number is the
	// claim: it is not asserted exactly (a fixture may legitimately gain a
	// by-design drop) but a fall to zero is a failure, because zero drops
	// means this fixture grades nothing.
	for _, tc := range []struct {
		fixture string
		drops   int
	}{
		{"java-spring-mini", 5}, // ormlink sentinels + #1613 hierarchy shadows (#6275)
		{"clojure-protocols-mini", 3},
		{"groovy-grails-mini", 1},
		{"java-quartz-mini", 1},
		{"php-slim-mini", 1},
	} {
		tc := tc
		t.Run(tc.fixture, func(t *testing.T) {
			s := facetLossOnFixture(t, tc.fixture)
			if s.Drops == 0 {
				t.Fatalf("%s no longer reaches the dedup branch (measured %d drops when this "+
					"roster was written). Conflicts==0 here would be an empty loop, not evidence: "+
					"replace this entry with a fixture that still collides", tc.fixture, tc.drops)
			}
			if s.Conflicts != 0 || s.LostRecords != 0 {
				t.Errorf("%s: %d drops produced %d conflicts across %d destroyed records; a "+
					"diagnostic dominated by by-design drops gets ignored: %+v",
					tc.fixture, s.Drops, s.Conflicts, s.LostRecords, s.Records)
			}
		})
	}
}
